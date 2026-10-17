# NOFX — Install from a fresh clone (any machine)

Nothing in this repo is machine-specific: no hardcoded usernames, home paths,
computer names, distro names, or node paths. Everything machine-dependent is
either in your local `.env` (never committed) or auto-detected at install time.

## Required packages

| What | Version | Notes |
|---|---|---|
| WSL2 + a Linux distro (or native Linux) | Ubuntu 22.04/24.04 tested | Windows: WSL2 with **systemd enabled** for autostart (`/etc/wsl.conf` → `[boot] systemd=true`) |
| Go | ≥ 1.25 (see `go.mod`) | https://go.dev/dl/ |
| Node.js + npm | LTS (18+; 22 tested) | nvm is fine — the autostart installer resolves nvm paths itself |
| git | any recent | |
| NinjaTrader 8 (Windows side) | 8.x | with your data/broker connection (SIM by default) |
| sqlite3 CLI (optional) | any | only for poking at `data/data.db` manually |

## Steps

```bash
# 1. Clone and enter
git clone <repo-url> nofx && cd nofx

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
go build -o nofx-bin .

# 4. Build the frontend
cd web && npm install && npm run build && cd ..
#    (development: `npm run dev` serves :3000 and proxies /api → :8080)

# 5. First run — creates a fresh SQLite DB at data/data.db automatically
./nofx-bin
```

## NinjaTrader 8 AddOn (Windows side — required for live bars + execution)

NT8 compiles NinjaScript **only** from its own Documents folder, never from
this repo:

1. Copy ALL of `ninjascript/*.cs` →
   `C:\Users\<you>\Documents\NinjaTrader 8\bin\Custom\AddOns\`
2. In NT8 press **F5** (compile).
3. Do ONE full NT8 restart (AddOns do not hot-reload).

The AddOn connects to the bot at `127.0.0.1:36974` and retries every 5s —
start order never matters (the bot is the TCP server and waits). On Windows
11 22H2+, set WSL2 to **mirrored networking** so `127.0.0.1` crosses the
WSL/Windows boundary (plain NAT needs firewall rules + host-IP discovery).

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
