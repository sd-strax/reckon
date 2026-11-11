// The pieces every identity section shares (adr/0211, adr/0324): the other
// end's chip, an edge's row with its origin, the request ids, and the inline
// form an act is sent from and settled in. The acts are a person's: the
// agent never records them, and the server refuses a delegate.
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useApp } from "../../app";
import { fmtDay } from "../../derive/events";
import type { CutEdge, IdentityEdge } from "../../../../src/viewTypes";
import type { InboundMessage } from "../../../../src/protocol";
import { identityActPending } from "../../store";
import { Badge, EntityChip } from "../shared";

/** The other end by what the store knows of it, else the tail of its ref. */
export function OtherEntity({ refId }: { refId: string }) {
  const { state, resolve } = useApp();
  const label = state.labels[refId];
  useEffect(() => { if (label === undefined) resolve(refId); }, [refId, label, resolve]);
  return <EntityChip refId={refId} text={label || refTail(refId)} />;
}

/** The first characters of a ref's uuid, for a chip whose label is not known yet. */
export function refTail(ref: string): string {
  const at = ref.indexOf("--") + 2;
  return ref.slice(at, at + 8) + "…";
}

/** An edge's origin and its marks, everything but the other end. */
export type EdgeOrigin = Omit<IdentityEdge, "otherRef">;

/** Where an edge came from: a source's record, or a person's assertion and its day. */
export function originText(e: EdgeOrigin): string {
  return e.kind === "asserted"
    ? `asserted by ${e.byName || e.by || "someone"} · ${fmtDay(e.at)}`
    : `from ${e.source || "a source's record"}`;
}

const withdrawTitle = "Withdraw this assertion: the reads follow the record again; the assertion stays on the record, marked";

/**
 * One edge: the other end, its origin, a withdrawn one dimmed and labelled,
 * Withdraw on a live assertion, the note. `children` carries the marks and
 * acts a row kind adds (a cut, "Not this person's…").
 */
export function EdgeRow({ e, other, onWithdraw, children }: { e: EdgeOrigin; other: string; onWithdraw?: () => void; children?: ReactNode }) {
  return (
    <div className={"irow iedge" + (e.withdrawn ? " withdrawn" : "")} data-edge={e.edgeId}>
      <OtherEntity refId={other} />
      <span className="imeta">{originText(e)}</span>
      {e.withdrawn ? <Badge>withdrawn</Badge> : null}
      {children}
      {e.kind === "asserted" && !e.withdrawn && onWithdraw
        ? <button className="iedgeact" title={withdrawTitle} onClick={onWithdraw}>Withdraw…</button>
        : null}
      {e.note ? <span className="inote">“{e.note}”</span> : null}
    </div>
  );
}

let requestSeq = 0;

/** A request id for one identity act, unique in this page. */
export function nextRequestId(): string {
  requestSeq += 1;
  return `identity-${Date.now().toString(36)}-${requestSeq}`;
}

/** One inline act: what it is called, what it sends, on which entity. */
export interface ActSpec {
  title: string;
  confirm: string;
  /** The entity the act is on: its lock, one act at a time. */
  actRef: string;
  /** The input naming the other end; absent when the act names it already. */
  other?: { label: string; placeholder: string };
  notePlaceholder?: string;
  /** The message to send under requestId, with the other end as typed. */
  message: (requestId: string, other: string, note: string | undefined) => InboundMessage;
}

/** A withdraw of one asserted edge, answered with the view of `ref`, one of its ends. */
export function withdrawSpec(ref: string, edgeId: string): ActSpec {
  return {
    title: "Withdraw this assertion", confirm: "Withdraw", actRef: ref, notePlaceholder: "why",
    message: (requestId, _other, note) => ({ type: "identity.withdraw", ref, requestId, edgeId, note }),
  };
}

/** A candidate's label: an account's login and type, a person's label or ref. */
function CandidateName({ refId }: { refId: string }) {
  const { state, resolve } = useApp();
  const label = state.labels[refId];
  useEffect(() => { if (label === undefined) resolve(refId); }, [refId, label, resolve]);
  return <>{label || refId}</>;
}

/**
 * The inline form an act is sent from (adr/0211). Settled by its own act's
 * outcome only: it closes once its act landed with the view; a refusal, or a
 * landed act whose view could not be read, keeps it open, saying so. A
 * refusal over a hint that named several lists them: picking one fills the
 * ref, and the analyst sends it.
 */
