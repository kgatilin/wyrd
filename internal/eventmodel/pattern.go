package eventmodel

import "strings"

// MatchPattern reports whether kind matches the dot-segmented glob pattern.
// Supported wildcards:
//   - "*" matches exactly one segment
//   - ">" or "**" matches one or more trailing segments (must be last)
//
// The pattern dialect is intentionally minimal; see design.md §7 for the
// open question on dialect extensibility.
func MatchPattern(pattern, kind string) bool {
	patParts := strings.Split(pattern, ".")
	kindParts := strings.Split(kind, ".")

	pi, ki := 0, 0
	for pi < len(patParts) && ki < len(kindParts) {
		p := patParts[pi]
		switch p {
		case "*":
			// Matches exactly one segment.
			pi++
			ki++
		case ">", "**":
			// Tail match — consumes all remaining kind segments.
			// Must be the last pattern token and must match at least one segment.
			if pi != len(patParts)-1 {
				return false
			}
			// ki is currently pointing at a segment, and there may be more.
			// The tail wildcard matches ki..end, which is at least one segment.
			return len(kindParts)-ki >= 1
		default:
			// Literal match.
			if p != kindParts[ki] {
				return false
			}
			pi++
			ki++
		}
	}

	// Both exhausted = match; leftover pattern or kind = no match,
	// unless the only remaining pattern token is a tail wildcard.
	if pi == len(patParts) && ki == len(kindParts) {
		return true
	}
	// Pattern exhausted but kind has leftover segments = no match.
	// Kind exhausted but pattern has leftover = check if it's a tail wildcard.
	if ki == len(kindParts) && pi == len(patParts)-1 {
		p := patParts[pi]
		if p == ">" || p == "**" {
			// Tail wildcard but no remaining kind segments. Since > matches 1+,
			// this is only valid if there are zero remaining (which means ki
			// already consumed all and we need at least one more — so no match).
			return false
		}
	}
	return false
}
