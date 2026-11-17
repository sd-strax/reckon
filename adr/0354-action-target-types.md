# 0354 — An action type declares the entity types it acts on, and the engine refuses a target of any other type

Issue: #354 · PR: pending · Status: proposed

## Context

Each action type acts on one kind of thing. `host.isolate` acts on a host,
`account.disable` on an account, `ioc.block` on an indicator. The catalog
says so only in the name of an input (`host`, `account`, `message`,
`indicator`) typed `"entity"` (`action/descriptor.go`). Nothing reads it.

A target is a `TargetSpec` (`04 §3.1`): an `entity_ref`, the STIX id of the
thing acted on, and a `resolved_identifier`, the string the tool receives.
The request check (`action.BuildRequestCommand`) requires a non-empty
`resolved_identifier` and never reads `entity_ref`. So "isolate host
`svc_backup`" with a user account's id, or with no id at all, is recorded
and goes to approval like a valid request. The approver is the only check,
and nothing tells them the target is the wrong kind.

Every request passes through `BuildRequestCommand`: the agent's action
tools, an analyst's request from the workbench, a re-request and a
reversal. Today they build targets differently. The agent sends both
fields. The workbench sends the name only, and its Reverse sends the
original action's names without their ids.

The ticket and notify actions target a destination: a project, a ticket, a
channel or an address. A destination is not an entity in the evidence, and
these requests carry no meaningful `entity_ref`.

## Decision

- **An action type declares the entity types it acts on.** The descriptor
  gets `TargetTypes`, a list of STIX object types (`target_types` in JSON).
  It is part of the frozen v0 catalog:
  - `host.isolate`, `host.unisolate`: `reckon-host`.
  - `account.disable`, `account.enable`: `user-account`.
  - `email.quarantine`, `email.release`, `email.purge`: `email-message`.
  - `ioc.block`, `ioc.unblock`: `ipv4-addr`, `ipv6-addr`, `domain-name`,
    `url`, `file`. A file stands for its hash.
  - The ticket and notify actions declare none. Their target is a
    destination.

  An IP address is not a host. A host is isolated by the host the tool
  knows, and one address can belong to different machines over time.
- **The engine refuses a target of any other type.** For a type that
  declares target types, every target must carry an `entity_ref`, and the
  STIX type of that id must be one of the declared types. Otherwise the
  request is refused with a 400 that names the declared types. Nothing is
  recorded. The check sits in `BuildRequestCommand`, so it holds for every
  requester. For a type that declares none, `entity_ref` is not read, as
  today.
- **A reversal acts on the original action's targets.** A request with
  `reversal_of_ref` takes its targets from the original action, as recorded.
  Targets in the request body are not used. The reversal is checked
  against the target types like any other request.
- **An undo type requested on its own is an ordinary action.**
  `account.enable` with no `reversal_of_ref` is checked like any other
  request. The engine does not tie it to an earlier `account.disable`.
- **`list_action_types` carries the target types** as part of each
  descriptor, so a surface can offer only the targets an action takes.

## Alternatives rejected

- **Reading the kind from the input name.** `host` and `account` name the
  kind only by convention. An indicator has five STIX types, and nothing
  maps a name onto them.
- **Inferring the kind from `resolved_identifier`.** A hostname, a login and
  a domain can be the same string.
- **Accepting `ipv4-addr` for host actions.** It acts on whichever machine
  holds the address at dispatch, not the host the evidence names. Tracing an
  address to its hosts belongs to target resolution, below.
- **Exempting a reversal from the check.** Its targets are the original's,
  which passed the check when the original was requested. The exemption
  would matter only for an action recorded before the check existed.
- **Refusing a reversal whose body targets differ from the original's.**
  The original's targets are the only correct answer, so the body's add
  nothing to check.
- **Refusing an undo type without `reversal_of_ref`.** An account disabled
  outside reckon could not be re-enabled from it.

## Consequences

- The workbench's request from a pin sends no `entity_ref`, so its requests
  for entity actions are refused until the dialog sends one. That is a
  separate change to the dialog.
- The agent's tools are unchanged. A wrong-type target comes back as the
  tool's error, which the model corrects, as it does for an undeclared
  parameter.
- A re-request copies the stored targets, so a re-request of an action with
  a wrong-type or missing `entity_ref` is refused. A new request is needed.
- The check is on the type only. It does not check that the entity is in
  this investigation, or that `resolved_identifier` names that entity.
  Resolving a target from a weak reference, such as an address to its hosts,
  with the analyst picking among several, is a larger mechanism and is not
  decided here.
- `design/04 §3.1` and `§8.1`, and the descriptor in `design/08 §3`, gain
  the target types. The worked example in `04` that isolates a host by its
  IP address now targets the host.
