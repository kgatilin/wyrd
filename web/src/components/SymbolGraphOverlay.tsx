import type { Component, Internal, Member, SymbolRelation, UIGraph } from '../types';
import type { SymbolFocusTarget } from '../domain/symbolFocus';

export interface SymbolGraphOverlayProps {
  graph: UIGraph;
  target: SymbolFocusTarget;
  onClose: () => void;
}

interface SymbolNode {
  id: string;
  label: string;
  kind: string;
  componentId: string;
  packageName: string;
  internalId: string;
  memberId?: string;
  exported?: boolean;
}

interface SymbolEdge {
  id: string;
  kind: string;
  from: string;
  to: string;
  synthetic?: boolean;
}

interface PositionedNode extends SymbolNode {
  x: number;
  y: number;
  depth: number;
}

const NODE_W = 178;
const NODE_H = 46;
const COL_W = 230;
const ROW_H = 70;
const PAD = 18;
const MAX_NODES = 80;

export function SymbolGraphOverlay({ graph, target, onClose }: SymbolGraphOverlayProps) {
  const model = buildSymbolGraph(graph, target);
  const selected = model.nodes.find((node) => node.id === model.selectedId);
  if (!selected) return null;

  return (
    <div className="hf-symbol-overlay" onClick={(e) => e.stopPropagation()}>
      <div className="hf-symbol-panel">
        <div className="hf-symbol-head">
          <div>
            <div className="hf-symbol-title">{selected.label}</div>
            <div className="hf-symbol-subtitle">{selected.packageName}</div>
          </div>
          <button className="hf-symbol-close" onClick={onClose} title="Close symbol graph">x</button>
        </div>
        <div className="hf-symbol-stage-wrap">
          <div
            className="hf-symbol-stage"
            style={{ width: model.width, height: model.height }}
          >
            <svg className="hf-symbol-edges" width={model.width} height={model.height}>
              <defs>
                <marker id="hf-symbol-arr" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto">
                  <path d="M 0 0 L 10 5 L 0 10 z" className="hf-symbol-arrow" />
                </marker>
              </defs>
              {model.edges.map((edge, index) => {
                const from = model.positioned.get(edge.from);
                const to = model.positioned.get(edge.to);
                if (!from || !to) return null;
                const path = edgePath(from, to, index);
                const mid = edgeMid(from, to, index);
                return (
                  <g key={edge.id} className={edge.synthetic ? 'synthetic' : ''}>
                    <path d={path} className={`hf-symbol-edge ${edgeKindClass(edge.kind)}`} markerEnd="url(#hf-symbol-arr)" />
                    <text x={mid.x} y={mid.y} className="hf-symbol-edge-label" textAnchor="middle">
                      {edge.kind}
                    </text>
                  </g>
                );
              })}
            </svg>
            {model.nodes.map((node) => (
              <div
                key={node.id}
                className={`hf-symbol-node ${node.id === model.selectedId ? 'selected' : ''} ${symbolVisibilityClass(node.exported)}`}
                style={{ left: node.x, top: node.y }}
                title={`${node.packageName}.${node.label}`}
              >
                <span className="hf-symbol-node-kind">{node.kind}</span>
                <span className="hf-symbol-node-label">{node.label}</span>
                <span className="hf-symbol-node-pkg">{node.packageName}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function buildSymbolGraph(graph: UIGraph, target: SymbolFocusTarget): {
  selectedId: string;
  nodes: PositionedNode[];
  edges: SymbolEdge[];
  positioned: Map<string, PositionedNode>;
  width: number;
  height: number;
} {
  const symbols = symbolIndex(graph);
  const selectedId = target.memberId ?? target.internalId;
  const edges = relationEdges(graph, symbols);
  const reachable = reachableSubgraph(selectedId, edges);
  const nodes = layoutNodes([...reachable.nodes].map((id) => symbols.get(id)).filter((node): node is SymbolNode => !!node), reachable.depths);
  const positioned = new Map(nodes.map((node) => [node.id, node]));
  const visibleEdges = reachable.edges.filter((edge) => positioned.has(edge.from) && positioned.has(edge.to));
  const maxDepth = nodes.reduce((max, node) => Math.max(max, node.depth), 0);
  const maxRows = Math.max(1, ...[...groupByDepth(nodes).values()].map((items) => items.length));

  return {
    selectedId,
    nodes,
    edges: visibleEdges,
    positioned,
    width: Math.max(560, PAD * 2 + NODE_W + maxDepth * COL_W),
    height: Math.max(260, PAD * 2 + NODE_H + (maxRows - 1) * ROW_H),
  };
}

function symbolIndex(graph: UIGraph): Map<string, SymbolNode> {
  const out = new Map<string, SymbolNode>();
  for (const component of graph.components) {
    for (const internal of component.internals) {
      const internalNode = internalSymbolNode(component, internal);
      out.set(internalNode.id, internalNode);
      for (const member of internal.members ?? []) {
        const memberNode = memberSymbolNode(component, internal, member);
        out.set(memberNode.id, memberNode);
      }
    }
  }
  return out;
}

function internalSymbolNode(component: Component, internal: Internal): SymbolNode {
  return {
    id: internal.id,
    label: internal.name,
    kind: internal.kind,
    componentId: component.id,
    packageName: component.name || component.id,
    internalId: internal.id,
    exported: internal.exported,
  };
}

function memberSymbolNode(component: Component, internal: Internal, member: Member): SymbolNode {
  return {
    id: member.id,
    label: member.name,
    kind: member.kind === 'method' ? 'method' : member.kind,
    componentId: component.id,
    packageName: component.name || component.id,
    internalId: internal.id,
    memberId: member.id,
    exported: member.exported,
  };
}

function relationEdges(graph: UIGraph, symbols: Map<string, SymbolNode>): SymbolEdge[] {
  const edges = new Map<string, SymbolEdge>();
  const add = (edge: SymbolEdge) => {
    if (!symbols.has(edge.from) || !symbols.has(edge.to)) return;
    edges.set(edge.id, edge);
  };

  for (const relation of graph.relations ?? []) {
    const from = endpointId(relation, 'from');
    const to = endpointId(relation, 'to');
    if (!from || !to) continue;
    add({ id: relation.id, kind: relation.kind, from, to });
  }

  for (const edge of implementationMemberEdges(graph)) {
    add(edge);
  }

  return [...edges.values()];
}

function endpointId(relation: SymbolRelation, side: 'from' | 'to'): string | null {
  if (side === 'from') return relation.fromMemberId || relation.fromInternalId || null;
  return relation.toMemberId || relation.toInternalId || null;
}

function implementationMemberEdges(graph: UIGraph): SymbolEdge[] {
  const componentById = new Map(graph.components.map((component) => [component.id, component]));
  const out: SymbolEdge[] = [];
  for (const relation of graph.relations ?? []) {
    if (relation.kind !== 'implements' || !relation.fromInternalId || !relation.toInternalId) continue;
    const concrete = componentById.get(relation.fromComponentId)?.internals.find((internal) => internal.id === relation.fromInternalId);
    const iface = componentById.get(relation.toComponentId)?.internals.find((internal) => internal.id === relation.toInternalId);
    if (!concrete || !iface) continue;
    const concreteByName = new Map((concrete.members ?? []).map((member) => [methodKey(member.name), member]));
    for (const ifaceMember of iface.members ?? []) {
      const concreteMember = concreteByName.get(methodKey(ifaceMember.name));
      if (!concreteMember) continue;
      out.push({
        id: `impl:${concreteMember.id}->${ifaceMember.id}`,
        kind: 'implements',
        from: concreteMember.id,
        to: ifaceMember.id,
        synthetic: true,
      });
    }
  }
  return out;
}

function methodKey(name: string): string {
  return name.split('(')[0].split(':')[0].trim();
}

function reachableSubgraph(selectedId: string, edges: SymbolEdge[]): { nodes: Set<string>; edges: SymbolEdge[]; depths: Map<string, number> } {
  const nodes = new Set<string>([selectedId]);
  const depths = new Map<string, number>([[selectedId, 0]]);
  const visibleEdges = new Map<string, SymbolEdge>();
  const incident = new Map<string, SymbolEdge[]>();
  for (const edge of edges) {
    incident.set(edge.from, [...(incident.get(edge.from) ?? []), edge]);
    incident.set(edge.to, [...(incident.get(edge.to) ?? []), edge]);
  }

  const queue = [selectedId];
  while (queue.length > 0 && nodes.size < MAX_NODES) {
    const id = queue.shift()!;
    const depth = depths.get(id) ?? 0;
    for (const edge of incident.get(id) ?? []) {
      visibleEdges.set(edge.id, edge);
      const other = edge.from === id ? edge.to : edge.from;
      if (nodes.has(other)) continue;
      nodes.add(other);
      depths.set(other, depth + 1);
      queue.push(other);
      if (nodes.size >= MAX_NODES) break;
    }
  }

  return { nodes, edges: [...visibleEdges.values()], depths };
}

function layoutNodes(nodes: SymbolNode[], depths: Map<string, number>): PositionedNode[] {
  const grouped = groupByDepth(nodes.map((node) => ({ ...node, depth: depths.get(node.id) ?? 0 })));
  const out: PositionedNode[] = [];
  for (const [depth, items] of grouped) {
    const sorted = [...items].sort((a, b) => a.packageName.localeCompare(b.packageName) || a.label.localeCompare(b.label));
    sorted.forEach((node, index) => {
      out.push({
        ...node,
        x: PAD + depth * COL_W,
        y: PAD + index * ROW_H,
      });
    });
  }
  return out;
}

function groupByDepth<T extends { depth: number }>(nodes: T[]): Map<number, T[]> {
  const out = new Map<number, T[]>();
  for (const node of nodes) {
    out.set(node.depth, [...(out.get(node.depth) ?? []), node]);
  }
  return new Map([...out.entries()].sort(([a], [b]) => a - b));
}

function edgePath(from: PositionedNode, to: PositionedNode, index: number): string {
  const sx = from.x + NODE_W;
  const sy = from.y + NODE_H / 2;
  const tx = to.x;
  const ty = to.y + NODE_H / 2;
  const lift = (index % 3) * 10;
  const dx = Math.max(40, Math.abs(tx - sx) * 0.42);
  return `M ${sx} ${sy} C ${sx + dx} ${sy - lift}, ${tx - dx} ${ty + lift}, ${tx} ${ty}`;
}

function edgeMid(from: PositionedNode, to: PositionedNode, index: number): { x: number; y: number } {
  return {
    x: (from.x + NODE_W + to.x) / 2,
    y: (from.y + to.y) / 2 + NODE_H / 2 - 8 - (index % 2) * 10,
  };
}

function edgeKindClass(kind: string): string {
  if (kind === 'implements') return 'implements';
  if (kind === 'calls') return 'calls';
  if (kind === 'returns') return 'returns';
  return 'uses';
}

function symbolVisibilityClass(exported?: boolean): string {
  if (exported === true) return 'symbol-public';
  if (exported === false) return 'symbol-internal';
  return 'symbol-unknown';
}
