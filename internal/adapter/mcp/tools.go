package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	archmotifAdapter "github.com/kgatilin/archai/internal/adapter/archmotif"
	yamlAdapter "github.com/kgatilin/archai/internal/adapter/yaml"
	"github.com/kgatilin/archai/internal/apply"
	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/plugin"
	"github.com/kgatilin/archai/internal/retrieval"
	"github.com/kgatilin/archai/internal/serve"
	"github.com/kgatilin/archai/internal/target"
	archmotifimport "github.com/kgatilin/archmotif/pkg/archmotifimport"
	archmotifComponents "github.com/kgatilin/archmotif/pkg/components"
	archmotifFilestats "github.com/kgatilin/archmotif/pkg/filestats"
	"github.com/kgatilin/archmotif/pkg/spectralcluster"
	archmotifTrophic "github.com/kgatilin/archmotif/pkg/trophic"
	yamlv3 "gopkg.in/yaml.v3"
)

// pluginToolsMu guards the package-level plugin tool registry. Plugin
// tool registration happens once at daemon startup (or once per CLI
// invocation in --no-daemon mode) so contention is irrelevant in
// practice; the mutex keeps `go test ./...` happy when several test
// binaries register and reset the registry in parallel.
var pluginToolsMu sync.RWMutex
var pluginTools []plugin.NamedMCPTool

// SetPluginTools registers the prefixed plugin tools the MCP transport
// should expose alongside the built-in tools. Calling SetPluginTools
// with a different slice replaces every previously registered plugin
// tool — the daemon owns the lifecycle.
func SetPluginTools(tools []plugin.NamedMCPTool) {
	pluginToolsMu.Lock()
	defer pluginToolsMu.Unlock()
	cp := make([]plugin.NamedMCPTool, len(tools))
	copy(cp, tools)
	pluginTools = cp
}

// pluginToolsSnapshot returns a copy of the current plugin tool list
// safe to iterate without holding the mutex.
func pluginToolsSnapshot() []plugin.NamedMCPTool {
	pluginToolsMu.RLock()
	defer pluginToolsMu.RUnlock()
	out := make([]plugin.NamedMCPTool, len(pluginTools))
	copy(out, pluginTools)
	return out
}

// ToolDefinition describes a single MCP tool exposed via tools/list.
// InputSchema is a JSON Schema object; we keep it as an any so each
// tool can define its own shape without a shared struct.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// ToolResultContent is one "content" item returned by tools/call. We
// only emit text blocks; binary/resource blocks are out of scope for
// the current tool surface.
type ToolResultContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolResult is the payload returned by tools/call. IsError is a hint
// for clients — JSON-RPC errors are used for protocol-level failures
// (unknown tool etc.) while tool-level errors (package not found) are
// reported via IsError + a text message so the model can see them.
type ToolResult struct {
	Content []ToolResultContent `json:"content"`
	IsError bool                `json:"isError,omitempty"`
}

// PackageSummary is the minimal record returned by list_packages. The
// summary is intentionally small so agents can decide which package to
// drill into via get_package without paying for the full payload.
type PackageSummary struct {
	Path           string `json:"path"`
	Name           string `json:"name"`
	Layer          string `json:"layer,omitempty"`
	InterfaceCount int    `json:"interface_count"`
	StructCount    int    `json:"struct_count"`
	FunctionCount  int    `json:"function_count"`
}

// ValidateResult is the structured payload returned by the `validate`
// tool: ok=true when no drift, otherwise the list of Change entries
// describing violations.
type ValidateResult struct {
	OK         bool          `json:"ok"`
	Target     string        `json:"target"`
	Violations []diff.Change `json:"violations"`
}

// BCSummary is the minimal record returned by list_bounded_contexts.
type BCSummary struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Relationship string   `json:"relationship,omitempty"`
	Aggregates   []string `json:"aggregates"`
	Upstream     []string `json:"upstream,omitempty"`
	Downstream   []string `json:"downstream,omitempty"`
}

// ToolDefinitions returns the built-in tools we advertise plus every
// plugin tool registered via SetPluginTools (M13). Plugin tools are
// surfaced with the canonical "plugin.<plugin-name>.<tool-name>"
// prefix; the prefix lets agents tell core tools from plugin tools at
// a glance and prevents accidental collisions.
func ToolDefinitions() []ToolDefinition {
	defs := builtinToolDefinitions()
	for _, t := range pluginToolsSnapshot() {
		schema := any(t.Tool.InputSchema)
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		defs = append(defs, ToolDefinition{
			Name:        plugin.PrefixedMCPName(t.Plugin, t.Tool.Name),
			Description: t.Tool.Description,
			InputSchema: schema,
		})
	}
	return defs
}

// builtinToolDefinitions returns the nine archai-core tools. Kept as a
// function rather than a var so the JSON-Schema maps don't become
// mutable package state.
func builtinToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "extract",
			Description: "Return the full extracted Go model (all packages) from the archai daemon's in-memory state. Optionally filter to specific package paths.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"paths": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Module-relative package paths to include. When omitted or empty, all packages are returned.",
					},
				},
			},
		},
		{
			Name:        "list_packages",
			Description: "List packages known to the daemon with minimal summary fields (path, name, layer, counts).",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "get_package",
			Description: "Return the full PackageModel for a single package identified by its module-relative path.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Module-relative package path, e.g. internal/service.",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "lock_target",
			Description: "Freeze the daemon's current in-memory model into .arch/targets/<id>/ and return the target meta.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "Target id — also the directory name under .arch/targets/.",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "Optional free-form description stored in meta.yaml.",
					},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "list_targets",
			Description: "List all locked targets under .arch/targets/, sorted by id.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "set_current_target",
			Description: "Mark the given target id as the active target by writing .arch/targets/CURRENT.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "Target id that must already exist under .arch/targets/.",
					},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "diff",
			Description: "Compute the structured diff between the current in-memory model and a locked target. Defaults to the active (CURRENT) target.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "Target id. When omitted, the active target from .arch/targets/CURRENT is used.",
					},
				},
			},
		},
		{
			Name:        "apply_diff",
			Description: "Apply a structured diff patch (YAML) to the active (or specified) target, rewriting its model/ snapshot.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"patch_yaml": map[string]any{
						"type":        "string",
						"description": "YAML-encoded diff.Diff patch (same shape as `archai diff --format yaml`).",
					},
					"target": map[string]any{
						"type":        "string",
						"description": "Target id. When omitted, the active target from .arch/targets/CURRENT is used.",
					},
				},
				"required": []string{"patch_yaml"},
			},
		},
		{
			Name:        "validate",
			Description: "Report drift between the current in-memory model and the active (or specified) target as {ok, violations:[...]}. ok=true means no drift.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "Target id. When omitted, the active target from .arch/targets/CURRENT is used.",
					},
				},
			},
		},
		{
			Name:        "list_bounded_contexts",
			Description: "List all bounded contexts declared in the archai.yaml overlay with their aggregates and context-map relationships.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "get_bounded_context",
			Description: "Return the full detail of a single bounded context identified by name, including aggregates, upstream/downstream peers, and member packages.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Bounded context name as declared in archai.yaml.",
					},
				},
				"required": []string{"name"},
			},
		},
		// Retrieval tools
		{
			Name:        "search",
			Description: "Hybrid semantic + lexical code search: dense vector similarity (when an embedder is configured) and BM25 lexical matching, fused with reciprocal rank fusion. Reach for it to find code by meaning — what something does or is about — when you don't have an exact name or a known symbol to navigate from; prefer literal text search for exact strings and direct go-to-definition / find-references when the symbol is already known. Returns a flat ranked list; use plain `search` to find WHERE something lives, and `search_graph` instead when you need HOW it connects to the rest of the code. Each result carries node_id, kind, file, line, doc, and a source snippet, so file:line lets you jump straight to the code. Scores are fused RRF ranks (small values, often ~0.03), not absolute 0..1 relevance — use them only to order results, never as a cutoff threshold. The `dense` flag in the response is true when the vector layer contributed to ranking for this query and false when results are BM25-only (no embedder / empty vector index) — a hint about recall, not result validity. Results reflect the last indexed snapshot; symbols written since then stay invisible until you call `refresh`. Narrow noise with filters.kinds / filters.package_prefix when you already know the symbol kind or package.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Natural language or keyword query to search for.",
					},
					"k": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default 10).",
					},
					"filters": map[string]any{
						"type":        "object",
						"description": "Optional filters to constrain results.",
						"properties": map[string]any{
							"kinds": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Symbol kinds to include (iface, class, func, type, const, var, error).",
							},
							"package_prefix": map[string]any{
								"type":        "string",
								"description": "Only include symbols from packages with this prefix.",
							},
						},
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "search_graph",
			Description: "Like `search`, but returns a subgraph instead of a flat list: the matching seed symbols plus their neighbors up to `hops` away via uses/returns/implements/calls edges. Default to this over plain `search` when the question is about connections rather than location — impact analysis, dependency tracing, or 'what calls / implements / returns X'. Output grows fast with `k` and `hops` (a broad query can return hundreds of nodes and edges), so keep `k` small (~5) and `hops` at 1–2 unless you deliberately want a wider blast radius. Same snapshot freshness (call `refresh` to pick up new code) and same RRF scoring semantics as `search`.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Natural language or keyword query to search for.",
					},
					"k": map[string]any{
						"type":        "integer",
						"description": "Maximum number of search results to use as seeds (default 10).",
					},
					"hops": map[string]any{
						"type":        "integer",
						"description": "Number of hops to expand from seed nodes (default 1).",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "expand",
			Description: "Expand from node IDs you already have (from a prior `search` or `search_graph` result) to their neighbors via graph edges, up to `hops` away. This is the breadth tool — it walks outward from many nodes at once and returns lightweight node summaries plus edges, without source bodies; use `get_node` when you instead need the full code of one symbol. Use expand to widen the graph around symbols you've already found without re-running a query. Restrict `edges` to specific kinds (uses, returns, implements, calls) to keep the result focused; node IDs use the package.SymbolName format returned by search.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"node_ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Node IDs to expand from (format: package.SymbolName).",
					},
					"hops": map[string]any{
						"type":        "integer",
						"description": "Number of hops to expand (default 1).",
					},
					"edges": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Edge kinds to traverse (uses, returns, implements, calls). Empty means all.",
					},
				},
				"required": []string{"node_ids"},
			},
		},
		{
			Name:        "get_node",
			Description: "Return full detail for a single symbol — its complete source body and incident edges — given a node_id (package.SymbolName) from a `search` or `search_graph` result. This is the depth tool: one node, with its actual code; use `expand` instead when you want to walk edges across many nodes without bodies. Use it to read the code once search has located the symbol.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "Node ID in format package.SymbolName.",
					},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "refresh",
			Description: "Rebuild the retrieval indexes from the current model snapshot, picking up code written since the last index. `search` and `search_graph` read this snapshot, so newly added or changed symbols stay invisible to them until you refresh. Returns counts of reindexed and removed nodes.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "spectral_cluster",
			Description: "Split a package or subgraph into natural module clusters using spectral clustering. Uses the eigengap heuristic for automatic K selection when k=\"auto\". Returns cluster assignments with boundary symbols (nodes pulled both ways) and cut quality metrics. Useful for identifying natural module boundaries, finding tightly-coupled subsystems, or suggesting package splits.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"selector": map[string]any{
						"type":        "object",
						"description": "Selects which nodes to cluster.",
						"properties": map[string]any{
							"package": map[string]any{
								"type":        "string",
								"description": "Package path prefix to filter nodes.",
							},
							"include_subpackages": map[string]any{
								"type":        "boolean",
								"description": "Include subpackages of the given package (default true).",
							},
							"node_kinds": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Node kinds to include (e.g. type, function, method). Empty means all symbol nodes.",
							},
							"edge_kinds": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Edge kinds to consider (e.g. calls, usesType, implements). Empty means all edges.",
							},
						},
					},
					"k": map[string]any{
						"oneOf": []any{
							map[string]any{"type": "string", "enum": []string{"auto"}},
							map[string]any{"type": "integer", "minimum": 1},
						},
						"description": "Number of clusters: \"auto\" uses eigengap heuristic, or specify an integer >= 1.",
					},
					"collapse_members": map[string]any{
						"type":        "boolean",
						"description": "Contract method and field nodes into their owning type nodes, re-pointing their edges. Use this to cluster at the same type+function granularity as semantic_cluster (default false).",
					},
				},
			},
		},
		{
			Name:        "components",
			Description: "Compute connected components of a package subgraph. Returns component count, size histogram, and per-component details including the center node (highest eigenvector centrality) for each. Use this to diagnose graph shatteredness before spectral clustering — a healthy package should have one dominant component with few isolated nodes. Singletons (size-1 components) indicate missing edges.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"package": map[string]any{
						"type":        "string",
						"description": "Package path to analyze. Required.",
					},
					"include_subpackages": map[string]any{
						"type":        "boolean",
						"description": "Include subpackages of the given package (default true).",
					},
				},
				"required": []string{"package"},
			},
		},
		{
			Name:        "semantic_cluster",
			Description: "Split a package or subgraph into natural module clusters based on semantic embedding similarity. Uses the same spectral clustering core as spectral_cluster, but edges come from embedding cosine similarity (kNN graph) instead of structural dependencies. Output format is identical to spectral_cluster so you can compare where semantic and structural structure agree/disagree. Requires an embedder and indexed vectors (call refresh first if needed).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"selector": map[string]any{
						"type":        "object",
						"description": "Selects which nodes to cluster.",
						"properties": map[string]any{
							"package": map[string]any{
								"type":        "string",
								"description": "Package path prefix to filter nodes.",
							},
							"include_subpackages": map[string]any{
								"type":        "boolean",
								"description": "Include subpackages of the given package (default true).",
							},
							"node_kinds": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Node kinds to include (e.g. type, function, method). Empty means all symbol nodes.",
							},
						},
					},
					"k": map[string]any{
						"oneOf": []any{
							map[string]any{"type": "string", "enum": []string{"auto"}},
							map[string]any{"type": "integer", "minimum": 1},
						},
						"description": "Number of clusters: \"auto\" uses eigengap heuristic, or specify an integer >= 1.",
					},
					"knn": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"description": "Number of nearest neighbors for the similarity graph (default 8).",
					},
					"min_sim": map[string]any{
						"type":        "number",
						"description": "Minimum cosine similarity to create an edge (default 0.0, disabled).",
					},
				},
			},
		},
		{
			Name:        "file_hotspots",
			Description: "Find structurally overloaded source files in a package — those carrying an outlier number of top-level declarations (types + functions). Returns per-file declaration counts sorted high-to-low, the median and max, and flags outliers (>= max(3x median, 20)). Use it to spot god-files that should be split. Methods and fields are not counted — they are not file-attributed in the model.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"package": map[string]any{
						"type":        "string",
						"description": "Package path to analyze. Required.",
					},
					"include_subpackages": map[string]any{
						"type":        "boolean",
						"description": "Include subpackages of the given package (default true).",
					},
				},
				"required": []string{"package"},
			},
		},
		{
			Name:        "trophic_layers",
			Description: "Derive emergent architectural layers from dependency direction — no policy required. Solves for a trophic height per node, then reports F0 (an incoherence score in [0,1]: ~0 = cleanly layered, >0.4 = tangled), the emergent layers (level 0 = foundation, top = entry points/CLI), backward edges that point UP the hierarchy (dependency inversions — the main actionable output, sorted by how far up they reach), and cycles where layering breaks down. Use this on unfamiliar code to ask \"are there layers, and where are the inversions?\" — unlike a policy check it needs no declared rules. The edge set analyzed is fixed (directional dependency edges); there is intentionally no knob for it, so the layering stays comparable across runs.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"package": map[string]any{
						"type":        "string",
						"description": "Package path to analyze. Required.",
					},
					"include_subpackages": map[string]any{
						"type":        "boolean",
						"description": "Include subpackages of the given package (default true).",
					},
				},
				"required": []string{"package"},
			},
		},
		{
			Name:        "latent_domains",
			Description: "Detect latent domains fused by cross-cutting coupling. Clusters the same node set two ways — structurally (dependency edges) and semantically (embedding similarity) — and compares the partitions with normalized mutual information. When semantics splits into balanced domains but structure collapses into one blob, the package holds real domains glued by a shared concern; the lens names the glue (the top structural fan-in nodes, e.g. shared helpers or a god-dispatcher) so you know what to pull to a thin boundary. Verdict is aligned | diverging | latent_domains_glued. Requires an embedder and indexed vectors (call refresh first).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"selector": map[string]any{
						"type":        "object",
						"description": "Selects which nodes to analyze.",
						"properties": map[string]any{
							"package": map[string]any{
								"type":        "string",
								"description": "Package path prefix to filter nodes.",
							},
							"include_subpackages": map[string]any{
								"type":        "boolean",
								"description": "Include subpackages of the given package (default true).",
							},
							"node_kinds": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Node kinds to include. Empty means all symbol nodes.",
							},
						},
					},
					"k": map[string]any{
						"oneOf": []any{
							map[string]any{"type": "string", "enum": []string{"auto"}},
							map[string]any{"type": "integer", "minimum": 1},
						},
						"description": "Number of clusters: \"auto\" uses the eigengap heuristic on the semantic side and mirrors it on the structural side.",
					},
					"knn": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"description": "Nearest neighbors for the semantic similarity graph (default 8).",
					},
				},
			},
		},
	}
}

