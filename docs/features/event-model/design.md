# Event Model — declarative event-driven architecture, visualized and validated

## Problem

Event-driven systems describe their architecture *implicitly*: what a component
subscribes to lives in a subscription pattern, what it emits lives in imperative
append calls scattered across rules, and the payload shape lives in a Go struct.
There is no machine-readable statement of a component's event interface, so:

- the **event graph** (who produces what, who consumes it, what dies unheard)
  cannot be drawn without reading every rule body;
- **payload evolution** is invisible — nothing says which field is deprecated,
  which shape is legacy-read-only, which producer still writes the old form;
- **composition errors** are found at runtime or never: a subscription with no
  producer, two components claiming the same namespace, a caller sending a
  payload the target does not accept.

Deriving this from code does not generalize: every project encodes its event
vocabulary differently (subject grammars, constructor helpers, envelope types),
and multi-language systems have no shared substrate at all.

## Goal

A **language-neutral declaration format** for event-driven components, plus the
archai machinery to make it useful:

1. **Visualize** — the event model as a first-class graph in archai's canvas:
   components, event kinds, production/consumption edges, payload schemas,
   deprecated fields, dead ends.
2. **Validate** — composition checks over the declared set, extensible with
   project-specific policy.
3. **Map to code** — the declaration is not documentation that rots beside the
   implementation; it *generates* the code that carries it, so drift is a build
   failure rather than a discovery.

The declarations are authored by agents, not by hand. Readability of the YAML is
a non-goal; *parseability, completeness and checkability* are the goals.

## Non-goals (v1)

- AsyncAPI / xRegistry / CloudEvents projections. The format is shaped so these
  map mechanically later, but nothing is built for them now.
- A persistent declaration registry, federated admission control, breaking-change
  diffing across deploys.
- Runtime conformance (observed log vs declared graph).
- Deriving declarations from code. Code is checked *against* the declaration,
  never the source of it.

---

## 1. The format

One file per component, `.arch/events.yaml`, in the component's directory —
consistent with archai's existing per-package `.arch/` convention.

The examples below use an invented order-processing system — `billing`, `ledger`,
`shipping` — with no relation to any particular codebase.

```yaml
version: 1
component: billing                # stable id, unique in the repo
owns: billing                     # namespace whose vocabulary this component defines
description: invoice lifecycle

extra:                            # opaque to archai, passed through to templates
  partition: account
  transport: queue

receives:
  - kind: billing.invoice.issue
    role: action                  # action | fact
    description: create an invoice for one account
    exposure: [public_api]        # free-form tags; projects decide what they mean
    schema:
      type: object
      required: [Account]
      properties:
        Account:  {type: string}
        Currency: {type: string}
        Lines:    {type: array, items: {$ref: '#/vocab/Line'}}

emits:
  - kind: billing.invoice.issued
    role: fact
    schema: {$ref: '#/vocab/Invoice'}
  - kind: ledger.entry.post       # role=action + kind outside `owns` ⇒ a call-out
    role: action

folds:
  - name: billing.open-invoices
    subject: svc.*.billing.{account}.invoice.>  # transport read-set with partition slots
    consumes: [billing.invoice.*]               # kinds the reducer folds (globs)
    state:
      type: object
      properties:
        Open: {type: array, items: {$ref: '#/vocab/Invoice'}}

vocab:                            # component-local shared shapes
  Invoice:
    type: object
    properties:
      Account: {type: string}
      Status:  {type: string, enum: [open, paid, void]}
      Lines:
        oneOf:
          - {type: array, items: {$ref: '#/vocab/Line'}}
          - {type: string, deprecated: true,
             description: legacy single-line form, read-only}
```

### Decisions baked into the shape

**`calls-out` is not a field.** It is derived: `calls(C) = {e ∈ emits(C) :
role == action}`, and the target is the component whose `receives` declares that
kind. Merging it into `emits` is what makes the role/ownership rule table below
total.

**`folds` are separate from `receives`.** `receives` is the *invocable* surface —
"how you call me" — predominantly `role: action`. It is what a future codegen step
projects into tool definitions. A fold is a projection maintained over event
*facts*; the reducer switches on the event kind and ignores everything else.

