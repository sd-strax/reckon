// Resolving an inspector target to a renderer (impl §2.4): from the id's
// shape and the snapshot, so most panes open instantly and only the ones that
// need the record wait on a round trip. Pure.
import type { EvidenceDoc, ThreadEntry } from "../../../src/viewTypes";
import type { InspectorTarget, InvestigationSnapshot } from "../../../src/protocol";
import { refType, refTypeLabel } from "../refs";
import { isPersonDoc } from "../../../src/identityKinds";

export type Renderer =
  | "evidence" | "entity" | "absence" | "detection" | "attestation"
  | "prediction" | "hypothesis" | "action" | "thread" | "case"
  | "turn" | "pin" | "act" | "person" | "alias" | "procedure" | "call" | "dependency";

/** An agent turn has a transcript to show; a pin or a human act does not (#144). */
export function turnRenderer(entry: ThreadEntry | undefined): "turn" | "pin" | "act" {
  if (!entry || entry.hasTranscript) return "turn";
  if (entry.interpretationType === "evidence-pin") return "pin";
  return entry.actor.kind === "AI_DELEGATED" && entry.toolCalls ? "turn" : "act";
}

const ENTITY_TYPES = new Set([
  "x-host", "ipv4-addr", "ipv6-addr", "user-account", "process", "file",
  "domain-name", "email-addr", "url", "windows-registry-key",
  "x-scheduled-task", "x-group", "mac-addr", "software",
  "x-cloud-account", "x-cloud-resource",
]);

/** A stable key for a target: the pane identity and the fetch cache key. */
export function targetKey(t: InspectorTarget): string {
  switch (t.kind) {
    case "ref": return "ref:" + t.ref;
    case "sop": return "sop:" + t.sopId;
    case "turn": return "turn:" + t.interpretationId;
    case "action": return "action:" + t.actionId;
    case "thread": return "thread:" + t.threadId;
    case "case": return "case:" + t.investigationRef;
    case "person": return "person:" + t.principalId;
    case "call": return "call:" + t.callId;
    case "dependency": return "dependency:" + t.dependencyId;
  }
}

/** Which renderer a target takes. A `relationship--` ref is an alias only when
 * the fetched edge says so; until the doc arrives it renders as evidence. */
export function rendererFor(t: InspectorTarget, doc?: EvidenceDoc | null, entry?: ThreadEntry): Renderer {
  switch (t.kind) {
    case "sop": return "procedure";
    case "turn": return turnRenderer(entry);
    case "action": return "action";
    case "thread": return "thread";
    case "case": return "case";
    case "person": return "person";
    case "call": return "call";
    case "dependency": return "dependency";
    case "ref": {
      const type = refType(t.ref);
      if (type === "x-null-observation") return "absence";
      if (type === "indicator" || type === "sighting") return "detection";
      if (type === "note") return "attestation";
      if (type === "x-prediction") return "prediction";
      if (type === "x-hypothesis") return "hypothesis";
      if (ENTITY_TYPES.has(type)) return "entity";
      // An identity is a person (adr/0324) or a vendor; only its doc tells.
      // Until it arrives, or when it cannot be read, it opens as an entity,
      // so a person's pane is the one the open mounts; a vendor's turns to
      // evidence once its doc says so.
      if (type === "identity") return !doc || isPersonDoc(doc) ? "entity" : "evidence";
      if (type === "relationship") {
        const p = (doc?.payload ?? {}) as { relationship_type?: unknown };
        return p.relationship_type === "aliases" ? "alias" : "evidence";
      }
      return "evidence";
    }
  }
}

/** The eyebrow type label for a renderer. */
export function rendererLabel(r: Renderer): string {
  switch (r) {
    case "evidence": return "Evidence";
    case "entity": return "Entity";
    case "absence": return "Evidence · absence";
    case "detection": return "Detection finding";
    case "attestation": return "Attestation";
    case "prediction": return "Prediction";
    case "hypothesis": return "Hypothesis";
    case "action": return "Action";
    case "thread": return "Notification thread";
    case "case": return "Similar past case";
    case "turn": return "Turn";
    case "pin": return "Pinned finding";
    case "act": return "Act on the record";
    case "person": return "Person";
    case "alias": return "Alias";
    case "procedure": return "Procedure";
    case "call": return "Capability call";
    case "dependency": return "Dependency";
  }
}

/** A human name for the pane before (and without) a fetch: from the snapshot
 * when the object lives there, the resolved label when a chip resolved it,
 * the type word otherwise. */
export function targetName(t: InspectorTarget, snap: InvestigationSnapshot | null, labels: Record<string, string>): string {
  switch (t.kind) {
    case "ref": {
      const type = refType(t.ref);
      if (type === "x-hypothesis") {
        const h = snap?.hypotheses.find((x) => x.id === t.ref);
        if (h) return h.handle ? h.handle + " · " + h.statement : h.statement;
      }
      if (type === "x-prediction") {
        for (const h of snap?.hypotheses || []) {
          const p = h.predictions.find((x) => x.id === t.ref);
          if (p) return (p.ordinal ? "P" + p.ordinal + " · " : "") + p.statement;
        }
      }
      return labels[t.ref] || refTypeLabel(t.ref);
    }
    case "sop": {
      const consulted = snap?.thread?.flatMap((e) => e.consultedSops || []).find((s) => s.sopId === t.sopId);
      return consulted?.title || labels["sop:" + t.sopId] || "Procedure";
    }
    case "turn": {
      const e = snap?.thread?.find((x) => x.interpretationId === t.interpretationId);
      return e?.summary || "Turn";
    }
    case "action": {
      const a = snap?.actions?.find((x) => x.actionId === t.actionId);
      return a ? a.actionType + " → " + a.targets.join(", ") : "Action";
    }
    case "thread": {
      const th = snap?.comms?.find((x) => x.threadId === t.threadId);
      return th ? (th.subject || th.target) : "Notification thread";
    }
    case "case": {
      const consulted = snap?.thread?.flatMap((e) => e.consultedSimilar || []).find((s) => s.investigationRef === t.investigationRef);
      return consulted?.title || labels["case:" + t.investigationRef] || "Past case";
    }
    case "call": return labels["call:" + t.callId] || "Capability call";
    case "dependency": {
      const d = snap?.dependencies?.find((x) => x.dependencyId === t.dependencyId);
      return d ? d.title : "Dependency";
    }
    case "person": {
      const e = snap?.thread?.find((x) => x.actor.principal === t.principalId && x.actor.principalName);
      return e?.actor.principalName || t.principalId;
    }
  }
}
