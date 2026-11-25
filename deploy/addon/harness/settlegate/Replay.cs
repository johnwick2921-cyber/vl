// Replays an event file (extract_nt8.py) through the production VLSettleGate: every
// close is judged at its arrival and, when unsettled, followed until it is released.
// The file holds account names and stays outside the repo.
//   O t acct inst ref buy exit kind phase filled | P t acct inst signed | C t acct inst held what | F t acct inst
// F = our Flatten was called. A held close is the counterfactual: the market exit NT8 made
// for it at once is cut out of the story (its fills too), and the gate's own release is judged.
// The 5 s upkeep passes run in every gap, as the settle thread would.
using System;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using System.Linq;
using NinjaTrader.NinjaScript.AddOns;

static class Replay
{
    sealed class S { public K K; public bool Bad; public long BadAt, LastT; public HashSet<object> Cut = new HashSet<object>(); public Dictionary<object, int> CutCum = new Dictionary<object, int>(); public int CutSum; public HashSet<object> Seen = new HashSet<object>(); public long HeldAt = long.MinValue / 2; }
    public static int Run(string file)
    {
        var g = new VLSettleGate(); var st = new Dictionary<string, S>(); var refs = new Dictionary<string, object>();
        var lines = File.ReadAllLines(file); int closes = 0, held = 0, flats = 0, unknown = 0, resyncs = 0, over = 0; var spells = new List<long>();
        var waiting = new List<Tuple<S, long, long, string>>();
        Func<string, string, long, S> get = (a, i, t) =>
        {
            S s; string key = a + "|" + i;
            if (!st.TryGetValue(key, out s)) { s = new S { K = new K(g, a, t - 5000) }; FixKey(s.K, i); int w; g.NotePosition(a, s.K.Key, 0, s, s, t - 5000); g.Upkeep(s.K.Key, s, true, true, 0, null, t - 5000, out w); g.Upkeep(s.K.Key, s, true, true, 0, null, t, out w); st[key] = s; }
            return s;
        };
        foreach (string line in lines)
        {
            string[] f = line.Split('\t'); long t = long.Parse(f[1], CultureInfo.InvariantCulture);
            // held closes look every 10 ms up to this event
            foreach (var w in waiting.ToList())
            {
                string why = null; long n = w.Item3;
                while (n <= t && why == null) { w.Item1.K.Now = n; why = g.CheckHeld(w.Item2, g.Judge(w.Item1.K.Key, true, w.Item1.K.Pos, null, n), n); if (why == null) n += 10; }
                waiting.Remove(w);
                if (why == null) { waiting.Add(Tuple.Create(w.Item1, w.Item2, n, w.Item4)); continue; }
                var h = g.Take(w.Item2); g.ReleaseDone(); bool flat = w.Item1.K.Pos == 0;
                if (flat) flats++;
                Console.WriteLine("HELD " + w.Item4 + " delay=" + (n - h.ArrivedMs) + " ms (" + why + ") position at release=" + w.Item1.K.Pos + " at arrival " + h.AtHold);
                g.NoteFlatten(w.Item1.K.Key, !flat, n);
            }
            S s = get(f[2], f[3], t); K k = s.K; int wl;
            for (int p = 1; p <= 4 && s.LastT + 5000L * p < t; p++)
                if (g.Upkeep(k.Key, s, true, true, k.Pos, null, s.LastT + 5000L * p, out wl) == UpkeepResult.Resynced)
                { resyncs++; if (s.Bad) { s.Bad = false; over++; } }
            s.LastT = t; k.Now = t; g.Tick(t);
            if (f[0] == "O")
            {
                object o; if (!refs.TryGetValue(f[4], out o)) { o = new object(); refs[f[4]] = o; }
                if (f[6] == "1" && f[7] == "2" && t - s.HeldAt < 1000 && !s.Seen.Contains(o)) s.Cut.Add(o);
                s.Seen.Add(o);
                if (s.Cut.Contains(o))
                {
                    int had; s.CutCum.TryGetValue(o, out had); int cum = int.Parse(f[9]);
                    if (cum > had) { s.CutCum[o] = cum; s.CutSum += (f[5] == "1" ? 1 : -1) * (cum - had); }
                    continue;
                }
                g.NoteOrder(k.Key, o, f[4], f[5] == "1", f[6] == "1", (SettleOrderKind)int.Parse(f[7]), (SettlePhase)int.Parse(f[8]), int.Parse(f[9]), t);
            }
            else if (f[0] == "P") { k.Pos = int.Parse(f[4]) - s.CutSum; if (int.Parse(f[4]) == 0 && s.CutSum != 0) { s.CutSum = 0; k.Pos = 0; } // the story ends at its next flat
                g.NotePosition(f[2], k.Key, k.Pos, s, s, t); }
            else if (f[0] == "F") { if (t - s.HeldAt > 1000) g.NoteFlatten(k.Key, k.Pos != 0, t); continue; }
            else
            {
                closes++; int c = int.Parse(f[4]);
                var v = g.Judge(k.Key, true, c, null, t);
                if (v.Kind == VerdictKind.Unknown) unknown++;
                if (v.Kind == VerdictKind.Unsettled)
                {
                    held++; long tok = g.NewToken();
                    if (g.PeekHeld(k.Key) != null) Console.WriteLine("JOINED " + f[2] + " " + f[1]);
                    else { g.Hold(new HeldClose { Token = tok, Account = f[2], Key = k.Key, ArrivedMs = t, DeadlineMs = t + 1000, AtHold = v }, t); waiting.Add(Tuple.Create(s, tok, t + 10, f[5])); s.HeldAt = t; }
                }
            }
            var j = g.Judge(k.Key, true, k.Pos, null, t); bool bad = (j.Why & (UnsettledWhy.LedgerVsPosition | UnsettledWhy.EventVsPosition | UnsettledWhy.TriggeredStop | UnsettledWhy.LiveMarketExit)) != 0;
            if (bad && !s.Bad) { s.Bad = true; s.BadAt = t; } else if (!bad && s.Bad) { s.Bad = false; spells.Add(t - s.BadAt); }
            g.Prune(t);
        }
        spells.Sort();
        Console.WriteLine("events " + lines.Length + " closes " + closes + " held " + held + " unknown " + unknown + " released-on-flat " + flats + " 0ms " + (closes - held) + " ledger resyncs " + resyncs + " (spells ended by one: " + over + ")");
        if (spells.Count > 0) Console.WriteLine("unsettled spells " + spells.Count + " median " + spells[spells.Count / 2] + " p99 " + spells[(int)(spells.Count * 0.99)] + " max " + spells[spells.Count - 1] + " over 1000: " + spells.Count(x => x > 1000));
        return 0;
    }
    static void FixKey(K k, string inst) { if (VLSettleGate.Key(k.Acct, inst) != k.Key) k.Key0(inst); }
}
