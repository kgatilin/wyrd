package mcp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/wyrd/internal/serve"
)

// TestCheckReadsAnEditNoEventReported: the State has no watcher, so the only
// way the tool can see the inversion written after the load is its own sync.
func TestCheckReadsAnEditNoEventReported(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module fake.test\n\ngo 1.21\n")
	mustWrite(t, filepath.Join(dir, "wyrd.yaml"), `module: fake.test
layers:
  domain:
    - internal/domain/...
  adapter:
    - internal/adapter/...
layer_rules:
  adapter:
    - domain
  domain: []
`)
	mustWrite(t, filepath.Join(dir, "internal", "domain", "domain.go"), "package domain\n\ntype Model struct{}\n")
	mustWrite(t, filepath.Join(dir, "internal", "adapter", "adapter.go"), `package adapter

import "fake.test/internal/domain"

type Reader struct{ M domain.Model }
`)
	state := serve.NewState(dir)
	if err := state.Load(context.Background()); err != nil {
		t.Fatalf("load state: %v", err)
	}

	res, rpcErr := Dispatch(state, "check", nil)
	if rpcErr != nil {
		t.Fatalf("check: %v", rpcErr)
	}
	if res.IsError {
		t.Fatalf("check failed on a clean tree:\n%s", res.Content[0].Text)
	}

	mustWrite(t, filepath.Join(dir, "internal", "domain", "domain.go"), `package domain

import "fake.test/internal/adapter"

type Model struct{ R adapter.Reader }
`)
	mustWrite(t, filepath.Join(dir, "internal", "adapter", "adapter.go"), "package adapter\n\ntype Reader struct{}\n")

	res, rpcErr = Dispatch(state, "check", nil)
	if rpcErr != nil {
		t.Fatalf("check: %v", rpcErr)
	}
	if !res.IsError {
		t.Fatalf("check passed an inversion written after the load:\n%s", res.Content[0].Text)
	}
	want := "package internal/domain (layer domain) imports package internal/adapter (layer adapter)"
	if !strings.Contains(res.Content[0].Text, want) {
		t.Fatalf("report does not name the inversion %q:\n%s", want, res.Content[0].Text)
	}
}

func TestCheckWithoutAnOverlayIsAnError(t *testing.T) {
	res, rpcErr := Dispatch(loadFakeState(t), "check", nil)
	if rpcErr != nil {
		t.Fatalf("check: %v", rpcErr)
	}
	if !res.IsError || !strings.Contains(res.Content[0].Text, "no overlay") {
		t.Fatalf("check without wyrd.yaml = %+v, want a no-overlay error", res)
	}
}
