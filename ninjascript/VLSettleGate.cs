// VLSettleGate.cs - Release 2 (2026-11-24): the settle gate, pure logic.
//
// WHY. 2026-11-19 06:47:30.252 a close frame arrived while a stop had already
// filled but NT8 still showed Short 1. Flatten sized itself from Short 1 and opened
// a new Long 1 that nothing protected (-1207.00).
//
// WHAT. The gate only DELAYS a close. It never answers "already flat" and never
// refuses one: every close still ends in HandleClosePosition, at once when NT8 is
// settled, else as soon as it settles, at the latest settle_ms after it arrived.
//
// RULES OF THIS FILE
//   - no NinjaTrader types (compiled and tested outside NT8: deploy/addon/harness/settlegate);
//   - time is a long in milliseconds from a monotonic clock, always passed in;
//   - one private lock; nothing is called out of this class while it is held, so
//     "NT8 lock then this lock" can happen and "this lock then anything" cannot;
//   - keys are (account name, instrument full name).
//
// Design: addon-r2 DESIGN.md + the FINAL RULES of ATTACK.md (which win).

#region Using declarations
using System;
using System.Collections.Generic;
using System.Globalization;
using System.Runtime.CompilerServices;
#endregion

namespace NinjaTrader.NinjaScript.AddOns
{
    internal enum SettleOrderKind { Other = 0, Stop = 1, Market = 2 }

    internal enum SettlePhase
    {
        Unknown = 0, Initialized = 1, Submitted = 2, Accepted = 3, TriggerPending = 4, Changing = 5,
        Working = 6, PartFilled = 7, CancelPending = 8, Filled = 9, Cancelled = 10, Rejected = 11
    }

    internal enum VerdictKind { Unknown = 0, Unsettled = 1, SettledFlat = 2, SettledHeld = 3 }

    // Why a key is unsettled. B3 is split so an entry frame can tell "our own running
    // Flatten" (OwnFlatten, with its own market Close) from a triggered stop.
    [Flags]
    internal enum UnsettledWhy { None = 0, LedgerVsPosition = 1, EventVsPosition = 2, TriggeredStop = 4, LiveMarketExit = 8, OwnFlatten = 16 }

    internal struct SettleVerdict
    {
        public VerdictKind Kind;
        public UnsettledWhy Why;
        public bool Trusted;   // the key has an anchor, so L means something
        public int L;          // ledger: signed sum of fills
        public int E;          // last position event
        public bool ESeen;     // a real position event arrived since the anchor was dropped
        public int C;          // fresh position read
        public bool CKnown;

        public override string ToString()
        {
            return "L=" + (Trusted ? L.ToString(CultureInfo.InvariantCulture) : "n/a")
                + " E=" + (ESeen ? E.ToString(CultureInfo.InvariantCulture) : "n/a")
                + " C=" + (CKnown ? C.ToString(CultureInfo.InvariantCulture) : "n/a")
                + " " + Kind + (Why == UnsettledWhy.None ? "" : " (" + Why + ")");
        }
    }

    // What an order looks like RIGHT NOW (its live properties, not its last event).
    internal struct LiveOrder
    {
        public object Ref;
        public int Filled;
        public SettlePhase Phase;
    }

    // An order as it stood in the account's order list, for anchoring.
    internal struct OrderSeed
    {
        public object Ref;
        public string OrderId;
        public bool Buy;
        public bool ExitSide;
        public SettleOrderKind Kind;
        public SettlePhase Phase;
        public int Filled;
    }

    internal sealed class HeldClose
    {
        public long Token;
        public string Account;
        public string Key;
        public bool IsLimit;
        public object Payload;
        public long ArrivedMs;
        public long DeadlineMs;
        public SettleVerdict AtHold;
        public object AccountRef;
        public object InstrumentRef;
        public IDisposable Timer;

        internal bool SettledSeen;
        internal long SettledMs;
        internal int SettledC;
    }

    internal enum CloseRoute { Judge = 0, RunNow = 1, Joined = 2 }

    internal sealed class OwnExit
    {
        public long Seq;
        public string Key;
        public object Ref;
        public bool IsLx;       // "-lx" limit exit; false = a standalone protective "-sl"
        public bool Buy;        // BuyToCover
        public long RegisteredMs;
        public bool Submitted;
    }

    internal sealed class SweepMark
    {
        public string Account;
        public string Key;
        public object AccountRef;
        public object InstrumentRef;
        public List<OwnExit> Items;   // FIXED at the Flat event
        public long CreatedMs;
        internal bool FlatSeen;
        internal long FlatMs;
        internal bool Confirmed;
    }

