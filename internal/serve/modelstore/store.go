// Package modelstore is the repository-level, content-addressed store of
// parsed package models that every worktree State of a daemon shares.
//
// It holds two things:
//
//   - a Version: one package's model, addressed by the hash of its canonical
//     encoding. Identical content in two worktrees, or in two commits, is one
//     Version, held once in memory and written once to disk;
//   - a snapshot: a commit revision mapped to the Version of every package in
//     its tree.
//
// A branch is a snapshot (its merge base) with the packages its diff touches
// read on top; that composition is the caller's (internal/serve), not this
// package's.
//
// Shared models are immutable. The Model a Version carries is the canonical
// instance: its slices are the same backing arrays every other holder of the
// Version sees, so nothing may write through them. Fields a worktree sets on
// its own copy (the overlay's Layer and Aggregate) are not part of a Version
// and are always zero in one.
package modelstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kgatilin/wyrd/internal/domain"
)

// snapshotLimit caps how many snapshots are kept, on disk and in memory. A
// snapshot is a few hundred hashes; what it costs is the Versions only it
// references, which on a slowly moving base is a handful of packages each.
const snapshotLimit = 16

const (
	versionsDir  = "versions"
	snapshotsDir = "snapshots"
)

// Version is one package's model and the hash it is addressed by.
type Version struct {
	// Hash is the hex SHA-256 of the model's canonical encoding (JSON, with
	// Layer and Aggregate cleared).
	Hash string
	// Model is the package model. Read-only: see the package doc.
	Model domain.PackageModel
}

// Store is the repository's set of Versions and snapshots. It is safe for
// concurrent use. A nil *Store holds nothing and persists nothing: Versions
// still hashes, Snapshot always misses, Record only hashes.
type Store struct {
	// dir is <repository dir>/<stamp key>.
	dir string

	mu sync.Mutex
	// held is every Version referenced by a snapshot in memory, keyed by
	// hash. It is what makes an unchanged package one allocation.
	held map[string]*heldVersion
	// snapshots is the in-memory snapshot set, rev → hashes.
	snapshots map[string][]string
	// order lists the in-memory snapshots, least recently used first.
	order []string
}

type heldVersion struct {
	model domain.PackageModel
	refs  int
}

