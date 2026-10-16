// Package qdrant holds the corpus's two text channels in a Qdrant collection
// instead of in the daemon's memory and files: dense vectors (Store) and BM25
// documents as sparse vectors (Lexical), both keyed by content hash.
//
// A collection has a named dense vector of the embedder's dimension with
// Cosine distance and a named sparse vector with the IDF modifier (see
// Bootstrap). The two channels keep separate points: a text is tokenized
// before it is embedded, the lexical recipe does not depend on the embedder,
// and an upsert replaces a point whole, so sharing one would make each
// channel's write a read-modify-write of the other's. A collection created
// before the sparse channel existed, with a single unnamed vector, is still
// read and written as the dense channel alone (Schema.Legacy); Migrate copies
// it into a named one.
//
// The token the daemon gets can read and write points but not create a
// collection, so Open only verifies it. Points are addressed by a UUID v5 of
// the recipe and the content hash, and carry the repository key, the recipe
// and the hash as payload; every query filters on repository and recipe, so
// one collection can serve several repositories and recipes. Plain REST over
// net/http: the subset used here is small and a client library would be the
// larger dependency.
package qdrant

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kgatilin/wyrd/internal/retrieval/types"
)

const (
	// DenseVector and SparseVector name the collection's two vectors.
	DenseVector  = "dense"
	SparseVector = "bm25"

	// batchSize bounds the points of one upsert or retrieve request.
	batchSize = 256

	// scrollPage is the page size of the startup scroll.
	scrollPage = 1000

	// oversample multiplies k for a filtered search, since the caller's
	// membership test is applied after Qdrant ranks; minFetch is its floor.
	oversample = 8
	minFetch   = 100
)

// Config locates the collection and the slice of it this daemon owns.
type Config struct {
	URL        string // base URL, e.g. https://qdrant.example:6333
	APIKey     string // sent as the api-key header when non-empty
	Collection string

	Repo   string // repository key, stored as payload and filtered on
	Recipe string // the channel's recipe, stored as payload and filtered on
	Dim    int    // the embedder's dimension the dense vector must match

	// HTTP is the client to use; nil means one with a 30s timeout.
	HTTP *http.Client
}

// client is what both channels share: the collection's address and the
// slice of it one repository and recipe own.
type client struct {
	cfg  Config
	http *http.Client
	ns   [16]byte // UUID v5 namespace of the recipe
}

func newClient(cfg Config) (*client, error) {
	if cfg.URL == "" || cfg.Collection == "" {
		return nil, fmt.Errorf("qdrant: URL and collection are required")
	}
	c := &client{cfg: cfg, http: cfg.HTTP, ns: uuidV5(urlNamespace, "wyrd:"+cfg.Recipe)}
	if c.http == nil {
		c.http = &http.Client{Timeout: 30 * time.Second}
	}
	return c, nil
}

// Schema is the vector configuration of a collection.
type Schema struct {
	// Legacy is a collection with a single unnamed vector: the dense channel
	// alone, as collections were created before the sparse one existed.
	Legacy bool

	DenseSize     int    // 0 when the collection has no dense vector
	DenseDistance string // as Qdrant reports it, e.g. "Cosine"

	Sparse    bool // the named sparse vector exists
	SparseIDF bool // and Qdrant applies IDF to it
}

// Inspect reads the collection's schema. A collection that does not exist is
// reported as ErrNoCollection.
func Inspect(ctx context.Context, cfg Config) (Schema, error) {
	c, err := newClient(cfg)
	if err != nil {
		return Schema{}, err
	}
	return c.schema(ctx)
}

// ErrNoCollection is returned for a collection Qdrant does not have.
var ErrNoCollection = errors.New("qdrant: collection does not exist")

