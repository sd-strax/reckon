# 0356 — Normalizers use OCSF's own class numbers and fields, checked by a test against the OCSF schema checked into the repo

Issue: #356 · PR: #358 · Status: proposed

## Context

reckon uses OCSF for telemetry (`design/01-domain-model.md`, Architectural
commitments). `design/03-capability-layer.md §4.16` sets a version range for
each normalizer:
- It must work with the oldest version (the floor).
- It reads newer fields (up to the ceiling) only when they are present.
- It checks whether a field exists, never the version number.

Only API Activity (`03 §4.15`, adr/0320) was ever checked against the
published schema.

The other class numbers and field lists came from the first spec draft. The
normalizers, fixtures and adapters were all written from that draft. They
agree with each other, so every test passes, but nothing compared them with
OCSF.

We checked every field path that each normalizer and the event view
(`capability/ocsfview`) reads. The check used the schema exports for 1.1.0,
1.7.0 and 1.9.0 from schema.ocsf.io, Windows extension included. It found the
problems below.

**Four classes are registered under the wrong number.** Each number belongs
to a different OCSF class:

| reckon registers | That number in OCSF is | The normalizer actually handles | OCSF's number for that |
|---|---|---|---|
| 1003 | Kernel Activity | registry | 201001 Registry Key Activity and 201002 Registry Value Activity (Windows extension) |
| 1009 | Script Activity (not in 1.1.0) | module loads | 1005 Module Activity |
| 3005 | User Access Management | account changes | 3001 Account Change |
| 4011 | Email File Activity | email link clicks | 4012 Email URL Activity |

**The account normalizer mixes three OCSF classes into one.** OCSF has:
- Account Change (3001), with `user` and `user_result`.
- User Access Management (3005), with `user`, `privileges[]` and `resource`.
- Group Management (3006), with `user`, `group` and `privileges[]`.

reckon reads `group` and a single-string `privileges` under one number.

The Okta adapter sends all its account events as 3005. It also sends its user
records as fake account changes. That dates from before the user inventory
normalizer (5003, `03 §4.19`) existed. The Security Lake adapter already
names 3001 correctly.

**Some fields reckon reads are not in OCSF at any version in range:**

| Normalizer | reckon reads | OCSF has |
|---|---|---|
| Registry | `reg_key.hive` | Only `reg_key.path`, the full path, on 201001. A 201002 event carries `reg_value` and names the key in `reg_value.path`. |
| Scheduled job | `job.path`, `job.command_line` | `job.file.path`, `job.cmd_line` |
| Network activity | `process.*` | `actor.process.*` |
| Email | `email.attachments[]` | `email.files[]` from 1.4.0. Before that, each attachment is its own Email File Activity event (4011). |
| Email | `email.bcc[]` | Nothing up to 1.8.0. `email.bcc[]` from 1.9.0. |
| Email link click | `email.message_uid` | `email_uid` at the top of the event. It matches `email.uid`, not `email.message_uid`. |
| Detection finding | `finding.*` (`uid`, `title`, `types`, `severity`, `confidence`) | `finding_info.*`, plus `severity`/`severity_id` and `confidence`/`confidence_id`/`confidence_score` at the top of the event |
| Detection finding | `evidence`: one embedded event with its own `class_uid` | `evidences[]`: a list of evidence pieces. At 1.1.0 a piece can hold `actor`, `process`, `file`, `src_endpoint`, `dst_endpoint`, `connection_info`, `query`, `api` and `data`. By 1.7.0 it can also hold `device`, `user`, `url`, `email`, `reg_key`, `reg_value`, `job` and more. None of them is an event. |
| Incident finding (case) | `finding_info.*`, `assignment_group`, `unmapped.link` | `finding_info_list[]`, `assignee_group` (a `group`), `src_url` |
| Device inventory | `device.criticality` | Nothing, up to 1.9.0 |

The ServiceNow bridge writes the incident fields above. The CrowdStrike
Falcon and GreyNoise adapters write `finding` and `evidence`. The fixtures in
all three scenarios do the same.

**Shared helpers look for fields some classes don't have.**
- The OS-family helper looks at `src_endpoint.os` and `dst_endpoint.os`, even
  in classes with no endpoints.
- The host helper looks for `uid_alt` on a network endpoint, which has no such
  field.

Neither causes a wrong read, but a schema check can't tell these lookups apart
from real mistakes.

**One class and several fields are newer than 1.1.0.** Cloud Resources
Inventory (5023) first appears in 1.4.0. These fields arrive between 1.2.0 and
1.6.0:
- `device.agent_list` and `device.owner`
- `email.files` and `email.urls`
- `device` on a user inventory event
- `actor.app_name` and `programmatic_credentials`
- `resources[].zone`

