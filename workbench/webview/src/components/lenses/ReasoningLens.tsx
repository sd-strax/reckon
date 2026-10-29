// Reasoning — the competing set (08 §8.4): the verdict basis strip, the
// tracker with one-line prediction rows, the Cards / Matrix toggle, the new-
// hypothesis affordance, and the pinned-evidence fold (the canonical place
// for pins).
import { useState } from "react";
import { useApp } from "../../app";
import { badgeTone, blockersOf, counted, hypName, hypWords, isLive, orderHypotheses, outstanding, predWords, tested } from "../../derive/hyps";
import { buildMatrix } from "../../derive/matrix";
import { HypothesisCard } from "../Hypotheses";
import { Badge, Empty, RefChip, fmtDateTime } from "../shared";
import { pinTargets, typeOptions } from "../../derive/pinAction";
import type { PinRow } from "../../../../src/viewTypes";

/** The basis a verdict would rest on (#138), stated as it is: which hypotheses
 * are decided, which are not, and what is still to test — in words, one item
 * per hypothesis, with no "basis" claimed while nothing is decided. */
function VerdictBasis() {
  const { state, openInspector } = useApp();
  const hs = state.snap?.hypotheses || [];
  const live = hs.filter((h) => h.status !== "ABANDONED");
  const open = live.flatMap((h) => h.predictions.filter((p) => outstanding(p, h)).map((p) => ({ h, p })));
  const blocked = live.some((h) => blockersOf(h, hs).length > 0);
  if (!hs.length) return null;
  const decided = live.filter((h) => h.status === "SUPPORTED" || h.status === "REFUTED");
  return (
    <div className="basis">
      <span className="kicker">{decided.length ? "Verdict basis" : "No verdict basis yet — no hypothesis is decided"}</span>
      <ul className="basislist">
        {live.map((h) => (
          <li key={h.id} className="basisitem">
            hypothesis <a className="hyphandle" title={hypName(h, hs)} onClick={() => openInspector({ kind: "ref", ref: h.id })}>{hypWords(h, hs)}</a>{" "}
            <Badge tone={badgeTone(h.status)}>{h.status.toLowerCase()}</Badge>
          </li>
        ))}
      </ul>
      <span className="imeta">
        {open.length ? <>Still to test: {open.map(({ p }) => predWords(p)).join(", ")}</> : "Nothing left to test"}
        {" · "}{blocked ? "a test that tells two hypotheses apart is still untested" : "every test that tells hypotheses apart is done"}
      </span>
    </div>
  );
}

function MatrixView() {
  const { state, openInspector } = useApp();
  const hs = state.snap?.hypotheses || [];
  const m = buildMatrix(hs);
  if (!m.rows.length) return <Empty>No predictions to score yet.</Empty>;
  return (
    <table className="matrix">
      <thead><tr><th>Prediction</th>{m.columns.map((h) => <th key={h.id}><a onClick={() => openInspector({ kind: "ref", ref: h.id })}>{hypName(h, hs)}</a></th>)}</tr></thead>
      <tbody>
        {m.rows.map((r) => (
          <tr key={r.p.id} className={r.open ? "open" : undefined}>
            <td><a onClick={() => openInspector({ kind: "ref", ref: r.p.id })}>P{r.p.ordinal ?? "?"}</a> <span className="mtitle">{r.p.statement}</span>{r.open ? <span className="imeta"> open</span> : null}</td>
            {r.cells.map((c, i) => <td key={i} className={"cell " + (c === "✓" ? "ok" : c === "✕" ? "bad" : c === "?" ? "open" : "")}>{c}</td>)}
          </tr>
        ))}
      </tbody>
      <tfoot><tr><td>Inconsistent</td>{m.inconsistent.map((n, i) => <td key={i} className="cell">{n}</td>)}</tr></tfoot>
    </table>
  );
}

