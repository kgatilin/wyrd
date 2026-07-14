package overlay

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadComposed_PolicyFragmentComposition(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "archai.yaml"), `module: github.com/example/app

layers:
  domain:
    - internal/domain/...
  serve:
    - internal/serve/...

layer_rules:
  serve:
    - domain

policy:
  deny_by_default: true
  components:
    - "internal/*"
  allow:
    - "@serve -> @domain"
  forbid:
    - "@domain -> @serve"
  reachability:
    - "@domain !~> internal/http"
`)
	writeFile(t, filepath.Join(root, "internal/serve/.arch/overlay.yaml"), `policy:
  components:
    - "internal/serve/*"
  allow:
    - "@serve -> internal/util"
  forbid:
    - "internal/serve -> internal/debug"
  reachability:
    - "@serve !~> internal/hack"
`)

	cfg, err := LoadComposed(filepath.Join(root, "archai.yaml"))
	if err != nil {
		t.Fatalf("LoadComposed: %v", err)
	}

	// deny_by_default should be preserved from root
	if cfg.Policy.DenyByDefault == nil || !*cfg.Policy.DenyByDefault {
		t.Errorf("DenyByDefault should be true from root, got %v", cfg.Policy.DenyByDefault)
	}

	// Components: root + fragment, no duplicates
	wantComponents := []string{"internal/*", "internal/serve/*"}
	if !equalSlices(cfg.Policy.Components, wantComponents) {
		t.Errorf("Components = %v, want %v", cfg.Policy.Components, wantComponents)
	}

	// Allow: root + fragment
	wantAllow := []string{"@serve -> @domain", "@serve -> internal/util"}
	if !equalSlices(cfg.Policy.Allow, wantAllow) {
		t.Errorf("Allow = %v, want %v", cfg.Policy.Allow, wantAllow)
	}

	// Forbid: root + fragment
	wantForbid := []string{"@domain -> @serve", "internal/serve -> internal/debug"}
	if !equalSlices(cfg.Policy.Forbid, wantForbid) {
		t.Errorf("Forbid = %v, want %v", cfg.Policy.Forbid, wantForbid)
	}

	// Reachability: root + fragment
	wantReachability := []string{"@domain !~> internal/http", "@serve !~> internal/hack"}
	if !equalSlices(cfg.Policy.Reachability, wantReachability) {
		t.Errorf("Reachability = %v, want %v", cfg.Policy.Reachability, wantReachability)
	}
}

func TestLoadComposed_PolicyDedup(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "archai.yaml"), `module: github.com/example/app

layers:
  domain:
    - internal/domain/...

layer_rules:
  domain: []

policy:
  components:
    - "internal/*"
  allow:
    - "@domain -> internal/util"
  forbid:
    - "@domain -> internal/debug"
  reachability:
    - "@domain !~> internal/hack"
`)
	// Fragment has duplicates of root entries
	writeFile(t, filepath.Join(root, "internal/domain/.arch/overlay.yaml"), `policy:
  components:
    - "internal/*"
    - "internal/domain/*"
  allow:
    - "@domain -> internal/util"
    - "@domain -> internal/helper"
  forbid:
    - "@domain -> internal/debug"
    - "@domain -> internal/unsafe"
  reachability:
    - "@domain !~> internal/hack"
    - "@domain !~> internal/legacy"
`)

	cfg, err := LoadComposed(filepath.Join(root, "archai.yaml"))
	if err != nil {
		t.Fatalf("LoadComposed: %v", err)
	}

	// Each duplicate should appear once only, root-first order
	wantComponents := []string{"internal/*", "internal/domain/*"}
	if !equalSlices(cfg.Policy.Components, wantComponents) {
		t.Errorf("Components = %v, want %v", cfg.Policy.Components, wantComponents)
	}

	wantAllow := []string{"@domain -> internal/util", "@domain -> internal/helper"}
	if !equalSlices(cfg.Policy.Allow, wantAllow) {
		t.Errorf("Allow = %v, want %v", cfg.Policy.Allow, wantAllow)
	}

	wantForbid := []string{"@domain -> internal/debug", "@domain -> internal/unsafe"}
	if !equalSlices(cfg.Policy.Forbid, wantForbid) {
		t.Errorf("Forbid = %v, want %v", cfg.Policy.Forbid, wantForbid)
	}

	wantReachability := []string{"@domain !~> internal/hack", "@domain !~> internal/legacy"}
	if !equalSlices(cfg.Policy.Reachability, wantReachability) {
		t.Errorf("Reachability = %v, want %v", cfg.Policy.Reachability, wantReachability)
	}
}

