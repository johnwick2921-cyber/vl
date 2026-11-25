// Wiring harness: the REAL VLTraderTCPClient.cs, driven through HandleFrame and the
// NT8 account events, against NT8WireStubs. It never activates the AddOn (no dial:
// 127.0.0.1:36974 is the live bot) - frames go to a loopback pair of its own.
// The same file compiles against the base source (no VLSettleGate.cs): the tests
// marked RED in the README fail there.
//   dotnet run -c Release [-p:AddonSrc=<dir>] -- [seq | one <test>]
using System;
using System.Collections.Concurrent;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Linq;
using System.Net;
using System.Net.Sockets;
using System.Reflection;
using System.Text;
using System.Threading;
using NinjaTrader.Cbi;
using NinjaTrader.NinjaScript;
using NinjaTrader.NinjaScript.AddOns;

sealed class Rig : IDisposable
{
    const BindingFlags BF = BindingFlags.Instance | BindingFlags.NonPublic | BindingFlags.Public;
    public static readonly Type GateT = typeof(VLTraderTCPClient).Assembly.GetType("NinjaTrader.NinjaScript.AddOns.VLSettleGate");
    public readonly VLTraderTCPClient C = new VLTraderTCPClient();
    public readonly Account A, B;
    public readonly ConcurrentQueue<string> Frames = new ConcurrentQueue<string>();
    public object Gate;
    readonly TcpListener lis; readonly TcpClient near, far;
    public static int AnchorMs = 30;

    public object Get(string f) { var x = typeof(VLTraderTCPClient).GetField(f, BF); return x == null ? null : x.GetValue(C); }
    public void Set(string f, object v) { var x = typeof(VLTraderTCPClient).GetField(f, BF); if (x != null) x.SetValue(C, v); }
    public object Run(string m, params object[] a)
    {
        var mi = typeof(VLTraderTCPClient).GetMethods(BF).FirstOrDefault(x => x.Name == m && x.GetParameters().Length == a.Length);
        return mi == null ? null : mi.Invoke(C, a);
    }

    public Rig(string switches = null, Action<Rig> before = null, bool gate = true)
    {
        Account.All.Clear(); Connection.Connections.Clear();
        A = new Account { Name = "SimA" }; B = new Account { Name = "SimB" };
        B.Connection = A.Connection; Connection.Connections.Add(A.Connection);
        Account.All.Add(A); Account.All.Add(B);
        NinjaScript.Lines.Clear(); NinjaScript.OnLog = null;
        Instrument.ThrowFullNameIn = -1; Order.ThrowFilledIn = -1; VLInstrumentLookup.ThrowIn = -1;
        lis = new TcpListener(IPAddress.Loopback, 0); lis.Start();
        near = new TcpClient(); near.Connect(IPAddress.Loopback, ((IPEndPoint)lis.LocalEndpoint).Port);
        far = lis.AcceptTcpClient();
        var t = new Thread(ReadFar) { IsBackground = true }; t.Start();
        Set("cts", new CancellationTokenSource()); Set("account", A); Set("stream", near.GetStream()); Set("helloWritten", true);
        if (before != null) before(this);
        if (GateT != null && gate)
        {
            Gate = Activator.CreateInstance(GateT, true);
            GateT.GetField("UpkeepMs", BF).SetValue(Gate, 10L);
            GateT.GetField("AnchorGapMs", BF).SetValue(Gate, (long)AnchorMs);
            Set("settleGate", Gate);
            string p = Path.Combine(Path.GetTempPath(), "vl-switch-" + Guid.NewGuid().ToString("N") + ".txt");
            if (switches != null) File.WriteAllText(p, switches);
            Set("settleSwitchPath", p); SwitchPath = p;
            Run("StartSettleGate");
        }
        Run("SubscribeOrderUpdate", A); Run("SubscribeOrderUpdate", B);
        Run("SubscribePositionUpdatesAllSim");
    }
    public string SwitchPath;
    void ReadFar()
    {
        try
        {
            var s = far.GetStream(); var h = new byte[4];
            while (true)
            {
                s.ReadExactly(h, 0, 4); int n = (h[0] << 24) | (h[1] << 16) | (h[2] << 8) | h[3];
                var b = new byte[n]; s.ReadExactly(b, 0, n); Frames.Enqueue(Encoding.UTF8.GetString(b));
            }
        }
        catch { }
    }
    public bool Trusted(Account a) { return Gate == null || (bool)GateT.GetMethod("IsTrusted", BF).Invoke(Gate, new object[] { GateT.GetMethod("Key", BF | BindingFlags.Static).Invoke(null, new object[] { a.Name, "MNQ 12-26" }) }); }
    // A round trip makes the key known and flat with a seen Flat event; then wait for trust.
    public void Seen(Account a) { if (Gate == null) return; Account.SimWait(() => a.RaisePos()); P.Until(() => Trusted(a), 3000); if (!Trusted(a)) throw new Exception("key never trusted"); }
    public void Frame(string json) { Run("HandleFrame", json); }
    public void Signal(Account a, string id, string side, int qty, string extra = "")
    {
        Frame("{\"type\":\"signal\",\"payload\":{\"symbol\":\"MNQ\",\"side\":\"" + side + "\",\"quantity\":" + qty + ",\"entry\":100,\"stop_loss\":90,\"take_profit\":110,\"signal_id\":\"" + id + "\",\"timestamp\":\"" + DateTime.UtcNow.ToString("o") + "\",\"account\":\"" + a.Name + "\"" + extra + "}}");
    }
    public void Open(Account a, string id, string side, int qty) { Signal(a, id, side, qty); var o = a.Find(id); Account.SimWait(() => a.FillNow(o, qty)); }
    public static string CloseJson(string acct, string id, string side, double limit = 0)
    {
        return "{\"type\":\"close_position\",\"payload\":{\"symbol\":\"MNQ\",\"signal_id\":\"" + id + "\",\"side\":\"" + side + "\",\"quantity\":0" + (limit > 0 ? ",\"limit_price\":" + limit : "") + (acct == null ? "" : ",\"account\":\"" + acct + "\"") + "}}";
    }
    public void Close(Account a, string id, string side, double limit = 0) { Frame(CloseJson(a.Name, id, side, limit)); }
    public void Stop(Account a, string id, string side, int qty) { Frame("{\"type\":\"place_protective_stop\",\"payload\":{\"symbol\":\"MNQ\",\"signal_id\":\"" + id + "\",\"position_side\":\"" + side + "\",\"quantity\":" + qty + ",\"stop_price\":90,\"account\":\"" + a.Name + "\",\"reason\":\"t\"}}"); }
    // the order half of a stop fill: triggered, filled, callback done - position not moved yet
    public void StopFilledPosLags(Account a, string slEnd, int qty) { var sl = a.Find(slEnd); Account.SimWait(() => { a.State(sl, OrderState.Working); a.FillEvent(sl, qty); }); }
    public int FramesWith(string s) { return Frames.Count(f => f.Contains(s)); }
    public string Seq(Account a) { return string.Join(" | ", a.Calls); }
    public void Dispose()
    {
        try { Run("StopSettleGate"); } catch { }
        NinjaScript.OnLog = null; A.OnCall = null; B.OnCall = null;
        try { near.Close(); far.Close(); lis.Stop(); } catch { }
        try { if (SwitchPath != null && SwitchPath.Contains("vl-switch-") && File.Exists(SwitchPath)) File.Delete(SwitchPath); } catch { }
    }
}

