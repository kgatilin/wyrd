package plugin

import (
	"context"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

// BootstrapResult collects every capability spec gathered while
// initializing the registered plugins. The caller (cmd/archai) wires
// these into the appropriate transports:
//   - CLI commands are added to the cobra root;
//   - MCPTools and HTTPHandlers are forwarded to their respective
//     adapters when those transports are active.
//
// M12 leaves dispatch wiring (prefixing under /api/plugins/<name>/
// for HTTP, namespacing for MCP, mounting under sidebar/dashboard
// slots for UI) to M13. The bootstrap simply ensures every Init
// runs and every accessor is invoked exactly once.
type BootstrapResult struct {
	// CLICommands are the cobra commands contributed by plugins,
	// each tagged with the manifest name of its source plugin so
	// the caller can decide on namespacing.
	CLICommands []NamedCLICommand

	// MCPTools are the tool descriptors contributed by plugins.
	MCPTools []NamedMCPTool

	// HTTPHandlers are the HTTP routes contributed by plugins.
	HTTPHandlers []NamedHTTPHandler

	// UIComponents are the UI panels/widgets contributed by plugins.
	UIComponents []NamedUIComponent
}

// NamedCLICommand pairs a CLI command spec with the plugin that
// produced it.
type NamedCLICommand struct {
	Plugin  string
	Command CLICommand
}

// NamedMCPTool pairs an MCP tool spec with the plugin that produced
// it.
type NamedMCPTool struct {
	Plugin string
	Tool   MCPTool
}

// NamedHTTPHandler pairs an HTTP handler spec with the plugin that
// produced it.
type NamedHTTPHandler struct {
	Plugin  string
	Handler HTTPHandler
}

// NamedUIComponent pairs a UI component spec with the plugin that
// produced it.
type NamedUIComponent struct {
	Plugin    string
	Component UIComponent
}

// ConfigPathFunc resolves the on-disk config path for a plugin by
// manifest name. Callers that don't need per-plugin configs can pass
// nil; in that case Init receives "" for configPath.
type ConfigPathFunc func(manifestName string) string

// Bootstrap initializes every registered plugin against host and
// collects their capability specs. Errors from individual plugin
// Inits are returned as a single aggregated error so a failed plugin
// does not prevent the rest from running — callers decide whether to
// abort.
func Bootstrap(ctx context.Context, host Host, configPath ConfigPathFunc) (BootstrapResult, error) {
	var result BootstrapResult
	var errs []error

	for _, p := range Registered() {
		mf := p.Manifest()
		var cfg string
		if configPath != nil {
			cfg = configPath(mf.Name)
		}
		if err := p.Init(ctx, host, cfg); err != nil {
			errs = append(errs, fmt.Errorf("plugin %q init: %w", mf.Name, err))
			continue
		}
		for _, c := range p.CLICommands() {
			if c.Cmd == nil {
				continue
			}
			result.CLICommands = append(result.CLICommands, NamedCLICommand{Plugin: mf.Name, Command: c})
		}
		for _, t := range p.MCPTools() {
			result.MCPTools = append(result.MCPTools, NamedMCPTool{Plugin: mf.Name, Tool: t})
		}
		for _, h := range p.HTTPHandlers() {
			result.HTTPHandlers = append(result.HTTPHandlers, NamedHTTPHandler{Plugin: mf.Name, Handler: h})
		}
		for _, u := range p.UIComponents() {
			result.UIComponents = append(result.UIComponents, NamedUIComponent{Plugin: mf.Name, Component: u})
		}
	}

	if len(errs) == 0 {
		return result, nil
	}
	return result, joinErrors(errs)
}

// AddCLICommandsToRoot adds every plugin-contributed cobra command to
// rootCmd. Used by tests; production code (cmd/archai) groups plugin
// commands under `archai plugin <name> ...` via BuildPluginCommand.
func AddCLICommandsToRoot(rootCmd *cobra.Command, cmds []NamedCLICommand) {
	for _, c := range cmds {
		if c.Command.Cmd == nil {
			continue
		}
		rootCmd.AddCommand(c.Command.Cmd)
	}
}

// MountHTTPHandlers registers every plugin-contributed HTTP route on
// mux at its literal Path (no prefixing). Kept for tests and for
// callers that want to mount routes outside the M13 /api/plugins/<name>/
// convention. Production callers use MountPluginAPIHandlers.
func MountHTTPHandlers(mux *http.ServeMux, handlers []NamedHTTPHandler) {
	for _, h := range handlers {
		if h.Handler.Handler == nil || h.Handler.Path == "" {
			continue
		}
		mux.Handle(h.Handler.Path, methodFiltered(h.Handler))
	}
}

// PluginAPIPrefix is the URL prefix under which every plugin's HTTP
// handlers are mounted by MountPluginAPIHandlers. Each plugin's
// HTTPHandler.Path is appended verbatim, so plugin-declared "/scores"
// becomes "/api/plugins/<name>/scores".
const PluginAPIPrefix = "/api/plugins/"

// PluginAssetPrefix is the URL prefix under which plugin static assets
// are served by MountPluginAssetHandlers. Each plugin's Assets fs.FS is
// served at "/plugins/<name>/assets/...".
const PluginAssetPrefix = "/plugins/"

// MountPluginAPIHandlers wires every plugin HTTPHandler under
// /api/plugins/<plugin-name><Path> on mux. Methods are enforced; an
// empty Path becomes the plugin's namespace root
// (/api/plugins/<plugin-name>).
func MountPluginAPIHandlers(mux *http.ServeMux, handlers []NamedHTTPHandler) {
	for _, h := range handlers {
		if h.Handler.Handler == nil {
			continue
		}
		full := PluginAPIPrefix + h.Plugin + h.Handler.Path
		// Strip the API prefix so the plugin sees relative paths.
		stripped := http.StripPrefix(PluginAPIPrefix+h.Plugin, methodFiltered(h.Handler))
		mux.Handle(full, stripped)
	}
}

// MountPluginAssetHandlers wires every plugin's UI Assets fs.FS under
// /plugins/<plugin-name>/assets/ on mux. Plugins that declare no
// UIComponents (or whose components carry a nil Assets) are skipped.
// A plugin contributing multiple UIComponents with the same Assets is
// mounted once.
func MountPluginAssetHandlers(mux *http.ServeMux, components []NamedUIComponent) {
	mounted := make(map[string]struct{})
	for _, c := range components {
		if c.Component.Assets == nil {
			continue
		}
		if _, ok := mounted[c.Plugin]; ok {
			continue
		}
		mounted[c.Plugin] = struct{}{}
		prefix := PluginAssetPrefix + c.Plugin + "/assets/"
		fs := http.FileServer(http.FS(c.Component.Assets))
		mux.Handle(prefix, http.StripPrefix(prefix, fs))
	}
}

// methodFiltered wraps h with a Method allow-list when Methods is
// non-empty. Empty Methods means "any verb".
func methodFiltered(h HTTPHandler) http.Handler {
	if len(h.Methods) == 0 {
		return h.Handler
	}
	allowed := make(map[string]struct{}, len(h.Methods))
	for _, m := range h.Methods {
		allowed[m] = struct{}{}
	}
	inner := h.Handler
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allowed[r.Method]; !ok {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// PrefixedMCPName returns the canonical MCP tool name for a plugin
// tool: "plugin.<plugin-name>.<tool-name>". Centralised so callers
// agree on the prefix and tests can verify it.
func PrefixedMCPName(pluginName, toolName string) string {
	return "plugin." + pluginName + "." + toolName
}

// BuildPluginCommand returns the `archai plugin ...` cobra subcommand
// tree: a parent "plugin" command with one child per registered plugin
// (`archai plugin <name> ...`) plus a built-in `archai plugin list`
// subcommand. The list subcommand prints, for each plugin, its
// manifest line and the names of the capabilities it contributes.
func BuildPluginCommand(res BootstrapResult) *cobra.Command {
	root := &cobra.Command{
		Use:   "plugin",
		Short: "Inspect and run archai plugins",
		Long: `Group of commands for archai plugins.

Each registered plugin appears as a subcommand: any CLI command that
plugin contributes is mounted under "archai plugin <name> ...". The
"plugin list" command prints every loaded plugin and the capabilities
(CLI / MCP / HTTP / UI) it exposes.`,
	}

	// Group every plugin's CLI commands under "archai plugin <name>".
	groups := make(map[string]*cobra.Command)
	for _, c := range res.CLICommands {
		if c.Command.Cmd == nil {
			continue
		}
		parent, ok := groups[c.Plugin]
		if !ok {
			parent = &cobra.Command{
				Use:   c.Plugin,
				Short: "Commands contributed by the " + c.Plugin + " plugin",
			}
			groups[c.Plugin] = parent
			root.AddCommand(parent)
		}
		parent.AddCommand(c.Command.Cmd)
	}

	root.AddCommand(buildPluginListCommand(res))
	return root
}

// buildPluginListCommand renders one row per plugin with the
// capabilities it contributes. The output is human-readable and stable
// across runs (we sort plugin names).
func buildPluginListCommand(res BootstrapResult) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List loaded plugins and their capabilities",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			names := pluginNames(res)
			if len(names) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No plugins loaded.")
				return nil
			}
			out := cmd.OutOrStdout()
			for _, name := range names {
				fmt.Fprintf(out, "%s\n", name)
				for _, line := range capabilitiesFor(res, name) {
					fmt.Fprintf(out, "  %s\n", line)
				}
			}
			return nil
		},
	}
	return cmd
}

