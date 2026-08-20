# Search as graph diffusion

Design for unifying archai's search signals — dense embeddings, BM25, and the
code graph with its spectral machinery — into one principled search operation.
The output of search is always a **subgraph** (rendered on the review canvas,
returned by the `search` MCP tool), so the goal is a query-adaptive, low-noise
subgraph with calibrated per-node scores, not a flat ranked list.

Status: §3.1–§3.4 are implemented and have replaced the RRF + BFS + PPR
pipeline described below — calibrated fusion, weighted seeds, edge-kind weights
and hub damping, and the push + sweep cut (with `hops` retained as a hard radius
bound on top of the sweep). The two entrypoints of §4 are now one: `search` and
`search_graph` have collapsed into a single `Service.Search` and a single MCP
tool, whose answer carries the seeds (marked, with their text mass) and the
community around them in one node list. Not yet built: the semantic edge mix
(λ, §3.2), the roll-up and edge-mass filtering of §3.5, and the remaining
parameterizations of §4 (A↔B connection mode, direction and edge masks).

---

## 1. What the current pipeline does, and its defects

Current flow (`internal/retrieval/query.go`):

1. `Search`: dense cosine top-K + BM25 top-K, fused with Reciprocal Rank
   Fusion (`1/(60+rank)`, equal channel weights).
2. `SearchGraph`: takes the top-k fused hits as seeds, gathers a candidate
   pool by BFS (cap 400 nodes), runs uniform-seed personalized PageRank over
   the pool, keeps a flat top-50.

Defects, each a direct source of canvas noise:

1. **Relevance is computed and then discarded.** PPR is seeded uniformly over
   the hits: the 10th weak hit contributes as much personalization mass as the
   top hit. The fused scores never reach the diffusion.
2. **The BFS candidate cap is arbitrary and order-dependent.** The frontier is
   sorted lexicographically, so the 400-node budget fills with alphabetically
   first neighbours. Deterministic, but meaningless.
3. **The final cut is a flat top-50.** Not adaptive to the query: a narrow
   question drowns in neighbours, a broad one gets truncated. Nothing
   guarantees the 50 nodes are connected.
4. **PPR loves hubs.** Shared low-level helpers with huge fan-in (the "glue"
   nodes `latent_domains` identifies) collect diffusion mass on *every* query.
   This is the dominant noise source.
5. **RRF discards score magnitudes.** Rank-only fusion is robust to
   incomparable scales but wastes calibration information that the diffusion
   stage needs (personalization mass must be proportional to relevance).

---

## 2. The unifying frame: relevance is a signal on the graph

The scientific umbrella is graph signal processing / label propagation.

The text channels produce a **signal** `y` on the nodes: `y_i` = calibrated
text relevance of node *i* (BM25 ⊕ dense, see §3.1). The graph supplies a
**smoothing operator**. The score field `f` we want is the solution of the
regularized problem (Zhou et al., *Learning with Local and Global
Consistency*, 2004):

```
f* = argmin_f   Σ_ij W_ij (f_i/√d_i − f_j/√d_j)²  +  μ Σ_i (f_i − y_i)²
                └── smoothness along edges ──┘        └── fidelity to text ──┘
```

Closed form:

```
f* = (1−α)(I − αS)⁻¹ y,     S = D^(−1/2) W D^(−1/2),   α = 1/(1+μ)
```

This is **personalized PageRank with the personalization vector set to y** —
not with uniform seeds. The current implementation is the degenerate case
(y = indicator of the top-k hits).

The spectral reading ties in the machinery already present in
`spectralcluster`. Expanding in the Laplacian eigenbasis:

```
(I − αS)⁻¹ = Σ_k  1/(1 − αλ_k) · u_k u_kᵀ
```

Diffusion is a **low-pass filter on the graph spectrum**: signal components
aligned with cluster modes (the same eigenvectors `spectral_cluster` uses to
cut modules) pass with gain up to `1/(1−α)`; high-frequency components
(relevance scattered across cluster boundaries) are damped. One sentence
unifies the three mechanisms: *BM25 + dense define the signal, the Laplacian
defines the filter, and the spectral clusters are the modes the filter
passes.* The search result is the smoothed signal, cut down to a coherent
community.

