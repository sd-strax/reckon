// @vitest-environment node
import { EventEmitter } from "events";
import { PassThrough } from "stream";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createMessageConnection, type MessageConnection, StreamMessageReader, StreamMessageWriter } from "vscode-jsonrpc/node";
import { SidecarTransport, type SidecarDeps, type TurnProgress } from "./agentTransport";
import { fakeLog, resetVscodeFake, setConfiguration } from "./test/vscode";

/**
 * A spawned sidecar: the host's stdio pipes, with a vscode-jsonrpc peer on the
 * far side playing the Go sidecar, so the real LSP framing is exercised.
 */
class FakeChild extends EventEmitter {
  readonly stdin = new PassThrough();
  readonly stdout = new PassThrough();
  readonly stderr = new PassThrough();
  exitCode: number | null = null;
  killed = false;
  readonly peer: MessageConnection;
  constructor(readonly command: string, readonly args: string[]) {
    super();
    this.peer = createMessageConnection(new StreamMessageReader(this.stdin), new StreamMessageWriter(this.stdout));
  }
  kill(): boolean {
    this.killed = true;
    this.exit(null);
    return true;
  }
  /** The process ends (crash, or kill). */
  exit(code: number | null): void {
    this.exitCode = code ?? -1;
    this.peer.dispose();
    this.emit("exit", code);
  }
}

const spawned = vi.hoisted(() => ({ children: [] as unknown[], onSpawn: (_c: never) => undefined as void, failWith: null as Error | null }));

vi.mock("child_process", () => ({
  spawn: (command: string, args: string[]) => {
    const child = new FakeChild(command, args);
    spawned.children.push(child);
    if (spawned.failWith) {
      const err = spawned.failWith;
      setImmediate(() => child.emit("error", err));
    } else {
      spawned.onSpawn(child as never);
      child.peer.listen();
      setImmediate(() => child.emit("spawn"));
    }
    return child;
  },
}));

const children = () => spawned.children as FakeChild[];

/** The sidecar's answers; each test overrides what it exercises. */
interface Sidecar {
  initialize: unknown[];
  createSession: string[];
  closeSession: string[];
  cancel: { session_id: string; reason?: string }[];
  /** Answer initialize; a test overrides it to model a refused or stalled handshake. */
  init(): Promise<unknown>;
  /** Answer closeSession; a test holding it open models a slow close. */
  close(): Promise<unknown>;
  turn(child: FakeChild, params: { session_id: string; text: string }): Promise<unknown>;
}

function installSidecar(over: Partial<Sidecar> = {}): Sidecar {
  const sc: Sidecar = {
    initialize: [],
    createSession: [],
    closeSession: [],
    cancel: [],
    init: async () => ({ protocol_version: 1, backend_api_version: 1, server_version: "test" }),
    close: async () => ({}),
    turn: async () => ({ text: "", tool_rounds: 0 }),
    ...over,
  };
  let n = 0;
  spawned.onSpawn = (child: FakeChild) => {
    child.peer.onRequest("initialize", (p: unknown) => {
      sc.initialize.push(p);
      return sc.init();
    });
    child.peer.onRequest("createSession", (p: { investigation_id: string }) => {
      sc.createSession.push(p.investigation_id);
      return { session_id: `s-${children().indexOf(child)}-${++n}`, tools: ["a", "b"] };
    });
    child.peer.onRequest("cancel", (p: { session_id: string; reason?: string }) => {
      sc.cancel.push(p);
      return {};
    });
    child.peer.onRequest("closeSession", (p: { session_id: string }) => {
      sc.closeSession.push(p.session_id);
      return sc.close();
    });
    child.peer.onRequest("turn", (p: { session_id: string; text: string }) => sc.turn(child, p));
  };
  return sc;
}

const silent: TurnProgress = {
  onText: () => undefined, onTextDelta: () => undefined, onStep: () => undefined,
  onToolCall: () => undefined, onToolResult: () => undefined,
};

function deps(over: Partial<SidecarDeps> = {}): SidecarDeps {
  return {
    backendUrl: () => "http://127.0.0.1:7700",
    model: () => "claude-test",
    maxToolResultKB: () => 256,
    contextWindowTokens: () => 150000,
    turnTimeoutMinutes: () => 10,
    apiKey: async () => undefined,
    getToken: async (kind) => `tok-${kind}`,
    ...over,
  };
}

let transport: SidecarTransport | null = null;

beforeEach(() => {
  resetVscodeFake();
  spawned.children.length = 0;
  spawned.failWith = null;
});

afterEach(() => {
  transport?.dispose();
  transport = null;
});

