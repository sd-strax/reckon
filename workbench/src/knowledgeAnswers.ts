// The procedure and past-case inspector answers (08 §8.6, §8.6.1), shared by
// the investigation document and the Knowledge view (adr/0107) so the two
// read them the same way. The document is the one read that must succeed;
// where it was cited (adr/0109), the outline (adr/0303) and, inside an
// investigation, what the agent read of it as it was (adr/0097) ride beside
// it, and a failed one leaves the pane without that part, never closed.
import type { BackendClient, ThreadEntry } from "./backend";
import { procedureReads } from "./procedureReads";
import type { RenderMessage } from "./protocol";
import { errText } from "./util";

export interface AnswerHost {
  client: BackendClient;
  log: { debug(message: string): void };
}

type ProcedureAnswer = Extract<RenderMessage, { type: "inspector.procedure" }>;
type CaseAnswer = Extract<RenderMessage, { type: "inspector.case" }>;

/**
 * One procedure for the inspector. `thread` is the investigation's committed
 * thread, read on demand; absent in the Knowledge view, where there is no
 * investigation and so nothing the agent read.
 */
export async function procedureAnswer(host: AnswerHost, sopId: string, thread?: () => Promise<ThreadEntry[]>): Promise<ProcedureAnswer> {
  const quiet = (what: string) => (err: unknown): undefined => { host.log.debug(`${what} ${sopId}: ${errText(err)}`); return undefined; };
  const citations = host.client.knowledge.sopCitations(sopId).catch(quiet("citations"));
  const reads = thread
    ? thread().then((t) => procedureReads(host.client.knowledge, sopId, t)).catch(quiet("procedure reads"))
    : Promise.resolve(undefined);
  const sop = await host.client.knowledge.sop(sopId);
  // The outline is read for the revision the body is, through the revision
  // route: two independent "current" reads could straddle an edit and show
  // one revision's body under another's headings.
  const outline = sop.revision
    ? await host.client.knowledge.revisionOutline(sopId, sop.revision).catch(quiet("outline"))
    : undefined;
  return { type: "inspector.procedure", sopId, sop, citations: await citations, outline, reads: await reads };
}

/** One past case for the inspector: its summary (null when the corpus holds none) and where it was cited. */
export async function caseAnswer(host: AnswerHost, investigationRef: string): Promise<CaseAnswer> {
  const [summary, citations] = await Promise.all([
    host.client.knowledge.caseSummary(investigationRef),
    host.client.knowledge.caseCitations(investigationRef).catch((err: unknown) => { host.log.debug(`citations ${investigationRef}: ${errText(err)}`); return undefined; }),
  ]);
  return { type: "inspector.case", investigationRef, summary, citations };
}
