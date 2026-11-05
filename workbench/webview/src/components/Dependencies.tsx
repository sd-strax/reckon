// Dependencies on others (adr/0101): the work the conclusion depends on that
// only someone outside reckon can do. Recorded by the analyst, ridden on a
// notify.* action, moved to SENT by the engine when that send succeeds, and
// resolved by the analyst alone: CONFIRMED with a note on the owner's word, or
// WAIVED with the reason. A reply on the channel never moves one. Nothing here
// mirrors the conversation; a dependency points out to where the work lives.
import { useApp } from "../app";
import { openDependencies, rideStopped } from "../derive/dependencies";
import type { Dependency } from "../../../src/viewTypes";
import type { PromptSpec } from "./PromptCard";
import { Badge, fmtDateTime } from "./shared";

export const DEPENDENCY_LABEL: Record<string, string> = { OPEN: "not yet asked", SENT: "asked", CONFIRMED: "confirmed", WAIVED: "waived" };

export function dependencyTone(status: string): string {
  return status === "CONFIRMED" ? "ok" : status === "WAIVED" ? "" : "warn";
}

/** The prompt that records a new dependency: what, who, why, where. */
export function createDependencySpec(notify: { actionId: string; label: string }[], onConfirm: (d: { title: string; owner: string; detail?: string; externalRef?: string; notifyActionId?: string }) => void): PromptSpec {
  return {
    title: "Record a dependency",
    label: "What has to be done",
    placeholder: "e.g. rotate the svc_backup credential",
    helper: "Work only someone outside reckon can do. It counts until you confirm or waive it; the conclude preflight lists it.",
    confirm: "Record",
    extrasTop: [
      { kind: "text", id: "owner", label: "Who owes it", placeholder: "e.g. platform team" },
      { kind: "text", id: "detail", label: "Why (optional)", placeholder: "e.g. the secret was exposed in the dropper's config" },
      { kind: "text", id: "externalRef", label: "Where it lives outside (optional)", placeholder: "a ticket number or a thread link" },
      ...(notify.length ? [{ kind: "select" as const, id: "notifyActionId", label: "Ride on a pending notification", options: [{ value: "", label: "none yet" }, ...notify.map((n) => ({ value: n.actionId, label: n.label }))] }] : []),
    ],
    onConfirm: (title, _picked, ex) => {
      const owner = String(ex.owner || "").trim();
      if (!owner) return;
      onConfirm({ title, owner, detail: String(ex.detail || "") || undefined, externalRef: String(ex.externalRef || "") || undefined, notifyActionId: String(ex.notifyActionId || "") || undefined });
    },
  };
}

/** The prompt that resolves a dependency: the analyst's note, and the pointer out. */
export function resolveDependencySpec(d: Dependency, verb: "confirm" | "waive", onConfirm: (note: string, externalRef?: string) => void): PromptSpec {
  return {
    title: (verb === "confirm" ? "Confirm — " : "Waive — ") + d.title,
    label: verb === "confirm" ? "What the owner confirmed, and how you know" : "Why this is left to no one",
    placeholder: verb === "confirm" ? "e.g. the platform lead confirmed the rotation on the ticket" : "e.g. the machine is being retired this week",
    helper: verb === "confirm"
      ? "Recorded as yours: the owner's word, as you received it. The record shows it as a human statement, never as something reckon observed."
      : "A waive needs a reason; a conclusion with work knowingly left open is legitimate only when the record says why.",
    confirm: verb === "confirm" ? "Record confirmation" : "Waive",
    extras: [{ kind: "text", id: "externalRef", label: "Where it lives outside (optional)", placeholder: "a ticket number or a thread link", value: d.externalRef || "" }],
    onConfirm: (note, _picked, ex) => onConfirm(note, String(ex.externalRef || "") || undefined),
  };
}

