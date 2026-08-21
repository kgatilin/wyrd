package serve

import (
	"path/filepath"

	"github.com/kgatilin/wyrd/internal/retrieval"
)

// vectorCacheDirName is the wyrd-home subdirectory holding the per-repo
// shared vector caches.
const vectorCacheDirName = "embeddings"

// VectorCacheProvider hands out the repo-level vector caches that a State's
// retrieval service consults before calling the embedder, and persists them.
//
// One provider is shared by every worktree State in a daemon: because vectors
// are keyed by content hash alone, a worktree only embeds the nodes whose
// text no other worktree has embedded yet. Implemented by
// adapter/vindex/vecstore.Store. A nil provider disables sharing, and each
// worktree embeds from scratch as before.
type VectorCacheProvider interface {
	// Namespace returns the cache for an embedder+recipe namespace
	// (see retrieval.VectorCacheNamespace).
	Namespace(ns string) retrieval.VectorCache

	// Save persists whatever the caches gained since the last call.
	Save() error
}

// VectorCacheDir returns the directory holding the shared vector cache for a
// repository: <wyrd home>/embeddings/<repo key>, alongside the daemon
// registry and keyed the same way.
//
// Pass the MAIN worktree root (worktree.RepoRoot) so a daemon started from a
// linked worktree resolves to the same directory as one started from the
// repo root — sharing across worktrees is the entire point.
func VectorCacheDir(repoRoot string) (string, error) {
	base, err := wyrdHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, vectorCacheDirName, repoKey(repoRoot)), nil
}
