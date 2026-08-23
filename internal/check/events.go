package check

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/kgatilin/wyrd/internal/eventmodel"
	"github.com/kgatilin/wyrd/internal/eventsrc"
)

// EventsOptions configures the event-model gate.
type EventsOptions struct {
	// Root is the project directory holding the overlay and the event
	// declarations it points at. Required.
	Root string
	// Only restricts the report to these finding kinds (for example
	// "unwired-kind"). Empty reports every kind.
	Only []string
	// Format is "text" or "json".
	Format string
	// Strict fails the gate on warnings as well as errors.
	Strict bool
	// Warn reports findings but keeps the exit code at zero.
	Warn bool
}

// EventsResult carries the outcome of an event-model check.
type EventsResult struct {
	// Declared is false when the project has no event declarations at all;
	// in that case Findings is empty and nothing was evaluated.
	Declared bool
	Findings []eventmodel.Finding
}

// Errors counts the findings that are rule breaches rather than suspicions.
func (r EventsResult) Errors() int {
	return countSeverity(r.Findings, eventmodel.SeverityError)
}

// Warnings counts the findings that depend on the composed set being whole.
func (r EventsResult) Warnings() int {
	return countSeverity(r.Findings, eventmodel.SeverityWarning)
}

func countSeverity(findings []eventmodel.Finding, want eventmodel.FindingSeverity) int {
	n := 0
	for _, f := range findings {
		if f.Severity == want {
			n++
		}
	}
	return n
}

// Events reads the project's event declarations and validates them.
//
// It reads no Go source. The event map is declared in YAML and answers
// questions about addresses rather than about imports, so this gate runs in
// projects whose code the extractor cannot load and costs nothing in the ones
// where it can.
func (c *Checker) Events(ctx context.Context, opts EventsOptions) (EventsResult, error) {
	_ = ensureCtx(ctx)
	if opts.Root == "" {
		return EventsResult{}, fmt.Errorf("events: no project root")
	}

	locations, err := eventsrc.Locations(opts.Root)
	if err != nil {
		return EventsResult{}, err
	}
	model, err := eventmodel.ReadSources(opts.Root, locations)
	if err != nil {
		return EventsResult{}, err
	}
	if model == nil || len(model.Components) == 0 {
		return EventsResult{Declared: false}, nil
	}

	findings, err := filterKinds(eventmodel.Validate(model), opts.Only)
	if err != nil {
		return EventsResult{}, err
	}
	return EventsResult{Declared: true, Findings: findings}, nil
}

// filterKinds narrows findings to the requested kinds.
//
// An unknown kind is an error rather than an empty report: `--only unwierd`
// silently finding nothing is the same output as a clean repository, and a
// gate that passes because its filter was misspelled is worse than no gate.
func filterKinds(findings []eventmodel.Finding, only []string) ([]eventmodel.Finding, error) {
	if len(only) == 0 {
		return findings, nil
	}
	want := make(map[string]bool, len(only))
	for _, kind := range only {
		for _, part := range strings.Split(kind, ",") {
			if part = strings.TrimSpace(part); part != "" {
				want[part] = true
			}
		}
	}

	seen := make(map[string]bool, len(findings))
	for _, f := range findings {
		seen[string(f.Kind)] = true
	}
	for kind := range want {
		if !eventmodel.KnownFindingKind(kind) {
			return nil, fmt.Errorf("unknown finding kind %q; known kinds: %s",
				kind, strings.Join(eventmodel.FindingKinds(), ", "))
		}
	}

	out := make([]eventmodel.Finding, 0, len(findings))
	for _, f := range findings {
		if want[string(f.Kind)] {
			out = append(out, f)
		}
	}
	return out, nil
}

// RunEvents performs the event-model check and writes the report to out.
// The returned error is non-nil when the gate fails, unless opts.Warn is set.
func (c *Checker) RunEvents(ctx context.Context, opts EventsOptions, out, errOut io.Writer) error {
	if opts.Format != "" && opts.Format != "text" && opts.Format != "json" {
		return fmt.Errorf("--format must be text or json, got %q", opts.Format)
	}

	res, err := c.Events(ctx, opts)
	if err != nil {
		return err
	}
	return reportEvents(out, res, opts)
}

