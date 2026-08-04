// Package events is the event-model plugin that validates and visualizes
// declarative event-driven architecture from .arch/events.yaml files.
// It implements the CLI, MCP, and HTTP surfaces defined in the event-model
// design doc (section 5).
//
// Reading strategy (v1): declarations are read and composed fresh on each
// request, with no caching or watching. This is the simplest correct approach;
// caching adds complexity (invalidation, staleness) that is premature for a
// feature proving its value. The cost is acceptable: the typical repo has
// few event declarations, and reading YAML is fast.
package events

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	adapterem "github.com/kgatilin/archai/internal/adapter/eventmodel"
	"github.com/kgatilin/archai/internal/eventmodel"
	"github.com/kgatilin/archai/internal/plugin"
)

// Plugin holds the in-memory state of the events plugin. The Host
// reference captured during Init is used by every capability handler
// at request time.
type Plugin struct {
	host plugin.Host
}

// Manifest implements plugin.Plugin.
func (p *Plugin) Manifest() plugin.Manifest {
	return plugin.Manifest{
		Name:        "events",
		Version:     "0.1.0",
		Description: "Event-model validation and visualization from .arch/events.yaml declarations.",
	}
}

// Init implements plugin.Plugin.
func (p *Plugin) Init(_ context.Context, host plugin.Host, _ string) error {
	if host == nil {
		return fmt.Errorf("events: host is nil")
	}
	p.host = host
	return nil
}

// CLICommands implements plugin.Plugin. Contributes validate and graph
// subcommands under `archai plugin events`.
func (p *Plugin) CLICommands() []plugin.CLICommand {
	return []plugin.CLICommand{
		{Cmd: p.validateCmd()},
		{Cmd: p.graphCmd()},
	}
}

// MCPTools implements plugin.Plugin. Exposes event_model, event_kind,
// and event_validate tools.
func (p *Plugin) MCPTools() []plugin.MCPTool {
	return []plugin.MCPTool{
		{
			Name:        "event_model",
			Description: "Return the composed event model: components with their receives/emits/folds/vocab, plus the projected graph for visualization.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Handler: p.handleEventModel,
		},
		{
			Name:        "event_kind",
			Description: "Return detail for a single event kind: role, owner, producers, consumers, payload schema, deprecated fields. Pass the full kind name (e.g. billing.invoice.issued).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind": map[string]any{
						"type":        "string",
						"description": "Event kind name (e.g. billing.invoice.issued).",
					},
				},
				"required": []string{"kind"},
			},
			Handler: p.handleEventKind,
		},
		{
			Name:        "event_validate",
			Description: "Validate all .arch/events.yaml declarations and return findings. Errors are fatal rule breaches; warnings are potential issues (starvation, orphans) that may be acceptable.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Handler: p.handleEventValidate,
		},
	}
}

// HTTPHandlers implements plugin.Plugin. Serves the projected graph
// as JSON at /api/plugins/events/model for the future canvas view.
func (p *Plugin) HTTPHandlers() []plugin.HTTPHandler {
	return []plugin.HTTPHandler{{
		Path:    "/model",
		Methods: []string{http.MethodGet},
		Handler: http.HandlerFunc(p.serveModel),
	}}
}

// UIComponents implements plugin.Plugin. Returns nil for v1; the UI
// lands separately.
func (p *Plugin) UIComponents() []plugin.UIComponent {
	return nil
}

// loadModel reads and composes the event model from the given root,
// falling back to the host's RepoRoot if root is empty.
func (p *Plugin) loadModel(root string) (*eventmodel.Model, error) {
	if root == "" {
		root = p.host.RepoRoot()
	}
	if root == "" {
		return nil, fmt.Errorf("events: repo root unavailable")
	}
	return eventmodel.Read(root)
}

