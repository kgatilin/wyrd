package d2

import (
	"fmt"
	"strings"
)

// d2ReservedKeywords lists the D2 grammar keywords that, when used
// unquoted as a key, are interpreted as built-in attributes rather than
// shape IDs. Matching is case-insensitive in D2, so a Java class named
// "Direction" or "Width" collides with `direction:` / `width:`.
//
// Symbol declarations whose name matches any of these must be emitted
// with the name in double quotes; D2 then treats the quoted form as a
// literal ID. Determined empirically against the D2 v0.x compiler —
// extend as new collisions surface.
var d2ReservedKeywords = map[string]struct{}{
	"shape":            {},
	"label":            {},
	"icon":             {},
	"style":            {},
	"class":            {},
	"classes":          {},
	"constraint":       {},
	"tooltip":          {},
	"link":             {},
	"near":             {},
	"width":            {},
	"height":           {},
	"top":              {},
	"left":             {},
	"vars":             {},
	"layers":           {},
	"scenarios":        {},
	"steps":            {},
	"direction":        {},
	"grid-rows":        {},
	"grid-columns":     {},
	"source-arrowhead": {},
	"target-arrowhead": {},
}

// d2SafeKey returns name as a D2 key, double-quoting it when the name
// matches a reserved keyword. Quoting is the only escape D2 honours for
// reserved-word collisions; an unquoted "Direction:" is parsed as the
// layout direction attribute and rejects child shapes.
func d2SafeKey(name string) string {
	if _, reserved := d2ReservedKeywords[strings.ToLower(name)]; reserved {
		return fmt.Sprintf("%q", name)
	}
	return name
}

// d2QualifiedPath joins a container path with a symbol name, applying
// d2SafeKey to the symbol so reserved-word collisions stay escaped when
// the symbol is referenced via its container path.
func d2QualifiedPath(container, symbol string) string {
	return container + "." + d2SafeKey(symbol)
}
