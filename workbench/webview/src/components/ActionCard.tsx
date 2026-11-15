// The action card (03 §3.3; impl §2.3; adr/0361). A pending action is a card:
// at rest it says what approving will do (the will-do sentence ending with the
// route, the countdown, reversibility in words, the escalation reason) with
// Approve, Edit… and Reject… below; its details hold the evidence, the type
// id, the tier, the approval requirement the engine froze (adr/0099) and the
// record link. Everything else is a timeline item: one line at rest that opens
// its details, or an open card when it needs attention (a failure, an unknown
// outcome, an expiry that can be re-requested). `compact` (a turn in the
// companion) shares the header; its body is the Because chips, the pending
// card's decision lines (reversibility, the escalation reason, the approval
// requirement when it is more than the reader's own) and the Approve/Reject
// pair. Every variant renders the same ActionRow, so they can
// never disagree. A notification lists the dependencies riding on it (adr/0101).
import { useState } from "react";
import { useApp } from "../app";
import { actionStatusBadge, approvalNeedsMore, attentionKind, awaitsDecision, canReverse, outcomeStatus, reversalEnded, successors, takenOverBy, wasApproved, type Takeover } from "../derive/actions";
import { actionPreview, undoWords } from "../derive/actionPreview";
import type { ActionRow } from "../../../src/viewTypes";
import type { InboundMessage } from "../../../src/protocol";
import { Countdown, RefChip, fmtDateTime, fmtTime } from "./shared";

const GROUP_ICON: Record<string, string> = { containment: "⛨", eradication: "✂", recovery: "↻", notification: "✉", case_record: "⇄" };

export type ActionGroup = "containment" | "eradication" | "recovery" | "notification" | "case_record";
const GROUPS = new Set<string>(["containment", "eradication", "recovery", "notification", "case_record"]);

/** The response group an action belongs to: the catalog's category (#123). The
 * id-prefix guess stays only for a row served without one. */
export function groupOf(a: Pick<ActionRow, "actionType" | "category">): ActionGroup {
  if (a.category && GROUPS.has(a.category)) return a.category as ActionGroup;
  const t = a.actionType;
  if (t.startsWith("notify.")) return "notification";
  if (t.startsWith("ticket.")) return "case_record";
  if (t.startsWith("host.unisolate") || t.startsWith("account.enable") || t.startsWith("ioc.unblock") || t.startsWith("email.release")) return "recovery";
  if (t.startsWith("email.purge")) return "eradication";
  return "containment";
}
export const GROUP_LABELS: Record<ActionGroup, string> = { containment: "Containment", eradication: "Eradication", recovery: "Recovery", notification: "Notification", case_record: "Case record" };

/** What the action will do (#124): the sentence and every parameter it carries,
 * shown before Approve on every type, not only messages. With `via`, the
 * sentence ends with the tool that will run it (adr/0361). */
function WillDo({ a, via }: { a: ActionRow; via?: boolean }) {
  const pv = actionPreview(a);
  const route = a.adapter || a.plannedAdapter;
  return (
    <div className={"willdo" + (a.actionType.startsWith("notify.") ? " sendpreview" : "")}>
      <div className="kicker">{willDoKicker(a)}</div>
      <div className="wdsentence">{pv.sentence}{via ? (route ? <span className="wdvia"> via <span className="mono">{route}</span></span> : <span className="wdvia"> · no tool connected</span>) : null}</div>
      {pv.fields.map((f) => (
        <div key={f.label} className={"wdfield" + (f.long ? " long" : "")}><span className="wdlabel">{f.label}</span> <span className="wdvalue">{f.value}</span></div>
      ))}
      {pv.followUp ? <div className="imeta">↻ follow-up on an existing thread</div> : null}
    </div>
  );
}

/** The will-do kicker (adr/0361), by outcome: "Will do" while the request is
 * open or approved and not yet dispatched, "Doing" while it runs, "Did" once
 * it took effect, "Tried to" when it failed or its outcome is unknown, and
 * "Asked to" for a request that never ran (rejected, expired, lapsed). */
export function willDoKicker(a: ActionRow): string {
  if (awaitsDecision(a) || a.status === "APPROVED") return "Will do";
  switch (outcomeStatus(a)) {
    case "EXECUTING": return "Doing";
    case "SUCCEEDED": case "REVERSED": return "Did";
    case "FAILED": case "PARTIAL": case "TIMEOUT": case "OUTCOME_UNKNOWN": return "Tried to";
    default: return "Asked to";
  }
}

