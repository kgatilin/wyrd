import type { UIGraph } from '../types';
import type { AskState } from './state';

/**
 * A search hit as the ask flow consumes it — the daemon's wire shape narrowed
 * to the fields the projection uses (`web/src/data/search.ts` maps onto it).
 * Snake_case: this is a wire shape, not the app's model.
 */
export interface RawAskHit {
  node_id: string;
  kind: string;
  package?: string;
  name?: string;
  file?: string;
  line?: number;
  doc?: string;
  score?: number;
  /** The query text matched this symbol; the rest is the region around it. */
  seed?: boolean;
}

/** A hit resolved against the loaded graph. */
export interface AskHit {
  /**
   * Retrieval node id — the same string as the card row's `Internal.id`, or a
   * member's `Member.id` when the hit is a method.
   */
  nodeId: string;
  kind: string;
  name: string;
  /** Component (package) id the symbol lives in; '' when it could not be resolved. */
  packageId: string;
  file: string;
  line: number;
  doc: string;
  score: number;
  /**
   * The query text matched this symbol. False means the graph diffusion
   * reached it from a seed — it is the answer's context, not its match.
   */
  seed: boolean;
  /** The package is a component in the loaded graph, so the hit can be shown. */
  inGraph: boolean;
  /** The symbol itself is drawable — a row on that component's card, or a member of one. */
  symbolInGraph: boolean;
  /**
   * The card row that carries the hit: the hit itself when it is a top-level
   * symbol, its receiver type when it is a method. The canvas draws internals,
   * not members, so this — not `nodeId` — is what a projection selects on. '' when
   * the hit resolved to nothing drawable.
   */
  internalId: string;
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
 * uigraph `Internal.id` share one scheme ("{package}.{Symbol}"), and a method's
 * id extends it the same way uigraph's `Member.id` does
 * ("{package}.{Receiver}.{Method}"), so a hit is a lookup — no id translation.
 * Hits whose package is not in the graph survive as unresolved rows: a query
 * that matched code the current projection cannot draw should say so, not
 * silently return fewer results.
 */
export function resolveAskHits(graph: UIGraph | null, raw: RawAskHit[]): AskHit[] {
  const index = buildGraphIndex(graph);
  return raw.map((hit) => {
    const packageId = hit.package || fallbackPackage(index, hit.node_id) || '';
    return {
      nodeId: hit.node_id,
      kind: hit.kind ?? '',
      name: hit.name || symbolName(hit.node_id, packageId),
      packageId,
      file: hit.file ?? '',
      line: hit.line ?? 0,
      doc: hit.doc ?? '',
      score: hit.score ?? 0,
      seed: hit.seed !== false,
      ...locate(index, packageId, hit.node_id),
    };
  });
}

/**
 * Re-check hits against a reloaded graph. The answer survives a model reload
 * (an agent edited the tree, the worktree changed); what changes is whether
 * each hit can still be drawn.
 */
export function reresolveAskHits(graph: UIGraph | null, hits: AskHit[]): AskHit[] {
  const index = buildGraphIndex(graph);
  return hits.map((hit) => {
    const packageId = hit.packageId || fallbackPackage(index, hit.nodeId) || '';
    return { ...hit, packageId, ...locate(index, packageId, hit.nodeId) };
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
    if (hit.internalId) internalIds.add(hit.internalId);
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

/**
 * What each component draws, keyed by symbol id: an internal maps to itself, a
 * member to the internal whose card row carries it. One map answers both "can
 * this hit be drawn" and "on which row" — a method hit resolves to a member id
 * that no internal has, and selecting on it alone would draw an empty card.
 */
type GraphIndex = Map<string, Map<string, string>>;

function buildGraphIndex(graph: UIGraph | null): GraphIndex {
  const index: GraphIndex = new Map();
  for (const component of graph?.components ?? []) {
    const rows = new Map<string, string>();
    for (const internal of component.internals) {
      rows.set(internal.id, internal.id);
      for (const member of internal.members) rows.set(member.id, internal.id);
    }
    index.set(component.id, rows);
  }
  return index;
}

/** Where a hit lands on the canvas, given the package it claims. */
function locate(
  index: GraphIndex,
  packageId: string,
  nodeId: string,
): { inGraph: boolean; symbolInGraph: boolean; internalId: string } {
  const rows = index.get(packageId);
  const internalId = rows?.get(nodeId) ?? '';
  return { inGraph: rows != null, symbolInGraph: internalId !== '', internalId };
}

/**
 * Longest component id that prefixes the node id — the fallback when the
 * daemon did not send the package. Package paths contain dots, so splitting on
 * the last dot would guess; matching known components cannot.
 */
function fallbackPackage(index: GraphIndex, nodeId: string): string | null {
  let best: string | null = null;
  for (const componentId of index.keys()) {
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
