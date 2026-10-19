// VLBarsSubscriptionManager.cs — Plan 4.4 Stage 1 (C# side of the NT8 bar feed).
//
// Subscribes to NT8 bars across multiple timeframes (native, one BarsRequest
// per timeframe — operator-locked Option 1: no Go-side aggregation), and
// streams `bars_historical` + `bar_update` frames over the EXISTING Plan 1.5
// TCP channel managed by VLTraderTCPClient.
//
// Architecture:
//   - Isolated in a separate class so the proven signal/fill/heartbeat path
//     in VLTraderTCPClient.cs stays byte-stable (ADR-007 spirit). The main
//     AddOn gets only: a field, a constructor call, two switch cases.
//   - Sends through VLTraderTCPClient.SendFrame() (now internal), which
//     reuses the existing writeLock + hand-rolled JSON encoder. NO second
//     unsynchronized socket writer.
//
// NT8 gotchas handled (each one is a separate "bug if missed"):
//   1. TIMEZONE  — bars.GetTime() returns LOCAL time. We convert to UTC
//                  epoch ms using bars.TradingHours.TimeZoneInfo before emit.
//   2. MULTI-BAR — a single .Update event can touch MinIndex..MaxIndex. We
//                  iterate that whole range and emit every bar (NEVER just
//                  the last bar).
//   3. HISTORICAL-DEDUP — track each subscription's last-emitted bar time so
//                  reconnect/restart doesn't re-emit bars the Go side
//                  already has.
//   4. RECONNECT — Connection.ConnectionStatusUpdate is hooked; on a
//                  reconnect, all active BarsRequests are disposed and
//                  recreated (NT8 silently stops updating them otherwise).
//   5. JSON      — no Newtonsoft. Payloads are built as
//                  Dictionary<string, object> and serialized by the same
//                  encoder VLTraderTCPClient uses for signal/fill frames.
//   6. WRITELOCK — every send goes through VLTraderTCPClient.SendFrame()
//                  which already takes writeLock; bar frames cannot
//                  interleave with signal/fill/heartbeat bytes on the wire.
//
// Spec: docs/superpowers/plans/2026-05-22-nq-databento-ninjatrader.md
//       (Plan 4.4 Deep Spec — "Wire protocol extension"; "NT8 SDK gotchas")
// Protocol: ninjascript/vltrader_tcp_PROTOCOL.md (frames 5-8)

#region Using declarations
using System;
using System.Collections.Generic;
using System.Threading;
using NinjaTrader.Cbi;
using NinjaTrader.Data;
#endregion

namespace NinjaTrader.NinjaScript.AddOns
{
    /// <summary>
    /// Owns the BarsRequest subscriptions and the per-subscription state needed
    /// to stream OHLCV over the VLTrader TCP wire. Constructed by
    /// VLTraderTCPClient at AddOn-Active time; disposed at AddOn-Terminated.
    /// </summary>
    public class VLBarsSubscriptionManager
    {
        // === Inbound dependencies (injected by VLTraderTCPClient) ===

        /// <summary>
        /// Sends a frame through the parent's writeLock + hand-rolled encoder.
        /// We hold a delegate rather than a back-reference so this class never
        /// reaches into the parent's network state directly.
        /// </summary>
        private readonly Action<string, Dictionary<string, object>> sendFrame;

        private readonly Action<string> logInfo;
        private readonly Action<string> logWarn;

        // === Subscription registry ===

        /// <summary>
        /// Active subscriptions keyed by "SYMBOL|TIMEFRAME". Synchronized via
        /// subsLock for ADD/REMOVE; individual entries are owned by NT8's
        /// data thread once the BarsRequest is firing.
        /// </summary>
        private readonly Dictionary<string, BarsRequestEntry> active =
            new Dictionary<string, BarsRequestEntry>();
        private readonly object subsLock = new object();

        // Default historical depth when bars_subscribe.bars_back is omitted,
        // AND the lookback used by the reconnect-rebuild path (OnConnectionReconnected).
        // 2000 (~33 ETH hours at 1m) — was 500 (~8.3h). 500 was SHORTER than an
        // overnight feed outage, so a reconnect-rebuilt request couldn't even reach
        // back far enough to backfill last night's hole. 2000 spans an overnight gap
        // so the fresh BarsRequest re-reads NT8's now-complete DB across the whole
        // hole. Matches the Go side's defaultAutoBarsBack. Coarse timeframes are
        // capped by the provider, so this is safe across the 14-tf set.
        private const int DEFAULT_BARS_BACK = 2000;

        // Extended/overnight (Globex) session template. Applied to every
        // BarsRequest so the series includes the EVENING session and .Update
        // keeps firing past the 16:00 CT RTH close. NT8's default template for
        // CME US Index Futures (ES/MES/MNQ/NQ). Without it the request inherits
        // an RTH-bounded template and the bar feed FREEZES at the daily session
        // close (observed 2026-06-01: stuck at 16:00 CT / 30523.75 while the
        // live evening market ran ~30389, leaving the chart + AI stale).
        private const string BARS_TRADING_HOURS = "CME US Index Futures ETH";

