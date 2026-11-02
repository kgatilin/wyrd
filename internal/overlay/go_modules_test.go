package overlay

import (
	"reflect"
	"strings"
	"testing"
)

func TestGoModules(t *testing.T) {
	cases := []struct {
		name    string
		overlay string
		want    []string
		wantErr string
	}{
		{name: "absent", overlay: "module: test.example/app\n", want: nil},
		{
			name:    "cleaned",
			overlay: "module: test.example\ngo:\n  modules: [api, ./worker/, .]\n",
			want:    []string{"api", "worker", "."},
		},
		{
			// The reader calls this on every read: an unknown key elsewhere is
			// the overlay gate's to report, not a reason to stop reading code.
			name:    "lenient outside the go block",
			overlay: "module: test.example\nno_such_key: 1\ngo:\n  modules: [api]\n",
			want:    []string{"api"},
		},
		{name: "outside root", overlay: "go:\n  modules: [../other]\n", wantErr: "outside the project root"},
		{name: "absolute", overlay: "go:\n  modules: [/src/api]\n", wantErr: "absolute"},
		{name: "twice", overlay: "go:\n  modules: [api, api/]\n", wantErr: "listed twice"},
		{name: "empty", overlay: "go:\n  modules: [\"\"]\n", wantErr: "empty entry"},
		{name: "malformed", overlay: "go:\n  modules: api\n", wantErr: "go block"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeOverlay(t, tc.overlay)
			got, err := GoModules(dir)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("GoModules: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("GoModules = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGoModulesWithoutOverlay(t *testing.T) {
	got, err := GoModules(t.TempDir())
	if err != nil || got != nil {
		t.Fatalf("GoModules = %v, %v; want nil, nil", got, err)
	}
}

func TestValidateReportsGoModules(t *testing.T) {
	cfg := &Config{
		Module: "test.example",
		Layers: map[string][]string{"all": {"..."}},
		Go:     GoConfig{Modules: []string{"api", "../other"}},
	}
	err := Validate(cfg, "")
	if err == nil || !strings.Contains(err.Error(), `go.modules: "../other" is outside the project root`) {
		t.Fatalf("Validate = %v, want the go.modules entry outside the root reported", err)
	}
}
