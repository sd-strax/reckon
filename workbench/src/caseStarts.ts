// The one start from a case (adr/0342). The sidebar's Cases row, the case
// page's Start investigation and the draft's From a case each create an
// investigation rooted on a case; all three run here, so one in-flight guard
// per case number covers them together — a second start of the same case
// while one is in flight sends no second POST, from whichever surface — and a
// start that succeeded tells whoever shows that case to re-read it.
export class CaseStarts {
  private readonly starting = new Set<string>();
  private readonly listeners: ((caseNumber: string) => void)[] = [];

  /** Whether a start of this case is in flight. */
  inFlight(caseNumber: string): boolean {
    return this.starting.has(caseNumber);
  }

  /**
   * Run `create` for the case unless a start of it is in flight (then
   * undefined, and nothing is sent). A create that resolves notifies the
   * listeners with the case number; a failure is the caller's to report.
   */
  async run<T>(caseNumber: string, create: () => Promise<T>): Promise<T | undefined> {
    if (this.starting.has(caseNumber)) {
      return undefined;
    }
    this.starting.add(caseNumber);
    try {
      const created = await create();
      for (const l of this.listeners) l(caseNumber);
      return created;
    } finally {
      this.starting.delete(caseNumber);
    }
  }

  /** Called with the case number after each start that succeeded. */
  onDidStart(listener: (caseNumber: string) => void): void {
    this.listeners.push(listener);
  }
}
