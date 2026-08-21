// Package domain contains the core domain models for wyrd.
// All models are concrete structs serving as data containers,
// following DDD principles for aggregate roots and value objects.
package domain

// Stereotype represents a DDD classification for a code element.
// Stereotypes determine visual styling (color, label) in D2 diagrams
// and help convey architectural intent.
type Stereotype string

const (
	// StereotypeNone indicates no specific stereotype classification.
	StereotypeNone Stereotype = ""

	// StereotypeInterface indicates a generic Go interface.
	StereotypeInterface Stereotype = "interface"

	// StereotypeService indicates a service interface (business operations).
	StereotypeService Stereotype = "service"

	// StereotypeRepository indicates a repository interface (data access).
	StereotypeRepository Stereotype = "repository"

	// StereotypePort indicates an adapter port (hexagonal architecture).
	StereotypePort Stereotype = "port"

	// StereotypeFactory indicates a factory function (creates instances).
	StereotypeFactory Stereotype = "factory"

	// StereotypeAggregate indicates a DDD aggregate root.
	StereotypeAggregate Stereotype = "aggregate"

	// StereotypeEntity indicates a DDD entity.
	StereotypeEntity Stereotype = "entity"

	// StereotypeValue indicates a value object.
	StereotypeValue Stereotype = "value"

	// StereotypeEnum indicates an enumeration type.
	StereotypeEnum Stereotype = "enum"
)

// String returns the string representation of the stereotype.
func (s Stereotype) String() string {
	if s == StereotypeNone {
		return ""
	}
	return string(s)
}

// IsEmpty returns true if the stereotype is not set.
func (s Stereotype) IsEmpty() bool {
	return s == StereotypeNone
}
