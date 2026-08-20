import { useEffect, useState } from 'react';
import type { AskHit, AskHitGroup } from '../domain/ask';
import type { AskState } from '../domain/state';

export interface AskPanelProps {
  ask: AskState;
  /** Hits grouped by package, in rank order. */
  groups: AskHitGroup[];
  /** Packages the answer put on the canvas. */
  packageCount: number;
  onSubmit: (query: string) => void;
  onClear: () => void;
  onHitClick: (hit: AskHit) => void;
  onDetailOnlyToggle: () => void;
  onDepthChange: (k: number) => void;
}

const DEPTHS = [10, 20, 50];

/**
 * Ask a question of the indexed code and read the answer as architecture: the
 * ranked answer here, the packages it spans on the canvas.
 *
 * The answer has two kinds of row. A *hit* is what the query text matched; a
 * *related* row is what the graph diffusion reached from those hits, and it is
 * marked as such — it is the answer's context, and the reader has to be able to
 * tell the two apart at a glance.
 */
export function AskPanel({
  ask,
  groups,
  packageCount,
  onSubmit,
  onClear,
  onHitClick,
  onDetailOnlyToggle,
  onDepthChange,
}: AskPanelProps) {
  const [draft, setDraft] = useState(ask.query);
  const seedCount = ask.hits.filter((hit) => hit.seed).length;
  const relatedCount = ask.hits.length - seedCount;
  // A cleared ask (or one restored from elsewhere) resets the box.
  useEffect(() => setDraft(ask.query), [ask.query]);

  const submit = () => {
    const query = draft.trim();
    if (!query) {
      onClear();
      return;
    }
    onSubmit(query);
  };

  return (
    <div className="hf-ask">
      <div className="hf-ask-input">
        <input
          className="hf-ask-field"
          value={draft}
          placeholder="What does this code do?"
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') submit();
            if (e.key === 'Escape') {
              setDraft('');
              onClear();
            }
            e.stopPropagation();
          }}
        />
        <button className="hf-ask-run" onClick={submit} disabled={ask.status === 'loading'}>
          {ask.status === 'loading' ? '…' : 'Ask'}
        </button>
      </div>

      <div className="hf-ask-meta">
        {ask.status === 'error' && <span className="hf-ask-error">{ask.error}</span>}
        {ask.status === 'loading' && <span>Searching…</span>}
        {ask.status === 'ready' && (
          <>
            <span>
              {seedCount} {seedCount === 1 ? 'hit' : 'hits'}
              {relatedCount > 0 && ` · ${relatedCount} related`} · {packageCount}{' '}
              {packageCount === 1 ? 'package' : 'packages'}
            </span>
            <span className={`hf-ask-mode ${ask.dense ? 'dense' : ''}`} title={
              ask.dense
                ? 'Ranked by embedding similarity fused with keyword matching'
                : 'No vector index answered — keyword ranking only'
            }>
              {ask.dense ? 'semantic' : 'lexical only'}
            </span>
          </>
        )}
        {ask.status === 'idle' && <span>Ranked by meaning, not by name.</span>}
      </div>

      {ask.query !== '' && (
        <div className="hf-ask-controls">
          <button
            className={`hf-ask-toggle ${ask.detailOnly ? 'on' : ''}`}
            onClick={onDetailOnlyToggle}
            title="Show only the matched symbols on each card, or the whole package"
          >
            {ask.detailOnly ? 'Matched symbols' : 'Full packages'}
          </button>
          <label className="hf-ask-depth" title="How many hits to ask for">
            <span>Hits</span>
            <select value={ask.k} onChange={(e) => onDepthChange(Number(e.target.value))}>
              {DEPTHS.map((depth) => (
                <option key={depth} value={depth}>
                  {depth}
                </option>
              ))}
            </select>
          </label>
          <button className="hf-ask-clear" onClick={onClear} title="Back to the review">
            Clear
          </button>
        </div>
      )}

      <div className="hf-ask-results">
        {groups.map((group) => (
          <div className="hf-ask-group" key={group.packageId}>
            <div className="hf-ask-group-head">
              <span className="hf-ask-group-name">{group.packageId}</span>
              <span className="hf-ask-group-count">{group.hits.length}</span>
            </div>
            {group.hits.map((hit) => (
              <div
                key={hit.nodeId}
                className={`hf-ask-hit ${ask.activeHitId === hit.nodeId ? 'active' : ''} ${hit.inGraph ? '' : 'off-graph'} ${hit.seed ? '' : 'related'}`}
                onClick={() => hit.inGraph && onHitClick(hit)}
                title={
                  hit.inGraph
                    ? hit.seed
                      ? hit.nodeId
                      : `${hit.nodeId} — reached from the hits through the graph`
                    : `${hit.nodeId} — not in the loaded graph`
                }
              >
                <div className="hf-ask-hit-row">
                  <span className={`hf-ask-kind ${hit.kind}`}>{hit.kind}</span>
                  <span className="hf-ask-hit-name">{hit.name}</span>
                  {!hit.seed && <span className="hf-ask-hit-flag">related</span>}
                  {hit.seed && !hit.symbolInGraph && hit.inGraph && <span className="hf-ask-hit-flag">package only</span>}
                  {!hit.inGraph && <span className="hf-ask-hit-flag">off canvas</span>}
                </div>
                {hit.doc && <div className="hf-ask-hit-doc">{hit.doc}</div>}
                {hit.file && (
                  <div className="hf-ask-hit-where">
                    {hit.file}
                    {hit.line ? `:${hit.line}` : ''}
                  </div>
                )}
              </div>
            ))}
          </div>
        ))}
        {ask.status === 'ready' && ask.hits.length === 0 && (
          <div className="hf-ask-empty">Nothing matched. Try naming the behaviour, not the symbol.</div>
        )}
      </div>
    </div>
  );
}
