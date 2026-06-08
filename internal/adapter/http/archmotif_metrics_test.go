package http

import (
	"bytes"
	"encoding/json"
	"io"
	nethttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/serve"
)

func TestArchMotifMetricsBuildsPackageCouplingAndCycles(t *testing.T) {
	packages := []domain.PackageModel{
		{
			Path: "alpha",
			Name: "alpha",
			Functions: []domain.FunctionDef{{
				Name: "New",
				Returns: []domain.TypeRef{{
					Name:    "Service",
					Package: "beta",
				}},
			}},
			Dependencies: []domain.Dependency{
				dep("alpha", "New", "beta", "Service", domain.DependencyReturns),
			},
		},
		{
			Path: "beta",
			Name: "beta",
			Dependencies: []domain.Dependency{
				dep("beta", "Service", "gamma", "Client", domain.DependencyUses),
			},
		},
		{
			Path: "gamma",
			Name: "gamma",
			Dependencies: []domain.Dependency{
				dep("gamma", "Client", "alpha", "New", domain.DependencyUses),
			},
		},
	}

	metrics := buildArchMotifMetricsForPackages(packages)
	if metrics.Nodes != 3 {
		t.Fatalf("Nodes = %d, want 3", metrics.Nodes)
	}
	if metrics.Edges != 3 {
		t.Fatalf("Edges = %d, want 3", metrics.Edges)
	}
	if metrics.Components != 1 {
		t.Fatalf("Components = %d, want 1", metrics.Components)
	}
	if metrics.Acyclic {
		t.Fatalf("Acyclic = true, want false")
	}
	if len(metrics.Cycles) != 1 {
		t.Fatalf("Cycles = %d, want 1", len(metrics.Cycles))
	}
	if got := strings.Join(metrics.Cycles[0].Packages, ","); got != "alpha,beta,gamma" {
		t.Fatalf("cycle packages = %q, want alpha,beta,gamma", got)
	}
	alpha := findMetric(t, metrics.Packages, "alpha")
	if alpha.FanIn != 1 || alpha.FanOut != 1 || alpha.Instability != 0.5 {
		t.Fatalf("alpha metric = %+v, want fanIn=1 fanOut=1 instability=0.5", alpha)
	}
}

func TestArchMotifPackageGraphMLIncludesSemanticDoc(t *testing.T) {
	graph := buildArchMotifPackageGraph([]domain.PackageModel{{
		Path: "internal/eventstore",
		Name: "eventstore",
		Interfaces: []domain.InterfaceDef{{
			Name: "Log",
			Methods: []domain.MethodDef{{
				Name: "Read",
				Params: []domain.ParamDef{{
					Name: "fromSeq",
					Type: domain.TypeRef{Name: "uint64"},
				}},
				Returns: []domain.TypeRef{{
					Name:    "Record",
					Package: "internal/eventstore",
					IsSlice: true,
				}},
			}},
		}},
	}})
	var buf bytes.Buffer
	if err := writeArchMotifPackageGraphML(&buf, graph); err != nil {
		t.Fatalf("write graphml: %v", err)
	}
	text := buf.String()
	for _, want := range []string{
		`attr.name="doc"`,
		`<node id="internal/eventstore">`,
		`package eventstore`,
		`Read(fromSeq uint64) []internal/eventstore.Record`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("GraphML missing %q:\n%s", want, text)
		}
	}
}

func TestArchMotifMetricsAPI(t *testing.T) {
	ts, _, _ := newAPITestServer(t)
	resp, err := nethttp.Get(ts.URL + "/api/archmotif/metrics")
	if err != nil {
		t.Fatalf("GET metrics: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	var payload archMotifMetricsResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, body)
	}
	if payload.Schema != archMotifMetricsSchema {
		t.Fatalf("schema = %q, want %q", payload.Schema, archMotifMetricsSchema)
	}
	if payload.Scope != "packages" {
		t.Fatalf("scope = %q, want packages", payload.Scope)
	}
	if payload.Nodes == 0 || len(payload.Packages) == 0 {
		t.Fatalf("payload has no packages: %+v", payload)
	}
}

func TestArchMotifEmbedAPIWritesTextGraphWhenBinaryUnavailable(t *testing.T) {
	ts, _, root := newAPITestServer(t)
	t.Setenv("ARCHMOTIF_BIN", filepath.Join(root, "missing-archmotif"))

	resp, err := nethttp.Post(ts.URL+"/api/archmotif/embed", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST embed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	var payload archMotifEmbedResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, body)
	}
	if payload.Status != "unavailable" {
		t.Fatalf("status = %q, want unavailable; payload=%+v", payload.Status, payload)
	}
	graphPath := filepath.Join(root, ".arch", "archmotif", "packages-text.graphml")
	data, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatalf("read text graph: %v", err)
	}
	if !strings.Contains(string(data), `attr.name="doc"`) {
		t.Fatalf("text graph missing doc attribute:\n%s", data)
	}
}

func buildArchMotifMetricsForPackages(packages []domain.PackageModel) archMotifMetricsResponse {
	return buildArchMotifMetrics(serve.Snapshot{Packages: packages})
}

func dep(fromPkg, fromSym, toPkg, toSym string, kind domain.DependencyKind) domain.Dependency {
	return domain.Dependency{
		From: domain.SymbolRef{Package: fromPkg, Symbol: fromSym},
		To:   domain.SymbolRef{Package: toPkg, Symbol: toSym},
		Kind: kind,
	}
}

func findMetric(t *testing.T, metrics []archMotifPackageMetric, id string) archMotifPackageMetric {
	t.Helper()
	for _, metric := range metrics {
		if metric.ID == id {
			return metric
		}
	}
	t.Fatalf("metric %q not found in %+v", id, metrics)
	return archMotifPackageMetric{}
}
