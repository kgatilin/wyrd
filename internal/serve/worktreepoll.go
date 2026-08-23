package serve

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// defaultWorktreePollInterval is how often a multi-worktree daemon re-runs
// `git worktree list`. The scan is one cheap git invocation, and it is the
// only thing that notices a worktree created after startup without anyone
// asking for it by name first — which is what keeps the registry record, and
// therefore every client that reads it, describing the repository as it is
// now. A var, not a const, so package-internal tests can shrink it.
var defaultWorktreePollInterval = 30 * time.Second

// pollWorktrees re-discovers the repository's worktrees on a ticker until ctx
// is cancelled. It reacts to nothing itself: MultiState.Refresh reports any
// change through the WorktreesChangedHook, so the poll and the request-driven
// re-discovery share one reaction path. Scan errors are logged and the loop
// continues — a repository that is momentarily unreadable (a rebase moving
// .git around) must not silently stop the daemon from tracking worktrees.
func pollWorktrees(ctx context.Context, m *MultiState, interval time.Duration, logOut io.Writer) {
	if m == nil || interval <= 0 {
		return
	}
	if logOut == nil {
		logOut = io.Discard
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.Refresh(); err != nil {
				fmt.Fprintf(logOut, "serve: worktree poll: %v\n", err)
			}
		}
	}
}

// daemonRecordPublisher owns this daemon's entry in the global registry. The
// record is written once, when the listener binds, and read by every CLI
// client to decide which worktree to address; a client standing in a worktree
// the record does not list falls back to the primary checkout. Keeping the
// published copy here lets the worktree list be corrected in place, without
// the caller having to reconstruct the address, PID and capabilities that were
// only known at bind time.
type daemonRecordPublisher struct {
	mu        sync.Mutex
	rec       DaemonRecord
	published bool
	retired   bool
}

// publish writes rec to the registry and remembers it as the current record.
func (p *daemonRecordPublisher) publish(rec DaemonRecord) error {
	p.mu.Lock()
	p.rec = rec
	p.published = true
	p.mu.Unlock()
	return WriteGlobalRecord(rec)
}

// republishWorktrees rewrites the published record with a new worktree list.
// It reports false when nothing has been published yet — the listener has not
// come up, so there is no stale record to correct and the eventual publish
// will carry the current list anyway — or once the daemon has retired.
func (p *daemonRecordPublisher) republishWorktrees(names []string) (bool, error) {
	p.mu.Lock()
	if !p.published || p.retired {
		p.mu.Unlock()
		return false, nil
	}
	p.rec.Worktrees = names
	rec := p.rec
	p.mu.Unlock()
	return true, WriteGlobalRecord(rec)
}

// retire closes the publisher. Called on the shutdown path before the record
// is removed, so a hook goroutine that was already running cannot write the
// record back for a process that is exiting.
func (p *daemonRecordPublisher) retire() {
	p.mu.Lock()
	p.retired = true
	p.mu.Unlock()
}
