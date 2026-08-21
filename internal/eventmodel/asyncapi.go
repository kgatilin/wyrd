package eventmodel

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
)

// Reading AsyncAPI 3 declarations.
//
// A project that already describes its event log as AsyncAPI does not have to
// restate it as .arch/events.yaml. wyrd reads `.arch/asyncapi.yaml` — one
// AsyncAPI 3.0.0 document carrying the `x-eventlog` extension — into the same
// Component the native format produces, so the graph, the canvas and the MCP
// tools work over either source without knowing which one it came from.
//
// # The extension
//
// AsyncAPI describes what one application sends and receives. Four facts of an
// event-log contract have no vocabulary in it, and the documents carry them
// under a single `x-eventlog` key at four locations:
//
//   - root: `owns` (address prefixes), `key` (the fold's partition key),
//     `addressing` (framework prefix and tenant parameter), `port`;
//   - channel: `partition`, the address parameters that are partition
//     coordinates — as opposed to the tenant and the port parameter, which are
//     ordinary parameters to AsyncAPI and are not part of any key;
//   - operation: `role` — `command`, `event`, `call` or `observe`, because
//     `action: send|receive` is two values for four roles;
//   - message: `class`, `command` or `event`.
//
// # Roles map onto the three lists
//
//	command   receive, owned address    -> Inputs
//	observe   receive, either           -> StateEvents
//	event     send, owned address       -> Outputs
//	call      send, foreign address     -> Outputs
//
// The read-set of the source model is commands plus observes, which is exactly
// Inputs plus StateEvents. What the projection loses is the event/call
// distinction, and it is recoverable by matching the address against `owns`.
//
// # The partition key is read, not derived
//
// partitionKeyOf takes every {slot} of a subject, which is right for the native
// format and wrong here: an AsyncAPI address is in wire coordinates, so
// `{tenant}` and the port parameter are slots that are not partition
// coordinates. The document states the answer — `x-eventlog.key` at the root
// and `partition` per channel — and this reader takes it verbatim rather than
// re-deriving a key the source went out of its way to publish.
//
// # A port is one component
//
// One document describes a family of instances sharing a contract. It reads as
// one component; `port.instances` and any per-operation `instances` are carried
// through in Extra for display. Expanding the family into one component per
// instance would draw truer call edges and is a separate decision, not
// something this reader guesses at.

const asyncAPIFileName = "asyncapi.yaml"

// asyncAPIDoc is the subset of AsyncAPI 3.0.0 the reader consumes. Decoding is
// deliberately NOT strict — unlike events.yaml, this is somebody else's format,
// and a document carrying `servers`, `tags` or a vocabulary revision wyrd has
// not seen must still read.
type asyncAPIDoc struct {
	AsyncAPI   string                     `yaml:"asyncapi"`
	ID         string                     `yaml:"id"`
	Info       asyncAPIInfo               `yaml:"info"`
	Eventlog   asyncAPIRootExt            `yaml:"x-eventlog"`
	Channels   map[string]asyncAPIChannel `yaml:"channels"`
	Operations map[string]asyncAPIOp      `yaml:"operations"`
	Components asyncAPIComponents         `yaml:"components"`
}

