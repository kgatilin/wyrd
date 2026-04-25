package d2

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/golang"
)

func findArchaiRootD2(tb testing.TB) string {
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

// BenchmarkD2Build_AllPackages measures the cost of rendering D2 text
// for every package in the archai project (both pub and internal views),
// which mirrors what the daemon does after a full reload before writers
// hit the filesystem.
func BenchmarkD2Build_AllPackages(b *testing.B) {
	root := findArchaiRootD2(b)
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
	if len(pkgs) == 0 {
		b.Fatal("no packages")
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, pkg := range pkgs {
			builder := newD2TextBuilder()
			_ = builder.Build(pkg, true)
			builder = newD2TextBuilder()
			_ = builder.Build(pkg, false)
		}
	}
}

// BenchmarkD2Build_Combined measures rendering of the combined diagram.
func BenchmarkD2Build_Combined(b *testing.B) {
	root := findArchaiRootD2(b)
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
		builder := newCombinedBuilder()
		_ = builder.Build(pkgs)
	}
}
