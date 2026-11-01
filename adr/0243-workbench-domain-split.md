# 0243 — The workbench host is split by domain: one client per domain, one module per group of the document's acts

Issue: #243 · PR: pending · Status: accepted

## Context

Two files had absorbed most of the host. `investigationDocument.ts` was a
controller of some sixty methods and fourteen per-investigation state fields:
panel lifecycle, the seed surface, action decisions and the dispatch watch,
the hypothesis, prediction, verdict and pin writes, evidence reads and the
inspector's answers, the knowledge library and the procedure drafted from a
case, the markdown export, enablement, lifecycle, rename. `backend.ts` was
one client of some sixty methods with every DTO and mapper and the request
plumbing. Two seams had already been cut and showed the shape: `TurnDriver`
is a class given the transport and two callbacks of the controller, and
`documentRouter.ts` is a typed table from message to handler, so the message
names already said which domain each act belongs to. The cache already
declared the narrow slice of the client it reads (`CacheClient`).

## Decision

- **Clients.** `client/http.ts` holds what every call shares: the base URL,
  the token, the one retry on a refused token (`adr/0240`) and how a refusal is
  reported. Each domain is its own client over it, with its raw DTOs and
  mappers: `investigations` (the list and the record, the thread and its
  writes, lifecycle, creation and rename, the exports), `actions` (the queue,
  the catalog, requests and decisions, the record, and comms, since an outbound
  message is an action through Gate 2), `evidence`, `knowledge`, `cases`,
  `capabilities`. `BackendClient` is the composition: the connection-level
  calls (`status`, `authConfig`, `me`, `incompatibilityReason`) and the six
  domains as properties. A call site names the domain (`client.actions.approve`).
  A consumer declares the slice it needs, as the cache does, and the
  composition satisfies it structurally.
- **The document.** `DocumentHost` (`documentHost.ts`) is what a domain may
  reach of the controller: the client, the log, the transport, the cache, and
  `post`, `refresh`, `load`, `mutate`, `sameIdentity`, `isOpen`,
  `refreshTrees`, `releaseSession`, `show`, `rekey`, `offerCaseNote`. The
  controller implements it and hands itself to six domain classes, each owning
  the state that belongs to it: `DraftFlow` (the seed surface, start from a
  case, the write-back offer; owns the in-flight seeds), `ActionDecisions`
  (approve, reject, edit, re-request, resolve, reverse, a request from a pin,
  comms; owns the dispatch watch), `ReasoningWrites` (pins, hypotheses,
  predictions, the verdict), `EvidenceReads` (documents and labels, the derived
  lenses, the inspector's answers; owns the evidence caches), `KnowledgeFlow`
  (the library, the drafted procedure; owns the drafts and their editors),
  `ReportExport` (the markdown preview and save; owns the scratch files). The
  controller keeps the panels, routing, load and refresh, identity, rename,
  lifecycle and enablement, and asks each domain to `forget` a closed panel
  and to `clear` on a change of person.
- Every message, toast and command keeps its wording; the controller's tests
  drive the same panel and pass unchanged apart from the client fakes.

## Alternatives rejected

- **Split the client's implementation across files but keep the flat
  sixty-method `BackendClient`.** Less churn at the call sites, but the surface
  the issue objects to stays, and a consumer still cannot name the slice it
  needs.
- **Domain modules as free functions taking the controller.** They would reach
  anything on it; a class holding a `DocumentHost` reaches only the interface,
  and the interface is the list of what the controller must keep providing.
- **Splitting the controller's state maps into a per-investigation record.**
  A real simplification, but a behaviour change in disguise (what is kept past
  a panel's close, what a change of person clears) and not what the issue asks.
  The domains now own their state, so that change is local when it comes.

## Consequences

- `backend.ts` is about 150 lines and `investigationDocument.ts` about 700; a
  domain module is one to three hundred lines and reads as one concern.
- A new act lands in its domain module and one line of the handler table; a
  new endpoint lands in its domain client.
- Faking the client in a test means faking a domain, not sixty methods.
- A domain's test constructs it with a fake `DocumentHost`
  (`actionDecisions.test.ts` pins the decide rule and the watch's bound).
