package java

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// jarFileName is the conventional sibling-binary name we look for last
// when resolving the analyzer JAR.
const jarFileName = "archai-java-analyzer.jar"

// jarEnvVar is the env var consulted second in the resolution chain.
const jarEnvVar = "ARCHAI_JAVA_JAR"

// resolveJarPath finds the JavaFacts analyzer JAR. Priority:
//  1. explicit non-empty constructor arg
//  2. ARCHAI_JAVA_JAR env var
//  3. sibling of the running binary: <exe-dir>/archai-java-analyzer.jar
//
// On miss, it returns a friendly error pointing the user at --java-jar /
// ARCHAI_JAVA_JAR. exeLookup is parameterised so tests can stub out
// os.Executable without touching the filesystem.
func resolveJarPath(explicit string, exeLookup func() (string, error)) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if env := os.Getenv(jarEnvVar); env != "" {
		return env, nil
	}
	exe, err := exeLookup()
	if err == nil && exe != "" {
		sibling := filepath.Join(filepath.Dir(exe), jarFileName)
		if _, statErr := os.Stat(sibling); statErr == nil {
			return sibling, nil
		}
	}
	return "", fmt.Errorf("Java sources detected but no %s found. Set --java-jar or %s.", jarFileName, jarEnvVar)
}

// runAnalyzer invokes `java -jar <jarPath> <paths...>`, captures stdout
// (the JavaFacts JSON document) and parses it.
//
// stderr is captured and surfaced verbatim on non-zero exit. The `java`
// binary is resolved via PATH; if it is missing the error is wrapped with
// an actionable message.
func runAnalyzer(ctx context.Context, jarPath string, paths []string) (*javaFacts, error) {
	javaBin, err := exec.LookPath("java")
	if err != nil {
		return nil, fmt.Errorf("`java` not found on PATH; install a JRE 21+ or set JAVA_HOME: %w", err)
	}

	args := append([]string{"-jar", jarPath}, paths...)
	cmd := exec.CommandContext(ctx, javaBin, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if runErr := cmd.Run(); runErr != nil {
		return nil, fmt.Errorf("archai-java-analyzer.jar failed: %w\nstderr:\n%s", runErr, stderr.String())
	}

	facts, parseErr := decodeFacts(stdout.Bytes())
	if parseErr != nil {
		return nil, fmt.Errorf("decoding JavaFacts JSON: %w", parseErr)
	}
	return facts, nil
}

// decodeFacts parses a JavaFacts JSON document and validates the schema
// version. Pulled out of runAnalyzer so it can be tested without invoking
// `java`.
func decodeFacts(raw []byte) (*javaFacts, error) {
	var facts javaFacts
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&facts); err != nil {
		return nil, err
	}
	if facts.Schema != schemaVersion {
		return nil, fmt.Errorf("unsupported JavaFacts schema %q (want %q)", facts.Schema, schemaVersion)
	}
	return &facts, nil
}
