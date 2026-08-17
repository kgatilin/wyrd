package http

import (
	"encoding/json"
	"fmt"
	nethttp "net/http"
)

// writeJSON marshals v as application/json. On error a 500 is emitted
// — the payload is always a plain struct so failures are exceptional.
func writeJSON(w nethttp.ResponseWriter, v any) {
	buf, err := json.Marshal(v)
	if err != nil {
		nethttp.Error(w, fmt.Sprintf("json marshal: %v", err), nethttp.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(buf)
}
