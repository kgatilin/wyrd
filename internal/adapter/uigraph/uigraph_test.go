package uigraph

import (
	"testing"

	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
	"github.com/kgatilin/archai/internal/publicapi"
)

func TestParseChangePath(t *testing.T) {
	cases := []struct {
		in         string
		wantPkg    string
		wantType   string
		wantMember string
		wantLevel  string // "package" | "type" | "member"
	}{
		{"internal/service", "internal/service", "", "", "package"},
		{"internal/service.Service", "internal/service", "Service", "", "type"},
		{"internal/service.Service.Handle", "internal/service", "Service", "Handle", "member"},
		{"github.com/x/y/pkg.T.M", "github.com/x/y/pkg", "T", "M", "member"},
	}
	for _, c := range cases {
		got := parseChangePath(c.in)
		if got.Pkg != c.wantPkg || got.Type != c.wantType || got.Member != c.wantMember || got.Level != c.wantLevel {
			t.Errorf("parseChangePath(%q) = %+v, want pkg=%q type=%q member=%q level=%q",
				c.in, got, c.wantPkg, c.wantType, c.wantMember, c.wantLevel)
		}
	}
}

func componentByID(components []Component, id string) *Component {
	for i := range components {
		if components[i].ID == id {
			return &components[i]
		}
	}
	return nil
}

func TestProjectMarksAddedInterface(t *testing.T) {
	// "current" has an interface Svc.
	// "target" has no interfaces (Svc is new in current).
	// After diff.Compute(current, target), Svc should show as OpAdd.
	current := []domain.PackageModel{
		{
			Path: "internal/svc",
			Name: "svc",
			Interfaces: []domain.InterfaceDef{
				{
					Name:       "Svc",
					IsExported: true,
					Methods: []domain.MethodDef{
						{Name: "Handle", IsExported: true},
					},
				},
			},
		},
	}
	target := []domain.PackageModel{
		{
			Path: "internal/svc",
			Name: "svc",
			// No interfaces - Svc is new
		},
	}

	d := diff.Compute(current, target)

	g, err := Project(current, nil, d)
	if err != nil {
		t.Fatal(err)
	}

	if g.Schema != Schema {
		t.Errorf("Schema = %q, want %q", g.Schema, Schema)
	}
	if len(g.Components) != 1 {
		t.Fatalf("len(Components) = %d, want 1", len(g.Components))
	}

	comp := g.Components[0]
	if comp.ID != "internal/svc" {
		t.Errorf("Component.ID = %q, want %q", comp.ID, "internal/svc")
	}

	var svcInternal *Internal
	for i := range comp.Internals {
		if comp.Internals[i].Name == "Svc" {
			svcInternal = &comp.Internals[i]
			break
		}
	}
	if svcInternal == nil {
		t.Fatal("Svc internal not found")
	}
	if svcInternal.Kind != "iface" {
		t.Errorf("Svc.Kind = %q, want %q", svcInternal.Kind, "iface")
	}
	// diff.Compute sees Svc in current but not in target => OpAdd
	if svcInternal.Diff != "added" {
		t.Errorf("Svc.Diff = %q, want %q", svcInternal.Diff, "added")
	}

	// Check that the member is present
	if len(svcInternal.Members) != 1 {
		t.Fatalf("len(Members) = %d, want 1", len(svcInternal.Members))
	}
	if svcInternal.Members[0].Name != "Handle()" {
		t.Errorf("Member.Name = %q, want %q", svcInternal.Members[0].Name, "Handle()")
	}
}