func reportEvents(out io.Writer, res EventsResult, opts EventsOptions) error {
	if !res.Declared {
		if opts.Format == "json" {
			return WriteEventsJSON(out, nil, opts.Strict)
		}
		fmt.Fprintf(out, "No event declarations found under %s. Nothing to check.\n", opts.Root)
		return nil
	}

	if opts.Format == "json" {
		if err := WriteEventsJSON(out, res.Findings, opts.Strict); err != nil {
			return err
		}
	} else {
		FormatEventFindings(out, res.Findings)
	}

	if failing := res.failing(opts.Strict); failing > 0 && !opts.Warn {
		return fmt.Errorf("%d event-model finding(s) found", failing)
	}
	return nil
}

// failing counts the findings this invocation treats as a failure. Warnings
// are suspicions by construction — the declarations in one repository can be a
// subset of the running system — so they only count under --strict.
func (r EventsResult) failing(strict bool) int {
	if strict {
		return len(r.Findings)
	}
	return r.Errors()
}

// eventFindingJSON is the wire shape of one finding. The library type carries
// unexported-friendly field names and a free-form Related map; naming the JSON
// keys here keeps the contract a consumer greps stable against refactors of the
// validator's internals.
type eventFindingJSON struct {
	Severity  string         `json:"severity"`
	Kind      string         `json:"kind"`
	Component string         `json:"component,omitempty"`
	File      string         `json:"file,omitempty"`
	Location  string         `json:"location,omitempty"`
	Message   string         `json:"message"`
	Related   map[string]any `json:"related,omitempty"`
}

// WriteEventsJSON emits the findings as {ok, errors, warnings, findings}.
//
// `ok` answers "did the gate pass", so it follows strict: a consumer parsing it
// under --strict and reading true over two warnings would be reading the
// opposite of the exit code beside it. It does not follow --warn, which asks
// for a report without a verdict and changes the exit code alone.
func WriteEventsJSON(w io.Writer, findings []eventmodel.Finding, strict bool) error {
	items := make([]eventFindingJSON, 0, len(findings))
	for _, f := range findings {
		items = append(items, eventFindingJSON{
			Severity:  string(f.Severity),
			Kind:      string(f.Kind),
			Component: f.Component,
			File:      f.File,
			Location:  f.Location,
			Message:   f.Message,
			Related:   f.Related,
		})
	}
	payload := struct {
		OK       bool               `json:"ok"`
		Errors   int                `json:"errors"`
		Warnings int                `json:"warnings"`
		Findings []eventFindingJSON `json:"findings"`
	}{
		OK:       EventsResult{Findings: findings}.failing(strict) == 0,
		Errors:   countSeverity(findings, eventmodel.SeverityError),
		Warnings: countSeverity(findings, eventmodel.SeverityWarning),
		Findings: items,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// FormatEventFindings renders a human-readable event-model report, grouped by
// the file that declared the offending component so a reader can open one file
// and fix everything it is responsible for.
func FormatEventFindings(w io.Writer, findings []eventmodel.Finding) {
	if len(findings) == 0 {
		fmt.Fprintln(w, "OK: no event-model findings.")
		return
	}

	errs := countSeverity(findings, eventmodel.SeverityError)
	warns := countSeverity(findings, eventmodel.SeverityWarning)
	fmt.Fprintf(w, "Found %d event-model finding(s): %d error(s), %d warning(s).\n\n", len(findings), errs, warns)

	byFile := map[string][]eventmodel.Finding{}
	for _, f := range findings {
		file := f.File
		if file == "" {
			file = "(no file)"
		}
		byFile[file] = append(byFile[file], f)
	}
	files := make([]string, 0, len(byFile))
	for file := range byFile {
		files = append(files, file)
	}
	sort.Strings(files)

	for _, file := range files {
		fmt.Fprintf(w, "%s\n", file)
		for _, f := range byFile[file] {
			where := f.Component
			if f.Location != "" {
				where = fmt.Sprintf("%s %s", where, f.Location)
			}
			fmt.Fprintf(w, "  %-7s %-22s %s\n      %s\n", f.Severity, f.Kind, where, f.Message)
		}
		fmt.Fprintln(w)
	}
}
