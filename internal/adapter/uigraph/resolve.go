package uigraph

import "strings"

type changePath struct {
	Pkg, Type, Member, Level string
}

// parseChangePath splits a diff path of the form
//
//	<pkg-path>[.<Type>[.<Member>]]
//
// The package path may itself contain dots (e.g. github.com/x/y), so we only
// treat dots AFTER the final '/' segment as Type/Member separators.
func parseChangePath(p string) changePath {
	slash := strings.LastIndex(p, "/")
	head, tail := "", p
	if slash >= 0 {
		head, tail = p[:slash+1], p[slash+1:] // head keeps trailing '/'
	}
	parts := strings.SplitN(tail, ".", 3)
	cp := changePath{Pkg: head + parts[0], Level: "package"}
	if len(parts) >= 2 && parts[1] != "" {
		cp.Type = parts[1]
		cp.Level = "type"
	}
	if len(parts) >= 3 && parts[2] != "" {
		cp.Member = parts[2]
		cp.Level = "member"
	}
	return cp
}

// diffWord maps a diff.Op-derived string. Callers pass the op as produced by
// the diff package's String()/marshalling. Keep mapping centralized here.
func diffWord(op string) string {
	switch op {
	case "add", "added", "Add":
		return "added"
	case "remove", "removed", "Remove":
		return "removed"
	case "change", "changed", "Change":
		return "changed"
	default:
		return ""
	}
}
