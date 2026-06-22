// Package bm25 provides a BM25-based lexical search index for code symbols.
// It tokenizes text by splitting on whitespace and identifier boundaries
// (camelCase, snake_case), then uses classic BM25 scoring.
package bm25

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/kgatilin/archai/internal/retrieval/types"
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

	// Remove old document if exists
	idx.removeLocked(id)

	// Add new document
	idx.docs[id] = tokens
	idx.docLengths[id] = len(tokens)
	idx.totalDocs++

	// Update inverted index and term frequencies
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

	idx.updateAvgDocLen()
}

func (idx *Index) updateAvgDocLen() {
	if idx.totalDocs == 0 {
		idx.avgDocLen = 0
		return
	}
	total := 0
	for _, length := range idx.docLengths {
		total += length
	}
	idx.avgDocLen = float64(total) / float64(idx.totalDocs)
}

// Scored is an alias for types.Scored.
type Scored = types.Scored

// Search returns the top-k results by BM25 score (descending).
func (idx *Index) Search(query string, k int) []types.Scored {
	if k <= 0 {
		return nil
	}

	queryTokens := Tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if idx.totalDocs == 0 {
		return nil
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
		return sorted[i].score > sorted[j].score
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
	return results
}

// Len returns the number of documents in the index.
func (idx *Index) Len() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.totalDocs
}

// Tokenize splits text into lowercase tokens, splitting on whitespace
// and identifier boundaries (camelCase, snake_case).
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

// persistedIndex is the JSON structure for persistence.
type persistedIndex struct {
	K1            float64             `json:"k1"`
	B             float64             `json:"b"`
	Docs          map[string][]string `json:"docs"`
	TotalDocs     int                 `json:"total_docs"`
	AvgDocLen     float64             `json:"avg_doc_len"`
}

// Save persists the index to a JSON file.
func (idx *Index) Save(path string) error {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	persisted := persistedIndex{
		K1:        idx.k1,
		B:         idx.b,
		Docs:      idx.docs,
		TotalDocs: idx.totalDocs,
		AvgDocLen: idx.avgDocLen,
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load restores the index from a JSON file.
func (idx *Index) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No persisted index, start fresh
		}
		return err
	}

	var persisted persistedIndex
	if err := json.Unmarshal(data, &persisted); err != nil {
		return err
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Reset and rebuild
	idx.k1 = persisted.K1
	idx.b = persisted.B
	idx.docs = make(map[string][]string)
	idx.docLengths = make(map[string]int)
	idx.invertedIndex = make(map[string][]string)
	idx.termFreqs = make(map[string]map[string]int)
	idx.totalDocs = 0
	idx.avgDocLen = 0

	// Re-add all documents to rebuild indexes
	for id, tokens := range persisted.Docs {
		idx.docs[id] = tokens
		idx.docLengths[id] = len(tokens)
		idx.totalDocs++

		// Build inverted index and term frequencies
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

	idx.updateAvgDocLen()
	return nil
}
