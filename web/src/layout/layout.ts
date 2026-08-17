import ELK from 'elkjs/lib/elk.bundled.js';
import type { ElkNode, ElkPort, ElkExtendedEdge } from 'elkjs';
import type { UIGraph, BoundedContext, Component, Port, Edge, SymbolRelation } from '../types';
import { componentPathPrefix } from '../domain/componentPath';
import { buildCardModel, type CardFile } from '../domain/cardModel';
import { layoutCard } from './cardLayout';

// Spacing between sibling components. These MUST be set on the node that owns the
// components — i.e. each bounded-context compound node — not just on the root.
// Root-level spacing only governs spacing *between* bounded contexts; the layered
// layout *inside* a BC reads spacing from the BC node, so options left only on the
// root fall back to ELK's ~20px defaults (which is why earlier bumps did nothing).
const SPACING_NODE_NODE = '72'; // vertical gap between components in the same layer
const SPACING_BETWEEN_LAYERS = '72'; // horizontal gap between component columns

// ELK layout options mirrored from internal/adapter/http/assets/graph.js
const ELK_LAYOUT_OPTIONS: Record<string, string> = {
  'elk.algorithm': 'layered',
  'elk.edgeRouting': 'ORTHOGONAL',
  'elk.hierarchyHandling': 'INCLUDE_CHILDREN',
  'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
  'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
  'elk.spacing.nodeNode': SPACING_NODE_NODE,
  'elk.layered.spacing.nodeNodeBetweenLayers': SPACING_BETWEEN_LAYERS,
  'elk.spacing.edgeNode': '32',
  'elk.spacing.edgeEdge': '20',
  'elk.direction': 'RIGHT',
};

// Collapsed component dimensions
const DEFAULT_W = 220;
const DEFAULT_H = 86;

// Component layout constants
const COMPONENT_HEADER_H = 36;
// Inner padding between the component border and its file containers. Those are
// absolutely positioned, so CSS padding on .hf-cmp-canvas is ignored for them —
// the padding is baked into the container coordinates and into the component's
// expanded width/height instead.
const CANVAS_PADDING = 10;

// Port layout within a component
const PORT_SPACING = 24;
const PORT_START_Y = 16;

// Collapsed-card height. Grows to fit the full description so long text is never
// clipped, with a minimum of ~1.5x the old default (86) for breathing room.
const COLLAPSED_MIN_H = 129; // ≈ 1.5 * DEFAULT_H
const COMPACT_COLLAPSED_MIN_H = 58;
const DESC_LINE_H = 16; // approx line box of .hf-cmp-desc (11px × 1.4)
const DESC_PAD_V = 16; // .hf-cmp-desc top + bottom padding
const DESC_CHARS_PER_LINE = 30; // conservative wrap width (~196px) → overestimates lines

/**
 * Height of a collapsed component: header + enough room for the whole
 * description, floored at COLLAPSED_MIN_H so short/empty descriptions still get
 * a comfortably tall card.
 */
function computeCollapsedHeight(component: Component, density: 'detailed' | 'compact' = 'detailed'): number {
  if (density === 'compact') {
    return Math.max(component.h ?? COMPACT_COLLAPSED_MIN_H, COMPACT_COLLAPSED_MIN_H);
  }
  const desc = component.desc ?? '';
  const lines = desc.length > 0 ? Math.ceil(desc.length / DESC_CHARS_PER_LINE) : 0;
  const descH = lines > 0 ? DESC_PAD_V + lines * DESC_LINE_H : 0;
  return Math.max(COLLAPSED_MIN_H, COMPONENT_HEADER_H + descH);
}

// Collapsed-card width: wide enough to keep the header (icon + name + tech tag +
// action button) on a single line so even short tech labels like "Go - gRPC"
// don't wrap. Glyph advances are estimated generously to avoid clipping.
const NAME_CHAR_W = 7.6; // .hf-cmp-name (Inter ~12.5px, semibold)
const PATH_CHAR_W = 6.4; // .hf-cmp-path (JetBrains Mono 10.5px) directory prefix
const TECH_CHAR_W = 6.4; // .hf-cmp-tech (JetBrains Mono 10px)
const TECH_CHROME_W = 16; // tech tag padding + border
const LAYER_BADGE_W = 62; // public/internal package layer badge
const HEAD_ICON_W = 18;
const HEAD_GAP = 8;
const HEAD_PAD_L = 12;
const HEAD_ACTIONS_W = 40; // reserved right side for the collapsed +/- button

function computeCollapsedWidth(component: Component, density: 'detailed' | 'compact' = 'detailed'): number {
  const nameW =
    component.name.length * NAME_CHAR_W +
    componentPathPrefix(component.id, component.name).length * PATH_CHAR_W;
  const techW = density === 'compact' ? 0 : component.tech ? component.tech.length * TECH_CHAR_W + TECH_CHROME_W : 0;
  const needed =
    HEAD_PAD_L + HEAD_ICON_W + HEAD_GAP + nameW + (techW ? HEAD_GAP + techW : 0) + HEAD_GAP + LAYER_BADGE_W + HEAD_ACTIONS_W;
  return Math.max(component.w ?? DEFAULT_W, DEFAULT_W, Math.ceil(needed));
}

