// Package vecstore provides a repo-level, content-addressed cache of node
// embedding vectors, shared by every worktree served by one wyrd daemon.
//
// A node's vector is a pure function of (node text, embedder model,
// embedding recipe). Nothing worktree-specific enters that key, so a fresh
// branch whose files are byte-identical to the ones already indexed inherits
// the entire embedding pass instead of paying for it again — which is the
// slow half of warming up a new worktree.
//
// The store sits *in front of* the embedder; the authoritative per-worktree
// index (adapter/vindex/brute) is untouched. Records are immutable and
// content-addressed, so a lost or clobbered write is only a cache miss — the
// node is re-embedded later — never corruption. That is why persistence is a
// plain atomic tmp+rename with a union against whatever is already on disk,
// and not an append-only log with locking, compaction and GC.
//
// A namespace therefore accumulates every vector the repo has ever embedded
// under that model and recipe; nothing is evicted. Deleting the file is the
// reset, and a model or recipe change opens a new namespace rather than
// growing the old one.
package vecstore

import (
	"bufio"
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kgatilin/wyrd/internal/retrieval"
)

const (
	// fileMagic tags a cache file so an unrelated or truncated file is
	// recognised as junk rather than decoded into garbage vectors.
	fileMagic = "wyrd-vecstore"

	// fileVersion is the on-disk layout version. A mismatch makes the file
	// unusable, which is treated as an empty cache (everything re-embeds).
	fileVersion = 1

	// fileExt is the extension for per-namespace cache files.
	fileExt = ".vec"
)

// fileFormat is the gob payload of one namespace file. Binary, not JSON: a
// thousand-odd 1024-wide float32 vectors are ~30MB as JSON text against ~7MB
// encoded, and JSON parsing would eat the warm-up win the cache exists to
// deliver.
type fileFormat struct {
	Magic     string
	Version   int
	Namespace string
	Vectors   map[string][]float32
}

// Store is a directory of namespaced vector caches. One Store is shared by
// every worktree State in a repo daemon; each State asks for the namespace
// matching its embedder and recipe.
//
// Safe for concurrent use.
type Store struct {
	dir string

	// mu guards caches. Held only for map lookup/insert, never across I/O.
	mu     sync.Mutex
	caches map[string]*cache

	// saveMu serializes Save so two States finishing their index passes at
	// the same time cannot interleave read-merge-write on one file.
	saveMu sync.Mutex
}

// New returns a Store over dir. The directory is not touched until the first
// Save; a missing directory reads as an empty cache.
func New(dir string) *Store {
	return &Store{
		dir:    dir,
		caches: make(map[string]*cache),
	}
}

// Namespace returns the cache for ns, creating it on first request and
// returning the same instance thereafter. The namespace string identifies
// the embedder model and the embedding recipe
// (see retrieval.VectorCacheNamespace): vectors only interchange between
// worktrees that agree on both.
func (s *Store) Namespace(ns string) retrieval.VectorCache {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.caches[ns]; ok {
		return c
	}
	c := &cache{ns: ns, path: filepath.Join(s.dir, fileName(ns)+fileExt)}
	s.caches[ns] = c
	return c
}

