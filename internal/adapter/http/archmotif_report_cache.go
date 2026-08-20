package http

import (
	"context"
	"sync"
	"time"

	"github.com/kgatilin/archai/internal/archreview"
)

// maxCachedReports bounds the cache. Base refs arrive from a URL query
// parameter, so without a ceiling a long-lived daemon accumulates one report
// per ref anybody ever asked for. Eight is far above the number of bases a
// review session uses and far below anything worth worrying about.
const maxCachedReports = 8

// reportKey identifies one report: a worktree compared against one base ref.
// Both halves are in the key because they are two halves of one question —
// answering "what did this branch do against main" with the report for another
// base is a wrong answer, not a stale one, so a partial match must miss.
type reportKey struct {
	worktree string
	baseRef  string
}

// reportCache holds the built report for a key and hands the same one to every
// reader until something invalidates it.
//
// The report costs a full archmotif graph over both the worktree's model and
// its base's, plus the branch's git hunks — around a second on a mid-sized repo
// and several on a large one, paid on every open of the review panel because
// the endpoint carries no ETag and rebuilt everything per request.
//
// One entry per key, computed once however many callers want it: a request that
// arrives while a warm is in flight waits for that build rather than starting a
// second one. The build runs on its own goroutine under a background context,
// so a reviewer who navigates away mid-build still leaves a warm entry behind —
// and so warming never blocks the request that asked for it.
type reportCache struct {
	build func(ctx context.Context, key reportKey) (archreview.Report, error)

	mu      sync.Mutex
	entries map[reportKey]*reportEntry
}

// reportEntry is one build: in flight until done is closed, then final.
// report and err are written before the close and read only after it, so the
// close is what publishes them.
type reportEntry struct {
	done   chan struct{}
	usedAt time.Time
	report archreview.Report
	err    error
}

func newReportCache(build func(ctx context.Context, key reportKey) (archreview.Report, error)) *reportCache {
	return &reportCache{build: build, entries: map[reportKey]*reportEntry{}}
}

// get returns the report for key, waiting for a build already under way or
// starting one. It returns ctx's error if the caller gives up first; the build
// carries on regardless, so the next caller finds it warm.
func (c *reportCache) get(ctx context.Context, key reportKey) (archreview.Report, error) {
	entry := c.start(key)
	select {
	case <-entry.done:
		return entry.report, entry.err
	case <-ctx.Done():
		return archreview.Report{}, ctx.Err()
	}
}

// warm starts a build for key unless one is cached or in flight, and returns
// immediately. It is the whole point of the cache: the panel's first open
// should read an answer, not commission one.
func (c *reportCache) warm(key reportKey) {
	c.start(key)
}

// drop forgets every report built for a worktree. It is called from the model
// event the review UI's SSE stream is published from, so the next request
// rebuilds instead of describing a working tree that has moved on. A build
// still in flight is left to finish, but its entry is gone from the map, so its
// result reaches only the callers already waiting on it and is never served to
// anyone who asked after the change.
func (c *reportCache) drop(worktree string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.entries {
		if key.worktree == worktree {
			delete(c.entries, key)
		}
	}
}

// dropKey forgets one report. It backs the reviewer's explicit refresh, which
// has to reach past the cache: the daemon learns about a changed working tree
// from the model event, and a change that leaves the model alone — an edited
// comment, a base branch that moved under the worktree — produces no event at
// all. Pressing refresh and getting the stored answer back would make the
// button a lie.
func (c *reportCache) dropKey(key reportKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// start returns the entry for key, creating and launching one when the cache
// has none.
func (c *reportCache) start(key reportKey) *reportEntry {
	c.mu.Lock()
	if entry, ok := c.entries[key]; ok {
		entry.usedAt = time.Now()
		c.mu.Unlock()
		return entry
	}
	entry := &reportEntry{done: make(chan struct{}), usedAt: time.Now()}
	c.entries[key] = entry
	c.evictLocked()
	c.mu.Unlock()

	go c.run(key, entry)
	return entry
}

// run builds the report and publishes it by closing done.
func (c *reportCache) run(key reportKey, entry *reportEntry) {
	// A background context, not the requester's: the build outlives whoever
	// triggered it, which is what lets a warm survive the request that started
	// it and a request survive a reviewer who closes the panel.
	entry.report, entry.err = c.build(context.Background(), key)

	c.mu.Lock()
	// A failure is not an answer worth keeping: the next open must ask the
	// daemon again rather than replay an error it may already have outgrown.
	if entry.err != nil && c.entries[key] == entry {
		delete(c.entries, key)
	}
	c.mu.Unlock()
	close(entry.done)
}

// evictLocked keeps the cache within maxCachedReports, dropping the least
// recently read entry. This is a bound, not a tuned replacement policy: the
// entries worth keeping are the warmed ones, and those are read on every open
// and rebuilt on every model change.
func (c *reportCache) evictLocked() {
	for len(c.entries) > maxCachedReports {
		var oldest reportKey
		var usedAt time.Time
		first := true
		for key, entry := range c.entries {
			if first || entry.usedAt.Before(usedAt) {
				oldest, usedAt, first = key, entry.usedAt, false
			}
		}
		delete(c.entries, oldest)
	}
}
