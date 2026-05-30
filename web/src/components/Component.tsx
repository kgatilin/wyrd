import type { Component as ComponentType, Internal, Member, Port } from '../types';
import { computeExpandedHeight } from '../state/hooks';

export interface ComponentProps {
  /** The component data with layout geometry */
  cmp: ComponentType;
  /** Whether this component is expanded */
  expanded: boolean;
  /** Callback to toggle expansion */
  onToggleExpand?: (id: string) => void;
  /** Set of expanded internal IDs */
  expandedInternals: Set<string>;
  /** Callback to toggle internal expansion */
  onToggleInternal?: (id: string) => void;
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
  onToggleInternal,
  showDiff,
  onSelect,
  focused = false,
  dimmed = false,
  onAddComment,
  commentTargets,
}: ComponentProps) {
  const diffCls = showDiff && cmp.diff ? cmp.diff : '';
  const w = expanded ? cmp.wx ?? cmp.w : cmp.w;
  const h = expanded ? computeExpandedHeight(cmp, expandedInternals) : cmp.h;

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
      {/* Header */}
      <div
        className="hf-cmp-head"
        onClick={handleHeadClick}
        onDoubleClick={handleHeadDoubleClick}
      >
        <div className="hf-cmp-icon">{cmp.name[0]}</div>
        <div className="hf-cmp-name">{cmp.name}</div>
        <span className="hf-cmp-tech">{cmp.tech}</span>
        <span style={{ flex: 1 }} />
        {showDiff && cmp.diff && (
          <span className="hf-cmp-diff-tag">
            {cmp.diff === 'added' ? 'NEW' : cmp.diff === 'removed' ? 'DEL' : 'MOD'}
          </span>
        )}
        <button className="hf-cmp-expand" onClick={handleExpandClick}>
          {expanded ? '−' : '+'}
        </button>
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
              onToggle={() => onToggleInternal?.(internal.id)}
              onAddComment={onAddComment}
              hasComment={hasComment}
            />
          ))}
        </div>
      )}

      {/* Ports */}
      {cmp.ports.map((port) => (
        <PortDot
          key={port.id}
          port={port}
          componentExpanded={expanded}
          componentHeight={h ?? 86}
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
  onToggle: () => void;
  onAddComment?: (target: { type: string; id: string }, event: React.MouseEvent) => void;
  hasComment: (id: string) => boolean;
}

function InternalCard({
  internal,
  showDiff,
  expanded,
  onToggle,
  onAddComment,
  hasComment,
}: InternalCardProps) {
  const diffCls = showDiff && internal.diff ? internal.diff : '';
  const memberHeight = expanded ? (internal.members?.length ?? 0) * 18 + 4 : 0;
  const h = 26 + memberHeight;

  const handleHeadClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onAddComment?.({ type: 'internal', id: internal.id }, e);
  };

  const handleToggleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onToggle();
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
        <span className="hf-internal-name">{internal.name}</span>
        {hasComment(internal.id) && <span className="hf-cmt-marker sm">!</span>}
        <span className="hf-internal-toggle" onClick={handleToggleClick}>
          {expanded ? '−' : '+'}
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
    <div className={`hf-member ${diffCls}`} onClick={handleClick}>
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
  componentExpanded: boolean;
  componentHeight: number;
  showDiff: boolean;
  hasComment: boolean;
  onAddComment?: (target: { type: string; id: string }, event: React.MouseEvent) => void;
}

function PortDot({
  port,
  componentExpanded,
  componentHeight,
  showDiff,
  hasComment,
  onAddComment,
}: PortDotProps) {
  const diffCls = showDiff && port.diff ? port.diff : '';

  // Calculate port Y position
  // When expanded, use the port's y directly; when collapsed, clamp to component height
  const portY = port.y ?? 58;
  const py = componentExpanded ? portY - 7 : Math.min(portY, componentHeight - 14) - 7;

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
