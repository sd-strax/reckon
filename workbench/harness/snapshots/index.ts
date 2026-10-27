// The recorded snapshot set (harness/record.ts writes these from a running
// stack on the reference scenario; fixture data only). `synthetic` is a
// hand-written stage that exercises every view shape at once. Add a stage
// here when recording it. `knowledge` is the Knowledge document's stage
// (impl §2.6): the demo pack's procedures and prior cases as the host would
// post them (harness/record.ts --knowledge re-records it from a seeded stack).
import type { HarnessKnowledge, HarnessSnapshot } from "../transport";
import knowledge from "./knowledge.json";
import seeded from "./seeded.json";
import synthetic from "./synthetic.json";

export const snapshots: HarnessSnapshot[] = [
  seeded as unknown as HarnessSnapshot,
  synthetic as unknown as HarnessSnapshot,
];

export const knowledgeSnapshot = knowledge as unknown as HarnessKnowledge;