// Open returns the store kept under dir for the parser identified by stamp.
//
// The stamp names the parser that produced the stored models: Versions and
// snapshots written under a different stamp, or for a different shape of
// domain.PackageModel, live under a different key and are removed here, since
// identical source no longer parses to the same model once the parser logic
// changed. Open also trims the snapshots to the retention limit and removes
// the Versions none of the survivors reference.
func Open(dir, stamp string) (*Store, error) {
	key := stampKey(stamp)
	root := filepath.Join(dir, key)
	for _, sub := range []string{versionsDir, snapshotsDir} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return nil, fmt.Errorf("modelstore: create %s: %w", sub, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("modelstore: list %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.Name() == key {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return nil, fmt.Errorf("modelstore: remove models of another binary: %w", err)
		}
	}
	s := &Store{
		dir:       root,
		held:      make(map[string]*heldVersion),
		snapshots: make(map[string][]string),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.collectLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

// Versions hashes models and, for each one the store already holds, swaps in
// the canonical instance. Nothing is added: a Version enters the store only
// through a snapshot (Record or Snapshot), so a model parsed off a dirty tree
// is shared when it matches what a snapshot holds and is its holder's own
// otherwise.
func (s *Store) Versions(models []domain.PackageModel) []Version {
	out := make([]Version, len(models))
	for i, m := range models {
		m, _, hash := canonical(m)
		out[i] = Version{Hash: hash, Model: m}
	}
	if s == nil {
		return out
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range out {
		if h, ok := s.held[out[i].Hash]; ok {
			out[i].Model = h.model
		}
	}
	return out
}

// Snapshot returns the Versions of the tree at rev, sorted by package path,
// reading them from disk when no snapshot of rev is in memory. ok is false
// when no snapshot of rev exists. An error means a stored snapshot could not
// be read; the caller treats it as a miss.
func (s *Store) Snapshot(rev string) (versions []Version, ok bool, err error) {
	if s == nil || rev == "" {
		return nil, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if hashes, ok := s.snapshots[rev]; ok {
		s.touchLocked(rev)
		return s.resolveLocked(hashes), true, nil
	}

	path := s.snapshotPath(rev)
	hashes, err := readSnapshotFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("modelstore: read snapshot %s: %w", short(rev), err)
	}
	versions = make([]Version, 0, len(hashes))
	for _, hash := range hashes {
		if h, ok := s.held[hash]; ok {
			versions = append(versions, Version{Hash: hash, Model: h.model})
			continue
		}
		model, err := s.readVersionFile(hash)
		if err != nil {
			// A snapshot naming a Version that is gone is not a snapshot;
			// drop it so the tree is parsed and recorded again.
			_ = os.Remove(path)
			return nil, false, fmt.Errorf("modelstore: snapshot %s: %w", short(rev), err)
		}
		versions = append(versions, Version{Hash: hash, Model: model})
	}
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		return nil, false, fmt.Errorf("modelstore: touch snapshot %s: %w", short(rev), err)
	}
	return s.holdLocked(rev, versions), true, nil
}

// Record stores models as the snapshot of rev and returns their Versions,
// each the canonical instance: a model identical to one already held is
// returned as the held instance, so the caller drops its own copy.
//
// The Versions are valid even when persisting fails; the error reports that
// the snapshot will not survive a restart.
func (s *Store) Record(rev string, models []domain.PackageModel) ([]Version, error) {
	if s == nil || rev == "" {
		return s.Versions(models), nil
	}
	type encoded struct {
		version Version
		data    []byte
	}
	enc := make([]encoded, len(models))
	for i, m := range models {
		m, data, hash := canonical(m)
		enc[i] = encoded{version: Version{Hash: hash, Model: m}, data: data}
	}
	sort.Slice(enc, func(i, j int) bool { return enc[i].version.Model.Path < enc[j].version.Model.Path })

	s.mu.Lock()
	defer s.mu.Unlock()
	versions := make([]Version, len(enc))
	var errs []error
	for i, e := range enc {
		versions[i] = e.version
		if err := s.writeVersionFile(e.version.Hash, e.data); err != nil {
			errs = append(errs, err)
		}
	}
	versions = s.holdLocked(rev, versions)
	if len(errs) == 0 {
		if err := writeSnapshotFile(s.snapshotPath(rev), rev, versions); err != nil {
			errs = append(errs, err)
		}
	}
	if err := s.collectLocked(); err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return versions, fmt.Errorf("modelstore: record snapshot %s: %w", short(rev), err)
	}
	return versions, nil
}

// holdLocked makes versions the in-memory snapshot of rev, replacing each
// model with the held instance where there is one and holding the rest, and
// returns the result. The least recently used snapshot beyond the limit is
// released, and with it every Version no other in-memory snapshot holds.
func (s *Store) holdLocked(rev string, versions []Version) []Version {
	if old, ok := s.snapshots[rev]; ok {
		s.releaseLocked(old)
	}
	hashes := make([]string, len(versions))
	for i := range versions {
		hash := versions[i].Hash
		hashes[i] = hash
		h, ok := s.held[hash]
		if !ok {
			h = &heldVersion{model: clip(versions[i].Model)}
			s.held[hash] = h
		}
		h.refs++
		versions[i].Model = h.model
	}
	s.snapshots[rev] = hashes
	s.touchLocked(rev)
	for len(s.order) > snapshotLimit {
		evicted := s.order[0]
		s.order = s.order[1:]
		s.releaseLocked(s.snapshots[evicted])
		delete(s.snapshots, evicted)
	}
	return versions
}

func (s *Store) releaseLocked(hashes []string) {
	for _, hash := range hashes {
		h, ok := s.held[hash]
		if !ok {
			continue
		}
		h.refs--
		if h.refs <= 0 {
			delete(s.held, hash)
		}
	}
}

func (s *Store) resolveLocked(hashes []string) []Version {
	out := make([]Version, len(hashes))
	for i, hash := range hashes {
		out[i] = Version{Hash: hash, Model: s.held[hash].model}
	}
	return out
}

func (s *Store) touchLocked(rev string) {
	for i, r := range s.order {
		if r == rev {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	s.order = append(s.order, rev)
}

// collectLocked enforces the disk retention: the snapshotLimit most recently
// used snapshots stay, and a Version stays while a surviving snapshot or the
// in-memory set references it.
func (s *Store) collectLocked() error {
	dir := filepath.Join(s.dir, snapshotsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("modelstore: list snapshots: %w", err)
	}
	type snap struct {
		path string
		mod  time.Time
	}
	var snaps []snap
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed under us
		}
		snaps = append(snaps, snap{path: filepath.Join(dir, e.Name()), mod: info.ModTime()})
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].mod.After(snaps[j].mod) })

	var errs []error
	referenced := make(map[string]bool, len(s.held))
	for hash := range s.held {
		referenced[hash] = true
	}
	for i, sn := range snaps {
		if i >= snapshotLimit {
			if err := os.Remove(sn.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
			continue
		}
		hashes, err := readSnapshotFile(sn.path)
		if err != nil {
			// Unreadable: it can never be served, so it references nothing.
			if err := os.Remove(sn.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
			continue
		}
		for _, hash := range hashes {
			referenced[hash] = true
		}
	}

	vdir := filepath.Join(s.dir, versionsDir)
	ventries, err := os.ReadDir(vdir)
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("modelstore: list versions: %w", err))...)
	}
	for _, e := range ventries {
		hash, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || referenced[hash] {
			continue
		}
		if err := os.Remove(filepath.Join(vdir, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("modelstore: collect: %w", err)
	}
	return nil
}

func (s *Store) snapshotPath(rev string) string {
	return filepath.Join(s.dir, snapshotsDir, rev+".json")
}

func (s *Store) versionPath(hash string) string {
	return filepath.Join(s.dir, versionsDir, hash+".json")
}

func (s *Store) readVersionFile(hash string) (domain.PackageModel, error) {
	data, err := os.ReadFile(s.versionPath(hash))
	if err != nil {
		return domain.PackageModel{}, fmt.Errorf("read version %s: %w", short(hash), err)
	}
	var m domain.PackageModel
	if err := json.Unmarshal(data, &m); err != nil {
		return domain.PackageModel{}, fmt.Errorf("decode version %s: %w", short(hash), err)
	}
	return m, nil
}

// writeVersionFile writes a Version once: the name is the content's hash, so
// a file that exists already holds these bytes.
func (s *Store) writeVersionFile(hash string, data []byte) error {
	path := s.versionPath(hash)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return writeAtomic(path, data)
}

type snapshotFile struct {
	Rev      string          `json:"rev"`
	Packages []snapshotEntry `json:"packages"`
}

type snapshotEntry struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

func readSnapshotFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f snapshotFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	hashes := make([]string, len(f.Packages))
	for i, p := range f.Packages {
		hashes[i] = p.Version
	}
	return hashes, nil
}

