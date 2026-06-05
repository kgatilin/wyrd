import type { UIGraph, Diff, Component as ComponentDef, SymbolRelation } from '../types';
import type { AppUI, Interaction, Marker, ReviewChangeFilter, ReviewImpactMode } from './state';

export interface ReviewSelectionOptions {
  impactMode?: ReviewImpactMode;
  changeFilter?: ReviewChangeFilter;
  hideUnchangedNeighbors?: boolean;
  changedDetailsOnly?: boolean;
}

/** Focused component + its direct edge neighbours; null when nothing is focused. */
export function relatedIds(graph: UIGraph, focusId: string | null): Set<string> | null {
  if (!focusId) return null;
  const r = new Set<string>([focusId]);
  for (const edge of graph.edges) {
    if (edge.from === focusId) r.add(edge.to);
    if (edge.to === focusId) r.add(edge.from);
  }
  for (const relation of graph.relations ?? []) {
    if (relation.fromComponentId === focusId) r.add(relation.toComponentId);
    if (relation.toComponentId === focusId) r.add(relation.fromComponentId);
  }
  return r;
}

/** Project the UI slice down to the inputs the layout engine needs. */
export function toInteraction(ui: AppUI): Interaction {
  return {
    expanded: ui.expanded,
    internalExpanded: ui.internalExpanded,
    internalWide: ui.internalWide,
    cardDensity: ui.cardDensity,
    showInlineSignatures: ui.showInlineSignatures,
  };
}

export function selectReviewGraph(
  graph: UIGraph,
  reviewViewId: string | null,
  scopeId: string | null,
  groupingId?: string | null,
  options?: ReviewSelectionOptions
): UIGraph {
  const view = reviewViewId
    ? graph.reviewViews?.find((v) => v.id === reviewViewId)
    : graph.reviewViews?.[0];
  const impactMode = options?.impactMode ?? 'changed_only';
  const wholeRepository = impactMode === 'repository';
  const allowedComponents = !wholeRepository && view ? new Set(view.componentIds) : null;
  const scope = scopeId ?? view?.defaultScope ?? graph.reviewScopes?.[0]?.id ?? 'everything';
  const publicOnly = scope === 'top_level_public_api' || scope === 'all_public_api';
  const internalOnly = scope === 'internal_implementation';
  const hasPublicPortFlags = graph.components.some((c) => c.ports.some((p) => p.public !== undefined));
  const hasPublicEdgeFlags = graph.edges.some((e) => e.public !== undefined);
  const hasPublicRelationFlags = (graph.relations ?? []).some((relation) => relation.public !== undefined);

  let components = graph.components
    .filter((c) => !allowedComponents || allowedComponents.has(c.id))
    .map((c) => {
      if (!publicOnly && !internalOnly) return c;
      if (internalOnly) {
        const internals = c.internals
          .map((i) => {
            if (!i.exported) return i;
            return {
              ...i,
              diff: undefined,
              members: (i.members ?? []).filter((m) => !m.exported),
            };
          })
          .filter((i) => !i.exported || (i.members ?? []).length > 0);
        const ports = hasPublicPortFlags ? c.ports.filter((p) => !p.public) : c.ports;
        return { ...c, internals, ports };
      }
      const internals = c.internals
        .filter((i) => i.exported)
        .map((i) => ({
          ...i,
          members: (i.members ?? []).filter((m) => m.exported),
        }));
      const ports = hasPublicPortFlags ? c.ports.filter((p) => p.public) : c.ports;
      return { ...c, internals, ports };
    })
    .filter((c) => {
      if (!publicOnly) return true;
      return c.internals.length > 0 || c.ports.length > 0 || c.diff != null;
    });
  if (internalOnly) {
    components = components.filter((c) => c.internals.length > 0 || c.ports.length > 0 || c.diff != null);
  }

  let componentIds = new Set(components.map((c) => c.id));
  let edges = graph.edges.filter((e) => componentIds.has(e.from) && componentIds.has(e.to));
  let relations = (graph.relations ?? []).filter(
    (relation) => componentIds.has(relation.fromComponentId) && componentIds.has(relation.toComponentId)
  );
  if (publicOnly && hasPublicEdgeFlags) {
    edges = edges.filter((e) => e.public);
  } else if (internalOnly && hasPublicEdgeFlags) {
    edges = edges.filter((e) => !e.public);
  }
  if (publicOnly && hasPublicRelationFlags) {
    relations = relations.filter((relation) => relation.public);
  } else if (internalOnly && hasPublicRelationFlags) {
    relations = relations.filter((relation) => !relation.public);
  }

  if (graph.pr != null) {
    const projected = selectDiffImpact(
      graph,
      components,
      edges,
      relations,
      view?.id ?? null,
      groupingId ?? graph.defaultGrouping ?? graph.reviewGroupings?.[0]?.id ?? null,
      impactMode,
      options?.changeFilter ?? 'all',
      options?.hideUnchangedNeighbors ?? false
    );
    components = projected.components;
    edges = projected.edges;
    relations = projected.relations;
  }

  if (graph.pr != null && options?.changedDetailsOnly) {
    const detailChangeFilter = options?.changeFilter ?? 'all';
    const visibleIds = new Set(components.map((component) => component.id));
    const policyViolations = filterPolicyViolations(graph.policyViolations, visibleIds);
    edges = filterEdgesToChangedDetails(edges, detailChangeFilter, policyViolations);
    components = filterComponentDetailsToChanges(components, detailChangeFilter);
    relations = filterRelationsToChangedDetails(relations, components, detailChangeFilter);
  }

  const grouped = applyReviewGrouping(
    graph,
    components,
    view?.id ?? null,
    groupingId ?? graph.defaultGrouping ?? graph.reviewGroupings?.[0]?.id ?? null
  );
  const visibleComponentIds = new Set(grouped.components.map((component) => component.id));
  relations = relations.filter(
    (relation) => visibleComponentIds.has(relation.fromComponentId) && visibleComponentIds.has(relation.toComponentId)
  );

  return {
    ...graph,
    policyViolations: filterPolicyViolations(graph.policyViolations, visibleComponentIds),
    boundedContexts: grouped.boundedContexts,
    components: grouped.components,
    edges,
    relations,
  };
}

