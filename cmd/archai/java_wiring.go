package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	javaAdapter "github.com/kgatilin/archai/internal/adapter/java"
	"github.com/kgatilin/archai/internal/adapter/golang"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/serve"
	"github.com/kgatilin/archai/internal/service"
)

// tryAssembleJavaReader returns a Java ModelReader if the toolchain is
// usable: `java` resolves on PATH AND a JAR can be located via the
// flag, env var, or sibling-of-binary lookup. Otherwise it returns nil
// plus an optional one-line note for stderr explaining what's missing
// (so users with pure-Go projects don't trip over the Java code path).
//
// jarFlag is the value of `--java-jar`. Empty means "auto-detect".
func tryAssembleJavaReader(jarFlag string) (*javaAdapter.Reader, string) {
	if _, err := exec.LookPath("java"); err != nil {
		// `java` missing is a soft-disable: the user might just be
		// working on a Go-only project.
		return nil, ""
	}

	jarPath := jarFlag
	if jarPath == "" {
		jarPath = os.Getenv("ARCHAI_JAVA_JAR")
	}
	if jarPath == "" {
		// Sibling-of-binary fallback. We mirror the resolution chain in
		// adapter/java but pre-check it here so we can surface a clear
		// "not bundled" note rather than failing later inside Read().
		if exe, err := os.Executable(); err == nil {
			candidate := filepath.Join(filepath.Dir(exe), "archai-java-analyzer.jar")
			if _, statErr := os.Stat(candidate); statErr == nil {
				jarPath = candidate
			}
		}
	}
	if jarPath == "" {
		return nil, "Note: Java sources will be skipped (set --java-jar or ARCHAI_JAVA_JAR to enable)."
	}

	if _, err := os.Stat(jarPath); err != nil {
		return nil, fmt.Sprintf("Note: Java sources will be skipped (jar at %q not readable: %v).", jarPath, err)
	}

	return javaAdapter.NewReader(jarPath), ""
}

// dispatcherReader adapts a *service.Service into the
// service.ModelReader interface so callers (notably internal/serve)
// can plug the multi-language read pipeline behind a single
// Read(ctx, paths) call.
type dispatcherReader struct {
	svc *service.Service
}

// Read forwards to svc.ReadModels, which honours the multi-language
// dispatch (Go / Java / future readers) wired through service.Options.
func (d dispatcherReader) Read(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
	return d.svc.ReadModels(ctx, paths)
}

// assembleServeReader builds a multi-language reader for `archai serve`
// using the same wiring as `archai diagram generate`. The Java reader
// is registered when the toolchain is available; the Go reader gets a
// match predicate so pure-Java projects don't trip over the Go loader's
// "directory prefix . does not contain main module" failure.
//
// The optional note is the same toolchain advisory surfaced by
// tryAssembleJavaReader; callers may print it to stderr.
func assembleServeReader() (service.ModelReader, string) {
	javaReader, note := tryAssembleJavaReader("")
	opts := []service.Option{}
	if javaReader != nil {
		opts = append(opts,
			service.WithJavaReader(javaReader),
			service.WithGoLanguageMatcher(service.MatchSubtreeHasExt(".go")),
		)
	}
	svc := service.NewService(golang.NewReader(), nil, nil, opts...)
	return dispatcherReader{svc: svc}, note
}

// newServeStateLoader returns a serve.StateLoader that constructs each
// worktree's State with the supplied multi-language reader. Mirrors
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