**`subject` vs `consumes` — two alphabets.** A fold declares two distinct things:

- **`subject`** is the transport read-set: a NATS-style pattern with `{slot}`
  tokens that declares the partition key layout ("one state per account") and
  wires subscriptions at runtime. archai validates `{slot}` syntax but does not
  match this against kinds — it is opaque to validation and carried through for
  codegen.

- **`consumes`** lists the event kinds the reducer actually folds, as kind globs.
  The existing `MatchPattern` applies; starvation is checked per entry.

The distinction matters: a subject pattern may deliver events the reducer ignores
(wrong kind in the namespace), and a consumed kind may be emitted onto subjects
the fold does not subscribe to. Conflating them (as a single `pattern` field did)
produces false starved-fold warnings whenever a project uses realistic NATS
subject patterns.

**Schemas are inline, in JSON Schema vocabulary, written as YAML.** JSON Schema
is a data model, not a syntax — schemas expressed in YAML are valid JSON Schema
after `yaml→json`. There is no second file and no external schema references by
default. `$ref: '#/vocab/X'` addresses the component's own vocab block;
`$ref: 'other-component#/vocab/X'` addresses another component's (which makes it
a declared cross-component type dependency, visible in the graph).

**The wire shape is authoritative, not the Go shape.** A payload that accepts a
legacy form declares `oneOf` with `deprecated: true`. This is information that
does not exist in the implementation type and cannot be recovered from it — the
custom unmarshaler that reads the legacy form is the *implementation* of the
declared `oneOf`, not an exception to it.

**`extra` is an opaque passthrough.** archai never interprets it. It carries
project-specific coordinates (subject-grammar tokens, scope kinds, transport
bindings) that the codegen template needs and the neutral model must not know
about. Validated only against an optional project-supplied schema fragment
(`.arch/events.extra.schema.yaml`).

**A component need not have a runtime.** `owns` + `emits`/`receives` with no
`folds` is legal: a pure vocabulary owner (a shared contract package) is a
first-class component.

**`owns` is a namespace prefix, resolved by longest prefix.** `owns: billing`
claims `billing.*`. Ownership is prefix-based rather than first-segment so that
a namespace can, in principle, be split across components without renaming its
kinds.

**Overlapping `owns` is an error today — including nesting.** Two components
declaring `billing` and `billing.invoice` would be unambiguous under
longest-prefix resolution, so this restriction is not a technical necessity; it
is a deliberate choice of direction. A permissive rule cannot be tightened later
without breaking every project that relied on it, while a strict one can be
relaxed at any time. The longest-prefix resolution stays in the implementation,
so allowing nesting later is a policy change rather than a rewrite. Until then,
split a namespace by giving each component a distinct prefix.

**`owns` may be omitted.** Such a component defines no vocabulary, and the rule
table below then permits it exactly two roles: emitting actions (calling other
components) and receiving facts (observing them). That is a real and useful
category — an edge or gateway component that orchestrates without owning any
event of its own — not a degenerate case.

### What archai must implement

- `internal/eventmodel/` — the domain: `Component`, `Slot{Kind, Role,
  Description, Exposure, Schema}`, `Fold{Name, Subject, PartitionArity, Consumes,
  State}`, `Vocab`, `Extra`. Pure data, no behavior, no dependencies (archai
  rule 4).
- A reader that discovers every `.arch/events.yaml` under the repo, parses it
  (reuse `internal/adapter/yaml`), validates it against the built-in meta-schema,
  and resolves `$ref`s into a composed `eventmodel.Model`.
- The meta-schema ships **inside the archai binary**. Projects never carry it.

---

## 2. Validation

### Built-in rules

The role × ownership matrix is total, and two of its four errors fall out of the
model rather than being invented:

| | kind ∈ `owns` | kind ∉ `owns` |
|---|---|---|
| **emit, role=fact** | ok (0..n consumers) | **error** — forging another namespace's fact |
| **emit, role=action** | ok — self-scheduling | ok — this is a call-out; must resolve to exactly one receiver |
| **receive, role=action** | ok — this is my inbound | **error** — accepting commands in another namespace |
| **receive, role=fact** | ok — self-observation | ok — normal subscription |