function selectDiffImpact(
  graph: UIGraph,
  components: ComponentDef[],
  edges: UIGraph['edges'],
  relations: SymbolRelation[],
  reviewViewId: string | null,
  groupingId: string | null,
  impactMode: ReviewImpactMode,
  changeFilter: ReviewChangeFilter,
  hideUnchangedNeighbors: boolean
): { components: ComponentDef[]; edges: UIGraph['edges']; relations: SymbolRelation[] } {
  const policyViolations = filterPolicyViolations(
    graph.policyViolations,
    new Set(components.map((component) => component.id))
  );
  const changed = changedComponentIDs(components, edges, relations, policyViolations, changeFilter);

  if (impactMode === 'review_view' || impactMode === 'repository') {
    const candidates = hideUnchangedNeighbors
      ? components.filter((component) => changed.has(component.id))
      : components;
    const componentsOut = filterComponentDiffs(candidates, changeFilter);
    const visibleIds = new Set(componentsOut.map((component) => component.id));
    return {
      components: componentsOut,
      edges: filterEdgesByChange(edges, changeFilter, policyViolations).filter(
        (edge) => visibleIds.has(edge.from) && visibleIds.has(edge.to)
      ),
      relations: filterRelationsByChange(relations, changeFilter).filter(
        (relation) => visibleIds.has(relation.fromComponentId) && visibleIds.has(relation.toComponentId)
      ),
    };
  }

  if (changed.size === 0) {
    if (changeFilter === 'all') return { components, edges, relations };
    return { components: [], edges: [], relations: [] };
  }

  let impacted = new Set(changed);
  if (!hideUnchangedNeighbors) {
    switch (impactMode) {
      case 'changed_only':
        break;
      case 'containing_group': {
        const grouped = applyReviewGrouping(graph, components, reviewViewId, groupingId);
        const bcByComponent = new Map(grouped.components.map((component) => [component.id, component.bc]));
        const changedGroups = new Set([...changed].map((id) => bcByComponent.get(id)).filter((id): id is string => !!id));
        impacted = new Set(grouped.components.filter((component) => changedGroups.has(component.bc)).map((component) => component.id));
        break;
      }
      case 'changed_neighbors':
      default:
        for (const edge of edges) {
          if (changed.has(edge.from) || changed.has(edge.to)) {
            impacted.add(edge.from);
            impacted.add(edge.to);
          }
        }
        for (const relation of relations) {
          if (changed.has(relation.fromComponentId) || changed.has(relation.toComponentId)) {
            impacted.add(relation.fromComponentId);
            impacted.add(relation.toComponentId);
          }
        }
        break;
    }
  }

  const componentsOut = filterComponentDiffs(
    components.filter((component) => impacted.has(component.id)),
    changeFilter
  );
  const visibleIds = new Set(componentsOut.map((component) => component.id));
  const edgesOut = filterEdgesByChange(edges, changeFilter, policyViolations).filter(
    (edge) => visibleIds.has(edge.from) && visibleIds.has(edge.to)
  );
  const relationsOut = filterRelationsByChange(relations, changeFilter).filter(
    (relation) => visibleIds.has(relation.fromComponentId) && visibleIds.has(relation.toComponentId)
  );
  return { components: componentsOut, edges: edgesOut, relations: relationsOut };
}

