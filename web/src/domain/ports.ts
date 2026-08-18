import type { UIGraph } from '../types';
import type { Interaction } from './state';

export interface LayoutPort {
  compute(graph: UIGraph, interaction: Interaction): Promise<UIGraph>;
}

export interface GraphSourcePort {
  load(worktree?: string): Promise<UIGraph>;
}

export interface NavigationPort {
  focusWorktree(name: string): void;
}

export interface ViewportPort {
  scrollToComponent(id: string, laid: UIGraph): void;
  /**
   * Centre one component and pick a zoom that shows the whole card. Returns the
   * zoom the caller must apply (the scroll is deferred until it lands), or null
   * when the current zoom already fits and the scroll was applied directly.
   */
  focusComponent(id: string, laid: UIGraph): number | null;
  /** Returns a fit-to-screen zoom level, or null if it cannot be computed. */
  fitZoom(laid: UIGraph): number | null;
}
