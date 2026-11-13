# 0352 — A pinned evidence card says what happened in one line; its parts sit behind one disclosure

Issue: #352 · PR: #TBD · Status: accepted

## Context

A pin records the refs of what the analyst or the agent pinned (`inputRefs`), and the
card drew one chip per ref. A pinned event put every object behind it on the card:
- the observed-data container
- the event
- the host
- the account under its SID, and the same account under its login (two account
  objects joined by an alias, `adr/0324`)
- the image file, and the process of the same binary
- the parent process

That came to eight chips for one event, and nothing on the card read as what happened.
The timeline already renders the same event as a sentence with chips inline (`adr/0090`).

## Decision

- **A card leads with what happened.** For each pinned event the workbench holds, the
  card shows the event's sentence, the same one the timeline renders, with entity chips
  inline. The event's time and class come before it. A pin of several events shows one
  sentence each.
- **The parts sit behind one disclosure.** "What this is made of · N" is collapsed by
  default. It holds every pinned ref, as today: the container, the objects, the event.
  Nothing on the record changes and nothing is dropped.
- **An account the record joins by an alias shows once**, in both places. The chip is
  the login (`CONTOSO\svc_backup`), and its other identifier (the SID) reads beside it.
  The inspector still opens on either object.
- **A pin with no event, or with an event the workbench does not hold,** keeps today's
  chips, because there is no sentence to show.

## Alternatives rejected

- **Hide the container and the event chip, and keep the rest flat.** It is still a list of
  parts instead of what happened.
- **Merge the two account objects in the record.** `0324` keeps aliasing explicit and
  never merges destructively. Joining the pair is a presentation choice, and that is where
  this decision makes it.

## Consequences

- A card reads like the timeline row it came from.
- The pinned objects stay one click away.
