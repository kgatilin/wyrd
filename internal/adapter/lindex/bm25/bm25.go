// Package bm25 provides a BM25-based lexical search index for code symbols.
// It tokenizes text by splitting on whitespace and identifier boundaries
// (camelCase, snake_case), then uses classic BM25 scoring.
package bm25

import (
	"bufio"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/kgatilin/wyrd/internal/retrieval/types"
)

// Default BM25 parameters
const (
	DefaultK1 = 1.2
	DefaultB  = 0.75
)

// Index implements retrieval.LexicalIndex using BM25 scoring.
type Index struct {
	mu sync.RWMutex

	// k1 and b are BM25 tuning parameters
	k1 float64
	b  float64

	// docs maps document ID to its tokens
	docs map[string][]string

	// docLengths maps document ID to its token count
	docLengths map[string]int

	// invertedIndex maps term to posting list (doc IDs)
	invertedIndex map[string][]string

	// termFreqs maps term -> docID -> frequency
	termFreqs map[string]map[string]int

	// avgDocLen is the average document length
	avgDocLen float64

	// totalLen is the sum of all document lengths, kept so the average is
	// O(1) per upsert rather than a pass over every document.
	totalLen int

	// totalDocs is the total number of documents
	totalDocs int
}

// Option configures the Index.
type Option func(*Index)

// WithK1 sets the BM25 k1 parameter (term frequency saturation).
func WithK1(k1 float64) Option {
	return func(idx *Index) {
		if k1 > 0 {
			idx.k1 = k1
		}
	}
}

// WithB sets the BM25 b parameter (document length normalization).
func WithB(b float64) Option {
	return func(idx *Index) {
		if b >= 0 && b <= 1 {
			idx.b = b
		}
	}
}

// New creates a new BM25 index with default parameters.
func New(opts ...Option) *Index {
	idx := &Index{
		k1:            DefaultK1,
		b:             DefaultB,
		docs:          make(map[string][]string),
		docLengths:    make(map[string]int),
		invertedIndex: make(map[string][]string),
		termFreqs:     make(map[string]map[string]int),
	}
	for _, opt := range opts {
		opt(idx)
	}
	return idx
}

// Upsert inserts or updates a document.
func (idx *Index) Upsert(id, text string) {
	tokens := Tokenize(text)

	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.addTokensLocked(id, tokens)
	idx.updateAvgDocLen()
}

// Remove deletes a document from the index.
func (idx *Index) Remove(id string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.removeLocked(id)
}

func (idx *Index) removeLocked(id string) {
	tokens, exists := idx.docs[id]
	if !exists {
		return
	}

	// Remove from inverted index
	seen := make(map[string]bool)
	for _, token := range tokens {
		if seen[token] {
			continue
		}
		seen[token] = true

		// Remove from posting list
		postings := idx.invertedIndex[token]
		for i, docID := range postings {
			if docID == id {
				idx.invertedIndex[token] = append(postings[:i], postings[i+1:]...)
				break
			}
		}
		if len(idx.invertedIndex[token]) == 0 {
			delete(idx.invertedIndex, token)
		}

		// Remove term frequency
		delete(idx.termFreqs[token], id)
		if len(idx.termFreqs[token]) == 0 {
			delete(idx.termFreqs, token)
		}
	}

	delete(idx.docs, id)
	delete(idx.docLengths, id)
	idx.totalDocs--
	idx.totalLen -= len(tokens)

	idx.updateAvgDocLen()
}

func (idx *Index) updateAvgDocLen() {
	if idx.totalDocs == 0 {
		idx.avgDocLen = 0
		return
	}
	idx.avgDocLen = float64(idx.totalLen) / float64(idx.totalDocs)
}

// Scored is an alias for types.Scored.
type Scored = types.Scored

// Has reports whether a document is indexed under id.
func (idx *Index) Has(id string) bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	_, ok := idx.docs[id]
	return ok
}

// Search returns the top-k results by BM25 score (descending) among the
// documents keep accepts; a nil keep accepts all. Statistics (IDF, average
// length) stay those of the whole index.
func (idx *Index) Search(_ context.Context, query string, k int, keep func(id string) bool) ([]types.Scored, error) {
	if k <= 0 {
		return nil, nil
	}

	queryTokens := Tokenize(query)
	if len(queryTokens) == 0 {
		return nil, nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if idx.totalDocs == 0 {
		return nil, nil
	}

	// Compute BM25 scores for all matching documents
	scores := make(map[string]float64)

	for _, term := range queryTokens {
		postings := idx.invertedIndex[term]
		if len(postings) == 0 {
			continue
		}

		// IDF: log((N - n + 0.5) / (n + 0.5) + 1)
		n := float64(len(postings))
		N := float64(idx.totalDocs)
		idf := math.Log((N-n+0.5)/(n+0.5) + 1)

		for _, docID := range postings {
			if keep != nil && !keep(docID) {
				continue
			}
			tf := float64(idx.termFreqs[term][docID])
			docLen := float64(idx.docLengths[docID])

			// BM25 term score
			numerator := tf * (idx.k1 + 1)
			denominator := tf + idx.k1*(1-idx.b+idx.b*(docLen/idx.avgDocLen))
			scores[docID] += idf * (numerator / denominator)
		}
	}

	// Sort by score descending
	type scored struct {
		id    string
		score float64
	}
	sorted := make([]scored, 0, len(scores))
	for id, score := range scores {
		sorted = append(sorted, scored{id: id, score: score})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].score != sorted[j].score {
			return sorted[i].score > sorted[j].score
		}
		return sorted[i].id < sorted[j].id
	})

	// Take top-k
	if k > len(sorted) {
		k = len(sorted)
	}

	results := make([]types.Scored, k)
	for i := 0; i < k; i++ {
		results[i] = types.Scored{
			ID:    sorted[i].id,
			Score: float32(sorted[i].score),
		}
	}
	return results, nil
}

