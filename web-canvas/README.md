# Archai Canvas

A standalone chat+canvas web app for the archai project. Features a two-pane resizable layout with an AI chat interface and a canvas for rendering architecture diagrams.

## Tech Stack

- Next.js 16 (App Router, TypeScript, Tailwind CSS v4)
- assistant-ui for the chat interface
- Vercel AI SDK with Anthropic Claude
- react-resizable-panels for the split-pane layout

## Getting Started

1. Install dependencies:
   ```bash
   npm install
   ```

2. Set up environment variables:
   ```bash
   export ANTHROPIC_API_KEY=your_api_key_here
   ```

3. Run the development server:
   ```bash
   npm run dev
   ```

4. Open [http://localhost:3000](http://localhost:3000)

## Project Structure

```
web-canvas/
├── src/
│   ├── app/
│   │   ├── api/chat/route.ts    # Chat API endpoint (Anthropic Claude)
│   │   ├── layout.tsx           # Root layout with TooltipProvider
│   │   ├── page.tsx             # Main page
│   │   └── globals.css          # Global styles (Tailwind v4)
│   ├── components/
│   │   ├── assistant-ui/        # assistant-ui styled components
│   │   ├── layout/              # Layout components
│   │   │   ├── chat-canvas-layout.tsx  # Main two-pane layout
│   │   │   ├── canvas-panel.tsx        # Canvas placeholder
│   │   │   └── artifacts-sidebar.tsx   # Artifacts menu
│   │   └── ui/                  # shadcn/ui components
│   └── lib/
│       └── utils.ts             # Utility functions
└── ...
```

## Features

- **Two-pane resizable layout**: Chat on the left, canvas on the right
- **Collapsible chat panel**: Toggle the chat pane visibility
- **Artifacts sidebar**: Preview of future artifact management (static stubs)
- **Real LLM backend**: Connected to Claude claude-sonnet-4-6 via Vercel AI SDK

## Development

```bash
npm run dev      # Start development server
npm run build    # Production build
npm run lint     # Run ESLint
```
