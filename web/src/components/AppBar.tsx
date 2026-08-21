import type { PR } from '../types';

export interface AppBarProps {
  /** Current theme */
  theme: 'dark' | 'light';
  onThemeToggle?: () => void;
  /** Reload live graph data from the server */
  onRefresh?: () => void;
  /** True while graph data is being reloaded */
  refreshing?: boolean;
  /** Open the architecture review report */
  onReport?: () => void;
  /** Open the domains canvas: structural clusters against semantic ones */
  onDomains?: () => void;
  /** The domains canvas is up in place of the review canvas */
  domainsOn?: boolean;
  /** Open the file-level diff of the reviewed branch */
  onDiff?: () => void;
  /** Open the ask panel and put the cursor in its question box */
  onAsk?: () => void;
  /** An answer is currently projecting the canvas */
  asking?: boolean;
  /** PR data for crumbs (optional - use defaults if absent) */
  pr?: PR;
}

export function AppBar({
  theme,
  onThemeToggle,
  onRefresh,
  refreshing = false,
  onReport,
  onDomains,
  domainsOn = false,
  onDiff,
  onAsk,
  asking = false,
  pr,
}: AppBarProps) {
  const branch = pr?.branch ?? 'main';
  const repoName = 'wyrd'; // fallback

  return (
    <div className="hf-appbar">
      <div className="hf-logo">A</div>
      <div className="hf-crumbs">
        <span>{repoName}</span>
        <span className="sep">/</span>
        <span>main</span>
        {pr && (
          <>
            <span className="sep">&larr;</span>
            <span className="branch">{branch}</span>
          </>
        )}
      </div>
      <div className="hf-spacer" />
      <button
        className="hf-btn"
        onClick={onThemeToggle}
        title="Toggle theme"
      >
        {theme === 'dark' ? '☾' : '☀'}
      </button>
      <button
        className={`hf-btn ${asking ? 'on' : ''}`}
        onClick={onAsk}
        title="Ask a question about this code and draw the packages that answer it"
      >
        Ask
      </button>
      <button
        className="hf-btn"
        onClick={onDiff}
        title="Show the file diff against the review base"
      >
        Diff
      </button>
      <button
        className="hf-btn"
        onClick={onReport}
        title="What this branch did to the structure, or what to refactor next"
      >
        ArchMotif
      </button>
      <button
        className={`hf-btn ${domainsOn ? 'on' : ''}`}
        onClick={onDomains}
        title="Read the structural clusters against the semantic ones"
      >
        Domains
      </button>
      <button
        className="hf-btn"
        onClick={onRefresh}
        disabled={refreshing}
        title="Reload graph from live wyrd serve data"
      >
        {refreshing ? 'Refreshing...' : 'Refresh'}
      </button>
    </div>
  );
}
