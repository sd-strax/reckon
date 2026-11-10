# 0208 — A sign-in's reported place rides the OCSF event as a Geo Location attributed to its provider, read by the event view and extracted by nothing

Issue: #208 · PR: #323 · Status: accepted

## Context

The Okta adapter reshapes a System Log sign-in into an OCSF authentication
event (`cmd/reckon-adapter-okta/normalize.go`; design/11 §0, OCSF shaping is
the adapter's) and mapped only `client.ipAddress` into `src_endpoint.ip`.
Okta's `client.geographicalContext` (city, state and country as full names,
postal code, a lat/lon pair) was dropped, so a real-tenant sign-in arrived
with an address and no place, and an identity-rooted investigation could not
ask where a sign-in came from.

The fixture for the identity-rooted scenario (`fixtures/impossible-travel-signin`)
already carries the place on every sign-in, at `src_endpoint.location` with
`country` and `city`, which is the OCSF-native path. The real adapter was
behind the fixture.

Three readers see an authentication event, and they differ. The model reads
the raw OCSF payload in the tool result; the eval's raw-data turn asks for
locations by name. The engine's authentication normalizer (`03 §4.2`) extracts
the user, the host and the source address into STIX objects and keeps logon
type and status as extension fields; it never reads a location. The event
view (`capability/ocsfview`, adr/0090) renders the sentence and fields the
inspector, the events read and the export show, and is never shown to the
model.

## Decision

- **The adapter maps `geographicalContext` onto an OCSF Geo Location at
  `src_endpoint.location`.** `city`; `region` from Okta's `state`; the
  country name Okta sends, verbatim, as `desc`; `postal_code` as a string
  (Okta's documented examples send both a number and a string); `lat` and
  `long` from `geolocation.lat` and `lon`, not the `coordinates` pair OCSF
  deprecated in 1.2.0 in their favour. The object is omitted when the
  context names no place at all: a bare coordinate pair is not a place the
  view can say. The raw Okta event stays under `okta` as before.
  - **The country rides as the name Okta sends, in `desc`; no code is
    guessed.** OCSF's `country` is the ISO 3166-1 alpha-2 code and Okta
    sends a full name ("Canada", "Türkiye"). Translating one into the other
    is a mapping reckon would have to own and keep current across every
    provider's spelling, for a two-letter code nothing in reckon reads: the
    analyst reads the place as text in the inspector and the model reads
    the raw event. So the name is carried verbatim as `desc`, "the
    description of the geographical location", and `country` is not
    written. A fixture or another adapter that holds a code writes
    `country`; the view reads whichever is there.
  - Okta's `state` is a name ("Ontario"), and OCSF names `region` "the
    alphanumeric code that identifies the principal subdivision". Okta
    carries no code, and OCSF has no other subdivision field, so the name is
    carried as given rather than dropped or guessed into a code.
  - A postal code Okta sends as a JSON number has lost any leading zero
    before the adapter sees it; it is carried as the digits that arrived.
    The string form, which Okta's current sample uses, is carried intact.
- **Attribution is OCSF's own field.** `location.provider` is "the provider
  of the geographical location data"; the adapter sets it to `okta`. The
  place is Okta's geolocation of the client address, not where the person
  was, and the event says so without an invented field.
- **The normalizer is unchanged.** STIX has a `location` object, but it is a
  threat-intelligence SDO; minting one per sign-in would put a telemetry
  attribute into the interpretation layer. The place stays on the immutable
  OCSF event, where the model already reads it. `03 §4.2` lists
  `src_endpoint.location` as an optional input that is carried, not
  extracted.
- **The event view says the place, and who located it, after the source it
  names.** The authentication sentence reads "… from 203.0.113.5 in
  Toronto, CA (located by okta)" (the region standing in for a missing
  city, the country alone when that is all there is; the parenthesis only
  when the event names a provider), only when the sentence names a source
  address: without one the place would read as the host's. The attribution
  is in the sentence, not only in a field, because the markdown export and
  the Story timeline read the sentence alone, and a report that said
  "Toronto" without saying it was a geolocation provider's reading of an
  address would state as fact what the record holds as a provider's claim.
  The sentence names a source by its address today, as before; a source
  given by hostname alone is not in the sentence and so neither is its
  place. A Location field carries city, region and country, then the
  location's `desc` as the free text OCSF defines it to be, whoever wrote
  it (for the adapter above, the country name Okta sent; for another
  producer, whatever it described), then "reported by <provider>"
  when the event names one, whether or not the sentence has the place. The
  view reads OCSF, not one adapter's convention: `desc` is never shown as
  the country. The place is not a group-by key (adr/0095): two sign-ins
  from one address fold together whatever the provider said of it.

## Alternatives rejected

- **An `x-location` or STIX `location` per sign-in.** A telemetry attribute
  would become an interpretation-layer object with an identity of its own,
  for no reader that needs one.
- **The deprecated `coordinates` pair.** OCSF deprecated it in 1.2.0 for
  `lat` and `long`; emitting it would be writing to a field the schema is
  retiring.
- **Carrying Okta's country name in `country`.** OCSF defines the field as
  the alpha-2 code; a name there is a value no consumer of the schema can
  rely on, and the fixture's events would carry codes while real ones
  carried names.
- **Translating the name into the code.** Tried in review, through the
  CLDR English region names `golang.org/x/text` carries plus a list of
  ISO's own short names and the renames those tables predate. It worked
  for most names and kept needing one more entry ("Türkiye", "Eswatini",
  "Hong Kong", "Saint Vincent and the Grenadines"), each a round of review,
  for a field no reader in reckon consults. A mapping that has to track
  every provider's spelling is a product of its own, not a line in an
  adapter.
- **Attribution in the Location field alone.** The inspector would say who
  located the address; the export and the timeline, which carry only the
  sentence, would not, and the report is where a reader cannot open the
  inspector.
- **Dropping Okta's `state`** because it is a name, not a code. The field is
  the only subdivision OCSF offers, and a name an analyst can read beats an
  absent value.
- **A custom attribution field** ("okta_geo", "not ground truth"). OCSF's
  `provider` already says whose reading the place is.
- **Adapter only, no view change.** A place that reaches the event but not
  the inspector reaches the model and not the analyst.

## Consequences

- A real Okta sign-in carries the place the fixture always had, and the
  identity-rooted scenario's questions can be asked of live data.
- No prompt text, tool definition or engine text changes: `PromptVersion`
  holds. No migration, no API shape change. The wire samples the workbench's
  decoders are tested against are unchanged: none of them carries a located
  sign-in.
- Other adapters that learn a place from their source map it to the same
  object and set their own `provider`, a country name as `desc` and a code
  as `country`; the view needs nothing more.
