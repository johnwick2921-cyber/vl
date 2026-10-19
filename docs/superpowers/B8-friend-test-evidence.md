# B8 real friend-PC test — read-only evidence checklist (DS-104)

Prep for the B8 end-to-end test (d26 §B8). This is a **read-only** evidence
checklist: it names the exact commands and PASS criteria for each B8 evidence
item. It does NOT execute anything and must not be used to mutate the box.

**Hard safety lines (CTO 2026-10-19 07:59):**
- Do NOT create friends, change accounts/keys, or touch the owner service / NT8.
- Do NOT run the S1/S2 scans or the revoke probe against an unconfirmed friend
  target.
- No simulated evidence labeled real B8 — every claim below must be **[A]**
  directly observed (ran the command, read the exact log line).

## Prerequisites

- Owner box: release #39 installed; Tailscale up; `TAILSCALE_API_KEY` in the
  owner `.env`; `deploy/friends/check-exposure.sh` present.
- Friend PC: Tailscale installed + joined the tailnet; `VL-Setup.exe` installed;
  install code pasted; friend instance `tom` running (`vl-friend@tom.service`).
- Friend name: `tom` (d26 §B8). Friend ports: read from `GET /api/owner/friends`
  (`web_port`, `nt_port`) — do not guess them.
- **Test-start timestamp:** capture `TS="$(date -u +%Y-%m-%dT%H:%M:%S)"` BEFORE
  the test and pass `--since "$TS"` to every `journalctl` below. Counts are
  scoped to the test window, never to all historical logs.
- **Key-presence audit:** never put the 64-hex key in a command's argv (it leaks
  into process listings and shell history). Read the key from the friend `.env`
  inside a private process and print only a match COUNT (see §1).

## 1. Hello auth accepted — WITHOUT the key contents

Proves B5 **S5** (constant-time compare, key never logged).

The server logs the handshake with metadata only — never the key:

| what | read-only command (owner box) |
|---|---|
| handshake accepted | `journalctl --user -u vl-friend@tom --since "$TS" --no-pager \| grep "hello handshake OK"` |
| no refusal | `journalctl --user -u vl-friend@tom --since "$TS" --no-pager \| grep -c "connect_key refused"` |
| key never logged | see the private-process audit below (prints a count only, key never in argv) |

**Key-presence audit** (private process — the key is read from the `.env`, never
passed on a command line, and only a match count is printed):

```
journalctl --user -u vl-friend@tom --since "$TS" --no-pager > /tmp/b8-tom.log
python3 - <<'PY'
import pathlib
key = ""
for ln in pathlib.Path("/home/hoang/vl-friends/tom/.env").read_text().splitlines():
    if ln.startswith("NT_CONNECT_KEY="):
        key = ln.split("=", 1)[1].strip()
log = pathlib.Path("/tmp/b8-tom.log").read_text()
print("key_matches_in_log_count:", log.count(key))   # count ONLY — never the key
PY
```

**PASS:** at least one `hello handshake OK` line **with a `build_id`** (plus
`protocol_version`, `source`, `nt8_pid`, `assembly_mvid`); refusal count `0`;
key-match count `0`.

Source: `provider/ninjatrader/tcp_server.go:2099` (refused — logs only `addr`),
`:2117` (handshake OK — logs only metadata). The compare is
`crypto/subtle.ConstantTimeCompare` (`:2097`).

## 2. MNQ bars arriving in tom's bot

Proves the friend's NT8 → AddOn → TCP → bot bar feed is live — **actual fresh
data received**, not merely the absence of an error.

The 60-second `wire_liveness` reporter answers "feed or math" at a glance:

```
journalctl --user -u vl-friend@tom --since "$TS" --no-pager | grep "wire_liveness" | tail -3
```

**PASS (all three, no absent-error-only evidence):**
- `last_frame_age` small (`< 60s` in the sampled line — a frame arrived within
  the last minute);
- `frames_per_min` `> 0`;
- `bar_age` shows a **fresh 1m bar** — `1m=<N>s` with `N < 120` (a bar stamped
  within the last two minutes), and `5m`/`15m` real (not `none`).

A `bar_age` of `none` or a large `last_frame_age` is FAIL even with no error
line.

Source: `provider/ninjatrader/tcp_server.go:1311` (livenessReporter) — logs
`wire_liveness` with `last_frame_age`, `frames_per_min`, `bar_age`.

## 3. S1 / S2 isolation scans (read-only)

Proves B5 **S1** (friend binds only to the tunnel IP, never `0.0.0.0`/`::`) and
**S2** (LAN cannot reach friend ports; the friend's Tailscale device reaches
only ITS two ports).

Tool: `deploy/friends/check-exposure.sh` — READ-ONLY (`ss -ltnp` + TCP probes;
never changes any socket, file or service).

| check | command |
|---|---|
| S1 bind (owner box) | `bash deploy/friends/check-exposure.sh --ports <friend ports> --owner-ports 8080,36974` |
| S2 from LAN | `bash deploy/friends/check-exposure.sh --probe <owner lan ip> --ports <friend ports> --owner-ports 8080,36974` |
| S2 from the friend's tailnet device | `bash deploy/friends/check-exposure.sh --probe <tunnel ip> --ports <its two ports> --owner-ports 8080,36974` |

