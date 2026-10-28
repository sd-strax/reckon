// Comms threads (Phase F, ui/04): conversation state that FOLLOWED an approved
// notify.* action — nothing here sends anything; Follow up requests a new
// action. Rendered under Notification in the Response lens.
import { useApp } from "../app";
import { badgeTone } from "../derive/hyps";
import { Badge, fmtDateTime } from "./shared";

export function Comms() {
  const { state, send, openPrompt, openInspector } = useApp();
  const threads = state.snap?.comms || [];
  if (!threads.length) return null;
  const open = threads.filter((t) => t.status === "awaiting_reply" || t.status === "followed_up").length;
  return (
    <div id="commsSection">
      <div className="kicker">External work · {open} open</div>
      <div id="commsBox">
        {threads.map((t) => {
          const last = (t.trail || [])[t.trail.length - 1];
          const openThread = t.status === "awaiting_reply" || t.status === "followed_up" || t.status === "replied";
          const bits = ["sent " + fmtDateTime(t.sentAt)];
          if (t.followUpHours) bits.push("follow-up " + t.followUpHours + "h · " + t.followUps + " sent");
          if (t.followUpDue) bits.push("⏰ follow-up due");
          return (
            <div key={t.threadId} className={"card commscard" + (t.status === "replied" ? " replied" : "")} style={t.status === "closed" ? { opacity: 0.7 } : undefined} id={"obj-" + t.threadId}>
              <div>
                <a className="statement" title="Open the thread" onClick={() => openInspector({ kind: "thread", threadId: t.threadId })}>{(t.actionType === "notify.email" ? "✉ " : "# ") + t.target}</a>
                <Badge tone={badgeTone(t.status)}>{t.status.replace("_", " ")}</Badge>
              </div>
              {t.subject ? <div className="cardmeta">{t.subject}</div> : null}
              {last ? <div className={"quote" + (last.direction === "inbound" ? " inbound" : "")}>{(last.direction === "inbound" ? "↩ " : "") + last.author + ": " + last.body}</div> : null}
              <div className="cardmeta">{bits.join(" · ")}</div>
              {t.escalationTriggered ? <div className="esc">⚠ Escalation policy triggered ({t.escalationPolicy || ""}) — repeated follow-ups, no resolution. Consider escalating; nothing auto-fires.</div> : null}
              {(t.unackedReply || openThread) ? (
                <div className="decide">
                  {t.unackedReply ? <button className="primary" title="Record that you saw the reply (kept on the trail)" onClick={() => send({ type: "comms.act", threadId: t.threadId, verb: "ack" })}>Acknowledge</button> : null}
                  {openThread ? (
                    <>
                      <button className={t.followUpDue && !t.unackedReply ? "primary" : undefined} title="Drafts a follow-up message — it becomes a new action you approve before anything is sent" onClick={() => openPrompt({
                        title: "Follow up — " + t.target, label: "The follow-up message (you approve before it sends)", placeholder: "Any update on this? Still needed for the investigation.",
                        helper: "Requests a new " + t.actionType + " action carrying this thread's lineage; it lands in the approval queue as a preview.", confirm: "Request follow-up",
                        onConfirm: (message) => send({ type: "comms.followup", threadId: t.threadId, actionType: t.actionType, target: t.target, subject: t.subject || "", message, followUpHours: t.followUpHours }),
                      })}>Follow up…</button>
                      {t.followUpDue ? <button onClick={() => send({ type: "comms.snooze", threadId: t.threadId, hours: 24 })}>Snooze 24h</button> : null}
                      <button title="The external work is resolved — closes the thread" onClick={() => send({ type: "comms.act", threadId: t.threadId, verb: "done" })}>Mark done</button>
                    </>
                  ) : null}
                </div>
              ) : null}
            </div>
          );
        })}
      </div>
    </div>
  );
}
