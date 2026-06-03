import { useState, useMemo, useEffect, useCallback } from 'react';
import type { UIGraph, Component } from '../types';

/**
 * Hook for managing expanded state of components.
 * Returns a Set of expanded component IDs, a toggle function, and a setter.
 */
export function useExpanded(initial: string[] = []): [
  Set<string>,
  (id: string) => void,
  React.Dispatch<React.SetStateAction<Set<string>>>
] {
  const [set, setSet] = useState<Set<string>>(() => new Set(initial));

  const toggle = useCallback((id: string) => {
    setSet((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  return [set, toggle, setSet];
}

/**
 * Hook for focus mode - tracks focused component ID and related IDs.
 * Related = the focused component + its edge neighbors.
 */
export function useFocus(graph: UIGraph): [
  string | null,
  (id: string | null) => void,
  Set<string> | null
] {
  const [focusId, setFocusId] = useState<string | null>(null);

  const related = useMemo(() => {
    if (!focusId) return null;
    const r = new Set<string>([focusId]);
    for (const edge of graph.edges) {
      if (edge.from === focusId) r.add(edge.to);
      if (edge.to === focusId) r.add(edge.from);
    }
    return r;
  }, [focusId, graph.edges]);

  return [focusId, setFocusId, related];
}

/**
 * Hook combining component expansion and internal expansion.
 * When a component expands, all its internals auto-expand.
 */
export function useExpansion(graph: UIGraph, initialExpanded: string[] = []): {
  expanded: Set<string>;
  toggle: (id: string) => void;
  internalExpanded: Set<string>;
  toggleInternal: (id: string) => void;
  internalWide: Set<string>;
  toggleInternalWide: (id: string) => void;
  setComponentWide: (componentId: string, wide: boolean) => void;
} {
  const [expanded, toggle] = useExpanded(initialExpanded);
  const [internalExpanded, toggleInternal, setInternalExpanded] = useExpanded([]);
  // Internals in "fit-width" mode — stretched so all member text is visible.
  // Membership-based state: to make fit-width the default for every component
  // in the future, seed this with all internal ids (e.g. useExpanded(allIds)).
  const [internalWide, toggleInternalWide, setInternalWide] = useExpanded([]);

  // Bulk-toggle every internal of one component into (or out of) fit-width mode.
  // Backs the component header's "expand all" button.
  const setComponentWide = useCallback(
    (componentId: string, wide: boolean) => {
      const comp = graph.components.find((c) => c.id === componentId);
      if (!comp) return;
      setInternalWide((prev) => {
        const next = new Set(prev);
        for (const internal of comp.internals) {
          if (wide) next.add(internal.id);
          else next.delete(internal.id);
        }
        return next;
      });
    },
    [graph.components, setInternalWide]
  );

  // Auto-expand internals when component expands
  useEffect(() => {
    setInternalExpanded((prev) => {
      const next = new Set(prev);
      for (const cid of expanded) {
        const comp = graph.components.find((c) => c.id === cid);
        if (comp) {
          for (const internal of comp.internals) {
            next.add(internal.id);
          }
        }
      }
      return next;
    });
  }, [expanded, graph.components, setInternalExpanded]);

  return {
    expanded,
    toggle,
    internalExpanded,
    toggleInternal,
    internalWide,
    toggleInternalWide,
    setComponentWide,
  };
}

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
