import { useEffect, useMemo, useRef, useState } from 'react';
import type { UIGraph } from '../types';
import type { ArchMotifScope } from '../domain/state';
import type { DomainsPort, LensPort } from '../domain/ports';
import type { SymbolFocusTarget } from '../domain/symbolFocus';
import { isLensPending } from '../data/lens';
import {
  buildDomainGrid,
  type DomainCell,
  type DomainGrid,
  type DomainMember,
  type RawLatentDomains,
} from '../domain/archMotifDomains';

/**
 * The domains canvas: the repository's structural clusters laid against its
 * semantic ones. It answers a question the review canvas cannot — *are the
 * module boundaries the same boundaries the subject matter has?* — and, when
 * they are not, names the shared helpers fusing them.
 *
 * Both partitions come from one solve on the daemon. Asking for them
 * separately would make the grid depend on the solver returning the same
 * partition twice running.
 *
 * Two ports, because they are two transports. Readiness is the `status` tool
 * over the LensPort: it is cheap, and it says which of a cold daemon's two slow
 * phases is in the way. The partition is the DomainsPort, which reads the
 * daemon's own endpoint — the tool endpoint clamps a result at 256 KiB to
 * protect an agent's context window, and a full partition does not fit under it
 * on a repository of any size.
 */

/** How often to re-ask while the daemon is still building its index. */
const POLL_MS = 3000;

type DomainsSession =
  | { status: 'loading'; phase?: string }
  | { status: 'indexing'; embedded: number; embeddable: number; message: string }
  | { status: 'no_embedder'; message: string }
  | { status: 'error'; error: string }
  | { status: 'ready'; raw: RawLatentDomains };

/** The `status` tool's payload, as far as readiness is concerned. */
interface RawStatus {
  ready?: boolean;
  indexing?: boolean;
  dense_available?: boolean;
  embedded?: number;
  embeddable?: number;
  message?: string;
}

export interface ArchMotifCanvasProps {
  graph: UIGraph;
  worktree: string;
  scope: ArchMotifScope;
  /** True when the worktree has a review base, so a diff region exists. */
  hasBase: boolean;
  /** Readiness (`status`), so the canvas reports a cold daemon's progress. */
  lens: LensPort;
  /** The partition itself. */
  domains: DomainsPort;
  /**
   * The wiring panel is up over the grid. While it is, it owns the keyboard —
   * Esc dismisses the panel, not the canvas underneath it.
   */
  symbolPanelOpen: boolean;
  onScopeChange: (scope: ArchMotifScope) => void;
  onSymbolFocus: (target: SymbolFocusTarget) => void;
  onClose: () => void;
}

export function ArchMotifCanvas({
  graph,
  worktree,
  scope,
  hasBase,
  lens,
  domains,
  symbolPanelOpen,
  onScopeChange,
  onSymbolFocus,
  onClose,
}: ArchMotifCanvasProps) {
  const session = useDomainsSession(lens, domains, worktree, scope, graph);
  const [selectedCell, setSelectedCell] = useState<string | null>(null);
  const [hoveredCell, setHoveredCell] = useState<string | null>(null);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (symbolPanelOpen) return;
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose, symbolPanelOpen]);

  // A new question is a new grid; the previous selection means nothing in it.
  useEffect(() => setSelectedCell(null), [scope.kind, scope.package]);

  const grid = useMemo(
    () => (session.status === 'ready' ? buildDomainGrid(session.raw, graph) : null),
    [session, graph]
  );

  const activeCell = selectedCell ?? hoveredCell;
  const packageIds = useMemo(
    () => graph.components.map((component) => component.id).sort((a, b) => a.localeCompare(b)),
    [graph.components]
  );

  return (
    <div className="hf-domains">
      <Header
        grid={grid}
        scope={scope}
        hasBase={hasBase}
        packageIds={packageIds}
        onScopeChange={onScopeChange}
        onClose={onClose}
      />
      {grid ? (
        <Grid
          grid={grid}
          activeCell={activeCell}
          selectedCell={selectedCell}
          onSelect={(id) => setSelectedCell((current) => (current === id ? null : id))}
          onHover={setHoveredCell}
          onSymbolFocus={onSymbolFocus}
        />
      ) : (
        <Readiness session={session} />
      )}
    </div>
  );
}

/**
 * The scope switch, the verdict figures, and the glue. Only the switch and the
 * close button survive when there is no grid: half a grid — the structural
 * side alone — would answer a different question than the one asked.
 */
