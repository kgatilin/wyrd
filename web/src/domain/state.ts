import type { Diff, UIGraph } from '../types';
import type { LayoutPins } from './layoutPins';
import type { ReviewDefaults } from './reviewDefaults';

export type ReviewImpactMode = 'changed_neighbors' | 'changed_only' | 'containing_group' | 'review_view' | 'repository';
export type ReviewChangeFilter = 'all' | Diff | 'dependency' | 'policy';
export type CardDensity = 'detailed' | 'compact';

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
  /**
   * Expanded components showing their call-sequence instead of internals.
   * A seq-mode card gets a fixed frame from layout (its diagram scrolls
   * inside), so the surrounding graph stays stable.
   */
  seqMode: ReadonlySet<string>;
  cardDensity: CardDensity;
  showInlineSignatures: boolean;
}

export interface AppUI {
  theme: 'dark' | 'light';
  focusId: string | null;
  expanded: ReadonlySet<string>;
  /** Expanded components currently flipped to their call-sequence view. */
  seqMode: ReadonlySet<string>;
  leftTab: 'changes' | 'tree';
  leftCollapsed: boolean;
  /** ArchMotif metrics panel is open as an overlay over the canvas. */
  archMotifOpen: boolean;
  activeChangeId: string | null;
  activeMarkerId: string | null;
  zoom: number;
  reviewViewId: string | null;
  reviewScopeId: string | null;
  reviewGroupingId: string | null;
  reviewImpactMode: ReviewImpactMode;
  reviewChangeFilter: ReviewChangeFilter;
  hideUnchangedNeighbors: boolean;
  changedDetailsOnly: boolean;
  reviewDefaultsKey: string | null;
  reviewDefaults: ReviewDefaults;
  cardDensity: CardDensity;
  showInlineSignatures: boolean;
  layoutPinScopeKey: string | null;
  layoutPins: LayoutPins;
}

export interface AppState {
  graph: UIGraph | null;
  ui: AppUI;
  markers: Marker[];
  pendingComment: PendingComment | null;
  geometry: { laid: UIGraph | null; status: 'idle' | 'ready' | 'error'; error: string | null };
  load: {
    status: 'loading' | 'ready' | 'error';
    error: string | null;
    /**
     * The worktree a switch is waiting on, or null/absent when the load in
     * flight is a plain (re)load of the one already shown. A switch replaces
     * the whole graph, so the UI covers it with the loading screen instead of
     * leaving a stale canvas on screen; a refresh does not.
     */
    pendingWorktree?: string | null;
  };
}

export const initialState: AppState = {
  graph: null,
  ui: {
    theme: 'dark',
    focusId: null,
    expanded: new Set(),
    seqMode: new Set(),
    leftTab: 'tree',
    leftCollapsed: false,
    archMotifOpen: false,
    activeChangeId: null,
    activeMarkerId: null,
    zoom: 1,
    reviewViewId: null,
    reviewScopeId: null,
    reviewGroupingId: null,
    reviewImpactMode: 'changed_only',
    reviewChangeFilter: 'all',
    hideUnchangedNeighbors: false,
    changedDetailsOnly: true,
    reviewDefaultsKey: null,
    reviewDefaults: {},
    cardDensity: 'detailed',
    showInlineSignatures: true,
    layoutPinScopeKey: null,
    layoutPins: {},
  },
  markers: [],
  pendingComment: null,
  geometry: { laid: null, status: 'idle', error: null },
  load: { status: 'loading', error: null },
};
