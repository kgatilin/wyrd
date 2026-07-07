# Dependency Policy — path-based allowed/forbidden dependencies

## Problem

We want to control which packages may depend on which, and to fail a check
(CI, MCP, the review UI) when the code violates that. Two existing shapes were
rejected:

1. **A per-package import allow-list** (e.g. go-arch-lint's
   `directories_import`): one entry per package listing every package it may
   import — `handlers: [service, model, …]`, `service: [model, event]` — plus an
   exception line and comment for every carve-out. On a real codebase this grows
   to hundreds of lines. It is *verbose* because Go imports are package-level and
   all-or-nothing: every "I only import B for one return type" needs its own
   explicit exception.

2. A pure **layer matrix**. archai already has one — `layer_rules` in
   `archai.yaml` — and `overlay.Merge` already flags cross-layer edges the
   matrix forbids. It is concise but (a) can't express exceptions without
   dropping to the verbose per-package form, and (b) only expresses *direct
   adjacency*, not *reachability* ("domain must never transitively reach an
   adapter").

The goal: a concise policy that reads as **paths in the graph**, expressive
enough to replace both, and stronger than either.

## Model

Two primitives, both concise.

### 1. Selectors — name a node set once, reference it everywhere

- `@name` — an overlay **layer** (later: a tag). Layers are already defined in
  `archai.yaml`'s `layers:`; the policy just references them by name.
- a **package glob** — `internal/plugins/*`, `agent`, `internal/tools/...` —
  using the existing overlay glob syntax (`pkg`, `pkg/*`, `pkg/...`).

The `@` sigil is load-bearing, not decoration: a project can have both a *layer*
named `application` and a *package* `application/…`. `@application` is the
layer; bare `application` is the package glob. The sigil goes on the layer (the
less frequent reference) so the verbose exception lines stay clean.

### 2. Path operators — a small closed set

Each rule is one line: `<selectors> OP <selectors> [via <selectors>] [(kinds)]`.

| Operator | Meaning | Substrate |
|----------|---------|-----------|
| `A -> B`        | A may depend **directly** on B (allow edge / carve-in) | adjacency membership |
| `A !-> B`       | A must **not** depend directly on B (wins over allow)  | adjacency membership |
| `A !~> B`       | A must **not reach** B transitively (no path A→…→B)     | reachability closure |
| `A ~> B via C`  | every path A→…→B must pass through C (chokepoint)       | reachability with C removed = ∅ |

Selectors on either side may be comma-lists (`agent -> event, hooks`). The
`(kinds)` qualifier constrains the rule to certain edge kinds — see
*Iterations*.

Everything compiles to operations over the package dependency graph — direct
rules are adjacency-matrix membership tests, reachability rules are transitive
closure. This is the archmotif "по матрице" approach, with a richer vocabulary
than a single grid. archmotif's `graphval` (already a dependency) provides the
matrix-algebra reachability primitives; iteration 1 uses a BFS for path
extraction so violations can show an example offending path.

### Semantics: total deny-by-default

`deny_by_default: true` (the default) means **an observed edge is a violation
unless some `allow` rule permits it — including within a layer.** This is the
key correction over the old `layer_rules` behavior, which treated same-layer
edges as always allowed. Intra-layer freedom is exactly the coupling
interface-first architecture forbids (implementations importing sibling
implementations instead of talking through public contracts), so it must be
denied by default too.

The reason total deny-by-default stays *concise* is selectors: `@application ->
@domain` is one line covering hundreds of package pairs. In a well-architected
codebase the legitimate allow edges are few, so **the length of the allow-list
is a health metric** — every extra line is a documented deviation from
dependency inversion.

`deny_by_default: false` flips to blacklist mode: only `forbid`/`reachability`
rules apply, everything else is allowed. Useful for incremental adoption on a
codebase not yet ready for a full allow-list.

### Precedence

For a given observed edge `(u → v)`:

1. If any `forbid` (`!->`) rule matches `(u, v)` → **violation** (explicit deny
   always wins).
2. Else if `deny_by_default` and no `allow` (`->`) rule matches `(u, v)` →
   **violation** (unlisted edge).
3. Else allowed.

Reachability rules (`!~>`, `~> via`) are evaluated separately over the
transitive closure and reported independently.

**Express "forbidden except X" with deny-by-default, not a broad forbid.**
Because a `forbid` always wins over an `allow` (step 1), you cannot carve a hole
in a broad `forbid` with a narrow `allow`. To say "no public cross-imports
except a few", simply *omit* the broad `@domain -> @domain` allow (deny-by-default
denies every unlisted edge) and list the few permitted edges as `allow` rules.
Reserve `forbid` for the opposite shape: an edge that a broad `allow` permits but
you want to hard-deny anyway (e.g. `@adapters -> @adapters` allowed, but one pair
banned).

### Components — the same-component (cohesion) allowance

A rule like "no implementation may import another implementation" over-reports if
taken package-literally: a cohesive component spans several packages (an `auth`
component is `internal/auth` + `internal/auth/oidc` + `internal/auth/oauth2password`),
and those legitimately import each other. The rule means "no *cross-component*
coupling", not "no cross-package".

`components:` declares the cohesion boundaries as package-tree root globs:

```yaml
policy:
  components:
    - "internal/*"            # each internal/<name> is a component…
    - "internal/plugins/*"    # …except plugins and tools, which are collections
    - "internal/tools/*"      # of separate components, so go one level deeper
    - "internal/services/*"
```

