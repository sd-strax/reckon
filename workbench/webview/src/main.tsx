// The webview bootstrap: pick the transport, mount the app. All content is
// rendered from posted messages; the CSP forbids any network from the page.
import { createRoot } from "react-dom/client";
import { AppProvider } from "./app";
import { App } from "./components/App";
import { VsCodeTransport } from "./transport";
import "./styles/tokens.css";
import "./styles/app.css";

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <AppProvider transport={new VsCodeTransport()}>
      <App />
    </AppProvider>,
  );
}
