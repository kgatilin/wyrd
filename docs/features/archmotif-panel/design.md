# ArchMotif panel and canvas — review metrics that lead to an action

## Problem

The `ArchMotif` overlay in the review UI (`web/src/components/ArchMotifPanel.tsx`,
served by `internal/adapter/http/archmotif_metrics.go`) is a grid of numbers
nobody acts on:

- It is computed on the **whole repository, with no base**. `buildArchMotifMetrics`
  takes only `snap.Packages`; the merge-base models that `handleUIGraphJSON`
  already resolves through `s.reviewBase` never reach it. So it cannot answer the
  one question a reviewer has — *did this branch make the structure worse, and
  where*.
- It is **package-level only, with its own math**. Fan-in/out, SCC cycles and
  component counts are re-implemented in `archmotif_metrics.go`, in parallel to
  `archmotif/pkg/{trophic,components,spectralcluster}` that the MCP lenses use.
  `layer 100%` is `1 − cycleEdges/edges`, not trophic incoherence — and since Go
  forbids package import cycles, it is always 100%.
- It is **dead text**. Rows are not clickable and nothing links to the canvas.
- The **Embeddings section is legacy**: `Embed` shells out to an external
  `archmotif embed` binary against Vertex AI and writes
  `.arch/archmotif/packages-vec.graphml`, which no lens reads. The daemon has its
  own retrieval index (Ollama) and the lenses use that.
- It is called ArchMotif but uses nothing from archmotif. The lenses do.

## Principle

**A section exists only for a state in which the reviewer does something.**
Every row is `(state → action → click target)`. A section whose state has not
occurred collapses to a single "ok" line; a clean branch yields five lines of
"none", not a grid of figures. Figures with no action attached are dropped:
package / edge / component totals (kept only as a muted footer), per-package
instability, the degree-sorted coupling table, the layer percentage, region
conductance.

## Review mode — "did this branch make it worse, where?"

Present when the worktree has a review base (merge-base models) and the model
diff is non-empty.

| Section | Actionable state | Reviewer's action | Click target |
|---|---|---|---|
| **New group cycles** | an SCC at *group* level exists in head and not in base, or an existing SCC gained a member | break it — the row names the **package edge, new in the branch, that closed the cycle** | highlight the cycle's edges on the canvas |
| **New cross-package edges** | any package edge `A→B` absent at base, tagged `policy` / `backward` / `ok` | `policy` — fix or accept explicitly; `backward` (against the package-level trophic direction) — invert through an interface in the lower layer; `ok` — confirm it was intended | select the edge; for `backward`, wiring panel on the source symbol |
| **New inversions** | symbol-level backward edges present in head and not in base; the trophic `F0` is the *heading* of this list, never a standalone figure | each is a candidate for a port / callback | wiring panel |
| **New unused exports** | an *added* exported symbol in an `internal/...` package with zero fan-in from other packages and not declared an external port (see below) | unexport (CLAUDE.md rule 2) | the symbol on its card |
| **Impact** | a symbol whose declared shape changed (model diff) **or** whose body lies in a changed hunk (git diff), with callers in packages the branch did not touch | look at those call sites / their tests; signature changes are compile-checked, behavioural ones are not | wiring panel, incoming, cross-package filter on |
| **Hotspot growth** | a changed file already at or past the god-file threshold gained declarations; a god-package gained degree | put the new declarations elsewhere | the file's patch (`±`) |
| **Orphans** | an added symbol with no incoming edge at all (same exclusions as unused exports) | delete or wire | the symbol on its card |

Dropped from an earlier draft: *broken public contract* (removed/changed
exported symbol with external callers) — the compiler catches it.

## Repo mode — "what to refactor next?"

Present when there is no base (e.g. on `main`) or the diff is empty.

