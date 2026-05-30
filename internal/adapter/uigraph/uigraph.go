// Package uigraph projects archai's domain model + overlay + diff into the
// UIGraph JSON shape consumed by the POC review UI. Pure data + a pure
// projection function; no I/O, no behavior on the types.
package uigraph

const Schema = "archai.uigraph/v0"

type UIGraph struct {
	Schema          string           `json:"schema"`
	PR              *PR              `json:"pr,omitempty"`
	BoundedContexts []BoundedContext `json:"boundedContexts"`
	Components      []Component      `json:"components"`
	Edges           []Edge           `json:"edges"`
	Comments        []Comment        `json:"comments"`
}

type PR struct {
	Title   string `json:"title"`
	Branch  string `json:"branch"`
	Agent   string `json:"agent"`
	Summary string `json:"summary"`
	Stats   Stats  `json:"stats"`
}

type Stats struct {
	Added    int `json:"added"`
	Removed  int `json:"removed"`
	Changed  int `json:"changed"`
	Comments int `json:"comments"`
}

type BoundedContext struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Component struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Tech      string     `json:"tech"`
	Desc      string     `json:"desc"`
	BC        string     `json:"bc"`
	Diff      string     `json:"diff,omitempty"` // added|removed|changed
	Internals []Internal `json:"internals"`
	Ports     []Port     `json:"ports"`
}

type Internal struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"` // class|iface
	Name    string   `json:"name"`
	Diff    string   `json:"diff,omitempty"`
	Members []Member `json:"members"`
}

type Member struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // method|prop
	Name string `json:"name"`
	Diff string `json:"diff,omitempty"`
}

type Port struct {
	ID   string `json:"id"`
	Side string `json:"side"` // left|right
	Kind string `json:"kind"` // in|out
	Name string `json:"name"`
	Diff string `json:"diff,omitempty"`
}

type Edge struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	To       string `json:"to"`
	FromPort string `json:"fromPort"`
	ToPort   string `json:"toPort"`
	Label    string `json:"label"`
	Diff     string `json:"diff,omitempty"`
}

type Comment struct {
	ID     string        `json:"id"`
	Target CommentTarget `json:"target"`
	Body   string        `json:"body"`
}

type CommentTarget struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
