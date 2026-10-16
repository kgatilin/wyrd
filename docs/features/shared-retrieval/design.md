# Shared retrieval: corpus per repository, view per worktree

Iteration 2 of the shared-model redesign (`docs/features/shared-model/design.md`).
Iteration 1 made the package model one content-addressed store per repository.
Retrieval stayed per worktree: each `serve.State` built its own BM25 index, its
own brute vector index (a full copy of every vector, persisted as
`vectors.json`) and its own graph. On a repo-level daemon with eleven worktrees
that was 3.9 GB of heap, 2 GB of it `json.MarshalIndent` buffers from
concurrent `vectors.json` saves, and eleven concurrent embedding passes against
one embedder. Nearly every symbol's text is the same in every worktree.

## 1. Two concepts

1. **Corpus** — one per repository and embedding recipe, shared by every State
   of the daemon (`retrieval.Corpus`). It holds the two text channels, both
   keyed by a node's **content hash** (`retrieval.ContentHash`, the SHA-256 of
   the node's full text — the key the vector cache already used):
   - the lexical index (BM25), document id = content hash;
   - the dense vector store, vector id = content hash.

   A hash's text is the same wherever it occurs, by construction, so a hash is
   tokenized once and embedded once for the repository, by whichever State
   sees it first.
2. **View** — one per State (`retrieval.Service`, unchanged name). It holds the
   mapping node id ↔ content hash for the nodes this worktree has, and the
   retrieval `Graph` the diffusion walks. Search, expand, readiness and
   coverage are answered by the view against the corpus.

There is no third thing. `Service` is the view: it is constructed with the
corpus injected (`NewService(root, corpus)`), and keeps every method its
consumers call.

## 2. Corpus

```
Corpus
  embedder  Embedder       nil ⇒ lexical only
  vectors   VectorStore    nil ⇒ lexical only        (local | qdrant)
  lexical   LexicalIndex   BM25, doc id = hash
  refs      hash → number of views holding it
```

