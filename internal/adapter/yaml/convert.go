package yaml

import "github.com/kgatilin/archai/internal/domain"

// toSpec converts a domain PackageModel to the YAML schema.
func toSpec(model domain.PackageModel, publicOnly bool) PackageSpec {
	spec := PackageSpec{
		Schema:  "archai/v1",
		Package: model.Path,
		Name:    model.Name,
	}

	ifaces := model.Interfaces
	if publicOnly {
		ifaces = model.ExportedInterfaces()
	}
	for _, iface := range ifaces {
		spec.Interfaces = append(spec.Interfaces, toInterfaceSpec(iface))
	}

	structs := model.Structs
	if publicOnly {
		structs = model.ExportedStructs()
	}
	for _, s := range structs {
		spec.Structs = append(spec.Structs, toStructSpec(s))
	}

	fns := model.Functions
	if publicOnly {
		fns = model.ExportedFunctions()
	}
	for _, fn := range fns {
		spec.Functions = append(spec.Functions, toFunctionSpec(fn))
	}

	tds := model.TypeDefs
	if publicOnly {
		tds = model.ExportedTypeDefs()
	}
	for _, td := range tds {
		spec.TypeDefs = append(spec.TypeDefs, toTypeDefSpec(td))
	}

	for _, dep := range model.Dependencies {
		if publicOnly && !dep.ThroughExported {
			continue
		}
		spec.Dependencies = append(spec.Dependencies, toDependencySpec(dep))
	}

	return spec
}

// fromSpec converts a YAML schema back to a domain PackageModel.
func fromSpec(spec PackageSpec) domain.PackageModel {
	model := domain.PackageModel{
		Path: spec.Package,
		Name: spec.Name,
	}

	for _, iface := range spec.Interfaces {
		model.Interfaces = append(model.Interfaces, fromInterfaceSpec(iface))
	}
	for _, s := range spec.Structs {
		model.Structs = append(model.Structs, fromStructSpec(s))
	}
	for _, fn := range spec.Functions {
		model.Functions = append(model.Functions, fromFunctionSpec(fn))
	}
	for _, td := range spec.TypeDefs {
		model.TypeDefs = append(model.TypeDefs, fromTypeDefSpec(td))
	}
	for _, dep := range spec.Dependencies {
		model.Dependencies = append(model.Dependencies, fromDependencySpec(dep))
	}

	return model
}

// ─── to-spec converters ─────────────────────────────────────────────────────

func toInterfaceSpec(i domain.InterfaceDef) InterfaceSpec {
	spec := InterfaceSpec{
		Name:       i.Name,
		Exported:   i.IsExported,
		SourceFile: i.SourceFile,
		Doc:        i.Doc,
		Stereotype: string(i.Stereotype),
	}
	for _, m := range i.Methods {
		spec.Methods = append(spec.Methods, toMethodSpec(m))
	}
	return spec
}

func toStructSpec(s domain.StructDef) StructSpec {
	spec := StructSpec{
		Name:       s.Name,
		Exported:   s.IsExported,
		SourceFile: s.SourceFile,
		Doc:        s.Doc,
		Stereotype: string(s.Stereotype),
	}
	for _, f := range s.Fields {
		spec.Fields = append(spec.Fields, toFieldSpec(f))
	}
	for _, m := range s.Methods {
		spec.Methods = append(spec.Methods, toMethodSpec(m))
	}
	return spec
}

func toFunctionSpec(f domain.FunctionDef) FunctionSpec {
	spec := FunctionSpec{
		Name:       f.Name,
		Exported:   f.IsExported,
		SourceFile: f.SourceFile,
		Doc:        f.Doc,
		Stereotype: string(f.Stereotype),
	}
	for _, p := range f.Params {
		spec.Params = append(spec.Params, toParamSpec(p))
	}
	for _, r := range f.Returns {
		spec.Returns = append(spec.Returns, toTypeRefSpec(r))
	}
	return spec
}

func toTypeDefSpec(t domain.TypeDef) TypeDefSpec {
	return TypeDefSpec{
		Name:           t.Name,
		UnderlyingType: toTypeRefSpec(t.UnderlyingType),
		Constants:      t.Constants,
		Exported:       t.IsExported,
		SourceFile:     t.SourceFile,
		Doc:            t.Doc,
		Stereotype:     string(t.Stereotype),
	}
}

func toMethodSpec(m domain.MethodDef) MethodSpec {
	spec := MethodSpec{
		Name:     m.Name,
		Exported: m.IsExported,
	}
	for _, p := range m.Params {
		spec.Params = append(spec.Params, toParamSpec(p))
	}
	for _, r := range m.Returns {
		spec.Returns = append(spec.Returns, toTypeRefSpec(r))
	}
	return spec
}

func toParamSpec(p domain.ParamDef) ParamSpec {
	return ParamSpec{
		Name: p.Name,
		Type: toTypeRefSpec(p.Type),
	}
}

func toFieldSpec(f domain.FieldDef) FieldSpec {
	return FieldSpec{
		Name:     f.Name,
		Type:     toTypeRefSpec(f.Type),
		Exported: f.IsExported,
		Tag:      f.Tag,
	}
}

