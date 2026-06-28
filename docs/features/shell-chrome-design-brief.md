# Archai Canvas — Shell Chrome Design Brief

**For:** a design pass (Claude-designer / Figma) on the **Shell** — the application
frame around the generative canvas. **Not** the artifacts inside it.

**Goal of this pass:** design the chrome that lets one person work across **multiple
projects** from a single canvas — switch project, switch/save artifacts, and bind
artifacts to a project — while keeping the canvas itself the star.

---

## 0. The one boundary that defines everything

> **Shell = frame. Artifact = content.**

The agent never renders chrome. Everything it produces is an **artifact** — a single
file of JSX that composes a fixed set of host-provided capabilities (Markdown, Graph,
File, Mermaid, …) and pulls live data. The **Shell** owns *everything around* those
artifacts and renders no domain content of its own.

Design consequence: the chrome must **recede**. It is navigation, status, and
persistence — never the subject. When an artifact is open, the eye should land on the
artifact, not the frame. Treat the shell like a pro tool's chrome (Linear, Figma,
an IDE): quiet, dense, keyboard-first, gets out of the way.

---

## 1. What exists today (the baseline to redesign)

Current layout — a top header over three horizontal zones:

```
┌───────────────────────────────────────────────────────────────────────┐
│ [‹] Archai Canvas                                          [sidebar ⌗] │  header (h-12)
├──────────────────┬──────────────────────────────────┬──────────────────┤
│                  │                                  │  GENERATED       │
│   Chat (AG-UI)   │        Canvas viewport           │   · …            │
│   collapsible    │     (the active artifact)        │  SAVED           │
│   ~35%           │            ~65%                  │   · Welcome      │
│                  │                                  │   · Plugin arch  │
│                  │                                  │  ─ Artifact VFS ─ │
└──────────────────┴──────────────────────────────────┴──────────────────┘
```

- **Header** — chat collapse toggle, app title "Archai Canvas", artifacts-sidebar toggle.
- **Chat panel** (~35%, collapsible to 0) — the conversation with the agent.
- **Canvas viewport** (~65%) — renders the one active artifact.
- **Artifacts sidebar** (~224px, toggleable) — two flat lists, **Generated** and
  **Saved**. A row = icon + name; hover reveals **save** (generated only) and
  **delete**. Click a row = make it active. Footer label "Artifact VFS".

**The gap:** there is **no project switcher** and **no artifact↔project binding**.
The backend project is implicitly the current working directory; all artifacts live in
one global bucket. This brief is about closing that gap in the chrome.

---

## 2. Surfaces to design

For each surface: what it's for, what data it binds to, its states, its interactions.
Design all states — empty, loading, active, error — not just the happy path.

### 2.1 Project (Workspace) switcher — **NEW, the centerpiece**
- **Role:** the top-level context selector. Picking a project re-points the agent,
  re-scopes the artifact list, and (later) swaps the enabled plugin set.
- **A project is:** a working folder + an agent + enabled plugins. Show it by a
  human name; the folder/path is secondary detail.
- **Binds to:** the list of known projects; the active project; per-project agent
  **connection status** (connected / connecting / offline / error).
- **States:** one project; many projects; long list (needs search/filter); a project
  whose agent is offline; "no projects yet" (first run / add-project affordance).
- **Interactions:** switch project; add/remove a project; quick keyboard switch
  (command-palette style). Switching should feel instant and unmistakable — it
  changes *everything* below it.
- **Placement question (for you to explore):** top-left in the header (like a
  workspace/org switcher) vs a dedicated left rail. Mock both.

### 2.2 Artifact navigator (today's sidebar, now project-scoped)
- **Role:** switch between artifacts, save a generated one, delete, and **bind/unbind
  an artifact to the current project**.
- **Binds to:** artifacts of the active project, split by kind — **Generated**
  (ephemeral, from the current session) vs **Saved** (durable dashboards).
- **New need — binding:** an artifact may be *project-scoped* (shows only inside its
  project) or *global* (a reusable dashboard available in every project). The
  navigator must express which a given artifact is, and let the user move it between
  scopes. Decide how "global vs this-project" reads visually (a section, a tag, a
  pin?).
- **States:** empty generated; empty saved; active row; renaming; a saved global
  artifact surfaced inside a project; busy/compiling.
- **Interactions:** select; save (generated→saved); rename; delete; bind to project /
  promote to global; (nice-to-have) reorder/group.

