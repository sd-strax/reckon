import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppProvider } from "../app";
import { RefChip } from "./shared";
import { HarnessTransport, type HarnessSnapshot } from "../../../harness/transport";
import synthetic from "../../../harness/snapshots/synthetic.json";
import { shortTitle } from "../derive/hyps";

describe("a chip names its object (#153)", () => {
  it("a prediction chip says which prediction, a hypothesis chip which hypothesis", async () => {
    const d = synthetic as unknown as HarnessSnapshot;
    const transport = new HarnessTransport(d);
    const h = d.snapshot.hypotheses[0];
    const p = h.predictions[0];
    const { container } = render(<AppProvider transport={transport}><RefChip refId={p.id} /> <RefChip refId={h.id} /></AppProvider>);
    await new Promise((r) => setTimeout(r, 0));
    const chips = container.querySelectorAll(".refchip");
    expect(chips[0].textContent).toContain(`P${p.ordinal} ${shortTitle(p)}`);
    expect(chips[1].textContent).toContain(h.handle || "");
  });
});