func (c *client) schema(ctx context.Context) (Schema, error) {
	var resp struct {
		Result struct {
			Config struct {
				Params struct {
					Vectors       json.RawMessage `json:"vectors"`
					SparseVectors map[string]struct {
						Modifier string `json:"modifier"`
					} `json:"sparse_vectors"`
				} `json:"params"`
			} `json:"config"`
		} `json:"result"`
	}
	if err := c.call(ctx, http.MethodGet, "", nil, &resp); err != nil {
		var status *statusError
		if errors.As(err, &status) && status.code == http.StatusNotFound {
			return Schema{}, fmt.Errorf("%w: %q", ErrNoCollection, c.cfg.Collection)
		}
		return Schema{}, fmt.Errorf("qdrant: reading collection %q: %w", c.cfg.Collection, err)
	}
	type vectorParams struct {
		Size     int    `json:"size"`
		Distance string `json:"distance"`
	}
	var schema Schema
	params := resp.Result.Config.Params
	var unnamed vectorParams
	var named map[string]vectorParams
	if err := json.Unmarshal(params.Vectors, &unnamed); err == nil && unnamed.Size > 0 {
		schema.Legacy = true
		schema.DenseSize, schema.DenseDistance = unnamed.Size, unnamed.Distance
	} else if err := json.Unmarshal(params.Vectors, &named); err == nil {
		schema.DenseSize, schema.DenseDistance = named[DenseVector].Size, named[DenseVector].Distance
	}
	if sparse, ok := params.SparseVectors[SparseVector]; ok {
		schema.Sparse = true
		schema.SparseIDF = strings.EqualFold(sparse.Modifier, "idf")
	}
	return schema, nil
}

// checkDense reports why the collection cannot hold this embedder's vectors.
func (c *client) checkDense(schema Schema) error {
	if schema.DenseSize == 0 {
		return fmt.Errorf("qdrant: collection %q has neither an unnamed vector nor one named %q", c.cfg.Collection, DenseVector)
	}
	if schema.DenseSize != c.cfg.Dim {
		return fmt.Errorf("qdrant: collection %q has vector size %d, the embedder produces %d", c.cfg.Collection, schema.DenseSize, c.cfg.Dim)
	}
	if !strings.EqualFold(schema.DenseDistance, "Cosine") {
		return fmt.Errorf("qdrant: collection %q uses %s distance, want Cosine", c.cfg.Collection, schema.DenseDistance)
	}
	return nil
}

// scrollHashes lists the hashes the collection holds for this repository
// and recipe, payload only.
func (c *client) scrollHashes(ctx context.Context) (map[string]bool, error) {
	known := make(map[string]bool)
	var offset any
	for {
		req := map[string]any{
			"filter":       c.filter(),
			"limit":        scrollPage,
			"with_payload": []string{"hash"},
			"with_vector":  false,
		}
		if offset != nil {
			req["offset"] = offset
		}
		var resp struct {
			Result struct {
				Points []struct {
					Payload payload `json:"payload"`
				} `json:"points"`
				NextPageOffset any `json:"next_page_offset"`
			} `json:"result"`
		}
		if err := c.call(ctx, http.MethodPost, "/points/scroll", req, &resp); err != nil {
			return nil, fmt.Errorf("qdrant: listing stored hashes: %w", err)
		}
		for _, p := range resp.Result.Points {
			if p.Payload.Hash != "" {
				known[p.Payload.Hash] = true
			}
		}
		if resp.Result.NextPageOffset == nil || len(resp.Result.Points) == 0 {
			return known, nil
		}
		offset = resp.Result.NextPageOffset
	}
}

// Store implements retrieval.VectorStore over a Qdrant collection. Only the
// set of hashes the collection holds for this repository and recipe is kept
// in memory, never a vector. Safe for concurrent use.
type Store struct {
	*client
	schema Schema

	mu    sync.RWMutex
	known map[string]bool
}

