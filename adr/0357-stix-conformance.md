# 0357 — Stored STIX objects are valid STIX 2.1, and reckon's custom types are declared by extension definitions under `reckon-` names

Issue: #357 · PR: — · Status: proposed · Amended by: adr/0371

Related:

- adr/0341 replaced `x-registry-key` with STIX's `windows-registry-key`.
- #355 states which parts of STIX the engine adopts.
- #356 makes the normalizers read OCSF as the schema defines it. It edits the
  detection-finding normalizer too.

## Context

The interpretation layer adopts STIX 2.1 (`design/01-domain-model.md`,
Architectural commitments §2). The engine uses STIX names, but what it stores
is not STIX JSON.

**Evidence is stored as Go structs.** `evidence/store.go` (`upsertObject`)
writes the normalizer's structs (`capability/interpretation.go`) as they are.

- An SCO keeps its properties under a `properties` key. STIX puts them at the
  top level.
- Observed-data, relationship, indicator, sighting, identity and
  `x-null-observation` payloads have no `type`, `spec_version`, `created` or
  `modified`. Those values exist only as table columns.
- A relationship payload has no `"type": "relationship"`.
- Every object has a top-level `provenance` property. STIX does not define
  it, and no extension declares it.

The reasoning objects (`x-hypothesis`, `x-prediction` and the attestation
`note`, written by `aggregate/reasoning_projection.go`) are already in STIX
form. So the store holds two different shapes.

**Custom types and properties use a deprecated mechanism.** STIX 2.1 §11 is
titled "Customizing STIX (Deprecated)". It keeps custom objects and custom
properties "for backward-compatibility". It says they "should not be used to
implement any new extensions". §7.3 replaces them with the
`extension-definition` object. An extension definition has a name, a version,
a schema and a list of extension types. Each object that uses it names it
under `extensions`.

reckon has no extension definitions. Its custom types are:

- `x-hypothesis`, `x-prediction`, `x-interpretation`, `x-action` and
  `x-null-observation`.
- `x-host`, `x-scheduled-task`, `x-group`, `x-cloud-account` and
  `x-cloud-resource`.
- `x-registry-key`, which adr/0341 removed.

Its custom properties are:

- `x_reckon_attestation` on the note.
- `provenance` on every evidence object.
- The observed-data `extensions` map, keyed by plain names such as
  `logon_type` (`capability/normalizer_auth.go`). In STIX 2.1 a key in
  `extensions` must be an extension-definition id.

**Imported detections put the wrong things in native fields.** Both cases
are in `capability/normalizer_detection.go`.

- ATT&CK technique ids go into an indicator's `indicator_types`. That field's
  vocabulary is values like `malicious-activity`.
  `design/06-knowledge-service.md §3.2` says summaries read techniques from
  it, but no code does.
- A detection with no pattern produces an indicator with no `pattern` and no
  `pattern_type`. STIX requires both.

adr/0356 already fixed two related problems in the same normalizer:

- `where_sighted_refs` no longer holds the entities the detection named.
  It is now empty. The entities reach the sighting through
  `observed_data_refs`.
- Confidence is left out when unknown, a score is capped at 0–100, and a
  level maps to 15, 50 or 85, as STIX 2.1 Appendix A gives.

**Some spec text disagrees with the store or with STIX.**

- `02 §2.2` promises a history table per STIX type. The store has one
  `stix_objects` table and no history tables. Evidence rows are written once
  and never changed. Reasoning objects change through projections, and the
  event log holds their history.
- `01` specifies `x-supports` and `x-refutes` relationships that start from a
  sighting. STIX §5.1 says a relationship's ends must be SDOs or SCOs. A
  sighting is an SRO, so it cannot be one. No code creates these
  relationships yet.
- `01` gives a sighting's `confidence` as `HIGH | MEDIUM | LOW`. STIX
  confidence is an integer from 0 to 100. The code already writes an integer.
- `01 §4`, `03 §7` and `CLAUDE.md` say there are three identity deviations.
  `03 §4.10` describes a fourth, for email-message. Two more are practised but
  not listed:
  - SCO ids use a namespace per tenant (`01 §5`).
  - SDO and SRO ids are UUIDv5. STIX §2.9 says they should be UUIDv4.
