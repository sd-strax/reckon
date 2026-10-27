// The hypothesis tracker — the drivable loop over a COMPETING SET (02 §2.9,
// adr/0047). Every hypothesis is a card, live ones first, the claim in full:
// the comparison IS the content. The decide guard appears beside ⚡ Decide and
// as a mark on the outstanding discriminator — where it bites — never as a
// banner telling the analyst what to do next.
import { useApp } from "../app";
import { blockersOf, badgeTone, counted, hypName, isLive, nameOf, orderHypotheses, predName, rivalsOf, tested } from "../derive/hyps";
import { Badge, Empty, Eyebrow, RefChip } from "./shared";
import { pinSpec, type PromptExtra, type PromptSpec } from "./PromptCard";
import type { CommsThread, Hypothesis, PinRow, Prediction } from "../../../src/backend";
import type { InboundMessage } from "../../../src/protocol";

/** Record a prediction's result as the analyst (adr/0054): outcome, who was
 * asked and how, and what they said — the attestation the outcome cites. A
 * reply already on a comms thread can be prefilled (quoted, not linked). */
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
      options: [{ value: "", label: "—" }].concat(replies.map((r, i) => ({
        value: String(i),
        label: (r.entry.author || r.thread.target) + " · " + new Date(r.entry.at).toLocaleString() + ": " + (r.entry.body.length > 60 ? r.entry.body.slice(0, 59) + "…" : r.entry.body),
      }))),
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
    placeholder: "e.g. Called J. Doe at 14:02: confirmed starting the RDP session from home over VPN to finish the backup job.",
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

