package http

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2dagrelayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	d2log "oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"
	"oss.terrastruct.com/util-go/go2"
)

var d2RenderLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// renderD2 compiles the given D2 source into an SVG byte slice using the
// bundled dagre layout engine. It is intentionally small and
// self-contained so M7b-f can call it from any handler that needs a
// server-side diagram render.
func renderD2(ctx context.Context, source string) ([]byte, error) {
	if source == "" {
		return nil, fmt.Errorf("render: empty d2 source")
	}
	ctx = d2log.With(ctx, d2RenderLogger)

	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, fmt.Errorf("render: new ruler: %w", err)
	}

	layoutResolver := func(engine string) (d2graph.LayoutGraph, error) {
		return d2dagrelayout.DefaultLayout, nil
	}

	layout := "dagre"
	compileOpts := &d2lib.CompileOptions{
		Ruler:          ruler,
		Layout:         &layout,
		LayoutResolver: layoutResolver,
	}
	renderOpts := &d2svg.RenderOpts{
		Pad: go2.Pointer(int64(d2svg.DEFAULT_PADDING)),
	}

	diagram, _, err := d2lib.Compile(ctx, source, compileOpts, renderOpts)
	if err != nil {
		return nil, fmt.Errorf("render: compile: %w", err)
	}

	svg, err := d2svg.Render(diagram, renderOpts)
	if err != nil {
		return nil, fmt.Errorf("render: svg: %w", err)
	}
	return svg, nil
}
