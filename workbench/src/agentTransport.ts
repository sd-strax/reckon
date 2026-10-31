// The AgentTransport seam (implementation/agent-sidecar.md §7 step 1) and its
// sidecar implementation (§7 step 3): the extension reaches the ONE agent-loop
// implementation — the Go `agent` package — by spawning `reckon investigate
// --stdio` and speaking LSP-framed JSON-RPC over its stdio (vscode-jsonrpc).
//
// Auth stays here, on the extension side (§5): the sidecar holds no refresh
// tokens; whenever the loop needs a bearer token it calls the getToken RPC
// back into this process, which answers from auth.Session — short-lived
// access tokens, per call. The BYOK Anthropic key crosses once, at
// initialize, over the local pipe.
//
// Process lifecycle: this owns spawn/respawn. A sidecar crash never takes the
// extension down — sessions are re-created from backend state on the next
// turn (the reasoning thread is server-persisted; only in-flight turn context
// was process-local).

import { ChildProcess, spawn } from "child_process";
import * as vscode from "vscode";
import {
  createMessageConnection,
  MessageConnection,
  StreamMessageReader,
  StreamMessageWriter,
} from "vscode-jsonrpc/node";
import { TokenKind } from "./auth";
import { SingleFlight } from "./singleFlight";

/** The stdio protocol contract version (sidecar.ProtocolVersion). */
const PROTOCOL_VERSION = 1;

/** Progress callbacks one turn renders. */
export interface TurnProgress {
  /** Round-complete text — fires only when the provider cannot stream. */
  onText(text: string): void;
  /**
   * A text fragment as the model generates it (E.4). Mutually exclusive with
   * onText per completion (enforced sidecar-side): a streamed completion's
   * text arrives ONLY as deltas. Renderers append both the same way.
   */
  onTextDelta(text: string): void;
  /** A new model↔tool round began (1-based) — the step marker. */
  onStep(round: number): void;
  onToolCall(name: string, input: unknown): void;
  /**
   * coverage/eventCount/refs are distilled sidecar-side from the FULL result
   * payload (content is clipped for transport and may not parse). Undefined
   * when the result is not a capability envelope (e.g. list_actions). refs
   * are the envelope's citation ids — what pin-from-result and citation-open
   * act on.
   */
  onToolResult(name: string, content: string, isError: boolean, coverage?: string, eventCount?: number, refs?: string[]): void;
  /** The history was condensed to fit the model's context window (adr/0230): what the model no longer sees. */
  onContextCompacted?(note: string): void;
}

/** One action awaiting the analyst (mirrors sidecar.pendingAction). */
export interface PendingAction {
  actionId: string;
  actionType: string;
  tier: string;
  status: string;
  targets: string[];
  /** Approval window elapsed — the engine refuses an approve; offer none. */
  expired: boolean;
}

/** What one turn produced (mirrors sidecar.turnResult). */
export interface TurnOutcome {
  text: string;
  interpretationId?: string;
  toolRounds: number;
  usage: { input: number; output: number; cacheRead: number; cacheWrite: number };
  pendingActions: PendingAction[];
  /** A turn that failed part-way still surfaces its partial outcome. */
  error?: string;
  /** Something the analyst should read about a turn that did not fail: it is on the record. */
  notice?: string;
}

/** A candidate SOP the model generalized from a concluded investigation
 * (design/06: the compounding loop). Not persisted — the analyst reviews it and
 * decides to add it to the library or discard. warranted=false means the model
 * judged no new procedure worth capturing (rationale says why). */
export interface SOPCandidate {
  warranted: boolean;
  title: string;
  body: string;
  tags: string[];
  recommendation: string;
  rationale: string;
}

/**
 * The seam the investigation document drives. Sessions are keyed by
 * investigation id; the transport owns their lifecycle (including re-creation
 * after a sidecar respawn).
 */