| Section | State | Action | Click target |
|---|---|---|---|
| **Group cycles** | SCC of more than one group; each row names its **weakest edge** (fewest symbol-level dependencies) | cut at the weakest edge — the row names the symbol that holds it | highlight the cycle |
| **Inversions** | symbol-level backward edges, sorted by span, capped with "N more" | interface / port in the lower layer | wiring panel |
| **God files** | `file_hotspots`: declarations ≥ `max(3 × median, 20)` | split the file | open the file |
| **God packages** | degree ≥ mean + 1.5σ and ≥ 4; the row offers *Domains* — `latent_domains` on that package: "one structural blob, K semantic domains, glue = X, Y" | pull the glue to a thin boundary — this is the jump into the canvas | ArchMotif canvas scoped to the package |
| **Islands** | `components` > 1 at package level; symbol-level singletons | delete dead code or add the missing edge | card / symbol |
| **Unused exports** | exported symbol in `internal/...`, no fan-in from other packages, not an external port; tagged `dead` when it has no fan-in at all | unexport / delete | the symbol |
| **Embedding index** | shown **only** while it is in the way: `indexing N/M` → wait; `dense_available: false` → configure an embedder. Hidden when ready | — | — |

## Definitions

### Groups (cycles "at level 2+")

Go forbids import cycles, so package-level SCCs are always empty. Cycles are
computed on the package graph **collapsed to groups**:

1. the overlay's `review_groups` when configured (first matching group in
   lexical key order owns a package, as the review UI resolves them);
2. otherwise the directory prefix at depth 2 (`internal/adapter`, `internal/serve`,
   `cmd`, …) — the existing `packageGroup`.

An SCC of ≥ 2 groups is a group cycle. Its *weakest edge* is the group edge with
the fewest underlying symbol-level dependencies; the row names that edge and
the symbol(s) behind it. In review mode only SCCs that are new or grew are
shown.

### Backward and inversion

- *Backward* (tag on a new package edge): `trophic.Analyze` over the
  **package** graph (packages as nodes, dependency counts as weights); an edge
  from a lower to a higher trophic level.
- *Inversion* (section): `trophic.Analyze` over the **symbol** flow graph
  (`calls / usesType / returns / implements`, the same projection the
  `trophic_layers` lens uses), whole repository; backward edges keyed by
  `(from, to)` so review mode can subtract base from head.

### External ports, unused exports, orphans — no heuristics

Three rules, none of them guesses:

1. **Language visibility.** Only `internal/...` packages are checked. Exports of
   any other package are importable by the world and are ports by Go's own
   rule.
2. **Overlay declaration.** `archai.yaml` gains a `ports` section for exported
   symbols reached from outside the dependency graph — plugin hooks, reflection
   / registration, generated code:

   ```yaml
   ports:
     external:
       - "internal/plugins/..."          # package glob: every export is a port
       - "internal/adapter/mcp.Dispatch" # one symbol
       - "internal/serve.State.Snapshot" # one method
   ```

   Selectors: package glob (overlay glob syntax), `pkg.Symbol`, `pkg.Type.Member`.
3. **Graph evidence.** A method of a struct that `implements` an interface
   declaring that method is used through the interface; the `implements` edge
   is in the graph (the wiring panel already synthesises the method-level pairs
   from it, `symbolNeighborhood.ts`). The server does the same.

Caveat said out loud in the row, not hidden: **test files are not in the graph**
(`packages.Config` without `Tests`), so "0 callers" may mean "used only by
tests" — which under CLAUDE.md rule 1 is also a finding, just a different one.
Rows read `0 callers (tests not in graph)`.

### Hotspots

Thresholds already established by the lenses are kept: god-file at
`≥ max(3 × median, 20)` declarations (`archmotif/pkg/filestats`); god-package at
degree `≥ mean + 1.5σ` and `≥ 4`.

## Canvas — structural × semantic domains

`latent_domains` already computes what the user wants to *see*: the structural
partition, the semantic partition, their agreement (AMI) and the glue. Today it
is a text verdict. On the canvas it becomes a **contingency grid**:

- rows = structural clusters, columns = semantic clusters, ordered so that the
  best-matching pairs sit on the diagonal (greedy max-overlap assignment);
- a cell = the symbols in both clusters, drawn as a card: package headers,
  symbol rows with kind glyphs — the same card vocabulary as the review canvas;
  a symbol click opens the wiring panel;
- empty cells collapse; cell size follows member count with a minimum;
- **diagonal-heavy = `aligned`; one row smeared across many columns = a
  structural blob hiding semantic domains (`glued`)**; glue nodes (top fan-in)
  carry a fan-in badge in their cell and are listed in the header with the
  *pull to a thin boundary* note;
