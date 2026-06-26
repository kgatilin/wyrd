"use client";

import { useEffect } from 'react';
import { useArtifactStore } from './store';
import { exampleArtifactFile } from './example-file';

const EXAMPLE_ID = 'example-architecture-overview';

/**
 * Seeds the store with the example artifact as a `generated` file on first
 * mount. Stands in for the agent's first write_file; idempotent by id.
 */
export function useSeedArtifacts(): void {
  useEffect(() => {
    const store = useArtifactStore.getState();
    const exists = store.artifacts.some((a) => a.id === EXAMPLE_ID);
    if (!exists) {
      store.writeArtifact({
        id: EXAMPLE_ID,
        name: 'Architecture overview',
        content: exampleArtifactFile,
      });
    } else if (store.activeId === null) {
      store.setActive(EXAMPLE_ID);
    }
  }, []);
}
