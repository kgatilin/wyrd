# Wyrd

Architecture diagram generator for Go projects. (Renamed from archai; the
name is the Norse "web of wyrd" — fate as a woven graph. Legacy names still
work everywhere: `archai.yaml` and `ARCHAI_HOME` are read as fallbacks, and
`.wyrd/` is accepted alongside `.arch/` in every read path, while the tool
keeps writing to `.arch/` and keeps its cache dir named `.archai/`.) Analyzes Go source code and generates D2 diagrams showing interfaces, structs, functions, and their relationships.

## Project Structure

```
wyrd/
├── cmd/wyrd/           # CLI entry point (Cobra)
│   └── main.go           # Wires dependencies, defines commands
│
├── cmd/wyrd-check/     # Validation-only CLI shipped to CI (see below)
│
├── internal/
│   ├── domain/           # Core domain models (data containers)
│   │   ├── package.go    # PackageModel - aggregate root
│   │   ├── interface.go  # InterfaceDef
│   │   ├── struct.go     # StructDef
│   │   ├── function.go   # FunctionDef
│   │   ├── method.go     # MethodDef, ParamDef, TypeRef
│   │   ├── field.go      # FieldDef
│   │   ├── dependency.go # Dependency, SymbolRef
│   │   ├── typedef.go    # TypeDef (for enums)
│   │   ├── stereotype.go # Stereotype enum
│   │   └── module.go     # Module (root context)
│   │
│   ├── adapter/
│   │   ├── golang/       # Go code adapter
│   │   │   ├── reader.go     # Parses Go code → domain models
│   │   │   ├── stereotype.go # Stereotype detection heuristics
│   │   │   └── writer.go     # Placeholder for future
│   │   │
│   │   ├── d2/           # D2 diagram adapter
│   │   │   ├── writer.go     # domain models → D2 files
│   │   │   ├── builder.go    # D2 text generation
│   │   │   ├── templates.go  # Legend template
│   │   │   └── styles.go     # Color mappings
│   │   │
│   │   └── git/          # Git adapter
│   │       └── diff.go       # working-tree diff → FileStat + patches
│   │
│   ├── check/            # CI gates: overlay rules, policy, target drift
│   │
│   └── service/          # Business operations
│       ├── service.go    # Service struct
│       ├── factory.go    # NewService(reader, writer)
│       ├── options.go    # ModelReader/ModelWriter interfaces
│       └── generate.go   # Generate operation
│
├── tests/
│   └── integration/      # Integration tests
│
└── docs/
    └── features/         # Feature specifications and plans
```

## Architecture

**Hexagonal Architecture (Ports & Adapters)**:
- Domain models at the center (no dependencies)
- Adapters handle read/write for different formats
- Service layer orchestrates operations
- CLI wires dependencies (proper DI)

**Key Interfaces**:
```go
type ModelReader interface {
    Read(ctx context.Context, paths []string) ([]domain.PackageModel, error)
}

type ModelWriter interface {
    Write(ctx context.Context, model domain.PackageModel, opts WriteOptions) error
}
```

## Usage

```bash
# Generate diagrams for packages
wyrd diagram generate ./internal/...

# Public API only
wyrd diagram generate ./internal/... --pub

# Internal implementation only
wyrd diagram generate ./internal/... --internal
```

Output goes to `.arch/` folder in each package:
- `pub.d2` - exported symbols only
- `internal.d2` - all symbols

## What's Implemented (US-1)

- [x] Go code parsing with `golang.org/x/tools/go/packages`
- [x] Symbol extraction (interfaces, structs, functions, type definitions)
- [x] Stereotype detection (factory functions via `New*` prefix)
- [x] D2 diagram generation with legend
- [x] File-based grouping of symbols
- [x] Dependency tracking and visualization
- [x] CLI with Cobra

## D2 Output Format

Stereotypes:
- `<<interface>>` - Go interfaces
- `<<struct>>` - Go structs
- `<<factory>>` - Factory functions (New* prefix)
- `<<function>>` - Regular functions
- `<<enum>>` - Type definitions with constants

Each function is its own class shape with parameters as fields and a `return` field.

## Architectural Analysis (MCP graph tools)

Beyond diagram generation, wyrd runs as an MCP server (`wyrd serve
--mcp-stdio`, wired in `.mcp.json`) exposing a typed **code graph** of the
project and a set of analysis lenses over it. The graph models:

```
package ─contains→ file ─contains→ type/function
type ─contains→ method/field
fn/method ─calls/usesType/returns→ type/fn      (behavioral flow edges)
struct ─implements→ interface
```

Node id scheme (see `internal/adapter/archmotif/exporter.go`):
`pkg:<path>`, `file:<path>/<base>`, `type:<path>.<Name>`, `fn:<path>.<Name>`,
`method:<path>.<Recv>.<Name>`, `field:<path>.<Struct>.<Name>`.

**Dogfood the MCP tools — do NOT explore wyrd with raw `grep`/`Read` first.**
wyrd serves a live graph of its own source. To understand where something lives
or how it is wired, reach for the wyrd MCP tools *before* shell search:
`search` (semantic + graph search for symbols/code, in one call), `get_node` /
`get_package` / `list_packages` (inspect a node or package surface), `expand`
(walk neighbors / callers / callees / implementers from a seed node). Use
`grep`/`Read` only to read an exact span once a graph tool has located it. The
graph tools return architecture (edges, callers, implementers) that grep cannot.
If a tool returns "daemon unreachable", start/refresh the daemon first rather than
silently falling back to grep.

### Daemon lifecycle (CLI)

The `.mcp.json` thin client auto-starts a repo-level daemon; manage it with the
`wyrd daemon` group (target = current repo by default, or `[name|pid]` —
numeric → PID, else repo basename):

- `wyrd daemon start` — start a repo-level (`--multi`) daemon for the repo
  containing cwd; idempotent. Resolves the repo root from any worktree.
- `wyrd daemon status [name|pid]` — readiness + indexing progress (the `status`
  tool over HTTP); the right thing to poll after a restart on a big repo.
- `wyrd daemon list` — live daemons (repo, worktrees, pid, url, caps, uptime).
- `wyrd daemon restart [name|pid]` / `stop [name|pid]` — zombie-aware (reads
  `ps` state, not just `kill -0`); restart always relaunches. Both can target a
  legacy per-worktree `serve.json` daemon, not just the global registry.

**Pin the port, or the URL is unbookmarkable.** `serve.http_addr` in
`wyrd.yaml` is read by *every* start path — `wyrd serve`, `wyrd
daemon start/restart`, and the MCP client's auto-start — so
`http://127.0.0.1:<port>/w/<worktree>/review/` survives restarts.
Without it the kernel assigns a fresh port each time. `--http` still
wins over the file; a pinned address that cannot be bound fails with
that diagnosis instead of silently falling back. The host half is
honoured as written, so a wildcard host really does bind every
interface — the loopback-only guarantee auto-start used to have applies
only to the unpinned default. `overlay.ServeHTTPAddr` owns the read;
`daemon start`/`restart` print the full review URL (wildcard hosts
normalized to loopback so the printed URL is openable).

**A daemon that cannot bind must exit.** Both wait loops in `Serve`
block until the context is cancelled, so a transport that failed on
startup used to leave a daemon running with no listener and no registry
record — invisible to `daemon list`, unreachable, and never stopped by
`restart` (which only stops what is registered). Harmless while every
daemon bound port 0; the moment ports are pinned, "address already in
use" is the normal collision and the orphans pile up. `Serve` now
selects on `httpErrCh` alongside the watcher / context wait.

**A detached daemon must get `/dev/null`, never `io.Discard`.**
`io.Discard` is not an `*os.File`, so `os/exec` hands the child a pipe
drained by the parent; a daemon outlives its parent by design, so the
first log line after the parent exits raises SIGPIPE on fd 2 and the Go
runtime kills it — the daemon registers, then vanishes, leaving a stale
record and a dead URL. This is why `wyrd daemon start` used to appear
to do nothing. `detachStdio` (`internal/serve/autostart.go`) is the one
place that wires it.

**A worktree created after the daemon started must find its way in.** The
registry record (`~/.wyrd/daemons/<repo-key>.json`) lists the worktrees that
existed when the listener bound, and the CLI picks which worktree to address
from that list — so a branch checked out since then matched nothing, and
`wyrd graph search` was answered from the primary checkout's model at
`status: ready` with no warning at all. (MCP was unaffected: the thin client
derives the name from cwd, and the HTTP dispatcher already re-discovers on a
miss.) Two changes: the daemon re-scans `git worktree list` every 30s
(`Options.WorktreePollInterval`), and any scan that changes the set —
periodic, miss-driven, or explicit — fires `MultiState`'s
`WorktreesChangedHook`, which republishes the record and starts parsing the
new checkout so it is warm before anyone asks. `daemonWorktreeName` also
addresses the worktree cwd is standing in whenever cwd belongs to the daemon's
repo, listed or not; a name that genuinely does not exist comes back as a
loud 404 instead of a quiet answer about `main`.

**A worktree that is not a module root is not served.** When the primary
checkout has a `go.mod` and a worktree does not — a checkout half-deleted by an
interrupted removal — go/packages climbs to the go.mod of the checkout the
worktree is nested in and reads the remains as that module's subdirectories:
`ready` with 0 packages, or a foreign model. Every scan leaves such a worktree
out of the set (logged once, when the skip starts), and it comes back on the
scan after its go.mod does.

**One model per repository, not per worktree.** A repo-level daemon used to
hold a full parsed model per worktree, another per materialized review base,
and a `.archai/cache/go-model.json` per worktree on disk — near-identical
copies, so memory grew with the worktree count. Now every State and every
review base reads one content-addressed store, `internal/serve/modelstore`
(design: `docs/features/shared-model/design.md`). Three concepts and no
fourth: a **Version** is one package's model addressed by the SHA-256 of its
JSON encoding (with `Layer`/`Aggregate` cleared); a **snapshot** maps a commit
rev to the Version of every package in its tree; a **branch view** is a State
whose model is a snapshot plus the packages its diff against that rev touches,
read from disk with the snapshot as `known`. A cold start tries the snapshot of
HEAD (a restart parses only uncommitted work), then of
merge-base(`main`, HEAD) (a branch parses only its diff), and otherwise parses
in full — no snapshot, a diff touching `go.mod`/`go.sum`, or a State built
without a store. A full parse of a worktree that is clean before and after it
is recorded as the snapshot of HEAD; nothing else records one, because a
branch view's untouched packages lack implementations of interfaces the branch
added, and that approximation must not become "the model of this commit".