export function Dependencies() {
  const { state, send, openPrompt, openInspector } = useApp();
  const snap = state.snap;
  const deps = snap?.dependencies;
  const actions = snap?.actions || [];
  const pendingNotify = actions.filter((a) => a.actionType.startsWith("notify.") && a.pending && !a.expired)
    .map((a) => ({ actionId: a.actionId, label: a.actionType.replace("notify.", "") + " → " + a.targets.join(", ") }));
  const closed = ["CONCLUDED", "ARCHIVED", "DISCARDED"].includes(String(snap?.investigation.state || "").toUpperCase());
  const open = openDependencies(deps) ?? 0;
  return (
    <section className="lsection" id="obj-dependencies">
      <h3>Dependencies on others{deps?.length ? <span className="count">{open}</span> : null}</h3>
      <div className="imeta">Work only someone outside reckon can do. Confirmed or waived by you, never by a reply.</div>
      {deps === null ? <div className="empty">The dependencies could not be read. The preflight treats their count as unchecked until they do.</div> : null}
      {deps && !deps.length ? <div className="empty">none recorded</div> : null}
      {(deps || []).map((d) => {
        const riding = d.notifyActionId ? actions.find((a) => a.actionId === d.notifyActionId) : undefined;
        const live = d.status === "OPEN" || d.status === "SENT";
        // A ride that will not send (failed, expired, rejected): the OPEN
        // dependency can move to a replacement. One in flight keeps its link.
        const rideDead = !!riding && rideStopped(riding);
        return (
          <div key={d.dependencyId} className={"card depcard" + (live ? "" : " settled")} id={"obj-" + d.dependencyId}>
            <div>
              <button className="linklike statement" title="Open the dependency" onClick={() => openInspector({ kind: "dependency", dependencyId: d.dependencyId })}>{d.title}</button>{" "}
              <Badge tone={dependencyTone(d.status)}>{DEPENDENCY_LABEL[d.status] || d.status.toLowerCase()}</Badge>
            </div>
            <div className="cardmeta">owed by <b>{d.owner}</b>{d.detail ? " · " + d.detail : ""}</div>
            <div className="cardmeta">
              {riding ? <>rides on <button className="linklike" onClick={() => openInspector({ kind: "action", actionId: riding.actionId })}>{riding.actionType.replace("notify.", "") + " → " + riding.targets.join(", ")}</button> · </> : null}
              {d.status === "SENT" ? "asked " : "recorded "}{fmtDateTime(d.status === "SENT" ? d.history.find((e) => e.event === "sent")?.at || d.createdAt : d.createdAt)}
              {d.externalRef ? <> · <span className="mono">{d.externalRef}</span></> : null}
            </div>
            {!live && d.resolutionNote ? <div className="quote">{(d.status === "CONFIRMED" ? "✓ " : "— ") + (d.resolvedByName || d.resolvedBy || "") + ": " + d.resolutionNote}</div> : null}
            {live && !closed ? (
              <div className="decide">
                <button className="primary" title="Record the owner's confirmation, as you received it" onClick={() => openPrompt(resolveDependencySpec(d, "confirm", (note, externalRef) => send({ type: "dependency.resolve", dependencyId: d.dependencyId, verb: "confirm", note, externalRef })))}>Confirm…</button>
                <button title="Leave this to no one, with the reason on the record" onClick={() => openPrompt(resolveDependencySpec(d, "waive", (note, externalRef) => send({ type: "dependency.resolve", dependencyId: d.dependencyId, verb: "waive", note, externalRef })))}>Waive…</button>
                {d.status === "OPEN" && (!riding || rideDead) && pendingNotify.length ? (
                  <button title={rideDead ? "That notification will not send; ride on a pending one instead" : "Ride on a notification that has not been sent yet; the send moves it to asked"} onClick={() => openPrompt({
                    title: "Ride on a notification — " + d.title, label: "Pick the pending notification", require: false, confirm: "Attach",
                    extrasTop: [{ kind: "select", id: "notifyActionId", label: "Notification", options: pendingNotify.map((n) => ({ value: n.actionId, label: n.label })) }],
                    onConfirm: (_v, _picked, ex) => { const id = String(ex.notifyActionId || pendingNotify[0]?.actionId || ""); if (id) send({ type: "dependency.attach", dependencyId: d.dependencyId, notifyActionId: id }); },
                  })}>{rideDead ? "Move to another notification…" : "Attach to notification…"}</button>
                ) : null}
              </div>
            ) : null}
          </div>
        );
      })}
      {deps && !closed ? (
        <div className="decide">
          <button onClick={() => openPrompt(createDependencySpec(pendingNotify, (d) => send({ type: "dependency.create", ...d })))}>Record a dependency…</button>
        </div>
      ) : null}
    </section>
  );
}