/** A parameter's label on the edit form, in the words the preview uses. */
const FIELD_LABEL = (actionType: string, key: string): string =>
  key === "body" ? (actionType.startsWith("ticket.") ? "Comment" : "Message")
    : ({ summary: "Summary", description: "Description", issue_type: "Type", assignee: "Assignee", to_status: "New state", resolution: "Resolution", subject: "Subject", message: "Message", follow_up_hours: "Follow up after (hours)", priority: "Priority (low, medium, high, critical)" } as Record<string, string>)[key]
    || key.replace(/_/g, " ");

/** The action's title: the catalog's human name, the id when there is none. */
export function actionTitle(a: Pick<ActionRow, "actionType" | "name">): string {
  return a.name || a.actionType;
}

/** What a failed or partial outcome means for the targets, in plain words: the
 * engine records an unconfirmed effect as unknown, never as done or undone.
 * Partial and timeout are read from the recorded outcome (adr/0361). */
export function failureSentence(a: Pick<ActionRow, "status" | "outcome" | "targets">): string {
  const on = a.targets.length ? a.targets.join(", ") : "the target";
  const s = outcomeStatus(a);
  if (s === "OUTCOME_UNKNOWN") return `Sent, but the run stopped before the tool answered — whether ${on} changed is unknown. Check the tool, then resolve it to what it shows.`;
  if (s === "PARTIAL") return `Partly done — some of ${on} may be changed; check each before relying on it.`;
  if (s === "TIMEOUT") return `No answer from the tool — whether ${on} changed is unknown.`;
  return `Did not complete — reckon cannot confirm any change on ${on}.`;
}


const STATUS_GLYPH: Record<string, string> = {
  SUCCEEDED: "✓", FAILED: "✗", TIMEOUT: "✗", PARTIAL: "◐", OUTCOME_UNKNOWN: "?", REVERSED: "↺",
  REJECTED: "⊘", EXPIRED: "⌛", APPROVED: "●", EXECUTING: "…",
};

/** The status as a glyph plus the engine's word, no pill (adr/0361). A
 * partial or timed-out outcome shows that word in place of the lifecycle
 * status it folds into. A request whose window ran out before the timer
 * converged reads EXPIRED. */
function StatusMark({ a }: { a: ActionRow }) {
  const sb = actionStatusBadge(a);
  const lapsed = a.expired && a.pending;
  const word = lapsed ? "EXPIRED" : sb.label;
  const tone = lapsed ? "warn" : sb.tone;
  return (
    <span className={"acstatus" + (tone ? " " + tone : "")}>
      <span aria-hidden="true">{STATUS_GLYPH[lapsed ? "EXPIRED" : outcomeStatus(a)] || "•"}</span>{word}
    </span>
  );
}

/** The Details disclosure on a card: a plain link with a chevron. */
function DetailsToggle({ open, onToggle }: { open: boolean; onToggle: () => void }) {
  return (
    <button className="linklike acdetailsbtn" aria-expanded={open} onClick={onToggle}>
      {open ? "Hide details" : "Details"}<span className={"chev" + (open ? " open" : "")} aria-hidden="true">⌄</span>
    </button>
  );
}

/** Every decision an action card offers, with its prompt: shared by the
 * pending card, the compact card and the timeline item, so only placement
 * differs between them. */
