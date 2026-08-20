package eventmodel

import (
	"fmt"
	"sort"
	"strings"
)

// Validate checks the composed model against the built-in rules from the
// event-model design doc (§2). It returns all findings; callers filter by
// severity as appropriate for their context.
//
// The base model is event-sourced choreography: an event is appended once and
// may be observed independently by any number of components. There is
// therefore NO rule that an output resolves to a single handler, and no rule
// that ownership of a namespace restricts who may append into it or observe
// it. Both were RPC assumptions and have been removed.
//
// Rules implemented:
//   - Single-owner namespaces: one component defines a namespace's schemas
//   - Single pattern per kind: where a kind lives on the wire is global
//   - Self-trigger: a kind is never both an input and an output of one
//     component; folding one's own output is state_events
//   - Closure: starved inputs, starved state events, orphan outputs
//   - Read-set coherence: {slot} syntax, one shared partition key
//   - Exclusive delivery (opt-in): a kind declared `delivery: exclusive`
//     must be the input of exactly one component
//   - $ref integrity: every $ref resolves, no cross-component cycles
//
// NOT implemented (see package doc):
//   - Schema compatibility between an output payload and a consumer's schema
func Validate(m *Model) []Finding {
	var findings []Finding

	// Build indexes for efficient lookup.
	ownerOf := make(map[string]string)          // namespace -> component id
	inputsOf := make(map[string][]string)       // kind -> component ids triggered by it
	foldersOf := make(map[string][]string)      // kind -> component ids folding it as state
	producersOf := make(map[string][]string)    // kind -> component ids that append it
	exclusiveKinds := make(map[string]struct{}) // kinds that opted into single-handler delivery

	// Track ownership claims (map prefix -> list of component IDs claiming it).
	ownerClaims := make(map[string][]string)

	for id, comp := range m.Components {
		// Record ownership prefixes for later validation.
		if comp.Owns != "" {
			ownerClaims[comp.Owns] = append(ownerClaims[comp.Owns], id)
			ownerOf[comp.Owns] = id
		}

		index := func(slots []Slot, into map[string][]string) {
			for _, slot := range slots {
				into[slot.Kind] = append(into[slot.Kind], id)
				if slot.Delivery.IsExclusive() {
					exclusiveKinds[slot.Kind] = struct{}{}
				}
			}
		}
		index(comp.Inputs, inputsOf)
		index(comp.Outputs, producersOf)
		index(comp.StateEvents, foldersOf)
	}

	// Check for overlapping namespace ownership. Exact duplicates and nesting
	// without explicit containment are both errors — sub-namespace ownership
	// must be unique among the declared set.
	findings = append(findings, validateOwnershipOverlaps(m, ownerOf, ownerClaims)...)

	// Per-component validation.
	for id, comp := range m.Components {
		findings = append(findings, validateRefs(id, comp, m)...)
		findings = append(findings, validateReadSet(id, comp)...)
		findings = append(findings, validateState(id, comp)...)
		findings = append(findings, validateSelfInput(id, comp)...)
	}

	// Cross-component validation.
	findings = append(findings, validatePatterns(m)...)
	findings = append(findings, validateClosure(m, inputsOf, foldersOf, producersOf)...)
	findings = append(findings, validateExclusiveDelivery(m, inputsOf, exclusiveKinds)...)
	findings = append(findings, validateRefCycles(m)...)

	sortFindings(findings)
	return findings
}

// OwnerOf returns the id of the component that defines the schemas for kind —
// the claimant of the longest `owns` prefix covering it — or "" when no
// component claims the namespace. Ownership is definitional authority only: it
// says nothing about who may emit the kind or who may observe it.
func OwnerOf(m *Model, kind string) string {
	owner, bestLen := "", -1
	for id, comp := range m.Components {
		if comp.Owns == "" || !kindHasPrefix(kind, comp.Owns) {
			continue
		}
		if len(comp.Owns) > bestLen {
			owner, bestLen = id, len(comp.Owns)
		}
	}
	return owner
}

