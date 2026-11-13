# 0341 — A registry key is STIX's native `windows-registry-key`, and `x-registry-key` is removed

Issue: #341 · PR: — · Status: proposed

## Context

The registry-key activity normalizer (`design/03-capability-layer.md §4.6`)
minted a custom `x-registry-key` SCO with `hive`, `key_path`, `value_name`
and `value_data`. The spec said this was because "Windows registry isn't
covered by STIX 2.1 native types". That is wrong: STIX 2.1 defines
`windows-registry-key` (§6.17).

What the STIX type looks like:

- `key` is "the full registry key including the hive". The JSON schema
  rejects the abbreviated hives `HKLM`, `HKCU`, `HKCR`, `HKU` and `HKCC`.
- `values` is a list of `windows-registry-value-type`. Each has `name`
  (empty for the key's default value), `data` and `data_type` (one of
  fourteen `REG_*` names).
- It also has `modified_time`, `creator_user_ref` and `number_of_subkeys`.
- STIX derives the id from `key` and `values`, under the STIX namespace.

These facts come from the STIX JSON schema and the reference library's
`_id_contributing_properties`.

The custom type caused three problems:

- The workbench's citation table lists `windows-registry-key` as an entity
  type. Nothing mints that type, and the type that is minted gets no label.
- Identity hashed the hive as the source wrote it. The in-tree fixture
  writes `HKEY_LOCAL_MACHINE` and the tests and most sources write `HKLM`,
  so one key minted two ids.
- An event naming only a key and an event naming the key's default value
  both hashed an empty `value_name`. So the two minted one id.

What OCSF says (1.3.0, Windows extension):

- `reg_key.path` is "the full path to the registry key". There is no hive
  attribute.
- `reg_value` carries `name`, `data` (JSON), `type` and `type_id` (the
  `REG_*` names), and `path`, the key the value lives under.
- The in-tree fixture and tests split the key into `reg_key.hive` and
  `reg_key.path`.

The rules this decision has to follow:

- STIX is the vocabulary we adopt, not invent (`design/01-domain-model.md`,
  Adopted vs invented). Any deviation from STIX's identity rule is
  documented in `03 §7.2`, the way the `process` one is.
- Identity cannot change cheaply once data is recorded against it
  (`03 §7.1`).
- The normalizer shapes inputs, and the resolver only folds (`03 §7.4`).

Registry key names and value names are not case sensitive. Microsoft
documents this for key names in "Structure of the Registry", and the
registry treats value names the same way. The custom type folded the key
but kept the value name's case. So `Updater` and `updater` under one key
minted two ids.

## Decision

**The type is `windows-registry-key`.**

- `identity.TypeWindowsRegistryKey` replaces `identity.TypeRegistryKey`.
- `x-registry-key` is removed from the code, the workbench and the specs.
  Nothing mints or reads it.
- The type leaves the list of custom SCOs in `01` and `03 §7.1`, and the
  `CLAUDE.md` list of `x-` objects.

**One object per host, key and value, as before.**

- An event that names a value mints one `windows-registry-key`. Its
  `values` holds that one value as `{name, data, data_type}`.
- An event that names only a key mints one with no `values`.
- The analyst pivots on the value, such as the `Run` key's `SyncHost`
  value, not on the `Run` key. One object per key with all its values would
  merge every autorun under `Run` into one entity.

**The key carries its hive expanded.** One identity function,
`identity.RegistryKeyPath(hive, path)`, builds the key string. The
normalizer and the event view (`capability/ocsfview`) both call it.

- When the source splits the key into `reg_key.hive` and `reg_key.path`,
  the function joins them with a backslash.
- Otherwise it reads `reg_key.path` as OCSF defines it (the full path),
  else `reg_value.path`.
- The leading root becomes the full hive name in upper case:
  - `HKLM`, `\REGISTRY\MACHINE` and `HKEY_LOCAL_MACHINE`, in any case,
    become `HKEY_LOCAL_MACHINE`.
  - `HKU`, `\REGISTRY\USER` and `HKEY_USERS` become `HKEY_USERS`.
  - `HKCU`, `HKCR` and `HKCC` become `HKEY_CURRENT_USER`,
    `HKEY_CLASSES_ROOT` and `HKEY_CURRENT_CONFIG`.
