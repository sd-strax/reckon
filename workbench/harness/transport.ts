// The harness transport (impl §5.4): answers the webview from a recorded
// snapshot set and echoes every intent, so a click's effect is visible and
// component tests can assert on what the page asked the host for. A stage
// with a `knowledge` slice is the Knowledge document (impl §2.6): it posts
// `knowledge.state` instead of an investigation snapshot and answers the
// document's reads and writes from that slice.
import type { ActionRecord, Appearance, BaselineView, CallDetail, CaseRow, CaseSummaryView, EvidenceDoc, InvestigationSummary, RetrievalRow, RevisionContent, SOPDoc, SOPRevision } from "../src/backend";
import type { ImportRow, InboundMessage, InspectorTarget, InvestigationSnapshot, KnowledgeState, RenderMessage, SearchResults } from "../src/protocol";
import type { WebviewTransport } from "../webview/src/transport";

/** One recorded stage of the reference scenario (harness/record.ts). */
export interface HarnessSnapshot {
  name: string;
  snapshot: InvestigationSnapshot;
  /** Resolved labels for the refs the snapshot cites. */
  labels: Record<string, string>;
  /** Cross-investigation appearances per entity ref (the inspector). */
  appearances?: Record<string, Appearance[]>;
  /** The inspector's records, keyed as the host answers them (impl §4.1). */
  docs?: Record<string, EvidenceDoc>;
  transcripts?: Record<string, string>;
  actionRecords?: Record<string, ActionRecord>;
  sops?: Record<string, SOPDoc>;
  caseSummaries?: Record<string, CaseSummaryView | null>;
  /** The capability calls the stage's events name (adr/0092). */
  calls?: Record<string, CallDetail>;
  /** Entity baselines (adr/0095); an entity absent here answers null. */
  baselines?: Record<string, BaselineView>;
  /** What the investigation retrieved of each procedure and each procedure's lineage (adr/0097). */
  sopRetrievals?: Record<string, RetrievalRow[]>;
  sopRevisions?: Record<string, SOPRevision[]>;
  /** The work queue (08 §8.3) as the host posts it. */
  queue?: { investigations: InvestigationSummary[]; cases: CaseRow[]; caseSource?: string };
}

/** The Knowledge document's recorded state (harness/record.ts --knowledge, or
 * the hand-built set from the demo pack): the corpus the host would post, the
 * answer a search gets, and the inspector's case summaries. */
export interface HarnessKnowledge {
  name: string;
  knowledge: KnowledgeState;
  /** The canned search answer; the query is substituted. */
  search?: Omit<SearchResults, "query">;
  caseSummaries?: Record<string, CaseSummaryView | null>;
  queue?: HarnessSnapshot["queue"];
  /** Each procedure's lineage and the revision bodies a compare can fetch (adr/0097). */
  revisions?: Record<string, SOPRevision[]>;
  revisionBodies?: Record<string, RevisionContent>;
}

export class HarnessTransport implements WebviewTransport {
  private handlers: ((m: RenderMessage) => void)[] = [];
  private state: unknown = undefined;
  readonly intents: InboundMessage[] = [];
  /** The Knowledge document's live copy (writes mutate it like the host's cache). */
  private kdoc: KnowledgeState | null = null;

  constructor(
    private data: HarnessSnapshot | HarnessKnowledge | null,
    private readonly onIntent?: (m: InboundMessage) => void,
  ) {}

  /** Swap the recorded stage (the harness stage picker). */
  load(data: HarnessSnapshot | HarnessKnowledge | null): void {
    this.data = data;
    this.kdoc = null;
    this.postInitial();
  }

  private get inv(): HarnessSnapshot | null {
    return this.data && "snapshot" in this.data ? this.data : null;
  }
  private get know(): HarnessKnowledge | null {
    return this.data && "knowledge" in this.data ? this.data : null;
  }

