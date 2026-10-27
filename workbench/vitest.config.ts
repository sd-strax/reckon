import { defineConfig } from "vitest/config";

// Component + derivation tests for the webview (impl §5.5). jsdom stands in
// for the webview; the harness snapshots under harness/snapshots feed them.
export default defineConfig({
  esbuild: { jsx: "automatic" },
  test: {
    environment: "jsdom",
    globals: true,
    include: ["webview/src/**/*.test.{ts,tsx}"],
    setupFiles: ["webview/src/test/setup.ts"],
  },
});
