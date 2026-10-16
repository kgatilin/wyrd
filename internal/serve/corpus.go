package serve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kgatilin/wyrd/internal/adapter/embed/noop"
	"github.com/kgatilin/wyrd/internal/adapter/embed/ollama"
	"github.com/kgatilin/wyrd/internal/adapter/embed/openai"
	"github.com/kgatilin/wyrd/internal/adapter/embed/vertex"
	"github.com/kgatilin/wyrd/internal/adapter/lindex/bm25"
	"github.com/kgatilin/wyrd/internal/adapter/vindex/qdrant"
	"github.com/kgatilin/wyrd/internal/adapter/vindex/vecstore"
	"github.com/kgatilin/wyrd/internal/retrieval"
	"github.com/kgatilin/wyrd/internal/worktree"
)

// corpusDirName is the wyrd-home subdirectory holding each repository's
// retrieval corpus files.
const corpusDirName = "embeddings"

// CorpusDir returns the directory holding a repository's retrieval corpus:
// <wyrd home>/embeddings/<repo key>, alongside the daemon registry and keyed
// the same way. Pass the MAIN worktree root (worktree.RepoRoot) so a daemon
// started from a linked worktree resolves to the same directory as one
// started from the repo root.
func CorpusDir(repoRoot string) (string, error) {
	base, err := wyrdHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, corpusDirName, repoKey(repoRoot)), nil
}

// RemoteRequired reports whether the daemon was told its text channels live
// in remote backends (WYRD_REQUIRE_REMOTE=1): an embedder that is not a
// stand-in, and a Qdrant collection holding both the dense vectors and the
// BM25 documents. Without it every one of those degrades to something local
// with a log line, which suits a workstation; with it the same conditions
// fail the start, which is what a deployment whose disk is not the index
// needs.
func RemoteRequired() bool {
	return os.Getenv("WYRD_REQUIRE_REMOTE") == "1"
}

// OpenCorpus opens the retrieval corpus of the repository containing root:
// the embedder, the dense vector store and the lexical index, shared by
// every worktree State the daemon builds. Every State of one repository must
// receive the same corpus: sharing is what it is for.
//
// Both channels are local files unless Qdrant is configured (see
// qdrantFromEnv). A collection with the named dense and sparse vectors holds
// both; a legacy one, with a single unnamed vector, holds the dense channel
// and the lexical index stays in its file. Any reason not to use Qdrant for
// a channel is logged and the local one is used instead — unless
// RemoteRequired, when it is returned.
func OpenCorpus(root string) (*retrieval.Corpus, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("serve: resolving %q: %w", root, err)
	}
	repoRoot := abs
	if mainRoot, ok := worktree.RepoRoot(abs); ok {
		repoRoot = mainRoot
	}
	dir, err := CorpusDir(repoRoot)
	if err != nil {
		return nil, err
	}
	required := RemoteRequired()

	emb, err := buildEmbedder()
	if err != nil {
		if required {
			return nil, fmt.Errorf("serve: embedder: %w", err)
		}
		fmt.Fprintf(os.Stderr, "serve: %v, falling back to %s\n", err, emb.ID())
	}
	if _, standIn := emb.(*noop.Embedder); standIn && required {
		return nil, errors.New("serve: WYRD_REQUIRE_REMOTE is set but the embedder is the noop stand-in; select a provider with WYRD_EMBED_PROVIDER")
	}

	recipe := retrieval.Recipe(emb.ID())
	localPath := vecstore.Path(dir, recipe)
	lexicalPath := filepath.Join(dir, retrieval.LexicalRecipe()+".bm25")
	var (
		vectors retrieval.VectorStore  = vecstore.New(localPath, recipe)
		lexical retrieval.LexicalIndex = bm25.New()
	)

	cfg, configured := qdrantFromEnv()
	cfg.Repo = repoKey(repoRoot)
	switch {
	case !configured && required:
		return nil, errors.New("serve: WYRD_REQUIRE_REMOTE is set but Qdrant is not configured (WYRD_QDRANT_URL and WYRD_QDRANT_COLLECTION)")
	case configured:
		remote, remoteLexical, err := openQdrant(cfg, emb, recipe, localPath)
		if err != nil && required {
			return nil, fmt.Errorf("serve: %w", err)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		}
		if remote != nil {
			vectors = remote
		}
		if remoteLexical != nil {
			lexical, lexicalPath = remoteLexical, ""
		}
	}

	corpus := retrieval.NewCorpus(emb, vectors, lexical, lexicalPath)
	if err := corpus.Load(); err != nil {
		// An unreadable lexical file is a cold index, not a startup failure.
		fmt.Fprintf(os.Stderr, "serve: %v; starting with an empty lexical index\n", err)
	}
	return corpus, nil
}

