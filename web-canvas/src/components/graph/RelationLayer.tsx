'use client';

import { memo, useMemo } from 'react';
import type { Component, SymbolRelation } from '@/lib/graph/types';

export interface RelationLayerProps {
  relations: SymbolRelation[];
  components: Component[];
  expandedSet: ReadonlySet<string>;
  expandedInternals: ReadonlySet<string>;
  showDiff: boolean;
  focusId: string | null;
}

interface Anchor {
  x: number;
  y: number;
}

const COMPONENT_HEADER_H = 36;
const INTERNAL_HEADER_H = 26;
const MEMBER_ROW_H = 18;
const MEMBER_LIST_PAD_TOP = 2;

function componentCenter(cmp: Component): Anchor {
  return {
    x: (cmp.x ?? 0) + (cmp.w ?? 220) / 2,
    y: (cmp.y ?? 0) + (cmp.h ?? 86) / 2,
  };
}

function relationAnchor(
  componentsById: Map<string, Component>,
  expandedSet: ReadonlySet<string>,
  expandedInternals: ReadonlySet<string>,
  componentId: string,
  internalId?: string,
  memberId?: string
): Anchor | null {
  const cmp = componentsById.get(componentId);
  if (!cmp || cmp.x == null || cmp.y == null) return null;
  if (!internalId || !expandedSet.has(cmp.id)) return componentCenter(cmp);

  const internal = cmp.internals.find((item) => item.id === internalId);
  if (!internal || internal.x == null || internal.y == null) return componentCenter(cmp);

  const baseX = cmp.x + internal.x;
  const baseY = cmp.y + COMPONENT_HEADER_H + internal.y;
  if (memberId && expandedInternals.has(internal.id)) {
    const idx = (internal.members ?? []).findIndex((member) => member.id === memberId);
    if (idx >= 0) {
      return {
        x: baseX + (internal.w ?? 180) - 8,
        y: baseY + INTERNAL_HEADER_H + MEMBER_LIST_PAD_TOP + idx * MEMBER_ROW_H + MEMBER_ROW_H / 2,
      };
    }
  }

  return {
    x: baseX + (internal.w ?? 180) / 2,
    y: baseY + Math.min((internal.h ?? INTERNAL_HEADER_H) / 2, INTERNAL_HEADER_H / 2),
  };
}

function relationPath(from: Anchor, to: Anchor, sameComponent: boolean): { path: string; mid: Anchor } {
  if (sameComponent) {
    const lift = Math.max(36, Math.min(90, Math.abs(to.y - from.y) + 28));
    const c1 = { x: from.x + 46, y: from.y - lift };
    const c2 = { x: to.x - 46, y: to.y - lift };
    return {
      path: `M ${from.x} ${from.y} C ${c1.x} ${c1.y}, ${c2.x} ${c2.y}, ${to.x} ${to.y}`,
      mid: { x: (from.x + to.x) / 2, y: Math.min(from.y, to.y) - lift + 12 },
    };
  }
  const dx = Math.max(48, Math.abs(to.x - from.x) * 0.28);
  const fromBend = to.x >= from.x ? from.x + dx : from.x - dx;
  const toBend = to.x >= from.x ? to.x - dx : to.x + dx;
  return {
    path: `M ${from.x} ${from.y} C ${fromBend} ${from.y}, ${toBend} ${to.y}, ${to.x} ${to.y}`,
    mid: { x: (from.x + to.x) / 2, y: (from.y + to.y) / 2 - 10 },
  };
}

function RelationLayerImpl({
  relations,
  components,
  expandedSet,
  expandedInternals,
  showDiff,
  focusId,
}: RelationLayerProps) {
  const componentsById = useMemo(() => {
    const m = new Map<string, Component>();
    for (const c of components) m.set(c.id, c);
    return m;
  }, [components]);

  const isRelated = (relation: SymbolRelation) =>
    !focusId || relation.fromComponentId === focusId || relation.toComponentId === focusId;

  return (
    <svg className="relations-svg" width="100%" height="100%">
      <defs>
        {['rel-arr', 'rel-arr-add', 'rel-arr-rem', 'rel-arr-chg'].map((id) => (
          <marker
            key={id}
            id={`hf-${id}`}
            viewBox="0 0 10 10"
            refX="9"
            refY="5"
            markerWidth="6"
            markerHeight="6"
            orient="auto-start-reverse"
          >
            <path
              d="M 0 0 L 10 5 L 0 10 z"
              className={`hf-relation-arrow ${
                id === 'rel-arr-add'
                  ? 'added'
                  : id === 'rel-arr-rem'
                    ? 'removed'
                    : id === 'rel-arr-chg'
                      ? 'changed'
                      : ''
              }`}
            />
          </marker>
        ))}
      </defs>

      {relations.map((relation) => {
        const from = relationAnchor(
          componentsById,
          expandedSet,
          expandedInternals,
          relation.fromComponentId,
          relation.fromInternalId,
          relation.fromMemberId
        );
        const to = relationAnchor(
          componentsById,
          expandedSet,
          expandedInternals,
          relation.toComponentId,
          relation.toInternalId,
          relation.toMemberId
        );
        if (!from || !to) return null;

        const { path, mid } = relationPath(from, to, relation.fromComponentId === relation.toComponentId);
        const diffCls = showDiff && relation.diff ? relation.diff : '';
        const dimmed = focusId && !isRelated(relation);
        const marker =
          !showDiff || !relation.diff
            ? 'url(#hf-rel-arr)'
            : relation.diff === 'added'
              ? 'url(#hf-rel-arr-add)'
              : relation.diff === 'removed'
                ? 'url(#hf-rel-arr-rem)'
                : 'url(#hf-rel-arr-chg)';

        return (
          <g key={relation.id} className={dimmed ? 'hf-relation-dimmed' : ''}>
            <path d={path} className={`hf-relation ${diffCls}`} markerEnd={marker} />
            {focusId && (
              <text x={mid.x} y={mid.y} className="hf-relation-label" textAnchor="middle">
                {relation.kind}
              </text>
            )}
          </g>
        );
      })}
    </svg>
  );
}

/**
 * Memoized so viewport-only changes (pan/zoom re-render the parent) don't
 * recompute every relation path. Re-renders only when its own props change
 * (relations set, layout, expansion, focus).
 */
export const RelationLayer = memo(RelationLayerImpl);
