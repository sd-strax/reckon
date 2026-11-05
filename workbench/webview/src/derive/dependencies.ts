// Dependencies (adr/0101) as the derivations read them: how many still count
// (OPEN or SENT), and the to-do rows they add. Pure.
import type { Dependency } from "../../../src/viewTypes";

export const dependencyLive = (d: Pick<Dependency, "status">): boolean => d.status === "OPEN" || d.status === "SENT";

/** OPEN + SENT: what the conclude preflight and the Notify step count. */
export function openDependencies(deps: Dependency[]): number {
  return deps.filter(dependencyLive).length;
}
