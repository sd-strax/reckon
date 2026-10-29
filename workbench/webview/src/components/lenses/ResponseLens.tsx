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
  const { state, aside } = useApp();
  const snap = state.snap;
  if (!snap) return null;
  const actions = snap.actions || [];
  const open = actions.filter((a) => a.pending && !a.expired);
  const waiting = open.filter((a) => !aside.has(a.actionId));
  const setAside = open.filter((a) => aside.has(a.actionId));
  const followed = proceduresFollowed(snap.thread || []);
  const engine = String(snap.investigation.state).toUpperCase();
  const draft = engine === "DRAFT";
  // Decisions come first (#125); the groups hold everything already decided.
  const decided = actions.filter((a) => !(a.pending && !a.expired));
  const groups = ORDER.map((g) => ({ g, rows: decided.filter((a) => groupOf(a) === g) })).filter((x) => x.rows.length);
  return (
    <div className="lens response">
      <div className="planhead">
        <h3>Response</h3>
        <span className="imeta">{actions.length} action{actions.length === 1 ? "" : "s"}{open.length ? ` · ${open.length} waiting on a decision` : ""}{followed.length ? ` · from procedures ${followed.map((f) => f.title).join(", ")}` : ""}</span>
      </div>
      {!actions.length ? <Empty>No actions requested yet. Ask reckon in the conversation; each request arrives here for you to approve.</Empty> : null}
      {waiting.length ? (
        <section className="lsection" id="obj-waiting">
          <h3>Waiting on you<span className="count">{waiting.length}</span></h3>
          {waiting.map((a) => <ActionCard key={a.actionId} a={a} variant="full" draft={draft} />)}
        </section>
      ) : null}
      {setAside.length ? (
        <section className="lsection" id="obj-aside">
          <h3>Set aside<span className="count">{setAside.length}</span></h3>
          <div className="imeta">Leaving a request undecided is a choice: it stays open until its approval window closes, then expires.</div>
          {setAside.map((a) => <ActionCard key={a.actionId} a={a} variant="full" draft={draft} />)}
        </section>
      ) : null}
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