  send(intent: InboundMessage): void {
    this.intents.push(intent);
    this.onIntent?.(intent);
    if (this.know) {
      this.sendKnowledge(intent);
      return;
    }
    switch (intent.type) {
      case "ready":
      case "refresh":
      case "ui.resync":
        this.postInitial();
        break;
      case "evidence.resolve": {
        const label = this.inv?.labels[intent.ref];
        if (label) this.emit({ type: "evidence.label", ref: intent.ref, label });
        break;
      }
      case "inspector.open":
        this.answerInspector(intent.target);
        break;
      case "queue.get":
        this.postQueue();
        break;
      case "knowledge.search":
        // The sidecar would merge its hits into the session's set; the harness
        // appends one hit, excluded, so the section's behaviour is visible.
        if (this.inv) {
          const items = this.inv.snapshot.knowledge.concat([{ kind: "sop", ref: "sop-search-" + intent.text.replace(/\W+/g, "-").toLowerCase(), title: "Result for “" + intent.text + "”", excerpt: "", score: 0.5, included: false, rationale: "matched your search" }]);
          this.emit({ type: "state.patch", seq: this.inv.snapshot.seq, patch: { knowledge: items } });
        }
        break;
      case "rename":
        this.emit({ type: "renamed", title: intent.title });
        break;
      case "owner.add":
      case "owner.confirm":
      case "owner.waive": {
        // Owner items (adr/0101): the acts mutate the stage's copy and re-post the slice.
        const snap = this.inv?.snapshot;
        if (!snap) break;
        const items = (snap.ownerItems || []).map((i) => ({ ...i }));
        const now = new Date().toISOString();
        if (intent.type === "owner.add") items.push({ id: "owner-" + (items.length + 1), title: intent.title, owner: intent.owner, detail: intent.detail, status: "OPEN", createdBy: "analyst-1", createdByName: "Analyst One", createdByKind: "HUMAN", createdAt: now, updatedAt: now, history: [{ at: now, actor: "analyst-1", actorName: "Analyst One", act: "created" }] });
        else {
          const it = items.find((i) => i.id === intent.id);
          if (it) {
            const act = intent.type === "owner.confirm" ? "confirmed" : "waived";
            Object.assign(it, { status: act.toUpperCase(), confirmedBy: "analyst-1", confirmedByName: "Analyst One", confirmedAt: now, confirmationNote: intent.type === "owner.confirm" ? intent.note : intent.reason, updatedAt: now, history: [...it.history, { at: now, actor: "analyst-1", actorName: "Analyst One", act, note: intent.type === "owner.confirm" ? intent.note : intent.reason }] });
          }
        }
        snap.ownerItems = items;
        this.emit({ type: "state.patch", seq: snap.seq, patch: { ownerItems: items } });
        break;
      }
      case "seed.caseSearch":
        this.emit({ type: "draft.cases", cases: [{ number: "CASE-1001", title: "A case from the queue", status: "New" }] });
        break;
      case "seed.submit":
      case "seed.alert":
      case "seed.case":
        // The real host re-keys the panel and loads the created investigation;
        // the harness only leaves the draft (it has nothing to load).
        this.emit({ type: "seeded" });
        break;
      // Every other intent is an act the real host forwards to the engine;
      // here it is only recorded.
    }
  }

