package check

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unwiredProject writes a two-component project whose ends name addresses that
// never match: the producer appends on `.agent.`, the observer listens on
// `.default.`. Everything else about the pair agrees, which is the case the
// counting rules cannot see.
func unwiredProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	write := func(dir, decl string) {
		archDir := filepath.Join(root, dir, ".arch")
		if err := os.MkdirAll(archDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(archDir, "events.yaml"), []byte(decl), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("ingress", `version: 2
component: ingress
owns: app.ingress
outputs:
  - kind: app.command.message.process
    pattern: 'app.entrypoint.agent.session.{session}.command.message.process'
`)
	write("entrypoint", `version: 2
component: entrypoint
owns: app.entrypoint
inputs:
  - kind: app.command.message.process
    pattern: 'app.entrypoint.default.session.{session}.command.message.process'
`)
	return root
}

func TestEventsReportsAnUnwiredKind(t *testing.T) {
	res, err := New(nil, nil).Events(context.Background(), EventsOptions{Root: unwiredProject(t)})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Declared {
		t.Fatal("Declared = false, want the project's declarations to be read")
	}

	var found bool
	for _, f := range res.Findings {
		if string(f.Kind) == "unwired-kind" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no unwired-kind finding among %+v", res.Findings)
	}
}

// The gate's whole purpose in CI: warnings pass by default, and --strict is
// how a project opts into failing on them.
//
// Scoped to unwired-kind because the native format calls this same mismatch a
// kind-pattern-conflict error as well — one kind, one address — and an error in
// the set would fail the default run for the wrong reason. An AsyncAPI project,
// where that rule does not apply, sees the warning alone.
func TestEventsWarningsPassUnlessStrict(t *testing.T) {
	root := unwiredProject(t)
	checker := New(nil, nil)
	only := []string{"unwired-kind"}

	var out bytes.Buffer
	if err := checker.RunEvents(context.Background(), EventsOptions{Root: root, Only: only}, &out, &out); err != nil {
		t.Fatalf("default run failed on warnings: %v", err)
	}
	if err := checker.RunEvents(context.Background(), EventsOptions{Root: root, Only: only, Strict: true}, &out, &out); err == nil {
		t.Fatal("--strict passed over a warning, want failure")
	}
	if err := checker.RunEvents(context.Background(), EventsOptions{Root: root, Only: only, Strict: true, Warn: true}, &out, &out); err != nil {
		t.Fatalf("--warn failed the gate: %v", err)
	}
}

// `ok` sits beside the exit code in a build log, so it has to agree with it.
func TestEventsJSONOKFollowsStrict(t *testing.T) {
	root := unwiredProject(t)

	for _, tc := range []struct {
		name   string
		strict bool
		wantOK bool
	}{
		{"warnings are not a failure by default", false, true},
		{"strict makes them one", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := New(nil, nil).RunEvents(context.Background(),
				EventsOptions{Root: root, Only: []string{"unwired-kind"}, Format: "json", Strict: tc.strict, Warn: true}, &out, &out)
			if err != nil {
				t.Fatal(err)
			}
			var payload struct {
				OK       bool `json:"ok"`
				Warnings int  `json:"warnings"`
			}
			if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
				t.Fatalf("output is not JSON: %v\n%s", err, out.String())
			}
			if payload.Warnings == 0 {
				t.Fatal("fixture produced no warnings")
			}
			if payload.OK != tc.wantOK {
				t.Errorf("ok = %v, want %v", payload.OK, tc.wantOK)
			}
		})
	}
}

// A misspelled filter must not read as a clean run.
func TestEventsRejectsAnUnknownFindingKind(t *testing.T) {
	_, err := New(nil, nil).Events(context.Background(),
		EventsOptions{Root: unwiredProject(t), Only: []string{"unwierd-kind"}})
	if err == nil {
		t.Fatal("unknown kind accepted, want an error")
	}
	if !strings.Contains(err.Error(), "unwired-kind") {
		t.Errorf("error does not list the known kinds: %v", err)
	}
}

func TestEventsOnlyNarrowsToTheNamedKinds(t *testing.T) {
	res, err := New(nil, nil).Events(context.Background(),
		EventsOptions{Root: unwiredProject(t), Only: []string{"unwired-kind"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) == 0 {
		t.Fatal("filter dropped everything")
	}
	for _, f := range res.Findings {
		if string(f.Kind) != "unwired-kind" {
			t.Errorf("finding of kind %q survived the filter", f.Kind)
		}
	}
}

// A project that declares no events is not a project that fails the gate.
func TestEventsOnAProjectWithNoDeclarations(t *testing.T) {
	var out bytes.Buffer
	err := New(nil, nil).RunEvents(context.Background(), EventsOptions{Root: t.TempDir()}, &out, &out)
	if err != nil {
		t.Fatalf("empty project failed the gate: %v", err)
	}
	if !strings.Contains(out.String(), "Nothing to check") {
		t.Errorf("report does not say why it checked nothing: %q", out.String())
	}
}
