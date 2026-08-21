# Event Model: Usage Guide

Declarative event-driven architecture for any project. Each component declares
its event interface as data in `.arch/events.yaml`; wyrd validates the
composed set and projects it to a graph for visualization and analysis.

Use this feature when your system is event-driven (pub/sub, CQRS, event
sourcing) and you want machine-readable documentation of what each component is
triggered by, produces, and keeps in state. The declarations enable composition
checks (namespace ownership, wire addresses, closure, partition coherence) and
graph-based analysis before runtime.

## The core rule

> A durable event is appended once and may be observed independently by any
> number of components.

## Three lists, and nothing else

A component declares what triggers it, what it appends, and what it keeps:

```
inputs        the events that trigger it
outputs       the events it appends to the log
state_events  the events it folds into its state without being triggered
```

`state_events` is the third list because observation splits in two. Being
*driven* by an event and *remembering* it are different relationships, and a
component routinely does the second to events it never reacts to — its own
outcome, or another component's, that its state has to track.

Everything else is derived:

| Derived | From |
|---|---|
| the fold's read-set | the `pattern` of every `inputs` and `state_events` entry |
| the kinds the reducer folds | the `kind` of every `inputs` and `state_events` entry |
| the partition key | the ordered `{slot}` tokens those patterns share |

**Outputs are not in the read-set.** Appending an event does not subscribe you
to it. A component that wants its own outcome back declares it in
`state_events` too, which is the common case and costs one line.

There is no `folds:` block. One component holds one state, so the fold *is* the
component; declaring it separately would only create a second place for the
read-set to be wrong.

## Inputs and outputs are ports; state events are not

A kind in both `inputs` and `outputs` describes a component triggering itself —
a loop through the boundary that exists to separate it from everyone else,
drawing a self-edge with no runtime referent. That is a `self-input-conflict`
**error**.

A kind in both `state_events` and `outputs` is the normal way to fold the
outcome you just appended, and is never a finding. The two rules only work
together: because there is a legal channel for folding your own output, an
inputs/outputs overlap is unambiguously a mistake rather than a legitimate
pattern written in the wrong section.

```yaml
# WRONG — self-input-conflict. The component triggers itself.
outputs:
  - kind: llm.failed
inputs:
  - kind: llm.failed

# RIGHT — state_events is not a port; it is state over the log.
outputs:
  - kind: llm.failed
    pattern: 'svc.*.llm.{session}.failed'
state_events:
  - kind: llm.failed
    pattern: 'svc.*.llm.{session}.failed'
state:
  type: object
  properties:
    Count: {type: integer}
```

The rule is narrow. It does not restrict a component folding its own kinds,
other components taking the kind as an input (any number of them), or the same
component outputting `X` and being triggered by a different kind `Y`.

## The pattern is the kind's address

A `kind` is a name; a `pattern` is where that name travels on the wire —
a NATS-style subject with `{slot}` tokens naming the partition key.

One kind has exactly one pattern across the whole composed set. Two answers to
"what subject is this on" means the subscribers of one will never see what a
producer appends on the other: a wiring bug invisible at runtime until the event
silently fails to arrive. Disagreement is a `kind-pattern-conflict` **error**.

```yaml
# WRONG — one kind, two addresses. kind-pattern-conflict.
# in llm/.arch/events.yaml
outputs:
  - kind: llm.message
    pattern: 'svc.*.llm.{session}.message'
# in router/.arch/events.yaml
inputs:
  - kind: llm.message
    pattern: 'svc.*.router.{session}.message'
```

The pattern is optional. A declaration that omits it is not disagreeing with one
that carries it — it contributes nothing to the read-set's subjects and nothing
to the partition key, and is skipped by the conflict check.

## One component, one state

Every pattern in the read-set must extract the **same ordered partition key**. A
component holds exactly one state, and every partitioned subject it reads must
identify that state identically. Disagreement is a `partition-mismatch`
**error**.

