# 0356 — Every normalizer reads the OCSF class and fields the schema defines, from a floor of 1.1.0 (or the version that introduced the class) to a ceiling of 1.7.0; a recorded schema and a lookup recorder fail a test on any class number or field path OCSF does not define

Issue: #356 · PR: #358 · Status: proposed

## Context

reckon adopts OCSF for the telemetry layer (`design/01-domain-model.md`,
Architectural commitments), and `design/03-capability-layer.md §4.16` holds
every normalizer to a version range: read the floor, take extra fields from
the ceiling when present, and detect the field rather than the version. Only
API Activity (`03 §4.15`, adr/0320) was ever checked against the published
schema.

The other class numbers and field lists came from the first spec draft. The
normalizers, fixtures and adapters were written from that draft, so they
agree with each other and every test passes, but nothing compares them with
OCSF.

A check of every path each normalizer and the event view (`capability/ocsfview`)
reads, against the schema exports for 1.1.0 and 1.7.0 from schema.ocsf.io
(Windows extension included), found:

- **Four classes registered under numbers OCSF gives to other classes:**

  | Registered | OCSF name for it | What the normalizer handles | OCSF number for that |
  |---|---|---|---|
  | 1003 | Kernel Activity | registry | Registry Key Activity 201001, Registry Value Activity 201002 (Windows extension) |
  | 1009 | Script Activity (absent at 1.1.0) | module activity | 1005 Module Activity |
  | 3005 | User Access Management | account change | 3001 Account Change |
  | 4011 | Email File Activity | email URL activity | 4012 Email URL Activity |

- **The account-change normalizer reads three classes' fields as one.**
  OCSF has three classes:
  - Account Change (3001) carries `user` and `user_result`.
  - User Access Management (3005) carries `user`, `privileges[]` and `resource`.
  - Group Management (3006) carries `user`, `group` and `privileges[]`.

  The normalizer reads `group` and a single-string `privileges` under one
  number. The Okta adapter emits all its account events as 3005. It also
  emits its user records as fake account changes, from before the user
  inventory normalizer (5003, `03 §4.19`) existed. The Security Lake adapter
  already names 3001 Account Change.

- **Field paths OCSF does not define, at any version in range:**

  | Normalizer | Reads | OCSF defines |
  |---|---|---|
  | Registry | `reg_key.hive` | Only `reg_key.path`, the full path, on 201001. 201002 carries `reg_value` and names the key by `reg_value.path`. |
  | Scheduled job | `job.path`, `job.command_line` | `job.file.path`, `job.cmd_line` |
  | Network activity | `process.*` | `actor.process.*` |
  | Email | `email.attachments[]` | `email.files[]`, from 1.4.0. Below that an attachment is its own Email File Activity event (4011). |
  | Email | `email.bcc[]` | Nothing |
  | Email URL | `email.message_uid` | `email_uid` at the top of the event, "used to correlate related email alert and activity events", which matches `email.uid`, not `email.message_uid` |
  | Detection finding | `finding.*` (`uid`, `title`, `types`, `severity`, `confidence`) | `finding_info.*`, top-level `severity`/`severity_id` and `confidence`/`confidence_id`/`confidence_score` |
  | Detection finding | `evidence`: one embedded event with its own `class_uid`, normalized through the registry | `evidences[]`: a list of evidence artifacts (`actor`, `process`, `file`, `src_endpoint`, `dst_endpoint`, `connection_info`, `query`, `api`, `data` at 1.1.0; `device`, `user`, `url`, `email`, `reg_key`, `reg_value`, `job` and more by 1.7.0), none of them an event |
  | Incident finding (case) | `finding_info.*`, `assignment_group`, `unmapped.link` | `finding_info_list[]`, `assignee_group` (a `group`), `src_url` |
  | Device inventory | `device.criticality` | Nothing, up to 1.9.0 |

  The ServiceNow bridge writes the incident fields above, and the CrowdStrike
  Falcon and GreyNoise adapters write `finding` and `evidence`. The
  lateral-movement, sign-in and cloud scenarios' fixtures do the same.

- **Shared helpers probe fields some classes lack.** The OS-family probe reads
  `src_endpoint.os`/`dst_endpoint.os` in classes that have no endpoints, and
  the host extractor reads `uid_alt` on a network endpoint, which has none.
  Nothing reads wrongly as a result, but a schema check cannot tell these reads
  from mistakes.

- **Classes newer than the floor.** Cloud Resources Inventory (5023) first
  appears in 1.4.0. `device.agent_list`, `device.owner`, `email.files`,
  `email.urls`, a user inventory event's `device`, `actor.app_name`,
  `programmatic_credentials` and `resources[].zone` arrive between 1.2.0 and
  1.6.0. Every one of them is already read as optional.

