// Package d2 provides adapters for reading and writing D2 diagram files.
package d2

import "github.com/kgatilin/archai/internal/domain"

// D2 color constants for different stereotypes.
// Container colors stay intentionally pale; D2 class shapes reuse
// style.fill for member-name text, so symbol class fills must be dark
// enough to remain readable on the white class body.
const (
	// ColorPurple is used for services, repositories, ports, and interfaces.
	ColorPurple      = "#f0e8fc"
	ColorPurpleClass = "#6d28d9"

	// ColorGreen is used for factory functions.
	ColorGreen      = "#e8fce8"
	ColorGreenClass = "#166534"

	// ColorBlue is used for aggregates and entities.
	ColorBlue      = "#e8f4fc"
	ColorBlueClass = "#1d4ed8"

	// ColorGray is used for value objects, enums, and unclassified types.
	ColorGray      = "#f8f8f8"
	ColorGrayClass = "#374151"

	// ColorText is the default readable text color on pale fills.
	ColorText = "#111827"

	// ColorClassTitle is the default readable title color on dark class fills.
	ColorClassTitle = "#ffffff"

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

	ClassDomainSymbol  = "domain_symbol"
	ClassServiceSymbol = "service_symbol"
	ClassFactorySymbol = "factory_symbol"
	ClassValueSymbol   = "value_symbol"
)

// StyleConfig configures generated D2 style classes. Empty fields fall back to
// the built-in readable defaults.
type StyleConfig struct {
	Domain  SemanticStyle
	Service SemanticStyle
	Factory SemanticStyle
	Value   SemanticStyle
	Legend  LegendStyle
}

// SemanticStyle is the palette for one semantic diagram category.
type SemanticStyle struct {
	ContainerFill      string
	ContainerFontColor string
	ClassFill          string
	ClassFontColor     string
}

// LegendStyle configures the generated legend container.
type LegendStyle struct {
	Fill   string
	Stroke string
}

// DefaultStyleConfig returns the built-in D2 palette.
func DefaultStyleConfig() StyleConfig {
	return StyleConfig{
		Domain: SemanticStyle{
			ContainerFill:      ColorBlue,
			ContainerFontColor: ColorText,
			ClassFill:          ColorBlueClass,
			ClassFontColor:     ColorClassTitle,
		},
		Service: SemanticStyle{
			ContainerFill:      ColorPurple,
			ContainerFontColor: ColorText,
			ClassFill:          ColorPurpleClass,
			ClassFontColor:     ColorClassTitle,
		},
		Factory: SemanticStyle{
			ContainerFill:      ColorGreen,
			ContainerFontColor: ColorText,
			ClassFill:          ColorGreenClass,
			ClassFontColor:     ColorClassTitle,
		},
		Value: SemanticStyle{
			ContainerFill:      ColorGray,
			ContainerFontColor: ColorText,
			ClassFill:          ColorGrayClass,
			ClassFontColor:     ColorClassTitle,
		},
		Legend: LegendStyle{
			Fill:   ColorLegendFill,
			Stroke: ColorLegendBorder,
		},
	}
}

func (c StyleConfig) withDefaults() StyleConfig {
	def := DefaultStyleConfig()
	return StyleConfig{
		Domain:  mergeSemanticStyle(def.Domain, c.Domain),
		Service: mergeSemanticStyle(def.Service, c.Service),
		Factory: mergeSemanticStyle(def.Factory, c.Factory),
		Value:   mergeSemanticStyle(def.Value, c.Value),
		Legend:  mergeLegendStyle(def.Legend, c.Legend),
	}
}

func mergeSemanticStyle(def, override SemanticStyle) SemanticStyle {
	if override.ContainerFill != "" {
		def.ContainerFill = override.ContainerFill
	}
	if override.ContainerFontColor != "" {
		def.ContainerFontColor = override.ContainerFontColor
	}
	if override.ClassFill != "" {
		def.ClassFill = override.ClassFill
	}
	if override.ClassFontColor != "" {
		def.ClassFontColor = override.ClassFontColor
	}
	return def
}

func mergeLegendStyle(def, override LegendStyle) LegendStyle {
	if override.Fill != "" {
		def.Fill = override.Fill
	}
	if override.Stroke != "" {
		def.Stroke = override.Stroke
	}
	return def
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

// stereotypeSymbolClass returns the D2 class style for class-shaped symbols.
func stereotypeSymbolClass(s domain.Stereotype) string {
	switch s {
	case domain.StereotypeService,
		domain.StereotypeRepository,
		domain.StereotypePort,
		domain.StereotypeInterface:
		return ClassServiceSymbol
	case domain.StereotypeFactory:
		return ClassFactorySymbol
	case domain.StereotypeAggregate,
		domain.StereotypeEntity:
		return ClassDomainSymbol
	case domain.StereotypeValue,
		domain.StereotypeEnum,
		domain.StereotypeNone:
		return ClassValueSymbol
	default:
		return ClassValueSymbol
	}
}

func interfaceSymbolStereotype(iface domain.InterfaceDef) domain.Stereotype {
	if iface.Stereotype != domain.StereotypeNone {
		return iface.Stereotype
	}
	return domain.StereotypeInterface
}

func structSymbolStereotype(s domain.StructDef) domain.Stereotype {
	if s.Stereotype != domain.StereotypeNone {
		return s.Stereotype
	}
	return domain.StereotypeValue
}

func functionSymbolStereotype(fn domain.FunctionDef) domain.Stereotype {
	if fn.Stereotype != domain.StereotypeNone {
		return fn.Stereotype
	}
	return domain.StereotypeValue
}

func typeDefSymbolStereotype(td domain.TypeDef) domain.Stereotype {
	if td.Stereotype != domain.StereotypeNone {
		return td.Stereotype
	}
	return domain.StereotypeValue
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
