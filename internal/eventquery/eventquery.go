// Package eventquery answers questions about an event model in the shape a
// reader can hold: which kinds and components match a word, and how the whole
// thing is wired.
//
// It exists because the two answers already available are both the wrong size.
// `event_model` returns the composed model with every payload schema — 87 KB
// on a nine-component repository, most of it JSON Schema nobody asked for —
// and `event_kind` answers only when you already know a kind's exact full
// name. Between "everything" and "this exact thing" there was nothing, which
// is the range every actual question falls in.
//
// Nothing here renders for a browser. The output is lines, because the callers
// are a terminal and an agent's context window.
package eventquery

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/kgatilin/wyrd/internal/eventmodel"
)

// Hit is one match: a kind or a component, with the facts that place it in the
// graph. Payload schemas are deliberately absent — a reader who wants one asks
// for that kind by name.
type Hit struct {
	Type string `json:"type"` // "kind" or "component"
	Name string `json:"name"`
	// Matched names the field the query hit, so a surprising result explains
	// itself instead of looking like a bad search.
	Matched     string `json:"matched"`
	Description string `json:"description,omitempty"`
	SourceFile  string `json:"source_file,omitempty"`

	// Kind facts.
	Subjects  []string `json:"subjects,omitempty"`
	Delivery  string   `json:"delivery,omitempty"`
	Health    string   `json:"health,omitempty"`
	Owner     string   `json:"owner,omitempty"`
	Producers []string `json:"producers,omitempty"`
	Triggers  []string `json:"triggers,omitempty"`
	Folders   []string `json:"folders,omitempty"`

	// Component facts. The counts summarise; the lists are the answer. A
	// count on its own ends the enquiry it started — a reader who learns a
	// component takes two kinds and appends five has to go to the wiring and
	// grep to learn which, and that second lookup is the one this command
	// exists to save.
	Owns         string   `json:"owns,omitempty"`
	PartitionKey []string `json:"partition_key,omitempty"`
	Inputs       int      `json:"inputs,omitempty"`
	Outputs      int      `json:"outputs,omitempty"`
	StateEvents  int      `json:"state_events,omitempty"`
	Takes        []string `json:"takes,omitempty"`
	Appends      []string `json:"appends,omitempty"`
	Folds        []string `json:"folds,omitempty"`

	score int
}

// Match scores, highest first. Ranked by how likely the reader meant this hit
// rather than how rare the match is: somebody typing a kind's full name wants
// that kind, not the six components whose description mentions it.
const (
	scoreExactName   = 100
	scorePrefixName  = 80
	scoreSubName     = 60
	scoreSubject     = 40
	scoreOwns        = 30
	scoreDescription = 20
)

// Search returns the kinds and components matching query, best first, and the
// number that matched before limit was applied.
//
// The caller gets both because a page that reports its own length as the total
// is a wrong answer with no tell: a reader who asks for "event" and is told
// "20 match(es)" stops looking, and the other 28 are never mentioned.
//
// Matching is a case-insensitive substring over the fields a reader actually
// remembers: names, wire addresses, owned namespaces and descriptions. There
// is no embedding index here on purpose — an event model is hundreds of
// entries, not the hundreds of thousands the code graph carries, and a
// substring that always finds `turn.completed` beats a ranking that sometimes
// does.
func Search(m *eventmodel.Model, query string, limit int) (hits []Hit, total int) {
	needle := strings.ToLower(strings.TrimSpace(query))
	if m == nil || needle == "" {
		return nil, 0
	}

	facts := collectKinds(m)
	hits = make([]Hit, 0, len(facts)+len(m.Components))

	for _, name := range sortedKeys(facts) {
		k := facts[name]
		if score, field := scoreKind(k, needle); score > 0 {
			hit := k.hit()
			hit.score, hit.Matched = score, field
			hits = append(hits, hit)
		}
	}
	for _, id := range sortedComponentIDs(m) {
		comp := m.Components[id]
		if score, field := scoreComponent(id, comp, needle); score > 0 {
			hit := componentHit(id, comp)
			hit.score, hit.Matched = score, field
			hits = append(hits, hit)
		}
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].Name < hits[j].Name
	})
	total = len(hits)
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, total
}

