// The webview bootstrap: read the surface the host named on the root, pick
// the transport, mount that surface's app (one bundle, several roots —
// adr/0248). All content is rendered from posted messages; the CSP forbids
// any network from the page.
import { createRoot } from "react-dom/client";
import type { Surface } from "../../src/protocol";
import { AppProvider } from "./app";
import { App } from "./components/App";
import { CaseApp } from "./surfaces/CaseApp";
import { ImportApp } from "./surfaces/ImportApp";
import { KnowledgeApp } from "./surfaces/KnowledgeApp";
import { VsCodeTransport } from "./transport";
import "./styles/tokens.css";
import "./styles/app.css";
import "./styles/panels.css";

const root = document.getElementById("root");
if (root) {
  const surface = (root.dataset.surface ?? "investigation") as Surface;
  const mount = createRoot(root);
  switch (surface) {
    case "case":
      mount.render(<CaseApp transport={new VsCodeTransport()} />);
      break;
    case "import":
      mount.render(<ImportApp transport={new VsCodeTransport()} />);
      break;
    case "knowledge":
      mount.render(<KnowledgeApp transport={new VsCodeTransport()} />);
      break;
    default:
      mount.render(
        <AppProvider transport={new VsCodeTransport()}>
          <App />
        </AppProvider>,
      );
  }
}
