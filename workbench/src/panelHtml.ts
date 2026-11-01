// The one page every reckon panel loads, and the one set of panel options
// (adr/0248). Every surface is a root of the same bundle, chosen by the
// `data-surface` attribute; the policy is nonce-only scripts, bundled styles
// and fonts from the extension's own files, nothing inline and no network.
import * as vscode from "vscode";
import type { Surface } from "./protocol";
import { escapeHtml, makeNonce } from "./util";

/** The webview options shared by every reckon panel. */
export function panelOptions(extensionUri: vscode.Uri): vscode.WebviewPanelOptions & vscode.WebviewOptions {
  return {
    enableScripts: true,
    retainContextWhenHidden: true,
    localResourceRoots: [
      vscode.Uri.joinPath(extensionUri, "out", "webview"),
      vscode.Uri.joinPath(extensionUri, "media"),
    ],
  };
}

/**
 * The page for one surface: the bundle's stylesheet and script under the one
 * CSP. The title may come from outside (a case number), so it is escaped.
 */
export function panelHtml(webview: vscode.Webview, extensionUri: vscode.Uri, surface: Surface, title: string): string {
  const nonce = makeNonce();
  const base = vscode.Uri.joinPath(extensionUri, "out", "webview");
  const script = webview.asWebviewUri(vscode.Uri.joinPath(base, "investigation.js"));
  const style = webview.asWebviewUri(vscode.Uri.joinPath(base, "investigation.css"));
  const csp = [
    `default-src 'none'`,
    `style-src ${webview.cspSource}`,
    `font-src ${webview.cspSource}`,
    `script-src 'nonce-${nonce}'`,
  ].join("; ");
  return /* html */ `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy" content="${csp}" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <link rel="stylesheet" href="${style.toString()}" />
  <title>${escapeHtml(title)}</title>
</head>
<body class="loading">
  <div id="root" data-surface="${surface}"></div>
  <script nonce="${nonce}" src="${script.toString()}"></script>
</body>
</html>`;
}