func TestProjectWithPublicDiffMarksAddedMethodOnChangedInterface(t *testing.T) {
	base := []domain.PackageModel{
		{
			Path: "application",
			Name: "application",
			Interfaces: []domain.InterfaceDef{
				{
					Name:       "Application",
					IsExported: true,
					SourceFile: "application.go",
					Methods: []domain.MethodDef{
						{Name: "Sessions", IsExported: true},
					},
				},
			},
		},
		{
			Path: "session",
			Name: "session",
			Interfaces: []domain.InterfaceDef{
				{Name: "SessionGraphRepository", IsExported: true},
			},
		},
	}
	current := []domain.PackageModel{
		{
			Path: "application",
			Name: "application",
			Interfaces: []domain.InterfaceDef{
				{
					Name:       "Application",
					IsExported: true,
					SourceFile: "application.go",
					Methods: []domain.MethodDef{
						{Name: "Sessions", IsExported: true},
						{
							Name:       "SessionGraphRepository",
							IsExported: true,
							Returns:    []domain.TypeRef{{Package: "session", Name: "SessionGraphRepository"}},
						},
					},
				},
			},
		},
		{
			Path: "session",
			Name: "session",
			Interfaces: []domain.InterfaceDef{
				{Name: "SessionGraphRepository", IsExported: true},
			},
		},
	}

	d := diff.Compute(current, base)
	pd := publicapi.Compare(publicapi.Project(current), publicapi.Project(base))
	g, err := ProjectWithPublicDiff(current, nil, d, &pd)
	if err != nil {
		t.Fatal(err)
	}

	application := componentByID(g.Components, "application")
	if application == nil {
		t.Fatal("application component not found")
	}
	var appInterface *Internal
	for i := range application.Internals {
		if application.Internals[i].ID == "application.Application" {
			appInterface = &application.Internals[i]
			break
		}
	}
	if appInterface == nil {
		t.Fatal("application.Application internal not found")
	}
	if appInterface.Diff != "changed" {
		t.Fatalf("Application.Diff = %q, want changed", appInterface.Diff)
	}

	memberDiffs := map[string]string{}
	for _, member := range appInterface.Members {
		memberDiffs[member.ID] = member.Diff
	}
	if memberDiffs["application.Application.Sessions"] != "" {
		t.Errorf("Sessions diff = %q, want empty", memberDiffs["application.Application.Sessions"])
	}
	if memberDiffs["application.Application.SessionGraphRepository"] != "added" {
		t.Errorf("SessionGraphRepository diff = %q, want added", memberDiffs["application.Application.SessionGraphRepository"])
	}
	for _, member := range appInterface.Members {
		if member.ID == "application.Application.SessionGraphRepository" {
			if member.SourceFile != "application.go" {
				t.Errorf("SessionGraphRepository.SourceFile = %q, want application.go", member.SourceFile)
			}
			if member.DiffAfter != "SessionGraphRepository() session.SessionGraphRepository" {
				t.Errorf("SessionGraphRepository.DiffAfter = %q, want signature", member.DiffAfter)
			}
			if member.DiffBefore != "" {
				t.Errorf("SessionGraphRepository.DiffBefore = %q, want empty", member.DiffBefore)
			}
		}
	}
}

func TestProjectWithPublicDiffCarriesChangedMethodBeforeAfter(t *testing.T) {
	base := []domain.PackageModel{
		{
			Path: "api",
			Name: "api",
			Interfaces: []domain.InterfaceDef{
				{
					Name:       "Client",
					IsExported: true,
					SourceFile: "client.go",
					Methods: []domain.MethodDef{
						{Name: "Do", IsExported: true, Returns: []domain.TypeRef{{Name: "string"}}},
					},
				},
			},
		},
	}
	current := []domain.PackageModel{
		{
			Path: "api",
			Name: "api",
			Interfaces: []domain.InterfaceDef{
				{
					Name:       "Client",
					IsExported: true,
					SourceFile: "client.go",
					Methods: []domain.MethodDef{
						{Name: "Do", IsExported: true, Returns: []domain.TypeRef{{Name: "int"}}},
					},
				},
			},
		},
	}

	d := diff.Compute(current, base)
	pd := publicapi.Compare(publicapi.Project(current), publicapi.Project(base))
	g, err := ProjectWithPublicDiff(current, nil, d, &pd)
	if err != nil {
		t.Fatal(err)
	}

	api := componentByID(g.Components, "api")
	if api == nil {
		t.Fatal("api component not found")
	}
	var got *Member
	for _, internal := range api.Internals {
		for i := range internal.Members {
			if internal.Members[i].ID == "api.Client.Do" {
				got = &internal.Members[i]
			}
		}
	}
	if got == nil {
		t.Fatal("api.Client.Do member not found")
	}
	if got.Diff != "changed" {
		t.Fatalf("Do.Diff = %q, want changed", got.Diff)
	}
	if got.DiffBefore != "Do() string" {
		t.Errorf("Do.DiffBefore = %q, want Do() string", got.DiffBefore)
	}
	if got.DiffAfter != "Do() int" {
		t.Errorf("Do.DiffAfter = %q, want Do() int", got.DiffAfter)
	}
}

