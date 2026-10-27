// The webview bootstrap: pick the transport, mount the app the host's shell
// names (data-surface: the investigation document or the Knowledge document,
// impl §2.6). All content is rendered from posted messages; the CSP forbids
// any network from the page.
import { createRoot } from "react-dom/client";
import { AppProvider } from "./app";
import { App } from "./components/App";
import { KnowledgeApp } from "./components/knowledge/KnowledgeApp";
import { VsCodeTransport } from "./transport";
import "./styles/tokens.css";
import "./styles/app.css";

const root = document.getElementById("root");
if (root) {
  const knowledge = document.body.dataset.surface === "knowledge";
  createRoot(root).render(
    <AppProvider transport={new VsCodeTransport()} surface={knowledge ? "knowledge" : "investigation"}>
      {knowledge ? <KnowledgeApp /> : <App />}
    </AppProvider>,
  );
}
