package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kgatilin/wyrd/internal/domain"
)

// fakeReader is a minimal stub ModelReader that records what paths it
// was asked to read and returns canned packages.
type fakeReader struct {
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

func TestReadPackages(t *testing.T) {
	goR := &fakeReader{pkgs: []domain.PackageModel{newPkg("a")}}
	svc := NewService(goR, nil, nil)

	got, err := svc.readPackages(context.Background(), []string{"./..."})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].Path != "a" {
		t.Errorf("got %+v", got)
	}
	if len(goR.calls) != 1 {
		t.Errorf("reader call count: %d", len(goR.calls))
	}
}

func TestReadPackages_PropagatesReaderError(t *testing.T) {
	goR := &fakeReader{returnErr: errors.New("boom")}
	svc := NewService(goR, nil, nil)

	_, err := svc.readPackages(context.Background(), []string{"./..."})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("want reader error, got %v", err)
	}
}

func TestReadPackages_NoReader(t *testing.T) {
	svc := NewService(nil, nil, nil)

	if _, err := svc.readPackages(context.Background(), []string{"./..."}); err == nil {
		t.Error("want error when no reader is configured")
	}
}
