import type { UIGraph } from '../types';

export interface ReviewDefaults {
  reviewViewId?: string;
  scopeByView?: Record<string, string>;
  groupingByView?: Record<string, string>;
}

const STORAGE_PREFIX = 'archai:review-defaults:v1';

export function buildReviewDefaultsKey(graph: UIGraph): string {
  return [
    STORAGE_PREFIX,
    keyPart(graph.repo?.root ?? graph.schema ?? 'graph'),
  ].join(':');
}

export function loadReviewDefaults(
  key: string,
  graph: UIGraph,
  storage: Storage | null = browserStorage()
): ReviewDefaults {
  if (!storage) return {};
  try {
    return normalizeReviewDefaults(JSON.parse(storage.getItem(key) ?? '{}'), graph);
  } catch {
    return {};
  }
}

export function saveReviewDefaults(
  key: string,
  defaults: ReviewDefaults,
  graph: UIGraph,
  storage: Storage | null = browserStorage()
): void {
  if (!storage) return;
  const normalized = normalizeReviewDefaults(defaults, graph);
  if (isEmptyDefaults(normalized)) {
    storage.removeItem(key);
    return;
  }
  storage.setItem(key, JSON.stringify(normalized));
}

export function normalizeReviewDefaults(value: unknown, graph: UIGraph): ReviewDefaults {
  if (!value || typeof value !== 'object') return {};
  const raw = value as ReviewDefaults;
  const out: ReviewDefaults = {};

  if (isKnownReviewView(graph, raw.reviewViewId)) {
    out.reviewViewId = raw.reviewViewId;
  }

  const scopeByView = normalizeChoiceMap(raw.scopeByView, (viewId, scopeId) =>
    isKnownReviewView(graph, viewId) && isKnownReviewScope(graph, scopeId)
  );
  if (Object.keys(scopeByView).length > 0) out.scopeByView = scopeByView;

  const groupingByView = normalizeChoiceMap(raw.groupingByView, (viewId, groupingId) =>
    isKnownReviewView(graph, viewId) && isKnownReviewGrouping(graph, groupingId)
  );
  if (Object.keys(groupingByView).length > 0) out.groupingByView = groupingByView;

  return out;
}

export function defaultsWithReviewView(defaults: ReviewDefaults, viewId: string): ReviewDefaults {
  return {
    ...defaults,
    reviewViewId: viewId,
  };
}

export function defaultsWithScope(defaults: ReviewDefaults, viewId: string | null, scopeId: string): ReviewDefaults {
  if (!viewId) return defaults;
  return {
    ...defaults,
    scopeByView: {
      ...(defaults.scopeByView ?? {}),
      [viewId]: scopeId,
    },
  };
}

export function defaultsWithGrouping(defaults: ReviewDefaults, viewId: string | null, groupingId: string): ReviewDefaults {
  if (!viewId) return defaults;
  return {
    ...defaults,
    groupingByView: {
      ...(defaults.groupingByView ?? {}),
      [viewId]: groupingId,
    },
  };
}

function normalizeChoiceMap(
  value: unknown,
  isValid: (viewId: string, choiceId: string) => boolean
): Record<string, string> {
  if (!value || typeof value !== 'object') return {};
  const out: Record<string, string> = {};
  for (const [viewId, choiceId] of Object.entries(value as Record<string, unknown>)) {
    if (typeof choiceId !== 'string') continue;
    if (!isValid(viewId, choiceId)) continue;
    out[viewId] = choiceId;
  }
  return out;
}

function isKnownReviewView(graph: UIGraph, id: string | null | undefined): id is string {
  return !!id && !!graph.reviewViews?.some((view) => view.id === id);
}

function isKnownReviewScope(graph: UIGraph, id: string | null | undefined): id is string {
  return !!id && !!graph.reviewScopes?.some((scope) => scope.id === id);
}

function isKnownReviewGrouping(graph: UIGraph, id: string | null | undefined): id is string {
  return !!id && !!graph.reviewGroupings?.some((grouping) => grouping.id === id);
}

function isEmptyDefaults(defaults: ReviewDefaults): boolean {
  return (
    !defaults.reviewViewId &&
    Object.keys(defaults.scopeByView ?? {}).length === 0 &&
    Object.keys(defaults.groupingByView ?? {}).length === 0
  );
}

function keyPart(value: string): string {
  return encodeURIComponent(value.trim() || 'default');
}

function browserStorage(): Storage | null {
  if (typeof window === 'undefined') return null;
  return window.localStorage;
}
