export type Diff = 'added' | 'removed' | 'changed';

export interface UIGraph {
  schema: string;
  pr?: PR;
  boundedContexts: BoundedContext[];
  components: Component[];
  edges: Edge[];
  comments: Comment[];
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
  kind: 'class' | 'iface';
  name: string;
  diff?: Diff;
  members: Member[];
  x?: number;
  y?: number;
  w?: number;
  h?: number;
}

export interface Member {
  id: string;
  kind: 'method' | 'prop';
  name: string;
  diff?: Diff;
}

export interface Port {
  id: string;
  side: 'left' | 'right';
  kind: 'in' | 'out';
  name: string;
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
  diff?: Diff;
}

export interface Comment {
  id: string;
  target: { type: string; id: string };
  body: string;
}
