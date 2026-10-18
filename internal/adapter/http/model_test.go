package http

import (
	"context"
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/wyrd/internal/adapter/golang"
	"github.com/kgatilin/wyrd/internal/domain"
	"github.com/kgatilin/wyrd/internal/serve"
	"github.com/kgatilin/wyrd/internal/serve/modelstore"
	"github.com/kgatilin/wyrd/internal/treestamp"
)

func writeModelFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newModelTestServer(t *testing.T, root string) *httptest.Server {
	t.Helper()
	state := serve.NewState(root)
	if err := state.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	srv, err := NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func getModel(t *testing.T, ts *httptest.Server, tree, reader string) *nethttp.Response {
	t.Helper()
	q := url.Values{}
	if tree != "" {
		q.Set("tree", tree)
	}
	if reader != "" {
		q.Set("reader", reader)
	}
	resp, err := ts.Client().Get(ts.URL + "/api/model?" + q.Encode())
	if err != nil {
		t.Fatalf("GET /api/model: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func fingerprint(t *testing.T, root string) string {
	t.Helper()
	tree, err := treestamp.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	return tree.Fingerprint()
}

// TestModelAnswersTheTreeOnDiskNow: an edit made after the model was read is
// in the answer, packages come in import-path order, and a caller looking at
// another tree or reading with another reader is refused.
func TestModelAnswersTheTreeOnDiskNow(t *testing.T) {
	root := t.TempDir()
	writeModelFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.21\n")
	writeModelFile(t, filepath.Join(root, "app.go"), "package app\n")
	writeModelFile(t, filepath.Join(root, "store", "store.go"), "package store\n")
	ts := newModelTestServer(t, root)
	reader := modelstore.StampKey(golang.ReaderStamp)
	stale := fingerprint(t, root)

	writeModelFile(t, filepath.Join(root, "store", "store.go"), "package store\n\ntype Item struct{}\n")
	resp := getModel(t, ts, fingerprint(t, root), reader)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Packages []domain.PackageModel `json:"packages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Packages) != 2 || body.Packages[0].Path != "." || body.Packages[1].Path != "store" {
		t.Fatalf("packages = %v, want [. store]", paths(body.Packages))
	}
	if s := body.Packages[1].Structs; len(s) != 1 || s[0].Name != "Item" {
		t.Fatalf("store structs = %v, want the Item added after the load", s)
	}

	for _, tc := range []struct {
		name, tree, reader string
		status             int
	}{
		{"stale tree", stale, reader, nethttp.StatusConflict},
		{"other reader", fingerprint(t, root), "go-reader@v0", nethttp.StatusConflict},
		{"no tree", "", reader, nethttp.StatusBadRequest},
	} {
		if got := getModel(t, ts, tc.tree, tc.reader).StatusCode; got != tc.status {
			t.Errorf("%s: status = %d, want %d", tc.name, got, tc.status)
		}
	}
}

func paths(models []domain.PackageModel) []string {
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, m.Path)
	}
	return out
}