function changedComponentIDs(
  components: ComponentDef[],
  edges: UIGraph['edges'],
  relations: SymbolRelation[],
  policyViolations: UIGraph['policyViolations'],
  changeFilter: ReviewChangeFilter
): Set<string> {
  const changed = new Set<string>();
  if (changeFilter !== 'dependency' && changeFilter !== 'policy') {
    for (const component of components) {
      if (componentHasDiff(component, changeFilter)) changed.add(component.id);
    }
  }
  if (changeFilter === 'all' || changeFilter === 'dependency') {
    for (const edge of edges) {
      if (!edge.diff) continue;
      changed.add(edge.from);
      changed.add(edge.to);
    }
    for (const relation of relations) {
      if (!relation.diff) continue;
      changed.add(relation.fromComponentId);
      changed.add(relation.toComponentId);
    }
  }
  if (changeFilter === 'all' || changeFilter === 'policy') {
    const componentIds = new Set(components.map((component) => component.id));
    for (const violation of policyViolations ?? []) {
      if (componentIds.has(violation.sourceComponentId)) changed.add(violation.sourceComponentId);
      if (componentIds.has(violation.targetComponentId)) changed.add(violation.targetComponentId);
    }
  }
  return changed;
}

function filterComponentDiffs(
  components: ComponentDef[],
  changeFilter: ReviewChangeFilter
): ComponentDef[] {
  if (changeFilter === 'all') return components;
  if (changeFilter === 'dependency' || changeFilter === 'policy') return components.map(clearComponentDiffs);
  return components
    .map((component) => filterComponentDiff(component, changeFilter))
    .filter((component) => componentHasDiff(component, changeFilter));
}

function clearComponentDiffs(component: ComponentDef): ComponentDef {
  return {
    ...component,
    diff: undefined,
    internals: component.internals.map((internal) => ({
      ...internal,
      diff: undefined,
      members: (internal.members ?? []).map((member) => ({ ...member, diff: undefined })),
    })),
    ports: component.ports.map((port) => ({ ...port, diff: undefined })),
  };
}

function filterComponentDetailsToChanges(
  components: ComponentDef[],
  changeFilter: ReviewChangeFilter
): ComponentDef[] {
  return components.map((component) => ({
    ...component,
    internals: component.internals
      .map((internal) => {
        const internalMatches = matchesDiffFilter(internal.diff, changeFilter);
        const memberMatches = (internal.members ?? []).filter((member) => matchesDiffFilter(member.diff, changeFilter));
        if (internal.diff === 'added' || internal.diff === 'removed') {
          return {
            ...internal,
            members: internalMatches ? (internal.members ?? []) : memberMatches,
          };
        }
        return {
          ...internal,
          diff: internal.diff === 'changed' && memberMatches.length > 0 ? undefined : internal.diff,
          members: memberMatches,
        };
      })
      .filter((internal) => matchesDiffFilter(internal.diff, changeFilter) || (internal.members ?? []).length > 0),
    ports: component.ports.filter((port) => matchesDetailPort(port, changeFilter)),
  }));
}

function matchesDetailPort(port: ComponentDef['ports'][number], changeFilter: ReviewChangeFilter): boolean {
  if (changeFilter === 'policy') return false;
  return matchesDiffFilter(port.diff, changeFilter);
}

function matchesDiffFilter(diff: Diff | undefined, changeFilter: ReviewChangeFilter): boolean {
  if (!diff) return false;
  if (changeFilter === 'all' || changeFilter === 'dependency') return true;
  if (changeFilter === 'policy') return false;
  return diff === changeFilter;
}

