using System;
using System.Collections.Generic;
using System.Linq;
using NinjaTrader.Data;
using NinjaTrader.NinjaScript.AddOns;

class Program
{
    static int failures = 0;
    static void Check(bool cond, string name)
    {
        Console.WriteLine((cond ? "PASS " : "FAIL ") + name);
        if (!cond) failures++;
    }

    static long NowMs() { return DateTimeOffset.UtcNow.ToUnixTimeMilliseconds(); }

    static Bars MakeBars(params long[] times)
    {
        var b = new Bars();
        foreach (var t in times) b.Add(t, 100, 110, 90, 105, 1000);
        return b;
    }

    static VLBarsSubscriptionManager NewManager(List<(string, Dictionary<string, object>)> frames)
    {
        return new VLBarsSubscriptionManager(
            (t, p) => frames.Add((t, p)),
            s => { },
            s => { });
    }

    static void Subscribe(VLBarsSubscriptionManager m)
    {
        m.HandleBarsSubscribe(new Dictionary<string, object>
        {
            { "symbol", "MNQ" },
            { "timeframes", new List<object> { "1m" } },
            { "bars_back", 500 }
        });
    }

    static int Main()
    {
        var frames = new List<(string, Dictionary<string, object>)>();

        // ---- Scenario 1: raw-empty seed - readiness + no false freshness ----
        var m1 = NewManager(frames);
        Subscribe(m1);
        var r1 = NinjaTrader.Data.BarsRequest.Created.Last();
        r1.Bars = new Bars();
        r1.FireRequest();
        m1.GetFeedFreshness(out long t1, out long a1, out string p1, out string tf1, out string s1);
        Check(t1 == 0, "S1 empty seed: no false freshness (last_bar_t_ms==0)");
        Check(tf1 == "" && s1 == "", "S1 empty seed: no sample identity");
        int bu = frames.Count(f => f.Item1 == "bar_update");
        r1.Bars = MakeBars(NowMs());
        r1.FireUpdate(0, 0);
        Check(frames.Count(f => f.Item1 == "bar_update") > bu, "S1 empty seed readiness: later live bar emits");

        // ---- Scenario 2: normal seed + delayed/fresh market honesty + dedup recovery ----
        frames.Clear();
        var m2 = NewManager(frames);
        Subscribe(m2);
        var r2 = NinjaTrader.Data.BarsRequest.Created.Last();
        long delayed = NowMs() - 600000;
        r2.Bars = MakeBars(delayed);
        r2.FireRequest();
        Check(frames.Any(f => f.Item1 == "bars_historical"), "S2 normal seed emits");
        m2.GetFeedFreshness(out long t2, out long a2, out string p2, out string tf2, out string s2);
        Check(t2 == delayed, "S2 delayed bar: market timestamp honest (10min lag)");
        Check(tf2 == "1m" && s2 == "MNQ", "S2 sample identified (symbol+timeframe)");
        long fresh = NowMs();
        r2.Bars = MakeBars(delayed, fresh);
        r2.FireUpdate(1, 1);
        m2.GetFeedFreshness(out t2, out a2, out p2, out tf2, out s2);
        Check(t2 == fresh, "S2 fresh bar: market timestamp advances");
        int hist = frames.Count(f => f.Item1 == "bars_historical");
        r2.Bars = MakeBars(delayed);
        r2.FireRequest();
        Check(frames.Count(f => f.Item1 == "bars_historical") == hist, "S2 dedup seed: no new historical");
        m2.GetFeedFreshness(out t2, out a2, out p2, out tf2, out s2);
        Check(t2 == fresh, "S2 dedup seed: freshness not falsely refreshed");
        bu = frames.Count(f => f.Item1 == "bar_update");
        long fresh2 = NowMs() + 1000;
        r2.Bars = MakeBars(delayed, fresh, fresh2);
        r2.FireUpdate(2, 2);
        Check(frames.Count(f => f.Item1 == "bar_update") > bu, "S2 dedup seed recovery: later live bar emits");

        // ---- Scenario 3: dedup-ALL FIRST seed (HistoricalSent still false) ----
        frames.Clear();
        var m3 = NewManager(frames);
        Subscribe(m3);
        var r3 = NinjaTrader.Data.BarsRequest.Created.Last();
        r3.Bars = MakeBars(0); // epoch-0 bar: t=0 <= watermark 0 -> dedup-all on the FIRST seed
        r3.FireRequest();
        Check(!frames.Any(f => f.Item1 == "bars_historical"), "S3 dedup-first seed: emits no historical");
        bu = frames.Count(f => f.Item1 == "bar_update");
        r3.Bars = MakeBars(0, NowMs());
        r3.FireUpdate(1, 1);
        Check(frames.Count(f => f.Item1 == "bar_update") > bu, "S3 dedup-first seed readiness: later live bar emits");

        Console.WriteLine(failures == 0 ? "ALL PASS" : failures + " FAILURE(S)");
        return failures == 0 ? 0 : 1;
    }
}
