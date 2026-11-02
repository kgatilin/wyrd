package golang

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	goversion "go/version"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/kgatilin/wyrd/internal/overlay"
)

// module is one Go module the model is read from: its module path and its
// directory, slash-separated and relative to the root the model is keyed by
// ("." for the root module). A package's model path is its directory under
// that root, so a package of a module in dir "api" is "api/<rest>".
type module struct {
	path string
	dir  string
}

// workspace is what one read covers when the project lists its modules
// (overlay go.modules): the modules, longest path first so a nested module
// path wins over its parent's, and the go.work that makes go/packages load
// them as one build.
type workspace struct {
	root    string
	modules []module
	goWork  string
}

// loadWorkspace reads go.modules from the overlay at root. Nil without
// one: the read is of the module at root, as `go list ./...` would read it.
func loadWorkspace(root string) (*workspace, error) {
	dirs, err := overlay.GoModules(root)
	if err != nil || len(dirs) == 0 {
		return nil, err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	ws := &workspace{root: rootAbs}
	goVer := ""
	for _, dir := range dirs {
		gomod := filepath.Join(rootAbs, filepath.FromSlash(dir), "go.mod")
		data, err := os.ReadFile(gomod)
		if err != nil {
			return nil, fmt.Errorf("go.modules: %s: %w", dir, err)
		}
		f, err := modfile.ParseLax(gomod, data, nil)
		if err != nil {
			return nil, fmt.Errorf("go.modules: %s: %w", dir, err)
		}
		if f.Module == nil || f.Module.Mod.Path == "" {
			return nil, fmt.Errorf("go.modules: %s: go.mod declares no module", dir)
		}
		ws.modules = append(ws.modules, module{path: f.Module.Mod.Path, dir: dir})
		if f.Go != nil && (goVer == "" || goversion.Compare("go"+f.Go.Version, "go"+goVer) > 0) {
			goVer = f.Go.Version
		}
	}
	sort.SliceStable(ws.modules, func(i, j int) bool {
		return len(ws.modules[i].path) > len(ws.modules[j].path)
	})
	ws.goWork, err = writeGoWork(rootAbs, dirs, goVer)
	if err != nil {
		return nil, err
	}
	return ws, nil
}

// writeGoWork writes the go.work for the listed modules and returns its
// path. It never goes into the project: a go.work there would change how
// the project builds for everyone who runs go in it. The file is named by
// its content, so concurrent reads of one project write the same bytes to
// the same place, and a go.work.sum go writes beside it stays with it.
func writeGoWork(rootAbs string, dirs []string, goVer string) (string, error) {
	var b strings.Builder
	if goVer != "" {
		fmt.Fprintf(&b, "go %s\n\n", goVer)
	}
	b.WriteString("use (\n")
	for _, dir := range dirs {
		fmt.Fprintf(&b, "\t%s\n", quoteIfNeeded(filepath.Join(rootAbs, filepath.FromSlash(dir))))
	}
	b.WriteString(")\n")
	content := b.String()

	sum := sha256.Sum256([]byte(content))
	dir := filepath.Join(os.TempDir(), "wyrd-gowork", hex.EncodeToString(sum[:8]))
	file := filepath.Join(dir, "go.work")
	if cur, err := os.ReadFile(file); err == nil && string(cur) == content {
		return file, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("go.modules: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "go.work.*")
	if err != nil {
		return "", fmt.Errorf("go.modules: %w", err)
	}
	_, werr := tmp.WriteString(content)
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), file)
	}
	if werr != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("go.modules: writing go.work: %w", werr)
	}
	return file, nil
}

// quoteIfNeeded quotes a go.work path that contains a space or a
// quote, which an unquoted use directive would split.
func quoteIfNeeded(p string) string {
	if strings.ContainsAny(p, " \t\"'`") {
		return fmt.Sprintf("%q", p)
	}
	return p
}

// patterns expands "./..." into one pattern per module: a workspace has no
// module at a root that is not itself listed, and `./...` there matches
// nothing. Every other pattern names directories and passes as written.
func (ws *workspace) patterns(paths []string) []string {
	out := make([]string, 0, len(paths)+len(ws.modules))
	for _, p := range paths {
		if p != "./..." {
			out = append(out, p)
			continue
		}
		for _, m := range ws.modules {
			if m.dir == "." {
				out = append(out, "./...")
			} else {
				out = append(out, "./"+m.dir+"/...")
			}
		}
	}
	return out
}

// moduleOf returns the module pkgPath belongs to, the most specific one
// when module paths nest.
func moduleOf(modules []module, pkgPath string) (module, bool) {
	for _, m := range modules {
		if pkgPath == m.path || strings.HasPrefix(pkgPath, m.path+"/") {
			return m, true
		}
	}
	return module{}, false
}

// rootRelative maps an import path in m to its directory under the root.
func (m module) rootRelative(pkgPath string) string {
	rest := strings.TrimPrefix(strings.TrimPrefix(pkgPath, m.path), "/")
	return path.Join(m.dir, rest)
}
