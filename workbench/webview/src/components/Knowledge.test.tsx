// Component tests for the Knowledge document (09 §9.1–§9.5, §9.7; impl §2.6)
// over the harness stage built from the demo pack: the sections and their
// counts, procedures with the detail, the editor, retire with a reason, the
// query bar and the results, import with the dry-run preview and the
// outcomes, the Work | Knowledge toggle as a navigation act on both surfaces.
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { KnowledgeApp } from "./knowledge/KnowledgeApp";
import { HarnessTransport, type HarnessKnowledge, type HarnessSnapshot } from "../../../harness/transport";
import knowledge from "../../../harness/snapshots/knowledge.json";
import synthetic from "../../../harness/snapshots/synthetic.json";

const kdata = knowledge as unknown as HarnessKnowledge;
const idata = synthetic as unknown as HarnessSnapshot;

function mountKnowledge() {
  const transport = new HarnessTransport(kdata);
  render(<AppProvider transport={transport} surface="knowledge"><KnowledgeApp /></AppProvider>);
  return transport;
}
const main = () => document.querySelector("#kmain") as HTMLElement;
const nav = () => document.querySelector("#leftRegion") as HTMLElement;
const section = (name: string) => fireEvent.click(within(nav()).getByText(name));
const dialog = () => screen.getByRole("dialog");