  /** The Knowledge document's host, over the recorded slice: reads answer from
   * it, writes mutate the copy and re-post the slices the host would. */
  private sendKnowledge(intent: InboundMessage): void {
    const k = this.know!;
    const doc = this.kdoc ?? (this.kdoc = structuredClone(k.knowledge));
    const counts = () => ({ ...doc.counts, procedures: doc.sops.filter((s) => s.status !== "retired").length });
    switch (intent.type) {
      case "ready":
      case "refresh":
      case "ui.resync":
        this.postInitial();
        break;
      case "queue.get":
        this.postQueue();
        break;
      case "sop.get": {
        const sop = doc.sops.find((s) => s.id === intent.sopId);
        if (sop) this.emit({ type: "knowledge.state", state: { detail: sop, detailRevisions: k.revisions?.[sop.id] ?? [] } });
        else this.emit({ type: "knowledge.error", message: "procedure not recorded in this harness stage" });
        break;
      }
      case "sop.revision": {
        const body = k.revisionBodies?.[intent.hash];
        if (body) {
          doc.revisionBodies = { ...(doc.revisionBodies || {}), [intent.hash]: body };
          this.emit({ type: "knowledge.state", state: { revisionBodies: doc.revisionBodies } });
        } else {
          this.emit({ type: "knowledge.error", message: "revision not recorded in this harness stage" });
        }
        break;
      }
      case "sop.save": {
        const sop = doc.sops.find((s) => s.id === intent.sopId);
        if (!sop) break;
        Object.assign(sop, { title: intent.title, body: intent.body, tags: intent.tags, recommendation: intent.recommendation, revisionCount: (sop.revisionCount ?? 1) + 1, updatedAt: new Date().toISOString() });
        // A save is a new revision: the lineage grows by one (adr/0097).
        const prior = (doc.detailRevisions && doc.detailRevisions.length ? doc.detailRevisions : k.revisions?.[sop.id] ?? []).map((r) => ({ ...r, supersededAt: r.supersededAt || new Date().toISOString() }));
        doc.detailRevisions = [{ revision: sop.revisionCount!, contentHash: "h-" + sop.revisionCount + "-" + sop.id, title: sop.title, status: sop.status, createdAt: sop.updatedAt!, authorId: sop.authorId, hasOriginal: false }, ...prior];
        this.emit({ type: "knowledge.state", state: { sops: doc.sops, counts: counts(), detail: { ...sop }, detailRevisions: doc.detailRevisions } });
        break;
      }
      case "sop.retire": {
        const sop = doc.sops.find((s) => s.id === intent.sopId);
        if (!sop) break;
        Object.assign(sop, { status: "retired", retiredAt: new Date().toISOString(), retiredReason: intent.reason, retiredBy: "analyst-1", revisionCount: (sop.revisionCount ?? 1) + 1 });
        this.emit({ type: "knowledge.state", state: { sops: doc.sops, counts: counts(), detail: { ...sop } } });
        break;
      }
      case "knowledge.query":
        if (intent.mode === "ask") {
          // Gap §6.23: the frame renders the gap line; nothing is answered.
        } else if (k.search) {
          this.emit({ type: "knowledge.state", state: { results: { ...k.search, query: intent.text } } });
        } else {
          this.emit({ type: "knowledge.state", state: { results: { query: intent.text, procedures: [], cases: [], coverage: { procedures: "EMPTY", cases: "EMPTY" } } } });
        }
        break;
      case "import.choose": {
        const n = doc.importRows.length + 1;
        const row: ImportRow = {
          id: "file:///sops/chosen-" + n + ".md", name: "chosen-" + n + ".md", dir: intent.folder ? "/sops/folder" : "/sops", fromEditor: false, dirty: false, checked: true,
          preview: { title: "Chosen document " + n, tags: [], source: { system: "file", url: "file:/sops/chosen-" + n + ".md" }, sourceDefaulted: true, missing: ["author", "tags", "source_url"], outcome: "added" },
        };
        doc.importRows.push(row);
        this.emit({ type: "knowledge.state", state: { importRows: doc.importRows } });
        break;
      }
      case "import.toggle": {
        const row = doc.importRows.find((r) => r.id === intent.id);
        if (row) row.checked = intent.checked;
        this.emit({ type: "knowledge.state", state: { importRows: doc.importRows } });
        break;
      }
      case "import.run":
        for (const r of doc.importRows.filter((x) => x.checked)) {
          if (r.previewError) r.result = { outcome: "failed", title: r.name, message: r.previewError };
          else { r.result = { outcome: r.preview?.outcome === "revised" ? "revised" : "added", title: r.preview?.title || r.name }; r.checked = false; }
        }
        this.emit({ type: "knowledge.state", state: { importRows: doc.importRows, importing: false, counts: counts() } });
        break;
      case "import.refresh":
        this.emit({ type: "knowledge.state", state: { importRows: doc.importRows } });
        break;
      case "cases.list":
        this.emit({ type: "knowledge.error", message: "Listing past cases lands with gap 00 §6.21." });
        break;
      case "inspector.open":
        if (intent.target.kind === "sop") {
          const sop = doc.sops.find((s) => s.id === (intent.target as { sopId: string }).sopId);
          if (sop) this.emit({ type: "inspector.procedure", sopId: sop.id, sop });
          else this.emit({ type: "inspector.error", target: (intent.target as { sopId: string }).sopId, message: "procedure not recorded in this harness stage" });
        } else if (intent.target.kind === "case") {
          this.emit({ type: "inspector.case", investigationRef: intent.target.investigationRef, summary: k.caseSummaries?.[intent.target.investigationRef] ?? null });
        }
        break;
      // Everything else (copy, investigation.open, queue.start) is only recorded.
    }
  }