        // Stall watchdog. NT8 can silently stop firing .Update (e.g. across a
        // session boundary) with NO event — the old code logged nothing, so a
        // stall was invisible. We stamp each entry on every emit; the watchdog
        // logs the most-stale bar age each tick (visibility) and, if it exceeds
        // WATCHDOG_STALL_MS, recreates all requests via the existing
        // OnConnectionReconnected() path. The threshold is deliberately larger
        // than the daily 16:00-17:00 CT maintenance halt (60 min) so the
        // EXPECTED daily gap never false-triggers a recreate.
        private const long WATCHDOG_PERIOD_MS = 15000L;        // check cadence (15s) — fast guard needs frequent ticks
        private const long WATCHDOG_STALL_MS  = 75L * 60000L;  // 75 min backstop (> 60-min daily halt) — mid-session silent death
        // Fast guard: a fresh (re)subscribe seeds historical but no LIVE .Update
        // arrives — the proven post-restart dead window (was 75 min until the slow
        // watchdog). Recreate within ~FAST_STALL_MS, capped at FAST_MAX_ATTEMPTS so a
        // genuine no-data window (daily halt / closed market) can't churn recreates.
        private const long FAST_STALL_MS     = 20000L;         // no live .Update within 20s of (re)subscribe => dead window
        private const int  FAST_MAX_ATTEMPTS = 3;              // cap fast recreates per dead window
        private Timer watchdogTimer;
        private long  lastWatchdogRecreateUtcMs = 0;
        private long  lastFastRecreateUtcMs = 0;
        private int   fastRecreateAttempts = 0;
        private long  lastAgeLogUtcMs = 0;
        private readonly object watchdogLock = new object();

        public VLBarsSubscriptionManager(
            Action<string, Dictionary<string, object>> sendFrame,
            Action<string> logInfo,
            Action<string> logWarn)
        {
            this.sendFrame = sendFrame;
            this.logInfo   = logInfo  ?? (s => { });
            this.logWarn   = logWarn ?? (s => { });

            // Start the stall watchdog (logs bar age each tick; recreates on a
            // genuine stall). Ticks do nothing until a subscription is streaming.
            watchdogTimer = new Timer(WatchdogTick, null, WATCHDOG_PERIOD_MS, WATCHDOG_PERIOD_MS);
        }

        // ==============================================================
        // Inbound frame handlers — called by VLTraderTCPClient.HandleFrame
        // ==============================================================

        /// <summary>
        /// Handle a `bars_subscribe` envelope. For each requested timeframe
        /// the manager opens (or reuses) one BarsRequest against NT8's data
        /// engine. Idempotent: re-subscribing returns immediately.
        /// </summary>
        public void HandleBarsSubscribe(Dictionary<string, object> payload)
        {
            if (payload == null)
            {
                logWarn("VLBarsSubscriptionManager: bars_subscribe payload missing");
                return;
            }

            string symbol = TryGetString(payload, "symbol");
            if (string.IsNullOrEmpty(symbol))
            {
                logWarn("VLBarsSubscriptionManager: bars_subscribe missing symbol");
                return;
            }

            int barsBack = TryGetInt(payload, "bars_back", DEFAULT_BARS_BACK);
            if (barsBack <= 0) barsBack = DEFAULT_BARS_BACK;

            var timeframes = TryGetStringList(payload, "timeframes");
            if (timeframes == null || timeframes.Count == 0)
            {
                logWarn("VLBarsSubscriptionManager: bars_subscribe missing timeframes");
                return;
            }

            // Resolve the bare root ("MNQ") to the NT8 front-month
            // contract ("MNQ 06-26") via VLContractResolver. NT8's
            // Instrument.GetInstrument requires the qualified contract;
            // a bare root returns null. The resolver is date-derived so
            // it auto-rolls at the quarterly boundary (June → September
            // around 2026-06-11 with the 8-day pre-roll offset). The
            // canonical Go-side symbol stays "MNQ"; the contract suffix
            // exists only inside this GetInstrument call.
            string contract = VLContractResolver.ResolveFrontMonthContract(symbol);
            string how;
            var instrument = VLInstrumentLookup.Resolve(symbol, logWarn, out how);
            logInfo("VLBarsSubscriptionManager: resolved " + symbol + " -> " + contract
                    + " => " + (instrument != null ? VLInstrumentLookup.ContractName(instrument) : "<null>") + " (" + how + ")");
            if (instrument == null)
            {
                // Clear unresolved signal (Phase 2) — no silent freeze. Either a
                // non-quarterly root (energy CL/NG, metal GC/SI) whose front-month
                // roll resolution is deferred (it passed through unchanged, so
                // symbol == contract), or a contract not loaded in NT8's database.
                bool passthrough = string.Equals(symbol, contract, StringComparison.OrdinalIgnoreCase);
                string reason = passthrough
                    ? "root not a quarterly family; front-month roll resolution deferred (energy/metal)"
                    : "qualified contract '" + contract + "' not found in NT8 (not loaded?)";
                logWarn("VLBarsSubscriptionManager: instrument_unresolved symbol=" + symbol
                        + " resolved=" + contract + " — " + reason);
                // P5.3 — surface the failure Go-side (subscribe_error ack) so a
                // bad symbol fails loudly in the bot instead of only in the
                // NT8 Output window.
                sendFrame("subscribe_error", new Dictionary<string, object>
                {
                    ["symbol"] = symbol,
                    ["reason"] = reason
                });
                return;
            }

            // Phase 4 — emit the resolved instrument's REAL specs (NT8 ground
            // truth) so Go can cross-check the hardcoded tables / source them.
            // Synchronous DB lookup; safe immediately after GetInstrument resolves.
            try
            {
                var mi = instrument.MasterInstrument;
                sendFrame("instrument_info", new Dictionary<string, object>
                {
                    ["symbol"]      = mi.Name,
                    ["contract"]    = instrument.FullName,
                    ["point_value"] = mi.PointValue,
                    ["tick_size"]   = mi.TickSize,
                });
            }
            catch (Exception ex)
            {
                logWarn("VLBarsSubscriptionManager: instrument_info emit failed: " + ex.Message);
            }

            foreach (var tf in timeframes)
            {
                Subscribe(symbol, tf, barsBack, instrument);
            }

            // P5.3 — positive ack with the resolved front-month so the Go side
            // (and the owner API) sees the subscription state without the NT8
            // Output window. Sent after the Subscribe loop; per-timeframe
            // failures (unsupported tf) are logged above and don't veto the
            // symbol-level ack.
            // The ACK names the contract NT8 RESOLVED (from its expiry), never the
            // rolling literal — Go keys the roll detection on this name.
            sendFrame("subscribed", new Dictionary<string, object>
            {
                ["symbol"]            = symbol,
                ["resolved_contract"] = VLInstrumentLookup.ContractName(instrument)
            });
        }

