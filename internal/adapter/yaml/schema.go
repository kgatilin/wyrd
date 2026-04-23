package yaml

// YAML schema v1 for PackageModel interchange format.
// These types define the serialization structure. They map 1:1 to domain types
// but use YAML tags for clean output and omitempty to keep files minimal.

// PackageSpec is the top-level YAML document for a single package.
type PackageSpec struct {
	Schema       string           `yaml:"schema"`                  // "archai/v1"
	Package      string           `yaml:"package"`                 // package path
	Name         string           `yaml:"name"`                    // package name
	Layer        string           `yaml:"layer,omitempty"`         // overlay-assigned layer
	Aggregate    string           `yaml:"aggregate,omitempty"`     // overlay-assigned aggregate
	Interfaces   []InterfaceSpec  `yaml:"interfaces,omitempty"`
	Structs      []StructSpec     `yaml:"structs,omitempty"`
	Functions    []FunctionSpec   `yaml:"functions,omitempty"`
	TypeDefs     []TypeDefSpec    `yaml:"typedefs,omitempty"`
	Constants    []ConstSpec      `yaml:"constants,omitempty"`
	Variables    []VarSpec        `yaml:"variables,omitempty"`
	Errors       []ErrorSpec      `yaml:"errors,omitempty"`
	Dependencies []DependencySpec `yaml:"dependencies,omitempty"`
	Implementations []ImplementationSpec `yaml:"implementations,omitempty"`
}

// ImplementationSpec represents an interface implementation relationship.
type ImplementationSpec struct {
	Concrete  SymbolRefSpec `yaml:"concrete"`
	Interface SymbolRefSpec `yaml:"interface"`
	Pointer   bool          `yaml:"pointer,omitempty"`
}

// ConstSpec represents a package-level constant declaration.
type ConstSpec struct {
	Name       string      `yaml:"name"`
	Type       TypeRefSpec `yaml:"type,omitempty"`
	Value      string      `yaml:"value,omitempty"`
	Exported   bool        `yaml:"exported"`
	SourceFile string      `yaml:"source_file,omitempty"`
	Doc        string      `yaml:"doc,omitempty"`
}

// VarSpec represents a package-level variable declaration.
type VarSpec struct {
	Name       string      `yaml:"name"`
	Type       TypeRefSpec `yaml:"type,omitempty"`
	Exported   bool        `yaml:"exported"`
	SourceFile string      `yaml:"source_file,omitempty"`
	Doc        string      `yaml:"doc,omitempty"`
}

// ErrorSpec represents a sentinel error declaration.
type ErrorSpec struct {
	Name       string `yaml:"name"`
	Message    string `yaml:"message,omitempty"`
	Exported   bool   `yaml:"exported"`
	SourceFile string `yaml:"source_file,omitempty"`
	Doc        string `yaml:"doc,omitempty"`
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
	Calls      []CallEdgeSpec `yaml:"calls,omitempty"`
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
	Calls    []CallEdgeSpec `yaml:"calls,omitempty"`
}

// CallEdgeSpec represents a static call edge from a function/method body
// to another function/method.
type CallEdgeSpec struct {
	To  SymbolRefSpec `yaml:"to"`
	Via string        `yaml:"via,omitempty"`
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
