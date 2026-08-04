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

	yaml := `version: 1
component: billing
owns: billing
description: invoice lifecycle

extra:
  partition: account
  transport: queue

receives:
  - kind: billing.invoice.issue
    role: action
    description: create an invoice
    exposure: [public_api]
    schema:
      type: object
      required: [Account]
      properties:
        Account: {type: string}

emits:
  - kind: billing.invoice.issued
    role: fact
    schema: {$ref: '#/vocab/Invoice'}
  - kind: ledger.entry.post
    role: action

folds:
  - name: billing.open-invoices
    pattern: billing.invoice.>
    state:
      type: object
      properties:
        Open: {type: array}

vocab:
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

	if comp.Version != 1 {
		t.Errorf("Version = %d, want 1", comp.Version)
	}
	if comp.Owns != "billing" {
		t.Errorf("Owns = %q, want 'billing'", comp.Owns)
	}
	if comp.Description != "invoice lifecycle" {
		t.Errorf("Description = %q, want 'invoice lifecycle'", comp.Description)
	}
	if len(comp.Receives) != 1 {
		t.Errorf("Receives: want 1, got %d", len(comp.Receives))
	} else {
		r := comp.Receives[0]
		if r.Kind != "billing.invoice.issue" {
			t.Errorf("Receives[0].Kind = %q", r.Kind)
		}
		if r.Role != RoleAction {
			t.Errorf("Receives[0].Role = %q", r.Role)
		}
		if len(r.Exposure) != 1 || r.Exposure[0] != "public_api" {
			t.Errorf("Receives[0].Exposure = %v", r.Exposure)
		}
	}
	if len(comp.Emits) != 2 {
		t.Errorf("Emits: want 2, got %d", len(comp.Emits))
	}
	if len(comp.Folds) != 1 {
		t.Errorf("Folds: want 1, got %d", len(comp.Folds))
	} else {
		f := comp.Folds[0]
		if f.Name != "billing.open-invoices" {
			t.Errorf("Folds[0].Name = %q", f.Name)
		}
		if f.Pattern != "billing.invoice.>" {
			t.Errorf("Folds[0].Pattern = %q", f.Pattern)
		}
	}
	if len(comp.Vocab) != 1 {
		t.Errorf("Vocab: want 1, got %d", len(comp.Vocab))
	}
	if _, ok := comp.Vocab["Invoice"]; !ok {
		t.Error("Vocab missing 'Invoice'")
	}
	if comp.Extra == nil || comp.Extra["partition"] != "account" {
		t.Errorf("Extra = %v", comp.Extra)
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
		yaml := `version: 1
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
			name: "missing component",
			yaml: `version: 1`,
			want: "missing required field 'component'",
		},
		{
			name: "missing slot kind",
			yaml: `version: 1
component: billing
receives:
  - role: action`,
			want: "missing required field 'kind'",
		},
		{
			name: "invalid role",
			yaml: `version: 1
component: billing
receives:
  - kind: billing.foo
    role: bogus`,
			want: "invalid role",
		},
		{
			name: "missing fold name",
			yaml: `version: 1
component: billing
folds:
  - pattern: billing.>`,
			want: "missing required field 'name'",
		},
		{
			name: "missing fold pattern",
			yaml: `version: 1
component: billing
folds:
  - name: foo`,
			want: "missing required field 'pattern'",
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
	yaml := `version: 1
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

func TestNamespace(t *testing.T) {
	cases := []struct {
		kind string
		want string
	}{
		{"billing.invoice.issued", "billing"},
		{"billing", "billing"},
		{"a.b.c.d.e", "a"},
		{"", ""},
	}
	for _, tc := range cases {
		got := namespace(tc.kind)
		if got != tc.want {
			t.Errorf("namespace(%q) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}
