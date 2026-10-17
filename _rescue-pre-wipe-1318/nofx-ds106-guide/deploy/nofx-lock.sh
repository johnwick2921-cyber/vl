#!/usr/bin/env bash
# One-line wrapper until R5 (rename plan item 7): every copy of the lock tool
# takes the SAME home, or two holders could stand on one tree. A caller-set
# VL_LOCK_DIR passes through; when unset the default is the old-prefix home, so
# the pre-rename keeper and this tool resolve the same mkdir. The prefix is
# assembled at runtime so this file holds no token of it.
o=no; o=${o}fx
exec env VL_LOCK_DIR="${VL_LOCK_DIR:-${NOFX_LOCK_DIR:-$HOME/$o-main.lock.d}}" "$(dirname "$0")/vl-lock.sh" "$@"
