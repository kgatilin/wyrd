import type { UIGraph, Diff } from '../types';

/**
 * A change entry derived from graph elements with diff flags.
 */
export interface ChangeEntry {
  /** Unique ID for the change entry */
  id: string;
  /** The kind of change */
  kind: Diff;
  /** Display name of the changed element */
  name: string;
  /** Where the change is (e.g., "component - Ordering") */
  where: string;
  /** Component ID (for navigation) */
  cmp: string;
  /** Internal ID if the change is inside an internal */
  internal?: string;
  /** Member ID if the change is a member */
  member?: string;
  /** Port ID if the change is a port */
  port?: string;
}

/**
 * Derives the list of changes by walking the graph for elements with diff flags.
 * Ported from HF.deriveChanges in hifi-shared.jsx.
 */
export function deriveChanges(graph: UIGraph): ChangeEntry[] {
  const out: ChangeEntry[] = [];

  for (const c of graph.components) {
    // Find the BC name for context
    const bcName = graph.boundedContexts.find((b) => b.id === c.bc)?.name ?? c.bc;

    // Component-level change
    if (c.diff) {
      out.push({
        id: `cmp-${c.id}`,
        kind: c.diff,
        name: c.name,
        where: `component - ${bcName}`,
        cmp: c.id,
      });
    }

    // Internal-level changes
    for (const i of c.internals) {
      if (i.diff) {
        out.push({
          id: `int-${i.id}`,
          kind: i.diff,
          name: i.name,
          where: `${i.kind} - ${c.name}`,
          cmp: c.id,
          internal: i.id,
        });
      }

      // Member-level changes
      for (const m of i.members ?? []) {
        if (m.diff) {
          out.push({
            id: `mem-${m.id}`,
            kind: m.diff,
            name: m.name,
            where: `${m.kind} - ${i.name}`,
            cmp: c.id,
            internal: i.id,
            member: m.id,
          });
        }
      }
    }

    // Port-level changes
    for (const p of c.ports) {
      if (p.diff) {
        out.push({
          id: `port-${p.id}`,
          kind: p.diff,
          name: p.name,
          where: `port - ${c.name}`,
          cmp: c.id,
          port: p.id,
        });
      }
    }
  }

  // Edge-level changes
  for (const e of graph.edges) {
    if (e.diff) {
      const fromName = graph.components.find((c) => c.id === e.from)?.name ?? e.from;
      const toName = graph.components.find((c) => c.id === e.to)?.name ?? e.to;
      out.push({
        id: `edg-${e.id}`,
        kind: e.diff,
        name: `${fromName} -> ${toName}`,
        where: `connection - ${e.label || ''}`,
        cmp: e.from,
      });
    }
  }

  return out;
}

export interface ChangesPanelProps {
  /** The full graph (for PR info) */
  graph: UIGraph;
  /** Derived change entries */
  changes: ChangeEntry[];
  /** Currently active/selected change ID */
  activeChangeId: string | null;
  /** Callback when a change is clicked */
  onChangeClick: (change: ChangeEntry) => void;
}

/**
 * Panel showing PR summary and change cards.
 * Includes AGENT PR tag, title, stats, and clickable change rows.
 */
export function ChangesPanel({
  graph,
  changes,
  activeChangeId,
  onChangeClick,
}: ChangesPanelProps) {
  const pr = graph.pr;
  if (!pr) return null;

  return (
    <>
      {/* PR Summary block */}
      <div
        style={{
          padding: '12px 14px 8px',
          borderBottom: '1px solid var(--line-1)',
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            marginBottom: 6,
          }}
        >
          <span className="hf-pr-tag">AGENT PR</span>
          <span
            style={{
              fontSize: 10,
              color: 'var(--fg-2)',
              fontFamily: 'JetBrains Mono, monospace',
            }}
          >
            {pr.agent}
          </span>
        </div>
        <div
          style={{
            fontWeight: 600,
            fontSize: 12.5,
            color: 'var(--fg-0)',
            lineHeight: 1.35,
          }}
        >
          {pr.title}
        </div>
        <div
          style={{
            display: 'flex',
            gap: 4,
            marginTop: 8,
            flexWrap: 'wrap',
          }}
        >
          <span className="hf-stat add">+{pr.stats.added}</span>
          <span className="hf-stat rem">-{pr.stats.removed}</span>
          <span className="hf-stat chg">~{pr.stats.changed}</span>
        </div>
      </div>

      {/* Change list */}
      <div className="hf-list">
        {changes.map((ch) => (
          <div
            key={ch.id}
            className={`hf-card ${activeChangeId === ch.id ? 'active' : ''}`}
            onClick={() => onChangeClick(ch)}
          >
            <div className="hf-change-card">
              <div className="hf-change-row1">
                <span className={`hf-change-badge ${ch.kind}`}>
                  {ch.kind === 'added' ? '+' : ch.kind === 'removed' ? '-' : '~'}
                </span>
                <span className="hf-change-name">{ch.name}</span>
              </div>
              <div className="hf-change-where">{ch.where}</div>
            </div>
          </div>
        ))}
      </div>
    </>
  );
}
