// The inspector dock and the frame every type shares (08 §8.6, impl §2.4):
// type label · pin · close, back + breadcrumb when reached from another
// object, a human name, "In this case", then the typed tabs. At most two
// panes: the pinned one and the active one. Esc closes the active one.
import { useEffect } from "react";
import { useApp, type InspectorPane as Pane } from "../../app";
import { rendererFor, rendererLabel, targetKey, targetName } from "../../derive/inspect";
import type { InspectorTarget } from "../../../../src/protocol";
import { InThisCaseBlock, InspectorError } from "./primitives";
import { AbsenceInspector, AliasInspector, DetectionInspector, EvidenceInspector } from "./EvidenceInspector";
import { EntityInspector } from "./EntityInspector";
import { AttestationInspector, HypothesisInspector, PredictionInspector } from "./ReasoningInspectors";
import { ActionInspector, ThreadInspector } from "./ActionInspectors";
import { CaseInspector, PersonInspector, ProcedureInspector, TurnInspector } from "./ProvenanceInspectors";

const RECORD_ONLY = new Set(["evidence", "absence", "detection", "alias"]);
const SNAPSHOT_ONLY = new Set(["prediction", "hypothesis", "thread", "person"]);

function Body({ target }: { target: InspectorTarget }) {
  const { state } = useApp();
  const doc = target.kind === "ref" ? state.docs[target.ref] : undefined;
  const r = rendererFor(target, doc);
  switch (r) {
    case "evidence": return <EvidenceInspector target={target as { kind: "ref"; ref: string }} />;
    case "entity": return <EntityInspector refId={(target as { ref: string }).ref} />;
    case "absence": return <AbsenceInspector refId={(target as { ref: string }).ref} />;
    case "detection": return <DetectionInspector refId={(target as { ref: string }).ref} />;
    case "alias": return <AliasInspector refId={(target as { ref: string }).ref} />;
    case "attestation": return <AttestationInspector refId={(target as { ref: string }).ref} />;
    case "prediction": return <PredictionInspector refId={(target as { ref: string }).ref} />;
    case "hypothesis": return <HypothesisInspector refId={(target as { ref: string }).ref} />;
    case "action": return <ActionInspector actionId={(target as { actionId: string }).actionId} />;
    case "thread": return <ThreadInspector threadId={(target as { threadId: string }).threadId} />;
    case "case": return <CaseInspector investigationRef={(target as { investigationRef: string }).investigationRef} />;
    case "turn": return <TurnInspector interpretationId={(target as { interpretationId: string }).interpretationId} />;
    case "person": return <PersonInspector principalId={(target as { principalId: string }).principalId} />;
    case "procedure": return <ProcedureInspector sopId={(target as { sopId: string }).sopId} />;
  }
}

function PaneView({ pane, which }: { pane: Pane; which: "active" | "pinned" }) {
  const { state, inspectorBack, closeInspector, pinInspector } = useApp();
  const target = pane.stack[pane.stack.length - 1];
  const doc = target.kind === "ref" ? state.docs[target.ref] : undefined;
  const renderer = rendererFor(target, doc);
  // The fetched record names the pane when the snapshot cannot (the Knowledge document has no snapshot).
  const fetched = target.kind === "sop" ? state.sops[target.sopId]?.title : target.kind === "case" ? state.caseSummaries[target.investigationRef]?.title : undefined;
  const name = fetched || targetName(target, state.snap, state.labels);
  const key = targetKey(target);
  const errKey = "ref" in target ? target.ref : "sopId" in target ? target.sopId : "interpretationId" in target ? target.interpretationId
    : "actionId" in target ? target.actionId : "investigationRef" in target ? target.investigationRef : "";
  const error = state.inspectorErrors[errKey];
  const crumbs = pane.stack.slice(0, -1);
  return (
    <section className={"inspector " + which} data-key={key} aria-label={rendererLabel(renderer)}>
      <header className="ihead">
        <span className="kicker">{rendererLabel(renderer)}</span>
        <span className="ihead-actions">
          {which === "active" ? <button className="iconbtn" title="Pin this pane — the next citation opens beside it" onClick={pinInspector}>📌</button> : null}
          <button className="iconbtn" title="Close (Esc)" onClick={() => closeInspector(which)}>✕</button>
        </span>
      </header>
      {crumbs.length && which === "active" ? (
        <div className="icrumbs">
          <button className="linklike" onClick={inspectorBack}>‹ back</button>
          {crumbs.map((c, i) => <span key={i} className="icrumb">{targetName(c, state.snap, state.labels)}</span>)}
        </div>
      ) : null}
      <h2 className="iname" title={"ref" in target ? target.ref : undefined}>{name}</h2>
      {"ref" in target ? <InThisCaseBlock refId={target.ref} /> : null}
      <div className="ibody">
        {/* A fetch error hides only what depended on the fetch: the snapshot-
            backed renderers (prediction, hypothesis, thread, person) still
            render, and the partial ones (entity, turn, action, procedure,
            case) show the error above what the snapshot holds. */}
        {error && RECORD_ONLY.has(renderer) ? <InspectorError message={error} /> : (
          <>
            {error && !SNAPSHOT_ONLY.has(renderer) ? <InspectorError message={error} /> : null}
            <Body target={target} />
          </>
        )}
      </div>
    </section>
  );
}

export function InspectorDock() {
  const { inspector, closeInspector } = useApp();
  useEffect(() => {
    if (!inspector.active) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !(e.target as HTMLElement | null)?.closest?.(".dialog")) closeInspector("active");
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [inspector.active, closeInspector]);
  if (!inspector.active && !inspector.pinned) return null;
  return (
    <div id="inspectorDock">
      {inspector.pinned ? <PaneView pane={inspector.pinned} which="pinned" /> : null}
      {inspector.active ? <PaneView pane={inspector.active} which="active" /> : null}
    </div>
  );
}
