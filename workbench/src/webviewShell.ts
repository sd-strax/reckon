// The HTML shell every reckon document webview loads (impl §5.1): one bundle
// under the nonce CSP, the stylesheet with the vendored fonts beside it, no
// network from the page. The investigation and the Knowledge documents share
// it; `surface` tells the bundle which app to mount.
import * as vscode from "vscode";

export function webviewShell(webview: vscode.Webview, extensionUri: vscode.Uri, opts: { title: string; surface: "investigation" | "knowledge" }): string {
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
  <title>${opts.title}</title>
</head>
<body class="loading" data-surface="${opts.surface}">
  <div id="root"></div>
  <script nonce="${nonce}" src="${script.toString()}"></script>
</body>
</html>`;
}

/** The webview panel options both documents use: scripts on, state kept
 * while hidden, resources only from the bundle and media directories. */
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

function makeNonce(): string {
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let out = "";
  for (let i = 0; i < 32; i++) {
    out += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return out;
}
