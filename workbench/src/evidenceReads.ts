// What the document reads beyond the snapshot: evidence documents and their
// labels (cached, since deterministic ids resolve the same everywhere), the
// derived lenses over the thread's citations, and the inspector's one-object
// answers (impl §2.4, §4.1). Failures are not cached; a change of person
// clears everything (adr/0241).
import type * as vscode from "vscode";
import type { Appearance, EventRow, EvidenceDoc, ThreadEntry } from "./backend";
import { buildDerivedViews, type CitingRecords } from "./derivedViews";
import type { DocumentHost } from "./documentHost";
import { deriveEvidenceLabel } from "./evidenceLabel";
import type { InspectorTarget } from "./protocol";
import { SingleFlight } from "./singleFlight";
import { errText } from "./util";

/** How many retrieved events the Story lens is given, and how many sit each side of an anchor. */
const eventsShown = 500;
const aroundEachSide = 5;

export class EvidenceReads {
  constructor(private readonly host: DocumentHost) {}

  // Resolved evidence-ref labels, shared across panels — deterministic ids
  // mean a ref resolves to the same thing everywhere, so cache once. Failures
  // are not cached, and everything here is cleared when the identity changes.
  private readonly labelCache = new Map<string, string>();

  // Full evidence docs, cached for the same reason. Feeds both label resolution
  // and the event-time strip without duplicate fetches.
  private readonly docCache = new Map<string, EvidenceDoc>();

  private readonly docReads = new SingleFlight<EvidenceDoc | null>();

  /** Fetch one evidence doc through the cache (null = unfetchable, this time). */
  private async getDoc(ref: string): Promise<EvidenceDoc | null> {
    const hit = this.docCache.get(ref);
    if (hit) {
      return hit;
    }
    const current = this.host.sameIdentity();
    return this.docReads.run(ref, async () => {
      try {
        const doc = await this.host.client.evidence.get(ref);
        if (current()) {
          this.docCache.set(ref, doc);
        }
        return doc;
      } catch {
        return null;
      }
    });
  }

  /**
   * Resolve a human label for an evidence ref (host · WIN-FILE01, account ·
   * svc_backup) from the evidence endpoint, cached. Posts back only on a hit; a
   * ref that can't be opened keeps its type-only label in the webview, and a
   * failed read says so, so the webview can ask again.
   */
  async resolveLabel(panel: vscode.WebviewPanel, ref: string): Promise<void> {
    let label = this.labelCache.get(ref);
    if (label === undefined) {
      const current = this.host.sameIdentity();
      const doc = await this.getDoc(ref);
      if (!doc) {
        void this.host.post(panel, { type: "evidence.unresolved", ref });
        return;
      }
      label = deriveEvidenceLabel(doc);
      if (current()) {
        this.labelCache.set(ref, label);
      }
    }
    if (label) {
      void this.host.post(panel, { type: "evidence.label", ref, label });
    }
  }

  /**
   * The derived lenses over the thread's citations (the event-time strip and
   * the evidence graph, src/derivedViews.ts), sharing one round of cached
   * evidence fetches — bounded; failures just leave gaps (both are lenses,
   * never load-bearing). Posted as a slice patch.
   */
  async postDerivedViews(panel: vscode.WebviewPanel, thread: ThreadEntry[], seq: number, citing: CitingRecords): Promise<void> {
    const derived = await buildDerivedViews(thread, (ref) => this.getDoc(ref), citing);
    void this.host.post(panel, { type: "state.patch", seq, patch: { derived } });
  }

  /**
   * The events retrieved for the investigation (adr/0092), posted as a slice
   * patch stamped with the sequence they were read for. A backend without the
   * read, or a failed one, leaves the slice as it was: the Story lens then
   * shows the cited events alone.
   */
  async postEvents(id: string, panel: vscode.WebviewPanel, seq: number): Promise<void> {
    const current = this.host.sameIdentity();
    try {
      const page = await this.host.client.evidence.events(id, { limit: eventsShown });
      if (current()) {
        void this.host.post(panel, { type: "state.patch", seq, patch: { events: page.events } });
      }
    } catch (err) {
      this.host.log.debug(`events ${id}: ${errText(err)}`);
    }
  }

  /** The retrieved events on the same entity around one event, for the
   * Evidence inspector's "Around it" (adr/0092); none when the record has none. */
  private async around(id: string, eventId: string): Promise<EventRow[]> {
    try {
      return (await this.host.client.evidence.events(id, { around: eventId, limit: aroundEachSide })).events;
    } catch (err) {
      this.host.log.debug(`around ${eventId}: ${errText(err)}`);
      return [];
    }
  }

  /**
   * Answer an inspector pane (impl §2.4): the webview opened it from the
   * snapshot and asks for what the record holds beyond it — the evidence
   * document with its cross-investigation memory (current investigation
   * excluded: "also seen in" means OTHERS), a procedure, a committed
   * transcript, an action's record, a case summary. A failed fetch posts an
   * honest error for that target; nothing else in the document is affected.
   */
  async answerInspector(id: string, panel: vscode.WebviewPanel, target: InspectorTarget): Promise<void> {
    try {
      switch (target.kind) {
        case "ref": {
          const doc = await this.getDoc(target.ref);
          if (!doc) {
            void this.host.post(panel, { type: "inspector.error", target: target.ref, message: "no persisted record for this ref — it may predate telemetry persistence" });
            return;
          }
          let apps: Appearance[] = [];
          try {
            apps = (await this.host.client.evidence.appearances(target.ref)).filter((a) => a.investigationId !== id);
          } catch (err) {
            this.host.log.debug(`appearances ${target.ref}: ${errText(err)}`);
          }
          const around = doc.kind === "ocsf" ? await this.around(id, target.ref) : [];
          void this.host.post(panel, { type: "inspector.evidence", ref: target.ref, doc, appearances: apps, around });
          return;
        }
        case "call":
          void this.host.post(panel, { type: "inspector.call", callId: target.callId, call: await this.host.client.evidence.call(id, target.callId) });
          return;
        case "sop":
          void this.host.post(panel, { type: "inspector.procedure", sopId: target.sopId, sop: await this.host.client.knowledge.sop(target.sopId) });
          return;
        case "turn": {
          const body = await this.host.cache.transcript(target.interpretationId);
          void this.host.post(panel, { type: "inspector.turn", interpretationId: target.interpretationId, body });
          return;
        }
        case "action": {
          const record = await this.host.client.actions.record(target.actionId);
          this.host.cache.servedRecord(id, target.actionId, this.host.cache.watermark(id));
          void this.host.post(panel, { type: "inspector.action", actionId: target.actionId, record });
          return;
        }
        case "case":
          void this.host.post(panel, { type: "inspector.case", investigationRef: target.investigationRef, summary: await this.host.client.knowledge.caseSummary(target.investigationRef) });
          return;
        default:
          return; // thread, person: rendered from the snapshot alone
      }
    } catch (err) {
      const key = "ref" in target ? target.ref : "sopId" in target ? target.sopId : "interpretationId" in target ? target.interpretationId
        : "actionId" in target ? target.actionId : "investigationRef" in target ? target.investigationRef : "callId" in target ? target.callId : "";
      void this.host.post(panel, { type: "inspector.error", target: key, message: errText(err) });
    }
  }

  /** The person changed: nothing read as the previous one stays. */
  clear(): void {
    this.docCache.clear();
    this.docReads.clear();
    this.labelCache.clear();
  }
}
