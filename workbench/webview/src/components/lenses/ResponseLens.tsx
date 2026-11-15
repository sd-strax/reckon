// Response — containment and handoff (08 §8.4; adr/0361): the plan header,
// then the pending actions under "Needs your decision" (the window that closes
// soonest first), then every other action on one timeline in the order it was
// requested, then the comms threads. The phase is the group tile on each item,
// not a section. A notification is asked for in the conversation like any
// action (#126); there is no second composer here.
import { useApp } from "../../app";
import { proceduresCited } from "../../derive/summary";
import { awaitsDecision, byDeadline, byRequested } from "../../derive/actions";
import { ActionCard, TimelineItem } from "../ActionCard";
import { Comms } from "../Rail";
import { Dependencies } from "../Dependencies";
import { Empty } from "../shared";
import type { ActionRow } from "../../../../src/viewTypes";

/** The day an ISO time falls on, in local time; empty when there is none. */
const dayOf = (iso: string | undefined): string => {
  const t = iso ? Date.parse(iso) : NaN;
  return Number.isNaN(t) ? "" : new Date(t).toDateString();
};

/** A timeline item carries its date when its day differs from the item above
 * it in the order shown, or, for the first item, when its day is not today:
 * the time is the ordering key and must not be ambiguous (adr/0361). */
function showDate(prev: ActionRow | undefined, a: ActionRow): boolean {
  const day = dayOf(a.requestedAt);
  if (!day) return false;
  return prev ? day !== dayOf(prev.requestedAt) : day !== new Date().toDateString();
}

export function ResponseLens() {
  const { state, responseOrder, setResponseOrder } = useApp();
  const snap = state.snap;
  if (!snap) return null;
  const actions = snap.actions || [];
  const open = byDeadline(actions.filter(awaitsDecision));
  const cited = proceduresCited(snap.thread || []);
  const engine = String(snap.investigation.state).toUpperCase();
  const draft = engine === "DRAFT";
  const timeline = byRequested(actions.filter((a) => !awaitsDecision(a)), responseOrder);
  return (
    <div className="lens response">
      <div className="planhead">
        <h3>Response</h3>
        <span className="imeta">{actions.length} action{actions.length === 1 ? "" : "s"}{open.length ? ` · ${open.length} waiting on a decision` : ""}{cited.length ? ` · procedures cited: ${cited.map((f) => f.title).join(", ")}` : ""}</span>
      </div>
      {!actions.length ? <Empty>No actions requested yet. Ask reckon in the conversation; each request arrives here for you to approve.</Empty> : null}
      {open.length ? (
        <section className="lsection" id="obj-waiting">
          <h3>Needs your decision<span className="count">{open.length}</span></h3>
          {open.map((a) => <ActionCard key={a.actionId} a={a} variant="full" draft={draft} />)}
        </section>
      ) : null}
      {timeline.length ? (
        <section className="lsection" id="obj-timeline">
          <div className="lsechead">
            <h3>Timeline<span className="count">{timeline.length}</span></h3>
            <div className="modetoggle" role="group" aria-label="Order">
              <button className={responseOrder === "oldest" ? "on" : ""} aria-pressed={responseOrder === "oldest"} onClick={() => setResponseOrder("oldest")}>Oldest first</button>
              <button className={responseOrder === "newest" ? "on" : ""} aria-pressed={responseOrder === "newest"} onClick={() => setResponseOrder("newest")}>Newest first</button>
            </div>
          </div>
          <ol className="tl">
            {timeline.map((a, i) => <TimelineItem key={a.actionId} a={a} showDate={showDate(timeline[i - 1], a)} />)}
          </ol>
        </section>
      ) : null}
      <section className="lsection" id="obj-notification-draft">
        <h3>Conversations with others</h3>
        <div className="imeta">To notify someone, ask reckon in the conversation — the message arrives here as a card you approve before it is sent.</div>
        <Comms />
      </section>
      <Dependencies />
    </div>
  );
}
