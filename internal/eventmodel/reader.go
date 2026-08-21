package eventmodel

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	yamlv3 "gopkg.in/yaml.v3"
)

const eventsFileName = "events.yaml"

// declarationFile is one discovered declaration and the parser that reads it.
type declarationFile struct {
	path  string
	parse func(string) (*Component, error)
}

// Read discovers every event declaration under root and composes them into a
// Model. Two formats are read from the same `.arch/` directory:
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
func Read(root string) (*Model, error) {
	files, err := findDeclarationFiles(root)
	if err != nil {
		return nil, err
	}

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

	var raw rawComponent
	dec := yamlv3.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // Strict decoding: unknown fields are errors.
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	// Structural validation. Version 1 is named explicitly because its shape
	// (receives/emits/folds) is close enough to this one to fail confusingly.
	switch raw.Version {
	case SchemaVersion:
	case 1:
		return nil, fmt.Errorf("version 1 is no longer supported: replace receives/emits/folds with inputs/outputs/state_events and set version: %d", SchemaVersion)
	default:
		return nil, fmt.Errorf("unsupported version %d (want %d)", raw.Version, SchemaVersion)
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
