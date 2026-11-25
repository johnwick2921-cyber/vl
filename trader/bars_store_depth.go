package trader

import (
	"sort"
	"strings"
	"time"

	"vl/kernel"
	"vl/logger"
	"vl/market"
	"vl/store"
)

// ── D2 — THE STORE IS THE HORIZON; THE RING IS THE CACHE ────────────────────
// (owner ruling, wave BARS HORIZON 2026-09-09)
//
// Three call sites asked for depth NO MARKET CONDITION COULD SUPPLY. The ring
// ceiling is DefaultBarCacheMaxBars = 2500 per (symbol, timeframe)
// (provider/ninjatrader/bar_cache.go:24), and a Go restart rebuilds it from a
// 2000-bar seed (defaultAutoBarsBack, tcp_server.go):
//
//	auto_trader_planner.go   1m × 12000 = 200.0 h  vs a 41.7 h ceiling
//	auto_trader_weekly.go    1m × 12000 = 200.0 h  vs a 41.7 h ceiling
//	auto_trader_planner.go   5m ×  3000 = 250.0 h  vs a 208.3 h ceiling
//
// The store held the depth the whole time and was wired to nothing:
// market.FuturesBarsProvider has ONE production assignment
// (trader/ninjatrader/bars_market_bridge.go) and it reads the BarCache only.
//
// CHOICE PER CALLER, recorded at the call site itself:
//   - the two 1m sites take (a) READ THE STORE — a candle table promising 8
//     session-days needs ~11,040 open minutes and the store has 21 days of them;
//   - the 5m site takes (b) CORRECT THE ASK AND STOP PROMISING 20 DAYS — see
//     rvBaseline5mBarsAsk in auto_trader_planner.go for why (a) was refused there.
//
// A10: this never gates, refuses, blocks or blanks. Every failure path returns
// what the ring returned, which is exactly the pre-wave value.
//
// ── KNOWN LIMITATION: THE STORE IS CONTRACT-BLIND (raised in review,
// 2026-09-09; NOT fixed here, because the wire and the AddOn are out of this
// wave's footprint). ──────────────────────────────────────────────────────────
//
// `.schema bars` (read 2026-09-09): PRIMARY KEY (symbol, tf, open_time_ms) —
// there is no contract/expiry column, and CLAUDE.md keeps the "MNQ 06-26" form
// inside the C# GetInstrument call only. So a stored MNQ 1m bar does not say
// WHICH contract it came from.
//
// WHAT THAT MEANS AFTER A QUARTERLY ROLL: the ring is the new contract while
// the store's older rows are the old one, and rule 3 admits those rows PRECISELY
// BECAUSE they are older. The quarterly basis on MNQ is tens of points, so the
// seam renders as a real price step in the 8-session daily table for as long as
// the pre-roll rows remain the deepest history — with no marker, because a
// contract change is not a missing bar and D1 measures presence, not identity.
//
// THE HAZARD PRE-EXISTS THIS WAVE (SeedHistorical merges, so a roll's old bars
// linger in the ring too) but its reach was 41.7 h; D2 extends it to the 1m
// retention window (90 days; 21 days held today). The next MNQ roll is the
// September→December contract. A later wave should persist the resolved contract
// per bar and MARK — never drop — a splice across a contract boundary.

// barsWithStoreDepth serves a bar request the ring alone cannot fill, by
// EXTENDING it backwards from the store. `now` comes from the caller (A28).
func (at *AutoTrader) barsWithStoreDepth(symbol, tf string, n int, now time.Time) []market.Kline {
	var ring []market.Kline
	if market.FuturesBarsProvider != nil {
		ring = market.FuturesBarsProvider(symbol, tf, n)
	}
	return barsWithStoreDepthFrom(ring, at.storeBarReader(symbol, tf), symbol, tf, n, now)
}

