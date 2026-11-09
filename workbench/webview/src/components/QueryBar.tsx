// The knowledge query bar (ui/09 §9.2, adr/0107): one box over both corpora,
// mounted in the Knowledge view and in the Library inside an investigation.
// Search only for now: it runs recall over procedures and past cases and
// lists what came back, relevance in words; nothing is summarized and
// nothing is recorded in any investigation. Enter searches, Escape clears.
import { useState } from "react";

export function QueryBar({ onSearch, onClear, showing }: {
  onSearch: (text: string) => void;
  onClear: () => void;
  /** Results are on screen: the bar offers Clear. */
  showing: boolean;
}) {
  const [q, setQ] = useState("");
  // Results dismissed from outside the bar (a "back" link) take the text with
  // them: the change is seen during render, React's pattern for state that
  // follows a prop.
  const [wasShowing, setWasShowing] = useState(showing);
  if (wasShowing !== showing) {
    setWasShowing(showing);
    if (!showing) setQ("");
  }
  const search = () => { const text = q.trim(); if (text) onSearch(text); };
  const clear = () => { setQ(""); onClear(); };
  return (
    <div className="ksearch">
      <input type="text" value={q} placeholder="Search procedures and past cases…" aria-label="Search procedures and past cases"
        onChange={(e) => setQ(e.target.value)}
        onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); search(); } if (e.key === "Escape") clear(); }} />
      <button disabled={!q.trim()} onClick={search}>Search</button>
      {showing ? <button className="ghost" onClick={clear}>Clear</button> : null}
    </div>
  );
}
