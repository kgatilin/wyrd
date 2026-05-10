package java

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/service"
)

// Reader reads Java source by shelling out to archai-java-analyzer.jar.
//
// Reader implements service.ModelReader. The underlying JAR is resolved
// at first call (not at construction) so that explicit-jar / env / sibling
// resolution sees the runtime environment. Successive Read calls reuse
// the same resolved path.
//
// After a successful Read, Warnings() returns any non-fatal parse warnings
// that the JAR surfaced; the run still succeeded.
type Reader struct {
	jarPath  string // explicit override; empty means "auto-resolve"
	resolved string // cached after first successful resolution

	mu       sync.Mutex
	warnings []ParseWarning
}

// ParseWarning is a non-fatal parse problem surfaced by the JAR.
// Mirrors tools/archai-java-analyzer/SCHEMA.md `ParseWarning` shape.
type ParseWarning struct {
	File    string
	Message string
}

// NewReader returns a Java code reader. jarPath is the explicit JAR path;
// empty falls back to ARCHAI_JAVA_JAR / sibling-of-binary resolution at
// first Read (see resolveJarPath).
func NewReader(jarPath string) *Reader {
	return &Reader{jarPath: jarPath}
}

// Compile-time interface check. The CLI / service layer can pass a *Reader
// anywhere a ModelReader is expected.
var _ service.ModelReader = (*Reader)(nil)

// Read invokes the analyzer JAR over the configured source roots and
// returns the resulting domain model. paths are passed through verbatim
// to the JAR — the JAR itself decides which directories to recurse into.
func (r *Reader) Read(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("java reader: no source paths")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	jar, err := r.ensureJar()
	if err != nil {
		return nil, err
	}

	facts, err := runAnalyzer(ctx, jar, paths)
	if err != nil {
		return nil, err
	}

	r.recordWarnings(facts.ParseWarnings)
	return translate(facts), nil
}

// Warnings returns the non-fatal parse warnings surfaced by the most
// recent Read invocation(s). Order is preserved across multiple Reads.
func (r *Reader) Warnings() []ParseWarning {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ParseWarning, len(r.warnings))
	copy(out, r.warnings)
	return out
}

func (r *Reader) ensureJar() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.resolved != "" {
		return r.resolved, nil
	}
	jar, err := resolveJarPath(r.jarPath, os.Executable)
	if err != nil {
		return "", err
	}
	r.resolved = jar
	return jar, nil
}

func (r *Reader) recordWarnings(in []parseWarning) {
	if len(in) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, w := range in {
		r.warnings = append(r.warnings, ParseWarning{File: w.File, Message: w.Message})
	}
}
