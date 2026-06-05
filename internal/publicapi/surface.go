// Package publicapi projects Archai package models into the exported Go package
// surface used by review scopes and future API targets.
package publicapi

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kgatilin/archai/internal/domain"
)

const Schema = "archai.public-surface/v0"

type Surface struct {
	Schema      string              `json:"schema"`
	Module      string              `json:"module,omitempty"`
	Packages    []Package           `json:"packages"`
	PackageDeps []PackageDependency `json:"packageDeps,omitempty"`
	Warnings    []Warning           `json:"warnings,omitempty"`
}

type Package struct {
	Path    string   `json:"path"`
	Name    string   `json:"name,omitempty"`
	Symbols []Symbol `json:"symbols"`
}

type Symbol struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Signature  string   `json:"signature,omitempty"`
	SourceFile string   `json:"sourceFile,omitempty"`
	Doc        string   `json:"doc,omitempty"`
	Members    []Member `json:"members,omitempty"`
}

type Member struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Signature string `json:"signature,omitempty"`
}

type PackageDependency struct {
	ID          string   `json:"id"`
	FromPackage string   `json:"fromPackage"`
	ToPackage   string   `json:"toPackage"`
	Kinds       []string `json:"kinds"`
}

type Warning struct {
	Package string `json:"package,omitempty"`
	Symbol  string `json:"symbol,omitempty"`
	Message string `json:"message"`
}

type Index struct {
	symbolIDs map[string]struct{}
	memberIDs map[string]struct{}
	depIDs    map[string]struct{}
}

func Project(models []domain.PackageModel) Surface {
	surface := Surface{
		Schema:   Schema,
		Packages: []Package{},
	}

	models = append([]domain.PackageModel(nil), models...)
	sort.Slice(models, func(i, j int) bool { return models[i].Path < models[j].Path })

	knownPackages := make(map[string]struct{}, len(models))
	for _, model := range models {
		knownPackages[model.Path] = struct{}{}
	}

	depKinds := make(map[string]map[string]struct{})
	for _, model := range models {
		pkg := Package{
			Path:    model.Path,
			Name:    model.Name,
			Symbols: publicSymbols(model),
		}
		if len(pkg.Symbols) > 0 {
			surface.Packages = append(surface.Packages, pkg)
		}

		for _, dep := range model.Dependencies {
			if !dep.ThroughExported || dep.To.Package == "" || dep.To.Package == model.Path {
				continue
			}
			if _, ok := knownPackages[dep.To.Package]; !ok {
				continue
			}
			id := packageDependencyID(model.Path, dep.To.Package)
			if depKinds[id] == nil {
				depKinds[id] = make(map[string]struct{})
			}
			depKinds[id][string(dep.Kind)] = struct{}{}
		}
	}

	depIDs := make([]string, 0, len(depKinds))
	for id := range depKinds {
		depIDs = append(depIDs, id)
	}
	sort.Strings(depIDs)
	for _, id := range depIDs {
		from, to := splitPackageDependencyID(id)
		kinds := make([]string, 0, len(depKinds[id]))
		for kind := range depKinds[id] {
			kinds = append(kinds, kind)
		}
		sort.Strings(kinds)
		surface.PackageDeps = append(surface.PackageDeps, PackageDependency{
			ID:          id,
			FromPackage: from,
			ToPackage:   to,
			Kinds:       kinds,
		})
	}

	return surface
}