type asyncAPIInfo struct {
	Title       string `yaml:"title"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
}

type asyncAPIRootExt struct {
	Vocabulary string             `yaml:"vocabulary"`
	Identity   asyncAPIIdentity   `yaml:"identity"`
	Addressing asyncAPIAddressing `yaml:"addressing"`
	Owns       []string           `yaml:"owns"`
	Key        []string           `yaml:"key"`
	Port       *asyncAPIPort      `yaml:"port"`
}

type asyncAPIIdentity struct {
	Service string `yaml:"service"`
	Name    string `yaml:"name"`
}

type asyncAPIAddressing struct {
	Prefix string `yaml:"prefix"`
	Tenant string `yaml:"tenant"`
}

type asyncAPIPort struct {
	Parameter string   `yaml:"parameter"`
	Instances []string `yaml:"instances"`
}

type asyncAPIChannel struct {
	Address  string                   `yaml:"address"`
	Messages map[string]asyncAPIRef   `yaml:"messages"`
	Eventlog asyncAPIChannelExt       `yaml:"x-eventlog"`
	Params   map[string]asyncAPIParam `yaml:"parameters"`
}

type asyncAPIParam struct {
	Description string   `yaml:"description"`
	Enum        []string `yaml:"enum"`
}

type asyncAPIChannelExt struct {
	Partition []string `yaml:"partition"`
}

type asyncAPIOp struct {
	Action   string        `yaml:"action"`
	Channel  asyncAPIRef   `yaml:"channel"`
	Messages []asyncAPIRef `yaml:"messages"`
	Summary  string        `yaml:"summary"`
	Eventlog asyncAPIOpExt `yaml:"x-eventlog"`
}

type asyncAPIOpExt struct {
	Role      string   `yaml:"role"`
	Instances []string `yaml:"instances"`
}

type asyncAPIRef struct {
	Ref string `yaml:"$ref"`
}

type asyncAPIComponents struct {
	Messages map[string]asyncAPIMessage `yaml:"messages"`
}

type asyncAPIMessage struct {
	Name     string             `yaml:"name"`
	Summary  string             `yaml:"summary"`
	Payload  asyncAPIPayload    `yaml:"payload"`
	Eventlog asyncAPIMessageExt `yaml:"x-eventlog"`
}

type asyncAPIMessageExt struct {
	Class string `yaml:"class"`
}

// asyncAPIPayload is a Multi Format Schema Object when it carries
// `schemaFormat`, and a bare schema otherwise. Both forms occur, so the payload
// is captured whole and unwrapped after decoding.
type asyncAPIPayload struct {
	Raw any
}

// UnmarshalYAML captures the payload node verbatim; the schema body is opaque
// to wyrd and must survive round-tripping unchanged.
func (p *asyncAPIPayload) UnmarshalYAML(node *yamlv3.Node) error {
	var raw any
	if err := node.Decode(&raw); err != nil {
		return err
	}
	p.Raw = raw
	return nil
}

// schema returns the payload's schema body and its declared format. A Multi
// Format Schema Object nests the body under `schema`; a bare payload is the
// body itself.
func (p asyncAPIPayload) schema() (any, string) {
	m, ok := p.Raw.(map[string]any)
	if !ok {
		return p.Raw, ""
	}
	format, _ := m["schemaFormat"].(string)
	if body, ok := m["schema"]; ok {
		return body, format
	}
	return p.Raw, format
}

// The four roles an operation may declare.
const (
	roleCommand = "command"
	roleEvent   = "event"
	roleCall    = "call"
	roleObserve = "observe"
)

// parseAsyncAPIFile reads one .arch/asyncapi.yaml into a Component.
func parseAsyncAPIFile(path string) (*Component, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	var doc asyncAPIDoc
	dec := yamlv3.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}
	return componentFromAsyncAPI(&doc, path)
}

// componentFromAsyncAPI projects a decoded document onto a Component. It is
// separate from the file read so tests can drive it from a literal.
func componentFromAsyncAPI(doc *asyncAPIDoc, path string) (*Component, error) {
	if doc.AsyncAPI == "" {
		return nil, fmt.Errorf("missing required field 'asyncapi'")
	}
	if !strings.HasPrefix(doc.AsyncAPI, "3.") {
		return nil, fmt.Errorf("unsupported asyncapi version %q (want 3.x)", doc.AsyncAPI)
	}

	id := doc.Info.Title
	if id == "" {
		id = asyncAPIIdentityID(doc.Eventlog.Identity)
	}
	if id == "" {
		return nil, fmt.Errorf("cannot name the component: neither info.title nor x-eventlog.identity is set")
	}

	comp := &Component{
		Version:      SchemaVersion,
		ID:           id,
		Owns:         doc.Eventlog.Identity.Service,
		Description:  doc.Info.Description,
		PartitionKey: doc.Eventlog.Key,
		Types:        map[string]SchemaNode{},
		Extra:        asyncAPIComponentExtra(doc),
		Source:       SourceAsyncAPI,
		SourceFile:   path,
	}

	// Operations in key order: the document is emitted deterministically and
	// the projection must not be the step that reorders it.
	opKeys := make([]string, 0, len(doc.Operations))
	for key := range doc.Operations {
		opKeys = append(opKeys, key)
	}
	sort.Strings(opKeys)

	for _, key := range opKeys {
		op := doc.Operations[key]
		slots, err := asyncAPISlots(doc, op)
		if err != nil {
			return nil, fmt.Errorf("operation %q: %w", key, err)
		}
		role := op.Eventlog.Role
		if role == "" {
			role = roleFromAction(op.Action)
		}
		for _, slot := range slots {
			switch role {
			case roleCommand:
				comp.Inputs = append(comp.Inputs, slot)
			case roleObserve:
				comp.StateEvents = append(comp.StateEvents, slot)
			case roleEvent, roleCall:
				comp.Outputs = append(comp.Outputs, slot)
			default:
				return nil, fmt.Errorf("operation %q: unknown role %q (want command, event, call or observe)", key, role)
			}
		}
	}

	// A document that states no key still has one if its channels name
	// partition coordinates; taking the first is the same rule the native
	// reader applies to slot tokens, over a list the document already vetted.
	if len(comp.PartitionKey) == 0 {
		comp.PartitionKey = firstChannelPartition(doc)
	}

	return comp, nil
}

// asyncAPISlots turns one operation into the slots it declares — normally one,
// since one channel carries one kind.
func asyncAPISlots(doc *asyncAPIDoc, op asyncAPIOp) ([]Slot, error) {
	channelKey, err := pointerTail(op.Channel.Ref, "channels")
	if err != nil {
		return nil, err
	}
	channel, ok := doc.Channels[channelKey]
	if !ok {
		return nil, fmt.Errorf("channel %q is not defined", channelKey)
	}

	kinds := operationKinds(op, channel)
	if len(kinds) == 0 {
		return nil, fmt.Errorf("channel %q declares no messages", channelKey)
	}

	slots := make([]Slot, 0, len(kinds))
	for _, kind := range kinds {
		msg := doc.Components.Messages[kind]
		body, format := msg.Payload.schema()

		description := msg.Summary
		if description == "" {
			description = op.Summary
		}

		slots = append(slots, Slot{
			Kind:        kind,
			Pattern:     channel.Address,
			Delivery:    DeliveryBroadcast,
			Description: description,
			Schema:      SchemaNode{Raw: body},
			Extra:       slotExtra(op, channel, msg, format),
		})
	}
	return slots, nil
}

// operationKinds names the messages an operation carries: the ones it lists,
// or — when it lists none, which the spec permits — every message on its
// channel.
func operationKinds(op asyncAPIOp, channel asyncAPIChannel) []string {
	var kinds []string
	for _, ref := range op.Messages {
		// '#/channels/<key>/messages/<kind>' — the kind is the last segment.
		if idx := strings.LastIndex(ref.Ref, "/"); idx >= 0 && idx+1 < len(ref.Ref) {
			kinds = append(kinds, ref.Ref[idx+1:])
		}
	}
	if len(kinds) > 0 {
		return kinds
	}
	for kind := range channel.Messages {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

// slotExtra carries the facts the three-list model has no field for. Nothing
// here is interpreted: it exists so the canvas can show a role, an instance
// list or a payload dialect that the projection would otherwise drop.
func slotExtra(op asyncAPIOp, channel asyncAPIChannel, msg asyncAPIMessage, format string) map[string]any {
	extra := map[string]any{}
	if role := op.Eventlog.Role; role != "" {
		extra["role"] = role
	}
	if op.Action != "" {
		extra["action"] = op.Action
	}
	if len(op.Eventlog.Instances) > 0 {
		extra["instances"] = op.Eventlog.Instances
	}
	if class := msg.Eventlog.Class; class != "" {
		extra["class"] = class
	}
	if len(channel.Eventlog.Partition) > 0 {
		extra["partition"] = channel.Eventlog.Partition
	}
	if format != "" {
		extra["schema_format"] = format
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

// asyncAPIComponentExtra carries the document-level facts with no field on
// Component: the address space it owns, the framework prefix those addresses
// are rooted at, and the port family the document describes.
func asyncAPIComponentExtra(doc *asyncAPIDoc) map[string]any {
	extra := map[string]any{}
	if doc.ID != "" {
		extra["asyncapi_id"] = doc.ID
	}
	if v := doc.Info.Version; v != "" {
		extra["declaration_version"] = v
	}
	if v := doc.Eventlog.Vocabulary; v != "" {
		extra["vocabulary"] = v
	}
	if len(doc.Eventlog.Owns) > 0 {
		extra["owns_addresses"] = doc.Eventlog.Owns
	}
	if p := doc.Eventlog.Addressing.Prefix; p != "" {
		extra["addressing_prefix"] = p
	}
	if t := doc.Eventlog.Addressing.Tenant; t != "" {
		extra["addressing_tenant"] = t
	}
	if name := doc.Eventlog.Identity.Name; name != "" {
		extra["instance_name"] = name
	}
	if port := doc.Eventlog.Port; port != nil {
		if port.Parameter != "" {
			extra["port_parameter"] = port.Parameter
		}
		if len(port.Instances) > 0 {
			extra["port_instances"] = port.Instances
		}
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

// asyncAPIIdentityID names a component from its identity when the document
// carries no title: `<service>` for a port, `<service>/<name>` for a single
// controller, matching the title the spec prescribes.
func asyncAPIIdentityID(identity asyncAPIIdentity) string {
	if identity.Service == "" {
		return ""
	}
	if identity.Name == "" {
		return identity.Service
	}
	return identity.Service + "/" + identity.Name
}

// roleFromAction is the fallback for a document with no `x-eventlog.role`: a
// plain AsyncAPI file still splits into sent and received, which is enough to
// place a slot even though it cannot tell an event from a call.
func roleFromAction(action string) string {
	switch action {
	case "receive":
		return roleObserve
	case "send":
		return roleEvent
	default:
		return ""
	}
}

// firstChannelPartition returns the partition coordinates of the first channel
// that names any, in key order.
func firstChannelPartition(doc *asyncAPIDoc) []string {
	keys := make([]string, 0, len(doc.Channels))
	for key := range doc.Channels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if part := doc.Channels[key].Eventlog.Partition; len(part) > 0 {
			return part
		}
	}
	return nil
}

// pointerTail resolves a local JSON Pointer of the form '#/<section>/<key>'.
// Keys in these documents are restricted to `^[a-zA-Z0-9.\-_]+$`, so no
// pointer escaping can occur and the tail is the key verbatim.
func pointerTail(ref, section string) (string, error) {
	prefix := "#/" + section + "/"
	if !strings.HasPrefix(ref, prefix) {
		return "", fmt.Errorf("expected a $ref into #/%s/, got %q", section, ref)
	}
	return strings.TrimPrefix(ref, prefix), nil
}