function filterComponentDiff(component: ComponentDef, diff: Diff): ComponentDef {
  return {
    ...component,
    diff: component.diff === diff ? component.diff : undefined,
    internals: component.internals
      .map((internal) => ({
        ...internal,
        diff: internal.diff === diff ? internal.diff : undefined,
        members: (internal.members ?? []).filter((member) => member.diff === diff),
      }))
      .filter((internal) => internal.diff === diff || (internal.members ?? []).length > 0),
    ports: component.ports.filter((port) => port.diff === diff),
  };
}

function filterEdgesByChange(
  edges: UIGraph['edges'],
  changeFilter: ReviewChangeFilter,
  policyViolations: UIGraph['policyViolations']
): UIGraph['edges'] {
  if (changeFilter === 'dependency') return edges.filter((edge) => edge.diff);
  if (changeFilter === 'policy') {
    const pairs = policyViolationPairs(policyViolations);
    return edges.filter((edge) => pairs.has(policyPairKey(edge.from, edge.to)));
  }
  return edges;
}

function filterEdgesToChangedDetails(
  edges: UIGraph['edges'],
  changeFilter: ReviewChangeFilter,
  policyViolations: UIGraph['policyViolations']
): UIGraph['edges'] {
  if (changeFilter === 'policy') {
    const pairs = policyViolationPairs(policyViolations);
    return edges.filter((edge) => pairs.has(policyPairKey(edge.from, edge.to)));
  }
  if (changeFilter === 'dependency' || changeFilter === 'all') {
    return edges.filter((edge) => edge.diff);
  }
  return edges.filter((edge) => edge.diff === changeFilter);
}

function filterRelationsByChange(
  relations: SymbolRelation[],
  changeFilter: ReviewChangeFilter
): SymbolRelation[] {
  if (changeFilter === 'dependency') return relations.filter((relation) => relation.diff);
  if (changeFilter === 'policy') return [];
  return relations;
}

function filterRelationsToChangedDetails(
  relations: SymbolRelation[],
  components: ComponentDef[],
  changeFilter: ReviewChangeFilter
): SymbolRelation[] {
  if (changeFilter === 'policy') return [];
  const componentIds = new Set(components.map((component) => component.id));
  const internalIds = new Set<string>();
  const memberIds = new Set<string>();
  const changedInternalIds = new Set<string>();
  const changedMemberIds = new Set<string>();
  for (const component of components) {
    for (const internal of component.internals) {
      internalIds.add(internal.id);
      if (matchesDiffFilter(internal.diff, changeFilter)) changedInternalIds.add(internal.id);
      for (const member of internal.members ?? []) {
        memberIds.add(member.id);
        if (matchesDiffFilter(member.diff, changeFilter)) changedMemberIds.add(member.id);
      }
    }
  }
  return relations.filter((relation) => {
    if (!componentIds.has(relation.fromComponentId) || !componentIds.has(relation.toComponentId)) return false;
    if (!relationEndpointVisible(relation.fromInternalId, relation.fromMemberId, internalIds, memberIds)) return false;
    if (changeFilter === 'dependency') return matchesDiffFilter(relation.diff, changeFilter);
    return relationEndpointVisible(relation.fromInternalId, relation.fromMemberId, changedInternalIds, changedMemberIds);
  });
}

function relationEndpointVisible(
  internalId: string | undefined,
  memberId: string | undefined,
  internalIds: Set<string>,
  memberIds: Set<string>
): boolean {
  if (memberId) return memberIds.has(memberId);
  if (internalId) return internalIds.has(internalId);
  return false;
}

function filterPolicyViolations(
  policyViolations: UIGraph['policyViolations'],
  componentIds: Set<string>
): UIGraph['policyViolations'] {
  const filtered = (policyViolations ?? []).filter(
    (violation) => componentIds.has(violation.sourceComponentId) && componentIds.has(violation.targetComponentId)
  );
  return filtered.length > 0 ? filtered : undefined;
}

function policyViolationPairs(policyViolations: UIGraph['policyViolations']): Set<string> {
  const pairs = new Set<string>();
  for (const violation of policyViolations ?? []) {
    pairs.add(policyPairKey(violation.sourceComponentId, violation.targetComponentId));
  }
  return pairs;
}

function policyPairKey(source: string, target: string): string {
  return `${source}\u0000${target}`;
}

