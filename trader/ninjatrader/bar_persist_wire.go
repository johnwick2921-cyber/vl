package ninjatrader

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"vl/market"

	"vl/kernel"
	"vl/logger"
	ntwire "vl/provider/ninjatrader"
	"vl/safe"
	"vl/store"
	"vl/telemetry"
)

// WireBarPersistence (2026-08-26) — installs the closed-bar writer on the TCP
// BarCache, flushes the boot backfill (~33h in-memory history), and arms the
// nightly retention prune. Idempotent (sync.Once): every trader's Start calls
// it, only the first wires.
var wireBarPersistenceOnce sync.Once

// contractFor is the ONE place this file resolves a symbol's contract.
//
// ROLL WAVE (2026-09-10). Source order, and the order is the point:
//  1. the AddOn's most recent `subscribed` ACK for the symbol — the frame that
//     names the instrument (the hello carries only build_id; bar frames carry
//     no instrument at all);
//  2. if no ACK has arrived THIS PROCESS yet (the first seconds after a boot),
//     the contract of the newest usable bar already in the store — the last
//     thing the broker told a previous process. Labelled as a fallback wherever
//     it is printed, never silently.
//  3. otherwise "" — and a writer given "" REFUSES (InsertBars), a reader given
//     "" reads nothing rather than everything. A24: no plausible value.
//
// Never a date rule. The AddOn's date rule is the defect that put two contracts
// on one tape; consulting a calendar here would rebuild it in Go.
func contractFor(bh *store.BarHistoryStore, server *ntwire.TCPServer, symbol string) (contract, source string) {
	if server != nil {
		if f, ok := server.CurrentContract(symbol); ok {
			return f.Contract, "subscribed@" + kernel.ClockCTSeconds(f.ReceivedAt)
		}
	}
	if bh != nil {
		if c, ok := bh.LatestContract(symbol); ok {
			return c, "store-fallback"
		}
	}
	return "", "none"
}

// barRowsForPersist is the persister's one mapping from a wire frame to store
// rows. It is a named function, not a loop inside the closure, so the stamp
// the callback writes can be pinned at the CALL SITE the worker actually
// invokes (A29: built is not wired). The frame's `historical` flag names the
// feed; a bar the ring has already labelled mixed (a boot minute across two
// scales) keeps that label whichever feed carried it.
func barRowsForPersist(symbol, tf, contract string, historical bool, closed []ntwire.Bar) []store.BarHistoryDB {
	feedSrc := store.BarSourceLive
	if historical {
		feedSrc = store.BarSourceHistorical
	}
	rows := make([]store.BarHistoryDB, 0, len(closed))
	for _, b := range closed {
		bs := feedSrc
		if b.Source == ntwire.BarSourceMixed {
			bs = store.BarSourceMixed
		}
		rows = append(rows, store.BarHistoryDB{
			Symbol: symbol, TF: tf, OpenTimeMs: b.T,
			O: b.O, H: b.H, L: b.L, C: b.C, V: b.V,
			Convention: market.StampConvention(tf),
			Contract:   contract,
			Source:     bs,
		})
	}
	return rows
}