function PredictionRow({ p, h, hs, pins }: { p: Prediction; h: Hypothesis; hs: Hypothesis[]; pins: PinRow[] }) {
  const { state, send, openPrompt, busy } = useApp();
  const live = isLive(h);
  const outstanding = live && p.status === "UNTESTED" && (p.discriminates || []).length > 0;
  const manual = p.testKind === "manual";
  const trefs = p.testResultRefs || [];
  // A decided prediction is pinnable: the pin cites the PREDICTION NODE too, so
  // an active pin is detectable and the affordance goes inert instead of
  // minting duplicates (un-pin brings it back — supersession, not deletion).
  const alreadyPinned = pins.some((x) => !x.superseded && ((x.inputRefs || []).includes(p.id) || (trefs.length > 0 && trefs.every((r) => (x.inputRefs || []).includes(r)))));
  return (
    <li className={outstanding ? "outstanding" : undefined} style={p.status === "WITHDRAWN" ? { opacity: 0.55 } : undefined}>
      <div className="predline">
        {p.ordinal ? <span className="pord" title="How to refer to this prediction — in the composer, and in the agent's reasoning state">P{p.ordinal}</span> : null}
        <span className="predtext">{p.statement}</span>
      </div>
      <div className="predrow">
        <Badge tone={badgeTone(p.status)}>{p.status}</Badge>
        {(p.discriminates || []).length ? (
          <span className="discchip" title="Discriminating test: these rivals predict the opposite outcome. CONFIRMED counts against them; DISCONFIRMED supports them.">
            vs <b>{p.discriminates.map((d) => nameOf(d, hs)).join(", ")}</b>
          </span>
        ) : null}
        {manual ? (
          <span className="manualchip" title={(p.testQuery && (p.testQuery.queryText || p.testQuery.tool))
            ? "Ask " + (p.testQuery.tool || "them") + (p.testQuery.queryText ? ": " + p.testQuery.queryText : "") + " — then Record result. The agent cannot record it."
            : "Decided by someone's answer, not a query. Ask them, then Record result. The agent cannot record it."}>a person tests this</span>
        ) : null}
        {outstanding ? <span className="outchip" title="This is the test that separates the hypotheses. Until it runs (or is withdrawn), neither side can be recorded decided.">undecided until tested</span> : null}
        {trefs.map((r) => <RefChip key={r} refId={r} />)}
        {p.status === "UNTESTED" ? (
          <>
            {live ? (
              <>
                <button className="ghost testbtn predact" title="Discard this test without deleting it (→ WITHDRAWN). To edit a test: withdraw it and add the corrected one." onClick={() => openPrompt({
                  title: "Withdraw prediction",
                  label: "Why (optional)",
                  placeholder: "e.g. not testable with our verbs; same outcome under both hypotheses",
                  helper: "The record keeps it as WITHDRAWN; the tally stops counting it.",
                  confirm: "Withdraw",
                  require: false,
                  onConfirm: (reason) => send({ type: "pred.withdraw", predictionRef: p.id, reason }),
                })}>Withdraw</button>
                <button className={manual ? "testbtn" : "ghost testbtn"} title={manual
                  ? "Record what the person said as the result of " + predName(p) + ". Kept as an attestation the outcome cites — a human statement, never telemetry."
                  : "Record the result yourself — e.g. you checked it by hand, or asked someone. Kept as an attestation the outcome cites, distinct from telemetry."}
                  onClick={() => openPrompt(resultSpec(p, h, hs, state.snap?.comms || [], send))}>Record result</button>
              </>
            ) : null}
            {!manual ? (
              <button className={"testbtn" + (live ? "" : " predact")} disabled={busy} title={p.testQuery && p.testQuery.queryText
                ? "Runs just this test as an agent turn: " + p.testQuery.queryText + " — the outcome and its evidence land on this row."
                : "Runs a test of just this prediction as an agent turn; the outcome and its evidence land on this row."}
                onClick={() => {
                  if (busy) return;
                  send({
                    type: "pred.test", predictionRef: p.id, predictionName: p.ordinal ? predName(p) : "this prediction",
                    statement: p.statement, queryText: p.testQuery?.queryText, tool: p.testQuery?.tool,
                    hypothesisRef: h.id, hypStatement: h.statement, handle: h.handle, rivals: rivalsOf(h, hs),
                  });
                }}>⚡ Test {p.ordinal ? predName(p) : "this"}</button>
            ) : null}
          </>
        ) : trefs.length ? (
          <button className="pincta testbtn predact" disabled={alreadyPinned}
            title={alreadyPinned ? "Already in the pinned evidence — un-pin there to revise" : "Pin this decided prediction + its test evidence (feeds the verdict gate)"}
            onClick={() => openPrompt(pinSpec([p.id].concat(trefs), "[" + p.status + "] " + p.statement, (finding) => send({ type: "pin.add", refs: [p.id].concat(trefs), finding })))}>
            {alreadyPinned ? "📌 pinned" : "📌 Pin"}
          </button>
        ) : null}
      </div>
      {p.attestation ? (
        // A human statement as evidence reads as one (adr/0054): who attested,
        // how, who was asked, and the words — never as a log line.
        <div className="attestline">
          <b>attested by {p.attestation.authorName || p.attestation.author}{p.attestation.channel ? " by " + p.attestation.channel : ""}{p.attestation.source ? " (" + p.attestation.source + ")" : ""}</b>: <q>{p.attestation.content}</q>
        </div>
      ) : null}
    </li>
  );
}

