package eventmodel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"unicode"

	"github.com/kgatilin/archai/internal/eventmodel"
)

// TemplateData is the data model archai exposes to a project's codegen
// templates. It is a stable contract, versioned alongside the declaration
// format: templates live in projects, so changing a field name here breaks
// every project that reads it.
//
// It is deliberately language-neutral. archai renders a project-supplied
// template and never learns the project's types — the whole point of the
// design is that archai is not a dependency of a production binary, in any
// language. A Go generator built into archai would invert that.
//
// Ordering is deterministic everywhere: maps are flattened into sorted slices
// so a template's output does not churn between runs.
type TemplateData struct {
	// Component is the declaring component's stable id.
	Component string

	// Owns is the namespace whose schemas this component defines ("" if none).
	Owns string

	// Description is the component's human-readable summary.
	Description string

	// Extra is the component's opaque passthrough block.
	Extra map[string]any

	// Inputs, Outputs and StateEvents are the component's three declared
	// lists, in declaration order.
	Inputs      []TemplateSlot
	Outputs     []TemplateSlot
	StateEvents []TemplateSlot

	// Fold is the component's derived projection: the read-set a runtime
	// subscribes with. Nothing declares it — it is Inputs plus StateEvents.
	Fold TemplateFold

	// Types are the component's reusable schema definitions, sorted by name.
	Types []TemplateType

	// ForeignInputs, ForeignOutputs and ForeignStateEvents are the subsets
	// whose kind falls outside Owns — the component's coupling to other
	// namespaces, which is usually what a template needs to treat differently
	// (imports, a separate address book, an external-API catalog).
	ForeignInputs      []TemplateSlot
	ForeignOutputs     []TemplateSlot
	ForeignStateEvents []TemplateSlot

	// Kinds is the sorted unique set of kinds this component declares on any
	// of the three lists — the natural input for a constant block.
	Kinds []string
}

// TemplateSlot is one inputs, outputs or state_events entry as templates
// see it.
type TemplateSlot struct {
	Kind string

	// Pattern is the subject the kind travels on, with {slot} tokens, or ""
	// when the declaration carries none.
	Pattern string

	// PartitionKey is the ordered {slot} names of Pattern — what a template
	// needs to bind subscription placeholders without parsing the pattern.
	PartitionKey []string

	// Delivery is normalized: "broadcast" when the declaration omitted it, so
	// templates never have to special-case the empty string.
	Delivery string

	Description string
	Exposure    []string

	// Schema is the raw JSON-Schema-as-YAML tree (maps, slices, scalars), or
	// nil when undeclared. Render it with the jsonRaw / jsonIndent helpers
	// rather than walking it by hand.
	Schema any

	Extra map[string]any
}

// Exclusive reports whether the slot opted into single-handler delivery.
// Exposed as a method so templates can write {{if .Exclusive}} instead of
// comparing strings.
func (s TemplateSlot) Exclusive() bool {
	return s.Delivery == string(eventmodel.DeliveryExclusive)
}

// TemplateFold is the component's derived projection as templates see it.
// It is not declared anywhere: Subjects and Consumes come from Inputs plus
// StateEvents, in that order, deduplicated. Outputs are excluded — appending
// an event does not subscribe you to it.
type TemplateFold struct {
	// Subjects are the transport patterns of the read-set; PartitionKey is the
	// ordered {slot} name list they all share. Both are what a subject-grammar
	// template needs to emit subscriptions and state keys.
	Subjects     []string
	PartitionKey []string

	// Consumes are the kinds the reducer folds.
	Consumes []string

	// State is the raw projection-state schema tree, or nil when the component
	// declared none.
	State any
}

// TemplateType is one reusable schema definition.
type TemplateType struct {
	Name   string
	Schema any
}

// BuildTemplateData projects one component into the template data model.
func BuildTemplateData(comp *eventmodel.Component) TemplateData {
	data := TemplateData{
		Component:   comp.ID,
		Owns:        comp.Owns,
		Description: comp.Description,
		Extra:       comp.Extra,
	}

	kinds := make(map[string]struct{})

	sections := []struct {
		slots   []eventmodel.Slot
		dst     *[]TemplateSlot
		foreign *[]TemplateSlot
	}{
		{comp.Inputs, &data.Inputs, &data.ForeignInputs},
		{comp.Outputs, &data.Outputs, &data.ForeignOutputs},
		{comp.StateEvents, &data.StateEvents, &data.ForeignStateEvents},
	}
	for _, section := range sections {
		for _, slot := range section.slots {
			ts := templateSlot(slot)
			*section.dst = append(*section.dst, ts)
			kinds[slot.Kind] = struct{}{}
			if !kindInNamespace(slot.Kind, comp.Owns) {
				*section.foreign = append(*section.foreign, ts)
			}
		}
	}

	data.Fold = TemplateFold{
		Subjects:     comp.Subjects(),
		PartitionKey: comp.PartitionKey,
		Consumes:     comp.Consumes(),
		State:        comp.State.Raw,
	}

	names := make([]string, 0, len(comp.Types))
	for name := range comp.Types {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data.Types = append(data.Types, TemplateType{Name: name, Schema: comp.Types[name].Raw})
	}

	data.Kinds = make([]string, 0, len(kinds))
	for kind := range kinds {
		data.Kinds = append(data.Kinds, kind)
	}
	sort.Strings(data.Kinds)

	return data
}