    internal enum SweepStep { Wait = 0, Cancel = 1, Done = 2, Drop = 3 }

    internal enum UpkeepResult { None = 0, Anchored = 1, Resynced = 2 }

    // === the switch file ======================================================
    internal sealed class AddonSwitches
    {
        public const int DefaultSettleMs = 1000;
        public const int MaxSettleMs = 2000;

        public readonly int SettleMs;
        public readonly bool KeepNote;
        public readonly bool StopCheck;
        public readonly bool OwnExitSweep;
        public readonly bool OneLx;

        public AddonSwitches() : this(DefaultSettleMs, true, true, true, true) { }

        public AddonSwitches(int settleMs, bool keepNote, bool stopCheck, bool ownExitSweep, bool oneLx)
        {
            SettleMs = settleMs; KeepNote = keepNote; StopCheck = stopCheck; OwnExitSweep = ownExitSweep; OneLx = oneLx;
        }

        public override string ToString()
        {
            return "settle_ms=" + SettleMs.ToString(CultureInfo.InvariantCulture) + " keep_note=" + (KeepNote ? 1 : 0)
                + " stop_check=" + (StopCheck ? 1 : 0) + " own_exit_sweep=" + (OwnExitSweep ? 1 : 0) + " one_lx=" + (OneLx ? 1 : 0);
        }

        // text == null: the file is absent, all defaults. A key that is not in the file is
        // its default. A key that IS there with a bad value keeps the LAST GOOD value (a
        // half-saved file must not turn a switched-off part back on).
        public static AddonSwitches Parse(string text, AddonSwitches lastGood, out List<string> warnings)
        {
            warnings = new List<string>();
            AddonSwitches last = lastGood ?? new AddonSwitches();
            int settle = DefaultSettleMs;
            bool note = true, stop = true, sweep = true, lx = true;
            if (text == null) return new AddonSwitches();
            foreach (string rawLine in text.Replace("﻿", "").Replace("\r", "\n").Split('\n'))
            {
                string line = rawLine;
                int hash = line.IndexOf('#');
                if (hash >= 0) line = line.Substring(0, hash);
                line = line.Trim();
                if (line.Length == 0) continue;
                int eq = line.IndexOf('=');
                if (eq <= 0) { warnings.Add("line without key=value ignored"); continue; }
                string key = line.Substring(0, eq).Trim().ToLowerInvariant();
                string val = line.Substring(eq + 1).Trim();
                switch (key)
                {
                    case "settle_ms":
                        int n;
                        if (int.TryParse(val, NumberStyles.None, CultureInfo.InvariantCulture, out n) && n >= 0 && n <= MaxSettleMs) settle = n;
                        else { settle = last.SettleMs; warnings.Add("settle_ms bad value, kept " + last.SettleMs.ToString(CultureInfo.InvariantCulture)); }
                        break;
                    case "keep_note": note = Flag(key, val, last.KeepNote, warnings); break;
                    case "stop_check": stop = Flag(key, val, last.StopCheck, warnings); break;
                    case "own_exit_sweep": sweep = Flag(key, val, last.OwnExitSweep, warnings); break;
                    case "one_lx": lx = Flag(key, val, last.OneLx, warnings); break;
                    default: warnings.Add("unknown key ignored"); break;
                }
            }
            return new AddonSwitches(settle, note, stop, sweep, lx);
        }

        private static bool Flag(string key, string val, bool last, List<string> warnings)
        {
            if (val == "0") return false;
            if (val == "1") return true;
            warnings.Add(key + " bad value, kept " + (last ? "1" : "0"));
            return last;
        }
    }

    // === small pure decisions ================================================
    internal enum NoteAction { Leave = 0, Remove = 1, PlaceBracket = 2, KeepAndWarn = 3 }

    internal static class NoteDecision
    {
        // An entry order is Cancelled. `sameOrder` = the note was written for exactly this
        // Order object (a signal id can be used again).
        public static NoteAction OnCancelled(bool notePresent, bool sameOrder, int filled, bool eSeen, int e, bool entryIsBuy)
        {
            if (!notePresent || !sameOrder) return NoteAction.Leave;
            if (filled <= 0) return NoteAction.Remove;
            bool held = eSeen && (entryIsBuy ? e > 0 : e < 0);
            return held ? NoteAction.PlaceBracket : NoteAction.KeepAndWarn;
        }
    }

    internal enum StopAction { Place = 0, RefuseFlat = 1, RefuseOpposite = 2 }

