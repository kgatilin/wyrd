import type { PR } from '../types';

export interface AppBarProps {
  /** Current theme */
  theme: 'dark' | 'light';
  onThemeToggle?: () => void;
  /** Reload live graph data from the server */
  onRefresh?: () => void;
  /** True while graph data is being reloaded */
  refreshing?: boolean;
  /** Open the ArchMotif analysis panel */
  onMetrics?: () => void;
  /** Open the file-level diff of the reviewed branch */
  onDiff?: () => void;
  /** PR data for crumbs (optional - use defaults if absent) */
  pr?: PR;
}

export function AppBar({
  theme,
  onThemeToggle,
  onRefresh,
  refreshing = false,
  onMetrics,
  onDiff,
  pr,
}: AppBarProps) {
  const branch = pr?.branch ?? 'main';
  const repoName = 'archai'; // fallback

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
        className="hf-btn"
        onClick={onDiff}
        title="Show the file diff against the review base"
      >
        Diff
      </button>
      <button
        className="hf-btn"
        onClick={onMetrics}
        title="Open ArchMotif package metrics"
      >
        ArchMotif
      </button>
      <button
        className="hf-btn"
        onClick={onRefresh}
        disabled={refreshing}
        title="Reload graph from live archai serve data"
      >
        {refreshing ? 'Refreshing...' : 'Refresh'}
      </button>
    </div>
  );
}
