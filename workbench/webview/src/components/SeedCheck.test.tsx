import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { App } from "./App";
import { HarnessTransport } from "../../../harness/transport";

describe("an entity nothing knows about (#159)", () => {
  it("is reported on the draft, and the analyst can start anyway", () => {
    const transport = new HarnessTransport(null);
    render(<AppProvider transport={transport}><App /></AppProvider>);
    fireEvent.change(document.querySelector("#draftInput")!, { target: { value: "WIN-FILE99" } });
    act(() => transport.emit({ type: "draft.error", message: "no host named \"WIN-FILE99\" in the host inventory reckon can see — check the name, or start anyway", unknownEntity: true }));
    expect(screen.getByText(/no host named "WIN-FILE99"/)).toBeTruthy();
    fireEvent.click(screen.getByText("Start anyway"));
    expect(transport.intents.filter((i) => i.type === "seed.submit").pop()).toMatchObject({ value: "WIN-FILE99", allowUnknown: true });
  });
});
