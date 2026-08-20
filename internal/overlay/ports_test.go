package overlay

import (
	"strings"
	"testing"
)

func TestParseExternalPortRecognisesTheThreeSelectorForms(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want ExternalPort
	}{
		{
			name: "package glob",
			raw:  "internal/plugins/...",
			want: ExternalPort{Raw: "internal/plugins/...", Glob: "internal/plugins/..."},
		},
		{
			name: "single package",
			raw:  "internal/plugins",
			want: ExternalPort{Raw: "internal/plugins", Glob: "internal/plugins"},
		},
		{
			name: "wildcard segment",
			raw:  "internal/plugins/*",
			want: ExternalPort{Raw: "internal/plugins/*", Glob: "internal/plugins/*"},
		},
		{
			name: "one symbol",
			raw:  "internal/adapter/mcp.Dispatch",
			want: ExternalPort{Raw: "internal/adapter/mcp.Dispatch", Package: "internal/adapter/mcp", Symbol: "Dispatch"},
		},
		{
			name: "one member",
			raw:  "internal/serve.State.Snapshot",
			want: ExternalPort{Raw: "internal/serve.State.Snapshot", Package: "internal/serve", Symbol: "State", Member: "Snapshot"},
		},
		{
			name: "root package symbol",
			raw:  "main.Run",
			want: ExternalPort{Raw: "main.Run", Package: "main", Symbol: "Run"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseExternalPort(tc.raw)
			if err != nil {
				t.Fatalf("ParseExternalPort(%q) error = %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("ParseExternalPort(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseExternalPortRejectsMalformedSelectors(t *testing.T) {
	for _, raw := range []string{
		"",
		" internal/x",
		"internal/x ",
		"internal/a b.C",
		"/internal/x",
		"internal/.../x",
		"internal/serve.State.Snapshot.Extra",
		"internal/serve.",
		"internal/serve..Snapshot",
	} {
		if _, err := ParseExternalPort(raw); err == nil {
			t.Errorf("ParseExternalPort(%q) accepted a malformed selector", raw)
		}
	}
}

func TestExternalPortMatches(t *testing.T) {
	glob, err := ParseExternalPort("internal/plugins/...")
	if err != nil {
		t.Fatal(err)
	}
	if !glob.Matches("internal/plugins/events", "Plugin", "") {
		t.Error("a glob port should cover every export of a matching package")
	}
	if !glob.Matches("internal/plugins/events", "Plugin", "Manifest") {
		t.Error("a glob port should cover members too")
	}
	if glob.Matches("internal/plugin", "Plugin", "") {
		t.Error("internal/plugin is not under internal/plugins")
	}

	symbol, err := ParseExternalPort("internal/adapter/mcp.Dispatch")
	if err != nil {
		t.Fatal(err)
	}
	if !symbol.Matches("internal/adapter/mcp", "Dispatch", "") {
		t.Error("a symbol port should cover the symbol it names")
	}
	if symbol.Matches("internal/adapter/mcp", "Dispatch", "Run") {
		t.Error("a symbol port must not cover the symbol's members")
	}
	if symbol.Matches("internal/adapter/mcp", "Other", "") {
		t.Error("a symbol port must not cover another symbol")
	}

	member, err := ParseExternalPort("internal/serve.State.Snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if !member.Matches("internal/serve", "State", "Snapshot") {
		t.Error("a member port should cover the member it names")
	}
	if member.Matches("internal/serve", "State", "Reload") {
		t.Error("a member port must not cover the type's other members")
	}
	if member.Matches("internal/serve", "State", "") {
		t.Error("a member port must not cover the type itself")
	}
}

func TestValidateReportsMalformedPortSelectors(t *testing.T) {
	cfg := &Config{
		Module: "example.com/m",
		Layers: map[string][]string{"domain": {"internal/domain/..."}},
		Ports:  Ports{External: []string{"internal/plugins/...", "internal/.../x"}},
	}
	err := Validate(cfg, "")
	if err == nil {
		t.Fatal("Validate accepted a malformed ports.external selector")
	}
	if want := "ports.external[1]"; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to point at %s", err.Error(), want)
	}
}
