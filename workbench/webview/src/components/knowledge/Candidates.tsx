// Candidates (09 §9.7): procedures the agent drafted at an investigation's
// conclusion, reviewed here whenever whoever manages knowledge chooses; Add
// to library imports the edited draft with a reckon://investigation/<id>
// source URL, Discard takes a reason. The store is gap 00 §6.24, so the frame
// carries the gap line until it lands; until then a drafted procedure opens
// as an editor tab at conclusion (parity M6b).
import { Empty } from "../shared";

export function Candidates() {
  return (
    <section className="lsection kcands" aria-label="Candidates">
      <h3>Candidates <span className="imeta">—</span></h3>
      <Empty>Candidate procedures land with gap 00 §6.24 (a table of drafts with their status: new, accepted, discarded). Until then a procedure the agent drafts at conclusion opens as an editor tab with Add to SOP library / Discard. Nothing is added without an explicit accept.</Empty>
    </section>
  );
}
