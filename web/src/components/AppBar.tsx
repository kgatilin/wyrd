import type { PR } from '../types';

export interface AppBarProps {
  /** Current abstraction level (0=L1/System, 1=L2/Container, 2=L3/Component) */
  level: number;
  onLevelChange?: (level: number) => void;
  /** Current theme */
  theme: 'dark' | 'light';
  onThemeToggle?: () => void;
  /** Number of comments for the badge */
  commentCount: number;
  /** PR data for crumbs (optional - use defaults if absent) */
  pr?: PR;
  /** Callback when Submit review is clicked */
  onSubmitReview?: () => void;
}

const LEVELS = [
  { label: 'L1', name: 'System' },
  { label: 'L2', name: 'Container' },
  { label: 'L3', name: 'Component' },
];

export function AppBar({
  level,
  onLevelChange,
  theme,
  onThemeToggle,
  commentCount,
  pr,
  onSubmitReview,
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
      <div className="hf-seg">
        {LEVELS.map((l, i) => (
          <button
            key={l.label}
            className={level === i ? 'on' : ''}
            onClick={() => onLevelChange?.(i)}
          >
            {l.label} &middot; {l.name}
          </button>
        ))}
      </div>
      <button
        className="hf-btn"
        onClick={onThemeToggle}
        title="Toggle theme"
      >
        {theme === 'dark' ? '☾' : '☀'}
      </button>
      <button className="hf-btn">Approve</button>
      <button className="hf-btn primary" onClick={onSubmitReview}>
        Submit review
        <span className="count">{commentCount}</span>
      </button>
    </div>
  );
}
