package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReadFile_ReadsWithinRoot(t *testing.T) {
	state := loadFakeState(t)
	res, rpcErr := Dispatch(state, "read_file", mustJSON(t, map[string]any{"path": "alpha/alpha.go"}))
	if rpcErr != nil {
		t.Fatalf("rpc error: %+v", rpcErr)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content[0].Text)
	}
	if !strings.Contains(res.Content[0].Text, "type Service interface") {
		t.Fatalf("file content missing expected source, got: %q", res.Content[0].Text)
	}
}

func TestReadFile_RejectsTraversal(t *testing.T) {
	state := loadFakeState(t)
	for _, p := range []string{"../../../etc/passwd", "/etc/passwd"} {
		res, rpcErr := Dispatch(state, "read_file", mustJSON(t, map[string]any{"path": p}))
		if rpcErr != nil {
			t.Fatalf("rpc error: %+v", rpcErr)
		}
		if !res.IsError {
			t.Fatalf("expected error for path outside root %q, got: %q", p, res.Content[0].Text)
		}
	}
}

func TestReadFile_MissingFile(t *testing.T) {
	state := loadFakeState(t)
	res, _ := Dispatch(state, "read_file", mustJSON(t, map[string]any{"path": "nope/missing.go"}))
	if !res.IsError {
		t.Fatalf("expected error for missing file, got: %q", res.Content[0].Text)
	}
}

func TestSearchFiles_FindsSubstring(t *testing.T) {
	state := loadFakeState(t)
	res, rpcErr := Dispatch(state, "search_files", mustJSON(t, map[string]any{"query": "interface"}))
	if rpcErr != nil {
		t.Fatalf("rpc error: %+v", rpcErr)
	}
	var out struct {
		Count   int `json:"count"`
		Matches []struct {
			Path string `json:"path"`
			Line int    `json:"line"`
			Text string `json:"text"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Count == 0 {
		t.Fatalf("expected at least one match for 'interface'")
	}
	found := false
	for _, m := range out.Matches {
		if strings.Contains(m.Path, "alpha.go") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a match in alpha.go, got %+v", out.Matches)
	}
}

func TestSearchFiles_GlobFilter(t *testing.T) {
	state := loadFakeState(t)
	// go.mod contains "module" but a *.go glob must exclude it.
	res, _ := Dispatch(state, "search_files", mustJSON(t, map[string]any{
		"query":     "module",
		"path_glob": "*.go",
	}))
	var out struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal([]byte(res.Content[0].Text), &out)
	if out.Count != 0 {
		t.Fatalf("expected 0 matches for 'module' under *.go, got %d", out.Count)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