// Save persists every namespace that gained a vector since the last Save.
// Namespaces with nothing new are skipped entirely, so a State that indexed
// a fully-cached worktree writes no bytes at all.
func (s *Store) Save() error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	s.mu.Lock()
	caches := make([]*cache, 0, len(s.caches))
	for _, c := range s.caches {
		caches = append(caches, c)
	}
	s.mu.Unlock()

	var errs []error
	for _, c := range caches {
		if err := c.save(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// cache is one embedder+recipe namespace: an in-memory contentHash → vector
// map backed by a single file. It implements retrieval.VectorCache.
type cache struct {
	ns   string
	path string

	// once defers the (potentially multi-megabyte) file read to the first
	// lookup, so constructing a namespace stays free.
	once sync.Once

	mu      sync.RWMutex
	vectors map[string][]float32
	dirty   bool
}

// Get returns the cached vector for a content hash. The returned slice is
// shared with the cache and with every other worktree that hit the same
// hash; callers must treat it as immutable.
func (c *cache) Get(contentHash string) ([]float32, bool) {
	if contentHash == "" {
		return nil, false
	}
	c.ensureLoaded()

	c.mu.RLock()
	defer c.mu.RUnlock()
	vec, ok := c.vectors[contentHash]
	return vec, ok
}

// Put records a vector for a content hash. It is idempotent: re-putting a
// known hash is a no-op and does not mark the namespace dirty, so replaying
// an already-persisted index (the migration path in retrieval.Service.Load)
// costs nothing on disk.
func (c *cache) Put(contentHash string, vec []float32) {
	if contentHash == "" || len(vec) == 0 {
		return
	}
	c.ensureLoaded()

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.vectors[contentHash]; ok {
		return
	}
	c.vectors[contentHash] = vec
	c.dirty = true
}

func (c *cache) ensureLoaded() {
	c.once.Do(func() {
		vectors, err := readFile(c.path, c.ns)
		if err != nil {
			// A cache that cannot be read is a cold cache, never a startup
			// failure: the daemon re-embeds and rewrites the file on Save.
			fmt.Fprintf(os.Stderr, "vecstore: %v; starting with an empty cache\n", err)
			vectors = make(map[string][]float32)
		}
		c.mu.Lock()
		c.vectors = vectors
		c.mu.Unlock()
	})
}

// save writes the namespace if it holds vectors that are not on disk yet.
func (c *cache) save() error {
	c.mu.Lock()
	if !c.dirty {
		c.mu.Unlock()
		return nil
	}
	// Clear dirty before the write so vectors added while we are encoding are
	// still flagged for the next Save.
	c.dirty = false
	snapshot := make(map[string][]float32, len(c.vectors))
	for hash, vec := range c.vectors {
		snapshot[hash] = vec
	}
	c.mu.Unlock()

	if err := writeFile(c.path, c.ns, snapshot); err != nil {
		c.mu.Lock()
		c.dirty = true
		c.mu.Unlock()
		return err
	}
	return nil
}

// readFile decodes a namespace file. A missing file is an empty cache, not an
// error. Anything else — unreadable, truncated, foreign format, wrong
// namespace — is reported so the caller can log it and start empty.
func readFile(path, ns string) (map[string][]float32, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string][]float32), nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var rec fileFormat
	if err := gob.NewDecoder(bufio.NewReader(f)).Decode(&rec); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}
	if rec.Magic != fileMagic || rec.Version != fileVersion {
		return nil, fmt.Errorf("ignoring %s: unrecognised header %q v%d", path, rec.Magic, rec.Version)
	}
	if rec.Namespace != ns {
		return nil, fmt.Errorf("ignoring %s: holds namespace %q, want %q", path, rec.Namespace, ns)
	}
	if rec.Vectors == nil {
		rec.Vectors = make(map[string][]float32)
	}
	return rec.Vectors, nil
}

// writeFile persists vectors atomically, unioning in whatever is already on
// disk first. The union is what makes several States (or several daemons)
// writing the same namespace safe without locking: entries are
// content-addressed, so an overlapping hash carries an identical vector and
// neither side can lose another's work.
func writeFile(path, ns string, vectors map[string][]float32) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("vecstore: creating %s: %w", dir, err)
	}

	if onDisk, err := readFile(path, ns); err == nil {
		for hash, vec := range onDisk {
			if _, ok := vectors[hash]; !ok {
				vectors[hash] = vec
			}
		}
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp")
	if err != nil {
		return fmt.Errorf("vecstore: creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	w := bufio.NewWriter(tmp)
	rec := fileFormat{Magic: fileMagic, Version: fileVersion, Namespace: ns, Vectors: vectors}
	if err := gob.NewEncoder(w).Encode(rec); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("vecstore: encoding %s: %w", path, err)
	}
	if err := w.Flush(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("vecstore: flushing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("vecstore: closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("vecstore: renaming %s to %s: %w", tmpName, path, err)
	}
	return nil
}

// fileName maps a namespace to a filename-safe, still human-recognisable
// name (embedder ids carry ':' and '/'). Two namespaces that sanitise alike
// would share a file; the namespace recorded in the header catches that and
// the losing side simply runs cold.
func fileName(ns string) string {
	var sb strings.Builder
	for _, r := range ns {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-', r == '@':
			sb.WriteRune(r)
		default:
			sb.WriteRune('-')
		}
	}
	name := strings.Trim(sb.String(), "-")
	if name == "" {
		return "unnamed"
	}
	return name
}
