"use client";

import { useAssistantTool } from "@assistant-ui/react";
import { z } from "zod";

import { useArtifactStore } from "./store";
import { renderAgentDeclaration } from "./capabilities";
import { compileArtifact } from "./compile";

/**
 * Registers the canvas's frontend tools with the assistant runtime. These are
 * client tools: the agent calls them over AG-UI, but they execute here, against
 * the in-browser artifact store. They are deliberately named *_artifact to stay
 * distinct from the backend's source-file tools (archai_read_file etc.) — those
 * read the project's source; these manage the canvas artifacts.
 *
 * The capability manifest travels with write_artifact's description, so the
 * manifest stays the single source of truth for how the agent authors artifacts.
 *
 * Call this once inside the AssistantRuntimeProvider.
 */
export function useArtifactTools(): void {
  const writeArtifact = useArtifactStore((s) => s.writeArtifact);

  // Compile-then-save: an invalid artifact returns the transpile/eval error to
  // the agent instead of being saved, so a broken artifact never reaches the
  // canvas and the agent gets actionable feedback to retry.
  async function saveCompiled(
    id: string | undefined,
    name: string,
    content: string,
  ): Promise<{ ok: true; id: string } | { ok: false; error: string; hint: string }> {
    const compiled = await compileArtifact(content);
    if (!compiled.ok) {
      return {
        ok: false,
        error: compiled.error,
        hint: "The artifact was NOT saved. Fix the error and call the tool again with corrected content.",
      };
    }
    return { ok: true, id: writeArtifact({ id, name, content }) };
  }

  useAssistantTool({
    toolName: "write_artifact",
    description:
      "Create or overwrite an artifact — a single file rendered on the canvas " +
      "— and show it to the user. Returns the artifact id.\n" +
      "- To create a NEW, separate artifact: OMIT id. A fresh id is generated and " +
      "returned; the previous artifacts are kept.\n" +
      "- To replace an existing artifact wholesale: pass its exact id (from the " +
      "earlier result or list_artifacts). NEVER reuse an id for unrelated content " +
      "— that destroys the previous artifact.\n" +
      "For a small change to an existing artifact, prefer edit_artifact.\n\n" +
      renderAgentDeclaration(),
    parameters: z.object({
      name: z.string().describe("Short human-readable artifact title."),
      content: z
        .string()
        .describe(
          "The artifact file source: a script that defines `function Artifact()` returning JSX, per the authoring contract.",
        ),
      id: z
        .string()
        .optional()
        .describe("Existing artifact id to overwrite; omit to create a new one."),
    }),
    execute: async ({ name, content, id }) => saveCompiled(id, name, content),
  });

  useAssistantTool({
    toolName: "edit_artifact",
    description:
      "Edit an existing artifact by replacing an exact substring — use this for " +
      "small fixes instead of resending the whole file. old_string must match " +
      "the current content exactly and uniquely (unless replace_all). The result " +
      "is recompiled; on a syntax/eval error nothing is changed and the error is " +
      "returned so you can retry.",
    parameters: z.object({
      id: z.string().describe("Id of the artifact to edit."),
      old_string: z
        .string()
        .describe("Exact substring to replace (must appear in the current content)."),
      new_string: z.string().describe("Replacement text."),
      replace_all: z
        .boolean()
        .optional()
        .describe("Replace every occurrence instead of requiring a unique match."),
    }),
    execute: async ({ id, old_string, new_string, replace_all }) => {
      const artifact = useArtifactStore.getState().artifacts.find((a) => a.id === id);
      if (!artifact) {
        return { ok: false, error: `No artifact with id "${id}". Use list_artifacts to see ids.` };
      }
      const occurrences = artifact.content.split(old_string).length - 1;
      if (occurrences === 0) {
        return { ok: false, error: "old_string not found in the artifact content." };
      }
      if (occurrences > 1 && !replace_all) {
        return {
          ok: false,
          error: `old_string matches ${occurrences} places; pass replace_all or use a longer, unique old_string.`,
        };
      }
      const next = replace_all
        ? artifact.content.split(old_string).join(new_string)
        : artifact.content.replace(old_string, new_string);
      return saveCompiled(id, artifact.name, next);
    },
  });

  useAssistantTool({
    toolName: "read_artifact",
    description:
      "Read an artifact's current source by id — use it before edit_artifact when " +
      "you don't already have the exact current content.",
    parameters: z.object({
      id: z.string().describe("Id of the artifact to read."),
    }),
    execute: async ({ id }) => {
      const artifact = useArtifactStore.getState().artifacts.find((a) => a.id === id);
      if (!artifact) {
        return { ok: false, error: `No artifact with id "${id}".` };
      }
      return { ok: true, id, name: artifact.name, content: artifact.content };
    },
  });

  useAssistantTool({
    toolName: "list_artifacts",
    description: "List the artifacts currently on the canvas (id, name, kind).",
    parameters: z.object({}),
    execute: async () => ({
      ok: true,
      artifacts: useArtifactStore
        .getState()
        .artifacts.map((a) => ({ id: a.id, name: a.name, kind: a.kind })),
    }),
  });
}