```yaml
# OK — both read-set entries address per-account state.
inputs:
  - kind: billing.invoice.issue
    pattern: 'svc.*.billing.{account}.invoice.issue'
state_events:
  - kind: billing.credit.applied
    pattern: 'svc.*.billing.{account}.credit.applied'

# ERROR (partition-mismatch) — which state does an event belong to?
state_events:
  - kind: billing.credit.applied
    pattern: 'svc.*.billing.{region}.credit.applied'

# ERROR (partition-mismatch) — order is significant, [region sku] != [sku region].
state_events:
  - kind: warehouse.stock.adjusted
    pattern: 'svc.*.warehouse.{region}.{sku}.stock.adjusted'
  - kind: warehouse.stock.depleted
    pattern: 'svc.*.warehouse.{sku}.{region}.stock.depleted'
```

Two exemptions, both deliberate:

- A pattern carrying **no slots at all** is exempt. A globally addressed event
  legitimately feeds a partitioned state.
- An **output-only** pattern is exempt, because outputs are not in the read-set.
  A component keyed by `{account}` may append onto a subject keyed by anything.

## File Location and Discovery

Place `.arch/events.yaml` in each component's directory. wyrd walks the tree
from the specified root, entering `.arch/` directories and reading
`events.yaml` when present. It reads `.arch/asyncapi.yaml` from the same
directory too — see [Reading AsyncAPI](#reading-asyncapi-declarations).

**Skipped directories** (below the root, not at the root itself):
`.git`, `.worktrees`, `.claude`, `bin`, `vendor`, `node_modules`, `testdata`.

**Explicit-root exception:** If the root path itself has a skipped name (e.g.,
`--root /path/to/testdata`), wyrd honors the explicit instruction and scans
it anyway; the skip list only filters subdirectories.

The component id must be unique across the entire scanned tree. Duplicate ids
cause a parse error.

## Format Reference

```yaml
version: 2                   # required; currently only version 2 is supported
component: billing           # required; stable unique id for this component
owns: billing                # optional; namespace whose schemas this component defines
description: Invoice lifecycle management  # optional

inputs:                                    # events that trigger this component
  - kind: billing.invoice.issue            # required; the event kind name
    pattern: 'svc.*.billing.{account}.invoice.issue'  # optional; wire address
    delivery: exclusive            # optional; "broadcast" (default) or "exclusive"
    description: Create an invoice # optional
    exposure: [public_api]         # optional; free-form tags
    schema:                        # optional; JSON Schema in YAML syntax
      type: object
      properties:
        Account: {type: string}

outputs:                                   # events this component appends
  - kind: billing.invoice.issued
    pattern: 'svc.*.billing.{account}.invoice.issued'
    schema: {$ref: '#/types/Invoice'}

state_events:                              # events folded into this component's state
  - kind: billing.invoice.issued
    pattern: 'svc.*.billing.{account}.invoice.issued'

state:                           # optional; the projection state schema
  type: object
  properties:
    Open: {type: array, items: {$ref: '#/types/Invoice'}}

types:                           # optional; reusable schema definitions ($defs)
  Invoice:
    type: object
    properties:
      Status: {type: string, enum: [open, paid, void]}

extra:                           # optional; opaque passthrough for templates
  partition: account
```

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `version` | int | yes | Schema version. Must be `2`. |
| `component` | string | yes | Stable component id, unique in the repo. |
| `owns` | string | no | Namespace whose schemas this component defines (e.g., `billing` defines `billing.*`). Not a production or observation restriction. |
| `description` | string | no | Human-readable summary. |
| `inputs` | list of Slot | no | Event kinds that trigger this component. |
| `outputs` | list of Slot | no | Event kinds this component appends to the log. |
| `state_events` | list of Slot | no | Event kinds this component folds into its state without being triggered by them. |
| `state` | schema | no | The projection state shape. Full schema or `$ref`. |
| `types` | map | no | Reusable JSON Schema definitions, `$defs`-style. |
| `extra` | map | no | Opaque passthrough for codegen templates. |

**Slot** (an entry in any of the three lists):

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `kind` | string | yes | The event kind name. |
| `pattern` | string | no | The subject the kind travels on, with `{slot}` tokens. |
| `delivery` | string | no | `broadcast` (default) or `exclusive`. |
| `description` | string | no | |
| `exposure` | list | no | Free-form API-surface tags. |
| `schema` | schema | no | Payload schema. |
| `extra` | map | no | Opaque passthrough. |

**Strict decoding:** Unknown YAML keys cause a parse error. Typos like
`componet:` or `inpts:` are caught immediately. A file still on `version: 1` is
rejected with a message naming what changed, because the old shape
(`receives`/`emits`/`folds`) parses far enough to fail confusingly.

## Reading AsyncAPI declarations

A project that already publishes its event log as AsyncAPI 3 does not have to
restate it. Put the document at `.arch/asyncapi.yaml` and wyrd projects it onto
the same three lists, so the graph, the canvas and the MCP tools work over it
without knowing which format it came from. Both files may sit in one `.arch/`
directory; they produce two components, and only colliding ids are an error.

wyrd reads the documents. It does not write them — there is no projection from
`events.yaml` to AsyncAPI.

### The extension the documents carry

AsyncAPI describes what one application sends and receives. Four facts of an
event-log contract have no vocabulary in it, and the documents carry them under
a single `x-eventlog` key at four locations:

| Where | Field | What it says |
|---|---|---|
| root | `owns` | address prefixes this application owns |
| root | `key` | the fold's partition key, in canonical order |
| root | `addressing` | the framework prefix and the tenant parameter |
| root | `port` | the instance parameter and the family it names |
| channel | `partition` | which of this address's parameters are partition coordinates |
| operation | `role` | `command`, `event`, `call` or `observe` |
| message | `class` | `command` or `event` |

Documents state `vocabulary: eventlog-asyncapi/1` at the root as a version gate.

### Roles map onto the three lists

| `x-eventlog.role` | action | address | wyrd list |
|---|---|---|---|
| `command` | receive | owned | `inputs` |
| `observe` | receive | either | `state_events` |
| `event` | send | owned | `outputs` |
| `call` | send | foreign | `outputs` |

The source model's read-set is commands plus observes, which is inputs plus
state events exactly. What the projection drops is the event/call distinction,
and it is recoverable by matching the address against `owns`. Both are kept on
the slot for display, so the canvas still shows a call as a call.

A document with no `x-eventlog` at all still reads: `action: receive` becomes an
observe and `send` becomes an output. That is enough to place every operation,
and not enough to tell an event from a call.

### The partition key is read, not derived

For a native declaration the key is every `{slot}` of the pattern. That rule is
wrong for an AsyncAPI address, which is in wire coordinates: `{tenant}` is
stamped by the bus and the port parameter selects an instance, and neither keys
a fold. The document states the answer — `x-eventlog.key` at the root, and
`partition` per channel — and wyrd takes it verbatim.

### A port is one component

One document describes a family of instances sharing a contract. It reads as one
component; `port.instances` and any per-operation `instances` ride along and are
shown on the node and in its detail. Nothing expands the family into one
component per instance.

### Imported declarations are not validated

`wyrd plugin events validate` skips them, and so does the CLI's component count.
Every rule is written against the native format's conventions, and the imported
form breaks three of them by construction:

- a port puts one kind on one address per instance, so `kind-pattern-conflict`
  would fire on every port kind;
- a caller may bind coordinates the owner leaves open, which is the same rule
  again from the other side;
- a document describes one application, not a closed system, so closure warnings
  would fire on every edge that leaves it.

wyrd reads a published document to draw it, not to grade it. The graph still
computes health across the composed set, so a kind nobody in the repo observes
still reads as an orphan on the canvas.

## Schemas

Schemas are JSON Schema written in YAML syntax. They are stored opaquely and
used for `$ref` resolution and deprecated-field detection. wyrd does NOT
validate payloads against schemas or check schema compatibility between
producers and observers.

### `$ref` resolution

Two forms are supported:

- **Local**: `{$ref: '#/types/Name'}` references a key in the same
  component's `types` block.
- **Cross-component**: `{$ref: 'other-component#/types/Name'}` references a
  key in another component's `types`. The target component must exist.

Unresolved refs produce an `unresolved-ref` error. Cross-component cycles
produce a `ref-cycle` error.

### Deprecated fields

Mark a schema property as deprecated:

```yaml
properties:
  OldField:
    type: string
    deprecated: true
    description: legacy field, read-only
```

The `deprecated: true` flag is preserved in the model. Schema alternatives can
use `oneOf` with one branch marked deprecated to represent legacy shapes.

## Subject Slot Syntax

Patterns are NATS-style subjects with `{slot}` tokens declaring the partition
key. wyrd validates only the `{slot}` syntax and never matches a pattern
against a kind — they are different alphabets, and a subject that shares no
segments with its kind's name is legal:

- `{slot}` tokens must be balanced (matching `{` and `}`)
- `{slot}` tokens must be non-empty (no `{}`)
- Nested braces are not allowed

Examples:
- `svc.*.billing.{account}.invoice.issued` — one slot, partition per account
- `svc.*.warehouse.{region}.{location}.stock.{sku}.>` — three slots

Malformed `{slot}` syntax produces a `malformed-slot` error.

Note: in YAML, a value containing `{` must not be written in flow style
(`[svc.{a}.>]` is a parse error). Quote the entry.

## Validation Rules

### Error severity

- **Errors** are fatal rule breaches; exit code 1.
- **Warnings** are potential issues that may be acceptable; exit code 0.

### What ownership does and does not mean

`owns` declares the component that **defines the schemas** for a namespace.
That is all. In particular:

| Situation | Verdict |
|---|---|
| output a kind in a namespace you do not own | legal |
| be triggered by a kind in a namespace you do not own | legal |
| fold a kind from a namespace you do not own | legal |
| a component with no `owns` at all | legal, in every position |
| two components claiming overlapping `owns` | **error** (`duplicate-owner`) |

Two claimants over one namespace mean two answers to "what does this kind look
like", so overlapping ownership — exact or nested — stays an error. Ownership
is resolved by longest prefix, and reported by `event_kind` as the kind's
schema owner.

### All finding kinds

| Finding Kind | Severity | Trigger | Fix |
|--------------|----------|---------|-----|
| `duplicate-owner` | error | Two components declare overlapping `owns` (exact or nested) | Give each component a distinct namespace prefix |
| `kind-pattern-conflict` | error | One kind is declared on more than one subject pattern across the composed set | Settle on one address, or split into separate kinds if the routes are genuinely different |
| `self-input-conflict` | error | One component declares the same kind in both `inputs` and `outputs` | Move it to `state_events` to fold the component's own outcome |
| `partition-mismatch` | error | The read-set's patterns extract different ordered partition keys | Make every partitioned subject address the same state |
| `malformed-slot` | error | A pattern has invalid `{slot}` syntax | Fix the `{slot}` tokens (balance braces, non-empty) |
| `unresolved-ref` | error | `$ref` points to a nonexistent `types` entry | Fix the path or add the missing type |
| `ref-cycle` | error | Cross-component `$ref` forms a cycle | Break the cycle by inlining or restructuring |
| `exclusive-unhandled` | error | A kind declared `delivery: exclusive` is nobody's input | Add the handler, or drop the exclusive declaration |
| `exclusive-conflict` | error | A kind declared `delivery: exclusive` is the input of more than one component | Remove the extra handlers, or drop the exclusive declaration |
| `starved-input` | warning | An `inputs` entry no component outputs — the trigger never fires | Add a producer or remove the unused input |
| `starved-state-event` | warning | A `state_events` entry no component outputs — the projection tracks nothing | Add a producer or remove the entry |
| `orphan-event` | warning | An output nobody takes as an input and nobody folds | Add an observer or remove the unused output |
| `underspecified-state` | warning | `state` is `type: object` with no properties and no `$ref` | Declare the projection shape, reference a type, or drop the field |

Exclusive cardinality counts **inputs alone**. A state event is an observation
that drives no reaction, so any number of components may fold an exclusive kind
without competing for the right to handle it.

### Removed rules

- **`kind-role-conflict`** — `role: action | fact` no longer exists. The list a
  kind is declared in says which direction it moves; the intent/outcome reading
  it also carried is documentation, and belongs in `description` and in the kind
  name (`x.do` / `x.done`).
- **`starved-fold`** — replaced by `starved-state-event`, which is exact rather
  than glob-matched, because `state_events` lists kinds and not patterns over
  them.
- **`unresolved-call`**, **`ambiguous-call`**, **`ownership-violation`** — RPC
  assumptions from the first iteration. Replaced by `orphan-event`,
  `exclusive-conflict`, and nothing, respectively.

### What is NOT checked

- **Schema compatibility**: wyrd does not verify that an output payload is
  compatible with a consumer's inbound schema. Schemas are recorded but not
  compared.
- **Runtime conformance**: wyrd does not observe actual event traffic.
- **Ordering**: nothing here constrains the order in which independent
  observers of one event complete, because nothing can.
- **Project-specific policy**: custom rules over the event graph are not yet
  implemented.

## Commands

### CLI

Commands live under `wyrd plugin events`.

**validate**

```
wyrd plugin events validate [--root PATH]
```

Validates all `.arch/events.yaml` declarations under root. Prints findings and
a summary. Exits 0 if no errors (warnings permitted), 1 if errors exist.

```
$ wyrd plugin events validate --root ./services
OK: 5 component(s) validated.
```

```
$ wyrd plugin events validate --root ./broken
ERROR [exclusive-unhandled] billing:ledger.entry.post: outputs "ledger.entry.post" declared delivery: exclusive but no component takes it as an input
ERROR [partition-mismatch] billing:svc.*.billing.{region}.invoice.applied: subject "svc.*.billing.{region}.invoice.applied" extracts partition key [region] but the component's read-set is keyed by [account]; one component holds one state, so every partitioned subject it reads must address it identically
WARN [orphan-event] billing:billing.invoice.issued: outputs "billing.invoice.issued" but no component takes it as an input or folds it into state

2 error(s), 1 warning(s)
Error: validation failed
```

**graph**

```
wyrd plugin events graph [--root PATH] [-f FORMAT] [-o FILE]
```

Generates a graph of the event model. Supported formats:
- `mermaid` (default): Mermaid flowchart diagram
- `graphml`: GraphML XML for analysis tools

```
$ wyrd plugin events graph --root ./services --format mermaid
flowchart LR
    subgraph ns___none__[unowned]
        gateway[gateway]
    end
    subgraph ns_billing[billing]
        billing[billing]
    end
    subgraph ns_ledger[ledger]
        ledger[ledger]
    end
    billing -->|entry.post| ledger
    billing -.->|invoice.issued| analytics
    gateway -->|invoice.issue| billing
    ledger -->|entry.posted| gateway
```

A solid arrow (`-->`) means the kind is an input of the target — it triggers it.
A dashed arrow (`-.->`) means the target only folds the kind into state. A
component folding its own output draws no self-loop: it is the normal idiom, so
drawing it would put a loop on nearly every node. The JSON graph keeps the edge.

### MCP Tools

When running as an MCP server, three tools are exposed:

**event_model**

Arguments: none

Returns the composed model as text: components with their
inputs/outputs/state_events/types, plus the derived fold read-set.

**event_kind**

Arguments: `{"kind": "billing.invoice.issued"}`

Returns detail for one event kind: subject pattern, delivery policy, schema
owner, producers, the components it triggers, the components that fold it into
state, payload schema, deprecated fields.

**event_validate**

Arguments: none

Returns validation findings as text (same format as the CLI).

**gen**

```
wyrd plugin events gen [--root PATH] [--templates DIR] [--out DIR]
                         [--component ID] [--dry-run] [--force] [--no-format]
```

Renders each component's declaration through project-supplied templates. See
[Codegen](#codegen) below.

### HTTP

`GET /api/plugins/events/model` returns the composed model in the canvas's
units: `components`, `flows` and `kinds`.

A flow is one producer-to-observer edge with the kind it carries and whether
that kind triggers the target or is only folded by it — the bipartite graph
collapsed onto the components, which is what a reader of an event model looks
at. Empty repos answer with empty lists, never nulls.

A kind carries its payload schema and an `example` — one instance of that
schema, with `$ref`s already followed. The example is built here rather than in
the canvas because a ref resolves against the component that declared the
schema, and the wire shape does not carry that. Where the schema states a value
(`const`, `example`, `examples`, `default`, an `enum`) the example uses it;
everywhere else it carries a placeholder naming the type. A recursive type
expands once and stops.

### Canvas

The **Events** button in the app bar opens the event canvas over the review
canvas. One node per component, one edge per component pair labelled with what
travels it, solid for a trigger and dashed for a fold. Clicking a node accents
everything it touches and opens its three lists; clicking a kind shows its
subject, partition coordinates, class, schema owner, everyone that appends, is
triggered by, or folds it, and its payload twice: as an example object first,
then as the schema. Esc puts the detail down, and a second Esc closes the
canvas.

The header counts what was read: components, kinds, how many components came
from an AsyncAPI document, and one chip per unhealthy state — `orphan`,
`starved`, `ambiguous` — counted separately, because a single total names
none of the three findings.

An imported node is marked `asyncapi` and says so in its detail, because it is
not validated. A component nothing reaches and that reaches nothing is drawn
with a dashed border rather than dropped — a declaration reaching nobody is the
finding the picture exists to make visible.

## Graph Projection

The model projects to a bipartite graph:

- nodes: `component:<id>`, `kind:<name>`, `type:<component>.<typeName>`
- edges: `output` (component → kind), `input` (kind → component), `state-event`
  (kind → component), `defines` (component → type), `payload` (kind → type),
  `refs` (type → type)
- component attributes: `owns`, `subjects`, `consumes`, `partition_key`,
  `partition_arity`, `has_state`
- kind attributes: `producer_count`, `input_count`, `state_fold_count`,
  `health` (`ok` | `orphan` | `starved` | `ambiguous`), `pattern`,
  `partition_key`, `delivery`, and `pattern_conflict: true` when declarations
  disagree (the reported pattern is then the first in deterministic order)

Every edge points the way the data travels: outputs away from the component,
inputs and state events into it.

**There is no fold node.** One component holds one state, so the fold is the
component; a separate vertex would sit alone next to every component and carry
the same three attributes.

`health: ambiguous` is reserved for exclusive kinds with more than one input
consumer. A broadcast kind with many observers is `ok`, and so is an exclusive
kind with one consumer and any number of components folding it.

## Codegen

wyrd owns the declaration format, its validation and its graph. **The project
owns the binding.** Templates live in the project, wyrd never learns the
project's types, and nothing wyrd produces is a runtime dependency of the
generated code.

There is deliberately **no `--lang` flag**. A language generator built into
wyrd would invert that split: wyrd would have to know your types, and would
become a dependency of your build. Instead it renders your template against a
stable data model.

### Running it

```
wyrd plugin events gen [--root PATH] [--templates DIR] [--out DIR]
                         [--component ID] [--dry-run] [--force] [--no-format]
```

- Templates come from `<root>/.arch/templates/*.tmpl` unless `--templates`
  points elsewhere. Each template is rendered **once per component**.
- The output name is the template name minus `.tmpl`, and it must contain
  `_gen.` — so `contract_gen.go.tmpl` produces `contract_gen.go`. A template
  that would write a plausibly-handwritten name is rejected before anything is
  generated: a generator that can clobber hand-edits will not be re-run.
- Output lands next to the component's `.arch` directory, or under
  `<out>/<component>/` with `--out`.
- The model is **validated first**. Generating from a set with errors produces
  code that is wrong in ways the compiler cannot catch — a subscription pointed
  at the wrong subject from a `kind-pattern-conflict`, a state keyed on the
  wrong slot from a `partition-mismatch`. `--force` overrides.
- Generated `.go` files are run through gofmt. This is syntactic only — it is
  not wyrd learning your types — and it exists because generated files are
  committed: unformatted output makes every diff noisy, and a template emitting
  invalid Go fails here rather than at compile time. `--no-format` skips it;
  other extensions always pass through verbatim.

A starting template ships at
`docs/features/event-model/templates/contract_gen.go.tmpl`. Copy it into your
project's `.arch/templates/` and edit it — it is an example, not a contract.

### Template data model

This is the contract. Templates live in projects, so these names are stable.

| Field | Type | Notes |
|-------|------|-------|
| `.Component` | string | Component id |
| `.Owns` | string | Owned namespace, `""` if none |
| `.Description` | string | |
| `.Extra` | map | The declaration's opaque passthrough block |
| `.Inputs` `.Outputs` `.StateEvents` | []Slot | In declaration order |
| `.Fold` | Fold | The derived read-set; nothing declares it |
| `.Types` | []Type | `{Name, Schema}`, sorted by name |
| `.ForeignInputs` `.ForeignOutputs` `.ForeignStateEvents` | []Slot | The subsets whose kind falls outside `.Owns` — the component's cross-namespace coupling |
| `.Kinds` | []string | Sorted unique kinds across all three lists; the natural input for a constant block |

**Slot**: `.Kind`, `.Pattern`, `.PartitionKey`, `.Delivery`, `.Description`,
`.Exposure`, `.Schema`, `.Extra`, and `.Exclusive` (a method, so
`{{if .Exclusive}}` works). `.PartitionKey` is the `{slot}` names parsed out of
`.Pattern`, so a template binding subscriptions never parses the pattern itself.
`.Delivery` is normalized — it is `"broadcast"` when the declaration omitted it,
so templates never special-case the empty string.

**Fold**: `.Subjects`, `.PartitionKey`, `.Consumes`, `.State`. Subjects and
Consumes are inputs plus state events, in that order, deduplicated; outputs are
excluded.

`.Schema` and `.State` are the raw JSON-Schema-as-YAML trees (maps, slices,
scalars) or nil. Render them with `jsonRaw` / `jsonIndent` rather than walking
them by hand.

Everything map-backed is flattened into a sorted slice, so re-running the
generator does not churn the diff.

### Helper functions

| Helper | Does |
|--------|------|
| `goIdent s` | Exported Go identifier: `billing.invoice.issued` → `BillingInvoiceIssued` |
| `unexported s` | Same, lowercased first rune |
| `quote s` | Go string literal |
| `jsonRaw v` / `jsonIndent v` | Schema node as compact / indented JSON |
| `indent n s` | Prefix every non-empty line with n spaces |
| `docComment s` | Wrap text as `//` lines; empty in, empty out |
| `hasPrefix` `trimPrefix` `join` | The `strings` equivalents |
| `sortedKeys m` | A map's keys in sorted order |

**The wire name is authoritative.** `goIdent` produces a Go *name*; the value it
labels must always be the declared kind string, emitted verbatim. A durable log
is not refactorable, so never derive a wire name from an identifier. The same
goes for `pattern`: it is an address, not a label.

**Missing keys are errors.** Templates run with `missingkey=error`: reaching for
`.Extra.whatever` when the declaration never set it fails at generation time
rather than silently emitting an empty string. Use `index` as the presence test:

```
{{ with index .Extra "go_package" }}{{ . }}{{ else }}{{ unexported $.Component }}{{ end }}
```

### Keeping it honest

Generated files are committed. Wire it up with
`//go:generate wyrd plugin events gen` plus `go generate ./... && git diff
--exit-code` in CI, and drift becomes a build failure rather than a discovery.

## Worked Example

A minimal system: billing issues invoices and asks the ledger to post entries;
the ledger records them; two independent observers watch the same event.

**billing/.arch/events.yaml**

```yaml
version: 2
component: billing
owns: billing
description: Invoice lifecycle management

inputs:
  - kind: billing.invoice.issue
    pattern: 'svc.*.billing.{account}.invoice.issue'
    description: Create an invoice
    exposure: [public_api]
    schema:
      type: object
      required: [Account]
      properties:
        Account: {type: string}

outputs:
  - kind: billing.invoice.issued
    pattern: 'svc.*.billing.{account}.invoice.issued'
    schema:
      type: object
      properties:
        InvoiceID: {type: string}
        Account: {type: string}
  - kind: ledger.entry.post
    pattern: 'svc.*.ledger.{account}.entry.post'
    description: >-
      Appended into a namespace billing does not own. Legal: ownership is
      schema authority, not an exclusive right to produce.
    schema:
      type: object
      properties:
        Amount: {type: number}

state_events:
  - kind: billing.invoice.issued
    pattern: 'svc.*.billing.{account}.invoice.issued'

state:
  type: object
  properties:
    Open: {type: integer}
```

Billing folds the outcome it just appended. That is `state_events`, not
`inputs` — the invoice does not re-trigger the component that issued it.

**ledger/.arch/events.yaml**

```yaml
version: 2
component: ledger
owns: ledger
description: Double-entry accounting ledger

inputs:
  # A genuine command with one owner — declared, not assumed.
  - kind: ledger.entry.post
    pattern: 'svc.*.ledger.{account}.entry.post'
    delivery: exclusive
    description: Post a ledger entry
    schema:
      type: object
      required: [Amount]
      properties:
        Amount: {type: number}

outputs:
  - kind: ledger.entry.posted
    pattern: 'svc.*.ledger.{account}.entry.posted'
    schema:
      type: object
      properties:
        EntryID: {type: string}

state_events:
  - kind: ledger.entry.posted
    pattern: 'svc.*.ledger.{account}.entry.posted'
  - kind: ledger.adjustment.applied
    pattern: 'svc.*.ledger.{account}.adjustment.applied'

state:
  type: object
  properties:
    Balance: {type: number}
    LastUpdated: {type: string, format: date-time}
```

Note the pattern on `ledger.entry.post`: it is the same string billing writes
on its output. That agreement is what `kind-pattern-conflict` enforces, and it
is the only thing that makes the two declarations describe one wire.

The read-set is the input plus the two state events, and all three are keyed by
`{account}`, so they feed one state.

**analytics/.arch/events.yaml** (a second, independent observer)

```yaml
version: 2
component: analytics
owns: analytics
description: Read models over the event log

state_events:
  - kind: billing.invoice.issued
    pattern: 'svc.*.billing.{account}.invoice.issued'

state:
  type: object
  properties:
    Count: {type: integer}
    Total: {type: number}
```

`billing.invoice.issued` is now folded by `analytics` **and** by `billing`
itself. That is two independent observations of one appended event, and
validation says nothing about it — no ambiguity, no ordering claim.

**gateway/.arch/events.yaml** (optional, completes the graph)

```yaml
version: 2
component: gateway
description: API gateway - orchestrates without owning any namespace

inputs:
  - kind: ledger.entry.posted
    pattern: 'svc.*.ledger.{account}.entry.posted'
    description: Observe ledger events for metrics

outputs:
  - kind: billing.invoice.issue
    pattern: 'svc.*.billing.{account}.invoice.issue'
    description: Trigger invoice creation on behalf of API clients
```

**Validation output (clean)**

```
$ wyrd plugin events validate --root ./example
OK: 4 component(s) validated.
```

### Broken variant

**bad/.arch/events.yaml**

```yaml
version: 2
component: bad
owns: bad
description: Component with deliberate violations

outputs:
  # Declares an exclusive contract nothing satisfies.
  - kind: bad.command.run
    pattern: 'svc.*.bad.{tenant}.command.run'
    delivery: exclusive

state_events:
  # Two entries, two different partition keys.
  - kind: bad.command.started
    pattern: 'svc.*.bad.{tenant}.command.started'
  - kind: bad.command.finished
    pattern: 'svc.*.bad.{region}.command.finished'

state:
  type: object
```

**Validation output**

```
$ wyrd plugin events validate --root ./bad
ERROR [exclusive-unhandled] bad:bad.command.run: outputs "bad.command.run" declared delivery: exclusive but no component takes it as an input
ERROR [partition-mismatch] bad:svc.*.bad.{region}.command.finished: subject "svc.*.bad.{region}.command.finished" extracts partition key [region] but the component's read-set is keyed by [tenant]; one component holds one state, so every partitioned subject it reads must address it identically
WARN [underspecified-state] bad:state: state is an object with no properties and no $ref; declare the projection shape, reference a type, or drop the field

2 error(s), 1 warning(s)
Error: validation failed
```

Note what is *not* reported: `bad` appends into its own namespace and folds its
own command, and nothing complains about who is allowed to do what.

## Adoption Workflow

1. **Start with the owner.** Pick the component that defines the most event
   schemas. Declare its `owns`, inputs, outputs and state events. Run
   `wyrd plugin events validate --root ./path`. Expect warnings for starved
   inputs and orphan outputs; errors mean structural mistakes.

2. **Add producers.** Declare components that append into the namespace. They do
   not need to own it. Validate after each addition.

3. **Add observers.** Declare components triggered by, or folding, those events.
   Orphan-event warnings should decrease. Adding a second or third observer of
   the same kind is expected and produces no findings.

4. **Agree on addresses.** If validation reports `kind-pattern-conflict`, do not
   patch it by editing one side at random — decide what the subject is, and if
   the two really are different routes, they are different kinds.

5. **Keep triggers out of your own outputs.** A `self-input-conflict` means the
   entry belongs in `state_events`; moving it is the whole fix.

6. **Mark the real commands.** Where a kind genuinely must have exactly one
   handler, add `delivery: exclusive` on the handler's input. Everything else
   stays broadcast.

7. **Close the graph.** Continue until warnings are intentional (external entry
   points with no internal producer, events published for external consumers).
   Use the Mermaid output to visualize flow.

8. **Iterate.** As the system evolves, re-run validation. New declarations that
   break closure, partition coherence or an exclusive contract surface
   immediately.