function useActionDecisions(a: ActionRow) {
  // successors() is computed here once per item; every rule below takes it.
  const { state, dispatch, send, openPrompt, openInspector } = useApp();
  // One decision at a time per action, across every card that renders it,
  // until the host settles it.
  const locked = Boolean(state.actionsInFlight[a.actionId]);
  const rows = state.snap?.actions || [];
  const next = successors(a, rows);
  // The dependencies this notification asks for (adr/0101).
  const riding = a.actionType.startsWith("notify.") ? (state.snap?.dependencies || []).filter((d) => d.notifyActionId === a.actionId) : [];
  const decideOn = (intent: InboundMessage) => {
    dispatch({ type: "action.sent", actionId: a.actionId });
    send(intent);
  };
  const openRecord = () => openInspector({ kind: "action", actionId: a.actionId });

  const approve = () => {
    if (a.tier === "T3") {
      openPrompt({
        title: "Approve " + a.actionType + " — Tier 3",
        label: "Type the challenge to confirm",
        helper: "High blast radius (" + a.targets.length + " targets). This action is "
          + (a.reversibility === "REVERSIBLE" ? "reversible." : a.reversibility === "BEST_EFFORT" ? "best-effort reversal — treat as permanent." : "irreversible."),
        confirm: "Approve",
        onConfirm: (challenge) => decideOn({ type: "action.approve", actionId: a.actionId, challenge }),
      });
    } else {
      decideOn({ type: "action.approve", actionId: a.actionId });
    }
  };
  // Resolve an unknown outcome (adr/0216): the person checked the tool and
  // records what it shows, what they checked, and where the evidence is.
  const resolve = () => openPrompt({
    title: "Resolve " + actionTitle(a) + (a.targets.length ? " — " + a.targets.join(", ") : ""),
    label: "What did you check, and what did it show?",
    placeholder: "e.g. the EDR console shows the host isolated since 14:02",
    extrasTop: [{
      kind: "select", id: "outcome", label: "The tool shows the action",
      options: [{ value: "SUCCEEDED", label: "took effect" }, { value: "FAILED", label: "did not take effect" }],
    }],
    extras: [
      { kind: "text", id: "reference", label: "Where you checked (optional)", placeholder: "e.g. a console link or the tool's job id" },
      { kind: "text", id: "artifactLocation", label: "Where the evidence is kept (optional) — reckon records the location, not the file", placeholder: "e.g. s3://ir-evidence/2026/console-capture.png" },
      { kind: "text", id: "artifactSystem", label: "Kept in", placeholder: "e.g. S3, SharePoint, the case system" },
      { kind: "text", id: "artifactAccess", label: "How a colleague gets access", placeholder: "e.g. ir-evidence-readers group" },
    ],
    helper: "Recorded as yours. Took effect: the action counts as done and can be reversed. Did not take effect: it counts as failed, and you can request it again.",
    confirm: "Resolve",
    onConfirm: (note, _picked, ex) => {
      const str = (k: string) => String(ex[k] ?? "").trim();
      const location = str("artifactLocation");
      decideOn({
        type: "action.resolve", actionId: a.actionId,
        outcome: str("outcome") === "FAILED" ? "FAILED" : "SUCCEEDED",
        note: note.trim(),
        reference: str("reference") || undefined,
        artifact: location ? { location, system: str("artifactSystem") || undefined, access: str("artifactAccess") || undefined } : undefined,
      });
    },
  });
  const reject = () => openPrompt({
    title: "Reject " + a.actionType, label: "Why is this rejected?", placeholder: "recorded on the audit trail", confirm: "Reject", require: false,
    onConfirm: (reason) => decideOn({ type: "action.reject", actionId: a.actionId, reason }),
  });
  // Edit before approve (adr/0127): the same request, its declared fields
  // prefilled; the longest text is the main box, the rest are fields beside it.
  const editable = (a.inputs || []).filter((i) => i.name !== "thread_ref");
  const edit = () => {
    const p = (a.parameters || {}) as Record<string, unknown>;
    const main = ["body", "message", "description"].find((k) => editable.some((i) => i.name === k));
    const others = editable.filter((i) => i.name !== main);
    openPrompt({
      title: "Edit " + actionTitle(a) + (a.targets.length ? " — " + a.targets.join(", ") : ""),
      label: main ? FIELD_LABEL(a.actionType, main) : "Why are you changing it? (optional)",
      value: main ? String(p[main] ?? "") : "",
      require: Boolean(main && editable.find((i) => i.name === main)?.required),
      extrasTop: others.map((i) => ({ kind: "text" as const, id: i.name, label: FIELD_LABEL(a.actionType, i.name) + (i.required ? "" : " (optional)"), value: p[i.name] === undefined ? "" : String(p[i.name]) })),
      extras: main ? [{ kind: "text" as const, id: "__reason", label: "Why are you changing it? (optional)", value: "" }] : [],
      helper: "Changes this request, not a new one: targets stay the same, the approval policies are checked again, and the record keeps what it said before.",
      confirm: "Save changes",
      onConfirm: (value, _picked, ex) => {
        const next: Record<string, unknown> = { ...p };
        if (main) next[main] = value;
        for (const i of others) {
          const v = String(ex[i.name] ?? "").trim();
          if (!v) { delete next[i.name]; continue; }
          next[i.name] = i.name === "follow_up_hours" && /^\d+$/.test(v) ? Number(v) : v;
        }
        const reason = String((main ? ex.__reason : value) ?? "").trim() || undefined;
        decideOn({ type: "action.edit", actionId: a.actionId, parameters: next, reason });
      },
    });
  };
  const rerequest = () => openPrompt({
    title: "Re-request " + a.actionType, label: "Why is this still warranted?", placeholder: "e.g. the host still shows the beaconing — containment still needed",
    helper: "Creates a NEW action (same targets & evidence, fresh approval window) you then approve. The original stays expired, linked as lineage.", confirm: "↻ Re-request",
    onConfirm: (rationale) => decideOn({ type: "action.rerequest", actionId: a.actionId, rationale }),
  });
  const reverse = () => openPrompt({
    title: "Request reversal of " + actionTitle(a), label: "Why reverse it?", placeholder: "e.g. the host is confirmed clean; restore connectivity",
    helper: a.reversibility === "BEST_EFFORT"
      ? "Requests the inverse action at the same tier, like any action. This undo is best effort: reckon cannot confirm it took effect everywhere, so the original stays SUCCEEDED and is marked as reversal attempted."
      : "Requests the inverse action at the same tier; it lands in the approval queue like any action.",
    confirm: "Request reversal",
    onConfirm: (rationale) => decideOn({ type: "action.reverse", actionId: a.actionId, rationale }),
  });
  return { locked, rows, next, riding, openRecord, openInspector, approve, resolve, reject, editable, edit, rerequest, reverse };
}

