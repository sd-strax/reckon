// The reckon workbench extension entry point.
//
// v0 slice (design/13 §7): backend version handshake, interactive PKCE login,
// BYOK Anthropic key into SecretStorage, and the real investigation list. The
// investigation document (conversation surface) is the next step.
//
// Workbench discipline (design/13 §3): every surface lives in reckon-owned real
// estate, every action is a command first, and nothing here assumes a workspace
// folder is open — investigation state comes from the backend, not the
// filesystem.

import * as vscode from "vscode";
import { BackendClient } from "./backend";
import { Session } from "./auth";
import { SidecarTransport } from "./agentTransport";
import { InvestigationDocuments } from "./investigationDocument";
import { ImportSOPsView } from "./importView";
import { CasesProvider } from "./casesTree";
import { InvestigationsProvider } from "./investigationsTree";
import { SeenTracker } from "./seenTracker";
import { CaseDocuments } from "./caseDocument";
import { applyPosture, offerPosture } from "./posture";
import { errText } from "./util";

/** Where the BYOK Anthropic key lives — never in settings, never on disk in the clear. */
const ANTHROPIC_KEY_SECRET = "reckon.anthropicApiKey";

export function activate(context: vscode.ExtensionContext): void {
  let started = false;
  const log = vscode.window.createOutputChannel("reckon", { log: true });
  log.info(`workbench activated (backend ${backendUrl()})`);

  const session = new Session(
    context.secrets,
    () => new BackendClient(() => backendUrl()).authConfig(),
    log,
  );
  // Authenticated client: its token source is the session, refreshed on demand.
  const client = new BackendClient(() => backendUrl(), () => session.token(), (rejected) => session.renew(rejected));

  // Last-seen event sequences — view state behind the tree's unseen-changes
  // cue (ui/02 §2.12). Never investigation state; lives in globalState.
  const seen = new SeenTracker(context.globalState, () => session.subject);
  const investigations = new InvestigationsProvider(client, session, seen);
  // The agent loop lives in the Go sidecar (implementation/agent-sidecar.md);
  // this transport spawns it and answers its getToken callbacks from the auth
  // session — the extension stays the sole auth owner.
  const transport = new SidecarTransport({
    backendUrl: () => backendUrl(),
    model: () => vscode.workspace.getConfiguration("reckon").get<string>("model", "claude-opus-4-8"),
    maxToolResultKB: () => vscode.workspace.getConfiguration("reckon").get<number>("agent.maxToolResultKB", 128),
    contextWindowTokens: () => vscode.workspace.getConfiguration("reckon").get<number>("agent.contextWindowTokens", 200000),
    turnTimeoutMinutes: () => Math.max(vscode.workspace.getConfiguration("reckon").get<number>("agent.turnTimeoutMinutes", 10), 1),
    apiKey: () => context.secrets.get(ANTHROPIC_KEY_SECRET),
    getToken: (kind, force) => session.token(kind, force),
  }, log);
  const importSOPs = new ImportSOPsView(context.extensionUri, client, log);
  const documents = new InvestigationDocuments(
    context.extensionUri, client, log, transport,
    () => client.me().then((m) => ({ name: m.username, principal: m.subject })).catch(() => null),
    (id, seq) => {
      void seen.set(id, seq).then(() => investigations.refresh());
    },
  );
  // The case queue (adr/0030-cases-pane.md): the SoR's queue below
  // Investigations; a case opens read-only and an investigation starts from it.
  const cases = new CasesProvider(client, session, context.globalState);
  const caseDocs = new CaseDocuments(context.extensionUri, client, log, (created) => {
    documents.show(created.id, created.title);
    // The case write-back posture (adr/0032): ask, act, or nothing.
    documents.offerCaseNote(created);
    investigations.refresh();
    cases.refresh();
  });
  context.subscriptions.push(
    vscode.window.registerTreeDataProvider("reckon.cases", cases),
    vscode.commands.registerCommand("reckon.openCase", (number?: string) => {
      if (typeof number === "string" && number !== "") {
        caseDocs.open(number);
      }
    }),
    vscode.commands.registerCommand("reckon.refreshCases", () => cases.refresh()),
    // One toggle, two commands: the view-title shows whichever order is NOT
    // current (context key reckon.casesSort), so the icon reads as "switch to".
    vscode.commands.registerCommand("reckon.cases.sortByAge", () => cases.setSort("age")),
    vscode.commands.registerCommand("reckon.cases.sortByPriority", () => cases.setSort("priority")),
    log,
    session,
    { dispose: () => transport.dispose() },
    { dispose: () => importSOPs.dispose() },
    session.onDidChangeIdentity(() => {
      transport.endSessions();
      documents.identityChanged();
      caseDocs.closeAll();
      importSOPs.identityChanged();
      // Not on the restore at start-up: nobody changed, the window just opened.
      if (started) {
        void vscode.commands.executeCommand("workbench.view.extension.reckon");
      }
    }),
    session.onDidChange(() => {
      void vscode.commands.executeCommand("setContext", "reckon.signedIn", session.signedIn);
      investigations.refresh();
      cases.refresh();
      documents.refreshAll();
    }),
    vscode.window.registerTreeDataProvider("reckon.investigations", investigations),

    vscode.commands.registerCommand("reckon.openInvestigation", (id?: string, title?: string) => {
      if (!id) {
        return; // invoked without a target (e.g. from the palette) — nothing to open
      }
      documents.show(id, title);
    }),

    // Seed a new investigation (design/ui/02 §2.7): open the document in its
    // unseeded state and let the analyst root it by typing a host/IP/hash or a
    // question into the composer — the seed surface IS the document, not a
    // wizard. Nothing is persisted until that first line, which keeps "an
    // investigation always begins from something concrete" honest. The server
    // resolves the typed value (minting the STIX id for an entity), so the
    // analyst never handles ids.
    vscode.commands.registerCommand("reckon.newInvestigation", () => {
      if (!session.signedIn) {
        void vscode.window.showWarningMessage("reckon: sign in before seeding an investigation");
        return;
      }
      documents.showDraft();
    }),

    // Seed an investigation FROM a system-of-record case (14-case-seed.md §4.1).
    // The command only LAUNCHES the flow; the search filter and case pick live
    // on the draft document surface (case mode), never the top-of-window quick
    // input — operational input belongs on a panel (workbench discipline, 13 §3).
    // The read fails closed server-side, so a bad case never seeds a half-loaded
    // investigation.
    vscode.commands.registerCommand("reckon.seedFromCase", () => {
      if (!session.signedIn) {
        void vscode.window.showWarningMessage("reckon: sign in before seeding an investigation");
        return;
      }
      documents.showDraft("case");
    }),

    // Import institutional knowledge into the SOP corpus (design/06 §2.4). SOPs
    // are consultative — the agent surfaces them (the knowledge rail); import is
    // an occasional populate act, so it is a COMMAND (workbench discipline, 13
    // §3), not a management view. The command opens the Import SOPs PANEL:
    // choosing what becomes institutional knowledge is operational input, so it
    // lives on a reckon surface (open-editor documents with checkboxes, add
    // from disk, per-document outcome) — never the top-of-window quick input.
    vscode.commands.registerCommand("reckon.importSOP", () => {
      if (!session.signedIn) {
        void vscode.window.showWarningMessage("reckon: sign in before importing SOPs");
        return;
      }
      importSOPs.open();
    }),

    // Rename an investigation from the tree's context menu (human curation — the
    // aggregate bars an AI delegate). Renaming happens ON the document: this
    // reveals the panel and pops its in-document rename card, so the input is
    // never the top-of-window quick input. The header pencil opens the same card
    // directly. Applying the rename + refreshing the tree is handled there.
    vscode.commands.registerCommand("reckon.renameInvestigation", (arg?: unknown) => {
      if (!session.signedIn) {
        void vscode.window.showWarningMessage("reckon: sign in before renaming an investigation");
        return;
      }
      if (arg && typeof arg === "object" && "invId" in arg) {
        const row = arg as { invId: string; invTitle?: string };
        documents.beginRename(row.invId, row.invTitle);
      }
    }),
    // Context reset (agent honesty): rebuild the agent's working conversation
    // from the engine record, discarding its own accumulated narration — the
    // recovery from narrative poisoning. Investigation state is untouched; the
    // reset is recorded on the thread and survives sidecar respawns.
    vscode.commands.registerCommand("reckon.resetAgentContext", async (arg?: unknown) => {
      if (!session.signedIn) {
        void vscode.window.showWarningMessage("reckon: sign in first");
        return;
      }
      if (!arg || typeof arg !== "object" || !("invId" in arg)) {
        return;
      }
      const row = arg as { invId: string; invTitle?: string };
      const subject = session.subject;
      const ok = await vscode.window.showWarningMessage(
        `reckon: reset the agent's conversation context for "${row.invTitle ?? row.invId}"? ` +
        "The investigation record (evidence, hypotheses, actions, chronicle) is untouched — " +
        "only the agent's working memory is rebuilt from it. Use this when the conversation " +
        "has drifted from the recorded state.",
        { modal: true },
        "Reset context",
      );
      if (ok !== "Reset context" || session.subject !== subject) {
        return;
      }
      try {
        await transport.resetSession(row.invId);
        // The reset is a committed act on the thread; refresh an open panel so
        // the chronicle shows the boundary immediately.
        documents.refreshOpen(row.invId);
        documents.releaseSession(row.invId);
        void vscode.window.showInformationMessage(
          "reckon: agent context reset — the next turn starts from the engine record.",
        );
      } catch (err) {
        void vscode.window.showErrorMessage(`reckon: context reset failed — ${errText(err)}`);
      }
    }),

    vscode.commands.registerCommand("reckon.signIn", async () => {
      try {
        await session.signIn();
        const me = await client.me();
        void vscode.window.showInformationMessage(
          `reckon: signed in as ${me.username} (${me.roles.join(", ") || "no roles"})`,
        );
      } catch (err) {
        void vscode.window.showErrorMessage(`reckon sign-in failed: ${errText(err)}`);
      }
    }),

    vscode.commands.registerCommand("reckon.signOut", async () => {
      await session.signOut();
      void vscode.window.showInformationMessage("reckon: signed out");
    }),

    vscode.commands.registerCommand("reckon.setAnthropicKey", async () => {
      const key = await vscode.window.showInputBox({
        title: "reckon — Anthropic API key (BYOK)",
        prompt: "Stored in the OS keychain via SecretStorage; never leaves this machine except in calls to Anthropic.",
        password: true,
        ignoreFocusOut: true,
        placeHolder: "sk-ant-...",
      });
      if (key === undefined) {
        return; // cancelled
      }
      if (key.trim() === "") {
        await context.secrets.delete(ANTHROPIC_KEY_SECRET);
        void vscode.window.showInformationMessage("reckon: Anthropic key cleared");
        return;
      }
      await context.secrets.store(ANTHROPIC_KEY_SECRET, key.trim());
      void vscode.window.showInformationMessage("reckon: Anthropic key stored");
    }),

    vscode.commands.registerCommand("reckon.checkBackend", async () => {
      const status = await client.status();
      const reason = client.incompatibilityReason(status);
      log.info(`status ${backendUrl()}: ${status.overall} (api v${status.apiVersion ?? "?"}, ${reason ?? "compatible"})`);
      if (reason) {
        void vscode.window.showWarningMessage(`reckon: ${reason}`);
      } else {
        const detail = Object.entries(status.components)
          .map(([name, c]) => `${name}: ${c.ready ? "ready" : "NOT READY"} (${c.message})`)
          .join("\n");
        void vscode.window.showInformationMessage(
          `reckon backend ${status.overall} at ${backendUrl()} (API v${status.apiVersion})`,
          { modal: false, detail },
        );
      }
      investigations.refresh();
    }),

    vscode.commands.registerCommand("reckon.refreshInvestigations", () => {
      investigations.refresh();
    }),

    // The profile trim (design/13 §6): investigation-first posture, offered
    // never forced, reversible. What settings CAN do; the honest ceiling —
    // hiding other view containers, rebranding the window, product.json
    // behavior — is the fork's payoff, not the extension's.
    vscode.commands.registerCommand("reckon.applyPosture", () => applyPosture(true)),
    vscode.commands.registerCommand("reckon.revertPosture", () => applyPosture(false)),
  );

  // First-run offer (design/13 §6): once, and "Not now" is remembered. An
  // analyst embedding reckon in their developer profile is a supported choice.
  offerPosture(context.globalState);

  // Fire-and-forget: restore a prior session, then seed the signed-in context key.
  void session.restore().finally(() => {
    started = true;
    void vscode.commands.executeCommand("setContext", "reckon.signedIn", session.signedIn);
  });
}

export function deactivate(): void {
  // Session and channel are held in context.subscriptions.
}

function backendUrl(): string {
  return vscode.workspace.getConfiguration("reckon").get<string>("backendUrl", "http://localhost:8080");
}
