#!/usr/bin/env bash
# post-install-check.sh — after an Update, read what the NEW process is doing.
#
# Checklist row Q11: "After Update, read live behaviour, not diffs." 95 refusals
# sat in the log for six days (10-23 → 10-29) because nobody read the running
# bot after an install; this script is the reading, one line per question.
#
# STRICTLY READ-ONLY. It never writes a file, never copies the database, never
# calls the bot's API, never restarts anything. The log is read with grep/awk;
# the database only through `sqlite3 -readonly "file:...?mode=ro"`.
#
# Usage:
#   deploy/post-install-check.sh [--log FILE] [--db FILE] [--release FILE]
#                                [--expect SHA|TAG]
#
# Defaults (the install dir is the repo this script sits in, or $VL_REPO):
#   --log      newest data/vl_*.log
#   --db       data/data.db
#   --release  deploy/RELEASE
#   --expect   none. The release you MEANT to install (a sha, 7+ hex, or a tag
#              resolved in the install dir's git). deploy/RELEASE is written by
#              the same install and restored with the binary by a rollback, so
#              only --expect can tell "the Update rolled back" from "it ran".
#
# The process judged is the one that wrote the LAST "BOOT INTEGRITY" line. Its
# lines start at its own start banner ("🚀 VL Intelligent", main.go), not at the
# boot line: main.go prints the boot line only after the traders have loaded
# and started, so their first lines (the mentor seed, the first context build)
# come before it. Lines from earlier processes never count against it.
#
# Time. Some answers need time to exist: the integrity beat runs every 60 s,
# FEED DOWN needs 10 minutes of silence plus grace, and the first AI cycle can
# take minutes. A check that looks too soon has seen nothing yet, and nothing
# seen is not the same as nothing wrong. So:
#   - the newest log line must be under 15 minutes old (a hung or dead process
#     stops writing, and every other check would read clean);
#   - under 12 minutes after boot the verdict is TOO EARLY, never PASS;
#   - a data_integrity_unknown pause is EXPECTED while the CME is closed (the
#     boot hole scan needs a live bar) and for 45 minutes after the reopen or
#     the boot, whichever is later (11-01: reopen 17:00, RESUMED 17:26). Until
#     then it is a WARN and the verdict is TOO EARLY; after that it is a FAIL;
#   - with the CME open past that settle time, no AI call since the process
#     started is a FAIL, and so is no SUCCESSFUL one (every ai_call ok=false,
#     whatever the class: 401, 429, timeout). While the CME is closed, every
#     call failing is a WARN and the verdict is TOO EARLY until the reopen plus
#     the settle time.
# "Now" is the clock, or POST_INSTALL_NOW (unix seconds) for tests.
#
# Output: one line per check, starting PASS / WARN / FAIL (INFO lines are shown
# for reading only and never change the verdict), then the last line
#   POST-INSTALL: PASS                   (exit 0) — no FAIL line, nothing pending
#   POST-INSTALL: FAIL                   (exit 1) — at least one FAIL line
#   POST-INSTALL: TOO EARLY — … re-run after MM-DD HH:MM CT  (exit 3)
#                                        — no FAIL yet, but something cannot be
#                                          judged until then
# Usage errors exit 2. No secrets are printed; trader ids are shortened to the
# trader's name plus the last 6 characters of its id.
set -u

# Settle times, in seconds (see "Time" above).
SETTLE_SEC=720        # 12 min: FEED DOWN threshold + grace, the first AI cycle
STALE_SEC=900         # 15 min: newest log line older than this = not alive
UNKNOWN_GRACE_SEC=2700 # 45 min after the reopen (or boot) for the boot hole scan

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
VL_REPO="${VL_REPO:-$(cd "$SCRIPT_DIR/.." && pwd)}"

LOG=""
DB="$VL_REPO/data/data.db"
RELEASE="$VL_REPO/deploy/RELEASE"
EXPECT=""

usage() {
  echo "usage: $0 [--log FILE] [--db FILE] [--release FILE] [--expect SHA|TAG]" >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --log)     [ $# -ge 2 ] || usage; LOG="$2"; shift 2 ;;
    --db)      [ $# -ge 2 ] || usage; DB="$2"; shift 2 ;;
    --release) [ $# -ge 2 ] || usage; RELEASE="$2"; shift 2 ;;
    --expect)  [ $# -ge 2 ] && [ -n "$2" ] || usage; EXPECT="$2"; shift 2 ;;
    -h|--help) usage ;;
    *) echo "unknown argument: $1" >&2; usage ;;
  esac