export interface AgentTransport {
  turn(investigationId: string, text: string, progress: TurnProgress): Promise<TurnOutcome>;
  /** Whether a turn is underway on the investigation; one runs at a time. */
  turnRunning(investigationId: string): boolean;
  cancel(investigationId: string): Promise<void>;
  /** End the investigation's session, stopping what runs on it (adr/0237). */
  closeSession(investigationId: string): Promise<void>;
  /** Client-side knowledge-summary narrative at conclusion (design/06 §3.2). */
  summarizeConcluded(investigationId: string): Promise<void>;
  /** Client-side candidate-SOP generalization at conclusion (design/06: the
   * compounding loop). Returns a draft for the analyst to accept or discard. */
  proposeSOP(investigationId: string, force?: boolean): Promise<SOPCandidate>;
  dispose(): void;
}

/** What the transport needs from the extension host. */
export interface SidecarDeps {
  backendUrl(): string;
  model(): string;
  /** KB one tool result may reach before the model reads it a page at a time; 0 = the agent default. */
  maxToolResultKB(): number;
  /** The model's context window in tokens; 0 = the agent default (adr/0230). */
  contextWindowTokens(): number;
  /** How long a turn may run before it is stopped (adr/0239). */
  turnTimeoutMinutes(): number;
  /** The BYOK key from SecretStorage; undefined lets the sidecar try its own fallback (env / OS keychain). */
  apiKey(): Thenable<string | undefined>;
  /** Short-lived access tokens from the auth session — the getToken answer. */
  getToken(kind: TokenKind, force: boolean): Promise<string>;
}

/** Wire shapes (snake_case, mirroring the Go structs). */
interface WireTurnResult {
  text?: string;
  interpretation_id?: string;
  tool_rounds?: number;
  usage?: { input?: number; output?: number; cache_read?: number; cache_write?: number };
  pending_actions?: {
    action_id?: string;
    action_type?: string;
    tier?: string;
    status?: string;
    targets?: string[];
    expired?: boolean;
  }[];
  /** A cancel ended the turn: not an error. */
  stopped?: boolean;
  error?: string;
}

function toOutcome(raw: WireTurnResult): TurnOutcome {
  return {
    text: raw.text ?? "",
    interpretationId: raw.interpretation_id,
    toolRounds: raw.tool_rounds ?? 0,
    usage: {
      input: raw.usage?.input ?? 0,
      output: raw.usage?.output ?? 0,
      cacheRead: raw.usage?.cache_read ?? 0,
      cacheWrite: raw.usage?.cache_write ?? 0,
    },
    pendingActions: (raw.pending_actions ?? []).map((a) => ({
      actionId: a.action_id ?? "",
      actionType: a.action_type ?? "",
      tier: a.tier ?? "",
      status: a.status ?? "",
      targets: a.targets ?? [],
      expired: a.expired ?? false,
    })),
    error: raw.error,
  };
}

export class SidecarTransport implements AgentTransport {
  private child: ChildProcess | null = null;
  private connection: MessageConnection | null = null;
  /** The connection of a process still in its handshake. */
  private handshaking: MessageConnection | null = null;
  private initializing: Promise<MessageConnection> | null = null;
  /** investigation id → live session id on the CURRENT sidecar process. */
  private readonly sessions = new Map<string, string>();
  /** investigation id → a session being created, shared by concurrent callers. */
  private readonly creating = new SingleFlight<string>();
  /** investigation id → a close in flight; a new session waits for it (see closeSession). */
  private readonly closing = new Map<string, Promise<void>>();
  /** session id → the in-flight turn's progress sink (for notifications). */
  private readonly inFlight = new Map<string, TurnProgress>();
  /** Investigations with a turn underway, from the call on (one at a time), and how to stop each. */
  private readonly running = new Map<string, () => void>();
  private disposed = false;

  constructor(
    private readonly deps: SidecarDeps,
    private readonly log: vscode.LogOutputChannel,
  ) {}

  async turn(investigationId: string, text: string, progress: TurnProgress): Promise<TurnOutcome> {
    if (this.running.has(investigationId)) {
      throw new Error("a turn is already running on this investigation");
    }
    this.running.set(investigationId, () => undefined);
    try {
      return await this.runTurn(investigationId, text, progress);
    } finally {
      this.running.delete(investigationId);
    }
  }

  turnRunning(investigationId: string): boolean {
    return this.running.has(investigationId);
  }