// WireBarPersistence attaches the store to the bar feed. st == nil → no-op.
func WireBarPersistence(st *store.Store) {
	if st == nil {
		return
	}
	wireBarPersistenceOnce.Do(func() {
		bh := st.BarHistory()
		// W-BARS-CONTRACT-KEY — the key migration's report is printed HERE,
		// where the owner reads boot lines; every figure on it is read back
		// from the table. A failed key migration is fail-open (the report's
		// Err) and the store keeps writing on the legacy key; only a failure
		// of the table setup itself disables persistence, as before.
		rep, err := bh.MigrateWithReport(time.Now())
		if err != nil {
			logger.Warnf("bars: migrate failed: %v (persistence disabled)", err)
			return
		}
		if rep.Err != nil {
			logger.Errorf("%s", rep.BootLine())
		} else {
			logger.Infof("%s", rep.BootLine())
		}
		ntwire.SetBarPersister(func(historical bool, symbol, tf string, bars []ntwire.Bar) {
			srv, _ := getOrStartTCPServer()
			// BAR-SOURCE WAVE — a LIVE frame is the moment the ring has judged
			// the replay it holds; release or discard the held replay rows for
			// this key FIRST, before this frame's own closed bars (which may be
			// none: a bar_update for the current minute is not closed yet, and
			// the verdict must not wait for the next one).
			if !historical && srv != nil {
				checked, offScale := srv.BarCache().SeedVerdict(symbol, tf)
				barReplayHold.resolve(symbol, tf, checked, offScale, bh.InsertBars)
			}
			closed := ntwire.ClosedBarsOnly(bars, tf, time.Now().UnixMilli())
			if historical {
				// BAR-TRUTH 2026-08-28: replay frames arrive CLOSE-stamped -
				// apply the cache's open-stamp conversion (the 2499/2500 mismatch root cause).
				closed = ntwire.OpenStampBars(closed, tf)
			}
			if len(closed) == 0 {
				return
			}
			// ROLL WAVE — stamp the contract the AddOn most recently named.
			// The bar frame itself carries none (C4), so the stamp is the
			// subscription's, read at RECEIPT and with its source recorded.
			contract, src := contractFor(bh, srv, symbol)
			if contract == "" {
				logger.Warnf("bars: persist %s %s SKIPPED %d bar(s) — no contract has been named this process and the store holds none; a bar on an unknown price scale is not written (roll wave)", symbol, tf, len(closed))
				return
			}
			if src == "store-fallback" {
				logger.Warnf("bars: persist %s %s stamping %d bar(s) as %s from the STORE FALLBACK — no subscription ACK yet this process", symbol, tf, len(closed), contract)
			}
			// BAR-SOURCE WAVE — the `historical` flag this callback has always
			// received is now RECORDED, not discarded. A bar the ring labelled
			// mixed (a boot minute across two scales) keeps that label.
			rows := barRowsForPersist(symbol, tf, contract, historical, closed)
			if historical {
				// AN UNVERIFIED REPLAY IS NOT THE RECORD. Held until the first
				// live bar lets the ring judge its scale; see replayHold.
				barReplayHold.add(symbol, tf, rows)
				return
			}
			if err := bh.InsertBars(rows); err != nil {
				logger.Warnf("bars: persist %s %s failed: %v (never blocks the loop)", symbol, tf, err)
			}
		})
		// Boot backfill + prune loop: the singleton server starts lazily on the
		// first trader load; poll for it briefly, then flush the cache. The
		// AddOn's bars_historical replay lands a few seconds after our restart,
		// so retry while the flush stays empty.
		safe.GoNet("nt8-bar-backfill", "", func() {
			for i := 0; i < 90; i++ {
				server, err := getOrStartTCPServer()
				if err == nil && server != nil && server.BarCache() != nil {
					backfilled := backfillBars(bh, server)
					if backfilled == 0 {
						// Cache still empty (replay in flight) — retry a few
						// times; the live persister catches bars regardless.
						for r := 0; r < 20 && backfilled == 0; r++ {
							time.Sleep(15 * time.Second)
							backfilled = backfillBars(bh, server)
						}
					}
					// BARS HORIZON (2026-09-09) — the replay has landed, so the
					// EMPTY arm may speak, and the depth line can report what
					// the ring ACTUALLY holds rather than a cold cache.
					noteBarHorizonBackfillLanded()
					// D3 — REHYDRATE THE RING FROM THE STORE, alongside the
					// AddOn's seed. `now` is taken HERE, at the boot entry
					// point, and handed down (A28).
					//
					// ORDERING (fixed in review, 2026-09-09): this runs BEFORE
					// the afterBackfillHook, because that hook prints the
					// "📊 bars after backfill" line AND the BarResolver behind it
					// picks nt8 vs nt8_agg vs own1m from what the cache can
					// reach. Running the rehydrate afterwards left the
					// best-known boot line describing a ring the bot no longer
					// had — two lines about the same instant, and the older,
					// more-read one wrong.
					rehydrateRingFromStore(bh, server, time.Now())
					// ROLL WAVE (D5) — when the AddOn names a NEW contract, the
					// server has already purged the ring; reseed it from the
					// store for the new contract only, and say so ONCE where
					// the owner will see it. Registered here so it runs with
					// the same store handle the boot rehydrate used.
					// BAR-SOURCE WAVE — when the first live bar shows the replay
					// on a different scale, the ring has dropped its historical
					// seed; refill it from the store, whose live rows the upsert
					// rule now protects, and say so ONCE where the owner sees it.
					// 101 D1'(2): a scale check that could not be made because its
					// only reference was not adjacent is SAID, once per reference,
					// with both ages (A9) — never silently skipped. The seed stays;
					// the check stays armed; the replay's rows stay held from the
					// store until an adjacent pair can judge them.
					ntwire.OnScaleCheckSkip(func(sk ntwire.ScaleCheckSkip) {
						logger.Warnf("🕳 scale check SKIPPED for %s %s at %s: the only reference bar is %s old (replay tail %s, live bar %s) — not adjacent, so a time gap is not judged as a scale gap; seed kept, check stays armed, replay rows held from the store (101 D1' 2026-09-16)",
							sk.Symbol, sk.Timeframe, kernel.ClockCTSeconds(sk.At), sk.ReferenceAge.Truncate(time.Minute),
							kernel.ClockCTSeconds(time.UnixMilli(sk.ReferenceT)), kernel.ClockCTSeconds(time.UnixMilli(sk.LiveT)))
					})
					ntwire.OnScaleMismatch(func(m ntwire.ScaleMismatch) {
						// 101 D1'(3), narrow form: COUNT the break (A11 reads it
						// onto the summary line) and SAY what the ring is now —
						// the store rehydrate below is 1m-only by the owner's
						// 2026-09-09 condition, so every other timeframe is
						// live-only until NT8's next full replay. That sentence
						// is the difference between a thin chart that is
						// explained and one that is a surprise.
						telemetry.IncScaleBreakDrop(m.HistoricalDropped)
						// (3a) [O "I want full data", 2026-09-16] — ask NT8 for the
						// window again. The AddOn rebuilds its BarsRequest on a repeat
						// subscribe; the reply is bars_historical with ≈bars_back per
						// tf, judged by the ring like any replay. Rate-limited per
						// symbol and refused while the feed is down; a refusal is
						// WARNed with its reason, never silent (A9).
						rerr := server.RequestHistoryReplayAt(m.Symbol, time.Now())
						if rerr != nil {
							if errors.Is(rerr, ntwire.ErrHistoryReplaySpent) {
								logger.Errorf("🚨 P0 — second scale break this boot — replay on another contract, restart the AddOn (%s %s: NT8 NOT re-asked; the ring keeps its live bars + the store refill below, entered replay-grade): %v", m.Symbol, m.Timeframe, rerr)
							} else {
								logger.Warnf("🧯 history replay NOT re-requested for %s after the %s scale break: %v", m.Symbol, m.Timeframe, rerr)
							}
						}
						rehydrateRingFromStoreWith(bh, server, time.Now(), true)
						srcCensus, _ := bh.SourceCensus(m.Symbol)
						refill := scaleBreakRefillTxt(m.Timeframe, rerr)
						events, bars := telemetry.ScaleBreakCounts()
						logger.Errorf("🚨 P0 — REPLAY AND LIVE ARE ON DIFFERENT PRICE SCALES for %s %s at %s: last replay close %.2f, first live close %.2f, delta %.2f pts (> %.2f%% of price). %d historical bars DROPPED from the ring; %s; the straddling bar is labelled mixed and no reader takes it. scale-break drops since boot: %d event(s), %d bar(s). bars by source now %v. (bar-source wave 2026-09-10; adjacency guard 101 2026-09-16)",
							m.Symbol, m.Timeframe, kernel.ClockCTSeconds(m.At), m.LastHistoricalC, m.FirstLiveC, m.DeltaPts, ntwire.ScaleMismatchPct*100, m.HistoricalDropped, refill, events, bars, srcCensus)
					})
					ntwire.OnContractRoll(func(symbol, from, to string, at time.Time) {
						safe.GoNet("nt8-contract-roll", "", func() {
							// Let the AddOn's post-subscribe replay land first —
							// the ring must not be cold when the reseed reads
							// AllPairs(), and the replay is the new contract's
							// own history.
							time.Sleep(5 * time.Second)
							rehydrateRingFromStoreWith(bh, server, time.Now(), true)
							census, _ := bh.ContractCensus(symbol)
							logger.Errorf("🚨 P0 — CONTRACT ROLLED %s → %s at %s: the ring was purged and reseeded from the store for %s only; bars by contract now %v. Levels seated on %s are on the retired scale and will re-seat on the next planner read. (roll wave 2026-09-10)",
								from, to, at.Format("2006-01-02 15:04:05 MST"), to, census, from)
						})
					})
					// R1 (2026-09-02) — the boot 📊 bars line ran before this
					// replay landed, so it reported own1m for every TF on a
					// cold cache. Now that the pantry is in, say what the
					// resolver can ACTUALLY reach.
					fireAfterBackfillHook()
					logger.Infof("%s", barHorizonBootLine(server.BarCache(), time.Now()))
					safe.GoNet("nt8-bar-prune", "", func() { pruneLoop(bh) })
					return
				}
				time.Sleep(time.Second)
			}
			logger.Warnf("bars: TCP server never came up — boot backfill skipped")
		})
	})
}

