// One reusable prompt card for every text-input action (pin, un-pin, reject,
// T3 challenge, adopt, restate, record result …): an in-context modal over the
// document, never the top-of-window quick input (13 §3).
import { useEffect, useRef, useState } from "react";
import { useApp } from "../app";
import { RefChip } from "./shared";

export interface PromptChoice { id: string; label: string; title?: string; checked?: boolean }

export interface PromptExtraApi {
  setInput: (text: string) => void;
  setExtra: (id: string, value: string | boolean) => void;
}

export interface PromptExtra {
  kind: "select" | "text" | "check";
  id: string;
  label: string;
  title?: string;
  value?: string | boolean;
  placeholder?: string;
  options?: { value: string; label: string }[];
  onChange?: (value: string | boolean, api: PromptExtraApi) => void;
}

export interface PromptSpec {
  title: string;
  label?: string;
  placeholder?: string;
  helper?: string;
  confirm?: string;
  value?: string;
  selectAll?: boolean;
  refs?: string[];
  /** Default true: the confirm button needs a non-empty value. */
  require?: boolean;
  choicesLabel?: string;
  choices?: PromptChoice[];
  extrasTop?: PromptExtra[];
  extras?: PromptExtra[];
  onConfirm: (value: string, picked: string[], extras: Record<string, string | boolean>) => void;
}

type ExtraValues = Record<string, string | boolean>;

function initialExtras(list: PromptExtra[] | undefined): ExtraValues {
  const out: ExtraValues = {};
  for (const x of list || []) {
    out[x.id] = x.kind === "check" ? !!x.value : (typeof x.value === "string" ? x.value : (x.kind === "select" ? (x.options?.[0]?.value ?? "") : ""));
  }
  return out;
}

export function PromptCard({ spec }: { spec: PromptSpec }) {
  const { closePrompt } = useApp();
  const [value, setValue] = useState(spec.value || "");
  const [picked, setPicked] = useState<string[]>((spec.choices || []).filter((c) => c.checked).map((c) => c.id));
  const [extras, setExtras] = useState<ExtraValues>({ ...initialExtras(spec.extrasTop), ...initialExtras(spec.extras) });
  const input = useRef<HTMLTextAreaElement>(null);
  const require = spec.require !== false;

  useEffect(() => {
    input.current?.focus();
    if (spec.selectAll) input.current?.select();
  }, [spec]);

  const api: PromptExtraApi = {
    setInput: (text) => setValue(text),
    setExtra: (id, v) => setExtras((e) => ({ ...e, [id]: v })),
  };

  const submit = () => {
    const v = value.trim();
    if (require && !v) return;
    const cb = spec.onConfirm;
    closePrompt();
    cb(v, picked, extras);
  };

  const renderExtras = (list: PromptExtra[] | undefined, id: string) => {
    if (!list || !list.length) return null;
    return (
      <div className="promptextras" id={id}>
        {list.map((x) => {
          const fieldId = "promptExtra_" + x.id;
          const v = extras[x.id];
          const onChange = (nv: string | boolean) => {
            setExtras((e) => ({ ...e, [x.id]: nv }));
            x.onChange?.(nv, api);
          };
          let field: React.ReactNode;
          if (x.kind === "select") {
            field = (
              <select id={fieldId} value={String(v ?? "")} onChange={(e) => onChange(e.target.value)}>
                {(x.options || []).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
              </select>
            );
          } else if (x.kind === "check") {
            field = <input id={fieldId} type="checkbox" checked={!!v} onChange={(e) => onChange(e.target.checked)} />;
          } else {
            field = <input id={fieldId} type="text" value={String(v ?? "")} placeholder={x.placeholder || ""} onChange={(e) => onChange(e.target.value)} />;
          }
          const label = <label htmlFor={fieldId} title={x.title}>{x.label}</label>;
          return (
            <div key={x.id} className={"pextra" + (x.kind === "check" ? " pcheck" : "")}>
              {x.kind === "check" ? <>{field}{label}</> : <>{label}{field}</>}
            </div>
          );
        })}
      </div>
    );
  };

  const refs = spec.refs || [];
  return (
    <div className="dialog" id="promptDialog" onKeyDown={(e) => { if (e.key === "Escape") closePrompt(); }}>
      <div className="dlg" role="dialog" aria-label={spec.title}>
        <h3>{spec.title}</h3>
        {renderExtras(spec.extrasTop, "promptExtrasTop")}
        {spec.label ? <div className="dlglabel">{spec.label}</div> : null}
        <textarea
          ref={input}
          id="promptInput"
          rows={3}
          value={value}
          placeholder={spec.placeholder || ""}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) submit(); }}
        />
        {refs.length ? (
          <div className="pinrefs">
            {refs.slice(0, 12).map((r) => <RefChip key={r} refId={r} />)}
            {refs.length > 12 ? <span className="cardmeta">+{refs.length - 12} more</span> : null}
          </div>
        ) : null}
        {(spec.choices || []).length ? (
          <div className="promptchoices">
            {spec.choicesLabel ? <div className="dlglabel">{spec.choicesLabel}</div> : null}
            {spec.choices!.map((c) => (
              <label key={c.id} className="choice">
                <input
                  type="checkbox"
                  checked={picked.includes(c.id)}
                  onChange={(e) => setPicked((p) => e.target.checked ? [...p, c.id] : p.filter((x) => x !== c.id))}
                />
                <span title={c.title || c.label}>{c.label}</span>
              </label>
            ))}
          </div>
        ) : null}
        {renderExtras(spec.extras, "promptExtras")}
        {spec.helper ? <div className="pinhelp">{spec.helper}</div> : null}
        <div className="decide">
          <button className="primary" disabled={require && value.trim() === ""} onClick={submit}>{spec.confirm || "Confirm"}</button>
          <button onClick={closePrompt}>Cancel</button>
        </div>
      </div>
    </div>
  );
}

/** Pin evidence: the finding note + cited refs. `suggested` prefills the finding
 * (visible and editable — a suggestion, never a silently-applied value). */
export function pinSpec(refs: string[], suggested: string | undefined, onConfirm: (finding: string) => void): PromptSpec {
  return {
    title: "Pin as evidence",
    label: "What does this evidence show?",
    placeholder: "e.g. First external egress from the svc_backup source host, 15s after the encoded PowerShell",
    helper: "Recorded on the investigation thread and cited by your verdict.",
    confirm: "📌 Pin evidence",
    refs,
    value: suggested || "",
    onConfirm: (finding) => onConfirm(finding),
  };
}
