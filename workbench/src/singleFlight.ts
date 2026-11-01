// One in-flight run per key: concurrent callers for the same key share the
// first caller's promise until it settles (#157). No vscode, so it is
// unit-testable apart from the transport that uses it.
export class SingleFlight<T> {
  private readonly pending = new Map<string, Promise<T>>();

  run(key: string, start: () => Promise<T>): Promise<T> {
    const inFlight = this.pending.get(key);
    if (inFlight) return inFlight;
    const p: Promise<T> = start().finally(() => {
      if (this.pending.get(key) === p) this.pending.delete(key);
    });
    this.pending.set(key, p);
    return p;
  }

  /** Resolves once the in-flight run for the key, if any, has settled. */
  settled(key: string): Promise<void> {
    return Promise.resolve(this.pending.get(key)).then(() => undefined, () => undefined);
  }

  clear(): void {
    this.pending.clear();
  }
}