func scoreKind(k *kindFacts, needle string) (int, string) {
	name := strings.ToLower(k.name)
	switch {
	case name == needle:
		return scoreExactName, "name"
	case strings.HasPrefix(name, needle):
		return scorePrefixName, "name"
	case strings.Contains(name, needle):
		return scoreSubName, "name"
	}
	for _, subject := range k.subjects {
		if strings.Contains(strings.ToLower(subject), needle) {
			return scoreSubject, "subject"
		}
	}
	if k.description != "" && strings.Contains(strings.ToLower(k.description), needle) {
		return scoreDescription, "description"
	}
	return 0, ""
}

func scoreComponent(id string, comp *eventmodel.Component, needle string) (int, string) {
	name := strings.ToLower(id)
	switch {
	case name == needle:
		return scoreExactName, "id"
	case strings.HasPrefix(name, needle):
		return scorePrefixName, "id"
	case strings.Contains(name, needle):
		return scoreSubName, "id"
	}
	if comp.Owns != "" && strings.Contains(strings.ToLower(comp.Owns), needle) {
		return scoreOwns, "owns"
	}
	if comp.Description != "" && strings.Contains(strings.ToLower(comp.Description), needle) {
		return scoreDescription, "description"
	}
	return 0, ""
}

// kindFacts is one kind gathered from every component that names it. A kind is
// not declared anywhere in particular — it exists wherever it is mentioned —
// so the answer to "what is this kind" has to be assembled.
type kindFacts struct {
	name        string
	description string
	delivery    string
	subjects    []string
	producers   []string
	triggers    []string
	folders     []string
	sourceFile  string
}

func (k *kindFacts) hit() Hit {
	return Hit{
		Type:        "kind",
		Name:        k.name,
		Description: k.description,
		SourceFile:  k.sourceFile,
		Subjects:    k.subjects,
		Delivery:    k.delivery,
		Producers:   k.producers,
		Triggers:    k.triggers,
		Folders:     k.folders,
	}
}

func collectKinds(m *eventmodel.Model) map[string]*kindFacts {
	out := map[string]*kindFacts{}
	get := func(name string) *kindFacts {
		if k, ok := out[name]; ok {
			return k
		}
		k := &kindFacts{name: name}
		out[name] = k
		return k
	}

	for _, id := range sortedComponentIDs(m) {
		comp := m.Components[id]
		for _, section := range []struct {
			slots []eventmodel.Slot
			into  func(*kindFacts) *[]string
		}{
			{comp.Outputs, func(k *kindFacts) *[]string { return &k.producers }},
			{comp.Inputs, func(k *kindFacts) *[]string { return &k.triggers }},
			{comp.StateEvents, func(k *kindFacts) *[]string { return &k.folders }},
		} {
			for _, slot := range section.slots {
				k := get(slot.Kind)
				*section.into(k) = appendUnique(*section.into(k), id)
				if slot.Pattern != "" {
					k.subjects = appendUnique(k.subjects, slot.Pattern)
				}
				if k.description == "" {
					k.description = slot.Description
				}
				if k.sourceFile == "" {
					k.sourceFile = comp.SourceFile
				}
				if slot.Delivery.IsExclusive() {
					k.delivery = "exclusive"
				}
			}
		}
	}
	return out
}

func componentHit(id string, comp *eventmodel.Component) Hit {
	takes, appends, folds := slotKinds(comp.Inputs), slotKinds(comp.Outputs), slotKinds(comp.StateEvents)
	// The counts count the lists printed under them. Counting declarations
	// instead would put "5 out" over four kinds whenever a component puts one
	// kind on two addresses, which reads as a list that was cut short.
	return Hit{
		Type:         "component",
		Name:         id,
		Description:  comp.Description,
		SourceFile:   comp.SourceFile,
		Owns:         comp.Owns,
		PartitionKey: comp.PartitionKey,
		Inputs:       len(takes),
		Outputs:      len(appends),
		StateEvents:  len(folds),
		Takes:        takes,
		Appends:      appends,
		Folds:        folds,
	}
}