- `03 §4.12` calls the indicator an SCO and the sighting an SDO. The indicator
  is an SDO and the sighting is an SRO.
- `01` (Adopted vs invented) and `CLAUDE.md` describe the `x-` prefix as the
  STIX rule for custom types. That rule is the deprecated one.

**No rule says how reckon's own STIX types change.** `03 §4.16` has rules for
OCSF, which reckon reads. STIX is what reckon writes, and it has no such
rules. STIX now changes through extension definitions and errata, not new
versions. 2.1 is the OASIS Standard, and Errata 01 is a Committee
Specification Draft. The committee's documentation lists no later version.

Constraints:

- **A type name is part of every id.** The type is the first part of an id,
  as in `x-host--<uuid>`. So renaming a type changes every id of that type.
  The product is still in development, so there is no stored data to keep
  (see the reset below).
- **Type names appear in the text the model reads.** The tool descriptions
  name `x-hypothesis` ids (`agent/tools.go`), and every id the model sees
  starts with its type. Changing that text changes the prompt version, so the
  eval scenarios need new baselines (`design/10-eval-harness.md §4.4`). #356
  needs new baselines too, because it changes the fixtures.
- **A company name can change.** A company can be renamed, acquired or
  closed. A product and its type names usually outlive that.
- **The model reads no stored payload.** Capability tool results give the
  agent refs and OCSF events (`server/capability_invoke.go`). So changing the
  stored shape does not change the text the model reads (adr/0232).
- **The OASIS schemas are BSD-3-Clause.** They are published in
  `oasis-open/cti-stix2-json-schemas`. They use JSON Schema draft 2020-12,
  and they refer to each other across files.
- **A Go validator handles them.** `google/jsonschema-go` (MIT licence) was
  run against all 57 schema files. It resolved every file. It accepted valid
  objects and rejected the shapes reckon stores today.

## Decision

**A stored STIX object is a valid STIX 2.1 object.** Each payload in
`stix_objects` and `stix_edges` is the object's STIX JSON. One serializer
writes every payload.

- Every object has `type`, `spec_version: "2.1"` and `id`.
- Every SDO and SRO has `created` and `modified` as STIX timestamps in UTC.
- An SCO's properties are at the top level.
- A relationship has `"type": "relationship"`, `relationship_type`,
  `source_ref` and `target_ref`.
- The vendor identity has `identity_class: organization`.
- The table columns stay. They are indexes over the payload and are filled
  from it.

**Each custom type has an extension definition and a `reckon-` name.**

- The `x-` prefix belongs to the deprecated mechanism, so it goes. The new
  prefix is `reckon-`, the product's name. It keeps reckon's names apart from
  any type a later STIX version adds.
- The ten custom types are renamed and each gets one `extension-definition`:
  - `new-sdo`: `reckon-hypothesis`, `reckon-prediction`,
    `reckon-interpretation`, `reckon-action` and `reckon-null-observation`.
  - `new-sco`: `reckon-host`, `reckon-scheduled-task`, `reckon-group`,
    `reckon-cloud-account` and `reckon-cloud-resource`.
- Every object of a custom type names its definition, for example
  `extensions: {"extension-definition--<id>": {"extension_type": "new-sdo"}}`.
- `reckon-interpretation` and `reckon-action` live inside the aggregate, not
  in the STIX store. They still get definitions, so every reckon type is
  declared in one place.
- The three custom relationship types with `x-` drop it too:
  `reckon-supports`, `reckon-refutes` and `reckon-authenticated-to`. A
  relationship type is a value, so it needs no extension definition.
  Relationship types without `x-`, such as `loads` and `clicked`, keep their
  names.
- The rename reaches everywhere the old names appear in current code and
  text: code, tests, fixtures, specs, `CLAUDE.md`, the workbench, the eval
  graders, and the prompt and tool text.
- Past ADRs and their index rows keep the names they were written with.
  They record what was decided at the time.

**Custom properties become property-extensions.**

- `reckon-provenance` replaces the top-level `provenance` on every evidence
  object. It holds `derivation_mode`, `tool`, `normalizer`,
  `normalizer_version` and `observed_at`.
- `reckon-observation` replaces the plain-keyed `extensions` map on
  observed-data. It holds the fields a normalizer keeps for its event class,
  such as logon type, status, case metadata and privileges. One definition
  covers all of them. Its schema lists every field, and each field is
  optional.
