import { useEffect, useId, useMemo, useState } from 'react';

/**
 * Native renderer for the daemon's package call-sequence diagrams, drawn in the
 * same visual language as the graph canvas (hf-* tokens) instead of Mermaid.
 *
 * The daemon returns each entry point as Mermaid `sequenceDiagram` source that
 * WE generate (internal/adapter/mermaid/sequence.go) — a strict two-line-shape
 * subset: `participant pN as Label` declarations followed by `pA->>pB: label`
 * messages. Parsing that subset here is lossless; when the daemon grows a
 * structured JSON format this parser is the only thing to swap out.
 */
export interface SequenceEntry {
  label: string;
  mermaid: string;
  hasCalls: boolean;
}

interface SequenceResult {
  package: string;
  mode: string;
  entries: SequenceEntry[];
}

export interface ParsedSequence {
  participants: { id: string; label: string }[];
  messages: { from: string; to: string; label: string }[];
}

export function parseSequenceMermaid(src: string): ParsedSequence {
  const participants: ParsedSequence['participants'] = [];
  const messages: ParsedSequence['messages'] = [];
  const seen = new Set<string>();
  for (const raw of src.split('\n')) {
    const line = raw.trim();
    let m = /^participant\s+(\S+)\s+as\s+(.+)$/.exec(line);
    if (m) {
      if (!seen.has(m[1])) {
        seen.add(m[1]);
        participants.push({ id: m[1], label: unquoteLabel(m[2]) });
      }
      continue;
    }
    m = /^([A-Za-z0-9_]+)->>([A-Za-z0-9_]+):\s*(.*)$/.exec(line);
    if (m) messages.push({ from: m[1], to: m[2], label: m[3] });
  }
  return { participants, messages };
}

function unquoteLabel(s: string): string {
  const t = s.trim();
  const unq = t.startsWith('"') && t.endsWith('"') && t.length >= 2 ? t.slice(1, -1) : t;
  return unq.split('#quot;').join('"');
}

// Geometry: lifelines are columns sized by their label, messages are rows in
// call order (DFS of the interaction tree — the order the source emits).
const ACTOR_H = 26;
const ACTOR_CHAR_W = 6.8;
const ACTOR_PAD_W = 24;
const ACTOR_MIN_W = 72;
const ACTOR_MAX_W = 220;
const ACTOR_MAX_CHARS = 28;
const COL_GAP = 26;
const SIDE_PAD = 18;
const TOP_PAD = 12;
const MSG_ROW_H = 26;
const MSG_START_GAP = 20;
const BOTTOM_PAD = 18;
const SELF_LOOP_W = 26;
const MSG_CHAR_W = 6.0;
const MSG_LABEL_PAD = 16;
const MSG_MAX_CHARS = 34;
// Past this span the midpoint of an arrow is nowhere near either end of it, so
// the label rides next to the caller instead of floating in empty canvas.
const LABEL_MID_MAX = 420;

function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, v));
}

function elide(s: string, max: number): string {
  return s.length <= max ? s : `${s.slice(0, max - 1)}…`;
}

interface SeqActor {
  id: string;
  /** What the column header shows — the package prefix stripped when it is the diagram's own. */
  label: string;
  /** The lifeline name as the daemon emitted it, kept for the tooltip. */
  full: string;
  /** Lives in a different package than the entry point this diagram starts from. */
  external: boolean;
}

interface SeqMessage {
  from: string;
  to: string;
  label: string;
  full: string;
}

interface SeqView {
  actors: SeqActor[];
  messages: SeqMessage[];
}

/**
 * The diagram's own package prefix repeats on nearly every lifeline and costs
 * roughly a third of the canvas width, so it is dropped from the headers. What
 * is left carrying a prefix is exactly the cross-package lifeline — the one
 * worth noticing. The prefix comes off the root participant, which is the entry
 * point the sequence was built from.
 */
function buildView(parsed: ParsedSequence): SeqView {
  const root = parsed.participants[0]?.label ?? '';
  const dot = root.indexOf('.');
  const prefix = dot > 0 ? root.slice(0, dot + 1) : '';
  const actors = parsed.participants.map((p) => {
    const own = prefix !== '' && p.label.startsWith(prefix) && p.label.length > prefix.length;
    return {
      id: p.id,
      label: elide(own ? p.label.slice(prefix.length) : p.label, ACTOR_MAX_CHARS),
      full: p.label,
      external: prefix !== '' && !own,
    };
  });
  const messages = parsed.messages.map((m) => ({
    from: m.from,
    to: m.to,
    label: elide(m.label, MSG_MAX_CHARS),
    full: m.label,
  }));
  return { actors, messages };
}

interface SeqGeometry {
  widths: number[];
  centers: number[];
  width: number;
  height: number;
  centerById: Map<string, number>;
}

/**
 * Column positions. Each gap is widened only by the labels that actually have
 * to span it, shortest span first — a single long label between two adjacent
 * columns must not set the scale for all the others, which is what one uniform
 * gap did (it blew a 12-lifeline flow out to several thousand pixels and pushed
 * every right-to-left arrow off the visible canvas entirely).
 */
