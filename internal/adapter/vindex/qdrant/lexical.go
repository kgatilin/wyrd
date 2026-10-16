package qdrant

import (
	"context"
	"fmt"
	"hash/fnv"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/kgatilin/wyrd/internal/adapter/lindex/bm25"
	"github.com/kgatilin/wyrd/internal/retrieval/types"
)

const (
	// AvgDocLen is the document length BM25's length normalization is
	// measured against. The local index uses the live average of its
	// documents; a document stored remotely is weighted once, when it is
	// written, so the average has to be a constant. Symbol texts of Go
	// repositories measured between 63 and 89 tokens on average.
	AvgDocLen = 80

	// weightsVersion identifies how a document becomes a sparse vector, and
	// is part of the recipe its points are stored under. A stored document
	// is weighted once, when it is written, so bump it whenever that
	// changes — bm25.Tokenize, DefaultK1, DefaultB, AvgDocLen, the token
	// hash — or old and new documents are ranked against each other with
	// different weights.
	weightsVersion = 1

	// flushTimeout bounds a flush nobody is waiting on with a context of
	// their own.
	flushTimeout = 2 * time.Minute
)

// Lexical implements retrieval.LexicalIndex over a Qdrant collection: one
// point per document, carrying its BM25 term weights as a sparse vector.
//
// Tokenization and term weighting happen here, with the tokenizer of the
// local index, so identifiers split the same way wherever the index lives:
// a term's dimension is a hash of the token, a document's value is the
// term's saturated, length-normalized frequency, and a query's value is the
// term's count. Qdrant multiplies by IDF (the vector's modifier), which makes
// the score of a hit the BM25 score.
//
// Writes are buffered and sent in batches; Search and Flush send what is
// pending first, so a search sees every document upserted before it. Safe
// for concurrent use.
type Lexical struct {
	*client

	mu    sync.Mutex
	known map[string]bool    // documents the collection holds
	puts  map[string]*sparse // documents to write
	dels  map[string]bool    // documents to delete
	// err is the failure of the last flush nobody could be told about; the
	// next Search or Flush returns it unless a retry has succeeded by then.
	err error
}

// sparse is a sparse vector as Qdrant takes it.
type sparse struct {
	Indices []uint32  `json:"indices"`
	Values  []float32 `json:"values"`
}

// OpenLexical verifies the collection has the sparse vector with the IDF
// modifier and reads which documents it already holds for cfg.Repo and
// cfg.Recipe.
func OpenLexical(ctx context.Context, cfg Config) (*Lexical, error) {
	cfg.Recipe += "/w" + strconv.Itoa(weightsVersion)
	c, err := newClient(cfg)
	if err != nil {
		return nil, err
	}
	schema, err := c.schema(ctx)
	if err != nil {
		return nil, err
	}
	if err := checkSparse(cfg.Collection, schema); err != nil {
		return nil, err
	}
	known, err := c.scrollHashes(ctx)
	if err != nil {
		return nil, err
	}
	return &Lexical{client: c, known: known, puts: map[string]*sparse{}, dels: map[string]bool{}}, nil
}

// checkSparse reports why the collection cannot hold BM25 documents.
func checkSparse(collection string, schema Schema) error {
	if !schema.Sparse {
		return fmt.Errorf("qdrant: collection %q has no sparse vector named %q", collection, SparseVector)
	}
	if !schema.SparseIDF {
		return fmt.Errorf("qdrant: sparse vector %q of collection %q lacks the idf modifier", SparseVector, collection)
	}
	return nil
}

// Upsert buffers the document for id, replacing one buffered or stored
// before, and sends the buffer once it holds a batch.
func (l *Lexical) Upsert(id, text string) {
	vec := documentVector(bm25.Tokenize(text))
	l.mu.Lock()
	defer l.mu.Unlock()
	l.puts[id] = vec
	delete(l.dels, id)
	if len(l.puts) >= batchSize {
		l.flushDetached()
	}
}

// Remove buffers the deletion of the document for id.
func (l *Lexical) Remove(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.puts, id)
	if l.known[id] {
		l.dels[id] = true
	}
	if len(l.dels) >= batchSize {
		l.flushDetached()
	}
}

// Has reports whether a document is stored or buffered under id.
func (l *Lexical) Has(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.puts[id] != nil {
		return true
	}
	return l.known[id] && !l.dels[id]
}

// Len returns the number of documents stored or buffered.
func (l *Lexical) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(l.known) - len(l.dels)
	for id := range l.puts {
		if !l.known[id] {
			n++
		}
	}
	return n
}

// Flush sends the buffered writes.
func (l *Lexical) Flush(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.flushLocked(ctx)
}

// Retain deletes every stored document keep rejects, so the collection does
// not accumulate the text of code that no longer exists.
func (l *Lexical) Retain(ctx context.Context, keep func(id string) bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id := range l.known {
		if !keep(id) && l.puts[id] == nil {
			l.dels[id] = true
		}
	}
	return l.flushLocked(ctx)
}

