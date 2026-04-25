package diff

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/golang"
	"github.com/kgatilin/archai/internal/domain"
)

func findArchaiRootDiff(tb testing.TB) string {
	tb.Helper()
	dir, err := os.Getwd()
	if err != nil {
		tb.Fatalf("getwd: %v", err)
	}
	const want = "module github.com/kgatilin/archai"
	for {
		gomod := filepath.Join(dir, "go.mod")
		if data, err := os.ReadFile(gomod); err == nil {
			s := string(data)
			for i := 0; i+len(want) <= len(s); i++ {
				if s[i:i+len(want)] == want {
					return dir
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatal("could not locate archai module root")
		}
		dir = parent
	}
}

// BenchmarkCompute_NoChange compares the archai model against itself —
// the worst case for the diff path because every package, symbol, and
// dependency is visited (no early-out) but no Changes are emitted.
func BenchmarkCompute_NoChange(b *testing.B) {
	root := findArchaiRootDiff(b)
	prev, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		b.Fatalf("chdir: %v", err)
	}
	b.Cleanup(func() { _ = os.Chdir(prev) })

	r := golang.NewReader()
	pkgs, err := r.Read(context.Background(), []string{"./..."})
	if err != nil {
		b.Fatalf("read: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Compute(pkgs, pkgs)
	}
}

// BenchmarkCompute_PackageRemoved drops one package from one side; this
// hits the "remove whole package" code path while keeping every other
// package on both sides for the full structural comparison.
func BenchmarkCompute_PackageRemoved(b *testing.B) {
	root := findArchaiRootDiff(b)
	prev, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		b.Fatalf("chdir: %v", err)
	}
	b.Cleanup(func() { _ = os.Chdir(prev) })

	r := golang.NewReader()
	pkgs, err := r.Read(context.Background(), []string{"./..."})
	if err != nil {
		b.Fatalf("read: %v", err)
	}
	if len(pkgs) < 2 {
		b.Fatalf("need >= 2 packages, got %d", len(pkgs))
	}
	target := make([]domain.PackageModel, 0, len(pkgs)-1)
	target = append(target, pkgs[1:]...)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Compute(pkgs, target)
	}
}
