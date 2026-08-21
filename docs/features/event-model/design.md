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

- xRegistry / CloudEvents projections. The format is shaped so these map
  mechanically later, but nothing is built for them now. (AsyncAPI 3 is now
  read, not written: see "Reading AsyncAPI declarations" in the usage guide.
  There is still no projection out of wyrd into AsyncAPI.)
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
version: 2
component: billing                # stable id, unique in the repo
owns: billing                     # namespace whose schemas this component defines
description: invoice lifecycle

extra:                            # opaque to archai, passed through to templates
  partition: account
  transport: queue

inputs:                           # the events that trigger this component
  - kind: billing.invoice.issue
    pattern: 'svc.*.billing.{account}.invoice.issue'   # wire address
    description: create an invoice for one account
    exposure: [public_api]        # free-form tags; projects decide what they mean
    schema:
      type: object
      required: [Account]
      properties:
        Account:  {type: string}
        Currency: {type: string}
        Lines:    {type: array, items: {$ref: '#/types/Line'}}

outputs:                          # append a durable event to the log
  - kind: billing.invoice.issued
    pattern: 'svc.*.billing.{account}.invoice.issued'
    schema: {$ref: '#/types/Invoice'}
  - kind: ledger.entry.post       # a kind outside `owns` — legal, not a call-out
    pattern: 'svc.*.ledger.{account}.entry.post'

state_events:                     # folded into state without triggering
  - kind: billing.invoice.issued
    pattern: 'svc.*.billing.{account}.invoice.issued'
  - kind: billing.credit.applied
    pattern: 'svc.*.billing.{account}.credit.applied'

state:                            # optional; the projection shape
  type: object
  properties:
    Open: {type: array, items: {$ref: '#/types/Invoice'}}

types:                            # reusable schema definitions ($defs analogue)
  Invoice:
    type: object
    properties:
      Account: {type: string}
      Status:  {type: string, enum: [open, paid, void]}
      Lines:
        oneOf:
          - {type: array, items: {$ref: '#/types/Line'}}
          - {type: string, deprecated: true,
             description: legacy single-line form, read-only}
