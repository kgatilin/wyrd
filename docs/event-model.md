# Event Model: Usage Guide

Declarative event-driven architecture for any project. Each component declares
its event interface as data in `.arch/events.yaml`; archai validates the
composed set and projects it to a graph for visualization and analysis.

Use this feature when your system is event-driven (pub/sub, CQRS, event
sourcing) and you want machine-readable documentation of what each component
produces, observes, and projects over. The declarations enable composition
checks (namespace ownership, closure, fold coherence) and graph-based analysis
before runtime.

## The core rule

> A durable event is published once and may be observed independently by any
> number of components and folds.

Everything else follows from this:

- **`emits`** — the component appends a durable event to the log.
- **`receives`** — the component observes *another* component's event. There
  may be 0..N observers of one kind, and they are independent of each other.
- **`folds[].consumes`** — *stateful* observation. Several folds, in the same
  or different components, may fold the same kind; their ordering and
  completion relative to one another is not guaranteed.
- **`role: action | fact`** — a *semantic* classification of the event.
  It is not a delivery contract: an action is not an RPC and does not require
  a handler, let alone exactly one. But it *is* a global property of the kind —
  see below.
- **`owns`** — authority over a namespace's *schema definitions*. It is not an
  exclusive right to produce events in that namespace, nor to observe them.

One appended event can drive several reactions at once, and nothing in the base
model treats that as ambiguity:

```
event appended
  ├─ controller A folds it → emits event X
  ├─ controller B folds it → emits event Y
  └─ projection C folds it → updates a read model
```

## Role is a property of the kind

One kind has exactly one role across the whole composed set. Role says what the
event *is* — an expressed intent or a recorded outcome — and one event cannot be
both.

- Different producers and observers **cannot** declare different roles for the
  same kind. Disagreement is a `kind-role-conflict` **error**.
- **Payload variants never change the role.** A `oneOf`, a legacy branch marked
  `deprecated: true`, an extra field — that is schema evolution. The kind still
  is what it was.
- Where a name would need both readings, that is **two kinds**, not one kind read
  two ways:

```yaml
# WRONG — one kind, two roles. kind-role-conflict.
receives:
  - kind: llm.message
    role: action        # "send this message"
emits:
  - kind: llm.message
    role: fact          # "a message happened"

# RIGHT — the intent and the outcome are separate events.
receives:
  - kind: llm.message.send
    role: action
emits:
  - kind: llm.message.sent
    role: fact
```

Folds subscribe by glob, so a split costs nothing on the consuming side:
`consumes: [llm.message.*]` still folds both.

The alternative — deciding the whole kind is a `fact` and letting intent live in
the payload — is legal too, and sometimes right when the "action" reading was
only ever a projection of who happened to be reading the log. It is a modelling
choice; what is not available is one kind carrying both roles at once.

## A component does not subscribe to itself

`receives` and `emits` are the component's ports: inputs and outputs. Routing
your own output back into your own input describes nothing — it is a loop
through the boundary that exists to separate you from everyone else, and it
draws a self-edge in the graph with no runtime referent. Declaring the same kind
in both is a `self-receive-conflict` **error**.

Folding your own events is a different thing entirely and is fully allowed: a
fold is not a port, it is state maintained over the log.

```yaml
# WRONG — self-receive-conflict.
emits:
  - kind: llm.failed
    role: fact
receives:
  - kind: llm.failed
    role: fact

# RIGHT — a fold is not an input port; it is state over the log, and it comes
# with a state schema and a partition key attached.
emits:
  - kind: llm.failed
    role: fact
folds:
  - name: llm.failures
    subjects: ['svc.*.llm.{session}.>']
    consumes: [llm.failed]
    state:
      type: object
      properties:
        Count: {type: integer}
```

The rule is narrow. It does not restrict `folds[].consumes` over the
component's own kinds, other components receiving the kind (any number of
them), or the same component emitting `X` and receiving a different kind `Y`.
Matching is on the exact kind; once the model becomes subject-aware the
comparison becomes `(kind, route)`, and today's check is the single-route case.

**Exclusive handling is opt-in.** If a kind really is a command with exactly
one owner, say so with `delivery: exclusive` on the slot. That — and only that
— makes receiver cardinality a validated rule. Exclusivity is a transport /
runtime policy, not a property of event sourcing, so it is never the default.

