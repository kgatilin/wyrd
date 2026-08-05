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
// may be observed independently by any number of components and folds. There
// is therefore NO rule that an emitted event resolves to a single handler, and
// no rule that ownership of a namespace restricts who may emit into it or
// observe it. Both were RPC assumptions and have been removed.
//
// Rules implemented:
//   - Single-owner namespaces: one component defines a namespace's schemas
//   - Single role per kind: role is global, not per declaration site
//   - Closure: starved receives, starved fold consumes, orphan events
//   - Fold coherence: {slot} syntax, one shared partition key, declared state
//   - Exclusive delivery (opt-in): a kind declared `delivery: exclusive`
//     must have exactly one receiver
//   - $ref integrity: every $ref resolves, no cross-component cycles
//
// NOT implemented (see package doc):
//   - Schema compatibility between an emitted payload and a receiver's schema
func Validate(m *Model) []Finding {
	var findings []Finding

	// Build indexes for efficient lookup.
	ownerOf := make(map[string]string)           // namespace -> component id
	receiversOf := make(map[string][]string)     // kind -> component ids that receive it
	emittersOf := make(map[string][]string)      // kind -> component ids that emit it
	allEmittedKinds := make(map[string]struct{}) // every kind appended to the log
	exclusiveKinds := make(map[string]struct{})  // kinds that opted into single-handler delivery

	// Track ownership claims (map prefix -> list of component IDs claiming it).
	ownerClaims := make(map[string][]string)

	for id, comp := range m.Components {
		// Record ownership prefixes for later validation.
		if comp.Owns != "" {
			ownerClaims[comp.Owns] = append(ownerClaims[comp.Owns], id)
			ownerOf[comp.Owns] = id
		}

		// Index receives.
		for _, slot := range comp.Receives {
			receiversOf[slot.Kind] = append(receiversOf[slot.Kind], id)
			if slot.Delivery.IsExclusive() {
				exclusiveKinds[slot.Kind] = struct{}{}
			}
		}

		// Index emits.
		for _, slot := range comp.Emits {
			emittersOf[slot.Kind] = append(emittersOf[slot.Kind], id)
			allEmittedKinds[slot.Kind] = struct{}{}
			if slot.Delivery.IsExclusive() {
				exclusiveKinds[slot.Kind] = struct{}{}
			}
		}
	}

	// Check for overlapping namespace ownership. Exact duplicates and nesting
	// without explicit containment are both errors — sub-namespace ownership
	// must be unique among the declared set.
	findings = append(findings, validateOwnershipOverlaps(m, ownerOf, ownerClaims)...)

	// Per-component validation.
	for id, comp := range m.Components {
		findings = append(findings, validateRefs(id, comp, m)...)
		findings = append(findings, validateFolds(id, comp)...)
	}

	// Cross-component validation.
	findings = append(findings, validateKindRoles(m)...)
	findings = append(findings, validateClosure(m, receiversOf, emittersOf, allEmittedKinds)...)
	findings = append(findings, validateExclusiveDelivery(m, receiversOf, exclusiveKinds)...)
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

// roleDecl records one site where a kind's role was declared.
type roleDecl struct {
	Component string
	Section   string // "receives" or "emits"
	Role      Role
}

// Site renders the declaration site as "component:section".
func (d roleDecl) Site() string { return d.Component + ":" + d.Section }

// roleDeclarations collects every role declaration per kind in a deterministic
// order: components sorted by id, and within a component receives before emits
// in declaration order. The first entry for a kind is the canonical role that
// renderers use when declarations disagree.
func roleDeclarations(m *Model) map[string][]roleDecl {
	out := make(map[string][]roleDecl)
	for _, id := range sortedComponentIDs(m) {
		comp := m.Components[id]
		for _, slot := range comp.Receives {
			out[slot.Kind] = append(out[slot.Kind], roleDecl{id, "receives", slot.Role})
		}
		for _, slot := range comp.Emits {
			out[slot.Kind] = append(out[slot.Kind], roleDecl{id, "emits", slot.Role})
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

// validateKindRoles enforces that a kind carries exactly one role across the
// whole composed set.
//
// Role is a property of the kind, not of the declaration site: it says what the
// event *is*, and one event cannot both express an intent and record an
// outcome. Producers and observers therefore cannot disagree about it, and a
// payload variant (a different schema branch, a legacy shape, an extra field)
// never changes it. Where a name would need both readings, that is two kinds —
// `x.thing.do` and `x.thing.done` — not one kind read two ways. Splitting them
// is the fix; there is no reading under which the conflict is benign, so it is
// an error rather than a warning.
func validateKindRoles(m *Model) []Finding {
	decls := roleDeclarations(m)

	kinds := make([]string, 0, len(decls))
	for kind := range decls {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	var findings []Finding
	for _, kind := range kinds {
		sitesByRole := make(map[Role][]string)
		var order []Role // roles in first-declared order
		for _, d := range decls[kind] {
			if _, seen := sitesByRole[d.Role]; !seen {
				order = append(order, d.Role)
			}
			sitesByRole[d.Role] = append(sitesByRole[d.Role], d.Site())
		}
		if len(order) < 2 {
			continue
		}

		parts := make([]string, 0, len(order))
		related := make(map[string]any, len(order))
		for _, role := range order {
			parts = append(parts, fmt.Sprintf("%s (%s)", role, strings.Join(sitesByRole[role], ", ")))
			related[string(role)] = sitesByRole[role]
		}

		findings = append(findings, Finding{
			Severity:  SeverityError,
			Kind:      KindRoleConflict,
			Component: decls[kind][0].Component,
			File:      m.Components[decls[kind][0].Component].SourceFile,
			Location:  kind,
			Message: fmt.Sprintf("kind %q is declared with conflicting roles: %s; role is a property of the kind, so split intent and outcome into separate kinds",
				kind, strings.Join(parts, " vs ")),
			Related: map[string]any{"roles": related},
		})
	}

	return findings
}

// validateClosure checks that receives and folds are not starved, and that
// emitted events are observed by somebody.
func validateClosure(m *Model, receiversOf, emittersOf map[string][]string, allEmittedKinds map[string]struct{}) []Finding {
	var findings []Finding

	// Starved receives: kind has no producer.
	for id, comp := range m.Components {
		for _, slot := range comp.Receives {
			if len(emittersOf[slot.Kind]) == 0 {
				findings = append(findings, Finding{
					Severity:  SeverityWarning,
					Kind:      KindStarvedReceive,
					Component: id,
					File:      comp.SourceFile,
					Location:  slot.Kind,
					Message:   fmt.Sprintf("receives %q but no component emits it", slot.Kind),
				})
			}
		}
	}

	// Starved folds: consumes entry matches no emitted kind.
	// Report per consumes entry, not per fold.
	for id, comp := range m.Components {
		for _, fold := range comp.Folds {
			for _, consumesEntry := range fold.Consumes {
				matched := false
				for kind := range allEmittedKinds {
					if MatchPattern(consumesEntry, kind) {
						matched = true
						break
					}
				}
				if !matched {
					findings = append(findings, Finding{
						Severity:  SeverityWarning,
						Kind:      KindStarvedFold,
						Component: id,
						File:      comp.SourceFile,
						Location:  fold.Name,
						Message: fmt.Sprintf("fold %q consumes %q but no emitted kind matches",
							fold.Name, consumesEntry),
					})
				}
			}
		}
	}

	// Orphan events: an emitted event that nobody observes — neither a
	// receives slot nor a fold. This applies to both roles: an action with no
	// observer is as dead as a fact with no observer, and neither is an error,
	// because the composed set seen statically may be a subset of the running
	// one.
	for id, comp := range m.Components {
		for _, slot := range comp.Emits {
			if len(receiversOf[slot.Kind]) > 0 {
				continue
			}
			if countFoldConsumers(m, slot.Kind) > 0 {
				continue
			}
			findings = append(findings, Finding{
				Severity:  SeverityWarning,
				Kind:      KindOrphanEvent,
				Component: id,
				File:      comp.SourceFile,
				Location:  slot.Kind,
				Message: fmt.Sprintf("emits %s %q but no component receives or folds it",
					slot.Role, slot.Kind),
			})
		}
	}

	return findings
}

// countFoldConsumers counts the folds across the model whose consumes globs
// match kind. Each fold counts once regardless of how many entries match.
func countFoldConsumers(m *Model, kind string) int {
	n := 0
	for _, comp := range m.Components {
		for _, fold := range comp.Folds {
			for _, consumesEntry := range fold.Consumes {
				if MatchPattern(consumesEntry, kind) {
					n++
					break
				}
			}
		}
	}
	return n
}

// validateExclusiveDelivery enforces single-handler cardinality, but ONLY for
// kinds that explicitly opted in via `delivery: exclusive`.
//
// This is deliberately not the default. In event-sourced choreography a
// durable event is appended once and folded independently by any number of
// controllers, projections and read models; requiring it to resolve to exactly
// one handler is an RPC assumption that does not hold. Where a project really
// does have a command with one owner, it says so, and then — and only then —
// zero or many receivers become errors.
//
// A kind is exclusive if any declaration of it (emit or receive) says so;
// exclusivity is a property of the kind, not of one side of it.
func validateExclusiveDelivery(m *Model, receiversOf map[string][]string, exclusiveKinds map[string]struct{}) []Finding {
	var findings []Finding

	for id, comp := range m.Components {
		for _, slot := range comp.Emits {
			if _, ok := exclusiveKinds[slot.Kind]; !ok {
				continue
			}
			receivers := receiversOf[slot.Kind]
			switch len(receivers) {
			case 0:
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindExclusiveUnhandled,
					Component: id,
					File:      comp.SourceFile,
					Location:  slot.Kind,
					Message: fmt.Sprintf("emits %q declared delivery: exclusive but no component receives it",
						slot.Kind),
				})
			case 1:
				// ok
			default:
				sorted := append([]string(nil), receivers...)
				sort.Strings(sorted)
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindExclusiveConflict,
					Component: id,
					File:      comp.SourceFile,
					Location:  slot.Kind,
					Message: fmt.Sprintf("emits %q declared delivery: exclusive but %d components receive it: %v",
						slot.Kind, len(sorted), sorted),
					Related: map[string]any{"receivers": sorted},
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

	for _, slot := range comp.Receives {
		walkSchema("receives:"+slot.Kind, slot.Schema)
	}
	for _, slot := range comp.Emits {
		walkSchema("emits:"+slot.Kind, slot.Schema)
	}
	for _, fold := range comp.Folds {
		walkSchema("folds:"+fold.Name, fold.State)
	}
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

	for _, slot := range comp.Receives {
		walkAll(slot.Schema)
	}
	for _, slot := range comp.Emits {
		walkAll(slot.Schema)
	}
	for _, fold := range comp.Folds {
		walkAll(fold.State)
	}
	for _, schema := range comp.Types {
		walkAll(schema)
	}
}

// validateFolds checks the internal coherence of each fold: {slot} syntax in
// every subject, one shared partition key across all of them, and a state
// schema that actually says something.
func validateFolds(id string, comp *Component) []Finding {
	var findings []Finding
	for _, fold := range comp.Folds {
		findings = append(findings, validateFoldSubjects(id, comp, fold)...)
		findings = append(findings, validateFoldState(id, comp, fold)...)
	}
	return findings
}

// validateFoldSubjects checks {slot} syntax per subject and then that every
// subject of the fold extracts the same ordered partition key.
//
// A fold instance holds exactly one state, addressed by its partition key. If
// two subjects of the same fold disagree on the key — different slot names, a
// different order, or a different count — there is no single answer to "which
// state does this event belong to", and the fold cannot be wired.
func validateFoldSubjects(id string, comp *Component, fold Fold) []Finding {
	var findings []Finding

	malformed := make(map[int]bool)
	for i, subject := range fold.Subjects {
		if subject == "" {
			continue
		}
		if err := ValidateSlotSyntax(subject); err != nil {
			malformed[i] = true
			findings = append(findings, Finding{
				Severity:  SeverityError,
				Kind:      KindMalformedSlot,
				Component: id,
				File:      comp.SourceFile,
				Location:  fold.Name,
				Message:   fmt.Sprintf("fold %q subject %q: %v", fold.Name, subject, err),
			})
		}
	}

	// Partition coherence is only meaningful once the syntax parses, and only
	// when there is more than one subject to compare.
	if len(fold.Subjects) < 2 {
		return findings
	}
	want := fold.PartitionKey
	for i := 1; i < len(fold.Subjects); i++ {
		if malformed[i] || malformed[0] {
			continue
		}
		got := SlotTokens(fold.Subjects[i])
		if slotKeysEqual(want, got) {
			continue
		}
		findings = append(findings, Finding{
			Severity:  SeverityError,
			Kind:      KindPartitionMismatch,
			Component: id,
			File:      comp.SourceFile,
			Location:  fold.Name,
			Message: fmt.Sprintf("fold %q subject %q extracts partition key %v but %q extracts %v; all subjects of one fold must extract the same ordered key",
				fold.Name, fold.Subjects[i], got, fold.Subjects[0], want),
			Related: map[string]any{"want": want, "got": got},
		})
	}

	return findings
}

// validateFoldState flags a state schema that declares no shape. The reader
// already rejects a missing state; this catches the placeholder that remains
// after someone writes `state: {type: object}` and moves on.
func validateFoldState(id string, comp *Component, fold Fold) []Finding {
	if !isUnderspecifiedState(fold.State) {
		return nil
	}
	return []Finding{{
		Severity:  SeverityWarning,
		Kind:      KindUnderspecifiedState,
		Component: id,
		File:      comp.SourceFile,
		Location:  fold.Name,
		Message: fmt.Sprintf("fold %q declares an object state with no properties and no $ref; declare the projection shape or reference a type",
			fold.Name),
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
