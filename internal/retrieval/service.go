package retrieval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/kgatilin/wyrd/internal/domain"
)

// Service is one worktree's view of the repository's corpus: which content
// hash each of its nodes has, and the graph the diffusion walks. Search,
// expand, readiness and coverage are answered by the view against the shared
// corpus, so N worktrees hold one corpus and N views.
type Service struct {
	mu sync.RWMutex

	// root is the project root directory, where node source is read.
	root string

	// corpus holds the text channels shared with the daemon's other views.
	corpus *Corpus

	// view maps node ids to content hashes and back. Immutable once
	// published: an update builds the next one and swaps it under mu.
	view *view

	// updateMu serializes view updates, so two updates cannot both build on
	// the same previous view.
	updateMu sync.Mutex

	// graph holds the adjacency map for expand/neighbor operations.
	graph *Graph

	// params are the tuning knobs of the search pipeline (fusion weights,
	// softmax temperatures, diffusion constants).
	params Params

	// denseAvailable tracks whether dense search is operational for this
	// view. Set to false if the embedder fails during this view's pass.
	denseAvailable bool

	// embedding counts this view's embedding passes in flight, including
	// one waiting for the corpus's embed lock behind another view. Read by
	// IndexStatus so callers can report "indexing" instead of blocking.
	embedding int

	// idxStartedAt is when the current (or last) embedding pass began.
	idxStartedAt time.Time
}

// view is the node id ↔ content hash mapping of one worktree.
type view struct {
	hashOf  map[string]string   // node id → content hash
	nodesOf map[string][]string // content hash → node ids (texts can repeat)
}

func emptyView() *view {
	return &view{hashOf: map[string]string{}, nodesOf: map[string][]string{}}
}

// has reports whether the view holds a node with this hash.
func (v *view) has(hash string) bool {
	_, ok := v.nodesOf[hash]
	return ok
}

// clone returns a copy the caller may modify.
func (v *view) clone() *view {
	next := &view{
		hashOf:  make(map[string]string, len(v.hashOf)),
		nodesOf: make(map[string][]string, len(v.nodesOf)),
	}
	for id, h := range v.hashOf {
		next.hashOf[id] = h
	}
	for h, ids := range v.nodesOf {
		next.nodesOf[h] = ids
	}
	return next
}

// remove drops a node. The id slice is replaced, never written: it may be
// shared with the previous view.
func (v *view) remove(id string) {
	h, ok := v.hashOf[id]
	if !ok {
		return
	}
	delete(v.hashOf, id)
	ids := v.nodesOf[h]
	kept := make([]string, 0, len(ids))
	for _, other := range ids {
		if other != id {
			kept = append(kept, other)
		}
	}
	if len(kept) == 0 {
		delete(v.nodesOf, h)
		return
	}
	v.nodesOf[h] = kept
}

// add maps a node to its hash, replacing any previous mapping.
func (v *view) add(id, hash string) {
	if old, ok := v.hashOf[id]; ok {
		if old == hash {
			return
		}
		v.remove(id)
	}
	v.hashOf[id] = hash
	ids := v.nodesOf[hash]
	v.nodesOf[hash] = append(ids[:len(ids):len(ids)], id)
}

// IndexStatus is a cheap, non-blocking snapshot of the view's readiness:
// whether an embedding pass is running, how many embeddable nodes have
// vectors so far, and whether dense search is operational.
type IndexStatus struct {
	InProgress     bool      `json:"in_progress"`
	DenseAvailable bool      `json:"dense_available"`
	Embedded       int       `json:"embedded"`   // embeddable nodes with a vector now
	Embeddable     int       `json:"embeddable"` // nodes that should carry a vector
	Pending        int       `json:"pending"`    // embeddable not yet embedded
	StartedAt      time.Time `json:"started_at,omitempty"`
}