function Header({
  grid,
  scope,
  hasBase,
  packageIds,
  onScopeChange,
  onClose,
}: {
  grid: DomainGrid | null;
  scope: ArchMotifScope;
  hasBase: boolean;
  packageIds: string[];
  onScopeChange: (scope: ArchMotifScope) => void;
  onClose: () => void;
}) {
  const header = grid?.header;
  return (
    <div className="hf-domains-head">
      <div className="hf-domains-title">
        <span className="hf-domains-label">DOMAINS</span>
        {header && (
          <span className={`hf-domains-verdict ${verdictClass(header.verdict)}`} title={header.glueNote}>
            {verdictLabel(header.verdict)}
          </span>
        )}
      </div>

      {header && (
        <div className="hf-domains-stats">
          <span className="hf-domains-stat" title="Adjusted mutual information: how much the two partitions agree, corrected for chance">
            AMI {header.ami.toFixed(2)}
          </span>
          <span
            className={`hf-domains-stat ${header.structuralModularity < header.semanticModularity ? 'warn' : ''}`}
            title="Newman modularity of each partition. Structural below semantic means a blob hiding real domains."
          >
            Q {header.structuralModularity.toFixed(2)} struct / {header.semanticModularity.toFixed(2)} sem
          </span>
          <span
            className={`hf-domains-stat ${header.structuralDominantShare >= 0.45 ? 'warn' : ''}`}
            title="Share of the nodes in the largest structural cluster"
          >
            blob {Math.round(header.structuralDominantShare * 100)}%
          </span>
          <span className="hf-domains-stat" title="Symbols clustered, and symbols skipped for want of an embedding">
            {header.nodeCount} nodes
            {header.droppedNodes > 0 ? ` · ${header.droppedNodes} dropped` : ''}
          </span>
          {grid && grid.diff.changedMembers > 0 && (
            <span
              className="hf-domains-stat diff"
              title="One cell means a local change; several mean the change cuts across domains."
            >
              {grid.diff.changedMembers} changed in {grid.diff.cells}{' '}
              {grid.diff.cells === 1 ? 'cell' : 'cells'} ({grid.diff.structuralClusters}×
              {grid.diff.semanticClusters})
            </span>
          )}
        </div>
      )}

      <div className="hf-domains-scope">
        <button
          className={`hf-domains-scope-btn ${scope.kind === 'diff' ? 'on' : ''}`}
          onClick={() => onScopeChange({ kind: 'diff' })}
          disabled={!hasBase}
          title={hasBase ? 'The region this branch’s changes pull on' : 'No review base to diff against'}
        >
          diff region
        </button>
        <button
          className={`hf-domains-scope-btn ${scope.kind === 'repo' ? 'on' : ''}`}
          onClick={() => onScopeChange({ kind: 'repo' })}
          title="The whole repository, types and functions only"
        >
          repo
        </button>
        <select
          className={`hf-domains-scope-pkg ${scope.kind === 'package' ? 'on' : ''}`}
          value={scope.kind === 'package' ? (scope.package ?? '') : ''}
          onChange={(e) => e.target.value && onScopeChange({ kind: 'package', package: e.target.value })}
          title="One package and its subpackages"
        >
          <option value="">package …</option>
          {packageIds.map((id) => (
            <option key={id} value={id}>
              {id}
            </option>
          ))}
        </select>
      </div>

      <button className="hf-domains-close" onClick={onClose} title="Back to the review canvas (Esc)">
        ×
      </button>

      {header && header.glue.length > 0 && (
        <div className="hf-domains-glue">
          <span className="hf-domains-glue-label">glue</span>
          {header.glue.map((member) => (
            <span key={member.id} className="hf-domains-glue-node" title={member.internalId}>
              {member.name}
              <span className="hf-domains-fanin">×{member.fanIn}</span>
            </span>
          ))}
          <span className="hf-domains-glue-note">
            highest structural fan-in — pull to a thin boundary and the domains separate
          </span>
        </div>
      )}
    </div>
  );
}

/** What the canvas says while it has no grid to draw. */
function Readiness({ session }: { session: DomainsSession }) {
  switch (session.status) {
    case 'no_embedder':
      return (
        <div className="hf-domains-state no-embedder">
          <div className="hf-domains-state-title">The semantic side needs an embedder</div>
          <div className="hf-domains-state-body">
            This grid is the structural partition read against the semantic one. Without embeddings
            there is no semantic side, and the structural half on its own answers a different
            question. Configure an embedder and refresh the index.
          </div>
          {session.message && <div className="hf-domains-state-detail">{session.message}</div>}
        </div>
      );
    case 'indexing':
      return (
        <div className="hf-domains-state indexing">
          <div className="hf-domains-state-title">
            Indexing {session.embedded}/{session.embeddable}
          </div>
          <div className="hf-domains-state-body">
            The dense pass is still running. This retries on its own.
          </div>
          {session.message && <div className="hf-domains-state-detail">{session.message}</div>}
        </div>
      );
    case 'error':
      return (
        <div className="hf-domains-state error">
          <div className="hf-domains-state-title">Clustering failed</div>
          <div className="hf-domains-state-detail">{session.error}</div>
        </div>
      );
    case 'loading':
      return (
        <div className="hf-domains-state loading">
          <div className="hf-domains-state-title">Clustering…</div>
          <div className="hf-domains-state-body">
            {session.phase === 'parsing'
              ? 'The daemon is still parsing this worktree.'
              : 'Both partitions come from one pass, so the answer arrives whole.'}
          </div>
        </div>
      );
    default:
      // Ready: the grid is drawn in place of this.
      return null;
  }
}

