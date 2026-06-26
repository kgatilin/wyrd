'use client';

import { useRef, useState } from 'react';
import type { Component as ComponentType, Diff, Internal, Member, Port, SymbolRelation } from '@/lib/graph/types';
import { displaySymbolName } from '@/lib/graph/symbol-names';
import type { CardDensity } from '@/lib/graph/types';

/**
 * Effective diff state of an internal: its own flag if set, otherwise "changed"
 * when any of its members carry a diff.
 */
function deriveInternalDiff(internal: Internal): Diff | undefined {
  if (internal.diff) return internal.diff;
  for (const m of internal.members ?? []) {
    if (m.diff) return 'changed';
  }
  return undefined;
}

/**
 * Effective diff state of a component: its own flag, otherwise "changed" when
 * any internal (derived) or port carries a diff.
 */
function deriveComponentDiff(cmp: ComponentType): Diff | undefined {
  if (cmp.diff) return cmp.diff;
  for (const it of cmp.internals) {
    if (deriveInternalDiff(it)) return 'changed';
  }
  for (const p of cmp.ports) {
    if (p.diff) return 'changed';
  }
  return undefined;
}

function internalKindLabel(kind: Internal['kind']): string {
  switch (kind) {
    case 'iface': return 'iface';
    case 'class': return 'class';
    case 'func': return 'func';
    case 'type': return 'type';
    case 'const': return 'const';
    case 'var': return 'var';
    case 'error': return 'error';
    default: return kind;
  }
}

function memberKindLabel(kind: Member['kind']): string {
  switch (kind) {
    case 'method': return 'fn';
    case 'prop': return ':';
    case 'const': return 'const';
    default: return kind;
  }
}

type PackageLayer = 'internal' | 'public';

function packageLayer(componentId: string): PackageLayer {
  return componentId.split('/').includes('internal') ? 'internal' : 'public';
}

function symbolVisibilityClass(exported?: boolean): string {
  if (exported === true) return 'symbol-public';
  if (exported === false) return 'symbol-internal';
  return 'symbol-unknown';
}

export interface ComponentCardProps {
  cmp: ComponentType;
  expanded: boolean;
  onToggleExpand?: (id: string) => void;
  expandedInternals: ReadonlySet<string>;
  wideInternals: ReadonlySet<string>;
  onToggleWide?: (id: string) => void;
  onToggleInternalExpand?: (id: string) => void;
  parentName?: string;
  showDiff: boolean;
  onSelect?: (cmp: ComponentType) => void;
  focused?: boolean;
  dimmed?: boolean;
  pinned?: boolean;
  cardDensity?: CardDensity;
  showInlineSignatures?: boolean;
  zoom?: number;
  onMove?: (id: string, x: number, y: number) => void;
  relations?: SymbolRelation[];
}

