import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    strictPort: false,
  },
  test: {
    environment: 'jsdom',
    // Only run unit files — never Playwright's e2e/*.spec.ts (which import
    // @playwright/test and would crash under Vitest). The existing
    // src/layout/layout.test.ts keeps matching the {test} branch.
    include: ['src/**/*.{test,harness.test}.{ts,tsx}'],
  },
});