function Grid({
  grid,
  activeCell,
  selectedCell,
  onSelect,
  onHover,
  onSymbolFocus,
}: {
  grid: DomainGrid;
  activeCell: string | null;
  selectedCell: string | null;
  onSelect: (id: string) => void;
  onHover: (id: string | null) => void;
  onSymbolFocus: (target: SymbolFocusTarget) => void;
}) {
  // Only the active cell's flow is drawn: every cross-cell edge at once is the
  // hairball the grid exists to replace.
  const edges = activeCell
    ? grid.edges.filter((edge) => edge.from === activeCell || edge.to === activeCell)
    : [];

  if (grid.cells.length === 0) {
    return (
      <div className="hf-domains-state empty">
        <div className="hf-domains-state-title">Nothing to cluster in this scope</div>
      </div>
    );
  }

  return (
    <div className="hf-domains-body">
      <div className="hf-domains-grid" style={{ width: grid.width, height: grid.height }}>
        <svg className="hf-domains-edges" width={grid.width} height={grid.height}>
          <defs>
            <marker id="hf-dom-arrow" markerWidth="8" markerHeight="8" refX="7" refY="3" orient="auto">
              <path d="M0,0 L7,3 L0,6 z" />
            </marker>
          </defs>
          {edges.map((edge) => (
            <line
              key={edge.id}
              className={`hf-domains-edge ${edge.from === activeCell ? 'out' : 'in'}`}
              x1={edge.x1}
              y1={edge.y1}
              x2={edge.x2}
              y2={edge.y2}
              strokeWidth={Math.min(6, 1 + Math.log2(edge.weight + 1))}
              markerEnd="url(#hf-dom-arrow)"
            >
              <title>{`${edge.from} → ${edge.to}: ${edge.weight}`}</title>
            </line>
          ))}
        </svg>

        {grid.cols.map((band) => (
          <div
            key={band.label}
            className="hf-domains-colhead"
            style={{ left: band.offset, top: 0, width: band.extent, height: grid.colHeaderHeight }}
            title="Semantic cluster — what this code is about"
          >
            <span className="hf-domains-band-name">{band.label}</span>
            <span className="hf-domains-band-size">{band.size}</span>
          </div>
        ))}

        {grid.rows.map((band) => (
          <div
            key={band.label}
            className="hf-domains-rowhead"
            style={{ left: 0, top: band.offset, width: grid.rowHeaderWidth, height: band.extent }}
            title="Structural cluster — how this code is wired"
          >
            <span className="hf-domains-band-name">{band.label}</span>
            <span className="hf-domains-band-size">{band.size}</span>
          </div>
        ))}

        {grid.cells.map((cell) => (
          <Cell
            key={cell.id}
            cell={cell}
            selected={selectedCell === cell.id}
            active={activeCell === cell.id}
            onSelect={onSelect}
            onHover={onHover}
            onSymbolFocus={onSymbolFocus}
          />
        ))}
      </div>
    </div>
  );
}

function Cell({
  cell,
  selected,
  active,
  onSelect,
  onHover,
  onSymbolFocus,
}: {
  cell: DomainCell;
  selected: boolean;
  active: boolean;
  onSelect: (id: string) => void;
  onHover: (id: string | null) => void;
  onSymbolFocus: (target: SymbolFocusTarget) => void;
}) {
  const classes = [
    'hf-domains-cell',
    cell.onDiagonal ? 'diagonal' : 'off-diagonal',
    selected ? 'selected' : '',
    active ? 'active' : '',
    cell.changedCount > 0 ? 'changed' : '',
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <div
      className={classes}
      style={{ left: cell.x, top: cell.y, width: cell.width, height: cell.height }}
      onMouseEnter={() => onHover(cell.id)}
      onMouseLeave={() => onHover(null)}
      onClick={() => onSelect(cell.id)}
      title={`Structural S${cell.structuralCluster} × semantic M${cell.semanticCluster}`}
    >
      <div className="hf-domains-cell-head">
        <span className="hf-domains-cell-id">
          S{cell.structuralCluster}·M{cell.semanticCluster}
        </span>
        <span className="hf-domains-cell-size">{cell.size}</span>
        {cell.glueCount > 0 && <span className="hf-domains-cell-glue">glue {cell.glueCount}</span>}
        {cell.changedCount > 0 && <span className="hf-domains-cell-diff">Δ{cell.changedCount}</span>}
      </div>
      <div className="hf-domains-cell-body">
        {cell.packages.map((block) => (
          <div className="hf-domains-pkg" key={block.componentId || block.name}>
            <div className="hf-domains-pkg-head" title={block.componentId}>
              {block.name}
            </div>
            {block.members.map((member) => (
              <SymbolRow key={member.id} member={member} onSymbolFocus={onSymbolFocus} />
            ))}
          </div>
        ))}
        {cell.overflow > 0 && <div className="hf-domains-more">+{cell.overflow} more</div>}
      </div>
    </div>
  );
}

