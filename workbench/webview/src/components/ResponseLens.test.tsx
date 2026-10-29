import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";

const lens = () => document.querySelector(".lensscroll") as HTMLElement;

describe("the Response lens (#125)", () => {
  it("puts decisions first, lets one be set aside and brought back, and remembers it", () => {
    const transport = new HarnessTransport(synthetic as unknown as HarnessSnapshot);
    render(<AppProvider transport={transport}><App /></AppProvider>);
    fireEvent.click(screen.getByRole("tab", { name: /^Response/ }));
    const sections = Array.from(lens().querySelectorAll("section > h3")).map((h) => h.childNodes[0].textContent);
    expect(sections[0]).toBe("Waiting on you");
    expect(within(lens()).getByRole("heading", { level: 3, name: "Response" })).toBeTruthy();
    expect(within(lens()).queryByText("Containment plan")).toBeNull();
    const card = document.getElementById("obj-act-2")!;
    fireEvent.click(within(card).getByText("Decide later"));
    expect(within(document.getElementById("obj-aside")!).getByText(/stays open until its approval window closes/)).toBeTruthy();
    expect(document.getElementById("obj-aside")!.contains(document.getElementById("obj-act-2"))).toBe(true);
    expect(transport.getState<{ aside?: string[] }>()?.aside).toEqual(["act-2"]);
    fireEvent.click(within(document.getElementById("obj-act-2")!).getByText("Bring back"));
    expect(document.getElementById("obj-aside")).toBeNull();
  });
});
