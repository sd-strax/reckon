// The analyst's writes to the reasoning thread (01 §x-hypothesis,
// §x-prediction, §Verdict, §Pinned evidence): pins, hypotheses, predictions
// and their results, the verdict of record. Each is one `mutate` on the human
// token; the aggregate's guards surface verbatim.
import * as vscode from "vscode";
import type { ArtifactLocation } from "./backend";
import type { DocumentHost } from "./documentHost";

export class ReasoningWrites {
  constructor(private readonly host: DocumentHost) {}

  /**
   * Pin evidence — the analyst's curation act, on the human token. The finding
   * note is collected in-webview (the pin card), so this just records it.
   */
  async pinCommit(id: string, panel: vscode.WebviewPanel, refs: string[], finding: string): Promise<void> {
    if (!refs.length || finding.trim() === "") {
      return;
    }
    await this.host.mutate(id, panel, "pin", () => this.host.client.investigations.pinEvidence(id, finding.trim(), refs));
  }

  /**
   * Un-pin = supersession with a reason; the pin stays visible, struck. The
   * reason is collected in the webview prompt card, so this just records it.
   */
  async unpin(id: string, panel: vscode.WebviewPanel, interpretationId: string, reason: string): Promise<void> {
    if (reason.trim() === "") {
      return;
    }
    await this.host.mutate(id, panel, "un-pin", () => this.host.client.investigations.supersedeInterpretation(interpretationId, id, reason.trim()));
  }

  /**
   * Acknowledge an AI-PROPOSED hypothesis — the human taking ownership of the
   * line of inquiry (02 §2.9). The aggregate enforces that this is a human act;
   * a rejection surfaces verbatim.
   */
  ackHypothesis(id: string, panel: vscode.WebviewPanel, hypothesisRef: string, restatement?: string, handle?: string): Promise<void> {
    return this.host.mutate(id, panel, "adopt", () => this.host.client.investigations.acknowledgeHypothesis(id, hypothesisRef, {
      restatement: restatement?.trim() || undefined,
      handle: handle?.trim() || undefined,
    }));
  }

  /** Rename / restate an OPEN hypothesis (01 §x-hypothesis; human, untested-only for the statement). */
  async reviseHypothesis(id: string, panel: vscode.WebviewPanel, hypothesisRef: string, restatement?: string, handle?: string): Promise<void> {
    const r = restatement?.trim() || undefined;
    const h = handle?.trim() || undefined;
    if (!r && !h) {
      return;
    }
    await this.host.mutate(id, panel, "revise", () => this.host.client.investigations.reviseHypothesis(id, hypothesisRef, { restatement: r, handle: h }));
  }

  /** Abandon a live hypothesis the analyst is not pursuing (terminal; the thread keeps it). */
  abandonHypothesis(id: string, panel: vscode.WebviewPanel, hypothesisRef: string, reason: string): Promise<void> {
    return this.host.mutate(id, panel, "abandon", () => this.host.client.investigations.abandonHypothesis(id, hypothesisRef, reason.trim() || "abandoned from the workbench — not pursued"));
  }

  /** Record the analyst's own test under a hypothesis (01 §x-prediction: either actor authors predictions). */
  async newPrediction(id: string, panel: vscode.WebviewPanel, hypothesisRef: string, statement: string, discriminates: string[], testKind?: string, title?: string): Promise<void> {
    if (statement.trim() === "") {
      return;
    }
    // The title is the analyst's when given; the engine derives one otherwise (adr/0090).
    await this.host.mutate(id, panel, "prediction", () => this.host.client.investigations.recordPrediction(id, hypothesisRef, statement.trim(), discriminates ?? [], testKind, title?.trim() || undefined));
  }

  /**
   * The analyst records a prediction's result themselves (adr/0054): the
   * outcome plus what was observed — a person's answer, a check made by hand —
   * which the engine keeps as an attestation the outcome cites. Rejections
   * (e.g. the prediction is no longer UNTESTED) surface verbatim.
   */
  async recordPredictionResult(
    id: string,
    panel: vscode.WebviewPanel,
    msg: { predictionRef: string; status: string; content: string; source?: string; channel?: string; reference?: string; hashReference?: boolean; artifact?: ArtifactLocation },
  ): Promise<void> {
    if (msg.content.trim() === "") {
      return;
    }
    const how = msg.channel?.trim() ? ` by ${msg.channel.trim()}` : "";
    await this.host.mutate(id, panel, "record result", () => this.host.client.investigations.recordPredictionResult(id, msg.predictionRef, msg.status, `result recorded by the analyst${how}`, {
      content: msg.content.trim(),
      source: msg.source?.trim() || undefined,
      channel: msg.channel?.trim() || undefined,
      reference: msg.reference?.trim() || undefined,
      hashReference: msg.hashReference,
      artifact: msg.artifact,
    }));
  }

  /** Withdraw an untested prediction — a status, never a deletion. */
  withdrawPrediction(id: string, panel: vscode.WebviewPanel, predictionRef: string, reason: string): Promise<void> {
    return this.host.mutate(id, panel, "withdraw", () => this.host.client.investigations.withdrawPrediction(id, predictionRef, reason.trim() || "withdrawn from the workbench"));
  }

  /**
   * Record an analyst-authored hypothesis — e.g. the alternate explanation
   * that competes with the AI's. Lands OPEN (human-authored); rejections
   * surface verbatim.
   */
  async newHypothesis(id: string, panel: vscode.WebviewPanel, statement: string, handle?: string): Promise<void> {
    if (statement.trim() === "") {
      return;
    }
    await this.host.mutate(id, panel, "hypothesis", () => this.host.client.investigations.recordHypothesis(id, statement.trim(), handle?.trim() || undefined));
  }

  /** Record the verdict of record — always the analyst's act here (human token). */
  submitVerdict(
    id: string,
    panel: vscode.WebviewPanel,
    msg: { disposition: string; rationale: string; refs: string[] },
  ): Promise<void> {
    return this.host.mutate(id, panel, "verdict", async () => {
      await this.host.client.investigations.recordVerdict(id, msg.disposition, msg.rationale, msg.refs);
      void vscode.window.showInformationMessage(`reckon: verdict recorded — ${msg.disposition}`);
    });
  }
}
