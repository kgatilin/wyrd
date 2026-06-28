"use client";

import { useEffect } from 'react';
import { useArtifactStore } from './store';
import { welcomeDashboardFile } from './welcome-dashboard';
import { pluginArchitectureFile } from './plugin-architecture';

/**
 * A system-seeded SAVED artifact: an ordinary artifact file (the same unit the
 * agent authors via write_artifact) that ships with the canvas. Each has a
 * version so returning users — whose copy is persisted in localStorage — pick up
 * a new revision when {@link version} is bumped, while their own dashboards are
 * left untouched.
 */
interface SystemDoc {
  id: string;
  name: string;
  file: string;
  version: number;
}

const LANDING_ID = 'welcome-dashboard';

const SYSTEM_DOCS: SystemDoc[] = [
  { id: LANDING_ID, name: 'Welcome', file: welcomeDashboardFile, version: 3 },
  { id: 'plugin-architecture', name: 'Plugin architecture', file: pluginArchitectureFile, version: 1 },
];

const versionKey = (id: string) => `archai-canvas-seed-version:${id}`;
// The welcome doc used this standalone key before system docs were generalized.
const LEGACY_WELCOME_KEY = 'archai-canvas-welcome-version';

/**
 * Ensures the canvas always opens on something useful, and keeps the bundled
 * system docs current. Per doc, the version key gives a tri-state:
 *   - key absent            → never seeded → introduce it (write + save)
 *   - key present & stale    → refresh in place to the new revision
 *   - key present, artifact gone → the user deleted it → leave it gone
 *
 * Saved artifacts persist to localStorage, so a returning user keeps their own
 * dashboards and this never overwrites them.
 */
export function useSeedArtifacts(): void {
  useEffect(() => {
    const store = useArtifactStore.getState();
    const prevActive = store.activeId;
    const introduced: string[] = [];

    for (const doc of SYSTEM_DOCS) {
      const seededVersion = readDocVersion(doc);
      const exists = store.artifacts.some((a) => a.id === doc.id);
      const introduce = seededVersion === null;
      const refresh = seededVersion !== null && exists && seededVersion < doc.version;
      if (introduce || refresh) {
        store.writeArtifact({ id: doc.id, name: doc.name, content: doc.file });
        store.saveArtifact(doc.id);
        writeDocVersion(doc);
      }
      if (introduce) introduced.push(doc.id);
    }

    // writeArtifact activates whatever it last wrote. Land on the welcome doc
    // when it was freshly introduced, on any newly-introduced doc otherwise
    // (so a new system doc surfaces itself once), and never steal an existing
    // user's focus on a plain refresh.
    if (introduced.length) {
      store.setActive(introduced.includes(LANDING_ID) ? LANDING_ID : introduced[introduced.length - 1]);
    } else if (prevActive) {
      if (store.activeId !== prevActive) store.setActive(prevActive);
    } else if (store.activeId === null) {
      const first = store.artifacts.find((a) => a.kind === 'saved') ?? store.artifacts[0];
      if (first) store.setActive(first.id);
    }
  }, []);
}

function readDocVersion(doc: SystemDoc): number | null {
  if (typeof window === 'undefined') return doc.version;
  const raw = window.localStorage.getItem(versionKey(doc.id));
  if (raw !== null) return Number(raw) || 0;
  // Migrate the welcome doc's legacy key so we neither re-seed a current copy
  // nor resurrect one the user deleted before the keys were generalized.
  if (doc.id === LANDING_ID) {
    const legacy = window.localStorage.getItem(LEGACY_WELCOME_KEY);
    if (legacy !== null) return Number(legacy) || 0;
  }
  return null;
}

function writeDocVersion(doc: SystemDoc): void {
  if (typeof window === 'undefined') return;
  window.localStorage.setItem(versionKey(doc.id), String(doc.version));
}
