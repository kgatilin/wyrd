package sequence

import (
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

func TestFormatTextSimpleChain(t *testing.T) {
	models := fixture()
	start := domain.SymbolRef{Package: "pkg/a", Symbol: "Alpha"}
	out := FormatText(Build(models, start, 5))

	want := "pkg/a.Alpha\n" +
		"└─ pkg/b.Beta\n" +
		"   └─ pkg/c.Gamma\n"
	if out != want {
		t.Errorf("text output mismatch.\n got:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatTextCycleMarker(t *testing.T) {
	models := fixture()
	start := domain.SymbolRef{Package: "pkg/c", Symbol: "Loop"}
	out := FormatText(Build(models, start, 5))
	if !strings.Contains(out, "(cycle)") {
		t.Errorf("expected (cycle) marker in output:\n%s", out)
	}
}

func TestFormatTextDepthLimit(t *testing.T) {
	models := fixture()
	start := domain.SymbolRef{Package: "pkg/a", Symbol: "Alpha"}
	out := FormatText(Build(models, start, 1))
	if !strings.Contains(out, "(depth limit)") {
		t.Errorf("expected (depth limit) marker in output:\n%s", out)
	}
}

func TestFormatTextInterfaceVia(t *testing.T) {
	models := fixture()
	start := domain.SymbolRef{Package: "pkg/svc", Symbol: "Service.Generate"}
	out := FormatText(Build(models, start, 5))
	if !strings.Contains(out, "[via io.Reader]") {
		t.Errorf("expected [via io.Reader] annotation:\n%s", out)
	}
	if !strings.Contains(out, "fileReader.Read") || !strings.Contains(out, "memReader.Read") {
		t.Errorf("expected both impls as siblings:\n%s", out)
	}
}

func TestFormatTextNilSafe(t *testing.T) {
	if FormatText(nil) != "" {
		t.Errorf("FormatText(nil) should be empty string")
	}
}
