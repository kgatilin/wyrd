package http

import (
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

// fixturePackages builds a small model with every symbol kind the
// search indexer understands so the tests can exercise each code path
// without pulling in the Go reader.
func fixturePackages() []domain.PackageModel {
	return []domain.PackageModel{
		{
			Path: "internal/service",
			Name: "service",
			Interfaces: []domain.InterfaceDef{
				{Name: "ModelReader", IsExported: true, SourceFile: "options.go"},
				{Name: "ModelWriter", IsExported: true, SourceFile: "options.go"},
			},
			Structs: []domain.StructDef{
				{Name: "Service", IsExported: true, SourceFile: "service.go"},
			},
			Functions: []domain.FunctionDef{
				{Name: "NewService", IsExported: true, SourceFile: "factory.go"},
			},
			TypeDefs: []domain.TypeDef{
				{Name: "GenerateMode", IsExported: true, SourceFile: "generate.go"},
			},
			Constants: []domain.ConstDef{
				{Name: "DefaultMode", IsExported: true, SourceFile: "generate.go"},
			},
			Variables: []domain.VarDef{
				{Name: "Version", IsExported: true, SourceFile: "service.go"},
			},
			Errors: []domain.ErrorDef{
				{Name: "ErrInvalidMode", IsExported: true, SourceFile: "generate.go"},
			},
		},
		{
			Path: "internal/adapter/golang",
			Name: "golang",
			Structs: []domain.StructDef{
				{Name: "Reader", IsExported: true, SourceFile: "reader.go"},
			},
		},
	}
}

func TestMatchScore_Tiers(t *testing.T) {
	cases := []struct {
		name   string
		target string
		query  string
		want   int
		ok     bool
	}{
		{"exact", "Service", "service", 0, true},
		{"prefix", "ServiceFactory", "serv", 1, true},
		{"substring", "NewService", "serv", 2, true},
		{"fuzzy subsequence", "NewServiceFactory", "nsf", 3, true},
		{"no match", "Reader", "xyz", 0, false},
		{"empty target", "", "q", 0, false},
		{"empty query", "Service", "", 0, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, ok := matchScore(tc.target, tc.query)
			if ok != tc.ok {
				t.Fatalf("matchScore(%q, %q) ok = %v, want %v", tc.target, tc.query, ok, tc.ok)
			}
			if tc.ok && got != tc.want {
				// Fuzzy tier returns 3+ so allow equality check for lower
				// tiers only; for fuzzy, just assert it's >= 3.
				if tc.want >= 3 {
					if got < 3 {
						t.Fatalf("fuzzy score = %d, want >= 3", got)
					}
				} else {
					t.Fatalf("matchScore(%q, %q) = %d, want %d", tc.target, tc.query, got, tc.want)
				}
			}
		})
	}
}

func TestMatchScore_PrefixBeatsSubstring(t *testing.T) {
	prefixScore, _ := matchScore("ServiceFactory", "serv")
	substringScore, _ := matchScore("NewService", "serv")
	if prefixScore >= substringScore {
		t.Fatalf("prefix score (%d) should rank before substring (%d)", prefixScore, substringScore)
	}
}

func TestRunSearch_EmptyQuery(t *testing.T) {
	got := runSearch(fixturePackages(), "", "")
	if len(got) != 0 {
		t.Fatalf("empty query should return no results, got %d", len(got))
	}
}

func TestRunSearch_SymbolByName(t *testing.T) {
	got := runSearch(fixturePackages(), "Service", "")
	if len(got) == 0 {
		t.Fatal("expected matches for 'Service'")
	}
	// "Service" should rank the exact-match struct first.
	if got[0].Name != "Service" || got[0].Kind != searchKindStruct {
		t.Fatalf("first result = %+v, want Service struct", got[0])
	}
}

func TestRunSearch_ByFilePath(t *testing.T) {
	got := runSearch(fixturePackages(), "factory.go", "")
	var sawFile bool
	for _, r := range got {
		if r.Kind == searchKindFile && r.Name == "factory.go" {
			sawFile = true
		}
	}
	if !sawFile {
		t.Fatalf("expected a file result for factory.go, got %d total results", len(got))
	}
}

func TestRunSearch_ByPackageName(t *testing.T) {
	got := runSearch(fixturePackages(), "service", "")
	var sawPackage bool
	for _, r := range got {
		if r.Kind == searchKindPackage && r.Name == "internal/service" {
			sawPackage = true
		}
	}
	if !sawPackage {
		t.Fatal("expected a package result for 'service'")
	}
}

func TestRunSearch_KindFilter(t *testing.T) {
	got := runSearch(fixturePackages(), "service", searchKindInterface)
	if len(got) == 0 {
		t.Fatal("expected interface matches for 'service'")
	}
	for _, r := range got {
		if r.Kind != searchKindInterface {
			t.Fatalf("kind filter violated: got %q result", r.Kind)
		}
	}
}

func TestRunSearch_FuzzyMatch(t *testing.T) {
	// "nsf" should fuzzy-match "NewService" (n-s-… has no 'f') —
	// pick a query that fails prefix/substring but is a subsequence.
	got := runSearch(fixturePackages(), "nwsrv", "")
	var sawFunc bool
	for _, r := range got {
		if r.Name == "NewService" {
			sawFunc = true
		}
	}
	if !sawFunc {
		t.Fatalf("fuzzy 'nwsrv' should match 'NewService', got %d results", len(got))
	}
}

func TestRunSearch_ResultHref(t *testing.T) {
	got := runSearch(fixturePackages(), "Service", searchKindStruct)
	if len(got) == 0 {
		t.Fatal("expected struct results")
	}
	want := "/packages/internal/service#struct-Service"
	if got[0].Href != want {
		t.Fatalf("Href = %q, want %q", got[0].Href, want)
	}
}

func TestRunSearch_LimitResults(t *testing.T) {
	// Build a big package list so we definitely exceed the cap.
	pkgs := []domain.PackageModel{{Path: "internal/big", Name: "big"}}
	for i := 0; i < searchLimit*3; i++ {
		pkgs[0].Structs = append(pkgs[0].Structs, domain.StructDef{
			Name:       "SearchableThing",
			IsExported: true,
			SourceFile: "things.go",
		})
	}
	got := runSearch(pkgs, "Searchable", "")
	if len(got) > searchLimit {
		t.Fatalf("results not capped: got %d, limit %d", len(got), searchLimit)
	}
}

func TestIsKnownKind(t *testing.T) {
	if !isKnownKind("") {
		t.Fatal("empty kind should be accepted (no filter)")
	}
	if !isKnownKind(searchKindStruct) {
		t.Fatal("struct should be a known kind")
	}
	if isKnownKind("bogus") {
		t.Fatal("bogus kind should not be accepted")
	}
}
