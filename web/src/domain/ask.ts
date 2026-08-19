import type { UIGraph } from '../types';
import type { AskState } from './state';

/**
 * A search hit exactly as the daemon returns it (POST /api/search).
 * Snake_case: this is the wire shape, not the app's model.
 */
export interface RawAskHit {
  node_id: string;
  kind: string;
  package?: string;
  name?: string;
  file?: string;
  line?: number;
  doc?: string;
  snippet?: string;
  score?: number;
}

/** A hit resolved against the loaded graph. */
export interface AskHit {
  /** Retrieval node id — the same string as the card row's `Internal.id`. */
  nodeId: string;
  kind: string;
  name: string;
  /** Component (package) id the symbol lives in; '' when it could not be resolved. */
  packageId: string;
  file: string;
  line: number;
  doc: string;
  score: number;
  /** The package is a component in the loaded graph, so the hit can be shown. */
  inGraph: boolean;
  /** The symbol itself is a row on that component's card. */
  symbolInGraph: boolean;
}

/** The component/symbol set an ask projects the canvas down to. */
export interface AskProjection {
  componentIds: ReadonlySet<string>;
  internalIds: ReadonlySet<string>;
  /** Cards show only the matched symbols; false shows the whole package. */
  detailOnly: boolean;
}

/**
 * The projection an ask state currently imposes, or null when no question is
 * active (or its answer landed nowhere drawable). Every consumer — the canvas,
 * the layout effect — goes through this, so they cannot disagree about what
 * the canvas is showing.
 */
export function askProjectionOf(ask: AskState): AskProjection | null {
  if (ask.query === '') return null;
  return buildAskProjection(ask.hits, ask.detailOnly);
}

/** Hits of one package, in rank order. */
export interface AskHitGroup {
  packageId: string;
  hits: AskHit[];
}

/**
 * Map raw search hits onto the loaded graph. The retrieval node id and the
 * uigraph `Internal.id` share one scheme ("{package}.{Symbol}"), so a hit is a
 * card row lookup — no id translation. Hits whose package is not in the graph
 * survive as unresolved rows: a query that matched code the current projection
 * cannot draw should say so, not silently return fewer results.
 */
export function resolveAskHits(graph: UIGraph | null, raw: RawAskHit[]): AskHit[] {
  const internalsByComponent = buildGraphIndex(graph);
  return raw.map((hit) => {
    const packageId = hit.package || fallbackPackage(internalsByComponent, hit.node_id) || '';
    const internals = internalsByComponent.get(packageId);
    return {
      nodeId: hit.node_id,
      kind: hit.kind ?? '',
      name: hit.name || symbolName(hit.node_id, packageId),
      packageId,
      file: hit.file ?? '',
      line: hit.line ?? 0,
      doc: hit.doc ?? '',
      score: hit.score ?? 0,
      inGraph: internals != null,
      symbolInGraph: internals != null && internals.has(hit.node_id),
    };
  });
}

/**
 * Re-check hits against a reloaded graph. The answer survives a model reload
 * (an agent edited the tree, the worktree changed); what changes is whether
 * each hit can still be drawn.
 */
export function reresolveAskHits(graph: UIGraph | null, hits: AskHit[]): AskHit[] {
  const internalsByComponent = buildGraphIndex(graph);
  return hits.map((hit) => {
    const packageId = hit.packageId || fallbackPackage(internalsByComponent, hit.nodeId) || '';
    const internals = internalsByComponent.get(packageId);
    return {
      ...hit,
      packageId,
      inGraph: internals != null,
      symbolInGraph: internals != null && internals.has(hit.nodeId),
    };
  });
}

/**
 * The canvas selection for a set of hits, or null when nothing landed in the
 * graph (the caller then leaves the current view alone).
 */
export function buildAskProjection(hits: AskHit[], detailOnly: boolean): AskProjection | null {
  const componentIds = new Set<string>();
  const internalIds = new Set<string>();
  for (const hit of hits) {
    if (!hit.inGraph) continue;
    componentIds.add(hit.packageId);
    if (hit.symbolInGraph) internalIds.add(hit.nodeId);
  }
  if (componentIds.size === 0) return null;
  return { componentIds, internalIds, detailOnly };
}

/** Hits grouped by package, packages ordered by their best-ranked hit. */
export function groupAskHits(hits: AskHit[]): AskHitGroup[] {
  const groups: AskHitGroup[] = [];
  const byPackage = new Map<string, AskHitGroup>();
  for (const hit of hits) {
    const key = hit.packageId || '(unresolved)';
    let group = byPackage.get(key);
    if (!group) {
      group = { packageId: key, hits: [] };
      byPackage.set(key, group);
      groups.push(group);
    }
    group.hits.push(hit);
  }
  return groups;
}

function buildGraphIndex(graph: UIGraph | null): Map<string, Set<string>> {
  const internalsByComponent = new Map<string, Set<string>>();
  for (const component of graph?.components ?? []) {
    internalsByComponent.set(component.id, new Set(component.internals.map((internal) => internal.id)));
  }
  return internalsByComponent;
}

/**
 * Longest component id that prefixes the node id — the fallback when the
 * daemon did not send the package. Package paths contain dots, so splitting on
 * the last dot would guess; matching known components cannot.
 */
function fallbackPackage(internalsByComponent: Map<string, Set<string>>, nodeId: string): string | null {
  let best: string | null = null;
  for (const componentId of internalsByComponent.keys()) {
    if (!nodeId.startsWith(componentId + '.')) continue;
    if (best == null || componentId.length > best.length) best = componentId;
  }
  return best;
}

function symbolName(nodeId: string, packageId: string): string {
  if (packageId && nodeId.startsWith(packageId + '.')) return nodeId.slice(packageId.length + 1);
  const dot = nodeId.lastIndexOf('.');
  return dot < 0 ? nodeId : nodeId.slice(dot + 1);
}