// Dispatch routes a tools/call invocation to the matching handler.
// Built-in tool names dispatch to their handlers directly; plugin tool
// names (the ones starting with "plugin.") are routed via the registry
// installed by SetPluginTools (M13). Unknown names surface as
// JSON-RPC method-not-found errors.
func Dispatch(state *serve.State, name string, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	switch name {
	case "extract":
		return handleExtract(state, rawArgs)
	case "list_packages":
		return handleListPackages(state)
	case "get_package":
		return handleGetPackage(state, rawArgs)
	case "lock_target":
		return handleLockTarget(state, rawArgs)
	case "list_targets":
		return handleListTargets(state)
	case "set_current_target":
		return handleSetCurrentTarget(state, rawArgs)
	case "diff":
		return handleDiff(state, rawArgs)
	case "apply_diff":
		return handleApplyDiff(state, rawArgs)
	case "validate":
		return handleValidate(state, rawArgs)
	case "list_bounded_contexts":
		return handleListBoundedContexts(state)
	case "get_bounded_context":
		return handleGetBoundedContext(state, rawArgs)
	case "search":
		return handleSearch(state, rawArgs)
	case "search_graph":
		return handleSearchGraph(state, rawArgs)
	case "expand":
		return handleExpand(state, rawArgs)
	case "get_node":
		return handleGetNode(state, rawArgs)
	case "refresh":
		return handleRefresh(state)
	case "spectral_cluster":
		return handleSpectralCluster(state, rawArgs)
	case "semantic_cluster":
		return handleSemanticCluster(state, rawArgs)
	case "components":
		return handleComponents(state, rawArgs)
	case "trophic_layers":
		return handleTrophicLayers(state, rawArgs)
	case "file_hotspots":
		return handleFileHotspots(state, rawArgs)
	case "latent_domains":
		return handleLatentDomains(state, rawArgs)
	}
	if strings.HasPrefix(name, "plugin.") {
		return dispatchPluginTool(name, rawArgs)
	}
	return ToolResult{}, &RPCError{
		Code:    ErrMethodNotFound,
		Message: fmt.Sprintf("unknown tool %q", name),
	}
}

// dispatchPluginTool resolves name against the registered plugin
// tools and invokes the matching Handler. The decoded arguments are
// passed as map[string]any (the contract every plugin Handler signs).
// Tool-level errors surface as IsError ToolResults; protocol errors
// (unknown name, malformed args) become RPCError values so the MCP
// transport can map them to JSON-RPC error responses.
func dispatchPluginTool(name string, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	for _, t := range pluginToolsSnapshot() {
		if plugin.PrefixedMCPName(t.Plugin, t.Tool.Name) != name {
			continue
		}
		var args map[string]any
		if len(rawArgs) > 0 {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return ToolResult{}, &RPCError{
					Code:    ErrInvalidParams,
					Message: fmt.Sprintf("invalid arguments: %v", err),
				}
			}
		}
		out, err := t.Tool.Handler(context.Background(), args)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		return textResult(out)
	}
	return ToolResult{}, &RPCError{
		Code:    ErrMethodNotFound,
		Message: fmt.Sprintf("unknown tool %q", name),
	}
}

// extractArgs is the input schema for the extract tool.
type extractArgs struct {
	Paths []string `json:"paths"`
}

// handleExtract returns the (optionally filtered) package list. An
// empty Paths slice means "return everything"; an unknown path is not
// an error — it just contributes nothing.
func handleExtract(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args extractArgs
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return ToolResult{}, &RPCError{
				Code:    ErrInvalidParams,
				Message: fmt.Sprintf("invalid arguments: %v", err),
			}
		}
	}

	snap := snapshotOrEmpty(state)
	out := snap.Packages
	if len(args.Paths) > 0 {
		want := make(map[string]struct{}, len(args.Paths))
		for _, p := range args.Paths {
			want[p] = struct{}{}
		}
		filtered := make([]domain.PackageModel, 0, len(args.Paths))
		for _, m := range snap.Packages {
			if _, ok := want[m.Path]; ok {
				filtered = append(filtered, m)
			}
		}
		out = filtered
	}
	if out == nil {
		out = []domain.PackageModel{}
	}
	return textResult(out)
}

// handleListPackages returns the minimal per-package summary.
func handleListPackages(state *serve.State) (ToolResult, *RPCError) {
	snap := snapshotOrEmpty(state)
	summaries := make([]PackageSummary, 0, len(snap.Packages))
	for _, m := range snap.Packages {
		summaries = append(summaries, PackageSummary{
			Path:           m.Path,
			Name:           m.Name,
			Layer:          m.Layer,
			InterfaceCount: len(m.Interfaces),
			StructCount:    len(m.Structs),
			FunctionCount:  len(m.Functions),
		})
	}
	return textResult(summaries)
}

// getPackageArgs is the input schema for the get_package tool.
type getPackageArgs struct {
	Path string `json:"path"`
}

// handleGetPackage returns a single PackageModel. Missing/empty paths
// and unknown packages come back as IsError=true tool results (not
// JSON-RPC errors) so the model can see the message and recover.
func handleGetPackage(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args getPackageArgs
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return ToolResult{}, &RPCError{
				Code:    ErrInvalidParams,
				Message: fmt.Sprintf("invalid arguments: %v", err),
			}
		}
	}
	if args.Path == "" {
		return errorResult("missing required argument: path"), nil
	}

	snap := snapshotOrEmpty(state)
	for _, m := range snap.Packages {
		if m.Path == args.Path {
			return textResult(m)
		}
	}
	return errorResult(fmt.Sprintf("package %q not found", args.Path)), nil
}

