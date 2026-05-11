package d2

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

// minMemberContrastRatio is the minimum WCAG 2.1 contrast ratio enforced for
// D2 class member-name text against the class body background (white). WCAG
// AA requires 4.5:1 for normal text and 3:1 for large text. We pick 4.5:1
// to keep dense class member rows readable even at small render sizes.
const minMemberContrastRatio = 4.5

// classBodyBackground is the SVG background color D2 paints behind class
// member rows. D2 renders class shapes with a white body regardless of the
// configured style.fill (which it reuses for member text color). The
// contrast check pins this constant so tests fail loudly if D2 ever
// changes the body background.
const classBodyBackground = "#FFFFFF"

// contrastSample is one class shape rendered in the contrast diagram.
// Each stereotype that maps to a distinct palette gets at least one sample,
// matching the regression coverage requested in #96 (factory, service /
// interface, value object, and aggregate / domain).
type contrastSample struct {
	stereotype string // human label used in t.Run subtests
	memberText string // exact text content of a member row in the SVG
	wantSymbol string // expected symbol-class hex (sanity check)
}

// TestClassMemberTextContrastAgainstBodyBackground asserts that the default
// D2 palette keeps class member-name text readable on the class body
// background. D2 reuses style.fill for member text color inside class
// shapes, so a pale fill chosen for visual semantics would otherwise paint
// member rows in near-invisible pale-on-white. This is the regression
// invariant for ticket #96.
func TestClassMemberTextContrastAgainstBodyBackground(t *testing.T) {
	pkg := domain.PackageModel{
		Name: "shop",
		Path: "internal/shop",
		Interfaces: []domain.InterfaceDef{{
			Name:       "OrderService",
			IsExported: true,
			Stereotype: domain.StereotypeService,
			Methods: []domain.MethodDef{{
				Name:       "Place",
				IsExported: true,
				Returns:    []domain.TypeRef{{Name: "error"}},
			}},
		}},
		Structs: []domain.StructDef{
			{
				Name:       "Order",
				IsExported: true,
				Stereotype: domain.StereotypeAggregate,
				Fields: []domain.FieldDef{
					{Name: "ID", Type: domain.TypeRef{Name: "string"}, IsExported: true},
				},
			},
			{
				Name:       "Money",
				IsExported: true,
				Stereotype: domain.StereotypeValue,
				Fields: []domain.FieldDef{
					{Name: "Amount", Type: domain.TypeRef{Name: "int64"}, IsExported: true},
				},
			},
		},
		Functions: []domain.FunctionDef{{
			Name:       "NewOrderService",
			IsExported: true,
			Stereotype: domain.StereotypeFactory,
			Returns:    []domain.TypeRef{{Name: "OrderService"}},
		}},
	}

	samples := []contrastSample{
		{stereotype: "service", memberText: "Place()", wantSymbol: ColorPurpleClass},
		{stereotype: "domain", memberText: "ID string", wantSymbol: ColorBlueClass},
		{stereotype: "value", memberText: "Amount int64", wantSymbol: ColorGrayClass},
		{stereotype: "factory", memberText: "return", wantSymbol: ColorGreenClass},
	}

	src := newCombinedBuilderWithMode(OverviewModePublic).Build([]domain.PackageModel{pkg})
	svg, err := RenderSVG(context.Background(), src)
	if err != nil {
		t.Fatalf("RenderSVG() error = %v\nsource:\n%s", err, src)
	}
	rendered := string(svg)

	for _, s := range samples {
		t.Run(s.stereotype, func(t *testing.T) {
			fill := findTextFill(t, rendered, s.memberText)
			if !strings.EqualFold(fill, s.wantSymbol) {
				t.Errorf("member %q fill = %q, want default palette %q", s.memberText, fill, s.wantSymbol)
			}
			ratio, err := contrastRatio(fill, classBodyBackground)
			if err != nil {
				t.Fatalf("contrast ratio for %q vs %q: %v", fill, classBodyBackground, err)
			}
			if ratio < minMemberContrastRatio {
				t.Errorf("member %q text fill %q has contrast %.2f:1 against class body %q, want >= %.2f:1 (WCAG AA)",
					s.memberText, fill, ratio, classBodyBackground, minMemberContrastRatio)
			}
		})
	}
}

