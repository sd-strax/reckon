import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { CaseInboundMessage, CaseRenderMessage } from "../../../src/protocol";
import { FakeTransport } from "../test/fakeTransport";
import { CaseApp } from "./CaseApp";

const open = () => new FakeTransport<CaseInboundMessage, CaseRenderMessage>();

describe("the case document surface (adr/0030)", () => {
  it("asks for the case when it mounts, then renders it with Start", () => {
    const t = open();
    render(<CaseApp transport={t} />);
    expect(t.intents).toEqual([{ type: "ready" }]);
    expect(screen.getByText("Reading the case…")).toBeTruthy();

    act(() => t.emit({ type: "case", case: { number: "C-7", title: "RDP from host-a", priority: "P2", status: "open", summary: "Lateral movement suspected." } }));
    expect(screen.getByRole("heading").textContent).toBe("RDP from host-a");
    expect(screen.getByText("P2")).toBeTruthy();
    expect(screen.getByText("Lateral movement suspected.")).toBeTruthy();

    fireEvent.click(screen.getByText("Start investigation"));
    expect(t.intents).toContainEqual({ type: "start" });

    act(() => t.emit({ type: "starting" }));
    const btn = screen.getByText("Starting…") as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it("offers Open when the case is already under investigation", () => {
    const t = open();
    render(<CaseApp transport={t} />);
    act(() => t.emit({ type: "case", case: { number: "C-7", investigation: { id: "inv-1", title: "RDP from host-a", status: "ACTIVE" } } }));
    expect(screen.queryByText("Start investigation")).toBeNull();
    fireEvent.click(screen.getByText("Open investigation"));
    expect(t.intents).toContainEqual({ type: "open", id: "inv-1", title: "RDP from host-a" });
  });

  it("shows a read failure with Retry, and an error beside a loaded case", () => {
    const t = open();
    render(<CaseApp transport={t} />);
    act(() => t.emit({ type: "error", message: "GET /cases/C-7 → 503" }));
    expect(screen.getByText("GET /cases/C-7 → 503")).toBeTruthy();
    fireEvent.click(screen.getByText("Retry"));
    expect(t.intents).toContainEqual({ type: "refresh" });

    act(() => t.emit({ type: "case", case: { number: "C-7" } }));
    act(() => t.emit({ type: "starting" }));
    act(() => t.emit({ type: "error", message: "start refused" }));
    expect(screen.getByText("start refused")).toBeTruthy();
    expect((screen.getByText("Start investigation") as HTMLButtonElement).disabled).toBe(false);
  });
});