done

NOW="${POST_INSTALL_NOW:-$(date +%s)}"
if ! [[ "$NOW" =~ ^[0-9]+$ ]]; then
  echo "POST_INSTALL_NOW must be unix seconds, got '$NOW'" >&2
  exit 2
fi

fails=0
pass() { echo "PASS $1"; }
warn() { echo "WARN $1"; }
fail() { fails=$((fails+1)); echo "FAIL $1"; }
info() { echo "INFO $1"; }

# defer_until EPOCH WHY — something cannot be judged before EPOCH. The verdict
# is then TOO EARLY (unless a FAIL was already found), with the latest such time.
DEFER_UNTIL=0
DEFER_WHY=""
defer_until() {
  [ "$1" -gt "$DEFER_UNTIL" ] && DEFER_UNTIL=$1
  case "; $DEFER_WHY;" in *"; $2;"*) ;; *) DEFER_WHY+="${DEFER_WHY:+; }$2" ;; esac
}

# --- Chicago time helpers (GNU date). The CME hours modelled: open Sunday
# 17:00 CT to Friday 16:00 CT, closed 16:00-17:00 CT Monday to Thursday.
# Exchange holidays are not modelled: a holiday reads as open, which can only
# make a check stricter, never hide a problem.
ct_fmt() { TZ=America/Chicago date -d "@$1" '+%m-%d %H:%M'; }
ct_epoch() { # $1 = "MM-DD HH:MM:SS" Chicago time, $2 = year → unix seconds
  TZ=UTC date -d "TZ=\"America/Chicago\" $2-${1:0:5} ${1:6:8}" +%s 2>/dev/null
}
cme_open() { # $1 = unix seconds; exit 0 when the CME is open then
  local dow hm
  dow=$(TZ=America/Chicago date -d "@$1" +%u)
  hm=$((10#$(TZ=America/Chicago date -d "@$1" +%H%M)))
  case "$dow" in
    6) return 1 ;;
    7) [ "$hm" -ge 1700 ] ;;
    5) [ "$hm" -lt 1600 ] ;;
    *) [ "$hm" -lt 1600 ] || [ "$hm" -ge 1700 ] ;;
  esac
}
cme_open_since() { # $1 = unix seconds while open → when this session opened (the latest 17:00 CT)
  local d e
  d=$(TZ=America/Chicago date -d "@$1" +%Y-%m-%d)
  e=$(TZ=America/Chicago date -d "$d 17:00" +%s)
  if [ "$e" -gt "$1" ]; then
    d=$(TZ=America/Chicago date -d "$d - 1 day" +%Y-%m-%d)
    e=$(TZ=America/Chicago date -d "$d 17:00" +%s)
  fi
  echo "$e"
}
cme_next_open() { # $1 = unix seconds while closed → the next 17:00 CT reopen
  local d0 d i e
  d0=$(TZ=America/Chicago date -d "@$1" +%Y-%m-%d)
  for i in 0 1 2 3 4 5 6; do
    d=$(TZ=America/Chicago date -d "$d0 + $i day" +%Y-%m-%d)
    case "$(TZ=America/Chicago date -d "$d" +%u)" in 5|6) continue ;; esac
    e=$(TZ=America/Chicago date -d "$d 17:00" +%s)
    if [ "$e" -gt "$1" ]; then echo "$e"; return; fi
  done
}

finish() {
  if [ "$fails" -gt 0 ]; then
    echo "POST-INSTALL: FAIL"
    exit 1
  fi
  if [ "$DEFER_UNTIL" -gt "$NOW" ]; then
    echo "POST-INSTALL: TOO EARLY — $DEFER_WHY; re-run after $(ct_fmt "$DEFER_UNTIL") CT"
    exit 3
  fi
  echo "POST-INSTALL: PASS"
  exit 0
}

# Newest bot log by modification time. Logs are named by the day the process
# booted, not by today, so the newest file is the running process's log.
if [ -z "$LOG" ]; then
  LOG=$(ls -t "$VL_REPO"/data/vl_*.log 2>/dev/null | head -n 1)
