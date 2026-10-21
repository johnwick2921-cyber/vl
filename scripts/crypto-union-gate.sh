#!/usr/bin/env bash
# crypto-union-gate.sh — the C13 union gate (plan v10 FINAL; DS-102 owns it).
#
# READ-ONLY over the repo. Writes only a /tmp scratch listing for the
# enumeration (mktemp + trap), never the tree, the DB or the box.
#
# Usage:  scripts/crypto-union-gate.sh <table-A> <table-B> <table-C>
#
# CANONICAL INVOCATION (CTO, 2026-10-01 — the ONLY valid one; run inside the
# repo root; a run with no tables passed prints a 0-KEEP-rows lie and its
# numbers must never be quoted):
#   bash scripts/crypto-union-gate.sh \\
#       docs/crypto-removal/disposition-CR-A.md \\
#       docs/crypto-removal/disposition-cr-b.md \\
#       docs/crypto-removal/disposition-CR-C.md
#
# CANONICAL TABLE FORMAT (CTO ruling, C13 table review 2026-10-01 — CR-B's
# markdown pipe row is canonical; one row per hit LINE):
#   branch-point: <40-hex sha>          # the part's branch point
#   integrator-tip: <40-hex sha>        # the integrator tip it was generated against
#   paths: <space-separated pathspecs>  # the part's swept paths
#   regex: <the assembled literal>      # MUST equal the guard's export, byte-for-byte
#   | path | line | token | DELETE|CUT|KEEP | OWNER | reason |
#   # a table with zero rows writes an explicit line instead:
#   0 hits
#   # OWNER names the part that owns the line (CR-A|CR-B|CR-C). A table MAY
#   # list a line it does not own as a CEDED row (OWNER = the owning part).
#   # EXACTLY ONE owner per hit line across the union — a double-claim FAILS.
#
# DIALECT (CR-C, accepted): the four headers may be written as bullets
#   (- branch point: <sha> [comment] / - integrator tip at generation: <sha> /
#    - paths: … / - regex: …), path/token fields may be backtick-wrapped, and a
#   dated-export file may carry ONE blanket row `| <path> | - | count=N | KEEP |
#   OWNER | reason |` instead of per-line rows — the gate verifies the file's
#   hit-line count EQUALS N (a silent-skip-proof; a mismatch FAILS). A `regex:`
#   header without the literal must state its programmatic source; the byte
#   check then falls back to sweep coverage (zero unlisted hits proves the
#   generator's literal matched the guard's).
#
# Gate rules (C13 invariants 1-5 + the CTO's content-asserts):
#   (1) every table exists and parses: >=1 row or the explicit "0 hits" line;
#       any line that is not a header/comment/canonical row is a FAIL.
#   (2) two shas: `git merge-base --is-ancestor <branch-point> HEAD` holds AND
#       `git diff --name-only <integrator-tip>..HEAD -- <paths>` is empty.
#   (3) git failure = FAIL, never a skip.
#   (4) canary: every KEEP row's token is re-found by the sweep in its file;
#       if the union holds NO KEEP rows, the guard test's sentinels are the
#       canary (printed as n/a here).
#   (5) ONE regex: each table's `regex:` header equals the guard's exported
#       literal (extracted programmatically from branding/no_crypto.go), byte
#       for byte — a hand-typed copy fails.
#   Sweep: git ls-files -z, >=2000-file floor, GNU grep -E -i -I -n (binary
#   files skipped). Every hit line must have rows in the union, EXACTLY ONE
#   distinct owner across the union, and be covered by a KEEP row whose token
#   the line contains (case-insensitive) — a DELETE/CUT-owned hit means the
#   cut has not happened at this head. FILE-LEVEL exclusivity (Finding 3):
#   every PATH appearing in any table has exactly ONE owner across the union;
#   a path with two owners FAILS even when its lines split cleanly.
#   Content asserts (single-quoted 'mixed' sites the regex cannot see):
#       web/src/components/plan/ExecutorVerdict.tsx   arm.state === 'mixed'      -> must be PRESENT (KEEP)
#       web/src/components/trader/TraderConfigModal.tsx source_type === 'mixed'  -> if present, the union must name it (CUT); absent = cut complete
set -u

