// picture_htf_replay — the LABELED historical replay of the two-picture mode.
//
// This harness re-runs the DETECTION rules (BodyPivots4H → H1CloseBreak →
// StructuralSwing5M → NearestOpposingZone → R:R gate) over a closed window of
// stored bars, the same kernel functions the live evaluator calls, and prints
// detected levels, H1 confirmations, geometry, and eligible/refused
// opportunities WITH REASONS.
//
// IT IS A REPLAY, NOT A LIVE PATH: it never touches the opportunity ledger,
// never fans out to the evaluator, never sends a wire frame. Historical
// receipts cannot trigger live entries — that is the separate mechanism pin
// (TestSept17ReplayNeverMintsOpportunities); THIS harness is the requested
// strategy replay.
//
// DATA LAW (CTO corrections, 2026-09-20):
//   - CONTRACT PURE: the ladder is built from ONE contract (the window's
//     dominant contract) for the whole context. Cross-contract timestamp
//     merging is refused — the Sep→Dec roll must never leak into one ladder.
//   - NATIVE BARS: 5m/1h come from the STORED NT8-native rows (session-aligned
//     by NT8 trading hours), never UTC-modulo aggregation of 1m. 4h has no
//     stored native rows → derived from the native 1h ladder on the ETH grid
//     (22:00Z-anchored 4-bar groups, all four 1h present) — a DISCLOSED proxy.
//     If native 5m/1h rows are absent the harness refuses eligibility claims
//     ("native replay unavailable").
//   - ENTRY-PRICE TIMING: entryRef is the close of the last 5m bar COMPLETED
//     BEFORE the entry interval opens — a price knowable at the entry instant,
//     never the close of the entry interval's own bar.
//   - STOP BUFFER: symmetric ONE tick — long stop −1 tick, short stop +1 tick
//     (mirrors the production evaluator).
//   - SEQUENCE PROOF: per break, the harness prints the NATIVE H1-close →
//     next-5m-start chain (the 5m bar completing AT the H1 close, the
//     interval opened by the PRODUCTION formula prev5m.OpenTime + 5m, the
//     first native 5m bar of the next interval) and every price is labeled
//     with its known-at time — nothing known after the entry instant is used.
//
// Usage:
//
//	go run ./cmd/picture_htf_replay --db /tmp/picture-htf-replay.db \
//	  --start 2026-09-16T22:00:00Z --end 2026-09-17T22:00:00Z
//
// The DB argument must be a COPY of the live data.db (mode=ro).
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	_ "modernc.org/sqlite"

	"nofx/kernel"
	"nofx/market"
)

