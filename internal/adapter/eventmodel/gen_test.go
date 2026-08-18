package eventmodel

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/eventmodel"
)

func genComponent() *eventmodel.Component {
	return &eventmodel.Component{
		Version:     1,
		ID:          "billing",
		Owns:        "billing",
		Description: "Invoice lifecycle",
		Extra:       map[string]any{"go_package": "billingevents", "partition": "account"},
		Receives: []eventmodel.Slot{
			{
				Kind:     "billing.invoice.issue",
				Role:     eventmodel.RoleAction,
				Exposure: []string{"public_api"},
				Schema:   eventmodel.SchemaNode{Raw: map[string]any{"type": "object"}},
			},
		},
		Emits: []eventmodel.Slot{
			{Kind: "billing.invoice.issued", Role: eventmodel.RoleFact,
				Schema: eventmodel.SchemaNode{Raw: map[string]any{"type": "object"}}},
			{Kind: "ledger.entry.post", Role: eventmodel.RoleAction,
				Delivery: eventmodel.DeliveryExclusive},
		},
		Folds: []eventmodel.Fold{{
			Name:         "billing.open-invoices",
			Subjects:     []string{"svc.*.billing.{account}.invoice.>"},
			PartitionKey: []string{"account"},
			Consumes:     []string{"billing.invoice.*"},
			State:        eventmodel.SchemaNode{Raw: map[string]any{"type": "object"}},
		}},
		Types: map[string]eventmodel.SchemaNode{
			"Invoice": {Raw: map[string]any{"type": "object"}},
			"Line":    {Raw: map[string]any{"type": "object"}},
		},
		SourceFile: "billing/.arch/events.yaml",
	}
}

func TestBuildTemplateData(t *testing.T) {
	d := BuildTemplateData(genComponent())

	if d.Component != "billing" || d.Owns != "billing" {
		t.Errorf("identity = %q/%q", d.Component, d.Owns)
	}
	if len(d.Receives) != 1 || len(d.Emits) != 2 || len(d.Folds) != 1 {
		t.Fatalf("ports = %d receives, %d emits, %d folds", len(d.Receives), len(d.Emits), len(d.Folds))
	}

	// Kinds is the sorted union of both ports.
	want := []string{"billing.invoice.issue", "billing.invoice.issued", "ledger.entry.post"}
	if strings.Join(d.Kinds, ",") != strings.Join(want, ",") {
		t.Errorf("Kinds = %v, want %v", d.Kinds, want)
	}

	// Types are flattened into a sorted slice so template output does not churn.
	if len(d.Types) != 2 || d.Types[0].Name != "Invoice" || d.Types[1].Name != "Line" {
		t.Errorf("Types = %+v, want [Invoice Line]", d.Types)
	}
}

func TestBuildTemplateDataForeign(t *testing.T) {
	d := BuildTemplateData(genComponent())

	if len(d.ForeignEmits) != 1 || d.ForeignEmits[0].Kind != "ledger.entry.post" {
		t.Errorf("ForeignEmits = %+v, want just ledger.entry.post", d.ForeignEmits)
	}
	if len(d.ForeignReceives) != 0 {
		t.Errorf("ForeignReceives = %+v, want empty", d.ForeignReceives)
	}
}

// A component owning nothing has no home namespace, so every kind it touches is
// foreign — the template can still tell inside from outside.
func TestBuildTemplateDataNoOwns(t *testing.T) {
	comp := genComponent()
	comp.Owns = ""

	d := BuildTemplateData(comp)
	if len(d.ForeignEmits) != 2 || len(d.ForeignReceives) != 1 {
		t.Errorf("without owns everything is foreign, got %d emits / %d receives",
			len(d.ForeignEmits), len(d.ForeignReceives))
	}
}

// Delivery is normalized so templates never special-case the empty string.
func TestBuildTemplateDataNormalizesDelivery(t *testing.T) {
	d := BuildTemplateData(genComponent())

	if got := d.Emits[0].Delivery; got != "broadcast" {
		t.Errorf("omitted delivery = %q, want broadcast", got)
	}
	if d.Emits[0].Exclusive() {
		t.Error("broadcast slot must not report Exclusive")
	}
	if got := d.Emits[1].Delivery; got != "exclusive" {
		t.Errorf("declared delivery = %q, want exclusive", got)
	}
	if !d.Emits[1].Exclusive() {
		t.Error("exclusive slot must report Exclusive")
	}
}

