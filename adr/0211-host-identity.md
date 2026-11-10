# 0211 — A host is one entity across sources: its hostname is its identity, every other identifier is kept beside it, and joins or splits the record cannot derive are explicit edges an analyst sees and makes

Issue: #211 · PR: #325 · Status: accepted

## Context

A host's STIX id is a deterministic UUIDv5 over its identity-contributing
fields (`design/01` §3, `design/03` §7.2), so the same machine seen through
two tools stitches into one `x-host` without a lookup. The rule read the first
available tier of an asset id, then domain and hostname, then MAC, then
hostname alone; the normalizer filled the asset-id tier from OCSF
`device.uid`.

Real sources disagree on every one of those fields for the same machine. An
endpoint agent, a host-based IDS and a fleet manager each put their own agent
or client id in `device.uid`; OCSF defines that field as the device's own
identifier and places the sensor id in `agent_list[].uid`, but the agent's id
is the only one an agent has. One source reports the NetBIOS domain, another
the DNS suffix, a third none; one reports the short name, another the FQDN.
The seed path minted on the typed name alone. Under the tiered rule one
machine therefore took a different id per source, per spelling and at the
seed, and an investigation read one host as several: the events filter, the
pivot, the entity inspector's role line, the `entity:` tags on past cases
and the targets of actions all key on the id. The fixture scenario already
showed it: the seed subject's id never equalled the host the events minted.

The domain model's answer to identity that the key cannot settle is the
`aliases` edge, "an identity assertion, non-destructive, never a merge"
(`design/01` edge table, `design/02` §2.2). Nothing produced or read one.
`design/03` §7.3 covered only the opposite failure, two machines under one
id, with a note the resolver would emit. The capability layer may not reason
(`design/03` §4), the record must stay honest about what was observed and
what was asserted, and the analyst, not the engine, decides what the engine
cannot derive.

## Decision

**The hostname is the host's identity.** `x-host` identity is the short
hostname: trimmed, lowercased, a trailing dot removed, cut at the first dot.
`WIN-FILE01`, `win-file01` and `win-file01.corp.example` are one host. The
rest of an FQDN is the host's DNS domain, a property. `device.domain`,
`device.uid`, `device.uid_alt`, `agent_list[].uid` and the MAC are
properties, never identity, and the normalizer keeps each one it sees on the
`x-host` it mints, the agent and device ids keyed by the source that gave
them. The folding is the resolver's (`canonicalHostname`), so the seed path
and every normalizer agree by construction; shaping the fields out of the
OCSF device object is the normalizer's (§7.4). The accepted cost is stated:
two machines with the same short name in two domains share an id, and the
record shows it (below) instead of guessing.

**A record with no hostname mints a weak host.** When a device object carries
no hostname, the host is minted on the MAC, or failing that on the pair
(source tool, `device.uid`), and carries `weak: true`. A weak identifier
never joins anything on its own: two sources' agent ids are two weak hosts.

**A placeholder is not an identity.** Sources emit a value when they have
none: an all-zero or all-one MAC, an unspecified, loopback or broadcast
address, `localhost` or a dash for a hostname, the nil GUID for a device id,
the hash of the empty file. Each names nothing, and minting on it makes one
entity of every record that carries it: every machine reporting a zero MAC
would alias to one weak host and read as one machine. The resolver holds a
built-in, per-type list of such values, and a value that does not parse as
its type counts the same, and treats a field holding one as absent: a host
falls to its next tier, an address mints nothing, a hash is passed over for
the next one or for the file's name and path, and a file with neither mints
nothing; the seen-as view does not count one as a domain, an address or an
agent id. The list is short, names only values that cannot be an identifier,
and is not configuration: a per-install list would mint different ids for one
record on two installs. Adding to it is a shaping change under §4.13.

**An alias from one record is a fact; the normalizer emits it.** When one
record carries a hostname and a MAC or a device id, or no hostname but a MAC
and a device id, the normalizer mints the weak host each lesser identifier
alone would have minted, outside the observation's `object_refs`, and emits
`aliases` from it to the host the record minted, DIRECT, with the record's
provenance. The source said in that record that its agent id
names that hostname; no cross-event inference and no statistics are
involved, so the capability layer is still pure I/O. An event the same source
later attributes to its agent id alone lands on the weak host and reads under
the strong one.

**Reads follow the alias set in one place.** The evidence store resolves an
entity to its alias set. Live `aliases` edges join; a live `distinct-from`
pair is never crossed. A host reachable from both ends of a split, each end
reached without passing through the other, is contested and reads alone: the
record ties it to two machines a person said are different, and the engine
does not pick a side. Every other host reads with the component it reaches
through uncontested hosts, bounded in depth. The answer does not depend on
which end of an alias the question starts from. The events filter, the
window around an event, the pivot and the appearances across investigations
expand the asked-for host to that set. The ids themselves are untouched:
nothing is merged, re-minted or deleted; an alias withdrawn tomorrow, or a
split placed on the link that is wrong, moves the reads with no data to
repair.

