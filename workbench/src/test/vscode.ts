// The editor stand-in for host tests (adr/0245): vitest resolves `vscode` to
// this module (vitest.config.ts), so a host module loads and runs without an
// editor. It covers the API the host uses at runtime, typed against
// @types/vscode so a shape the real API does not have fails `tsc`. Panels and
// webviews are recorders the test drives: post a message in, read what the
// host posted out, dispose on demand. Nothing here ships behaviour; the
// extension never imports it.
import type * as vscode from "vscode";
import { vi } from "vitest";

// --- events -------------------------------------------------------------------

export class Disposable implements vscode.Disposable {
  constructor(private readonly callOnDispose: () => unknown) {}
  static from(...items: { dispose(): unknown }[]): Disposable {
    return new Disposable(() => items.forEach((i) => i.dispose()));
  }
  dispose(): unknown {
    return this.callOnDispose();
  }
}

export class EventEmitter<T> implements vscode.EventEmitter<T> {
  private listeners = new Set<(e: T) => unknown>();
  readonly event: vscode.Event<T> = (listener, thisArgs, disposables) => {
    const bound = (e: T) => listener.call(thisArgs, e);
    this.listeners.add(bound);
    const d = new Disposable(() => this.listeners.delete(bound));
    disposables?.push(d);
    return d;
  };
  fire(data: T): void {
    for (const l of [...this.listeners]) l(data);
  }
  dispose(): void {
    this.listeners.clear();
  }
}

// --- values -------------------------------------------------------------------