    internal static class StopDecision
    {
        // Never waits. The only refusals are three KNOWN sources that all say the same:
        // flat, or held on the other side. Everything else is placed.
        public static StopAction Decide(bool askLong, int askQty, SettleVerdict v, bool closeInFlight, out int qty)
        {
            qty = askQty;
            if (!closeInFlight && v.Trusted && v.ESeen && v.CKnown && v.L == v.E && v.E == v.C)
            {
                if (v.C == 0) return StopAction.RefuseFlat;
                if ((v.C > 0) != askLong) return StopAction.RefuseOpposite;
            }
            if (v.Kind == VerdictKind.SettledHeld && (v.C > 0) == askLong)
                qty = Math.Min(askQty, Math.Abs(v.C));
            return StopAction.Place;
        }
    }

    internal static class LxDecision
    {
        public const long SubmittedGraceMs = 2000;

        // Does this live "-lx" already exit the whole held position?
        public static bool Covers(bool lxBuy, SettlePhase phase, int remaining, long ageMs, bool needBuy, int heldQty)
        {
            if (lxBuy != needBuy || heldQty <= 0 || remaining < heldQty) return false;
            switch (phase)
            {
                case SettlePhase.Submitted: return ageMs < SubmittedGraceMs;
                case SettlePhase.Accepted:
                case SettlePhase.Working:
                case SettlePhase.PartFilled: return true;
                default: return false;
            }
        }
    }

    // === the gate =============================================================
    internal sealed class VLSettleGate
    {
        public const long TwoCheckGapMs = 20;
        public const long OwnFlattenMinMs = 300;
        public const long TickStaleBusyMs = 250;
        public const long TickStaleIdleMs = 6000;
        public const long TerminalFoldMs = 5000;
        public const long TerminalKeepMs = 600000;
        public const int TerminalCap = 2000;
        public const int MarkCap = 64;

        // Not const: the harness shortens them.
        internal long UpkeepMs = 5000;
        internal long AnchorGapMs = 5000;

        private sealed class RefEq : IEqualityComparer<object>
        {
            bool IEqualityComparer<object>.Equals(object a, object b) { return ReferenceEquals(a, b); }
            int IEqualityComparer<object>.GetHashCode(object o) { return RuntimeHelpers.GetHashCode(o); }
        }

        private sealed class Tracked
        {
            public object Ref;
            public string Key;
            public string Id = "";
            public bool Buy, ExitSide;
            public SettleOrderKind Kind;
            public SettlePhase Phase;
            public int Counted;
            public long FirstSeenMs;
            public long WorkingMs = -1;   // when it last ENTERED Working/PartFilled
            public long TerminalMs = -1;
        }

        private sealed class KeyState
        {
            public bool Trusted;
            public object Conn;
            public int L;
            public int E;
            public bool ESeen;
            public long FillSeq;
            public long LastFillMs = -1;
            // anchor candidate
            public bool HasCand; public int CandC; public long CandMs, CandSeq;
            // resync candidate
            public bool HasRes; public int ResC; public long ResMs, ResSeq;
            // our own last Flatten
            public bool HasFlatten, FlattenHeld, FlattenSawFlat, FlattenDead;
            public long FlattenMs;
        }

        private readonly object gate = new object();
        private readonly Dictionary<string, KeyState> keys = new Dictionary<string, KeyState>(StringComparer.Ordinal);
        private readonly Dictionary<object, Tracked> byRef = new Dictionary<object, Tracked>(new RefEq());
        private readonly Dictionary<string, Tracked> byId = new Dictionary<string, Tracked>(StringComparer.Ordinal);
        private readonly Dictionary<string, HeldClose> held = new Dictionary<string, HeldClose>(StringComparer.Ordinal);
        private readonly List<OwnExit> ownExits = new List<OwnExit>();
        private readonly List<SweepMark> marks = new List<SweepMark>();
        private readonly Dictionary<object, bool> connWasDown = new Dictionary<object, bool>(new RefEq());
        private AddonSwitches switches = new AddonSwitches();
        private long nextToken = 1, nextSeq = 1;
        private int releasing;
        private bool sweepsStopped;
        private long lastTickMs = -1, busySinceMs = -1;

        public static string Key(string account, string instrumentFullName)
        {
            return (account ?? "") + "\u001f" + (instrumentFullName ?? "").ToUpperInvariant();
        }

        public AddonSwitches Switches
        {
            get { lock (gate) { return switches; } }
            set { lock (gate) { switches = value ?? new AddonSwitches(); } }
        }

        // The window every "recent" rule uses. settle_ms=0 turns the hold off but the sweep
        // and the stop check still need a length for "recent".
        private long WindowLocked() { return switches.SettleMs > 0 ? switches.SettleMs : AddonSwitches.DefaultSettleMs; }