// IndexStatus returns a snapshot of the view's readiness, computed from the
// view's own hashes: Embedded climbs toward Embeddable as the corpus stores
// vectors, whichever view's pass stored them.
func (s *Service) IndexStatus() IndexStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := IndexStatus{
		InProgress:     s.embedding > 0,
		DenseAvailable: s.denseAvailable,
		StartedAt:      s.idxStartedAt,
	}
	if s.graph != nil {
		for id, n := range s.graph.NodesByID {
			if !n.Embeddable {
				continue
			}
			st.Embeddable++
			if h := s.view.hashOf[id]; h != "" && s.corpus.hasVector(h) {
				st.Embedded++
			}
		}
	}
	st.Pending = st.Embeddable - st.Embedded
	return st
}

// NewService returns an empty view over corpus. Every worktree of one
// repository should be given the same corpus: sharing is what it is for.
func NewService(root string, corpus *Corpus) *Service {
	return &Service{
		root:           root,
		corpus:         corpus,
		view:           emptyView(),
		denseAvailable: corpus.dense(),
		params:         DefaultParams(),
	}
}

// Save persists the corpus.
func (s *Service) Save() error {
	return s.corpus.Save()
}

// Index replaces the view with nodes and embeds what the corpus lacks.
// If embedding fails, dense search is disabled for this view and the error
// is logged; the lexical channel keeps working.
func (s *Service) Index(ctx context.Context, nodes []Node) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.update(nodes, nil, true)
	return s.embedNodes(ctx, nodes)
}

// Update applies a delta to the view: nodes are added or re-hashed, removed
// ids are dropped. It touches the lexical channel only; EmbedPending brings
// the dense channel up to date, so an update never waits for embedding.
func (s *Service) Update(ctx context.Context, nodes []Node, removedIDs []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.update(nodes, removedIDs, false)
	return nil
}

// update builds the next view, acquires the hashes it gained, publishes it,
// and releases the hashes it lost — in that order, so a search never sees a
// hash the corpus has not indexed.
func (s *Service) update(nodes []Node, removedIDs []string, replace bool) {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()

	hashes := hashNodes(nodes, s.root)

	s.mu.RLock()
	prev := s.view
	s.mu.RUnlock()

	next := emptyView()
	if !replace {
		next = prev.clone()
		for _, id := range removedIDs {
			next.remove(id)
		}
	}
	textOf := make(map[string]Node, len(nodes))
	for _, n := range nodes {
		h := hashes[n.ID]
		next.add(n.ID, h)
		textOf[h] = n
	}

	for h := range next.nodesOf {
		if prev.has(h) {
			continue
		}
		n := textOf[h]
		s.corpus.acquire(h, func() string {
			text, _ := EmbedText(n, s.root)
			return text
		})
	}

	s.mu.Lock()
	s.view = next
	s.mu.Unlock()

	for h := range prev.nodesOf {
		if !next.has(h) {
			s.corpus.release(h)
		}
	}
}

// EmbedPending embeds every embeddable node of the current graph whose hash
// has no vector. It is idempotent and order-free: whatever updates landed
// before it, it converges the dense channel on the view as it is now. An
// embedding failure disables dense search for this view and is logged, not
// returned: the lexical channel still answers.
func (s *Service) EmbedPending(ctx context.Context) error {
	var nodes []Node
	if g := s.Graph(); g != nil {
		nodes = make([]Node, 0, len(g.NodesByID))
		for _, n := range g.NodesByID {
			nodes = append(nodes, n)
		}
	}
	return s.embedNodes(ctx, nodes)
}

// embedNodes embeds the embeddable nodes whose hash in the current view has
// no vector yet.
func (s *Service) embedNodes(ctx context.Context, nodes []Node) error {
	// The update before this pass acquired the view's texts; a remote
	// lexical index still holds the tail of them in its buffer.
	s.corpus.flushLexical(ctx)

	if !s.corpus.dense() {
		s.setDenseAvailable(false)
		return nil
	}

	s.mu.Lock()
	s.embedding++
	s.idxStartedAt = time.Now()
	v := s.view
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.embedding--
		s.mu.Unlock()
	}()

	var pending []Node
	for _, n := range nodes {
		if h := v.hashOf[n.ID]; n.Embeddable && h != "" && !s.corpus.hasVector(h) {
			pending = append(pending, n)
		}
	}
	// Stable order: the embedder's batches, and so its failures, repeat.
	sort.Slice(pending, func(i, j int) bool { return pending[i].ID < pending[j].ID })

	if err := s.corpus.embed(ctx, pending, v.hashOf, s.root); err != nil {
		fmt.Fprintf(os.Stderr, "retrieval: embedding failed, dense search disabled: %v\n", err)
		s.setDenseAvailable(false)
		return nil
	}
	s.setDenseAvailable(true)
	return nil
}

