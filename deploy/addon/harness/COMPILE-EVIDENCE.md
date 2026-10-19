# P0 feed-freshness — compile + harness evidence

Source SHA: `8a2f1aeb6` (ninjascript identical at HEAD `da72da6e3`; the harness commit only added deploy/addon/harness/).

## Isolated compile (read-only NT8 references, staging output only)

Command:
```
dotnet build VL.AddOn.csproj -c Release --no-incremental   # via Windows dotnet 9.0.307 (C:\Program Files\dotnet)
```

Result: **Build succeeded · 0 Warning(s) · 0 Error(s)** (net48, LangVersion 13, refs = NinjaTrader.Custom/Vendor/Core/Gui + Newtonsoft.Json from the installed NT8, output to a NEW staging dir, never live Custom). Full log captured to the staging dir.

Staged .cs sha256 (each MATCHES the committed ninjascript file):
- VLBarsSubscriptionManager.cs  `d166de3d8c5303b268c33d0745b8509cd5d3c467da6d7d88cd6487d3d3daa7a7`
- VLTraderTCPClient.cs  `7aae2529182945095002f61ae0550e275420e825dd4cb098480466b8b27098bb`
- VLInstrumentLookup.cs  `6a1c6d5e403a15aaca5ca6ebd4c36f2f5264ba41028ee64b8a3d5e6866e3e6d0`
- VLContractResolver.cs  `f609d20e68ec98786ea224c1a53c37a3c3b37c7ec3000b397589c78cc5248244`
- VLHistoryPull.cs  `9c64dd3c827c35aac2118b8c76e1bdd53aca3c4cc0938a775cd216ea91244186`

## Production callback harness (deploy/addon/harness/)

- Compiles the ACTUAL `ninjascript/VLBarsSubscriptionManager.cs` against stub NinjaTrader.Data/Cbi types.
- 12 pins PASS (raw-empty seed, normal seed, delayed/fresh market timestamps, symbol+timeframe sample, dedup-all seed, dedup-first-seed readiness).
- 2 mutants RED then restored: (1) raw-empty drops HistoricalSent; (2) dedup path sets HistoricalSent after the empty check.
- Reproduce: `dotnet run --project deploy/addon/harness/Harness.csproj -c Release` (Windows .NET 9).