  /**
   * One turn, limited and stoppable from entry: starting the sidecar or the
   * session can hang too. A turn stopped there never starts, and a hung process
   * is ended, or the next turn would wait on the same start.
   */
  private async runTurn(investigationId: string, text: string, progress: TurnProgress): Promise<TurnOutcome> {
    const minutes = this.deps.turnTimeoutMinutes();
    const timedOutNotice = `turn timed out after ${minutes} minutes`;
    let stopRequested = false;
    let timedOut = false;
    let cancelAtSidecar: ((reason: string) => void) | null = null;
    const stopped = new Promise<null>((resolve) => {
      this.running.set(investigationId, () => {
        stopRequested = true;
        resolve(null);
      });
    });
    const limit = setTimeout(() => {
      timedOut = true;
      this.running.get(investigationId)?.();
      cancelAtSidecar?.("timeout");
    }, minutes * 60_000);
    try {
      const opened = await Promise.race([this.openSession(investigationId), stopped]);
      if (!opened || stopRequested) {
        if (!this.connection) {
          this.teardown("a turn was stopped during startup");
        }
        return { ...toOutcome({ stopped: true }), notice: timedOut ? timedOutNotice : undefined };
      }
      const { conn, sessionId } = opened;
      cancelAtSidecar = (reason) => {
        conn.sendRequest("cancel", { session_id: sessionId, reason }).catch((err: unknown) => {
          this.log.warn(`cancelling a timed-out turn: ${err instanceof Error ? err.message : String(err)}`);
        });
      };
      this.inFlight.set(sessionId, progress);
      try {
        const raw = await conn.sendRequest("turn", { session_id: sessionId, text }) as WireTurnResult;
        return { ...toOutcome(raw), notice: timedOut && raw.stopped ? timedOutNotice : undefined };
      } finally {
        if (this.inFlight.get(sessionId) === progress) {
          this.inFlight.delete(sessionId);
        }
      }
    } finally {
      clearTimeout(limit);
    }
  }

  private async openSession(investigationId: string): Promise<{ conn: MessageConnection; sessionId: string }> {
    const conn = await this.ensureConnection();
    return { conn, sessionId: await this.ensureSession(conn, investigationId) };
  }

  async cancel(investigationId: string): Promise<void> {
    this.requestStop(investigationId);
    const sessionId = this.sessions.get(investigationId);
    if (!sessionId || !this.connection) {
      return; // nothing in flight on this process — cancel is idempotent
    }
    await this.connection.sendRequest("cancel", { session_id: sessionId });
  }

  /**
   * End the investigation's session: the sidecar stops what runs on it and
   * records any turn still held, so the thread is complete before a later
   * session rehydrates from it. No session is a no-op. A failure is logged: it
   * means a held turn is lost, and no caller can act on that.
   */
  async closeSession(investigationId: string): Promise<void> {
    this.requestStop(investigationId);
    await this.creating.settled(investigationId);
    const sessionId = this.sessions.get(investigationId);
    if (!sessionId || !this.connection) {
      return;
    }
    this.sessions.delete(investigationId);
    const closing = this.connection.sendRequest("closeSession", { session_id: sessionId }).then(
      () => undefined,
      (err: unknown) => this.log.warn(`closing session ${sessionId}: ${err instanceof Error ? err.message : String(err)}`),
    ).finally(() => {
      if (this.closing.get(investigationId) === closing) {
        this.closing.delete(investigationId);
      }
    });
    this.closing.set(investigationId, closing);
    await closing;
  }

  /**
   * End every session by ending the process: the identity changed, and nothing
   * of the previous person's conversation may reach the next turn (adr/0237).
   * The next turn spawns clean.
   */
  endSessions(): void {
    this.teardown("identity changed");
  }

  /**
   * Reset the agent's working context for one investigation: the sidecar drops
   * the model's conversation (its own past narration included — the vector for
   * narrative poisoning) and re-grounds on the engine record. The durable
   * investigation state is untouched. The session is created first if none is
   * live — the reset also writes the durable thread marker that stops FUTURE
   * rehydration replaying the discarded narration, so it must run even when
   * no in-process session exists yet.
   */
  async resetSession(investigationId: string): Promise<void> {
    const conn = await this.ensureConnection();
    const sessionId = await this.ensureSession(conn, investigationId);
    await conn.sendRequest("resetSession", { session_id: sessionId });
  }

