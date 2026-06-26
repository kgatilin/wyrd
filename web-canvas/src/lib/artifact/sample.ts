import { fixtureGraph } from '@/lib/graph/fixture';
import type { Artifact } from './types';

/**
 * A sample artifact that demonstrates the document model: prose explanation,
 * then an embedded graph widget, then a closing note. This is the shape the
 * agent will eventually produce; for now it is hardcoded so the canvas has
 * something representative to render.
 */
export const sampleArtifact: Artifact = {
  id: 'sample-architecture-overview',
  title: 'Architecture overview',
  blocks: [
    {
      type: 'prose',
      markdown: [
        '## Architecture overview',
        '',
        'This package is organised as **ports & adapters**. The domain types sit',
        'at the centre with no outward dependencies; adapters translate between',
        'the domain and the outside world, and the `service` layer orchestrates',
        'the operations.',
        '',
        'The diagram below shows the current component graph — expand a component',
        'to see its members, and click one to focus its relationships.',
      ].join('\n'),
    },
    {
      type: 'graph',
      title: 'Component graph',
      caption: 'internal/… — 5 components across 2 packages',
      graph: fixtureGraph,
      height: 520,
      options: { showDiff: true, cardDensity: 'detailed', showInlineSignatures: true },
    },
    {
      type: 'prose',
      markdown: [
        '### What changed',
        '',
        'Highlighted in green is the newly added `service`, which depends on the',
        'domain model and is wired by `cmd`. The `Component` struct gained a field',
        '(marked `changed`). Nothing in the domain layer takes an outward',
        'dependency — the layering still holds.',
      ].join('\n'),
    },
  ],
};
