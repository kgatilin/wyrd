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
  /** Returns a fit-to-screen zoom level, or null if it cannot be computed. */
  fitZoom(laid: UIGraph): number | null;
}