fi
if [ -z "$LOG" ] || [ ! -r "$LOG" ]; then
  fail "log: no readable bot log (${LOG:-none found under $VL_REPO/data/vl_*.log})"
  finish
fi
info "log: $LOG"

# The app's OWN boot line, by shape (time, level, main.go caller, the 🔐
# prefix) — the same shape internal/updaterworker/bootcheck.go accepts, so a
# chat message or an echoed log line that merely contains the words does not
# move the start of the window.
BOOT_RE='^[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} \[(INFO|ERRO)\] [^ ]*/main\.go:[0-9]+ 🔐 BOOT INTEGRITY (OK|REFUSED) — rev '
boot=$(LC_ALL=C grep -nE "$BOOT_RE" "$LOG" | tail -n 1)
if [ -z "$boot" ]; then
  fail "boot: no BOOT INTEGRITY line in $LOG — the install did not boot, or this is not its log"
  finish
fi
BOOT_LN=${boot%%:*}
BOOT_LINE=${boot#*:}
BOOT_TS=${BOOT_LINE:0:14} # MM-DD HH:MM:SS, Chicago time

# The process start: its own banner line, the last one before the boot line.
# No banner (a log cut short) falls back to the boot line itself.
START_RE='^[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} \[INFO\] [^ ]*/main\.go:[0-9]+ ║ +🚀 VL Intelligent'
START_LN=$(head -n "$BOOT_LN" "$LOG" | LC_ALL=C grep -nE "$START_RE" | tail -n 1 | cut -d: -f1)
[ -n "$START_LN" ] || START_LN=$BOOT_LN
START_TS=$(sed -n "${START_LN}p" "$LOG" | cut -c1-14)

# after_boot prints every line this process wrote: from its start banner to the
# end of the log. The traders start before the boot line, so their boot-time
# errors are inside it.
after_boot() { tail -n +"$START_LN" "$LOG"; }

# Log times are Chicago wall clock without a year; the year comes from the
# log's name (vl_YYYY-MM-DD.log), else this year.
log_year=$(basename "$LOG" | sed -nE 's/^vl_([0-9]{4})-([0-9]{2})-[0-9]{2}\.log$/\1/p')
log_month=$(basename "$LOG" | sed -nE 's/^vl_([0-9]{4})-([0-9]{2})-[0-9]{2}\.log$/\2/p')
[ -n "$log_year" ] || log_year=$(date +%Y)
boot_month=${BOOT_TS:0:2}
# A log named in December whose boot line says January rolled the year.
if [ -n "$log_month" ] && [ "$boot_month" -lt "$log_month" ] 2>/dev/null; then
  log_year=$((log_year + 1))
fi
year_of() { # $1 = "MM-DD ..." stamp → its year (a month before the boot's rolled over)
  if [ "${1:0:2}" -lt "$boot_month" ] 2>/dev/null; then echo $((log_year + 1)); else echo "$log_year"; fi
}
boot_epoch=$(ct_epoch "$BOOT_TS" "$log_year")
start_epoch=$(ct_epoch "$START_TS" "$(year_of "$START_TS")")
[ -n "$start_epoch" ] || start_epoch=$boot_epoch
# The log's newest stamped line: how fresh the process is, and where the
# database window ends.
end_ts=$(after_boot | LC_ALL=C grep -E '^[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} ' | tail -n 1 | cut -c1-14)
[ -n "$end_ts" ] || end_ts=$BOOT_TS
end_epoch=$(ct_epoch "$end_ts" "$(year_of "$end_ts")")
if [ -z "$boot_epoch" ] || [ -z "$end_epoch" ]; then
  fail "time: could not read the log times ($BOOT_TS, $end_ts) — cannot tell how long the process has run or whether it is alive"
  finish
fi
# The later of the boot and the current session's open: from then on, the
# bot has had live bars to work with.
if cme_open "$NOW"; then
  CME_NOW="open"
  live_since=$(cme_open_since "$NOW")
  [ "$boot_epoch" -gt "$live_since" ] && live_since=$boot_epoch
else
  CME_NOW="closed"
  live_since=""
fi

# ---------------------------------------------------------------------------
# 1. Boot integrity: OK, the rev is the one deploy/RELEASE names, goldens PASS.
# ---------------------------------------------------------------------------
boot_status=$(echo "$BOOT_LINE" | sed -E 's/.*BOOT INTEGRITY ([A-Z]+) — rev .*/\1/')
boot_rev=$(echo "$BOOT_LINE" | sed -E 's/.* — rev ([^ ]+).*/\1/')
boot_goldens=$(echo "$BOOT_LINE" | sed -nE 's/.* · goldens ([A-Z]+).*/\1/p')
want_rev=""
if [ -r "$RELEASE" ]; then
  want_rev=$(head -n 1 "$RELEASE" | tr -d '[:space:]')
fi

if [ "$boot_status" = "OK" ]; then
  pass "boot integrity: OK at $BOOT_TS CT (log line $BOOT_LN)"
else
  fail "boot integrity: $boot_status at $BOOT_TS CT — trading is refused for this process"
fi
case "$BOOT_LINE" in
  *" +dirty "*) warn "boot rev: binary $boot_rev was built from a tree with uncommitted changes (+dirty)" ;;
