import ELK from 'elkjs/lib/elk.bundled.js';
import type { ElkNode, ElkPort, ElkExtendedEdge } from 'elkjs';
import type { UIGraph, BoundedContext, Component, Port, Edge, Internal } from '../types';

// ELK layout options mirrored from internal/adapter/http/assets/graph.js
const ELK_LAYOUT_OPTIONS: Record<string, string> = {
  'elk.algorithm': 'layered',
  'elk.edgeRouting': 'ORTHOGONAL',
  'elk.hierarchyHandling': 'INCLUDE_CHILDREN',
  'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
  'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
  // Spacing between sibling components (and between BCs) — widened ~1.7x so
  // components no longer crowd each other. Group-border padding (elk.padding
  // below) and internal element spacing (INTERNAL_GAP) are deliberately left
  // unchanged.
  'elk.spacing.nodeNode': '72',
  'elk.layered.spacing.nodeNodeBetweenLayers': '144',
  'elk.spacing.edgeNode': '32',
  'elk.spacing.edgeEdge': '20',
  'elk.direction': 'RIGHT',
};

// Collapsed component dimensions
const DEFAULT_W = 220;
const DEFAULT_H = 86;

// Internal card dimensions
const INTERNAL_W = 180;
const INTERNAL_HEADER_H = 26;
const INTERNAL_MEMBER_H = 18;
const INTERNAL_MEMBER_PADDING = 4;

// Component layout constants
const COMPONENT_HEADER_H = 36;
// Inner padding between the component border and its internal grid.
// Internals are absolutely positioned, so CSS padding on .hf-cmp-canvas is
// ignored for them — this padding is baked into the internal coordinates and
// into the component's expanded width/height. Kept equal to INTERNAL_GAP so the
// gap around the grid matches the gap between internal cards.
const CANVAS_PADDING = 10;

// Port layout within a component
const PORT_SPACING = 24;
const PORT_START_Y = 16;

// Internal grid layout spacing
const INTERNAL_GAP = 10;

export interface LayoutOptions {
  expanded: Set<string>;         // component ids currently expanded
  internalExpanded: Set<string>; // internal ids currently expanded (affects expanded height)
}

/**
 * Compute the height of an internal card based on whether it's expanded.
 */
function computeInternalHeight(internal: Internal, internalExpanded: Set<string>): number {
  if (internalExpanded.has(internal.id)) {
    const memberCount = internal.members?.length ?? 0;
    return INTERNAL_HEADER_H + (memberCount > 0 ? memberCount * INTERNAL_MEMBER_H + INTERNAL_MEMBER_PADDING : 0);
  }
  return INTERNAL_HEADER_H;
}

/**
 * Layout internals within an expanded component using a simple grid/packing algorithm.
 * Returns the internals with x, y, w, h set, plus the required canvas content dimensions.
 */
function layoutInternals(
  internals: Internal[],
  internalExpanded: Set<string>,
  availableWidth: number
): { laid: Internal[]; contentW: number; contentH: number } {
  if (internals.length === 0) {
    return { laid: [], contentW: 0, contentH: 0 };
  }

  // Compute heights for all internals
  const withHeights = internals.map((int) => ({
    internal: int,
    w: INTERNAL_W,
    h: computeInternalHeight(int, internalExpanded),
  }));

  // Simple row-based packing: place items left-to-right, wrap when exceeding available width
  const laid: Internal[] = [];
  let x = 0;
  let y = 0;
  let rowHeight = 0;
  let maxX = 0;

  for (const item of withHeights) {
    // Check if we need to wrap to next row
    if (x > 0 && x + item.w > availableWidth) {
      x = 0;
      y += rowHeight + INTERNAL_GAP;
      rowHeight = 0;
    }

    laid.push({
      ...item.internal,
      x,
      y,
      w: item.w,
      h: item.h,
    });

    maxX = Math.max(maxX, x + item.w);
    rowHeight = Math.max(rowHeight, item.h);
    x += item.w + INTERNAL_GAP;
  }

  const contentW = maxX;
  const contentH = y + rowHeight;

  return { laid, contentW, contentH };
}

/**
 * Compute the expanded dimensions of a component based on its internal layout.
 * This replaces the heuristic computeExpandedHeight function.
 */
