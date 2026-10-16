package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kgatilin/wyrd/internal/adapter/golang"
	"github.com/kgatilin/wyrd/internal/overlay"
	"github.com/kgatilin/wyrd/internal/retrieval"
	"github.com/kgatilin/wyrd/internal/serve"
	"github.com/kgatilin/wyrd/internal/serve/modelstore"
	"github.com/kgatilin/wyrd/internal/service"
)

// assembleServeReader builds the model reader for `wyrd serve` using
// the same wiring as `wyrd diagram generate`.
func assembleServeReader() service.ModelReader {
	return golang.NewReader()
}

// assembleCorpus opens the repository's retrieval corpus, shared by every
// worktree State the daemon loads, so a text is indexed and embedded once for
// the repository rather than once per worktree. It is keyed by the MAIN
// worktree root, so a daemon started from a linked worktree lands on the same
// corpus as one started from the repo root. A corpus that cannot be opened is
// not fatal: each State then builds a private in-memory one — unless the
// daemon was told its backends are required (serve.RemoteRequired), when the
// reason is returned and the daemon does not start.
func assembleCorpus(root string) (*retrieval.Corpus, error) {
	required := serve.RemoteRequired()
	if os.Getenv("WYRD_RETRIEVAL_DISABLE") == "1" {
		if required {
			return nil, errors.New("WYRD_REQUIRE_REMOTE is set but retrieval is disabled (WYRD_RETRIEVAL_DISABLE)")
		}
		return nil, nil
	}
	corpus, err := serve.OpenCorpus(root)
	if err != nil {
		if required {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "wyrd: shared retrieval corpus disabled: %v\n", err)
		return nil, nil
	}
	return corpus, nil
}

// readinessRecheckTimeout bounds the backend calls one readiness probe may
// make while a channel is at fault.
const readinessRecheckTimeout = 10 * time.Second

// corpusReadiness is what the daemon's readiness probe answers from: the
// corpus's faults, failing the probe only when the backends are required.
// Each probe re-asks the backends at fault, so a daemon the probe took out
// of rotation returns to it without a search having to arrive first.
func corpusReadiness(corpus *retrieval.Corpus) (func() map[string]string, bool) {
	if corpus == nil {
		return nil, false
	}
	return func() map[string]string {
		ctx, cancel := context.WithTimeout(context.Background(), readinessRecheckTimeout)
		defer cancel()
		return corpus.Recheck(ctx)
	}, serve.RemoteRequired()
}

// assembleModelStore opens the repository's shared model store, so every
// worktree State and every review base holds one copy of each unchanged
// package. A store that cannot be opened is not fatal: the daemon runs
// without it and every worktree parses in full on a cold start.
func assembleModelStore(root string) *modelstore.Store {
	store, err := serve.OpenModelStore(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wyrd: shared model store disabled: %v\n", err)
		return nil
	}
	return store
}

// newServeStateLoader returns a serve.StateLoader that constructs each
// worktree's State with the supplied reader, shared retrieval corpus and
// shared model store; a branch cold-starts over the snapshot of its merge base with
// baseRef. Mirrors serve.DefaultStateLoader's behaviour otherwise.
func newServeStateLoader(reader service.ModelReader, corpus *retrieval.Corpus, store *modelstore.Store, baseRef string) serve.StateLoader {
	return func(ctx context.Context, _, path string) (*serve.State, error) {
		opts := []serve.StateOption{serve.WithReader(reader)}
		if corpus != nil {
			opts = append(opts, serve.WithCorpus(corpus))
		}
		if store != nil {
			opts = append(opts, serve.WithModelStore(store, baseRef))
		}
		state := serve.NewState(path, opts...)
		if err := state.Load(ctx); err != nil {
			return nil, err
		}
		return state, nil
	}
}

// resolveServeAuthToken returns the bearer token the daemon's MCP endpoint
// requires, or "" to leave it open. WYRD_HTTP_TOKEN wins over the overlay's
// serve.auth_token, so a workstation can hold the secret outside a file that
// is committed with the project.
func resolveServeAuthToken(root string) (string, error) {
	if env := strings.TrimSpace(os.Getenv("WYRD_HTTP_TOKEN")); env != "" {
		return env, nil
	}
	return overlay.ServeAuthToken(root)
}