export interface LayoutOptions {
  expanded: Set<string>;      // component ids currently expanded
  seqMode?: Set<string>;      // expanded components showing their call-sequence (fixed frame)
  // 'compact' shows class headers only; 'detailed' shows their full bodies.
  cardDensity?: 'detailed' | 'compact';
  // Whether class bodies show the right-hand type column.
  showInlineSignatures?: boolean;
}

// Fixed frame for a card in sequence mode; the diagram scrolls inside it, so
// layout stays independent of the (async-fetched) sequence content.
const SEQ_CARD_W = 620;
const SEQ_CARD_H = 420;

/**
 * Relations that stay inside one package — the only ones a card can draw.
 */
function cardRelations(componentId: string, relations: SymbolRelation[]): SymbolRelation[] {
  return relations.filter(
    (relation) =>
      relation.fromComponentId === componentId &&
      relation.toComponentId === componentId &&
      relation.fromInternalId &&
      relation.toInternalId &&
      relation.fromInternalId !== relation.toInternalId
  );
}

/**
 * Compute the expanded dimensions of a component from the layout of its
 * source-file containers.
 */
async function computeExpandedDimensions(
  component: Component,
  relations: SymbolRelation[],
  cardDensity: 'detailed' | 'compact',
  showInlineSignatures: boolean
): Promise<{ w: number; h: number; files: CardFile[] }> {
  const collapsedW = computeCollapsedWidth(component, cardDensity);
  // Floor expanded height at the collapsed height so expanding never shrinks a card.
  const collapsedH = computeCollapsedHeight(component, cardDensity);
  const minWidth = Math.max(collapsedW, DEFAULT_W);

  // A compact card shows class headers only; the type column follows the
  // toolbar's signature toggle. layoutCard owns the wrap width — it is the
  // only place that knows container widths and the gaps between them.
  const model = buildCardModel(component.internals);
  const { files, contentW, contentH } = await layoutCard(model, cardRelations(component.id, relations), {
    showRows: cardDensity !== 'compact',
    showTypes: showInlineSignatures,
    minWidth: minWidth - 2 * CANVAS_PADDING,
  });

  // Offset the containers by CANVAS_PADDING so there is a top/left gap matching
  // the spacing between cards. layoutCard emits coords from (0,0); the padding
  // can't come from CSS because the containers are absolutely positioned, so we
  // bake it into the coordinates here.
  const offsetFiles = files.map((file) => ({
    ...file,
    x: (file.x ?? 0) + CANVAS_PADDING,
    y: (file.y ?? 0) + CANVAS_PADDING,
  }));

  const w = Math.max(minWidth, contentW + 2 * CANVAS_PADDING);
  const h = Math.max(collapsedH, COMPONENT_HEADER_H + 2 * CANVAS_PADDING + contentH);

  return { w, h, files: offsetFiles };
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
 * source-file containers are laid out within expanded components.
 */
export async function layout(graph: UIGraph, opts?: LayoutOptions): Promise<UIGraph> {
  const expanded = opts?.expanded ?? new Set<string>();
  const seqMode = opts?.seqMode ?? new Set<string>();
  const cardDensity = opts?.cardDensity ?? 'detailed';
  const showInlineSignatures = opts?.showInlineSignatures ?? true;

  // --- 0. Pre-compute expanded component dimensions and card layouts ---
  // We need to know component sizes BEFORE building ELK input, and we need
  // card layouts for expanded components. Seq-mode cards get a fixed
  // frame instead — no card layout needed for them.

  const expandedLayouts = new Map<string, { w: number; h: number; files: CardFile[] }>();
  for (const c of graph.components) {
    if (expanded.has(c.id) && !seqMode.has(c.id)) {
      expandedLayouts.set(c.id, await computeExpandedDimensions(
        c,
        graph.relations ?? [],
        cardDensity,
        showInlineSignatures
      ));
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
      const isSeq = isExpanded && seqMode.has(c.id);
      const expandedLayout = expandedLayouts.get(c.id);

      const w = isSeq ? SEQ_CARD_W : isExpanded && expandedLayout ? expandedLayout.w : computeCollapsedWidth(c, cardDensity);
      const h = isSeq ? SEQ_CARD_H : isExpanded && expandedLayout ? expandedLayout.h : computeCollapsedHeight(c, cardDensity);

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
        // Spacing for the components laid out INSIDE this BC (see note above).
        'elk.spacing.nodeNode': SPACING_NODE_NODE,
        'elk.layered.spacing.nodeNodeBetweenLayers': SPACING_BETWEEN_LAYERS,
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
      const cmpW = info?.node.width ?? computeCollapsedWidth(c, cardDensity);
      const cmpH = info?.node.height ?? c.h ?? computeCollapsedHeight(c, cardDensity);

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

      // Get the card layout for expanded components. Collapsed cards carry no
      // file containers — they render their description instead.
      const isExpanded = expanded.has(c.id);
      const expandedLayout = expandedLayouts.get(c.id);

      return {
        ...c,
        x: cmpAbsX,
        y: cmpAbsY,
        w: cmpW,
        h: cmpH,
        ports: returnedPorts,
        files: isExpanded && expandedLayout ? expandedLayout.files : undefined,
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
