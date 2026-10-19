# VL AddOn callback harness (P0 feed freshness)

Compiles the PRODUCTION `ninjascript/VLBarsSubscriptionManager.cs` against stub
`NinjaTrader.Data` / `NinjaTrader.Cbi` types (the real NT8 DLLs cannot run a
`BarsRequest` outside NT8) and exercises the production historical/streaming
callbacks. This is the runtime proof for the readiness-latch / freshness split —
it is NOT a substitute for NT8 F5 integration.

Run (Windows, .NET 9 SDK, from WSL):

    powershell.exe -NoProfile -Command "cd 'C:\path\to\repo\deploy\addon\harness'; dotnet run -c Release"

Pins: raw-empty seed, dedup-all first seed, normal seed, delayed/fresh market
timestamps, symbol+timeframe sample, recovery after a deduped seed. Two mutants
(raw-empty drops HistoricalSent; dedup path sets HistoricalSent after the empty
check) each go RED then restore to green.