        /// <summary>
        /// Handle a `bars_unsubscribe` envelope. Omitting timeframes disposes
        /// every subscription for the named symbol.
        /// </summary>
        public void HandleBarsUnsubscribe(Dictionary<string, object> payload)
        {
            if (payload == null)
            {
                logWarn("VLBarsSubscriptionManager: bars_unsubscribe payload missing");
                return;
            }

            string symbol = TryGetString(payload, "symbol");
            if (string.IsNullOrEmpty(symbol))
            {
                logWarn("VLBarsSubscriptionManager: bars_unsubscribe missing symbol");
                return;
            }

            var timeframes = TryGetStringList(payload, "timeframes");

            lock (subsLock)
            {
                var toRemove = new List<string>();
                foreach (var kv in active)
                {
                    if (!kv.Value.Symbol.Equals(symbol, StringComparison.OrdinalIgnoreCase))
                        continue;
                    if (timeframes != null && timeframes.Count > 0
                        && !timeframes.Contains(kv.Value.Timeframe))
                        continue;
                    toRemove.Add(kv.Key);
                }
                foreach (var key in toRemove)
                {
                    DisposeEntry(active[key]);
                    active.Remove(key);
                    logInfo("VLBarsSubscriptionManager: unsubscribed " + key);
                }

                // P5.3 — teardown ack (count of disposed symbol|timeframe
                // subscriptions; 0 means the symbol wasn't subscribed).
                sendFrame("unsubscribed", new Dictionary<string, object>
                {
                    ["symbol"]  = symbol,
                    ["removed"] = toRemove.Count
                });
            }
        }

        // ==============================================================
        // Per-subscription lifecycle
        // ==============================================================

        private void Subscribe(string symbol, string timeframe, int barsBack, Instrument instrument)
        {
            string key = (symbol + "|" + timeframe).ToUpperInvariant();

            lock (subsLock)
            {
                if (active.ContainsKey(key))
                {
                    // N4 (was N3 re-seed): the Go side re-subscribed to an
                    // already-active subscription — its in-memory BarCache was
                    // wiped on a Go PROCESS restart, but our BarsRequest survived
                    // (it is tied to NT8's data engine, not the Go TCP socket).
                    //
                    // The old N3 path merely re-emitted the EXISTING request's
                    // window without recreating it. That replays a FROZEN snapshot:
                    // a BarsRequest that straddled a feed outage (NT8 feed drops
                    // ~16:00, resumes the next afternoon) holds that hole forever
                    // and never backfills — even though NT8's own DB fills the gap
                    // from the provider's historical server. So a bot restart kept
                    // re-sending last night's hole.
                    //
                    // Now we DISPOSE + RECREATE: a fresh BarsRequest re-reads NT8's
                    // now-complete DB over the full barsBack lookback, so an
                    // overnight gap SELF-HEALS on the next bot restart. The Go-side
                    // mergeBarsByTime accepts the backfilled interior bars into the
                    // existing hole (it is a time-ordered union, not append-only).
                    // Cost: one full re-fetch per bot reconnect (heavier seed burst;
                    // the FAST_STALL fast-guard covers the brief no-live-Update window).
                    DisposeEntry(active[key]);
                    active.Remove(key);
                    logInfo("VLBarsSubscriptionManager: rebuilding " + key
                            + " on Go reconnect — fresh BarsRequest re-reads NT8 DB (gap self-heal)");
                    // fall through to the fresh-subscribe path below
                }

                BarsPeriod period;
                try
                {
                    period = MapTimeframe(timeframe);
                }
                catch (Exception ex)
                {
                    logWarn("VLBarsSubscriptionManager: unsupported timeframe " + timeframe + ": " + ex.Message);
                    return;
                }

                // FLAG: NT8 API — BarsRequest constructor + how to set BarsPeriod
                // + how to wire .Update vary slightly across NT8 8.0.x and
                // 8.1.x. The shape below matches the NT8 8.1.6 docs:
                //
                //   var req = new BarsRequest(instrument, barsBack);
                //   req.BarsPeriod = period;
                //   req.TradingHours = TradingHours.Get("CME US Index Futures ETH");
                //   req.Request((bars, err, msg) => { ... });
                //   req.Update += (s, e) => { ... };
                //
                // If the operator's NT8 build rejects this constructor, the
                // alternative is the two-argument (DateTime from, DateTime to)
                // form — flagged here so the compile error is immediately
                // diagnosable.
                BarsRequest request;
                try
                {
                    request = new BarsRequest(instrument, barsBack);
                    request.BarsPeriod = period;
                    // DO NOT MERGE (owner ruling 2026-09-11, bar-source wave). With no
                    // policy set here the request inherited NT8's global Merge policy,
                    // and under MergeBackAdjusted the historical series for MNQ 12-26
                    // was served ~290 pts below the live feed for the SAME minutes
                    // (research facts 16516009 live 29358.25 vs 16518205 replay
                    // 29068.25, 2026-09-10 22:37 CT). The replay and the live feed must
                    // be one price scale; a back-adjusted history is a different
                    // instrument wearing the same label.
                    request.MergePolicy = MergePolicy.DoNotMerge;
                    // Force the EXTENDED (overnight/Globex) session so the series
                    // includes the evening session and .Update keeps firing past
                    // the 16:00 CT RTH close (the freeze fix). Non-fatal if the
                    // template name is absent on this build — fall back to the
                    // instrument default rather than dropping the subscription.
                    try { request.TradingHours = TradingHours.Get(BARS_TRADING_HOURS); }
                    catch (Exception thEx)
                    {
                        logWarn("VLBarsSubscriptionManager: TradingHours.Get(\"" + BARS_TRADING_HOURS
                                + "\") failed for " + key + ": " + thEx.Message
                                + " — using instrument default trading hours");
                    }
                }
                catch (Exception ex)
                {
                    logWarn("VLBarsSubscriptionManager: BarsRequest ctor failed for "
                            + key + ": " + ex.Message);
                    return;
                }

                var entry = new BarsRequestEntry
                {
                    Key       = key,
                    Symbol    = symbol,
                    Timeframe = timeframe,
                    Contract  = VLInstrumentLookup.ContractName(instrument),
                    Request   = request,
                    LastEmittedTimeUtcMs = 0,
                    LastFinalizedTimeUtcMs = 0,
                    HistoricalSent = false,
                    SubscribedAtUtcMs = NowUtcMs(), // fast-guard: start the no-live-.Update clock now
                    LiveUpdateSeen = false
                };
                active[key] = entry;

                // Wire the streaming-update handler BEFORE firing the
                // historical Request — otherwise an event fired between the
                // historical load and the handler attach is lost.
                request.Update += (sender, args) => OnBarsUpdate(entry, args);

                logInfo("VLBarsSubscriptionManager: subscribing " + key + " barsBack=" + barsBack);

                // Fire the historical load.
                try
                {
                    // NT8 8.1: the callback's first arg is the BarsRequest
                    // itself (NOT a Bars collection). The actual Bars data
                    // hangs off request.Bars. We capture `entry` and read
                    // entry.Request.Bars inside EmitHistorical so the path
                    // is consistent with OnBarsUpdate.
                    request.Request((req, errorCode, errorMessage) =>
                    {
                        if (errorCode != ErrorCode.NoError)
                        {
                            logWarn("VLBarsSubscriptionManager: historical load failed for "
                                    + key + ": " + errorCode + " — " + errorMessage);
                            return;
                        }
                        EmitHistorical(entry);
                    });
                }
                catch (Exception ex)
                {
                    logWarn("VLBarsSubscriptionManager: Request() threw for "
                            + key + ": " + ex.Message);
                }
            }
        }

