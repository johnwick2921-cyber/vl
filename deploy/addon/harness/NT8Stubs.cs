// Minimal NT8 type stubs for the VLBarsSubscriptionManager callback harness.
// Same fully-qualified names as the real NinjaTrader assemblies, so the
// production manager source compiles against these WITHOUT the real NT8 DLLs.
using System;
using System.Collections.Generic;

namespace NinjaTrader.Data
{
    public enum BarsPeriodType { Tick = 0, Minute = 1, Day = 2, Week = 3, Hour = 4, Volume = 5 }
    public enum MergePolicy { UseGlobalSettings = 0, MergeBackAdjusted = 1, MergeNonBackAdjusted = 2, DoNotMerge = 3 }
    public enum ErrorCode { NoError = 0, UserAbort = 1, Error = 2 }

    public class BarsPeriod
    {
        public BarsPeriodType BarsPeriodType { get; set; }
        public int Value { get; set; }
    }

    public class BarsUpdateEventArgs : EventArgs
    {
        public int MinIndex { get; set; }
        public int MaxIndex { get; set; }
    }

    public class TradingHours
    {
        public TimeZoneInfo TimeZoneInfo { get; set; } = TimeZoneInfo.Utc;
        public static TradingHours Get(string name) { return new TradingHours(); }
    }

    public class Bars
    {
        private readonly List<double[]> rows = new List<double[]>(); // [tMs,o,h,l,c,v]
        public TradingHours TradingHours { get; set; } = new TradingHours();
        public int Count => rows.Count;
        public void Add(long tMs, double o, double h, double l, double c, long v) { rows.Add(new double[] { tMs, o, h, l, c, v }); }
        public DateTime GetTime(int i) { return new DateTime(1970, 1, 1, 0, 0, 0, DateTimeKind.Utc).AddMilliseconds(rows[i][0]); }
        public double GetOpen(int i) { return rows[i][1]; }
        public double GetHigh(int i) { return rows[i][2]; }
        public double GetLow(int i) { return rows[i][3]; }
        public double GetClose(int i) { return rows[i][4]; }
        public long GetVolume(int i) { return (long)rows[i][5]; }
    }

    public class BarsRequest
    {
        public static readonly List<BarsRequest> Created = new List<BarsRequest>();
        private Action<BarsRequest, ErrorCode, string> requestCallback;
        public event Action<object, BarsUpdateEventArgs> Update;
        public Bars Bars { get; set; }
        public BarsPeriod BarsPeriod { get; set; }
        public MergePolicy MergePolicy { get; set; }
        public TradingHours TradingHours { get; set; }
        public BarsRequest(NinjaTrader.Cbi.Instrument instrument, int barsBack) { Created.Add(this); }
        public void Request(Action<BarsRequest, ErrorCode, string> callback) { requestCallback = callback; }
        public void Dispose() { }
        // Test hooks
        public void FireRequest() { requestCallback?.Invoke(this, ErrorCode.NoError, ""); }
        public void FireUpdate(int minIdx, int maxIdx) { Update?.Invoke(this, new BarsUpdateEventArgs { MinIndex = minIdx, MaxIndex = maxIdx }); }
    }
}

namespace NinjaTrader.Cbi
{
    public class MasterInstrument
    {
        public string Name { get; set; } = "";
        public double PointValue { get; set; } = 1;
        public double TickSize { get; set; } = 1;
    }
    public class Instrument
    {
        public string FullName { get; set; } = "";
        public MasterInstrument MasterInstrument { get; set; } = new MasterInstrument();
    }
}
