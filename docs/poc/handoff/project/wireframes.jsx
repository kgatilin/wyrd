/* Wireframes — archai */

const useExpanded = (initial = []) => {
  const [set, setSet] = React.useState(new Set(initial));
  const toggle = (id) => setSet(prev => {
    const n = new Set(prev);
    n.has(id) ? n.delete(id) : n.add(id);
    return n;
  });
  return [set, toggle, setSet];
};

const deriveChanges = () => {
  const out = [];
  SCENARIO.components.forEach(c => {
    if (c.diff) out.push({ id: `cmp-${c.id}`, kind: c.diff, name: c.name,
      where: `component · ${SCENARIO.boundedContexts.find(b => b.id === c.bc).name}`, cmp: c.id });
    c.internals.forEach(i => {
      if (i.diff) out.push({ id: `int-${i.id}`, kind: i.diff, name: i.name,
        where: `${i.kind} · ${c.name}`, cmp: c.id, internal: i.id });
      (i.members || []).forEach(m => {
        if (m.diff) out.push({ id: `mem-${m.id}`, kind: m.diff, name: m.name,
          where: `${m.kind} · ${i.name}`, cmp: c.id, internal: i.id, member: m.id });
      });
    });
    c.ports.forEach(p => {
      if (p.diff) out.push({ id: `port-${p.id}`, kind: p.diff, name: p.name,
        where: `port · ${c.name}`, cmp: c.id, port: p.id });
    });
  });
  SCENARIO.edges.forEach(e => {
    if (e.diff) out.push({ id: `edg-${e.id}`, kind: e.diff,
      name: `${SCENARIO.components.find(c=>c.id===e.from).name} → ${SCENARIO.components.find(c=>c.id===e.to).name}`,
      where: `connection · ${e.label || ''}`, cmp: e.from });
  });
  return out;
};

const COMMENT_PINS = {
  'c1': { x: 632, y: 162 },
  'c2': { x: 410, y: 295 },
  'c3': { x: 540, y: 360 },
};

