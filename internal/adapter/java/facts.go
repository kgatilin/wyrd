// Package java reads Java source by shelling out to archai-java-analyzer.jar
// (issue #101) and translates its JavaFacts JSON output to domain models.
//
// The adapter is split in three layers:
//
//   - facts.go       — Go structs mirroring the JavaFacts/v1 JSON shape.
//   - exec.go        — JAR/`java` resolution and process execution.
//   - translator.go  — pure JavaFacts → []domain.PackageModel translation.
//   - reader.go      — wires the three together as a service.ModelReader.
//
// The schema is owned by the Go side (see tools/archai-java-analyzer/SCHEMA.md):
// when archai's domain evolves, only translator.go changes; the JAR is left
// alone unless the JSON shape itself needs new fields.
package java

// schemaVersion is the JavaFacts schema version this adapter understands.
const schemaVersion = "javafacts/v1"

// javaFacts mirrors the top-level JavaFacts JSON document.
type javaFacts struct {
	Schema        string         `json:"schema"`
	SrcRoots      []string       `json:"src_roots"`
	Packages      []string       `json:"packages"`
	Classes       []javaClass    `json:"classes"`
	Imports       []javaImport   `json:"imports"`
	ParseWarnings []parseWarning `json:"parse_warnings"`
}

// javaClass mirrors a JavaClass entry. `kind` is one of
// class | interface | enum | record | annotation.
type javaClass struct {
	FQN            string           `json:"fqn"`
	Package        string           `json:"package"`
	Name           string           `json:"name"`
	Kind           string           `json:"kind"`
	Modifiers      []string         `json:"modifiers"`
	TypeParameters []string         `json:"type_parameters"`
	Extends        string           `json:"extends"`
	Implements     []string         `json:"implements"`
	Permits        []string         `json:"permits"`
	SourceFile     string           `json:"source_file"`
	Doc            string           `json:"doc"`
	Annotations    []javaAnnotation `json:"annotations"`
	Fields         []javaField      `json:"fields"`
	Methods        []javaMethod     `json:"methods"`
	EnumConstants  []string         `json:"enum_constants"`
}

type javaField struct {
	Name        string           `json:"name"`
	Type        string           `json:"type"`
	Modifiers   []string         `json:"modifiers"`
	Annotations []javaAnnotation `json:"annotations"`
	Doc         string           `json:"doc"`
}

// javaMethod mirrors a JavaMethod entry. `kind` is method | constructor.
type javaMethod struct {
	Name           string           `json:"name"`
	Kind           string           `json:"kind"`
	Modifiers      []string         `json:"modifiers"`
	TypeParameters []string         `json:"type_parameters"`
	Params         []javaParam      `json:"params"`
	Returns        string           `json:"returns"`
	Throws         []string         `json:"throws"`
	Annotations    []javaAnnotation `json:"annotations"`
	Doc            string           `json:"doc"`
	Calls          []javaCall       `json:"calls"`
}

type javaParam struct {
	Name        string           `json:"name"`
	Type        string           `json:"type"`
	Varargs     bool             `json:"varargs"`
	Modifiers   []string         `json:"modifiers"`
	Annotations []javaAnnotation `json:"annotations"`
}

type javaCall struct {
	ToClass    string             `json:"to_class"`
	ToMethod   string             `json:"to_method"`
	Static     bool               `json:"static"`
	External   bool               `json:"external"`
	TargetFQN  string             `json:"target_fqn"`
	Unresolved javaCallUnresolved `json:"unresolved"`
}

type javaCallUnresolved struct {
	ReceiverText string `json:"receiver_text"`
	MethodName   string `json:"method_name"`
}

type javaAnnotation struct {
	FQN  string   `json:"fqn"`
	Args []string `json:"args"`
}

type javaImport struct {
	From    string `json:"from"`
	ToClass string `json:"to_class"`
	Kind    string `json:"kind"`
}

type parseWarning struct {
	File    string `json:"file"`
	Message string `json:"message"`
}
