// Summary — where it stands (08 §8.4): four tiles, the narrative (gap §6.18:
// absent until drafted, never templated), key entities with derivable
// status words, still to do, procedures followed, the handoff note and the
// export. Every element names its source or says it is a gap.
import { useApp } from "../../app";
import { terminal } from "../../derive/stage";
import { hypName } from "../../derive/hyps";
import { handoffNote, keyEntities, proceduresFollowed, stillToDo } from "../../derive/summary";
import { Badge, Countdown, Empty, RefChip, fmtTime } from "../shared";
import { verdictLinks } from "../../derive/verdict";
import { scrollToAct } from "../Chronicle";

/** What concluding produced (adr/0156): the knowledge summary's outcome, and a
 * procedure draft the analyst asked for — its progress, a decline with the
 * reason, a failure, or the draft with Add to SOP library / Discard. */
function AfterConclusion() {
  const { state, send } = useApp();
  const sum = state.summaryStatus;
  const d = state.sopDraft;
  return (
    <div className="afterconclusion">
      {sum ? <div className="imeta">Knowledge summary: {sum.state === "written" ? `narrative written${sum.detail ? " by " + sum.detail : ""}` : sum.state === "baseline" ? "the structured baseline only — no narrative was written" : `the narrative failed (${sum.detail || "unknown error"}); the structured baseline stands`}</div> : null}
      {d?.state === "drafting" ? <div className="imeta">Drafting a procedure from this case…</div> : null}
      {d?.state === "declined" ? <div className="imeta">No new procedure: {d.message} <a onClick={() => send({ type: "sop.draft", force: true })}>Draft anyway</a></div> : null}
      {d?.state === "failed" ? <div className="imeta err">Drafting failed: {d.message} <a onClick={() => send({ type: "sop.draft" })}>Try again</a></div> : null}
      {d?.state === "ready" ? (
        <div className="card sopdraft">
          <div className="kicker">Draft procedure</div>
          <div><strong>{d.title}</strong> — open beside the panel; edit it there before adding.</div>
          {d.message ? <div className="imeta">{d.message}</div> : null}
          <div className="decide">
            <button className="primary" onClick={() => send({ type: "sop.accept" })}>Add to SOP library</button>
            <button onClick={() => send({ type: "sop.discard" })}>Discard</button>
          </div>
        </div>
      ) : null}
      {d?.state === "added" ? <div className="imeta">“{d.title}” {d.message}.</div> : null}
      {d?.state === "discarded" ? <div className="imeta">Draft discarded.</div> : null}
    </div>
  );
}

