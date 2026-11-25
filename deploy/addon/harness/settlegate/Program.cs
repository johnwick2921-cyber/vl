// Pure unit harness for ninjascript/VLSettleGate.cs (design U1-U18, attack X6-X8, X13).
//   dotnet run -c Release                 -> unit tests
//   dotnet run -c Release -- replay <f>   -> Replay.cs over an event file (kept out of the repo)
using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Threading;
using NinjaTrader.NinjaScript.AddOns;

// One (account, instrument) with a hand clock. The "NT8 side" is Pos (the fresh read).
sealed class K
{
    public readonly VLSettleGate G;
    public readonly string Acct;
    public string Key;
    public void Key0(string inst) { Key = VLSettleGate.Key(Acct, inst); }
    public long Now;
    public int Pos;
    public readonly Dictionary<object, LiveOrder> LiveMap = new Dictionary<object, LiveOrder>(); // overrides of live state
    public K(VLSettleGate g = null, string acct = "SimA", long now = 1000000) { G = g ?? new VLSettleGate(); Acct = acct; Key = VLSettleGate.Key(acct, "MNQ 12-26"); Now = now; G.Tick(Now); }
    public K Anchor(bool eSeen = true)
    {
        int was;
        if (eSeen) G.NotePosition(Acct, Key, Pos, this, this, Now);
        G.Upkeep(Key, this, true, true, Pos, null, Now, out was); Now += 5000;
        if (G.Upkeep(Key, this, true, true, Pos, null, Now, out was) != UpkeepResult.Anchored) throw new Exception("not anchored");
        G.Tick(Now);
        return this;
    }
    public SettleVerdict J()
    {
        var live = new List<LiveOrder>();
        foreach (object r in G.LiveRefs(Key, Now)) { LiveOrder o; if (LiveMap.TryGetValue(r, out o)) live.Add(o); }
        return G.Judge(Key, true, Pos, live, Now);
    }
    public SettleVerdict JUnread() { return G.Judge(Key, false, 0, null, Now); }
    public void Ord(object o, bool buy, bool exit, SettleOrderKind k, SettlePhase p, int cum, string id = null) { G.NoteOrder(Key, o, id, buy, exit, k, p, cum, Now); }
    public void Ev(int pos) { Pos = pos; G.NotePosition(Acct, Key, pos, this, this, Now); }
    public long Hold(bool limit = false, string payload = "p1")
    {
        long t = G.NewToken();
        G.Hold(new HeldClose { Token = t, Account = Acct, Key = Key, IsLimit = limit, Payload = payload, ArrivedMs = Now, DeadlineMs = Now + G.Switches.SettleMs }, Now);
        return t;
    }
    // looks every `step` ms until released; returns the ms it took, -1 = not in `max`
    public long Until(long token, long max, out string why, int step = 10)
    {
        long t0 = Now; why = null;
        while (Now - t0 <= max) { why = G.CheckHeld(token, J(), Now); if (why != null) return Now - t0; Now += step; }
        return -1;
    }
}

static class Program
{
    static int pass, fail;
    static void Ok(string name, bool ok, string detail = "") { if (ok) pass++; else { fail++; Console.WriteLine("FAIL " + name + "  <- " + detail); } if (ok) Console.WriteLine("PASS " + name); }

    static int Main(string[] a)
    {
        if (a.Length > 1 && a[0] == "replay") return Replay.Run(a[1]);
        foreach (Action t in new Action[] { U1, U2, U3, U4, U5, U5b, U6, U7, U8, U9, U10, U11, U12, U13, U14, U15, U16, U17, U18, X6, X7, X8, X13, Stale, Prune, Conn })
        { try { t(); } catch (Exception ex) { Ok(t.Method.Name, false, "threw " + ex.Message); } }
        Console.WriteLine("settlegate: " + pass + " PASS, " + fail + " FAIL");
        return fail == 0 ? 0 : 1;
    }

    const SettleOrderKind STOP = SettleOrderKind.Stop, MKT = SettleOrderKind.Market, OTH = SettleOrderKind.Other;