A package belongs to the **deepest** declared root that is its ancestor:
`internal/auth/oidc` → `internal/auth`; `internal/plugins/orders/client` →
`internal/plugins/orders` (not `internal/plugins`). An edge between two packages
in the **same** component is never a violation; edges that cross a component
boundary fall through to the allow/forbid rules. This distinguishes legitimate
cohesion (`internal/auth → internal/auth/oidc`, sibling
`internal/eventstorage/encoders → internal/eventstorage/types`) from real coupling
(`internal/plugins/orders → internal/plugins/billing` — different components), and
keeps a collection like `internal/plugins/*` internally isolated. A `forbid` still
wins over the same-component allowance.

## Example

Take a typical hexagonal Go service with four layers defined in `archai.yaml`:
`domain` (public contracts and value types), `app` (use-cases), `infra`
(persistence and external clients), and `adapters` (http, cli, …).

### Declared architecture — a per-package allow-list, concisely

This expresses the common "coarse layer matrix + contract purity" shape: the
layer matrix (intra-layer allowed for implementations) plus the rule that public
contract packages are stdlib-only except a handful of carve-outs. Contract
purity needs **no `forbid`**: under deny-by-default, omitting a broad
`@domain -> @domain` allow denies every contract cross-import, and the carve-outs
re-permit exactly the intended ones.

```yaml
policy:
  deny_by_default: true
  allow:
    # Coarse layer matrix (intra-layer allowed for implementations).
    - "@app      -> @domain, @infra, @app"
    - "@infra    -> @domain, @app, @infra"
    - "@adapters -> @domain, @app, @infra, @adapters"
    # Contract carve-outs (every other contract->contract edge is denied).
    - "order   -> money, event"
    - "payment -> money, event"
    - "session -> event"
    # The composition root is the only place that imports concretes.
    - "internal/app -> @app, @infra, @adapters"
    # Test code may import anything.
    - "test/... -> @domain, @app, @infra, @adapters, test/..."
```

A handful of lines replace a per-package allow-list that would run to hundreds.
A first run on a real codebase typically surfaces **overlay staleness** before
any real violation: packages added since the `layers:` globs were last edited
land in no layer, so their edges show as unlisted. Classifying them (adding the
new paths to the right layer) is the first, useful output.

### Strict dependency inversion, and why cohesion must be excluded

Tightening to "implementations depend only on `@domain` contracts; the
composition root wires concretes" replaces the matrix rows with
`@app -> @domain`, `@infra -> @domain`, `@adapters -> @domain`.

A *blanket* "impl → contracts only" over-reports, because a cohesive
multi-package component legitimately imports its own sub-tree (e.g.
`internal/orders` importing `internal/orders/store`). The genuine signal is
*cross-component* coupling — one implementation reaching into an unrelated one
instead of through a contract. In practice the blanket rule flags many edges of
which only a minority are real cross-component breaks. This motivates an
iteration-3 **"same-component" allowance** (a package may import its own path
sub-tree) so strict inversion reports only the cross-component coupling.

## Architecture

- **Schema** (`internal/overlay/config.go`): `Config.Policy PolicyConfig` holds
  the raw rule strings (`deny_by_default`, `allow`, `forbid`, `reachability`).
  `overlay` stays a pure schema/loader package with no dependency on the policy
  evaluator (no import cycle).
- **Engine** (`internal/policy`, new): parses the DSL, resolves selectors to
  package sets against `Config.Layers` + globs, evaluates rules over the package
  dependency graph, returns `[]Violation`. Depends on `domain`, `overlay`, and
  `archmotif/pkg/graphval`.
- **Surfaces**:
  - CLI `archai policy check [packages…]` — reads the Go model, applies
    `overlay.Merge` (layer annotation), runs the engine, prints violations,
    exits non-zero. For CI.
  - (iteration 2) MCP `policy` tool `{ok, violations}`, and feed the engine into
    the existing UI `PolicyViolations`.

### Convergence with `layer_rules`

Three places currently compute the old `layer_rules` violations:
`overlay.Merge`, `uigraph.buildPolicyViolations`, `http.buildLayerEdges`. The
policy engine is a **superset** (total deny-by-default + path operators +
selectors). The end state: `layer_rules` is deprecated sugar that desugars into
`allow` edges, and the policy engine is the single evaluator behind all
surfaces. To avoid destabilizing serve/UI, iteration 1 leaves those paths intact
and only activates the engine when a `policy:` block is present. If `policy:` is
absent, behavior is unchanged.

## Iterations

- **Iteration 1 (this):** schema + engine + `archai policy check` CLI.
  Package-level granularity. Operators `->`, `!->`, `!~>`, `~> via`. Total
  deny-by-default. The `(kinds)` qualifier parses but is ignored (config is
  forward-compatible). Unit-tested on synthetic models.
- **Iteration 2:** edge-kind qualifiers — `(type)` / `(calls)` / `(implements)`
  restrict a rule to those `domain.DependencyKind` edges, the feature that beats
  go-arch-lint (forbid behavioral coupling while permitting type reference). MCP
  `policy` tool. Wire the engine into the review UI.
- **Same-component allowance (implemented):** `components:` roots define cohesion
  boundaries so strict inversion reports cross-component coupling only, not
  intra-component cohesion (see *Components* above).
- **Iteration 3:** `tags:` for cross-cutting selectors; `no cycles in S`;
  converge the three legacy `layer_rules` paths onto the engine; desugar
  `layer_rules` → `allow`.

The CLI also carries a `-C, --chdir` flag (like `go -C`) so the checker can be
pointed at another module's root from a script or CI without a shell `cd`.

## Development rules honored

- No test-only exports: the engine's public surface (`Parse`, `Check`, the
  result types) is what the CLI and tests both use; internals are unexported and
  tested via internal tests.
- `overlay` does not depend on `policy` (policy depends on overlay) — no cycle.
- Selector glob matching mirrors `overlay`'s `matchGlob` semantics; it is
  re-implemented locally (small, stable) rather than exporting overlay internals.
