package diff

import (
	"encoding/json"
	"strings"
	"testing"

	yamlv3 "gopkg.in/yaml.v3"
)

func TestFormatText_Empty(t *testing.T) {
	got := FormatText(&Diff{})
	if !strings.Contains(got, "No changes") {
		t.Errorf("expected 'No changes' for empty diff, got %q", got)
	}

	gotNil := FormatText(nil)
	if !strings.Contains(gotNil, "No changes") {
		t.Errorf("expected 'No changes' for nil diff, got %q", gotNil)
	}
}

func TestFormatText_Smoke(t *testing.T) {
	d := &Diff{Changes: []Change{
		{Op: OpAdd, Kind: KindStruct, Path: "pkg.Foo"},
		{Op: OpRemove, Kind: KindInterface, Path: "pkg.Bar"},
		{Op: OpChange, Kind: KindFunction, Path: "pkg.Baz"},
	}}
	got := FormatText(d)

	checks := []string{
		"+ added struct pkg.Foo",
		"- removed interface pkg.Bar",
		"~ changed function pkg.Baz",
		"1 added, 1 removed, 1 changed",
	}
	for _, s := range checks {
		if !strings.Contains(got, s) {
			t.Errorf("expected output to contain %q, got:\n%s", s, got)
		}
	}
}

func TestFormatYAML_Roundtrip(t *testing.T) {
	d := &Diff{Changes: []Change{
		{Op: OpAdd, Kind: KindStruct, Path: "pkg.Foo", After: map[string]any{"name": "Foo"}},
	}}
	got, err := FormatYAML(d)
	if err != nil {
		t.Fatalf("FormatYAML error: %v", err)
	}
	if !strings.Contains(got, "op: add") {
		t.Errorf("expected 'op: add' in yaml output, got:\n%s", got)
	}

	var parsed Diff
	if err := yamlv3.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("roundtrip unmarshal: %v", err)
	}
	if len(parsed.Changes) != 1 || parsed.Changes[0].Op != OpAdd {
		t.Errorf("roundtrip mismatch: %+v", parsed)
	}
}

func TestFormatJSON_Roundtrip(t *testing.T) {
	d := &Diff{Changes: []Change{
		{Op: OpRemove, Kind: KindInterface, Path: "pkg.Bar", Before: map[string]any{"name": "Bar"}},
	}}
	got, err := FormatJSON(d)
	if err != nil {
		t.Fatalf("FormatJSON error: %v", err)
	}
	if !strings.Contains(got, `"op": "remove"`) {
		t.Errorf("expected op=remove in json, got:\n%s", got)
	}

	var parsed Diff
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("json roundtrip: %v", err)
	}
	if len(parsed.Changes) != 1 || parsed.Changes[0].Op != OpRemove {
		t.Errorf("roundtrip mismatch: %+v", parsed)
	}
}
