package http

import (
	"encoding/json"
	"fmt"
	nethttp "net/http"

	"github.com/kgatilin/archai/internal/plugin"
)

type modelEventJSON struct {
	Kind   string   `json:"kind"`
	Paths  []string `json:"paths,omitempty"`
	Target string   `json:"target,omitempty"`
	At     string   `json:"at"`
}

func (s *Server) handleModelEvents(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodGet {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "no state available", nethttp.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(nethttp.Flusher)
	if !ok {
		nethttp.Error(w, "streaming unsupported", nethttp.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")

	events := make(chan plugin.ModelEvent, 8)
	cancel := state.Bus().Subscribe(func(ev plugin.ModelEvent) {
		select {
		case events <- ev:
		default:
		}
	})
	defer cancel()

	fmt.Fprint(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-events:
			data, err := json.Marshal(modelEventJSON{
				Kind:   string(ev.Kind),
				Paths:  ev.Paths,
				Target: ev.Target,
				At:     ev.At.Format("2006-01-02T15:04:05.000Z07:00"),
			})
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: model-changed\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}