// qdrantStartTimeout bounds the work at start against Qdrant and the
// embedder: verifying the collection, probing the dimension, seeding.
const qdrantStartTimeout = 10 * time.Minute

// qdrantFromEnv reads the Qdrant connection. The URL and key are
// WYRD_QDRANT_URL / WYRD_QDRANT_API_KEY, else the generic QDRANT_URL /
// QDRANT_API_KEY a service runner injects; the collection is
// WYRD_QDRANT_COLLECTION only. The backend is selected when both a URL and
// the collection are set, so a process that merely has a tenant connection
// in its environment does not switch backends by accident.
func qdrantFromEnv() (qdrant.Config, bool) {
	return QdrantFromEnv()
}

// QdrantFromEnv is the Qdrant connection the daemon would use, and whether
// it is complete enough to select the backend.
func QdrantFromEnv() (qdrant.Config, bool) {
	first := func(names ...string) string {
		for _, n := range names {
			if v := os.Getenv(n); v != "" {
				return v
			}
		}
		return ""
	}
	cfg := qdrant.Config{
		URL:        first("WYRD_QDRANT_URL", "QDRANT_URL"),
		APIKey:     first("WYRD_QDRANT_API_KEY", "QDRANT_API_KEY"),
		Collection: os.Getenv("WYRD_QDRANT_COLLECTION"),
	}
	return cfg, cfg.URL != "" && cfg.Collection != ""
}

// openQdrant opens the collection's two channels. Either may come back nil
// with the error saying why: the dense store when the collection cannot hold
// this embedder's vectors, the lexical index when the collection is a legacy
// one or lacks the sparse vector. A channel that opened is returned even when
// the other did not.
//
// The dense store is opened for the embedder's dimension — probing the
// embedder when nothing else can say — and seeded from the local vector file
// when that exists: only the hashes the collection lacks, and nothing is
// embedded. The file is left in place.
func openQdrant(cfg qdrant.Config, emb retrieval.Embedder, recipe, localPath string) (*qdrant.Store, *qdrant.Lexical, error) {
	ctx, cancel := context.WithTimeout(context.Background(), qdrantStartTimeout)
	defer cancel()

	cfg.Recipe = recipe
	cfg.Dim = emb.Dim()
	if cfg.Dim == 0 {
		cfg.Dim = localDim(localPath, cfg.Recipe)
	}
	if cfg.Dim == 0 {
		// Nothing has been embedded under this recipe yet, so only the
		// embedder can say. This is the one start that needs it up.
		vec, err := emb.EmbedQuery(ctx, "dimension probe")
		if err != nil {
			return nil, nil, fmt.Errorf("qdrant not used, keeping both channels locally: probing the embedder's dimension: %w", err)
		}
		cfg.Dim = len(vec)
	}
	store, err := qdrant.Open(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("qdrant not used, keeping both channels locally: %w", err)
	}

	if _, err := os.Stat(localPath); err == nil {
		n, err := store.Seed(ctx, func(fn func(string, []float32) error) error {
			_, err := vecstore.Scan(localPath, cfg.Recipe, fn)
			return err
		})
		fmt.Fprintf(os.Stderr, "serve: seeded %d vector(s) into qdrant collection %q from %s\n", n, cfg.Collection, localPath)
		if err != nil {
			// What was stored stays stored; the rest is embedded on demand.
			fmt.Fprintf(os.Stderr, "serve: seeding qdrant stopped early: %v\n", err)
		}
	}

	if store.Schema().Legacy {
		return store, nil, fmt.Errorf("qdrant collection %q is a legacy one (a single unnamed vector): it holds the dense vectors, the lexical index stays in its local file; `wyrd qdrant migrate` copies it into a collection that holds both", cfg.Collection)
	}
	cfg.Recipe = retrieval.LexicalRecipe()
	lexical, err := qdrant.OpenLexical(ctx, cfg)
	if err != nil {
		return store, nil, fmt.Errorf("qdrant holds the dense vectors only, keeping the lexical index locally: %w", err)
	}
	return store, lexical, nil
}

