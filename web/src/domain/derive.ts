import type { UIGraph, Diff, Component as ComponentDef } from '../types';
import type { AppUI, Interaction, Marker } from './state';

/** Focused component + its direct edge neighbours; null when nothing is focused. */
export function relatedIds(graph: UIGraph, focusId: string | null): Set<string> | null {
  if (!focusId) return null;
  const r = new Set<string>([focusId]);
  for (const edge of graph.edges) {
    if (edge.from === focusId) r.add(edge.to);
    if (edge.to === focusId) r.add(edge.from);
  }
  return r;
}

/** Project the UI slice down to the inputs the layout engine needs. */
export function toInteraction(ui: AppUI): Interaction {
  return { expanded: ui.expanded, internalExpanded: ui.internalExpanded, internalWide: ui.internalWide };
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

/** Which components start expanded after a graph loads ("orders" if present, else first). */
export function initialExpanded(graph: UIGraph): string[] {
  const orders = graph.components.find((c) => c.id === 'orders');
  if (orders) return ['orders'];
  if (graph.components.length > 0) return [graph.components[0].id];
  return [];
}

/** A change entry derived from graph elements with diff flags. */
export interface ChangeEntry {
  id: string;
  kind: Diff;
  name: string;
  where: string;
  cmp: string;
  internal?: string;
  member?: string;
  port?: string;
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
        out.push({ id: `int-${i.id}`, kind: i.diff, name: i.name, where: `${i.kind} - ${c.name}`, cmp: c.id, internal: i.id });
      }
      for (const m of i.members ?? []) {
        if (m.diff) {
          out.push({ id: `mem-${m.id}`, kind: m.diff, name: m.name, where: `${m.kind} - ${i.name}`, cmp: c.id, internal: i.id, member: m.id });
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
