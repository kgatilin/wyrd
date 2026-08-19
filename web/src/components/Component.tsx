import { useRef, useState } from 'react';
import type { Component as ComponentType, Diff, Internal, Port, SymbolRelation } from '../types';
import { componentPathPrefix } from '../domain/componentPath';
import { isDiagramRelation, rowLabel, rowText, type CardBlock, type CardFile, type CardRow } from '../domain/cardModel';
import { blockRect } from '../domain/cardAnchors';
import { CARD_LAYOUT_METRICS } from '../layout/cardLayout';
import { CardSequence } from './SequenceCanvas';
import type { CardDensity } from '../domain/state';
import type { SymbolFocusTarget } from '../domain/symbolFocus';
import { sourceFilePath } from '../domain/sourcePath';

/**
 * Effective diff state of an internal: its own flag if set, otherwise "changed"
 * when any of its members carry a diff. Lets a block whose members were
 * added/removed read as changed even when the source didn't flag the block.
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

/** Short tag shown on a class shape's header. */
function blockKindLabel(kind: CardBlock['kind']): string {
  switch (kind) {
    case 'iface':
      return 'iface';
    case 'class':
      return 'struct';
    case 'func':
      return 'func';
    case 'type':
      return 'type';
    case 'consts':
      return 'const';
    case 'vars':
      return 'var';
    case 'errors':
      return 'error';
    default:
      return kind;
  }
}