- **Shared models are immutable.** Copying a `PackageModel` copies slice
  headers, so nothing may write through a slice of a model it did not
  allocate. Writing a top-level field of a copy is fine, which is why
  `overlay.Merge` keeps working on the consumer's own slice and why the
  overlay facts (`Layer`, `Aggregate`) are never stored in a Version — they
  would make one package two Versions in two worktrees. The canonical
  instance's top-level slices are clipped to their length so an `append`
  reallocates. `apply.applyPackage` used to keep a shallow copy of the live
  package and then edit it in place (reachable from MCP `apply_diff`); it
  clones now.
- **One instance is the point.** Every model entering a State goes through the
  store (`Store.Versions`), which swaps in the held instance of an identical
  Version. The in-memory set is the Versions of the 16 most recently used
  snapshots; a Version dropped from it stays alive in the States holding it
  but is no longer deduplicated against.
- **Disk:** `~/.wyrd/models/<repo key>/<stamp key>/{versions,snapshots}/`
  (`WYRD_HOME` honoured; the home is `~/.wyrd`, and a `~/.arch` left by the
  rename is used while it is the only one — move it once, with no daemon
  running). The stamp key hashes `golang.ModelVersion` with the
  shape fingerprint of `domain.PackageModel`, and opening the store removes
  every other stamp's directory: bumping the version or changing the shape
  discards the previous parser's models, and the first start after it parses
  in full. A rebuild that changes neither keeps them. 16 snapshots are kept
  (LRU by mtime); a Version file goes when no kept snapshot references it.
- Retrieval is shared the same way, by text rather than by package: see
  "One corpus per repository" below.

**A base tree only produces a snapshot.** `ReviewBase` keeps its two fast
paths (the base worktree clean on the merge base; this worktree *is* the merge
base), then reads the store's snapshot of the merge base, and only without one
does `basetree.go` `git archive` the commit under
`~/.wyrd/basetrees/<repo key>/<rev>/`, parse it, record the snapshot and
delete the tree. Its old eviction counted only the loads in the daemon's
memory, so every restart forgot what was on disk and trees piled up for
months (22 trees, 1.8 GB on one repository). Now the tree is removed by the load that made it,
and the first request sweeps whatever an earlier daemon left behind. The
sweep never removes a load still in flight in this process — removing a tree
under its parser left a husk, a directory with no source that `materialize`
used to take for a finished tree — nor anything younger than an hour, which
may be another process's load. A staged tree also gets a `.wyrd-basetree`
marker before its rename and only a marked directory counts.

**The model follows the disk by content stamps, not by events.** A State
keeps the (mtime, size) of every file its model was read from — non-test
`.go` files under the trees the Go tool walks, plus the root `go.mod` and
`go.sum` — taken *before* the read. `State.Sync` (`internal/serve/sync.go`)
walks the tree with stats only, reads the package directories whose files
differ over the current model (`readOver`, the rest as known), drops a
package whose source is gone, parses in full when `go.mod`/`go.sum` moved,
and returns the packages whose model moved. An unchanged tree costs one walk.
Load, Reload and ReloadOver record stamps the same way, and all three plus
Sync are serialized on `readMu`.

- **A watcher batch only triggers Sync.** It used to name the package from
  the event: `FindOwningPackage` resolved a deleted file from a filesystem
  that no longer had it and climbed to an ancestor, a directory removed
  wholesale arrived as one bare-directory event, and each needed its own
  repair (`PruneMissingPackages`). A package that outlived its source on a
  branch is drawn as a package **the branch added**, since the review base is
  read fresh from a commit. Sync compares the tree with the stamps, so which
  events arrived no longer decides what is read.
- **Attribute events are passed on.** macOS raises NOTE_ATTRIB on files
  `git status`/`git diff` merely inspect, and colima's `mountInotify`
  forwards every host edit as a `chmod` in the guest. The watcher used to
  hold its own stamp map to tell the two apart; now an inspection costs Sync
  a walk and moves nothing, and nothing is published.
- **A failed read leaves the model and the stamps as they were**, so the next
  Sync reads those packages again. A package the reader cannot model at all
  is what a full parse skips too, and it leaves the model.
- **Stamps are taken before the read.** A file edited while it is being read
  keeps its old stamp, so the next Sync reads it again rather than trusting a
  model that may predate the edit.
- **The MCP `check` tool syncs first** (`internal/adapter/mcp/check.go`, CLI
  `wyrd graph check`): Sync, publish what moved (the sync consumed the change
  the watcher would have broadcast), then `check.RunAll` over the synced
  snapshot with the worktree's own `wyrd.yaml`. It is the daemon's answer,
  not CI's: `wyrd-check` stays a standalone full parse and never asks a
  daemon. The incremental model misses one thing a full parse has — an
  `implements` edge from an untouched package to an interface a changed
  package added — and policy counts `implements` as a dependency.

**Refresh re-reads what the branch changed.** The review UI's `Refresh`
sends `Cache-Control: no-cache` on `/api/uigraph`, and the handler calls
`MultiState.ReloadWorktree`. On a branch that is the review base's model
(the merge base's snapshot) with every package the branch's
`git diff` touches — committed, uncommitted, untracked — read from disk over
it (`State.ReloadOver`), so it costs the branch's packages, not a compile of
the repository. A package the diff does not touch is by definition the
base's, which is also how a package the branch model lost comes back. On the
base worktree, with no base, or when the diff touches `go.mod`/`go.sum`, it
is a full `State.Reload`, never a stored snapshot. The base is never
re-read on a branch's behalf: a stale base is fixed by a Refresh on the base.
A change publishes one `model-changed`. Everything else called refresh
(`/api/refresh`, MCP `refresh`) only re-indexes the model already in memory.

Two things used to empty a branch model silently and keep it empty: the
single-package reload dropped a package on any read error, and the
per-worktree model cache, since replaced by the store, skipped a package its
manifest listed but its models did not hold.

**Stored models are keyed by the reader's version, not by the binary.**
`golang.ModelVersion` (`internal/adapter/golang/version.go`) is a hand-bumped
constant, the model's counterpart of `EmbedRecipeVersion`. The key used to be
the executable's mtime and size, so a rebuild that touched only the review UI
discarded every model and each daemon parsed its repository from scratch.
**Bump it whenever identical source would parse to a different model with the
struct shape unchanged** — an extraction rule, a stereotype heuristic, a span
or path convention, a go/packages upgrade that changes what is read. The
shape itself is fingerprinted and needs no bump. A change left unbumped is
served from the old reader's models and reads as a fix that did not take
effect; nothing detects it. Embeddings were always independent of the binary:
they are keyed by content hash.

**One corpus per repository, one view per worktree.**
(`docs/features/shared-retrieval/design.md`.) `retrieval.Corpus` holds both
text channels keyed by a node's **content hash** (`retrieval.ContentHash`):
the BM25 index (doc id = hash) and the dense `VectorStore` (vector id =
hash). It is opened once by `serve.OpenCorpus` in `cmd/wyrd` and handed to
every State through `serve.WithCorpus`, like the model store; a State without
one builds a private in-memory corpus that writes nothing. `retrieval.Service`
is a worktree's **view**: node id ↔ hash maps plus the retrieval graph.

- **A hash is tokenized once and embedded once for the repository.** Views
  acquire/release hashes (refcounted): the first acquire indexes the text, the
  last release removes the BM25 document. Vectors are never evicted.
- **One embedding pass at a time.** `Corpus.embed` holds the corpus's embed
  lock and re-checks `Has` under it, so two worktrees never embed one hash and
  the embedder sees one pass, not eleven. A view waiting behind another's pass
  reports `indexing`.
- **`Update` never embeds; `EmbedPending` does, in the background.**
  `State.Load` and `refreshRetrievalLocked` build/patch the view under the
  State lock and then run `EmbedPending` on a goroutine. Embedding under the
  State lock would block a worktree's model behind another worktree's cold
  pass. `EmbedPending` converges on the current view, so it is order-free.
- **Search filters before ranking.** Both channels take `keep = view has
  hash`; each scored hash expands to every node id of the view with that text.
  BM25 and the local store apply it before the top-k cut (exact); Qdrant after
  (oversampled ×8, min 100). Everything after — fusion, floor, diffusion — is
  unchanged and works on node ids.
- **Readiness is per view**: `embeddable`/`embedded` count the view's graph
  nodes whose hash the store `Has`. A cold worktree whose texts are all in the
  corpus is ready with zero embedder calls.
- **Clustering reads vectors in one batch**: `clustering.Vectors` is
  `Vectors(ctx, ids)`, implemented by `Service` (ids → hashes → one `Get`).
- **Persistence, per repository only**, under
  `~/.wyrd/embeddings/<repo-key>/` (`WYRD_HOME` honoured; repo key = the
  daemon-registry hash of the **main** worktree root):
  `<embedder>@r<N>.vec` — the local vector store, an append-only record file
  (a save appends what is new; v1 gob files are read and rewritten once; a
  truncated tail is dropped) — and `lexical@r<N>.bm25`, the BM25 documents
  some view holds, streamed record by record. Nothing is written per worktree:
  `vectors.json` and `bm25.json` are gone (old files are ignored, not read).
  Never `MarshalIndent` a corpus, and never encode a whole index into one
  buffer to save it.
- IDF is computed over every live version in the repository, not one tree — a
  small, accepted skew.

`r<N>` is `retrieval.EmbedRecipeVersion` — **bump it whenever the text fed to
the embedder changes** (`buildHeader`, `readSpanBody`, `EmbedTextBudget`,
`BuildChunks`, `MeanPoolVectors`, …), since the content hash covers the full
node text, not the chunked/pooled text actually embedded.
`retrieval.Recipe(embedderID)` = `<embedder>@r<N>` names the local file and
tags every Qdrant point. `hashNodes` must stay byte-identical with
`ContentHash` (a test pins it). Watch either phase warm up with
`wyrd daemon status`.

**Vertex AI embedder** (`internal/adapter/embed/vertex`,
`WYRD_EMBED_PROVIDER=vertex`): Gemini embedding models over the
publisher-model `:predict` endpoint, authenticated with Application Default
Credentials — no key in the environment. Settings and defaults are in
`docs/retrieval.md`. Three things to keep:

- **Its id carries the model, the dimensionality and both task types**
  (`vertex:gemini-embedding-001/768/RETRIEVAL_DOCUMENT+CODE_RETRIEVAL_QUERY`).
  Every other adapter's id is `provider:model` because the dimension follows
  from the model; here it is a setting, and so are the task types, and each
  changes what a vector means. The project and location are deliberately
  absent: they say who is billed.
- **One text per request** is the model's limit, not a choice, so a batch is
  that many requests behind a concurrency bound.
- **`Dim()` is known before the first embed**, so opening Qdrant never has to
  probe this embedder.

Tests drive an httptest fake of `:predict` with an injected token source —
never the live endpoint, never real credentials.

**Qdrant (opt-in) holds both text channels**
(`internal/adapter/vindex/qdrant`, plain REST): the dense vectors (`Store`)
and the BM25 documents as sparse vectors (`Lexical`). Daemon configuration:

| Variable | Meaning |
|---|---|
| `WYRD_QDRANT_URL` | Qdrant base URL; falls back to `QDRANT_URL` |
| `WYRD_QDRANT_API_KEY` | JWT sent as the `api-key` header; falls back to `QDRANT_API_KEY` |
| `WYRD_QDRANT_COLLECTION` | collection name; **no fallback** |
| `WYRD_QDRANT_ADMIN_API_KEY` | token `wyrd qdrant …` uses instead, when set |
| `QDRANT_URL`, `QDRANT_API_KEY` | the service runner's generic connection |
| `WYRD_REQUIRE_REMOTE` | `1`: a backend that cannot be used fails the start (below) |

Selected only when a URL **and** `WYRD_QDRANT_COLLECTION` are set, so a
process that merely has the tenant connection in its environment keeps the
local files. The token cannot create a collection, so the daemon only verifies
it; `wyrd qdrant bootstrap` creates one (idempotent), `check` verifies,
`migrate --from <legacy>` copies an old one. Tests use an httptest fake of the
REST subset — never a live Qdrant. Supported from Qdrant 1.10 (sparse vectors
with the IDF modifier, the `/points/query` API); verified against 1.19.

- **Schema:** a named dense vector `dense` (`{size: <embedder dim>, distance:
  Cosine}`) and a named sparse vector `bm25` with `modifier: idf`, keyword
  payload indexes on `repo` and `recipe`. A **legacy** collection — a single
  unnamed vector, as they were created before the sparse channel — still
  works as the dense channel alone (`Schema.Legacy`), with the lexical index
  in its local file; the daemon says so at every start.
- **The two channels keep separate points.** A text is tokenized before it is
  embedded, the lexical recipe does not depend on the embedder, and an upsert
  replaces a point whole — sharing one point would make each channel's write a
  read-modify-write of the other's. Point id = UUID v5 of (recipe, content
  hash); payload `repo`, `recipe`, `hash`; every query filters on repo +
  recipe. Recipes: `<embedder>@r<N>` for dense, `lexical@r<N>/w<M>` for
  lexical.
