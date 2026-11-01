// The host's cache of open investigations (impl §3.1–§3.2): it reads the
// projection slices from the backend, stamps them with one watermark, and
// serializes every read of one investigation so a slower, older read can never
// be posted after a newer one. It knows nothing of VS Code or the webview; the
// investigation document posts what it returns.
import type { ActionRow, BackendClient, ThreadEntry } from "./backend";
import type { ChronicleTurn, InvestigationSnapshot } from "./protocol";

/** The backend reads the cache needs — the seam its tests fake. */
export type CacheClient = Pick<
  BackendClient,
  "getInvestigation" | "hypotheses" | "capabilities" | "thread" | "actions" | "pins" | "comms" | "transcript"
>;

/** The event-sourced slices one write can change, re-read together after it. */
export type Projection = Pick<
  InvestigationSnapshot,
  "investigation" | "hypotheses" | "actions" | "pins" | "comms" | "thread" | "turns"
>;

/** A read: the investigation always; the rest when they read. */
type Read = Pick<Projection, "investigation"> & Partial<Projection>;

export interface Logger {
  debug(message: string): void;
}

/** How many of the newest transcript-bearing thread entries carry their bodies. */
const TURN_WINDOW = 40;

/** Slices not read from the event log: their answers carry no sequence. */
export type UnsequencedSlice = "knowledge" | "enablement";

export class InvestigationCache {
  private readonly watermarks = new Map<string, number>();
  private readonly chains = new Map<string, Promise<unknown>>();
  // The latest request per unsequenced slice of each investigation.
  private readonly requests = new Map<string, number>();
  // Action records the inspector holds, per investigation, with the sequence
  // they were read at.
  private readonly records = new Map<string, Map<string, number>>();
  // The actions as last read, per investigation: what a decision is checked against.
  private readonly actions = new Map<string, ActionRow[]>();
  // Transcripts are immutable once committed: content-addressed, shared
  // across investigations.
  private readonly transcripts = new Map<string, string>();
  // Bumped by clear: a read that began before it may not fill the cache after.
  private generation = 0;

  constructor(private readonly client: CacheClient, private readonly log: Logger) {}

  /** The highest event sequence posted for the investigation; 0 before any. */
  watermark(id: string): number {
    return this.watermarks.get(id) ?? 0;
  }

  /**
   * Start a request for an unsequenced slice. The returned check says whether
   * it is still the latest request for that slice when its answer arrives, so
   * a slow earlier answer is discarded instead of overwriting a newer one.
   */
  beginRequest(id: string, slice: UnsequencedSlice): () => boolean {
    const key = `${id}:${slice}`;
    const n = (this.requests.get(key) ?? 0) + 1;
    this.requests.set(key, n);
    return () => this.requests.get(key) === n;
  }

  /** Note that the inspector holds an action record read at sequence `seq`. */
  servedRecord(id: string, actionId: string, seq: number): void {
    let m = this.records.get(id);
    if (!m) {
      m = new Map();
      this.records.set(id, m);
    }
    m.set(actionId, seq);
  }

  /**
   * The held action records read before `seq`, the investigation's newest
   * event. A record changes on events that leave the action's status alone (a
   * re-request, a best-effort reversal), so any newer event re-sends it.
   */
  staleRecords(id: string, seq: number): string[] {
    const m = this.records.get(id);
    return m ? [...m].filter(([, readAt]) => seq > readAt).map(([actionId]) => actionId) : [];
  }

  /** An action as the record last showed it; undefined if no read has carried it. */
  action(id: string, actionId: string): ActionRow | undefined {
    return this.actions.get(id)?.find((a) => a.actionId === actionId);
  }

  /** Drop what is held for an investigation whose panel closed. */
  forget(id: string): void {
    this.records.delete(id);
    this.actions.delete(id);
    this.watermarks.delete(id);
    for (const key of [...this.requests.keys()]) {
      if (key.startsWith(id + ":")) this.requests.delete(key);
    }
    // A read still running writes these when it lands, so they go again after
    // it. An operation chain deletes itself when it settles.
    void this.exclusive(id, async () => {
      this.actions.delete(id);
      this.watermarks.delete(id);
    });
  }

  /**
   * Drop everything: the identity changed, and all of this was read as someone
   * else, the shared transcripts included (adr/0241). Reads still running are
   * not cached when they land.
   */
  clear(): void {
    this.generation++;
    this.watermarks.clear();
    this.chains.clear();
    this.requests.clear();
    this.records.clear();
    this.actions.clear();
    this.transcripts.clear();
  }

