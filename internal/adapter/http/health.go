package http

import (
	"encoding/json"
	nethttp "net/http"
)

// WithReadiness installs what /readyz answers from. faults returns the
// backends that failed their last call, by channel; required says whether a
// fault makes the daemon not ready. Returns s for chaining. Safe to call
// before Serve; not safe to call concurrently with Serve.
//
// Readiness is about the backends, not about the model: a worktree still
// being parsed or embedded answers its callers with a loading record and is
// ready to do so.
func (s *Server) WithReadiness(faults func() map[string]string, required bool) *Server {
	s.faults = faults
	s.faultsFail = required
	return s
}

// registerHealthRoutes installs the two probe endpoints at the top level in
// both modes: they describe the process, not a worktree.
//
//   - /healthz answers 200 while the listener serves: liveness.
//   - /readyz answers 200, or 503 when a required backend failed its last
//     call, with the faults in the body either way.
func (s *Server) registerHealthRoutes(mux *nethttp.ServeMux) {
	mux.HandleFunc("/healthz", func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		writeHealth(w, nethttp.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/readyz", func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		var faults map[string]string
		if s.faults != nil {
			faults = s.faults()
		}
		ready := len(faults) == 0 || !s.faultsFail
		body := map[string]any{"ready": ready}
		if len(faults) > 0 {
			body["faults"] = faults
		}
		status := nethttp.StatusOK
		if !ready {
			status = nethttp.StatusServiceUnavailable
		}
		writeHealth(w, status, body)
	})
}

func writeHealth(w nethttp.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
