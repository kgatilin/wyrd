# Event Model: Usage Guide

Declarative event-driven architecture for any project. Each component declares
its event interface as data in `.arch/events.yaml`; archai validates the
composed set and projects it to a graph for visualization and analysis.

Use this feature when your system is event-driven (pub/sub, CQRS, event
sourcing) and you want machine-readable documentation of what each component
produces, consumes, and projects over. The declarations enable composition
checks (ownership, closure, call resolution) and graph-based analysis before
runtime.

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
owns: billing                # optional; namespace prefix this component owns
description: Invoice lifecycle management  # optional

receives:
  - kind: billing.invoice.issue    # required; the event kind name
    role: action                   # required; "action" or "fact"
    description: Create an invoice # optional
    exposure: [public_api]         # optional; free-form tags
    schema:                        # optional; JSON Schema in YAML syntax
      type: object
      properties:
        Account: {type: string}

emits:
  - kind: billing.invoice.issued
    role: fact
    schema: {$ref: '#/vocab/Invoice'}

folds:
  - name: billing.open-invoices  # required
    pattern: billing.invoice.>   # required; pattern matched against emitted kinds
    state:                       # optional; JSON Schema for projection state
      type: object

vocab:                           # optional; component-local shared schema shapes
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
| `owns` | string | no | Namespace prefix this component owns (e.g., `billing` owns `billing.*`). |
| `description` | string | no | Human-readable summary. |
| `receives` | list of Slot | no | Event kinds this component handles. |
| `emits` | list of Slot | no | Event kinds this component produces. |
| `folds` | list of Fold | no | Projections maintained over event patterns. |
| `vocab` | map[string]Schema | no | Reusable schema shapes within this component. |
| `extra` | map[string]any | no | Opaque data passed through to templates; archai never interprets it. |

**Slot fields** (receives/emits entries):

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `kind` | string | yes | Event kind name (e.g., `billing.invoice.issued`). |
| `role` | string | yes | `"action"` (command) or `"fact"` (event record). |
| `description` | string | no | Human-readable summary. |
| `exposure` | list of string | no | Free-form tags (e.g., `["public_api"]`). |
| `schema` | Schema | no | Payload schema. |
| `extra` | map[string]any | no | Opaque passthrough. |

**Fold fields**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Fold identifier (e.g., `billing.open-invoices`). |
| `pattern` | string | yes | Subject pattern matched against emitted kinds. |
| `state` | Schema | no | Schema for the projection state. |
| `extra` | map[string]any | no | Opaque passthrough. |

**Strict decoding:** Unknown YAML keys cause a parse error. Typos like
`componet:` or `recives:` are caught immediately.

## Schemas

Schemas are JSON Schema written in YAML syntax. They are stored opaquely and
used for `$ref` resolution and deprecated-field detection. archai does NOT
validate payloads against schemas or check schema compatibility between
callers and receivers.

### `$ref` resolution

Two forms are supported:

- **Local**: `{$ref: '#/vocab/Name'}` references a key in the same
  component's `vocab` block.
- **Cross-component**: `{$ref: 'other-component#/vocab/Name'}` references a
  key in another component's `vocab`. The target component must exist.

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

## Pattern Matching

Fold patterns use a minimal dot-segmented glob syntax:

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

## Validation Rules

### Error severity

- **Errors** are fatal rule breaches; exit code 1.
- **Warnings** are potential issues that may be acceptable; exit code 0.

### Role x Ownership Matrix