// backfillBars flushes every closed bar the cache already holds (idempotent —
// INSERT OR IGNORE) and logs the spec boot line. Returns the flushed count so
// the caller can retry while the AddOn's bars_historical replay is still
// arriving (the cache is empty for the first seconds after a Go restart).
func backfillBars(bh *store.BarHistoryStore, server *ntwire.TCPServer) int {
	now := time.Now().UnixMilli()
	total, held := 0, 0
	for _, pair := range server.BarCache().AllPairs() {
		closed := ntwire.ClosedBarsOnly(server.BarCache().Get(pair[0], pair[1]), pair[1], now)
		contract, src := contractFor(bh, server, pair[0])
		if contract == "" {
			logger.Warnf("bars: backfill %s %s SKIPPED %d bar(s) — no contract named yet (roll wave)", pair[0], pair[1], len(closed))
			continue
		}
		if src == "store-fallback" {
			logger.Warnf("bars: backfill %s %s stamping as %s from the STORE FALLBACK", pair[0], pair[1], contract)
		}
		rows := make([]store.BarHistoryDB, 0, len(closed))
		for _, b := range closed {
			// BAR-SOURCE WAVE — the ring stamped each bar at Seed/Upsert; the
			// backfill carries that through. A ring bar with no source is a
			// programming error and is refused by InsertBars rather than guessed.
			rows = append(rows, store.BarHistoryDB{Symbol: pair[0], TF: pair[1], OpenTimeMs: b.T,
				O: b.O, H: b.H, L: b.L, C: b.C, V: b.V, Convention: market.StampConvention(pair[1]),
				Contract: contract, Source: b.Source})
		}
		// BAR-SOURCE WAVE — the boot ring is the AddOn's replay. It is HELD,
		// not written: the store learns it only after the first live bar has
		// let the ring judge its scale (replayHold). Bars the ring already
		// holds as live or mixed are written now.
		var write, hold []store.BarHistoryDB
		for _, r := range rows {
			if r.Source == store.BarSourceHistorical {
				hold = append(hold, r)
			} else {
				write = append(write, r)
			}
		}
		if len(hold) > 0 {
			barReplayHold.add(pair[0], pair[1], hold)
			held += len(hold)
		}
		if len(write) > 0 {
			if err := bh.InsertBars(write); err != nil {
				logger.Warnf("bars: backfill %s %s failed: %v", pair[0], pair[1], err)
				continue
			}
			total += len(write)
		}
	}
	pairs, _ := bh.SymbolTFCount()
	count, _ := bh.Count()
	logger.Infof("📦 bars: persisting %d symbol×tf retention=%dd rows=%d (backfilled %d live/mixed · %d replay row(s) HELD until the first live bar judges their scale)",
		pairs, store.BarRetentionDays(), count, total, held)
	// The caller retries while nothing landed; a held replay HAS landed.
	return total + held
}

