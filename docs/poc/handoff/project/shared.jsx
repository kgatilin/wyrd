/* Shared scenario + primitives for archai wireframes */

window.SCENARIO = {
  pr: {
    title: "Add OrderEvents stream + harden Payment retries",
    branch: "agent/order-events-2026-04-30",
    agent: "claude-haiku-4-5",
    summary: "Introduces an event stream for order lifecycle, splits " +
             "PaymentService.charge() into authorize() + capture(), " +
             "removes direct DB read from Notifier.",
    stats: { added: 18, removed: 5, changed: 9, comments: 3 }
  },

  boundedContexts: [
    { id: 'ordering', name: 'Ordering',      x: 40,  y: 80,  w: 540, h: 360 },
    { id: 'payments', name: 'Payments',      x: 600, y: 80,  w: 320, h: 240 },
    { id: 'notify',   name: 'Notifications', x: 600, y: 340, w: 320, h: 160 },
  ],

  components: [
    {
      id: 'api', name: 'CheckoutAPI', tech: 'Go · gRPC',
      desc: 'Edge service. Orchestrates checkout.',
      bc: 'ordering',
      x: 70, y: 130, w: 220, h: 86, wx: 280, hx: 240,
      internals: [
        { id: 'api.IPayClient', kind: 'iface', name: 'IPayClient',
          x: 16, y: 40, w: 110, h: 36,
          members: [
            { id: 'api.IPayClient.auth',    kind: 'method', name: 'authorize(amt)' },
            { id: 'api.IPayClient.capture', kind: 'method', name: 'capture(id)' },
          ]},
        { id: 'api.IEventBus', kind: 'iface', name: 'IEventBus', diff: 'added',
          x: 140, y: 40, w: 110, h: 36,
          members: [
            { id: 'api.IEventBus.publish', kind: 'method', name: 'publish(evt)', diff: 'added' },
          ]},
      ],
      ports: [
        { id: 'api.submit',  side: 'left',  y: 58,  name: 'SubmitOrder', kind: 'in'  },
        { id: 'api.status',  side: 'left',  y: 104, name: 'OrderStatus', kind: 'in' },
        { id: 'api.pay',     side: 'right', y: 58,  name: 'use Pay',     kind: 'out' },
        { id: 'api.events',  side: 'right', y: 104, name: 'use Events',  kind: 'out', diff: 'added' },
      ],
    },
    {
      id: 'orders', name: 'OrderService', tech: 'Go',
      desc: 'Order aggregate · state machine.',
      bc: 'ordering',
      x: 340, y: 130, w: 220, h: 86, wx: 280, hx: 280,
      diff: 'changed',
      internals: [
        { id: 'orders.Order', kind: 'class', name: 'Order',
          x: 16, y: 40, w: 110, h: 36,
          members: [
            { id: 'orders.Order.id',     kind: 'prop',   name: 'id : OrderId' },
            { id: 'orders.Order.items',  kind: 'prop',   name: 'items : LineItem[]' },
            { id: 'orders.Order.state',  kind: 'prop',   name: 'state : State' },
            { id: 'orders.Order.submit', kind: 'method', name: 'submit()' },
            { id: 'orders.Order.cancel', kind: 'method', name: 'cancel(reason)' },
          ]},
        { id: 'orders.IRepo', kind: 'iface', name: 'IOrderRepo',
          x: 140, y: 40, w: 110, h: 36,
          members: [
            { id: 'orders.IRepo.save', kind: 'method', name: 'save(order)' },
            { id: 'orders.IRepo.byId', kind: 'method', name: 'byId(id)' },
          ]},
        { id: 'orders.IEmitter', kind: 'iface', name: 'IEventEmitter', diff: 'added',
          x: 140, y: 100, w: 110, h: 36,
          members: [
            { id: 'orders.IEmitter.emit', kind: 'method', name: 'emit(evt)', diff: 'added' },
          ]},
      ],
      ports: [
        { id: 'orders.create',  side: 'left',  y: 58,  name: 'CreateOrder', kind: 'in' },
        { id: 'orders.cancel',  side: 'left',  y: 104, name: 'CancelOrder', kind: 'in' },
        { id: 'orders.emit',    side: 'right', y: 104, name: 'emit events', kind: 'out', diff: 'added' },
        { id: 'orders.dbread',  side: 'right', y: 150, name: 'ordersDB',    kind: 'out', diff: 'removed' },
      ],
    },
    {
      id: 'events', name: 'OrderEvents', tech: 'Kafka topic',
      desc: 'Append-only stream for order lifecycle.',
      bc: 'ordering',
      x: 340, y: 350, w: 220, h: 80, wx: 280, hx: 220,
      diff: 'added',
      internals: [
        { id: 'events.Created', kind: 'class', name: 'OrderCreated', diff: 'added',
          x: 16, y: 40, w: 110, h: 36,
          members: [
            { id: 'events.Created.id', kind: 'prop', name: 'orderId : OrderId', diff: 'added' },
            { id: 'events.Created.at', kind: 'prop', name: 'at : timestamp',    diff: 'added' },
          ]},
        { id: 'events.Shipped', kind: 'class', name: 'OrderShipped', diff: 'added',
          x: 140, y: 40, w: 110, h: 36,
          members: [
            { id: 'events.Shipped.id',  kind: 'prop', name: 'orderId',  diff: 'added' },
            { id: 'events.Shipped.via', kind: 'prop', name: 'carrier',  diff: 'added' },
          ]},
      ],
      ports: [
        { id: 'events.in',  side: 'left',  y: 58,  name: 'publish',   kind: 'in',  diff: 'added' },
        { id: 'events.in2', side: 'left',  y: 100, name: 'publish',   kind: 'in',  diff: 'added' },
        { id: 'events.out', side: 'right', y: 80,  name: 'subscribe', kind: 'out', diff: 'added' },
      ],
    },
    {
      id: 'pay', name: 'PaymentService', tech: 'Java · REST',
      desc: 'Card/bank charges + retry.',
      bc: 'payments',
      x: 640, y: 110, w: 240, h: 96, wx: 300, hx: 260,
      diff: 'changed',
      internals: [
        { id: 'pay.IGateway', kind: 'iface', name: 'IGateway',
          x: 16, y: 40, w: 130, h: 36,
          members: [
            { id: 'pay.IGateway.auth',    kind: 'method', name: 'authorize(amt)', diff: 'added' },
            { id: 'pay.IGateway.capture', kind: 'method', name: 'capture(id)',    diff: 'added' },
            { id: 'pay.IGateway.charge',  kind: 'method', name: 'charge(amt)',    diff: 'removed' },
            { id: 'pay.IGateway.refund',  kind: 'method', name: 'refund(id)' },
          ]},
        { id: 'pay.RetryPolicy', kind: 'class', name: 'RetryPolicy', diff: 'changed',
          x: 160, y: 40, w: 120, h: 36,
          members: [
            { id: 'pay.RetryPolicy.max',     kind: 'prop',   name: 'maxAttempts : int' },
            { id: 'pay.RetryPolicy.backoff', kind: 'prop',   name: 'backoff : Duration', diff: 'changed' },
            { id: 'pay.RetryPolicy.next',    kind: 'method', name: 'next(attempt)' },
          ]},
      ],
      ports: [
        { id: 'pay.auth',    side: 'left', y: 58,  name: 'authorize()', kind: 'in', diff: 'added' },
        { id: 'pay.capture', side: 'left', y: 100, name: 'capture()',   kind: 'in', diff: 'added' },
        { id: 'pay.charge',  side: 'left', y: 144, name: 'charge()',    kind: 'in', diff: 'removed' },
        { id: 'pay.refund',  side: 'left', y: 188, name: 'refund()',    kind: 'in' },
      ],
    },
    {
      id: 'notif', name: 'Notifier', tech: 'Node',
      desc: 'Emails / push on milestones.',
      bc: 'notify',
      x: 640, y: 370, w: 240, h: 86, wx: 280, hx: 220,
      diff: 'changed',
      internals: [
        { id: 'notif.Sub', kind: 'class', name: 'EventSubscriber', diff: 'added',
          x: 16, y: 40, w: 130, h: 36,
          members: [
            { id: 'notif.Sub.on',     kind: 'method', name: 'onOrderShipped(e)', diff: 'added' },
            { id: 'notif.Sub.on2',    kind: 'method', name: 'onOrderCreated(e)', diff: 'added' },
          ]},
        { id: 'notif.IMailer', kind: 'iface', name: 'IMailer',
          x: 160, y: 40, w: 100, h: 36,
          members: [
            { id: 'notif.IMailer.send', kind: 'method', name: 'send(to, tpl)' },
          ]},
      ],
      ports: [
        { id: 'notif.sub',    side: 'left',  y: 58,  name: 'subscribe(events)', kind: 'in', diff: 'added' },
        { id: 'notif.dbread', side: 'left',  y: 104, name: 'ordersDB read',     kind: 'in', diff: 'removed' },
        { id: 'notif.send',   side: 'right', y: 80,  name: 'SMTP / FCM',         kind: 'out' },
      ],
    },
  ],

  edges: [
    { id: 'e1', from: 'api',    to: 'orders', fromPort: 'api.submit',    toPort: 'orders.create', label: 'submit' },
    { id: 'e2', from: 'api',    to: 'pay',    fromPort: 'api.pay',       toPort: 'pay.auth',      label: 'authorize', diff: 'changed' },
    { id: 'e3', from: 'api',    to: 'events', fromPort: 'api.events',    toPort: 'events.in',     label: 'emit',      diff: 'added' },
    { id: 'e4', from: 'orders', to: 'events', fromPort: 'orders.emit',   toPort: 'events.in2',    label: 'shipped',   diff: 'added' },
    { id: 'e5', from: 'events', to: 'notif',  fromPort: 'events.out',    toPort: 'notif.sub',     label: 'subscribe', diff: 'added' },
    { id: 'e6', from: 'orders', to: 'notif',  fromPort: 'orders.dbread', toPort: 'notif.dbread',  label: 'direct read', diff: 'removed' },
  ],

  comments: [
    { id: 'c1', target: { type: 'port',   id: 'pay.auth' },        body: "Will authorize() implicitly hold funds? Need a TTL spec." },
    { id: 'c2', target: { type: 'edge',   id: 'e3' },              body: "Why does API emit OrderCreated and not OrderService?" },
    { id: 'c3', target: { type: 'member', id: 'pay.IGateway.auth' }, body: "What's the timeout? Idempotency key required?" },
  ],
};

