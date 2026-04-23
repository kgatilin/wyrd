// Package diff defines a structured, format-agnostic representation of the
// differences between two snapshots of a project's architecture model
// (typically the "current" code-derived model and a locked target).
//
// The types in this package are intentionally small and free of behavior so
// that they can be marshalled to YAML/JSON or rendered to a terminal without
// coupling to any particular output format.
package diff

// Op identifies the kind of change a Change entry describes.
type Op string

const (
	// OpAdd indicates a symbol present in the target model but missing in
	// the current model — i.e. something that still needs to be added to
	// the codebase to match the target.
	OpAdd Op = "add"

	// OpRemove indicates a symbol present in the current model but missing
	// in the target model — i.e. something in the code that the target
	// wants gone.
	OpRemove Op = "remove"

	// OpChange indicates a symbol that exists on both sides but whose
	// representation differs. The Change entry carries Before (current)
	// and After (target) snapshots.
	OpChange Op = "change"
)

// Kind identifies the architectural element a Change entry refers to.
type Kind string

const (
	KindPackage   Kind = "package"
	KindInterface Kind = "interface"
	KindStruct    Kind = "struct"
	KindFunction  Kind = "function"
	KindMethod    Kind = "method"
	KindField     Kind = "field"
	KindConst     Kind = "const"
	KindVar       Kind = "var"
	KindError     Kind = "error"
	KindDep       Kind = "dep"
	KindLayerRule Kind = "layer-rule"
	KindTypeDef   Kind = "typedef"
)

// Change describes a single difference between two models.
//
// Path is a dotted identifier that locates the element inside the model,
// e.g. "internal/service" for a package, "internal/service.Service" for a
// struct or interface, or "internal/service.Service.Handle" for a method.
// Before/After are populated for OpChange; for OpAdd only After is set, for
// OpRemove only Before is set.
type Change struct {
	Op     Op     `yaml:"op" json:"op"`
	Kind   Kind   `yaml:"kind" json:"kind"`
	Path   string `yaml:"path" json:"path"`
	Before any    `yaml:"before,omitempty" json:"before,omitempty"`
	After  any    `yaml:"after,omitempty" json:"after,omitempty"`
}

// Diff is the ordered list of Changes produced by Compute.
type Diff struct {
	Changes []Change `yaml:"changes" json:"changes"`
}

// IsEmpty reports whether d contains no changes.
func (d *Diff) IsEmpty() bool {
	return d == nil || len(d.Changes) == 0
}
