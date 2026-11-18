import { describe, expect, it } from "vitest";
import { citableRefs, isCitable } from "./citable";

describe("citable refs (adr/0364)", () => {
  it("keeps STIX ids and leaves raw event ids out", () => {
    const od = "observed-data--7c1d5e4f-ae6f-4182-8d4e-5f6a7b8c9d04";
    const host = "reckon-host--4f8a2d1e-7b3c-4e5f-9a1b-2c3d4e5f6a01";
    const event = "0a1b2c3d-4e5f-4061-8172-93a4b5c6d7e8";
    expect(citableRefs([event, od, host])).toEqual([od, host]);
    expect(isCitable(event)).toBe(false);
    expect(isCitable("observed-data--1")).toBe(false);
  });
});
