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
// Rules implemented:
//   - Role x ownership matrix (all four cells)
//   - Single-owner namespaces
//   - Closure: starved receives, starved fold patterns, orphan facts
//   - Call resolution: emitted action must resolve to exactly one receiver
//   - Vocab/$ref integrity: every $ref resolves, no cross-component cycles
//
// NOT implemented (see package doc):
//   - Schema compatibility between call-out payload and target's receives schema
func Validate(m *Model) []Finding {
	var findings []Finding

	// Build indexes for efficient lookup.
	ownerOf := make(map[string]string)             // namespace -> component id
	receiversOf := make(map[string][]string)       // kind -> component ids that receive it
	emittersOf := make(map[string][]string)        // kind -> component ids that emit it
	allEmittedKinds := make(map[string]struct{})   // all kinds emitted as facts
	allEmittedActions := make(map[string]struct{}) // all kinds emitted as actions

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
		}

		// Index emits and check ownership.
		for _, slot := range comp.Emits {
			emittersOf[slot.Kind] = append(emittersOf[slot.Kind], id)
			switch slot.Role {
			case RoleFact:
				allEmittedKinds[slot.Kind] = struct{}{}
			case RoleAction:
				allEmittedActions[slot.Kind] = struct{}{}
			}
		}
	}

	// Check for overlapping namespace ownership. Exact duplicates and nesting
	// without explicit containment are both errors — sub-namespace ownership
	// must be unique among the declared set.
	findings = append(findings, validateOwnershipOverlaps(m, ownerOf, ownerClaims)...)

	// Per-component validation.
	for id, comp := range m.Components {
		findings = append(findings, validateOwnership(id, comp, ownerOf)...)
		findings = append(findings, validateRefs(id, comp, m)...)
	}

	// Cross-component validation.
	findings = append(findings, validateClosure(m, receiversOf, emittersOf, allEmittedKinds)...)
	findings = append(findings, validateCallResolution(m, receiversOf)...)
	findings = append(findings, validateRefCycles(m)...)

	sortFindings(findings)
	return findings
}

// validateOwnershipOverlaps detects conflicting namespace ownership claims.
// Exact duplicates are always errors. Nested prefixes (e.g., "billing" and
// "billing.invoice") are errors because longest-prefix-wins resolution
// requires unique ownership — allowing nesting would make ownership ambiguous
// at declaration time.
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

// resolveOwner finds the component that owns a kind by longest prefix match.
// Returns empty string if no component owns the kind's namespace.
func resolveOwner(kind string, ownerOf map[string]string) string {
	var bestPrefix string
	var bestOwner string
	for prefix, owner := range ownerOf {
		if kindHasPrefix(kind, prefix) {
			if len(prefix) > len(bestPrefix) {
				bestPrefix = prefix
				bestOwner = owner
			}
		}
	}
	return bestOwner
}

// validateOwnership checks the role x ownership matrix for one component.
// The ownerOf map is used for longest-prefix resolution of kind ownership.
func validateOwnership(id string, comp *Component, ownerOf map[string]string) []Finding {
	var findings []Finding

	// emit, role=fact, kind not in owns => error (forging another namespace's fact)
	for _, slot := range comp.Emits {
		if slot.Role == RoleFact {
			if comp.Owns == "" {
				// A component without owns cannot emit facts at all.
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindOwnershipViolation,
					Component: id,
					File:      comp.SourceFile,
					Location:  slot.Kind,
					Message:   fmt.Sprintf("emitting fact %q but component has no owns", slot.Kind),
				})
			} else if !kindHasPrefix(slot.Kind, comp.Owns) {
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindOwnershipViolation,
					Component: id,
					File:      comp.SourceFile,
					Location:  slot.Kind,
					Message: fmt.Sprintf("emitting fact %q outside owned namespace %q",
						slot.Kind, comp.Owns),
				})
			}
		}
		// emit, role=action is always ok (self-scheduling or call-out).
	}

	// receive, role=action, kind not in owns => error (accepting commands in another namespace)
	for _, slot := range comp.Receives {
		if slot.Role == RoleAction {
			if comp.Owns == "" {
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindOwnershipViolation,
					Component: id,
					File:      comp.SourceFile,
					Location:  slot.Kind,
					Message:   fmt.Sprintf("receiving action %q but component has no owns", slot.Kind),
				})
			} else if !kindHasPrefix(slot.Kind, comp.Owns) {
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindOwnershipViolation,
					Component: id,
					File:      comp.SourceFile,
					Location:  slot.Kind,
					Message: fmt.Sprintf("receiving action %q outside owned namespace %q",
						slot.Kind, comp.Owns),
				})
			}
		}
		// receive, role=fact is always ok (self-observation or subscription).
	}

	return findings
}