// validateOwnershipOverlaps detects conflicting namespace ownership claims.
// Ownership is authority over a namespace's schema definitions, so two
// claimants mean two answers to "what does this kind look like". Exact
// duplicates are always errors. Nested prefixes (e.g., "billing" and
// "billing.invoice") are errors because longest-prefix-wins resolution
// requires unique ownership — allowing nesting would make ownership ambiguous
// at declaration time.
//
// This is the only remaining ownership rule. Ownership does not restrict who
// may emit into a namespace or who may observe it.
func validateOwnershipOverlaps(m *Model, ownerOf map[string]string, ownerClaims map[string][]string) []Finding {
	var findings []Finding

	// Check for exact duplicates first.
	for prefix, claimants := range ownerClaims {
		if len(claimants) > 1 {
			// Sort claimants for deterministic error ordering.
			sorted := make([]string, len(claimants))
			copy(sorted, claimants)
			sort.Strings(sorted)

			// Report an error for each claimant after the first.
			for i := 1; i < len(sorted); i++ {
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindDuplicateOwner,
					Component: sorted[i],
					File:      m.Components[sorted[i]].SourceFile,
					Message: fmt.Sprintf("namespace %q already owned by %q",
						prefix, sorted[0]),
					Related: map[string]any{"other": sorted[0]},
				})
			}
		}
	}

	// Collect unique prefixes sorted by length (longest first) for stable ordering.
	prefixes := make([]string, 0, len(ownerOf))
	for prefix := range ownerOf {
		prefixes = append(prefixes, prefix)
	}
	sort.Slice(prefixes, func(i, j int) bool {
		if len(prefixes[i]) != len(prefixes[j]) {
			return len(prefixes[i]) > len(prefixes[j])
		}
		return prefixes[i] < prefixes[j]
	})

	// Check each pair for nesting (one prefix being a prefix of another).
	for i, p1 := range prefixes {
		for _, p2 := range prefixes[i+1:] {
			if p1 == p2 {
				continue // handled above
			}
			if kindHasPrefix(p1, p2) {
				// p2 is a prefix of p1 (p1 is the longer nested prefix).
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindDuplicateOwner,
					Component: ownerOf[p1],
					File:      m.Components[ownerOf[p1]].SourceFile,
					Message: fmt.Sprintf("namespace %q nests inside %q owned by %q; ownership must be unique",
						p1, p2, ownerOf[p2]),
					Related: map[string]any{"other": ownerOf[p2], "parent_prefix": p2},
				})
			} else if kindHasPrefix(p2, p1) {
				// p1 is a prefix of p2 (p2 is the longer nested prefix).
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindDuplicateOwner,
					Component: ownerOf[p2],
					File:      m.Components[ownerOf[p2]].SourceFile,
					Message: fmt.Sprintf("namespace %q nests inside %q owned by %q; ownership must be unique",
						p2, p1, ownerOf[p1]),
					Related: map[string]any{"other": ownerOf[p1], "parent_prefix": p1},
				})
			}
		}
	}

	return findings
}

// validateSelfInput enforces that a component never takes its own output as
// an input. Inputs and outputs are the component's ports — what triggers it and
// what it appends — so a kind in both describes the component triggering
// itself: a loop through the boundary that exists to separate it from everyone
// else, drawing a self-edge with no runtime referent.
//
// The rule is narrow, and state_events is what makes it narrow. Folding one's
// own output into state is the normal case (record the outcome you just
// appended) and it is declared there, where it costs nothing. So an overlap
// between inputs and outputs is unambiguously a mistake rather than a
// legitimate pattern written in the wrong section.
//
// Matching is on the exact kind. Once one kind can travel several routes and a
// component may legitimately append on one and observe another, the comparison
// becomes (kind, pattern) rather than kind alone, and this is the degenerate
// single-route case.
func validateSelfInput(id string, comp *Component) []Finding {
	if len(comp.Outputs) == 0 || len(comp.Inputs) == 0 {
		return nil
	}

	outputPositions := make(map[string][]int, len(comp.Outputs))
	for i, slot := range comp.Outputs {
		outputPositions[slot.Kind] = append(outputPositions[slot.Kind], i)
	}

	var findings []Finding
	reported := make(map[string]bool)
	for i, slot := range comp.Inputs {
		outputs, ok := outputPositions[slot.Kind]
		if !ok || reported[slot.Kind] {
			continue
		}
		reported[slot.Kind] = true

		inputs := []int{i}
		for j := i + 1; j < len(comp.Inputs); j++ {
			if comp.Inputs[j].Kind == slot.Kind {
				inputs = append(inputs, j)
			}
		}

		findings = append(findings, Finding{
			Severity:  SeverityError,
			Kind:      KindSelfInputConflict,
			Component: id,
			File:      comp.SourceFile,
			Location:  slot.Kind,
			Message: fmt.Sprintf("component %q declares kind %q as both an input and an output, so it triggers itself; move it to state_events to fold its own outcome",
				id, slot.Kind),
			Related: map[string]any{"outputs": outputs, "inputs": inputs},
		})
	}

	return findings
}

