# B8 friend revoke/recovery — minimal follow-up design (DS-104)

Status: **PROPOSAL** — for the CTO to assign as a separate dispatch. **#39 stays
updater-only.** No live changes. DS-108's artifact/signature availability report
is the current B8 priority; this follow-up comes after it.

## Ruling (CTO 2026-10-19 08:08 — resolved)

- **Revoke is TERMINAL** for the old credentials/instance. The archived folder
  and its provenance are preserved — never overwritten, deleted, or reused with
  ambiguity.
- **Recovery = explicit Re-add** (the Add flow) with a FRESH key + install code.
  The old key is never restored.
- **Pause/Resume is the reversible path** (unchanged).
- **Verified device association** needs more than a client self-reported ID: the
  owner's bot cross-checks through the OWNER-controlled Tailscale API against the
  authenticated connection/source and the expected unique friend tag, and refuses
  ambiguous or mismatched identities. A client-supplied arbitrary ID is never
  deleted; hostname is never guessed.

## Problem (reproduced 2026-10-19, isolated temp fixture)

1. **Recoverability mis-specified.** `revoke` runs `deploy/friends/remove.sh`,
   which MOVES the whole friend folder (`/home/hoang/vl-friends/<name>`:
   `.env` + `data/`) to trash and disables the unit, so the old B8 step 5
   ("revoke → resume/new key → reconnect") is impossible: `resume` starts a gone
   `WorkingDirectory` and `rotate` reads the moved `.env` → ENOENT. The ruling
   resolves this by making revoke terminal and recovery an explicit Re-add.
2. **Device enrollment.** `tailscale_device_id` is written empty at provision
   (`deploy/friends/provision.sh:151`) and never populated, so revoke's Tailscale
   device delete always returns `no_device_id` (surfaced, not silent) and the
   device is never actually removed from the tailnet.

## Security invariants (non-negotiable)

- **I-1 Archive provenance.** When revoke moves a folder, it must persist the
  EXACT archive path + timestamp + actor into the registry — never a guessed
  trash path (trash paths can collide or change). The archived data is preserved;
  no overwrite, delete, or reuse ambiguity.
- **I-2 Fresh key on Re-add.** Recovery is a fresh Add: a NEW connect key +
  install code is minted and `key_sha256` updated. The old key is never restored
  (a revoked client must not be reauthorized by its old key).
- **I-3 Verified device association (owner-side cross-check).** The friend's
  Tailscale device is resolved through the OWNER-controlled Tailscale API,
  cross-checked against the authenticated connection/source and the expected
  unique friend tag (`tag:friend-<name>`). Ambiguous or mismatched identities are
  refused. A client self-reported ID is never trusted alone, an arbitrary
  client-supplied ID is never deleted, and hostname is never guessed.
- **I-4 Sequence pins.** Production-path tests pin the exact lifecycle sequence
  end-to-end (see below). No invariant is satisfied by an absent-error check.

## Minimal scope

Only the friend lifecycle path:
- `deploy/friends/remove.sh` — write archive provenance (I-1).
- `deploy/friends/provision.sh` — Re-add produces a fresh key/code (I-2).
- `api/handler_friends.go` — `resume`/`rotate` refuse a `revoked` friend (no
  restore path); `revoke` deletes only a VERIFIED device ID via the owner's
  Tailscale API cross-check (I-3).
- Registry `friends.json` — archive-provenance + device-association fields.

NOT in scope: the TCP wire format, the connect-key check (S4/S5/S6), the updater
(#39), the owner setup, or any owner-service change.

## Proposed ownership (dispatch lanes)

- **DS-101** (`deploy/friends/*.sh`): archive provenance + Re-add fresh-key/code;
  pins.
- **DS-106** (`api/handler_friends.go`): `resume`/`rotate` guard on `revoked`;
  owner-side Tailscale API device cross-check + verified delete; pins.
- **DS-108** (`cmd/vl-friend`): the friend side of the authenticated association
  (after its artifact report). Pins.
- Reviewer pairs per d26 (101↔102, 105↔106, 107↔108) — CTO to confirm.

## Sequence pins (tests to write)

1. **Provision → revoke → re-add**: provision creates `tom` (key A); revoke
   archives the folder + records provenance and state `revoked`; re-add mints
   key B (`key_sha256` changes), a NEW install code, and the friend reconnects
   with key B.
2. **Rejected old credentials**: after revoke + re-add, a hello carrying key A is
   REFUSED (`connect_key refused`); key B is accepted.
3. **Resume/rotate refuse revoked**: `resume` and `rotate` on a `revoked` friend
   return a clear error (no folder restore, no old-key write).
4. **Device cross-check refuses ambiguity**: a self-reported or hostname-guessed
   ID is NOT accepted; the owner-side Tailscale API must confirm the friend tag +
   authenticated source before the ID is stored.
5. **Revoke deletes only the verified device**: revoke deletes exactly the
   cross-checked device ID; with none, it returns `no_device_id` (surfaced) and
   the owner sees it.

## Open questions for the CTO / owner

- Which Tailscale API fields authenticate the source (device owner/ACL tags) for
  the I-3 cross-check — `tailnet` device `tags` + `authorized` + the friend's
  ACL-assigned `tag:friend-<name>`? DS-106 to pin the exact fields.
- Where the friend app reads its own device ID (e.g. `tailscale status --json`
  after `tailscale up`) — only as ONE input to the owner-side cross-check, never
  trusted alone.