export function HypothesisCard({ h, hs, pins }: { h: Hypothesis; hs: Hypothesis[]; pins: PinRow[] }) {
  const { send, openPrompt, busy } = useApp();
  const live = isLive(h);
  const preds = (h.predictions || []).filter(counted);
  const anyTested = preds.some(tested);
  const hasUntested = preds.some((p) => p.status === "UNTESTED");
  const name = hypName(h, hs);
  const all = h.predictions || [];
  const blockers = blockersOf(h, hs);

  const aside = (label: string, title: string, type: "hyp.proposeTests" | "hyp.runTests" | "hyp.decide") => (
    <button className="askdecide" disabled={busy} title={title} onClick={() => {
      if (busy) return;
      send({ type, hypothesisRef: h.id, statement: h.statement, handle: h.handle, rivals: rivalsOf(h, hs) });
    }}>{label}</button>
  );

  return (
    <div className="card">
      <div className="hyphead">
        <span className="hyphandle" title={live ? "Rename this hypothesis" : h.status} onClick={live ? () => openPrompt({
          title: "Rename " + name,
          label: "Short handle — how you and the agent will refer to it",
          value: name,
          selectAll: true,
          placeholder: "e.g. H2 legit-admin",
          helper: "Appears in the tabs, the composer, the thread and the export.",
          confirm: "Rename",
          onConfirm: (handleText) => send(h.status === "PROPOSED"
            ? { type: "hyp.ack", hypothesisRef: h.id, handle: handleText }
            : { type: "hyp.revise", hypothesisRef: h.id, handle: handleText }),
        }) : undefined}>{name}</span>
        <span className="hypmeta">
          <Badge tone={badgeTone(h.status)} title={h.status === "PROPOSED"
            ? "Proposed by the agent. Adopt it to take ownership. Until a human does, the agent can propose and run tests against it but cannot record it supported or refuted."
            : h.status === "OPEN" ? "Adopted — yours. Decided by its tests against the rivals." : h.status}>{h.status}</Badge>
          {preds.length ? <span className="hypcount" title="Predictions tested over predictions recorded (withdrawn ones not counted)">{preds.filter(tested).length}/{preds.length} tested</span> : null}
        </span>
      </div>
      <span className="statement">{h.statement}</span>
      <div className="decide">
        {h.status === "PROPOSED" ? (
          // Adopt = acknowledge-with-restatement (01 §x-hypothesis): the
          // statement is editable in the card — sharpening the claim is the
          // ownership moment. Unchanged text sends a plain acknowledgment.
          <button className="primary" title="Take ownership of this AI-proposed line of inquiry (PROPOSED → OPEN), sharpening the claim if you want. A human act — the engine refuses it from the AI." onClick={() => openPrompt({
            title: "Adopt " + name,
            label: "The claim, as you would state it",
            value: h.statement,
            helper: anyTested
              ? "Predictions under this hypothesis already carry results, so the wording is frozen — adopt as is; a changed theory is a new hypothesis."
              : "Edit freely: the original stays in the thread, the tests are derived from what you adopt. Cmd/Ctrl+Enter to adopt.",
            confirm: "✓ Adopt",
            onConfirm: (statement) => send({ type: "hyp.ack", hypothesisRef: h.id, restatement: statement !== h.statement ? statement : undefined }),
          })}>✓ Adopt</button>
        ) : null}
        {/* The drive affordance (02 §2.9) follows the state of the work — one rung at a time. */}
        {h.status === "OPEN" ? (
          !preds.length ? aside("⚡ Propose tests", "Runs a scoped agent turn over the whole competing set: record the falsifiable predictions that would decide this — at least one that comes out the opposite way under a rival. Lands as a collapsed aside; predictions appear here.", "hyp.proposeTests")
          : hasUntested ? aside("⚡ Run tests", "Runs the untested predictions — they are independent, so they run in parallel where possible. Outcomes and evidence land back on these rows.", "hyp.runTests")
          : (
            <>
              {aside("⚡ Decide", "Weighs the tested predictions AGAINST THE RIVALS and records the hypothesis outcome (supported / refuted / inconclusive) citing the decisive evidence. The engine refuses it while a rival's discriminating test is untested: test it, withdraw it, or record its result yourself. Terminal in v0.", "hyp.decide")}
              {blockers.length ? (
                <span className="guardnote" title="The engine will not record this hypothesis decided while a rival holds an untested prediction that comes out the opposite way under it. Test it, withdraw it, or record its result yourself (Record result on the row).">
                  blocked: {blockers.map((x) => predName(x.p) + " (" + hypName(x.owner, hs) + ")").join(", ")}{blockers.length === 1 ? " is untested and splits it from " : " are untested and split it from "}{name}
                </span>
              ) : null}
            </>
          )
        ) : null}
        {live ? (
          <>
            <button className="ghost" title="Add your own test under this hypothesis — e.g. a baseline only you know. Lands UNTESTED; mark the rivals it would decide against." onClick={() => {
              const rivals = hs.filter((r) => r.id !== h.id && isLive(r));
              openPrompt({
                title: "New prediction under " + name,
                label: "If " + name + " is true, we should observe…",
                placeholder: "e.g. svc_backup's 30-day logon baseline shows only Network logons from BKP-01 at 02:00 — never RemoteInteractive, never from 10.0.4.20.",
                helper: "Recorded as yours, UNTESTED. ⚡ Test on its row runs it as an agent turn that records the outcome with evidence. A test a person answers is yours to record, with what they said.",
                confirm: "Record prediction",
                choicesLabel: rivals.length ? "This test comes out the OPPOSITE way under (discriminates):" : "",
                choices: rivals.map((r) => ({ id: r.id, label: hypName(r, hs) + " — " + (r.statement.length > 70 ? r.statement.slice(0, 69) + "…" : r.statement), title: r.statement, checked: rivals.length === 1 })),
                extras: [{ kind: "check", id: "manual", label: "A person tests this — the account owner, the change owner, a phone call. No tool query; I record the result.", title: "A manual test (adr/0054): the agent can propose one and remind you of it, never record its result." }],
                onConfirm: (statement, picked, ex) => send({ type: "pred.new", hypothesisRef: h.id, statement, discriminates: picked || [], testKind: ex.manual ? "manual" : undefined }),
              });
            }}>+ prediction</button>
            {h.status === "OPEN" && !anyTested ? (
              <button className="ghost" title="Sharpen the claim (legal while nothing under it has been tested). The original stays in the thread." onClick={() => openPrompt({
                title: "Restate " + name,
                label: "The claim, sharpened",
                value: h.statement,
                helper: "Predictions already recorded stay as they are — withdraw any the new wording makes moot.",
                confirm: "Restate",
                onConfirm: (statement) => { if (statement !== h.statement) send({ type: "hyp.revise", hypothesisRef: h.id, restatement: statement }); },
              })}>Restate</button>
            ) : null}
            <button className="ghost" title="Not pursuing this line of inquiry (→ ABANDONED, terminal; the thread keeps it). Keeps unadopted proposals from piling up." onClick={() => openPrompt({
              title: "Abandon " + name,
              label: "Why it is not being pursued (optional)",
              placeholder: "e.g. superseded by H3; no evidence path available in this tenant",
              helper: "Terminal: a later change of mind is a new hypothesis. The verdict residual stops listing it.",
              confirm: "Abandon",
              require: false,
              onConfirm: (reason) => send({ type: "hyp.abandon", hypothesisRef: h.id, reason }),
            })}>Abandon</button>
          </>
        ) : null}
      </div>
      {/* The predictions name themselves (adr/0047, #52). */}
      <div className="predhead" title="A prediction is a falsifiable test of the hypothesis. One marked “vs H2” is a discriminator: it comes out the opposite way under that rival, so its result moves both.">
        {all.length
          ? "Predictions — if this hypothesis is true, we should observe each of these. A confirmed one supports it; a disconfirmed one counts against it."
          : (live ? "No predictions yet — nothing can confirm or refute this until a test is recorded (⚡ Propose tests, or + prediction)." : "No predictions were recorded.")}
      </div>
      {all.length ? (
        <ul className="predictions">
          {all.map((p) => <PredictionRow key={p.id} p={p} h={h} hs={hs} pins={pins} />)}
        </ul>
      ) : null}
    </div>
  );
}