// pluginNames returns the sorted set of plugin names appearing in res.
func pluginNames(res BootstrapResult) []string {
	seen := make(map[string]struct{})
	add := func(n string) {
		if n == "" {
			return
		}
		seen[n] = struct{}{}
	}
	for _, c := range res.CLICommands {
		add(c.Plugin)
	}
	for _, t := range res.MCPTools {
		add(t.Plugin)
	}
	for _, h := range res.HTTPHandlers {
		add(h.Plugin)
	}
	for _, u := range res.UIComponents {
		add(u.Plugin)
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sortStrings(out)
	return out
}

// capabilitiesFor returns one descriptive line per capability a plugin
// contributes. Lines are pre-formatted (CLI / MCP / HTTP / UI) so the
// list command stays tidy.
func capabilitiesFor(res BootstrapResult, name string) []string {
	var lines []string
	for _, c := range res.CLICommands {
		if c.Plugin != name || c.Command.Cmd == nil {
			continue
		}
		lines = append(lines, "CLI : archai plugin "+name+" "+c.Command.Cmd.Use)
	}
	for _, t := range res.MCPTools {
		if t.Plugin != name {
			continue
		}
		lines = append(lines, "MCP : "+PrefixedMCPName(name, t.Tool.Name))
	}
	for _, h := range res.HTTPHandlers {
		if h.Plugin != name {
			continue
		}
		methods := "ANY"
		if len(h.Handler.Methods) > 0 {
			methods = joinMethods(h.Handler.Methods)
		}
		lines = append(lines, "HTTP: "+methods+" "+PluginAPIPrefix+name+h.Handler.Path)
	}
	for _, u := range res.UIComponents {
		if u.Plugin != name {
			continue
		}
		for _, slot := range u.Component.EmbedAt {
			lines = append(lines, fmt.Sprintf("UI  : <%s> on %s/%s", u.Component.Element, slot.View, slot.Slot))
		}
	}
	return lines
}

func joinMethods(m []string) string {
	if len(m) == 0 {
		return ""
	}
	out := m[0]
	for _, s := range m[1:] {
		out += "," + s
	}
	return out
}

func sortStrings(s []string) {
	// Tiny insertion sort: n is always small (one entry per plugin).
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// joinErrors returns errors.Join-style aggregation without depending
// on stdlib errors.Join (available since Go 1.20; archai already
// requires newer Go but keeping this local makes the package
// self-contained and the behavior obvious).
func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	return aggErr{errs: errs}
}

type aggErr struct{ errs []error }

func (a aggErr) Error() string {
	msg := ""
	for i, e := range a.errs {
		if i > 0 {
			msg += "; "
		}
		msg += e.Error()
	}
	return msg
}
