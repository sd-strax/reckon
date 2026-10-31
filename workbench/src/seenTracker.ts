import type * as vscode from "vscode";

/**
 * Per-investigation last-seen event sequence, per person, in globalState. The
 * tree's unseen-changes cue compares this against the backend's
 * last_event_sequence: strictly view state (ui/02 §2.12), so it never touches
 * the record. It is the person's own: what one analyst has seen says nothing of
 * what another has (adr/0241).
 */
export class SeenTracker {
  private static readonly PREFIX = "reckon.seenSeq";

  constructor(private readonly memento: vscode.Memento, private readonly subject: () => string | undefined) {
    // The one key shared by everyone who ever signed in here.
    void memento.update(SeenTracker.PREFIX, undefined);
  }

  get(id: string): number {
    return this.marks()[id] ?? 0;
  }

  async set(id: string, seq: number): Promise<void> {
    const key = this.key();
    if (!key) {
      return;
    }
    const all = { ...this.marks() };
    if ((all[id] ?? 0) >= seq) {
      return;
    }
    all[id] = seq;
    await this.memento.update(key, all);
  }

  private marks(): Record<string, number> {
    const key = this.key();
    return key ? this.memento.get<Record<string, number>>(key, {}) : {};
  }

  private key(): string | undefined {
    const subject = this.subject();
    return subject ? `${SeenTracker.PREFIX}.${subject}` : undefined;
  }
}
