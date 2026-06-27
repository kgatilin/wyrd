"use client";

import { useAssistantTool } from "@assistant-ui/react";
import { z } from "zod";

import { useArtifactStore } from "./store";
import { renderAgentDeclaration } from "./capabilities";

/**
 * Registers the canvas's frontend tools with the assistant runtime. These are
 * client tools: the agent calls them over AG-UI, but they execute here, against
 * the in-browser artifact store. The capability manifest travels with the
 * write_file description, so the manifest stays the single source of truth for
 * how the agent authors artifacts — the backend hardcodes nothing.
 *
 * Call this once inside the AssistantRuntimeProvider.
 */
export function useArtifactTools(): void {
  const writeArtifact = useArtifactStore((s) => s.writeArtifact);

  useAssistantTool({
    toolName: "write_file",
    description:
      "Create or overwrite an artifact — a single file rendered on the canvas " +
      "— and show it to the user. Returns the artifact id. Pass an existing id " +
      "to overwrite (edit) that artifact; omit it to create a new one.\n\n" +
      renderAgentDeclaration(),
    parameters: z.object({
      name: z.string().describe("Short human-readable artifact title."),
      content: z
        .string()
        .describe(
          "The artifact file source: a JSX module that defines `function Artifact()` returning JSX, per the authoring contract.",
        ),
      id: z
        .string()
        .optional()
        .describe("Existing artifact id to overwrite; omit to create a new one."),
    }),
    execute: async ({ name, content, id }) => {
      const artifactId = writeArtifact({ id, name, content });
      return { ok: true, id: artifactId };
    },
  });
}
