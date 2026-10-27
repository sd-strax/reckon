// The /api/stream subscription (impl §3.3): one WebSocket per open
// investigation document, authenticated at the handshake with the session's
// bearer token (the server closes on expiry → reconnect with a fresh one).
// Each delta names the event family that touched the projection; the document
// host refreshes the matching slices. The hub fans out HTTP-handler writes
// only (Temporal-emitted dispatch/result events do not reach it), so the
// bounded dispatch poll stays beside this. Nothing in the webview knows a
// WebSocket exists.
import WebSocket from "ws";

export interface StreamDelta {
  type: string;
  aggregateId?: string;
  eventType?: string;
  sequenceNo?: number;
}

export interface StreamOptions {
  backendUrl: () => string;
  token: () => Promise<string>;
  onDelta: (d: StreamDelta) => void;
  log: { debug(msg: string): void };
}

export class StreamSubscription {
  private socket: WebSocket | null = null;
  private closed = false;
  private attempt = 0;
  private timer: ReturnType<typeof setTimeout> | null = null;

  constructor(private readonly investigationId: string, private readonly opts: StreamOptions) {
    void this.connect();
  }

  private async connect(): Promise<void> {
    if (this.closed) return;
    let token: string;
    try {
      token = await this.opts.token();
    } catch (err) {
      this.opts.log.debug(`stream ${this.investigationId}: no token (${err instanceof Error ? err.message : String(err)})`);
      this.retry();
      return;
    }
    const base = this.opts.backendUrl().replace(/^http/, "ws").replace(/\/$/, "");
    const url = `${base}/api/stream?investigation=${encodeURIComponent(this.investigationId)}`;
    const socket = new WebSocket(url, { headers: { Authorization: `Bearer ${token}` } });
    this.socket = socket;
    socket.on("open", () => { this.attempt = 0; });
    socket.on("message", (data: WebSocket.RawData) => {
      try {
        const raw = JSON.parse(String(data)) as { type?: string; aggregate_id?: string; event_type?: string; sequence_no?: number };
        if (raw && typeof raw.type === "string") {
          this.opts.onDelta({ type: raw.type, aggregateId: raw.aggregate_id, eventType: raw.event_type, sequenceNo: raw.sequence_no });
        }
      } catch {
        // a malformed frame is ignored; the next load reconciles
      }
    });
    socket.on("close", (code: number) => {
      this.socket = null;
      this.opts.log.debug(`stream ${this.investigationId}: closed (${code})`);
      this.retry();
    });
    socket.on("error", (err: Error) => {
      this.opts.log.debug(`stream ${this.investigationId}: ${err.message}`);
    });
  }

  /** Reconnect with backoff (token expiry closes the socket by design). */
  private retry(): void {
    if (this.closed) return;
    const delay = Math.min(30000, 1000 * 2 ** Math.min(this.attempt++, 5));
    this.timer = setTimeout(() => void this.connect(), delay);
  }

  dispose(): void {
    this.closed = true;
    if (this.timer) clearTimeout(this.timer);
    this.socket?.close();
    this.socket = null;
  }
}
