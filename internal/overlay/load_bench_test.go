package overlay

import (
	"os"
	"path/filepath"
	"testing"
)

func findArchaiRootOverlay(tb testing.TB) string {
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

// BenchmarkLoad_Archai measures the cost of parsing the root archai.yaml.
// This is the path the daemon hits whenever archai.yaml changes.
func BenchmarkLoad_Archai(b *testing.B) {
	root := findArchaiRootOverlay(b)
	cfgPath := filepath.Join(root, "archai.yaml")
	if _, err := os.Stat(cfgPath); err != nil {
		b.Skipf("archai.yaml not present at %s", cfgPath)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Load(cfgPath); err != nil {
			b.Fatalf("load: %v", err)
		}
	}
}

// BenchmarkLoadComposed_FragmentScan measures the directory-walk cost of
// finding package overlay fragments under the archai project. This
// dominates LoadComposed when fragments are sparse: a fresh walk per
// reload visits every directory under root looking for .arch/overlay.yaml.
//
// The benchmark probes the walk in isolation by calling
// findPackageOverlayFragments — this is intentionally an internal
// (white-box) bench so we measure the hot path without paying YAML
// parse overhead.
func BenchmarkLoadComposed_FragmentScan(b *testing.B) {
	root := findArchaiRootOverlay(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := findPackageOverlayFragments(root); err != nil {
			b.Fatalf("scan: %v", err)
		}
	}
}
