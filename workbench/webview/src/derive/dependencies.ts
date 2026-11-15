// Dependencies (adr/0101) as the derivations read them: how many still count
// (OPEN or SENT), and the to-do rows they add. Pure.
import type { ActionRow, Dependency } from "../../../src/viewTypes";

export const dependencyLive = (d: Pick<Dependency, "status">): boolean => d.status === "OPEN" || d.status === "SENT";

/**
 * A ride that will not send: the notification's outcome is terminal without a
 * full success, or it expired (the stored status, or a pending one past its
 * deadline). A send in flight — approved, executing, or with an outcome a
 * person has yet to establish — keeps the link: its success marks the
 * dependency asked, and moving it off would lose that.
 *
 * It reads the lifecycle status, as the server does: a SUCCEEDED notify marks
 * its dependencies SENT, a partial send included, so a partial ride did not
 * stop. A timed-out send is FAILED and stopped.
 */
export const rideStopped = (a: Pick<ActionRow, "status" | "expired">): boolean =>
  a.expired || ["FAILED", "REJECTED", "EXPIRED"].includes(a.status);

/**
 * OPEN + SENT: what the conclude preflight and the Notify step count. A slice
 * that could not be read is null, not zero: the preflight and the stage then
 * say the count is unchecked rather than clean. An empty list that did read
 * is zero.
 */
export function openDependencies(deps: Dependency[] | null | undefined): number | null {
  return deps ? deps.filter(dependencyLive).length : null;
}
