// A person's identity in the Entity inspector (adr/0324): the persons it is
// joined to and kept apart from, each with its origin; the accounts attached
// to it, a directory's record or a person's assertion, a cut one listed with
// its mark; the hosts the retrieved events name beside its accounts; and the
// analyst's acts: "Same person as…", "Not the same person as…", "Not this
// person's…" on a directory's account, and Withdraw on an assertion. The
// acts are a person's: the agent never records them, and the server refuses
// a delegate.
import { useCallback, useState } from "react";
import { useApp } from "../../app";
import type { PersonAccountEdge, PersonIdentity } from "../../../../src/viewTypes";
import { identityKindOf } from "../../../../src/identityKinds";
import { typedOther } from "./AccountIdentity";
import { BusyLine, CutMark, EdgeRow, IdentityForm, OtherEntity, StaleLine, withdrawSpec, type ActSpec } from "./IdentityParts";
import { Section } from "./primitives";

type FormKind =
  | { act: "aliases" | "distinct" }
  // An act from a row is sent on the row's own ends: its account, and the
  // person the edge is on (the one in view, or one aliased to it).
  | { act: "cut"; accountRef: string; personRef: string; at: string }
  | { act: "withdraw"; edgeId: string; end: string; at: string };

function personSpec(refId: string, kind: FormKind): ActSpec {
  switch (kind.act) {
    case "withdraw":
      return withdrawSpec(kind.end, kind.edgeId);
    case "cut":
      // The cut is sent on the account, the person as the other end: its
      // scope is the account's (adr/0324).
      return {
        title: "Not this person's account", confirm: "Cut", actRef: kind.accountRef,
        message: (requestId, _other, note) => ({ type: "identity.assert", ref: kind.accountRef, requestId, relation: "distinct", other: { ref: kind.personRef }, note }),
      };
    default: {
      const relation = kind.act;
      return {
        title: relation === "aliases" ? "Same person as" : "Not the same person as",
        confirm: relation === "aliases" ? "Join" : "Split",
        actRef: refId,
        other: { label: "The other person: a principal name, mail, directory id, or its ref", placeholder: "jdoe@contoso.example or identity--…" },
        message: (requestId, other, note) => ({ type: "identity.assert", ref: refId, requestId, relation, other: typedOther(other, "identity--"), note }),
      };
    }
  }
}

/**
 * The hosts the retrieved events of this investigation name beside one of
 * the accounts, live ones only: a lens over the events the webview holds,
 * never a read of its own.
 */
function hostsUsed(events: { entityRefs: string[] }[], accounts: Set<string>): string[] {
  // accounts holds every account of each live row's alias set: an event that
  // names an alias of an owned account is that account's.
  const out: string[] = [];
  for (const e of events) {
    if (!e.entityRefs.some((r) => accounts.has(r))) continue;
    for (const r of e.entityRefs) if (identityKindOf(r) === "host" && !out.includes(r)) out.push(r);
  }
  return out;
}