func main() {
	dbPath := flag.String("db", "", "path to a COPY of data.db (opened read-only)")
	startS := flag.String("start", "2026-09-16T22:00:00Z", "window start (RFC3339, UTC)")
	endS := flag.String("end", "2026-09-17T22:00:00Z", "window end (RFC3339, UTC)")
	symbol := flag.String("symbol", "MNQ", "symbol to replay")
	minRR := flag.Float64("min-rr", 2.5, "R:R gate (the resolved picture_htf minimum)")
	contextDays := flag.Int("context-days", 30, "days of 4H/1H context BEFORE --start for level detection (native 1h rows reach further back than the 5m/1m store)")
	flag.Parse()

	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "HISTORICAL REPLAY: --db is required (a COPY of data.db)")
		os.Exit(2)
	}
	start, err := time.Parse(time.RFC3339, *startS)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad --start: %v\n", err)
		os.Exit(2)
	}
	end, err := time.Parse(time.RFC3339, *endS)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad --end: %v\n", err)
		os.Exit(2)
	}

	fmt.Println("════════════════════════════════════════════════════════════════")
	fmt.Println("  PICTURE-HTF HISTORICAL REPLAY — feed receipts NOT claimed")
	fmt.Println("  no ledger writes · no evaluator fan-out · no wire frames")
	fmt.Println("════════════════════════════════════════════════════════════════")
	fmt.Printf("  db      : %s (read-only)\n", *dbPath)
	fmt.Printf("  symbol  : %s\n", *symbol)
	fmt.Printf("  window  : %s → %s\n", start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	fmt.Printf("  min R:R : %.2f\n", *minRR)
	fmt.Println()

	ctxStart := start.Add(-time.Duration(*contextDays) * 24 * time.Hour).UnixMilli()

	// ── Contract purity: ONE contract for the whole ladder ──
	contract := dominantContract(*dbPath, *symbol, start.UnixMilli(), end.UnixMilli())
	fmt.Printf("window contract (dominant by 1m rows): %q — the ENTIRE ladder is built contract-pure from it\n", contract)
	if contract == "" {
		fmt.Println("VERDICT: no rows for the window — nothing to replay (state this honestly).")
		os.Exit(0)
	}

	// ── NATIVE stored bars (session-aligned by NT8), contract-pure ──
	native5m := loadNative(*dbPath, *symbol, "5m", contract, ctxStart, end.UnixMilli())
	native1h := loadNative(*dbPath, *symbol, "1h", contract, ctxStart, end.UnixMilli())
	native1m := loadNative(*dbPath, *symbol, "1m", contract, start.UnixMilli(), end.UnixMilli())
	fmt.Printf("native rows loaded (contract-pure %s): 1m(window)=%d · 5m=%d · 1h=%d\n\n",
		contract, len(native1m), len(native5m), len(native1h))
	if len(native5m) == 0 || len(native1h) == 0 {
		fmt.Println("VERDICT: native stored 5m/1h rows absent — NATIVE REPLAY UNAVAILABLE;")
		fmt.Println("no eligibility claims are made from UTC-aggregated bars.")
		os.Exit(0)
	}

	// 4h is DERIVED from the native 1h ladder on the ETH grid (22:00Z-anchored
	// 4-bar groups, all four 1h present) — the only derivation left, disclosed.
	fourH := eth4hFrom1h(native1h)
	fmt.Printf("4h derived from native 1h on the ETH 22:00Z grid (proxy — no stored 4h): %d bars\n", len(fourH))

	// In-window 5m ladder (the opportunity machinery runs only in the window);
	// the H1 ladder carries ONE extra bar of context so the first in-window
	// boundary (prev bar from the prior hour) is evaluated, like the live
	// cache would.
	fiveM := filter(native5m, start.UnixMilli(), end.UnixMilli())
	h1 := filter(native1h, start.UnixMilli()-3600_000, end.UnixMilli())
	fmt.Printf("in-window: 5m=%d · 1h=%d (1h includes the boundary context bar)\n\n", len(fiveM), len(h1))

	// ── Section A: 4H levels in force when the window opened ──
	fmt.Println("── A. DETECTED 4H LEVELS (BodyPivots4H, as-of window start; retirement inside the window noted) ──")
	levelsAtStart := levelsAt(fourH, start.UnixMilli())
	levelsAtEnd := levelsAt(fourH, end.UnixMilli())
	activeEnd := map[int64]bool{}
	for _, l := range levelsAtEnd {
		activeEnd[l.SourceOpen] = true
	}
	for _, l := range levelsAtStart {
		ret := ""
		if !activeEnd[l.SourceOpen] {
			ret = " · RETIRED during the window"
		}
		fmt.Printf("  %-10s body %8.2f–%-8.2f wick %8.2f/%8.2f · source %s · knowable %s%s\n",
			l.Role, l.BodyTop, l.BodyBottom, l.WickHigh, l.WickLow,
			ts(l.SourceOpen), ts(l.KnowableAt), ret)
	}
	fmt.Printf("  in force at window start: %d · surviving at window end: %d\n\n", len(levelsAtStart), len(levelsAtEnd))

	// ── Section B: H1 confirmations ──
	fmt.Println("── B. H1 CONFIRMATIONS (each completed in-window H1 boundary vs levels active AT THAT BOUNDARY) ──")
	type breakEvent struct {
		cur     market.Kline
		prev    market.Kline
		verdict kernel.H1CloseBreakResult
	}
	var breaks []breakEvent
	h1Confirmed := 0
	for i := 1; i < len(h1); i++ {
		prev, cur := h1[i-1], h1[i]
		if cur.CloseTime >= end.UnixMilli() {
			break // the last H1 is still forming inside the window
		}
		act := kernel.ActiveLevels(levelsAt(fourH, cur.OpenTime), cur.OpenTime)
		v := kernel.H1CloseBreak(act, prev, cur, 0.25)
		h1Confirmed++
		if v.Fired {
			breaks = append(breaks, breakEvent{prev: prev, cur: cur, verdict: v})
			fmt.Printf("  H1 %s: prev %.2f → new %.2f · BREAK %s over %s %.2f (active levels %d, crossed %d)\n",
				ts(cur.CloseTime), prev.Close, cur.Close, v.Direction, levelRoleOf(act, v.LevelIdx), v.Boundary, len(act), len(v.Crossed))
		} else {
			fmt.Printf("  H1 %s: prev %.2f → new %.2f · no break (active levels %d)\n",
				ts(cur.CloseTime), prev.Close, cur.Close, len(act))
		}
	}
	fmt.Printf("  H1 completions evaluated: %d · breaks fired: %d\n\n", h1Confirmed, len(breaks))

	// ── Section C: per-break NATIVE SEQUENCE PROOF + geometry + verdict ──
	fmt.Println("── C. OPPORTUNITIES — per break: the NATIVE H1-close → next-5m-start sequence, geometry, verdict ──")
	eligible, refused := 0, 0
	reasonCounts := map[string]int{}
	for bi, b := range breaks {
		fmt.Printf("\n  #%d %s break over %.2f (H1 confirm %s)\n", bi+1, b.verdict.Direction, b.verdict.Boundary, ts(b.cur.CloseTime))

		// SEQUENCE STEP 1 — the native 5m bar COMPLETING AT the H1 close
		// (grid alignment: its CloseTime equals the H1 bar's CloseTime).
		prevIdx := -1
		for j, k := range native5m {
			if k.CloseTime == b.cur.CloseTime {
				prevIdx = j
				break
			}
		}
		if prevIdx < 0 {
			fmt.Printf("    SEQUENCE EVIDENCE MISSING — no native 5m bar completes at the H1 close %s\n", ts(b.cur.CloseTime))
			refused++
			reasonCounts["sequence_evidence_missing"]++
			continue
		}
		prev5m := native5m[prevIdx]

		// SEQUENCE STEP 2 — intervalStart by the PRODUCTION formula: the
		// completed bar's open + 5m. (Not H1 arithmetic — same formula the
		// evaluator computes from the live 5m ladder.)
		intervalStart := prev5m.OpenTime + 5*60_000

		// SEQUENCE STEP 3 — the FIRST native 5m bar of the next interval must
		// exist in the tape (its open == intervalStart).
		firstNext := -1
		for j, k := range native5m {
			if k.OpenTime == intervalStart {
				firstNext = j
				break
			}
		}
		fmt.Printf("    SEQUENCE (native NT8 candles, contract-pure):\n")
		fmt.Printf("      H1 confirm  : %s → %s  close %.2f (native 1h row)\n", ts(b.cur.OpenTime), ts(b.cur.CloseTime), b.cur.Close)
		fmt.Printf("      5m at close : %s → %s  close %.2f (native 5m row — CloseTime equals the H1 close: grid aligned)\n",
			ts(prev5m.OpenTime), ts(prev5m.CloseTime), prev5m.Close)
		fmt.Printf("      interval    : opens %s = prev5m.OpenTime + 5m (PRODUCTION formula)\n", ts(intervalStart))
		if firstNext < 0 {
			fmt.Printf("      next 5m bar : MISSING from the native tape\n")
			fmt.Printf("    verdict: REFUSED — interval_unfilled (the next 5m bar does not exist natively)\n")
			refused++
			reasonCounts["interval_unfilled"]++
			continue
		}
		fmt.Printf("      next 5m bar : opens %s (native 5m row — the next interval exists)\n", ts(native5m[firstNext].OpenTime))

		// EntryRef: a price KNOWABLE at the entry instant — the close of the
		// bar completed at the H1 close. The next bar's own close is known
		// five minutes later and is never used.
		entryRef := prev5m.Close
		fmt.Printf("      entry info  : entryRef %.2f (CloseTime %s ≤ entry instant %s — knowable at entry)\n",
			entryRef, ts(prev5m.CloseTime), ts(intervalStart))

		// Swing stop: completed 5m bars, deadline = the H1 close.
		lookback := native5m[:prevIdx+1]
		stopPx, ok := kernel.StructuralSwing5M(lookback, b.verdict.Direction, 24, b.cur.CloseTime)
		if !ok {
			fmt.Printf("    verdict: REFUSED — no confirmed 5m swing stop before the H1 close\n")
			refused++
			reasonCounts["no_swing"]++
			continue
		}
		// Symmetric ONE-tick buffer, mirrors the production evaluator.
		stopAdj := stopPx - 0.25
		if b.verdict.Direction == "short" {
			stopAdj = stopPx + 0.25
		}
		targetPx, ok := kernel.NearestOpposingZone(kernel.ActiveLevels(levelsAt(fourH, intervalStart), intervalStart), entryRef, b.verdict.Direction, intervalStart)
		if !ok {
			fmt.Printf("    swing stop %.2f (adj %.2f) · opposing zone: NONE\n", stopPx, stopAdj)
			fmt.Println("    verdict: REFUSED — no eligible opposing 4H zone")
			refused++
			reasonCounts["no_opposing_zone"]++
			continue
		}
		fmt.Printf("      geometry     : swing stop %.2f (adj %.2f, 5m swings completed ≤ H1 close) · opposing zone %.2f (4H level knowable ≤ entry instant)\n",
			stopPx, stopAdj, targetPx)
		risk := entryRef - stopAdj
		if risk < 0 {
			risk = -risk
		}
		reward := targetPx - entryRef
		if reward < 0 {
			reward = -reward
		}
		rr := 0.0
		if risk > 0 {
			rr = reward / risk
		}
		if rr < *minRR {
			fmt.Printf("    R:R %.2f below the %.2f minimum\n", rr, *minRR)
			fmt.Printf("    verdict: REFUSED — rr_below_min (nearer zone never skipped)\n")
			refused++
			reasonCounts["rr_below_min"]++
			continue
		}
		fmt.Printf("    R:R %.2f\n", rr)
		fmt.Println("    verdict: ELIGIBLE — would submit in live mode (subject to flat/fresh/unreconciled re-checks)")
		eligible++
	}

	fmt.Println("\n════════════════════════════════════════════════════════════════")
	fmt.Printf("  SUMMARY: levels(as-of start)=%d · H1 completions=%d · breaks=%d · eligible=%d · refused=%d\n",
		len(levelsAtStart), h1Confirmed, len(breaks), eligible, refused)
	if len(reasonCounts) > 0 {
		fmt.Println("  refusal reasons:")
		for r, n := range reasonCounts {
			fmt.Printf("    - %s ×%d\n", r, n)
		}
	}
	fmt.Println("  LABEL: HISTORICAL REPLAY — feed receipts NOT claimed; no opportunity")
	fmt.Println("  rows were written; live entries require native live frames + the")
	fmt.Println("  AddOn capability (MinAddonBuildPictureHtf) and the flat/fresh/")
	fmt.Println("  unreconciled re-checks at send time.")
	fmt.Println("════════════════════════════════════════════════════════════════")
}