- The rest of the path keeps the source's case. Surrounding whitespace and
  a leading or trailing backslash are trimmed.
- A path whose root is none of these is kept as written. Dropping the
  evidence would be worse than keeping a key STIX would call malformed.
- An event with neither a key nor a value path mints no registry key, as
  before.

**The value is what the source wrote.**

- `name` is `reg_value.name`. It is empty for the default value.
- `data` is `reg_value.data` as a string. OCSF types it as JSON, so a
  non-string value is written as its JSON text.
- `data_type` is `reg_value.type` when that is one of STIX's `REG_*`
  names, else the name of `reg_value.type_id`. It is left out when neither
  is present or the value is not a STIX `REG_*` name.
- `modified_time`, `creator_user_ref` and `number_of_subkeys` are not
  mapped.

**Identity is `(host_ref.id, key, value_name)`.**

- `key` is the expanded key in lower case, because key names are case
  insensitive.
- `value_name` is in lower case too, because value names are also case
  insensitive.
- `value_name` is left out of the canonical JSON when the event names no
  value. It is the empty string for the default value. So a key and its
  default value are two entities.

This deviates from STIX's `key` + `values` rule in three ways, all
documented in `03 §7.2`:

- The id is host-scoped, as the custom type's was. A registry key is part
  of a host's state, so one path on two hosts is two entities.
- The value's data and type are not part of identity. A value whose data
  changes is still the same value, and the observation records the change.
- The key and the value name are lower-cased.

The host is kept the way `process` keeps it: in the identity tuple and in
the observation's `object_refs`. It is not a property of the object.

**The normalizer moves to version 3, and nothing reads the old type.** No
recorded data has to stay readable. So there is no backfill and no reader
for the old type, and the workbench's inspector drops `x-registry-key`
from its entity types.

**The event view and the labels use the native shape.**

- The registry sentence in `capability/ocsfview` links to the
  `windows-registry-key` whose lower-cased key and value name match the
  event's.
- The workbench's evidence label and the markdown export's `stixLabel`
  show the key, then the value name in parentheses. The default value
  shows as `(default)`. Neither has a branch for registry keys today, so
  the chip shows only "registry".
- The citation table's existing `windows-registry-key` entries
  ("registry", popover) now apply to what is minted.

## Alternatives rejected

- **Keep `x-registry-key` and fix the hive and default-value defects.**
  STIX already defines the type. Inventing a type STIX has is what `01`'s
  Adopted vs invented rule forbids.
- **One object per key, with every value seen collected in `values`
  (STIX's shape taken literally).** Each event sees only one value. So the
  stored list would be whatever the last writer saw, and the per-value
  pivot would be lost.
- **Use STIX's identity rule unchanged (`key` + `values`, no host).** One
  path on every host would become one entity. Every change to a value's
  data would become a new entity.
- **Store the host as a custom property (`x_host_ref`).** `process`
  already keeps its host in identity and in `object_refs`. No SCO here has
  a host property, and a second way to record the same fact is not needed.
- **Keep `x-registry-key` readable for objects already recorded.** No such
  objects have to survive the change. A reader for a type nothing mints
  would be a second shape every consumer has to handle.

## Consequences

- A registry key from `HKLM` and the same key from `HKEY_LOCAL_MACHINE` are
  one entity.
- A key and its default value are two entities.
- A source that writes OCSF's full `reg_key.path` with no hive now yields a
  STIX key. Before, it yielded an empty hive.
- The lateral-movement scenario's persistence answer now shows a
  `windows-registry-key` with a `key` and a `values` list. This changes the
  data the model reads, but no prompt text, tool definition or engine-text
  template. So `PromptVersion` is unchanged. The scenario is re-run on both
  models before merge.
- OCSF puts registry key activity at class 201001 in its Windows extension.
  The normalizer registers class 1003, as `03 §2.5` and the fixtures do.
  This record does not change that.