func TestProjectCarriesLegacyMemberBeforeAfterOnChangedInterface(t *testing.T) {
	base := []domain.PackageModel{
		{
			Path: "internal/worker",
			Name: "worker",
			Interfaces: []domain.InterfaceDef{
				{
					Name:       "worker",
					SourceFile: "worker.go",
					Methods: []domain.MethodDef{
						{Name: "build", Returns: []domain.TypeRef{{Name: "string"}}},
						{Name: "keep", Returns: []domain.TypeRef{{Name: "int"}}},
					},
				},
			},
		},
	}
	current := []domain.PackageModel{
		{
			Path: "internal/worker",
			Name: "worker",
			Interfaces: []domain.InterfaceDef{
				{
					Name:       "worker",
					SourceFile: "worker.go",
					Methods: []domain.MethodDef{
						{Name: "build", Returns: []domain.TypeRef{{Name: "int"}}},
						{Name: "keep", Returns: []domain.TypeRef{{Name: "int"}}},
						{Name: "sessionGraphRepository", Returns: []domain.TypeRef{{Package: "session", Name: "SessionGraphRepository"}}},
					},
				},
			},
		},
		{
			Path:       "session",
			Name:       "session",
			Interfaces: []domain.InterfaceDef{{Name: "SessionGraphRepository", IsExported: true}},
		},
	}

	g, err := Project(current, nil, diff.Compute(current, base))
	if err != nil {
		t.Fatal(err)
	}

	worker := componentByID(g.Components, "internal/worker")
	if worker == nil {
		t.Fatal("internal/worker component not found")
	}
	var iface *Internal
	for i := range worker.Internals {
		if worker.Internals[i].ID == "internal/worker.worker" {
			iface = &worker.Internals[i]
			break
		}
	}
	if iface == nil {
		t.Fatal("internal/worker.worker internal not found")
	}

	members := map[string]Member{}
	for _, member := range iface.Members {
		members[member.ID] = member
	}
	if members["internal/worker.worker.keep"].Diff != "" {
		t.Errorf("keep diff = %q, want empty", members["internal/worker.worker.keep"].Diff)
	}
	build := members["internal/worker.worker.build"]
	if build.Diff != "changed" {
		t.Fatalf("build diff = %q, want changed", build.Diff)
	}
	if build.DiffBefore != "build() string" || build.DiffAfter != "build() int" {
		t.Errorf("build before/after = %q -> %q, want build() string -> build() int", build.DiffBefore, build.DiffAfter)
	}
	added := members["internal/worker.worker.sessionGraphRepository"]
	if added.Diff != "added" {
		t.Fatalf("sessionGraphRepository diff = %q, want added", added.Diff)
	}
	if added.DiffBefore != "" || added.DiffAfter != "sessionGraphRepository() session.SessionGraphRepository" {
		t.Errorf("sessionGraphRepository before/after = %q -> %q, want added signature", added.DiffBefore, added.DiffAfter)
	}
}

func TestProjectWithPublicDiffEmitsRemovedPublicMember(t *testing.T) {
	base := []domain.PackageModel{
		{
			Path: "api",
			Name: "api",
			Interfaces: []domain.InterfaceDef{
				{
					Name:       "Existing",
					IsExported: true,
					Methods: []domain.MethodDef{
						{Name: "Do", IsExported: true},
						{Name: "Old", IsExported: true},
					},
				},
			},
		},
	}
	current := []domain.PackageModel{
		{
			Path: "api",
			Name: "api",
			Interfaces: []domain.InterfaceDef{
				{
					Name:       "Existing",
					IsExported: true,
					Methods: []domain.MethodDef{
						{Name: "Do", IsExported: true},
					},
				},
			},
		},
	}

	d := diff.Compute(current, base)
	pd := publicapi.Compare(publicapi.Project(current), publicapi.Project(base))
	g, err := ProjectWithPublicDiff(current, nil, d, &pd)
	if err != nil {
		t.Fatal(err)
	}

	api := componentByID(g.Components, "api")
	if api == nil {
		t.Fatal("api component not found")
	}
	var existing *Internal
	for i := range api.Internals {
		if api.Internals[i].ID == "api.Existing" {
			existing = &api.Internals[i]
			break
		}
	}
	if existing == nil {
		t.Fatal("api.Existing internal not found")
	}
	var old *Member
	for i := range existing.Members {
		if existing.Members[i].ID == "api.Existing.Old" {
			old = &existing.Members[i]
			break
		}
	}
	if old == nil {
		t.Fatal("removed api.Existing.Old member not projected")
	}
	if old.Diff != "removed" {
		t.Errorf("Old.Diff = %q, want removed", old.Diff)
	}
	if old.Name != "Old()" {
		t.Errorf("Old.Name = %q, want Old()", old.Name)
	}
}

