// Stub resolution services so HandleBarsSubscribe resolves without the real NT8 DB.
using System;
using NinjaTrader.Cbi;

namespace NinjaTrader.NinjaScript.AddOns
{
    public static class VLContractResolver
    {
        public static string ResolveFrontMonthContract(string symbol) { return symbol; }
    }

    public static class VLInstrumentLookup
    {
        public static Instrument Resolve(string symbol, Action<string> logWarn, out string how)
        {
            how = "stub";
            return new Instrument
            {
                FullName = symbol + " 12-26",
                MasterInstrument = new MasterInstrument { Name = symbol, PointValue = 2.0, TickSize = 0.25 }
            };
        }
        public static string ContractName(Instrument instrument) { return instrument.FullName; }
    }
}
