// Package plugin defines archai's in-process plugin contract: the
// Plugin interface that capability providers implement, the Host
// interface that exposes archai's read-only model + utilities, and the
// supporting capability descriptors (CLI / MCP / HTTP / UI).
//
// v1 scope (M12, issue #65):
//   - In-process Go plugins only. Plugins compile into the archai
//     binary and register themselves from init() via RegisterPlugin.
//   - Plugins are read-only consumers of the Model. They cannot mutate
//     it; mutation stays inside the daemon.
//   - Capability dispatch (prefixing, /api/plugins/<name>/, UI mount
//     points) is wired in M13 (#66). For M12 we collect the spec
//     structs and hand them to the bootstrap so M13 can mount them
//     without a contract change.
//
// Design notes (M12):
//
// Model lives in this package even though it composes domain.PackageModel
// and overlay.Config. The alternative — putting it in internal/domain —
// would force domain to import overlay, breaking the rule that domain
// has no dependencies. Plugin already depends on both, so it's the
// natural home for the unified view.
package plugin

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/spf13/cobra"
)

// Plugin is the contract every archai plugin implements.
//
// The Host pulls capability slices from a plugin and decides where to
// mount them; the plugin never calls back into the Host to register
// itself. This keeps registration declarative and easy to test.
type Plugin interface {
	// Manifest identifies the plugin (name, version, description). The
	// name acts as the namespace prefix for CLI commands, MCP tools,
	// HTTP routes, and UI mount points (M13).
	Manifest() Manifest

	// Init runs once during archai bootstrap, before any capability
	// accessors are queried. The plugin receives the Host (read-only
	// view of the model + utilities) and a path to its config file
	// resolved by archai. configPath may be empty when no config is
	// present; plugins that need configuration must fail here.
	Init(ctx context.Context, host Host, configPath string) error

	// CLICommands returns the cobra commands this plugin contributes.
	// Returning nil/empty is fine.
	CLICommands() []CLICommand

	// MCPTools returns the MCP tool descriptors this plugin exposes.
	MCPTools() []MCPTool

	// HTTPHandlers returns the HTTP routes this plugin serves.
	HTTPHandlers() []HTTPHandler

	// UIComponents returns the UI panels/widgets this plugin contributes.
	UIComponents() []UIComponent
}

// Manifest is a small descriptor returned by Plugin.Manifest().
type Manifest struct {
	// Name is the plugin's stable identifier. Used as the namespace
	// for prefixing CLI commands, MCP tools, HTTP routes, and UI
	// mount points (M13). Must be a valid identifier (no slashes).
	Name string

	// Version is a free-form version string. Plugins set whatever
	// makes sense (semver, build tag, "0.1-dev").
	Version string

	// Description is a short human-readable summary shown in `archai
	// plugins list` (M13) and in plugin discovery responses.
	Description string
}

// Host is the contract a plugin sees: a read-only view of the live
// model plus a small toolbox (Diff, Validate, Subscribe). Plugins must
// not assume any particular concrete implementation; the daemon and
// the one-shot CLI provide different Hosts that satisfy this same
// interface.
type Host interface {
	// CurrentModel returns the unified Model (code + overlay merged).
	// The returned pointer is a snapshot; plugins should treat it as
	// read-only and re-call CurrentModel after a ModelEvent rather
	// than retaining the pointer indefinitely.
	CurrentModel() *Model

	// Targets lists the locked targets discovered under
	// .arch/targets/. The list is sorted by id.
	Targets() []TargetMeta

	// Target loads a snapshot for the given target id.
	Target(id string) (*TargetSnapshot, error)

	// ActiveTarget returns the snapshot of the CURRENT target, or nil
	// when no target is active.
	ActiveTarget() *TargetSnapshot

	// Diff computes a structured diff between two snapshots. fromID
	// or toID may be empty to mean "current code model" (e.g. Diff("",
	// "v1") returns "current vs target v1").
	Diff(fromID, toID string) (*Diff, error)

	// Validate runs the same checks `archai validate` performs against
	// the named target (or CURRENT when modelID == "") and returns the
	// resulting report.
	Validate(modelID string) (*ValidationReport, error)

	// Subscribe registers handler to receive ModelEvent broadcasts.
	// Events are delivered synchronously from the dispatch goroutine;
	// the handler must be cheap and non-blocking. Returns an
	// Unsubscribe func that detaches the handler. Calling Unsubscribe
	// twice is safe.
	Subscribe(handler func(ModelEvent)) Unsubscribe

	// Logger is a slog.Logger scoped to the plugin. Plugins should use
	// it for any structured logging so daemon output stays consistent.
	Logger() *slog.Logger
}

