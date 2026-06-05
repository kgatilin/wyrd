import type { UIGraph, Component, Edge, BoundedContext } from '../types';

export interface LayoutPin {
  x: number;
  y: number;
}

export type LayoutPins = Record<string, LayoutPin>;

const STORAGE_PREFIX = 'archai:review-layout:v1';
const GROUP_PADDING = 30;
const PIN_COLLISION_PADDING = 24;

export function buildLayoutScopeKey(
  graph: UIGraph,
  reviewViewId: string | null,
  reviewScopeId: string | null,
  reviewGroupingId: string | null
): string {
  return [
    STORAGE_PREFIX,
    keyPart(graph.repo?.root ?? graph.schema ?? 'graph'),
    keyPart(reviewViewId ?? graph.defaultReviewView ?? 'default-view'),
    keyPart(reviewScopeId ?? graph.defaultReviewScope ?? 'default-scope'),
    keyPart(reviewGroupingId ?? graph.defaultGrouping ?? 'default-grouping'),
  ].join(':');
}

export function buildLayoutRepoKeyPrefix(graph: UIGraph): string {
  return [
    STORAGE_PREFIX,
    keyPart(graph.repo?.root ?? graph.schema ?? 'graph'),
    '',
  ].join(':');
}

export function loadLayoutPins(key: string, storage: Storage | null = browserStorage()): LayoutPins {
  if (!storage) return {};
  try {
    return normalizePins(JSON.parse(storage.getItem(key) ?? '{}'));
  } catch {
    return {};
  }
}

export function saveLayoutPins(
  key: string,
  pins: LayoutPins,
  storage: Storage | null = browserStorage()
): void {
  if (!storage) return;
  const normalized = normalizePins(pins);
  if (Object.keys(normalized).length === 0) {
    storage.removeItem(key);
    return;
  }
  storage.setItem(key, JSON.stringify(normalized));
}

export function clearLayoutPinsForRepo(graph: UIGraph, storage: Storage | null = browserStorage()): void {
  if (!storage) return;
  const prefix = buildLayoutRepoKeyPrefix(graph);
  for (let i = storage.length - 1; i >= 0; i -= 1) {
    const key = storage.key(i);
    if (key?.startsWith(prefix)) storage.removeItem(key);
  }
}

export function applyLayoutPins(graph: UIGraph, pins: LayoutPins): UIGraph {
  const normalized = normalizePins(pins);
  if (Object.keys(normalized).length === 0) return graph;

  const deltas = new Map<string, { dx: number; dy: number }>();
  let components = graph.components.map((component) => {
    const pin = normalized[component.id];
    if (!pin || component.x == null || component.y == null) return component;
    const dx = pin.x - component.x;
    const dy = pin.y - component.y;
    if (dx !== 0 || dy !== 0) deltas.set(component.id, { dx, dy });
    return { ...component, x: pin.x, y: pin.y };
  });
  components = placeUnpinnedAroundPins(components, normalized, deltas);

  return {
    ...graph,
    boundedContexts: recomputePinnedContexts(graph.boundedContexts, components, normalized, deltas),
    components,
    edges: graph.edges.map((edge) => applyEdgeEndpointDeltas(edge, deltas)),
  };
}

function placeUnpinnedAroundPins(
  components: Component[],
  pins: LayoutPins,
  deltas: Map<string, { dx: number; dy: number }>
): Component[] {
  const pinnedIds = new Set(Object.keys(pins));
  if (pinnedIds.size === 0) return components;

  const blockers: Rect[] = [];
  for (const component of components) {
    if (!pinnedIds.has(component.id)) continue;
    const rect = componentRect(component);
    if (rect) blockers.push(rect);
  }
  if (blockers.length === 0) return components;

  return components.map((component) => {
    const rect = componentRect(component);
    if (!rect) return component;
    if (pinnedIds.has(component.id)) return component;

    let placed = rect;
    for (let guard = 0; guard < blockers.length + components.length; guard += 1) {
      const blocker = blockers.find((candidate) => overlaps(placed, candidate));
      if (!blocker) break;
      placed = {
        ...placed,
        y: blocker.y + blocker.h + PIN_COLLISION_PADDING,
      };
    }
    blockers.push(placed);

    if (placed.x === rect.x && placed.y === rect.y) return component;
    const dx = placed.x - rect.x;
    const dy = placed.y - rect.y;
    deltas.set(component.id, { dx, dy });
    return { ...component, x: placed.x, y: placed.y };
  });
}

interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

function componentRect(component: Component): Rect | null {
  const x = component.x;
  const y = component.y;
  const w = component.w ?? component.wx;
  const h = component.h ?? component.hx;
  if (x == null || y == null || w == null || h == null) return null;
  return { x, y, w, h };
}

function overlaps(a: Rect, b: Rect): boolean {
  return (
    a.x < b.x + b.w + PIN_COLLISION_PADDING &&
    a.x + a.w + PIN_COLLISION_PADDING > b.x &&
    a.y < b.y + b.h + PIN_COLLISION_PADDING &&
    a.y + a.h + PIN_COLLISION_PADDING > b.y
  );
}

function recomputePinnedContexts(
  contexts: BoundedContext[],
  components: Component[],
  pins: LayoutPins,
  deltas: Map<string, { dx: number; dy: number }>
): BoundedContext[] {
  const affectedContextIds = new Set<string>();
  for (const component of components) {
    if (pins[component.id] || deltas.has(component.id)) affectedContextIds.add(component.bc);
  }
  if (affectedContextIds.size === 0) return contexts;

  const componentsByContext = new Map<string, Component[]>();
  for (const component of components) {
    if (!affectedContextIds.has(component.bc)) continue;
    const items = componentsByContext.get(component.bc) ?? [];
    items.push(component);
    componentsByContext.set(component.bc, items);
  }

  return contexts.map((context) => {
    const items = componentsByContext.get(context.id);
    if (!items || items.length === 0) return context;

    let minX = context.x ?? Number.POSITIVE_INFINITY;
    let minY = context.y ?? Number.POSITIVE_INFINITY;
    let maxX = context.x != null && context.w != null ? context.x + context.w : 0;
    let maxY = context.y != null && context.h != null ? context.y + context.h : 0;

    for (const component of items) {
      const x = component.x ?? 0;
      const y = component.y ?? 0;
      const w = component.w ?? 0;
      const h = component.h ?? 0;
      minX = Math.min(minX, x - GROUP_PADDING);
      minY = Math.min(minY, y - GROUP_PADDING);
      maxX = Math.max(maxX, x + w + GROUP_PADDING);
      maxY = Math.max(maxY, y + h + GROUP_PADDING);
    }

    if (!Number.isFinite(minX) || !Number.isFinite(minY)) return context;
    return {
      ...context,
      x: minX,
      y: minY,
      w: Math.max(context.w ?? 0, maxX - minX),
      h: Math.max(context.h ?? 0, maxY - minY),
    };
  });
}

function applyEdgeEndpointDeltas(edge: Edge, deltas: Map<string, { dx: number; dy: number }>): Edge {
  if (!edge.points || edge.points.length === 0) return edge;

  const fromDelta = deltas.get(edge.from);
  const toDelta = deltas.get(edge.to);
  if (!fromDelta && !toDelta) return edge;

  const sameDelta =
    fromDelta &&
    toDelta &&
    fromDelta.dx === toDelta.dx &&
    fromDelta.dy === toDelta.dy;

  if (sameDelta) {
    return {
      ...edge,
      points: edge.points.map((point) => ({
        x: point.x + fromDelta.dx,
        y: point.y + fromDelta.dy,
      })),
    };
  }

  const points = edge.points.map((point) => ({ ...point }));
  if (fromDelta) {
    points[0] = {
      x: points[0].x + fromDelta.dx,
      y: points[0].y + fromDelta.dy,
    };
  }
  if (toDelta) {
    const last = points.length - 1;
    points[last] = {
      x: points[last].x + toDelta.dx,
      y: points[last].y + toDelta.dy,
    };
  }
  return { ...edge, points };
}

function normalizePins(value: unknown): LayoutPins {
  if (!value || typeof value !== 'object') return {};
  const out: LayoutPins = {};
  for (const [id, pin] of Object.entries(value as Record<string, unknown>)) {
    if (!pin || typeof pin !== 'object') continue;
    const x = Number((pin as { x?: unknown }).x);
    const y = Number((pin as { y?: unknown }).y);
    if (!Number.isFinite(x) || !Number.isFinite(y)) continue;
    out[id] = { x: Math.round(x), y: Math.round(y) };
  }
  return out;
}

function keyPart(value: string): string {
  return encodeURIComponent(value.trim() || 'default');
}

function browserStorage(): Storage | null {
  if (typeof window === 'undefined') return null;
  return window.localStorage;
}