func levelRoleOf(levels []kernel.PictureHtfLevel, idx int) string {
	if idx < 0 || idx >= len(levels) {
		return "?"
	}
	return levels[idx].Role
}

// levelsAt is the TIME-FAITHFUL snapshot: pivots over the 4H bars COMPLETED
// before `ms` (retirement scans only those bars) — the same semantics as the
// evaluator's rebuild (bars filtered by CloseTime < now).
func levelsAt(fourH []market.Kline, ms int64) []kernel.PictureHtfLevel {
	completed := make([]market.Kline, 0, len(fourH))
	for _, b := range fourH {
		if b.CloseTime < ms {
			completed = append(completed, b)
		}
	}
	return kernel.BodyPivots4H(completed, 120)
}

func ts(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04:05Z")
}

// dominantContract is the contract with the most 1m rows in the window — the
// whole ladder is then built contract-pure from it (no cross-contract
// merging, ever).
func dominantContract(dbPath, symbol string, startMs, endMs int64) string {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return ""
	}
	defer db.Close()
	var contract string
	err = db.QueryRow(`
		SELECT contract FROM bars
		WHERE symbol = ? AND tf = '1m' AND open_time_ms >= ? AND open_time_ms < ?
		GROUP BY contract ORDER BY COUNT(*) DESC LIMIT 1`, symbol, startMs, endMs).Scan(&contract)
	if err != nil {
		return ""
	}
	return contract
}