function solveGeometry(view: SeqView): SeqGeometry {
  const { actors, messages } = view;
  const n = actors.length;
  const indexById = new Map(actors.map((a, i) => [a.id, i]));
  const widths = actors.map((a) =>
    clamp(a.label.length * ACTOR_CHAR_W + ACTOR_PAD_W, ACTOR_MIN_W, ACTOR_MAX_W)
  );
  const gaps = new Array(Math.max(n - 1, 0)).fill(COL_GAP);

  const spans: { lo: number; hi: number; need: number }[] = [];
  for (const m of messages) {
    const a = indexById.get(m.from);
    const b = indexById.get(m.to);
    if (a == null || b == null || a === b) continue;
    spans.push({
      lo: Math.min(a, b),
      hi: Math.max(a, b),
      need: m.label.length * MSG_CHAR_W + MSG_LABEL_PAD,
    });
  }
  spans.sort((p, q) => p.hi - p.lo - (q.hi - q.lo));
  for (const s of spans) {
    let have = (widths[s.lo] + widths[s.hi]) / 2;
    for (let i = s.lo; i < s.hi; i++) have += gaps[i];
    for (let i = s.lo + 1; i < s.hi; i++) have += widths[i];
    const deficit = s.need - have;
    if (deficit <= 0) continue;
    const share = deficit / (s.hi - s.lo);
    for (let i = s.lo; i < s.hi; i++) gaps[i] += share;
  }

  const centers: number[] = [];
  let x = SIDE_PAD;
  for (let i = 0; i < n; i++) {
    centers.push(x + widths[i] / 2);
    x += widths[i] + (gaps[i] ?? 0);
  }
  const width = Math.max(x + SIDE_PAD, 240);
  const height = TOP_PAD + ACTOR_H + MSG_START_GAP + messages.length * MSG_ROW_H + BOTTOM_PAD;
  const centerById = new Map(actors.map((a, i) => [a.id, centers[i]]));
  return { widths, centers, width, height, centerById };
}

function SequenceDiagram({ parsed }: { parsed: ParsedSequence }) {
  const markerId = useId().replace(/[^a-zA-Z0-9_-]/g, '');
  const view = useMemo(() => buildView(parsed), [parsed]);
  const geo = useMemo(() => solveGeometry(view), [view]);

  return (
    <div className="hf-seq-stage" style={{ width: geo.width, height: geo.height }}>
      <svg className="hf-seq-svg" width={geo.width} height={geo.height}>
        <defs>
          <marker
            id={`seq-arr-${markerId}`}
            viewBox="0 0 10 10"
            refX="9"
            refY="5"
            markerWidth="7"
            markerHeight="7"
            orient="auto-start-reverse"
          >
            <path d="M 0 0 L 10 5 L 0 10 z" className="hf-seq-arrow" />
          </marker>
          <marker
            id={`seq-arr-back-${markerId}`}
            viewBox="0 0 10 10"
            refX="9"
            refY="5"
            markerWidth="7"
            markerHeight="7"
            orient="auto-start-reverse"
          >
            <path d="M 0 0 L 10 5 L 0 10 z" className="hf-seq-arrow-back" />
          </marker>
        </defs>
        {view.actors.map((a, i) => (
          <line
            key={a.id}
            className="hf-seq-life"
            x1={geo.centers[i]}
            y1={TOP_PAD + ACTOR_H}
            x2={geo.centers[i]}
            y2={geo.height - 6}
          />
        ))}
        {view.messages.map((m, i) => {
          const y = TOP_PAD + ACTOR_H + MSG_START_GAP + i * MSG_ROW_H + MSG_ROW_H / 2;
          const x1 = geo.centerById.get(m.from);
          const x2 = geo.centerById.get(m.to);
          if (x1 == null || x2 == null) return null;
          if (m.from === m.to) {
            const path = `M ${x1} ${y - 6} C ${x1 + SELF_LOOP_W} ${y - 6}, ${x1 + SELF_LOOP_W} ${y + 8}, ${x1 + 4} ${y + 8}`;
            return (
              <g key={i}>
                <path d={path} className="hf-seq-msg" markerEnd={`url(#seq-arr-${markerId})`} />
                <text x={x1 + SELF_LOOP_W + 6} y={y + 4} className="hf-seq-label" textAnchor="start">
                  <title>{m.full}</title>
                  {m.label}
                </text>
              </g>
            );
          }
          // A call back to a lifeline that already appeared reads right-to-left.
          // It gets its own colour so the direction is legible even when the
          // arrowhead is thousands of pixels away, off the scrolled viewport.
          const back = x2 < x1;
          const dir = back ? -1 : 1;
          const far = Math.abs(x2 - x1) > LABEL_MID_MAX;
          const lx = far ? x1 + dir * 10 : (x1 + x2) / 2;
          const anchor = far ? (back ? 'end' : 'start') : 'middle';
          return (
            <g key={i}>
              <line
                x1={x1}
                y1={y}
                x2={x2 - dir * 3}
                y2={y}
                className={back ? 'hf-seq-msg hf-seq-msg-back' : 'hf-seq-msg'}
                markerEnd={`url(#seq-arr-${back ? 'back-' : ''}${markerId})`}
              />
              <text
                x={lx}
                y={y - 6}
                className={back ? 'hf-seq-label hf-seq-label-back' : 'hf-seq-label'}
                textAnchor={anchor}
              >
                <title>{m.full}</title>
                {m.label}
              </text>
            </g>
          );
        })}
      </svg>
      {view.actors.map((a, i) => (
        <div
          key={a.id}
          className={a.external ? 'hf-seq-actor hf-seq-actor-ext' : 'hf-seq-actor'}
          style={{
            left: geo.centers[i] - geo.widths[i] / 2,
            top: TOP_PAD,
            width: geo.widths[i],
            height: ACTOR_H,
          }}
          title={a.full}
        >
          {a.label}
        </div>
      ))}
    </div>
  );
}