- `reckon-attestation` replaces `x_reckon_attestation` on the note.
- Native objects carry custom properties too. Each native type with custom
  properties gets one property-extension:
  - `reckon-account` on `user-account`: `domain`, `email`,
    `principal_name`, `provider`, `user_type`, `credential_uid`,
    `cloud_account_uid`, `uid_alt`, `source_ids` and `weak`.
  - `reckon-person` on an `identity` that is a person: `principal_name`,
    `mail`, `directory`, `directory_id`, `employee_id`, `department`,
    `title`, `organization`, `display_name` and `enabled`.
  - `reckon-file` on `file`: `path`.
  - `reckon-process` on `process`: `name`.
  - `reckon-email-message` on an `email-message`: `thread_uid`, the thread
    id adr/0359 keeps on a message, and `envelope_to_refs`, the envelope
    recipients (`smtp_to` and Delivered-To). STIX stores the header
    recipients in `to_refs`, `cc_refs` and `bcc_refs`, but has no property
    for the envelope ones. adr/0359 mints a message with no Message-ID from
    its recipients, the envelope ones included. Storing them keeps that id
    minted from stored values.
  - A recipient that is still not an address after cleanup is kept as text
    in `unparsed_recipients`, and the id counts it. Two copies of one run
    that differ only there stay two messages, as adr/0359 requires.
    - This extension first held the weak message known only by a source's
      own id. adr/0359 removed that message, because the id names a thread,
      not a message. The extension is not yet published, so it changes.
  - `reckon-observation` also holds `thread_uid` for a click or an
    attachment event, which names its email only by a thread id
    (adr/0359).
  - `reckon-assertion` on a relationship a person asserted: who asserted it
    and when, the note, the history, and the withdrawal.
- These lists come from running every bundled fixture through the
  normalizers. #356 changes some fields, so each schema is written from the
  fields left after #356.

**Extension definitions are the same in every install.**

- Each definition's id is a fixed UUIDv4 written in the code. Nothing about a
  definition depends on the tenant. So a consumer recognizes a reckon type
  from any install.
- One fixed `identity` stands for the organization that publishes reckon.
  Its `name` is `Hypredge Inc.` and its `identity_class` is `organization`.
  Every definition names it as `created_by_ref`.
- The company is named only in that identity. STIX lets an identity be
  updated under the same id. So if the company changes, only that object
  changes, and no type name or id does.
- Every name that lasts is the product's: the type names, the
  property-extension names and the schema file paths all start with
  `reckon-` or sit under a `reckon` path.
- Each definition's JSON Schema is a file in the repository. The binary
  embeds it. The definition's `schema` property holds the file's URL in the
  public repository.
- A schema file's path includes its version. A published version's file is
  never edited, so its URL always shows the same schema.
- One package holds the definitions, their schemas and the serializer.

**Five shapes the normalizers write today are fixed.** A run of every
bundled fixture through the normalizers, validated against the OASIS schemas,
found the first four. The run was repeated after adr/0356 merged, and all four
were still there. The fifth was found by reading the email normalizer.

- `x-cloud-resource` keeps the provider's resource type in a property named
  `type`. Once the properties are at the top level, that collides with the
  STIX `type`. The property becomes `resource_type`. The cloud inventory
  observation's field of the same name becomes `resource_type` too.
- `network-traffic` writes `protocols` as one string. STIX requires a list of
  lowercase names, such as `["tcp"]`.
- The user inventory normalizer puts the person's `identity` into
  observed-data's `object_refs`. That list may hold only SCOs and
  relationships. It holds the account and the `belongs-to` relationship
  instead.
  - adr/0324 made the directory record the one observation that names a
    person. The record keeps that link in `reckon-observation`'s
    `person_ref`, which names the person the record describes.
  - Readers that ask which records are about a person read `person_ref` as
    well as `object_refs`. This holds whether the record names an account or
    not. A directory with no accounts, such as an HR system, is then still
    found by its person.
- A person `identity` is an SDO, but it is written through the SCO path, so
  it has no `created` or `modified`. It gets both.
- An `email-message` has no `is_multipart`, which STIX requires. OCSF has no
  field for it. A source can say it only in the email's `raw_header`. So
  `is_multipart` is `true` when `raw_header` has a `Content-Type` of
  `multipart/…`, and `false` otherwise.