func publicSymbols(model domain.PackageModel) []Symbol {
	var symbols []Symbol

	for _, iface := range model.Interfaces {
		if !iface.IsExported {
			continue
		}
		id := symbolID(model.Path, iface.Name)
		symbols = append(symbols, Symbol{
			ID:         id,
			Name:       iface.Name,
			Kind:       "interface",
			SourceFile: iface.SourceFile,
			Doc:        iface.Doc,
			Members:    publicMethods(id, iface.Methods),
		})
	}

	for _, strct := range model.Structs {
		if !strct.IsExported {
			continue
		}
		id := symbolID(model.Path, strct.Name)
		members := publicFields(id, strct.Fields)
		members = append(members, publicMethods(id, strct.Methods)...)
		sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
		symbols = append(symbols, Symbol{
			ID:         id,
			Name:       strct.Name,
			Kind:       "struct",
			SourceFile: strct.SourceFile,
			Doc:        strct.Doc,
			Members:    members,
		})
	}

	for _, fn := range model.Functions {
		if !fn.IsExported {
			continue
		}
		symbols = append(symbols, Symbol{
			ID:         symbolID(model.Path, fn.Name),
			Name:       fn.Name,
			Kind:       "function",
			Signature:  fn.Signature(),
			SourceFile: fn.SourceFile,
			Doc:        fn.Doc,
		})
	}

	for _, td := range model.TypeDefs {
		if !td.IsExported {
			continue
		}
		id := symbolID(model.Path, td.Name)
		symbols = append(symbols, Symbol{
			ID:         id,
			Name:       td.Name,
			Kind:       "type",
			Signature:  td.Name + " : " + td.UnderlyingType.String(),
			SourceFile: td.SourceFile,
			Doc:        td.Doc,
			Members:    publicEnumConstants(id, td.Constants),
		})
	}

	for _, c := range model.Constants {
		if !c.IsExported {
			continue
		}
		symbols = append(symbols, Symbol{
			ID:         symbolID(model.Path, c.Name),
			Name:       c.Name,
			Kind:       "const",
			Signature:  constSignature(c),
			SourceFile: c.SourceFile,
			Doc:        c.Doc,
		})
	}

	for _, v := range model.Variables {
		if !v.IsExported {
			continue
		}
		symbols = append(symbols, Symbol{
			ID:         symbolID(model.Path, v.Name),
			Name:       v.Name,
			Kind:       "var",
			Signature:  varSignature(v),
			SourceFile: v.SourceFile,
			Doc:        v.Doc,
		})
	}

	for _, e := range model.Errors {
		if !e.IsExported {
			continue
		}
		symbols = append(symbols, Symbol{
			ID:         symbolID(model.Path, e.Name),
			Name:       e.Name,
			Kind:       "error",
			Signature:  e.Name,
			SourceFile: e.SourceFile,
			Doc:        e.Doc,
		})
	}

	sort.Slice(symbols, func(i, j int) bool { return symbols[i].ID < symbols[j].ID })
	return symbols
}

func publicMethods(ownerID string, methods []domain.MethodDef) []Member {
	var members []Member
	for _, method := range methods {
		if !method.IsExported {
			continue
		}
		members = append(members, Member{
			ID:        memberID(ownerID, method.Name),
			Name:      method.Name,
			Kind:      "method",
			Signature: method.Signature(),
		})
	}
	sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
	return members
}

func publicFields(ownerID string, fields []domain.FieldDef) []Member {
	var members []Member
	for _, field := range fields {
		if !field.IsExported {
			continue
		}
		members = append(members, Member{
			ID:        memberID(ownerID, field.Name),
			Name:      field.Name,
			Kind:      "field",
			Signature: field.String(),
		})
	}
	return members
}

func publicEnumConstants(ownerID string, constants []string) []Member {
	var members []Member
	for _, c := range constants {
		if !isExportedName(c) {
			continue
		}
		members = append(members, Member{
			ID:        memberID(ownerID, c),
			Name:      c,
			Kind:      "const",
			Signature: c,
		})
	}
	sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
	return members
}

func isExportedName(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return r != utf8.RuneError && unicode.IsUpper(r)
}

func constSignature(c domain.ConstDef) string {
	out := c.Name
	if c.Type.Name != "" || c.Type.Package != "" {
		out += " : " + c.Type.String()
	}
	if c.Value != "" {
		out += " = " + c.Value
	}
	return out
}

func varSignature(v domain.VarDef) string {
	out := v.Name
	if v.Type.Name != "" || v.Type.Package != "" {
		out += " : " + v.Type.String()
	}
	return out
}

func (s Surface) Index() Index {
	idx := Index{
		symbolIDs: map[string]struct{}{},
		memberIDs: map[string]struct{}{},
		depIDs:    map[string]struct{}{},
	}
	for _, pkg := range s.Packages {
		for _, symbol := range pkg.Symbols {
			idx.symbolIDs[symbol.ID] = struct{}{}
			for _, member := range symbol.Members {
				idx.memberIDs[member.ID] = struct{}{}
			}
		}
	}
	for _, dep := range s.PackageDeps {
		idx.depIDs[dep.ID] = struct{}{}
	}
	return idx
}

func (idx Index) HasSymbolID(id string) bool {
	_, ok := idx.symbolIDs[id]
	return ok
}

func (idx Index) HasMemberID(id string) bool {
	_, ok := idx.memberIDs[id]
	return ok
}

func (idx Index) HasPackageDependency(fromPackage, toPackage string) bool {
	_, ok := idx.depIDs[packageDependencyID(fromPackage, toPackage)]
	return ok
}

func symbolID(pkg, name string) string {
	return pkg + "." + name
}

func memberID(ownerID, name string) string {
	return ownerID + "." + name
}

func packageDependencyID(fromPackage, toPackage string) string {
	return "e:" + fromPackage + "->" + toPackage
}

func splitPackageDependencyID(id string) (string, string) {
	if !strings.HasPrefix(id, "e:") {
		return "", ""
	}
	from, to, ok := strings.Cut(strings.TrimPrefix(id, "e:"), "->")
	if !ok {
		return "", ""
	}
	return from, to
}
