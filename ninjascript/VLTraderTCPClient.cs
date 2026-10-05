// VLTraderTCPClient.cs — Plan 1.5 TCP NinjaTrader bridge (alternative to CSV).
//
// NinjaScript AddOn that connects to the VL Trader Go bot via local TCP
// (127.0.0.1:36974) and exchanges length-prefixed JSON frames with the Go-side
// server in provider/ninjatrader/tcp_server.go. This file CANNOT be compiled
// in WSL2 — the operator compiles on Windows via NT8's NinjaScript editor (F5)
// or Visual Studio. See vltrader_tcp_README.md for the install procedure and
// vltrader_tcp_PROTOCOL.md for the wire-format spec.
//
// Spec: docs/superpowers/plans/2026-05-22-nq-databento-ninjatrader.md L4343-4447.
// ADR : docs/adr/ADR-001-csv-bridge-vs-tcp.md.

#region Using declarations
using System;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using System.Linq;
using System.Net.Sockets;
using System.Text;
using System.Threading;
using NinjaTrader.Cbi;
using NinjaTrader.Core;
using NinjaTrader.NinjaScript;
#endregion

namespace NinjaTrader.NinjaScript.AddOns
{
    /// <summary>
    /// VLTrader TCP client. Loaded automatically by NT8 when dropped in
    /// Documents\NinjaTrader 8\bin\Custom\AddOns\. Lifecycle is driven by
    /// OnStateChange (SetDefaults / Active / Terminated).
    /// </summary>
    public class VLTraderTCPClient : AddOnBase
    {
        // === Connection constants (must match Go-side tcp_server.go) ===
        private const string GO_SERVER_HOST          = "127.0.0.1";
        private const int    GO_SERVER_PORT          = 36974; // NOT NT's ATI port 36973
        private const int    HEARTBEAT_INTERVAL_MS   = 30000; // spec L4408
        // F12: the working-order snapshot period. The Go side calls a book older
        // than 2x this STALE (trader.DefaultOrderSnapshotSecs = 30). Changing one
        // without the other changes what "stale" means on only one side.
        private const int    ORDER_SNAPSHOT_INTERVAL_MS = 30000;
        private const int    RECONNECT_INTERVAL_MS   = 5000;  // spec L4415
        private const int    STALE_SIGNAL_AGE_SECONDS = 60;   // spec L4414
        private const int    SEEN_SIGNAL_TTL_MINUTES  = 10;   // FIX-DOUBLE-ENTRY: replay dedupe memory (≥10 min per CTO ruling)
        private const int    SEEN_SIGNAL_CAP          = 1024; // FIX-DOUBLE-ENTRY: bounded seen set
        // Wire-protocol generation. v2 = symbol-tagged fills + the hello handshake.
        // v3 (A2/G1) = identity stamp + echo-verify: order/modify/cancel frames carry
        // (trader_id, account, seq) and this AddOn echoes all three on fill/close/
        // reject so Go verifies it acted for the originator. MUST match
        // provider/ninjatrader/tcp_framing.go ProtocolVersion — bump ONLY with a
        // lockstep C#+Go ship.
        private const int    PROTOCOL_VERSION        = 3;
        // E7 capability handshake (2026-08-30): reported on every heartbeat so
        // the Go side refuses frame types this build hasn't proven. Bump on any
        // additive wire change; Go gates on FarSideBuildE7 in tcp_framing.go.
        // 2026-09-22-m2 (W-ONE-BUTTON M2): maintenance / maintenance_ack frames
        // + the hello epoch fields. 2026-09-23-m21 (M2.1): census `settled`,
        // no nested census locks, source_hash at activation. The ISO-date
        // prefix is kept (CTO ruling Q3).
        private const string  VL_BUILD_ID             = "2026-10-04-d1";
        private const int    MAX_FRAME_BYTES         = 1 << 20; // 1 MB, spec L4376

        // === State ===
        private TcpClient                client;
        private NetworkStream            stream;
        private Thread                   readerThread;
        private Thread                   heartbeatThread;
        private CancellationTokenSource  cts;
        private Account                  account;   // primary trading account

        // W-ONE-BUTTON M2 site 7 — the installation maintenance hold, as Go last
        // told it on THIS connection (reset on every connect: Go re-sends it at
        // accept while held, before any queued signal). While held, HandleSignal
        // refuses NEW entries; protection, brackets on fill, part-fill amends,
        // flatten, cancel and modify are never touched.
        private volatile bool            maintenanceHeld  = false;
        private volatile string          maintenanceJobId = "";
        // CTO ruling Q3 — minted once per AddOn activation, carried on every hello.
        private readonly string          activationNonce  = Guid.NewGuid().ToString("N");
        private string                   sourceHashCache;   // computed once per activation
        private readonly object          writeLock = new object();

        // Track signal_id → original entry price for slippage calculation.
        private readonly Dictionary<string, double> signalEntryByOco = new Dictionary<string, double>();
        private readonly Dictionary<string, double> signalTickSizeByOco = new Dictionary<string, double>();
        private readonly object signalMapLock = new object();

        // Pending protective brackets, keyed by signal_id. The SL/TP are placed
        // only AFTER the entry fills (see OnOrderUpdate) — placing them in the
        // entry's OCO group caused NT to cancel them the instant the entry
        // filled (OCO = one-cancels-other), leaving the position unprotected.
        private readonly Dictionary<string, PendingBracket> pendingBrackets = new Dictionary<string, PendingBracket>();

        // PHASE 3 (per-order routing) — the set of accounts whose OrderUpdate we have
        // hooked to OnOrderUpdate. The fill→bracket→fill-frame lifecycle must fire for
        // EVERY account we route an order to, not just the active one. This set is the
        // single source of truth, so we never double-subscribe (which would duplicate
        // fills). Per-account balance/position snapshots remain Phase 4.
        private readonly HashSet<Account> orderSubscribed = new HashSet<Account>();
        private readonly object orderSubLock = new object();

        // PositionUpdate is subscribed for EVERY SIM account (not just the active
        // one) so a trader bound to a non-active sub-account gets an on-change
        // positions frame instantly — the reconcile-before-open flat signal no longer
        // waits on the 30s heartbeat. Tracked here for clean unsubscribe on Terminate.
        private readonly HashSet<Account> positionSubscribed = new HashSet<Account>();
        private readonly object positionSubLock = new object();

        // PHASE 4 (per-account close) — which account holds each open position, keyed by
        // ROOT symbol (e.g. "MNQ"). Populated on entry-fill (from e.Order.Account) and
        // cleared on exit-fill, so HandleClosePosition flattens the CORRECT account
        // instead of the active one. Back-compat: one trader → resolves to Sim101.
        private readonly Dictionary<string, Account> positionAccountBySymbol = new Dictionary<string, Account>();
        private readonly object posAcctLock = new object();

        private class PendingBracket
        {
            public Instrument  Instrument;
            public OrderAction ExitAction;
            public int         Qty;
            public double      Sl;
            public double      Tp;
            public Account     Account;   // PHASE 3: the routed submit account (SL/TP follow the entry)
            // REVIEW-353: the split AT ENTRY — leg 1 = ceil(n/2) with its OWN TP;
            // leg 2 = the runner with TP = Tp. 0 = the single-bracket legacy path.
            public int         Leg1Qty;
            public double      Leg1Tp;
        }

        // A bracket whose entry has FILLED and whose SL/TP now rest at the exchange,
        // keyed by signal_id. Populated in SubmitBracketOnEntryFill, removed on exit
        // fill. Lets HandleMoveStop find the live resting stop to MOVE it (auto-
        // breakeven) without closing the position.
        private class PlacedBracket
        {
            public Order       SlOrder;     // the live resting stop order (movable)
            public Order       TpOrder;     // 4.3: the live take-profit order — tracked so
                                           // CancelBracketsFor can cancel BOTH legs after a
                                           // limit exit fills (an orphaned TP could re-enter)
            public Account     Account;
            public Instrument  Instrument;
            public OrderAction ExitAction;
            public int         Qty;
            public string      ExitOco;     // the OCO group the SL + TP share
            public double      TickSize;
            // REVIEW-353: leg 2's OCO pair (leg 1 rides SlOrder/TpOrder).
            // Leg2Qty == 0 → the single-bracket legacy shape.
            public int         Leg1Qty;
            public int         Leg2Qty;
            public double      Leg1Tp;
            public double      RunnerTp;   // leg 2's TP (the trade target)
            public Order       SlOrder2;
            public Order       TpOrder2;
            public string      ExitOco2;
            public int         Leg1Exited; // REVIEW-SPLIT-2 P1 fold: leg-1 contracts already exited (Leg1Qty is NEVER zeroed)
            public double      LastStop;   // last known leg-1 stop price (leg-2 creation after leg 1 exits)
        }
        private readonly Dictionary<string, PlacedBracket> placedBrackets = new Dictionary<string, PlacedBracket>();

        // PHASE 2 armed orders — resting LIMIT entries keyed by signal_id, so Go's
        // cancel_order can pull them before they fill. Removed on entry fill/reject.
        private readonly Dictionary<string, Order> workingEntries = new Dictionary<string, Order>();

        // PHASE 2 armed orders — per-order last STATE (order_update dedup): the
        // order_update frame emits every state change exactly once per order name.
        private readonly Dictionary<string, string> lastOrderState = new Dictionary<string, string>();

        // A2 (G1) — the identity (trader_id + seq) the Go side stamped on each order,
        // keyed by signal_id. The AddOn echoes it on the paired fill/close/reject so
        // Go verifies it acted for the ORIGINATING trader. Guarded by signalMapLock.
        private class SignalIdentity { public string TraderId; public long Seq; }
        private readonly Dictionary<string, SignalIdentity> signalIdentity = new Dictionary<string, SignalIdentity>();
        // FIX-DOUBLE-ENTRY — seen-signal set for replayed entry frames: keyed by
        // signal_id, TTL SEEN_SIGNAL_TTL_MINUTES, NEVER cleared on fill (a
        // post-fill duplicate must be ignored too). Bounded by SEEN_SIGNAL_CAP.
        private readonly Dictionary<string, DateTime> seenSignals = new Dictionary<string, DateTime>();

        // A3 (G2) — session account ALLOWLIST. The AddOn executes ONLY on accounts
        // this session's bound traders use: the active account, any account the owner
        // account_selects, and any account a Go-stamped signal declares (A1 verifies
        // signal.account == the trader's bound account on the Go side). A submit target
        // outside this set is HARD-REFUSED with a rejected (error) fill frame —
        // defense in depth so a routing bug or a rogue/mis-stamped account can never
        // reach CreateOrder, on top of the SIM/connected/resolved guards.
        private readonly HashSet<string> sessionAccounts = new HashSet<string>();
        private readonly object sessionAccountsLock = new object();
        // True once the Go side has DECLARED the allowlist (account_register). Until
        // then the AddOn fails OPEN (a pre-A3 Go never declares → don't brick trading).
        private bool sessionAccountsDeclared = false;
        private void RegisterSessionAccount(string name)
        {
            if (string.IsNullOrEmpty(name)) return;
            lock (sessionAccountsLock) { sessionAccounts.Add(name); }
        }
        // IsSessionAccount enforces membership ONLY once the allowlist was declared by
        // Go. Before declaration (legacy Go) it returns true (fail-open) so the deploy
        // window never bricks; after declaration a non-member is refused.
        private bool IsSessionAccount(string name)
        {
            lock (sessionAccountsLock)
            {
                if (!sessionAccountsDeclared) return true; // legacy / not yet declared
                return !string.IsNullOrEmpty(name) && sessionAccounts.Contains(name);
            }
        }

        // A3 (G2) — Go declares the AUTHORITATIVE bound-account allowlist (sent on
        // connect before any signal, and on each new bind). REPLACE the set with it;
        // from now on execution is enforced against this owner-declared set — never a
        // signal-derived one. account_select still adds the owner-selected active
        // account as belt-and-suspenders.
        private void HandleAccountRegister(Dictionary<string, object> p)
        {
            if (p == null) { LogWarn("VLTraderTCPClient: account_register empty payload"); return; }
            var list = p.ContainsKey("accounts") ? p["accounts"] as List<object> : null;
            var parsed = new HashSet<string>();
            if (list != null)
                foreach (var a in list)
                {
                    string name = a as string;
                    if (!string.IsNullOrEmpty(name)) parsed.Add(name);
                }
            // SAFETY: never declare an EMPTY allowlist (would refuse every account and
            // brick trading). Go only sends non-empty sets; a malformed/empty frame is
            // ignored and the AddOn stays in its prior state (fail-open if never declared).
            if (parsed.Count == 0)
            {
                LogWarn("VLTraderTCPClient: account_register carried no accounts — ignoring (allowlist unchanged)");
                return;
            }
            lock (sessionAccountsLock)
            {
                sessionAccounts.Clear();
                foreach (var n in parsed) sessionAccounts.Add(n);
                sessionAccountsDeclared = true;
            }
            LogInfo("VLTraderTCPClient: account_register allowlist declared (" + parsed.Count + " accounts)");
        }

        // Plan 4.4 Stage 1 — multi-timeframe BarsRequest subscriptions. Owns
        // its own state; calls back into SendFrame() which serializes through
        // writeLock so bar frames cannot interleave with signal/fill bytes.
        private VLBarsSubscriptionManager barsManager;

        // HISTORY IMPORT (wave 101) — named-contract historical pulls, kept
        // strictly separate from the live subscription manager.
        private VLHistoryPullManager historyPulls;

        // Phase 0 — data-feed reconnect recovery state. NT8 does NOT auto-resume
        // a BarsRequest after the price feed drops, so we recreate them on
        // recovery (VLBarsSubscriptionManager.OnConnectionReconnected). Guard so
        // only a genuine was-lost -> Connected transition fires the recreate.
        private readonly object connStatusLock = new object();
        private bool dataFeedWasLost = false;
        // Debounce for the recreate-on-(re)connect path. The recreate is now a
        // full multi-timeframe/multi-symbol BarsRequest refetch with a DEEP
        // (2000-bar / ~33h) lookback so it can backfill an overnight feed-gap
        // from NT8's DB. That depth makes thrashing expensive, so a rapidly
        // flapping feed must coalesce into ONE rebuild per cooldown rather than
        // rebuilding on every micro-blip. A genuine recovery (or startup) still
        // fires once; the 20s FAST_STALL fast-guard backstops a .Update that dies
        // inside the cooldown window.
        private DateTime lastRecreateUtc = DateTime.MinValue;
        private static readonly TimeSpan RecreateDebounce = TimeSpan.FromSeconds(30);

        protected override void OnStateChange()
        {
            if (State == State.SetDefaults)
            {
                Name = "VLTrader TCP Client";
                Description = "Connects to the VL Trader Go bot via local TCP (Plan 1.5). " +
                              "Receives signals over the wire, places OCO bracket orders, " +
                              "and emits fills back. Falls back to CSV bridge if disabled.";
            }
            else if (State == State.Active)
            {
                cts = new CancellationTokenSource();
                ResolveAccount();
                if (account != null)
                {
                    SubscribeOrderUpdate(account);   // PHASE 3: dedup'd OrderUpdate subscription
                    // Plan 4.11 — emit the real account balance on cash/PnL
                    // changes so the dashboard reflects the live SIM account.
                    account.AccountItemUpdate += OnAccountItemUpdate;
                    // Open-position read-back — emit each account's open positions on
                    // ANY change (incl. MANUAL trades in NT8) so the Go side / AI always
                    // tracks NT8's real position. Subscribed for ALL SIM accounts (not
                    // just the active one) so a non-active-account trader gets on-change
                    // frames, not just the 30s heartbeat. Initial snapshot also on
                    // connect (SendAllOpenPositions).
                    SubscribePositionUpdatesAllSim();
                }
                // Plan 4.4 Stage 1 — instantiate the bars manager BEFORE the
                // reader thread starts so an early bars_subscribe frame has a
                // destination. SendFrame is wired through this instance so
                // bar frames inherit the same writeLock + encoder used by the
                // proven signal/fill/heartbeat path.
                barsManager = new VLBarsSubscriptionManager(SendFrame, LogInfo, LogWarn);

                // HISTORY IMPORT (wave 101) — instantiate the named-contract
                // history pull manager beside the bars manager. It owns ONLY
                // disposable, per-request BarsRequests for EXPLICIT contract
                // names; it never touches the live subscriptions.
                historyPulls = new VLHistoryPullManager(SendFrame, LogInfo, LogWarn);

                // Phase 0 — hook the NT8 data-feed status so BarsRequests get
                // recreated when the NT8<->provider price feed recovers. Without
                // this, NT8 silently stops delivering bars after a feed reconnect
                // and the Go BarCache goes stale with no recovery. Static event
                // across all connections; the handler filters on PriceStatus.
                Connection.ConnectionStatusUpdate += OnVLConnectionStatusUpdate;

                // W-ONE-BUTTON M2.1 — hash the AddOn source NOW, at activation, so
                // the hello's source_hash describes what this activation compiled
                // (a copy made before the next F5 must not change it).
                sourceHashCache = ComputeSourceHash();

                readerThread = new Thread(() => RunConnectionLoop(cts.Token))
                {
                    IsBackground = true,
                    Name = "VLTrader-TCP-Reader"
                };
                heartbeatThread = new Thread(() => RunHeartbeatLoop(cts.Token))
                {
                    IsBackground = true,
                    Name = "VLTrader-TCP-Heartbeat"
                };
                readerThread.Start();
                heartbeatThread.Start();
                LogInfo("VLTraderTCPClient: AddOn Active, connecting to "
                        + GO_SERVER_HOST + ":" + GO_SERVER_PORT);
            }
            else if (State == State.Terminated)
            {
                try { cts?.Cancel(); } catch { }
                // PHASE 3: unsubscribe OrderUpdate from EVERY account we hooked (active + routed)
                lock (orderSubLock)
                {
                    foreach (var a in orderSubscribed) { try { a.OrderUpdate -= OnOrderUpdate; } catch { } }
                    orderSubscribed.Clear();
                }
                UnsubscribeAllPositionUpdates();
                if (account != null)
                {
                    try { account.AccountItemUpdate -= OnAccountItemUpdate; } catch { }
                }
                try { Connection.ConnectionStatusUpdate -= OnVLConnectionStatusUpdate; } catch { }
                try { barsManager?.DisposeAll(); } catch { }
                try { historyPulls?.DisposeAll(); } catch { }
                try { stream?.Close(); } catch { }
                try { client?.Close(); } catch { }
                LogInfo("VLTraderTCPClient: AddOn Terminated");
            }
        }

