// The seed surface (design/ui/02 §2.7) and what roots an investigation: a
// draft's seed persisting into the live record, the case search behind "From
// a case…", starting from a case row, and the case write-back posture
// (adr/0032). The controller routes the messages here; this owns the flow.
import * as vscode from "vscode";
import { BackendRequestError, InvestigationDetail, existingInvestigation } from "./backend";
import type { DocumentHost } from "./documentHost";
import type { KnownAccount, SeedEntityKind } from "./protocol";
import { errText, panelTitle } from "./util";

/**
 * The accounts a 422 on an account seed lists (adr/0324): the persisted
 * accounts the record holds under the name, well-formed entries only.
 */
export function knownAccountsOf(body: Record<string, unknown> | undefined): KnownAccount[] | undefined {
  if (!body || !Array.isArray(body.known_accounts)) return undefined;
  return body.known_accounts.flatMap((a): KnownAccount[] => {
    const r = a as Record<string, unknown>;
    return r && typeof r.ref === "string" && typeof r.account_login === "string"
      ? [{ ref: r.ref, accountType: typeof r.account_type === "string" ? r.account_type : "", accountLogin: r.account_login }]
      : [];
  });
}

/** A not-yet-seeded draft's synthetic key: no aggregate id until the seed persists. */
export function isDraftKey(key: string): boolean {
  return key.startsWith("__draft__");
}

export class DraftFlow {
  constructor(private readonly host: DocumentHost) {}

  // Draft keys with a create in flight (the host half of the double-submit guard).
  private readonly seeding = new Set<string>();

  /**
   * Create an investigation, or open the one a case already has: a create the
   * server refuses because the case is already investigated (#158) opens that
   * investigation instead. On success `opened` places the record (a new panel,
   * or the draft re-keyed), then the write-back posture applies and both trees
   * re-read. Nothing opens or writes after a change of person.
   */
  private async createOrOpen(
    create: () => Promise<InvestigationDetail>,
    opened: (created: InvestigationDetail) => void,
    existing: (id: string) => void,
    failed: (err: unknown) => void,
  ): Promise<void> {
    const current = this.host.sameIdentity();
    try {
      const created = await create();
      if (!current()) return;
      opened(created);
      this.offerCaseNote(created);
      this.host.refreshTrees({ cases: true });
    } catch (err) {
      if (!current()) return;
      const found = existingInvestigation(err);
      if (found) {
        existing(found);
        return;
      }
      failed(err);
    }
  }

  /**
   * Persist a draft's seed and become the live investigation. On success the
   * panel is re-keyed from its synthetic draft key to the real aggregate id and
   * loaded in place — the surface never closes, it transforms.
   */
  async seedDraft(
    ref: { id: string },
    panel: vscode.WebviewPanel,
    seed: { value?: string; kind?: string; alertId?: string; source?: string; allowUnknown?: boolean; entityKind?: SeedEntityKind },
  ): Promise<void> {
    // Second line of the double-submit defense (the webview also guards): once
    // this draft key has a create in flight, or has already been re-keyed to a
    // real id, further seed messages are dropped — never a duplicate POST.
    if (!isDraftKey(ref.id) || this.seeding.has(ref.id)) {
      return;
    }
    this.seeding.add(ref.id);
    const draftKey = ref.id;
    try {
      await this.createOrOpen(
        () => seed.alertId !== undefined
          ? this.host.client.investigations.create("", { type: "alert", alertId: seed.alertId, source: seed.source })
          : this.host.client.investigations.createFromInput(seed.value ?? "", seed.kind ?? "", undefined, seed.allowUnknown, seed.entityKind),
        (created) => {
          // A panel closed meanwhile stays closed: the create cannot be recalled,
          // so the investigation surfaces through the trees and opens from there.
          // The case write-back offer (adr/0032) opens once the load has rendered.
          if (this.host.isOpen(draftKey, panel)) {
            ref.id = created.id;
            this.host.rekey(draftKey, created.id, panel);
            panel.title = panelTitle(created.title);
            void this.host.post(panel, { type: "seeded" });
            void this.host.load(created.id, panel);
          }
        },
        (existing) => {
          // The draft panel closes and the existing record opens in its place.
          if (!this.host.isOpen(draftKey, panel)) {
            void vscode.window.showInformationMessage(`reckon: ${seed.value ?? "this case"} already has an investigation`);
            return;
          }
          void vscode.window.showInformationMessage(`reckon: ${seed.value ?? "this case"} already has an investigation — opening it`);
          panel.dispose();
          this.host.show(existing);
        },
        (err) => {
          // An entity the host inventory or the record has never seen (#159,
          // adr/0324): say so, list the accounts the record holds under the
          // name, and let the analyst start anyway. A person seed the record
          // does not know is a 404 or a 409, never this: no start anyway.
          const unknownEntity = err instanceof BackendRequestError && err.status === 422 && err.body?.unknown_entity === true;
          const said = err instanceof BackendRequestError && typeof err.body?.error === "string" ? err.body.error : undefined;
          const message = (unknownEntity || seed.entityKind === "person") && said ? said : errText(err);
          const knownAccounts = unknownEntity ? knownAccountsOf(err.body) : undefined;
          void this.host.post(panel, { type: "draft.error", message, unknownEntity, ...(knownAccounts?.length ? { knownAccounts } : {}) });
        },
      );
    } finally {
      this.seeding.delete(draftKey);
    }
  }