// pruneLoop runs the retention prune + the NIGHTLY INTEGRITY CHECK (F5,
// 2026-08-27) at boot and then daily: duplicate natural-key groups must be 0
// and only tf='1m' may be stored (aggregates derive on read). WARN on drift.
func pruneLoop(bh *store.BarHistoryStore) {
	integrityCheck := func() {
		dups, tfs, total, err := bh.BarsIntegrity()
		if err != nil {
			logger.Warnf("bars: integrity check failed: %v", err)
			return
		}
		// W-BARS-CONTRACT-KEY — a minute held by two contracts is a roll
		// overlap, counted separately and READ; it is expected after a roll
		// and impossible on the legacy key.
		overlaps, oerr := bh.RollOverlaps()
		overlapTxt := "n/a"
		if oerr == nil {
			overlapTxt = fmt.Sprintf("%d", overlaps)
		}
		if dups > 0 {
			logger.Warnf("🚨 bars integrity DRIFT: dups=%d tfs=%v total=%d roll_overlaps=%s (expected dups=0) — replay/calibration readers must not trust stored aggregates", dups, tfs, total, overlapTxt)
			return
		}
		logger.Infof("✅ bars integrity OK: dups=0 tfs=%v total=%d roll_overlaps=%s (minutes held by two contracts; expected after a roll)", tfs, total, overlapTxt)
	}
	pruneOnce := func() {
		// BAR-SOURCE WAVE 2026-09-02 — retention is PER TF. The old single
		// cutoff was TF-blind and would have deleted the 383 weekly bars back
		// to 2019 on the first nightly prune after they were persisted.
		byTF, err := bh.PruneByTF(time.Now())
		if err != nil {
			logger.Warnf("bars: prune failed: %v", err)
			return
		}
		for tf, n := range byTF {
			logger.Infof("🧹 bars: pruned %d %s rows older than %dd (per-TF retention)", n, tf, store.RetentionDaysFor(tf))
		}
	}
	pruneOnce()
	integrityCheck()
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for range t.C {
		pruneOnce()
		integrityCheck()
	}
}