type Decisions = ReturnType<typeof useActionDecisions>;

/** The card header every variant shares (adr/0361): the group tile, the title
 * (it opens the action record), the target chips, the request time, then the
 * live countdown while a decision is open, else the status. */
function ActionHead({ a, d, time }: { a: ActionRow; d: Decisions; time: boolean }) {
  const group = groupOf(a);
  return (
    <div className="achead">
      <span className={"actile " + group} title={GROUP_LABELS[group]}>{GROUP_ICON[group]}</span>
      <a className="statement actitle" title={"Open the action record · " + a.actionType} onClick={d.openRecord}>{(a.isReversal ? "↺ " : "") + actionTitle(a)}</a>
      <span className="actargets">{a.targets.map((t) => <span key={t} className="mono">{t}</span>)}</span>
      <span className="acspacer" />
      {time && a.requestedAt ? <span className="actime mono" title={a.requestedAt}>requested {fmtTime(a.requestedAt)}</span> : null}
      {awaitsDecision(a) ? (a.expiresAt ? <Countdown expiresAt={a.expiresAt} /> : null) : <StatusMark a={a} />}
    </div>
  );
}

/** The retry lineage: the FAILED/EXPIRED action this request replaces. */
function RetryLine({ a, d }: { a: ActionRow; d: Decisions }) {
  return a.retryOf ? <div className="cardmeta">↻ retry of <a onClick={() => d.openInspector({ kind: "action", actionId: a.retryOf! })}>{a.retryOf.slice(0, 8)}…</a> (the original can never re-dispatch)</div> : null;
}

function RidingLine({ d }: { d: Decisions }) {
  return d.riding.length ? <div className="cardmeta acdeps">asks for {d.riding.map((x, i) => <span key={x.dependencyId}>{i ? ", " : ""}<button className="linklike" onClick={() => d.openInspector({ kind: "dependency", dependencyId: x.dependencyId })}>{x.title}</button> ({x.owner})</span>)}</div> : null;
}

function EditedLine({ a, d }: { a: ActionRow; d: Decisions }) {
  return a.editedAt ? <div className="cardmeta">✎ edited before approval · {fmtTime(a.editedAt)} — <a onClick={d.openRecord}>see what changed</a></div> : null;
}

function BasedOn({ a }: { a: ActionRow }) {
  return <>{a.evidenceRefs.map((r) => <RefChip key={r} refId={r} />)}</>;
}

/** The approval fact, with the Gate 2 decision link while the request is open. */
function ApprovalValue({ a, d }: { a: ActionRow; d: Decisions }) {
  return <>{approvalFact(a)}{a.pending ? <> · <button className="linklike" title="The Gate 2 decision on this request: the policy that matched, its effect, and what a shadow policy would have done" onClick={d.openRecord}>policy decision ›</button></> : null}</>;
}

