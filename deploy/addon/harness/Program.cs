using System;
using System.Collections.Generic;
using System.Linq;
using NinjaTrader.Data;
using NinjaTrader.NinjaScript.AddOns;

class Program
{
    static int failures = 0;
    static long fakeNow = 1_700_000_000_000L; // deterministic clock
    static void Check(bool cond, string name)
    {
        Console.WriteLine((cond ? "PASS " : "FAIL ") + name);
        if (!cond) failures++;
    }
    static void Advance(long ms) { fakeNow += ms; }
    static Bars MakeBars(params long[] times)
    {
        var b = new Bars();
        foreach (var t in times) b.Add(t, 100, 110, 90, 105, 1000);
        return b;
    }
    static VLBarsSubscriptionManager NewManager(List<(string, Dictionary<string, object>)> frames)
    {
        return new VLBarsSubscriptionManager((t, p) => frames.Add((t, p)), s => { }, s => { });
    }
    static void Subscribe(VLBarsSubscriptionManager m, string tf = "1m")
    {
        m.HandleBarsSubscribe(new Dictionary<string, object>
        {
            { "symbol", "MNQ" },
            { "timeframes", new List<object> { tf } },
            { "bars_back", 500 }
        });
    }

    static int Main()
    {
        VLBarsSubscriptionManager.ClockMs = () => fakeNow;
        VLBarsSubscriptionManager.DisableWatchdogTimerForTest = true; // deterministic ticks only, no 15s background timer
        var frames = new List<(string, Dictionary<string, object>)>();

        // ---- S1: raw-empty seed - readiness + no false ARRIVAL ----
        var m1 = NewManager(frames);
        Subscribe(m1);
        var r1 = NinjaTrader.Data.BarsRequest.Created.Last();
        long t0 = fakeNow;
        r1.Bars = new Bars();
        r1.FireRequest();
        m1.GetFeedFreshness(out long t, out long a, out string p, out string tf, out string s);
        Check(t == 0 && a == 0, "S1 empty seed: no false freshness/arrival (t==0 && a==0)");
        Advance(10_000);
        int bu = frames.Count(f => f.Item1 == "bar_update");
        r1.Bars = MakeBars(fakeNow);
        r1.FireUpdate(0, 0);
        Check(frames.Count(f => f.Item1 == "bar_update") > bu, "S1 empty seed readiness: later live bar emits");
        m1.GetFeedFreshness(out t, out a, out p, out tf, out s);
        Check(a == fakeNow, "S1 live bar arrival stamped exactly at controlled clock");

        // ---- S2: normal + delayed/fresh market honesty + dedup recovery (arrival unchanged) ----
        frames.Clear();
        var m2 = NewManager(frames);
        Subscribe(m2);
        var r2 = NinjaTrader.Data.BarsRequest.Created.Last();
        fakeNow = 1_700_000_000_000L;
        long delayed = fakeNow - 600_000;
        r2.Bars = MakeBars(delayed);
        r2.FireRequest();
        m2.GetFeedFreshness(out t, out a, out p, out tf, out s);
        Check(t == delayed && a == fakeNow, "S2 delayed bar: market t honest + arrival = seed time");
        Check(tf == "1m" && s == "MNQ", "S2 sample identified (symbol+timeframe)");
        Advance(1000);
        long fresh = fakeNow;
        r2.Bars = MakeBars(delayed, fresh);
        r2.FireUpdate(1, 1);
        m2.GetFeedFreshness(out t, out a, out p, out tf, out s);
        Check(t == fresh && a == fakeNow, "S2 fresh bar: market t advances + arrival updates");
        Advance(1000);
        long arrivalBeforeDedup = fakeNow - 1000; // the fresh bar's arrival
        int hist = frames.Count(f => f.Item1 == "bars_historical");
        r2.Bars = MakeBars(delayed);
        r2.FireRequest();
        Check(frames.Count(f => f.Item1 == "bars_historical") == hist, "S2 dedup seed: no new historical");
        m2.GetFeedFreshness(out t, out a, out p, out tf, out s);
        Check(t == fresh, "S2 dedup seed: freshness not refreshed");
        Check(a == arrivalBeforeDedup, "S2 dedup seed: ARRIVAL unchanged (not falsely refreshed)");
        bu = frames.Count(f => f.Item1 == "bar_update");
        Advance(1000);
        long fresh2 = fakeNow;
        r2.Bars = MakeBars(delayed, fresh, fresh2);
        r2.FireUpdate(2, 2);
        Check(frames.Count(f => f.Item1 == "bar_update") > bu, "S2 dedup seed recovery: later live bar emits");

        // ---- S3: dedup-ALL FIRST seed (HistoricalSent still false) ----
        frames.Clear();
        var m3 = NewManager(frames);
        Subscribe(m3);
        var r3 = NinjaTrader.Data.BarsRequest.Created.Last();
        r3.Bars = MakeBars(0); // t=0 <= watermark 0 -> dedup-all on the FIRST seed
        r3.FireRequest();
        Check(!frames.Any(f => f.Item1 == "bars_historical"), "S3 dedup-first seed: emits no historical");
        bu = frames.Count(f => f.Item1 == "bar_update");
        r3.Bars = MakeBars(0, fakeNow);
        r3.FireUpdate(1, 1);
        Check(frames.Count(f => f.Item1 == "bar_update") > bu, "S3 dedup-first seed readiness: later live bar emits");

        // ---- S4: REAL OnConnectionReconnected path (the incident path) ----
        frames.Clear();
        var m4 = NewManager(frames);
        Subscribe(m4);
        var r4a = NinjaTrader.Data.BarsRequest.Created.Last();
        fakeNow = 1_700_000_000_000L;
        long bar1 = fakeNow - 60_000;
        r4a.Bars = MakeBars(bar1);
        r4a.FireRequest();
        Check(frames.Any(f => f.Item1 == "bars_historical"), "S4 normal seed emits");
        m4.GetFeedFreshness(out t, out long a0, out p, out tf, out s);
        int createdBefore = NinjaTrader.Data.BarsRequest.Created.Count;
        Advance(2000);
        m4.OnConnectionReconnected(); // new BarsRequest, watermark PRESERVED = bar1
        Check(NinjaTrader.Data.BarsRequest.Created.Count > createdBefore, "S4 reconnect creates a new BarsRequest");
        var r4b = NinjaTrader.Data.BarsRequest.Created.Last();
        int hist4 = frames.Count(f => f.Item1 == "bars_historical");
        r4b.Bars = MakeBars(bar1); // delayed feed re-returns the SAME old bar
        r4b.FireRequest();
        Check(frames.Count(f => f.Item1 == "bars_historical") == hist4, "S4 reconnect dedup-empty: no duplicate historical");
        m4.GetFeedFreshness(out t, out a, out p, out tf, out s);
        Check(a == 0 && t == 0, "S4 reconnect dedup-empty: fresh entry carries NO false arrival/freshness");
        bu = frames.Count(f => f.Item1 == "bar_update");
        Advance(1000);
        long newbar = fakeNow;
        r4b.Bars = MakeBars(bar1, newbar);
        r4b.FireUpdate(1, 1);
        Check(frames.Count(f => f.Item1 == "bar_update") > bu, "S4 reconnect recovery: later live bar emits after dedup-empty seed");

        // ---- S5: watchdog - dead empty seed eligible for BOUNDED recovery ----
        frames.Clear();
        var m5 = NewManager(frames);
        fakeNow = 1_700_000_000_000L; // BEFORE subscribe so SubscribedAtUtcMs is deterministic
        Subscribe(m5);
        var r5 = NinjaTrader.Data.BarsRequest.Created.Last();
        r5.Bars = new Bars();
        r5.FireRequest(); // empty seed: HistoricalSent=true, LiveUpdateSeen=false
        int recreates = 0;
        for (int i = 0; i < 4; i++)
        {
            Advance(20_001); // past FAST_STALL_MS
            int before = NinjaTrader.Data.BarsRequest.Created.Count;
            m5.TickForTest();
            if (NinjaTrader.Data.BarsRequest.Created.Count > before)
            {
                recreates++;
                // Complete the RECREATED request with EMPTY history so it is
                // seeded (HistoricalSent=true, LiveUpdateSeen=false) and the
                // watchdog counts it dead on the next tick — the cap must then
                // stop the loop, not a skipped unseeded entry.
                var rn = NinjaTrader.Data.BarsRequest.Created.Last();
                rn.Bars = new Bars();
                rn.FireRequest();
            }
        }
        Check(recreates == 3, "S5 fast-guard reaches EXACTLY 3 recreates (no 4th after cap)");

        // ---- S6: watchdog - minArrival backstop independent of oldest market-time entry ----
        frames.Clear();
        var m6 = NewManager(frames);
        fakeNow = 1_700_000_000_000L; // T0, BEFORE subscribe
        Subscribe(m6, "5m"); // entry B
        var r6b = NinjaTrader.Data.BarsRequest.Created.Last();
        long bMarket = fakeNow; // B's newest market time (LATER than A's)
        r6b.Bars = MakeBars(bMarket - 1000);
        r6b.FireRequest();
        r6b.Bars = MakeBars(bMarket - 1000, bMarket);
        r6b.FireUpdate(1, 1); // B live: market = T0, arrival = T0 (OLD arrival)
        long bArrival = fakeNow;
        Advance(4_500_001); // 75 min+ → T1
        Subscribe(m6, "1m"); // entry A
        var r6a = NinjaTrader.Data.BarsRequest.Created.Last();
        long aMarket = 1_700_000_000_000L - 3_600_000; // A market STRICTLY OLDER than B (T0-60min)
        r6a.Bars = MakeBars(aMarket - 1000);
        r6a.FireRequest();
        r6a.Bars = MakeBars(aMarket - 1000, aMarket);
        r6a.FireUpdate(1, 1); // A live: market = T0-60min (OLDEST), arrival = T1 (LATER)

        // Assert the ordering BEFORE ticking: A has the oldest MARKET, B has the
        // oldest ARRIVAL. The backstop must key on B's arrival, not A's.
        var samples = m6.SampleEntriesForTest();
        var aEntry = samples.First(x => x.Key == "MNQ|1M");
        var bEntry = samples.First(x => x.Key == "MNQ|5M");
        Check(aEntry.MarketMs < bEntry.MarketMs, "S6 setup: A (1m) has the OLDEST market time");
        Check(bEntry.ArrivalMs < aEntry.ArrivalMs, "S6 setup: B (5m) has the OLDEST arrival");

        int beforeBackstop = NinjaTrader.Data.BarsRequest.Created.Count;
        m6.TickForTest();
        Check(NinjaTrader.Data.BarsRequest.Created.Count > beforeBackstop,
              "S6 backstop: minArrival (not oldest-market arrival) drives the 75-min recreate");

        Console.WriteLine(failures == 0 ? "ALL PASS" : failures + " FAILURE(S)");
        return failures == 0 ? 0 : 1;
    }
}
