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

## 1. Hello auth accepted — WITHOUT the key contents

Proves B5 **S5** (constant-time compare, key never logged).

The server logs the handshake with metadata only — never the key:

| what | read-only command (owner box) |
|---|---|
| handshake accepted | `journalctl --user -u vl-friend@tom --no-pager \| grep "hello handshake OK"` |
| no refusal | `journalctl --user -u vl-friend@tom --no-pager \| grep -c "connect_key refused"` |
| key never logged | `journalctl --user -u vl-friend@tom --no-pager \| grep -c "<the friend's 64-hex connect key>"` |

**PASS:** at least one `hello handshake OK` line (with `protocol_version`,
`source`, `build_id`, `nt8_pid`, `assembly_mvid`); refusal count `0`; key-string
count `0`.

Source: `provider/ninjatrader/tcp_server.go:2099` (refused — logs only `addr`),
`:2117` (handshake OK — logs only metadata). The compare is
`crypto/subtle.ConstantTimeCompare` (`:2097`).

## 2. MNQ bars arriving in tom's bot

Proves the friend's NT8 → AddOn → TCP → bot bar feed is live.

The 60-second `wire_liveness` reporter answers "feed or math" at a glance:

```
journalctl --user -u vl-friend@tom --no-pager | grep "wire_liveness" | tail -3
```

**PASS:** `last_frame_age` is small (seconds), `frames_per_min` > 0, and
`bar_age` shows real ages for `1m`/`5m`/`15m` (NOT `none`).

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
| reconnect | `journalctl --user -u vl-friend@tom --no-pager \| grep "hello handshake OK"` (fresh line); `grep -c "connect_key refused"` still `0` |

**PASS:** unit inactive within 10s; `state: revoked`; tailscale device removed;
after rotate, a fresh `hello handshake OK` and refusal count `0`.

Source: `api/handler_friends.go:459` (revoke → `remove.sh` + tailscale device
delete), `deploy/friends/remove.sh:53` (`systemctl --user stop/disable` — moves
the folder to trash, never deletes).
