// What the document reads beyond the snapshot: evidence documents and their
// labels (cached, since deterministic ids resolve the same everywhere), the
// derived lenses over the thread's citations, and the inspector's one-object
// answers (impl §2.4, §4.1). Failures are not cached; a change of person
// clears everything (adr/0241).
import type * as vscode from "vscode";
import type { Appearance, EventRepeats, EventRow, EvidenceDoc, IdentityViews, ThreadEntry } from "./backend";
import { BackendRequestError } from "./client/http";
import { buildDerivedViews, liveAliases, type CitingRecords } from "./derivedViews";
import type { DocumentHost } from "./documentHost";
import { deriveEvidenceLabel } from "./evidenceLabel";
import { bearsIdentity, identityKindOf, identityViewKeys, identityWord, isPersonDoc } from "./identityKinds";
import type { IdentityOther, InspectorTarget, KnownAccount, RenderMessage } from "./protocol";
import { SingleFlight } from "./singleFlight";
import { caseAnswer, procedureAnswer } from "./knowledgeAnswers";
import { errText } from "./util";

/** How many retrieved events the Story lens is given, and how many sit each side of an anchor. */
const eventsShown = 500;
const aroundEachSide = 5;

/** The id an inspector target is answered (and its error posted) under. */
function targetKey(target: InspectorTarget): string {
  return "ref" in target ? target.ref : "sopId" in target ? target.sopId : "interpretationId" in target ? target.interpretationId
    : "actionId" in target ? target.actionId : "investigationRef" in target ? target.investigationRef : "callId" in target ? target.callId : "";
}

/**
 * An identity act's refusal for its form: the server's own message, verbatim,
 * and, when a hint named several accounts or persons (409), the candidates,
 * for the analyst to pick one.
 */
