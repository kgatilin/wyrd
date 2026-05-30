/* archai hi-fi V4 — merged: V2 grid + V3 changes-first + collapsible panels + inline comments + pinned markers */

const useFocus4 = () => {
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

function useExpansion4(initial) {
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

// Inline popover anchored at canvas-coordinate (x,y)
function InlinePopover({ pending, onCancel, onSubmit }) {
  const [text, setText] = React.useState('');
  React.useEffect(() => { setText(''); }, [pending && pending.target.id]);
  if (!pending) return null;
  const { x, y, target } = pending;
  return (
    <div className="hf-popover" style={{ left: x, top: y }}
         onClick={(e) => e.stopPropagation()}>
      <div className="hf-popover-arrow" />
      <div className="hf-popover-meta">
        <span className="hf-popover-tag">{target.type}</span>
        <span className="hf-popover-target mono">{target.id}</span>
      </div>
      <textarea autoFocus placeholder="Leave a comment…"
        value={text} onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') onCancel();
          if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) onSubmit && onSubmit(text || '…');
        }} />
      <div className="hf-popover-actions">
        <button className="hf-btn" onClick={onCancel}>Cancel</button>
        <button className="hf-btn primary" onClick={() => onSubmit && onSubmit(text || '…')}
                disabled={!text.trim()}
                style={{ opacity: text.trim() ? 1 : 0.5 }}>
          Comment
        </button>
      </div>
    </div>
  );
}

// Pinned marker (numbered pill placed at the spot of the original comment)
function PinnedMarker({ marker, active, onClick }) {
  return (
    <div className={`hf-pin-marker ${active ? 'active' : ''}`}
         style={{ left: marker.x, top: marker.y }}
         onClick={(e) => { e.stopPropagation(); onClick && onClick(marker); }}>
      <span className="hf-pin-marker-num">{marker.n}</span>
    </div>
  );
}