window.findCmp = (id) => SCENARIO.components.find(c => c.id === id);

// Compute the dynamic expanded height for a component given which internals
// inside it are themselves expanded. Each expanded internal grows by N members.
window.computeCmpHeight = function(cmp, expandedInternals) {
  if (!expandedInternals) return cmp.hx;
  let extra = 0;
  cmp.internals.forEach(it => {
    if (expandedInternals.has(it.id)) {
      extra = Math.max(extra, (it.members?.length || 0) * 18);
    }
  });
  return cmp.hx + extra;
};

// ─── ComponentBlock ─────────────────────────────────────
window.ComponentBlock = function ComponentBlock({
  cmp, expanded, onToggle, onSelect, selected, showDiff,
  expandedInternals, onToggleInternal, onAddComment, commentTargets,
  dimmed, focused,
}) {
  const diffCls = showDiff && cmp.diff ? cmp.diff : '';
  const w = expanded ? cmp.wx : cmp.w;
  const h = expanded ? computeCmpHeight(cmp, expandedInternals) : cmp.h;
  const has = (id) => commentTargets && commentTargets.has(id);

  return (
    <div
      className={`cmp ${diffCls} ${expanded ? 'expanded' : ''} ${selected ? 'selected' : ''} ${dimmed ? 'dimmed' : ''} ${focused ? 'focused' : ''}`}
      style={{ left: cmp.x, top: cmp.y, width: w, height: h }}
      onClick={(e) => { e.stopPropagation(); onSelect && onSelect(cmp); }}
    >
      <div className="cmp-head"
           onClick={(e) => {
             e.stopPropagation();
             if (e.shiftKey) { onAddComment && onAddComment({ type: 'cmp', id: cmp.id }); return; }
             onSelect && onSelect(cmp);
           }}
           onDoubleClick={(e) => { e.stopPropagation(); onAddComment && onAddComment({ type: 'cmp', id: cmp.id }); }}>
        <div className="cmp-name">{cmp.name}</div>
        <span className="cmp-tech">{cmp.tech}</span>
        {has(cmp.id) && <span className="has-cmt" title="has comment">!</span>}
        <button className="expand-btn"
          onClick={(e) => { e.stopPropagation(); onToggle && onToggle(cmp.id); }}>
          {expanded ? '−' : '+'}
        </button>
      </div>

      {!expanded && <div className="cmp-desc">{cmp.desc}</div>}

      {expanded && (
        <div className="cmp-canvas">
          {cmp.internals.map(it => {
            const idiff = showDiff && it.diff ? it.diff : '';
            const isExp = expandedInternals && expandedInternals.has(it.id);
            const ih = isExp ? it.h + (it.members?.length || 0) * 18 + 4 : it.h;
            return (
              <div key={it.id}
                   className={`internal ${it.kind} ${idiff} ${isExp ? 'expanded' : ''}`}
                   style={{ left: it.x, top: it.y, width: it.w, height: ih }}>
                <div className="internal-head"
                     onClick={(e) => { e.stopPropagation(); onAddComment && onAddComment({ type: 'internal', id: it.id }); }}>
                  <span className="internal-kind">{it.kind === 'iface' ? '«I»' : 'C'}</span>
                  <span className="internal-name">{it.name}</span>
                  {has(it.id) && <span className="has-cmt sm">!</span>}
                  <span className="internal-toggle"
                        onClick={(e) => { e.stopPropagation(); onToggleInternal && onToggleInternal(it.id); }}>
                    {isExp ? '−' : '+'}
                  </span>
                </div>
                {isExp && (
                  <div className="member-list">
                    {(it.members || []).map(m => {
                      const mdiff = showDiff && m.diff ? m.diff : '';
                      return (
                        <div key={m.id} className={`member ${mdiff}`}
                             onClick={(e) => { e.stopPropagation(); onAddComment && onAddComment({ type: 'member', id: m.id }); }}>
                          <span className="member-kind">{m.kind === 'method' ? 'fn' : ':'}</span>
                          <span className="member-name">{m.name}</span>
                          {has(m.id) && <span className="has-cmt sm">!</span>}
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
        const py = expanded ? p.y : Math.min(p.y, cmp.h - 12);
        const pdiff = showDiff && p.diff ? p.diff : '';
        return (
          <div key={p.id} className={`port ${p.side} ${pdiff}`}
               data-port-id={p.id} style={{ top: py }}
               onClick={(e) => { e.stopPropagation(); onAddComment && onAddComment({ type: 'port', id: p.id }); }}>
            <span className="port-dot" />
            <span className="port-label">
              {p.name}
              {has(p.id) && <span className="has-cmt sm">!</span>}
            </span>
          </div>
        );
      })}
    </div>
  );
};

// ─── Edge endpoints ────────────────────────────────────
window.computeEdgePath = function(edge, expandedSet, components, expandedInternals) {
  const src = components.find(c => c.id === edge.from);
  const dst = components.find(c => c.id === edge.to);
  if (!src || !dst) return null;
  const portFor = (cmp, portId) => {
    const p = cmp.ports.find(p => p.id === portId);
    if (!p) return null;
    const isExp = expandedSet.has(cmp.id);
    const w = isExp ? cmp.wx : cmp.w;
    const h = isExp ? computeCmpHeight(cmp, expandedInternals) : cmp.h;
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

window.EdgeLayer = function EdgeLayer({ edges, components, expandedSet, expandedInternals, showDiff, onPickEdge, onAddComment, commentTargets, focusId, relatedIds }) {
  const has = (id) => commentTargets && commentTargets.has(id);
  const isRelated = (e) => !focusId || e.from === focusId || e.to === focusId;
  return (
    <svg className="edges-svg" width="100%" height="100%">
      <defs>
        {['arr', 'arr-add', 'arr-rem', 'arr-chg'].map(id => (
          <marker key={id} id={id} viewBox="0 0 10 10" refX="9" refY="5"
                  markerWidth="8" markerHeight="8" orient="auto-start-reverse">
            <path d="M 0 0 L 10 5 L 0 10 z"
                  className={`edge-arrow ${id === 'arr-add' ? 'added' : id === 'arr-rem' ? 'removed' : id === 'arr-chg' ? 'changed' : ''}`} />
          </marker>
        ))}
      </defs>
      {edges.map(e => {
        const r = computeEdgePath(e, expandedSet, components, expandedInternals);
        if (!r) return null;
        const diffCls = showDiff && e.diff ? e.diff : '';
        const marker = !showDiff || !e.diff ? 'url(#arr)' :
          e.diff === 'added' ? 'url(#arr-add)' :
          e.diff === 'removed' ? 'url(#arr-rem)' : 'url(#arr-chg)';
        return (
          <g key={e.id} style={{ pointerEvents: 'auto', cursor: 'pointer' }}
             className={focusId ? (isRelated(e) ? 'edge-focused' : 'edge-dimmed') : ''}
             onClick={() => onPickEdge && onPickEdge(e)}>
            <path d={r.path} className={`edge ${diffCls}`} markerEnd={marker} />
            {e.label && (
              <text x={r.mid.x} y={r.mid.y} className="edge-label" textAnchor="middle">
                {e.label}
              </text>
            )}
            {has(e.id) && (
              <g transform={`translate(${r.mid.x + 18} ${r.mid.y - 14})`}>
                <rect x="-7" y="-9" width="14" height="14" rx="6" fill="var(--chg)" stroke="var(--ink)" />
                <text x="0" y="2" textAnchor="middle" fontFamily="var(--hand)" fontSize="11" fontWeight="700">!</text>
              </g>
            )}
            <path d={r.path} className="edge-hit"
                  onClick={(ev) => { ev.stopPropagation(); onAddComment && onAddComment({ type: 'edge', id: e.id }); }} />
          </g>
        );
      })}
    </svg>
  );
};

window.BCGroups = function BCGroups({ show, contexts }) {
  if (!show) return null;
  return contexts.map(bc => (
    <div key={bc.id} className="bc-group" style={{ left: bc.x, top: bc.y, width: bc.w, height: bc.h }}>
      <span className="bc-label">{bc.name}</span>
    </div>
  ));
};

window.CommentPin = function CommentPin({ x, y, label, onClick, active }) {
  return (
    <div className={`comment-pin ${active ? 'active' : ''}`} style={{ left: x, top: y }} onClick={onClick}>
      {label}
    </div>
  );
};

window.CommentBubble = function CommentBubble({ x, y, comment, onClose }) {
  if (!comment) return null;
  return (
    <div className="comment-bubble" style={{ left: x, top: y }}>
      <div className="author">
        you · 2m ago
        <span style={{flex:1}} />
        <span style={{cursor:'pointer', color:'var(--ink-3)'}} onClick={onClose}>×</span>
      </div>
      <div className="body">{comment.body}</div>
      <div className="reply-row">
        <input placeholder="reply…" />
        <button>send</button>
      </div>
    </div>
  );
};

window.AppBar = function AppBar({ level, onLevel, commentCount }) {
  const levels = ['System', 'Container', 'Component'];
  return (
    <div className="appbar">
      <div className="brand">archai</div>
      <div className="crumbs">
        <span>checkout-platform</span><span className="sep">/</span>
        <span>main</span><span className="sep">←</span>
        <span style={{color:'var(--ink)'}}>{SCENARIO.pr.branch}</span>
      </div>
      <div style={{flex:1}} />
      {levels.map((l, i) => (
        <button key={l} className={`pill ${level === i ? 'active' : ''}`}
                onClick={() => onLevel && onLevel(i)}>L{i + 1} · {l}</button>
      ))}
      <span style={{width: 12}} />
      <button className="submit">
        Submit review<span className="count">{commentCount}</span>
      </button>
    </div>
  );
};

window.PrHeader = function PrHeader() {
  const { pr } = SCENARIO;
  return (
    <div className="pr-header">
      <span className="agent-tag">AGENT PR</span>
      <span className="pr-title">{pr.title}</span>
      <span className="pr-meta">by {pr.agent}</span>
      <span style={{flex:1}} />
      <span className="stat add">+{pr.stats.added} added</span>
      <span className="stat rem">−{pr.stats.removed} removed</span>
      <span className="stat chg">~{pr.stats.changed} changed</span>
      <span className="stat">{pr.stats.comments} comments</span>
    </div>
  );
};

Object.assign(window, {
  SCENARIO: window.SCENARIO,
  findCmp: window.findCmp,
  computeCmpHeight: window.computeCmpHeight,
  ComponentBlock: window.ComponentBlock,
  EdgeLayer: window.EdgeLayer,
  computeEdgePath: window.computeEdgePath,
  BCGroups: window.BCGroups,
  CommentPin: window.CommentPin,
  CommentBubble: window.CommentBubble,
  AppBar: window.AppBar,
  PrHeader: window.PrHeader,
});
