// A host's identity in the Entity inspector (adr/0211): every alias with its
// origin, a source's record or a person's assertion, withdrawn ones dimmed and
// labelled; the hosts a person said it is not; what the record has seen under
// its id; and the analyst's acts, each an inline form: "Same as another
// host…", "Not the same host as…", and Withdraw on an asserted edge. The acts
// are a person's: the agent never records them, and the server refuses a
// delegate.
import { useCallback, useEffect, useState } from "react";
import { useApp } from "../../app";
import { fmtDay } from "../../derive/events";
import type { HostIdentity, IdentityEdge } from "../../../../src/viewTypes";
import { Badge, EntityChip } from "../shared";
import { Section } from "./primitives";

type FormKind = { act: "aliases" | "distinct" } | { act: "withdraw"; edgeId: string };

/** The other host by what the store knows of it, else the tail of its ref. */
function OtherHost({ refId }: { refId: string }) {
  const { state, resolve } = useApp();
  const label = state.labels[refId];
  useEffect(() => { if (label === undefined) resolve(refId); }, [refId, label, resolve]);
  const tail = refId.slice(refId.indexOf("--") + 2, refId.indexOf("--") + 10);
  return <EntityChip refId={refId} text={label || tail + "…"} />;
}

function EdgeRow({ e, onWithdraw }: { e: IdentityEdge; onWithdraw?: () => void }) {
  const origin = e.kind === "asserted"
    ? `asserted by ${e.byName || e.by || "someone"} · ${fmtDay(e.at)}`
    : `from ${e.source || "a source's record"}`;
  return (
    <div className={"irow iedge" + (e.withdrawn ? " withdrawn" : "")} data-edge={e.edgeId}>
      <OtherHost refId={e.otherRef} />
      <span className="imeta">{origin}</span>
      {e.withdrawn ? <Badge>withdrawn</Badge> : null}
      {e.kind === "asserted" && !e.withdrawn && onWithdraw
        ? <button className="iedgeact" title="Withdraw this assertion: the reads follow the record again; the assertion stays on the record, marked" onClick={onWithdraw}>Withdraw…</button>
        : null}
      {e.note ? <span className="inote">“{e.note}”</span> : null}
    </div>
  );
}

function IdentityForm({ kind, refId, onClose }: { kind: FormKind; refId: string; onClose: () => void }) {
  const { state, send, dispatch } = useApp();
  const acts = state.identityActs[refId];
  const [other, setOther] = useState("");
  const [note, setNote] = useState("");
  const [sentAt, setSentAt] = useState<number | null>(null);
  // The form closes once its own act landed; a refusal keeps it open, saying why.
  useEffect(() => { if (sentAt !== null && acts && acts.landed > sentAt) onClose(); }, [acts, sentAt, onClose]);
  const needsOther = kind.act !== "withdraw";
  const title = kind.act === "aliases" ? "Same as another host" : kind.act === "distinct" ? "Not the same host as" : "Withdraw this assertion";
  const confirm = kind.act === "withdraw" ? "Withdraw" : kind.act === "aliases" ? "Join" : "Split";
  const submit = () => {
    if (needsOther && !other.trim()) return;
    dispatch({ type: "identity.sent", ref: refId });
    setSentAt(acts?.landed ?? 0);
    if (kind.act === "withdraw") send({ type: "identity.withdraw", ref: refId, edgeId: kind.edgeId, note: note.trim() || undefined });
    else send({ type: "identity.assert", ref: refId, relation: kind.act, other: other.trim(), note: note.trim() || undefined });
  };
  const pending = sentAt !== null && !!acts?.pending;
  return (
    <form className="iform" aria-label={title} onSubmit={(ev) => { ev.preventDefault(); submit(); }}>
      <span className="kicker">{title}</span>
      {needsOther ? (
        <label>The other host: its name or its ref
          <input type="text" value={other} autoFocus placeholder="WIN-FILE01 or x-host--…" onChange={(ev) => setOther(ev.target.value)} />
        </label>
      ) : null}
      <label>Note (optional)
        <input type="text" value={note} autoFocus={!needsOther} placeholder={needsOther ? "what you checked" : "why"} onChange={(ev) => setNote(ev.target.value)} />
      </label>
      {sentAt !== null && acts?.error ? <div className="iformerr">{acts.error}</div> : null}
      <div className="iformbtns">
        <button type="submit" className="primary" disabled={pending || (needsOther && !other.trim())}>{pending ? "…" : confirm}</button>
        <button type="button" onClick={onClose}>Cancel</button>
      </div>
    </form>
  );
}

export function HostIdentitySection({ refId, identity }: { refId: string; identity: HostIdentity | undefined }) {
  const [form, setForm] = useState<FormKind | null>(null);
  const close = useCallback(() => setForm(null), []);
  const seen = identity?.seenAs;
  const domains = seen?.domains ?? [];
  const facts = !!seen && (domains.length > 0 || seen.agents.length > 0 || seen.ips.length > 0);
  // The ids by source, compactly: "edr a-7, a-8 · ids x-9".
  const bySource = new Map<string, string[]>();
  for (const a of seen?.agents ?? []) bySource.set(a.tool, [...(bySource.get(a.tool) ?? []), a.uid]);
  const withdraw = (e: IdentityEdge) => () => setForm({ act: "withdraw", edgeId: e.edgeId });
  const formFor = (edgeId: string) => form?.act === "withdraw" && form.edgeId === edgeId
    ? <IdentityForm key={"w" + edgeId} kind={form} refId={refId} onClose={close} />
    : null;
  return (
    <>
      <Section title="Also seen as">
        {identity === undefined ? <div className="empty">no identity view for this host</div>
          : !identity.aliases.length ? <div className="imeta">No other host is joined to this one.</div>
          : identity.aliases.map((e) => <div key={e.edgeId}><EdgeRow e={e} onWithdraw={withdraw(e)} />{formFor(e.edgeId)}</div>)}
      </Section>
      {identity?.distinct.length ? (
        <Section title="Not the same host as">
          {identity.distinct.map((e) => <div key={e.edgeId}><EdgeRow e={e} onWithdraw={withdraw(e)} />{formFor(e.edgeId)}</div>)}
        </Section>
      ) : null}
      {facts && seen ? (
        <Section title="Seen under this id">
          {domains.length > 1
            ? <div className="irow iseenwarn">seen under {domains.length} domains: {domains.join(", ")}</div>
            : domains.length === 1 ? <div className="irow"><span className="imeta">domain</span> <span className="mono">{domains[0]}</span></div> : null}
          {bySource.size ? (
            <div className="irow"><span className="imeta">ids</span> <span className="mono">{[...bySource].map(([tool, uids]) => `${tool} ${uids.join(", ")}`).join(" · ")}</span></div>
          ) : null}
          {seen.ips.length ? <div className="irow"><span className="imeta">addresses</span> <span className="mono">{seen.ips.join(", ")}</span></div> : null}
        </Section>
      ) : null}
      <div className="decide">
        <button title="Join this host to one the record did not: writes an aliases edge, on the record with your name and note" onClick={() => setForm({ act: "aliases" })}>Same as another host…</button>
        <button title="Separate this host from one the record wrongly joined, or one you know apart: writes a distinct-from edge" onClick={() => setForm({ act: "distinct" })}>Not the same host as…</button>
      </div>
      {form && form.act !== "withdraw" ? <IdentityForm key={form.act} kind={form} refId={refId} onClose={close} /> : null}
    </>
  );
}
