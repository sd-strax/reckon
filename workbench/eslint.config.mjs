// The workbench lint (adr/0249): ESLint's and typescript-eslint's recommended
// rules everywhere, the React hooks rules on the webview. Findings are errors;
// `make workbench-ci` runs it.
import js from "@eslint/js";
import tseslint from "typescript-eslint";
import reactHooks from "eslint-plugin-react-hooks";

export default tseslint.config(
  { ignores: ["out/**", "harness/dist/**", "node_modules/**"] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    // An underscore marks a deliberately unused name, as tsc's noUnusedParameters allows.
    rules: {
      "@typescript-eslint/no-unused-vars": ["error", {
        argsIgnorePattern: "^_", varsIgnorePattern: "^_", caughtErrorsIgnorePattern: "^_", destructuredArrayIgnorePattern: "^_",
      }],
    },
  },
  {
    files: ["webview/src/**/*.{ts,tsx}"],
    ...reactHooks.configs.flat.recommended,
  },
  {
    // Build scripts run under Node.
    files: ["*.mjs"],
    languageOptions: { globals: { process: "readonly", console: "readonly" } },
  },
  { linterOptions: { reportUnusedDisableDirectives: "error" } },
);
