import { useState, useEffect } from 'react';

export type { PendingComment } from '../domain/state';
import type { PendingComment } from '../domain/state';

export interface InlinePopoverProps {
  /** The pending comment state (null = hidden) */
  pending: PendingComment | null;
  /** Callback when cancel is clicked or Escape pressed */
  onCancel: () => void;
  /** Callback when comment is submitted */
  onSubmit: (text: string) => void;
}

/**
 * Inline popover for adding comments.
 * Anchored at canvas coordinates (x, y) relative to canvas wrap.
 * Ported from hifi-v4.jsx InlinePopover.
 */
export function InlinePopover({ pending, onCancel, onSubmit }: InlinePopoverProps) {
  const [text, setText] = useState('');

  // Reset text when target changes
  useEffect(() => {
    setText('');
  }, [pending?.target.id]);

  if (!pending) return null;

  const { x, y, target } = pending;

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Escape') {
      onCancel();
    }
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      onSubmit(text || '...');
    }
  };

  const handleSubmitClick = () => {
    onSubmit(text || '...');
  };

  return (
    <div
      className="hf-popover"
      style={{ left: x, top: y }}
      onClick={(e) => e.stopPropagation()}
    >
      <div className="hf-popover-arrow" />
      <div className="hf-popover-meta">
        <span className="hf-popover-tag">{target.type}</span>
        <span className="hf-popover-target mono">{target.id}</span>
      </div>
      <textarea
        autoFocus
        placeholder="Leave a comment..."
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={handleKeyDown}
      />
      <div className="hf-popover-actions">
        <button className="hf-btn" onClick={onCancel}>
          Cancel
        </button>
        <button
          className="hf-btn primary"
          onClick={handleSubmitClick}
          disabled={!text.trim()}
          style={{ opacity: text.trim() ? 1 : 0.5 }}
        >
          Comment
        </button>
      </div>
    </div>
  );
}