        private KeyState GetLocked(string key)
        {
            KeyState k;
            if (!keys.TryGetValue(key, out k)) { k = new KeyState(); keys[key] = k; }
            return k;
        }

        public bool IsTrusted(string key)
        {
            lock (gate) { KeyState k; return keys.TryGetValue(key, out k) && k.Trusted; }
        }

        public List<string> KnownKeys(string account)
        {
            var res = new List<string>();
            string pre = (account ?? "") + "\u001f";
            lock (gate) { foreach (string k in keys.Keys) if (k.StartsWith(pre, StringComparison.Ordinal)) res.Add(k); }
            return res;
        }

        // === facts, from events ================================================
        private Tracked FindLocked(string key, object orderRef, string orderId)
        {
            Tracked t;
            if (byRef.TryGetValue(orderRef, out t)) return t;
            if (!string.IsNullOrEmpty(orderId) && byId.TryGetValue(key + "\u001f" + orderId, out t))
            {
                // Same order, new object (NT8 does not promise identity across a disconnect).
                byRef.Remove(t.Ref);
                t.Ref = orderRef;
                byRef[orderRef] = t;
                return t;
            }
            return null;
        }

        private static bool IsTerminal(SettlePhase p)
        {
            return p == SettlePhase.Filled || p == SettlePhase.Cancelled || p == SettlePhase.Rejected;
        }

        private static bool IsTriggered(SettlePhase p) { return p == SettlePhase.Working || p == SettlePhase.PartFilled; }

        private Tracked TrackLocked(string key, object orderRef, string orderId, bool buy, bool exitSide, SettleOrderKind kind, long now)
        {
            Tracked t = FindLocked(key, orderRef, orderId);
            if (t == null)
            {
                t = new Tracked { Key = key, Buy = buy, ExitSide = exitSide, Kind = kind, FirstSeenMs = now };
                byRef[orderRef] = t;
                t.Ref = orderRef;
            }
            if (!string.IsNullOrEmpty(orderId) && t.Id != orderId)
            {
                t.Id = orderId;
                byId[key + "\u001f" + orderId] = t;
            }
            return t;
        }

        // Cumulative: a repeat or a smaller value adds nothing.
        private void CountFillLocked(Tracked t, int cumFilled, long now)
        {
            int d = cumFilled - t.Counted;
            if (d <= 0) return;
            t.Counted = cumFilled;
            KeyState k = GetLocked(t.Key);
            k.L += t.Buy ? d : -d;
            k.FillSeq++;
            k.LastFillMs = now;
        }

        private void PhaseLocked(Tracked t, SettlePhase phase, long now)
        {
            if (t.TerminalMs >= 0) return; // terminal is terminal
            if (IsTriggered(phase)) { if (!IsTriggered(t.Phase)) t.WorkingMs = now; }
            else t.WorkingMs = -1;
            t.Phase = phase;
            if (IsTerminal(phase)) t.TerminalMs = now;
        }

        // An order update. Returns true when the settle thread has something to do.
        public bool NoteOrder(string key, object orderRef, string orderId, bool buy, bool exitSide,
                              SettleOrderKind kind, SettlePhase phase, int cumFilled, long now)
        {
            if (orderRef == null) return false;
            lock (gate)
            {
                Tracked t = TrackLocked(key, orderRef, orderId, buy, exitSide, kind, now);
                CountFillLocked(t, cumFilled, now);
                PhaseLocked(t, phase, now);
                if (IsTerminal(phase))
                {
                    if (t.Kind == SettleOrderKind.Market && t.ExitSide && phase != SettlePhase.Filled)
                    {
                        KeyState k = GetLocked(key);
                        // only an order made AFTER our Flatten can be its Close
                        if (k.HasFlatten && t.FirstSeenMs >= k.FlattenMs) k.FlattenDead = true;
                    }
                    for (int i = ownExits.Count - 1; i >= 0; i--)
                        if (ReferenceEquals(ownExits[i].Ref, orderRef)) ownExits.RemoveAt(i);
                }
                else GetLocked(key);
                return BusyLocked();
            }
        }