---

## 3. The operation, stage by stage

### 3.1 Signal y — calibrated fusion (replaces RRF)

Softmax with temperature per channel over the fetched candidates, then a
convex combination:

```
y = β · softmax(cos/T_d)  +  (1−β) · softmax(bm25/T_b)
```

- Exact identifier match gets a guaranteed mass floor (`ExactNameBoost`):
  a candidate whose symbol name equals a query token case-insensitively is
  raised to at least the floor, then y is renormalized to sum 1.
- With no embedder (or an empty vector index), y is pure `softmax(bm25)`;
  the `dense` response flag keeps its meaning.
- Rationale for temperatures: cosine similarities live in a narrow band
  (low `T_d` sharpens), BM25 scores have wide dynamic range (higher `T_b`
  flattens).

`Result.Score` in the flat `search` tool becomes this mass: values in (0,1]
that sum to ≈1 over the candidate pool. Still meaningful only for ordering
within one response — never a cross-query threshold.

### 3.2 Graph W — hybrid and weighted

- **Edge-kind weights**: `calls: 1.0, implements: 0.8, uses/returns: 0.6`
  (starting values; all knobs). `contains` does not participate in diffusion —
  it is used only for roll-up (§3.5). It is a real edge in the graph since
  methods became nodes: a type contains its methods, and giving that edge a
  weight would hand every type the relevance of everything its methods touch.
- **Hub damping**: `W_uv ← W_uv / (d_u·d_v)^τ` with the *pre-damping*
  weighted degrees, `τ ≈ 0.25–0.5`. The symmetric normalization in `S`
  already suppresses hub flow partially; τ is the extra knob aimed squarely
  at defect 4. The glue list from `latent_domains` is the ready-made
  diagnostic for tuning it.
- **Optional semantic edges**: `W = W_struct + λ·W_knn`, where `W_knn` is the
  embedding-cosine kNN graph that `buildSemanticKNNGraph` already constructs
  for `semantic_cluster`. Relevance then flows not only along wiring but
  along "aboutness" — parallel adapter implementations pull each other in
  even when structurally disconnected. `λ = 0` is a legitimate default for
  the canvas.

### 3.3 Diffusion — local push instead of BFS + full PPR

Replace "BFS to 400 candidates, then dense PPR on the pool" with the push
algorithm from local graph clustering (Andersen–Chung–Lang, 2006): an
approximation of PPR with residual threshold ε that expands *only where mass
actually flows*. The 400-node cap and its alphabetical fill disappear; the
support of the result is bounded by `O(1/(ε(1−α)))` and is independent of
repository size. After α (locality radius), ε is the second main knob: it
controls tail detail.

### 3.4 Cut — conductance sweep instead of top-50

Sort nodes by `f_i/d_i`, walk the prefixes, keep the prefix with minimum
**conductance** (fraction of edge mass crossing the boundary). This is the
standard local Cheeger sweep, and it delivers exactly what the canvas needs:

- the result size is **query-adaptive**: a narrow question yields a small
  dense community, a broad one a larger region;
- the result is a connected community around the seeds, not 50 disconnected
  points;
- conductance is a numeric quality measure of the cut: display it, use it as
  a "nothing coherent found" threshold, and tune against it.

A hard payload cap (`MaxGraphNodes`) remains as an MCP response-size guard,
applied by mass order after the sweep, with truncation reported.

### 3.5 Output — mass, not ranks

The field `f` has a property rank lists lack: it is a **measure, and it is
additive**. Roll-up to any granularity is principled: a package's score is
the sum of its members' masses. The canvas draws package blocks — sum; the
wiring panel draws symbols — use as-is. Edges are filtered by the same mass:
render weight `W_uv·(f_u+f_v)`, thin edges cut by threshold — noise leaves
both the node set and the line set.

---

## 4. Other question shapes are parameterizations, not features

- **"How is A connected to B"**: two diffusions, `f_A` from seeds A and `f_B`
  from seeds B; node rank = `f_A·f_B` (or min) — high exactly on the paths
  between them. This is the connection-subgraph problem (Faloutsos, McCurley,
  Tomkins, 2004) without a separate k-shortest-paths algorithm. A sweep cut
  over the product yields the connecting community.
