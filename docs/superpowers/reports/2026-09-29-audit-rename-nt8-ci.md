# 2026-09-29 — AUDIT-RENAME-NT8-CI (DS-108, READ-ONLY)

**Slice:** NT8 AddOn + wire + CI (R3, R1b item 2 wire, CI workflows).
**Plan audited:** `/home/hoang/rename-vl-plan/2026-09-29-rename-nofx-to-vl-plan.md`, md5 `b16759336fe0e264d11c1bc885ce5192` (matches dispatch).
**Branch:** `audit/rename-nt8-ci` @ `397dbfc85ca714cba5dc7761b6571a914ba206b9` · base `origin/dev` = `9d52f5dc611ed001fc057bca613685071d77efab` (RELEASE boot 7, 2026-09-28T01:01:07-05:00, `git log -1 --format='%H %cI' origin/dev`).
**Mode:** L-AUDIT — no code change; the only file written is this report. No lock, no DB, no live-box action.

## Verdict

The plan's three core claims for this slice HOLD at the cited lines, verified [A]. No P0.
Three P1s, two P2s, three P3s — all are fixes to the plan/dispatch TEXT, none invalidate the
design. The highest-risk item is the account.txt dual-read precedence (P1-1): it is the only
spot in this slice where the rename can silently change WHICH account the AddOn trades.

## Direct answers to the dispatch's CHECK items

### 1. AddOn strings + the hello-`source` claim + account.txt both-exist behavior

- **Full `nofx`/`Nofx` inventory in `ninjascript/`** [A] (`grep -rniE 'nofx' ninjascript/`):
  - `VLTraderTCPClient.cs:429` (comment) and `:440` (`Path.Combine(…, "NofxTrader", "account.txt")`) — the plan's "~429,440" is exact.
  - `vltrader_tcp_README.md:38` and `:52` — `./nofx-bin` / `NT_TRANSPORT=tcp` examples.
  - `vltrader_tcp_PROTOCOL.md:150` (`"nofx-go"` in the allowed `source` list) and `:574` (pinned byte literal).
  - Nothing else. No log text, no source names, no other paths.
- **The AddOn IGNORES the Go hello `source` — CONFIRMED** [A]. The hello branch
  (`VLTraderTCPClient.cs` ~747-765) reads only `protocol_version` and compares it to
  `PROTOCOL_VERSION`; `PROTOCOL.md:150` says `source` is "diagnostics only". The Go side
  replies `Source: "nofx-go"` (`provider/ninjatrader/tcp_server.go:2027`) and logs the AddOn's
  `source` only as a log field (`tcp_server.go:2023`). Changing the Go reply value to `vl-go`
  cannot change AddOn behavior. **No `ProtocolVersion` bump is needed — CONFIRMED**, with the
  wording caveat in P2-2.
- **Both `VLTrader\account.txt` and `NofxTrader\account.txt` exist/differ:** today impossible
  (single path, `VLTraderTCPClient.cs:440`). Under the plan's dual read, first-match wins and
  VL shadows Nofx — the plan does not say what happens on disagreement. See P1-1.

### 2. VL_BUILD_ID bump — every Go reader

- `provider/ninjatrader/order_snapshot.go:234` — `const ExpectedAddonBuild = "2026-09-23-m21"`:
  must move in the SAME commit as the .cs bump, or `trader/wave_b_addon_slot_test.go:128-141`
  (`TestAddonBuildIDMovesInLockstep` reads `VL_BUILD_ID` from the .cs and asserts equality with
  `ExpectedAddonBuild` and `>= MinAddonBuild*`) fails CI, and every boot line prints match=NO
  (`trader/class33_boot_sweep.go:140`, `AddonBuildLine(farSideBuildID, ExpectedAddonBuild)`).
  The plan's item 8 does not name this file. See P1-3.
