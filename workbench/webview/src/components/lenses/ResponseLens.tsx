// Response — containment and handoff (08 §8.4): the plan header, the action
// cards in every engine state grouped Containment → Eradication → Recovery →
// Notification → External work, owner action items (gap §6.17: absent until
// the record carries them), the notification draft assembled from the record
// (the mandatory preview is the approval), and the comms threads.
import { useState } from "react";
import { useApp } from "../../app";
import { hypName } from "../../derive/hyps";
import { proceduresFollowed } from "../../derive/summary";
import { ActionCard, GROUP_LABELS, groupOf } from "../ActionCard";
import { Comms } from "../Rail";
import { Empty, Badge, fmtDateTime } from "../shared";
import type { ActionRow } from "../../../../src/backend";

const ORDER = ["containment", "eradication", "recovery", "notification", "external"] as const;

export function ResponseLens() {
  const { state, send, openPrompt, openInspector } = useApp();
  const snap = state.snap;
  const [lockedOn, setLockedOn] = useState<ActionRow[] | null>(null);
  if (!snap) return null;
  const actions = snap.actions || [];
  const locked = lockedOn !== null && lockedOn === snap.actions;
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
  const ownerItems = snap.ownerItems || [];
  // The ask of every OPEN item rides the draft (adr/0101): the items attach
  // to the notification once it is requested and move to SENT when it succeeds.
  const askItems = ownerItems.filter((i) => i.status === "OPEN");
  const needs = [
    ...outstanding.map((a) => `approval of ${a.actionType} on ${a.targets.join(", ")}`),
    ...askItems.map((i) => `${i.owner}: ${i.title}`),
  ];
  const notifyDraft = [
    "What happened: " + (verdict ? `${verdict.disposition.toLowerCase()}${supported.length ? " — " + supported.map((h) => h.statement).join(" ") : ""}` : "under investigation; no verdict of record yet"),
    "What we did: " + (done.length ? done.map((a) => `${a.actionType} on ${a.targets.join(", ")}`).join("; ") : "no containment has taken effect yet"),
    "What we need: " + (needs.length ? needs.join("; ") : "nothing outstanding on our side"),
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
      send({ type: "notify.request", channel, target, subject: typeof ex.subject === "string" ? ex.subject : undefined, body, ownerItemIds: askItems.length ? askItems.map((i) => i.id) : undefined });
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
          {rows.slice().reverse().map((a) => <ActionCard key={a.actionId} a={a} variant="full" locked={locked} onLock={() => setLockedOn(snap.actions ?? null)} draft={draft} />)}
        </section>
      ))}
      <section className="lsection" id="obj-owner-items">
        <h3>Owner action items<span className="count">{ownerItems.length}</span>{snap.ownerItems ? <button className="ghost" title="Work only an owner outside reckon can do, tracked to their confirmation" onClick={() => openPrompt({
          title: "New owner action item", label: "What must the owner do?", placeholder: "e.g. rotate the service account's credential",
          extrasTop: [{ kind: "text", id: "owner", label: "Owner (team, role or person)", placeholder: "e.g. Identity team" }],
          extras: [{ kind: "text", id: "detail", label: "Why (what evidence this rests on)", placeholder: "optional" }],
          confirm: "Add item",
          onConfirm: (title, _picked, ex) => { const owner = String(ex.owner || "").trim(); if (owner) send({ type: "owner.add", title, owner, detail: typeof ex.detail === "string" && ex.detail.trim() ? ex.detail.trim() : undefined }); },
        })}>+ Owner item</button> : null}</h3>
        {snap.ownerItems === null ? <Empty>Owner action items could not be read (the layer may be off).</Empty>
          : !ownerItems.length ? <Empty>No owner action items yet. Work only an owner outside reckon can do goes here; the agent proposes some, you add others.</Empty>
          : ownerItems.map((i) => (
            <div key={i.id} className={"card ownercard " + i.status.toLowerCase()} id={"obj-" + i.id}>
              <div className="ownerline">
                <Badge tone={i.status === "CONFIRMED" ? "ok" : i.status === "WAIVED" ? "muted" : i.status === "SENT" ? "info" : "warn"}>{i.status}</Badge>
                <a className="ownertitle" onClick={() => openInspector({ kind: "ownerItem", id: i.id })}>{i.title}</a>
                <span className="imeta">· {i.owner}{i.createdByKind === "AI_DELEGATED" ? " · proposed by reckon" : ""}{i.status === "OPEN" ? " · will be asked in the notification" : i.status === "SENT" && i.sentAt ? " · asked " + fmtDateTime(i.sentAt) : ""}</span>
              </div>
              {i.detail ? <div className="imeta">{i.detail}</div> : null}
              {i.status === "OPEN" || i.status === "SENT" ? (
                <div className="decide">
                  <button onClick={() => openPrompt({ title: "Mark confirmed", label: `How did ${i.owner} confirm “${i.title}”?`, placeholder: "e.g. confirmed by phone at 14:05", confirm: "Mark confirmed", onConfirm: (note) => send({ type: "owner.confirm", id: i.id, note }) })}>Mark confirmed…</button>
                  <button onClick={() => openPrompt({ title: "Waive owner item", label: "Why is this item waived without the owner's confirmation?", placeholder: "e.g. overtaken by the re-image", confirm: "Waive", onConfirm: (reason) => send({ type: "owner.waive", id: i.id, reason }) })}>Waive…</button>
                </div>
              ) : i.confirmationNote ? <div className="imeta">{i.status === "WAIVED" ? "waived" : "confirmed"}{i.confirmedByName || i.confirmedBy ? " by " + (i.confirmedByName || i.confirmedBy) : ""}: {i.confirmationNote}</div> : null}
            </div>
          ))}
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
