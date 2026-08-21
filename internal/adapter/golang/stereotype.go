// Package golang provides an adapter for reading Go source code
// and converting it to domain.PackageModel structures.
package golang

import (
	"regexp"
	"strings"

	"github.com/kgatilin/wyrd/internal/domain"
)

// annotationPattern matches archspec:<stereotype> annotations in doc comments.
var annotationPattern = regexp.MustCompile(`archspec:(\w+)`)

// parseAnnotation extracts a stereotype from a doc comment's archspec annotation.
// Returns StereotypeNone if no valid annotation is found.
func parseAnnotation(doc string) domain.Stereotype {
	matches := annotationPattern.FindStringSubmatch(doc)
	if len(matches) < 2 {
		return domain.StereotypeNone
	}

	switch matches[1] {
	case "service":
		return domain.StereotypeService
	case "repository":
		return domain.StereotypeRepository
	case "port":
		return domain.StereotypePort
	case "factory":
		return domain.StereotypeFactory
	case "aggregate":
		return domain.StereotypeAggregate
	case "entity":
		return domain.StereotypeEntity
	case "value":
		return domain.StereotypeValue
	case "enum":
		return domain.StereotypeEnum
	case "interface":
		return domain.StereotypeInterface
	default:
		return domain.StereotypeNone
	}
}

// detectInterfaceStereotype determines the stereotype for an interface.
// Priority: explicit annotation > heuristic > default (StereotypeInterface).
func detectInterfaceStereotype(iface domain.InterfaceDef, pkgPath string) domain.Stereotype {
	// 1. Check for explicit annotation
	if s := parseAnnotation(iface.Doc); s != domain.StereotypeNone {
		return s
	}

	// 2. Apply heuristics based on name suffix
	name := iface.Name

	// Service patterns: *Service, *Handler, *Manager, *Controller
	servicePatterns := []string{"Service", "Handler", "Manager", "Controller"}
	for _, pattern := range servicePatterns {
		if strings.HasSuffix(name, pattern) {
			return domain.StereotypeService
		}
	}

	// Repository patterns: *Repository, *Store
	if strings.HasSuffix(name, "Repository") || strings.HasSuffix(name, "Store") {
		return domain.StereotypeRepository
	}

	// Port patterns: *Reader, *Writer (hexagonal architecture ports)
	if strings.HasSuffix(name, "Reader") || strings.HasSuffix(name, "Writer") {
		return domain.StereotypePort
	}

	// 3. Default to generic interface
	return domain.StereotypeInterface
}

// detectStructStereotype determines the stereotype for a struct.
// Priority: explicit annotation > heuristic > default (StereotypeNone).
func detectStructStereotype(s domain.StructDef, pkgPath string) domain.Stereotype {
	// 1. Check for explicit annotation
	if st := parseAnnotation(s.Doc); st != domain.StereotypeNone {
		return st
	}

	// 2. Apply heuristics based on name suffix
	name := s.Name

	// Value object patterns: *Options, *Config, *Result, *Request, *Response, *Params, *Ref, *Info
	valuePatterns := []string{
		"Options", "Config", "Result", "Request",
		"Response", "Params", "Ref", "Info",
	}
	for _, pattern := range valuePatterns {
		if strings.HasSuffix(name, pattern) {
			return domain.StereotypeValue
		}
	}

	// 3. Check if located in domain/model/entity path
	if isDomainPath(pkgPath) {
		// Aggregate if it has methods (behavior), Entity otherwise
		if len(s.Methods) > 0 {
			return domain.StereotypeAggregate
		}
		return domain.StereotypeEntity
	}

	// 4. Check if struct has no methods (likely a value object)
	if len(s.Methods) == 0 {
		return domain.StereotypeValue
	}

	// 5. Default to no specific stereotype
	return domain.StereotypeNone
}

// detectFunctionStereotype determines the stereotype for a package-level function.
// Priority: explicit annotation > heuristic > default (StereotypeNone).
func detectFunctionStereotype(fn domain.FunctionDef) domain.Stereotype {
	// 1. Check for explicit annotation
	if s := parseAnnotation(fn.Doc); s != domain.StereotypeNone {
		return s
	}

	// 2. Apply factory heuristic: New* prefix and returns a type
	if strings.HasPrefix(fn.Name, "New") && len(fn.Returns) > 0 {
		return domain.StereotypeFactory
	}

	// 3. Default to no specific stereotype
	return domain.StereotypeNone
}

// detectTypeDefStereotype determines the stereotype for a type definition.
// Priority: explicit annotation > heuristic > default (StereotypeNone).
func detectTypeDefStereotype(td domain.TypeDef) domain.Stereotype {
	// 1. Check for explicit annotation
	if s := parseAnnotation(td.Doc); s != domain.StereotypeNone {
		return s
	}

	// 2. Apply enum heuristic: has associated constants
	if len(td.Constants) > 0 {
		return domain.StereotypeEnum
	}

	// 3. Default to no specific stereotype
	return domain.StereotypeNone
}

// isDomainPath checks if a package path indicates a domain/model/entity layer.
// Matches paths like "internal/domain", "pkg/model/", "entity/user", etc.
func isDomainPath(pkgPath string) bool {
	domainIndicators := []string{"domain", "model", "entity"}
	for _, indicator := range domainIndicators {
		// Check if path contains /indicator/ (mid-path)
		if strings.Contains(pkgPath, "/"+indicator+"/") {
			return true
		}
		// Check if path ends with /indicator (end of path)
		if strings.HasSuffix(pkgPath, "/"+indicator) {
			return true
		}
		// Check if path equals indicator (root-level)
		if pkgPath == indicator {
			return true
		}
	}
	return false
}
