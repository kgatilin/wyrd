package java

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveJarPath_ExplicitWins(t *testing.T) {
	t.Setenv(jarEnvVar, "/from/env.jar")
	got, err := resolveJarPath("/explicit/wins.jar", func() (string, error) {
		return "/usr/local/bin/archai", nil
	})
	if err != nil || got != "/explicit/wins.jar" {
		t.Errorf("explicit should win, got %q err=%v", got, err)
	}
}

func TestResolveJarPath_EnvFallback(t *testing.T) {
	t.Setenv(jarEnvVar, "/from/env.jar")
	got, err := resolveJarPath("", func() (string, error) {
		return "/usr/local/bin/archai", nil
	})
	if err != nil || got != "/from/env.jar" {
		t.Errorf("env fallback failed: got %q err=%v", got, err)
	}
}

func TestResolveJarPath_SiblingFallback(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "archai")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	jar := filepath.Join(dir, jarFileName)
	if err := os.WriteFile(jar, []byte("PK"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(jarEnvVar, "")

	got, err := resolveJarPath("", func() (string, error) { return exe, nil })
	if err != nil || got != jar {
		t.Errorf("sibling fallback: got %q err=%v want %q", got, err, jar)
	}
}

func TestResolveJarPath_NotFound(t *testing.T) {
	t.Setenv(jarEnvVar, "")
	_, err := resolveJarPath("", func() (string, error) {
		// Return a path with no sibling jar present.
		return filepath.Join(t.TempDir(), "archai"), nil
	})
	if err == nil {
		t.Fatal("expected an error when nothing resolves")
	}
	if !strings.Contains(err.Error(), "ARCHAI_JAVA_JAR") || !strings.Contains(err.Error(), "java-jar") {
		t.Errorf("error message should mention --java-jar / ARCHAI_JAVA_JAR: %v", err)
	}
}

func TestResolveJarPath_ExeLookupErrorTreatedAsMiss(t *testing.T) {
	t.Setenv(jarEnvVar, "")
	_, err := resolveJarPath("", func() (string, error) {
		return "", errors.New("os.Executable failed")
	})
	if err == nil {
		t.Fatal("expected an error when sibling lookup fails and no env / explicit set")
	}
}

func TestDecodeFacts_Roundtrip(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "facts_simple.json"))
	if err != nil {
		t.Fatal(err)
	}
	facts, err := decodeFacts(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if facts.Schema != schemaVersion {
		t.Errorf("schema: %q", facts.Schema)
	}
	if len(facts.Classes) == 0 {
		t.Error("classes empty")
	}
}

func TestDecodeFacts_RejectsUnknownFields(t *testing.T) {
	bad := []byte(`{"schema":"javafacts/v1","src_roots":[],"packages":[],"classes":[],"imports":[],"parse_warnings":[],"surprise":42}`)
	if _, err := decodeFacts(bad); err == nil {
		t.Error("decodeFacts should reject unknown top-level fields to catch schema drift early")
	}
}
