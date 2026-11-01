// Presentation helpers both bundles render with: the extension host and the
// webview. Nothing here touches vscode or the HTTP client, so the browser
// bundle can import it without compiling the host's client in.

/** A search hit's relevance in words (#130), from the similarity band. */
export function relevanceOfBand(band: string | undefined): string {
  return band === "NEAR_DUPLICATE" ? "close match" : band === "RELATED" ? "related" : "loosely related";
}

/** A procedure hit's relevance in words (#130), from its score. */
export function relevanceOfScore(score: number | undefined): string {
  const v = score ?? 0;
  return v >= 0.6 ? "close match" : v >= 0.4 ? "related" : "loosely related";
}
