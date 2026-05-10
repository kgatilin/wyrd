//go:build e2e

package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/d2"
	"github.com/kgatilin/archai/internal/adapter/golang"
	javaAdapter "github.com/kgatilin/archai/internal/adapter/java"
	yamlAdapter "github.com/kgatilin/archai/internal/adapter/yaml"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/service"
)

// TestJavaEndToEnd runs the analyzer JAR over the in-tree 3-package
// fixture and asserts the generate pipeline produces models for all
// three packages, then round-trips each through adapter/yaml.
//
// Gated behind both the e2e build tag and RUN_E2E=1 so the default `go
// test ./...` run on machines without a JVM stays unaffected. To run:
//
//	RUN_E2E=1 go test -tags e2e -run TestJavaEndToEnd ./tests/integration/...
func TestJavaEndToEnd(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to enable the Java end-to-end smoke test")
	}
	if _, err := exec.LookPath("java"); err != nil {
		t.Skip("`java` not on PATH; install JRE 21+ to run this test")
	}

	repoRoot := repoRootDir(t)
	jar := ensureAnalyzerJar(t, repoRoot)

	fixtureSrc := filepath.Join(repoRoot, "tests", "integration", "fixtures", "java", "src")
	output := t.TempDir()
	t.Logf("running analyzer over %s; output → %s", fixtureSrc, output)

	// Wiring matches what runGenerate in cmd/archai sets up for
	// --format=d2; we exercise the service directly so the test stays
	// hermetic (no need to `make build` first).
	svc := service.NewService(
		golang.NewReader(),
		d2.NewReader(),
		d2.NewWriter(),
		service.WithYAML(yamlAdapter.NewReader(), yamlAdapter.NewWriter()),
		service.WithJavaReader(javaAdapter.NewReader(jar)),
	)

	models, err := svc.ReadModels(context.Background(), []string{fixtureSrc})
	if err != nil {
		t.Fatalf("ReadModels: %v", err)
	}
	if len(models) < 3 {
		t.Fatalf("want ≥3 packages from 3-package fixture, got %d (%v)", len(models), packagePaths(models))
	}

	// Round-trip every model through adapter/yaml. Catches schema drift
	// between the in-memory model produced by the translator and what
	// the YAML reader expects on disk.
	yw := yamlAdapter.NewWriter()
	yr := yamlAdapter.NewReader()
	for _, m := range models {
		dir := filepath.Join(output, m.Path, ".arch")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		yamlPath := filepath.Join(dir, "pub.yaml")
		if err := yw.Write(context.Background(), m, domain.WriteOptions{
			OutputPath: yamlPath,
			PublicOnly: true,
		}); err != nil {
			t.Fatalf("yaml write %s: %v", m.Path, err)
		}
		got, err := yr.Read(context.Background(), []string{yamlPath})
		if err != nil {
			t.Fatalf("yaml read-back %s: %v", m.Path, err)
		}
		if len(got) == 0 {
			t.Fatalf("yaml round-trip yielded zero packages for %s", m.Path)
		}
	}
}

func ensureAnalyzerJar(t *testing.T, repoRoot string) string {
	t.Helper()
	jar := filepath.Join(repoRoot, "tools", "archai-java-analyzer", "target", "archai-java-analyzer.jar")
	if _, err := os.Stat(jar); err == nil {
		return jar
	}
	if _, err := exec.LookPath("mvn"); err != nil {
		t.Skip("analyzer JAR missing and `mvn` not on PATH; run `make java-analyzer` to bootstrap")
	}
	cmd := exec.Command("make", "java-analyzer-build")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("`make java-analyzer-build` failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(jar); err != nil {
		t.Fatalf("expected JAR at %s after build, got %v", jar, err)
	}
	return jar
}

func repoRootDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile = .../tests/integration/java_e2e_test.go → repo root is
	// two levels up.
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

func packagePaths(ms []domain.PackageModel) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Path)
	}
	return out
}
