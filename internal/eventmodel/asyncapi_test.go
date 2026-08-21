package eventmodel

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// A port document: one service, two instances, addresses in wire coordinates.
// The shape follows the eventlog-asyncapi/1 vocabulary; the names are invented.
const portDoc = `
asyncapi: 3.0.0
id: urn:eventlog:svc.app:connectors
info:
  title: connectors
  version: "1"
  description: Event-log port connectors; one instance per configured backend.
defaultContentType: application/json

x-eventlog:
  vocabulary: eventlog-asyncapi/1
  identity: {service: connectors}
  addressing: {prefix: svc.app, tenant: tenant}
  owns: ["svc.app.{tenant}.connectors.{group}"]
  key: [scope, scope-id]
  port: {parameter: group, instances: [alpha, beta]}

channels:
  connectors.GROUP.SCOPE.SCOPE-ID.command.call:
    address: svc.app.{tenant}.connectors.{group}.{scope}.{scope-id}.command.call
    parameters:
      tenant:   {description: Deployment tenant, stamped by the event bus on append.}
      group:    {description: Backend group; the port instance., enum: [alpha, beta]}
      scope:    {description: Partition coordinate.}
      scope-id: {description: Partition coordinate.}
    messages:
      connectors.command.call: {$ref: '#/components/messages/connectors.command.call'}
    x-eventlog:
      partition: [scope, scope-id]

  connectors.GROUP.SCOPE.SCOPE-ID.event.call.completed:
    address: svc.app.{tenant}.connectors.{group}.{scope}.{scope-id}.event.call.completed
    messages:
      connectors.event.call.completed: {$ref: '#/components/messages/connectors.event.call.completed'}
    x-eventlog:
      partition: [scope, scope-id]

operations:
  command.connectors.GROUP.SCOPE.SCOPE-ID.command.call:
    action: receive
    channel: {$ref: '#/channels/connectors.GROUP.SCOPE.SCOPE-ID.command.call'}
    messages:
      - $ref: '#/channels/connectors.GROUP.SCOPE.SCOPE-ID.command.call/messages/connectors.command.call'
    summary: Request one operation from the configured backend
    x-eventlog: {role: command}

  event.connectors.GROUP.SCOPE.SCOPE-ID.event.call.completed:
    action: send
    channel: {$ref: '#/channels/connectors.GROUP.SCOPE.SCOPE-ID.event.call.completed'}
    summary: Record one successfully completed call
    x-eventlog: {role: event}

  observe.connectors.GROUP.SCOPE.SCOPE-ID.event.call.completed:
    action: receive
    channel: {$ref: '#/channels/connectors.GROUP.SCOPE.SCOPE-ID.event.call.completed'}
    summary: Record one successfully completed call
    x-eventlog: {role: observe}

components:
  messages:
    connectors.command.call:
      name: connectors.command.call
      summary: Request one operation from the configured backend
      payload:
        schemaFormat: application/schema+json;version=draft-2020-12
        schema:
          type: object
          required: [call_id, op]
          properties:
            call_id: {type: string}
            op:      {type: string}
      x-eventlog: {class: command}
    connectors.event.call.completed:
      name: connectors.event.call.completed
      summary: Record one successfully completed call
      payload:
        schemaFormat: application/schema+json;version=draft-2020-12
        schema:
          type: object
          properties:
            call_id: {type: string}
      x-eventlog: {class: event}
`

// A caller: it appends into the port's address space, binding the port
// parameter to a literal instance and the owner's open coordinates to literals.
const callerDoc = `
asyncapi: 3.0.0
id: urn:eventlog:svc.app:router
info:
  title: router
  version: "1"
defaultContentType: application/json

x-eventlog:
  vocabulary: eventlog-asyncapi/1
  identity: {service: router}
  addressing: {prefix: svc.app, tenant: tenant}
  owns: ["svc.app.{tenant}.router.{id}"]
  key: [session]
  port: {parameter: id, instances: [north, south]}

channels:
  connectors.alpha.session.SESSION.command.call:
    address: svc.app.{tenant}.connectors.alpha.session.{session}.command.call
    messages:
      connectors.command.call: {$ref: '#/components/messages/connectors.command.call'}
    x-eventlog: {partition: [session]}

operations:
  call.connectors.alpha.session.SESSION.command.call:
    action: send
    channel: {$ref: '#/channels/connectors.alpha.session.SESSION.command.call'}
    summary: Request one operation from the configured backend
    x-eventlog: {role: call, instances: [north]}

components:
  messages:
    connectors.command.call:
      name: connectors.command.call
      summary: Request one operation from the configured backend
      x-eventlog: {class: command}
`