// loadNative reads STORED NT8-native bars (session-aligned) for ONE contract.
// The PK is (symbol,tf,contract,open_time_ms) on the live schema, so the
// contract is in the WHERE clause — isolation is enforced at the query.
func loadNative(dbPath, symbol, tf, contract string, startMs, endMs int64) []market.Kline {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query(`
		SELECT open_time_ms, o, h, l, c FROM bars
		WHERE symbol = ? AND tf = ? AND contract = ?
		  AND open_time_ms >= ? AND open_time_ms < ?
		ORDER BY open_time_ms ASC`, symbol, tf, contract, startMs, endMs)
	if err != nil {
		return nil
	}
	defer rows.Close()
	span := tfMs(tf)
	var out []market.Kline
	for rows.Next() {
		var openMs int64
		var o, h, l, c float64
		if err := rows.Scan(&openMs, &o, &h, &l, &c); err != nil {
			return nil
		}
		out = append(out, market.Kline{
			OpenTime:  openMs,
			CloseTime: openMs + span - 1,
			Open:      o, High: h, Low: l, Close: c,
			Final: true, // stored rows are closed by definition
		})
	}
	return out
}

func tfMs(tf string) int64 {
	switch tf {
	case "1m":
		return 60_000
	case "5m":
		return 5 * 60_000
	case "1h":
		return 60 * 60_000
	}
	return 60_000
}

