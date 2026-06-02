import type { UIGraph } from '../types';

/** Comment marker placed on the canvas. Canonical home (was components/PinnedMarker). */
export interface Marker {
  id: string;
  n: number;
  x: number;
  y: number;
  target: { type: string; id: string };
  body: string;
  author: string;
  when: string;
}

/** A comment being authored. Canonical home (was components/InlinePopover). */
export interface PendingComment {
  x: number;
  y: number;
  target: { type: string; id: string };
}

/** The expansion inputs the layout engine needs. */
export interface Interaction {
  expanded: ReadonlySet<string>;
  internalExpanded: ReadonlySet<string>;
  internalWide: ReadonlySet<string>;
}

export interface AppUI {
  level: number;
  theme: 'dark' | 'light';
  focusId: string | null;
  expanded: ReadonlySet<string>;
  internalExpanded: ReadonlySet<string>;
  internalWide: ReadonlySet<string>;
  leftTab: 'changes' | 'tree';
  leftCollapsed: boolean;
  rightCollapsed: boolean;
  activeChangeId: string | null;
  activeMarkerId: string | null;
  zoom: number;
}

export interface AppState {
  graph: UIGraph | null;
  ui: AppUI;
  markers: Marker[];
  pendingComment: PendingComment | null;
  geometry: { laid: UIGraph | null; status: 'idle' | 'ready' | 'error'; error: string | null };
  load: { status: 'loading' | 'ready' | 'error'; error: string | null };
}

export const initialState: AppState = {
  graph: null,
  ui: {
    level: 2,
    theme: 'dark',
    focusId: null,
    expanded: new Set(),
    internalExpanded: new Set(),
    internalWide: new Set(),
    leftTab: 'tree',
    leftCollapsed: false,
    rightCollapsed: false,
    activeChangeId: null,
    activeMarkerId: null,
    zoom: 1,
  },
  markers: [],
  pendingComment: null,
  geometry: { laid: null, status: 'idle', error: null },
  load: { status: 'loading', error: null },
};