// afterBackfillHook lets the trader layer print its post-backfill lines (the
// R1 "📊 bars after backfill", the 📈 regime input window, the 🧮 planner
// tape) without this package importing it.
//
// ORDER MUST NOT MATTER (2026-09-16 15:32 CT, boot of c6579347): the trader
// installs this hook at load; the backfill goroutine checked it at 15:32:03,
// found nil, and the trader installed it at 15:32:05 — three boot lines gone,
// silently. The earlier atomic.Value was a mailbox with no memory of the
// event. Now: whichever side arrives second fires the hook, exactly once, under
// one mutex — no window between "checked nil" and "installed".
var (
	afterBackfillMu     sync.Mutex
	afterBackfillFn     func()
	afterBackfillLanded bool
	afterBackfillFired  bool
)

// SetAfterBackfillHook installs the callback. If the backfill has ALREADY
// landed and nothing has fired yet, it fires now. Later registrations replace
// the callback but never re-fire it.
func SetAfterBackfillHook(fn func()) {
	afterBackfillMu.Lock()
	afterBackfillFn = fn
	run := fn != nil && afterBackfillLanded && !afterBackfillFired
	if run {
		afterBackfillFired = true
	}
	afterBackfillMu.Unlock()
	if run {
		fn()
	}
}

// fireAfterBackfillHook marks the event landed and fires the hook if one is
// installed and it has not fired yet.
func fireAfterBackfillHook() {
	afterBackfillMu.Lock()
	afterBackfillLanded = true
	fn := afterBackfillFn
	run := fn != nil && !afterBackfillFired
	if run {
		afterBackfillFired = true
	}
	afterBackfillMu.Unlock()
	if run {
		fn()
	}
}

func resetAfterBackfillHookForTest() {
	afterBackfillMu.Lock()
	afterBackfillFn, afterBackfillLanded, afterBackfillFired = nil, false, false
	afterBackfillMu.Unlock()
}

