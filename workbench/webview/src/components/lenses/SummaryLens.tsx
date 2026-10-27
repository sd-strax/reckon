// Summary — where it stands (08 §8.4): four tiles, the narrative (adr/0103:
// drafted on the client plane or written by the analyst, with its authorship;
// never templated), key entities with derivable status words, still to do,
// procedures followed, the handoff note and the export. Every element names
// its source or says it is a gap.
import { useApp } from "../../app";
import { terminal } from "../../derive/stage";
import { hypName } from "../../derive/hyps";
import { handoffNote, keyEntities, proceduresFollowed, stillToDo } from "../../derive/summary";
import { Badge, Countdown, Empty, Markdown, RefChip, fmtDateTime } from "../shared";

/** The narrative with its authorship line and the two acts (adr/0103). */
function Narrative() {
  const { state, send, openPrompt } = useApp();
  const snap = state.snap!;
  const s = snap.summary;
  const closed = ["CONCLUDED", "ARCHIVED"].includes(String(snap.investigation.state).toUpperCase());
  const n = s?.narrative ?? null;
  const a = s?.authorship;
  const stale = !!(a && n && !a.latestIsCurrent);
  return (
    <>
      {state.narrativeDrafting ? <div className="imeta">drafting from the record…</div> : null}
      {state.narrativeError ? <div className="banner error" role="alert">The draft failed: {state.narrativeError}</div> : null}
      {s === null ? <Empty>The summary could not be read.</Empty>
        : !n ? <Empty>No narrative has been drafted for this investigation yet. Draft one from the record, or write it; the thread and the Story lens hold the record either way.</Empty>
        : (
          <>
            <Markdown text={n.body} className="narrative" />
            <div className="imeta authorship">
              {a?.draftedBy === "reckon" ? <>drafted by reckon{a.draftedModel ? ` (${a.draftedModel})` : ""}</> : <>written by {a?.draftedBy || n.authorName || n.authorId}</>}
              {a?.editedBy ? <> · edited by {a.editedBy}</> : null}
              {" · "}revision {n.revision} · {fmtDateTime(n.createdAt)}
              {stale ? <> · <span className="warn">the record has moved on since (as of sequence {n.asOfSequence}, now {snap.seq})</span></> : null}
            </div>
          </>
        )}
      {!closed ? (
        <div className="decide">
          <button className={n ? undefined : "primary"} disabled={state.narrativeDrafting} title="One model completion over the record, on your key; recorded as a draft you can edit" onClick={() => send({ type: "narrative.draft" })}>{n ? "Redraft" : "Draft"}</button>
          <button onClick={() => openPrompt({ title: n ? "Edit the narrative" : "Write the narrative", label: "What happened, as it stands — your words become a revision above the draft", value: n?.body || "", confirm: "Save revision", onConfirm: (body) => send({ type: "narrative.save", body }) })}>{n ? "Edit" : "Write"}</button>
        </div>
      ) : null}
    </>
  );
}

export function SummaryLens() {
  const { state, send, openInspector, goTo } = useApp();
  const snap = state.snap;
  if (!snap) return null;
  const hs = snap.hypotheses;
  const actions = snap.actions || [];
  const comms = snap.comms || [];
  const verdict = snap.investigation.verdict;
  const contain = actions.filter((a) => !a.actionType.startsWith("notify."));
  const approved = contain.filter((a) => a.status !== "REQUESTED" && a.status !== "PENDING_SECONDARY" && a.status !== "REJECTED" && a.status !== "EXPIRED").length;
  const nearest = actions.filter((a) => a.pending && !a.expired && a.expiresAt).map((a) => a.expiresAt!).sort()[0];
  const notify = actions.filter((a) => a.actionType.startsWith("notify."));
  const awaiting = comms.filter((t) => t.status === "awaiting_reply" || t.status === "followed_up").length;
  const open = hs.flatMap((h) => h.status === "ABANDONED" ? [] : h.predictions.filter((p) => p.status === "UNTESTED" || p.status === "INCONCLUSIVE").map((p) => ({ h, p })));
  const ents = keyEntities(snap.investigation.seed, snap.thread || [], snap.pins, actions, state.labels);
  const todo = stillToDo(actions, comms, hs, state.labels, snap.ownerItems);
  const followed = proceduresFollowed(snap.thread || []);
  const supported = hs.filter((h) => h.status === "SUPPORTED").map((h) => hypName(h, hs));
  const refuted = hs.filter((h) => h.status === "REFUTED").map((h) => hypName(h, hs));
  return (
    <div className="lens summary">
      <div className="tiles">
        <div className="tile" onClick={() => goTo({ kind: "lens", lens: "reasoning" })}>
          <div className="kicker">Verdict</div>
          <div className="tilemain">{verdict ? verdict.disposition.toLowerCase() : "none yet"}</div>
          <div className="imeta">{verdict ? [supported.length ? supported.join(", ") + " supported" : "", refuted.length ? refuted.join(", ") + " refuted" : ""].filter(Boolean).join(", ") || "recorded" : "record one from the Reasoning lens"}</div>
        </div>
        <div className="tile" onClick={() => goTo({ kind: "lens", lens: "response" })}>
          <div className="kicker">Containment</div>
          <div className="tilemain">{contain.length ? `${approved} of ${contain.length} approved` : "no actions"}</div>
          <div className="imeta">{nearest ? <>first window closes in <Countdown expiresAt={nearest} className="inline" /></> : contain.length && contain.every(terminal) ? "every action terminal" : "—"}</div>
        </div>
        <div className="tile" onClick={() => goTo({ kind: "lens", lens: "response", anchor: "notification" })}>
          <div className="kicker">Notification</div>
          <div className="tilemain">{notify.some((a) => a.status === "SUCCEEDED") ? "notified" : notify.some((a) => a.pending) ? "awaiting approval" : "not yet notified"}</div>
          <div className="imeta">{awaiting ? `${awaiting} reply outstanding` : notify.length ? `${notify.length} sent or requested` : "draft from the record"}</div>
        </div>
        <div className="tile" onClick={() => goTo({ kind: "lens", lens: "reasoning" })}>
          <div className="kicker">Open questions</div>
          <div className="tilemain">{open.length}</div>
          <div className="imeta">{open.slice(0, 2).map(({ h, p }) => `P${p.ordinal ?? "?"} (${hypName(h, hs)})`).join(", ") || "none"}</div>
        </div>
      </div>

      <section className="lsection" id="obj-narrative">
        <h3>What happened</h3>
        <Narrative />
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
        <button title="The facts, the narrative and what remains, rendered by the server so the copy and the export agree" onClick={() => send(snap.summary ? { type: "handoff.copy" } : { type: "copy", text: handoffNote(snap.investigation.title, verdict, actions, todo, hs, followed) })}>Copy handoff note</button>
        <button title="The live markdown snapshot — paste into a ticket unedited" onClick={() => send({ type: "export.open" })}>Export report</button>
      </div>
    </div>
  );
}