// lockTargetArgs is the input schema for the lock_target tool.
type lockTargetArgs struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// handleLockTarget freezes the current snapshot into .arch/targets/<id>/.
// The daemon's in-memory packages are first materialized as .arch/*.yaml
// under the project tree so target.Lock has something to copy — this
// mirrors what `archai target lock` does via `archai diagram generate`
// but avoids re-parsing Go sources.
func handleLockTarget(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args lockTargetArgs
	if rpcErr := unmarshalArgs(rawArgs, &args); rpcErr != nil {
		return ToolResult{}, rpcErr
	}
	if args.ID == "" {
		return errorResult("missing required argument: id"), nil
	}
	root, ok := requireRoot(state)
	if !ok {
		return errorResult("daemon state has no project root configured"), nil
	}

	snap := state.Snapshot()
	if err := writeCurrentArchYAML(snap.Packages, root); err != nil {
		return errorResult(fmt.Sprintf("error: materializing .arch/*.yaml: %v", err)), nil
	}

	if err := target.Lock(root, args.ID, target.LockOptions{Description: args.Description}); err != nil {
		return errorResult(fmt.Sprintf("error: %v", err)), nil
	}

	metas, err := target.List(root)
	if err != nil {
		return errorResult(fmt.Sprintf("error: listing targets after lock: %v", err)), nil
	}
	for _, m := range metas {
		if m.ID == args.ID {
			return textResult(m)
		}
	}
	return errorResult(fmt.Sprintf("error: locked target %q not found in list", args.ID)), nil
}

// handleListTargets returns []TargetMeta for every target under
// .arch/targets/, sorted by id.
func handleListTargets(state *serve.State) (ToolResult, *RPCError) {
	root, ok := requireRoot(state)
	if !ok {
		return errorResult("daemon state has no project root configured"), nil
	}
	metas, err := target.List(root)
	if err != nil {
		return errorResult(fmt.Sprintf("error: %v", err)), nil
	}
	if metas == nil {
		metas = []target.TargetMeta{}
	}
	return textResult(metas)
}

// setCurrentTargetArgs is the input schema for the set_current_target tool.
type setCurrentTargetArgs struct {
	ID string `json:"id"`
}

// handleSetCurrentTarget writes .arch/targets/CURRENT and syncs the
// in-memory state's currentTarget so subsequent diff/validate calls see
// the change without waiting for the watcher to pick up the file write.
func handleSetCurrentTarget(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args setCurrentTargetArgs
	if rpcErr := unmarshalArgs(rawArgs, &args); rpcErr != nil {
		return ToolResult{}, rpcErr
	}
	if args.ID == "" {
		return errorResult("missing required argument: id"), nil
	}
	root, ok := requireRoot(state)
	if !ok {
		return errorResult("daemon state has no project root configured"), nil
	}
	if err := target.Use(root, args.ID); err != nil {
		return errorResult(fmt.Sprintf("error: %v", err)), nil
	}
	if state != nil {
		_ = state.SwitchTarget(args.ID)
	}
	return textResult(map[string]any{"current": args.ID})
}

// diffArgs is the input schema for the diff tool.
type diffArgs struct {
	Target string `json:"target"`
}

// handleDiff loads current (from snapshot) and target (from disk) and
// returns the structured Diff as JSON.
func handleDiff(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args diffArgs
	if rpcErr := unmarshalArgs(rawArgs, &args); rpcErr != nil {
		return ToolResult{}, rpcErr
	}
	root, ok := requireRoot(state)
	if !ok {
		return errorResult("daemon state has no project root configured"), nil
	}

	snap := state.Snapshot()
	targetID, tErr := resolveTargetID(args.Target, snap.CurrentTarget, root)
	if tErr != "" {
		return errorResult(tErr), nil
	}

	targetModels, err := loadTargetModelsFromDisk(context.Background(), root, targetID)
	if err != nil {
		return errorResult(fmt.Sprintf("error: loading target %q: %v", targetID, err)), nil
	}

	d := diff.Compute(snap.Packages, targetModels)
	if d.Changes == nil {
		d.Changes = []diff.Change{}
	}
	return textResult(d)
}

// applyDiffArgs is the input schema for the apply_diff tool.
type applyDiffArgs struct {
	PatchYAML string `json:"patch_yaml"`
	Target    string `json:"target"`
}

// handleApplyDiff parses the patch YAML, loads current+target models,
// runs apply.Apply and rewrites the target's model/ tree on disk.
func handleApplyDiff(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args applyDiffArgs
	if rpcErr := unmarshalArgs(rawArgs, &args); rpcErr != nil {
		return ToolResult{}, rpcErr
	}
	if args.PatchYAML == "" {
		return errorResult("missing required argument: patch_yaml"), nil
	}
	root, ok := requireRoot(state)
	if !ok {
		return errorResult("daemon state has no project root configured"), nil
	}

	var patch diff.Diff
	if err := yamlv3.Unmarshal([]byte(args.PatchYAML), &patch); err != nil {
		return errorResult(fmt.Sprintf("error: parsing patch: %v", err)), nil
	}
	if err := validatePatch(&patch); err != nil {
		return errorResult(fmt.Sprintf("error: %v", err)), nil
	}

	snap := state.Snapshot()
	targetID, tErr := resolveTargetID(args.Target, snap.CurrentTarget, root)
	if tErr != "" {
		return errorResult(tErr), nil
	}

	ctx := context.Background()
	targetModels, err := loadTargetModelsFromDisk(ctx, root, targetID)
	if err != nil {
		return errorResult(fmt.Sprintf("error: loading target %q: %v", targetID, err)), nil
	}

	updated, err := apply.Apply(&patch, snap.Packages, targetModels)
	if err != nil {
		return errorResult(fmt.Sprintf("error: applying patch: %v", err)), nil
	}
	if err := writeTargetModelsToDisk(ctx, root, targetID, updated); err != nil {
		return errorResult(fmt.Sprintf("error: writing target %q: %v", targetID, err)), nil
	}
	return textResult(map[string]any{
		"target":        targetID,
		"changes":       len(patch.Changes),
		"packages_kept": len(updated),
	})
}

// validateArgs is the input schema for the validate tool.
type validateArgs struct {
	Target string `json:"target"`
}

// handleValidate returns {ok, target, violations:[...]}. ok=true when
// the current snapshot matches the target exactly.
func handleValidate(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args validateArgs
	if rpcErr := unmarshalArgs(rawArgs, &args); rpcErr != nil {
		return ToolResult{}, rpcErr
	}
	root, ok := requireRoot(state)
	if !ok {
		return errorResult("daemon state has no project root configured"), nil
	}

	snap := state.Snapshot()
	targetID, tErr := resolveTargetID(args.Target, snap.CurrentTarget, root)
	if tErr != "" {
		return errorResult(tErr), nil
	}

	targetModels, err := loadTargetModelsFromDisk(context.Background(), root, targetID)
	if err != nil {
		return errorResult(fmt.Sprintf("error: loading target %q: %v", targetID, err)), nil
	}

	d := diff.Compute(snap.Packages, targetModels)
	res := ValidateResult{
		OK:         d.IsEmpty(),
		Target:     targetID,
		Violations: []diff.Change{},
	}
	if !d.IsEmpty() {
		res.Violations = d.Changes
	}
	return textResult(res)
}

// handleListBoundedContexts returns the summary of every bounded context
// declared in the overlay. When no overlay is loaded (or no BCs are
// declared) an empty slice is returned — not an error — so agents don't
// have to special-case missing overlays.
func handleListBoundedContexts(state *serve.State) (ToolResult, *RPCError) {
	snap := snapshotOrEmpty(state)
	if snap.Overlay == nil || len(snap.Overlay.BoundedContexts) == 0 {
		return textResult([]BCSummary{})
	}

	names := make([]string, 0, len(snap.Overlay.BoundedContexts))
	for n := range snap.Overlay.BoundedContexts {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]BCSummary, 0, len(names))
	for _, name := range names {
		bc := snap.Overlay.BoundedContexts[name]
		aggs := bc.Aggregates
		if aggs == nil {
			aggs = []string{}
		}
		out = append(out, BCSummary{
			Name:         name,
			Description:  bc.Description,
			Relationship: bc.Relationship,
			Aggregates:   aggs,
			Upstream:     bc.Upstream,
			Downstream:   bc.Downstream,
		})
	}
	return textResult(out)
}

// getBCArgs is the input schema for the get_bounded_context tool.
type getBCArgs struct {
	Name string `json:"name"`
}

// bcDetail extends BCSummary with the member package paths.
type bcDetail struct {
	BCSummary
	Packages []string `json:"packages"`
}

// handleGetBoundedContext returns the full detail for a single bounded
// context, including the paths of all member packages (those whose
// Aggregate field matches one of the BC's declared aggregates).
func handleGetBoundedContext(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args getBCArgs
	if rpcErr := unmarshalArgs(rawArgs, &args); rpcErr != nil {
		return ToolResult{}, rpcErr
	}
	if args.Name == "" {
		return errorResult("missing required argument: name"), nil
	}

	snap := snapshotOrEmpty(state)
	if snap.Overlay == nil {
		return errorResult(fmt.Sprintf("bounded context %q not found: no overlay loaded", args.Name)), nil
	}
	bc, ok := snap.Overlay.BoundedContexts[args.Name]
	if !ok {
		return errorResult(fmt.Sprintf("bounded context %q not found", args.Name)), nil
	}

	// Collect member package paths.
	aggSet := make(map[string]struct{}, len(bc.Aggregates))
	for _, a := range bc.Aggregates {
		aggSet[a] = struct{}{}
	}
	var pkgPaths []string
	for _, p := range snap.Packages {
		if _, ok := aggSet[p.Aggregate]; ok {
			pkgPaths = append(pkgPaths, p.Path)
		}
	}
	sort.Strings(pkgPaths)
	if pkgPaths == nil {
		pkgPaths = []string{}
	}

	aggs := bc.Aggregates
	if aggs == nil {
		aggs = []string{}
	}
	detail := bcDetail{
		BCSummary: BCSummary{
			Name:         args.Name,
			Description:  bc.Description,
			Relationship: bc.Relationship,
			Aggregates:   aggs,
			Upstream:     bc.Upstream,
			Downstream:   bc.Downstream,
		},
		Packages: pkgPaths,
	}
	return textResult(detail)
}

// --- shared helpers ---

// snapshotOrEmpty returns a Snapshot even when state is nil, so the
// tools can be unit-tested in isolation and the daemon can answer
// requests before Load completes.
func snapshotOrEmpty(state *serve.State) serve.Snapshot {
	if state == nil {
		return serve.Snapshot{Packages: []domain.PackageModel{}}
	}
	return state.Snapshot()
}

// requireRoot returns the project root from state, or ("", false) when
// state is nil / has an empty root. Tools that touch disk need a root.
func requireRoot(state *serve.State) (string, bool) {
	if state == nil {
		return "", false
	}
	r := state.Root()
	if r == "" {
		return "", false
	}
	return r, true
}

