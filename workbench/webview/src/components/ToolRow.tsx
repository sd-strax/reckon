// A tool call as one line with its args behind the disclosure (02 §2.3). The
// pin lives on the always-visible summary (pinning is the point of a result);
// the citation chips stay in the expanded body. Shared by the live turn and
// the reconstructed transcript so the two can never disagree.
import { useApp } from "../app";
import { RefChip } from "./shared";
import { pinSpec } from "./PromptCard";
import type { PinRow } from "../../../src/backend";

export interface ToolRowResult {
  isError: boolean;
  detail: string;
  coverage?: string;
  refs: string[];
  /** A non-envelope result body to show behind the disclosure. */
  body?: string;
  /** The capability call the result came from (adr/0092). */
  callId?: string;
}

/** A tool result's refs are already pinned when some active pin cites all of
 * them — the same dedup the tracker's pin uses (a UI guard; the engine permits overlap). */
export function refsAlreadyPinned(pins: PinRow[], refs: string[]): boolean {
  if (!refs.length) return false;
  return pins.some((x) => !x.superseded && refs.every((r) => (x.inputRefs || []).includes(r)));
}

export function ToolRow({ name, argsHint, argsPretty, result }: { name: string; argsHint: string; argsPretty: string; result?: ToolRowResult }) {
  const { state, send, openPrompt, openEnableForm, openInspector } = useApp();
  const pins = state.snap?.pins || [];
  const refs = result?.refs || [];
  const pinned = refsAlreadyPinned(pins, refs);
  const enablement = state.snap?.enablement;
  // The gap hint (11 §5.1): the verb came back unavailable AND an installed-
  // but-disabled adapter could serve it. Actionable only through the form.
  let gapAdapter: string | null = null;
  if (result && !result.isError && typeof result.coverage === "string" && result.coverage.indexOf("UNAVAILABLE") === 0 && enablement) {
    const v = (enablement.verbs || []).find((x) => x.verb === name);
    if (v && !v.enabled && (v.closableBy || []).length) gapAdapter = v.closableBy[0];
  }
  return (
    <details className="tool">
      <summary>
        <span className={"mark" + (result ? (result.isError ? " err" : " ok") : "")}>{result ? (result.isError ? "✗" : "✓") : "→"}</span>
        <span className="verb">{name}</span>
        <span className="hint">{argsHint}</span>
        {result ? <span className="hint status">{result.detail}</span> : null}
        {result?.callId ? <button className="linklike opencall" title="Open the capability call: binding, parameters sent, what it produced" onClick={(e) => { e.stopPropagation(); e.preventDefault(); openInspector({ kind: "call", callId: result.callId! }); }}>open call</button> : null}
        {refs.length ? (
          <button
            className={"pincta summaryPin" + (pinned ? " pinnedInert" : "")}
            disabled={pinned}
            title={pinned ? "Already in the pinned evidence — un-pin there to revise" : "Pin this result as evidence (cites all " + refs.length + " refs)"}
            onClick={(e) => {
              e.stopPropagation(); e.preventDefault();
              if (pinned) return;
              openPrompt(pinSpec(refs, undefined, (finding) => send({ type: "pin.add", refs, finding })));
            }}
          >{pinned ? "📌 pinned" : "📌 Pin"}</button>
        ) : null}
      </summary>
      <pre>{argsPretty}</pre>
      {result?.body ? <pre>{result.body}</pre> : null}
      {refs.length ? <div className="toolrefs">{refs.slice(0, 12).map((r) => <RefChip key={r} refId={r} />)}</div> : null}
      {gapAdapter ? (
        <div className="gaphint">
          ⚡ {gapAdapter} could answer this — installed, not enabled.
          <button title="Opens the schema-derived form; you confirm natively — the AI cannot apply config" onClick={(e) => { e.stopPropagation(); openEnableForm(gapAdapter!); }}>Set up…</button>
        </div>
      ) : null}
    </details>
  );
}
