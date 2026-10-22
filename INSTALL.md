# VL — Install from a fresh clone (any machine)

Nothing in this repo is machine-specific: no hardcoded usernames, home paths,
computer names, distro names, or node paths. Everything machine-dependent is
either in your local `.env` (never committed) or auto-detected at install time.

## Required packages

| What | Version | Notes |
|---|---|---|
| WSL2 + a Linux distro (or native Linux) | Ubuntu 22.04/24.04 tested | Windows: WSL2 with **systemd enabled** for autostart (`/etc/wsl.conf` → `[boot] systemd=true`) |
| Go | ≥ 1.26.10 (see `go.mod`) | https://go.dev/dl/ |
| Node.js + npm | LTS (18+; 22 tested) | nvm is fine — the autostart installer resolves nvm paths itself |
| git | any recent | |
| NinjaTrader 8 (Windows side) | 8.x | with your data/broker connection (SIM by default) |
| sqlite3 CLI (optional) | any | only for poking at `data/data.db` manually |

## Steps

```bash
# 1. Clone and enter
git clone <repo-url> vl && cd vl

# 2. Config — copy the template and fill it in
cp .env.example .env
#    REQUIRED for NinjaTrader trading:
#      NT_TRANSPORT=tcp          (already in .env.example — do NOT remove;
#                                 without it the bot falls back to the
#                                 deprecated CSV bridge and you get no live bars)
#    REQUIRED before any non-localhost deploy:
#      JWT_SECRET=$(openssl rand -base64 64)
#    Fill in your AI provider key(s) via the Settings UI after first start,
#    or pre-seed exchange/AI keys per .env.example comments.

# 3. Build the backend
go build -o vl-bin .

# 4. Build the frontend
cd web && npm install && VITE_GUIDE_BUILT_REV=$(git rev-parse HEAD) npm run build && cd ..
#    (development: `npm run dev` serves :3000 and proxies /api → :8080)

# 5. First run — creates a fresh SQLite DB at data/data.db automatically
./vl-bin
```

## NinjaTrader 8 AddOn (Windows side — required for live bars + execution)

The AddOn ships as a **compiled DLL** (`VL.AddOn.dll`), not as loose `.cs` files
you F5 into NT8. The one-button update builds and places it automatically
(`internal/addonbuild` via `build-addon.ps1`), using NT8's bundled Roslyn
compiler — no .NET SDK, no per-release F5.

### Automatic (the one-button update)

Nothing to do by hand: the updater compiles the release's `ninjascript/*.cs`
into `VL.AddOn.dll` and places it under
`Documents\NinjaTrader 8\bin\Custom\`. **One-time migration only** if this box
previously used the old copy-`.cs`-then-F5 method: the updater refuses with

> One-time step: open NinjaTrader → NinjaScript Editor → press F5 once, then
> click Update again.

It has already MOVEd the old `VL*.cs` out of `bin\Custom\AddOns\` (never
deleted), so that one F5 recompiles `NinjaTrader.Custom.dll` WITHOUT the old VL
classes — otherwise NT8 would load the AddOn twice. After that single F5 the
update proceeds and every later release is DLL-only.

### Manual (no updater)

Compile with the repo's script — it FAILS FAST if `-CustomDir` is missing,
because without it the build runs with no NT8 references and fails with
hundreds of "type not found" errors (always pass all three):

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File `
  <repo>\deploy\addon\build-addon.ps1 `
  -Src <repo>\ninjascript `
  -Out <repo>\build\addon `
  -CustomDir "C:\Users\<you>\Documents\NinjaTrader 8\bin\Custom"
```

Then move the produced `VL.AddOn.dll` into
`C:\Users\<you>\Documents\NinjaTrader 8\bin\Custom\` and fully restart NT8
(AddOns do not hot-reload). If this is a box that previously used the
copy-`.cs`-then-F5 method, do the one-time migration FIRST: move every
`VL*.cs` out of `bin\Custom\AddOns\` (to a backup folder, not deleted), press
**F5 once** so `NinjaTrader.Custom.dll` drops the old VL classes, then place the
DLL.

### OneDrive-redirected Documents

Windows can redirect `Documents` into OneDrive. The detector accepts EITHER
location and refuses if it finds more than one:

- `C:\Users\<you>\Documents\NinjaTrader 8\bin\Custom`
- `C:\Users\<you>\OneDrive\...\Documents\NinjaTrader 8\bin\Custom`

Set `VL_NT8_DOCS_DIR=/mnt/c/Users/<you>/Documents/NinjaTrader 8` (WSL path) in
`~/.config/vl-updater/env` to pin the exact folder when auto-detection is
ambiguous.

The AddOn connects to the bot at `127.0.0.1:36974` and retries every 5s —
start order never matters (the bot is the TCP server and waits). On Windows
11 22H2+, set WSL2 to **mirrored networking** so `127.0.0.1` crosses the
WSL/Windows boundary (plain NAT needs firewall rules + host-IP discovery).

## Partner layout facts

For the partner box (`vlautoagenttraderv1`):

- The install dir may be `~/vlautoagenttraderv1`; a `~/vl` symlink to it is
  supported (the setup script compares `realpath`s, so the symlink and the real
  path are treated as the same install).
- Boxes without gcc build with `-tags cgofree` (the sqlite driver's pure-Go
  backend). The one-click Update installs the **prebuilt** release binary, so
  gcc is never required for updates.
- The legacy binary name `nofx-bin` is gone — move any stale `nofx-bin` out of
  the tree (it is git-ignored, not built).

## deploy/RELEASE is tracked (build-THEN-stamp)

`deploy/RELEASE` is a tracked file whose single line names the booted release.
It is stamped **after** the binary is built: `go build` bakes `vcs.modified`
from git status at build time, so the binary records `vcs.modified=false` even
though the tree afterwards shows ` M deploy/RELEASE` from the stamp. That
post-build dirty line is **by design** — never "clean it up" with
`git checkout -- deploy/RELEASE`, or the next release's recorded boot revision
is wrong.

## Optional: auto-start on reboot

```bash
sudo bash deploy/install-autostart.sh
```

Detects your user, clone path, and node location automatically (templates in
`deploy/` contain only placeholders). Windows-side steps — Task Scheduler
using YOUR distro name from `wsl.exe -l -v`, plus optional NT8 startup — are
in [docs/AUTOSTART.md](docs/AUTOSTART.md).

## Verify

```bash
ss -tlnp | grep -E ':(8080|3000|36974)'   # API, UI, NT8 bridge listening
# UI at http://localhost:3000 — create your AI model + exchange + trader.
# With NT8 open + AddOn deployed: bot log shows "hello handshake OK" and bars.
```

SIM-only by default: live/funded accounts are blocked by the risk gate unless
explicitly allow-listed.
