package serve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kgatilin/wyrd/internal/adapter/git"
	"github.com/kgatilin/wyrd/internal/serve/modelstore"
)

// baseTreeDirName is the wyrd-home subdirectory holding materialized base
// commits, alongside the daemon registry and the vector caches.
const baseTreeDirName = "basetrees"

// baseTreeStaleAfter is how old an entry of the base-tree directory must be
// before a sweep removes it when no load of this process owns it. A tree is
// deleted by the load that made it, so what the sweep finds is a leftover — a
// daemon that crashed or predates that rule — or the load of another process,
// which is recent.
const baseTreeStaleAfter = time.Hour

// baseTrees produces the snapshot of a *commit* — the merge base a branch is
// reviewed against — that no State has parsed: it materializes the commit's
// tree, parses it, records the snapshot in the model store and deletes the
// tree. The tree is only a means to the snapshot.
//
// It exists because the base worktree is not the base commit: main's checkout
// moves on, and diffing a branch against main's tip reports everything that
// landed after the branch point as the branch's own doing, in reverse. The
// textual diff has always used the merge base; this makes the model diff
// agree with it.
type baseTrees struct {
	mu    sync.Mutex
	dir   string
	loads map[string]*baseTreeLoad // in flight, by revision
	swept bool
}

type baseTreeLoad struct {
	done     chan struct{}
	versions []modelstore.Version
	err      error
}

func newBaseTrees(cacheDir string) *baseTrees {
	return &baseTrees{dir: cacheDir, loads: make(map[string]*baseTreeLoad)}
}

// BaseTreeDir returns the directory holding this repository's materialized
// base commits: <wyrd home>/basetrees/<repo key>.
//
// It lives outside the repository on purpose. A checkout inside the repo
// would either show up in `git status` or, as a linked worktree, in
// `git worktree list` — and from there in this UI's own branch picker and in
// every other tool pointed at the repo. It would also hand the model watcher
// a few thousand file-creation events.
func BaseTreeDir(repoRoot string) (string, error) {
	base, err := wyrdHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, baseTreeDirName, repoKey(repoRoot)), nil
}

// Versions returns the snapshot of rev, producing it when the store has none:
// the tree is materialized from sourcePath (any worktree of the repository —
// the object store is shared), parsed, recorded and deleted.
//
// Concurrent callers for the same revision collapse onto one load, which runs
// to completion even when the caller that started it gives up.
func (b *baseTrees) Versions(ctx context.Context, store *modelstore.Store, sourcePath, rev string) ([]modelstore.Version, error) {
	if rev == "" {
		return nil, nil
	}
	b.mu.Lock()
	load, ok := b.loads[rev]
	if !ok {
		// Checked under the lock: a load that finished since the caller last
		// looked has recorded its snapshot and left the in-flight set.
		versions, ok, err := store.Snapshot(rev)
		if err != nil {
			fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		}
		if ok {
			b.mu.Unlock()
			return versions, nil
		}
		load = &baseTreeLoad{done: make(chan struct{})}
		b.loads[rev] = load
		go b.run(context.WithoutCancel(ctx), store, sourcePath, rev, load)
	}
	sweep := !b.swept
	b.swept = true
	b.mu.Unlock()
	if sweep {
		b.sweep()
	}
	return waitBaseTree(ctx, load)
}

func (b *baseTrees) run(ctx context.Context, store *modelstore.Store, sourcePath, rev string, load *baseTreeLoad) {
	load.versions, load.err = b.read(ctx, store, sourcePath, rev)
	b.mu.Lock()
	// Done either way: a recorded snapshot answers the next request, and a
	// failed load must not be cached — the next request retries rather than
	// inherit a transient git or parse error.
	delete(b.loads, rev)
	close(load.done)
	b.mu.Unlock()
}

