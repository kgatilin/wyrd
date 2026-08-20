import { useEffect, useMemo, useRef, useState } from 'react';
import type { UIGraph } from '../types';
import type { SymbolFocusTarget } from '../domain/symbolFocus';
import { componentPathPrefix } from '../domain/componentPath';
import {
  definitionLocation,
  definitionLookupIds,
  isDeclaringTypeFallback,
  type DefinitionAnchor,
  type SymbolDefinition,
} from '../domain/symbolDefinition';
import { fetchSymbolDefinition } from '../data/symbolDefinition';
import { highlightedLines } from './highlight';
import {
  buildNeighborhood,
  toTarget,
  type NeighborDirection,
  type NeighborGroup,
  type NeighborLink,
} from '../domain/symbolNeighborhood';

export interface SymbolGraphOverlayProps {
  graph: UIGraph;
  target: SymbolFocusTarget;
  /** Worktree the declaration is read from; empty means the served root. */
  worktree: string;
  /**
   * Open the whole file a symbol is declared in, at the declaration. The panel
   * shows the declaration itself; this is for the rest of the file around it.
   */
  onOpenFile: (path: string, line?: number) => void;
  onClose: () => void;
}

/**
 * First-level wiring of one symbol, read as blocks rather than drawn as a
 * diagram: callers on the left, dependencies on the right, each grouped into
 * the package the neighbour lives in. Depth comes from walking — clicking a
 * neighbour re-anchors the panel on it — so the view never degrades into the
 * reachable closure.
 *
 * Above both columns sits the symbol's own declaration, because the panel is
 * most often opened from a name in someone else's file: what it is has to be
 * answered before what is around it means anything.
 */
export function SymbolGraphOverlay({ graph, target, worktree, onOpenFile, onClose }: SymbolGraphOverlayProps) {
  const [trail, setTrail] = useState<SymbolFocusTarget[]>([target]);
  const [crossOnly, setCrossOnly] = useState(false);
  const [sourceOpen, setSourceOpen] = useState(true);
  useEffect(() => setTrail([target]), [target]);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const current = trail[trail.length - 1];
  const model = useMemo(() => buildNeighborhood(graph, current), [graph, current]);
  const anchor = model.anchor;
  const definition = useSymbolDefinition(anchor, worktree, graph);
  if (!anchor) return null;

  const walk = (next: SymbolFocusTarget) => setTrail((prev) => [...prev, next]);
  const back = () => setTrail((prev) => (prev.length > 1 ? prev.slice(0, -1) : prev));
  const empty = model.counts.incoming + model.counts.outgoing === 0;

  return (
    <div className="hf-symbol-overlay" onClick={(e) => e.stopPropagation()}>
      <div className="hf-symbol-panel">
        <div className="hf-symbol-head">
          {trail.length > 1 && (
            <button className="hf-symbol-back" onClick={back} title="Back to the previous symbol">
              &lt;
            </button>
          )}
          <div className="hf-symbol-ident">
            <div className="hf-symbol-title">
              <span className={`hf-symbol-kind ${visibilityClass(anchor.exported)}`}>{anchor.kind}</span>
              {anchor.label}
            </div>
            <div className="hf-symbol-subtitle">{anchor.packageName}</div>
          </div>
          <div className="hf-symbol-stats">
            <span className="hf-symbol-stat" title="Direct callers and users">
              {model.counts.incoming} in
            </span>
            <span className="hf-symbol-stat" title="Direct dependencies">
              {model.counts.outgoing} out
            </span>
            <span
              className={`hf-symbol-stat cross ${model.counts.crossPackage > 0 ? 'live' : ''}`}
              title="Relations that leave this package"
            >
              {model.counts.crossPackage} cross-package
            </span>
          </div>
          <button
            className={`hf-symbol-filter ${crossOnly ? 'on' : ''}`}
            onClick={() => setCrossOnly((on) => !on)}
            title="Hide neighbours inside this package"
          >
            cross-package only
          </button>
          <button className="hf-symbol-close" onClick={onClose} title="Close symbol wiring">
            x
          </button>
        </div>

        <Definition
          state={definition}
          anchor={anchor}
          open={sourceOpen}
          onToggle={() => setSourceOpen((on) => !on)}
          onOpenFile={onOpenFile}
        />

        {empty ? (
          <div className="hf-symbol-empty">No first-level relations recorded for this symbol.</div>
        ) : (
          <div className="hf-symbol-body">
            <Column
              direction="in"
              title="Incoming"
              hint="who depends on this"
              groups={visible(model.incoming, crossOnly)}
              total={model.counts.incoming}
              onWalk={walk}
            />
            <Column
              direction="out"
              title="Outgoing"
              hint="what this depends on"
              groups={visible(model.outgoing, crossOnly)}
              total={model.counts.outgoing}
              onWalk={walk}
            />
          </div>
        )}
      </div>
    </div>
  );
}

