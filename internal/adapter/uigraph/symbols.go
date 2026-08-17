package uigraph

import (
	"strconv"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
)

// This file turns domain symbols into the two-column shape the package card
// renders: a bare name on the left, a type on the right. It mirrors what the
// D2 writer does for a class shape (internal/adapter/d2/builder.go) so the
// browser card and the generated .d2 diagram describe a symbol the same way.

// formatParams renders a parameter list as it appears between the parentheses
// of a signature.
func formatParams(params []domain.ParamDef) string {
	parts := make([]string, 0, len(params))
	for _, p := range params {
		parts = append(parts, p.String())
	}
	return strings.Join(parts, ", ")
}

// formatReturns renders return types as a single right-hand column value.
// Multiple returns are parenthesized, matching Go's own syntax.
func formatReturns(returns []domain.TypeRef) string {
	if len(returns) == 0 {
		return ""
	}
	parts := make([]string, 0, len(returns))
	for _, r := range returns {
		parts = append(parts, r.String())
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// methodMember builds a class-body row for an interface or struct method.
func methodMember(parentID string, m domain.MethodDef, sourceFile string, exported bool) Member {
	return Member{
		ID:         parentID + "." + m.Name,
		Kind:       "method",
		Name:       domain.NameWithTypeParams(m.Name, m.TypeParams),
		Params:     formatParams(m.Params),
		Type:       formatReturns(m.Returns),
		SourceFile: sourceFile,
		Exported:   exported,
	}
}

// fieldMember builds a class-body row for a struct field.
func fieldMember(parentID string, f domain.FieldDef, sourceFile string, exported bool) Member {
	return Member{
		ID:         parentID + "." + f.Name,
		Kind:       "prop",
		Name:       f.Name,
		Type:       f.Type.String(),
		SourceFile: sourceFile,
		Exported:   exported,
	}
}

// functionMembers builds the class body of a function: one row per parameter
// plus a "return" row. Rows inherit the function's public-API reachability —
// a parameter is only as reachable as the function that takes it, and it has
// no identity of its own in the public API index.
func functionMembers(parentID string, fn domain.FunctionDef, sourceFile string, exported bool) []Member {
	members := make([]Member, 0, len(fn.Params)+1)
	for i, p := range fn.Params {
		members = append(members, Member{
			// Parameter names are optional in Go and need not be unique, so the
			// index keeps row IDs stable and collision-free.
			ID:         parentID + ".param." + strconv.Itoa(i),
			Kind:       "param",
			Name:       p.Name,
			Type:       p.Type.String(),
			SourceFile: sourceFile,
			Exported:   exported,
		})
	}
	if ret := formatReturns(fn.Returns); ret != "" {
		members = append(members, Member{
			ID:         parentID + ".return",
			Kind:       "return",
			Name:       "return",
			Type:       ret,
			SourceFile: sourceFile,
			Exported:   exported,
		})
	}
	return members
}

// typeDefTypeLabel is the right-hand column of a type definition: its
// underlying type.
func typeDefTypeLabel(td domain.TypeDef) string {
	return td.UnderlyingType.String()
}

// constTypeLabel is the right-hand column of a constant: its declared type and
// literal value, either of which may be absent for untyped or computed consts.
func constTypeLabel(c domain.ConstDef) string {
	label := ""
	if c.Type.Name != "" || c.Type.Package != "" {
		label = c.Type.String()
	}
	if c.Value == "" {
		return label
	}
	if label == "" {
		return "= " + c.Value
	}
	return label + " = " + c.Value
}

// varTypeLabel is the right-hand column of a package-level variable.
func varTypeLabel(v domain.VarDef) string {
	if v.Type.Name == "" && v.Type.Package == "" {
		return ""
	}
	return v.Type.String()
}

// errorTypeLabel is the right-hand column of a sentinel error: its message.
func errorTypeLabel(e domain.ErrorDef) string {
	if e.Message == "" {
		return ""
	}
	return `"` + e.Message + `"`
}
