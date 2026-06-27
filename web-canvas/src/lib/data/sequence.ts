"use client";

import { useEffect, useState } from 'react';

/**
 * Sequence data-source over the archai daemon, via the same-origin
 * `/api/sequence` route. Given a package path it returns the package's
 * call-sequence diagrams as Mermaid `sequenceDiagram` source, projected to the
 * type-interaction level: lifelines are types, and an edge is drawn only for a
 * cross-type call (intra-type chatter is collapsed). Entry points are the
 * package's public API, but a public-rooted flow that crosses into another
 * type via an unexported method is still drawn.
 */
export interface SequenceEntry {
  label: string;
  mermaid: string;
  hasCalls: boolean;
}

export interface SequenceResult {
  package: string;
  mode: string;
  entries: SequenceEntry[];
}

export async function resolveSequence(pkg: string, depth?: number): Promise<SequenceResult> {
  const params = new URLSearchParams({ package: pkg });
  if (depth != null) params.set('depth', String(depth));
  const res = await fetch(`/api/sequence?${params.toString()}`, {
    headers: { Accept: 'application/json' },
  });
  if (!res.ok) {
    let detail = '';
    try {
      detail = ((await res.json()) as { error?: string }).error ?? '';
    } catch {
      // ignore non-JSON error bodies
    }
    throw new Error(`sequence fetch failed (${res.status})${detail ? `: ${detail}` : ''}`);
  }
  return (await res.json()) as SequenceResult;
}

interface SequenceState {
  data: SequenceResult | null;
  error: string | null;
  loading: boolean;
}

/** Reactive sequence fetch for a package. Pass null/'' to stay idle. */
export function useSequence(pkg: string | null, depth?: number): SequenceState {
  const [state, setState] = useState<SequenceState>({ data: null, error: null, loading: false });

  useEffect(() => {
    if (!pkg) {
      setState({ data: null, error: null, loading: false });
      return;
    }
    let cancelled = false;
    setState({ data: null, error: null, loading: true });
    resolveSequence(pkg, depth)
      .then((data) => {
        if (!cancelled) setState({ data, error: null, loading: false });
      })
      .catch((err) => {
        if (!cancelled) setState({ data: null, error: String(err), loading: false });
      });
    return () => {
      cancelled = true;
    };
  }, [pkg, depth]);

  return state;
}