/** What the panel knows about the anchor's declaration right now. */
type DefinitionState =
  | { status: 'loading' }
  | { status: 'ready'; definition: SymbolDefinition }
  | { status: 'missing' }
  | { status: 'error'; error: string };

/**
 * Read the anchor's declaration from the daemon, one lookup per anchor.
 *
 * Answers are kept for the walk: a trail runs back and forth over the same
 * few symbols, and re-reading a declaration only to redraw the same block is
 * a flash of "reading..." for nothing. They are dropped when the model
 * changes, since a reparse can move a declaration or delete it. Failures are
 * not kept — a daemon that was busy is worth asking again.
 */
function useSymbolDefinition(
  anchor: DefinitionAnchor | null,
  worktree: string,
  graph: UIGraph
): DefinitionState {
  const cache = useRef<{ graph: UIGraph | null; entries: Map<string, DefinitionState> }>({
    graph: null,
    entries: new Map(),
  });
  const [state, setState] = useState<DefinitionState>({ status: 'loading' });
  const id = anchor?.id ?? '';
  const internalId = anchor?.internalId ?? '';
  const memberId = anchor?.memberId;

  useEffect(() => {
    if (cache.current.graph !== graph) cache.current = { graph, entries: new Map() };
    if (!id) return;
    const cached = cache.current.entries.get(id);
    if (cached) {
      setState(cached);
      return;
    }
    let live = true;
    setState({ status: 'loading' });
    const remember = (next: DefinitionState) => {
      cache.current.entries.set(id, next);
      setState(next);
    };
    void (async () => {
      try {
        for (const lookup of definitionLookupIds({ id, internalId, memberId })) {
          const found = await fetchSymbolDefinition(lookup, worktree);
          if (!live) return;
          if (found) {
            remember({ status: 'ready', definition: found });
            return;
          }
        }
        remember({ status: 'missing' });
      } catch (err) {
        if (!live) return;
        setState({ status: 'error', error: err instanceof Error ? err.message : String(err) });
      }
    })();
    return () => {
      live = false;
    };
  }, [id, internalId, memberId, worktree, graph]);

  return state;
}

/**
 * The anchor's declaration: where it is written, its signature, its doc and
 * its own source. The signature stays visible when the source is folded away —
 * it is the one line that says what the symbol is.
 */
