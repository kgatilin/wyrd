package http

import (
	"context"
	"io"
	nethttp "net/http"
	nethttptest "net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
	"github.com/kgatilin/archai/internal/serve"
)

func TestDefaultLiteral_Primitives(t *testing.T) {
	cases := []struct {
		name string
		in   domain.TypeRef
		want string
	}{
		{"string", domain.TypeRef{Name: "string"}, `""`},
		{"bool", domain.TypeRef{Name: "bool"}, "false"},
		{"int", domain.TypeRef{Name: "int"}, "0"},
		{"float64", domain.TypeRef{Name: "float64"}, "0"},
		{"error", domain.TypeRef{Name: "error"}, "nil"},
		{"any", domain.TypeRef{Name: "any"}, "nil"},
		{"pointer", domain.TypeRef{Name: "Widget", IsPointer: true}, "nil"},
		{"slice of string", domain.TypeRef{Name: "string", IsSlice: true}, `[]string{}`},
		{
			"map string->int",
			domain.TypeRef{
				IsMap:     true,
				KeyType:   &domain.TypeRef{Name: "string"},
				ValueType: &domain.TypeRef{Name: "int"},
			},
			"map[string]int{}",
		},
		{"named local struct", domain.TypeRef{Name: "Widget"}, "Widget{}"},
		{"external type", domain.TypeRef{Name: "Context", Package: "context"}, "context.Context{}"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := defaultLiteral(tc.in)
			if got != tc.want {
				t.Fatalf("defaultLiteral(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSynthesizeExample_Struct(t *testing.T) {
	s := domain.StructDef{
		Name: "ServerConfig",
		Fields: []domain.FieldDef{
			{Name: "Host", Type: domain.TypeRef{Name: "string"}},
			{Name: "Port", Type: domain.TypeRef{Name: "int"}},
			{Name: "Allow", Type: domain.TypeRef{Name: "string", IsSlice: true}},
		},
	}
	got := synthesizeExample(s)
	for _, want := range []string{
		"ServerConfig{",
		`Host:  ""`,
		"Port:  0",
		"Allow: []string{}",
		"}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("example missing %q:\n%s", want, got)
		}
	}
}

func TestSynthesizeExample_EmptyStruct(t *testing.T) {
	s := domain.StructDef{Name: "Empty"}
	got := synthesizeExample(s)
	if got != "Empty{}" {
		t.Fatalf("got %q, want Empty{}", got)
	}
}

func TestCollectConfigs_OverlayResolvesAndMissing(t *testing.T) {
	pkgs := []domain.PackageModel{
		{
			Path: "internal/config",
			Structs: []domain.StructDef{
				{
					Name:       "App",
					IsExported: true,
					Fields: []domain.FieldDef{
						{Name: "Addr", Type: domain.TypeRef{Name: "string"}, IsExported: true},
					},
				},
			},
		},
	}
	cfg := &overlay.Config{
		Module:  "example.com/app",
		Configs: []string{"example.com/app/internal/config.App", "example.com/app/internal/config.Missing"},
	}
	got, missing := collectConfigs(pkgs, cfg)
	if len(got) != 1 || got[0].Name != "App" {
		t.Fatalf("expected one App entry, got %+v", got)
	}
	if got[0].Example == "" {
		t.Fatalf("expected synthesized example, got empty")
	}
	if len(missing) != 1 || missing[0] != "example.com/app/internal/config.Missing" {
		t.Fatalf("expected one missing entry, got %+v", missing)
	}
}

func TestCollectConfigs_NoOverlay(t *testing.T) {
	pkgs := []domain.PackageModel{{Path: "internal/foo"}}
	got, missing := collectConfigs(pkgs, nil)
	if len(got) != 0 || len(missing) != 0 {
		t.Fatalf("expected empty result, got configs=%+v missing=%+v", got, missing)
	}
}

func TestHandleConfigs_EmptyState(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/configs")
	if err != nil {
		t.Fatalf("GET /configs: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, "no overlay") {
		t.Errorf("expected empty-state message, got: %s", truncate(s, 400))
	}
}

func TestHandleConfigs_WithOverlay(t *testing.T) {
	ts := newConfigsFixtureServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/configs")
	if err != nil {
		t.Fatalf("GET /configs: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	for _, want := range []string{
		"ServerConfig",                      // config type name
		"Host",                              // field
		"Port",                              // field
		`ServerConfig{`,                     // example heading
		"/types/internal/conf.ServerConfig", // type detail backlink
	} {
		if !strings.Contains(s, want) {
			t.Errorf("/configs page missing %q: %s", want, truncate(s, 500))
		}
	}
}

// newConfigsFixtureServer builds a fixture with a struct that the
// overlay declares as a config so collectConfigs has something to
// resolve against.
func newConfigsFixtureServer(t *testing.T) *nethttptest.Server {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/cfgfix\n\ngo 1.21\n")
	mustWrite(t, filepath.Join(root, "internal", "conf", "config.go"), `package conf

// ServerConfig is the fixture config type.
type ServerConfig struct {
	Host string
	Port int
}
`)
	mustWrite(t, filepath.Join(root, "archai.yaml"), `module: example.com/cfgfix
layers:
  domain:
    - "internal/..."
configs:
  - example.com/cfgfix/internal/conf.ServerConfig
`)

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
	return nethttptest.NewServer(mux)
}