// resolveTargetID picks the explicit arg if non-empty, otherwise falls
// back to the snapshot's current target, then to .arch/targets/CURRENT.
// Returns a non-empty error message string when no target can be resolved.
func resolveTargetID(explicit, snapshotCurrent, root string) (string, string) {
	if explicit != "" {
		return explicit, ""
	}
	if snapshotCurrent != "" {
		return snapshotCurrent, ""
	}
	// Fall back to on-disk CURRENT in case snapshot is stale (the watcher
	// hasn't caught up yet when tests write CURRENT synchronously).
	cur, err := target.Current(root)
	if err != nil {
		return "", fmt.Sprintf("error: reading CURRENT: %v", err)
	}
	if cur != "" {
		return cur, ""
	}
	return "", "error: no target specified and no CURRENT target set"
}

// unmarshalArgs decodes rawArgs into dst. Empty input leaves dst at its
// zero value; malformed JSON becomes an invalid-params RPC error.
func unmarshalArgs(rawArgs json.RawMessage, dst any) *RPCError {
	if len(rawArgs) == 0 {
		return nil
	}
	if err := json.Unmarshal(rawArgs, dst); err != nil {
		return &RPCError{
			Code:    ErrInvalidParams,
			Message: fmt.Sprintf("invalid arguments: %v", err),
		}
	}
	return nil
}

// writeCurrentArchYAML materializes each in-memory package as
// <pkg>/.arch/internal.yaml under root so target.Lock has per-package
// snapshots to freeze. Packages with an empty Path are written at
// <root>/.arch/internal.yaml (project-root package).
func writeCurrentArchYAML(packages []domain.PackageModel, root string) error {
	writer := yamlAdapter.NewWriter()
	for _, m := range packages {
		pkgDir := root
		if m.Path != "" && m.Path != "." {
			pkgDir = filepath.Join(root, filepath.FromSlash(m.Path))
		}
		out := filepath.Join(pkgDir, ".arch", "internal.yaml")
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(out), err)
		}
		if err := writer.Write(context.Background(), m, domain.WriteOptions{OutputPath: out}); err != nil {
			return fmt.Errorf("writing %s: %w", out, err)
		}
	}
	return nil
}

// loadTargetModelsFromDisk loads the frozen per-package models under
// .arch/targets/<id>/model/.
func loadTargetModelsFromDisk(ctx context.Context, root, id string) ([]domain.PackageModel, error) {
	targetDir := filepath.Join(root, ".arch", "targets", id)
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

// writeTargetModelsToDisk overwrites .arch/targets/<id>/model/ with the
// per-package models. Mirrors cmd/archai's writeTargetModels so the
// apply_diff tool produces the same on-disk layout as `archai diff apply`.
func writeTargetModelsToDisk(ctx context.Context, root, id string, models []domain.PackageModel) error {
	targetDir := filepath.Join(root, ".arch", "targets", id)
	modelDir := filepath.Join(targetDir, "model")
	if err := os.RemoveAll(modelDir); err != nil {
		return fmt.Errorf("removing %s: %w", modelDir, err)
	}
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", modelDir, err)
	}
	writer := yamlAdapter.NewWriter()
	for _, m := range models {
		sub := m.Path
		if sub == "" || sub == "." {
			sub = "."
		}
		out := filepath.Join(modelDir, filepath.FromSlash(sub), "internal.yaml")
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(out), err)
		}
		if err := writer.Write(ctx, m, domain.WriteOptions{OutputPath: out}); err != nil {
			return fmt.Errorf("writing %s: %w", out, err)
		}
	}
	return nil
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
		if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
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

// validatePatch ensures every Change in d carries a recognized Op and
// Kind so we fail fast on malformed patches before any on-disk writes.
// Duplicated from cmd/archai to avoid exporting validation from the CLI.
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

// textResult marshals payload as indented JSON and wraps it in a
// single text content block.
func textResult(payload any) (ToolResult, *RPCError) {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return ToolResult{}, &RPCError{
			Code:    ErrInternal,
			Message: fmt.Sprintf("marshalling result: %v", err),
		}
	}
	return ToolResult{
		Content: []ToolResultContent{{Type: "text", Text: string(data)}},
	}, nil
}

// errorResult wraps a human-readable message as an IsError=true tool
// result. Used for input validation failures the agent can surface.
func errorResult(msg string) ToolResult {
	return ToolResult{
		Content: []ToolResultContent{{Type: "text", Text: msg}},
		IsError: true,
	}
}

// --- Retrieval tool handlers ---

// searchArgs is the input schema for the search tool.
type searchArgs struct {
	Query   string        `json:"query"`
	K       int           `json:"k"`
	Filters searchFilters `json:"filters"`
}

type searchFilters struct {
	Kinds         []string `json:"kinds"`
	PackagePrefix string   `json:"package_prefix"`
}

// searchResult is the response structure for the search tool.
type searchResult struct {
	Results []searchResultItem `json:"results"`
	Dense   bool               `json:"dense"`
}

type searchResultItem struct {
	NodeID  string  `json:"node_id"`
	Kind    string  `json:"kind"`
	File    string  `json:"file"`
	Line    int     `json:"line"`
	Doc     string  `json:"doc"`
	Snippet string  `json:"snippet"`
	Score   float32 `json:"score"`
}

// handleSearch performs hybrid code search.
func handleSearch(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args searchArgs
	if rpcErr := unmarshalArgs(rawArgs, &args); rpcErr != nil {
		return ToolResult{}, rpcErr
	}
	if args.Query == "" {
		return errorResult("missing required argument: query"), nil
	}

	if state == nil {
		return errorResult("no state available"), nil
	}

	svc := state.Retrieval()
	if svc == nil {
		return errorResult("retrieval not initialized"), nil
	}

	k := args.K
	if k <= 0 {
		k = 10
	}

	ctx := context.Background()
	filters := retrieval.Filters{
		Kinds:         args.Filters.Kinds,
		PackagePrefix: args.Filters.PackagePrefix,
	}

	results, denseUsed, err := svc.Search(ctx, args.Query, k, filters)
	if err != nil {
		return errorResult(fmt.Sprintf("search failed: %v", err)), nil
	}

	items := make([]searchResultItem, len(results))
	for i, r := range results {
		items[i] = searchResultItem{
			NodeID:  r.NodeID,
			Kind:    r.Kind,
			File:    r.File,
			Line:    r.Line,
			Doc:     r.Doc,
			Snippet: r.Snippet,
			Score:   r.Score,
		}
	}

	return textResult(searchResult{Results: items, Dense: denseUsed})
}

// searchGraphArgs is the input schema for the search_graph tool.
type searchGraphArgs struct {
	Query string `json:"query"`
	K     int    `json:"k"`
	Hops  int    `json:"hops"`
}

// subgraphResult is the response structure for search_graph and expand tools.
type subgraphResult struct {
	Nodes []nodeInfo `json:"nodes"`
	Edges []edgeInfo `json:"edges"`
	Dense bool       `json:"dense,omitempty"`
}

type nodeInfo struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Package   string  `json:"package"`
	Name      string  `json:"name"`
	File      string  `json:"file,omitempty"`
	Line      int     `json:"line,omitempty"`
	Signature string  `json:"signature,omitempty"`
	Doc       string  `json:"doc,omitempty"`
	Score     float64 `json:"score,omitempty"`
}

type edgeInfo struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// handleSearchGraph performs search and expands results into a subgraph.
func handleSearchGraph(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args searchGraphArgs
	if rpcErr := unmarshalArgs(rawArgs, &args); rpcErr != nil {
		return ToolResult{}, rpcErr
	}
	if args.Query == "" {
		return errorResult("missing required argument: query"), nil
	}

	if state == nil {
		return errorResult("no state available"), nil
	}

	svc := state.Retrieval()
	if svc == nil {
		return errorResult("retrieval not initialized"), nil
	}

	k := args.K
	if k <= 0 {
		k = 10
	}
	hops := args.Hops
	if hops <= 0 {
		hops = 1
	}

	ctx := context.Background()
	subgraph, denseUsed, err := svc.SearchGraph(ctx, args.Query, k, hops)
	if err != nil {
		return errorResult(fmt.Sprintf("search_graph failed: %v", err)), nil
	}

	nodes := make([]nodeInfo, len(subgraph.Nodes))
	for i, n := range subgraph.Nodes {
		nodes[i] = nodeInfo{
			ID:        n.ID,
			Kind:      n.Kind,
			Package:   n.Package,
			Name:      n.Name,
			File:      n.File,
			Line:      n.Line,
			Signature: n.Signature,
			Doc:       n.Doc,
			Score:     n.Score,
		}
	}

	edges := make([]edgeInfo, len(subgraph.Edges))
	for i, e := range subgraph.Edges {
		edges[i] = edgeInfo{From: e.From, To: e.To, Kind: e.Kind}
	}

	return textResult(subgraphResult{Nodes: nodes, Edges: edges, Dense: denseUsed})
}

// expandArgs is the input schema for the expand tool.
type expandArgs struct {
	NodeIDs   []string `json:"node_ids"`
	Hops      int      `json:"hops"`
	EdgeKinds []string `json:"edges"`
}

// handleExpand expands from given nodes via graph edges.
func handleExpand(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args expandArgs
	if rpcErr := unmarshalArgs(rawArgs, &args); rpcErr != nil {
		return ToolResult{}, rpcErr
	}
	if len(args.NodeIDs) == 0 {
		return errorResult("missing required argument: node_ids"), nil
	}

	if state == nil {
		return errorResult("no state available"), nil
	}

	svc := state.Retrieval()
	if svc == nil {
		return errorResult("retrieval not initialized"), nil
	}

	hops := args.Hops
	if hops <= 0 {
		hops = 1
	}

	ctx := context.Background()
	subgraph, err := svc.Expand(ctx, args.NodeIDs, hops, args.EdgeKinds)
	if err != nil {
		return errorResult(fmt.Sprintf("expand failed: %v", err)), nil
	}

	nodes := make([]nodeInfo, len(subgraph.Nodes))
	for i, n := range subgraph.Nodes {
		nodes[i] = nodeInfo{
			ID:        n.ID,
			Kind:      n.Kind,
			Package:   n.Package,
			Name:      n.Name,
			File:      n.File,
			Line:      n.Line,
			Signature: n.Signature,
			Doc:       n.Doc,
			Score:     n.Score,
		}
	}

	edges := make([]edgeInfo, len(subgraph.Edges))
	for i, e := range subgraph.Edges {
		edges[i] = edgeInfo{From: e.From, To: e.To, Kind: e.Kind}
	}

	return textResult(subgraphResult{Nodes: nodes, Edges: edges})
}

// getNodeArgs is the input schema for the get_node tool.
type getNodeArgs struct {
	ID string `json:"id"`
}

// nodeDetailResult is the response structure for the get_node tool.
type nodeDetailResult struct {
	NodeID    string     `json:"node_id"`
	Kind      string     `json:"kind"`
	Package   string     `json:"package"`
	Name      string     `json:"name"`
	File      string     `json:"file"`
	Line      int        `json:"line"`
	Signature string     `json:"signature,omitempty"`
	Doc       string     `json:"doc,omitempty"`
	Body      string     `json:"body,omitempty"`
	Edges     []edgeInfo `json:"edges"`
}

