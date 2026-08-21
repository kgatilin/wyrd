package eventmodel

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
)

const eventsFileName = "events.yaml"

// declarationFile is one discovered declaration and the parser that reads it.
type declarationFile struct {
	path  string
	parse func(string) (*Component, error)
}

// Format names one of the two declaration formats.
type Format string

const (
	// FormatEvents is the events.yaml schema wyrd owns.
	FormatEvents Format = "events"
	// FormatAsyncAPI is an AsyncAPI 3 document carrying x-eventlog.
	FormatAsyncAPI Format = "asyncapi"
)

// Location is a declared place to look for event declarations, on top of the
// `.arch/` convention Read always follows.
//
// The convention assumes a project keeps each component's declaration next to
// that component's code. Plenty of projects instead generate every schema into
// one directory — a flat list of `<component>.asyncapi.yaml` — and nothing
// about that layout is wrong, so a project says where its schemas live rather
// than moving them to be found.
type Location struct {
	// Path is the directory to scan, relative to the scan root (an absolute
	// path is taken as-is). Scanned recursively.
	Path string

	// Include is a glob narrowing which files in Path count. A pattern with
	// no "/" matches against the file name; one with a "/" matches against
	// the path relative to Path. Empty means every file whose name names a
	// format (see FormatOf).
	Include string

	// Format forces how the matched files are parsed. Empty infers it from
	// each file's name, which is what a directory holding both formats needs.
	Format Format
}

// Read discovers every event declaration under root and composes them into a
// Model, with no configured sources — see ReadSources.
func Read(root string) (*Model, error) {
	return ReadSources(root, nil)
}

// ReadSources discovers every event declaration under root and composes them
// into a Model. Two formats are read:
//
//   - `events.yaml` — the native format, parsed strictly (unknown fields are
//     errors, because typos in a format wyrd owns are typos);
//   - `asyncapi.yaml` — an AsyncAPI 3 document with the x-eventlog extension,
//     parsed leniently, because it is somebody else's format and carries
//     fields wyrd has no reason to model.
//
// Both project onto the same Component, so everything downstream — the graph,
// the canvas, the MCP tools — works without knowing which one a component came
// from. Parse errors are returned immediately; validation runs separately via
// Validate.
//
// Every `.arch/` and `.wyrd/` directory under root is always scanned. sources
// adds directories on top of that: declaring one never turns the convention
// off, so a project that generates most of its schemas into one folder can
// still keep a hand-written declaration beside the code it describes.
func ReadSources(root string, sources []Location) (*Model, error) {
	files, err := findDeclarationFiles(root)
	if err != nil {
		return nil, err
	}
	extra, err := findSourceFiles(root, sources)
	if err != nil {
		return nil, err
	}
	files = append(files, extra...)
	files = dedupeByPath(files)

	model := &Model{Components: make(map[string]*Component)}
	for _, file := range files {
		comp, err := file.parse(file.path)
		if err != nil {
			return nil, fmt.Errorf("eventmodel: %s: %w", file.path, err)
		}
		if comp == nil {
			continue
		}
		if existing, ok := model.Components[comp.ID]; ok {
			return nil, fmt.Errorf("eventmodel: duplicate component id %q: %s and %s",
				comp.ID, existing.SourceFile, file.path)
		}
		model.Components[comp.ID] = comp
	}
	return model, nil
}

func findDeclarationFiles(root string) ([]declarationFile, error) {
	var out []declarationFile
	// Clean the root path for consistent comparison.
	cleanRoot := filepath.Clean(root)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		// Skip common non-source directories, but never skip the explicitly
		// requested root — an explicit --root is an instruction to scan that
		// directory regardless of its name.
		cleanPath := filepath.Clean(path)
		if cleanPath != cleanRoot {
			switch name {
			case ".git", ".worktrees", ".claude", "bin", "vendor", "node_modules", "testdata":
				return filepath.SkipDir
			}
		}
		if name == ".arch" || name == ".wyrd" {
			// Both declaration formats live side by side in this directory. A
			// component declaring both is not an error here — the two produce
			// two component ids, and Read rejects them only if the ids collide.
			candidates := []declarationFile{
				{filepath.Join(path, eventsFileName), parseEventsFile},
				{filepath.Join(path, asyncAPIFileName), parseAsyncAPIFile},
			}
			for _, candidate := range candidates {
				if _, statErr := os.Stat(candidate.path); statErr == nil {
					out = append(out, candidate)
				} else if !os.IsNotExist(statErr) {
					return fmt.Errorf("eventmodel: stat %s: %w", candidate.path, statErr)
				}
			}
			// Don't recurse into .arch subdirectories (targets, worktrees, etc).
			return filepath.SkipDir
		}
		return nil
	})
	return out, err
}