  /**
   * Run `op` after every earlier operation on the same investigation has
   * settled. Reads of one investigation therefore complete in the order they
   * were asked for, and each posts state at least as new as the one before.
   */
  exclusive<T>(id: string, op: () => Promise<T>): Promise<T> {
    const prior = this.chains.get(id) ?? Promise.resolve();
    const run = prior.then(op, op);
    const settled = run.then(() => undefined, () => undefined);
    this.chains.set(id, settled);
    void settled.then(() => {
      if (this.chains.get(id) === settled) this.chains.delete(id);
    });
    return run;
  }

  /**
   * The whole snapshot, for a first load or a reload. The investigation and
   * its hypotheses must read; every other slice degrades to its "unavailable"
   * value. The bulkier optional layers (enablement, knowledge, derived views)
   * are not read here.
   */
  async snapshot(id: string): Promise<InvestigationSnapshot> {
    const generation = this.generation;
    const [r, capabilities] = await Promise.all([
      this.read(id, "snapshot", true),
      // A 503 means the capability layer is off; null renders as "unavailable".
      this.client.capabilities().catch(() => null),
    ]);
    return {
      seq: this.advance(id, r, generation),
      investigation: r.investigation,
      hypotheses: r.hypotheses ?? [],
      actions: r.actions ?? null,
      pins: r.pins ?? [],
      comms: r.comms ?? null,
      thread: r.thread ?? null,
      turns: r.turns ?? [],
      capabilities, enablement: null, derived: null,
    };
  }

  /**
   * The event-sourced slices after a write, read in parallel and stamped
   * together. A slice whose read fails is left out, so the webview keeps the
   * copy it holds rather than blanking it; only the investigation must read.
   * Hypotheses are optional here, unlike a snapshot, so a transient failure of
   * that one read cannot hold back the actions a write or a dispatch changed.
   */
  async refresh(id: string): Promise<{ seq: number; patch: Partial<Projection> }> {
    const generation = this.generation;
    const r = await this.read(id, "refresh");
    return { seq: this.advance(id, r, generation), patch: r };
  }

  /**
   * The newest transcript-bearing entries with their committed bodies. A body
   * that fails to load is left out; the webview offers to open it instead.
   */
  async turns(thread: ThreadEntry[]): Promise<ChronicleTurn[]> {
    const recent = thread.filter((e) => e.hasTranscript).slice(-TURN_WINDOW);
    const turns = await Promise.all(recent.map(async (e) => {
      const body = await this.transcript(e.interpretationId).catch(() => null);
      return body === null ? null : { sequenceNo: e.sequenceNo, occurredAt: e.occurredAt, body };
    }));
    return turns.filter((t): t is ChronicleTurn => t !== null);
  }

  /** One committed transcript, from the cache when it has been read before. */
  async transcript(interpretationId: string): Promise<string> {
    let body = this.transcripts.get(interpretationId);
    if (body === undefined) {
      const generation = this.generation;
      body = (await this.client.transcript(interpretationId)).body;
      if (generation === this.generation) this.transcripts.set(interpretationId, body);
    }
    return body;
  }

  /** `strict`: the hypotheses must read too (a snapshot has no copy to fall back on). */
  private async read(id: string, why: string, strict = false): Promise<Read> {
    const generation = this.generation;
    const optional = <T>(what: string, p: Promise<T>): Promise<T | undefined> =>
      p.catch((err: unknown) => {
        this.log.debug(`${why} ${what} ${id}: ${err instanceof Error ? err.message : String(err)}`);
        return undefined;
      });
    const [investigation, hypotheses, thread, actions, pins, comms] = await Promise.all([
      this.client.getInvestigation(id),
      strict ? this.client.hypotheses(id) : optional("hypotheses", this.client.hypotheses(id)),
      optional("thread", this.client.thread(id)),
      optional("actions", this.client.actions(id)),
      optional("pins", this.client.pins(id)),
      optional("comms", this.client.comms(id)),
    ]);
    const projection: Read = { investigation };
    if (hypotheses !== undefined) projection.hypotheses = hypotheses;
    if (thread !== undefined) {
      projection.thread = thread;
      projection.turns = await this.turns(thread);
    }
    if (actions !== undefined) {
      projection.actions = actions;
      if (generation === this.generation) this.actions.set(id, actions);
    }
    if (pins !== undefined) projection.pins = pins;
    if (comms !== undefined) projection.comms = comms;
    return projection;
  }

  /**
   * The read's sequence: the investigation's last event, or the thread's last
   * entry if the thread was read after a newer event landed. The watermark
   * keeps the highest sequence read.
   */
  private advance(id: string, r: Read, generation: number): number {
    const last = r.thread && r.thread.length ? r.thread[r.thread.length - 1].sequenceNo : 0;
    const seq = Math.max(r.investigation.lastEventSequence, last);
    if (generation === this.generation && seq > this.watermark(id)) this.watermarks.set(id, seq);
    return seq;
  }
}
