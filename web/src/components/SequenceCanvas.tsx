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
const ACTOR_MAX_W = 260;
const COL_GAP = 26;
const MAX_COL_GAP = 320;
const SIDE_PAD = 18;
const TOP_PAD = 12;
const MSG_ROW_H = 26;
const MSG_START_GAP = 20;
const BOTTOM_PAD = 18;
const SELF_LOOP_W = 26;
const MSG_CHAR_W = 6.0;

function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, v));
}

function SequenceDiagram({ parsed }: { parsed: ParsedSequence }) {
  const markerId = useId().replace(/[^a-zA-Z0-9_-]/g, '');

  const geo = useMemo(() => {
    const indexById = new Map(parsed.participants.map((p, i) => [p.id, i]));

    // Widen the uniform column gap so message labels between near columns fit:
    // a label spanning k columns needs ~labelW/k per gap.
    let gap = COL_GAP;
    for (const m of parsed.messages) {
      const a = indexById.get(m.from);
      const b = indexById.get(m.to);
      if (a == null || b == null || a === b) continue;
      const k = Math.abs(a - b);
      gap = clamp((m.label.length * MSG_CHAR_W + 16) / k, gap, MAX_COL_GAP);
    }

    const widths = parsed.participants.map((p) =>
      clamp(p.label.length * ACTOR_CHAR_W + ACTOR_PAD_W, ACTOR_MIN_W, ACTOR_MAX_W)
    );
    const centers: number[] = [];
    let x = SIDE_PAD;
    widths.forEach((w) => {
      centers.push(x + w / 2);
      x += w + gap;
    });
    const width = Math.max(x - gap + SIDE_PAD, 240);
    const height =
      TOP_PAD + ACTOR_H + MSG_START_GAP + parsed.messages.length * MSG_ROW_H + BOTTOM_PAD;
    const centerById = new Map(parsed.participants.map((p, i) => [p.id, centers[i]]));
    return { widths, centers, width, height, centerById };
  }, [parsed]);

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
        </defs>
        {parsed.participants.map((p, i) => (
          <line
            key={p.id}
            className="hf-seq-life"
            x1={geo.centers[i]}
            y1={TOP_PAD + ACTOR_H}
            x2={geo.centers[i]}
            y2={geo.height - 6}
          />
        ))}
        {parsed.messages.map((m, i) => {
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
                  {m.label}
                </text>
              </g>
            );
          }
          const dir = x2 > x1 ? 1 : -1;
          return (
            <g key={i}>
              <line
                x1={x1}
                y1={y}
                x2={x2 - dir * 3}
                y2={y}
                className="hf-seq-msg"
                markerEnd={`url(#seq-arr-${markerId})`}
              />
              <text x={(x1 + x2) / 2} y={y - 6} className="hf-seq-label" textAnchor="middle">
                {m.label}
              </text>
            </g>
          );
        })}
      </svg>
      {parsed.participants.map((p, i) => (
        <div
          key={p.id}
          className="hf-seq-actor"
          style={{
            left: geo.centers[i] - geo.widths[i] / 2,
            top: TOP_PAD,
            width: geo.widths[i],
            height: ACTOR_H,
          }}
          title={p.label}
        >
          {p.label}
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