- **Reference counts.** A view *acquires* a hash when it gains its first node
  with that hash and *releases* it when it loses its last. The first acquire
  tokenizes the text into BM25 (unless the document is already there, e.g.
  loaded from disk); the last release removes the document. So BM25 holds the
  union of what the live views hold, plus whatever was loaded from disk and
  not claimed yet (bounded by the previous run's live set, see §6).
- **Dense vectors are never evicted.** They are the expensive half and a cache
  of every text the repository ever embedded under this recipe; an edit that
  is reverted, or a branch rebased back, finds its vectors again. This is the
  rule the vecstore already had.
- **One embedding pass at a time.** `EmbedPending` takes the corpus's embed
  lock, then re-checks which hashes still lack a vector. Two views missing the
  same hash cannot both embed it, and the embedder sees one pass at a time
  rather than one per worktree. A pass embeds in batches of nodes and stores
  each batch as it lands, so `embedded` climbs during the pass and an embedder
  failure keeps what was stored before it.

### 2.1 Dense backends

The `VectorStore` port (`retrieval/ports.go`):

```go
Has(hash) bool                                   // in memory, cheap
Get(ctx, hashes) (map[hash][]float32, error)     // batch fetch
Put(ctx, map[hash][]float32) error               // batch store
Search(ctx, vec, k, keep func(hash) bool) ([]Scored, error)
Save() error
```

`keep` is the view's membership test. A store returns at most k hashes that
pass it, best first.

- **local** (default) — `internal/adapter/vindex/vecstore`. One in-memory
  brute-force index per repository, backed by
  `~/.wyrd/embeddings/<repo key>/<embedder>@r<N>.vec`. `Search` scores only
  the hashes `keep` accepts, so it is exact: no oversampling. The file is an
  append-only record stream (header, then one record per vector). `Save`
  appends the vectors added since the last save — never a rewrite of the
  file, never an encode of the whole map into one buffer. The previous gob
  format (v1) is still read; the first save after reading one rewrites the
  file once in the new format. A truncated tail (a crash mid-append) is
  dropped on read.
- **qdrant** (opt-in by environment) — `internal/adapter/vindex/qdrant`,
  plain REST over `net/http` + `encoding/json`, no client dependency.
  - Selected when a URL (`WYRD_QDRANT_URL`, else `QDRANT_URL`) **and**
    `WYRD_QDRANT_COLLECTION` are set; the API key is `WYRD_QDRANT_API_KEY`,
    else `QDRANT_API_KEY`, sent as the `api-key` header. The collection has no
    fallback name, so a process that merely has a tenant connection in its
    environment does not switch backends by accident.
  - The collection is created by an operator (the token can read/write it but
    not create it): a single unnamed vector `{size: <dim>, distance: Cosine}`.
    On open the adapter reads it and checks the vector size against the
    embedder's dimension (probed with one query embedding when the adapter has
    not learned it yet). Missing collection, named vectors, a size mismatch,
    an unreachable server or an embedder that cannot be probed: the daemon
    logs why and uses the local backend.
  - Point id = UUID v5 of (namespace = UUID v5 of the recipe
    `<embedder>@r<N>` in the URL namespace, name = content hash). Payload:
    `repo` (the repository key), `recipe`, `hash`.
  - On open, a scroll over `repo`+`recipe` (payload `hash` only, no vectors)
    fills the in-memory `Has` set; puts add to it. Nothing else is held: no
    vector is kept in RAM, and no vecstore file is written (Qdrant is the
    cache).
  - `Search` is a filtered kNN (`repo`, `recipe`) with the limit oversampled
    (×8, at least 100), then `keep` applied and the list cut to k.
  - `Get` fetches points by id in batches, with vectors; `Put` upserts in
    batches with `wait=true`.
  - **Seeding.** When the qdrant backend is selected and the local vecstore
    file for the current recipe exists, the vectors it holds are upserted into
    the collection in batches — only the hashes the collection does not
    already have (the scroll above says which). Nothing is re-embedded. One
    log line reports the count. The file is left in place for an operator to
    delete; the next start finds nothing missing and seeding costs one read of
    the file. The old per-worktree `vectors.json` files need no migration: the
    previous release's `Service.Load` already pushed them into the vecstore.

### 2.2 Lexical index

