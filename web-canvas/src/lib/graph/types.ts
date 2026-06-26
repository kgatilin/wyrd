/**
 * Graph types for the architecture diagram renderer.
 * Ported from web/src/types.ts, stripped of review-only types.
 */

export type Diff = 'added' | 'removed' | 'changed';

export interface UIGraph {
  schema: string;
  boundedContexts: BoundedContext[];
  components: Component[];
  edges: Edge[];
  relations?: SymbolRelation[];
}

export interface BoundedContext {
  id: string;
  name: string;
  x?: number;
  y?: number;
  w?: number;
  h?: number;
}

export interface Component {
  id: string;
  name: string;
  tech: string;
  desc: string;
  bc: string;
  diff?: Diff;
  internals: Internal[];
  ports: Port[];
  x?: number;
  y?: number;
  w?: number;
  h?: number;
  wx?: number;
  hx?: number;
}

export interface Internal {
  id: string;
  kind: 'class' | 'iface' | 'func' | 'type' | 'const' | 'var' | 'error';
  name: string;
  sourceFile?: string;
  exported?: boolean;
  diff?: Diff;
  diffBefore?: string;
  diffAfter?: string;
  members: Member[];
  x?: number;
  y?: number;
  w?: number;
  h?: number;
}

export interface Member {
  id: string;
  kind: 'method' | 'prop' | 'const';
  name: string;
  sourceFile?: string;
  exported?: boolean;
  diff?: Diff;
  diffBefore?: string;
  diffAfter?: string;
}

export interface Port {
  id: string;
  side: 'left' | 'right';
  kind: 'in' | 'out';
  name: string;
  public?: boolean;
  diff?: Diff;
  y?: number;
}

export interface Edge {
  id: string;
  from: string;
  to: string;
  fromPort: string;
  toPort: string;
  label: string;
  public?: boolean;
  diff?: Diff;
  points?: { x: number; y: number }[];
}

export interface SymbolRelation {
  id: string;
  kind: 'uses' | 'returns' | 'implements' | 'extends' | 'nested-in' | string;
  fromComponentId: string;
  fromInternalId?: string;
  fromMemberId?: string;
  fromLabel?: string;
  toComponentId: string;
  toInternalId?: string;
  toMemberId?: string;
  toLabel?: string;
  public?: boolean;
  diff?: Diff;
}

/** Local interaction state for the graph renderer */
export type CardDensity = 'detailed' | 'compact';

export interface GraphInteraction {
  expanded: Set<string>;
  internalExpanded: Set<string>;
  internalWide: Set<string>;
  cardDensity: CardDensity;
  showInlineSignatures: boolean;
}