describe("SidecarTransport: start", () => {
  it("spawns `reckon investigate --stdio` and opens with the handshake", async () => {
    const sc = installSidecar();
    transport = new SidecarTransport(deps({ apiKey: async () => "sk-byok" }), fakeLog());
    await transport.turn("inv-1", "hi", silent);

    expect(children()).toHaveLength(1);
    expect([children()[0].command, ...children()[0].args]).toEqual(["reckon", "investigate", "--stdio"]);
    expect(sc.initialize).toEqual([{
      protocol_version: 1, backend_url: "http://127.0.0.1:7700", model: "claude-test", anthropic_api_key: "sk-byok",
      max_tool_result_kb: 256,
      context_window_tokens: 150000,
    }]);
  });

  it("runs the configured reckon.sidecarPath instead", async () => {
    installSidecar();
    setConfiguration("reckon.sidecarPath", "  /opt/reckon/bin/reckon ");
    transport = new SidecarTransport(deps(), fakeLog());
    await transport.turn("inv-1", "hi", silent);
    expect(children()[0].command).toBe("/opt/reckon/bin/reckon");
  });

  it("fails with an install hint when the binary cannot start", async () => {
    spawned.failWith = new Error("spawn reckon ENOENT");
    transport = new SidecarTransport(deps(), fakeLog());
    await expect(transport.turn("inv-1", "hi", silent)).rejects.toThrow(/install reckon or set reckon.sidecarPath/);
  });
});

describe("SidecarTransport: turns", () => {
  it("maps the wire result and routes the turn's notifications to its sink", async () => {
    installSidecar({
      turn: async (child, p) => {
        child.peer.sendNotification("turn/step", { session_id: p.session_id, round: 1 });
        child.peer.sendNotification("turn/tool_call", { session_id: p.session_id, name: "query", input: { q: 1 } });
        child.peer.sendNotification("turn/tool_result", {
          session_id: p.session_id, name: "query", content: "{}", is_error: false, coverage: "COMPLETE", event_count: 3, refs: ["r-1"],
        });
        child.peer.sendNotification("turn/context_compacted", { session_id: p.session_id, text: "Context condensed" });
        child.peer.sendNotification("turn/text_delta", { session_id: p.session_id, text: "done" });
        // Another session's notification never reaches this turn.
        child.peer.sendNotification("turn/text_delta", { session_id: "someone-else", text: "leak" });
        return {
          text: "done", interpretation_id: "i-1", tool_rounds: 1,
          usage: { input: 10, output: 2, cache_read: 4 },
          pending_actions: [{ action_id: "a-1", action_type: "host.isolate", tier: "T2", status: "PENDING_MANUAL", targets: ["h"] }],
        };
      },
    });
    const seen: string[] = [];
    const sink: TurnProgress = {
      onText: (t) => seen.push(`text ${t}`),
      onTextDelta: (t) => seen.push(`delta ${t}`),
      onStep: (r) => seen.push(`step ${r}`),
      onToolCall: (n, i) => seen.push(`call ${n} ${JSON.stringify(i)}`),
      onToolResult: (n, _c, e, cov, ev, refs) => seen.push(`result ${n} ${e} ${cov} ${ev} ${refs?.join(",")}`),
      onContextCompacted: (note) => seen.push(`compacted ${note}`),
    };
    transport = new SidecarTransport(deps(), fakeLog());
    const out = await transport.turn("inv-1", "go", sink);

    expect(seen).toEqual([
      "step 1", 'call query {"q":1}', "result query false COMPLETE 3 r-1", "compacted Context condensed", "delta done",
    ]);
    expect(out).toEqual({
      text: "done", interpretationId: "i-1", toolRounds: 1,
      usage: { input: 10, output: 2, cacheRead: 4, cacheWrite: 0 },
      pendingActions: [{ actionId: "a-1", actionType: "host.isolate", tier: "T2", status: "PENDING_MANUAL", targets: ["h"], expired: false }],
      error: undefined,
    });
  });

  it("keeps one session per investigation across turns", async () => {
    const sc = installSidecar();
    transport = new SidecarTransport(deps(), fakeLog());
    await transport.turn("inv-1", "a", silent);
    await transport.turn("inv-1", "b", silent);
    await transport.turn("inv-2", "c", silent);
    expect(sc.createSession).toEqual(["inv-1", "inv-2"]);
    expect(children()).toHaveLength(1);
  });

  it("answers the sidecar's getToken from the auth session", async () => {
    const asked: [string, boolean][] = [];
    let answered: unknown[] = [];
    installSidecar({
      turn: async (child) => {
        answered = [
          await child.peer.sendRequest("getToken", { kind: "human", force: true }),
          await child.peer.sendRequest("getToken", {}),
        ];
        return {};
      },
    });
    transport = new SidecarTransport(deps({ getToken: async (kind, force) => { asked.push([kind, force]); return `tok-${kind}`; } }), fakeLog());
    await transport.turn("inv-1", "x", silent);
    expect(asked).toEqual([["human", true], ["delegate", false]]);
    expect(answered).toEqual([{ token: "tok-human" }, { token: "tok-delegate" }]);
  });
});

