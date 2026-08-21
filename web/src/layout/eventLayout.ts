import ELK from 'elkjs/lib/elk.bundled.js';
import type { ElkNode, ElkExtendedEdge } from 'elkjs';
import type { EventComponent, EventLink, EventModel } from '../domain/eventModel';
import { buildLinks, shortKind, slotCount } from '../domain/eventModel';

/**
 * Layout for the event canvas.
 *
 * This is deliberately not the review canvas's layout(). That one lays out a
 * UIGraph — bounded contexts as compound nodes, components with ports and
 * expanded cards — and an event model has none of those. Reusing it would mean
 * dressing components up as something they are not to get coordinates back.
 * ELK is the shared thing here, not the graph shape.
 *
 * The direction is RIGHT because an event model reads as a flow: producers on
 * the left, the components they reach on the right.
 */

const elk = new ELK();

const ELK_OPTIONS: Record<string, string> = {
  'elk.algorithm': 'layered',
  'elk.direction': 'RIGHT',
  'elk.edgeRouting': 'ORTHOGONAL',
  'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
  'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
  'elk.spacing.nodeNode': '52',
  'elk.layered.spacing.nodeNodeBetweenLayers': '150',
  'elk.spacing.edgeNode': '24',
  'elk.spacing.edgeEdge': '14',
  // Edge labels are handed to ELK with their measured size, so it routes
  // around them instead of leaving them to land wherever they land. The side
  // is ELK's choice: forcing every label below its edge adds a label's height
  // of vertical space per edge, which on a model of any size is most of the
  // diagram.
  'elk.spacing.edgeLabel': '6',
  'elk.layered.edgeLabels.sideSelection': 'SMART_DOWN',
};

/** Label geometry, shared with the renderer so ELK reserves what gets drawn. */
export const LABEL_HEIGHT = 18;
export function labelText(link: EventLink): string {
  const first = shortKind(link.kinds[0].kind);
  return link.kinds.length === 1 ? first : `${first} +${link.kinds.length - 1}`;
}
export function labelWidth(text: string): number {
  // The label is 10px JetBrains Mono; 6.2px a glyph plus the box padding.
  return text.length * 6.2 + 12;
}

export const NODE_WIDTH = 208;
const NODE_BASE_HEIGHT = 62;
const NODE_ROW_HEIGHT = 15;
/** Beyond this many entries the node stops growing and the panel takes over. */
const NODE_MAX_ROWS = 6;

/** A laid-out component node. */
export interface EventNodeLayout {
  id: string;
  x: number;
  y: number;
  width: number;
  height: number;
  component: EventComponent;
}

/** A laid-out link, as the polyline ELK routed for it. */
export interface EventLinkLayout {
  link: EventLink;
  points: Array<{ x: number; y: number }>;
  /** Midpoint of the routed line, where the label sits. */
  labelX: number;
  labelY: number;
}

export interface EventLayout {
  nodes: EventNodeLayout[];
  links: EventLinkLayout[];
  width: number;
  height: number;
}

/**
 * A node is as tall as what it declares, up to a cap. The size is a cue about
 * weight, not a rendering of the contents: past a handful of entries the
 * difference between eleven and nineteen is not readable on a canvas, and the
 * detail panel is where anyone counting them is going to look.
 */
export function nodeHeight(component: EventComponent): number {
  const rows = Math.min(slotCount(component), NODE_MAX_ROWS);
  return NODE_BASE_HEIGHT + rows * NODE_ROW_HEIGHT;
}

