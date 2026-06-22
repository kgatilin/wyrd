# archai retrieval service

archai is a **stateful retrieval service over a Go codebase**. Every symbol in
the AST graph (func / method / struct / interface / type / const / var / error)
becomes a *node*; nodes get a dense embedding + a BM25 lexical index. Search is
hybrid (dense cosine + BM25, fused with RRF) and can expand along graph edges.
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
`Search` / `SearchGraph` / `Expand` / `Node` (query).

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
predicate per node decides what gets a vector (default: `func`/`iface`/`struct`/
`type` yes; `const`/`var`/`error` no). Oversized bodies are split along AST
statement boundaries into budget-sized sub-chunks (signature as header on each),
then **mean-pooled + L2-normalized into a single vector** — so the index stays
one vector per node. Budget ≈ 2048 chars (~512 tokens).

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
`implements`, `calls`.

### `POST /api/search` — hybrid search
```bash
curl -XPOST :8800/api/search -d '{
  "query": "split function body into chunks",
  "k": 5,
  "filters": { "kinds": ["func","iface"], "package_prefix": "internal/retrieval" }
}'
```
```jsonc
{ "results": [ { "node_id", "kind", "file", "doc", "snippet", "score" } ], "dense": true }
```

### `POST /api/search_graph` — search + graph expand
```bash
curl -XPOST :8800/api/search_graph -d '{ "query": "retrieval service", "k": 2, "hops": 1 }'
```
```jsonc
{ "nodes": [ { "id","kind","package","name","signature","doc" } ],
  "edges": [ { "from","to","kind" } ], "dense": true }
```

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
curl ":8800/api/node/internal/retrieval.rrfFuse"
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
| `search` | `{query, k, filters}` | ranked nodes |
| `search_graph` | `{query, k, hops}` | subgraph (nodes + edges) |
| `expand` | `{node_ids, hops, edges}` | neighbour nodes |
| `get_node` | `{id}` | node body + edges |
| `refresh` | `{}` | reindex counts |

```bash
curl -XPOST :8800/api/mcp/tools/call \
  -d '{"name":"search","arguments":{"query":"rrf fusion","k":3}}'
```

---

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
