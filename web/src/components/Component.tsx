import type { Component as ComponentType, Diff, Internal, Member, Port } from '../types';

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

export interface ComponentProps {
  /** The component data with layout geometry */
  cmp: ComponentType;
  /** Whether this component is expanded */
  expanded: boolean;
  /** Callback to toggle expansion */
  onToggleExpand?: (id: string) => void;
  /** Set of expanded internal IDs (members visible) */
  expandedInternals: Set<string>;
  /** Set of internal IDs in fit-width mode (stretched to show all member text) */
  wideInternals: Set<string>;
  /** Callback to toggle an internal's fit-width mode */
  onToggleWide?: (id: string) => void;
  /** Callback to set ALL internals of this component to/from fit-width mode */
  onSetAllWide?: (id: string, wide: boolean) => void;
  /** Display name of the parent (bounded context); drives the header icon letter */
  parentName?: string;
  /** Whether to show diff styling */
  showDiff: boolean;

  // Phase D props - stub/no-op for now
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
}

/**
 * Renders a component card with header, ports, and internals mini-canvas.
 * Supports collapsed (shows desc) and expanded (shows internals) states.
 */
export function Component({
  cmp,
  expanded,
  onToggleExpand,
  expandedInternals,
  wideInternals,
  onToggleWide,
  onSetAllWide,
  parentName,
  showDiff,
  onSelect,
  focused = false,
  dimmed = false,
  onAddComment,
  commentTargets,
}: ComponentProps) {
  const effectiveDiff = deriveComponentDiff(cmp);
  const diffCls = showDiff && effectiveDiff ? effectiveDiff : '';
  // Layout computes both collapsed and expanded dimensions in cmp.w/h
  const w = cmp.w;
  const h = cmp.h;

  // "Expand all" is satisfied when every internal is already in fit-width mode.
  const hasInternals = cmp.internals.length > 0;
  const allWide = hasInternals && cmp.internals.every((it) => wideInternals.has(it.id));

  // Header icon shows the parent's (bounded context) initial, falling back to the
  // component's own first letter when no parent name is supplied.
  const parentInitial = (parentName || cmp.name).charAt(0).toUpperCase();

  const hasComment = (id: string) => commentTargets?.has(id) ?? false;

  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onSelect?.(cmp);
  };

  const handleHeadClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (e.shiftKey) {
      onAddComment?.({ type: 'cmp', id: cmp.id }, e);
      return;
    }
    onSelect?.(cmp);
  };

  const handleHeadDoubleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onAddComment?.({ type: 'cmp', id: cmp.id }, e);
  };

  const handleExpandClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onToggleExpand?.(cmp.id);
  };

  const handleExpandAllClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onSetAllWide?.(cmp.id, !allWide);
  };

  return (
    <div
      className={`hf-cmp ${diffCls} ${focused ? 'focused' : ''} ${dimmed ? 'dimmed' : ''}`}
      style={{
        left: cmp.x,
        top: cmp.y,
        width: w,
        height: h,
      }}
      onClick={handleClick}
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
        >
          <div className="hf-cmp-icon">{parentInitial}</div>
          <div className="hf-cmp-name">{cmp.name}</div>
          <span className="hf-cmp-tech">{cmp.tech}</span>
        </div>

        {/* Description (collapsed only) */}
        {!expanded && <div className="hf-cmp-desc">{cmp.desc}</div>}

        {/* Internals mini-canvas (expanded only) */}
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
                onAddComment={onAddComment}
                hasComment={hasComment}
              />
            ))}
          </div>
        )}
      </div>

      {/* Floating action group — kept OUTSIDE .hf-cmp-inner so the (i) popover
          escapes the card's overflow clipping. Grouping the buttons stops the
          (i) and expand-all buttons from overlapping each other. */}
      <div className="hf-cmp-actions">
        {/* Description info button — only when expanded (collapsed cards show
            the description in the body); its popover opens above the button. */}
        {cmp.desc && expanded && (
          <div className="hf-cmp-info">
            <span className="hf-cmp-info-icon">i</span>
            <div className="hf-cmp-info-pop">{cmp.desc}</div>
          </div>
        )}
        {/* Expand-all: widens every internal so all member text shows (or resets
            them). Only meaningful while the component is open and has internals. */}
        {expanded && hasInternals && (
          <button
            className="hf-cmp-expand-all"
            onClick={handleExpandAllClick}
            title={allWide ? 'Reset all blocks width' : 'Expand all blocks to fit text'}
          >
            {allWide ? '»«' : '«»'}
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

interface InternalCardProps {
  internal: Internal;
  showDiff: boolean;
  expanded: boolean;
  /** Fit-width mode (card stretched to show all member text). Drives the +/− button. */
  wide: boolean;
  onToggleWide: () => void;
  onAddComment?: (target: { type: string; id: string }, event: React.MouseEvent) => void;
  hasComment: (id: string) => boolean;
}

function InternalCard({
  internal,
  showDiff,
  expanded,
  wide,
  onToggleWide,
  onAddComment,
  hasComment,
}: InternalCardProps) {
  const diffCls = showDiff && deriveInternalDiff(internal) ? deriveInternalDiff(internal) : '';
  // Use layout-provided height if available, otherwise compute locally
  // Layout sets internal.h based on expanded state at layout time
  const memberHeight = expanded ? (internal.members?.length ?? 0) * 18 + 4 : 0;
  const h = internal.h ?? (26 + memberHeight);

  const handleHeadClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onAddComment?.({ type: 'internal', id: internal.id }, e);
  };

  const handleToggleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onToggleWide();
  };

  return (
    <div
      className={`hf-internal ${internal.kind} ${diffCls}`}
      style={{
        left: internal.x,
        top: internal.y,
        width: internal.w,
        height: h,
      }}
    >
      <div className="hf-internal-head" onClick={handleHeadClick}>
        <span className="hf-internal-kind">
          {internal.kind === 'iface' ? 'iface' : 'class'}
        </span>
        <span className="hf-internal-name" title={internal.name}>{internal.name}</span>
        {hasComment(internal.id) && <span className="hf-cmt-marker sm">!</span>}
        <span
          className="hf-internal-toggle"
          onClick={handleToggleClick}
          title={wide ? 'Reset width' : 'Fit width to member text'}
        >
          {wide ? '−' : '+'}
        </span>
      </div>
      {expanded && (
        <div className="hf-member-list">
          {(internal.members ?? []).map((member) => (
            <MemberRow
              key={member.id}
              member={member}
              showDiff={showDiff}
              hasComment={hasComment(member.id)}
              onAddComment={onAddComment}
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
  hasComment: boolean;
  onAddComment?: (target: { type: string; id: string }, event: React.MouseEvent) => void;
}

function MemberRow({ member, showDiff, hasComment, onAddComment }: MemberRowProps) {
  const diffCls = showDiff && member.diff ? member.diff : '';

  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onAddComment?.({ type: 'member', id: member.id }, e);
  };

  return (
    <div className={`hf-member ${diffCls}`} onClick={handleClick} title={member.name}>
      <span className={`hf-member-kind ${member.kind === 'method' ? 'fn' : 'prop'}`}>
        {member.kind === 'method' ? 'fn' : ':'}
      </span>
      <span className="hf-member-name">{member.name}</span>
      {hasComment && <span className="hf-cmt-marker sm">!</span>}
    </div>
  );
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
