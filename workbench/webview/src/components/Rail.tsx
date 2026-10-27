// The state rail's remaining sections: the knowledge rail (06 §5.1), pinned
// evidence (01 §Pinned evidence), the action ledger, external work (ui/04)
// and the capability health fold (13 §4; collapsed to a count here).
import { useApp } from "../app";
import { actionStatusBadge } from "../derive/actions";
import { badgeTone } from "../derive/hyps";
import { Badge, Empty, Eyebrow, RefChip, fmtDateTime } from "./shared";

export function KnowledgeRail() {
  const { state, dispatch, openInspector } = useApp();
  const items = state.knowledge;
  if (!items.length) return null;
  const anyIncluded = items.some((k) => k.included);
  return (
    <section id="knowledgeSection">
      <Eyebrow>Institutional knowledge<span className="posture">{anyIncluded ? "in context" : "check to include"}</span></Eyebrow>
      <div id="knowledgeBox">
        {items.map((k) => (
          <div key={k.ref} className={"kitem" + (k.included ? "" : " excluded")}>
            <input
              type="checkbox"
              checked={!!k.included}
              title={k.included ? "Included in the agent's context" : "Not in context — check to include"}
              onChange={(e) => dispatch({ type: "knowledge.toggle", ref: k.ref, included: e.target.checked })}
            />
            <div className="kbody" title={k.excerpt || ""}>
              <div className="ktitle">
                <span className="kkind">{k.kind === "sop" ? "SOP" : "PRIOR CASE"}</span>
                <a onClick={() => openInspector(k.kind === "sop" ? { kind: "sop", sopId: k.ref } : { kind: "case", investigationRef: k.ref })}>{k.title || k.ref}</a>{" "}
                {k.kind === "case" && k.band ? <span className={"kband " + (k.band === "NEAR_DUPLICATE" ? "near" : k.band === "DISTINCT" ? "distinct" : "")}>{k.band}</span> : null}
              </div>
              {k.rationale ? <div className="krationale">{k.rationale}</div> : null}
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}

export function Pins() {
  const { state, send, openPrompt } = useApp();
  const pins = state.snap?.pins || [];
  const active = pins.filter((p) => !p.superseded).length;
  return (
    <section>
      <Eyebrow>Pinned evidence{pins.length ? " · " + active : ""}</Eyebrow>
      <div id="pinsBox">
        {!pins.length ? <Empty>Nothing pinned yet — pin key findings from tool results</Empty> : pins.map((p) => (
          <div key={p.interpretationId} className={"card pinrow" + (p.superseded ? " superseded" : "")}>
            {!p.superseded ? (
              <button className="unpin" title="Supersede this pin (stays visible, struck)" onClick={() => openPrompt({
                title: "Remove from evidence",
                label: "Why remove this?",
                placeholder: "e.g. re-scoped — this logon was expected activity",
                helper: "The pin stays visible on the thread, struck — nothing is deleted.",
                confirm: "Remove",
                onConfirm: (reason) => send({ type: "pin.unpin", interpretationId: p.interpretationId, reason }),
              })}>un-pin</button>
            ) : null}
            <div className="finding">{p.finding}</div>
            <div className="cardmeta">{(p.actor === "AI_DELEGATED" ? "AI" : p.actor === "SYSTEM" ? "system" : "analyst") + (p.pinnedAt ? " · " + fmtDateTime(p.pinnedAt) : "")}</div>
            <div>{(p.inputRefs || []).map((r) => <RefChip key={r} refId={r} pinned={!p.superseded} />)}</div>
          </div>
        ))}
      </div>
    </section>
  );
}

/** The action ledger: every action on this investigation, newest first, with
 * where it landed. Read-only — the approval affordances stay in the interrupt. */
export function ActionsLedger() {
  const { state, openInspector } = useApp();
  const all = state.snap?.actions || [];
  if (!all.length) return null;
  return (
    <section id="actionsSection">
      <Eyebrow>Actions · {all.length}</Eyebrow>
      <div id="actionsBox">
        {all.slice().reverse().map((a) => {
          const sb = actionStatusBadge(a);
          return (
            <div key={a.actionId} className={"actrow " + (a.pending ? "pend" : "settled")}>
              <div className="artop">
                <a className="atype" title="Open the action record" onClick={() => openInspector({ kind: "action", actionId: a.actionId })}>{(a.isReversal ? "↺ " : "") + a.actionType}</a>
                <Badge tone={a.tier}>{a.tier}</Badge>
                <Badge tone={sb.tone}>{sb.label}</Badge>
              </div>
              <div className="artgt">→ {(a.targets || []).join(", ")}</div>
              {a.adapter ? <div className="armeta via">via {a.adapter}{a.resultRef ? " → " + a.resultRef : ""}</div> : null}
              {a.status === "REVERSED" ? <div className="armeta">↺ reversed</div>
                : a.status === "SUCCEEDED" && a.reversibility === "BEST_EFFORT" ? <div className="armeta">↩? best-effort — cannot be verified undone</div> : null}
              {a.tierEscalated ? <div className="armeta">⚠ escalated to {a.tier} (blast radius)</div> : null}
              {a.errorDetail && (a.status === "FAILED" || a.status === "TIMEOUT" || a.status === "PARTIAL") ? <div className="armeta err">✗ {a.errorDetail}</div> : null}
            </div>
          );
        })}
      </div>
    </section>
  );
}

/** Comms threads (Phase F, ui/04): conversation state that FOLLOWED an approved
 * notify.* action — nothing here sends anything; Follow up requests a new action. */
export function Comms() {
  const { state, send, openPrompt, openInspector } = useApp();
  const threads = state.snap?.comms || [];
  if (!threads.length) return null;
  const open = threads.filter((t) => t.status === "awaiting_reply" || t.status === "followed_up").length;
  return (
    <section id="commsSection">
      <Eyebrow>External work · {open} open</Eyebrow>
      <div id="commsBox">
        {threads.map((t) => {
          const last = (t.trail || [])[t.trail.length - 1];
          const openThread = t.status === "awaiting_reply" || t.status === "followed_up" || t.status === "replied";
          const bits = ["sent " + fmtDateTime(t.sentAt)];
          if (t.followUpHours) bits.push("follow-up " + t.followUpHours + "h · " + t.followUps + " sent");
          if (t.followUpDue) bits.push("⏰ follow-up due");
          return (
            <div key={t.threadId} className={"card commscard" + (t.status === "replied" ? " replied" : "")} style={t.status === "closed" ? { opacity: 0.7 } : undefined}>
              <div>
                <a className="statement" title="Open the thread" onClick={() => openInspector({ kind: "thread", threadId: t.threadId })}>{(t.actionType === "notify.email" ? "✉ " : "# ") + t.target}</a>
                <Badge tone={badgeTone(t.status)}>{t.status.replace("_", " ")}</Badge>
              </div>
              {t.subject ? <div className="cardmeta">{t.subject}</div> : null}
              {last ? <div className={"quote" + (last.direction === "inbound" ? " inbound" : "")}>{(last.direction === "inbound" ? "↩ " : "") + last.author + ": " + last.body}</div> : null}
              <div className="cardmeta">{bits.join(" · ")}</div>
              {t.escalationTriggered ? <div className="esc">⚠ Escalation policy triggered ({t.escalationPolicy || ""}) — repeated follow-ups, no resolution. Consider escalating; nothing auto-fires.</div> : null}
              {(t.unackedReply || openThread) ? (
                <div className="decide">
                  {t.unackedReply ? <button className="primary" title="Record that you saw the reply (kept on the trail)" onClick={() => send({ type: "comms.act", threadId: t.threadId, verb: "ack" })}>Acknowledge</button> : null}
                  {openThread ? (
                    <>
                      <button className={t.followUpDue && !t.unackedReply ? "primary" : undefined} title="Drafts a follow-up message — it becomes a new action you approve before anything is sent" onClick={() => openPrompt({
                        title: "Follow up — " + t.target,
                        label: "The follow-up message (you approve before it sends)",
                        placeholder: "Any update on this? Still needed for the investigation.",
                        helper: "Requests a new " + t.actionType + " action carrying this thread's lineage; it lands in the approval queue as a preview.",
                        confirm: "Request follow-up",
                        onConfirm: (message) => send({ type: "comms.followup", threadId: t.threadId, actionType: t.actionType, target: t.target, subject: t.subject || "", message, followUpHours: t.followUpHours }),
                      })}>Follow up…</button>
                      {t.followUpDue ? <button onClick={() => send({ type: "comms.snooze", threadId: t.threadId, hours: 24 })}>Snooze 24h</button> : null}
                      <button title="The external work is resolved — closes the thread" onClick={() => send({ type: "comms.act", threadId: t.threadId, verb: "done" })}>Mark done</button>
                    </>
                  ) : null}
                </div>
              ) : null}
            </div>
          );
        })}
      </div>
    </section>
  );
}

export function CapabilitiesFold() {
  const { state, openEnableForm } = useApp();
  const caps = state.snap ? state.snap.capabilities : undefined;
  const enablement = state.snap?.enablement;
  let head = "Capabilities";
  let body: React.ReactNode;
  if (caps === undefined) {
    body = <Empty>…</Empty>;
  } else if (caps === null) {
    head = "Capabilities · off";
    body = <Empty>capability layer off</Empty>;
  } else if (!caps.length) {
    body = <Empty>none configured</Empty>;
  } else {
    const avail = caps.filter((c) => c.status === "available").length;
    head = "Capabilities · " + avail + "/" + caps.length + " available";
    const order: Record<string, number> = { available: 0, degraded: 1, unavailable: 2 };
    const sorted = caps.slice().sort((a, b) => (order[a.status] ?? 3) - (order[b.status] ?? 3) || a.verb.localeCompare(b.verb));
    body = sorted.map((c) => {
      // Installed-not-enabled (11 §6.2): the operator-facing affordance on the health row itself.
      let setup: React.ReactNode = null;
      if (c.status === "unavailable" && enablement) {
        const ev = (enablement.verbs || []).find((x) => x.verb === c.verb);
        if (ev && !ev.enabled && (ev.closableBy || []).length) {
          setup = <button className="setup" title={ev.closableBy[0] + " is installed but not enabled — enable it to serve " + c.verb} onClick={() => openEnableForm(ev.closableBy[0])}>set up</button>;
        }
      }
      return (
        <div key={c.verb} className={"caprow " + c.status}>
          <span className={"dot " + c.status} title={c.status} />
          <span className="verb">{c.verb}</span>
          {setup}
        </div>
      );
    });
  }
  return (
    <details className="railfold" id="capsFold">
      <summary>{head}</summary>
      <div id="caps">{body}</div>
    </details>
  );
}
