# wyrd retrieval service

wyrd is a **stateful retrieval service over a Go codebase**. Every symbol in
the AST graph (func / method / struct / interface / type / const / var / error)
becomes a *node*; nodes get a dense embedding + a BM25 lexical index. Search is
hybrid (dense cosine + BM25, fused into a calibrated relevance mass) and can
expand along graph edges.
The index lives in `.wyrd/cache/` and refreshes incrementally by content hash.

Boundary: wyrd answers **"what in the code is relevant"** (knowledge). It does
*not* orchestrate — deciding *when* to search, with *what* query, and *what to do
next* belongs to the caller (an agent/orchestrator, IDE, or MCP client).

---

## Architecture (ports & adapters)

Core lives in `internal/retrieval` and depends only on `internal/domain` + ports:

```go
// Asymmetric embedder: documents (nodes) vs queries are embedded differently
// so per-model task instructions can be applied.
type Embedder interface {
    Embed(ctx, texts []string) ([][]float32, error)  // documents
    EmbedQuery(ctx, query string) ([]float32, error) // query (task-instructed)
    Dim() int
    ID() string // "provider:model"; changing it invalidates cached vectors
}

type VectorStore interface {  // dense, keyed by content hash
    Has(hash string) bool
    Get(ctx, hashes []string) (map[string][]float32, error)
    Put(ctx, vectors map[string][]float32) error
    Search(ctx, vec []float32, k int, keep func(hash string) bool) ([]Scored, error)
    Save() error
}

type LexicalIndex interface { // BM25 inverted index (always available), doc id = content hash
    Upsert(id, text string); Remove(id string); Has(id string) bool
    Search(query string, k int, keep func(id string) bool) []Scored; Len() int
}
```

Both channels live in a `retrieval.Corpus`, one per repository, shared by every
worktree; `retrieval.Service` is one worktree's view of it (node id ↔ content
hash, plus the graph): `Index` / `Update` / `EmbedPending` (maintain the view),
`Search` / `Expand` / `Node` (query). See
`docs/features/shared-retrieval/design.md`. There is one search — `Search` — and it
returns both the hits and the community around them.

**Adapters:**

| Adapter | Package | Role |
|---|---|---|
| Ollama embedder | `internal/adapter/embed/ollama` | default; per-model query/doc templates, batching, bounded concurrency |
| OpenAI embedder | `internal/adapter/embed/openai` | remote, OpenAI-compatible `/v1/embeddings`; API key from env only |
| Noop embedder | `internal/adapter/embed/noop` | deterministic stub for tests + degradation fallback |
| Local vector store | `internal/adapter/vindex/vecstore` | default; in-memory brute-force cosine, one append-only `.vec` file per repository |
| Qdrant vector store and lexical index | `internal/adapter/vindex/qdrant` | opt-in (`WYRD_QDRANT_URL` + `WYRD_QDRANT_COLLECTION`); REST, dense kNN and BM25 as sparse vectors, nothing in RAM |
| BM25 lexical index | `internal/adapter/lindex/bm25` | inverted index + BM25, one `lexical@r<N>.bm25` file per repository |

### Nodes & chunking

