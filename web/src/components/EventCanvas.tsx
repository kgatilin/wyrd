import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { EventModelPort } from '../domain/ports';
import {
  componentById,
  isolatedComponents,
  kindByName,
  linkId,
  sectionsOf,
  shortKind,
  type EventComponent,
  type EventModel,
  type EventSlot,
} from '../domain/eventModel';
import { layoutEventModel, linkPath, type EventLayout } from '../layout/eventLayout';

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
 */

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

  const model = session.status === 'ready' ? session.model : null;

  useEffect(() => {
    if (!model) {
      setLayout(null);
      return;
    }
    let live = true;
    layoutEventModel(model).then(
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
  }, [model]);

  // A new model is a new picture; a selection into the old one means nothing.
  useEffect(() => setSelected(null), [model]);

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
      <Header model={model} onClose={onClose} />
      <div className="hf-events-body">
        <div className="hf-events-stage">
          {session.status === 'loading' && <Notice text="Reading declarations…" />}
          {session.status === 'error' && <Notice text={session.error} tone="error" />}
          {session.status === 'ready' && session.model.components.length === 0 && (
            <Notice text="No event declarations under this root. Add .arch/events.yaml or .arch/asyncapi.yaml to a component directory." />
          )}
          {model && layout && (
            <Diagram
              model={model}
              layout={layout}
              selected={selected}
              onSelect={setSelected}
            />
          )}
        </div>
        {model && selected && (
          <Detail model={model} selection={selected} onSelect={setSelected} onClose={() => setSelected(null)} />
        )}
      </div>
    </div>
  );
}

/** What the reader clicked: a node, or the bundle of kinds on one edge. */
type Selection =
  | { kind: 'component'; id: string }
  | { kind: 'link'; from: string; to: string }
  | { kind: 'event'; name: string };

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

function Header({ model, onClose }: { model: EventModel | null; onClose: () => void }) {
  const imported = model?.components.filter((c) => c.source === 'asyncapi').length ?? 0;
  const unhealthy = model?.kinds.filter((k) => k.health && k.health !== 'ok').length ?? 0;

  return (
    <div className="hf-events-head">
      <div className="hf-events-title">
        <span className="hf-events-label">EVENT MODEL</span>
      </div>
      {model && (
        <div className="hf-events-stats">
          <span className="hf-events-stat">{model.components.length} components</span>
          <span className="hf-events-stat">{model.kinds.length} kinds</span>
          {imported > 0 && <span className="hf-events-stat import">{imported} imported</span>}
          {unhealthy > 0 && <span className="hf-events-stat warn">{unhealthy} unreached</span>}
        </div>
      )}
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
      <button className="hf-events-close" onClick={onClose} title="Close (Esc)">
        ×
      </button>
    </div>
  );
}

function Notice({ text, tone }: { text: string; tone?: 'error' }) {
  return <div className={`hf-events-notice${tone === 'error' ? ' error' : ''}`}>{text}</div>;
}

function Diagram({
  model,
  layout,
  selected,
  onSelect,
}: {
  model: EventModel;
  layout: EventLayout;
  selected: Selection | null;
  onSelect: (selection: Selection | null) => void;
}) {
  const isolated = useMemo(() => isolatedComponents(model), [model]);
  const PADDING = 32;

  // Which nodes and links the current selection lights up. Selecting a
  // component accents everything it touches, so the question "what does this
  // component talk to" is answered by one click rather than by tracing lines.
  const accented = useMemo(() => accentOf(selected, layout), [selected, layout]);

  return (
    <svg
      className="hf-events-svg"
      width={layout.width + PADDING * 2}
      height={layout.height + PADDING * 2}
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
  const kinds = laid.link.kinds;
  const first = shortKind(kinds[0].kind);
  const label = kinds.length === 1 ? first : `${first} +${kinds.length - 1}`;
  const width = label.length * 6.2 + 12;

  return (
    <g transform={`translate(${laid.labelX}, ${laid.labelY})`}>
      <rect className="hf-events-label-bg" x={-width / 2} y={-9} width={width} height={18} rx={3} />
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
  const imported = component.source === 'asyncapi';
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
      <text className="hf-events-node-name" x={12} y={22}>
        {component.id}
      </text>
      {imported && (
        <text className="hf-events-node-badge" x={width - 12} y={22} textAnchor="end">
          asyncapi
        </text>
      )}
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
  layout: EventLayout
): { active: boolean; nodes: Set<string>; links: Set<string> } {
  const nodes = new Set<string>();
  const links = new Set<string>();
  if (!selected) return { active: false, nodes, links };

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
  // with four observers is seen as one append rather than four arrows.
  for (const laid of layout.links) {
    if (laid.link.kinds.some((flow) => flow.kind === selected.name)) {
      links.add(laid.link.id);
      nodes.add(laid.link.from);
      nodes.add(laid.link.to);
    }
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
    </aside>
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

  return (
    <>
      <h2 className="hf-events-detail-title mono">{kind.name}</h2>
      {kind.description && <p className="hf-events-detail-desc">{kind.description}</p>}
      <dl className="hf-events-facts">
        {kind.pattern && <Fact label="subject" value={kind.pattern} mono />}
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
      {kind.schema != null && (
        <>
          <h3 className="hf-events-section-title">Payload</h3>
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
              {slot.role && <span className="hf-events-tag role">{slot.role}</span>}
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
