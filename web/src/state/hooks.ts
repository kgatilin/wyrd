import type { Component } from '../types';

/**
 * Compute expanded height for a component based on expanded internals.
 */
export function computeExpandedHeight(
  cmp: Component,
  expandedInternals: ReadonlySet<string>
): number {
  let extra = 0;
  for (const internal of cmp.internals) {
    if (expandedInternals.has(internal.id)) {
      extra = Math.max(extra, (internal.members?.length || 0) * 18);
    }
  }
  return (cmp.hx ?? 180) + extra;
}
