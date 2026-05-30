# Concept — Architecture-as-Data & the AI-Agent Review Surface

> Status: concept capture (POC phase). This is the durable statement of the *idea*.
> It is intentionally language-agnostic and implementation-free. Date: 2026-05-30.

## One sentence

A modular system that reads a software repository in **any language**, projects it
into a **language-agnostic domain model of architecture** (components, their public
interfaces, and the relationships between them), and renders that model — and the
**diff between a target architecture and the current one** — as something a human can
read, version in git, and review.

## The core idea

Software has an architecture that lives *above* the code: components, the public
interfaces they expose, and how they connect. Today that architecture is implicit —
scattered across files, only in people's heads, and re-derived by hand every time
someone needs to reason about the system.

The idea is to make architecture a **first-class, versioned artifact**:

1. **Analyze** a repo and extract its architecture (not deep code — only the public
   surface: components, interfaces, connections).
2. **Represent** it as a domain model that does not depend on the source language.
3. **Project** that model into useful outputs: a versionable YAML/text file, a
   diagram, a diff.
4. **Compare** a *target architecture* (what we want) against the *current
   architecture* (what the code actually is) and show what diverges.

Because the projection is plain text, it can be **committed to the repo, versioned,
diffed, and reviewed** like any other source artifact. You get history of *how the
architecture changed over time*, not just how the code changed.

## The reframing that matters most

The single most important framing (the user's own words, from the design chat):

> Это интерфейс в первую очередь для коммуникации с AI coding agents. Идея в том,
> чтобы вместо ревью кода конкретных компонентов подняться на уровень выше и смотреть
> на дизайн интерфейсов и их связей, а также на изменения, которые AI сделал в рамках
> задачи.

In English: **this is, first and foremost, a surface for reviewing AI coding agents'
work at the architecture level.** Instead of reading the diff of every file an agent
touched, the reviewer rises one level up and looks at: *what components, interfaces,
and connections did the agent add, remove, or change?*

That reframes the whole product:

- The **author** of a change is (often) an AI agent.
- The **reviewer** is a human engineer.
- The **unit of review** is an *architectural* diff, not a line-level code diff.
- "Target architecture" is the contract the agent is expected to honor; the review
  surface shows where its work diverges from that contract and lets a human comment
  on specific components / interfaces / connections.

## Modularity (the language story)

The system is modular by design. Adding support for a new programming language means
writing **one new module** that satisfies the same interface as every other language
module: *given source paths, produce domain-model objects.* Everything downstream
(projection to YAML, to diagrams, diffing, the review UI) is language-agnostic and
unchanged.

```
  Go module ─┐
  Java module ┤→  [ common reader interface ]  →  domain model  →  projections
  TS module ──┘                                                     ├─ YAML (versioned)
  …any lang──┘                                                      ├─ diagram
                                                                    └─ diff (target↔current)
```

## Key concepts (ubiquitous language)

| Term | Meaning |
|------|---------|
| **Domain model** | Language-agnostic representation of architecture: components, interfaces, members, relationships. Pure data, no behavior. |
| **Language module** | A pluggable reader that turns source in one language into domain-model objects. Behind a common interface. |
| **Component** | An architectural unit (a package / service / module). Has a name, technology tag, a set of public interfaces, and ports. |
| **Interface / class** | A named contract or type *inside* a component, with members (methods and properties). |
| **Port** | A public entry/exit point of a component (an exposed interface or a used dependency). |
| **Connection / edge** | A relationship between two components (one uses / emits to / depends on another). |
| **Bounded context** | A higher-level grouping of components (DDD). Used to organize the diagram. |
| **Projection** | A transform from the domain model into an output format (YAML, diagram, diff JSON). Reversible where it makes sense. |
| **Target architecture** | A frozen snapshot of the *intended* architecture. The contract. |
| **Current architecture** | The architecture extracted from the code as it is right now. |
| **Architecture diff** | The structured set of additions / removals / changes between target and current, at component / interface / member / connection granularity. |
| **Review** | A human reading an architecture diff and attaching comments to specific architectural elements. |

## What the system is NOT

- It is **not** a code-level diff viewer. It deliberately stays above the code.
- It does **not** capture deep implementation detail — only the public architectural
  surface.
- It is **not** tied to one language; language specifics live only in modules.
- It is **not** (in this POC) a persistence/collaboration backend — comments and
  reviews can be ephemeral for now.

## Why this is worth building

- **Versioned architecture**: the design lives in git, with history.
- **Drift detection**: target-vs-current diff tells you when reality has drifted from
  intent.
- **AI-agent governance**: a fast, high-altitude way to review what an agent changed
  structurally — the most valuable kind of review when agents write most of the code.
- **One model, many views**: the same domain model powers YAML, diagrams, diffs, and
  an interactive review UI.

## Related documents

- [`repo-assessment.md`](./repo-assessment.md) — how the existing `archai` codebase
  already realizes most of this concept, and what is missing.
- [`design.md`](./design.md) — the concrete POC design built on top of `archai`.
- [`handoff/`](./handoff/) — the Claude Design hi-fi mockup bundle (the review UI).
