package eventmodel

// One instance of a payload schema.
//
// A schema answers "what may be on the wire"; a reader looking at a kind wants
// "what is on the wire". ExampleOf renders the second from the first — one
// value per property, arrays with a single element, $refs followed — so the
// canvas and the tools can show an object next to the schema instead of asking
// everybody to compile the schema in their head.
//
// The example is derived, not declared. Where the schema states a value —
// `const`, `example`, `examples`, `default`, an `enum` — that value is used;
// everywhere else the example carries a placeholder naming the type, the way
// generated API documentation does. Nothing here validates: an example is a
// reading aid, and a schema wyrd cannot make sense of yields nil rather than a
// finding.

// exampleDepthLimit bounds a schema that nests without recursing — a chain of
// distinct objects deep enough to be a document rather than an example. Ref
// cycles are caught separately, by the visited set.
const exampleDepthLimit = 12

// ExampleOf renders one example instance of schema. comp is the component the
// schema was declared in, which is what a local `#/types/X` ref is relative to;
// m resolves cross-component refs. Returns nil when the schema says nothing an
// example can be built from — an empty node, or a ref that resolves nowhere.
func ExampleOf(m *Model, comp *Component, schema SchemaNode) any {
	if schema.IsZero() {
		return nil
	}
	return exampleOf(m, comp, schema, map[string]struct{}{}, 0)
}

func exampleOf(m *Model, comp *Component, schema SchemaNode, visiting map[string]struct{}, depth int) any {
	if schema.IsZero() || depth > exampleDepthLimit {
		return nil
	}
	node := schema.AsMap()
	if node == nil {
		// A schema that is not an object is not a schema wyrd can read. The
		// one shape worth honouring is JSON Schema's boolean form: `true`
		// admits anything, `false` admits nothing, and neither describes a
		// value to show.
		return nil
	}

	if ref, ok := node["$ref"].(string); ok {
		return exampleOfRef(m, comp, ref, visiting, depth)
	}
	if stated, ok := statedValue(node); ok {
		return stated
	}
	if branch, target, ok := firstBranch(node); ok {
		if target == "allOf" {
			return exampleOfAllOf(m, comp, branch, visiting, depth)
		}
		return exampleOf(m, comp, SchemaNode{Raw: branch[0]}, visiting, depth)
	}

	switch typeOf(node) {
	case "object":
		return exampleObject(m, comp, node, visiting, depth)
	case "array":
		return exampleArray(m, comp, node, visiting, depth)
	case "string":
		return exampleString(node)
	case "integer", "number":
		return 0
	case "boolean":
		return false
	case "null":
		return nil
	}

	// No `type`, but the keywords give the shape away.
	if _, ok := node["properties"]; ok {
		return exampleObject(m, comp, node, visiting, depth)
	}
	if _, ok := node["items"]; ok {
		return exampleArray(m, comp, node, visiting, depth)
	}
	return nil
}

// exampleOfRef follows a $ref, refusing to follow one it is already inside:
// a self-referential type describes an unbounded value, and an example of it
// has to stop somewhere.
func exampleOfRef(m *Model, comp *Component, ref string, visiting map[string]struct{}, depth int) any {
	target, targetComp := resolveTypeRef(m, comp, ref)
	if target.IsZero() {
		return nil
	}
	key := ref
	if targetComp != nil {
		key = targetComp.ID + ref
	}
	if _, seen := visiting[key]; seen {
		return nil
	}
	visiting[key] = struct{}{}
	defer delete(visiting, key)
	return exampleOf(m, targetComp, target, visiting, depth+1)
}

// resolveTypeRef resolves "#/types/X" against comp and "other#/types/X"
// against the model, answering with the type and the component it belongs to —
// the component a ref nested inside it is relative to.
func resolveTypeRef(m *Model, comp *Component, ref string) (SchemaNode, *Component) {
	if name, ok := cutLocalTypeRef(ref); ok {
		if comp == nil {
			return SchemaNode{}, nil
		}
		return comp.Types[name], comp
	}
	compID, name, ok := parseCrossComponentRef(ref)
	if !ok || m == nil {
		return SchemaNode{}, nil
	}
	target, exists := m.Components[compID]
	if !exists {
		return SchemaNode{}, nil
	}
	return target.Types[name], target
}

