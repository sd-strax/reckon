// What the investigation document's domains (draftFlow, actionDecisions,
// reasoningWrites, evidenceReads, knowledgeFlow, reportExport) need of the
// controller that owns the panels (adr/0243). The controller implements it;
// a domain holds this and nothing else of the controller, so what it reaches
// is this list.
import type * as vscode from "vscode";
import type { AgentTransport } from "./agentTransport";
import type { BackendClient, InvestigationDetail } from "./backend";
import type { InvestigationCache } from "./investigationCache";
import type { RenderMessage } from "./protocol";

export interface DocumentHost {
  readonly client: BackendClient;
  readonly log: vscode.LogOutputChannel;
  readonly transport: AgentTransport;
  /** The projection reads, their watermark and the per-investigation ordering (impl §3.1). */
  readonly cache: InvestigationCache;
  /** Post one render message; false when the panel is gone. */
  post(panel: vscode.WebviewPanel, msg: RenderMessage): Thenable<boolean>;
  /** Re-read the event-sourced slices after a write and post them as a patch. */
  refresh(id: string, panel: vscode.WebviewPanel): Promise<void>;
  /** Re-read the whole snapshot. */
  load(id: string, panel: vscode.WebviewPanel): Promise<void>;
  /** One write from the panel: run it, toast a failure under `verb`, re-read (adr/0246). */
  mutate(id: string, panel: vscode.WebviewPanel, verb: string, fn: () => Promise<void>, opts?: { reload?: boolean }): Promise<void>;
  /** A check that holds until the person changes (adr/0241). */
  sameIdentity(): () => boolean;
  /** The handshake (adr/0244): why the backend must not be spoken to, or null. */
  incompatibleBackend(): Promise<string | null>;
  /** Whether this panel is still the open document for the investigation. */
  isOpen(id: string, panel: vscode.WebviewPanel): boolean;
  /** The trees re-read: Investigations always, Cases too when a case changed hands. */
  refreshTrees(opts?: { cases?: boolean }): void;
  /** End the agent session of an investigation no panel holds (adr/0237). */
  releaseSession(id: string): void;
  /** Open (or reveal) the document for one investigation. */
  show(id: string, titleHint?: string): void;
  /** A draft panel became the live investigation: hold it under the real id. */
  rekey(from: string, to: string, panel: vscode.WebviewPanel): void;
  /** Apply the tenant's case write-back posture to a just-created investigation (adr/0032). */
  offerCaseNote(created: InvestigationDetail): void;
}
