import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { KnowledgeInboundMessage, KnowledgeRenderMessage } from "../../../src/protocol";
import type { PastCaseRow } from "../../../src/viewTypes";
import { FakeTransport } from "../test/fakeTransport";
import { KnowledgeApp } from "./KnowledgeApp";

const open = () => {
  const t = new FakeTransport<KnowledgeInboundMessage, KnowledgeRenderMessage>();
  render(<KnowledgeApp transport={t} />);
  return t;
};

const rdp: PastCaseRow = {
  ref: "grouping--a", title: "RDP lateral movement", concludedAt: "2026-05-14T18:20:00Z", disposition: "MALICIOUS",
  techniques: ["T1021.001", "T1078"], entities: ["FINANCE-07", "DC-02"],
  actions: [{ actionType: "host.isolate", targetCount: 1, outcome: "SUCCEEDED" }], durationHours: 6.5, citedIn: 1, narrative: true,
};
const creds: PastCaseRow = {
  ref: "grouping--b", title: "Credential dumping", concludedAt: "2026-06-02T09:00:00Z", disposition: "SUSPICIOUS",
  techniques: ["T1003"], entities: ["HR-03"], actions: [], citedIn: 0, narrative: false,
};

function library(t: FakeTransport<KnowledgeInboundMessage, KnowledgeRenderMessage>) {
  act(() => t.emit({
    type: "library",
    sops: [{ kind: "sop", ref: "sop-1", title: "Containment", detail: "rdp", citedIn: 2 }],
    cases: [{ kind: "case", ref: rdp.ref, title: rdp.title, citedIn: 1 }, { kind: "case", ref: creds.ref, title: creds.title, citedIn: 0 }],
    casesTotal: 150, // the corpus, beyond the listing's page
  }));
  act(() => t.emit({ type: "pastCases.facets", facets: { techniques: [{ value: "T1078", count: 1 }, { value: "T1003", count: 1 }], dispositions: [{ value: "MALICIOUS", count: 1 }, { value: "SUSPICIOUS", count: 1 }] } }));
}

const lastCasesAsk = (t: FakeTransport<KnowledgeInboundMessage, KnowledgeRenderMessage>) =>
  [...t.intents].reverse().find((i) => i.type === "pastCases.get");

