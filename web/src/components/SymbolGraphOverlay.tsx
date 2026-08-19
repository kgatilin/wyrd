import { useEffect, useMemo, useState } from 'react';
import type { UIGraph } from '../types';
import type { SymbolFocusTarget } from '../domain/symbolFocus';
import { componentPathPrefix } from '../domain/componentPath';
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
  onClose: () => void;
}

/**
 * First-level wiring of one symbol, read as blocks rather than drawn as a
 * diagram: callers on the left, dependencies on the right, each grouped into
 * the package the neighbour lives in. Depth comes from walking — clicking a
 * neighbour re-anchors the panel on it — so the view never degrades into the
 * reachable closure.
 */
export function SymbolGraphOverlay({ graph, target, onClose }: SymbolGraphOverlayProps) {
  const [trail, setTrail] = useState<SymbolFocusTarget[]>([target]);
  const [crossOnly, setCrossOnly] = useState(false);
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
