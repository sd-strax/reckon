// The seed chip: the investigation's root, at the document head (01 §Extension
// 1), with the single-use "Note the case" action on a case seed (adr/0032).
import { useApp } from "../app";
import { caseNoteStatus } from "../derive/actions";
import { seedTypeLabel } from "../refs";

export function SeedChip() {
  const { state, send, openPrompt } = useApp();
  const snap = state.snap;
  const seed = snap?.investigation.seed;
  if (!snap || !seed || !seed.type) return null;
  const caseNote = snap.investigation.caseNote || null;
  let body: React.ReactNode = null;
  let control: React.ReactNode = null;
  if (seed.type === "entity") {
    body = <>Investigating {seedTypeLabel(seed.entityRef)} <b>{seed.entityIdentifier || seed.entityRef || ""}</b></>;
  } else if (seed.type === "question") {
    body = <>Hunting — <b>{seed.hypothesisStatement || ""}</b></>;
  } else if (seed.type === "alert") {
    body = <>From alert <b>{seed.source ? seed.source + ": " : ""}{seed.alertId || ""}</b></>;
  } else if (seed.type === "case") {
    body = <>From case <b>{seed.source ? seed.source + ": " : ""}{seed.caseId || ""}</b></>;
    // Once a comment action on the seed case exists — from this button, the
    // agent, or the analyst by hand — the line shows its status instead, read
    // from the investigation's own action record.
    const word = caseNoteStatus(seed, snap.actions || []);
    if (word) {
      control = <span className="seedNote" title="A comment on this case is on the action record">noted · {word}</span>;
    } else if (caseNote) {
      control = (
        <button
          className="linklike seedNoteBtn"
          title="Post a comment on the case that an investigation was opened here — a ticket.comment action, approved like any other"
          onClick={() => openPrompt({
            title: "Add note to " + (caseNote.systemName || "the case system"),
            label: "Comment to post on " + caseNote.caseId + " — requested as a ticket.comment action in this investigation",
            helper: "Cancel posts nothing. The request goes through approval like any action.",
            value: caseNote.body,
            require: false,
            confirm: "Post note",
            onConfirm: (v) => send({ type: "caseNote.post", body: v }),
          })}
        >Note the case</button>
      );
    }
  }
  if (!body) return null;
  return <div className="seedchip" id="seedChip"><span>{body}</span>{control}</div>;
}