static class P
{
    static int pass, fail;
    static void Ok(string name, bool ok, string detail = "") { if (ok) pass++; else fail++; Console.WriteLine((ok ? "PASS " : "FAIL ") + name + (ok ? "" : "  <- " + detail)); }
    public static bool Until(Func<bool> f, int ms) { var s = Stopwatch.StartNew(); while (s.ElapsedMilliseconds < ms) { if (f()) return true; Thread.Sleep(1); } return f(); }
    static readonly List<(string, Action<string>)> Tests = new List<(string, Action<string>)>();
    static void T(string n, Action<string> a) { Tests.Add((n, a)); }

    static int Main(string[] args)
    {
        // HARD STOP: this harness must never reach the live bot's port.
        Console.WriteLine("gate compiled in: " + (Rig.GateT != null));
        if (args.Length > 0 && args[0] == "seq") { SeqMode(); return 0; }
        Define();
        foreach (var (n, a) in Tests)
        {
            if (args.Length > 1 && args[0] == "one" && !n.StartsWith(args[1])) continue;
            try { a(n); } catch (Exception ex) { Ok(n, false, "threw " + (ex.InnerException ?? ex).Message); }
        }
        Console.WriteLine("settlewire: " + pass + " PASS, " + fail + " FAIL");
        Console.Out.Flush();
        Environment.Exit(fail == 0 ? 0 : 1);
        return 0;
    }

