package d2

import (
	"context"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/sequence"
)

func sequenceFixture() []domain.PackageModel {
	alpha := domain.FunctionDef{
		Name: "Alpha",
		Calls: []domain.CallEdge{
			{To: domain.SymbolRef{Package: "pkg/b", Symbol: "Beta"}},
		},
	}
	beta := domain.FunctionDef{
		Name: "Beta",
		Calls: []domain.CallEdge{
			{To: domain.SymbolRef{Package: "pkg/c", Symbol: "Gamma"}},
		},
	}
	gamma := domain.FunctionDef{Name: "Gamma"}

	loop := domain.FunctionDef{
		Name: "Loop",
		Calls: []domain.CallEdge{
			{To: domain.SymbolRef{Package: "pkg/c", Symbol: "Loop"}},
		},
	}

	service := domain.StructDef{
		Name: "Service",
		Methods: []domain.MethodDef{
			{
				Name: "Generate",
				Calls: []domain.CallEdge{
					{To: domain.SymbolRef{Package: "pkg/rd", Symbol: "fileReader.Read"}, Via: "io.Reader"},
					{To: domain.SymbolRef{Package: "pkg/rd", Symbol: "memReader.Read"}, Via: "io.Reader"},
				},
			},
		},
	}
	rdStruct1 := domain.StructDef{Name: "fileReader", Methods: []domain.MethodDef{{Name: "Read"}}}
	rdStruct2 := domain.StructDef{Name: "memReader", Methods: []domain.MethodDef{{Name: "Read"}}}

	return []domain.PackageModel{
		{Path: "pkg/a", Name: "a", Functions: []domain.FunctionDef{alpha}},
		{Path: "pkg/b", Name: "b", Functions: []domain.FunctionDef{beta}},
		{Path: "pkg/c", Name: "c", Functions: []domain.FunctionDef{gamma, loop}},
		{Path: "pkg/svc", Name: "svc", Structs: []domain.StructDef{service}},
		{Path: "pkg/rd", Name: "rd", Structs: []domain.StructDef{rdStruct1, rdStruct2}},
	}
}

func TestBuildSequenceSourceHasHeader(t *testing.T) {
	models := sequenceFixture()
	start := domain.SymbolRef{Package: "pkg/a", Symbol: "Alpha"}
	out := BuildSequenceSource(sequence.Build(models, start, 5))
	if !strings.HasPrefix(out, "shape: sequence_diagram\n") {
		t.Errorf("D2 output must start with shape directive, got:\n%s", out)
	}
}

func TestBuildSequenceSourceSimpleChainArrows(t *testing.T) {
	models := sequenceFixture()
	start := domain.SymbolRef{Package: "pkg/a", Symbol: "Alpha"}
	out := BuildSequenceSource(sequence.Build(models, start, 5))

	for _, want := range []string{
		`"a.Alpha" -> "b.Beta": Beta`,
		`"b.Beta" -> "c.Gamma": Gamma`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected arrow %q in output:\n%s", want, out)
		}
	}
}

