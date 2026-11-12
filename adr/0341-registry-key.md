# 0341 — A registry key is STIX's `windows-registry-key`, one object per host, key and value; the key carries its hive expanded; identity is host-scoped over the folded key and the value name, and leaves the value's data out; `x-registry-key` is no longer minted

Issue: #341 · PR: — · Status: proposed

## Context

The registry-key activity normalizer (`design/03-capability-layer.md §4.6`)
minted a custom `x-registry-key` SCO with `hive`, `key_path`, `value_name`
and `value_data`. The spec gave the reason as "Windows registry isn't covered
by STIX 2.1 native types". STIX 2.1 does cover it. `windows-registry-key`
(§6.17) has `key`, "the full registry key including the hive", whose hive the
JSON schema refuses in abbreviated form (`HKLM`, `HKCU`, `HKCR`, `HKU`,
`HKCC`). It also has `values`, a list of `windows-registry-value-type`
(`name`, empty for the key's default value; `data`; `data_type`, one of the
fourteen `REG_*` names), plus `modified_time`, `creator_user_ref` and
`number_of_subkeys`. STIX derives the id from `key` and `values` under the
STIX namespace. The STIX JSON schema and the reference library's
`_id_contributing_properties` were both read for this record.

The invented type had three consequences:

- The workbench's citation table listed `windows-registry-key` as an entity
  type, so it covered a type nothing minted and gave the minted one no label.
- Identity hashed the hive as the source wrote it. The in-tree fixture writes
  `HKEY_LOCAL_MACHINE`, while the tests and most sources write `HKLM`, so one
  key minted two ids.
- An event naming only a key and an event naming the key's default value both
  hashed an empty `value_name`, so the two minted one id.

OCSF (1.3.0, Windows extension) defines `reg_key.path` as "the full path to
the registry key", with no hive attribute. `reg_value` carries `name`, `data`
(JSON), `type` and `type_id` (the `REG_*` names), and `path`, the key the
value lives under. The in-tree fixture and tests split the key into
`reg_key.hive` and `reg_key.path`.

The constraints that bind the answer:

- STIX is the vocabulary, adopted rather than invented
  (`design/01-domain-model.md`, Adopted vs invented). A deviation from STIX's
  identity rule is documented in `03 §7.2`, as the one for `process` is.
- Identity is a one-way door once data is recorded against it (`03 §7.1`).
- The normalizer shapes inputs and the resolver only folds (`03 §7.4`).