- edges: only flow edges that cross cell boundaries, aggregated per cell pair,
  weighted; drawn **for the selected / hovered cell only** — all at once is a
  hairball;
- header: verdict, AMI, `Q_struct` vs `Q_sem`, dominant share, dropped-node
  count, scope;
- **diff overlay**: symbols the review marks as changed are highlighted, and
  the header says how many structural × semantic cells the change lands in —
  one cell = a local change, many = the change cuts across domains.

### Scope

- In diff view (a base exists and the model diff is non-empty): the **diff
  region** — `selector.diff: true` (seed = changed symbols ∪ endpoints of
  changed edges, grown by ACL local partitioning; `internal/adapter/mcp/diff_region.go`).
- Otherwise: the **whole repository**, `selector.node_kinds: ["type", "fn"]`
  (methods and fields dropped — a few hundred nodes instead of ~4k, the
  semantic kNN side stays cheap).
- A scope switch in the header: `diff region | repo | package …`, the package
  entry is how a god-package row from the panel lands here.

### Readiness

`status` first. `dense_available: false` → the canvas says the semantic side
needs an embedder and offers nothing else. `indexing` → progress, polled every
few seconds; the structural side is not drawn alone — half a grid answers the
wrong question.

### Data path

`POST /w/<wt>/api/mcp/tools/call` with `{name, arguments}` already exists
(`internal/adapter/http/api.go`); the canvas calls `status` and
`latent_domains` through it. One server change: `latent_domains` gains
`include_members: true` returning **full membership on both sides** in one
response. It samples today because it is a verdict lens (the 2×K full dump blew
past 70 KB on a large region); the canvas needs the partition itself, and one
call keeps both sides consistent by construction — three calls
(`latent_domains` + `spectral_cluster` + `semantic_cluster`) would depend on the
solver returning the same partition three times. Payload is member ids only.

## Server design

### `internal/archreview` — the report

A transport-free package that owns the computation and the wording:

```go
type Input struct {
    Head     []domain.PackageModel
    Base     []domain.PackageModel   // nil → repo mode
    Overlay  *overlay.Config         // groups, ports, policy, layers
    Changed  map[string][]LineRange  // git diff hunks by file, for Impact (nil in repo mode)
    Index    IndexStatus             // from retrieval.Service.IndexStatus(); shown only when not ready
}

func Build(in Input) Report
```

`Report` (`schema: archai.archreview/v1`):

```
mode:     "review" | "repo"
base:     {ref, rev}                      (review only)
sections: [ {id, title, severity, state: "ok" | "flag", count, items: [...]} ]  ordered by severity
totals:   {packages, edges, components}   (muted footer)
index:    {ready, indexing, embedded, embeddable, denseAvailable}  (only when !ready)
```

Every item carries **click targets in uigraph id conventions** so the panel
maps rows to canvas actions without translation: `componentId` = package path,
`internalId` = `{package}.{Symbol}`, `memberId`, `edge: {from, to}` (package
ids), `file` = module-relative path. Section ids: review —
`group_cycles_new`, `edges_new`, `inversions_new`, `unused_exports_new`,
`impact`, `hotspot_growth`, `orphans_new`; repo — `group_cycles`, `inversions`,
`god_files`, `god_packages`, `islands`, `unused_exports`.

Analysis reuses archmotif: `archmotifAdapter` exporter → graph,
`trophic.Analyze`, `components`, `filestats`, `localpartition`; SCC on the
collapsed group graph is the one piece written here (or lifted from
`archmotif_metrics.go`, which is deleted). Policy tags come from
`internal/policy` against the overlay, the same evaluator the uigraph projection
uses. Nothing here is duplicated from `internal/adapter/mcp` — if a lens handler
holds logic the report needs, the logic moves down to a shared package and the
handler calls it.

### HTTP

- `GET /w/<wt>/api/archmotif/report?base=<ref>` → `Report`. Base models through
  `s.reviewBase`, exactly as `handleUIGraphJSON`; changed hunks through
  `internal/adapter/git.Diff`, exactly as `handleGitDiff`.
- `/api/archmotif/metrics` and `/api/archmotif/embed` are **removed**, with
  `resolveArchMotifBin`, `archMotifEmbeddingInfo`, the GraphML writer and the
  `.arch/archmotif` artifact dir. Tests move with the logic to `archreview`.