esac
if [ -z "$want_rev" ]; then
  fail "boot rev: cannot read the release sha from $RELEASE — cannot confirm the running binary"
elif [ "${#boot_rev}" -ge 7 ] && [ "${want_rev:0:${#boot_rev}}" = "$boot_rev" ]; then
  pass "boot rev: $boot_rev matches $(basename "$RELEASE") ${want_rev:0:12}"
else
  fail "boot rev: running $boot_rev but $(basename "$RELEASE") names ${want_rev:0:12} — a stale or wrong binary is running"
fi
if [ -n "$EXPECT" ]; then
  if [[ "$EXPECT" =~ ^[0-9a-f]{7,40}$ ]]; then
    expect_sha=$EXPECT
  else
    expect_sha=$(git -C "$VL_REPO" rev-parse --verify -q "refs/tags/$EXPECT^{commit}" 2>/dev/null)
  fi
  if [ -z "$expect_sha" ]; then
    fail "expect: cannot resolve '$EXPECT' to a commit (not a sha, and no such tag in $VL_REPO) — cannot confirm the release you meant to install is running"
  else
    exp_n=${#boot_rev}; [ "${#expect_sha}" -lt "$exp_n" ] && exp_n=${#expect_sha}
    if [ "$exp_n" -ge 7 ] && [ "${expect_sha:0:$exp_n}" = "${boot_rev:0:$exp_n}" ]; then
      pass "expect: running $boot_rev is $EXPECT"
    else
      fail "expect: running $boot_rev but you expected $EXPECT (${expect_sha:0:12}) — the release you meant to install is not running (rolled back, or never activated)"
    fi
  fi
fi
if [ "$boot_goldens" = "PASS" ]; then
  pass "goldens: PASS"
else
  fail "goldens: ${boot_goldens:-missing} — the prompt goldens did not verify on this boot"
fi

# ---------------------------------------------------------------------------
# 1b. Alive and settled. A process that hung or died after a good boot writes
#     nothing more, and every check below would then read clean. And a check
#     run seconds after boot has seen nothing yet.
# ---------------------------------------------------------------------------
since_boot=$((NOW - boot_epoch))
info "time: checked $(ct_fmt "$NOW") CT, $((since_boot / 60)) min after boot; the CME is $CME_NOW"
age=$((NOW - end_epoch))
if [ "$age" -gt "$STALE_SEC" ]; then
  fail "alive: the newest log line ($end_ts CT) is $((age / 60)) min old — the process is hung, dead, or writing another log"
else
  pass "alive: newest log line $end_ts CT, ${age} s before this check"
fi
if [ "$since_boot" -lt "$SETTLE_SEC" ]; then
  defer_until $((boot_epoch + SETTLE_SEC)) "only $since_boot s since boot (the integrity beat, the feed watcher and the first AI cycle need ~$((SETTLE_SEC / 60)) min)"
fi

# ---------------------------------------------------------------------------
# 2. [ERRO] lines since boot, with the three most common messages. Numbers,
#    times and trader ids are folded so repeats of one message group together.
#    A count above zero is a WARN: the serious ones (feed down, AI credit,
#    pauses, push failures) each have their own FAIL check below.
# ---------------------------------------------------------------------------
redact() {
  sed -E 's/(bot)[0-9]+:[A-Za-z0-9_-]+/\1[redacted]/g;
          s/(sk-)[A-Za-z0-9_-]+/\1[redacted]/g;
          s/eyJ[A-Za-z0-9._-]+/[redacted-jwt]/g'
}
erro_count=$(after_boot | grep -c ' \[ERRO\] ')
if [ "$erro_count" -eq 0 ]; then
  pass "errors: 0 [ERRO] lines since the process started ($START_TS CT, log line $START_LN)"
else
  warn "errors: $erro_count [ERRO] lines since the process started ($START_TS CT, log line $START_LN) — most common:"
  after_boot | grep ' \[ERRO\] ' |
    sed -E 's/^[0-9-]+ [0-9:]+ \[ERRO\] [^ ]+ //;
            s/^\[trader_id=[^ ]* trader_name=[^]]*\] //;
            s/(trader(_id)?=)[^ ]+/\1…/g;
            s/[0-9]+(ms|h|m|s)/N\1/g;
            s/(^|[^A-Za-z])[0-9]+/\1N/g' |
    redact | awk '{ print substr($0, 1, 110) }' |
    sort | uniq -c | sort -rn | head -n 3 |
    sed -E 's/^ *([0-9]+) /     \1× /'
