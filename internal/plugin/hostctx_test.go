package plugin

import (
	"context"
	"log/slog"
	"testing"
)

type namedHost struct {
	Host
	root string
}

func (h namedHost) RepoRoot() string     { return h.root }
func (h namedHost) Logger() *slog.Logger { return slog.Default() }

func TestHostFromContextRoundTrips(t *testing.T) {
	ctx := ContextWithHost(context.Background(), namedHost{root: "/w/feature"})
	got := HostFromContext(ctx)
	if got == nil {
		t.Fatal("want the scoped host, got nil")
	}
	if got.RepoRoot() != "/w/feature" {
		t.Errorf("root = %q, want /w/feature", got.RepoRoot())
	}
}

func TestContextWithoutAHostReturnsNil(t *testing.T) {
	if got := HostFromContext(context.Background()); got != nil {
		t.Errorf("host = %v, want nil so callers fall back to the bootstrap host", got)
	}
	if got := HostFromContext(nil); got != nil { //nolint:staticcheck // a nil ctx must not panic
		t.Errorf("host = %v, want nil", got)
	}
}

func TestContextWithNilHostIsTheIdentity(t *testing.T) {
	ctx := context.Background()
	if ContextWithHost(ctx, nil) != ctx {
		t.Error("a nil host should leave the context untouched")
	}
}