/** The lines a pending card states at rest so Approve is an informed click
 * (adr/0361): reversibility in words, the escalation reason, and the approval
 * requirement when it is more than the reader's own approval. The full and
 * the compact card both render this one component. */
function DecisionLines({ a, d }: { a: ActionRow; d: Decisions }) {
  return (
    <>
      <div className="acundo">Can be undone: {undoWords(a.reversibility)}</div>
      {a.tierEscalated ? <div className="cardmeta">⚠ escalated to {a.tier}: {a.targets.length} targets (blast radius)</div> : null}
      {approvalNeedsMore(a) ? <div className="cardmeta"><ApprovalValue a={a} d={d} /></div> : null}
    </>
  );
}

/** A pending action (adr/0361). `full` is the Response card; `compact` is the
 * companion's, with the same header and its own body. */
export function ActionCard({ a, variant, draft }: { a: ActionRow; variant: "full" | "compact"; draft?: boolean }) {
  const { goTo } = useApp();
  const d = useActionDecisions(a);
  const [open, setOpen] = useState(false);
  const isNotify = a.actionType.startsWith("notify.");
  const group = groupOf(a);
  const decide = awaitsDecision(a);
  const buttons = decide ? (
    <div className="decide">
      <button className="primary" disabled={d.locked} onClick={d.approve}>{a.tier === "T3" ? "Approve (challenge)…" : isNotify ? "Approve · Send" : "Approve"}</button>
      {d.editable.length && a.status === "REQUESTED" ? <button disabled={d.locked} onClick={d.edit}>Edit…</button> : null}
      <button disabled={d.locked} onClick={d.reject}>Reject…</button>
    </div>
  ) : null;

  if (variant === "compact") {
    return (
      <div className={"card actioncard compact " + group}>
        <ActionHead a={a} d={d} time={true} />
        {decide ? <WillDo a={a} /> : null}
        {(a.evidenceRefs || []).length ? <div className="acbecause">Based on {a.evidenceRefs.slice(0, 4).map((r) => <RefChip key={r} refId={r} />)}</div> : null}
        {decide ? <DecisionLines a={a} d={d} /> : null}
        {buttons}
        <div className="imeta"><a onClick={() => goTo({ kind: "lens", lens: "response" })}>Full card in Response ›</a></div>
      </div>
    );
  }

  return (
    <div className={"card actioncard full pending " + group} id={"obj-" + a.actionId}>
      <ActionHead a={a} d={d} time={true} />
      <div className="acbody">
        <WillDo a={a} via />
        <DecisionLines a={a} d={d} />
        <RidingLine d={d} />
        <EditedLine a={a} d={d} />
        {draft && decide ? <div className="cardmeta actnote">First action on the world — approving activates the investigation.</div> : null}
        <div className="acactions">
          {buttons}
          <span className="acspacer" />
          <DetailsToggle open={open} onToggle={() => setOpen(!open)} />
        </div>
        {open ? (
          <div className="acdetails">
            <dl className="ackv">
              {a.evidenceRefs.length ? <><dt>Based on</dt><dd className="acbecause"><BasedOn a={a} /></dd></> : null}
              <dt>Action</dt><dd className="mono">{a.actionType}</dd>
              <dt>Tier</dt><dd>{a.tier}</dd>
              <dt>Approval</dt><dd><ApprovalValue a={a} d={d} /></dd>
              <dt>Record</dt><dd><button className="linklike" onClick={d.openRecord}>Open action record ›</button></dd>
            </dl>
            <RetryLine a={a} d={d} />
          </div>
        ) : null}
      </div>
    </div>
  );
}

/** The history line (adr/0361): when it was requested, then whether it was
 * approved, rejected or expired. The words come from the status; a recorded
 * time only adds when, labelled with the event it records. approvedAt is the
 * first approval's time: on a two-party action it is "first approval", then
 * "approved" with no time; on a request never fully approved it stays a first
 * approval. */
export function historyLine(a: ActionRow): string {
  const parts: string[] = [];
  if (a.requestedAt) parts.push(fmtTime(a.requestedAt) + " requested");
  if (wasApproved(a.status)) {
    if (a.approvedAt && a.requiredMode === "TWO_PARTY") parts.push("first approval " + fmtTime(a.approvedAt), "approved");
    else parts.push("approved" + (a.approvedAt ? " " + fmtTime(a.approvedAt) : ""));
  } else if (a.approvedAt) parts.push("first approval " + fmtTime(a.approvedAt));
  if (a.status === "REJECTED") parts.push("rejected");
  else if (a.status === "EXPIRED" || (a.pending && a.expired)) parts.push("expired");
  return parts.join(" · ") || "—";
}