// handleGetNode returns full detail for a single node.
func handleGetNode(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args getNodeArgs
	if rpcErr := unmarshalArgs(rawArgs, &args); rpcErr != nil {
		return ToolResult{}, rpcErr
	}
	if args.ID == "" {
		return errorResult("missing required argument: id"), nil
	}

	if state == nil {
		return errorResult("no state available"), nil
	}

	svc := state.Retrieval()
	if svc == nil {
		return errorResult("retrieval not initialized"), nil
	}

	ctx := context.Background()
	detail, err := svc.Node(ctx, args.ID)
	if err != nil {
		return errorResult(fmt.Sprintf("get_node failed: %v", err)), nil
	}

	if detail.NodeID == "" {
		return errorResult(fmt.Sprintf("node %q not found", args.ID)), nil
	}

	edges := make([]edgeInfo, len(detail.Edges))
	for i, e := range detail.Edges {
		edges[i] = edgeInfo{From: e.From, To: e.To, Kind: e.Kind}
	}

	return textResult(nodeDetailResult{
		NodeID:    detail.NodeID,
		Kind:      detail.Kind,
		Package:   detail.Package,
		Name:      detail.Name,
		File:      detail.File,
		Line:      detail.Line,
		Signature: detail.Signature,
		Doc:       detail.Doc,
		Body:      detail.Body,
		Edges:     edges,
	})
}

// refreshResult is the response structure for the refresh tool.
type refreshResult struct {
	Reindexed int  `json:"reindexed"`
	Removed   int  `json:"removed"`
	Dense     bool `json:"dense"`
}

// handleRefresh rebuilds the retrieval indexes from the current model.
func handleRefresh(state *serve.State) (ToolResult, *RPCError) {
	if state == nil {
		return errorResult("no state available"), nil
	}

	svc := state.Retrieval()
	if svc == nil {
		return errorResult("retrieval not initialized"), nil
	}

	// Get current model snapshot
	snap := state.Snapshot()
	models := snap.Packages

	// Count nodes before
	oldCount := 0
	if svc.Graph() != nil {
		oldCount = len(svc.Graph().NodesByID)
	}

	// Reindex from models
	ctx := context.Background()
	if err := svc.IndexFromModels(ctx, models); err != nil {
		return errorResult(fmt.Sprintf("refresh failed: %v", err)), nil
	}

	// Save indexes
	if err := svc.Save(); err != nil {
		return errorResult(fmt.Sprintf("save failed: %v", err)), nil
	}

	// Count nodes after
	newCount := 0
	if svc.Graph() != nil {
		newCount = len(svc.Graph().NodesByID)
	}

	// Calculate removed
	removed := 0
	if oldCount > newCount {
		removed = oldCount - newCount
	}

	return textResult(refreshResult{
		Reindexed: newCount,
		Removed:   removed,
		Dense:     svc.DenseAvailable(),
	})
}

// --- Spectral cluster tool handler ---

// spectralClusterArgs is the input schema for the spectral_cluster tool.
type spectralClusterArgs struct {
	Selector        spectralSelector `json:"selector"`
	K               any              `json:"k"`                // "auto" or integer
	CollapseMembers bool             `json:"collapse_members"` // contract method/field nodes into owning type
}

type spectralSelector struct {
	Package            string   `json:"package"`
	IncludeSubpackages *bool    `json:"include_subpackages"`
	NodeKinds          []string `json:"node_kinds"`
	EdgeKinds          []string `json:"edge_kinds"`
}

// spectralClusterResponse is the output structure for spectral_cluster.
type spectralClusterResponse struct {
	NodeCount           int                      `json:"node_count"`
	EdgeCount           int                      `json:"edge_count"`
	CutAnalysis         spectralCutAnalysis      `json:"cut_analysis"`
	Clusters            []spectralClusterInfo    `json:"clusters"`
	BoundarySymbols     []string                 `json:"boundary_symbols"`
	BoundarySymbolCount int                      `json:"boundary_symbol_count"`
	CutQuality          spectralCutQualityResult `json:"cut_quality"`
	Modularity          float64                  `json:"modularity"`            // Newman Q of the partition; ~0 = hairball
	Eigenvalues         []float64                `json:"eigenvalues,omitempty"` // smallest Laplacian eigenvalues the K was read from
}

type spectralCutAnalysis struct {
	ChosenK    int                  `json:"chosen_k"`
	KSource    string               `json:"k_source"`
	Candidates []spectralKCandidate `json:"candidates"`
}

type spectralKCandidate struct {
	K          int     `json:"k"`
	Gap        float64 `json:"gap"`        // absolute eigengap (selection metric)
	GapRatio   float64 `json:"gap_ratio"`  // ratio (context only)
	Modularity float64 `json:"modularity"` // Q of this K's partition (0 if not evaluated)
	Confidence string  `json:"confidence"`
}

type spectralClusterInfo struct {
	ID            int      `json:"id"`
	Size          int      `json:"size"`
	Members       []string `json:"members,omitempty"`        // full list when the cluster is small enough
	MembersSample []string `json:"members_sample,omitempty"` // representative sample when truncated
	Truncated     bool     `json:"truncated,omitempty"`      // true when only a sample is returned
}

// Caps that keep analysis-lens output a fixed-size summary instead of growing
// O(N) in node-id strings. Cluster membership is the product of the tool, so it
// is returned in full up to clusterMembersFullLimit (a whole-subgraph safeguard);
// boundary symbols and component dumps are always capped.
const (
	clusterMembersFullLimit = 150 // above this a cluster returns a sample, not the full list
	clusterMembersSample    = 20
	boundarySymbolLimit     = 20
	componentMembersFull    = 12 // components at/below this size return full members (the actionable islands)
	componentMembersSample  = 8
	componentListLimit      = 40 // cap the number of components echoed; the histogram still summarizes all
)

// buildClusterInfos maps archmotif cluster members to symbol IDs and applies the
// full-vs-sample cap. Shared by spectral_cluster and semantic_cluster.
func buildClusterInfos(clusters []spectralcluster.Cluster) []spectralClusterInfo {
	out := make([]spectralClusterInfo, len(clusters))
	for i, c := range clusters {
		members := mapNodeIDsToSymbols(c.Members)
		info := spectralClusterInfo{ID: c.ID, Size: len(members)}
		if len(members) <= clusterMembersFullLimit {
			info.Members = members
		} else {
			info.MembersSample = sampleStrings(members, clusterMembersSample)
			info.Truncated = true
		}
		out[i] = info
	}
	return out
}

// capBoundary maps boundary node IDs to symbols and caps to boundarySymbolLimit,
// returning the capped slice and the true total.
func capBoundary(syms []string) (capped []string, total int) {
	mapped := mapNodeIDsToSymbols(syms)
	total = len(mapped)
	if len(mapped) > boundarySymbolLimit {
		mapped = mapped[:boundarySymbolLimit]
	}
	return mapped, total
}

type spectralCutQualityResult struct {
	IntraEdges int `json:"intra_edges"`
	InterEdges int `json:"inter_edges"`
}

// handleSpectralCluster performs spectral clustering on a package/subgraph.
func handleSpectralCluster(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args spectralClusterArgs
	if rpcErr := unmarshalArgs(rawArgs, &args); rpcErr != nil {
		return ToolResult{}, rpcErr
	}

	if state == nil {
		return errorResult("no state available"), nil
	}

	snap := state.Snapshot()
	if len(snap.Packages) == 0 {
		return errorResult("no packages loaded"), nil
	}

	// Build archmotif graph from the model.
	graph, err := archmotifAdapter.ToArchmotifGraph(snap.Packages, snap.Overlay)
	if err != nil {
		return errorResult(fmt.Sprintf("building graph: %v", err)), nil
	}

	// Compute node subset matching the selector.
	nodeIDs := selectNodes(graph, snap.Packages, args.Selector)
	if len(nodeIDs) == 0 {
		return errorResult("no nodes match the selector"), nil
	}

	// Parse K.
	k := 0 // auto
	if args.K != nil {
		switch v := args.K.(type) {
		case string:
			if v != "auto" {
				return errorResult(fmt.Sprintf("invalid k value: %q (use \"auto\" or an integer)", v)), nil
			}
		case float64:
			k = int(v)
			if k < 1 {
				return errorResult("k must be >= 1"), nil
			}
		case int:
			k = v
			if k < 1 {
				return errorResult("k must be >= 1"), nil
			}
		default:
			return errorResult(fmt.Sprintf("invalid k type: %T", args.K)), nil
		}
	}

	// If collapse_members is set, contract method/field nodes into their owning types.
	clusterGraph := graph
	clusterNodeIDs := nodeIDs
	edgeCount := graph.EdgeCount()

	if args.CollapseMembers {
		collapsed, collapsedNodeIDs, collapsedEdgeCount, collapseErr := buildCollapsedGraph(graph, nodeIDs)
		if collapseErr != nil {
			return errorResult(fmt.Sprintf("collapsing members: %v", collapseErr)), nil
		}
		clusterGraph = collapsed
		clusterNodeIDs = collapsedNodeIDs
		edgeCount = collapsedEdgeCount
	}

	// Call spectral clustering.
	opts := spectralcluster.DefaultOptions()
	opts.K = k
	opts.NodeIDs = clusterNodeIDs
	opts.EdgeKinds = args.Selector.EdgeKinds

	result, err := spectralcluster.SpectralCluster(clusterGraph, opts)
	if err != nil {
		return errorResult(fmt.Sprintf("spectral clustering failed: %v", err)), nil
	}

	// Map archmotif node IDs back to archai symbol IDs (members capped, boundary capped).
	clusters := buildClusterInfos(result.Clusters)
	boundarySymbols, boundaryTotal := capBoundary(result.BoundarySymbols)

	candidates := make([]spectralKCandidate, len(result.Candidates))
	for i, c := range result.Candidates {
		candidates[i] = spectralKCandidate{
			K:          c.K,
			Gap:        c.Gap,
			GapRatio:   c.GapRatio,
			Modularity: c.Modularity,
			Confidence: c.Confidence,
		}
	}

	resp := spectralClusterResponse{
		NodeCount: len(clusterNodeIDs),
		EdgeCount: edgeCount,
		CutAnalysis: spectralCutAnalysis{
			ChosenK:    result.ChosenK,
			KSource:    result.KSource,
			Candidates: candidates,
		},
		Clusters:            clusters,
		BoundarySymbols:     boundarySymbols,
		BoundarySymbolCount: boundaryTotal,
		CutQuality: spectralCutQualityResult{
			IntraEdges: result.CutQuality.IntraEdges,
			InterEdges: result.CutQuality.InterEdges,
		},
		Modularity:  result.Modularity,
		Eigenvalues: result.Eigenvalues,
	}

	return textResult(resp)
}

