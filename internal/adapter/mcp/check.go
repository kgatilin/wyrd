package mcp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kgatilin/wyrd/internal/check"
	"github.com/kgatilin/wyrd/internal/domain"
	"github.com/kgatilin/wyrd/internal/overlay"
	"github.com/kgatilin/wyrd/internal/serve"
)

// checkToolDefinition is a function for the reason builtinToolDefinitions is.
func checkToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name:        "check",
		Description: "Run the architecture gates — the overlay's layer rules and its dependency policy — over this worktree as it is on disk now. The daemon first syncs its model with the disk (files whose mtime or size moved since they were read are read again), so an edit saved a moment ago is checked without waiting for a file event. The answer is the gates' text report; isError is set when a gate fails, the overlay is invalid or missing, or the sync could not read the tree. The model is the daemon's incremental one: an `implements` edge from an untouched package to an interface a changed package added is only seen after a full parse, as CI runs it.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

// modelSource hands the gates a model already in memory. The gates ask for
// the whole tree, which is the only thing a worktree's model holds.
type modelSource []domain.PackageModel

func (m modelSource) Read(context.Context, []string) ([]domain.PackageModel, error) {
	return m, nil
}

// handleCheck syncs the worktree's model with the disk and runs the overlay
// and dependency-policy gates over it.
func handleCheck(ctx context.Context, state *serve.State) (ToolResult, *RPCError) {
	root, ok := requireRoot(state)
	if !ok {
		return errorResult("daemon state has no project root configured"), nil
	}
	moved, err := state.Sync(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("syncing the model with the disk: %v", err)), nil
	}
	// The sync consumed the change the watcher would have broadcast.
	if len(moved) > 0 {
		state.PublishPackageReload(moved)
	}

	overlayPath := overlay.DiscoverPath(root)
	if overlayPath == "" {
		return errorResult("no overlay: create wyrd.yaml in the worktree root"), nil
	}
	goModPath := filepath.Join(root, "go.mod")
	if _, err := os.Stat(goModPath); err != nil {
		goModPath = ""
	}

	var out bytes.Buffer
	err = check.New(modelSource(state.Snapshot().Packages), nil).
		RunAll(ctx, check.AllOptions{OverlayPath: overlayPath, GoModPath: goModPath}, &out, &out)
	if err != nil {
		fmt.Fprintf(&out, "\n%v\n", err)
		return errorResult(out.String()), nil
	}
	return text(out.String())
}
