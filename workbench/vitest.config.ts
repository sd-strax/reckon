import { defineConfig } from "vitest/config";

// Component + derivation tests for the webview (impl §5.5), and tests of the
// host modules that do not import vscode (the investigation cache). jsdom
// stands in for the webview; the harness snapshots under harness/snapshots
// feed the component tests.
export default defineConfig({
  esbuild: { jsx: "automatic" },
  test: {
    environment: "jsdom",
    globals: true,
    include: ["webview/src/**/*.test.{ts,tsx}", "src/**/*.test.ts"],
    setupFiles: ["webview/src/test/setup.ts"],
  },
});
