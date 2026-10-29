// The ⚡ family's wording (ui/02 §2.9, adr/0075): each scoped turn the analyst
// orders from a hypothesis or prediction is an instruction to the agent with
// the hypothesis cited, its live rivals named, and where the turn ends. Pure
// text, so the wording is reviewable and testable apart from the panel.
import type { RivalBrief } from "./protocol";

/** A scoped turn: the instruction sent to the agent and the collapsed label it renders under. */
export interface ScopedTurn {
  instruction: string;
  label: string;
}

/** The short name a hypothesis is referred to by: its handle, else a clipped statement. */
export function hypLabel(handle: string | undefined, statement: string): string {
  if (handle && handle.trim()) {
    return handle.trim();
  }
  return statement.length > 60 ? statement.slice(0, 59) + "…" : statement;
}

/** How a scoped turn cites the hypothesis it acts on: handle + claim + id. */
export function hypCite(handle: string | undefined, statement: string, ref: string): string {
  const h = handle && handle.trim() ? `${handle.trim()}: ` : "";
  return `"${h}${statement}" (${ref})`;
}

/**
 * The rivals clause of a scoped turn: the live competing hypotheses, named by
 * handle and id, followed by the set-wide instruction. Empty when there is no
 * rival — a single-hypothesis investigation is still a legal shape.
 */
export function rivalsClause(rivals: RivalBrief[] | undefined, instruction: string): string {
  if (!rivals || !rivals.length) {
    return "";
  }
  const list = rivals.map((r) => {
    const name = r.handle && r.handle.trim() ? `${r.handle.trim()}: ` : "";
    const pending = r.untestedDiscriminators > 0
      ? ` [${r.untestedDiscriminators} untested discriminating prediction${r.untestedDiscriminators === 1 ? "" : "s"}]`
      : "";
    return `${name}"${r.statement}" (${r.id})${pending}`;
  });
  return `Live rivals: ${list.join("; ")}. ${instruction}`;
}

/**
 * Fire the propose-tests aside (ui/02 §2.9, the "btw" lens): a scoped
 * agent turn the analyst ordered with one click. Same audit record as any
 * turn — the transcript commits to the one thread — but rendered collapsed
 * in the conversation; the predictions it records land on the tracker.
 * The "⚡ Propose tests:" prefix doubles as the cold-restore marker.
 */
export function proposeTestsTurn(hypothesisRef: string, statement: string, handle?: string, rivals?: RivalBrief[]): ScopedTurn {
  const name = hypLabel(handle, statement);
  const instruction = `⚡ Propose tests: for the hypothesis ${hypCite(handle, statement, hypothesisRef)}, `
    + "record the falsifiable predictions that would decide it — one prediction per test, "
    + "each with a concrete test query against the available capability verbs. Prove it or break it. "
    + rivalsClause(rivals, "Read the live rivals too and make at least one prediction DISCRIMINATE — "
      + "opposite expected outcome under this hypothesis and a rival — and set its discriminates to that rival's id. "
      + "A test that comes out the same under both tells us nothing.")
    + "Record them with the tools ONLY. This turn ENDS when the predictions are recorded: do not run any test, "
    + "do not record any prediction outcome, do not record any hypothesis outcome — Run tests and Decide are "
    + "separate steps the analyst orders. Keep your final message to one sentence and do not "
    + "restate the predictions in prose — the tracker renders them.";
  return { instruction, label: `⚡ propose tests · ${name}` };
}

/**
 * Run the untested predictions — the execute half of the drivable loop.
 * They are independent claims, so the agent runs them in parallel where
 * possible; outcomes must cite what was observed (the aggregate refuses a
 * CONFIRMED/DISCONFIRMED without test_result_refs).
 */
export function runTestsTurn(hypothesisRef: string, statement: string, handle?: string, rivals?: RivalBrief[]): ScopedTurn {
  const name = hypLabel(handle, statement);
  const instruction = `⚡ Run tests: execute the UNTESTED predictions under the hypothesis ${hypCite(handle, statement, hypothesisRef)}. `
    + "They are independent — run their declared test queries in parallel where possible, then record each "
    + "outcome (CONFIRMED / DISCONFIRMED / INCONCLUSIVE) citing the observed evidence as test_result_refs. "
    + "Skip any prediction marked manual — a person tests it and I record its result; name it in one clause and move on. "
    + rivalsClause(rivals, "Where a prediction discriminates against a rival, say in one clause which way the result cuts for that rival. ")
    + "This turn ENDS when the outcomes are recorded: do not record a hypothesis outcome — Decide is a separate "
    + "step the analyst orders. Keep your final message to one or two sentences on what was decisive — do not restate the outcomes; the tracker renders them.";
  return { instruction, label: `⚡ run tests · ${name}` };
}

/**
 * Run ONE prediction's declared test — the per-row member of the ⚡ family
 * (adr/0075). Same contract as Run tests, scoped to a single prediction: the
 * outcome is recorded citing evidence, and the turn ends there.
 */
export function testPredictionTurn(m: {
predictionRef: string; predictionName: string; statement: string; queryText?: string; tool?: string;
hypothesisRef: string; hypStatement: string; handle?: string; rivals?: RivalBrief[];
}): ScopedTurn {
  const name = hypLabel(m.handle, m.hypStatement);
  const declared = m.queryText
    ? `Its declared test${m.tool ? " via " + m.tool : ""}: ${m.queryText} `
    : "";
  const instruction = `⚡ Test ${m.predictionName}: run the test for prediction ${m.predictionRef} under the hypothesis ${hypCite(m.handle, m.hypStatement, m.hypothesisRef)} — "${m.statement}". `
    + declared
    + "Record its outcome (CONFIRMED / DISCONFIRMED / INCONCLUSIVE) citing the observed evidence as test_result_refs. "
    + rivalsClause(m.rivals, "If it discriminates against a rival, say in one clause which way the result cuts for that rival. ")
    + "This turn ENDS when that one outcome is recorded: do not run other tests and do not record a hypothesis outcome — Decide is a separate step the analyst orders. "
    + "Keep your final message to one or two sentences on what was decisive; the tracker renders the outcome.";
  return { instruction, label: `⚡ test ${m.predictionName} · ${name}` };
}

/**
 * Decide the hypothesis — the judgment rung, ordered by the analyst once
 * every prediction is decided. The agent weighs the outcomes AGAINST THE
 * RIVALS and records support / refutation / inconclusive citing the
 * decisive test evidence; it declines while a rival's discriminator is
 * untested unless the analyst overrides. Terminal in v0 (01): a changed
 * judgment later is a NEW hypothesis chained via parent_ref.
 */
export function decideTurn(hypothesisRef: string, statement: string, handle?: string, rivals?: RivalBrief[]): ScopedTurn {
  const name = hypLabel(handle, statement);
  const instruction = `⚡ Decide: every prediction under the hypothesis ${hypCite(handle, statement, hypothesisRef)} has been tested. `
    + "Weigh the outcomes and record the hypothesis outcome — support, refutation, or inconclusive — citing the "
    + "decisive test evidence. Be honest about mixed results: confirmed predictions support only what they actually tested. "
    + rivalsClause(rivals, "Decide against the rivals, not by counting confirmations: if a live rival still has an UNTESTED prediction "
      + "that discriminates between you, do NOT record an outcome — the engine refuses it; name that test and ask to run it first (I can run it, withdraw it, or record the outcome myself). ")
    + "Keep your final message to one or two sentences on the judgment and why — the tracker renders the status.";
  return { instruction, label: `⚡ decide · ${name}` };
}
