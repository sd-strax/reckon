// Response — containment and handoff (08 §8.4): the plan header, the action
// cards in every engine state grouped by the catalog's category, and the comms
// threads. A notification is asked for in the conversation like any action
// (#126); there is no second composer here.
import { useApp } from "../../app";
import { proceduresFollowed } from "../../derive/summary";
import { ActionCard, GROUP_LABELS, groupOf } from "../ActionCard";
import { Comms } from "../Rail";
import { Empty } from "../shared";

const ORDER = ["containment", "eradication", "recovery", "notification", "case_record"] as const;

export function ResponseLens() {
  const { state } = useApp();
  const snap = state.snap;
  if (!snap) return null;
  const actions = snap.actions || [];
  const pending = actions.filter((a) => a.pending && !a.expired);
  const followed = proceduresFollowed(snap.thread || []);
  const engine = String(snap.investigation.state).toUpperCase();
  const draft = engine === "DRAFT";
  const groups = ORDER.map((g) => ({ g, rows: actions.filter((a) => groupOf(a) === g) })).filter((x) => x.rows.length);
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
      <section className="lsection" id="obj-notification-draft">
        <h3>Conversations with others</h3>
        <div className="imeta">To notify someone, ask reckon in the conversation — the message arrives here as a card you approve before it is sent.</div>
        <Comms />
      </section>
    </div>
  );
}
