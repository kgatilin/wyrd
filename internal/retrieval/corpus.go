package retrieval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
)

// embedBatchNodes is how many nodes one embedder call carries. Each batch is
// stored as it lands, so readiness climbs during a pass and an embedder
// failure keeps what was stored before it.
const embedBatchNodes = 64

// Corpus is the repository's text channels, shared by every view (Service) of
// the daemon: a lexical index and a dense vector store, both keyed by content
// hash. A hash's text is identical wherever it occurs, so it is tokenized
// once and embedded once for the repository, by whichever view meets it
// first.
//
// Safe for concurrent use.
type Corpus struct {
	// embedder and vectors are both nil, or both set: the dense channel
	// exists only with both.
	embedder Embedder
	vectors  VectorStore

	lexical LexicalIndex

	// lexicalPath is where the lexical index is persisted; "" keeps it in
	// memory only.
	lexicalPath string

	// mu guards refs and serializes lexical upserts/removals with them.
	mu sync.Mutex
	// refs counts the views holding each hash. The first acquire indexes the
	// text, the last release removes it.
	refs map[string]int

	// embedMu admits one embedding pass at a time: two views missing the same
	// hash cannot both embed it, and the embedder sees one pass, not one per
	// worktree.
	embedMu sync.Mutex

	// saveMu serializes Save.
	saveMu sync.Mutex

	// faultMu guards faults: the last failure of each channel that has not
	// succeeded since.
	faultMu sync.Mutex
	faults  map[string]string
}

// The channels a fault is recorded under.
const (
	FaultEmbedder = "embedder"
	FaultVectors  = "vectors"
	FaultLexical  = "lexical"
)

// report records the outcome of a call to a channel's backend: an error
// becomes the channel's fault, a success clears it.
func (c *Corpus) report(channel string, err error) {
	c.faultMu.Lock()
	defer c.faultMu.Unlock()
	if err == nil {
		delete(c.faults, channel)
		return
	}
	if c.faults == nil {
		c.faults = make(map[string]string)
	}
	c.faults[channel] = err.Error()
}

// Faults returns the channels whose backend failed the last time it was
// called, with the error. A search goes on answering from the channels that
// work, so this is where a backend that is down shows: empty means every
// channel answered its last call.
func (c *Corpus) Faults() map[string]string {
	c.faultMu.Lock()
	defer c.faultMu.Unlock()
	out := make(map[string]string, len(c.faults))
	for channel, msg := range c.faults {
		out[channel] = msg
	}
	return out
}

// Recheck calls the backend of every channel at fault once, clears the faults
// of those that answer, and returns what is left. It is how a daemon taken
// out of rotation for a fault comes back: nothing else would call a backend
// while no search arrives. With no fault it calls nothing.
func (c *Corpus) Recheck(ctx context.Context) map[string]string {
	for channel := range c.Faults() {
		var err error
		switch channel {
		case FaultEmbedder:
			if c.embedder != nil {
				_, err = c.embedder.EmbedQuery(ctx, "readiness probe")
			}
		case FaultVectors:
			if c.vectors != nil {
				_, err = c.vectors.Get(ctx, []string{"readiness-probe"})
			}
		case FaultLexical:
			_, err = c.lexical.Search(ctx, "readiness probe", 1, nil)
		}
		c.report(channel, err)
	}
	return c.Faults()
}

// lexicalFlusher is a LexicalIndex that buffers its writes.
type lexicalFlusher interface {
	Flush(ctx context.Context) error
}

// lexicalRetainer is a LexicalIndex that is its own persistence and has to
// be told which documents are still held, where a file would simply not be
// written with the others.
type lexicalRetainer interface {
	Retain(ctx context.Context, keep func(id string) bool) error
}

// lexicalPersister is a LexicalIndex that can be written to and read from a
// file. Save writes only the documents keep accepts.
type lexicalPersister interface {
	Save(path string, keep func(id string) bool) error
	Load(path string) error
}

// NewCorpus returns a corpus over the given channels. A nil embedder or vector
// store leaves the corpus lexical-only. lexicalPath "" keeps the lexical index
// in memory only; otherwise Load and Save read and write it there.
func NewCorpus(emb Embedder, vectors VectorStore, lexical LexicalIndex, lexicalPath string) *Corpus {
	if emb == nil || vectors == nil {
		emb, vectors = nil, nil
	}
	return &Corpus{
		embedder:    emb,
		vectors:     vectors,
		lexical:     lexical,
		lexicalPath: lexicalPath,
		refs:        make(map[string]int),
	}
}

// Load restores the persisted lexical index. A missing file is an empty
// index.
func (c *Corpus) Load() error {
	p, ok := c.lexical.(lexicalPersister)
	if !ok || c.lexicalPath == "" {
		return nil
	}
	if err := p.Load(c.lexicalPath); err != nil {
		return fmt.Errorf("loading lexical index: %w", err)
	}
	return nil
}

