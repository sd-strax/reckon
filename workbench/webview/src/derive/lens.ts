// The default lens follows the stage (08 §8.4, impl §7.2); the analyst's last
// choice per investigation (view state) wins when present. Pure.
import type { Lens } from "../../../src/protocol";

export function defaultLens(status: string, hasVerdict: boolean): Lens {
  const s = String(status).toUpperCase();
  if (s === "CONCLUDED" || s === "ARCHIVED") return "summary";
  if (hasVerdict) return "response";
  return "reasoning";
}

export const LENSES: Lens[] = ["summary", "story", "reasoning", "response"];
export const LENS_LABELS: Record<Lens, string> = { summary: "Summary", story: "Story", reasoning: "Reasoning", response: "Response" };