// patternDecl records one site where a kind's subject pattern was declared.
type patternDecl struct {
	Component string
	Section   string // "inputs", "outputs" or "state_events"
	Pattern   string
}

// Site renders the declaration site as "component:section".
func (d patternDecl) Site() string { return d.Component + ":" + d.Section }

// patternDeclarations collects every non-empty pattern declaration per kind in
// a deterministic order: components sorted by id, and within a component
// inputs, then outputs, then state events, in declaration order. The first
// entry for a kind is the canonical pattern renderers use when declarations
// disagree.
func patternDeclarations(m *Model) map[string][]patternDecl {
	out := make(map[string][]patternDecl)
	for _, id := range sortedComponentIDs(m) {
		comp := m.Components[id]
		sections := []struct {
			name  string
			slots []Slot
		}{
			{"inputs", comp.Inputs},
			{"outputs", comp.Outputs},
			{"state_events", comp.StateEvents},
		}
		for _, section := range sections {
			for _, slot := range section.slots {
				if slot.Pattern == "" {
					continue
				}
				out[slot.Kind] = append(out[slot.Kind], patternDecl{id, section.name, slot.Pattern})
			}
		}
	}
	return out
}

// sortedComponentIDs returns component ids in a stable order.
func sortedComponentIDs(m *Model) []string {
	ids := make([]string, 0, len(m.Components))
	for id := range m.Components {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// PatternOf returns the canonical subject pattern of a kind — the first one
// declared for it in deterministic order — or "" when no declaration site
// carries a pattern.
func PatternOf(m *Model, kind string) string {
	decls := patternDeclarations(m)[kind]
	if len(decls) == 0 {
		return ""
	}
	return decls[0].Pattern
}

// validatePatterns enforces that a kind travels on exactly one subject pattern
// across the whole composed set.
//
// The pattern is where the kind lives on the wire; the kind is only its name.
// Two answers to "what subject is this on" means the subscribers of one
// pattern will never see what a producer appends on the other — a wiring bug
// that no amount of agreement about the payload can rescue, and one that is
// invisible at runtime until the event silently fails to arrive. A component
// that declares no pattern for a kind is not disagreeing and is skipped.
//
// Where a kind genuinely travels several routes, that is a model this format
// does not yet describe, and the fix is to name the routes as separate kinds
// rather than to let one kind mean two addresses.
func validatePatterns(m *Model) []Finding {
	decls := patternDeclarations(m)

	kinds := make([]string, 0, len(decls))
	for kind := range decls {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	var findings []Finding
	for _, kind := range kinds {
		sitesByPattern := make(map[string][]string)
		var order []string // patterns in first-declared order
		for _, d := range decls[kind] {
			if _, seen := sitesByPattern[d.Pattern]; !seen {
				order = append(order, d.Pattern)
			}
			sitesByPattern[d.Pattern] = append(sitesByPattern[d.Pattern], d.Site())
		}
		if len(order) < 2 {
			continue
		}

		parts := make([]string, 0, len(order))
		related := make(map[string]any, len(order))
		for _, pattern := range order {
			parts = append(parts, fmt.Sprintf("%s (%s)", pattern, strings.Join(sitesByPattern[pattern], ", ")))
			related[pattern] = sitesByPattern[pattern]
		}

		findings = append(findings, Finding{
			Severity:  SeverityError,
			Kind:      KindPatternConflict,
			Component: decls[kind][0].Component,
			File:      m.Components[decls[kind][0].Component].SourceFile,
			Location:  kind,
			Message: fmt.Sprintf("kind %q is declared on conflicting subject patterns: %s; the pattern is the kind's address, so subscribers of one will never see what the other appends",
				kind, strings.Join(parts, " vs ")),
			Related: map[string]any{"patterns": related},
		})
	}

	return findings
}

// validateClosure checks that a component's read-set is fed by somebody, and
// that everything it appends is observed by somebody.
//
// Both directions are warnings, not errors: the composed set seen statically
// may be a subset of the running one — a component whose declaration is not in
// this repository still emits, and still observes.
func validateClosure(m *Model, inputsOf, foldersOf, producersOf map[string][]string) []Finding {
	var findings []Finding

	// Starved read-set: an input or a state event nobody appends. The two are
	// separate finding kinds because the fixes differ — a starved input means
	// the trigger never fires, a starved state event means the projection is
	// tracking something that never happens.
	starved := []struct {
		slots   func(*Component) []Slot
		kind    FindingKind
		section string
	}{
		{func(c *Component) []Slot { return c.Inputs }, KindStarvedInput, "inputs"},
		{func(c *Component) []Slot { return c.StateEvents }, KindStarvedStateEvent, "state_events"},
	}
	for id, comp := range m.Components {
		for _, section := range starved {
			for _, slot := range section.slots(comp) {
				if len(producersOf[slot.Kind]) > 0 {
					continue
				}
				findings = append(findings, Finding{
					Severity:  SeverityWarning,
					Kind:      section.kind,
					Component: id,
					File:      comp.SourceFile,
					Location:  slot.Kind,
					Message: fmt.Sprintf("declares %q in %s but no component outputs it",
						slot.Kind, section.section),
				})
			}
		}
	}

	// Orphan outputs: appended by this component and observed by nobody —
	// neither an input nor a state event anywhere. A component folding its own
	// output counts as an observer, which is why this reads foldersOf rather
	// than excluding the producer.
	for id, comp := range m.Components {
		for _, slot := range comp.Outputs {
			if len(inputsOf[slot.Kind]) > 0 || len(foldersOf[slot.Kind]) > 0 {
				continue
			}
			findings = append(findings, Finding{
				Severity:  SeverityWarning,
				Kind:      KindOrphanEvent,
				Component: id,
				File:      comp.SourceFile,
				Location:  slot.Kind,
				Message: fmt.Sprintf("outputs %q but no component takes it as an input or folds it into state",
					slot.Kind),
			})
		}
	}

	return findings
}

// validateExclusiveDelivery enforces single-handler cardinality, but ONLY for
// kinds that explicitly opted in via `delivery: exclusive`.
//
// This is deliberately not the default. In event-sourced choreography a
// durable event is appended once and folded independently by any number of
// controllers, projections and read models; requiring it to resolve to exactly
// one handler is an RPC assumption that does not hold. Where a project really
// does have a command with one owner, it says so, and then — and only then —
// zero or many consumers become errors.
//
// Cardinality is counted over inputs alone. A state event is an observation
// that drives no reaction, so any number of components may fold an exclusive
// kind without competing for the right to handle it.
//
// A kind is exclusive if any declaration of it says so; exclusivity is a
// property of the kind, not of one side of it.
func validateExclusiveDelivery(m *Model, inputsOf map[string][]string, exclusiveKinds map[string]struct{}) []Finding {
	var findings []Finding

	for id, comp := range m.Components {
		for _, slot := range comp.Outputs {
			if _, ok := exclusiveKinds[slot.Kind]; !ok {
				continue
			}
			consumers := inputsOf[slot.Kind]
			switch len(consumers) {
			case 0:
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindExclusiveUnhandled,
					Component: id,
					File:      comp.SourceFile,
					Location:  slot.Kind,
					Message: fmt.Sprintf("outputs %q declared delivery: exclusive but no component takes it as an input",
						slot.Kind),
				})
			case 1:
				// ok
			default:
				sorted := append([]string(nil), consumers...)
				sort.Strings(sorted)
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindExclusiveConflict,
					Component: id,
					File:      comp.SourceFile,
					Location:  slot.Kind,
					Message: fmt.Sprintf("outputs %q declared delivery: exclusive but %d components take it as an input: %v",
						slot.Kind, len(sorted), sorted),
					Related: map[string]any{"consumers": sorted},
				})
			}
		}
	}

	return findings
}

