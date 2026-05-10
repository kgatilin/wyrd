package java

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReader_ImplementsModelReader(t *testing.T) {
	// Compile-time check is in reader.go; this test asserts the type
	// constructor returns a non-nil value.
	r := NewReader("")
	if r == nil {
		t.Fatal("NewReader returned nil")
	}
}

func TestReader_NoPathsErrors(t *testing.T) {
	r := NewReader("/dev/null") // never invoked because paths is empty
	_, err := r.Read(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "no source paths") {
		t.Errorf("want no-paths error, got %v", err)
	}
}

func TestReader_PropagatesContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := NewReader("/dev/null")
	_, err := r.Read(ctx, []string{"src"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

func TestReader_RecordsWarnings(t *testing.T) {
	r := NewReader("/dev/null")
	r.recordWarnings([]parseWarning{
		{File: "Bad.java", Message: "expected ';'"},
		{File: "Worse.java", Message: "EOF"},
	})
	got := r.Warnings()
	if len(got) != 2 || got[0].File != "Bad.java" || got[1].Message != "EOF" {
		t.Errorf("warnings: %+v", got)
	}
}