function applyReviewGrouping(
  graph: UIGraph,
  components: ComponentDef[],
  reviewViewId: string | null,
  groupingId: string | null
): { boundedContexts: UIGraph['boundedContexts']; components: ComponentDef[] } {
  const grouping = groupingId ? graph.reviewGroupings?.find((g) => g.id === groupingId) : null;
  if (!grouping || grouping.groups.length === 0) {
    return {
      boundedContexts: filteredBoundedContexts(graph, components),
      components,
    };
  }

  const componentToGroup = new Map<string, string>();
  const preferredReviewGroupId =
    grouping.id === 'review_view' && reviewViewId ? `review_view:${reviewViewId}` : null;

  for (const group of grouping.groups) {
    for (const componentId of group.componentIds) {
      if (preferredReviewGroupId === group.id || !componentToGroup.has(componentId)) {
        componentToGroup.set(componentId, group.id);
      }
    }
  }

  const groupedComponents = components.map((component) => {
    const groupId = componentToGroup.get(component.id);
    return groupId ? { ...component, bc: groupId } : component;
  });

  const usedBCs = new Set(groupedComponents.map((component) => component.bc));
  const boundedContexts = [];
  const emitted = new Set<string>();
  for (const group of grouping.groups) {
    if (!usedBCs.has(group.id)) continue;
    emitted.add(group.id);
    boundedContexts.push({ id: group.id, name: group.title });
  }

  const originalNames = new Map(graph.boundedContexts.map((bc) => [bc.id, bc.name]));
  for (const id of usedBCs) {
    if (emitted.has(id)) continue;
    boundedContexts.push({ id, name: originalNames.get(id) ?? id });
  }

  return { boundedContexts, components: groupedComponents };
}

function filteredBoundedContexts(graph: UIGraph, components: ComponentDef[]): UIGraph['boundedContexts'] {
  const usedBCs = new Set(components.map((c) => c.bc));
  return graph.boundedContexts.filter((bc) => usedBCs.has(bc.id));
}

function componentHasDiff(component: ComponentDef, diff?: ReviewChangeFilter): boolean {
  if (diff === 'dependency' || diff === 'policy') return false;
  if (component.diff && (diff == null || diff === 'all' || component.diff === diff)) return true;
  for (const internal of component.internals) {
    if (internal.diff && (diff == null || diff === 'all' || internal.diff === diff)) return true;
    for (const member of internal.members ?? []) {
      if (member.diff && (diff == null || diff === 'all' || member.diff === diff)) return true;
    }
  }
  return component.ports.some((port) => port.diff && (diff == null || diff === 'all' || port.diff === diff));
}

/** Union `prev` with the internals of every currently-expanded component (add-only). */
export function addInternalsOfExpanded(
  graph: UIGraph,
  expanded: ReadonlySet<string>,
  prev: ReadonlySet<string>
): Set<string> {
  const next = new Set(prev);
  for (const c of graph.components) {
    if (expanded.has(c.id)) {
      for (const internal of c.internals) next.add(internal.id);
    }
  }
  return next;
}

/** Which components start expanded after a graph loads or a review view changes. */
export function initialExpanded(graph: UIGraph, expansion: string | null = 'auto'): string[] {
  switch (expansion) {
    case 'collapsed':
      return [];
    case 'expanded':
      return graph.components.map((c) => c.id);
    case 'changed':
      return graph.components.filter((c) => componentHasDiff(c, 'all')).map((c) => c.id);
    case 'auto':
    default:
      break;
  }
  const orders = graph.components.find((c) => c.id === 'orders');
  if (orders) return ['orders'];
  if (graph.components.length > 0) return [graph.components[0].id];
  return [];
}

/** A change entry derived from graph elements with diff flags. */
export type ChangeKind = Diff | 'policy';

export interface ChangeEntry {
  id: string;
  kind: ChangeKind;
  name: string;
  where: string;
  cmp: string;
  internal?: string;
  member?: string;
  port?: string;
  relation?: string;
  diffBefore?: string;
  diffAfter?: string;
}

export interface ChangeStats {
  added: number;
  removed: number;
  changed: number;
  policy: number;
  total: number;
}

export function deriveChangeStats(changes: ChangeEntry[]): ChangeStats {
  const stats: ChangeStats = { added: 0, removed: 0, changed: 0, policy: 0, total: changes.length };
  for (const change of changes) {
    switch (change.kind) {
      case 'added':
        stats.added++;
        break;
      case 'removed':
        stats.removed++;
        break;
      case 'changed':
        stats.changed++;
        break;
      case 'policy':
        stats.policy++;
        break;
    }
  }
  return stats;
}

