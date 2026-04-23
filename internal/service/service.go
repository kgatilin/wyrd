package service

// Service orchestrates diagram generation operations.
// It uses adapters to read from various sources (Go code, D2 files, YAML files)
// and write to various destinations (D2 files, YAML files, stdout).
type Service struct {
	// goReader reads package models from Go source code.
	goReader ModelReader

	// d2Reader reads package models from D2 diagram files.
	d2Reader ModelReader

	// d2Writer writes package models as D2 diagram files.
	d2Writer ModelWriter

	// yamlReader reads package models from YAML files (optional).
	yamlReader ModelReader

	// yamlWriter writes package models as YAML files (optional).
	yamlWriter ModelWriter
}
