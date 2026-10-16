// Package vecstore is the corpus's local dense channel: one in-memory,
// brute-force vector index per repository and embedding recipe, keyed by
// content hash and backed by one file,
// <wyrd home>/embeddings/<repo key>/<embedder>@r<N>.vec.
//
// A node's vector is a pure function of (node text, embedder model, embedding
// recipe), so nothing worktree-specific enters the key and every worktree of
// the repository reads the same vectors. Nothing is evicted: the file holds
// every vector the repository ever embedded under the recipe, so a reverted
// edit or a rebased branch finds its vectors again. Deleting the file is the
// reset; a model or recipe change opens a new file.
//
// The file is an append-only record stream — a header naming the recipe, then
// one record per vector — so a save writes only what was added since the
// last one: never a rewrite of the file and never an encoding of the whole
// map into one buffer. A truncated tail (a crash mid-append) is dropped on
// read and the file is rewritten whole once. The gob format of earlier
// releases (v1) is still read, and rewritten in this format on the first save.
// One daemon per repository writes the file; two appending at once can
// interleave records.
package vecstore

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
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

	"github.com/kgatilin/wyrd/internal/retrieval/types"
)

const (
	// fileMagic opens a v2 file.
	fileMagic = "WYRDVEC2"

	// legacyMagic and legacyVersion identify the v1 gob format.
	legacyMagic   = "wyrd-vecstore"
	legacyVersion = 1

	// fileExt is the extension of a vector file.
	fileExt = ".vec"

	// maxDim bounds a record's dimension, so a corrupt length cannot ask for
	// gigabytes.
	maxDim = 1 << 16
)

// legacyFile is the v1 gob payload: the whole namespace in one value.
type legacyFile struct {
	Magic     string
	Version   int
	Namespace string
	Vectors   map[string][]float32
}

// Path returns the vector file for a recipe (see retrieval.Recipe) in dir.
func Path(dir, recipe string) string {
	return filepath.Join(dir, fileName(recipe)+fileExt)
}

// Store is the local vector store. It implements retrieval.VectorStore.
// Safe for concurrent use.
type Store struct {
	path   string // "" keeps the store in memory only
	recipe string

	// once defers the file read to the first use, so opening is free.
	once sync.Once

	mu      sync.RWMutex
	vectors map[string][]float32
	// unsaved lists the hashes put since the last save, in order.
	unsaved []string
	// rewrite is set when the file must be written whole on the next save:
	// absent, foreign, v1, or with a malformed tail appends cannot follow.
	rewrite bool

	saveMu sync.Mutex
}

// New returns a store over the file at path holding recipe's vectors. An
// empty path keeps the store in memory only.
func New(path, recipe string) *Store {
	return &Store{path: path, recipe: recipe}
}

func (s *Store) ensureLoaded() {
	s.once.Do(func() {
		vectors := make(map[string][]float32)
		rewrite := true
		if s.path != "" {
			appendable, err := Scan(s.path, s.recipe, func(hash string, vec []float32) error {
				vectors[hash] = vec
				return nil
			})
			if err != nil {
				// An unreadable file is a cold store, never a startup
				// failure: the daemon re-embeds and rewrites it.
				fmt.Fprintf(os.Stderr, "vecstore: %v; starting with what was read\n", err)
			}
			rewrite = !appendable
		}
		s.mu.Lock()
		s.vectors = vectors
		s.rewrite = rewrite
		s.mu.Unlock()
	})
}

// Has reports whether a vector is stored for hash.
func (s *Store) Has(hash string) bool {
	s.ensureLoaded()
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.vectors[hash]
	return ok
}

// Get returns the stored vectors of hashes. The slices are shared and must be
// treated as immutable.
func (s *Store) Get(_ context.Context, hashes []string) (map[string][]float32, error) {
	s.ensureLoaded()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]float32, len(hashes))
	for _, h := range hashes {
		if vec, ok := s.vectors[h]; ok {
			out[h] = vec
		}
	}
	return out, nil
}

