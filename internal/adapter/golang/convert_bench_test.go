package golang

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/kgatilin/archai/internal/domain"
)

// convertSerial mirrors the historical (pre-parallel) loop so the bench
// can quantify the speedup from convertPackagesParallel.
func (r *reader) convertSerial(pkgs []*packages.Package) ([]domain.PackageModel, error) {
	results := make([]domain.PackageModel, 0, len(pkgs))
	for _, pkg := range pkgs {
		m, err := r.convertPackage(pkg)
		if err != nil {
			return nil, fmt.Errorf("converting %s: %w", pkg.PkgPath, err)
		}
		results = append(results, m)
	}
	return results, nil
}

// BenchmarkConvertPackagesParallel measures only the conversion phase
// (everything after go/packages.Load), which is the slice that the
// parallel fan-out actually optimises. The loader cost is paid once
// during setup so it does not dominate the timer.
func BenchmarkConvertPackagesParallel(b *testing.B) {
	root := findArchaiRoot(b)
	prev, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		b.Fatalf("chdir: %v", err)
	}
	b.Cleanup(func() { _ = os.Chdir(prev) })

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports |
			packages.NeedModule,
		Context: context.Background(),
	}
	pkgs, err := packages.Load(cfg, "./internal/...")
	if err != nil {
		b.Fatalf("load: %v", err)
	}

	sort.SliceStable(pkgs, func(i, j int) bool { return pkgs[i].PkgPath < pkgs[j].PkgPath })

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := &reader{}
		if len(pkgs) > 0 && pkgs[0].Module != nil {
			r.modulePath = pkgs[0].Module.Path
		}
		if _, err := r.convertPackagesParallel(context.Background(), pkgs); err != nil {
			b.Fatalf("convert: %v", err)
		}
	}
}

// BenchmarkConvertSerial is the serial baseline: same workload, no
// goroutines. The delta against BenchmarkConvertPackagesParallel is the
// real measure of the parallel fan-out on this machine.
func BenchmarkConvertSerial(b *testing.B) {
	root := findArchaiRoot(b)
	prev, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		b.Fatalf("chdir: %v", err)
	}
	b.Cleanup(func() { _ = os.Chdir(prev) })

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports |
			packages.NeedModule,
		Context: context.Background(),
	}
	pkgs, err := packages.Load(cfg, "./internal/...")
	if err != nil {
		b.Fatalf("load: %v", err)
	}
	sort.SliceStable(pkgs, func(i, j int) bool { return pkgs[i].PkgPath < pkgs[j].PkgPath })

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := &reader{}
		if len(pkgs) > 0 && pkgs[0].Module != nil {
			r.modulePath = pkgs[0].Module.Path
		}
		if _, err := r.convertSerial(pkgs); err != nil {
			b.Fatalf("convert: %v", err)
		}
	}
}