// validateRefs checks $ref resolution within one component. Cross-component
// refs are checked separately for cycles.
func validateRefs(id string, comp *Component, m *Model) []Finding {
	var findings []Finding

	// Collect all refs and check resolution.
	checkRef := func(location string, node SchemaNode) {
		ref := node.Ref()
		if ref == "" {
			return
		}
		if !resolveRef(ref, comp, m) {
			findings = append(findings, Finding{
				Severity:  SeverityError,
				Kind:      KindUnresolvedRef,
				Component: id,
				File:      comp.SourceFile,
				Location:  location,
				Message:   fmt.Sprintf("$ref %q does not resolve", ref),
			})
		}
	}

	walkSchema := func(location string, node SchemaNode) {
		walkSchemaNode(node, func(n SchemaNode) {
			checkRef(location, n)
		})
	}

	for _, slot := range comp.Inputs {
		walkSchema("inputs:"+slot.Kind, slot.Schema)
	}
	for _, slot := range comp.Outputs {
		walkSchema("outputs:"+slot.Kind, slot.Schema)
	}
	for _, slot := range comp.StateEvents {
		walkSchema("state_events:"+slot.Kind, slot.Schema)
	}
	walkSchema("state", comp.State)
	for name, schema := range comp.Types {
		walkSchema("types:"+name, schema)
	}

	return findings
}