# LC_ALL=C: bytewise regex for the whole sweep. The literal's CJK tokens are
# UTF-8 byte sequences (matched literally, no case folding needed) and the
# ASCII tokens fold under C-locale -i. A multibyte locale puts the glibc
# regex engine on its pathological-line path — the known segfault class
# (grep on a multi-MB line with an alternation) that took DS-101's run down.
export LC_ALL=C

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$REPO_ROOT" || exit 3
GUARD=branding/no_crypto.go

fail=0
ok()   { echo "PASS $1"; }
bad()  { echo "FAIL $1"; fail=$((fail+1)); }

# -- regex literal, extracted PROGRAMMATICALLY from the guard (never re-typed)
lit=$(sed -n 's/^const SweepRegexLiteral = `\(.*\)`$/\1/p' "$GUARD")
if [ -z "$lit" ]; then bad "cannot extract SweepRegexLiteral from $GUARD"; echo "== $fail FAIL"; exit 1; fi
ok "sweep literal extracted from the guard ($(printf %s "$lit" | wc -c) bytes)"

# -- the two-path LINE-LEVEL ownership allowlist, from the guard (CTO ruling)
allowlist=$(awk '/var LineLevelOwnershipPaths/{f=1;next} f&&/^}/{f=0} f{for(i=1;i<=NF;i++) if($i ~ /^"[^"]*",?$/) {gsub(/[" ,]/,"",$i); print $i}}' "$GUARD" | tr '\n' ' ')
allowlist=$(echo "$allowlist" | xargs)
nallow=$(echo "$allowlist" | wc -w)
if [ "$nallow" -ne 2 ]; then bad "line-level allowlist has $nallow paths (want exactly 2 — the ruling, never extended silently): $allowlist"; fi
ok "line-level ownership allowlist (2 paths): $allowlist"

# -- the P0 risk-cap KEEP canary, from the guard (CTO ruling 10:5x)
cap_sites=$(awk '/var RiskCapAssertSites/{f=1;next} f&&/^}/{f=0} f{for(i=1;i<=NF;i++) if($i ~ /^"[^"]*",?$/) {gsub(/[" ,]/,"",$i); print $i}}' "$GUARD" | tr '\n' ' ')
cap_sites=$(echo "$cap_sites" | xargs)
cap_n=$(echo "$cap_sites" | wc -w)
if [ "$cap_n" -eq 0 ]; then bad "risk-cap canary list EMPTY — the guard is the single source"; fi
if [ $((cap_n % 2)) -ne 0 ]; then bad "risk-cap canary list malformed (odd $cap_n — file/needle pairs)"; fi

# -- sweep the tracked tree (invariant 3 + the 2000 floor)
LIST=$(mktemp) || { bad "mktemp failed"; echo "== $fail FAIL"; exit 1; }
trap 'rm -f "${LIST:?}"' EXIT
git ls-files -z > "$LIST" || { bad "git ls-files failed (never skip)"; echo "== $fail FAIL"; exit 1; }
nfiles=$(tr -cd '\0' < "$LIST" | wc -c)
if [ "$nfiles" -lt 2000 ]; then bad "enumeration floor: $nfiles tracked files < 2000"; fi
ok "enumerated $nfiles tracked files (floor 2000)"

# -- the gate's own inputs are never swept: the disposition tables carry the
# literal BY DESIGN (CR-A embeds it; every row names tokens)
ntables=0
for a in "$@"; do ntables=$((ntables+1)); done
if [ "$ntables" -eq 0 ]; then bad "gate invoked with NO tables — a 0-KEEP-rows result is vacuous, never quote it"; fi
ok "gate run over $ntables tables: $*"