func TestProjectEmitsSymbolRelationsFromSignatures(t *testing.T) {
	dep := domain.Dependency{
		From:            domain.SymbolRef{Package: "application", Symbol: "Application"},
		To:              domain.SymbolRef{Package: "session", Symbol: "SessionGraphRepository"},
		Kind:            domain.DependencyReturns,
		ThroughExported: true,
	}
	base := []domain.PackageModel{
		{
			Path: "application",
			Name: "application",
			Interfaces: []domain.InterfaceDef{
				{
					Name:       "Application",
					IsExported: true,
					SourceFile: "application.go",
					Methods:    []domain.MethodDef{{Name: "Sessions", IsExported: true}},
				},
			},
		},
		{
			Path:       "session",
			Name:       "session",
			Interfaces: []domain.InterfaceDef{{Name: "SessionGraphRepository", IsExported: true}},
		},
	}
	current := []domain.PackageModel{
		{
			Path: "application",
			Name: "application",
			Interfaces: []domain.InterfaceDef{
				{
					Name:       "Application",
					IsExported: true,
					SourceFile: "application.go",
					Methods: []domain.MethodDef{
						{Name: "Sessions", IsExported: true},
						{
							Name:       "SessionGraphRepository",
							IsExported: true,
							Returns:    []domain.TypeRef{{Package: "session", Name: "SessionGraphRepository"}},
						},
					},
				},
			},
			Dependencies: []domain.Dependency{dep},
		},
		{
			Path: "session",
			Name: "session",
			Interfaces: []domain.InterfaceDef{
				{Name: "SessionGraphRepository", IsExported: true},
			},
			Structs: []domain.StructDef{
				{
					Name:       "Service",
					SourceFile: "service.go",
					Fields: []domain.FieldDef{
						{Name: "Repo", Type: domain.TypeRef{Name: "SessionGraphRepository"}},
					},
				},
			},
		},
	}

	g, err := Project(current, nil, diff.Compute(current, base))
	if err != nil {
		t.Fatal(err)
	}

	relations := map[string]SymbolRelation{}
	for _, relation := range g.Relations {
		relations[relation.ID] = relation
	}

	methodRelationID := "r:returns:application.Application.SessionGraphRepository->session.SessionGraphRepository"
	methodRelation, ok := relations[methodRelationID]
	if !ok {
		t.Fatalf("missing method relation %q in %+v", methodRelationID, g.Relations)
	}
	if methodRelation.FromMemberID != "application.Application.SessionGraphRepository" {
		t.Errorf("FromMemberID = %q, want application.Application.SessionGraphRepository", methodRelation.FromMemberID)
	}
	if methodRelation.Kind != "returns" || !methodRelation.Public || methodRelation.Diff != "added" {
		t.Errorf("method relation = %+v, want public added returns", methodRelation)
	}

	intraRelationID := "r:uses:session.Service.Repo->session.SessionGraphRepository"
	intraRelation, ok := relations[intraRelationID]
	if !ok {
		t.Fatalf("missing intra-package relation %q in %+v", intraRelationID, g.Relations)
	}
	if intraRelation.FromComponentID != "session" || intraRelation.ToComponentID != "session" {
		t.Errorf("intra relation endpoints = %q -> %q, want session -> session", intraRelation.FromComponentID, intraRelation.ToComponentID)
	}
}

func TestProjectMarksAddedDependencyEdge(t *testing.T) {
	dep := domain.Dependency{
		From: domain.SymbolRef{Package: "app", Symbol: "Service"},
		To:   domain.SymbolRef{Package: "repo", Symbol: "Repository"},
		Kind: domain.DependencyUses,
	}
	current := []domain.PackageModel{
		{Path: "app", Name: "app", Dependencies: []domain.Dependency{dep}},
		{Path: "repo", Name: "repo"},
	}
	target := []domain.PackageModel{
		{Path: "app", Name: "app"},
		{Path: "repo", Name: "repo"},
	}

	d := diff.Compute(current, target)
	g, err := Project(current, nil, d)
	if err != nil {
		t.Fatal(err)
	}

	if len(g.Edges) != 1 {
		t.Fatalf("len(Edges) = %d, want 1", len(g.Edges))
	}
	if g.Edges[0].ID != "e:app->repo" {
		t.Errorf("Edge.ID = %q, want e:app->repo", g.Edges[0].ID)
	}
	if g.Edges[0].Diff != "added" {
		t.Errorf("Edge.Diff = %q, want added", g.Edges[0].Diff)
	}
}

