export type Diff = 'added' | 'removed' | 'changed';

export interface UIGraph {
  schema: string;
  repo?: Repo;
  worktrees?: Worktree[];
  pr?: PR;
  reviewScopes?: ReviewScope[];
  reviewViews?: ReviewView[];
  reviewGroupings?: ReviewGrouping[];
  defaultReviewView?: string;
  defaultReviewScope?: string;
  defaultGrouping?: string;
  policyViolations?: PolicyViolation[];
  boundedContexts: BoundedContext[];
  components: Component[];
  edges: Edge[];
  relations?: SymbolRelation[];
  comments: Comment[];
}

export interface Repo {
  root?: string;
  activeWorktree?: string;
  baseRef?: string;
  baseWorktree?: string;
  compare?: string;
}

export interface Worktree {
  name: string;
  branch?: string;
  head?: string;
  current?: boolean;
  base?: boolean;
}

export interface PR {
  title: string;
  branch: string;
  agent: string;
  summary: string;
  stats: Stats;
}

export interface Stats {
  added: number;
  removed: number;
  changed: number;
  comments: number;
}

export interface BoundedContext {
  id: string;
  name: string;
  x?: number;
  y?: number;
  w?: number;
  h?: number;
}

export interface ReviewScope {
  id: string;
  title: string;
}

export interface ReviewView {
  id: string;
  title: string;
  defaultScope: string;
  defaultExpansion?: 'auto' | 'changed' | 'expanded' | 'collapsed';
  groupBy?: string;
  componentIds: string[];
  componentCount: number;
}

export interface ReviewGrouping {
  id: string;
  title: string;
  groups: ReviewGroup[];
}

export interface ReviewGroup {
  id: string;
  title: string;
  componentIds: string[];
  componentCount: number;
}

export interface PolicyViolation {
  id: string;
  kind: 'layer_rule' | string;
  sourceComponentId: string;
  targetComponentId: string;
  sourceLayer?: string;
  targetLayer?: string;
  message: string;
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
  points?: { x: number; y: number }[]; // ELK-routed polyline: start + bend points + end
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

export interface Comment {
  id: string;
  target: { type: string; id: string };
  body: string;
}
