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

	for id, comp := range m.Components {
		// Check for duplicate namespace ownership.
		if comp.Owns != "" {
			if existing, ok := ownerOf[comp.Owns]; ok {
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindDuplicateOwner,
					Component: id,
					File:      comp.SourceFile,
					Message: fmt.Sprintf("namespace %q already owned by %q",
						comp.Owns, existing),
					Related: map[string]any{"other": existing},
				})
			} else {
				ownerOf[comp.Owns] = id
			}
		}

		// Index receives.
		for _, slot := range comp.Receives {
			receiversOf[slot.Kind] = append(receiversOf[slot.Kind], id)
		}

		// Index emits and check ownership.
		for _, slot := range comp.Emits {
			emittersOf[slot.Kind] = append(emittersOf[slot.Kind], id)
			if slot.Role == RoleFact {
				allEmittedKinds[slot.Kind] = struct{}{}
			} else if slot.Role == RoleAction {
				allEmittedActions[slot.Kind] = struct{}{}
			}
		}
	}

	// Per-component validation.
	for id, comp := range m.Components {
		findings = append(findings, validateOwnership(id, comp)...)
		findings = append(findings, validateRefs(id, comp, m)...)
	}

	// Cross-component validation.
	findings = append(findings, validateClosure(m, receiversOf, emittersOf, allEmittedKinds)...)
	findings = append(findings, validateCallResolution(m, receiversOf)...)
	findings = append(findings, validateRefCycles(m)...)

	sortFindings(findings)
	return findings
}

// validateOwnership checks the role x ownership matrix for one component.
func validateOwnership(id string, comp *Component) []Finding {
	var findings []Finding

	// emit, role=fact, kind not in owns => error (forging another namespace's fact)
	for _, slot := range comp.Emits {
		if slot.Role == RoleFact {
			ns := namespace(slot.Kind)
			if comp.Owns != "" && ns != comp.Owns {
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindOwnershipViolation,
					Component: id,
					File:      comp.SourceFile,
					Location:  slot.Kind,
					Message: fmt.Sprintf("emitting fact %q outside owned namespace %q",
						slot.Kind, comp.Owns),
				})
			} else if comp.Owns == "" {
				// A component without owns cannot emit facts at all.
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindOwnershipViolation,
					Component: id,
					File:      comp.SourceFile,
					Location:  slot.Kind,
					Message:   fmt.Sprintf("emitting fact %q but component has no owns", slot.Kind),
				})
			}
		}
		// emit, role=action is always ok (self-scheduling or call-out).
	}

	// receive, role=action, kind not in owns => error (accepting commands in another namespace)
	for _, slot := range comp.Receives {
		if slot.Role == RoleAction {
			ns := namespace(slot.Kind)
			if comp.Owns != "" && ns != comp.Owns {
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindOwnershipViolation,
					Component: id,
					File:      comp.SourceFile,
					Location:  slot.Kind,
					Message: fmt.Sprintf("receiving action %q outside owned namespace %q",
						slot.Kind, comp.Owns),
				})
			} else if comp.Owns == "" {
				findings = append(findings, Finding{
					Severity:  SeverityError,
					Kind:      KindOwnershipViolation,
					Component: id,
					File:      comp.SourceFile,
					Location:  slot.Kind,
					Message:   fmt.Sprintf("receiving action %q but component has no owns", slot.Kind),
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
	if strings.HasPrefix(ref, "#/vocab/") {
		name := strings.TrimPrefix(ref, "#/vocab/")
		_, ok := comp.Vocab[name]
		return ok
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
