# STIX 2.1 JSON schemas

These files are copies of the OASIS STIX 2.1 JSON schemas.

reckon's tests read them. The tests check that every STIX object reckon
writes is a valid STIX 2.1 object. See `adr/0357-stix-conformance.md` for the
reasons.

## What is here

- `schemas/` is the `schemas/` directory of the upstream repository: the
  common definitions, the domain objects (`sdos/`), the relationship objects
  (`sros/`) and the cyber-observable objects (`observables/`).
- `LICENSE` is the upstream licence file.

The Go package that reads these files is `internal/stixschema`.

## Where the files come from

The source is https://github.com/oasis-open/cti-stix2-json-schemas at the
commit `9af1db41b7b86c06324f899649ae83480134f66e`.

Each schema's `$id` is a `raw.githubusercontent.com` URL under
`oasis-open/cti-stix2-json-schemas/stix2.1/schemas/`. The files refer to each
other by those URLs. The tests map them onto this directory, so nothing is
fetched from the network.

## The files are not modified

The files are copied byte for byte. Nothing is added, removed or reformatted.
Do not edit them by hand.

## Licence

The schemas are licensed under the BSD 3-Clause licence. The full text is in
`LICENSE`.

## Updating

1. Clone the upstream repository and check out the newer commit.
2. Replace `schemas/` and `LICENSE` here with the upstream copies.
3. Change the commit above.
4. Run the tests. A failure names the object, the schema and what is wrong.