func TestLoadComposed_PolicyOrdering(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "archai.yaml"), `module: github.com/example/app

layers:
  domain:
    - internal/domain/...

layer_rules:
  domain: []

policy:
  allow:
    - "root-rule-1"
    - "root-rule-2"
`)
	// Two fragments at different paths; they should compose in sorted path order
	writeFile(t, filepath.Join(root, "internal/a/.arch/overlay.yaml"), `policy:
  allow:
    - "a-rule"
`)
	writeFile(t, filepath.Join(root, "internal/z/.arch/overlay.yaml"), `policy:
  allow:
    - "z-rule"
`)

	cfg, err := LoadComposed(filepath.Join(root, "archai.yaml"))
	if err != nil {
		t.Fatalf("LoadComposed: %v", err)
	}

	// Root first, then fragments in sorted path order (internal/a before internal/z)
	wantAllow := []string{"root-rule-1", "root-rule-2", "a-rule", "z-rule"}
	if !equalSlices(cfg.Policy.Allow, wantAllow) {
		t.Errorf("Allow = %v, want %v", cfg.Policy.Allow, wantAllow)
	}
}

func TestLoadComposed_PolicyDenyByDefaultInFragmentError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "archai.yaml"), `module: github.com/example/app

layers:
  domain:
    - internal/domain/...

layer_rules:
  domain: []

policy:
  deny_by_default: true
`)
	writeFile(t, filepath.Join(root, "internal/domain/.arch/overlay.yaml"), `policy:
  deny_by_default: false
`)

	_, err := LoadComposed(filepath.Join(root, "archai.yaml"))
	if err == nil {
		t.Fatal("expected error for deny_by_default in fragment, got nil")
	}
	if !strings.Contains(err.Error(), "internal/domain/.arch/overlay.yaml") {
		t.Errorf("error should mention fragment path, got: %v", err)
	}
	if !strings.Contains(err.Error(), "deny_by_default") {
		t.Errorf("error should mention deny_by_default, got: %v", err)
	}
}

func TestLoadComposed_PolicyRootOnlyUnchanged(t *testing.T) {
	root := t.TempDir()
	denyTrue := true
	writeFile(t, filepath.Join(root, "archai.yaml"), `module: github.com/example/app

layers:
  domain:
    - internal/domain/...

layer_rules:
  domain: []

policy:
  deny_by_default: true
  components:
    - "internal/*"
  allow:
    - "@domain -> internal/util"
  forbid:
    - "@domain -> internal/debug"
  reachability:
    - "@domain !~> internal/hack"
`)

	cfg, err := LoadComposed(filepath.Join(root, "archai.yaml"))
	if err != nil {
		t.Fatalf("LoadComposed: %v", err)
	}

	// No fragments, policy should be unchanged
	if cfg.Policy.DenyByDefault == nil || *cfg.Policy.DenyByDefault != denyTrue {
		t.Errorf("DenyByDefault = %v, want true", cfg.Policy.DenyByDefault)
	}
	if !equalSlices(cfg.Policy.Components, []string{"internal/*"}) {
		t.Errorf("Components = %v, want [internal/*]", cfg.Policy.Components)
	}
	if !equalSlices(cfg.Policy.Allow, []string{"@domain -> internal/util"}) {
		t.Errorf("Allow = %v, want [@domain -> internal/util]", cfg.Policy.Allow)
	}
	if !equalSlices(cfg.Policy.Forbid, []string{"@domain -> internal/debug"}) {
		t.Errorf("Forbid = %v, want [@domain -> internal/debug]", cfg.Policy.Forbid)
	}
	if !equalSlices(cfg.Policy.Reachability, []string{"@domain !~> internal/hack"}) {
		t.Errorf("Reachability = %v, want [@domain !~> internal/hack]", cfg.Policy.Reachability)
	}
}

func TestLoadComposed_PolicyFragmentOnlyNoRoot(t *testing.T) {
	// Root has no policy block; fragment adds one
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "archai.yaml"), `module: github.com/example/app

layers:
  domain:
    - internal/domain/...

layer_rules:
  domain: []
`)
	writeFile(t, filepath.Join(root, "internal/domain/.arch/overlay.yaml"), `policy:
  allow:
    - "@domain -> internal/util"
  forbid:
    - "@domain -> internal/debug"
`)

	cfg, err := LoadComposed(filepath.Join(root, "archai.yaml"))
	if err != nil {
		t.Fatalf("LoadComposed: %v", err)
	}

	// Fragment rules should compose into the empty root policy
	if !equalSlices(cfg.Policy.Allow, []string{"@domain -> internal/util"}) {
		t.Errorf("Allow = %v, want [@domain -> internal/util]", cfg.Policy.Allow)
	}
	if !equalSlices(cfg.Policy.Forbid, []string{"@domain -> internal/debug"}) {
		t.Errorf("Forbid = %v, want [@domain -> internal/debug]", cfg.Policy.Forbid)
	}
	// deny_by_default should remain nil (not set by anyone)
	if cfg.Policy.DenyByDefault != nil {
		t.Errorf("DenyByDefault = %v, want nil", cfg.Policy.DenyByDefault)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