// walkSchemaNode traverses a schema node tree, calling fn for each node.
func walkSchemaNode(node SchemaNode, fn func(SchemaNode)) {
	if node.IsZero() {
		return
	}
	fn(node)

	m := node.AsMap()
	if m == nil {
		return
	}

	// Recurse into known schema keywords that contain schemas.
	for _, key := range []string{"properties", "items", "additionalProperties"} {
		if v, ok := m[key]; ok {
			if props, ok := v.(map[string]any); ok {
				for _, prop := range props {
					walkSchemaNode(SchemaNode{Raw: prop}, fn)
				}
			} else {
				walkSchemaNode(SchemaNode{Raw: v}, fn)
			}
		}
	}

	// oneOf, anyOf, allOf
	for _, key := range []string{"oneOf", "anyOf", "allOf"} {
		if v, ok := m[key]; ok {
			if arr, ok := v.([]any); ok {
				for _, elem := range arr {
					walkSchemaNode(SchemaNode{Raw: elem}, fn)
				}
			}
		}
	}
}

// typeRefMarker is the JSON Pointer prefix addressing a component's reusable
// type definitions — the event-model analogue of JSON Schema's "#/$defs/".
const typeRefMarker = "#/types/"

// resolveRef checks if a $ref resolves. Supports:
//   - "#/types/X" — local type reference
//   - "other-component#/types/X" — cross-component reference
func resolveRef(ref string, comp *Component, m *Model) bool {
	if name, ok := strings.CutPrefix(ref, typeRefMarker); ok {
		_, exists := comp.Types[name]
		return exists
	}

	// Cross-component ref: "component#/types/X"
	if idx := strings.Index(ref, typeRefMarker); idx > 0 {
		targetID := ref[:idx]
		name := ref[idx+len(typeRefMarker):]
		target, ok := m.Components[targetID]
		if !ok {
			return false
		}
		_, ok = target.Types[name]
		return ok
	}

	// Unknown ref format.
	return false
}

// validateRefCycles detects cycles in cross-component $ref dependencies.
func validateRefCycles(m *Model) []Finding {
	// Build cross-component dependency graph: component -> components it refs.
	deps := make(map[string]map[string]struct{})
	for id, comp := range m.Components {
		deps[id] = make(map[string]struct{})
		collectCrossRefs(comp, func(targetID string) {
			if targetID != id {
				deps[id][targetID] = struct{}{}
			}
		})
	}

	// Detect cycles using DFS.
	var findings []Finding
	visited := make(map[string]int) // 0=unvisited, 1=in-stack, 2=done
	var path []string

	var dfs func(id string) bool
	dfs = func(id string) bool {
		if visited[id] == 2 {
			return false
		}
		if visited[id] == 1 {
			// Found a cycle.
			cycleStart := 0
			for i, p := range path {
				if p == id {
					cycleStart = i
					break
				}
			}
			cycle := append(path[cycleStart:], id)
			findings = append(findings, Finding{
				Severity:  SeverityError,
				Kind:      KindRefCycle,
				Component: id,
				File:      m.Components[id].SourceFile,
				Message:   fmt.Sprintf("cross-component $ref cycle: %s", strings.Join(cycle, " -> ")),
				Related:   map[string]any{"cycle": cycle},
			})
			return true
		}

		visited[id] = 1
		path = append(path, id)

		for target := range deps[id] {
			if dfs(target) {
				return true
			}
		}

		path = path[:len(path)-1]
		visited[id] = 2
		return false
	}

	// Start DFS from each component to find all cycles.
	ids := make([]string, 0, len(m.Components))
	for id := range m.Components {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		if visited[id] == 0 {
			dfs(id)
		}
	}

	return findings
}

