// A host's identity in the Entity inspector (adr/0211): every alias with its
// origin, a source's record or a person's assertion, withdrawn ones dimmed and
// labelled; the hosts a person said it is not; what the record has seen under
// its id; and the analyst's acts, each an inline form: "Same as another
// host…", "Not the same host as…", and Withdraw on an asserted edge. The acts
// are a person's: the agent never records them, and the server refuses a
// delegate.
import { useCallback, useState } from "react";
import type { HostIdentity, IdentityEdge } from "../../../../src/viewTypes";
import { BusyLine, EdgeRow, IdentityForm, StaleLine, withdrawSpec, type ActSpec } from "./IdentityParts";
import { Section } from "./primitives";

type FormKind = { act: "aliases" | "distinct" } | { act: "withdraw"; edgeId: string };

/** The act a host's form sends: the other host by its name or its ref, as typed. */
function hostSpec(refId: string, kind: FormKind): ActSpec {
  if (kind.act === "withdraw") return withdrawSpec(refId, kind.edgeId);
  const relation = kind.act;
  return {
    title: relation === "aliases" ? "Same as another host" : "Not the same host as",
    confirm: relation === "aliases" ? "Join" : "Split",
    actRef: refId,
    other: { label: "The other host: its name or its ref", placeholder: "WIN-FILE01 or x-host--…" },
    message: (requestId, other, note) => ({ type: "identity.assert", ref: refId, requestId, relation, other, note }),
  };
}

/** Mounted per host (keyed by its ref): a form opened on one host never carries to another. */
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
    ? <IdentityForm key={"w" + edgeId} spec={hostSpec(refId, form)} paneRef={refId} onClose={close} />
    : null;
  return (
    <>
      <Section title="Also seen as">
        <StaleLine refId={refId} />
        {identity === undefined ? <div className="empty">no identity view for this host</div>
          : !identity.aliases.length ? <div className="imeta">No other host is joined to this one.</div>
          : identity.aliases.map((e) => <div key={e.edgeId}><EdgeRow e={e} other={e.otherRef} onWithdraw={withdraw(e)} />{formFor(e.edgeId)}</div>)}
      </Section>
      {identity?.distinct.length ? (
        <Section title="Not the same host as">
          {identity.distinct.map((e) => <div key={e.edgeId}><EdgeRow e={e} other={e.otherRef} onWithdraw={withdraw(e)} />{formFor(e.edgeId)}</div>)}
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
      <BusyLine refId={refId} word="host" />
      {form && form.act !== "withdraw" ? <IdentityForm key={form.act} spec={hostSpec(refId, form)} paneRef={refId} onClose={close} /> : null}
    </>
  );
}
