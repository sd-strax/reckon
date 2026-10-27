// The approval queue — an INTERRUPT, not a section: hidden when empty, pinned
// above everything when something needs the analyst. Each card is decision-
// grade (03 §3.3): what, which tool, the pre-send preview for notify.*, the
// lineage, the window — and Approve/Reject posting to the host, which does the
// real call on the human token.
import { useState } from "react";
import { useApp } from "../app";
import { pendingQueue } from "../derive/actions";
import { Badge, Countdown, Eyebrow, RefChip } from "./shared";
import type { ActionRow } from "../../../src/backend";

function ApprovalCard({ a, engine, locked, onLock }: { a: ActionRow; engine: string; locked: boolean; onLock: () => void }) {
  const { send, openPrompt, openInspector } = useApp();
  const p = (a.parameters || {}) as Record<string, unknown>;
  const isNotify = !!a.actionType && a.actionType.indexOf("notify.") === 0;
  const bodyText = typeof p.message === "string" && p.message ? p.message : typeof p.body === "string" ? p.body : "";
  return (
    <div className="card" style={a.expired ? { opacity: 0.7 } : undefined}>
      <div>
        <a className="statement" title="Open the action record" onClick={() => openInspector({ kind: "action", actionId: a.actionId })}>{a.actionType}</a>
        <Badge tone={a.tier}>{a.tier}</Badge>
        <Badge tone={a.expired ? "warn" : ""}>{a.expired ? "EXPIRED" : a.pendingLabel}</Badge>
      </div>
      <div className="cardmeta">→ {(a.targets || []).join(", ")}</div>
      {/* Pre-approval provenance (08 §4): which tool WILL execute this if approved now. */}
      {a.plannedAdapter ? <div className="cardmeta via">⇒ will dispatch via {a.plannedAdapter}</div> : null}
      {/* The mandatory pre-send preview (binding §4): approving a notify.* action IS sending it. */}
      {isNotify && a.parameters ? (
        <>
          <div className="sendpreview">
            {typeof p.subject === "string" && p.subject ? <div className="subj">{p.subject}</div> : null}
            {bodyText}
          </div>
          {typeof p.thread_ref === "string" && p.thread_ref ? <div className="cardmeta">↻ follow-up on an existing thread</div> : null}
        </>
      ) : null}
      {a.retryOf ? <div className="cardmeta">↻ retry of {a.retryOf.slice(0, 8)}… (the original can never re-dispatch)</div> : null}
      {a.tierEscalated ? <div className="cardmeta">⚠ escalated to {a.tier}: {(a.targets || []).length} targets (blast radius)</div> : null}
      {a.reversibility ? (
        <div className="cardmeta">
          {a.reversibility === "REVERSIBLE" ? "↩ reversible"
            : a.reversibility === "BEST_EFFORT" ? "↩? best-effort reversal — cannot be verified undone; treat as permanent"
            : "⚠ irreversible"}
        </div>
      ) : null}
      {(a.evidenceRefs || []).length ? <div className="cardmeta">evidence: {a.evidenceRefs.map((r) => <RefChip key={r} refId={r} />)}</div> : null}
      {!a.expired && a.expiresAt ? <Countdown expiresAt={a.expiresAt} className="cardmeta" /> : null}
      {a.expired ? (
        <>
          <div className="cardmeta">approval window elapsed — the world may have moved on</div>
          <div className="decide">
            <button className="primary" disabled={locked} title="Create a fresh request (same targets + evidence, new approval window)" onClick={() => openPrompt({
              title: "Re-request " + a.actionType,
              label: "Why is this still warranted?",
              placeholder: "e.g. WIN-9 still shows the beaconing — containment still needed",
              helper: "Creates a NEW action (same targets & evidence, fresh approval window) you then approve. The original stays expired, linked as lineage.",
              confirm: "↻ Re-request",
              onConfirm: (rationale) => send({ type: "action.rerequest", actionId: a.actionId, actionType: a.actionType, rationale }),
            })}>↻ Re-request</button>
          </div>
        </>
      ) : (
        <>
          {/* Invisible activate (01 §Extension 2): the first action on a DRAFT is the activation moment. */}
          {engine === "DRAFT" ? <div className="cardmeta actnote">First action on the world — approving activates the investigation.</div> : null}
          <div className="decide">
            <button className="primary" disabled={locked} onClick={() => {
              if (a.tier === "T3") {
                // T3 (04 §5.5): the typed challenge, in the prompt card.
                openPrompt({
                  title: "Approve " + a.actionType + " — Tier 3",
                  label: "Type the challenge to confirm",
                  helper: "High blast radius (" + (a.targets || []).length + " targets). This action is "
                    + (a.reversibility === "REVERSIBLE" ? "reversible." : a.reversibility === "BEST_EFFORT" ? "best-effort reversal — treat as permanent." : "irreversible."),
                  confirm: "Approve",
                  onConfirm: (challenge) => send({ type: "action.approve", actionId: a.actionId, tier: a.tier, actionType: a.actionType, challenge }),
                });
              } else {
                onLock();
                send({ type: "action.approve", actionId: a.actionId, tier: a.tier, actionType: a.actionType });
              }
            }}>{a.tier === "T3" ? "Approve (challenge)…" : isNotify ? "Approve · Send" : "Approve"}</button>
            <button disabled={locked} onClick={() => openPrompt({
              title: "Reject " + a.actionType,
              label: "Why is this rejected?",
              placeholder: "recorded on the audit trail",
              confirm: "Reject",
              require: false,
              onConfirm: (reason) => send({ type: "action.reject", actionId: a.actionId, actionType: a.actionType, reason }),
            })}>Reject…</button>
          </div>
        </>
      )}
    </div>
  );
}

export function PendingSection() {
  const { state } = useApp();
  const actions = state.snap?.actions || [];
  const engine = state.snap ? String(state.snap.investigation.state).toUpperCase() : "";
  // One decision in flight at a time: the lock is taken on the actions slice's
  // identity, and the host re-posts the slice after every outcome (including a
  // failed approve), which replaces the array and lifts the lock.
  const [lockedOn, setLockedOn] = useState<ActionRow[] | null>(null);
  const locked = lockedOn !== null && lockedOn === state.snap?.actions;
  const pend = pendingQueue(actions);
  if (!pend.length) return null;
  return (
    <section id="pendingSection">
      <Eyebrow>Needs your approval</Eyebrow>
      <div id="pending">
        {pend.map((a) => (
          <ApprovalCard key={a.actionId} a={a} engine={engine} locked={locked} onLock={() => setLockedOn(state.snap?.actions ?? null)} />
        ))}
      </div>
    </section>
  );
}