// ── D3 — THE RING REHYDRATES FROM THE STORE ON BOOT ─────────────────────────
// (owner-authorised expansion, wave BARS HORIZON 2026-09-09)
//
// A Go restart drops the ring to the AddOn's 2000-bar seed
// (defaultAutoBarsBack) while the persisted `bars` table holds 21 days
// (measured 2026-09-09: MNQ 1m 20,043 rows back to 2026-08-19 10:00 CT). The
// ring climbs back to 2500 across a session because SeedHistorical MERGES —
// and then the next restart shortens the horizon again. Nothing ever
// rehydrated it.
//
// This runs ALONGSIDE the AddOn's seed, immediately after the replay lands, and
// it deliberately touches nothing else: not the AddOn, not the subscription,
// not the backfill.
//
// EVERY CONSTRAINT IS ENFORCED BY RehydrateOlder, not here:
//   - it MERGES (mergeBarsByTime, the same discipline SeedHistorical uses);
//   - it never replaces a live bar with a stale one (older-only, existing wins);
//   - it is bounded by the ring's own maxBars;
//   - it is a NO-OP on a cold key, so a dead feed can never be made to look alive.
//
// EVERY SUBSCRIBED TIMEFRAME — OWNER RULING 2026-09-16, direct: "i want fuull
// data". It supersedes condition (a) below; conditions (b), (c) and (d) stand
// and are what keep the regime input where it was. The four guards are on
// pairsToRehydrate. What follows is the 09-09 ruling and its measurement,
// kept because they are the reason guard (ii) stamps every store row as
// replay-grade rather than trusting an aggregate as a live bar.
//
// 1m ONLY — OWNER CONDITION (a), RULING 2026-09-09 18:18 CT — SUPERSEDED.
//
//	"RULING on D3: the regime input MAY change. Rehydrating the ring from the
//	 store changes what RVBaseline is fed — and what it is fed today is 41
//	 hours labelled as 20 days. A regime value computed on a shorter window
//	 than its name is the defect; correcting the window is not a scope
//	 violation, it is the fix. A31 forbids changing the RULE, not correcting
//	 the INPUT the rule was promised."
//
// The first cut rehydrated EVERY (symbol, timeframe) pair the cache held. That
// deepened the 5m ring from the AddOn's ~2000-bar seed to the 2500 cap, and
// min5Long (auto_trader_planner.go) was the ONE production ring read above
// 2000 — so it moved a REGIME input using NT8's own 5m aggregates. MEASURED on
// the live store, MNQ, kernel.RVBaselineFrom5mDays(·, 20, 5):
//
//	stored 5m, newest 2000 rows → 0.876371 over 7 complete session-days
//	stored 5m, newest 2500 rows → 0.884746 over 9 complete session-days
//	1m tape 12000 → agg 5m 2400 → 0.893543 over 9 complete session-days
//
// The last two cover the SAME nine days and disagree by 0.9%: the stored 5m
// rows do not agree with their own 1m constituents (store/bar_history.go's
// migration once deleted every non-1m row for exactly that reason). So the
// rehydrate takes the 1m rows ONLY — the feed's own closed bars, the tape every
// other series is aggregated FROM — and the deeper regime window is served from
// that 1m tail instead (owner condition (b): ResolveRVBaselineTape,
// trader/regime_input_window.go). The non-1m rings keep exactly what the AddOn
// seeded, exactly as before the wave.
//
// THIS WAVE DOES MOVE COMPUTED VALUES, AND THE REPORT NAMES THEM. Claiming
// otherwise while the boot line shows a change is class 82. What moves:
// RVBaseline / RVBaselineDays (7 → 9 complete session-days), and — through the
// D2 1m store splice, not through this rehydrate — CompletedWeekCount (0 → 2)
// and WeeklyShadowRefs (1 → 3). The RULE (kernel.RVBaselineFrom5mDays) is
// byte-for-byte unchanged and pinned so by an E7 golden.
//
// A10: a failed store read WARNs and boot continues. Nothing here gates,
// refuses, blocks or blanks.
func rehydrateRingFromStore(bh *store.BarHistoryStore, server *ntwire.TCPServer, now time.Time) {
	rehydrateRingFromStoreWith(bh, server, now, false)
}

