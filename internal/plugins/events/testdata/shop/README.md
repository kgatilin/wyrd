# shop — a worked event model

A storefront, invented whole, in both declaration formats at once. It exists to
be looked at: point the daemon at it and open **Events** in the app bar.

```
go run ./cmd/wyrd serve --root internal/plugins/events/testdata/shop --http 127.0.0.1:7900
```

Or read it without a browser:

```
go run ./cmd/wyrd plugin events graph    --root internal/plugins/events/testdata/shop
go run ./cmd/wyrd plugin events validate --root internal/plugins/events/testdata/shop
```

The `.go` files are placeholders. The daemon loads a Go model before it serves
anything, and a directory of YAML is not one.

## What is in it

| Component | Format | Why it is here |
|---|---|---|
| `storefront` | native | Appends commands into a namespace it does not own — legal, and the usual shape for an entry point |
| `orders` | AsyncAPI | The lifecycle: two commands, three events, two calls out, six observes |
| `payments` | AsyncAPI port | Two instances (`card`, `transfer`); one event only `card` has |
| `carrier` | AsyncAPI port | Two instances (`air`, `ground`) |
| `inventory` | native | The one genuine command, `delivery: exclusive` |
| `fulfilment` | native | Triggered by one event, appends a command, folds two outcomes |
| `notifications` | native | Two triggers, one output nobody folds |
| `analytics` | native | Folds four kinds, appends nothing |
| `pricing` | native | Wired to nothing at all |

## What it is meant to show

**Both formats compose.** `carrier` publishes AsyncAPI; `fulfilment`,
`notifications` and `analytics` fold its events from `events.yaml`. One graph.

**A port is one node.** `payments` draws once and names its instances.
`payments.event.declined` carries `x-eventlog.instances: [card]`, which the
detail rail shows as `only card`.

**A caller binds what the owner leaves open.** `orders` calls
`payments.card.{order}.command.authorize` while the port declares
`payments.{method}.{order}.command.authorize`. One kind, two addresses, and not
a conflict.

**The tenant and the port parameter key nothing.** Every address here starts
`shop.{tenant}.`, and the imported declarations still key their fold on
`[order]` alone, because the documents say so.

**A native subscriber wildcards the port parameter.** `analytics` folds
`shop.*.carrier.*.{order}.event.delivered` rather than naming `{lane}` — the
native format derives its key from every slot it sees, and the lane is not one.

**Validation is honest about what it read.** Three warnings, all deliberate:
`notifications.event.sent` leaves for an external provider, and `pricing` is
scaffolding nobody has wired up. Nothing complains about the imported
declarations, and nothing wired to them comes back starved.