func writeSnapshotFile(path, rev string, versions []Version) error {
	f := snapshotFile{Rev: rev, Packages: make([]snapshotEntry, len(versions))}
	for i, v := range versions {
		f.Packages[i] = snapshotEntry{Path: v.Model.Path, Version: v.Hash}
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// canonical returns m as a Version holds it — Layer and Aggregate cleared —
// with its canonical encoding and that encoding's hash.
//
// The encoding is JSON: encoding/json writes struct fields in declaration
// order and has no maps to order in this model, so equal models encode to
// equal bytes, and the bytes hashed are the bytes stored.
func canonical(m domain.PackageModel) (domain.PackageModel, []byte, string) {
	m.Layer, m.Aggregate = "", ""
	data, err := json.Marshal(m)
	if err != nil {
		// Only a field of a kind JSON cannot carry (a func, a channel) gets
		// here, and that is a change to domain.PackageModel, not an input.
		panic(fmt.Sprintf("modelstore: domain.PackageModel is not JSON-encodable: %v", err))
	}
	sum := sha256.Sum256(data)
	return m, data, hex.EncodeToString(sum[:])
}

// clip caps every top-level slice of m at its length, so an append by any
// holder of a copy allocates rather than writing into the shared array's
// spare capacity (a decoded slice usually has some).
func clip(m domain.PackageModel) domain.PackageModel {
	m.Interfaces = m.Interfaces[:len(m.Interfaces):len(m.Interfaces)]
	m.Structs = m.Structs[:len(m.Structs):len(m.Structs)]
	m.Functions = m.Functions[:len(m.Functions):len(m.Functions)]
	m.TypeDefs = m.TypeDefs[:len(m.TypeDefs):len(m.TypeDefs)]
	m.Constants = m.Constants[:len(m.Constants):len(m.Constants)]
	m.Variables = m.Variables[:len(m.Variables):len(m.Variables)]
	m.Errors = m.Errors[:len(m.Errors):len(m.Errors)]
	m.Dependencies = m.Dependencies[:len(m.Dependencies):len(m.Dependencies)]
	m.Imports = m.Imports[:len(m.Imports):len(m.Imports)]
	m.Implementations = m.Implementations[:len(m.Implementations):len(m.Implementations)]
	return m
}

// stampKey is the directory a parser's models live under: the stamp the
// caller passes (the parser's version) and the structural shape of
// domain.PackageModel, so a model written for another shape is never decoded
// into this one.
func stampKey(stamp string) string {
	sum := sha256.Sum256([]byte(schemaFingerprint + "\n" + stamp))
	return hex.EncodeToString(sum[:8])
}

// schemaFingerprint is a structural hash of domain.PackageModel, recomputed
// from the live type at startup: adding, removing or renaming a field
// anywhere in the model changes it.
var schemaFingerprint = computeSchemaFingerprint(reflect.TypeOf(domain.PackageModel{}))

// computeSchemaFingerprint walks the structural shape of t (field names and
// types, recursing through structs/slices/maps/pointers) and returns a stable
// short hash. Recursive types are broken with a type-name marker.
func computeSchemaFingerprint(t reflect.Type) string {
	var sb strings.Builder
	writeTypeShape(&sb, t, map[reflect.Type]bool{})
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:8])
}

func writeTypeShape(sb *strings.Builder, t reflect.Type, seen map[reflect.Type]bool) {
	switch t.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Array:
		sb.WriteByte('[')
		writeTypeShape(sb, t.Elem(), seen)
		sb.WriteByte(']')
	case reflect.Map:
		sb.WriteString("map[")
		writeTypeShape(sb, t.Key(), seen)
		sb.WriteByte(']')
		writeTypeShape(sb, t.Elem(), seen)
	case reflect.Struct:
		if seen[t] {
			sb.WriteString("@" + t.String())
			return
		}
		seen[t] = true
		sb.WriteByte('{')
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue // unexported: not serialized by encoding/json
			}
			sb.WriteString(f.Name)
			sb.WriteByte(':')
			writeTypeShape(sb, f.Type, seen)
			sb.WriteByte(';')
		}
		sb.WriteByte('}')
	default:
		sb.WriteString(t.Kind().String())
	}
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