func TestBuildSequenceSourceMethodActorsShareType(t *testing.T) {
	models := sequenceFixture()
	start := domain.SymbolRef{Package: "pkg/svc", Symbol: "Service.Generate"}
	out := BuildSequenceSource(sequence.Build(models, start, 5))
	for _, want := range []string{
		`"svc.Service" -> "rd.fileReader": "Read [via io.Reader]"`,
		`"svc.Service" -> "rd.memReader": "Read [via io.Reader]"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in D2 output:\n%s", want, out)
		}
	}
}

func TestBuildSequenceSourceCycleAnnotation(t *testing.T) {
	models := sequenceFixture()
	start := domain.SymbolRef{Package: "pkg/c", Symbol: "Loop"}
	out := BuildSequenceSource(sequence.Build(models, start, 5))
	if !strings.Contains(out, "(cycle)") {
		t.Errorf("D2 cycle arrow missing annotation:\n%s", out)
	}
}

func TestBuildSequenceSourceDepthLimitAnnotation(t *testing.T) {
	models := sequenceFixture()
	start := domain.SymbolRef{Package: "pkg/a", Symbol: "Alpha"}
	out := BuildSequenceSource(sequence.Build(models, start, 1))
	if !strings.Contains(out, "(depth limit)") {
		t.Errorf("D2 depth-limit arrow missing annotation:\n%s", out)
	}
}

func TestBuildSequenceSourceNilSafe(t *testing.T) {
	if BuildSequenceSource(nil) != "" {
		t.Errorf("BuildSequenceSource(nil) should be empty")
	}
}

func TestBuildPackageSequenceSourcesIncludesEntrySignatureAndReturns(t *testing.T) {
	pkg := domain.PackageModel{
		Path: "internal/apply",
		Functions: []domain.FunctionDef{{
			Name:       "Apply",
			IsExported: true,
			Params: []domain.ParamDef{
				{Name: "d", Type: domain.TypeRef{Name: "Diff", Package: "diff", IsPointer: true}},
				{Name: "currentModels", Type: domain.TypeRef{Name: "PackageModel", Package: "domain", IsSlice: true}},
				{Name: "targetModels", Type: domain.TypeRef{Name: "PackageModel", Package: "domain", IsSlice: true}},
			},
			Returns: []domain.TypeRef{
				{Name: "PackageModel", Package: "domain", IsSlice: true},
				{Name: "error"},
			},
			Calls: []domain.CallEdge{{To: domain.SymbolRef{Package: "internal/domain", Symbol: "SymbolRef.String"}}},
		}},
	}
	domainPkg := domain.PackageModel{
		Path: "internal/domain",
		Structs: []domain.StructDef{{
			Name:       "SymbolRef",
			IsExported: true,
			Methods: []domain.MethodDef{{
				Name:       "String",
				IsExported: true,
				Returns:    []domain.TypeRef{{Name: "string"}},
			}},
		}},
	}

	got := BuildPackageSequenceSources([]domain.PackageModel{pkg, domainPkg}, pkg, SequenceOptions{Mode: OverviewModePublic})
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1: %+v", len(got), got)
	}
	for _, want := range []string{
		`caller -> "apply.Apply": "Apply(d *diff.Diff, currentModels []domain.PackageModel, targetModels []domain.PackageModel)"`,
		`"apply.Apply" -> "domain.SymbolRef": "String()"`,
		`"domain.SymbolRef" -> "apply.Apply": "return string"`,
		`"apply.Apply" -> caller: "return []domain.PackageModel, error"`,
	} {
		if !strings.Contains(got[0].Source, want) {
			t.Fatalf("source missing %q:\n%s", want, got[0].Source)
		}
	}
}

func TestBuildPackageSequenceSources(t *testing.T) {
	pkg := domain.PackageModel{
		Path: "internal/svc",
		Functions: []domain.FunctionDef{
			{
				Name: "NewService", IsExported: true,
				Stereotype: domain.StereotypeFactory,
				Calls:      []domain.CallEdge{{To: domain.SymbolRef{Package: "internal/svc", Symbol: "Service.Run"}}},
			},
			{
				Name: "Helper", IsExported: true,
				Calls: []domain.CallEdge{{To: domain.SymbolRef{Package: "internal/svc", Symbol: "internal"}}},
			},
			{Name: "internal", IsExported: false},
		},
		Structs: []domain.StructDef{
			{
				Name: "Service", IsExported: true,
				Methods: []domain.MethodDef{{
					Name: "Run", IsExported: true,
					Calls: []domain.CallEdge{{To: domain.SymbolRef{Package: "internal/svc", Symbol: "Helper"}}},
				}},
			},
		},
	}

	got := BuildPackageSequenceSources([]domain.PackageModel{pkg}, pkg, SequenceOptions{Mode: OverviewModePublic})
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2: %+v", len(got), got)
	}
	if got[0].Label != "NewService" {
		t.Fatalf("constructor not first: %+v", got)
	}
	if got[1].Label != "Service.Run" {
		t.Fatalf("method sequence not second: %+v", got)
	}
	for _, entry := range got {
		if !strings.Contains(entry.Source, "shape: sequence_diagram") {
			t.Fatalf("%s missing D2 sequence source: %q", entry.Label, entry.Source)
		}
	}
	if !strings.Contains(got[0].Source, `"svc.NewService" -> "svc.Service": "Run()"`) {
		t.Fatalf("constructor sequence should show package-to-type call: %q", got[0].Source)
	}
	if !strings.Contains(got[1].Source, `"svc.Service" -> "svc.Helper": "Helper()"`) {
		t.Fatalf("method sequence should show type-to-package call: %q", got[1].Source)
	}
}

func TestBuildPackageSequenceSourcesFullModeAndSkipsRootOnly(t *testing.T) {
	pkg := domain.PackageModel{
		Path: "internal/svc",
		Functions: []domain.FunctionDef{
			{Name: "Bare", IsExported: true},
			{
				Name: "internal", IsExported: false,
				Calls: []domain.CallEdge{{To: domain.SymbolRef{Package: "internal/svc", Symbol: "worker.Run"}}},
			},
		},
		Structs: []domain.StructDef{{
			Name: "worker",
			Methods: []domain.MethodDef{{
				Name: "Run",
			}},
		}},
	}
	got := BuildPackageSequenceSources([]domain.PackageModel{pkg}, pkg, SequenceOptions{Mode: OverviewModePublic})
	if len(got) != 0 {
		t.Fatalf("public mode should skip root-only/unexported entries, got %+v", got)
	}
	got = BuildPackageSequenceSources([]domain.PackageModel{pkg}, pkg, SequenceOptions{Mode: OverviewModeFull})
	if len(got) != 1 || got[0].Label != "internal" {
		t.Fatalf("full mode entries = %+v, want internal only", got)
	}
	if !strings.Contains(got[0].Source, `"svc.internal" -> "svc.worker": "Run()"`) {
		t.Fatalf("full mode sequence should include hidden type interaction: %q", got[0].Source)
	}
}

func TestBuildPackageSequenceSourcesCollapsesSameActorHelpers(t *testing.T) {
	pkg := domain.PackageModel{
		Path: "internal/svc",
		Structs: []domain.StructDef{
			{
				Name: "Service", IsExported: true,
				Methods: []domain.MethodDef{
					{
						Name:       "Run",
						IsExported: true,
						Calls: []domain.CallEdge{
							{To: domain.SymbolRef{Package: "internal/svc", Symbol: "Service.step"}},
						},
					},
					{
						Name: "step",
						Calls: []domain.CallEdge{
							{To: domain.SymbolRef{Package: "internal/svc", Symbol: "Repo.Get"}},
						},
					},
				},
			},
			{
				Name: "Repo", IsExported: true,
				Methods: []domain.MethodDef{{
					Name:       "Get",
					IsExported: true,
				}},
			},
		},
	}

	got := BuildPackageSequenceSources([]domain.PackageModel{pkg}, pkg, SequenceOptions{Mode: OverviewModePublic})
	if len(got) != 1 {
		t.Fatalf("entries = %d, want only Run: %+v", len(got), got)
	}
	if got[0].Label != "Service.Run" {
		t.Fatalf("label = %q, want Service.Run", got[0].Label)
	}
	if strings.Contains(got[0].Source, "step") {
		t.Fatalf("same-actor helper should be collapsed out of D2 source: %q", got[0].Source)
	}
	if !strings.Contains(got[0].Source, `"svc.Service" -> "svc.Repo": "Get()"`) {
		t.Fatalf("sequence should show the public inter-type call: %q", got[0].Source)
	}
}

func TestRenderSVGSequence(t *testing.T) {
	svg, err := RenderSVG(context.Background(), "shape: sequence_diagram\na -> b: call\n")
	if err != nil {
		t.Fatalf("RenderSVG: %v", err)
	}
	if !strings.Contains(string(svg), "<svg") {
		t.Fatalf("rendered SVG missing <svg: %s", svg)
	}
}
