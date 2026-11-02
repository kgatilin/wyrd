// Package treestamp records what a Go model is read from: the (mtime, size)
// of every file that contributes to it. Two scans of one tree that differ
// name the packages whose source changed; a fingerprint of a scan lets a
// client and a daemon tell whether they look at the same source.
package treestamp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kgatilin/wyrd/internal/overlay"
)

// Stamp is what an edit changes and an inspection does not.
type Stamp struct {
	mod  int64
	size int64
}

// Tree is the stamp of every file the model is read from, keyed by
// slash-separated path relative to the scanned root: each non-test .go file
// under the directories the Go tool walks, plus every go.mod and go.sum
// among them. When the overlay lists go.modules, the list is one more entry,
// keyed by the list itself: the modules a read covers are as much what the
// model is read from as the files are, so a changed list is a changed tree.
type Tree map[string]Stamp

// modulesKey prefixes the entry that records go.modules. No file is stamped
// under it: only .go files, go.mod and go.sum are.
const modulesKey = "go.modules="

// Scan stats the source files of the tree at root without reading them.
func Scan(root string) (Tree, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	out := make(Tree)
	err = filepath.WalkDir(rootAbs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != rootAbs && SkipDir(d.Name()) {
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
			out[rel] = Stamp{mod: info.ModTime().UnixNano(), size: info.Size()}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	mods, err := overlay.GoModules(rootAbs)
	if err != nil {
		return nil, err
	}
	if len(mods) > 0 {
		out[modulesKey+strings.Join(mods, ",")] = Stamp{}
	}
	return out, nil
}

// SkipDir reports whether the Go tool skips a directory of this name when it
// expands ./...: dot and underscore directories, testdata, vendor, and
// node_modules.
func SkipDir(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "testdata":
		return true
	}
	return false
}

// isModuleFile reports whether rel decides what every package reads as: a
// go.mod or go.sum at any depth, or the go.modules entry. A nested go.mod
// counts whether or not the model reads that module, which costs a full
// parse for an edit in a module the model leaves out.
func isModuleFile(rel string) bool {
	if strings.HasPrefix(rel, modulesKey) {
		return true
	}
	base := path.Base(rel)
	return base == "go.mod" || base == "go.sum"
}

// isModelSource reports whether a file contributes to the model. Test files
// do not: the reader leaves them out.
func isModelSource(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

// Changed returns the package directories whose source differs from prev —
// a file edited, added or removed — and whether a go.mod, a go.sum or the
// go.modules list did.
func (t Tree) Changed(prev Tree) (dirs []string, module bool) {
	set := make(map[string]bool)
	mark := func(rel string) {
		if isModuleFile(rel) {
			module = true
			return
		}
		set[filepath.ToSlash(filepath.Dir(rel))] = true
	}
	for rel, stamp := range t {
		if old, ok := prev[rel]; !ok || old != stamp {
			mark(rel)
		}
	}
	for rel := range prev {
		if _, ok := t[rel]; !ok {
			mark(rel)
		}
	}
	for dir := range set {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs, module
}

// Fingerprint identifies the scan: equal fingerprints mean the same files
// with the same stamps under the same relative paths. An empty Tree has one
// too, so "no source" is a fingerprint like any other.
func (t Tree) Fingerprint() string {
	paths := make([]string, 0, len(t))
	for rel := range t {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		s := t[rel]
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", rel, s.mod, s.size)
	}
	return hex.EncodeToString(h.Sum(nil))
}