        // A position event. A Flat one fixes the sweep list. Returns true when the settle
        // thread has something to do.
        public bool NotePosition(string account, string key, int signed, object accountRef, object instrumentRef, long now)
        {
            lock (gate)
            {
                KeyState k = GetLocked(key);
                k.E = signed;
                k.ESeen = true;
                if (signed == 0)
                {
                    if (k.HasFlatten) k.FlattenSawFlat = true;
                    if (switches.OwnExitSweep && !sweepsStopped)
                    {
                        List<OwnExit> items = null;
                        foreach (OwnExit o in ownExits)
                            if (o.Key == key) { if (items == null) items = new List<OwnExit>(); items.Add(o); }
                        if (items != null && marks.Count < MarkCap)
                        {
                            MarkBusyLocked(now);
                            marks.Add(new SweepMark { Account = account, Key = key, AccountRef = accountRef, InstrumentRef = instrumentRef, Items = items, CreatedMs = now });
                        }
                    }
                }
                return BusyLocked();
            }
        }

        public void ReadEvent(string key, out bool eSeen, out int e)
        {
            lock (gate)
            {
                KeyState k;
                eSeen = false; e = 0;
                if (keys.TryGetValue(key, out k)) { eSeen = k.ESeen; e = k.E; }
            }
        }

        // Stamped IMMEDIATELY before Account.Flatten, on every path.
        public void NoteFlatten(string key, bool held, long now)
        {
            lock (gate)
            {
                KeyState k = GetLocked(key);
                k.HasFlatten = true; k.FlattenMs = now; k.FlattenHeld = held;
                k.FlattenSawFlat = false; k.FlattenDead = false;
            }
        }

        // The Flatten call itself threw: nothing of ours is running.
        public void NoteFlattenFailed(string key)
        {
            lock (gate) { KeyState k; if (keys.TryGetValue(key, out k)) k.HasFlatten = false; }
        }

        // === connections ========================================================
        // Only Disconnected -> ... -> Connected drops the anchors of the keys on THAT
        // connection. ConnectionLost and back does not (nothing is restored then).
        public bool NoteConnection(object conn, bool isConnected, bool isDisconnected, bool isLost, bool previousWasConnected)
        {
            if (conn == null) return false;
            lock (gate)
            {
                bool down;
                bool known = connWasDown.TryGetValue(conn, out down);
                if (isDisconnected) { connWasDown[conn] = true; return false; }
                if (isLost) { if (!known) connWasDown[conn] = false; return false; }
                if (!isConnected) return false;
                // First sight: a repeat of Connected is not a transition; anything else is a start.
                if (!known) down = !previousWasConnected;
                connWasDown[conn] = false;
                if (!down) return false;
                foreach (KeyState k in keys.Values)
                    if (ReferenceEquals(k.Conn, conn)) DropAnchorLocked(k);
                return true;
            }
        }

        private static void DropAnchorLocked(KeyState k)
        {
            k.Trusted = false; k.ESeen = false; k.E = 0; k.HasCand = false; k.HasRes = false;
        }

        // === upkeep (settle thread, about every 5 s) ============================
        // Untrusted key: anchored only when the position read is the same twice, at least
        // AnchorGapMs apart, with no fill between. Trusted key: a ledger that is wrong for
        // that long with the event and the read agreeing is set right (a missed event).
        public UpkeepResult Upkeep(string key, object conn, bool connected, bool cKnown, int c, List<OrderSeed> seeds, long now, out int wasL)
        {
            wasL = 0;
            lock (gate)
            {
                KeyState k = GetLocked(key);
                if (conn != null)
                {
                    // Moved to another connection: its state may have been restored there without
                    // an event (10-22), so it is anchored again.
                    if (k.Conn != null && !ReferenceEquals(k.Conn, conn)) DropAnchorLocked(k);
                    k.Conn = conn;
                }
                wasL = k.L;
                if (!cKnown || !connected) { k.HasCand = false; k.HasRes = false; return UpkeepResult.None; }
                if (!k.Trusted)
                {
                    bool moved = false;
                    if (seeds != null)
                        foreach (OrderSeed s in seeds)
                        {
                            if (s.Ref == null) continue;
                            Tracked t = FindLocked(key, s.Ref, s.OrderId);
                            if (t == null)
                            {
                                t = TrackLocked(key, s.Ref, s.OrderId, s.Buy, s.ExitSide, s.Kind, now);
                                t.Counted = s.Filled;
                                PhaseLocked(t, s.Phase, now);
                                if (s.Filled > 0) moved = true;
                            }
                            else if (s.Filled > t.Counted) moved = true; // live is ahead of its event
                        }
                    if (moved || !k.HasCand || k.CandC != c || k.CandSeq != k.FillSeq)
                    {
                        k.HasCand = true; k.CandC = c; k.CandMs = now; k.CandSeq = k.FillSeq;
                        return UpkeepResult.None;
                    }
                    if (now - k.CandMs < AnchorGapMs) return UpkeepResult.None;
                    k.L = c; k.Trusted = true; k.HasCand = false; k.HasRes = false;
                    return UpkeepResult.Anchored;
                }
                bool quiet = k.LastFillMs < 0 || now - k.LastFillMs > WindowLocked();
                if (k.L == c || !quiet || (k.ESeen && k.E != c)) { k.HasRes = false; return UpkeepResult.None; }
                if (!k.HasRes || k.ResC != c || k.ResSeq != k.FillSeq)
                {
                    k.HasRes = true; k.ResC = c; k.ResMs = now; k.ResSeq = k.FillSeq;
                    return UpkeepResult.None;
                }
                if (now - k.ResMs < AnchorGapMs) return UpkeepResult.None;
                k.L = c; k.HasRes = false;
                return UpkeepResult.Resynced;
            }
        }