```

There is no `folds:` block. The fold is derived from the two lists that are
read-set members: its subjects are their patterns, its consumed kinds are their
kinds, and its partition key is the ordered `{slot}` tokens they share.

### The rule everything else follows from

> **A durable event is appended once and may be observed independently by any
> number of components.**

One appended event can drive several independent reactions — controller A folds
it and appends X, controller B folds it and appends Y, projection C updates a
read model — and neither their ordering nor their completion relative to one
another is guaranteed. There is no ambiguity to detect here. The first iteration of this
design imported an RPC assumption ("an action resolves to exactly one handler")
into an event-sourced model, which is simply wrong for choreography; the rules
that encoded it (`unresolved-call`, `ambiguous-call`, the role × ownership
matrix) are gone.

Exclusive handling is a **transport/runtime policy**, not a property of event
sourcing. Where a project really does have a command with one owner, it opts in
per slot with `delivery: exclusive`, and only then does receiver cardinality
become a validated rule.

### Decisions baked into the shape

**Three lists, and the third is the point.** `inputs` and `outputs` are the
component's ports — what triggers it, what it appends. `state_events` is neither:
it is what the component folds into state without being driven by it.

Observation splits in two because being *driven* by an event and *remembering*
it are different relationships, and a component routinely does the second to
events it never reacts to — its own outcome (record what you just appended) or
another component's that its state has to track. Collapsing both into one
"receives" list loses which of the two a declaration meant, and with it the only
rule that makes a self-loop detectable.

The earlier shape carried `role: action | fact` instead. Direction now says what
role said about movement, and the intent-vs-outcome reading role also carried is
documentation: it belongs in `description` and in the kind name (`x.do` /
`x.done`), not in a field that every producer and observer has to agree on. The
`kind-role-conflict` rule is gone with it.

**A component never triggers itself.** The same kind in `inputs` and `outputs` is
a loop through the boundary that exists to separate the component from everyone
else — a `self-input-conflict` error. The rule can stay narrow precisely because
`state_events` exists: folding your own output is legal, common, and declared
somewhere else, so an inputs/outputs overlap is unambiguously a mistake rather
than a legitimate pattern written in the wrong section. The check is on the exact
kind, and becomes `(kind, pattern)` if one kind ever travels several routes.

**The fold is derived, never declared.** One component holds one state, so the
fold *is* the component. A `folds:` block with its own `name`, `subjects` and
`consumes` would only create a second place for the read-set to be wrong — and a
second place to keep in sync with the slots it is supposed to describe.

**Outputs are not in the read-set.** Appending an event does not subscribe you to
it. This is what makes the derivation safe: without the exclusion, every producer
would silently subscribe to its own entire output surface.

**`pattern` is the kind's address, and it lives on the slot.** A kind is a name;
a pattern is where that name travels — a NATS-style subject with `{slot}` tokens
naming the partition key. Putting it on the slot rather than on a fold block puts
it next to the kind that owns it, which is the only place both sides can be
checked against each other.

One kind has exactly one pattern across the composed set
(`kind-pattern-conflict` error). Two answers to "what subject is this on" means
the subscribers of one will never see what a producer appends on the other: a
wiring bug invisible at runtime until the event silently fails to arrive. This is
a stronger rule than the role agreement it replaces — a role disagreement was a
taxonomy argument, a pattern disagreement is a broken wire. The pattern is
optional, and a declaration that omits it is not disagreeing with one that
carries it.

archai validates `{slot}` syntax but never matches a pattern against a kind —
they are different alphabets, and a subject sharing no segments with its kind's
name is legal.

**One component, one partition key.** Every pattern in the read-set must extract
the same ordered `{slot}` list, or there is no single answer to "which state does
this event belong to" (`partition-mismatch` error). Two exemptions, both
deliberate: a pattern with no slots at all (a globally addressed event
legitimately feeds a partitioned state), and an output-only pattern (outputs are
not in the read-set, so a component keyed by `{account}` may append onto a
subject keyed by anything).

**`state` is optional.** A component may fold nothing worth describing, and the
earlier shape could require it because a `folds` entry existed only when there
was something to project. With the fold derived, requiring a state schema would
force one onto every component that has any input at all. A declared-but-shapeless
`object` still earns an `underspecified-state` warning — that is the shape a
placeholder leaves behind.

**`owns` is definitional authority.** It names the component that defines a
namespace's schemas. It does *not* confer an exclusive right to produce events
in that namespace or to observe them. The only ownership rule left is that two
components must not claim overlapping prefixes, because that would mean two
answers to "what does this kind look like".

**Globs are gone with `consumes`.** The old fold matched kinds by glob
(`consumes: [billing.invoice.*]`), which made starvation fuzzy: a warning fired
per unmatched *entry*, not per kind, and a glob that happened to cover nothing
read the same as a typo. `state_events` lists kinds, so starvation is exact and
the dot-segmented matcher has no remaining caller.

**`types` are reusable schema definitions**, the direct analogue of JSON Schema's
`$defs` — named shapes referenced from payload and state schemas. (Named
`vocab` in the first iteration, which oversold them as a vocabulary concept when
they are plain type definitions.)

**Schemas are inline, in JSON Schema vocabulary, written as YAML.** JSON Schema
is a data model, not a syntax — schemas expressed in YAML are valid JSON Schema
after `yaml→json`. There is no second file and no external schema references by
default. `$ref: '#/types/X'` addresses the component's own types block;
`$ref: 'other-component#/types/X'` addresses another component's (which makes it
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

**A component need not have a runtime.** `owns` + `types` with no `inputs`,
`outputs` or `state_events` is legal: a pure schema owner (a shared contract
package) is a first-class component.

**`owns` is a namespace prefix, resolved by longest prefix.** `owns: billing`
claims schema authority over `billing.*`. Ownership is prefix-based rather than
first-segment so that a namespace can, in principle, be split across components
without renaming its kinds.

**Overlapping `owns` is an error today — including nesting.** Two components
declaring `billing` and `billing.invoice` would be unambiguous under
longest-prefix resolution, so this restriction is not a technical necessity; it
is a deliberate choice of direction. A permissive rule cannot be tightened later
without breaking every project that relied on it, while a strict one can be
relaxed at any time. The longest-prefix resolution stays in the implementation,
so allowing nesting later is a policy change rather than a rewrite. Until then,
split a namespace by giving each component a distinct prefix.

**`owns` may be omitted.** Such a component defines no schemas of its own. It
may still append and observe anything — an edge or gateway component that
orchestrates without defining any namespace is a first-class case, not a
degenerate one. (In the first iteration an ownerless component was restricted in
what it could produce and observe; that restriction came from the RPC framing and
is gone.)

### What archai must implement

- `internal/eventmodel/` — the domain: `Component{Inputs, Outputs, StateEvents,
  State, PartitionKey, Types, Extra}` with `ReadSet()`/`Subjects()`/`Consumes()`
  deriving the fold, and `Slot{Kind, Pattern, Delivery, Description, Exposure,
  Schema, Extra}`. Pure data, no behavior, no dependencies (archai rule 4).
- A reader that discovers every `.arch/events.yaml` under the repo, parses it
  (reuse `internal/adapter/yaml`), validates it against the built-in meta-schema,
  and resolves `$ref`s into a composed `eventmodel.Model`.
- The meta-schema ships **inside the archai binary**. Projects never carry it.

---

## 2. Validation

### Built-in rules

There is **no role × ownership matrix**. Every cell of the one this design
originally carried is now legal: any component may append into any namespace and
observe any namespace, whether or not it owns one. Ownership is authority over a
namespace's *schemas*, and the only rule it produces is uniqueness.

- **single owner** — no two components declare overlapping `owns` (exact or
  nested). Two claimants mean two answers to "what does this kind look like".
  Resolution is longest-prefix.
- **no self-trigger** — a component must not declare the same kind in both
  `inputs` and `outputs` (`self-input-conflict` error). The two are the
  component's ports; routing an output back into your own input is a loop through
  the boundary that exists to separate you from everyone else. Folding your own
  output is unrestricted and is declared in `state_events`. The check is on the
  exact kind, and becomes `(kind, pattern)` once one kind can travel several
  routes.
- **single pattern per kind** — a kind travels one subject across the composed
  set (`kind-pattern-conflict` error otherwise). The pattern is the kind's
  address, not a label; see §1. The graph projection resolves a conflicting kind
  to its first declaration in deterministic order and flags the node with
  `pattern_conflict`, so the projection never silently picks a side. A
  declaration that omits the pattern is not a second answer and is skipped.
- **closure** — every `inputs` kind has ≥1 producer (`starved-input`); every
  `state_events` kind has ≥1 producer (`starved-state-event`); every output has
  ≥1 observer, counting inputs and state events alike, including its own
  producer's (`orphan-event`). All three are warnings.
- **partition coherence** — every pattern in the component's read-set extracts
  the same ordered `{slot}` key (`partition-mismatch` error otherwise). One
  component holds one state; every partitioned subject it reads must address that
  state identically. A pattern with no slots, and an output-only pattern, are
  exempt.
- **state** — `state` is optional; a declared object schema with no properties
  and no `$ref` is an `underspecified-state` warning.
- **exclusive delivery (opt-in)** — a kind declared `delivery: exclusive`
  anywhere must be the input of exactly one component: zero is
  `exclusive-unhandled`, more than one is `exclusive-conflict`, both errors.
  Cardinality counts inputs alone — a state event drives no reaction, so any
  number of components may fold an exclusive kind without competing to handle it.
  Without the opt-in, consumer cardinality is unconstrained — that is the whole
  point.
- **type integrity** — every `$ref` resolves; cross-component refs do not cycle.
- **slot syntax** — `{slot}` tokens in patterns are balanced and non-empty
  (`malformed-slot` error otherwise). archai does not interpret the subject
  beyond this syntax check.
- **schema compatibility** — *deferred, not implemented.* An output payload
  should be a structural subset of each consumer's inbound schema for that kind.
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

- `archai plugin events validate` (static, whole repo): ownership uniqueness,
  pattern agreement, partition coherence, ref integrity and exclusive-delivery
  breaches are **errors**; starvation, orphans and underspecified state are
  **warnings**.
- The same rules evaluated by the project at startup over the *actually composed*
  set: starvation becomes an **error**. archai supplies the rules; enforcement at
  startup is the project's code (see §4).

### Project-specific rules

Reuse the `dependency-policy` selector/operator design over the event graph
rather than inventing a second policy language. Selectors name component sets
(`@layer`, globs); operators run over event edges:

```
@edge     ->  billing.*             # may append into this namespace
@domain   !-> @adapter              # domain components must not call adapters
shipping  ~>  ledger.* via @audit   # every path to ledger.* passes through audit
```

Lives in the existing `.arch/overlay.yaml` policy block, qualified to event
edges. Implementation reuses `internal/policy` with a different edge source.

---

## 3. Graph and visualization — the primary deliverable

### Projection

The composed model projects to a **bipartite graph**:

- nodes: `component:<id>`, `kind:<name>`, `type:<component>.<typeName>`
- edges (every one pointing the way data travels):
  - `component --output--> kind`
  - `kind --input--> component` (attr: exposure)
  - `kind --state-event--> component` (attr: exposure)
  - `component --defines--> type` (a component's declared shapes; without it,
    type definitions float disconnected from their owner)
  - `kind --payload--> type`, `type --refs--> type`
- node attributes:
  - component: `owns`, `subjects`, `consumes`, `partition_key`,
    `partition_arity`, `has_state`
  - kind: `producer_count`, `input_count`, `state_fold_count`, `health`
    (`ok` | `orphan` | `starved` | `ambiguous`), `pattern`, `partition_key`,
    `pattern_conflict`, `delivery`.
    `ambiguous` is reserved for exclusive kinds with more than one input
    consumer; a broadcast kind with many observers is `ok`, and so is an
    exclusive kind with one consumer and any number of components folding it.
  - type: `component`, `deprecated`

**There is no fold node.** One component holds one state, so the fold is the
component, and its read-set, partition key and state shape are attributes of the
component that holds them. A separate vertex would sit alone next to every
component carrying exactly those three things.

The partition layout ("one state per account") stays exposed in the graph
because it is an architectural fact renderers should be able to read.

Node ids are built from the **owning component reached structurally**, never by
parsing a name — a type in a component named `billing.invoice` must not be
recovered by splitting `type:billing.invoice.Line` on the last dot.

Edge direction here is flow: `kind --input--> component` reads "this kind
triggers that component". Renderers are free to collapse it — the Mermaid
exporter draws component→component with the kind as the edge label, which is
what a reader of a flow diagram expects.

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
chips between them, edge direction = production/observation. A kind that
triggers its target and one it merely updates read differently (a solid versus a
dashed edge in Mermaid); an exclusive kind is the one that deserves call-like
emphasis.
Health is the colour axis: starved subscriptions and orphan emissions must be
visible without hunting. Namespace ownership is the grouping axis — a kind chip
sits inside the group of the component that defines it, and an emission into
another namespace is an edge crossing a group boundary, which is exactly the
picture that makes coupling obvious.

**Kind detail** — schema as a field table: name, type, required, **deprecated**,
description. `oneOf` branches rendered as alternative shapes with their
deprecation state. Producers and consumers listed with links. Where a Go binding
exists (§4), a link to the bound type in archai's code graph — this is the
"maps to code" affordance.

**Component detail** — inbound observations, outbound emissions and folds, plus
the owned namespace, each linking into the map.

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

There is deliberately no `--lang` flag. A language generator built into archai
would invert the split this whole section rests on: archai would have to know
the project's types, and would become a dependency of its build.

Template data model (a stable archai contract, versioned alongside the format —
templates live in projects, so renaming a field breaks every project reading it):

```
.Component .Owns .Description .Extra
.Inputs[] .Outputs[] .StateEvents[]
                       {Kind, Pattern, PartitionKey, Delivery, Description,
                        Exposure, Schema, Extra}
                       + .Exclusive (method)  ·  Delivery normalized to "broadcast"
