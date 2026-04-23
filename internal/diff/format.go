package diff

import (
	"encoding/json"
	"fmt"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
)

// FormatText renders a Diff as a plain, human-readable listing. Each change
// is a single line prefixed with "+" for add, "-" for remove, "~" for change.
// A trailing summary line counts per-op totals.
func FormatText(d *Diff) string {
	if d == nil || len(d.Changes) == 0 {
		return "No changes.\n"
	}

	var sb strings.Builder
	var added, removed, changed int
	for _, c := range d.Changes {
		switch c.Op {
		case OpAdd:
			fmt.Fprintf(&sb, "+ added %s %s\n", c.Kind, c.Path)
			added++
		case OpRemove:
			fmt.Fprintf(&sb, "- removed %s %s\n", c.Kind, c.Path)
			removed++
		case OpChange:
			fmt.Fprintf(&sb, "~ changed %s %s\n", c.Kind, c.Path)
			changed++
		default:
			fmt.Fprintf(&sb, "? %s %s %s\n", c.Op, c.Kind, c.Path)
		}
	}
	fmt.Fprintf(&sb, "\n%d added, %d removed, %d changed\n", added, removed, changed)
	return sb.String()
}

// FormatYAML marshals the Diff to a YAML document.
func FormatYAML(d *Diff) (string, error) {
	if d == nil {
		d = &Diff{}
	}
	out, err := yamlv3.Marshal(d)
	if err != nil {
		return "", fmt.Errorf("diff: marshal yaml: %w", err)
	}
	return string(out), nil
}

// FormatJSON marshals the Diff to indented JSON.
func FormatJSON(d *Diff) (string, error) {
	if d == nil {
		d = &Diff{}
	}
	out, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "", fmt.Errorf("diff: marshal json: %w", err)
	}
	return string(out) + "\n", nil
}
