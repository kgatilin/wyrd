package http

import (
	"context"
	"fmt"
	nethttp "net/http"
	"strings"
)

// writeText sends text/plain with a Content-Disposition attachment so
// browsers prompt the user to save. Used by the D2 export endpoints.
func writeText(w nethttp.ResponseWriter, filename, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	_, _ = w.Write([]byte(body))
}

// writeSVG renders D2 source to SVG and sends it as an attachment.
func writeSVG(w nethttp.ResponseWriter, ctx context.Context, filename, source string) {
	svg, err := renderD2(ctx, source)
	if err != nil {
		nethttp.Error(w, "render: "+err.Error(), nethttp.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	_, _ = w.Write(svg)
}

// safeFilename replaces path separators so a package path is usable as
// a download filename.
func safeFilename(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	if s == "" {
		s = "package"
	}
	return s
}
