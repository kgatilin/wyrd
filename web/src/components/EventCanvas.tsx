import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { EventModelPort } from '../domain/ports';
import {
  componentById,
  healthCounts,
  isolatedComponents,
  kindsInState,
  kindByName,
  linkId,
  participantsOf,
  sectionsOf,
  UNHEALTHY,
  type EventComponent,
  type EventHealth,
  type EventKind,
  type EventModel,
  type EventSlot,
} from '../domain/eventModel';
import {
  LABEL_HEIGHT,
  labelText,
  labelWidth,
  layoutEventModel,
  linkPath,
  type EventDirection,
  type EventLayout,
} from '../layout/eventLayout';
import { CanvasToolbar } from './CanvasToolbar';
import { PAN_MARGIN, ZOOM_MAX, ZOOM_STEP } from '../view/viewportConstants';

/**
 * The event canvas: who appends what, and who it reaches.
 *
 * The daemon composes declarations from two formats — the native
 * `.arch/events.yaml` and an imported `.arch/asyncapi.yaml` — into one model,
 * and this draws the result. An imported node is marked, because it is not
 * validated: wyrd reads a published document to draw it, not to grade it, and a
 * reader should know which half of the picture carries findings.
 *
 * One edge per component pair, labelled with what travels it. The model's own
 * projection is one edge per kind, which puts six parallel lines between a pair
 * that exchanges six events and makes the shape unreadable at exactly the
 * moment it starts being interesting.
 *
 * It scrolls, pans and zooms like the review canvas, down to where the chrome
 * sits: legend top-left, zoom bottom-left, wheel scrolls the pane, Cmd/Ctrl +
 * wheel zooms about the cursor, drag the background to pan. Two diagrams in
 * one pane answering to different gestures is a cost paid by every reader who
 * moves between them.
 */

/** Room around the diagram so a node's border is never on the stage edge. */
const PADDING = 32;

/**
 * Below the review canvas's floor on purpose: an event model lays out wider
 * than a package graph, and a floor of 0.4 would open a large one already
 * cropped. Step, ceiling and the pan slack around the diagram are the review
 * canvas's, so the two behave the same under the same gesture.
 */
const MIN_ZOOM = 0.2;

function clampZoom(zoom: number): number {
  return Math.min(ZOOM_MAX, Math.max(MIN_ZOOM, Math.round(zoom * 100) / 100));
}

/** Scroll position that leaves the content point under (x, y) where it is. */
function anchoredScroll(
  wrap: HTMLElement,
  from: number,
  to: number,
  x: number,
  y: number
): { left: number; top: number } {
  const contentX = (wrap.scrollLeft + x) / from;
  const contentY = (wrap.scrollTop + y) / from;
  return { left: contentX * to - x, top: contentY * to - y };
}

type Session =
  | { status: 'loading' }
  | { status: 'error'; error: string }
  | { status: 'ready'; model: EventModel };

export interface EventCanvasProps {
  worktree: string;
  events: EventModelPort;
  /** Bumped by the model-changed SSE, so the canvas re-reads with the page. */
  reloadToken?: number;
  onClose: () => void;
}

