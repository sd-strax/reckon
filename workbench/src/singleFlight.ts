// One in-flight run per key: concurrent callers for the same key share the
// first caller's promise until it settles (#157). No vscode, so it is
// unit-testable apart from the transport that uses it.
export class SingleFlight<T> {
  private readonly pending = new Map<string, Promise<T>>();

  run(key: string, start: () => Promise<T>): Promise<T> {
    const inFlight = this.pending.get(key);
    if (inFlight) return inFlight;
    const p = start().finally(() => this.pending.delete(key));
    this.pending.set(key, p);
    return p;
  }

  clear(): void {
    this.pending.clear();
  }
}
