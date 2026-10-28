// Response — containment and handoff (08 §8.4): the plan header, the action
// cards in every engine state grouped Containment → Eradication → Recovery →
// Notification → External work, owner action items (gap §6.17: absent until
// the record carries them), the notification draft assembled from the record
// (the mandatory preview is the approval), and the comms threads.
import { useApp } from "../../app";
import { hypName } from "../../derive/hyps";
import { proceduresFollowed } from "../../derive/summary";
import { ActionCard, GROUP_LABELS, groupOf } from "../ActionCard";
import { Comms } from "../Rail";
import { Empty } from "../shared";

const ORDER = ["containment", "eradication", "recovery", "notification", "external"] as const;

export function ResponseLens() {
  const { state, send, openPrompt } = useApp();
  const snap = state.snap;
  if (!snap) return null;
  const actions = snap.actions || [];
  const hs = snap.hypotheses;
  const pending = actions.filter((a) => a.pending && !a.expired);
  const followed = proceduresFollowed(snap.thread || []);
  const engine = String(snap.investigation.state).toUpperCase();
  const draft = engine === "DRAFT";
  const groups = ORDER.map((g) => ({ g, rows: actions.filter((a) => groupOf(a) === g) })).filter((x) => x.rows.length);
  const verdict = snap.investigation.verdict;
  const supported = hs.filter((h) => h.status === "SUPPORTED");
  const done = actions.filter((a) => a.status === "SUCCEEDED" && !a.actionType.startsWith("notify."));
  const outstanding = actions.filter((a) => a.pending);
  const notifyDraft = [
    "What happened: " + (verdict ? `${verdict.disposition.toLowerCase()}${supported.length ? " — " + supported.map((h) => h.statement).join(" ") : ""}` : "under investigation; no verdict of record yet"),
    "What we did: " + (done.length ? done.map((a) => `${a.actionType} on ${a.targets.join(", ")}`).join("; ") : "no containment has taken effect yet"),
    "What we need: " + (outstanding.length ? outstanding.map((a) => `approval of ${a.actionType} on ${a.targets.join(", ")}`).join("; ") : "nothing outstanding on our side"),
  ].join("\n\n");
  const sendNotification = (channel: "notify.email" | "notify.slack") => openPrompt({
    title: "Preview & send — " + (channel === "notify.email" ? "email" : "channel message"),
    label: "The exact message that will be sent (approving the action is sending it)",
    value: notifyDraft,
    extrasTop: [
      { kind: "text", id: "target", label: channel === "notify.email" ? "To (address)" : "To (channel)", placeholder: channel === "notify.email" ? "security-contact@example.test" : "#incident-response" },
      ...(channel === "notify.email" ? [{ kind: "text" as const, id: "subject", label: "Subject", value: "Investigation update: " + snap.investigation.title }] : []),
    ],
    helper: "Requests a " + channel + " action through the same approval path as any action; the approval card shows this text verbatim before it goes out.",
    confirm: "Request to send",
    onConfirm: (body, _picked, ex) => {
      const target = String(ex.target || "").trim();
      if (!target) return;
      send({ type: "notify.request", channel, target, subject: typeof ex.subject === "string" ? ex.subject : undefined, body });
    },
  });
  return (
    <div className="lens response">
      <div className="planhead">
        <h3>Containment plan</h3>
        <span className="imeta">{actions.length} action{actions.length === 1 ? "" : "s"}{pending.length ? ` · ${pending.length} awaiting your decision` : ""}{followed.length ? ` · from procedures ${followed.map((f) => f.title).join(", ")}` : ""}</span>
      </div>
      {!actions.length ? <Empty>No actions proposed yet. The agent proposes them from the evidence; you approve each one.</Empty> : null}
      {groups.map(({ g, rows }) => (
        <section key={g} className="lsection" id={g === "notification" ? "obj-notification" : undefined}>
          <h3>{GROUP_LABELS[g]}<span className="count">{rows.length}</span></h3>
          {rows.slice().reverse().map((a) => <ActionCard key={a.actionId} a={a} variant="full" draft={draft} />)}
        </section>
      ))}
      <section className="lsection">
        <h3>Owner action items</h3>
        <Empty>Work only an owner outside reckon can do is tracked here once the record carries owner action items (gap 00 §6.17).</Empty>
      </section>
      <section className="lsection" id="obj-notification-draft">
        <h3>Notification</h3>
        <div className="card draftcard">
          <div className="kicker">Stakeholder notification · assembled from the record</div>
          <pre className="draftbody">{notifyDraft}</pre>
          <div className="imeta">{hs.length ? `basis: ${hs.filter((h) => h.status !== "ABANDONED").map((h) => `${hypName(h, hs)} ${h.status.toLowerCase()}`).join(", ")}` : "no hypotheses yet"}</div>
          <div className="decide">
            <button className="primary" disabled={engine === "CONCLUDED" || engine === "ARCHIVED"} onClick={() => sendNotification("notify.email")}>Preview & send email…</button>
            <button disabled={engine === "CONCLUDED" || engine === "ARCHIVED"} onClick={() => sendNotification("notify.slack")}>Preview & send to a channel…</button>
          </div>
        </div>
        <Comms />
      </section>
    </div>
  );
}