// BarsWithStoreDepth is the API/dashboard seam: the same contract-filtered
// store splice the planner uses, driven by an explicit contract instead of the
// AutoTrader's ACK lookup (F1, 2026-09-14). A nil store or an unnamed contract
// returns the ring untouched — the chart may be shallow, it is never
// wrong or mixed-scale (A10/A24, roll wave).
func BarsWithStoreDepth(ring []market.Kline, st *store.Store, contract, symbol, tf string, n int, now time.Time) []market.Kline {
	if st == nil || st.BarHistory() == nil || strings.TrimSpace(contract) == "" {
		return ring
	}
	reader := func(n int) ([]market.Kline, error) {
		// NT8-only, like storeBarReader — this seam documents itself as the
		// planner's splice and must read what the planner reads.
		rows, err := st.BarHistory().LastNBarsFromNT8On(symbol, tf, contract, n)
		if err != nil {
			return nil, err
		}
		return storeRowsToKlines(rows, tf), nil
	}
	return barsWithStoreDepthFrom(ring, reader, symbol, tf, n, now)
}

// BarsWithStoreDepthDisplay is BarsWithStoreDepth for CHART DISPLAY: it keeps
// historical_import rows EXCEPT isolated ones. Wave-101 bulk imports are sparse
// snapshots (measured 2026-09-14: MNQ 12-26 1m had ONE bar per day at
// 09-07 17:00, 09-08/09/10 21:00) — honest history for the store, but on a
// chart they render as lonely candles with 24-hour gaps where NT8's own chart
// shows nothing. The dense history import (F1 hole fill, 2026-09-14) is the
// SAME source flag, so a source filter can no longer tell the two apart: an
// import bar is dropped only when it is ISOLATED in the merged display series
// (both neighbors farther than 3× the TF span, a missing neighbor counting as
// far). A dense import fill renders; a lonely snapshot does not. The planner
// and every other reader keep the full store, untouched.
func BarsWithStoreDepthDisplay(ring []market.Kline, st *store.Store, contract, symbol, tf string, n int, now time.Time) []market.Kline {
	if st == nil || st.BarHistory() == nil || strings.TrimSpace(contract) == "" {
		return ring
	}
	reader := func(n int) ([]market.Kline, error) {
		rows, err := st.BarHistory().LastNBarsOn(symbol, tf, contract, n)
		if err != nil {
			return nil, err
		}
		return storeRowsToKlines(rows, tf), nil
	}
	out := barsWithStoreDepthFrom(ring, reader, symbol, tf, n, now)
	// F1 hole-fill (2026-09-14): NT8's local seed has INTERIOR holes (measured
	// 09-11 09:06-12:45 — NT8 was off that window) that the history import
	// filled in the STORE. The ring also holds the sparse snapshot bars at its
	// left edge, so the "strictly older" splice above can never fire for them
	// (nothing in the store predates the ring's oldest). Interleave store rows
	// into ring holes: a store row is inserted only where the ring has NO bar
	// at that open time — the ring wins every collision, the live tail is
	// untouched, and both sides are the same 12-26 scale.
	stored, err := st.BarHistory().LastNBarsOn(symbol, tf, contract, n)
	if err == nil && len(stored) > 0 {
		out = mergeStoreIntoRingHoles(out, storeRowsToKlines(stored, tf), n)
	}
	// The RING side of the sparse-snapshot filter: NT8's own BarsRequest seed
	// carries the wave-101 import snapshots (the four 1m bars at 09-07 17:00 /
	// 09-08·09·10 21:00Z arrive IN the ring, not in the splice). The bars key
	// is (symbol, tf, open_time_ms), so a matched timestamp is either the
	// import row or nothing — but only an ISOLATED one is dropped, by the same
	// neighbor test as the store side. (W-BARS-CONTRACT-KEY: the key is now
	// (symbol, tf, contract, open_time_ms); this read is per contract, so the
	// statement still holds within the contract.)
	drop, err := st.BarHistory().ImportSnapshotTimes(symbol, tf, contract)
	if err != nil || len(drop) == 0 {
		return out
	}
	return dropIsolatedImportSnapshots(out, drop, importIsolationGap(tf))
}

