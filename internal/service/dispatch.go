package service

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
)

// languageReader pairs a ModelReader with a path-matching predicate. A
// reader is invoked only for the input paths whose subtree contains the
// language's source files. The concrete type is unexported because
// callers wire readers via the WithJavaReader / WithLanguageReader
// options rather than constructing this struct directly.
type languageReader struct {
	name   string
	reader ModelReader
	// match returns true if the given input path's subtree contains
	// source files this reader can analyse. When match is nil, the
	// reader runs unconditionally (legacy behaviour for the Go reader
	// passed to NewService).
	match func(path string) bool
}

// ReadModels is the exported counterpart of readPackages. CLI callsites
// that want to bypass the full Generate pipeline (e.g. combined-full
// mode in cmd/archai) can use this to honour the multi-language
// dispatch without re-implementing it.
func (s *Service) ReadModels(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
	return s.readPackages(ctx, paths)
}

// readPackages dispatches the input paths to every registered language
// reader whose match predicate accepts them, then concatenates the
// resulting models. This is the single entry point used by both
// generateInternal and generateCombined so the dispatch logic stays in
// one place.
//
// Behaviour summary:
//
//   - The Go reader passed to NewService runs on every path (legacy
//     behaviour) unless the caller registers a Go matcher via
//     WithGoLanguageMatcher. This keeps existing tests green.
//   - Additional language readers (e.g., Java via WithJavaReader) only
//     run on paths whose match predicate returns true.
//   - If no reader produces any output AND at least one was registered
//     beyond Go, returns a "no supported sources" error so the user gets
//     a clear signal instead of silent empty output.
func (s *Service) readPackages(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
	// Backward-compat: callers that build a Service via struct literal
	// (older internal tests) skip NewService's langReaders bootstrap.
	// Fall back to the bare goReader so they keep working unchanged.
	if len(s.langReaders) == 0 {
		if s.goReader == nil {
			return nil, fmt.Errorf("service has no language readers configured")
		}
		return s.goReader.Read(ctx, paths)
	}

	var all []domain.PackageModel
	for _, lr := range s.langReaders {
		matched := paths
		if lr.match != nil {
			matched = filterMatchingPaths(paths, lr.match)
			if len(matched) == 0 {
				continue
			}
		}
		// Non-Go readers don't understand Go's "./..." package pattern;
		// strip it so the reader receives a plain directory path.
		// The Go reader keeps the pattern as-is — packages.Load needs it
		// to discover sub-packages.
		if lr.name != "go" {
			matched = stripGoPatternFromAll(matched)
		}
		pkgs, err := lr.reader.Read(ctx, matched)
		if err != nil {
			return nil, fmt.Errorf("%s reader: %w", lr.name, err)
		}
		all = append(all, pkgs...)
	}

	if len(all) == 0 && len(s.langReaders) > 1 {
		// Only error out for multi-reader setups: a single Go-reader
		// service may legitimately yield zero packages (empty workspace),
		// matching legacy behaviour.
		return nil, fmt.Errorf("no supported sources at %v", paths)
	}
	return all, nil
}

func filterMatchingPaths(paths []string, match func(string) bool) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if match(p) {
			out = append(out, p)
		}
	}
	return out
}

// matchSubtreeHasExt returns a path matcher that walks the path's
// subtree and reports whether any file has the given suffix. Used as
// the default matcher for language readers registered via
// WithJavaReader / WithLanguageReader.
//
// "./..." style Go patterns are stripped before walking — the heuristic
// reduces them to the leading directory.
func matchSubtreeHasExt(ext string) func(string) bool {
	return func(path string) bool {
		root := stripGoPattern(path)
		info, err := os.Stat(root)
		if err != nil {
			return false
		}
		if !info.IsDir() {
			return strings.HasSuffix(root, ext)
		}
		var found bool
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // skip unreadable subtrees rather than failing detection
			}
			if d.IsDir() {
				// Always descend into the root we were given (its Name
				// can be "." when callers pass the cwd shorthand, and
				// the hidden-dir prefix check below would otherwise
				// skip the entire tree).
				if p == root {
					return nil
				}
				if strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "target" {
					return fs.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(d.Name(), ext) {
				found = true
				return fs.SkipAll
			}
			return nil
		})
		return found
	}
}

// stripGoPattern reduces a `./...` style path to its directory root so
// os.Stat / filepath.WalkDir can operate on it. "./internal/..." → "./internal".
func stripGoPattern(path string) string {
	if strings.HasSuffix(path, "/...") {
		return strings.TrimSuffix(path, "/...")
	}
	if path == "..." {
		return "."
	}
	return path
}

// stripGoPatternFromAll applies stripGoPattern to every input path.
func stripGoPatternFromAll(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = stripGoPattern(p)
	}
	return out
}