// validateClosure checks that receives and folds are not starved, and facts
// are not orphaned.
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

	// Starved folds: pattern matches no emitted kind.
	for id, comp := range m.Components {
		for _, fold := range comp.Folds {
			matched := false
			for kind := range allEmittedKinds {
				if MatchPattern(fold.Pattern, kind) {
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
					Message: fmt.Sprintf("fold %q pattern %q matches no emitted kind",
						fold.Name, fold.Pattern),
				})
			}
		}
	}

	// Orphan facts: emitted fact has no consumer.
	for id, comp := range m.Components {
		for _, slot := range comp.Emits {
			if slot.Role != RoleFact {
				continue
			}
			if len(receiversOf[slot.Kind]) == 0 {
				// Also check folds.
				consumed := false
				for _, otherComp := range m.Components {
					for _, fold := range otherComp.Folds {
						if MatchPattern(fold.Pattern, slot.Kind) {
							consumed = true
							break
						}
					}
					if consumed {
						break
					}
				}
				if !consumed {
					findings = append(findings, Finding{
						Severity:  SeverityWarning,
						Kind:      KindOrphanFact,
						Component: id,
						File:      comp.SourceFile,
						Location:  slot.Kind,
						Message:   fmt.Sprintf("emits fact %q but no component receives it", slot.Kind),
					})
				}
			}
		}
	}

	return findings
}

// validateCallResolution checks that emitted actions resolve to exactly one receiver.
func validateCallResolution(m *Model, receiversOf map[string][]string) []Finding {
	var findings []Finding

	for id, comp := range m.Components {
		for _, slot := range comp.Emits {
			if slot.Role != RoleAction {
				continue
			}
			// Self-scheduling (action in owns) is ok but still needs a receiver.
			receivers := receiversOf[slot.Kind]
			switch len(receivers) {
			case 0:
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindUnresolvedCall,
					Component: id,
					File:      comp.SourceFile,
					Location:  slot.Kind,
					Message:   fmt.Sprintf("emits action %q but no component receives it", slot.Kind),
				})
			case 1:
				// ok
			default:
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindAmbiguousCall,
					Component: id,
					File:      comp.SourceFile,
					Location:  slot.Kind,
					Message: fmt.Sprintf("emits action %q but multiple components receive it: %v",
						slot.Kind, receivers),
					Related: map[string]any{"receivers": receivers},
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
	for name, schema := range comp.Vocab {
		walkSchema("vocab:"+name, schema)
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

// resolveRef checks if a $ref resolves. Supports:
//   - "#/vocab/X" — local vocab reference
//   - "other-component#/vocab/X" — cross-component reference
func resolveRef(ref string, comp *Component, m *Model) bool {
	if name, ok := strings.CutPrefix(ref, "#/vocab/"); ok {
		_, exists := comp.Vocab[name]
		return exists
	}

	// Cross-component ref: "component#/vocab/X"
	if idx := strings.Index(ref, "#/vocab/"); idx > 0 {
		targetID := ref[:idx]
		name := ref[idx+len("#/vocab/"):]
		target, ok := m.Components[targetID]
		if !ok {
			return false
		}
		_, ok = target.Vocab[name]
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
		if idx := strings.Index(ref, "#/vocab/"); idx > 0 {
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
	for _, schema := range comp.Vocab {
		walkAll(schema)
	}
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
