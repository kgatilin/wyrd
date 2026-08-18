package serve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/kgatilin/archai/internal/adapter/git"
	"github.com/kgatilin/archai/internal/domain"
)

// baseTreeDirName is the archai-home subdirectory holding materialized base
// commits, alongside the daemon registry and the vector caches.
const baseTreeDirName = "basetrees"

// defaultBaseTreeLimit caps how many base commits are kept materialized (on
// disk and parsed in memory) at once. Branches under review usually share a
// merge base, so a small cache covers the working set; each extra entry is a
// second copy of the tree plus a second parsed model.
const defaultBaseTreeLimit = 2

// baseTrees resolves the package models of a *commit* — the merge base a
// branch is reviewed against — by materializing its tree once and parsing it.
//
// It exists because the base worktree is not the base commit: main's checkout
// moves on, and diffing a branch against main's tip reports everything that
// landed after the branch point as the branch's own doing, in reverse. The
// textual diff has always used the merge base; this makes the model diff
// agree with it.
type baseTrees struct {
	mu    sync.Mutex
	dir   string
	limit int
	loads map[string]*baseTreeLoad
	order []string // revisions, least-recently requested first
}

type baseTreeLoad struct {
	done   chan struct{}
	models []domain.PackageModel
	err    error
}

func newBaseTrees(cacheDir string, limit int) *baseTrees {
	if limit <= 0 {
		limit = defaultBaseTreeLimit
	}
	return &baseTrees{dir: cacheDir, limit: limit, loads: make(map[string]*baseTreeLoad)}
}

// BaseTreeDir returns the directory holding this repository's materialized
// base commits: <archai home>/basetrees/<repo key>.
//
// It lives outside the repository on purpose. A checkout inside the repo
// would either show up in `git status` or, as a linked worktree, in
// `git worktree list` — and from there in this UI's own branch picker and in
// every other tool pointed at the repo. It would also hand the model watcher
// a few thousand file-creation events.
func BaseTreeDir(repoRoot string) (string, error) {
	base, err := archaiHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, baseTreeDirName, repoKey(repoRoot)), nil
}

// Models returns the package models of rev, materializing and parsing its
// tree on first request. sourcePath is any worktree of the repository — the
// object store is shared, so any of them can export the commit.
//
// Concurrent callers for the same revision collapse onto one load.
func (b *baseTrees) Models(ctx context.Context, sourcePath, rev string) ([]domain.PackageModel, error) {
	if rev == "" {
		return nil, nil
	}
	b.mu.Lock()
	if load, ok := b.loads[rev]; ok {
		b.touchLocked(rev)
		b.mu.Unlock()
		return waitBaseTree(ctx, load)
	}
	load := &baseTreeLoad{done: make(chan struct{})}
	b.loads[rev] = load
	b.touchLocked(rev)
	evicted := b.evictLocked()
	b.mu.Unlock()

	for _, dir := range evicted {
		_ = os.RemoveAll(dir)
	}

	go func() {
		load.models, load.err = b.read(ctx, sourcePath, rev)
		close(load.done)
		if load.err != nil {
			// A failed load must not be cached: the next request should
			// retry rather than inherit a transient git or parse error.
			b.mu.Lock()
			if b.loads[rev] == load {
				delete(b.loads, rev)
				b.dropOrderLocked(rev)
			}
			b.mu.Unlock()
		}
	}()

	return waitBaseTree(ctx, load)
}

func waitBaseTree(ctx context.Context, load *baseTreeLoad) ([]domain.PackageModel, error) {
	select {
	case <-load.done:
		return load.models, load.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// read materializes rev if needed and parses it. The extracted tree keeps
// its own .archai/cache/go-model.json, so a daemon restart re-parses only
// what it no longer has on disk.
func (b *baseTrees) read(ctx context.Context, sourcePath, rev string) ([]domain.PackageModel, error) {
	dir, err := b.materialize(sourcePath, rev)
	if err != nil {
		return nil, err
	}
	models, err := NewState(dir).readAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("serve: parsing base commit %s: %w", short(rev), err)
	}
	return models, nil
}

// materialize extracts rev into the cache, atomically: the tree is written
// to a temporary sibling and renamed, so an interrupted export can never be
// mistaken for a complete one.
func (b *baseTrees) materialize(sourcePath, rev string) (string, error) {
	dir := filepath.Join(b.dir, rev)
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
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
	if err := os.Rename(tmp, dir); err != nil {
		_ = os.RemoveAll(tmp)
		// Another daemon (or another goroutine) may have won the race.
		if _, statErr := os.Stat(dir); statErr == nil {
			return dir, nil
		}
		return "", fmt.Errorf("serve: publish base tree %s: %w", short(rev), err)
	}
	return dir, nil
}

// touchLocked marks rev as most recently used.
func (b *baseTrees) touchLocked(rev string) {
	b.dropOrderLocked(rev)
	b.order = append(b.order, rev)
}

func (b *baseTrees) dropOrderLocked(rev string) {
	for i, existing := range b.order {
		if existing == rev {
			b.order = append(b.order[:i], b.order[i+1:]...)
			return
		}
	}
}

// evictLocked trims the cache to its limit and returns the directories the
// caller should delete (outside the lock).
func (b *baseTrees) evictLocked() []string {
	var dirs []string
	for len(b.order) > b.limit {
		oldest := b.order[0]
		b.order = b.order[1:]
		delete(b.loads, oldest)
		dirs = append(dirs, filepath.Join(b.dir, oldest))
	}
	return dirs
}

func short(rev string) string {
	if len(rev) > 8 {
		return rev[:8]
	}
	return rev
}
