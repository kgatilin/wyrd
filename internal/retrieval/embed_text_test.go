package retrieval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

func TestEmbedText_Assembly(t *testing.T) {
	// Create a temp file with source code
	tmpDir := t.TempDir()
	srcContent := `func NewState(cfg Config) *State {
	return &State{Config: cfg}
}`
	srcPath := filepath.Join(tmpDir, "internal", "serve", "state.go")
	if err := os.MkdirAll(filepath.Dir(srcPath), 0755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	if err := os.WriteFile(srcPath, []byte(srcContent), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	node := Node{
		ID:        "internal/serve.NewState",
		Kind:      "func",
		Package:   "internal/serve",
		Name:      "NewState",
		Signature: "NewState(cfg Config) *State",
		Doc:       "NewState creates a new State instance.",
		Span: domain.Span{
			File:      filepath.Join("internal", "serve", "state.go"),
			StartByte: 0,
			EndByte:   len(srcContent),
			StartLine: 1,
			EndLine:   3,
		},
	}

	text, err := EmbedText(node, tmpDir)
	if err != nil {
		t.Fatalf("EmbedText: %v", err)
	}

	// Check that all components are present
	if !strings.Contains(text, "package serve") {
		t.Error("embed text should contain package header")
	}
	if !strings.Contains(text, "NewState(cfg Config) *State") {
		t.Error("embed text should contain signature")
	}
	if !strings.Contains(text, "creates a new State instance") {
		t.Error("embed text should contain doc")
	}
	if !strings.Contains(text, "return &State{Config: cfg}") {
		t.Error("embed text should contain source body")
	}
}

func TestEmbedText_Truncation(t *testing.T) {
	// Create a very long source body
	tmpDir := t.TempDir()
	longBody := strings.Repeat("// comment line\n", 500)
	srcPath := filepath.Join(tmpDir, "pkg", "long.go")
	if err := os.MkdirAll(filepath.Dir(srcPath), 0755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	if err := os.WriteFile(srcPath, []byte(longBody), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	node := Node{
		ID:        "pkg.Long",
		Kind:      "func",
		Package:   "pkg",
		Signature: "Long()",
		Span: domain.Span{
			File:      filepath.Join("pkg", "long.go"),
			StartByte: 0,
			EndByte:   len(longBody),
			StartLine: 1,
			EndLine:   500,
		},
	}

	text, err := EmbedText(node, tmpDir)
	if err != nil {
		t.Fatalf("EmbedText: %v", err)
	}

	if len(text) > EmbedTextBudget {
		t.Errorf("embed text should be truncated to %d chars, got %d", EmbedTextBudget, len(text))
	}
}

func TestEmbedText_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()

	node := Node{
		ID:        "pkg.Missing",
		Kind:      "func",
		Package:   "pkg",
		Signature: "Missing()",
		Span: domain.Span{
			File:      "nonexistent.go",
			StartByte: 0,
			EndByte:   100,
			StartLine: 1,
			EndLine:   5,
		},
	}

	// Should not error - body is just omitted
	text, err := EmbedText(node, tmpDir)
	if err != nil {
		t.Fatalf("EmbedText should not error on missing file: %v", err)
	}

	// Should still have signature
	if !strings.Contains(text, "Missing()") {
		t.Error("embed text should contain signature even without body")
	}
}

func TestEmbedText_InvalidSpan(t *testing.T) {
	tmpDir := t.TempDir()

	node := Node{
		ID:        "pkg.NoSpan",
		Kind:      "func",
		Package:   "pkg",
		Signature: "NoSpan()",
		Span:      domain.Span{}, // Invalid span
	}

	text, err := EmbedText(node, tmpDir)
	if err != nil {
		t.Fatalf("EmbedText: %v", err)
	}

	// Should have signature but no body
	if !strings.Contains(text, "NoSpan()") {
		t.Error("embed text should contain signature")
	}
}

func TestContentHash_Deterministic(t *testing.T) {
	tmpDir := t.TempDir()
	srcContent := `func Foo() {}`
	srcPath := filepath.Join(tmpDir, "pkg", "foo.go")
	if err := os.MkdirAll(filepath.Dir(srcPath), 0755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	if err := os.WriteFile(srcPath, []byte(srcContent), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	node := Node{
		ID:        "pkg.Foo",
		Kind:      "func",
		Package:   "pkg",
		Signature: "Foo()",
		Span: domain.Span{
			File:      filepath.Join("pkg", "foo.go"),
			StartByte: 0,
			EndByte:   len(srcContent),
			StartLine: 1,
			EndLine:   1,
		},
	}

	hash1, err := ContentHash(node, tmpDir)
	if err != nil {
		t.Fatalf("ContentHash: %v", err)
	}

	hash2, err := ContentHash(node, tmpDir)
	if err != nil {
		t.Fatalf("ContentHash: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("ContentHash should be deterministic: %s != %s", hash1, hash2)
	}

	// Hash should be a valid hex string (64 chars for SHA-256)
	if len(hash1) != 64 {
		t.Errorf("expected 64 char hex hash, got %d chars", len(hash1))
	}
}

func TestContentHash_ChangesWithContent(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "pkg", "foo.go")
	if err := os.MkdirAll(filepath.Dir(srcPath), 0755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}

	// First version
	srcContent1 := `func Foo() {}`
	if err := os.WriteFile(srcPath, []byte(srcContent1), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	node := Node{
		ID:        "pkg.Foo",
		Kind:      "func",
		Package:   "pkg",
		Signature: "Foo()",
		Span: domain.Span{
			File:      filepath.Join("pkg", "foo.go"),
			StartByte: 0,
			EndByte:   len(srcContent1),
			StartLine: 1,
			EndLine:   1,
		},
	}

	hash1, err := ContentHash(node, tmpDir)
	if err != nil {
		t.Fatalf("ContentHash: %v", err)
	}

	// Second version with different content
	srcContent2 := `func Foo() { return }`
	if err := os.WriteFile(srcPath, []byte(srcContent2), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	node.Span.EndByte = len(srcContent2)
	hash2, err := ContentHash(node, tmpDir)
	if err != nil {
		t.Fatalf("ContentHash: %v", err)
	}

	if hash1 == hash2 {
		t.Error("ContentHash should change when content changes")
	}
}

func TestTruncateToSignatureHeader(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		signature string
		budget    int
		wantLen   int
	}{
		{
			name:      "fits within budget",
			text:      "short text",
			signature: "",
			budget:    100,
			wantLen:   10,
		},
		{
			name:      "truncated",
			text:      "this is a longer text that exceeds the budget",
			signature: "",
			budget:    20,
			wantLen:   20,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := truncateToSignatureHeader(tc.text, tc.signature, tc.budget)
			if len(result) != tc.wantLen {
				t.Errorf("expected length %d, got %d", tc.wantLen, len(result))
			}
		})
	}
}
