import type { UIGraph } from '../types';
import type { ArchMotifScopeInput, RawLatentDomains } from './archMotifDomains';
import type { EventModel } from './eventModel';
import type { RawAskHit } from './ask';
import type { Interaction } from './state';

export interface LayoutPort {
  compute(graph: UIGraph, interaction: Interaction): Promise<UIGraph>;
}

export interface GraphSourcePort {
  /**
   * fresh asks the daemon to re-read the worktree (and the base worktree it
   * compares against) from disk before answering, instead of serving the
   * model its file events have kept.
   */
  load(worktree?: string, opts?: { fresh?: boolean }): Promise<UIGraph>;
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
  /**
   * Daemon readiness. Its own call rather than `call('status', …)`: the tools
   * surface renders text for an agent to read, and a poll needs fields.
   */
  status(options: { worktree?: string }): Promise<unknown>;
}

/**
 * The domains partition at one scope. Separate from LensPort because it is a
 * separate transport, not a separate tool: the lens endpoint clamps a result at
 * 256 KiB for an agent's sake and a full partition does not fit, so the canvas
 * reads its own endpoint. Readiness still comes over LensPort — its `status`
 * call is cheap and says which phase a cold daemon is in.
 */
export interface DomainsPort {
  load(scope: ArchMotifScopeInput, options: { worktree?: string }): Promise<RawLatentDomains>;
}

/**
 * The composed event model. Its own transport, like DomainsPort: the plugin
 * serves it from its own endpoint, and the answer is a whole model rather than
 * one tool's result.
 */
export interface EventModelPort {
  /**
   * `base` asks for the model marked up against merge-base(base, HEAD). The
   * answer is still the whole map — everything current plus everything the base
   * had and this branch does not — because "what changed" is a property of the
   * picture, not a smaller picture.
   */
  load(options: { worktree?: string; base?: string }): Promise<EventModel>;
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
