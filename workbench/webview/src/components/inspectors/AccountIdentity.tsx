// An account's identity in the Entity inspector (adr/0324): the accounts it
// is joined to and kept apart from, each with its origin; what its alias set
// has been seen as; the persons it belongs to, a directory's record or a
// person's assertion, a cut one listed with its mark; and the analyst's acts,
// each an inline form: "Same as another account…", "Not the same account
// as…", "Set owner…", "Not this person's…" on a directory's owner, and
// Withdraw on an assertion. A directory's record is never withdrawn: it is
// cut. The acts are a person's: the agent never records them, and the server
// refuses a delegate.
import { useCallback, useState } from "react";
import type { AccountIdentity, OwnerEdge } from "../../../../src/viewTypes";
import { identityKindOf } from "../../../../src/identityKinds";
import { BusyLine, CutMark, EdgeRow, IdentityForm, StaleLine, withdrawSpec, type ActSpec } from "./IdentityParts";
import { Section } from "./primitives";

/** The other end as typed: a ref of the kind when it is one, else a hint the server looks up. */
export function typedOther(typed: string, prefix: string): { ref: string } | { hint: string } {
  return typed.toLowerCase().startsWith(prefix) ? { ref: typed } : { hint: typed };
}

type FormKind =
  | { act: "aliases" | "distinct" | "owner" }
  // An act from a row is sent on the account the row's edge is on.
  | { act: "cut"; accountRef: string; personRef: string; at: string }
  | { act: "withdraw"; edgeId: string; end: string; at: string };

function accountSpec(refId: string, kind: FormKind): ActSpec {
  switch (kind.act) {
    case "withdraw":
      return withdrawSpec(kind.end, kind.edgeId);
    case "cut":
      return {
        title: "Not this person's account", confirm: "Cut", actRef: kind.accountRef,
        message: (requestId, _other, note) => ({ type: "identity.assert", ref: kind.accountRef, requestId, relation: "distinct", other: { ref: kind.personRef }, note }),
      };
    case "owner":
      return {
        title: "Set owner", confirm: "Set", actRef: refId,
        other: { label: "The person: a principal name, mail, directory id, or its ref", placeholder: "jdoe@contoso.example or identity--…" },
        message: (requestId, other, note) => ({ type: "identity.owner", ref: refId, requestId, person: typedOther(other, "identity--"), note }),
      };
    default: {
      const relation = kind.act;
      return {
        title: relation === "aliases" ? "Same as another account" : "Not the same account as",
        confirm: relation === "aliases" ? "Join" : "Split",
        actRef: refId,
        other: { label: "The other account: its login, principal name, or its ref", placeholder: "CONTOSO\\jdoe or user-account--…" },
        message: (requestId, other, note) => ({ type: "identity.assert", ref: refId, requestId, relation, other: typedOther(other, "user-account--"), note }),
      };
    }
  }
}

/** The ids by source, compactly: "dc S-1-5-21-1001 · idp 8f6a…". */
function idsBySource(ids: { tool: string; uid: string }[]): string {
  const by = new Map<string, string[]>();
  for (const i of ids) by.set(i.tool, [...(by.get(i.tool) ?? []), i.uid]);
  return [...by].map(([tool, uids]) => `${tool} ${uids.join(", ")}`).join(" · ");
}

