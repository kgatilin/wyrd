import type { Diff, UIGraph } from '../types';
import type { AskHit } from './ask';
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

/**
 * Which node set the domains canvas puts through `latent_domains`. `diff` is
 * the change region the branch pulls on, `repo` the whole repository, and
 * `package` one package — the entry the panel's god-package row uses.
 */
export type ArchMotifScopeKind = 'diff' | 'repo' | 'package';

export interface ArchMotifScope {
  kind: ArchMotifScopeKind;
  package?: string;
}

/**
 * The domains canvas: structural clusters × semantic clusters as a grid. Only
 * what is being asked for lives here — the answer itself is fetched by the
 * canvas, like the file diff, because it is a view over the review rather
 * than part of the review's state machine.
 */
export interface ArchMotifCanvasState {
  open: boolean;
  scope: ArchMotifScope;
}

export interface AppUI {
  theme: 'dark' | 'light';
  focusId: string | null;
  expanded: ReadonlySet<string>;
  /** Expanded components currently flipped to their call-sequence view. */
  seqMode: ReadonlySet<string>;
  leftTab: 'review' | 'ask';
  leftCollapsed: boolean;
  /** ArchMotif metrics panel is open as an overlay over the canvas. */
  archMotifOpen: boolean;
  /** ArchMotif domains canvas — replaces the review canvas while open. */
  archMotifCanvas: ArchMotifCanvasState;
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

/**
 * A semantic question asked of the indexed code. The answer is a ranked hit
 * list; the canvas is projected down to the packages those hits live in, so
 * "where is X handled" is answered as architecture, not as a list of files.
 */
export interface AskState {
  /** The submitted query. Empty means no ask is active and the review shows. */
  query: string;
  status: 'idle' | 'loading' | 'ready' | 'error';
  error: string | null;
  hits: AskHit[];
  /**
   * The vector layer contributed to this ranking. False means the answer is
   * BM25-only — worth saying, because that is a recall difference, not a
   * failure.
   */
  dense: boolean;
  /**
   * How many hits to ask for. A count, never a score cutoff: the daemon's
   * scores are fused RRF ranks, which carry no absolute relevance.
   */
  k: number;
  /** Cards show only the matched symbols; false shows the whole package. */
  detailOnly: boolean;
  activeHitId: string | null;
  /**
   * The card expansion the review had before the first ask. An answer expands
   * the packages it matched; clearing it puts the reviewer back where they
   * were instead of on a collapsed canvas.
   */
  expandedBefore: ReadonlySet<string> | null;
}

export const initialAsk: AskState = {
  query: '',
  status: 'idle',
  error: null,
  hits: [],
  dense: false,
  k: 20,
  detailOnly: true,
  activeHitId: null,
  expandedBefore: null,
};

export interface AppState {
  graph: UIGraph | null;
  ui: AppUI;
  ask: AskState;
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
    leftTab: 'review',
    leftCollapsed: false,
    archMotifOpen: false,
    archMotifCanvas: { open: false, scope: { kind: 'diff' } },
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
  ask: initialAsk,
  markers: [],
  pendingComment: null,
  geometry: { laid: null, status: 'idle', error: null },
  load: { status: 'loading', error: null },
};
