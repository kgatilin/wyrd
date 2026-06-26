"use client";

import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

/**
 * The internal artifact store — a virtual filesystem of artifacts. An artifact
 * is a single agent-authored file (JSX/HTML executed at runtime). This is the
 * store the agent's file tools target: write_file → {@link ArtifactStore.writeArtifact},
 * read_file → lookup, etc.
 *
 * Two kinds: `generated` (ephemeral, in-memory) and `saved` (durable dashboards,
 * persisted to localStorage). Only saved artifacts survive a reload.
 */
export interface Artifact {
  id: string;
  name: string;
  /** The artifact file's source (agent-authored). */
  content: string;
  kind: 'generated' | 'saved';
  createdAt: number;
}

interface ArtifactStore {
  artifacts: Artifact[];
  activeId: string | null;

  /** Create or overwrite an artifact by id (defaults to generated) and activate it. */
  writeArtifact: (input: { id?: string; name: string; content: string }) => string;
  /** Read an artifact's content (the agent's read_file). */
  readArtifact: (id: string) => string | null;
  /** Promote a generated artifact to a saved dashboard (persisted). */
  saveArtifact: (id: string) => void;
  deleteArtifact: (id: string) => void;
  setActive: (id: string) => void;
}

function freshId(): string {
  return `art-${Math.random().toString(36).slice(2, 9)}`;
}

export const useArtifactStore = create<ArtifactStore>()(
  persist(
    (set, get) => ({
      artifacts: [],
      activeId: null,

      writeArtifact: ({ id, name, content }) => {
        const targetId = id ?? freshId();
        set((s) => {
          const existing = s.artifacts.find((a) => a.id === targetId);
          if (existing) {
            return {
              artifacts: s.artifacts.map((a) =>
                a.id === targetId ? { ...a, name, content } : a,
              ),
              activeId: targetId,
            };
          }
          const artifact: Artifact = {
            id: targetId,
            name,
            content,
            kind: 'generated',
            createdAt: Date.now(),
          };
          return { artifacts: [...s.artifacts, artifact], activeId: targetId };
        });
        return targetId;
      },

      readArtifact: (id) => get().artifacts.find((a) => a.id === id)?.content ?? null,

      saveArtifact: (id) =>
        set((s) => ({
          artifacts: s.artifacts.map((a) =>
            a.id === id ? { ...a, kind: 'saved' as const } : a,
          ),
        })),

      deleteArtifact: (id) =>
        set((s) => {
          const artifacts = s.artifacts.filter((a) => a.id !== id);
          const activeId =
            s.activeId === id ? (artifacts[0]?.id ?? null) : s.activeId;
          return { artifacts, activeId };
        }),

      setActive: (id) => set({ activeId: id }),
    }),
    {
      name: 'archai-canvas-artifacts',
      storage: createJSONStorage(() => localStorage),
      // Only durable (saved) artifacts persist; generated ones are ephemeral.
      partialize: (s) => ({
        artifacts: s.artifacts.filter((a) => a.kind === 'saved'),
      }),
    },
  ),
);
