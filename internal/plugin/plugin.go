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
	"io/fs"
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

// UIComponent describes a custom-element widget contributed by a plugin.
// M13 (#66) spec: the host serves the plugin's static assets at
// /plugins/<plugin-name>/assets/... from Assets, queries the UI registry
// per (view, slot) on each browser page, injects a <plugin-X> custom
// element wherever the component requested a mount, and emits a
// <script src='/plugins/<plugin-name>/assets/<entry>' defer> tag once.
type UIComponent struct {
	// Element is the custom-element tag name the plugin registers in
	// its entry script (e.g. "plugin-complexity-heatmap"). Hyphenation
	// is required by the Custom Elements spec; archai also uses it as
	// an HTML-safe id, so it must not contain whitespace or quotes.
	Element string

	// Assets is the embedded filesystem holding the plugin's browser
	// bundle. Served verbatim by http.FileServer at
	// /plugins/<plugin-name>/assets/.
	Assets fs.FS

	// Entry is the asset-relative path of the script that defines
	// Element. Loaded via <script defer> on every host page that
	// embeds this component.
	Entry string

	// EmbedAt lists every (view, slot) pair where this component
	// should be rendered. A single component can appear in multiple
	// views (e.g. dashboard card + package_detail tab).
	EmbedAt []EmbedSlot
}

// EmbedSlot identifies one mount point on a host page. View picks the
// page (dashboard, package_detail, ...); Slot picks the region within
// that page (main, side_panel, extra_tab, header_widget). Label is the
// human-readable string used for tabbed slots.
type EmbedSlot struct {
	// View names the host page. Allowed values for v1 are the
	// constants ViewDashboard, ViewLayers, ViewPackages,
	// ViewPackageDetail, ViewTypeDetail, ViewDiff, ViewTargets.
	// Plugins that pass an unknown view value are dropped from the
	// registry with a warning.
	View string

	// Slot picks the region inside View. Allowed values for v1 are
	// SlotMain, SlotSidePanel, SlotExtraTab, SlotHeaderWidget.
	Slot string

	// Label is the human-readable name used when the slot is a tab
	// (SlotExtraTab) or a sidebar item. Ignored by other slots.
	Label string
}

// View names recognised by the UI registry. Unknown views are dropped.
const (
	ViewDashboard     = "dashboard"
	ViewLayers        = "layers"
	ViewPackages      = "packages"
	ViewPackageDetail = "package_detail"
	ViewTypeDetail    = "type_detail"
	ViewDiff          = "diff"
	ViewTargets       = "targets"
)

// Slot names recognised by the UI registry. Unknown slots are dropped.
const (
	SlotMain          = "main"
	SlotSidePanel     = "side_panel"
	SlotExtraTab      = "extra_tab"
	SlotHeaderWidget  = "header_widget"
)