export function IdentityForm({ spec, paneRef, onClose }: { spec: ActSpec; paneRef: string; onClose: () => void }) {
  const { state, send, dispatch } = useApp();
  const [other, setOther] = useState("");
  const [note, setNote] = useState("");
  // The act this form sent: only its own outcome settles the form.
  const [requestId, setRequestId] = useState<string | null>(null);
  const own = requestId ? state.identityActs[requestId] : undefined;
  // Closing the form forgets its act's entry (once it settles, if in flight).
  const sent = useRef<string | null>(null);
  useEffect(() => () => { if (sent.current) dispatch({ type: "identity.forget", requestId: sent.current }); }, [dispatch]);
  // One act at a time per entity: while any is in flight, no form submits.
  const busy = identityActPending(state, spec.actRef) || identityActPending(state, paneRef);
  useEffect(() => { if (own?.state === "landed" && !own.message) onClose(); }, [own, onClose]);
  const needsOther = !!spec.other;
  const landed = own?.state === "landed";
  const submit = () => {
    if (busy || landed || (needsOther && !other.trim())) return;
    const id = nextRequestId();
    dispatch({ type: "identity.sent", ref: spec.actRef, requestId: id });
    sent.current = id;
    setRequestId(id);
    send(spec.message(id, other.trim(), note.trim() || undefined));
  };
  const pending = own?.state === "pending";
  const failed = own?.state === "failed";
  const candidates = failed && needsOther
    ? [...(own.accounts ?? []).map((a) => ({ ref: a.ref, text: <>{a.accountLogin} · {a.accountType}</> })),
      ...(own.persons ?? []).map((p) => ({ ref: p, text: <CandidateName refId={p} /> }))]
    : [];
  return (
    <form className="iform" aria-label={spec.title} onSubmit={(ev) => { ev.preventDefault(); submit(); }}>
      <span className="kicker">{spec.title}</span>
      {spec.other ? (
        <label>{spec.other.label}
          <input type="text" value={other} autoFocus placeholder={spec.other.placeholder} onChange={(ev) => setOther(ev.target.value)} />
        </label>
      ) : null}
      <label>Note (optional)
        <input type="text" value={note} autoFocus={!needsOther} placeholder={spec.notePlaceholder ?? (needsOther ? "what you checked" : "why")} onChange={(ev) => setNote(ev.target.value)} />
      </label>
      {failed && own.message ? <div className="iformerr">{own.message}</div> : null}
      {candidates.length ? (
        <div className="icands" aria-label="Candidates">
          <span className="imeta">Pick one, then send it:</span>
          {candidates.map((c) => (
            <button key={c.ref} type="button" className="linklike icand" title={"Name this one by its ref: " + c.ref} onClick={() => setOther(c.ref)}>{c.text}</button>
          ))}
        </div>
      ) : null}
      {landed && own?.message ? <div className="imeta" role="status">{own.message}</div> : null}
      <div className="iformbtns">
        {landed ? null : <button type="submit" className="primary" disabled={busy || (needsOther && !other.trim())}>{pending ? "…" : spec.confirm}</button>}
        <button type="button" onClick={onClose}>{landed ? "Close" : "Cancel"}</button>
      </div>
    </form>
  );
}

const cutTitle = "A person asserted that this account is not this person's: the edge is listed, and the reads do not follow it";
const withdrawCutTitle = "Withdraw the assertion that this account is not this person's: the reads follow the ownership again";

/**
 * The mark a cut row carries (adr/0324): listed, never hidden. The cutting
 * edge may be on an account aliased to the row's, or against a person
 * aliased to the row's: the mark then names the ends it was asserted on.
 * Its Withdraw names the edge and its account end, one of the edge's ends.
 */
export function CutMark({ cutBy, accountRef, personRef, onWithdraw }: { cutBy?: CutEdge; accountRef: string; personRef: string; onWithdraw: (edgeId: string, end: string) => void }) {
  const viaAccount = cutBy && cutBy.accountRef !== accountRef ? cutBy.accountRef : undefined;
  const viaPerson = cutBy && cutBy.personRef !== personRef ? cutBy.personRef : undefined;
  return (
    <>
      <span className="icut" title={cutTitle}>cut · not this person's, by assertion</span>
      {viaAccount || viaPerson ? (
        <span className="imeta icutvia">cut via {viaAccount ? <OtherEntity refId={viaAccount} /> : null}{viaAccount && viaPerson ? " / " : null}{viaPerson ? <OtherEntity refId={viaPerson} /> : null}</span>
      ) : null}
      {cutBy ? <button className="iedgeact" title={withdrawCutTitle} onClick={() => onWithdraw(cutBy.edgeId, cutBy.accountRef)}>Withdraw the cut…</button> : null}
    </>
  );
}

/** The line a section shows while its held view may be superseded. */
export function StaleLine({ refId }: { refId: string }) {
  const { state } = useApp();
  return state.staleEntities[refId] ? <div className="imeta" role="status">This view may be out of date.</div> : null;
}

/** The line a section shows while an act on its entity is in flight. */
export function BusyLine({ refId, word }: { refId: string; word: string }) {
  const { state } = useApp();
  return identityActPending(state, refId)
    ? <div className="imeta" role="status">An act on this {word} is in flight; the next can be sent once it settles.</div>
    : null;
}
