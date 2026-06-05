import type { PR, Stats } from '../types';

export interface PrHeaderProps {
  pr: PR;
  stats?: Stats;
  policyCount?: number;
}

export function PrHeader({ pr, stats = pr.stats, policyCount = 0 }: PrHeaderProps) {
  return (
    <div className="hf-prheader">
      <span className="hf-pr-tag">AGENT PR</span>
      <span className="hf-pr-title">{pr.title}</span>
      <span className="hf-pr-meta">opened by {pr.agent} &middot; 12m ago</span>
      <span style={{ flex: 1 }} />
      <span className="hf-stat add">+{stats.added}</span>
      <span className="hf-stat rem">&minus;{stats.removed}</span>
      <span className="hf-stat chg">~{stats.changed}</span>
      {policyCount > 0 && <span className="hf-stat pol">!{policyCount}</span>}
      <span className="hf-stat com">&#128172; {stats.comments}</span>
    </div>
  );
}