### 2.3 Agent connection status — **NEW**
- **Role:** make the live link to the project's agent legible. Because each project
  carries its own agent, the user must always know *who they're talking to* and
  *whether it's live*.
- **Binds to:** the active agent identity + AG-UI connection state + raw event-stream
  liveness (there is already a live event feed; "live/idle" is a real signal).
- **States:** connected & idle; connected & streaming (agent working); reconnecting;
  offline; misconfigured endpoint.
- **Placement:** likely a compact indicator in the header near the project name, or
  folded into the project switcher. Keep it quiet until it needs attention.

### 2.4 Canvas viewport chrome
- **Role:** the minimal frame around the active artifact — its title, and a small set
  of artifact-level actions (save, fullscreen, maybe copy/share). The artifact's *own*
  widgets (graph fullscreen, file expand) are **not** yours — leave room for them.
- **States:** an artifact open; nothing open (empty canvas / "ask the agent to build
  something" affordance); an artifact that failed to compile (the shell shows a clean
  error frame; the agent gets the error back to fix it).
- **Keep it nearly invisible.** A title and a couple of icon actions. The content is
  the star.

### 2.5 Chat panel framing
- **Role:** only the *frame* — collapse/expand, width, and the seam between chat and
  canvas. The thread internals (messages, tool calls, reasoning) are an existing
  component, **out of scope** here except where they touch the frame.
- **States:** expanded; collapsed (canvas full-width); resizing.

### 2.6 App frame / header
- **Role:** holds the project switcher, app identity, connection status, and the panel
  toggles. This is the one piece of persistent chrome. Define its information
  hierarchy: project context (left) vs global controls (right).

---

## 3. New concepts the design must make legible

A first-time user should be able to *read these off the screen* without explanation:

- **Project / Workspace** — "I am working in project X (folder + its agent)." Switching
  it changes the agent, the artifacts, and the data the artifacts pull.
- **Artifact kind** — Generated (ephemeral, this session) vs Saved (durable dashboard).
- **Artifact scope** — bound to one project vs global/reusable.
- **Agent liveness** — connected / working / offline.

---

## 4. Design principles & constraints

- **Chrome recedes, content leads.** Quiet surfaces, restrained color, the artifact
  carries the visual weight.
- **Keyboard-first.** Project switch and artifact switch should have command-palette
  speed; design with shortcuts in mind.
- **Density over decoration.** This is a daily-driver tool, not a marketing page.
  Think Linear / Figma / IDE chrome.
- **Theme-aware.** Honor the existing light/dark design tokens (CSS custom properties:
  `--background`, `--foreground`, `--border`, `--muted`, `--accent`, `--card`, …).
  Don't hardcode colors; design for both modes.
- **Standalone-app feel.** This is meant to read as a desktop app (eventual macOS
  bundle), not a web page — no browser-y nav, no marketing chrome.
- **Resizable, persistent layout.** Panels are user-resizable and the layout state
  persists; design handles/affordances for that.

---

## 5. Explicitly out of scope (do not design these here)

- Artifact *internals* — graphs, file viewers, dashboards. Those are agent-authored.
- The chat thread internals (message bubbles, tool-call rendering) — existing.
- A plugin-authoring / plugin-marketplace UI — later pass (see open decisions).
- Backend/agent configuration screens beyond "add/switch a project."

---

## 6. Open product decisions (flag both options in mockups)

These are unsettled; please mock the variants rather than assume one:

1. **Project ↔ agent cardinality.** Is a project exactly *one* folder + *one* agent,
   or can a folder host *several* agents (e.g. an arch-review agent + a coding agent)
   you switch between? The latter makes the switcher two-level (project → agent).
2. **Where unbound artifacts live.** Are artifacts project-scoped by default with an
   opt-in "global", or global by default with an opt-in "pin to project"?
3. **Plugin enablement in the chrome.** Do enabled plugins surface in this pass (e.g.
   a per-project plugin indicator / toggle), or is that a later surface? A plugin
   contributes capabilities the agent can use; the user may want to see/toggle them
   per project.

---

## 7. Deliverables requested from the design pass

- The **header + project switcher** in its states (1 project, many, offline agent).
- The **artifact navigator** with generated/saved + project-scoped vs global binding.
- The **empty canvas** state and the **compile-error** frame.
- Light and dark.
- A short rationale for the project-switcher placement you chose.
