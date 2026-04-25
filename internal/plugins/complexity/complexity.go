// Package complexity is the built-in demo plugin shipped with archai
// to prove the M12 plugin contract end-to-end. It computes a tiny
// per-package complexity score (interfaces + structs + functions +
// methods) and exposes it through every capability accessor: a CLI
// command, an MCP tool, an HTTP route, and a UI component descriptor.
//
// Heuristic note: this is intentionally a one-screen heuristic, not a
// real cyclomatic complexity. Future work (a separate plugin or M13
// follow-up) can swap the scorer without touching the contract.
package complexity

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"sort"

	"github.com/spf13/cobra"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/plugin"
)

//go:embed assets/*.js
var assetsFS embed.FS

// pluginAssets is the asset sub-FS rooted at the plugin's "assets/"
// directory so the host serves /plugins/complexity/assets/<file>.
var pluginAssets fs.FS = mustSub(assetsFS, "assets")

func mustSub(f fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		// Compile-time embed failure is the only realistic cause; panic
		// is acceptable in init-style code.
		panic(fmt.Sprintf("complexity: assets sub-fs %q: %v", dir, err))
	}
	return sub
}

// Plugin is the in-memory state of the complexity plugin. The Host
// reference captured during Init is used by every capability
// handler at request time.
type Plugin struct {
	host plugin.Host
}

// Manifest implements plugin.Plugin.
func (p *Plugin) Manifest() plugin.Manifest {
	return plugin.Manifest{
		Name:        "complexity",
		Version:     "0.1.0",
		Description: "Per-package complexity heuristic (interfaces + structs + functions + methods).",
	}
}

// Init implements plugin.Plugin.
func (p *Plugin) Init(_ context.Context, host plugin.Host, _ string) error {
	if host == nil {
		return fmt.Errorf("complexity: host is nil")
	}
	p.host = host
	return nil
}

// CLICommands implements plugin.Plugin. The single contributed
// command prints a sorted complexity table to stdout.
func (p *Plugin) CLICommands() []plugin.CLICommand {
	cmd := &cobra.Command{
		Use:   "complexity",
		Short: "Show per-package complexity scores",
		Long: `Print a per-package complexity score for the current model.

The score is a simple heuristic: interfaces + structs + functions +
methods (sum across all symbols in the package). Useful as a
proof-of-concept for the plugin contract; not a substitute for real
cyclomatic complexity tooling.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scores := p.scores()
			out := cmd.OutOrStdout()
			if len(scores) == 0 {
				fmt.Fprintln(out, "No packages loaded.")
				return nil
			}
			fmt.Fprintf(out, "%-50s  %s\n", "PACKAGE", "SCORE")
			for _, s := range scores {
				fmt.Fprintf(out, "%-50s  %d\n", s.Package, s.Score)
			}
			return nil
		},
	}
	return []plugin.CLICommand{{Cmd: cmd}}
}

// MCPTools implements plugin.Plugin. The tool returns the same
// scores as the CLI command, in JSON-friendly form.
func (p *Plugin) MCPTools() []plugin.MCPTool {
	return []plugin.MCPTool{{
		Name:        "complexity.scores",
		Description: "Return per-package complexity scores for the current model.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Handler: func(ctx context.Context, _ map[string]any) (any, error) {
			return p.scores(), nil
		},
	}}
}

// HTTPHandlers implements plugin.Plugin. The route serves the per-package
// scores as JSON. M13 mounts every plugin route under
// /api/plugins/<plugin-name><Path>; the path declared here is therefore
// relative to that prefix. The browser custom element fetches
// /api/plugins/complexity/scores.
func (p *Plugin) HTTPHandlers() []plugin.HTTPHandler {
	return []plugin.HTTPHandler{{
		Path:    "/scores",
		Methods: []string{http.MethodGet},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(p.scores())
		}),
	}}
}

// UIComponents implements plugin.Plugin. M13 mounts the embedded JS
// bundle at /plugins/complexity/assets/ and renders
// <plugin-complexity-heatmap data-model-url="/api/plugins/complexity/scores">
// on the dashboard (main slot) and on the package detail page (extra
// tab labelled "Complexity").
func (p *Plugin) UIComponents() []plugin.UIComponent {
	return []plugin.UIComponent{{
		Element: "plugin-complexity-heatmap",
		Assets:  pluginAssets,
		Entry:   "heatmap.js",
		EmbedAt: []plugin.EmbedSlot{
			{View: plugin.ViewDashboard, Slot: plugin.SlotMain, Label: "Complexity"},
			{View: plugin.ViewPackageDetail, Slot: plugin.SlotExtraTab, Label: "Complexity"},
		},
		// HTTPHandlers above mounts /scores; the host's per-plugin
		// prefix /api/plugins/complexity makes the full URL
		// /api/plugins/complexity/scores. Set explicitly so the
		// dashboard widget's data-model-url matches the registered
		// route (the default fallback would be /api/plugins/complexity
		// → 404). See issue #74.
		ModelURL: plugin.PluginAPIPrefix + "complexity/scores",
	}}
}

// PackageScore is the per-package score returned by the CLI/MCP/HTTP
// handlers. Exported because the MCP handler returns it directly to
// the client (encoded as JSON by the MCP transport).
type PackageScore struct {
	Package string `json:"package"`
	Layer   string `json:"layer,omitempty"`
	Score   int    `json:"score"`
}

func (p *Plugin) scores() []PackageScore {
	if p == nil || p.host == nil {
		return nil
	}
	model := p.host.CurrentModel()
	if model == nil {
		return nil
	}
	out := make([]PackageScore, 0, len(model.Packages))
	for _, pkg := range model.Packages {
		if pkg == nil {
			continue
		}
		out = append(out, PackageScore{
			Package: pkg.Path,
			Layer:   pkg.Layer,
			Score:   complexityScore(pkg),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Package < out[j].Package
	})
	return out
}

// complexityScore returns a tiny per-package heuristic. Sum of:
//   - interface count
//   - struct count
//   - function count
//   - method count across all structs
func complexityScore(pkg *domain.PackageModel) int {
	if pkg == nil {
		return 0
	}
	score := len(pkg.Interfaces) + len(pkg.Structs) + len(pkg.Functions)
	for _, s := range pkg.Structs {
		score += len(s.Methods)
	}
	for _, i := range pkg.Interfaces {
		score += len(i.Methods)
	}
	return score
}

// init registers the plugin with the package-global registry. The
// plugin is then picked up by plugin.Bootstrap during archai
// startup. To compile the plugin into a binary, import this package
// for side effects:
//
//	import _ "github.com/kgatilin/archai/internal/plugins/complexity"
func init() {
	plugin.RegisterPlugin(&Plugin{})
}