**Four inputs the validation test found are handled.** The test found them in
the normalizers' unit tests. No bundled fixture has them.

- A hash that does not fit its algorithm is dropped. An example is a SHA-256
  that is not 64 hex characters. It is dropped from the stored `file` and from
  the properties its id is minted from. A broken hash cannot say which file
  this is. This changes `file` ids, so rule 6 applies, and this ADR records it.
- A `network-traffic` with no protocol name gets its `protocols` from the
  telemetry. The normalizer uses OCSF's `protocol_name` when present. Else it
  maps `protocol_num` to a name: 6 is `tcp`, 17 is `udp` and 1 is `icmp`. Else
  it uses `ipv4` or `ipv6`, from the addresses.
- An event with no `time` is a broken record. OCSF requires `time`, and STIX
  needs it for `first_observed`. Normalizing returns an error before any
  normalizer runs, so the rule holds for every class. The raw event is kept
  with a note, as for any normalize error.
- An adapter sends the engine only valid OCSF, so each event's `time` comes
  from the vendor record. An adapter never uses the time it fetched the
  record, because that is not when the event happened. A record with no time
  is not sent. The adapter reports it the way it reports any record it cannot
  convert.
- A detection with no pattern, no `finding_info.uid` and no title is broken in
  the same way. OCSF requires the uid at every supported version, and the
  title up to 1.3.0. It gets the same error.

`reckon-provenance` keeps `tool` required. Every ingest comes through a
binding, and the binding names its tool.

**Every gap between OCSF and STIX has a rule.** The four inputs above were
found one test input at a time. So every constraint STIX puts on each type
the normalizers write was then compared with the OCSF fields it comes from,
at every supported OCSF version. Where valid OCSF can still give invalid
STIX, the normalizer follows a rule. No rule depends on the vendor, and no
rule invents a value.