        // Phase 0 — data-feed reconnect recovery handler. Subscribed to the
        // static Connection.ConnectionStatusUpdate in State.Active and
        // unsubscribed in State.Terminated. NT8 raises this for every
        // connection's status change on a background thread, so we work off a
        // copy and keep the body light. DATA feed = PriceStatus (Status is the
        // order/broker connection). We recreate BarsRequests only on a genuine
        // data-feed recovery — PriceStatus was lost (or Disconnected) and is now
        // Connected — which also covers the ConnectionLost -> Connecting ->
        // Connected path. The existing OnConnectionReconnected() does the
        // dispose + front-month re-resolve + re-subscribe.
        private void OnVLConnectionStatusUpdate(object sender, ConnectionStatusEventArgs e)
        {
            // Multi-threading: NT8 staff guidance — copy the args, they may race
            // ahead of us while we process.
            ConnectionStatusEventArgs eCopy = e;
            try
            {
                string priceStatus = eCopy.PriceStatus.ToString();
                string prevPriceStatus = eCopy.PreviousPriceStatus.ToString();
                bool fireRecreate = false;

                lock (connStatusLock)
                {
                    if (priceStatus == "ConnectionLost" || priceStatus == "Disconnected")
                    {
                        if (!dataFeedWasLost)
                        {
                            dataFeedWasLost = true;
                            LogWarn("VLTraderTCPClient: data feed lost (PriceStatus="
                                    + priceStatus + ") — BarsRequests will be recreated on recovery");
                        }
                    }
                    else if (priceStatus == "Connected")
                    {
                        // Recreate on ANY transition INTO Connected — recovery
                        // (dataFeedWasLost) AND a FRESH STARTUP (PreviousPriceStatus =
                        // Connecting/Disconnected). The old loss-only guard left a clean
                        // startup with no recreate, so .Update sat dead from the restart
                        // until the slow 75-min watchdog. PreviousPriceStatus != Connected
                        // means a real transition, so this fires once per connect (not on
                        // repeated Connected events where the price feed never dropped).
                        bool freshConnect = (prevPriceStatus != "Connected");
                        if (dataFeedWasLost || freshConnect)
                        {
                            dataFeedWasLost = false;  // clear inside the lock -> no double-fire
                            // Flap debounce: skip the (deep, expensive) rebuild if one
                            // fired within RecreateDebounce. The first connect
                            // (lastRecreateUtc=MinValue) always passes; a genuine
                            // resume-after-a-real-gap passes; only rapid flaps coalesce.
                            DateTime nowUtc = DateTime.UtcNow;
                            if (nowUtc - lastRecreateUtc >= RecreateDebounce)
                            {
                                lastRecreateUtc = nowUtc;
                                fireRecreate = true;
                            }
                            else
                            {
                                LogInfo("VLTraderTCPClient: feed Connected but a BarsRequest rebuild "
                                        + "fired <" + (int)RecreateDebounce.TotalSeconds
                                        + "s ago — debounced (feed-flap guard)");
                            }
                        }
                    }
                }

                if (fireRecreate)
                {
                    LogInfo("VLTraderTCPClient: data feed Connected (prev=" + prevPriceStatus
                            + ") — recreating BarsRequests via OnConnectionReconnected");
                    barsManager?.OnConnectionReconnected();
                }

                // TRACK A — forward the price-feed status to Go so the bot can gate
                // opens/closes when the feed is down (the SIM rejects "no market
                // data" — the upstream condition behind the phantom-close mess).
                // Emitted on every connection-status transition.
                SendFeedStatusFrame(priceStatus);
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: OnVLConnectionStatusUpdate failed: " + ex.Message);
            }
        }

        // === Account resolution ===
        // Selection order (first match wins):
        //   1. The account name in %USERPROFILE%\VLTrader\account.txt
        //      (operator-editable — one line, e.g. "Sim101" or "MyPropAcct").
        //      NOTE (R5, 2026-10-03): the legacy pre-rename account.txt
        //      migration branch was REMOVED in the cancel-confirm AddOn update
        //      (one F5) — the pre-rename folder holds only data\ and VLTrader
        //      did not exist there yet, so there is nothing to migrate. If you
        //      ever had the pre-rename folder's account.txt, move it to
        //      VLTrader\account.txt yourself.
        //   2. "Sim101" (SIM default).
        //   3. The first available account.
        private void ResolveAccount()
        {
            string preferred = ReadAccountFile("VLTrader");

            lock (Account.All)
            {
                if (preferred != null)
                {
                    foreach (var a in Account.All)
                        if (a.Name == preferred) { account = a; break; }
                    if (account == null)
                        LogWarn("VLTraderTCPClient: account.txt requested '" + preferred
                                + "' but no such account — falling back");
                }
                if (account == null)
                    foreach (var a in Account.All)
                        if (a.Name == "Sim101") { account = a; break; }
                if (account == null && Account.All.Count > 0) account = Account.All[0];
            }

            if (account == null)
            {
                LogWarn("VLTraderTCPClient: no account resolved; signals will be rejected");
            }
            else
            {
                LogInfo("VLTraderTCPClient: using account " + account.Name
                        + (preferred != null && account.Name == preferred ? " (from account.txt)" : ""));
            }
        }

        // ReadAccountFile reads and trims %USERPROFILE%\<folder>\account.txt.
        // An absent file or an empty value returns null.
        private string ReadAccountFile(string folder)
        {
            try
            {
                string path = Path.Combine(
                    Environment.GetEnvironmentVariable("USERPROFILE") ?? "",
                    folder, "account.txt");
                if (File.Exists(path))
                {
                    string value = File.ReadAllText(path).Trim();
                    return value.Length == 0 ? null : value;
                }
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: could not read " + folder + "\\account.txt: " + ex.Message);
            }
            return null;
        }

        // === SIM detection ===
        // Returns true if the account is a simulation account. Tries Account.Simulation
        // property first (NT8 8.1+), then falls back to name pattern matching (Sim*).
        private bool IsSimAccount(Account a)
        {
            if (a == null) return false;
            try
            {
                // NT8 8.1+ exposes Account.Simulation property
                var simProp = a.GetType().GetProperty("Simulation");
                if (simProp != null && simProp.PropertyType == typeof(bool))
                {
                    return (bool)simProp.GetValue(a, null);
                }
            }
            catch { }
            // Fallback: name-based detection (Sim prefix is NT8 standard for SIM accounts)
            return a.Name != null && a.Name.StartsWith("Sim");
        }

        // === PHASE 3 — dedup'd OrderUpdate (un)subscription ===
        // SubscribeOrderUpdate is idempotent: a no-op if the account is already hooked,
        // so routing to the active account (or re-routing to an already-routed one) never
        // double-hooks OnOrderUpdate (which would duplicate fills). ALL OrderUpdate
        // subscribe/unsubscribe flows through these so `orderSubscribed` stays the single
        // source of truth. (AccountItemUpdate stays active-account-only; PositionUpdate
        // is now all-SIM-accounts via SubscribePositionUpdatesAllSim — reconcile fix.)
        private void SubscribeOrderUpdate(Account a)
        {
            if (a == null) return;
            bool added = false;
            lock (orderSubLock)
            {
                if (!orderSubscribed.Contains(a))
                {
                    a.OrderUpdate += OnOrderUpdate;
                    orderSubscribed.Add(a);
                    added = true;
                }
            }
            if (added) LogInfo("VLTraderTCPClient: subscribed OrderUpdate on account '" + a.Name + "'");
        }
        private void UnsubscribeOrderUpdate(Account a)
        {
            if (a == null) return;
            lock (orderSubLock)
            {
                if (!orderSubscribed.Contains(a)) return;
                try { a.OrderUpdate -= OnOrderUpdate; } catch { }
                orderSubscribed.Remove(a);
            }
        }

        // Subscribe OnPositionUpdate on EVERY SIM account (dedup'd via
        // positionSubscribed), so on-change position frames flow for a non-active
        // account too. Mirrors the balance all-account model; the reconcile-before-open
        // flat signal then arrives on change, not on the 30s heartbeat.
        private void SubscribePositionUpdatesAllSim()
        {
            lock (Account.All)
            {
                foreach (var a in Account.All)
                {
                    if (!IsSimAccount(a)) continue;
                    lock (positionSubLock)
                    {
                        if (positionSubscribed.Add(a))
                        {
                            a.PositionUpdate += OnPositionUpdate;
                            LogInfo("VLTraderTCPClient: subscribed PositionUpdate on account '" + a.Name + "'");
                        }
                    }
                }
            }
        }
        private void UnsubscribeAllPositionUpdates()
        {
            lock (positionSubLock)
            {
                foreach (var a in positionSubscribed) { try { a.PositionUpdate -= OnPositionUpdate; } catch { } }
                positionSubscribed.Clear();
            }
        }

        // === Connection loop with reconnect (spec L4415: every 5s) ===
        private void RunConnectionLoop(CancellationToken ct)
        {
            while (!ct.IsCancellationRequested)
            {
                try
                {
                    client = new TcpClient();
                    client.Connect(GO_SERVER_HOST, GO_SERVER_PORT);
                    stream = client.GetStream();
                    LogInfo("VLTraderTCPClient: CONNECTED");
                    // W-ONE-BUTTON M2 — a hold belongs to the connection that was
                    // told it. Go re-sends it at accept while held.
                    maintenanceHeld = false;
                    maintenanceJobId = "";

                    // P5.2 — hello MUST be the FIRST frame so the Go server
                    // can version-check before any data. On a mismatch the
                    // server refuses (closes); we land back in the reconnect
                    // loop with its loud mismatch log explaining why.
                    SendHello();
                    LogInfo("VLTraderTCPClient: About to call SendAccountsList");

                    // Trigger on connect in case accounts are already loaded
                    SendAccountsList();
                    LogInfo("VLTraderTCPClient: SendAccountsList completed");

                    // Plan 4.11 — push account balances immediately on (re)connect
                    // so the dashboard shows the real SIM balances without waiting
                    // for the next AccountItemUpdate. Seed EVERY SIM account (not
                    // just the active one) so a 2nd trader on another sub-account
                    // isn't stuck on a stale/absent balance until it goes active.
                    SendAllAccountBalances();
                    // Open-position read-back — snapshot EVERY SIM account's open
                    // positions on (re)connect so the Go side re-syncs each trader's
                    // tracked position after a restart/feed reconnect, not just the
                    // active account's (companion to the Phase 0 reconnect fix + the
                    // BUG A all-account positions fix).
                    SendAllOpenPositions();
                    RunReadLoop(ct);
                }
                catch (Exception ex)
                {
                    LogWarn("VLTraderTCPClient: connect failed: " + ex.Message);
                }
                finally
                {
                    try { stream?.Close(); } catch { }
                    try { client?.Close(); } catch { }
                    stream = null;
                    client = null;
                }
                if (!ct.IsCancellationRequested)
                {
                    Thread.Sleep(RECONNECT_INTERVAL_MS);
                }
            }
        }

        // === Read loop: 4-byte BE length prefix + JSON body (spec L4376) ===
        private void RunReadLoop(CancellationToken ct)
        {
            byte[] header = new byte[4];
            while (!ct.IsCancellationRequested && stream != null)
            {
                if (!ReadExact(stream, header, 4)) return;
                uint length =
                    ((uint)header[0] << 24) |
                    ((uint)header[1] << 16) |
                    ((uint)header[2] << 8)  |
                    ((uint)header[3]);
                if (length > MAX_FRAME_BYTES)
                {
                    // Spec L4416: oversized frame is a protocol error; close.
                    LogWarn("VLTraderTCPClient: oversized frame " + length + " bytes — closing");
                    return;
                }
                byte[] body = new byte[length];
                if (!ReadExact(stream, body, (int)length)) return;
                string json = Encoding.UTF8.GetString(body);
                HandleFrame(json);
            }
        }

        private bool ReadExact(NetworkStream s, byte[] buf, int n)
        {
            int got = 0;
            while (got < n)
            {
                int r;
                try
                {
                    r = s.Read(buf, got, n - got);
                }
                catch (Exception ex)
                {
                    LogWarn("VLTraderTCPClient: read err " + ex.Message);
                    return false;
                }
                if (r <= 0) return false;
                got += r;
            }
            return true;
        }

        // === Frame dispatch ===
        private void HandleFrame(string json)
        {
            try
            {
                var root = (Dictionary<string, object>)new JsonParser(json).Parse();
                string type = GetString(root, "type");
                Dictionary<string, object> payload = null;
                if (root.ContainsKey("payload"))
                {
                    payload = root["payload"] as Dictionary<string, object>;
                }
                if (type == "signal")
                {
                    HandleSignal(payload);
                }
                else if (type == "close_position")
                {
                    HandleClosePosition(payload);
                }
                else if (type == "move_stop")
                {
                    HandleMoveStop(payload);
                }
                else if (type == "cancel_order")
                {
                    // PHASE 2 armed orders — cancel a working limit entry and/or
                    // its protective bracket legs (managed Account.Cancel).
                    HandleCancelOrder(payload);
                }
                else if (type == "place_protective_stop")
                {
                    // D5 (2026-09-07) — restore a stop for a position the broker
                    // is holding UNPROTECTED. Standalone: no bracket, no OCO group.
                    HandlePlaceProtectiveStop(payload);
                }
                else if (type == "modify_bracket")
                {
                    // PHASE 2 armed orders — in-place SL/TP modify on the LIVE
                    // bracket (same safe Change pattern as move_stop).
                    HandleModifyBracket(payload);
                }
                else if (type == "account_select")
                {
                    HandleAccountSelect(payload);
                }
                else if (type == "account_register")
                {
                    HandleAccountRegister(payload);
                }
                else if (type == "maintenance")
                {
                    // W-ONE-BUTTON M2 site 7 — the installation hold.
                    HandleMaintenance(payload);
                }
                else if (type == "heartbeat")
                {
                    // Ack incoming heartbeats per spec L4410.
                    SendAck("heartbeat");
                }
                else if (type == "ack")
                {
                    // Server-side ack of our heartbeat/fill — informational only.
                }
                else if (type == "bars_subscribe")
                {
                    // Plan 4.4 Stage 1 — forward to the bars manager.
                    barsManager?.HandleBarsSubscribe(payload);
                }
                else if (type == "bars_unsubscribe")
                {
                    // Plan 4.4 Stage 1 — forward to the bars manager.
                    barsManager?.HandleBarsUnsubscribe(payload);
                }
                else if (type == "bars_history_request")
                {
                    // HISTORY IMPORT (wave 101) — named-contract pull. Zero
                    // interaction with the live subscription state.
                    historyPulls?.HandleBarsHistoryRequest(payload);
                }
                else if (type == "hello")
                {
                    // P5.2 — the Go server's hello reply. A version mismatch is
                    // logged LOUDLY (the server side refuses mismatched clients;
                    // this is the symmetric check so the operator sees it from
                    // the NT8 Output window too).
                    int goVersion = 0;
                    try { goVersion = GetInt(payload, "protocol_version"); } catch { }
                    if (goVersion != PROTOCOL_VERSION)
                    {
                        LogWarn("VLTraderTCPClient: *** PROTOCOL VERSION MISMATCH *** Go server v"
                                + goVersion + " vs AddOn v" + PROTOCOL_VERSION
                                + " — redeploy the C# AddOn + Go binary in LOCKSTEP (cp → F5 → NT8 restart)");
                    }
                    else
                    {
                        LogInfo("VLTraderTCPClient: hello handshake OK (protocol v" + goVersion + ")");
                    }
                }
                else
                {
                    LogWarn("VLTraderTCPClient: unknown frame type " + type);
                }
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: malformed frame: " + ex.Message);
            }
        }

