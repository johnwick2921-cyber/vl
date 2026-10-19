# B8 friend revoke/recovery — minimal follow-up design (DS-104)

Status: **PROPOSAL** — for the CTO to assign as a separate dispatch. **#39 stays
updater-only.** No live changes.

## Problem (reproduced 2026-10-19, isolated temp fixture)

1. **Recoverability.** `revoke` runs `deploy/friends/remove.sh`, which MOVES the
   whole friend folder (`/home/hoang/vl-friends/<name>`: `.env` + `data/`) to
   trash and disables the unit. After that `resume` (`systemctl --user start`
   on a gone `WorkingDirectory`) and `rotate` (`setFriendEnvKey` reads the moved
   `.env` → ENOENT) both fail. B8 step 5 ("revoke → resume/new key → reconnect")
   is therefore not achievable.
2. **Device enrollment.** `tailscale_device_id` is written empty at provision
   (`deploy/friends/provision.sh:151`) and never populated, so revoke's Tailscale
   device delete always returns `no_device_id` (surfaced, not silent) and the
   device is never actually removed from the tailnet.

## Security invariants (non-negotiable)

- **I-1 Archive provenance.** When revoke moves a folder, it must persist the
  EXACT archive path + timestamp + actor into the registry — never a guessed
  trash path (trash paths can collide or change).
- **I-2 Fresh key before enable.** Re-enabling a revoked friend must mint a NEW
  connect key (`rotate`) and update `key_sha256`; the old key is never restored
  (a revoked client must not be reauthorized by its old key).
- **I-3 Verified device association.** The Tailscale device ID must be associated
  with the friend through an authenticated mechanism — NOT hostname matching
  (hostname is not an authenticated identity). The friend app (`cmd/vl-friend`,
  which runs `tailscale up` and owns its device) reports its device ID to the
  owner's bot over the authenticated friend channel; the owner stores it.
- **I-4 Sequence pins.** Production-path tests pin the exact lifecycle sequence
  end-to-end (see below). No invariant is satisfied by an absent-error check.

## Minimal scope

Only the friend lifecycle path:
- `deploy/friends/remove.sh` — write archive provenance (I-1).
- `api/handler_friends.go` — `resume`/`rotate`/`revoke` honor I-1/I-2/I-3.
- `cmd/vl-friend` — report its own tailscale device ID (I-3).
- Registry `friends.json` — add the archive-provenance and device-association
  fields.

NOT in scope: the TCP wire format, the connect-key check (S4/S5/S6), the updater
(#39), the owner setup, or any owner-service change.

## Proposed ownership (dispatch lanes)

- **DS-101** (`deploy/friends/*.sh`): archive provenance + registry writes; pins.
- **DS-106** (`api/handler_friends.go`): resume/rotate/revoke recovery logic;
  fresh-key + verified-device gates; pins.
- **DS-108** (`cmd/vl-friend`): authenticated device-ID report; pins.
- Reviewer pairs per d26 (101↔102, 105↔106, 107↔108) — CTO to confirm.

## Sequence pins (tests to write)

1. **Revoke provenance**: after `remove.sh`, `friends.json` records
   `archive_path` (the exact moved path), `archived_at`, and state `revoked`.
2. **Resume refuses without recovery**: `resume` on a revoked friend with no
   fresh key and no restored folder → refused, no unit start.
3. **Rotate mints a fresh key**: `rotate` never reuses the old key;
   `key_sha256` changes; the new key is written to the restored `.env` only.
4. **Device association is authenticated**: the device ID is stored only from
   the friend app's authenticated report; a forged/hostname-guessed ID is
   rejected.
5. **Revoke deletes the verified device**: with a stored device ID, revoke calls
   the Tailscale delete for THAT id; with none, it returns `no_device_id`
   (surfaced) and the owner sees it.

## Open questions for the CTO / owner

- Is "revoke then resume" a real UX, or should revoke be terminal (re-Add the
  friend) with `pause/resume` as the only reversible path? The design should
  implement whichever the owner chooses, pinned by the sequence tests.
- Where exactly should the friend app read its device ID (e.g.
  `tailscale status --json` on the friend PC after `tailscale up`)?
