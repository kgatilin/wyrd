package golang

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kgatilin/wyrd/internal/domain"
)

// writeImportsModule lays out a module whose packages each reach the catalog
// package in one way that leaves no type in a declaration: a blank import, a
// dot import, a call in a body, a constant, a variable initialised from a
// call, a method call on an inferred value, and an import from a _test.go file
// only. It returns the module root.
func writeImportsModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	write("go.mod", "module test.example/shop\n\ngo 1.21\n")
	write("shop.go", `package shop

// Version is the module's version string.
const Version = "1"
`)
	write("catalog/catalog.go", `package catalog

// Size is a constant other packages read.
const Size = 3

// Name returns the catalog name.
func Name() string { return "catalog" }

// Client is reached only through an inferred value.
type Client struct{}

// New builds a Client.
func New() *Client { return &Client{} }

// Hello is called on an inferred *Client.
func (c *Client) Hello() string { return "hello" }
`)
	write("blankuse/blankuse.go", `package blankuse

import _ "test.example/shop/catalog"
`)
	write("dotuse/dotuse.go", `package dotuse

import . "test.example/shop/catalog"

// Label calls through a dot import.
func Label() string { return Name() }
`)
	write("calluse/calluse.go", `package calluse

import (
	"strings"

	"test.example/shop/catalog"
)

// Upper uses catalog only through a call with an inferred result.
func Upper() string {
	x := catalog.Name()
	return strings.ToUpper(x)
}
`)
	write("constuse/constuse.go", `package constuse

import "test.example/shop/catalog"

// Double is a constant computed from another package's constant.
const Double = catalog.Size * 2
`)
	write("varuse/varuse.go", `package varuse

import "test.example/shop/catalog"

// Title is a package variable initialised from a call.
var Title = catalog.Name()
`)
	write("methoduse/methoduse.go", `package methoduse

import "test.example/shop/catalog"

// Greet calls a method on a value whose type is inferred.
func Greet() string {
	c := catalog.New()
	return c.Hello()
}
`)
	write("rootuse/rootuse.go", `package rootuse

import "test.example/shop"

// V reads the module root package.
var V = shop.Version
`)
	write("testonly/testonly.go", `package testonly

// Nothing imports catalog outside the tests.
func Nothing() {}
`)
	write("testonly/testonly_test.go", `package testonly

import (
	"testing"

	"test.example/shop/catalog"
)

func TestNothing(t *testing.T) { _ = catalog.Name() }
`)
	write("testonly/external_test.go", `package testonly_test

import (
	"testing"

	"test.example/shop/catalog"
)

func TestExternal(t *testing.T) { _ = catalog.Size }
`)
	return root
}

func TestReader_RecordsInModuleImports(t *testing.T) {
	root := writeImportsModule(t)
	t.Chdir(root)

	models, err := NewReader().Read(context.Background(), []string{"./..."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	byPath := make(map[string]domain.PackageModel, len(models))
	for _, m := range models {
		byPath[m.Path] = m
	}

	want := map[string][]string{
		"blankuse":  {"catalog"},
		"dotuse":    {"catalog"},
		"calluse":   {"catalog"}, // "strings" is outside the module
		"constuse":  {"catalog"},
		"varuse":    {"catalog"},
		"methoduse": {"catalog"},
		"rootuse":   {"."}, // the module root, in the form its Path takes
		"testonly":  nil,   // only _test.go files import catalog
		"catalog":   nil,
		".":         nil,
	}
	for path, imports := range want {
		m, ok := byPath[path]
		if !ok {
			t.Errorf("package %q missing from models %v", path, modelPaths(models))
			continue
		}
		if !reflect.DeepEqual(m.Imports, imports) {
			t.Errorf("%s: Imports = %v, want %v", path, m.Imports, imports)
		}
	}
}
