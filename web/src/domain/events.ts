import type { UIGraph } from '../types';
import type { ChangeEntry } from './derive';
import type { Marker } from './state';

/** Identifies which canvas object a context-tree row points at. Canonical home (was components/Tree). */
export interface TreeFocusTarget {
  componentId: string;
  internalId?: string;
  memberId?: string;
}

export type Event =
  // lifecycle
  | { type: 'GraphRequested' }
  | { type: 'GraphLoaded'; graph: UIGraph }
  | { type: 'GraphLoadFailed'; error: string }
  // chrome
  | { type: 'ThemeToggled' }
  | { type: 'LevelChanged'; level: number }
  | { type: 'LeftTabChanged'; tab: 'changes' | 'tree' }
  | { type: 'LeftCollapsedToggled' }
  | { type: 'RightCollapsedToggled' }
  | { type: 'ZoomChanged'; zoom: number }
  | { type: 'ZoomFitRequested' }
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
