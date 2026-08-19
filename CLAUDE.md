# Archai

Architecture diagram generator for Go projects. Analyzes Go source code and generates D2 diagrams showing interfaces, structs, functions, and their relationships.

## Project Structure

```
archai/
├── cmd/archai/           # CLI entry point (Cobra)
│   └── main.go           # Wires dependencies, defines commands
│
├── cmd/archai-check/     # Validation-only CLI shipped to CI (see below)
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
archai diagram generate ./internal/...

# Public API only
archai diagram generate ./internal/... --pub

# Internal implementation only
archai diagram generate ./internal/... --internal
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

Beyond diagram generation, archai runs as an MCP server (`archai serve
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

**Dogfood the MCP tools — do NOT explore archai with raw `grep`/`Read` first.**
archai serves a live graph of its own source. To understand where something lives
or how it is wired, reach for the archai MCP tools *before* shell search:
`search`/`search_graph` (semantic + graph search for symbols/code), `get_node` /
`get_package` / `list_packages` (inspect a node or package surface), `expand`
(walk neighbors / callers / callees / implementers from a seed node). Use
`grep`/`Read` only to read an exact span once a graph tool has located it. The
graph tools return architecture (edges, callers, implementers) that grep cannot.
If a tool returns "daemon unreachable", start/refresh the daemon first rather than
silently falling back to grep.

### Daemon lifecycle (CLI)

The `.mcp.json` thin client auto-starts a repo-level daemon; manage it with the
`archai daemon` group (target = current repo by default, or `[name|pid]` —
numeric → PID, else repo basename):

- `archai daemon start` — start a repo-level (`--multi`) daemon for the repo
  containing cwd; idempotent. Resolves the repo root from any worktree.
- `archai daemon status [name|pid]` — readiness + indexing progress (the `status`
  tool over HTTP); the right thing to poll after a restart on a big repo.
- `archai daemon list` — live daemons (repo, worktrees, pid, url, caps, uptime).
- `archai daemon restart [name|pid]` / `stop [name|pid]` — zombie-aware (reads
  `ps` state, not just `kill -0`); restart always relaunches. Both can target a
  legacy per-worktree `serve.json` daemon, not just the global registry.