// buildCollapsedGraph contracts method and field nodes into their owning type nodes.
// It re-points edges from members to their owning type, removes self-loops, and dedupes
// parallel edges. Returns the collapsed graph, surviving node IDs, edge count, and any error.
func buildCollapsedGraph(original *archmotifimport.Graph, selectedNodeIDs []string) (*archmotifimport.Graph, []string, int, error) {
	// Build maps:
	// - memberToOwner: maps method:/field: IDs to their owning type: ID
	// - survivingNodes: set of non-member node IDs that will remain
	memberToOwner := make(map[string]string)
	survivingNodes := make(map[string]bool)

	for _, id := range selectedNodeIDs {
		ownerID := memberOwnerID(id)
		if ownerID != "" {
			// This is a member node; map it to its owner
			memberToOwner[id] = ownerID
		} else {
			// Not a member node; it survives
			survivingNodes[id] = true
		}
	}

	// Collect unique packages from surviving nodes to register first.
	pkgPaths := make(map[string]bool)
	for id := range survivingNodes {
		pkgPath := extractPackagePath(id)
		pkgPaths[pkgPath] = true
	}
	sortedPkgs := make([]string, 0, len(pkgPaths))
	for p := range pkgPaths {
		sortedPkgs = append(sortedPkgs, p)
	}
	sort.Strings(sortedPkgs)

	b := archmotifimport.NewBuilder()

	// Register packages.
	for _, pkgPath := range sortedPkgs {
		if err := b.AddPackage("pkg:"+pkgPath, "", ""); err != nil {
			return nil, nil, 0, fmt.Errorf("adding package %s: %w", pkgPath, err)
		}
	}

	// Add surviving nodes (types and functions).
	sortedSurviving := make([]string, 0, len(survivingNodes))
	for id := range survivingNodes {
		sortedSurviving = append(sortedSurviving, id)
	}
	sort.Strings(sortedSurviving)

	for _, id := range sortedSurviving {
		pkgPath := extractPackagePath(id)
		pkgID := "pkg:" + pkgPath
		if strings.HasPrefix(id, "fn:") {
			if err := b.AddFunction(id, pkgID); err != nil {
				return nil, nil, 0, fmt.Errorf("adding function %s: %w", id, err)
			}
		} else {
			// type:, const:, var:, etc. - add as type node
			if err := b.AddType(id, pkgID, false, ""); err != nil {
				return nil, nil, 0, fmt.Errorf("adding type %s: %w", id, err)
			}
		}
	}

	// Collect edges from original graph, re-pointing member endpoints to their owners.
	// Dedupe and remove self-loops.
	type edgeKey struct {
		from, to string
		kind     string // EdgeKind is a string
	}
	seenEdges := make(map[edgeKey]bool)
	var edgesToAdd []edgeKey

	// Build a set of selected node IDs for quick lookup
	selectedSet := make(map[string]bool, len(selectedNodeIDs))
	for _, id := range selectedNodeIDs {
		selectedSet[id] = true
	}

	for _, e := range original.Edges() {
		// Only process edges where both endpoints are in our selection
		if !selectedSet[e.From] || !selectedSet[e.To] {
			continue
		}

		fromID := e.From
		toID := e.To

		// Re-point member nodes to their owners
		if owner, ok := memberToOwner[fromID]; ok {
			fromID = owner
		}
		if owner, ok := memberToOwner[toID]; ok {
			toID = owner
		}

		// Skip self-loops
		if fromID == toID {
			continue
		}

		// Skip if either endpoint is not a surviving node
		if !survivingNodes[fromID] || !survivingNodes[toID] {
			continue
		}

		// Dedupe
		key := edgeKey{from: fromID, to: toID, kind: string(e.Kind)}
		if seenEdges[key] {
			continue
		}
		seenEdges[key] = true
		edgesToAdd = append(edgesToAdd, key)
	}

	// Sort edges for deterministic output
	sort.Slice(edgesToAdd, func(i, j int) bool {
		if edgesToAdd[i].from != edgesToAdd[j].from {
			return edgesToAdd[i].from < edgesToAdd[j].from
		}
		if edgesToAdd[i].to != edgesToAdd[j].to {
			return edgesToAdd[i].to < edgesToAdd[j].to
		}
		return string(edgesToAdd[i].kind) < string(edgesToAdd[j].kind)
	})

	// Add edges to builder.
	// Map edge kind strings back to archmotifimport.DependencyKind.
	// Skip structural edges (contains, implements) which have dedicated methods.
	for _, e := range edgesToAdd {
		depKind := edgeKindToDependencyKind(e.kind)
		if depKind == "" {
			// Skip structural edges that can't be added via AddDependency
			continue
		}
		if err := b.AddDependency(e.from, e.to, depKind); err != nil {
			// Skip errors (e.g., duplicate edges from asymmetric views)
			continue
		}
	}

	g, err := b.Build()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("building collapsed graph: %w", err)
	}

	return g, sortedSurviving, len(edgesToAdd), nil
}

// memberOwnerID returns the owning type ID for a method or field node ID,
// or empty string if the node is not a member.
// method:pkg.Recv.Method -> type:pkg.Recv
// field:pkg.Struct.Field -> type:pkg.Struct
func memberOwnerID(id string) string {
	switch {
	case strings.HasPrefix(id, "method:"):
		// method:internal/domain.StructName.MethodName -> type:internal/domain.StructName
		rest := strings.TrimPrefix(id, "method:")
		// Find the last dot (before method name) and second-to-last dot (before receiver name)
		lastDot := strings.LastIndex(rest, ".")
		if lastDot < 0 {
			return ""
		}
		// rest[:lastDot] is "internal/domain.StructName"
		return "type:" + rest[:lastDot]

	case strings.HasPrefix(id, "field:"):
		// field:internal/domain.StructName.FieldName -> type:internal/domain.StructName
		rest := strings.TrimPrefix(id, "field:")
		lastDot := strings.LastIndex(rest, ".")
		if lastDot < 0 {
			return ""
		}
		return "type:" + rest[:lastDot]
	}
	return ""
}

// edgeKindToDependencyKind maps archmotif EdgeKind strings to archmotifimport.DependencyKind.
// Returns empty string for structural edges (contains, implements) that can't be added via AddDependency.
func edgeKindToDependencyKind(kind string) archmotifimport.DependencyKind {
	switch kind {
	case "dependsOn":
		return archmotifimport.DependencyDependsOn
	case "calls":
		return archmotifimport.DependencyCalls
	case "callsFrom":
		return archmotifimport.DependencyCallsFrom
	case "references":
		return archmotifimport.DependencyReferences
	case "embeds":
		return archmotifimport.DependencyEmbeds
	case "returns":
		return archmotifimport.DependencyReturns
	case "usesType":
		return archmotifimport.DependencyUsesType
	case "contains", "implements":
		// Structural edges - skip, they have dedicated AddContains/AddImplements methods
		return ""
	default:
		return ""
	}
}

// --- Semantic cluster tool handler ---

// semanticClusterArgs is the input schema for the semantic_cluster tool.
type semanticClusterArgs struct {
	Selector spectralSelector `json:"selector"`
	K        any              `json:"k"`       // "auto" or integer
	KNN      int              `json:"knn"`     // k nearest neighbors for similarity graph
	MinSim   float64          `json:"min_sim"` // minimum similarity threshold
}

// semanticClusterResponse extends spectralClusterResponse with dropped node info.
type semanticClusterResponse struct {
	spectralClusterResponse
	DroppedNodes int `json:"dropped_nodes"` // nodes without embeddings
}

// handleSemanticCluster performs spectral clustering on a semantic similarity graph.
func handleSemanticCluster(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args semanticClusterArgs
	if rpcErr := unmarshalArgs(rawArgs, &args); rpcErr != nil {
		return ToolResult{}, rpcErr
	}

	if state == nil {
		return errorResult("no state available"), nil
	}

	// Get retrieval service for vector access
	svc := state.Retrieval()
	if svc == nil {
		return errorResult("retrieval not initialized — call refresh first"), nil
	}

	vidx := svc.VectorIndexWithLookup()
	if vidx == nil {
		return errorResult("vector index not available — embedder may not be configured or refresh needed"), nil
	}

	snap := state.Snapshot()
	if len(snap.Packages) == 0 {
		return errorResult("no packages loaded"), nil
	}

	// Build archmotif graph to use selectNodes for consistent filtering
	graph, err := archmotifAdapter.ToArchmotifGraph(snap.Packages, snap.Overlay)
	if err != nil {
		return errorResult(fmt.Sprintf("building graph: %v", err)), nil
	}

	// Get selected archmotif node IDs
	archmotifNodeIDs := selectNodes(graph, snap.Packages, args.Selector)
	if len(archmotifNodeIDs) == 0 {
		return errorResult("no nodes match the selector"), nil
	}

	// Map archmotif node IDs to retrieval index keys and collect vectors.
	// Archmotif IDs: type:pkg.Name, fn:pkg.Name, method:pkg.Recv.Method, field:pkg.Struct.Field
	// Retrieval IDs: pkg.Name (no prefix)
	var nodesWithVectors []semanticNode
	droppedCount := 0

	for _, amid := range archmotifNodeIDs {
		rid := archmotifIDToRetrievalID(amid)
		if rid == "" {
			droppedCount++
			continue
		}
		vec, ok := vidx.Vector(rid)
		if !ok || len(vec) == 0 {
			droppedCount++
			continue
		}
		nodesWithVectors = append(nodesWithVectors, semanticNode{
			archmotifID: amid,
			retrievalID: rid,
			vec:         vec,
		})
	}

	if len(nodesWithVectors) < 2 {
		return errorResult(fmt.Sprintf("only %d nodes have embeddings (need at least 2); %d dropped",
			len(nodesWithVectors), droppedCount)), nil
	}

	// Apply defaults
	knn := args.KNN
	if knn < 1 {
		knn = 8
	}
	minSim := args.MinSim // default 0.0 (no floor)

	// Build semantic kNN graph using archmotifimport.Builder
	semanticGraph, edgeCount, err := buildSemanticKNNGraph(nodesWithVectors, knn, minSim)
	if err != nil {
		return errorResult(fmt.Sprintf("building semantic graph: %v", err)), nil
	}

	// Collect surviving node IDs for spectral clustering
	survivingNodeIDs := make([]string, len(nodesWithVectors))
	for i, nv := range nodesWithVectors {
		survivingNodeIDs[i] = nv.archmotifID
	}

	// Parse K
	k := 0 // auto
	if args.K != nil {
		switch v := args.K.(type) {
		case string:
			if v != "auto" {
				return errorResult(fmt.Sprintf("invalid k value: %q (use \"auto\" or an integer)", v)), nil
			}
		case float64:
			k = int(v)
			if k < 1 {
				return errorResult("k must be >= 1"), nil
			}
		case int:
			k = v
			if k < 1 {
				return errorResult("k must be >= 1"), nil
			}
		default:
			return errorResult(fmt.Sprintf("invalid k type: %T", args.K)), nil
		}
	}

	// Call spectral clustering on the semantic graph.
	// We use "references" edges for semantic similarity since archmotifimport
	// only accepts known DependencyKind values (dependsOn, calls, references,
	// embeds, returns, usesType). "references" is semantically closest to
	// "these symbols are related by meaning".
	opts := spectralcluster.DefaultOptions()
	opts.K = k
	opts.NodeIDs = survivingNodeIDs
	opts.EdgeKinds = []string{"references"} // semantic kNN edges use references kind

	result, err := spectralcluster.SpectralCluster(semanticGraph, opts)
	if err != nil {
		return errorResult(fmt.Sprintf("spectral clustering failed: %v", err)), nil
	}

	// Map archmotif node IDs back to symbols (same caps as spectral_cluster).
	clusters := buildClusterInfos(result.Clusters)
	boundarySymbols, boundaryTotal := capBoundary(result.BoundarySymbols)

	candidates := make([]spectralKCandidate, len(result.Candidates))
	for i, c := range result.Candidates {
		candidates[i] = spectralKCandidate{
			K:          c.K,
			Gap:        c.Gap,
			GapRatio:   c.GapRatio,
			Modularity: c.Modularity,
			Confidence: c.Confidence,
		}
	}

	resp := semanticClusterResponse{
		spectralClusterResponse: spectralClusterResponse{
			NodeCount: len(survivingNodeIDs),
			EdgeCount: edgeCount,
			CutAnalysis: spectralCutAnalysis{
				ChosenK:    result.ChosenK,
				KSource:    result.KSource,
				Candidates: candidates,
			},
			Clusters:            clusters,
			BoundarySymbols:     boundarySymbols,
			BoundarySymbolCount: boundaryTotal,
			CutQuality: spectralCutQualityResult{
				IntraEdges: result.CutQuality.IntraEdges,
				InterEdges: result.CutQuality.InterEdges,
			},
			Modularity:  result.Modularity,
			Eigenvalues: result.Eigenvalues,
		},
		DroppedNodes: droppedCount,
	}

	return textResult(resp)
}

