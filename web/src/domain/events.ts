import type { UIGraph } from '../types';
import type { ChangeEntry } from './derive';
import type { LayoutPins } from './layoutPins';
import type { ReviewDefaults } from './reviewDefaults';
import type { CardDensity, Marker, ReviewChangeFilter, ReviewImpactMode } from './state';

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
  | { type: 'LevelChanged'; level: number }
  | { type: 'LeftTabChanged'; tab: 'changes' | 'tree' }
  | { type: 'LeftCollapsedToggled' }
  | { type: 'RightCollapsedToggled' }
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
  | { type: 'GroupLabelsToggled' }
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
  | { type: 'InternalWideToggled'; id: string }
  | { type: 'ComponentAllWideSet'; id: string; wide: boolean }
  // focus / navigation
  | { type: 'ComponentSelected'; id: string }
  | { type: 'FocusCleared' }
  | { type: 'CanvasCleared' }
  | { type: 'ChangeActivated'; change: ChangeEntry }
  | { type: 'TreeFocusRequested'; target: TreeFocusTarget }
  | { type: 'ScrollToComponentRequested'; id: string }
  | { type: 'MarkerActivated'; id: string }
  // comments
  | { type: 'CommentStarted'; target: { type: string; id: string }; anchor: { x: number; y: number } }
  | { type: 'CommentSubmitted'; text: string }
  | { type: 'CommentCancelled' }
  | { type: 'MarkersSeeded'; markers: Marker[] }
  // layout (internal, posted by the layout effect)
  | { type: 'LayoutComputed'; laid: UIGraph }
  | { type: 'LayoutFailed'; error: string };
