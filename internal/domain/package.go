package domain

// PackageModel is the aggregate root representing a Go package's structure.
// It contains all the symbols (interfaces, structs, functions, type definitions,
// constants, variables, errors) found in the package, along with their
// dependencies.
type PackageModel struct {
	// Path is the package path relative to the module root, e.g., "internal/service".
	Path string

	// Name is the package name as declared in source files, e.g., "service".
	Name string

	// Interfaces is the list of interface definitions in this package.
	Interfaces []InterfaceDef

	// Structs is the list of struct definitions in this package.
	Structs []StructDef

	// Functions is the list of package-level function definitions.
	Functions []FunctionDef

	// TypeDefs is the list of type definitions (type aliases) in this package.
	TypeDefs []TypeDef

	// Constants is the list of standalone package-level constants. Constants
	// that belong to an enum-like TypeDef are not duplicated here.
	Constants []ConstDef

	// Variables is the list of package-level variables (excluding sentinel
	// errors, which are captured in Errors).
	Variables []VarDef

	// Errors is the list of sentinel error variables (e.g. declarations of
	// the form `var ErrFoo = errors.New(...)`).
	Errors []ErrorDef

	// Dependencies is the list of dependencies between symbols.
	Dependencies []Dependency
}

// SourceFiles returns a deduplicated list of all source files in the package.
func (p PackageModel) SourceFiles() []string {
	seen := make(map[string]bool)
	var files []string

	addFile := func(f string) {
		if f != "" && !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}

	for _, iface := range p.Interfaces {
		addFile(iface.SourceFile)
	}
	for _, s := range p.Structs {
		addFile(s.SourceFile)
	}
	for _, fn := range p.Functions {
		addFile(fn.SourceFile)
	}
	for _, td := range p.TypeDefs {
		addFile(td.SourceFile)
	}
	for _, c := range p.Constants {
		addFile(c.SourceFile)
	}
	for _, v := range p.Variables {
		addFile(v.SourceFile)
	}
	for _, e := range p.Errors {
		addFile(e.SourceFile)
	}

	return files
}

// ExportedInterfaces returns only the exported interfaces.
func (p PackageModel) ExportedInterfaces() []InterfaceDef {
	var result []InterfaceDef
	for _, iface := range p.Interfaces {
		if iface.IsExported {
			result = append(result, iface)
		}
	}
	return result
}

// ExportedStructs returns only the exported structs.
func (p PackageModel) ExportedStructs() []StructDef {
	var result []StructDef
	for _, s := range p.Structs {
		if s.IsExported {
			result = append(result, s)
		}
	}
	return result
}

// ExportedFunctions returns only the exported functions.
func (p PackageModel) ExportedFunctions() []FunctionDef {
	var result []FunctionDef
	for _, fn := range p.Functions {
		if fn.IsExported {
			result = append(result, fn)
		}
	}
	return result
}

// ExportedTypeDefs returns only the exported type definitions.
func (p PackageModel) ExportedTypeDefs() []TypeDef {
	var result []TypeDef
	for _, td := range p.TypeDefs {
		if td.IsExported {
			result = append(result, td)
		}
	}
	return result
}

// ExportedConstants returns only the exported constants.
func (p PackageModel) ExportedConstants() []ConstDef {
	var result []ConstDef
	for _, c := range p.Constants {
		if c.IsExported {
			result = append(result, c)
		}
	}
	return result
}

// ExportedVariables returns only the exported variables.
func (p PackageModel) ExportedVariables() []VarDef {
	var result []VarDef
	for _, v := range p.Variables {
		if v.IsExported {
			result = append(result, v)
		}
	}
	return result
}

// ExportedErrors returns only the exported sentinel errors.
func (p PackageModel) ExportedErrors() []ErrorDef {
	var result []ErrorDef
	for _, e := range p.Errors {
		if e.IsExported {
			result = append(result, e)
		}
	}
	return result
}

// HasExportedSymbols returns true if the package has any exported symbols.
func (p PackageModel) HasExportedSymbols() bool {
	for _, iface := range p.Interfaces {
		if iface.IsExported {
			return true
		}
	}
	for _, s := range p.Structs {
		if s.IsExported {
			return true
		}
	}
	for _, fn := range p.Functions {
		if fn.IsExported {
			return true
		}
	}
	for _, td := range p.TypeDefs {
		if td.IsExported {
			return true
		}
	}
	for _, c := range p.Constants {
		if c.IsExported {
			return true
		}
	}
	for _, v := range p.Variables {
		if v.IsExported {
			return true
		}
	}
	for _, e := range p.Errors {
		if e.IsExported {
			return true
		}
	}
	return false
}
