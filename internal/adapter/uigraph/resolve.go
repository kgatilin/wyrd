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

// diffWord maps a diff.Op-derived string to UI diff status.
//
// The diff package semantics are from the TARGET's perspective:
//   - OpAdd: symbol exists in target but not in current (must add to current)
//   - OpRemove: symbol exists in current but not in target (must remove from current)
//
// The UI shows changes from the CURRENT code's perspective (what did we do?):
//   - OpAdd in diff → current is missing something → "removed" in UI
//   - OpRemove in diff → current has something new → "added" in UI
//   - OpChange → symbol changed → "changed" in UI
func diffWord(op string) string {
	switch op {
	case "add", "Add":
		return "removed" // target has it, current doesn't → current "removed" it
	case "remove", "Remove":
		return "added" // current has it, target doesn't → current "added" it
	case "change", "Change":
		return "changed"
	default:
		return ""
	}
}