// Put stores vectors. A known hash keeps its vector: entries are immutable.
func (s *Store) Put(_ context.Context, vectors map[string][]float32) error {
	s.ensureLoaded()
	s.mu.Lock()
	defer s.mu.Unlock()
	for h, vec := range vectors {
		if h == "" || len(vec) == 0 {
			continue
		}
		if _, ok := s.vectors[h]; ok {
			continue
		}
		s.vectors[h] = vec
		s.unsaved = append(s.unsaved, h)
	}
	return nil
}

// Search scores the vectors keep accepts by cosine similarity and returns the
// best k. Filtering before ranking makes the answer exact for the caller.
func (s *Store) Search(_ context.Context, query []float32, k int, keep func(hash string) bool) ([]types.Scored, error) {
	if k <= 0 {
		return nil, nil
	}
	s.ensureLoaded()
	s.mu.RLock()
	scores := make([]types.Scored, 0, 64)
	for h, vec := range s.vectors {
		if keep != nil && !keep(h) {
			continue
		}
		scores = append(scores, types.Scored{ID: h, Score: cosine(query, vec)})
	}
	s.mu.RUnlock()

	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Score != scores[j].Score {
			return scores[i].Score > scores[j].Score
		}
		return scores[i].ID < scores[j].ID
	})
	if len(scores) > k {
		scores = scores[:k]
	}
	return scores, nil
}

// Len returns the number of stored vectors.
func (s *Store) Len() int {
	s.ensureLoaded()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.vectors)
}

// Save appends the vectors put since the last save, or writes the file whole
// when it cannot be appended to. A store without a path saves nothing.
func (s *Store) Save() error {
	if s.path == "" {
		return nil
	}
	s.ensureLoaded()
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	s.mu.Lock()
	rewrite, pending := s.rewrite, s.unsaved
	s.unsaved = nil
	s.mu.Unlock()
	if !rewrite && len(pending) == 0 {
		return nil
	}

	var err error
	if rewrite {
		err = s.writeWhole()
	} else {
		err = s.appendRecords(pending)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		// Keep what was not written for the next save.
		s.unsaved = append(pending, s.unsaved...)
		return err
	}
	if rewrite {
		s.rewrite = false
	}
	return nil
}