fi

# The awk programs below share one helper: the trader a line belongs to,
# shortened to "name [..last6 of id]". Lines without a trader prefix return "".
AWK_TRADER='
function trader(line,    s, rest, sp, id, nm, e) {
  s = index(line, "[trader_id=")
  if (s == 0) return ""
  rest = substr(line, s + 11)
  sp = index(rest, " trader_name=")
  if (sp == 0) return ""
  id = substr(rest, 1, sp - 1)
  rest = substr(rest, sp + 13)
  e = index(rest, "]")
  nm = substr(rest, 1, e - 1)
  return nm " [.." substr(id, length(id) - 5) "]"
}
function ts(line) { return substr(line, 1, 14) }
'

# open_episodes OPEN CLOSE — per trader, an episode opened by a line containing
# OPEN and closed by a later line containing CLOSE. OPEN may list several
# openers separated by "|"; a line containing any one of them opens. Prints
# "trader<TAB>time<TAB>class" for every trader whose last OPEN has no CLOSE
# after it, then "#n<TAB>opens". The class is the text in brackets right after
# an opener that ends in "(" (the pause class), else "-".
open_episodes() {
  after_boot | awk -v opener="$1" -v closer="$2" "$AWK_TRADER"'
    BEGIN { nop = split(opener, ops, "|") }
    { op = ""; for (i = 1; i <= nop; i++) if (index($0, ops[i])) { op = ops[i]; break } }
    op != "" {
      t = trader($0); if (t == "") next
      since[t] = ts($0); n++; cls[t] = "-"
      if (substr(op, length(op)) == "(") {
        rest = substr($0, index($0, op) + length(op))
        e = index(rest, ")"); if (e > 1) cls[t] = substr(rest, 1, e - 1)
      }
      next
    }
    index($0, closer) { t = trader($0); if (t != "") delete since[t] }
    END { for (t in since) print t "\t" since[t] "\t" cls[t]; print "#n\t" n + 0 }'
}

# report_episodes NAME OPEN CLOSE WHAT [EXPECTED_CLASS] — FAIL per unresolved
# trader, else PASS. A reader that did not run is a FAIL, never a PASS: an
# unread log is not a clean one.
#
# EXPECTED_CLASS names an episode class that is expected to stay open until
# live bars have had time to arrive: while the CME is closed, or within
# UNKNOWN_GRACE_SEC of the later of the session open and the boot. Such an
# episode is a WARN and makes the verdict TOO EARLY; past that it is a FAIL.
report_episodes() {
  local name="$1" what="$4" expected="${5:-}" out n bad=0 t s c nxt tag
  out=$(open_episodes "$2" "$3")
  n=$(printf '%s\n' "$out" | awk -F'\t' '$1 == "#n" { print $2 }')
  if [ -z "$n" ]; then
    fail "$name: the log reader failed — cannot tell whether anything is unresolved"
    return
  fi
  while IFS=$'\t' read -r t s c; do
    [ -z "$t" ] && continue
    [ "$t" = "#n" ] && continue
    bad=1
    if [ -n "$expected" ] && [ "$c" = "$expected" ]; then
      if [ "$CME_NOW" = "closed" ]; then
        nxt=$(cme_next_open "$NOW")
        warn "$name: $t — $what ($c) since $s CT, still open — expected while the CME is closed: the check needs a live bar, and those start at the $(ct_fmt "$nxt") CT reopen"
        defer_until $((nxt + UNKNOWN_GRACE_SEC)) "$name: $c is expected until live bars arrive after the reopen"
        continue
      fi
      if [ "$NOW" -lt $((live_since + UNKNOWN_GRACE_SEC)) ]; then
        warn "$name: $t — $what ($c) since $s CT, still open — expected for up to $((UNKNOWN_GRACE_SEC / 60)) min after the reopen or boot ($(ct_fmt "$live_since") CT)"
        defer_until $((live_since + UNKNOWN_GRACE_SEC)) "$name: $c may still clear once live bars arrive"
        continue
      fi
    fi
    tag=""
    [ -n "$c" ] && [ "$c" != "-" ] && tag=" ($c)"
    fail "$name: $t — $what$tag since $s CT, never resolved"
  done <<<"$out"
  [ "$bad" -eq 0 ] && pass "$name: $n start line(s) since the process started, every trader resolved"
}

