package http

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/diff"
)

// renderDiffD2 returns a deterministic D2 source for the given diff,
// one node per changed symbol plus edges from parent packages to the
// changes they contain.
func renderDiffD2(d *diff.Diff, filter string) string {
	var b strings.Builder
	if d == nil {
		return "title: \"(no diff)\"\n"
	}
	b.WriteString("title: {\n  label: \"Diff overlay\"\n  near: top-center\n  shape: text\n}\n")

	parents := make(map[string]struct{})
	for _, c := range d.Changes {
		if filter != "" && string(c.Kind) != filter {
			continue
		}
		parent := diffParentID(c.Path)
		if parent != "" {
			parents[parent] = struct{}{}
		}
		color := diffOpColor(string(c.Op))
		fmt.Fprintf(&b, "%s: {\n  label: %s\n  style.stroke: %q\n}\n",
			d2ID(string(c.Kind)+":"+c.Path),
			quoteD2(c.Path),
			color)
		if parent != "" {
			fmt.Fprintf(&b, "%s -> %s\n",
				d2ID(parent), d2ID(string(c.Kind)+":"+c.Path))
		}
	}
	parentIDs := make([]string, 0, len(parents))
	for p := range parents {
		parentIDs = append(parentIDs, p)
	}
	sort.Strings(parentIDs)
	for _, p := range parentIDs {
		fmt.Fprintf(&b, "%s: {\n  label: %s\n  shape: package\n}\n", d2ID(p), quoteD2(p))
	}
	return b.String()
}

func diffOpColor(op string) string {
	switch op {
	case "add":
		return "#16a34a"
	case "remove":
		return "#dc2626"
	case "change":
		return "#d97706"
	}
	return "#64748b"
}