BM25 stays in RAM, once per repository, with doc id = content hash.
Persistence is one file per repository and recipe,
`~/.wyrd/embeddings/<repo key>/lexical@r<N>.bm25` (the text indexed is the
recipe's embed text, so a recipe change starts it fresh). `Save` streams
the documents the live views hold, token lists only, record by record under
the index's read lock: no `MarshalIndent`, no second copy of the index. Load
rebuilds the postings from the token lists, so a warm start tokenizes nothing.

## 3. View

```
Service (view)
  hashOf   node id → content hash        (every node of the worktree)
  nodesOf  content hash → node ids       (two nodes may share a text)
  graph    retrieval Graph               (adjacency for diffusion/expand)
```

The two maps are an immutable snapshot swapped under the service lock, so a
search reads a consistent view without holding the lock across corpus calls.

- `Index(nodes)` replaces the view; `Update(nodes, removed)` applies a delta.
  Both compute each node's hash (reading each source file once per pass),
  acquire the hashes the view gained and release the ones it lost. Neither
  embeds.
- `EmbedPending` embeds every embeddable node of the *current* view whose hash
  has no vector. It is idempotent and order-free, so the daemon runs it in the
  background after any update without serializing updates behind embedding
  (a watcher edit in one worktree never waits for another worktree's cold
  pass while holding its State's lock).
- `IndexFromModels` = build nodes + graph, `Index`, `EmbedPending` — the
  manual refresh path (MCP `refresh`, `/api/refresh`).

### 3.1 How search maps hashes to nodes

1. The dense channel: `vectors.Search(queryVec, fetchK, keep = view has hash)`.
2. The lexical channel: `lexical.Search(query, fetchK, keep = view has hash)`.
3. Each scored hash expands to every node id of the view that has it, with the
   hash's score. Everything after that — calibrated fusion, the exact-name
   floor, filters, the diffusion — is unchanged and works on node ids.

The local backends filter *before* ranking, so the top k is exact for the
view. Only Qdrant filters after ranking (§2.1).

### 3.2 Readiness

`IndexStatus` is computed for the view: `embeddable` = embeddable nodes of the
view's graph, `embedded` = those whose hash the vector store `Has`,
`pending` = the difference, `in_progress` = an `EmbedPending` of this view is
running (including waiting for the corpus's embed lock behind another view).
`status` keeps reporting ready / indexing / lexical-only per worktree. A cold
State whose hashes are all in the corpus is ready as soon as its view is built,
with no embedder call.

`DenseAvailable` stays per view: false without an embedder or vector store,
false after an embedding failure in this view's pass, true after a successful
pass.

### 3.3 Vectors for clustering

`clustering.Vectors` becomes a batch port,
`Vectors(ctx, ids) (map[id][]float32, error)`, which `Service` implements:
ids → hashes through the view, one batched `Get`, hashes → ids. The semantic
lenses fetch the vectors of their node set once, which is what makes them
usable over Qdrant.

## 4. Wiring

`serve.OpenCorpus(root)` builds the embedder, chooses the backend, opens the
BM25 file and returns the corpus. `cmd/wyrd` opens it once and hands it to
every State through `serve.WithCorpus`, the way the model store is handed out.
A State built without one (tests, `wyrd profile`) builds a private in-memory
corpus with no persistence, which behaves exactly like a one-view corpus.

## 5. What is removed

- `vectors.json` and `bm25.json` under each worktree's `.archai/cache/`:
  neither is read nor written. Existing files are left on disk.
- `adapter/vindex/brute` (the per-worktree vector index): its search is the
  local `VectorStore`.
- `retrieval.VectorCache`, `serve.VectorCacheProvider`, `WithVectorCache`:
  the content-addressed cache *is* the corpus's vector store now.
- `Service.VectorIndex`, `VectorIndexWithLookup`, `LexicalIndex`, `Load`,
  `Refresh` (replaced by `Update` + `EmbedPending`).

## 6. Trade-offs

- **IDF over the union.** BM25 statistics (document count, document
  frequencies, average length) are computed over every version any live view
  holds, not over one tree. Two near-identical versions of a symbol count its
  terms twice. The skew is small and identical for every view; scores still
  rank the same documents in nearly the same order. Duplicate texts inside one
  tree count once, where they used to count once per node.
- **Unclaimed loaded documents.** BM25 documents loaded from disk that no view
  claims stay in RAM for the daemon's lifetime (they are not saved again, so
  they do not survive the next restart). They cost memory and skew IDF a
  little; they never appear in an answer, because the view filters them.
- **Dense never evicts.** The local vector store grows with every text ever
  embedded under the recipe, as the vecstore did. Deleting the file resets
  it; a recipe bump starts a new file.
- **Qdrant oversampling.** A filtered kNN over a long-lived collection can
  spend its oversampled limit on versions no view holds, and then return fewer
  than k for this view. ×8 with a floor of 100 keeps this rare; it is not
  exact.
- **Qdrant fallback is silent apart from the log.** A misconfigured collection
  makes the daemon run on the local backend; the reason is one log line at
  start.
- **Qdrant needs the embedder at start.** The dimension check probes the
  embedder when it has not reported a dimension, so starting with Qdrant
  selected and the embedder down falls back to local.
- **One daemon per repository.** Two daemons appending to one vecstore file
  can interleave records; the reader drops a malformed tail but a record
  spliced mid-stream ends the read there. Two daemons sharing one Qdrant
  collection are safe (upserts are idempotent per hash), but each holds only
  its own `Has` set and can re-embed a hash the other stored after its scroll.
- **Readiness is per view, embedding is per corpus.** A worktree waiting for
  another's embedding pass reports `indexing` for that time.

## 7. Memory shape

With N worktrees: one BM25 index over the union of their distinct texts, one
set of vectors (local) or a set of hash strings (Qdrant), plus per worktree
two maps over its node ids and its retrieval graph. No per-worktree vector
copy, no per-worktree lexical index, no per-worktree persistence buffers.
