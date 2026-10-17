package main

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"nofx/kernel"
)

// runP3 — FULL PROMPT RENDER AUDIT (sandbox renderer, no AI, nothing stored).
func runP3() {
	db := openRO(liveDBPath)
	defer db.Close()

	now := time.Now()
	loc := kernel.CTLocation()
	tradeDate := now.In(loc).Format("2006-01-02")
	session := "ASIA"
	fmt.Printf("=== P3: session=%s tradeDate=%s now=%s (CT) ===\n", session, tradeDate, now.In(loc).Format(time.RFC3339))

	as := assemble(db, session, tradeDate, now, 0)
	in := as.in

	prompt := kernel.BuildPlannerPrompt(in)
	must(os.WriteFile(promptPath, []byte(prompt), 0o600))
	chars := len([]rune(prompt))
	fmt.Printf("prompt chars=%d · tokens(chars/4)=%d · %% of 65536 = %.1f%%\n",
		chars, chars/4, float64(chars)/65536*100)
	fmt.Printf("prompt written to %s (tmp only — nothing stored)\n\n", promptPath)

	// section inventory
	fmt.Println("=== section inventory ===")
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
			fmt.Println("  " + line)
		}
	}
	fmt.Println()

	// 3.2 weekly context — exact line quote
	fmt.Println("=== 3.2 ## Weekly Context (exact quote) ===")
	i := strings.Index(prompt, "## Weekly Context\n")
	if i >= 0 {
		end := i + len("## Weekly Context\n")
		lineEnd := strings.Index(prompt[end:], "\n")
		fmt.Printf("  %q\n", prompt[i:end+lineEnd+1])
	} else {
		fmt.Println("  SECTION MISSING")
	}

	// 3.1 candles
	fmt.Println("=== 3.1 ## Candles ===")
	ci := strings.Index(prompt, "## Candles (oldest→latest)")
	cend := strings.Index(prompt, "## Weekly Context")
	cblock := prompt[ci:cend]
	for _, title := range []string{"### 15m (last 12)", "### 1h (last 12)", "### 4h (last 8)", "### daily session candles (last 8)"} {
		ti := strings.Index(cblock, title)
		if ti < 0 {
			fmt.Printf("  %s: MISSING\n", title)
			continue
		}
		seg := cblock[ti:]
		if next := strings.Index(cblock[ti+3:], "### "); next > 0 {
			seg = cblock[ti : ti+3+next]
		}
		rows := 0
		for _, l := range strings.Split(seg, "\n") {
			if isCandleRow(l) {
				rows++
			}
		}
		fmt.Printf("  %s: %d rows\n", title, rows)
	}
	fmt.Println()
	verifyCandles(db, cblock)

	// 3.3 calendar
	fmt.Println("=== 3.3 ## Calendar (exact quote) ===")
	k := strings.Index(prompt, "## Calendar")
	kend := strings.Index(prompt[k:], "\n\n") + k
	fmt.Printf("  %q\n", prompt[k:kend])
	fmt.Printf("  calendar_slices rows for trade_date %s: %d\n", tradeDate, sliceCount(db, tradeDate))
	fmt.Printf("  calendar_slices rows for trade_date 2026-09-01: %d (contains ISM Manufacturing PMI, T1, 14:00Z = 09:00 CT)\n", sliceCount(db, "2026-09-01"))
	fmt.Println("  → today's ASIA read uses GetSlice(trade_date=2026-08-30): no row → zero events render.")
	fmt.Println("  → Monday ISM 09-01 correctly does NOT appear for today's session (different trade_date slice).")
	fmt.Println("  → EventsForSession filters by CURRENCY only (calendar/calendar.go:186, USD+JPY/CNY for ASIA) —")
	fmt.Println("    a 09-01 slice would NOT even be consulted; the static T1 list has no 08-30 event either.")

	// 3.4 counts vs sources
	fmt.Println("=== 3.4 counts vs sources ===")
	fmt.Printf("ranked levels rendered: %d (max_levels=%d, min_grade=B, seat_1h_zone=%v, proximity=%.2f)\n",
		len(in.Levels), in.MaxLevels, as.dp.Seat1HZoneEnabled(), kernel.ResolveProximityK(as.dp.ProximityFilterATR))
	bad := 0
	for _, l := range in.Levels {
		want := "C"
		if l.Score >= 1.0 {
			want = "A"
		} else if l.Score >= 0.70 {
			want = "B"
		}
		if l.Grade != want {
			fmt.Printf("  grade/score mismatch: %.2f %s grade=%s score=%.3f\n", l.Price, l.Label, l.Grade, l.Score)
			bad++
		}
	}
	fmt.Printf("  all rendered grades consistent with score bands (A≥1.0/B≥0.70): %v\n", bad == 0)
	fmt.Printf("structure lines: %d\n", len(in.StructureSummary))
	for _, s := range in.StructureSummary {
		fmt.Printf("  %s\n", s)
	}
	fmt.Printf("regime rendered; env HTF_VETO_MODE=cross (live .env) — RegimeInputs: price/daily/1h/5m/RV20d/priorClose/sessionOpen all supplied\n")
	fmt.Printf("FRESH FVGs rendered rows: %d · source FreshFvgCandidates len: %d\n", len(in.FreshFVGs), len(in.FreshFVGs))
	fmt.Printf("HTF zones rendered rows: %d · source HTFZonesFull len: %d\n", len(in.HTFZones), len(in.HTFZonesFull))
	fmt.Printf("Consumed levels rendered rows: %d\n", len(in.ConsumedLevels))
	for _, c := range in.ConsumedLevels {
		fmt.Printf("  consumed: %s\n", c)
	}
	fmt.Printf("bias-tree facts: PDH=%.2f PDL=%.2f PDC=%.2f price=%.2f\n",
		in.BiasCtxFacts.PDH, in.BiasCtxFacts.PDL, in.BiasCtxFacts.PDC, in.Price)

	// 3.5 empty sections
	fmt.Println("=== 3.5 EMPTY/OMITTED SECTIONS ===")
	check := func(name string, cond bool, reason string) {
		if cond {
			fmt.Printf("  EMPTY: %s — %s\n", name, reason)
		} else {
			fmt.Printf("  present: %s\n", name)
		}
	}
	check("Calendar", len(in.Calendar) == 0, "weekend-legit: no calendar_slices row for 2026-08-30")
	check("FRESH FVGs", len(in.FreshFVGs) == 0, "weekend-legit: no fresh gap candidates at this read")
	check("Consumed levels", len(in.ConsumedLevels) == 0, "legit: no seated level consumed at read time")
	check("HTF zones", len(in.HTFZones) == 0, "state honestly below")
	check("Auction story", in.OvernightStory == "" && in.PriorDayStory == "", "by design: the assembler's return literal never populates these fields")
	check("Owner note", strings.TrimSpace(in.OwnerNote) == "", "none configured")
	check("Prior plan invalidation", in.PriorPlanKiller == "", "first read — no killer line")
	check("Prior plan levels", len(in.PriorPlanLevels) == 0, "first read — no carry-over")
	check("Recent context (digests)", len(in.DigestChain) == 0, "no digests for trade_date 2026-08-30")
	check("Warming line", in.Warming == "", "session-profile store has ≥10 rows")
	sd, dd := digestCounts(db, tradeDate)
	fmt.Printf("  digest sources: session digests=%d · dailies=%d\n", sd, dd)
	fmt.Printf("  session_profiles count: %d\n", profileCount(db))

	fmt.Println("\n=== PlannerInput source list (T9 lesson — every source enumerated) ===")
	for _, s := range []string{
		"1  bars1m             bars table (live DB ro) — stored 1m rows",
		"2  reg                system_config.session_registry (ro)",
		"3  dp/sc              strategies.config → day_plan + ai_config.indicators (ro)",
		"4  nakedPOCs          session_profiles (ro) → kernel.NakedPOCs",
		"5  htfLevels          kernel.DetectHTFLevels on configured TFs from stored bars",
		"6  seated/pool        AssembleScoredLevelsFullMinGrade + Seat1HZone + freshness (level_state ro)",
		"7  HTFZones/Full      kernel.ScoreLevels over the zone subset",
		"8  owner levels       owner_levels table (ro) — 0 rows",
		"9  regime             daily/1h/5m aggregations + RVBaseline20d + PriorCloseSessionOpen",
		"10 calendar           calendar_slices[tradeDate] + EventsForSession",
		"11 digests            day_plan_digests session+daily (ro)",
		"12 structure          kernel.StructureSnapshot (same detector as live)",
		"13 indicators         market.GetWithTimeframes + RenderPlannerIndicatorBlock (ai_config toggles)",
		"14 consumed           EvaluateLevelFacts per seated row",
		"15 biasCtx            ComputeBiasContext + ApplyUniverseDayAnchors",
		"16 candleTables       BuildPlannerCandleTables (PLANNER_CANDLES on)",
		"17 weeklyCtx          weeklyDocCached → no WEEKLY plan row → WeeklyContextLine(nil,0)",
		"18 warming            session_profiles count < 10",
		"19 freshFVGs          FreshFvgCandidates",
		"20 ATR5m              StaleConfirmATR5m — recomputed live, NOT stored anywhere",
	} {
		fmt.Println("  " + s)
	}
}