window.HFV4 = function HFV4({ tweaks }) {
  const [level, setLevel] = React.useState(2);
  const [theme, setTheme] = React.useState(tweaks.theme || 'dark');
  const { exp, toggle, intExp, toggleInt } = useExpansion4(['orders']);
  const [focusId, setFocusId, related] = useFocus4();
  const [pendingComment, setPendingComment] = React.useState(null);
  const [activeChange, setActiveChange] = React.useState(null);
  const [activeMarker, setActiveMarker] = React.useState(null);

  const showDiff = tweaks.diff;
  const [leftTab, setLeftTab] = React.useState(showDiff ? 'changes' : 'tree');
  const [leftCol, setLeftCol] = React.useState(false);
  const [rightCol, setRightCol] = React.useState(false);
  const canvasWrapRef = React.useRef(null);
  const cmtTargets = React.useMemo(() => new Set(HF.S.comments.map(c => c.target.id)), []);
  const changes = React.useMemo(HF.deriveChanges, []);

  // local pinned-marker list (n-th comment, with x/y on canvas)
  const seedMarkers = React.useMemo(() => HF.S.comments.map((cm, i) => ({
    id: `seed-${i}`, n: i + 1,
    x: 80 + (i * 130), y: 30 + (i % 2) * 40,  // placeholder fallback; we'll override below
    target: cm.target, body: cm.body, author: '@you', when: '2m',
  })), []);
  // place seed markers near their target by walking SCENARIO
  const seededWithCoords = React.useMemo(() => {
    return seedMarkers.map(m => {
      // find component containing this target
      let host = HF.S.components.find(c => c.id === m.target.id);
      if (!host) {
        host = HF.S.components.find(c =>
          c.internals.some(i => i.id === m.target.id || (i.members||[]).some(mm => mm.id === m.target.id)) ||
          c.ports.some(p => p.id === m.target.id));
      }
      if (!host && m.target.type === 'edge') {
        const e = HF.S.edges.find(ed => ed.id === m.target.id);
        if (e) host = HF.S.components.find(c => c.id === e.from);
      }
      if (host) {
        return { ...m, x: host.x + host.w + 8, y: host.y - 10 };
      }
      return m;
    });
  }, [seedMarkers]);

  const [markers, setMarkers] = React.useState(seededWithCoords);

  const startComment = (target, evt) => {
    let x = 300, y = 300;
    if (evt && canvasWrapRef.current) {
      const wrap = canvasWrapRef.current.getBoundingClientRect();
      const sx = canvasWrapRef.current.scrollLeft;
      const sy = canvasWrapRef.current.scrollTop;
      if (evt.currentTarget && evt.currentTarget.getBoundingClientRect) {
        const rect = evt.currentTarget.getBoundingClientRect();
        // for SVG path, getBBox is in canvas coords already, but bounding rect works
        x = rect.left - wrap.left + sx + rect.width / 2;
        y = rect.bottom - wrap.top + sy + 8;
      } else if (evt.clientX != null) {
        x = evt.clientX - wrap.left + sx;
        y = evt.clientY - wrap.top + sy + 8;
      }
    }
    setPendingComment({ target, x, y });
  };

  const submitComment = (text) => {
    if (!pendingComment) return;
    const n = markers.length + 1;
    const marker = {
      id: `m-${Date.now()}`, n,
      x: pendingComment.x, y: pendingComment.y - 8,
      target: pendingComment.target, body: text,
      author: '@you', when: 'just now',
    };
    setMarkers(prev => [...prev, marker]);
    setPendingComment(null);
    setActiveMarker(marker.id);
  };

  const goToChange = (ch) => {
    setActiveChange(ch.id);
    setFocusId(ch.cmp);
    if (ch.cmp && (ch.internal || ch.member || ch.port) && !exp.has(ch.cmp)) toggle(ch.cmp);
    setTimeout(() => {
      const c = HF.S.components.find(x => x.id === ch.cmp);
      if (c && canvasWrapRef.current) {
        canvasWrapRef.current.scrollTo({
          left: c.x + c.w/2 - canvasWrapRef.current.clientWidth/2,
          top:  c.y + c.h/2 - canvasWrapRef.current.clientHeight/2,
          behavior: 'smooth',
        });
      }
    }, 150);
  };

  return (
    <div className={`hifi v4 theme-${theme}`} style={{ width: '100%', height: '100%', display: 'flex', flexDirection: 'column' }}>
      <HF.AppBar level={level} onLevel={setLevel}
                 theme={theme} onTheme={() => setTheme(t => t === 'dark' ? 'light' : 'dark')}
                 commentCount={markers.length} />
      {showDiff && <HF.PrHeader />}

      <div className="hf-stage">
        {/* LEFT — collapsible, 2 modes (CHANGES | CONTEXTS) */}
        <div className={`hf-side hf-collapsible ${leftCol ? 'collapsed' : ''}`}>
          <button className="hf-side-toggle left" onClick={() => setLeftCol(!leftCol)}>
            {leftCol ? '›' : '‹'}
          </button>
          {leftCol ? (
            <span className="hf-side-vlabel">{leftTab === 'changes' ? 'CHANGES' : 'CONTEXTS'}</span>
          ) : (
            <>
              <div className="hf-tabs" style={{ flexShrink: 0 }}>
                {showDiff && (
                  <button className={leftTab === 'changes' ? 'on' : ''} onClick={() => setLeftTab('changes')}>
                    CHANGES<span className="count">{changes.length}</span>
                  </button>
                )}
                <button className={leftTab === 'tree' ? 'on' : ''} onClick={() => setLeftTab('tree')}>
                  CONTEXTS<span className="count">{HF.S.boundedContexts.length}</span>
                </button>
              </div>

              {leftTab === 'changes' && showDiff && (
                <>
                  <div style={{ padding: '12px 14px 8px', borderBottom: '1px solid var(--line-1)' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 6 }}>
                      <span className="hf-pr-tag">AGENT PR</span>
                      <span style={{ fontSize: 10, color: 'var(--fg-2)', fontFamily: 'JetBrains Mono, monospace' }}>
                        {HF.S.pr.agent}
                      </span>
                    </div>
                    <div style={{ fontWeight: 600, fontSize: 12.5, color: 'var(--fg-0)', lineHeight: 1.35 }}>
                      {HF.S.pr.title}
                    </div>
                    <div style={{ display: 'flex', gap: 4, marginTop: 8, flexWrap: 'wrap' }}>
                      <span className="hf-stat add">+{HF.S.pr.stats.added}</span>
                      <span className="hf-stat rem">−{HF.S.pr.stats.removed}</span>
                      <span className="hf-stat chg">~{HF.S.pr.stats.changed}</span>
                    </div>
                  </div>
                  <div className="hf-list">
                    {changes.map(ch => (
                      <div key={ch.id} className={`hf-card ${activeChange === ch.id ? 'active' : ''}`}
                           onClick={() => goToChange(ch)}>
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
                </>
              )}

              {leftTab === 'tree' && (
                <div className="hf-list" style={{ paddingTop: 6 }}>
                  <HF.Tree showDiff={showDiff} />
                </div>
              )}
            </>
          )}
        </div>

        {/* CENTER — canvas */}
        <div ref={canvasWrapRef} className="hf-canvas-wrap" style={{ flex: 1 }}
             onClick={() => { setFocusId(null); setPendingComment(null); setActiveMarker(null); }}>
          <div className="hf-canvas">
            <HF.BCGroups show={tweaks.bc} />
            <HF.EdgeLayer edges={HF.S.edges}
              expandedSet={exp} expandedInternals={intExp}
              showDiff={showDiff} focusId={focusId} flow={tweaks.flow}
              commentTargets={cmtTargets}
              onAddComment={(t, ev) => startComment(t, ev)} />
            {HF.S.components.map(c => (
              <HF.Component key={c.id} cmp={c}
                expanded={exp.has(c.id)} onToggleExpand={toggle}
                expandedInternals={intExp} onToggleInternal={toggleInt}
                showDiff={showDiff}
                focused={focusId === c.id}
                dimmed={focusId && related && !related.has(c.id)}
                onSelect={(cmp) => setFocusId(prev => prev === cmp.id ? null : cmp.id)}
                onAddComment={(t, ev) => startComment(t, ev)}
                commentTargets={cmtTargets} />
            ))}

            {/* Pinned numbered comment markers */}
            {markers.map(m => (
              <PinnedMarker key={m.id} marker={m}
                active={activeMarker === m.id}
                onClick={(mm) => setActiveMarker(mm.id)} />
            ))}

            <InlinePopover pending={pendingComment}
              onCancel={() => setPendingComment(null)}
              onSubmit={submitComment} />
          </div>
          <HF.CanvasToolbar />
          <HF.Legend showDiff={showDiff} />
        </div>

        {/* RIGHT — comments reference (read-only) */}
        <div className={`hf-side right hf-collapsible ${rightCol ? 'collapsed' : ''}`}>
          <button className="hf-side-toggle right" onClick={() => setRightCol(!rightCol)}>
            {rightCol ? '‹' : '›'}
          </button>
          {rightCol ? (
            <span className="hf-side-vlabel">COMMENTS · {markers.length}</span>
          ) : (
            <>
              <div className="hf-side-title" style={{ display: 'flex', alignItems: 'center' }}>
                Comments
                <span style={{ flex: 1 }} />
                <span style={{ fontSize: 10, color: 'var(--fg-3)', textTransform: 'none', letterSpacing: 0 }}>
                  {markers.length} thread{markers.length !== 1 ? 's' : ''}
                </span>
              </div>
              <div className="hf-list" style={{ paddingTop: 4 }}>
                {markers.map(m => (
                  <div key={m.id} className={`hf-card ${activeMarker === m.id ? 'active' : ''}`}
                       onClick={() => {
                         setActiveMarker(m.id);
                         if (canvasWrapRef.current) {
                           canvasWrapRef.current.scrollTo({
                             left: m.x - canvasWrapRef.current.clientWidth/2,
                             top:  m.y - canvasWrapRef.current.clientHeight/2,
                             behavior: 'smooth',
                           });
                         }
                       }}>
                    <div className="hf-card-meta">
                      <span className="hf-pin-marker-mini">{m.n}</span>
                      <span className="hf-card-author">{m.author}</span>
                      <span>· {m.when}</span>
                      <span className="hf-card-target">{m.target.type}:{m.target.id}</span>
                    </div>
                    <div className="hf-card-body">{m.body}</div>
                  </div>
                ))}
                <div style={{ textAlign: 'center', color: 'var(--fg-3)', fontSize: 11, padding: '12px 0',
                              fontFamily: 'JetBrains Mono, monospace' }}>
                  click any element on canvas → comment
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
};

Object.assign(window, { HFV4: window.HFV4 });
