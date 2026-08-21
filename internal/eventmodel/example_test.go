package eventmodel

import (
	"encoding/json"
	"testing"
)

// schema parses a JSON literal into the shape a YAML decode produces, so the
// tests can state a schema the way it reads in a declaration file.
func schema(t *testing.T, literal string) SchemaNode {
	t.Helper()
	var raw any
	if err := json.Unmarshal([]byte(literal), &raw); err != nil {
		t.Fatalf("bad schema literal: %v", err)
	}
	return SchemaNode{Raw: raw}
}

// wantJSON compares an example against a JSON literal, which keeps the
// expectations readable and does not care what order a map came out in.
func wantJSON(t *testing.T, got any, want string) {
	t.Helper()
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal example: %v", err)
	}
	var gotAny, wantAny any
	if err := json.Unmarshal(encoded, &gotAny); err != nil {
		t.Fatalf("re-read example: %v", err)
	}
	if err := json.Unmarshal([]byte(want), &wantAny); err != nil {
		t.Fatalf("bad want literal: %v", err)
	}
	gotText, _ := json.Marshal(gotAny)
	wantText, _ := json.Marshal(wantAny)
	if string(gotText) != string(wantText) {
		t.Errorf("example mismatch\n got: %s\nwant: %s", gotText, wantText)
	}
}

func TestExampleOfScalarsAndObjects(t *testing.T) {
	orders := comp("orders", "orders")
	m := model(orders)

	got := ExampleOf(m, orders, schema(t, `{
		"type": "object",
		"required": ["order_id"],
		"properties": {
			"order_id": {"type": "string"},
			"total":    {"type": "number"},
			"lines":    {"type": "integer"},
			"paid":     {"type": "boolean"},
			"placed_at": {"type": "string", "format": "date-time"}
		}
	}`))

	wantJSON(t, got, `{
		"order_id": "string",
		"total": 0,
		"lines": 0,
		"paid": false,
		"placed_at": "2024-01-01T00:00:00Z"
	}`)
}

func TestExampleOfArrayCarriesOneElement(t *testing.T) {
	orders := comp("orders", "orders")
	m := model(orders)

	got := ExampleOf(m, orders, schema(t, `{
		"type": "object",
		"properties": {
			"lines": {"type": "array", "items": {"type": "object", "properties": {"sku": {"type": "string"}}}},
			"tags":  {"type": "array"}
		}
	}`))

	wantJSON(t, got, `{"lines": [{"sku": "string"}], "tags": []}`)
}

func TestExampleOfPrefersStatedValues(t *testing.T) {
	orders := comp("orders", "orders")
	m := model(orders)

	got := ExampleOf(m, orders, schema(t, `{
		"type": "object",
		"properties": {
			"currency": {"type": "string", "default": "EUR"},
			"status":   {"type": "string", "enum": ["placed", "confirmed"]},
			"version":  {"const": 3},
			"customer": {"type": "string", "example": "acme"},
			"channel":  {"type": "string", "examples": ["web", "app"]}
		}
	}`))

	wantJSON(t, got, `{
		"currency": "EUR",
		"status": "placed",
		"version": 3,
		"customer": "acme",
		"channel": "web"
	}`)
}

func TestExampleOfFollowsLocalAndCrossComponentRefs(t *testing.T) {
	inventory := comp("inventory", "inventory")
	inventory.Types["Line"] = schema(t, `{"type": "object", "properties": {"sku": {"type": "string"}, "qty": {"type": "integer"}}}`)
	inventory.Types["Hold"] = schema(t, `{"type": "object", "properties": {"order_id": {"type": "string"}, "lines": {"type": "array", "items": {"$ref": "#/types/Line"}}}}`)

	orders := comp("orders", "orders")
	m := model(inventory, orders)

	local := ExampleOf(m, inventory, schema(t, `{"$ref": "#/types/Hold"}`))
	wantJSON(t, local, `{"order_id": "string", "lines": [{"sku": "string", "qty": 0}]}`)

	// A ref inside the target resolves against the component that owns the
	// target, not against the one that pointed at it.
	cross := ExampleOf(m, orders, schema(t, `{"$ref": "inventory#/types/Hold"}`))
	wantJSON(t, cross, `{"order_id": "string", "lines": [{"sku": "string", "qty": 0}]}`)
}

func TestExampleOfStopsAtRefCycle(t *testing.T) {
	tree := comp("tree", "tree")
	tree.Types["Node"] = schema(t, `{"type": "object", "properties": {"name": {"type": "string"}, "child": {"$ref": "#/types/Node"}}}`)
	m := model(tree)

	// The type expands once and stops: re-entering a ref it is already inside
	// would run forever, and one level is enough to show the shape.
	got := ExampleOf(m, tree, schema(t, `{"$ref": "#/types/Node"}`))
	wantJSON(t, got, `{"name": "string", "child": null}`)
}

func TestExampleOfCombinators(t *testing.T) {
	billing := comp("billing", "billing")
	m := model(billing)

	merged := ExampleOf(m, billing, schema(t, `{
		"allOf": [
			{"type": "object", "properties": {"id": {"type": "string"}}},
			{"type": "object", "properties": {"amount": {"type": "number"}}}
		]
	}`))
	wantJSON(t, merged, `{"id": "string", "amount": 0}`)

	first := ExampleOf(m, billing, schema(t, `{
		"oneOf": [
			{"type": "object", "properties": {"card": {"type": "string"}}},
			{"type": "object", "properties": {"transfer": {"type": "string"}}}
		]
	}`))
	wantJSON(t, first, `{"card": "string"}`)
}

func TestExampleOfUnreadableSchema(t *testing.T) {
	orders := comp("orders", "orders")
	m := model(orders)

	if got := ExampleOf(m, orders, SchemaNode{}); got != nil {
		t.Errorf("empty schema: want nil, got %#v", got)
	}
	if got := ExampleOf(m, orders, schema(t, `{"$ref": "#/types/Nowhere"}`)); got != nil {
		t.Errorf("dangling ref: want nil, got %#v", got)
	}
	// A nullable union names the type that is not null.
	wantJSON(t, ExampleOf(m, orders, schema(t, `{"type": ["string", "null"]}`)), `"string"`)
}
