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
	gofmt "go/format"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	adapterem "github.com/kgatilin/wyrd/internal/adapter/eventmodel"
	"github.com/kgatilin/wyrd/internal/eventmodel"
	"github.com/kgatilin/wyrd/internal/plugin"
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

// CLICommands implements plugin.Plugin. Contributes validate, graph and gen
// subcommands under `wyrd plugin events`.
func (p *Plugin) CLICommands() []plugin.CLICommand {
	return []plugin.CLICommand{
		{Cmd: p.validateCmd()},
		{Cmd: p.graphCmd()},
		{Cmd: p.genCmd()},
	}
}

// MCPTools implements plugin.Plugin. Exposes event_model, event_kind,
// and event_validate tools.
func (p *Plugin) MCPTools() []plugin.MCPTool {
	return []plugin.MCPTool{
		{
			Name:        "event_model",
			Description: "Return the composed event model: components with their inputs/outputs/state_events/types, the fold read-set derived from them, plus the projected graph for visualization.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Handler: p.handleEventModel,
		},
		{
			Name:        "event_kind",
			Description: "Return detail for a single event kind: subject pattern, delivery policy, schema owner, producers, the components it triggers, the components that fold it into state, payload schema, deprecated fields. Pass the full kind name (e.g. billing.invoice.issued).",
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
			Description: "Validate all .arch/events.yaml declarations and return findings. Errors are fatal rule breaches; warnings are potential issues (starvation, orphans) that may be acceptable. Multiple observers of one event are never a finding unless the kind declares delivery: exclusive.",
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

Errors are fatal rule breaches (duplicate namespace owners, incoherent
partition keys, malformed subject slots, $ref cycles, and violations of an
explicit 'delivery: exclusive' contract). Warnings are potential issues
(starved inputs, orphan outputs, underspecified state) that may be
acceptable depending on the composed set.

Multiple components observing the same event is NOT a finding: a durable event
is appended once and may be folded independently by any number of observers.

Exit codes:
  0 - no errors (warnings may be present)
  1 - one or more errors`,
		Args:         cobra.NoArgs,
		SilenceUsage: true, // Validation failure is not a usage error.
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

The graph shows components, event kinds, and type definitions with their
relationships (output, input, state-event, defines, payload refs).

Supported formats:
  graphml  - GraphML XML for archmotif analysis
  mermaid  - Mermaid flowchart diagram`,
		Args:         cobra.NoArgs,
		SilenceUsage: true, // Graph generation errors are not usage errors.
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

// generatedMarker is the substring every generated file name must contain.
// The generator writes only to files carrying it and never touches anything
// else: a generator that can clobber hand-edits will not be re-run.
const generatedMarker = "_gen."

// templatesDirName is the conventional location of a project's codegen
// templates, relative to the generation root.
const templatesDirName = ".arch/templates"

// genCmd returns the `gen` subcommand.
func (p *Plugin) genCmd() *cobra.Command {
	var root string
	var templatesDir string
	var outDir string
	var componentID string
	var dryRun bool
	var force bool
	var noFormat bool

	cmd := &cobra.Command{
		Use:   "gen [--root PATH]",
		Short: "Render event declarations through project templates",
		Long: `Render each component's event declaration through project-supplied
templates.

wyrd owns the declaration format, its validation and its graph; the PROJECT
owns the binding. Templates live in the project (default: .arch/templates/*.tmpl
under the root), wyrd never learns the project's types, and nothing wyrd
produces is a runtime dependency of the generated code. There is deliberately no
--lang flag: a language generator built into wyrd would invert that split.

Each template is rendered once per component. The output file name is the
template name minus its .tmpl suffix, and it must contain "` + generatedMarker + `" —
so contract_gen.go.tmpl produces contract_gen.go. The generator writes only to
those files and never touches handwritten code.

Output goes next to the component's .arch directory, or under --out/<component>
when --out is given. Generated .go files are run through gofmt (syntactic only)
so committed output has stable diffs and a template emitting invalid Go fails
here rather than at compile time; --no-format skips it.

The model is validated first: generating from a declaration set with errors
produces broken code. Use --force to generate anyway.

Template data model and helper functions: see docs/event-model.md.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolvedRoot := root
			if resolvedRoot == "" {
				resolvedRoot = p.host.RepoRoot()
			}
			if resolvedRoot == "" {
				return fmt.Errorf("events: repo root unavailable")
			}

			model, err := p.loadModel(resolvedRoot)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(model.Components) == 0 {
				fmt.Fprintln(out, "No event declarations found.")
				return nil
			}

			if err := p.checkGenerable(out, model, force); err != nil {
				return err
			}

			if templatesDir == "" {
				templatesDir = filepath.Join(resolvedRoot, filepath.FromSlash(templatesDirName))
			}
			templates, err := loadTemplates(templatesDir)
			if err != nil {
				return err
			}
			if len(templates) == 0 {
				return fmt.Errorf("no *.tmpl templates found in %s; codegen is template-driven, so the project supplies them", templatesDir)
			}

			ids := make([]string, 0, len(model.Components))
			for id := range model.Components {
				if componentID != "" && id != componentID {
					continue
				}
				ids = append(ids, id)
			}
			if componentID != "" && len(ids) == 0 {
				return fmt.Errorf("component %q not found", componentID)
			}
			sort.Strings(ids)

			written := 0
			for _, id := range ids {
				comp := model.Components[id]
				data := adapterem.BuildTemplateData(comp)
				destDir, err := generationDir(comp, outDir)
				if err != nil {
					return err
				}
				for _, tpl := range templates {
					content, err := adapterem.RenderTemplate(tpl.name, tpl.text, data)
					if err != nil {
						return fmt.Errorf("component %s: %w", id, err)
					}
					if !noFormat {
						content, err = formatGenerated(tpl.outputName, content)
						if err != nil {
							return fmt.Errorf("component %s, template %s: %w", id, tpl.name, err)
						}
					}
					dest := filepath.Join(destDir, tpl.outputName)
					if dryRun {
						fmt.Fprintf(out, "would write %s (%d bytes)\n", dest, len(content))
						written++
						continue
					}
					if err := os.MkdirAll(destDir, 0o755); err != nil {
						return fmt.Errorf("creating %s: %w", destDir, err)
					}
					if err := os.WriteFile(dest, content, 0o644); err != nil {
						return fmt.Errorf("writing %s: %w", dest, err)
					}
					fmt.Fprintf(out, "wrote %s\n", dest)
					written++
				}
			}

			fmt.Fprintf(out, "\n%d file(s) from %d template(s) across %d component(s).\n",
				written, len(templates), len(ids))
			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory to scan (default: repo root)")
	cmd.Flags().StringVar(&templatesDir, "templates", "", "Template directory (default: <root>/"+templatesDirName+")")
	cmd.Flags().StringVar(&outDir, "out", "", "Write under <out>/<component>/ instead of next to each component")
	cmd.Flags().StringVar(&componentID, "component", "", "Generate for one component only")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be written without writing it")
	cmd.Flags().BoolVar(&force, "force", false, "Generate even when validation reports errors")
	cmd.Flags().BoolVar(&noFormat, "no-format", false, "Skip gofmt of generated .go files")

	return cmd
}

// formatGenerated canonicalizes generated Go before it is written.
//
// This is not wyrd learning the project's types — go/format is purely
// syntactic. It is here because generated files are committed: unformatted
// output makes every diff noisy, and a template that emits invalid Go should
// fail at generation time rather than at compile time. Files of any other
// extension pass through untouched.
func formatGenerated(name string, content []byte) ([]byte, error) {
	if filepath.Ext(name) != ".go" {
		return content, nil
	}
	formatted, err := gofmt.Source(content)
	if err != nil {
		return nil, fmt.Errorf("produced invalid Go: %w", err)
	}
	return formatted, nil
}

// checkGenerable refuses to generate from a model that does not validate.
// A kind declared with two roles, or a fold whose subjects disagree on the
// partition key, yields generated code that is wrong in ways the compiler
// cannot catch — colliding constants, a subscription keyed on the wrong slot.
func (p *Plugin) checkGenerable(out io.Writer, model *eventmodel.Model, force bool) error {
	var errs []eventmodel.Finding
	for _, f := range eventmodel.Validate(model) {
		if f.Severity == eventmodel.SeverityError {
			errs = append(errs, f)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	for _, f := range errs {
		loc := f.Component
		if f.Location != "" {
			loc += ":" + f.Location
		}
		fmt.Fprintf(out, "ERROR [%s] %s: %s\n", f.Kind, loc, f.Message)
	}
	if force {
		fmt.Fprintf(out, "\n%d error(s); generating anyway (--force)\n\n", len(errs))
		return nil
	}
	return fmt.Errorf("%d validation error(s); fix them or pass --force", len(errs))
}

// projectTemplate is one loaded template file.
type projectTemplate struct {
	name       string // file name, for diagnostics
	outputName string // name minus the .tmpl suffix
	text       string
}

// loadTemplates reads *.tmpl files from dir, rejecting any whose output name
// would not be recognizably generated.
func loadTemplates(dir string) ([]projectTemplate, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading templates from %s: %w", dir, err)
	}

	var out []projectTemplate
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tmpl") {
			continue
		}
		outputName := strings.TrimSuffix(e.Name(), ".tmpl")
		if !strings.Contains(outputName, generatedMarker) {
			return nil, fmt.Errorf("template %s would write %q, which does not contain %q; "+
				"name it like contract%sgo.tmpl so generated files are never mistaken for handwritten ones",
				e.Name(), outputName, generatedMarker, generatedMarker)
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading template %s: %w", e.Name(), err)
		}
		out = append(out, projectTemplate{name: e.Name(), outputName: outputName, text: string(data)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// generationDir resolves where a component's generated files go: next to its
// .arch directory by default, or under <outDir>/<component> when redirected.
func generationDir(comp *eventmodel.Component, outDir string) (string, error) {
	if outDir != "" {
		return filepath.Join(outDir, comp.ID), nil
	}
	if comp.SourceFile == "" {
		return "", fmt.Errorf("component %s has no source file; pass --out to choose a destination", comp.ID)
	}
	// SourceFile is <component>/.arch/events.yaml.
	return filepath.Dir(filepath.Dir(comp.SourceFile)), nil
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

		for _, section := range []struct {
			label string
			slots []eventmodel.Slot
		}{
			{"inputs", comp.Inputs},
			{"outputs", comp.Outputs},
			{"state_events", comp.StateEvents},
		} {
			if len(section.slots) == 0 {
				continue
			}
			sb.WriteString("  " + section.label + ":\n")
			for _, slot := range section.slots {
				sb.WriteString("    - " + slotLine(slot))
			}
		}
		// The fold is derived, so it is reported rather than listed: an agent
		// reading this needs the read-set it will actually subscribe with.
		if subjects := comp.Subjects(); len(subjects) > 0 {
			sb.WriteString(fmt.Sprintf("  fold: subjects %v, partition key %v\n",
				subjects, comp.PartitionKey))
		}
		if len(comp.Types) > 0 {
			sb.WriteString("  types:\n")
			typeNames := make([]string, 0, len(comp.Types))
			for name := range comp.Types {
				typeNames = append(typeNames, name)
			}
			sort.Strings(typeNames)
			for _, name := range typeNames {
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

	// Find the kind across all components. Ownership is resolved by longest
	// `owns` prefix and identifies who defines the schema — not who is
	// allowed to emit or observe the kind.
	var producers []string
	var consumers []string
	var stateFolders []string
	var exclusive bool
	var schema eventmodel.SchemaNode
	var deprecatedFields []string

	owner := eventmodel.OwnerOf(model, kindName)
	pattern := eventmodel.PatternOf(model, kindName)

	for id, comp := range model.Components {
		for _, section := range []struct {
			slots []eventmodel.Slot
			into  *[]string
		}{
			{comp.Outputs, &producers},
			{comp.Inputs, &consumers},
			{comp.StateEvents, &stateFolders},
		} {
			for _, slot := range section.slots {
				if slot.Kind != kindName {
					continue
				}
				*section.into = append(*section.into, id)
				exclusive = exclusive || slot.Delivery.IsExclusive()
				if !slot.Schema.IsZero() {
					schema = slot.Schema
				}
			}
		}
	}

	if len(producers) == 0 && len(consumers) == 0 && len(stateFolders) == 0 {
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
	if pattern != "" {
		sb.WriteString(fmt.Sprintf("Subject: %s\n", pattern))
	}
	if exclusive {
		sb.WriteString("Delivery: exclusive (exactly one input consumer required)\n")
	} else {
		sb.WriteString("Delivery: broadcast (0..N independent observers)\n")
	}
	if owner != "" {
		sb.WriteString(fmt.Sprintf("Schema owner: %s\n", owner))
	} else {
		sb.WriteString("Schema owner: (none - namespace unclaimed)\n")
	}

	sort.Strings(producers)
	sort.Strings(consumers)
	sort.Strings(stateFolders)

	sb.WriteString(fmt.Sprintf("Outputs (producers): %s\n", formatList(producers)))
	sb.WriteString(fmt.Sprintf("Inputs (triggered): %s\n", formatList(consumers)))
	if len(stateFolders) > 0 {
		sb.WriteString(fmt.Sprintf("State events (folded by): %s\n", formatList(stateFolders)))
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

// slotLine renders one inputs/outputs/state_events entry. The subject pattern
// is shown when declared; delivery only when it departs from the broadcast
// default, so the common case stays quiet.
func slotLine(slot eventmodel.Slot) string {
	var parts []string
	if slot.Pattern != "" {
		parts = append(parts, slot.Pattern)
	}
	if slot.Delivery.IsExclusive() {
		parts = append(parts, "delivery: exclusive")
	}
	if len(parts) == 0 {
		return slot.Kind + "\n"
	}
	return fmt.Sprintf("%s [%s]\n", slot.Kind, strings.Join(parts, ", "))
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
