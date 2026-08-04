package eventmodel

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	yamlv3 "gopkg.in/yaml.v3"
)

const eventsFileName = "events.yaml"

// Read discovers all .arch/events.yaml files under root, parses them with
// strict decoding (unknown fields are errors), and composes them into a Model.
// Parse errors are returned immediately; validation runs separately via
// Validate.
func Read(root string) (*Model, error) {
	files, err := findEventsFiles(root)
	if err != nil {
		return nil, err
	}

	model := &Model{Components: make(map[string]*Component)}
	for _, path := range files {
		comp, err := parseEventsFile(path)
		if err != nil {
			return nil, fmt.Errorf("eventmodel: %s: %w", path, err)
		}
		if comp == nil {
			continue
		}
		if existing, ok := model.Components[comp.ID]; ok {
			return nil, fmt.Errorf("eventmodel: duplicate component id %q: %s and %s",
				comp.ID, existing.SourceFile, path)
		}
		model.Components[comp.ID] = comp
	}
	return model, nil
}

func findEventsFiles(root string) ([]string, error) {
	var out []string
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
		if name == ".arch" {
			// Check for events.yaml in this .arch directory.
			eventsPath := filepath.Join(path, eventsFileName)
			if _, statErr := os.Stat(eventsPath); statErr == nil {
				out = append(out, eventsPath)
			} else if !os.IsNotExist(statErr) {
				return fmt.Errorf("eventmodel: stat %s: %w", eventsPath, statErr)
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
	Version     int                    `yaml:"version"`
	Component   string                 `yaml:"component"`
	Owns        string                 `yaml:"owns"`
	Description string                 `yaml:"description"`
	Receives    []rawSlot              `yaml:"receives"`
	Emits       []rawSlot              `yaml:"emits"`
	Folds       []rawFold              `yaml:"folds"`
	Vocab       map[string]any         `yaml:"vocab"`
	Extra       map[string]any         `yaml:"extra"`
}

type rawSlot struct {
	Kind        string         `yaml:"kind"`
	Role        string         `yaml:"role"`
	Description string         `yaml:"description"`
	Exposure    []string       `yaml:"exposure"`
	Schema      any            `yaml:"schema"`
	Extra       map[string]any `yaml:"extra"`
}

type rawFold struct {
	Name    string         `yaml:"name"`
	Pattern string         `yaml:"pattern"`
	State   any            `yaml:"state"`
	Extra   map[string]any `yaml:"extra"`
}

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

	// Structural validation.
	if raw.Version != 1 {
		return nil, fmt.Errorf("unsupported version %d (want 1)", raw.Version)
	}
	if raw.Component == "" {
		return nil, fmt.Errorf("missing required field 'component'")
	}

	comp := &Component{
		Version:     raw.Version,
		ID:          raw.Component,
		Owns:        raw.Owns,
		Description: raw.Description,
		Vocab:       make(map[string]SchemaNode, len(raw.Vocab)),
		Extra:       raw.Extra,
		SourceFile:  path,
	}

	// Convert vocab.
	for name, schema := range raw.Vocab {
		comp.Vocab[name] = SchemaNode{Raw: schema}
	}

	// Convert receives.
	for i, rs := range raw.Receives {
		slot, err := convertSlot(rs, "receives", i)
		if err != nil {
			return nil, err
		}
		comp.Receives = append(comp.Receives, slot)
	}

	// Convert emits.
	for i, rs := range raw.Emits {
		slot, err := convertSlot(rs, "emits", i)
		if err != nil {
			return nil, err
		}
		comp.Emits = append(comp.Emits, slot)
	}

	// Convert folds.
	for i, rf := range raw.Folds {
		if rf.Name == "" {
			return nil, fmt.Errorf("folds[%d]: missing required field 'name'", i)
		}
		if rf.Pattern == "" {
			return nil, fmt.Errorf("folds[%d] (%s): missing required field 'pattern'", i, rf.Name)
		}
		comp.Folds = append(comp.Folds, Fold{
			Name:    rf.Name,
			Pattern: rf.Pattern,
			State:   SchemaNode{Raw: rf.State},
			Extra:   rf.Extra,
		})
	}

	return comp, nil
}

func convertSlot(rs rawSlot, section string, idx int) (Slot, error) {
	if rs.Kind == "" {
		return Slot{}, fmt.Errorf("%s[%d]: missing required field 'kind'", section, idx)
	}
	role := Role(rs.Role)
	if !role.Valid() {
		return Slot{}, fmt.Errorf("%s[%d] (%s): invalid role %q (want 'action' or 'fact')",
			section, idx, rs.Kind, rs.Role)
	}
	return Slot{
		Kind:        rs.Kind,
		Role:        role,
		Description: rs.Description,
		Exposure:    rs.Exposure,
		Schema:      SchemaNode{Raw: rs.Schema},
		Extra:       rs.Extra,
	}, nil
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