**PASS:** bind check prints every friend port `bound to <tunnel>` (none on
`0.0.0.0`/`::`); the LAN probe prints every friend port UNREACHABLE; the
tailnet probe prints reachable ONLY the friend's two ports.

Constraint: only after the friend target is confirmed.

## 4. Revoke ≤ 10s, then rekey → reconnect

Proves B5 **S11** (revoke works within 10s) and B8 step 5.

| step | read-only command (owner box) |
|---|---|
| stamp before | `date +%s.%N` |
| revoke | owner clicks Revoke on `tom` (the one mutating action — owner does it) |
| unit stopped | `systemctl --user is-active vl-friend@tom` → `inactive` within 10s of the stamp |
| state recorded | `GET /api/owner/friends` → `tom` shows `state: revoked` |
| tailscale device gone | `tailscale status` no longer lists the friend's device |
| rekey | `POST /api/owner/friends/tom/rotate` → new key → friend reconnects |
| reconnect | `journalctl --user -u vl-friend@tom --since "$TS" --no-pager \| grep "hello handshake OK"` (fresh line); `grep -c "connect_key refused"` still `0` |

**PASS:** unit inactive within 10s; `state: revoked`; tailscale device removed;
after rotate, a fresh `hello handshake OK` and refusal count `0`.

Source: `api/handler_friends.go:459` (revoke → `remove.sh` + tailscale device
delete), `deploy/friends/remove.sh:53` (`systemctl --user stop/disable` — moves
the folder to trash, never deletes).

## 5. Known gaps — B8 BLOCKED (verified 2026-10-19, DS-104)

B8 is **BLOCKED** on (a) revoke recovery + device enrollment and (b) the real
friend prerequisites (owner B6 steps + friend install). The revoke →
resume/new-key → reconnect sequence does **not** hold against the current code.

**Exact reproduction (isolated temp fixture — no live services, no secrets):**

```
FIX=$(mktemp -d); ROOT="$FIX/vl-friends"; TRASH="$FIX/trash"; mkdir -p "$ROOT/tom" "$TRASH"
cat > "$ROOT/tom/.env" <<'EOF'   # friend .env (NT_CONNECT_KEY=<64-hex test key> …)
NT_CONNECT_KEY=<64-hex-test-key>
API_SERVER_PORT=18081
API_SERVER_HOST=100.64.0.7
NT_TCP_LISTEN_ADDR=100.64.0.7:36981
VL_FRIEND_INSTANCE=1
VL_FRIEND_NAME=tom
EOF
python3 - "$ROOT/friends.json" <<'PY'   # registry row: state active, tailscale_device_id ""
import json, sys
json.dump([{"name":"tom","web_port":18081,"nt_port":36981,"key_sha256":"<sha256>",
            "state":"active","created_at":"2026-10-19T00:00:00+00:00",
            "tailscale_device_id":""}], open(sys.argv[1],"w"), indent=2)
PY
HOME="$FIX" TRASH_DIR="$TRASH" bash deploy/friends/remove.sh tom --root "$ROOT"
```

Observed output (exact, condensed):

```
remove: friend 'tom' moved to <FIX>/trash/friend-tom
remove rc=0
ROOT/tom exists? NO — folder moved
friend .env still readable for rotate? NO — setFriendEnvKey would return ENOENT
registry state: {'state': 'revoked', 'tailscale_device_id': ''}
unit WorkingDirectory (<FIX>/vl-friends/tom) exists? NO — systemd start would fail to chdir
```

- **GAP-1 recoverability.** `revoke` → `remove.sh` moves the whole friend folder
  (`/home/hoang/vl-friends/<name>`, including `.env` and `data/`) to trash. So
  `resume` (`systemctl --user start`, but the unit is disabled AND its
  `WorkingDirectory` is gone) and `rotate` (`setFriendEnvKey` reads
  `<root>/<name>/.env` → ENOENT) both fail. Implemented = shutdown only.
- **GAP-2 device enrollment.** `TailscaleDeviceID` is never populated
  (`deploy/friends/provision.sh:151` writes `""`; no code sets it), so
  `tailscaleDeleteDevice` (`api/handler_friends.go:615`) returns `no_device_id`
  — the omission IS surfaced as `no_device_id` (logged "not done:
  no_device_id"), **not silent** — but the device is never actually deleted.

**Design direction (ruled, NOT an unverified workaround).** The follow-up design
and dispatch live in
`docs/superpowers/B8-friend-revoke-recovery-design.md`. Ruled: revoke is
TERMINAL (archived folder + provenance preserved); recovery is an explicit
Re-add with a fresh key + install code (the old key is never restored);
pause/resume stays reversible. No guessed trash-path restore, no hostname-based
device delete, no client-self-reported ID trusted alone — device association is
cross-checked through the OWNER-controlled Tailscale API against the
authenticated source and the expected friend tag, refusing ambiguous/mismatched
identities. Required tests exercise provision → revoke → re-add and rejected old
credentials. #39 stays updater-only.
