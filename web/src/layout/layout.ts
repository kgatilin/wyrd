import ELK from 'elkjs/lib/elk.bundled.js';
import type { ElkNode, ElkPort, ElkExtendedEdge } from 'elkjs';
import type { UIGraph, BoundedContext, Component, Port, Edge } from '../types';
import { computeExpandedHeight } from '../state/hooks';

// ELK layout options mirrored from internal/adapter/http/assets/graph.js
const ELK_LAYOUT_OPTIONS: Record<string, string> = {
  'elk.algorithm': 'layered',
  'elk.edgeRouting': 'ORTHOGONAL',
  'elk.hierarchyHandling': 'INCLUDE_CHILDREN',
  'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
  'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
  'elk.spacing.nodeNode': '42',
  'elk.layered.spacing.nodeNodeBetweenLayers': '96',
  'elk.spacing.edgeNode': '32',
  'elk.spacing.edgeEdge': '20',
  'elk.direction': 'RIGHT',
};

// Collapsed component dimensions
const DEFAULT_W = 220;
const DEFAULT_H = 86;
const DEFAULT_WX_EXTRA = 60; // expanded width = collapsed + this

// Port layout within a component
const PORT_SPACING = 24;
const PORT_START_Y = 16;

export interface LayoutOptions {
  expanded: Set<string>;         // component ids currently expanded
  internalExpanded: Set<string>; // internal ids currently expanded (affects expanded height)
}

/**
 * Compute layout for a UIGraph using ELK.
 * Returns a NEW UIGraph with absolute canvas coordinates; input is not mutated.
 * BCs → compound ELK nodes; components → child nodes; ports → ELK ports;
 * internals are NOT given to ELK (opaque sized box).
 */
