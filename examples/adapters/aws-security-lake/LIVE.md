# aws-security-lake — live check

The runbook for checking the adapter against a real Amazon Security Lake
(`design/11 §4.4`, adr/0335). Continuous integration cannot reach a lake, so
this is where a change in the CloudTrail table is caught. The adapter's
recorded profiles live under
`cmd/reckon-adapter-aws-security-lake/testdata/profiles/<profile>/`; today the
one profile is `cloudtrail-mgmt-2.0` (the `cloud_trail_mgmt_2_0` table,
OCSF 1.1.0).

## Stand up

Use a sandbox organization or account, never production. Everything created
here is listed again under *Tear down*.

1. **An organization trail.** Security Lake collects CloudTrail management
   events only when the organization has at least one **multi-Region
   organization trail that logs read and write management events**. Create
   it (from the management account, with CloudTrail trusted access enabled in
   Organizations) before adding the source, with its own short-expiry bucket.
   Without it the lake stays empty and the only signal is
   `aws securitylake list-data-lake-exceptions`, which names the missing
   trail: check it first, not after waiting.
2. **The lake.** Register a delegated administrator for Security Lake, create
   the meta-store manager role it asks for, make the role you run as a Lake
   Formation data-lake administrator, and create the data lake in one Region
   with a short object expiry.
3. **The CloudTrail source.** Add `CLOUD_TRAIL_MGMT` at source version `2.0`.
   If the source was added before the trail existed, remove it and add it
   again: it does not retry by itself.
4. **Generate events.** Make the calls the adapter answers about, in the
   lake's Region: create a bucket, set and suspend versioning, tag it, put and
   read its bucket policy, and make one denied call (a failure row). There is
   no backfill: only calls made after collection starts reach the lake.
   Collection starts a few minutes after the source is (re)added, and a
   delivered object is committed to the Iceberg table about five minutes
   after it lands, so allow ten minutes before the first rows are visible.
   IAM calls are logged in `us-east-1`; a lake in another Region does not
   see them.
5. **A query-access subscriber.** Create a subscriber with query access for
   the CloudTrail source, accept its Lake Formation resource share in the
   subscriber account, and grant the querying principal `SELECT` and
   `DESCRIBE` on the shared table through Lake Formation (programmatic
   enablement does not grant it). The subscriber sees the table as a resource
   link whose target it may not be able to describe; the adapter then uses
   the recorded profile and says so in health. The run that recorded
   `cloudtrail-mgmt-2.0` queried as a Lake Formation administrator in the
   lake account instead; check the Security Lake subscriber documentation
   for the current steps before relying on these.
6. **An Athena workgroup** with an output location in a short-expiry results
   bucket the querying principal may write.
7. **Configure the adapter** (`tenant-config.yaml` in this directory) with
   the Region, workgroup, output location and temporary credentials
   (`session_token`), then run `reckon adapter test` against it. Health is
   healthy; against the 2.0 table it lists `resource by name` and
   `resource scope by zone, group or namespace` as degraded on both
   CloudTrail operations.

## Capture

Work in a `data/` directory outside the clone (or one git ignores); raw
captures carry account identifiers, principals and addresses and are never
committed.

1. **The schema.**
   `aws glue get-table --database-name <db> --name <table> > data/glue-get-table-<table>.json`
   (from the lake account, or through the resource link's target where the
   subscriber may read it).
2. **A few rows**, selecting every column of the adapter's projection
   (`projection` in `query.go`) the table carries, each as the adapter reads
   it:
   `SELECT "time", time_dt, activity_id, activity_name, status, status_id, CAST(actor AS JSON) AS actor, CAST(api AS JSON) AS api, CAST(cloud AS JSON) AS cloud, CAST(resources AS JSON) AS resources, CAST(src_endpoint AS JSON) AS src_endpoint, CAST(http_request AS JSON) AS http_request, CAST(metadata AS JSON) AS metadata, CAST(unmapped AS JSON) AS unmapped, severity_id, class_uid, category_uid, type_uid, accountid AS lake_accountid FROM <db>.<table> WHERE time_dt > <recent> LIMIT 5`
   (drop a column the table lacks, such as `status_id` on the 2.0 table) in
   the workgroup, one success and one failure at least; save each row as a
   JSON object of column to value (structs and arrays as the JSON Athena
   rendered, nulls kept) under `data/`. The first recorded page lacks
   `time` and `accountid`; the next capture should carry them.
3. **Scrub a copy** into `testdata/profiles/<profile>/`:
   - `schema.json`: the table's `Name`, `StorageDescriptor.Columns` (name, type
     and each column's `Parameters`, which carry `iceberg.field.current`) and
     `PartitionKeys` only — no catalog id, location or table parameters.
   - `pages/<n>.json`: each row, its shape unchanged. Replace account ids
     with `123456789012`, user names and emails with `analyst@example.com`,
     role and session suffixes and Identity Center ids with same-shape
     placeholders, bucket names with `example-bucket`, IP addresses with
     documentation addresses (`203.0.113.0/24`), access key ids with
     `ASIAIOSFODNN7EXAMPLE`, and event and request ids with placeholders.
   - A new source version is a new profile directory, a `recordedProfiles`
     entry with its table suffix (`_cloud_trail_mgmt_<version>`), and an entry in `describe`'s
     `schema_source.profiles`.

## Compare

- Diff the captured columns and types against
  `testdata/profiles/<profile>/schema.json`. A column or nested field added
  or removed is a profile update; rerun the adapter's tests, which read the
  profile: the degraded set, the statements naming only present fields, and
  the page decoding and normalizing.
- Run the adapter's health and a call of each shape against the live lake
  (principal, credential, operation, outcome, resource by uid, region, and
  resource history) and check each answers or is declined with the reason
  health reported.
- A core field (`time`, `time_dt`, `class_uid`, `accountid`) missing is a
  code change, not a profile update.

## Tear down

In this order (Security Lake blocks deleting its required trail while the
lake exists):

1. The subscriber, its resource share and the Lake Formation grants.
2. The Athena workgroup and its results bucket.
3. The CloudTrail source, then the data lake; its Glue database, the
   meta-store manager Lambda and its SQS queues if they remain; the lake
   bucket (empty it first).
4. The organization trail and its bucket.
5. Any scratch Glue database or table made while waiting for the commit,
   and the `default` database Athena may have created.
6. The test bucket and any IAM user or key made to generate events.
7. The meta-store manager role and the Security Lake service-linked roles;
   Lake Formation administrators reverted.
8. The delegated administrator, deregistered through Organizations, and the
   Organizations service access enabled for Security Lake and CloudTrail.

List the account's buckets, trails, Glue databases, Lambda functions and
roles afterwards to confirm nothing from *Stand up* is left.

## When

Before a release that changes the adapter, and when AWS announces a new
Security Lake source version or a change to the CloudTrail OCSF mapping.
