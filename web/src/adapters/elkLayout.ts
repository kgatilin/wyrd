import { layout } from '../layout/layout';
import type { LayoutPort } from '../domain/ports';

/** LayoutPort backed by elkjs. Copies the readonly interaction sets into the
 *  mutable Sets the layout() function expects (the adapter is the boundary). */
export function createElkLayout(): LayoutPort {
  return {
    compute(graph, interaction) {
      return layout(graph, {
        expanded: new Set(interaction.expanded),
        internalExpanded: new Set(interaction.internalExpanded),
        internalWide: new Set(interaction.internalWide),
      });
    },
  };
}