  /**
   * Run the client-side tier of the two-tier knowledge summary (design/06
   * §3.2): one BYOK model completion in the sidecar over the session's own
   * conversation, posted to the backend as the concluded investigation's
   * narrative. The model key never leaves the client plane. A session is
   * created if none is live (a fresh one rehydrates from the committed
   * thread, so the narrative still writes from the record).
   */
  async summarizeConcluded(investigationId: string): Promise<void> {
    const conn = await this.ensureConnection();
    const sessionId = await this.ensureSession(conn, investigationId);
    await conn.sendRequest("summarizeConcluded", { session_id: sessionId });
  }

  /**
   * Generalize a candidate SOP from the concluded investigation (design/06: the
   * compounding loop). One BYOK completion in the sidecar over the session's own
   * conversation, informed by the SOPs + similar past cases it consulted. The
   * candidate is returned, not persisted — the analyst accepts or discards it.
   */
  async proposeSOP(investigationId: string, force?: boolean): Promise<SOPCandidate> {
    const conn = await this.ensureConnection();
    const sessionId = await this.ensureSession(conn, investigationId);
    return (await conn.sendRequest("proposeSOP", { session_id: sessionId, force: force || undefined })) as SOPCandidate;
  }

  dispose(): void {
    this.disposed = true;
    this.teardown("dispose");
  }

  // --- internals ------------------------------------------------------------

  private requestStop(investigationId: string): void {
    this.running.get(investigationId)?.();
  }

  /** Spawn + initialize once; reuse until the process dies, then respawn lazily. */
  private ensureConnection(): Promise<MessageConnection> {
    if (this.connection) {
      return Promise.resolve(this.connection);
    }
    if (this.initializing) {
      return this.initializing;
    }
    const starting: Promise<MessageConnection> = this.start().finally(() => {
      if (this.initializing === starting) {
        this.initializing = null;
      }
    });
    this.initializing = starting;
    return starting;
  }

  private async start(): Promise<MessageConnection> {
    if (this.disposed) {
      throw new Error("transport disposed");
    }
    const command = this.sidecarCommand();
    this.log.info(`spawning agent sidecar: ${command} investigate --stdio`);
    const child = spawn(command, ["investigate", "--stdio"], { stdio: ["pipe", "pipe", "pipe"] });
    // Owned from here: however the rest ends, the process ends with the
    // transport (adr/0239).
    this.child = child;
    try {
      return await this.handshake(child);
    } catch (err) {
      if (this.child === child) {
        this.teardown(`start failed: ${err instanceof Error ? err.message : String(err)}`);
      }
      throw err;
    }
  }

