#!/usr/bin/env bash
# ROUND 24 — the S4 pass re-evaluated with the extended episode writer
# (price/lo/hi/label/polarity/delta/contract). Inputs read-only; outputs to out-r24/.
set -u
HERE="$(cd "$(dirname "$0")" && pwd -P)"
BIN="${R24_HARNESS:-$HERE/../../../../../.r24harness}"
DB=/home/hoang/vl-r101/data/db.copy.db
OUT="$HERE/out-r24"
mkdir -p "$OUT"
rm -f "${OUT:?}/harness.done"
echo "start $(date -Is) bin=$BIN sha256=$(sha256sum "$BIN" | cut -c1-16) db=$DB"
"$BIN" -db "$DB" -out "$OUT" -s4
rc=$?
echo "exit rc=$rc $(date -Is)"
if [ $rc -eq 0 ]; then
  sha256sum "$OUT/episodes.jsonl" > "$OUT/episodes.jsonl.sha256"
  echo "rc=0 $(date -Is)" > "$OUT/harness.done"
fi
exit $rc