// archmotifIDToRetrievalID converts an archmotif node ID to a retrieval index key.
// Archmotif IDs have kind prefixes (type:, fn:, method:, field:, pkg:, file:).
// Retrieval IDs are just "pkg.SymbolName" with no prefix.
func archmotifIDToRetrievalID(amid string) string {
	switch {
	case strings.HasPrefix(amid, "type:"):
		// type:internal/domain.PackageModel -> internal/domain.PackageModel
		return strings.TrimPrefix(amid, "type:")
	case strings.HasPrefix(amid, "fn:"):
		// fn:internal/service.Generate -> internal/service.Generate
		return strings.TrimPrefix(amid, "fn:")
	case strings.HasPrefix(amid, "method:"):
		// method:internal/domain.StructName.MethodName -> not indexed in retrieval
		// Retrieval indexes structs/interfaces, not individual methods
		return ""
	case strings.HasPrefix(amid, "field:"):
		// field:internal/domain.StructName.FieldName -> not indexed in retrieval
		return ""
	case strings.HasPrefix(amid, "pkg:"):
		// pkg:internal/domain -> packages are not indexed in retrieval
		return ""
	case strings.HasPrefix(amid, "file:"):
		// file:internal/domain/model.go -> files are not indexed in retrieval
		return ""
	}
	return ""
}

// semanticNode holds an archmotif node ID with its retrieval ID and embedding vector.
type semanticNode struct {
	archmotifID string
	retrievalID string
	vec         []float32
}

// buildSemanticKNNGraph constructs an archmotif graph with semantic edges.
// Each node gets edges to its top-k most similar neighbors (by cosine similarity).
// Returns the graph and the edge count.
func buildSemanticKNNGraph(nodes []semanticNode, knn int, minSim float64) (*archmotifimport.Graph, int, error) {
	b := archmotifimport.NewBuilder()

	// Pass 1: Collect unique packages and register them first.
	// archmotifimport.Builder requires parent package nodes to exist
	// before AddType/AddFunction can reference them.
	pkgPaths := make(map[string]bool)
	for _, n := range nodes {
		pkgPath := extractPackagePath(n.archmotifID)
		pkgPaths[pkgPath] = true
	}
	// Sort for deterministic output.
	sortedPkgs := make([]string, 0, len(pkgPaths))
	for p := range pkgPaths {
		sortedPkgs = append(sortedPkgs, p)
	}
	sort.Strings(sortedPkgs)
	for _, pkgPath := range sortedPkgs {
		if err := b.AddPackage("pkg:"+pkgPath, "", ""); err != nil {
			return nil, 0, fmt.Errorf("adding package %s: %w", pkgPath, err)
		}
	}

	// Pass 2: Add all symbol nodes (types/functions).
	// archmotif expects typed nodes; we add them as type nodes with empty role.
	for _, n := range nodes {
		pkgPath := extractPackagePath(n.archmotifID)
		pkgID := "pkg:" + pkgPath
		// Determine whether this is a function or a type based on the ID prefix.
		if strings.HasPrefix(n.archmotifID, "fn:") {
			if err := b.AddFunction(n.archmotifID, pkgID); err != nil {
				return nil, 0, fmt.Errorf("adding node %s: %w", n.archmotifID, err)
			}
		} else {
			// type:, method:, field: - add as type node
			if err := b.AddType(n.archmotifID, pkgID, false, ""); err != nil {
				return nil, 0, fmt.Errorf("adding node %s: %w", n.archmotifID, err)
			}
		}
	}

	// Compute pairwise cosine similarities and build kNN edges
	edgeCount := 0
	for i, ni := range nodes {
		// Compute similarity to all other nodes
		type simPair struct {
			idx int
			sim float64
		}
		var similarities []simPair
		for j, nj := range nodes {
			if i == j {
				continue
			}
			sim := cosineSimilarity64(ni.vec, nj.vec)
			if sim >= minSim {
				similarities = append(similarities, simPair{idx: j, sim: sim})
			}
		}

		// Sort by similarity descending
		sort.Slice(similarities, func(a, b int) bool {
			return similarities[a].sim > similarities[b].sim
		})

		// Take top-k neighbors
		limit := knn
		if limit > len(similarities) {
			limit = len(similarities)
		}

		for _, sp := range similarities[:limit] {
			nj := nodes[sp.idx]
			// Add edge with "references" kind (archmotifimport's valid kind for
			// semantic similarity; the underlying meaning is "these symbols are
			// semantically related").
			// Note: directed edge from ni to nj; the graph's undirected view will symmetrize.
			if err := b.AddDependency(ni.archmotifID, nj.archmotifID, archmotifimport.DependencyReferences); err != nil {
				// Edge may already exist from the reverse direction; skip duplicates
				continue
			}
			edgeCount++
		}
	}

	g, err := b.Build()
	return g, edgeCount, err
}

// cosineSimilarity64 computes cosine similarity between two float32 vectors,
// returning a float64 result.
func cosineSimilarity64(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// selectNodes returns archmotif node IDs matching the selector.
func selectNodes(graph *spectralcluster.Graph, packages []domain.PackageModel, sel spectralSelector) []string {
	// Build a set of package paths matching the selector.
	includeSubpkgs := true
	if sel.IncludeSubpackages != nil {
		includeSubpkgs = *sel.IncludeSubpackages
	}

	matchingPkgs := map[string]bool{}
	for _, pkg := range packages {
		if sel.Package == "" {
			matchingPkgs[pkg.Path] = true
		} else if pkg.Path == sel.Package {
			matchingPkgs[pkg.Path] = true
		} else if includeSubpkgs && strings.HasPrefix(pkg.Path, sel.Package+"/") {
			matchingPkgs[pkg.Path] = true
		}
	}

	// Build node kind filter.
	nodeKindFilter := map[string]bool{}
	for _, k := range sel.NodeKinds {
		nodeKindFilter[k] = true
	}

	// Collect matching node IDs.
	// Archmotif node IDs have format: "pkg:<path>", "type:<path>.<Name>",
	// "fn:<path>.<Name>", "method:<path>.<RecvName>.<MethodName>",
	// "field:<path>.<StructName>.<FieldName>".
	var nodeIDs []string
	for _, n := range graph.Nodes() {
		// Parse the node ID to extract package path.
		pkgPath := extractPackagePath(n.ID)
		if !matchingPkgs[pkgPath] {
			continue
		}

		// Filter by node kind.
		if len(nodeKindFilter) > 0 {
			kindStr := string(n.Kind)
			if !nodeKindFilter[kindStr] {
				continue
			}
		}

		// Exclude package and file containers by default (we want
		// symbol-level clustering, not the structural layout).
		if (n.Kind == "package" || n.Kind == "file") && len(sel.NodeKinds) == 0 {
			continue
		}

		nodeIDs = append(nodeIDs, n.ID)
	}

	sort.Strings(nodeIDs)
	return nodeIDs
}

// extractPackagePath extracts the package path from an archmotif node ID.
func extractPackagePath(id string) string {
	// ID formats:
	//   pkg:<path>             -> <path>
	//   type:<path>.<Name>     -> <path>
	//   fn:<path>.<Name>       -> <path>
	//   method:<path>.<Recv>.<Method> -> <path>
	//   field:<path>.<Struct>.<Field> -> <path>
	switch {
	case strings.HasPrefix(id, "pkg:"):
		return strings.TrimPrefix(id, "pkg:")
	case strings.HasPrefix(id, "type:"):
		return extractPathBeforeLastDot(strings.TrimPrefix(id, "type:"))
	case strings.HasPrefix(id, "fn:"):
		return extractPathBeforeLastDot(strings.TrimPrefix(id, "fn:"))
	case strings.HasPrefix(id, "method:"):
		// method:<path>.<Recv>.<Method> - need to strip last two segments
		rest := strings.TrimPrefix(id, "method:")
		return extractPathBeforeLastNDots(rest, 2)
	case strings.HasPrefix(id, "field:"):
		// field:<path>.<Struct>.<Field> - need to strip last two segments
		rest := strings.TrimPrefix(id, "field:")
		return extractPathBeforeLastNDots(rest, 2)
	case strings.HasPrefix(id, "file:"):
		// file:<path>/<basename> -> strip the trailing /<basename>
		rest := strings.TrimPrefix(id, "file:")
		if i := strings.LastIndex(rest, "/"); i >= 0 {
			return rest[:i]
		}
		return rest
	}
	return ""
}

func extractPathBeforeLastDot(s string) string {
	idx := strings.LastIndex(s, ".")
	if idx < 0 {
		return s
	}
	return s[:idx]
}

func extractPathBeforeLastNDots(s string, n int) string {
	for i := 0; i < n; i++ {
		idx := strings.LastIndex(s, ".")
		if idx < 0 {
			return s
		}
		s = s[:idx]
	}
	return s
}

// mapNodeIDsToSymbols converts archmotif node IDs to archai symbol IDs.
// For now, we just return the archmotif IDs as-is since they are human-readable.
func mapNodeIDsToSymbols(nodeIDs []string) []string {
	// The archmotif IDs are already descriptive:
	//   type:internal/domain.PackageModel
	//   fn:internal/service.Generate
	// So we return them directly.
	out := make([]string, len(nodeIDs))
	copy(out, nodeIDs)
	return out
}

// --- components tool ---

type componentsArgs struct {
	Package            string `json:"package"`
	IncludeSubpackages *bool  `json:"include_subpackages,omitempty"`
}

type componentInfo struct {
	Size          int      `json:"size"`
	Center        string   `json:"center"`
	Centrality    float64  `json:"centrality"`
	Members       []string `json:"members,omitempty"`        // full list only for small components (the actionable islands)
	MembersSample []string `json:"members_sample,omitempty"` // representative sample for large components
}

type componentsResponse struct {
	NodeCount       int             `json:"node_count"`
	EdgeCount       int             `json:"edge_count"`
	ComponentCount  int             `json:"component_count"`
	SizeHistogram   map[int]int     `json:"size_histogram"`
	Components      []componentInfo `json:"components"`
	ComponentsTrunc bool            `json:"components_truncated,omitempty"` // true when only the largest components are echoed
}

func handleComponents(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args componentsArgs
	if rpcErr := unmarshalArgs(rawArgs, &args); rpcErr != nil {
		return ToolResult{}, rpcErr
	}

	if args.Package == "" {
		return errorResult("package is required"), nil
	}

	if state == nil {
		return errorResult("no state available"), nil
	}

	snap := state.Snapshot()
	if len(snap.Packages) == 0 {
		return errorResult("no packages loaded"), nil
	}

	// Build archmotif graph from the model.
	graph, err := archmotifAdapter.ToArchmotifGraph(snap.Packages, snap.Overlay)
	if err != nil {
		return errorResult(fmt.Sprintf("building graph: %v", err)), nil
	}

	// Select nodes matching the package filter.
	includeSubpkgs := true
	if args.IncludeSubpackages != nil {
		includeSubpkgs = *args.IncludeSubpackages
	}

	matchingPkgs := map[string]bool{}
	for _, pkg := range snap.Packages {
		if pkg.Path == args.Package {
			matchingPkgs[pkg.Path] = true
		} else if includeSubpkgs && strings.HasPrefix(pkg.Path, args.Package+"/") {
			matchingPkgs[pkg.Path] = true
		}
	}

	if len(matchingPkgs) == 0 {
		return errorResult(fmt.Sprintf("no packages match %q", args.Package)), nil
	}

	// Collect all symbol-level node IDs (exclude package nodes).
	var nodeIDs []string
	for _, n := range graph.Nodes() {
		pkgPath := extractPackagePath(n.ID)
		if !matchingPkgs[pkgPath] {
			continue
		}
		// Exclude package and external nodes.
		if n.Kind == "package" || n.Kind == "external" || n.Kind == "file" {
			continue
		}
		nodeIDs = append(nodeIDs, n.ID)
	}
	sort.Strings(nodeIDs)

	if len(nodeIDs) == 0 {
		return errorResult("no symbol nodes in the selected packages"), nil
	}

	// Call the components analysis.
	result := archmotifComponents.Analyze(graph, nodeIDs)

	// Build response. Components are echoed largest-first and capped, so the
	// payload stays a fixed-size summary instead of dumping every member of a
	// giant component. Small components return full members (the orphan islands
	// you actually act on); large ones return a sample plus their size.
	sorted := make([]archmotifComponents.Component, len(result.Components))
	copy(sorted, result.Components)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Size > sorted[j].Size })

	truncated := false
	if len(sorted) > componentListLimit {
		sorted = sorted[:componentListLimit]
		truncated = true
	}

	components := make([]componentInfo, len(sorted))
	for i, c := range sorted {
		info := componentInfo{
			Size:       c.Size,
			Center:     c.CenterNodeID,
			Centrality: c.Centrality,
		}
		if c.Size <= componentMembersFull {
			info.Members = c.Members
		} else {
			info.MembersSample = sampleStrings(c.Members, componentMembersSample)
		}
		components[i] = info
	}

	resp := componentsResponse{
		NodeCount:       result.NodeCount,
		EdgeCount:       result.EdgeCount,
		ComponentCount:  result.ComponentCount,
		SizeHistogram:   result.SizeHistogram,
		Components:      components,
		ComponentsTrunc: truncated,
	}

	return textResult(resp)
}

