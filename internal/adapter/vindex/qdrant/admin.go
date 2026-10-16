package qdrant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Bootstrap makes the collection ready for both channels and reports whether
// it created it. A missing collection is created with a dense vector of
// cfg.Dim dimensions and the sparse vector, and gets keyword indexes on the
// two payload fields every query filters on. An existing one is only
// verified, so running Bootstrap again changes nothing. It needs a token that
// may create collections, which the daemon's does not have to be.
func Bootstrap(ctx context.Context, cfg Config) (created bool, err error) {
	c, err := newClient(cfg)
	if err != nil {
		return false, err
	}
	if cfg.Dim <= 0 {
		return false, fmt.Errorf("qdrant: the dense vector's dimension is required to create collection %q", cfg.Collection)
	}
	schema, err := c.schema(ctx)
	switch {
	case errors.Is(err, ErrNoCollection):
		body := map[string]any{
			"vectors":        map[string]any{DenseVector: map[string]any{"size": cfg.Dim, "distance": "Cosine"}},
			"sparse_vectors": map[string]any{SparseVector: map[string]any{"modifier": "idf"}},
		}
		if err := c.call(ctx, http.MethodPut, "", body, nil); err != nil {
			return false, fmt.Errorf("qdrant: creating collection %q: %w", cfg.Collection, err)
		}
		created = true
	case err != nil:
		return false, err
	default:
		if err := c.checkBoth(schema); err != nil {
			return false, err
		}
	}
	for _, field := range []string{"repo", "recipe"} {
		body := map[string]any{"field_name": field, "field_schema": "keyword"}
		if err := c.call(ctx, http.MethodPut, "/index?wait=true", body, nil); err != nil {
			return created, fmt.Errorf("qdrant: indexing payload field %q of %q: %w", field, cfg.Collection, err)
		}
	}
	return created, nil
}

// Check reports why the collection cannot serve both channels for an
// embedder of cfg.Dim dimensions; nil when it can.
func Check(ctx context.Context, cfg Config) error {
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	schema, err := c.schema(ctx)
	if err != nil {
		return err
	}
	return c.checkBoth(schema)
}

func (c *client) checkBoth(schema Schema) error {
	if schema.Legacy {
		return fmt.Errorf("qdrant: collection %q has a single unnamed vector and no sparse one; create a new collection and copy it over with `wyrd qdrant migrate`", c.cfg.Collection)
	}
	if err := c.checkDense(schema); err != nil {
		return err
	}
	return checkSparse(c.cfg.Collection, schema)
}

// Migrate copies every point of the legacy collection from — a single
// unnamed vector — into the collection to, under the named dense vector,
// keeping ids and payloads, and returns how many it copied. Nothing is
// embedded and from is left as it is. The copy covers every repository and
// recipe in from, since a collection is migrated whole; cfg.Repo and
// cfg.Recipe are not consulted. Copying again overwrites the same points.
func Migrate(ctx context.Context, from, to Config) (int, error) {
	src, err := newClient(from)
	if err != nil {
		return 0, err
	}
	dst, err := newClient(to)
	if err != nil {
		return 0, err
	}
	srcSchema, err := src.schema(ctx)
	if err != nil {
		return 0, err
	}
	if !srcSchema.Legacy {
		return 0, fmt.Errorf("qdrant: collection %q is not a legacy collection (no single unnamed vector)", from.Collection)
	}
	dstSchema, err := dst.schema(ctx)
	if err != nil {
		return 0, err
	}
	dst.cfg.Dim = srcSchema.DenseSize
	if err := dst.checkBoth(dstSchema); err != nil {
		return 0, err
	}

	copied := 0
	var offset any
	for {
		req := map[string]any{"limit": batchSize, "with_payload": true, "with_vector": true}
		if offset != nil {
			req["offset"] = offset
		}
		var resp struct {
			Result struct {
				Points []struct {
					ID      any             `json:"id"`
					Vector  []float32       `json:"vector"`
					Payload json.RawMessage `json:"payload"`
				} `json:"points"`
				NextPageOffset any `json:"next_page_offset"`
			} `json:"result"`
		}
		if err := src.call(ctx, http.MethodPost, "/points/scroll", req, &resp); err != nil {
			return copied, fmt.Errorf("qdrant: reading %q: %w", from.Collection, err)
		}
		points := make([]map[string]any, 0, len(resp.Result.Points))
		for _, p := range resp.Result.Points {
			if len(p.Vector) == 0 {
				continue
			}
			points = append(points, map[string]any{
				"id":      p.ID,
				"vector":  map[string][]float32{DenseVector: p.Vector},
				"payload": p.Payload,
			})
		}
		if len(points) > 0 {
			if err := dst.call(ctx, http.MethodPut, "/points?wait=true", map[string]any{"points": points}, nil); err != nil {
				return copied, fmt.Errorf("qdrant: writing %q: %w", to.Collection, err)
			}
			copied += len(points)
		}
		if resp.Result.NextPageOffset == nil || len(resp.Result.Points) == 0 {
			return copied, nil
		}
		offset = resp.Result.NextPageOffset
	}
}
