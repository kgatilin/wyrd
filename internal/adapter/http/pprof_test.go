package http

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"
)

func TestDebugRoutesServeLoopbackOnly(t *testing.T) {
	mux := nethttp.NewServeMux()
	registerDebugRoutes(mux)

	for _, tc := range []struct {
		remote string
		want   int
	}{
		{"127.0.0.1:5000", nethttp.StatusOK},
		{"[::1]:5000", nethttp.StatusOK},
		{"192.168.5.2:5000", nethttp.StatusNotFound},
	} {
		req := httptest.NewRequest(nethttp.MethodGet, "/debug/pprof/heap", nil)
		req.RemoteAddr = tc.remote
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("remote %s: status %d, want %d", tc.remote, rec.Code, tc.want)
		}
	}
}
