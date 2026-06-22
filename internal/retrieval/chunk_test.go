package retrieval

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

func TestBuildChunks_NormalNode(t *testing.T) {
	tmpDir := t.TempDir()
	srcContent := `func Foo() {
	return
}`
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
		Signature: "func Foo()",
		Span: domain.Span{
			File:      filepath.Join("pkg", "foo.go"),
			StartByte: 0,
			EndByte:   len(srcContent),
		},
	}

	chunks, err := BuildChunks(node, tmpDir)
	if err != nil {
		t.Fatalf("BuildChunks: %v", err)
	}

	// Normal node should produce exactly one chunk
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk for normal node, got %d", len(chunks))
	}

	// Chunk should contain signature and body
	if !strings.Contains(chunks[0].Text, "func Foo()") {
		t.Error("chunk should contain signature")
	}
	if !strings.Contains(chunks[0].Text, "return") {
		t.Error("chunk should contain body")
	}
}

func TestBuildChunks_OversizedNode(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a function body that exceeds EmbedTextBudget
	var bodyLines []string
	for i := 0; i < 200; i++ {
		bodyLines = append(bodyLines, "\tfmt.Println(\"line "+string(rune('a'+i%26))+"\")")
	}
	srcContent := "func LongFunc() {\n" + strings.Join(bodyLines, "\n") + "\n}"
	srcPath := filepath.Join(tmpDir, "pkg", "long.go")
	if err := os.MkdirAll(filepath.Dir(srcPath), 0755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	if err := os.WriteFile(srcPath, []byte(srcContent), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	node := Node{
		ID:        "pkg.LongFunc",
		Kind:      "func",
		Package:   "pkg",
		Signature: "func LongFunc()",
		Span: domain.Span{
			File:      filepath.Join("pkg", "long.go"),
			StartByte: 0,
			EndByte:   len(srcContent),
		},
	}

	// Verify the content is actually oversized
	if len(srcContent) <= EmbedTextBudget {
		t.Fatalf("test content should exceed budget: %d <= %d", len(srcContent), EmbedTextBudget)
	}

	chunks, err := BuildChunks(node, tmpDir)
	if err != nil {
		t.Fatalf("BuildChunks: %v", err)
	}

	// Oversized node should produce multiple chunks
	if len(chunks) <= 1 {
		t.Errorf("expected >1 chunks for oversized node, got %d", len(chunks))
	}

	// Each chunk should have signature header
	for i, chunk := range chunks {
		if !strings.Contains(chunk.Text, "func LongFunc()") {
			t.Errorf("chunk %d should contain signature header", i)
		}
	}

	// Each chunk should be within budget
	for i, chunk := range chunks {
		if len(chunk.Text) > EmbedTextBudget {
			t.Errorf("chunk %d exceeds budget: %d > %d", i, len(chunk.Text), EmbedTextBudget)
		}
	}
}

func TestBuildChunks_PreservesSignatureHeader(t *testing.T) {
	tmpDir := t.TempDir()

	// Create oversized content
	body := strings.Repeat("x := 1\n", 500)
	srcContent := "func BigFunc(a, b, c int) (string, error) {\n" + body + "}"
	srcPath := filepath.Join(tmpDir, "pkg", "big.go")
	if err := os.MkdirAll(filepath.Dir(srcPath), 0755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	if err := os.WriteFile(srcPath, []byte(srcContent), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	sig := "func BigFunc(a, b, c int) (string, error)"
	node := Node{
		ID:        "pkg.BigFunc",
		Kind:      "func",
		Package:   "pkg",
		Signature: sig,
		Span: domain.Span{
			File:      filepath.Join("pkg", "big.go"),
			StartByte: 0,
			EndByte:   len(srcContent),
		},
	}

	chunks, err := BuildChunks(node, tmpDir)
	if err != nil {
		t.Fatalf("BuildChunks: %v", err)
	}

	// Every chunk must start with the signature
	for i, chunk := range chunks {
		if !strings.HasPrefix(chunk.Text, sig) {
			t.Errorf("chunk %d should start with signature %q, got prefix: %q",
				i, sig, chunk.Text[:min(len(chunk.Text), 50)])
		}
	}
}

func TestMeanPoolVectors_SingleVector(t *testing.T) {
	vec := []float32{3, 4} // 3-4-5 triangle
	result := MeanPoolVectors([][]float32{vec})

	// Should be L2 normalized
	expected := []float32{0.6, 0.8} // 3/5, 4/5
	if !vecApproxEqual(result, expected, 0.001) {
		t.Errorf("MeanPoolVectors([3,4]) = %v, want ~%v", result, expected)
	}

	// Verify unit length
	var sumSq float32
	for _, v := range result {
		sumSq += v * v
	}
	if math.Abs(float64(sumSq)-1.0) > 0.001 {
		t.Errorf("result should be unit length, got norm^2 = %f", sumSq)
	}
}

func TestMeanPoolVectors_MultipleVectors(t *testing.T) {
	vecs := [][]float32{
		{1, 0, 0},
		{0, 1, 0},
	}
	result := MeanPoolVectors(vecs)

	// Mean is [0.5, 0.5, 0], normalized length = sqrt(0.25+0.25) = sqrt(0.5)
	// So normalized = [0.5/sqrt(0.5), 0.5/sqrt(0.5), 0] = [1/sqrt(2), 1/sqrt(2), 0]
	expectedVal := float32(1.0 / math.Sqrt(2))
	expected := []float32{expectedVal, expectedVal, 0}

	if !vecApproxEqual(result, expected, 0.001) {
		t.Errorf("MeanPoolVectors = %v, want ~%v", result, expected)
	}

	// Verify unit length
	var sumSq float32
	for _, v := range result {
		sumSq += v * v
	}
	if math.Abs(float64(sumSq)-1.0) > 0.001 {
		t.Errorf("result should be unit length, got norm^2 = %f", sumSq)
	}
}

func TestMeanPoolVectors_Empty(t *testing.T) {
	result := MeanPoolVectors(nil)
	if result != nil {
		t.Errorf("MeanPoolVectors(nil) = %v, want nil", result)
	}

	result = MeanPoolVectors([][]float32{})
	if result != nil {
		t.Errorf("MeanPoolVectors([]) = %v, want nil", result)
	}
}

func TestFullTextForHash_CoversFull(t *testing.T) {
	tmpDir := t.TempDir()

	// Create oversized content - the full text should be included
	body := strings.Repeat("line\n", 1000)
	srcContent := "func Huge() {\n" + body + "}"
	srcPath := filepath.Join(tmpDir, "pkg", "huge.go")
	if err := os.MkdirAll(filepath.Dir(srcPath), 0755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	if err := os.WriteFile(srcPath, []byte(srcContent), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	node := Node{
		ID:        "pkg.Huge",
		Kind:      "func",
		Package:   "pkg",
		Signature: "func Huge()",
		Span: domain.Span{
			File:      filepath.Join("pkg", "huge.go"),
			StartByte: 0,
			EndByte:   len(srcContent),
		},
	}

	fullText, err := FullTextForHash(node, tmpDir)
	if err != nil {
		t.Fatalf("FullTextForHash: %v", err)
	}

	// Full text should include the entire body, not be truncated
	if !strings.Contains(fullText, body) {
		t.Error("FullTextForHash should include complete body")
	}

	// Verify it's longer than EmbedText (which truncates)
	embedText, _ := EmbedText(node, tmpDir)
	if len(fullText) <= len(embedText) {
		t.Error("FullTextForHash should be longer than truncated EmbedText for oversized node")
	}
}

func TestContentHash_DetectsBodyChanges(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "pkg", "change.go")
	if err := os.MkdirAll(filepath.Dir(srcPath), 0755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}

	// Create oversized content so changes at the end would be truncated by EmbedText
	body1 := strings.Repeat("a := 1\n", 500) + "// version 1"
	srcContent1 := "func Changing() {\n" + body1 + "\n}"
	if err := os.WriteFile(srcPath, []byte(srcContent1), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	node := Node{
		ID:        "pkg.Changing",
		Kind:      "func",
		Package:   "pkg",
		Signature: "func Changing()",
		Span: domain.Span{
			File:      filepath.Join("pkg", "change.go"),
			StartByte: 0,
			EndByte:   len(srcContent1),
		},
	}

	hash1, err := ContentHash(node, tmpDir)
	if err != nil {
		t.Fatalf("ContentHash v1: %v", err)
	}

	// Change content at the end (beyond truncation point)
	body2 := strings.Repeat("a := 1\n", 500) + "// version 2"
	srcContent2 := "func Changing() {\n" + body2 + "\n}"
	if err := os.WriteFile(srcPath, []byte(srcContent2), 0644); err != nil {
		t.Fatalf("writing source v2: %v", err)
	}

	node.Span.EndByte = len(srcContent2)
	hash2, err := ContentHash(node, tmpDir)
	if err != nil {
		t.Fatalf("ContentHash v2: %v", err)
	}

	// Hash should change even though the change was beyond EmbedText truncation
	if hash1 == hash2 {
		t.Error("ContentHash should detect changes in the full body, not just truncated portion")
	}
}

func TestContentHash_StableForUnchanged(t *testing.T) {
	tmpDir := t.TempDir()
	srcContent := "func Stable() { return }"
	srcPath := filepath.Join(tmpDir, "pkg", "stable.go")
	if err := os.MkdirAll(filepath.Dir(srcPath), 0755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	if err := os.WriteFile(srcPath, []byte(srcContent), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	node := Node{
		ID:        "pkg.Stable",
		Kind:      "func",
		Package:   "pkg",
		Signature: "func Stable()",
		Span: domain.Span{
			File:      filepath.Join("pkg", "stable.go"),
			StartByte: 0,
			EndByte:   len(srcContent),
		},
	}

	hash1, _ := ContentHash(node, tmpDir)
	hash2, _ := ContentHash(node, tmpDir)

	if hash1 != hash2 {
		t.Errorf("ContentHash should be stable: %s != %s", hash1, hash2)
	}
}

func vecApproxEqual(a, b []float32, epsilon float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) > epsilon {
			return false
		}
	}
	return true
}