// Len returns the number of documents in the index.
func (idx *Index) Len() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.totalDocs
}

// Tokenize splits text into lowercase tokens, splitting on whitespace
// and identifier boundaries (camelCase, snake_case).
//
// The remote lexical index (vindex/qdrant) tokenizes with it too and stores
// the result; a change here needs that package's weightsVersion bumped.
func Tokenize(text string) []string {
	var tokens []string

	// First split on whitespace and punctuation
	words := strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r)
	})

	for _, word := range words {
		// Split identifiers and add sub-tokens
		subTokens := splitIdentifier(word)
		for _, st := range subTokens {
			lower := strings.ToLower(st)
			if lower != "" && len(lower) >= 2 { // Skip single-char tokens
				tokens = append(tokens, lower)
			}
		}
	}

	return tokens
}

// splitIdentifier splits a camelCase or snake_case identifier into parts.
func splitIdentifier(s string) []string {
	if s == "" {
		return nil
	}

	var parts []string
	var current strings.Builder

	flush := func() {
		if current.Len() > 0 {
			parts = append(parts, current.String())
			current.Reset()
		}
	}

	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == '_':
			// Snake case separator
			flush()
		case unicode.IsUpper(r):
			// Start of new word in camelCase
			if current.Len() > 0 {
				// Look ahead to handle acronyms like "HTTPServer" -> "HTTP", "Server"
				if i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
					flush()
				}
			}
			current.WriteRune(r)
		default:
			current.WriteRune(r)
		}
	}
	flush()

	// Also add the original identifier as a token for exact matches
	if len(parts) > 1 {
		parts = append(parts, s)
	}

	return parts
}

// fileMagic and fileVersion head a persisted index, so a foreign or older
// file reads as "no index" instead of as garbage.
const (
	fileMagic   = "wyrd-bm25"
	fileVersion = 2
)

// fileHeader is the first record of a persisted index; one docRecord per
// document follows until EOF.
type fileHeader struct {
	Magic   string
	Version int
	K1, B   float64
}

type docRecord struct {
	ID     string
	Tokens []string
}

// Save streams the documents keep accepts (all, for a nil keep) to path,
// record by record, under the read lock: the index is never encoded into one
// buffer and never copied. The write is atomic (temp file + rename).
func (idx *Index) Save(path string, keep func(id string) bool) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("bm25: creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp")
	if err != nil {
		return fmt.Errorf("bm25: creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}

	w := bufio.NewWriter(tmp)
	enc := gob.NewEncoder(w)

	idx.mu.RLock()
	err = enc.Encode(fileHeader{Magic: fileMagic, Version: fileVersion, K1: idx.k1, B: idx.b})
	for id, tokens := range idx.docs {
		if err != nil {
			break
		}
		if keep != nil && !keep(id) {
			continue
		}
		err = enc.Encode(docRecord{ID: id, Tokens: tokens})
	}
	idx.mu.RUnlock()
	if err != nil {
		return fail(fmt.Errorf("bm25: encoding %s: %w", path, err))
	}

	if err := w.Flush(); err != nil {
		return fail(fmt.Errorf("bm25: flushing %s: %w", tmpName, err))
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("bm25: closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("bm25: renaming %s: %w", tmpName, err)
	}
	return nil
}

// Load adds the documents persisted at path to the index. A missing file, or
// one in another format, is an empty index; a truncated tail is dropped.
func (idx *Index) Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("bm25: opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	dec := gob.NewDecoder(bufio.NewReader(f))
	var hdr fileHeader
	if err := dec.Decode(&hdr); err != nil || hdr.Magic != fileMagic || hdr.Version != fileVersion {
		return nil
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.k1, idx.b = hdr.K1, hdr.B
	for {
		var rec docRecord
		if err := dec.Decode(&rec); err != nil {
			if !errors.Is(err, io.EOF) {
				fmt.Fprintf(os.Stderr, "bm25: %s: dropping unreadable tail: %v\n", path, err)
			}
			break
		}
		idx.addTokensLocked(rec.ID, rec.Tokens)
	}
	idx.updateAvgDocLen()
	return nil
}

// addTokensLocked indexes an already tokenized document. Must be called with
// mu held for writing; the caller refreshes the average length.
func (idx *Index) addTokensLocked(id string, tokens []string) {
	idx.removeLocked(id)
	idx.docs[id] = tokens
	idx.docLengths[id] = len(tokens)
	idx.totalDocs++
	idx.totalLen += len(tokens)

	termCounts := make(map[string]int)
	for _, token := range tokens {
		termCounts[token]++
	}
	for term, count := range termCounts {
		if idx.termFreqs[term] == nil {
			idx.termFreqs[term] = make(map[string]int)
		}
		idx.termFreqs[term][id] = count
		idx.invertedIndex[term] = append(idx.invertedIndex[term], id)
	}
}
