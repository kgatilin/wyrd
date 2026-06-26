import { anthropic } from "@ai-sdk/anthropic";
import {
  streamText,
  convertToModelMessages,
  type UIMessage,
} from "ai";
import { renderAgentDeclaration } from "@/lib/artifact/capabilities";

export async function POST(req: Request) {
  // Check for API key at runtime, not build time
  const apiKey = process.env.ANTHROPIC_API_KEY;
  if (!apiKey) {
    return new Response(
      JSON.stringify({
        error: "ANTHROPIC_API_KEY environment variable is not set",
      }),
      {
        status: 500,
        headers: { "Content-Type": "application/json" },
      }
    );
  }

  const {
    messages,
    system: clientSystem,
  }: {
    messages: UIMessage[];
    system?: string;
  } = await req.json();

  // The capability declaration is always prepended so the agent knows what an
  // artifact may use; any system text from the client follows it.
  const system = [renderAgentDeclaration(), clientSystem].filter(Boolean).join("\n\n");

  const result = streamText({
    model: anthropic("claude-sonnet-4-6"),
    messages: await convertToModelMessages(messages),
    system,
  });

  return result.toUIMessageStreamResponse();
}