export function Hypotheses() {
  const { state, send, openPrompt } = useApp();
  const hs = state.snap?.hypotheses || [];
  const pins = state.snap?.pins || [];
  return (
    <section>
      <Eyebrow>
        Hypotheses{" "}
        <button className="railadd" title="Frame your own hypothesis — e.g. the alternate (benign or different-attacker) explanation. Lands OPEN, tested like any other." onClick={() => openPrompt({
          title: "New hypothesis",
          label: "The claim to test — ONE claim, often the ALTERNATE explanation",
          placeholder: "e.g. The svc_backup RDP logon was legitimate maintenance by the backup team, not lateral movement.",
          helper: "Recorded as yours, OPEN immediately, named H" + (hs.length + 1) + " (rename by clicking the handle). Then “Propose tests” on it — competing hypotheses are decided by evidence, not argument.",
          confirm: "Record hypothesis",
          onConfirm: (statement) => send({ type: "hyp.new", statement, handle: "H" + (hs.length + 1) }),
        })}>+ new…</button>
      </Eyebrow>
      <div id="hyps">
        {!hs.length ? <Empty>None yet</Empty> : orderHypotheses(hs).map((h) => <HypothesisCard key={h.id} h={h} hs={hs} pins={pins} />)}
      </div>
    </section>
  );
}
