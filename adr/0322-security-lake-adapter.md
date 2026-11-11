# 0322 — The first cloud audit-log adapter reads AWS Security Lake through an Athena query-access subscriber, answers resource context from AWS Config, and is held to the fixture's identities by an offline equivalence test

Issue: #322 · PR: #330 · Status: accepted

The adapter is named `aws-security-lake` since adr/0331; the text below keeps the name it was decided under.

## Context

adr/0320 gave cloud control-plane events typed identities and adr/0321 gave
the agent three verbs over them, answered so far only by the bundled
fixture. No adapter read a real cloud audit log. The first one should read a
source that already emits OCSF, so the work is authentication, querying,
paging and windowing rather than schema translation. AWS Security Lake
normalizes CloudTrail management events to OCSF API Activity (6003) at
schema 1.1.0, the floor `03 §4.15` already reads, and exposes them as Glue
tables partitioned by `time_dt`, `region` and `accountid`. A subscriber
reads them either as a push stream of objects (data access) or by querying
a shared table with Athena in its own account (query access). The lake
carries no configuration snapshots, so "what is this resource now" needs a
second source.

An out-of-process adapter (`design/11`) receives its secrets resolved in
`configure` and nothing else: its environment carries no credentials. It
answers each call with OCSF events or a classified error (`03 §6.2`); the
engine normalizes, scopes and computes coverage. Identity is the engine's
(`03 §7`), so a live event and a fixture event of the same thing must mint
the same ids or the scenario rehearses an investigation the live source
cannot reproduce.

## Decision

**Query access, not data access.** A verb is a question asked at
investigation time over a window; query access answers it with one Athena
query and honours the lake's own retention. Data access would need a
continuous ingestion pipeline and a local store, and the persistence design
writes telemetry only as the result of calls.

**One NATIVE_API adapter, `reckon-adapter-security-lake`, bundled.** A static
Go binary on the stdio skeleton the other native adapter uses, serial, with
the AWS SDK for Go v2 for request signing, retries, throttling and
assume-role refresh. It is bundled like the other first-party adapters, so
`reckon adapter setup security-lake` installs and configures it.
Credentials are built from the resolved `configure` values only — an access
key, optionally a session token, optionally a role to assume — and the
default credential chain, shared configuration files and the instance
metadata service are never consulted.

**Every query is bounded and parameterized.** Each query carries the
window as a `time_dt` range and, on an instance configured with accounts,
an `accountid` filter; a call with no window is refused as FATAL rather than
scanning the lake, which is the refusal adr/0321 leaves to the source.
Values travel only as Athena execution parameters, each rendered as a SQL
literal; the database and table names come from configuration, are
validated against a closed character set and quoted. The window bounds are
parsed as ISO 8601 timestamps in the query, since `time_dt` is a timestamp
column. `limit` caps the rows. The activity verb filters by principal
(matched against the user's identifier, the session issuer and the user's
name, so an ARN or a bare name both answer), credential, account, operation,
service, outcome (on the numeric `status_id`) and resource. A resource
given by its unique identifier matches on that identifier alone and is not
narrowed by account, region, zone, group or namespace, since the identifier
carries its own owner and scope; a resource given by name must match one
element of `resources[]` on that name and on every scope the caller gave —
type, the element's own region, zone, resource group and namespace, the
same tuple its identity is minted from — with the account and the region
narrowing the query on the lake's partitions, so the scan is bounded and an
event whose payload omits its account object is not lost (the
region partition assumes the call was logged in the resource's region, a
live-run check for services that log elsewhere); a
scoped question never broadens to an unscoped answer; such a check can return fewer rows than the limit.
The history verb requires a resource and keeps the Create, Update and
Delete calls: the newest within the limit, returned oldest first, so a long
timeline loses its oldest entries rather than the change under
investigation. A window with no end runs to the present; a window with no
start is refused. A call that names another provider is declined, so a
binding that can answer it is tried.

**Rows are reassembled into the event the lake stored.** The query keeps
API Activity rows only (`class_uid = 6003`); each projected struct is cast
to JSON and decoded back into the OCSF object, `metadata` passed through and
the `unmapped` object kept, since the lake puts source fields without a
mapping there (the recipient account of a cross-account call among them)
and the stored event is the raw evidence later reads and re-normalization
work from. A row whose payload carries no account object takes the
account from the lake's own recording-account partition, so a row the
partition filter returns is attributed to the account the question named
rather than to the caller's home account; a present account object is
never overwritten. A missing column shows as an unhealthy instance (see
the health check below) rather than as every call declining. Whether `cloud.account` always equals the
recording account when present, and whether every subscriber's table
carries `unmapped`, are live-run checks. `metadata.version` stays 1.1.0 and
the engine's 6003 normalizer does all the shaping; beyond that account, the
adapter adds only the table name as `metadata.log_name` when the lake left
it empty. Whether the lake files some IAM operations under
Account Change (3001) rather than API Activity is a live-run check; if it
does, those calls are a second class for this adapter to read.