function PinnedEvidence() {
  const { state, send, openPrompt } = useApp();
  const pins = state.snap?.pins || [];
  const engine = String(state.snap?.investigation.state || "").toUpperCase();
  const closed = engine === "CONCLUDED" || engine === "ARCHIVED" || engine === "DISCARDED";
  // An action from a pin, without the agent (#146): type, target, reason —
  // then the type's own fields when it declares any.
  const requestFromPin = (pin: PinRow) => {
    const targets = pinTargets(pin, state.snap?.derived?.graph, state.labels);
    const types = typeOptions(state.actionTypes);
    openPrompt({
      title: "Request an action", require: false, label: "Why is it needed? (recorded with the request)", placeholder: "e.g. the logon came from an unapproved host — contain the account",
      helper: "The request is yours, cites this pin's evidence, and goes through approval like any action.",
      extrasTop: [
        { kind: "select", id: "type", label: "Action", options: types, value: types[0]?.value },
        targets.length ? { kind: "select", id: "target", label: "On", options: [...targets, { value: "", label: "another target…" }], value: targets[0].value } : { kind: "text", id: "other", label: "On (host, account, address or indicator)", value: "" },
        ...(targets.length ? [{ kind: "text" as const, id: "other", label: "…or another target", value: "" }] : []),
      ],
      confirm: "Next",
      onConfirm: (rationale, _picked, ex) => {
        const actionType = String(ex.type || "");
        const target = String(ex.other || "").trim() || String(ex.target || "").trim();
        if (!actionType || !target) return;
        const t = state.actionTypes.find((x) => x.actionType === actionType);
        const send1 = (parameters: Record<string, unknown>) => send({ type: "action.request", actionType, target, parameters, rationale: rationale.trim() || "requested from a pinned finding", evidenceRefs: pin.inputRefs || [] });
        const fields = (t?.inputs || []).filter((i) => i.name !== "thread_ref" && i.name !== "follow_up_hours");
        if (!fields.length) { send1({}); return; }
        // The confirm closes this prompt after the handler returns: open the
        // second step on the next tick so it is not closed with it.
        setTimeout(() => openPrompt({
          title: (t?.name || actionType) + " — " + target, label: "Details",
          helper: "The fields this action carries. Required ones must be filled.",
          value: "", require: false,
          extrasTop: fields.map((i) => ({ kind: "text" as const, id: i.name, label: i.name.replace(/_/g, " ") + (i.required ? "" : " (optional)"), value: "" })),
          confirm: "Request",
          onConfirm: (_v, _p, ex2) => {
            const parameters: Record<string, unknown> = {};
            for (const i of fields) { const v = String(ex2[i.name] ?? "").trim(); if (v) parameters[i.name] = v; }
            if (fields.some((i) => i.required && !parameters[i.name])) return;
            send1(parameters);
          },
        }), 0);
      },
    });
  };
  const active = pins.filter((p) => !p.superseded).length;
  return (
    <details className="railfold pinsfold" open={active > 0}>
      <summary>Pinned evidence{pins.length ? ` · ${active}` : ""}</summary>
      <div id="pinsBox">
        {!pins.length ? <Empty>Nothing pinned yet — pin key findings from tool results</Empty> : pins.map((p) => (
          <div key={p.interpretationId} className={"card pinrow" + (p.superseded ? " superseded" : "")}>
            {!p.superseded ? <button className="unpin" title="Supersede this pin (stays visible, struck)" onClick={() => openPrompt({
              title: "Remove from evidence", label: "Why remove this?", placeholder: "e.g. re-scoped — this logon was expected activity",
              helper: "The pin stays visible on the thread, struck — nothing is deleted.", confirm: "Remove",
              onConfirm: (reason) => send({ type: "pin.unpin", interpretationId: p.interpretationId, reason }),
            })}>un-pin</button> : null}
            <div className="finding">{p.finding}</div>
            <div className="cardmeta">{(p.actor === "AI_DELEGATED" ? "AI" : p.actor === "SYSTEM" ? "system" : "analyst") + (p.pinnedAt ? " · " + fmtDateTime(p.pinnedAt) : "")}</div>
            <div>{(p.inputRefs || []).map((r) => <RefChip key={r} refId={r} pinned={!p.superseded} />)}</div>
            {!p.superseded && !closed && state.actionTypes.length ? <a className="pinact" onClick={() => requestFromPin(p)}>Request action on this…</a> : null}
          </div>
        ))}
      </div>
    </details>
  );
}

export function ReasoningLens() {
  const { state, send, openPrompt } = useApp();
  const [view, setView] = useState<"cards" | "matrix">("cards");
  const hs = state.snap?.hypotheses || [];
  const pins = state.snap?.pins || [];
  const engine = state.snap ? String(state.snap.investigation.state).toUpperCase() : "";
  const closed = engine === "CONCLUDED" || engine === "ARCHIVED" || engine === "DISCARDED";
  const tally = hs.filter(isLive).map((h) => { const ps = h.predictions.filter(counted); return `${hypName(h, hs)} ${ps.filter(tested).length}/${ps.length}`; });
  void tally;
  return (
    <div className="lens reasoning">
      <VerdictBasis />
      <div className="lenstools">
        <div className="modetoggle" role="tablist">
          <button role="tab" aria-selected={view === "cards"} className={view === "cards" ? "on" : ""} onClick={() => setView("cards")}>Cards</button>
          <button role="tab" aria-selected={view === "matrix"} className={view === "matrix" ? "on" : ""} onClick={() => setView("matrix")}>Matrix</button>
        </div>
        {!closed ? <button className="railadd" title="Frame your own hypothesis — e.g. the alternate (benign or different-attacker) explanation. Lands OPEN, tested like any other." onClick={() => openPrompt({
          title: "New hypothesis", label: "The claim to test — ONE claim, often the ALTERNATE explanation",
          placeholder: "e.g. The logon was legitimate maintenance by the backup team, not lateral movement.",
          helper: "Recorded as yours, OPEN immediately, named H" + (hs.length + 1) + " (rename by clicking the handle). Then “Propose tests” on it — competing hypotheses are decided by evidence, not argument.",
          confirm: "Record hypothesis",
          onConfirm: (statement) => send({ type: "hyp.new", statement, handle: "H" + (hs.length + 1) }),
        })}>+ Add a hypothesis</button> : null}
      </div>
      {view === "matrix" ? <MatrixView /> : (
        <div id="hyps">{!hs.length ? <Empty>None yet</Empty> : orderHypotheses(hs).map((h) => <HypothesisCard key={h.id} h={h} hs={hs} pins={pins} />)}</div>
      )}
      <PinnedEvidence />
    </div>
  );
}