Plus:

- **single owner** — no two components declare overlapping `owns`.
- **closure** — every `receives` kind has ≥1 producer; every `consumes` entry in
  a fold matches ≥1 emitted kind (reported per entry, not per fold); every
  emitted fact has ≥1 consumer (receive or fold consumes match).
- **call resolution** — an emitted action resolves to exactly one receiving
  component (zero = starved, more than one = ambiguous).
- **vocab integrity** — every `$ref` resolves; cross-component refs do not cycle.
- **slot syntax** — `{slot}` tokens in fold subjects are balanced and non-empty
  (`malformed-slot` error otherwise). archai does not interpret the subject
  beyond this syntax check.
- **schema compatibility** — *deferred, not implemented.* A call-out's payload
  should be a structural subset of the target's inbound schema for that kind.
  Doing this properly means implementing JSON Schema subtyping (required
  properties, type widening, `oneOf` branches, nested objects) over the opaque
  schema representation, which is a feature in its own right. The restricted
  form worth building first: for object schemas only, every `required` property
  of the target must be present in the caller's payload with a compatible
  `type`, and anything the check cannot interpret is reported as *unknown*
  rather than passing silently. Until it exists, the model carries schemas but
  does not compare them.

**Severity is context-dependent.** Where the composed set is only known at
runtime (plugin-contributed components, config-expanded instances), a static run
sees a *superset* of declarations and cannot honestly call an unconsumed fact an
error. Therefore:

- `archai plugin events validate` (static, whole repo): ownership/schema/ref violations
  are **errors**; starvation and orphans are **warnings**.
- The same rules evaluated by the project at startup over the *actually composed*
  set: starvation becomes an **error**. archai supplies the rules; enforcement at
  startup is the project's code (see §4).

### Project-specific rules

Reuse the `dependency-policy` selector/operator design over the event graph
rather than inventing a second policy language. Selectors name component sets
(`@layer`, globs); operators run over event edges:

```
@edge     ->  billing.*             # may emit into this namespace
@domain   !-> @adapter              # domain components must not call adapters
shipping  ~>  ledger.* via @audit   # every path to ledger.* passes through audit
```

Lives in the existing `.arch/overlay.yaml` policy block, qualified to event
edges. Implementation reuses `internal/policy` with a different edge source.

---

## 3. Graph and visualization — the primary deliverable

### Projection

The composed model projects to a **bipartite graph**:

- nodes: `component:<id>`, `kind:<name>`, `fold:<component>.<name>`,
  `type:<component>.<vocabName>`
- edges:
  - `component --emits--> kind` (attr: role)
  - `kind --receives--> component` (attr: role, exposure)
  - `kind --feeds--> fold` (consumes match), `fold --held-by--> component`
  - `component --vocab--> type` (a component's declared shapes; without it,
    vocabulary types float disconnected from their owner)
  - `kind --payload--> type`, `type --refs--> type`
- node attributes:
  - component: `owns`, `deprecated`
  - kind: `producer_count`, `consumer_count`, `health` (`ok` | `orphan` |
    `starved` | `ambiguous`), `role`
  - fold: `subject`, `partition_arity`, `consumes`, `component`
  - type: `component`, `deprecated`

Fold nodes carry `subject` and `partition_arity` as attributes because the
partition layout ("one state per account") is an architectural fact worth
exposing in the graph and renderers.

Node ids are built from the **owning component reached structurally**, never by
parsing a name — a fold named `billing.open-invoices` must not be split on a dot
to recover its component.

Edge direction here is *containment and derivation*, not flow: `kind --receives-->
component` reads "this kind is delivered to that component". Renderers are free
to invert it — the Mermaid exporter draws component→component with the kind as
the edge label, which is what a reader of a flow diagram expects.

Exposed three ways:

1. **GraphML export** so archmotif's existing lenses — components, trophic
   layers, spectral clustering — run over the *choreography* instead of the Go
   call graph. Emergent layers and inversions over event flow is the analysis
   that does not exist anywhere else.