        private void DisposeEntry(BarsRequestEntry entry)
        {
            if (entry == null) return;
            try { entry.Request.Dispose(); } catch { }
        }

        // ==============================================================
        // Historical + streaming emit
        // ==============================================================

        private void EmitHistorical(BarsRequestEntry entry)
        {
            // NT8 8.1: BarsRequest exposes its loaded data via request.Bars.
            var bars = entry.Request != null ? entry.Request.Bars : null;
            if (bars == null || bars.Count == 0)
            {
                logInfo("VLBarsSubscriptionManager: historical " + entry.Key + " — zero bars");
                return;
            }

            var barsList = new List<object>(bars.Count);
            long lastT = 0;
            // FLAG: NT8 API — `bars.Count` and the indexed accessors
            // (bars.GetTime(i), bars.GetOpen(i), ...) are the standard
            // NT8 8.1 API. If the operator's build exposes only `bars[i]`
            // with named properties, this loop needs a minor refactor.
            for (int i = 0; i < bars.Count; i++)
            {
                long t = ToUtcEpochMs(bars.GetTime(i), bars.TradingHours);
                if (t <= entry.LastEmittedTimeUtcMs) continue; // dedup guard
                lastT = t;
                barsList.Add(BuildBarObject(t,
                    bars.GetOpen(i), bars.GetHigh(i),
                    bars.GetLow(i),  bars.GetClose(i),
                    bars.GetVolume(i), true, NowUtcMs())); // historical bars are closed by definition
            }

            if (barsList.Count == 0)
            {
                // Empty seed (every bar was deduped — delayed/older feed
                // re-returning bars we already emitted): do NOT stamp
                // HistoricalSent or the watchdog clock. An empty historical
                // callback must not read as "fresh" — that was the incident:
                // bars=0 still reset LastUpdateUtcMs so the watchdog reported
                // age 0 while the market timestamps were ~10 min late.
                logInfo("VLBarsSubscriptionManager: historical " + entry.Key
                        + " — deduped all (no new bars); not stamping freshness");
                return;
            }

            entry.HistoricalSent = true;
            entry.LastUpdateUtcMs = NowUtcMs();       // liveness: wall-clock of this emit
            entry.LastBarTUtcMs = lastT;              // freshness: newest bar's MARKET time
            entry.LastBarArrivalUtcMs = NowUtcMs();   // liveness: arrival of that newest bar
            entry.LastBarProvenance = "bars_historical";
            if (lastT > entry.LastEmittedTimeUtcMs)
                entry.LastEmittedTimeUtcMs = lastT;
            if (lastT > entry.LastFinalizedTimeUtcMs)
                entry.LastFinalizedTimeUtcMs = lastT;

            var payload = new Dictionary<string, object>
            {
                ["symbol"]    = entry.Symbol,
                ["timeframe"] = entry.Timeframe,
                ["contract"]  = entry.Contract, // EVERY bar frame names its contract (owner ruling 2026-09-11)
                ["bars"]      = barsList
            };
            sendFrame("bars_historical", payload);

            logInfo("VLBarsSubscriptionManager: emitted bars_historical "
                    + entry.Key + " bars=" + barsList.Count);
        }