/** Leading glyph of a body row, mirroring the kind column of a UML class. */
function rowKindLabel(kind: CardRow['kind']): string {
  switch (kind) {
    case 'method':
      return 'fn';
    case 'prop':
      return ':';
    case 'param':
      return '→';
    case 'return':
      return '←';
    case 'const':
      return '=';
    case 'type':
      return 'T';
    default:
      return '';
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

export interface ComponentProps {
  /** The component data with layout geometry */
  cmp: ComponentType;
  /** Whether this component is expanded */
  expanded: boolean;
  /** Callback to toggle expansion */
  onToggleExpand?: (id: string) => void;
  /** Expanded card shows its call-sequence instead of its file containers */
  seqActive?: boolean;
  /** Callback to flip the expanded card between contents and call-sequence */
  onToggleSeq?: (id: string) => void;
  /** Display name of the parent (bounded context); drives the header icon letter */
  parentName?: string;
  /** Whether to show diff styling */
  showDiff: boolean;

  /** Callback when component is selected (for focus mode) */
  onSelect?: (cmp: ComponentType) => void;
  /** Whether this component is focused */
  focused?: boolean;
  /** Whether this component is dimmed (not related to focused) */
  dimmed?: boolean;
  /** Callback to add a comment */
  onAddComment?: (target: { type: string; id: string }, event: React.MouseEvent) => void;
  /** Set of IDs that have comments */
  commentTargets?: Set<string>;
  /** Whether this component has a manually pinned layout position */
  pinned?: boolean;
  /** Collapsed-card presentation density; 'compact' also hides class bodies */
  cardDensity?: CardDensity;
  /** Whether class bodies show the right-hand type column */
  showTypes?: boolean;
  /** Canvas zoom used to convert screen-pixel drag deltas to graph coordinates */
  zoom?: number;
  /** Callback when the component is manually moved on the canvas */
  onMove?: (id: string, x: number, y: number) => void;
  /** Callback to clear this component's manually pinned layout position */
  onResetLayout?: (id: string) => void;
  /** Same-package symbol relations rendered inside the expanded component */
  relations?: SymbolRelation[];
  /** Opens the symbol wiring graph for a function/type/method. */
  onSymbolFocus?: (target: SymbolFocusTarget) => void;
  /** Opens the file diff at one of the card's source files. */
  onOpenFileDiff?: (path: string) => void;
}

/**
 * Renders a package card: header, ports, and — when expanded — the package's
 * source-file containers, each holding class shapes with two-column bodies.
 */
export function Component({
  cmp,
  expanded,
  onToggleExpand,
  seqActive = false,
  onToggleSeq,
  parentName,
  showDiff,
  onSelect,
  focused = false,
  dimmed = false,
  onAddComment,
  commentTargets,
  pinned = false,
  cardDensity = 'detailed',
  showTypes = true,
  zoom = 1,
  onMove,
  onResetLayout,
  relations = [],
  onSymbolFocus,
  onOpenFileDiff,
}: ComponentProps) {
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
  // Layout computes both collapsed and expanded dimensions in cmp.w/h
  const w = cmp.w;
  const h = cmp.h;

  const layer = packageLayer(cmp.id);
  const files = cmp.files ?? [];

  // Header icon shows the parent's (bounded context) initial, falling back to the
  // component's own first letter when no parent name is supplied.
  const parentInitial = (parentName || cmp.name).charAt(0).toUpperCase();
  const pathPrefix = componentPathPrefix(cmp.id, cmp.name);

  const hasComment = (id: string) => commentTargets?.has(id) ?? false;

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
    if (e.shiftKey) {
      onAddComment?.({ type: 'cmp', id: cmp.id }, e);
      return;
    }
    onSelect?.(cmp);
  };

  const handleHeadDoubleClick = (e: React.MouseEvent) => {
    if (consumeSuppressedClick(e)) return;
    e.stopPropagation();
    onAddComment?.({ type: 'cmp', id: cmp.id }, e);
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

  const handleSeqClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onToggleSeq?.(cmp.id);
  };

  const handleResetLayoutClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onResetLayout?.(cmp.id);
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
      {/* Clipped content layer: header + body are rounded-corner clipped here,
          while ports (below) live outside this layer so their dots/labels are
          never cut off by the card's overflow. */}
      <div className="hf-cmp-inner">
        {/* Header */}
        <div
          className="hf-cmp-head"
          style={{ paddingRight: expanded ? 92 : 34 }}
          onClick={handleHeadClick}
          onDoubleClick={handleHeadDoubleClick}
          onPointerDown={handleDragPointerDown}
        >
          <div className="hf-cmp-icon">{parentInitial}</div>
          <div className="hf-cmp-name" title={cmp.id}>
            {pathPrefix && <span className="hf-cmp-path">{pathPrefix}</span>}
            <span className="hf-cmp-base">{cmp.name}</span>
          </div>
          <span className="hf-cmp-tech">{cmp.tech}</span>
          <span className={`hf-cmp-layer ${layer}`} title={`${layer} package`}>{layer}</span>
        </div>

        {/* Description (collapsed only) */}
        {!expanded && <div className="hf-cmp-desc">{cmp.desc}</div>}

        {/* Call-sequence body (expanded, seq mode) */}
        {expanded && seqActive && (
          <div className="hf-cmp-seqbody" onClick={(e) => e.stopPropagation()}>
            <CardSequence pkg={cmp.id} />
          </div>
        )}

        {/* Source-file containers (expanded only) */}
        {expanded && !seqActive && (
          <div className="hf-cmp-canvas">
            {files.map((file) => (
              <FileContainer
                key={file.id}
                file={file}
                componentId={cmp.id}
                showDiff={showDiff}
                showTypes={showTypes}
                onAddComment={onAddComment}
                hasComment={hasComment}
                onSymbolFocus={onSymbolFocus}
                onOpenFileDiff={onOpenFileDiff}
              />
            ))}
            <IntraPackageRelations cmp={cmp} relations={relations} showDiff={showDiff} />
          </div>
        )}
      </div>

      {/* Floating action group — kept OUTSIDE .hf-cmp-inner so the (i) popover
          escapes the card's overflow clipping. */}
      <div className="hf-cmp-actions">
        {/* Description info button — only when expanded (collapsed cards show
            the description in the body); its popover opens above the button. */}
        {cmp.desc && expanded && (
          <div className="hf-cmp-info">
            <span className="hf-cmp-info-icon">i</span>
            <div className="hf-cmp-info-pop">{cmp.desc}</div>
          </div>
        )}
        {/* Deps|sequence toggle: flips the expanded card body between its
            contents and the package's call-sequence, in place. */}
        {expanded && onToggleSeq && (
          <button
            className={`hf-cmp-seq-toggle${seqActive ? ' on' : ''}`}
            onClick={handleSeqClick}
            title={seqActive ? 'Show contents' : 'Show call sequence'}
          >
            ⇄
          </button>
        )}
        {pinned && onResetLayout && (
          <button
            className="hf-cmp-reset-layout"
            onClick={handleResetLayoutClick}
            title="Reset this package layout"
          >
            ↺
          </button>
        )}
        <button className="hf-cmp-expand" onClick={handleExpandClick}>
          {expanded ? '−' : '+'}
        </button>
      </div>

      {/* Ports — rendered outside .hf-cmp-inner so they are not clipped */}
      {cmp.ports.map((port) => (
        <PortDot
          key={port.id}
          port={port}
          showDiff={showDiff}
          hasComment={hasComment(port.id)}
          onAddComment={onAddComment}
        />
      ))}

      {/* Comment pin indicator */}
      {hasComment(cmp.id) && <span className="hf-cmt-pin">!</span>}
    </div>
  );
}