  /** The inspector's answers come from the recorded maps; a record the stage
   * did not capture is an honest error, as the host would post for a ref it
   * cannot open. */
  private answerInspector(target: InspectorTarget): void {
    const d = this.inv;
    switch (target.kind) {
      case "ref": {
        const doc = d?.docs?.[target.ref];
        if (doc) this.emit({ type: "inspector.evidence", ref: target.ref, doc, appearances: d?.appearances?.[target.ref] ?? [] });
        else this.emit({ type: "inspector.error", target: target.ref, message: "not recorded in this harness stage" });
        // The baseline (adr/0095) is a read over the investigation's events, independent of the object's own record.
        if (!target.ref.startsWith("observed-data--") && target.ref.includes("--")) this.emit({ type: "inspector.baseline", ref: target.ref, baseline: d?.baselines?.[target.ref] ?? null });
        break;
      }
      case "sop": {
        const sop = d?.sops?.[target.sopId];
        if (sop) this.emit({ type: "inspector.procedure", sopId: target.sopId, sop, retrievals: d?.sopRetrievals?.[target.sopId] ?? [], revisions: d?.sopRevisions?.[target.sopId] ?? [] });
        else this.emit({ type: "inspector.error", target: target.sopId, message: "procedure not recorded in this harness stage" });
        break;
      }
      case "turn": {
        const body = d?.transcripts?.[target.interpretationId] ?? d?.snapshot.turns.find((t) => d.snapshot.thread?.find((e) => e.interpretationId === target.interpretationId)?.sequenceNo === t.sequenceNo)?.body;
        if (body !== undefined) this.emit({ type: "inspector.turn", interpretationId: target.interpretationId, body });
        else this.emit({ type: "inspector.error", target: target.interpretationId, message: "transcript not recorded in this harness stage" });
        break;
      }
      case "action": {
        const record = d?.actionRecords?.[target.actionId];
        if (record) this.emit({ type: "inspector.action", actionId: target.actionId, record });
        else this.emit({ type: "inspector.error", target: target.actionId, message: "action record not recorded in this harness stage" });
        break;
      }
      case "case":
        this.emit({ type: "inspector.case", investigationRef: target.investigationRef, summary: d?.caseSummaries?.[target.investigationRef] ?? null });
        break;
      case "call": {
        const call = d?.calls?.[target.callId];
        if (call) this.emit({ type: "inspector.call", callId: target.callId, call });
        else this.emit({ type: "inspector.error", target: target.callId, message: "call not recorded in this harness stage" });
        break;
      }
      // thread, person: rendered from the snapshot alone
    }
  }

  /** Push a host message as if the extension had posted it (tests, the stage picker). */
  emit(m: RenderMessage): void {
    for (const h of this.handlers) h(m);
  }

  onMessage(handler: (m: RenderMessage) => void): () => void {
    this.handlers.push(handler);
    return () => { this.handlers = this.handlers.filter((h) => h !== handler); };
  }
  getState<T>(): T | undefined { return this.state as T | undefined; }
  setState(s: unknown): void { this.state = s; }

  private postInitial(): void {
    if (this.know) {
      const doc = this.kdoc ?? (this.kdoc = structuredClone(this.know.knowledge));
      this.emit({ type: "knowledge.state", state: doc });
      this.emit({ type: "me", name: "Analyst One", principal: "analyst-1" });
      return;
    }
    if (!this.inv) {
      this.emit({ type: "draft", caseQueue: "soc-triage" });
      return;
    }
    this.emit({ type: "state.snapshot", seq: this.inv.snapshot.seq, snapshot: this.inv.snapshot });
    this.postQueue();
    this.emit({ type: "me", name: "Analyst One", principal: "analyst-1" });
  }

  private postQueue(): void {
    const q = this.data?.queue;
    if (q) this.emit({ type: "queue", investigations: q.investigations, cases: q.cases, caseSource: q.caseSource });
  }
}
