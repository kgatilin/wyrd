package yaml

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kgatilin/wyrd/internal/domain"
)

func TestRoundtrip_Imports(t *testing.T) {
	model := domain.PackageModel{
		Path:    "internal/orders",
		Name:    "orders",
		Imports: []string{"internal/billing", "internal/stock"},
	}
	ctx := context.Background()

	for _, tc := range []struct {
		name       string
		publicOnly bool
		want       []string
	}{
		{"full spec keeps imports", false, []string{"internal/billing", "internal/stock"}},
		{"public spec leaves them out", true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "spec.yaml")
			if err := NewWriter().Write(ctx, model, domain.WriteOptions{OutputPath: path, PublicOnly: tc.publicOnly}); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(string(raw), "imports:"); got != (tc.want != nil) {
				t.Errorf("imports key written = %v, want %v:\n%s", got, tc.want != nil, raw)
			}
			models, err := NewReader().Read(ctx, []string{path})
			if err != nil {
				t.Fatal(err)
			}
			if len(models) != 1 || !reflect.DeepEqual(models[0].Imports, tc.want) {
				t.Fatalf("Imports = %v, want %v", models[0].Imports, tc.want)
			}
		})
	}
}

// TestRead_SpecWithoutImports reads a file written before the imports key
// existed: it loads, and the model has no imports.
func TestRead_SpecWithoutImports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "internal.yaml")
	old := `schema: wyrd/v1
package: internal/orders
name: orders
functions:
    - name: Place
      exported: true
`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	models, err := NewReader().Read(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Imports != nil || len(models[0].Functions) != 1 {
		t.Fatalf("models = %+v, want one package with a function and nil imports", models)
	}
}
