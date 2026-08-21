package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kgatilin/wyrd/internal/adapter/golang"
	"github.com/kgatilin/wyrd/internal/adapter/vindex/vecstore"
	"github.com/kgatilin/wyrd/internal/serve"
	"github.com/kgatilin/wyrd/internal/service"
	"github.com/kgatilin/wyrd/internal/worktree"
)

// assembleServeReader builds the model reader for `wyrd serve` using
// the same wiring as `wyrd diagram generate`.
func assembleServeReader() service.ModelReader {
	return golang.NewReader()
}

// assembleVectorCache builds the repo-level vector cache shared by every
// worktree State the daemon loads, so a fresh branch reuses the embeddings
// its siblings already paid for instead of re-running the whole dense pass.
//
// It is keyed by the MAIN worktree root, so a daemon started from a linked
// worktree lands on the same store as one started from the repo root. A store
// that cannot be located is not fatal: the daemon runs without it and each
// worktree embeds for itself, exactly as before.
func assembleVectorCache(root string) serve.VectorCacheProvider {
	abs, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wyrd: shared vector cache disabled: resolving %q: %v\n", root, err)
		return nil
	}
	repoRoot := abs
	if mainRoot, ok := worktree.RepoRoot(abs); ok {
		repoRoot = mainRoot
	}
	dir, err := serve.VectorCacheDir(repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wyrd: shared vector cache disabled: %v\n", err)
		return nil
	}
	return vecstore.New(dir)
}

// newServeStateLoader returns a serve.StateLoader that constructs each
// worktree's State with the supplied reader and shared vector cache.
// Mirrors serve.DefaultStateLoader's behaviour otherwise.
func newServeStateLoader(reader service.ModelReader, vecCache serve.VectorCacheProvider) serve.StateLoader {
	return func(ctx context.Context, _, path string) (*serve.State, error) {
		opts := []serve.StateOption{serve.WithReader(reader)}
		if vecCache != nil {
			opts = append(opts, serve.WithVectorCache(vecCache))
		}
		state := serve.NewState(path, opts...)
		if err := state.Load(ctx); err != nil {
			return nil, err
		}
		return state, nil
	}
}