# ---------------------------------------------------------------------------
# 3. TRADING PAUSED without a later TRADING RESUMED for the same trader
#    (trader/data_integrity_gate.go: "⏸ TRADING PAUSED (" / "▶ TRADING RESUMED:").
#    A pause that comes back within the merge window of its own resume logs
#    "⏸ TRADING PAUSED AGAIN (" instead, with no new owner push (m469) — it
#    refuses entries exactly the same, so it opens a pause here too.
#    data_integrity_unknown is logged whenever a window is Blocked, open or
#    closed (data_integrity_gate.go: only the owner PUSH waits for the open),
#    so on a closed-market install it is expected until bars arrive.
# ---------------------------------------------------------------------------
report_episodes "pause" "⏸ TRADING PAUSED (|⏸ TRADING PAUSED AGAIN (" "▶ TRADING RESUMED:" "TRADING PAUSED (new entries refused)" "data_integrity_unknown"

# ---------------------------------------------------------------------------
# 4. FEED DOWN without a later "bars are flowing again" for the same trader
#    (trader/auto_trader_feedwatch.go).
# ---------------------------------------------------------------------------
report_episodes "feed" "🚨 FEED DOWN:" "✅ feed: bars are flowing again" "FEED DOWN (no NT8 bars)"

# ---------------------------------------------------------------------------
# 5. AI calls.
#  a) The newest `ai_call` line (mcp/client.go; it names no trader), and
#     whether ANY ai_call since the process started has ok=true.
#  b) Per trader in the log: the newest "💸 DEEPSEEK PAYMENT FAILURE" with no
#     later "⏱️ AI call" for that trader means its cycles are dying on 402.
#  c) Per trader in the database: the newest decision_records row since boot.
# ---------------------------------------------------------------------------
last_ai=$(after_boot | grep ' ai_call model=' | tail -n 1)
if [ -z "$last_ai" ]; then
  # While the CME is closed an idle AI is plausible. Once it has been open past
  # the settle time, no AI call at all means the bot is deciding nothing.
  if [ "$CME_NOW" = "closed" ]; then
    warn "ai: no ai_call line since the process started (the CME is closed)"
  elif [ "$NOW" -ge $((live_since + SETTLE_SEC)) ]; then
    fail "ai: no ai_call line since the process started, and the CME has been open $(((NOW - live_since) / 60)) min of it — the bot is not deciding anything"
  else
    warn "ai: no ai_call line since the process started yet"
    defer_until $((live_since + SETTLE_SEC)) "no AI call yet"
  fi
