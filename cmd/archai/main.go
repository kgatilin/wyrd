// Package main provides the CLI entry point for archai.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/kgatilin/archai/internal/adapter/d2"
	"github.com/kgatilin/archai/internal/adapter/golang"
	httpAdapter "github.com/kgatilin/archai/internal/adapter/http"
	"github.com/kgatilin/archai/internal/adapter/mcp"
	yamlAdapter "github.com/kgatilin/archai/internal/adapter/yaml"
	"github.com/kgatilin/archai/internal/apply"
	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
	"github.com/kgatilin/archai/internal/sequence"
	"github.com/kgatilin/archai/internal/serve"
	"github.com/kgatilin/archai/internal/service"
	"github.com/kgatilin/archai/internal/target"
	"github.com/kgatilin/archai/internal/worktree"
	"github.com/spf13/cobra"
	yamlv3 "gopkg.in/yaml.v3"
)

func main() {
	// Root command
	rootCmd := &cobra.Command{
		Use:   "archai",
		Short: "Architecture diagram generator for Go projects",
		Long: `archai generates D2 architecture diagrams from Go source code.

It analyzes Go packages and creates visual representations of the code structure,
including interfaces, structs, functions, and their relationships.`,
	}

	// Diagram command group
	diagramCmd := &cobra.Command{
		Use:   "diagram",
		Short: "Commands for working with architecture diagrams",
		Long:  "Commands for generating, splitting, and composing architecture diagrams.",
	}
	rootCmd.AddCommand(diagramCmd)

	// Generate subcommand
	generateCmd := &cobra.Command{
		Use:   "generate [packages...]",
		Short: "Generate D2 diagrams from Go packages",
		Long: `Generate D2 architecture diagrams from Go source code.

Analyzes the specified Go packages and creates D2 diagram files in .arch/ folders
within each package directory.

By default, generates both:
  - pub.d2: Public API (exported symbols only)
  - internal.d2: Full implementation (all symbols)

Examples:
  # Generate diagrams for all packages in internal/
  archai diagram generate ./internal/...

  # Generate only public API diagrams
  archai diagram generate ./internal/... --pub

  # Generate only internal diagrams
  archai diagram generate ./internal/... --internal

  # Generate combined diagram to single file
  archai diagram generate ./internal/... -o architecture.d2`,
		Args: cobra.MinimumNArgs(1),
		RunE: runGenerate,
	}

	// Add flags
	generateCmd.Flags().Bool("pub", false, "Generate only pub.d2 (public API)")
	generateCmd.Flags().Bool("internal", false, "Generate only internal.d2 (full implementation)")
	generateCmd.Flags().StringP("output", "o", "", "Output to single file (combined mode)")
	generateCmd.Flags().StringP("format", "f", "d2", "Output format: d2 or yaml")
	generateCmd.Flags().Bool("debug", false, "Print debug information about packages and dependencies")
	generateCmd.Flags().String("overlay", "", "Path to archai.yaml overlay (default: auto-detect in current directory)")

	diagramCmd.AddCommand(generateCmd)

	// Split subcommand
	splitCmd := &cobra.Command{
		Use:   "split <diagram-file>",
		Short: "Split combined diagram into per-package specs",
		Long: `Split a combined D2 diagram into per-package specification files.

Takes a combined diagram (e.g., created with 'diagram generate -o') and creates
individual pub-spec.d2 files in each package's .arch/ directory.

The -spec.d2 suffix indicates these are target specifications (what the architecture
should look like) as opposed to pub.d2 files generated from actual code.

Examples:
  # Split a combined diagram into per-package specs
  archai diagram split docs/architecture.d2

  # Preview what would be created without writing files
  archai diagram split docs/architecture.d2 --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: runSplit,
	}
	splitCmd.Flags().Bool("dry-run", false, "Show what would be created without writing files")
	diagramCmd.AddCommand(splitCmd)

	// Compose subcommand
	composeCmd := &cobra.Command{
		Use:   "compose [packages...]",
		Short: "Compose a single diagram from saved .arch/ spec files",
		Long: `Compose reads saved .arch/ specification files from multiple packages
and combines them into a single diagram file.

By default, uses pub.d2 files (code-generated diagrams).
Use --spec to compose from pub-spec.d2 files (target specifications).

Examples:
  # Compose from code-generated diagrams (default)
  archai diagram compose ./internal/... --output=docs/current-architecture.d2

  # Compose from saved specs (target state)
  archai diagram compose ./internal/... --spec --output=docs/target-architecture.d2`,
		Args: cobra.MinimumNArgs(1),
		RunE: runCompose,
	}
	composeCmd.Flags().StringP("output", "o", "", "Output file path (required)")
	composeCmd.Flags().Bool("spec", false, "Use *-spec.d2 files instead of pub.d2")
	_ = composeCmd.MarkFlagRequired("output")
	diagramCmd.AddCommand(composeCmd)

	// Target command group (M4a)
	targetCmd := &cobra.Command{
		Use:   "target",
		Short: "Manage target architecture snapshots",
		Long: `Lock, list, inspect, select and delete frozen target snapshots.

A "target" is a frozen copy of the project's per-package .arch/*.yaml
specifications plus the overlay (archai.yaml) at a point in time. Targets
live under .arch/targets/<id>/ and the active target is tracked in
.arch/targets/CURRENT.`,
	}
	rootCmd.AddCommand(targetCmd)

	// target lock
	targetLockCmd := &cobra.Command{
		Use:   "lock <id>",
		Short: "Freeze the current architecture as target <id>",
		Long: `Regenerate per-package YAML specs (archai diagram generate --format yaml)
and freeze them — along with archai.yaml — into .arch/targets/<id>/.`,
		Args: cobra.ExactArgs(1),
		RunE: runTargetLock,
	}
	targetLockCmd.Flags().String("description", "", "Optional description for this target")
	targetLockCmd.Flags().Bool("skip-generate", false, "Skip regeneration; use existing .arch/*.yaml files")
	targetLockCmd.Flags().StringSliceP("paths", "p", []string{"./..."}, "Package paths to regenerate (only used when generate is enabled)")
	targetCmd.AddCommand(targetLockCmd)

	// target list
	targetListCmd := &cobra.Command{
		Use:   "list",
		Short: "List locked targets",
		Args:  cobra.NoArgs,
		RunE:  runTargetList,
	}
	targetCmd.AddCommand(targetListCmd)

	// target show
	targetShowCmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show target metadata and package summary",
		Args:  cobra.ExactArgs(1),
		RunE:  runTargetShow,
	}
	targetCmd.AddCommand(targetShowCmd)

	// target use
	targetUseCmd := &cobra.Command{
		Use:   "use <id>",
		Short: "Mark <id> as the active (CURRENT) target",
		Args:  cobra.ExactArgs(1),
		RunE:  runTargetUse,
	}
	targetCmd.AddCommand(targetUseCmd)

	// target delete
	targetDeleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a locked target",
		Args:  cobra.ExactArgs(1),
		RunE:  runTargetDelete,
	}
	targetDeleteCmd.Flags().Bool("force", false, "Delete even if <id> is the current target")
	targetCmd.AddCommand(targetDeleteCmd)

	// Diff command (M4b)
	diffCmd := &cobra.Command{
		Use:   "diff",
		Short: "Show differences between current code and a locked target",
		Long: `Compare the project's current architecture model (from .arch/*.yaml specs,
or the Go source if no specs are present) against a locked target and print
the structured differences.

By default, the active target (.arch/targets/CURRENT) is used; override with
--target <id>. Output format is plain text; use --format yaml or --format
json for machine-readable output.`,
		Args: cobra.NoArgs,
		RunE: runDiff,
	}
	diffCmd.Flags().String("target", "", "Target id to compare against (defaults to CURRENT)")
	diffCmd.Flags().StringP("format", "f", "text", "Output format: text, yaml, or json")
	rootCmd.AddCommand(diffCmd)

	// diff apply <patch.yaml> (M4c)
	diffApplyCmd := &cobra.Command{
		Use:   "apply <patch.yaml>",
		Short: "Apply a diff patch onto the active target snapshot",
		Long: `Apply a previously-computed diff (YAML) onto the active target
snapshot. The patch is interpreted as "how current code differs from target";
applying it updates the target model so it matches the current code.

The current code model is read from .arch/*.yaml (or via the Go reader when
no specs are present) and is used as the source of truth for any symbol
payload the patch needs. Target snapshot files under
.arch/targets/<id>/model/ are overwritten.`,
		Args: cobra.ExactArgs(1),
		RunE: runDiffApply,
	}
	diffApplyCmd.Flags().String("target", "", "Target id to apply patch onto (defaults to CURRENT)")
	diffCmd.AddCommand(diffApplyCmd)

	// validate (M4c)
	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate that current code matches the active target (CI mode)",
		Long: `Compare the project's current architecture model against the active
target and exit 0 when they match, non-zero otherwise. Violations are
printed in a CI-friendly format (one per line: <op> <kind> <path>).

Use --format yaml|json for machine-readable diff output. --target overrides
the CURRENT target.`,
		Args: cobra.NoArgs,
		RunE: runValidate,
	}
	validateCmd.Flags().String("target", "", "Target id to validate against (defaults to CURRENT)")
	validateCmd.Flags().StringP("format", "f", "text", "Output format: text, yaml, or json")
	rootCmd.AddCommand(validateCmd)

	// Overlay command group (M3c)
	overlayCmd := &cobra.Command{
		Use:   "overlay",
		Short: "Commands for working with archai.yaml overlays",
		Long: `Commands for validating and inspecting the composed overlay:
root archai.yaml plus package-local .arch/overlay.yaml fragments.`,
	}
	rootCmd.AddCommand(overlayCmd)

	// overlay check
	overlayCheckCmd := &cobra.Command{
		Use:   "check",
		Short: "Validate overlay and report layer-rule violations",
		Long: `Load the composed overlay (archai.yaml plus package-local
.arch/overlay.yaml fragments), validate it against go.mod, extract the current
Go model, and report any layer-rule violations.

Exits 0 when the overlay is valid and there are no violations; exits 1
when the overlay fails validation or when any violations are reported.

Examples:
  # Check the overlay at ./archai.yaml (default)
  archai overlay check

  # Check a specific overlay file
  archai overlay check --overlay path/to/archai.yaml`,
		Args: cobra.NoArgs,
		RunE: runOverlayCheck,
	}
	overlayCheckCmd.Flags().String("overlay", "", "Path to archai.yaml overlay (default: ./archai.yaml)")
	overlayCmd.AddCommand(overlayCheckCmd)

	// Serve command — long-running daemon holding an in-memory model
	// kept current via fsnotify. HTTP transport (M7a) is wired to the
	// browser UI; MCP stdio (--mcp-stdio) is still a stub until M5b.
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Run archai as a long-running daemon with an in-memory model",
		Long: `Run archai as a long-running daemon.

Loads the Go model, the archai.yaml overlay (if present), and the active
target id into memory, then watches the project root with fsnotify and
incrementally refreshes the model on change.

The HTTP transport (--http) serves the browser UI (dashboard, layers,
packages, configs, targets, diff, search) backed by the in-memory
model. MCP stdio (--mcp-stdio) remains a stub until M5b. With no
transport flag, the daemon runs as a silent model-keeper useful for
manual verification and as a base for future features.`,
		Args: cobra.NoArgs,
		RunE: runServe,
	}
	serveCmd.Flags().String("root", ".", "Project root directory")
	serveCmd.Flags().Bool("mcp-stdio", false, "Run as MCP stdio thin client, proxying tools/call to the worktree's HTTP daemon")
	// Default to loopback-only port 0 so parallel worktrees don't
	// fight over a fixed port and the daemon isn't exposed on the LAN
	// without an explicit opt-in. Users who want LAN access can pass
	// --http 0.0.0.0:PORT (or any specific interface). The bound
	// address is recorded in .arch/.worktree/<name>/serve.json for
	// `archai where` / `archai list-daemons` to discover.
	serveCmd.Flags().String("http", "127.0.0.1:0", "HTTP transport address (\"\" disables HTTP; default 127.0.0.1:0 binds loopback on a free port, pass 0.0.0.0:PORT for LAN access)")
	// M10: --multi discovers every git worktree of the project and
	// exposes each under /w/{name}/ so a single daemon can drive them
	// all. Omit the flag to keep the classic single-worktree behaviour.
	serveCmd.Flags().Bool("multi", false, "Serve every git worktree under /w/{name}/* (multi-worktree mode)")
	serveCmd.Flags().Bool("debug", false, "Verbose per-event logging")
	// M11: --no-daemon switches --mcp-stdio to one-shot mode. The MCP
	// stdio wrapper skips discovery/auto-start and runs every tool
	// call against a freshly-loaded in-memory model in the same
	// process (no fsnotify).
	serveCmd.Flags().Bool("no-daemon", false, "With --mcp-stdio: skip auto-start and run one-shot in-process (no HTTP daemon, no watcher)")
	// M11 idle-timeout: when non-zero, the HTTP daemon exits after
	// this duration without any requests. Auto-started daemons pass
	// 15m to keep untracked idle daemons from outliving their MCP
	// clients. User-started `archai serve` defaults to 0 (disabled).
	serveCmd.Flags().Duration("idle-timeout", 0, "Exit the daemon after this duration with no HTTP requests (0 disables; auto-started MCP daemons use 15m)")
	rootCmd.AddCommand(serveCmd)

	// where — print this worktree's active serve URL (if any).
	whereCmd := &cobra.Command{
		Use:   "where",
		Short: "Print this worktree's running serve URL",
		Long: `Read .arch/.worktree/<name>/serve.json and print the URL of the
daemon currently serving this worktree. Exits non-zero when no
daemon is running.`,
		Args: cobra.NoArgs,
		RunE: runWhere,
	}
	rootCmd.AddCommand(whereCmd)

	// list-daemons — scan all worktrees under this repo for live daemons.
	listDaemonsCmd := &cobra.Command{
		Use:   "list-daemons",
		Short: "List live archai serve daemons across all worktrees",
		Long: `Scan .arch/.worktree/*/serve.json under the current project root
and print one row per live daemon (worktree name, PID, URL, uptime).
Stale records (processes that have exited) are skipped.`,
		Args: cobra.NoArgs,
		RunE: runListDaemons,
	}
	rootCmd.AddCommand(listDaemonsCmd)

	// Sequence command (M6b)
	sequenceCmd := &cobra.Command{
		Use:   "sequence <target>",
		Short: "Render a static call-sequence tree rooted at a function or method",
		Long: `Walk the static call graph starting at the given symbol and print
the resulting tree as either an indented outline (default) or a D2 sequence
diagram.

Target format:
  <pkg/path>.<FuncName>
  <pkg/path>.<TypeName>.<MethodName>

The current model is loaded from per-package .arch/*.yaml files when
present; otherwise the Go reader parses ./... directly.

Examples:
  archai sequence internal/service.Service.Generate
  archai sequence internal/service.Service.Generate --depth 3
  archai sequence internal/service.Service.Generate --format d2 -o gen.d2`,
		Args: cobra.ExactArgs(1),
		RunE: runSequence,
	}
	sequenceCmd.Flags().Int("depth", 5, "Maximum call-chain depth")
	sequenceCmd.Flags().StringP("format", "f", "text", "Output format: text or d2")
	sequenceCmd.Flags().StringP("output", "o", "", "Write output to file instead of stdout")
	rootCmd.AddCommand(sequenceCmd)

	// version — prints `archai <Version>`.
	rootCmd.AddCommand(newVersionCmd())

	// extract — dumps per-package YAML/JSON (mirror of the MCP extract tool).
	rootCmd.AddCommand(newExtractCmd())

	// M12: in-process plugin contract. Built-in plugins register
	// themselves via init(); wirePlugins runs the bootstrap once
	// against a CLI-scoped Host and mounts every plugin-contributed
	// CLI command under the cobra root. M13 (#66) will mount the
	// MCP / HTTP / UI capabilities through the corresponding
	// transports.
	wirePlugins(rootCmd)

	// Execute root command
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// runServe handles `archai serve`. It wires SIGINT/SIGTERM into a
// cancellable context and dispatches between three operational modes:
//
//  1. Default: long-running HTTP daemon (no --mcp-stdio).
//  2. Thin-client MCP: --mcp-stdio without --no-daemon. Discovers (or
//     auto-starts) an HTTP daemon on this worktree and proxies every
//     tools/call to it over HTTP.
//  3. One-shot MCP: --mcp-stdio --no-daemon. Runs the full in-process
//     daemon (model + MCP stdio) in the current process with no HTTP
//     transport and no watcher-driven auto-reload; every tool call
//     sees whatever was loaded at startup.
func runServe(cmd *cobra.Command, args []string) error {
	root, _ := cmd.Flags().GetString("root")
	mcpStdio, _ := cmd.Flags().GetBool("mcp-stdio")
	httpAddr, _ := cmd.Flags().GetString("http")
	debug, _ := cmd.Flags().GetBool("debug")
	multi, _ := cmd.Flags().GetBool("multi")
	noDaemon, _ := cmd.Flags().GetBool("no-daemon")
	idleTimeout, _ := cmd.Flags().GetDuration("idle-timeout")

	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// M11: --mcp-stdio dispatches to thin-client or one-shot mode
	// before the classic daemon code path. --multi is not compatible
	// with either MCP mode.
	if mcpStdio {
		if multi {
			return fmt.Errorf("--multi is not compatible with --mcp-stdio")
		}
		if !noDaemon {
			return runMCPThinClient(ctx, root)
		}
		// One-shot in-process MCP: the in-memory model is loaded once,
		// no HTTP listener is started, and the watcher is skipped by
		// clearing HTTPAddr. The MCP stdio callback owns the session
		// lifetime.
		return serve.Serve(ctx, serve.Options{
			Root:     root,
			MCPStdio: true,
			MCPServe: func(ctx context.Context, state *serve.State) error {
				return mcp.Serve(ctx, state)
			},
			HTTPAddr: "",
			Debug:    debug,
		})
	}

	opts := serve.Options{
		Root:        root,
		MCPStdio:    false,
		HTTPAddr:    httpAddr,
		Debug:       debug,
		IdleTimeout: idleTimeout,
	}

	if multi {
		// Discover worktrees up-front. The MultiState is shared with
		// the HTTP server so lazy-loads of individual worktree models
		// happen on first request.
		absRoot := root
		if absRoot == "" {
			absRoot = "."
		}
		multiState := serve.NewMultiState(absRoot, serve.DefaultStateLoader)
		if err := multiState.Refresh(); err != nil {
			return fmt.Errorf("serve: refresh worktrees: %w", err)
		}
		opts.MultiState = multiState
		opts.HTTPServerFactory = func(_ *serve.State) (serve.HTTPTransport, error) {
			return httpAdapter.NewMultiServer(multiState)
		}
	} else {
		opts.HTTPServerFactory = func(state *serve.State) (serve.HTTPTransport, error) {
			return httpAdapter.NewServer(state)
		}
	}

	return serve.Serve(ctx, opts)
}

// autoStartIdleTimeout is the --idle-timeout value passed to daemons
// auto-started by the MCP thin client. The daemon exits after this
// duration of no HTTP requests so an orphaned MCP client doesn't leave
// a long-lived daemon running on the user's machine.
const autoStartIdleTimeout = 15 * time.Minute

// runMCPThinClient implements the `archai serve --mcp-stdio` thin
// client. It resolves the worktree's running HTTP daemon (auto-starting
// one if necessary) and runs the MCP stdio transport in client mode.
func runMCPThinClient(ctx context.Context, root string) error {
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolving root %s: %w", root, err)
	}

	rec, _, err := serve.DiscoverDaemon(absRoot)
	if err != nil {
		return fmt.Errorf("mcp-client: discover daemon: %w", err)
	}
	if rec == nil {
		// Auto-start a detached HTTP daemon on loopback and wait for it
		// to register serve.json. The daemon is bound to 127.0.0.1 so
		// auto-start never opens the daemon to the LAN, and it's asked
		// to exit after autoStartIdleTimeout of quiet to avoid leaking
		// orphan daemons past the MCP client's lifetime.
		rec, err = serve.AutoStartDaemon(serve.AutoStartOptions{
			Root:        absRoot,
			HTTPAddr:    "127.0.0.1:0",
			IdleTimeout: autoStartIdleTimeout,
		})
		if err != nil {
			return fmt.Errorf("mcp-client: auto-start daemon: %w", err)
		}
		fmt.Fprintf(os.Stderr, "mcp-client: auto-started daemon pid=%d addr=%s idle-timeout=%s\n",
			rec.PID, rec.HTTPAddr, autoStartIdleTimeout)
	} else {
		fmt.Fprintf(os.Stderr, "mcp-client: attached to daemon pid=%d addr=%s\n", rec.PID, rec.HTTPAddr)
	}

	return mcp.ServeClient(ctx, mcp.ClientOptions{
		Endpoint: "http://" + rec.HTTPAddr,
	})
}

// runSequence handles `archai sequence <target>`. It parses the target,
// loads the current model (YAML specs preferred, Go reader fallback),
// builds the call-sequence tree and emits it in the requested format.
func runSequence(cmd *cobra.Command, args []string) error {
	target := args[0]
	depth, _ := cmd.Flags().GetInt("depth")
	format, _ := cmd.Flags().GetString("format")
	output, _ := cmd.Flags().GetString("output")

	start, ok := sequence.ParseTarget(target)
	if !ok {
		return fmt.Errorf("invalid target %q (expected pkg/path.Func or pkg/path.Type.Method)", target)
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd: %w", err)
	}

	models, err := loadCurrentModel(ctx, projectRoot)
	if err != nil {
		return fmt.Errorf("loading current model: %w", err)
	}

	tree := sequence.Build(models, start, depth)
	if tree == nil {
		return fmt.Errorf("could not build sequence for %q", target)
	}

	var rendered string
	switch format {
	case "", "text":
		rendered = sequence.FormatText(tree)
	case "d2":
		rendered = sequence.FormatD2(tree)
	default:
		return fmt.Errorf("unsupported format %q (use text or d2)", format)
	}

	if output == "" {
		fmt.Print(rendered)
		return nil
	}
	if err := os.WriteFile(output, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", output, err)
	}
	return nil
}

// runOverlayCheck executes `archai overlay check`. It loads the overlay,
// validates it against the adjacent go.mod, extracts the current Go
// model, merges the overlay to detect layer-rule violations, and prints
// a human-readable report. Returns a non-nil error (which Cobra turns
// into a non-zero exit code) when validation fails or violations exist.
func runOverlayCheck(cmd *cobra.Command, args []string) error {
	overlayFlag, _ := cmd.Flags().GetString("overlay")

	overlayPath, goModPath := resolveOverlay(overlayFlag)
	if overlayPath == "" {
		return fmt.Errorf("no overlay found: pass --overlay or create archai.yaml in the current directory")
	}

	cfg, err := overlay.LoadComposed(overlayPath)
	if err != nil {
		return fmt.Errorf("loading overlay %s: %w", overlayPath, err)
	}

	if err := overlay.Validate(cfg, goModPath); err != nil {
		fmt.Fprintf(os.Stderr, "Overlay validation failed:\n%v\n", err)
		return fmt.Errorf("overlay validation failed")
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Extract current Go model. Always scan "./..." from the working
	// directory — callers are expected to run `archai overlay check`
	// from the project root (same directory as go.mod).
	goReader := golang.NewReader()
	models, err := goReader.Read(ctx, []string{"./..."})
	if err != nil {
		return fmt.Errorf("reading Go packages: %w", err)
	}

	mergedModels, violations, err := overlay.Merge(models, cfg)
	if err != nil {
		return fmt.Errorf("merging overlay: %w", err)
	}

	if len(violations) == 0 {
		fmt.Println("OK: overlay is valid and no layer-rule violations found.")
		return nil
	}

	// Build module-relative pkg path -> layer lookup from merged models
	// so the report can show the imported package's layer.
	pkgLayer := make(map[string]string)
	for _, m := range mergedModels {
		if m.Layer == "" {
			continue
		}
		rel := m.Path
		if cfg.Module != "" {
			rel = trimModulePrefix(cfg.Module, m.Path)
		}
		pkgLayer[rel] = m.Layer
	}

	printOverlayViolations(os.Stdout, violations, pkgLayer)
	return fmt.Errorf("%d layer-rule violation(s) found", violationCount(violations))
}

// printOverlayViolations renders a human-readable report of the given
// violations to w. pkgLayer maps module-relative package paths to their
// assigned layer so the "layer B" half of each line is accurate.
func printOverlayViolations(w io.Writer, violations []overlay.Violation, pkgLayer map[string]string) {
	total := violationCount(violations)
	fmt.Fprintf(w, "Found %d layer-rule violation(s):\n\n", total)
	for _, v := range violations {
		for _, imp := range v.Imports {
			targetLayer := pkgLayer[imp]
			if targetLayer == "" {
				targetLayer = "?"
			}
			fmt.Fprintf(w,
				"VIOLATION: package %s (layer %s) imports package %s (layer %s) — not allowed\n",
				v.Package, v.Layer, imp, targetLayer)
		}
	}
}

// trimModulePrefix returns pkgPath with the module prefix stripped, or
// pkgPath unchanged if the prefix does not apply.
func trimModulePrefix(module, pkgPath string) string {
	if pkgPath == module {
		return ""
	}
	if len(pkgPath) > len(module) && pkgPath[:len(module)] == module && pkgPath[len(module)] == '/' {
		return pkgPath[len(module)+1:]
	}
	return pkgPath
}

// violationCount sums the forbidden-import entries across all Violation records.
func violationCount(violations []overlay.Violation) int {
	n := 0
	for _, v := range violations {
		n += len(v.Imports)
	}
	return n
}

// resolveOverlay determines the overlay path and accompanying go.mod
// path used by the generate command. When explicitPath is non-empty
// it is used verbatim (and the adjacent go.mod is looked up); when
// empty we auto-detect ./archai.yaml in the working directory.
// Returns empty strings when no overlay is found.
func resolveOverlay(explicitPath string) (overlayPath, goModPath string) {
	if explicitPath != "" {
		dir := filepath.Dir(explicitPath)
		gm := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(gm); err != nil {
			gm = ""
		}
		return explicitPath, gm
	}
	candidate := "archai.yaml"
	if _, err := os.Stat(candidate); err != nil {
		return "", ""
	}
	gm := "go.mod"
	if _, err := os.Stat(gm); err != nil {
		gm = ""
	}
	return candidate, gm
}

// runGenerate executes the diagram generation command.
func runGenerate(cmd *cobra.Command, args []string) error {
	// Build options from flags
	pubOnly, _ := cmd.Flags().GetBool("pub")
	internalOnly, _ := cmd.Flags().GetBool("internal")
	output, _ := cmd.Flags().GetString("output")
	format, _ := cmd.Flags().GetString("format")
	debug, _ := cmd.Flags().GetBool("debug")
	overlayFlag, _ := cmd.Flags().GetString("overlay")

	// Resolve overlay path: explicit flag wins; otherwise auto-detect
	// archai.yaml in the current working directory.
	overlayPath, goModPath := resolveOverlay(overlayFlag)

	// Wire up dependencies based on format
	goReader := golang.NewReader()
	d2Reader := d2.NewReader()

	var writer service.ModelWriter
	var fileExt string
	switch format {
	case "yaml":
		writer = yamlAdapter.NewWriter()
		fileExt = ".yaml"
	case "d2":
		writer = d2.NewWriter()
		fileExt = ".d2"
	default:
		return fmt.Errorf("unsupported format %q (use d2 or yaml)", format)
	}

	yamlReader := yamlAdapter.NewReader()
	yamlWriter := yamlAdapter.NewWriter()
	svc := service.NewService(goReader, d2Reader, writer, service.WithYAML(yamlReader, yamlWriter))

	// Validate flags
	if pubOnly && internalOnly {
		return fmt.Errorf("cannot specify both --pub and --internal flags")
	}

	// Get context
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Combined mode: --output flag present
	if output != "" {
		// --pub and --internal don't apply to combined mode
		if pubOnly || internalOnly {
			fmt.Fprintln(os.Stderr, "Note: --pub and --internal flags are ignored in combined mode (always public)")
		}

		opts := service.GenerateCombinedOptions{
			Paths:      args,
			OutputPath: output,
			Debug:      debug,
		}

		result, err := svc.GenerateCombined(ctx, opts)
		if err != nil {
			return fmt.Errorf("generation failed: %w", err)
		}

		if result.PackageCount == 0 {
			fmt.Println("No packages with exported symbols found")
			return nil
		}

		fmt.Printf("Combined diagram generated: %s\n", result.OutputPath)
		fmt.Printf("Packages included: %d\n", result.PackageCount)
		return nil
	}

	// Split mode: existing logic
	opts := service.GenerateOptions{
		Paths:         args,
		PublicOnly:    pubOnly,
		InternalOnly:  internalOnly,
		FileExtension: fileExt,
		Debug:         debug,
		OverlayPath:   overlayPath,
		GoModPath:     goModPath,
	}

	results, violations, err := svc.GenerateWithOverlay(ctx, opts)
	if err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	// Print any overlay layer-rule violations to stderr so the user
	// sees them alongside generation output.
	if len(violations) > 0 {
		fmt.Fprintf(os.Stderr, "\nOverlay layer-rule violations (%d):\n", len(violations))
		for _, v := range violations {
			fmt.Fprintf(os.Stderr, "  %s [%s] imports forbidden: %v\n", v.Package, v.Layer, v.Imports)
		}
	}

	// Display results
	successCount := 0
	errorCount := 0

	for _, r := range results {
		if r.Error != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s: %v\n", r.PackagePath, r.Error)
			errorCount++
		} else {
			if r.PubFile != "" {
				fmt.Printf("Created %s\n", r.PubFile)
			}
			if r.InternalFile != "" {
				fmt.Printf("Created %s\n", r.InternalFile)
			}
			successCount++
		}
	}

	// Summary
	fmt.Printf("\nProcessed %d package(s): %d succeeded, %d failed\n", len(results), successCount, errorCount)

	if errorCount > 0 {
		return fmt.Errorf("generation completed with %d error(s)", errorCount)
	}

	return nil
}

// runSplit executes the diagram split command.
func runSplit(cmd *cobra.Command, args []string) error {
	// Wire up dependencies
	goReader := golang.NewReader()
	d2Reader := d2.NewReader()
	d2Writer := d2.NewWriter()
	svc := service.NewService(goReader, d2Reader, d2Writer, service.WithYAML(yamlAdapter.NewReader(), yamlAdapter.NewWriter()))

	// Get flags
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Get context
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	opts := service.SplitOptions{
		DiagramPath: args[0],
		DryRun:      dryRun,
	}

	result, err := svc.Split(ctx, opts)
	if err != nil {
		return fmt.Errorf("split failed: %w", err)
	}

	// Display results
	successCount := 0
	errorCount := 0

	for _, r := range result.Files {
		if r.Error != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s: %v\n", r.PackagePath, r.Error)
			errorCount++
		} else {
			if result.DryRun {
				fmt.Printf("Would create: %s\n", r.FilePath)
			} else {
				fmt.Printf("Created: %s\n", r.FilePath)
			}
			successCount++
		}
	}

	// Summary
	if result.DryRun {
		fmt.Printf("\n%d spec file(s) would be created\n", successCount)
	} else {
		fmt.Printf("\nSplit complete: %d spec file(s) created\n", successCount)
	}

	if errorCount > 0 {
		return fmt.Errorf("split completed with %d error(s)", errorCount)
	}

	return nil
}

// runCompose executes the diagram compose command.
func runCompose(cmd *cobra.Command, args []string) error {
	// Wire up dependencies
	goReader := golang.NewReader()
	d2Reader := d2.NewReader()
	d2Writer := d2.NewWriter()
	svc := service.NewService(goReader, d2Reader, d2Writer, service.WithYAML(yamlAdapter.NewReader(), yamlAdapter.NewWriter()))

	// Get flags
	outputPath, _ := cmd.Flags().GetString("output")
	specOnly, _ := cmd.Flags().GetBool("spec")

	// Determine mode
	mode := service.ComposeModeAuto
	if specOnly {
		mode = service.ComposeModeSpec
	}

	// Get context
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Execute compose
	result, err := svc.Compose(ctx, service.ComposeOptions{
		Paths:      args,
		OutputPath: outputPath,
		Mode:       mode,
	})
	if err != nil {
		return fmt.Errorf("compose failed: %w", err)
	}

	// Display result
	fmt.Printf("Composed %d packages into %s\n", result.PackageCount, result.OutputPath)
	if len(result.SkippedPaths) > 0 {
		fmt.Printf("Skipped %d packages (no .arch/ folder or missing files):\n", len(result.SkippedPaths))
		for _, path := range result.SkippedPaths {
			fmt.Printf("  - %s\n", path)
		}
	}

	return nil
}

// runTargetLock handles `archai target lock <id>`. It (optionally)
// regenerates per-package YAML specs and then freezes them plus
// archai.yaml into .arch/targets/<id>/.
func runTargetLock(cmd *cobra.Command, args []string) error {
	id := args[0]
	description, _ := cmd.Flags().GetString("description")
	skipGenerate, _ := cmd.Flags().GetBool("skip-generate")
	paths, _ := cmd.Flags().GetStringSlice("paths")

	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd: %w", err)
	}

	if !skipGenerate {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		goReader := golang.NewReader()
		d2Reader := d2.NewReader()
		yamlReader := yamlAdapter.NewReader()
		yamlWriter := yamlAdapter.NewWriter()
		svc := service.NewService(goReader, d2Reader, yamlWriter, service.WithYAML(yamlReader, yamlWriter))

		opts := service.GenerateOptions{
			Paths:         paths,
			FileExtension: ".yaml",
		}
		if overlayPath, goModPath := resolveOverlay(""); overlayPath != "" {
			opts.OverlayPath = overlayPath
			opts.GoModPath = goModPath
		}
		results, err := svc.Generate(ctx, opts)
		if err != nil {
			return fmt.Errorf("regenerating specs: %w", err)
		}
		for _, r := range results {
			if r.Error != nil {
				fmt.Fprintf(os.Stderr, "WARN: %s: %v\n", r.PackagePath, r.Error)
			}
		}
	}

	if err := target.Lock(projectRoot, id, target.LockOptions{Description: description}); err != nil {
		return err
	}
	fmt.Printf("Locked target %q\n", id)
	return nil
}

// runTargetList handles `archai target list`.
func runTargetList(cmd *cobra.Command, args []string) error {
	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd: %w", err)
	}
	metas, err := target.List(projectRoot)
	if err != nil {
		return err
	}
	if len(metas) == 0 {
		fmt.Println("No targets found.")
		return nil
	}
	cur, _ := target.Current(projectRoot)
	for _, m := range metas {
		marker := "  "
		if m.ID == cur {
			marker = "* "
		}
		fmt.Printf("%s%s  %s  %s\n", marker, m.ID, m.CreatedAt, m.Description)
	}
	return nil
}

// runTargetShow handles `archai target show <id>`.
func runTargetShow(cmd *cobra.Command, args []string) error {
	id := args[0]
	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd: %w", err)
	}
	meta, pkgs, err := target.Show(projectRoot, id)
	if err != nil {
		return err
	}
	fmt.Printf("id:          %s\n", meta.ID)
	fmt.Printf("created_at:  %s\n", meta.CreatedAt)
	fmt.Printf("base_commit: %s\n", meta.BaseCommit)
	if meta.Description != "" {
		fmt.Printf("description: %s\n", meta.Description)
	}
	fmt.Printf("packages:    %d\n", len(pkgs))
	for _, p := range pkgs {
		fmt.Printf("  - %s\n", p)
	}
	return nil
}

// runTargetUse handles `archai target use <id>`.
func runTargetUse(cmd *cobra.Command, args []string) error {
	id := args[0]
	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd: %w", err)
	}
	if err := target.Use(projectRoot, id); err != nil {
		return err
	}
	fmt.Printf("Using target %q\n", id)
	return nil
}

// runTargetDelete handles `archai target delete <id>`.
func runTargetDelete(cmd *cobra.Command, args []string) error {
	id := args[0]
	force, _ := cmd.Flags().GetBool("force")
	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd: %w", err)
	}
	if err := target.Delete(projectRoot, id, force); err != nil {
		return err
	}
	fmt.Printf("Deleted target %q\n", id)
	return nil
}

// runDiff handles `archai diff`. It loads the current model from the
// project's per-package .arch/*.yaml files (falling back to parsing Go
// sources if no specs are present) and the target model from
// .arch/targets/<id>/model/, computes a structured diff and prints it in
// the requested format.
func runDiff(cmd *cobra.Command, args []string) error {
	targetID, _ := cmd.Flags().GetString("target")
	format, _ := cmd.Flags().GetString("format")

	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd: %w", err)
	}

	// Resolve target id.
	if targetID == "" {
		cur, err := target.Current(projectRoot)
		if err != nil {
			return fmt.Errorf("reading CURRENT: %w", err)
		}
		if cur == "" {
			return errors.New("no target specified and no CURRENT target set; use --target <id> or `archai target use <id>`")
		}
		targetID = cur
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	current, err := loadCurrentModel(ctx, projectRoot)
	if err != nil {
		return fmt.Errorf("loading current model: %w", err)
	}

	targetModel, err := loadTargetModel(ctx, projectRoot, targetID)
	if err != nil {
		return fmt.Errorf("loading target %q: %w", targetID, err)
	}

	d := diff.Compute(current, targetModel)

	switch format {
	case "", "text":
		fmt.Print(diff.FormatText(d))
	case "yaml":
		out, err := diff.FormatYAML(d)
		if err != nil {
			return err
		}
		fmt.Print(out)
	case "json":
		out, err := diff.FormatJSON(d)
		if err != nil {
			return err
		}
		fmt.Print(out)
	default:
		return fmt.Errorf("unsupported format %q (use text, yaml, or json)", format)
	}

	return nil
}

// loadCurrentModel builds the "current" package model for diff. Preference
// order:
//  1. Per-package .arch/*.yaml specs found under projectRoot (excluding
//     .arch/targets/).
//  2. Fallback: parse Go sources under ./... via the Go reader.
func loadCurrentModel(ctx context.Context, projectRoot string) ([]domain.PackageModel, error) {
	files, err := findCurrentYAMLSpecs(projectRoot)
	if err != nil {
		return nil, err
	}
	if len(files) > 0 {
		return yamlAdapter.NewReader().Read(ctx, files)
	}

	// Fallback to Go source parsing.
	return golang.NewReader().Read(ctx, []string{"./..."})
}

// loadTargetModel loads the frozen model from .arch/targets/<id>/model/.
func loadTargetModel(ctx context.Context, projectRoot, id string) ([]domain.PackageModel, error) {
	targetDir := filepath.Join(projectRoot, ".arch", "targets", id)
	if _, err := os.Stat(targetDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("target %q not found", id)
		}
		return nil, err
	}
	modelDir := filepath.Join(targetDir, "model")
	files, err := collectYAMLFiles(modelDir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("target %q has no model files under %s", id, modelDir)
	}
	return yamlAdapter.NewReader().Read(ctx, files)
}

// findCurrentYAMLSpecs walks projectRoot for package-level .arch/*.yaml
// files. The .arch/targets tree is skipped so locked targets don't leak
// into the "current" model.
func findCurrentYAMLSpecs(projectRoot string) ([]string, error) {
	var out []string
	targetsTree := filepath.Join(projectRoot, ".arch", "targets")

	err := filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			// Skip the targets tree entirely.
			if path == targetsTree || strings.HasPrefix(path, targetsTree+string(os.PathSeparator)) {
				return filepath.SkipDir
			}
			// Skip hidden directories except `.arch` itself.
			name := d.Name()
			if strings.HasPrefix(name, ".") && name != ".arch" && name != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
			return nil
		}
		// Only include files located directly inside a .arch directory.
		if filepath.Base(filepath.Dir(path)) != ".arch" {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// collectYAMLFiles returns every *.yaml / *.yml file under root.
func collectYAMLFiles(root string) ([]string, error) {
	var out []string
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".yaml" || filepath.Ext(path) == ".yml" {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// runDiffApply handles `archai diff apply <patch.yaml>`. It loads the patch,
// resolves the active target, rebuilds the current + target models, invokes
// apply.Apply, and overwrites the target snapshot's model/ tree with the
// result.
func runDiffApply(cmd *cobra.Command, args []string) error {
	patchPath := args[0]
	targetID, _ := cmd.Flags().GetString("target")

	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd: %w", err)
	}

	patchData, err := os.ReadFile(patchPath)
	if err != nil {
		return fmt.Errorf("reading patch %s: %w", patchPath, err)
	}
	var patch diff.Diff
	if err := yamlv3.Unmarshal(patchData, &patch); err != nil {
		return fmt.Errorf("parsing patch %s: %w", patchPath, err)
	}
	if err := validatePatch(&patch); err != nil {
		return fmt.Errorf("patch %s: %w", patchPath, err)
	}

	if targetID == "" {
		cur, err := target.Current(projectRoot)
		if err != nil {
			return fmt.Errorf("reading CURRENT: %w", err)
		}
		if cur == "" {
			return errors.New("no target specified and no CURRENT target set; use --target <id> or `archai target use <id>`")
		}
		targetID = cur
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	current, err := loadCurrentModel(ctx, projectRoot)
	if err != nil {
		return fmt.Errorf("loading current model: %w", err)
	}

	targetModel, err := loadTargetModel(ctx, projectRoot, targetID)
	if err != nil {
		return fmt.Errorf("loading target %q: %w", targetID, err)
	}

	updated, err := apply.Apply(&patch, current, targetModel)
	if err != nil {
		return fmt.Errorf("applying patch: %w", err)
	}

	if err := writeTargetModels(ctx, projectRoot, targetID, updated); err != nil {
		return fmt.Errorf("writing target %q: %w", targetID, err)
	}
	fmt.Printf("Applied %d change(s) to target %q\n", len(patch.Changes), targetID)
	return nil
}

// runValidate handles `archai validate`. It exits 0 when current matches
// target, non-zero otherwise. Output format is controlled by --format.
func runValidate(cmd *cobra.Command, args []string) error {
	targetID, _ := cmd.Flags().GetString("target")
	format, _ := cmd.Flags().GetString("format")

	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd: %w", err)
	}

	if targetID == "" {
		cur, err := target.Current(projectRoot)
		if err != nil {
			return fmt.Errorf("reading CURRENT: %w", err)
		}
		if cur == "" {
			return errors.New("no target specified and no CURRENT target set; use --target <id> or `archai target use <id>`")
		}
		targetID = cur
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	current, err := loadCurrentModel(ctx, projectRoot)
	if err != nil {
		return fmt.Errorf("loading current model: %w", err)
	}
	targetModel, err := loadTargetModel(ctx, projectRoot, targetID)
	if err != nil {
		return fmt.Errorf("loading target %q: %w", targetID, err)
	}

	d := diff.Compute(current, targetModel)
	if d.IsEmpty() {
		fmt.Printf("code matches target %q\n", targetID)
		return nil
	}

	switch format {
	case "", "text":
		// CI-friendly: one violation per line, "<op> <kind> <path>".
		for _, c := range d.Changes {
			fmt.Printf("%s %s %s\n", c.Op, c.Kind, c.Path)
		}
	case "yaml":
		out, err := diff.FormatYAML(d)
		if err != nil {
			return err
		}
		fmt.Print(out)
	case "json":
		out, err := diff.FormatJSON(d)
		if err != nil {
			return err
		}
		fmt.Print(out)
	default:
		return fmt.Errorf("unsupported format %q (use text, yaml, or json)", format)
	}
	return fmt.Errorf("drift detected: %d change(s) against target %q", len(d.Changes), targetID)
}

// validatePatch ensures every Change in d carries a recognized Op and Kind
// so we fail fast on malformed patches before any on-disk writes.
func validatePatch(d *diff.Diff) error {
	if d == nil {
		return nil
	}
	for i, c := range d.Changes {
		switch c.Op {
		case diff.OpAdd, diff.OpRemove, diff.OpChange:
		default:
			return fmt.Errorf("change[%d]: unknown op %q", i, c.Op)
		}
		switch c.Kind {
		case diff.KindPackage, diff.KindInterface, diff.KindStruct, diff.KindFunction,
			diff.KindMethod, diff.KindField, diff.KindConst, diff.KindVar, diff.KindError,
			diff.KindDep, diff.KindLayerRule, diff.KindTypeDef:
		default:
			return fmt.Errorf("change[%d]: unknown kind %q", i, c.Kind)
		}
		if c.Path == "" {
			return fmt.Errorf("change[%d]: empty path", i)
		}
	}
	return nil
}

// runWhere handles `archai where`. It prints the URL of the daemon
// currently serving this worktree (if any) and exits non-zero when no
// daemon is recorded or the record is stale.
func runWhere(cmd *cobra.Command, args []string) error {
	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd: %w", err)
	}
	name := worktree.Name(projectRoot)
	rec, err := worktree.ReadServe(projectRoot, name)
	if err != nil {
		return fmt.Errorf("reading serve.json: %w", err)
	}
	if rec == nil {
		return fmt.Errorf("no daemon running in worktree %q (run `archai serve`)", name)
	}
	if !worktree.PIDAlive(rec.PID) {
		return fmt.Errorf("stale serve.json for worktree %q (pid %d not alive)", name, rec.PID)
	}
	fmt.Printf("http://%s\n", rec.HTTPAddr)
	return nil
}

// runListDaemons handles `archai list-daemons`. It prints a small
// table of live daemons keyed by worktree name.
func runListDaemons(cmd *cobra.Command, args []string) error {
	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd: %w", err)
	}
	daemons, err := worktree.ListDaemons(projectRoot)
	if err != nil {
		return err
	}
	if len(daemons) == 0 {
		fmt.Println("No live daemons.")
		return nil
	}
	fmt.Printf("%-20s  %-7s  %-22s  %s\n", "WORKTREE", "PID", "URL", "UPTIME")
	now := time.Now().UTC()
	for _, d := range daemons {
		uptime := "?"
		if !d.StartedAt.IsZero() {
			uptime = formatUptime(now.Sub(d.StartedAt))
		}
		fmt.Printf("%-20s  %-7d  %-22s  %s\n",
			d.Worktree, d.Record.PID, "http://"+d.Record.HTTPAddr, uptime)
	}
	return nil
}

// formatUptime renders a duration as a short human-readable string
// (e.g. "3m", "2h14m", "5d3h"). Precision is intentionally coarse.
func formatUptime(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) - 60*h
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	}
	days := int(d.Hours()) / 24
	h := int(d.Hours()) - 24*days
	if h == 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dd%dh", days, h)
}

// writeTargetModels overwrites the target snapshot's model/ tree with the
// given per-package models. Existing package YAML files are replaced; the
// internal.yaml per package is regenerated (pub.yaml is redundant with
// internal.yaml for the full model and is left absent to avoid drift
// between the two on subsequent diffs — the YAML reader accepts either).
func writeTargetModels(ctx context.Context, projectRoot, id string, models []domain.PackageModel) error {
	targetDir := filepath.Join(projectRoot, ".arch", "targets", id)
	modelDir := filepath.Join(targetDir, "model")

	// Wipe the existing model/ tree so removed packages vanish cleanly.
	if err := os.RemoveAll(modelDir); err != nil {
		return fmt.Errorf("removing %s: %w", modelDir, err)
	}
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", modelDir, err)
	}

	writer := yamlAdapter.NewWriter()
	for _, m := range models {
		out := filepath.Join(modelDir, m.Path, "internal.yaml")
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(out), err)
		}
		if err := writer.Write(ctx, m, domain.WriteOptions{OutputPath: out}); err != nil {
			return fmt.Errorf("writing %s: %w", out, err)
		}
	}
	return nil
}