        // === Signal handling: OCO bracket placement (spec L4387-4396) ===
        private void HandleSignal(Dictionary<string, object> p)
        {
            if (p == null)
            {
                LogWarn("VLTraderTCPClient: signal payload missing or not an object");
                return;
            }
            string symbol;
            string side;
            int qty;
            double entry;
            double sl;
            double tp;
            int    leg1Qty = 0;
            double leg1Tp  = 0.0;
            string signalId;
            string ts;
            // PHASE 2 armed orders (additive, back-compat): order_type
            // "market" (default) | "limit" + limit_price for resting entries.
            // E7 (entry-mechanics 2026-08-30): "stop_entry" + stop_price for
            // STOP-MARKET entries (breakout-retest fallback).
            string orderType = "market";
            double limitPx = 0.0;
            double stopPx = 0.0;
            try
            {
                symbol   = GetString(p, "symbol");
                side     = GetString(p, "side");                 // "long" | "short"
                qty      = GetInt(p, "quantity");
                entry    = GetDouble(p, "entry");
                sl       = GetDouble(p, "stop_loss");
                tp       = GetDouble(p, "take_profit");
                // REVIEW-353 (wire v4, optional): the split AT ENTRY —
                // leg1_qty > 0 → the AddOn places TWO OCO pairs on the one
                // fill; 0/absent → the single-bracket legacy path (byte-identical).
                leg1Qty = GetInt(p, "leg1_qty");
                leg1Tp  = GetDouble(p, "leg1_tp");
                signalId = GetString(p, "signal_id");
                ts       = GetString(p, "timestamp");
                string ot = GetString(p, "order_type");
                if (!string.IsNullOrEmpty(ot)) orderType = ot;
                if (orderType == "limit")
                {
                    limitPx = GetDouble(p, "limit_price");
                    if (limitPx <= 0)
                    {
                        LogWarn("VLTraderTCPClient: limit signal " + signalId + " missing limit_price — rejecting");
                        SendFillFrame(signalId, 0.0, side, qty, 0.0, "rejected", symbol: symbol);
                        return;
                    }
                }
                // E7 — a "stop_entry" signal places a STOP-MARKET entry at the
                // trigger price (the tick offset was applied Go-side). Bracket
                // on fill is identical to limits (SubmitBracketOnEntryFill).
                if (orderType == "stop_entry")
                {
                    stopPx = GetDouble(p, "stop_price");
                    if (stopPx <= 0)
                    {
                        LogWarn("VLTraderTCPClient: stop_entry signal " + signalId + " missing stop_price — rejecting");
                        SendFillFrame(signalId, 0.0, side, qty, 0.0, "rejected", symbol: symbol);
                        return;
                    }
                }
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: signal payload missing field: " + ex.Message);
                return;
            }

            // A2 (G1) — capture the identity stamp (trader_id + seq) keyed by
            // signal_id so every fill/close/reject for this order echoes it back for
            // Go's echo-verify. Optional fields (pre-v3 Go omits them → empty/0).
            {
                string sigTrader = GetString(p, "trader_id");
                long   sigSeq    = GetLong(p, "seq");
                lock (signalMapLock)
                {
                    signalIdentity[signalId] = new SignalIdentity { TraderId = sigTrader, Seq = sigSeq };
                }
            }

            // PHASE 2 (parse only, back-compat) — optional target account. GetString
            // returns null when the field is absent (TryGetValue, not a throw), so
            // today's Go frames (which omit it) leave this null → it falls through to
            // the active account below, byte-identical to current behavior. This is NOT
            // a new required field; the un-changed Go side keeps working. The actual
            // per-order ROUTING that submits to this account is Phase 3.
            string targetAccount = GetString(p, "account");

            // W-ONE-BUTTON M2 site 7 — the installation is held for an update:
            // refuse the NEW entry. (Go refuses first; this is the AddOn's half.)
            // A signal frame is only ever an entry, so nothing protective is here.
            if (maintenanceHeld)
            {
                LogWarn("VLTraderTCPClient: REFUSING entry " + signalId + " — installation maintenance hold (job "
                        + maintenanceJobId + "); protection, exits and position management continue");
                SendFillFrame(signalId, 0.0, side, qty, 0.0, "rejected", symbol: symbol);
                return;
            }

            // Spec L4414: stale signal rejection at 60s.
            if (DateTime.TryParse(ts, null,
                System.Globalization.DateTimeStyles.RoundtripKind, out var sigTime))
            {
                var ageSec = (DateTime.UtcNow - sigTime.ToUniversalTime()).TotalSeconds;
                if (ageSec > STALE_SIGNAL_AGE_SECONDS)
                {
                    LogWarn("VLTraderTCPClient: stale signal " + signalId
                            + " (age " + ageSec.ToString("F1") + "s) — rejecting");
                    SendFillFrame(signalId, 0.0, side, qty, 0.0, "rejected", symbol: symbol);
                    return;
                }
            }

            if (account == null)
            {
                LogWarn("VLTraderTCPClient: no account — rejecting signal " + signalId);
                SendFillFrame(signalId, 0.0, side, qty, 0.0, "rejected", symbol: symbol);
                return;
            }
            // A3 (G2) — NOTE: the allowlist is NOT populated from the signal payload
            // (that would be vacuous — a signal would authorize its own account). It is
            // declared by Go via account_register (owner-driven) + account_select. The
            // submit guard below enforces membership against that owner-declared set.

            // SAFETY RAIL (Stage-2 Phase-1, defense-in-depth) — the LAST line before an
            // order reaches NT8. NEVER submit on a non-SIM (live/funded) account, and
            // never on a disconnected one. Do NOT fall back to another account — reject.
            // The Go side + account_select already guard; this is the C# half of the
            // double-guard, so a live order is structurally impossible even if an
            // upstream check is bypassed.
            if (!IsSimAccount(account))
            {
                LogWarn("VLTraderTCPClient: REFUSING order " + signalId + " — account '"
                        + account.Name + "' is NOT a SIM account; live/funded accounts are never auto-traded");
                SendFillFrame(signalId, 0.0, side, qty, 0.0, "rejected", symbol: symbol);
                return;
            }
            if (account.Connection == null || account.Connection.Status != ConnectionStatus.Connected)
            {
                LogWarn("VLTraderTCPClient: REFUSING order " + signalId + " — account '"
                        + account.Name + "' is not Connected");
                SendFillFrame(signalId, 0.0, side, qty, 0.0, "rejected", symbol: symbol);
                return;
            }

            // PHASE 2 (resolve + guard, NO routing yet) — if the frame named a target
            // account, resolve it against Account.All (same pattern as account_select)
            // and run the SAME double-guard on it (SIM + connected). This PROVES the
            // parse → resolve → guard path end-to-end and LOGS the resolution. It does
            // NOT change which account submits: the active `account` (already guarded
            // above) still places the order — Phase 3 adds the per-order routing that
            // actually submits to the resolved account. A named-but-bad target (unknown
            // / non-SIM / disconnected) is REJECTED here (reuse the P1 reject); we never
            // silently fall back to the active account when a specific one was requested.
            // An ABSENT/empty field skips this block entirely = today's behavior.
            Account resolved = null;   // PHASE 3: the routed submit target (null = active-account fallback)
            if (!string.IsNullOrEmpty(targetAccount))
            {
                lock (Account.All)
                {
                    foreach (var a in Account.All)
                        if (a.Name == targetAccount) { resolved = a; break; }
                }
                if (resolved == null)
                {
                    LogWarn("VLTraderTCPClient: REFUSING order " + signalId + " — target account '"
                            + targetAccount + "' not found in Account.All");
                    SendFillFrame(signalId, 0.0, side, qty, 0.0, "rejected", symbol: symbol);
                    return;
                }
                if (!IsSimAccount(resolved))
                {
                    LogWarn("VLTraderTCPClient: REFUSING order " + signalId + " — target account '"
                            + targetAccount + "' is NOT a SIM account; live/funded accounts are never auto-traded");
                    SendFillFrame(signalId, 0.0, side, qty, 0.0, "rejected", symbol: symbol);
                    return;
                }
                if (resolved.Connection == null || resolved.Connection.Status != ConnectionStatus.Connected)
                {
                    LogWarn("VLTraderTCPClient: REFUSING order " + signalId + " — target account '"
                            + targetAccount + "' is not Connected");
                    SendFillFrame(signalId, 0.0, side, qty, 0.0, "rejected", symbol: symbol);
                    return;
                }
                // Resolved + guarded — PHASE 3 ROUTES the submit to THIS account (below),
                // not the active one. A named-but-bad target was already rejected above.
                LogInfo("VLTraderTCPClient: signal " + signalId + " routed to account '" + targetAccount
                        + "' (resolved, sim, connected)");
            }

            // Resolve the bare root ("MNQ") to the NT8 front-month
            // contract ("MNQ 06-26") via VLContractResolver (date-derived,
            // auto-rolls). NT8's Instrument.GetInstrument requires the
            // qualified contract; a bare root returns null. Canonical
            // symbol on the wire stays "MNQ"; contract form only exists
            // inside this GetInstrument call.
            string contract = VLContractResolver.ResolveFrontMonthContract(symbol);
            string how;
            var instrument = VLInstrumentLookup.Resolve(symbol, LogWarn, out how);
            LogInfo("VLTraderTCPClient: resolved " + symbol + " -> " + contract + " => "
                    + (instrument != null ? VLInstrumentLookup.ContractName(instrument) : "<null>") + " (" + how + ")");
            if (instrument == null)
            {
                LogWarn("VLTraderTCPClient: instrument " + symbol
                        + " (resolved to " + contract + ") not found — rejecting");
                SendFillFrame(signalId, 0.0, side, qty, 0.0, "rejected", symbol: symbol);
                return;
            }

            // PHASE 3 (per-order ROUTING) — submit to the RESOLVED account when the frame
            // named one (already guarded above), else the active account (back-compat —
            // every order today, since Go sends no account field until Phase 5). FINAL
            // guard on the EXACT submit target (defense-in-depth): re-assert SIM + connected
            // on the account that will receive CreateOrder, no matter how it was chosen —
            // the LIVE account can never be a submit target. (The Go-side allow-list is
            // enforced at placeEntry; there is no config channel into the AddOn.)
            Account submitAccount = resolved ?? account;
            if (!IsSimAccount(submitAccount)
                || submitAccount.Connection == null
                || submitAccount.Connection.Status != ConnectionStatus.Connected)
            {
                LogWarn("VLTraderTCPClient: REFUSING order " + signalId + " — submit target '"
                        + (submitAccount != null ? submitAccount.Name : "<null>")
                        + "' failed the final SIM/connected guard");
                SendFillFrame(signalId, 0.0, side, qty, 0.0, "rejected", symbol: symbol);
                return;
            }
            // A3 (G2) — HARD REFUSE if the resolved submit account is not in this
            // session's bound-trader allowlist (populated above + on account_select).
            // Catches any routing bug or rogue account before CreateOrder; the rejected
            // fill is the error frame back to Go.
            if (!IsSessionAccount(submitAccount.Name))
            {
                LogWarn("VLTraderTCPClient: HARD-REFUSING order " + signalId + " — submit account '"
                        + submitAccount.Name + "' is NOT in this session's bound-trader allowlist (A3/G2)");
                SendFillFrame(signalId, 0.0, side, qty, 0.0, "rejected", symbol: symbol);
                return;
            }
            // The fill→bracket→fill-frame lifecycle (OnOrderUpdate) must fire for this
            // account. Idempotent — a no-op when it is the already-subscribed active
            // account. (Per-account balance/position snapshots + manual-close Flatten
            // routing remain Phase 4.)
            SubscribeOrderUpdate(submitAccount);

            // Track entry + tick size for slippage computation in fill emission.
            double tickSize = 0.25; // sensible NQ default; overridden if instrument exposes it
            try { tickSize = instrument.MasterInstrument.TickSize; } catch { }
            lock (signalMapLock)
            {
                signalEntryByOco[signalId] = entry;
                signalTickSizeByOco[signalId] = tickSize;
            }

            // FIX-DOUBLE-ENTRY (far-side half of the Go attempted-entry guard,
            // defence in depth — the Go side must be correct WITHOUT this):
            // a SECOND entry frame carrying a signal_id already seen here is a
            // replayed frame (Go re-queued an `attempted` entry on a mid-flush
            // write failure and re-sent it after reconnect). The first entry may
            // already be working or FILLED at NT8 — a resubmit would double the
            // position. It is NOT submitted; it is answered with a fill frame
            // status "duplicate_ignored", which Go treats as already-handled —
            // never as a reject that re-arms. The seen set is NOT cleared on
            // fill (a post-fill duplicate must be ignored too) and is bounded by
            // a TTL + size cap.
            lock (signalMapLock)
            {
                if (seenSignals.TryGetValue(signalId, out DateTime seenAt))
                {
                    if ((DateTime.UtcNow - seenAt).TotalMinutes < SEEN_SIGNAL_TTL_MINUTES)
                    {
                        LogWarn("VLTraderTCPClient: duplicate entry " + signalId + " ignored (seen-signal dedupe)");
                        SendFillFrame(signalId, 0.0, side, qty, 0.0, "duplicate_ignored", symbol: symbol);
                        return;
                    }
                }
                seenSignals[signalId] = DateTime.UtcNow;
                if (seenSignals.Count > SEEN_SIGNAL_CAP)
                {
                    // P3 (DS-101 adversarial review, 2026-09-26): a HARD cap —
                    // evict oldest-first, TTL or not. A soft cap that removes
                    // only expired entries is bounded by TTL x rate, not by
                    // SEEN_SIGNAL_CAP.
                    var oldest = seenSignals.OrderBy(kv => kv.Value)
                                            .Take(seenSignals.Count - SEEN_SIGNAL_CAP)
                                            .Select(kv => kv.Key).ToList();
                    foreach (var k in oldest) seenSignals.Remove(k);
                    LogWarn("VLTraderTCPClient: seen-signal set hit its hard cap " + SEEN_SIGNAL_CAP +
                            " — evicted " + oldest.Count + " oldest entries (oldest-first)");
                }
            }

            // Submit the ENTRY only; the protective SL/TP are placed once the
            // entry fills (OnOrderUpdate → SubmitBracketOnEntryFill). Submitting
            // all three in one OCO group cancels the SL/TP the instant the
            // Market entry fills (OCO = one-cancels-other) — the position then
            // has no working stop/target in NT8 and never auto-closes. The
            // entry stands alone (empty OCO); SL+TP get their own OCO pair.
            try
            {
                // WAVE B / class 77 (2026-09-05) — FOLD THE CASE. These two
                // ternaries were ORDINAL: "LONG" is not "long", so an uppercase
                // side fell to the else branch and a LONG entry was submitted as
                // a live SellShort. The Go ledger canonicalizes side to
                // UPPERCASE at the write (store/armed_orders.go:181, 2026-09-03)
                // and the armed placement path handed that value straight to the
                // wire. Go now folds before it sends; this folds again on
                // arrival, because a ternary whose UNKNOWN branch opens a
                // position in the opposite direction must not be the last line
                // of defence. The same file already does this at the
                // close-position handler (string.Equals + OrdinalIgnoreCase).
                bool isLongSide = string.Equals(side, "long", StringComparison.OrdinalIgnoreCase);
                var entryAction = isLongSide ? OrderAction.Buy : OrderAction.SellShort;
                var exitAction  = isLongSide ? OrderAction.Sell : OrderAction.BuyToCover;

                // PHASE 2 armed orders — a "limit" signal places a RESTING limit
                // entry (fills fire OnOrderUpdate exactly like market entries;
                // the SL/TP bracket still defers to SubmitBracketOnEntryFill).
                // E7 — a "stop_entry" signal places a STOP-MARKET entry at the
                // trigger price (the tick offset was applied Go-side).
                bool isLimit    = orderType == "limit" && limitPx > 0;
                bool isStopEntry = orderType == "stop_entry" && stopPx > 0;
                // MENTOR STOP-LIMIT (PR B, 2026-10-03): stop_limit=true builds
                // OrderType.StopLimit instead of StopMarket — fills at its
                // price or misses, never a stop-MARKET (D1.4 p1 @24:41,
                // p2 @00:00). Go sets the flag only when its mentor knob is ON
                // and the far side proved the floor.
                bool stopLimitWanted = string.Equals(GetString(p, "stop_limit"), "true", StringComparison.OrdinalIgnoreCase);
                OrderType orderT = isLimit ? OrderType.Limit
                    : (isStopEntry ? (stopLimitWanted ? OrderType.StopLimit : OrderType.StopMarket) : OrderType.Market);
                // WAVE B / D1 (2026-09-05) — ONE ORDER, CORRECT SLOTS.
                // Account.CreateOrder is POSITIONAL: after `quantity` come
                // (limitPrice, stopPrice, oco, name, gtd, customOrder). Until
                // today this call computed a single `orderPx` and passed it into
                // the limitPrice slot for BOTH kinds, with a literal 0 in
                // stopPrice — so every StopMarket entry reached NT8 as
                // `Limit price=<trigger> Stop price=0`. A stop with a ZERO
                // trigger is not rejected and not worked; it rests inert forever
                // (22 of 22 lifetime submissions, 0 fills, 2026-08-31 + 09-04).
                // The correct shape was always in this file: the bracket stop
                // loss below builds a StopMarket as (b.Qty, 0, b.Sl) and fills.
                // A limit entry keeps the shape it has today: price in
                // limitPrice, 0 in stopPrice.
                // N12 (PR B, 2026-10-03): LimitPrice == StopPrice leaves a
                // RESTING limit when the market gaps through the trigger, and
                // it can fill later at a stale price. The Go side closes that
                // window with a per-order EXPIRY (expiry_ms, authored by the
                // evaluator's intent; the armed pass cancels an unfilled order
                // when it lapses). The order itself keeps the mentor's exact
                // shape — limit == stop (D1.4 p1 @24:41).
                double limitArg = isLimit ? limitPx
                    : (isStopEntry && stopLimitWanted ? stopPx : 0);
                double stopArg  = isStopEntry ? stopPx : 0;
                var entryOrder = submitAccount.CreateOrder(
                    instrument, entryAction, orderT, OrderEntry.Manual,
                    TimeInForce.Day, qty, limitArg, stopArg, string.Empty, signalId,
                    Core.Globals.MaxDate, null);

                lock (signalMapLock)
                {
                    pendingBrackets[signalId] = new PendingBracket
                    {
                        Instrument = instrument,
                        ExitAction = exitAction,
                        Qty        = qty,
                        Sl         = sl,
                        Tp         = tp,
                        Account    = submitAccount,   // PHASE 3: the bracket follows the entry's account
                        Leg1Qty    = leg1Qty,         // REVIEW-353 split
                        Leg1Tp     = leg1Tp,
                    };
                    // PHASE 2 armed orders — resting limit entries are cancelable.
                    if (isLimit) { workingEntries[signalId] = entryOrder; }
                    // E7 — stop-market entries are cancelable the same way.
                    if (isStopEntry) { workingEntries[signalId] = entryOrder; }
                }

                submitAccount.Submit(new[] { entryOrder });
                // A9 — LOG WHAT WAS SENT, NOT WHAT WAS PARSED. The old line printed
                // "stop@29590.5" (the parsed variable) on all 21 malformed
                // submissions while handing NT8 a zero stop slot, which is why a
                // slot bug survived in a file that logs every placement. These
                // four values are the arguments actually given to CreateOrder, so
                // the AddOn log and the NT8 order log must now agree or disagree
                // visibly.
                LogInfo("VLTraderTCPClient: submitted entry signal_id=" + signalId
                        + " on account=" + submitAccount.Name
                        + " " + side + " " + qty + " " + symbol
                        + " action=" + entryAction + " type=" + orderT
                        + " limitPrice=" + limitArg + " stopPrice=" + stopArg
                        // A MARKET entry has 0 in BOTH price slots by
                        // construction, so the four argument fields alone would
                        // name no price at all on the path that carries almost
                        // every live entry. Keep the reference entry for that
                        // case only — it is not an argument to CreateOrder and
                        // must not read as one.
                        + (isLimit || isStopEntry ? "" : " entry~=" + entry)
                        + " (SL=" + sl + " TP=" + tp + " on fill)");
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: order submit failed: " + ex.Message);
                lock (signalMapLock) { pendingBrackets.Remove(signalId); }
                SendFillFrame(signalId, 0.0, side, qty, 0.0, "rejected", symbol: symbol);
            }
        }

        // === Manual close: flatten the position + cancel its working orders ===
        // account.Flatten closes the position at market AND cancels the bracket's
        // SL/TP, so no orphaned exit orders are left to re-open a position. The
        // flatten's market fill arrives in OnOrderUpdate as an exit (Sell /
        // BuyToCover) and is reported as a position_close with reason "manual".

        // CancelBracketsFor (4.3 + 2026-08-25 orphan fix): cancel the still-live
        // SL/TP legs tracked for a signalId — used when the limit exit fills so
        // the protective bracket can never re-enter the now-flat position.
        // Best-effort; OCO fills already cancel their own sibling.
        //
        // 2026-08-25 fix: the close_position frame carries a FRESH signal id (Go
        // generates a new uuid per close), so the -lx leg id never matches the
        // ENTRY's bracket key — the bracket survived and left 1 limit + 1 stop
        // resting after the position closed (and could re-enter a flat account).
        // Fall back to the tracked bracket for the SAME (account, instrument
        // root) so the protective legs are always cancelled on a bot exit.
        private void CancelBracketsFor(string signalId, string rootSymbol, string acctName)
        {
            if (string.IsNullOrEmpty(signalId)) return;
            PlacedBracket pb = null;
            List<string> keysToRemove = new List<string>();
            lock (signalMapLock)
            {
                placedBrackets.TryGetValue(signalId, out pb);
                if (pb == null)
                {
                    // Fresh close id → find the entry bracket by (account, instrument).
                    foreach (var kv in placedBrackets)
                    {
                        var cand = kv.Value;
                        if (cand == null) continue;
                        string candRoot = "";
                        string candAcct = "";
                        try { candRoot = cand.Instrument.MasterInstrument.Name; } catch { }
                        try { candAcct = cand.Account != null ? cand.Account.Name : ""; } catch { }
                        if (string.Equals(candRoot, rootSymbol, StringComparison.OrdinalIgnoreCase)
                            && (string.IsNullOrEmpty(acctName) || string.Equals(candAcct, acctName, StringComparison.OrdinalIgnoreCase)))
                        {
                            pb = cand;
                            keysToRemove.Add(kv.Key);
                        }
                    }
                }
                else
                {
                    keysToRemove.Add(signalId);
                }
                if (pb != null)
                {
                    Account ba = pb.Account ?? account;
                    var toCancel = new List<Order>();
                    if (pb.SlOrder != null) toCancel.Add(pb.SlOrder);
                    if (pb.TpOrder != null) toCancel.Add(pb.TpOrder);
                    // REVIEW-353: cancel leg 2's pair too — EVERY bracket of the signal.
                    if (pb.SlOrder2 != null) toCancel.Add(pb.SlOrder2);
                    if (pb.TpOrder2 != null) toCancel.Add(pb.TpOrder2);
                    if (toCancel.Count > 0 && ba != null)
                    {
                        try
                        {
                            ba.Cancel(toCancel);
                            LogInfo("VLTraderTCPClient: bracket legs cancelled after exit (signal_id=" + signalId + ")");
                        }
                        catch (Exception ex)
                        {
                            LogWarn("VLTraderTCPClient: bracket cancel after exit failed: " + ex.Message);
                        }
                    }
                    foreach (var k in keysToRemove)
                    {
                        placedBrackets.Remove(k);
                    }
                }
            }
        }
        private void HandleClosePosition(Dictionary<string, object> p)
        {
            if (p == null) { LogWarn("VLTraderTCPClient: close_position empty payload"); return; }
            if (account == null) { LogWarn("VLTraderTCPClient: close_position no account"); return; }
            try
            {
                string symbol = GetString(p, "symbol");
                string contract = VLContractResolver.ResolveFrontMonthContract(symbol);
                string how;
                var instrument = VLInstrumentLookup.Resolve(symbol, LogWarn, out how);
                if (instrument == null)
                {
                    LogWarn("VLTraderTCPClient: close_position instrument not found " + symbol + " (" + contract + ", " + how + ")");
                    return;
                }
                // PHASE 4 — resolve the account that actually HOLDS this symbol's open
                // position (recorded on entry-fill), and flatten THAT account, not the
                // active one. Back-compat: one trader → the map misses (or returns Sim101)
                // → byte-identical today. GUARD: never flatten a non-SIM (LIVE) account —
                // the resolved target must pass IsSimAccount or the close is refused.
                Account closeTarget = null;
                // Look up by the SAME normalized root the entry-fill populate used
                // (MasterInstrument.Name), not the raw wire symbol — so a non-canonical
                // close symbol can't miss the map and silently fall back to the active
                // account. Defaults to the raw symbol if MasterInstrument is unavailable.
                string posKey = symbol;
                try { posKey = instrument.MasterInstrument.Name; } catch { }
                lock (posAcctLock) { positionAccountBySymbol.TryGetValue(posKey, out closeTarget); }
                if (closeTarget == null) closeTarget = account;
                if (closeTarget == null || !IsSimAccount(closeTarget))
                {
                    LogWarn("VLTraderTCPClient: REFUSING close " + symbol + " — resolved account '"
                            + (closeTarget != null ? closeTarget.Name : "<null>")
                            + "' is not a SIM account; position left OPEN");
                    return;
                }
                // 4.3 limit-then-market: when limit_price > 0, submit a LIMIT exit
                // (favorable side) instead of an immediate market flatten. Go sends the
                // market fallback as a later close_position frame WITHOUT limit_price,
                // whose Flatten cancels this resting limit. The -lx name routes its fill
                // to position_close with reason "limit" and cancels the bracket legs.
                double limitPrice = GetDouble(p, "limit_price");
                if (limitPrice > 0)
                {
                    try
                    {
                        string sideStr = GetString(p, "side");
                        string signalId = GetString(p, "signal_id");
                        int closeQty = GetInt(p, "quantity");
                        if (closeQty <= 0) closeQty = 1;
                        OrderAction exitAction = string.Equals(sideStr, "long", StringComparison.OrdinalIgnoreCase)
                            ? OrderAction.Sell : OrderAction.BuyToCover;
                        var limitOrder = closeTarget.CreateOrder(
                            instrument, exitAction, OrderType.Limit, OrderEntry.Manual,
                            TimeInForce.Day, closeQty, limitPrice, 0, string.Empty, signalId + "-lx",
                            Core.Globals.MaxDate, null);
                        closeTarget.Submit(new[] { limitOrder });
                        LogInfo("VLTraderTCPClient: limit exit submitted " + symbol + " " + sideStr
                                + " qty=" + closeQty + " @ " + limitPrice + " on account=" + closeTarget.Name
                                + " — Go schedules the market fallback (not yet closed)");
                        return;
                    }
                    catch (Exception ex)
                    {
                        LogWarn("VLTraderTCPClient: limit exit submit failed (" + ex.Message
                                + ") — falling back to market flatten");
                    }
                }
                closeTarget.Flatten(new[] { instrument });
                // Flatten is async fire-and-forget: the actual fill (or a REJECT, e.g. "no
                // market data" with the feed down) arrives later in OnOrderUpdate. This log
                // means SUBMITTED, not closed — the close is confirmed only by the
                // position_close fill frame (or reported failed by position_close_rejected).
                LogInfo("VLTraderTCPClient: flatten submitted " + symbol + " (" + contract
                        + ") on account=" + closeTarget.Name + " — awaiting NT8 fill/reject (not yet closed)");
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: close_position failed: " + ex.Message);
            }
        }

