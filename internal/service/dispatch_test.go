package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

// fakeReader is a minimal stub ModelReader that records what paths it
// was asked to read and returns canned packages.
type fakeReader struct {
	name      string
	calls     [][]string
	pkgs      []domain.PackageModel
	returnErr error
}

func (f *fakeReader) Read(_ context.Context, paths []string) ([]domain.PackageModel, error) {
	f.calls = append(f.calls, append([]string(nil), paths...))
	if f.returnErr != nil {
		return nil, f.returnErr
	}
	return f.pkgs, nil
}

func newPkg(path string) domain.PackageModel {
	return domain.PackageModel{Path: path, Name: path}
}

func TestReadPackages_GoOnly(t *testing.T) {
	goR := &fakeReader{name: "go", pkgs: []domain.PackageModel{newPkg("a")}}
	svc := NewService(goR, nil, nil)

	got, err := svc.readPackages(context.Background(), []string{"./..."})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].Path != "a" {
		t.Errorf("got %+v", got)
	}
	if len(goR.calls) != 1 {
		t.Errorf("go reader call count: %d", len(goR.calls))
	}
}

func TestReadPackages_JavaOnly_GoSkipsWhenMatcher(t *testing.T) {
	goR := &fakeReader{name: "go", pkgs: []domain.PackageModel{newPkg("go-pkg")}}
	javaR := &fakeReader{name: "java", pkgs: []domain.PackageModel{newPkg("com.example")}}

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "Hello.java"), []byte("class Hello {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := NewService(goR, nil, nil,
		// Override the Go matcher so Java-only inputs skip it: this
		// mirrors the CLI wiring that lands in #103.
		WithLanguageReader("java", javaR, matchSubtreeHasExt(".java")),
	)
	// Replace the default unconditional Go entry with a Go-file matcher
	// so the test exercises pure-Java input.
	for i := range svc.langReaders {
		if svc.langReaders[i].name == "go" {
			svc.langReaders[i].match = matchSubtreeHasExt(".go")
		}
	}

	got, err := svc.readPackages(context.Background(), []string{tmp})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].Path != "com.example" {
		t.Errorf("got %+v", got)
	}
	if len(goR.calls) != 0 {
		t.Errorf("go reader should be skipped on pure-Java input, got calls=%v", goR.calls)
	}
	if len(javaR.calls) != 1 {
		t.Errorf("java reader should run once, got %d", len(javaR.calls))
	}
}

func TestReadPackages_Polyglot(t *testing.T) {
	goR := &fakeReader{name: "go", pkgs: []domain.PackageModel{newPkg("go-pkg")}}
	javaR := &fakeReader{name: "java", pkgs: []domain.PackageModel{newPkg("com.example")}}

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "Hello.java"), []byte("class Hello {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := NewService(goR, nil, nil, WithJavaReader(javaR))

	got, err := svc.readPackages(context.Background(), []string{tmp})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	paths := []string{}
	for _, p := range got {
		paths = append(paths, p.Path)
	}
	sort.Strings(paths)
	if len(paths) != 2 || paths[0] != "com.example" || paths[1] != "go-pkg" {
		t.Errorf("polyglot dispatch should yield both packages, got %v", paths)
	}
}

func TestReadPackages_PropagatesReaderError(t *testing.T) {
	javaR := &fakeReader{name: "java", returnErr: errors.New("boom")}
	tmp := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmp, "Hello.java"), []byte("class Hello {}\n"), 0o644)

	goR := &fakeReader{name: "go"}
	svc := NewService(goR, nil, nil, WithJavaReader(javaR))

	_, err := svc.readPackages(context.Background(), []string{tmp})
	if err == nil || !strings.Contains(err.Error(), "java reader") {
		t.Errorf("want wrapped java reader error, got %v", err)
	}
}

func TestMatchSubtreeHasExt_DetectsJava(t *testing.T) {
	tmp := t.TempDir()
	nested := filepath.Join(tmp, "src", "main", "java", "com", "example")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "X.java"), []byte("class X {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !matchSubtreeHasExt(".java")(tmp) {
		t.Errorf("should detect .java file in nested subtree")
	}
	if matchSubtreeHasExt(".java")(t.TempDir()) {
		t.Errorf("empty tree should not match")
	}
}

func TestMatchSubtreeHasExt_HonoursGoPattern(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "X.java"), []byte("class X {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !matchSubtreeHasExt(".java")(tmp + "/...") {
		t.Errorf("`./...` patterns should be stripped before walking")
	}
}
