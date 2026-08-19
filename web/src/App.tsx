import { useEffect, useLayoutEffect, useState, useMemo, useRef } from 'react';
import type { UIGraph, Component as ComponentType, ReviewGrouping } from './types';
import { createAppStore } from './runtime/createAppStore';
import { StoreProvider, useStore, useDispatch, useStoreApi } from './runtime/react';
import type { DomViewport } from './adapters/domViewport';
import { askProjectionOf, groupAskHits } from './domain/ask';
import { relatedIds, deriveChanges, deriveChangeStats, selectReviewGraph, type ReviewSelectionOptions } from './domain/derive';
import {
  applyLayoutPins,
  buildLayoutScopeKey,
  clearLayoutPinsForRepo,
  loadLayoutPins,
  saveLayoutPins,
  type LayoutPins,
} from './domain/layoutPins';
import {
  buildReviewDefaultsKey,
  loadReviewDefaults,
  saveReviewDefaults,
} from './domain/reviewDefaults';
import { AppBar } from './components/AppBar';
import { PrHeader } from './components/PrHeader';
import { BCGroups } from './components/BCGroups';
import { Component } from './components/Component';
import { EdgeLayer } from './components/EdgeLayer';
import { RelationLayer } from './components/RelationLayer';
import { isDiagramRelation } from './domain/cardModel';
import { Legend } from './components/Legend';
import { CanvasToolbar } from './components/CanvasToolbar';
import { Tree, TreeFocusTarget } from './components/Tree';
import { SourceDrawer, type SaveSourceResult, type SourceDrawerState } from './components/SourceDrawer';
import { ArchMotifPanel } from './components/ArchMotifPanel';
import { AskPanel } from './components/AskPanel';
import { DiffOverlay, useDiffSession } from './components/DiffOverlay';
import { SymbolGraphOverlay } from './components/SymbolGraphOverlay';
import { PAN_MARGIN, ZOOM_MIN, ZOOM_MAX, ZOOM_STEP } from './view/viewportConstants';
import { PinnedMarker } from './components/PinnedMarker';
import type { SymbolFocusTarget } from './domain/symbolFocus';

/**
 * Embed mode (?embed=1): render just the review canvas + reviewbar — no
 * AppBar, PR header, or side panels — for framing the graph into другие
 * shells (e.g. Atrium's worktree detail iframes /w/<name>/review/?embed=1).
 * Read once at module load: the param can't change without a navigation.
 */
const EMBED =
  typeof window !== 'undefined' && new URLSearchParams(window.location.search).has('embed');

/**
 * Main application component - V4 layout shell.
 * Owns the store (composition root) and bootstraps the graph load; the store
 * now drives data/layout/semantic state. AppContent renders once a graph exists.
 */
export default function App() {
  const [{ store, viewport }] = useState(() => createAppStore());
  useEffect(() => {
    store.dispatch({ type: 'GraphRequested' });
  }, [store]);
  return (
    <StoreProvider store={store}>
      <AppRoot viewport={viewport} />
    </StoreProvider>
  );
}

function AppRoot({ viewport }: { viewport: DomViewport }) {
  const theme = useStore((s) => s.ui.theme);
  const load = useStore((s) => s.load);
  const graph = useStore((s) => s.graph);

  // Switching worktrees re-parses a whole other checkout in the daemon,
  // which takes seconds. Cover it with the same screen the cold start uses:
  // the previous worktree's canvas left on screen looks like a hung app.
  const pending = load.pendingWorktree ?? null;

  if (load.status === 'error' && (!graph || pending)) {
    return (
      <LoadingScreen
        theme={theme}
        worktree={pending}
        error={load.error ?? 'Failed to load architecture graph.'}
      />
    );
  }
  if (!graph || pending) {
    return <LoadingScreen theme={theme} worktree={pending} />;
  }
  return <AppContent graph={graph} viewport={viewport} />;
}

function LoadingScreen({
  theme,
  error,
  worktree,
}: {
  theme: 'dark' | 'light';
  error?: string;
  worktree?: string | null;
}) {
  const title = error
    ? worktree
      ? `Worktree ${worktree} failed to load`
      : 'Architecture graph failed to load'
    : worktree
      ? `Reading ${worktree}`
      : 'Reading architecture graph';
  const subtitle =
    error ?? (worktree
      ? 'Parsing this worktree\u2019s packages in the daemon...'
      : 'Parsing source packages and preparing the review canvas...');
  return (
    <div className={`hifi v4 theme-${theme} hf-load-screen`}>
      <div className={`hf-load-card ${error ? 'error' : ''}`}>
        {!error && <div className="hf-load-spinner" aria-hidden="true" />}
        <div className="hf-load-title">{title}</div>
        <div className="hf-load-subtitle">{subtitle}</div>
      </div>
    </div>
  );
}

/**
 * Inner component that renders the app once graph is loaded.
 * Separated to ensure hooks can reference graph.
 *
 * Data-flow:
 *   graph (raw, from App) ──► semantic consumers (expansion, focus, diff, tree, changes)
 *                         └──► layout effect ──► laid (UIGraph with ELK geometry)
 *                                                  └──► geometry consumers (BCGroups, EdgeLayer,
 *                                                       Component map, canvasDimensions)
 */
