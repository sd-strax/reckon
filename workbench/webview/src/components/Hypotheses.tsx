// The hypothesis tracker — the drivable loop over a COMPETING SET (02 §2.9,
// adr/0047) with 08 §8.4's density: a prediction row is ONE LINE at rest
// (glyph with its word for screen readers, the ordinal, a short title —
// the short title of record (adr/0090) — a `splits H<n>` marker, the
// evidence count or "attested"); expanding shows the statement, the
// attestation with its author by name, the evidence chips, what it decides
// against, the Pinned marker and the row's affordances.
import { useState } from "react";
import { useApp } from "../app";
import { blockersOf, badgeTone, counted, hypName, isLive, nameOf, predName, rivalsOf, tested } from "../derive/hyps";
import { Badge, RefChip } from "./shared";
import { pinSpec, type PromptExtra, type PromptSpec } from "./PromptCard";
import type { CommsThread, Hypothesis, PinRow, Prediction } from "../../../src/backend";
import type { InboundMessage } from "../../../src/protocol";

/** Record a prediction's result as the analyst (adr/0054): outcome, who was
 * asked and how, and what they said — the attestation the outcome cites. */
export function resultSpec(p: Prediction, h: Hypothesis, hs: Hypothesis[], comms: CommsThread[], send: (m: InboundMessage) => void): PromptSpec {
  const rivals = (p.discriminates || []).map((d) => nameOf(d, hs));
  const replies: { thread: CommsThread; entry: CommsThread["trail"][number] }[] = [];
  for (const t of comms) for (const e of t.trail || []) if (e.direction === "inbound") replies.push({ thread: t, entry: e });
  const extrasTop: PromptExtra[] = [{
    kind: "select", id: "status", label: "Outcome",
    options: [
      { value: "CONFIRMED", label: "Confirmed — observed as predicted" },
      { value: "DISCONFIRMED", label: "Disconfirmed — the opposite was observed" },
      { value: "INCONCLUSIVE", label: "Inconclusive — no clear answer" },
    ],
  }];
  if (replies.length) {
    extrasTop.push({
      kind: "select", id: "reply", label: "Prefill from a reply on a comms thread",
      options: [{ value: "", label: "—" }].concat(replies.map((r, i) => ({ value: String(i), label: (r.entry.author || r.thread.target) + " · " + new Date(r.entry.at).toLocaleString() + ": " + (r.entry.body.length > 60 ? r.entry.body.slice(0, 59) + "…" : r.entry.body) }))),
      onChange: (v, api) => {
        const r = replies[Number(v)];
        if (!r) return;
        api.setInput(r.entry.body);
        api.setExtra("source", r.entry.author || r.thread.target);
        api.setExtra("channel", (r.thread.actionType || "comms").replace("notify.", "") + " reply");
      },
    });
  }
  extrasTop.push(
    { kind: "text", id: "source", label: "Who was asked, or what was checked", placeholder: "e.g. J. Doe (account owner)" },
    { kind: "text", id: "channel", label: "How", placeholder: "e.g. phone, chat, email, in person" },
  );
  return {
    title: "Record result of " + predName(p) + " under " + hypName(h, hs),
    label: "What was observed — who said what, or what you found",
    placeholder: "e.g. Called the owner at 14:02: confirmed starting the session to finish the backup job.",
    helper: "Recorded as yours — an attestation the outcome cites. The record shows it as a human statement, never as telemetry."
      + (rivals.length ? " This test discriminates vs " + rivals.join(", ") + ": a confirmed result counts against them; a disconfirmed one supports them." : ""),
    confirm: "Record result",
    extrasTop,
    onConfirm: (content, _picked, ex) => send({
      type: "pred.result", predictionRef: p.id, status: String(ex.status || "INCONCLUSIVE"),
      content, source: typeof ex.source === "string" ? ex.source : undefined, channel: typeof ex.channel === "string" ? ex.channel : undefined,
    }),
  };
}

const GLYPH: Record<string, { g: string; word: string; tone: string }> = {
  CONFIRMED: { g: "✓", word: "confirmed", tone: "ok" },
  DISCONFIRMED: { g: "✕", word: "disconfirmed", tone: "bad" },
  INCONCLUSIVE: { g: "?", word: "inconclusive", tone: "" },
  UNTESTED: { g: "?", word: "untested", tone: "" },
  WITHDRAWN: { g: "–", word: "withdrawn", tone: "muted" },
};

