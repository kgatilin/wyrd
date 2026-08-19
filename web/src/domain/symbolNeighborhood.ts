import type { Component, Diff, Internal, Member, SymbolRelation, UIGraph } from '../types';
import type { SymbolFocusTarget } from './symbolFocus';

/**
 * First-level wiring around one symbol, grouped by the package the neighbour
 * lives in. This is deliberately *not* a transitive walk: a symbol's direct
 * callers and callees are a fact you can read, while the reachable closure is a
 * hairball. Depth is recovered by walking one hop at a time (`toTarget`).
 */

export type NeighborDirection = 'in' | 'out';

export interface NeighborSymbol {
  /** Node id: the member id when the endpoint is a member, else the internal id. */
  id: string;
  componentId: string;
  packageName: string;
  internalId: string;
  memberId?: string;
  label: string;
  kind: string;
  exported?: boolean;
  /** False for endpoints outside the loaded graph — shown, but not walkable. */
  navigable: boolean;
}

export interface NeighborLink {
  id: string;
  direction: NeighborDirection;
  symbol: NeighborSymbol;
  /** Relation kinds collapsed onto this neighbour: `calls`, `uses`, … */
  kinds: string[];
  crossPackage: boolean;
  /**
   * When the anchor is a type, the members of it that actually carry the edge.
   * Empty when the edge hangs off the type itself.
   */
  via: string[];
  diff?: Diff;
}

export interface NeighborGroup {
  componentId: string;
  packageName: string;
  crossPackage: boolean;
  links: NeighborLink[];
}

export interface NeighborCounts {
  incoming: number;
  outgoing: number;
  crossPackage: number;
  packages: number;
}

export interface SymbolNeighborhood {
  anchor: NeighborSymbol | null;
  incoming: NeighborGroup[];
  outgoing: NeighborGroup[];
  counts: NeighborCounts;
}

/** Relation flattened to plain endpoint ids, so both sides read the same way. */
interface FlatEdge {
  id: string;
  kind: string;
  fromId: string;
  fromComponentId: string;
  fromInternalId: string;
  fromMemberId?: string;
  fromLabel?: string;
  toId: string;
  toComponentId: string;
  toInternalId: string;
  toMemberId?: string;
  toLabel?: string;
  diff?: Diff;
}

interface EdgeSide {
  id: string;
  componentId: string;
  internalId: string;
  memberId?: string;
  label?: string;
}

export function buildNeighborhood(graph: UIGraph, target: SymbolFocusTarget): SymbolNeighborhood {
  const symbols = symbolIndex(graph);
  const anchorId = target.memberId ?? target.internalId;
  const anchor = symbols.get(anchorId) ?? null;
  const empty: NeighborCounts = { incoming: 0, outgoing: 0, crossPackage: 0, packages: 0 };
  if (!anchor) return { anchor: null, incoming: [], outgoing: [], counts: empty };

  const links = new Map<string, NeighborLink>();
  for (const edge of flattenEdges(graph)) {
    const from = fromSide(edge);
    const to = toSide(edge);
    const fromIsAnchor = sideIsAnchor(from, target);
    const toIsAnchor = sideIsAnchor(to, target);
    // A type's own method calling the type is wiring inside the anchor, not a
    // neighbour — the block already contains it.
    if (fromIsAnchor === toIsAnchor) continue;

    const direction: NeighborDirection = fromIsAnchor ? 'out' : 'in';
    const near = fromIsAnchor ? from : to;
    const far = fromIsAnchor ? to : from;
    mergeLink(links, symbols, anchor, direction, near, far, edge);
  }

  const all = [...links.values()];
  return {
    anchor,
    incoming: groupByPackage(all.filter((link) => link.direction === 'in')),
    outgoing: groupByPackage(all.filter((link) => link.direction === 'out')),
    counts: {
      incoming: all.filter((link) => link.direction === 'in').length,
      outgoing: all.filter((link) => link.direction === 'out').length,
      crossPackage: all.filter((link) => link.crossPackage).length,
      packages: new Set(all.map((link) => link.symbol.componentId)).size,
    },
  };
}

