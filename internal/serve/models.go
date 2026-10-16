package serve

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/kgatilin/wyrd/internal/adapter/golang"
	"github.com/kgatilin/wyrd/internal/serve/modelstore"
	"github.com/kgatilin/wyrd/internal/worktree"
)

// modelStoreDirName is the wyrd-home subdirectory holding the per-repo shared
// model stores, alongside the vector caches and the base trees.
const modelStoreDirName = "models"

// ModelStoreDir returns the directory holding the shared model store for a
// repository: <wyrd home>/models/<repo key>. Pass the MAIN worktree root so
// every worktree of the repository lands on the same store.
func ModelStoreDir(repoRoot string) (string, error) {
	base, err := wyrdHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, modelStoreDirName, repoKey(repoRoot)), nil
}

// OpenModelStore opens the model store of the repository containing root,
// keyed by the reader's model version: models survive a rebuild of the binary
// and are discarded when golang.ModelVersion moves.
func OpenModelStore(root string) (*modelstore.Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("serve: resolving %q: %w", root, err)
	}
	repoRoot := abs
	if mainRoot, ok := worktree.RepoRoot(abs); ok {
		repoRoot = mainRoot
	}
	dir, err := ModelStoreDir(repoRoot)
	if err != nil {
		return nil, err
	}
	return modelstore.Open(dir, "go-reader@v"+strconv.Itoa(golang.ModelVersion))
}
