// The browser dev harness (impl §5.4): the same <App/> over a HarnessTransport
// fed by recorded snapshots, with a stage picker, a theme toggle and an intent
// log in a side strip. Nothing here ships in the extension.
import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { AppProvider } from "../webview/src/app";
import { App } from "../webview/src/components/App";
import type { InboundMessage } from "../src/protocol";
import { HarnessTransport, type HarnessSnapshot } from "./transport";
import { snapshots } from "./snapshots";
import "../webview/src/styles/tokens.css";
import "../webview/src/styles/app.css";
import "./harness.css";

function Harness() {
  const [stage, setStage] = useState(() => new URLSearchParams(location.search).get("stage") || snapshots[0]?.name || "draft");
  const [theme, setTheme] = useState<"dark" | "light">(() => (new URLSearchParams(location.search).get("theme") === "light" ? "light" : "dark"));
  const [intents, setIntents] = useState<InboundMessage[]>([]);
  const data: HarnessSnapshot | null = useMemo(() => snapshots.find((s) => s.name === stage) ?? null, [stage]);
  const transport = useMemo(() => new HarnessTransport(data, (m) => setIntents((l) => [m, ...l].slice(0, 40))), []);
  useEffect(() => { transport.load(data); }, [transport, data]);
  useEffect(() => {
    document.body.classList.toggle("vscode-dark", theme === "dark");
    document.body.classList.toggle("vscode-light", theme === "light");
  }, [theme]);
  return (
    <>
      <div id="harnessStrip">
        <label>stage
          <select value={stage} onChange={(e) => setStage(e.target.value)}>
            <option value="draft">draft (unseeded)</option>
            {snapshots.map((s) => <option key={s.name} value={s.name}>{s.name}</option>)}
          </select>
        </label>
        <button onClick={() => setTheme((t) => (t === "dark" ? "light" : "dark"))}>{theme === "dark" ? "light theme" : "dark theme"}</button>
        <details className="intents">
          <summary>intents · {intents.length}</summary>
          <pre>{intents.map((i) => JSON.stringify(i)).join("\n")}</pre>
        </details>
      </div>
      <div id="harnessPage">
        <AppProvider transport={transport}><App /></AppProvider>
      </div>
    </>
  );
}

createRoot(document.getElementById("root")!).render(<Harness />);