// mergeStoreIntoRingHoles interleaves store bars into ring holes: every stored
// bar whose open time is absent from the ring series is inserted, then the
// series is re-sorted ascending and capped at n. Ring bars win every
// timestamp collision (they are the live truth); the ring's oldest bar is
// never displaced.
func mergeStoreIntoRingHoles(ring []market.Kline, stored []market.Kline, n int) []market.Kline {
	if len(stored) == 0 {
		return ring
	}
	have := make(map[int64]bool, len(ring))
	for _, k := range ring {
		have[k.OpenTime] = true
	}
	out := ring
	for _, k := range stored {
		if have[k.OpenTime] {
			continue
		}
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OpenTime < out[j].OpenTime })
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// importIsolationGap is the neighbor distance that makes an import bar
// "isolated" on the display chart: 3× the TF span. Dense history (1m gap) is
// never isolated; the wave-101 one-bar-per-day snapshots are.
func importIsolationGap(tf string) int64 {
	mins := market.TFMinutes(tf)
	if mins <= 0 {
		mins = 1
	}
	return 3 * int64(mins) * 60000
}

// dropIsolatedImportSnapshots removes import-marked bars only when BOTH
// neighbors are farther than gap away (a missing neighbor counts as far).
// `in` must be ascending by OpenTime — the seam's merge contract.
func dropIsolatedImportSnapshots(in []market.Kline, snap map[int64]bool, gap int64) []market.Kline {
	if len(in) == 0 {
		return in
	}
	kept := make([]market.Kline, 0, len(in))
	for i, k := range in {
		if !snap[k.OpenTime] {
			kept = append(kept, k)
			continue
		}
		prevFar := i == 0 || in[i-1].OpenTime < k.OpenTime-gap
		nextFar := i == len(in)-1 || in[i+1].OpenTime > k.OpenTime+gap
		if prevFar && nextFar {
			continue
		}
		kept = append(kept, k)
	}
	return kept
}

// storeBarReader returns the store read as a closure, so barsWithStoreDepthFrom
// — the merge that actually matters — is driven by pins rather than by a copy
// of itself (class 86). A nil store yields a reader that reports UNAVAILABLE
// rather than an empty result, so "no store" can never look like "no history".
func (at *AutoTrader) storeBarReader(symbol, tf string) func(int) ([]market.Kline, error) {
	return func(n int) ([]market.Kline, error) {
		if at == nil || at.store == nil {
			return nil, errStoreUnavailable
		}
		// ROLL WAVE — the store deepens the ring with the CURRENT contract
		// only. This unfiltered read is how the planner received ~2,000
		// September bars under December ones: the merge preserved the live
		// tail and extended it backwards across a ~292-point basis, and the
		// 359-point RANGE on the desk strip was that step, not the market.
		contract, src := at.currentContract(symbol)
		if contract == "" {
			// UNKNOWN is not "everything": with no contract named, the depth
			// read is skipped and the ring stands alone (A24).
			return nil, errContractUnknown
		}
		// NT8-ONLY (CTO ruling under the owner's delegation, 2026-09-16):
		// historical_import never reaches a planner door. The chart's seam
		// (BarsWithStoreDepthDisplay) keeps imports, labelled; this one does
		// not. 426 imported 12-26 1m bars were inside this tape on 09-16.
		rows, err := at.store.BarHistory().LastNBarsFromNT8On(symbol, tf, contract, n)
		if err != nil {
			return nil, err
		}
		_ = src
		return storeRowsToKlines(rows, tf), nil
	}
}

// errStoreUnavailable distinguishes "there is no store" from "the store is
// empty" — an uncomputed answer is UNKNOWN, never zero (A24).
var errStoreUnavailable = storeUnavailableError{}

// errContractUnknown — no frame has named the contract and the store holds no
// stamped bar to fall back to. The depth read is skipped, never widened.
var errContractUnknown = contractUnknownError{}

type contractUnknownError struct{}

func (contractUnknownError) Error() string {
	return "no contract named yet — depth read skipped rather than read across a roll"
}

type storeUnavailableError struct{}

func (storeUnavailableError) Error() string { return "no store attached — depth read skipped" }

