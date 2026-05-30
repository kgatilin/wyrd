# Architecture Review UI (POC) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Design source of truth:** [`design.md`](./design.md), [`concept.md`](./concept.md), [`repo-assessment.md`](./repo-assessment.md).
> **Mockup source of truth (port from these, do NOT re-invent):** `docs/poc/handoff/project/` — `hifi-tokens.css`, `hifi-shared.jsx`, `hifi-v4.jsx`, `shared.jsx` (the `SCENARIO`), `design-canvas.jsx`. Read them before porting.

**Goal:** Render a real Go repo's architecture, and a target-vs-current architectural diff, in the hi-fi "V4" review UI — fed by a new `UIGraph` JSON projection emitted by archai.

**Architecture:** archai stays the engine (Go). Add a pure projection `internal/adapter/uigraph` (`model + overlay + diff → UIGraph`) + CLI `archai export ui`. A new standalone Vite+React+TS app in `web/` fetches the `UIGraph` JSON, computes layout deterministically, and renders the V4 layout ported 1:1 from the mockup. Comments are UI-local.

**Tech Stack:** Go 1.25 (cobra, existing archai packages), TypeScript, React 18, Vite 5, vitest. CSS reused verbatim from `hifi-tokens.css`.

**Conventions:** Go follows repo rules in `CLAUDE.md` (hexagonal, TDD, no test-only exports, domain is pure data). Commit after each green step. The web app is POC-grade: typed and structured, lightly tested.

---

## File Structure

**archai (Go) — new files only; engine untouched otherwise:**
- `internal/adapter/uigraph/uigraph.go` — `UIGraph` types (JSON tags) + `Project(...)`.
- `internal/adapter/uigraph/resolve.go` — diff-path resolver (`diff.Change.Path` → node id + level).
- `internal/adapter/uigraph/uigraph_test.go` — projection + resolver unit tests.
- `cmd/archai/export.go` — `archai export ui` command wiring.
- `cmd/archai/export_test.go` — wire-level test.
- `cmd/archai/main.go` — MODIFY: register `newExportCmd()` (one line, like `newExtractCmd()`).

**web/ (new standalone app):**
- `web/index.html`, `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`
- `web/src/main.tsx`, `web/src/App.tsx`, `web/src/types.ts`
- `web/src/data/load.ts`, `web/src/data/fixture.ts`
- `web/src/layout/layout.ts`, `web/src/layout/layout.test.ts`
- `web/src/state/hooks.ts` (useExpansion, useFocus, comments)
- `web/src/components/`: `AppBar.tsx`, `PrHeader.tsx`, `Tree.tsx`, `BCGroups.tsx`, `EdgeLayer.tsx`, `Component.tsx`, `Legend.tsx`, `CanvasToolbar.tsx`, `InlinePopover.tsx`, `PinnedMarker.tsx`, `ChangesPanel.tsx`
- `web/src/styles/hifi-tokens.css` (copied verbatim from handoff)
- `web/public/archgraph.sample.json` (committed sample), `web/public/archgraph.json` (generated; gitignored)
- `web/.gitignore`, `web/README.md`

---

## Phase A — Contract & engine (Go)

### Task A1: `UIGraph` types + TS mirror

**Files:**
- Create: `internal/adapter/uigraph/uigraph.go`
- Create: `web/src/types.ts`

- [ ] **Step 1: Define the Go types** (semantic only; no geometry). These are the single source of truth for the contract.