        // Forget terminal orders after TerminalKeepMs, and beyond TerminalCap oldest first.
        // (Forgotten at once + a repeated Filled = counted twice.)
        public void Prune(long now)
        {
            lock (gate)
            {
                var term = new List<Tracked>();
                foreach (Tracked t in byRef.Values) if (t.TerminalMs >= 0) term.Add(t);
                term.Sort((a, b) => a.TerminalMs.CompareTo(b.TerminalMs));
                int over = term.Count - TerminalCap;
                for (int i = 0; i < term.Count; i++)
                    if (i < over || now - term[i].TerminalMs > TerminalKeepMs)
                    {
                        byRef.Remove(term[i].Ref);
                        Tracked x;
                        string id = term[i].Key + "\u001f" + term[i].Id;
                        if (byId.TryGetValue(id, out x) && ReferenceEquals(x, term[i])) byId.Remove(id);
                    }
            }
        }

        internal int TrackedCount { get { lock (gate) { return byRef.Count; } } }

        // === the verdict ========================================================
        // The orders whose LIVE state Judge needs: every non-terminal one of the key, and
        // the ones that ended in the last 5 s. Read their properties outside any lock.
        public List<object> LiveRefs(string key, long now)
        {
            var res = new List<object>();
            lock (gate)
            {
                foreach (Tracked t in byRef.Values)
                    if (t.Key == key && (t.TerminalMs < 0 || now - t.TerminalMs <= TerminalFoldMs)) res.Add(t.Ref);
            }
            return res;
        }

        public SettleVerdict Judge(string key, bool cKnown, int c, List<LiveOrder> live, long now)
        {
            lock (gate)
            {
                var v = new SettleVerdict { CKnown = cKnown, C = c };
                KeyState k;
                if (!keys.TryGetValue(key, out k)) return v;
                // The live Filled runs up to 32 ms ahead of its own event: fold it first.
                if (live != null)
                    foreach (LiveOrder o in live)
                    {
                        Tracked t;
                        if (o.Ref == null || !byRef.TryGetValue(o.Ref, out t)) continue;
                        CountFillLocked(t, o.Filled, now);
                        if (o.Phase != SettlePhase.Unknown && o.Phase != t.Phase) PhaseLocked(t, o.Phase, now);
                    }
                v.Trusted = k.Trusted; v.L = k.L; v.E = k.E; v.ESeen = k.ESeen;
                if (!cKnown || !k.Trusted) return v;

                long win = WindowLocked();
                bool liveMarket = false;
                if (k.L != c) v.Why |= UnsettledWhy.LedgerVsPosition;
                if (k.ESeen && k.E != c) v.Why |= UnsettledWhy.EventVsPosition;
                foreach (Tracked t in byRef.Values)
                {
                    if (t.Key != key || !t.ExitSide || t.TerminalMs >= 0) continue;
                    if (t.Kind == SettleOrderKind.Market)
                    {
                        liveMarket = true;
                        if (now - t.FirstSeenMs < win) v.Why |= UnsettledWhy.LiveMarketExit;
                    }
                    else if (t.Kind == SettleOrderKind.Stop && IsTriggered(t.Phase) && now - t.WorkingMs < win)
                        v.Why |= UnsettledWhy.TriggeredStop;
                }
                if (k.HasFlatten && k.FlattenHeld && now - k.FlattenMs < win)
                {
                    // 113-237 ms pass between Flatten and its Close order: until it is over, a
                    // second Flatten would size itself from the same position.
                    bool over = !liveMarket && (k.FlattenDead || (k.FlattenSawFlat && now - k.FlattenMs >= OwnFlattenMinMs));
                    if (!over) v.Why |= UnsettledWhy.OwnFlatten;
                }
                if (v.Why != UnsettledWhy.None) v.Kind = VerdictKind.Unsettled;
                else if (c != 0) v.Kind = VerdictKind.SettledHeld;
                else if (k.ESeen) v.Kind = VerdictKind.SettledFlat;
                return v;
            }
        }

