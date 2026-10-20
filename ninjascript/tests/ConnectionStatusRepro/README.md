# DS-104 — `OnVLConnectionStatusUpdate` multi-connection handler repro

## Status: NOT EXECUTED (blocked by environment) — scaffold for Windows-side run

This harness drives the **actual** `NinjaTrader.NinjaScript.AddOns.VLTraderTCPClient.OnVLConnectionStatusUpdate`
call site via reflection. It does **not** copy the algorithm — it invokes the real
private method on the real type and observes the real `VLBarsSubscriptionManager.OnConnectionReconnected()`
side effects through injected log delegates.

## Exact dependencies blocking execution on this WSL box (DS-104, 2026-10-20)

1. **No C# toolchain** — `dotnet`, `mono`, `csc`, `mcs` are all absent on this host.
2. **`NinjaTrader.Cbi.dll` is not on the WSL-accessible path.** `/mnt/c/Program Files/NinjaTrader 8/bin/`
   exposes `NinjaTrader.Core.dll`, `NinjaTrader.Gui.dll`, `NinjaTrader.Client.dll` but **not**
   `NinjaTrader.Cbi.dll` (which defines `Connection`, `Connection.ConnectionStatusUpdate`, and
   `ConnectionStatusEventArgs`). `NinjaTrader.Custom.dll` / `NinjaTrader.Vendor.dll` exist only under
   `Documents\NinjaTrader 8\bin\Custom\`.
3. **`VLTraderTCPClient.cs` itself declares it cannot be compiled in WSL2** (file header): the
   operator compiles on Windows via NT8 F5 or Visual Studio.
4. The static `Connection.ConnectionStatusUpdate` event is raised by a running NT8; a faithful
   execution also needs a live NT8 process (or the full NT8 SDK + a Windows .NET SDK to build the
   reflection driver against the real assemblies).

Because of #1–#3 the harness below has **not been compiled or executed here**. It is a
reviewable scaffold that must be built/run once on a Windows box with the NT8 assemblies.

## What the harness proves (once run)

- **Scenario A — unrelated connection contamination:** a connection that never carries the MNQ
  subscription (`sender = "blunox"`, Rithmic) flapping `Disconnected → Connected` sets the shared
  `dataFeedWasLost` and fires a full `OnConnectionReconnected()` (all BarsRequests recreated).
- **Scenario B — debounce consumption:** after an unrelated recreate, a genuine `fundfuture`
  `Connected` recovery inside the 30s `RecreateDebounce` window is **skipped** (recreation count = 0).

## Run (on a Windows box with .NET SDK + NT8)

```powershell
# from ninjascript\tests\ConnectionStatusRepro
# 1) confirm assembly paths in ConnectionStatusRepro.csproj match this machine
# 2) build + run
dotnet run -c Debug
```

Expected output: both scenarios print `recreateCount` and the program asserts the current
(connection-agnostic) behavior described above, then exits non-zero with a "defect demonstrated"
message if the behavior is present.

## Verified vs not

- **Verified [A]:** source-level mechanism (read from `VLTraderTCPClient.cs` @ `359435ab` and
  `VLBarsSubscriptionManager.cs` @ `4e8f7931`): shared `dataFeedWasLost`, global `RecreateDebounce=30s`,
  handler ignores `sender`, `OnConnectionReconnected()` disposes+recreates all active BarsRequests.
- **Not verified:** the exact runtime counts — requires the Windows run above.