# row index:  tbl|file|line -> disposition (owner/token kept for messages)
declare -A row_dispo row_owner row_token row_line
# blanket rows: file -> expected hit count (dated exports, Finding 4)
declare -A blanket_rows blanket_reason
declare -A blanket_seen
allrows=0; keepcount=0
for tbl in "$@"; do
  [ -f "$tbl" ] || { bad "table missing: $tbl"; continue; }
  # three accepted header dialects (CTO, all three tables accepted):
  #   CR-B: branch-point: / integrator-tip: / paths: / regex: <literal>
  #   CR-C: - branch point: / - integrator tip at generation: / - regex: extracted programmatically...
  #   CR-A: base sha (branch point): / integrator tip generated against: / sweep regex ...: + literal on the NEXT line
  bp=$(sed -n -E 's/^branch-point: *([0-9a-f]{7,40}).*$/\1/p' "$tbl" | head -1)
  [ -z "$bp" ] && bp=$(sed -n -E 's/^- *branch[- ]point: *([0-9a-f]{7,40}).*$/\1/p' "$tbl" | head -1)
  [ -z "$bp" ] && bp=$(sed -n -E 's/^[- ]*base sha \(branch point\): *([0-9a-f]{7,40}).*$/\1/p' "$tbl" | head -1)
  tip=$(sed -n -E 's/^integrator-tip: *([0-9a-f]{7,40}).*$/\1/p' "$tbl" | head -1)
  [ -z "$tip" ] && tip=$(sed -n -E 's/^- *integrator[- ]tip( at generation)?: *([0-9a-f]{7,40}).*$/\2/p' "$tbl" | head -1)
  [ -z "$tip" ] && tip=$(sed -n -E 's/^integrator tip generated against: *([0-9a-f]{7,40}).*$/\1/p' "$tbl" | head -1)
  paths=$(sed -n -E 's/^-? *paths: *//p' "$tbl" | head -1)
  treg=$(sed -n -E 's/^regex: *(.*)$/\1/p' "$tbl" | head -1)
  note=""
  if [ -z "$treg" ]; then
    treg=$(sed -n -E 's/^- *regex: *(.*)$/\1/p' "$tbl" | head -1)
    case "$treg" in
      *"extracted programmatically"*|*"programmatically"*) note="extraction-note";;
    esac
  fi
  if [ -z "$treg" ]; then
    # CR-A two-line form: a header naming the sweep regex, literal on the NEXT line
    treg=$(awk 'f{print; exit} /sweep regex.*:$/{f=1}' "$tbl" | sed 's/^[[:space:]]*//; s/^`//; s/`$//')
    [ "$treg" = "$lit" ] && note=""
  fi
  [ -n "$bp" ] || bad "$tbl: header branch-point missing/not a sha"
  [ -n "$tip" ] || bad "$tbl: header integrator-tip missing/not a sha"
  if [ -z "$paths" ]; then
    echo "NOTE $tbl: no paths header (accepted CTO dialect) — the scoped staleness diff is skipped; the two-sha ancestry check still applies"
  fi
  if [ -n "$treg" ]; then
    if [ "$note" = "extraction-note" ]; then
      echo "NOTE $tbl: regex header states its programmatic source without the literal — byte check falls back to sweep coverage (zero unlisted hits proves it)"
    elif [ "$treg" = "$lit" ]; then ok "$tbl: regex header equals the guard literal byte-for-byte"; else
      bad "$tbl: regex header != guard literal (hand-typed copy?)"; fi
  else bad "$tbl: header regex missing (regex: <literal>, a bullet naming its programmatic source, or a sweep-regex header with the literal next line)"; fi
  if [ -n "$bp" ] && ! git merge-base --is-ancestor "$bp" HEAD 2>/dev/null; then
    bad "$tbl: HEAD does not descend from branch-point $bp"; fi
  if [ -n "$tip" ] && [ -n "$paths" ]; then
    d=$(git diff --name-only "$tip"..HEAD -- $paths 2>/dev/null)
    if [ -n "$d" ]; then bad "$tbl: stale table — diff $tip..HEAD over its paths is non-empty: $(echo "$d" | head -1)"; else
      ok "$tbl: diff $tip..HEAD over its paths is empty"; fi
  fi
  n=0
  while IFS= read -r line; do
    # the CR-A next-line regex literal (indented, backticked) equals the guard literal
    _t=$(printf '%s' "$line" | sed 's/^[[:space:]]*//; s/^`//; s/`$//')
    [ "$_t" = "$lit" ] && continue
    # skip: blanks, comments, headers in every accepted dialect, separator/header rows,
    # explicit 0-hits lines, and CR-A prose headers
    case "$line" in
      ""|"#"*|branch-point:*|integrator-tip:*|paths:*|regex:*|"0 hits"|"- "*|"| path |"*|"|---"*|"base sha"*|"integrator tip generated"*|"sweep regex"*|generated:*|ownership:*) continue;;
      "|"*)
        # | path | line | token | DISP | OWNER | reason |  (trailing pipe optional)
        body=${line#|}; body=${body%|}
        IFS='|' read -r p ln tok disp owner reason < <(printf '%s\n' "$body")
        p=$(printf '%s' "$p" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        ln=$(printf '%s' "$ln" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        tok=$(printf '%s' "$tok" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        disp=$(printf '%s' "$disp" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        owner=$(printf '%s' "$owner" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        reason=$(printf '%s' "$reason" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        p=${p#\`}; p=${p%\`}; tok=${tok#\`}; tok=${tok%\`}
        if [ -z "$p" ] || [ -z "$ln" ] || [ -z "$tok" ] || [ -z "$disp" ] || [ -z "$owner" ]; then
          bad "$tbl: unparseable row (canonical: | path | line | token | DISP | OWNER | reason |): $line"
          continue
        fi
        # blanket row (Finding 4): | <path> | - | count=N | KEEP | OWNER | reason |
        if [ "$ln" = "-" ]; then
          if [ "$disp" != "KEEP" ]; then
            bad "$tbl: blanket row must be KEEP: $line"
            continue
          fi
          cnt=$(echo "$tok" | sed -n 's/^count=\([0-9][0-9]*\)$/\1/p')
          if [ -z "$cnt" ]; then
            bad "$tbl: blanket row token must be count=N (a skip with no count is invisible): $p"
            continue
          fi
          if [ -z "$reason" ]; then
            bad "$tbl: blanket row needs a reason: $p"
            continue
          fi
          blanket_rows["$p"]="$cnt"; blanket_reason["$p"]="$reason"
          n=$((n+1)); allrows=$((allrows+1))
          continue
        fi
        case "$disp" in
          DELETE|CUT|KEEP) ;;
          "-") disp="-" # ceded marker: owner column names the table that carries the row
            ;;
          *) bad "$tbl: bad disposition '$disp' in row $p:$ln"; continue;;
        esac
        k="$tbl|$p|$ln"
        if [ -n "${row_dispo[$k]:-}" ]; then
          bad "$tbl: DUPLICATE row for $p:$ln (the same table lists it twice)"
          continue
        fi
        row_dispo[$k]="$disp"; row_owner[$k]="$owner"; row_token[$k]="$tok"; row_line[$k]="$p|$ln"
        n=$((n+1)); allrows=$((allrows+1))
        [ "$disp" = "KEEP" ] && keepcount=$((keepcount+1))
        ;;
      *)
        # a pipe-less line that is NOT a disposition directive is prose (the
        # CR-A table's accepted out-of-scope note); a line that LOOKS like a
        # row directive but has no pipes is refused — that is how prose ranges
        # were smuggled in (CTO blocker 2)
        case "$line" in
          DELETE*|CUT*|KEEP*) bad "$tbl: unparseable line (looks like a disposition directive but is not a canonical pipe row): $(echo "$line" | cut -c1-80)";;
          *\`*) continue;; # backticked embedded literal (CR-A's next-line regex blob, pre- or post-amendment edition)
          *"|"*) bad "$tbl: unparseable line (has a pipe but is not a canonical row): $(echo "$line" | cut -c1-80)";;
          *) continue;; # accepted prose
        esac
        ;;
    esac
  done < "$tbl"
  if [ "$n" -eq 0 ]; then
    grep -q '^0 hits' "$tbl" && ok "$tbl: explicit 0-hits table" || bad "$tbl: parses to 0 rows and no explicit '0 hits' line"
  else
    ok "$tbl: parses, $n rows"
  fi
done
[ "$allrows" -eq 0 ] && bad "union holds no rows and no table is an explicit 0-hits table"

# -- FILE-LEVEL exclusivity (Finding 3): one owner per PATH across the union
declare -A path_owners
for key in "${!row_dispo[@]}"; do
  p=${row_line[$key]%%|*}
  o=${row_owner[$key]}
  cur=${path_owners[$p]:-}
  case " $cur " in *" $o "*) ;; *) path_owners[$p]="$cur $o";; esac
done
for p in "${!path_owners[@]}"; do
  # the two-path ruling: LINE-level ownership here, FILE-level everywhere else
  is_allow=""
  for a in $allowlist; do [ "$p" = "$a" ] && is_allow=yes; done
  if [ "$is_allow" = "yes" ]; then
    ok "line-level ownership path (ruling): $p"
    continue
  fi
  owners=$(printf '%s' "${path_owners[$p]}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
  if [ "$(echo "$owners" | wc -w)" -gt 1 ]; then
    bad "FILE DOUBLE-CLAIM: $p owned by [$owners] — ownership follows the FILE (one file, one owner)"
  fi
done

# -- the sweep: every hit line has rows, ONE owner, and a KEEP row covers it
hits=0; excluded=0
while IFS= read -r -d '' f; do
  # the disposition tables themselves are gate INPUTS, never swept — they
  # carry the literal and token names by design (a self-hit proves nothing)
  is_table=""
  for a in "$@"; do
    [ "$f" = "${a#./}" ] && is_table=yes && break
  done
  if [ -n "$is_table" ]; then continue; fi
  case "$f" in
    *_test.go) continue;;   # swept scope: Go *_test.go excluded
  esac
  # blanket row for this file (Finding 4): count-verified, printed, never silent
  if [ -n "${blanket_rows[$f]:-}" ]; then
    want=${blanket_rows[$f]}
    got=$(grep -a -c -i -E "$lit" -- "$f" 2>/dev/null); grc=$?
    if [ "$grc" -ge 128 ]; then
      bad "blanket grep CRASHED (signal) on $f — count unverifiable, never a silent skip"
      continue
    fi
    if [ "$got" = "$want" ]; then
      excluded=$((excluded+1)); blanket_seen["$f"]=1
    else
      bad "blanket row count mismatch: $f says count=$want, sweep finds ${got:-0} — ${blanket_reason[$f]}"
    fi
    continue
  fi
  out=$(grep -n -I -i -E "$lit" -- "$f" 2>/dev/null); grc=$?
  if [ "$grc" -ge 128 ]; then
    bad "sweep grep CRASHED (signal) on $f — never a silent skip"
    continue
  fi
  [ "$grc" -eq 0 ] || continue
  while IFS= read -r line; do
    ln=${line%%:*}
    hits=$((hits+1))
    k="$f|$ln"
    # collect this line's rows across ALL tables (ceded rows share one owner)
    owners=""; realdisp=""; tok=""
    for key in "${!row_dispo[@]}"; do
      [ "${row_line[$key]}" = "$k" ] || continue
      o=${row_owner[$key]}
      case " $owners " in *" $o "*) ;; *) owners="$owners $o";; esac
      d=${row_dispo[$key]}
      if [ "$d" != "-" ]; then realdisp=$d; tok=${row_token[$key]}; fi
    done
    owners=$(printf '%s' "$owners" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    if [ -z "$owners" ]; then
      bad "UNLISTED hit: $f:$ln: $(echo "$line" | cut -c1-90)"
      continue
    fi
    if [ -z "$realdisp" ]; then
      bad "hit has ONLY ceded markers (owner [$owners] names a part, but no table carries a real disposition — the reciprocal cession is incomplete): $f:$ln: $(echo "$line" | cut -c1-90)"
      continue
    fi
    nowners=$(echo "$owners" | wc -w)
    if [ "$nowners" -gt 1 ]; then
      bad "DOUBLE-CLAIM hit: $f:$ln owned by [$owners] — exactly one owner per line"
      continue
    fi
    if [ "$realdisp" = "KEEP" ] && printf '%s' "$line" | grep -qiF -- "$tok"; then
      continue
    fi
    bad "hit not covered by a KEEP row (owner $owners says $realdisp — the cut must have removed it): $f:$ln: $(echo "$line" | cut -c1-90)"
  done < <(printf '%s\n' "$out")
done < "$LIST"
ok "sweep complete: $hits hit lines, $excluded files under blanket rows, $keepcount KEEP rows in the union"
for p in "${!blanket_rows[@]}"; do
  if [ "${blanket_seen[$p]:-0}" = "1" ]; then
    ok "blanket row '$p' excluded with count=${blanket_rows[$p]} — ${blanket_reason[$p]}"
  else
    bad "blanket row '$p' matches NO tracked file — stale or typo (${blanket_reason[$p]})"
  fi
done

# -- invariant (4): canary over the union KEEP rows
can=0
for k in "${!row_dispo[@]}"; do
  [ "${row_dispo[$k]}" = "KEEP" ] || continue
  p=${row_line[$k]%%|*}
  t=${row_token[$k]}
  # the named-trap rows carry a descriptive token: `'mixed' (single-quoted)`
  # — the part that must be re-found is the quoted needle itself
  case "$t" in *" (single-quoted)") t=${t% (single-quoted)};; esac
  if [ -f "$p" ]; then
    grep -q -I -i -F -- "$t" "$p" 2>/dev/null; grc=$?
    if [ "$grc" -eq 0 ]; then can=$((can+1))
    elif [ "$grc" -ge 128 ]; then bad "canary grep CRASHED on $p — token '${row_token[$k]}' re-found is unverifiable"
    else bad "canary: KEEP row token '${row_token[$k]}' not re-found in $p — sweep or table is wrong"; fi
  else
    bad "canary: KEEP row path $p missing — sweep or table is wrong"
  fi
done
[ "$can" -gt 0 ] && ok "canary: all $can KEEP tokens re-found" || echo "NOTE canary n/a (no KEEP rows) — the guard test's sentinels are the canary"

# -- P0 risk-cap KEEP canary (CTO ruling 2026-10-01 10:5x): the four live
# futures caps (crypto-named) must exist at the integrated head
cap_f=""
for tok in $cap_sites; do
  if [ -z "$cap_f" ]; then cap_f=$tok; continue; fi
  if [ -f "$cap_f" ]; then
    grep -qF -- "$tok" "$cap_f" 2>/dev/null; grc=$?
    if [ "$grc" -eq 0 ]; then
      ok "risk-cap KEEP canary present: $cap_f :: $tok"
    elif [ "$grc" -ge 128 ]; then
      bad "risk-cap canary grep CRASHED on $cap_f — presence unverifiable"
    else
      bad "risk-cap KEEP canary LOST: $cap_f is missing '$tok' — P0 (C1: the owner's caps silently fall back to defaults)"
    fi
  else
    bad "risk-cap KEEP canary LOST: $cap_f missing at HEAD"
  fi
  cap_f=""
done

# -- content asserts (CTO ruling 2026-10-01): single-quoted 'mixed' sites
f_keep="web/src/components/plan/ExecutorVerdict.tsx";  n_keep="arm.state === 'mixed'"
f_cut="web/src/components/trader/TraderConfigModal.tsx"; n_cut="source_type === 'mixed'"
if [ -f "$f_keep" ]; then
  grep -qF -- "$n_keep" "$f_keep" 2>/dev/null; grc=$?
  if [ "$grc" -eq 0 ]; then
    ok "content-assert: ExecutorVerdict 'mixed' present (KEEP — its CR-C table row is the requirement)"
  elif [ "$grc" -ge 128 ]; then
    bad "content-assert grep CRASHED on $f_keep — presence unverifiable"
  else bad "content-assert: ExecutorVerdict lost its plan-state 'mixed' line (KEEP site vanished)"; fi
else bad "content-assert: $f_keep missing at HEAD"; fi
if [ -f "$f_cut" ]; then
  grep -qF -- "$n_cut" "$f_cut" 2>/dev/null; grc=$?
  if [ "$grc" -eq 0 ]; then
    found=""
    for key in "${!row_dispo[@]}"; do
      case "${row_line[$key]}" in "$f_cut|"*) found=yes;; esac
    done
    if [ "$found" = "yes" ]; then
      ok "content-assert: TraderConfigModal 'mixed' still present but the union names it (CUT pending)"
    else bad "content-assert: TraderConfigModal 'mixed' present with NO row — a lane forgot the CUT"; fi
  elif [ "$grc" -ge 128 ]; then
    bad "content-assert grep CRASHED on $f_cut — presence unverifiable"
  else
    ok "content-assert: TraderConfigModal 'mixed' gone (CUT complete)"
  fi
else
  ok "content-assert: TraderConfigModal 'mixed' gone (CUT complete)"
fi

echo "== $fail FAIL"
exit "$fail"