- **"Who calls X"**: edge mask `{calls}`, incoming direction, small α (nearly
  no diffusion) — degenerates into the exact traversal, as it should.
- **Direction in general**: diffuse over `W`, `Wᵀ`, or `(W+Wᵀ)/2` —
  "what does this depend on" / "what depends on this" / neutral canvas view.

One function with a parameter struct `{y, edge_mask, direction, α, ε, τ, λ,
β}` serves both the canvas and the MCP `search` tool: tune it against what the
canvas shows, and the MCP tool inherits the same behavior — it is literally
one code path. (Built: `Service.Search` + `SearchOptions`; the direction and
edge-mask parameters are still to come.)

---

## 5. Tuning by eye

Few knobs, all interpretable:

| Knob | Meaning | Default |
|---|---|---|
| α (`DiffusionAlpha`) | diffusion radius (teleport constant) | 0.15 |
| ε (`DiffusionEpsilon`) | tail detail / support bound | 1e-4 |
| τ (`HubDamping`) | hub suppression | 0.25 |
| β (`DenseWeight`) | dense vs BM25 in y | 0.5 |
| `DenseTemp` / `LexTemp` | per-channel softmax temperatures | 0.05 / 2.0 |
| λ | semantic kNN edge mix | 0 (off) |
| `EdgeKindWeights` | per-kind diffusion weights | 1.0 / 0.8 / 0.6 / 0.6 |
| `MaxGraphNodes` | MCP payload cap | 50 |

Tuning loop: collect a dozen labeled pairs on the canvas (query → nodes that
*must* be on screen / nodes that must *not*), metrics = node-set
precision/recall + cut size distribution + conductance. Grid search over the
scalars. No training — the by-eye labels are the dataset.

---

## 6. Where the code lives

- **archmotif `pkg/localpartition`** — the algorithm core: ACL push + sweep
  cut. Extended for this design with weighted seeds (personalization mass ∝
  y), weighted edges (kind weights respected by the walk and the conductance),
  `HubDamping` in `Options`, and a representation-agnostic entrypoint
  `LocalPartitionWeighted(edges, seeds, opts)` so archai's symbol graph can
  be fed without constructing archmotif's internal typed graph. `Result`
  carries `Region` (the sweep set), `Weights` (degree-normalized mass for
  every reached node), `Conductance`, `SeedCount`.
- **archai `internal/retrieval`**:
  - `params.go` — the `Params` struct with every knob above.
  - `query.go` — calibrated fusion (§3.1) in `Search`; `SearchGraph` builds
    the weighted edge list from the retrieval graph (kind weights), seeds the
    diffusion with the calibrated masses, calls `LocalPartitionWeighted`,
    maps `Region`/`Weights` into the response subgraph. The
    `NeighborNodes`-cap + `rankByDiffusion` path is retired from search;
    `Expand` keeps its full-neighbourhood contract untouched.
- **Config**: knobs surfaced in `archai.yaml` so tuning does not require a
  rebuild.

Rollout order: (1) weighted seeds — one parameter, fixes the crudest defect
immediately; (2) edge weights + hub damping; (3) push + sweep cut replacing
BFS/top-50; (4) λ-semantics and A↔B connection mode — independent add-ons.

## References

- Zhou, Bousquet, Lal, Weston, Schölkopf. *Learning with Local and Global
  Consistency.* NIPS 2004. (Label propagation; the regularized objective and
  its PPR closed form.)
- Andersen, Chung, Lang. *Local Graph Partitioning using PageRank Vectors.*
  FOCS 2006. (Push approximation, sweep cut, local Cheeger guarantee.)
- Faloutsos, McCurley, Tomkins. *Fast Discovery of Connection Subgraphs.*
  KDD 2004. (The A↔B connection formulation.)
- Shuman, Narang, Frossard, Ortega, Vandergheynst. *The Emerging Field of
  Signal Processing on Graphs.* IEEE SPM 2013. (The filter reading.)