| | kind in `owns` | kind outside `owns` |
|---|---|---|
| **emit fact** | ok | `ownership-violation` (forging another namespace's fact) |
| **emit action** | ok (self-scheduling) | ok (call-out; must resolve to one receiver) |
| **receive action** | ok (inbound command) | `ownership-violation` (accepting commands in another namespace) |
| **receive fact** | ok (self-observation) | ok (normal subscription) |

A component without `owns` may only emit actions and receive facts.

### All finding kinds

| Finding Kind | Severity | Trigger | Fix |
|--------------|----------|---------|-----|
| `ownership-violation` | error | Emitting fact or receiving action outside owned namespace | Move the kind under your `owns` prefix, or change role |
| `duplicate-owner` | error | Two components declare overlapping `owns` (exact or nested) | Give each component a distinct namespace prefix |
| `unresolved-call` | error | Emitted action has zero receivers | Add a component that receives this action |
| `ambiguous-call` | error | Emitted action has multiple receivers | Remove duplicate receivers; only one component may handle an action |
| `unresolved-ref` | error | `$ref` points to nonexistent vocab entry | Fix the path or add the missing vocab entry |
| `ref-cycle` | error | Cross-component `$ref` forms a cycle | Break the cycle by inlining or restructuring |
| `starved-receive` | warning | Receives a kind no component emits | Add a producer or remove the unused receive |
| `starved-fold` | warning | Fold pattern matches no emitted kind | Fix the pattern or add emitters |
| `orphan-fact` | warning | Emitted fact has no consumer (no receive or fold match) | Add a consumer or remove the unused emit |

### What is NOT checked

- **Schema compatibility**: archai does not verify that a call-out's payload is
  compatible with the target's inbound schema. Schemas are recorded but not
  compared.
- **Runtime conformance**: archai does not observe actual event traffic.
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
ERROR [ownership-violation] billing:ledger.entry.post: receiving action "ledger.entry.post" outside owned namespace "billing"
ERROR [ownership-violation] billing:ledger.entry.posted: emitting fact "ledger.entry.posted" outside owned namespace "billing"
WARN [orphan-fact] billing:ledger.entry.posted: emits fact "ledger.entry.posted" but no component receives it
WARN [starved-receive] billing:ledger.entry.post: receives "ledger.entry.post" but no component emits it

2 error(s), 2 warning(s)
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

Solid arrows (`-->`) are facts; dashed arrows (`-.->`) are actions.

### MCP Tools

When running as an MCP server, three tools are exposed:

**event_model**

Arguments: none

Returns the composed model as text: components with their receives/emits/folds/vocab.

**event_kind**

Arguments: `{"kind": "billing.invoice.issued"}`

Returns detail for one event kind: role, owner, producers, consumers, payload
schema, deprecated fields.

**event_validate**

Arguments: none

Returns validation findings as text (same format as the CLI).

### HTTP

`GET /api/plugins/events/model` returns the composed model and projected graph
as JSON.

## Worked Example

A minimal two-component system: billing issues invoices and posts to the
ledger; the ledger records entries.

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
    description: Post a ledger entry for the invoice
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
  - kind: ledger.entry.post
    role: action
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
```

**gateway/.arch/events.yaml** (optional, completes the graph)

```yaml
version: 1
component: gateway
description: API gateway - orchestrates without owning events

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
OK: 3 component(s) validated.
```

### Broken variant

**bad/.arch/events.yaml**

```yaml
version: 1
component: bad
owns: bad
description: Component with deliberate violations

receives:
  - kind: ledger.entry.post
    role: action
    description: Receiving action outside owned namespace

emits:
  - kind: ledger.entry.posted
    role: fact
    description: Emitting fact outside owned namespace
```

**Validation output**

```
$ archai plugin events validate --root ./bad
ERROR [ownership-violation] bad:ledger.entry.post: receiving action "ledger.entry.post" outside owned namespace "bad"
ERROR [ownership-violation] bad:ledger.entry.posted: emitting fact "ledger.entry.posted" outside owned namespace "bad"
WARN [orphan-fact] bad:ledger.entry.posted: emits fact "ledger.entry.posted" but no component receives it
WARN [starved-receive] bad:ledger.entry.post: receives "ledger.entry.post" but no component emits it

2 error(s), 2 warning(s)
Error: validation failed
```

## Adoption Workflow

1. **Start with the owner.** Pick the component that owns the most events (the
   one whose namespace prefix others call into). Declare its `owns`, receives,
   and emits. Run `archai plugin events validate --root ./path`. Expect
   warnings for starved receives and orphan facts; errors mean structural
   mistakes.

2. **Add callers.** Declare components that emit actions into the owner's
   namespace. Each action must resolve to exactly one receiver. Validate after
   each addition.

3. **Add observers.** Declare components that receive facts from the owner.
   Orphan-fact warnings should decrease.

4. **Close the graph.** Continue until warnings are intentional (external
   entry points with no internal producer, facts published for external
   consumers). Use the Mermaid output to visualize flow.

5. **Iterate.** As the system evolves, re-run validation. New emits or receives
   that break ownership or closure rules surface immediately.

The graph output (`archai plugin events graph --format mermaid`) shows
components, event flow, and health status. Facts are solid arrows; actions are
dashed. Health issues (orphan, starved, ambiguous) appear as annotations.