reckon already reads all of them only when present.

Every other registered class, and every other path, is correct at both ends
of the range.

**Why the floor is 1.1.0.** Security Lake writes it. AWS's page on OCSF in
Security Lake gives `metadata.version` `1.1.0` for every version-2 native
source. Version-1 sources wrote `1.0.0-rc.2`. Custom sources may write up
to 1.3.

**Why the ceiling is 1.9.0.** It is the newest OCSF release.
- The site's bulk export of 1.9.0 fails by default because two optional
  profiles (`linux_users`, `macos_users`) both define the same field.
- Leaving those two profiles out makes the export work, as reckon reads
  nothing from either profile.

From 1.7.0 to 1.9.0, nothing reckon reads is removed or becomes required.
Three things change:
- `email.bcc[]` is added in 1.9.0.
- `job.cmd_line` is deprecated in 1.9.0. Its replacement is the job's list of
  actions, `job.job_actions[].cmd_line`. (The deprecation note says
  `job_action`, but that is the object's type name. The field is called
  `job_actions`.)
- `actor.app_name` is deprecated in 1.9.0. Its replacement is
  `actor.application.name`.

Some fields that are required at 1.1.0 become optional later:
- `email.from`, `email.to` and `finding_info.title` become recommended in
  1.4.0.
- `job.name` and `privileges` become recommended in 1.9.0.

The normalizers already handle a missing field (`03 §4.16`: a version defines
fields, and a source fills only some of them). So this needs no new code.
Each normalizer's tests cover an event without each of these fields.

`03 §4.18` gave Cloud Resources Inventory the range 1.7.0 to 1.7.0. That was
because its only source is an adapter reckon ships, which writes 1.7.0. A test
already shows that a 1.4.0-shaped event gives the same ids.

The OCSF schema is licensed Apache-2.0.

## Decision

**The version range.**
- Each class's floor is 1.1.0, or the version that introduced the class if
  that is later.
- The ceiling is 1.9.0 for every class.
- `03 §4.16` now applies to every normalizer, not just `§4.15`.
- Cloud Resources Inventory's range becomes 1.4.0 to 1.9.0. Its fixture still
  declares 1.7.0, because that is the version it is written in.
- A field newer than a class's floor is read only when present, as today.
- When a field is deprecated inside the range, the normalizer reads the
  replacement first and the old field as a fallback (`03 §4.16`). The new
  pairs are listed below.

**Class numbers are OCSF's.** Each normalizer registers OCSF's number. The
event view's sentence templates use the same numbers.

- **Registry Key Activity (201001)** reads `reg_key.path`.
- **Registry Value Activity (201002)** reads
  `reg_value.{path,name,data,type,type_id}`.
  - One normalizer handles both registry classes, so a key event and a value
    event produce the same type of registry object. Their ids differ,
    because a value's id includes the value name (§4.6).
  - Until adr/0341 lands, that type is today's `x-registry-key`. `hive` is
    the first segment of the path and `key_path` is the rest.
  - 1003 is left unregistered. A Kernel Activity event then goes to the
    opaque default instead of being read as a registry change.
- **Module Activity (1005)** reads the same `module.file.*` and
  `actor.process.*` fields as today. Those fields were already correct.
- **Account Change (3001), User Access Management (3005) and Group
  Management (3006)** share one function, registered three times. Each class
  reads only its own fields:
  - `user`, `actor.user` and `device` for all three.
  - `group` (`name`, `domain`, `uid`) for 3006 only.
  - `privileges[]` for 3005 and 3006. It is kept as a list, `privileges`, in
    the observation's extensions.
  - 3006 still produces the `x-group` and the `member-of-group` link, as
    today.
- **Email URL Activity (4012)** handles link clicks.
- **Email File Activity (4011) is newly handled.** Before 1.4.0, this event is
  the only place an attachment appears.
  - The event's `file` becomes the attachment's `file` object.
  - Its `email_uid` gives a weak `email-message`, the same way a click does.
  - A `contains` link joins the message to the file, so attachment hashes stay
    usable at the floor.

**Field paths are OCSF's.**

- **File hashes are OCSF's list form only.** OCSF writes `hashes` as a list
  of fingerprints, each with `algorithm`, `algorithm_id` and `value`. reckon
  also accepts an object form, `{"SHA-256": "…"}`, which is not OCSF. The
  object form is dropped, and the fixtures that use it move to the list.

- **Scheduled job:**
  - The task path is `job.file.path`.
  - The command line comes from `job.job_actions[].cmd_line` (1.9.0 and
    later), else from `job.cmd_line`.
  - When a task has several actions, `command_line` holds the first one.
    `command_lines` holds the list of all of them.
- **Network activity:** the process is `actor.process`.
- **API activity:** the calling application comes from
  `actor.application.name` (1.9.0 and later), else from `actor.app_name`.
