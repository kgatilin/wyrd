import type { Component, Diff, SymbolRelation } from '../types';
import { COMPONENT_HEADER_H, symbolAnchor } from '../domain/cardAnchors';

export interface RelationLayerProps {
  relations: SymbolRelation[];
  components: Component[];
  expandedSet: ReadonlySet<string>;
  showDiff: boolean;
  focusId: string | null;
}

interface Anchor {
  x: number;
  y: number;
}

interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

// Layout writes the card's real size onto w/h for both states, so the rect is
// simply the laid geometry.
function componentRect(cmp: Component): Rect {
  return {
    x: cmp.x ?? 0,
    y: cmp.y ?? 0,
    w: cmp.w ?? 220,
    h: cmp.h ?? 86,
  };
}

function rectCenter(rect: Rect): Anchor {
  return { x: rect.x + rect.w / 2, y: rect.y + rect.h / 2 };
}

/**
 * The point where the segment center→toward crosses the rect border. Keeps a
 * collapsed card's arrows OUTSIDE the card, so the arrowhead lands visibly on
 * its edge instead of vanishing underneath.
 */
function borderPoint(rect: Rect, toward: Anchor): Anchor {
  const c = rectCenter(rect);
  const dx = toward.x - c.x;
  const dy = toward.y - c.y;
  if (dx === 0 && dy === 0) return c;
  const sx = dx !== 0 ? rect.w / 2 / Math.abs(dx) : Infinity;
  const sy = dy !== 0 ? rect.h / 2 / Math.abs(dy) : Infinity;
  const t = Math.min(sx, sy);
  return { x: c.x + dx * t, y: c.y + dy * t };
}