function computeExpandedDimensions(
  component: Component,
  internalExpanded: Set<string>
): { w: number; h: number; internals: Internal[] } {
  // For expanded component, we need to lay out internals first to determine size
  const collapsedW = component.w ?? DEFAULT_W;
  const collapsedH = component.h ?? DEFAULT_H;
  const minWidth = Math.max(collapsedW, DEFAULT_W);
  const n = component.internals.length;

  // Compute desired column count for a balanced grid:
  // - Use sqrt(n) for rough balance, capped at 3 columns max
  // - At least 1 column
  const desiredCols = n > 0 ? Math.min(3, Math.ceil(Math.sqrt(n))) : 1;

  // Compute available width to fit that many columns
  // Each column is INTERNAL_W wide, with INTERNAL_GAP between columns
  const gridWidth = desiredCols * INTERNAL_W + (desiredCols - 1) * INTERNAL_GAP;
  const availableWidth = Math.max(minWidth - 2 * CANVAS_PADDING, gridWidth);

  // Layout internals with the computed available width
  const { laid, contentW, contentH } = layoutInternals(
    component.internals,
    internalExpanded,
    availableWidth
  );

  // Offset the internal grid by CANVAS_PADDING so there is a top/left gap
  // matching the spacing between cards. layoutInternals emits coords from
  // (0,0); the padding can't come from CSS because the cards are absolutely
  // positioned, so we bake it into the coordinates here.
  const offsetInternals = laid.map((it) => ({
    ...it,
    x: (it.x ?? 0) + CANVAS_PADDING,
    y: (it.y ?? 0) + CANVAS_PADDING,
  }));

  // Calculate final dimensions - must be >= collapsed dimensions
  const w = Math.max(minWidth, contentW + 2 * CANVAS_PADDING);
  const h = Math.max(collapsedH, COMPONENT_HEADER_H + 2 * CANVAS_PADDING + contentH);

  return { w, h, internals: offsetInternals };
}

/**
 * Extract short name from a component ID for synthesized port labels.
 * e.g., "internal/adapter/uigraph" -> "uigraph"
 */
function shortName(id: string): string {
  const parts = id.split('/');
  return parts[parts.length - 1] || id;
}

/**
 * Compute layout for a UIGraph using ELK.
 * Returns a NEW UIGraph with absolute canvas coordinates; input is not mutated.
 * BCs → compound ELK nodes; components → child nodes; ports → ELK ports;
 * internals are laid out within expanded components.
 */