type trophicLayersArgs struct {
	Package            string `json:"package"`
	IncludeSubpackages *bool  `json:"include_subpackages,omitempty"`
}

type trophicCoherence struct {
	F0      float64 `json:"f0"`
	Verdict string  `json:"verdict"`
}

type trophicLayerInfo struct {
	Level  int      `json:"level"`
	Size   int      `json:"size"`
	Center string   `json:"center"`
	Sample []string `json:"sample,omitempty"`
}

type trophicCycleInfo struct {
	Size          int      `json:"size"`
	Center        string   `json:"center"`
	MembersSample []string `json:"members_sample,omitempty"`
}

type trophicBackwardEdge struct {
	From string  `json:"from"`
	To   string  `json:"to"`
	Span float64 `json:"span"`
}

type trophicLayersResponse struct {
	EdgeKindsUsed     []string              `json:"edge_kinds_used"`
	NodeCount         int                   `json:"node_count"`
	EdgeCount         int                   `json:"edge_count"`
	Coherence         trophicCoherence      `json:"coherence"`
	LayerCount        int                   `json:"layer_count"`
	Layers            []trophicLayerInfo    `json:"layers"`
	Cycles            []trophicCycleInfo    `json:"cycles,omitempty"`
	BackwardEdges     []trophicBackwardEdge `json:"backward_edges,omitempty"`
	BackwardEdgeCount int                   `json:"backward_edge_count"`
}

// trophicVerdict maps the incoherence F0 to a human-readable verdict.
func trophicVerdict(f0 float64) string {
	switch {
	case f0 < 0.05:
		return "layered"
	case f0 < 0.25:
		return "mostly_layered"
	case f0 < 0.45:
		return "partially_layered"
	default:
		return "tangled"
	}
}

func handleTrophicLayers(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args trophicLayersArgs
	if rpcErr := unmarshalArgs(rawArgs, &args); rpcErr != nil {
		return ToolResult{}, rpcErr
	}

	if args.Package == "" {
		return errorResult("package is required"), nil
	}
	if state == nil {
		return errorResult("no state available"), nil
	}

	snap := state.Snapshot()
	if len(snap.Packages) == 0 {
		return errorResult("no packages loaded"), nil
	}

	graph, err := archmotifAdapter.ToArchmotifGraph(snap.Packages, snap.Overlay)
	if err != nil {
		return errorResult(fmt.Sprintf("building graph: %v", err)), nil
	}

	includeSubpkgs := true
	if args.IncludeSubpackages != nil {
		includeSubpkgs = *args.IncludeSubpackages
	}
	matchingPkgs := map[string]bool{}
	for _, pkg := range snap.Packages {
		if pkg.Path == args.Package {
			matchingPkgs[pkg.Path] = true
		} else if includeSubpkgs && strings.HasPrefix(pkg.Path, args.Package+"/") {
			matchingPkgs[pkg.Path] = true
		}
	}
	if len(matchingPkgs) == 0 {
		return errorResult(fmt.Sprintf("no packages match %q", args.Package)), nil
	}

	var nodeIDs []string
	for _, n := range graph.Nodes() {
		if !matchingPkgs[extractPackagePath(n.ID)] {
			continue
		}
		// trophic_layers is a behavioral (flow) analysis. Structural kinds
		// carry no flow edges by construction — package/file are containers,
		// and a field's type-coupling is recorded on its owning struct, not on
		// the field node — so they would only pile up at height 0 as a spurious
		// foundation layer. Restrict to behavioral nodes (type/fn/method).
		if n.Kind == "package" || n.Kind == "external" || n.Kind == "file" || n.Kind == "field" {
			continue
		}
		nodeIDs = append(nodeIDs, n.ID)
	}
	sort.Strings(nodeIDs)
	if len(nodeIDs) == 0 {
		return errorResult("no symbol nodes in the selected packages"), nil
	}

	result := archmotifTrophic.Analyze(graph, archmotifTrophic.Options{NodeIDs: nodeIDs})

	const (
		layerSampleLimit  = 8
		cycleSampleLimit  = 8
		backwardEdgeLimit = 25
	)

	layers := make([]trophicLayerInfo, len(result.Layers))
	for i, l := range result.Layers {
		layers[i] = trophicLayerInfo{
			Level:  l.Level,
			Size:   l.Size,
			Center: l.Center,
			Sample: sampleStrings(l.Members, layerSampleLimit),
		}
	}

	cycles := make([]trophicCycleInfo, len(result.Cycles))
	for i, c := range result.Cycles {
		cycles[i] = trophicCycleInfo{
			Size:          c.Size,
			Center:        c.Center,
			MembersSample: sampleStrings(c.Members, cycleSampleLimit),
		}
	}

	backward := make([]trophicBackwardEdge, 0, len(result.BackwardEdges))
	for i, be := range result.BackwardEdges {
		if i >= backwardEdgeLimit {
			break
		}
		backward = append(backward, trophicBackwardEdge{From: be.From, To: be.To, Span: be.Span})
	}

	resp := trophicLayersResponse{
		EdgeKindsUsed: result.EdgeKindsUsed,
		NodeCount:     result.NodeCount,
		EdgeCount:     result.EdgeCount,
		Coherence: trophicCoherence{
			F0:      result.IncoherenceF0,
			Verdict: trophicVerdict(result.IncoherenceF0),
		},
		LayerCount:        result.LayerCount,
		Layers:            layers,
		Cycles:            cycles,
		BackwardEdges:     backward,
		BackwardEdgeCount: result.BackwardEdgeCount,
	}

	return textResult(resp)
}

// sampleStrings returns at most limit elements of s (a representative sample).
func sampleStrings(s []string, limit int) []string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}

type fileHotspotsArgs struct {
	Package            string `json:"package"`
	IncludeSubpackages *bool  `json:"include_subpackages,omitempty"`
}

type fileHotspotInfo struct {
	File        string `json:"file"`
	SymbolCount int    `json:"symbol_count"`
	Outlier     bool   `json:"outlier"`
}

type fileHotspotsResponse struct {
	FileCount     int               `json:"file_count"`
	TotalSymbols  int               `json:"total_symbols"`
	MedianSymbols float64           `json:"median_symbols"`
	MaxSymbols    int               `json:"max_symbols"`
	OutlierCount  int               `json:"outlier_count"`
	Files         []fileHotspotInfo `json:"files"`
}

func handleFileHotspots(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args fileHotspotsArgs
	if rpcErr := unmarshalArgs(rawArgs, &args); rpcErr != nil {
		return ToolResult{}, rpcErr
	}
	if args.Package == "" {
		return errorResult("package is required"), nil
	}
	if state == nil {
		return errorResult("no state available"), nil
	}

	snap := state.Snapshot()
	if len(snap.Packages) == 0 {
		return errorResult("no packages loaded"), nil
	}

	graph, err := archmotifAdapter.ToArchmotifGraph(snap.Packages, snap.Overlay)
	if err != nil {
		return errorResult(fmt.Sprintf("building graph: %v", err)), nil
	}

	includeSubpkgs := true
	if args.IncludeSubpackages != nil {
		includeSubpkgs = *args.IncludeSubpackages
	}
	matchingPkgs := map[string]bool{}
	for _, pkg := range snap.Packages {
		if pkg.Path == args.Package {
			matchingPkgs[pkg.Path] = true
		} else if includeSubpkgs && strings.HasPrefix(pkg.Path, args.Package+"/") {
			matchingPkgs[pkg.Path] = true
		}
	}
	if len(matchingPkgs) == 0 {
		return errorResult(fmt.Sprintf("no packages match %q", args.Package)), nil
	}

	// Select file nodes belonging to the matching packages.
	var fileIDs []string
	for _, n := range graph.Nodes() {
		if n.Kind != "file" {
			continue
		}
		if matchingPkgs[extractPackagePath(n.ID)] {
			fileIDs = append(fileIDs, n.ID)
		}
	}
	if len(fileIDs) == 0 {
		return errorResult("no file nodes in the selected packages"), nil
	}

	result := archmotifFilestats.Analyze(graph, archmotifFilestats.Options{NodeIDs: fileIDs})

	// Cap the per-file list to the heaviest files; the summary keeps the
	// full counts.
	const fileLimit = 30
	files := make([]fileHotspotInfo, 0, len(result.Files))
	for i, f := range result.Files {
		if i >= fileLimit {
			break
		}
		files = append(files, fileHotspotInfo{File: f.File, SymbolCount: f.SymbolCount, Outlier: f.Outlier})
	}

	resp := fileHotspotsResponse{
		FileCount:     result.FileCount,
		TotalSymbols:  result.TotalSymbols,
		MedianSymbols: result.MedianSymbols,
		MaxSymbols:    result.MaxSymbols,
		OutlierCount:  result.OutlierCount,
		Files:         files,
	}
	return textResult(resp)
}
