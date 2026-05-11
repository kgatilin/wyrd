package archgraph

import (
	"fmt"
)

// ModuleID returns the stable id for a module node.
func ModuleID(modulePath string) string {
	return "mod:" + modulePath
}

// PackageID returns the stable id for a package node.
func PackageID(pkgPath string) string {
	return "pkg:" + pkgPath
}

// FileID returns the stable id for a file node.
func FileID(pkgPath, base string) string {
	return "file:" + pkgPath + "/" + base
}

// TypeID returns the stable id for an interface / struct / typedef
// node. The scheme is shared across all three because they live in
// the same package-level type namespace.
func TypeID(pkgPath, name string) string {
	return "type:" + pkgPath + "." + name
}

// FunctionID returns the stable id for a package-level function.
func FunctionID(pkgPath, name string) string {
	return "fn:" + pkgPath + "." + name
}

// MethodID returns the stable id for a method on a struct or
// interface.
func MethodID(pkgPath, recv, name string) string {
	return "method:" + pkgPath + "." + recv + "." + name
}

// FieldID returns the stable id for a field on a struct.
func FieldID(pkgPath, structName, fieldName string) string {
	return "field:" + pkgPath + "." + structName + "." + fieldName
}

// ConstID returns the stable id for a package-level constant.
func ConstID(pkgPath, name string) string {
	return "const:" + pkgPath + "." + name
}

// VarID returns the stable id for a package-level variable.
func VarID(pkgPath, name string) string {
	return "var:" + pkgPath + "." + name
}

// ErrorID returns the stable id for a sentinel error.
func ErrorID(pkgPath, name string) string {
	return "err:" + pkgPath + "." + name
}

// ExternalID returns the stable id for a symbol outside the loaded
// module set.
func ExternalID(pkg, name string) string {
	if pkg == "" {
		return "ext:" + name
	}
	return "ext:" + pkg + "." + name
}

// edgeID composes an edge id from kind and endpoints. Keeping this
// helper internal so all edge ids are formatted identically.
func edgeID(kind EdgeKind, from, to string) string {
	return fmt.Sprintf("%s:%s->%s", string(kind), from, to)
}
