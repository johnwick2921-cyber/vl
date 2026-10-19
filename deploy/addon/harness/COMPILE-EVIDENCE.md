# P0 feed-freshness — compile + harness evidence

Tested source SHA: `735c0962f` (branch `fix/p0-addon-feed-freshness`, off dev `7b0a1c46a`).

## Isolated compile (read-only NT8 references, staging output only)

Command:
```
dotnet build VL.AddOn.csproj -c Release --no-incremental   # via Windows dotnet 9.0.307 (C:\Program Files\dotnet)
```

Result: **Build succeeded · 0 Warning(s) · 0 Error(s)** (net48, LangVersion 13, refs = NinjaTrader.Custom/Vendor/Core/Gui + Newtonsoft.Json from the installed NT8, output to a NEW staging dir, never live Custom).

Retained artifacts:
- Build log: `C:\Users\hoang\AppData\Local\Temp\vl-addon-compile-evidence\build.log`
- Output DLL: `C:\Users\hoang\AppData\Local\Temp\vl-addon-compile-evidence\bin\Release\VL.AddOn.dll`

Staged `ninjascript/*.cs` sha256 (each MATCHES the committed file at `735c0962f`):
- VLBarsSubscriptionManager.cs  `246faad17459b7349ae21254ad3cc7ef5b26e987e8c742b010927eca0a6b9c1a`
- VLTraderTCPClient.cs  `7aae2529182945095002f61ae0550e275420e825dd4cb098480466b8b27098bb`
- VLInstrumentLookup.cs  `6a1c6d5e403a15aaca5ca6ebd4c36f2f5264ba41028ee64b8a3d5e6866e3e6d0`
- VLContractResolver.cs  `f609d20e68ec98786ea224c1a53c37a3c3b37c7ec3000b397589c78cc5248244`
- VLHistoryPull.cs  `9c64dd3c827c35aac2118b8c76e1bdd53aca3c4cc0938a775cd216ea91244186`

Harness source sha256 (`deploy/addon/harness/`):
- Program.cs  `f33b08d77db88e7fa3283ca15ae0c58db40cfdb5fffdaad78d26a0368b9c87b2`
- NT8Stubs.cs  `7fe3bcb4b152088f3a12a46f568d8d05d93bd4d3617b4b7b1dd00f62f5d8c696`
- VLStubs.cs  `7674990c31cf8a86421825818c07f84c625034a8cf5c17488d08645e74091219`

Harness run staging: `C:\Users\hoang\AppData\Local\Temp\vl-addon-harness-ds103`

## Production callback harness (deploy/addon/harness/)

- Compiles the ACTUAL `ninjascript/VLBarsSubscriptionManager.cs` against stub NinjaTrader.Data/Cbi types (real NT8 DLLs cannot run BarsRequest outside NT8).
- Deterministic clock seam (`ClockMs`) + `TickForTest`; background 15s watchdog Timer disabled via `DisableWatchdogTimerForTest` (default false = production unchanged).
- **21 pins PASS**: raw-empty seed (no false freshness/arrival + later live bar emits), normal seed, delayed/fresh market-timestamp honesty, symbol+timeframe sample, dedup-all seed (no new historical + no false refresh + later live bar emits), dedup-first-seed readiness, real `OnConnectionReconnected` path (preserved watermark → dedup-empty → later live bar emits), watchdog dead-empty-seed bounded recovery (exactly 3 recreates, no 4th), minArrival backstop independent of the oldest market-time entry.
- **4 mutants RED then restored**: (1) raw-empty drops `HistoricalSent`; (2) dedup path sets `HistoricalSent` after the empty check; (3) fast-guard cap check removed; (4) 75-min backstop uses the arrival paired with the oldest market-time entry.
- Reproduce: `dotnet run --project deploy/addon/harness/Harness.csproj -c Release` (Windows .NET 9).