// statedValue returns a value the schema states outright, in the order a
// reader would trust them: an explicit example beats a default, and both beat
// a guess derived from the type.
func statedValue(node map[string]any) (any, bool) {
	if v, ok := node["const"]; ok {
		return v, true
	}
	if v, ok := node["example"]; ok {
		return v, true
	}
	if list, ok := node["examples"].([]any); ok && len(list) > 0 {
		return list[0], true
	}
	if v, ok := node["default"]; ok {
		return v, true
	}
	if list, ok := node["enum"].([]any); ok && len(list) > 0 {
		return list[0], true
	}
	return nil, false
}

// firstBranch finds a combinator. oneOf and anyOf are alternatives and an
// example shows the first; allOf is a conjunction and is merged.
func firstBranch(node map[string]any) ([]any, string, bool) {
	for _, key := range []string{"allOf", "oneOf", "anyOf"} {
		if list, ok := node[key].([]any); ok && len(list) > 0 {
			return list, key, true
		}
	}
	return nil, "", false
}

func exampleOfAllOf(m *Model, comp *Component, branches []any, visiting map[string]struct{}, depth int) any {
	merged := map[string]any{}
	var fallback any
	for _, branch := range branches {
		value := exampleOf(m, comp, SchemaNode{Raw: branch}, visiting, depth+1)
		object, ok := value.(map[string]any)
		if !ok {
			// A non-object member of an allOf constrains a scalar rather than
			// adding properties; the last one that produced a value stands in
			// for the whole, since there is nothing to merge it into.
			if value != nil {
				fallback = value
			}
			continue
		}
		for name, field := range object {
			merged[name] = field
		}
	}
	if len(merged) == 0 {
		return fallback
	}
	return merged
}

func exampleObject(m *Model, comp *Component, node map[string]any, visiting map[string]struct{}, depth int) any {
	props, _ := node["properties"].(map[string]any)
	out := map[string]any{}
	for name, raw := range props {
		out[name] = exampleOf(m, comp, SchemaNode{Raw: raw}, visiting, depth+1)
	}
	// An object with no properties is still an object: `{}` reads as "a body
	// with nothing declared in it", which is what the schema says.
	return out
}

func exampleArray(m *Model, comp *Component, node map[string]any, visiting map[string]struct{}, depth int) any {
	items, ok := node["items"]
	if !ok {
		return []any{}
	}
	element := exampleOf(m, comp, SchemaNode{Raw: items}, visiting, depth+1)
	if element == nil {
		return []any{}
	}
	return []any{element}
}

// exampleString names the type, except where `format` names something
// narrower. The values are fixed rather than generated: an example that
// changes between two reads of the same model is a diff nobody asked for.
func exampleString(node map[string]any) any {
	switch format, _ := node["format"].(string); format {
	case "date-time":
		return "2024-01-01T00:00:00Z"
	case "date":
		return "2024-01-01"
	case "time":
		return "00:00:00Z"
	case "duration":
		return "PT1H"
	case "uuid":
		return "00000000-0000-0000-0000-000000000000"
	case "email":
		return "someone@example.com"
	case "uri", "uri-reference", "url":
		return "https://example.com"
	case "hostname":
		return "example.com"
	case "ipv4":
		return "192.0.2.1"
	case "ipv6":
		return "2001:db8::1"
	default:
		return "string"
	}
}

// typeOf reads the `type` keyword, taking the first entry of a union — a
// nullable field is written `[string, "null"]` and an example of it is a
// string, not a null.
func typeOf(node map[string]any) string {
	switch typed := node["type"].(type) {
	case string:
		return typed
	case []any:
		for _, entry := range typed {
			name, ok := entry.(string)
			if ok && name != "null" {
				return name
			}
		}
	}
	return ""
}
