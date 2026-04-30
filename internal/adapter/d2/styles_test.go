package d2

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

func TestDefaultClassStylesKeepMemberTextReadable(t *testing.T) {
	pkg := domain.PackageModel{
		Name: "service",
		Path: "internal/service",
		Interfaces: []domain.InterfaceDef{
			{
				Name:       "Service",
				IsExported: true,
				Stereotype: domain.StereotypeService,
				Methods: []domain.MethodDef{
					{Name: "Run", IsExported: true, Returns: []domain.TypeRef{{Name: "error"}}},
				},
			},
		},
		Structs: []domain.StructDef{
			{
				Name:       "Options",
				IsExported: true,
				Stereotype: domain.StereotypeValue,
				Fields: []domain.FieldDef{
					{Name: "Config", Type: domain.TypeRef{Name: "string"}, IsExported: true},
				},
			},
		},
		Functions: []domain.FunctionDef{
			{
				Name:       "NewService",
				IsExported: true,
				Stereotype: domain.StereotypeFactory,
				Returns:    []domain.TypeRef{{Name: "Service"}},
			},
		},
	}

	src := newCombinedBuilderWithMode(OverviewModePublic).Build([]domain.PackageModel{pkg})
	svg, err := RenderSVG(context.Background(), src)
	if err != nil {
		t.Fatalf("RenderSVG() error = %v\nsource:\n%s", err, src)
	}

	assertTextFill(t, string(svg), "Run()", ColorPurpleClass)
	assertTextFill(t, string(svg), "Config string", ColorGrayClass)
	assertTextFill(t, string(svg), "return", ColorGreenClass)
}

func TestStyleConfigOverridesContainerAndSymbolStyles(t *testing.T) {
	style := StyleConfig{
		Factory: SemanticStyle{
			ContainerFill:      "#dcfce7",
			ContainerFontColor: "#052e16",
			ClassFill:          "#14532d",
			ClassFontColor:     "#f0fdf4",
		},
		Legend: LegendStyle{
			Fill:   "#ffffff",
			Stroke: "#d1d5db",
		},
	}

	got := newCombinedBuilderWithStyle(OverviewModePublic, style).Build(nil)
	wantParts := []string{
		ClassFactory + `: {`,
		`style.fill: "#dcfce7"`,
		`style.font-color: "#052e16"`,
		ClassFactorySymbol + `: {`,
		`style.fill: "#14532d"`,
		`style.font-color: "#f0fdf4"`,
		`style.stroke: "#d1d5db"`,
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Errorf("Build() output missing %q\noutput:\n%s", want, got)
		}
	}
}

func assertTextFill(t *testing.T, svg, text, want string) {
	t.Helper()
	re := regexp.MustCompile(`<text[^>]*fill="([^"]+)"[^>]*>` + regexp.QuoteMeta(text) + `</text>`)
	m := re.FindStringSubmatch(svg)
	if m == nil {
		t.Fatalf("SVG missing text %q\nsvg:\n%s", text, svg)
	}
	if !strings.EqualFold(m[1], want) {
		t.Fatalf("text %q fill = %q, want %q", text, m[1], want)
	}
}
