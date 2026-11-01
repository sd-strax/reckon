// The investigation-first workbench posture (design/13 §6): offered once,
// never forced, reversible. What settings CAN do; the honest ceiling (hiding
// other view containers, rebranding the window, product.json behaviour) is a
// fork's payoff, not the extension's.
import * as vscode from "vscode";

/**
 * The posture's settings, applied globally: no startup editor, no command
 * center, no tips, menu bar behind Alt (Win/Linux; macOS keeps its native bar
 * per-OS convention). Revert clears our overrides back to defaults rather than
 * guessing what the user had. Keybindings for the core loop ship via
 * contributes.keybindings and need no toggling.
 */
const POSTURE: Record<string, unknown> = {
  "workbench.startupEditor": "none",
  "window.commandCenter": false,
  "workbench.tips.enabled": false,
  "window.menuBarVisibility": "toggle",
};

export async function applyPosture(on: boolean): Promise<void> {
  const cfg = vscode.workspace.getConfiguration();
  for (const [key, value] of Object.entries(POSTURE)) {
    try {
      await cfg.update(key, on ? value : undefined, vscode.ConfigurationTarget.Global);
    } catch {
      // A missing/renamed upstream setting must not break the rest.
    }
  }
  void vscode.window.showInformationMessage(
    on ? "reckon: workbench posture applied (revert any time: “reckon: Revert Workbench Posture”)"
      : "reckon: workbench posture reverted — your defaults are back",
  );
}

/** The first-run offer: once, and "Not now" is remembered. */
export function offerPosture(state: vscode.Memento): void {
  if (state.get<boolean>("reckon.postureOffered")) {
    return;
  }
  void state.update("reckon.postureOffered", true);
  void vscode.window
    .showInformationMessage(
      "reckon: apply the investigation-first workbench posture? (no startup editor, quieter chrome — every bit reversible via “reckon: Revert Workbench Posture”)",
      "Apply", "Not now",
    )
    .then((choice) => {
      if (choice === "Apply") {
        void applyPosture(true);
      }
    });
}