// writeWhole writes header and every vector to a temp file and renames it
// over the file. The read lock is held while streaming, so the map is never
// copied; puts wait for it.
func (s *Store) writeWhole() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("vecstore: creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp")
	if err != nil {
		return fmt.Errorf("vecstore: creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}

	w := bufio.NewWriter(tmp)
	err = writeHeader(w, s.recipe)
	s.mu.RLock()
	for h, vec := range s.vectors {
		if err != nil {
			break
		}
		err = writeRecord(w, h, vec)
	}
	s.mu.RUnlock()
	if err == nil {
		err = w.Flush()
	}
	if err != nil {
		return fail(fmt.Errorf("vecstore: writing %s: %w", tmpName, err))
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("vecstore: closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("vecstore: renaming %s: %w", tmpName, err)
	}
	return nil
}

// appendRecords appends the records of hashes to the file.
func (s *Store) appendRecords(hashes []string) error {
	f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return fmt.Errorf("vecstore: opening %s: %w", s.path, err)
	}
	w := bufio.NewWriter(f)
	s.mu.RLock()
	for _, h := range hashes {
		if err = writeRecord(w, h, s.vectors[h]); err != nil {
			break
		}
	}
	s.mu.RUnlock()
	if err == nil {
		err = w.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("vecstore: appending to %s: %w", s.path, err)
	}
	return nil
}

// Scan reads the vector file at path, calling fn for every vector of recipe,
// without holding the file in memory. appendable reports whether new records
// may be appended to the file as it is: false when it is missing, foreign, in
// the v1 format, or ends in a malformed record. A missing file is not an
// error; a foreign one, or one for another recipe, is reported with nothing
// read.
func Scan(path, recipe string, fn func(hash string, vec []float32) error) (appendable bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	r := bufio.NewReader(f)
	magic, err := r.Peek(len(fileMagic))
	if err != nil || string(magic) != fileMagic {
		return false, scanLegacy(r, path, recipe, fn)
	}
	if _, err := r.Discard(len(fileMagic)); err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	got, err := readString(r)
	if err != nil {
		return false, fmt.Errorf("reading %s header: %w", path, err)
	}
	if got != recipe {
		return false, fmt.Errorf("ignoring %s: holds recipe %q, want %q", path, got, recipe)
	}
	for {
		hash, vec, err := readRecord(r)
		if errors.Is(err, io.EOF) {
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("%s: dropping malformed tail: %w", path, err)
		}
		if err := fn(hash, vec); err != nil {
			return false, err
		}
	}
}

// scanLegacy reads a v1 (gob) file.
func scanLegacy(r io.Reader, path, recipe string, fn func(string, []float32) error) error {
	var rec legacyFile
	if err := gob.NewDecoder(r).Decode(&rec); err != nil {
		return fmt.Errorf("decoding %s: %w", path, err)
	}
	if rec.Magic != legacyMagic || rec.Version != legacyVersion {
		return fmt.Errorf("ignoring %s: unrecognised header %q v%d", path, rec.Magic, rec.Version)
	}
	if rec.Namespace != recipe {
		return fmt.Errorf("ignoring %s: holds recipe %q, want %q", path, rec.Namespace, recipe)
	}
	for h, vec := range rec.Vectors {
		if err := fn(h, vec); err != nil {
			return err
		}
	}
	return nil
}

func writeHeader(w io.Writer, recipe string) error {
	if _, err := io.WriteString(w, fileMagic); err != nil {
		return err
	}
	return writeString(w, recipe)
}

// writeRecord writes one vector: the hash, the dimension, the components as
// little-endian float32.
func writeRecord(w io.Writer, hash string, vec []float32) error {
	if err := writeString(w, hash); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(vec))); err != nil {
		return err
	}
	return binary.Write(w, binary.LittleEndian, vec)
}

// readRecord reads one vector. io.EOF means the stream ended cleanly between
// records; any other error is a malformed or truncated record.
func readRecord(r io.Reader) (string, []float32, error) {
	hash, err := readString(r)
	if err != nil {
		return "", nil, err
	}
	var dim uint32
	if err := binary.Read(r, binary.LittleEndian, &dim); err != nil {
		return "", nil, unexpected(err)
	}
	if dim == 0 || dim > maxDim {
		return "", nil, fmt.Errorf("record %s: dimension %d out of range", hash, dim)
	}
	vec := make([]float32, dim)
	if err := binary.Read(r, binary.LittleEndian, vec); err != nil {
		return "", nil, unexpected(err)
	}
	return hash, vec, nil
}

func writeString(w io.Writer, s string) error {
	if err := binary.Write(w, binary.LittleEndian, uint16(len(s))); err != nil {
		return err
	}
	_, err := io.WriteString(w, s)
	return err
}

// readString reads a length-prefixed string. io.EOF only when nothing at all
// was left to read.
func readString(r io.Reader) (string, error) {
	var n uint16
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		if errors.Is(err, io.EOF) {
			return "", io.EOF
		}
		return "", unexpected(err)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", unexpected(err)
	}
	if bytes.IndexByte(buf, 0) >= 0 {
		return "", errors.New("string holds a NUL byte")
	}
	return string(buf), nil
}

// unexpected turns a clean EOF inside a record into ErrUnexpectedEOF: the
// record started and did not finish.
func unexpected(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

// cosine returns the cosine similarity of two vectors, 0 when either is zero
// or their lengths differ.
func cosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

// fileName maps a recipe to a filename-safe, still human-recognisable name
// (embedder ids carry ':' and '/'). Two recipes that sanitise alike would
// share a file; the recipe recorded in the header catches that and the losing
// side simply runs cold.
func fileName(recipe string) string {
	var sb strings.Builder
	for _, r := range recipe {
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