  private async handshake(child: ChildProcess): Promise<MessageConnection> {

    const spawned = new Promise<void>((resolve, reject) => {
      child.once("spawn", resolve);
      child.once("error", (err) =>
        reject(new Error(
          `could not start the reckon sidecar (${err.message}) — install reckon or set reckon.sidecarPath`,
        )));
    });
    await spawned;
    if (this.child !== child) {
      throw new Error("the sidecar was stopped while it started");
    }

    child.stderr?.on("data", (chunk: Buffer) => {
      this.log.debug(`sidecar: ${chunk.toString().trimEnd()}`);
    });
    child.on("exit", (code) => {
      this.log.info(`sidecar exited (code ${code ?? "signal"})`);
      if (this.child === child) {
        this.teardown(`exit ${code ?? "signal"}`);
      }
    });

    const connection = createMessageConnection(
      new StreamMessageReader(child.stdout!),
      new StreamMessageWriter(child.stdin!),
    );
    this.handshaking = connection;

    // The token handoff (§5): the loop asks, the auth session answers.
    connection.onRequest("getToken", async (params: { kind?: string; force?: boolean }) => {
      const kind: TokenKind = params?.kind === "human" ? "human" : "delegate";
      const token = await this.deps.getToken(kind, params?.force ?? false);
      return { token };
    });

    // Turn progress → the in-flight turn's sink, routed by session id.
    connection.onNotification("turn/text", (p: { session_id?: string; text?: string }) => {
      this.inFlight.get(p?.session_id ?? "")?.onText(p?.text ?? "");
    });
    connection.onNotification("turn/text_delta", (p: { session_id?: string; text?: string }) => {
      this.inFlight.get(p?.session_id ?? "")?.onTextDelta(p?.text ?? "");
    });
    connection.onNotification("turn/step", (p: { session_id?: string; round?: number }) => {
      this.inFlight.get(p?.session_id ?? "")?.onStep(p?.round ?? 0);
    });
    connection.onNotification("turn/tool_call", (p: { session_id?: string; name?: string; input?: unknown }) => {
      this.inFlight.get(p?.session_id ?? "")?.onToolCall(p?.name ?? "", p?.input);
    });
    connection.onNotification(
      "turn/tool_result",
      (p: {
        session_id?: string;
        name?: string;
        content?: string;
        is_error?: boolean;
        coverage?: string;
        event_count?: number;
        refs?: string[];
      }) => {
        this.inFlight.get(p?.session_id ?? "")?.onToolResult(
          p?.name ?? "", p?.content ?? "", p?.is_error ?? false, p?.coverage, p?.event_count, p?.refs,
        );
      },
    );
    connection.onNotification("turn/context_compacted", (p: { session_id?: string; text?: string }) => {
      this.inFlight.get(p?.session_id ?? "")?.onContextCompacted?.(p?.text ?? "");
    });
    connection.listen();

    // The stdio handshake (§4) — fail closed with the sidecar's diagnostic.
    const init = await connection.sendRequest("initialize", {
      protocol_version: PROTOCOL_VERSION,
      backend_url: this.deps.backendUrl(),
      model: this.deps.model(),
      anthropic_api_key: (await this.deps.apiKey()) ?? "",
      max_tool_result_kb: this.deps.maxToolResultKB(),
      context_window_tokens: this.deps.contextWindowTokens(),
    }) as { protocol_version?: number; backend_api_version?: number; server_version?: string };
    this.log.info(
      `sidecar ready: ${init.server_version ?? "?"} (protocol v${init.protocol_version}, backend api v${init.backend_api_version})`,
    );

    if (this.child !== child) {
      throw new Error("the sidecar was stopped while it started");
    }
    this.handshaking = null;
    this.connection = connection;
    return connection;
  }

  /**
   * One session per investigation (#157): calls racing on the same
   * investigation — the conclude-time summary and an analyst's turn — share
   * the session being created rather than each creating their own.
   */
  private async ensureSession(conn: MessageConnection, investigationId: string): Promise<string> {
    await this.closing.get(investigationId);
    const existing = this.sessions.get(investigationId);
    if (existing) {
      return existing;
    }
    return this.creating.run(investigationId, () => this.createSession(conn, investigationId));
  }

  private async createSession(conn: MessageConnection, investigationId: string): Promise<string> {
    const res = await conn.sendRequest("createSession", { investigation_id: investigationId }) as {
      session_id?: string;
      tools?: string[];
    };
    if (!res.session_id) {
      throw new Error("sidecar returned no session id");
    }
    this.log.info(`session ${res.session_id} over ${investigationId} (${res.tools?.length ?? 0} tools)`);
    this.sessions.set(investigationId, res.session_id);
    return res.session_id;
  }

  /** Drop the dead process's state; the next turn respawns and re-creates sessions. */
  private teardown(reason: string): void {
    this.handshaking?.dispose();
    this.handshaking = null;
    this.initializing = null; // that start is dead; the next turn begins its own
    this.connection?.dispose();
    this.connection = null;
    const child = this.child;
    this.child = null;
    this.sessions.clear();
    this.creating.clear();
    this.closing.clear();
    this.inFlight.clear();
    if (child && child.exitCode === null && !child.killed) {
      child.kill();
    }
    this.log.debug(`sidecar torn down (${reason})`);
  }

  private sidecarCommand(): string {
    const configured = vscode.workspace.getConfiguration("reckon").get<string>("sidecarPath", "");
    return configured.trim() !== "" ? configured.trim() : "reckon";
  }
}
