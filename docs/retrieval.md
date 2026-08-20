# archai retrieval service

archai is a **stateful retrieval service over a Go codebase**. Every symbol in
the AST graph (func / method / struct / interface / type / const / var / error)
becomes a *node*; nodes get a dense embedding + a BM25 lexical index. Search is
hybrid (dense cosine + BM25, fused into a calibrated relevance mass) and can
expand along graph edges.
The index lives in `.archai/cache/` and refreshes incrementally by content hash.

Boundary: archai answers **"what in the code is relevant"** (knowledge). It does
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

type VectorIndex interface {  // dense, brute-force cosine top-K
    Upsert(id string, vec []float32); Remove(id string)
    Search(vec []float32, k int) []Scored; Len() int
}

type LexicalIndex interface { // BM25 inverted index (always available)
    Upsert(id, text string); Remove(id string)
    Search(query string, k int) []Scored; Len() int
}
```

`retrieval.Service` orchestrates them: `Index` / `Refresh` (maintain indexes),
`Search` / `Expand` / `Node` (query). There is one search — `Search` — and it
returns both the hits and the community around them.

**Adapters:**

| Adapter | Package | Role |
|---|---|---|
| Ollama embedder | `internal/adapter/embed/ollama` | default; per-model query/doc templates, batching, bounded concurrency |
| OpenAI embedder | `internal/adapter/embed/openai` | remote, OpenAI-compatible `/v1/embeddings`; API key from env only |
| Noop embedder | `internal/adapter/embed/noop` | deterministic stub for tests + degradation fallback |
| Brute vector index | `internal/adapter/vindex/brute` | in-memory cosine top-K, persists `vectors.json` |
| BM25 lexical index | `internal/adapter/lindex/bm25` | inverted index + BM25, persists `bm25.json` |

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

The Qwen3 task string is configurable (`ARCHAI_EMBED_QUERY_INSTRUCTION`).

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
| `ARCHAI_EMBED_PROVIDER` | `ollama` | `ollama` / `openai` / `noop` |
| `ARCHAI_EMBED_MODEL` | `qwen3-embedding:0.6b` | embedding model |
| `ARCHAI_EMBED_ENDPOINT` | `http://localhost:11434` | Ollama endpoint |
| `ARCHAI_EMBED_QUERY_INSTRUCTION` | code-search default | query task instruction (Qwen3) |
| `ARCHAI_EMBED_BATCH` | `64` | inputs per `/api/embed` request |
| `ARCHAI_EMBED_CONCURRENCY` | `4` | concurrent batch requests (helps remote; no-op for local embedding models — see Performance) |
| `ARCHAI_EMBED_API_KEY` | — | API key for `openai` provider (env only, never on disk) |
| `ARCHAI_RETRIEVAL_DISABLE` | — | `1` disables retrieval entirely |

---

## Running

```bash
# Ollama running with the embedding model pulled:
ollama pull qwen3-embedding:0.6b

# Serve archai (retrieval is wired into the existing server):
archai serve --root . --http 127.0.0.1:8800
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

This repo ships a project-scoped `.mcp.json` that registers archai as an MCP
server for Claude Code:

```json
{
  "mcpServers": {
    "archai": { "command": "archai", "args": ["serve", "--mcp-stdio", "--root", "."] }
  }
}
```

`archai serve --mcp-stdio` is a **thin client**: it speaks MCP over stdio but
forwards `tools/call` to a background HTTP daemon, auto-starting one if none is
running. Discovery and auto-start are keyed per git worktree (via
`.arch/.worktree/<name>/serve.json`) and serialized by a lock file, so:

- **Multiple Claude Code instances in this project share ONE daemon** — each
  spawns its own lightweight stdio wrapper, but they all connect to the same
  AST/HTTP server and the same warm dense/BM25 indexes. Indexing is paid once.
- The auto-started daemon self-terminates after 15 min idle (then the next call
  re-starts and re-indexes it). To keep it permanently warm, run a manual daemon
  that never times out: `archai serve --root . --http 127.0.0.1:0` — the thin
  clients will discover and reuse it.

Setup notes:
- `archai` must be on `PATH`. Reinstall after code changes so the daemon runs the
  current build: `go install ./cmd/archai` (or `make install`).
- Project-scoped MCP servers require approval on first use; restart/reapprove
  Claude Code after adding `.mcp.json`.
- `--multi` is **not** compatible with `--mcp-stdio`; MCP targets the single
  worktree given by `--root`.

## Freshness

`.archai/cache/` holds:
- `go-model.json` — parsed AST model, per-file mtime/size stamps (incremental re-parse)
- `vectors.json` — dense vectors keyed by `node_id` + content hash + embedder ID
- `bm25.json` — lexical index

The file watcher re-runs `Refresh` for the changed package only. A symbol whose
embed-text hash is unchanged is not re-embedded; disappeared nodes are dropped.
Changing the embedder model (its `ID()`) invalidates all persisted vectors.

---

## Performance notes

- **Initial indexing is a one-time cost** (~5 min for ~1400 nodes with
  `qwen3-embedding:0.6b` on a laptop). Incremental refresh after edits is
  near-instant.
- **`OLLAMA_NUM_PARALLEL` does not speed up embeddings.** Ollama gives embedding
  models a single decode slot (`-np 1`) regardless of the variable, so neither
  it nor client-side concurrency parallelizes local embedding. The only
  Ollama-side win is batching (one larger request packs more per forward pass),
  which is already in place. `ARCHAI_EMBED_CONCURRENCY` still helps the **remote**
  (OpenAI) embedder, where requests are network-bound and parallelizable.
- Levers if indexing time matters: a smaller/faster model
  (`ARCHAI_EMBED_MODEL=embeddinggemma:300m`), or embedding less text per node.
