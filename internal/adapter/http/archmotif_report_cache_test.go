package http

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kgatilin/wyrd/internal/archreview"
)

// countingCache returns a cache whose build records what it was asked for and
// answers with the base ref in the report's Base, so a test can tell one key's
// answer from another's.
func countingCache() (*reportCache, *int64) {
	var builds int64
	cache := newReportCache(func(_ context.Context, key reportKey) (archreview.Report, error) {
		atomic.AddInt64(&builds, 1)
		return archreview.Report{
			Schema: archreview.Schema,
			Mode:   archreview.ModeReview,
			Base:   &archreview.Base{Ref: key.baseRef},
		}, nil
	})
	return cache, &builds
}

func mustGet(t *testing.T, cache *reportCache, key reportKey) archreview.Report {
	t.Helper()
	report, err := cache.get(context.Background(), key)
	if err != nil {
		t.Fatalf("get %+v: %v", key, err)
	}
	return report
}

func TestReportCacheBuildsOnceForRepeatedReads(t *testing.T) {
	cache, builds := countingCache()
	key := reportKey{worktree: "feature", baseRef: "main"}

	mustGet(t, cache, key)
	mustGet(t, cache, key)
	mustGet(t, cache, key)

	if got := atomic.LoadInt64(builds); got != 1 {
		t.Errorf("builds = %d, want 1 — reopening the panel must not rebuild the report", got)
	}
}

// A model event is the daemon's only announcement that the working tree moved.
// If it does not force a rebuild, the panel goes on describing a tree that no
// longer exists — the trap the file diff already documents.
func TestReportCacheRebuildsAfterAModelEvent(t *testing.T) {
	cache, builds := countingCache()
	key := reportKey{worktree: "feature", baseRef: "main"}

	mustGet(t, cache, key)
	cache.drop(key.worktree)
	mustGet(t, cache, key)

	if got := atomic.LoadInt64(builds); got != 2 {
		t.Errorf("builds = %d, want 2 — the read after a model event must recompute", got)
	}
}

// Dropping one worktree must not blank another's: two branches are reviewed
// side by side, and an edit to one says nothing about the other.
func TestReportCacheDropsOnlyTheWorktreeThatChanged(t *testing.T) {
	cache, builds := countingCache()
	feature := reportKey{worktree: "feature", baseRef: "main"}
	other := reportKey{worktree: "other", baseRef: "main"}

	mustGet(t, cache, feature)
	mustGet(t, cache, other)
	cache.drop("feature")
	mustGet(t, cache, feature)
	mustGet(t, cache, other)

	if got := atomic.LoadInt64(builds); got != 3 {
		t.Errorf("builds = %d, want 3 — only the changed worktree should rebuild", got)
	}
}

// The base ref is half the question, so a partial key match has to miss.
// Answering "what did this branch do against main" with the report built
// against another base is a wrong answer, not a stale one.
func TestReportCacheKeysOnTheBaseRef(t *testing.T) {
	cache, builds := countingCache()
	main := reportKey{worktree: "feature", baseRef: "main"}
	release := reportKey{worktree: "feature", baseRef: "release"}

	if got := mustGet(t, cache, main).Base.Ref; got != "main" {
		t.Fatalf("base ref = %q, want main", got)
	}
	if got := mustGet(t, cache, release).Base.Ref; got != "release" {
		t.Errorf("base ref = %q, want release — a different base must not read main's entry", got)
	}
	if got := atomic.LoadInt64(builds); got != 2 {
		t.Errorf("builds = %d, want 2", got)
	}
	// And main's own entry survived the miss.
	if got := mustGet(t, cache, main).Base.Ref; got != "main" {
		t.Errorf("base ref = %q, want main", got)
	}
	if got := atomic.LoadInt64(builds); got != 2 {
		t.Errorf("builds = %d, want 2 — the second base must not evict the first", got)
	}
}

// Warming is only worth having if it costs the caller nothing: it runs on the
// load hook, on /api/warm and on every model event, none of which may wait for
// a graph build.
func TestReportCacheWarmDoesNotBlock(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var builds int64
	cache := newReportCache(func(_ context.Context, key reportKey) (archreview.Report, error) {
		atomic.AddInt64(&builds, 1)
		close(started)
		<-release
		return archreview.Report{Schema: archreview.Schema, Base: &archreview.Base{Ref: key.baseRef}}, nil
	})
	key := reportKey{worktree: "feature", baseRef: "main"}

	returned := make(chan struct{})
	go func() {
		cache.warm(key)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("warm blocked on the build")
	}

	// It returned while the build was genuinely still running: a reader that
	// gives up now gets its context's error rather than the report.
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := cache.get(ctx, key); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("get during the warm = %v, want the context deadline", err)
	}

	close(release)
	if got := mustGet(t, cache, key).Base.Ref; got != "main" {
		t.Errorf("base ref = %q, want main", got)
	}
	if got := atomic.LoadInt64(&builds); got != 1 {
		t.Errorf("builds = %d, want 1 — a read must join the warm, not race it", got)
	}
}

// Concurrent first opens must collapse into one build. Without this the panel,
// the reviewer's refresh and the warm could each start their own.
func TestReportCacheCollapsesConcurrentReads(t *testing.T) {
	var builds int64
	release := make(chan struct{})
	cache := newReportCache(func(_ context.Context, _ reportKey) (archreview.Report, error) {
		atomic.AddInt64(&builds, 1)
		<-release
		return archreview.Report{Schema: archreview.Schema}, nil
	})
	key := reportKey{worktree: "feature", baseRef: "main"}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = cache.get(context.Background(), key)
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt64(&builds); got != 1 {
		t.Errorf("builds = %d, want 1", got)
	}
}

// A failure is not an answer. Caching one would replay a transient git or parse
// error at every open until something unrelated changed the model.
func TestReportCacheDoesNotStoreAFailure(t *testing.T) {
	var builds int64
	cache := newReportCache(func(_ context.Context, _ reportKey) (archreview.Report, error) {
		if atomic.AddInt64(&builds, 1) == 1 {
			return archreview.Report{}, errors.New("merge-base unavailable")
		}
		return archreview.Report{Schema: archreview.Schema}, nil
	})
	key := reportKey{worktree: "feature", baseRef: "main"}

	if _, err := cache.get(context.Background(), key); err == nil {
		t.Fatal("get = nil error, want the build's failure")
	}
	if got := mustGet(t, cache, key).Schema; got != archreview.Schema {
		t.Errorf("schema = %q, want the retried build to succeed", got)
	}
	if got := atomic.LoadInt64(&builds); got != 2 {
		t.Errorf("builds = %d, want 2 — a failed build must not be cached", got)
	}
}

// Base refs come from a URL query parameter, so the cache needs a ceiling that
// does not depend on callers being well behaved.
func TestReportCacheBoundsItsEntries(t *testing.T) {
	cache, _ := countingCache()
	for i := 0; i < maxCachedReports*3; i++ {
		mustGet(t, cache, reportKey{worktree: "feature", baseRef: fmt.Sprintf("base-%d", i)})
	}

	cache.mu.Lock()
	size := len(cache.entries)
	cache.mu.Unlock()
	if size > maxCachedReports {
		t.Errorf("entries = %d, want at most %d", size, maxCachedReports)
	}
}