// rehydrateRingFromStoreWith is the body; reseeded=true marks a roll-driven
// run so the boot line can distinguish "seeded at boot" from "reseeded on roll".
func rehydrateRingFromStoreWith(bh *store.BarHistoryStore, server *ntwire.TCPServer, now time.Time, reseeded bool) {
	if bh == nil || server == nil || server.BarCache() == nil {
		logger.Warnf("🧯 ring rehydrate SKIPPED: store=%v server=%v — the ring keeps whatever the AddOn seeded",
			bh != nil, server != nil)
		return
	}
	cache := server.BarCache()
	pairs := cache.AllPairs()
	if len(pairs) == 0 {
		logger.Warnf("🧯 ring rehydrate SKIPPED: the cache holds 0 symbol×tf pairs — a COLD ring is never filled from the store (the store deepens a live tape, it never substitutes for one)")
		return
	}
	selected := pairsToRehydrate(pairs)
	skipped := len(pairs) - len(selected)
	totalAdded, deepened, failed := 0, 0, 0
	rehydrateKept, rehydrateFiltered := 0, 0
	for _, pair := range selected {
		symbol, tf := pair[0], pair[1]
		before := cache.Count(symbol, tf)
		// ROLL WAVE — the store is read for the CURRENT contract only. This
		// unfiltered LastNBars is precisely how ~2,000 September bars were
		// spliced under December ones on the 4fc670aa boot: the ring's depth is
		// bounded by how much of THIS contract exists, which is the truth.
		contract, src := contractFor(bh, server, symbol)
		if contract == "" {
			failed++
			logger.Warnf("🧯 ring rehydrate %s %s SKIPPED: no contract named and none in the store — the ring keeps the AddOn seed (%d bars)", symbol, tf, before)
			continue
		}
		rows, err := bh.LastNBarsOn(symbol, tf, contract, cache.MaxBars())
		// A9 — every bar the filter kept OUT is counted, per contract, so the
		// boot line can say how much of the store was NOT this instrument.
		if all, aerr := bh.LastNBars(symbol, tf, cache.MaxBars()); aerr == nil {
			rehydrateKept += len(rows)
			rehydrateFiltered += len(all) - len(rows)
			if len(all)-len(rows) > 0 {
				logger.Infof("🧯 ring rehydrate %s %s: %d of the newest %d stored bars are NOT on %s and were filtered out (retired contract or spans-roll; kept in the store as history)",
					symbol, tf, len(all)-len(rows), len(all), contract)
			}
		}
		if err != nil {
			failed++
			logger.Warnf("🧯 ring rehydrate %s %s FAILED: %v — boot continues on the AddOn seed alone (%d bars)", symbol, tf, err, before)
			continue
		}
		if len(rows) == 0 {
			continue
		}
		// the source breakdown the boot line names [O], then guards (i)+(iii)
		storeLive, storeHist := 0, 0
		for _, r := range rows {
			switch r.Source {
			case store.BarSourceHistorical:
				storeHist++
			case store.BarSourceHistoricalImport:
				// counted by the door below
			default:
				storeLive++
			}
		}
		rows, importExcluded := rehydrateRowsFor(rows, reseeded)
		// guard (ii)
		bars := rehydrateBarsFromRows(rows)
		added := cache.RehydrateOlder(symbol, tf, bars)
		if added == 0 {
			continue
		}
		totalAdded += added
		deepened++
		after := cache.Get(symbol, tf)
		h := kernel.HorizonOf(barsToKlines(after, tf), tf, cache.MaxBars(), now)
		// nt8=<what the ring held from NT8 before> store_live/store_hist=<what
		// the store offered by stamp> import=<rows the DOOR refused — guard
		// (iii), counted in rehydrateRowsFor; LastNBarsOn filters only
		// mixed+off-scale and hands imports to its callers> total=<t>/cap —
		// every number READ, the [O] naming the ruling.
		logger.Infof("🧯 ring rehydrated %s %s [O 2026-09-16]: nt8=%d store_live=%d store_hist=%d (post-drop excluded=%v) import=%d (refused at the door — guard iii) total=%d/%d (+%d older, all entered as historical — guard ii) · contract=%s (%s) · %s",
			symbol, tf, before, storeLive, storeHist, reseeded, importExcluded, len(after), cache.MaxBars(), added, contract, src, h.Line())
	}
	logger.Infof("🧯 ring rehydrate done [O \"i want fuull data\" 2026-09-16]: %d of %d symbol×tf pairs deepened, +%d bars total, %d read failure(s), %d pair(s) not selected · every rehydrated row enters as historical (replay-grade to this process); the regime baseline is served from the %s tail (condition (b)) · store retention %s=%dd (the ring is the cache; the store is the horizon)",
		deepened, len(pairs), totalAdded, failed, skipped, rehydrateTimeframe, rehydrateTimeframe, store.RetentionDaysFor(rehydrateTimeframe))
	// ROLL WAVE (D6) — the contract boot line, every field read. One per
	// primary symbol the ring holds.
	seen := map[string]bool{}
	for _, pair := range selected {
		if seen[pair[0]] {
			continue
		}
		seen[pair[0]] = true
		logger.Infof("%s", contractBootLineFor(bh, server, pair[0], rehydrateKept, rehydrateFiltered, reseeded))
		// 101 E1 — what NT8 delivered at subscribe, per tf, READ from the
		// server's own frame records. One line that answers "did the history
		// arrive" without leaving Go (the 09-16 diagnosis needed the NT8 log).
		logger.Infof("%s", server.HistoryAtSubscribeLineFor(pair[0]))
		if sc, err := bh.SourceCensus(pair[0]); err == nil {
			logger.Infof("%s", SourceBootLine(pair[0], sc, cache.ScaleMismatches(), ntwire.ScaleMismatchPct, barReplayHold.line(pair[0])))
		}
	}
}