**Pin the port, or the URL is unbookmarkable.** `serve.http_addr` in
`archai.yaml` is read by *every* start path — `archai serve`, `archai
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
record and a dead URL. This is why `archai daemon start` used to appear
to do nothing. `detachStdio` (`internal/serve/autostart.go`) is the one
place that wires it.

Rebuilding the binary bumps the model-cache stamp ⇒ the next start re-parses
every package from scratch. Embeddings survive it: `vectors.json` is keyed by
node id + content hash, so only nodes whose text changed are re-embedded. A
**fresh worktree** still re-parses (its `.archai/cache/` is empty) but no
longer re-embeds: vectors are also cached repo-wide, content-addressed, at
`~/.arch/embeddings/<repo-key>/<embedder>@r<N>.vec` (`ARCHAI_HOME` honoured;
repo key = the daemon-registry hash of the **main** worktree root). One
`vecstore.Store` is shared by every worktree `State` in the daemon and
consulted *before* the embedder, so a new branch only embeds the nodes whose
text nothing has embedded yet. `Service.Load` pushes an existing
`vectors.json` into it, which is also the (zero-code) migration. `r<N>` is
`retrieval.EmbedRecipeVersion` — **bump it whenever the text fed to the
embedder changes** (`buildHeader`, `readSpanBody`, `EmbedTextBudget`,
`BuildChunks`, `MeanPoolVectors`, …), since the freshness hash covers the full
node text, not the chunked/pooled text actually embedded. Watch either phase
warm up with `archai daemon status`.

The MCP thin client pings `POST /w/<worktree>/api/warm` as soon as it attaches,
so that cold parse starts at client startup rather than on the agent's first
tool call. The daemon separately warms only its *default* worktree at startup.

### Analysis lenses (MCP tools)

All take `{package, include_subpackages}` and run on the package subgraph.
(Exception: `status` takes no args — it reports daemon readiness, not a subgraph.)

- **`status`** — daemon readiness + indexing progress, cheap (no graph build).
  Reports `ready` (dense index available **and** not indexing), `indexing`,
  `embedded`/`embeddable`/`pending` counts, `embedder` id, and a human `message`.
  Call this when an embedding-backed lens (`semantic_cluster`, `latent_domains`)
  comes back `{status:"indexing"}` or `{status:"loading"}` — it tells you whether
  to wait (dense pass still running) or that the embedder is misconfigured
  (`dense_available:false` ⇒ lexical-only, those lenses won't run). Also exposed
  as `archai daemon status [name|pid]` over the same endpoint.
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
- **Model cache** (`internal/serve/model_cache.go`) is keyed on the binary's
  build version + executable stamp. After `make install`, restart the daemon and
  `refresh`, or a parser-logic change is masked by a stale cache.
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
  capped member **sample** (`buildClusterSummaries`, not the full-150-per-cluster
  `buildClusterInfos` that `spectral_cluster`/`semantic_cluster` use) — its
  product is the AMI verdict + glue + modularity, not membership. Full membership
  ⇒ call the single-sided lens. Without this the 2×K full dump blew past 70KB on
  a large region.

### Where the code lives

- Graph build: `internal/adapter/golang/reader.go` (parse) →
  `internal/adapter/archmotif/exporter.go` (→ archmotif graph).
- Analysis: `github.com/kgatilin/archmotif/pkg/{components,filestats,trophic,spectralcluster}`.
  `spectralcluster` owns the modularity-validated auto-K, the modularity metric,
  and the exposed spectrum. `semantic_cluster` reuses it over a kNN graph built
  in `tools.go` (`buildSemanticKNNGraph`) from retrieval-service embedding vectors.
- MCP handlers: `internal/adapter/mcp/tools.go` (`handle*`, registered in
  `builtinToolDefinitions` + `Dispatch`). `latent_domains` lives in its own
  `internal/adapter/mcp/latent_domains.go` (NMI/AMI math + glue detection) — kept
  out of `tools.go`, which is itself the god-file these lenses flag.
- Readiness: `handleStatus` + `indexingGate` (`tools.go`) report/gate on
  `retrieval.Service.IndexStatus()` (`internal/retrieval/service.go`). The
  non-blocking parse path is `MultiState.Loaded`/`ensureLoad` (`multistate.go`)
  and the `/api/mcp/tools/call` loading short-circuit in
  `internal/adapter/http/multi.go`. CLI: `cmd/archai/daemon.go`.

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
  (`showDiff && file.diff`) — on `Details: Full package` a changed package
  also draws its untouched files, and those have no patch to open. The path
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

- **Depth is walked, not drawn.** A neighbour row re-anchors the panel on
  itself and pushes onto a back stack (`<` in the header, `Esc` closes).
  One hop at a time stays readable where a transitive closure never does.
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
  between struct and interface. View: `SymbolGraphOverlay.tsx`. Specs go
  through `testing/harness/symbol-wiring.harness.ts`, never raw selectors.
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
  → `retrieval.Service.Search` — hybrid dense (embeddings) + BM25, fused
  with RRF. No new endpoint; the review UI and the MCP tool share one.
- **Node ids are the join.** A retrieval `node_id` *is* uigraph's
  `Internal.id` (`{package}.{Symbol}`) and its package is the component id,
  so a hit is a card-row lookup with no translation. `retrieval.Result` also
  carries `package`/`name` explicitly: package paths contain dots, so a
  client splitting the id would be guessing. `web/src/domain/ask.ts` still
  keeps a longest-component-prefix fallback and resolves against the loaded
  graph, so a hit that cannot be drawn is *flagged*, never dropped.
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
- **Depth is a count, never a score cutoff.** Fused RRF scores carry no
  absolute relevance, so the panel offers `Hits 10/20/50` and no threshold.
  The response's `dense` flag is reported as `semantic` vs `lexical only` —
  a recall difference worth saying out loud, not a failure.
- Model: `web/src/domain/ask.ts` (pure, tested). View:
  `components/AskPanel.tsx`. Effect: `effects/ask.ts` over a `SearchPort`
  (`data/search.ts` + `adapters/httpSearchSource.ts`). Specs go through
  `testing/harness/ask-panel.harness.ts`; `mountAppDom` takes a `search`
  responder so the DOM specs answer without a daemon.

### Event Model

Declarative event-driven architecture declarations (`.arch/events.yaml`) with
validation and graph projection. See `docs/event-model.md` for the full usage
guide: format reference, validation rules, CLI/MCP surfaces, worked examples.

**The model is event-sourced choreography, not RPC.** A durable event is
appended once and may be observed independently by any number of components and
folds. Consequences baked into the rules:

- `role: action | fact` is *semantic classification only* — no cardinality —
  but it is **global to the kind**: every producer and observer must agree,
  payload variants never change it, and intent vs outcome means two kinds
  (`x.do` / `x.done`). Disagreement is a `kind-role-conflict` error.
- `owns` is authority over a namespace's **schemas**, not an exclusive right to
  emit into it or observe it. Its only rule is uniqueness (`duplicate-owner`).
- Single-handler semantics are opt-in per slot via `delivery: exclusive`; only
  then do `exclusive-unhandled` / `exclusive-conflict` fire. The removed
  `unresolved-call` / `ambiguous-call` / `ownership-violation` rules were the
  RPC assumption and must not come back as defaults.
- `receives`/`emits` are ports (in/out), so a component never `receives` its own
  emitted kind — that is a loop through its own boundary
  (`self-receive-conflict`). Folds are NOT ports and may freely consume the
  component's own kinds. Exact-kind match today, `(kind, route)` once the model
  is subject-aware.
- `folds[].subjects` is a list; every entry must extract the **same ordered**
  `{slot}` partition key (one fold instance = one state), else
  `partition-mismatch`. `state` is required.
- `types` (not `vocab`) are reusable JSON Schema definitions, `$defs`-style,
  addressed as `#/types/X` or `other-component#/types/X`.

