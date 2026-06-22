package noop

import (
	"context"
	"testing"
)

func TestEmbedder_Deterministic(t *testing.T) {
	embedder := New()

	texts := []string{"hello world", "test input", "another text"}

	// Embed twice
	vecs1, err := embedder.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("first Embed: %v", err)
	}

	vecs2, err := embedder.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("second Embed: %v", err)
	}

	// Results should be identical
	if len(vecs1) != len(vecs2) {
		t.Fatalf("vector count mismatch: %d vs %d", len(vecs1), len(vecs2))
	}

	for i := range vecs1 {
		if len(vecs1[i]) != len(vecs2[i]) {
			t.Errorf("vector %d dimension mismatch: %d vs %d", i, len(vecs1[i]), len(vecs2[i]))
			continue
		}
		for j := range vecs1[i] {
			if vecs1[i][j] != vecs2[i][j] {
				t.Errorf("vector %d[%d] differs: %f vs %f", i, j, vecs1[i][j], vecs2[i][j])
			}
		}
	}
}

func TestEmbedder_DifferentTexts(t *testing.T) {
	embedder := New()

	vecs, err := embedder.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}

	// Different texts should produce different vectors
	different := false
	for i := range vecs[0] {
		if vecs[0][i] != vecs[1][i] {
			different = true
			break
		}
	}

	if !different {
		t.Error("different texts should produce different vectors")
	}
}

func TestEmbedder_Dim(t *testing.T) {
	embedder := New()
	if embedder.Dim() != DefaultDim {
		t.Errorf("expected dim %d, got %d", DefaultDim, embedder.Dim())
	}

	embedder2 := New(WithDim(128))
	if embedder2.Dim() != 128 {
		t.Errorf("expected dim 128, got %d", embedder2.Dim())
	}

	// Verify the vectors actually have the right dimension
	vecs, _ := embedder2.Embed(context.Background(), []string{"test"})
	if len(vecs[0]) != 128 {
		t.Errorf("expected vector dim 128, got %d", len(vecs[0]))
	}
}

func TestEmbedder_ID(t *testing.T) {
	embedder := New()
	if id := embedder.ID(); id != "noop" {
		t.Errorf("expected ID 'noop', got %q", id)
	}
}

func TestEmbedder_Empty(t *testing.T) {
	embedder := New()

	vecs, err := embedder.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if vecs != nil {
		t.Errorf("expected nil for empty input, got %v", vecs)
	}
}

func TestEmbedder_Normalized(t *testing.T) {
	embedder := New()

	vecs, err := embedder.Embed(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	// Check that the vector is approximately normalized (length ~= 1)
	var sumSq float32
	for _, v := range vecs[0] {
		sumSq += v * v
	}

	// Allow some floating point tolerance
	if sumSq < 0.99 || sumSq > 1.01 {
		t.Errorf("vector should be normalized, got length^2 = %f", sumSq)
	}
}

func TestEmbedder_ContextCancellation(t *testing.T) {
	embedder := New()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := embedder.Embed(ctx, []string{"test"})
	if err == nil {
		t.Error("expected error on cancelled context")
	}
}