function Definition({
  state,
  anchor,
  open,
  onToggle,
  onOpenFile,
}: {
  state: DefinitionState;
  anchor: DefinitionAnchor;
  open: boolean;
  onToggle: () => void;
  onOpenFile: (path: string, line?: number) => void;
}) {
  const definition = state.status === 'ready' ? state.definition : null;
  const lines = useMemo(
    () => (definition?.body ? highlightedLines(definition.file, definition.body) : []),
    [definition?.body, definition?.file]
  );
  const first = Math.max(definition?.line ?? 1, 1);

  return (
    <section className={`hf-symbol-def ${open ? 'open' : 'folded'}`}>
      <div className="hf-symbol-def-head">
        <span className="hf-symbol-def-label">Definition</span>
        {definition && definition.file && (
          <button
            className="hf-symbol-def-open"
            onClick={() => onOpenFile(definition.file, definition.line)}
            title="Open the whole file at this declaration"
          >
            <span className="hf-symbol-def-icon">&lt;&gt;</span>
            {definitionLocation(definition)}
          </button>
        )}
        {definition && isDeclaringTypeFallback(definition, anchor) && (
          <span
            className="hf-symbol-def-tag"
            title="The graph records no node for this member; what is shown is the type that declares it"
          >
            declared in {definition.name}
          </span>
        )}
        <button
          className="hf-symbol-def-toggle"
          onClick={onToggle}
          title={open ? 'Fold the source away' : 'Show the source'}
        >
          {open ? '−' : '+'}
        </button>
      </div>

      {state.status === 'loading' && <div className="hf-symbol-def-state">Reading the declaration...</div>}
      {state.status === 'missing' && (
        <div className="hf-symbol-def-state">No declaration recorded for this symbol.</div>
      )}
      {state.status === 'error' && <div className="hf-symbol-def-state error">{state.error}</div>}

      {definition?.signature && <code className="hf-symbol-def-sig">{definition.signature}</code>}
      {open && definition?.doc && <div className="hf-symbol-def-doc">{definition.doc}</div>}
      {open && lines.length > 0 && (
        <div className="hf-symbol-def-code">
          <table className="hf-symbol-def-table">
            <tbody>
              {lines.map((line, idx) => (
                <tr key={idx}>
                  <td className="hf-symbol-def-no">{first + idx}</td>
                  <td className="hf-symbol-def-src">
                    <code dangerouslySetInnerHTML={{ __html: line || ' ' }} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

function visible(groups: NeighborGroup[], crossOnly: boolean): NeighborGroup[] {
  return crossOnly ? groups.filter((group) => group.crossPackage) : groups;
}

function Column({
  direction,
  title,
  hint,
  groups,
  total,
  onWalk,
}: {
  direction: NeighborDirection;
  title: string;
  hint: string;
  groups: NeighborGroup[];
  total: number;
  onWalk: (target: SymbolFocusTarget) => void;
}) {
  const shown = groups.reduce((sum, group) => sum + group.links.length, 0);
  return (
    <section className={`hf-symbol-col ${direction}`}>
      <div className="hf-symbol-col-head">
        <span className="hf-symbol-col-title">{title}</span>
        <span className="hf-symbol-col-hint">{hint}</span>
        <span className="hf-symbol-col-count">{shown < total ? `${shown} / ${total}` : total}</span>
      </div>
      <div className="hf-symbol-col-body">
        {groups.length === 0 ? (
          <div className="hf-symbol-none">{total === 0 ? 'none' : 'none outside this package'}</div>
        ) : (
          groups.map((group) => <PackageGroup key={group.componentId} group={group} onWalk={onWalk} />)
        )}
      </div>
    </section>
  );
}

function PackageGroup({
  group,
  onWalk,
}: {
  group: NeighborGroup;
  onWalk: (target: SymbolFocusTarget) => void;
}) {
  return (
    <div className={`hf-symbol-group ${group.crossPackage ? 'cross' : 'same'}`}>
      <div className="hf-symbol-group-head" title={group.componentId}>
        <span className="hf-symbol-group-pkg">
          <span className="hf-symbol-group-path">{componentPathPrefix(group.componentId, group.packageName)}</span>
          {group.packageName}
        </span>
        {group.crossPackage && <span className="hf-symbol-group-tag">cross-package</span>}
        <span className="hf-symbol-group-count">{group.links.length}</span>
      </div>
      <ul className="hf-symbol-links">
        {group.links.map((link) => (
          <LinkRow key={link.id} link={link} onWalk={onWalk} />
        ))}
      </ul>
    </div>
  );
}

function LinkRow({ link, onWalk }: { link: NeighborLink; onWalk: (target: SymbolFocusTarget) => void }) {
  const walkable = link.symbol.navigable;
  return (
    <li
      className={`hf-symbol-link ${walkable ? 'walkable' : ''} ${link.diff ? `diff-${link.diff}` : ''}`}
      onClick={walkable ? () => onWalk(toTarget(link.symbol)) : undefined}
      title={walkable ? `Focus ${link.symbol.packageName}.${link.symbol.label}` : 'Outside the loaded graph'}
    >
      <span className="hf-symbol-link-kinds">
        {link.kinds.map((kind) => (
          <span key={kind} className={`hf-symbol-rel ${relClass(kind)}`}>
            {kind}
          </span>
        ))}
      </span>
      <span className={`hf-symbol-kind ${visibilityClass(link.symbol.exported)}`}>{link.symbol.kind}</span>
      <span className="hf-symbol-link-name">{link.symbol.label}</span>
      {link.via.length > 0 && <span className="hf-symbol-link-via">via {link.via.join(', ')}</span>}
    </li>
  );
}

function relClass(kind: string): string {
  if (kind === 'implements') return 'implements';
  if (kind === 'calls') return 'calls';
  if (kind === 'returns') return 'returns';
  return 'uses';
}

function visibilityClass(exported?: boolean): string {
  if (exported === true) return 'symbol-public';
  if (exported === false) return 'symbol-internal';
  return 'symbol-unknown';
}
