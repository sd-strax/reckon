// Record harness snapshots from a running stack (impl §5.4). Logs in through
// the realm's password grant (the dev-auth principal), builds each open
// investigation's snapshot exactly as the document host does, resolves every
// cited ref's label, and writes harness/snapshots/<name>.json. Fixture data only.
//
//   npm run harness:record -- [--backend http://localhost:8080] [--name stage] [investigation-id]
//
// Env: RECKON_USER (default reckon-admin), RECKON_PASSWORD (default reckon).
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { BackendClient, type ActionRecord, type Appearance, type CaseSummaryView, type EvidenceDoc, type SOPDoc, type ThreadEntry } from "../src/backend";
import { buildDerivedViews } from "../src/derivedViews";
import { deriveEvidenceLabel } from "../src/evidenceLabel";
import type { ChronicleTurn, InvestigationSnapshot } from "../src/protocol";
import { ENTITY_POPOVER_TYPES } from "../webview/src/refs";

function arg(name: string, fallback: string): string {
  const i = process.argv.indexOf(name);
  return i >= 0 && process.argv[i + 1] ? process.argv[i + 1] : fallback;
}

async function login(backend: string): Promise<string> {
  const cfg = await (await fetch(`${backend}/api/auth-config`)).json() as { issuer: string; client_id: string };
  const body = new URLSearchParams({
    grant_type: "password",
    client_id: cfg.client_id,
    username: process.env.RECKON_USER || "reckon-admin",
    password: process.env.RECKON_PASSWORD || "reckon",
  });
  const res = await fetch(`${cfg.issuer}/protocol/openid-connect/token`, {
    method: "POST",
    headers: { "content-type": "application/x-www-form-urlencoded" },
    body,
  });
  if (!res.ok) throw new Error(`login failed (${res.status}): run \`reckon dev-auth\` first`);
  return ((await res.json()) as { access_token: string }).access_token;
}

async function main(): Promise<void> {
  const backend = arg("--backend", process.env.RECKON_BACKEND || "http://localhost:8080");
  const token = await login(backend);
  const client = new BackendClient(() => backend, async () => token);
  const positional = process.argv.slice(2).filter((a, i, all) => !a.startsWith("--") && !(i > 0 && all[i - 1].startsWith("--")));
  const ids = positional.length ? positional : (await client.investigations()).map((i) => i.id);
  // Compiled to harness/dist/record/harness/record.js; the snapshots live beside the source.
  const outDir = join(__dirname, "..", "..", "..", "snapshots");
  mkdirSync(outDir, { recursive: true });

  const docCache = new Map<string, EvidenceDoc | null>();
  const getDoc = async (ref: string): Promise<EvidenceDoc | null> => {
    if (docCache.has(ref)) return docCache.get(ref)!;
    let doc: EvidenceDoc | null;
    try { doc = await client.evidence(ref); } catch { doc = null; }
    docCache.set(ref, doc);
    return doc;
  };

  for (const id of ids) {
    const [investigation, hypotheses, capabilities, thread, actions, pins, comms, enablement] = await Promise.all([
      client.getInvestigation(id),
      client.hypotheses(id),
      client.capabilities().catch(() => null),
      client.thread(id).catch(() => null),
      client.actions(id).catch(() => null),
      client.pins(id).catch(() => []),
      client.comms(id).catch(() => null),
      client.enablement().catch(() => null),
    ]);
    const turns: ChronicleTurn[] = [];
    for (const e of (thread ?? []).filter((x: ThreadEntry) => x.hasTranscript).slice(-40)) {
      try {
        turns.push({ sequenceNo: e.sequenceNo, occurredAt: e.occurredAt, body: (await client.transcript(e.interpretationId)).body });
      } catch { /* a stub in the chronicle */ }
    }
    const derived = await buildDerivedViews(thread ?? [], getDoc);
    const snapshot: InvestigationSnapshot = {
      seq: investigation.lastEventSequence,
      investigation, hypotheses, actions, pins, comms, thread, turns, capabilities, enablement,
      knowledge: [], // the sidecar's retrieval is not recorded here; the harness shows the rail from a hand-written set
      derived,
    };
    // Every ref the snapshot cites gets its label and, for entities, its appearances.
    const refs = new Set<string>();
    const walk = (v: unknown) => {
      if (typeof v === "string") { if (/^[a-z0-9-]+--[0-9a-f-]{36}$/.test(v) || /^[0-9a-f-]{36}$/.test(v)) refs.add(v); }
      else if (Array.isArray(v)) v.forEach(walk);
      else if (v && typeof v === "object") Object.values(v).forEach(walk);
    };
    walk({ hypotheses, actions, pins, thread, derived });
    const labels: Record<string, string> = {};
    const appearances: Record<string, Appearance[]> = {};
    const docs: Record<string, EvidenceDoc> = {};
    for (const r of refs) {
      const doc = await getDoc(r);
      if (doc) docs[r] = doc;
      const label = doc ? deriveEvidenceLabel(doc) : "";
      if (label) labels[r] = label;
      if (ENTITY_POPOVER_TYPES.has(r.includes("--") ? r.slice(0, r.indexOf("--")) : "")) {
        try { appearances[r] = (await client.appearances(r)).filter((a) => a.investigationId !== id); } catch { /* none */ }
      }
    }
    // The inspector's records (impl §4.1): transcripts for every turn with
    // one, the record of every action, and the procedures / case summaries the
    // thread consulted.
    const transcripts: Record<string, string> = {};
    for (const e of (thread ?? []).filter((x: ThreadEntry) => x.hasTranscript)) {
      try { transcripts[e.interpretationId] = (await client.transcript(e.interpretationId)).body; } catch { /* a stub */ }
    }
    const actionRecords: Record<string, ActionRecord> = {};
    for (const a of actions ?? []) {
      try { actionRecords[a.actionId] = await client.actionRecord(a.actionId); } catch { /* not recorded */ }
    }
    const sops: Record<string, SOPDoc> = {};
    const caseSummaries: Record<string, CaseSummaryView | null> = {};
    for (const e of thread ?? []) {
      for (const s of e.consultedSops || []) if (!(s.sopId in sops)) { try { sops[s.sopId] = await client.sop(s.sopId); } catch { /* gone */ } }
      for (const s of e.consultedSimilar || []) if (!(s.investigationRef in caseSummaries)) { try { caseSummaries[s.investigationRef] = await client.caseSummary(s.investigationRef); } catch { /* none */ } }
    }
    const name = arg("--name", investigation.state.toLowerCase() + "-" + id.slice(0, 8));
    const file = join(outDir, name + ".json");
    writeFileSync(file, JSON.stringify({ name, snapshot, labels, appearances, docs, transcripts, actionRecords, sops, caseSummaries }, null, 2) + "\n");
    console.log(`wrote ${file} (seq ${snapshot.seq}, ${thread?.length ?? 0} thread entries, ${Object.keys(labels).length} labels)`);
  }
}

main().catch((err) => { console.error(err); process.exit(1); });