        private void OnBarsUpdate(BarsRequestEntry entry, BarsUpdateEventArgs args)
        {
            // Historical-load dedup: ignore updates that fire BEFORE the
            // initial Request callback returns. NT8 may fire .Update during
            // backfill on some builds; we drop those, the historical batch
            // already covered the same bar times.
            if (!entry.HistoricalSent) return;

            // NT8 8.1: BarsUpdateEventArgs exposes MinIndex + MaxIndex only.
            // The source Bars collection hangs off the BarsRequest, NOT off
            // the event args (confirmed via operator compile-check —
            // CS1061 'BarsUpdateEventArgs' does not contain a definition
            // for 'Bars'). We read from the captured entry.Request.Bars.
            var bars = entry.Request != null ? entry.Request.Bars : null;
            int minIdx = args.MinIndex;
            int maxIdx = args.MaxIndex;

            if (bars == null || minIdx < 0 || maxIdx < minIdx) return;

            var emitted = new List<object>();
            long maxEmittedT = entry.LastEmittedTimeUtcMs;
            for (int i = minIdx; i <= maxIdx && i < bars.Count; i++)
            {
                long t = ToUtcEpochMs(bars.GetTime(i), bars.TradingHours);
                // We DO emit updates to a bar whose time we've already seen —
                // that's the "this bar is still forming" case. Only dedup
                // against bars STRICTLY OLDER than the latest historical t,
                // which means we never roll back into the historical batch.
                // A bar already finalized must never re-emit as forming (the
                // Go cache would overwrite final=true back to false).
                if (t < entry.LastEmittedTimeUtcMs || t <= entry.LastFinalizedTimeUtcMs) continue;
                emitted.Add(BuildBarObject(t,
                    bars.GetOpen(i), bars.GetHigh(i),
                    bars.GetLow(i),  bars.GetClose(i),
                    bars.GetVolume(i), false, NowUtcMs())); // forming
                if (t > maxEmittedT) maxEmittedT = t;
            }
            // PICTURE-HTF evidence: when a NEW bar started forming, the bar
            // just before it CLOSED. NT8 never re-emits the just-closed bar
            // on its own, so the boundary finalization is ours: re-emit the
            // previous bar once with final=true. The Go side consumes only
            // final-marked bars for the two-picture mode.
            if (bars.Count >= 2)
            {
                int prevIdx = bars.Count - 2;
                long tPrev = ToUtcEpochMs(bars.GetTime(prevIdx), bars.TradingHours);
                if (tPrev >= entry.LastEmittedTimeUtcMs && tPrev > entry.LastFinalizedTimeUtcMs)
                {
                    emitted.Add(BuildBarObject(tPrev,
                        bars.GetOpen(prevIdx), bars.GetHigh(prevIdx),
                        bars.GetLow(prevIdx),  bars.GetClose(prevIdx),
                        bars.GetVolume(prevIdx), true, NowUtcMs())); // closed
                    entry.LastFinalizedTimeUtcMs = tPrev;
                }
            }
            if (emitted.Count == 0) return;

            entry.LastEmittedTimeUtcMs = maxEmittedT;
            entry.LastUpdateUtcMs = NowUtcMs(); // watchdog: live bar received
            entry.LastBarTUtcMs = maxEmittedT;        // freshness: newest forming bar's MARKET time
            entry.LastBarArrivalUtcMs = NowUtcMs();   // liveness: arrival of that bar
            entry.LastBarProvenance = "bar_update";
            entry.LiveUpdateSeen = true;        // fast-guard: a real live .Update has fired since (re)subscribe

            var payload = new Dictionary<string, object>
            {
                ["symbol"]    = entry.Symbol,
                ["timeframe"] = entry.Timeframe,
                ["contract"]  = entry.Contract, // EVERY bar frame names its contract (owner ruling 2026-09-11)
                ["bars"]      = emitted
            };
            sendFrame("bar_update", payload);
        }

        // ==============================================================
        // Reconnect handling — called by VLTraderTCPClient when the broker
        // connection (NT8 ↔ data provider, NOT the Go TCP) reconnects
        // ==============================================================

        /// <summary>
        /// Re-create every active BarsRequest. NT8 stops delivering updates
        /// to the original request after a connection drop and gives no
        /// explicit signal — so on reconnect we tear down and re-open every
        /// subscription, preserving the LastEmittedTimeUtcMs cursor so the
        /// Go side doesn't see duplicate historical bars.
        /// </summary>
        public void OnConnectionReconnected()
        {
            lock (subsLock)
            {
                var toRecreate = new List<BarsRequestEntry>(active.Values);
                foreach (var entry in toRecreate)
                {
                    DisposeEntry(entry);
                    active.Remove(entry.Key);
                    logInfo("VLBarsSubscriptionManager: reconnect — re-subscribing " + entry.Key);

                    // Same root → front-month resolution as the initial
                    // subscribe path. Re-resolved on each reconnect so a
                    // reconnect that straddles a quarterly roll picks up
                    // the new contract automatically.
                    string contract = VLContractResolver.ResolveFrontMonthContract(entry.Symbol);
                    string how;
                    var instrument = VLInstrumentLookup.Resolve(entry.Symbol, logWarn, out how);
                    logInfo("VLBarsSubscriptionManager: reconnect resolved "
                            + entry.Symbol + " -> " + contract + " => "
                            + (instrument != null ? VLInstrumentLookup.ContractName(instrument) : "<null>") + " (" + how + ")");
                    if (instrument == null)
                    {
                        logWarn("VLBarsSubscriptionManager: reconnect — instrument "
                                + entry.Symbol + " (resolved to " + contract + ") not found");
                        continue;
                    }
                    // Preserve LastEmittedTimeUtcMs on the new entry so the
                    // freshly-fired historical load can dedup against what
                    // the Go side already received.
                    Subscribe(entry.Symbol, entry.Timeframe, DEFAULT_BARS_BACK, instrument);
                    BarsRequestEntry fresh;
                    if (active.TryGetValue(entry.Key, out fresh))
                    {
                        fresh.LastEmittedTimeUtcMs = entry.LastEmittedTimeUtcMs;
                    }
                }
            }
        }

