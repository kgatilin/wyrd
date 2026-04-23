package sequence

import (
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

func TestFormatD2HasHeader(t *testing.T) {
	models := fixture()
	start := domain.SymbolRef{Package: "pkg/a", Symbol: "Alpha"}
	out := FormatD2(Build(models, start, 5))
	if !strings.HasPrefix(out, "shape: sequence_diagram\n") {
		t.Errorf("D2 output must start with shape directive, got:\n%s", out)
	}
}

func TestFormatD2SimpleChainArrows(t *testing.T) {
	models := fixture()
	start := domain.SymbolRef{Package: "pkg/a", Symbol: "Alpha"}
	out := FormatD2(Build(models, start, 5))

	for _, want := range []string{
		"a.Alpha -> b.Beta: Beta",
		"b.Beta -> c.Gamma: Gamma",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected arrow %q in output:\n%s", want, out)
		}
	}
}

func TestFormatD2MethodActorsShareType(t *testing.T) {
	models := fixture()
	start := domain.SymbolRef{Package: "pkg/svc", Symbol: "Service.Generate"}
	out := FormatD2(Build(models, start, 5))
	// Actor for Service.Generate should be "svc.Service"; for
	// fileReader.Read it should be "rd.fileReader"; arrow label is
	// just "Read" with a [via io.Reader] annotation.
	for _, want := range []string{
		"svc.Service -> rd.fileReader: Read [via io.Reader]",
		"svc.Service -> rd.memReader: Read [via io.Reader]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in D2 output:\n%s", want, out)
		}
	}
}

func TestFormatD2CycleAnnotation(t *testing.T) {
	models := fixture()
	start := domain.SymbolRef{Package: "pkg/c", Symbol: "Loop"}
	out := FormatD2(Build(models, start, 5))
	if !strings.Contains(out, "(cycle)") {
		t.Errorf("D2 cycle arrow missing annotation:\n%s", out)
	}
}

func TestFormatD2DepthLimitAnnotation(t *testing.T) {
	models := fixture()
	start := domain.SymbolRef{Package: "pkg/a", Symbol: "Alpha"}
	out := FormatD2(Build(models, start, 1))
	if !strings.Contains(out, "(depth limit)") {
		t.Errorf("D2 depth-limit arrow missing annotation:\n%s", out)
	}
}

func TestFormatD2NilSafe(t *testing.T) {
	if FormatD2(nil) != "" {
		t.Errorf("FormatD2(nil) should be empty")
	}
}