function AppContent({ graph, viewport }: { graph: UIGraph; viewport: DomViewport }) {
  // Store-owned data/layout/semantic state (Plan 2a/2c). The viewport (zoom/pan/
  // scroll) remains a local island.
  const dispatch = useDispatch();
  const storeApi = useStoreApi();
  const zoom = useStore((s) => s.ui.zoom);
  const theme = useStore((s) => s.ui.theme);
  const load = useStore((s) => s.load);
  const expanded = useStore((s) => s.ui.expanded);
  const seqMode = useStore((s) => s.ui.seqMode);
  const focusId = useStore((s) => s.ui.focusId);
  const leftCollapsed = useStore((s) => s.ui.leftCollapsed);
  const leftTab = useStore((s) => s.ui.leftTab);
  const ask = useStore((s) => s.ask);
  const archMotifOpen = useStore((s) => s.ui.archMotifOpen);
  const markers = useStore((s) => s.markers);
  const activeMarkerId = useStore((s) => s.ui.activeMarkerId);
  const reviewViewId = useStore((s) => s.ui.reviewViewId);
  const reviewScopeId = useStore((s) => s.ui.reviewScopeId);
  const reviewGroupingId = useStore((s) => s.ui.reviewGroupingId);
  const reviewImpactMode = useStore((s) => s.ui.reviewImpactMode);
  const reviewChangeFilter = useStore((s) => s.ui.reviewChangeFilter);
  const hideUnchangedNeighbors = useStore((s) => s.ui.hideUnchangedNeighbors);
  const changedDetailsOnly = useStore((s) => s.ui.changedDetailsOnly);
  const reviewDefaultsKey = useStore((s) => s.ui.reviewDefaultsKey);
  const reviewDefaults = useStore((s) => s.ui.reviewDefaults);
  const cardDensity = useStore((s) => s.ui.cardDensity);
  const showInlineSignatures = useStore((s) => s.ui.showInlineSignatures);
  const layoutPins = useStore((s) => s.ui.layoutPins);
  const layoutPinScopeKey = useStore((s) => s.ui.layoutPinScopeKey);
  const laid = useStore((s) => s.geometry.laid);
  const layoutError = useStore((s) => (s.geometry.status === 'error' ? s.geometry.error : null));
  // An answered question replaces the review's package selection; while one is
  // in flight (or matched nothing drawable) the review stays on screen.
  const askProjection = useMemo(() => askProjectionOf(ask), [ask]);
  const askGroups = useMemo(() => groupAskHits(ask.hits), [ask.hits]);
  const displayGraph = useMemo(
    () => selectReviewGraph(graph, reviewViewId, reviewScopeId, reviewGroupingId, {
      impactMode: reviewImpactMode,
      changeFilter: reviewChangeFilter,
      hideUnchangedNeighbors,
      changedDetailsOnly,
      focusedPackageId: focusId,
      ask: askProjection,
    }),
    [graph, reviewViewId, reviewScopeId, reviewGroupingId, reviewImpactMode, reviewChangeFilter, hideUnchangedNeighbors, changedDetailsOnly, focusId, askProjection]
  );
  const groupingOptions = useMemo(
    () => visibleReviewGroupings(graph, reviewViewId, reviewScopeId, reviewGroupingId, {
      impactMode: reviewImpactMode,
      changeFilter: reviewChangeFilter,
      hideUnchangedNeighbors,
      changedDetailsOnly,
    }),
    [graph, reviewViewId, reviewScopeId, reviewGroupingId, reviewImpactMode, reviewChangeFilter, hideUnchangedNeighbors, changedDetailsOnly]
  );
  const desiredLayoutPinScopeKey = useMemo(
    () => buildLayoutScopeKey(graph, reviewViewId, reviewScopeId, reviewGroupingId),
    [graph, reviewViewId, reviewScopeId, reviewGroupingId]
  );
  const desiredReviewDefaultsKey = useMemo(
    () => buildReviewDefaultsKey(graph),
    [graph]
  );
  const activeLayoutPins = useMemo(
    () => (layoutPinScopeKey === desiredLayoutPinScopeKey ? layoutPins : {}),
    [layoutPinScopeKey, desiredLayoutPinScopeKey, layoutPins]
  );
  const displayLaid = useMemo(
    () => (laid ? applyLayoutPins(laid, activeLayoutPins) : null),
    [laid, activeLayoutPins]
  );
  const [sourceViewer, setSourceViewer] = useState<SourceDrawerState | null>(null);
  // Review knobs are collapsed by default: opening a review should just show
  // the review, not a bar of selectors.
  const [viewOptionsOpen, setViewOptionsOpen] = useState(false);
  const [symbolFocus, setSymbolFocus] = useState<SymbolFocusTarget | null>(null);
  // The file diff is a viewer over the same review, opened on demand — like
  // the source drawer, it is not part of the canvas state machine.
  const [diffOpen, setDiffOpen] = useState(false);
  const sourceRequestSeq = useRef(0);
  const pinnedCount = Object.keys(activeLayoutPins).length;
  const pinnedGroupIds = useMemo(() => {
    const ids = new Set<string>();
    if (!displayLaid) return ids;
    for (const component of displayLaid.components) {
      if (activeLayoutPins[component.id]) ids.add(component.bc);
    }
    return ids;
  }, [displayLaid, activeLayoutPins]);
  const activeWorktree =
    graph.repo?.activeWorktree ??
    graph.worktrees?.find((worktree) => worktree.current)?.name ??
    '';
  const reviewBaseRef = graph.repo?.baseRef ?? 'main';
  // Held here, not in the overlay: the diff survives closing it, so opening
  // it again costs nothing and lands back on the file being read.
  const diffSession = useDiffSession(activeWorktree, reviewBaseRef, diffOpen);
  const reloadDiff = diffSession.reload;
  const related = useMemo(() => relatedIds(displayGraph, focusId), [displayGraph, focusId]);

  // Whether the collapsed "View" popover has anything to offer at all.
  const hasViewOptions =
    (graph.reviewViews?.length ?? 0) > 0 ||
    (graph.reviewScopes?.length ?? 0) > 0 ||
    groupingOptions.length > 1 ||
    graph.pr != null;

  useEffect(() => {
    dispatch({
      type: 'ReviewDefaultsLoaded',
      key: desiredReviewDefaultsKey,
      defaults: loadReviewDefaults(desiredReviewDefaultsKey, graph),
    });
  }, [dispatch, desiredReviewDefaultsKey, graph]);

  useEffect(() => {
    if (reviewDefaultsKey !== desiredReviewDefaultsKey) return;
    saveReviewDefaults(reviewDefaultsKey, reviewDefaults, graph);
  }, [reviewDefaultsKey, desiredReviewDefaultsKey, reviewDefaults, graph]);

  useEffect(() => {
    dispatch({
      type: 'LayoutPinsLoaded',
      scopeKey: desiredLayoutPinScopeKey,
      pins: loadLayoutPins(desiredLayoutPinScopeKey),
    });
  }, [dispatch, desiredLayoutPinScopeKey]);

  useEffect(() => {
    if (layoutPinScopeKey !== desiredLayoutPinScopeKey) return;
    saveLayoutPins(layoutPinScopeKey, layoutPins);
  }, [layoutPinScopeKey, desiredLayoutPinScopeKey, layoutPins]);

  useEffect(() => {
    if (!graph) return;
    if (typeof EventSource === 'undefined') return;
    const events = new EventSource(eventsAPIURL(activeWorktree));
    let refreshTimer: ReturnType<typeof setTimeout> | null = null;
    events.addEventListener('model-changed', () => {
      if (refreshTimer) window.clearTimeout(refreshTimer);
      refreshTimer = window.setTimeout(() => {
        dispatch({ type: 'GraphRequested', worktree: activeWorktree || undefined, source: 'auto' });
        // The working tree moved, so the cached file diff is as stale as the
        // canvas was. Re-read it too, or the two halves of the review would
        // start describing different working trees.
        reloadDiff();
      }, 250);
    });
    return () => {
      if (refreshTimer) window.clearTimeout(refreshTimer);
      events.close();
    };
  }, [activeWorktree, dispatch, graph, reloadDiff]);

  // Determine if diff mode is active (semantic — raw graph)
  const showDiff = graph.pr != null;

  // Map bounded-context id → display name, so each component's header icon can
  // show its parent (BC) initial instead of its own.
  const bcNameById = useMemo(() => {
    const m = new Map<string, string>();
    for (const bc of displayGraph.boundedContexts) m.set(bc.id, bc.name);
    return m;
  }, [displayGraph.boundedContexts]);

  // Derive changes from the current review projection, so the first-screen
  // summary matches the selected view/scope/filter instead of the full repo.
  const changes = useMemo(() => deriveChanges(displayGraph), [displayGraph]);
  const changeStats = useMemo(() => deriveChangeStats(changes), [changes]);
  const projectedPrStats = useMemo(
    () => graph.pr
      ? {
          added: changeStats.added,
          removed: changeStats.removed,
          changed: changeStats.changed,
          comments: graph.pr.stats.comments,
        }
      : null,
    [graph.pr, changeStats]
  );

  // Canvas wrap ref for scroll operations
  const canvasWrapRef = useRef<HTMLDivElement>(null);

  // Empty slack (unscaled px) reserved around the diagram on every side so the
  // canvas feels "borderless": you can drag/scroll well past the diagram in any
  // direction (≈ one screen). The diagram content is shifted right/down by this
  // much, so all viewport↔content scroll math below adds PAN_MARGIN.
  // (PAN_MARGIN imported from ./view/viewportConstants)
  // True when the last pointer interaction was a pan-drag, so the click that
  // follows mouseup doesn't also clear focus/selection.
  const didPanRef = useRef(false);

  // ── Zoom ────────────────────────────────────────────────────────────────
  // Applied as a CSS transform on .hf-canvas; a sizer reserves the scaled space
  // so scrollbars track. Default 1 (100%).
  // (ZOOM_MIN, ZOOM_MAX, ZOOM_STEP imported from ./view/viewportConstants)
  const zoomBy = (delta: number) => {
    const z = storeApi.getState().ui.zoom;
    const next = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, Math.round((z + delta) * 100) / 100));
    if (next !== z) dispatch({ type: 'ZoomChanged', zoom: next });
  };
  const fitZoom = () => {
    const wrap = canvasWrapRef.current;
    if (!wrap) {
      dispatch({ type: 'ZoomChanged', zoom: 1 });
      return;
    }
    const fit = Math.min(
      wrap.clientWidth / canvasDimensions.width,
      wrap.clientHeight / canvasDimensions.height,
      1
    );
    const z = Math.max(ZOOM_MIN, Math.round(fit * 100) / 100);
    // Land the diagram's top-left near the viewport corner (past the margin).
    // Applied via pendingScrollRef so it runs after the sizer resizes for `z`.
    pendingScrollRef.current = { left: PAN_MARGIN * z - 40, top: PAN_MARGIN * z - 40 };
    dispatch({ type: 'ZoomChanged', zoom: z });
  };

  // After a wheel-zoom changes `zoom`, re-anchor the scroll so the content point
  // under the cursor stays put. Applied in a layout effect so the sizer has
  // already grown/shrunk (otherwise scroll would clamp to the stale size).
  const pendingScrollRef = useRef<{ left: number; top: number } | null>(null);
  useLayoutEffect(() => {
    const wrap = canvasWrapRef.current;
    if (wrap && pendingScrollRef.current) {
      wrap.scrollLeft = pendingScrollRef.current.left;
      wrap.scrollTop = pendingScrollRef.current.top;
      pendingScrollRef.current = null;
    }
  }, [zoom]);

  // Ctrl/Cmd + wheel (and trackpad pinch, which the browser reports as
  // ctrl+wheel) zooms the diagram instead of the whole page. Attached natively
  // with { passive: false } because React's onWheel is passive — preventDefault
  // there is ignored. Plain wheel keeps scrolling the canvas as usual.
  useEffect(() => {
    const wrap = canvasWrapRef.current;
    if (!wrap) return;
    const onWheel = (e: WheelEvent) => {
      if (!(e.ctrlKey || e.metaKey)) return;
      e.preventDefault();
      const rect = wrap.getBoundingClientRect();
      const cx = e.clientX - rect.left;
      const cy = e.clientY - rect.top;
      const step = e.deltaY < 0 ? ZOOM_STEP : -ZOOM_STEP;
      const z = storeApi.getState().ui.zoom;
      const next = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, Math.round((z + step) * 100) / 100));
      if (next === z) return;
      // Content point currently under the cursor (unscaled canvas coords);
      // the diagram is shifted by PAN_MARGIN, so subtract it here…
      const contentX = (wrap.scrollLeft + cx) / z - PAN_MARGIN;
      const contentY = (wrap.scrollTop + cy) / z - PAN_MARGIN;
      // …and add it back so that point remains under the cursor at `next`.
      pendingScrollRef.current = {
        left: (contentX + PAN_MARGIN) * next - cx,
        top: (contentY + PAN_MARGIN) * next - cy,
      };
      dispatch({ type: 'ZoomChanged', zoom: next });
    };
    wrap.addEventListener('wheel', onWheel, { passive: false });
    return () => wrap.removeEventListener('wheel', onWheel);
  }, [storeApi, dispatch]);

  // Drag-to-pan: grab empty canvas background and drag to scroll the diagram in
  // any direction. Drags that start on an interactive node (component, port,
  // edge, marker, chrome) are ignored so clicks/selection still work there.
  useEffect(() => {
    const wrap = canvasWrapRef.current;
    if (!wrap) return;
    let pan: { x: number; y: number; left: number; top: number; moved: boolean } | null = null;
    const INTERACTIVE = '.hf-cmp, .hf-port, .hf-pin-marker, .hf-popover, .edges-svg g, .hf-canvas-toolbar, .hf-canvas-legend';
    const onDown = (e: MouseEvent) => {
      if (e.button !== 0) return;
      const t = e.target as HTMLElement;
      if (t.closest && t.closest(INTERACTIVE)) return;
      pan = { x: e.clientX, y: e.clientY, left: wrap.scrollLeft, top: wrap.scrollTop, moved: false };
      wrap.classList.add('panning');
    };
    const onMove = (e: MouseEvent) => {
      if (!pan) return;
      const dx = e.clientX - pan.x;
      const dy = e.clientY - pan.y;
      if (Math.abs(dx) > 3 || Math.abs(dy) > 3) pan.moved = true;
      wrap.scrollLeft = pan.left - dx;
      wrap.scrollTop = pan.top - dy;
    };
    const onUp = () => {
      if (pan && pan.moved) didPanRef.current = true;
      pan = null;
      wrap.classList.remove('panning');
    };
    wrap.addEventListener('mousedown', onDown);
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
    return () => {
      wrap.removeEventListener('mousedown', onDown);
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };
  }, []);

  // ── Initial fit ─────────────────────────────────────────────────────────
  // When a layout lands for a fresh view (first load or a review view/scope/
  // grouping switch), fit the CONTENT bounding box into the viewport: pick a
  // zoom that shows the whole graph and scroll to the content's top-left, so
  // the canvas never opens onto the empty PAN_MARGIN slack. In an embed the
  // wrap can still be zero-sized when the layout resolves (hidden iframe) —
  // then the fit is retried by a ResizeObserver once the wrap gets real
  // dimensions instead of being silently clamped to scroll 0,0.
  const needsFitRef = useRef(true);
  const displayLaidRef = useRef<typeof displayLaid>(null);
  displayLaidRef.current = displayLaid;

  const fitToContent = (): boolean => {
    const wrap = canvasWrapRef.current;
    const laid = displayLaidRef.current;
    if (!wrap || !laid) return false;
    if (wrap.clientWidth < 40 || wrap.clientHeight < 40) return false;
    let minX = Infinity;
    let minY = Infinity;
    let maxX = -Infinity;
    let maxY = -Infinity;
    for (const bc of laid.boundedContexts) {
      if (bc.x == null || bc.y == null) continue;
      minX = Math.min(minX, bc.x);
      minY = Math.min(minY, bc.y);
      maxX = Math.max(maxX, bc.x + (bc.w ?? 0));
      maxY = Math.max(maxY, bc.y + (bc.h ?? 0));
    }
    for (const c of laid.components) {
      if (c.x == null || c.y == null) continue;
      minX = Math.min(minX, c.x);
      minY = Math.min(minY, c.y);
      maxX = Math.max(maxX, c.x + (c.wx ?? c.w ?? 220));
      maxY = Math.max(maxY, c.y + (c.hx ?? c.h ?? 86));
    }
    if (!Number.isFinite(minX) || maxX <= minX || maxY <= minY) return false;
    const PAD = 40;
    const bw = maxX - minX + PAD * 2;
    const bh = maxY - minY + PAD * 2;
    const fit = Math.min(wrap.clientWidth / bw, wrap.clientHeight / bh, 1);
    const z = Math.max(ZOOM_MIN, Math.round(fit * 100) / 100);
    const target = {
      left: (PAN_MARGIN + minX - PAD) * z,
      top: (PAN_MARGIN + minY - PAD) * z,
    };
    if (z !== storeApi.getState().ui.zoom) {
      // Scroll applies via pendingScrollRef after the sizer resizes for `z`.
      pendingScrollRef.current = target;
      dispatch({ type: 'ZoomChanged', zoom: z });
    } else {
      wrap.scrollLeft = target.left;
      wrap.scrollTop = target.top;
    }
    return true;
  };

  // A review view/scope/grouping switch re-lays out into very different
  // geometry — re-fit on the next LayoutComputed.
  const reviewSelectionKey = `${reviewViewId}|${reviewScopeId}|${reviewGroupingId}`;
  const prevSelectionKeyRef = useRef(reviewSelectionKey);
  if (prevSelectionKeyRef.current !== reviewSelectionKey) {
    prevSelectionKeyRef.current = reviewSelectionKey;
    needsFitRef.current = true;
  }

  useLayoutEffect(() => {
    if (needsFitRef.current && fitToContent()) needsFitRef.current = false;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [displayLaid]);

  useEffect(() => {
    const wrap = canvasWrapRef.current;
    if (!wrap || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(() => {
      if (needsFitRef.current && fitToContent()) needsFitRef.current = false;
    });
    ro.observe(wrap);
    return () => ro.disconnect();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Comment targets for highlighting (from current markers)
  const commentTargets = useMemo(
    () => new Set(markers.map((m) => m.target.id)),
    [markers]
  );

  // Focus an object from the context tree: focus + expand + scroll. The viewport
  // effect scrolls on the next LayoutComputed, via the DomViewport.
  const focusFromTree = (t: TreeFocusTarget) => {
    dispatch({ type: 'TreeFocusRequested', target: t });
  };

  // Handle component selection (for focus mode) — store toggles focus.
  const handleSelectComponent = (cmp: ComponentType) => {
    dispatch({ type: 'ComponentSelected', id: cmp.id });
  };

  const handleMoveComponent = (id: string, x: number, y: number) => {
    dispatch({ type: 'ComponentLayoutPinned', id, x, y });
  };

  const handleMoveGroup = (id: string, dx: number, dy: number) => {
    if (!displayLaid) return;
    const pins: LayoutPins = {};
    for (const component of displayLaid.components) {
      if (component.bc !== id || component.x == null || component.y == null) continue;
      pins[component.id] = {
        x: Math.max(0, Math.round(component.x + dx)),
        y: Math.max(0, Math.round(component.y + dy)),
      };
    }
    if (Object.keys(pins).length > 0) {
      dispatch({ type: 'ComponentsLayoutPinned', pins });
    }
  };

  const handleResetGroupLayout = (id: string) => {
    if (!displayLaid) return;
    const componentIds = displayLaid.components
      .filter((component) => component.bc === id)
      .map((component) => component.id);
    if (componentIds.length > 0) {
      dispatch({ type: 'LayoutGroupPinsReset', componentIds });
    }
  };

  const handleResetRepoLayout = () => {
    clearLayoutPinsForRepo(graph);
    dispatch({ type: 'LayoutRepoPinsReset' });
  };

  const refreshGraph = () => {
    dispatch({ type: 'GraphRequested', worktree: activeWorktree || undefined });
  };

  const toggleArchMotifPanel = () => {
    setSourceViewer(null);
    setSymbolFocus(null);
    dispatch({ type: 'ArchMotifToggled' });
  };

  const openSourceFile = (path: string) => {
    setSymbolFocus(null);
    const seq = ++sourceRequestSeq.current;
    setSourceViewer({ path, status: 'loading' });
    fetch(sourceAPIURL(path, activeWorktree))
      .then(async (res) => {
        if (!res.ok) {
          const msg = await res.text();
          throw new Error(msg.trim() || `HTTP ${res.status}`);
        }
        return res.json() as Promise<{ path: string; content: string; hash?: string }>;
      })
      .then((data) => {
        if (sourceRequestSeq.current !== seq) return;
        setSourceViewer({ path: data.path || path, status: 'loaded', content: data.content, hash: data.hash });
      })
      .catch((err: unknown) => {
        if (sourceRequestSeq.current !== seq) return;
        setSourceViewer({
          path,
          status: 'error',
          error: err instanceof Error ? err.message : String(err),
        });
      });
  };

  // Jump from a file container on the canvas straight into that file's patch.
  const openFileDiff = (path: string) => {
    diffSession.select(path);
    setDiffOpen(true);
  };

  const saveSourceFile = async (path: string, content: string, baseHash: string): Promise<SaveSourceResult> => {
    const res = await fetch(sourceAPIURL(path, activeWorktree), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, content, baseHash }),
    });
    if (!res.ok) {
      const msg = await res.text();
      throw new Error(msg.trim() || `HTTP ${res.status}`);
    }
    const data = (await res.json()) as SaveSourceResult;
    const next = { path: data.path || path, status: 'loaded' as const, content: data.content, hash: data.hash };
    setSourceViewer(next);
    dispatch({ type: 'GraphRequested', worktree: activeWorktree || undefined });
    return data;
  };

  // Handle canvas background click (clear focus + pending comment + activeMarkerId).
  // Skipped right after a pan-drag so dragging the canvas doesn't also clear the selection.
  const handleCanvasClick = () => {
    if (didPanRef.current) {
      didPanRef.current = false;
      return;
    }
    dispatch({ type: 'CanvasCleared' });
  };

  // Calculate canvas dimensions based on laid-out content (geometry)
  const canvasDimensions = useMemo(() => {
    let maxX = 1000;
    let maxY = 600;

    // Use laid geometry; fall back to raw (pre-layout) values so canvas
    // is not zero-sized on the initial render before ELK resolves.
    const bcs = displayLaid?.boundedContexts ?? displayGraph.boundedContexts;
    const cmps = displayLaid?.components ?? displayGraph.components;

    for (const bc of bcs) {
      if (bc.x != null && bc.w != null) {
        maxX = Math.max(maxX, bc.x + bc.w + 50);
      }
      if (bc.y != null && bc.h != null) {
        maxY = Math.max(maxY, bc.y + bc.h + 50);
      }
    }

    for (const c of cmps) {
      if (c.x != null && c.wx != null) {
        maxX = Math.max(maxX, c.x + c.wx + 50);
      }
      if (c.y != null && c.hx != null) {
        maxY = Math.max(maxY, c.y + c.hx + 100);
      }
    }

    return { width: maxX, height: maxY };
  }, [displayLaid, displayGraph.boundedContexts, displayGraph.components]);

  // Bind the DomViewport to the live canvas so the viewport effect can scroll
  // imperatively (reading zoom/dimensions from the store + this render's geometry).
  useLayoutEffect(() => {
    viewport.bind({
      el: canvasWrapRef.current!,
      getZoom: () => storeApi.getState().ui.zoom,
      getCanvasDimensions: () => canvasDimensions,
      scheduleScroll: (target) => {
        pendingScrollRef.current = target;
      },
    });
    return () => viewport.bind(null);
  }, [viewport, storeApi, canvasDimensions]);

  return (
    <div
      className={`hifi v4 theme-${theme}${EMBED ? ' embed' : ''}`}
      style={{ width: '100%', height: '100vh', display: 'flex', flexDirection: 'column' }}
    >
      {/* AppBar uses repo-level PR data; PrHeader stats reflect the active review projection. */}
      <AppBar
        theme={theme}
        onThemeToggle={() => dispatch({ type: 'ThemeToggled' })}
        onRefresh={refreshGraph}
        refreshing={load.status === 'loading'}
        onMetrics={toggleArchMotifPanel}
        onDiff={() => setDiffOpen(true)}
        onAsk={() => {
          if (leftCollapsed) dispatch({ type: 'LeftCollapsedToggled' });
          dispatch({ type: 'LeftTabChanged', tab: 'ask' });
        }}
        asking={askProjection != null}
        pr={graph.pr}
      />

      {graph.pr && projectedPrStats && (
        <PrHeader pr={graph.pr} stats={projectedPrStats} policyCount={changeStats.policy} />
      )}

      {(
        askProjection != null ||
        ((graph.worktrees?.length ?? 0) > 0) ||
        ((graph.reviewViews?.length ?? 0) > 0) ||
        ((graph.reviewScopes?.length ?? 0) > 0) ||
        ((graph.reviewGroupings?.length ?? 0) > 0)
      ) && (
        <div className="hf-reviewbar">
          {graph.worktrees && graph.worktrees.length > 0 && (
            <label title="Branch/worktree being reviewed.">
              <span>Branch</span>
              <select
                value={activeWorktree}
                onChange={(e) => dispatch({ type: 'WorktreeChanged', name: e.target.value })}
              >
                {graph.worktrees.map((worktree) => (
                  <option key={worktree.name} value={worktree.name}>
                    {worktree.branch ? `${worktree.name} (${worktree.branch})` : worktree.name}
                    {worktree.base ? ' - base' : ''}
                  </option>
                ))}
              </select>
            </label>
          )}
          {hasViewOptions && (
            <div className="hf-reviewbar-options">
              <button
                className={`hf-reviewbar-toggle ${viewOptionsOpen ? 'on' : ''}`}
                title="Packages, surface, grouping and change filters"
                onClick={() => setViewOptionsOpen((open) => !open)}
              >
                View {viewOptionsOpen ? '\u25B4' : '\u25BE'}
              </button>
              {viewOptionsOpen && (
                <div className="hf-reviewbar-popover">
                  {graph.reviewViews && graph.reviewViews.length > 0 && (
                    <label title="Package set for this review slice.">
                      <span>Packages</span>
                      <select
                        value={reviewViewId ?? ''}
                        onChange={(e) => dispatch({ type: 'ReviewViewChanged', id: e.target.value })}
                      >
                        {graph.reviewViews.map((view) => (
                          <option key={view.id} value={view.id}>
                            {view.title} ({view.componentCount})
                          </option>
                        ))}
                      </select>
                    </label>
                  )}
                  {graph.reviewScopes && graph.reviewScopes.length > 0 && (
                    <label title="Symbol visibility inside selected packages.">
                      <span>Surface</span>
                      <select
                        value={reviewScopeId ?? ''}
                        onChange={(e) => dispatch({ type: 'ReviewScopeChanged', id: e.target.value })}
                      >
                        {graph.reviewScopes.map((scope) => (
                          <option key={scope.id} value={scope.id}>
                            {scope.title}
                          </option>
                        ))}
                      </select>
                    </label>
                  )}
                  {groupingOptions.length > 1 && (
                    <label title="Group visible packages on the canvas.">
                      <span>Group by</span>
                      <select
                        value={reviewGroupingId ?? ''}
                        onChange={(e) => dispatch({ type: 'ReviewGroupingChanged', id: e.target.value })}
                      >
                        {groupingOptions.map((grouping) => (
                          <option key={grouping.id} value={grouping.id}>
                            {reviewGroupingLabel(grouping)}
                          </option>
                        ))}
                      </select>
                    </label>
                  )}
                  {graph.pr && (
                    <>
                      <label title="How far to expand from changed packages.">
                        <span>Focus</span>
                        <select
                          value={reviewImpactMode}
                          onChange={(e) => dispatch({ type: 'ReviewImpactModeChanged', mode: e.target.value as typeof reviewImpactMode })}
                        >
                          <option value="changed_only">Changed packages</option>
                          <option value="changed_neighbors">Changed + linked packages</option>
                          <option value="containing_group">Changed package groups</option>
                          <option value="review_view">Whole view</option>
                          <option value="repository">Whole repo</option>
                        </select>
                      </label>
                      <label title="Which change kinds are visible.">
                        <span>Changes</span>
                        <select
                          value={reviewChangeFilter}
                          onChange={(e) => dispatch({ type: 'ReviewChangeFilterChanged', filter: e.target.value as typeof reviewChangeFilter })}
                        >
                          <option value="all">All changes</option>
                          <option value="added">Additions</option>
                          <option value="removed">Removals</option>
                          <option value="changed">Changed signatures</option>
                          <option value="dependency">Dependency changes</option>
                          <option value="policy">Policy/grouping</option>
                        </select>
                      </label>
                      <label title="How much package detail to show for visible packages.">
                        <span>Details</span>
                        <select
                          value={changedDetailsOnly ? 'changed' : 'full'}
                          onChange={(e) => {
                            const next = e.target.value === 'changed';
                            if (next !== changedDetailsOnly) dispatch({ type: 'ChangedDetailsOnlyToggled' });
                          }}
                        >
                          <option value="changed">Changed symbols</option>
                          <option value="full">Full package</option>
                        </select>
                      </label>
                    </>
                  )}
                </div>
              )}
            </div>
          )}
          <span className="hf-reviewbar-meta">
            {displayGraph.components.length} packages
          </span>
          {askProjection && (
            <span className="hf-reviewbar-ask" title={`Answering: ${ask.query}`}>
              ask: {ask.query}
              <button
                className="hf-reviewbar-ask-clear"
                onClick={() => dispatch({ type: 'AskCleared' })}
                title="Back to the review"
              >
                ✕
              </button>
            </span>
          )}
          {graph.repo?.compare && (
            <span className="hf-reviewbar-meta">
              {graph.repo.compare}
            </span>
          )}
        </div>
      )}

      <div className="hf-stage">
        {/* LEFT PANE - collapsible, semantic review tree from the active projection */}
        <div className={`hf-side hf-collapsible ${leftCollapsed ? 'collapsed' : ''}`}>
          <button
            className="hf-side-toggle left"
            onClick={() => dispatch({ type: 'LeftCollapsedToggled' })}
          >
            {leftCollapsed ? '›' : '‹'}
          </button>

          {leftCollapsed ? (
            <span className="hf-side-vlabel">
              {leftTab === 'ask' ? 'ASK' : 'REVIEW'}
            </span>
          ) : (
            <>
              <div className="hf-tabs" style={{ flexShrink: 0 }}>
                <button
                  className={leftTab === 'review' ? 'on' : ''}
                  onClick={() => dispatch({ type: 'LeftTabChanged', tab: 'review' })}
                >
                  REVIEW<span className="count">{showDiff ? changes.length : displayGraph.components.length}</span>
                </button>
                <button
                  className={leftTab === 'ask' ? 'on' : ''}
                  onClick={() => dispatch({ type: 'LeftTabChanged', tab: 'ask' })}
                >
                  ASK{ask.hits.length > 0 && <span className="count">{ask.hits.length}</span>}
                </button>
              </div>

              {leftTab === 'ask' ? (
                <AskPanel
                  ask={ask}
                  groups={askGroups}
                  packageCount={askProjection ? askProjection.componentIds.size : 0}
                  onSubmit={(query) => dispatch({ type: 'AskSubmitted', query })}
                  onClear={() => dispatch({ type: 'AskCleared' })}
                  onHitClick={(hit) => dispatch({ type: 'AskHitActivated', hit })}
                  onDetailOnlyToggle={() => dispatch({ type: 'AskDetailOnlyToggled' })}
                  onDepthChange={(k) => {
                    dispatch({ type: 'AskDepthChanged', k });
                    if (ask.query) dispatch({ type: 'AskSubmitted', query: ask.query });
                  }}
                />
              ) : (
                <div className="hf-list" style={{ paddingTop: 6 }}>
                  <Tree
                    boundedContexts={displayGraph.boundedContexts}
                    components={displayGraph.components}
                    showDiff={showDiff}
                    activeId={focusId}
                    onFocus={focusFromTree}
                    onOpenFile={openSourceFile}
                  />
                </div>
              )}
            </>
          )}
        </div>

        {/* CENTER PANE - canvas viewport (does not scroll; pins toolbar/legend) */}
        <div className="hf-canvas-viewport">
        <div
          ref={canvasWrapRef}
          className="hf-canvas-wrap"
          onClick={handleCanvasClick}
        >
          {/* Show loading placeholder until first ELK layout resolves.
              We keep the previous laid graph visible during re-layout so
              the canvas never flashes empty. On an unrecoverable ELK error
              we surface a message instead of hanging indefinitely. */}
          {displayLaid == null ? (
            <div style={{ padding: 20 }}>
              {layoutError != null
                ? <p style={{ color: 'var(--rem-fg)' }}>Layout failed: {layoutError}</p>
                : <p>Laying out…</p>
              }
            </div>
          ) : (
          // Sizer reserves the scaled footprint so scrollbars track the zoom;
          // the inner .hf-canvas is scaled from its top-left corner.
          <div
            className="hf-canvas-sizer"
            style={{
              width: (canvasDimensions.width + 2 * PAN_MARGIN) * zoom,
              height: (canvasDimensions.height + 2 * PAN_MARGIN) * zoom,
            }}
          >
          <div
            className="hf-canvas"
            style={{
              width: canvasDimensions.width,
              height: canvasDimensions.height,
              transform: `translate(${PAN_MARGIN * zoom}px, ${PAN_MARGIN * zoom}px) scale(${zoom})`,
              transformOrigin: '0 0',
            }}
          >
            {/* Bounded context groups — geometry from laid */}
            <BCGroups
              boundedContexts={displayLaid.boundedContexts}
              show={true}
              zoom={zoom}
              onMoveGroup={handleMoveGroup}
              pinnedGroupIds={pinnedGroupIds}
              onResetGroupLayout={handleResetGroupLayout}
            />

            {/* Edge layer (SVG) — geometry from laid */}
            <EdgeLayer
              edges={displayLaid.edges}
              components={displayLaid.components}
              expandedSet={expanded}
              showDiff={showDiff}
              focusId={focusId}
              commentTargets={commentTargets}
            />

            <RelationLayer
              relations={(displayLaid.relations ?? []).filter(
                (relation) =>
                  relation.fromComponentId !== relation.toComponentId && isDiagramRelation(relation)
              )}
              components={displayLaid.components}
              // Seq-mode cards don't render internals, so their symbol
              // relations have no anchors — treat them as collapsed here.
              expandedSet={new Set([...expanded].filter((id) => !seqMode.has(id)))}
              showDiff={showDiff}
              focusId={focusId}
            />

            {/* Components — geometry from laid, expansion/focus from raw state */}
            {displayLaid.components.map((c) => (
              <Component
                key={c.id}
                cmp={c}
                expanded={expanded.has(c.id)}
                seqActive={seqMode.has(c.id)}
                onToggleSeq={(id) => dispatch({ type: 'ComponentSeqToggled', id })}
                onToggleExpand={(id) => dispatch({ type: 'ComponentToggled', id })}
                parentName={bcNameById.get(c.bc)}
                showDiff={showDiff}
                focused={focusId === c.id}
                dimmed={!!(focusId && related && !related.has(c.id))}
                onSelect={handleSelectComponent}
                commentTargets={commentTargets}
                pinned={activeLayoutPins[c.id] != null}
                cardDensity={cardDensity}
                showTypes={showInlineSignatures}
                relations={(displayLaid.relations ?? []).filter(
                  (relation) => relation.fromComponentId === c.id && relation.toComponentId === c.id
                )}
                zoom={zoom}
                onMove={handleMoveComponent}
                onResetLayout={(id) => dispatch({ type: 'LayoutPinReset', id })}
                onSymbolFocus={setSymbolFocus}
                onOpenFileDiff={openFileDiff}
              />
            ))}

            {/* Pinned numbered comment markers */}
            {markers.map((m) => (
              <PinnedMarker
                key={m.id}
                marker={m}
                active={activeMarkerId === m.id}
                onClick={(mm) => dispatch({ type: 'MarkerActivated', id: mm.id })}
              />
            ))}

          </div>
          </div>
          )}
        </div>

          {/* Canvas toolbar — pinned to the viewport, not the scroller */}
          <CanvasToolbar
            zoom={zoom}
            onZoomOut={() => zoomBy(-ZOOM_STEP)}
            onZoomIn={() => zoomBy(ZOOM_STEP)}
            onFit={() => {
              if (!fitToContent()) fitZoom();
            }}
            onExpandAll={() => dispatch({ type: 'ComponentsExpandedAll' })}
            onCollapseAll={() => dispatch({ type: 'ComponentsCollapsedAll' })}
            pinnedCount={pinnedCount}
            onResetLayout={pinnedCount > 0 ? () => dispatch({ type: 'LayoutPinsReset' }) : undefined}
            onResetRepoLayout={handleResetRepoLayout}
            cardDensity={cardDensity}
            onToggleCardDensity={() => dispatch({
              type: 'CardDensityChanged',
              density: cardDensity === 'compact' ? 'detailed' : 'compact',
            })}
            showInlineSignatures={showInlineSignatures}
            onToggleInlineSignatures={() => dispatch({ type: 'InlineSignaturesToggled' })}
          />

          {/* Legend */}
          <Legend showDiff={showDiff} />
          {symbolFocus && (
            <SymbolGraphOverlay
              graph={graph}
              target={symbolFocus}
              onClose={() => setSymbolFocus(null)}
            />
          )}
        </div>

        {/* ArchMotif metrics — an overlay over the canvas, opened from the app
            bar, instead of a permanent right rail eating canvas width. */}
        {archMotifOpen && (
          <div className="hf-motif-overlay">
            <div className="hf-motif-overlay-head">
              <span>ARCHMOTIF</span>
              <button
                className="hf-motif-overlay-close"
                title="Close ArchMotif"
                onClick={() => dispatch({ type: 'ArchMotifToggled' })}
              >
                ×
              </button>
            </div>
            <ArchMotifPanel worktree={activeWorktree} />
          </div>
        )}
        <SourceDrawer source={sourceViewer} onClose={() => setSourceViewer(null)} onSave={saveSourceFile} />

        {diffOpen && (
          <DiffOverlay
            session={diffSession}
            graph={graph}
            worktree={activeWorktree}
            baseRef={reviewBaseRef}
            onClose={() => setDiffOpen(false)}
          />
        )}
      </div>
    </div>
  );
}

