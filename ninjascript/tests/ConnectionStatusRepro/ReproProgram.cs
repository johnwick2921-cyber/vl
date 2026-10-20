// DS-104 — connection-agnostic BarsRequest-recreate repro harness.
//
// STATUS: NOT COMPILED / NOT EXECUTED ON WSL (no C# toolchain; NinjaTrader.Cbi.dll
// not on the WSL-accessible path). This is a reviewable scaffold that must be built
// and run ONCE on a Windows box with the .NET SDK + the NinjaTrader 8 assemblies.
//
// WHAT IT DOES (does NOT copy the algorithm):
//   - Locates the REAL NinjaTrader.NinjaScript.AddOns.VLTraderTCPClient type (the
//     source files are compiled into this same assembly — see the .csproj).
//   - Instantiates it WITHOUT running OnStateChange (GetUninitializedObject), then
//     initializes the private fields the handler needs (connStatusLock).
//   - Invokes the REAL private OnVLConnectionStatusUpdate(object, ConnectionStatusEventArgs)
//     via reflection, with two distinct `sender` identities (unrelated vs desired).
//   - Spies the actual recreation call indirectly via the ONLY state that gates it:
//       fireRecreate == true  <=>  lastRecreateUtc advanced  <=>  barsManager.OnConnectionReconnected() was called
//     (lastRecreateUtc is written only inside the fireRecreate branch in the real handler).
//   - Also latches dataFeedWasLost transitions (the shared cross-connection flag).
//
// SCENARIOS (assert current, connection-agnostic behavior):
//   A. unrelated (blunox) Disconnected -> Connected  => recreate fires (count 1)
//   B. desired (fundfuture) Connected recovery inside 30s debounce => recreate SKIPPED (count stays 0)

using System;
using System.Reflection;
using System.Runtime.CompilerServices;
using System.Threading;

internal static class ReproProgram
{
    private static int recreateCount = 0;

    private static int Main(string[] args)
    {
        // The harness assembly compiles the ninjascript sources in, so the type
        // under test is the actual shipped source (repo dev tip), not a copy.
        Type addon = typeof(NinjaTrader.NinjaScript.AddOns.VLTraderTCPClient);

        // --- fields we read/write on the real type (names verified against source) ---
        FieldInfo fConnStatusLock = addon.GetField("connStatusLock", BindingFlags.NonPublic | BindingFlags.Instance);
        FieldInfo fDataFeedWasLost = addon.GetField("dataFeedWasLost", BindingFlags.NonPublic | BindingFlags.Instance);
        FieldInfo fLastRecreateUtc = addon.GetField("lastRecreateUtc", BindingFlags.NonPublic | BindingFlags.Instance);
        MethodInfo handler = addon.GetMethod("OnVLConnectionStatusUpdate", BindingFlags.NonPublic | BindingFlags.Instance);

        if (fConnStatusLock == null || fDataFeedWasLost == null || fLastRecreateUtc == null || handler == null)
        {
            Console.WriteLine("FAIL: expected private fields/method not found on VLTraderTCPClient");
            return 2;
        }

        object instance = RuntimeHelpers.GetUninitializedObject(addon);
        fConnStatusLock.SetValue(instance, new object());   // readonly field is null after GetUninitializedObject
        fDataFeedWasLost.SetValue(instance, false);
        fLastRecreateUtc.SetValue(instance, DateTime.MinValue);

        Type evtArgsType = typeof(NinjaTrader.Cbi.ConnectionStatusEventArgs);
        Type connStatusEnum = typeof(NinjaTrader.Cbi.ConnectionStatus);

        // SCENARIO A — unrelated connection flaps Disconnected -> Connected.
        // sender is IGNORED by the current handler, so any identity behaves the same.
        Invoke(addon, instance, handler, "blunox", evtArgsType, connStatusEnum, "Disconnected", "Connected");
        bool lostAfterDrop = (bool)fDataFeedWasLost.GetValue(instance);
        Invoke(addon, instance, handler, "blunox", evtArgsType, connStatusEnum, "Connected", "Disconnected");
        bool recreateAFired = ((DateTime)fLastRecreateUtc.GetValue(instance)) > DateTime.UtcNow.AddSeconds(-5);

        Console.WriteLine($"[A] unrelated blunox Disconnected->Connected : dataFeedWasLost={lostAfterDrop} recreateFired={recreateAFired}");

        // SCENARIO B — desired feed recovers INSIDE the global 30s debounce window.
        // Prime: an unrelated recreate already fired ~10s ago (simulate the churn from A),
        // and the desired connection genuinely lost its feed.
        fLastRecreateUtc.SetValue(instance, DateTime.UtcNow.AddSeconds(-10)); // within RecreateDebounce (30s)
        fDataFeedWasLost.SetValue(instance, true);                            // desired feed genuinely lost
        Invoke(addon, instance, handler, "fundfuture", evtArgsType, connStatusEnum, "Connected", "Disconnected");
        bool recreateBFired = ((DateTime)fLastRecreateUtc.GetValue(instance)) > DateTime.UtcNow.AddSeconds(-5);

        Console.WriteLine($"[B] fundfuture recovery inside debounce : recreateFired={recreateBFired} (expected false -> desired feed left dead until watchdog)");

        bool defectDemonstrated = recreateAFired && !recreateBFired;
        Console.WriteLine(defectDemonstrated
            ? "RESULT: connection-agnostic recreate + debounce-consumption DEMONSTRATED."
            : "RESULT: expected behavior NOT reproduced — check assembly/reflection wiring.");
        return defectDemonstrated ? 0 : 1;
    }

    private static void Invoke(Type addon, object instance, MethodInfo handler,
        object sender, Type evtArgsType, Type connStatusEnum,
        string priceStatus, string prevPriceStatus)
    {
        object args = RuntimeHelpers.GetUninitializedObject(evtArgsType);
        SetEnumBacking(evtArgsType, args, "PriceStatus", connStatusEnum, priceStatus);
        SetEnumBacking(evtArgsType, args, "PreviousPriceStatus", connStatusEnum, prevPriceStatus);
        handler.Invoke(instance, new object[] { sender, args });
    }

    private static void SetEnumBacking(Type evtArgsType, object args, string propName, Type enumType, string value)
    {
        // C# auto-property backing field: <PriceStatus>k__BackingField
        foreach (FieldInfo f in evtArgsType.GetFields(BindingFlags.NonPublic | BindingFlags.Instance))
        {
            if (f.Name.Contains(propName))
            {
                f.SetValue(args, Enum.Parse(enumType, value));
                return;
            }
        }
        throw new MissingFieldException("no backing field for " + propName);
    }
}