describe("SidecarTransport: lifecycle", () => {
  it("respawns after the sidecar exits and re-creates the session", async () => {
    const sc = installSidecar();
    transport = new SidecarTransport(deps(), fakeLog());
    await transport.turn("inv-1", "a", silent);
    children()[0].exit(2);
    await transport.turn("inv-1", "b", silent);

    expect(children()).toHaveLength(2);
    expect(sc.initialize).toHaveLength(2);
    expect(sc.createSession).toEqual(["inv-1", "inv-1"]);
  });

  it("kills the child on dispose and refuses to start again", async () => {
    installSidecar();
    transport = new SidecarTransport(deps(), fakeLog());
    await transport.turn("inv-1", "a", silent);
    transport.dispose();
    expect(children()[0].killed).toBe(true);
    await expect(transport.turn("inv-1", "b", silent)).rejects.toThrow("transport disposed");
  });

  it("closes a session, and the next turn opens a new one", async () => {
    const sc = installSidecar();
    transport = new SidecarTransport(deps(), fakeLog());
    await transport.turn("inv-1", "a", silent);
    await transport.closeSession("inv-1");
    expect(sc.closeSession).toHaveLength(1);
    await transport.turn("inv-1", "b", silent);
    expect(sc.createSession).toEqual(["inv-1", "inv-1"]);
  });

  it("does nothing closing an investigation with no session", async () => {
    const sc = installSidecar();
    transport = new SidecarTransport(deps(), fakeLog());
    await transport.turn("inv-1", "a", silent);
    await transport.closeSession("inv-2");
    expect(sc.closeSession).toEqual([]);
  });

  it("waits for a close before opening the next session, so rehydration sees the whole thread", async () => {
    let finish!: (v: unknown) => void;
    const sc = installSidecar({ close: () => new Promise((r) => { finish = r; }) });
    transport = new SidecarTransport(deps(), fakeLog());
    await transport.turn("inv-1", "a", silent);
    void transport.closeSession("inv-1");
    await vi.waitFor(() => expect(sc.closeSession).toHaveLength(1));

    const next = transport.turn("inv-1", "b", silent);
    await new Promise((r) => setImmediate(r));
    expect(sc.createSession).toEqual(["inv-1"]);

    finish({});
    await next;
    expect(sc.createSession).toEqual(["inv-1", "inv-1"]);
  });

  it("a close that fails is logged, not thrown", async () => {
    installSidecar({ close: async () => { throw new Error("held turn lost"); } });
    const log = fakeLog();
    transport = new SidecarTransport(deps(), log);
    await transport.turn("inv-1", "a", silent);
    await expect(transport.closeSession("inv-1")).resolves.toBeUndefined();
    expect(log.lines.some((l) => l.includes("held turn lost"))).toBe(true);
  });

  it("ends every session with the process when the identity changes", async () => {
    const sc = installSidecar();
    transport = new SidecarTransport(deps(), fakeLog());
    await transport.turn("inv-1", "a", silent);
    transport.endSessions();
    expect(children()[0].killed).toBe(true);

    await transport.turn("inv-1", "b", silent);
    expect(children()).toHaveLength(2);
    expect(sc.createSession).toEqual(["inv-1", "inv-1"]);
  });

  it("kills the child when the handshake fails, so a retry leaves nothing behind", async () => {
    installSidecar({ init: async () => { throw new Error("backend API v9 unsupported"); } });
    transport = new SidecarTransport(deps(), fakeLog());
    await expect(transport.turn("inv-1", "a", silent)).rejects.toThrow("backend API v9 unsupported");
    await expect(transport.turn("inv-1", "b", silent)).rejects.toThrow("backend API v9 unsupported");
    expect(children()).toHaveLength(2);
    expect(children().every((c) => c.killed)).toBe(true);
  });

  it("kills the child when disposed during the handshake", async () => {
    const sc = installSidecar({ init: () => new Promise(() => undefined) });
    transport = new SidecarTransport(deps(), fakeLog());
    const turn = transport.turn("inv-1", "a", silent);
    await vi.waitFor(() => expect(sc.initialize).toHaveLength(1));
    transport.dispose();
    await expect(turn).rejects.toThrow();
    expect(children()[0].killed).toBe(true);
  });
});

