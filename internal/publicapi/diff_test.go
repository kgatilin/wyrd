package publicapi

import "testing"

func TestCompareReportsPublicSurfaceChanges(t *testing.T) {
	base := Surface{
		Schema: Schema,
		Packages: []Package{
			{
				Path: "api",
				Name: "api",
				Symbols: []Symbol{
					{
						ID:      "api.Client",
						Name:    "Client",
						Kind:    "interface",
						Members: []Member{{ID: "api.Client.Do", Name: "Do", Kind: "method", Signature: "Do() string"}},
					},
					{ID: "api.Old", Name: "Old", Kind: "type", Signature: "Old : string"},
				},
			},
			{Path: "storage", Name: "storage", Symbols: []Symbol{{ID: "storage.Repository", Name: "Repository", Kind: "interface"}}},
		},
		PackageDeps: []PackageDependency{{ID: "e:api->storage", FromPackage: "api", ToPackage: "storage", Kinds: []string{"returns"}}},
	}
	current := Surface{
		Schema: Schema,
		Packages: []Package{
			{
				Path: "api",
				Name: "api",
				Symbols: []Symbol{
					{
						ID:      "api.Client",
						Name:    "Client",
						Kind:    "interface",
						Members: []Member{{ID: "api.Client.Do", Name: "Do", Kind: "method", Signature: "Do() int"}},
					},
					{ID: "api.New", Name: "New", Kind: "function", Signature: "New() Client"},
				},
			},
			{Path: "cache", Name: "cache", Symbols: []Symbol{{ID: "cache.Cache", Name: "Cache", Kind: "interface"}}},
			{Path: "storage", Name: "storage", Symbols: []Symbol{{ID: "storage.Repository", Name: "Repository", Kind: "interface"}}},
		},
		PackageDeps: []PackageDependency{{ID: "e:api->cache", FromPackage: "api", ToPackage: "cache", Kinds: []string{"uses"}}},
	}

	diff := Compare(current, base)

	changes := map[string]DiffChange{}
	for _, change := range diff.Changes {
		changes[change.Kind+":"+change.ID] = change
	}
	assertChange(t, changes, "member:method:api.Client.Do", "changed")
	assertChange(t, changes, "symbol:function:api.New", "added")
	assertChange(t, changes, "symbol:type:api.Old", "removed")
	assertChange(t, changes, "package:pkg:cache", "added")
	assertChange(t, changes, "package_dependency:e:api->cache", "added")
	assertChange(t, changes, "package_dependency:e:api->storage", "removed")
}

func assertChange(t *testing.T, changes map[string]DiffChange, id, op string) {
	t.Helper()
	change, ok := changes[id]
	if !ok {
		t.Fatalf("missing change %q in %+v", id, changes)
	}
	if change.Op != op {
		t.Fatalf("change %q op = %q, want %q", id, change.Op, op)
	}
}