else
  ai_ok=$(echo "$last_ai" | sed -nE 's/.* ok=([a-z]+).*/\1/p')
  ai_http=$(echo "$last_ai" | sed -nE 's/.* http_status=([0-9]+).*/\1/p')
  ai_class=$(echo "$last_ai" | sed -nE 's/.* class=([^ ]+).*/\1/p')
  ai_model=$(echo "$last_ai" | sed -nE 's/.* ai_call model=([^ ]+).*/\1/p')
  ai_ts=${last_ai:0:14}
  # How many ai_call lines since the process started succeeded. None at all is
  # the bot deciding nothing, whatever the failure class (401, 429, timeout).
  ai_n=$(after_boot | grep -c ' ai_call model=')
  ai_ok_n=$(after_boot | grep ' ai_call model=' | grep -cE ' ok=true( |$)')
  ai_latest="latest ai_call ($ai_model, $ai_ts CT) ok=${ai_ok:-n/a} http_status=${ai_http:-n/a} class=${ai_class:-n/a}"
  if [ "$ai_http" = "402" ]; then
    fail "ai: AI credit exhausted — latest ai_call ($ai_model, $ai_ts CT) ok=$ai_ok http_status=402"
  elif [ "$ai_ok" = "true" ]; then
    pass "ai: latest ai_call ($ai_model, $ai_ts CT) ok=true http_status=${ai_http:-n/a}"
  elif [ "$ai_ok_n" -gt 0 ]; then
    warn "ai: $ai_latest — $ai_ok_n of $ai_n ai_call(s) since the process started succeeded"
  elif [ "$CME_NOW" = "closed" ]; then
    # Cannot be judged until the bot has had live bars past the settle time.
    nxt=$(cme_next_open "$NOW")
    warn "ai: no successful AI call since the process started ($ai_n ai_call(s), every one failed; $ai_latest) — the CME is closed"
    defer_until $((nxt + SETTLE_SEC)) "no successful AI call yet"
  elif [ "$NOW" -ge $((live_since + SETTLE_SEC)) ]; then
    fail "ai: no successful AI call since the process started — $ai_n ai_call(s), every one failed; $ai_latest; the CME has been open $(((NOW - live_since) / 60)) min of it — the bot is not deciding anything"
  else
    warn "ai: no successful AI call since the process started yet ($ai_n ai_call(s), every one failed; $ai_latest)"
    defer_until $((live_since + SETTLE_SEC)) "no successful AI call yet"
  fi
fi

ai402=$(after_boot | awk "$AWK_TRADER"'
  index($0, "💸 DEEPSEEK PAYMENT FAILURE") { t = trader($0); if (t != "") { last402[t] = ts($0); n402[t]++ } ; next }
  index($0, "⏱️ AI call (")              { t = trader($0); if (t != "") delete last402[t] }
  END { for (t in last402) print t "\t" last402[t] "\t" n402[t]; print "#ok" }')
case "$ai402" in
  *"#ok") ;;
  *) ai402="#failed" ;;
esac
if [ "$ai402" = "#failed" ]; then
  fail "ai per trader (log): the log reader failed — cannot tell whether a trader is on 402"
elif [ "$ai402" = "#ok" ]; then
  pass "ai per trader (log): no trader's latest AI cycle is a 402 payment failure"
else
  while IFS=$'\t' read -r t s c; do
    [ "$t" = "#ok" ] && continue
    fail "ai per trader (log): $t — AI credit exhausted (402), latest at $s CT, $c payment failure(s) since boot"
  done <<<"$ai402"
fi

# Database leg. decision_records stamps are UTC ("+00:00"). The window starts
# at the process start and ends at the log's newest stamped line, so an older
# log is judged by its own process's rows, not by a later process that wrote to
# the same table.
utc_of() { TZ=UTC date -d "@$1" '+%Y-%m-%d %H:%M:%S'; }
boot_utc=$(utc_of "$start_epoch")
end_utc=$(utc_of "$end_epoch")

if ! command -v sqlite3 >/dev/null 2>&1; then
  warn "ai per trader (db): sqlite3 is not installed — database leg skipped"
elif [ ! -r "$DB" ]; then
  warn "ai per trader (db): no readable database at $DB — database leg skipped"
elif [ -z "$boot_utc" ] || [ -z "$end_utc" ]; then
  warn "ai per trader (db): could not convert the log times ($BOOT_TS, $end_ts) to UTC — database leg skipped"
