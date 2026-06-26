package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// graph.go exposes the daemon-backed MCP tools (search, the analysis
// lenses, node/package inspection, …) as first-class CLI commands under
// `archai graph <tool>`. Each command is named after its MCP tool and
// carries typed flags mirroring that tool's input schema; at run time it
// does not compute anything locally — it proxies a tools/call to a running
// daemon via callDaemonTool, so the same graph that MCP clients query is
// reachable from any shell. Target another repo's daemon with --daemon.

// argKind is the value type of a tool flag, deciding both how the cobra
// flag is registered and how its value is marshaled into the JSON
// arguments object.
type argKind int

const (
	argString argKind = iota
	argInt
	argNumber
	argBool
	argStringSlice
	// argKAuto is the cluster-count flag: the schema accepts either the
	// string "auto" or an integer, so a numeric value is sent as an int
	// and anything else as a string.
	argKAuto
)

// argFlag describes one CLI flag and where its value lands in the tool's
// JSON arguments. path is dotted to allow nested objects (e.g.
// "selector.package" or "filters.kinds").
type argFlag struct {
	name     string
	kind     argKind
	path     string
	required bool
	def      any
	usage    string
}

// toolCmd is one `graph <tool>` subcommand: the MCP tool name (also the
// command name) and the flags that build its arguments.
type toolCmd struct {
	name  string
	short string
	flags []argFlag
}

