package http

import (
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
)

func probe(t *testing.T, srv *Server, path string) (int, map[string]any) {
	t.Helper()
	mux := nethttp.NewServeMux()
	srv.registerHealthRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(nethttp.MethodGet, path, nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: body %q: %v", path, rec.Body.String(), err)
	}
	return rec.Code, body
}

func TestReadyz(t *testing.T) {
	down := func() map[string]string { return map[string]string{"vectors": "connection refused"} }
	up := func() map[string]string { return nil }

	tests := []struct {
		name     string
		faults   func() map[string]string
		required bool
		want     int
	}{
		{name: "no check installed", want: nethttp.StatusOK},
		{name: "required and healthy", faults: up, required: true, want: nethttp.StatusOK},
		{name: "required and a backend down", faults: down, required: true, want: nethttp.StatusServiceUnavailable},
		// A workstation daemon answers from what still works, and says what
		// does not.
		{name: "not required and a backend down", faults: down, want: nethttp.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := (&Server{}).WithReadiness(tc.faults, tc.required)
			code, body := probe(t, srv, "/readyz")
			if code != tc.want {
				t.Errorf("status = %d, want %d", code, tc.want)
			}
			if body["ready"] != (tc.want == nethttp.StatusOK) {
				t.Errorf("ready = %v with status %d", body["ready"], code)
			}
			faults, _ := body["faults"].(map[string]any)
			if tc.faults != nil && len(tc.faults()) > 0 && faults["vectors"] != "connection refused" {
				t.Errorf("faults = %v, want the failing channel named", body["faults"])
			}
		})
	}
}

// Liveness does not follow the backends: a daemon whose Qdrant is down must
// be taken out of rotation, not restarted.
func TestHealthzIgnoresFaults(t *testing.T) {
	srv := (&Server{}).WithReadiness(func() map[string]string { return map[string]string{"embedder": "quota"} }, true)
	if code, _ := probe(t, srv, "/healthz"); code != nethttp.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
}