function relationAnchor(
  components: Component[],
  expandedSet: ReadonlySet<string>,
  componentId: string,
  internalId?: string,
  memberId?: string
): Anchor | null {
  const cmp = components.find((component) => component.id === componentId);
  if (!cmp || cmp.x == null || cmp.y == null) return null;
  if (!internalId || !expandedSet.has(cmp.id)) return rectCenter(componentRect(cmp));

  const anchor = symbolAnchor(cmp, internalId, memberId);
  if (!anchor) return rectCenter(componentRect(cmp));
  return { x: cmp.x + anchor.x, y: cmp.y + COMPONENT_HEADER_H + anchor.y };
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

/**
 * Slightly-bowed line between two border points. `bow` shifts the curve
 * perpendicular to the segment — opposite directions of a cyclic pair get
 * opposite bows, so A→B and B→A read as two distinct arrows.
 */
function bowedPath(from: Anchor, to: Anchor, bow: number): { path: string; mid: Anchor } {
  const mx = (from.x + to.x) / 2;
  const my = (from.y + to.y) / 2;
  const dx = to.x - from.x;
  const dy = to.y - from.y;
  const len = Math.sqrt(dx * dx + dy * dy) || 1;
  const nx = -dy / len;
  const ny = dx / len;
  const cx = mx + nx * bow;
  const cy = my + ny * bow;
  return {
    path: `M ${from.x} ${from.y} Q ${cx} ${cy} ${to.x} ${to.y}`,
    mid: { x: mx + nx * (bow + 10), y: my + ny * (bow + 10) },
  };
}

/** One aggregated arrow for all relations between a collapsed pair, per direction. */
interface AggregatedRelation {
  key: string;
  fromComponentId: string;
  toComponentId: string;
  kinds: Map<string, number>;
  diff: Diff | undefined;
  dimmedCandidate: boolean;
}

function aggregateLabel(kinds: Map<string, number>): string {
  return [...kinds.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([kind, count]) => (count > 1 ? `${kind} ×${count}` : kind))
    .join(' · ');
}

/** added > removed > changed — the most attention-worthy diff wins the color. */
function mergeDiff(current: Diff | undefined, next: Diff | undefined): Diff | undefined {
  const order: Record<string, number> = { added: 3, removed: 2, changed: 1 };
  if (!next) return current;
  if (!current || (order[next] ?? 0) > (order[current] ?? 0)) return next;
  return current;
}

function markerFor(showDiff: boolean, diff: Diff | undefined): string {
  if (!showDiff || !diff) return 'url(#hf-rel-arr)';
  if (diff === 'added') return 'url(#hf-rel-arr-add)';
  if (diff === 'removed') return 'url(#hf-rel-arr-rem)';
  return 'url(#hf-rel-arr-chg)';
}

export function RelationLayer({
  relations,
  components,
  expandedSet,
  showDiff,
  focusId,
}: RelationLayerProps) {
  const isRelated = (fromId: string, toId: string) => !focusId || fromId === focusId || toId === focusId;
  const byId = new Map(components.map((c) => [c.id, c]));
  const isCollapsed = (id: string) => !expandedSet.has(id);

  // Split: relations whose BOTH endpoints are collapsed aggregate into one
  // directed arrow per component pair (border-to-border, labeled with the
  // dependency kinds). Everything else renders individually, with collapsed
  // endpoints clamped to the card border so arrowheads stay visible.
  const aggregated = new Map<string, AggregatedRelation>();
  const detailed: SymbolRelation[] = [];
  for (const relation of relations) {
    const cross = relation.fromComponentId !== relation.toComponentId;
    if (cross && isCollapsed(relation.fromComponentId) && isCollapsed(relation.toComponentId)) {
      const key = `${relation.fromComponentId}->${relation.toComponentId}`;
      let agg = aggregated.get(key);
      if (!agg) {
        agg = {
          key,
          fromComponentId: relation.fromComponentId,
          toComponentId: relation.toComponentId,
          kinds: new Map(),
          diff: undefined,
          dimmedCandidate: false,
        };
        aggregated.set(key, agg);
      }
      agg.kinds.set(relation.kind, (agg.kinds.get(relation.kind) ?? 0) + 1);
      agg.diff = mergeDiff(agg.diff, showDiff ? relation.diff : undefined);
      continue;
    }
    // A self-relation inside a collapsed card has nowhere visible to go.
    if (!cross && isCollapsed(relation.fromComponentId)) continue;
    detailed.push(relation);
  }

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

      {[...aggregated.values()].map((agg) => {
        const fromCmp = byId.get(agg.fromComponentId);
        const toCmp = byId.get(agg.toComponentId);
        if (!fromCmp || !toCmp || fromCmp.x == null || toCmp.x == null) return null;
        const fromRect = componentRect(fromCmp);
        const toRect = componentRect(toCmp);
        const from = borderPoint(fromRect, rectCenter(toRect));
        const to = borderPoint(toRect, rectCenter(fromRect));
        // A cyclic pair renders as two opposite-bowed arcs instead of one
        // overlapping line — the cycle is visible at a glance.
        const hasReverse = aggregated.has(`${agg.toComponentId}->${agg.fromComponentId}`);
        const { path, mid } = bowedPath(from, to, hasReverse ? 14 : 6);
        const dimmed = focusId && !isRelated(agg.fromComponentId, agg.toComponentId);
        return (
          <g key={agg.key} className={dimmed ? 'hf-relation-dimmed' : ''}>
            <path
              d={path}
              className={`hf-relation hf-relation-agg ${agg.diff ?? ''}`}
              markerEnd={markerFor(showDiff, agg.diff)}
            />
            <text x={mid.x} y={mid.y} className="hf-relation-label" textAnchor="middle">
              {aggregateLabel(agg.kinds)}
            </text>
          </g>
        );
      })}

      {detailed.map((relation) => {
        let from = relationAnchor(
          components,
          expandedSet,
          relation.fromComponentId,
          relation.fromInternalId,
          relation.fromMemberId
        );
        let to = relationAnchor(
          components,
          expandedSet,
          relation.toComponentId,
          relation.toInternalId,
          relation.toMemberId
        );
        if (!from || !to) return null;

        // Clamp collapsed endpoints from the card center to its border, so the
        // line and its arrowhead stop AT the card instead of under it.
        const fromCmp = byId.get(relation.fromComponentId);
        const toCmp = byId.get(relation.toComponentId);
        if (fromCmp && isCollapsed(fromCmp.id)) {
          from = borderPoint(componentRect(fromCmp), to);
        }
        if (toCmp && isCollapsed(toCmp.id)) {
          to = borderPoint(componentRect(toCmp), from);
        }

        const { path, mid } = relationPath(from, to, relation.fromComponentId === relation.toComponentId);
        const diffCls = showDiff && relation.diff ? relation.diff : '';
        const dimmed = focusId && !isRelated(relation.fromComponentId, relation.toComponentId);
        const showLabel = focusId || isCollapsed(relation.toComponentId) || isCollapsed(relation.fromComponentId);

        return (
          <g key={relation.id} className={dimmed ? 'hf-relation-dimmed' : ''}>
            <path
              d={path}
              className={`hf-relation ${diffCls}`}
              markerEnd={markerFor(showDiff, showDiff ? relation.diff : undefined)}
            />
            {showLabel && (
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
