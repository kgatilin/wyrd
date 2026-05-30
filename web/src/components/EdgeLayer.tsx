import type { Edge, Component } from '../types';
import { computeExpandedHeight } from '../state/hooks';

export interface EdgeLayerProps {
  /** All edges to render */
  edges: Edge[];
  /** All components (for port lookups) */
  components: Component[];
  /** Set of expanded component IDs */
  expandedSet: Set<string>;
  /** Set of expanded internal IDs (for height calculation) */
  expandedInternals: Set<string>;
  /** Whether to show diff styling */
  showDiff: boolean;
  /** Currently focused component ID (null = none) */
  focusId: string | null;
  /** Whether to show animated flow dots */
  flow?: boolean;
  /** Set of IDs that have comments (for markers on edges) */
  commentTargets?: Set<string>;
  /** Callback to add a comment on an edge */
  onAddComment?: (target: { type: string; id: string }, event: React.MouseEvent) => void;
}

interface EdgePath {
  path: string;
  s: { x: number; y: number; side: string };
  d: { x: number; y: number; side: string };
  mid: { x: number; y: number };
}

/**
 * Computes the bezier path between two component ports.
 */
function computeEdgePath(
  edge: Edge,
  components: Component[],
  expandedSet: Set<string>,
  expandedInternals: Set<string>
): EdgePath | null {
  const src = components.find((c) => c.id === edge.from);
  const dst = components.find((c) => c.id === edge.to);
  if (!src || !dst) return null;

  const portFor = (
    cmp: Component,
    portId: string
  ): { x: number; y: number; side: string } | null => {
    const port = cmp.ports.find((p) => p.id === portId);
    if (!port) return null;

    const isExp = expandedSet.has(cmp.id);
    const w = isExp ? (cmp.wx ?? cmp.w ?? 220) : (cmp.w ?? 220);
    const h = isExp ? computeExpandedHeight(cmp, expandedInternals) : (cmp.h ?? 86);
    const y = isExp ? (cmp.y ?? 0) + (port.y ?? 58) : (cmp.y ?? 0) + h / 2;
    const x = port.side === 'left' ? (cmp.x ?? 0) : (cmp.x ?? 0) + w;

    return { x, y, side: port.side };
  };

  const s = portFor(src, edge.fromPort) ?? {
    x: (src.x ?? 0) + (src.w ?? 220),
    y: (src.y ?? 0) + (src.h ?? 86) / 2,
    side: 'right',
  };
  const d = portFor(dst, edge.toPort) ?? {
    x: dst.x ?? 0,
    y: (dst.y ?? 0) + (dst.h ?? 86) / 2,
    side: 'left',
  };

  // Calculate bezier control points
  const dx = Math.max(40, Math.abs(d.x - s.x) * 0.4);
  const sx2 = s.side === 'right' ? s.x + dx : s.x - dx;
  const dx2 = d.side === 'left' ? d.x - dx : d.x + dx;

  return {
    path: `M ${s.x} ${s.y} C ${sx2} ${s.y}, ${dx2} ${d.y}, ${d.x} ${d.y}`,
    s,
    d,
    mid: { x: (s.x + d.x) / 2, y: (s.y + d.y) / 2 - 6 },
  };
}

/**
 * SVG layer rendering edges between component ports.
 * Includes bezier paths, arrow markers, diff classes, flow dots, and labels.
 */
export function EdgeLayer({
  edges,
  components,
  expandedSet,
  expandedInternals,
  showDiff,
  focusId,
  flow = false,
  commentTargets,
  onAddComment,
}: EdgeLayerProps) {
  const hasComment = (id: string) => commentTargets?.has(id) ?? false;
  const isRelated = (e: Edge) => !focusId || e.from === focusId || e.to === focusId;

  const handleHitClick = (edge: Edge, e: React.MouseEvent<SVGPathElement>) => {
    e.stopPropagation();
    onAddComment?.({ type: 'edge', id: edge.id }, e as unknown as React.MouseEvent);
  };

  return (
    <svg className="edges-svg" width="100%" height="100%">
      <defs>
        {/* Arrow markers for different diff states */}
        {['arr', 'arr-add', 'arr-rem', 'arr-chg'].map((id) => (
          <marker
            key={id}
            id={`hf-${id}`}
            viewBox="0 0 10 10"
            refX="9"
            refY="5"
            markerWidth="7"
            markerHeight="7"
            orient="auto-start-reverse"
          >
            <path
              d="M 0 0 L 10 5 L 0 10 z"
              className={`hf-edge-arrow ${
                id === 'arr-add'
                  ? 'added'
                  : id === 'arr-rem'
                    ? 'removed'
                    : id === 'arr-chg'
                      ? 'changed'
                      : ''
              }`}
            />
          </marker>
        ))}
      </defs>

      {edges.map((edge, i) => {
        const r = computeEdgePath(edge, components, expandedSet, expandedInternals);
        if (!r) return null;

        const diffCls = showDiff && edge.diff ? edge.diff : '';
        const marker =
          !showDiff || !edge.diff
            ? 'url(#hf-arr)'
            : edge.diff === 'added'
              ? 'url(#hf-arr-add)'
              : edge.diff === 'removed'
                ? 'url(#hf-arr-rem)'
                : 'url(#hf-arr-chg)';

        const focused = focusId && isRelated(edge);
        const dimmed = focusId && !isRelated(edge);

        return (
          <g
            key={edge.id}
            className={`${focused ? 'hf-edge-focused' : ''} ${dimmed ? 'hf-edge-dimmed' : ''}`}
          >
            {/* Main path */}
            <path
              id={`epath-${edge.id}`}
              d={r.path}
              className={`hf-edge ${diffCls}`}
              markerEnd={marker}
            />

            {/* Flow dot animation */}
            {flow && !dimmed && (
              <circle
                r="3"
                className={`hf-flow-dot ${diffCls}`}
                style={{
                  offsetPath: `path("${r.path}")`,
                  animationDelay: `${i * 0.4}s`,
                }}
              />
            )}

            {/* Edge label */}
            {edge.label && (
              <text
                x={r.mid.x}
                y={r.mid.y}
                className="hf-edge-label"
                textAnchor="middle"
              >
                {edge.label}
              </text>
            )}

            {/* Comment marker on edge */}
            {hasComment(edge.id) && (
              <g transform={`translate(${r.mid.x + 18} ${r.mid.y - 14})`}>
                <rect
                  x="-7"
                  y="-9"
                  width="14"
                  height="14"
                  rx="6"
                  fill="var(--accent)"
                  stroke="var(--bg-0)"
                  strokeWidth="1.5"
                />
                <text
                  x="0"
                  y="2"
                  textAnchor="middle"
                  fontFamily="JetBrains Mono, monospace"
                  fontSize="9"
                  fontWeight="700"
                  fill="white"
                >
                  !
                </text>
              </g>
            )}

            {/* Invisible hit path for click detection */}
            <path
              d={r.path}
              className="hf-edge-hit"
              onClick={(e) => handleHitClick(edge, e)}
            />
          </g>
        );
      })}
    </svg>
  );
}
