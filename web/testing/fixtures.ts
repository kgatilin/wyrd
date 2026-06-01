import type { UIGraph } from '../src/types';
import { fixture } from '../src/data/fixture';

/**
 * The rich diff dataset (PR + diffs + comments). Re-exported from the app's own
 * fixture so the suite and the app stay in lock-step. 5 components, 3 bounded
 * contexts, 6 edges (5 diffed), deriveChanges length 38.
 */
export const diffGraph: UIGraph = fixture;

/**
 * Non-diff graph: no `pr` (so showDiff=false → no PR header, no CHANGES tab, no
 * legend, no diff classes), 1 bounded context, 2 plain components.
 */
export const nonDiffGraph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [{ id: 'core', name: 'Core', x: 40, y: 40, w: 520, h: 240 }],
  components: [
    {
      id: 'svcA',
      name: 'ServiceA',
      tech: 'Go',
      desc: 'Plain service A.',
      bc: 'core',
      x: 70,
      y: 90,
      w: 220,
      h: 86,
      wx: 280,
      hx: 200,
      internals: [
        {
          id: 'svcA.Impl',
          kind: 'class',
          name: 'Impl',
          x: 16,
          y: 40,
          w: 110,
          h: 36,
          members: [{ id: 'svcA.Impl.run', kind: 'method', name: 'run()' }],
        },
      ],
      ports: [{ id: 'svcA.in', side: 'left', y: 58, name: 'callA', kind: 'in' }],
    },
    {
      id: 'svcB',
      name: 'ServiceB',
      tech: 'Go',
      desc: 'Plain service B.',
      bc: 'core',
      x: 340,
      y: 90,
      w: 220,
      h: 86,
      wx: 280,
      hx: 200,
      internals: [
        {
          id: 'svcB.Impl',
          kind: 'class',
          name: 'Impl',
          x: 16,
          y: 40,
          w: 110,
          h: 36,
          members: [{ id: 'svcB.Impl.run', kind: 'method', name: 'run()' }],
        },
      ],
      ports: [{ id: 'svcB.in', side: 'left', y: 58, name: 'callB', kind: 'in' }],
    },
  ],
  edges: [
    { id: 'e1', from: 'svcA', to: 'svcB', fromPort: 'svcA.out', toPort: 'svcB.in', label: 'calls' },
  ],
  comments: [],
};

/**
 * One component / one internal whose member name is 50+ chars, so toggling
 * fit-width visibly grows the internal card width (INTERNAL_W=180 → much wider).
 * Component id is the first (and only) one, which App auto-expands on load.
 */
const LONG_MEMBER_NAME =
  'reconcileOutstandingSettlementsWithLedgerSnapshot(window)'; // 56 chars

export const longMemberGraph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [{ id: 'ledger', name: 'Ledger', x: 40, y: 40, w: 600, h: 320 }],
  components: [
    {
      id: 'reconciler',
      name: 'Reconciler',
      tech: 'Go',
      desc: 'Reconciles settlements.',
      bc: 'ledger',
      x: 70,
      y: 90,
      w: 220,
      h: 86,
      wx: 320,
      hx: 220,
      internals: [
        {
          id: 'reconciler.Engine',
          kind: 'class',
          name: 'Engine',
          x: 16,
          y: 40,
          w: 110,
          h: 36,
          members: [
            { id: 'reconciler.Engine.long', kind: 'method', name: LONG_MEMBER_NAME },
            { id: 'reconciler.Engine.tick', kind: 'method', name: 'tick()' },
          ],
        },
      ],
      ports: [{ id: 'reconciler.in', side: 'left', y: 58, name: 'run', kind: 'in' }],
    },
  ],
  edges: [],
  comments: [],
};
