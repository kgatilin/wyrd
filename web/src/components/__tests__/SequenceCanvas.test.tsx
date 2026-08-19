import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render } from '@testing-library/react';
import { SequenceCanvas, type SequenceEntry } from '../SequenceCanvas';

afterEach(cleanup);

function entry(label: string, lines: string[]): SequenceEntry {
  return { label, mermaid: ['sequenceDiagram', ...lines.map((l) => `    ${l}`)].join('\n'), hasCalls: true };
}

/** Lifeline x positions, in declaration order. */
function columns(container: HTMLElement): number[] {
  return [...container.querySelectorAll('line.hf-seq-life')].map((l) => Number(l.getAttribute('x1')));
}

function messages(container: HTMLElement) {
  return [...container.querySelectorAll('line.hf-seq-msg')].map((l) => ({
    x1: Number(l.getAttribute('x1')),
    x2: Number(l.getAttribute('x2')),
    back: l.classList.contains('hf-seq-msg-back'),
  }));
}

describe('SequenceCanvas', () => {
  it('draws a call back to an earlier lifeline right-to-left and marks it', () => {
    const { container } = render(
      <SequenceCanvas
        entries={[
          entry('Dispatch', [
            'participant p0 as mcp.Dispatch',
            'participant p1 as mcp.handleSearch',
            'participant p2 as retrieval.Service',
            'p0->>p1: handleSearch()',
            'p1->>p2: Search() [via Searcher]',
            'p2->>p0: Dispatch() (cycle)',
          ]),
        ]}
      />
    );

    const msgs = messages(container);
    expect(msgs).toHaveLength(3);
    expect(msgs.slice(0, 2).map((m) => m.back)).toEqual([false, false]);
    expect(msgs[0].x2).toBeGreaterThan(msgs[0].x1);

    const backward = msgs[2];
    expect(backward.back).toBe(true);
    expect(backward.x2).toBeLessThan(backward.x1);
    expect(container.querySelector('line.hf-seq-msg-back')?.getAttribute('marker-end')).toMatch(
      /^url\(#seq-arr-back-/
    );
  });

  it('widens only the gaps a long label has to span', () => {
    const { container } = render(
      <SequenceCanvas
        entries={[
          entry('A', [
            'participant p0 as pkg.A',
            'participant p1 as pkg.B',
            'participant p2 as pkg.C',
            'participant p3 as pkg.D',
            'p0->>p1: aVeryLongMessageLabelThatNeedsPlentyOfRoom()',
            'p2->>p3: x()',
          ]),
        ]}
      />
    );

    const [a, b, c, d] = columns(container);
    expect(b - a).toBeGreaterThan(200);
    // The untouched pair keeps the base gap; it must not inherit the wide one.
    expect(d - c).toBeLessThan(110);
  });

  it('drops the diagram package prefix and flags foreign lifelines', () => {
    const { container } = render(
      <SequenceCanvas
        entries={[
          entry('Dispatch', [
            'participant p0 as mcp.Dispatch',
            'participant p1 as mcp.handleSearch',
            'participant p2 as retrieval.Service',
            'p0->>p1: handleSearch()',
            'p1->>p2: Search()',
          ]),
        ]}
      />
    );

    const actors = [...container.querySelectorAll('.hf-seq-actor')];
    expect(actors.map((a) => a.textContent)).toEqual(['Dispatch', 'handleSearch', 'retrieval.Service']);
    expect(actors.map((a) => a.classList.contains('hf-seq-actor-ext'))).toEqual([false, false, true]);
    expect(actors[0].getAttribute('title')).toBe('mcp.Dispatch');
  });

  it('elides a message label but keeps the full text reachable', () => {
    const long = 'buildPackageIndexForTheEntireModuleTree()';
    const { container } = render(
      <SequenceCanvas
        entries={[
          entry('A', ['participant p0 as pkg.A', 'participant p1 as pkg.B', `p0->>p1: ${long}`]),
        ]}
      />
    );

    const text = container.querySelector('text.hf-seq-label')!;
    expect(text.lastChild?.textContent).toContain('…');
    expect(text.lastChild?.textContent!.length).toBeLessThan(long.length);
    expect(text.querySelector('title')?.textContent).toBe(long);
  });
});
