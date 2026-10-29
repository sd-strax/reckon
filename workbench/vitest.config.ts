import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

// Component + derivation tests for the webview (impl §5.5), and tests of the
// host modules. A host module's `vscode` import resolves to the editor fake
// (src/test/vscode.ts, adr/0245), so the host runs without an editor. jsdom
// stands in for the webview; the harness snapshots under harness/snapshots
// feed the component tests. The stylesheet is loaded as text (`?raw`) for the
// layout rules jsdom cannot compute.
export default defineConfig({
  esbuild: { jsx: "automatic" },
  resolve: { alias: { vscode: fileURLToPath(new URL("./src/test/vscode.ts", import.meta.url)) } },
  test: {
    environment: "jsdom",
    globals: true,
    include: ["webview/src/**/*.test.{ts,tsx}", "src/**/*.test.ts"],
    setupFiles: ["webview/src/test/setup.ts"],
    css: { include: [/app\.css/] },
  },
});
