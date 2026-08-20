import type { UIGraph } from '../types';
import type { RawAskHit } from './ask';
import type { Interaction } from './state';

export interface LayoutPort {
  compute(graph: UIGraph, interaction: Interaction): Promise<UIGraph>;
}

export interface GraphSourcePort {
  load(worktree?: string): Promise<UIGraph>;
}

export interface SearchPort {
  search(query: string, options: { k: number; worktree?: string }): Promise<{ hits: RawAskHit[]; dense: boolean }>;
}

/**
 * One call to a daemon analysis lens (an MCP tool). The payload is `unknown`
 * on purpose: a port carries the transport, and each caller owns the shape of
 * the tool it asked for.
 */
export interface LensPort {
  call(name: string, args: Record<string, unknown>, options: { worktree?: string }): Promise<unknown>;
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