        // Unsettled ONLY because our own Flatten is still running (its own market Close may
        // already be live): a second one adds nothing.
        public static bool OnlyOwnFlatten(SettleVerdict v)
        {
            return v.Kind == VerdictKind.Unsettled && (v.Why & UnsettledWhy.OwnFlatten) != 0
                && (v.Why & ~(UnsettledWhy.OwnFlatten | UnsettledWhy.LiveMarketExit)) == 0;
        }

        public bool CloseInFlight(string key, long now)
        {
            lock (gate)
            {
                if (held.ContainsKey(key)) return true;
                KeyState k;
                return keys.TryGetValue(key, out k) && k.HasFlatten && now - k.FlattenMs < OwnFlattenMinMs;
            }
        }

        // === held closes ========================================================
        // First look at a close frame. Judge = the caller reads NT8 and decides. Joined =
        // one close of this key is already held (the first deadline stays; a market close
        // replaces a limit one). RunNow = run `payload` now: the gate is off, the link is
        // down or the settle thread does not answer; a close already held for the key is
        // taken with it, so it runs once.
        public CloseRoute OnCloseFrame(string key, bool isLimit, object incoming, bool linkUp, bool threadAlive, long now, out object payload, out IDisposable timer)
        {
            payload = incoming;
            timer = null;
            lock (gate)
            {
                HeldClose h;
                bool have = held.TryGetValue(key, out h);
                if (switches.SettleMs <= 0 || !linkUp || !threadAlive || StaleLocked(now))
                {
                    if (have)
                    {
                        held.Remove(key);
                        timer = h.Timer;
                        if (isLimit && !h.IsLimit) payload = h.Payload;
                        IdleLocked();
                    }
                    return CloseRoute.RunNow;
                }
                if (!have) return CloseRoute.Judge;
                if (h.IsLimit && !isLimit) { h.IsLimit = false; h.Payload = incoming; }
                return CloseRoute.Joined;
            }
        }

        public long NewToken() { lock (gate) { return nextToken++; } }

        // The LAST statement of a hold: after it the close belongs to the list.
        public void Hold(HeldClose h, long now)
        {
            lock (gate)
            {
                if (held.ContainsKey(h.Key)) throw new InvalidOperationException("a close is already held for this key");
                MarkBusyLocked(now);
                held[h.Key] = h;
            }
        }

        // Exactly once: whoever gets it runs it, then calls ReleaseDone.
        public HeldClose Take(long token)
        {
            lock (gate)
            {
                foreach (HeldClose h in held.Values)
                    if (h.Token == token)
                    {
                        held.Remove(h.Key);
                        releasing++;
                        IdleLocked();
                        return h;
                    }
                return null;
            }
        }

        public void ReleaseDone() { lock (gate) { if (releasing > 0) releasing--; } }

        public HeldClose PeekHeld(string key) { lock (gate) { HeldClose h; return held.TryGetValue(key, out h) ? h : null; } }

        public List<HeldClose> HeldSnapshot() { lock (gate) { return new List<HeldClose>(held.Values); } }

        // One look at a held close. Returns why it must be released now, or null.
        public string CheckHeld(long token, SettleVerdict v, long now)
        {
            lock (gate)
            {
                HeldClose h = null;
                foreach (HeldClose x in held.Values) if (x.Token == token) { h = x; break; }
                if (h == null) return null;
                if (switches.SettleMs <= 0) return "gate switched off";
                if (now >= h.DeadlineMs) return "NOT settled after " + (now - h.ArrivedMs).ToString(CultureInfo.InvariantCulture) + " ms, closing anyway";
                if (v.Kind == VerdictKind.Unknown) return "state unknown";
                if (v.Kind == VerdictKind.Unsettled) { h.SettledSeen = false; return null; }
                if (!h.SettledSeen || h.SettledC != v.C) { h.SettledSeen = true; h.SettledMs = now; h.SettledC = v.C; return null; }
                return now - h.SettledMs >= TwoCheckGapMs ? "settled" : null;
            }
        }

        // What the maintenance answer must report: work that has not run yet.
        public int PendingCount { get { lock (gate) { return held.Count + releasing + marks.Count; } } }

        // === the settle thread's pulse =========================================
        private bool BusyLocked() { return held.Count > 0 || marks.Count > 0; }
        public bool Busy { get { lock (gate) { return BusyLocked(); } } }
        private void MarkBusyLocked(long now) { if (!BusyLocked()) busySinceMs = now; }
        private void IdleLocked() { if (!BusyLocked()) busySinceMs = -1; }

