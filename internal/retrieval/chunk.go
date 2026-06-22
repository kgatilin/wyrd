package retrieval

import (
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// Chunk represents a text segment for embedding. Normal nodes produce
// exactly one chunk; oversized nodes (body > EmbedTextBudget) are split
// into multiple chunks, each prefixed with the signature as a header.
type Chunk struct {
	Text string // Text content to embed
}

// BuildChunks returns the embedding chunks for a node. For normal nodes
// (total text within EmbedTextBudget), returns a single chunk identical
// to EmbedText. For oversized nodes, returns multiple chunks:
//   - Each chunk starts with the signature header
//   - Body is split at Go statement boundaries (AST-aware)
//   - Each chunk fits within EmbedTextBudget
//
// The caller should embed all chunks, then mean-pool the vectors into
// a single vector for the node (preserving the "one node = one vector"
// invariant).
func BuildChunks(n Node, projectRoot string) ([]Chunk, error) {
	// Build header: package + signature + doc
	header := buildHeader(n)

	// Read body from span
	body, err := readSpanBody(n.Span, projectRoot)
	if err != nil {
		// Body read failure: return single chunk with header only
		return []Chunk{{Text: truncateToSignatureHeader(header, n.Signature, EmbedTextBudget)}}, nil
	}

	fullText := header + body

	// If within budget, return single chunk
	if len(fullText) <= EmbedTextBudget {
		return []Chunk{{Text: fullText}}, nil
	}

	// Oversized: split body into chunks, each prefixed with signature header
	sigHeader := buildSignatureHeader(n)
	return splitBodyIntoChunks(n, body, sigHeader, projectRoot)
}

// buildHeader creates the header portion (package + signature + doc) for embedding.
func buildHeader(n Node) string {
	var sb strings.Builder

	// Package header
	if n.Package != "" {
		sb.WriteString("package ")
		sb.WriteString(filepath.Base(n.Package))
		sb.WriteString("\n\n")
	}

	// Signature
	if n.Signature != "" {
		sb.WriteString(n.Signature)
		sb.WriteString("\n\n")
	}

	// Doc comment
	if n.Doc != "" {
		sb.WriteString(n.Doc)
		if !strings.HasSuffix(n.Doc, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildSignatureHeader creates a minimal header for sub-chunks (signature only).
func buildSignatureHeader(n Node) string {
	if n.Signature == "" {
		return ""
	}
	return n.Signature + "\n\n"
}

// splitBodyIntoChunks splits an oversized body into budget-compliant chunks.
// Each chunk is prefixed with the signature header.
//
// Strategy:
//  1. Try AST-based splitting at statement boundaries (cleanest)
//  2. Fall back to line-based splitting if AST parsing fails
func splitBodyIntoChunks(n Node, body, sigHeader, projectRoot string) ([]Chunk, error) {
	// Calculate available budget for body in each chunk
	bodyBudget := EmbedTextBudget - len(sigHeader)
	if bodyBudget <= 0 {
		// Signature alone exceeds budget; just return truncated signature
		return []Chunk{{Text: sigHeader[:min(len(sigHeader), EmbedTextBudget)]}}, nil
	}

	// Try AST-based splitting first
	chunks, err := splitByAST(n, body, sigHeader, bodyBudget, projectRoot)
	if err == nil && len(chunks) > 0 {
		return chunks, nil
	}

	// Fall back to line-based splitting
	return splitByLines(body, sigHeader, bodyBudget), nil
}

// splitByAST attempts to split body at Go statement boundaries using AST parsing.
// Returns nil if AST parsing fails.
func splitByAST(n Node, body, sigHeader string, bodyBudget int, projectRoot string) ([]Chunk, error) {
	// Read the full file to get proper AST context
	filePath := filepath.Join(projectRoot, n.Span.File)
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, n.Span.File, fileContent, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	// Find the declaration that spans our node
	var decl ast.Decl
	for _, d := range file.Decls {
		start := fset.Position(d.Pos()).Offset
		end := fset.Position(d.End()).Offset
		if start <= n.Span.StartByte && end >= n.Span.EndByte {
			decl = d
			break
		}
	}

	if decl == nil {
		return nil, nil // Not found, fall back to lines
	}

	// Extract statement boundaries from function body
	var stmtBounds []stmtBound
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Body != nil {
			stmtBounds = extractStatementBounds(fset, d.Body.List, fileContent)
		}
	default:
		// For non-functions (structs, interfaces), fall back to lines
		return nil, nil
	}

	if len(stmtBounds) == 0 {
		return nil, nil // No statements found
	}

	// Group statements into chunks that fit within bodyBudget
	return groupStatementsIntoChunks(stmtBounds, sigHeader, bodyBudget), nil
}

// stmtBound tracks a statement's text and boundaries.
type stmtBound struct {
	text  string
	start int
	end   int
}

// extractStatementBounds extracts statement text and boundaries from an AST.
func extractStatementBounds(fset *token.FileSet, stmts []ast.Stmt, fileContent []byte) []stmtBound {
	bounds := make([]stmtBound, 0, len(stmts))
	for _, stmt := range stmts {
		start := fset.Position(stmt.Pos()).Offset
		end := fset.Position(stmt.End()).Offset
		if start >= 0 && end <= len(fileContent) && start < end {
			bounds = append(bounds, stmtBound{
				text:  string(fileContent[start:end]),
				start: start,
				end:   end,
			})
		}
	}
	return bounds
}

// groupStatementsIntoChunks groups statements into budget-compliant chunks.
func groupStatementsIntoChunks(bounds []stmtBound, sigHeader string, bodyBudget int) []Chunk {
	var chunks []Chunk
	var current strings.Builder

	flushChunk := func() {
		if current.Len() > 0 {
			chunks = append(chunks, Chunk{Text: sigHeader + current.String()})
			current.Reset()
		}
	}

	for _, b := range bounds {
		stmtText := b.text + "\n"

		// If single statement exceeds budget, split it further (rare)
		if len(stmtText) > bodyBudget {
			flushChunk()
			// Split large statement by lines
			for _, line := range strings.SplitAfter(stmtText, "\n") {
				if current.Len()+len(line) > bodyBudget {
					flushChunk()
				}
				current.WriteString(line)
			}
			flushChunk()
			continue
		}

		// Would adding this statement exceed budget?
		if current.Len()+len(stmtText) > bodyBudget {
			flushChunk()
		}
		current.WriteString(stmtText)
	}
	flushChunk()

	return chunks
}

// splitByLines splits body at line boundaries when AST parsing fails.
// Used as a fallback for non-Go files or when AST parsing is impractical.
func splitByLines(body, sigHeader string, bodyBudget int) []Chunk {
	lines := strings.SplitAfter(body, "\n")
	var chunks []Chunk
	var current strings.Builder

	flushChunk := func() {
		if current.Len() > 0 {
			chunks = append(chunks, Chunk{Text: sigHeader + current.String()})
			current.Reset()
		}
	}

	for _, line := range lines {
		// If single line exceeds budget, just include it (truncated if needed)
		if len(line) > bodyBudget {
			flushChunk()
			truncated := line
			if len(truncated) > bodyBudget {
				truncated = truncated[:bodyBudget]
			}
			chunks = append(chunks, Chunk{Text: sigHeader + truncated})
			continue
		}

		// Would adding this line exceed budget?
		if current.Len()+len(line) > bodyBudget {
			flushChunk()
		}
		current.WriteString(line)
	}
	flushChunk()

	return chunks
}

// FullTextForHash returns the complete text for a node (all chunks concatenated)
// for use in content hash computation. This ensures the hash changes when any
// part of an oversized node changes.
func FullTextForHash(n Node, projectRoot string) (string, error) {
	// Build header
	header := buildHeader(n)

	// Read body from span
	body, err := readSpanBody(n.Span, projectRoot)
	if err != nil {
		return header, nil
	}

	return header + body, nil
}

// MeanPoolVectors computes the mean of multiple vectors and L2-normalizes
// the result. Used to combine chunk vectors into a single node vector.
// Returns a zero vector if inputs is empty.
func MeanPoolVectors(vectors [][]float32) []float32 {
	if len(vectors) == 0 {
		return nil
	}
	if len(vectors) == 1 {
		// Single vector: just normalize it
		return l2Normalize(vectors[0])
	}

	dim := len(vectors[0])
	mean := make([]float32, dim)

	// Sum all vectors
	for _, vec := range vectors {
		for i, v := range vec {
			mean[i] += v
		}
	}

	// Divide by count to get mean
	n := float32(len(vectors))
	for i := range mean {
		mean[i] /= n
	}

	// L2 normalize
	return l2Normalize(mean)
}

// l2Normalize normalizes a vector to unit length using L2 norm.
func l2Normalize(vec []float32) []float32 {
	if len(vec) == 0 {
		return vec
	}

	var sumSq float64
	for _, v := range vec {
		sumSq += float64(v) * float64(v)
	}

	if sumSq == 0 {
		return vec
	}

	norm := float32(math.Sqrt(sumSq))
	result := make([]float32, len(vec))
	for i, v := range vec {
		result[i] = v / norm
	}
	return result
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