describe("SidecarTransport: stopping a turn", () => {
  /** A sidecar whose turn parks until the test ends it. */
  function parkedTurn() {
    let finish!: (r: unknown) => void;
    const sc = installSidecar({ turn: () => new Promise((r) => { finish = r; }) });
    return { sc, end: (r: unknown) => finish(r) };
  }

  it("refuses a second turn, and the first keeps its sink", async () => {
    let child!: FakeChild;
    let finish!: (r: unknown) => void;
    let session = "";
    installSidecar({
      turn: (c, p) => { child = c; session = p.session_id; return new Promise((r) => { finish = r; }); },
    });
    transport = new SidecarTransport(deps(), fakeLog());
    const text: string[] = [];
    const first = transport.turn("inv-1", "a", { ...silent, onText: (t) => text.push(t) });
    await vi.waitFor(() => expect(session).not.toBe(""));
    expect(transport.turnRunning("inv-1")).toBe(true);

    await expect(transport.turn("inv-1", "b", silent)).rejects.toThrow("already running");

    await child.peer.sendNotification("turn/text", { session_id: session, text: "still here" });
    await vi.waitFor(() => expect(text).toEqual(["still here"]));
    finish({ text: "", tool_rounds: 0 });
    await first;
    expect(transport.turnRunning("inv-1")).toBe(false);
  });

  it("does not start a turn stopped before it reached the sidecar", async () => {
    let turns = 0;
    installSidecar({ turn: async () => { turns++; return {}; } });
    transport = new SidecarTransport(deps(), fakeLog());
    const turn = transport.turn("inv-1", "a", silent);
    await transport.cancel("inv-1");
    await turn;
    expect(turns).toBe(0);
  });

  it("does not report a stopped turn as a failure", async () => {
    const { sc, end } = parkedTurn();
    transport = new SidecarTransport(deps(), fakeLog());
    const turn = transport.turn("inv-1", "a", silent);
    await vi.waitFor(() => expect(sc.createSession).toHaveLength(1));
    await new Promise((r) => setImmediate(r));
    await transport.cancel("inv-1");
    expect(sc.cancel).toHaveLength(1);
    expect(sc.cancel[0].reason).toBeUndefined();
    end({ stopped: true, tool_rounds: 1 });
    expect((await turn).error).toBeUndefined();
  });

  it("stops a turn past its time limit and says so", async () => {
    const { sc, end } = parkedTurn();
    transport = new SidecarTransport(deps({ turnTimeoutMinutes: () => 0.0001 }), fakeLog());
    const turn = transport.turn("inv-1", "a", silent);
    await vi.waitFor(() => expect(sc.cancel).toHaveLength(1));
    expect(sc.cancel[0].reason).toBe("timeout");
    end({ stopped: true, tool_rounds: 0 });
    const outcome = await turn;
    expect(outcome.error).toBeUndefined();
    expect(outcome.notice).toBe("turn timed out after 0.0001 minutes");
  });

  it("a hung handshake cannot outlast the limit, and the next turn starts clean", async () => {
    let hang = true;
    const sc = installSidecar({
      init: () => hang ? new Promise(() => undefined) : Promise.resolve({ protocol_version: 1, backend_api_version: 1 }),
    });
    transport = new SidecarTransport(deps({ turnTimeoutMinutes: () => 0.0001 }), fakeLog());

    const outcome = await transport.turn("inv-1", "a", silent);
    expect(outcome.notice).toBe("turn timed out after 0.0001 minutes");
    expect(children()[0].killed).toBe(true);
    expect(transport.turnRunning("inv-1")).toBe(false);

    hang = false;
    await transport.turn("inv-1", "b", silent);
    expect(children()).toHaveLength(2);
    expect(sc.createSession).toEqual(["inv-1"]);
  });

  it("Stop gets out of a hung handshake", async () => {
    const sc = installSidecar({ init: () => new Promise(() => undefined) });
    transport = new SidecarTransport(deps(), fakeLog());
    const turn = transport.turn("inv-1", "a", silent);
    await vi.waitFor(() => expect(sc.initialize).toHaveLength(1));

    await transport.cancel("inv-1");

    expect((await turn).error).toBeUndefined();
    expect(children()[0].killed).toBe(true);
  });

  it("Stop gets out of a hung session creation, and leaves the process running", async () => {
    const sc = installSidecar();
    transport = new SidecarTransport(deps(), fakeLog());
    await transport.turn("inv-1", "a", silent);
    spawned.onSpawn = () => undefined;
    children()[0].peer.onRequest("createSession", (p: { investigation_id: string }) => {
      sc.createSession.push(p.investigation_id);
      return new Promise(() => undefined);
    });
    const turn = transport.turn("inv-2", "b", silent);
    await vi.waitFor(() => expect(sc.createSession).toContain("inv-2"));

    await transport.cancel("inv-2");

    expect((await turn).error).toBeUndefined();
    expect(children()[0].killed).toBe(false);
  });
});