// rehydrateTimeframe was the 1m-only selection of owner condition (a),
// 2026-09-09. It remains the name of the FEED-OWN timeframe (the tape every
// other series is aggregated from, the one the regime baseline is served
// from under condition (b)); it is no longer the selection.
const rehydrateTimeframe = "1m"

// pairsToRehydrate is THE selection, extracted so a pin drives IT rather than a
// copy of it (class 86).
//
// OWNER RULING 2026-09-16, direct, verbatim: "i want fuull data". It
// SUPERSEDES condition (a) of 2026-09-09 ("the boot rehydrate touches 1m
// ONLY"): every subscribed timeframe is rehydrated from the store's
// current-contract rows — at boot after NT8's replay lands, and after a
// confirmed scale-break drop — under four guards enforced below and pinned:
//
//	(i)   post-drop, `historical` rows are the rejected seed's kin: excluded
//	(ii)  RING-SIDE: every store row enters the ring as historical — a store
//	      row is replay-grade to this process whatever its stamp says
//	      (vl-93's census: migration-stamped 09-26 `live`, catch-up-stamped
//	      12-26 `live`; age distinguishes neither)
//	(iii) historical_import rows never reach a planner ring (LastNBarsOn's own
//	      filter — measured, E4)
//	(iv)  deficit-only, NT8's replay wins, cap 2500 (RehydrateOlder's contract)
//
// The regime input is protected by condition (b), not by this selection:
// TestRegimeLabelUnchanged pins that the baseline did not move.
func pairsToRehydrate(pairs [][2]string) [][2]string {
	out := make([][2]string, 0, len(pairs))
	out = append(out, pairs...)
	return out
}

// rehydrateRowsFor applies guard (i): after a confirmed drop (reseeded=true)
// rows stamped `historical` are excluded — they are the rejected seed's kin;
// on the boot path they were verified in a prior boot and are kept.
func rehydrateRowsFor(rows []store.BarHistoryDB, reseeded bool) (kept []store.BarHistoryDB, importExcluded int) {
	out := make([]store.BarHistoryDB, 0, len(rows))
	for _, r := range rows {
		// guard (iii): an import never enters the ring, on either path. The
		// reader (LastNBarsOn) filters mixed+off-scale ONLY and hands imports
		// to every caller; this door is the only line that keeps them out of
		// the ring, and the boot line prints THIS count (vl-93 objection 1).
		if r.Source == store.BarSourceHistoricalImport {
			importExcluded++
			continue
		}
		// guard (i): after a confirmed drop, store-backed replay rows are
		// what the drop just judged — only live rows refill
		if reseeded && r.Source == store.BarSourceHistorical {
			continue
		}
		out = append(out, r)
	}
	return out, importExcluded
}

// rehydrateBarsFromRows applies guard (ii): EVERY row enters the ring stamped
// historical. The scale check, the merge and the persister then treat it as
// the replay it is to this process — never as a sacred live bar.
func rehydrateBarsFromRows(rows []store.BarHistoryDB) []ntwire.Bar {
	bars := make([]ntwire.Bar, 0, len(rows))
	for _, r := range rows {
		bars = append(bars, ntwire.Bar{T: r.OpenTimeMs, O: r.O, H: r.H, L: r.L, C: r.C, V: r.V, Source: ntwire.BarSourceHistorical})
	}
	return bars
}

// scaleBreakRefillTxt is the P0 line's account of what the ring holds for the
// broken timeframe after the drop, and whether NT8 was asked again. Under [O]
// "i want fuull data" (2026-09-16) EVERY timeframe refills from the store's
// live rows (3b, entered replay-grade — guard ii); the 1m-only "LIVE-ONLY
// UNTIL NT8's NEXT FULL REPLAY" literal of the 09-09 condition is retired
// here with it. The re-ask (3a) is once per boot; a refusal names its reason.
func scaleBreakRefillTxt(tf string, reask error) string {
	refill := tf + " ring = live bars + the store's live rows (entered replay-grade)"
	switch {
	case reask == nil:
		return refill + "; NT8 re-asked for its full replay (1/1 this boot)"
	case errors.Is(reask, ntwire.ErrHistoryReplaySpent):
		return refill + "; NT8 NOT re-asked — second break this boot, restart the AddOn"
	default:
		return refill + "; NT8 NOT re-asked (" + reask.Error() + ")"
	}
}