- **Dense:** on open it scrolls the repo+recipe hashes into memory (`Has`); no
  vector is held in RAM and no `.vec` file is written. **Seeding:** when the
  local `.vec` file for the current recipe exists, the hashes the collection
  lacks are upserted from it in batches at start, nothing re-embedded, one log
  line with the count; the file is left for an operator to delete.
- **Lexical: wyrd tokenizes and weights, Qdrant applies IDF.** A term's
  dimension is a hash of the token `bm25.Tokenize` produced — the tokenizer of
  the local index, so camelCase, snake_case and qualified names split the same
  way wherever the index lives. A document's value is BM25's saturated,
  length-normalized term frequency; a query's is the term's count; the score
  Qdrant returns is then the BM25 score, on the scale `LexTemp` was tuned for.
  Qdrant's own `qdrant/bm25` inference tokenizes for natural language and was
  not used. Measured on a live pair of daemons, one per backend, the top five
  seeds were identical on nine identifier and natural-language queries.
- **`AvgDocLen` is a constant (80).** The local index normalizes length by the
  live average of its documents; a stored document is weighted once, when it
  is written, so remotely the average cannot move. Real corpora measured 63–89
  tokens per symbol. `weightsVersion` in `lexical.go` is part of the lexical
  recipe: **bump it whenever `bm25.Tokenize`, `DefaultK1`, `DefaultB`,
  `AvgDocLen` or the token hash changes**, or old and new documents are ranked
  against each other with different weights.
- **IDF is collection-wide.** Qdrant computes it over the collection, not over
  the repo+recipe slice a query filters to, so several repositories in one
  collection share term statistics. One repository per collection keeps it
  exact.
- **Writes are buffered.** `Upsert`/`Remove` are sync and error-free by the
  port's shape, so `Lexical` batches them (256) and sends what is pending
  before every `Search`, on `Flush` (the start of every embedding pass) and on
  `Retain`. A failed send stays buffered and is returned by the next call that
  can return an error.
- **`Retain` is the remote index's garbage collection.** A local index is
  saved as "the documents some view holds"; a collection has to be told, so
  `Corpus.Save` calls `Retain` with the held set — **under `c.mu`**, because a
  hash acquired while documents are being dropped would find its document
  present, skip the upsert, and then lose it. Dense vectors are still never
  evicted.
- **Both remote searches filter after ranking** (oversampled ×8, min 100):
  Qdrant filters on repo + recipe, the view's membership test is applied to
  what comes back.

**A backend that fails is a fault, not an empty answer.**
`LexicalIndex.Search` takes a context and returns an error, and `Corpus`
records the last failure of each channel (`embedder`, `vectors`, `lexical`)
until that channel next succeeds (`Corpus.Faults`). A search still answers
from the channel that works; with the lexical index down and no dense answer
it returns the error, since an empty result would read as "no such code".
`GET /healthz` is liveness and never looks at faults; `GET /readyz` lists
them, and each probe **re-asks the backends at fault** (`Corpus.Recheck`) —
a daemon the probe took out of rotation gets no searches, so nothing else
would ever clear the fault.

**`WYRD_REQUIRE_REMOTE=1` turns every fallback into a failure.** Without it,
an embedder that cannot be built, a Qdrant that is not configured or does not
answer, a wrong dimension and a legacy collection each degrade one channel to
something local with a log line — right for a workstation. With it the same
conditions fail the start (`serve.OpenCorpus` returns the reason, `wyrd
serve` exits), the noop embedder is refused, and `/readyz` answers 503 while
any channel is at fault. `buildEmbedder` returns the stand-in *and* the
reason, so the one place that decides is `OpenCorpus`.

The MCP thin client pings `POST /w/<worktree>/api/warm` as soon as it attaches,
so that cold parse starts at client startup rather than on the agent's first
tool call. It warms the model only; nothing else is precomputed. The daemon separately warms only its *default* worktree at startup.

### MCP over HTTP (`/mcp`)

The daemon speaks MCP itself, not only through the stdio thin client:
`/mcp` on a single-worktree daemon, `/w/<name>/mcp` on a repo-level one, and
`wyrd where` prints the endpoint for the worktree you are in. It exists
whenever HTTP does — no flag. This is the transport for a client that cannot
spawn a process (an agent in a container reaching the daemon on the host);
`.mcp.json` clients keep using stdio.

Stateless Streamable HTTP: one JSON-RPC message per POST, one back, `202` for a
notification, `405` on GET/DELETE (no SSE stream, no session id — the tools are
synchronous, so there is nothing to stream). Both transports route through
`mcp.HandleRequest`, so neither holds a method switch of its own, and
`protocolVersion` is negotiated rather than fixed (a client's revision is
echoed when we speak it, else `2025-06-18`).

Two guards, on `/mcp` only — the browser UI cannot send headers:
`WYRD_HTTP_TOKEN` (or `serve.auth_token`; the environment wins) demands
`Authorization: Bearer`, and an `Origin` that is neither this daemon's host nor
a loopback address is refused. The second is not optional paranoia: without it
any website can drive every tool on `127.0.0.1` from a visitor's browser.

The endpoint does not block on a cold parse — `initialize` and `tools/list`
need no model, and `tools/call` returns the same `{"status":"loading"}` record
the thin client gets — so an agent can attach while the worktree is still being
read.

**The compatibility test is the specification.**
`internal/adapter/http/mcp_sdk_compat_test.go` drives the official
`modelcontextprotocol/go-sdk` client (the one Go MCP client plugins use)
against a live listener. Hand-written checks only pin what we *believe* the
contract is; that one fails when the belief is wrong.

### Search is one tool

`search` is the only search: the text channels (dense embeddings + BM25) score
what the query matches, and that calibrated mass is the seed weight of a graph
diffusion (ACL push + min-conductance sweep cut) over calls/implements/uses/
returns edges. So one call answers both "where does this live" and "how does it
connect" — there is no `search_graph` any more, and no flat-list mode.

- The answer is one node list plus the edges among those nodes: seeds first
  (`seed: true`, in text-relevance order, carrying `text_score`), then what the
  diffusion reached (in `score`, the degree-normalized diffusion mass).
- **A seed is never dropped.** Neither `hops` nor the `MaxGraphNodes` payload
  cap applies to the query's own hits — the cap is spent on the region. A
  symbol the query named comes back even when it is isolated from everything
  the diffusion found.
- **`filters` constrain the seeds, not the region.** `kinds` /
  `package_prefix` say what the text may match; the community around a match is
  the wiring the caller asked the graph for, and filtering it would answer a
  different question.
- `conductance` is the quality signal (low = a real community, high = the best
  cut still runs through a hairball). `hops` is a hard radius on top of the
  sweep; keep it at 1–2.
- **Methods are nodes** (`{pkg}.{Recv}.{Method}`, kind `method`), so a method is
  findable by name and its body is in the corpus. It was not: a struct's span
  stops at its type declaration, its methods are separate declarations, and
  nothing else covered them — searching for a method by name matched nothing at
  all. Interface methods stay inside their interface, whose span already covers
  the signature and which has no body. A method's calls hang off the method, not
  the receiver; a `contains` edge ties the type to its methods and is
  deliberately absent from `EdgeKindWeights` (which `diffusionEdges` reads as
  "no edge"), or a type would inherit the relevance of everything its methods
  touch. Traversals over all edges — `expand`, an answer's induced edges — still
  walk it.
