package eventmodel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadValidComponent(t *testing.T) {
	dir := t.TempDir()
	archDir := filepath.Join(dir, "billing", ".arch")
	if err := os.MkdirAll(archDir, 0755); err != nil {
		t.Fatal(err)
	}

	yaml := `version: 2
component: billing
owns: billing
description: invoice lifecycle

extra:
  partition: account
  transport: queue

inputs:
  - kind: billing.invoice.issue
    pattern: svc.*.billing.{account}.invoice.issue
    description: create an invoice
    exposure: [public_api]
    schema:
      type: object
      required: [Account]
      properties:
        Account: {type: string}

outputs:
  - kind: billing.invoice.issued
    pattern: svc.*.billing.{account}.invoice.issued
    schema: {$ref: '#/types/Invoice'}
  - kind: ledger.entry.post
    pattern: svc.*.ledger.{account}.entry.post

state_events:
  - kind: billing.invoice.issued
    pattern: svc.*.billing.{account}.invoice.issued
  - kind: billing.credit.applied
    pattern: svc.*.billing.{account}.credit.applied

state:
  type: object
  properties:
    Open: {type: array}

types:
  Invoice:
    type: object
    properties:
      Account: {type: string}
      Status: {type: string, enum: [open, paid, void]}
`
	if err := os.WriteFile(filepath.Join(archDir, "events.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	model, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(model.Components) != 1 {
		t.Fatalf("want 1 component, got %d", len(model.Components))
	}

	comp := model.Components["billing"]
	if comp == nil {
		t.Fatal("component 'billing' not found")
	}

	if comp.Version != SchemaVersion {
		t.Errorf("Version = %d, want %d", comp.Version, SchemaVersion)
	}
	if comp.Owns != "billing" {
		t.Errorf("Owns = %q, want 'billing'", comp.Owns)
	}
	if comp.Description != "invoice lifecycle" {
		t.Errorf("Description = %q, want 'invoice lifecycle'", comp.Description)
	}
	if len(comp.Inputs) != 1 {
		t.Errorf("Inputs: want 1, got %d", len(comp.Inputs))
	} else {
		r := comp.Inputs[0]
		if r.Kind != "billing.invoice.issue" {
			t.Errorf("Inputs[0].Kind = %q", r.Kind)
		}
		if r.Pattern != "svc.*.billing.{account}.invoice.issue" {
			t.Errorf("Inputs[0].Pattern = %q", r.Pattern)
		}
		if len(r.Exposure) != 1 || r.Exposure[0] != "public_api" {
			t.Errorf("Inputs[0].Exposure = %v", r.Exposure)
		}
	}
	if len(comp.Outputs) != 2 {
		t.Errorf("Outputs: want 2, got %d", len(comp.Outputs))
	}
	if len(comp.StateEvents) != 2 {
		t.Errorf("StateEvents: want 2, got %d", len(comp.StateEvents))
	}

	// The fold is derived, never declared: its read-set is inputs plus state
	// events, deduplicated, and it excludes the output-only kind.
	wantSubjects := []string{
		"svc.*.billing.{account}.invoice.issue",
		"svc.*.billing.{account}.invoice.issued",
		"svc.*.billing.{account}.credit.applied",
	}
	if !slotKeysEqual(comp.Subjects(), wantSubjects) {
		t.Errorf("Subjects() = %v, want %v", comp.Subjects(), wantSubjects)
	}
	wantConsumes := []string{
		"billing.invoice.issue",
		"billing.invoice.issued",
		"billing.credit.applied",
	}
	if !slotKeysEqual(comp.Consumes(), wantConsumes) {
		t.Errorf("Consumes() = %v, want %v", comp.Consumes(), wantConsumes)
	}
	if !slotKeysEqual(comp.PartitionKey, []string{"account"}) {
		t.Errorf("PartitionKey = %v, want [account]", comp.PartitionKey)
	}
	if comp.State.IsZero() {
		t.Error("State should be parsed")
	}

	if len(comp.Types) != 1 {
		t.Errorf("Types: want 1, got %d", len(comp.Types))
	}
	if _, ok := comp.Types["Invoice"]; !ok {
		t.Error("Types missing 'Invoice'")
	}
	if comp.Extra == nil || comp.Extra["partition"] != "account" {
		t.Errorf("Extra = %v", comp.Extra)
	}
}

// TestReadRejectsVersion1 pins the migration message: the v1 shape parses far
// enough to look plausible, so the reader has to name what changed rather than
// report an unknown field.
func TestReadRejectsVersion1(t *testing.T) {
	dir := t.TempDir()
	archDir := filepath.Join(dir, "billing", ".arch")
	if err := os.MkdirAll(archDir, 0755); err != nil {
		t.Fatal(err)
	}
	// A realistic version 1 document, carrying the fields that make it one.
	// Strict decoding rejects `receives` before any version check can run, so
	// reading the version first is what makes the message below reachable at
	// all — a minimal `version: 1` stub would pass either way.
	yaml := `version: 1
component: billing
owns: billing
receives:
  - kind: billing.invoice.issue
emits:
  - kind: billing.invoice.issued
folds:
  - kind: billing.invoice.issued
`
	if err := os.WriteFile(filepath.Join(archDir, "events.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Read(dir)
	if err == nil {
		t.Fatal("want an error for version 1")
	}
	for _, want := range []string{"version 1", "inputs/outputs/state_events"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestReadDuplicateComponentID(t *testing.T) {
	dir := t.TempDir()

	// Create two components with the same id.
	for _, subdir := range []string{"a", "b"} {
		archDir := filepath.Join(dir, subdir, ".arch")
		if err := os.MkdirAll(archDir, 0755); err != nil {
			t.Fatal(err)
		}
		yaml := `version: 2
component: billing
owns: billing
`
		if err := os.WriteFile(filepath.Join(archDir, "events.yaml"), []byte(yaml), 0644); err != nil {
			t.Fatal(err)
		}
	}

	_, err := Read(dir)
	if err == nil {
		t.Fatal("want error for duplicate component id")
	}
	if !strings.Contains(err.Error(), "duplicate component id") {
		t.Errorf("error = %q, want 'duplicate component id'", err.Error())
	}
}

func TestReadMissingRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "missing version",
			yaml: `component: billing`,
			want: "unsupported version 0",
		},
		{
			name: "unknown future version",
			yaml: `version: 99
component: billing`,
			want: "unsupported version 99",
		},
		{
			name: "missing component",
			yaml: `version: 2`,
			want: "missing required field 'component'",
		},
		{
			name: "missing input kind",
			yaml: `version: 2
component: billing
inputs:
  - pattern: svc.*.billing.{account}.foo`,
			want: "inputs[0]: missing required field 'kind'",
		},
		{
			name: "missing output kind",
			yaml: `version: 2
component: billing
outputs:
  - description: nameless`,
			want: "outputs[0]: missing required field 'kind'",
		},
		{
			name: "missing state event kind",
			yaml: `version: 2
component: billing
state_events:
  - description: nameless`,
			want: "state_events[0]: missing required field 'kind'",
		},
		{
			name: "invalid delivery",
			yaml: `version: 2
component: billing
inputs:
  - kind: billing.foo
    delivery: at-most-once`,
			want: "invalid delivery",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			archDir := filepath.Join(dir, ".arch")
			if err := os.MkdirAll(archDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(archDir, "events.yaml"), []byte(tc.yaml), 0644); err != nil {
				t.Fatal(err)
			}

			_, err := Read(dir)
			if err == nil {
				t.Fatal("want error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestReadStrictDecoding(t *testing.T) {
	// Unknown fields should cause an error.
	dir := t.TempDir()
	archDir := filepath.Join(dir, ".arch")
	if err := os.MkdirAll(archDir, 0755); err != nil {
		t.Fatal(err)
	}
	yaml := `version: 2
component: billing
unknown_field: value
`
	if err := os.WriteFile(filepath.Join(archDir, "events.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Read(dir)
	if err == nil {
		t.Fatal("want error for unknown field")
	}
	if !strings.Contains(err.Error(), "unknown_field") {
		t.Errorf("error = %q, want to mention 'unknown_field'", err.Error())
	}
}

func TestReadEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	model, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(model.Components) != 0 {
		t.Errorf("want 0 components, got %d", len(model.Components))
	}
}

func TestSlotTokens(t *testing.T) {
	cases := []struct {
		subject string
		want    []string
	}{
		{"", nil},
		{"svc.*.billing.invoice.>", nil},
		{"svc.*.billing.{account}.invoice.>", []string{"account"}},
		{"svc.*.billing.{region}.{location}.stock.{sku}.>", []string{"region", "location", "sku"}},
		{"svc.{a}.{b}.{c}.{d}.>", []string{"a", "b", "c", "d"}},
		{"{unclosed", nil},   // Malformed; only complete tokens are reported.
		{"{}", []string{""}}, // Empty slot still occupies a position.
	}
	for _, tc := range cases {
		got := SlotTokens(tc.subject)
		if !slotKeysEqual(got, tc.want) {
			t.Errorf("SlotTokens(%q) = %v, want %v", tc.subject, got, tc.want)
		}
	}
}

// TestSlotTokensOrderIsSignificant pins the property the partition-key rule
// rests on: the same slot names in a different order are a different key.
func TestSlotTokensOrderIsSignificant(t *testing.T) {
	a := SlotTokens("svc.{region}.{sku}.>")
	b := SlotTokens("svc.{sku}.{region}.>")
	if slotKeysEqual(a, b) {
		t.Errorf("SlotTokens must preserve order: %v == %v", a, b)
	}
}

func TestValidateSlotSyntax(t *testing.T) {
	valid := []string{
		"",
		"svc.*.billing.invoice.>",
		"svc.*.billing.{account}.invoice.>",
		"svc.*.warehouse.{region}.{location}.stock.{sku}.>",
		"{slot}",
		"a.{b}.c.{d}",
	}
	for _, s := range valid {
		if err := ValidateSlotSyntax(s); err != nil {
			t.Errorf("ValidateSlotSyntax(%q) = %v, want nil", s, err)
		}
	}

	invalid := []struct {
		subject string
		wantErr string
	}{
		{"{", "unclosed"},
		{"svc.{account.>", "unclosed"},
		{"{}", "empty slot"},
		{"svc.{}.foo", "empty slot"},
		{"svc.{a{b}}.>", "nested"},
		{"svc.}.foo", "unmatched"},
		{"svc.{{nested}}.>", "nested"},
	}
	for _, tc := range invalid {
		err := ValidateSlotSyntax(tc.subject)
		if err == nil {
			t.Errorf("ValidateSlotSyntax(%q) = nil, want error containing %q", tc.subject, tc.wantErr)
		} else if !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("ValidateSlotSyntax(%q) = %v, want error containing %q", tc.subject, err, tc.wantErr)
		}
	}
}

func TestKindHasPrefix(t *testing.T) {
	cases := []struct {
		kind string
		owns string
		want bool
	}{
		// Exact matches.
		{"billing", "billing", true},
		{"billing.invoice", "billing.invoice", true},
		{"", "", false}, // empty owns never matches

		// Prefix matches.
		{"billing.invoice.issued", "billing", true},
		{"billing.invoice.issued", "billing.invoice", true},
		{"billing.invoice", "billing", true},

		// Non-matches.
		{"billing", "billing.invoice", false}, // kind shorter than owns
		{"ledger.entry.posted", "billing", false},
		{"billingX", "billing", false},     // not a segment boundary
		{"billingX.foo", "billing", false}, // not a segment boundary

		// Edge cases.
		{"billing", "", false},
		{"", "billing", false},
	}
	for _, tc := range cases {
		got := kindHasPrefix(tc.kind, tc.owns)
		if got != tc.want {
			t.Errorf("kindHasPrefix(%q, %q) = %v, want %v", tc.kind, tc.owns, got, tc.want)
		}
	}
}

// TestReadRootNamedTestdata verifies that an explicitly supplied root whose
// basename is "testdata" is honored — the skip list applies only to directories
// encountered below the root, not to the root itself.
func TestReadRootNamedTestdata(t *testing.T) {
	// Create a directory named "testdata" with a component inside.
	parent := t.TempDir()
	testdataDir := filepath.Join(parent, "testdata")
	archDir := filepath.Join(testdataDir, "mycomp", ".arch")
	if err := os.MkdirAll(archDir, 0755); err != nil {
		t.Fatal(err)
	}

	yaml := `version: 2
component: mycomp
owns: mycomp
`
	if err := os.WriteFile(filepath.Join(archDir, "events.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	// Read with root pointing directly at the testdata directory.
	model, err := Read(testdataDir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(model.Components) != 1 {
		t.Fatalf("want 1 component when root is named 'testdata', got %d", len(model.Components))
	}
	if model.Components["mycomp"] == nil {
		t.Error("component 'mycomp' not found")
	}
}

// TestReadSkipsTestdataSubdirectory verifies that a subdirectory named
// "testdata" is still skipped when it's not the root.
func TestReadSkipsTestdataSubdirectory(t *testing.T) {
	dir := t.TempDir()

	// Create a component in a testdata subdirectory — should be skipped.
	testdataArch := filepath.Join(dir, "testdata", "hidden", ".arch")
	if err := os.MkdirAll(testdataArch, 0755); err != nil {
		t.Fatal(err)
	}
	yaml := `version: 2
component: hidden
owns: hidden
`
	if err := os.WriteFile(filepath.Join(testdataArch, "events.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a component outside testdata — should be found.
	visibleArch := filepath.Join(dir, "visible", ".arch")
	if err := os.MkdirAll(visibleArch, 0755); err != nil {
		t.Fatal(err)
	}
	yaml2 := `version: 2
component: visible
owns: visible
`
	if err := os.WriteFile(filepath.Join(visibleArch, "events.yaml"), []byte(yaml2), 0644); err != nil {
		t.Fatal(err)
	}

	model, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(model.Components) != 1 {
		t.Fatalf("want 1 component (testdata subdir should be skipped), got %d", len(model.Components))
	}
	if model.Components["visible"] == nil {
		t.Error("component 'visible' not found")
	}
	if model.Components["hidden"] != nil {
		t.Error("component 'hidden' should have been skipped (inside testdata subdir)")
	}
}

// nativeDoc is the smallest well-formed native declaration; the tests below
// care about where a file was found, not what it says.
func nativeDoc(component string) string {
	return "version: 2\ncomponent: " + component + "\nowns: " + component + "\n" +
		"outputs:\n  - kind: " + component + ".thing.happened\n    pattern: svc.{scope}." + component + ".happened\n"
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadSourcesFindsAFlatDirectoryOfDeclarations(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".event-schemas", "billing.events.yaml"), nativeDoc("billing"))
	writeFile(t, filepath.Join(root, ".event-schemas", "shipping.events.yaml"), nativeDoc("shipping"))
	// A file that names no format shares the directory and must be ignored
	// rather than parsed and failed: generated schema folders hold more than
	// declarations.
	writeFile(t, filepath.Join(root, ".event-schemas", "README.md"), "not a declaration\n")

	model, err := ReadSources(root, []Location{{Path: ".event-schemas"}})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(model.Components) != 2 {
		t.Fatalf("components = %d, want 2: %v", len(model.Components), model.Components)
	}
	for _, id := range []string{"billing", "shipping"} {
		if _, ok := model.Components[id]; !ok {
			t.Errorf("missing component %q", id)
		}
	}
}

func TestReadSourcesAddsToTheArchConventionRatherThanReplacingIt(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "billing", ".arch", "events.yaml"), nativeDoc("billing"))
	writeFile(t, filepath.Join(root, "schemas", "shipping.events.yaml"), nativeDoc("shipping"))

	model, err := ReadSources(root, []Location{{Path: "schemas"}})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(model.Components) != 2 {
		t.Fatalf("components = %d, want both the conventional and the declared one", len(model.Components))
	}
}

func TestReadSourcesIncludeNarrowsAndForcedFormatOverridesTheName(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "schemas", "billing.events.yaml"), nativeDoc("billing"))
	writeFile(t, filepath.Join(root, "schemas", "shipping.events.yaml"), nativeDoc("shipping"))

	model, err := ReadSources(root, []Location{{Path: "schemas", Include: "billing.*"}})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(model.Components) != 1 || model.Components["billing"] == nil {
		t.Fatalf("components = %v, want only billing", model.Components)
	}

	// A directory whose names say nothing about the format still reads when
	// the project states it.
	other := t.TempDir()
	writeFile(t, filepath.Join(other, "schemas", "billing.yaml"), nativeDoc("billing"))
	model, err = ReadSources(other, []Location{{Path: "schemas", Format: FormatEvents}})
	if err != nil {
		t.Fatalf("read with forced format: %v", err)
	}
	if model.Components["billing"] == nil {
		t.Fatalf("components = %v, want billing", model.Components)
	}
}

func TestReadSourcesScansNestedDirectories(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "schemas", "billing", "billing.events.yaml"), nativeDoc("billing"))

	model, err := ReadSources(root, []Location{{Path: "schemas"}})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if model.Components["billing"] == nil {
		t.Fatalf("components = %v, want billing found below the source directory", model.Components)
	}
}

func TestReadSourcesRejectsADirectoryThatIsNotThere(t *testing.T) {
	root := t.TempDir()
	_, err := ReadSources(root, []Location{{Path: "schemas"}})
	if err == nil {
		t.Fatal("want an error: a source is a statement about this project, and a typo must not read as zero components")
	}
	if !strings.Contains(err.Error(), "schemas") {
		t.Errorf("error = %v, want it to name the source", err)
	}
}

func TestReadSourcesDoesNotParseAFileTwice(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "billing", ".arch", "events.yaml"), nativeDoc("billing"))

	// The source covers the same file the convention already found. Reading it
	// twice would report it as a duplicate component id against itself.
	model, err := ReadSources(root, []Location{{Path: "billing"}})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(model.Components) != 1 {
		t.Fatalf("components = %d, want 1", len(model.Components))
	}
}