## File Location and Discovery

Place `.arch/events.yaml` in each component's directory. archai walks the tree
from the specified root, entering `.arch/` directories and reading
`events.yaml` when present.

**Skipped directories** (below the root, not at the root itself):
`.git`, `.worktrees`, `.claude`, `bin`, `vendor`, `node_modules`, `testdata`.

**Explicit-root exception:** If the root path itself has a skipped name (e.g.,
`--root /path/to/testdata`), archai honors the explicit instruction and scans
it anyway; the skip list only filters subdirectories.

The component id must be unique across the entire scanned tree. Duplicate ids
cause a parse error.

## Format Reference

```yaml
version: 1                   # required; currently only version 1 is supported
component: billing           # required; stable unique id for this component
owns: billing                # optional; namespace whose schemas this component defines
description: Invoice lifecycle management  # optional

receives:
  - kind: billing.invoice.issue    # required; the event kind name
    role: action                   # required; "action" or "fact" (semantic only)
    delivery: exclusive            # optional; "broadcast" (default) or "exclusive"
    description: Create an invoice # optional
    exposure: [public_api]         # optional; free-form tags
    schema:                        # optional; JSON Schema in YAML syntax
      type: object
      properties:
        Account: {type: string}

emits:
  - kind: billing.invoice.issued
    role: fact
    schema: {$ref: '#/types/Invoice'}

folds:
  - name: billing.open-invoices     # required
    subjects:                       # optional; transport read-set (see below)
      - svc.*.billing.{account}.invoice.>
      - svc.*.billing.{account}.credit.>
    consumes: [billing.invoice.*]   # required; kinds the reducer folds (globs)
    state:                          # required; JSON Schema or $ref
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
| `version` | int | yes | Schema version. Must be `1`. |
| `component` | string | yes | Stable component id, unique in the repo. |
| `owns` | string | no | Namespace whose schemas this component defines (e.g., `billing` defines `billing.*`). Not a production or observation restriction. |
| `description` | string | no | Human-readable summary. |
| `receives` | list of Slot | no | Event kinds this component observes. |
| `emits` | list of Slot | no | Event kinds this component appends. |
| `folds` | list of Fold | no | Projections maintained over event patterns. |
| `types` | map[string]Schema | no | Reusable schema definitions, the analogue of JSON Schema `$defs`. |
| `extra` | map[string]any | no | Opaque data passed through to templates; archai never interprets it. |

**Slot fields** (receives/emits entries):

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `kind` | string | yes | Event kind name (e.g., `billing.invoice.issued`). |
| `role` | string | yes | `"action"` (expresses intent) or `"fact"` (records what happened). Classification only — no cardinality implied — but global to the kind: every declaration must agree. |
| `delivery` | string | no | `"broadcast"` (default, 0..N observers) or `"exclusive"` (opt into exactly-one-receiver validation). |
| `description` | string | no | Human-readable summary. |
| `exposure` | list of string | no | Free-form tags (e.g., `["public_api"]`). |
| `schema` | Schema | no | Payload schema. |
| `extra` | map[string]any | no | Opaque passthrough. |

**Fold fields**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Fold identifier (e.g., `billing.open-invoices`). |
| `subjects` | list of string | no | Transport subject patterns (e.g., `svc.*.billing.{account}.>`). All entries must extract the same ordered partition key. Opaque to kind matching; carried for codegen. |
| `consumes` | list of string | yes | Kind globs the reducer folds. Starvation checked per entry. |
| `state` | Schema | yes | Schema for the projection state — a full schema or a `$ref` to a type. |
| `extra` | map[string]any | no | Opaque passthrough. |

### Subjects vs Consumes

A fold declares two distinct things in different alphabets:

- **`subjects`** is the *transport read-set*: NATS-style patterns with `{slot}`
  tokens that declare the partition key layout ("one state per account") and
  wire subscriptions. archai validates `{slot}` syntax but does NOT match these
  against kinds.

- **`consumes`** lists the *kinds the reducer actually folds*. These are kind
  globs; the pattern matching below applies. Starvation is checked here, not on
  the subjects.

The distinction matters: a subject pattern may deliver events the reducer
ignores (wrong kind in the namespace), and a consumed kind may be emitted onto
subjects the fold does not subscribe to. Conflating them produces false
starved-fold warnings.

### One fold, one partition key

`subjects` is a list because a fold may need to read several transport streams
into one state. All of them must extract the **same ordered partition key**: a
fold instance holds exactly one state, and every subject it reads must identify
that state identically.

```yaml
# OK — both subjects address per-account state.
subjects:
  - svc.*.billing.{account}.invoice.>
  - svc.*.billing.{account}.credit.>

