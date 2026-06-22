package retrieval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
)

// EmbedTextBudget is the maximum character count for embed text.
// Approximately 512 tokens at ~4 chars/token.
const EmbedTextBudget = 2048

// EmbedText assembles the text to be embedded for a node. It includes:
// 1. Package header (e.g., "package serve")
// 2. Signature
// 3. Doc comment
// 4. Source body (read from disk via Span)
//
// The result is truncated to EmbedTextBudget characters, keeping the
// signature as the header. If the body cannot be read, it is omitted.
func EmbedText(n Node, projectRoot string) (string, error) {
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

	// Source body from span
	body, err := readSpanBody(n.Span, projectRoot)
	if err != nil {
		// Body read failure is not fatal; we still have signature+doc
		body = ""
	}
	if body != "" {
		sb.WriteString(body)
	}

	text := sb.String()
	return truncateToSignatureHeader(text, n.Signature, EmbedTextBudget), nil
}

// ContentHash returns a SHA-256 hex digest of the FULL node text (not truncated).
// Used for freshness detection: if the hash changes, re-embedding is needed.
// This covers the complete body so that changes to any part of oversized nodes
// trigger re-embedding.
func ContentHash(n Node, projectRoot string) (string, error) {
	text, err := FullTextForHash(n, projectRoot)
	if err != nil {
		return "", fmt.Errorf("generating full text for hash: %w", err)
	}
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:]), nil
}

// readSpanBody reads the source code slice defined by the span from disk.
// Returns empty string if span is invalid or file cannot be read.
func readSpanBody(span domain.Span, projectRoot string) (string, error) {
	if !span.IsValid() {
		return "", nil
	}

	filePath := filepath.Join(projectRoot, span.File)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", filePath, err)
	}

	if span.StartByte < 0 || span.EndByte > len(data) || span.StartByte >= span.EndByte {
		return "", fmt.Errorf("invalid span [%d:%d] for file of length %d", span.StartByte, span.EndByte, len(data))
	}

	return string(data[span.StartByte:span.EndByte]), nil
}

// truncateToSignatureHeader truncates text to budget characters while
// keeping the signature at the beginning if present. If text already
// fits within budget, it is returned unchanged.
func truncateToSignatureHeader(text, signature string, budget int) string {
	if len(text) <= budget {
		return text
	}

	// If we have a signature, ensure it stays at the front
	if signature != "" && strings.HasPrefix(text, "package ") {
		// Find the signature in the text after the package line
		sigIdx := strings.Index(text, signature)
		if sigIdx > 0 {
			// Keep package header + signature + as much of the rest as fits
			sigEnd := sigIdx + len(signature)
			if sigEnd <= budget {
				// Signature fits; truncate after it
				remaining := budget - sigEnd
				return text[:sigEnd+remaining]
			}
		}
	}

	// Simple truncation
	return text[:budget]
}
