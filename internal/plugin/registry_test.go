package plugin

import (
	"context"
	"testing"
)

// fakePlugin is a minimal Plugin used by registry/bootstrap tests.
type fakePlugin struct {
	name      string
	initCalls int
	initErr   error
	cli       []CLICommand
	mcp       []MCPTool
	http      []HTTPHandler
	ui        []UIComponent
	host      Host
}

func (p *fakePlugin) Manifest() Manifest {
	return Manifest{Name: p.name, Version: "0.0.1", Description: "fake"}
}
func (p *fakePlugin) Init(_ context.Context, h Host, _ string) error {
	p.host = h
	p.initCalls++
	return p.initErr
}
func (p *fakePlugin) CLICommands() []CLICommand   { return p.cli }
func (p *fakePlugin) MCPTools() []MCPTool         { return p.mcp }
func (p *fakePlugin) HTTPHandlers() []HTTPHandler { return p.http }
func (p *fakePlugin) UIComponents() []UIComponent { return p.ui }

func TestRegisterPlugin_AddsToRegistry(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	p := &fakePlugin{name: "alpha"}
	RegisterPlugin(p)

	got := Registered()
	if len(got) != 1 {
		t.Fatalf("Registered() len = %d, want 1", len(got))
	}
	if got[0].Manifest().Name != "alpha" {
		t.Errorf("Registered()[0].Name = %q, want %q", got[0].Manifest().Name, "alpha")
	}
}

func TestRegisterPlugin_NilPanics(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("RegisterPlugin(nil) did not panic")
		}
	}()
	RegisterPlugin(nil)
}

func TestRegisterPlugin_DuplicateNamePanics(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	RegisterPlugin(&fakePlugin{name: "dup"})
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("RegisterPlugin with duplicate name did not panic")
		}
	}()
	RegisterPlugin(&fakePlugin{name: "dup"})
}

func TestRegistered_SortedByName(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	RegisterPlugin(&fakePlugin{name: "zeta"})
	RegisterPlugin(&fakePlugin{name: "alpha"})
	RegisterPlugin(&fakePlugin{name: "mu"})

	got := Registered()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	want := []string{"alpha", "mu", "zeta"}
	for i, p := range got {
		if p.Manifest().Name != want[i] {
			t.Errorf("Registered()[%d].Name = %q, want %q", i, p.Manifest().Name, want[i])
		}
	}
}