- Capability floors `MinAddonBuildStopSlot` / `MinAddonBuildProtectiveStop` / `MinAddonBuildPictureHtf`
  compare BYTEWISE (`vltrader_tcp_PROTOCOL.md:577-578`: "keeps its ISO-date prefix, because the
  capability floors compare it bytewise"). "a new dated id" satisfies this; any non-ISO-tag
  format breaks `FarSideProven`. Item 8 should pin the prefix.
- Updater gate: `internal/updaterworker/nt8.go:22-24,68-69` — `nt8_skipped` requires
  `addon_ack.build_id == the SIGNED manifest's addon.build_id`. The manifest auto-extracts the
  id from the source (`deploy/release/manifest.sh:46`: `grep -hoE 'VL_BUILD_ID…' "$STAGE"/ninjascript/*.cs | head -1`).
  So the bump does NOT break a gate — it converts the next update to the PARK path
  (`nt8_updated`) until the owner's F5 compile, which the R2/D4 owner steps already include.
  Consequence should be written down. See P2-1.
- Boot lines: `main.go:290-300` — the snapshot build id is READ from received frames, never the
  constant; it says match=NO loudly until the owner reloads. Correct by design; the bump rides
  it.

### 3. CI workflows

- Full inventory [A] (`grep -rniE 'nofx' .github/workflows/`): `pr-docker-check.yml:55,73,92,130,146`
  (image tags `nofx-*:pr-test`), `docker-build.yml:95-96,164,192` (GHCR/DockerHub image paths
  `…/nofx-$suffix`), `pr-docker-compose-healthcheck.yml:38-40,91,99,102,112,120,123,156,161,174,177`
  (container names + `NOFX_BACKEND_PORT/NOFX_FRONTEND_PORT/NOFX_TIMEZONE`),
  `pr-checks.yml:214` (`go build -v -o nofx`), `release.yml:39` (`RELEASE_REPO: johnwick2921-cyber/nofx`)
  and `:134` (`-o nofx-bin`), `pr-checks-comment.yml:259-260` (upstream NoFxAiOS links — allow-listed).
- R1b names `release.yml` only (items 1/2/7). The GHCR image paths in `docker-build.yml` break
  for real after the R4 repo rename (registry paths do not redirect), and the compose env names
  are simply left stale. See P1-2.
- **Nothing goes green on empty on this rename:** `test.yml` runs `go test -timeout 30m ./...`
  (backend) + vitest (frontend), which run the branding `scope_test.go` and the census guard
  once it exists; security/secrets jobs are content scans (Trivy fs, TruffleHog) unaffected by
  naming [A]. The census guard is a Go test → picked up by `go test ./...` automatically.
- CORS: `api/server.go` `Access-Control-Allow-Headers: "Content-Type, Authorization"` — neither
  update header is allowed today; after R1b item 3 the pin test must keep excluding both. Plan
  already says this.

### 4. One-order reconnect check and double-entry dedupe

- **Not touched by the rename's logic changes.** The reconnect check is
  `provider/ninjatrader/attempted_guard.go` (post-reconnect FRESH broker truth before flushing an
  attempted ENTRY frame); the dedupe is `trader/ninjatrader/replay_hold.go:67-74` (merge dedup)
  and `trader/ninjatrader/tcp_trader.go:364-369` (`duplicate_ignored` for `signal_id`). None of
  them read `source`, `VL_BUILD_ID`, env names, or the module path.
- The rename touches these files ONLY as `"nofx/…"` → `"vl/…"` import rewrites
  (`attempted_guard.go:12`, `replay_hold.go:9-10`) and the telemetry metric renames (item 2).
- **One behavioral adjacency, and it is the P1-1:** the account.txt dual-read decides WHICH
  account the AddOn resolves. Reconcile/one-order logic keys on `(account, symbol, side)`; a
  silently different `account` on the wire is not "a rename" to those paths — it is a trading
  behavior change. Precedence must be loud.

### 5. Wording ambiguities (D2 item 2 and item 8) — see P2-2 and P1-1.

## Findings (ranked)

### P1-1 — account.txt: "copy it across once, log which" has no agent; both-exist/differ is undefined
- **Where:** plan R1b item 8 ("read `%USERPROFILE%\VLTrader\account.txt`, else `NofxTrader\account.txt` (copy it across once, log which)"). Current code `VLTraderTCPClient.cs:440` [A].
- **Why it matters:** (a) "copy it across once" is not in R2's job list (D3 steps 0-6) and not in the Owner steps — nobody owns it, so it silently never happens (harmless only because the fallback keeps working) or someone hand-copies and later edits diverge. (b) If BOTH files exist and differ, first-match VL wins silently → the AddOn trades an account the operator did not intend; reconcile/dedupe keyed on `(account, symbol, side)` would then "discover" a different book. (c) An empty VL file currently degrades to default (preferred=null → Sim101) — a copied EMPTY file would mask the real NofxTrader content.
- **Exact fix to the plan text:** item 8 becomes: "Read `VLTrader\account.txt`; if absent read `NofxTrader\account.txt`. NEVER copy in the AddOn (a stale copy would silently shadow later operator edits). When BOTH exist, `LogWarn` naming both values AND which one won — disagreement is a WARN, not a preference. The one-shot copy is an OWNER step at the R2 boot (add to the Owner-steps list). Empty file = absent, as today (`VLTraderTCPClient.cs:442-443`)."

### P1-2 — CI image/registry/compose names are not in the rename inventory
- **Where:** `.github/workflows/docker-build.yml:95-96,164,192` (GHCR `…/nofx-$suffix`, DockerHub `$USER/nofx-$suffix`); `pr-docker-check.yml` tags; `docker-compose*.yml:11,18,34,42,47` (`${NOFX_BACKEND_PORT…}`, `${NOFX_FRONTEND_PORT…}`) + `pr-docker-compose-healthcheck.yml:38-40` env [A]. R1b items 1/2/7 list none of them.
- **Why it matters:** GHCR registry paths do NOT follow repo renames — after R4 the publish job pushes to a path that no longer matches the repo, and pulls elsewhere break. The compose env vars stay stale `NOFX_*`, which the new census guard (item 10) will flag unless someone decides.
- **Exact fix:** add to item 1/7: "`.github/workflows/docker-build.yml`, `pr-docker-check.yml` image names → `vl-*`; `docker-compose*.yml` + `pr-docker-compose-healthcheck.yml` env → `${VL_X:-${NOFX_X:-<default>}}` (R1a-style dual form) OR an explicit census allow-list entry with the dated reason. Silence is not an option."

### P1-3 — `ExpectedAddonBuild` lockstep not named in item 8
- **Where:** plan R1b item 8 says "bump VL_BUILD_ID to a new dated id; re-pin the .cs hash". `provider/ninjatrader/order_snapshot.go:234` and `trader/wave_b_addon_slot_test.go:128-141` [A].
- **Why it matters:** not a silent break — the lockstep test fails CI and boot lines print match=NO — but a lane reading only the plan will land the .cs bump, see red, and scramble. The bytewise floors also require the ISO-date prefix (`PROTOCOL.md:577-578`).
- **Exact fix:** item 8 adds: "same commit: `ExpectedAddonBuild` (`provider/ninjatrader/order_snapshot.go:234`) moves to the new id; keep the ISO-date prefix (`2026-…-…-<tag>`) — `FarSideProven` floors compare bytewise; `manifest.sh:46` auto-propagates the id into the signed manifest, so no manifest edit."

### P2-1 — the bump's updater consequence is unstated
- **Where:** `internal/updaterworker/nt8.go:22-24,68-69`; `deploy/release/manifest.sh:46` [A].
- **Why it matters:** after the bump, until the owner's F5 compile, `ack.build_id != manifest.addon.build_id` → `nt8_updated` → the next update PARKS for attended F5. Correct behavior; if unstated it will be filed as an updater bug during the cutover.
- **Exact fix:** D2 "Gates"/PR-body note + R2 runbook: "the first update after the VL_BUILD_ID bump parks (`nt8_updated`) until the owner's F5+restart — expected, not a failure."

### P2-2 — "no protocol bump" is scoped wrong; the literal-site list is incomplete
- **Where:** R1b item 2 wire sentence; literal sites `tcp_server.go:2027`, `tcp_framing.go:118` (comment documents both values), `PROTOCOL.md:150` + `:574` (pinned bytes), `TestGoHelloReplyIsByteIdentical` [A].
- **Why it matters:** "protocol bump" can be misread as "don't touch anything protocol-ish" — but VL_BUILD_ID and `source_hash` DO change (correctly). And changing the Go reply bytes without updating the pin sites breaks the pin test. One subtle trap: the AddOn's OWN `source` value `"vltrader-addon"` must NOT change (identity string; `tcp_framing.go:118` documents the pair).
- **Exact fix:** item 2 becomes: "NT8 hello reply `Source: "nofx-go"` → `"vl-go"` — NO `ProtocolVersion` change. Literal sites: `provider/ninjatrader/tcp_server.go:2027`, the comment at `provider/ninjatrader/tcp_framing.go:118`, `vltrader_tcp_PROTOCOL.md:150` and the pinned bytes at `:574`, `TestGoHelloReplyIsByteIdentical`. Do NOT change the AddOn's `source` (`"vltrader-addon"`)."

### P3-1 — `manifest.sh` extracts the id with `head -1` over the whole glob
- **Where:** `deploy/release/manifest.sh:46` [A].
- **Why it matters:** a second `VL_BUILD_ID` literal in any file alphabetically before `VLTraderTCPClient.cs` silently wins the manifest. Today only one exists.
- **Exact fix:** pin the grep to `ninjascript/VLTraderTCPClient.cs` in the same wave.

### P3-2 — `ninjascript` docs hold `nofx` strings the census guard will flag
- **Where:** `vltrader_tcp_README.md:38,52` (`./nofx-bin`, `NT_TRANSPORT=tcp`) [A].
- **Why it matters:** item 8 only re-pins the .cs hash; the census guard (item 10) has no allow-list entry for these and they are not "dated history docs".
- **Exact fix:** fold the two README examples into item 9's docs sweep, or add a dated allow-list entry.

### P3-3 — fixture names a nonexistent file
- **Where:** `internal/updaterworker/releasefixture/releasefixture.go:167` emits `"ninjascript/VLTraderTcp.cs"` while the real file is `VLTraderTCPClient.cs` [A].
- **Why it matters:** fixture-only today, but a lane touching AddOn fixtures during the rename will chase a filename that never existed.
- **Exact fix:** rename the fixture path when item 8 touches ninjascript fixtures.

## What this audit did NOT do
- No code changed anywhere; only this report file.
- No race run: one `go test -race` slot is held by another lane (L16) at audit time; build+vet green at the audit head — `go build ./...` RC=0, `go vet ./...` RC=0. Race noted as not-run-here and deferred to the dispatch's own gates.
- No DB read, no live-box action, no lock, no deploy.

## Evidence base
`git log -1 --format='%H %cI' origin/dev` = `9d52f5dc611ed001fc057bca613685071d77efab 2026-09-28T01:01:07-05:00` (the only spec consulted besides the dispatch text; plan md5 `b16759336fe0e264d11c1bc885ce5192`).