// eth4hFrom1h derives 4h bars from the NATIVE 1h ladder on the ETH grid
// (anchored 22:00Z — the CME ETH session open). A bucket is included only
// when all four constituent 1h bars are present. This is the ONLY derivation
// in the replay and is disclosed; NT8-native 4h bars are not stored.
func eth4hFrom1h(h1 []market.Kline) []market.Kline {
	// ETH anchor: 22:00Z. A 4h bucket opens at 22:00Z + k*4h.
	const ethAnchorMod = (22 * 60 * 60 * 1000) % (4 * 60 * 60 * 1000)
	type acc struct {
		o, h, l, c float64
		open       int64
		n          int
	}
	m := map[int64]*acc{}
	var order []int64
	for _, b := range h1 {
		bucket := b.OpenTime - ((b.OpenTime - ethAnchorMod) % (4 * 60 * 60 * 1000))
		a, ok := m[bucket]
		if !ok {
			m[bucket] = &acc{o: b.Open, h: b.High, l: b.Low, c: b.Close, open: bucket, n: 1}
			order = append(order, bucket)
			continue
		}
		if b.High > a.h {
			a.h = b.High
		}
		if b.Low < a.l {
			a.l = b.Low
		}
		a.c = b.Close
		a.n++
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := make([]market.Kline, 0, len(order))
	for _, b := range order {
		a := m[b]
		if a.n != 4 {
			continue // partial 4h bucket — not a real closed bar
		}
		out = append(out, market.Kline{
			OpenTime: a.open, CloseTime: a.open + 4*60*60*1000 - 1,
			Open: a.o, High: a.h, Low: a.l, Close: a.c, Final: true,
		})
	}
	return out
}

// filter keeps bars with open time inside [startMs, endMs).
func filter(bars []market.Kline, startMs, endMs int64) []market.Kline {
	out := make([]market.Kline, 0, len(bars))
	for _, b := range bars {
		if b.OpenTime >= startMs && b.OpenTime < endMs {
			out = append(out, b)
		}
	}
	return out
}