**Over-merge is visible, not detected.** The host's identity view lists what
the record has seen under the id: the distinct domains, the agent and device
ids per source, the addresses, computed at read time from the device objects
of the events that reference it. One hostname under two domains reads as
"seen under 2 domains". The resolver emits no note; the analyst reads the
fact and decides.

**The analyst joins and splits by explicit edge, on the entity.** Two hosts
the record did not join are joined by "Same as another host", which writes an
`aliases` edge; two the record wrongly joined, or two the analyst knows
apart, are separated by "Not the same host", which writes a `distinct-from`
edge, a new relationship type added to the domain model's edge table and its
invented list. Both are human acts: the endpoint refuses an AI-delegated
token as approval does, and the agent may say in the thread that two hosts
look like one but never records it. An assertion is a row in `stix_edges`
with a deterministic id over the ordered pair and the fact that a person
asserted it, so one pair has one asserted row per type, beside any edge a
source's record produced and never in its place; its payload carries who
asserted it, when, the note, and a thin history. Withdrawing marks the row
rather than deleting it (`design/02` §2.2, CRUD with history, supersession
over deletion); a source's edge cannot be withdrawn, only cut by a split. A
join over a pair a person has split is refused until the split is withdrawn:
one act at a time, nothing implicit. The entity inspector shows every alias
with its origin, a source's record or a person's assertion, and offers the
withdraw on the asserted ones.

**Normalizers that mint hosts move to version 2.** The shaping change is a
re-normalization under §4.13; observations minted at version 1 keep their
ids and their old hosts. There is no backfill: records minted under the old
rule read as they were, and the re-normalization pass is the place for a
re-id if one is ever wanted.

**Conformance is a test the adapters run.** Three device objects naming one
machine as three tools would (client id with short name and FQDN; agent id
with agent name; FQDN as the hostname with a hardware uuid) normalize to one
strong host with the weak hosts aliased to it, and an agent-id-only record
joins it once an inventory record names both. The test ships with the engine
and an adapter for a live source is held to it over its own captures.

## Alternatives rejected

- **Keep the tiers and stitch afterwards with inferred aliases.** A step that
  joins two hosts because they share a hostname and do not contradict is
  reasoning in the capability layer, every host read becomes a closure query
  on which correctness depends, and the inference is wrong exactly where the
  tiers were: same name, two machines.
- **Keep `device.uid` as the asset-id tier when a CMDB supplies it.** OCSF has
  no field that says "this is the asset register's id", so the normalizer
  cannot tell a register's id from an agent's; a per-adapter declaration
  would move identity into configuration, where two installs of one tool
  would differ.
- **Domain and hostname together as the strong key.** The sources do not
  agree on the domain's form, NetBIOS against DNS suffix against none, so the
  key that is meant to stitch across tools splits across them; the domain is
  kept as a property and the over-merge it would have prevented is shown
  instead.
- **Detect a likely false merge and emit a note (the §7.3 text).** A note
  from the resolver is a judgement the capability layer is not allowed to
  make, and it would fire on every multi-domain estate; the facts the note
  would cite are listed on the entity, and the analyst's split is the act.
- **A merge.** Rewriting one host's events onto another's id is destructive,
  unrecoverable when wrong, and forbidden by `design/01` §3.
- **Leaving the human assertion for a later issue.** The issue asks for one
  host "or two, explicitly aliased"; the screen where the analyst says so is
  the surface the mechanism exists for, and the over-merge view has no action
  to offer without the split.

## Consequences

- A host seen through several sources, and the host an analyst seeds, read
  as one entity wherever the record is read by entity; a source that names
  the machine by its agent id alone joins once any of its records names both.
- Two machines with one short name in two domains read as one host until an
  analyst splits them; the inspector says what the record has seen so the
  analyst can.
- A chain of aliases between two hosts a person split reads as single hosts
  until the person cuts the link that is wrong; the inspector lists every
  alias so the link can be found. An identifier that moves from one machine
  to another in sequence, an agent id following a re-imaged laptop, is a
  question of when each association was observed, which the alias edges'
  provenance already holds; reading an identifier by the association live at
  the event's time is a follow-up, not a split.
- `distinct-from` enters the domain model beside `aliases`; `stix_edges`
  holds human assertions with their history; three analyst-only endpoints and
  an identity view on the evidence read are added; the entity inspector gains
  "Also seen as", the seen-under line, and the join, split and withdraw acts.
- Host-minting normalizers are at version 2; observations from version 1 are
  unchanged and their hosts are not re-identified. The same bump carries the
  placeholder rule for addresses and hashes: an address or file entity minted
  on a placeholder under version 1 keeps its id and gains no new events.
- Account, group and hash-less file identity have the same shape of problem
  and are not changed here; the same two layers apply when they are.