/** Focus target that re-anchors the view on a neighbour. */
export function toTarget(symbol: NeighborSymbol): SymbolFocusTarget {
  return { componentId: symbol.componentId, internalId: symbol.internalId, memberId: symbol.memberId };
}

export function sameTarget(a: SymbolFocusTarget, b: SymbolFocusTarget): boolean {
  return a.internalId === b.internalId && (a.memberId ?? '') === (b.memberId ?? '');
}

function mergeLink(
  links: Map<string, NeighborLink>,
  symbols: Map<string, NeighborSymbol>,
  anchor: NeighborSymbol,
  direction: NeighborDirection,
  near: EdgeSide,
  far: EdgeSide,
  edge: FlatEdge
): void {
  const symbol = symbols.get(far.id) ?? unresolvedSymbol(far);
  const key = `${direction}:${symbol.id}`;
  const existing = links.get(key);
  // The edge hangs off a member of the anchor rather than the anchor itself
  // only when the anchor is the whole type; keep that provenance.
  const viaId = near.memberId && near.memberId !== anchor.id ? near.memberId : undefined;
  const via = viaId ? symbols.get(viaId)?.label ?? viaId : undefined;

  if (!existing) {
    links.set(key, {
      id: key,
      direction,
      symbol,
      kinds: [edge.kind],
      crossPackage: symbol.componentId !== anchor.componentId,
      via: via ? [via] : [],
      diff: edge.diff,
    });
    return;
  }
  if (!existing.kinds.includes(edge.kind)) existing.kinds = [...existing.kinds, edge.kind].sort();
  if (via && !existing.via.includes(via)) existing.via = [...existing.via, via].sort();
  if (!existing.diff && edge.diff) existing.diff = edge.diff;
}

function groupByPackage(links: NeighborLink[]): NeighborGroup[] {
  const groups = new Map<string, NeighborGroup>();
  for (const link of links) {
    const existing = groups.get(link.symbol.componentId);
    if (existing) {
      existing.links.push(link);
      continue;
    }
    groups.set(link.symbol.componentId, {
      componentId: link.symbol.componentId,
      packageName: link.symbol.packageName,
      crossPackage: link.crossPackage,
      links: [link],
    });
  }
  for (const group of groups.values()) {
    group.links.sort((a, b) => a.symbol.label.localeCompare(b.symbol.label));
  }
  // Cross-package groups first: crossing a package boundary is the finding,
  // wiring inside the package is the baseline.
  return [...groups.values()].sort(
    (a, b) => Number(b.crossPackage) - Number(a.crossPackage) || a.packageName.localeCompare(b.packageName)
  );
}

/** True when this endpoint is the focused symbol — or, for a type, a member of it. */
function sideIsAnchor(side: EdgeSide, target: SymbolFocusTarget): boolean {
  if (target.memberId) return side.memberId === target.memberId;
  return side.internalId === target.internalId;
}

function fromSide(edge: FlatEdge): EdgeSide {
  return {
    id: edge.fromId,
    componentId: edge.fromComponentId,
    internalId: edge.fromInternalId,
    memberId: edge.fromMemberId,
    label: edge.fromLabel,
  };
}

function toSide(edge: FlatEdge): EdgeSide {
  return {
    id: edge.toId,
    componentId: edge.toComponentId,
    internalId: edge.toInternalId,
    memberId: edge.toMemberId,
    label: edge.toLabel,
  };
}

/**
 * Endpoint outside the loaded graph. Rendered so a cross-package edge is never
 * silently dropped, but flagged unnavigable — there is nothing to walk into.
 */
function unresolvedSymbol(side: EdgeSide): NeighborSymbol {
  return {
    id: side.id,
    componentId: side.componentId,
    packageName: side.componentId,
    internalId: side.internalId,
    memberId: side.memberId,
    label: side.label || side.id.split('.').pop() || side.id,
    kind: 'symbol',
    navigable: false,
  };
}