        // ==============================================================
        // Stall watchdog — recreate-on-stall + visibility
        // ==============================================================

        /// <summary>
        /// Periodic check (WATCHDOG_PERIOD_MS). Logs the most-stale active
        /// subscription's bar age (so a stall is never silent), and if it
        /// exceeds WATCHDOG_STALL_MS recreates all BarsRequests via the existing
        /// OnConnectionReconnected() path. The threshold is larger than the daily
        /// 16:00-17:00 CT maintenance halt so the expected daily gap (and, with a
        /// re-arm guard, the weekend) does not churn recreates. This is a backstop
        /// behind the primary fix (ETH trading hours, which should keep .Update
        /// firing past the session close on its own).
        /// </summary>
        private void WatchdogTick(object state)
        {
            try
            {
                long now = NowUtcMs();
                long oldestBarT = long.MaxValue;     // freshness: newest bar's MARKET time (min over entries)
                long oldestArrival = long.MaxValue;  // liveness: arrival of that bar
                string staleKey = null;
                bool anyDead = false; // (re)subscribed + historical sent, but NO live .Update within FAST_STALL_MS
                bool anyLive = false; // at least one subscription has seen a REAL live .Update
                int deadCount = 0;
                lock (subsLock)
                {
                    foreach (var kv in active)
                    {
                        var e = kv.Value;
                        // Counted BEFORE the seeding filter below: the re-arm test must
                        // mean "a live update actually arrived", never "nothing currently
                        // looks dead" (see the re-arm block for why that distinction is
                        // the whole bug).
                        if (e.LiveUpdateSeen) anyLive = true;
                        if (!e.HistoricalSent) continue;
                        if (!e.LiveUpdateSeen && now - e.SubscribedAtUtcMs >= FAST_STALL_MS) { anyDead = true; deadCount++; }
                        // Freshness is the MARKET time of the newest bar, NOT the
                        // arrival wall-clock. A steady stream of DELAYED bars keeps
                        // LastUpdateUtcMs at age 0 while LastBarTUtcMs shows the
                        // ~10-min lag — that is the truthful signal.
                        if (e.LastBarTUtcMs != 0 && e.LastBarTUtcMs < oldestBarT)
                        {
                            oldestBarT = e.LastBarTUtcMs;
                            oldestArrival = e.LastBarArrivalUtcMs;
                            staleKey = e.Key;
                        }
                    }
                }

                // Visibility — throttled to ~1/min so the 15s cadence doesn't spam the log.
                if (staleKey != null && now - lastAgeLogUtcMs >= 60000L)
                {
                    lastAgeLogUtcMs = now;
                    long marketAge = oldestBarT != 0 ? (now - oldestBarT) / 1000 : -1;
                    long arrivalAge = oldestArrival != 0 ? (now - oldestArrival) / 1000 : -1;
                    logInfo("VLBarsSubscriptionManager: watchdog — most-stale " + staleKey
                            + " bar age(market) " + marketAge + "s / last-arrival "
                            + arrivalAge + "s; dead-subscriptions=" + deadCount);
                }

                // FAST GUARD — the post-(re)subscribe dead window. Historical seeded but
                // no live .Update arrived: recreate quickly (the proven OnConnectionReconnected
                // revival). Bounded by FAST_MAX_ATTEMPTS so a real no-data window (halt/closed)
                // cannot churn; the counter re-arms once a live .Update is seen anywhere.
                // RE-ARM ONLY WHEN GENUINELY HEALTHY — three states, not two.
                //
                //   healthy  : nothing dead AND something live   → re-arm the budget
                //   seeding  : nothing dead AND nothing live yet → do nothing
                //   degraded : something dead                    → spend the budget
                //
                // The old test re-armed on `!anyDead` alone, which merged "healthy"
                // with "seeding". The loop above skips entries whose HistoricalSent is
                // false, so in the moments right after a recreate NOTHING qualifies as
                // dead — the old test read that as healthy and reset the very counter
                // it had just incremented, so each recreate re-armed the next one and
                // FAST_MAX_ATTEMPTS became unreachable.
                //
                // With the market closed there are no live updates BY DEFINITION, so
                // every subscription looks dead forever and the loop never exits.
                // Observed 2026-08-14..16: the guard fired at the Friday 16:00 CT close
                // and ran ~120x/hour for 48 hours — 807 recreates Friday, 80,444
                // Saturday, >100k rebuilds, every one logging "attempt 1/3" and never
                // once reaching 2/3. It stopped only when NT8 was restarted.
                //
                // Re-arming on anyLive ALONE would be a second livelock, during market
                // hours: a coarse timeframe (1W/3D) can sit without a live .Update while
                // 1M ticks, so anyDead stays true and the budget would refill every tick.
                // Requiring BOTH conditions bounds a genuine dead window to
                // FAST_MAX_ATTEMPTS recreates and then goes quiet until real data
                // returns — which is also the right behavior for a closed market, with
                // no session-hours lookup needed.
                if (!anyDead && anyLive)
                {
                    if (fastRecreateAttempts != 0) fastRecreateAttempts = 0; // genuinely healthy → re-arm
                }
                else if (anyDead)
                {
                    bool doFast = false;
                    lock (watchdogLock)
                    {
                        if (fastRecreateAttempts < FAST_MAX_ATTEMPTS
                            && now - lastFastRecreateUtcMs >= FAST_STALL_MS)
                        {
                            lastFastRecreateUtcMs = now;
                            fastRecreateAttempts++;
                            doFast = true;
                        }
                    }
                    if (doFast)
                    {
                        logWarn("VLBarsSubscriptionManager: fast guard — " + deadCount
                                + " subscription(s) seeded but no live .Update within " + (FAST_STALL_MS / 1000)
                                + "s (attempt " + fastRecreateAttempts + "/" + FAST_MAX_ATTEMPTS
                                + "); recreating all BarsRequests");
                        OnConnectionReconnected();
                        return; // recreate fired this tick; observe the result on subsequent ticks
                    }
                }

                // 75-MIN BACKSTOP — catches a SILENT mid-session .Update death (live updates
                // were flowing, then stopped, so the fast guard above does not apply).
                if (staleKey == null) return;
                long ageMs = now - oldest;
                if (ageMs < WATCHDOG_STALL_MS) return;

                bool doRecreate = false;
                lock (watchdogLock)
                {
                    if (now - lastWatchdogRecreateUtcMs >= WATCHDOG_STALL_MS)
                    {
                        lastWatchdogRecreateUtcMs = now;
                        doRecreate = true;
                    }
                }
                if (doRecreate)
                {
                    logWarn("VLBarsSubscriptionManager: watchdog — bar feed stalled "
                            + (ageMs / 1000) + "s (> " + (WATCHDOG_STALL_MS / 1000)
                            + "s); recreating all BarsRequests");
                    OnConnectionReconnected();
                }
            }
            catch (Exception ex)
            {
                logWarn("VLBarsSubscriptionManager: watchdog tick failed: " + ex.Message);
            }
        }