  /**
   * Search the case system of record for the draft's "from a case" surface
   * (14 §4.1): the query and its results live on the panel, never the
   * top-of-window quick input (13 §3). A read failure surfaces inline in the
   * sub-form; seeding from a picked case runs the ordinary seedDraft path
   * (kind "case"), which fails closed server-side on a bad case id.
   */
  async searchCases(panel: vscode.WebviewPanel, filter: string, queue?: string): Promise<void> {
    try {
      const cases = await this.host.client.cases.search(filter.trim() || undefined, queue || undefined);
      void this.host.post(panel, { type: "draft.cases", cases });
    } catch (err) {
      void this.host.post(panel, { type: "draft.caseError", message: errText(err) });
    }
  }

  /**
   * Apply the tenant's case write-back posture to a just-created
   * investigation (adr/0032 as amended). Only "auto" acts here: it requests
   * the note now. Under "confirm" (the default) nothing happens at creation —
   * the document carries a "Note the case" action on its seed line, usable
   * whenever the analyst wants, once. "off" (or a non-case seed) sends no
   * offer at all, so the absence of created.caseNote is the whole test.
   */
  offerCaseNote(created: InvestigationDetail): void {
    if (!created.caseNote || created.caseNoteOnSeed !== "auto") {
      return;
    }
    void this.host.client.investigations.requestCaseNote(created.id).then(
      (r) => void vscode.window.showInformationMessage(`reckon: note on ${created.caseNote?.caseId} requested (${r.status.toLowerCase().replace(/_/g, " ")})`),
      (err: unknown) => void vscode.window.showWarningMessage(`reckon: could not request the case note: ${errText(err)}`),
    );
  }

  /** The analyst confirmed the note: request it as an action, then reload. */
  async postCaseNote(id: string, panel: vscode.WebviewPanel, body: string): Promise<void> {
    try {
      const r = await this.host.client.investigations.requestCaseNote(id, body.trim() || undefined);
      void vscode.window.showInformationMessage(`reckon: case note requested (${r.status.toLowerCase().replace(/_/g, " ")})`);
      void this.host.refresh(id, panel);
    } catch (err) {
      void vscode.window.showWarningMessage(`reckon: could not request the case note: ${errText(err)}`);
    }
  }

  /**
   * Render the unseeded draft, carrying the tenant's configured case queue
   * (14 §4.2) when the capability layer reports one, so the "From a case…"
   * path opens on the analyst's queue instead of a blank search. A missing or
   * unreachable capability layer is not an error here — the draft simply
   * behaves as before.
   */
  async postDraft(panel: vscode.WebviewPanel, mode?: "case"): Promise<void> {
    const incompatible = await this.host.incompatibleBackend();
    if (incompatible) {
      void this.host.post(panel, { type: "draft" });
      void this.host.post(panel, { type: "draft.error", message: incompatible });
      return;
    }
    let caseQueue: string | undefined;
    try {
      caseQueue = await this.host.client.cases.configuredQueue();
    } catch {
      caseQueue = undefined;
    }
    void this.host.post(panel, { type: "draft", mode, caseQueue });
  }

  /** Start an investigation from a case row: the seed path, then open it. */
  startFromCase(caseNumber: string): Promise<void> {
    return this.createOrOpen(
      () => this.host.client.investigations.createFromInput(caseNumber, "case"),
      (created) => this.host.show(created.id, created.title),
      (existing) => {
        void vscode.window.showInformationMessage(`reckon: ${caseNumber} already has an investigation — opening it`);
        this.host.show(existing);
      },
      (err) => void vscode.window.showErrorMessage(`reckon: could not start from ${caseNumber} — ${errText(err)}`),
    );
  }
}