- A value is cleaned up when the result is the same value.
  - An IP address has surrounding white space trimmed.
  - An email address has surrounding white space trimmed. A mailbox with a
    display name, such as `Alice <alice@corp.example>`, keeps its address,
    `alice@corp.example`.
    *Amended (adr/0371): every address is then written back in its shortest
    RFC 5322 form. The local part is quoted only when it can't be a
    dot-atom or would start or end with white space. So
    `"alice"@corp.example` is `alice@corp.example`, and a local part holding
    quotes keeps them.*
  - An IPv4 address written as IPv4-mapped IPv6 (`::ffff:10.0.0.5`) is
    stored as the IPv4 address. It is then the same object as that address
    seen anywhere else.
  - A registry key whose root is a known alias of a hive, such as
    `Computer\` or `\REGISTRY\MACHINE`, gets the hive's full name.
  - A file with a path, no name and no hash that fits takes its name from
    the last element of the path.
- A value STIX cannot hold is dropped. The raw event still holds it.
  - A URL that is not a URI gets no `url`.
  - An email address that is not an address is dropped.
  - A hash whose algorithm name is shorter than 3 or longer than 250
    characters is dropped.
  - A registry key with no root, such as a relative path, gets no
    `windows-registry-key`.
  - A domain name that breaks RFC 1034 for any reason other than an
    underscore gets no `domain-name`.
  - A person with no name gets no `identity`. The account is still written.
- A record whose event `time` is outside the years 0000 to 9999 is broken,
  in the same way as a record with no time. Any other time outside that
  range, such as a resource's creation time, is a value STIX cannot hold. It
  is dropped, and the object is kept.
- A domain name with an underscore label, such as `_ldap._tcp.corp.local`,
  is kept. Such names are real and common in DNS. `03 §7` lists this as a
  deviation.
- `reckon-observation`'s `configuration` holds any JSON value, because OCSF
  types it as any JSON. The extension is not yet published, so its 1.0.0
  schema changes.
- Two id rules depart from STIX, and `03 §7` now lists them. Neither was
  listed before.
  - A `file` is identified by its content: the strongest hash alone,
    SHA-256 first. STIX also uses the name and parent directory, and picks
    MD5 first. reckon keeps its rule, so a file copied under a new name is
    still one file. reckon's ids are already minted under a per-tenant
    namespace, so matching other systems' ids is not lost.
  - A `network-traffic` is identified without STIX's `start` and `end`, so
    one connection seen in many events is one object.
- A `network-traffic` id is minted from the stored `protocols` and ports,
  not from the raw `protocol_name`. A port the record does not give is left
  out of the id, so it no longer shares an id with port 0. Before, two connections that differed only in their
  protocol number had one id but different content. `TCP` and `tcp` had two
  ids for the same protocols. This changes ids, so rule 6 applies.
- A person `identity` has a UUIDv5 id, like the other SDOs the normalizers
  write. `03 §7` now names it.
- A value that is unknown is left out, never written as zero or false. The
  serializer already leaves out an empty value. The comparison found
  normalizers that wrote a zero for a missing value, such as a process id or
  a port.

**A raw event a reasoning object cites is kept apart from its STIX refs.**
adr/0348 lets a prediction cite the raw OCSF events a timeline row stands
for, and the agent is told to cite them. A raw event id is a bare UUID, not a
STIX id. STIX reserves the `_ref` and `_refs` suffixes for STIX ids, so the
stored prediction was invalid.

- A citation list on a stored reasoning object holds STIX ids only. A raw
  event id the same citation names is kept in a sibling list whose name ends
  in `_event_ids`, such as `test_result_event_ids`. Nothing cited is lost.
- The command keeps the citation as the agent gave it. The split happens when
  the object is stored, so the event log is unchanged and a replay gives the
  same object.
- A ref that is neither a STIX id nor an event id is refused.
- Readers of a citation read both lists.
- Whether a raw event should count as evidence at all is a separate decision.
  It belongs with adr/0348, the UI spec's rule on evidence refs, and the
  prompt rule on citing.
- Every change to a stored reasoning object goes through the serializer, not
  only its first write. So a change cannot store what the serializer would
  refuse.

**A record with nothing to extract is observed as an `artifact`.**

- Some events give a normalizer no entity to extract. A case record (class
  2005) holds a case number, title, status and link, but no host, user or
  address. An event no normalizer handles goes to the fallback, which
  extracts nothing.
- STIX requires observed-data to list at least one object. Today these events
  produce observed-data with an empty list.
- For such an event, the normalizer also writes a STIX `artifact` that stands
  for the stored OCSF record:
  - `url` is `reckon://ocsf-record/sha256/<hash>`, in the form reckon
    already uses for `reckon://investigation/<id>`. It names the record by
    its content, as the id does. One record fetched twice is one artifact,
    so a url naming one event would name whichever event came first. The
    events behind an observation are found through the observation, as
    before.
  - `hashes` holds the SHA-256 of the stored payload. This is the content
    hash the observed-data id already uses.
  - `mime_type` is `application/json`.
  - The id is minted from the hash, by STIX's rule for `artifact`.
- The observed-data lists that artifact in `object_refs`. Its other fields
  stay in `reckon-observation`, as for any event.
- So every piece of evidence is still an observed-data, and no reader of
  citations meets a new kind.
- STIX expects an artifact's `url` to open the content. This one opens only
  inside the reckon install. Someone outside gets the hash, not the record.
  That matters only at export, which is later work.
- An event that does yield entities is unchanged. It gets no artifact.

**Imported detections use native fields as STIX defines them.**

- A sighting's `where_sighted_refs` holds the vendor identity. STIX means
  the field as who saw the indicator, and the vendor did.
- ATT&CK technique ids go into the indicator's `external_references`, as
  `{source_name: "mitre-attack", external_id: "<id>"}`. `06 §3.2` reads the
  techniques from there.
- `indicator_types` is set only when the source gives a STIX indicator type.
- An indicator always has a `pattern` and a `pattern_type`. When the source
  gives no pattern, the pattern is the source's rule reference. That is the
  finding uid, or its title when there is no uid. The `pattern_type` is then
  the source tool's name. `pattern-type-ov` is an open vocabulary, so it
  accepts that value.
- The indicator's id is minted from values the indicator stores: the vendor
  (`created_by_ref`), the `pattern`, the `pattern_type`, and the finding uid
  when the source gives one. The uid is stored as an `external_references`
  entry under the vendor's name, which is where STIX puts a source's own id
  for an object. Before, the id came from the uid and the source's own
  pattern, and neither was always stored. So every finding with neither
  shared one id, whatever its title. Two vendors, or two findings, with the
  same pattern now stay apart, as they did before. This changes indicator
  ids, so rule 6 applies.