// ═══════════════════════════════════════════════════════════
// W1 — Codebase IDE
// ═══════════════════════════════════════════════════════════
window.W1 = function W1({ tweaks }) {
  const [level, setLevel] = React.useState(2);
  const [expanded, toggle] = useExpanded(['orders']);
  const [expandedInternals, toggleInternal, setExpandedInternals] = useExpanded([]);

  // Auto-expand all internals of any newly-expanded component
  React.useEffect(() => {
    setExpandedInternals(prev => {
      const n = new Set(prev);
      expanded.forEach(cid => {
        const cmp = findCmp(cid);
        if (cmp) cmp.internals.forEach(i => n.add(i.id));
      });
      return n;
    });
  }, [expanded]);
  const [activeComment, setActiveComment] = React.useState('c1');
  const [activeChange, setActiveChange] = React.useState(null);
  const [rightTab, setRightTab] = React.useState('comments');
  const [leftCol, setLeftCol] = React.useState(false);
  const [rightCol, setRightCol] = React.useState(false);
  const [pendingComment, setPendingComment] = React.useState(null);
  const [focusedCmp, setFocusedCmp] = React.useState(null);
  const canvasRef = React.useRef(null);

  // compute related components for focus mode
  const relatedToFocus = React.useMemo(() => {
    if (!focusedCmp) return null;
    const rel = new Set([focusedCmp]);
    SCENARIO.edges.forEach(e => {
      if (e.from === focusedCmp) rel.add(e.to);
      if (e.to === focusedCmp) rel.add(e.from);
    });
    return rel;
  }, [focusedCmp]);

  const showDiff = tweaks.diff;
  const showBC   = tweaks.bc;
  const changes  = React.useMemo(deriveChanges, []);
  const commentTargets = React.useMemo(
    () => new Set(SCENARIO.comments.map(c => c.target.id)), []);

  const filteredCmps = tweaks.changesOnly
    ? SCENARIO.components.filter(c => c.diff || SCENARIO.edges.some(e =>
        (e.from === c.id || e.to === c.id) && e.diff))
    : SCENARIO.components;
  const filteredIds = new Set(filteredCmps.map(c => c.id));
  const filteredEdges = SCENARIO.edges.filter(e =>
    filteredIds.has(e.from) && filteredIds.has(e.to));

  // Click change row → expand needed component(s) and scroll into view
  const goToChange = (ch) => {
    setActiveChange(ch.id);
    if (ch.cmp && (ch.internal || ch.member || ch.port)) {
      if (!expanded.has(ch.cmp)) toggle(ch.cmp);
    }
    if (ch.member && !expandedInternals.has(ch.internal)) {
      toggleInternal(ch.internal);
    }
    // scroll to center
    setTimeout(() => {
      const cmp = findCmp(ch.cmp);
      if (!cmp || !canvasRef.current) return;
      const cx = cmp.x + cmp.w / 2;
      const cy = cmp.y + cmp.h / 2;
      canvasRef.current.scrollTo({
        left: cx - canvasRef.current.clientWidth / 2,
        top:  cy - canvasRef.current.clientHeight / 2,
        behavior: 'smooth'
      });
    }, 200);
  };

  const startComment = (target) => {
    setPendingComment({ target, body: '' });
    setRightTab('comments');
    setRightCol(false);
  };

  return (
    <>
      <AppBar level={level} onLevel={setLevel} commentCount={SCENARIO.comments.length} />
      {showDiff && <PrHeader />}
      <div className="stage">
        <div className={`side ${leftCol ? 'collapsed' : ''}`}>
          <button className="side-toggle" onClick={() => setLeftCol(!leftCol)}>
            {leftCol ? '›' : '‹'}
          </button>
          <span className="vlabel">explorer</span>
          <h4>Bounded Contexts</h4>
          {SCENARIO.boundedContexts.map(bc => (
            <div key={bc.id} style={{ marginBottom: 8 }}>
              <div className="item" style={{ fontWeight: 600 }}><span className="dot" />{bc.name}</div>
              <div className="nest">
                {SCENARIO.components.filter(c => c.bc === bc.id).map(c => (
                  <div key={c.id}
                       className={`item ${showDiff && c.diff ? c.diff : ''}`}>
                    <span className="dot" />{c.name}
                  </div>
                ))}
              </div>
            </div>
          ))}
          <h4 style={{ marginTop: 14 }}>Filters</h4>
          <label className="item"><input type="checkbox" checked={tweaks.changesOnly} readOnly /> changes + neighbors</label>
          <label className="item"><input type="checkbox" checked={showBC} readOnly /> bounded contexts</label>
          <label className="item"><input type="checkbox" checked={showDiff} readOnly /> diff coloring</label>
        </div>

        <div ref={canvasRef} style={{ position: 'relative', flex: 1, overflow: 'auto', minWidth: 0 }}>
          <div style={{ position: 'relative', width: 1100, height: 600 }}>
            <BCGroups show={showBC} contexts={SCENARIO.boundedContexts} />
            <EdgeLayer edges={filteredEdges} components={SCENARIO.components}
                       expandedSet={expanded} expandedInternals={expandedInternals}
                       showDiff={showDiff} commentTargets={commentTargets}
                       focusId={focusedCmp}
                       onAddComment={startComment} />
            {filteredCmps.map(c => (
              <ComponentBlock key={c.id} cmp={c}
                expanded={expanded.has(c.id)}
                onToggle={toggle}
                expandedInternals={expandedInternals}
                onToggleInternal={toggleInternal}
                selected={(activeChange && changes.find(ch => ch.id === activeChange)?.cmp === c.id)}
                focused={focusedCmp === c.id}
                dimmed={focusedCmp && relatedToFocus && !relatedToFocus.has(c.id)}
                onSelect={(cmp) => setFocusedCmp(prev => prev === cmp.id ? null : cmp.id)}
                showDiff={showDiff}
                commentTargets={commentTargets}
                onAddComment={startComment} />
            ))}
            {SCENARIO.comments.map(cm => {
              const p = COMMENT_PINS[cm.id]; if (!p) return null;
              const idx = SCENARIO.comments.findIndex(x => x.id === cm.id) + 1;
              return <CommentPin key={cm.id} x={p.x} y={p.y} label={idx}
                                 active={activeComment === cm.id}
                                 onClick={() => { setActiveComment(cm.id); setRightTab('comments'); }} />;
            })}
            <div className="handnote" style={{ left: 350, top: 215 }}>
              click component → focus · double-click element → comment
            </div>
          </div>
        </div>

        <div className={`side right ${rightCol ? 'collapsed' : ''}`}>
          <button className="side-toggle" onClick={() => setRightCol(!rightCol)}>
            {rightCol ? '‹' : '›'}
          </button>
          <span className="vlabel">review</span>

          <div className="seg">
            <button className={rightTab === 'comments' ? 'on' : ''}
                    onClick={() => setRightTab('comments')}>
              COMMENTS · {SCENARIO.comments.length}
            </button>
            <button className={rightTab === 'changes' ? 'on' : ''}
                    onClick={() => setRightTab('changes')}>
              CHANGES · {changes.length}
            </button>
          </div>

          {rightTab === 'comments' && (
            <>
              {pendingComment && (
                <div className="comment-card active" style={{ borderColor: 'var(--accent)' }}>
                  <div className="meta">
                    <span style={{ fontWeight: 700, color: 'var(--accent)' }}>NEW</span>
                    <span style={{ flex: 1 }} />
                    <span className="target">{pendingComment.target.type}:{pendingComment.target.id}</span>
                  </div>
                  <textarea placeholder="write a comment…" autoFocus
                    style={{ width: '100%', minHeight: 50, fontFamily: 'var(--hand)',
                             fontSize: 14, border: '1px solid var(--rule)',
                             padding: 4, background: 'var(--paper-2)' }} />
                  <div style={{ display: 'flex', gap: 6, marginTop: 4 }}>
                    <button style={{ flex: 1, padding: '4px 8px', border: '1px solid var(--ink)',
                                    background: 'var(--ink)', color: 'var(--paper)', fontFamily: 'var(--mono)', fontSize: 10 }}
                            onClick={() => setPendingComment(null)}>save</button>
                    <button style={{ padding: '4px 8px', border: '1px solid var(--rule)',
                                    background: 'var(--paper)', fontFamily: 'var(--mono)', fontSize: 10 }}
                            onClick={() => setPendingComment(null)}>cancel</button>
                  </div>
                </div>
              )}
              {SCENARIO.comments.map((cm, i) => (
                <div key={cm.id}
                     className={`comment-card ${activeComment === cm.id ? 'active' : ''}`}
                     onClick={() => setActiveComment(cm.id)}>
                  <div className="meta">
                    <span style={{ fontWeight: 700 }}>#{i + 1} you</span>
                    <span>· 2m ago</span>
                    <span style={{ flex: 1 }} />
                    <span className="target">{cm.target.type}:{cm.target.id}</span>
                  </div>
                  <div className="body">{cm.body}</div>
                </div>
              ))}
              <div className="add-comment">
                hover an element on canvas → "+" to comment
              </div>
            </>
          )}

          {rightTab === 'changes' && (
            <>
              {changes.map(ch => (
                <div key={ch.id}
                     className={`change-card ${activeChange === ch.id ? 'active' : ''}`}
                     onClick={() => goToChange(ch)}>
                  <div className="row1">
                    <span className={`badge ${ch.kind}`}>
                      {ch.kind === 'added' ? '+ add' : ch.kind === 'removed' ? '− rem' : '~ chg'}
                    </span>
                    <span className="name">{ch.name}</span>
                  </div>
                  <div className="where">{ch.where}</div>
                </div>
              ))}
            </>
          )}
        </div>
      </div>
    </>
  );
};

