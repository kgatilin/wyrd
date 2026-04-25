package golang

import (
	"context"
	"os"
	"reflect"
	"testing"
)

// TestReader_ParallelExtractionIsDeterministic asserts that repeated full
// extractions over the archai project yield byte-identical PackageModel
// slices. This is the regression guard for the parallel convertPackage
// fan-out: any non-determinism in worker scheduling, map iteration, or
// shared state should surface here as a DeepEqual mismatch.
func TestReader_ParallelExtractionIsDeterministic(t *testing.T) {
	root := findArchaiRoot(t)
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	ctx := context.Background()
	const runs = 4

	first, err := NewReader().Read(ctx, []string{"./internal/..."})
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("expected packages")
	}

	for i := 1; i < runs; i++ {
		again, err := NewReader().Read(ctx, []string{"./internal/..."})
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if len(again) != len(first) {
			t.Fatalf("run %d: package count drift: got %d want %d", i, len(again), len(first))
		}
		for j := range first {
			if first[j].Path != again[j].Path {
				t.Fatalf("run %d: package %d ordering drift: got %q want %q",
					i, j, again[j].Path, first[j].Path)
			}
			if !reflect.DeepEqual(first[j], again[j]) {
				t.Fatalf("run %d: package %q content drift", i, first[j].Path)
			}
		}
	}
}