// Open verifies the collection — it exists and its dense vector, unnamed or
// named DenseVector, has cfg.Dim dimensions with Cosine distance — and reads
// which hashes it already holds for cfg.Repo and cfg.Recipe. Any failure is
// returned; the caller decides whether to fall back.
func Open(ctx context.Context, cfg Config) (*Store, error) {
	c, err := newClient(cfg)
	if err != nil {
		return nil, err
	}
	schema, err := c.schema(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.checkDense(schema); err != nil {
		return nil, err
	}
	known, err := c.scrollHashes(ctx)
	if err != nil {
		return nil, err
	}
	return &Store{client: c, schema: schema, known: known}, nil
}

// Schema is the collection's schema as read at Open.
func (s *Store) Schema() Schema { return s.schema }

// Has reports whether the collection holds a vector for hash, as of the
// startup scroll plus this process's puts.
func (s *Store) Has(hash string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.known[hash]
}

// Get fetches the vectors of hashes by point id, in batches.
func (s *Store) Get(ctx context.Context, hashes []string) (map[string][]float32, error) {
	out := make(map[string][]float32, len(hashes))
	for start := 0; start < len(hashes); start += batchSize {
		batch := hashes[start:min(start+batchSize, len(hashes))]
		ids := make([]string, len(batch))
		for i, h := range batch {
			ids[i] = s.pointID(h)
		}
		var resp struct {
			Result []struct {
				Vector  json.RawMessage `json:"vector"`
				Payload payload         `json:"payload"`
			} `json:"result"`
		}
		var withVector any = true
		if !s.schema.Legacy {
			withVector = []string{DenseVector}
		}
		req := map[string]any{"ids": ids, "with_payload": []string{"hash"}, "with_vector": withVector}
		if err := s.call(ctx, http.MethodPost, "/points", req, &resp); err != nil {
			return nil, fmt.Errorf("qdrant: fetching vectors: %w", err)
		}
		for _, p := range resp.Result {
			if vec := s.decodeVector(p.Vector); p.Payload.Hash != "" && len(vec) > 0 {
				out[p.Payload.Hash] = vec
			}
		}
	}
	return out, nil
}

// Put upserts vectors in batches, waiting for each to be applied.
func (s *Store) Put(ctx context.Context, vectors map[string][]float32) error {
	batch := make([]point, 0, min(batchSize, len(vectors)))
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.call(ctx, http.MethodPut, "/points?wait=true", map[string]any{"points": batch}, nil); err != nil {
			return fmt.Errorf("qdrant: storing vectors: %w", err)
		}
		s.mu.Lock()
		for _, p := range batch {
			s.known[p.Payload.Hash] = true
		}
		s.mu.Unlock()
		batch = batch[:0]
		return nil
	}
	for h, vec := range vectors {
		if h == "" || len(vec) == 0 {
			continue
		}
		batch = append(batch, point{
			ID:      s.pointID(h),
			Vector:  s.encodeVector(vec),
			Payload: payload{Repo: s.cfg.Repo, Recipe: s.cfg.Recipe, Hash: h},
		})
		if len(batch) == batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

// Search runs a kNN filtered on repository and recipe, oversampled because
// keep can only be applied to what comes back, and returns at most k hashes
// keep accepts. Not exact: a long-lived collection can spend the oversampled
// limit on texts the caller does not hold.
func (s *Store) Search(ctx context.Context, vec []float32, k int, keep func(hash string) bool) ([]types.Scored, error) {
	if k <= 0 {
		return nil, nil
	}
	limit := max(k*oversample, minFetch)
	var query any = vec
	if !s.schema.Legacy {
		query = map[string]any{"name": DenseVector, "vector": vec}
	}
	req := map[string]any{
		"vector":       query,
		"limit":        limit,
		"filter":       s.filter(),
		"with_payload": []string{"hash"},
	}
	var resp struct {
		Result []struct {
			Score   float32 `json:"score"`
			Payload payload `json:"payload"`
		} `json:"result"`
	}
	if err := s.call(ctx, http.MethodPost, "/points/search", req, &resp); err != nil {
		return nil, fmt.Errorf("qdrant: searching: %w", err)
	}
	out := make([]types.Scored, 0, k)
	for _, r := range resp.Result {
		if r.Payload.Hash == "" || (keep != nil && !keep(r.Payload.Hash)) {
			continue
		}
		out = append(out, types.Scored{ID: r.Payload.Hash, Score: r.Score})
		if len(out) == k {
			break
		}
	}
	return out, nil
}

// Save does nothing: the collection is its own persistence.
func (s *Store) Save() error { return nil }

// Seed upserts every vector scan yields whose hash the collection does not
// hold yet, in batches, and returns how many it stored. Nothing is embedded:
// scan replays vectors computed before, e.g. the local vector file.
func (s *Store) Seed(ctx context.Context, scan func(fn func(hash string, vec []float32) error) error) (int, error) {
	stored := 0
	batch := make(map[string][]float32, batchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.Put(ctx, batch); err != nil {
			return err
		}
		stored += len(batch)
		clear(batch)
		return nil
	}
	err := scan(func(hash string, vec []float32) error {
		if s.Has(hash) || batch[hash] != nil {
			return nil
		}
		batch[hash] = vec
		if len(batch) == batchSize {
			return flush()
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	return stored, err
}

// encodeVector is a dense vector as a point carries it: bare in a legacy
// collection, under its name otherwise.
func (s *Store) encodeVector(vec []float32) any {
	if s.schema.Legacy {
		return vec
	}
	return map[string][]float32{DenseVector: vec}
}

// decodeVector reads a point's dense vector in either shape.
func (s *Store) decodeVector(raw json.RawMessage) []float32 {
	if s.schema.Legacy {
		var vec []float32
		_ = json.Unmarshal(raw, &vec)
		return vec
	}
	var named map[string]json.RawMessage
	if json.Unmarshal(raw, &named) != nil {
		return nil
	}
	var vec []float32
	_ = json.Unmarshal(named[DenseVector], &vec)
	return vec
}

// filter confines a query to this repository and recipe.
func (c *client) filter() map[string]any {
	return map[string]any{"must": []map[string]any{
		{"key": "repo", "match": map[string]any{"value": c.cfg.Repo}},
		{"key": "recipe", "match": map[string]any{"value": c.cfg.Recipe}},
	}}
}

// pointID is the point id of a hash: a UUID v5 under the recipe's namespace,
// so one text has one point per recipe whichever daemon stores it.
func (c *client) pointID(hash string) string {
	return formatUUID(uuidV5(c.ns, hash))
}

type point struct {
	ID      string  `json:"id"`
	Vector  any     `json:"vector,omitempty"`
	Payload payload `json:"payload"`
}

type payload struct {
	Repo   string `json:"repo,omitempty"`
	Recipe string `json:"recipe,omitempty"`
	Hash   string `json:"hash"`
}

// statusError is a non-2xx answer, carrying the start of its body.
type statusError struct {
	code    int
	status  string
	excerpt string
}

func (e *statusError) Error() string { return e.status + ": " + e.excerpt }

// call sends one request to the collection and decodes the response into
// out (when non-nil). A non-2xx status is a *statusError.
func (c *client) call(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request: %w", err)
		}
		rd = bytes.NewReader(data)
	}
	endpoint := strings.TrimRight(c.cfg.URL, "/") + "/collections/" + url.PathEscape(c.cfg.Collection) + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, rd)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cfg.APIKey != "" {
		req.Header.Set("api-key", c.cfg.APIKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: %w", method, path, &statusError{
			code: resp.StatusCode, status: resp.Status, excerpt: strings.TrimSpace(string(excerpt)),
		})
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s %s: decoding response: %w", method, path, err)
	}
	return nil
}

// urlNamespace is the RFC 4122 namespace for URLs.
var urlNamespace = [16]byte{0x6b, 0xa7, 0xb8, 0x11, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8}

// uuidV5 is the RFC 4122 name-based (SHA-1) UUID of name in namespace.
func uuidV5(namespace [16]byte, name string) [16]byte {
	h := sha1.New()
	h.Write(namespace[:])
	h.Write([]byte(name))
	var u [16]byte
	copy(u[:], h.Sum(nil))
	u[6] = (u[6] & 0x0f) | 0x50
	u[8] = (u[8] & 0x3f) | 0x80
	return u
}

func formatUUID(u [16]byte) string {
	s := hex.EncodeToString(u[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}
