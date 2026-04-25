package serve

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kgatilin/archai/internal/plugin"
)

// TestHost_CurrentModelExposesPackages confirms the serve-backed Host
// returns a unified Model with packages populated after Load().
func TestHost_CurrentModelExposesPackages(t *testing.T) {
	root := t.TempDir()
	writeGoModule(t, root, "example.com/x")
	writeGoFile(t, filepath.Join(root, "internal", "thing", "thing.go"), `package thing

type Widget struct{ Name string }

func New() *Widget { return &Widget{} }
`)

	state := NewState(root)
	if err := state.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	host := NewHost(state, slog.Default())

	model := host.CurrentModel()
	if model == nil {
		t.Fatalf("CurrentModel returned nil")
	}
	found := false
	for _, p := range model.Packages {
		if p != nil && p.Path == "internal/thing" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("internal/thing missing from model: paths = %s", modelPackagePaths(model))
	}
}

// TestHost_SubscribeReceivesEvents confirms that publishing a
// ModelEvent on the State's bus delivers to a subscriber set up
// through Host.Subscribe.
func TestHost_SubscribeReceivesEvents(t *testing.T) {
	root := t.TempDir()
	writeGoModule(t, root, "example.com/x")

	state := NewState(root)
	if err := state.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	host := NewHost(state, slog.Default())

	var (
		mu     sync.Mutex
		events []plugin.ModelEvent
	)
	cancel := host.Subscribe(func(ev plugin.ModelEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, ev)
	})
	defer cancel()

	state.PublishOverlayReload()
	state.PublishPackageReload([]string{"internal/foo"})
	state.PublishTargetSwitch("v2")

	mu.Lock()
	got := append([]plugin.ModelEvent(nil), events...)
	mu.Unlock()

	if len(got) != 3 {
		t.Fatalf("events len = %d, want 3", len(got))
	}
	if got[0].Kind != plugin.ModelEventKindOverlayReload {
		t.Errorf("got[0].Kind = %q", got[0].Kind)
	}
	if got[1].Kind != plugin.ModelEventKindPackageReload || len(got[1].Paths) != 1 || got[1].Paths[0] != "internal/foo" {
		t.Errorf("got[1] = %+v", got[1])
	}
	if got[2].Kind != plugin.ModelEventKindTargetSwitch || got[2].Target != "v2" {
		t.Errorf("got[2] = %+v", got[2])
	}

	// Ensure At is populated.
	if got[0].At.IsZero() {
		t.Errorf("event At not populated")
	}
	// Sanity: At is recent.
	if time.Since(got[0].At) > 5*time.Second {
		t.Errorf("event At too far in past: %v", got[0].At)
	}
}

// TestHost_UnsubscribeStopsDelivery confirms the Unsubscribe closure
// returned by Host.Subscribe detaches the handler.
func TestHost_UnsubscribeStopsDelivery(t *testing.T) {
	root := t.TempDir()
	writeGoModule(t, root, "example.com/x")

	state := NewState(root)
	if err := state.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	host := NewHost(state, slog.Default())

	var calls int
	cancel := host.Subscribe(func(_ plugin.ModelEvent) { calls++ })
	state.PublishOverlayReload()
	if calls != 1 {
		t.Fatalf("after first publish: calls = %d, want 1", calls)
	}
	cancel()
	state.PublishOverlayReload()
	if calls != 1 {
		t.Errorf("after unsubscribe: calls = %d, want 1", calls)
	}
}

// helpers

func writeGoModule(t *testing.T, root, mod string) {
	t.Helper()
	p := filepath.Join(root, "go.mod")
	body := "module " + mod + "\n\ngo 1.22\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
}

func writeGoFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func modelPackagePaths(m *plugin.Model) []string {
	out := make([]string, 0, len(m.Packages))
	for _, p := range m.Packages {
		if p == nil {
			continue
		}
		out = append(out, p.Path)
	}
	return out
}
