"use client";

import { useEffect, useState } from 'react';

/**
 * Source-file data-source over the same-origin `/api/source` route. Returns a
 * repo file's working-tree content and, when diff is requested, its base-ref
 * content so a widget can render an inline diff.
 */
export interface SourceResult {
  path: string;
  content: string;
  baseRef: string;
  baseContent: string | null;
  hasDiff: boolean;
}

export async function resolveSource(path: string, diff = false): Promise<SourceResult> {
  const params = new URLSearchParams({ path });
  if (diff) params.set('diff', '1');
  const res = await fetch(`/api/source?${params.toString()}`, {
    headers: { Accept: 'application/json' },
  });
  if (!res.ok) {
    let detail = '';
    try {
      detail = ((await res.json()) as { error?: string }).error ?? '';
    } catch {
      // ignore non-JSON error bodies
    }
    throw new Error(`source fetch failed (${res.status})${detail ? `: ${detail}` : ''}`);
  }
  return (await res.json()) as SourceResult;
}

interface SourceState {
  data: SourceResult | null;
  error: string | null;
  loading: boolean;
}

/** Reactive source fetch. Pass null/'' to stay idle. */
export function useSource(path: string | null, diff = false): SourceState {
  const [state, setState] = useState<SourceState>({ data: null, error: null, loading: false });

  useEffect(() => {
    if (!path) {
      setState({ data: null, error: null, loading: false });
      return;
    }
    let cancelled = false;
    setState({ data: null, error: null, loading: true });
    resolveSource(path, diff)
      .then((data) => {
        if (!cancelled) setState({ data, error: null, loading: false });
      })
      .catch((err) => {
        if (!cancelled) setState({ data: null, error: String(err), loading: false });
      });
    return () => {
      cancelled = true;
    };
  }, [path, diff]);

  return state;
}