/** Mounted per account (keyed by its ref): a form opened on one account never carries to another. */
export function AccountIdentitySection({ refId, identity }: { refId: string; identity: AccountIdentity | undefined }) {
  const [form, setForm] = useState<FormKind | null>(null);
  const close = useCallback(() => setForm(null), []);
  const formAt = (at: string) => form && "at" in form && form.at === at
    ? <IdentityForm key={form.act + at} spec={accountSpec(refId, form)} paneRef={refId} onClose={close} />
    : null;
  const withdraw = (e: { edgeId: string }, end: string, at: string) => () => setForm({ act: "withdraw", edgeId: e.edgeId, end, at });
  const accountApart = (identity?.distinct ?? []).filter((d) => identityKindOf(d.otherRef) === "account");
  const personApart = (identity?.distinct ?? []).filter((d) => identityKindOf(d.otherRef) === "person");
  const owners = identity?.owners ?? [];
  // A cut row names its cutting edge and carries its Withdraw; the edges to
  // persons no cut row names (a withdrawn cut, or one over a person with no
  // owner edge here) are listed on their own, so none is hidden.
  const placed = new Set(owners.flatMap((o) => (o.cut && o.cutBy ? [o.cutBy.edgeId] : [])));
  const unplaced = personApart.filter((d) => !placed.has(d.edgeId));
  const liveOwner = owners.some((o) => !o.withdrawn && !o.cut);
  const seen = identity?.seenAs;
  const facts = !!seen && (seen.logins.length > 0 || seen.ids.length > 0 || seen.principalNames.length > 0);
  const rowKey = (o: OwnerEdge) => "o" + o.edgeId + o.accountRef;
  return (
    <>
      <Section title="Also seen as">
        <StaleLine refId={refId} />
        {identity === undefined ? <div className="empty">no identity view for this account</div>
          : !identity.aliases.length ? <div className="imeta">No other account is joined to this one.</div>
          : identity.aliases.map((e) => <div key={e.edgeId}><EdgeRow e={e} other={e.otherRef} onWithdraw={withdraw(e, refId, e.edgeId)} />{formAt(e.edgeId)}</div>)}
      </Section>
      {accountApart.length ? (
        <Section title="Not the same account as">
          {accountApart.map((e) => <div key={e.edgeId}><EdgeRow e={e} other={e.otherRef} onWithdraw={withdraw(e, refId, e.edgeId)} />{formAt(e.edgeId)}</div>)}
        </Section>
      ) : null}
      {facts && seen ? (
        <Section title="Seen under this account">
          {seen.logins.length ? <div className="irow"><span className="imeta">logins</span> <span className="mono">{seen.logins.join(", ")}</span></div> : null}
          {seen.ids.length ? <div className="irow"><span className="imeta">ids</span> <span className="mono">{idsBySource(seen.ids)}</span></div> : null}
          {seen.principalNames.length ? <div className="irow"><span className="imeta">principal names</span> <span className="mono">{seen.principalNames.join(", ")}</span></div> : null}
        </Section>
      ) : null}
      {identity ? (
        <Section title="Belongs to">
          {owners.map((o) => {
            const at = rowKey(o);
            return (
              <div key={at}>
                {/* The owner edge's end is the account of the set it starts from: the withdrawal names it. */}
                <EdgeRow e={o} other={o.personRef} onWithdraw={withdraw(o, o.accountRef, at)}>
                  {o.cut ? <CutMark cutBy={o.cutBy} accountRef={o.accountRef} personRef={o.personRef} onWithdraw={(edgeId, end) => setForm({ act: "withdraw", edgeId, end, at })} /> : null}
                  {o.kind === "observed" && !o.withdrawn && !o.cut
                    ? <button className="iedgeact" title="A directory's record is not withdrawn: assert that this account is not this person's, a distinct-from on the record with your name and note" onClick={() => setForm({ act: "cut", accountRef: o.accountRef, personRef: o.personRef, at })}>Not this person's…</button>
                    : null}
                </EdgeRow>
                {formAt(at)}
              </div>
            );
          })}
          {unplaced.map((e) => (
            <div key={e.edgeId}>
              <EdgeRow e={e} other={e.otherRef} onWithdraw={withdraw(e, refId, e.edgeId)}>
                <span className="imeta">asserted not this account's person</span>
              </EdgeRow>
              {formAt(e.edgeId)}
            </div>
          ))}
          {!liveOwner ? <div className="imeta">No person owns this account.</div> : null}
        </Section>
      ) : null}
      <div className="decide">
        <button title="Join this account to one the record did not: writes an aliases edge, on the record with your name and note" onClick={() => setForm({ act: "aliases" })}>Same as another account…</button>
        <button title="Separate this account from one the record wrongly joined, or one you know apart: writes a distinct-from edge" onClick={() => setForm({ act: "distinct" })}>Not the same account as…</button>
        <button title="Say whose account this is: writes a belongs-to edge, on the record with your name and note" onClick={() => setForm({ act: "owner" })}>Set owner…</button>
      </div>
      <BusyLine refId={refId} word="account" />
      {form && !("at" in form) ? <IdentityForm key={form.act} spec={accountSpec(refId, form)} paneRef={refId} onClose={close} /> : null}
    </>
  );
}