- A sighting's id also takes its `observed_data_refs`. Before, two findings
  for one indicator in the same second shared one sighting, even on
  different evidence. This changes sighting ids, so rule 6 applies.
- Confidence stays as adr/0356 made it.

**A new section of `01` states how reckon conforms to STIX and how its types
change.** It replaces the adoption of the `x-` prefix rule. It holds nine
rules.

1. **The version.** reckon writes STIX 2.1, read with Errata 01 (CSD01). This
   section is the only place that states the version. Changing it needs an
   ADR.
2. **Native first.** A native STIX object or property is used when one fits.
   If none fits, a published OASIS extension is used. Only when neither fits
   does reckon define its own extension. A native property is never given a
   different meaning (§7.3).
3. **Declared and versioned.** Every reckon type and property set has an
   extension definition. Each definition has a semver `version` and a schema.
4. **Names start with `reckon-`.** Every custom type and property-extension
   is named after the product, never the company. The prefix stops a later
   STIX version from using a reckon name for something else. A type name is
   part of every id, so renaming a type later needs an ADR.
5. **Additive and breaking changes.**
   - An additive change, such as a new optional property, raises the minor
     version. The definition id stays the same.
   - A breaking change gets a new definition with a new id. Renaming a
     property, removing one, making one required or changing its meaning are
     all breaking.
   - Objects written under the old definition keep it. Readers accept both
     until the old objects are rewritten. `03 §4.16` reads both sides of an
     OCSF deprecation in the same way.
6. **Identity properties are fixed per definition.** Changing the properties
   that make up a type's id changes ids. So it needs an ADR.
7. **Deviations are listed in one place.** That place is `03 §7`. Each
   deviation states its reason. Adding one needs an ADR.
8. **Stored objects are validated.** A test checks every object the
   normalizers and projections write. It checks them against the OASIS STIX
   2.1 schemas and reckon's extension schemas.
   - The OASIS schemas are copied into the repository, with their licence.
   - The test validates with `google/jsonschema-go`.
   - `google/jsonschema-go` does not check `format`. The test checks the
     `date-time`, `uri`, `email` and `idn-hostname` formats itself.
   - Some OASIS patterns disagree with the spec text. Examples are the
     `ipv6-addr` pattern, which refuses valid addresses, and the
     `windows-registry-key` pattern, which anchors only one hive. The copied
     schemas stay as upstream has them. The test lists each correction and
     the spec section it follows.
   - The OASIS schemas do not cover every rule in the spec text. The test
     checks the missing rules by hand. Property names and extension keys
     are checked against the spec's character rules. Two more are:
     - `sighting.json` lets `sighting_of_ref` name an SCO, but the spec
       requires an SDO.
     - The schemas accept any unknown top-level property. The test rejects a
       top-level property the type does not define, so a custom property
       outside an extension fails.
9. **Draft extensions are adopted by definition id.** A later revision of a
   draft arrives under a new id. So a definition's meaning never changes
   silently.

**The specs are corrected to match what is stored.**

- `01`:
  - A sighting's confidence is an integer from 0 to 100.
  - `reckon-supports` and `reckon-refutes` start from an SDO: observed-data,
    `reckon-null-observation`, note or indicator. They never start from a
    sighting. A sighting is cited through its observed-data.
  - The new conformance section described above.
- `02 §2.2` describes one `stix_objects` table and one `stix_edges` table,
  with payloads in STIX form. Evidence rows are written once. The event log
  holds a reasoning object's history.
- `03 §4.12` calls the indicator an SDO and the sighting an SRO.
- `03 §7` lists every deviation:
  - `process`, `email-addr`, `user-account` and `email-message`.
  - The per-tenant SCO namespace.
  - UUIDv5 ids for SDOs and SROs.
  - adr/0341 added `windows-registry-key`.
- `01 §4` and `CLAUDE.md` point to `03 §7` instead of giving a count.
- `CLAUDE.md` says custom types are declared by extension definitions and
  named with the `reckon-` prefix.

**Readers use the STIX shape.**

- Go readers that expect the nested shape change to read top-level
  properties and the property-extensions: `server/evidence.go`,
  `server/seed_resolve.go`, `server/export_markdown.go` and
  `capability/ocsfview`.