// Search returns at most k documents keep accepts, by BM25 score. The
// repository and recipe are filtered by Qdrant before it ranks; keep is
// applied to what comes back, so the request is oversampled. Not exact: a
// collection holding several worktrees' text can spend the oversampled limit
// on documents the caller does not hold.
func (l *Lexical) Search(ctx context.Context, query string, k int, keep func(id string) bool) ([]types.Scored, error) {
	if k <= 0 {
		return nil, nil
	}
	vec := queryVector(bm25.Tokenize(query))
	if len(vec.Indices) == 0 {
		return nil, nil
	}
	if err := l.Flush(ctx); err != nil {
		return nil, err
	}
	req := map[string]any{
		"query":        vec,
		"using":        SparseVector,
		"filter":       l.filter(),
		"limit":        max(k*oversample, minFetch),
		"with_payload": []string{"hash"},
	}
	var resp struct {
		Result struct {
			Points []struct {
				Score   float32 `json:"score"`
				Payload payload `json:"payload"`
			} `json:"points"`
		} `json:"result"`
	}
	if err := l.call(ctx, http.MethodPost, "/points/query", req, &resp); err != nil {
		return nil, fmt.Errorf("qdrant: lexical search: %w", err)
	}
	out := make([]types.Scored, 0, k)
	for _, p := range resp.Result.Points {
		if p.Payload.Hash == "" || (keep != nil && !keep(p.Payload.Hash)) {
			continue
		}
		out = append(out, types.Scored{ID: p.Payload.Hash, Score: p.Score})
		if len(out) == k {
			break
		}
	}
	return out, nil
}

// flushDetached flushes from a call that has no context and cannot return an
// error. A failure leaves the buffer in place and is kept for the next
// caller that can be told. l.mu must be held.
func (l *Lexical) flushDetached() {
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	_ = l.flushLocked(ctx)
}

// flushLocked sends the buffered deletions and documents. What was sent
// leaves the buffer; what failed stays for the next flush. l.mu must be held.
func (l *Lexical) flushLocked(ctx context.Context) error {
	l.err = l.sendLocked(ctx)
	return l.err
}

func (l *Lexical) sendLocked(ctx context.Context) error {
	for len(l.dels) > 0 {
		hashes := takeKeys(l.dels, batchSize)
		ids := make([]string, len(hashes))
		for i, h := range hashes {
			ids[i] = l.pointID(h)
		}
		if err := l.call(ctx, http.MethodPost, "/points/delete?wait=true", map[string]any{"points": ids}, nil); err != nil {
			return fmt.Errorf("qdrant: deleting lexical documents: %w", err)
		}
		for _, h := range hashes {
			delete(l.dels, h)
			delete(l.known, h)
		}
	}
	for len(l.puts) > 0 {
		hashes := takeKeys(l.puts, batchSize)
		points := make([]point, len(hashes))
		for i, h := range hashes {
			points[i] = point{
				ID:      l.pointID(h),
				Vector:  map[string]*sparse{SparseVector: l.puts[h]},
				Payload: payload{Repo: l.cfg.Repo, Recipe: l.cfg.Recipe, Hash: h},
			}
		}
		if err := l.call(ctx, http.MethodPut, "/points?wait=true", map[string]any{"points": points}, nil); err != nil {
			return fmt.Errorf("qdrant: storing lexical documents: %w", err)
		}
		for _, h := range hashes {
			delete(l.puts, h)
			l.known[h] = true
		}
	}
	return nil
}

// takeKeys returns up to n keys of m, sorted so a batch is reproducible.
func takeKeys[V any](m map[string]V, n int) []string {
	keys := make([]string, 0, min(n, len(m)))
	for k := range m {
		keys = append(keys, k)
		if len(keys) == n {
			break
		}
	}
	sort.Strings(keys)
	return keys
}

// documentVector weights a document's terms: BM25's saturated term frequency
// normalized by the document's length, with the parameters of the local
// index. IDF is left to Qdrant.
func documentVector(tokens []string) *sparse {
	counts := termCounts(tokens)
	norm := bm25.DefaultK1 * (1 - bm25.DefaultB + bm25.DefaultB*float64(len(tokens))/AvgDocLen)
	return sparseOf(counts, func(tf float64) float64 {
		return tf * (bm25.DefaultK1 + 1) / (tf + norm)
	})
}

// queryVector weights a query's terms by their count, which is how often the
// local index adds a repeated term's score.
func queryVector(tokens []string) *sparse {
	return sparseOf(termCounts(tokens), func(tf float64) float64 { return tf })
}

// termCounts counts tokens by dimension. Two tokens whose hashes collide
// share a dimension and are counted as one term.
func termCounts(tokens []string) map[uint32]int {
	counts := make(map[uint32]int, len(tokens))
	for _, tok := range tokens {
		h := fnv.New32a()
		h.Write([]byte(tok))
		counts[h.Sum32()]++
	}
	return counts
}

func sparseOf(counts map[uint32]int, weight func(tf float64) float64) *sparse {
	vec := &sparse{Indices: make([]uint32, 0, len(counts)), Values: make([]float32, 0, len(counts))}
	for idx := range counts {
		vec.Indices = append(vec.Indices, idx)
	}
	sort.Slice(vec.Indices, func(i, j int) bool { return vec.Indices[i] < vec.Indices[j] })
	for _, idx := range vec.Indices {
		vec.Values = append(vec.Values, float32(weight(float64(counts[idx]))))
	}
	return vec
}
