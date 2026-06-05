import type { UIGraph } from '../types';
export { deriveChanges } from '../domain/derive';
export type { ChangeEntry } from '../domain/derive';
import type { ChangeEntry } from '../domain/derive';
import { SignatureDiff } from './SignatureDiff';

export interface ChangesPanelProps {
  /** The full graph (for PR info) */
  graph: UIGraph;
  /** Derived change entries */
  changes: ChangeEntry[];
  /** Currently active/selected change ID */
  activeChangeId: string | null;
  /** Callback when a change is clicked */
  onChangeClick: (change: ChangeEntry) => void;
}

/**
 * Panel showing PR summary and change cards.
 * Includes AGENT PR tag, title, stats, and clickable change rows.
 */
export function ChangesPanel({
  graph,
  changes,
  activeChangeId,
  onChangeClick,
}: ChangesPanelProps) {
  // PR title/agent/stats already live in the global PrHeader, so this panel
  // shows only the change list (no duplicated PR summary block).
  if (!graph.pr) return null;

  return (
    <div className="hf-list">
      {changes.map((ch) => (
        <div
          key={ch.id}
          className={`hf-card ${activeChangeId === ch.id ? 'active' : ''}`}
          onClick={() => onChangeClick(ch)}
        >
          <div className="hf-change-card">
            <div className="hf-change-row1">
              <span className={`hf-change-badge ${ch.kind}`}>
                {ch.kind === 'added' ? '+' : ch.kind === 'removed' ? '-' : ch.kind === 'policy' ? '!' : '~'}
              </span>
              <span className="hf-change-name">{ch.name}</span>
            </div>
            {ch.kind === 'changed' && (
              <SignatureDiff before={ch.diffBefore} after={ch.diffAfter} />
            )}
            <div className="hf-change-where">{ch.where}</div>
          </div>
        </div>
      ))}
    </div>
  );
}