**Resource context from AWS Config.** The latest configuration item for the
resource is shaped into one Cloud Resources Inventory Info event (5023) at
OCSF 1.7.0, the version `03 §4.18` pins for the class: the resource's
identifier, name, type, region, configuration — the base configuration
with Config's supplementary configuration merged in, since for a bucket the
policy, access control and public-access settings live there; only the
layers Config documents as encoded (the supplementary value itself, a
policy's text) are decoded, and every other string is kept as written, so a
policy condition that looks like JSON is not rewritten — tags,
creation and capture times, under the account it belongs to. Only an item
whose status says the resource is recorded and present is current context;
a deleted or unrecorded item is declined, never shown as a snapshot. A
name-identified request's zone is checked against the item's availability
zone; a resource group or namespace cannot be established from a Config
item, so a request that names one is declined. Config's resource type vocabulary is
the one OCSF already carries for AWS resources. Config keys a resource by
its own identifier, which is not always the ARN (a bucket's is its name), so
the adapter passes an identifier that is not an ARN directly and otherwise
finds the resource by type and name, then by the identifier the ARN
carries, then by the ARN itself, accepting the match only when its ARN
equals the one asked for, and a name-only lookup only when its account and
region match those given, and a name Config holds more than once for the
type is declined rather than guessed. With no type given, the type is read from the
ARN by a table the adapter owns. A resource Config has not recorded, or cannot
answer for, is declined (FALLTHROUGH), so one unanswerable lookup does not
mark the instance unhealthy for the activity verbs. An instance configured
without Config still describes the verb, since the description precedes
configuration, and declines it at call time; the engine then reports the
context unavailable.

**Errors map onto the taxonomy.** Throttling, server errors and a query
that outlives its timeout (which the adapter also stops) are RETRY; denied
access, invalid or expired credentials and a missing table or database are
UNHEALTHY; a query Athena rejects as malformed, and a call with no window,
are FATAL, except a failure that names a missing column or a type
mismatch, which is the schema-version mismatch `03 §6.2` files under
FALLTHROUGH; an operation the instance does not serve is FALLTHROUGH. A
query that fails after it started is stopped, not left to run. Health
runs the queries' own projection and predicates against the table under a
condition that matches nothing, which proves the workgroup, the
credentials, the read grant and every column and nested field any query
reads, at the cost of planning a query; it then reads the
table's schema, following a Lake Formation resource link to the shared
table it names, so a missing column is reported by name, and notes a
schema it cannot read rather than failing on it.

**Instances and scope.** The lake is organization-wide, so an operator runs
one instance per organization under `03 §3.5` source scope and narrows it to
accounts with `account_uids`; an instance per account is the same mechanism
with one account listed. An unscoped instance with no accounts listed
answers the across-accounts form of the activity verb.

**Equivalence is pinned offline.** Recorded and redacted Athena result
pages (`03 §9.1` raw responses) for the scenario's operations pass through
the adapter's row decoding and the engine's registry, and the test asserts
that the principal, account and resource ids equal those the fixture
scenario mints for the same account, principal and resources. The
observation ids differ, by design: they carry the source tool and the
payload. Two facts the documentation does not settle are recorded when the
first live query runs: what the actor's identifier and session issuer hold
for an assumed-role call, and whether a management event's resource carries
its ARN or only its name. `03 §4.15`'s shaping is correct under either
answer; a name-only resource is the split it already names.

## Alternatives rejected

- **Data access (objects plus notifications).** Needs ingestion and a
  store; a verb is a pull.
- **CloudTrail `LookupEvents`.** Not OCSF, ninety days, management events
  only, heavily rate-limited, no cross-account view.
- **CloudTrail Lake queries.** Not OCSF; a second query engine and schema.
- **Azure or Google first.** No OCSF-native export was found for either.
- **Resource context from findings in the lake.** A finding describes a
  problem, not the resource's configuration.
- **The ambient AWS credential chain.** The plugin contract gives an
  adapter its credentials through `configure` and nothing else.
- **A hand-written signer and API client on the standard library.** About
  six hundred lines that are right until a header is, plus hand-rolled
  retries and credential refresh; the SDK's modules are all Apache-2.0.
- **A shared adapter framing package in this change.** Every adapter
  copies the same stdio framing; moving it into a shared kit touches every
  adapter and is a change of its own.

## Consequences

- The AWS SDK for Go v2 and its transitive modules enter `go.mod`, every one
  Apache-2.0; the PR lists them, since the repository has no license
  manifest yet.
- The bundled-adapter registry, build targets and container image gain the
  adapter; its example manifest and tenant configuration show the bindings
  for the three verbs, in the shape the cloud scenario's bindings take.
- A live investigation against a real lake becomes possible; the scenario's
  identities are held equal to it by a test, not by inspection.
- Athena queries cost per data scanned and take seconds; the window and
  account filters are the bound, and a windowless call is refused.
- Rows can number fewer than `limit` when a scope the table cannot express
  is filtered after the fetch.
- The snapshot's created and modified times arrive as the schema's epoch
  milliseconds, where the fixture carries strings; the 5023 normalizer now
  writes both forms as RFC 3339 and the event view renders them as dates, a
  shaping change that moves that normalizer to version 3 (`03 §4.13`).
- The S3 data-event table, which answers "was anything read from the
  bucket", is a later child; this one reads the management table.