// validateCmd returns the `validate` subcommand.
func (p *Plugin) validateCmd() *cobra.Command {
	var root string

	cmd := &cobra.Command{
		Use:   "validate [--root PATH]",
		Short: "Validate .arch/events.yaml declarations",
		Long: `Validate all .arch/events.yaml declarations under the specified root.

Errors are fatal rule breaches (ownership violations, unresolved calls,
duplicate owners, $ref cycles). Warnings are potential issues (starved
receives, orphan facts) that may be acceptable depending on the composed set.

Exit codes:
  0 - no errors (warnings may be present)
  1 - one or more errors`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			model, err := p.loadModel(root)
			if err != nil {
				return err
			}
			if len(model.Components) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No event declarations found.")
				return nil
			}

			findings := eventmodel.Validate(model)
			out := cmd.OutOrStdout()
			hasError := false

			for _, f := range findings {
				severity := "WARN"
				if f.Severity == eventmodel.SeverityError {
					severity = "ERROR"
					hasError = true
				}
				loc := f.Component
				if f.Location != "" {
					loc += ":" + f.Location
				}
				fmt.Fprintf(out, "%s [%s] %s: %s\n", severity, f.Kind, loc, f.Message)
			}

			if len(findings) == 0 {
				fmt.Fprintf(out, "OK: %d component(s) validated.\n", len(model.Components))
			} else {
				errorCount := 0
				warnCount := 0
				for _, f := range findings {
					if f.Severity == eventmodel.SeverityError {
						errorCount++
					} else {
						warnCount++
					}
				}
				fmt.Fprintf(out, "\n%d error(s), %d warning(s)\n", errorCount, warnCount)
			}

			if hasError {
				// Return an error to trigger non-zero exit.
				return fmt.Errorf("validation failed")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory to scan (default: repo root)")
	return cmd
}

// graphCmd returns the `graph` subcommand.
func (p *Plugin) graphCmd() *cobra.Command {
	var format string
	var output string
	var root string

	cmd := &cobra.Command{
		Use:   "graph [--root PATH]",
		Short: "Generate event model graph",
		Long: `Generate a graph of the event model from .arch/events.yaml declarations.

The graph shows components, event kinds, folds, and vocab types with
their relationships (emits, receives, feeds, payload refs).

Supported formats:
  graphml  - GraphML XML for archmotif analysis
  mermaid  - Mermaid flowchart diagram`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			model, err := p.loadModel(root)
			if err != nil {
				return err
			}
			if len(model.Components) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No event declarations found.")
				return nil
			}

			g := eventmodel.BuildGraph(model)

			var content string
			switch format {
			case "graphml":
				ag, err := adapterem.ToArchmotifGraph(g)
				if err != nil {
					return fmt.Errorf("converting to graphml: %w", err)
				}
				var buf bytes.Buffer
				if err := ag.WriteGraphML(&buf); err != nil {
					return fmt.Errorf("writing graphml: %w", err)
				}
				content = buf.String()
			case "mermaid":
				content = adapterem.ToMermaid(g)
			default:
				return fmt.Errorf("unknown format %q (want graphml or mermaid)", format)
			}

			var w io.Writer = cmd.OutOrStdout()
			if output != "" {
				f, err := os.Create(output)
				if err != nil {
					return fmt.Errorf("creating output file: %w", err)
				}
				defer f.Close()
				w = f
			}

			_, err = fmt.Fprint(w, content)
			return err
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "mermaid", "Output format: graphml or mermaid")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file (default: stdout)")
	cmd.Flags().StringVar(&root, "root", "", "Root directory to scan (default: repo root)")

	return cmd
}