        // === Account switching (Go-server → C#-AddOn command) ===
        // Unsubscribe from the old account's events, resolve the new account by name,
        // subscribe to its events, clear pending state tied to the old account,
        // and re-emit account_balance for the new account.
        private void HandleAccountSelect(Dictionary<string, object> p)
        {
            if (p == null) { LogWarn("VLTraderTCPClient: account_select empty payload"); return; }
            try
            {
                string requestedAccount = GetString(p, "account");
                if (string.IsNullOrEmpty(requestedAccount))
                {
                    LogWarn("VLTraderTCPClient: account_select missing account name");
                    return;
                }

                // Resolve the requested account from Account.All
                Account newAccount = null;
                lock (Account.All)
                {
                    foreach (var a in Account.All)
                        if (a.Name == requestedAccount) { newAccount = a; break; }
                }

                if (newAccount == null)
                {
                    LogWarn("VLTraderTCPClient: account_select account not found: " + requestedAccount);
                    return;
                }

                // If already on that account, no-op
                if (account == newAccount)
                {
                    LogInfo("VLTraderTCPClient: account_select already on " + requestedAccount);
                    return;
                }

                // Unsubscribe from the old account's events. PositionUpdate is NOT
                // touched here — it is subscribed for ALL SIM accounts globally
                // (SubscribePositionUpdatesAllSim), so switching the active account must
                // not unsubscribe/re-subscribe it (that would desync positionSubscribed
                // and double-hook the new account).
                if (account != null)
                {
                    UnsubscribeOrderUpdate(account);   // PHASE 3: dedup'd
                    try { account.AccountItemUpdate -= OnAccountItemUpdate; } catch { }
                }

                // Switch to the new account and subscribe to its (active-only) events.
                account = newAccount;
                RegisterSessionAccount(account.Name); // A3 (G2) — an owner-selected account is a bound-trader account
                SubscribeOrderUpdate(account);   // PHASE 3: dedup'd
                account.AccountItemUpdate += OnAccountItemUpdate;

                // G7 decouple: do NOT global-Clear the signal/bracket maps on an
                // account switch. Those maps are keyed by unique signal_id/OCO and
                // span ALL accounts, so a global Clear here dropped the OTHER trader's
                // in-flight bracket/entry correlation (a cross-account side effect).
                // Entries are removed on their own fill/close lifecycle, so leaving a
                // switched-away account's entries in place is harmless and correct.
                // (The Go side no longer sends account_select at all — G7 — so this
                // path is unreachable in normal operation; the scoping is defensive.)

                // Re-emit account_balance immediately for the new account
                SendAccountBalance();
                // Open-position read-back — snapshot the NEWLY-SELECTED account's
                // current open positions so the AI re-tracks the real position on
                // switch-back (fixes the orphaned-position bug). NT8 reports
                // positions on change only, so this on-select re-report is what
                // repopulates the Go-side per-account cache.
                SendOpenPositions(account);
                LogInfo("VLTraderTCPClient: switched to account " + account.Name);
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: account_select failed: " + ex.Message);
            }
        }

        // === Fill subscription (spec L4398-4406) ===
        private void OnOrderUpdate(object sender, OrderEventArgs e)
        {
            // PHASE 2 armed orders — emit EVERY state change (deduped) so Go can
            // track working/cancelled resting entries + bracket legs. The fill /
            // position_close terminal frames below stay the entry/exit path.
            SendOrderUpdateFrame(e);

            // Only act on filled/rejected states; ignore accepted/working.
            if (e.OrderState != OrderState.Filled
                && e.OrderState != OrderState.Rejected
                && e.OrderState != OrderState.PartFilled)
            {
                return;
            }
            if (e.Order == null) return;

            // Classify by ORDER ACTION, not name: Buy/SellShort = entry,
            // Sell/BuyToCover = exit (an SL/TP leg OR a manual flatten — a
            // flatten order may have no -sl/-tp name, so name alone is not
            // enough). The order Name still carries the -sl/-tp suffix when
            // present → the exit reason; otherwise the exit is a "manual" close.
            string orderName = e.Order.Name ?? "";
            string ocoId     = e.Order.Oco ?? "";
            string signalId  = orderName.Length > 0 ? orderName : ocoId;
            string exitReason = null;
            // REVIEW-353: leg 2's pair is named -sl2/-tp2 but reports the SAME
            // entry signal id (its residual qty drives ApplyNT8Exit's partial path).
            if (signalId.EndsWith("-sl2")) { exitReason = "sl"; signalId = signalId.Substring(0, signalId.Length - 4); }
            else if (signalId.EndsWith("-tp2")) { exitReason = "tp"; signalId = signalId.Substring(0, signalId.Length - 4); }
            else if (signalId.EndsWith("-sl")) { exitReason = "sl"; signalId = signalId.Substring(0, signalId.Length - 3); }
            else if (signalId.EndsWith("-tp")) { exitReason = "tp"; signalId = signalId.Substring(0, signalId.Length - 3); }
            else if (signalId.EndsWith("-lx"))
            {
                // 4.3 limit-then-market exit fill — cancel the still-live bracket
                // legs so they can never re-enter the now-flat position. The
                // close id is FRESH (Go uuid per close), so fall back to the
                // (account, instrument) bracket lookup.
                exitReason = "limit";
                signalId = signalId.Substring(0, signalId.Length - 3);
                // 2026-09-07 — ONLY ON A REAL FILL.
                //
                // The state gate above admits Filled, Rejected AND PartFilled,
                // and this branch had no state check at all. A limit exit the
                // SIM REJECTS — and this file documents that exact rejection,
                // "There is no market data available to drive the simulation
                // engine" — cancelled the position's stop and target while the
                // position was still OPEN. The 2026-09-06 naked position,
                // reached from the exit side instead of the cancel side.
                //
                // The justification for cancelling here is "the position is now
                // flat". A rejected exit means it is not, and a PART fill means
                // it is not YET: the remaining quantity still needs its stop.
                if (e.OrderState == OrderState.Filled)
                {
                    string lxRoot = "";
                    string lxAcct = "";
                    try { lxRoot = e.Order.Instrument.MasterInstrument.Name; } catch { }
                    try { lxAcct = e.Order.Account != null ? e.Order.Account.Name : ""; } catch { }
                    CancelBracketsFor(signalId, lxRoot, lxAcct);
                }
                else
                {
                    LogWarn("VLTraderTCPClient: limit exit " + signalId + "-lx is " + e.OrderState
                            + ", NOT Filled — the position is still open, so its bracket is LEFT ALONE"
                            + " (protective orders are not cancelled by an exit that did not happen)");
                }
            }

            var action = e.Order.OrderAction;
            bool isExit = (action == OrderAction.Sell || action == OrderAction.BuyToCover);

            // position_close with the real exit price (reason from the leg name,
            // else "manual"). Only on a full Filled — PartFilled is transient (NT
            // sends a final Filled; per-partial would duplicate the close) and a
            // rejection is an error. Held side is opposite the exit action.
            if (isExit)
            {
                if (e.OrderState == OrderState.Filled)
                {
                    string positionSide = (action == OrderAction.BuyToCover) ? "short" : "long";
                    string rootSymbol = "";
                    try { rootSymbol = e.Order.Instrument.MasterInstrument.Name; } catch { }
                    string exitAcct = e.Order.Account != null ? e.Order.Account.Name
                                      : (account != null ? account.Name : "");
                    // P1-2: name which leg exited so the receipt identity stays distinct.
                    // A single bracket's -sl/-tp is the WHOLE position (leg 0); only a
                    // split bracket's -sl/-tp is leg 1.
                    int closeLeg = 0;
                    if (orderName.EndsWith("-sl2") || orderName.EndsWith("-tp2"))
                    {
                        closeLeg = 2;
                    }
                    else if (orderName.EndsWith("-sl") || orderName.EndsWith("-tp"))
                    {
                        PlacedBracket cb;
                        bool wasSplit = false;
                        lock (signalMapLock)
                        {
                            if (placedBrackets.TryGetValue(signalId, out cb) && cb != null)
                                wasSplit = cb.Leg1Qty > 0;
                        }
                        closeLeg = wasSplit ? 1 : 0;
                    }
                    SendPositionCloseFrame(signalId, rootSymbol, positionSide,
                                           e.AverageFillPrice, e.Filled, exitReason ?? "manual", exitAcct, closeLeg);
                    // P1-1: clear ONLY the filled leg's references; drop the record
                    // and the A2 identity only when NO live leg remains. A leg-1 TP
                    // fill must not untrack the runner (leg 2) — its BE/trail
                    // would silently vanish.
                    lock (signalMapLock)
                    {
                        PlacedBracket pb;
                        if (placedBrackets.TryGetValue(signalId, out pb) && pb != null)
                        {
                            bool isLeg2 = orderName.EndsWith("-sl2") || orderName.EndsWith("-tp2");
                            bool isLeg1 = orderName.EndsWith("-sl") || orderName.EndsWith("-tp");
                            if (isLeg2) { pb.SlOrder2 = null; pb.TpOrder2 = null; pb.Leg2Qty = 0; }
                            else if (isLeg1)
                            {
                                // (a) NEVER zero Leg1Qty — AmendBracketQuantity's split
                                // path gates on it. Record the exit qty + the last resting
                                // stop so a later entry fill can still protect under leg 2.
                                pb.Leg1Exited = e.Filled;
                                if (pb.SlOrder != null) pb.LastStop = pb.SlOrder.StopPrice;
                                pb.SlOrder = null; pb.TpOrder = null;
                            }
                            else { pb.SlOrder = null; pb.TpOrder = null; pb.SlOrder2 = null; pb.TpOrder2 = null; } // manual/flatten: whole position

                            bool live1 = pb.SlOrder != null || pb.TpOrder != null;
                            bool live2 = pb.SlOrder2 != null || pb.TpOrder2 != null;
                            // (c) remove only when no leg is live AND the entry is no longer
                            // working — a still-working entry can fill again and must find
                            // its bracket record (AmendBracketQuantity).
                            bool entryWorking = workingEntries.ContainsKey(signalId);
                            if (!live1 && !live2 && !entryWorking)
                            {
                                placedBrackets.Remove(signalId);
                                signalIdentity.Remove(signalId);
                            }
                        }
                        else
                        {
                            signalIdentity.Remove(signalId);
                        }
                    }
                }
                else if (e.OrderState == OrderState.Rejected)
                {
                    // The SIM/broker REJECTED the exit/flatten (e.g. "There is no
                    // market data available to drive the simulation engine" while the
                    // data feed is down). The close did NOT take — the position is
                    // STILL OPEN in NT8. Tell Go so it never records a phantom close
                    // and raises an alarm (the id=45→id=46 net-2 root cause).
                    string positionSide = (action == OrderAction.BuyToCover) ? "short" : "long";
                    string rootSymbol = "";
                    try { rootSymbol = e.Order.Instrument.MasterInstrument.Name; } catch { }
                    string reason = "";
                    try { reason = e.Comment ?? ""; } catch { }
                    if (string.IsNullOrEmpty(reason)) { try { reason = e.Error.ToString(); } catch { } }
                    string rejAcct = e.Order.Account != null ? e.Order.Account.Name
                                     : (account != null ? account.Name : "");
                    SendPositionCloseRejectedFrame(signalId, rootSymbol, positionSide, reason, rejAcct);
                    LogWarn("VLTraderTCPClient: exit/flatten REJECTED signal_id=" + signalId
                            + " " + positionSide + " reason=" + reason
                            + " — position STILL OPEN (not closed)");
                }
                return;
            }

            // Entry leg (Buy = long, SellShort = short) → fill-frame path.
            string status;
            switch (e.OrderState)
            {
                case OrderState.Filled:     status = "filled";   break;
                case OrderState.Rejected:   status = "rejected"; break;
                case OrderState.PartFilled: status = "partial";  break;
                default:                    return;
            }

            // Entry just filled → place the protective SL/TP now that the
            // position exists (deferred from HandleSignal to dodge the OCO
            // cancel-on-entry-fill bug). On rejection, drop the pending bracket.
            // D2 (2026-09-07) — a PART fill is protected too. The bracket is
            // placed for whatever has filled so far and amended as more arrives;
            // waiting for OrderState.Filled left a partially filled position
            // naked for the width of the remaining fill.
            if (e.OrderState == OrderState.Filled || e.OrderState == OrderState.PartFilled)
            {
                SubmitBracketOnEntryFill(signalId, e.Filled, e.AverageFillPrice);
            }
            if (e.OrderState == OrderState.Filled)
            {
                lock (signalMapLock) { workingEntries.Remove(signalId); } // PHASE 2: resting limit consumed
                // PHASE 4: record which account now holds this symbol's open position so a
                // later close flattens the RIGHT account (not the active one). Sourced from
                // the order's own account — account-agnostic, correct per routed account.
                try
                {
                    string entrySym = e.Order.Instrument.MasterInstrument.Name;
                    if (e.Order.Account != null && !string.IsNullOrEmpty(entrySym))
                        lock (posAcctLock) { positionAccountBySymbol[entrySym] = e.Order.Account; }
                }
                catch (Exception ex)
                {
                    LogWarn("VLTraderTCPClient: PHASE 4 — failed to record position account for "
                            + signalId + ": " + ex.Message + " (a later close falls back to the active account)");
                }
            }
            else if (e.OrderState == OrderState.Rejected)
            {
                lock (signalMapLock) { pendingBrackets.Remove(signalId); workingEntries.Remove(signalId); } // PHASE 2: resting limit rejected → gone
            }

            string sideStr = (action == OrderAction.Buy) ? "long" : "short";

            double slippageTicks = 0.0;
            lock (signalMapLock)
            {
                if (signalEntryByOco.TryGetValue(signalId, out var origEntry)
                    && signalTickSizeByOco.TryGetValue(signalId, out var tick)
                    && tick > 0)
                {
                    slippageTicks = (e.AverageFillPrice - origEntry) / tick;
                }
            }

            // P5.2 — tag the fill with the order's instrument root for
            // multi-symbol attribution (same accessor PHASE 4 uses above).
            string fillSymbol = "";
            try { fillSymbol = e.Order.Instrument.MasterInstrument.Name; } catch { }

            SendFillFrame(signalId, e.AverageFillPrice, sideStr, e.Filled,
                          slippageTicks, status,
                          e.Order.Account != null ? e.Order.Account.Name
                              : (account != null ? account.Name : ""),
                          fillSymbol);
        }

        // === Write helpers ===

        /// <summary>
        /// P5.2 handshake — sent as the FIRST frame on every (re)connect so the
        /// Go server can version-check before any data flows. The server replies
        /// with its own hello (handled in HandleFrame) or refuses on mismatch.
        /// </summary>
        private void SendHello()
        {
            var hello = new Dictionary<string, object>
            {
                ["protocol_version"] = PROTOCOL_VERSION,
                ["source"]           = "vltrader-addon",
                // F12: the running DLL identifies itself on the FIRST frame, so
                // the Go boot line can answer "which build is NT8 running"
                // without waiting for a heartbeat or a snapshot.
                ["build_id"]         = VL_BUILD_ID
            };
            // CTO ruling Q3 — this activation's epoch. Best-effort: a value that
            // cannot be read is left OUT (absent = unknown), never guessed.
            int pid = Nt8Pid();
            if (pid > 0) hello["nt8_pid"] = pid;
            long startMs = Nt8StartMs();
            if (startMs > 0) hello["nt8_start_ms"] = startMs;
            string mvid = AssemblyMvid();
            if (!string.IsNullOrEmpty(mvid)) hello["assembly_mvid"] = mvid;
            if (sourceHashCache == null) sourceHashCache = ComputeSourceHash();
            if (sourceHashCache.Length > 0) hello["source_hash"] = sourceHashCache;
            hello["activation_nonce"] = activationNonce;
            WriteEnvelope("hello", hello);
        }

        // === W-ONE-BUTTON M2 site 7 — installation maintenance hold ===
        // Go sends `maintenance {held, job_id}` only while the installation is held
        // for an update (plus one held:false release). Every maintenance frame is
        // answered with a maintenance_ack carrying a census of what this NT8 can
        // see: flags and counts only, NEVER names (the repo is public and Go logs
        // the ack). Go's installation gate fails on any connected non-SIM
        // connection, any position or working order on ANY account, or a census
        // this AddOn could not take (census_error / an absent list).
        private void HandleMaintenance(Dictionary<string, object> p)
        {
            bool held = true; // fail-closed: a notice we cannot read holds
            object hv;
            if (p != null && p.TryGetValue("held", out hv) && hv is bool)
                held = (bool)hv;
            else
                LogWarn("VLTraderTCPClient: maintenance frame without a boolean 'held' — treating it as HELD (fail-closed)");
            string job = GetString(p, "job_id") ?? "";
            bool changed = held != maintenanceHeld || job != maintenanceJobId;
            maintenanceHeld = held;
            maintenanceJobId = job;
            if (changed)
            {
                if (held)
                    LogWarn("VLTraderTCPClient: MAINTENANCE HOLD (job " + job + ") — NEW entries are refused; protection, exits and position management continue");
                else
                    LogInfo("VLTraderTCPClient: maintenance hold RELEASED — entries are accepted again");
            }
            WriteEnvelope("maintenance_ack", BuildMaintenanceAck(held, job));
        }