function symbolIndex(graph: UIGraph): Map<string, NeighborSymbol> {
  const out = new Map<string, NeighborSymbol>();
  for (const component of graph.components) {
    for (const internal of component.internals) {
      const node = internalSymbol(component, internal);
      out.set(node.id, node);
      for (const member of internal.members ?? []) {
        const memberNode = memberSymbol(component, internal, member);
        out.set(memberNode.id, memberNode);
      }
    }
  }
  return out;
}

function internalSymbol(component: Component, internal: Internal): NeighborSymbol {
  return {
    id: internal.id,
    componentId: component.id,
    packageName: component.name || component.id,
    internalId: internal.id,
    label: internal.name,
    kind: internal.kind,
    exported: internal.exported,
    navigable: true,
  };
}

function memberSymbol(component: Component, internal: Internal, member: Member): NeighborSymbol {
  return {
    id: member.id,
    componentId: component.id,
    packageName: component.name || component.id,
    internalId: internal.id,
    memberId: member.id,
    label: member.name,
    kind: member.kind,
    exported: member.exported,
    navigable: true,
  };
}

function flattenEdges(graph: UIGraph): FlatEdge[] {
  const out: FlatEdge[] = [];
  for (const relation of graph.relations ?? []) {
    const edge = flattenRelation(relation);
    if (edge) out.push(edge);
  }
  out.push(...implementationMemberEdges(graph));
  return out;
}

function flattenRelation(relation: SymbolRelation): FlatEdge | null {
  const fromInternalId = relation.fromInternalId;
  const toInternalId = relation.toInternalId;
  if (!fromInternalId || !toInternalId) return null;
  return {
    id: relation.id,
    kind: relation.kind,
    fromId: relation.fromMemberId || fromInternalId,
    fromComponentId: relation.fromComponentId,
    fromInternalId,
    fromMemberId: relation.fromMemberId,
    fromLabel: relation.fromLabel,
    toId: relation.toMemberId || toInternalId,
    toComponentId: relation.toComponentId,
    toInternalId,
    toMemberId: relation.toMemberId,
    toLabel: relation.toLabel,
    diff: relation.diff,
  };
}

/**
 * Method-level `implements` edges. The graph records implementation between a
 * struct and an interface; matching the method names recovers which concrete
 * method satisfies which interface method, which is the level a reviewer asks
 * about when focused on one method.
 */
function implementationMemberEdges(graph: UIGraph): FlatEdge[] {
  const componentById = new Map(graph.components.map((component) => [component.id, component]));
  const out: FlatEdge[] = [];
  for (const relation of graph.relations ?? []) {
    if (relation.kind !== 'implements' || !relation.fromInternalId || !relation.toInternalId) continue;
    const concrete = componentById
      .get(relation.fromComponentId)
      ?.internals.find((internal) => internal.id === relation.fromInternalId);
    const iface = componentById
      .get(relation.toComponentId)
      ?.internals.find((internal) => internal.id === relation.toInternalId);
    if (!concrete || !iface) continue;
    const concreteByName = new Map((concrete.members ?? []).map((member) => [methodKey(member.name), member]));
    for (const ifaceMember of iface.members ?? []) {
      const concreteMember = concreteByName.get(methodKey(ifaceMember.name));
      if (!concreteMember) continue;
      out.push({
        id: `impl:${concreteMember.id}->${ifaceMember.id}`,
        kind: 'implements',
        fromId: concreteMember.id,
        fromComponentId: relation.fromComponentId,
        fromInternalId: relation.fromInternalId,
        fromMemberId: concreteMember.id,
        fromLabel: concreteMember.name,
        toId: ifaceMember.id,
        toComponentId: relation.toComponentId,
        toInternalId: relation.toInternalId,
        toMemberId: ifaceMember.id,
        toLabel: ifaceMember.name,
      });
    }
  }
  return out;
}

function methodKey(name: string): string {
  return name.split('(')[0].split(':')[0].trim();
}
