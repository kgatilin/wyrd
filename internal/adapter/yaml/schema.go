package yaml

// YAML schema v1 for PackageModel interchange format.
// These types define the serialization structure. They map 1:1 to domain types
// but use YAML tags for clean output and omitempty to keep files minimal.

// PackageSpec is the top-level YAML document for a single package.
type PackageSpec struct {
	Schema       string          `yaml:"schema"`                  // "archai/v1"
	Package      string          `yaml:"package"`                 // package path
	Name         string          `yaml:"name"`                    // package name
	Interfaces   []InterfaceSpec `yaml:"interfaces,omitempty"`
	Structs      []StructSpec    `yaml:"structs,omitempty"`
	Functions    []FunctionSpec  `yaml:"functions,omitempty"`
	TypeDefs     []TypeDefSpec   `yaml:"typedefs,omitempty"`
	Dependencies []DependencySpec `yaml:"dependencies,omitempty"`
}

// InterfaceSpec represents an interface definition.
type InterfaceSpec struct {
	Name       string       `yaml:"name"`
	Methods    []MethodSpec `yaml:"methods,omitempty"`
	Exported   bool         `yaml:"exported"`
	SourceFile string       `yaml:"source_file,omitempty"`
	Doc        string       `yaml:"doc,omitempty"`
	Stereotype string       `yaml:"stereotype,omitempty"`
}

// StructSpec represents a struct definition.
type StructSpec struct {
	Name       string       `yaml:"name"`
	Fields     []FieldSpec  `yaml:"fields,omitempty"`
	Methods    []MethodSpec `yaml:"methods,omitempty"`
	Exported   bool         `yaml:"exported"`
	SourceFile string       `yaml:"source_file,omitempty"`
	Doc        string       `yaml:"doc,omitempty"`
	Stereotype string       `yaml:"stereotype,omitempty"`
}

// FunctionSpec represents a package-level function.
type FunctionSpec struct {
	Name       string       `yaml:"name"`
	Params     []ParamSpec  `yaml:"params,omitempty"`
	Returns    []TypeRefSpec `yaml:"returns,omitempty"`
	Exported   bool         `yaml:"exported"`
	SourceFile string       `yaml:"source_file,omitempty"`
	Doc        string       `yaml:"doc,omitempty"`
	Stereotype string       `yaml:"stereotype,omitempty"`
}

// TypeDefSpec represents a type definition (e.g., type Status string).
type TypeDefSpec struct {
	Name           string      `yaml:"name"`
	UnderlyingType TypeRefSpec `yaml:"underlying_type"`
	Constants      []string    `yaml:"constants,omitempty"`
	Exported       bool        `yaml:"exported"`
	SourceFile     string      `yaml:"source_file,omitempty"`
	Doc            string      `yaml:"doc,omitempty"`
	Stereotype     string      `yaml:"stereotype,omitempty"`
}

// MethodSpec represents a method signature.
type MethodSpec struct {
	Name     string       `yaml:"name"`
	Params   []ParamSpec  `yaml:"params,omitempty"`
	Returns  []TypeRefSpec `yaml:"returns,omitempty"`
	Exported bool         `yaml:"exported"`
}

// ParamSpec represents a function/method parameter.
type ParamSpec struct {
	Name string      `yaml:"name,omitempty"`
	Type TypeRefSpec `yaml:"type"`
}

// FieldSpec represents a struct field.
type FieldSpec struct {
	Name     string      `yaml:"name"`
	Type     TypeRefSpec `yaml:"type"`
	Exported bool        `yaml:"exported"`
	Tag      string      `yaml:"tag,omitempty"`
}

// TypeRefSpec represents a type reference.
type TypeRefSpec struct {
	Name      string       `yaml:"name"`
	Package   string       `yaml:"package,omitempty"`
	Pointer   bool         `yaml:"pointer,omitempty"`
	Slice     bool         `yaml:"slice,omitempty"`
	Map       bool         `yaml:"map,omitempty"`
	KeyType   *TypeRefSpec `yaml:"key_type,omitempty"`
	ValueType *TypeRefSpec `yaml:"value_type,omitempty"`
}

// DependencySpec represents a dependency between symbols.
type DependencySpec struct {
	From            SymbolRefSpec `yaml:"from"`
	To              SymbolRefSpec `yaml:"to"`
	Kind            string        `yaml:"kind"` // uses, returns, implements
	ThroughExported bool          `yaml:"through_exported,omitempty"`
}

// SymbolRefSpec represents a reference to a symbol.
type SymbolRefSpec struct {
	Package  string `yaml:"package,omitempty"`
	File     string `yaml:"file,omitempty"`
	Symbol   string `yaml:"symbol"`
	External bool   `yaml:"external,omitempty"`
}
