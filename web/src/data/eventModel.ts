import type { EventModel } from '../domain/eventModel';

/**
 * The composed event model, from the events plugin's own endpoint.
 *
 * The daemon reads the declarations fresh on every request — a repo has a
 * handful of them and parsing YAML is fast — so there is no cache to invalidate
 * here either. The canvas re-reads on the model-changed SSE like everything
 * else on the page.
 */
export async function fetchEventModel(options: { worktree?: string } = {}): Promise<EventModel> {
  const res = await fetch(`${apiPrefix(options.worktree)}/api/plugins/events/model`);
  if (!res.ok) {
    throw new Error(await errorText(res));
  }
  const body = (await res.json()) as Partial<EventModel>;
  // A daemon built before this endpoint learned its shape, or a plugin that
  // failed to load, must not crash the canvas on a missing array.
  return {
    components: body.components ?? [],
    flows: body.flows ?? [],
    kinds: body.kinds ?? [],
  };
}

function apiPrefix(worktree?: string): string {
  if (worktree) return `/w/${encodeURIComponent(worktree)}`;
  return currentWorktreePrefix() ?? '';
}

function currentWorktreePrefix(): string | null {
  if (typeof window === 'undefined') return null;
  const match = window.location.pathname.match(/^\/w\/([^/]+)(?:\/|$)/);
  if (!match) return null;
  return `/w/${match[1]}`;
}

async function errorText(res: Response): Promise<string> {
  try {
    const body = (await res.text()).trim();
    if (body) return body;
  } catch {
    // fall through to the status line
  }
  return `event model failed (${res.status})`;
}