func TestProjectMarksPublicDependencyEdgesAndPortsFromPublicSurface(t *testing.T) {
	publicDep := domain.Dependency{
		From:            domain.SymbolRef{Package: "api", Symbol: "NewRepository"},
		To:              domain.SymbolRef{Package: "storage", Symbol: "Repository"},
		Kind:            domain.DependencyReturns,
		ThroughExported: true,
	}
	privateDep := domain.Dependency{
		From: domain.SymbolRef{Package: "api", Symbol: "helper"},
		To:   domain.SymbolRef{Package: "internal/cache", Symbol: "Cache"},
		Kind: domain.DependencyUses,
	}
	current := []domain.PackageModel{
		{
			Path: "api",
			Name: "api",
			Functions: []domain.FunctionDef{
				{Name: "NewRepository", IsExported: true},
				{Name: "helper"},
			},
			Dependencies: []domain.Dependency{publicDep, privateDep},
		},
		{
			Path: "storage",
			Name: "storage",
			Interfaces: []domain.InterfaceDef{
				{Name: "Repository", IsExported: true},
			},
		},
		{Path: "internal/cache", Name: "cache"},
	}

	g, err := Project(current, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	edges := map[string]Edge{}
	for _, edge := range g.Edges {
		edges[edge.ID] = edge
	}
	if !edges["e:api->storage"].Public {
		t.Fatalf("api -> storage edge is not public: %+v", edges["e:api->storage"])
	}
	if edges["e:api->internal/cache"].Public {
		t.Fatalf("api -> internal/cache edge should not be public: %+v", edges["e:api->internal/cache"])
	}

	api := componentByID(g.Components, "api")
	if api == nil {
		t.Fatal("api component not found")
	}
	ports := map[string]Port{}
	for _, port := range api.Ports {
		ports[port.ID] = port
	}
	if !ports["api:out:storage"].Public {
		t.Fatalf("api:out:storage port is not public: %+v", ports["api:out:storage"])
	}
	if ports["api:out:internal/cache"].Public {
		t.Fatalf("api:out:internal/cache port should not be public: %+v", ports["api:out:internal/cache"])
	}

	storage := componentByID(g.Components, "storage")
	if storage == nil {
		t.Fatal("storage component not found")
	}
	var repoPort *Port
	for i := range storage.Ports {
		if storage.Ports[i].ID == "storage:in:Repository" {
			repoPort = &storage.Ports[i]
			break
		}
	}
	if repoPort == nil || !repoPort.Public {
		t.Fatalf("storage public interface port = %+v, want public", repoPort)
	}
}

func TestProjectEmitsRemovedDependencyEdgeWhenPackagesRemain(t *testing.T) {
	dep := domain.Dependency{
		From:            domain.SymbolRef{Package: "app", Symbol: "Service"},
		To:              domain.SymbolRef{Package: "repo", Symbol: "Repository"},
		Kind:            domain.DependencyUses,
		ThroughExported: true,
	}
	current := []domain.PackageModel{
		{Path: "app", Name: "app"},
		{Path: "repo", Name: "repo"},
	}
	target := []domain.PackageModel{
		{Path: "app", Name: "app", Dependencies: []domain.Dependency{dep}},
		{Path: "repo", Name: "repo"},
	}

	d := diff.Compute(current, target)
	g, err := Project(current, nil, d)
	if err != nil {
		t.Fatal(err)
	}

	if len(g.Edges) != 1 {
		t.Fatalf("len(Edges) = %d, want 1", len(g.Edges))
	}
	if g.Edges[0].ID != "e:app->repo" {
		t.Errorf("Edge.ID = %q, want e:app->repo", g.Edges[0].ID)
	}
	if g.Edges[0].Diff != "removed" {
		t.Errorf("Edge.Diff = %q, want removed", g.Edges[0].Diff)
	}
	if !g.Edges[0].Public {
		t.Errorf("Edge.Public = false, want true for removed public dependency")
	}
}

func TestProjectEmitsPackageLevelPublicSurfaceSymbols(t *testing.T) {
	current := []domain.PackageModel{
		{
			Path: "api",
			Name: "api",
			Functions: []domain.FunctionDef{
				{
					Name:       "NewClient",
					IsExported: true,
					Params:     []domain.ParamDef{{Name: "addr", Type: domain.TypeRef{Name: "string"}}},
					Returns:    []domain.TypeRef{{Name: "Client"}},
					Doc:        "NewClient constructs a client.",
				},
				{Name: "helper"},
			},
			TypeDefs: []domain.TypeDef{
				{
					Name:           "Mode",
					UnderlyingType: domain.TypeRef{Name: "string"},
					Constants:      []string{"ModeFast", "modeSlow"},
					IsExported:     true,
				},
			},
			Constants: []domain.ConstDef{
				{Name: "DefaultPort", Type: domain.TypeRef{Name: "int"}, Value: "8080", IsExported: true},
			},
			Variables: []domain.VarDef{
				{Name: "DefaultClient", Type: domain.TypeRef{Name: "Client"}, IsExported: true},
			},
			Errors: []domain.ErrorDef{
				{Name: "ErrMissing", Message: "missing", IsExported: true},
			},
		},
	}
	target := []domain.PackageModel{{Path: "api", Name: "api"}}

	g, err := Project(current, nil, diff.Compute(current, target))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Components) != 1 {
		t.Fatalf("len(Components) = %d, want 1", len(g.Components))
	}
	comp := g.Components[0]
	if comp.Desc != "NewClient constructs a client." {
		t.Errorf("Component.Desc = %q, want function doc first line", comp.Desc)
	}

	got := map[string]Internal{}
	for _, internal := range comp.Internals {
		got[internal.ID] = internal
	}
	cases := []struct {
		id       string
		kind     string
		exported bool
		diff     string
	}{
		{"api.NewClient", "func", true, "added"},
		{"api.helper", "func", false, "added"},
		{"api.Mode", "type", true, "added"},
		{"api.DefaultPort", "const", true, "added"},
		{"api.DefaultClient", "var", true, "added"},
		{"api.ErrMissing", "error", true, "added"},
	}
	for _, tt := range cases {
		internal, ok := got[tt.id]
		if !ok {
			t.Fatalf("missing internal %q in %+v", tt.id, comp.Internals)
		}
		if internal.Kind != tt.kind {
			t.Errorf("%s Kind = %q, want %q", tt.id, internal.Kind, tt.kind)
		}
		if internal.Exported != tt.exported {
			t.Errorf("%s Exported = %v, want %v", tt.id, internal.Exported, tt.exported)
		}
		if internal.Diff != tt.diff {
			t.Errorf("%s Diff = %q, want %q", tt.id, internal.Diff, tt.diff)
		}
	}

	mode := got["api.Mode"]
	if len(mode.Members) != 2 {
		t.Fatalf("Mode members = %+v, want 2 constants", mode.Members)
	}
	if mode.Members[0].Name != "ModeFast" || !mode.Members[0].Exported {
		t.Errorf("Mode first member = %+v, want exported ModeFast", mode.Members[0])
	}
	if mode.Members[1].Name != "modeSlow" || mode.Members[1].Exported {
		t.Errorf("Mode second member = %+v, want unexported modeSlow", mode.Members[1])
	}
}

