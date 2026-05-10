package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	javaAdapter "github.com/kgatilin/archai/internal/adapter/java"
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