function refusal(ref: string, requestId: string, err: unknown): Extract<RenderMessage, { type: "identity.error" }> {
  const out: Extract<RenderMessage, { type: "identity.error" }> = { type: "identity.error", ref, requestId, message: errText(err) };
  if (!(err instanceof BackendRequestError) || !err.body) return out;
  const body = err.body;
  if (typeof body.error === "string" && body.error) out.message = body.error;
  if (err.status === 409 && Array.isArray(body.accounts)) {
    out.accounts = body.accounts.flatMap((a): KnownAccount[] => {
      const r = a as Record<string, unknown>;
      return typeof r.ref === "string" ? [{ ref: r.ref, accountType: String(r.account_type ?? ""), accountLogin: String(r.account_login ?? "") }] : [];
    });
  }
  if (err.status === 409 && Array.isArray(body.persons)) {
    out.persons = body.persons.filter((p): p is string => typeof p === "string");
  }
  return out;
}

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

  // An identity view, a host's, an account's or a person's, is mutable
  // (adr/0211, adr/0324): it is never cached, and every act bumps this, so a
  // read that began before an act moved an identity is dropped rather than
  // posted.
  private identityGeneration = 0;

  // What each panel last derived its views from (adr/0352), for an identity act to derive them again.
  private derivedInputs = new WeakMap<vscode.WebviewPanel, { thread: ThreadEntry[]; seq: number; citing: CitingRecords }>();

  // The entities with an identity act in flight, from any panel: one at a time.
  private readonly actsInFlight = new Set<string>();

  // The last inspector read issued per panel and target: only it may post.
  private readonly readSeqs = new WeakMap<vscode.WebviewPanel, Map<string, number>>();

  /** The immutable part of an evidence doc: everything but its identity view, of any kind. */
  private static immutable(doc: EvidenceDoc): EvidenceDoc {
    const held = { ...doc };
    for (const key of identityViewKeys) delete held[key];
    return held;
  }

  /**
   * Whether an inspector open of ref reads its doc fresh: a host, an account
   * or a person, whose identity view is mutable. An `identity--` ref is read
   * fresh until a held doc says it is a vendor's, which carries no view.
   */
  private readsFresh(ref: string): boolean {
    if (!bearsIdentity(ref)) return false;
    const held = this.docCache.get(ref);
    return !(identityKindOf(ref) === "person" && held && !isPersonDoc(held));
  }

  /**
   * Fetch one evidence doc through the cache (null = unfetchable, this time).
   * The cache holds the immutable doc only: an identity view is read by
   * identityDoc, fresh.
   */
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
          this.docCache.set(ref, EvidenceReads.immutable(doc));
        }
        return doc;
      } catch {
        return null;
      }
    });
  }

  /**
   * An identity-bearing evidence doc read fresh, its identity view current.
   * Null when it cannot be read, and "stale" when an identity act began while
   * it was in flight: then nothing is cached and nothing is to be posted.
   */
  private async identityDoc(ref: string, latest: () => boolean): Promise<EvidenceDoc | null | "stale"> {
    const current = this.host.sameIdentity();
    const generation = this.identityGeneration;
    let doc: EvidenceDoc;
    try {
      doc = await this.host.client.evidence.get(ref);
    } catch {
      return generation === this.identityGeneration ? null : "stale";
    }
    if (generation !== this.identityGeneration) {
      return "stale";
    }
    // A newer read of this target writes, not this one.
    if (current() && latest()) {
      this.docCache.set(ref, EvidenceReads.immutable(doc));
    }
    return doc;
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
    this.derivedInputs.set(panel, { thread, seq, citing });
    const current = this.host.sameIdentity();
    const generation = this.identityGeneration;
    // An account's aliases are read fresh: the held doc keeps no identity view (adr/0352).
    const derived = await buildDerivedViews(thread, (ref) => this.getDoc(ref), citing, async (ref) => liveAliases(await this.host.client.evidence.get(ref).catch(() => null)));
    // An identity act landed meanwhile: the aliases may be from before it, and
    // the act's own re-derive posts instead.
    if (!current() || generation !== this.identityGeneration) return;
    void this.host.post(panel, { type: "state.patch", seq, patch: { derived } });
  }

  /**
   * After an identity act, every open investigation derives its views again
   * from the record it last derived them from (adr/0352): which pinned
   * accounts the record joins can have changed with no thread change.
   */
  private rederiveOpen(): Promise<void[]> {
    return Promise.all(this.host.openPanels().flatMap((p) => {
      const last = this.derivedInputs.get(p);
      return last ? [this.postDerivedViews(p, last.thread, last.seq, last.citing)] : [];
    }));
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
   *
   * For one target in one panel the latest read issued wins: each read takes
   * the next sequence number for (panel, target), and an older one's
   * completion, an answer or an error, is dropped without a cache write (the
   * newer read is in flight and will post). The webview's store is per
   * panel, so the order is too.
   */
  async answerInspector(id: string, panel: vscode.WebviewPanel, target: InspectorTarget): Promise<void> {
    let seqs = this.readSeqs.get(panel);
    if (!seqs) {
      seqs = new Map();
      this.readSeqs.set(panel, seqs);
    }
    const key = target.kind + ":" + targetKey(target).toLowerCase();
    const seq = (seqs.get(key) ?? 0) + 1;
    seqs.set(key, seq);
    const order = seqs;
    await this.answer(id, panel, target, () => order.get(key) === seq);
  }

  private async answer(id: string, panel: vscode.WebviewPanel, target: InspectorTarget, latest: () => boolean): Promise<void> {
    try {
      switch (target.kind) {
        case "ref": {
          // An identity view is read fresh on every open (adr/0211,
          // adr/0324). A read an identity act overtook is read again: the act
          // may have changed this entity's view though the act was on another
          // entity or in another panel, where no newer read of this target
          // was issued.
          const fresh = this.readsFresh(target.ref);
          const generation = this.identityGeneration;
          const read = fresh ? await this.identityDoc(target.ref, latest) : await this.getDoc(target.ref);
          if (!latest()) {
            return;
          }
          if (read === "stale") {
            return this.answerInspector(id, panel, target);
          }
          const doc = read;
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
          if (!latest()) {
            return;
          }
          if (fresh && generation !== this.identityGeneration) {
            return this.answerInspector(id, panel, target); // an act moved the identity while the rest was read
          }
          void this.host.post(panel, { type: "inspector.evidence", ref: target.ref, doc, appearances: apps, around, repeats });
          return;
        }
        case "call": {
          const call = await this.host.client.evidence.call(id, target.callId);
          if (latest()) void this.host.post(panel, { type: "inspector.call", callId: target.callId, call });
          return;
        }
        case "sop": {
          // The reads start from the committed thread, not from anything the
          // webview holds (adr/0097).
          const answer = await procedureAnswer(this.host, target.sopId, () => this.host.client.investigations.thread(id));
          if (latest()) void this.host.post(panel, answer);
          return;
        }
        case "turn": {
          const body = await this.host.cache.transcript(target.interpretationId);
          if (latest()) void this.host.post(panel, { type: "inspector.turn", interpretationId: target.interpretationId, body });
          return;
        }
        case "action": {
          const record = await this.host.client.actions.record(target.actionId);
          if (!latest()) return;
          this.host.cache.servedRecord(id, target.actionId, this.host.cache.watermark(id));
          void this.host.post(panel, { type: "inspector.action", actionId: target.actionId, record });
          return;
        }
        case "case": {
          const answer = await caseAnswer(this.host, target.investigationRef);
          if (latest()) void this.host.post(panel, answer);
          return;
        }
        default:
          return; // thread, person: rendered from the snapshot alone
      }
    } catch (err) {
      if (latest()) void this.host.post(panel, { type: "inspector.error", target: targetKey(target), message: errText(err) });
    }
  }

  /**
   * The analyst's "same as" / "not the same" on the host, account or person
   * at ref (adr/0211, adr/0324), or "this account is not this person's".
   * The outcome is posted under the act's request id: ref's view after it
   * (null when the server could not read it: the last view stands), or a
   * refusal for the inline form to say. The pane is then answered again
   * from a fresh read, since other investigations and repeated observations
   * follow the alias set too.
   */
  async assertIdentity(id: string, panel: vscode.WebviewPanel, msg: { ref: string; requestId: string; relation: "aliases" | "distinct"; other: string | IdentityOther; note?: string }): Promise<void> {
    await this.identityAct(id, panel, msg.ref, msg.requestId, () => this.host.client.evidence.assertIdentity(msg.ref, msg.relation, msg.other, msg.note));
  }

  /** The account at ref belongs to the person named (adr/0324), answering the account's view. */
  async assertOwner(id: string, panel: vscode.WebviewPanel, msg: { ref: string; requestId: string; person: { ref: string } | { hint: string }; note?: string }): Promise<void> {
    const person = "ref" in msg.person ? { personRef: msg.person.ref } : { personHint: msg.person.hint };
    await this.identityAct(id, panel, msg.ref, msg.requestId, () => this.host.client.evidence.assertOwner(msg.ref, person, msg.note));
  }

  /** Withdraw an asserted identity edge (adr/0211), answering the view of ref, one of its ends. */
  async withdrawIdentity(id: string, panel: vscode.WebviewPanel, msg: { ref: string; requestId: string; edgeId: string; note?: string }): Promise<void> {
    await this.identityAct(id, panel, msg.ref, msg.requestId, () => this.host.client.evidence.withdrawIdentity(msg.edgeId, msg.ref, msg.note));
  }

  private async identityAct(id: string, panel: vscode.WebviewPanel, ref: string, requestId: string, act: () => Promise<IdentityViews | null>): Promise<void> {
    // One act in flight per entity across every panel: a second is refused.
    const key = ref.toLowerCase();
    if (this.actsInFlight.has(key)) {
      void this.host.post(panel, { type: "identity.error", ref, requestId, message: `an act on this ${identityWord(ref)} is in flight` });
      return;
    }
    this.actsInFlight.add(key);
    // The lock is released the moment the act settles, before its outcome is
    // posted (the webview may send the next act at once); the re-read after
    // it is guarded by the generation, not the lock. The finally is only a
    // safety net. Release is one-shot: once this act let the lock go, a later
    // act on the host may hold it, and this act's finally must not free it.
    let held = true;
    const release = () => {
      if (held) {
        held = false;
        this.actsInFlight.delete(key);
      }
    };
    try {
      await this.runIdentityAct(id, panel, ref, requestId, act, release);
    } finally {
      release();
    }
  }

  private async runIdentityAct(id: string, panel: vscode.WebviewPanel, ref: string, requestId: string, act: () => Promise<IdentityViews | null>, release: () => void): Promise<void> {
    const current = this.host.sameIdentity();
    // Reads in flight now, or begun while the act is, may hold the view from
    // before it: the generation moves at both ends of the act.
    this.identityGeneration++;
    let views: IdentityViews | null;
    try {
      views = await act();
    } catch (err) {
      this.identityGeneration++;
      release();
      void this.host.post(panel, refusal(ref, requestId, err));
      return;
    }
    this.identityGeneration++;
    release();
    if (!current()) {
      // The answer was the previous person's: drop it, but settle the form.
      void this.host.post(panel, { type: "identity.error", ref, requestId, message: `the signed-in person changed while this was in flight; open the ${identityWord(ref)} again to see where it stands` });
      return;
    }
    void this.host.post(panel, { type: "inspector.identity", ref, requestId, views });
    // Every open investigation, this one too, may hold entities whose view
    // this act changed (the alias closure reaches beyond the edge): each
    // re-reads the one it shows. This panel's view of ref came with the
    // answer, and the read below refreshes the rest of its pane.
    void this.host.post(panel, { type: "identity.changed", ref, acted: true });
    for (const other of this.host.openPanels()) {
      if (other !== panel) void this.host.post(other, { type: "identity.changed", ref });
    }
    await Promise.all([this.answerInspector(id, panel, { kind: "ref", ref }), this.rederiveOpen()]);
  }

  /** The person changed: nothing read as the previous one stays. */
  clear(): void {
    this.pages.clear();
    this.generation.clear();
    this.docCache.clear();
    this.docReads.clear();
    this.labelCache.clear();
    this.derivedInputs = new WeakMap();
  }
}
