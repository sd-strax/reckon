// What the document reads beyond the snapshot: evidence documents and their
// labels (cached, since deterministic ids resolve the same everywhere), the
// derived lenses over the thread's citations, and the inspector's one-object
// answers (impl §2.4, §4.1). Failures are not cached; a change of person
// clears everything (adr/0241).
import type * as vscode from "vscode";
import type { Appearance, EventRepeats, EventRow, EvidenceDoc, HostIdentity, ThreadEntry } from "./backend";
import { BackendRequestError } from "./client/http";
import { buildDerivedViews, type CitingRecords } from "./derivedViews";
import type { DocumentHost } from "./documentHost";
import { deriveEvidenceLabel } from "./evidenceLabel";
import type { InspectorTarget } from "./protocol";
import { SingleFlight } from "./singleFlight";
import { caseAnswer, procedureAnswer } from "./knowledgeAnswers";
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

  // The retrieved events posted so far per investigation, newest first, with
  // the cursor of the next older page: "Show older" appends to it. Every read
  // of the slice takes a generation, and a result that is no longer current
  // (a newer read started while it was in flight) is dropped, so the pages
  // here never fall behind what the webview holds.
  private readonly pages = new Map<string, { events: EventRow[]; total: number; cursor?: string }>();
  private readonly generation = new Map<string, number>();

  // The fold each investigation's timeline is in (adr/0095): a key spec, or
  // null for the flat page. Starts at the default key.
  private readonly fold = new Map<string, string | null>();

  private nextGeneration(id: string): () => boolean {
    const g = (this.generation.get(id) ?? 0) + 1;
    this.generation.set(id, g);
    return () => this.generation.get(id) === g;
  }

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
   * patch stamped with the sequence they were read for. By default the read is
   * folded (adr/0095): the newest events grouped by the key per class
   * (`keys`, "default" or the analyst's choice), with what each class can key
   * on. With `keys` null the read is the flat newest page with the cursor of
   * the next older one. A failed read is posted too, as `eventsError`, so the
   * Story lens says the record could not be read rather than showing the cited
   * events alone, which is what a record without membership looks like.
   */
  async postEvents(id: string, panel: vscode.WebviewPanel, seq: number, keys?: string | null): Promise<void> {
    // A re-read of the record keeps the fold the timeline is in.
    if (keys === undefined) {
      const held = this.fold.get(id); // null is a choice (flat), undefined is none yet
      keys = held === undefined ? "default" : held;
    }
    this.fold.set(id, keys);
    const current = this.host.sameIdentity();
    const latest = this.nextGeneration(id);
    try {
      if (keys === null) {
        const page = await this.host.client.evidence.events(id, { limit: eventsShown, order: "newest" });
        if (current() && latest()) {
          this.pages.set(id, { events: page.events, total: page.total, cursor: page.nextCursor });
          void this.host.post(panel, { type: "state.patch", seq, patch: { events: page.events, eventsTotal: page.total, eventsError: null, eventsCursor: page.nextCursor ?? null, eventGroups: null, eventKeys: [], eventsFoldedOver: 0 } });
        }
        return;
      }
      const page = await this.host.client.evidence.events(id, { groupBy: keys });
      if (current() && latest()) {
        this.pages.set(id, { events: page.events, total: page.total });
        void this.host.post(panel, { type: "state.patch", seq, patch: { events: page.events, eventsTotal: page.total, eventsError: null, eventsCursor: null, eventGroups: page.groups ?? [], eventKeys: page.keys ?? [], eventsFoldedOver: page.foldedOver ?? page.events.length } });
      }
    } catch (err) {
      // A retired read's failure is not the record's state either.
      if (current() && latest()) {
        void this.host.post(panel, { type: "state.patch", seq, patch: { eventsError: errText(err) } });
      }
    }
  }

  /**
   * The next older page of retrieved events, appended to what the timeline
   * already holds (adr/0092). A cursor that is not the one last posted is a
   * repeat or a stale click and is ignored.
   */
  async postOlderEvents(id: string, panel: vscode.WebviewPanel, seq: number, cursor: string): Promise<void> {
    const held = this.pages.get(id);
    if (!held || held.cursor !== cursor) {
      return;
    }
    const current = this.host.sameIdentity();
    const latest = this.nextGeneration(id);
    try {
      const page = await this.host.client.evidence.events(id, { limit: eventsShown, order: "newest", cursor });
      if (current() && latest()) {
        const next = { events: [...held.events, ...page.events], total: page.total, cursor: page.nextCursor };
        this.pages.set(id, next);
        void this.host.post(panel, { type: "state.patch", seq, patch: { events: next.events, eventsTotal: next.total, eventsError: null, eventsCursor: next.cursor ?? null } });
      }
    } catch (err) {
      if (current() && latest()) {
        void this.host.post(panel, { type: "state.patch", seq, patch: { eventsError: errText(err) } });
      }
    }
  }

  /** The retrieved events on the same entity around one event, for the
   * Evidence inspector's "Around it" (adr/0092): empty when the record has
   * none, and empty too when the event is not a member of the investigation
   * (a cited event persisted before membership: a 404 here). Any other
   * failure throws, and the pane shows the error; an empty window means the
   * record was read and held nothing. */
  private async around(id: string, eventId: string): Promise<EventRow[]> {
    try {
      return (await this.host.client.evidence.events(id, { around: eventId, limit: aroundEachSide })).events;
    } catch (err) {
      if (err instanceof BackendRequestError && err.status === 404) {
        return [];
      }
      throw err;
    }
  }

  /**
   * One entity's retrieved events folded by the default key (adr/0095), for the
   * Entity inspector's "Repeated observations". A failed read leaves the
   * section out rather than failing the pane: the fold is a lens over the
   * record, never load-bearing.
   */
  private async repeats(id: string, ref: string): Promise<EventRepeats | undefined> {
    try {
      const page = await this.host.client.evidence.events(id, { entity: ref, groupBy: "default" });
      return { events: page.events, groups: page.groups ?? [], total: page.total, foldedOver: page.foldedOver ?? page.events.length };
    } catch (err) {
      this.host.log.debug(`repeats ${ref}: ${errText(err)}`);
      return undefined;
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
          const repeats = doc.kind === "ocsf" ? undefined : await this.repeats(id, target.ref);
          void this.host.post(panel, { type: "inspector.evidence", ref: target.ref, doc, appearances: apps, around, repeats });
          return;
        }
        case "call":
          void this.host.post(panel, { type: "inspector.call", callId: target.callId, call: await this.host.client.evidence.call(id, target.callId) });
          return;
        case "sop": {
          // The reads start from the committed thread, not from anything the
          // webview holds (adr/0097).
          void this.host.post(panel, await procedureAnswer(this.host, target.sopId, () => this.host.client.investigations.thread(id)));
          return;
        }
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
        case "case": {
          void this.host.post(panel, await caseAnswer(this.host, target.investigationRef));
          return;
        }
        default:
          return; // thread, person: rendered from the snapshot alone
      }
    } catch (err) {
      const key = "ref" in target ? target.ref : "sopId" in target ? target.sopId : "interpretationId" in target ? target.interpretationId
        : "actionId" in target ? target.actionId : "investigationRef" in target ? target.investigationRef : "callId" in target ? target.callId : "";
      void this.host.post(panel, { type: "inspector.error", target: key, message: errText(err) });
    }
  }

  /**
   * The analyst's "same as another host" / "not the same host" on the host in
   * view (adr/0211). The answer is the host's refreshed identity view, posted
   * for the pane to re-render; the rest of the pane (other investigations,
   * repeated observations) follows the alias set, so it is answered again. A
   * refusal is posted for the inline form to say.
   */
  async assertIdentity(id: string, panel: vscode.WebviewPanel, msg: { ref: string; relation: "aliases" | "distinct"; other: string; note?: string }): Promise<void> {
    await this.identityAct(id, panel, msg.ref, () => this.host.client.evidence.assertIdentity(msg.ref, msg.relation, msg.other, msg.note));
  }

  /** Withdraw an asserted identity edge (adr/0211), answering the view of the host in view. */
  async withdrawIdentity(id: string, panel: vscode.WebviewPanel, msg: { ref: string; edgeId: string; note?: string }): Promise<void> {
    await this.identityAct(id, panel, msg.ref, () => this.host.client.evidence.withdrawIdentity(msg.edgeId, msg.ref, msg.note));
  }

  private async identityAct(id: string, panel: vscode.WebviewPanel, ref: string, act: () => Promise<HostIdentity>): Promise<void> {
    const current = this.host.sameIdentity();
    let identity: HostIdentity;
    try {
      identity = await act();
    } catch (err) {
      void this.host.post(panel, { type: "identity.error", ref, message: errText(err) });
      return;
    }
    if (!current()) {
      // The answer was the previous person's: drop it, but settle the form.
      void this.host.post(panel, { type: "identity.error", ref, message: "the signed-in person changed while this was in flight; open the host again to see where it stands" });
      return;
    }
    // The alias closure can change beyond the edge's two ends: every host's
    // view is read afresh when next opened.
    const held = this.docCache.get(ref);
    for (const key of [...this.docCache.keys()]) {
      if (key.startsWith("x-host--")) this.docCache.delete(key);
    }
    if (held) this.docCache.set(ref, { ...held, identity });
    void this.host.post(panel, { type: "inspector.identity", ref, identity });
    await this.answerInspector(id, panel, { kind: "ref", ref });
  }

  /** The person changed: nothing read as the previous one stays. */
  clear(): void {
    this.pages.clear();
    this.generation.clear();
    this.docCache.clear();
    this.docReads.clear();
    this.labelCache.clear();
  }
}