interface FileContainerProps {
  file: CardFile;
  componentId: string;
  showDiff: boolean;
  showTypes: boolean;
  onAddComment?: (target: { type: string; id: string }, event: React.MouseEvent) => void;
  hasComment: (id: string) => boolean;
  onSymbolFocus?: (target: SymbolFocusTarget) => void;
  onOpenFileDiff?: (path: string) => void;
}

/** One source file of the package, holding its class shapes. */
function FileContainer({
  file,
  componentId,
  showDiff,
  showTypes,
  onAddComment,
  hasComment,
  onSymbolFocus,
  onOpenFileDiff,
}: FileContainerProps) {
  const diffCls = showDiff && file.diff ? file.diff : '';
  // The card says which symbols changed; the patch says what the change was.
  // Offered on every file, not only the ones the projection flagged: a change
  // that touches no signature (a call moved, a body rewritten) leaves the card
  // unmarked and is exactly the case a reviewer needs the text for.
  const diffPath = sourceFilePath(componentId, file.path);
  return (
    <div
      className={`hf-file ${diffCls}`}
      style={{ left: file.x, top: file.y, width: file.w, height: file.h }}
    >
      <div className="hf-file-head" title={file.label}>
        <span className="hf-file-name">{file.label}</span>
        {onOpenFileDiff && diffPath && (
          <button
            className="hf-file-diff"
            title={`Open ${diffPath} in the file diff`}
            onClick={(e) => {
              e.stopPropagation();
              onOpenFileDiff(diffPath);
            }}
          >
            &plusmn;
          </button>
        )}
      </div>
      {file.blocks.map((block) => (
        <ClassBlock
          key={block.id}
          block={block}
          componentId={componentId}
          showDiff={showDiff}
          showTypes={showTypes}
          onAddComment={onAddComment}
          hasComment={hasComment}
          onSymbolFocus={onSymbolFocus}
        />
      ))}
    </div>
  );
}

interface ClassBlockProps {
  block: CardBlock;
  componentId: string;
  showDiff: boolean;
  showTypes: boolean;
  onAddComment?: (target: { type: string; id: string }, event: React.MouseEvent) => void;
  hasComment: (id: string) => boolean;
  onSymbolFocus?: (target: SymbolFocusTarget) => void;
}

/**
 * A class shape: header with kind tag, name and stereotype chip, then the
 * two-column body. Bodies are rendered whenever the layout reserved room for
 * them — the card shows a symbol's structure without a further click.
 */
function ClassBlock({
  block,
  componentId,
  showDiff,
  showTypes,
  onAddComment,
  hasComment,
  onSymbolFocus,
}: ClassBlockProps) {
  const diffCls = showDiff && block.diff ? block.diff : '';
  // The layout reserves height for rows; when it did not, bodies are hidden.
  const showRows = (block.h ?? 0) > CARD_LAYOUT_METRICS.BLOCK_HEADER_H && block.rows.length > 0;

  const handleHeadClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (block.internalId) onSymbolFocus?.({ componentId, internalId: block.internalId });
  };

  const handleHeadDoubleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onAddComment?.({ type: 'internal', id: block.internalId ?? block.id }, e);
  };

  return (
    <div
      className={`hf-block ${block.kind} ${symbolVisibilityClass(block.exported)} ${diffCls}`}
      style={{ left: block.x, top: block.y, width: block.w, height: block.h }}
    >
      <div className="hf-block-head" onClick={handleHeadClick} onDoubleClick={handleHeadDoubleClick}>
        <span className="hf-block-kind">{blockKindLabel(block.kind)}</span>
        <span className="hf-block-name" title={block.name}>
          {block.name}
        </span>
        {block.stereotype && (
          <span className="hf-block-stereo" title={`stereotype: ${block.stereotype}`}>
            {block.stereotype}
          </span>
        )}
        {block.internalId && hasComment(block.internalId) && <span className="hf-cmt-marker sm">!</span>}
      </div>
      {showRows && (
        <div className="hf-block-rows">
          {block.rows.map((row) => (
            <BodyRow
              key={row.id}
              row={row}
              componentId={componentId}
              showDiff={showDiff}
              showTypes={showTypes}
              hasComment={hasComment(row.memberId ?? row.id)}
              onAddComment={onAddComment}
              onSymbolFocus={onSymbolFocus}
            />
          ))}
        </div>
      )}
    </div>
  );
}

interface BodyRowProps {
  row: CardRow;
  componentId: string;
  showDiff: boolean;
  showTypes: boolean;
  hasComment: boolean;
  onAddComment?: (target: { type: string; id: string }, event: React.MouseEvent) => void;
  onSymbolFocus?: (target: SymbolFocusTarget) => void;
}