- **Email activity:**
  - Attachments come from `email.files[]` when present (1.4.0 and later).
  - Links come from `email.urls[]`, the same way.
  - Blind copy comes from `email.bcc[]` (1.9.0 and later). Before 1.9.0, it
    comes from `unmapped.bcc[]`, if the adapter's vendor reports it.
    `unmapped` is where OCSF puts a source's own fields. `bcc_refs` stays.
- **Email link click:**
  - The click names its message by `email_uid`.
  - The `email-message` identity follows the same rule as hosts and accounts
    (adr/0211, adr/0324). The strongest identifier is used first.
  - A weaker identifier creates its own weak object, marked `weak: true`.
    A weak object never merges with another by itself. Only an `aliases` link
    from a record that names both identifiers joins them.
  - So:
    - An email with `email.message_uid` (the RFC 5322 Message-ID) is
      identified by it, as today.
    - An email with only the source's `email.uid` creates a weak
      `email-message` keyed on `(source_tool, uid)`. This matches the host's
      weak key, `(source_tool, device_uid)`. A click can only name this weak
      object.
    - An email activity event with both ids creates the strong message and
      the weak one, plus an `aliases` link from the weak message to the
      strong one, as for hosts and accounts. That link is how a click
      reaches the delivered message.
- **Detection finding fields:**
  - The finding comes from `finding_info.{uid,title,types}`.
  - Severity comes from `severity_id`, else from the `severity` text.
  - Confidence comes from `confidence_score`, else from `confidence_id`.
  - OCSF has no field for a detection's pattern. An adapter whose vendor
    gives one puts it in `unmapped.pattern`, and the Indicator's `pattern`
    is read from there. Without it, the Indicator keeps today's
    vendor-specific fallback pattern.
- **Detection finding evidence is `evidences[]`.** It is a list of evidence
  pieces, not an event.
  - Each piece is read with the same helpers the activity normalizers use:
    - `process` and `actor.process` give a process.
    - `file` gives a file.
    - `src_endpoint` and `dst_endpoint` give addresses and domains.
    - `actor.user` gives an account, and so does `user` from 1.4.0.
    - `query` gives a domain.
    - `url` gives a URL, in the versions that have it.
    - `device` gives the host, in the versions that have it.
  - A piece with no `device` uses the finding's own `device`. A detection
    finding always has one.
  - Everything a piece names goes into the Sighting's `where_sighted` and the
    observation's refs, as before.
  - Pieces are no longer sent through the registry by class number. OCSF
    gives them no class number.
- **Incident finding:**
  - The case title, uid and description come from `finding_info_list[]`. The
    first entry names the case. Every entry's uid is kept.
  - The assignment group comes from `assignee_group.name`, else from
    `assignee_group.uid`.
  - The link back to the ticket system is `src_url`.
  - The ticket system's name stays in `unmapped.sor`, as OCSF has no field
    for it.
- **Device inventory:** criticality comes from `unmapped.criticality`, as
  OCSF has no such field on a device up to 1.9.0.
- **Helpers get their field locations from the caller.** The OS-family helper
  and the host helper read only the endpoints and objects the calling class
  passes in. So every lookup names a field that class actually has.

**Adapters and fixtures produce what the normalizers read.**

- **Okta:**
  - Lifecycle and account events become 3001.
  - `group.user_membership.*` events become 3006.
  - Privilege grants and revocations become 3005.
  - User records become 5003 User Inventory.
- **ServiceNow:** the bridge writes `finding_info_list`, `assignee_group` and
  `src_url`.
- **CrowdStrike Falcon and GreyNoise:** write `finding_info` and
  `evidences[]`.
- **Every fixture** is written at its class's floor and states that version
  in `metadata.version`. This completes the "Test both ends" rule in
  `03 §4.16` for the two older scenarios. A fixture that needs a newer field
  states the version that has it.

**A test fails on any class or field that OCSF doesn't define.**

- **The OCSF schema is checked into the repo, one file per release.**
  - Every release in the supported range is included: 1.1.0, 1.2.0, 1.3.0,
    1.4.0, 1.5.0, 1.6.0, 1.7.0, 1.8.0 and 1.9.0.
  - Each is the full schema as schema.ocsf.io exports it, with the Windows,
    Linux and macOS extensions. It is not trimmed to the classes reckon uses.
  - The files live in `third_party/ocsf-schema/<version>/schema.json`.
  - OCSF's `LICENSE` (Apache-2.0) and `NOTICE` are copied verbatim into
    `third_party/ocsf-schema/`, as Apache-2.0 requires.
  - A `README.md` there records the provenance: the source, the export URL
    with its parameters, the profiles left out and why, the download date,
    and that the files are unmodified.
  - A script, `scripts/fetch-ocsf-schema`, downloads a release the same way.
    It is run by hand when a new release is added.