else
  rows=$(sqlite3 -readonly -separator $'\t' "file:$DB?mode=ro" "
    SELECT COALESCE(t.name, ''), d.trader_id, substr(d.timestamp, 1, 19), d.success,
           COALESCE(d.error_class, ''),
           (SELECT count(*) FROM decision_records x
             WHERE x.trader_id = d.trader_id AND x.timestamp >= '$boot_utc'
               AND substr(x.timestamp, 1, 19) <= '$end_utc'
               AND x.error_class = 'ai_payment_402')
      FROM decision_records d LEFT JOIN traders t ON t.id = d.trader_id
     WHERE d.id IN (SELECT max(id) FROM decision_records
                     WHERE timestamp >= '$boot_utc'
                       AND substr(timestamp, 1, 19) <= '$end_utc'
                     GROUP BY trader_id)
     ORDER BY 1;" 2>&1)
  rc=$?
  if [ "$rc" -ne 0 ]; then
    warn "ai per trader (db): read failed: $(echo "$rows" | head -n 1 | redact)"
  elif [ -z "$rows" ]; then
    warn "ai per trader (db): no decision_records between boot and the log's last line ($boot_utc .. $end_utc UTC)"
  else
    while IFS=$'\t' read -r name id at ok class n402; do
      label="$name [..${id: -6}]"
      if [ "$class" = "ai_payment_402" ]; then
        fail "ai per trader (db): $label — AI credit exhausted (402) on the latest decision at $at UTC; $n402 such row(s) since boot"
      elif [ "$ok" = "1" ]; then
        pass "ai per trader (db): $label — latest decision at $at UTC succeeded"
      else
        warn "ai per trader (db): $label — latest decision at $at UTC failed (${class:-no error_class})"
      fi
    done <<<"$rows"
  fi
fi

# ---------------------------------------------------------------------------
# 6. Mentor funnel — the newest "🧑‍🏫 mentor funnel" line per trader, for
#    reading only (trader/mentor_funnel.go). Placed 0 is not an error by itself.
# ---------------------------------------------------------------------------
funnel=$(after_boot | awk "$AWK_TRADER"'
  index($0, "🧑‍🏫 mentor funnel [") {
    t = trader($0); if (t == "") next
    p = "n/a"; f = "n/a"; e = "n/a"
    if (match($0, /placed [0-9]+/)) p = substr($0, RSTART + 7, RLENGTH - 7)
    if (match($0, /filled [0-9]+/)) f = substr($0, RSTART + 7, RLENGTH - 7)
    if (match($0, /entry intents [0-9]+/)) e = substr($0, RSTART + 14, RLENGTH - 14)
    last[t] = ts($0) " CT: entry intents " e " · placed " p " · filled " f
  }
  END { for (t in last) print t ": " last[t] }' | sort)
if [ -z "$funnel" ]; then
  info "funnel: no mentor funnel line since boot"
else
  while IFS= read -r ln; do info "funnel: $ln"; done <<<"$funnel"
fi

# ---------------------------------------------------------------------------
# 7. Owner push (Telegram) — internal/ownerpush/ownerpush.go. A send error
#    with no later SENT means the owner is not being told anything right now.
#    Held messages discarded as stale or evicted from a full hold never reached
#    the owner either.
# ---------------------------------------------------------------------------
push=$(after_boot | awk '
  index($0, "owner push: SENT ")        { sent++; lastsent = NR; next }
  index($0, "owner push: SEND FAILED ") ||
  index($0, "owner push: sender panicked") ||
  index($0, "deadline - abandoned")     { errs++; lasterr = NR; lasterrts = substr($0, 1, 14); next }
  index($0, "owner push: held ") && index($0, "discarded as stale") { dropped++; next }
  index($0, "owner push: hold full")    { dropped++ }
  END { if (lasterrts == "") lasterrts = "n/a" # an empty field would collapse in read
        printf "%d\t%d\t%d\t%s\t%d\n", sent, errs, dropped, lasterrts, (lasterr > lastsent) }')
IFS=$'\t' read -r p_sent p_errs p_dropped p_lasterr p_stuck <<<"$push"
if [ -z "${p_stuck:-}" ]; then
  fail "push: the log reader failed — cannot tell whether owner pushes are reaching Telegram"
elif [ "$p_errs" -gt 0 ] && [ "$p_stuck" -eq 1 ]; then
  fail "push: $p_errs Telegram send error(s) since boot and nothing sent after the last one ($p_lasterr CT) — the owner is not being told"
elif [ "$p_errs" -gt 0 ]; then
  warn "push: $p_errs Telegram send error(s) since boot, recovered ($p_sent sent, the last after the errors)"
else
  pass "push: $p_sent sent, 0 send errors since boot"
fi
if [ "${p_dropped:-0}" -gt 0 ]; then
  warn "push: $p_dropped held message(s) dropped (stale or hold full) — never delivered"
fi

finish
