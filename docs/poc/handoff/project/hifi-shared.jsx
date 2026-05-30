/* archai hi-fi shared primitives */

window.HF = window.HF || {};

// reuse low-fi SCENARIO if present, else nothing
HF.S = window.SCENARIO;

HF.computeHeight = function(cmp, expandedInternals) {
  let extra = 0;
  cmp.internals.forEach(it => {
    if (expandedInternals && expandedInternals.has(it.id)) {
      extra = Math.max(extra, (it.members?.length || 0) * 18);
    }
  });
  return cmp.hx + extra;
};

HF.useExpanded = function(initial = []) {
  const [set, setSet] = React.useState(new Set(initial));
  const toggle = (id) => setSet(prev => {
    const n = new Set(prev); n.has(id) ? n.delete(id) : n.add(id); return n;
  });
  return [set, toggle, setSet];
};

// ─── Component ─────────────────────────────────────────
HF.Component = function HFComponent({
  cmp, expanded, onToggleExpand, expandedInternals, onToggleInternal,
  showDiff, onSelect, focused, dimmed, onAddComment, commentTargets,
  variant = 'v1',
}) {
  const diffCls = showDiff && cmp.diff ? cmp.diff : '';
  const w = expanded ? cmp.wx : cmp.w;
  const h = expanded ? HF.computeHeight(cmp, expandedInternals) : cmp.h;
  const has = (id) => commentTargets && commentTargets.has(id);

  return (
    <div className={`hf-cmp ${diffCls} ${focused ? 'focused' : ''} ${dimmed ? 'dimmed' : ''}`}
         style={{ left: cmp.x, top: cmp.y, width: w, height: h }}
         onClick={(e) => { e.stopPropagation(); onSelect && onSelect(cmp); }}>
      <div className="hf-cmp-head"
           onClick={(e) => {
             e.stopPropagation();
             if (e.shiftKey) { onAddComment && onAddComment({ type: 'cmp', id: cmp.id }, e); return; }
             onSelect && onSelect(cmp);
           }}
           onDoubleClick={(e) => { e.stopPropagation(); onAddComment && onAddComment({ type: 'cmp', id: cmp.id }, e); }}>
        <div className="hf-cmp-icon">{cmp.name[0]}</div>
        <div className="hf-cmp-name">{cmp.name}</div>
        <span className="hf-cmp-tech">{cmp.tech}</span>
        <span style={{ flex: 1 }} />
        {showDiff && cmp.diff && (
          <span className="hf-cmp-diff-tag">
            {cmp.diff === 'added' ? 'NEW' : cmp.diff === 'removed' ? 'DEL' : 'MOD'}
          </span>
        )}
        <button className="hf-cmp-expand"
                onClick={(e) => { e.stopPropagation(); onToggleExpand && onToggleExpand(cmp.id); }}>
          {expanded ? '−' : '+'}
        </button>
      </div>

      {!expanded && <div className="hf-cmp-desc">{cmp.desc}</div>}

      {expanded && (
        <div className="hf-cmp-canvas">
          {cmp.internals.map(it => {
            const idiff = showDiff && it.diff ? it.diff : '';
            const isExp = expandedInternals && expandedInternals.has(it.id);
            const ih = isExp ? 26 + (it.members?.length || 0) * 18 + 4 : 26;
            return (
              <div key={it.id}
                   className={`hf-internal ${it.kind} ${idiff}`}
                   style={{ left: it.x, top: it.y, width: it.w, height: ih }}>
                <div className="hf-internal-head"
                     onClick={(e) => { e.stopPropagation(); onAddComment && onAddComment({ type: 'internal', id: it.id }, e); }}>
                  <span className="hf-internal-kind">{it.kind === 'iface' ? 'iface' : 'class'}</span>
                  <span className="hf-internal-name">{it.name}</span>
                  {has(it.id) && <span className="hf-cmt-marker sm">!</span>}
                  <span className="hf-internal-toggle"
                        onClick={(e) => { e.stopPropagation(); onToggleInternal && onToggleInternal(it.id); }}>
                    {isExp ? '−' : '+'}
                  </span>
                </div>
                {isExp && (
                  <div className="hf-member-list">
                    {(it.members || []).map(m => {
                      const mdiff = showDiff && m.diff ? m.diff : '';
                      return (
                        <div key={m.id} className={`hf-member ${mdiff}`}
                             onClick={(e) => { e.stopPropagation(); onAddComment && onAddComment({ type: 'member', id: m.id }, e); }}>
                          <span className={`hf-member-kind ${m.kind === 'method' ? 'fn' : 'prop'}`}>
                            {m.kind === 'method' ? 'fn' : ':'}
                          </span>
                          <span className="hf-member-name">{m.name}</span>
                          {has(m.id) && <span className="hf-cmt-marker sm">!</span>}
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {cmp.ports.map(p => {
        const py = expanded ? p.y - 7 : Math.min(p.y, h - 14) - 7;
        const pdiff = showDiff && p.diff ? p.diff : '';
        return (
          <div key={p.id} className={`hf-port ${p.side} ${pdiff}`}
               style={{ top: py }}
               onClick={(e) => { e.stopPropagation(); onAddComment && onAddComment({ type: 'port', id: p.id }, e); }}>
            <span className="hf-port-dot" />
            <span className="hf-port-label">{p.name}{has(p.id) && <span className="hf-cmt-marker sm">!</span>}</span>
          </div>
        );
      })}

      {has(cmp.id) && <span className="hf-cmt-pin">!</span>}
    </div>
  );
};

// ─── Edge layer ─────────────────────────────────────────
HF.computeEdgePath = function(edge, expandedSet, expandedInternals) {
  const components = HF.S.components;
  const src = components.find(c => c.id === edge.from);
  const dst = components.find(c => c.id === edge.to);
  if (!src || !dst) return null;
  const portFor = (cmp, portId) => {
    const p = cmp.ports.find(p => p.id === portId);
    if (!p) return null;
    const isExp = expandedSet.has(cmp.id);
    const w = isExp ? cmp.wx : cmp.w;
    const h = isExp ? HF.computeHeight(cmp, expandedInternals) : cmp.h;
    const y = isExp ? cmp.y + p.y : cmp.y + h / 2;
    const x = p.side === 'left' ? cmp.x : cmp.x + w;
    return { x, y, side: p.side };
  };
  const s = portFor(src, edge.fromPort) || { x: src.x + src.w, y: src.y + src.h/2, side: 'right' };
  const d = portFor(dst, edge.toPort)   || { x: dst.x,         y: dst.y + dst.h/2, side: 'left'  };
  const dx = Math.max(40, Math.abs(d.x - s.x) * 0.4);
  const sx2 = s.side === 'right' ? s.x + dx : s.x - dx;
  const dx2 = d.side === 'left'  ? d.x - dx : d.x + dx;
  return {
    path: `M ${s.x} ${s.y} C ${sx2} ${s.y}, ${dx2} ${d.y}, ${d.x} ${d.y}`,
    s, d, mid: { x: (s.x + d.x) / 2, y: (s.y + d.y) / 2 - 6 }
  };
};

HF.EdgeLayer = function HFEdgeLayer({
  edges, expandedSet, expandedInternals, showDiff, focusId,
  flow = false, commentTargets, onAddComment,
}) {
  const has = (id) => commentTargets && commentTargets.has(id);
  const isRelated = (e) => !focusId || e.from === focusId || e.to === focusId;
  return (
    <svg className="edges-svg" width="100%" height="100%">
      <defs>
        {['arr', 'arr-add', 'arr-rem', 'arr-chg'].map(id => (
          <marker key={id} id={`hf-${id}`} viewBox="0 0 10 10" refX="9" refY="5"
                  markerWidth="7" markerHeight="7" orient="auto-start-reverse">
            <path d="M 0 0 L 10 5 L 0 10 z"
                  className={`hf-edge-arrow ${id === 'arr-add' ? 'added' : id === 'arr-rem' ? 'removed' : id === 'arr-chg' ? 'changed' : ''}`} />
          </marker>
        ))}
      </defs>
      {edges.map((e, i) => {
        const r = HF.computeEdgePath(e, expandedSet, expandedInternals);
        if (!r) return null;
        const diffCls = showDiff && e.diff ? e.diff : '';
        const marker = !showDiff || !e.diff ? 'url(#hf-arr)' :
          e.diff === 'added' ? 'url(#hf-arr-add)' :
          e.diff === 'removed' ? 'url(#hf-arr-rem)' : 'url(#hf-arr-chg)';
        const focused = focusId && isRelated(e);
        const dimmed = focusId && !isRelated(e);
        return (
          <g key={e.id} className={`${focused ? 'hf-edge-focused' : ''} ${dimmed ? 'hf-edge-dimmed' : ''}`}>
            <path id={`epath-${e.id}`} d={r.path}
                  className={`hf-edge ${diffCls}`} markerEnd={marker} />
            {flow && !dimmed && (
              <circle r="3" className={`hf-flow-dot ${diffCls}`}
                      style={{ offsetPath: `path("${r.path}")`, animationDelay: `${i * 0.4}s` }} />
            )}
            {e.label && (
              <text x={r.mid.x} y={r.mid.y} className="hf-edge-label" textAnchor="middle">
                {e.label}
              </text>
            )}
            {has(e.id) && (
              <g transform={`translate(${r.mid.x + 18} ${r.mid.y - 14})`}>
                <rect x="-7" y="-9" width="14" height="14" rx="6"
                      fill="var(--accent)" stroke="var(--bg-0)" strokeWidth="1.5" />
                <text x="0" y="2" textAnchor="middle"
                      fontFamily="JetBrains Mono, monospace" fontSize="9"
                      fontWeight="700" fill="white">!</text>
              </g>
            )}
            <path d={r.path} className="hf-edge-hit"
                  onClick={(ev) => { ev.stopPropagation(); onAddComment && onAddComment({ type: 'edge', id: e.id }, ev); }} />
          </g>
        );
      })}
    </svg>
  );
};

// ─── BC groups ─────────────────────────────────────────
HF.BCGroups = function({ show }) {
  if (!show) return null;
  return HF.S.boundedContexts.map(bc => (
    <div key={bc.id} className="hf-bc-group" style={{ left: bc.x, top: bc.y, width: bc.w, height: bc.h }}>
      <span className="hf-bc-label">{bc.name}</span>
    </div>
  ));
};

// ─── App bar ────────────────────────────────────────────
HF.AppBar = function({ level, onLevel, theme, onTheme, commentCount, onSubmit }) {
  const levels = ['L1', 'L2', 'L3'];
  const labels = ['System', 'Container', 'Component'];
  return (
    <div className="hf-appbar">
      <div className="hf-logo">A</div>
      <div className="hf-crumbs">
        <span>checkout-platform</span>
        <span className="sep">/</span>
        <span>main</span>
        <span className="sep">←</span>
        <span className="branch">agent/order-events</span>
      </div>
      <div className="hf-spacer" />
      <div className="hf-seg">
        {levels.map((l, i) => (
          <button key={l} className={level === i ? 'on' : ''}
                  onClick={() => onLevel && onLevel(i)}>
            {l} · {labels[i]}
          </button>
        ))}
      </div>
      <button className="hf-btn" onClick={onTheme} title="Toggle theme">
        {theme === 'dark' ? '☾' : '☀'}
      </button>
      <button className="hf-btn">Approve</button>
      <button className="hf-btn primary">
        Submit review<span className="count">{commentCount}</span>
      </button>
    </div>
  );
};

// ─── PR header ──────────────────────────────────────────
HF.PrHeader = function() {
  const { pr } = HF.S;
  return (
    <div className="hf-prheader">
      <span className="hf-pr-tag">AGENT PR</span>
      <span className="hf-pr-title">{pr.title}</span>
      <span className="hf-pr-meta">opened by {pr.agent} · 12m ago</span>
      <span style={{ flex: 1 }} />
      <span className="hf-stat add">+{pr.stats.added}</span>
      <span className="hf-stat rem">−{pr.stats.removed}</span>
      <span className="hf-stat chg">~{pr.stats.changed}</span>
      <span className="hf-stat com">💬 {pr.stats.comments}</span>
    </div>
  );
};

// ─── Tree (left panel) ──────────────────────────────────
HF.Tree = function({ showDiff }) {
  return (
    <div className="hf-tree">
      {HF.S.boundedContexts.map(bc => (
        <div key={bc.id}>
          <div className="hf-tree-row bc">
            <span className="chev">▾</span>
            <span className="ico">▣</span>
            <span className="name">{bc.name}</span>
          </div>
          {HF.S.components.filter(c => c.bc === bc.id).map(c => {
            const dcls = showDiff && c.diff ? c.diff : '';
            return (
              <div key={c.id} className={`hf-tree-row cmp ${dcls}`}
                   style={{ paddingLeft: 20 }}>
                <span className="ico">◆</span>
                <span className="name">{c.name}</span>
                {showDiff && c.diff && (
                  <span className="badge">
                    {c.diff === 'added' ? '+' : c.diff === 'removed' ? '−' : '~'}
                  </span>
                )}
              </div>
            );
          })}
        </div>
      ))}
    </div>
  );
};

// ─── Canvas legend / toolbar ──────────────────────────
HF.Legend = function({ showDiff }) {
  if (!showDiff) return null;
  return (
    <div className="hf-canvas-legend">
      <div className="hf-legend-item">
        <span className="hf-legend-swatch" style={{ background: 'var(--add-fg)' }} /> added
      </div>
      <div className="hf-legend-item">
        <span className="hf-legend-swatch" style={{ background: 'var(--rem-fg)' }} /> removed
      </div>
      <div className="hf-legend-item">
        <span className="hf-legend-swatch" style={{ background: 'var(--chg-fg)' }} /> changed
      </div>
    </div>
  );
};

HF.CanvasToolbar = function({ zoom = 100 }) {
  return (
    <div className="hf-canvas-toolbar">
      <button title="Zoom out">−</button>
      <button className="zoom">{zoom}%</button>
      <button title="Zoom in">+</button>
      <button title="Fit">⊡</button>
      <button title="Mini-map">⊞</button>
    </div>
  );
};

// ─── Derive change list ─────────────────────────────────
HF.deriveChanges = function() {
  const out = [];
  HF.S.components.forEach(c => {
    if (c.diff) out.push({ id: `cmp-${c.id}`, kind: c.diff, name: c.name,
      where: `component · ${HF.S.boundedContexts.find(b => b.id === c.bc).name}`, cmp: c.id });
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
        where: `port · ${c.name}`, cmp: c.id });
    });
  });
  HF.S.edges.forEach(e => {
    if (e.diff) out.push({ id: `edg-${e.id}`, kind: e.diff,
      name: `${HF.S.components.find(c=>c.id===e.from).name} → ${HF.S.components.find(c=>c.id===e.to).name}`,
      where: `connection · ${e.label || ''}`, cmp: e.from });
  });
  return out;
};

window.HF = HF;