2. **Mermaid** for a static event-flow diagram.
3. **uigraph** so the canvas renders it natively — lands with the UI work, not
   with the projection.

### Views

**Event map** — the default canvas view. Components as boxes, event kinds as
chips between them, edge direction = production/consumption. Facts and actions
visually distinct (an action edge is a *call*, a fact edge is a *broadcast*).
Health is the colour axis: starved subscriptions and orphan emissions must be
visible without hunting. Namespace ownership is the grouping axis — a kind chip
sits inside its owner's group, and a call-out is an edge crossing a group
boundary, which is exactly the picture that makes coupling obvious.

**Kind detail** — schema as a field table: name, type, required, **deprecated**,
description. `oneOf` branches rendered as alternative shapes with their
deprecation state. Producers and consumers listed with links. Where a Go binding
exists (§4), a link to the bound type in archai's code graph — this is the
"maps to code" affordance.

**Component detail** — the four roles (inbound / outbound / folds / call-outs)
plus the owned namespace, each linking into the map.

**Deprecation lens** — every deprecated field/shape across the model, with its
producers and consumers, so "can this be removed" is answerable in one view.
This is the cheapest high-value view and should not be deferred.

**Choreography trace (later)** — pick an action, follow action→fact→action
through the graph, render as a Mermaid sequence via the existing
`internal/sequence` + `internal/adapter/mermaid` path.

---

## 4. Mapping to code

archai owns the format, the validation and the graph. **The project owns the
binding.** archai never learns a project's Go types, and is never a dependency of
a production binary — not as a library, not at runtime.

### Codegen

`archai plugin events gen` renders each component's declaration through a
**project-supplied template** (`.arch/templates/*.tmpl`) into the component's
package. Generated files import only project types.

Template data model (a stable archai contract, versioned alongside the format):

```
.Component .Owns .Description .Extra
.Receives[] .Emits[]   {Kind, Role, Description, Exposure, Schema, Extra}
.Folds[]               {Name, Pattern, State, Extra}
.Vocab{}               {Name, Schema, GoType?}
.CallsOut[]            derived: emits where role=action and kind ∉ owns
+ helpers: goIdent, constName, quote, jsonRaw, indent, docComment
```

Generation tiers, adopt in order:

1. **Constants + declaration value** — kind constants and a `Declaration` literal
   in the project's own type. Enough for the project to wire folds from
   `subscribes`, guard its log against undeclared emissions, and project an
   external-API catalog from `exposure`.
2. **Address book** — where a project has a subject grammar, the whole mechanical
   surface (source patterns, partition keys, event constructors, decoders) is a
   function of the declaration and belongs in the template.
3. **Payload structs** — generated from the schemas.

### Rules that make tier 3 survivable

- **Methods are never generated.** Only structs, field tags and doc comments.
  Custom marshaling, validation and helpers are handwritten in another file of
  the same package — Go's multi-file packages are the escape hatch, and a
  generated type never conflicts with a handwritten method.
- **Wire format is reproduced exactly.** Property names in the schema are the
  names on the wire. The generator emits explicit tags and never "improves"
  casing — a durable log is not refactorable.
- **Bindings file** (`.arch/bindings/go.yaml`) does three jobs:
  ```yaml
  types:
    Money: example.com/proj/money.Amount   # reuse existing type
    Bytes: "[]byte"                        # non-bijective mapping
  verify_only:
    - example.com/proj/billing.Invoice     # don't generate, check
  ```
  1. reuse an existing type instead of generating a duplicate (no conversion
     layer appears at the boundary);
  2. express mappings JSON Schema cannot (`[]byte`↔base64, int64, time);
  3. exempt a type from generation while still checking it — archai resolves the
     named type in its **code graph** and diffs its fields against the schema.
     This is the one place the existing Go graph does real work here, and it does
     not require running the project.

### Keeping it honest

Generated files are committed. `//go:generate archai plugin events gen ./...` plus
`go generate ./... && git diff --exit-code` in CI. The generator writes only to
`*_gen.go` and never touches handwritten files — a generator that can clobber
hand-edits will not be re-run.