        public void Tick(long now) { lock (gate) { lastTickMs = now; } }

        private bool StaleLocked(long now)
        {
            if (lastTickMs < 0) return true;
            if (BusyLocked()) return now - Math.Max(lastTickMs, busySinceMs) > TickStaleBusyMs;
            return now - lastTickMs > TickStaleIdleMs;
        }

        // === our own exit orders and the sweep =================================
        public void RegisterOwnExit(string key, object orderRef, bool isLx, bool buy, long now)
        {
            if (orderRef == null) return;
            lock (gate)
            {
                ownExits.Add(new OwnExit { Seq = nextSeq++, Key = key, Ref = orderRef, IsLx = isLx, Buy = buy, RegisteredMs = now });
            }
        }

        public void MarkOwnExitSubmitted(object orderRef)
        {
            lock (gate)
            {
                foreach (OwnExit o in ownExits) if (ReferenceEquals(o.Ref, orderRef)) o.Submitted = true;
                foreach (SweepMark m in marks) foreach (OwnExit o in m.Items) if (ReferenceEquals(o.Ref, orderRef)) o.Submitted = true;
            }
        }

        public void DropOwnExit(object orderRef)
        {
            lock (gate)
            {
                for (int i = ownExits.Count - 1; i >= 0; i--) if (ReferenceEquals(ownExits[i].Ref, orderRef)) ownExits.RemoveAt(i);
                foreach (SweepMark m in marks)
                    for (int i = m.Items.Count - 1; i >= 0; i--) if (ReferenceEquals(m.Items[i].Ref, orderRef)) m.Items.RemoveAt(i);
            }
        }

        public List<OwnExit> OwnLx(string key)
        {
            var res = new List<OwnExit>();
            lock (gate) { foreach (OwnExit o in ownExits) if (o.IsLx && o.Key == key) res.Add(o); }
            return res;
        }

        public List<SweepMark> MarksSnapshot() { lock (gate) { return new List<SweepMark>(marks); } }

        // The settle thread ended: nobody would ever finish a mark.
        public void StopSweeps() { lock (gate) { sweepsStopped = true; marks.Clear(); IdleLocked(); } }

        public void DropMark(SweepMark m) { lock (gate) { marks.Remove(m); IdleLocked(); } }

        // One look at a sweep mark. `phases` is the live phase of each of m.Items, same order.
        // cancel = the orders to cancel on this step; why = the reason of a Drop.
        public SweepStep CheckSweep(SweepMark m, SettleVerdict v, List<LiveOrder> live, long now, out List<object> cancel, out string why)
        {
            cancel = new List<object>();
            why = "";
            lock (gate)
            {
                long win = WindowLocked();
                long age = now - m.CreatedMs;
                var phase = new Dictionary<object, SettlePhase>(new RefEq());
                if (live != null) foreach (LiveOrder o in live) if (o.Ref != null) phase[o.Ref] = o.Phase;
                bool allDone = true;
                foreach (OwnExit o in m.Items)
                {
                    SettlePhase p;
                    if (!phase.TryGetValue(o.Ref, out p)) p = SettlePhase.Unknown;
                    if (IsTerminal(p) || p == SettlePhase.CancelPending) continue;
                    allDone = false;
                    if (o.Submitted) cancel.Add(o.Ref);
                }
                if (allDone) { cancel.Clear(); return SweepStep.Done; }
                if (v.Kind == VerdictKind.SettledHeld)
                {
                    cancel.Clear();
                    why = "a position is held again";
                    return SweepStep.Drop;
                }
                if (age > 2 * win) { cancel.Clear(); why = "not all cancelled in time"; return SweepStep.Drop; }
                if (v.Kind != VerdictKind.SettledFlat)
                {
                    cancel.Clear();
                    m.FlatSeen = false;
                    m.Confirmed = false; // two fresh checks again before any cancel
                    if (age > win) { why = "never settled flat"; return SweepStep.Drop; }
                    return SweepStep.Wait;
                }
                if (!m.FlatSeen) { m.FlatSeen = true; m.FlatMs = now; }
                if (now - m.FlatMs >= TwoCheckGapMs) m.Confirmed = true;
                if (!m.Confirmed)
                {
                    cancel.Clear();
                    if (age > win) { why = "never settled flat"; return SweepStep.Drop; }
                    return SweepStep.Wait;
                }
                return cancel.Count > 0 ? SweepStep.Cancel : SweepStep.Wait;
            }
        }
    }
}
