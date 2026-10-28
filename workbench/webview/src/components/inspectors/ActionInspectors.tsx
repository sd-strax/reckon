// Action (x-action: History · Authorization · Targets, from GET /api/actions/{id})
// and Notification thread (a notify.* action + its comms thread: Thread ·
// Sent message) — 08 §8.6.1. The card is the summary; this is the record.
import { useApp } from "../../app";
import { actionStatusBadge } from "../../derive/actions";
import { Badge, RefChip, fmtDateTime, fmtTime } from "../shared";
import { Json, KV, Loading, Section, Tabs } from "./primitives";

const ESCALATION_THRESHOLD = 10; // 04 §1: T2→T3 above this many distinct targets

export function ActionInspector({ actionId }: { actionId: string }) {
  const { state, dispatch, openInspector, openPrompt, send } = useApp();
  const row = state.snap?.actions?.find((a) => a.actionId === actionId);
  const rec = state.actionRecords[actionId];
  const a = rec?.action || row;
  if (!a) return rec === undefined ? <Loading what="action record" /> : <div className="empty">This action is not in the snapshot.</div>;
  const sb = actionStatusBadge(a);
  const auth = rec?.authorization;
  const name = (p?: { principal: string; principalName?: string }) => p ? (p.principalName || p.principal) : "";
  const distinct = new Set(a.targets).size;
  const reversible = a.reversibility === "REVERSIBLE" && a.status === "SUCCEEDED" && !rec?.links.reversedByRef;
  return (
    <>
      <div className="isub"><Badge tone={sb.tone}>{sb.label}</Badge> <span className="mono">{a.actionType}</span> · <Badge tone={a.tier}>{a.tier}</Badge> {a.reversibility ? a.reversibility.toLowerCase().replace("_", "-") : ""} · {a.targets.length} target{a.targets.length === 1 ? "" : "s"}</div>
      <Tabs tabs={[
        { id: "history", label: "History", body: () => (
          rec ? (
            <ol className="ihistory">
              {rec.history.map((h) => (
                <li key={h.sequenceNo}>
                  <span className="imeta">{fmtTime(h.occurredAt)}</span>{" "}
                  {h.status ? <b>{h.status}</b> : <b>{h.eventType.replace("action.", "").replace("_", " ")}</b>}
                  {h.detail ? <span> · {h.detail}</span> : null}
                  <span className="imeta"> · {h.actor.kind === "AI_DELEGATED" ? "reckon on behalf of " : h.actor.kind === "SYSTEM" ? "" : ""}<a onClick={() => openInspector({ kind: "person", principalId: h.actor.principal })}>{h.actor.principalName || h.actor.principal}</a></span>
                </li>
              ))}
            </ol>
          ) : <Loading what="history" />
        ) },
        { id: "authorization", label: "Authorization", body: () => (
          <>
            <KV rows={[
              { k: "Tier", v: `${a.tier} · ${distinct} distinct target${distinct === 1 ? "" : "s"}, escalation threshold ${ESCALATION_THRESHOLD} · ${a.tierEscalated ? "escalated (blast radius)" : "not escalated"}` },
              { k: "Required", v: auth?.requiredMode || a.requiredMode, mono: true },
              { k: "Decision", v: auth ? (auth.matchedPolicyRef ? `policy ${auth.matchedPolicyRef}` : (auth.requiredMode === "TWO_PARTY" ? "two-party · no tenant policy auto-approves this" : "manual · no tenant policy auto-approves this")) : undefined },
              { k: "Approver", v: auth?.primaryApprover ? name(auth.primaryApprover) + (auth.primaryApprover.at ? " · " + fmtDateTime(auth.primaryApprover.at) : "") : (a.pending ? "none yet" : undefined) },
              { k: "Second approver", v: auth?.secondaryApprover ? name(auth.secondaryApprover) : (auth?.secondaryApproverPool?.length ? "one of " + auth.secondaryApproverPool.join(", ") : undefined) },
              { k: "Challenge", v: auth?.challengeAnswered ? "typed challenge answered" : undefined },
            ]} />
            {auth?.evaluations?.length ? (
              <Section title="Policies evaluated">
                {auth.evaluations.map((e, i) => (
                  <div key={i} className="irow mono">{e.policyRef} v{e.policyVersion} · {e.effect}{e.shadow ? (e.wouldHaveFired ? " · shadow: would have fired (not in force)" : " · shadow") : e.wouldHaveFired ? " · fired" : " · did not fire"}</div>
                ))}
              </Section>
            ) : null}
            <Section title="Undo">
              <KV rows={[
                { k: "Reversibility", v: a.reversibility?.toLowerCase().replace("_", "-") },
                { k: "Reversed by", v: rec?.links.reversedByRef ? <a onClick={() => openInspector({ kind: "action", actionId: rec.links.reversedByRef! })}>{rec.links.reversedByRef}</a> : undefined },
                { k: "Reversal attempted", v: rec?.links.reversalAttemptedByRef ? <a onClick={() => openInspector({ kind: "action", actionId: rec.links.reversalAttemptedByRef! })}>{rec.links.reversalAttemptedByRef}</a> : undefined },
                { k: "Reverses", v: rec?.links.reversalOfRef ? <a onClick={() => openInspector({ kind: "action", actionId: rec.links.reversalOfRef! })}>{rec.links.reversalOfRef}</a> : undefined },
                { k: "Retry of", v: rec?.links.retryOf || a.retryOf ? <a onClick={() => openInspector({ kind: "action", actionId: (rec?.links.retryOf || a.retryOf)! })}>{rec?.links.retryOf || a.retryOf}</a> : undefined },
                { k: "Retried by", v: rec?.links.retriedBy?.length ? <>{rec.links.retriedBy.map((r) => <a key={r} onClick={() => openInspector({ kind: "action", actionId: r })}>{r} </a>)}</> : undefined },
              ]} />
              {reversible ? (
                <div className="decide">
                  <button disabled={Boolean(state.actionsInFlight[actionId])} onClick={() => openPrompt({
                    title: "Request reversal of " + a.actionType,
                    label: "Why reverse it?",
                    placeholder: "e.g. the host is confirmed clean; restore connectivity",
                    helper: "Requests the inverse action at the same tier; it lands in the approval queue like any action. The original stays SUCCEEDED until the reversal verifiably took effect.",
                    confirm: "Request reversal",
                    onConfirm: (rationale) => {
                      dispatch({ type: "action.sent", actionId: a.actionId });
                      send({ type: "action.reverse", actionId: a.actionId, rationale });
                    },
                  })}>Request reversal</button>
                </div>
              ) : null}
            </Section>
          </>
        ) },
        { id: "targets", label: "Targets", body: () => (
          <>
            <Section title="Targets">
              {a.targets.map((t, i) => (
                <div key={i} className="irow mono">{t}{rec?.result?.perTarget?.find((p) => p.target === t) ? <> · <Badge tone={rec.result!.perTarget!.find((p) => p.target === t)!.outcome === "SUCCEEDED" ? "ok" : "warn"}>{rec.result!.perTarget!.find((p) => p.target === t)!.outcome}</Badge></> : null}</div>
              ))}
            </Section>
            {rec?.result ? (
              <Section title="Result">
                <KV rows={[
                  { k: "Outcome", v: rec.result.finalOutcome, mono: true },
                  { k: "Via", v: rec.result.adapter || a.adapter, mono: true },
                  { k: "Reference", v: rec.result.rawResponseRef || a.resultRef, mono: true },
                  { k: "Attempts", v: rec.result.attempts ? String(rec.result.attempts) : undefined, mono: true },
                  { k: "Error", v: rec.result.errorDetail },
                ]} />
              </Section>
            ) : a.plannedAdapter ? <p className="imeta">will dispatch via {a.plannedAdapter}</p> : null}
            {(a.evidenceRefs || []).length ? <Section title="Grounded on">{a.evidenceRefs.map((r) => <RefChip key={r} refId={r} />)}</Section> : null}
            {a.parameters ? <details className="iraw"><summary className="kicker">Parameters (frozen at request)</summary><Json value={a.parameters} /></details> : null}
          </>
        ) },
      ]} />
    </>
  );
}

