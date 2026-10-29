// The composer: Enter sends, Shift+Enter newline. Parked honestly on closed
// records (the engine would refuse the commit anyway). Staged text (the pivot
// pattern, "Ask reckon to use this" from the library) lands here for review —
// never auto-fired.
import { useEffect, useRef, useState } from "react";
import { useApp } from "../app";

export function Composer() {
  const { state, send, busy, setBusy, staged } = useApp();
  const [text, setText] = useState("");
  const input = useRef<HTMLTextAreaElement>(null);
  const engine = state.snap ? String(state.snap.investigation.state).toUpperCase() : "";
  const closed = engine === "ARCHIVED" || engine === "CONCLUDED" || engine === "DISCARDED";
  const disabled = closed || busy || !state.snap;

  useEffect(() => {
    if (staged && !disabled) {
      setText(staged.text);
      const el = input.current;
      if (el) { el.focus(); requestAnimationFrame(() => el.setSelectionRange(staged.text.length, staged.text.length)); }
    }
  }, [staged, disabled]);

  useEffect(() => { if (!disabled) input.current?.focus(); }, [disabled]);

  const submit = () => {
    const t = text.trim();
    if (!t || disabled) return;
    setText("");
    setBusy(true);
    send({ type: "send", text: t });
  };

  const placeholder = engine === "ARCHIVED" ? "Archived — this record is final."
    : engine === "DISCARDED" ? "Discarded — this draft was set aside and accepts no further changes."
    : engine === "CONCLUDED" ? "Concluded — reopen to continue the conversation."
    : "Ask reckon to investigate… (Enter to send, Shift+Enter for newline)";

  return (
    <div id="composer">
      <textarea
        ref={input}
        id="input"
        rows={1}
        value={text}
        disabled={disabled}
        placeholder={placeholder}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); submit(); } }}
      />
      <button id="send" disabled={disabled} onClick={submit}>Send</button>
    </div>
  );
}
