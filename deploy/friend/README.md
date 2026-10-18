# Friend app + installer build (S8, DS-108)

Builds the two artifacts a friend's PC receives, and the signed manifest the
friend app verifies:

- `vl-friend.exe` — the friend app (cross-compiled Go; first run is the install
  wizard, every later start checks for updates). Also shipped as `VL-Setup.exe`.
- `VL.AddOn.dll` — the add-on, compiled by NinjaTrader's own compiler and then
  **obfuscated** (Obfuscar: rename symbols + encrypt strings). No `.cs` file ever
  reaches a friend (B10.1).
- `manifest.json` + `manifest.json.sig` — signed with the friend-distribution
  key (`/home/hoang/.vl-friend-dist/key`, namespace `release`). The public half
  is embedded in `vl-friend.exe` (`cmd/vl-friend/friend_pubkey.go`).

## One-time setup (already done by DS-108 on 2026-10-18)

1. Friend-distribution key:
   `ssh-keygen -t ed25519 -f /home/hoang/.vl-friend-dist/key -N '' -C vl-friend-dist`
   (dir 0700, key 0600, private NEVER in git). The embedded public key is
   `cmd/vl-friend/friend_pubkey.go`.
2. Obfuscar on Windows: `dotnet tool install --global Obfuscar.GlobalTool`
   (or drop `Obfuscar.Console.exe` somewhere on PATH).

## Build a friend release

On the box (WSL), from the repo root:

```bash
BUILD=37 APP_VERSION=1.1.0 bash deploy/friend/build.sh
```

- `BUILD` defaults to a numeric timestamp; the app compares it numerically.
- The output lands in `/home/hoang/vl-friends/_dist/<build>/` and `_dist/LATEST`
  is rewritten to point at it (old builds persist — never deleted).
- The friend's own bot serves it at `/api/friend/manifest`, `/api/friend/manifest.sig`
  and `/api/friend/artifact/{addon,app}` (friend mode only; 404 on the owner's bot).

## What the friend's PC gets (B8 real test)

1. `VL-Setup.exe` (a copy of `vl-friend.exe`) → paste the install code.
2. The app: Tailscale → `connect.json` → download + verify + place `VL.AddOn.dll`
   into `Documents\NinjaTrader 8\bin\Custom` → the VL desktop icon.
3. Every start: signed manifest check → "VL update available — Update now / Later".
   Update now → verify → wait until flat → move the old DLL to `VL\old\` → "Updated ✓".

## Safety

- The app verifies the manifest SIGNATURE and every artifact's SHA-256 **before**
  installing anything; a tamper refuses and changes nothing (B5 S9).
- The old DLL is MOVED to `VL\old\` before a swap — never deleted (THE ONE RULE).
- The install code is shown once and never stored; `ts_authkey` is used once and
  not saved.
- SmartScreen will warn (unsigned exe); note it in the friend guide. A
  code-signing certificate is an optional later step.