        private Dictionary<string, object> BuildMaintenanceAck(bool held, string job)
        {
            var payload = new Dictionary<string, object>
            {
                ["held"]            = held,
                ["job_id"]          = job,
                // Frames execute synchronously on the read thread: when this ack is
                // written, every frame received before it has already run. There is
                // no command queue, so the depth is 0 by construction. A future
                // asynchronous dispatcher MUST report its real depth here.
                ["queued_commands"] = 0,
                ["build_id"]        = VL_BUILD_ID
            };
            try
            {
                var accounts = new List<object>();
                // connection -> "every account seen on it is SIM"
                var connAllSim = new Dictionary<Connection, bool>();
                // M2.1: snapshot each NT8 collection under ITS OWN lock and count
                // outside it — never one lock while holding another (this runs on
                // the read thread that also carries close / protective frames).
                var accts = new List<Account>();
                lock (Account.All)
                {
                    foreach (Account a in Account.All)
                        if (a != null) accts.Add(a);
                }
                foreach (Account a in accts)
                {
                    bool sim = IsSimAccount(a);
                    var positionsSnap = new List<Position>();
                    lock (a.Positions)
                    {
                        foreach (Position pos in a.Positions)
                            positionsSnap.Add(pos);
                    }
                    var ordersSnap = new List<Order>();
                    lock (a.Orders)
                    {
                        foreach (Order o in a.Orders)
                            ordersSnap.Add(o);
                    }
                    int positions = 0, working = 0;
                    foreach (Position pos in positionsSnap)
                        if (pos != null && pos.MarketPosition != MarketPosition.Flat && pos.Quantity != 0) positions++;
                    // ANY non-terminal order of ANY action: entries, exits and
                    // protection alike (a resting exit can reverse a flat account).
                    foreach (Order o in ordersSnap)
                        if (o != null && !IsTerminalOrderState(o.OrderState)) working++;
                    accounts.Add(new Dictionary<string, object>
                    {
                        ["sim"] = sim, ["positions"] = positions, ["working"] = working
                    });
                    Connection c = a.Connection;
                    if (c != null)
                    {
                        bool prev;
                        connAllSim[c] = (connAllSim.TryGetValue(c, out prev) ? prev : true) && sim;
                    }
                }
                var connsSnap = new List<Connection>();
                lock (Connection.Connections)
                {
                    foreach (Connection c in Connection.Connections)
                        if (c != null) connsSnap.Add(c);
                }
                var connections = new List<object>();
                foreach (Connection c in connsSnap)
                {
                    // A connection with no account seen on it is NOT known to be
                    // SIM, so it reads as non-SIM (fail-closed).
                    bool allSim;
                    bool sim = connAllSim.TryGetValue(c, out allSim) && allSim;
                    // M2.1: settled = Connected or Disconnected. Any other state
                    // (Connecting, ConnectionLost …) cannot vouch for its accounts.
                    bool settled = c.Status == ConnectionStatus.Connected || c.Status == ConnectionStatus.Disconnected;
                    connections.Add(new Dictionary<string, object>
                    {
                        ["sim"] = sim, ["connected"] = c.Status == ConnectionStatus.Connected, ["settled"] = settled
                    });
                }
                payload["connections"] = connections;
                payload["accounts"]    = accounts;
            }
            catch (Exception ex)
            {
                // The type name only: an exception message could carry an account name.
                payload["census_error"] = "census failed: " + ex.GetType().Name;
                payload.Remove("connections");
                payload.Remove("accounts");
            }
            return payload;
        }

        // Filled / Cancelled / Rejected are history; every other state (Unknown
        // included) counts as working — the census must never under-report.
        private static bool IsTerminalOrderState(OrderState st)
        {
            return st == OrderState.Filled || st == OrderState.Cancelled || st == OrderState.Rejected;
        }

        private static int Nt8Pid()
        {
            try { return System.Diagnostics.Process.GetCurrentProcess().Id; } catch { return 0; }
        }

        private static long Nt8StartMs()
        {
            try
            {
                DateTime st = System.Diagnostics.Process.GetCurrentProcess().StartTime.ToUniversalTime();
                return (long)(st - new DateTime(1970, 1, 1, 0, 0, 0, DateTimeKind.Utc)).TotalMilliseconds;
            }
            catch { return 0; }
        }

        // The loaded assembly's module version id: NT8 compiles every NinjaScript
        // into one assembly, and the MVID changes on every compile.
        private static string AssemblyMvid()
        {
            try { return typeof(VLTraderTCPClient).Assembly.ManifestModule.ModuleVersionId.ToString("N"); }
            catch { return ""; }
        }

        // SHA-256 of this AddOn's source file as it stood on disk when this
        // activation started ("" when it cannot be read). Content-derived, unlike
        // VL_BUILD_ID; read beside assembly_mvid, which is the compile identity.
        private static string ComputeSourceHash()
        {
            try
            {
                string path = Path.Combine(Globals.UserDataDir, "bin", "Custom", "AddOns", "VLTraderTCPClient.cs");
                using (var sha = System.Security.Cryptography.SHA256.Create())
                using (var fs = File.OpenRead(path))
                {
                    byte[] h = sha.ComputeHash(fs);
                    var sb = new StringBuilder(h.Length * 2);
                    foreach (byte b in h) sb.Append(b.ToString("x2", CultureInfo.InvariantCulture));
                    return sb.ToString();
                }
            }
            catch { return ""; }
        }

        private void SendFillFrame(string signalId, double fillPrice, string side,
                                   int qty, double slippageTicks, string status,
                                   string acctName = "", string symbol = "")
        {
            var payload = new Dictionary<string, object>
            {
                ["signal_id"]      = signalId,
                ["fill_price"]     = fillPrice,
                ["fill_time"]      = DateTime.UtcNow.ToString("o"),
                ["side"]           = side,
                ["quantity"]       = qty,
                ["slippage_ticks"] = slippageTicks,
                ["status"]         = status,
                // PHASE 4: which account this fill is on (e.Order.Account). Additive +
                // back-compat: an un-updated Go binary ignores unknown JSON fields.
                ["account"]        = acctName ?? "",
                // P5.2: which INSTRUMENT this fill is for (multi-symbol
                // attribution). Empty = unknown; the Go side treats empty as the
                // primary trading symbol (legacy-compatible).
                ["symbol"]         = symbol ?? ""
            };
            StampIdentity(payload, signalId); // A2 (G1) — echo trader_id + seq
            WriteEnvelope("fill", payload);
        }

        // A2 (G1) — echo the originating identity (trader_id + seq) onto an outbound
        // fill/close/reject so Go can verify the AddOn acted for the right trader. The
        // account is already on those payloads. No-op when the signal_id is unknown
        // (a manual/uncorrelated exit) → Go treats the absent seq as legacy-tolerated.
        private void StampIdentity(Dictionary<string, object> payload, string signalId)
        {
            if (payload == null || string.IsNullOrEmpty(signalId)) return;
            SignalIdentity id = null;
            lock (signalMapLock) { signalIdentity.TryGetValue(signalId, out id); }
            if (id != null)
            {
                payload["trader_id"] = id.TraderId ?? "";
                payload["seq"]       = id.Seq;
            }
        }

        // PHASE 2 armed orders — push every order state change to Go (deduped
        // per order name) so the armed engine sees working/cancelled resting
        // entries and bracket legs. signal_id is the bare order name minus the
        // -sl/-tp/-lx suffix (same derivation as the fill path).
        // ── F12: the working-order SNAPSHOT ──────────────────────────────────
        //
        // Every other frame this AddOn sends is an EVENT. order_update fires on a
        // state change, so a Go-side restart loses the picture until the next
        // change happens — which on a quiet book may be never. Cutover leg 4
        // therefore had to read the Go side's own armed_orders ledger and call it
        // the broker's book.
        //
        // This frame is the broker's answer: every working/accepted order on the
        // account, emitted every ORDER_SNAPSHOT_INTERVAL_MS and immediately on any
        // order state change. It is ACCOUNT-scoped and each order carries its own
        // symbol; the Go side filters by instrument. That keeps an empty book
        // representable — orders: [] is an explicit empty, and "no orders" and "no
        // answer" must never render as the same thing.
        private void SendOrderSnapshot(Account acc, string reason)
        {
            if (acc == null) return;
            try
            {
                var orders = new List<object>();
                // Copy under the collection's lock: NT8 mutates account
                // collections from its own threads and a bare foreach throws.
                lock (acc.Orders)
                {
                    foreach (Order o in acc.Orders)
                    {
                        if (o == null) continue;
                        string st = o.OrderState.ToString();
                        // Terminal orders are history. The Go side filters again
                        // (one definition of "working" lives there), but shipping
                        // the whole history every 30s would grow without bound.
                        //
                        // D3 (2026-09-07) — "Unknown" WAS in this list and is not
                        // any more. It is not history; it is the state we are
                        // least entitled to act on. Dropped here it did not reach
                        // Go as unknown, it reached Go as ABSENT — and absence is
                        // what every "this order is gone" branch keys on: the
                        // stale reaper cancels and marks the row cancelled,
                        // cancel_pending is promoted to cancelled, entryIsResting
                        // sees no children and allows the cancel, leg 4 counts
                        // zero working orders, and the D5 reconciler concludes a
                        // position is unprotected and places a SECOND stop beside
                        // an invisible live one. Owner ruling 2026-09-07: UNKNOWN
                        // is non-terminal and takes no destructive branch. That
                        // is decided HERE, not in Go.
                        if (st == "Filled" || st == "Cancelled" || st == "Rejected" ||
                            st == "Expired")
                            continue;

                        string sym = "";
                        try { sym = o.Instrument.MasterInstrument.Name; } catch { }

                        string otype = "market";
                        try
                        {
                            if (o.OrderType == OrderType.Limit)           otype = "limit";
                            else if (o.OrderType == OrderType.StopMarket) otype = "stop";
                            else if (o.OrderType == OrderType.StopLimit)  otype = "stop-limit";
                            else if (o.OrderType == OrderType.StopLimit)  otype = "stop_limit";
                        }
                        catch { }

                        string act = "sell";
                        try
                        {
                            if (o.OrderAction == OrderAction.Buy || o.OrderAction == OrderAction.BuyToCover)
                                act = "buy";
                        }
                        catch { }

                        string tif = "";
                        try { tif = o.TimeInForce.ToString(); } catch { }

                        orders.Add(new Dictionary<string, object>
                        {
                            ["order_id"]    = o.OrderId ?? "",
                            ["name"]        = o.Name ?? "",
                            ["action"]      = act,
                            ["type"]        = otype,
                            ["limit_price"] = o.LimitPrice,
                            ["stop_price"]  = o.StopPrice,
                            ["quantity"]    = o.Quantity,
                            ["filled"]      = o.Filled,
                            ["state"]       = st,
                            ["oco"]         = o.Oco ?? "",
                            // D6 (2026-09-07) — the TIF rides the book so the boot
                            // line can READ whether protective orders are actually
                            // GTC instead of asserting it (A11). Additive: an older
                            // Go build ignores it, and an older AddOn simply omits
                            // it, which reads as "n/a" rather than as "Day".
                            ["tif"]         = tif,
                            ["symbol"]      = sym ?? ""
                        });
                    }
                }

                WriteEnvelope("order_snapshot", new Dictionary<string, object>
                {
                    ["account"]       = acc.Name ?? "",
                    ["build_id"]      = VL_BUILD_ID,
                    ["emitted_at_ms"] = SnapshotNowUtcMs(),
                    ["reason"]        = reason ?? "periodic",
                    ["orders"]        = orders
                });
            }
            catch (Exception ex)
            {
                // A10 — never fatal. A dropped snapshot ages out on the Go side
                // and leg 4 fails closed; a throw here would take the AddOn down.
                LogWarn("VLTraderTCPClient: order_snapshot frame failed: " + ex.Message);
            }
        }

        // Epoch milliseconds. Same derivation VLBarsSubscriptionManager.NowUtcMs
        // uses; duplicated rather than shared because the two files are compiled
        // as independent AddOns and NT8 does not link them.
        private static long SnapshotNowUtcMs()
        {
            return (long)((DateTime.UtcNow
                - new DateTime(1970, 1, 1, 0, 0, 0, DateTimeKind.Utc)).TotalMilliseconds);
        }

        private void SendOrderUpdateFrame(OrderEventArgs e)
        {
            try
            {
                if (e.Order == null) return;
                string orderName = e.Order.Name ?? "";
                string state = e.OrderState.ToString().ToLowerInvariant();
                string key = orderName.Length > 0 ? orderName : (e.Order.Oco ?? "");
                if (string.IsNullOrEmpty(key)) return;
                lock (signalMapLock)
                {
                    if (lastOrderState.TryGetValue(key, out var prev) && prev == state) return;
                    lastOrderState[key] = state;
                }
                string signalId = orderName.Length > 0 ? orderName : (e.Order.Oco ?? "");
                // REVIEW-SPLIT-2 P2-3: strip the leg-2 suffixes first so an
                // order_update for -sl2/-tp2 reports the SAME base signal id the
                // Go side tracks (it never sees a dangling "-sl2").
                if (signalId.EndsWith("-sl2") || signalId.EndsWith("-tp2"))
                    signalId = signalId.Substring(0, signalId.Length - 4);
                else if (signalId.EndsWith("-sl") || signalId.EndsWith("-tp") || signalId.EndsWith("-lx"))
                    signalId = signalId.Substring(0, signalId.Length - 3);
                string sym = "";
                try { sym = e.Order.Instrument.MasterInstrument.Name; } catch { }
                var payload = new Dictionary<string, object>
                {
                    ["signal_id"]  = signalId,
                    ["order_name"] = orderName,
                    ["state"]      = state,
                    ["fill_price"] = e.AverageFillPrice,
                    ["quantity"]   = e.Filled,
                    ["symbol"]     = sym ?? "",
                    ["account"]    = e.Order.Account != null ? e.Order.Account.Name
                                         : (account != null ? account.Name : "")
                };
                // PICTURE-HTF evidence: a rejected order names WHY on the wire
                // (the Go side already receives an additive reason field).
                if (e.OrderState == OrderState.Rejected)
                {
                    string reason = "";
                    try { reason = e.Comment ?? ""; } catch { }
                    if (string.IsNullOrEmpty(reason)) { try { reason = e.Error.ToString(); } catch { } }
                    if (string.IsNullOrEmpty(reason)) reason = "rejected";
                    payload["reason"] = reason;
                }
                StampIdentity(payload, signalId);
                WriteEnvelope("order_update", payload);
                // F12 — a state change makes the book stale the instant it
                // happens, so the snapshot follows the event rather than waiting
                // up to 30s. The Go side is idempotent: a snapshot simply
                // replaces the cached book.
                try
                {
                    SendOrderSnapshot(e.Order.Account != null ? e.Order.Account : account, "state_change");
                }
                catch (Exception exSnap)
                {
                    LogWarn("VLTraderTCPClient: post-update order_snapshot failed: " + exSnap.Message);
                }
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: order_update frame failed: " + ex.Message);
            }
        }

        // PHASE 2 armed orders — cancel a working resting limit entry and/or its
        // live protective bracket legs (managed Account.Cancel). Idempotent: a
        // no-op (with ack) when nothing is working for the signal id.
        private void HandleCancelOrder(Dictionary<string, object> p)
        {
            try
            {
                if (p == null) { LogWarn("VLTraderTCPClient: cancel_order empty payload"); return; }
                string signalId = GetString(p, "signal_id");
                if (string.IsNullOrEmpty(signalId)) { LogWarn("VLTraderTCPClient: cancel_order missing signal_id"); return; }

                // ── D1 (2026-09-07) — A CANCEL TARGETS THE ENTRY. FULL STOP. ──
                //
                // This handler used to be a TWO-PART cancel: the resting entry
                // from workingEntries, and then, unconditionally, the SL and TP
                // from placedBrackets.
                //
                // 2026-09-06 23:37:02 a cancel_order arrived for signal
                // aa07e583 whose entry had filled ~100 seconds earlier.
                // workingEntries.Remove had already fired on that fill (see the
                // Filled branch of OnOrderUpdate), so part one found nothing and
                // ONLY THE SECOND HALF RAN — cancelling accepted stop 29554 and
                // working target 29623. Position 592 ran unprotected for 8h19m,
                // through the Monday open, and the owner flattened it by hand.
                //
                // This was never OCO propagation. The entry carries an EMPTY oco
                // group and the children their own shared "<signal>-exit" id
                // (SubmitBracketOnEntryFill). Our own code reached across and
                // killed them.
                //
                // OWNER RULING 2026-09-07: cancelling a bracket leg is its own
                // explicit call, never a side effect of cancelling an entry. The
                // two places that legitimately retire protection — closing a
                // position, and the netting-flat sweep — still do so, because
                // retiring protection is what those handlers ARE.
                Order working = null;
                lock (signalMapLock)
                {
                    workingEntries.TryGetValue(signalId, out working);
                    workingEntries.Remove(signalId);
                }
                if (working != null)
                {
                    OrderState wst = OrderState.Unknown;
                    try { wst = working.OrderState; } catch { }
                    try
                    {
                        var acct = working.Account ?? account;
                        acct.Cancel(new[] { working });
                        LogInfo("VLTraderTCPClient: cancel_order cancelled resting ENTRY " + signalId
                                + " state=" + wst + " source=workingEntries (bracket legs untouched)");
                    }
                    catch (Exception ex)
                    {
                        LogWarn("VLTraderTCPClient: cancel_order entry Cancel failed: " + signalId
                                + " state=" + wst + ": " + ex.Message);
                    }
                }
                else
                {
                    // NOT AN ERROR, AND NOT A LICENCE. The entry is not resting:
                    // it filled, or it was already cancelled. Either way there is
                    // no entry to cancel and the protections are not ours to
                    // touch from here.
                    // This handler does not READ placedBrackets either. A read
                    // is one edit away from a cancel, and the rule is that the
                    // bracket is not this handler's business at all.
                    LogInfo("VLTraderTCPClient: cancel_order found NO resting entry for " + signalId
                            + " source=workingEntries — nothing cancelled (a filled entry's"
                            + " protections are never cancelled by an entry cancel;"
                            + " 2026-09-06 23:37:02)");
                }
                // The DEFERRED bracket for this entry is not a live order — it is
                // our own note to place one WHEN the entry fills. A cancelled
                // entry must not leave that note behind: SubmitBracketOnEntryFill
                // fires on ANY later Filled for the same key, and nothing else
                // clears it (only a REJECT did), so the map also grew for the
                // life of the AddOn. Bookkeeping, not an order — and emphatically
                // NOT the placedBrackets walk this handler no longer does. A
                // no-op when the entry already filled, because the bracket was
                // placed and the note consumed at that moment.
                lock (signalMapLock) { pendingBrackets.Remove(signalId); }
                SendCancelReport(signalId);
                SendAck("cancel_order");
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: cancel_order failed: " + ex.Message);
            }
        }

        // ── CANCEL-REPORT (2026-10-03-c1) ─────────────────────────────────────
        //
        // The Go side's report regime (CANCEL_CONFIRM_REQUIRE_REPORT, off by
        // default) settles a cancel ONLY on a POSITIVE per-order terminal
        // report: the order_snapshot deliberately omits terminal orders (D3,
        // 2026-09-07), so absence from a book cannot distinguish "cancelled"
        // from "never answered". This echo is emitted on EVERY cancel_order
        // receipt and bypasses the lastOrderState dedupe — it is the report the
        // gate waits for, not a state-change event. A subsequent OnOrderUpdate
        // still emits the real terminal transition (state=cancelled), which the
        // Go side records as the latest report; both are idempotent there.
        //
        // FAIL-CLOSED BY CONSTRUCTION: if the order is not found in the
        // account's collection (never placed, or purged), NOTHING is emitted —
        // the Go side settles nothing and the slot stays busy. An absence we
        // cannot explain is never dressed up as a report.
        private void SendCancelReport(string signalId)
        {
            try
            {
                Order target = null;
                Account acct = account;
                lock (signalMapLock)
                {
                    workingEntries.TryGetValue(signalId, out target);
                    if (target != null) { try { acct = target.Account ?? account; } catch { } }
                }
                if (target == null && acct != null)
                {
                    lock (acct.Orders)
                    {
                        foreach (Order o in acct.Orders)
                        {
                            if (o == null) continue;
                            string n = o.Name ?? "";
                            string g = o.Oco ?? "";
                            if (n == signalId || g == signalId) { target = o; break; }
                        }
                    }
                }
                if (target == null) return;
                string state = "";
                try { state = target.OrderState.ToString().ToLowerInvariant(); } catch { }
                var payload = new Dictionary<string, object>
                {
                    ["signal_id"]     = signalId,
                    ["order_name"]    = target.Name ?? "",
                    ["state"]         = state,
                    ["fill_price"]    = target.AverageFillPrice,
                    ["quantity"]      = target.Filled,
                    ["account"]       = (target.Account ?? acct) != null ? (target.Account ?? acct).Name : "",
                    ["cancel_report"] = true
                };
                try { payload["symbol"] = target.Instrument.MasterInstrument.Name ?? ""; } catch { }
                StampIdentity(payload, signalId);
                WriteEnvelope("order_update", payload);
                LogInfo("VLTraderTCPClient: cancel_report echo " + signalId + " state=" + state);
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: cancel_report echo failed: " + ex.Message);
            }
        }

