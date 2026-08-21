import { describe, expect, it } from 'vitest';
import {
  buildLinks,
  isolatedComponents,
  linkId,
  shortKind,
  slotCount,
  type EventFlow,
  type EventModel,
} from './eventModel';

function flow(from: string, to: string, kind: string, extra: Partial<EventFlow> = {}): EventFlow {
  return { from, to, kind, trigger: false, ...extra };
}

describe('buildLinks', () => {
  it('draws one line per component pair, not one per kind', () => {
    const links = buildLinks([
      flow('billing', 'ledger', 'ledger.entry.post'),
      flow('billing', 'ledger', 'ledger.entry.void'),
      flow('billing', 'analytics', 'billing.invoice.issued'),
    ]);

    expect(links.map((l) => l.id)).toEqual([
      linkId('billing', 'analytics'),
      linkId('billing', 'ledger'),
    ]);
    expect(links[1].kinds.map((k) => k.kind)).toEqual(['ledger.entry.post', 'ledger.entry.void']);
  });

  it('treats a pair as a trigger when any kind on it triggers', () => {
    const links = buildLinks([
      flow('billing', 'ledger', 'ledger.entry.post', { trigger: false }),
      flow('billing', 'ledger', 'ledger.entry.void', { trigger: true }),
    ]);

    expect(links[0].trigger).toBe(true);
  });

  it('carries the worst health on the pair, so one bad kind is visible', () => {
    const links = buildLinks([
      flow('a', 'b', 'x', { health: 'ok' }),
      flow('a', 'b', 'y', { health: 'ambiguous' }),
      flow('a', 'b', 'z', { health: 'starved' }),
    ]);

    expect(links[0].health).toBe('ambiguous');
  });

  it('defaults an unstated health to ok rather than dropping the link', () => {
    expect(buildLinks([flow('a', 'b', 'x')])[0].health).toBe('ok');
  });
});

describe('isolatedComponents', () => {
  it('names the declarations nothing reaches and that reach nothing', () => {
    const model: EventModel = {
      components: [{ id: 'a', has_state: false }, { id: 'b', has_state: false }, { id: 'c', has_state: false }],
      flows: [flow('a', 'b', 'x')],
      kinds: [],
    };

    expect([...isolatedComponents(model)]).toEqual(['c']);
  });
});

describe('shortKind', () => {
  it('keeps the last two segments, which is what fits on an edge', () => {
    expect(shortKind('billing.invoice.issued')).toBe('invoice.issued');
  });

  it('leaves a short name alone', () => {
    expect(shortKind('invoice.issued')).toBe('invoice.issued');
    expect(shortKind('issued')).toBe('issued');
  });
});

describe('slotCount', () => {
  it('counts all three lists, and a component declaring nothing counts zero', () => {
    expect(
      slotCount({
        id: 'billing',
        has_state: true,
        inputs: [{ kind: 'a' }],
        outputs: [{ kind: 'b' }, { kind: 'c' }],
        state_events: [{ kind: 'b' }],
      })
    ).toBe(4);
    expect(slotCount({ id: 'empty', has_state: false })).toBe(0);
  });
});
