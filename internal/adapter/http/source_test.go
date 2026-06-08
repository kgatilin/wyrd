package http

import (
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/serve"
)

func TestSourceFile_RendersRelativeFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "event"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "event", "event.go"), []byte("package event\nconst x = \"<tag>\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := newSourceTestServer(t, root)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/source?file=" + url.QueryEscape("internal/event/event.go"))
	if err != nil {
		t.Fatalf("GET /source: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"internal/event/event.go",
		`<td class="source-no">1</td>`,
		"package event",
		`&lt;tag&gt;`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestSourceFileJSON_ReturnsContent(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "event"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "event", "event.go"), []byte("package event\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := newSourceTestServer(t, root)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/source?file=" + url.QueryEscape("internal/event/event.go"))
	if err != nil {
		t.Fatalf("GET /api/source: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var payload struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Path != "internal/event/event.go" {
		t.Errorf("Path = %q, want internal/event/event.go", payload.Path)
	}
	if payload.Content != "package event\n" {
		t.Errorf("Content = %q, want package event newline", payload.Content)
	}
}

func TestSourceFileJSON_ResolvesNestedPackagePathSuffix(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "services", "billing", "internal", "eventstore")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "eventstore.go"), []byte("package eventstore\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := newSourceTestServer(t, root)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/source?file=" + url.QueryEscape("internal/eventstore/eventstore.go"))
	if err != nil {
		t.Fatalf("GET /api/source: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, string(body))
	}
	var payload struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Path != "services/billing/internal/eventstore/eventstore.go" {
		t.Errorf("Path = %q, want nested relative path", payload.Path)
	}
	if payload.Content != "package eventstore\n" {
		t.Errorf("Content = %q, want package eventstore newline", payload.Content)
	}
}

func TestSourceFile_RejectsTraversal(t *testing.T) {
	root := t.TempDir()
	ts := newSourceTestServer(t, root)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/source?file=" + url.QueryEscape("../secret.go"))
	if err != nil {
		t.Fatalf("GET /source: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func newSourceTestServer(t *testing.T, root string) *httptest.Server {
	t.Helper()
	state := serve.NewState(root)
	srv, err := NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	return httptest.NewServer(mux)
}