/** Entry-point picker + one diagram at a time. */
export function SequenceCanvas({ entries }: { entries: SequenceEntry[] }) {
  const [sel, setSel] = useState(0);
  const idx = Math.min(sel, entries.length - 1);
  const entry = entries[idx] ?? null;
  const parsed = useMemo(() => (entry ? parseSequenceMermaid(entry.mermaid) : null), [entry]);

  if (!entry) return <div className="hf-seq-status">No sequence entries.</div>;

  return (
    <div className="hf-seq">
      <div className="hf-seq-entrybar">
        {entries.length > 1 ? (
          <select value={idx} onChange={(e) => setSel(Number(e.target.value))} title="Entry point">
            {entries.map((en, i) => (
              <option key={i} value={i}>
                {en.label}
              </option>
            ))}
          </select>
        ) : (
          <span className="hf-seq-entry-label" title={entry.label}>
            {entry.label}
          </span>
        )}
        {entries.length > 1 && (
          <span className="hf-seq-count">
            {idx + 1}/{entries.length}
          </span>
        )}
      </div>
      <div className="hf-seq-scroll">
        {parsed && parsed.participants.length > 0 && parsed.messages.length > 0 ? (
          <SequenceDiagram parsed={parsed} />
        ) : (
          <div className="hf-seq-status">No cross-type calls in this flow.</div>
        )}
      </div>
    </div>
  );
}

// --- data fetch ---
// The embed page lives under /w/<name>/review/, so a prefix-relative fetch
// automatically targets the SAME worktree the graph came from (matching
// data/load.ts). Only fall back to the root /api/sequence when there is no
// worktree prefix — silently mixing in the main checkout would be wrong.
function currentWorktreePrefix(): string | null {
  if (typeof window === 'undefined') return null;
  const match = window.location.pathname.match(/^\/w\/([^/]+)(?:\/|$)/);
  if (!match) return null;
  return `/w/${match[1]}`;
}

function sequenceURL(pkg: string): string {
  const qs = new URLSearchParams({ package: pkg }).toString();
  const prefix = currentWorktreePrefix();
  return `${prefix ?? ''}/api/sequence?${qs}`;
}

const seqCache = new Map<string, SequenceResult>();

async function fetchSequence(pkg: string): Promise<SequenceResult> {
  const url = sequenceURL(pkg);
  const cached = seqCache.get(url);
  if (cached) return cached;
  const res = await fetch(url, { headers: { Accept: 'application/json' } });
  if (!res.ok) throw new Error(`sequence fetch failed (${res.status})`);
  const data = (await res.json()) as SequenceResult;
  seqCache.set(url, data);
  return data;
}

/**
 * Sequence body for an expanded package card on the graph canvas: fetches the
 * package's call-sequence (from the current worktree) and renders it in the
 * card's fixed frame.
 */
export function CardSequence({ pkg }: { pkg: string }) {
  const [state, setState] = useState<{
    data: SequenceResult | null;
    error: string | null;
  }>({ data: seqCache.get(sequenceURL(pkg)) ?? null, error: null });

  useEffect(() => {
    let cancelled = false;
    setState({ data: seqCache.get(sequenceURL(pkg)) ?? null, error: null });
    fetchSequence(pkg).then(
      (data) => {
        if (!cancelled) setState({ data, error: null });
      },
      (err) => {
        if (!cancelled) setState({ data: null, error: String(err) });
      }
    );
    return () => {
      cancelled = true;
    };
  }, [pkg]);

  if (state.error) return <div className="hf-seq-status">Sequence unavailable: {state.error}</div>;
  if (!state.data) return <div className="hf-seq-status">Loading call sequence…</div>;
  if (state.data.entries.length === 0)
    return <div className="hf-seq-status">No cross-type interactions in this package.</div>;
  return <SequenceCanvas key={pkg} entries={state.data.entries} />;
}