// findSourceFiles resolves the configured sources into declaration files.
// A source naming a directory that does not exist is an error: it is a
// statement about where this project keeps its schemas, and silently reading
// zero components from a typo is the failure this whole path exists to fix.
func findSourceFiles(root string, sources []Location) ([]declarationFile, error) {
	var out []declarationFile
	for _, src := range sources {
		dir := src.Path
		if dir == "" {
			return nil, fmt.Errorf("eventmodel: event source with no path")
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		info, err := os.Stat(dir)
		if err != nil {
			return nil, fmt.Errorf("eventmodel: event source %s: %w", src.Path, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("eventmodel: event source %s: not a directory", src.Path)
		}

		err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", ".worktrees", "node_modules", "vendor":
					if filepath.Clean(path) != filepath.Clean(dir) {
						return filepath.SkipDir
					}
				}
				return nil
			}
			rel, relErr := filepath.Rel(dir, path)
			if relErr != nil {
				return relErr
			}
			match, matchErr := matchesInclude(src.Include, filepath.ToSlash(rel))
			if matchErr != nil {
				return fmt.Errorf("eventmodel: event source %s: include %q: %w", src.Path, src.Include, matchErr)
			}
			if !match {
				return nil
			}
			format := src.Format
			if format == "" {
				format = FormatOf(d.Name())
			}
			switch format {
			case FormatEvents:
				out = append(out, declarationFile{path, parseEventsFile})
			case FormatAsyncAPI:
				out = append(out, declarationFile{path, parseAsyncAPIFile})
			case "":
				// A file in a scanned directory whose name names no format —
				// a README, a JSON Schema a document $refs. Not an error.
			default:
				return fmt.Errorf("eventmodel: event source %s: unknown format %q (want %q or %q)",
					src.Path, format, FormatEvents, FormatAsyncAPI)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// matchesInclude reports whether rel (slash-separated, relative to the source
// directory) is selected by pattern. An empty pattern selects everything and
// leaves the choice to FormatOf.
func matchesInclude(pattern, rel string) (bool, error) {
	if pattern == "" {
		return true, nil
	}
	if strings.Contains(pattern, "/") {
		return path.Match(pattern, rel)
	}
	return path.Match(pattern, path.Base(rel))
}

// FormatOf infers a declaration format from a file name. It recognises the two
// conventional names and their `<component>.` prefixed forms, which is how a
// directory holding one file per component is written.
func FormatOf(name string) Format {
	switch {
	case name == eventsFileName || strings.HasSuffix(name, "."+eventsFileName):
		return FormatEvents
	case name == asyncAPIFileName || strings.HasSuffix(name, "."+asyncAPIFileName):
		return FormatAsyncAPI
	}
	return ""
}

// dedupeByPath drops files discovered twice — a source pointing at a directory
// that also holds a `.arch/`, say — keeping the first occurrence so the
// convention's parser wins over a source's forced format.
func dedupeByPath(files []declarationFile) []declarationFile {
	seen := make(map[string]struct{}, len(files))
	out := files[:0:0]
	for _, f := range files {
		key := filepath.Clean(f.path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, f)
	}
	return out
}

// rawComponent is the YAML structure for parsing. Field names match the
// schema from design.md §1.
type rawComponent struct {
	Version     int            `yaml:"version"`
	Component   string         `yaml:"component"`
	Owns        string         `yaml:"owns"`
	Description string         `yaml:"description"`
	Inputs      []rawSlot      `yaml:"inputs"`
	Outputs     []rawSlot      `yaml:"outputs"`
	StateEvents []rawSlot      `yaml:"state_events"`
	State       any            `yaml:"state"`
	Types       map[string]any `yaml:"types"`
	Extra       map[string]any `yaml:"extra"`
}

type rawSlot struct {
	Kind        string         `yaml:"kind"`
	Pattern     string         `yaml:"pattern"`
	Delivery    string         `yaml:"delivery"`
	Description string         `yaml:"description"`
	Exposure    []string       `yaml:"exposure"`
	Schema      any            `yaml:"schema"`
	Extra       map[string]any `yaml:"extra"`
}

// SchemaVersion is the only .arch/events.yaml version this reader accepts.
const SchemaVersion = 2

func parseEventsFile(path string) (*Component, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	// The version is read first, leniently. Strict decoding is right for a
	// version this reader understands, but it rejects a version 1 document on
	// its first `receives:` — before the version check below, whose whole job
	// is to say what happened to a version 1 document.
	var probe struct {
		Version int `yaml:"version"`
	}
	if err := yamlv3.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}
	switch probe.Version {
	case SchemaVersion:
	case 1:
		return nil, fmt.Errorf("version 1 is no longer supported: replace receives/emits/folds with inputs/outputs/state_events and set version: %d", SchemaVersion)
	default:
		return nil, fmt.Errorf("unsupported version %d (want %d)", probe.Version, SchemaVersion)
	}

	var raw rawComponent
	dec := yamlv3.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // Strict decoding: unknown fields are errors.
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}
	if raw.Component == "" {
		return nil, fmt.Errorf("missing required field 'component'")
	}

	comp := &Component{
		Version:     raw.Version,
		ID:          raw.Component,
		Owns:        raw.Owns,
		Description: raw.Description,
		State:       SchemaNode{Raw: raw.State},
		Types:       make(map[string]SchemaNode, len(raw.Types)),
		Extra:       raw.Extra,
		SourceFile:  path,
	}

	// Convert reusable type definitions.
	for name, schema := range raw.Types {
		comp.Types[name] = SchemaNode{Raw: schema}
	}

	sections := []struct {
		name string
		raw  []rawSlot
		dst  *[]Slot
	}{
		{"inputs", raw.Inputs, &comp.Inputs},
		{"outputs", raw.Outputs, &comp.Outputs},
		{"state_events", raw.StateEvents, &comp.StateEvents},
	}
	for _, section := range sections {
		for i, rs := range section.raw {
			slot, err := convertSlot(rs, section.name, i)
			if err != nil {
				return nil, err
			}
			*section.dst = append(*section.dst, slot)
		}
	}

	comp.PartitionKey = partitionKeyOf(comp.Subjects())

	return comp, nil
}

func convertSlot(rs rawSlot, section string, idx int) (Slot, error) {
	if rs.Kind == "" {
		return Slot{}, fmt.Errorf("%s[%d]: missing required field 'kind'", section, idx)
	}
	delivery := Delivery(rs.Delivery)
	if !delivery.Valid() {
		return Slot{}, fmt.Errorf("%s[%d] (%s): invalid delivery %q (want 'broadcast' or 'exclusive')",
			section, idx, rs.Kind, rs.Delivery)
	}
	return Slot{
		Kind:        rs.Kind,
		Pattern:     rs.Pattern,
		Delivery:    delivery,
		Description: rs.Description,
		Exposure:    rs.Exposure,
		Schema:      SchemaNode{Raw: rs.Schema},
		Extra:       rs.Extra,
	}, nil
}

// partitionKeyOf returns the ordered {slot} names of the first subject that
// carries any, which the validator then requires every other subject of the
// component's read-set to repeat. Patterns with no slots are skipped rather
// than treated as an empty key: a kind addressed globally sits in the same
// read-set as partitioned ones without forcing the key to nil.
// It does not validate syntax; use ValidateSlotSyntax for that.
func partitionKeyOf(subjects []string) []string {
	for _, subject := range subjects {
		if key := SlotTokens(subject); len(key) > 0 {
			return key
		}
	}
	return nil
}

// kindHasPrefix reports whether kind starts with the given owns prefix.
// An owns prefix matches if the kind equals the prefix or starts with
// prefix followed by a dot. For example: owns "billing" matches kinds
// "billing", "billing.invoice", "billing.invoice.issued".
func kindHasPrefix(kind, owns string) bool {
	if owns == "" {
		return false
	}
	if kind == owns {
		return true
	}
	if len(kind) > len(owns) && kind[:len(owns)] == owns && kind[len(owns)] == '.' {
		return true
	}
	return false
}

// SlotTokens returns the {slot} names of a subject pattern in declaration
// order. The order is significant: it is the partition key layout, and two
// subjects reading into the same component state must agree on it exactly.
// It does not validate syntax; use ValidateSlotSyntax for that.
// SubjectsMatch reports whether two declared subjects can carry the same
// event.
//
// A subject is a dotted address whose segments are literals, `{slot}` tokens
// the declaration left open, or `*`. Two subjects address the same events when
// they have the same number of segments and every pair either agrees literally
// or has an open segment on one side. Matching is what makes a port family
// readable: a caller addressing `channel.{name}.…` reaches every channel
// instance, while a caller addressing `channel.runner.…` reaches exactly one.
//
// A missing subject matches anything. A native declaration need not state one,
// and dropping its flows would be a worse answer than drawing them.
func SubjectsMatch(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if openSegment(as[i]) || openSegment(bs[i]) {
			continue
		}
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

// openSegment reports whether a segment stands for something rather than being
// it: a `{slot}` the declaration left for the caller, or a `*`.
func openSegment(segment string) bool {
	return segment == "*" || strings.Contains(segment, "{")
}

func SlotTokens(subject string) []string {
	var out []string
	for i := 0; i < len(subject); {
		if subject[i] == '{' {
			// Find closing brace.
			end := i + 1
			for end < len(subject) && subject[end] != '}' {
				end++
			}
			if end < len(subject) {
				out = append(out, subject[i+1:end])
				i = end + 1
				continue
			}
		}
		i++
	}
	return out
}

// ValidateSlotSyntax checks that {slot} tokens in a subject are well-formed:
// balanced braces and non-empty slot names. Returns nil if valid, otherwise
// a descriptive error.
func ValidateSlotSyntax(subject string) error {
	if subject == "" {
		return nil
	}
	for i := 0; i < len(subject); {
		if subject[i] == '{' {
			// Find closing brace.
			end := i + 1
			for end < len(subject) && subject[end] != '}' {
				// Nested brace is malformed.
				if subject[end] == '{' {
					return fmt.Errorf("nested '{' at position %d", end)
				}
				end++
			}
			if end >= len(subject) {
				return fmt.Errorf("unclosed '{' at position %d", i)
			}
			// Check for empty slot name.
			if end == i+1 {
				return fmt.Errorf("empty slot '{}' at position %d", i)
			}
			i = end + 1
			continue
		}
		// Unmatched closing brace.
		if subject[i] == '}' {
			return fmt.Errorf("unmatched '}' at position %d", i)
		}
		i++
	}
	return nil
}