        // D3 (2026-09-07) — THE ADDON'S ONE DEFINITION OF "LIVE AT THE EXCHANGE".
        //
        // HandleModifyBracket and HandleMoveStop each carried their own copy of
        // `st != Working && st != Accepted`. Two copies of a two-state whitelist
        // is how the tree ended up disagreeing with itself about TriggerPending —
        // which NT8 holds on THIS PC and which neither side should treat as
        // protection at the exchange. Mirrors ClassifyOrderState's LivenessLive
        // in provider/ninjatrader/order_state.go; change them together.
        private static bool IsLiveAtExchange(OrderState st)
        {
            return st == OrderState.Working || st == OrderState.Accepted;
        }

        // ── D5 (2026-09-07) — PLACE A STOP FOR AN UNPROTECTED POSITION ──
        //
        // The reconciler on the Go side has established, from the broker's own
        // book, that a position is open and NOTHING protective is resting for
        // it. That is the one case where acting beats reporting (owner ruling).
        //
        // STANDALONE BY DESIGN. This order joins no OCO group. A group is what
        // lets one cancel take another order with it, and this stop exists
        // precisely because the last one went away. It is GTC for the same
        // reason the bracket legs are: a protective order must not expire while
        // the position it protects survives.
        //
        // It is named "<signal>-sl" so every existing reader — accepted_risk's
        // leg naming, entryIsResting's child detection, the reconciler's own
        // next pass — sees it as the protection it is.
        private void HandlePlaceProtectiveStop(Dictionary<string, object> p)
        {
            try
            {
                if (p == null) { LogWarn("VLTraderTCPClient: place_protective_stop empty payload"); return; }
                string signalId = GetString(p, "signal_id");
                string symbol   = GetString(p, "symbol");
                string side     = (GetString(p, "position_side") ?? "").ToUpperInvariant();
                string acctName = GetString(p, "account");
                string reason   = GetString(p, "reason");
                double stopPx   = 0.0;
                double qtyD     = 0.0;
                try { stopPx = GetDouble(p, "stop_price"); } catch { }
                try { qtyD   = GetDouble(p, "quantity"); } catch { }
                int qty = (int)qtyD;

                if (string.IsNullOrEmpty(signalId) || string.IsNullOrEmpty(symbol)
                    || (side != "LONG" && side != "SHORT") || qty <= 0 || stopPx <= 0)
                {
                    LogWarn("VLTraderTCPClient: place_protective_stop REFUSED — incomplete request"
                            + " signal_id=" + signalId + " symbol=" + symbol + " side=" + side
                            + " qty=" + qty + " stop=" + stopPx
                            + " (a protective order is never sized or priced from a zero)");
                    SendAck("place_protective_stop_error");
                    return;
                }

                // Resolve the account the SAME way an entry does, and re-assert
                // every guard on the exact submit target.
                Account resolved = null;
                if (!string.IsNullOrEmpty(acctName))
                {
                    lock (Account.All)
                    {
                        foreach (var a in Account.All)
                            if (a.Name == acctName) { resolved = a; break; }
                    }
                }
                Account ba = resolved ?? account;
                if (ba == null || !IsSimAccount(ba)
                    || ba.Connection == null || ba.Connection.Status != ConnectionStatus.Connected
                    || !IsSessionAccount(ba.Name))
                {
                    LogWarn("VLTraderTCPClient: place_protective_stop HARD-REFUSED for " + signalId
                            + " — submit target failed the SIM/connected/allowlist guard");
                    SendAck("place_protective_stop_error");
                    return;
                }

                string contract = VLContractResolver.ResolveFrontMonthContract(symbol);
                string how;
                var instrument = VLInstrumentLookup.Resolve(symbol, LogWarn, out how);
                if (instrument == null)
                {
                    LogWarn("VLTraderTCPClient: place_protective_stop — instrument " + symbol
                            + " (resolved to " + contract + ", " + how + ") not found; position stays UNPROTECTED");
                    SendAck("place_protective_stop_error");
                    return;
                }

                OrderAction exitAction = (side == "LONG") ? OrderAction.Sell : OrderAction.BuyToCover;
                SubscribeOrderUpdate(ba);
                var slOrder = ba.CreateOrder(
                    instrument, exitAction, OrderType.StopMarket, OrderEntry.Manual,
                    TimeInForce.Gtc, qty, 0, stopPx, string.Empty, signalId + "-sl",
                    Core.Globals.MaxDate, null);
                ba.Submit(new[] { slOrder });
                LogInfo("VLTraderTCPClient: PLACED protective stop signal_id=" + signalId
                        + " " + side + " " + symbol + " qty=" + qty + " stop=" + stopPx
                        + " oco=<none> tif=Gtc name=" + signalId + "-sl"
                        + " reason=" + (reason ?? "reconciler"));
                SendAck("place_protective_stop");
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: place_protective_stop FAILED: " + ex.Message
                        + " — the position remains UNPROTECTED");
                SendAck("place_protective_stop_error");
            }
        }

        // PHASE 2 armed orders — in-place SL/TP modify on the LIVE bracket: the
        // SAME safe Change pattern as move_stop (new_stop_loss / new_take_profit
        // optional; missing = leave untouched). Refuses with modify_bracket_error
        // when no live bracket exists or a leg is not Working/Accepted.
        private void HandleModifyBracket(Dictionary<string, object> p)
        {
            try
            {
                if (p == null) { LogWarn("VLTraderTCPClient: modify_bracket empty payload"); return; }
                string signalId = GetString(p, "signal_id");
                if (string.IsNullOrEmpty(signalId)) { LogWarn("VLTraderTCPClient: modify_bracket missing signal_id"); return; }
                double newSl = 0.0, newTp = 0.0;
                int    leg   = GetInt(p, "leg");   // 1 = leg 1, 2 = leg 2, 0/absent = ALL
                try { newSl = GetDouble(p, "new_stop_loss"); } catch { }
                try { newTp = GetDouble(p, "new_take_profit"); } catch { }
                if (newSl <= 0 && newTp <= 0) { LogWarn("VLTraderTCPClient: modify_bracket nothing to change"); return; }

                PlacedBracket pb;
                lock (signalMapLock)
                {
                    if (!placedBrackets.TryGetValue(signalId, out pb))
                    {
                        LogWarn("VLTraderTCPClient: modify_bracket no live bracket for " + signalId);
                        SendAck("modify_bracket_error");
                        return;
                    }
                }
                Account ba = pb.Account ?? account;

                // REVIEW-353: honour `leg` — 1 = leg 1 only, 2 = leg 2 only, 0/absent = ALL brackets.
                var slLegs = new List<Order>();
                var tpLegs = new List<Order>();
                if (leg != 2)
                {
                    if (pb.SlOrder != null) slLegs.Add(pb.SlOrder);
                    if (pb.TpOrder != null) tpLegs.Add(pb.TpOrder);
                }
                if (leg != 1)
                {
                    if (pb.SlOrder2 != null) slLegs.Add(pb.SlOrder2);
                    if (pb.TpOrder2 != null) tpLegs.Add(pb.TpOrder2);
                }

                // P1-1b: skip terminal legs (e.g. leg 1 already filled); error only
                // when NO selected leg is live.
                bool anyLive = false;
                var all = new List<Order>();
                if (newSl > 0)
                {
                    foreach (var sl in slLegs)
                    {
                        if (!IsLiveAtExchange(sl.OrderState)) continue;
                        anyLive = true;
                        sl.StopPriceChanged = newSl;
                        all.Add(sl);
                    }
                }
                if (newTp > 0)
                {
                    foreach (var tp in tpLegs)
                    {
                        if (!IsLiveAtExchange(tp.OrderState)) continue;
                        anyLive = true;
                        tp.LimitPriceChanged = newTp;
                        all.Add(tp);
                    }
                }
                if (!anyLive)
                {
                    LogWarn("VLTraderTCPClient: modify_bracket refused — no live leg for " + signalId);
                    SendAck("modify_bracket_error");
                    return;
                }
                if (all.Count > 0) ba.Change(all.ToArray());
                LogInfo("VLTraderTCPClient: modify_bracket → SL=" + newSl + " TP=" + newTp + " leg=" + leg + " for " + signalId + " (in-place Change — OCO preserved)");
                SendAck("modify_bracket");
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: modify_bracket failed: " + ex.Message);
                SendAck("modify_bracket_error");
            }
        }

        // Place the protective SL + TP once the entry has filled. They share
        // their OWN OCO group (signal_id-exit) so one cancels the other when
        // triggered, and — crucially — they are NOT in the entry's OCO group,
        // so the entry fill no longer cancels them. Names keep the -sl/-tp
        // suffix so OnOrderUpdate routes their fills to position_close.
        // Auto-breakeven: move a live bracket's resting stop to a new price WITHOUT
        // closing the position. The new StopMarket is submitted in the SAME OCO group
        // FIRST, then the old one is cancelled — so the position is NEVER momentarily
        // unprotected (and if anything throws, the original stop survives). No-op if
        // the bracket is gone (already exited) or the stop is already at/better than
        // the requested price (restart idempotency). Acks back to Go.
        private void HandleMoveStop(Dictionary<string, object> p)
        {
            try
            {
                if (p == null) { LogWarn("VLTraderTCPClient: move_stop empty payload"); return; }
                string signalId = GetString(p, "signal_id");
                double newStop  = GetDouble(p, "new_stop_loss");
                int    leg      = GetInt(p, "leg");   // 1 = leg 1, 2 = leg 2, 0/absent = ALL
                if (string.IsNullOrEmpty(signalId) || newStop <= 0)
                { LogWarn("VLTraderTCPClient: move_stop bad payload (signal_id/new_stop_loss)"); return; }

                PlacedBracket pb;
                lock (signalMapLock)
                {
                    if (!placedBrackets.TryGetValue(signalId, out pb))
                    { LogWarn("VLTraderTCPClient: move_stop no live bracket for " + signalId + " (already exited?)"); return; }
                }

                Account ba = pb.Account ?? account;

                // REVIEW-353: honour `leg` — 1 = leg 1 only, 2 = leg 2 only, 0/absent = ALL brackets.
                var stops = new List<Order>();
                if (leg != 2 && pb.SlOrder != null) stops.Add(pb.SlOrder);
                if (leg != 1 && pb.SlOrder2 != null) stops.Add(pb.SlOrder2);

                bool anyLive = false;
                bool moved = false;
                foreach (var sl in stops)
                {
                    OrderState st = sl.OrderState;
                    if (!IsLiveAtExchange(st))
                        continue; // P1-1b: skip terminal legs (e.g. leg 1 already filled)
                    anyLive = true;
                    if (Math.Abs(sl.StopPrice - newStop) < (pb.TickSize / 2.0))
                        continue; // this leg is already there
                    // IN-PLACE modification (fix 2026-08-07): move the SAME resting stop's price
                    // via Account.Change — same Order object, same OCO group, NO new order and
                    // NO cancel. The target and the OCO group are NEVER disturbed.
                    try
                    {
                        sl.StopPriceChanged = newStop;
                        ba.Change(new[] { sl });
                        moved = true;
                    }
                    catch (Exception cx)
                    {
                        LogWarn("VLTraderTCPClient: move_stop Change failed: " + cx.Message + " for " + signalId);
                        SendAck("move_stop_error");
                        return;
                    }
                }
                if (!anyLive)
                {
                    LogWarn("VLTraderTCPClient: move_stop refused — no live stop for " + signalId);
                    SendAck("move_stop_error");
                    return;
                }
                if (!moved)
                { LogInfo("VLTraderTCPClient: move_stop no-op (stop already ~" + newStop + ")"); return; }
                LogInfo("VLTraderTCPClient: move_stop → " + newStop + " for signal_id=" + signalId + " leg=" + leg + " (auto-breakeven, in-place Change — target + OCO preserved)");
                SendAck("move_stop");
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: move_stop failed: " + ex.Message);
            }
        }