/** One row of a class body: kind glyph, name (with parameters), type column. */
function BodyRow({
  row,
  componentId,
  showDiff,
  showTypes,
  hasComment,
  onAddComment,
  onSymbolFocus,
}: BodyRowProps) {
  const diffCls = showDiff && row.diff ? row.diff : '';

  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onSymbolFocus?.({ componentId, internalId: row.internalId, memberId: row.memberId });
    onAddComment?.({ type: row.memberId ? 'member' : 'internal', id: row.memberId ?? row.internalId }, e);
  };

  return (
    <div
      className={`hf-row ${row.kind} ${symbolVisibilityClass(row.exported)} ${diffCls}`}
      onClick={handleClick}
      title={rowText(row)}
    >
      <span className={`hf-row-kind ${row.kind}`}>{rowKindLabel(row.kind)}</span>
      <span className="hf-row-name">{rowLabel(row)}</span>
      {showTypes && row.type && <span className="hf-row-type">{row.type}</span>}
      {hasComment && <span className="hf-cmt-marker sm">!</span>}
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

function IntraPackageRelations({ cmp, relations, showDiff }: IntraPackageRelationsProps) {
  const visibleRelations = internalRenderRelations(cmp, relations);
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

/**
 * Same-package relations that connect two *different* class shapes. Symbols
 * folded into the same aggregate block (constants of one file, say) have no
 * arrow to draw between them.
 */
function internalRenderRelations(cmp: ComponentType, relations: SymbolRelation[]): SymbolRelation[] {
  const out = new Map<string, SymbolRelation>();
  for (const relation of relations) {
    if (relation.fromComponentId !== cmp.id || relation.toComponentId !== cmp.id) continue;
    if (!isDiagramRelation(relation)) continue;
    if (!relation.fromInternalId || !relation.toInternalId) continue;
    if (relation.fromInternalId === relation.toInternalId) continue;
    const from = blockRect(cmp, relation.fromInternalId);
    const to = blockRect(cmp, relation.toInternalId);
    if (!from || !to) continue;
    if (from.x === to.x && from.y === to.y) continue;
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
  const from = blockRect(cmp, relation.fromInternalId!);
  const to = blockRect(cmp, relation.toInternalId!);
  if (!from || !to) return null;
  return intraAnchors(from, to);
}

function intraAnchors(
  from: { x: number; y: number; w: number; h: number },
  to: { x: number; y: number; w: number; h: number }
): { from: RelationPoint; to: RelationPoint } {
  const fromC = { cx: from.x + from.w / 2, cy: from.y + from.h / 2 };
  const toC = { cx: to.x + to.w / 2, cy: to.y + to.h / 2 };
  const dx = toC.cx - fromC.cx;
  const dy = toC.cy - fromC.cy;

  if (Math.abs(dy) >= Math.abs(dx) * 0.55) {
    if (dy >= 0) {
      return {
        from: { x: fromC.cx, y: from.y + from.h, side: 'bottom' },
        to: { x: toC.cx, y: to.y, side: 'top' },
      };
    }
    return {
      from: { x: fromC.cx, y: from.y, side: 'top' },
      to: { x: toC.cx, y: to.y + to.h, side: 'bottom' },
    };
  }

  if (dx >= 0) {
    return {
      from: { x: from.x + from.w, y: fromC.cy, side: 'right' },
      to: { x: to.x, y: toC.cy, side: 'left' },
    };
  }
  return {
    from: { x: from.x, y: fromC.cy, side: 'left' },
    to: { x: to.x + to.w, y: toC.cy, side: 'right' },
  };
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

interface PortDotProps {
  port: Port;
  showDiff: boolean;
  hasComment: boolean;
  onAddComment?: (target: { type: string; id: string }, event: React.MouseEvent) => void;
}

function PortDot({
  port,
  showDiff,
  hasComment,
  onAddComment,
}: PortDotProps) {
  const diffCls = showDiff && port.diff ? port.diff : '';

  // Use ELK-computed port.y directly. The .hf-port row is 14px tall and centers
  // its dot, so anchor the row at port.y - 7 to put the dot's center on port.y.
  const portY = port.y ?? 58;
  const py = portY - 7;

  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onAddComment?.({ type: 'port', id: port.id }, e);
  };

  return (
    <div
      className={`hf-port ${port.side} ${diffCls}`}
      style={{ top: py }}
      onClick={handleClick}
    >
      <span className="hf-port-dot" />
      <span className="hf-port-label">
        {port.name}
        {hasComment && <span className="hf-cmt-marker sm">!</span>}
      </span>
    </div>
  );
}
