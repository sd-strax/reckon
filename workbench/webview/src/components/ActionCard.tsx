// The action card (03 §3.3; impl §2.3): one component, two variants. `full`
// (Response) carries every decision-grade element and the state's buttons;
// `compact` (a turn in the companion) carries the header, the Because chips
// and the Approve/Reject pair, everything else a link to the full card. Both
// render the same ActionRow, so they can never disagree. The policy fact is
// gap §6.16: until it lands the card states the required mode the engine froze.
import { useApp } from "../app";
import { actionStatusBadge } from "../derive/actions";
import type { ActionRow } from "../../../src/backend";
import { Badge, Countdown, RefChip } from "./shared";

const GROUP_ICON: Record<string, string> = { containment: "⛨", eradication: "✂", recovery: "↻", notification: "✉", external: "⇄" };

/** The response-plan group an action type belongs to (08 §8.4 Response). */
export function groupOf(a: Pick<ActionRow, "actionType">): "containment" | "eradication" | "recovery" | "notification" | "external" {
  const t = a.actionType;
  if (t.startsWith("notify.")) return "notification";
  if (t.startsWith("ticket.")) return "external";
  if (t.startsWith("host.unisolate") || t.startsWith("account.enable") || t.startsWith("account.reset")) return "recovery";
  if (t.startsWith("process.") || t.startsWith("file.") || t.startsWith("ioc.")) return t.startsWith("ioc.") ? "containment" : "eradication";
  return "containment";
}
export const GROUP_LABELS: Record<ReturnType<typeof groupOf>, string> = { containment: "Containment", eradication: "Eradication", recovery: "Recovery", notification: "Notification", external: "External work" };

/** The policy fact as the record states it (adr/0099): a REQUESTED action a
 * policy auto-approved never exists, so a pending card says that none
 * pre-authorizes it and where the approval routes; a shadow policy that would
 * have fired is named; an approved one keeps its mode. */
export function policyFact(a: ActionRow): string {
  const p = a.policy;
  const shadow = p?.shadowWouldFire.length ? ` · a shadow policy would have pre-authorized this (${p.shadowWouldFire.join(", ")})` : "";
  if (a.pending) {
    if (p?.effect === "REQUIRE_TWO_PARTY" || a.requiredMode === "TWO_PARTY" || p?.routesTo === "pool") {
      const pool = p?.secondaryApproverPool?.length ? ` from ${p.secondaryApproverPool.join(", ")}` : "";
      return `Two-party${p?.matchedPolicyRef ? ` (policy ${p.matchedPolicyRef})` : ""}: a second approval${pool}` + shadow;
    }
    return `No policy pre-authorizes ${a.actionType} · needs your approval` + shadow;
  }
  if (p?.effect === "AUTO_APPROVE") return `Auto-approved by policy ${p.matchedPolicyRef || ""}`.trim();
  if (p?.effect === "DENY") return `Denied by policy ${p.matchedPolicyRef || ""}`.trim();
  if (a.status === "APPROVED" || a.status === "EXECUTING" || a.status === "SUCCEEDED") return "approved" + shadow;
  return "—";
}

