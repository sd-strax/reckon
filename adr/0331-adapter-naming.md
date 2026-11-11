# 0331 — An adapter's name carries its vendor: the product name alone when it is the vendor's name, `vendor-product` otherwise; the Security Lake adapter is `aws-security-lake`

Issue: #331 · PR: pending · Status: accepted

## Context

An adapter's name is a contract: it is the bundled and installed name, the
binary the engine spawns (`reckon-adapter-<name>`), the key a tenant config
binds verbs to, and the prefix of the keychain references setup writes. The
tree named adapters without a stated rule. `okta`, `greynoise` and
`servicenow` are product names that are also the vendor's name;
`crowdstrike-falcon` prefixes the vendor to a product name that does not
say whose it is; `crowdstrike-response` is a second adapter on the same
Falcon API, the write-side containment sibling of the first, named by its
role rather than a product. The adapter of adr/0322 was
named `security-lake`, a product name that does not say whose it is, from a
vendor whose many other products (Config, GuardDuty, Security Hub,
CloudTrail Lake) are each a source an adapter could read; its Config source
tool is already `aws-config`. The name entered main with PR #330 and had
not been released.

## Decision

**An adapter's name carries its vendor.** When the product's name is the
vendor's name, that name is the adapter's (`okta`, `servicenow`,
`greynoise`). Otherwise the name is `vendor-product` (`crowdstrike-falcon`,
`aws-security-lake`). A further adapter on the same product appends its
role (`vendor-product-role`, for example a write-side adapter beside a read
one). Names are lower-case words joined by hyphens. `crowdstrike-response`
predates the rule (under it, it would be `crowdstrike-falcon-response`) and
is not renamed here: it is bundled, named in example configurations and
installed by anyone running the CrowdStrike pair, so its rename is a change
of its own.

**The Security Lake adapter is `aws-security-lake`**: its binary, bundled
and installed name, manifest, example directory, tenant-config instance and
keychain references, its source tool for lake events, and its role-session
name. AWS's own resource names in its configuration defaults
(`amazon_security_lake_…`) are the vendor's and are unchanged. adr/0322
keeps its file and its text as the record of that decision; its header
notes the rename.

**Source tools are not renamed by this rule.** A source tool is part of
every observation id its events mint (`03 §7.1`), and the existing ones
follow their own adapter-specific forms (`okta:<tool>`, `falcon:<tool>`,
`aws-config`); renaming one would change the identity of every observation
already recorded from it. This adapter's lake source tool changes because
nothing has been recorded under it.

## Alternatives rejected

- **Vendor-prefix every adapter:** renames `okta`, `servicenow` and
  `greynoise` to say their vendor twice.
- **Prefix only when the vendor ships, or may ship, another product the
  engine reads:** depends on a guess about the future and would make `okta`
  and `servicenow` non-conformant the moment either vendor's second product
  is named; whether the name says whose product it is can be decided from
  the name alone.
- **Keep `security-lake`:** a name that does not say whose product it is,
  beside a source tool already named `aws-config`; the rename is free only
  before the first release.
- **Rename ADR 0322's file:** an ADR is a record; a note in its header
  carries the change without breaking links to it.

## Consequences

- The rule is stated once, here; the next adapter's name follows it.
- Observations from the lake carry `aws-security-lake` as their source tool
  from the first live run.
- An install made from main between PR #330 and this change is not
  migrated: remove its `security-lake` instance from the tenant config, its
  install directory and its two `reckon/security-lake-*` keychain entries
  (or, where setup fell back to the environment, its `SECURITY_LAKE_*`
  variables, which the new name reads as `AWS_SECURITY_LAKE_*`), and run
  `reckon adapter setup aws-security-lake`.
