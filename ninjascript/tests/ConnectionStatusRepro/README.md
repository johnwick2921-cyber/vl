# DS-104 — `OnVLConnectionStatusUpdate` multi-connection handler repro

## Status: NOT EXECUTED (blocked by environment) — scaffold for Windows-side run

This harness drives the **actual** `NinjaTrader.NinjaScript.AddOns.VLTraderTCPClient.OnVLConnectionStatusUpdate`
call site via reflection. It does **not** copy the algorithm — it invokes the real
private method on the real type and observes the real `VLBarsSubscriptionManager.OnConnectionReconnected()`
side effects through injected log delegates.

## Correction (2026-10-20 00:2x CT) — earlier "NinjaTrader.Cbi.dll required" claim was WRONG

`NinjaTrader.Cbi` is a **namespace**, and its types (`ConnectionStatusEventArgs`, `ConnectionStatus`)
plus `NinjaTrader.NinjaScript.AddOnBase` all live in **`NinjaTrader.Core.dll`**. There is no separate
`NinjaTrader.Cbi.dll`. PROVEN: the Windows .NET Framework compiler `csc.exe` (4.8.9221.0, runs via WSL
interop) compiled a probe that references `NinjaTrader.Cbi.ConnectionStatusEventArgs` and
`NinjaTrader.NinjaScript.AddOnBase` against `-r:"C:\Program Files\NinjaTrader 8\bin\NinjaTrader.Core.dll"`
with NO errors.

## Offline compile: FEASIBLE (proven) — offline EXECUTION: BLOCKED

- **Compile works:** `csc.exe` (Framework 4.8) is available and compiles against the NT8 assemblies.
  Limitation: `csc.exe` is C# 5 only; the production sources use C# ≥ 6, so the full build needs
  `dotnet.exe` 9.0.307 (Roslyn) with a `net48` target. The .NET Framework 4.8 targeting pack is present
  at `C:\Program Files (x86)\Reference Assemblies\Microsoft\Framework\.NETFramework\v4.8`, so the
  build is feasible offline (no NuGet network needed if `TargetFrameworkRootPath` is set).
- **Execution is blocked:** `NinjaTrader.Core.dll` is AgileDotNet-protected; loading it in a standalone
  CLR throws at `<AgileDotNetRT>.Initialize()` (`ArgumentNullException`). A faithful repro of the real
  `OnVLConnectionStatusUpdate` therefore must run **inside NT8's AppDomain** (launching NT8 is not
  authorized for DS-104 at this time). The reflection driver below is the correct shape but cannot be
  executed standalone.

## Withdrawn (DS-104)

- The earlier claim that a separate `NinjaTrader.Cbi.dll` is required.
- The "desired feed stays dead until the 75-minute watchdog" outcome — not directly demonstrated; withdrawn.
- Framing `lastRecreateUtc` advance as a direct runtime proof of `OnConnectionReconnected()` execution —
  it is a source-level equivalence (`fireRecreate` is the only setter), not a runtime demonstration.

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