        private static long NowUtcMs()
        {
            return (long)((DateTime.UtcNow
                - new DateTime(1970, 1, 1, 0, 0, 0, DateTimeKind.Utc)).TotalMilliseconds);
        }

        /// <summary>
        /// Feed freshness for the additive feed_status frame: the newest bar's
        /// MARKET timestamp + its arrival wall-clock + provenance, across all
        /// active subscriptions. mode is NOT returned here — the C# side has no
        /// supported NT8 API for the market-data subscription mode (Delayed vs
        /// Realtime), so the caller always sends mode="unknown".
        /// </summary>
        public void GetFeedFreshness(out long lastBarTMs, out long lastBarArrivalMs, out string provenance)
        {
            long bestT = 0, bestArrival = 0;
            string bestProv = "none";
            lock (subsLock)
            {
                foreach (var kv in active)
                {
                    var e = kv.Value;
                    if (e.LastBarTUtcMs > bestT)
                    {
                        bestT = e.LastBarTUtcMs;
                        bestArrival = e.LastBarArrivalUtcMs;
                        bestProv = e.LastBarProvenance ?? "none";
                    }
                }
            }
            lastBarTMs = bestT;
            lastBarArrivalMs = bestArrival;
            provenance = bestProv;
        }

        /// <summary>
        /// Tear down all subscriptions. Called by VLTraderTCPClient at
        /// AddOn-Terminated time.
        /// </summary>
        public void DisposeAll()
        {
            try { if (watchdogTimer != null) watchdogTimer.Dispose(); } catch { }
            watchdogTimer = null;
            lock (subsLock)
            {
                foreach (var kv in active) DisposeEntry(kv.Value);
                active.Clear();
            }
        }

        // ==============================================================
        // Timeframe → BarsPeriod mapping
        // ==============================================================

        /// <summary>
        /// Map a Go-side timeframe string (matching
        /// store/strategy.go::normalizeTimeframe) to an NT8 BarsPeriod.
        /// Throws on unsupported values — the caller drops the subscription
        /// and logs.
        /// </summary>
        private static BarsPeriod MapTimeframe(string tf)
        {
            if (string.IsNullOrEmpty(tf))
                throw new ArgumentException("empty timeframe");

            switch (tf)
            {
                // Sub-hour: native Minute multipliers.
                case "1m":  return new BarsPeriod { BarsPeriodType = BarsPeriodType.Minute, Value = 1   };
                case "3m":  return new BarsPeriod { BarsPeriodType = BarsPeriodType.Minute, Value = 3   };
                case "5m":  return new BarsPeriod { BarsPeriodType = BarsPeriodType.Minute, Value = 5   };
                case "15m": return new BarsPeriod { BarsPeriodType = BarsPeriodType.Minute, Value = 15  };
                case "30m": return new BarsPeriod { BarsPeriodType = BarsPeriodType.Minute, Value = 30  };

                // Hourly: Minute*60 is universally supported in NT8. Using
                // BarsPeriodType.Hour with Value=N also works on most builds
                // but Minute multipliers avoid an "unknown enum" risk on
                // older builds.
                case "1h":  return new BarsPeriod { BarsPeriodType = BarsPeriodType.Minute, Value = 60  };
                case "2h":  return new BarsPeriod { BarsPeriodType = BarsPeriodType.Minute, Value = 120 };
                case "4h":  return new BarsPeriod { BarsPeriodType = BarsPeriodType.Minute, Value = 240 };
                case "6h":  return new BarsPeriod { BarsPeriodType = BarsPeriodType.Minute, Value = 360 };
                case "8h":  return new BarsPeriod { BarsPeriodType = BarsPeriodType.Minute, Value = 480 };
                case "12h": return new BarsPeriod { BarsPeriodType = BarsPeriodType.Minute, Value = 720 };

                // Daily / weekly.
                case "1d":  return new BarsPeriod { BarsPeriodType = BarsPeriodType.Day,    Value = 1   };
                case "3d":  return new BarsPeriod { BarsPeriodType = BarsPeriodType.Day,    Value = 3   };
                case "1w":  return new BarsPeriod { BarsPeriodType = BarsPeriodType.Week,   Value = 1   };

                default:
                    throw new ArgumentException("unsupported timeframe: " + tf);
            }
        }