function sourceAPIURL(path: string, worktree: string): string {
  const query = `file=${encodeURIComponent(path)}`;
  if (worktree) return `/w/${encodeURIComponent(worktree)}/api/source?${query}`;
  const prefix = currentWorktreePrefix();
  return `${prefix}/api/source?${query}`;
}

function eventsAPIURL(worktree: string): string {
  if (worktree) return `/w/${encodeURIComponent(worktree)}/api/events`;
  return `${currentWorktreePrefix()}/api/events`;
}

function currentWorktreePrefix(): string {
  if (typeof window === 'undefined') return '';
  const match = window.location.pathname.match(/^\/w\/[^/]+/);
  return match?.[0] ?? '';
}

function visibleReviewGroupings(
  graph: UIGraph,
  reviewViewId: string | null,
  reviewScopeId: string | null,
  currentGroupingId: string | null,
  options: ReviewSelectionOptions
): ReviewGrouping[] {
  const groupings = graph.reviewGroupings ?? [];
  if (groupings.length <= 1) return groupings;

  const out: ReviewGrouping[] = [];
  const seenSignatures = new Set<string>();

  for (const grouping of groupings) {
    const projection = selectReviewGraph(graph, reviewViewId, reviewScopeId, grouping.id, options);
    const signature = groupingProjectionSignature(projection);
    const selected = grouping.id === currentGroupingId;
    if (selected || !seenSignatures.has(signature)) {
      out.push(grouping);
      seenSignatures.add(signature);
    }
  }

  const withoutReviewViews = out.filter((grouping) => grouping.id !== 'review_view');
  return withoutReviewViews.length > 0 ? withoutReviewViews : out;
}

function groupingProjectionSignature(graph: UIGraph): string {
  const groups = new Map<string, string[]>();
  for (const component of graph.components) {
    const ids = groups.get(component.bc) ?? [];
    ids.push(component.id);
    groups.set(component.bc, ids);
  }
  return [...groups.values()]
    .map((componentIds) => componentIds.sort().join(','))
    .sort((a, b) => a.localeCompare(b))
    .join('|');
}

function reviewGroupingLabel(grouping: ReviewGrouping): string {
  switch (grouping.id) {
    case 'directory':
      return 'Package path';
    case 'review_view':
      return 'Review slice';
    case 'package_owner':
      return 'Owner';
    case 'layer':
      return 'Layer';
    case 'configured_groups':
      return 'Categories';
    default:
      return grouping.title;
  }
}