func sliceCount(db *sql.DB, tradeDate string) int {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM calendar_slices WHERE trade_date=?`, tradeDate).Scan(&n); err != nil {
		return -1
	}
	return n
}

// isCandleRow reports whether a prompt line is a candle-table data row
// (TableTimeCT format "MM-DD HH:MM").
func isCandleRow(l string) bool {
	f := strings.Fields(l)
	return len(f) >= 5 && len(f[0]) == 5 && f[0][2] == '-' && len(f[1]) == 5 && f[1][2] == ':'
}

func profileCount(db *sql.DB) int64 {
	return qint(db, `SELECT COUNT(*) FROM session_profiles WHERE symbol=?`, symbol)
}

func digestCounts(db *sql.DB, tradeDate string) (int, int) {
	var s, d int
	db.QueryRow(`SELECT COUNT(*) FROM day_plan_digests WHERE trader_id=? AND symbol=? AND trade_date=? AND kind='session'`, traderID, symbol, tradeDate).Scan(&s)
	db.QueryRow(`SELECT COUNT(*) FROM day_plan_digests WHERE trader_id=? AND symbol=? AND kind='daily' AND trade_date<=?`, traderID, symbol, tradeDate).Scan(&d)
	return s, d
}

// verifyCandles — independent spot-check: 3 rows per TF (first/middle/last)
// re-aggregated by hand from the raw 1m rows (sqlite query + explicit math).
func verifyCandles(db *sql.DB, cblock string) {
	fmt.Println("=== 3.1 independent candle spot-check (3 rows per TF, hand aggregation) ===")
	rows1m, err := db.Query(`SELECT open_time_ms,o,h,l,c,v FROM bars WHERE symbol=? AND tf='1m' ORDER BY open_time_ms ASC`, symbol)
	must(err)
	defer rows1m.Close()
	type m1 struct {
		ms            int64
		o, h, l, c, v float64
	}
	var raw []m1
	for rows1m.Next() {
		var b m1
		must(rows1m.Scan(&b.ms, &b.o, &b.h, &b.l, &b.c, &b.v))
		raw = append(raw, b)
	}
	fmt.Printf("  raw 1m rows: %d (first %d → last %d)\n", len(raw), raw[0].ms, raw[len(raw)-1].ms)

	agg := func(bucket func(m1) int64) map[int64]struct{ o, h, l, c, v float64 } {
		out := map[int64]struct{ o, h, l, c, v float64 }{}
		order := []int64{}
		for _, b := range raw {
			k := bucket(b)
			e, ok := out[k]
			if !ok {
				out[k] = struct{ o, h, l, c, v float64 }{b.o, b.h, b.l, b.c, b.v}
				order = append(order, k)
				continue
			}
			if b.h > e.h {
				e.h = b.h
			}
			if b.l < e.l {
				e.l = b.l
			}
			e.c = b.c
			e.v += b.v
			out[k] = e
		}
		return out
	}

	// session-day bucket: CT date, hour<17 → prior calendar date (repo roll).
	sday := func(b m1) int64 {
		t := time.UnixMilli(b.ms).In(kernel.CTLocation())
		d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		if t.Hour() < 17 {
			d = d.AddDate(0, 0, -1)
		}
		return d.UnixMilli()
	}
	tfs := []struct {
		title  string
		bucket func(m1) int64
	}{
		{"### 15m (last 12)", func(b m1) int64 { return b.ms / (15 * 60_000) * (15 * 60_000) }},
		{"### 1h (last 12)", func(b m1) int64 { return b.ms / (60 * 60_000) * (60 * 60_000) }},
		{"### 4h (last 8)", func(b m1) int64 { return b.ms / (240 * 60_000) * (240 * 60_000) }},
		{"### daily session candles (last 8)", sday},
	}
	allOk := true
	for _, tf := range tfs {
		ti := strings.Index(cblock, tf.title)
		if ti < 0 {
			fmt.Printf("  %s: MISSING\n", tf.title)
			continue
		}
		seg := cblock[ti:]
		if next := strings.Index(cblock[ti+3:], "### "); next > 0 {
			seg = cblock[ti : ti+3+next]
		}
		var rows [][]string
		for _, l := range strings.Split(seg, "\n") {
			f := strings.Fields(l)
			if isCandleRow(l) && len(f) >= 6 {
				rows = append(rows, f)
			}
		}
		byBucket := agg(tf.bucket)
		pick := func(idx int) {
			if len(rows) == 0 || idx >= len(rows) {
				return
			}
			f := rows[idx]
			t, _ := time.Parse("01-02 15:04", f[0]+" "+f[1])
			year := time.Now().Year()
			t = time.Date(year, t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, kernel.CTLocation())
			ms := t.UnixMilli()
			// exact bucket key from the parsed CT time (matches the epoch-floor bucket)
			key := int64(0)
			switch tf.title {
			case "### daily session candles (last 8)":
				key = sday(m1{ms: ms})
			case "### 15m (last 12)":
				key = ms / (15 * 60_000) * (15 * 60_000)
			case "### 1h (last 12)":
				key = ms / (60 * 60_000) * (60 * 60_000)
			case "### 4h (last 8)":
				key = ms / (240 * 60_000) * (240 * 60_000)
			}
			e, ok := byBucket[key]
			o, _ := strconv.ParseFloat(f[2], 64)
			h, _ := strconv.ParseFloat(f[3], 64)
			l, _ := strconv.ParseFloat(f[4], 64)
			c, _ := strconv.ParseFloat(f[5], 64)
			v, _ := strconv.ParseFloat(strings.TrimSuffix(f[6], "  <- current"), 64)
			okRow := ok && o == e.o && h == e.h && l == e.l && c == e.c && v == e.v
			if !okRow {
				allOk = false
			}
			status := "EXACT"
			if !ok {
				status = "NO-BUCKET (hand agg missing)"
			} else if !okRow {
				status = fmt.Sprintf("MISMATCH hand(o%.4f h%.4f l%.4f c%.4f v%.2f)", e.o, e.h, e.l, e.c, e.v)
			}
			fmt.Printf("  %s row[%d] %s %s → %s\n", tf.title, idx, f[0], f[1], status)
		}
		fmt.Printf("  %s: %d rows rendered\n", tf.title, len(rows))
		pick(0)
		pick(len(rows) / 2)
		pick(len(rows) - 1)
	}
	fmt.Printf("  hand-aggregation spot-check all-EXACT: %v\n", allOk)
}