// handleEventModel is the MCP handler for event_model.
func (p *Plugin) handleEventModel(_ context.Context, _ map[string]any) (any, error) {
	model, err := p.loadModel("")
	if err != nil {
		return nil, err
	}
	if len(model.Components) == 0 {
		return "No event declarations found.", nil
	}

	// Build a compact text representation for agents.
	var sb strings.Builder
	sb.WriteString("Event Model:\n\n")

	// Sort component IDs for deterministic output.
	ids := make([]string, 0, len(model.Components))
	for id := range model.Components {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		comp := model.Components[id]
		sb.WriteString(fmt.Sprintf("COMPONENT: %s", id))
		if comp.Owns != "" {
			sb.WriteString(fmt.Sprintf(" (owns: %s)", comp.Owns))
		}
		sb.WriteString("\n")
		if comp.Description != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", comp.Description))
		}

		if len(comp.Receives) > 0 {
			sb.WriteString("  receives:\n")
			for _, slot := range comp.Receives {
				sb.WriteString(fmt.Sprintf("    - %s [%s]\n", slot.Kind, slot.Role))
			}
		}
		if len(comp.Emits) > 0 {
			sb.WriteString("  emits:\n")
			for _, slot := range comp.Emits {
				sb.WriteString(fmt.Sprintf("    - %s [%s]\n", slot.Kind, slot.Role))
			}
		}
		if len(comp.Folds) > 0 {
			sb.WriteString("  folds:\n")
			for _, fold := range comp.Folds {
				sb.WriteString(fmt.Sprintf("    - %s (pattern: %s)\n", fold.Name, fold.Pattern))
			}
		}
		if len(comp.Vocab) > 0 {
			sb.WriteString("  vocab:\n")
			vocabNames := make([]string, 0, len(comp.Vocab))
			for name := range comp.Vocab {
				vocabNames = append(vocabNames, name)
			}
			sort.Strings(vocabNames)
			for _, name := range vocabNames {
				sb.WriteString(fmt.Sprintf("    - %s\n", name))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// handleEventKind is the MCP handler for event_kind.
func (p *Plugin) handleEventKind(_ context.Context, args map[string]any) (any, error) {
	kindName, _ := args["kind"].(string)
	if kindName == "" {
		return nil, fmt.Errorf("missing required argument: kind")
	}

	model, err := p.loadModel("")
	if err != nil {
		return nil, err
	}
	if len(model.Components) == 0 {
		return nil, fmt.Errorf("no event declarations found")
	}

	// Find the kind across all components.
	var producers []string
	var consumers []string
	var foldConsumers []string
	var role eventmodel.Role
	var owner string
	var schema eventmodel.SchemaNode
	var deprecatedFields []string

	for id, comp := range model.Components {
		// Check if this component owns the kind's namespace.
		if eventmodel.MatchPattern(comp.Owns+".*", kindName) || comp.Owns == kindName {
			owner = id
		}

		for _, slot := range comp.Emits {
			if slot.Kind == kindName {
				producers = append(producers, id)
				role = slot.Role
				if !slot.Schema.IsZero() {
					schema = slot.Schema
				}
			}
		}
		for _, slot := range comp.Receives {
			if slot.Kind == kindName {
				consumers = append(consumers, id)
				role = slot.Role
				if !slot.Schema.IsZero() {
					schema = slot.Schema
				}
			}
		}
		for _, fold := range comp.Folds {
			if eventmodel.MatchPattern(fold.Pattern, kindName) {
				foldConsumers = append(foldConsumers, id+":"+fold.Name)
			}
		}
	}

	if len(producers) == 0 && len(consumers) == 0 {
		return nil, fmt.Errorf("kind %q not found in any component", kindName)
	}

	// Extract deprecated fields from schema.
	if props := schema.Properties(); props != nil {
		for name, prop := range props {
			if prop.Deprecated() {
				deprecatedFields = append(deprecatedFields, name)
			}
		}
	}
	sort.Strings(deprecatedFields)

	// Build compact text output for agents.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("KIND: %s\n", kindName))
	sb.WriteString(fmt.Sprintf("Role: %s\n", role))
	if owner != "" {
		sb.WriteString(fmt.Sprintf("Owner: %s\n", owner))
	} else {
		sb.WriteString("Owner: (none - cross-cutting or unowned)\n")
	}

	sort.Strings(producers)
	sort.Strings(consumers)
	sort.Strings(foldConsumers)

	sb.WriteString(fmt.Sprintf("Producers: %s\n", formatList(producers)))
	sb.WriteString(fmt.Sprintf("Consumers: %s\n", formatList(consumers)))
	if len(foldConsumers) > 0 {
		sb.WriteString(fmt.Sprintf("Fold consumers: %s\n", formatList(foldConsumers)))
	}

	if !schema.IsZero() {
		sb.WriteString("\nPayload schema:\n")
		sb.WriteString(formatSchema(schema, "  "))
	}

	if len(deprecatedFields) > 0 {
		sb.WriteString(fmt.Sprintf("\nDeprecated fields: %s\n", strings.Join(deprecatedFields, ", ")))
	}

	return sb.String(), nil
}

// handleEventValidate is the MCP handler for event_validate.
func (p *Plugin) handleEventValidate(_ context.Context, _ map[string]any) (any, error) {
	model, err := p.loadModel("")
	if err != nil {
		return nil, err
	}
	if len(model.Components) == 0 {
		return "No event declarations found.", nil
	}

	findings := eventmodel.Validate(model)
	if len(findings) == 0 {
		return fmt.Sprintf("OK: %d component(s) validated, no issues.", len(model.Components)), nil
	}

	var sb strings.Builder
	errorCount := 0
	warnCount := 0

	for _, f := range findings {
		severity := "WARN"
		if f.Severity == eventmodel.SeverityError {
			severity = "ERROR"
			errorCount++
		} else {
			warnCount++
		}
		loc := f.Component
		if f.Location != "" {
			loc += ":" + f.Location
		}
		sb.WriteString(fmt.Sprintf("%s [%s] %s: %s\n", severity, f.Kind, loc, f.Message))
	}
	sb.WriteString(fmt.Sprintf("\n%d error(s), %d warning(s)", errorCount, warnCount))

	return sb.String(), nil
}

// serveModel is the HTTP handler for /api/plugins/events/model.
func (p *Plugin) serveModel(w http.ResponseWriter, _ *http.Request) {
	model, err := p.loadModel("")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(model.Components) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"components":[],"graph":{"nodes":[],"edges":[]}}`))
		return
	}

	g := eventmodel.BuildGraph(model)

	// Build JSON response with both model and graph.
	type response struct {
		Components map[string]*eventmodel.Component `json:"components"`
		Graph      *eventmodel.Graph                `json:"graph"`
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response{
		Components: model.Components,
		Graph:      g,
	})
}

// formatList formats a slice as a comma-separated string, or "(none)".
func formatList(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, ", ")
}

// formatSchema formats a schema node as indented text.
func formatSchema(n eventmodel.SchemaNode, indent string) string {
	if n.IsZero() {
		return indent + "(empty)\n"
	}

	m := n.AsMap()
	if m == nil {
		return indent + fmt.Sprintf("%v\n", n.Raw)
	}

	var sb strings.Builder

	// Type
	if t, ok := m["type"].(string); ok {
		sb.WriteString(indent + "type: " + t + "\n")
	}

	// Ref
	if ref := n.Ref(); ref != "" {
		sb.WriteString(indent + "$ref: " + ref + "\n")
	}

	// Properties
	if props := n.Properties(); props != nil {
		sb.WriteString(indent + "properties:\n")
		names := make([]string, 0, len(props))
		for name := range props {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			prop := props[name]
			deprecated := ""
			if prop.Deprecated() {
				deprecated = " (DEPRECATED)"
			}
			propType := "object"
			if pm := prop.AsMap(); pm != nil {
				if t, ok := pm["type"].(string); ok {
					propType = t
				}
			}
			sb.WriteString(fmt.Sprintf("%s  %s: %s%s\n", indent, name, propType, deprecated))
		}
	}

	// Required
	if req, ok := m["required"].([]any); ok && len(req) > 0 {
		reqStrs := make([]string, 0, len(req))
		for _, r := range req {
			if s, ok := r.(string); ok {
				reqStrs = append(reqStrs, s)
			}
		}
		sb.WriteString(indent + "required: " + strings.Join(reqStrs, ", ") + "\n")
	}

	return sb.String()
}

// init registers the plugin with the package-global registry.
func init() {
	plugin.RegisterPlugin(&Plugin{})
}