// graphTools is the registry of proxied tools. apply_diff is intentionally
// omitted — it takes a whole YAML patch body and already has the local
// `archai apply` command.
func graphTools() []toolCmd {
	pkgFlags := []argFlag{
		{name: "package", kind: argString, path: "package", required: true, usage: "Package path to analyze"},
		{name: "include-subpackages", kind: argBool, path: "include_subpackages", def: true, usage: "Include subpackages"},
	}
	return []toolCmd{
		{name: "status", short: "Daemon readiness and indexing progress"},
		{name: "refresh", short: "Rebuild retrieval indexes from the current snapshot"},
		{name: "embedding_coverage", short: "Dense-embedding coverage over the indexed graph"},
		{name: "list_packages", short: "List packages known to the daemon"},
		{name: "list_targets", short: "List locked targets under .arch/targets/"},
		{name: "list_bounded_contexts", short: "List bounded contexts from the overlay"},
		{
			name:  "extract",
			short: "Return the extracted Go model (optionally filtered by package)",
			flags: []argFlag{{name: "path", kind: argStringSlice, path: "paths", usage: "Module-relative package path (repeatable)"}},
		},
		{
			name:  "get_package",
			short: "Return the full PackageModel for one package",
			flags: []argFlag{{name: "path", kind: argString, path: "path", required: true, usage: "Module-relative package path"}},
		},
		{
			name:  "get_bounded_context",
			short: "Return one bounded context by name",
			flags: []argFlag{{name: "name", kind: argString, path: "name", required: true, usage: "Bounded context name"}},
		},
		{
			name:  "diff",
			short: "Structured diff between the current model and a locked target",
			flags: []argFlag{{name: "target", kind: argString, path: "target", usage: "Target id (default: active target)"}},
		},
		{
			name:  "validate",
			short: "Report drift between the current model and a target",
			flags: []argFlag{{name: "target", kind: argString, path: "target", usage: "Target id (default: active target)"}},
		},
		{
			name:  "lock_target",
			short: "Freeze the current model into .arch/targets/<id>/",
			flags: []argFlag{
				{name: "id", kind: argString, path: "id", required: true, usage: "Target id"},
				{name: "description", kind: argString, path: "description", usage: "Optional description"},
			},
		},
		{
			name:  "set_current_target",
			short: "Mark a target id as the active target",
			flags: []argFlag{{name: "id", kind: argString, path: "id", required: true, usage: "Target id (must exist)"}},
		},
		{
			name:  "search",
			short: "Hybrid semantic + lexical code search",
			flags: []argFlag{
				{name: "query", kind: argString, path: "query", required: true, usage: "Search query"},
				{name: "k", kind: argInt, path: "k", usage: "Max results (default 10)"},
				{name: "kind", kind: argStringSlice, path: "filters.kinds", usage: "Symbol kinds to include (repeatable)"},
				{name: "package-prefix", kind: argString, path: "filters.package_prefix", usage: "Only symbols under this package prefix"},
			},
		},
		{
			name:  "search_graph",
			short: "Search returning a subgraph of seeds plus neighbors",
			flags: []argFlag{
				{name: "query", kind: argString, path: "query", required: true, usage: "Search query"},
				{name: "k", kind: argInt, path: "k", usage: "Max seed results (default 10)"},
				{name: "hops", kind: argInt, path: "hops", usage: "Hops to expand from seeds (default 1)"},
			},
		},
		{
			name:  "expand",
			short: "Expand from node IDs to their neighbors",
			flags: []argFlag{
				{name: "node", kind: argStringSlice, path: "node_ids", required: true, usage: "Node ID to expand from (repeatable)"},
				{name: "hops", kind: argInt, path: "hops", usage: "Hops to expand (default 1)"},
				{name: "edge", kind: argStringSlice, path: "edges", usage: "Edge kinds to traverse (repeatable; empty = all)"},
			},
		},
		{
			name:  "get_node",
			short: "Full detail (source + edges) for one symbol",
			flags: []argFlag{{name: "id", kind: argString, path: "id", required: true, usage: "Node ID (package.SymbolName)"}},
		},
		{name: "components", short: "Connected components of a package subgraph", flags: pkgFlags},
		{name: "file_hotspots", short: "Structurally overloaded files in a package", flags: pkgFlags},
		{name: "trophic_layers", short: "Emergent dependency layers and inversions", flags: pkgFlags},
		{
			name:  "spectral_cluster",
			short: "Natural module clusters over structural edges",
			flags: []argFlag{
				{name: "package", kind: argString, path: "selector.package", usage: "Package path prefix to cluster"},
				{name: "include-subpackages", kind: argBool, path: "selector.include_subpackages", def: true, usage: "Include subpackages"},
				{name: "node-kind", kind: argStringSlice, path: "selector.node_kinds", usage: "Node kinds to include (repeatable)"},
				{name: "edge-kind", kind: argStringSlice, path: "selector.edge_kinds", usage: "Edge kinds to consider (repeatable)"},
				{name: "k", kind: argKAuto, path: "k", usage: `Cluster count: "auto" or an integer`},
				{name: "collapse-members", kind: argBool, path: "collapse_members", usage: "Contract methods/fields into owning types"},
			},
		},
		{
			name:  "semantic_cluster",
			short: "Natural module clusters over embedding similarity",
			flags: []argFlag{
				{name: "package", kind: argString, path: "selector.package", usage: "Package path prefix to cluster"},
				{name: "include-subpackages", kind: argBool, path: "selector.include_subpackages", def: true, usage: "Include subpackages"},
				{name: "node-kind", kind: argStringSlice, path: "selector.node_kinds", usage: "Node kinds to include (repeatable)"},
				{name: "k", kind: argKAuto, path: "k", usage: `Cluster count: "auto" or an integer`},
				{name: "knn", kind: argInt, path: "knn", usage: "Nearest neighbors for the similarity graph (default 8)"},
				{name: "min-sim", kind: argNumber, path: "min_sim", usage: "Minimum cosine similarity for an edge"},
			},
		},
		{
			name:  "latent_domains",
			short: "Detect latent domains glued by cross-cutting coupling",
			flags: []argFlag{
				{name: "package", kind: argString, path: "selector.package", usage: "Package path prefix to analyze"},
				{name: "include-subpackages", kind: argBool, path: "selector.include_subpackages", def: true, usage: "Include subpackages"},
				{name: "node-kind", kind: argStringSlice, path: "selector.node_kinds", usage: "Node kinds to include (repeatable)"},
				{name: "diff", kind: argBool, path: "selector.diff", usage: "Scope to the change region vs the review base"},
				{name: "k", kind: argKAuto, path: "k", usage: `Cluster count: "auto" or an integer`},
				{name: "knn", kind: argInt, path: "knn", usage: "Nearest neighbors for the similarity graph (default 8)"},
			},
		},
	}
}