describe("the Knowledge view (adr/0107)", () => {
  it("asks for the library on mount and lists procedures with their counts, never offering to act on an investigation", () => {
    const t = open();
    expect(t.intents).toEqual([{ type: "ready" }]);
    library(t);
    const nav = screen.getByRole("navigation", { name: "Knowledge sections" });
    expect(within(nav).getByText("Procedures").textContent).toContain("1");
    expect(within(nav).getByText("Past cases").textContent).toContain("150"); // the corpus total, not the page
    expect(screen.getByText("Containment")).toBeTruthy();
    expect(screen.getByText(/cited in 2 investigations/)).toBeTruthy();
    expect(screen.queryByText("Ask reckon to use this")).toBeNull(); // the view acts on no investigation (ui/09 §9.1)
  });

  it("lists past cases through the API's filter, says how many match, and every filter change asks again", () => {
    const t = open();
    library(t);
    fireEvent.click(screen.getByRole("button", { name: /^Past cases/ }));
    expect(lastCasesAsk(t)).toEqual({ type: "pastCases.get", filter: { techniques: [] } });
    act(() => t.emit({ type: "pastCases", filter: { techniques: [] }, rows: [creds, rdp], total: 2 }));
    expect(screen.getByText("2 past cases")).toBeTruthy();
    const row = screen.getByText("RDP lateral movement").closest("tr") as HTMLElement;
    expect(within(row).getByText("2026-05-14")).toBeTruthy();
    expect(within(row).getByText("malicious")).toBeTruthy();
    expect(within(row).getByText("T1021.001, T1078")).toBeTruthy();
    expect(within(row).getByText("FINANCE-07, DC-02")).toBeTruthy();
    expect(within(row).getByText(/· SUCCEEDED/)).toBeTruthy(); // the recorded outcome word, verbatim
    expect(within(row).getByText("6.5 h")).toBeTruthy();
    expect(within(row).getByText("1 investigation")).toBeTruthy();

    // A technique chip, then the disposition and the days: each is a new ask.
    fireEvent.click(screen.getByRole("button", { name: /T1078/ }));
    expect(lastCasesAsk(t)).toEqual({ type: "pastCases.get", filter: { techniques: ["T1078"] } });
    act(() => t.emit({ type: "pastCases", filter: { techniques: ["T1078"] }, rows: [rdp], total: 1 }));
    expect(screen.getByText("1 of 150 match")).toBeTruthy();
    expect(screen.queryByText("Credential dumping")).toBeNull();

    fireEvent.change(screen.getByRole("combobox"), { target: { value: "MALICIOUS" } });
    expect(lastCasesAsk(t)).toMatchObject({ filter: { techniques: ["T1078"], disposition: "MALICIOUS" } });
    fireEvent.change(screen.getByLabelText("Concluded from"), { target: { value: "2026-05-01" } });
    fireEvent.change(screen.getByLabelText("to"), { target: { value: "2026-05-31" } });
    expect(lastCasesAsk(t)).toEqual({ type: "pastCases.get", filter: { techniques: ["T1078"], disposition: "MALICIOUS", from: "2026-05-01", to: "2026-05-31" } });

    // An answer for a filter no longer on screen is not shown under it.
    act(() => t.emit({ type: "pastCases", filter: { techniques: ["T9"] }, rows: [rdp], total: 1 }));
    expect(screen.queryByText("RDP lateral movement")).toBeNull();
    act(() => t.emit({ type: "pastCases", filter: { techniques: ["T1078"], disposition: "MALICIOUS", from: "2026-05-01", to: "2026-05-31" }, rows: [], total: 0 }));
    expect(screen.getByText("No past case matches these filters.")).toBeTruthy();
    fireEvent.click(screen.getByText("Clear filters"));
    expect(lastCasesAsk(t)).toEqual({ type: "pastCases.get", filter: { techniques: [] } });

    act(() => t.emit({ type: "pastCases", filter: { techniques: [] }, rows: [], total: 0, error: "backend down" }));
    expect(screen.getByText(/could not be read: backend down/)).toBeTruthy();
  });

  it("opens a past case beside the list without investigation parts, and names the investigation it came from as a link", () => {
    const t = open();
    library(t);
    fireEvent.click(screen.getByRole("button", { name: /^Past cases/ }));
    act(() => t.emit({ type: "pastCases", filter: { techniques: [] }, rows: [rdp], total: 1 }));
    fireEvent.click(screen.getByText("RDP lateral movement"));
    expect(t.intents).toContainEqual({ type: "inspector.open", target: { kind: "case", investigationRef: rdp.ref } });
    act(() => t.emit({
      type: "inspector.case", investigationRef: rdp.ref,
      summary: { investigationRef: rdp.ref, title: rdp.title, body: "What happened.", techniques: rdp.techniques, primaryEntities: rdp.entities, actionsTaken: [], tags: [], revision: 1, learnedProcedures: [] },
      citations: { ref: rdp.ref, citedIn: 1, recalledIn: 1, investigations: [
        { investigationId: "inv-1", title: "Later case", status: "ACTIVE", cited: true, lastAt: "2026-07-01T00:00:00Z", acts: 1, revisions: [] },
        { investigationId: "inv-2", title: "Another", status: "CONCLUDED", cited: false, lastAt: "2026-07-02T00:00:00Z", acts: 1, revisions: [] },
      ] },
    }));
    const pane = screen.getByRole("region", { name: "Similar past case" });
    expect(within(pane).getByRole("heading", { level: 2 }).textContent).toBe("RDP lateral movement"); // named from the answer, with no thread to name it
    expect(within(pane).queryByText(/In this case/)).toBeNull();
    expect(within(pane).queryByRole("tab", { name: "What the agent read" })).toBeNull();
    fireEvent.click(within(pane).getByRole("tab", { name: "Where cited · 1 of 2" }));
    expect(within(pane).getByText("Later case")).toBeTruthy();
    expect(within(pane).getByText("Another")).toBeTruthy(); // nothing is "elsewhere" here: every investigation is listed
    fireEvent.click(within(pane).getByText("Open that investigation"));
    expect(t.intents).toContainEqual({ type: "investigation.open", id: "a", title: rdp.title });
  });

  it("opens a procedure with its outline, and no investigation parts", () => {
    const t = open();
    library(t);
    fireEvent.click(screen.getByText("Containment"));
    expect(t.intents).toContainEqual({ type: "inspector.open", target: { kind: "sop", sopId: "sop-1" } });
    act(() => t.emit({
      type: "inspector.procedure", sopId: "sop-1",
      sop: { id: "sop-1", title: "Containment", body: "# Containment\n\nIsolate.", tags: ["rdp"], status: "published" },
      outline: { revision: 3, cut: "1/16000", sections: [{ idx: 1, location: "Containment", headingPath: ["Containment"] }, { idx: 2, location: "Containment > Triage", headingPath: ["Containment", "Triage"] }] },
    }));
    const pane = screen.getByRole("region", { name: "Procedure" });
    expect(within(pane).queryByText(/In this case/)).toBeNull();
    expect(within(pane).queryByRole("tab", { name: "What the agent read" })).toBeNull();
    expect(within(pane).getByText("Outline · 2 sections")).toBeTruthy();
    expect(within(pane).getByText("Containment > Triage")).toBeTruthy();
    expect(within(pane).getByText("3")).toBeTruthy(); // the revision the outline is of
  });

  it("searches both corpora, shows the results under the query with a way back, and imports", () => {
    const t = open();
    library(t);
    fireEvent.change(screen.getByLabelText("Search procedures and past cases"), { target: { value: "rdp" } });
    fireEvent.click(screen.getByText("Search"));
    expect(t.intents).toContainEqual({ type: "library.search", text: "rdp" });
    act(() => t.emit({ type: "library.results", query: "rdp", sops: [{ kind: "sop", ref: "sop-1", title: "Containment", relevance: "close match" }], cases: [] }));
    expect(screen.getByText("Results for “rdp”")).toBeTruthy();
    expect(screen.getByText("no past case matches")).toBeTruthy();
    fireEvent.click(screen.getByText("‹ back to Procedures"));
    expect(screen.queryByText("Results for “rdp”")).toBeNull();
    expect((screen.getByLabelText("Search procedures and past cases") as HTMLInputElement).value).toBe(""); // the text goes with the results

    fireEvent.click(screen.getByRole("button", { name: "Import" }));
    fireEvent.click(screen.getByText("Import procedures…"));
    expect(t.intents).toContainEqual({ type: "library.import" });
  });

  it("shows a failed load with Retry, which clears it and asks again", () => {
    const t = open();
    act(() => t.emit({ type: "error", message: "backend API v9 unsupported" }));
    expect(screen.getByRole("alert").textContent).toContain("backend API v9 unsupported");
    fireEvent.click(screen.getByText("Retry"));
    expect(t.intents).toContainEqual({ type: "refresh" });
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("copies a record's JSON, opens the case title from the keyboard, and says nothing waits for the count when it was not read", () => {
    const t = open();
    act(() => t.emit({ type: "library", sops: [], cases: [] })); // the case read failed: no total
    const nav = screen.getByRole("navigation", { name: "Knowledge sections" });
    expect(within(nav).getByText("Past cases").textContent).toBe("Past cases");
    fireEvent.click(screen.getByRole("button", { name: /^Past cases/ }));
    act(() => t.emit({ type: "pastCases", filter: { techniques: [] }, rows: [rdp], total: 1 }));
    expect(screen.getByText("1 past case")).toBeTruthy();
    const title = screen.getByRole("button", { name: "RDP lateral movement" }); // a button: reachable by keyboard
    fireEvent.click(title);
    act(() => t.emit({ type: "inspector.case", investigationRef: rdp.ref, summary: { investigationRef: rdp.ref, title: rdp.title, body: "b", techniques: [], primaryEntities: [], actionsTaken: [], tags: [], revision: 1, learnedProcedures: [] } }));
    const pane = screen.getByRole("region", { name: "Similar past case" });
    fireEvent.click(within(pane).getByText("Raw"));
    fireEvent.click(within(pane).getByText("Copy"));
    expect(t.intents.some((i) => i.type === "copy")).toBe(true);
  });

  it("says the procedures could not be read, with Retry, rather than calling the library empty", () => {
    const t = open();
    act(() => t.emit({ type: "library", sops: [], cases: [], sopsError: "HTTP 500" }));
    expect(screen.getByRole("alert").textContent).toContain("The procedures could not be read: HTTP 500");
    expect(screen.queryByText("no procedures yet — import some")).toBeNull();
    const nav = screen.getByRole("navigation", { name: "Knowledge sections" });
    expect(within(nav).getByText("Procedures").textContent).toBe("Procedures"); // no count claimed
    fireEvent.click(screen.getByText("Retry"));
    expect(t.intents).toContainEqual({ type: "library.get" });
  });

  it("does not let a search dismissed by changing section come back", () => {
    const t = open();
    library(t);
    fireEvent.change(screen.getByLabelText("Search procedures and past cases"), { target: { value: "rdp" } });
    fireEvent.click(screen.getByText("Search"));
    fireEvent.click(screen.getByRole("button", { name: "Import" })); // left before the answer came
    act(() => t.emit({ type: "library.results", query: "rdp", sops: [], cases: [] }));
    expect(screen.queryByText("Results for “rdp”")).toBeNull();
    expect(screen.getByText("Import procedures…")).toBeTruthy();
  });
});
