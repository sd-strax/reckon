// The recorded snapshot set (harness/record.ts writes these from a running
// stack on the reference scenario; fixture data only). Add a stage here when
// recording it.
import type { HarnessSnapshot } from "../transport";
import synthetic from "./synthetic.json";

export const snapshots: HarnessSnapshot[] = [
  synthetic as unknown as HarnessSnapshot,
];
