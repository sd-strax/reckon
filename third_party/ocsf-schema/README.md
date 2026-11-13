# OCSF schema

These files are copies of the Open Cybersecurity Schema Framework (OCSF)
schema. There is one copy for each OCSF release that reckon supports, from
1.1.0 to 1.9.0.

reckon's tests read them. The tests check that every class number and field
that reckon reads from an OCSF event is one that OCSF defines. See
`adr/0356-ocsf-conformance.md` for the reasons.

## What is here

- `<version>/schema.json` is the full schema for that release. It has every
  class and every object, not only the ones reckon uses.
- `LICENSE` and `NOTICE` are OCSF's own files.

The Go package that reads these files is `internal/ocsfschema`.

## Where the files come from

OCSF runs a schema server at https://schema.ocsf.io. Each `schema.json` is
that server's export for one release. The export URL is:

```
https://schema.ocsf.io/<version>/export/schema?extensions=win,linux,macos&profiles=<profiles>
```

- `extensions=win,linux,macos` adds the Windows, Linux and macOS extensions to
  the core schema.
- `<profiles>` is every profile that the release has, except two. The list of
  a release's profiles comes from `https://schema.ocsf.io/api/<version>/profiles`.
  The names are sorted and joined with commas.

Two profiles are left out: `linux/linux_users` and `macos/macos_users`. Both
define the field `process.euid`. From 1.8.0 on, the server cannot export a
schema that has both, so its default export fails. reckon reads nothing from
either profile. They are left out of every release, including the older ones
where the export would work, so that all nine files are made the same way.

The profiles in each file are:

| Release | Profiles |
|---|---|
| 1.1.0 | `cloud`, `container`, `datetime`, `host`, `load_balancer`, `network_proxy`, `security_control` |
| 1.2.0 | the 1.1.0 list and `data_classification` |
| 1.3.0 | the 1.2.0 list and `osint` |
| 1.4.0, 1.5.0, 1.6.0, 1.7.0 | the 1.3.0 list, `incident` and `trace` |
| 1.8.0 | the 1.7.0 list and `ai_operation` |
| 1.9.0 | the 1.8.0 list and `record_integrity` |

## The files are not modified

Each `schema.json` is the server's response, saved byte for byte. It is not
reformatted, so each file is one long line. Nothing is added, removed or
reordered. Do not edit these files by hand.

`LICENSE` and `NOTICE` are exact copies of the files of the same name in
https://github.com/ocsf/ocsf-schema at the tag `v1.9.0`.

## Licence

The OCSF schema is licensed under the Apache License, Version 2.0. The full
text is in `LICENSE`. OCSF's attribution notice is in `NOTICE`.

## Adding a release

Run the script from the repository root with the release number:

```
scripts/fetch-ocsf-schema 1.10.0
```

The script needs `curl` and `jq`. It applies the rule above, checks that the
export declares the version asked for, and writes
`third_party/ocsf-schema/<version>/schema.json`.

Then update the download date in this file if it differs, add the release to
the table above, and run the tests. `adr/0356-ocsf-conformance.md` says what
to do when the newest supported release moves.
