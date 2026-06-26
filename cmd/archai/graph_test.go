package main

import (
	"reflect"
	"testing"
)

func toolByName(t *testing.T, name string) toolCmd {
	t.Helper()
	for _, tc := range graphTools() {
		if tc.name == name {
			return tc
		}
	}
	t.Fatalf("tool %q not found in graphTools()", name)
	return toolCmd{}
}

// argsFor parses the given flags into a tool subcommand and returns the
// JSON arguments object buildToolArgs would send to the daemon.
func argsFor(t *testing.T, name string, flags ...string) map[string]any {
	t.Helper()
	tc := toolByName(t, name)
	cmd := newGraphToolCmd(tc)
	if err := cmd.ParseFlags(flags); err != nil {
		t.Fatalf("ParseFlags(%v): %v", flags, err)
	}
	args, err := buildToolArgs(cmd, tc)
	if err != nil {
		t.Fatalf("buildToolArgs: %v", err)
	}
	return args
}

func TestBuildToolArgs_NestedFiltersAndSlices(t *testing.T) {
	got := argsFor(t, "search",
		"--query", "call edge order",
		"--k", "5",
		"--kind", "iface", "--kind", "func",
		"--package-prefix", "internal",
	)
	want := map[string]any{
		"query": "call edge order",
		"k":     5,
		"filters": map[string]any{
			"kinds":          []string{"iface", "func"},
			"package_prefix": "internal",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("search args mismatch:\n got=%#v\nwant=%#v", got, want)
	}
}

func TestBuildToolArgs_OmitsUnsetOptionalFlags(t *testing.T) {
	// trophic_layers has a required --package and an optional, default-true
	// --include-subpackages. Leaving the latter unset must omit it so the
	// daemon keeps its own default rather than receiving false.
	got := argsFor(t, "trophic_layers", "--package", "internal/sequence")
	want := map[string]any{"package": "internal/sequence"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("trophic_layers args mismatch:\n got=%#v\nwant=%#v", got, want)
	}
}

func TestBuildToolArgs_IncludeSubpackagesWhenSet(t *testing.T) {
	got := argsFor(t, "components", "--package", "internal", "--include-subpackages=false")
	want := map[string]any{"package": "internal", "include_subpackages": false}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("components args mismatch:\n got=%#v\nwant=%#v", got, want)
	}
}

func TestBuildToolArgs_KAutoCoercion(t *testing.T) {
	// An integer --k is sent as an int; "auto" stays a string. Both land
	// under the top-level "k" while --package nests under selector.
	asInt := argsFor(t, "spectral_cluster", "--package", "internal/adapter/mcp", "--k", "3")
	want := map[string]any{
		"selector": map[string]any{"package": "internal/adapter/mcp"},
		"k":        3,
	}
	if !reflect.DeepEqual(asInt, want) {
		t.Errorf("spectral_cluster --k 3 mismatch:\n got=%#v\nwant=%#v", asInt, want)
	}

	asAuto := argsFor(t, "spectral_cluster", "--k", "auto")
	if got := asAuto["k"]; got != "auto" {
		t.Errorf("expected k=\"auto\" (string), got %#v", got)
	}
}

func TestBuildToolArgs_RepeatableNodeIDs(t *testing.T) {
	got := argsFor(t, "expand", "--node", "pkg.A", "--node", "pkg.B", "--hops", "2")
	want := map[string]any{
		"node_ids": []string{"pkg.A", "pkg.B"},
		"hops":     2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expand args mismatch:\n got=%#v\nwant=%#v", got, want)
	}
}

func TestSetArgPath_CreatesNestedObjects(t *testing.T) {
	m := map[string]any{}
	setArgPath(m, "selector.package", "p")
	setArgPath(m, "selector.include_subpackages", true)
	setArgPath(m, "k", 4)
	want := map[string]any{
		"selector": map[string]any{"package": "p", "include_subpackages": true},
		"k":        4,
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("setArgPath mismatch:\n got=%#v\nwant=%#v", m, want)
	}
}
