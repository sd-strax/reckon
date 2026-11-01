// The case document (adr/0030): one system-of-record case, read-only, with
// the one act the analyst can take on it here: start an investigation rooted
// on it, or open the one already rooted on it. It does not link out; the
// document IS the read, and the case number is enough for anyone who needs
// the SoR's own page. Nothing here writes to the case system.
import { useEffect, useState } from "react";
import type { CaseInboundMessage, CaseRenderMessage } from "../../../src/protocol";
import type { CaseRow } from "../../../src/viewTypes";
import type { WebviewTransport } from "../transport";

export type CaseTransport = WebviewTransport<CaseInboundMessage, CaseRenderMessage>;

interface CaseState {
  current: CaseRow | null;
  starting: boolean;
  error: string | null;
}

export function CaseApp({ transport }: { transport: CaseTransport }) {
  const [state, setState] = useState<CaseState>({ current: null, starting: false, error: null });
  useEffect(() => {
    const off = transport.onMessage((m) => {
      switch (m.type) {
        case "case":
          setState({ current: m.case, starting: false, error: null });
          return;
        case "starting":
          setState((s) => ({ ...s, starting: true, error: null }));
          return;
        case "error":
          setState((s) => ({ ...s, starting: false, error: m.message }));
          return;
      }
    });
    transport.send({ type: "ready" });
    return off;
  }, [transport]);

  const c = state.current;
  if (!c) {
    return (
      <div className="panel case">
        {state.error === null
          ? <p className="hint">Reading the case…</p>
          : <>
              <p className="err">{state.error}</p>
              <p><button onClick={() => transport.send({ type: "refresh" })}>Retry</button></p>
            </>}
      </div>
    );
  }
  const inv = c.investigation;
  const open = () => inv && transport.send({ type: "open", id: inv.id, title: inv.title });
  return (
    <div className="panel case">
      <div className="num">{c.number}</div>
      <h1>{c.title || c.number}</h1>
      <div className="meta">
        {c.priority ? <span><b>{c.priority}</b></span> : null}
        {c.status ? <span>{c.status}</span> : null}
        {c.assignmentGroup ? <span>{c.assignmentGroup}</span> : null}
        {c.openedAt ? <span>opened {new Date(c.openedAt).toLocaleString()}</span> : null}
        {c.source ? <span>source {c.source}</span> : null}
      </div>
      {inv ? (
        <div className="flag">Under investigation: <a onClick={open}>{inv.title}</a> <span className="hint">({inv.status})</span></div>
      ) : null}
      <pre>{c.summary || "(no description)"}</pre>
      <div className="actions">
        {inv ? (
          <>
            <button className="primary" onClick={open}>Open investigation</button>
            <span className="hint">A second investigation on this case can still be started from New Investigation → From a case…</span>
          </>
        ) : (
          <>
            <button className="primary" disabled={state.starting} onClick={() => { if (!state.starting) transport.send({ type: "start" }); }}>
              {state.starting ? "Starting…" : "Start investigation"}
            </button>
            <span className="hint">Roots a new investigation on this case; the case body is treated as a claim to verify.</span>
          </>
        )}
      </div>
      {state.error ? <div className="err">{state.error}</div> : null}
    </div>
  );
}
