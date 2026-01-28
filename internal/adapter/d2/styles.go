// Package d2 provides adapters for reading and writing D2 diagram files.
package d2

import "github.com/kgatilin/archai/internal/domain"

// D2 color constants for different stereotypes.
// Colors follow the DDD color scheme defined in the plan.
const (
	// ColorPurple is used for services, repositories, ports, and interfaces.
	ColorPurple = "#f0e8fc"

	// ColorGreen is used for factory functions.
	ColorGreen = "#e8fce8"

	// ColorBlue is used for aggregates and entities.
	ColorBlue = "#e8f4fc"

	// ColorGray is used for value objects, enums, and unclassified types.
	ColorGray = "#f8f8f8"

	// ColorLegendBorder is the stroke color for the legend box.
	ColorLegendBorder = "#999"

	// ColorLegendFill is the fill color for the legend box.
	ColorLegendFill = "#fafafa"
)

// D2 class names for different stereotype categories.
const (
	ClassDomain  = "domain"
	ClassService = "service"
	ClassFactory = "factory"
	ClassValue   = "value"
)

// stereotypeColor returns the D2 fill color for a given stereotype.
func stereotypeColor(s domain.Stereotype) string {
	switch s {
	case domain.StereotypeService,
		domain.StereotypeRepository,
		domain.StereotypePort,
		domain.StereotypeInterface:
		return ColorPurple
	case domain.StereotypeFactory:
		return ColorGreen
	case domain.StereotypeAggregate,
		domain.StereotypeEntity:
		return ColorBlue
	case domain.StereotypeValue,
		domain.StereotypeEnum,
		domain.StereotypeNone:
		return ColorGray
	default:
		return ColorGray
	}
}

// stereotypeClass returns the D2 class name for a given stereotype.
func stereotypeClass(s domain.Stereotype) string {
	switch s {
	case domain.StereotypeService,
		domain.StereotypeRepository,
		domain.StereotypePort,
		domain.StereotypeInterface:
		return ClassService
	case domain.StereotypeFactory:
		return ClassFactory
	case domain.StereotypeAggregate,
		domain.StereotypeEntity:
		return ClassDomain
	case domain.StereotypeValue,
		domain.StereotypeEnum,
		domain.StereotypeNone:
		return ClassValue
	default:
		return ClassValue
	}
}

// stereotypeLabel returns the D2 stereotype label (e.g., "<<service>>").
// Returns empty string for StereotypeNone.
func stereotypeLabel(s domain.Stereotype) string {
	if s == domain.StereotypeNone {
		return ""
	}
	return "<<" + string(s) + ">>"
}

// symbolInfo holds metadata about a symbol for color calculations.
type symbolInfo struct {
	stereotype domain.Stereotype
}

// fileContainerColor determines the fill color for a file container
// based on the dominant stereotype of its contents.
func fileContainerColor(symbols []symbolInfo) string {
	return stereotypeColor(dominantStereotype(symbols))
}

// fileContainerClass determines the D2 class for a file container
// based on the dominant stereotype of its contents.
func fileContainerClass(symbols []symbolInfo) string {
	return stereotypeClass(dominantStereotype(symbols))
}

// dominantStereotype finds the most common stereotype in a list of symbols.
func dominantStereotype(symbols []symbolInfo) domain.Stereotype {
	if len(symbols) == 0 {
		return domain.StereotypeNone
	}

	// Count occurrences of each stereotype
	counts := make(map[domain.Stereotype]int)
	for _, sym := range symbols {
		counts[sym.stereotype]++
	}

	// Find the dominant stereotype
	var dominant domain.Stereotype
	maxCount := 0
	for s, c := range counts {
		if c > maxCount {
			dominant = s
			maxCount = c
		}
	}

	return dominant
}