func toTypeRefSpec(t domain.TypeRef) TypeRefSpec {
	spec := TypeRefSpec{
		Name:    t.Name,
		Package: t.Package,
		Pointer: t.IsPointer,
		Slice:   t.IsSlice,
		Map:     t.IsMap,
	}
	if t.KeyType != nil {
		kt := toTypeRefSpec(*t.KeyType)
		spec.KeyType = &kt
	}
	if t.ValueType != nil {
		vt := toTypeRefSpec(*t.ValueType)
		spec.ValueType = &vt
	}
	return spec
}

func toDependencySpec(d domain.Dependency) DependencySpec {
	return DependencySpec{
		From:            toSymbolRefSpec(d.From),
		To:              toSymbolRefSpec(d.To),
		Kind:            string(d.Kind),
		ThroughExported: d.ThroughExported,
	}
}

func toSymbolRefSpec(r domain.SymbolRef) SymbolRefSpec {
	return SymbolRefSpec{
		Package:  r.Package,
		File:     r.File,
		Symbol:   r.Symbol,
		External: r.External,
	}
}

// ─── from-spec converters ───────────────────────────────────────────────────

func fromInterfaceSpec(s InterfaceSpec) domain.InterfaceDef {
	iface := domain.InterfaceDef{
		Name:       s.Name,
		IsExported: s.Exported,
		SourceFile: s.SourceFile,
		Doc:        s.Doc,
		Stereotype: domain.Stereotype(s.Stereotype),
	}
	for _, m := range s.Methods {
		iface.Methods = append(iface.Methods, fromMethodSpec(m))
	}
	return iface
}

func fromStructSpec(s StructSpec) domain.StructDef {
	st := domain.StructDef{
		Name:       s.Name,
		IsExported: s.Exported,
		SourceFile: s.SourceFile,
		Doc:        s.Doc,
		Stereotype: domain.Stereotype(s.Stereotype),
	}
	for _, f := range s.Fields {
		st.Fields = append(st.Fields, fromFieldSpec(f))
	}
	for _, m := range s.Methods {
		st.Methods = append(st.Methods, fromMethodSpec(m))
	}
	return st
}

func fromFunctionSpec(s FunctionSpec) domain.FunctionDef {
	fn := domain.FunctionDef{
		Name:       s.Name,
		IsExported: s.Exported,
		SourceFile: s.SourceFile,
		Doc:        s.Doc,
		Stereotype: domain.Stereotype(s.Stereotype),
	}
	for _, p := range s.Params {
		fn.Params = append(fn.Params, fromParamSpec(p))
	}
	for _, r := range s.Returns {
		fn.Returns = append(fn.Returns, fromTypeRefSpec(r))
	}
	return fn
}

func fromTypeDefSpec(s TypeDefSpec) domain.TypeDef {
	return domain.TypeDef{
		Name:           s.Name,
		UnderlyingType: fromTypeRefSpec(s.UnderlyingType),
		Constants:      s.Constants,
		IsExported:     s.Exported,
		SourceFile:     s.SourceFile,
		Doc:            s.Doc,
		Stereotype:     domain.Stereotype(s.Stereotype),
	}
}

func fromMethodSpec(s MethodSpec) domain.MethodDef {
	m := domain.MethodDef{
		Name:       s.Name,
		IsExported: s.Exported,
	}
	for _, p := range s.Params {
		m.Params = append(m.Params, fromParamSpec(p))
	}
	for _, r := range s.Returns {
		m.Returns = append(m.Returns, fromTypeRefSpec(r))
	}
	return m
}

func fromParamSpec(s ParamSpec) domain.ParamDef {
	return domain.ParamDef{
		Name: s.Name,
		Type: fromTypeRefSpec(s.Type),
	}
}

func fromFieldSpec(s FieldSpec) domain.FieldDef {
	return domain.FieldDef{
		Name:       s.Name,
		Type:       fromTypeRefSpec(s.Type),
		IsExported: s.Exported,
		Tag:        s.Tag,
	}
}

func fromTypeRefSpec(s TypeRefSpec) domain.TypeRef {
	t := domain.TypeRef{
		Name:      s.Name,
		Package:   s.Package,
		IsPointer: s.Pointer,
		IsSlice:   s.Slice,
		IsMap:     s.Map,
	}
	if s.KeyType != nil {
		kt := fromTypeRefSpec(*s.KeyType)
		t.KeyType = &kt
	}
	if s.ValueType != nil {
		vt := fromTypeRefSpec(*s.ValueType)
		t.ValueType = &vt
	}
	return t
}

func fromDependencySpec(s DependencySpec) domain.Dependency {
	return domain.Dependency{
		From:            fromSymbolRefSpec(s.From),
		To:              fromSymbolRefSpec(s.To),
		Kind:            domain.DependencyKind(s.Kind),
		ThroughExported: s.ThroughExported,
	}
}

func fromSymbolRefSpec(s SymbolRefSpec) domain.SymbolRef {
	return domain.SymbolRef{
		Package:  s.Package,
		File:     s.File,
		Symbol:   s.Symbol,
		External: s.External,
	}
}