// storeRowsToKlines adapts stored rows to the ONE Kline shape every reader uses.
// CloseTime is derived from the timeframe exactly as the NT8 bridge derives it,
// so a store-sourced bar and a ring-sourced bar are indistinguishable downstream.
//
// m326 (2026-10-22) — the read path RE-ANCHORS the NT8-native 16:00-close stub.
// Rows persisted by a pre-m220/m134 binary sit ONE HOUR EARLY (the 4h 13:00
// bucket at 12:00 CT, the 2h 15:00 bucket at 14:00 CT); ReanchorCMEStub is
// idempotent (a correct row is untouched, a legacy stub shifts +1h), and when
// BOTH the legacy and the corrected row are present they collapse to ONE bar —
// the later, corrected stamp wins. No store write: this is display/read-only.
func storeRowsToKlines(rows []store.BarHistoryDB, tf string) []market.Kline {
	if len(rows) == 0 {
		return nil
	}
	durMs, _ := kernel.TFDurationMs(tf)
	out := make([]market.Kline, 0, len(rows))
	for _, r := range rows {
		ot := kernel.ReanchorCMEStub(tf, r.OpenTimeMs)
		k := market.Kline{OpenTime: ot, Open: r.O, High: r.H, Low: r.L, Close: r.C, Volume: r.V}
		if durMs > 0 {
			k.CloseTime = ot + durMs - 1
		}
		if len(out) > 0 && out[len(out)-1].OpenTime == ot {
			out[len(out)-1] = k // the corrected (later) stamp wins over the legacy stub
			continue
		}
		out = append(out, k)
	}
	return out
}

// barsWithStoreDepthFrom splices a store read onto the OLDER end of a ring read.
//
// THE RULES, in the order they are applied:
//
//  1. AN EMPTY RING IS NEVER SUBSTITUTED. An empty ring means the feed is down
//     or the AddOn seed has not landed; handing a planner 12,000 stored bars
//     would let it write a plan on a dead tape, which is the exact failure
//     "no NT8 → no decisions" exists to prevent. The store DEEPENS a live tape;
//     it never stands in for one.
//  2. A RING THAT ALREADY SERVES THE ASK IS NOT SECOND-GUESSED — no store read
//     at all, so the ~29 healthy call sites pay nothing.
//  3. ONLY BARS STRICTLY OLDER THAN THE RING'S OLDEST ARE TAKEN. A live bar is
//     never replaced by a stored one, so the freshest OHLCV always wins and the
//     forming bar survives untouched.
//  4. A FAILED OR EMPTY STORE READ DEGRADES TO THE RING, loudly (A9/A10).
//
// The result is ascending by open time, tail-capped at n, and byte-identical to
// the pre-wave value whenever the store adds nothing.
func barsWithStoreDepthFrom(ring []market.Kline, storeRead func(int) ([]market.Kline, error), symbol, tf string, n int, now time.Time) []market.Kline {
	if len(ring) == 0 || len(ring) >= n || n <= 0 || storeRead == nil {
		return ring
	}
	stored, err := storeRead(n)
	if err != nil {
		logger.Warnf("📚 bars depth: %s %s ask=%d ring=%d — store read FAILED (%v); serving the ring alone (nothing gated)",
			symbol, tf, n, len(ring), err)
		return ring
	}
	oldestRing := ring[0].OpenTime
	older := make([]market.Kline, 0, len(stored))
	for _, k := range stored {
		if k.OpenTime < oldestRing {
			older = append(older, k)
		}
	}
	if len(older) == 0 {
		logger.Infof("📚 bars depth: %s %s ask=%d ring=%d store=%d older=0 — the store holds nothing before the ring's oldest bar (%s CT)",
			symbol, tf, n, len(ring), len(stored), kernel.ClockHHMMCT(time.UnixMilli(oldestRing)))
		return ring
	}
	out := make([]market.Kline, 0, len(older)+len(ring))
	out = append(out, older...)
	out = append(out, ring...)
	if len(out) > n {
		out = out[len(out)-n:]
	}
	before := kernel.HorizonOf(ring, tf, n, now)
	after := kernel.HorizonOf(out, tf, n, now)
	logger.Infof("📚 bars depth: %s %s ask=%d · ring %s → store-deepened %s (+%d older bars; live tail untouched)",
		symbol, tf, n, before.Line(), after.Line(), len(older))
	return out
}