// privateCorpus is the corpus of a State built without a shared one (tests,
// `wyrd profile`): in memory only, so it behaves as a one-view corpus and
// writes nothing.
func privateCorpus() *retrieval.Corpus {
	emb, _ := buildEmbedder()
	return retrieval.NewCorpus(emb, vecstore.New("", retrieval.Recipe(emb.ID())), bm25.New(), "")
}

// buildEmbedder creates an embedder based on WYRD_EMBED_PROVIDER:
//   - "ollama" (default): local Ollama server
//   - "openai": OpenAI-compatible API (requires WYRD_EMBED_API_KEY)
//   - "vertex": Gemini embedding models on Vertex AI (requires a project,
//     WYRD_VERTEX_PROJECT or GOOGLE_CLOUD_PROJECT, and Application Default
//     Credentials)
//   - "noop": deterministic pseudo-embeddings for testing
//
// A provider that cannot be built as asked — openai without an API key,
// vertex without a project, a name nobody knows — comes back as an error
// together with the embedder to use in its place (noop, or ollama for an
// unknown name); the caller decides whether a stand-in will do.
func buildEmbedder() (retrieval.Embedder, error) {
	if os.Getenv("WYRD_EMBED_DISABLE") == "1" {
		return noop.New(), nil
	}

	provider := os.Getenv("WYRD_EMBED_PROVIDER")
	switch provider {
	case "openai":
		emb := openai.New()
		if !emb.HasAPIKey() {
			return noop.New(), errors.New("openai embedder selected but WYRD_EMBED_API_KEY is not set")
		}
		return emb, nil

	case "vertex":
		emb := vertex.New()
		if !emb.HasProject() {
			return noop.New(), errors.New("vertex embedder selected but neither WYRD_VERTEX_PROJECT nor GOOGLE_CLOUD_PROJECT is set")
		}
		return emb, nil

	case "noop":
		return noop.New(), nil

	case "ollama", "":
		// Default to Ollama (it will fail gracefully if not running)
		return ollama.New(), nil

	default:
		return ollama.New(), fmt.Errorf("unknown WYRD_EMBED_PROVIDER %q", provider)
	}
}

// localDim reads the dimension of the vectors the local file holds for the
// recipe, 0 when there is no such file or it is empty. An embedder that
// reports its dimension only after its first call (Ollama, OpenAI) would
// otherwise make Qdrant's selection depend on the embedder being up at
// start, and a daemon restarted while it was down would silently keep its
// vectors local until the next restart.
func localDim(path, recipe string) int {
	dim := 0
	stop := errors.New("first vector read")
	_, err := vecstore.Scan(path, recipe, func(_ string, vec []float32) error {
		dim = len(vec)
		return stop
	})
	if err != nil && !errors.Is(err, stop) {
		return 0
	}
	return dim
}

// EmbedderDim reports the dimension of the configured embedder's vectors,
// asking the embedder for one vector when it does not know before its first
// call. It is what a collection has to be created with.
func EmbedderDim(ctx context.Context) (int, error) {
	emb, err := buildEmbedder()
	if err != nil {
		return 0, err
	}
	if dim := emb.Dim(); dim > 0 {
		return dim, nil
	}
	vec, err := emb.EmbedQuery(ctx, "dimension probe")
	if err != nil {
		return 0, fmt.Errorf("probing the embedder's dimension: %w", err)
	}
	return len(vec), nil
}