// TestClassHeaderTextContrastAgainstHeaderFill pins the other half of the
// contrast invariant: the class HEADER renders style.font-color text over
// the style.fill background, so we need that pair to be readable too.
// Without this we could regress by darkening fills without darkening the
// header font color (e.g. setting both to gray).
func TestClassHeaderTextContrastAgainstHeaderFill(t *testing.T) {
	def := DefaultStyleConfig()
	cases := []struct {
		stereotype string
		fill       string
		font       string
	}{
		{"service", def.Service.ClassFill, def.Service.ClassFontColor},
		{"domain", def.Domain.ClassFill, def.Domain.ClassFontColor},
		{"value", def.Value.ClassFill, def.Value.ClassFontColor},
		{"factory", def.Factory.ClassFill, def.Factory.ClassFontColor},
	}
	for _, c := range cases {
		t.Run(c.stereotype, func(t *testing.T) {
			ratio, err := contrastRatio(c.font, c.fill)
			if err != nil {
				t.Fatalf("contrast ratio for %q vs %q: %v", c.font, c.fill, err)
			}
			if ratio < minMemberContrastRatio {
				t.Errorf("header font %q on fill %q has contrast %.2f:1, want >= %.2f:1 (WCAG AA)",
					c.font, c.fill, ratio, minMemberContrastRatio)
			}
		})
	}
}

// TestContainerFontReadableOnContainerFill keeps the file/package container
// labels readable against the pale container palette. Container fill IS
// the visible background here (D2 does not reuse it as text color for
// container labels), so we exercise the container_font_color / container_fill
// pairing directly.
func TestContainerFontReadableOnContainerFill(t *testing.T) {
	def := DefaultStyleConfig()
	cases := []struct {
		stereotype string
		fill       string
		font       string
	}{
		{"service", def.Service.ContainerFill, def.Service.ContainerFontColor},
		{"domain", def.Domain.ContainerFill, def.Domain.ContainerFontColor},
		{"value", def.Value.ContainerFill, def.Value.ContainerFontColor},
		{"factory", def.Factory.ContainerFill, def.Factory.ContainerFontColor},
	}
	for _, c := range cases {
		t.Run(c.stereotype, func(t *testing.T) {
			ratio, err := contrastRatio(c.font, c.fill)
			if err != nil {
				t.Fatalf("contrast ratio for %q vs %q: %v", c.font, c.fill, err)
			}
			if ratio < minMemberContrastRatio {
				t.Errorf("container font %q on fill %q has contrast %.2f:1, want >= %.2f:1 (WCAG AA)",
					c.font, c.fill, ratio, minMemberContrastRatio)
			}
		})
	}
}

// findTextFill returns the fill attribute of the first <text> element whose
// inner text exactly matches the given content. D2's SVG renderer escapes
// "<" / ">" as entities, so callers should pass the unescaped form (e.g.
// "<<entry-point>>" — the helper handles the entity translation).
func findTextFill(t *testing.T, svg, text string) string {
	t.Helper()
	escaped := strings.ReplaceAll(text, "<", "&lt;")
	escaped = strings.ReplaceAll(escaped, ">", "&gt;")
	re := regexp.MustCompile(`<text[^>]*fill="([^"]+)"[^>]*>` + regexp.QuoteMeta(escaped) + `</text>`)
	m := re.FindStringSubmatch(svg)
	if m == nil {
		t.Fatalf("SVG missing text %q (escaped %q)", text, escaped)
	}
	return m[1]
}

// contrastRatio computes the WCAG 2.1 relative-luminance contrast ratio
// between two CSS hex colors (3- or 6-digit, with optional leading '#').
// The result is in the range [1, 21]; WCAG AA requires >= 4.5 for normal
// body text.
func contrastRatio(a, b string) (float64, error) {
	la, err := relativeLuminance(a)
	if err != nil {
		return 0, fmt.Errorf("color %q: %w", a, err)
	}
	lb, err := relativeLuminance(b)
	if err != nil {
		return 0, fmt.Errorf("color %q: %w", b, err)
	}
	light, dark := la, lb
	if dark > light {
		light, dark = dark, light
	}
	return (light + 0.05) / (dark + 0.05), nil
}

// relativeLuminance returns the WCAG relative luminance of a CSS hex color.
func relativeLuminance(hex string) (float64, error) {
	r, g, b, err := parseHexColor(hex)
	if err != nil {
		return 0, err
	}
	return 0.2126*linearize(r) + 0.7152*linearize(g) + 0.0722*linearize(b), nil
}

func linearize(c float64) float64 {
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// parseHexColor parses a CSS hex color into three [0, 1] floats. It accepts
// "#rgb", "rgb", "#rrggbb", and "rrggbb".
func parseHexColor(hex string) (float64, float64, float64, error) {
	s := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	switch len(s) {
	case 3:
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	case 6:
		// already canonical
	default:
		return 0, 0, 0, fmt.Errorf("invalid hex color %q", hex)
	}
	var r, g, b int
	if _, err := fmt.Sscanf(strings.ToLower(s), "%02x%02x%02x", &r, &g, &b); err != nil {
		return 0, 0, 0, fmt.Errorf("invalid hex color %q: %w", hex, err)
	}
	return float64(r) / 255, float64(g) / 255, float64(b) / 255, nil
}