/** The marker on a failure a later action took over (adr/0361), chosen by the
 * rule that matched. It names a request, never an outcome: the successor is
 * its own item. */
const TAKEOVER_MARKER: Record<NonNullable<Takeover>["how"], string> = {
  "retry": "re-requested ↻",
  "undo-partial": "reversal requested ↺",
  "re-reversal": "reversal requested again ↺",
};

/** One action on the Response timeline (adr/0361): a one-line row that opens
 * its details, or an open card when it needs attention. */
export function TimelineItem({ a, showDate }: { a: ActionRow; showDate?: boolean }) {
  const d = useActionDecisions(a);
  const [open, setOpen] = useState(false);
  const group = groupOf(a);
  const takeover = takenOverBy(a, d.rows, d.next);
  const kind = attentionKind(a, d.rows, d.next, takeover);
  const details = open ? <TimelineDetails a={a} d={d} /> : null;

  // The request time is the ordering key: the date rides along whenever the
  // day changes, and the tooltip gives the full local date and time.
  const time = (
    <span className="tltime mono" title={a.requestedAt ? fmtDateTime(a.requestedAt) : undefined}>
      <span>{a.requestedAt ? fmtTime(a.requestedAt) : "—"}</span>
      {showDate && a.requestedAt ? <span className="tldate">{fmtLocalDay(a.requestedAt)}</span> : null}
    </span>
  );

  if (kind) {
    return (
      <li className="tlitem" id={"obj-" + a.actionId}>
        {time}
        <span className={"tldot " + (kind === "rerequest" ? "warn" : "bad")} aria-hidden="true" />
        <div className={"card actioncard full attention " + (kind === "rerequest" ? "warn " : "bad ") + group}>
          <ActionHead a={a} d={d} time={false} />
          <div className="acbody">
            {kind === "failed" ? <div className="armeta err">✗ {failureSentence(a)}</div> : null}
            {kind === "unknown" ? <div className="armeta err">⚠ {failureSentence(a)}</div> : null}
            {kind === "rerequest" ? <div className="cardmeta">approval window elapsed — the world may have moved on</div> : null}
            <div className="acactions">
              {kind === "unknown" ? (
                <div className="decide">
                  <button className="primary" disabled={d.locked} title="Record what the tool shows. Nothing reruns this action until someone does." onClick={d.resolve}>Resolve…</button>
                </div>
              ) : null}
              {kind === "rerequest" ? (
                <div className="decide">
                  <button className="primary" disabled={d.locked} title="Create a fresh request (same targets + evidence, new approval window)" onClick={d.rerequest}>↻ Re-request</button>
                </div>
              ) : null}
              <span className="acspacer" />
              <DetailsToggle open={open} onToggle={() => setOpen(!open)} />
            </div>
            {details}
          </div>
        </div>
      </li>
    );
  }

  return (
    <li className="tlitem" id={"obj-" + a.actionId}>
      {time}
      <span className={"tldot ring " + (actionStatusBadge(a).tone || "")} aria-hidden="true" />
      <div className="tlmain">
        <div className="tlline">
          <button className="tlrow" aria-expanded={open} onClick={() => setOpen(!open)}>
            <span className={"actile " + group} title={GROUP_LABELS[group]}>{GROUP_ICON[group]}</span>
            <span className="tltitle">{(a.isReversal ? "↺ " : "") + actionTitle(a)}</span>
            <span className="actargets">{a.targets.map((t) => <span key={t} className="mono">{t}</span>)}</span>
            <span className="acspacer" />
            <StatusMark a={a} />
            <span className={"chev" + (open ? " open" : "")} aria-hidden="true">⌄</span>
          </button>
          {/* A failure a later action took over is handled (adr/0361): that
              action is its own item, and the marker links to it. */}
          {takeover ? (
            <button className="linklike tlretry" title={"Open the action that took over · " + takeover.by.actionId} onClick={() => d.openInspector({ kind: "action", actionId: takeover.by.actionId })}>{TAKEOVER_MARKER[takeover.how]}</button>
          ) : null}
        </div>
        {details}
      </div>
    </li>
  );
}