    // 06:47: short 3, stop 2 + stop 1, close arrives before the last position event.
    static K Short3() { var k = new K().Anchor(); var e = new object(); k.Ord(e, false, false, MKT, SettlePhase.Filled, 3); k.Ev(-3); k.Now += 60000; return k; }
    static void U1()
    {
        var k = Short3(); object s1 = new object(), s2 = new object();
        k.Ord(s1, true, true, STOP, SettlePhase.Accepted, 0); k.Ord(s2, true, true, STOP, SettlePhase.Accepted, 0);
        k.Now += 100; k.Ord(s1, true, true, STOP, SettlePhase.Working, 0); k.Ord(s2, true, true, STOP, SettlePhase.Working, 0);
        k.Now += 57; k.Ord(s1, true, true, STOP, SettlePhase.Filled, 2); k.Now += 29; k.Ev(-1);
        k.Now += 39; k.Ord(s2, true, true, STOP, SettlePhase.Filled, 1);   // .225 ledger 0
        k.Now += 27;                                                       // .252 the close frame
        var v = k.J();
        Ok("U1 06:47 at the frame: L=0, C=-1, UNSETTLED", v.Kind == VerdictKind.Unsettled && v.L == 0 && v.C == -1, v.ToString());
        long tok = k.Hold(); k.Now += 2; k.Ev(0);                          // .254 flat
        string why; long ms = k.Until(tok, 2000, out why);
        Ok("U1 released settled, flat, two checks after the Flat event", why == "settled" && ms >= 20 && ms <= 40 && k.Pos == 0, why + " " + ms);
        var o = Short3(); o.G.Switches = new AddonSwitches(0, true, true, true, true); object pay; IDisposable tm;
        Ok("U1 with settle_ms=0 the frame runs at once (the RED shape)", o.G.OnCloseFrame(o.Key, false, "p", true, true, o.Now, out pay, out tm) == CloseRoute.RunNow);
    }
    static void U2()
    {
        var k = Short3(); k.Ord(new object(), true, true, STOP, SettlePhase.Filled, 3);
        long tok = k.Hold(); long t0 = k.Now; string why = null;
        k.Now = t0 + 999; string at999 = k.G.CheckHeld(tok, k.J(), k.Now);
        k.Now = t0 + 1000; why = k.G.CheckHeld(tok, k.J(), k.Now);
        Ok("U2 the position event never comes: released at exactly 1000 ms, not at 999", at999 == null && why != null && why.StartsWith("NOT settled after 1000"), at999 + "/" + why);
    }
    static void U3()
    {
        var k = Short3(); k.Ord(new object(), true, true, STOP, SettlePhase.Accepted, 0); k.Now += 50;
        var v = k.J(); Ok("U3 settled held, no triggered stop: 0 ms", v.Kind == VerdictKind.SettledHeld && v.Why == UnsettledWhy.None, v.ToString());
    }
    static void U4()
    {
        var k = Short3(); var s = new object();
        foreach (var p in new[] { SettlePhase.Accepted, SettlePhase.TriggerPending, SettlePhase.Changing })
        { k.Ord(s, true, true, STOP, p, 0); Ok("U4 a stop that is " + p + " is not imminent", k.J().Kind == VerdictKind.SettledHeld); }
        k.Ord(s, true, true, STOP, SettlePhase.Working, 0); var v = k.J();
        Ok("U4 a triggered stop holds although L=E=C", v.Kind == VerdictKind.Unsettled && v.Why == UnsettledWhy.TriggeredStop, v.ToString());
        long tok = k.Hold(); k.Now += 200; k.Ord(s, true, true, STOP, SettlePhase.PartFilled, 1);
        Ok("U4 part filled still held", k.G.CheckHeld(tok, k.J(), k.Now) == null); k.Ev(-2);
        k.Ord(s, true, true, STOP, SettlePhase.Filled, 3); k.Now += 5; Ok("U4 filled, no event yet: held", k.J().Kind == VerdictKind.Unsettled);
        k.Ev(0); string why; long ms = k.Until(tok, 1000, out why); Ok("U4 filled + event: released settled", why == "settled" && k.J().Kind == VerdictKind.SettledFlat, why + ms);
        var e = Short3(); e.Ord(new object(), true, false, STOP, SettlePhase.Working, 0);
        Ok("U4 a working stop ENTRY is not an exit", e.J().Kind == VerdictKind.SettledHeld);
        var m = Short3(); m.Ord(new object(), true, true, MKT, SettlePhase.Accepted, 0);
        Ok("U4 a live market exit holds", m.J().Why == UnsettledWhy.LiveMarketExit);
        var tpk = Short3(); tpk.Ord(new object(), true, true, OTH, SettlePhase.Working, 0);
        Ok("U4 a resting target does not hold", tpk.J().Kind == VerdictKind.SettledHeld);
    }
    static void U5()
    {
        var k = Short3(); long tok = k.Hold(); // unsettled for whatever reason, now equal for one look
        string a = k.G.CheckHeld(tok, k.J(), k.Now); k.Now += 19; string b = k.G.CheckHeld(tok, k.J(), k.Now);
        k.Ord(new object(), true, true, STOP, SettlePhase.Filled, 1); // leg 2 of the burst
        k.Now += 5; string c = k.G.CheckHeld(tok, k.J(), k.Now); k.Ev(-2); string d = k.G.CheckHeld(tok, k.J(), k.Now);
        k.Now += 20; string e = k.G.CheckHeld(tok, k.J(), k.Now);
        Ok("U5 equality on one look (19 ms) is not a release; the burst starts the count again", a == null && b == null && c == null && d == null && e == "settled", a + b + c + d + e);
    }
    static void U5b()
    {
        var k = Short3(); long tok = k.Hold(); k.G.CheckHeld(tok, k.J(), k.Now); k.Now += 10; object s = new object(); k.Ord(s, true, true, STOP, SettlePhase.Working, 0);
        string a = k.G.CheckHeld(tok, k.J(), k.Now); k.Now += 5; k.Ord(s, true, true, STOP, SettlePhase.Accepted, 0); k.Now += 10;
        string b = k.G.CheckHeld(tok, k.J(), k.Now); k.Now += 20; string c = k.G.CheckHeld(tok, k.J(), k.Now);
        Ok("U5 unsettled in between with the same position: the 20 ms start again", a == null && b == null && c == "settled", a + b + c);
    }
    static void U6()
    {
        var k = Short3(); k.Ord(new object(), true, true, OTH, SettlePhase.Filled, 2); k.Ev(-1); k.Now += 1;
        var v = k.J(); Ok("U6 leg 1 target filled, runner settled: 0 ms, the runner is closed", v.Kind == VerdictKind.SettledHeld && v.C == -1, v.ToString());
    }
    static void U7()
    {
        var k = new K().Anchor(); object o = new object();
        k.Ord(o, true, false, MKT, SettlePhase.PartFilled, 1); k.Ord(o, true, false, MKT, SettlePhase.PartFilled, 2); k.Ord(o, true, false, MKT, SettlePhase.PartFilled, 2);
        k.Ord(o, true, false, MKT, SettlePhase.PartFilled, 1); k.Ord(o, true, false, MKT, SettlePhase.Filled, 3); k.Ord(o, true, false, MKT, SettlePhase.Filled, 3);
        object sm = new object(); k.Ord(sm, true, false, MKT, SettlePhase.PartFilled, 2); k.Ord(sm, true, false, MKT, SettlePhase.PartFilled, 1); Ok("U7 a smaller value changes nothing", k.J().L == 5, k.J().ToString()); k.Ord(sm, true, false, MKT, SettlePhase.Cancelled, 2); k.G.NotePosition(k.Acct, k.Key, 0, k, k, k.Now); k.Ord(new object(), false, true, OTH, SettlePhase.Filled, 2);
        Ok("U7 repeats, growth and a smaller value count once", k.J().L == 3, k.J().ToString());
        k.Ord(new object(), true, false, MKT, SettlePhase.Filled, 3);
        Ok("U7 another Order object is another order", k.J().L == 6, k.J().ToString());
        object p = new object(); k.Ord(p, true, false, MKT, SettlePhase.PartFilled, 1, "id9"); k.Ord(new object(), true, false, MKT, SettlePhase.Filled, 2, "id9");
        Ok("U7 the same OrderId in a new object is the same order", k.J().L == 8, k.J().ToString());
    }
    static void U8()
    {
        var k = Short3(); k.G.NoteFlatten(k.Key, true, k.Now); k.Now += 5;
        Ok("U8 5 ms after our own Flatten: held (no Close order exists yet)", k.J().Why == UnsettledWhy.OwnFlatten);
        k.Now += 125; object c = new object(); k.Ord(c, true, true, MKT, SettlePhase.Submitted, 0); k.Ord(c, true, true, MKT, SettlePhase.Filled, 3); k.Ev(0); k.Now += 10;
        var lm = Short3(); lm.G.NoteFlatten(lm.Key, true, lm.Now); lm.Now += 130; lm.Ord(new object(), true, true, MKT, SettlePhase.Submitted, 0); lm.Ev(0); lm.Now += 300;
        Ok("U8 past 300 ms and flat, but its market exit is still live: not over", (lm.J().Why & UnsettledWhy.OwnFlatten) != 0, lm.J().ToString());
        Ok("U8 the Flat event does not clear it before 300 ms", k.J().Why == UnsettledWhy.OwnFlatten, k.J().ToString());
        k.Now += 161; Ok("U8 301 ms after, flat: settled", k.J().Kind == VerdictKind.SettledFlat, k.J().ToString());
        var r = Short3(); r.G.NoteFlatten(r.Key, true, r.Now); r.Now += 100; object c2 = new object(); r.Ord(c2, true, true, MKT, SettlePhase.Submitted, 0); r.Ord(c2, true, true, MKT, SettlePhase.Rejected, 0);
        Ok("U8 its market exit was rejected: the next close goes at once", r.J().Kind == VerdictKind.SettledHeld, r.J().ToString());
        var o = Short3(); object old = new object(); o.Ord(old, true, true, MKT, SettlePhase.Accepted, 0); o.Now += 2000; o.G.NoteFlatten(o.Key, true, o.Now); o.Now += 5; o.Ord(old, true, true, MKT, SettlePhase.Cancelled, 0);
        Ok("U8 an OLDER market order cancelled by the Flatten does not clear it", o.J().Why == UnsettledWhy.OwnFlatten, o.J().ToString());
        var n = Short3(); n.G.NoteFlatten(n.Key, true, n.Now); n.Now += 999; var b = n.J(); n.Now += 1; var d = n.J();
        Ok("U8 nothing ever comes: 1000 ms", b.Why == UnsettledWhy.OwnFlatten && d.Kind == VerdictKind.SettledHeld);
        var f = new K().Anchor(); f.G.NoteFlatten(f.Key, false, f.Now); Ok("U8 a Flatten of a flat account is nothing to wait for", f.J().Kind == VerdictKind.SettledFlat);
        var x = Short3(); x.G.NoteFlatten(x.Key, true, x.Now); x.G.NoteFlattenFailed(x.Key); Ok("U8 a Flatten that threw is nothing to wait for", x.J().Kind == VerdictKind.SettledHeld);
        var y = Short3(); y.G.NoteFlatten(y.Key, true, y.Now); y.Now += 130; y.Ord(new object(), true, true, MKT, SettlePhase.Submitted, 0);
        Ok("U8 own Flatten + its own market exit = only our own Flatten", VLSettleGate.OnlyOwnFlatten(y.J()));
        y.Ord(new object(), true, true, STOP, SettlePhase.Working, 0); Ok("U8 plus a triggered stop: not only", !VLSettleGate.OnlyOwnFlatten(y.J()));
        var z = Short3(); z.Ord(new object(), true, true, MKT, SettlePhase.Submitted, 0); Ok("U8 a market exit without our Flatten: not ours", !VLSettleGate.OnlyOwnFlatten(z.J()));
    }
    static void U9()
    {
        foreach (bool first in new[] { false, true }) foreach (bool second in new[] { false, true })
        {
            var k = Short3(); long tok = k.Hold(first, "first"); long t0 = k.Now; k.Now += 400; k.G.Tick(k.Now); object pay; IDisposable tm;
            var route = k.G.OnCloseFrame(k.Key, second, "second", true, true, k.Now, out pay, out tm); var h = k.G.PeekHeld(k.Key);
            bool wantSecond = first && !second;
            Ok("U9 " + (first ? "limit" : "market") + " then " + (second ? "limit" : "market"), route == CloseRoute.Joined && h.Token == tok && h.DeadlineMs == t0 + 1000
                && (string)h.Payload == (wantSecond ? "second" : "first") && h.IsLimit == (first && second), route + " " + h.Payload);
        }
        var s = Short3(); object so = new object(); s.Ord(so, true, true, STOP, SettlePhase.Filled, 1); long tk = s.Hold(); long a0 = s.Now; string why = null; int cum = 0;
        object big = new object(); s.Ord(big, true, true, OTH, SettlePhase.Working, 0);
        while (why == null && s.Now - a0 < 3000) { cum++; s.Ord(big, true, true, OTH, SettlePhase.PartFilled, cum); s.Now += 10; why = s.G.CheckHeld(tk, s.J(), s.Now); }
        Ok("U9 a stream of fills never extends the deadline", s.Now - a0 == 1000 && why.StartsWith("NOT settled"), (s.Now - a0) + why);
        var t = Short3(); t.Hold(false, "held"); object p2; IDisposable t2;
        Ok("U9 gate off: the held one and the new one run once (market wins)", t.G.OnCloseFrame(t.Key, true, "new", false, true, t.Now, out p2, out t2) == CloseRoute.RunNow && (string)p2 == "held" && t.G.PeekHeld(t.Key) == null);
        var u = Short3(); long ut = u.Hold(); Ok("U9 taken exactly once", u.G.Take(ut) != null && u.G.Take(ut) == null && u.G.PendingCount == 1);
        u.G.ReleaseDone(); Ok("U9 pending is 0 after the release", u.G.PendingCount == 0);
        var ci = Short3(); bool c0 = ci.G.CloseInFlight(ci.Key, ci.Now); long ct = ci.Hold(); bool c1 = ci.G.CloseInFlight(ci.Key, ci.Now); ci.G.Take(ct); ci.G.ReleaseDone(); bool c2 = ci.G.CloseInFlight(ci.Key, ci.Now);
        ci.G.NoteFlatten(ci.Key, true, ci.Now); bool c3 = ci.G.CloseInFlight(ci.Key, ci.Now + 299), c4 = ci.G.CloseInFlight(ci.Key, ci.Now + 300); Ok("U9 a close is in flight while held, and 300 ms after our Flatten", !c0 && c1 && !c2 && c3 && !c4);
        var w = Short3(); long wt = w.Hold(); w.G.Switches = new AddonSwitches(0, true, true, true, true); Ok("U9 settle_ms set to 0 releases a held close", w.G.CheckHeld(wt, w.J(), w.Now) == "gate switched off");
    }
    static void U10()
    {
        var k = Short3(); k.Ord(new object(), false, false, MKT, SettlePhase.Filled, 1); // counted, but NT8 never moved: L wrong by 1
        k.Now += 2000; long tok = k.Hold(); string why; long ms = k.Until(tok, 3000, out why); Ok("U10 ledger wrong: a close is at most 1000 ms late", ms == 1000, ms + why);
        k.G.Take(tok); k.G.ReleaseDone(); int was; var r1 = k.G.Upkeep(k.Key, k, true, true, -3, null, k.Now, out was); k.Now += 5000; var r2 = k.G.Upkeep(k.Key, k, true, true, -3, null, k.Now, out was);
        Ok("U10 two passes 5 s apart set it right", r1 == UpkeepResult.None && r2 == UpkeepResult.Resynced && was == -4 && k.J().Kind == VerdictKind.SettledHeld, r1 + " " + r2 + " " + was);
        var f = Short3(); f.Ord(new object(), true, true, STOP, SettlePhase.Filled, 1); f.Now += 500; f.G.Upkeep(f.Key, f, true, true, -3, null, f.Now, out was); Ok("U10 no resync while the last fill is fresh", f.J().L == -2);
        var e = Short3(); e.Ord(new object(), true, true, STOP, SettlePhase.Filled, 1); e.Now += 2000; e.G.Upkeep(e.Key, e, true, true, -2, null, e.Now, out was); e.Now += 5000; Ok("U10 no resync while the event disagrees (E=-3, C=-2, L=-2 is right anyway)", e.G.Upkeep(e.Key, e, true, true, -2, null, e.Now, out was) == UpkeepResult.None);
        var q = Short3(); q.Ord(new object(), true, true, STOP, SettlePhase.Filled, 1); q.Now += 500; q.G.Upkeep(q.Key, q, true, true, -3, null, q.Now, out was); q.Now += 5000; Ok("U10 the pass inside the fresh time does not count as the first", q.G.Upkeep(q.Key, q, true, true, -3, null, q.Now, out was) == UpkeepResult.None);
        var de = Short3(); de.Ord(new object(), true, true, STOP, SettlePhase.Filled, 2); de.Now += 2000; de.G.Upkeep(de.Key, de, true, true, -2, null, de.Now, out was); de.Now += 5000; Ok("U10 three different answers (L=-1 E=-3 C=-2): no resync", de.G.Upkeep(de.Key, de, true, true, -2, null, de.Now, out was) == UpkeepResult.None && de.J().L == -1);
        var m = Short3(); m.Ord(new object(), true, true, STOP, SettlePhase.Filled, 1); m.Now += 2000; m.G.Upkeep(m.Key, m, true, true, -3, null, m.Now, out was); m.Now += 2500; m.Ord(new object(), true, true, STOP, SettlePhase.Filled, 1); m.Now += 2500;
        Ok("U10 a fill between the two passes starts the count again", m.G.Upkeep(m.Key, m, true, true, -3, null, m.Now, out was) == UpkeepResult.None && m.J().L == -1);
    }
    static void U11()
    {
        var k = new K(); k.Pos = -1; Ok("U11 restart, key never seen: UNKNOWN (runs now)", k.J().Kind == VerdictKind.Unknown);
        k.Anchor(false); var v = k.J(); Ok("U11 anchored, held, no event ever: settled held, 0 ms", v.Kind == VerdictKind.SettledHeld && !v.ESeen, v.ToString());
        k.Pos = 0; k.Ord(new object(), true, true, STOP, SettlePhase.Filled, 1); Ok("U11 flat without a seen event is UNKNOWN, never 'flat'", k.J().Kind == VerdictKind.Unknown, k.J().ToString());
        var s = new K(); s.Anchor(true); s.Ord(new object(), false, false, MKT, SettlePhase.Filled, 1); s.Pos = -1; // E stays 0
        long tok = s.Hold(); string why; long ms = s.Until(tok, 2000, out why); Ok("U11 stale event 0, held: waits, then closed", ms == 1000 && why.StartsWith("NOT"), ms + why);
        Ok("U11 unreadable position is UNKNOWN", s.JUnread().Kind == VerdictKind.Unknown); Ok("U11 a held close whose state becomes UNKNOWN is released", s.G.CheckHeld(tok, s.JUnread(), s.Now - 500) == "state unknown");
        var p = new K(); int was; object part = new object(); p.Pos = 2;
        var seeds = new List<OrderSeed> { new OrderSeed { Ref = part, OrderId = "q", Buy = true, Kind = OTH, Phase = SettlePhase.PartFilled, Filled = 2 } };
        p.G.Upkeep(p.Key, p, true, true, 2, seeds, p.Now, out was); p.Now += 5000; p.G.Upkeep(p.Key, p, true, true, 2, seeds, p.Now, out was);
        p.Ord(part, true, false, OTH, SettlePhase.Filled, 5, "q"); p.Pos = 5;
        Ok("U11 an order part filled before the restart counts only what came after", p.J().L == 5 && p.J().Kind == VerdictKind.SettledHeld, p.J().ToString());
        var q = new K(); q.G.Upkeep(q.Key, q, true, true, 0, null, q.Now, out was); q.Now += 4999; Ok("U11 not anchored before 5 s", q.G.Upkeep(q.Key, q, true, true, 0, null, q.Now, out was) == UpkeepResult.None);
        q.Now += 1; Ok("U11 not anchored on a different read", q.G.Upkeep(q.Key, q, true, true, 1, null, q.Now, out was) == UpkeepResult.None && !q.G.IsTrusted(q.Key));
        var d = new K(); d.G.Upkeep(d.Key, d, true, true, 0, null, d.Now, out was); d.Now += 2500; d.Ord(new object(), true, false, MKT, SettlePhase.Filled, 1); d.Now += 2500; Ok("U11 not anchored across a fill", d.G.Upkeep(d.Key, d, true, true, 0, null, d.Now, out was) == UpkeepResult.None);
        var n = new K(); n.G.Upkeep(n.Key, n, false, true, 0, null, n.Now, out was); n.Now += 5000; n.G.Upkeep(n.Key, n, false, true, 0, null, n.Now, out was); Ok("U11 never anchored on a connection that is not Connected", !n.G.IsTrusted(n.Key));
    }
    static void U12()
    {
        int refusals = 0, bad = 0, n = 0;
        int[] vals = { 0, 2, -2, 99 }; // 99 = not known
        foreach (int l in vals) foreach (int e in vals) foreach (int c in vals) foreach (bool inflight in new[] { false, true }) foreach (VerdictKind kind in new[] { VerdictKind.Unknown, VerdictKind.Unsettled, VerdictKind.SettledFlat, VerdictKind.SettledHeld })
        {
            n++;
            var v = new SettleVerdict { Kind = kind, Trusted = l != 99, L = l == 99 ? 0 : l, ESeen = e != 99, E = e == 99 ? 0 : e, CKnown = c != 99, C = c == 99 ? 0 : c };
            int q; var r = StopDecision.Decide(true, 5, v, inflight, out q);
            bool allKnown = l != 99 && e != 99 && c != 99, agree = allKnown && l == e && e == c;
            var want = !inflight && agree && c == 0 ? StopAction.RefuseFlat : !inflight && agree && c < 0 ? StopAction.RefuseOpposite : StopAction.Place;
            int wantQ = kind == VerdictKind.SettledHeld && v.C > 0 ? 2 : 5;
            if (r != want || (r == StopAction.Place && q != wantQ)) bad++;
            if (r != StopAction.Place) refusals++;
        }
        Ok("U12 place-stop table " + n + " cases: refused only when three known sources agree", bad == 0 && refusals == 8, "bad " + bad + " refusals " + refusals);
        int q2; var vv = new SettleVerdict { Kind = VerdictKind.SettledHeld, Trusted = true, ESeen = true, CKnown = true, L = 3, E = 3, C = 3 };
        StopDecision.Decide(true, 2, vv, false, out q2); Ok("U12 asked 2, held 3: 2 (never more than asked)", q2 == 2);
        var s = new SettleVerdict { Kind = VerdictKind.SettledHeld, Trusted = true, ESeen = true, CKnown = true, L = -3, E = -3, C = -3 };
        Ok("U12 short side mirrored", StopDecision.Decide(false, 9, s, false, out q2) == StopAction.Place && q2 == 3 && StopDecision.Decide(true, 9, s, false, out q2) == StopAction.RefuseOpposite);
    }
    static List<LiveOrder> Live(SweepMark m, SettlePhase p) { var l = new List<LiveOrder>(); foreach (OwnExit x in m.Items) l.Add(new LiveOrder { Ref = x.Ref, Phase = p }); return l; }
    static SweepStep Step(K k, SweepMark m, SettlePhase p, out List<object> c) { string w; return k.G.CheckSweep(m, k.J(), Live(m, p), k.Now, out c, out w); }
    static void U13()
    {
        List<object> c; K k = Short3(); object lx = new object(), sl = new object();
        k.G.RegisterOwnExit(k.Key, lx, true, true, k.Now); k.G.MarkOwnExitSubmitted(lx); k.G.RegisterOwnExit(k.Key, sl, false, true, k.Now); k.G.MarkOwnExitSubmitted(sl);
        k.Ord(new object(), true, true, STOP, SettlePhase.Filled, 3); k.Ev(0); var m = k.G.MarksSnapshot()[0];
        var a = Step(k, m, SettlePhase.Working, out c); k.Now += 19; var b = Step(k, m, SettlePhase.Working, out c); k.Now += 1; var d = Step(k, m, SettlePhase.Working, out c);
        Ok("U13 flat on two checks 20 ms apart: cancel both, not before", a == SweepStep.Wait && b == SweepStep.Wait && d == SweepStep.Cancel && c.Count == 2, a + " " + b + " " + d);
        Ok("U13 all CancelPending: done", Step(k, m, SettlePhase.CancelPending, out c) == SweepStep.Done && c.Count == 0);
        K h = Short3(); object o = new object(); h.G.RegisterOwnExit(h.Key, o, true, true, h.Now); h.G.MarkOwnExitSubmitted(o); h.Ord(new object(), true, true, STOP, SettlePhase.Filled, 3); h.Ev(0); m = h.G.MarksSnapshot()[0];
        h.Ord(new object(), false, false, MKT, SettlePhase.Filled, 1); h.Ev(-1); Ok("U13 a position again: nothing cancelled, dropped", Step(h, m, SettlePhase.Working, out c) == SweepStep.Drop && c.Count == 0);
        K w = Short3(); o = new object(); w.G.RegisterOwnExit(w.Key, o, true, true, w.Now); w.G.MarkOwnExitSubmitted(o); w.Ev(0); // L still -3: the ledger disagrees
        m = w.G.MarksSnapshot()[0]; bool any = false; SweepStep last = SweepStep.Wait; for (int i = 0; i <= 101; i++) { last = Step(w, m, SettlePhase.Working, out c); if (c.Count > 0) any = true; if (last == SweepStep.Drop) break; w.Now += 10; }
        Ok("U13 ledger disagrees: nothing, dropped after the window", !any && last == SweepStep.Drop);
        K n = Short3(); object early = new object(), late = new object(); n.G.RegisterOwnExit(n.Key, early, true, true, n.Now); n.Ord(new object(), true, true, STOP, SettlePhase.Filled, 3); n.Ev(0); n.G.RegisterOwnExit(n.Key, late, false, true, n.Now); n.G.MarkOwnExitSubmitted(late);
        m = n.G.MarksSnapshot()[0]; Step(n, m, SettlePhase.Initialized, out c); n.Now += 25; var s1 = Step(n, m, SettlePhase.Initialized, out c); n.G.MarkOwnExitSubmitted(early); var s2 = Step(n, m, SettlePhase.Submitted, out c);
        Ok("X10 the list is fixed at the Flat event; an unsubmitted order waits for its Submit", s1 == SweepStep.Wait && s2 == SweepStep.Cancel && c.Count == 1 && ReferenceEquals(c[0], early), s1 + " " + s2);
        n.Ord(new object(), false, false, MKT, SettlePhase.Filled, 1); n.Now += 10; var s3 = Step(n, m, SettlePhase.Submitted, out c); Ok("X10 unsettled again in the middle: no cancel", s3 == SweepStep.Wait && c.Count == 0, s3.ToString());
        n.Ord(new object(), true, false, MKT, SettlePhase.Filled, 1); n.Now += 10; var s4 = Step(n, m, SettlePhase.Submitted, out c); n.Now += 20; var s5 = Step(n, m, SettlePhase.Submitted, out c);
        Ok("X10 settled again: two fresh checks before the next cancel", s4 == SweepStep.Wait && s5 == SweepStep.Cancel, s4 + " " + s5);
        var two = new VLSettleGate(); K ka = new K(two, "SimA"), kb = new K(two, "SimB"); object oa = new object(), ob = new object(); two.RegisterOwnExit(ka.Key, oa, true, true, 1); two.RegisterOwnExit(kb.Key, ob, true, true, 1); ka.Ev(0);
        Ok("U13 only the flat key's orders are listed", two.MarksSnapshot().Count == 1 && two.MarksSnapshot()[0].Items.Count == 1 && ReferenceEquals(two.MarksSnapshot()[0].Items[0].Ref, oa));
        K t = Short3(); object tt = new object(); t.G.RegisterOwnExit(t.Key, tt, true, true, t.Now); t.Ord(tt, true, true, OTH, SettlePhase.Filled, 3); t.Ev(0); Ok("U13 a filled -lx leaves the list, no mark", t.G.MarksSnapshot().Count == 0 && t.G.PendingCount == 0);
        K f = Short3(); object fo = new object(); f.G.RegisterOwnExit(f.Key, fo, true, true, f.Now); f.G.DropOwnExit(fo); f.Ev(0); Ok("U13 Submit threw: dropped", f.G.MarksSnapshot().Count == 0);
        K x = Short3(); x.G.Switches = new AddonSwitches(1000, true, true, false, true); x.G.RegisterOwnExit(x.Key, new object(), true, true, x.Now); x.Ev(0); Ok("U13 own_exit_sweep=0: no mark", x.G.MarksSnapshot().Count == 0);
        K y = Short3(); y.G.RegisterOwnExit(y.Key, new object(), true, true, y.Now); y.Ev(0); Ok("U13 marks count as pending", y.G.PendingCount == 1); y.G.StopSweeps(); y.Ev(0); Ok("U13 settle thread gone: no marks pile up", y.G.PendingCount == 0);
    }
    static void U14()
    {
        Ok("U14 working covers", LxDecision.Covers(true, SettlePhase.Working, 3, 9999, true, 3)); Ok("U14 accepted covers", LxDecision.Covers(true, SettlePhase.Accepted, 3, 9999, true, 3));
        Ok("U14 part filled covers the rest", LxDecision.Covers(true, SettlePhase.PartFilled, 2, 9999, true, 2)); Ok("U14 smaller than held: no", !LxDecision.Covers(true, SettlePhase.Working, 2, 10, true, 3));
        Ok("U14 submitted 1999 ms", LxDecision.Covers(true, SettlePhase.Submitted, 3, 1999, true, 3)); Ok("U14 submitted 2000 ms: no", !LxDecision.Covers(true, SettlePhase.Submitted, 3, 2000, true, 3));
        foreach (var p in new[] { SettlePhase.Initialized, SettlePhase.Unknown, SettlePhase.CancelPending, SettlePhase.Cancelled, SettlePhase.Filled, SettlePhase.Rejected, SettlePhase.Changing, SettlePhase.TriggerPending }) Ok("U14 " + p + ": no", !LxDecision.Covers(true, p, 3, 10, true, 3));
        Ok("U14 other side: no (X9)", !LxDecision.Covers(false, SettlePhase.Working, 3, 10, true, 3));
        K k = Short3(); object a = new object(), b = new object(); k.G.RegisterOwnExit(k.Key, a, true, true, k.Now); k.G.RegisterOwnExit(k.Key, b, false, true, k.Now); Ok("U14 a standalone -sl is not an -lx", k.G.OwnLx(k.Key).Count == 1);
        k.Ord(a, true, true, OTH, SettlePhase.Cancelled, 0); Ok("U14 terminal leaves the list", k.G.OwnLx(k.Key).Count == 0);
    }
    static void U15()
    {
        Ok("U15 Cancelled, 0 filled: removed", NoteDecision.OnCancelled(true, true, 0, true, 0, true) == NoteAction.Remove);
        Ok("U15 Cancelled, 2 filled, held long: bracket", NoteDecision.OnCancelled(true, true, 2, true, 2, true) == NoteAction.PlaceBracket);
        Ok("U15 Cancelled, 2 filled, held short (sell): bracket", NoteDecision.OnCancelled(true, true, 2, true, -2, false) == NoteAction.PlaceBracket);
        Ok("U15 filled but no event / flat / other side: kept, warned", NoteDecision.OnCancelled(true, true, 2, false, 2, true) == NoteAction.KeepAndWarn && NoteDecision.OnCancelled(true, true, 2, true, 0, true) == NoteAction.KeepAndWarn && NoteDecision.OnCancelled(true, true, 2, true, -1, true) == NoteAction.KeepAndWarn);
        Ok("U15 another Order object: untouched", NoteDecision.OnCancelled(true, false, 0, true, 0, true) == NoteAction.Leave && NoteDecision.OnCancelled(true, false, 2, true, 2, true) == NoteAction.Leave);
        Ok("U15 note gone (bracket placed): nothing", NoteDecision.OnCancelled(false, false, 2, true, 2, true) == NoteAction.Leave);
    }
    static string Sw(string t, AddonSwitches last = null) { List<string> w; return AddonSwitches.Parse(t, last, out w).ToString() + " w" + w.Count; }
    static void U16()
    {
        string def = "settle_ms=1000 keep_note=1 stop_check=1 own_exit_sweep=1 one_lx=1 w0";
        Ok("U16 absent = defaults", Sw(null) == def); Ok("U16 empty = defaults", Sw("") == def);
        Ok("U16 all off", Sw("settle_ms=0\nkeep_note=0\nstop_check=0\nown_exit_sweep=0\none_lx=0") == "settle_ms=0 keep_note=0 stop_check=0 own_exit_sweep=0 one_lx=0 w0");
        foreach (string key in new[] { "keep_note", "stop_check", "own_exit_sweep", "one_lx" }) Ok("U16 only " + key + " off", Sw(key + "=0") == def.Replace(key + "=1", key + "=0"));
        foreach (string bad in new[] { "-1", "2001", "abc", "", "1000.5", "+5", "1 000" }) { string got = Sw("settle_ms=" + bad); Ok("U16 settle_ms='" + bad + "' kept 1000, warned", got == def.Replace("w0", "w1"), got); }
        Ok("U16 2000 and 0 are allowed", Sw("settle_ms=2000").StartsWith("settle_ms=2000 ") && Sw("settle_ms=0").StartsWith("settle_ms=0 "));
        var last = new AddonSwitches(0, false, false, false, false);
        Ok("U16 a bad value keeps the LAST GOOD one, not the default", Sw("settle_ms=\nkeep_note=\nstop_check=x\nown_exit_sweep=2\none_lx=on", last) == "settle_ms=0 keep_note=0 stop_check=0 own_exit_sweep=0 one_lx=0 w5");
        Ok("U16 a key taken out of the file is its default", Sw("settle_ms=0", last) == def.Replace("settle_ms=1000", "settle_ms=0"));
        Ok("U16 unknown key, junk line: warned, ignored", Sw("foo=1\nhello\n") == def.Replace("w0", "w2"));
        Ok("U16 CRLF, BOM, spaces, case, comment", Sw("﻿Settle_MS = 500   # half\r\n\r\n# x\r\nKEEP_NOTE=0\r\n") == def.Replace("settle_ms=1000", "settle_ms=500").Replace("keep_note=1", "keep_note=0"));
    }
    static void U17()
    {
        var g = new VLSettleGate(); var a = new K(g, "SimA").Anchor(); var b = new K(g, "SimB", a.Now).Anchor();
        a.Ord(new object(), true, true, STOP, SettlePhase.Working, 0); a.G.NoteFlatten(a.Key, true, a.Now); b.Now = a.Now;
        Ok("U17 two accounts, same contract: no cross effect", a.J().Kind == VerdictKind.Unsettled && b.J().Kind == VerdictKind.SettledFlat);
        long t = a.Hold(); object p; IDisposable tm; g.Tick(b.Now); Ok("U17 a close held on A does not join B's", g.OnCloseFrame(b.Key, false, "b", true, true, b.Now, out p, out tm) == CloseRoute.Judge && g.Take(t) != null);
        Ok("U17 key is case-blind on the contract, exact on the account", VLSettleGate.Key("A", "mnq 12-26") == VLSettleGate.Key("A", "MNQ 12-26") && VLSettleGate.Key("a", "X") != VLSettleGate.Key("A", "X"));
    }
    static void U18()
    {
        var g = new VLSettleGate(); string key = VLSettleGate.Key("S", "MNQ"); int held = 0, ran = 0; var stop = new ManualResetEventSlim(); var sw = Stopwatch.StartNew(); int errs = 0;
        var ths = new List<Thread>(); g.Tick(0);
        for (int i = 0; i < 4; i++)
        {
            int id = i; var t = new Thread(() =>
            {
                try
                {
                    var rnd = new Random(id); var orders = new List<object>(); for (int j = 0; j < 8; j++) orders.Add(new object());
                    for (int n = 0; n < 25000; n++)
                    {
                        long now = sw.ElapsedMilliseconds; object o = orders[rnd.Next(8)];
                        switch (rnd.Next(6))
                        {
                            case 0: g.NoteOrder(key, o, null, rnd.Next(2) == 0, true, STOP, SettlePhase.Working, rnd.Next(5), now); break;
                            case 1: g.NotePosition("S", key, rnd.Next(-2, 3), o, o, now); break;
                            case 2: g.Judge(key, true, rnd.Next(-2, 3), null, now); break;
                            case 3: if (g.PeekHeld(key) == null) { try { g.Hold(new HeldClose { Token = g.NewToken(), Key = key, Account = "S", ArrivedMs = now, DeadlineMs = now + 1 }, now); Interlocked.Increment(ref held); } catch (InvalidOperationException) { } } break;
                            case 4: foreach (var h in g.HeldSnapshot()) if (g.CheckHeld(h.Token, new SettleVerdict(), now) != null && g.Take(h.Token) != null) { Interlocked.Increment(ref ran); g.ReleaseDone(); } break;
                            default: g.RegisterOwnExit(key, o, true, true, now); g.LiveRefs(key, now); g.Prune(now); int w; g.Upkeep(key, g, true, true, 0, null, now, out w); foreach (var m in g.MarksSnapshot()) g.DropMark(m); break;
                        }
                        if (n % 500 == 0) g.DropOwnExit(o);
                    }
                }
                catch (Exception) { Interlocked.Increment(ref errs); }
            }) { IsBackground = true }; ths.Add(t); t.Start();
        }
        bool done = true; foreach (var t in ths) done &= t.Join(60000);
        foreach (var h in g.HeldSnapshot()) if (g.Take(h.Token) != null) { ran++; g.ReleaseDone(); }
        Ok("U18 4 threads, 100k events: no deadlock, every hold ran exactly once (" + held + ")", done && errs == 0 && held == ran && held > 100 && g.HeldSnapshot().Count == 0, "done " + done + " errs " + errs + " " + held + "/" + ran);
    }
    static void X6()
    {
        var k = new K(); k.Pos = -1; int was; k.G.NoteConnection(k, true, false, false, false); // Connected
        k.G.Upkeep(k.Key, k, true, true, 0, null, k.Now, out was); k.Now += 185; // not restored yet, then silently Short 1
        Ok("X6 10-22 restore: close at once (UNKNOWN)", k.J().Kind == VerdictKind.Unknown);
        k.Now += 5000; k.G.Upkeep(k.Key, k, true, true, -1, null, k.Now, out was); Ok("X6 a changed read restarts the anchor", !k.G.IsTrusted(k.Key));
        k.Now += 5000; k.G.Upkeep(k.Key, k, true, true, -1, null, k.Now, out was); Ok("X6 then settled held, 0 ms", k.J().Kind == VerdictKind.SettledHeld, k.J().ToString());
    }
    static void X7()
    {
        var g = new VLSettleGate(); object c1 = new object(), c2 = new object(); var k = new K(g); int was;
        k.G.NotePosition(k.Acct, k.Key, 0, k, k, k.Now); g.Upkeep(k.Key, c1, true, true, 0, null, k.Now, out was); k.Now += 5000; g.Upkeep(k.Key, c1, true, true, 0, null, k.Now, out was);
        Action<object> blip = c => { g.NoteConnection(c, false, false, true, false); g.NoteConnection(c, false, false, false, false); g.NoteConnection(c, true, false, false, false); };
        g.NoteConnection(c1, true, false, false, true); Ok("X7 a repeat of Connected clears nothing", g.IsTrusted(k.Key));
        blip(c1); Ok("X7 ConnectionLost and back clears nothing", g.IsTrusted(k.Key) && k.J().ESeen); blip(c2);
        g.NoteConnection(c2, false, true, false, true); g.NoteConnection(c2, true, false, false, false); Ok("X7 another connection clears nothing", g.IsTrusted(k.Key));
        g.NoteConnection(c1, false, true, false, true); Ok("X7 Disconnected alone clears nothing yet", g.IsTrusted(k.Key));
        g.NoteConnection(c1, false, false, false, false); g.NoteConnection(c1, true, false, false, false); Ok("X7 Disconnected to Connected drops the anchor and the event", !g.IsTrusted(k.Key) && !k.J().ESeen && k.J().Kind == VerdictKind.Unknown);
    }
    static void Conn()
    {
        var g = new VLSettleGate(); var k = new K(g); int was; g.NoteConnection(k, false, false, false, false);
        g.Upkeep(k.Key, k, true, true, 0, null, k.Now, out was); k.Now += 5000; g.Upkeep(k.Key, k, true, true, 0, null, k.Now, out was);
        Ok("Conn first Connected ever seen (from Connecting) is a start", g.NoteConnection(k, true, false, false, false) && !g.IsTrusted(k.Key));
        g.Upkeep(k.Key, k, true, true, 0, null, k.Now, out was); k.Now += 5000; g.Upkeep(k.Key, k, true, true, 0, null, k.Now, out was); bool tr = g.IsTrusted(k.Key);
        g.Upkeep(k.Key, null, false, true, 0, null, k.Now, out was); bool still = g.IsTrusted(k.Key); g.Upkeep(k.Key, new object(), true, true, 0, null, k.Now, out was);
        Ok("Conn an account that moves to another connection is anchored again (none for a moment is not a move)", tr && still && !g.IsTrusted(k.Key));
        Ok("Conn the second one is not", !g.NoteConnection(k, true, false, false, true));
    }
    static void X8()
    {
        var k = Short3(); object s = new object(); k.Ord(s, true, true, STOP, SettlePhase.Working, 0); k.Now += 999; var a = k.J(); k.Now += 1; var b = k.J();
        Ok("X8 a stop that has been Working for 1.0 s is not imminent any more", a.Why == UnsettledWhy.TriggeredStop && b.Kind == VerdictKind.SettledHeld);
        k.Ord(s, true, true, STOP, SettlePhase.PartFilled, 0); Ok("X8 Working to PartFilled is not a new trigger", k.J().Kind == VerdictKind.SettledHeld);
        k.Ord(s, true, true, STOP, SettlePhase.Accepted, 0); k.Ord(s, true, true, STOP, SettlePhase.Working, 0); Ok("X8 triggered again is", k.J().Why == UnsettledWhy.TriggeredStop);
        var m = Short3(); m.Ord(new object(), true, true, MKT, SettlePhase.Accepted, 0); m.Now += 1000; Ok("X8 a market exit stuck for 1 s does not hold for ever", m.J().Kind == VerdictKind.SettledHeld);
        var l = Short3(); object o = new object(); l.Ord(o, true, true, STOP, SettlePhase.Accepted, 0); l.LiveMap[o] = new LiveOrder { Ref = o, Filled = 1, Phase = SettlePhase.Filled };
        var v = l.J(); Ok("X12 the live Filled is folded before its event", v.L == -2 && v.Kind == VerdictKind.Unsettled, v.ToString());
        l.Ord(o, true, true, STOP, SettlePhase.Filled, 1); Ok("X12 and its event is not counted again", l.J().L == -2);
    }
    static void X13()
    {
        var k = new K().Anchor(); object o = new object(); k.Ord(o, true, false, MKT, SettlePhase.Filled, 2); k.Ev(2);
        k.Now += 590000; k.G.Prune(k.Now); k.Ord(o, true, false, MKT, SettlePhase.Filled, 2); Ok("X13 a Filled repeated 9 min 50 s later counts once", k.J().L == 2);
    }
    static void Prune()
    {
        var k = new K().Anchor(); for (int i = 0; i < 2100; i++) { k.Ord(new object(), true, true, OTH, SettlePhase.Cancelled, 0, "c" + i); } object live = new object(); k.Ord(live, true, true, OTH, SettlePhase.Working, 0);
        k.G.Prune(k.Now); Ok("Prune at most 2000 ended orders are kept, live ones always", k.G.TrackedCount == 2001, k.G.TrackedCount.ToString());
        k.Now += 600001; k.G.Prune(k.Now); Ok("Prune ended orders go after 10 min", k.G.TrackedCount == 1, k.G.TrackedCount.ToString());
    }
    static void Stale()
    {
        object p; IDisposable t; var k = Short3(); k.G.Tick(k.Now);
        k.Now += 6000; Ok("Stale idle thread 6.0 s: still gated", k.G.OnCloseFrame(k.Key, false, "x", true, true, k.Now, out p, out t) == CloseRoute.Judge);
        k.Now += 1; Ok("Stale idle thread 6.001 s: runs now", k.G.OnCloseFrame(k.Key, false, "x", true, true, k.Now, out p, out t) == CloseRoute.RunNow);
        k.G.Tick(k.Now); k.Now += 4000; long tok = k.Hold(); k.Now += 250; Ok("Stale a hold after 4 s of idle counts from the hold", k.G.OnCloseFrame(k.Key, false, "x", true, true, k.Now, out p, out t) == CloseRoute.Joined);
        k.Now += 1; Ok("Stale busy thread 251 ms without a pulse: runs now, the held one goes with it", k.G.OnCloseFrame(k.Key, false, "x", true, true, k.Now, out p, out t) == CloseRoute.RunNow && k.G.PeekHeld(k.Key) == null && k.G.Take(tok) == null);
        k.G.Tick(k.Now); Ok("Stale link down: runs now", k.G.OnCloseFrame(k.Key, false, "x", false, true, k.Now, out p, out t) == CloseRoute.RunNow);
        Ok("Stale thread ended: runs now", k.G.OnCloseFrame(k.Key, false, "x", true, false, k.Now, out p, out t) == CloseRoute.RunNow);
        var n = new VLSettleGate(); Ok("Stale never ticked: runs now", n.OnCloseFrame("k", false, "x", true, true, 5, out p, out t) == CloseRoute.RunNow);
    }
}
