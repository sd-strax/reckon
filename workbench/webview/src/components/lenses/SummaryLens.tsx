// Summary — where it stands (08 §8.4): four tiles, the narrative (gap §6.18:
// absent until drafted, never templated), key entities with derivable
// status words, still to do, procedures cited, the handoff note and the
// export. Every element names its source or says it is a gap.
import { useState } from "react";
import { useApp } from "../../app";
import { terminal } from "../../derive/stage";
import { hypName } from "../../derive/hyps";
import { handoffNote, keyEntities, proceduresCited, stillToDo } from "../../derive/summary";
import { Badge, Countdown, Empty, Markdown, RefChip, fmtDateTime, fmtTime } from "../shared";
import type { NarrativeAuthor, NarrativeView } from "../../../../src/viewTypes";
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

/** "drafted by reckon" or "drafted by <name>", with the model when the record names one. */
function authorWords(a: NarrativeAuthor): string {
  if (a.kind === "AI_DELEGATED") return "drafted by reckon" + (a.model ? ` (${a.model})` : "");
  return "drafted by " + (a.name || "a person");
}

/**
 * What happened (ui/08 §8.4, adr/0103): the narrative with its authorship,
 * drafted by the model on request and revised by a person, never templated.
 * The line names who drafted the first revision and who edited last, says
 * how far the record has moved since it was written, and offers Draft and
 * Edit. Nothing here is written until the analyst asks.
 */
function WhatHappened({ narrative, lastEventSequence, hasRecord }: { narrative: NarrativeView | undefined; lastEventSequence: number; hasRecord: boolean }) {
  const { state, send, dispatch } = useApp();
  const status = state.narrativeStatus;
  // The editor's text, and the record's save count when it was submitted.
  // The text is the only copy of the analyst's edit, so the editor stays
  // open with it until the record carries the revision (the count moves) or
  // the analyst cancels; a refused save shows why and keeps the text.
  const [editing, setEditing] = useState<{ text: string; submittedAt: number | null } | null>(null);
  const drafting = status?.state === "drafting";
  const open = editing !== null && (editing.submittedAt === null || editing.submittedAt === state.narrativeSaves);
  const save = () => {
    if (!editing) return;
    const body = editing.text.trim();
    if (!body || body === narrative?.body) {
      setEditing(null);
      return;
    }
    setEditing({ ...editing, submittedAt: state.narrativeSaves });
    // Saving from this instant, ahead of the host's own word, so a second
    // press cannot send the revision twice and an earlier refusal is gone.
    dispatch({ type: "msg", msg: { type: "narrative.status", state: "saving" } });
    send({ type: "narrative.edit", body });
  };
  if (editing !== null && open) {
    const submitted = editing.submittedAt !== null;
    const refused = submitted && status?.state === "save-failed" ? (status.message ?? "the record refused it") : null;
    const saving = submitted && refused === null;
    return (
      <div className="narrative-edit">
        <textarea aria-label="Narrative" value={editing.text} onChange={(e) => setEditing({ text: e.target.value, submittedAt: null })} rows={10} disabled={saving} />
        <div className="decide">
          <button className="primary" disabled={saving || !editing.text.trim()} onClick={save}>{saving ? "Saving…" : "Save as my revision"}</button>
          <button disabled={saving} onClick={() => setEditing(null)}>Cancel</button>
          {refused !== null ? (
            <span className="imeta err">Not saved: {refused}. Your text is still here.</span>
          ) : (
            <span className="imeta">Saved as a revision in your name; the earlier text stays on the record.</span>
          )}
        </div>
      </div>
    );
  }
  const moved = narrative ? lastEventSequence - narrative.sequenceNo : 0;
  return (
    <>
      {narrative ? (
        <>
          <Markdown text={narrative.body} className="narrative" />
          <div className="imeta">
            {authorWords(narrative.draftedBy)}
            {narrative.revision > 1 && narrative.author.kind === "HUMAN" ? ` · edited by ${narrative.author.name || "a person"}` : ""}
            {/* A redraft is the current revision's model, which may differ from the first draft's. */}
            {narrative.revision > 1 && narrative.author.kind !== "HUMAN" ? ` · redrafted by reckon${narrative.author.model ? ` (${narrative.author.model})` : ""}` : ""}
            {` · revision ${narrative.revision} · ${fmtDateTime(narrative.occurredAt)}`}
            {moved > 0 ? ` · the record has moved ${moved} event${moved === 1 ? "" : "s"} since` : ""}
          </div>
        </>
      ) : !drafting ? (
        <Empty>No narrative yet. Draft one from the record, or write your own; the Thread and the Story lens hold the record either way.</Empty>
      ) : null}
      {drafting ? <div className="imeta">Drafting from the record…</div> : null}
      {status?.state === "failed" ? <div className="imeta err">Drafting failed: {status.message}</div> : null}
      <div className="decide">
        <button disabled={drafting || !hasRecord} title={hasRecord ? "One model call over the record, on your key; recorded as the model's draft under your name" : "Nothing on the record to narrate yet"} onClick={() => send({ type: "narrative.draft" })}>{narrative ? "Draft again" : "Draft narrative"}</button>
        <button disabled={drafting} onClick={() => setEditing({ text: narrative?.body ?? "", submittedAt: null })}>{narrative ? "Edit" : "Write"}</button>
      </div>
    </>
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
  const todo = stillToDo(actions, comms, hs, state.labels, snap.dependencies || []);
  const cited = proceduresCited(snap.thread || []);
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
        {/* A context-reset marker alone is not an act; the session judges the same way (agent/session.go). */}
        <WhatHappened narrative={snap.investigation.narrative} lastEventSequence={snap.investigation.lastEventSequence} hasRecord={(snap.thread || []).some((e) => e.interpretationType !== "context-reset") || hs.length > 0 || actions.length > 0} />
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
                {t.ref ? <a onClick={() => openInspector({ kind: "ref", ref: t.ref! })}>{t.text}</a> : t.dependencyId ? <a onClick={() => openInspector({ kind: "dependency", dependencyId: t.dependencyId! })}>{t.text}</a> : <a onClick={() => goTo({ kind: "lens", lens: t.kind === "prediction" ? "reasoning" : "response" })}>{t.text}</a>}
                {t.expiresAt ? <Countdown expiresAt={t.expiresAt} className="inline" /> : null}
              </li>
            ))}
          </ul>
        ) : <Empty>nothing outstanding</Empty>}
      </section>

      <section className="lsection">
        <h3>Procedures cited</h3>
        {cited.length ? cited.map((f) => <div key={f.sopId}><a onClick={() => openInspector({ kind: "sop", sopId: f.sopId })}>{f.title}</a></div>) : <Empty>none cited yet</Empty>}
      </section>

      <div className="decide">
        <button onClick={() => send({ type: "copy", text: handoffNote(snap.investigation.title, verdict, actions, todo, hs, cited, snap.investigation.narrative?.body) })}>Copy handoff note</button>
        <button title="The live markdown snapshot — paste into a ticket unedited" onClick={() => send({ type: "export.open" })}>Export report</button>
        {concluded ? <button title="Ask reckon to generalize a reusable procedure from this case; you review it before it joins the library" disabled={state.sopDraft?.state === "drafting"} onClick={() => send({ type: "sop.draft" })}>Draft SOP from this case</button> : null}
      </div>
      {concluded ? <AfterConclusion /> : null}
    </div>
  );
}
