#!/usr/bin/env bash
# Rebuild the demo world from scratch: stop → reset → start → seed → restart.
#
# Needs `reckon start --detach` (adr/0034): each step returns, so the whole
# lifecycle runs unattended. Refuses, via `reckon demo reset`, on an install
# that is not in demo mode — pass --force through if you really mean it.
#
#   scripts/demo-reseed.sh                 # uses `reckon` on PATH
#   RECKON=bin/reckon scripts/demo-reseed.sh
#   scripts/demo-reseed.sh --force         # reset a non-demo install
set -euo pipefail

RECKON="${RECKON:-reckon}"
TIMEOUT="${RECKON_START_TIMEOUT:-3m}"

step() { printf '\n==> %s\n' "$*"; }

step "stop (if running)"
if "$RECKON" status >/dev/null 2>&1; then
  "$RECKON" stop
else
  echo "not running"
fi

step "demo reset"
"$RECKON" demo reset "$@"

step "start"
"$RECKON" start --detach --timeout "$TIMEOUT"

step "demo seed"
"$RECKON" demo seed

step "restart (activates the seeded capabilities)"
"$RECKON" stop
"$RECKON" start --detach --timeout "$TIMEOUT"

step "status"
"$RECKON" status
echo
echo "Demo world rebuilt. Reload the VS Code window; the Cases view shows the seeded queue."
