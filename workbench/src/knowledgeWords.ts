// The words the knowledge library is offered in, written once (adr/0344): the
// request "Ask reckon to use this" stages in an investigation's composer, and
// the "cited in N investigations" reading. The sidebar's Knowledge view and
// the Knowledge view's rows both read them. Pure, and types only, so the
// extension host and the webview bundle both compile it.

/** What a library row names: a procedure by its SOP id, a past case by its investigation ref. */
export interface KnowledgeRef {
  kind: "sop" | "case";
  ref: string;
  title: string;
}

/**
 * The request staged in the composer, never sent: the analyst reviews it and
 * sends it, and the agent recalls the item through its tools (adr/0129).
 */
export function askToUseText(k: KnowledgeRef): string {
  return k.kind === "sop"
    ? `Use the procedure “${k.title}” (SOP ${k.ref}) for this investigation: recall it and apply what fits.`
    : `Compare this investigation with the past case “${k.title}” (${k.ref}): recall it and tell me what carries over.`;
}

/** Distinct investigations with an act that cited the item (adr/0109). */
export function citedInWords(n: number): string {
  return `cited in ${n} investigation${n === 1 ? "" : "s"}`;
}