// Unsubscribe detaches a Subscribe handler. Safe to call multiple times.
type Unsubscribe func()

// CLICommand wraps a cobra.Command produced by a plugin. Wrapping (vs.
// returning *cobra.Command directly) leaves room to add metadata
// later (e.g. minimum archai version) without breaking the contract.
type CLICommand struct {
	// Cmd is the cobra command this plugin contributes. archai mounts
	// it under a plugin-namespaced parent in M13; for M12 it can be
	// added to the root command as-is.
	Cmd *cobra.Command
}

// MCPTool is the descriptor archai uses to register a tool with the
// MCP transport. The Handler is called with the raw JSON arguments
// passed by the client; archai owns serialization to/from the wire.
type MCPTool struct {
	// Name is the tool name as exposed to MCP clients. M13 will
	// prefix it with the plugin manifest name.
	Name string

	// Description is the human-readable summary returned by
	// tools/list.
	Description string

	// InputSchema is the JSON Schema for the tool's arguments. May be
	// empty (tool takes no args).
	InputSchema map[string]any

	// Handler executes the tool. It receives the raw decoded
	// arguments and returns the result that will be JSON-encoded
	// into the tools/call response.
	Handler func(ctx context.Context, args map[string]any) (any, error)
}

// HTTPHandler is the descriptor archai uses to mount a plugin route
// onto its HTTP transport.
type HTTPHandler struct {
	// Path is the URL path. M13 mounts these under
	// /api/plugins/<plugin-name>/<path>; for M12 we hand them to the
	// bootstrap as-is.
	Path string

	// Methods restricts the handler to specific HTTP verbs. Empty
	// means "any verb".
	Methods []string

	// Handler is the http.Handler that serves the route.
	Handler http.Handler
}

// UIComponent describes a UI panel or widget contributed by a plugin.
// The browser-side bundle is shipped by the plugin; archai just hands
// the descriptor to its UI registry (M13).
type UIComponent struct {
	// Slot identifies where the component should mount. Allowed
	// values are defined by the UI host (e.g. EmbedSlotDashboard).
	Slot EmbedSlot

	// Title is the human-readable label shown in nav/tabs.
	Title string

	// AssetPath is the URL path under which the plugin serves its
	// UI assets via one of its HTTPHandlers. The UI registry uses
	// this to fetch the panel script/markup.
	AssetPath string
}

// EmbedSlot enumerates the UI mount points archai's browser shell
// exposes to plugins.
type EmbedSlot string

const (
	// EmbedSlotDashboard mounts a card on the dashboard landing page.
	EmbedSlotDashboard EmbedSlot = "dashboard"

	// EmbedSlotPackagePanel mounts a tab inside the package detail
	// view.
	EmbedSlotPackagePanel EmbedSlot = "package-panel"

	// EmbedSlotLayerPanel mounts a tab inside the layer detail view.
	EmbedSlotLayerPanel EmbedSlot = "layer-panel"

	// EmbedSlotSidebar adds an entry to the left-side navigation.
	EmbedSlotSidebar EmbedSlot = "sidebar"
)