export function EventCanvas({ worktree, events, reloadToken = 0, onClose }: EventCanvasProps) {
  const session = useEventModel(events, worktree, reloadToken);
  const [layout, setLayout] = useState<EventLayout | null>(null);
  const [selected, setSelected] = useState<Selection | null>(null);
  const wrapRef = useRef<HTMLDivElement>(null);
  const [zoom, setZoom] = useState(1);
  // Which way the flow runs. Not a property of event models — a chain reads
  // left to right and a hub reads downwards — so the reader picks.
  const [direction, setDirection] = useState<EventDirection>('DOWN');
  // Handlers on the DOM read the live zoom from here: a listener attached once
  // closes over the zoom of the render that attached it, which is already stale
  // by the second tick of a wheel gesture.
  const zoomRef = useRef(zoom);
  zoomRef.current = zoom;
  // A scroll position parked until the sizer has resized for a new zoom.
  // Scrolling before that clamps against the old, smaller extent.
  const pendingScrollRef = useRef<{ left: number; top: number } | null>(null);
  // True when the last gesture was a pan-drag, so the click that ends it does
  // not also clear the selection.
  const didPanRef = useRef(false);

  const model = session.status === 'ready' ? session.model : null;
  const content = {
    width: (layout?.width ?? 0) + PADDING * 2,
    height: (layout?.height ?? 0) + PADDING * 2,
  };

  useEffect(() => {
    if (!model) {
      setLayout(null);
      return;
    }
    let live = true;
    layoutEventModel(model, direction).then(
      (laid) => {
        if (live) setLayout(laid);
      },
      () => {
        if (live) setLayout(null);
      }
    );
    return () => {
      live = false;
    };
  }, [model, direction]);

  // A new model is a new picture; a selection into the old one means nothing.
  useEffect(() => setSelected(null), [model]);

  // Fit each new diagram to the pane and centre it there. A model of any size
  // lays out wider than a viewport, and opening onto the top-left corner of one
  // shows a reader four nodes and no shape. Never above 1:1 — scaling three
  // nodes up to fill a screen makes them look like something they are not.
  const fit = useCallback(() => {
    const wrap = wrapRef.current;
    if (!wrap || !layout || layout.width === 0) return;
    const width = layout.width + PADDING * 2;
    const height = layout.height + PADDING * 2;
    const next = clampZoom(Math.min(1, wrap.clientWidth / width, wrap.clientHeight / height));
    const target = {
      left: PAN_MARGIN * next - Math.max(0, (wrap.clientWidth - width * next) / 2),
      top: PAN_MARGIN * next - Math.max(0, (wrap.clientHeight - height * next) / 2),
    };
    // Only a zoom change resizes the sizer, so only then is the scroll parked;
    // at an unchanged zoom the extent is already right and it applies now.
    if (next === zoomRef.current) {
      wrap.scrollLeft = target.left;
      wrap.scrollTop = target.top;
      return;
    }
    pendingScrollRef.current = target;
    setZoom(next);
  }, [layout]);

  useEffect(fit, [fit]);

  // Apply a parked scroll once the sizer has grown or shrunk for the new zoom.
  useLayoutEffect(() => {
    const wrap = wrapRef.current;
    if (!wrap || !pendingScrollRef.current) return;
    wrap.scrollLeft = pendingScrollRef.current.left;
    wrap.scrollTop = pendingScrollRef.current.top;
    pendingScrollRef.current = null;
  }, [zoom]);

  /** A toolbar step, keeping the middle of the pane still. */
  const zoomBy = useCallback((delta: number) => {
    const wrap = wrapRef.current;
    const from = zoomRef.current;
    const to = clampZoom(from + delta);
    if (to === from) return;
    if (wrap) {
      pendingScrollRef.current = anchoredScroll(wrap, from, to, wrap.clientWidth / 2, wrap.clientHeight / 2);
    }
    setZoom(to);
  }, []);

  // Cmd/Ctrl + wheel — and the trackpad pinch the browser reports as
  // ctrl+wheel — zooms about the cursor; a plain wheel keeps scrolling the
  // pane. Attached natively with { passive: false } because React's onWheel is
  // passive and preventDefault there is ignored.
  useEffect(() => {
    const wrap = wrapRef.current;
    if (!wrap) return;
    const onWheel = (event: WheelEvent) => {
      if (!(event.ctrlKey || event.metaKey)) return;
      event.preventDefault();
      const from = zoomRef.current;
      const to = clampZoom(from + (event.deltaY < 0 ? ZOOM_STEP : -ZOOM_STEP));
      if (to === from) return;
      const rect = wrap.getBoundingClientRect();
      pendingScrollRef.current = anchoredScroll(
        wrap,
        from,
        to,
        event.clientX - rect.left,
        event.clientY - rect.top
      );
      setZoom(to);
    };
    wrap.addEventListener('wheel', onWheel, { passive: false });
    return () => wrap.removeEventListener('wheel', onWheel);
  }, []);

  // Drag the empty background to pan. Drags that start on a node, an edge or
  // the chrome are left alone, so clicking there still selects.
  useEffect(() => {
    const wrap = wrapRef.current;
    if (!wrap) return;
    const INTERACTIVE = '.hf-events-node, .hf-events-link, .hf-events-hud, .hf-events-close, .hf-events-tools';
    let pan: { x: number; y: number; left: number; top: number; moved: boolean } | null = null;
    const onDown = (event: MouseEvent) => {
      if (event.button !== 0) return;
      // A drag that ended outside the pane never produced the click that clears
      // this, so a new gesture starts from a clean flag rather than swallowing
      // its own click.
      didPanRef.current = false;
      const target = event.target as Element | null;
      if (target?.closest && target.closest(INTERACTIVE)) return;
      pan = { x: event.clientX, y: event.clientY, left: wrap.scrollLeft, top: wrap.scrollTop, moved: false };
      wrap.classList.add('panning');
    };
    const onMove = (event: MouseEvent) => {
      if (!pan) return;
      const dx = event.clientX - pan.x;
      const dy = event.clientY - pan.y;
      if (Math.abs(dx) > 3 || Math.abs(dy) > 3) pan.moved = true;
      wrap.scrollLeft = pan.left - dx;
      wrap.scrollTop = pan.top - dy;
    };
    const onUp = () => {
      if (pan?.moved) didPanRef.current = true;
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

  const select = useCallback((selection: Selection | null) => {
    if (didPanRef.current) {
      didPanRef.current = false;
      return;
    }
    setSelected(selection);
  }, []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      // Esc closes the detail first and the canvas second: a reader who opened
      // a node expects the first press to put it down, not to lose the graph.
      setSelected((current) => {
        if (current) return null;
        onClose();
        return null;
      });
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div className="hf-events">
      <div className="hf-events-viewport">
        <div className="hf-events-wrap" ref={wrapRef}>
          {session.status === 'loading' && <Notice text="Reading declarations…" />}
          {session.status === 'error' && <Notice text={session.error} tone="error" />}
          {session.status === 'ready' && session.model.components.length === 0 && (
            <Notice text="No event declarations under this root. Add .arch/events.yaml or .arch/asyncapi.yaml to a component directory." />
          )}
          {model && layout && (
            // The sizer reserves the scaled diagram plus a margin of slack on
            // every side, so the scrollbars track the zoom and the diagram can
            // be dragged away from the pane's edges.
            <div
              className="hf-events-sizer"
              style={{
                width: (content.width + PAN_MARGIN * 2) * zoom,
                height: (content.height + PAN_MARGIN * 2) * zoom,
              }}
            >
              <Diagram
                model={model}
                layout={layout}
                zoom={zoom}
                offset={PAN_MARGIN * zoom}
                selected={selected}
                onSelect={select}
              />
            </div>
          )}
        </div>
        <Hud model={model} selected={selected} onSelect={setSelected} />
        <div className="hf-events-tools">
          <CanvasToolbar
            zoom={zoom}
            onZoomOut={() => zoomBy(-ZOOM_STEP)}
            onZoomIn={() => zoomBy(ZOOM_STEP)}
            onFit={fit}
          />
          {/* Its own pill: the zoom moves the camera, this re-solves the
              picture, and the two are not the same kind of control. */}
          <div className="hf-canvas-toolbar">
            <button
              className="hf-events-direction"
              title={
                direction === 'DOWN' ? 'Lay the flow out left to right' : 'Lay the flow out top to bottom'
              }
              onClick={() => setDirection(direction === 'DOWN' ? 'RIGHT' : 'DOWN')}
            >
              {direction === 'DOWN' ? '↓' : '→'}
            </button>
          </div>
        </div>
        <button className="hf-events-close" onClick={onClose} title="Close (Esc)">
          ×
        </button>
      </div>
      {model && selected && (
        <Detail model={model} selection={selected} onSelect={setSelected} onClose={() => setSelected(null)} />
      )}
    </div>
  );
}

/**
 * What the reader clicked: a node, the bundle of kinds on one edge, one kind,
 * or a legend chip.
 *
 * A chip is a selection rather than a readout because the kinds it counts are
 * the ones the diagram cannot show: an orphan has no observer and a starved
 * kind has no producer, so neither sits on an edge, and a reader told there
 * are two of them has nowhere to look. Selecting the chip lists them and
 * accents the components that declared them.
 */
type Selection =
  | { kind: 'component'; id: string }
  | { kind: 'link'; from: string; to: string }
  | { kind: 'event'; name: string }
  | { kind: 'health'; health: EventHealth }
  | { kind: 'imported' };

function useEventModel(events: EventModelPort, worktree: string, reloadToken: number): Session {
  const [session, setSession] = useState<Session>({ status: 'loading' });
  const requestRef = useRef(0);

  const load = useCallback(() => {
    const request = ++requestRef.current;
    setSession({ status: 'loading' });
    events.load({ worktree: worktree || undefined }).then(
      (model) => {
        if (requestRef.current === request) setSession({ status: 'ready', model });
      },
      (error: unknown) => {
        if (requestRef.current !== request) return;
        setSession({ status: 'error', error: error instanceof Error ? error.message : String(error) });
      }
    );
  }, [events, worktree]);

  useEffect(load, [load, reloadToken]);

  return session;
}

/**
 * The roles the canvas has a colour for. They are the four an events.yaml slot
 * or an x-eventlog operation can carry; a document that states anything else
 * still shows its word, in neutral.
 */
const TONED_ROLES = new Set(['command', 'call', 'event', 'observe']);

function roleClass(role: string): string {
  const tone = role.toLowerCase();
  return TONED_ROLES.has(tone) ? ` ${tone}` : '';
}

/** What each unhealthy state means, for the legend chips. */
const HEALTH_HINTS: Record<string, string> = {
  orphan: 'appended by somebody, and no component takes it as an input or folds it into state',
  starved: 'taken as an input or folded into state, and no component appends it',
  ambiguous: 'declared delivery: exclusive, but not by exactly one input',
};

/**
 * What was read, what is unhealthy in it, and what the two line styles mean —
 * over the diagram's top-left corner, where the review canvas keeps its legend.
 * The counts sit with the legend rather than in a bar of their own: a chip is a
 * way into the diagram, and the way in belongs next to the key that reads it.
 */
function Hud({
  model,
  selected,
  onSelect,
}: {
  model: EventModel | null;
  selected: Selection | null;
  onSelect: (selection: Selection | null) => void;
}) {
  const imported = model?.components.filter((c) => c.source === 'asyncapi').length ?? 0;
  const health = healthCounts(model?.kinds ?? []);

  return (
    <div className="hf-events-hud">
      <div className="hf-events-stats">
        <span className="hf-events-label">EVENT MODEL</span>
        {model && (
          <>
            <span className="hf-events-stat">{model.components.length} components</span>
            <span className="hf-events-stat">{model.kinds.length} kinds</span>
            {imported > 0 && (
              <button
                className={`hf-events-stat import${selected?.kind === 'imported' ? ' on' : ''}`}
                title="Components read from an AsyncAPI document rather than events.yaml"
                onClick={() => onSelect(selected?.kind === 'imported' ? null : { kind: 'imported' })}
              >
                {imported} imported
              </button>
            )}
            {UNHEALTHY.filter((state) => health[state] > 0).map((state) => {
              const on = selected?.kind === 'health' && selected.health === state;
              return (
                <button
                  key={state}
                  className={`hf-events-stat warn${on ? ' on' : ''}`}
                  title={HEALTH_HINTS[state]}
                  onClick={() => onSelect(on ? null : { kind: 'health', health: state })}
                >
                  {health[state]} {state}
                </button>
              );
            })}
          </>
        )}
      </div>
      <div className="hf-events-legend">
        <span className="hf-events-legend-item">
          <svg width="26" height="8" aria-hidden="true">
            <line x1="0" y1="4" x2="26" y2="4" className="hf-events-legend-line trigger" />
          </svg>
          triggers
        </span>
        <span className="hf-events-legend-item">
          <svg width="26" height="8" aria-hidden="true">
            <line x1="0" y1="4" x2="26" y2="4" className="hf-events-legend-line fold" />
          </svg>
          folded into state
        </span>
      </div>
    </div>
  );
}

function Notice({ text, tone }: { text: string; tone?: 'error' }) {
  return <div className={`hf-events-notice${tone === 'error' ? ' error' : ''}`}>{text}</div>;
}

function Diagram({
  model,
  layout,
  zoom,
  offset,
  selected,
  onSelect,
}: {
  model: EventModel;
  layout: EventLayout;
  zoom: number;
  /** Where the diagram sits inside the sizer, i.e. the scaled pan slack. */
  offset: number;
  selected: Selection | null;
  onSelect: (selection: Selection | null) => void;
}) {
  const isolated = useMemo(() => isolatedComponents(model), [model]);
  const width = layout.width + PADDING * 2;
  const height = layout.height + PADDING * 2;

  // Which nodes and links the current selection lights up. Selecting a
  // component accents everything it touches, so the question "what does this
  // component talk to" is answered by one click rather than by tracing lines.
  const accented = useMemo(() => accentOf(selected, layout, model), [selected, layout, model]);

  return (
    <svg
      className="hf-events-svg"
      style={{ left: offset, top: offset }}
      width={width * zoom}
      height={height * zoom}
      viewBox={`0 0 ${width} ${height}`}
      onClick={() => onSelect(null)}
    >
      <defs>
        <marker id="hf-ev-arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto">
          <path d="M0 0 L8 4 L0 8 z" className="hf-events-arrow" />
        </marker>
        <marker id="hf-ev-arrow-on" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto">
          <path d="M0 0 L8 4 L0 8 z" className="hf-events-arrow on" />
        </marker>
      </defs>
      <g transform={`translate(${PADDING}, ${PADDING})`}>
        {layout.links.map((laid) => {
          const on = accented.links.has(laid.link.id);
          const dim = accented.active && !on;
          return (
            <g
              key={laid.link.id}
              className={`hf-events-link${on ? ' on' : ''}${dim ? ' dim' : ''}`}
              onClick={(e) => {
                e.stopPropagation();
                onSelect({ kind: 'link', from: laid.link.from, to: laid.link.to });
              }}
            >
              <path
                d={linkPath(laid.points)}
                className={`hf-events-line ${laid.link.trigger ? 'trigger' : 'fold'} ${laid.link.health}`}
                markerEnd={`url(#${on ? 'hf-ev-arrow-on' : 'hf-ev-arrow'})`}
              />
              <LinkLabel laid={laid} />
            </g>
          );
        })}
        {layout.nodes.map((node) => {
          const on = accented.nodes.has(node.id);
          const dim = accented.active && !on;
          return (
            <ComponentNode
              key={node.id}
              x={node.x}
              y={node.y}
              width={node.width}
              height={node.height}
              component={node.component}
              isolated={isolated.has(node.id)}
              accented={on}
              dimmed={dim}
              onSelect={() => onSelect({ kind: 'component', id: node.id })}
            />
          );
        })}
      </g>
    </svg>
  );
}

function LinkLabel({ laid }: { laid: EventLayout['links'][number] }) {
  // The same text and width the layout reserved space for; measuring it twice
  // differently is how a label ends up not fitting the gap made for it.
  const label = labelText(laid.link);
  const width = labelWidth(label);

  return (
    <g transform={`translate(${laid.labelX}, ${laid.labelY})`}>
      <rect
        className="hf-events-label-bg"
        x={-width / 2}
        y={-LABEL_HEIGHT / 2}
        width={width}
        height={LABEL_HEIGHT}
        rx={3}
      />
      <text className="hf-events-label-text" textAnchor="middle" dy="4">
        {label}
      </text>
    </g>
  );
}

function ComponentNode({
  x,
  y,
  width,
  height,
  component,
  isolated,
  accented,
  dimmed,
  onSelect,
}: {
  x: number;
  y: number;
  width: number;
  height: number;
  component: EventComponent;
  isolated: boolean;
  accented: boolean;
  dimmed: boolean;
  onSelect: () => void;
}) {
  const sections = sectionsOf(component);
  const instances = component.instances ?? [];

  return (
    <g
      className={`hf-events-node${accented ? ' on' : ''}${dimmed ? ' dim' : ''}${isolated ? ' isolated' : ''}`}
      transform={`translate(${x}, ${y})`}
      onClick={(e) => {
        e.stopPropagation();
        onSelect();
      }}
    >
      <rect className="hf-events-node-box" width={width} height={height} rx={6} />
      {/* No source badge on the box: which document a component was read from
          is a property of the declaration, not of the component's place in the
          picture, and it cost a name-width of room on every node to say so.
          The legend's "imported" chip accents them all at once, and the detail
          rail names the file. */}
      <text className="hf-events-node-name" x={12} y={22}>
        {component.id}
      </text>
      <text className="hf-events-node-sub" x={12} y={38}>
        {instances.length > 0
          ? `${instances.length} instances: ${instances.join(', ')}`
          : component.partition_key && component.partition_key.length > 0
            ? `keyed by ${component.partition_key.join(', ')}`
            : 'no partition key'}
      </text>
      <text className="hf-events-node-counts" x={12} y={54}>
        <tspan className="in">{sections.inputs.length} in</tspan>
        <tspan className="out" dx="10">
          {sections.outputs.length} out
        </tspan>
        <tspan className="state" dx="10">
          {sections.stateEvents.length} state
        </tspan>
      </text>
    </g>
  );
}

/** Nodes and links the current selection accents. */
function accentOf(
  selected: Selection | null,
  layout: EventLayout,
  model: EventModel | null
): { active: boolean; nodes: Set<string>; links: Set<string> } {
  const nodes = new Set<string>();
  const links = new Set<string>();
  if (!selected) return { active: false, nodes, links };

  if (selected.kind === 'imported') {
    for (const component of model?.components ?? []) {
      if (component.source === 'asyncapi') nodes.add(component.id);
    }
    return { active: true, nodes, links };
  }

  // A chip accents whoever declared the kinds it counts. Those kinds are the
  // ones with no edge to accent — that is what makes them findings — so the
  // node is the only place a reader can be pointed at.
  if (selected.kind === 'health') {
    for (const kind of kindsInState(model, selected.health)) {
      for (const id of participantsOf(kind)) nodes.add(id);
      for (const laid of layout.links) {
        if (laid.link.kinds.some((flow) => flow.kind === kind.name)) links.add(laid.link.id);
      }
    }
    return { active: true, nodes, links };
  }

  if (selected.kind === 'component') {
    nodes.add(selected.id);
    for (const laid of layout.links) {
      if (laid.link.from === selected.id || laid.link.to === selected.id) {
        links.add(laid.link.id);
        nodes.add(laid.link.from);
        nodes.add(laid.link.to);
      }
    }
    return { active: true, nodes, links };
  }

  if (selected.kind === 'link') {
    nodes.add(selected.from);
    nodes.add(selected.to);
    links.add(linkId(selected.from, selected.to));
    return { active: true, nodes, links };
  }

  // A kind lights up every edge that carries it, which is how a broadcast event
  // with four observers is seen as one append rather than four arrows — and
  // every component that declared it, so a kind travelling no edge at all
  // still points at the component to look at.
  for (const laid of layout.links) {
    if (laid.link.kinds.some((flow) => flow.kind === selected.name)) {
      links.add(laid.link.id);
      nodes.add(laid.link.from);
      nodes.add(laid.link.to);
    }
  }
  const kind = kindByName(model ?? { components: [], flows: [], kinds: [] }).get(selected.name);
  if (kind) {
    for (const id of participantsOf(kind)) nodes.add(id);
  }
  return { active: true, nodes, links };
}

function Detail({
  model,
  selection,
  onSelect,
  onClose,
}: {
  model: EventModel;
  selection: Selection;
  onSelect: (selection: Selection | null) => void;
  onClose: () => void;
}) {
  return (
    <aside className="hf-events-detail">
      <button className="hf-events-detail-close" onClick={onClose} title="Close (Esc)">
        ×
      </button>
      {selection.kind === 'component' && (
        <ComponentDetail model={model} id={selection.id} onSelect={onSelect} />
      )}
      {selection.kind === 'link' && (
        <LinkDetail model={model} from={selection.from} to={selection.to} onSelect={onSelect} />
      )}
      {selection.kind === 'event' && <KindDetail model={model} name={selection.name} onSelect={onSelect} />}
      {selection.kind === 'health' && (
        <HealthDetail model={model} health={selection.health} onSelect={onSelect} />
      )}
      {selection.kind === 'imported' && <ImportedDetail model={model} onSelect={onSelect} />}
    </aside>
  );
}

/**
 * What a legend chip opens: the kinds it counted, and who declared them.
 *
 * An orphan and a starved kind are both absent from the diagram — one has no
 * observer, the other has no producer, and neither sits on an edge — so the
 * list is where a reader finds them. Each row opens the kind, which names
 * everyone that appends, is triggered by, or folds it.
 */
function HealthDetail({
  model,
  health,
  onSelect,
}: {
  model: EventModel;
  health: EventHealth;
  onSelect: (selection: Selection | null) => void;
}) {
  const kinds = kindsInState(model, health);

  return (
    <>
      <h2 className="hf-events-detail-title">
        {kinds.length} {health}
      </h2>
      <p className="hf-events-detail-desc">{HEALTH_HINTS[health]}</p>
      <ul className="hf-events-slots">
        {kinds.map((kind) => (
          <li key={kind.name}>
            <button className="hf-events-slot" onClick={() => onSelect({ kind: 'event', name: kind.name })}>
              <span className="hf-events-slot-kind">{kind.name}</span>
              <span className="hf-events-tag excl">{health}</span>
              <span className="hf-events-slot-pattern">{whereItHangs(kind, health)}</span>
              {kind.pattern && <span className="hf-events-slot-pattern">{kind.pattern}</span>}
            </button>
          </li>
        ))}
      </ul>
    </>
  );
}

/** The component a finding is anchored to — the answer to "where do I look". */
function whereItHangs(kind: EventKind, health: EventHealth): string {
  const producers = kind.producers ?? [];
  const observers = [...(kind.triggers ?? []), ...(kind.folders ?? [])];
  if (health === 'starved') {
    return observers.length > 0 ? `observed by ${observers.join(', ')}` : 'observed by nobody';
  }
  if (health === 'ambiguous') {
    return `appended by ${producers.join(', ')} — ${kind.triggers?.length ?? 0} input(s)`;
  }
  return producers.length > 0 ? `appended by ${producers.join(', ')}` : 'appended by nobody';
}

/** The components read from an AsyncAPI document rather than from events.yaml. */
function ImportedDetail({
  model,
  onSelect,
}: {
  model: EventModel;
  onSelect: (selection: Selection | null) => void;
}) {
  const imported = model.components.filter((component) => component.source === 'asyncapi');

  return (
    <>
      <h2 className="hf-events-detail-title">{imported.length} imported</h2>
      <p className="hf-events-detail-desc">
        Read from an AsyncAPI 3 document with the x-eventlog extension. Their addresses are in wire
        coordinates and their partition key is taken as declared, so the conventions wyrd checks over
        events.yaml are not checked over them.
      </p>
      <ul className="hf-events-slots">
        {imported.map((component) => (
          <li key={component.id}>
            <button
              className="hf-events-slot"
              onClick={() => onSelect({ kind: 'component', id: component.id })}
            >
              <span className="hf-events-slot-kind">{component.id}</span>
              {component.instances && component.instances.length > 0 && (
                <span className="hf-events-tag role">{component.instances.join(', ')}</span>
              )}
              {component.source_file && (
                <span className="hf-events-slot-pattern">{component.source_file}</span>
              )}
            </button>
          </li>
        ))}
      </ul>
    </>
  );
}

function ComponentDetail({
  model,
  id,
  onSelect,
}: {
  model: EventModel;
  id: string;
  onSelect: (selection: Selection | null) => void;
}) {
  const component = componentById(model).get(id);
  if (!component) return <p className="hf-events-detail-empty">No declaration for {id}.</p>;

  const sections = sectionsOf(component);

  return (
    <>
      <h2 className="hf-events-detail-title">{component.id}</h2>
      {component.description && <p className="hf-events-detail-desc">{component.description}</p>}
      <dl className="hf-events-facts">
        {component.owns && <Fact label="owns" value={component.owns} />}
        {component.partition_key && component.partition_key.length > 0 && (
          <Fact label="key" value={component.partition_key.join(', ')} />
        )}
        {component.instances && component.instances.length > 0 && (
          <Fact label="instances" value={component.instances.join(', ')} />
        )}
        <Fact label="state" value={component.has_state ? 'declared' : 'none'} />
        {component.source === 'asyncapi' && <Fact label="source" value="AsyncAPI (not validated)" />}
        {component.source_file && <Fact label="file" value={component.source_file} mono />}
      </dl>
      <SlotSection title="Inputs" hint="triggers this component" slots={sections.inputs} onSelect={onSelect} />
      <SlotSection title="Outputs" hint="appended to the log" slots={sections.outputs} onSelect={onSelect} />
      <SlotSection
        title="State events"
        hint="folded without triggering"
        slots={sections.stateEvents}
        onSelect={onSelect}
      />
    </>
  );
}

function LinkDetail({
  model,
  from,
  to,
  onSelect,
}: {
  model: EventModel;
  from: string;
  to: string;
  onSelect: (selection: Selection | null) => void;
}) {
  const flows = model.flows.filter((flow) => flow.from === from && flow.to === to);
  return (
    <>
      <h2 className="hf-events-detail-title">
        <button className="hf-events-link-btn" onClick={() => onSelect({ kind: 'component', id: from })}>
          {from}
        </button>
        <span className="hf-events-arrow-glyph">→</span>
        <button className="hf-events-link-btn" onClick={() => onSelect({ kind: 'component', id: to })}>
          {to}
        </button>
      </h2>
      <p className="hf-events-detail-desc">
        {flows.length} {flows.length === 1 ? 'kind' : 'kinds'} travel this edge.
      </p>
      <ul className="hf-events-slots">
        {flows.map((flow) => (
          <li key={flow.kind}>
            <button className="hf-events-slot" onClick={() => onSelect({ kind: 'event', name: flow.kind })}>
              <span className="hf-events-slot-kind">{flow.kind}</span>
              <span className={`hf-events-tag ${flow.trigger ? 'trigger' : 'fold'}`}>
                {flow.trigger ? 'triggers' : 'folded'}
              </span>
            </button>
          </li>
        ))}
      </ul>
    </>
  );
}

function KindDetail({
  model,
  name,
  onSelect,
}: {
  model: EventModel;
  name: string;
  onSelect: (selection: Selection | null) => void;
}) {
  const kind = kindByName(model).get(name);
  if (!kind) return <p className="hf-events-detail-empty">No kind named {name}.</p>;

  const subjects = kind.subjects ?? (kind.pattern ? [kind.pattern] : []);

  return (
    <>
      <h2 className="hf-events-detail-title mono">{kind.name}</h2>
      {kind.description && <p className="hf-events-detail-desc">{kind.description}</p>}
      <dl className="hf-events-facts">
        {/* One kind can travel one address per port instance. Showing the
            first of them as "the" subject is what makes an edge into one
            instance read as an edge into all of them. */}
        {subjects.length > 1 ? (
          <>
            <dt className="hf-events-facts-label">subjects</dt>
            <dd className="hf-events-facts-value mono">
              {subjects.map((subject) => (
                <div key={subject}>{subject}</div>
              ))}
            </dd>
          </>
        ) : (
          kind.pattern && <Fact label="subject" value={kind.pattern} mono />
        )}
        {kind.partition_key && kind.partition_key.length > 0 && (
          <Fact label="partition" value={kind.partition_key.join(', ')} />
        )}
        {kind.class && <Fact label="class" value={kind.class} />}
        {kind.delivery && <Fact label="delivery" value={kind.delivery} />}
        {kind.owner && <Fact label="schema owner" value={kind.owner} />}
        {kind.health && kind.health !== 'ok' && <Fact label="health" value={kind.health} warn />}
      </dl>
      <Participants label="Appended by" ids={kind.producers} onSelect={onSelect} />
      <Participants label="Triggers" ids={kind.triggers} onSelect={onSelect} />
      <Participants label="Folded by" ids={kind.folders} onSelect={onSelect} />
      {kind.example !== undefined && kind.example !== null && (
        <>
          <h3 className="hf-events-section-title">
            Payload <span className="hf-events-section-hint">example</span>
          </h3>
          <pre className="hf-events-schema">{JSON.stringify(kind.example, null, 2)}</pre>
        </>
      )}
      {kind.schema != null && (
        <>
          <h3 className="hf-events-section-title">
            Payload <span className="hf-events-section-hint">schema</span>
          </h3>
          <pre className="hf-events-schema">{JSON.stringify(kind.schema, null, 2)}</pre>
        </>
      )}
    </>
  );
}

function Participants({
  label,
  ids,
  onSelect,
}: {
  label: string;
  ids: string[] | undefined;
  onSelect: (selection: Selection | null) => void;
}) {
  if (!ids || ids.length === 0) {
    return (
      <p className="hf-events-participants">
        <span className="hf-events-facts-label">{label}</span> <span className="hf-events-none">nobody</span>
      </p>
    );
  }
  return (
    <p className="hf-events-participants">
      <span className="hf-events-facts-label">{label}</span>{' '}
      {ids.map((id) => (
        <button key={id} className="hf-events-link-btn" onClick={() => onSelect({ kind: 'component', id })}>
          {id}
        </button>
      ))}
    </p>
  );
}

function SlotSection({
  title,
  hint,
  slots,
  onSelect,
}: {
  title: string;
  hint: string;
  slots: EventSlot[];
  onSelect: (selection: Selection | null) => void;
}) {
  if (slots.length === 0) return null;
  return (
    <>
      <h3 className="hf-events-section-title">
        {title} <span className="hf-events-section-hint">{hint}</span>
      </h3>
      <ul className="hf-events-slots">
        {slots.map((slot, index) => (
          <li key={`${slot.kind}-${index}`}>
            <button className="hf-events-slot" onClick={() => onSelect({ kind: 'event', name: slot.kind })}>
              <span className="hf-events-slot-kind">{slot.kind}</span>
              {slot.role && (
                <span className={`hf-events-tag role${roleClass(slot.role)}`}>{slot.role}</span>
              )}
              {slot.delivery === 'exclusive' && <span className="hf-events-tag excl">exclusive</span>}
              {slot.pattern && <span className="hf-events-slot-pattern">{slot.pattern}</span>}
              {slot.instances && slot.instances.length > 0 && (
                <span className="hf-events-slot-pattern">only {slot.instances.join(', ')}</span>
              )}
            </button>
          </li>
        ))}
      </ul>
    </>
  );
}

function Fact({ label, value, mono, warn }: { label: string; value: string; mono?: boolean; warn?: boolean }) {
  return (
    <>
      <dt className="hf-events-facts-label">{label}</dt>
      <dd className={`hf-events-facts-value${mono ? ' mono' : ''}${warn ? ' warn' : ''}`}>{value}</dd>
    </>
  );
}
