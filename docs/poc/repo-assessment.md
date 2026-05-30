# Repo Assessment — How well does `archai` fit the concept?

> Verdict up front: **archai is already ~80% of the engine the concept describes.**
> The pipeline `code → language-agnostic model → versioned YAML → target → diff` is
> built and tested today. What is missing is (a) a projection shaped for the *review
> UI* and (b) the UI itself. The POC is therefore mostly **a new front-end on an
> existing engine**, not a new engine.

## Concept → archai capability map

| Concept element | In archai today? | Where |
|---|---|---|
| Language-agnostic domain model | ✅ Yes | `internal/domain/` — `PackageModel`, `InterfaceDef`, `StructDef`, `FunctionDef`, `MethodDef`, `FieldDef`, `Dependency`, `Stereotype`. No language code in domain. |
| Pluggable language modules behind a common interface | ✅ Yes | `service.ModelReader` (`Read(ctx, paths) → []PackageModel`). Go + Java readers exist; `service.WithLanguageReader(name, reader, match)` adds more. |
| Component / interface / member / relationship | ✅ Yes | Package ≈ component; interfaces+structs ≈ internals; methods+fields ≈ members; `Dependency` ≈ edges. |
| Bounded contexts / aggregates grouping | ✅ Yes | `overlay.Config.BoundedContexts`, `.Aggregates`, `.Layers`. |
| Projection to versionable YAML | ✅ Yes | `internal/adapter/yaml` (schema `archai/v1`), reader+writer round-trip. `archai diagram generate --format yaml`. |
| Projection to diagram | ✅ Yes | `internal/adapter/d2` (D2 + SVG render). |
| Target architecture (frozen snapshot) | ✅ Yes | `internal/target` — `.arch/targets/<id>/`, `archai target lock/list/show/use/delete`. |
| Architecture diff (target ↔ current) | ✅ Yes | `internal/diff` — `Compute(current, target) → []Change{Op, Kind, Path, Before, After}`. `archai diff [--format json\|yaml]`. |
| Apply / validate diff (CI) | ✅ Yes | `internal/apply`, `archai validate`. |
| Graph read model | ✅ Yes | `internal/domain/archgraph` — nodes/edges with stable IDs, round-trips with `PackageModel`. |
| HTTP daemon + JSON API | ✅ Yes | `internal/serve` + `internal/adapter/http`; `archai serve --http`; `/api/*` incl. MCP-over-HTTP. |
| MCP transport (talk to agents) | ✅ Yes | `internal/adapter/mcp` (stdio + over-HTTP). Directly relevant to the "AI-agent" framing. |
| Plugin system incl. UI registry | ✅ Yes | `internal/plugin` — CLI/MCP/HTTP/UI capabilities, `UIRegistry`. |
| An *existing* web UI | ✅ Yes (but different design) | `internal/adapter/http/templates` + assets (htmx + cytoscape/ELK/dagre). Functional, not the hi-fi mockup. |

## What is missing (the POC's actual work)

1. **A projection shaped for the review UI.** The hi-fi mockup consumes a specific
   JSON shape (bounded contexts → components → internals → members + ports; edges;
   per-element diff flags; PR meta). archai has every *fact* needed but emits it as
   `PackageModel`/`Diff`, not as this UI graph. We need a thin projection
   `model + overlay + diff → UIGraph JSON`.

2. **The hi-fi review UI itself.** The existing htmx UI does not match the mockup
   (dark IDE, dot-grid canvas, expandable mini-canvas components, focus mode,
   animated edges, inline click-to-comment with numbered pins, CHANGES↔CONTEXTS
   left panel). This is the front-end we build.

3. **Layout.** The UI graph the mockup uses carries coordinates (`x/y/w/h/wx/hx`).
   archai does not compute geometry. The UI must run a layout pass to place
   components, internals, and ports. (archai already vendors ELK/dagre for its own
   UI, so the approach is proven; the new app will do layout client-side.)

4. **PR framing.** The mockup frames a change as "an agent's PR" with a title,
   branch, agent name, and add/remove/change stats. archai has the diff (the stats)
   and git can supply branch/commit; we synthesize the PR header from those.

## Mapping the data: archai → UIGraph

The seam is a single JSON document. Everything below the line ("layout") is computed
by the UI; everything above comes straight from archai.

```
SEMANTIC (from archai)                         SOURCE in archai
─────────────────────────────────────────────────────────────────────────
pr.title / branch / agent                      git + diff summary (synthesized)
pr.stats.{added,removed,changed}               diff.Compute → count by Op
boundedContexts[].{id,name}                    overlay.Config.BoundedContexts (fallback: layers)
components[].{id,name,tech,desc,bc}            PackageModel (path, language, layer/BC, doc)
components[].diff                              diff.Change on KindPackage
  internals[].{id,kind,name}                   InterfaceDef / StructDef (kind: iface/class)
  internals[].diff                             diff.Change on KindInterface/KindStruct
    members[].{id,kind,name}                    MethodDef (method) / FieldDef (prop)
    members[].diff                              diff.Change on KindMethod/KindField
  ports[].{id,name,kind(in/out)}               exported InterfaceDef (in) / outbound Dependency (out)
  ports[].diff                                 diff.Change
edges[].{id,from,to,label}                     Dependency (From→To, Kind as label)
edges[].diff                                   diff.Change on KindDep
comments[]                                     (UI-local for POC; not from archai)
─────────────────────────────────────────────────────────────────────────
LAYOUT (computed by UI)                        ELK/dagre + deterministic placement
boundedContexts[].{x,y,w,h}
components[].{x,y,w,h,wx,hx}
  internals[].{x,y,w,h}
  ports[].{side,y}
```

`diff` on every element is the value `added | removed | changed`, derived by mapping
`diff.Op` (`OpAdd → added`, `OpRemove → removed`, `OpChange → changed`) onto the
element identified by `diff.Change.Path`.

## Risks / unknowns to validate in the POC

- **Granularity of ports.** "Port" is a UI concept (a labeled dot on a component
  wall). The cleanest archai source is *exported interfaces* (inbound) and *outbound
  dependencies* (outbound). We will confirm this reads well on a real project.
- **Diff path → element identity.** `diff.Change.Path` is a string like
  `internal/service.Service.Handle`. The projection must parse it to attach the
  `added/removed/changed` flag to the right component/internal/member/port. Needs a
  small, well-tested resolver.
- **Layout quality.** Auto-layout of nested mini-canvases (components containing
  internals containing members) is the least proven piece. The POC will start with a
  simple deterministic layout and only reach for ELK if needed.

## Conclusion

Build the POC as a **standalone web app** (per the chosen direction) that consumes a
new **UIGraph JSON projection** from archai. archai's engine stays essentially
untouched; we add one thin Go projection + CLI/endpoint, and one new front-end.
See [`design.md`](./design.md).