- One code path: `retrieval.Service.Search` + `SearchOptions`
  (`internal/retrieval/query.go`), the MCP `search` tool
  (`internal/adapter/mcp/tools.go`), and `POST /api/search`
  (`internal/adapter/http/retrieval.go`). Design:
  `docs/features/search-diffusion/design.md`, usage: `docs/retrieval.md`.

### Analysis lenses (MCP tools)

All take `{package, include_subpackages}` and run on the package subgraph.
(Exception: `status` takes no args — it reports daemon readiness, not a subgraph.)

- **`status`** — daemon readiness + indexing progress, cheap (no graph build).
  Renders a state word (`ready` = dense index available **and** not indexing /
  `indexing` / `lexical-only`), the package/node/embedding counts, the
  `embedder` id, and a message saying what to do next. Call this when an
  embedding-backed lens (`semantic_cluster`, `latent_domains`) comes back
  `{status:"indexing"}` or `{status:"loading"}` — it tells you whether to wait
  (dense pass still running) or that the embedder is misconfigured
  (lexical-only ⇒ those lenses won't run). Also exposed as
  `wyrd daemon status [name|pid]` over the same endpoint. **The same record as
  JSON is at `GET /api/mcp/status`** — the tool renders text because an agent
  reads it, and the review UI polls fields on a timer, so the two surfaces are
  separate (`mcp.StatusSnapshot`, `internal/adapter/http/api.go`).
- **`components`** — connected components over *all* edges. Finds shattered
  graphs / isolated symbols (missing edges). Singletons = something unlinked.
- **`file_hotspots`** — top-level declarations per file; flags structural
  overload (god-files) at `>= max(3× median, 20)`. Backed by `filestats`.
- **`trophic_layers`** — emergent layers from dependency *direction* (no policy
  needed). Solves a graph-Laplacian for a trophic height per node; reports
  `F0` incoherence ∈ [0,1] (~0 layered, >0.4 tangled), integer layers
  (0 = foundation, top = entry points), backward edges (inversions), and cycles.
- **`spectral_cluster`** — natural module clusters over *structural* dependency
  edges. Auto-K is **modularity-validated**: candidates come from the absolute
  eigengap `δ_k = λ_{k+1} − λ_k` (not the ratio, which is unstable near zero and
  biased K toward ~3), and the chosen K is the candidate whose partition
  maximizes Newman modularity `Q`. Response exposes `modularity`, the
  `eigenvalues` spectrum, and per-candidate `gap`/`modularity`.
- **`semantic_cluster`** — same spectral core and output as `spectral_cluster`,
  but the graph is a kNN graph over *embedding cosine similarity* instead of
  structural edges (clusters by what code is *about*, not how it's wired).
  Requires a configured embedder + indexed vectors (`refresh` first); reports
  `dropped_nodes` for symbols with no embedding.
- **`latent_domains`** — clusters the same node set *both* ways (structural +
  semantic) and compares the partitions to find domains fused by cross-cutting
  coupling. Verdict (`aligned` | `diverging` | `latent_domains_glued`) is driven
  by **AMI** (Adjusted Mutual Information — corrected for chance/K, so it does
  *not* drift as K grows, unlike raw NMI); `glued` also requires absolute
  structural degeneracy (dominant cluster ≥ 45%). Names the **glue**: the top
  structural fan-in nodes (shared helpers / a god-dispatcher) to pull to a thin
  boundary. Reports per-side `modularity` (structural Q < semantic Q ⇒ a blob
  hiding real domains). Requires embeddings (`refresh` first). This is the lens
  that surfaces, on its own, what otherwise needs eyeballing spectral vs
  semantic side by side.

### Tool results are text

Every read tool (`search`, `expand`, `get_node`, `get_package`, `extract`,
`list_packages`, `list_bounded_contexts`, `get_bounded_context`, `status`,
`refresh`, `embedding_coverage`, the lenses) renders compact indented text in
`internal/adapter/mcp/render.go`. The rules the renderers hold to:

- **The format is documented once**, in the tool description the client gets
  with `tools/list` — never re-explained in the body of a result. A legend on
  every response is a per-call tax on something the reader knows by the second
  call.
- **Name the home package once.** A header declares it; same-package symbols
  print bare, others as `<lastsegment>.<Name>`, which the node list resolves
  back into a `get_node` id.
- **Fold edges toward whichever endpoint carries the most of them.**
  `foldEdges` picks groups greedily: `src --kind--> a, b, c` where one source
  fans out, `dst <--kind-- a, b, c` where many converge. Folding by source
  alone left the converging case — twenty implementations of one interface,
  twenty identical lines — completely unfolded. `get_node` goes further and
  folds its incident edges by kind alone (`foldIncident`): the section header
  already names the node.
- **Depth is per-section, not global.** `search` prints signatures under
  `hits` and name/kind/location under `region`: the region is context, and a
  reader who wants a signature there calls `get_node`.
- **Drop empty columns, never pad them.** `symbolLine` omits an absent
  location rather than leaving the trailing spaces of the column; census lines
  omit zero counts (`countCols`), on `get_package`/`extract` as well as
  `list_packages`.
- **One kind vocabulary, and it is Go's.** The graph stores `class`/`iface`,
  inherited from the generic code model it is projected through; every surface
  a reader sees prints `struct`/`interface` via `retrieval.DisplayKind`, and
  `filters.kinds` takes either spelling via `retrieval.CanonicalKind`. Census
  labels use the same words `kinds` accepts (`interface`, not `iface`; `error`,
  not `err`). The rule is that whatever word an answer prints for a kind is a
  word a filter accepts — reading `executor  struct` and passing
  `--kind struct` used to be a silent zero. The JSON surfaces keep the stored
  kind: `/api/node/…` is parsed by the review UI, not read.
- **A set-valued argument rejects what it does not recognise, by name.**
  Nothing downstream can tell "filtered everything out" from "matched
  nothing", so an unrecognised `filters.kinds` / `edges` / `kinds` entry would
  reach the caller as an empty answer — "there is no such code" instead of
  "there is no such kind". `unknownValueError` (`tools.go`) names the value and
  lists the accepted ones, and the list it offers is one that then works. The
  check runs before the state and index checks, so a typo is reported as a typo
  even on a cold daemon.
- **Sort what a reader reads as a map.** `list_packages` and `extract`'s index
  sort by package path — the daemon holds packages in a map, and a few hundred
  lines in hash order are not a map of the repository.
- **A doc comment is cut at its first sentence, not at a width.**
  `internal/godoc.Synopsis` unwraps the leading paragraph and stops at the
  sentence end (with go/doc's initials rule), clipping on a word boundary only
  when no sentence end arrives first. Retrieval bounds the field on the way in
  and the renderer bounds it again on the way out; both call the same helper,
  so a doc is never cut twice by two different rules.
- **Programs get their own surface.** Anything a program parses — the review
  UI's readiness poll, the domains partition — has a JSON endpoint of its own
  (`GET /api/mcp/status`, `GET /api/archmotif/domains`). Do not reintroduce
  JSON into a tool result to serve a client; add the endpoint.

### Key concepts (hard-won)

- **Flow vs structural nodes.** `calls/usesType/returns/implements` are
  behavioral flow edges; `contains` is structural. `trophic_layers` runs on the
  flow projection and **excludes `field`/`file`/`package`** kinds (structural
  leaves with no flow edges — a field's type-coupling is recorded on its struct,
  not the field). `components` keeps them (it uses all edges incl. `contains`).
- **Trophic level ≠ DDD layer.** Trophic height is pure dependency-flow depth:
  sinks (depend on nothing) at the bottom, sources (entry points) on top. An
  aggregate like `domain.PackageModel` sits *above* its leaf value types because
  it depends on them — so "domain" is not a single bottom layer. The macro
  ordering (domain < serve < http < mcp) is still correct.
- **Layers are integer-rounded trophic levels**, not gap-cuts — gap-cutting does
  not scale to dense graphs (everything collapses into one band).
- **Structure vs semantics divergence = latent domains.** A package can be one
  structural hairball (low modularity Q, one dominant blob) yet split cleanly
  into balanced *semantic* domains. That divergence means real domains exist but
  are fused by a cross-cutting concern — typically shared transport/serialization
  helpers every handler calls (`errorResult`/`textResult`/`unmarshalArgs`). The
  glue shows up as the highest structural **fan-in** nodes; pulling them to a
  thin boundary (a generic `bind()` at the transport edge) dissolves the blob.
  `latent_domains` detects and names this automatically.
- **NMI inflates with K; use AMI for verdicts.** Raw normalized mutual
  information rises mechanically as the cluster count grows, so a fixed threshold
  flips a verdict between K values. Adjusted Mutual Information subtracts the
  agreement expected by chance (hypergeometric null), so it is K-stable.
- **Auto-K must validate by quality, not just the gap.** On a hairball there is
  no clean eigengap, so picking the largest gap returns a degenerate K. Cluster
  at each candidate and keep the K with the best modularity instead.
- **`components.Analyze` is not free at symbol granularity.** It solves an
  eigenvector centrality per component, and a codebase's symbols form one large
  component: a dense n×n matrix and a hundred power iterations. On 2,000 symbols
  that measured 0.5–1.2s, and the review report's islands section threw the
  centre away — it only wanted the singletons. That one call was most of what a
  repo-mode report cost. Ask the lens for a *centre*; count degrees yourself
  (`side.isolatedSymbols`) when the question is "what is unconnected".
- **Model store** (`internal/serve/modelstore`) is keyed on
  `golang.ModelVersion` plus the model's shape. A parser-logic change that
  does not bump it is masked by models the old reader stored. A worktree's old `.archai/cache/go-model.json` is no longer read and
  can be deleted.
- **`diff.Compute` must not touch the model it is handed.** Its normalizers
  drop the fields that are diff noise (spans, call edges) from a *copy* of each
  symbol — but a `Methods` slice shares its backing array with the caller, so
  writing normalized methods back in place stripped `Calls` and `Span` from the
  live daemon model. Every method's outgoing calls disappeared from the review
  graph, and with them every symbol's *incoming* wiring from a method: the
  wiring panel read `0 in` for a factory the code plainly calls. It reproduced
  only through the daemon, because `Project` alone is clean — the mutation
  happens in the `diff.Compute` the review handler runs first. Package-level
  functions were untouched, which is exactly why the graph looked half-right.
  `normalizeMethods` in `internal/diff/compute.go` owns the copy.
- **Readiness is gated, not blocked.** A cold daemon goes through two slow
  phases: *parsing* the model (`go/packages`) then building *dense embeddings*
  (Ollama). Neither blocks the MCP transport. `MultiState` backgrounds the parse
  and `/api/mcp/tools/call` answers a `{status:"loading",phase:"parsing"}`
  ToolResult immediately while it runs (`Loaded` vs the blocking `Get` in
  `internal/serve/multistate.go`); once parsed, `indexingGate` (`tools.go`)
  returns `{status:"indexing",embedded,embeddable}` from any embedding-backed
  lens until the dense pass finishes. So tools **return a meaningful readiness
  payload instead of timing out** — poll `status` and retry, don't treat the
  loading payload as a failure. The daemon warms the default worktree at startup
  so `status` shows progress right after a restart.
- **Verdict lenses sample, membership lenses dump.** `latent_domains` emits
  clusters on *both* the structural and semantic side, so it always returns a
  capped member **sample** (`clusterSummaries`, not the full-150-per-cluster
  `buildClusterInfos` that `spectral_cluster`/`semantic_cluster` use) — its
  product is the AMI verdict + glue + modularity, not membership. Full membership
  ⇒ call the single-sided lens. Without this the 2×K full dump blew past 70KB on
  a large region.
- **An argument that lifts that cap is a bug, not a feature.** `include_members`
  did exactly that, for the domains canvas, and on a real repository the reply
  came back as `{"error":"result_too_large","bytes":340464,"limit":262144}` after
  the better part of a minute of computing — `clampToolResult` enforces
  `maxResultBytes` on every `Dispatch` result, in the same package. It is gone. A
  client that needs both partitions in full reads `GET /api/archmotif/domains`,
  which is not the agent transport and has no such ceiling. Bounding it instead
  would have been worse: a partition silently missing members draws a grid that
  looks complete.

### Where the code lives

- Graph build: `internal/adapter/golang/reader.go` (parse) →
  `internal/adapter/archmotif/exporter.go` (→ archmotif graph).
- Analysis: `github.com/kgatilin/archmotif/pkg/{components,filestats,trophic,spectralcluster}`.
  `spectralcluster` owns the modularity-validated auto-K, the modularity metric,
  and the exposed spectrum.
- **`internal/clustering` owns what the clustering lenses share**, transport-free:
  node selection by selector (`SelectNodes`, `DiffRegionNodes`), the semantic kNN
  graph over embedding vectors (`SemanticGraph`, over a batch `Vectors` port
  so the retrieval service is injected rather than reached for), and the whole
  latent-domains analysis (`LatentDomains` — both clusterings, AMI/NMI agreement,
  glue detection, verdict). Two surfaces call it and neither holds a copy of the
  maths: the MCP tool and the review UI's domains endpoint.
- MCP handlers: `internal/adapter/mcp/tools.go` (`handle*`, registered in
  `builtinToolDefinitions` + `Dispatch`). `latent_domains` lives in its own
  `internal/adapter/mcp/latent_domains.go`, which is now argument decoding,
  readiness gating and the sampled text verdict — kept out of `tools.go`, which
  is itself the god-file these lenses flag.
- Readiness: `handleStatus` + `indexingGate` (`tools.go`) report/gate on
  `retrieval.Service.IndexStatus()` (`internal/retrieval/service.go`). The
  non-blocking parse path is `MultiState.Loaded`/`ensureLoad` (`multistate.go`)
  and the loading short-circuits for `/api/mcp/tools/call` and
  `/api/mcp/status` in `internal/adapter/http/multi.go`, which share one body
  (`loadingStatusBody`/`failedStatusBody`). CLI: `cmd/wyrd/daemon.go`.
- **Result rendering: `internal/adapter/mcp/render.go`.** Every read tool
  answers compact indented text, not JSON — see "Tool results are text" below.

### The review bar has two toggles

- **`Diff | Full`**, on a branch only, opening on `Diff`. `Diff` draws the
  changed packages narrowed to what changed in them; `Full` draws every
  package whole, with the diff marks kept. One event,
  `ReviewFullViewChanged`, sets both fields it stands for
  (`reviewImpactMode` `changed_only`/`repository` and `changedDetailsOnly`),
  so the two cannot be set apart.
- **`Public | Public + Internal`**: exported symbols (scope
  `all_public_api`) or everything (`everything`).

They replaced a `View` popover of six selects. Grouping, change kind and the
review view (the package set a `wyrd.yaml` slice names) still exist in the
store and in `selectReviewGraph` at their defaults, with no control that
moves them: `Diff` stays inside the default review view, `Full` ignores it,
and a view or grouping the old popover saved in `localStorage` is still
applied.

### File diff in the review UI

`Diff` in the app bar opens a magit/GitLab-style overlay: the changed-file
list sectioned by package on the left, the selected file's patch on the
right (both sides numbered, syntax highlighted, `j`/`k` to walk, `Esc` to
close). It answers "what changed in the files", next to the canvas that
answers "what changed architecturally".

- Data: `GET /w/<worktree>/api/gitdiff?base=main` →
  `internal/adapter/http/gitdiff.go` → `internal/adapter/git.Diff`.
- **The diff is three-dot and ends at the working tree**: it starts at
  `merge-base(base, HEAD)`, so a branch still reads correctly after the
  base moves ahead, and uncommitted agent work shows without a commit.
  Untracked, non-ignored files are appended as synthetic additions —
  a review that hides new files is a trap.
- Untracked files have no numstat, so their stats are derived from the
  patch. Both that count and the binary check are **line-anchored**: a
  file that merely quotes `Binary files ... differ` in its own text is
  not a binary blob (this bit the first version).
- UI: `web/src/domain/gitDiff.ts` (pure patch parser + grouping, tested),
  `web/src/components/DiffOverlay.tsx` (the overlay),
  `web/src/components/highlight.ts` (highlight.js, shared with the source
  drawer). No diff library: the grouped rail, the theme and the
  highlighter are ours anyway, so a component would only have brought a
  hunk renderer plus a second highlighter and a competing stylesheet.
- **A changed file container on a card opens its own patch.** The `±` button
  in a file panel's header (`hf-file-diff`) opens the overlay straight at
  that file. It appears on exactly the panels the review paints as changed
  (`showDiff && file.diff`) — `Full` and an `Ask` answer draw the untouched
  files of a changed package too, and those have no patch to open. The path
  is `sourceFilePath(componentId, file.path)`
  (`web/src/domain/sourcePath.ts`), the same package-id + `sourceFile`
  convention the source drawer uses, so the two cannot disagree about which
  file a card names. If a requested path is missing from the diff anyway
  (the graph and the working tree drifted apart), the overlay **says the file
  is unchanged instead of falling back to the first file in the list** —
  answering "open controller.go" with someone else's patch reads as that
  file's diff.
- **Next to it, `<>` just reads the file.** The same header carries a
  `hf-file-src` button that opens the source drawer at
  `sourceFilePath(componentId, file.path)` — the drawer `Tree.tsx` already
  opens, so a file has one viewer whichever surface names it. Unlike `±` it is
  on **every** file panel the card draws: a card reached by `Ask` or by
  browsing has no diff at all, and "search → look at the canvas → read the
  code" is the reason the button exists. Both live in a `hf-file-acts` group so
  the always-present one keeps its place when the patch button is absent.
- **The session is cached in the app, not in the overlay.** `useDiffSession`
  (exported from `DiffOverlay.tsx`, held by `AppContent`) owns the fetched
  diff plus the reviewer's place in it — selected file, folded sections,
  per-file scroll — so closing the overlay costs nothing and reopening is
  free. Nothing else caches: the endpoint has no ETag and the daemon
  recomputes the whole diff per request, so the client is the only cache
  there is. It is dropped when the worktree or base changes, on the
  `Reload` button, and on the same `model-changed` SSE that reloads the
  canvas — the file diff and the architecture diff must never end up
  describing different working trees.

### Symbol wiring in the review UI

Clicking a symbol on a card opens the wiring panel: **that symbol's
first-level relations only**, rendered as package blocks — incoming
(who depends on this) on the left, outgoing (what this depends on) on
the right, each side grouped into the package the neighbour lives in.
It replaced a BFS-over-the-whole-graph node/edge canvas that reliably
produced a hairball of crossing curves.

- **The declaration sits above both columns.** The panel is most often
  opened from a name in someone else's file, where "what uses this" means
  nothing until "what is this" is answered — so the block names the file and
  line the symbol is written at, its signature, its doc and its own source
  (`GET /w/<worktree>/api/node/<id>` → `retrieval.Service.Node`, the same
  node the MCP `get_node` tool returns, body read from the indexed span).
  Three consequences:
  - **The panel's anchor id *is* the retrieval node id**, so there is no
    lookup table: `{pkg}.{Symbol}` for a type or function,
    `{pkg}.{Recv}.{Method}` for a method. A field and an interface method
    are spans inside their type rather than nodes, so a 404 falls back to
    the declaring type and the block says whose declaration it is showing
    (`definitionLookupIds`, `web/src/domain/symbolDefinition.ts`).
  - **The signature survives folding**; the source is capped at 240px and
    scrolls, so a 300-line function never squeezes the wiring off the panel.
    Line numbers are the file's, starting at the declaration.
  - **`<> file:line` opens the whole file at that line** in the source
    drawer, accented and scrolled to. Opened from the file diff it closes
    the diff first — the drawer renders *under* that overlay, and the diff
    session is cached in the app, so `Diff` returns to the same patch at the
    same scroll.
- **Depth is walked, not drawn.** A neighbour row re-anchors the panel on
  itself and pushes onto a back stack (`<` in the header, `Esc` closes).
  One hop at a time stays readable where a transitive closure never does.
  Declarations read along the way are kept for the walk and dropped when the
  model changes — a reparse can move a declaration.
- **Cross-package is the finding.** Blocks whose package differs from the
  anchor's are accent-bordered, tagged, and sorted ahead of the anchor's
  own package; the header carries a cross-package count and a
  `cross-package only` filter that survives a walk.
- **Focusing a type rolls its members up.** Relations recorded on a
  method/field of the focused type count as the type's, with the member
  named in the row's `via`. Focusing a member scopes to that member alone.
  Wiring where both ends are inside the anchor is dropped — the card
  already shows it.
- Relations to endpoints outside the loaded graph are still listed (from
  the relation's own label/component), just dimmed and unwalkable. A
  cross-package edge must never vanish because its target package is out
  of view.
- Model: `web/src/domain/symbolNeighborhood.ts` (pure, tested) — it also
  synthesizes the method-level `implements` pairs the graph only records
  between struct and interface; `web/src/domain/symbolDefinition.ts` +
  `web/src/data/symbolDefinition.ts` for the declaration. View:
  `SymbolGraphOverlay.tsx`. Specs go through
  `testing/harness/symbol-wiring.harness.ts`, never raw selectors;
  `mountAppDom` takes a `node` responder, so the DOM specs answer without a
  daemon.
- **The file diff opens it too.** Every identifier in a patch that the
  graph can resolve is clickable (`hf-code-sym`, underlined on hover); the
  wiring panel opens over the diff, so "what uses this?" is answered
  without leaving the file. Resolution is nearest-scope — the file being
  read, then its package, then a graph-wide unique name — and a name that
  stays ambiguous is left unmarked rather than guessed at
  (`web/src/domain/codeSymbols.ts`, tested). Marking works on the
  highlighter's HTML string, never the DOM, so a line is still one
  `dangerouslySetInnerHTML`; the resolve-per-name result is cached per
  file. Only Go symbols exist in the graph, so a TypeScript patch marks
  nothing. While the panel is up it owns the keyboard — `Esc` dismisses
  it, not the diff, and `j`/`k` stop walking the file list behind it.

### Sequence diagrams in the review UI

An expanded card can flip to its call-sequence view: `GET /api/sequence` →
`internal/adapter/http/sequence_api.go` → `internal/adapter/mermaid` emits a
strict Mermaid `sequenceDiagram` subset (`participant pN as Label`, `pA->>pB:
label`) that `web/src/components/SequenceCanvas.tsx` parses and draws itself in
hf-* tokens. Lifelines are types (a method maps to its receiver); one column per
callee, ordered by first appearance in the DFS.

- **A sequence's width is the whole readability problem.** One uniform column
  gap sized to the longest label put `internal/adapter/mcp`'s `Dispatch` entry at
  68,000px inside a 620px card. Its 191 right-to-left messages *were* drawn — you
  just never saw one, because a backward arrow spans most of that width and both
  its ends sit off the viewport. Gaps are now solved per column pair (shortest
  span first, deficit split across the gaps the label must cross), so one long
  adjacent label no longer sets the scale for every other column.
- **The package prefix is dropped from lifeline headers.** It repeats on nearly
  every column, so what still carries one is exactly the cross-package lifeline —
  accent-bordered, same convention as the wiring panel. The prefix is read off
  the root participant (the entry point the diagram was built from).
- Backward messages carry their own colour + arrow marker, and past
  `LABEL_MID_MAX` the label rides next to the caller rather than at a midpoint
  thousands of pixels from either end.
- `SEQ_CARD_W/H` (`web/src/layout/layout.ts`) is a *fixed* frame — layout must
  not depend on async-fetched sequence content — so it is deliberately much
  larger than an expanded class card. The diagram scrolls inside it.
- Deep fan-out is a data shape, not a layout bug: `depth` defaults to 4 and
  package-level helper functions each become a lifeline, which is how one entry
  point reaches 165 columns. `&depth=<n>` on the endpoint is the lever.

### Ask in the review UI

`Ask` in the app bar (or the `ASK` tab in the left rail) puts a question to
the same retrieval index the MCP `search` tool queries, and answers it on
the canvas: the ranked hits list in the rail, and the packages those hits
live in drawn as cards narrowed to the matched symbols. It answers "where
is X handled" as architecture instead of as a file list.

- Data: `POST /w/<worktree>/api/search` → `internal/adapter/http/retrieval.go`
  → `retrieval.Service.Search` — hybrid dense (embeddings) + BM25, fused into
  a calibrated mass that then seeds the graph diffusion. No new endpoint; the
  review UI and the MCP tool share one.
- **The canvas draws the whole answer**, hits and region alike: the packages
  the query matched *and* the community the diffusion reached from them, cards
  narrowed to those symbols. The rail marks the difference — a row the graph
  added is tagged `related` and dimmed, and the meta line counts them
  separately (`2 hits · 1 related · 2 packages`) — because a neighbour read as
  a match is a wrong answer, while a neighbour read as context is the point.
- **Node ids are the join.** A retrieval `node_id` *is* uigraph's
  `Internal.id` (`{package}.{Symbol}`) and its package is the component id,
  so a hit is a card-row lookup with no translation. A hit also carries
  `package`/`name` explicitly: package paths contain dots, so a client
  splitting the id would be guessing. `web/src/domain/ask.ts` still
  keeps a longest-component-prefix fallback and resolves against the loaded
  graph, so a hit that cannot be drawn is *flagged*, never dropped.
  A method hit joins the same way against uigraph's `Member.id`
  (`{package}.{Receiver}.{Method}`), but the canvas draws internals, not
  members, so the resolved hit also carries `internalId` — the receiver's row —
  and that, not `nodeId`, is what the projection selects on. Selecting on the
  member id narrows a card to a row it does not have and draws it empty.
- **An ask replaces the review selection, not just filters it.** The `ask`
  option on `selectReviewGraph` overrides the review view's package
  allowlist *and* skips the diff projection: a question is asked of the
  repository, so a matched package draws whether or not the branch touched
  it. Diff badges on those cards survive.
- `askProjectionOf(ask)` is the one place the projection is derived —
  `AppContent` and the layout effect both call it, so the drawn canvas and
  the laid-out graph cannot disagree about what the answer is. Ask events
  are in `LAYOUT_TRIGGERS` for the same reason.
- An answer expands the packages it matched (a collapsed card would hide
  the symbols asked about); `Clear` restores the expansion the review had
  before the first ask (`AskState.expandedBefore`).
- **Depth is a count, never a score cutoff.** A calibrated mass orders one
  response and means nothing across queries, so the panel offers
  `Hits 10/20/50` and no threshold.
  The response's `dense` flag is reported as `semantic` vs `lexical only` —
  a recall difference worth saying out loud, not a failure.
- Model: `web/src/domain/ask.ts` (pure, tested). View:
  `components/AskPanel.tsx`. Effect: `effects/ask.ts` over a `SearchPort`
  (`data/search.ts` + `adapters/httpSearchSource.ts`). Specs go through
  `testing/harness/ask-panel.harness.ts`; `mountAppDom` takes a `search`
  responder so the DOM specs answer without a daemon.

### The architecture report in the review UI

`ArchMotif` in the app bar opens the review report: what this branch did to the
structure and where, or — with no base to compare against — what to refactor
next. Every row is a state the reviewer acts on, so it names the finding, what
to do about it, and where to click.

- Data: `GET /w/<worktree>/api/archmotif/report?base=main` →
  `internal/adapter/http/archmotif_report.go` → `internal/archreview.Build`.
  The base models come through the same `s.reviewBase` the uigraph handler
  uses, and the changed hunks through the same `internal/adapter/git.Diff` the
  file-diff handler uses, so the report, the canvas and the patch list describe
  one working tree against one base.
- **Two modes, two vocabularies.** A base plus a non-empty model diff gives
  `mode: "review"` (`group_cycles_new`, `edges_new`, `inversions_new`,
  `unused_exports_new`, `impact`, `hotspot_growth`, `orphans_new`); anything
  else gives `mode: "repo"` (`group_cycles`, `inversions`, `god_files`,
  `god_packages`, `islands`, `unused_exports`). The ids differ even where the
  measurement is the same, because "this branch added it" and "the repository
  has it" carry different actions.
- **A section whose state has not occurred renders as one line.** A clean
  branch reads as a handful of "none" lines. `totals` is a muted footer,
  `index` appears only while the embedding index is in the way, and `warnings`
  are rendered as `did not run: …` — a section that could not run must never
  read as a section that found nothing.
- **A row's gesture is read off the shape of its target.** `rowActions`
  (`web/src/domain/archReport.ts`, pure and tested) looks at `target`: edges
  accent them on the canvas, `internalId` opens the wiring panel, `file` opens
  it, a bare `componentId` focuses the package. A section the server grows
  later inherits the mapping without a client change. Two things the target
  shape cannot say come from elsewhere: the mode decides which way round a file
  row reads (review mode opens the patch, since the finding is that a *changed*
  file grew; repo mode has no base and opens the code — the other gesture stays
  on the row either way), and the god-package row also offers `Domains`, the
  jump into the domains canvas scoped to that package.
- Ids need no translation anywhere in the client: the server already writes
  `componentId` as a package path, `internalId` as `{package}.{Symbol}` and
  `file` as module-relative, which is what uigraph calls them.
- **An accent on an edge nothing draws answers nothing.** Highlighting is
  `ui.highlightedEdges` (package edges, both ends component ids), painted by
  `EdgeLayer` as `hf-edge-hot`. The review projection draws the changed
  packages, so a highlight row also focuses the package it names, which pulls
  that package and its edge neighbours into the projection. Clicking the row
  again puts the accent down, and so does a click on the empty canvas.
- **The report is built on demand, never warmed.** Building it costs an
  archmotif graph over the worktree's model and another over its base, plus the
  branch's git hunks. It used to be warmed in the background — on every
  worktree's load, on `/api/warm`, and again after every model change — which on
  a repository with a dozen worktrees ran that build for each of them on every
  edit, concurrently, for a panel that is rarely open. With the dense trophic
  solve archmotif had then, that was gigabytes per build and the daemon's whole
  memory problem. Now the panel's open is the only trigger:
  `internal/adapter/http/archmotif_report_cache.go` holds one built report per
  `(worktree, base ref)`, a request arriving mid-build waits for that build
  instead of starting a second, and the build runs under a background context
  so a reviewer who closes the panel still leaves the report for the next open.
- **Invalidation is by event, never by clock.** The entry is dropped (not rebuilt) on
  the same `plugin.ModelEvent` the SSE `model-changed` stream is published from
  — the subscription is made in `watchModel`, before the snapshot the build
  reads, so an event landing mid-build cannot be missed — and a request whose worktree or
  base differs is a miss that computes synchronously. Nothing is served because
  it is *probably* still current. Two things move with no model event behind
  them, and both are handled rather than tolerated: the embedding index, which
  is re-stamped onto the cached report on the way out (`Report.WithIndex`, so
  the rule about when to report it lives in one place); and a change the model
  does not see — an edited comment, a base branch that moved underneath — which
  is what the panel's `Refresh` is for. It sends `Cache-Control: no-cache` and
  the handler drops the entry first, so the button cannot hand back the answer
  the reviewer is trying to replace.
- **The session is also cached in the app, not in the panel**
  (`useArchReportSession`, exported from `ArchMotifPanel.tsx`): the response
  carries no ETag, so even a cached answer is a round trip and a re-render.
  It is re-read on `Refresh` and, while the panel is open, on the same
  `model-changed` SSE that reloads the canvas and the file diff — and only
  `Refresh` sends `no-cache`, because by the time that SSE arrives the daemon
  has already dropped its own copy.
- Harness `testing/harness/archmotif-panel.harness.ts`; `mountAppDom` takes a
  `report` responder, so the DOM specs answer without a daemon.
- What this replaced: a grid of package counts, a degree-sorted coupling table
  and an `Embed` button that shelled out to an external `archmotif` binary to
  write `.arch/archmotif/packages-vec.graphml`, which no lens read. None of the
  figures had an action attached; `layer 100%` was `1 − cycleEdges/edges` and
  therefore always 100%, since Go forbids package import cycles and a cycle
  lives one level up, on the group graph `internal/archreview` collapses to;
  and the whole grid was computed over the repository with no base, which is
  the one comparison a reviewer came for.

### Domains canvas in the review UI

`Domains` in the app bar — or a god-package row in the report — replaces the
review canvas with the repository's structural clusters laid against its
semantic ones: rows are structural clusters, columns semantic ones, ordered so
the best-matching pairs sit on the diagonal. One row smeared across many
columns is a structural blob hiding semantic domains, and the header names the
glue holding it together.

- Data: `GET /w/<worktree>/api/archmotif/domains?scope=diff|repo|package` →
  `internal/adapter/http/archmotif_domains.go` → `clustering.LatentDomains` —
  the same analysis the MCP `latent_domains` tool runs, so both partitions come
  out of a single solve. Asking for them separately would make the grid depend
  on the solver returning the same partition twice running.
- **The browser does not go through the agent's result budget.** `Dispatch`
  clamps every MCP tool result at `maxResultBytes` (256 KiB,
  `internal/adapter/mcp/result_budget.go`) to protect an agent's context window
  and the NATS bridge behind it. A full partition of a few thousand symbols is
  larger than that however it is encoded, so the canvas fetching it through
  `/api/mcp/tools/call` capped the whole feature at small repositories — it
  computed for the better part of a minute and then got `result_too_large`. A
  browser fetching the data for the page it is rendering is neither of those
  callers, so it reads its own endpoint and gets the partition whole.
- **The partition is a shared node list plus a label per node per side**:
  `{nodes: [id, …], structural: {k, modularity, dominant_share, labels: [int, …]},
  semantic: {…}}`, `-1` for a symbol a side did not place. A node id is written
  once whatever the number of sides, where a member-list-per-cluster shape wrote
  every id twice — measured on this repository at 46 bytes per node against 94.
  The client joins the two sides by index, not by id lookup
  (`web/src/domain/archMotifDomains.ts`).
- Readiness first, over the `LensPort`'s own `status` call (`data/lens.ts` →
  `GET /api/mcp/status`, not the tools surface — that one answers text now): it
  is cheap and says whether the dense pass is still running or no embedder is
  configured. The
  partition comes over a second port (`data/domains.ts` +
  `adapters/httpDomainsSource.ts`) because it is a second transport, not a
  second tool. The structural half is never drawn on its own — half a grid
  answers a different question than the one asked.
- Scope: the `diff` region, the whole `repo` (types and functions only), or one
  package, which is the entry the report's god-package row uses.
- **Repo scope drops methods and fields because the analysis is roughly cubic.**
  Measured warm on this repository: 187 nodes 0.2s, 438 (repo scope) 0.4s, 794
  (a diff region) 4.5s, 1,997 (every symbol kind) 44s. The wall is the two dense
  eigendecompositions inside `spectralcluster` — `spectral_cluster` alone over
  1,997 nodes takes 94s, because its auto-K search re-clusters at every
  candidate; `latent_domains` avoids that on the structural side by mirroring
  the K the semantic side settled on. The kNN similarity pass is ~2.4x cheaper
  than it was (unit-normalized vectors, each pair measured once, bounded top-k —
  benchmarked both ways in `internal/clustering`) but it is only about 6% of the
  time at that size. Cutting the rest means a sparse eigensolver in archmotif,
  not a tighter loop here.
- Cross-cell flow is drawn for the selected or hovered cell alone; all of it at
  once is the hairball the grid replaced.
- Model: `web/src/domain/archMotifDomains.ts` (pure, tested); view
  `components/ArchMotifCanvas.tsx`; harness
  `testing/harness/archmotif-canvas.harness.ts`. `mountAppDom` takes a `domains`
  responder, so the DOM specs answer without a daemon.

### Event Model

Declarative event-driven architecture declarations (`.arch/events.yaml`) with
validation and graph projection. See `docs/event-model.md` for the full usage
guide: format reference, validation rules, CLI/MCP surfaces, worked examples.

**A component declares three lists and nothing else** (`version: 2`):
`inputs` (what triggers it), `outputs` (what it appends), `state_events` (what
it folds into state without being triggered). The **fold is derived, never
declared**: read-set = the `pattern` of every input and state event, consumed
kinds = their `kind`, partition key = the ordered `{slot}` tokens they share.
There is no `folds:` block — one component holds one state, so the fold *is* the
component, and a separate block would only be a second place for the read-set to
be wrong. **Outputs are not in the read-set**: appending an event does not
subscribe you to it, and without that exclusion every producer would silently
subscribe to its own whole output surface.

**The model is event-sourced choreography, not RPC.** A durable event is
appended once and may be observed independently by any number of components.
Consequences baked into the rules:

- **`state_events` is why the self-loop rule can stay narrow.** A kind in both
  `inputs` and `outputs` is a component triggering itself
  (`self-input-conflict`, error). A kind in both `state_events` and `outputs` is
  the normal way to fold the outcome you just appended and is never a finding.
  Because the legal channel exists and costs one line, an inputs/outputs overlap
  is unambiguously a mistake rather than a legitimate pattern in the wrong
  section. Exact-kind match today, `(kind, pattern)` if a kind ever travels
  several routes.
- **`pattern` is the kind's address, and it lives on the slot** — next to the
  kind that owns it, which is the only place both sides can be checked against
  each other. One kind travels one subject globally; disagreement is
  `kind-pattern-conflict` (error), because the subscribers of one pattern will
  never see what a producer appends on the other — a broken wire, invisible at
  runtime until the event silently fails to arrive. Optional: a declaration that
  omits it is not a second answer. wyrd validates `{slot}` syntax and **never
  matches a pattern against a kind** — different alphabets.
- **`role: action | fact` is gone**, with `kind-role-conflict`. Direction says
  what role said about movement; the intent-vs-outcome reading belongs in
  `description` and in the kind name (`x.do` / `x.done`), not in a field every
  producer and observer has to agree on. `kind-pattern-conflict` replaces it and
  is the stronger rule: a role disagreement was a taxonomy argument, a pattern
  disagreement is a wiring bug.
- `owns` is authority over a namespace's **schemas**, not an exclusive right to
  append into it or observe it. Its only rule is uniqueness (`duplicate-owner`).
- Single-handler semantics are opt-in per slot via `delivery: exclusive`; only
  then do `exclusive-unhandled` / `exclusive-conflict` fire, and they count
  **inputs alone** — a state event drives no reaction, so folding an exclusive
  kind competes with nobody. The removed `unresolved-call` / `ambiguous-call` /
  `ownership-violation` rules were the RPC assumption and must not come back as
  defaults.
- **One component, one partition key**: every read-set pattern must extract the
  **same ordered** `{slot}` list, else `partition-mismatch`. Two deliberate
  exemptions — a pattern with *no* slots (a globally addressed event legitimately
  feeds a partitioned state) and an *output-only* pattern (not in the read-set).
- `state` is **optional**. It could be required when a `folds` entry existed only
  where there was something to project; with the fold derived, requiring it would
  force a schema onto every component that has any input. Declared-but-shapeless
  is still an `underspecified-state` warning.
- **Kind globs are gone with `consumes`.** `state_events` lists kinds exactly, so
  starvation is exact (`starved-input` / `starved-state-event`) instead of
  per-glob-entry, and the dot-segmented matcher had no caller left.
- `types` (not `vocab`) are reusable JSON Schema definitions, `$defs`-style,
  addressed as `#/types/X` or `other-component#/types/X`.
- **Graph: no fold node.** `component:<id>`, `kind:<name>`,
  `type:<comp>.<name>`; edges `output` (component→kind), `input` and
  `state-event` (kind→component), plus `defines`/`payload`/`refs`. The read-set,
  partition key and state shape are component attributes — a fold vertex would
  sit alone beside every component carrying exactly those three things.
- **`unwired` is health for a *pair*, and it exists because two rules answered
  from different keys.** Orphan and starved count declarations by kind *name*;
  a flow exists only when producer and observer agree on the *subject*
  (`BuildFlows` → `SubjectsMatch`). A kind appended by one component and taken
  as an input by another, on addresses that never match, therefore scored `ok`
  on both sides while the canvas drew nothing between them — and empty space
  looks exactly like a relation the model never had. `UnwiredKinds` names that
  state, `BuildFlows` draws the pair anyway (red, dotted), and `validateWiring`
  reports `unwired-kind`.
  - It is the **one rule reported over the whole composed set**, imported
    documents included. The three reasons AsyncAPI is excluded elsewhere — one
    kind on several subjects, wire coordinates instead of partition keys, an
    open rather than closed system — none apply: it compares two addresses in
    the same alphabet and says nothing until both ends are in the set.
  - Guards against the port family: a pair inside one component is the
    self-fold idiom and never fires, and a cross-component pair must exist by
    name before the addresses are compared at all.
- **`OwnerOf` must walk components in sorted order.** Two components may claim
  one namespace — a `duplicate-owner` error natively, ungraded when both are
  imported — and the map walk it used to do returned a different one of them
  per call. Nothing noticed until something compared two answers: the model
  diff below reported every kind in the tied namespace as `changed`, at random,
  on a repo where nothing had changed.

#### Diff on the event canvas

`GET /api/plugins/events/model?base=<ref>` answers the model **marked up**
against merge-base(base, HEAD) instead of narrowed to it. The review canvas's
base is passed straight through (`App.tsx` → `EventCanvas base=`), so the two
diagrams sharing that pane can never describe different comparisons.

- **The response is a union, not a delta.** Everything current, plus everything
  the base had and the branch does not, stamped `removed`. A view that drops the
  removed half answers "what is here now", which the unmarked view already did —
  and the whole ask was to keep seeing the map while seeing what moved on it.
- **The diff is computed over the wire shape** (`internal/plugins/events/diff.go`),
  not over `eventmodel.Model`. The reader's question is "what changed on this
  picture", and the picture *is* that shape: a mark appears exactly when
  something the canvas draws differs. A second comparison over the domain model
  would have to track every projection choice `buildModelView` makes, and the
  first time it drifted a component would paint unchanged while showing
  different contents.
- **The base is read a file at a time, not as a tree.** `git ls-tree` →
  `eventmodel.DeclarationCandidates` → `git show` per file → `ReadSources` over a
  scratch dir (`internal/plugins/events/base.go`). `serve.baseTrees` materializes
  a whole checkout because the Go reader needs one; an event model is a handful
  of YAML files, and exporting a large repo to read nine of them is the cost of a
  feature nobody would enable. `DeclarationCandidates` is deliberately a
  **superset** filter — a wrong include costs one `git show`, a wrong exclude
  silently drops a base component and reports it as added.
- **`rebaseSourceFiles` is not cosmetic.** Components read from the scratch dir
  carry its path, which changes per request, so without rewriting it back to the
  worktree every component differs from its own current self and a diff of
  nothing comes back as "everything changed".
- **Sources are the union of both sides' overlays.** Each alone is wrong in one
  direction: the branch's alone hides what a deleted source used to find, the
  base's alone makes everything under an added source look new. Hence
  `overlay.Decode`, which parses the overlay out of a git revision.
- **Slot matching is (kind, address) first, kind second.** A port family puts one
  kind on several addresses, so the pair is the identity; but an address moving
  is the thing a reader opens the panel for, and pair-only matching reports that
  as a removal beside an unrelated addition. Exact pairs are claimed first so an
  unmoved entry is never consumed as the "was" of a sibling that moved.
- **A kind's `was` carries a `schema_changed` flag, not the old schema.** The
  payload is the largest field a kind has; doubling it would cost more than every
  other field together, and a reader who wants the detail has the file diff,
  where a schema change reads as a patch instead of two JSON blobs.
- **A link is `removed` only when everything on it is gone** (`linkDiff`). One
  removed kind out of six leaves the pair standing; drawing it as removed would
  say two components stopped talking when they did not.
- **No marks at all when no base resolved.** An unresolvable ref returns the view
  untouched *and unlabelled* — the absent `base` field is how the canvas keeps
  "not compared" distinguishable from "nothing changed", and why a failed
  comparison never takes down the picture that works.
- Clicking a `+N ~N -N` chip dims everything that did not move. That is the
  "changes only" reading of the same picture, with no second view to keep in step
  and one click back to the whole map.

Codegen (`wyrd plugin events gen`) is **template-driven and language-neutral**:
templates live in the project (`.arch/templates/*.tmpl`), wyrd renders them
against a stable data model (`internal/adapter/eventmodel/gen.go`) and never
learns the project's types. There is no `--lang` flag and adding one would
invert the design — see design.md §4. Output names must contain `_gen.`;
generation is gated on validation; `.go` output goes through go/format
(syntactic only). Example template + data-model reference: `docs/event-model.md`
→ Codegen.

## CI gates and the two binaries

The repo ships **two** binaries from the same tree:

- `wyrd` — everything (graph, MCP, review UI, diagram rendering).
  ~47 MB, and `make build` must run the web build first because
  `web/dist_embed.go` embeds `web/dist`, which is not committed.
- `wyrd-check` (`cmd/wyrd-check`) — the architecture gates only:
  `overlay`, `policy`, `target`, `all`, `version`. 177 packages instead of
  433, ~8 MB (~6 MB stripped), and it builds **without npm** because the
  review UI embed is not in its dependency tree.

The size split is not feature trimming: `oss.terrastruct.com/d2`'s SVG
renderer alone — goja (a JS interpreter, for the dagre layout), embedded
fonts, chroma — measures ~30 MB of the full binary. Not linking it is the
whole optimization. Anything that imports `internal/adapter/d2/render.go`
inherits those 30 MB, so keep it out of the check path.

Both binaries call **`internal/check`**, which owns the gates *and their
report wording* with the model readers injected (`check.New(source,
specs)`). `wyrd overlay check` / `wyrd policy check` / `wyrd
validate` are thin wrappers over it, so the local gate and the CI gate
cannot drift apart. Add a new gate there, not in a `cmd/`.

Workflows: `.github/workflows/ci.yml` (arch-gate job runs
`go run ./cmd/wyrd-check all` with no Node; separate jobs build/test Go
and the review UI) and `.github/workflows/release.yml` (on `v*` tags:
cross-compiles both binaries for linux/darwin × amd64/arm64 with
`CGO_ENABLED=0 -trimpath -ldflags "-s -w -X main.Version=$TAG"`, tars them
with checksums, publishes via `gh release`). `make wyrd-check` runs the
same gate locally.

**Gotcha that made the gate a no-op for a long time:** the Go reader
records `SymbolRef.Package` **module-relative** (`internal/adapter`), while
`overlay.Merge` used to skip any dependency lacking the module prefix — so
it skipped every internal edge and always reported "no violations". Merge
now normalizes both shapes and decides module membership by
package→layer-map membership. When touching Merge, test with
module-relative dependency paths; fully-qualified fixtures pass either way.

## Container image and Helm chart

`Dockerfile` builds the daemon's image; `charts/wyrd` deploys it. Operator
documentation is `charts/wyrd/README.md` — this section is what to keep in
mind when changing either.

- **The runtime image is the golang image on purpose.** The reader shells out
  to `go list`, so the Go toolchain, git and CA certificates are run-time
  dependencies, not build ones. The image builds the review UI in its own
  Node stage, because `web/dist` is embedded and not in git.
- **Everything written goes under `/data`**: `WYRD_HOME=/data/wyrd`,
  `GOMODCACHE` and `GOCACHE` beside it, and the checkout at `/data/repo`. The
  chart mounts one volume there. None of it is the index — that is in Qdrant
  — so losing the volume costs a parse and a module download, not embeddings.
- **The chart does not check the repository out.** Which git host, which
  credentials and how often to fetch belong to the deployment, so the chart
  takes containers for it (`sync.initContainers`, `sync.containers`, both
  `tpl`-rendered) and wyrd follows the directory through its file watcher.
  The image sets `safe.directory '*'`, since a sync container usually writes
  the checkout as another user.
- **One replica, `Recreate`.** The model store, the checkout and the lexical
  documents' `Retain` each assume a single writer.
- **The collection is bootstrapped by an init container**, `wyrd qdrant
  bootstrap`, not by a Helm hook: a hook would have to order itself against
  the bundled Qdrant coming up, while an init container that fails is simply
  retried. It is idempotent, and it takes the admin token when one is given.
- **`requireRemote` defaults to true** and the chart refuses at render time
  what the daemon would refuse at start (`wyrd.validate` in `_helpers.tpl`):
  the vertex provider without a project, noop with `requireRemote`, no Qdrant
  URL with the bundled one off. `values.schema.json` refuses unknown keys.
- **Probes:** startup and liveness on `/healthz`, readiness on `/readyz`. The
  readiness timeout (12s) is above the daemon's recheck budget (10s).
- **The bundled Qdrant is pinned** in `Chart.yaml` to the version the adapter
  was verified against; `Chart.lock` is committed, the `.tgz` is not.
- **Checks:** `make chart-lint` lints and renders every values case under
  `charts/wyrd/ci/` (CI runs it); `make image` builds the image. A release
  tag publishes `ghcr.io/<owner>/wyrd:<tag>` and
  `oci://ghcr.io/<owner>/charts/wyrd`.
- **Verified by hand, once:** installed into k3s with the bundled Qdrant and
  Gemini on Vertex — collection created on first start, a small repository
  indexed and searched through UI, API and MCP, a pod restart re-embedding
  nothing. No test in the repository starts a cluster.
- Known noise: the daemon logs `write serve.json: … permission denied` when
  the checkout is not writable by its user; the legacy per-worktree record is
  not needed in a container.

## Development Rules

1. **No test-only production code** - Don't add functions/parameters, types, or exported wrappers solely for testing. If you need to expose internals for testing, the architecture is wrong. Solutions:
   - Test through the public interface (e.g., test `Writer.Write()` output, not internal builder methods)
   - Use internal tests (`package foo`) instead of external tests (`package foo_test`) when testing implementation details
   - Refactor to make the code naturally testable through its public API
2. **No unnecessary exports** - Don't export functions, types, or constants that are only used within the package. Exported symbols are public API and should have a reason to be public. If tests need access to internals, use internal tests (`package foo`) not external tests.
3. **Proper DI** - Services receive dependencies via constructor, not create them
4. **Domain models are data containers** - No behavior, no external dependencies
5. **Adapters depend on domain** - Never the reverse
6. **CLI does the wiring** - Assembles adapters and passes to service
7. **Open-source ready** - Nothing personal and nothing from any employer or client in code, tests, fixtures, docs, design notes, generated artifacts (`.arch/`, diagrams, snapshots) or commit messages: no names of private repositories, packages, hosts, tickets, people or tenants, and no paths from a home directory. Fixtures and examples use neutral invented names (`test.example/...`, `store`, `app`). A bug found on a private repository is reproduced on an invented fixture and described in those terms.

## Running Tests

```bash
go test ./...
```

## Building

```bash
make build          # full wyrd (runs the web build first)
make build-check    # wyrd-check only — no npm needed
```