        // D2 (2026-09-07) — THE BRACKET COVERS WHAT FILLED, SIZED FROM THE EVENT.
        //
        // This used to size the pair from the cached PendingBracket captured when
        // the entry was SUBMITTED. NinjaTrader staff guidance is to update from
        // the passed-in event parameters rather than a cached mirror or the
        // return of Submit: ordering across OrderUpdate / ExecutionUpdate /
        // PositionUpdate is not guaranteed, and on a part-fill the quantity we
        // asked for is not the quantity that needs protecting.
        //
        // filledQty is the order's CUMULATIVE filled quantity (OrderEventArgs
        // .Filled), so a completing part-fill AMENDS the existing pair to the new
        // absolute quantity instead of placing a second bracket. A second bracket
        // in the same exit OCO group would be worse than none: one stop firing
        // would cancel the other and leave the rest of the position naked.
        private void SubmitBracketOnEntryFill(string signalId, int filledQty, double avgFillPx)
        {
            if (filledQty <= 0)
            {
                LogWarn("VLTraderTCPClient: bracket NOT placed for " + signalId
                        + " — fill event reported quantity " + filledQty
                        + "; refusing to size a protective order from a non-positive fill");
                return;
            }

            // A later fill on the same entry amends what is already resting.
            PlacedBracket already = null;
            lock (signalMapLock) { placedBrackets.TryGetValue(signalId, out already); }
            if (already != null)
            {
                AmendBracketQuantity(signalId, already, filledQty);
                return;
            }

            PendingBracket b;
            lock (signalMapLock)
            {
                if (!pendingBrackets.TryGetValue(signalId, out b)) return;
                pendingBrackets.Remove(signalId); // idempotent: only place once
            }
            try
            {
                // PHASE 3: the protective bracket is placed on the SAME account the entry
                // routed to (stored in PendingBracket), not necessarily the active one.
                Account ba = b.Account ?? account;
                // D6 (2026-09-07) — PROTECTIVE ORDERS ARE GTC.
                //
                // These were TimeInForce.Day. A Day order is dropped at session
                // end; GTC survives NT8's daily maintenance window. A stop that
                // expires while the position it protects does not is a naked
                // position on a timer — the 09-06 exposure arriving by the
                // calendar instead of by a cancel. The ENTRY stays Day (see
                // HandleSignal): an unfilled entry must die with its session
                // rather than wake up and fire into a market its plan never saw.
                double tick = 0.25;
                try { tick = b.Instrument.MasterInstrument.TickSize; } catch { }

                // REVIEW-353: the split AT ENTRY — leg 1 (ceil(n/2), its OWN TP)
                // + leg 2 (the runner, TP = b.Tp), BOTH under the one entry signal id.
                // P0-1: treat Leg1Qty >= Qty as a single bracket; a first fill at or
                // below leg1_qty must NOT submit a 0-qty leg-2 pair.
                bool splitWanted = b.Leg1Qty > 0 && b.Leg1Tp > 0 && b.Leg1Qty < b.Qty;
                int leg1Qty = splitWanted ? Math.Min(b.Leg1Qty, filledQty) : 0;
                int leg2Qty = splitWanted ? Math.Max(0, filledQty - leg1Qty) : 0;

                if (!splitWanted)
                {
                    // Single bracket (legacy / n=1 / swing): one SL+TP pair.
                    string exitOco = signalId + "-exit";
                    var slOrder = ba.CreateOrder(
                        b.Instrument, b.ExitAction, OrderType.StopMarket, OrderEntry.Manual,
                        TimeInForce.Gtc, filledQty, 0, b.Sl, exitOco, signalId + "-sl",
                        Core.Globals.MaxDate, null);
                    var tpOrder = ba.CreateOrder(
                        b.Instrument, b.ExitAction, OrderType.Limit, OrderEntry.Manual,
                        TimeInForce.Gtc, filledQty, b.Tp, 0, exitOco, signalId + "-tp",
                        Core.Globals.MaxDate, null);
                    ba.Submit(new[] { slOrder, tpOrder });
                    lock (signalMapLock)
                    {
                        placedBrackets[signalId] = new PlacedBracket
                        {
                            SlOrder = slOrder, TpOrder = tpOrder, Account = ba, Instrument = b.Instrument,
                            ExitAction = b.ExitAction, Qty = filledQty, ExitOco = exitOco, TickSize = tick,
                            // REVIEW-353: a partial FIRST fill of a split entry still records the
                            // intended split so a later fill grows leg 2 (AmendBracketQuantity).
                            Leg1Qty = splitWanted ? b.Leg1Qty : 0, Leg1Tp = splitWanted ? b.Leg1Tp : 0,
                            RunnerTp = b.Tp,
                        };
                    }
                    LogInfo("VLTraderTCPClient: placed protective bracket signal_id=" + signalId
                            + " sl=" + b.Sl + " tp=" + b.Tp
                            + " qty=" + filledQty + " (requested " + b.Qty + ")"
                            + " avg_fill=" + avgFillPx
                            + " entry_oco=<none> exit_oco=" + exitOco + " tif=Gtc");
                }
                else if (leg2Qty > 0)
                {
                    // REVIEW-353: TWO OCO pairs under the ONE entry signal id —
                    // leg 1 (leg1_qty, TP=leg1_tp) + leg 2 (runner, TP=b.Tp).
                    string exitOco1 = signalId + "-exit";
                    string exitOco2 = signalId + "-exit2";
                    var sl1 = ba.CreateOrder(
                        b.Instrument, b.ExitAction, OrderType.StopMarket, OrderEntry.Manual,
                        TimeInForce.Gtc, leg1Qty, 0, b.Sl, exitOco1, signalId + "-sl",
                        Core.Globals.MaxDate, null);
                    var tp1 = ba.CreateOrder(
                        b.Instrument, b.ExitAction, OrderType.Limit, OrderEntry.Manual,
                        TimeInForce.Gtc, leg1Qty, b.Leg1Tp, 0, exitOco1, signalId + "-tp",
                        Core.Globals.MaxDate, null);
                    var sl2 = ba.CreateOrder(
                        b.Instrument, b.ExitAction, OrderType.StopMarket, OrderEntry.Manual,
                        TimeInForce.Gtc, leg2Qty, 0, b.Sl, exitOco2, signalId + "-sl2",
                        Core.Globals.MaxDate, null);
                    var tp2 = ba.CreateOrder(
                        b.Instrument, b.ExitAction, OrderType.Limit, OrderEntry.Manual,
                        TimeInForce.Gtc, leg2Qty, b.Tp, 0, exitOco2, signalId + "-tp2",
                        Core.Globals.MaxDate, null);
                    ba.Submit(new[] { sl1, tp1, sl2, tp2 });
                    lock (signalMapLock)
                    {
                        placedBrackets[signalId] = new PlacedBracket
                        {
                            SlOrder = sl1, TpOrder = tp1, SlOrder2 = sl2, TpOrder2 = tp2,
                            Account = ba, Instrument = b.Instrument, ExitAction = b.ExitAction,
                            Qty = filledQty, ExitOco = exitOco1, ExitOco2 = exitOco2, TickSize = tick,
                            Leg1Qty = b.Leg1Qty, Leg2Qty = leg2Qty, Leg1Tp = b.Leg1Tp, RunnerTp = b.Tp,
                        };
                    }
                    LogInfo("VLTraderTCPClient: placed SPLIT bracket signal_id=" + signalId
                            + " leg1=" + leg1Qty + "@" + b.Leg1Tp
                            + " leg2=" + leg2Qty + "@" + b.Tp
                            + " sl=" + b.Sl + " avg_fill=" + avgFillPx
                            + " oco1=" + exitOco1 + " oco2=" + exitOco2 + " tif=Gtc");
                }
                else
                {
                    // P0-1: a first fill at or below leg1_qty — place LEG 1 ONLY
                    // (TP = leg1_tp) and record the INTENDED split so a later fill
                    // grows leg 2 via AmendBracketQuantity's create branch. Never
                    // submit a 0-qty leg-2 pair.
                    string exitOco1 = signalId + "-exit";
                    var sl1 = ba.CreateOrder(
                        b.Instrument, b.ExitAction, OrderType.StopMarket, OrderEntry.Manual,
                        TimeInForce.Gtc, leg1Qty, 0, b.Sl, exitOco1, signalId + "-sl",
                        Core.Globals.MaxDate, null);
                    var tp1 = ba.CreateOrder(
                        b.Instrument, b.ExitAction, OrderType.Limit, OrderEntry.Manual,
                        TimeInForce.Gtc, leg1Qty, b.Leg1Tp, 0, exitOco1, signalId + "-tp",
                        Core.Globals.MaxDate, null);
                    ba.Submit(new[] { sl1, tp1 });
                    lock (signalMapLock)
                    {
                        placedBrackets[signalId] = new PlacedBracket
                        {
                            SlOrder = sl1, TpOrder = tp1, Account = ba, Instrument = b.Instrument,
                            ExitAction = b.ExitAction, Qty = filledQty, ExitOco = exitOco1, TickSize = tick,
                            Leg1Qty = b.Leg1Qty, Leg2Qty = 0, Leg1Tp = b.Leg1Tp, RunnerTp = b.Tp,
                        };
                    }
                    LogInfo("VLTraderTCPClient: placed LEG-1-ONLY bracket (partial first fill) signal_id=" + signalId
                            + " leg1=" + leg1Qty + "@" + b.Leg1Tp + " intended_leg1=" + b.Leg1Qty
                            + " sl=" + b.Sl + " avg_fill=" + avgFillPx + " tif=Gtc");
                }
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: bracket submit failed signal_id=" + signalId
                        + ": " + ex.Message);
            }
        }

        // D2 (2026-09-07) — a completing part-fill AMENDS the resting pair.
        //
        // NEVER cancel-and-replace: that leaves the filled quantity unprotected
        // for the width of the round trip, which is the exposure this whole wave
        // exists to remove. NEVER add a second pair to the same exit OCO group
        // either — one stop firing would cancel the other and leave the rest of
        // the position naked.
        //
        // filledQty is the order's CUMULATIVE filled quantity, so this sets an
        // ABSOLUTE quantity, not a delta. QuantityChanged + Account.Change is the
        // same staging pattern move_stop and modify_bracket already use for
        // prices.
        private void AmendBracketQuantity(string signalId, PlacedBracket pb, int filledQty)
        {
            if (pb == null || filledQty <= 0) return;
            if (filledQty == pb.Qty) return; // nothing changed; stay quiet
            Account ba = pb.Account ?? account;
            if (ba == null) return;
            try
            {
                var changed = new List<Order>();
                if (pb.Leg1Qty > 0)
                {
                    // REVIEW-353: allocate leg 1 FIRST up to its qty, then leg 2.
                    // REVIEW-SPLIT-2 P1 fold (b): when leg 1 is GONE (its exit
                    // already filled), every later fill protects under leg 2.
                    bool leg1Gone = pb.SlOrder == null && pb.TpOrder == null && pb.Leg1Exited > 0;
                    int leg1Qty = leg1Gone ? 0 : Math.Min(pb.Leg1Qty, filledQty);
                    int leg2Qty = leg1Gone ? Math.Max(0, filledQty - pb.Leg1Exited)
                                           : Math.Max(0, filledQty - leg1Qty);
                    if (!leg1Gone)
                    {
                        if (pb.SlOrder != null) { pb.SlOrder.QuantityChanged = leg1Qty; changed.Add(pb.SlOrder); }
                        if (pb.TpOrder != null) { pb.TpOrder.QuantityChanged = leg1Qty; changed.Add(pb.TpOrder); }
                    }
                    if (leg2Qty > 0)
                    {
                        if (pb.SlOrder2 == null || pb.TpOrder2 == null)
                        {
                            // Leg 2's pair was never placed (partial first fill, or leg 1
                            // exited before leg 2 existed): create it now at the last known
                            // leg-1 stop price.
                            string oco2 = signalId + "-exit2";
                            double stop = pb.LastStop;
                            if (stop <= 0 && pb.SlOrder != null) stop = pb.SlOrder.StopPrice;
                            var sl2 = ba.CreateOrder(pb.Instrument, pb.ExitAction, OrderType.StopMarket, OrderEntry.Manual,
                                TimeInForce.Gtc, leg2Qty, 0, stop, oco2, signalId + "-sl2", Core.Globals.MaxDate, null);
                            var tp2 = ba.CreateOrder(pb.Instrument, pb.ExitAction, OrderType.Limit, OrderEntry.Manual,
                                TimeInForce.Gtc, leg2Qty, pb.RunnerTp, 0, oco2, signalId + "-tp2", Core.Globals.MaxDate, null);
                            ba.Submit(new[] { sl2, tp2 });
                            pb.SlOrder2 = sl2; pb.TpOrder2 = tp2; pb.ExitOco2 = oco2;
                            LogInfo("VLTraderTCPClient: leg-2 bracket created signal_id=" + signalId
                                    + " qty=" + leg2Qty + " tp=" + pb.RunnerTp + " oco=" + oco2);
                        }
                        else
                        {
                            pb.SlOrder2.QuantityChanged = leg2Qty; changed.Add(pb.SlOrder2);
                            pb.TpOrder2.QuantityChanged = leg2Qty; changed.Add(pb.TpOrder2);
                        }
                    }
                    if (changed.Count > 0) ba.Change(changed.ToArray());
                    int was = pb.Qty;
                    lock (signalMapLock) { pb.Qty = filledQty; pb.Leg2Qty = leg2Qty; }
                    LogInfo("VLTraderTCPClient: SPLIT bracket amended signal_id=" + signalId
                            + " qty " + was + " -> " + filledQty
                            + " leg1=" + leg1Qty + " leg2=" + leg2Qty + (leg1Gone ? " (leg 1 already exited)" : " (leg 1 filled first)"));
                    return;
                }
                if (pb.SlOrder != null) { pb.SlOrder.QuantityChanged = filledQty; changed.Add(pb.SlOrder); }
                if (pb.TpOrder != null) { pb.TpOrder.QuantityChanged = filledQty; changed.Add(pb.TpOrder); }
                if (changed.Count == 0)
                {
                    LogWarn("VLTraderTCPClient: bracket amend found NO legs for " + signalId
                            + " — the position may be holding " + filledQty + " unprotected");
                    return;
                }
                ba.Change(changed.ToArray());
                int was2 = pb.Qty;
                lock (signalMapLock) { pb.Qty = filledQty; }
                LogInfo("VLTraderTCPClient: bracket amended signal_id=" + signalId
                        + " qty " + was2 + " -> " + filledQty
                        + " legs=" + changed.Count + " exit_oco=" + pb.ExitOco
                        + " (cumulative fill from the event)");
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: bracket amend FAILED signal_id=" + signalId
                        + " target_qty=" + filledQty + ": " + ex.Message
                        + " — the resting pair still covers " + pb.Qty);
            }
        }

        // Position-history fix — emit a position_close frame when an OCO exit
        // leg (SL/TP) fills. position_side is the side that was HELD. The Go
        // side computes realized PnL against the recorded entry × the futures
        // point value, so we send only the raw exit price here.
        private void SendPositionCloseFrame(string signalId, string symbol,
                                            string positionSide, double exitPrice,
                                            int qty, string exitReason,
                                            string acctName = "", int leg = 0)
        {
            var payload = new Dictionary<string, object>
            {
                ["signal_id"]     = signalId,
                ["symbol"]        = symbol ?? "",
                ["position_side"] = positionSide,
                ["exit_price"]    = exitPrice,
                ["quantity"]      = qty,
                ["exit_reason"]   = exitReason ?? "",
                // PHASE 4: the account this close is on (was absent; the Go struct already
                // has the field). e.Order.Account from the caller; "" → Go keeps today's
                // in-place-row behavior (the entry row already holds the account).
                ["account"]       = acctName ?? "",
                // REVIEW-SPLIT-2 P1-2: which leg exited (1/2/0=whole position).
                ["leg"]           = leg,
                ["exit_time"]     = DateTime.UtcNow.ToString("o")
            };
            StampIdentity(payload, signalId); // A2 (G1) — echo trader_id + seq (entry identity)
            WriteEnvelope("position_close", payload);
        }

        // Emit when an exit/flatten order is REJECTED (e.g. SIM "no market data"
        // while the feed is down). Tells Go the close did NOT take — the position
        // is STILL OPEN in NT8 — so the bot must not record it closed. Mirrors
        // SendPositionCloseFrame; reason carries the NT8 reject comment.
        private void SendPositionCloseRejectedFrame(string signalId, string symbol,
                                                    string positionSide, string reason,
                                                    string acctName = null)
        {
            var payload = new Dictionary<string, object>
            {
                ["signal_id"]     = signalId ?? "",
                ["symbol"]        = symbol ?? "",
                ["position_side"] = positionSide ?? "",
                ["reason"]        = reason ?? "",
                // PHASE 4 fix: stamp the account the rejected EXIT was on (e.Order.Account
                // from the caller), NOT the active field — the active-account stamp was a
                // latent bug once routing is live. Falls back to the active account for
                // back-compat if the caller passes none.
                ["account"]       = !string.IsNullOrEmpty(acctName) ? acctName : (account != null ? account.Name : ""),
                ["reject_time"]   = DateTime.UtcNow.ToString("o")
            };
            StampIdentity(payload, signalId); // A2 (G1) — echo trader_id + seq (entry identity)
            WriteEnvelope("position_close_rejected", payload);
        }

        // TRACK A — emit the NT8 price-feed status so Go can gate opens/closes when
        // the feed is down (the SIM rejects orders with "no market data"). Sent on
        // every connection-status transition (OnVLConnectionStatusUpdate).
        private void SendFeedStatusFrame(string priceStatus)
        {
            var payload = new Dictionary<string, object>
            {
                ["price_status"] = priceStatus ?? "",
                ["time"]         = DateTime.UtcNow.ToString("o")
            };
            WriteEnvelope("feed_status", payload);
        }

        // Open-position read-back — emit the selected account's CURRENT open
        // positions as a FULL snapshot. Called on connect, on account_select,
        // and on any Account.PositionUpdate (incl. MANUAL trades in NT8). NT8 is
        // the source of truth; before this the Go side only knew positions it
        // opened itself (via fills), so it lost the position on account
        // switch-back and never saw manual trades. The Go side REPLACES its
        // per-account cache with this set (an empty list = the account is flat).
        // Full snapshot from the (settled) live collection — used by the connect
        // + account_select paths, where positions are already settled. Delegates
        // to the evt-aware overload with no event override.
        private void SendOpenPositions(Account acc)
        {
            SendOpenPositions(acc, null);
        }

        // Emit the account's open positions. When called from OnPositionUpdate
        // (evt != null), the just-updated instrument's AveragePrice in the live
        // acc.Positions collection has NOT settled to the new fill yet — it still
        // holds the PRIOR netted average (the stale-entry bug). The
        // PositionEventArgs carries the SETTLED account-level values at the event
        // (NT staff + help guide: e.MarketPosition/e.Quantity/e.AveragePrice are
        // "the actual account value at that time", unlike e.Position.* which is
        // only the currently-updating position), so for THAT instrument we use
        // evt's by-value fields. Other instruments + the connect/select snapshot
        // (evt == null) read the settled collection directly.
        private void SendOpenPositions(Account acc, PositionEventArgs evt)
        {
            if (acc == null) return;
            try
            {
                // Copy the event values by-value up front (NT8 background-thread
                // guidance — the args can race ahead while we process). qty==0 or
                // Flat means the just-updated instrument went flat (close/remove).
                string evtRoot = null;
                MarketPosition evtMp = MarketPosition.Flat;
                int evtQty = 0;
                double evtAvg = 0;
                if (evt != null && evt.Position != null)
                {
                    try { evtRoot = evt.Position.Instrument.MasterInstrument.Name; } catch { }
                    evtMp = evt.MarketPosition;
                    evtQty = evt.Quantity;
                    evtAvg = evt.AveragePrice;
                }

                var list = new List<object>();
                bool evtSeen = false;
                // Background-thread safe: lock the Positions collection while
                // enumerating (NT8 mutates it from worker threads).
                lock (acc.Positions)
                {
                    foreach (Position pos in acc.Positions)
                    {
                        if (pos == null) continue;
                        string root = "";
                        try { root = pos.Instrument.MasterInstrument.Name; } catch { }

                        MarketPosition mp = pos.MarketPosition;
                        int qty = pos.Quantity;
                        double avg = pos.AveragePrice;
                        // Override the just-updated instrument with the SETTLED
                        // event values (its collection avg is still stale here).
                        if (evtRoot != null && root == evtRoot)
                        {
                            mp = evtMp; qty = evtQty; avg = evtAvg; evtSeen = true;
                        }
                        if (mp == MarketPosition.Flat || qty == 0)
                            continue; // skip flat / closed entries
                        list.Add(new Dictionary<string, object>
                        {
                            ["symbol"]    = root,
                            ["side"]      = (mp == MarketPosition.Long) ? "long" : "short",
                            ["quantity"]  = qty,
                            ["avg_price"] = avg
                        });
                    }
                }
                // Edge: a brand-new position may not be in acc.Positions at the
                // event instant — add it from the settled event values so we
                // never under-report the just-opened position.
                if (evtRoot != null && !evtSeen && evtMp != MarketPosition.Flat && evtQty != 0)
                {
                    list.Add(new Dictionary<string, object>
                    {
                        ["symbol"]    = evtRoot,
                        ["side"]      = (evtMp == MarketPosition.Long) ? "long" : "short",
                        ["quantity"]  = evtQty,
                        ["avg_price"] = evtAvg
                    });
                }

                var payload = new Dictionary<string, object>
                {
                    ["account"]   = acc.Name,
                    ["positions"] = list
                };
                WriteEnvelope("positions", payload);
                LogInfo("VLTraderTCPClient: sent positions snapshot account=" + acc.Name + " count=" + list.Count);
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: SendOpenPositions failed: " + ex.Message);
            }
        }

        // Account.PositionUpdate handler — fires on ANY position change for the
        // subscribed account, INCLUDING a manual open/close in NT8. Raised on a
        // background thread; we pass the event into SendOpenPositions so the
        // just-updated instrument uses the SETTLED event average (the live
        // acc.Positions average lags the fill at this instant — the stale-entry
        // bug). A close/flat arrives as MarketPosition.Flat or Quantity 0.
        private void OnPositionUpdate(object sender, PositionEventArgs e)
        {
            var acc = (sender as Account) ?? account;
            // Emit for the account that actually changed (sender) — PositionUpdate is
            // now hooked on ALL SIM accounts, so this can fire for a non-active account.
            try { SendOpenPositions(acc, e); }
            catch (Exception ex) { LogWarn("VLTraderTCPClient: OnPositionUpdate failed: " + ex.Message); }
            // CLASS-27 (2026-08-31 netting-orphan): the instant a position goes
            // FLAT — whether closed by an SL/TP leg, a manual flatten, or NETTED
            // by an opposite-side ENTRY fill — sweep EVERY tracked protective
            // bracket for this (account, instrument). A netting close emits no
            // exit-order fill, so the old position's SL/TP would otherwise rest
            // as orphans and re-enter a flat account (live proof 2026-08-31
            // 13:35:47: the S1 stop fired 26 minutes after its long was netted).
            try
            {
                if (e != null && e.MarketPosition == MarketPosition.Flat)
                {
                    string flatRoot = null;
                    try { if (e.Position != null) flatRoot = e.Position.Instrument.MasterInstrument.Name; } catch { }
                    if (!string.IsNullOrEmpty(flatRoot))
                    {
                        CancelAllBracketsFor(flatRoot, acc != null ? acc.Name : "");
                        // REVIEW-SPLIT-2 P2-2: drop the account-ownership mapping HERE,
                        // where the position has already settled. In the exit-fill handler
                        // e.Order.Account.Positions can still show the pre-fill quantity,
                        // which wrongly kept the mapping alive past a full close.
                        lock (posAcctLock)
                        {
                            Account owner;
                            if (positionAccountBySymbol.TryGetValue(flatRoot, out owner)
                                && owner != null && acc != null
                                && string.Equals(owner.Name, acc.Name, StringComparison.OrdinalIgnoreCase))
                            {
                                positionAccountBySymbol.Remove(flatRoot);
                            }
                        }
                    }
                }
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: netting-flat bracket sweep failed: " + ex.Message);
            }
        }

        // CLASS-27 (2026-08-31): cancel EVERY tracked bracket for (account,
        // instrument). Used when the account went flat by NETTING (an
        // opposite-side entry fill) — there is no exit order whose name can
        // route us, so sweep by (root, account) instead of by signal id.
        private void CancelAllBracketsFor(string rootSymbol, string acctName)
        {
            if (string.IsNullOrEmpty(rootSymbol)) return;
            List<PlacedBracket> victims = new List<PlacedBracket>();
            List<string> keysToRemove = new List<string>();
            lock (signalMapLock)
            {
                foreach (var kv in placedBrackets)
                {
                    var cand = kv.Value;
                    if (cand == null) continue;
                    string candRoot = "";
                    string candAcct = "";
                    try { candRoot = cand.Instrument.MasterInstrument.Name; } catch { }
                    try { candAcct = cand.Account != null ? cand.Account.Name : ""; } catch { }
                    if (string.Equals(candRoot, rootSymbol, StringComparison.OrdinalIgnoreCase)
                        && (string.IsNullOrEmpty(acctName) || string.Equals(candAcct, acctName, StringComparison.OrdinalIgnoreCase)))
                    {
                        victims.Add(cand);
                        keysToRemove.Add(kv.Key);
                    }
                }
                if (victims.Count == 0) return;
                int cancelled = 0;
                foreach (var pb in victims)
                {
                    Account ba = pb.Account ?? account;
                    var toCancel = new List<Order>();
                    if (pb.SlOrder != null) toCancel.Add(pb.SlOrder);
                    if (pb.TpOrder != null) toCancel.Add(pb.TpOrder);
                    // P0-2: the netting-flat sweep must cancel leg 2 too.
                    if (pb.SlOrder2 != null) toCancel.Add(pb.SlOrder2);
                    if (pb.TpOrder2 != null) toCancel.Add(pb.TpOrder2);
                    if (toCancel.Count > 0 && ba != null)
                    {
                        try
                        {
                            ba.Cancel(toCancel);
                            cancelled += toCancel.Count;
                        }
                        catch (Exception ex)
                        {
                            LogWarn("VLTraderTCPClient: netting-flat bracket cancel failed: " + ex.Message);
                        }
                    }
                }
                foreach (var k in keysToRemove)
                {
                    placedBrackets.Remove(k);
                }
                LogInfo("VLTraderTCPClient: netting-flat bracket sweep cancelled " + cancelled
                        + " leg(s) for " + rootSymbol + " (" + victims.Count + " bracket(s))");
            }
        }

        // Emit the accounts_list frame reporting all available NT accounts with
        // SIM detection. Fired on connect so the Go server can populate the UI
        // account selector. Also used on account list change (if NT fires an event).
        // WriteEnvelope no-ops if not yet connected.
        // Handler for when accounts become available
        private void OnAccountStatusUpdate(object sender, AccountStatusEventArgs e)
        {
            LogInfo(string.Format("@@@ OnAccountStatusUpdate FIRED: {0}, status={1}", e.Account.Name, e.Status));
            SendAccountsList();
        }

        private bool IsRealAccount(Account a)
        {
            if (a == null) return false;

            // Skip NT8 internal/test/bracket accounts (auto-generated).
            if (a.Name.StartsWith("FTPROPLUSM") || a.Name.StartsWith("FTPROPLUS") ||
                a.Name.StartsWith("TAKEPROFIT") || a.Name.StartsWith("TDFYSL") ||
                a.Name.StartsWith("TDFYG") || a.Name.StartsWith("MFFU"))
                return false;

            // Per NinjaTrader staff: filter to accounts on a FULLY CONNECTED connection.
            // A connection is "green/fully connected" when BOTH Status and PriceStatus
            // are ConnectionStatus.Connected (one can be up without the other).
            try
            {
                if (a.Connection == null) return false;

                // Check both Status and PriceStatus (per NT staff guidance).
                // ConnectionStatus.Connected is the enum value we're matching.
                var connStatus = a.Connection.Status;
                var priceStatus = a.Connection.PriceStatus;

                // Only include if BOTH are Connected (fully green in NT8 Control Center)
                bool isConnected = (connStatus.ToString() == "Connected") &&
                                  (priceStatus.ToString() == "Connected");

                if (!isConnected) return false;

                // If Status && PriceStatus filter isn't enough and Backtest/Playback
                // still show as green, add a provider/name narrowing (not needed if
                // only real accounts are green).
                return true;
            }
            catch (Exception ex)
            {
                LogWarn(string.Format("@@@ IsRealAccount check failed for {0}: {1}", a.Name, ex.Message));
                return false;
            }
        }

        private void SendAccountsList()
        {
            LogInfo("@@@ SendAccountsList START");
            var accountsList = new List<object>();
            try
            {
                lock (Account.All)
                {
                    LogInfo(string.Format("@@@ Account.All.Count={0}", Account.All.Count));
                    foreach (var a in Account.All)
                    {
                        if (!IsRealAccount(a)) continue;
                        if (accountsList.Count < 5)  // Log first 5
                            LogInfo(string.Format("@@@   account[{0}]: {1}", accountsList.Count, a.Name));
                        accountsList.Add(new Dictionary<string, object>
                        {
                            ["name"]   = a.Name,
                            ["is_sim"] = IsSimAccount(a)
                        });
                    }
                }
            }
            catch (Exception ex)
            {
                LogWarn(string.Format("@@@ lock failed: {0}", ex.Message));
                return;
            }

            LogInfo(string.Format("@@@ built list with {0} accounts, calling WriteEnvelope", accountsList.Count));
            LogInfo(string.Format("@@@ stream status: {0}", stream == null ? "NULL" : "CONNECTED"));
            var payload = new Dictionary<string, object> { ["accounts"] = accountsList };
            try
            {
                WriteEnvelope("accounts_list", payload);
                LogInfo(string.Format("@@@ WriteEnvelope succeeded, sent {0} accounts", accountsList.Count));
            }
            catch (Exception ex)
            {
                LogWarn(string.Format("@@@ WriteEnvelope FAILED: {0}", ex.Message));
            }
        }

        // Plan 4.11 — emit the real NT SIM account balance as an account_balance
        // frame (replaces the Go-side $50k mock). Fired on (re)connect and on
        // AccountItemUpdate. WriteEnvelope no-ops if not yet connected.
        //
        // FLAG: NT8 API — account.Get(AccountItem.X, Currency.UsDollar) and the
        // AccountItem enum (CashValue/BuyingPower/RealizedProfitLoss/
        // UnrealizedProfitLoss) match NT8 8.1. If the operator's build differs,
        // adjust the enum names here (compile error will point right at it).
        // NetLiquidation is derived (cash + unrealized) rather than via a
        // possibly-absent AccountItem.NetLiquidation.
        private void SendAccountBalance()
        {
            SendAccountBalanceFor(account);
        }

        // Emit ONE account's balance snapshot. Parameterized so the heartbeat can
        // stream EVERY account (not just the active one) — see SendAllAccountBalances.
        private void SendAccountBalanceFor(Account a)
        {
            if (a == null) return;
            try
            {
                double cash       = a.Get(AccountItem.CashValue, Currency.UsDollar);
                double buying     = a.Get(AccountItem.BuyingPower, Currency.UsDollar);
                double realized   = a.Get(AccountItem.RealizedProfitLoss, Currency.UsDollar);
                double unrealized = a.Get(AccountItem.UnrealizedProfitLoss, Currency.UsDollar);
                var payload = new Dictionary<string, object>
                {
                    ["account"]         = a.Name,
                    ["cash_value"]      = cash,
                    ["buying_power"]    = buying,
                    ["realized_pnl"]    = realized,
                    ["unrealized_pnl"]  = unrealized,
                    ["net_liquidation"] = cash + unrealized
                };
                WriteEnvelope("account_balance", payload);
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: account_balance emit failed for "
                        + (a != null ? a.Name : "<null>") + ": " + ex.Message);
            }
        }

        // Stream EVERY SIM account's balance, not just the connection's active one.
        // The active-account-only stream (AccountItemUpdate is hooked on `account`
        // alone) left a SECOND trader on another sub-account reading a STALE/absent
        // balance — the dashboard showed one account for everyone ("stale on 1
        // account"). account.Get() reads any Account.All member's live values on
        // demand (subscription is only needed for change callbacks), so polling them
        // all here keeps every account's snapshot <=1 heartbeat fresh on the Go side,
        // where GetBalance serves each trader its OWN bound account.
        private void SendAllAccountBalances()
        {
            lock (Account.All)
            {
                foreach (var a in Account.All)
                {
                    if (IsSimAccount(a)) SendAccountBalanceFor(a);
                }
            }
        }

        // Stream EVERY SIM account's open positions, not just the active one — the
        // positions analogue of SendAllAccountBalances. PositionUpdate is hooked on
        // the active account only (P4), so a trader bound to a NON-active sub-account
        // otherwise reads its own book as EMPTY (Go PositionsFor(boundAccount) !ok),
        // goes blind, and COMPOUNDS (its never-compound / same-side / max-position
        // gates all read that empty snapshot and pass). SendOpenPositions(acc) already
        // tags the frame with acc.Name and enumerates acc.Positions under lock, so it
        // is account-correct per call; here we just call it for every SIM account.
        private void SendAllOpenPositions()
        {
            lock (Account.All)
            {
                foreach (var a in Account.All)
                {
                    if (IsSimAccount(a)) SendOpenPositions(a);
                }
            }
        }

        private void OnAccountItemUpdate(object sender, AccountItemEventArgs e)
        {
            // Emit only on cash/PnL changes to limit frame churn.
            if (e.AccountItem == AccountItem.CashValue
                || e.AccountItem == AccountItem.BuyingPower
                || e.AccountItem == AccountItem.RealizedProfitLoss
                || e.AccountItem == AccountItem.UnrealizedProfitLoss)
            {
                SendAccountBalance();
            }
        }

        private void SendAck(string acks)
        {
            WriteEnvelope("ack", new Dictionary<string, object> { ["acks"] = acks });
        }

        /// <summary>
        /// Plan 4.4 Stage 1 — entry point for VLBarsSubscriptionManager.
        /// Routes its bar frames (bars_historical, bar_update) through the
        /// SAME WriteEnvelope path the signal/fill/heartbeat code uses, so
        /// bar bytes inherit the writeLock and the hand-rolled encoder.
        /// </summary>
        internal void SendFrame(string type, Dictionary<string, object> payload)
        {
            WriteEnvelope(type, payload);
        }

        private void WriteEnvelope(string type, Dictionary<string, object> payload)
        {
            if (stream == null) return;
            try
            {
                string json = EncodeFrame(type, payload);
                byte[] body = Encoding.UTF8.GetBytes(json);
                if (body.Length > MAX_FRAME_BYTES)
                {
                    LogWarn("VLTraderTCPClient: outgoing frame too large " + body.Length);
                    return;
                }
                byte[] header = new byte[]
                {
                    (byte)((body.Length >> 24) & 0xFF),
                    (byte)((body.Length >> 16) & 0xFF),
                    (byte)((body.Length >> 8)  & 0xFF),
                    (byte)( body.Length        & 0xFF)
                };
                lock (writeLock)
                {
                    stream.Write(header, 0, 4);
                    stream.Write(body, 0, body.Length);
                    stream.Flush();
                }
            }
            catch (Exception ex)
            {
                LogWarn("VLTraderTCPClient: write err " + ex.Message);
            }
        }

        // === Heartbeat loop (spec L4408: 30s interval) ===
        private void RunHeartbeatLoop(CancellationToken ct)
        {
            int beatCount = 0;
            while (!ct.IsCancellationRequested)
            {
                Thread.Sleep(HEARTBEAT_INTERVAL_MS);
                if (ct.IsCancellationRequested) return;
                WriteEnvelope("heartbeat", new Dictionary<string, object>
                {
                    ["build_id"] = VL_BUILD_ID,
                });

                // F12 — the PERIODIC working-order snapshot. It rides this loop
                // because HEARTBEAT_INTERVAL_MS and ORDER_SNAPSHOT_INTERVAL_MS are
                // both 30s; a second thread on the same period would buy nothing
                // but another thing to get out of step. If the periods ever
                // diverge this needs its own timer, and the constants sit beside
                // each other so that is visible.
                try { SendOrderSnapshot(account, "periodic"); }
                catch (Exception exSnap)
                {
                    LogWarn("RunHeartbeatLoop: order_snapshot failed (will retry): " + exSnap.Message);
                }

                // Periodic re-emit of accounts every 3 heartbeats (90s) to ensure
                // Go always has them after a restart (not just on connect/change)
                beatCount++;
                if (beatCount % 3 == 0)
                {
                    SendAccountsList();
                }

                // Periodic poll of account balance every heartbeat (30s) — Tradovate's
                // AccountItemUpdate doesn't fire reliably, so poll to ensure real balance
                // populates even if AccountItemUpdate misses + connect-seed failed.
                // Poll EVERY SIM account (not just the active one) so concurrent traders
                // on different sub-accounts each get a fresh balance — the Go side keys
                // snapshots per-account and serves each trader its own bound account.
                try
                {
                    SendAllAccountBalances();
                }
                catch (Exception ex)
                {
                    LogWarn($"RunHeartbeatLoop: SendAllAccountBalances poll failed (will retry): {ex.Message}");
                }

                // Periodic poll of open positions for EVERY SIM account (30s) — the
                // positions analogue of the balance poll above. PositionUpdate is
                // hooked on the active account only, so a trader on a non-active
                // sub-account would otherwise never receive a positions frame and read
                // its book as empty (BUG A: blindness → compounding). This closes it.
                try
                {
                    SendAllOpenPositions();
                }
                catch (Exception ex)
                {
                    LogWarn($"RunHeartbeatLoop: SendAllOpenPositions poll failed (will retry): {ex.Message}");
                }
            }
        }

        // === Log helpers — NT8 Log API varies slightly across versions; isolate ===
        private static void LogInfo(string msg)
        {
            try { NinjaScript.Log(msg, LogLevel.Information); } catch { }
        }

        private static void LogWarn(string msg)
        {
            try { NinjaScript.Log(msg, LogLevel.Warning); } catch { }
        }

        // ==============================================================
        // Hand-rolled JSON encoder + parser (no external deps).
        // NT8 user AddOns cannot resolve Newtonsoft.Json reliably, so we
        // implement only what the 4-message wire protocol needs:
        // {type, payload} envelopes with flat snake_case payloads.
        // On-wire bytes must match Go encoding/json: snake_case keys,
        // invariant-culture numbers, compact (no pretty-print), UTF-8.
        // ==============================================================

        private static string EncodeFrame(string type, Dictionary<string, object> payload)
        {
            var sb = new StringBuilder();
            sb.Append('{');
            sb.Append("\"type\":");
            AppendString(sb, type);
            sb.Append(",\"payload\":");
            AppendObject(sb, payload);
            sb.Append('}');
            return sb.ToString();
        }

        private static void AppendObject(StringBuilder sb, Dictionary<string, object> obj)
        {
            sb.Append('{');
            if (obj != null)
            {
                bool first = true;
                foreach (var kv in obj)
                {
                    if (!first) sb.Append(',');
                    first = false;
                    AppendString(sb, kv.Key);
                    sb.Append(':');
                    AppendValue(sb, kv.Value);
                }
            }
            sb.Append('}');
        }

        private static void AppendValue(StringBuilder sb, object v)
        {
            if (v == null) { sb.Append("null"); return; }
            if (v is string s) { AppendString(sb, s); return; }
            if (v is bool b) { sb.Append(b ? "true" : "false"); return; }
            if (v is int i) { sb.Append(i.ToString(CultureInfo.InvariantCulture)); return; }
            if (v is long l) { sb.Append(l.ToString(CultureInfo.InvariantCulture)); return; }
            if (v is double d) { sb.Append(d.ToString("R", CultureInfo.InvariantCulture)); return; }
            if (v is float f) { sb.Append(((double)f).ToString("R", CultureInfo.InvariantCulture)); return; }
            if (v is decimal m) { sb.Append(m.ToString(CultureInfo.InvariantCulture)); return; }
            if (v is Dictionary<string, object> nested) { AppendObject(sb, nested); return; }
            // Plan 4.4 bar-payload fix: List<object> / arrays. Must come AFTER
            // string + Dictionary (string is IEnumerable<char>; Dictionary is
            // IEnumerable<KeyValuePair>). Without this, bars_historical /
            // bar_update payloads (["bars"] = List<object>) get stringified
            // via the .ToString() fallback to "System.Collections.Generic.
            // List`1[System.Object]", and Go rejects them with
            // "cannot unmarshal string into ... []Bar".
            if (v is System.Collections.IEnumerable e)
            {
                sb.Append('[');
                bool firstItem = true;
                foreach (var item in e)
                {
                    if (!firstItem) sb.Append(',');
                    firstItem = false;
                    AppendValue(sb, item);
                }
                sb.Append(']');
                return;
            }
            AppendString(sb, v.ToString());
        }

        private static void AppendString(StringBuilder sb, string s)
        {
            sb.Append('"');
            if (s != null)
            {
                foreach (char c in s)
                {
                    switch (c)
                    {
                        case '"':  sb.Append("\\\""); break;
                        case '\\': sb.Append("\\\\"); break;
                        case '\n': sb.Append("\\n"); break;
                        case '\r': sb.Append("\\r"); break;
                        case '\t': sb.Append("\\t"); break;
                        case '\b': sb.Append("\\b"); break;
                        case '\f': sb.Append("\\f"); break;
                        default:
                            if (c < 0x20) sb.AppendFormat("\\u{0:x4}", (int)c);
                            else sb.Append(c);
                            break;
                    }
                }
            }
            sb.Append('"');
        }

        // === Field-extraction helpers (typed reads from parsed dictionary) ===
        private static string GetString(Dictionary<string, object> obj, string key)
        {
            if (obj != null && obj.TryGetValue(key, out var v) && v != null) return v.ToString();
            return null;
        }

        private static double GetDouble(Dictionary<string, object> obj, string key)
        {
            if (obj != null && obj.TryGetValue(key, out var v) && v != null)
            {
                if (v is double d) return d;
                if (v is long l) return (double)l;
                if (v is int i) return (double)i;
                if (v is decimal m) return (double)m;
                return Convert.ToDouble(v, CultureInfo.InvariantCulture);
            }
            return 0.0;
        }

        private static int GetInt(Dictionary<string, object> obj, string key)
        {
            if (obj != null && obj.TryGetValue(key, out var v) && v != null)
            {
                if (v is long l) return (int)l;
                if (v is int i) return i;
                if (v is double d) return (int)d;
                return Convert.ToInt32(v, CultureInfo.InvariantCulture);
            }
            return 0;
        }

        // A2 (G1) — parse the op seq (uint64 on the wire) as a long; 0 when absent.
        private static long GetLong(Dictionary<string, object> obj, string key)
        {
            if (obj != null && obj.TryGetValue(key, out var v) && v != null)
            {
                if (v is long l) return l;
                if (v is int i) return i;
                if (v is double d) return (long)d;
                return Convert.ToInt64(v, CultureInfo.InvariantCulture);
            }
            return 0;
        }

        // === Recursive-descent JSON parser ===
        private class JsonParser
        {
            private readonly string s;
            private int pos;

            public JsonParser(string input)
            {
                s = input ?? string.Empty;
                pos = 0;
            }

            public object Parse()
            {
                SkipWs();
                return ParseValue();
            }

            private object ParseValue()
            {
                SkipWs();
                if (pos >= s.Length) throw new Exception("unexpected EOF");
                char c = s[pos];
                if (c == '{') return ParseObject();
                if (c == '"') return ParseString();
                if (c == 't' || c == 'f') return ParseBool();
                if (c == 'n') return ParseNull();
                if (c == '-' || (c >= '0' && c <= '9')) return ParseNumber();
                if (c == '[') return ParseArray();
                throw new Exception("unexpected char '" + c + "' at " + pos);
            }

            private Dictionary<string, object> ParseObject()
            {
                var result = new Dictionary<string, object>();
                pos++; // consume '{'
                SkipWs();
                if (pos < s.Length && s[pos] == '}') { pos++; return result; }
                while (true)
                {
                    SkipWs();
                    string key = ParseString();
                    SkipWs();
                    if (pos >= s.Length || s[pos] != ':') throw new Exception("expected ':' at " + pos);
                    pos++; // consume ':'
                    SkipWs();
                    object value = ParseValue();
                    result[key] = value;
                    SkipWs();
                    if (pos < s.Length && s[pos] == ',') { pos++; continue; }
                    if (pos < s.Length && s[pos] == '}') { pos++; return result; }
                    throw new Exception("expected ',' or '}' at " + pos);
                }
            }

            private string ParseString()
            {
                if (pos >= s.Length || s[pos] != '"') throw new Exception("expected string at " + pos);
                pos++; // consume opening '"'
                var sb = new StringBuilder();
                while (pos < s.Length && s[pos] != '"')
                {
                    if (s[pos] == '\\' && pos + 1 < s.Length)
                    {
                        char esc = s[pos + 1];
                        switch (esc)
                        {
                            case '"':  sb.Append('"'); pos += 2; break;
                            case '\\': sb.Append('\\'); pos += 2; break;
                            case '/':  sb.Append('/'); pos += 2; break;
                            case 'n':  sb.Append('\n'); pos += 2; break;
                            case 'r':  sb.Append('\r'); pos += 2; break;
                            case 't':  sb.Append('\t'); pos += 2; break;
                            case 'b':  sb.Append('\b'); pos += 2; break;
                            case 'f':  sb.Append('\f'); pos += 2; break;
                            case 'u':
                                if (pos + 5 < s.Length)
                                {
                                    sb.Append((char)Convert.ToInt32(s.Substring(pos + 2, 4), 16));
                                    pos += 6;
                                }
                                else
                                {
                                    pos++;
                                }
                                break;
                            default: sb.Append(esc); pos += 2; break;
                        }
                    }
                    else
                    {
                        sb.Append(s[pos]);
                        pos++;
                    }
                }
                if (pos >= s.Length) throw new Exception("unterminated string");
                pos++; // consume closing '"'
                return sb.ToString();
            }

            private object ParseNumber()
            {
                int start = pos;
                if (s[pos] == '-') pos++;
                while (pos < s.Length &&
                       (char.IsDigit(s[pos]) || s[pos] == '.' ||
                        s[pos] == 'e' || s[pos] == 'E' ||
                        s[pos] == '+' || s[pos] == '-'))
                {
                    pos++;
                }
                string num = s.Substring(start, pos - start);
                if (num.IndexOfAny(new[] { '.', 'e', 'E' }) >= 0)
                {
                    return double.Parse(num, CultureInfo.InvariantCulture);
                }
                if (long.TryParse(num, NumberStyles.Integer, CultureInfo.InvariantCulture, out long l))
                {
                    return l;
                }
                return double.Parse(num, CultureInfo.InvariantCulture);
            }

            private bool ParseBool()
            {
                if (pos + 4 <= s.Length && s.Substring(pos, 4) == "true") { pos += 4; return true; }
                if (pos + 5 <= s.Length && s.Substring(pos, 5) == "false") { pos += 5; return false; }
                throw new Exception("invalid bool at " + pos);
            }

            private object ParseNull()
            {
                if (pos + 4 <= s.Length && s.Substring(pos, 4) == "null") { pos += 4; return null; }
                throw new Exception("invalid null at " + pos);
            }

            private List<object> ParseArray()
            {
                var result = new List<object>();
                pos++; // consume '['
                SkipWs();
                if (pos < s.Length && s[pos] == ']') { pos++; return result; }
                while (true)
                {
                    SkipWs();
                    result.Add(ParseValue());
                    SkipWs();
                    if (pos < s.Length && s[pos] == ',') { pos++; continue; }
                    if (pos < s.Length && s[pos] == ']') { pos++; return result; }
                    throw new Exception("expected ',' or ']' at " + pos);
                }
            }

            private void SkipWs()
            {
                while (pos < s.Length &&
                       (s[pos] == ' ' || s[pos] == '\t' ||
                        s[pos] == '\n' || s[pos] == '\r'))
                {
                    pos++;
                }
            }
        }
    }
}
