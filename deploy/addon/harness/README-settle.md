# Settle gate harnesses (Release 2, AddOn 2026-11-24-a)

Both compile PRODUCTION C# files outside NinjaTrader (Windows .NET 9 SDK). Copy the
folders to a NEW Windows-visible folder first; never build inside the NT8 folder.
They are not a substitute for the live SIM checks (S1-S6 of the release gate).

## settlegate - pure logic

Compiles `ninjascript/VLSettleGate.cs` (no NinjaTrader types) with C# 7.3.

    dotnet run -c Release                      # unit tests
    dotnet run -c Release -- replay events.tsv # NT8 history through the same classes

`extract_nt8.py log|trace <dir>` makes the event file (read only). It carries account
names: keep it out of the repo. 2026-10-26..11-24: 48 close frames, exactly 1 held
(06:47:30.252, 30 ms, position 0 at release); 4,488 unsettled spells, max 541 ms,
none over 1000. Trace files: 38 SIM flattens, 36 at 0 ms; the other two are 06:47
and the hand close of the reverse that 06:47 opened (it does not exist in the
replayed story).

## settlewire - the real client

Compiles `ninjascript/VLTraderTCPClient.cs` against `NT8WireStubs.cs` (232 lines) and
drives HandleFrame by reflection. It never activates the AddOn: port 36974 is the live bot.

    dotnet run -c Release                                   # GREEN
    dotnet run -c Release -p:AddonSrc=<base ninjascript>    # RED on c6f9c0663: 28 of 61 (before round 2)
    dotnet run -c Release -- seq                            # call + frame sequences, all switches off

`seq` is byte-identical on the base source and on this one with every switch at 0 (W14).

Round 2 (R1-R14 in settlewire, R2-1..R2-5 in settlegate): RED on 58f66b1c7: settlewire 8 of
79, settlegate 14 of 172 (behaviour tests; the R2-5 log API did not exist). R14 and the price rule are a KNOWN LIMIT pin, not a fix.