func waitBaseTree(ctx context.Context, load *baseTreeLoad) ([]modelstore.Version, error) {
	select {
	case <-load.done:
		return load.versions, load.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// read materializes rev, parses it, records the snapshot and removes the tree.
func (b *baseTrees) read(ctx context.Context, store *modelstore.Store, sourcePath, rev string) ([]modelstore.Version, error) {
	dir, err := b.materialize(sourcePath, rev)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	models, err := NewState(dir).readPackages(ctx, []string{"./..."})
	if err != nil {
		return nil, fmt.Errorf("serve: parsing base commit %s: %w", short(rev), err)
	}
	versions, err := store.Record(rev, models)
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
	}
	return versions, nil
}

// sweep removes what earlier daemons left in the base-tree directory: every
// entry no load of this process owns and older than baseTreeStaleAfter. It
// used to be the cache's eviction, and it counted only the loads in memory —
// so every restart forgot the trees on disk and they were never removed.
//
// A load still in flight is never touched, however old: its directory is
// being parsed, and removing it under the parser leaves a husk (see
// materialize). The age floor extends the same courtesy to another process's
// load, which this one cannot see.
func (b *baseTrees) sweep() {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return // no directory yet: nothing to sweep
	}
	b.mu.Lock()
	inFlight := make([]string, 0, len(b.loads))
	for rev := range b.loads {
		inFlight = append(inFlight, rev)
	}
	b.mu.Unlock()
	cutoff := time.Now().Add(-baseTreeStaleAfter)
	for _, e := range entries {
		if ownedBy(e.Name(), inFlight) {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(b.dir, e.Name())); err != nil {
			fmt.Fprintf(os.Stderr, "serve: sweep base tree %s: %v\n", e.Name(), err)
		}
	}
}

// ownedBy reports whether a base-tree directory entry — a tree, or the
// staging directory of one — belongs to one of revs.
func ownedBy(name string, revs []string) bool {
	for _, rev := range revs {
		if name == rev || strings.HasPrefix(name, rev+".tmp") {
			return true
		}
	}
	return false
}

// baseTreeMarker is written into a staged tree before it is published, and is
// what makes a directory in the cache a materialized tree rather than just a
// directory. Nothing else in the cache creates it.
const baseTreeMarker = ".wyrd-basetree"

// materialize extracts rev into the cache, atomically: the tree is written
// to a temporary sibling, marked, and renamed, so an interrupted export can
// never be mistaken for a complete one.
func (b *baseTrees) materialize(sourcePath, rev string) (string, error) {
	dir := filepath.Join(b.dir, rev)
	if baseTreeComplete(dir) {
		return dir, nil
	}
	// A directory without the marker is not a tree. Existence used to be the
	// whole test, and it is not enough: a directory evicted while its tree
	// was still being parsed was recreated by the parser to hold its model
	// cache (daemons before the shared store wrote one there), and the husk
	// left behind — a .archai directory and no source — was then accepted
	// forever. Every later review against that commit
	// failed with "directory prefix . does not contain main module", and no
	// restart could clear it because nothing ever re-exported.
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("serve: clear incomplete base tree %s: %w", short(rev), err)
	}
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return "", fmt.Errorf("serve: create base tree cache: %w", err)
	}
	tmp, err := os.MkdirTemp(b.dir, rev+".tmp")
	if err != nil {
		return "", fmt.Errorf("serve: create base tree staging dir: %w", err)
	}
	if err := git.ExportTree(sourcePath, rev, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmp, baseTreeMarker), []byte(rev+"\n"), 0o644); err != nil {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("serve: mark base tree %s: %w", short(rev), err)
	}
	if err := os.Rename(tmp, dir); err != nil {
		_ = os.RemoveAll(tmp)
		// Another daemon (or another goroutine) may have won the race.
		if baseTreeComplete(dir) {
			return dir, nil
		}
		return "", fmt.Errorf("serve: publish base tree %s: %w", short(rev), err)
	}
	return dir, nil
}

// baseTreeComplete reports whether dir holds a tree that was published in
// full, as opposed to a directory that merely exists.
func baseTreeComplete(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, baseTreeMarker))
	return err == nil
}

func short(rev string) string {
	if len(rev) > 8 {
		return rev[:8]
	}
	return rev
}
