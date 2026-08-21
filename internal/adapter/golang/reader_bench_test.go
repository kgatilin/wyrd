package golang

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// findWyrdRoot walks up from the test's CWD looking for go.mod with the
// wyrd module path, so the benchmark can self-host on the wyrd source
// tree regardless of where `go test` is invoked.
func findWyrdRoot(tb testing.TB) string {
	tb.Helper()
	dir, err := os.Getwd()
	if err != nil {
		tb.Fatalf("getwd: %v", err)
	}
	for {
		gomod := filepath.Join(dir, "go.mod")
		if data, err := os.ReadFile(gomod); err == nil {
			s := string(data)
			if strContains(s, "module github.com/kgatilin/wyrd") {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatal("could not locate wyrd module root from cwd")
		}
		dir = parent
	}
}

func strContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// BenchmarkReader_WyrdAll measures the end-to-end Read time for the
// whole wyrd project under ./.... This is the single largest hot path
// the daemon hits on a cold load and the natural reference workload for
// the parallel-extraction work in #58.
func BenchmarkReader_WyrdAll(b *testing.B) {
	root := findWyrdRoot(b)
	prev, err := os.Getwd()
	if err != nil {
		b.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		b.Fatalf("chdir: %v", err)
	}
	b.Cleanup(func() { _ = os.Chdir(prev) })

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := NewReader()
		pkgs, err := r.Read(ctx, []string{"./..."})
		if err != nil {
			b.Fatalf("read: %v", err)
		}
		if len(pkgs) == 0 {
			b.Fatal("expected at least one package")
		}
	}
}

// BenchmarkReader_WyrdInternal exercises a smaller scope (./internal/...)
// to get a finer-grained number that excludes cmd/ and tests/.
func BenchmarkReader_WyrdInternal(b *testing.B) {
	root := findWyrdRoot(b)
	prev, err := os.Getwd()
	if err != nil {
		b.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		b.Fatalf("chdir: %v", err)
	}
	b.Cleanup(func() { _ = os.Chdir(prev) })

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := NewReader()
		pkgs, err := r.Read(ctx, []string{"./internal/..."})
		if err != nil {
			b.Fatalf("read: %v", err)
		}
		if len(pkgs) == 0 {
			b.Fatal("expected at least one package")
		}
	}
}
