import { describe, expect, it } from 'vitest';
import {
  buildLinks,
  diffCounts,
  linksInDiff,
  healthCounts,
  isolatedComponents,
  linkId,
  movedFields,
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

  it('ranks a broken wire above every counting state', () => {
    const links = buildLinks([
      flow('a', 'b', 'x', { health: 'unwired' }),
      flow('a', 'b', 'y', { health: 'ambiguous' }),
      flow('a', 'b', 'z', { health: 'orphan' }),
    ]);

    expect(links[0].health).toBe('unwired');
  });
});

describe('link marks', () => {
  it('marks a pair removed only when everything on it is gone', () => {
    const gone = buildLinks([
      flow('a', 'b', 'x', { diff: 'removed' }),
      flow('a', 'b', 'y', { diff: 'removed' }),
    ]);
    expect(gone[0].diff).toBe('removed');

    // One kind dropped out of two leaves the pair standing. Drawing it as
    // removed would say the two components stopped talking, which is false.
    const partial = buildLinks([
      flow('a', 'b', 'x', { diff: 'removed' }),
      flow('a', 'b', 'y'),
    ]);
    expect(partial[0].diff).toBe('changed');
  });

  it('marks a pair added only when every kind on it is new', () => {
    expect(buildLinks([flow('a', 'b', 'x', { diff: 'added' })])[0].diff).toBe('added');
    expect(buildLinks([flow('a', 'b', 'x', { diff: 'added' }), flow('a', 'b', 'y')])[0].diff).toBe('changed');
  });

  it('leaves a pair whose kinds all stayed put unmarked', () => {
    expect(buildLinks([flow('a', 'b', 'x'), flow('a', 'b', 'y')])[0].diff).toBeUndefined();
  });
});

describe('movedFields', () => {
  it('reports only the fields that differ', () => {
    const moved = movedFields(
      { pattern: 'app.new.thing', delivery: 'broadcast' },
      { pattern: 'app.old.thing', delivery: 'broadcast' },
      ['pattern', 'delivery']
    );

    expect(moved).toEqual([{ field: 'pattern', before: 'app.old.thing', after: 'app.new.thing' }]);
  });

  it('says nothing at all when there is no previous value to compare', () => {
    expect(movedFields({ pattern: 'x' }, undefined, ['pattern'])).toEqual([]);
  });

  it('compares lists by their contents, not by identity', () => {
    expect(movedFields({ subjects: ['a', 'b'] }, { subjects: ['a', 'b'] }, ['subjects'])).toEqual([]);
    expect(movedFields({ subjects: ['a'] }, { subjects: ['a', 'b'] }, ['subjects'])).toEqual([
      { field: 'subjects', before: 'a, b', after: 'a' },
    ]);
  });
});

describe('diffCounts', () => {
  it('counts components, kinds and pairs together', () => {
    const counts = diffCounts({
      components: [
        { id: 'a', has_state: false, diff: 'added' },
        { id: 'b', has_state: false, diff: 'changed' },
        { id: 'c', has_state: false },
      ],
      kinds: [{ name: 'k', diff: 'removed' }],
      flows: [{ from: 'a', to: 'b', kind: 'k', trigger: true, diff: 'added' }],
    });

    expect(counts).toEqual({ added: 2, changed: 1, removed: 1 });
  });

  // The reason pairs are counted at all. Flows used to be excluded on the
  // reasoning that an edge is not a thing a reader counts, it is what two
  // counted things do — which holds right up until a branch moves who folds
  // what. Then every component and every kind survives, only the wiring moved,
  // and a header counting the other two units reports nothing changed while the
  // canvas draws a dozen faded lines.
  it('sees a rewiring that changed no component and no kind', () => {
    const counts = diffCounts({
      components: [
        { id: 'a', has_state: false },
        { id: 'b', has_state: false },
      ],
      kinds: [{ name: 'k' }],
      flows: [{ from: 'a', to: 'b', kind: 'k', trigger: false, diff: 'removed' }],
    });

    expect(counts).toEqual({ added: 0, changed: 0, removed: 1 });
  });

  // Counted per pair, not per flow, because that is the unit the canvas draws
  // and the unit clicking the chip opens.
  it('counts a pair once however many kinds travel it', () => {
    const counts = diffCounts({
      components: [
        { id: 'a', has_state: false },
        { id: 'b', has_state: false },
      ],
      kinds: [],
      flows: [
        { from: 'a', to: 'b', kind: 'k1', trigger: true, diff: 'added' },
        { from: 'a', to: 'b', kind: 'k2', trigger: false, diff: 'added' },
        { from: 'a', to: 'b', kind: 'k3', trigger: false, diff: 'added' },
      ],
    });

    expect(counts).toEqual({ added: 1, changed: 0, removed: 0 });
  });
});

describe('linksInDiff', () => {
  // A pair is `changed` when its kinds did not all move the same way, so the
  // list behind a chip has to agree with the mark the canvas painted.
  it('lists a pair under the mark the canvas drew it with', () => {
    const model = {
      components: [
        { id: 'a', has_state: false },
        { id: 'b', has_state: false },
      ],
      kinds: [],
      flows: [
        { from: 'a', to: 'b', kind: 'k1', trigger: true, diff: 'removed' as const },
        { from: 'a', to: 'b', kind: 'k2', trigger: false },
      ],
    };

    expect(linksInDiff(model, 'removed')).toEqual([]);
    expect(linksInDiff(model, 'changed').map((l) => l.id)).toEqual(['a \u2192 b']);
  });
});

describe('healthCounts', () => {
  it('counts each state on its own, unwired included', () => {
    const counts = healthCounts([
      { name: 'a', health: 'unwired' },
      { name: 'b', health: 'unwired' },
      { name: 'c', health: 'orphan' },
      { name: 'd', health: 'ok' },
      { name: 'e' },
    ]);

    expect(counts).toEqual({ ok: 1, orphan: 1, starved: 0, ambiguous: 0, unwired: 2 });
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
