names rewritten to vl on 2026-09-30 (VL rename)
# STARTUP — vl NQ Futures Bot

Operator runbook for cold-starting the bot against a live NT8 host. Terse, copy-paste-ready. See plan doc Task 30 for source.

## 1. Pre-flight checks

Before launching, verify every item below. Each unchecked item is a likely incident.

**Environment variables** (must all be set in `.env` at repo root):

```bash
grep -E "^(JWT_SECRET|DATABENTO_API_KEY|NINJATRADER_DATA_DIR)=" .env
```

Expected output: 4 non-empty lines. If `JWT_SECRET` is missing the bot will boot with the insecure default `default-jwt-secret-change-in-production` (config/config.go:86); regenerate with:

```bash
openssl rand -base64 64
```

**NT8 host (Windows side)**:

- NinjaTrader 8 running and connected (Control Center → green dot)
- `vltrader.cs` strategy compiled (NinjaScript Editor → F5; renamed from `claudetrader.cs` on 2026-05-23)
- VLTrader attached to active MNQ chart (e.g. `MNQH6` for March 2026 contract)
- WSL2 mirrored networking active on the host: `wsl --version` must report mode = `mirrored` (Win11 22H2+ required for `127.0.0.1` to reach Windows-side NT8)

**Trading mode set to futures** (if NQ path desired):

```bash
grep TRADING_MODE .env   # must be ABSENT (futures-only build; any value is ignored with a boot warning)
```

## 2. Cold start

```bash
./vl-bin > /tmp/vl.log 2>&1 &
```

Verify port 8080 is listening:

```bash
ss -tlnp | grep :8080
```

Verify startup log:

```bash
grep "System started successfully" /tmp/vl.log
```

Expected output: a single line containing `✅ System started successfully`. Absence means crash — `tail -200 /tmp/vl.log` for the panic.

## 3. Verify trader loads

```bash
curl -s localhost:8080/api/traders | jq '.[] | {id, exchange, symbol}'
```

Expected: at least one entry with `exchange: "ninjatrader"`. Cross-check the log:

```bash
grep "Loading trader" /tmp/vl.log
```

Expected line shape: `📦 Loading trader <id> (AI Model: <model>, Exchange: ninjatrader/...)`.

## 4. First-trade smoke test

Run the end-to-end NQ smoke (Databento → indicators → prompt → CSV signal → NT fill):

```bash
DATABENTO_API_KEY=$KEY \
NINJATRADER_DATA_DIR=/mnt/c/Users/<u>/VLTrader/data \
go run ./cmd/nq_smoke
```

While that runs, in a separate terminal tail the NT-side fills file:

```bash
tail -f /mnt/c/Users/<u>/VLTrader/data/trades_taken.csv
```

A successful smoke ends with a 3-field row appearing in `trades_taken.csv` (DateTime,Direction,Entry_Price). NT must be live and VLTrader attached for the fill to land.

## 5. Shutdown

```bash
pkill -TERM -f vl-bin
```

Verify no lingering file handles into the NT data dir:

```bash
lsof | grep VLTrader
```

Expected: empty. Any remaining handles indicate a stuck goroutine — investigate before restarting.

## 6. Windows Defender exclusion (one-time per host)

Defender's real-time scanner can transiently lock files during `os.Rename`, breaking the atomic temp+rename pattern in `provider/ninjatrader/csv_writer.go` and surfacing as `rename: Access is denied` in the Go log. The errors are recoverable (next write succeeds) but noisy.

From an **Administrator** PowerShell prompt on the Windows host:

```powershell
Add-MpPreference -ExclusionPath "C:\Users\<user>\VLTrader\data"
```

Verify the exclusion is active:

```powershell
Get-MpPreference | Select-Object -ExpandProperty ExclusionPath
```

The output list should include the data dir. Why this matters: `csv_writer.go` writes a temp file and then `os.Rename`s it to `trade_signals.csv`. If Defender holds the file at the moment of rename, NT may briefly see no signal or a partial one. The exclusion eliminates that class of failure.

## Cross-references

- `provider/ninjatrader/csv_writer.go` — the temp+rename pattern that Defender exclusion protects.
- Plan doc Task 7 — CSV bridge initial design.
- Plan doc Task 30 — source of this runbook.
