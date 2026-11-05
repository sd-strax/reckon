// Dependencies (adr/0101) as the derivations read them: how many still count
// (OPEN or SENT), and the to-do rows they add. Pure.
import type { Dependency } from "../../../src/viewTypes";

export const dependencyLive = (d: Pick<Dependency, "status">): boolean => d.status === "OPEN" || d.status === "SENT";

/**
 * OPEN + SENT: what the conclude preflight and the Notify step count. A slice
 * that could not be read is null, not zero: the preflight and the stage then
 * say the count is unchecked rather than clean. An empty list that did read
 * is zero.
 */
export function openDependencies(deps: Dependency[] | null | undefined): number | null {
  return deps ? deps.filter(dependencyLive).length : null;
}