func TestGoIdent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"billing.invoice.issued", "BillingInvoiceIssued"},
		{"billing-invoice_issued", "BillingInvoiceIssued"},
		{"llm.failed", "LlmFailed"},
		{"open-invoices", "OpenInvoices"},
		{"already", "Already"},
		{"", ""},
		{"2fa.enabled", "X2faEnabled"}, // an identifier cannot start with a digit
	}
	for _, tc := range cases {
		if got := GoIdent(tc.in); got != tc.want {
			t.Errorf("GoIdent(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The wire name is authoritative: the identifier is derived from it, never the
// other way round, and two kinds that collapse to one identifier must still
// carry their own distinct wire strings.
func TestGoIdentDoesNotAlterWireNames(t *testing.T) {
	d := BuildTemplateData(genComponent())
	out, err := RenderTemplate("t", `{{ range .Kinds }}{{ goIdent . }}={{ . }};{{ end }}`, d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, kind := range d.Kinds {
		if !strings.Contains(string(out), "="+kind+";") {
			t.Errorf("wire name %q must appear verbatim: %s", kind, out)
		}
	}
}

func TestRenderTemplateHelpers(t *testing.T) {
	d := BuildTemplateData(genComponent())

	cases := []struct {
		name, tmpl, want string
	}{
		{"quote", `{{ quote .Component }}`, `"billing"`},
		{"unexported", `{{ unexported "billing.invoice" }}`, "billingInvoice"},
		{"jsonRaw", `{{ jsonRaw (index .Types 0).Schema }}`, `{"type":"object"}`},
		{"join", `{{ join (index .Folds 0).PartitionKey "," }}`, "account"},
		{"docComment", `{{ docComment .Description }}`, "// Invoice lifecycle"},
		{"indent", `{{ indent 2 "a\nb" }}`, "  a\n  b"},
		{"trimPrefix", `{{ trimPrefix (index .Kinds 0) "billing." }}`, "invoice.issue"},
		{"sortedKeys", `{{ join (sortedKeys .Extra) "," }}`, "go_package,partition"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := RenderTemplate(tc.name, tc.tmpl, d)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if string(out) != tc.want {
				t.Errorf("= %q, want %q", out, tc.want)
			}
		})
	}
}

// A template reaching for a key the declaration never set is a bug worth
// surfacing at generation time, not a silent hole in the generated code.
func TestRenderTemplateMissingKeyIsAnError(t *testing.T) {
	d := BuildTemplateData(genComponent())
	if _, err := RenderTemplate("t", `{{ .Extra.nope }}`, d); err == nil {
		t.Error("want an error for a missing Extra key")
	}
	// `index` is the presence test that stays available.
	out, err := RenderTemplate("t", `{{ with index .Extra "nope" }}{{ . }}{{ else }}absent{{ end }}`, d)
	if err != nil {
		t.Fatalf("index should not error on an absent key: %v", err)
	}
	if string(out) != "absent" {
		t.Errorf("= %q, want absent", out)
	}
}

func TestRenderTemplateParseError(t *testing.T) {
	if _, err := RenderTemplate("bad", `{{ if }}`, BuildTemplateData(genComponent())); err == nil {
		t.Error("want a parse error")
	}
}

// repoFile resolves a path relative to the repository root.
func repoFile(t *testing.T, rel string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file location")
	}
	// .../internal/adapter/eventmodel/gen_test.go -> repo root
	root := filepath.Join(filepath.Dir(file), "..", "..", "..")
	return filepath.Join(root, filepath.FromSlash(rel))
}

// TestShippedExampleTemplateProducesValidGo renders the example template that
// ships with the docs and parses the result as Go. The example is what projects
// copy, so it has to actually work.
func TestShippedExampleTemplateProducesValidGo(t *testing.T) {
	path := repoFile(t, "docs/features/event-model/templates/contract_gen.go.tmpl")
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading example template: %v", err)
	}

	out, err := RenderTemplate("contract_gen.go.tmpl", string(text), BuildTemplateData(genComponent()))
	if err != nil {
		t.Fatalf("rendering example template: %v", err)
	}

	if _, err := parser.ParseFile(token.NewFileSet(), "contract_gen.go", out, parser.AllErrors); err != nil {
		t.Fatalf("example template produced invalid Go: %v\n---\n%s", err, out)
	}

	for _, want := range []string{
		"// Code generated by archai plugin events gen. DO NOT EDIT.",
		"package billingevents", // from extra.go_package
		`KindBillingInvoiceIssued = "billing.invoice.issued"`,
		"FoldBillingOpenInvoicesPartitionKey",
		"var Exclusive = []string{", // ledger.entry.post is exclusive
		"var Foreign = []string{",   // ledger.entry.post is out of namespace
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("generated file should contain %q:\n%s", want, out)
		}
	}
}

// Without extra.go_package the template falls back to the component id, and the
// result must still parse.
func TestShippedExampleTemplateWithoutExtra(t *testing.T) {
	path := repoFile(t, "docs/features/event-model/templates/contract_gen.go.tmpl")
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading example template: %v", err)
	}

	comp := genComponent()
	comp.Extra = nil
	comp.Folds = nil
	comp.Types = nil

	out, err := RenderTemplate("contract_gen.go.tmpl", string(text), BuildTemplateData(comp))
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "contract_gen.go", out, parser.AllErrors); err != nil {
		t.Fatalf("invalid Go: %v\n---\n%s", err, out)
	}
	if !strings.Contains(string(out), "package billing") {
		t.Errorf("package should fall back to the component id:\n%s", out)
	}
}

// Rendering is deterministic: map-backed fields are sorted, so re-running the
// generator does not churn the diff.
func TestRenderIsDeterministic(t *testing.T) {
	path := repoFile(t, "docs/features/event-model/templates/contract_gen.go.tmpl")
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading example template: %v", err)
	}

	first, err := RenderTemplate("t", string(text), BuildTemplateData(genComponent()))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for i := 0; i < 20; i++ {
		again, err := RenderTemplate("t", string(text), BuildTemplateData(genComponent()))
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if string(again) != string(first) {
			t.Fatal("generated output is not stable across runs")
		}
	}
}