export class Uri implements vscode.Uri {
  private constructor(
    readonly scheme: string,
    readonly authority: string,
    readonly path: string,
    readonly query: string,
    readonly fragment: string,
  ) {}
  static parse(value: string): Uri {
    const u = new URL(value);
    return new Uri(u.protocol.replace(/:$/, ""), u.host, u.pathname, u.search.replace(/^\?/, ""), u.hash.replace(/^#/, ""));
  }
  static file(path: string): Uri {
    return new Uri("file", "", path, "", "");
  }
  static joinPath(base: Uri, ...segments: string[]): Uri {
    return base.with({ path: [base.path.replace(/\/+$/, ""), ...segments].join("/") });
  }
  get fsPath(): string {
    return this.path;
  }
  with(change: { scheme?: string; authority?: string; path?: string; query?: string; fragment?: string }): Uri {
    return new Uri(
      change.scheme ?? this.scheme, change.authority ?? this.authority, change.path ?? this.path,
      change.query ?? this.query, change.fragment ?? this.fragment,
    );
  }
  toString(): string {
    return `${this.scheme}://${this.authority}${this.path}${this.query ? `?${this.query}` : ""}${this.fragment ? `#${this.fragment}` : ""}`;
  }
  toJSON(): unknown {
    return this.toString();
  }
}

export const ViewColumn = { Active: -1, Beside: -2, One: 1, Two: 2, Three: 3 } satisfies Partial<Record<keyof typeof vscode.ViewColumn, number>>;
export const TreeItemCollapsibleState = { None: 0, Collapsed: 1, Expanded: 2 } satisfies Record<keyof typeof vscode.TreeItemCollapsibleState, number>;
export const ConfigurationTarget = { Global: 1, Workspace: 2, WorkspaceFolder: 3 } satisfies Record<keyof typeof vscode.ConfigurationTarget, number>;

export class ThemeIcon {
  constructor(readonly id: string, readonly color?: ThemeColor) {}
}

export class ThemeColor {
  constructor(readonly id: string) {}
}

export class TreeItem {
  description?: string;
  tooltip?: string;
  contextValue?: string;
  iconPath?: ThemeIcon;
  command?: vscode.Command;
  id?: string;
  constructor(readonly label: string, readonly collapsibleState: number = TreeItemCollapsibleState.None) {}
}

export class TabInputText {
  constructor(readonly uri: Uri) {}
}

export class TabInputWebview {
  constructor(readonly viewType: string) {}
}

// --- webviews -----------------------------------------------------------------

/** A webview the test drives: `receive` plays the page, `posted` is what the host sent. */
export class FakeWebview implements vscode.Webview {
  options: vscode.WebviewOptions = {};
  html = "";
  readonly cspSource = "vscode-resource:";
  readonly posted: unknown[] = [];
  private readonly inbound = new EventEmitter<unknown>();
  readonly onDidReceiveMessage = this.inbound.event;
  disposed = false;

  constructor(private readonly owner: FakeWebviewPanel) {}

  postMessage(message: unknown): Thenable<boolean> {
    if (this.owner.disposed) {
      // The real webview throws once its panel is gone; the host must cope.
      throw new Error("Webview is disposed");
    }
    this.posted.push(message);
    return Promise.resolve(true);
  }
  asWebviewUri(localResource: vscode.Uri): vscode.Uri {
    return localResource.with({ scheme: "vscode-webview" });
  }
  /** Deliver a message from the page to the host. */
  receive(message: unknown): void {
    this.inbound.fire(message);
  }
}

export class FakeWebviewPanel implements vscode.WebviewPanel {
  readonly webview: FakeWebview;
  readonly options: vscode.WebviewPanelOptions;
  iconPath?: vscode.Uri;
  readonly viewColumn: vscode.ViewColumn | undefined;
  active = true;
  visible = true;
  disposed = false;
  revealed = 0;
  private readonly disposeEmitter = new EventEmitter<void>();
  readonly onDidDispose = this.disposeEmitter.event;
  private readonly viewState = new EventEmitter<vscode.WebviewPanelOnDidChangeViewStateEvent>();
  readonly onDidChangeViewState = this.viewState.event;

  constructor(readonly viewType: string, public title: string, column: number, options: vscode.WebviewPanelOptions & vscode.WebviewOptions) {
    this.viewColumn = column as vscode.ViewColumn;
    this.options = options;
    this.webview = new FakeWebview(this);
    this.webview.options = options;
  }
  reveal(): void {
    this.revealed++;
  }
  /** Close the panel as the user would: fires onDidDispose once. */
  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.disposeEmitter.fire();
  }
}

// --- storage, logs ------------------------------------------------------------

export class FakeSecretStorage implements vscode.SecretStorage {
  readonly data = new Map<string, string>();
  private readonly changed = new EventEmitter<vscode.SecretStorageChangeEvent>();
  readonly onDidChange = this.changed.event;
  async get(key: string): Promise<string | undefined> {
    return this.data.get(key);
  }
  async keys(): Promise<string[]> {
    return [...this.data.keys()];
  }
  async store(key: string, value: string): Promise<void> {
    this.data.set(key, value);
    this.changed.fire({ key });
  }
  async delete(key: string): Promise<void> {
    this.data.delete(key);
    this.changed.fire({ key });
  }
}

/** A log channel that keeps its lines, for asserting what the host reported. */
export function fakeLog(): vscode.LogOutputChannel & { lines: string[] } {
  const lines: string[] = [];
  const rec = (level: string) => (msg: string | Error) => void lines.push(`${level} ${msg instanceof Error ? msg.message : msg}`);
  const levelChanged = new EventEmitter<vscode.LogLevel>();
  return {
    lines,
    name: "reckon",
    logLevel: 2 as vscode.LogLevel,
    onDidChangeLogLevel: levelChanged.event,
    trace: rec("trace"), debug: rec("debug"), info: rec("info"), warn: rec("warn"), error: rec("error"),
    append: rec("append"), appendLine: rec("append"), replace: rec("replace"),
    clear: () => void (lines.length = 0), show: () => undefined, hide: () => undefined, dispose: () => undefined,
  };
}

// --- namespaces ---------------------------------------------------------------

const panels: FakeWebviewPanel[] = [];
const settings = new Map<string, unknown>();
const registered = new Map<string, (...args: unknown[]) => unknown>();

// Each namespace literal `satisfies` its real counterpart's member names: a
// renamed or invented member fails `tsc`. The classes above check their full
// shapes through `implements`.
type Members<T> = Partial<Record<keyof T, unknown>>;

export const window = {
  createWebviewPanel: vi.fn((viewType: string, title: string, column: number | { viewColumn: number }, options?: vscode.WebviewPanelOptions & vscode.WebviewOptions) => {
    const p = new FakeWebviewPanel(viewType, title, typeof column === "number" ? column : column.viewColumn, options ?? {});
    panels.push(p);
    return p;
  }),
  showInformationMessage: vi.fn(async (..._args: unknown[]): Promise<string | undefined> => undefined),
  showWarningMessage: vi.fn(async (..._args: unknown[]): Promise<string | undefined> => undefined),
  showErrorMessage: vi.fn(async (..._args: unknown[]): Promise<string | undefined> => undefined),
  showInputBox: vi.fn(async (..._args: unknown[]): Promise<string | undefined> => undefined),
  showOpenDialog: vi.fn(async (..._args: unknown[]): Promise<Uri[] | undefined> => undefined),
  showSaveDialog: vi.fn(async (..._args: unknown[]): Promise<Uri | undefined> => undefined),
  showTextDocument: vi.fn(async (..._args: unknown[]) => undefined),
  createOutputChannel: vi.fn((..._args: unknown[]) => fakeLog()),
  registerTreeDataProvider: vi.fn((..._args: unknown[]) => new Disposable(() => undefined)),
  activeTextEditor: undefined as vscode.TextEditor | undefined,
  tabGroups: { all: [] as vscode.TabGroup[], close: vi.fn(async (..._args: unknown[]) => true) },
} satisfies Members<typeof vscode.window>;

export const workspace = {
  getConfiguration: vi.fn((section?: string) => ({
    get: <T>(key: string, defaultValue?: T): T | undefined => {
      const full = section ? `${section}.${key}` : key;
      return (settings.has(full) ? settings.get(full) : defaultValue) as T | undefined;
    },
    has: (key: string) => settings.has(section ? `${section}.${key}` : key),
    update: vi.fn(async (key: string, value: unknown) => {
      settings.set(section ? `${section}.${key}` : key, value);
    }),
  })),
  fs: {
    readFile: vi.fn(async (..._args: unknown[]): Promise<Uint8Array> => new Uint8Array()),
    writeFile: vi.fn(async (..._args: unknown[]) => undefined),
    createDirectory: vi.fn(async (..._args: unknown[]) => undefined),
    delete: vi.fn(async (..._args: unknown[]) => undefined),
  },
  openTextDocument: vi.fn(async (..._args: unknown[]) => ({}) as vscode.TextDocument),
  textDocuments: [] as vscode.TextDocument[],
} satisfies Members<typeof vscode.workspace>;

export const commands = {
  registerCommand: vi.fn((id: string, handler: (...args: unknown[]) => unknown) => {
    registered.set(id, handler);
    return new Disposable(() => registered.delete(id));
  }),
  executeCommand: vi.fn(async (id: string, ...args: unknown[]) => registered.get(id)?.(...args)),
} satisfies Members<typeof vscode.commands>;

export const env = {
  openExternal: vi.fn(async (_target: vscode.Uri) => true),
  clipboard: { writeText: vi.fn(async (_value: string) => undefined), readText: vi.fn(async () => "") },
} satisfies Members<typeof vscode.env>;


// --- test controls ------------------------------------------------------------

/** Every webview panel the host created since the last reset, oldest first. */
export function createdPanels(): readonly FakeWebviewPanel[] {
  return panels;
}

/** Set a configuration value as the user would (e.g. `reckon.sidecarPath`). */
export function setConfiguration(key: string, value: unknown): void {
  settings.set(key, value);
}

/** Fresh state for the next test: no panels, no settings, no commands, cleared spies. */
export function resetVscodeFake(): void {
  panels.length = 0;
  settings.clear();
  registered.clear();
  window.activeTextEditor = undefined;
  for (const ns of [window, workspace, workspace.fs, commands, env, env.clipboard, window.tabGroups]) {
    for (const v of Object.values(ns)) {
      if (vi.isMockFunction(v)) v.mockClear();
    }
  }
}
