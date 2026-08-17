package main

import (
	"context"

	"github.com/kgatilin/archai/internal/adapter/golang"
	"github.com/kgatilin/archai/internal/serve"
	"github.com/kgatilin/archai/internal/service"
)

// assembleServeReader builds the model reader for `archai serve` using
// the same wiring as `archai diagram generate`.
func assembleServeReader() service.ModelReader {
	return golang.NewReader()
}

// newServeStateLoader returns a serve.StateLoader that constructs each
// worktree's State with the supplied reader. Mirrors
// serve.DefaultStateLoader's behaviour otherwise.
func newServeStateLoader(reader service.ModelReader) serve.StateLoader {
	return func(ctx context.Context, _, path string) (*serve.State, error) {
		state := serve.NewState(path, serve.WithReader(reader))
		if err := state.Load(ctx); err != nil {
			return nil, err
		}
		return state, nil
	}
}