- SQL readers do too. About 40 JSON paths in `evidence/person.go`,
  `evidence/identity.go` and `evidence/assertions.go` read
  `payload->'properties'` or the asserted relationship's top-level fields.
- One package-level constant per extension holds its JSON path, so no query
  spells out an extension-definition id.
- The workbench already reads an SCO with or without `properties`
  (`evidenceLabel.ts`, `identityKinds.ts`, `derivedViews.ts`).
  `evidenceLabel.ts` reads a process's `name` and a file's `path` from their
  property-extensions. The other workbench readers use shapes the server
  builds.

**Stored data is reset, not migrated.** The product is still in
development, so there is no stored data to keep. Existing installs reset
their data (`reckon demo reset`, or a fresh database). The new code writes
only the new shape, and no reader handles the old one.

## Alternatives rejected

- **Keep the `x-` names.** §7.3 allows them. But they look like the
  deprecated mechanism, so a reader could take them for it.
- **Plain names, such as `host`.** §11.2 warns that an unprefixed name "may
  be used in a future version of the specification with a different
  meaning". STIX could well add a `host` type.
- **A company prefix, such as `hypredge-host`.** A company can be renamed,
  acquired or closed, and the type names are part of every id.
- **Keep the deprecated mechanism.** §11 says not to use it for new
  extensions. It also gives a consumer no schema to validate against, and no
  version to tell two shapes apart.
- **Make only exported objects valid STIX, and keep the store as it is.**
  Investigations cite the stored evidence, so the store is what has to be
  right. The store already holds two shapes, and export would add a third.
- **Create extension-definition ids per tenant.** A consumer could not tell
  that two installs' `reckon-host` are the same type.
- **Use a `toplevel-property-extension` for provenance.** §10.5 recommends
  `property-extension` over it.
- **`santhosh-tekuri/jsonschema` as the validator.** It is the most widely
  used Go library, but one person maintains it. The Google library passed the
  same check.
- **`xeipuuv/gojsonschema` or `qri-io/jsonschema`.** Neither supports draft
  2020-12, and neither has been updated since 2024.
- **Fetch the OASIS schemas with a script instead of copying them.** The
  test would then depend on the network or on a manual step. A copy pins the
  exact version the test uses.
- **One property-extension per group of normalizers.** That means more
  definitions to version. One schema with optional fields validates just as
  well.
- **A custom `reckon-record` type for a record with nothing extracted.**
  Evidence would then come in two kinds, and every reader of a citation
  would have to handle both.
- **A case record as a STIX `incident`.** It fits a case's meaning. But STIX
  defines `incident` as a stub, and its OASIS extension is still a draft. The
  case fields would sit in a reckon extension anyway, and the unknown events
  would still need another answer.
- **Leave `where_sighted_refs` empty, as adr/0356 left it.** That is valid
  STIX, but the sighting would then not say who saw the indicator. The vendor
  identity is already stored and is the sighting's `created_by_ref`.
- **Put ATT&CK ids in `kill_chain_phases`.** A kill-chain phase names a
  tactic, such as `lateral-movement`. It does not name a technique.
- **Create no indicator when the source gives no pattern.** A sighting must
  point at an SDO. The indicator is the vendor's claim, and it is what the
  sighting points at.

## Consequences

- Every stored object can be checked against a published schema. The test
  fails when a writer stops producing valid STIX.
- A new custom type or property needs an extension definition, a schema and
  a test fixture.
- A breaking change to a custom type gets a new definition id.
- The detection-finding normalizer's output changes, so its version goes up.
- The rename changes the prompt version. Both eval scenarios need new
  baselines on every supported model. One run after this change and #356
  covers both.
- The whole change lands after #356.
  - The rename touches the normalizer and identity files #356 edits.
  - #356 also edits the detection-finding normalizer.
  - The `reckon-observation` schema lists the observed-data fields #356
    leaves.
- This change lands after adr/0341, which edited the same files.
- adr/0341's registry type is native, so it needs no extension definition.
- These are out of scope and left for later work:
  - A valid export bundle.
  - The Grouping and Report as stored objects.
  - OCSF event ids inside `_refs` properties.
  - `identity` objects for analysts and the AI delegate, used as
    `created_by_ref`.
  - Ids under the standard STIX namespace at export.
  - Data markings.