// slotKinds names the kinds in a slot list, once each. A component that puts
// one kind on one address per port instance declares it several times and
// takes it once.
func slotKinds(slots []eventmodel.Slot) []string {
	var out []string
	for _, slot := range slots {
		out = appendUnique(out, slot.Kind)
	}
	sort.Strings(out)
	return out
}

// WithHealth stamps each kind hit with the health the graph computed for it,
// so a search result carries the same verdict the canvas and the gate report.
func WithHealth(hits []Hit, g *eventmodel.Graph) []Hit {
	if g == nil {
		return hits
	}
	health := map[string]string{}
	for _, node := range g.Nodes {
		if node.Kind != eventmodel.NodeEventKind {
			continue
		}
		if h, ok := node.Attrs["health"].(string); ok {
			health[strings.TrimPrefix(node.ID, "kind:")] = h
		}
	}
	for i, hit := range hits {
		if hit.Type == "kind" {
			hits[i].Health = health[hit.Name]
		}
	}
	return hits
}

// FormatHits renders search results as lines. total is how many matched
// before the limit cut the list, which the header states whenever the two
// differ — a page that presents itself as the whole answer is the one kind of
// wrong a reader cannot detect.
func FormatHits(w io.Writer, hits []Hit, total int, query string) {
	if len(hits) == 0 {
		fmt.Fprintf(w, "No event kind or component matches %q.\n", query)
		return
	}
	if total > len(hits) {
		fmt.Fprintf(w, "showing %d of %d match(es) for %q  (--limit 0 for all)\n\n", len(hits), total, query)
	} else {
		fmt.Fprintf(w, "%d match(es) for %q\n\n", total, query)
	}
	for _, hit := range hits {
		if hit.Type == "component" {
			fmt.Fprintf(w, "component  %s", hit.Name)
			if hit.Owns != "" {
				fmt.Fprintf(w, "  owns %s", hit.Owns)
			}
			if len(hit.PartitionKey) > 0 {
				fmt.Fprintf(w, "  keyed by %s", strings.Join(hit.PartitionKey, ", "))
			}
			fmt.Fprintf(w, "\n           %d in / %d out / %d state  (matched %s)\n",
				hit.Inputs, hit.Outputs, hit.StateEvents, hit.Matched)
			if hit.Description != "" {
				fmt.Fprintf(w, "           %s\n", hit.Description)
			}
			writeKinds(w, "triggered by", hit.Takes)
			writeKinds(w, "appends", hit.Appends)
			writeKinds(w, "folds", hit.Folds)
			fmt.Fprintln(w)
			continue
		}

		fmt.Fprintf(w, "kind       %s", hit.Name)
		if hit.Delivery != "" {
			fmt.Fprintf(w, "  [%s]", hit.Delivery)
		}
		if hit.Health != "" && hit.Health != "ok" {
			fmt.Fprintf(w, "  %s", strings.ToUpper(hit.Health))
		}
		fmt.Fprintf(w, "  (matched %s)\n", hit.Matched)
		if hit.Description != "" {
			fmt.Fprintf(w, "           %s\n", hit.Description)
		}
		for _, subject := range hit.Subjects {
			fmt.Fprintf(w, "           on %s\n", subject)
		}
		writeRole(w, "appended by", hit.Producers)
		writeRole(w, "triggers", hit.Triggers)
		writeRole(w, "folded by", hit.Folders)
		fmt.Fprintln(w)
	}
}

func writeRole(w io.Writer, label string, who []string) {
	if len(who) == 0 {
		return
	}
	fmt.Fprintf(w, "           %-12s %s\n", label, strings.Join(who, ", "))
}

