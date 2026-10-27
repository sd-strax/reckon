// The recorded snapshot set (harness/record.ts writes these from a running
// stack on the reference scenario; fixture data only). `synthetic` is a
// hand-written stage that exercises every view shape at once. Add a stage
// here when recording it.
import type { HarnessSnapshot } from "../transport";
import seeded from "./seeded.json";
import synthetic from "./synthetic.json";

export const snapshots: HarnessSnapshot[] = [
  seeded as unknown as HarnessSnapshot,
  synthetic as unknown as HarnessSnapshot,
];