func writeAsyncAPI(t *testing.T, dir, body string) string {
	t.Helper()
	archDir := filepath.Join(dir, ".arch")
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(archDir, asyncAPIFileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestParseAsyncAPIRolesMapOntoTheThreeLists(t *testing.T) {
	path := writeAsyncAPI(t, filepath.Join(t.TempDir(), "connectors"), portDoc)
	comp, err := parseAsyncAPIFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if comp.ID != "connectors" {
		t.Errorf("id = %q, want connectors", comp.ID)
	}
	if comp.Owns != "connectors" {
		t.Errorf("owns = %q, want connectors (the identity service)", comp.Owns)
	}
	if !comp.IsAsyncAPI() {
		t.Error("component should be marked as read from AsyncAPI")
	}

	// command -> inputs, event -> outputs, observe -> state_events. The last
	// two are the same kind: the port emits its outcome and folds it back.
	if got := kindsOf(comp.Inputs); !reflect.DeepEqual(got, []string{"connectors.command.call"}) {
		t.Errorf("inputs = %v", got)
	}
	if got := kindsOf(comp.Outputs); !reflect.DeepEqual(got, []string{"connectors.event.call.completed"}) {
		t.Errorf("outputs = %v", got)
	}
	if got := kindsOf(comp.StateEvents); !reflect.DeepEqual(got, []string{"connectors.event.call.completed"}) {
		t.Errorf("state_events = %v", got)
	}
}

func TestParseAsyncAPIKeyComesFromTheDocumentNotTheAddress(t *testing.T) {
	path := writeAsyncAPI(t, filepath.Join(t.TempDir(), "connectors"), portDoc)
	comp, err := parseAsyncAPIFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	want := []string{"scope", "scope-id"}
	if !reflect.DeepEqual(comp.PartitionKey, want) {
		t.Errorf("partition key = %v, want %v", comp.PartitionKey, want)
	}

	// Deriving it from the address the native way would drag in the tenant and
	// the port parameter, neither of which keys a fold. This is the exact
	// mistake x-eventlog.key exists to prevent, so it is asserted rather than
	// left implied.
	derived := SlotTokens(comp.Inputs[0].Pattern)
	if reflect.DeepEqual(derived, want) {
		t.Fatal("the fixture no longer distinguishes wire slots from partition coordinates")
	}
	if len(derived) != 4 {
		t.Errorf("address slots = %v, expected all four wire coordinates", derived)
	}
}

func TestParseAsyncAPICarriesTheFactsTheThreeListsHaveNoFieldFor(t *testing.T) {
	path := writeAsyncAPI(t, filepath.Join(t.TempDir(), "connectors"), portDoc)
	comp, err := parseAsyncAPIFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got := comp.Extra["port_instances"]; !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Errorf("port instances = %v", got)
	}
	if got := comp.Extra["addressing_prefix"]; got != "svc.app" {
		t.Errorf("addressing prefix = %v", got)
	}
	if got := comp.Extra["owns_addresses"]; !reflect.DeepEqual(got, []string{"svc.app.{tenant}.connectors.{group}"}) {
		t.Errorf("owns addresses = %v", got)
	}

	input := comp.Inputs[0]
	if got := input.Extra["role"]; got != "command" {
		t.Errorf("role = %v", got)
	}
	if got := input.Extra["class"]; got != "command" {
		t.Errorf("class = %v", got)
	}
	if got := input.Extra["partition"]; !reflect.DeepEqual(got, []string{"scope", "scope-id"}) {
		t.Errorf("channel partition = %v", got)
	}
	if got := input.Extra["schema_format"]; got != "application/schema+json;version=draft-2020-12" {
		t.Errorf("schema format = %v", got)
	}

	// The payload body is the schema itself, not the Multi Format wrapper.
	if input.Schema.Get("type").Raw != "object" {
		t.Errorf("payload schema was not unwrapped: %#v", input.Schema.Raw)
	}
}

func TestParseAsyncAPIOperationWithoutMessagesUsesItsChannel(t *testing.T) {
	path := writeAsyncAPI(t, filepath.Join(t.TempDir(), "connectors"), portDoc)
	comp, err := parseAsyncAPIFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Both event.* operations omit `messages`, which the spec permits.
	if len(comp.Outputs) != 1 || comp.Outputs[0].Kind != "connectors.event.call.completed" {
		t.Fatalf("outputs = %v", kindsOf(comp.Outputs))
	}
	if comp.Outputs[0].Description != "Record one successfully completed call" {
		t.Errorf("description = %q", comp.Outputs[0].Description)
	}
}

func TestReadComposesBothFormats(t *testing.T) {
	root := t.TempDir()
	writeAsyncAPI(t, filepath.Join(root, "connectors"), portDoc)
	writeAsyncAPI(t, filepath.Join(root, "router"), callerDoc)

	nativeDir := filepath.Join(root, "audit", ".arch")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	native := `version: 2
component: audit
owns: audit
state_events:
  - kind: connectors.event.call.completed
    pattern: 'svc.app.{tenant}.connectors.{group}.{scope}.{scope-id}.event.call.completed'
state:
  type: object
  properties:
    Count: {type: integer}
`
	if err := os.WriteFile(filepath.Join(nativeDir, eventsFileName), []byte(native), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	model, err := Read(root)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	ids := make([]string, 0, len(model.Components))
	for id := range model.Components {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if want := []string{"audit", "connectors", "router"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("components = %v, want %v", ids, want)
	}
	if model.Components["audit"].IsAsyncAPI() {
		t.Error("the native declaration should not be marked as imported")
	}
	if !model.Components["router"].IsAsyncAPI() {
		t.Error("the AsyncAPI declaration should be marked as imported")
	}
}

func TestValidateSkipsImportedDeclarations(t *testing.T) {
	root := t.TempDir()
	writeAsyncAPI(t, filepath.Join(root, "connectors"), portDoc)
	writeAsyncAPI(t, filepath.Join(root, "router"), callerDoc)

	model, err := Read(root)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// The two documents put connectors.command.call on two addresses — the
	// owner's parameterised one and the caller's bound one. That is a
	// kind-pattern-conflict under the native rules and correct here.
	if findings := Validate(model); len(findings) != 0 {
		t.Errorf("imported declarations should not be validated, got %d finding(s): %v", len(findings), findings)
	}
	if got := len(ValidatableModel(model).Components); got != 0 {
		t.Errorf("validatable components = %d, want 0", got)
	}
}

func TestGraphKeepsImportedPartitionAndSuppressesTheConflictFlag(t *testing.T) {
	root := t.TempDir()
	writeAsyncAPI(t, filepath.Join(root, "connectors"), portDoc)
	writeAsyncAPI(t, filepath.Join(root, "router"), callerDoc)

	model, err := Read(root)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	g := BuildGraph(model)

	var node *Node
	for i := range g.Nodes {
		if g.Nodes[i].ID == kindID("connectors.command.call") {
			node = &g.Nodes[i]
		}
	}
	if node == nil {
		t.Fatal("no node for connectors.command.call")
	}
	if _, flagged := node.Attrs["pattern_conflict"]; flagged {
		t.Error("a port kind on several addresses is the format working, not a conflict")
	}
	if got := node.Attrs["partition_key"]; !reflect.DeepEqual(got, []string{"scope", "scope-id"}) {
		t.Errorf("partition key = %v, want the coordinates the channel declared", got)
	}
}

func TestBuildFlowsCollapsesOntoComponents(t *testing.T) {
	root := t.TempDir()
	writeAsyncAPI(t, filepath.Join(root, "connectors"), portDoc)
	writeAsyncAPI(t, filepath.Join(root, "router"), callerDoc)

	model, err := Read(root)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	flows := BuildFlows(BuildGraph(model))

	// router appends the command; connectors takes it as an input, so the edge
	// is a trigger. connectors folding its own output draws nothing.
	want := []Flow{{From: "router", To: "connectors", Kind: "connectors.command.call", Trigger: true, Health: "ok"}}
	if !reflect.DeepEqual(flows, want) {
		t.Errorf("flows = %+v, want %+v", flows, want)
	}
}

func TestParseAsyncAPIRejectsWhatItCannotPlace(t *testing.T) {
	tests := []struct {
		name string
		doc  string
	}{
		{"no version", "info: {title: x}\n"},
		{"asyncapi 2", "asyncapi: 2.6.0\ninfo: {title: x}\n"},
		{"unknown role", `asyncapi: 3.0.0
info: {title: x}
channels:
  c: {address: a.b, messages: {k: {$ref: '#/components/messages/k'}}}
operations:
  o:
    action: send
    channel: {$ref: '#/channels/c'}
    x-eventlog: {role: broadcast}
`},
		{"dangling channel ref", `asyncapi: 3.0.0
info: {title: x}
operations:
  o:
    action: send
    channel: {$ref: '#/channels/missing'}
`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeAsyncAPI(t, filepath.Join(t.TempDir(), "c"), tt.doc)
			if _, err := parseAsyncAPIFile(path); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func kindsOf(slots []Slot) []string {
	out := make([]string, 0, len(slots))
	for _, slot := range slots {
		out = append(out, slot.Kind)
	}
	return out
}

// Not grading an imported declaration must not mean pretending its events do
// not exist. A native component wired to one is fed and observed like any
// other, and reporting it as starved or orphaned would make every mixed repo
// noisy in exactly the place the two formats meet.
func TestClosureSeesImportedProducersAndConsumers(t *testing.T) {
	root := t.TempDir()
	writeAsyncAPI(t, filepath.Join(root, "connectors"), portDoc)

	nativeDir := filepath.Join(root, "ops", ".arch")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Fed by the port's event, and feeding the port's command.
	native := `version: 2
component: ops
owns: ops
inputs:
  - kind: connectors.event.call.completed
    pattern: 'svc.app.{tenant}.connectors.{group}.{scope}.{scope-id}.event.call.completed'
outputs:
  - kind: connectors.command.call
    pattern: 'svc.app.{tenant}.connectors.alpha.{scope}.{scope-id}.command.call'
`
	if err := os.WriteFile(filepath.Join(nativeDir, eventsFileName), []byte(native), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	model, err := Read(root)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	for _, f := range Validate(model) {
		t.Errorf("unexpected %s finding on %s: %s", f.Kind, f.Component, f.Message)
	}
}

// The other half of the same rule: a native declaration wired to nothing is
// still reported, so widening the indexes did not quietly disable closure.
func TestClosureStillReportsAnUnwiredNativeDeclaration(t *testing.T) {
	root := t.TempDir()
	writeAsyncAPI(t, filepath.Join(root, "connectors"), portDoc)

	nativeDir := filepath.Join(root, "ops", ".arch")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	native := `version: 2
component: ops
owns: ops
inputs:
  - kind: ops.command.sweep
    pattern: 'svc.app.{tenant}.ops.{scope}.command.sweep'
`
	if err := os.WriteFile(filepath.Join(nativeDir, eventsFileName), []byte(native), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	model, err := Read(root)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var kinds []FindingKind
	for _, f := range Validate(model) {
		kinds = append(kinds, f.Kind)
	}
	if !reflect.DeepEqual(kinds, []FindingKind{KindStarvedInput}) {
		t.Errorf("findings = %v, want exactly one starved-input", kinds)
	}
}