- **Three checks run against these copies.**
  1. **Registration.** Every registered class number exists in the ceiling
     copy, under the OCSF name the normalizer uses (`"account_change"`,
     `"registry_value_activity"`, and so on).
     - Three normalizers use their own names today. They take OCSF's:
       `case` becomes `incident_finding`, `cloud_inventory` becomes
       `cloud_resources_inventory_info`, and `device_inventory_info` becomes
       `inventory_info`.
     - The name appears in each observation's provenance, so provenance
       changes too. It is not part of any id.
  2. **Lookups.** Each package already has one function for looking up a
     field path. During tests, a hook records every path looked up through
     it. Each recorded path must exist in the ceiling copy for its class.
     `unmapped.*` is exempt.
     - The check only sees lookups the tests make. So the tests must cover
       every helper and every fallback a normalizer has.
  3. **Fixtures and adapters.**
     - Every fixture is walked field by field. Each field must exist for its
       class at the version the fixture declares.
     - The walk also checks each value's shape. A list field must hold a
       list, an object field an object, and a scalar field a scalar. That
       catches a field read in the wrong shape, such as `privileges` as one
       string instead of a list.
     - Each in-tree adapter's tests check its output events the same way.
- **To move the ceiling later,** fetch the new release and fix whatever fails.

**Every normalizer whose input changes moves to its next version.** There is
no back-compat to keep. So there is no backfill and nothing reads the old
class numbers.

## Alternatives rejected

- **Keep reckon's numbers and translate OCSF's at ingest.** Every OCSF source
  would then need a translation table that reckon invented. That defeats the
  point of adopting OCSF.
- **One account normalizer under 3001 that also reads `group` and
  `privileges` when present.** Group Management and User Access Management
  events would still go to the opaque default. And a 3001 event never has
  those fields.
- **Keep the embedded-event evidence and also read `evidences[]`.** That is
  two shapes for one thing, and the embedded one isn't OCSF. Every in-tree
  producer can be changed, since there is no back-compat to keep.
- **Guess a class for each evidence piece from its fields, then send it
  through the registry.** That is guessing from field names. `03 §4.16`
  forbids guessing for versions, and the same reasoning applies to classes.
- **Keep `email-message` identity on `message_uid` only, and have the click
  look up its message.** A normalizer does no I/O (`03 §4.13`). And OCSF's
  click event names the message only by `email_uid`.
- **Check against the live schema at test time.** Tests run offline, and they
  must not change when schema.ocsf.io changes. The checked-in copy is
  versioned and only updated on purpose.
- **Check in only the classes reckon registers.** A trimmed copy has to be
  regenerated every time reckon adds a class. It also can't answer what a
  class reckon doesn't handle yet looks like. The full schema answers both.
- **Check in only the versions a fixture declares.** An adapter may write any
  release in the range, for example 1.3 from a Security Lake custom source.
  Its events can only be checked against that release if the release is in
  the repo.
- **Reference OCSF's source repository at each release tag instead.** That
  repository holds the schema's source files. Classes and objects there
  inherit from each other, and only OCSF's schema server resolves that into
  final field lists. The export is that resolved form, so it is what a test
  can check against directly.
- **Have each normalizer declare its field list for the check.** A declared
  list drifts away from the code. Recording the actual lookups checks what
  the code really reads.

## Consequences

- Registry, module, account, group, privilege, email link, detection and
  incident events from an OCSF source now get normalized. Before, they went
  to the opaque default.
- A Kernel Activity event is no longer read as a registry change.
- The data the model sees changes in all three scenarios. The affected events
  are detection findings, cases, the privilege event and the Run-key events.
  No prompt text, tool definition or engine-text template changes, so
  `PromptVersion` stays the same. Each scenario is re-run on both models
  before merge.
- The lookup check only covers code paths the tests run. A read on a branch
  no test takes isn't recorded. So each normalizer's tests must cover its
  fallbacks.
- The registry change here only affects input. adr/0341 then replaces
  `x-registry-key` with STIX's `windows-registry-key`, using these classes and
  fields. Its input section and its note about class 1003 get updated to
  match.
- The schema files are third-party material (Apache-2.0). They are committed
  with OCSF's licence and notice, and with a record of where they came from.
- The nine files are 25 MB of JSON. Git stores them compressed, so the
  repository grows by about 3 MB.
- Each schema file is one long line, as the server returns it. A
  `.gitattributes` entry marks them as not diffable, so `git show` and
  `git log -p` stay readable.
- One of the pre-commit check's keywords now matches as a whole word only.
  As a substring, it also matched a product name inside the OCSF schema.
