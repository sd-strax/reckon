// @vitest-environment node
// The lifecycle status folds PARTIAL into SUCCEEDED and TIMEOUT into FAILED,
// so neither word is ever a status (adr/0361). They appear only where the
// recorded outcome is read: outcomeStatus and the readers keyed by it. A new
// line naming either word, quoted or as a key, is a reader of the folded
// status by mistake, or a new outcome reader to add here on purpose.
import { readdirSync, readFileSync, statSync } from "fs";
import * as path from "path";
import { describe, expect, it } from "vitest";

const WORKBENCH = path.join(__dirname, "..", "..");

// Lines naming either word, by file, comments excluded. A generated wire file
// (adr/0278) lists the words Go sends, and reads none, so it is not counted.
const ALLOWED: Record<string, number> = {
  // outcomeStatus (2), the badge (2), failedOutcome, takenOverBy's partial rule.
  "webview/src/derive/actions.ts": 6,
  // willDoKicker, failureSentence (2), the status glyphs (one line), reversalLabel.
  "webview/src/components/ActionCard.tsx": 5,
  // The tile words for a partial and a timed-out send.
  "webview/src/derive/summary.ts": 2,
  // A capability call's coverage, not an action: its tone, and the copy on
  // a partial empty answer (adr/0061).
  "webview/src/components/inspectors/CallInspector.tsx": 1,
  "webview/src/components/inspectors/EvidenceInspector.tsx": 1,
};

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = path.join(dir, f);
    if (statSync(p).isDirectory()) return sources(p);
    return /\.tsx?$/.test(f) && !/\.(test|generated)\.tsx?$/.test(f) ? [p] : [];
  });
}

/** The code of a file with its comments blanked, one entry per line. */
function codeLines(text: string): string[] {
  const noBlocks = text.replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, " "));
  return noBlocks.split("\n").map((l) => l.replace(/(^|[^:"'`\\])\/\/.*$/, "$1"));
}

describe("the outcome words", () => {
  it("appear only where the recorded outcome is read", () => {
    const counts: Record<string, number> = {};
    for (const dir of ["webview/src", "src"]) {
      for (const file of sources(path.join(WORKBENCH, dir))) {
        const rel = path.relative(WORKBENCH, file).split(path.sep).join("/");
        const n = codeLines(readFileSync(file, "utf8")).filter((l) => /\b(PARTIAL|TIMEOUT)\b/.test(l)).length;
        if (n) counts[rel] = n;
      }
    }
    expect(counts).toEqual(ALLOWED);
  });
});