export function ThreadInspector({ threadId }: { threadId: string }) {
  const { state, openInspector, openPrompt, send } = useApp();
  const t = state.snap?.comms?.find((x) => x.threadId === threadId);
  if (!t) return <div className="empty">This thread is not in the snapshot.</div>;
  const action = state.snap?.actions?.find((a) => a.actionId === t.actionId);
  const p = (action?.parameters || {}) as Record<string, unknown>;
  const openThread = t.status === "awaiting_reply" || t.status === "followed_up" || t.status === "replied";
  return (
    <>
      <div className="isub"><Badge tone={t.status === "replied" ? "ok" : t.status === "closed" ? "" : "warn"}>{t.status.replace("_", " ")}</Badge> <span className="mono">{t.actionType}</span> · sent {fmtDateTime(t.sentAt)} · {t.status === "closed" ? "closed" : "thread open"}</div>
      <Tabs tabs={[
        { id: "thread", label: "Thread", body: () => (
          <>
            <ol className="ihistory">
              {(t.trail || []).map((e, i) => (
                <li key={i}><span className="imeta">{fmtTime(e.at)}</span> <b>{e.direction === "inbound" ? "Reply" : e.direction === "note" ? "Note" : "Sent"}</b> · {e.author}: {e.body}</li>
              ))}
            </ol>
            <Section title="Follow-up">
              <p>{t.followUpDue ? "follow-up due" : t.nextFollowUpAt ? "next follow-up " + fmtDateTime(t.nextFollowUpAt) : "no follow-up scheduled"} · every {t.followUpHours}h · {t.followUps} sent</p>
              {openThread ? (
                <div className="decide">
                  {t.unackedReply ? <button className="primary" onClick={() => send({ type: "comms.act", threadId: t.threadId, verb: "ack" })}>Acknowledge</button> : null}
                  <button onClick={() => openPrompt({
                    title: "Follow up — " + t.target, label: "The follow-up message (you approve before it sends)", placeholder: "Any update on this?",
                    helper: "Requests a new " + t.actionType + " action carrying this thread's lineage; it lands in the approval queue as a preview.", confirm: "Request follow-up",
                    onConfirm: (message) => send({ type: "comms.followup", threadId: t.threadId, actionType: t.actionType, target: t.target, subject: t.subject || "", message, followUpHours: t.followUpHours }),
                  })}>Send follow-up…</button>
                  {t.followUpDue ? <button onClick={() => send({ type: "comms.snooze", threadId: t.threadId, hours: 24 })}>Snooze 24h</button> : null}
                  <button onClick={() => send({ type: "comms.act", threadId: t.threadId, verb: "done" })}>Mark done</button>
                </div>
              ) : null}
            </Section>
            <Section title="Escalation">
              <p className="imeta">{t.escalationTriggered ? `Escalation policy ${t.escalationPolicy || ""} triggered: repeated follow-ups, no resolution. ` : ""}Escalation surfaces a prompt if unanswered past its window. It never sends on its own.</p>
            </Section>
            {action ? <p><a onClick={() => openInspector({ kind: "action", actionId: action.actionId })}>Open the sending action</a></p> : null}
          </>
        ) },
        { id: "message", label: "Sent message", body: () => (
          <div className="sendpreview">
            {typeof p.subject === "string" && p.subject ? <div className="subj">{p.subject}</div> : t.subject ? <div className="subj">{t.subject}</div> : null}
            {typeof p.message === "string" && p.message ? p.message : typeof p.body === "string" ? p.body : (t.trail || [])[0]?.body || ""}
            <div className="imeta">to {t.target}</div>
          </div>
        ) },
      ]} />
    </>
  );
}
