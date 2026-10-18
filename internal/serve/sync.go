package serve

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kgatilin/wyrd/internal/serve/modelstore"
)

// contentStamp is what an edit changes and an inspection does not.
type contentStamp struct {
	mod  int64
	size int64
}

func stampOf(info os.FileInfo) contentStamp {
	return contentStamp{mod: info.ModTime().UnixNano(), size: info.Size()}
}

// treeStamps is the content stamp of every file the model is read from,
// keyed by slash-separated path relative to the worktree root: each non-test
// .go file under the directories the Go tool walks, plus the root go.mod and
// go.sum.
type treeStamps map[string]contentStamp

// scanStamps stats the source files of the tree at root without reading them.
func scanStamps(root string) (treeStamps, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	out := make(treeStamps)
	err = filepath.WalkDir(rootAbs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != rootAbs && skipSourceDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !isModuleFile(rel) && !isModelSource(d.Name()) {
			return nil
		}
		if info, err := d.Info(); err == nil {
			out[rel] = stampOf(info)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func isModuleFile(rel string) bool {
	return rel == "go.mod" || rel == "go.sum"
}

// isModelSource reports whether a file contributes to the model. Test files
// do not: the reader leaves them out.
func isModelSource(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

// changedSince returns the package directories whose source differs from
// prev — a file edited, added or removed — and whether go.mod or go.sum did.
func (now treeStamps) changedSince(prev treeStamps) (dirs []string, module bool) {
	set := make(map[string]bool)
	mark := func(rel string) {
		if isModuleFile(rel) {
			module = true
			return
		}
		set[filepath.ToSlash(filepath.Dir(rel))] = true
	}
	for rel, stamp := range now {
		if old, ok := prev[rel]; !ok || old != stamp {
			mark(rel)
		}
	}
	for rel := range prev {
		if _, ok := now[rel]; !ok {
			mark(rel)
		}
	}
	for dir := range set {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs, module
}

// Sync brings the model to what is on disk now and returns the packages
// whose model moved (nil when nothing did).
//
// It compares the tree's content stamps with the ones recorded when the
// model was read, so it answers without trusting that every file event
// arrived: the packages whose files differ are read over the current model,
// one whose source is gone is dropped, and a change to go.mod or go.sum is a
// full parse. An unchanged tree costs one walk of stats. Every read records
// the stamps it took before reading, so a file edited during a read keeps
// its old stamp and the next Sync reads it again.
func (s *State) Sync(ctx context.Context) ([]string, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()

	now, err := scanStamps(s.root)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	prev := s.stamps
	s.mu.RUnlock()

	dirs, module := now.changedSince(prev)
	var versions []modelstore.Version
	switch {
	case module:
		versions, err = s.readAll(ctx)
	case len(dirs) == 0:
		return nil, nil
	default:
		versions, err = s.readOver(ctx, s.versionList(), dirs)
	}
	if err != nil {
		return nil, err
	}
	moved, _, err := s.replaceModel(ctx, versions, now)
	return moved, err
}