    static void Define()
    {
        T("W1 06:47: close while the stop fill has not moved the position: no reverse", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1);
                r.StopFilledPosLags(r.A, "e1-sl", 1);
                r.Close(r.A, "c1", "short");
                int atFrame = r.A.Count("Flatten pos=-1");
                Account.SimWait(() => r.A.MovePos(+1));
                Until(() => r.A.Count("Flatten") > 0, 1500); Thread.Sleep(400);
                Ok(n, atFrame == 0 && r.A.Pos == 0 && r.A.Count("Close2") == 0 && r.A.Count("Flatten pos=0") == 1, r.Seq(r.A) + " pos=" + r.A.Pos);
            }
        });
        T("W2 settled close runs on the reader, at once, as today", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "long", 2); Thread.Sleep(20);
                r.Close(r.A, "c1", "long");
                bool now = r.A.Count("Flatten pos=2") == 1;
                Until(() => r.A.Pos == 0, 2000);
                Ok(n, now && r.A.Pos == 0 && r.A.Count("Flatten") == 1, r.Seq(r.A));
            }
        });
        T("W3 heartbeat is acked while a close is held", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); r.StopFilledPosLags(r.A, "e1-sl", 1);
                r.Close(r.A, "c1", "short"); int f = r.A.Count("Flatten");
                r.Frame("{\"type\":\"heartbeat\",\"payload\":{}}");
                bool ack = Until(() => r.FramesWith("\"acks\":\"heartbeat\"") == 1, 300);
                Ok(n, f == 0 && ack && r.A.Count("Flatten") == 0, "flattens at frame " + f + " ack " + ack);
                Account.SimWait(() => r.A.MovePos(+1)); Until(() => r.A.Count("Flatten") == 1, 1500);
            }
        });
        T("W4 11-20: cancel_order on an entry that has already filled keeps its bracket", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A);
                r.Signal(r.A, "e1", "long", 1, ",\"order_type\":\"limit\",\"limit_price\":99");
                var o = r.A.Find("e1"); o.Filled = 1; o.OrderState = OrderState.Filled; // live state, callback not run yet
                r.Frame("{\"type\":\"cancel_order\",\"payload\":{\"signal_id\":\"e1\",\"account\":\"SimA\"}}");
                Account.SimWait(() => { r.A.FillEvent(o, 1); r.A.MovePos(1); });
                Ok(n, r.A.Count("Submit e1-sl Sell StopMarket 1") == 1, r.Seq(r.A));
            }
        });
        T("W4b a really cancelled entry drops its note (a later Filled of that id places nothing)", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A);
                r.Signal(r.A, "e1", "long", 1, ",\"order_type\":\"limit\",\"limit_price\":99");
                var o = r.A.Find("e1");
                r.Frame("{\"type\":\"cancel_order\",\"payload\":{\"signal_id\":\"e1\",\"account\":\"SimA\"}}");
                Until(() => o.OrderState == OrderState.Cancelled, 1000); Account.SimWait(() => { });
                Account.SimWait(() => r.A.FillEvent(new Order { Name = "e1", OrderId = "zz", Account = r.A, Instrument = Instrument.Mnq, OrderAction = OrderAction.Buy, Quantity = 1 }, 1));
                Ok(n, r.A.Count("Submit e1-sl") == 0, r.Seq(r.A));
            }
        });
        T("W5 10-30: two limit closes 4 ms apart place one -lx", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); Thread.Sleep(20);
                r.Close(r.A, "c1", "short", 101); Thread.Sleep(4); r.Close(r.A, "c2", "short", 101);
                int lx = r.A.Calls.Count(c => c.Contains("-lx"));
                bool snap = Rig.GateT == null || Until(() => r.FramesWith("\"reason\":\"lx_working\"") == 1, 500);
                Ok(n, lx == 1 && snap, r.Seq(r.A));
            }
        });
        T("W5b a cancelled -lx does not block the next one", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); Thread.Sleep(20);
                r.Close(r.A, "c1", "short", 101); var lx = r.A.Find("c1-lx");
                Account.SimWait(() => r.A.State(lx, OrderState.Cancelled));
                r.Close(r.A, "c2", "short", 101);
                Ok(n, r.A.Count("Submit c2-lx") == 1, r.Seq(r.A));
            }
        });
        T("W5c Submit of the -lx throws: market flatten, and the next limit close is placed", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); Thread.Sleep(20);
                r.A.ThrowOnSubmit = true; r.Close(r.A, "c1", "short", 101); r.A.ThrowOnSubmit = false;
                int fl = r.A.Count("Flatten pos=-1");
                Until(() => r.A.Pos == 0, 2000); Thread.Sleep(400);
                r.Open(r.A, "e2", "short", 1); Thread.Sleep(20); r.Close(r.A, "c2", "short", 101);
                Ok(n, fl == 1 && r.A.Count("Submit c2-lx") == 1, r.Seq(r.A));
            }
        });
        T("W6 our resting -lx is swept on flat; a manual order and a resting entry are not", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); Thread.Sleep(20);
                r.Signal(r.A, "e9", "long", 1, ",\"order_type\":\"limit\",\"limit_price\":50");
                var man = new Order { Account = r.A, Instrument = Instrument.Mnq, Name = "m-lx", OrderAction = OrderAction.BuyToCover, OrderType = OrderType.Limit, Quantity = 1, OrderId = "man" };
                r.A.Submit(new[] { man });
                r.Close(r.A, "c1", "short", 101);
                var sl = r.A.Find("e1-sl");
                Account.SimWait(() => { r.A.State(sl, OrderState.Working); r.A.FillNow(sl, 1); });
                bool c = Until(() => r.A.Find("c1-lx").Terminal, 1500);
                Ok(n, c && r.A.Count("Cancel c1-lx") >= 1 && r.A.Count("Cancel m-lx") == 0 && r.A.Count("Cancel e9") == 0, r.Seq(r.A));
            }
        });
        T("W7a place-stop on a flat account places nothing", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Stop(r.A, "s1", "LONG", 1);
                bool ack = Rig.GateT == null || Until(() => r.FramesWith("place_protective_stop_flat") == 1, 500);
                Ok(n, r.A.Count("Submit") == 0 && ack, r.Seq(r.A));
            }
        });
        T("W7b place-stop on the opposite side places nothing", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); Thread.Sleep(20); r.Stop(r.A, "s1", "LONG", 1);
                Ok(n, r.A.Count("Submit s1-sl") == 0, r.Seq(r.A));
            }
        });
        T("W7c place-stop on a held position is placed, capped at what is held", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "long", 2); Thread.Sleep(20);
                r.Stop(r.A, "s1", "LONG", 5); int five = r.A.Count("Submit s1-sl Sell StopMarket 5"), two = r.A.Count("Submit s1-sl Sell StopMarket 2");
                r.Stop(r.A, "s2", "LONG", 1); int one = r.A.Count("Submit s2-sl Sell StopMarket 1");
                Ok(n, two == 1 && five == 0 && one == 1, r.Seq(r.A));
            }
        });
        T("X11/W7d place-stop never waits: on disagreement it is placed at once, as asked", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); r.StopFilledPosLags(r.A, "e1-sl", 1);
                var sw = Stopwatch.StartNew(); r.Stop(r.A, "s1", "SHORT", 3); sw.Stop();
                Ok(n, r.A.Count("Submit s1-sl BuyToCover StopMarket 3") == 1 && sw.ElapsedMilliseconds < 100, r.Seq(r.A));
                Account.SimWait(() => r.A.MovePos(1));
            }
        });
        T("W7e a stop placed into a closing account is swept when the Flat comes", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); Thread.Sleep(20);
                r.Close(r.A, "c1", "short"); r.Stop(r.A, "s1", "SHORT", 1);
                bool placed = r.A.Count("Submit s1-sl") == 1;
                bool gone = Until(() => r.A.Find("s1-sl") != null && r.A.Find("s1-sl").Terminal, 2500);
                Ok(n, placed && gone && r.A.Pos == 0, r.Seq(r.A));
            }
        });
        T("W8 force-flat shape (side long on a SHORT), held, still flattens", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 2);
                var tp = r.A.Find("e1-tp"); tp.Quantity = 2;
                Account.SimWait(() => r.A.FillEvent(tp, 1)); // 1 of 2, position lags
                r.Close(r.A, "c1", "long"); int at = r.A.Count("Flatten");
                Account.SimWait(() => r.A.MovePos(1));
                Until(() => r.A.Count("Flatten pos=-1") == 1, 1500); Until(() => r.A.Pos == 0, 1500);
                Ok(n, at == 0 && r.A.Count("Flatten pos=-1") == 1 && r.A.Pos == 0, r.Seq(r.A));
            }
        });
        T("W9 an entry frame releases the held close first: Flatten before Submit", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); r.StopFilledPosLags(r.A, "e1-sl", 1);
                r.Close(r.A, "c1", "short"); int at = r.A.Count("Flatten");
                r.Signal(r.A, "e2", "long", 1);
                var l = r.A.Calls.ToList(); int fi = l.FindIndex(c => c.StartsWith("Flatten")), si = l.FindIndex(c => c.StartsWith("Submit e2"));
                Ok(n, at == 0 && fi >= 0 && si > fi && r.A.Count("Flatten") == 1, r.Seq(r.A));
                Account.SimWait(() => r.A.MovePos(1));
            }
        });
        T("W9b a REFUSED entry frame releases nothing; another account's entry releases nothing", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Seen(r.B); r.Open(r.A, "e1", "short", 1); r.StopFilledPosLags(r.A, "e1-sl", 1);
                r.Close(r.A, "c1", "short"); int at = r.A.Count("Flatten");
                r.Signal(r.A, "e1", "long", 1);               // duplicate signal id: refused
                r.Signal(r.B, "e3", "long", 1);               // other account
                Ok(n, at == 0 && r.A.Count("Flatten") == 0 && r.B.Count("Submit e3") == 1, r.Seq(r.A));
                Account.SimWait(() => r.A.MovePos(1)); Until(() => r.A.Count("Flatten") == 1, 1500);
            }
        });
        T("W10 Terminated while held: the close is released", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); r.StopFilledPosLags(r.A, "e1-sl", 1);
                r.Close(r.A, "c1", "short"); int at = r.A.Count("Flatten");
                r.C.State = State.Terminated; r.Run("OnStateChange");
                Ok(n, at == 0 && r.A.Count("Flatten") == 1, r.Seq(r.A));
                Account.SimWait(() => r.A.MovePos(1));
            }
        });
        T("W11 non-SIM, unknown and unnamed accounts keep today's answers", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.B.Simulation = false;
                r.Close(r.B, "c1", "long"); r.Frame(Rig.CloseJson("Nope", "c2", "long"));
                bool two = Until(() => r.FramesWith("position_close_rejected") == 2, 500);
                r.Open(r.A, "e1", "short", 1); r.StopFilledPosLags(r.A, "e1-sl", 1);
                r.Frame(Rig.CloseJson(null, "c3", "short"));
                Ok(n, two && r.B.Count("Flatten") == 0 && r.A.Count("Flatten pos=-1") == 1, r.Seq(r.A));
                Account.SimWait(() => r.A.MovePos(1));
            }
        });
        T("W12 maintenance_ack reports the held close", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); r.StopFilledPosLags(r.A, "e1-sl", 1);
                r.Close(r.A, "c1", "short");
                r.Frame("{\"type\":\"maintenance\",\"payload\":{\"held\":true,\"job_id\":\"j\"}}");
                bool one = Until(() => r.Frames.Any(f => f.Contains("maintenance_ack") && f.Contains("\"queued_commands\":1")), 500);
                Account.SimWait(() => r.A.MovePos(1)); Until(() => r.A.Count("Flatten") == 1, 1500); Thread.Sleep(50);
                r.Frame("{\"type\":\"maintenance\",\"payload\":{\"held\":true,\"job_id\":\"j2\"}}");
                bool zero = Until(() => r.Frames.Any(f => f.Contains("\"j2\"") && f.Contains("\"queued_commands\":0")), 500);
                Ok(n, one && zero, "one=" + one + " zero=" + zero);
            }
        });
        T("W13 the ledger handler is first in NT8's delegate list", n =>
        {
            using (var r = new Rig())
            {
                var ev = (Delegate)typeof(Account).GetField("OrderUpdate", BindingFlags.Instance | BindingFlags.NonPublic).GetValue(r.A);
                var names = ev.GetInvocationList().Select(d => d.Method.Name).ToList();
                Ok(n, names.Count == 2 && names[0] == "OnLedgerOrderUpdate" && names[1] == "OnOrderUpdate", string.Join(",", names));
            }
        });
        T("W15 never settles: closes anyway at 1000 ms, never before", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); r.StopFilledPosLags(r.A, "e1-sl", 1);
                var sw = Stopwatch.StartNew(); r.Close(r.A, "c1", "short");
                Until(() => r.A.Count("Flatten") == 1, 2000); sw.Stop();
                Ok(n, sw.ElapsedMilliseconds >= 985 && sw.ElapsedMilliseconds < 1250, "at " + sw.ElapsedMilliseconds + " " + r.Seq(r.A));
                Account.SimWait(() => r.A.MovePos(1));
            }
        });
        T("W16 settle_ms=0 written to the switch file releases the held close at once", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); r.StopFilledPosLags(r.A, "e1-sl", 1);
                var sw = Stopwatch.StartNew(); r.Close(r.A, "c1", "short"); int at = r.A.Count("Flatten");
                if (r.SwitchPath != null) File.WriteAllText(r.SwitchPath, "settle_ms=0\r\n");
                Until(() => r.A.Count("Flatten") == 1, 2000); sw.Stop();
                Ok(n, at == 0 && sw.ElapsedMilliseconds < 600, "at " + sw.ElapsedMilliseconds);
                Account.SimWait(() => r.A.MovePos(1));
            }
        });
        T("W17 link down: nothing is held", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); r.StopFilledPosLags(r.A, "e1-sl", 1);
                r.Set("stream", null); r.Close(r.A, "c1", "short");
                Ok(n, r.A.Count("Flatten pos=-1") == 1, r.Seq(r.A));
                Account.SimWait(() => r.A.MovePos(1));
            }
        });
        T("X1 second close between the take and the Flatten: one Flatten of the position", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "long", 2); var tp = r.A.Find("e1-tp");
                Account.SimWait(() => r.A.FillEvent(tp, 1)); // 1 of 2, position lags: held
                int sent = 0; Thread second = null;
                NinjaScript.OnLog = m =>
                {
                    if (m.Contains("close_position MNQ") && Interlocked.Exchange(ref sent, 1) == 0)
                    {
                        second = new Thread(() => r.Close(r.A, "c2", "long")) { IsBackground = true }; second.Start();
                        Thread.Sleep(80); // the window the settle thread stands in
                    }
                };
                r.Close(r.A, "c1", "long"); int at = r.A.Count("Flatten");
                Account.SimWait(() => r.A.MovePos(-1));
                Until(() => sent == 1 && second != null && second.Join(0), 2500); Until(() => r.A.Pos == 0, 2000); Thread.Sleep(600);
                Ok(n, at == 0 && r.A.Count("Flatten pos=1") == 1 && r.A.Count("Close2") == 1 && r.A.Pos == 0, r.Seq(r.A) + " pos=" + r.A.Pos);
            }
        });
        T("X2 entry between the take and the Flatten: Flatten before Submit", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); r.StopFilledPosLags(r.A, "e1-sl", 1);
                int sent = 0;
                NinjaScript.OnLog = m =>
                {
                    if (m.Contains("close_position MNQ") && Interlocked.Exchange(ref sent, 1) == 0)
                    { var t = new Thread(() => r.Signal(r.A, "e2", "long", 1)) { IsBackground = true }; t.Start(); Thread.Sleep(80); }
                };
                r.Close(r.A, "c1", "short"); int at = r.A.Count("Flatten");
                Account.SimWait(() => r.A.MovePos(1));
                Until(() => r.A.Count("Submit e2") == 1, 2000);
                var l = r.A.Calls.ToList(); int fi = l.FindIndex(c => c.StartsWith("Flatten")), si = l.FindIndex(c => c.StartsWith("Submit e2"));
                Ok(n, at == 0 && fi >= 0 && si > fi, r.Seq(r.A));
            }
        });
        foreach (var what in new[] { "resolve", "fullname", "positions", "filled" })
            for (int i = 0; i < 5; i++)
            {
                string w = what; int k = i;
                T("X3 a throw in the gate (" + w + " #" + k + "): the close runs exactly once", n =>
                {
                    using (var r = new Rig())
                    {
                        r.Seen(r.A); r.Open(r.A, "e1", "short", 1); r.StopFilledPosLags(r.A, "e1-sl", 1);
                        if (w == "resolve") VLInstrumentLookup.ThrowIn = k;
                        if (w == "fullname") Instrument.ThrowFullNameIn = k;
                        if (w == "positions") r.A.ThrowPositionsIn = k;
                        if (w == "filled") Order.ThrowFilledIn = k;
                        r.Close(r.A, "c1", "short"); Thread.Sleep(30);
                        Account.SimWait(() => r.A.MovePos(1));
                        Until(() => r.A.Count("Flatten") + r.FramesWith("position_close_rejected") > 0, 1500); Thread.Sleep(500);
                        int runs = r.A.Count("Flatten") + r.FramesWith("position_close_rejected");
                        Ok(n, runs == 1, "runs=" + runs + " " + r.Seq(r.A));
                    }
                });
            }
        T("X4 settle thread stuck in account A's release: account B is flattened by 1.25 s", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Seen(r.B);
                r.Open(r.A, "a1", "short", 1); r.Open(r.B, "b1", "short", 1);
                var slA = r.A.Find("a1-sl"); var slB = r.B.Find("b1-sl");
                Account.SimWait(() => { r.A.State(slA, OrderState.Working); r.A.FillEvent(slA, 1); r.B.State(slB, OrderState.Working); r.B.FillEvent(slB, 1); });
                var block = new ManualResetEventSlim(); bool stuck = false;
                r.A.OnCall = c => { if (c.StartsWith("Flatten")) { stuck = true; block.Wait(8500); } };
                r.Close(r.A, "ca", "short"); int a0 = r.A.Count("Flatten");
                Account.SimWait(() => r.A.MovePos(1));
                Until(() => stuck, 1500);
                var sw = Stopwatch.StartNew(); r.Close(r.B, "cb", "short");
                Until(() => r.B.Count("Flatten") == 1, 3000); sw.Stop();
                // a second command for A waits for the one that is running
                var t = new Thread(() => r.Close(r.A, "ca2", "short")) { IsBackground = true }; t.Start();
                bool waited = !t.Join(200);
                int aNow = r.A.Count("Flatten");
                block.Set(); t.Join(3000);
                Ok(n, a0 == 0 && stuck && sw.ElapsedMilliseconds <= 1250 && waited && aNow == 1, "B at " + sw.ElapsedMilliseconds + " waited=" + waited + " a=" + aNow);
                Account.SimWait(() => r.B.MovePos(1));
            }
        });
        T("X4b B was already held when the settle thread got stuck: its own timer releases it", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Seen(r.B);
                r.Open(r.A, "a1", "short", 1); r.Open(r.B, "b1", "short", 1);
                var slA = r.A.Find("a1-sl"); var slB = r.B.Find("b1-sl");
                Account.SimWait(() => { r.A.State(slA, OrderState.Working); r.A.FillEvent(slA, 1); r.B.State(slB, OrderState.Working); r.B.FillEvent(slB, 1); });
                var block = new ManualResetEventSlim(); bool stuck = false;
                r.A.OnCall = c => { if (c.StartsWith("Flatten")) { stuck = true; block.Wait(8500); } };
                r.Close(r.A, "ca", "short");
                var sw = Stopwatch.StartNew(); r.Close(r.B, "cb", "short"); int b0 = r.B.Count("Flatten");
                Account.SimWait(() => r.A.MovePos(1));
                Until(() => stuck, 1500);
                Until(() => r.B.Count("Flatten") == 1, 3000); sw.Stop(); block.Set();
                Ok(n, b0 == 0 && stuck && sw.ElapsedMilliseconds >= 985 && sw.ElapsedMilliseconds <= 1250, "B at " + sw.ElapsedMilliseconds);
                Account.SimWait(() => r.B.MovePos(1));
            }
        });
        T("X6 10-22: position restored silently (no event): the first close has 0 ms delay", n =>
        {
            using (var r = new Rig(null, x => x.A.SetPos(-1)))
            {
                bool early; r.Close(r.A, "c0", "short"); early = r.A.Count("Flatten pos=-1") == 1; // before any anchor
                Until(() => r.A.Pos == 0, 1500);
            }
            using (var r = new Rig(null, x => x.A.SetPos(-1)))
            {
                Until(() => r.Trusted(r.A), 2000);
                var sw = Stopwatch.StartNew(); r.Close(r.A, "c1", "short"); sw.Stop();
                Ok(n, r.A.Count("Flatten pos=-1") == 1 && sw.ElapsedMilliseconds < 200, r.Seq(r.A));
            }
        });
        T("X9 an orphan opposite -lx does not stop the exit", n =>
        {
            using (var r = new Rig("own_exit_sweep=0"))
            {
                r.Seen(r.A); r.Open(r.A, "e1", "long", 1); Thread.Sleep(20);
                r.Close(r.A, "c1", "long", 120); // resting Sell -lx
                var sl = r.A.Find("e1-sl"); Account.SimWait(() => { r.A.State(sl, OrderState.Working); r.A.FillNow(sl, 1); });
                Thread.Sleep(50); r.Open(r.A, "e2", "short", 1); Thread.Sleep(50);
                r.Close(r.A, "c2", "short", 80);
                Ok(n, r.A.Count("Submit c2-lx BuyToCover") == 1, r.Seq(r.A));
            }
        });
        T("X10 a new trade and its stop inside the sweep window survive", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); Thread.Sleep(20); r.Close(r.A, "c1", "short", 101);
                var sl = r.A.Find("e1-sl"); Account.SimWait(() => { r.A.State(sl, OrderState.Working); r.A.FillNow(sl, 1); });
                Account.SimWait(() => r.A.MovePos(-1)); // a new position the same instant (a resting entry of the owner)
                r.Stop(r.A, "s2", "SHORT", 1);
                Thread.Sleep(1300);
                Ok(n, r.A.Count("Cancel s2-sl") == 0 && r.A.Count("Cancel c1-lx") == 0, r.Seq(r.A));
            }
        });
        T("X12 live Filled before its event: held", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); Thread.Sleep(20);
                var sl = r.A.Find("e1-sl"); sl.Filled = 1; sl.OrderState = OrderState.Filled; // NT8 state moved, our handler has not run
                r.Close(r.A, "c1", "short"); int at = r.A.Count("Flatten");
                Account.SimWait(() => { r.A.FillEvent(sl, 1); r.A.MovePos(1); });
                Until(() => r.A.Count("Flatten") == 1, 1500); Thread.Sleep(300);
                Ok(n, at == 0 && r.A.Pos == 0 && r.A.Count("Close2") == 0, r.Seq(r.A));
            }
        });
        T("X14 the stop fills inside our Flatten wait: the second close waits 300 ms, then closes the reverse", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "short", 1); Thread.Sleep(20);
                var sl = r.A.Find("e1-sl"); var sw = Stopwatch.StartNew();
                r.A.OnCall = c => { if (c.StartsWith("Flatten pos=-1")) { sl.OrderState = OrderState.Accepted; } };
                r.Close(r.A, "c1", "short");                        // Flatten sized Short 1
                Account.SimWait(() => { if (!sl.Terminal) { } sl.OrderState = OrderState.Working; r.A.FillNow(sl, 1); }); // the stop still filled: flat
                r.Close(r.A, "c2", "short"); int at = r.A.Count("Flatten");
                Until(() => r.A.Pos == 1, 1000);                     // the first Close reverses
                Until(() => r.A.Count("Flatten pos=1") == 1, 2000); long ms = sw.ElapsedMilliseconds;
                Until(() => r.A.Pos == 0, 1500);
                Ok(n, at == 1 && ms >= 300 && r.A.Pos == 0, "ms=" + ms + " " + r.Seq(r.A) + " pos=" + r.A.Pos);
            }
        });
        T("X16 a rejected market exit lets the next close through", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "long", 1); Thread.Sleep(20);
                r.A.CloseRejects = true; r.A.CloseDelayMs = 20; r.Close(r.A, "c1", "long"); Thread.Sleep(80);
                r.A.CloseRejects = false; r.Close(r.A, "c2", "long");
                Until(() => r.A.Count("Flatten pos=1") == 2, 1200); Until(() => r.A.Pos == 0, 1000);
                Ok(n, r.A.Count("Flatten pos=1") == 2 && r.A.Pos == 0, r.Seq(r.A));
            }
        });
        T("X17 two entry frames: our own running Flatten is not repeated by an entry", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Open(r.A, "e1", "long", 1); Thread.Sleep(20);
                r.Close(r.A, "c1", "long"); r.Close(r.A, "c2", "long"); // second is held on B4
                int at = r.A.Count("Flatten");
                r.Signal(r.A, "e2", "long", 1, ",\"order_type\":\"limit\",\"limit_price\":50");
                int after = r.A.Count("Flatten");
                Until(() => r.A.Pos == 0, 1500); Thread.Sleep(400);
                Ok(n, at == 1 && after == 1 && r.A.Count("Flatten") == 1 && r.A.Count("Submit e2") == 1, r.Seq(r.A));
            }
        });
        foreach (var viaLx in new[] { false, true })
        {
            bool lx = viaLx;
            T("X5 Cancel raises while another thread holds NT8's lock (" + (lx ? "CancelBracketsFor" : "CancelAllBracketsFor") + "): no deadlock", n =>
            {
                using (var r = new Rig())
                {
                    r.Open(r.A, "e1", "short", 1); Thread.Sleep(20);
                    if (lx) r.Close(r.A, "c1", "short", 101);
                    var api = new object(); r.A.ApiLock = api;
                    var held = new ManualResetEventSlim(); var inCancel = new ManualResetEventSlim();
                    r.A.OnCall = c => { if (c.StartsWith("Cancel e1")) { inCancel.Set(); held.Wait(2000); } };
                    var other = new Order { Account = r.A, Instrument = Instrument.Mnq, Name = "other", OrderAction = OrderAction.Buy, Quantity = 1, OrderId = "x" };
                    var t2 = new Thread(() => { inCancel.Wait(2000); lock (api) { held.Set(); Thread.Sleep(50); r.A.State(other, OrderState.Working); } }) { IsBackground = true };
                    t2.Start();
                    var t1 = new Thread(() => { if (lx) r.A.FillEvent(r.A.Find("c1-lx"), 1); else { r.A.SetPos(0); r.A.RaisePos(); } }) { IsBackground = true };
                    t1.Start();
                    bool done = t1.Join(10000) && t2.Join(1000);
                    Ok(n, done && inCancel.IsSet, "deadlock: signalMapLock held across Account.Cancel");
                }
            });
        }
        T("W18 a Cancel that throws keeps the bracket tracked; one that works drops it", n =>
        {
            using (var r = new Rig())
            {
                r.Open(r.A, "e1", "short", 1); Thread.Sleep(20);
                int thrown = 0;
                r.A.OnCall = c => { if (c.StartsWith("Cancel e1") && thrown++ == 0) throw new InvalidOperationException("stub cancel"); };
                var map = (System.Collections.IDictionary)r.Get("placedBrackets");
                Account.SimWait(() => { r.A.SetPos(0); r.A.RaisePos(); }); bool kept = map.Contains("e1");
                Account.SimWait(() => r.A.RaisePos()); bool dropped = !map.Contains("e1");
                Ok(n, kept && dropped, "kept=" + kept + " dropped=" + dropped);
            }
        });
        T("W19 nothing is logged and no frame is sent from the ledger handler", n =>
        {
            using (var r = new Rig())
            {
                r.Seen(r.A); r.Run("UnsubscribeOrderUpdate", r.A); int l0 = NinjaScript.Lines.Count, f0 = r.Frames.Count;
                var o = new Order { Account = r.A, Instrument = Instrument.Mnq, Name = "x", OrderAction = OrderAction.Buy, Quantity = 1, OrderId = "x" };
                Account.SimWait(() => { r.A.State(o, OrderState.Working); r.A.FillEvent(o, 1); r.A.State(new Order { Account = r.A, Instrument = null, Name = "bad" }, OrderState.Working); });
                Ok(n, NinjaScript.Lines.Count == l0 && r.Frames.Count == f0, "lines " + (NinjaScript.Lines.Count - l0));
            }
        });
    }

    // seq: the call sequences of W1..W7 for the byte comparison with the base (W14).
    static void SeqMode()
    {
        string off = "settle_ms=0\nkeep_note=0\nstop_check=0\nown_exit_sweep=0\none_lx=0\n";
        Action<string, Action<Rig>> S = (name, f) => { using (var r = new Rig(off)) { f(r); Thread.Sleep(450); Console.WriteLine("SEQ " + name + " :: " + r.Seq(r.A) + " :: pos=" + r.A.Pos + " :: " + string.Join(",", r.Frames.Select(Kind))); } };
        S("W1", r => { r.Open(r.A, "e1", "short", 1); r.StopFilledPosLags(r.A, "e1-sl", 1); r.Close(r.A, "c1", "short"); Account.SimWait(() => r.A.MovePos(1)); });
        S("W2", r => { r.Open(r.A, "e1", "long", 2); r.Close(r.A, "c1", "long"); });
        S("W4", r => { r.Signal(r.A, "e1", "long", 1, ",\"order_type\":\"limit\",\"limit_price\":99"); var o = r.A.Find("e1"); o.Filled = 1; o.OrderState = OrderState.Filled; r.Frame("{\"type\":\"cancel_order\",\"payload\":{\"signal_id\":\"e1\",\"account\":\"SimA\"}}"); Account.SimWait(() => { r.A.FillEvent(o, 1); r.A.MovePos(1); }); });
        S("W5", r => { r.Open(r.A, "e1", "short", 1); r.Close(r.A, "c1", "short", 101); r.Close(r.A, "c2", "short", 101); });
        S("W6", r => { r.Open(r.A, "e1", "short", 1); r.Close(r.A, "c1", "short", 101); var sl = r.A.Find("e1-sl"); Account.SimWait(() => { r.A.State(sl, OrderState.Working); r.A.FillNow(sl, 1); }); });
        S("W7a", r => r.Stop(r.A, "s1", "LONG", 1));
        S("W7b", r => { r.Open(r.A, "e1", "short", 1); r.Stop(r.A, "s1", "LONG", 1); });
        S("W7c", r => { r.Open(r.A, "e1", "long", 2); r.Stop(r.A, "s1", "LONG", 5); });
        S("W9", r => { r.Open(r.A, "e1", "short", 1); r.StopFilledPosLags(r.A, "e1-sl", 1); r.Close(r.A, "c1", "short"); r.Signal(r.A, "e2", "long", 1); Account.SimWait(() => r.A.MovePos(1)); });
    }
    static string Kind(string f) { int i = f.IndexOf("\"type\":\"") + 8; return f.Substring(i, f.IndexOf('"', i) - i); }
}
