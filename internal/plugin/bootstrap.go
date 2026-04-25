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
// rootCmd. M12 keeps it flat (no per-plugin subcommand grouping) so
// existing tests that walk root.Commands() keep their guarantees.
// M13 (#66) replaces this with a `plugins` subgroup.
func AddCLICommandsToRoot(rootCmd *cobra.Command, cmds []NamedCLICommand) {
	for _, c := range cmds {
		if c.Command.Cmd == nil {
			continue
		}
		rootCmd.AddCommand(c.Command.Cmd)
	}
}

// MountHTTPHandlers registers every plugin-contributed HTTP route on
// the given mux. M12 mounts at the route's literal Path; M13 will
// prefix /api/plugins/<plugin>/. The Methods field is honored by
// wrapping the handler with a method check.
func MountHTTPHandlers(mux *http.ServeMux, handlers []NamedHTTPHandler) {
	for _, h := range handlers {
		if h.Handler.Handler == nil || h.Handler.Path == "" {
			continue
		}
		hh := h.Handler
		if len(hh.Methods) == 0 {
			mux.Handle(hh.Path, hh.Handler)
			continue
		}
		methods := make(map[string]struct{}, len(hh.Methods))
		for _, m := range hh.Methods {
			methods[m] = struct{}{}
		}
		mux.Handle(hh.Path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := methods[r.Method]; !ok {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			hh.Handler.ServeHTTP(w, r)
		}))
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
