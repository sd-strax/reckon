// The turn driver: one analyst turn (or ⚡ scoped turn) through the agent
// transport, streamed to the panel as render messages, then a refresh so the
// panel shows what the turn wrote to the record.
import type * as vscode from "vscode";
import type { AgentTransport } from "./agentTransport";
import type { RenderMessage } from "./protocol";
import { errText } from "./documentText";

export class TurnDriver {
  constructor(
    private readonly transport: AgentTransport,
    private readonly post: (panel: vscode.WebviewPanel, msg: RenderMessage) => Thenable<boolean>,
    private readonly refresh: (id: string, panel: vscode.WebviewPanel) => Promise<void>,
  ) {}

  /** Drive one analyst turn through the transport, streaming progress to the webview. */
  async run(id: string, panel: vscode.WebviewPanel, text: string, aside?: { label: string }, includedKnowledge?: string[]): Promise<void> {
    if (aside) {
      void this.post(panel, { type: "aside.start", label: aside.label });
    } else {
      void this.post(panel, { type: "turn.user", text });
      void this.post(panel, { type: "turn.start" });
    }
    try {
      const outcome = await this.transport.turn(id, text, {
        // Both text paths land on the same appendable turn.text case: deltas
        // stream token-by-token (E.4); round-complete text arrives only from a
        // non-streaming provider (mutually exclusive per completion, enforced
        // sidecar-side — never both for the same text).
        onText: (chunk) => void this.post(panel, { type: "turn.text", delta: chunk }),
        onTextDelta: (chunk) => void this.post(panel, { type: "turn.text", delta: chunk }),
        onStep: (round) => void this.post(panel, { type: "turn.step", round }),
        onToolCall: (name, input) => void this.post(panel, { type: "turn.tool", name, input }),
        // coverage/events come distilled from the sidecar (from the full,
        // unclipped payload) — absent for non-envelope results, and the
        // webview renders that honestly instead of a bogus "? · 0".
        onToolResult: (name, _content, isError, coverage, events, refs) =>
          void this.post(panel, { type: "turn.toolResult", name, isError, coverage, events, refs }),
      }, includedKnowledge);
      if (outcome.interpretationId) {
        void this.post(panel, { type: "turn.committed", interpretationId: outcome.interpretationId });
      }
      // Pending actions are NOT posted from the outcome: the finally-load
      // re-fetches the durable queue, so the panel renders the backend's
      // truth (including actions from earlier turns/sessions) with the
      // approve/reject affordances attached.
      if (outcome.error) {
        void this.post(panel, { type: "turn.error", message: outcome.error });
      }
      void this.post(panel, {
        type: "turn.end",
        rounds: outcome.toolRounds,
        usage: {
          input: outcome.usage.input + outcome.usage.cacheRead + outcome.usage.cacheWrite,
          output: outcome.usage.output,
          cacheRead: outcome.usage.cacheRead,
        },
      });
    } catch (err) {
      void this.post(panel, { type: "turn.error", message: errText(err) });
      void this.post(panel, { type: "turn.end" });
    } finally {
      // Reasoning nodes may have changed — refresh the thread panel.
      void this.refresh(id, panel);
    }
  }
}
