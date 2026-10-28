#!/usr/bin/env bash
# Stop the embedded Postgres instances that test runs left behind.
#
# Each Postgres-backed package's TestMain starts its own instance in a temp
# directory named <package>-test-pg-<n> and stops it at the end. A test binary
# that panics (or is killed) skips that teardown, and the instance keeps
# running. This stops only those: a postmaster whose data directory is
# <tmp>/<package>-test-pg-<n>/data, never the stack's own Postgres. Each is
# shut down with its own pg_ctl (fast mode), then its temp directory removed,
# which is what the teardown would have done.
#
#   scripts/stop-test-pg.sh            # stop and clean up every leftover
#   scripts/stop-test-pg.sh --dry-run  # list them only

set -euo pipefail

dry_run=false
if [ "${1:-}" = "--dry-run" ]; then
    dry_run=true
fi

found=0
while read -r pid bin flag data rest; do
    [ "$flag" = "-D" ] || continue
    case "$bin" in */bin/postgres) ;; *) continue ;; esac
    root=$(dirname "$data")
    [ "$(basename "$data")" = "data" ] || continue
    [[ "$(basename "$root")" =~ ^[a-z]+-test-pg-[0-9]+$ ]] || continue
    found=$((found + 1))
    echo "test postgres pid $pid in $root"
    if $dry_run; then
        continue
    fi
    "$(dirname "$bin")/pg_ctl" -D "$data" stop -m fast >/dev/null
    rm -rf "$root"
    echo "  stopped, temp directory removed"
done < <(ps -axo pid=,command=)

if [ "$found" -eq 0 ]; then
    echo "no leftover test postgres"
fi