export function ComponentCard({
  cmp,
  expanded,
  onToggleExpand,
  expandedInternals,
  wideInternals,
  onToggleWide,
  onToggleInternalExpand,
  parentName,
  showDiff,
  onSelect,
  focused = false,
  dimmed = false,
  pinned = false,
  cardDensity = 'detailed',
  showInlineSignatures = true,
  zoom = 1,
  onMove,
  relations = [],
}: ComponentCardProps) {
  const [dragging, setDragging] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const dragRef = useRef<{
    pointerId: number;
    startClientX: number;
    startClientY: number;
    startX: number;
    startY: number;
    dragging: boolean;
  } | null>(null);
  const suppressClickRef = useRef(false);
  const effectiveDiff = deriveComponentDiff(cmp);
  const diffCls = showDiff && effectiveDiff ? effectiveDiff : '';
  const w = cmp.w;
  const h = cmp.h;

  const hasInternals = cmp.internals.length > 0;
  const layer = packageLayer(cmp.id);
  const showFitControls = !showInlineSignatures;
  const allWide = hasInternals && cmp.internals.every((it) => wideInternals.has(it.id));

  const parentInitial = (parentName || cmp.name).charAt(0).toUpperCase();

  const consumeSuppressedClick = (e: React.MouseEvent) => {
    if (!suppressClickRef.current) return false;
    suppressClickRef.current = false;
    e.stopPropagation();
    e.preventDefault();
    return true;
  };

  const handleClick = (e: React.MouseEvent) => {
    if (consumeSuppressedClick(e)) return;
    e.stopPropagation();
    onSelect?.(cmp);
  };

  const handleHeadClick = (e: React.MouseEvent) => {
    if (consumeSuppressedClick(e)) return;
    e.stopPropagation();
    onSelect?.(cmp);
  };

  const handleDragPointerDown = (e: React.PointerEvent) => {
    if (!onMove || e.button !== 0 || e.shiftKey || cmp.x == null || cmp.y == null) return;
    e.stopPropagation();
    dragRef.current = {
      pointerId: e.pointerId,
      startClientX: e.clientX,
      startClientY: e.clientY,
      startX: cmp.x,
      startY: cmp.y,
      dragging: false,
    };
    rootRef.current?.setPointerCapture(e.pointerId);
  };

  const handlePointerMove = (e: React.PointerEvent) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== e.pointerId) return;
    const screenDx = e.clientX - drag.startClientX;
    const screenDy = e.clientY - drag.startClientY;
    if (!drag.dragging && Math.hypot(screenDx, screenDy) < 4) return;
    if (!drag.dragging) {
      drag.dragging = true;
      setDragging(true);
    }
    e.preventDefault();
    const scale = zoom > 0 ? zoom : 1;
    onMove?.(
      cmp.id,
      Math.max(0, drag.startX + screenDx / scale),
      Math.max(0, drag.startY + screenDy / scale)
    );
  };

  const handlePointerEnd = (e: React.PointerEvent) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== e.pointerId) return;
    if (drag.dragging) suppressClickRef.current = true;
    dragRef.current = null;
    setDragging(false);
    rootRef.current?.releasePointerCapture(e.pointerId);
  };

  const handleExpandClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onToggleExpand?.(cmp.id);
  };

  const handleExpandAllClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    // Toggle all internals wide
    if (allWide) {
      cmp.internals.forEach((it) => onToggleWide?.(it.id));
    } else {
      cmp.internals.filter((it) => !wideInternals.has(it.id)).forEach((it) => onToggleWide?.(it.id));
    }
  };

  return (
    <div
      ref={rootRef}
      className={`hf-cmp ${cardDensity} ${expanded ? 'expanded' : 'collapsed'} layer-${layer} ${diffCls} ${focused ? 'focused' : ''} ${dimmed ? 'dimmed' : ''} ${pinned ? 'pinned' : ''} ${dragging ? 'dragging' : ''}`}
      style={{
        left: cmp.x,
        top: cmp.y,
        width: w,
        height: h,
      }}
      onClick={handleClick}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerEnd}
      onPointerCancel={handlePointerEnd}
    >
      <div className="hf-cmp-inner">
        <div
          className="hf-cmp-head"
          style={{ paddingRight: expanded ? 92 : 34 }}
          onClick={handleHeadClick}
          onPointerDown={handleDragPointerDown}
        >
          <div className="hf-cmp-icon">{parentInitial}</div>
          <div className="hf-cmp-name">{cmp.name}</div>
          <span className="hf-cmp-tech">{cmp.tech}</span>
          <span className={`hf-cmp-layer ${layer}`} title={`${layer} package`}>{layer}</span>
        </div>

        {!expanded && <div className="hf-cmp-desc">{cmp.desc}</div>}

        {expanded && (
          <div className="hf-cmp-canvas">
            {cmp.internals.map((internal) => (
              <InternalCard
                key={internal.id}
                internal={internal}
                showDiff={showDiff}
                expanded={expandedInternals.has(internal.id)}
                wide={wideInternals.has(internal.id)}
                onToggleWide={() => onToggleWide?.(internal.id)}
                onToggleExpand={() => onToggleInternalExpand?.(internal.id)}
                showInlineSignatures={showInlineSignatures}
                showFitControl={showFitControls}
                componentId={cmp.id}
              />
            ))}
            <IntraPackageRelations
              cmp={cmp}
              relations={relations}
              showDiff={showDiff}
            />
          </div>
        )}
      </div>

      <div className="hf-cmp-actions">
        {cmp.desc && expanded && (
          <div className="hf-cmp-info">
            <span className="hf-cmp-info-icon">i</span>
            <div className="hf-cmp-info-pop">{cmp.desc}</div>
          </div>
        )}
        {expanded && hasInternals && showFitControls && (
          <button
            className="hf-cmp-expand-all"
            onClick={handleExpandAllClick}
            title={allWide ? 'Reset all blocks width' : 'Expand all blocks to fit text'}
          >
            {allWide ? '>><' : '<<>'}
          </button>
        )}
        <button className="hf-cmp-expand" onClick={handleExpandClick}>
          {expanded ? '-' : '+'}
        </button>
      </div>

      {cmp.ports.map((port) => (
        <PortDot
          key={port.id}
          port={port}
          showDiff={showDiff}
        />
      ))}
    </div>
  );
}

interface IntraPackageRelationsProps {
  cmp: ComponentType;
  relations: SymbolRelation[];
  showDiff: boolean;
}

interface RelationPoint {
  x: number;
  y: number;
  side: 'top' | 'right' | 'bottom' | 'left';
}

