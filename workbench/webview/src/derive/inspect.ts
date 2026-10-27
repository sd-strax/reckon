// Resolving an inspector target to a renderer (impl §2.4): from the id's
// shape and the snapshot, so most panes open instantly and only the ones that
// need the record wait on a round trip. Pure.
import type { EvidenceDoc } from "../../../src/backend";
import type { InspectorTarget, InvestigationSnapshot } from "../../../src/protocol";
import { refType, refTypeLabel } from "../refs";

export type Renderer =
  | "evidence" | "entity" | "absence" | "detection" | "attestation"
  | "prediction" | "hypothesis" | "action" | "thread" | "case"
  | "turn" | "person" | "alias" | "procedure";

const ENTITY_TYPES = new Set([
  "x-host", "ipv4-addr", "ipv6-addr", "user-account", "process", "file",
  "domain-name", "email-addr", "url", "windows-registry-key", "x-registry-key",
  "x-scheduled-task", "x-group", "mac-addr", "software",
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
  }
}

/** Which renderer a target takes. A `relationship--` ref is an alias only when
 * the fetched edge says so; until the doc arrives it renders as evidence. */
export function rendererFor(t: InspectorTarget, doc?: EvidenceDoc | null): Renderer {
  switch (t.kind) {
    case "sop": return "procedure";
    case "turn": return "turn";
    case "action": return "action";
    case "thread": return "thread";
    case "case": return "case";
    case "person": return "person";
    case "ref": {
      const type = refType(t.ref);
      if (type === "x-null-observation") return "absence";
      if (type === "indicator" || type === "sighting") return "detection";
      if (type === "note") return "attestation";
      if (type === "x-prediction") return "prediction";
      if (type === "x-hypothesis") return "hypothesis";
      if (ENTITY_TYPES.has(type)) return "entity";
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
    case "person": return "Person";
    case "alias": return "Alias";
    case "procedure": return "Procedure";
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
          if (p) return (p.ordinal ? "P" + p.ordinal + " · " : "") + (p.title || p.statement);
        }
      }
      return labels[t.ref] || refTypeLabel(t.ref);
    }
    case "sop": {
      const k = snap?.knowledge.find((x) => x.kind === "sop" && x.ref === t.sopId);
      const consulted = snap?.thread?.flatMap((e) => e.consultedSops || []).find((s) => s.sopId === t.sopId);
      return k?.title || consulted?.title || "Procedure";
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
      const k = snap?.knowledge.find((x) => x.kind === "case" && x.ref === t.investigationRef);
      const consulted = snap?.thread?.flatMap((e) => e.consultedSimilar || []).find((s) => s.investigationRef === t.investigationRef);
      return k?.title || consulted?.title || "Similar past case";
    }
    case "person": {
      const e = snap?.thread?.find((x) => x.actor.principal === t.principalId && x.actor.principalName);
      return e?.actor.principalName || t.principalId;
    }
  }
}
