// Builds harness/snapshots/knowledge.json from the demo knowledge pack
// (demo/knowledge: the staged SOP documents and the two prior-case summaries)
// without a running stack — the shape the Knowledge document's host posts
// after `reckon demo seed` + the staged import. `npm run harness:record --
// --knowledge` replaces it with a real recording when a stack is up.
//
//   node harness/build-knowledge-snapshot.mjs
import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..", "..");
const sopsDir = join(root, "demo", "knowledge", "sops");
const casesDir = join(root, "demo", "knowledge", "cases");

/** A deterministic UUID-shaped id for a document (the facade mints a random lineage id; the harness needs a stable one). */
function idFor(name) {
  const h = createHash("sha1").update(name).digest("hex");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-4${h.slice(13, 16)}-8${h.slice(17, 20)}-${h.slice(20, 32)}`;
}

/** The minimal frontmatter split the recorder does not need: the server parses for real. */
function parse(raw) {
  const text = raw.replace(/\r\n/g, "\n");
  if (!text.startsWith("---\n")) return { fm: {}, body: text.trim() };
  const end = text.indexOf("\n---\n", 4);
  const fm = {};
  let section = null;
  for (const line of text.slice(4, end).split("\n")) {
    const m = /^(\s*)([a-z_]+):\s*(.*)$/.exec(line);
    if (!m) continue;
    const [, indent, key, value] = m;
    if (indent) { if (section) fm[section][key] = value.replace(/^"|"$/g, ""); continue; }
    if (value === "") { section = key; fm[key] = {}; continue; }
    section = null;
    fm[key] = value.startsWith("[") ? value.slice(1, -1).split(",").map((t) => t.trim()).filter(Boolean) : value.replace(/^"|"$/g, "");
  }
  return { fm, body: text.slice(end + 5).trim() };
}

const importedAt = "2026-10-26T09:12:00Z";
const sops = readdirSync(sopsDir).filter((f) => f.endsWith(".md")).sort().map((f, i) => {
  const { fm, body } = parse(readFileSync(join(sopsDir, f), "utf8"));
  const id = idFor(f);
  return {
    id, title: fm.title || f, body, tags: fm.tags || [], status: "published", recommendation: fm.recommendation,
    authorId: fm.author, source: fm.source ? { system: fm.source.system, url: fm.source.url, version: fm.source.version } : undefined,
    importedBy: "analyst-1", importedAt, createdAt: importedAt, updatedAt: importedAt, revisionCount: i === 0 ? 2 : 1,
  };
});
// One retired procedure so the hidden-by-default rule and the reason line are visible.
sops.push({
  id: idFor("legacy-vpn-lockout.md"), title: "Legacy VPN lockout procedure", body: "# Legacy VPN lockout procedure\n\nRetired: the VPN concentrator was decommissioned.\n",
  tags: ["vpn", "access"], status: "retired", recommendation: "escalate", authorId: "SOC Runbooks",
  source: { system: "file", url: "file:/sops/legacy-vpn-lockout.md" }, importedBy: "analyst-1", importedAt: "2026-09-01T10:00:00Z",
  createdAt: "2026-09-01T10:00:00Z", updatedAt: "2026-10-20T15:30:00Z", revisionCount: 3,
  retiredAt: "2026-10-20T15:30:00Z", retiredReason: "the VPN concentrator was decommissioned", retiredBy: "analyst-1",
});

const caseSummaries = {};
const cases = readdirSync(casesDir).filter((f) => f.endsWith(".json")).sort().map((f) => JSON.parse(readFileSync(join(casesDir, f), "utf8")));
for (const c of cases) {
  caseSummaries[c.investigation_ref] = {
    investigationRef: c.investigation_ref, title: c.title, body: c.summary_text || "", conclusionAt: c.concluded_at,
    seedKind: c.seed_kind, seedValue: c.seed_value, techniques: c.techniques || [], primaryEntities: c.primary_entities || [],
    actionsTaken: (c.actions_taken || []).map((a) => ({ actionType: a.action_type, targetCount: a.target_count, outcome: a.outcome })),
    conclusionOutcome: c.conclusion_outcome, durationHours: c.duration_hours, verdictDisposition: c.verdict_disposition,
    verdictRationale: c.verdict_rationale, tags: (c.techniques || []).map((t) => "technique:" + t), revision: 1,
  };
}

const importRows = [
  {
    id: "file:///Users/analyst/sops/credential-reset-runbook.md", name: "credential-reset-runbook.md", dir: "/Users/analyst/sops", fromEditor: true, dirty: true, checked: true,
    preview: { title: "Credential reset runbook", author: "SOC Runbooks", tags: ["credential", "reset"], recommendation: "reset", source: { system: "wiki", url: "https://wiki.example.test/RB-CRED-RESET", version: "1.2" }, sourceDefaulted: false, missing: [], outcome: "added" },
  },
  {
    id: "file:///Users/analyst/sops/host-isolation-runbook.md", name: "host-isolation-runbook.md", dir: "/Users/analyst/sops", fromEditor: false, dirty: false, checked: true,
    preview: { title: "Host Isolation Runbook", author: "SOC Runbooks", tags: ["containment", "host-isolation", "evidence-preservation"], recommendation: "isolate", source: { system: "demo", url: "demo://sops/host-isolation-runbook", version: "1.0" }, sourceDefaulted: false, missing: [], outcome: "revised", existingId: sops.find((s) => s.source?.url === "demo://sops/host-isolation-runbook")?.id, existingTitle: "Host Isolation Runbook" },
  },
  {
    id: "file:///Users/analyst/sops/notes.md", name: "notes.md", dir: "/Users/analyst/sops", fromEditor: false, dirty: false, checked: false,
    preview: { title: "notes", tags: [], source: { system: "file", url: "file:/Users/analyst/sops/notes.md" }, sourceDefaulted: true, missing: ["title", "author", "tags", "source_url"], outcome: "added" },
  },
  {
    id: "file:///Users/analyst/sops/empty.md", name: "empty.md", dir: "/Users/analyst/sops", fromEditor: false, dirty: false, checked: true,
    previewError: "POST /api/sops/import_markdown?dry_run=true → 400: parse sop document: empty body",
  },
];

// Citations (adr/0109): the first two procedures were consulted by the stage's own investigation and
// one prior one; the second prior case was drawn on by the stage's investigation and taught one procedure.
const stageInv = { investigationId: "11111111-1111-4111-8111-111111111111", title: "Workstation logon chain", status: "active" };
const priorInv = { investigationId: "22222222-2222-4222-8222-222222222222", title: "Service account misuse", status: "concluded" };
const sopCitations = {
  [sops[0].id]: [
    { ...stageInv, applied: true, interpretations: 2, firstAt: "2026-10-20T09:04:00Z", lastAt: "2026-10-20T09:40:00Z" },
    { ...priorInv, applied: false, interpretations: 1, firstAt: "2026-09-12T14:00:00Z", lastAt: "2026-09-12T14:00:00Z" },
  ],
  [sops[1].id]: [{ ...stageInv, applied: false, interpretations: 1, firstAt: "2026-10-20T09:04:00Z", lastAt: "2026-10-20T09:04:00Z" }],
};
const citations = Object.fromEntries(Object.entries(sopCitations).map(([id, rows]) => [id, new Set(rows.map((r) => r.investigationId)).size]));
const caseRefs = Object.keys(caseSummaries);
const caseCitations = Object.fromEntries(caseRefs.map((ref, i) => [ref, i === 0
  ? { investigations: [{ ...stageInv, applied: true, interpretations: 1, firstAt: "2026-10-20T09:04:00Z", lastAt: "2026-10-20T09:04:00Z" }], learned: [{ sopId: sops[1].id, title: sops[1].title }] }
  : { investigations: [], learned: [] }]));

// Candidates (adr/0112): one waiting for review, drafted at the stage's own investigation; one discarded.
const candidates = [
  { id: "cand-1", investigationId: stageInv.investigationId, title: "Confirm an unexpected interactive session with the account owner", body: "# Confirm an unexpected interactive session\n\n1. Reach the account owner out of band.\n2. Record the answer as an attestation on the prediction.\n3. If unrecognised, isolate the host and disable the account.", tags: ["identity", "containment"], recommendation: "isolate", rationale: "The case turned on an owner attestation; no procedure covers reaching the owner first.", generatorModel: "harness-model", draftedAt: "2026-10-20T11:00:00Z", draftedBy: "analyst-1", draftedByName: "Analyst One", status: "NEW" },
  { id: "cand-2", investigationId: priorInv.investigationId, title: "Rotate a service account after misuse", body: "Rotate it.", tags: ["identity"], recommendation: "reset", rationale: "", generatorModel: "harness-model", draftedAt: "2026-09-12T16:00:00Z", draftedBy: "analyst-1", draftedByName: "Analyst One", status: "DISCARDED", decidedBy: "analyst-1", decidedByName: "Analyst One", decidedAt: "2026-09-13T09:00:00Z", discardReason: "the credential reset runbook already covers it" },
];
const candidateDetails = { "cand-1": { closest: { sopId: sops[0].id, title: sops[0].title, excerpt: sops[0].body.split("\n").slice(2, 4).join(" ").slice(0, 160), score: 0.61, rationale: "shares the containment steps" } } };

const search = {
  procedures: sops.filter((s) => s.status !== "retired").slice(0, 2).map((s, i) => ({ sopId: s.id, title: s.title, excerpt: s.body.split("\n").slice(2, 5).join(" ").slice(0, 220), score: 0.82 - i * 0.2, rationale: i === 0 ? "the seed context names RDP lateral movement" : "shares the containment vocabulary", tags: s.tags, recommendation: s.recommendation, status: s.status, backend: "vector-cosine/text-embedding-3-small" })),
  cases: cases.map((c, i) => ({ investigationRef: c.investigation_ref, title: c.title, excerpt: (c.summary_text || "").slice(0, 220), score: 0.9 - i * 0.35, band: i === 0 ? "NEAR_DUPLICATE" : "DISTINCT", rationale: i === 0 ? "same technique and seed kind" : "different technique family", tags: (c.techniques || []).map((t) => "technique:" + t), backend: "vector-cosine/text-embedding-3-small" })),
  coverage: { procedures: "COMPLETE", cases: "COMPLETE" },
};

const out = {
  name: "knowledge",
  knowledge: {
    counts: { procedures: sops.filter((s) => s.status !== "retired").length, cases: Object.keys(caseSummaries).length, candidates: candidates.filter((c) => c.status === "NEW").length },
    sops, cases: Object.values(caseSummaries).sort((a, b) => (b.conclusionAt || "").localeCompare(a.conclusionAt || "")), candidates, candidateDetail: null, citations, detailCitations: null, detail: null, detailRevisions: null, revisionBodies: {}, results: null, answer: null, importRows, importing: false,
  },
  // Each procedure's lineage (adr/0097): one revision per rev count, the first imported, later ones edits; the bodies for a compare.
  revisions: Object.fromEntries(sops.map((s) => [s.id, Array.from({ length: s.revisionCount }, (_, i) => {
    const rev = s.revisionCount - i;
    const hash = createHash("sha256").update(s.id + ":" + rev).digest("hex");
    return { revision: rev, contentHash: hash, title: s.title, status: rev === s.revisionCount ? s.status : "published", createdAt: new Date(Date.parse(s.importedAt) + (rev - 1) * 86400000).toISOString(), authorId: s.authorId, importedBy: rev === 1 ? s.importedBy : undefined, sourceVersion: rev === 1 ? s.source?.version : undefined, supersededAt: rev === s.revisionCount ? undefined : new Date(Date.parse(s.importedAt) + rev * 86400000).toISOString(), hasOriginal: rev === 1 };
  })])),
  revisionBodies: Object.fromEntries(sops.flatMap((s) => Array.from({ length: s.revisionCount }, (_, i) => {
    const rev = s.revisionCount - i;
    const hash = createHash("sha256").update(s.id + ":" + rev).digest("hex");
    return [hash, { contentHash: hash, title: s.title, body: rev === s.revisionCount ? s.body : s.body.split("\n").slice(0, -3).join("\n") + "\n\nAn earlier wording of the closing steps.\n", tags: s.tags, purged: false }];
  }))),
  search,
  caseSummaries,
  sopCitations,
  caseCitations,
  candidateDetails,
  queue: JSON.parse(readFileSync(join(here, "snapshots", "synthetic.json"), "utf8")).queue,
};
const file = join(here, "snapshots", "knowledge.json");
writeFileSync(file, JSON.stringify(out, null, 2) + "\n");
console.log(`wrote ${file} (${sops.length} procedures, ${cases.length} case summaries, ${importRows.length} import rows)`);