function IntraPackageRelations({
  cmp,
  relations,
  showDiff,
}: IntraPackageRelationsProps) {
  const visibleRelations = internalRenderRelations(cmp.id, relations, cmp.internals);
  if (visibleRelations.length === 0 || cmp.w == null || cmp.h == null) return null;
  const width = cmp.w;
  const height = Math.max(0, cmp.h - 36);
  if (width <= 0 || height <= 0) return null;

  return (
    <svg className="hf-intra-relations" width={width} height={height} aria-hidden="true">
      <defs>
        {['intra', 'intra-add', 'intra-rem', 'intra-chg'].map((id) => (
          <marker
            key={id}
            id={`hf-${id}-${safeMarkerId(cmp.id)}`}
            viewBox="0 0 10 10"
            refX="9"
            refY="5"
            markerWidth="7"
            markerHeight="7"
            orient="auto-start-reverse"
          >
            <path
              d="M 0 0 L 10 5 L 0 10 z"
              className={`hf-intra-arrow ${
                id === 'intra-add'
                  ? 'added'
                  : id === 'intra-rem'
                    ? 'removed'
                    : id === 'intra-chg'
                      ? 'changed'
                      : ''
              }`}
            />
          </marker>
        ))}
      </defs>
      {visibleRelations.map((relation, idx) => {
        const endpoints = intraRelationEndpoints(cmp, relation);
        if (!endpoints) return null;
        const { from, to } = endpoints;
        const { path, label } = intraRelationPath(from, to, idx);
        const diffCls = showDiff && relation.diff ? relation.diff : '';
        const marker = intraRelationMarker(cmp.id, relation, showDiff);
        return (
          <g key={relation.id} className="hf-intra-rel-group">
            <path d={path} className={`hf-intra-rel ${diffCls}`} markerEnd={marker} />
            <text x={label.x} y={label.y} className="hf-intra-label" textAnchor="middle">
              {relation.kind}
            </text>
          </g>
        );
      })}
    </svg>
  );
}

function internalRenderRelations(componentId: string, relations: SymbolRelation[], internals: Internal[]): SymbolRelation[] {
  const internalIds = new Set(internals.map((internal) => internal.id));
  const out = new Map<string, SymbolRelation>();
  for (const relation of relations) {
    if (relation.fromComponentId !== componentId || relation.toComponentId !== componentId) continue;
    if (!relation.fromInternalId || !relation.toInternalId) continue;
    if (relation.fromInternalId === relation.toInternalId) continue;
    if (!internalIds.has(relation.fromInternalId) || !internalIds.has(relation.toInternalId)) continue;
    const key = `${relation.kind} ${relation.fromInternalId} ${relation.toInternalId}`;
    if (!out.has(key)) out.set(key, relation);
  }
  return [...out.values()].sort((a, b) => a.id.localeCompare(b.id));
}

function intraRelationMarker(componentId: string, relation: SymbolRelation, showDiff: boolean): string {
  const suffix = safeMarkerId(componentId);
  if (!showDiff || !relation.diff) return `url(#hf-intra-${suffix})`;
  if (relation.diff === 'added') return `url(#hf-intra-add-${suffix})`;
  if (relation.diff === 'removed') return `url(#hf-intra-rem-${suffix})`;
  return `url(#hf-intra-chg-${suffix})`;
}

function safeMarkerId(id: string): string {
  return id.replace(/[^a-zA-Z0-9_-]/g, '-');
}

function intraRelationEndpoints(
  cmp: ComponentType,
  relation: SymbolRelation
): { from: RelationPoint; to: RelationPoint } | null {
  const fromInternal = cmp.internals.find((internal) => internal.id === relation.fromInternalId);
  const toInternal = cmp.internals.find((internal) => internal.id === relation.toInternalId);
  if (!fromInternal || !toInternal) return null;
  return intraAnchors(fromInternal, toInternal);
}

function intraAnchors(fromInternal: Internal, toInternal: Internal): { from: RelationPoint; to: RelationPoint } {
  const from = internalBox(fromInternal);
  const to = internalBox(toInternal);
  const dx = to.cx - from.cx;
  const dy = to.cy - from.cy;

  if (Math.abs(dy) >= Math.abs(dx) * 0.55) {
    if (dy >= 0) {
      return {
        from: { x: from.cx, y: from.y + from.h, side: 'bottom' },
        to: { x: to.cx, y: to.y, side: 'top' },
      };
    }
    return {
      from: { x: from.cx, y: from.y, side: 'top' },
      to: { x: to.cx, y: to.y + to.h, side: 'bottom' },
    };
  }

  if (dx >= 0) {
    return {
      from: { x: from.x + from.w, y: from.cy, side: 'right' },
      to: { x: to.x, y: to.cy, side: 'left' },
    };
  }
  return {
    from: { x: from.x, y: from.cy, side: 'left' },
    to: { x: to.x + to.w, y: to.cy, side: 'right' },
  };
}