```go
// Package uigraph projects archai's domain model + overlay + diff into the
// UIGraph JSON shape consumed by the POC review UI. Pure data + a pure
// projection function; no I/O, no behavior on the types.
package uigraph

const Schema = "archai.uigraph/v0"

type UIGraph struct {
	Schema          string           `json:"schema"`
	PR              *PR              `json:"pr,omitempty"`
	BoundedContexts []BoundedContext `json:"boundedContexts"`
	Components      []Component      `json:"components"`
	Edges           []Edge           `json:"edges"`
	Comments        []Comment        `json:"comments"`
}

type PR struct {
	Title   string `json:"title"`
	Branch  string `json:"branch"`
	Agent   string `json:"agent"`
	Summary string `json:"summary"`
	Stats   Stats  `json:"stats"`
}
type Stats struct {
	Added, Removed, Changed, Comments int `json:"added","removed","changed","comments"` // see Step 2
}

type BoundedContext struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Component struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Tech      string     `json:"tech"`
	Desc      string     `json:"desc"`
	BC        string     `json:"bc"`
	Diff      string     `json:"diff,omitempty"` // added|removed|changed
	Internals []Internal `json:"internals"`
	Ports     []Port     `json:"ports"`
}

type Internal struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"` // class|iface
	Name    string   `json:"name"`
	Diff    string   `json:"diff,omitempty"`
	Members []Member `json:"members"`
}

type Member struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // method|prop
	Name string `json:"name"`
	Diff string `json:"diff,omitempty"`
}

type Port struct {
	ID   string `json:"id"`
	Side string `json:"side"` // left|right
	Kind string `json:"kind"` // in|out
	Name string `json:"name"`
	Diff string `json:"diff,omitempty"`
}

type Edge struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	To       string `json:"to"`
	FromPort string `json:"fromPort"`
	ToPort   string `json:"toPort"`
	Label    string `json:"label"`
	Diff     string `json:"diff,omitempty"`
}

type Comment struct {
	ID     string        `json:"id"`
	Target CommentTarget `json:"target"`
	Body   string        `json:"body"`
}
type CommentTarget struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
```

> NOTE for implementer: the `Stats` struct tags above are written compactly for the
> plan; write them correctly as four separate fields:
> `Added int `+"`json:\"added\"`"+`` etc.

- [ ] **Step 2: Fix `Stats` tags properly** in `uigraph.go`:

```go
type Stats struct {
	Added    int `json:"added"`
	Removed  int `json:"removed"`
	Changed  int `json:"changed"`
	Comments int `json:"comments"`
}
```

- [ ] **Step 3: Mirror the contract in TypeScript** at `web/src/types.ts` — same field names. Add **layout** fields as optional (filled by the layout pass), and the diff union type:

```ts
export type Diff = 'added' | 'removed' | 'changed';
export interface UIGraph {
  schema: string;
  pr?: PR;
  boundedContexts: BoundedContext[];
  components: Component[];
  edges: Edge[];
  comments: Comment[];
}
export interface PR { title: string; branch: string; agent: string; summary: string; stats: Stats; }
export interface Stats { added: number; removed: number; changed: number; comments: number; }
export interface BoundedContext { id: string; name: string; x?: number; y?: number; w?: number; h?: number; }
export interface Component {
  id: string; name: string; tech: string; desc: string; bc: string; diff?: Diff;
  internals: Internal[]; ports: Port[];
  x?: number; y?: number; w?: number; h?: number; wx?: number; hx?: number;
}
export interface Internal { id: string; kind: 'class' | 'iface'; name: string; diff?: Diff; members: Member[]; x?: number; y?: number; w?: number; h?: number; }
export interface Member { id: string; kind: 'method' | 'prop'; name: string; diff?: Diff; }
export interface Port { id: string; side: 'left' | 'right'; kind: 'in' | 'out'; name: string; diff?: Diff; y?: number; }
export interface Edge { id: string; from: string; to: string; fromPort: string; toPort: string; label: string; diff?: Diff; }
export interface Comment { id: string; target: { type: string; id: string }; body: string; }
```

- [ ] **Step 4: Commit** — `git add internal/adapter/uigraph/uigraph.go web/src/types.ts && git commit -m "feat(poc): define UIGraph contract (Go + TS)"`

### Task A2: diff-path resolver (TDD — the one tricky piece)