export function SummaryLens() {
  const { state, send, openInspector, goTo, setCompanionMode } = useApp();
  const snap = state.snap;
  if (!snap) return null;
  const hs = snap.hypotheses;
  const actions = snap.actions || [];
  const comms = snap.comms || [];
  const verdict = snap.investigation.verdict;
  const concluded = String(snap.investigation.state).toUpperCase() === "CONCLUDED";
  const contain = actions.filter((a) => !a.actionType.startsWith("notify."));
  const approved = contain.filter((a) => a.status !== "REQUESTED" && a.status !== "PENDING_SECONDARY" && a.status !== "REJECTED" && a.status !== "EXPIRED").length;
  const nearest = actions.filter((a) => a.pending && !a.expired && a.expiresAt).map((a) => a.expiresAt!).sort()[0];
  const notify = actions.filter((a) => a.actionType.startsWith("notify."));
  const awaiting = comms.filter((t) => t.status === "awaiting_reply" || t.status === "followed_up").length;
  const open = hs.flatMap((h) => h.status === "ABANDONED" ? [] : h.predictions.filter((p) => p.status === "UNTESTED" || p.status === "INCONCLUSIVE").map((p) => ({ h, p })));
  const ents = keyEntities(snap.investigation.seed, snap.thread || [], snap.pins, actions, state.labels, snap.derived?.entities);
  const todo = stillToDo(actions, comms, hs, state.labels);
  const followed = proceduresFollowed(snap.thread || []);
  // The verdict of record replaced an earlier one (#155): say so where the verdict is read.
  const current = (snap.thread || []).filter((e) => e.interpretationType === "verdict" && !e.superseded).pop();
  const replacedVerdict = current ? verdictLinks(snap.thread || []).get(current.interpretationId)?.replaces : undefined;
  const supported = hs.filter((h) => h.status === "SUPPORTED").map((h) => hypName(h, hs));
  const refuted = hs.filter((h) => h.status === "REFUTED").map((h) => hypName(h, hs));
  return (
    <div className="lens summary">
      <div className="tiles">
        <div className="tile" onClick={() => goTo({ kind: "lens", lens: "reasoning" })}>
          <div className="kicker">Verdict</div>
          <div className="tilemain">{verdict ? verdict.disposition.toLowerCase() : "none yet"}</div>
          <div className="imeta">{verdict ? [supported.length ? supported.join(", ") + " supported" : "", refuted.length ? refuted.join(", ") + " refuted" : ""].filter(Boolean).join(", ") || "recorded" : "record one from the Reasoning lens"}</div>
          {replacedVerdict ? (
            <a className="tilenote warnnote" onClick={(ev) => { ev.stopPropagation(); setCompanionMode("thread"); setTimeout(() => scrollToAct(replacedVerdict.interpretationId), 0); }}>
              replaces {replacedVerdict.disposition ? `a ${replacedVerdict.disposition} verdict` : "an earlier verdict"} from {fmtTime(replacedVerdict.occurredAt)}
            </a>
          ) : null}
        </div>
        <div className="tile" onClick={() => goTo({ kind: "lens", lens: "response" })}>
          <div className="kicker">Containment</div>
          <div className="tilemain">{contain.length ? `${approved} of ${contain.length} approved` : "no actions"}</div>
          <div className="imeta">{nearest ? <>first window closes in <Countdown expiresAt={nearest} className="inline" /></> : contain.length && contain.every(terminal) ? "every action terminal" : "—"}</div>
        </div>
        <div className="tile" onClick={() => goTo({ kind: "lens", lens: "response", anchor: "notification" })}>
          <div className="kicker">Notification</div>
          <div className="tilemain">{notify.some((a) => a.status === "SUCCEEDED") ? "notified" : notify.some((a) => a.pending) ? "awaiting approval" : "not yet notified"}</div>
          <div className="imeta">{awaiting ? `${awaiting} reply outstanding` : notify.length ? `${notify.length} sent or requested` : "ask reckon in the conversation"}</div>
        </div>
        <div className="tile" onClick={() => goTo({ kind: "lens", lens: "reasoning" })}>
          <div className="kicker">Open questions</div>
          <div className="tilemain">{open.length}</div>
          <div className="imeta">{open.slice(0, 2).map(({ h, p }) => `P${p.ordinal ?? "?"} (${hypName(h, hs)})`).join(", ") || "none"}</div>
        </div>
      </div>

      <section className="lsection">
        <h3>What happened</h3>
        <Empty>No narrative yet — it is written when the investigation concludes. The Thread and the Story lens hold the record.</Empty>
      </section>

      <section className="lsection">
        <h3>Key entities</h3>
        {ents.length ? (
          <div className="entities">
            {ents.map((e) => (
              <div key={e.ref} className="entrow">
                <RefChip refId={e.ref} />
                <span className="imeta">{e.role}{e.citations ? ` · cited ${e.citations}×` : ""}</span>
                {e.status ? <Badge tone={e.status.endsWith("requested") ? "warn" : "info"}>{e.status}</Badge> : null}
              </div>
            ))}
          </div>
        ) : <Empty>no entities cited yet</Empty>}
      </section>

      <section className="lsection">
        <h3>Still to do</h3>
        {todo.length ? (
          <ul className="todo">
            {todo.map((t, i) => (
              <li key={i} className={t.kind}>
                <span className="dot" aria-hidden="true">{t.kind === "approve" ? "●" : "○"}</span>
                {t.ref ? <a onClick={() => openInspector({ kind: "ref", ref: t.ref! })}>{t.text}</a> : <a onClick={() => goTo({ kind: "lens", lens: t.kind === "prediction" ? "reasoning" : "response" })}>{t.text}</a>}
                {t.expiresAt ? <Countdown expiresAt={t.expiresAt} className="inline" /> : null}
              </li>
            ))}
          </ul>
        ) : <Empty>nothing outstanding</Empty>}
      </section>

      <section className="lsection">
        <h3>Procedures followed</h3>
        {followed.length ? followed.map((f) => <div key={f.sopId}><a onClick={() => openInspector({ kind: "sop", sopId: f.sopId })}>{f.title}</a></div>) : <Empty>none applied yet</Empty>}
      </section>

      <div className="decide">
        <button onClick={() => send({ type: "copy", text: handoffNote(snap.investigation.title, verdict, actions, todo, hs, followed) })}>Copy handoff note</button>
        <button title="The live markdown snapshot — paste into a ticket unedited" onClick={() => send({ type: "export.open" })}>Export report</button>
        {concluded ? <button title="Ask reckon to generalize a reusable procedure from this case; you review it before it joins the library" disabled={state.sopDraft?.state === "drafting"} onClick={() => send({ type: "sop.draft" })}>Draft SOP from this case</button> : null}
      </div>
      {concluded ? <AfterConclusion /> : null}
    </div>
  );
}