export function ActionCard({ a, variant, locked, onLock, draft }: { a: ActionRow; variant: "full" | "compact"; locked: boolean; onLock: () => void; draft?: boolean }) {
  const { send, openPrompt, openInspector, goTo } = useApp();
  const p = (a.parameters || {}) as Record<string, unknown>;
  const isNotify = a.actionType.startsWith("notify.");
  const sb = actionStatusBadge(a);
  const group = groupOf(a);
  const bodyText = typeof p.message === "string" && p.message ? p.message : typeof p.body === "string" ? p.body : "";
  const decide = a.pending && !a.expired;

  const approve = () => {
    if (a.tier === "T3") {
      openPrompt({
        title: "Approve " + a.actionType + " — Tier 3",
        label: "Type the challenge to confirm",
        helper: "High blast radius (" + a.targets.length + " targets). This action is "
          + (a.reversibility === "REVERSIBLE" ? "reversible." : a.reversibility === "BEST_EFFORT" ? "best-effort reversal — treat as permanent." : "irreversible."),
        confirm: "Approve",
        onConfirm: (challenge) => send({ type: "action.approve", actionId: a.actionId, tier: a.tier, actionType: a.actionType, challenge }),
      });
    } else {
      onLock();
      send({ type: "action.approve", actionId: a.actionId, tier: a.tier, actionType: a.actionType });
    }
  };
  const reject = () => openPrompt({
    title: "Reject " + a.actionType, label: "Why is this rejected?", placeholder: "recorded on the audit trail", confirm: "Reject", require: false,
    onConfirm: (reason) => send({ type: "action.reject", actionId: a.actionId, actionType: a.actionType, reason }),
  });
  const buttons = decide ? (
    <div className="decide">
      <button className="primary" disabled={locked} onClick={approve}>{a.tier === "T3" ? "Approve (challenge)…" : isNotify ? "Approve · Send" : "Approve"}</button>
      <button disabled={locked} onClick={reject}>Reject…</button>
    </div>
  ) : a.expired && a.pending !== false && a.status === "EXPIRED" ? null : null;

  const head = (
    <div className="achead">
      <span className={"actile " + group} title={GROUP_LABELS[group]}>{GROUP_ICON[group]}</span>
      <span className="actitle">
        <a className="statement" title="Open the action record" onClick={() => openInspector({ kind: "action", actionId: a.actionId })}>{(a.isReversal ? "↺ " : "") + a.actionType}</a>
        <span className="actargets">{a.targets.map((t) => <span key={t} className="mono">{t}</span>)}</span>
      </span>
      <span className="acmeta">
        <Badge tone={a.tier}>{a.tier}{a.reversibility ? " · " + a.reversibility.replace("_", " ") : ""}</Badge>
        <Badge tone={a.expired ? "warn" : sb.tone}>{a.expired && a.pending ? "EXPIRED" : sb.label}</Badge>
      </span>
    </div>
  );

  if (variant === "compact") {
    return (
      <div className={"card actioncard compact " + group}>
        {head}
        {(a.evidenceRefs || []).length ? <div className="acbecause">Because {a.evidenceRefs.slice(0, 4).map((r) => <RefChip key={r} refId={r} />)}</div> : null}
        {buttons}
        <div className="imeta"><a onClick={() => goTo({ kind: "lens", lens: "response" })}>Full card in Response ›</a></div>
      </div>
    );
  }

  return (
    <div className={"card actioncard full " + group} id={"obj-" + a.actionId} style={a.expired && a.status === "EXPIRED" ? { opacity: 0.75 } : undefined}>
      {head}
      {(a.evidenceRefs || []).length ? <div className="acbecause"><span className="kicker">Because</span> {a.evidenceRefs.map((r) => <RefChip key={r} refId={r} />)}</div> : null}
      {isNotify && a.parameters ? (
        <div className="sendpreview">
          {typeof p.subject === "string" && p.subject ? <div className="subj">{p.subject}</div> : null}
          {bodyText}
          {typeof p.thread_ref === "string" && p.thread_ref ? <div className="imeta">↻ follow-up on an existing thread</div> : null}
        </div>
      ) : null}
      <dl className="acfacts">
        <div><dt>Undo</dt><dd>{a.reversibility === "REVERSIBLE" ? "reversible" : a.reversibility === "BEST_EFFORT" ? "best effort — treat as permanent" : a.reversibility === "IRREVERSIBLE" ? "irreversible" : "—"}</dd></div>
        <div><dt>Runs via</dt><dd className="mono">{a.adapter || a.plannedAdapter || "—"}{a.resultRef ? " → " + a.resultRef : ""}</dd></div>
        <div><dt>Response policy</dt><dd>{policyFact(a)}</dd></div>
        <div><dt>Window</dt><dd>{a.pending && !a.expired && a.expiresAt ? <Countdown expiresAt={a.expiresAt} /> : a.expired ? "elapsed" : "—"}</dd></div>
      </dl>
      {a.retryOf ? <div className="cardmeta">↻ retry of <a onClick={() => openInspector({ kind: "action", actionId: a.retryOf! })}>{a.retryOf.slice(0, 8)}…</a> (the original can never re-dispatch)</div> : null}
      {a.tierEscalated ? <div className="cardmeta">⚠ escalated to {a.tier}: {a.targets.length} targets (blast radius)</div> : null}
      {a.errorDetail && (a.status === "FAILED" || a.status === "TIMEOUT" || a.status === "PARTIAL") ? <div className="armeta err">✗ {a.errorDetail}</div> : null}
      {draft && decide ? <div className="cardmeta actnote">First action on the world — approving activates the investigation.</div> : null}
      {buttons}
      {a.expired && a.status === "EXPIRED" ? (
        <div className="decide">
          <span className="cardmeta">approval window elapsed — the world may have moved on</span>
          <button className="primary" disabled={locked} title="Create a fresh request (same targets + evidence, new approval window)" onClick={() => openPrompt({
            title: "Re-request " + a.actionType, label: "Why is this still warranted?", placeholder: "e.g. the host still shows the beaconing — containment still needed",
            helper: "Creates a NEW action (same targets & evidence, fresh approval window) you then approve. The original stays expired, linked as lineage.", confirm: "↻ Re-request",
            onConfirm: (rationale) => send({ type: "action.rerequest", actionId: a.actionId, actionType: a.actionType, rationale }),
          })}>↻ Re-request</button>
        </div>
      ) : null}
      {a.status === "SUCCEEDED" && a.reversibility === "REVERSIBLE" ? (
        <div className="decide">
          <button className="ghost" onClick={() => openPrompt({
            title: "Request reversal of " + a.actionType, label: "Why reverse it?", placeholder: "e.g. the host is confirmed clean; restore connectivity",
            helper: "Requests the inverse action at the same tier; it lands in the approval queue like any action.", confirm: "Request reversal",
            onConfirm: (rationale) => send({ type: "action.reverse", actionId: a.actionId, rationale }),
          })}>Reverse…</button>
        </div>
      ) : null}
    </div>
  );
}
