import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ImportInboundMessage, ImportRenderMessage, ImportRow } from "../../../src/protocol";
import { FakeTransport } from "../test/fakeTransport";
import { ImportApp } from "./ImportApp";

const open = () => new FakeTransport<ImportInboundMessage, ImportRenderMessage>();
const row = (over: Partial<ImportRow>): ImportRow => ({ id: "file:///a.md", name: "a.md", dir: "/", fromDisk: false, dirty: false, checked: false, ...over });

describe("the Import SOPs surface (design/06 §2.4)", () => {
  it("asks for the state when it mounts, lists the rows by origin, and posts the analyst's choices", () => {
    const t = open();
    render(<ImportApp transport={t} />);
    expect(t.intents).toEqual([{ type: "ready" }]);

    act(() => t.emit({ type: "state", busy: false, rows: [
      row({ id: "file:///a.md", name: "a.md", dirty: true }),
      row({ id: "file:///b.md", name: "b.md", fromDisk: true, checked: true }),
    ] }));
    expect(screen.getByText("Open in the editor · 1")).toBeTruthy();
    expect(screen.getByText("unsaved edits · imported as shown")).toBeTruthy();
    const importBtn = screen.getByText("Import 1 document") as HTMLButtonElement;
    expect(importBtn.disabled).toBe(false);

    fireEvent.click(screen.getByLabelText(/a\.md/));
    expect(t.intents).toContainEqual({ type: "toggle", id: "file:///a.md", checked: true });
    expect(screen.getByText("Import 2 documents")).toBeTruthy();

    fireEvent.click(screen.getByText("Rescan open editors"));
    fireEvent.click(screen.getByText("Add files…"));
    fireEvent.click(screen.getByText("Import 2 documents"));
    expect(t.intents.map((i) => i.type)).toEqual(["ready", "toggle", "refresh", "addFiles", "import"]);
  });

  it("disables everything while importing and reports the outcome per row and in the footer", () => {
    const t = open();
    render(<ImportApp transport={t} />);
    act(() => t.emit({ type: "state", busy: true, rows: [row({ checked: true })] }));
    expect((screen.getByText("Importing…") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByText("Add files…") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByLabelText(/a\.md/) as HTMLInputElement).disabled).toBe(true);

    act(() => t.emit({ type: "state", busy: false, done: { ok: 1, failed: 1 }, rows: [
      row({ result: { outcome: "revised", title: "Containment" } }),
      row({ id: "file:///c.md", name: "c.md", result: { outcome: "failed", title: "c.md", message: "no frontmatter title" } }),
    ] }));
    expect(screen.getByText("Revised existing SOP “Containment”")).toBeTruthy();
    expect(screen.getByText("no frontmatter title")).toBeTruthy();
    expect(screen.getByText("1 imported, 1 failed — see each row.")).toBeTruthy();
  });

  it("says what to do when nothing is open or selected", () => {
    const t = open();
    render(<ImportApp transport={t} />);
    act(() => t.emit({ type: "state", busy: false, rows: [] }));
    expect(screen.getByText(/No markdown or text documents are open/)).toBeTruthy();
    expect(screen.getByText("No files added.")).toBeTruthy();
    expect(screen.getByText("Select the documents to import.")).toBeTruthy();
    expect((screen.getByText("Import") as HTMLButtonElement).disabled).toBe(true);
  });
});