Codegen (`archai plugin events gen`) is **template-driven and language-neutral**:
templates live in the project (`.arch/templates/*.tmpl`), archai renders them
against a stable data model (`internal/adapter/eventmodel/gen.go`) and never
learns the project's types. There is no `--lang` flag and adding one would
invert the design — see design.md §4. Output names must contain `_gen.`;
generation is gated on validation; `.go` output goes through go/format
(syntactic only). Example template + data-model reference: `docs/event-model.md`
→ Codegen.

## CI gates and the two binaries

The repo ships **two** binaries from the same tree:

- `archai` — everything (graph, MCP, review UI, diagram rendering).
  ~47 MB, and `make build` must run the web build first because
  `web/dist_embed.go` embeds `web/dist`, which is not committed.
- `archai-check` (`cmd/archai-check`) — the architecture gates only:
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
specs)`). `archai overlay check` / `archai policy check` / `archai
validate` are thin wrappers over it, so the local gate and the CI gate
cannot drift apart. Add a new gate there, not in a `cmd/`.

Workflows: `.github/workflows/ci.yml` (arch-gate job runs
`go run ./cmd/archai-check all` with no Node; separate jobs build/test Go
and the review UI) and `.github/workflows/release.yml` (on `v*` tags:
cross-compiles both binaries for linux/darwin × amd64/arm64 with
`CGO_ENABLED=0 -trimpath -ldflags "-s -w -X main.Version=$TAG"`, tars them
with checksums, publishes via `gh release`). `make archai-check` runs the
same gate locally.

**Gotcha that made the gate a no-op for a long time:** the Go reader
records `SymbolRef.Package` **module-relative** (`internal/adapter`), while
`overlay.Merge` used to skip any dependency lacking the module prefix — so
it skipped every internal edge and always reported "no violations". Merge
now normalizes both shapes and decides module membership by
package→layer-map membership. When touching Merge, test with
module-relative dependency paths; fully-qualified fixtures pass either way.

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

## Running Tests

```bash
go test ./...
```

## Building

```bash
make build          # full archai (runs the web build first)
make build-check    # archai-check only — no npm needed
```