// Save persists the lexical documents some view holds, and the vectors stored
// since the last save.
func (c *Corpus) Save() error {
	c.saveMu.Lock()
	defer c.saveMu.Unlock()

	var errs []error
	if p, ok := c.lexical.(lexicalPersister); ok && c.lexicalPath != "" {
		// The membership set is copied first: Save holds the index's read
		// lock while it streams, and acquire takes c.mu before the index's
		// write lock, so asking c.mu from inside Save could deadlock.
		held := c.heldSet()
		keep := func(id string) bool { return held[id] }
		if err := p.Save(c.lexicalPath, keep); err != nil {
			errs = append(errs, fmt.Errorf("saving lexical index: %w", err))
		}
	}
	if r, ok := c.lexical.(lexicalRetainer); ok {
		// Under c.mu for the whole call: a hash acquired while documents are
		// being dropped would find its document present, skip the upsert,
		// and then lose it.
		c.mu.Lock()
		err := r.Retain(context.Background(), func(id string) bool { return c.refs[id] > 0 })
		c.mu.Unlock()
		c.report(FaultLexical, err)
		if err != nil {
			errs = append(errs, fmt.Errorf("saving lexical index: %w", err))
		}
	}
	if c.vectors != nil {
		if err := c.vectors.Save(); err != nil {
			errs = append(errs, fmt.Errorf("saving vectors: %w", err))
		}
	}
	return errors.Join(errs...)
}

// EmbedderID names the embedder behind the dense channel, "" without one.
func (c *Corpus) EmbedderID() string {
	if c.embedder == nil {
		return ""
	}
	return c.embedder.ID()
}

// dense reports whether the corpus has a dense channel at all.
func (c *Corpus) dense() bool {
	return c.embedder != nil && c.vectors != nil
}

// heldSet returns the hashes some view holds.
func (c *Corpus) heldSet() map[string]bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	set := make(map[string]bool, len(c.refs))
	for h := range c.refs {
		set[h] = true
	}
	return set
}

// acquire records that one more view holds hash, indexing its text when no
// document exists for it yet. text is only called then.
func (c *Corpus) acquire(hash string, text func() string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refs[hash]++
	if c.refs[hash] == 1 && !c.lexical.Has(hash) {
		c.lexical.Upsert(hash, text())
	}
}

// release records that one view no longer holds hash, removing its document
// when it was the last.
func (c *Corpus) release(hash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.refs[hash] - 1
	if n > 0 {
		c.refs[hash] = n
		return
	}
	delete(c.refs, hash)
	c.lexical.Remove(hash)
}

// flushLexical sends the lexical writes a remote index has buffered, so a
// view that has just acquired its texts does not leave them in memory until
// the first search.
func (c *Corpus) flushLexical(ctx context.Context) {
	f, ok := c.lexical.(lexicalFlusher)
	if !ok {
		return
	}
	err := f.Flush(ctx)
	c.report(FaultLexical, err)
	if err != nil {
		fmt.Fprintf(os.Stderr, "retrieval: storing lexical documents failed: %v\n", err)
	}
}

// hasVector reports whether the dense channel holds a vector for hash.
func (c *Corpus) hasVector(hash string) bool {
	return c.dense() && c.vectors.Has(hash)
}

// embed stores a vector for every node whose hash has none, one pass at a
// time across the whole corpus. The check is repeated after the lock is
// taken, so what another view embedded meanwhile is not embedded again.
// root is the asking view's root, where the nodes' source is read.
func (c *Corpus) embed(ctx context.Context, nodes []Node, hashOf map[string]string, root string) error {
	if !c.dense() {
		return nil
	}
	c.embedMu.Lock()
	defer c.embedMu.Unlock()

	seen := make(map[string]bool, len(nodes))
	pending := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		h := hashOf[n.ID]
		if h == "" || seen[h] || c.vectors.Has(h) {
			continue
		}
		seen[h] = true
		pending = append(pending, n)
	}

	for start := 0; start < len(pending); start += embedBatchNodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(start+embedBatchNodes, len(pending))
		vecs, err := c.embedBatch(ctx, pending[start:end], root)
		if ctx.Err() == nil {
			c.report(FaultEmbedder, err)
		}
		if err != nil {
			return err
		}
		batch := make(map[string][]float32, len(vecs))
		for i, n := range pending[start:end] {
			batch[hashOf[n.ID]] = vecs[i]
		}
		err = c.vectors.Put(ctx, batch)
		if ctx.Err() == nil {
			c.report(FaultVectors, err)
		}
		if err != nil {
			return fmt.Errorf("storing vectors: %w", err)
		}
	}
	return nil
}

// embedBatch embeds nodes, one vector per node: an oversized node is split
// into chunks whose vectors are mean-pooled.
func (c *Corpus) embedBatch(ctx context.Context, nodes []Node, root string) ([][]float32, error) {
	var texts []string
	counts := make([]int, len(nodes))
	for i, n := range nodes {
		chunks, err := BuildChunks(n, root)
		if err != nil || len(chunks) == 0 {
			text, _ := EmbedText(n, root)
			texts = append(texts, text)
			counts[i] = 1
			continue
		}
		for _, ch := range chunks {
			texts = append(texts, ch.Text)
		}
		counts[i] = len(chunks)
	}

	vectors, err := c.embedder.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("calling embedder: %w", err)
	}
	if len(vectors) != len(texts) {
		return nil, fmt.Errorf("embedder returned %d vectors for %d texts", len(vectors), len(texts))
	}

	out := make([][]float32, len(nodes))
	at := 0
	for i, n := range counts {
		if n == 1 {
			out[i] = vectors[at]
		} else {
			out[i] = MeanPoolVectors(vectors[at : at+n])
		}
		at += n
	}
	return out, nil
}