        // ==============================================================
        // Helpers
        // ==============================================================

        /// <summary>
        /// Convert NT8's local-time bar timestamp to UTC epoch milliseconds.
        /// `tradingHours.TimeZoneInfo` is the canonical source per the NT8
        /// help guide ("How Bars Are Built" — bar times reflect the chart's
        /// configured time zone, which derives from the Trading Hours
        /// template attached to the BarsRequest).
        /// </summary>
        private static long ToUtcEpochMs(DateTime localTime, TradingHours tradingHours)
        {
            DateTime utc;
            if (localTime.Kind == DateTimeKind.Utc)
            {
                utc = localTime;
            }
            else
            {
                TimeZoneInfo tz = null;
                if (tradingHours != null)
                {
                    // FLAG: NT8 API — `TradingHours.TimeZoneInfo` is the typical
                    // accessor. Some builds expose it as `tradingHours.TimeZone`
                    // (no "Info" suffix). If the operator hits a compile error,
                    // swap to that name.
                    tz = tradingHours.TimeZoneInfo;
                }
                if (tz == null)
                {
                    // Fall back to the machine's local zone — wrong if the
                    // bars were stamped in CT and the machine is in another
                    // zone, but better than throwing.
                    tz = TimeZoneInfo.Local;
                }
                utc = TimeZoneInfo.ConvertTimeToUtc(
                    DateTime.SpecifyKind(localTime, DateTimeKind.Unspecified), tz);
            }
            var unixEpoch = new DateTime(1970, 1, 1, 0, 0, 0, DateTimeKind.Utc);
            return (long)((utc - unixEpoch).TotalMilliseconds);
        }

        private static Dictionary<string, object> BuildBarObject(
            long tUtcMs, double o, double h, double l, double c, double v,
            bool final, long emittedAtUtcMs)
        {
            return new Dictionary<string, object>
            {
                ["t"] = tUtcMs,
                ["o"] = o,
                ["h"] = h,
                ["l"] = l,
                ["c"] = c,
                ["v"] = v,
                ["final"]      = final,
                ["emitted_at"] = emittedAtUtcMs
            };
        }

        private static string TryGetString(Dictionary<string, object> obj, string key)
        {
            object v;
            if (obj != null && obj.TryGetValue(key, out v) && v != null) return v.ToString();
            return null;
        }

        private static int TryGetInt(Dictionary<string, object> obj, string key, int dflt)
        {
            object v;
            if (obj != null && obj.TryGetValue(key, out v) && v != null)
            {
                if (v is long lo)   return (int)lo;
                if (v is int io)    return io;
                if (v is double doo) return (int)doo;
                int n;
                if (int.TryParse(v.ToString(), out n)) return n;
            }
            return dflt;
        }

        private static List<string> TryGetStringList(Dictionary<string, object> obj, string key)
        {
            object v;
            if (obj == null || !obj.TryGetValue(key, out v) || v == null) return null;
            var list = v as List<object>;
            if (list == null) return null;
            var result = new List<string>(list.Count);
            foreach (var item in list)
            {
                if (item == null) continue;
                var s = item.ToString();
                if (!string.IsNullOrEmpty(s)) result.Add(s);
            }
            return result;
        }

        /// <summary>
        /// Per-subscription state. Lives in the active dictionary for the
        /// lifetime of the BarsRequest. Mutated from NT8's data thread under
        /// subsLock (or — once the BarsRequest is firing — directly from the
        /// event callback; LastEmittedTimeUtcMs is the only field touched by
        /// that path).
        /// </summary>
        private class BarsRequestEntry
        {
            public string       Key;
            public string       Symbol;
            public string       Timeframe;
            public string       Contract;  // the contract NT8 resolved for this request ("MNQ 09-26")
            public BarsRequest  Request;
            public long         LastEmittedTimeUtcMs;
            public long         LastFinalizedTimeUtcMs; // the newest bar emitted with final=true
            public bool         HistoricalSent;
            public long         LastUpdateUtcMs;   // wall-clock UTC ms of last emit (stall watchdog / liveness)
            public long         LastBarTUtcMs;      // MARKET open time (UTC ms) of the newest bar emitted (freshness)
            public long         LastBarArrivalUtcMs;// wall-clock UTC ms when that newest bar was emitted (liveness)
            public string       LastBarProvenance;  // "bars_historical" | "bar_update" for the newest bar
            public long         SubscribedAtUtcMs; // wall-clock UTC ms when this request was (re)subscribed — fast-guard baseline
            public bool         LiveUpdateSeen;    // a genuine live .Update (OnBarsUpdate) has fired since (re)subscribe
        }
    }
}
