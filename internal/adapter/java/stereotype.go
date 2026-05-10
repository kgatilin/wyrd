package java

import (
	"strings"

	"github.com/kgatilin/archai/internal/domain"
)

// detectClassStereotype returns a stereotype for a JavaClass. Heuristics
// (v1, mirroring adapter/golang/stereotype.go but adapted to Java idiom):
//
//	repository — name ends with "Repository" or "Dao".
//	service    — name ends with "Service".
//	controller — name ends with "Controller", or class is annotated
//	             @RestController / @Controller (FQN suffix match).
//	record     — `kind: "record"`.
//
// All other classes return StereotypeNone — the diagram layer will fall
// back to its default rendering. Future PRs may extend this table.
func detectClassStereotype(c javaClass) domain.Stereotype {
	if c.Kind == "record" {
		return domain.StereotypeValue
	}
	for _, ann := range c.Annotations {
		if hasSimpleName(ann.FQN, "RestController") || hasSimpleName(ann.FQN, "Controller") {
			return domain.StereotypeService
		}
	}
	switch {
	case strings.HasSuffix(c.Name, "Repository"), strings.HasSuffix(c.Name, "Dao"):
		return domain.StereotypeRepository
	case strings.HasSuffix(c.Name, "Service"):
		return domain.StereotypeService
	case strings.HasSuffix(c.Name, "Controller"):
		return domain.StereotypeService
	}
	return domain.StereotypeNone
}

// detectInterfaceStereotype mirrors the Go adapter's interface heuristics
// translated to Java naming:
//
//	repository — *Repository / *Dao
//	service    — *Service / *Handler / *Manager / *Controller
//	port       — *Reader / *Writer
//
// Unmatched names default to StereotypeInterface so the D2 layer still
// renders an interface stereotype tag.
func detectInterfaceStereotype(c javaClass) domain.Stereotype {
	switch {
	case strings.HasSuffix(c.Name, "Repository"), strings.HasSuffix(c.Name, "Dao"):
		return domain.StereotypeRepository
	case strings.HasSuffix(c.Name, "Service"),
		strings.HasSuffix(c.Name, "Handler"),
		strings.HasSuffix(c.Name, "Manager"),
		strings.HasSuffix(c.Name, "Controller"):
		return domain.StereotypeService
	case strings.HasSuffix(c.Name, "Reader"), strings.HasSuffix(c.Name, "Writer"):
		return domain.StereotypePort
	}
	return domain.StereotypeInterface
}

// detectFactoryStereotype returns StereotypeFactory when a method on the
// enclosing class is a likely factory: static, returns the enclosing type,
// and is named "of", "create", or starts with "new".
func detectFactoryStereotype(class javaClass, m javaMethod) domain.Stereotype {
	if !containsModifier(m.Modifiers, "static") {
		return domain.StereotypeNone
	}
	if m.Returns != class.Name && m.Returns != class.FQN {
		return domain.StereotypeNone
	}
	switch {
	case m.Name == "of", m.Name == "create":
		return domain.StereotypeFactory
	case strings.HasPrefix(m.Name, "new"):
		return domain.StereotypeFactory
	}
	return domain.StereotypeNone
}

// hasSimpleName returns true when fqn ends with "."+simple or equals simple.
// "Simple-name" matching keeps the heuristic permissive across stdlib /
// Spring / Jakarta annotations without forcing the analyzer to resolve
// every annotation FQN.
func hasSimpleName(fqn, simple string) bool {
	if fqn == simple {
		return true
	}
	return strings.HasSuffix(fqn, "."+simple)
}

func containsModifier(mods []string, want string) bool {
	for _, m := range mods {
		if m == want {
			return true
		}
	}
	return false
}