func TestProjectEmitsLayerRulePolicyViolations(t *testing.T) {
	models := []domain.PackageModel{
		{
			Path: "internal/domain",
			Name: "domain",
			Dependencies: []domain.Dependency{
				{
					From: domain.SymbolRef{Package: "internal/domain", Symbol: "Order"},
					To:   domain.SymbolRef{Package: "internal/service", Symbol: "Service"},
					Kind: domain.DependencyUses,
				},
			},
		},
		{Path: "internal/service", Name: "service"},
	}
	cfg := &overlay.Config{
		Module: "github.com/example/app",
		Layers: map[string][]string{
			"domain":  {"internal/domain/..."},
			"service": {"internal/service/..."},
		},
		LayerRules: map[string][]string{
			"service": {"domain"},
		},
	}

	g, err := Project(models, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(g.PolicyViolations) != 1 {
		t.Fatalf("len(PolicyViolations) = %d, want 1: %+v", len(g.PolicyViolations), g.PolicyViolations)
	}
	v := g.PolicyViolations[0]
	if v.ID != "policy:layer-rule:internal/domain->internal/service" {
		t.Errorf("PolicyViolation.ID = %q, want policy:layer-rule:internal/domain->internal/service", v.ID)
	}
	if v.SourceComponentID != "internal/domain" || v.TargetComponentID != "internal/service" {
		t.Errorf("PolicyViolation endpoints = %q -> %q, want internal/domain -> internal/service", v.SourceComponentID, v.TargetComponentID)
	}
	if v.SourceLayer != "domain" || v.TargetLayer != "service" {
		t.Errorf("PolicyViolation layers = %q -> %q, want domain -> service", v.SourceLayer, v.TargetLayer)
	}
	if v.Kind != "layer_rule" {
		t.Errorf("PolicyViolation.Kind = %q, want layer_rule", v.Kind)
	}
}

func TestProjectWithNoDiff(t *testing.T) {
	// Test that Project works without a diff
	models := []domain.PackageModel{
		{
			Path: "internal/domain",
			Name: "domain",
			Structs: []domain.StructDef{
				{
					Name:       "Order",
					IsExported: true,
					Fields: []domain.FieldDef{
						{Name: "ID", IsExported: true, Type: domain.TypeRef{Name: "string"}},
					},
				},
			},
		},
	}

	g, err := Project(models, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if g.Schema != Schema {
		t.Errorf("Schema = %q, want %q", g.Schema, Schema)
	}
	if g.PR != nil {
		t.Error("PR should be nil when no diff")
	}
	if len(g.Components) != 1 {
		t.Fatalf("len(Components) = %d, want 1", len(g.Components))
	}

	comp := g.Components[0]
	if comp.Name != "domain" {
		t.Errorf("Component.Name = %q, want %q", comp.Name, "domain")
	}
	if comp.Tech != "Go" {
		t.Errorf("Component.Tech = %q, want %q", comp.Tech, "Go")
	}

	// Check struct is an internal
	if len(comp.Internals) != 1 {
		t.Fatalf("len(Internals) = %d, want 1", len(comp.Internals))
	}
	internal := comp.Internals[0]
	if internal.Kind != "class" {
		t.Errorf("Internal.Kind = %q, want %q", internal.Kind, "class")
	}
	if internal.Name != "Order" {
		t.Errorf("Internal.Name = %q, want %q", internal.Name, "Order")
	}

	// Check field is a member
	if len(internal.Members) != 1 {
		t.Fatalf("len(Members) = %d, want 1", len(internal.Members))
	}
	member := internal.Members[0]
	if member.Kind != "prop" {
		t.Errorf("Member.Kind = %q, want %q", member.Kind, "prop")
	}
	if !member.Exported {
		t.Error("Member.Exported = false, want true")
	}
}

func TestReviewViewSelectorMatchesIncludesAndExcludes(t *testing.T) {
	selector := overlay.PackageSelector{
		Include: []string{"*"},
		Exclude: []string{"internal", "internal/...", "tools/..."},
	}
	cases := []struct {
		pkg  string
		want bool
	}{
		{"config", true},
		{"session", true},
		{"internal", false},
		{"internal/service", false},
		{"tools/codegen", false},
		{"plugin/testdata", false},
	}
	for _, tt := range cases {
		if got := selectorMatches(selector, tt.pkg); got != tt.want {
			t.Errorf("selectorMatches(%q) = %v, want %v", tt.pkg, got, tt.want)
		}
	}
}

func TestReviewViewSelectorSupportsNestedWildcard(t *testing.T) {
	selector := overlay.PackageSelector{
		Include: []string{"internal/plugins/..."},
		Exclude: []string{"internal/plugins/*/testdata/..."},
	}
	cases := []struct {
		pkg  string
		want bool
	}{
		{"internal/plugins", true},
		{"internal/plugins/sessions", true},
		{"internal/plugins/sessions/api", true},
		{"internal/plugins/sessions/testdata", false},
		{"internal/plugins/sessions/testdata/fixtures", false},
		{"internal/other", false},
	}
	for _, tt := range cases {
		if got := selectorMatches(selector, tt.pkg); got != tt.want {
			t.Errorf("selectorMatches(%q) = %v, want %v", tt.pkg, got, tt.want)
		}
	}
}

func TestProjectEmitsConfiguredReviewViews(t *testing.T) {
	models := []domain.PackageModel{
		{Path: "config", Name: "config"},
		{Path: "session", Name: "session"},
		{Path: "internal/runtime", Name: "runtime"},
		{Path: "tools/codegen", Name: "codegen"},
	}
	cfg := &overlay.Config{
		ReviewViews: map[string]overlay.ReviewView{
			"framework_api": {
				Title:            "Framework API",
				DefaultScope:     "top_level_public_api",
				DefaultExpansion: "collapsed",
				GroupBy:          "api_area",
				Packages: overlay.PackageSelector{
					Include: []string{"*"},
					Exclude: []string{"internal/...", "tools/..."},
				},
			},
		},
	}
	g, err := Project(models, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.ReviewScopes) != 4 {
		t.Fatalf("len(ReviewScopes) = %d, want 4", len(g.ReviewScopes))
	}
	if len(g.ReviewViews) != 1 {
		t.Fatalf("len(ReviewViews) = %d, want 1", len(g.ReviewViews))
	}
	view := g.ReviewViews[0]
	if view.ID != "framework_api" {
		t.Errorf("ReviewView.ID = %q, want framework_api", view.ID)
	}
	if view.DefaultScope != "top_level_public_api" {
		t.Errorf("ReviewView.DefaultScope = %q, want top_level_public_api", view.DefaultScope)
	}
	if view.DefaultExpansion != "collapsed" {
		t.Errorf("ReviewView.DefaultExpansion = %q, want collapsed", view.DefaultExpansion)
	}
	if view.GroupBy != "api_area" {
		t.Errorf("ReviewView.GroupBy = %q, want api_area", view.GroupBy)
	}
	want := []string{"config", "session"}
	if len(view.ComponentIDs) != len(want) {
		t.Fatalf("ComponentIDs = %v, want %v", view.ComponentIDs, want)
	}
	for i := range want {
		if view.ComponentIDs[i] != want[i] {
			t.Errorf("ComponentIDs[%d] = %q, want %q", i, view.ComponentIDs[i], want[i])
		}
	}
}

func TestProjectEmitsReviewGroupings(t *testing.T) {
	models := []domain.PackageModel{
		{Path: "config", Name: "config", Layer: "api", Structs: []domain.StructDef{{Name: "Config", IsExported: true}}},
		{Path: "internal/runtime", Name: "runtime", Layer: "runtime"},
		{Path: "internal/plugins/sessions", Name: "sessions", Layer: "plugin"},
	}
	cfg := &overlay.Config{
		Aggregates: map[string]overlay.Aggregate{
			"config": {Root: "config.Config"},
		},
		BoundedContexts: map[string]overlay.BoundedContext{
			"framework": {Name: "Framework", Aggregates: []string{"config"}},
		},
		ReviewViews: map[string]overlay.ReviewView{
			"framework_api": {
				Title:        "Framework API",
				DefaultScope: "top_level_public_api",
				GroupBy:      "configured_groups",
				Packages: overlay.PackageSelector{
					Include: []string{"*"},
					Exclude: []string{"internal/..."},
				},
			},
		},
		PackageOwners: map[string]overlay.PackageOwner{
			"platform": {
				Name: "Platform API",
				Packages: overlay.PackageSelector{
					Include: []string{"*"},
					Exclude: []string{"internal/..."},
				},
			},
			"plugins": {
				Name: "Plugin Team",
				Packages: overlay.PackageSelector{
					Include: []string{"internal/plugins/..."},
				},
			},
			"runtime": {
				Name: "Runtime Team",
				Packages: overlay.PackageSelector{
					Include: []string{"internal/runtime"},
				},
			},
		},
	}

	g, err := Project(models, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}

	if g.DefaultGrouping != "configured_groups" {
		t.Errorf("DefaultGrouping = %q, want configured_groups", g.DefaultGrouping)
	}

	groupings := make(map[string]ReviewGrouping)
	for _, grouping := range g.ReviewGroupings {
		groupings[grouping.ID] = grouping
	}
	for _, id := range []string{"review_view", "configured_groups", "layer", "directory", "package_owner"} {
		if _, ok := groupings[id]; !ok {
			t.Fatalf("missing ReviewGrouping %q in %#v", id, g.ReviewGroupings)
		}
	}

	if got := groupComponentIDs(groupings["review_view"], "review_view:framework_api"); !sameStrings(got, []string{"config"}) {
		t.Errorf("review_view:framework_api ComponentIDs = %v, want [config]", got)
	}
	if got := groupComponentIDs(groupings["configured_groups"], "configured_groups:framework"); !sameStrings(got, []string{"config"}) {
		t.Errorf("configured_groups:framework ComponentIDs = %v, want [config]", got)
	}
	if got := groupComponentIDs(groupings["layer"], "layer:api"); !sameStrings(got, []string{"config"}) {
		t.Errorf("layer:api ComponentIDs = %v, want [config]", got)
	}
	if got := groupComponentIDs(groupings["directory"], "directory:internal"); !sameStrings(got, []string{"internal/plugins/sessions", "internal/runtime"}) {
		t.Errorf("directory:internal ComponentIDs = %v, want internal packages", got)
	}
	if got := groupComponentIDs(groupings["package_owner"], "package_owner:platform"); !sameStrings(got, []string{"config"}) {
		t.Errorf("package_owner:platform ComponentIDs = %v, want [config]", got)
	}
	if got := groupComponentIDs(groupings["package_owner"], "package_owner:runtime"); !sameStrings(got, []string{"internal/runtime"}) {
		t.Errorf("package_owner:runtime ComponentIDs = %v, want [internal/runtime]", got)
	}
	if got := groupComponentIDs(groupings["package_owner"], "package_owner:plugins"); !sameStrings(got, []string{"internal/plugins/sessions"}) {
		t.Errorf("package_owner:plugins ComponentIDs = %v, want [internal/plugins/sessions]", got)
	}
}

func TestProjectEmitsUnownedPackageOwnerGroupingByDefault(t *testing.T) {
	models := []domain.PackageModel{
		{Path: "config", Name: "config"},
		{Path: "internal/runtime", Name: "runtime"},
	}
	g, err := Project(models, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ownerGrouping *ReviewGrouping
	for i := range g.ReviewGroupings {
		if g.ReviewGroupings[i].ID == "package_owner" {
			ownerGrouping = &g.ReviewGroupings[i]
			break
		}
	}
	if ownerGrouping == nil {
		t.Fatalf("missing package_owner grouping in %+v", g.ReviewGroupings)
	}
	if got := groupComponentIDs(*ownerGrouping, "package_owner:unowned"); !sameStrings(got, []string{"config", "internal/runtime"}) {
		t.Errorf("package_owner:unowned ComponentIDs = %v, want all packages", got)
	}
}

func groupComponentIDs(grouping ReviewGrouping, groupID string) []string {
	for _, group := range grouping.Groups {
		if group.ID == groupID {
			return group.ComponentIDs
		}
	}
	return nil
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestProjectDefaultsToFirstNonEmptyReviewView(t *testing.T) {
	models := []domain.PackageModel{
		{Path: "internal/runtime", Name: "runtime"},
	}
	g, err := Project(models, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if g.DefaultReviewView != "all" {
		t.Errorf("DefaultReviewView = %q, want all", g.DefaultReviewView)
	}
	if g.DefaultReviewScope != "all_public_api" {
		t.Errorf("DefaultReviewScope = %q, want all_public_api", g.DefaultReviewScope)
	}
}
