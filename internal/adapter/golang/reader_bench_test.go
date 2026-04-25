package golang

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// findArchaiRoot walks up from the test's CWD looking for go.mod with the
// archai module path, so the benchmark can self-host on the archai source
// tree regardless of where `go test` is invoked.
func findArchaiRoot(tb testing.TB) string {
	tb.Helper()
	dir, err := os.Getwd()
	if err != nil {
		tb.Fatalf("getwd: %v", err)
	}
	for {
		gomod := filepath.Join(dir, "go.mod")
		if data, err := os.ReadFile(gomod); err == nil {
			s := string(data)
			if strContains(s, "module github.com/kgatilin/archai") {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatal("could not locate archai module root from cwd")
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

// BenchmarkReader_ArchaiAll measures the end-to-end Read time for the
// whole archai project under ./.... This is the single largest hot path
// the daemon hits on a cold load and the natural reference workload for
// the parallel-extraction work in #58.
func BenchmarkReader_ArchaiAll(b *testing.B) {
	root := findArchaiRoot(b)
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

// BenchmarkReader_ArchaiInternal exercises a smaller scope (./internal/...)
// to get a finer-grained number that excludes cmd/ and tests/.
func BenchmarkReader_ArchaiInternal(b *testing.B) {
	root := findArchaiRoot(b)
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