describe("the Knowledge document", () => {
  it("lists the sections with their counts and shows procedures with retired ones hidden by default", () => {
    mountKnowledge();
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Knowledge");
    expect(document.querySelector("#querybar")).toBeTruthy();
    expect(document.querySelector("#companion")).toBeNull(); // no investigation surface here
    const secs = Array.from(nav().querySelectorAll(".ksec")).map((s) => s.textContent);
    expect(secs).toEqual(["Procedures3", "Past cases—", "Import4", "Candidates—"]);
    expect(main().querySelectorAll("tbody tr").length).toBe(3);
    fireEvent.click(within(main()).getByLabelText("show retired"));
    const rows = main().querySelectorAll("tbody tr");
    expect(rows.length).toBe(4);
    expect(rows[3].classList.contains("retired")).toBe(true);
    expect(within(main()).getByText("rev 3")).toBeTruthy(); // the retired row: no source version, so rev N
    expect(within(main()).getAllByText("v1.0").length).toBeGreaterThan(0); // the source's own version wins
  });

  it("opens a procedure beside the list, edits it as a new revision and copies its link", () => {
    const transport = mountKnowledge();
    const first = kdata.knowledge.sops[0];
    fireEvent.click(within(main()).getByText(first.title));
    expect(transport.intents.find((i) => i.type === "sop.get")).toEqual({ type: "sop.get", sopId: first.id });
    const detail = main().querySelector(".kdetail") as HTMLElement;
    expect(within(detail).getByRole("heading", { level: 3 }).textContent).toBe(first.title);
    expect(within(detail).getByText("Frontmatter")).toBeTruthy();
    expect(within(detail).getByText(first.source!.url)).toBeTruthy();
    fireEvent.click(within(detail).getByRole("tab", { name: /^Versions/ }));
    expect(within(detail).getByText(/rev 2 · current/)).toBeTruthy();
    fireEvent.click(within(detail).getByRole("tab", { name: "Where used" }));
    expect(within(detail).getByText(/gap 00 §6.22/)).toBeTruthy();
    fireEvent.click(within(detail).getByText("Copy link"));
    expect(transport.intents.find((i) => i.type === "copy")).toEqual({ type: "copy", text: "reckon://procedure/" + first.id });
    fireEvent.click(within(detail).getByText("Edit"));
    const md = within(detail).getByLabelText("Markdown") as HTMLTextAreaElement;
    fireEvent.change(md, { target: { value: first.body + "\n\n## Added step\n" } });
    expect(within(detail).getByText("Added step")).toBeTruthy(); // the live preview
    fireEvent.click(within(detail).getByText("Save as a new revision"));
    const save = transport.intents.find((i) => i.type === "sop.save") as { body: string; title: string; tags: string[] };
    expect(save.body.endsWith("## Added step\n")).toBe(true);
    expect(save.title).toBe(first.title);
    expect(save.tags).toEqual(first.tags);
    // The harness re-posted the slices; the editor closed on the fresh detail.
    expect(detail.querySelector(".keditor")).toBeNull();
    expect(within(detail).getByRole("tab", { name: "Versions · 3" })).toBeTruthy();
  });

  it("retires with a reason and keeps the record readable", () => {
    const transport = mountKnowledge();
    const first = kdata.knowledge.sops[0];
    fireEvent.click(within(main()).getByText(first.title));
    fireEvent.click(within(main()).getByText("Retire…"));
    fireEvent.change(within(dialog()).getByRole("textbox"), { target: { value: "superseded by the new runbook" } });
    fireEvent.click(within(dialog()).getByText("Retire"));
    expect(transport.intents.find((i) => i.type === "sop.retire")).toEqual({ type: "sop.retire", sopId: first.id, reason: "superseded by the new runbook" });
    expect(main().querySelectorAll("tbody tr").length).toBe(2); // hidden from the default list
    const detail = main().querySelector(".kdetail") as HTMLElement;
    expect(within(detail).getByText(/Retired .* by analyst-1/)).toBeTruthy();
    expect(within(detail).getByText("superseded by the new runbook")).toBeTruthy();
    expect(within(detail).queryByText("Edit")).toBeNull();
    expect(nav().querySelector(".ksec")?.textContent).toBe("Procedures2");
  });

  it("searches both corpora, opens a result in the inspector without an In-this-case fold, and goes back", () => {
    const transport = mountKnowledge();
    fireEvent.change(document.querySelector("#queryText")!, { target: { value: "rdp lateral movement" } });
    fireEvent.change(document.querySelector("#queryTags")!, { target: { value: "T1021.001" } });
    fireEvent.keyDown(document.querySelector("#queryText")!, { key: "Enter" });
    expect(transport.intents.find((i) => i.type === "knowledge.query")).toEqual({ type: "knowledge.query", mode: "search", text: "rdp lateral movement", filters: { tags: ["T1021.001"], includeRetired: undefined } });
    expect(within(main()).getByText(/Results for “rdp lateral movement”/)).toBeTruthy();
    expect(within(main()).getByText(/2 procedures · 2 past cases · vector-cosine/)).toBeTruthy();
    expect(within(main()).getByText("NEAR_DUPLICATE")).toBeTruthy();
    expect(within(main()).getByText("DISTINCT")).toBeTruthy();
    fireEvent.click(within(main()).getByText(kdata.search!.procedures[0].title));
    const pane = document.querySelector(".inspector.active") as HTMLElement;
    expect(pane.getAttribute("aria-label")).toBe("Procedure");
    expect(pane.querySelector(".ithiscase")).toBeNull();
    expect(within(pane).queryByText("Open in the Knowledge view")).toBeNull();
    fireEvent.click(within(main()).getByText(kdata.search!.cases[0].title));
    const casePane = document.querySelector(".inspector.active") as HTMLElement;
    expect(casePane.getAttribute("aria-label")).toBe("Similar past case");
    expect(within(casePane).getByText("What happened there")).toBeTruthy();
    fireEvent.click(within(main()).getByText(/back to Procedures/));
    expect(main().querySelectorAll("tbody tr").length).toBe(3);
  });

  it("Ask shows the gap line until §6.23 lands; Past cases and Candidates show theirs", () => {
    mountKnowledge();
    fireEvent.click(screen.getByRole("tab", { name: "Ask" }));
    fireEvent.change(document.querySelector("#queryText")!, { target: { value: "what do we do about rdp?" } });
    fireEvent.click(within(document.querySelector("#querybar") as HTMLElement).getByRole("button", { name: "Ask" }));
    expect(within(main()).getByText(/Ask lands with gap 00 §6.23/)).toBeTruthy();
    section("Past cases");
    expect(within(main()).getByText(/gap 00 §6.21/)).toBeTruthy();
    section("Candidates");
    expect(within(main()).getByText(/gap 00 §6.24/)).toBeTruthy();
  });

  it("Import: editor and disk rows with the parsed frontmatter, missing fields and the parser's error; outcomes after the run", () => {
    const transport = mountKnowledge();
    section("Import");
    expect(within(main()).getByText("Open in the editor").querySelector(".imeta")?.textContent).toBe("1");
    expect(within(main()).getByText("From disk").querySelector(".imeta")?.textContent).toBe("3");
    expect(within(main()).getByText("unsaved edits")).toBeTruthy();
    expect(within(main()).getByText("missing title, author, tags, source url")).toBeTruthy();
    expect(within(main()).getByText(/will revise/).textContent).toContain("Host Isolation Runbook");
    expect(within(main()).getByText(/parse sop document: empty body/)).toBeTruthy();
    const run = within(main()).getByText("Import 3 documents");
    fireEvent.click(within(main()).getByLabelText("Import notes.md"));
    expect(transport.intents.find((i) => i.type === "import.toggle")).toMatchObject({ checked: true });
    expect(run.textContent).toBe("Import 4 documents");
    fireEvent.click(within(main()).getByText("Add files…"));
    expect(transport.intents.find((i) => i.type === "import.choose")).toEqual({ type: "import.choose", folder: false });
    expect(within(main()).getByText("chosen-5.md")).toBeTruthy();
    fireEvent.click(within(main()).getByText("Import 5 documents"));
    expect(transport.intents.find((i) => i.type === "import.run")).toBeTruthy();
    expect(within(main()).getAllByText("added").length).toBe(3);
    expect(within(main()).getByText("revised")).toBeTruthy();
    expect(within(main()).getByText("failed")).toBeTruthy();
    expect(within(main()).getByText("4 imported, 1 failed — see each row.")).toBeTruthy();
  });

  it("the left region's Work mode lists the queue and opens an investigation; a reveal lands on a section", () => {
    const transport = mountKnowledge();
    fireEvent.click(within(nav()).getByRole("tab", { name: "Work" }));
    expect(transport.intents.find((i) => i.type === "queue.get")).toBeTruthy();
    const rows = nav().querySelectorAll(".qrow:not(.case)");
    expect(rows.length).toBe(2);
    fireEvent.click(rows[0]);
    expect(transport.intents.find((i) => i.type === "investigation.open")).toBeTruthy();
    expect(nav().querySelector("#capsFold")).toBeNull(); // the capability fold belongs to an investigation
    fireEvent.click(within(nav()).getByRole("tab", { name: "Knowledge" }));
    expect(nav().querySelectorAll(".ksec").length).toBe(4);
    act(() => transport.emit({ type: "knowledge.reveal", section: "import" }));
    expect(main().getAttribute("aria-label")).toBe("Import");
    expect(nav().querySelector(".ksec.current")?.textContent).toBe("Import4");
  });

  it("the error banner shows the host's message until the next slice", () => {
    const transport = mountKnowledge();
    act(() => transport.emit({ type: "knowledge.error", message: "Could not load the procedures — 503" }));
    expect(screen.getByRole("alert").textContent).toBe("Could not load the procedures — 503");
    act(() => transport.emit({ type: "knowledge.state", state: { counts: { procedures: 3, cases: null, candidates: null } } }));
    expect(screen.queryByRole("alert")).toBeNull();
  });
});

describe("the investigation document's way to the Knowledge view", () => {
  it("the Knowledge toggle and the section link reveal the document instead of swapping the region", () => {
    const transport = new HarnessTransport(idata);
    render(<AppProvider transport={transport}><App /></AppProvider>);
    const region = document.querySelector("#leftRegion") as HTMLElement;
    fireEvent.click(within(region).getByRole("tab", { name: "Knowledge" }));
    expect(transport.intents.find((i) => i.type === "knowledge.open")).toEqual({ type: "knowledge.open" });
    expect(within(region).getByRole("tab", { name: "Work" }).getAttribute("aria-selected")).toBe("true");
    expect(region.querySelectorAll(".qrow").length).toBeGreaterThan(0);
    fireEvent.click(screen.getByText("Open the Knowledge view"));
    expect(transport.intents.filter((i) => i.type === "knowledge.open").pop()).toEqual({ type: "knowledge.open", section: "procedures" });
  });
});
