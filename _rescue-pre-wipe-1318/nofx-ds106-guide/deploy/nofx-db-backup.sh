#!/usr/bin/env bash
# One-line wrapper until R5 (rename plan item 7): execs the vl twin — systemd
# runs this wrapper directly while the old timer is enabled, so it must stay
# executable (mode 100755, pinned by a Go test).
exec "$(dirname "$0")/vl-db-backup.sh" "$@"