/** A timeline item's details, built only while open (adr/0361). */
function TimelineDetails({ a, d }: { a: ActionRow; d: Decisions }) {
  return (
    <div className="acdetails">
      <WillDo a={a} />
      <dl className="ackv">
        <dt>History</dt><dd className="mono">{historyLine(a)}</dd>
        <dt>Action</dt><dd className="mono">{a.actionType}</dd>
        <dt>Tier</dt><dd>{a.tier}{a.tierEscalated ? ` · escalated: ${a.targets.length} targets (blast radius)` : ""}</dd>
        <dt>Can be undone</dt><dd>{undoWords(a.reversibility)}</dd>
        <dt>Runs on</dt><dd className="mono">{a.adapter || a.plannedAdapter || "no tool connected"}{a.resultRef ? " → " + a.resultRef : ""}</dd>
        <dt>Approval</dt><dd><ApprovalValue a={a} d={d} /></dd>
        {a.evidenceRefs.length ? <><dt>Based on</dt><dd className="acbecause"><BasedOn a={a} /></dd></> : null}
        {a.errorDetail ? <><dt>Tool error</dt><dd className="mono acerror">{a.errorDetail}</dd></> : null}
      </dl>
      <RidingLine d={d} />
      <EditedLine a={a} d={d} />
      <RetryLine a={a} d={d} />
      {d.next.retry ? <SuccessorLine label="↻ re-requested as" s={d.next.retry} d={d} /> : null}
      {d.next.reversal ? <SuccessorLine label={reversalLabel(d.next.reversal)} s={d.next.reversal} d={d} /> : null}
      <div className="acactions">
        {canReverse(a, d.rows, d.next) ? (
          <button className="ghost" disabled={d.locked} onClick={d.reverse}>{(d.next.reversal ? "Reverse again" : "Reverse") + (a.reversibility === "BEST_EFFORT" ? " (best effort)…" : "…")}</button>
        ) : null}
        <button className="linklike" onClick={d.openRecord}>Open action record ›</button>
      </div>
    </div>
  );
}

/** The label on an original's reversal line: a reversal the tool never
 * answered is unconfirmed, not known to have failed (adr/0361). */
export function reversalLabel(r: ActionRow): string {
  if (outcomeStatus(r) === "TIMEOUT") return "↺ reversal unconfirmed:";
  return reversalEnded(r) ? "↺ reversal did not take effect:" : "↺ reversal";
}

/** A short local month and day ("Oct 27"). */
function fmtLocalDay(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

function SuccessorLine({ label, s, d }: { label: string; s: ActionRow; d: Decisions }) {
  return <div className="cardmeta">{label} <a onClick={() => d.openInspector({ kind: "action", actionId: s.actionId })}>{s.actionId.slice(0, 8)}…</a> · {actionStatusBadge(s).label}</div>;
}

// approvalFact states the human approval a pending action still needs, from
// what the list row carries (adr/0099): a manual request needs the reader's;
// a two-party one names who completes it, the pool the requiring policy
// froze, less whoever approved first (two distinct humans, 04 §3.3), or a
// second analyst when the policy named no pool; anything else pending needs
// the reader's approval, a row frozen AUTO_POLICY included (the server holds
// a pre-authorized request on a draft investigation, when the policy names no
// accountable human, or when the approval write failed, and an edit keeps
// the mode when the predicate stops matching: the row does not say which).
// Whether a policy matched is the record's to say: the row carries the
// requirement, not the evaluation.
export function approvalFact(a: ActionRow): string {
  if (!a.pending) {
    return wasApproved(a.status) ? "approved" : a.status === "REJECTED" ? "rejected" : a.status === "EXPIRED" ? "expired" : "—";
  }
  // A request whose window ran out needs nothing any more (adr/0361).
  if (a.expired) return "expired";
  const pool = a.secondaryApproverPool ?? [];
  if (a.status === "PENDING_SECONDARY") {
    if (!pool.length) {
      return "awaiting a second analyst's approval";
    }
    const rest = pool.filter((p) => p !== a.primaryApproverRef);
    if (!rest.length) {
      return "no one left in the pool can complete it · its only member gave the first approval";
    }
    return `awaiting a second approval from one of ${rest.join(", ")}`;
  }
  if (a.requiredMode === "TWO_PARTY") {
    return pool.length ? `two-party · a second approval from one of ${pool.join(", ")}` : "two-party · a second analyst must also approve";
  }
  return "needs your approval";
}
