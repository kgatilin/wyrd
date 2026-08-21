import type { UIGraph } from '../types';
import type { AskHit, RawAskHit } from './ask';
import type { ChangeEntry } from './derive';
import type { LayoutPins } from './layoutPins';
import type { ReviewDefaults } from './reviewDefaults';
import type { ArchMotifScope, CardDensity, HighlightedEdge, Marker, ReviewChangeFilter, ReviewImpactMode } from './state';

/** Identifies which canvas object a context-tree row points at. Canonical home (was components/Tree). */
export interface TreeFocusTarget {
  componentId: string;
  internalId?: string;
  memberId?: string;
}

export type Event =
  // lifecycle
  | { type: 'GraphRequested'; worktree?: string; source?: 'manual' | 'auto' }
  | { type: 'GraphLoaded'; graph: UIGraph }
  | { type: 'GraphUnchanged' }
  | { type: 'GraphLoadFailed'; error: string }
  // chrome
  | { type: 'ThemeToggled' }
  | { type: 'LeftTabChanged'; tab: 'review' | 'ask' }
  | { type: 'LeftCollapsedToggled' }
  | { type: 'ArchMotifToggled' }
  | { type: 'ArchMotifCanvasOpened'; scope: ArchMotifScope }
  | { type: 'ArchMotifCanvasClosed' }
  | { type: 'ArchMotifScopeChanged'; scope: ArchMotifScope }
  | { type: 'EventCanvasToggled' }
  | { type: 'EventCanvasClosed' }
  | { type: 'EdgesHighlighted'; edges: HighlightedEdge[] }
  | { type: 'EdgesHighlightCleared' }
  | { type: 'ZoomChanged'; zoom: number }
  | { type: 'ZoomFitRequested' }
  | { type: 'ReviewViewChanged'; id: string }
  | { type: 'ReviewScopeChanged'; id: string }
  | { type: 'ReviewGroupingChanged'; id: string }
  | { type: 'ReviewImpactModeChanged'; mode: ReviewImpactMode }
  | { type: 'ReviewChangeFilterChanged'; filter: ReviewChangeFilter }
  | { type: 'UnchangedNeighborsToggled' }
  | { type: 'ChangedDetailsOnlyToggled' }
  | { type: 'ReviewDefaultsLoaded'; key: string; defaults: ReviewDefaults }
  | { type: 'CardDensityChanged'; density: CardDensity }
  | { type: 'InlineSignaturesToggled' }
  | { type: 'WorktreeChanged'; name: string }
  | { type: 'LayoutPinsLoaded'; scopeKey: string; pins: LayoutPins }
  | { type: 'ComponentLayoutPinned'; id: string; x: number; y: number }
  | { type: 'ComponentsLayoutPinned'; pins: LayoutPins }
  | { type: 'LayoutPinReset'; id: string }
  | { type: 'LayoutGroupPinsReset'; componentIds: string[] }
  | { type: 'LayoutPinsReset' }
  | { type: 'LayoutRepoPinsReset' }
  // expansion
  | { type: 'ComponentToggled'; id: string }
  | { type: 'ComponentSeqToggled'; id: string }
  | { type: 'ComponentsExpandedAll' }
  | { type: 'ComponentsCollapsedAll' }
  // focus / navigation
  | { type: 'ComponentSelected'; id: string }
  | { type: 'FocusCleared' }
  | { type: 'CanvasCleared' }
  | { type: 'ChangeActivated'; change: ChangeEntry }
  | { type: 'TreeFocusRequested'; target: TreeFocusTarget }
  | { type: 'ScrollToComponentRequested'; id: string }
  | { type: 'MarkerActivated'; id: string }
  // ask
  | { type: 'AskSubmitted'; query: string }
  | { type: 'AskResultsLoaded'; query: string; hits: RawAskHit[]; dense: boolean }
  | { type: 'AskFailed'; query: string; error: string }
  | { type: 'AskCleared' }
  | { type: 'AskDetailOnlyToggled' }
  | { type: 'AskDepthChanged'; k: number }
  | { type: 'AskHitActivated'; hit: AskHit }
  // comments
  | { type: 'CommentStarted'; target: { type: string; id: string }; anchor: { x: number; y: number } }
  | { type: 'CommentSubmitted'; text: string }
  | { type: 'CommentCancelled' }
  | { type: 'MarkersSeeded'; markers: Marker[] }
  // layout (internal, posted by the layout effect)
  | { type: 'LayoutComputed'; laid: UIGraph }
  | { type: 'LayoutFailed'; error: string };
