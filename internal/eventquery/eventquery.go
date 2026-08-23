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

	// Component facts.
	Owns         string   `json:"owns,omitempty"`
	PartitionKey []string `json:"partition_key,omitempty"`
	Inputs       int      `json:"inputs,omitempty"`
	Outputs      int      `json:"outputs,omitempty"`
	StateEvents  int      `json:"state_events,omitempty"`

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

// Search returns the kinds and components matching query, best first.
//
// Matching is a case-insensitive substring over the fields a reader actually
// remembers: names, wire addresses, owned namespaces and descriptions. There
// is no embedding index here on purpose — an event model is hundreds of
// entries, not the hundreds of thousands the code graph carries, and a
// substring that always finds `turn.completed` beats a ranking that sometimes
// does.
func Search(m *eventmodel.Model, query string, limit int) []Hit {
	needle := strings.ToLower(strings.TrimSpace(query))
	if m == nil || needle == "" {
		return nil
	}

	facts := collectKinds(m)
	hits := make([]Hit, 0, len(facts)+len(m.Components))

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
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
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
	return Hit{
		Type:         "component",
		Name:         id,
		Description:  comp.Description,
		SourceFile:   comp.SourceFile,
		Owns:         comp.Owns,
		PartitionKey: comp.PartitionKey,
		Inputs:       len(comp.Inputs),
		Outputs:      len(comp.Outputs),
		StateEvents:  len(comp.StateEvents),
	}
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

// FormatHits renders search results as lines.
func FormatHits(w io.Writer, hits []Hit, query string) {
	if len(hits) == 0 {
		fmt.Fprintf(w, "No event kind or component matches %q.\n", query)
		return
	}
	fmt.Fprintf(w, "%d match(es) for %q\n\n", len(hits), query)
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

// Wiring renders the whole model as one flow per line, grouped by the
// component that appends.
//
// Out-edges only, so every flow appears exactly once and the listing is the
// graph rather than a view of it. A kind nobody observes still gets its line,
// pointing at nobody: an append that reaches no one is the finding a reader is
// most often looking for, and leaving it out would hide it behind a blank.
func Wiring(m *eventmodel.Model, g *eventmodel.Graph, w io.Writer) {
	if m == nil || len(m.Components) == 0 {
		fmt.Fprintln(w, "No event declarations found.")
		return
	}
	facts := collectKinds(m)
	health := map[string]string{}
	if g != nil {
		for _, node := range g.Nodes {
			if node.Kind == eventmodel.NodeEventKind {
				if h, ok := node.Attrs["health"].(string); ok {
					health[strings.TrimPrefix(node.ID, "kind:")] = h
				}
			}
		}
	}

	fmt.Fprintf(w, "EVENT MODEL  %d components  %d kinds\n", len(m.Components), len(facts))
	fmt.Fprintln(w, "→ triggers the target · ⇢ folded into its state · (nobody) nothing observes it")

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

		if len(comp.Outputs) == 0 {
			fmt.Fprintln(w, "  (appends nothing)")
			continue
		}
		for _, slot := range comp.Outputs {
			k := facts[slot.Kind]
			mark := ""
			if h := health[slot.Kind]; h != "" && h != "ok" {
				mark = "  " + strings.ToUpper(h)
			}
			observers := 0
			for _, target := range without(k.triggers, id) {
				fmt.Fprintf(w, "  → %-22s %s%s\n", target, slot.Kind, mark)
				observers++
			}
			for _, target := range without(k.folders, id) {
				fmt.Fprintf(w, "  ⇢ %-22s %s%s\n", target, slot.Kind, mark)
				observers++
			}
			if observers == 0 {
				fmt.Fprintf(w, "  → %-22s %s%s\n", "(nobody)", slot.Kind, mark)
			}
		}
	}
}

// without drops one id from a list, so a component folding its own output —
// the normal idiom for recording an outcome — does not draw a line to itself.
func without(ids []string, self string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != self {
			out = append(out, id)
		}
	}
	return out
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
