/**
 * The default landing dashboard — a SAVED artifact seeded when the canvas has
 * no saved dashboards yet (see seed.ts). It is an ordinary artifact file (the
 * same unit the agent authors via write_artifact): a script defining
 * `function Artifact()` that composes host components and pulls everything from
 * data-sources — it bakes no data. It doubles as a how-to for the canvas.
 */
export const welcomeDashboardFile = `
// A ticking "now" so relative timestamps stay fresh while the agent is idle.
function useNow(interval) {
  const [now, setNow] = React.useState(() => Date.now());
  React.useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), interval || 1000);
    return () => clearInterval(id);
  }, [interval]);
  return now;
}

function relTime(ts, now) {
  const d = Math.max(0, now - ts);
  if (d < 1500) return 'now';
  const s = Math.round(d / 1000);
  if (s < 60) return s + 's ago';
  const m = Math.round(s / 60);
  if (m < 60) return m + 'm ago';
  return Math.round(m / 60) + 'h ago';
}

// Fold runs of the same (type, source) into one row with a count, so a burst of
// llm.chunk events reads as "llm.chunk ×8" instead of eight identical bullets.
function foldEvents(events) {
  const rows = [];
  for (const e of events) {
    const last = rows[rows.length - 1];
    if (last && last.type === e.type && last.source === e.source) {
      last.count += 1;
      last.seq = e.seq;
      last.ts = e.ts;
    } else {
      rows.push({ type: e.type, source: e.source, count: 1, seq: e.seq, ts: e.ts });
    }
  }
  return rows;
}

// Source → dot color. Self-contained inline styling so the widget renders
// correctly regardless of the host stylesheet (var(--token, fallback) keeps it
// theme-aware while still working if a token is missing).
function srcColor(s) {
  return {
    llm: '#6366f1',
    agent: '#22c55e',
    workflow: '#f59e0b',
    session: '#06b6d4',
    eventlog: '#a855f7',
  }[s] || '#9ca3af';
}

function ActivityFeed() {
  const events = useEvents();
  const now = useNow(1000);
  const rows = foldEvents(events).slice(-14).reverse();
  const live = events.length > 0 && now - events[events.length - 1].ts < 3000;
  const muted = 'var(--muted-foreground, #6b7280)';

  return (
    <div style={{
      border: '1px solid var(--border, #e5e7eb)',
      borderRadius: 12,
      overflow: 'hidden',
      background: 'var(--card, #ffffff)',
      boxShadow: '0 1px 2px rgba(0,0,0,0.04), 0 8px 24px -12px rgba(0,0,0,0.18)',
    }}>
      <div style={{
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        padding: '10px 14px',
        borderBottom: '1px solid var(--border, #e5e7eb)',
        background: 'color-mix(in srgb, var(--foreground, #111827) 4%, transparent)',
      }}>
        <span style={{
          width: 8, height: 8, borderRadius: '50%', flex: 'none',
          background: live ? '#22c55e' : '#9ca3af',
          boxShadow: live ? '0 0 0 3px rgba(34,197,94,0.18)' : 'none',
        }} />
        <span style={{
          fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em',
          fontSize: 11, color: muted,
        }}>{live ? 'live' : 'idle'}</span>
        <span style={{
          marginLeft: 'auto', color: muted, fontSize: 12,
          fontVariantNumeric: 'tabular-nums',
        }}>{events.length} event{events.length === 1 ? '' : 's'}</span>
      </div>

      {rows.length === 0 ? (
        <div style={{ padding: '24px 14px', textAlign: 'center', color: muted, fontSize: 13 }}>
          No activity yet — start chatting.
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', maxHeight: 360, overflowY: 'auto' }}>
          {rows.map((r, i) => (
            <div key={r.seq} style={{
              display: 'flex',
              alignItems: 'center',
              gap: 10,
              padding: '8px 14px',
              borderTop: i === 0 ? 'none' : '1px solid color-mix(in srgb, var(--border, #e5e7eb) 45%, transparent)',
            }}>
              <span style={{ width: 7, height: 7, borderRadius: '50%', flex: 'none', background: srcColor(r.source) }} />
              <span style={{
                fontFamily: 'var(--font-geist-mono, ui-monospace, SFMono-Regular, monospace)',
                fontSize: 12.5,
                color: 'var(--foreground, #111827)',
              }}>{r.type}</span>
              {r.source ? (
                <span style={{ fontSize: 10.5, letterSpacing: '0.03em', textTransform: 'uppercase', color: muted }}>
                  {r.source}
                </span>
              ) : null}
              {r.count > 1 ? (
                <span style={{
                  fontSize: 11, fontVariantNumeric: 'tabular-nums',
                  padding: '1px 6px', borderRadius: 999,
                  background: 'color-mix(in srgb, var(--foreground, #111827) 9%, transparent)',
                  color: muted,
                }}>×{r.count}</span>
              ) : null}
              <span style={{
                marginLeft: 'auto', fontSize: 12, color: muted,
                fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap',
              }}>{relTime(r.ts, now)}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function Artifact() {
  return (
    <article className="artifact-doc">
      <Markdown>{\`
# archai canvas

A live view of the **archai** Go codebase. Ask the agent to explore it — it
reads the real source through archai's graph tools and renders the answer here
as an interactive artifact.

Try: *"show me the files in package internal/retrieval"*, *"open
internal/adapter/d2/sequence.go"*, or *"diagram the retrieval pipeline"*.
\`}</Markdown>

      <Graph
        source="internal"
        height={520}
        title="archai — internal packages"
        caption="click a node to focus its deps · flip to Sequence for a package's call flow"
      />

      <Markdown>{\`
## What you can put on this canvas

- **Graph** — any slice of the code graph; its header has a **Graph / Sequence**
  toggle that draws a package's call flow as a type-interaction diagram.
- **File** — a single source file with syntax highlighting and line numbers;
  collapsed by default, with an inline diff when it differs from main.
- **FileTree** — a mini file browser over a chosen subtree; click a file to open
  it. Expand to fullscreen with the ⛶ button.
- **Markdown · Mermaid · math** — prose, diagrams, and KaTeX.
\`}</Markdown>

      <FileTree
        root="internal/sequence"
        height={420}
        title="Browse a package"
        caption="internal/sequence — click a file to open it"
      />

      <Markdown>{\`
## Agent activity

The raw backend event stream, live — runs of the same event are folded, newest first.
\`}</Markdown>

      <ActivityFeed />
    </article>
  );
}
`;