function internalBox(internal: Internal): { x: number; y: number; w: number; h: number; cx: number; cy: number } {
  const x = internal.x ?? 0;
  const y = internal.y ?? 0;
  const w = internal.w ?? 180;
  const h = internal.h ?? 26;
  return { x, y, w, h, cx: x + w / 2, cy: y + h / 2 };
}

function intraRelationPath(from: RelationPoint, to: RelationPoint, index: number): { path: string; label: { x: number; y: number } } {
  const vertical = (from.side === 'top' || from.side === 'bottom') && (to.side === 'top' || to.side === 'bottom');
  if (vertical) {
    const sign = from.side === 'bottom' ? 1 : -1;
    const dy = Math.max(54, Math.abs(to.y - from.y) * 0.42);
    return {
      path: `M ${from.x} ${from.y} C ${from.x} ${from.y + sign * dy}, ${to.x} ${to.y - sign * dy}, ${to.x} ${to.y}`,
      label: { x: (from.x + to.x) / 2, y: (from.y + to.y) / 2 - 8 - (index % 2) * 8 },
    };
  }

  const dx = Math.max(46, Math.abs(to.x - from.x) * 0.34);
  const fromDir = from.side === 'right' ? 1 : -1;
  const toDir = to.side === 'right' ? 1 : -1;
  const c1x = from.x + fromDir * dx;
  const c2x = to.x + toDir * dx;
  return {
    path: `M ${from.x} ${from.y} C ${c1x} ${from.y}, ${c2x} ${to.y}, ${to.x} ${to.y}`,
    label: { x: (from.x + to.x) / 2, y: (from.y + to.y) / 2 - 8 - (index % 2) * 10 },
  };
}

interface InternalCardProps {
  internal: Internal;
  componentId: string;
  showDiff: boolean;
  expanded: boolean;
  wide: boolean;
  onToggleWide: () => void;
  onToggleExpand?: () => void;
  showInlineSignatures: boolean;
  showFitControl: boolean;
}

function InternalCard({
  internal,
  showDiff,
  expanded,
  wide,
  onToggleWide,
  onToggleExpand,
  showInlineSignatures,
  showFitControl,
}: InternalCardProps) {
  const diffCls = showDiff && deriveInternalDiff(internal) ? deriveInternalDiff(internal) : '';
  const internalName = displaySymbolName(internal.name, showInlineSignatures);
  const memberHeight = expanded ? (internal.members?.length ?? 0) * 18 + 4 : 0;
  const h = internal.h ?? (26 + memberHeight);

  const handleToggleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onToggleWide();
  };

  const handleHeadClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onToggleExpand?.();
  };

  return (
    <div
      className={`hf-internal ${internal.kind} ${symbolVisibilityClass(internal.exported)} ${diffCls}`}
      style={{
        left: internal.x,
        top: internal.y,
        width: internal.w,
        height: h,
      }}
    >
      <div className="hf-internal-head" onClick={handleHeadClick}>
        <span className="hf-internal-kind">
          {internalKindLabel(internal.kind)}
        </span>
        <span className="hf-internal-name" title={internal.name}>{internalName}</span>
        {showFitControl && (
          <span
            className="hf-internal-toggle"
            onClick={handleToggleClick}
            title={wide ? 'Reset width' : 'Fit width to member text'}
          >
            {wide ? '-' : '+'}
          </span>
        )}
      </div>
      {expanded && (
        <div className="hf-member-list">
          {(internal.members ?? []).map((member) => (
            <MemberRow
              key={member.id}
              member={member}
              showDiff={showDiff}
              showInlineSignatures={showInlineSignatures}
            />
          ))}
        </div>
      )}
    </div>
  );
}

interface MemberRowProps {
  member: Member;
  showDiff: boolean;
  showInlineSignatures: boolean;
}

function MemberRow({ member, showDiff, showInlineSignatures }: MemberRowProps) {
  const diffCls = showDiff && member.diff ? member.diff : '';
  const memberName = displaySymbolName(member.name, showInlineSignatures);

  return (
    <div className={`hf-member ${symbolVisibilityClass(member.exported)} ${diffCls}`} title={member.name}>
      <span className={`hf-member-kind ${member.kind === 'method' ? 'fn' : member.kind}`}>
        {memberKindLabel(member.kind)}
      </span>
      <span className="hf-member-name">{memberName}</span>
    </div>
  );
}

interface PortDotProps {
  port: Port;
  showDiff: boolean;
}

function PortDot({
  port,
  showDiff,
}: PortDotProps) {
  const diffCls = showDiff && port.diff ? port.diff : '';
  const portY = port.y ?? 58;
  const py = portY - 7;

  return (
    <div
      className={`hf-port ${port.side} ${diffCls}`}
      style={{ top: py }}
    >
      <span className="hf-port-dot" />
      <span className="hf-port-label">{port.name}</span>
    </div>
  );
}