function PredictionRow({ p, h, hs, pins }: { p: Prediction; h: Hypothesis; hs: Hypothesis[]; pins: PinRow[] }) {
  const { state, send, openPrompt, busy, openInspector } = useApp();
  const [open, setOpen] = useState(false);
  const live = isLive(h);
  const manual = p.testKind === "manual";
  const trefs = p.testResultRefs || [];
  const glyph = GLYPH[p.status] || GLYPH.UNTESTED;
  const splits = (p.discriminates || []).map((d) => nameOf(d, hs));
  const alreadyPinned = pins.some((x) => !x.superseded && ((x.inputRefs || []).includes(p.id) || (trefs.length > 0 && trefs.every((r) => (x.inputRefs || []).includes(r)))));
  const outstanding = live && p.status === "UNTESTED" && splits.length > 0;
  return (
    <li className={"prow" + (open ? " open" : "") + (outstanding ? " outstanding" : "") + (p.status === "WITHDRAWN" ? " withdrawn" : "")} id={"obj-" + p.id}>
      <div className="pline" onClick={() => setOpen((o) => !o)} role="button" aria-expanded={open}>
        <span className={"pglyph " + glyph.tone} aria-hidden="true">{glyph.g}</span><span className="sr">{glyph.word}</span>
        <span className="pord" title="Open this prediction" onClick={(e) => { e.stopPropagation(); openInspector({ kind: "ref", ref: p.id }); }}>{predName(p)}</span>
        <span className="ptitle" title={p.statement}>{p.title}</span>
        {splits.length ? <span className="psplits" title="Discriminating test: these rivals predict the opposite outcome">splits {splits.join(", ")}</span> : null}
        <span className="pcount">{manual && p.attestation ? "attested" : trefs.length ? `${trefs.length} evidence` : manual ? "a person tests this" : p.status === "UNTESTED" ? "untested" : ""}</span>
        <span className="pchev" aria-hidden="true">{open ? "⌄" : "›"}</span>
      </div>
      {open ? (
        <div className="pbody">
          <p className="pstatement">{p.statement}</p>
          {p.attestation ? <div className="attestline"><q>{p.attestation.content}</q><br /><b>{p.attestation.source || "a person"}</b> · recorded by {p.attestation.authorName || p.attestation.author}{p.attestation.channel ? " by " + p.attestation.channel : ""}</div> : null}
          {trefs.length ? <div className="pevidence">{trefs.map((r) => <RefChip key={r} refId={r} />)}</div> : null}
          {p.testQuery?.queryText ? <div className="imeta">Query: <span className="mono">{p.testQuery.queryText}</span>{p.testQuery.tool ? ` via ${p.testQuery.tool}` : ""}</div> : null}
          {splits.length ? <div className="imeta">Decides against {splits.join(", ")}{p.status === "CONFIRMED" ? " (counts against them)" : p.status === "DISCONFIRMED" ? " (supports them)" : ""}</div> : null}
          {alreadyPinned ? <div className="imeta">📌 Pinned</div> : null}
          <div className="predrow">
            <Badge tone={badgeTone(p.status)}>{p.status}</Badge>
            {outstanding ? <span className="outchip" title="Until this runs (or is withdrawn), neither side can be recorded decided.">undecided until tested</span> : null}
            {p.status === "UNTESTED" ? (
              <>
                {live ? (
                  <>
                    <button className="ghost testbtn" title="Discard this test without deleting it (→ WITHDRAWN)." onClick={() => openPrompt({
                      title: "Withdraw prediction", label: "Why (optional)", placeholder: "e.g. not testable with our verbs; same outcome under both hypotheses",
                      helper: "The record keeps it as WITHDRAWN; the tally stops counting it.", confirm: "Withdraw", require: false,
                      onConfirm: (reason) => send({ type: "pred.withdraw", predictionRef: p.id, reason }),
                    })}>Withdraw</button>
                    <button className={manual ? "testbtn" : "ghost testbtn"} title={manual ? "Record what the person said as the result. The agent cannot record it." : "Record the result yourself — kept as an attestation the outcome cites."} onClick={() => openPrompt(resultSpec(p, h, hs, state.snap?.comms || [], send))}>Record result</button>
                  </>
                ) : null}
                {!manual ? <button className="testbtn predact" disabled={busy} title={p.testQuery?.queryText ? "Runs just this test as an agent turn: " + p.testQuery.queryText : "Runs a test of just this prediction as an agent turn"} onClick={() => {
                  if (busy) return;
                  send({ type: "pred.test", predictionRef: p.id, predictionName: p.ordinal ? predName(p) : "this prediction", statement: p.statement, queryText: p.testQuery?.queryText, tool: p.testQuery?.tool, hypothesisRef: h.id, hypStatement: h.statement, handle: h.handle, rivals: rivalsOf(h, hs) });
                }}>⚡ Test {p.ordinal ? predName(p) : "this"}</button> : null}
              </>
            ) : trefs.length ? (
              <button className="pincta testbtn predact" disabled={alreadyPinned} title={alreadyPinned ? "Already in the pinned evidence — un-pin there to revise" : "Pin this decided prediction + its test evidence (feeds the verdict gate)"}
                onClick={() => openPrompt(pinSpec([p.id].concat(trefs), "[" + p.status + "] " + p.statement, (finding) => send({ type: "pin.add", refs: [p.id].concat(trefs), finding })))}>{alreadyPinned ? "📌 pinned" : "📌 Pin"}</button>
            ) : null}
          </div>
        </div>
      ) : null}
    </li>
  );
}