# ERROR (partition-mismatch) — which state does an event belong to?
subjects:
  - svc.*.billing.{account}.invoice.>
  - svc.*.billing.{region}.invoice.>

# ERROR (partition-mismatch) — order is significant, [region sku] != [sku region].
subjects:
  - svc.*.warehouse.{region}.{sku}.stock.>
  - svc.*.warehouse.{sku}.{region}.stock.>
```

**Strict decoding:** Unknown YAML keys cause a parse error. Typos like
`componet:` or `recives:` are caught immediately.

## Schemas

Schemas are JSON Schema written in YAML syntax. They are stored opaquely and
used for `$ref` resolution and deprecated-field detection. archai does NOT
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

## Pattern Matching (consumes)

The `consumes` entries use a minimal dot-segmented glob syntax:

| Token | Meaning |
|-------|---------|
| literal | Exact segment match |
| `*` | Exactly one segment |
| `>` or `**` | One or more trailing segments (must be last) |

Examples:
- `billing.invoice.issued` matches only that exact kind
- `billing.*` matches `billing.invoice`, `billing.credit`, but not `billing` or `billing.invoice.issued`
- `billing.>` matches `billing.invoice`, `billing.invoice.issued`, but not `billing`
- `*.invoice.*` matches `billing.invoice.issued`, `sales.invoice.voided`

Folds match against **every** emitted kind, actions included: an action is a
durable event like any other.

## Subject Slot Syntax

The `subjects` entries use NATS-style patterns with `{slot}` tokens to declare
partition keys. archai validates only the `{slot}` syntax:

- `{slot}` tokens must be balanced (matching `{` and `}`)
- `{slot}` tokens must be non-empty (no `{}`)
- Nested braces are not allowed

Examples:
- `svc.*.billing.{account}.invoice.>` — one slot, partition per account
- `svc.*.warehouse.{region}.{location}.stock.{sku}.>` — three slots

Malformed `{slot}` syntax produces a `malformed-slot` error.

Note: in YAML, a value containing `{` must not be written in flow style
(`[svc.{a}.>]` is a parse error). Use a block sequence or quote the entry.

## Validation Rules

### Error severity

- **Errors** are fatal rule breaches; exit code 1.
- **Warnings** are potential issues that may be acceptable; exit code 0.

### What ownership does and does not mean

`owns` declares the component that **defines the schemas** for a namespace.
That is all. In particular:

| Situation | Verdict |
|---|---|
| emit a fact in a namespace you do not own | legal |
| emit an action in a namespace you do not own | legal |
| observe an action in a namespace you do not own | legal |
| observe a fact in a namespace you do not own | legal |
| a component with no `owns` at all | legal, in every role |
| two components claiming overlapping `owns` | **error** (`duplicate-owner`) |

Two claimants over one namespace mean two answers to "what does this kind look
like", so overlapping ownership — exact or nested — stays an error. Ownership
is resolved by longest prefix, and reported by `event_kind` as the kind's
schema owner.

### All finding kinds

| Finding Kind | Severity | Trigger | Fix |
|--------------|----------|---------|-----|
| `duplicate-owner` | error | Two components declare overlapping `owns` (exact or nested) | Give each component a distinct namespace prefix |
| `kind-role-conflict` | error | One kind is declared with more than one role across the composed set | Split the intent and the outcome into separate kinds, or settle on one role |
| `self-receive-conflict` | error | One component declares the same kind in both `emits` and `receives` | Drop the receives slot; use `folds[].consumes` to carry your own events into state |
| `partition-mismatch` | error | A fold's `subjects` extract different ordered partition keys | Make every subject address the same state, or split into separate folds |
| `malformed-slot` | error | A fold subject has invalid `{slot}` syntax | Fix the `{slot}` tokens (balance braces, non-empty) |
| `unresolved-ref` | error | `$ref` points to a nonexistent `types` entry | Fix the path or add the missing type |
| `ref-cycle` | error | Cross-component `$ref` forms a cycle | Break the cycle by inlining or restructuring |
| `exclusive-unhandled` | error | A kind declared `delivery: exclusive` has no receiver | Add the handler, or drop the exclusive declaration |
| `exclusive-conflict` | error | A kind declared `delivery: exclusive` has more than one receiver | Remove the extra handlers, or drop the exclusive declaration |
| `starved-receive` | warning | Observes a kind no component emits | Add a producer or remove the unused receive |
| `starved-fold` | warning | A `consumes` entry matches no emitted kind | Fix the consumes entry or add emitters |
| `orphan-event` | warning | An emitted event (either role) has no observer — no receive, no fold | Add an observer or remove the unused emit |
| `underspecified-state` | warning | A fold state is `type: object` with no properties and no `$ref` | Declare the projection shape or reference a type |

### Removed rules

Two rules from the first iteration were RPC assumptions and no longer exist:

- **`unresolved-call`** — "an emitted action must have a receiver". Replaced by
  the `orphan-event` warning, which applies to both roles, and by
  `exclusive-unhandled` when the kind opts into single-handler delivery.
- **`ambiguous-call`** — "an emitted action must have exactly one receiver".
  Multiple independent observers are the normal case. Replaced by
  `exclusive-conflict`, gated on `delivery: exclusive`.
- **`ownership-violation`** — the role × ownership matrix. Ownership no longer
  restricts who may produce or observe.

### What is NOT checked

- **Schema compatibility**: archai does not verify that an emitted payload is
  compatible with an observer's inbound schema. Schemas are recorded but not
  compared.
- **Runtime conformance**: archai does not observe actual event traffic.
- **Ordering**: nothing here constrains the order in which independent
  observers of one event complete, because nothing can.
- **Project-specific policy**: custom rules over the event graph are not yet
  implemented.

## Commands

### CLI

Commands live under `archai plugin events`.

**validate**

```
archai plugin events validate [--root PATH]
```

Validates all `.arch/events.yaml` declarations under root. Prints findings and
a summary. Exits 0 if no errors (warnings permitted), 1 if errors exist.

```
$ archai plugin events validate --root ./services
OK: 5 component(s) validated.
```

```
$ archai plugin events validate --root ./broken
ERROR [exclusive-unhandled] billing:ledger.entry.post: emits "ledger.entry.post" declared delivery: exclusive but no component receives it
ERROR [partition-mismatch] billing:billing.mixed: fold "billing.mixed" subject "svc.*.billing.{region}.invoice.>" extracts partition key [region] but "svc.*.billing.{account}.invoice.>" extracts [account]; all subjects of one fold must extract the same ordered key
WARN [orphan-event] billing:billing.invoice.issued: emits fact "billing.invoice.issued" but no component receives or folds it

2 error(s), 1 warning(s)
Error: validation failed
```

**graph**

```
archai plugin events graph [--root PATH] [-f FORMAT] [-o FILE]
```

Generates a graph of the event model. Supported formats:
- `mermaid` (default): Mermaid flowchart diagram
- `graphml`: GraphML XML for analysis tools

```
$ archai plugin events graph --root ./services --format mermaid
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
    billing -->|invoice.issued| ledger
    billing -.->|entry.post| ledger
    gateway -.->|invoice.issue| billing
    ledger -->|entry.posted| gateway
```

Solid arrows (`-->`) are facts; dashed arrows (`-.->`) are actions. The arrow
style reflects the semantic role only — a dashed edge is still a broadcast
unless the kind declares `delivery: exclusive`.

### MCP Tools

When running as an MCP server, three tools are exposed:

**event_model**

Arguments: none

Returns the composed model as text: components with their
receives/emits/folds/types.

**event_kind**

Arguments: `{"kind": "billing.invoice.issued"}`

Returns detail for one event kind: role, delivery policy, schema owner,
producers, observers (receives and folds), payload schema, deprecated fields.

**event_validate**

Arguments: none

Returns validation findings as text (same format as the CLI).

**gen**

```
archai plugin events gen [--root PATH] [--templates DIR] [--out DIR]
                         [--component ID] [--dry-run] [--force] [--no-format]
```

Renders each component's declaration through project-supplied templates. See
[Codegen](#codegen) below.

### HTTP

`GET /api/plugins/events/model` returns the composed model and projected graph
as JSON.

## Graph Projection

The model projects to a bipartite graph:

- nodes: `component:<id>`, `kind:<name>`, `fold:<component>.<name>`,
  `type:<component>.<typeName>`
- edges: `emits`, `receives`, `feeds` (kind → fold), `held-by` (fold →
  component), `defines` (component → type), `payload` (kind → type), `refs`
  (type → type)
- kind attributes: `producer_count`, `consumer_count`, `fold_consumer_count`,
  `health` (`ok` | `orphan` | `starved` | `ambiguous`), `role`, `delivery`, and
  `role_conflict: true` when declarations disagree (the reported role is then
  the first in deterministic order)
- fold attributes: `subjects`, `partition_key`, `partition_arity`, `consumes`,
  `component`

`health: ambiguous` is reserved for exclusive kinds with more than one
receiver. A broadcast kind with many receivers is `ok`.

## Codegen

archai owns the declaration format, its validation and its graph. **The project
owns the binding.** Templates live in the project, archai never learns the
project's types, and nothing archai produces is a runtime dependency of the
generated code.

There is deliberately **no `--lang` flag**. A language generator built into
archai would invert that split: archai would have to know your types, and would
become a dependency of your build. Instead it renders your template against a
stable data model.

### Running it

```
archai plugin events gen [--root PATH] [--templates DIR] [--out DIR]
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
  code that is wrong in ways the compiler cannot catch — colliding constants
  from a `kind-role-conflict`, a subscription keyed on the wrong slot from a
  `partition-mismatch`. `--force` overrides.
- Generated `.go` files are run through gofmt. This is syntactic only — it is
  not archai learning your types — and it exists because generated files are
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
| `.Receives` `.Emits` | []Slot | In declaration order |
| `.Folds` | []Fold | In declaration order |
| `.Types` | []Type | `{Name, Schema}`, sorted by name |
| `.ForeignEmits` `.ForeignReceives` | []Slot | The subsets whose kind falls outside `.Owns` — the component's cross-namespace coupling |
| `.Kinds` | []string | Sorted unique kinds across both ports; the natural input for a constant block |

**Slot**: `.Kind`, `.Role`, `.Delivery`, `.Description`, `.Exposure`,
`.Schema`, `.Extra`, and `.Exclusive` (a method, so `{{if .Exclusive}}` works).
`.Delivery` is normalized — it is `"broadcast"` when the declaration omitted it,
so templates never special-case the empty string.

**Fold**: `.Name`, `.Subjects`, `.PartitionKey`, `.Consumes`, `.State`,
`.Extra`.

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
is not refactorable, so never derive a wire name from an identifier.

**Missing keys are errors.** Templates run with `missingkey=error`: reaching for
`.Extra.whatever` when the declaration never set it fails at generation time
rather than silently emitting an empty string. Use `index` as the presence test:

```
{{ with index .Extra "go_package" }}{{ . }}{{ else }}{{ unexported $.Component }}{{ end }}
```

### Keeping it honest

Generated files are committed. Wire it up with
`//go:generate archai plugin events gen` plus `go generate ./... && git diff
--exit-code` in CI, and drift becomes a build failure rather than a discovery.

## Worked Example

A minimal system: billing issues invoices and asks the ledger to post entries;
the ledger records them; two independent observers watch the same fact.

**billing/.arch/events.yaml**

```yaml
version: 1
component: billing
owns: billing
description: Invoice lifecycle management

receives:
  - kind: billing.invoice.issue
    role: action
    description: Create an invoice
    exposure: [public_api]
    schema:
      type: object
      required: [Account]
      properties:
        Account: {type: string}

emits:
  - kind: billing.invoice.issued
    role: fact
    schema:
      type: object
      properties:
        InvoiceID: {type: string}
        Account: {type: string}
  - kind: ledger.entry.post
    role: action
    description: >-
      Emitted into a namespace billing does not own. Legal: ownership is schema
      authority, not an exclusive right to produce.
    schema:
      type: object
      properties:
        Amount: {type: number}
```

**ledger/.arch/events.yaml**

```yaml
version: 1
component: ledger
owns: ledger
description: Double-entry accounting ledger

receives:
  # A genuine command with one owner — declared, not assumed.
  - kind: ledger.entry.post
    role: action
    delivery: exclusive
    description: Post a ledger entry
    schema:
      type: object
      required: [Amount]
      properties:
        Amount: {type: number}
  - kind: billing.invoice.issued
    role: fact
    description: Observe invoice issuance for audit

emits:
  - kind: ledger.entry.posted
    role: fact
    schema:
      type: object
      properties:
        EntryID: {type: string}

folds:
  - name: ledger.account-balances
    subjects:
      - svc.*.ledger.{account}.entry.>
      - svc.*.ledger.{account}.adjustment.>
    consumes: [ledger.entry.*]
    state:
      type: object
      properties:
        Balance: {type: number}
        LastUpdated: {type: string, format: date-time}
```

The fold consumes the same kind (`ledger.entry.posted`) that this component
emits. A component folding its own facts is the common case — the fold
maintains projection state from the events the component produces. Its two
subjects share the `{account}` partition key, so both feed one state.

**analytics/.arch/events.yaml** (a second, independent observer)

```yaml
version: 1
component: analytics
owns: analytics
description: Read models over the event log

folds:
  - name: analytics.invoice-volume
    subjects:
      - svc.*.analytics.{tenant}.billing.>
    consumes: [billing.invoice.*]
    state:
      type: object
      properties:
        Count: {type: integer}
        Total: {type: number}
```

`billing.invoice.issued` is now folded by `analytics.invoice-volume` **and**
received by `ledger`. That is two independent observations of one appended
event, and validation says nothing about it — no ambiguity, no ordering claim.

**gateway/.arch/events.yaml** (optional, completes the graph)

```yaml
version: 1
component: gateway
description: API gateway - orchestrates without owning any namespace

receives:
  - kind: ledger.entry.posted
    role: fact
    description: Observe ledger events for metrics

emits:
  - kind: billing.invoice.issue
    role: action
    description: Trigger invoice creation on behalf of API clients
```

**Validation output (clean)**

```
$ archai plugin events validate --root ./example
OK: 4 component(s) validated.
```

### Broken variant

**bad/.arch/events.yaml**

```yaml
version: 1
component: bad
owns: bad
description: Component with deliberate violations

emits:
  # Declares an exclusive contract nothing satisfies.
  - kind: bad.command.run
    role: action
    delivery: exclusive

folds:
  # Two subjects, two different partition keys.
  - name: bad.mixed
    subjects:
      - svc.*.bad.{tenant}.events.>
      - svc.*.bad.{region}.events.>
    consumes: [bad.command.*]
    state:
      type: object
```

**Validation output**

```
$ archai plugin events validate --root ./bad
ERROR [exclusive-unhandled] bad:bad.command.run: emits "bad.command.run" declared delivery: exclusive but no component receives it
ERROR [partition-mismatch] bad:bad.mixed: fold "bad.mixed" subject "svc.*.bad.{region}.events.>" extracts partition key [region] but "svc.*.bad.{tenant}.events.>" extracts [tenant]; all subjects of one fold must extract the same ordered key
WARN [underspecified-state] bad:bad.mixed: fold "bad.mixed" declares an object state with no properties and no $ref; declare the projection shape or reference a type

2 error(s), 1 warning(s)
Error: validation failed
```

Note what is *not* reported: `bad` emits into its own namespace and folds its
own command, and nothing complains about who is allowed to do what.

## Adoption Workflow

1. **Start with the owner.** Pick the component that defines the most event
   schemas. Declare its `owns`, receives, emits and folds. Run
   `archai plugin events validate --root ./path`. Expect warnings for starved
   receives and orphan events; errors mean structural mistakes.

2. **Add producers.** Declare components that emit into the namespace. They do
   not need to own it. Validate after each addition.

3. **Add observers.** Declare components that receive or fold those events.
   Orphan-event warnings should decrease. Adding a second or third observer of
   the same kind is expected and produces no findings.

4. **Keep roles global.** If validation reports `kind-role-conflict`, do not
   patch it by flipping one side — decide whether the kind is an intent or an
   outcome, and split it if it is genuinely both.

5. **Mark the real commands.** Where a kind genuinely must have exactly one
   handler, add `delivery: exclusive` on the handler's slot. Everything else
   stays broadcast.

6. **Close the graph.** Continue until warnings are intentional (external entry
   points with no internal producer, events published for external consumers).
   Use the Mermaid output to visualize flow.

7. **Iterate.** As the system evolves, re-run validation. New emits, receives
   or folds that break closure, fold coherence or an exclusive contract surface
   immediately.
