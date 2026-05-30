/* archai hi-fi — V1 V2 V3 */

const useFocus = () => {
  const [focusId, setFocusId] = React.useState(null);
  const related = React.useMemo(() => {
    if (!focusId) return null;
    const r = new Set([focusId]);
    HF.S.edges.forEach(e => {
      if (e.from === focusId) r.add(e.to);
      if (e.to === focusId)   r.add(e.from);
    });
    return r;
  }, [focusId]);
  return [focusId, setFocusId, related];
};

function useExpansion(initial) {
  const [exp, toggle, setExp] = HF.useExpanded(initial);
  const [intExp, toggleInt, setIntExp] = HF.useExpanded([]);
  React.useEffect(() => {
    setIntExp(prev => {
      const n = new Set(prev);
      exp.forEach(cid => {
        const c = HF.S.components.find(x => x.id === cid);
        if (c) c.internals.forEach(i => n.add(i.id));
      });
      return n;
    });
  }, [exp]);
  return { exp, toggle, intExp, toggleInt };
}

// ─── Right inspector — comments + changes ──────────────
function Inspector({ activeChange, setActiveChange, onGoToChange, pendingComment, setPendingComment, theme }) {
  const [tab, setTab] = React.useState('comments');
  const changes = React.useMemo(HF.deriveChanges, []);
  const [active, setActive] = React.useState('c1');

  return (
    <div className="hf-side right" style={{ display: 'flex', flexDirection: 'column' }}>
      <div className="hf-tabs">
        <button className={tab === 'comments' ? 'on' : ''} onClick={() => setTab('comments')}>
          COMMENTS<span className="count">{HF.S.comments.length}</span>
        </button>
        <button className={tab === 'changes' ? 'on' : ''} onClick={() => setTab('changes')}>
          CHANGES<span className="count">{changes.length}</span>
        </button>
      </div>

      <div className="hf-list">
        {tab === 'comments' && (
          <>
            {pendingComment && (
              <div className="hf-card active">
                <div className="hf-card-meta">
                  <span style={{ color: 'var(--accent)', fontWeight: 700 }}>NEW</span>
                  <span style={{ flex: 1 }} />
                  <span className="hf-card-target">{pendingComment.target.type}:{pendingComment.target.id}</span>
                </div>
                <textarea autoFocus placeholder="Leave a comment…"
                  style={{ width: '100%', background: 'var(--bg-1)', border: '1px solid var(--line-2)',
                           borderRadius: 6, color: 'var(--fg-0)', padding: 8, fontSize: 12,
                           fontFamily: 'Inter, sans-serif', minHeight: 60, marginTop: 4 }} />
                <div style={{ display: 'flex', gap: 6, marginTop: 6 }}>
                  <button className="hf-btn primary" style={{ flex: 1 }}>Comment</button>
                  <button className="hf-btn" onClick={() => setPendingComment(null)}>Cancel</button>
                </div>
              </div>
            )}
            {HF.S.comments.map((cm, i) => (
              <div key={cm.id} className={`hf-card ${active === cm.id ? 'active' : ''}`}
                   onClick={() => setActive(cm.id)}>
                <div className="hf-card-meta">
                  <span className="hf-card-author">@you</span>
                  <span>· 2m ago</span>
                  <span className="hf-card-target">{cm.target.type}:{cm.target.id}</span>
                </div>
                <div className="hf-card-body">{cm.body}</div>
              </div>
            ))}
            <div style={{ textAlign: 'center', color: 'var(--fg-3)', fontSize: 11, padding: '12px 0',
                          fontFamily: 'JetBrains Mono, monospace' }}>
              click any element to comment
            </div>
          </>
        )}
        {tab === 'changes' && changes.map(ch => (
          <div key={ch.id} className={`hf-card ${activeChange === ch.id ? 'active' : ''}`}
               onClick={() => onGoToChange && onGoToChange(ch)}>
            <div className="hf-change-card">
              <div className="hf-change-row1">
                <span className={`hf-change-badge ${ch.kind}`}>
                  {ch.kind === 'added' ? '+ ADD' : ch.kind === 'removed' ? '− DEL' : '~ MOD'}
                </span>
                <span className="hf-change-name">{ch.name}</span>
              </div>
              <div className="hf-change-where">{ch.where}</div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

// ─── V1 — VSCode-ish, balanced ──────────────────────────
window.HFV1 = function HFV1({ tweaks }) {
  const [level, setLevel] = React.useState(2);
  const [theme, setTheme] = React.useState(tweaks.theme || 'dark');
  const { exp, toggle, intExp, toggleInt } = useExpansion(['orders']);
  const [focusId, setFocusId, related] = useFocus();
  const [pendingComment, setPendingComment] = React.useState(null);
  const [activeChange, setActiveChange] = React.useState(null);
  const canvasRef = React.useRef(null);
  const showDiff = tweaks.diff;
  const showBC = tweaks.bc;
  const cmtTargets = React.useMemo(() => new Set(HF.S.comments.map(c => c.target.id)), []);

  const startComment = (target) => setPendingComment({ target });
  const goToChange = (ch) => {
    setActiveChange(ch.id);
    if (ch.cmp && (ch.internal || ch.member || ch.port) && !exp.has(ch.cmp)) toggle(ch.cmp);
    setTimeout(() => {
      const c = HF.S.components.find(x => x.id === ch.cmp);
      if (c && canvasRef.current) {
        canvasRef.current.scrollTo({
          left: c.x + c.w/2 - canvasRef.current.clientWidth/2,
          top:  c.y + c.h/2 - canvasRef.current.clientHeight/2,
          behavior: 'smooth',
        });
      }
    }, 200);
  };

  return (
    <div className={`hifi v1 theme-${theme}`} style={{ width: '100%', height: '100%', display: 'flex', flexDirection: 'column' }}>
      <HF.AppBar level={level} onLevel={setLevel}
                 theme={theme} onTheme={() => setTheme(t => t === 'dark' ? 'light' : 'dark')}
                 commentCount={HF.S.comments.length} />
      {showDiff && <HF.PrHeader />}
      <div className="hf-stage">
        <div className="hf-side">
          <div className="hf-side-title">Bounded contexts</div>
          <HF.Tree showDiff={showDiff} />
          <div className="hf-side-title">Filters</div>
          <div style={{ padding: '0 14px 16px', fontSize: 11, color: 'var(--fg-2)', display: 'flex', flexDirection: 'column', gap: 6 }}>
            <label><input type="checkbox" checked={showDiff} readOnly /> diff coloring</label>
            <label><input type="checkbox" checked={showBC} readOnly /> bounded contexts</label>
            <label><input type="checkbox" checked={tweaks.flow} readOnly /> animated flow</label>
          </div>
        </div>

        <div ref={canvasRef} className="hf-canvas-wrap">
          <div className="hf-canvas" onClick={() => setFocusId(null)}>
            <HF.BCGroups show={showBC} />
            <HF.EdgeLayer edges={HF.S.edges}
              expandedSet={exp} expandedInternals={intExp}
              showDiff={showDiff} focusId={focusId} flow={tweaks.flow}
              commentTargets={cmtTargets} onAddComment={startComment} />
            {HF.S.components.map(c => (
              <HF.Component key={c.id} cmp={c}
                expanded={exp.has(c.id)} onToggleExpand={toggle}
                expandedInternals={intExp} onToggleInternal={toggleInt}
                showDiff={showDiff}
                focused={focusId === c.id}
                dimmed={focusId && related && !related.has(c.id)}
                onSelect={(cmp) => setFocusId(prev => prev === cmp.id ? null : cmp.id)}
                onAddComment={startComment} commentTargets={cmtTargets} />
            ))}
          </div>
          <HF.CanvasToolbar />
          <HF.Legend showDiff={showDiff} />
        </div>

        <Inspector activeChange={activeChange} setActiveChange={setActiveChange}
                   onGoToChange={goToChange}
                   pendingComment={pendingComment} setPendingComment={setPendingComment} />
      </div>
    </div>
  );
};

// ─── V2 — graphical canvas focus, larger boxes, no PR header in line, big diff banner ──────
window.HFV2 = function HFV2({ tweaks }) {
  const [theme, setTheme] = React.useState(tweaks.theme || 'dark');
  const { exp, toggle, intExp, toggleInt } = useExpansion(['orders', 'pay']);
  const [focusId, setFocusId, related] = useFocus();
  const [pendingComment, setPendingComment] = React.useState(null);
  const cmtTargets = React.useMemo(() => new Set(HF.S.comments.map(c => c.target.id)), []);
  const startComment = (t) => setPendingComment({ target: t });

  return (
    <div className={`hifi v2 theme-${theme}`} style={{ width: '100%', height: '100%', display: 'flex', flexDirection: 'column' }}>
      <HF.AppBar level={2} onLevel={() => {}} theme={theme}
                 onTheme={() => setTheme(t => t === 'dark' ? 'light' : 'dark')}
                 commentCount={HF.S.comments.length} />
      <HF.PrHeader />
      <div className="hf-stage">
        <div className="hf-canvas-wrap" style={{ flex: 1 }}>
          <div className="hf-canvas" onClick={() => setFocusId(null)}>
            <HF.BCGroups show={true} />
            <HF.EdgeLayer edges={HF.S.edges}
              expandedSet={exp} expandedInternals={intExp}
              showDiff={true} focusId={focusId} flow={true}
              commentTargets={cmtTargets} onAddComment={startComment} />
            {HF.S.components.map(c => (
              <HF.Component key={c.id} cmp={c}
                expanded={exp.has(c.id)} onToggleExpand={toggle}
                expandedInternals={intExp} onToggleInternal={toggleInt}
                showDiff={true}
                focused={focusId === c.id}
                dimmed={focusId && related && !related.has(c.id)}
                onSelect={(cmp) => setFocusId(prev => prev === cmp.id ? null : cmp.id)}
                onAddComment={startComment} commentTargets={cmtTargets} />
            ))}
          </div>
          <HF.CanvasToolbar />
          <HF.Legend showDiff={true} />
        </div>
        <Inspector pendingComment={pendingComment} setPendingComment={setPendingComment} />
      </div>
    </div>
  );
};

// ─── V3 — review-first: changes list dominant, canvas as preview ──────
window.HFV3 = function HFV3({ tweaks }) {
  const [theme, setTheme] = React.useState(tweaks.theme || 'dark');
  const { exp, toggle, intExp, toggleInt } = useExpansion(['orders', 'pay', 'events']);
  const [focusId, setFocusId, related] = useFocus();
  const [pendingComment, setPendingComment] = React.useState(null);
  const [activeChange, setActiveChange] = React.useState('cmp-events');
  const cmtTargets = React.useMemo(() => new Set(HF.S.comments.map(c => c.target.id)), []);
  const startComment = (t) => setPendingComment({ target: t });
  const changes = React.useMemo(HF.deriveChanges, []);
  const canvasRef = React.useRef(null);

  const goTo = (ch) => {
    setActiveChange(ch.id);
    setFocusId(ch.cmp);
    if (ch.cmp && (ch.internal || ch.member || ch.port) && !exp.has(ch.cmp)) toggle(ch.cmp);
    setTimeout(() => {
      const c = HF.S.components.find(x => x.id === ch.cmp);
      if (c && canvasRef.current) {
        canvasRef.current.scrollTo({
          left: c.x + c.w/2 - canvasRef.current.clientWidth/2,
          top:  c.y + c.h/2 - canvasRef.current.clientHeight/2,
          behavior: 'smooth',
        });
      }
    }, 100);
  };

  return (
    <div className={`hifi v3 theme-${theme}`} style={{ width: '100%', height: '100%', display: 'flex', flexDirection: 'column' }}>
      <HF.AppBar level={2} onLevel={() => {}} theme={theme}
                 onTheme={() => setTheme(t => t === 'dark' ? 'light' : 'dark')}
                 commentCount={HF.S.comments.length} />
      <div className="hf-stage">
        {/* Left: summary + change list */}
        <div className="hf-side" style={{ width: 340 }}>
          <div className="v3-diff-banner">
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
              <span className="hf-pr-tag">AGENT PR</span>
              <span style={{ fontSize: 11, color: 'var(--fg-2)', fontFamily: 'JetBrains Mono, monospace' }}>
                {HF.S.pr.agent}
              </span>
            </div>
            <div style={{ fontWeight: 600, fontSize: 14, color: 'var(--fg-0)', marginBottom: 8, lineHeight: 1.35 }}>
              {HF.S.pr.title}
            </div>
            <div style={{ fontSize: 12, color: 'var(--fg-2)', lineHeight: 1.5, marginBottom: 10 }}>
              {HF.S.pr.summary}
            </div>
            <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
              <span className="hf-stat add">+{HF.S.pr.stats.added}</span>
              <span className="hf-stat rem">−{HF.S.pr.stats.removed}</span>
              <span className="hf-stat chg">~{HF.S.pr.stats.changed}</span>
              <span className="hf-stat com">{HF.S.pr.stats.comments}c</span>
            </div>
          </div>
          <div className="hf-side-title">All changes ({changes.length})</div>
          <div className="hf-list" style={{ paddingTop: 0 }}>
            {changes.map(ch => (
              <div key={ch.id} className={`hf-card ${activeChange === ch.id ? 'active' : ''}`}
                   onClick={() => goTo(ch)}>
                <div className="hf-change-card">
                  <div className="hf-change-row1">
                    <span className={`hf-change-badge ${ch.kind}`}>
                      {ch.kind === 'added' ? '+' : ch.kind === 'removed' ? '−' : '~'}
                    </span>
                    <span className="hf-change-name">{ch.name}</span>
                  </div>
                  <div className="hf-change-where">{ch.where}</div>
                </div>
              </div>
            ))}
          </div>
        </div>

        {/* Center: canvas */}
        <div ref={canvasRef} className="hf-canvas-wrap" style={{ flex: 1 }}>
          <div className="hf-canvas" onClick={() => setFocusId(null)}>
            <HF.BCGroups show={true} />
            <HF.EdgeLayer edges={HF.S.edges}
              expandedSet={exp} expandedInternals={intExp}
              showDiff={true} focusId={focusId} flow={true}
              commentTargets={cmtTargets} onAddComment={startComment} />
            {HF.S.components.map(c => (
              <HF.Component key={c.id} cmp={c}
                expanded={exp.has(c.id)} onToggleExpand={toggle}
                expandedInternals={intExp} onToggleInternal={toggleInt}
                showDiff={true}
                focused={focusId === c.id}
                dimmed={focusId && related && !related.has(c.id)}
                onSelect={(cmp) => setFocusId(prev => prev === cmp.id ? null : cmp.id)}
                onAddComment={startComment} commentTargets={cmtTargets} />
            ))}
          </div>
          <HF.CanvasToolbar />
        </div>

        {/* Right: discussion thread of selected change */}
        <div className="hf-side right" style={{ display: 'flex', flexDirection: 'column' }}>
          <div className="hf-side-title">Discussion</div>
          <div className="hf-list">
            {HF.S.comments.map(cm => (
              <div key={cm.id} className="hf-card">
                <div className="hf-card-meta">
                  <span className="hf-card-author">@you</span>
                  <span>· 2m ago</span>
                  <span className="hf-card-target">{cm.target.type}:{cm.target.id}</span>
                </div>
                <div className="hf-card-body">{cm.body}</div>
              </div>
            ))}
          </div>
          <div className="hf-compose">
            <textarea placeholder={pendingComment ? `Comment on ${pendingComment.target.type}:${pendingComment.target.id}…` : "Click any element on canvas to comment, or write here…"} />
            <div className="row">
              {pendingComment && <span className="target-tag">{pendingComment.target.type}:{pendingComment.target.id}</span>}
              <span style={{ flex: 1 }} />
              <button className="hf-btn">Cancel</button>
              <button className="hf-btn primary">Comment</button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

Object.assign(window, { HFV1: window.HFV1, HFV2: window.HFV2, HFV3: window.HFV3 });
