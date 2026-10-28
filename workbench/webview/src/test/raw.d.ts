// Vitest (Vite) imports a file's text with the `?raw` suffix; tests use it to
// check stylesheet rules that jsdom cannot lay out.
declare module "*?raw" {
  const text: string;
  export default text;
}