One graph node = one retrieval unit (`chunk == node`). Embedded text =
enclosing header (`package …`) + signature + docstring + **body** (read from disk
via the symbol's source `Span`, captured by the Go reader). An `Embeddable`
predicate per node decides what gets a vector (default:
`func`/`method`/`iface`/`struct`/`type` yes; `const`/`var`/`error` no).
Oversized bodies are split along AST
statement boundaries into budget-sized sub-chunks (signature as header on each),
then **mean-pooled + L2-normalized into a single vector** — so the index stays
one vector per node. Budget ≈ 2048 chars (~512 tokens).

Node ids follow uigraph's scheme, `{package}.{Symbol}`, and a method extends it
the same way uigraph's members do: `{package}.{Receiver}.{Method}`. Methods are
nodes of their own because a struct's span stops at its type declaration — its
methods are separate declarations, so without a node each their bodies are not
in the corpus at all. Interface methods get no node: an interface's span already
covers its whole declaration, method set included, and there is no body to read.
Their calls hang off the method node, and a `contains` edge ties a type to its
methods; that edge is structural, so it carries no diffusion weight, but
traversals over all edges (`expand`, an answer's induced edges) walk it.

### Asymmetric embedding (task instructions)

Retrieval models want different prompts for documents vs queries; the Ollama
adapter applies them by model family:

| Model | Document | Query |
|---|---|---|
| `qwen3-embedding*` | raw text | `Instruct: <task>\nQuery: <q>` |
| `embeddinggemma*` | `title: none \| text: <d>` | `task: search result \| query: <q>` |
| `nomic-embed*` | `search_document: <d>` | `search_query: <q>` |
| other | raw | raw |

The Qwen3 task string is configurable (`WYRD_EMBED_QUERY_INSTRUCTION`).

### Search is one operation

There is a single search, and it answers "where does this live" and "what does
it hang together with" in one response. The text channels score what the query
matches; that score is the weight each match carries into a local graph
partition — the Andersen–Chung–Lang push approximation of personalized
PageRank, followed by a sweep cut — which returns the community the hits sit
in. Design and references: `docs/features/search-diffusion/design.md`.

The answer is one node list plus the edges induced among those nodes: the
seeds first (`seed: true`, in text-relevance order, carrying `text_score`),
then what the diffusion reached (in diffusion-mass order). A seed is never
dropped — not by `hops`, not by the payload cap — because a search that hides
its own hits is not a search.

- **Seeds carry their mass.** Personalization is proportional to the fused
  relevance of each hit, so the top hit pulls the region toward itself and a
  marginal one barely moves it.
- **Edges are weighted by kind** (`calls` 1.0, `implements` 0.8, `uses`/`returns`
  0.6). The weight map is also the participation list: a kind it does not name
  contributes no edge at all.
- **Hubs are damped.** Each edge is divided by `(d_u·d_v)^τ`, so a path through
  a symbol half the codebase touches costs more than a path between two
  ordinarily connected ones. This is what stops shared helpers turning up on
  every query.
- **The cut is a sweep, not a top-N.** The region is the mass-ordered prefix of
  minimum **conductance** (fraction of edge weight crossing its boundary), so
  its size follows the query: a narrow question yields a small dense community,
  a broad one a larger region. `conductance` ∈ [0,1] comes back with the result
  — low means a real community, high means the best available cut still runs
  through the middle of a hairball, which is the "nothing coherent found" signal.
- **Two bounds sit on top.** `hops` is a hard radius: nothing farther than that
  many undirected edges from a seed is returned, however much mass it collected.
  `MaxGraphNodes` is the payload cap on the region, applied last and by mass,
  and reported as `truncated`.
- **Filters constrain the seeds, not the region.** `filters.kinds` /
  `filters.package_prefix` say what the query text may match; the community
  around a match is the wiring you asked the graph for, and filtering that
  would answer a different question.

Node `score` is the degree-normalized diffusion mass and `text_score` the
calibrated text mass on seeds — both order within one response, never a
threshold across queries. Every knob lives in `retrieval.Params`
(`internal/retrieval/params.go`).

### Graceful degradation

If the embedder is unavailable (e.g. Ollama not running), dense search is
disabled, BM25 keeps working, and responses carry `"dense": false`. Indexing and
serving never fail because of a missing embedder.

---

## Configuration (environment variables)

| Variable | Default | Purpose |
|---|---|---|
| `WYRD_EMBED_PROVIDER` | `ollama` | `ollama` / `openai` / `vertex` / `noop` |
| `WYRD_EMBED_MODEL` | `qwen3-embedding:0.6b` | embedding model |
| `WYRD_EMBED_ENDPOINT` | `http://localhost:11434` | Ollama endpoint |
| `WYRD_EMBED_QUERY_INSTRUCTION` | code-search default | query task instruction (Qwen3) |
| `WYRD_EMBED_BATCH` | `64` | inputs per `/api/embed` request |
| `WYRD_EMBED_CONCURRENCY` | `4` | concurrent batch requests (helps remote; no-op for local embedding models — see Performance) |
| `WYRD_EMBED_API_KEY` | — | API key for `openai` provider (env only, never on disk) |
| `WYRD_RETRIEVAL_DISABLE` | — | `1` disables retrieval entirely |

### Gemini embeddings on Vertex AI (`WYRD_EMBED_PROVIDER=vertex`)

| Variable | Default | Purpose |
|---|---|---|
| `WYRD_VERTEX_PROJECT` | `GOOGLE_CLOUD_PROJECT` | Google Cloud project the requests are billed to; required |
| `WYRD_VERTEX_LOCATION` | `GOOGLE_CLOUD_LOCATION`, else `global` | Vertex AI location |
| `WYRD_EMBED_MODEL` | `gemini-embedding-001` | embedding model |
| `WYRD_EMBED_DIM` | `768` | output dimensionality; the model supports 768, 1536 and 3072 |
| `WYRD_EMBED_DOCUMENT_TASK` | `RETRIEVAL_DOCUMENT` | task type sent with indexed code |
| `WYRD_EMBED_QUERY_TASK` | `CODE_RETRIEVAL_QUERY` | task type sent with a search query |
| `WYRD_EMBED_CONCURRENCY` | `8` | requests in flight |

Authentication is Application Default Credentials: a workload identity or
attached service account in a cluster, `GOOGLE_APPLICATION_CREDENTIALS`, or
`gcloud auth application-default login` on a workstation. The identity needs
`aiplatform.endpoints.predict` on the project (role `roles/aiplatform.user`).
No key is read from the environment or from disk by wyrd itself.

`gemini-embedding-001` takes one text per request, so a cold index of N
symbols is at least N requests; concurrency is the only lever. A throttled
(`429`) or failed (`5xx`) request is retried with exponential backoff, six
attempts in all, honouring `Retry-After`; a rejected one (`4xx`) is not.

The model, the dimensionality and both task types are part of the embedder's
id, and the id names the stored vectors. Changing any of them starts a new
set of vectors and re-embeds; the project and the location are not part of
it, so moving between them keeps what is stored. Vectors are scaled to unit
length on arrival, since the model returns unit vectors only at 3072.

### Qdrant for both channels

| Variable | Default | Purpose |
|---|---|---|
| `WYRD_QDRANT_URL` | `QDRANT_URL` | Qdrant base URL |
| `WYRD_QDRANT_COLLECTION` | — | collection name; Qdrant is used only when this and a URL are set |
| `WYRD_QDRANT_API_KEY` | `QDRANT_API_KEY` | token sent as the `api-key` header |
| `WYRD_QDRANT_ADMIN_API_KEY` | — | token the `wyrd qdrant` commands use instead, when set |
| `WYRD_REQUIRE_REMOTE` | — | `1` makes an unusable embedder or collection a failed start |

Qdrant 1.10 or newer is required: sparse vectors with the IDF modifier and
the `/points/query` API. Verified against 1.19.

The collection holds two named vectors: `dense`, of the embedder's dimension
with Cosine distance, and the sparse `bm25` with `modifier: idf`. The daemon
never creates it:

```bash
wyrd qdrant bootstrap     # create it if missing, verify it either way
wyrd qdrant check         # verify only; non-zero exit when unusable
```

Both take the dimension from the configured embedder, or from `--dim`. A
token that may only read and write points is enough for the daemon;
`bootstrap` needs one that may create a collection.

BM25 documents are tokenized by wyrd, with the tokenizer of the local index —
`loadHTTPConfig`, `load_http_config` and `cfg.LoadHTTPConfig` are found by
`http`, by `config` and by the whole name — and stored as sparse vectors of
term weights; Qdrant applies IDF. Term statistics are those of the whole
collection, so use one collection per repository if you want them exact.

**Migrating a collection created before the sparse channel** (a single
unnamed vector). It keeps working as the dense channel, with the lexical
index in its local file, and the daemon says so at start. To move it:

```bash
export WYRD_QDRANT_COLLECTION=code-v2      # the new collection
wyrd qdrant migrate --from code            # creates code-v2, copies every vector
# restart the daemon with WYRD_QDRANT_COLLECTION=code-v2, then delete "code"
```

Ids and payloads are kept, so nothing is embedded again. The lexical
documents are written by the daemon on its first start against the new
collection; that costs no embedding.

### Required backends and probes

By default every backend problem degrades one channel to something local and
logs why: no embedder credentials means no dense search, an unreachable Qdrant
means local files. `WYRD_REQUIRE_REMOTE=1` is for a deployment whose disk is
not the index: the same conditions make `wyrd serve` exit with the reason, and
the `noop` embedder is refused.

| Endpoint | Answers |
|---|---|
| `GET /healthz` | `200` while the listener serves |
| `GET /readyz` | `200`, or `503` when `WYRD_REQUIRE_REMOTE=1` and a backend failed its last call; the body lists the faults by channel (`embedder`, `vectors`, `lexical`) either way |

A fault clears when the channel next succeeds, and `/readyz` re-asks the
backends at fault on every probe, so a daemon taken out of rotation returns by
itself. A search during a fault answers from the channel that still works and
returns an error when neither does.

---

## Running

```bash
# Ollama running with the embedding model pulled:
ollama pull qwen3-embedding:0.6b

# Serve wyrd (retrieval is wired into the existing server):
wyrd serve --root . --http 127.0.0.1:8800
```

Initial indexing runs in the background on load. To force/await a reconcile,
call `POST /api/refresh`. There is no dedicated search CLI command — retrieval is
exposed over HTTP and MCP on the same `serve` process.

---

## HTTP API

JSON endpoints on the serve process. In multi-worktree mode they are also
available under `/w/{worktree}/…`. Edge `kind` values: `uses`, `returns`,
`implements`, `calls`, `contains`.

### `POST /api/search` — the hits and the community around them
```bash
curl -XPOST :8800/api/search -d '{
  "query": "split function body into chunks",
  "k": 5,
  "hops": 1,
  "filters": { "kinds": ["func","iface"], "package_prefix": "internal/retrieval" }
}'
```
```jsonc
{ "hits": [ { "id","kind","package","name","file","line","signature","doc",
              "score", "seed": true, "text_score": 0.31 } ],
  "edges": [ { "from","to","kind" } ], "dense": true,
  "conductance": 0.11, "seed_count": 2, "truncated": false }
```
The hits the query text matched come first, marked `seed` and ordered by
`text_score`; the nodes the diffusion reached follow in `score` order. See
[Search is one operation](#search-is-one-operation) for what the rest means.

### `POST /api/expand` — neighbours of given nodes
```bash
curl -XPOST :8800/api/expand -d '{
  "node_ids": ["internal/retrieval.Service"],
  "hops": 1,
  "edges": ["uses","calls"]
}'   # "edges": [] or omitted = all kinds
```
```jsonc
{ "nodes": [ … ], "edges": [ … ] }
```

### `GET /api/node/{id}` — full node detail (body + edges)
```bash
curl ":8800/api/node/internal/retrieval.NewService"
```
```jsonc
{ "node_id","kind","package","name","file","signature","doc","body","edges":[…] }
```

### `POST /api/refresh` — reindex changed nodes
```bash
curl -XPOST :8800/api/refresh -d '{}'
```
```jsonc
{ "reindexed": 1360, "removed": 0, "dense": true }
```

---

## MCP tools

Mirror the HTTP endpoints; callable via the MCP registry or
`POST /api/mcp/tools/call`:

| Tool | Arguments | Returns |
|---|---|---|
| `search` | `{query, k, hops, filters}` | seeds + community, edges, `conductance` / `seed_count` / `truncated` |
| `expand` | `{node_ids, hops, edges}` | neighbour nodes |
| `get_node` | `{id}` | node body + edges |
| `refresh` | `{}` | reindex counts |

```bash
curl -XPOST :8800/api/mcp/tools/call \
  -d '{"name":"search","arguments":{"query":"rrf fusion","k":3}}'
```

---

## Using from Claude Code (MCP)

This repo ships a project-scoped `.mcp.json` that registers wyrd as an MCP
server for Claude Code:

```json
{
  "mcpServers": {
    "wyrd": { "command": "wyrd", "args": ["serve", "--mcp-stdio", "--root", "."] }
  }
}
```

`wyrd serve --mcp-stdio` is a **thin client**: it speaks MCP over stdio but
forwards `tools/call` to a background HTTP daemon, auto-starting one if none is
running. Discovery and auto-start are keyed per git worktree (via
`.arch/.worktree/<name>/serve.json`) and serialized by a lock file, so:

- **Multiple Claude Code instances in this project share ONE daemon** — each
  spawns its own lightweight stdio wrapper, but they all connect to the same
  AST/HTTP server and the same warm dense/BM25 indexes. Indexing is paid once.
- The auto-started daemon self-terminates after 15 min idle (then the next call
  re-starts and re-indexes it). To keep it permanently warm, run a manual daemon
  that never times out: `wyrd serve --root . --http 127.0.0.1:0` — the thin
  clients will discover and reuse it.

Setup notes:
- `wyrd` must be on `PATH`. Reinstall after code changes so the daemon runs the
  current build: `go install ./cmd/wyrd` (or `make install`).
- Project-scoped MCP servers require approval on first use; restart/reapprove
  Claude Code after adding `.mcp.json`.
- `--multi` is **not** compatible with `--mcp-stdio`; MCP targets the single
  worktree given by `--root`.

## Freshness

Retrieval state is per repository, under `~/.wyrd/embeddings/<repo key>/`:
- `<embedder>@r<N>.vec` — dense vectors keyed by content hash (local store)
- `lexical@r<N>.bm25` — the BM25 documents some worktree holds

Nothing is persisted per worktree. The file watcher updates the view for the
changed package only and embeds in the background; a text whose content hash
any worktree already embedded is not embedded again; disappeared nodes leave
the view. Changing the embedder model (its `ID()`) or the recipe opens a new
vector file.

---

## Performance notes

- **Initial indexing is a one-time cost** (~5 min for ~1400 nodes with
  `qwen3-embedding:0.6b` on a laptop). Incremental refresh after edits is
  near-instant.
- **`OLLAMA_NUM_PARALLEL` does not speed up embeddings.** Ollama gives embedding
  models a single decode slot (`-np 1`) regardless of the variable, so neither
  it nor client-side concurrency parallelizes local embedding. The only
  Ollama-side win is batching (one larger request packs more per forward pass),
  which is already in place. `WYRD_EMBED_CONCURRENCY` still helps the **remote**
  (OpenAI) embedder, where requests are network-bound and parallelizable.
- Levers if indexing time matters: a smaller/faster model
  (`WYRD_EMBED_MODEL=embeddinggemma:300m`), or embedding less text per node.
