#!/bin/sh
# check-dll-readers.sh — m400 gate (bug class: the installed updater must accept
# the new artifact). Runs the #46.1 legacy reader AND the current reader over
# <dll> and fails unless BOTH return the SAME single build id.
#
# Usage: deploy/addon/check-dll-readers.sh <VL.AddOn.dll>
set -e

DLL="${1:-}"
if [ -z "$DLL" ]; then
  echo "usage: $0 <VL.AddOn.dll>" >&2
  exit 2
fi
if [ ! -f "$DLL" ]; then
  echo "check-dll-readers: $DLL does not exist" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "$0")/../.." && pwd -P)"
cd "$ROOT"

# Absolute path: go test runs the package's test binary with the package dir as
# its working directory, so a relative DLL path would not resolve.
ABS_DLL="$(cd "$(dirname "$DLL")" && pwd -P)/$(basename "$DLL")"

CHECK_DLL="$ABS_DLL" go test ./internal/addonbuild/ -run 'TestCheckDLLReadersAgree' -count=1 -v