func templateSlot(slot eventmodel.Slot) TemplateSlot {
	delivery := slot.Delivery
	if delivery == "" {
		delivery = eventmodel.DeliveryBroadcast
	}
	return TemplateSlot{
		Kind:         slot.Kind,
		Pattern:      slot.Pattern,
		PartitionKey: eventmodel.SlotTokens(slot.Pattern),
		Delivery:     string(delivery),
		Description:  slot.Description,
		Exposure:     slot.Exposure,
		Schema:       slot.Schema.Raw,
		Extra:        slot.Extra,
	}
}

// kindInNamespace reports whether kind sits under the owns prefix. An empty
// prefix owns nothing, so every kind is foreign to it.
func kindInNamespace(kind, owns string) bool {
	if owns == "" {
		return false
	}
	return kind == owns || (strings.HasPrefix(kind, owns) && len(kind) > len(owns) && kind[len(owns)] == '.')
}

// RenderTemplate executes a project template against one component's data.
// Missing map keys are an error rather than an empty string: a template that
// reaches for a key the declaration never set is a bug worth surfacing at
// generation time, not a silent hole in generated code.
func RenderTemplate(name, text string, data TemplateData) ([]byte, error) {
	tmpl, err := template.New(name).Funcs(TemplateFuncs()).Option("missingkey=error").Parse(text)
	if err != nil {
		return nil, fmt.Errorf("parsing template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("executing template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// TemplateFuncs returns the helper functions available to project templates.
// Like TemplateData, this set is a contract: templates live in projects.
func TemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"goIdent":    GoIdent,
		"unexported": unexportedIdent,
		"quote":      strconv.Quote,
		"jsonRaw":    jsonRaw,
		"jsonIndent": jsonIndent,
		"indent":     indentLines,
		"docComment": docComment,
		"hasPrefix":  strings.HasPrefix,
		"trimPrefix": strings.TrimPrefix,
		"join":       strings.Join,
		"sortedKeys": sortedKeys,
	}
}

// GoIdent converts an arbitrary declaration name into an exported Go-style
// identifier: "billing.invoice.issued" becomes "BillingInvoiceIssued", as does
// "billing-invoice_issued". Segment separators are any non-alphanumeric rune.
//
// The wire name is never derived from the identifier — only the other way
// round. A durable log is not refactorable, so templates must emit the
// declared kind string verbatim and use the identifier only as a Go name.
func GoIdent(s string) string {
	var b strings.Builder
	upper := true
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if upper {
				b.WriteRune(unicode.ToUpper(r))
				upper = false
			} else {
				b.WriteRune(r)
			}
		default:
			upper = true
		}
	}
	out := b.String()
	if out == "" {
		return ""
	}
	// An identifier cannot start with a digit, and an exported one cannot
	// start with an underscore, so prefix a letter instead.
	if unicode.IsDigit(rune(out[0])) {
		return "X" + out
	}
	return out
}

func unexportedIdent(s string) string {
	ident := GoIdent(s)
	if ident == "" {
		return ""
	}
	r := []rune(ident)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// jsonRaw renders a schema node as compact JSON, suitable for embedding in a
// string literal or a raw-string block.
func jsonRaw(v any) (string, error) {
	if v == nil {
		return "null", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshaling schema: %w", err)
	}
	return string(b), nil
}

// jsonIndent renders a schema node as indented JSON.
func jsonIndent(v any) (string, error) {
	if v == nil {
		return "null", nil
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling schema: %w", err)
	}
	return string(b), nil
}

// indentLines prefixes every non-empty line with n spaces.
func indentLines(n int, s string) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = pad + line
		}
	}
	return strings.Join(lines, "\n")
}

// docComment wraps text as Go line comments, one per input line. Empty input
// yields empty output so templates can splice it unconditionally.
func docComment(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = "//"
		} else {
			lines[i] = "// " + line
		}
	}
	return strings.Join(lines, "\n")
}

// sortedKeys returns a map's keys in sorted order, so templates can range over
// Extra blocks deterministically.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
