#!/usr/bin/env bash
# W-ONE-BUTTON M4 — refuse to ship a secret.
#
# Runs over a STAGED TREE (and, in the workflow, again over the unpacked
# archive — the two are different things and a check that only sees one of them
# is a check that can be bypassed by whatever happens in between).
#
# This is a DENY-LIST of shapes that must never leave the machine, plus
# gitleaks when it is available. The deny-list is not a substitute for gitleaks;
# it is the part that still works when gitleaks is not installed, and it covers
# the files this repo has actually leaked or nearly leaked before: .env is
# WRITTEN at runtime by api/handler_onboarding.go, data/ holds the live SQLite
# database, and ~/nofx-backups/ holds copies of both.
#
# Exit 0 = clean. Non-zero = REFUSED, with every offending path named on stdout.
set -uo pipefail

ROOT="${1:-}"
if [ -z "$ROOT" ] || [ ! -d "$ROOT" ]; then
  echo "secret-scan: usage: secret-scan.sh <staged-tree-dir>" >&2
  exit 2
fi

# Shapes that are never part of a release. Each is a path GLOB matched against
# the path relative to the root, so a nested data/data.db is caught too.
DENY=(
  '.env' '.env.*' '*.env'
  '*.db' '*.db-wal' '*.db-shm'
  '*.key' '*.pem' '*.p12' '*.pfx'
  'id_rsa' 'id_dsa' 'id_ecdsa' 'id_ed25519' 'id_*'
  '*.bak'
  'backups/*' '*/backups/*'
  'data/*' '*/data/*'
  '.git/*' '*/.git/*'
  '*.tfstate' 'credentials' '*.netrc' '.npmrc'
)

found=0
while IFS= read -r -d '' abs; do
  rel="${abs#"$ROOT"/}"
  base="$(basename "$rel")"
  for pat in "${DENY[@]}"; do
    # shellcheck disable=SC2053
    if [[ "$rel" == $pat || "$base" == $pat ]]; then
      echo "secret-scan: REFUSED — denied path in the release tree: $rel (matched '$pat')"
      found=1
      break
    fi
  done
done < <(find "$ROOT" -type f -print0)

# A second, content-shaped pass: a private key header or an obvious live token
# inside an otherwise allowed file.
while IFS= read -r -d '' abs; do
  rel="${abs#"$ROOT"/}"
  if LC_ALL=C grep -qlE 'BEGIN (RSA |EC |OPENSSH |PGP )?PRIVATE KEY|sk-[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16}' "$abs" 2>/dev/null; then
    echo "secret-scan: REFUSED — secret-shaped content in: $rel"
    found=1
  fi
done < <(find "$ROOT" -type f -size -2M -print0)

if command -v gitleaks >/dev/null 2>&1; then
  if ! gitleaks detect --no-git --source "$ROOT" --redact --exit-code 1 >/dev/null 2>&1; then
    echo "secret-scan: REFUSED — gitleaks found a secret in the staged tree (output redacted by design)"
    found=1
  fi
elif [ "${GITLEAKS_REQUIRED:-0}" = "1" ]; then
  # In CI an absent scanner is a FAILURE, never a silent downgrade to the
  # deny-list. The release job sets GITLEAKS_REQUIRED=1 precisely so that a
  # failed install cannot quietly weaken the scan that guards a signing key.
  echo "secret-scan: REFUSED — gitleaks is REQUIRED here and is not installed"
  found=1
else
  # NOT a pass. A local run without gitleaks is explicitly a weaker check and
  # says so rather than implying a clean bill.
  echo "secret-scan: NOTE — gitleaks not installed; deny-list and content pass only"
fi

if [ "$found" -ne 0 ]; then
  echo "secret-scan: the release is REFUSED. Nothing was packaged."
  exit 1
fi
echo "secret-scan: clean ($(find "$ROOT" -type f | wc -l) files)"