/** Walk the graph for diff-flagged elements. Moved verbatim from components/ChangesPanel. */
export function deriveChanges(graph: UIGraph): ChangeEntry[] {
  const out: ChangeEntry[] = [];

  for (const c of graph.components) {
    const bcName = graph.boundedContexts.find((b) => b.id === c.bc)?.name ?? c.bc;

    if (c.diff) {
      out.push({ id: `cmp-${c.id}`, kind: c.diff, name: c.name, where: `component - ${bcName}`, cmp: c.id });
    }

    for (const i of c.internals) {
      if (i.diff) {
        out.push({
          id: `int-${i.id}`,
          kind: i.diff,
          name: i.name,
          where: `${i.kind} - ${c.name}`,
          cmp: c.id,
          internal: i.id,
          diffBefore: i.diffBefore,
          diffAfter: i.diffAfter,
        });
      }
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
            diffBefore: m.diffBefore,
            diffAfter: m.diffAfter,
          });
        }
      }
    }

    for (const p of c.ports) {
      if (p.diff) {
        out.push({ id: `port-${p.id}`, kind: p.diff, name: p.name, where: `port - ${c.name}`, cmp: c.id, port: p.id });
      }
    }
  }

  for (const e of graph.edges) {
    if (e.diff) {
      const fromName = graph.components.find((c) => c.id === e.from)?.name ?? e.from;
      const toName = graph.components.find((c) => c.id === e.to)?.name ?? e.to;
      out.push({ id: `edg-${e.id}`, kind: e.diff, name: `${fromName} -> ${toName}`, where: `connection - ${e.label || ''}`, cmp: e.from });
    }
  }

  const diffEdgePairs = new Set(
    graph.edges
      .filter((edge) => edge.diff)
      .map((edge) => policyPairKey(edge.from, edge.to))
  );
  for (const relation of graph.relations ?? []) {
    if (!relation.diff) continue;
    if (diffEdgePairs.has(policyPairKey(relation.fromComponentId, relation.toComponentId))) continue;
    const fromName = relation.fromLabel || relation.fromMemberId || relation.fromInternalId || relation.fromComponentId;
    const toName = relation.toLabel || relation.toMemberId || relation.toInternalId || relation.toComponentId;
    out.push({
      id: `rel-${relation.id}`,
      kind: relation.diff,
      name: `${fromName} -> ${toName}`,
      where: `relation - ${relation.kind}`,
      cmp: relation.fromComponentId,
      internal: relation.fromInternalId,
      member: relation.fromMemberId,
      relation: relation.id,
    });
  }

  for (const violation of graph.policyViolations ?? []) {
    const source = graph.components.find((c) => c.id === violation.sourceComponentId);
    const target = graph.components.find((c) => c.id === violation.targetComponentId);
    if (!source || !target) continue;
    const layerSummary =
      violation.sourceLayer && violation.targetLayer
        ? `${violation.sourceLayer} -> ${violation.targetLayer}`
        : violation.kind;
    out.push({
      id: `pol-${violation.id}`,
      kind: 'policy',
      name: `${source.name} -> ${target.name}`,
      where: violation.message || `policy - ${layerSummary}`,
      cmp: violation.sourceComponentId,
    });
  }

  return out;
}

/**
 * Seed comment markers from `graph.comments`, positioned beside their host
 * component using laid geometry (falls back to a staggered default offset).
 * Moved verbatim from App's seedMarkers useMemo.
 */
export function seedMarkers(graph: UIGraph, laid: UIGraph | null): Marker[] {
  const laidComponents = laid?.components ?? graph.components;
  const laidEdges = laid?.edges ?? graph.edges;

  return graph.comments.map((cm, i) => {
    let host: ComponentDef | undefined = laidComponents.find((c) => c.id === cm.target.id);
    if (!host) {
      host = laidComponents.find(
        (c) =>
          c.internals.some(
            (it) =>
              it.id === cm.target.id || (it.members ?? []).some((mm) => mm.id === cm.target.id)
          ) || c.ports.some((p) => p.id === cm.target.id)
      );
    }
    if (!host && cm.target.type === 'edge') {
      const edge = laidEdges.find((e) => e.id === cm.target.id);
      if (edge) host = laidComponents.find((c) => c.id === edge.from);
    }

    let x = 80 + i * 130;
    let y = 30 + (i % 2) * 40;
    if (host && host.x != null && host.y != null && host.w != null) {
      x = host.x + host.w + 8;
      y = host.y - 10;
    }

    return { id: `seed-${i}`, n: i + 1, x, y, target: cm.target, body: cm.body, author: '@you', when: '2m' };
  });
}