/** Mounted per person (keyed by its ref): a form opened on one person never carries to another. */
export function PersonIdentitySection({ refId, identity }: { refId: string; identity: PersonIdentity | undefined }) {
  const { state } = useApp();
  const [form, setForm] = useState<FormKind | null>(null);
  const close = useCallback(() => setForm(null), []);
  const formAt = (at: string) => form && "at" in form && form.at === at
    ? <IdentityForm key={form.act + at} spec={personSpec(refId, form)} paneRef={refId} onClose={close} />
    : null;
  // A withdrawal names one of the edge's ends: the person for its own edges.
  const withdraw = (e: { edgeId: string }, at: string) => () => setForm({ act: "withdraw", edgeId: e.edgeId, end: refId, at });
  const personApart = (identity?.distinct ?? []).filter((d) => identityKindOf(d.otherRef) === "person");
  const accountApart = (identity?.distinct ?? []).filter((d) => identityKindOf(d.otherRef) === "account");
  const accounts = identity?.accounts ?? [];
  // A cut row names its cutting edge and carries its Withdraw; the edges to
  // accounts no cut row names (a withdrawn cut, or one over an account not
  // attached here) are listed on their own, so none is hidden.
  const placed = new Set(accounts.flatMap((a) => (a.cut && a.cutBy ? [a.cutBy.edgeId] : [])));
  const unplaced = accountApart.filter((d) => !placed.has(d.edgeId));
  const live = new Set(accounts.filter((a) => !a.withdrawn && !a.cut).flatMap((a) => (a.accountAliases.length ? a.accountAliases : [a.accountRef])));
  // Over the events the webview holds, and says so when they are a page of more.
  const held = state.snap?.events ?? [];
  const total = state.snap?.eventsTotal ?? held.length;
  const hosts = hostsUsed(held, live);
  const covers = total > held.length
    ? `named by events that name one of these accounts, among the newest ${held.length} of ${total} retrieved events in this investigation`
    : "named by the retrieved events in this investigation that name one of these accounts";
  const rowKey = (a: PersonAccountEdge) => "a" + a.edgeId;
  return (
    <>
      <Section title="Same person as">
        <StaleLine refId={refId} />
        {identity === undefined ? <div className="empty">no identity view for this person</div>
          : !identity.aliases.length ? <div className="imeta">No other person is joined to this one.</div>
          : identity.aliases.map((e) => <div key={e.edgeId}><EdgeRow e={e} other={e.otherRef} onWithdraw={withdraw(e, e.edgeId)} />{formAt(e.edgeId)}</div>)}
      </Section>
      {personApart.length ? (
        <Section title="Not the same person as">
          {personApart.map((e) => <div key={e.edgeId}><EdgeRow e={e} other={e.otherRef} onWithdraw={withdraw(e, e.edgeId)} />{formAt(e.edgeId)}</div>)}
        </Section>
      ) : null}
      {identity ? (
        <Section title="Accounts">
          {accounts.map((a) => {
            const at = rowKey(a);
            return (
              <div key={at}>
                {/* The edge's person end names the withdrawal: the person in view, or one aliased to it. */}
                <EdgeRow e={a} other={a.accountRef} onWithdraw={() => setForm({ act: "withdraw", edgeId: a.edgeId, end: a.personRef || refId, at })}>
                  {a.personRef && a.personRef !== refId ? <span className="imeta ivia">via <OtherEntity refId={a.personRef} /></span> : null}
                  {a.cut ? <CutMark cutBy={a.cutBy} accountRef={a.accountRef} personRef={a.personRef || refId} onWithdraw={(edgeId, end) => setForm({ act: "withdraw", edgeId, end, at })} /> : null}
                  {a.kind === "observed" && !a.withdrawn && !a.cut
                    ? <button className="iedgeact" title="A directory's record is not withdrawn: assert that this account is not this person's, a distinct-from on the record with your name and note" onClick={() => setForm({ act: "cut", accountRef: a.accountRef, personRef: a.personRef || refId, at })}>Not this person's…</button>
                    : null}
                </EdgeRow>
                {formAt(at)}
              </div>
            );
          })}
          {unplaced.map((e) => (
            <div key={e.edgeId}>
              <EdgeRow e={e} other={e.otherRef} onWithdraw={withdraw(e, e.edgeId)}>
                <span className="imeta">asserted not this person's</span>
              </EdgeRow>
              {formAt(e.edgeId)}
            </div>
          ))}
          {!accounts.length && !unplaced.length ? <div className="imeta">No account is attached to this person.</div> : null}
        </Section>
      ) : null}
      {hosts.length ? (
        <Section title="Hosts used">
          <div className="imeta">{covers}</div>
          <div className="irow ihosts">{hosts.map((h) => <OtherEntity key={h} refId={h} />)}</div>
        </Section>
      ) : null}
      <div className="decide">
        <button title="Join this person to one the record did not: writes an aliases edge, on the record with your name and note" onClick={() => setForm({ act: "aliases" })}>Same person as…</button>
        <button title="Separate this person from one the record wrongly joined, or one you know apart: writes a distinct-from edge" onClick={() => setForm({ act: "distinct" })}>Not the same person as…</button>
      </div>
      <BusyLine refId={refId} word="person" />
      {form && !("at" in form) ? <IdentityForm key={form.act} spec={personSpec(refId, form)} paneRef={refId} onClose={close} /> : null}
    </>
  );
}
