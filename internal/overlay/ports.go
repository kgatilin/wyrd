package overlay

import (
	"fmt"
	"strings"
)

// Ports declares the exported symbols that are reached from outside the
// dependency graph — plugin hooks, reflection and registration targets,
// generated code. Nothing in the code reaches them by an edge wyrd can see,
// so without the declaration an unused-export check would report them as dead.
//
// The section is a statement of intent, not a heuristic: whatever is listed
// here is a port because the project says so.
type Ports struct {
	External []string `yaml:"external,omitempty"`
}

// ExternalPort is one parsed ports.external selector. Exactly one of the two
// forms is populated:
//
//   - Glob: a package glob in overlay glob syntax ("internal/plugins/...");
//     every export of every matching package is a port.
//   - Package plus Symbol (and optionally Member): one symbol
//     ("internal/adapter/mcp.Dispatch") or one member of a type
//     ("internal/serve.State.Snapshot").
type ExternalPort struct {
	// Raw is the selector as written, for diagnostics.
	Raw string
	// Glob is the package glob, empty for symbol selectors.
	Glob string
	// Package, Symbol and Member name a single declaration. Package and
	// Symbol are empty for glob selectors; Member is empty unless the
	// selector names a method or field of Symbol.
	Package string
	Symbol  string
	Member  string
}

// Matches reports whether the selector covers the declaration pkg.symbol, or
// pkg.symbol.member when member is non-empty. A glob selector covers every
// declaration of a package it matches, so it ignores symbol and member.
func (p ExternalPort) Matches(pkg, symbol, member string) bool {
	if p.Glob != "" {
		return matchGlob(p.Glob, pkg)
	}
	if p.Package != pkg || p.Symbol != symbol {
		return false
	}
	// A type-level declaration (member == "") is covered only by a
	// type-level selector: "pkg.State" is not a licence for every method
	// of State, and "pkg.State.Snapshot" is not one for State itself.
	return p.Member == member
}

// ParseExternalPort parses one ports.external selector. A selector whose last
// path segment carries a dot names a symbol; anything else is a package glob.
func ParseExternalPort(raw string) (ExternalPort, error) {
	sel := strings.TrimSpace(raw)
	if sel == "" {
		return ExternalPort{}, fmt.Errorf("port selector is empty")
	}
	if sel != raw {
		return ExternalPort{}, fmt.Errorf("port selector %q has leading/trailing whitespace", raw)
	}
	if strings.ContainsAny(sel, " \t\n") {
		return ExternalPort{}, fmt.Errorf("port selector %q contains whitespace", raw)
	}
	if strings.HasPrefix(sel, "/") {
		return ExternalPort{}, fmt.Errorf("port selector %q must be a relative package path", raw)
	}

	head, last := splitLastSegment(sel)
	// "..." and "*" are glob syntax, not a symbol separator, so a segment
	// built out of them stays a package glob however many dots it holds.
	if !strings.Contains(last, ".") || strings.Contains(sel, "*") || strings.Contains(last, "...") {
		if err := validateGlobSyntax(sel); err != nil {
			return ExternalPort{}, fmt.Errorf("port selector %q: %w", raw, err)
		}
		return ExternalPort{Raw: raw, Glob: sel}, nil
	}

	parts := strings.Split(last, ".")
	pkgTail, names := parts[0], parts[1:]
	if pkgTail == "" {
		return ExternalPort{}, fmt.Errorf("port selector %q has an empty package path", raw)
	}
	if len(names) > 2 {
		return ExternalPort{}, fmt.Errorf(
			"port selector %q has too many name parts (expected \"pkg.Symbol\" or \"pkg.Type.Member\")", raw)
	}
	for _, name := range names {
		if name == "" {
			return ExternalPort{}, fmt.Errorf("port selector %q has an empty name part", raw)
		}
	}
	port := ExternalPort{Raw: raw, Package: joinSegment(head, pkgTail), Symbol: names[0]}
	if len(names) == 2 {
		port.Member = names[1]
	}
	return port, nil
}

func splitLastSegment(sel string) (head, last string) {
	if i := strings.LastIndexByte(sel, '/'); i >= 0 {
		return sel[:i], sel[i+1:]
	}
	return "", sel
}

func joinSegment(head, tail string) string {
	if head == "" {
		return tail
	}
	return head + "/" + tail
}

// validateGlobSyntax checks a package glob for the same syntactic problems
// validateGlob reports, without the layer-flavoured wording.
func validateGlobSyntax(glob string) error {
	if idx := strings.Index(glob, "..."); idx >= 0 && idx != len(glob)-3 {
		return fmt.Errorf("\"...\" may only appear at the end")
	}
	return nil
}
