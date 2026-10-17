#!/usr/bin/env bash
# One-line wrapper until R5 (rename plan item 7): execs the vl twin — same
# content, same flags, same exit codes.
exec "$(dirname "$0")/vl-claim.sh" "$@"
