// Bundles the investigation webview (React) and the browser dev harness.
//
//   node esbuild.mjs            one production build → out/webview/
//   node esbuild.mjs --watch    rebuild on change
//   node esbuild.mjs --serve    harness at http://localhost:8765 (rebuilds on change)
//
// Only the webview is bundled; the extension host stays tsc-compiled. Fonts
// are vendored under media/fonts and copied next to the bundle (file loader),
// so the page loads nothing from the network (design/ui/01; impl §5.1).
import * as esbuild from "esbuild";

const watch = process.argv.includes("--watch");
const serve = process.argv.includes("--serve");
const production = !watch && !serve;

/** @type {import("esbuild").BuildOptions} */
const common = {
  bundle: true,
  format: "iife",
  target: ["es2022", "chrome120"],
  jsx: "automatic",
  sourcemap: production ? false : "inline",
  minify: production,
  legalComments: "none",
  loader: { ".woff2": "file" },
  assetNames: "[name]-[hash]",
  define: { "process.env.NODE_ENV": JSON.stringify(production ? "production" : "development") },
  logLevel: "info",
};

const webview = {
  ...common,
  entryPoints: { investigation: "webview/src/main.tsx" },
  outdir: "out/webview",
};

const harness = {
  ...common,
  entryPoints: { harness: "harness/main.tsx" },
  outdir: "harness/dist",
};

if (serve) {
  const ctx = await esbuild.context(harness);
  await ctx.watch();
  const { host, port } = await ctx.serve({ servedir: "harness", port: 8765 });
  console.log(`harness: http://${host === "0.0.0.0" ? "localhost" : host}:${port}/`);
} else if (watch) {
  const ctx = await esbuild.context(webview);
  await ctx.watch();
} else {
  await esbuild.build(webview);
}
