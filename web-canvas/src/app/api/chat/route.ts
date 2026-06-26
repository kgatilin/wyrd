import { anthropic } from "@ai-sdk/anthropic";
import {
  streamText,
  convertToModelMessages,
  type UIMessage,
} from "ai";

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
    system,
  }: {
    messages: UIMessage[];
    system?: string;
  } = await req.json();

  const result = streamText({
    model: anthropic("claude-sonnet-4-6"),
    messages: await convertToModelMessages(messages),
    ...(system === undefined ? {} : { system }),
  });

  return result.toUIMessageStreamResponse();
}