function SymbolRow({
  member,
  onSymbolFocus,
}: {
  member: DomainMember;
  onSymbolFocus: (target: SymbolFocusTarget) => void;
}) {
  const classes = [
    'hf-domains-sym',
    member.glue ? 'glue' : '',
    member.inGraph ? 'walkable' : 'off-graph',
    member.diff ? `diff-${member.diff}` : '',
  ]
    .filter(Boolean)
    .join(' ');
  return (
    <div
      className={classes}
      onClick={(event) => {
        // The cell owns the background click (selection); a symbol owns its own.
        event.stopPropagation();
        if (!member.inGraph) return;
        onSymbolFocus({ componentId: member.componentId, internalId: member.internalId });
      }}
      title={member.inGraph ? `${member.internalId} — open its wiring` : `${member.internalId} — not in the loaded graph`}
    >
      <span className="hf-domains-sym-kind">{kindLabel(member.kind)}</span>
      <span className="hf-domains-sym-name">{member.name}</span>
      {member.glue && (
        <span className="hf-domains-sym-fanin" title="Incoming flow edges from inside this scope">
          ×{member.fanIn}
        </span>
      )}
    </div>
  );
}

/**
 * Readiness, then the partition. `status` is cheap and says which of the two
 * slow phases the daemon is in, so the canvas reports progress rather than
 * sitting on a call that cannot answer yet — and a call that fails is shown as
 * a failure rather than as a canvas that never fills in.
 */
function useDomainsSession(
  lens: LensPort,
  domains: DomainsPort,
  worktree: string,
  scope: ArchMotifScope,
  graph: UIGraph
): DomainsSession {
  const [session, setSession] = useState<DomainsSession>({ status: 'loading' });
  const scopeKey = `${scope.kind}:${scope.package ?? ''}`;
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    let cancelled = false;

    const schedule = () => {
      timer.current = setTimeout(() => {
        if (!cancelled) void run();
      }, POLL_MS);
    };

    const run = async (): Promise<void> => {
      try {
        const status = (await lens.call('status', {}, { worktree })) as RawStatus | null;
        if (cancelled) return;
        if (isLensPending(status)) {
          setSession({ status: 'loading', phase: status.phase });
          schedule();
          return;
        }
        if (status?.indexing) {
          setSession({
            status: 'indexing',
            embedded: status.embedded ?? 0,
            embeddable: status.embeddable ?? 0,
            message: status.message ?? '',
          });
          schedule();
          return;
        }
        if (status && status.dense_available === false) {
          setSession({ status: 'no_embedder', message: status.message ?? '' });
          return;
        }

        const payload = await domains.load(scope, { worktree });
        if (cancelled) return;
        setSession({ status: 'ready', raw: payload });
      } catch (err) {
        if (cancelled) return;
        setSession({ status: 'error', error: err instanceof Error ? err.message : String(err) });
      }
    };

    setSession({ status: 'loading' });
    void run();
    return () => {
      cancelled = true;
      if (timer.current) clearTimeout(timer.current);
    };
    // `graph` is a dependency on purpose: a reload means the working tree
    // moved, and a grid describing the previous one is worse than none.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lens, domains, worktree, scopeKey, graph]);

  return session;
}

function kindLabel(kind: string): string {
  switch (kind) {
    case 'class':
      return 'struct';
    case 'consts':
      return 'const';
    case 'vars':
      return 'var';
    case 'errors':
      return 'error';
    default:
      return kind;
  }
}

function verdictLabel(verdict: string): string {
  switch (verdict) {
    case 'latent_domains_glued':
      return 'latent domains glued';
    case 'aligned':
      return 'aligned';
    case 'diverging':
      return 'diverging';
    default:
      return verdict;
  }
}

function verdictClass(verdict: string): string {
  switch (verdict) {
    case 'latent_domains_glued':
      return 'glued';
    case 'aligned':
      return 'aligned';
    default:
      return 'diverging';
  }
}