// newGraphCmd builds the `archai graph` group. The --daemon flag is
// persistent so every subcommand inherits it.
func newGraphCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Call the daemon's graph/analysis tools from the CLI",
		Long: `Invoke the same MCP tools the daemon serves — search, the analysis
lenses (trophic_layers, spectral_cluster, latent_domains, …), and node/package
inspection — as CLI commands. Each subcommand proxies a tools/call to a running
daemon rather than computing locally, so the live graph is reachable from any
shell. By default it targets the current repo's daemon; use --daemon <name|pid>
to reach another. Start a daemon first with 'archai daemon start'.`,
	}
	cmd.PersistentFlags().String("daemon", "", "Target daemon by name or pid (default: current repo's daemon)")
	for _, t := range graphTools() {
		cmd.AddCommand(newGraphToolCmd(t))
	}
	return cmd
}

func newGraphToolCmd(t toolCmd) *cobra.Command {
	sub := &cobra.Command{
		Use:   t.name,
		Short: t.short,
		Args:  cobra.NoArgs,
		// A daemon-unreachable or tool-level error is a runtime failure, not
		// a usage mistake — don't dump the help text on it.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGraphTool(cmd, t)
		},
	}
	for _, f := range t.flags {
		registerArgFlag(sub, f)
		if f.required {
			_ = sub.MarkFlagRequired(f.name)
		}
	}
	return sub
}

func registerArgFlag(cmd *cobra.Command, f argFlag) {
	switch f.kind {
	case argString, argKAuto:
		def, _ := f.def.(string)
		cmd.Flags().String(f.name, def, f.usage)
	case argInt:
		def, _ := f.def.(int)
		cmd.Flags().Int(f.name, def, f.usage)
	case argNumber:
		def, _ := f.def.(float64)
		cmd.Flags().Float64(f.name, def, f.usage)
	case argBool:
		def, _ := f.def.(bool)
		cmd.Flags().Bool(f.name, def, f.usage)
	case argStringSlice:
		cmd.Flags().StringSlice(f.name, nil, f.usage)
	}
}

func runGraphTool(cmd *cobra.Command, t toolCmd) error {
	daemonArg, _ := cmd.Flags().GetString("daemon")
	args, err := buildToolArgs(cmd, t)
	if err != nil {
		return err
	}
	text, isErr, err := callDaemonTool(daemonArg, t.name, args)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), prettyJSON(text))
	if isErr {
		return fmt.Errorf("%s reported an error", t.name)
	}
	return nil
}

// buildToolArgs assembles the JSON arguments object for a tool from its
// flags. A flag is included only when the user set it (or it is required),
// so the daemon applies the tool's own defaults for everything else.
func buildToolArgs(cmd *cobra.Command, t toolCmd) (map[string]any, error) {
	args := map[string]any{}
	for _, f := range t.flags {
		if !f.required && !cmd.Flags().Changed(f.name) {
			continue
		}
		v, ok, err := argValue(cmd, f)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		setArgPath(args, f.path, v)
	}
	return args, nil
}

// argValue reads a flag's value as the JSON type the tool expects. ok is
// false when the value should be omitted (e.g. an empty slice).
func argValue(cmd *cobra.Command, f argFlag) (value any, ok bool, err error) {
	switch f.kind {
	case argString:
		s, _ := cmd.Flags().GetString(f.name)
		return s, true, nil
	case argInt:
		n, _ := cmd.Flags().GetInt(f.name)
		return n, true, nil
	case argNumber:
		x, _ := cmd.Flags().GetFloat64(f.name)
		return x, true, nil
	case argBool:
		b, _ := cmd.Flags().GetBool(f.name)
		return b, true, nil
	case argStringSlice:
		ss, _ := cmd.Flags().GetStringSlice(f.name)
		if len(ss) == 0 {
			return nil, false, nil
		}
		return ss, true, nil
	case argKAuto:
		s, _ := cmd.Flags().GetString(f.name)
		if s == "" {
			return nil, false, nil
		}
		if n, convErr := strconv.Atoi(s); convErr == nil {
			return n, true, nil
		}
		return s, true, nil
	}
	return nil, false, fmt.Errorf("unhandled flag kind for %q", f.name)
}

// setArgPath assigns v at a dotted path within m, creating intermediate
// objects as needed (e.g. "selector.package" -> m["selector"]["package"]).
func setArgPath(m map[string]any, path string, v any) {
	parts := strings.Split(path, ".")
	for i := 0; i < len(parts)-1; i++ {
		next, ok := m[parts[i]].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[parts[i]] = next
		}
		m = next
	}
	m[parts[len(parts)-1]] = v
}