// collectCrossRefs finds all cross-component $refs in a component.
func collectCrossRefs(comp *Component, fn func(targetID string)) {
	visit := func(node SchemaNode) {
		ref := node.Ref()
		if ref == "" {
			return
		}
		if idx := strings.Index(ref, typeRefMarker); idx > 0 {
			fn(ref[:idx])
		}
	}

	walkAll := func(node SchemaNode) {
		walkSchemaNode(node, visit)
	}

	for _, slot := range comp.Inputs {
		walkAll(slot.Schema)
	}
	for _, slot := range comp.Outputs {
		walkAll(slot.Schema)
	}
	for _, slot := range comp.StateEvents {
		walkAll(slot.Schema)
	}
	walkAll(comp.State)
	for _, schema := range comp.Types {
		walkAll(schema)
	}
}

// validateReadSet checks the coherence of the component's derived fold: the
// {slot} syntax of every subject pattern in its read-set, and that all of them
// extract the same ordered partition key.
//
// A component holds exactly one state, addressed by its partition key. If two
// patterns in its read-set disagree on the key — different slot names, a
// different order, or a different count — there is no single answer to "which
// state does this event belong to", and the fold cannot be wired. Patterns
// that carry no slots at all are exempt: a globally addressed event legitimately
// feeds a partitioned state.
func validateReadSet(id string, comp *Component) []Finding {
	var findings []Finding

	subjects := comp.Subjects()
	malformed := make(map[string]bool)
	for _, subject := range subjects {
		if err := ValidateSlotSyntax(subject); err != nil {
			malformed[subject] = true
			findings = append(findings, Finding{
				Severity:  SeverityError,
				Kind:      KindMalformedSlot,
				Component: id,
				File:      comp.SourceFile,
				Location:  subject,
				Message:   fmt.Sprintf("subject %q: %v", subject, err),
			})
		}
	}

	want := comp.PartitionKey
	if len(want) == 0 {
		return findings
	}
	for _, subject := range subjects {
		if malformed[subject] {
			continue
		}
		got := SlotTokens(subject)
		if len(got) == 0 || slotKeysEqual(want, got) {
			continue
		}
		findings = append(findings, Finding{
			Severity:  SeverityError,
			Kind:      KindPartitionMismatch,
			Component: id,
			File:      comp.SourceFile,
			Location:  subject,
			Message: fmt.Sprintf("subject %q extracts partition key %v but the component's read-set is keyed by %v; one component holds one state, so every partitioned subject it reads must address it identically",
				subject, got, want),
			Related: map[string]any{"want": want, "got": got},
		})
	}

	return findings
}

// validateState flags a declared state schema that says nothing. The schema is
// optional — a component may fold nothing worth describing — so this catches
// the placeholder that remains after someone writes `state: {type: object}` and
// moves on, not the absence of the field.
func validateState(id string, comp *Component) []Finding {
	if !isUnderspecifiedState(comp.State) {
		return nil
	}
	return []Finding{{
		Severity:  SeverityWarning,
		Kind:      KindUnderspecifiedState,
		Component: id,
		File:      comp.SourceFile,
		Location:  "state",
		Message:   "state is an object with no properties and no $ref; declare the projection shape, reference a type, or drop the field",
	}}
}

// isUnderspecifiedState reports whether a fold state is an object schema with
// nothing in it. Non-object schemas (arrays, scalars, enums) are left alone —
// they carry their shape in other keywords.
func isUnderspecifiedState(state SchemaNode) bool {
	m := state.AsMap()
	if m == nil {
		return false
	}
	if t, ok := m["type"].(string); !ok || t != "object" {
		return false
	}
	for _, key := range []string{"properties", "$ref", "oneOf", "anyOf", "allOf", "additionalProperties", "patternProperties"} {
		if _, ok := m[key]; ok {
			return false
		}
	}
	return true
}

// slotKeysEqual compares two partition keys element-wise; order is significant.
func slotKeysEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sortFindings(fs []Finding) {
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].Severity != fs[j].Severity {
			// Errors before warnings.
			return fs[i].Severity < fs[j].Severity
		}
		if fs[i].Component != fs[j].Component {
			return fs[i].Component < fs[j].Component
		}
		if fs[i].Kind != fs[j].Kind {
			return fs[i].Kind < fs[j].Kind
		}
		return fs[i].Location < fs[j].Location
	})
}
