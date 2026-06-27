'use client';

import { memo } from 'react';
import type { Edge, Component } from '@/lib/graph/types';

export interface EdgeLayerProps {
  edges: Edge[];
  components: Component[];
  expandedSet: ReadonlySet<string>;
  showDiff: boolean;
  focusId: string | null;
}

interface EdgePath {
  path: string;
  s: { x: number; y: number; side: string };
  d: { x: number; y: number; side: string };
  mid: { x: number; y: number };
}

/**
 * Builds an SVG path string for an orthogonal polyline through the given points.
 */
function buildOrthogonalPath(pts: { x: number; y: number }[]): {
  path: string;
  mid: { x: number; y: number };
} {
  const r = 6;
  const n = pts.length;

  let d = `M ${pts[0].x} ${pts[0].y}`;
  for (let i = 1; i < n - 1; i++) {
    const prev = pts[i - 1];
    const cur = pts[i];
    const next = pts[i + 1];

    const dx1 = cur.x - prev.x;
    const dy1 = cur.y - prev.y;
    const dx2 = next.x - cur.x;
    const dy2 = next.y - cur.y;
    const len1 = Math.sqrt(dx1 * dx1 + dy1 * dy1);
    const len2 = Math.sqrt(dx2 * dx2 + dy2 * dy2);

    if (len1 === 0 || len2 === 0) {
      d += ` L ${cur.x} ${cur.y}`;
      continue;
    }

    const actualR = Math.min(r, len1 / 2, len2 / 2);
    const approachX = cur.x - (dx1 / len1) * actualR;
    const approachY = cur.y - (dy1 / len1) * actualR;
    const departX = cur.x + (dx2 / len2) * actualR;
    const departY = cur.y + (dy2 / len2) * actualR;

    d += ` L ${approachX} ${approachY} Q ${cur.x} ${cur.y} ${departX} ${departY}`;
  }
  d += ` L ${pts[n - 1].x} ${pts[n - 1].y}`;

  const midIdx = Math.floor((n - 1) / 2);
  const p0 = pts[midIdx];
  const p1 = pts[midIdx + 1];
  const mid = {
    x: (p0.x + p1.x) / 2,
    y: (p0.y + p1.y) / 2 - 6,
  };

  return { path: d, mid };
}

/**
 * Computes the SVG path between two component ports.
 */
function computeEdgePath(
  edge: Edge,
  components: Component[],
  expandedSet: ReadonlySet<string>
): EdgePath | null {
  // ELK-routed path
  if (edge.points && edge.points.length >= 2) {
    const { path, mid } = buildOrthogonalPath(edge.points);
    const first = edge.points[0];
    const last = edge.points[edge.points.length - 1];
    return {
      path,
      s: { x: first.x, y: first.y, side: 'right' },
      d: { x: last.x, y: last.y, side: 'left' },
      mid,
    };
  }

  // Fallback: bezier from port positions
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
    const h = cmp.h ?? 86;
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

  const ddx = Math.max(40, Math.abs(d.x - s.x) * 0.4);
  const sx2 = s.side === 'right' ? s.x + ddx : s.x - ddx;
  const dx2 = d.side === 'left' ? d.x - ddx : d.x + ddx;

  return {
    path: `M ${s.x} ${s.y} C ${sx2} ${s.y}, ${dx2} ${d.y}, ${d.x} ${d.y}`,
    s,
    d,
    mid: { x: (s.x + d.x) / 2, y: (s.y + d.y) / 2 - 6 },
  };
}

function EdgeLayerImpl({
  edges,
  components,
  expandedSet,
  showDiff,
  focusId,
}: EdgeLayerProps) {
  const isRelated = (e: Edge) => !focusId || e.from === focusId || e.to === focusId;

  return (
    <svg className="edges-svg" width="100%" height="100%">
      <defs>
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

      {edges.map((edge) => {
        const r = computeEdgePath(edge, components, expandedSet);
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
            <path
              id={`epath-${edge.id}`}
              d={r.path}
              className={`hf-edge ${diffCls}`}
              markerEnd={marker}
            />
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
          </g>
        );
      })}
    </svg>
  );
}

/** Memoized so pan/zoom re-renders of the parent don't recompute edge paths. */
export const EdgeLayer = memo(EdgeLayerImpl);