export function HypothesisCard({ h, hs, pins }: { h: Hypothesis; hs: Hypothesis[]; pins: PinRow[] }) {
  const { send, openPrompt, busy, openInspector } = useApp();
  const live = isLive(h);
  const preds = h.predictions.filter(counted);
  const anyTested = preds.some(tested);
  const hasUntested = preds.some((p) => p.status === "UNTESTED");
  const name = hypName(h, hs);
  const blockers = blockersOf(h, hs);
  const aside = (label: string, title: string, type: "hyp.proposeTests" | "hyp.runTests" | "hyp.decide") => (
    <button className="askdecide" disabled={busy} title={title} onClick={() => { if (!busy) send({ type, hypothesisRef: h.id, statement: h.statement, handle: h.handle, rivals: rivalsOf(h, hs) }); }}>{label}</button>
  );
  return (
    <div className="card hypcard" id={"obj-" + h.id}>
      <div className="hyphead">
        <span className="hyphandle" title={live ? "Rename this hypothesis" : h.status} onClick={live ? () => openPrompt({
          title: "Rename " + name, label: "Short handle — how you and the agent will refer to it", value: name, selectAll: true, placeholder: "e.g. H2 legit-admin",
          helper: "Appears in the tabs, the composer, the thread and the export.", confirm: "Rename",
          onConfirm: (handleText) => send(h.status === "PROPOSED" ? { type: "hyp.ack", hypothesisRef: h.id, handle: handleText } : { type: "hyp.revise", hypothesisRef: h.id, handle: handleText }),
        }) : undefined}>{name}</span>
        <span className="hypmeta">
          {preds.length ? <span className="hypcount" title="Predictions tested over predictions recorded (withdrawn ones not counted)">{preds.filter(tested).length}/{preds.length} tested</span> : null}
          <Badge tone={badgeTone(h.status)} title={h.status === "PROPOSED" ? "Proposed by the agent. Adopt it to take ownership." : h.status === "OPEN" ? "Adopted — yours. Decided by its tests against the rivals." : h.status}>{h.status}</Badge>
        </span>
      </div>
      <span className="statement" title="Open this hypothesis" onClick={() => openInspector({ kind: "ref", ref: h.id })}>{h.statement}</span>
      <ul className="predictions rows">
        {h.predictions.map((p) => <PredictionRow key={p.id} p={p} h={h} hs={hs} pins={pins} />)}
      </ul>
      {!h.predictions.length ? <div className="predhead">{live ? "No predictions yet — nothing can confirm or refute this until a test is recorded (⚡ Propose tests, or + prediction)." : "No predictions were recorded."}</div> : null}
      <div className="decide">
        {h.status === "PROPOSED" ? (
          <button className="primary" title="Take ownership of this AI-proposed line of inquiry (PROPOSED → OPEN), sharpening the claim if you want." onClick={() => openPrompt({
            title: "Adopt " + name, label: "The claim, as you would state it", value: h.statement,
            helper: anyTested ? "Predictions under this hypothesis already carry results, so the wording is frozen — adopt as is." : "Edit freely: the original stays in the thread, the tests are derived from what you adopt. Cmd/Ctrl+Enter to adopt.",
            confirm: "✓ Adopt",
            onConfirm: (statement) => send({ type: "hyp.ack", hypothesisRef: h.id, restatement: statement !== h.statement ? statement : undefined }),
          })}>✓ Adopt</button>
        ) : null}
        {h.status === "OPEN" ? (
          !preds.length ? aside("⚡ Propose tests", "Runs a scoped agent turn over the whole competing set: record the falsifiable predictions that would decide this.", "hyp.proposeTests")
          : hasUntested ? aside("⚡ Run tests", "Runs the untested predictions — independent, so in parallel where possible.", "hyp.runTests")
          : <>{aside("⚡ Decide", "Weighs the tested predictions AGAINST THE RIVALS and records the outcome citing the decisive evidence.", "hyp.decide")}
              {blockers.length ? <span className="guardnote" title="The engine will not record this hypothesis decided while a rival holds an untested prediction that comes out the opposite way under it.">blocked: {blockers.map((x) => predName(x.p) + " (" + hypName(x.owner, hs) + ")").join(", ")} untested</span> : null}</>
        ) : null}
        {live ? (
          <>
            <button className="ghost" title="Add your own test under this hypothesis. Lands UNTESTED; mark the rivals it would decide against." onClick={() => {
              const rivals = hs.filter((r) => r.id !== h.id && isLive(r));
              openPrompt({
                title: "New prediction under " + name, label: "If " + name + " is true, we should observe…",
                placeholder: "e.g. the account's 30-day logon history shows only network logons from the backup host at 02:00.",
                helper: "Recorded as yours, UNTESTED. ⚡ Test on its row runs it as an agent turn. A test a person answers is yours to record.", confirm: "Record prediction",
                choicesLabel: rivals.length ? "This test comes out the OPPOSITE way under (discriminates):" : "",
                choices: rivals.map((r) => ({ id: r.id, label: hypName(r, hs) + " — " + (r.statement.length > 70 ? r.statement.slice(0, 69) + "…" : r.statement), title: r.statement, checked: rivals.length === 1 })),
                extras: [
                  { kind: "text", id: "title", label: "Short title (optional, ≤ 64 characters; derived from the statement when empty)", placeholder: "e.g. the account's 30-day logon history", title: "The row's name of record (adr/0090); frozen with the statement." },
                  { kind: "check", id: "manual", label: "A person tests this — the account owner, the change owner, a phone call. No tool query; I record the result.", title: "A manual test (adr/0054)." },
                ],
                onConfirm: (statement, picked, ex) => send({ type: "pred.new", hypothesisRef: h.id, statement, discriminates: picked || [], testKind: ex.manual ? "manual" : undefined, title: typeof ex.title === "string" && ex.title.trim() ? ex.title.trim() : undefined }),
              });
            }}>+ prediction</button>
            {h.status === "OPEN" && !anyTested ? <button className="ghost" title="Sharpen the claim (legal while nothing under it has been tested)." onClick={() => openPrompt({
              title: "Restate " + name, label: "The claim, sharpened", value: h.statement, helper: "Predictions already recorded stay as they are — withdraw any the new wording makes moot.", confirm: "Restate",
              onConfirm: (statement) => { if (statement !== h.statement) send({ type: "hyp.revise", hypothesisRef: h.id, restatement: statement }); },
            })}>Restate</button> : null}
            <button className="ghost" title="Not pursuing this line of inquiry (→ ABANDONED, terminal; the thread keeps it)." onClick={() => openPrompt({
              title: "Abandon " + name, label: "Why it is not being pursued (optional)", placeholder: "e.g. superseded by H3", helper: "Terminal: a later change of mind is a new hypothesis.", confirm: "Abandon", require: false,
              onConfirm: (reason) => send({ type: "hyp.abandon", hypothesisRef: h.id, reason }),
            })}>Abandon</button>
          </>
        ) : null}
      </div>
    </div>
  );
}
