import type { PR } from '../types';

export interface PrHeaderProps {
  pr: PR;
}

export function PrHeader({ pr }: PrHeaderProps) {
  return (
    <div className="hf-prheader">
      <span className="hf-pr-tag">AGENT PR</span>
      <span className="hf-pr-title">{pr.title}</span>
      <span className="hf-pr-meta">opened by {pr.agent} &middot; 12m ago</span>
      <span style={{ flex: 1 }} />
      <span className="hf-stat add">+{pr.stats.added}</span>
      <span className="hf-stat rem">&minus;{pr.stats.removed}</span>
      <span className="hf-stat chg">~{pr.stats.changed}</span>
      <span className="hf-stat com">&#128172; {pr.stats.comments}</span>
    </div>
  );
}