Microsoft documents registry key names as not case sensitive ("Structure of
the Registry"); the pages read for this record say nothing about the case of
value names.

## Decision

**The type is `windows-registry-key`.** `identity.TypeWindowsRegistryKey`
replaces `identity.TypeRegistryKey`, and no code path mints `x-registry-key`.
The type leaves the list of custom SCOs in `01` and `03 §7.1`, and the
`CLAUDE.md` list of `x-` objects.

**One object per host, key and value, as before.** An event that names a value
mints one `windows-registry-key` whose `values` holds that one value as
`{name, data, data_type}`. An event that names only a key mints one with no
`values`. The persistence artifact an analyst pivots on is the value: the
`Run` key's `SyncHost` value, not the `Run` key. One object per key with the
values accumulated would merge every autorun under `Run` into one entity.

**The key carries its hive expanded.** One identity function,
`identity.RegistryKeyPath(hive, path)`, builds the key string. The normalizer
and the event view (`capability/ocsfview`) both call it.

- When the source splits the key into `reg_key.hive` and `reg_key.path`, it
  joins them with a backslash. When it does not, it reads `reg_key.path` as
  OCSF defines it (the full path), else `reg_value.path`.
- The leading root is rewritten to the full hive name in upper case:
  - `HKLM`, `\REGISTRY\MACHINE` and `HKEY_LOCAL_MACHINE` in any case become
    `HKEY_LOCAL_MACHINE`;
  - `HKU`, `\REGISTRY\USER` and `HKEY_USERS` become `HKEY_USERS`;
  - `HKCU`, `HKCR` and `HKCC` become `HKEY_CURRENT_USER`,
    `HKEY_CLASSES_ROOT` and `HKEY_CURRENT_CONFIG`.
- The rest of the path keeps the source's case, trimmed of surrounding
  whitespace and of a leading or trailing backslash.
- A path whose root is none of these is kept as written, since dropping the
  evidence would be worse than a key STIX would call malformed.
- No key, no object: an event with neither a key nor a value path mints no
  registry key, as before.

**The value is what the source wrote.**

- `name` is `reg_value.name`, with an empty name for the default value.
- `data` is `reg_value.data` as a string; OCSF types it JSON, so a
  non-string value is rendered as its JSON text.
- `data_type` is `reg_value.type` when it is one of STIX's `REG_*` names,
  else the name of `reg_value.type_id`. It is left out when neither is
  present or the value is not a STIX `REG_*` name.
- `modified_time`, `creator_user_ref` and `number_of_subkeys` are not mapped.

**Identity is `(host_ref.id, key, value_name)`.**

- `key` is the expanded key, folded to lower case, because key names are case
  insensitive.
- `value_name` keeps its case, as before; no source read for this record
  documents otherwise.
- `value_name` is absent from the canonical JSON when the event names no
  value, and is the empty string for the default value. A key and its default
  value are two entities.

Three deviations from STIX's `key` + `values` rule are documented in
`03 §7.2`:

- The id is host-scoped, for the reason the custom type was: a registry key
  is a host's state, and one path on two hosts is two entities.
- The value's data and type are out of identity. A value whose data changes is
  the same value, and the change is what the observation records.
- The key is case-folded.

The host stays where `process` keeps it, in the identity tuple and in the
observation's `object_refs`, not as a property of the object.

**The normalizer moves to version 3, with no backfill**, as adr/0324 did for
accounts. Objects already recorded keep their `x-registry-key` ids, and the
investigation event log still cites them. The workbench's inspector keeps
`x-registry-key` in the set of entity types so such a citation still opens as
an entity. That is the one place the old type remains, and it only reads the
type; nothing mints it.

**The event view and the labels read the native shape.**

- The registry sentence in `capability/ocsfview` binds the
  `windows-registry-key` whose folded key and value name match the event's.
- The workbench's evidence label and the markdown export's `stixLabel` label
  the object as its key followed by the value name in parentheses, with
  `(default)` for the default value. They have no branch for it today, so
  the chip read only "registry".
- The citation table's existing `windows-registry-key` entries ("registry",
  popover) now apply to what is minted.

## Alternatives rejected

- **Keep `x-registry-key` and fix the hive and default-value defects.** STIX
  adopts the type, and inventing one it already defines is what `01`'s
  Adopted vs invented rule forbids.
- **One object per key, with every value seen accumulated in `values`
  (STIX's shape taken literally).** Each event sees one value, so the stored
  list would be whatever the last writer saw, and the per-value pivot would be
  lost.
- **STIX's identity rule unchanged (`key` + `values`, no host).** One path on
  every host becomes one entity, and every change to a value's data a new one.
- **Carry the host as a custom property (`x_host_ref`).** `process` already
  keeps its host in identity and in `object_refs`, and no SCO here carries a
  host property; a second way to say the same thing is not needed.
- **Rewrite recorded `x-registry-key` objects to the new ids.** The event log
  is immutable and cites them. The accounts' re-identification (adr/0324)
  set the precedent: new version, no backfill.

## Consequences

- A registry key from `HKLM` and from `HKEY_LOCAL_MACHINE` is one entity, and
  a key is distinct from its default value.
- A source that writes OCSF's full `reg_key.path` with no hive now yields a
  STIX key, where it yielded an empty hive before.
- The lateral-movement scenario's persistence answer reads
  `windows-registry-key` with a `key` and a `values` list. The data the model
  reads changes; no prompt text, tool definition or engine-text template does,
  so `PromptVersion` is unchanged. The scenario is re-run against both
  models before merge.
- Records made before version 3 keep `x-registry-key` objects that are not
  joined to the new ones.
- OCSF puts registry key activity at class 201001 in its Windows extension.
  The normalizer registers class 1003, as `03 §2.5` and the fixtures do. That
  is unchanged here.