// writeKinds lists a component's kinds one per line. Comma-joining them would
// fit more on the screen and less through a grep, and the reader of this
// output is usually piping it.
func writeKinds(w io.Writer, label string, kinds []string) {
	if len(kinds) == 0 {
		return
	}
	for i, kind := range kinds {
		head := ""
		if i == 0 {
			head = label
		}
		fmt.Fprintf(w, "           %-12s %s\n", head, kind)
	}
}

// Wiring renders the whole model as one append per destination, grouped by
// the component that appends.
//
// Out-edges only, so every relation appears exactly once and the listing is
// the graph rather than a view of it. Resolution is per address, from
// eventmodel.BuildAppends: a component that publishes one kind on several
// addresses reaches a different observer from each, and pairing every address
// with every observer of the kind — which is what joining on the kind name
// does — prints relations the model does not have. When one kind travels more
// than one address out of a component, each line names the address it took,
// because that fan-out is the whole point of addressing it.
//
// An append nobody hears still gets its line, pointing at nobody: it is the
// finding a reader is most often looking for, and leaving it out would hide it
// behind a blank.
func Wiring(m *eventmodel.Model, g *eventmodel.Graph, w io.Writer) {
	if m == nil || len(m.Components) == 0 {
		fmt.Fprintln(w, "No event declarations found.")
		return
	}
	if g == nil {
		g = eventmodel.BuildGraph(m)
	}
	facts := collectKinds(m)

	// Appends by the component that made them, and the addresses each kind
	// left that component on — the second is what decides whether a line has
	// to name its address to be unambiguous.
	byComponent := map[string][]eventmodel.Append{}
	addresses := map[[2]string]map[string]bool{}
	for _, app := range eventmodel.BuildAppends(g) {
		byComponent[app.From] = append(byComponent[app.From], app)
		key := [2]string{app.From, app.Kind}
		if addresses[key] == nil {
			addresses[key] = map[string]bool{}
		}
		addresses[key][app.Subject] = true
	}

	fmt.Fprintf(w, "EVENT MODEL  %d components  %d kinds\n", len(m.Components), len(facts))
	fmt.Fprintln(w, "\u2192 triggers the target \u00b7 \u21e2 folded into its state \u00b7 (nobody) nothing observes it")
	fmt.Fprintln(w, "on <subject>  the address this append took, shown when the kind takes more than one")

	for _, id := range sortedComponentIDs(m) {
		comp := m.Components[id]
		fmt.Fprintf(w, "\n%s", id)
		if comp.Owns != "" {
			fmt.Fprintf(w, "  owns %s", comp.Owns)
		}
		if len(comp.PartitionKey) > 0 {
			fmt.Fprintf(w, "  keyed by %s", strings.Join(comp.PartitionKey, ", "))
		}
		fmt.Fprintln(w)

		appends := byComponent[id]
		if len(appends) == 0 {
			fmt.Fprintln(w, "  (appends nothing)")
			continue
		}
		for _, app := range appends {
			mark := ""
			if app.Health != "" && app.Health != string(eventmodel.HealthOK) {
				mark = "  " + strings.ToUpper(app.Health)
			}
			// Only a kind that left on more than one address needs its line
			// to say which; naming the single address every other append took
			// would double the width of the listing to disambiguate nothing.
			where := ""
			if len(addresses[[2]string{id, app.Kind}]) > 1 {
				where = "  on " + app.Subject
			}
			if len(app.Reaches) == 0 {
				fmt.Fprintf(w, "  \u2192 %-22s %s%s%s\n", "(nobody)", app.Kind, mark, where)
				continue
			}
			for _, reach := range app.Reaches {
				arrow := "\u21e2"
				if reach.Trigger {
					arrow = "\u2192"
				}
				fmt.Fprintf(w, "  %s %-22s %s%s%s\n", arrow, reach.To, app.Kind, mark, where)
			}
		}
	}
}

func appendUnique(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}

func sortedComponentIDs(m *eventmodel.Model) []string {
	out := make([]string, 0, len(m.Components))
	for id := range m.Components {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]*kindFacts) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
