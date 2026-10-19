# VL AddOn callback harness (P0 feed freshness)

Compiles the PRODUCTION `ninjascript/VLBarsSubscriptionManager.cs` against stub
`NinjaTrader.Data` / `NinjaTrader.Cbi` types (the real NT8 DLLs cannot run a
`BarsRequest` outside NT8) and exercises the production historical/streaming
callbacks. This is the runtime proof for the readiness-latch / freshness split —
it is NOT a substitute for NT8 F5 integration.

Run (Windows, .NET 9 SDK, from WSL):

    powershell.exe -NoProfile -Command "cd 'C:\path\to\repo\deploy\addon\harness'; dotnet run -c Release"

Tested source SHA: `735c0962f` (see COMPILE-EVIDENCE.md for hashes + retained
log paths).

21 pins PASS: raw-empty seed (no false freshness/arrival + later live bar emits),
normal seed, delayed/fresh market timestamps, symbol+timeframe sample, dedup-all
seed, dedup-first-seed readiness, real `OnConnectionReconnected` path, watchdog
dead-empty-seed bounded recovery (exactly 3 recreates, no 4th), minArrival
backstop independence. 4 mutants RED then restored: raw-empty drops
`HistoricalSent`; dedup path sets `HistoricalSent` after the empty check;
fast-guard cap removed; 75-min backstop uses the oldest market-time entry's
paired arrival. Deterministic clock (`ClockMs`) + `TickForTest`; the 15s
background watchdog Timer is disabled via `DisableWatchdogTimerForTest` (default
false = production unchanged).