### Overlay

`overlay.Config.Ports{External []string}` + validation (selectors parse, globs
well-formed). `archai.yaml` of this repository gets the section, dogfooded.

## UI design

### Panel (`ArchMotifPanel.tsx`, rewritten)

- Fetches the report; renders sections in severity order; `state: "ok"`
  sections render as one line.
- Rows are buttons. Callbacks from `App`: focus a component (`ComponentSelected`
  + `ScrollToComponentRequested`, expanding it), open the wiring panel on a
  symbol (`setSymbolFocus`), highlight an edge or a cycle (new
  `ui.highlightedEdges`, drawn accented by `EdgeLayer`), open a file's patch
  (`openFileDiff`) or source (`openSourceFile`), open the canvas scoped to a
  package.
- `Refresh` re-fetches; the panel also re-fetches on the `model-changed` SSE the
  canvas reloads on — the report and the canvas must describe the same tree.
- No `Embed`, no embedding-file status; the index line appears only when the
  report carries `index`.

### Canvas mode

- `ui.archMotifCanvas: { open, scope: {kind: 'diff' | 'repo' | 'package', package?} }`,
  events `ArchMotifCanvasOpened {scope}`, `ArchMotifCanvasClosed`,
  `ArchMotifScopeChanged`.
- When open, `ArchMotifCanvas` replaces the review canvas (the left rail stays);
  `Esc` closes. Entry points: a `Domains` button in the app bar next to
  `ArchMotif`, the god-package row in the panel, and the scope switch.
- Pure model in `web/src/domain/archMotifDomains.ts` (grid assembly, diagonal
  ordering, cell edge aggregation, diff overlay counts) — tested. View in
  `components/ArchMotifCanvas.tsx`. Data via a `LensPort`
  (`data/lens.ts` + `adapters/httpLensSource.ts`) so DOM specs answer without a
  daemon. Harness: `testing/harness/archmotif-canvas.harness.ts`, specs never
  touch raw selectors.

## Iterations

**Iteration 1 — server report** (worktree `archmotif-panel`, branch
`archmotif-panel`): `internal/archreview` with both modes and every section
above; `overlay.Ports` + validation + `archai.yaml` dogfood;
`/api/archmotif/report`; remove the metrics/embed endpoints and the external
binary path; unit tests per section with module-relative fixtures (review and
repo mode, the group-collapse, the three port rules, the tests-not-in-graph
wording). `go test ./...`, `go vet`, `wyrd-check all` green. Commit.

**Iteration 2 — domains canvas** (worktree `archmotif-canvas`, branch
`archmotif-canvas`, from `main`, runs in parallel with 1 — disjoint files):
`latent_domains include_members`; `LensPort`; pure grid model + tests; the
canvas component, readiness states, scope switch, diff overlay, selected-cell
edges; app-bar entry; harness + DOM specs. `vitest`, `tsc --noEmit`,
`npm run build` green. Commit.

**Iteration 3 — panel rewrite and wiring** (after merging `archmotif-canvas`
into `archmotif-panel`): the panel over the report; clickable rows and the new
highlight state; god-package → canvas; `Embed` gone; harness
`archmotif-panel.harness.ts` + DOM specs with a report responder in
`mountAppDom`; `CLAUDE.md` section replacing the current panel description.
Commit.

**Close-out** (main session): merge to `main`, `make install`, `archai daemon
restart`, verify against the live daemon (report on a branch with changes, the
canvas in both scopes).

## Agents

Iterations 1 and 2 run concurrently as two agents on their own worktrees
(`.worktrees/archmotif-panel`, `.worktrees/archmotif-canvas`); iteration 3 runs
after both, on `archmotif-panel`. Each agent commits its own work before
reporting. Briefs are self-contained (cwd, branch, files, contract, checks).

## Out of scope

- Layer-level cycles: `layer_rules` admitting a two-way dependency is a config
  property, not a code metric; the policy engine owns layer rules.
- Symbol-level cycles inside a package (mutual recursion, self-referential
  types) — legal and usually intended; not reported.
- Any threshold the reviewer tunes in the UI. Thresholds are the lenses'.