### Scaffolding (migration only)

`archai plugin events scaffold` reads existing types via the code graph and emits draft
declarations. Sound for ordinary structs, **unsound wherever custom marshaling
exists** — reflection cannot see a legacy wire form. It is a one-time migration
aid, never a maintained direction. There is exactly one authoritative direction:
declaration → code.

---

## 5. Surfaces

Implement as an in-process plugin (`internal/plugins/events`), following
`internal/plugins/complexity`. The plugin contract already carries the four
capability kinds this needs.

| Surface | Entry points |
|---|---|
| CLI | `archai plugin events validate [--root PATH]` · `graph [--root PATH] [--format graphml\|mermaid] [-o FILE]` · later `gen` · `scaffold` |
| MCP | `event_model` (composed model) · `event_kind` (one kind + producers/consumers/schema) · `event_validate` (findings) |
| HTTP | `/api/plugins/events/model` — the projected graph the UI consumes |
| UI | `plugin-events-map` embedded in the canvas; kind/component detail panels |

The CLI lives under `archai plugin <name> …` because the plugin contract uses the
plugin name as the command namespace. That is the contract's behaviour, not a
workaround; a top-level `archai events` alias would require a core change and is
not worth one.

`--root` defaults to the repository root. Discovery skips `.git`, `vendor`,
`node_modules`, `testdata` and similar while walking a tree, but **never skips
the root it was pointed at** — an explicit root is an explicit instruction, and
without that exception a fixture tree under `testdata/` is unreachable from the
CLI.

A validation failure is not a usage error: findings and the summary line are
printed, the exit code is 1, and cobra's usage block stays hidden.

The MCP tools matter as much as the UI: agents author these declarations, and
they need to query the composed model — "who consumes this kind", "what is this
payload's schema", "what breaks if I remove this field" — without reading files.

Host extension likely needed: the plugin must read `.arch/events.yaml` files, and
`plugin.Host` currently exposes only the code model. Add a repo-root/FS accessor
rather than smuggling file reads through the model.

---

## 6. Iterations

**Iteration 1 — model and validation.**
`internal/eventmodel` domain + reader + meta-schema; discovery and composition;
built-in rule set; `archai plugin events validate`; MCP `event_model` / `event_validate`.
No codegen, no UI. Proves the format on real declarations.

**Iteration 2 — graph and views.**
Bipartite projection; GraphML export; uigraph adapter; canvas event map, kind
detail, deprecation lens; Mermaid export. This is the payload of the whole
feature and should land before anything in §4.

**Iteration 3 — codegen tiers 1–2.**
Template data model; `archai plugin events gen`; example templates. Project-side
enforcement (wiring, log guard) is demonstrated, not shipped by archai.

**Iteration 4 — payload binding.**
Bindings file; struct generation (tier 3); `verify_only` checking against the
code graph; `scaffold`.

**Iteration 5 — event policy.**
Selector/operator rules over event edges in the overlay policy block.

Iteration 1 and 2 are worth building even if 3–5 never happen: a validated,
visualized event model is useful with handwritten declarations.

## 7. Open questions

- **Config-expanded components.** Where a component fans out into instances whose
  kinds depend on configuration, part of the contract is not static. Options: a
  parameterized declaration with named holes that the runtime fills and validates
  on the composed set, or declaring the expanded family as a pattern. Needs a
  real example before choosing.
- **Consumes pattern semantics.** `folds[].consumes` entries are matched against
  emitted kinds using dot-segmented globs with NATS semantics: `*` matches
  exactly one segment, `>` matches **one or more** trailing segments (not zero),
  and `**` is an alias for `>`. If a project needs a different dialect, the
  matcher is deliberately isolated so it can be swapped.
- **Subject/kind coherence.** archai does not verify that a consumed kind is
  emitted onto a subject the fold's pattern matches. This coherence check is
  deliberately deferred because it requires archai to understand the project's
  subject grammar, which varies between transports.
- **Kind identity across repos.** Within one repo `kind` is unique. Federating
  later needs a qualifier (owner + version). Deferred, but the format should not
  make it a breaking change.