// ═══════════════════════════════════════════════════════════
// W2 — Whiteboard
// ═══════════════════════════════════════════════════════════
window.W2 = function W2({ tweaks }) {
  const [level, setLevel] = React.useState(2);
  const [expanded, toggle] = useExpanded(['pay']);
  const [expandedInternals, toggleInternal, setExpInt2] = useExpanded([]);
  const [focusedCmp, setFocusedCmp] = React.useState(null);
  React.useEffect(() => {
    setExpInt2(prev => {
      const n = new Set(prev);
      expanded.forEach(cid => {
        const cmp = findCmp(cid);
        if (cmp) cmp.internals.forEach(i => n.add(i.id));
      });
      return n;
    });
  }, [expanded]);
  const relatedToFocus = React.useMemo(() => {
    if (!focusedCmp) return null;
    const rel = new Set([focusedCmp]);
    SCENARIO.edges.forEach(e => {
      if (e.from === focusedCmp) rel.add(e.to);
      if (e.to === focusedCmp) rel.add(e.from);
    });
    return rel;
  }, [focusedCmp]);
  const [openComment, setOpenComment] = React.useState('c1');
  const showDiff = tweaks.diff;
  const commentTargets = React.useMemo(
    () => new Set(SCENARIO.comments.map(c => c.target.id)), []);
  const bubblePos = openComment ? { x: COMMENT_PINS[openComment].x + 28, y: COMMENT_PINS[openComment].y - 8 } : null;

  return (
    <>
      <AppBar level={level} onLevel={setLevel} commentCount={3} />
      <div className="stage">
        <BCGroups show={tweaks.bc} contexts={SCENARIO.boundedContexts} />
        <EdgeLayer edges={SCENARIO.edges} components={SCENARIO.components}
                   expandedSet={expanded} expandedInternals={expandedInternals}
                   showDiff={showDiff} commentTargets={commentTargets}
                   focusId={focusedCmp} />
        {SCENARIO.components.map(c => (
          <ComponentBlock key={c.id} cmp={c}
            expanded={expanded.has(c.id)} onToggle={toggle}
            expandedInternals={expandedInternals} onToggleInternal={toggleInternal}
            focused={focusedCmp === c.id}
            dimmed={focusedCmp && relatedToFocus && !relatedToFocus.has(c.id)}
            onSelect={(cmp) => setFocusedCmp(prev => prev === cmp.id ? null : cmp.id)}
            showDiff={showDiff} commentTargets={commentTargets} />
        ))}

        <div className="float-tl floatcard" style={{ display: 'flex', gap: 8, alignItems: 'center', maxWidth: 460 }}>
          <span className="agent-tag">AGENT PR</span>
          <span style={{ fontFamily: 'var(--hand)', fontSize: 16 }}>{SCENARIO.pr.title}</span>
          <span className="tag" style={{ color: 'var(--add)', borderColor: 'var(--add)' }}>+{SCENARIO.pr.stats.added}</span>
          <span className="tag" style={{ color: 'var(--rem)', borderColor: 'var(--rem)' }}>−{SCENARIO.pr.stats.removed}</span>
          <span className="tag" style={{ color: 'var(--chg)', borderColor: 'var(--chg)' }}>~{SCENARIO.pr.stats.changed}</span>
        </div>
        <div className="float-tr floatcard" style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <div style={{ fontSize: 9, color: 'var(--ink-2)', letterSpacing: 1 }}>FILTER</div>
          <label><input type="checkbox" checked={tweaks.changesOnly} readOnly /> changes + neighbors</label>
          <label><input type="checkbox" checked={tweaks.bc} readOnly /> show contexts</label>
          <label><input type="checkbox" checked={showDiff} readOnly /> diff coloring</label>
        </div>
        <div className="float-bl floatcard">
          <div style={{ fontSize: 9, color: 'var(--ink-2)', letterSpacing: 1, marginBottom: 4 }}>LEGEND</div>
          <div style={{ display: 'flex', gap: 10, alignItems: 'center', fontSize: 10 }}>
            <span><span style={{display:'inline-block',width:10,height:10,background:'var(--add-soft)',border:'1.5px solid var(--add)',verticalAlign:'middle'}}/> added</span>
            <span><span style={{display:'inline-block',width:10,height:10,background:'var(--rem-soft)',border:'1.5px solid var(--rem)',verticalAlign:'middle'}}/> removed</span>
            <span><span style={{display:'inline-block',width:10,height:10,background:'var(--chg-soft)',border:'1.5px solid var(--chg)',verticalAlign:'middle'}}/> changed</span>
          </div>
        </div>
        <div className="float-br floatcard" style={{ display: 'flex', gap: 6 }}>
          <button className="pill" style={{ borderColor: 'var(--ink)' }}>−</button>
          <button className="pill" style={{ borderColor: 'var(--ink)' }}>fit</button>
          <button className="pill" style={{ borderColor: 'var(--ink)' }}>+</button>
        </div>

        {SCENARIO.comments.map((cm, i) => {
          const p = COMMENT_PINS[cm.id]; if (!p) return null;
          return <CommentPin key={cm.id} x={p.x} y={p.y} label={i + 1}
                             active={openComment === cm.id}
                             onClick={() => setOpenComment(cm.id)} />;
        })}
        {bubblePos && (
          <CommentBubble x={bubblePos.x} y={bubblePos.y}
            comment={SCENARIO.comments.find(c => c.id === openComment)}
            onClose={() => setOpenComment(null)} />
        )}
        <div className="handnote" style={{ left: 280, top: 60 }}>
          click component → focus · double-click → comment · drag-to-pan
        </div>
      </div>
    </>
  );
};

Object.assign(window, { W1: window.W1, W2: window.W2 });