// DenseAvailable reports whether dense (vector) search is operational.
func (s *Service) DenseAvailable() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.denseAvailable
}

func (s *Service) setDenseAvailable(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.denseAvailable = v
}

// Vectors returns the embeddings of the given node ids, in one batched fetch
// from the corpus. An id without a vector is absent from the map. It
// implements clustering.Vectors.
func (s *Service) Vectors(ctx context.Context, ids []string) (map[string][]float32, error) {
	out := make(map[string][]float32, len(ids))
	if !s.corpus.dense() {
		return out, nil
	}
	s.mu.RLock()
	v := s.view
	s.mu.RUnlock()

	hashes := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if h := v.hashOf[id]; h != "" && !seen[h] {
			seen[h] = true
			hashes = append(hashes, h)
		}
	}
	if len(hashes) == 0 {
		return out, nil
	}
	byHash, err := s.corpus.vectors.Get(ctx, hashes)
	if err != nil {
		return nil, fmt.Errorf("fetching vectors: %w", err)
	}
	for _, id := range ids {
		if vec, ok := byHash[v.hashOf[id]]; ok {
			out[id] = vec
		}
	}
	return out, nil
}

// Embedded reports whether the node carries a vector.
func (s *Service) Embedded(id string) bool {
	s.mu.RLock()
	h := s.view.hashOf[id]
	s.mu.RUnlock()
	return h != "" && s.corpus.hasVector(h)
}

// Root returns the project root directory.
func (s *Service) Root() string {
	return s.root
}

// SetGraph updates the service's graph adjacency map.
// Called when the model is refreshed.
func (s *Service) SetGraph(g *Graph) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.graph = g
}

// Graph returns the current graph.
func (s *Service) Graph() *Graph {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.graph
}

// EmbedderID returns the identifier of the corpus's embedder (e.g. an Ollama
// model name, or "noop"). Empty when the corpus has no dense channel.
func (s *Service) EmbedderID() string {
	return s.corpus.EmbedderID()
}

// IndexFromModels builds nodes and graph from domain models, then indexes.
// This is a convenience method that combines BuildGraph + SetGraph + Index.
func (s *Service) IndexFromModels(ctx context.Context, models []domain.PackageModel) error {
	nodes, graph := BuildGraph(models)
	s.SetGraph(graph)
	return s.Index(ctx, nodes)
}

// hashNodes returns every node's content hash. Nodes are visited file by
// file and each source file is read once, where ContentHash would read it
// once per node.
func hashNodes(nodes []Node, root string) map[string]string {
	order := make([]int, len(nodes))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return nodes[order[a]].Span.File < nodes[order[b]].Span.File
	})

	hashes := make(map[string]string, len(nodes))
	var (
		file string
		data []byte
		err  error
	)
	for _, i := range order {
		n := nodes[i]
		body := ""
		if n.Span.IsValid() {
			if n.Span.File != file {
				file = n.Span.File
				data, err = os.ReadFile(filepath.Join(root, file))
			}
			if err == nil && n.Span.StartByte >= 0 && n.Span.EndByte <= len(data) && n.Span.StartByte < n.Span.EndByte {
				body = string(data[n.Span.StartByte:n.Span.EndByte])
			}
		}
		// Must stay the hash ContentHash computes: header + body, the body
		// left out when it cannot be read.
		sum := sha256.Sum256([]byte(buildHeader(n) + body))
		hashes[n.ID] = hex.EncodeToString(sum[:])
	}
	return hashes
}
