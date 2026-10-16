package http

import (
	"net"
	nethttp "net/http"
	"net/http/pprof"
)

// registerDebugRoutes mounts the Go runtime profiles under /debug/pprof/.
// A repo-level daemon holds every worktree's model for as long as it runs,
// so its heap is a question only the live process can answer.
//
// Only a loopback peer is served. The listener can be bound to a wildcard
// host, and a request proxied in (ingress, a pod) arrives from a non-loopback
// address, so the profiles are reachable from this machine alone.
func registerDebugRoutes(mux *nethttp.ServeMux) {
	mux.Handle("/debug/pprof/", loopbackOnly(nethttp.HandlerFunc(pprof.Index)))
	mux.Handle("/debug/pprof/cmdline", loopbackOnly(nethttp.HandlerFunc(pprof.Cmdline)))
	mux.Handle("/debug/pprof/profile", loopbackOnly(nethttp.HandlerFunc(pprof.Profile)))
	mux.Handle("/debug/pprof/symbol", loopbackOnly(nethttp.HandlerFunc(pprof.Symbol)))
	mux.Handle("/debug/pprof/trace", loopbackOnly(nethttp.HandlerFunc(pprof.Trace)))
}

func loopbackOnly(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
			nethttp.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
