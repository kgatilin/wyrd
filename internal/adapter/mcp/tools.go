package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	yamlAdapter "github.com/kgatilin/archai/internal/adapter/yaml"
	"github.com/kgatilin/archai/internal/apply"
	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/plugin"
	"github.com/kgatilin/archai/internal/serve"
	"github.com/kgatilin/archai/internal/target"
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
