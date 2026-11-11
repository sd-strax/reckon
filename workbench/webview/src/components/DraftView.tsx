// The unseeded state (design/ui/02 §2.7): a full-cover overlay where the
// analyst roots the investigation. NOT a wizard — one line in the same kind of
// composer, with a live interpretation and a one-click correction. The alert
// and case paths are disclosure toggles on the same card (13 §3: the case
// filter is a field here, never the top-of-window quick input).
import { useEffect, useRef, useState } from "react";
import { useApp } from "../app";
import { classifyDraft, effectiveDraftKind, effectiveEntityKind, entitySubLabel, type DraftKind, type SeedEntityKind } from "../derive/draft";

const entityKinds: SeedEntityKind[] = ["host", "account", "person"];

type SeedPath = "alert" | "case" | null;

export function DraftView() {
  const { state, send, dispatch } = useApp();
  const d = state.draft;
  const [value, setValue] = useState("");
  const [override, setOverride] = useState<DraftKind | null>(null);
  // The analyst's kind for an entity token (adr/0324); null is the default
  // from the token's shape, as the server derives it.
  const [entityKind, setEntityKind] = useState<SeedEntityKind | null>(null);
  // A persisted account picked from the server's list (adr/0324): seeded by
  // its ref, never re-resolved from the login the box shows. Typing drops it.
  const [picked, setPicked] = useState<{ ref: string; type: string } | null>(null);
  const [path, setPath] = useState<SeedPath>(d.mode === "case" ? "case" : null);
  const [alertId, setAlertId] = useState("");
  const [alertSource, setAlertSource] = useState("");
  const [filter, setFilter] = useState("");
  const input = useRef<HTMLTextAreaElement>(null);
  const caseFilter = useRef<HTMLInputElement>(null);
  const alertInput = useRef<HTMLInputElement>(null);
  const searched = useRef(false);

  const search = () => {
    dispatch({ type: "draft.searching", status: d.caseQueue ? "Reading the " + d.caseQueue + " queue…" : "Searching the case system of record…" });
    send({ type: "seed.caseSearch", filter, queue: d.caseQueue || undefined });
  };

  useEffect(() => {
    if (path === "alert") alertInput.current?.focus();
    else if (path === "case") {
      caseFilter.current?.focus();
      // A configured queue is the analyst's starting point: open on it.
      if (d.caseQueue && !searched.current) { searched.current = true; search(); }
    } else input.current?.focus();
    // Focus and the queue search follow the path alone: re-running when the
    // queue or search changes would steal focus while the analyst types.
    // eslint-disable-next-line react-hooks/exhaustive-deps -- deliberately keyed on path
  }, [path]);

  const v = value.trim();
  const kind = picked ? "entity" : effectiveDraftKind(v, override);
  const singleToken = !/\s/.test(v) && !v.endsWith("?");
  // The kind an entity seed is sent with: host, account or person for a
  // name; none for a question, an address or a hash.
  const ek = kind === "entity" && !picked ? effectiveEntityKind(v, entityKind) : undefined;
  const seedMessage = (allowUnknown?: boolean) => picked
    ? { type: "seed.submit" as const, value: v, kind: "entity", accountRef: picked.ref, ...(allowUnknown ? { allowUnknown } : {}) }
    : {
      type: "seed.submit" as const, value: v, kind: classifyDraft(v) ? kind : "",
      ...(allowUnknown ? { allowUnknown } : {}), ...(ek ? { entityKind: ek } : {}),
    };
  const submit = () => {
    if (!v || d.submitting) return;
    dispatch({ type: "draft.submitting" });
    send(seedMessage());
  };
  // A known account picked from a refusal: its login in the box for the
  // analyst to see, its ref kept to seed on; the analyst presses Start.
  const pickAccount = (a: { ref: string; accountLogin: string; accountType: string }) => {
    setValue(a.accountLogin || a.accountType || "account");
    setOverride(null);
    setEntityKind(null);
    setPicked({ ref: a.ref, type: a.accountType });
    input.current?.focus();
  };
  const toggle = (p: SeedPath) => setPath((cur) => (cur === p ? null : p));
  const where = d.caseQueue ? " in the " + d.caseQueue + " queue" : " in the system of record";

  return (
    <div id="draftView">
      <div className="draftCard">
        <h2>What are you investigating?</h2>
        <p className="draftLead">A host, an account, a person, an IP, or a file hash — or a question to hunt.</p>
        <textarea
          ref={input}
          id="draftInput"
          rows={1}
          value={value}
          placeholder="WIN-FILE01   ·   185.220.101.5   ·   a file hash   —   or a question"
          onChange={(e) => { setValue(e.target.value); setOverride(null); setEntityKind(null); setPicked(null); }}
          onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); submit(); } }}
        />
        <div className="draftInterp">
          {!v ? null : picked ? (
            <>→ investigate account <b>{v}</b> <span className="imeta">the listed {picked.type ? picked.type + " " : ""}account</span></>
          ) : kind === "entity" ? (
            <>→ investigate {ek ?? entitySubLabel(v)} <b>{v}</b>
              {ek ? (
                <span className="draftKinds" role="group" aria-label="What the name is">
                  {entityKinds.map((k) => (
                    <button key={k} type="button" className={"linklike draftKind" + (k === ek ? " on" : "")} aria-pressed={k === ek} onClick={() => setEntityKind(k)}>{k}</button>
                  ))}
                </span>
              ) : null}
              <span className="toggle" onClick={() => setOverride("question")}>hunt this as a question instead</span></>
          ) : (
            <>→ a hunt (open question){singleToken ? <span className="toggle" onClick={() => setOverride("entity")}>investigate a specific host/IP instead</span> : null}</>
          )}
        </div>
        <div className="draftActions">
          <button className="primary" disabled={!v || d.submitting} onClick={submit}>Start investigation</button>
          <button className="linklike" aria-pressed={path === "alert"} onClick={() => toggle("alert")}>From an alert…</button>
          <button className="linklike" aria-pressed={path === "case"} onClick={() => toggle("case")}>From a case…</button>
        </div>
        {path === "alert" ? (
          <div id="draftAlertForm">
            <input ref={alertInput} type="text" placeholder="alert id in its tool (e.g. EDR-ALERT-7741)" value={alertId} onChange={(e) => setAlertId(e.target.value)} />
            <input type="text" placeholder="which tool raised it (e.g. crowdstrike-edr)" value={alertSource} onChange={(e) => setAlertSource(e.target.value)} />
            <button className="primary" disabled={!(alertId.trim() && alertSource.trim()) || d.submitting} onClick={() => {
              if (d.submitting) return;
              dispatch({ type: "draft.submitting" });
              send({ type: "seed.alert", alertId: alertId.trim(), source: alertSource.trim() });
            }}>Start from alert</button>
          </div>
        ) : null}
        {path === "case" ? (
          <div id="draftCaseForm">
            <div className="draftCaseSearch">
              <input
                ref={caseFilter}
                id="draftCaseFilter"
                type="text"
                value={filter}
                placeholder={d.caseQueue ? "narrow the " + d.caseQueue + " queue — e.g. rdp (blank = the whole queue)" : "filter your case system of record (blank = recent) — e.g. reimage"}
                onChange={(e) => setFilter(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); search(); } }}
              />
              <button className="primary" onClick={search}>Search cases</button>
            </div>
            <div className="draftCaseStatus">
              {d.status ? d.status
                : d.cases === null ? ""
                : d.cases.length ? d.cases.length + " case" + (d.cases.length === 1 ? "" : "s") + where + " — pick one to investigate."
                : (filter.trim() ? "No matching cases" : "No open cases") + where + "."}
            </div>
            <div className="draftCaseResults">
              {(d.cases || []).map((c) => (
                <button key={c.number} className="caseRow" onClick={() => {
                  if (d.submitting) return;
                  dispatch({ type: "draft.submitting" });
                  dispatch({ type: "draft.searching", status: "Seeding from " + c.number + "…" });
                  send({ type: "seed.case", caseNumber: c.number });
                }}>
                  <span className="caseNum">{c.number}</span>
                  <span className="caseStatus">{c.status || ""}</span>
                  <span className="caseTitle">{c.title || ""}</span>
                </button>
              ))}
            </div>
          </div>
        ) : null}
        <div className="draftErr">{d.error}
          {d.error && d.unknownEntity && v ? <button className="linklike" disabled={d.submitting} onClick={() => { dispatch({ type: "draft.submitting" }); send(seedMessage(true)); }}>Start anyway</button> : null}
          {d.error && d.knownAccounts?.length ? (
            <div className="draftKnown" aria-label="Accounts the record holds under this name">
              {d.knownAccounts.map((a) => (
                <button key={a.ref} type="button" className="linklike draftKnownAccount" title="Put this account's login in the box; then start" onClick={() => pickAccount(a)}>{a.accountLogin} · {a.accountType}</button>
              ))}
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}