export function layout(graph: UIGraph, opts?: LayoutOptions): Promise<UIGraph> {
  const expanded = opts?.expanded ?? new Set<string>();
  const internalExpanded = opts?.internalExpanded ?? new Set<string>();

  // --- 0. Pre-compute expanded component dimensions and internal layouts ---
  // We need to know component sizes BEFORE building ELK input, and we need
  // internal layouts for expanded components.

  const expandedLayouts = new Map<string, { w: number; h: number; internals: Internal[] }>();
  for (const c of graph.components) {
    if (expanded.has(c.id)) {
      expandedLayouts.set(c.id, computeExpandedDimensions(c, internalExpanded));
    }
  }

  // --- 1. Synthesize inbound ports for edges with undeclared toPort ---
  // Build a map of component ID -> list of ports to add
  const declaredPortIds = new Set<string>();
  for (const c of graph.components) {
    for (const p of c.ports) {
      declaredPortIds.add(p.id);
    }
  }

  // Map: targetComponentId -> list of synthesized ports
  const synthesizedPorts = new Map<string, Port[]>();

  for (const edge of graph.edges) {
    // If toPort is not declared, synthesize an inbound port on the target component
    if (!declaredPortIds.has(edge.toPort)) {
      const targetId = edge.to;
      if (!synthesizedPorts.has(targetId)) {
        synthesizedPorts.set(targetId, []);
      }

      // Create a stable ID derived from the edge
      const synthId = `${targetId}:synth:${edge.id}`;

      // Check if we already synthesized this port (edge could be processed multiple times)
      const existingPorts = synthesizedPorts.get(targetId)!;
      if (!existingPorts.some((p) => p.id === synthId)) {
        existingPorts.push({
          id: synthId,
          side: 'left',
          kind: 'in',
          name: shortName(edge.from),
        });
      }
    }
  }

  // --- 2. Build component list with synthesized ports ---
  const componentsWithSynthPorts: Component[] = graph.components.map((c) => {
    const synth = synthesizedPorts.get(c.id) ?? [];
    if (synth.length > 0) {
      return { ...c, ports: [...c.ports, ...synth] };
    }
    return c;
  });

  // Update declared ports set with synthesized ones
  for (const [, ports] of synthesizedPorts) {
    for (const p of ports) {
      declaredPortIds.add(p.id);
    }
  }

  // Create edge-to-synthPort mapping for routing
  const edgeToSynthPort = new Map<string, string>();
  for (const edge of graph.edges) {
    if (!graph.components.some((c) => c.ports.some((p) => p.id === edge.toPort))) {
      // toPort was not originally declared, use synthesized port
      const synthId = `${edge.to}:synth:${edge.id}`;
      edgeToSynthPort.set(edge.id, synthId);
    }
  }

  // --- 3. Build ELK input graph ---

  // Index components by BC
  const compsByBc = new Map<string, Component[]>();
  for (const c of componentsWithSynthPorts) {
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
      const expandedLayout = expandedLayouts.get(c.id);

      const w = isExpanded && expandedLayout ? expandedLayout.w : (c.w ?? DEFAULT_W);
      const h = isExpanded && expandedLayout ? expandedLayout.h : (c.h ?? DEFAULT_H);

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
  const componentIds = new Set<string>(componentsWithSynthPorts.map((c) => c.id));

  // Build ELK edges. Use synthesized ports where applicable.
  const elkEdges: ElkExtendedEdge[] = [];
  for (const edge of graph.edges) {
    const srcPort = edge.fromPort;
    const tgtPort = edgeToSynthPort.get(edge.id) ?? edge.toPort;

    const src = declaredPortIds.has(srcPort) ? srcPort
      : componentIds.has(edge.from) ? edge.from
      : null;
    const tgt = declaredPortIds.has(tgtPort) ? tgtPort
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

  // --- 4. Run ELK ---

  const elk = new ELK();

  return elk.layout(elkRoot).then((laid) => {
    // --- 5. Flatten ELK output to absolute coords ---

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

    // Build returned components with absolute coords + port y values + internal layouts
    const returnedComponents: Component[] = graph.components.map((c) => {
      const info = laidCmpMap.get(c.id);
      const cmpAbsX = (info?.bcX ?? 0) + (info?.node.x ?? 0);
      const cmpAbsY = (info?.bcY ?? 0) + (info?.node.y ?? 0);
      const cmpW = info?.node.width ?? (c.w ?? DEFAULT_W);
      const cmpH = info?.node.height ?? (c.h ?? DEFAULT_H);

      // Get original ports + synthesized ports
      const componentWithSynth = componentsWithSynthPorts.find((cws) => cws.id === c.id)!;

      // Map port y-values: ELK port coords are relative to component
      const elkPortMap = new Map<string, ElkPort>();
      for (const ep of (info?.node.ports ?? [])) {
        elkPortMap.set(ep.id, ep);
      }

      const returnedPorts: Port[] = componentWithSynth.ports.map((p, i) => {
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

      // Get internal layout for expanded components
      const isExpanded = expanded.has(c.id);
      const expandedLayout = expandedLayouts.get(c.id);
      const returnedInternals = isExpanded && expandedLayout
        ? expandedLayout.internals
        : c.internals;

      return {
        ...c,
        x: cmpAbsX,
        y: cmpAbsY,
        w: cmpW,
        h: cmpH,
        ports: returnedPorts,
        internals: returnedInternals,
      };
    });

    // Build returned edges with routed points (absolute coords)
    // With elk.hierarchyHandling: INCLUDE_CHILDREN, ELK expresses edge section
    // coordinates relative to the LOWEST COMMON ANCESTOR (LCA) of the edge's
    // two endpoints, NOT relative to the node whose .edges array holds the edge.
    // For two components in the same BC, the LCA is that BC.
    // For components in different BCs, the LCA is root (offset 0,0).

    // Build componentId → bcId map
    const componentToBc = new Map<string, string>();
    for (const c of componentsWithSynthPorts) {
      componentToBc.set(c.id, c.bc || 'default');
    }

    // Build bcId → absolute offset map
    const bcAbsoluteOffset = new Map<string, { x: number; y: number }>();
    for (const bcNode of laid.children ?? []) {
      bcAbsoluteOffset.set(bcNode.id, { x: bcNode.x ?? 0, y: bcNode.y ?? 0 });
    }

    // Collect raw edge sections from ELK output (they live in root.edges)
    const rawEdgeSections = new Map<string, { startPoint: { x: number; y: number }; bendPoints?: { x: number; y: number }[]; endPoint: { x: number; y: number } }>();
    for (const edge of (laid.edges ?? []) as ElkExtendedEdge[]) {
      const sections = (edge as any).sections;
      if (sections && sections.length > 0) {
        rawEdgeSections.set(edge.id, sections[0]);
      }
    }

    // For each edge, compute the LCA offset and apply to section points
    const edgePointsMap = new Map<string, { x: number; y: number }[]>();
    for (const edge of graph.edges) {
      const section = rawEdgeSections.get(edge.id);
      if (!section) continue;

      // Determine LCA offset: if source and target are in the same BC, use BC offset; else use (0,0)
      const srcBc = componentToBc.get(edge.from);
      const tgtBc = componentToBc.get(edge.to);
      let offsetX = 0;
      let offsetY = 0;
      if (srcBc && tgtBc && srcBc === tgtBc) {
        const bcOffset = bcAbsoluteOffset.get(srcBc);
        if (bcOffset) {
          offsetX = bcOffset.x;
          offsetY = bcOffset.y;
        }
      }

      // Build absolute points
      const points: { x: number; y: number }[] = [];
      if (section.startPoint) {
        points.push({ x: offsetX + section.startPoint.x, y: offsetY + section.startPoint.y });
      }
      for (const bp of section.bendPoints ?? []) {
        points.push({ x: offsetX + bp.x, y: offsetY + bp.y });
      }
      if (section.endPoint) {
        points.push({ x: offsetX + section.endPoint.x, y: offsetY + section.endPoint.y });
      }

      if (points.length >= 2) {
        edgePointsMap.set(edge.id, points);
      }
    }

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

