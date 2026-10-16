package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/kgatilin/wyrd/internal/adapter/vindex/qdrant"
	"github.com/kgatilin/wyrd/internal/serve"
)

// qdrantAdminTimeout bounds one administration command.
const qdrantAdminTimeout = 30 * time.Minute

// newQdrantCmd builds `wyrd qdrant`: the operator's side of the Qdrant
// collection the daemon keeps its two text channels in. The daemon only
// verifies its collection; creating one and moving an old one are done here,
// with a token that may do so.
func newQdrantCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "qdrant",
		Short: "Create, verify and migrate the Qdrant collection of the search index",
		Long: `Administer the Qdrant collection the daemon keeps its search index in.

The connection is read from the environment, as the daemon reads it:

  WYRD_QDRANT_URL         base URL (falls back to QDRANT_URL)
  WYRD_QDRANT_COLLECTION  collection name
  WYRD_QDRANT_API_KEY     token (falls back to QDRANT_API_KEY)
  WYRD_QDRANT_ADMIN_API_KEY
                          token used by these commands instead, when set:
                          creating a collection needs more than the daemon's
                          read/write token`,
	}
	cmd.AddCommand(newQdrantBootstrapCmd(), newQdrantCheckCmd(), newQdrantMigrateCmd())
	return cmd
}

// qdrantAdminConfig is the daemon's connection with the administration token
// in place of its own, when one is set.
func qdrantAdminConfig() (qdrant.Config, error) {
	cfg, ok := serve.QdrantFromEnv()
	if !ok {
		return cfg, errors.New("WYRD_QDRANT_URL (or QDRANT_URL) and WYRD_QDRANT_COLLECTION are required")
	}
	if key := os.Getenv("WYRD_QDRANT_ADMIN_API_KEY"); key != "" {
		cfg.APIKey = key
	}
	return cfg, nil
}

// resolveDim is the dense vector's dimension: the flag, else what the
// configured embedder produces.
func resolveDim(ctx context.Context, flag int) (int, error) {
	if flag > 0 {
		return flag, nil
	}
	dim, err := serve.EmbedderDim(ctx)
	if err != nil {
		return 0, fmt.Errorf("%w; pass --dim to name the dimension", err)
	}
	return dim, nil
}

func newQdrantBootstrapCmd() *cobra.Command {
	var dim int
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Create the collection if it is missing, and verify it either way",
		Long: `Create the collection with a dense vector of the embedder's dimension and
the sparse BM25 vector, plus the payload indexes queries filter on. A
collection that already exists is verified and left alone, so the command can
run before every start.

The dimension is --dim, else the configured embedder's (WYRD_EMBED_PROVIDER
and its settings); an embedder that only knows its dimension after a call is
asked for one vector.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), qdrantAdminTimeout)
			defer cancel()
			cfg, err := qdrantAdminConfig()
			if err != nil {
				return err
			}
			if cfg.Dim, err = resolveDim(ctx, dim); err != nil {
				return err
			}
			created, err := qdrant.Bootstrap(ctx, cfg)
			if err != nil {
				return err
			}
			state := "verified"
			if created {
				state = "created"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "collection %q %s: dense vector of %d dimensions, sparse BM25 vector\n", cfg.Collection, state, cfg.Dim)
			return nil
		},
	}
	cmd.Flags().IntVar(&dim, "dim", 0, "dimension of the dense vector (default: the configured embedder's)")
	return cmd
}

func newQdrantCheckCmd() *cobra.Command {
	var dim int
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Report whether the collection can hold both channels",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), qdrantAdminTimeout)
			defer cancel()
			cfg, err := qdrantAdminConfig()
			if err != nil {
				return err
			}
			if cfg.Dim, err = resolveDim(ctx, dim); err != nil {
				return err
			}
			if err := qdrant.Check(ctx, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "collection %q holds a dense vector of %d dimensions and the sparse BM25 vector\n", cfg.Collection, cfg.Dim)
			return nil
		},
	}
	cmd.Flags().IntVar(&dim, "dim", 0, "dimension of the dense vector (default: the configured embedder's)")
	return cmd
}

func newQdrantMigrateCmd() *cobra.Command {
	var from string
	cmd := &cobra.Command{
		Use:   "migrate --from <legacy collection>",
		Short: "Copy a legacy collection's vectors into the configured collection",
		Long: `Copy every vector of a legacy collection — one created with a single
unnamed vector, before the index kept its BM25 documents in Qdrant — into the
configured collection, under its named dense vector. Ids and payloads are
kept, so the daemon finds every vector where it left it and embeds nothing
again.

The target (WYRD_QDRANT_COLLECTION) is created first when it is missing,
with the legacy collection's dimension. The legacy collection is not changed:
delete it once the daemon runs on the new one. BM25 documents are not copied,
because a legacy collection has none; the daemon writes them on its next
start, which costs no embedding.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), qdrantAdminTimeout)
			defer cancel()
			to, err := qdrantAdminConfig()
			if err != nil {
				return err
			}
			if from == "" || from == to.Collection {
				return errors.New("--from must name the legacy collection, and differ from WYRD_QDRANT_COLLECTION")
			}
			src := to
			src.Collection = from
			schema, err := qdrant.Inspect(ctx, src)
			if err != nil {
				return err
			}
			to.Dim = schema.DenseSize
			if _, err := qdrant.Bootstrap(ctx, to); err != nil {
				return err
			}
			n, err := qdrant.Migrate(ctx, src, to)
			if err != nil {
				return fmt.Errorf("after %d point(s): %w", n, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "copied %d vector(s) from %q to %q\n", n, from, to.Collection)
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "the legacy collection to copy from")
	return cmd
}