Every other registered class, and every other path, resolves at both ends.

The floor is 1.1.0 because Security Lake writes it: AWS's page on OCSF in
Security Lake gives `metadata.version` `1.1.0` for every version-2 native
source (version 1 wrote `1.0.0-rc.2`). Custom sources may write up to 1.3.
Exports of 1.8.0 and 1.9.0 fail on schema.ocsf.io ("Cannot export in the
legacy format"), so the ceiling stays at 1.7.0, the newest exportable release.

The OCSF schema is Apache-2.0.

## Decision

**Each class's floor is 1.1.0, or the version that introduced the class if
later. The ceiling is 1.7.0 for all.** `03 §4.16` applies to every normalizer,
not only §4.15's. Cloud Resources Inventory's floor is 1.4.0. A field above a
class's floor is read only when present, as today.

**Class numbers are OCSF's.** Each normalizer registers the OCSF number, and
the event view's sentence templates use the same number:

- **Registry Key Activity (201001)** reads `reg_key.path`.
- **Registry Value Activity (201002)** reads `reg_value.{path,name,data,type,type_id}`.
  - Both registry classes share one normalizer, so a key event and a value
    event produce the same registry object. Until adr/0341 lands, that object
    is today's `x-registry-key`: `hive` is the path's first segment and
    `key_path` is the rest.
  - 1003 is left unregistered, so a Kernel Activity event goes to the opaque
    default instead of being read as a registry change.
- **Module Activity (1005)** reads the `module.file.*` and `actor.process.*`
  fields it reads today. Those fields were already OCSF.
- **Account Change (3001), User Access Management (3005) and Group Management
  (3006)** share one shaping function, registered three times. Each reads only
  its class's fields:
  - `user`, `actor.user` and `device` for all three.
  - `group` (`name`, `domain`, `uid`) for 3006 only.
  - `privileges[]` for 3005 and 3006, kept as a list in the observation's
    extensions as `privileges`.
  - The `x-group` and `member-of-group` output for 3006 is unchanged.
- **Email URL Activity (4012).**
- **Email File Activity (4011) is handled, new.** It is how an attachment
  arrives below 1.4.0. The event's `file` gives the attachment's `file`,
  `email_uid` gives the weak `email-message` (as for a click), and a
  `contains` relationship joins them, so an attachment hash stays a pivot at
  the floor.

**Field paths are OCSF's, class by class:**

- **Scheduled job:** the task path is `job.file.path`, the command line
  `job.cmd_line`.
- **Network activity:** the process is `actor.process`.
- **Email activity:**
  - Attachments are `email.files[]`, read when present (1.4.0+).
  - `email.urls[]` is read the same way.
  - Blind copy is read from `unmapped.bcc[]`, where an adapter whose
    vendor reports it puts it. OCSF defines no blind copy, and `unmapped` is
    OCSF's place for a source's own fields. `bcc_refs` stays.
- **Email URL activity: the click names the message by `email_uid`.** The
  `email-message` identity follows the rule hosts and accounts already
  follow (adr/0211, adr/0324): strongest identifier first; a lesser one
  mints its own weak object (`weak: true`), which never joins another on
  its own and is joined only by an `aliases` edge from a record that names
  both.
  - An email that carries `email.message_uid` (the RFC 5322 Message-ID) is
    identified by it, as today.
  - An email that carries only the source's `email.uid` mints a weak
    `email-message` on `(source_tool, uid)`, `weak: true`, the counterpart
    of the host's `(source_tool, device_uid)`. That is the only identity a
    click can name.
  - An email activity event carrying both mints the strong message and the
    weak one, with an `aliases` edge, so the click stitches to the delivered
    message through it.
- **Detection finding, read from OCSF's fields:**
  - The finding is `finding_info.{uid,title,types}`.
  - Severity is `severity_id`, falling back to the `severity` caption.
  - Confidence is `confidence_score`, falling back to `confidence_id`.
  - The Indicator's `pattern` keeps its vendor-specific fallback.
- **Detection finding: evidence is `evidences[]`, a list of artifacts, not an
  event.**
  - Each artifact is read through the same extractors the activity
    normalizers use:
    - `process` and `actor.process` give a process.
    - `file` gives a file.
    - `src_endpoint`/`dst_endpoint` give addresses and domains.
    - `actor.user` and, from 1.4.0, `user` give accounts.
    - `query` gives a domain.
    - `url` gives a URL, from its version on.
    - `device` gives the host, from its version on.
  - An artifact with no `device` takes the finding's own `device`. A
    detection finding always carries one.
  - Everything an artifact names goes into the Sighting's
    `where_sighted`/observation refs, as the recursion's output did.
  - No artifact is dispatched by a class number. OCSF gives artifacts none,
    so the recursion through the registry is gone.
- **Incident finding:**
  - Case title, uid and description come from `finding_info_list[]` (the
    first entry names the case; every entry's uid is kept).
  - The assignment group is `assignee_group.name`, else `assignee_group.uid`.
  - The link back to the system of record is `src_url`.
  - The system-of-record name stays in `unmapped.sor`, since OCSF has no
    field for it and `unmapped` is OCSF's place for a source's own fields.
- **Device inventory:** criticality is read from `unmapped.criticality`, for
  the same reason. Nothing up to 1.9.0 defines it on `device`.
- **Helpers take their locations from the caller.** The OS-family probe and
  the host extractor read only the endpoints and objects the calling class
  passes them, so every lookup names a field the class defines.

**Adapters and fixtures write what the normalizers read.**

- **Okta:**
  - Lifecycle and account events become 3001.
  - `group.user_membership.*` becomes 3006.
  - Privilege grants and revocations become 3005.
  - User records become 5003 User Inventory.
- **ServiceNow:** the bridge writes `finding_info_list`, `assignee_group` and
  `src_url`.
- **CrowdStrike Falcon and GreyNoise:** write `finding_info` and `evidences[]`.
- **Every fixture** is shaped at its class's floor and declares it in
  `metadata.version`. This finishes the "Test both ends" rule `03 §4.16` set
  for the two older scenarios. A fixture that needs an above-floor field
  declares the version that carries it.

**A test fails on a class or field OCSF does not define.**

- **A recorded schema:**
  - A generator (`scripts/ocsf-snapshot`, run by hand) writes, for each of
    1.1.0, 1.4.0 and 1.7.0, the attribute tree of every class reckon
    registers, from schema.ocsf.io's export.
  - The output goes under `capability/testdata/ocsf/<version>.json`, with the
    OCSF Apache-2.0 notice beside it.
- **Three checks against the snapshots:**
  1. **Registration.** Every registered class number exists in the ceiling
     snapshot under the OCSF name the normalizer gives its builder (`"account_change"`,
     `"registry_value_activity"`, …).
  2. **Lookups.** Every lookup the normalizer and event-view tests make goes
     through the one path-lookup function each package already has. A test
     hook records the lookups, and each recorded path must resolve in the
     ceiling snapshot for its class. `unmapped.*` is exempt.
  3. **Fixtures and adapters.**
     - Every fixture payload is walked, and each field must resolve for its
       class at the version it declares.
     - Each in-tree adapter's emitted events are checked the same way in its
       own tests.
- **Moving the ceiling** is regenerating the snapshot and fixing what fails.

**Every normalizer whose input changes moves to its next version.** There is
no back-compat to keep, so there is no backfill and no reader for the old
class numbers.

## Alternatives rejected

- **Keep reckon's numbers and translate OCSF's at ingest.** Every source that
  writes OCSF would need a translation table reckon invented, which is the
  opposite of adopting the schema.
- **One account normalizer under 3001, reading `group` and `privileges` when
  present.** Group Management and User Access Management events would still go
  to opaque, and 3001 never carries those fields.
- **Keep the embedded-event evidence and also read `evidences[]`.** Two shapes
  for one thing, and the embedded one is not OCSF. Every in-tree producer can
  be changed, since there is no back-compat to keep.
- **Recurse each evidence artifact through the registry by guessing a class
  from its fields.** Guessing a class from field names is not shaping by
  detected field. It is what `03 §4.16` forbids for versions, applied to
  classes.
- **Keep `email-message` identity on `message_uid` alone and have a click
  fetch the message.** A normalizer does no I/O (`03 §4.13`), and OCSF's click
  event names the message only by `email_uid`.
- **Validate against a schema fetched at test time.** Tests run offline and
  must not change when schema.ocsf.io does. The snapshot is versioned and
  regenerated on purpose.
- **Each normalizer declares its field list for the check.** A declared list
  drifts from the code that reads. Recording the lookups the tests make
  checks the reads themselves.

## Consequences

- An OCSF source's registry, module, account, group, privilege, email URL,
  detection and incident events normalize instead of falling to opaque, and a
  Kernel Activity event is no longer read as a registry change.
- The data the model reads changes in all three scenarios: detection findings,
  cases, the privilege event and the Run-key events. No prompt text, tool
  definition or engine-text template changes, so `PromptVersion` is unchanged.
  Each scenario is re-run on both models before merge.
- The lookup check covers what the tests exercise. A read on a branch no test
  takes is not recorded, so each normalizer's tests must cover its fallbacks.
- The registry change here is input-only. adr/0341 then replaces
  `x-registry-key` with STIX's `windows-registry-key` on top of these classes
  and fields, and its input section and its consequence about class 1003 are
  revised to match.
- The snapshots are third-party material (Apache-2.0) committed with their
  notice.
