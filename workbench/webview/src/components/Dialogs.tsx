// The verdict dialog (02 §2.10: preflight + residual rendered BEFORE the
// attempt) and the enablement form (11 §5.1: schema-derived, extension-rendered;
// secrets never get a field here — the host's secure input captures a
// REFERENCE after the native confirm).
import { useState } from "react";
import { useApp } from "../app";
import { verdictChecklist, verdictRefs, verdictResidual } from "../derive/verdict";

export function VerdictDialog() {
  const { state, send, setVerdictOpen } = useApp();
  const [disposition, setDisposition] = useState("");
  const [rationale, setRationale] = useState("");
  const pins = state.snap?.pins || [];
  const verdict = state.snap?.investigation.verdict || null;
  const checklist = verdictChecklist(pins, verdict);
  const residual = verdictResidual(state.snap?.capabilities ?? null, state.snap?.hypotheses || [], state.snap?.turns || []);
  const pinsOk = pins.some((p) => !p.superseded);
  const close = () => setVerdictOpen(false);
  return (
    <div className="dialog" id="verdictDialog" onKeyDown={(e) => { if (e.key === "Escape") close(); }}>
      <div className="dlg" role="dialog" aria-label="Record verdict">
        <h3>Record verdict</h3>
        <div className="checklist">
          {checklist.map((c, i) => <div key={i} className={"item " + (c.ok ? "ok" : "unmet")}>{(c.ok ? "☑ " : "☐ ") + c.text}</div>)}
        </div>
        <div className="dlglabel">Disposition</div>
        <div className="dispositions">
          {["BENIGN", "SUSPICIOUS", "MALICIOUS"].map((d) => (
            <label key={d}><input type="radio" name="disp" value={d} checked={disposition === d} onChange={() => setDisposition(d)} /> {d}</label>
          ))}
        </div>
        <div className="dlglabel">Rationale (required — this is the record)</div>
        <textarea id="verdictRationale" rows={3} value={rationale} onChange={(e) => setRationale(e.target.value)} />
        <div className="residual">
          <div className="rtitle">What this verdict does not cover</div>
          {residual.map((l, i) => <div key={i}>· {l}</div>)}
        </div>
        <div className="decide">
          <button className="primary" disabled={!(disposition && rationale.trim() && pinsOk)} onClick={() => {
            send({ type: "verdict.submit", disposition, rationale: rationale.trim(), refs: verdictRefs(pins) });
            close();
          }}>Record verdict</button>
          <button onClick={close}>Cancel</button>
        </div>
      </div>
    </div>
  );
}

export function EnableForm({ adapter }: { adapter: string }) {
  const { state, send, closeEnableForm } = useApp();
  const enablement = state.snap?.enablement;
  const a = (enablement?.adapters || []).find((x) => x.name === adapter);
  const [values, setValues] = useState<Record<string, string>>(() => {
    const out: Record<string, string> = {};
    if (a) for (const name in a.configFields || {}) if (!a.configFields[name].secret) out[name] = name === "scenario" ? (a.scenario || "") : "";
    return out;
  });
  if (!a || !a.supportable) return null; // no affordance for classes v0 cannot spawn
  const secretFields = Object.keys(a.configFields || {}).filter((n) => a.configFields[n].secret);
  return (
    <div className="dialog" id="enableDialog" onKeyDown={(e) => { if (e.key === "Escape") closeEnableForm(); }}>
      <div className="dlg" role="dialog" aria-label={(a.enabled ? "Configure " : "Enable ") + a.name}>
        <h3>{(a.enabled ? "Configure " : "Enable ") + a.name}</h3>
        <div className="pinhelp">Class {a.class}. Applying edits the tenant config file (the source of truth) and is recorded with your identity. You confirm in a native dialog next.</div>
        <div id="enableFields">
          {Object.keys(a.configFields || {}).map((name) => {
            const f = a.configFields[name];
            if (f.secret) {
              return <div key={name} className="field"><div className="secretnote">{name}: secret — captured via secure input when you apply (a keychain:// / env:// / vault:// reference, never the value, never in this conversation)</div></div>;
            }
            return (
              <div key={name} className="field">
                <label>{name}{(a.requiredFields || []).includes(name) ? " (required)" : ""}</label>
                <input type="text" value={values[name] ?? ""} onChange={(e) => setValues((v) => ({ ...v, [name]: e.target.value }))} />
                {f.description ? <div className="fdesc">{f.description}</div> : null}
              </div>
            );
          })}
        </div>
        <div className="decide">
          <button className="primary" onClick={() => {
            const config: Record<string, string> = {};
            for (const name in values) { const v = values[name].trim(); if (v !== "") config[name] = v; }
            send({ type: "enablement.apply", adapter: a.name, enabled: true, config, secretFields });
            closeEnableForm();
          }}>{a.enabled ? "Apply changes" : "Enable " + a.name}</button>
          <button onClick={closeEnableForm}>Cancel</button>
        </div>
      </div>
    </div>
  );
}
