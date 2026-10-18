package golang

import (
	"crypto/sha256"
	"encoding/hex"
	"go/types"
	"strings"
)

// apiHash hashes everything another package's type checker sees of pkg:
// every package-level object with its type, a named type's underlying type
// and methods, a constant's value. Function bodies are not part of it.
//
// Unexported objects are included: an exported type reaches them through
// embedding and inference. So the hash moves on some edits an importer
// cannot observe, and never stays put on one it can.
func apiHash(pkg *types.Package) string {
	if pkg == nil {
		return ""
	}
	var b strings.Builder
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		b.WriteString(types.ObjectString(obj, nil))
		switch o := obj.(type) {
		case *types.Const:
			b.WriteString(" = ")
			b.WriteString(o.Val().ExactString())
		case *types.TypeName:
			if o.IsAlias() {
				break
			}
			b.WriteString(" ")
			b.WriteString(types.TypeString(o.Type().Underlying(), nil))
			if named, ok := o.Type().(*types.Named); ok {
				for i := 0; i < named.NumMethods(); i++ {
					b.WriteString("\n\t")
					b.WriteString(types.ObjectString(named.Method(i), nil))
				}
			}
		}
		b.WriteString("\n")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
