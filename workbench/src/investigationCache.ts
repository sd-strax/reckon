// The host's cache of open investigations (impl §3.1–§3.2): it reads the
// projection slices from the backend, stamps them with one watermark, and
// serializes every read of one investigation so a slower, older read can never
// be posted after a newer one. It knows nothing of VS Code or the webview; the
// investigation document posts what it returns.
import type { BackendClient, ThreadEntry } from "./backend";
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

export class InvestigationCache {
  private readonly watermarks = new Map<string, number>();
  private readonly chains = new Map<string, Promise<unknown>>();
  // Transcripts are immutable once committed: content-addressed, shared
  // across investigations.
  private readonly transcripts = new Map<string, string>();

  constructor(private readonly client: CacheClient, private readonly log: Logger) {}

  /** The highest event sequence posted for the investigation; 0 before any. */
  watermark(id: string): number {
    return this.watermarks.get(id) ?? 0;
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
    const [r, capabilities] = await Promise.all([
      this.read(id, "snapshot", true),
      // A 503 means the capability layer is off; null renders as "unavailable".
      this.client.capabilities().catch(() => null),
    ]);
    return {
      seq: this.advance(id, r),
      investigation: r.investigation,
      hypotheses: r.hypotheses ?? [],
      actions: r.actions ?? null,
      pins: r.pins ?? [],
      comms: r.comms ?? null,
      thread: r.thread ?? null,
      turns: r.turns ?? [],
      capabilities, enablement: null, knowledge: [], derived: null,
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
    const r = await this.read(id, "refresh");
    return { seq: this.advance(id, r), patch: r };
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
      body = (await this.client.transcript(interpretationId)).body;
      this.transcripts.set(interpretationId, body);
    }
    return body;
  }

  /** `strict`: the hypotheses must read too (a snapshot has no copy to fall back on). */
  private async read(id: string, why: string, strict = false): Promise<Read> {
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
    if (actions !== undefined) projection.actions = actions;
    if (pins !== undefined) projection.pins = pins;
    if (comms !== undefined) projection.comms = comms;
    return projection;
  }

  /**
   * The read's sequence: the investigation's last event, or the thread's last
   * entry if the thread was read after a newer event landed. The watermark
   * keeps the highest sequence read.
   */
  private advance(id: string, r: Read): number {
    const last = r.thread && r.thread.length ? r.thread[r.thread.length - 1].sequenceNo : 0;
    const seq = Math.max(r.investigation.lastEventSequence, last);
    if (seq > this.watermark(id)) this.watermarks.set(id, seq);
    return seq;
  }
}
