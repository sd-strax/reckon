# 0335 — The Security Lake adapter reads the table's schema, builds each filter from the fields present, and declines what the table cannot express

Issue: #335 · PR: — · Status: accepted

## Context

adr/0322 built the Security Lake adapter from a fixed column list. Against a
live lake's CloudTrail management table (source version 2.0, OCSF 1.1.0) its
health probe failed with a column-not-found error, and every query would
have. The table is AWS's mapping of a subset of OCSF 1.1.0: it has no
top-level `status_id` (the outcome is the `status` string), no
`actor.session.credential_uid` or `uid`, no `resources[].name` or `region`,
and its `cloud` carries no account. adr/0334 set the rule for an adapter over
a store it does not own (`11 §4.4`) and exempted this adapter until it was
brought to it.

## Decision

**The schema is read, not assumed.** The adapter reads the table's schema
from the Glue catalog, through a resource link when the subscriber holds one,
on every health probe, which the engine runs before each call, and keeps it
for the call that follows; a call that finds none held reads it first. Any
failure drops it: a failed read, temporary or not, a missing core field, a
probe the store rejects, or a call's own query failing for any reason. The next call reads again, so no call is built from
a schema the store has disproved, and there is no rule for which failures may
be trusted to pass. Nothing is read at configure, so no read started then can
land after a newer one. A column the Iceberg table no longer carries
(`iceberg.field.current` false) is not counted. The schema is parsed into the fields it carries,
nested ones included (`actor.user.credential_uid`, `resources[].uid`). When
the subscriber cannot read the schema behind its resource link (the lake's
catalog denies or does not find the link's target, or answers 401 or 403 with
no error code the service names), which is common for a query-access
subscriber, or the catalog returns a table with no columns, the adapter uses the
recorded profile for the table's source and version, named by the suffix
of the table the link points to, or of the configured table when it is no link, (`_cloud_trail_mgmt_2_0`), and says so in its
health message; with neither, it is unhealthy.

**Each filter names its candidate fields.** A filter is expressed over the
candidates the table carries, joined with OR: a principal over
`actor.user.uid`, `actor.session.issuer` and `actor.user.name`; a credential
over `actor.user.credential_uid` and `actor.session.credential_uid`; the
outcome over `status_id` where it exists and the `status` string, compared
without case, where it does not; the event's region over the `region` column, or
`cloud.region` where the table has no such column, one or the other, never
both. The projection reads only the columns the
table has. The event time, `class_uid` and `accountid` are the core: a table
without one is unhealthy.

**What the table cannot express is declined.** A filter with no candidate
present is a degraded capability, reported in health for each operation it
affects, and a call that needs it is declined with FALLTHROUGH naming the
absent fields. A condition is never dropped to let the call through: a
limited query without it answers a page that may miss what was asked for, and
its empty answer would read as complete. A resource's type, zone, group or namespace is held to the same rule: where
the table carries the field it is part of the query, so the limit applies to
matching rows, and a call that gives one the table does not carry is
declined, not answered empty. A resource given by its identifier is matched on the identifier or
declined: a name given beside it is not a substitute, since a name can match
another account's resource. A link whose target is in another Region is read
in that Region. Against the 2.0 table, a resource named without its unique
identifier cannot be matched (`resources[].name` is absent), and a resource
cannot be scoped by zone, group or namespace, on either CloudTrail
operation; everything else answers.

**The live table is the first profile.** `cloudtrail-mgmt-2.0` records the
table's schema and a result page as captured in the run that found the
defect, with account identifiers and principals replaced. The offline tests
read it, so the queries are checked against what a lake returns, and the
live-check runbook says how to capture the next one. The exemption in
adr/0334 ends.

## Alternatives rejected

- **A column map per source version, without discovery.** An Iceberg table
  may gain columns within a version, and a mapping change would break a
  fixed map again.
- **Dropping an unexpressible condition and filtering after the fetch.** A
  limited query returns the newest matches of the looser filter, which may
  exclude every match of the real one; the empty answer is then false.
- **An unreadable schema as unhealthy.** It would disable the adapter for the
  subscriber it was built for; the probe still checks every column the
  queries read.
- **Athena's `information_schema` as the schema source.** Not verified for a
  query-access subscriber; the catalog read is what the adapter already
  makes.

## Consequences

- Health against the 2.0 table is healthy with two degraded capabilities,
  resource by name and resource scope by zone, group or namespace, on
  `cloudtrail.api_activity` and `cloudtrail.resource_history`;
  `list_capabilities` shows them as limitations.
- An investigation that knows a resource only by name reaches it through its
  identifier: AWS Config answers `get_cloud_resource_context` by name and
  returns the identifier.
- A new source version is a new profile: captured by the runbook, embedded,
  and named in `describe`. The queries do not change unless the version
  removes a core field.
- The recorded page holds the columns the capture selected, which did not
  include `time` or `accountid`; those two are tested on constructed pages
  until the next live capture, which the runbook has select every projected
  column.