.Fold                  {Subjects, PartitionKey, Consumes, State}   derived, not declared
.Types[]               {Name, Schema}          sorted by name
.ForeignInputs[] .ForeignOutputs[] .ForeignStateEvents[]   derived: kind ∉ owns
.Kinds[]                                       sorted union of all three lists
+ helpers: goIdent, unexported, quote, jsonRaw, jsonIndent, indent, docComment,
           hasPrefix, trimPrefix, join, sortedKeys
```

Map-backed fields are flattened into sorted slices so re-running the generator
does not churn the diff. Templates run with `missingkey=error`: reaching for a
key the declaration never set fails at generation time rather than silently
emitting an empty string, and `index` remains available as the presence test.

Generation is gated on validation: a `kind-pattern-conflict` yields colliding
constants and a `partition-mismatch` yields a subscription keyed on the wrong
slot, neither of which the compiler catches. `--force` overrides.

Generated `.go` output is passed through `go/format`. That is syntactic only —
not archai learning the project's types — and it earns its place because
generated files are committed: unformatted output makes every diff noisy, and a
template emitting invalid Go fails at generation time rather than at compile
time. Other extensions pass through verbatim.

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
  go_types:
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
| MCP | `event_model` (composed model) · `event_kind` (one kind + producers/observers/delivery/schema) · `event_validate` (findings) |
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

**Iteration 3 — codegen tiers 1–2.** *(tier 1 shipped)*
Template data model, `archai plugin events gen` and an example template are in
place: a project copies `docs/features/event-model/templates/contract_gen.go.tmpl`
into its `.arch/templates/` and gets kind constants plus the mechanical surface
(port lists, foreign kinds, exclusive kinds, fold subjects/partition keys,
schema literals). Tier 2 — a subject address book, constructors, decoders — is
a richer template over the same data model, and lives in the project.
Project-side enforcement (wiring, log guard) is demonstrated, not shipped by
archai.

**Iteration 4 — payload binding.**
Bindings file; struct generation (tier 3); `verify_only` checking against the
code graph; `scaffold`.

**Iteration 5 — event policy.**
Selector/operator rules over event edges in the overlay policy block. This is
also where anything resembling a delivery or handling constraint beyond
`delivery: exclusive` belongs — as declared project policy, never as default
event-model semantics.

Iteration 1 and 2 are worth building even if 3–5 never happen: a validated,
visualized event model is useful with handwritten declarations.

## 7. Open questions

- **Config-expanded components.** Where a component fans out into instances whose
  kinds depend on configuration, part of the contract is not static. Options: a
  parameterized declaration with named holes that the runtime fills and validates
  on the composed set, or declaring the expanded family as a pattern. Needs a
  real example before choosing.
- **Kind globs.** `state_events` lists kinds exactly. Whether a project ever
  needs to fold a family of kinds by pattern is open; the earlier `consumes`
  glob made starvation fuzzy without a real case demanding it, so it was
  removed rather than kept on speculation.
- **Subject/kind coherence.** archai does not verify that the pattern a kind
  declares is one the transport would actually route it on. That check requires
  archai to understand the project's subject grammar, which varies between
  transports, so the format settles for the weaker guarantee that everyone
  writes the *same* pattern for a kind.
- **Undeclared kinds.** A component may append or observe a kind in a namespace
  owned by another component that declares no slot for it — the schema is
  undefined. That is arguably an `undeclared-kind` warning under the new
  ownership semantics, but it is not implemented; ownership currently produces
  only the uniqueness rule.
- **Delivery policies beyond exclusive.** `broadcast` and `exclusive` are the
  two the model needs today. Anything richer (queue groups, at-least-once vs
  effectively-once, ordering guarantees) is transport configuration and should
  stay out of the architectural declaration until a real example forces it.
- **Kind identity across repos.** Within one repo `kind` is unique. Federating
  later needs a qualifier (owner + version). Deferred, but the format should not
  make it a breaking change.