**Files:** Create `internal/adapter/uigraph/resolve.go`, `internal/adapter/uigraph/uigraph_test.go`

- [ ] **Step 1: Write the failing test.** `diff.Change.Path` is a dotted path like
  `internal/service` (package), `internal/service.Service` (type), or
  `internal/service.Service.Handle` (member). The resolver classifies the level and
  returns the package path + remaining segments.

```go
package uigraph

import "testing"

func TestParseChangePath(t *testing.T) {
	cases := []struct {
		in        string
		wantPkg   string
		wantType  string
		wantMember string
		wantLevel string // "package" | "type" | "member"
	}{
		{"internal/service", "internal/service", "", "", "package"},
		{"internal/service.Service", "internal/service", "Service", "", "type"},
		{"internal/service.Service.Handle", "internal/service", "Service", "Handle", "member"},
		{"github.com/x/y/pkg.T.M", "github.com/x/y/pkg", "T", "M", "member"},
	}
	for _, c := range cases {
		got := parseChangePath(c.in)
		if got.Pkg != c.wantPkg || got.Type != c.wantType || got.Member != c.wantMember || got.Level != c.wantLevel {
			t.Errorf("parseChangePath(%q) = %+v, want pkg=%q type=%q member=%q level=%q",
				c.in, got, c.wantPkg, c.wantType, c.wantMember, c.wantLevel)
		}
	}
}
```

- [ ] **Step 2: Run, verify it fails** — `go test ./internal/adapter/uigraph/ -run TestParseChangePath -v` → FAIL (undefined `parseChangePath`).

- [ ] **Step 3: Implement `resolve.go`.** Package paths contain `/` and may contain `.` (e.g. `github.com/x/y`), so split on the LAST path segment: the package is everything up to and including the last segment that has no following `.`-separated type. Concretely: the package path is the longest prefix that is a known package — but since the resolver is pure, classify by splitting the final path element on `.`:

```go
package uigraph

import "strings"

type changePath struct {
	Pkg, Type, Member, Level string
}

// parseChangePath splits a diff path of the form
//   <pkg-path>[.<Type>[.<Member>]]
// The package path may itself contain dots (e.g. github.com/x/y), so we only
// treat dots AFTER the final '/' segment as Type/Member separators.
func parseChangePath(p string) changePath {
	slash := strings.LastIndex(p, "/")
	head, tail := "", p
	if slash >= 0 {
		head, tail = p[:slash+1], p[slash+1:] // head keeps trailing '/'
	}
	parts := strings.SplitN(tail, ".", 3)
	cp := changePath{Pkg: head + parts[0], Level: "package"}
	if len(parts) >= 2 && parts[1] != "" {
		cp.Type = parts[1]
		cp.Level = "type"
	}
	if len(parts) >= 3 && parts[2] != "" {
		cp.Member = parts[2]
		cp.Level = "member"
	}
	return cp
}

// diffWord maps a diff.Op-derived string. Callers pass the op as produced by
// the diff package's String()/marshalling. Keep mapping centralized here.
func diffWord(op string) string {
	switch op {
	case "add", "added", "Add":
		return "added"
	case "remove", "removed", "Remove":
		return "removed"
	case "change", "changed", "Change":
		return "changed"
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run, verify pass** — `go test ./internal/adapter/uigraph/ -run TestParseChangePath -v` → PASS.

- [ ] **Step 5: Commit** — `git commit -am "feat(poc): uigraph diff-path resolver (TDD)"`

### Task A3: `Project(...)` projection (TDD)

**Files:** Modify `internal/adapter/uigraph/uigraph.go`, `internal/adapter/uigraph/uigraph_test.go`

> Implementer: FIRST read `internal/domain/*.go` to confirm exact field names on
> `PackageModel`, `InterfaceDef`, `StructDef`, `MethodDef`, `FieldDef`, `Dependency`,
> and `overlay.Config.BoundedContexts`. Read `internal/diff/diff.go` for `Change`,
> `Op`, `Kind`, `Path`, and how `Op`/`Kind` stringify. Adjust field access to match;
> the test below pins behavior, not field names.

- [ ] **Step 1: Write the failing projection test** — build 2 tiny `domain.PackageModel`s (a "current" with an extra method vs a "target"), compute a diff, project, and assert: one component per package, internals from interfaces+structs, members from methods+fields, the added method's member carries `diff:"added"`, and an edge exists for a dependency.

```go
func TestProjectMarksAddedMember(t *testing.T) {
	current := []domain.PackageModel{ /* pkg internal/svc with iface Svc{Handle, Close} */ }
	target  := []domain.PackageModel{ /* same but iface Svc{Close} (Handle is new) */ }
	d := diff.Compute(current, target)

	g, err := Project(current, nil, &d)
	if err != nil { t.Fatal(err) }

	// find member internal/svc.Svc.Handle and assert diff == "added"
	// (walk g.Components -> Internals -> Members)
	// assert g.Schema == Schema and len(g.Components) == 1
}
```

- [ ] **Step 2: Run, verify it fails** — `go test ./internal/adapter/uigraph/ -run TestProject -v` → FAIL.

- [ ] **Step 3: Implement `Project`** in `uigraph.go`. Mapping rules (per `design.md` §3 and `repo-assessment.md`):
  - `boundedContexts`: from `cfg.BoundedContexts` if non-nil; else derive from package `Layer`; else a single `{id:"all",name:"All"}`.
  - one `Component` per `PackageModel`: `id = pkg.Path`, `name = last path segment`, `tech = pkg.Language or "Go"`, `desc = pkg.Doc` (first line), `bc = resolved BC id`.
  - `internals`: each `InterfaceDef` (`kind:"iface"`) and `StructDef` (`kind:"class"`); `id = pkg.Path + "." + Name`.
  - `members`: each method (`kind:"method"`, name `Name(params)` short form) and field (`kind:"prop"`, name `Name : Type`).
  - `ports`: one `in` port per exported interface (`side:"left"`), one `out` port per distinct outbound dependency target (`side:"right"`); `id = pkg.Path + ":in:" + Name` / `":out:" + targetPkg`.
  - `edges`: one per dependency between two packages present in the model; `id = "e:"+from+"->"+to`, `label = dependency kind`.
  - diff: for each `diff.Change`, `parseChangePath(change.Path)` → stamp `diffWord(op)` on the matching component/internal/member (and on ports/edges by id when resolvable). Unmatched changes are skipped (logged at debug).
  - `Project` is a pure function: `func Project(models []domain.PackageModel, cfg *overlay.Config, d *diff.Diff) (UIGraph, error)`. When `d == nil`, emit no diff flags and `PR == nil`.

- [ ] **Step 4: Run, verify pass** — `go test ./internal/adapter/uigraph/... -v` → PASS.

- [ ] **Step 5: Commit** — `git commit -am "feat(poc): uigraph.Project model+overlay+diff → UIGraph"`

### Task A4: `archai export ui` CLI

**Files:** Create `cmd/archai/export.go`, `cmd/archai/export_test.go`; Modify `cmd/archai/main.go` (register `newExportCmd()`).

> Implementer: reuse existing helpers in `cmd/archai/` — `loadCurrentModel`,
> overlay resolution (`resolveOverlay`/`loadD2StyleConfig` pattern), and the target/
> diff plumbing used by `runDiff` (read `cmd/archai/main.go` around `runDiff`,
> `runValidate`, and `internal/target`, `internal/diff`). Synthesize `PR` from the git
> branch (best-effort; `""` if unavailable) + diff stats.

- [ ] **Step 1: Write a wire-level test** in `export_test.go`: run `export ui` over a tiny temp Go module (or `./internal/...`) with `-o tmpfile`, assert the file is valid JSON, `schema == "archai.uigraph/v0"`, and `len(components) > 0`.

- [ ] **Step 2: Run, verify it fails** — `go test ./cmd/archai/ -run TestExportUI -v` → FAIL.

- [ ] **Step 3: Implement `newExportCmd()`** — `archai export ui [paths...]` with flags `--target <id>` (default CURRENT if present), `-o/--output` (default stdout), `--overlay`. Load current model, optionally compute diff vs target, `uigraph.Project(...)`, `json.MarshalIndent`, write. Register in `main.go` next to `newExtractCmd()`.

- [ ] **Step 4: Run, verify pass** — `go test ./cmd/archai/ -run TestExportUI -v` → PASS; then `go build ./... && go test ./...` → all green.

- [ ] **Step 5: Generate the committed sample** — `go run ./cmd/archai export ui ./internal/... -o web/public/archgraph.sample.json` and inspect it has bounded contexts + components. Commit code + sample: `git add -A && git commit -m "feat(poc): archai export ui command + sample UIGraph"`

---

## Phase B — Web app shell (Vite + React + TS)

### Task B1: Scaffold + tokens + fixture + data load

**Files:** `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/index.html`, `web/src/main.tsx`, `web/src/styles/hifi-tokens.css`, `web/src/data/load.ts`, `web/src/data/fixture.ts`, `web/.gitignore`, `web/public/archgraph.sample.json` (from A4).

- [ ] **Step 1:** `npm create vite@latest web -- --template react-ts` is NOT used (avoid interactive). Instead author files directly. `package.json` deps: `react`, `react-dom`; devDeps: `vite`, `@vitejs/plugin-react`, `typescript`, `@types/react`, `@types/react-dom`, `vitest`. Scripts: `dev`, `build`, `preview`, `test`.
- [ ] **Step 2:** `vite.config.ts` — `plugins:[react()]`, `server:{ port:5173, strictPort:false }` (auto-fallback to 5174 if busy). `test:{ environment:'jsdom' }` (add `jsdom` devDep) or `node` if no DOM tests.
- [ ] **Step 3:** Copy `docs/poc/handoff/project/hifi-tokens.css` verbatim → `web/src/styles/hifi-tokens.css`; import it in `main.tsx`.
- [ ] **Step 4:** `web/src/data/fixture.ts` — export the mockup `SCENARIO` (from `docs/poc/handoff/project/shared.jsx`) transcribed to a typed `UIGraph` const (this is the guaranteed-rich diff fallback; it already includes geometry, so the layout pass should pass geometry through if present).
- [ ] **Step 5:** `web/src/data/load.ts` — `async function loadGraph(): Promise<UIGraph>` that `fetch('/archgraph.json')`, falls back to `/archgraph.sample.json`, falls back to the imported fixture. Validate `schema` prefix `archai.uigraph/`.
- [ ] **Step 6:** `npm install` in `web/`, then `npm run dev` → confirm it boots and serves a blank page with tokens loaded. **Commit** — `git add web && git commit -m "feat(poc): scaffold web app (vite+react+ts) + tokens + data load"`

### Task B2: Deterministic layout

**Files:** Create `web/src/layout/layout.ts`, `web/src/layout/layout.test.ts`

- [ ] **Step 1: Write failing vitest** — `layout(graph)` returns a graph where every component has numeric `x,y,w,h`; components in the same `bc` fall inside that BC's box; no two components in the same BC overlap. If a node already has geometry (fixture), it is preserved.
- [ ] **Step 2: Run** — `npm test` → FAIL.
- [ ] **Step 3: Implement `layout.ts`** per `design.md` §5.3: BCs in a wrapping row; components in an inner grid (default `w=220,h=86`, expanded `wx≈280`, `hx` grows with internals); internals in a grid inside expanded components; ports stacked on left(`in`)/right(`out`) walls with even `y`. Pure function `layout(g: UIGraph): UIGraph`.
- [ ] **Step 4: Run** — `npm test` → PASS.
- [ ] **Step 5: Commit** — `git commit -am "feat(poc): deterministic layout for UIGraph"`

### Task B3: App frame (appbar + PR header + 3-pane stage)

**Files:** `web/src/App.tsx`, `web/src/components/AppBar.tsx`, `web/src/components/PrHeader.tsx`, `web/src/state/hooks.ts`

- [ ] **Step 1:** Port `HF.AppBar` and `HF.PrHeader` from `hifi-shared.jsx` to TSX (props typed; level switcher, theme toggle, comment count, Submit review). Crumbs/branch/agent come from `graph.pr` (fallback to repo name).
- [ ] **Step 2:** Port `useExpanded`/`useFocus4`/`useExpansion4` from `hifi-v4.jsx`/`hifi-shared.jsx` into `web/src/state/hooks.ts` (typed).
- [ ] **Step 3:** `App.tsx` ports `HFV4` skeleton: load graph (B1) → layout (B2) → render `<AppBar>`, `<PrHeader>` (when `graph.pr`), and the `.hf-stage` with three placeholder panes (left/canvas/right). `className="hifi v4 theme-dark"`.
- [ ] **Step 4:** `npm run dev` → appbar + PR header render with real data from the sample JSON. **Commit** — `git commit -am "feat(poc): app frame — appbar, PR header, 3-pane stage"`

---

## Phase C — Canvas

### Task C1: BC groups + Component (collapsed/expanded/internals/members/ports)

**Files:** `web/src/components/BCGroups.tsx`, `web/src/components/Component.tsx`

- [ ] **Step 1:** Port `HF.BCGroups` (positioned BC boxes with labels).
- [ ] **Step 2:** Port `HF.Component` from `hifi-shared.jsx`: header (icon, name, tech, diff tag NEW/DEL/MOD, expand button), collapsed `desc`, expanded mini-canvas of internals; each internal header (`class`/`iface` kind, name, expand) with member list (`fn`/`:` kind icon, name); ports on left/right walls. Apply diff classes from `diff` fields. Geometry from layout output.
- [ ] **Step 3:** Wire into `App.tsx` canvas: `graph.components.map(<Component>)`. Expansion via B2 hooks; members auto-expand when component expands (port `useExpansion4` effect).
- [ ] **Step 4:** `npm run dev` → components render in BC groups, expand to show internals+members, diff colors visible. **Commit** — `git commit -am "feat(poc): canvas — BC groups + components with internals/members/ports"`

### Task C2: Edges + legend + toolbar

**Files:** `web/src/components/EdgeLayer.tsx`, `web/src/components/Legend.tsx`, `web/src/components/CanvasToolbar.tsx`

- [ ] **Step 1:** Port `HF.computeEdgePath` + `HF.EdgeLayer` (SVG bezier between component ports, arrow markers, diff classes, optional animated flow dots, edge labels, invisible hit-path for clicks).
- [ ] **Step 2:** Port `HF.Legend` (added/removed/changed swatches) and `HF.CanvasToolbar` (zoom controls — cosmetic).
- [ ] **Step 3:** `npm run dev` → edges connect components with labels + diff coloring + flow animation. **Commit** — `git commit -am "feat(poc): canvas edges, legend, toolbar"`

---

## Phase D — Interactions

### Task D1: Focus mode + CHANGES/CONTEXTS left panel + collapsible panels

**Files:** `web/src/components/Tree.tsx`, `web/src/components/ChangesPanel.tsx`, `web/src/App.tsx`

- [ ] **Step 1:** Derive the changes list in TS (port `HF.deriveChanges`) — or, simpler, the projection already carries `diff` flags; walk the graph to build change rows. Port `ChangesPanel` (PR summary block + change cards: badge `+`/`−`/`~`, name, where).
- [ ] **Step 2:** Port `HF.Tree` (bounded-context → component rows with diff badges). Left panel switches CHANGES↔CONTEXTS (default CHANGES when `graph.pr`, else CONTEXTS). `count` chips.
- [ ] **Step 3:** Focus mode: clicking a component sets `focusId`; related = component + edge neighbors; others get `dimmed`, unrelated edges fade (port `useFocus4` + the `dimmed`/`focused` props already wired in C1/C2). Clicking the same component again or the canvas background clears focus.
- [ ] **Step 4:** Collapsible left/right panels (`hf-collapsible`/`hf-side-toggle`/`hf-side-vlabel`) and `goToChange` (click change row → focus + expand + smooth-scroll canvas to component). Port from `hifi-v4.jsx`.
- [ ] **Step 5:** `npm run dev` → focus dims unrelated; CHANGES/CONTEXTS switch; panels collapse; clicking a change centers it. **Commit** — `git commit -am "feat(poc): focus mode, changes/contexts panel, collapsible panels"`

### Task D2: Inline click-to-comment + pinned markers + theme toggle

**Files:** `web/src/components/InlinePopover.tsx`, `web/src/components/PinnedMarker.tsx`, `web/src/App.tsx`

- [ ] **Step 1:** Port `InlinePopover` and `PinnedMarker` from `hifi-v4.jsx`. Comment state is local React (`useState` list of markers). Click any element (component head double/shift-click, internal, member, port, edge) → popover at the click point → submit pins a numbered marker and adds a card to the right rail. Seed markers from `graph.comments` (empty for real archai data; populated for the fixture).
- [ ] **Step 2:** Right panel: comments reference list (port from `hifi-v4.jsx`); clicking a card scrolls canvas to its marker. Theme toggle flips `theme-dark`/`theme-light` on the root (`hifi v4 theme-*`).
- [ ] **Step 3:** `npm run dev` → click element → comment → numbered pin appears + listed right; theme toggles. **Commit** — `git commit -am "feat(poc): inline click-to-comment with pinned markers + theme toggle"`

---

## Phase E — Integrate & verify

### Task E1: Real end-to-end + demo script + verify in Chrome

**Files:** Create `web/README.md`, `scripts/poc-demo.sh` (or document inline in README).

- [ ] **Step 1:** Build archai: `go build -o ./bin/archai ./cmd/archai`.
- [ ] **Step 2:** Demo data with a real diff:
```bash
./bin/archai diagram generate ./internal/... --format yaml      # current specs
./bin/archai target lock baseline                               # freeze target
# introduce ONE real architectural change (e.g. add a method to an exported interface)
./bin/archai export ui ./internal/... --target baseline -o web/public/archgraph.json
```
- [ ] **Step 3:** `cd web && npm run dev`. Confirm the dev URL (5173 or 5174 fallback).
- [ ] **Step 4: Verify in Chrome (port from `--http`/Vite):** architecture renders from real archai data; the introduced change shows as `added/removed/changed` and appears in CHANGES; expand a component to see members; focus mode works; click-to-comment pins a marker. Capture one screenshot to `docs/poc/handoff/../` or `docs/screenshots/`.
- [ ] **Step 5:** `web/README.md` documents the demo + how the app maps to archai. Revert the temporary architectural change if it was only for the demo (or keep it as the demo state — note which). **Commit** — `git commit -am "docs(poc): demo script + web README; verified end-to-end"`

---

## Self-review checklist (run before declaring done)
- Spec coverage: every success criterion in `design.md` §1 maps to a task (A4/E1 = real data + diff; C1/C2 = render; D1/D2 = interactions; E1 = Chrome verify). ✓
- No placeholders: Go code is complete for the novel pieces; the mechanical port references exact mockup files in `docs/poc/handoff/project/`. ✓
- Type consistency: `UIGraph` field names identical across `uigraph.go` and `types.ts`; layout fields optional only on the TS side. ✓
- Engine untouched: only new Go files + one registration line in `main.go`. ✓