export function layout(graph: UIGraph, opts?: LayoutOptions): Promise<UIGraph> {
  const expanded = opts?.expanded ?? new Set<string>();
  const internalExpanded = opts?.internalExpanded ?? new Set<string>();

  // --- 1. Build ELK input graph ---

  // Index components by BC
  const compsByBc = new Map<string, Component[]>();
  for (const c of graph.components) {
    const bcId = c.bc || 'default';
    if (!compsByBc.has(bcId)) compsByBc.set(bcId, []);
    compsByBc.get(bcId)!.push(c);
  }

  // Ensure all BCs are represented
  const allBcIds = new Set(graph.boundedContexts.map((bc) => bc.id));
  const allBcs: BoundedContext[] = [...graph.boundedContexts];
  for (const bcId of compsByBc.keys()) {
    if (!allBcIds.has(bcId)) {
      allBcs.push({ id: bcId, name: bcId === 'default' ? 'Default' : bcId });
    }
  }

  // Build ELK child nodes for each BC
  const elkBcNodes: ElkNode[] = allBcs.map((bc) => {
    const comps = compsByBc.get(bc.id) ?? [];
    const children: ElkNode[] = comps.map((c) => {
      const isExpanded = expanded.has(c.id);
      const w = isExpanded ? (c.wx ?? (c.w ?? DEFAULT_W) + DEFAULT_WX_EXTRA) : (c.w ?? DEFAULT_W);
      const h = isExpanded
        ? computeExpandedHeight(c, internalExpanded)
        : (c.h ?? DEFAULT_H);

      // Build ELK ports
      const ports: ElkPort[] = c.ports.map((p) => ({
        id: p.id,
        layoutOptions: {
          'port.side': p.side === 'left' ? 'WEST' : 'EAST',
        },
      }));

      return {
        id: c.id,
        width: w,
        height: h,
        ports,
        layoutOptions: {
          'elk.portConstraints': 'FIXED_SIDE',
        },
      };
    });

    return {
      id: bc.id,
      children,
      layoutOptions: {
        'elk.padding': '[top=30, left=30, bottom=30, right=30]',
      },
    };
  });

  // Build sets of valid ELK references for resilient edge resolution.
  // The real archai projection emits placeholder port ids (e.g. "<pkg>:in:...")
  // that are never declared as actual ports. ELK rejects any reference to an
  // undeclared shape, so we must fall back to the component id in that case.
  const declaredPortIds = new Set<string>();
  for (const c of graph.components) {
    for (const p of c.ports) {
      declaredPortIds.add(p.id);
    }
  }
  const componentIds = new Set<string>(graph.components.map((c) => c.id));

  // Build ELK edges (between ports, scoped at root so ELK handles hierarchy).
  // For each endpoint: use the port id if declared, otherwise fall back to the
  // component id. Drop the edge entirely if either endpoint cannot be resolved
  // to a known ELK shape (port or component) — the renderer already handles
  // edges without routed points via a bezier fallback.
  const elkEdges: ElkExtendedEdge[] = [];
  for (const edge of graph.edges) {
    const src = declaredPortIds.has(edge.fromPort) ? edge.fromPort
      : componentIds.has(edge.from) ? edge.from
      : null;
    const tgt = declaredPortIds.has(edge.toPort) ? edge.toPort
      : componentIds.has(edge.to) ? edge.to
      : null;
    if (src !== null && tgt !== null) {
      elkEdges.push({ id: edge.id, sources: [src], targets: [tgt] });
    }
  }

  const elkRoot: ElkNode = {
    id: 'root',
    layoutOptions: ELK_LAYOUT_OPTIONS,
    children: elkBcNodes,
    edges: elkEdges,
  };

  // --- 2. Run ELK ---

  const elk = new ELK();

  return elk.layout(elkRoot).then((laid) => {
    // --- 3. Flatten ELK output to absolute coords ---

    const laidBcMap = new Map<string, ElkNode>();
    const laidCmpMap = new Map<string, { node: ElkNode; bcX: number; bcY: number }>();

    for (const bcNode of (laid.children ?? [])) {
      laidBcMap.set(bcNode.id, bcNode);
      const bcAbsX = bcNode.x ?? 0;
      const bcAbsY = bcNode.y ?? 0;

      for (const cmpNode of (bcNode.children ?? [])) {
        laidCmpMap.set(cmpNode.id, { node: cmpNode, bcX: bcAbsX, bcY: bcAbsY });
      }
    }

    // Build returned BCs with absolute coords
    const returnedBcs: BoundedContext[] = allBcs.map((bc) => {
      const elkBc = laidBcMap.get(bc.id);
      return {
        ...bc,
        x: elkBc?.x ?? 0,
        y: elkBc?.y ?? 0,
        w: elkBc?.width ?? DEFAULT_W,
        h: elkBc?.height ?? DEFAULT_H,
      };
    });

    // Build returned components with absolute coords + port y values
    const returnedComponents: Component[] = graph.components.map((c) => {
      const info = laidCmpMap.get(c.id);
      const cmpAbsX = (info?.bcX ?? 0) + (info?.node.x ?? 0);
      const cmpAbsY = (info?.bcY ?? 0) + (info?.node.y ?? 0);
      const cmpW = info?.node.width ?? (c.w ?? DEFAULT_W);
      const cmpH = info?.node.height ?? (c.h ?? DEFAULT_H);

      // Map port y-values: ELK port coords are relative to component; keep that convention
      const elkPortMap = new Map<string, ElkPort>();
      for (const ep of (info?.node.ports ?? [])) {
        elkPortMap.set(ep.id, ep);
      }

      const returnedPorts: Port[] = c.ports.map((p, i) => {
        const ep = elkPortMap.get(p.id);
        let portY: number;
        if (ep && typeof ep.y === 'number') {
          portY = ep.y;
        } else {
          // Fallback: evenly spaced
          portY = PORT_START_Y + i * PORT_SPACING;
        }
        return { ...p, y: portY };
      });

      return {
        ...c,
        x: cmpAbsX,
        y: cmpAbsY,
        w: cmpW,
        h: cmpH,
        ports: returnedPorts,
      };
    });

    // Build returned edges with routed points (absolute coords)
    // ELK edge sections are relative to the edge's containing node (root in our case).
    // Root has no offset, so section coords are canvas-absolute.
    const edgePointsMap = new Map<string, { x: number; y: number }[]>();
    collectEdgePoints(laid, 0, 0, edgePointsMap);

    const returnedEdges: Edge[] = graph.edges.map((edge) => ({
      ...edge,
      points: edgePointsMap.get(edge.id),
    }));

    return {
      ...graph,
      boundedContexts: returnedBcs,
      components: returnedComponents,
      edges: returnedEdges,
    };
  });
}

/**
 * Recursively walk ELK output nodes, collecting edge routing sections.
 * ELK edge section coordinates are relative to the node that contains the edge.
 * Walk the containment tree accumulating absolute offsets.
 */
function collectEdgePoints(
  node: ElkNode,
  parentAbsX: number,
  parentAbsY: number,
  result: Map<string, { x: number; y: number }[]>
): void {
  const absX = parentAbsX + (node.x ?? 0);
  const absY = parentAbsY + (node.y ?? 0);

  for (const edge of (node.edges ?? []) as ElkExtendedEdge[]) {
    const sections = (edge as any).sections;
    if (!sections || sections.length === 0) continue;

    const section = sections[0];
    const points: { x: number; y: number }[] = [];

    if (section.startPoint) {
      points.push({ x: absX + section.startPoint.x, y: absY + section.startPoint.y });
    }
    for (const bp of (section.bendPoints ?? [])) {
      points.push({ x: absX + bp.x, y: absY + bp.y });
    }
    if (section.endPoint) {
      points.push({ x: absX + section.endPoint.x, y: absY + section.endPoint.y });
    }

    if (points.length >= 2) {
      result.set(edge.id, points);
    }
  }

  for (const child of (node.children ?? [])) {
    collectEdgePoints(child, absX, absY, result);
  }
}