export async function layoutEventModel(model: EventModel): Promise<EventLayout> {
  const links = buildLinks(model.flows);
  const components = [...model.components].sort((a, b) => a.id.localeCompare(b.id));

  if (components.length === 0) {
    return { nodes: [], links: [], width: 0, height: 0 };
  }

  const children: ElkNode[] = components.map((component) => ({
    id: component.id,
    width: NODE_WIDTH,
    height: nodeHeight(component),
  }));

  const known = new Set(components.map((c) => c.id));
  // A flow can name a component no document declares — a system is described
  // one application at a time, and the other end may not be in this repo. The
  // edge is dropped rather than pointing at a node that does not exist.
  const drawable = links.filter((link) => known.has(link.from) && known.has(link.to));

  const edges: ElkExtendedEdge[] = drawable.map((link, index) => {
    const text = labelText(link);
    return {
      id: `e${index}`,
      sources: [link.from],
      targets: [link.to],
      labels: [{ text, width: labelWidth(text), height: LABEL_HEIGHT }],
    };
  });

  const laid = await elk.layout({
    id: 'root',
    layoutOptions: ELK_OPTIONS,
    children,
    edges,
  });

  const byID = new Map(components.map((component) => [component.id, component]));
  const nodes: EventNodeLayout[] = (laid.children ?? []).flatMap((child) => {
    const component = byID.get(child.id);
    if (!component) return [];
    return [
      {
        id: child.id,
        x: child.x ?? 0,
        y: child.y ?? 0,
        width: child.width ?? NODE_WIDTH,
        height: child.height ?? nodeHeight(component),
        component,
      },
    ];
  });

  const positions = new Map(nodes.map((node) => [node.id, node]));
  const linkLayouts: EventLinkLayout[] = (laid.edges ?? []).flatMap((edge, index) => {
    const link = drawable[index];
    if (!link) return [];
    const points = edgePoints(edge, positions.get(link.from), positions.get(link.to));
    if (points.length < 2) return [];
    // ELK's placement when it made one, the polyline's own middle when it did
    // not — an edge it could not place a label on still gets a drawn one.
    const placed = edge.labels?.[0];
    const spot =
      placed && placed.x != null && placed.y != null
        ? { x: placed.x + (placed.width ?? 0) / 2, y: placed.y + (placed.height ?? LABEL_HEIGHT) / 2 }
        : midpoint(points);
    return [{ link, points, labelX: spot.x, labelY: spot.y }];
  });

  return {
    nodes,
    links: linkLayouts,
    width: laid.width ?? 0,
    height: laid.height ?? 0,
  };
}

/**
 * ELK's routed sections, flattened into one polyline. When it routes nothing —
 * which happens for an edge it could not place — the line falls back to the
 * straight run between the two nodes' centres, so a link is never silently
 * invisible.
 */
function edgePoints(
  edge: ElkExtendedEdge,
  from: EventNodeLayout | undefined,
  to: EventNodeLayout | undefined
): Array<{ x: number; y: number }> {
  const sections = edge.sections ?? [];
  if (sections.length > 0) {
    const points: Array<{ x: number; y: number }> = [];
    for (const section of sections) {
      if (points.length === 0) points.push({ x: section.startPoint.x, y: section.startPoint.y });
      for (const bend of section.bendPoints ?? []) points.push({ x: bend.x, y: bend.y });
      points.push({ x: section.endPoint.x, y: section.endPoint.y });
    }
    return points;
  }
  if (!from || !to) return [];
  return [
    { x: from.x + from.width, y: from.y + from.height / 2 },
    { x: to.x, y: to.y + to.height / 2 },
  ];
}

/**
 * The point halfway along the polyline by arc length, not the middle vertex.
 * An orthogonal route's vertices bunch at the corners, and a label pinned to
 * one of those sits in the bend rather than on the run.
 */
function midpoint(points: Array<{ x: number; y: number }>): { x: number; y: number } {
  let total = 0;
  const lengths: number[] = [];
  for (let i = 1; i < points.length; i++) {
    const length = Math.hypot(points[i].x - points[i - 1].x, points[i].y - points[i - 1].y);
    lengths.push(length);
    total += length;
  }
  if (total === 0) return points[0];

  let walked = 0;
  for (let i = 0; i < lengths.length; i++) {
    if (walked + lengths[i] >= total / 2) {
      const remaining = total / 2 - walked;
      const t = lengths[i] === 0 ? 0 : remaining / lengths[i];
      return {
        x: points[i].x + (points[i + 1].x - points[i].x) * t,
        y: points[i].y + (points[i + 1].y - points[i].y) * t,
      };
    }
    walked += lengths[i];
  }
  return points[points.length - 1];
}

/** An SVG path for a routed polyline. */
export function linkPath(points: Array<{ x: number; y: number }>): string {
  return points.map((p, i) => `${i === 0 ? 'M' : 'L'}${p.x} ${p.y}`).join(' ');
}
