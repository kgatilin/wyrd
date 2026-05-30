import type { UIGraph, BoundedContext, Component, Port } from '../types';

// Layout constants
const BC_PADDING = 30;
const BC_GAP = 40;
const BC_HEADER = 30;
const COMPONENT_W = 220;
const COMPONENT_H = 86;
const COMPONENT_GAP = 20;
const COMPONENTS_PER_ROW = 2;
const INTERNAL_BASE_H = 36;
const PORT_SPACING = 46;
const PORT_START_Y = 58;

/**
 * Computes deterministic layout for a UIGraph.
 * - BCs laid out left-to-right (wrap into rows if needed)
 * - Components in a grid inside their BC
 * - Ports stacked on left (in) / right (out) walls
 * - Preserves any geometry already present
 */
export function layout(graph: UIGraph): UIGraph {
  // Group components by BC
  const compsByBc = new Map<string, Component[]>();
  for (const c of graph.components) {
    const bcId = c.bc || 'default';
    if (!compsByBc.has(bcId)) compsByBc.set(bcId, []);
    compsByBc.get(bcId)!.push(c);
  }

  // Ensure all BCs exist (create default if components have no BC)
  const bcIds = new Set(graph.boundedContexts.map((bc) => bc.id));
  const allBcs: BoundedContext[] = [...graph.boundedContexts];
  for (const bcId of compsByBc.keys()) {
    if (!bcIds.has(bcId)) {
      allBcs.push({ id: bcId, name: bcId === 'default' ? 'Default' : bcId });
      bcIds.add(bcId);
    }
  }

  // Layout BCs
  const laidOutBcs: BoundedContext[] = [];
  let bcX = BC_GAP;
  const bcY = BC_GAP;

  for (const bc of allBcs) {
    // If BC already has geometry, preserve it
    if (hasGeometry(bc)) {
      laidOutBcs.push({ ...bc });
      continue;
    }

    const comps = compsByBc.get(bc.id) || [];
    const rows = Math.ceil(comps.length / COMPONENTS_PER_ROW);
    const cols = Math.min(comps.length, COMPONENTS_PER_ROW);

    const bcW = BC_PADDING * 2 + cols * COMPONENT_W + (cols - 1) * COMPONENT_GAP;
    const bcH = BC_PADDING + BC_HEADER + rows * COMPONENT_H + (rows - 1) * COMPONENT_GAP + BC_PADDING;

    laidOutBcs.push({
      ...bc,
      x: bcX,
      y: bcY,
      w: Math.max(bcW, 200),
      h: Math.max(bcH, 150),
    });

    bcX += Math.max(bcW, 200) + BC_GAP;
  }

  // Create BC lookup by id
  const bcLookup = new Map<string, BoundedContext>();
  for (const bc of laidOutBcs) {
    bcLookup.set(bc.id, bc);
  }

  // Layout components inside their BCs
  const laidOutComponents: Component[] = [];
  const compCountInBc = new Map<string, number>();

  for (const c of graph.components) {
    // If component already has geometry, preserve it
    if (hasComponentGeometry(c)) {
      laidOutComponents.push(layoutPorts({ ...c }));
      continue;
    }

    const bcId = c.bc || 'default';
    const bc = bcLookup.get(bcId);
    if (!bc) {
      // Should not happen, but fallback
      laidOutComponents.push(
        layoutPorts({
          ...c,
          x: 50,
          y: 50,
          w: COMPONENT_W,
          h: COMPONENT_H,
        })
      );
      continue;
    }

    const idx = compCountInBc.get(bcId) || 0;
    compCountInBc.set(bcId, idx + 1);

    const row = Math.floor(idx / COMPONENTS_PER_ROW);
    const col = idx % COMPONENTS_PER_ROW;

    const compX = bc.x! + BC_PADDING + col * (COMPONENT_W + COMPONENT_GAP);
    const compY = bc.y! + BC_HEADER + BC_PADDING + row * (COMPONENT_H + COMPONENT_GAP);

    laidOutComponents.push(
      layoutPorts({
        ...c,
        x: compX,
        y: compY,
        w: COMPONENT_W,
        h: COMPONENT_H,
        wx: c.wx ?? COMPONENT_W + 60,
        hx: c.hx ?? computeExpandedHeight(c),
      })
    );
  }

  return {
    ...graph,
    boundedContexts: laidOutBcs,
    components: laidOutComponents,
  };
}

function hasGeometry(bc: BoundedContext): boolean {
  return (
    typeof bc.x === 'number' &&
    typeof bc.y === 'number' &&
    typeof bc.w === 'number' &&
    typeof bc.h === 'number'
  );
}

function hasComponentGeometry(c: Component): boolean {
  return (
    typeof c.x === 'number' &&
    typeof c.y === 'number' &&
    typeof c.w === 'number' &&
    typeof c.h === 'number'
  );
}

function computeExpandedHeight(c: Component): number {
  // Base height + space for internals
  const internalRows = Math.ceil(c.internals.length / 2);
  const internalHeight = internalRows * (INTERNAL_BASE_H + 10);
  return COMPONENT_H + 50 + internalHeight;
}

function layoutPorts(c: Component): Component {
  const leftPorts = c.ports.filter((p) => p.side === 'left');
  const rightPorts = c.ports.filter((p) => p.side === 'right');

  const assignY = (ports: Port[], startY: number): Port[] => {
    return ports.map((p, i) => {
      // Preserve existing y if set
      if (typeof p.y === 'number') return p;
      return { ...p, y: startY + i * PORT_SPACING };
    });
  };

  return {
    ...c,
    ports: [...assignY(leftPorts, PORT_START_Y), ...assignY(rightPorts, PORT_START_Y)],
  };
}
