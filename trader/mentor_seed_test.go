package trader

import (
	"sort"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vl/kernel"
	"vl/store"
	"vl/store/sqlitedriver"
)

// TestMentorSeedAtStart is the P0 splice call-site test (CTO 1791030462901):
// a COLD store seeds with nothing and the placement gate refuses with the
// NAMED source; a SEEDED store passes and the per-source depth seam serves the
// seed's own numbers. Mutant: removing the seam wiring in mentorSeedAtStart
// makes the seeded case refuse (n/a) → RED.
func TestMentorSeedAtStart(t *testing.T) {
	t.Run("cold store refuses with the named source", func(t *testing.T) {
		st := mentorSeedStore(t)
		at := &AutoTrader{
			id: "t-seed-cold",
			config: AutoTraderConfig{
				StrategyConfig: &store.StrategyConfig{
					RiskControl: store.RiskControlConfig{MentorMode: true},
				},
			},
			store: st,
		}
		wireMentorPlacementSeams(t)
		at.mentorSeedAtStart()

		if ms := at.mentorEval.SourcesMissing(); len(ms) == 0 {
			t.Fatalf("a cold store must leave the evaluator seeded-but-missing, got none")
		}
		missing := strings.Join(at.mentorSourcesMissing(), ", ")
		for _, want := range []string{
			"4h EMA34 (0/102)", "1m EMA34 (0/102)",
			"1h level set (0/2)", "today session (0/1)", "closed 15m (0/1)",
		} {
			if !textHas(missing, want) {
				t.Fatalf("cold store refusal must name %q: %q", want, missing)
			}
		}
	})

	t.Run("seeded store passes", func(t *testing.T) {
		// FLAKE-SEED-CLOCK: the seed reads the wall clock. Pin it at fixed
		// mid-session instants through the mentorNowSource seam so the result
		// is deterministic across the midnight CT rollover. Three instants:
		// 00:30 (the current 4h bucket started 21:00 yesterday, so the
		// calendar-day depth is empty without the fixture's midnight bar),
		// 12:00 (plain mid-session) and 16:30 (right before the 17:00 Globex
		// flip).
		for _, tc := range mentorSeedClockInstants(t) {
			t.Run(tc.name, func(t *testing.T) {
				st := mentorSeedStore(t)
				now := tc.instant.UnixMilli()
				mentorNowSource = func() time.Time { return tc.instant }
				t.Cleanup(func() { mentorNowSource = nil })
				bh := store.NewBarHistoryStore(st.GormDB())
				rows1m := mentorSeedBars1m(now)
				if err := bh.InsertBars(rows1m); err != nil {
					t.Fatalf("InsertBars: %v", err)
				}
				at := &AutoTrader{
					id: "t-seed-warm",
					config: AutoTraderConfig{
						StrategyConfig: &store.StrategyConfig{
							RiskControl: store.RiskControlConfig{MentorMode: true},
						},
					},
					store: st,
				}
				wireMentorPlacementSeams(t)
				at.mentorSeedAtStart()

				if ms := at.mentorEval.SourcesMissing(); len(ms) != 0 {
					t.Fatalf("a seeded store must leave no missing source: %v", ms)
				}
				if missing := strings.Join(at.mentorSourcesMissing(), ", "); textHas(missing, "history:") {
					t.Fatalf("a seeded store must not refuse on history depth: %q", missing)
				}
				// the seam serves the seed's own depths (not the test fallback
				// 9999): the 1m EMA 34 depth is the evaluator's own closed-1m
				// count, which since REL10-438-FIXES #3 is read from the
				// stitched series (the stitched tail is the current contract,
				// so the EMA value is unchanged; the count drops the newest
				// contract's pre-roll sparse snapshots).
				want1m := at.mentorEval.Depths()["1m EMA34"]
				if d, ok := mentorSourceDepth("1m EMA34"); !ok || d != want1m {
					t.Fatalf("1m EMA34 depth from the seed: %d/%v, want %d/true", d, ok, want1m)
				}
				if d, ok := mentorSourceDepth("4h EMA34"); !ok || d < 102 || d > 110 {
					t.Fatalf("4h EMA34 depth from the seed: %d/%v, want 102..110/true", d, ok)
				}
			})
		}
	})
}

// TestMentorSeedAtStartFirstTickDoesNotReAdvance (DS-105 P3 call-site pin) —
// after mentorSeedAtStart seeds the trigger/HTF lines via SeedFull, the FIRST
// evaluator tick over the tail window (a subset of the seeded history) must not
// re-advance the committed buckets. The kernel pins Seed+Tick directly; this
// drives the trader call site (mentorSeedAtStart → SeedFull) and then ticks the
// evaluator the way the tick path does.
func TestMentorSeedAtStartFirstTickDoesNotReAdvance(t *testing.T) {
	st := mentorSeedStore(t)
	now := time.Date(2026, time.September, 16, 12, 0, 0, 0, kernel.CTLocation())
	mentorNowSource = func() time.Time { return now }
	t.Cleanup(func() { mentorNowSource = nil })

	bh := store.NewBarHistoryStore(st.GormDB())
	rows := mentorSeedBars1m(now.UnixMilli())
	if err := bh.InsertBars(rows); err != nil {
		t.Fatalf("InsertBars: %v", err)
	}
	at := &AutoTrader{
		id: "t-seed-tick",
		config: AutoTraderConfig{
			StrategyConfig: &store.StrategyConfig{
				RiskControl: store.RiskControlConfig{MentorMode: true},
			},
		},
		store: st,
	}
	wireMentorPlacementSeams(t)
	at.mentorSeedAtStart()

	if at.mentorEval == nil {
		t.Fatal("mentorSeedAtStart must build the evaluator")
	}
	beforeTrigger := at.mentorEval.State.Trigger.LastBucket
	before4h := at.mentorEval.State.HTF.FourH.LastBucket
	if beforeTrigger == 0 || before4h == 0 {
		t.Fatalf("the seed must commit trigger/HTF buckets: trigger=%d 4h=%d", beforeTrigger, before4h)
	}

	// The first tick: the tail window, the same subset the live tick passes.
	kline := storeBarsToKlines(rows, 60_000)
	tail := kline[len(kline)-1500:]
	at.mentorEval.Tick(tail, now.UnixMilli())

	if at.mentorEval.State.Trigger.LastBucket != beforeTrigger {
		t.Fatalf("first tick re-advanced 5m buckets: %d -> %d", beforeTrigger, at.mentorEval.State.Trigger.LastBucket)
	}
	if at.mentorEval.State.HTF.FourH.LastBucket != before4h {
		t.Fatalf("first tick re-advanced 4h buckets: %d -> %d", before4h, at.mentorEval.State.HTF.FourH.LastBucket)
	}
}

// mentorSeedClockInstants is the FLAKE-SEED-CLOCK proof matrix: three fixed
// mid-session instants on one CT day, pinned through mentorNowSource. The date
// is FIXED IN THE PAST (the same convention as the other seed fixtures) so the
// test is deterministic forever — and so the mutant that drops the seam reads
// the real wall-clock day, finds no fixture bars on that day, and goes RED.
func mentorSeedClockInstants(t *testing.T) []struct {
	name    string
	instant time.Time
} {
	t.Helper()
	ct := kernel.CTLocation()
	at := func(h, m int) time.Time { return time.Date(2026, time.September, 16, h, m, 0, 0, ct) }
	return []struct {
		name    string
		instant time.Time
	}{
		{"00_30_after_midnight", at(0, 30)},
		{"12_00_midsession", at(12, 0)},
		{"16_30_before_globex_flip", at(16, 30)},
	}
}

// mentorSeedStore builds an in-memory Store with ONLY the bars table (read-only
// seed path; data.db is never involved).
func mentorSeedStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := gorm.Open(sqlitedriver.GormDialector(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm: %v", err)
	}
	if err := store.NewBarHistoryStore(db).Migrate(); err != nil {
		t.Fatalf("bars migrate: %v", err)
	}
	st, err := store.NewFromGorm(db)
	if err != nil {
		t.Fatalf("NewFromGorm: %v", err)
	}
	return st
}

// mentorSeedBars1m builds 430 hours of closed 1m bars (25,800 rows) ending one
// hour before the current 4h bucket start (22:00 UTC = 17:00 CT anchor) — 107
// fully closed 4h buckets, above the 102 warm-up — plus one bar on today's
// session (00:00 CT = 05:00 UTC, read by OpenTime) for the "today session"
// source. Closes alternate hour by hour so the aggregated 1H RTH level set
// draws ≥2 candles (the 1m-only P1 seed builds 1h from these rows).
func mentorSeedBars1m(now int64) []store.BarHistoryDB {
	return mentorSeedBars1mHours(now, 430)
}

// mentorSeedBars1mHours is mentorSeedBars1m with the history length in hours.
func mentorSeedBars1mHours(now int64, hours int64) []store.BarHistoryDB {
	const hourMs = int64(3600_000)
	const bucketMs = int64(4 * 3600_000)
	b0 := now - ((now - 22*hourMs) % bucketMs) // current 4h bucket start
	var rows []store.BarHistoryDB
	for h := int64(1); h <= hours; h++ {
		h0 := b0 - h*hourMs
		green := h%2 == 0
		for m := int64(0); m < 60; m++ {
			o := h0 + m*60_000
			if o+60_000 > now {
				continue // never seed a bar that has not closed yet
			}
			base := 29000.0 - float64(h)*0.5 + float64(m)*0.1
			c := base + 1
			if !green {
				c = base - 1
			}
			rows = append(rows, store.BarHistoryDB{
				Symbol: "MNQ", TF: "1m", Contract: "MNQ 12-26",
				Source: store.BarSourceLive, OpenTimeMs: o,
				O: base, H: base + 5, L: base - 5, C: c, V: 1,
			})
		}
	}
	// "today's session" for the seed is the Globex session, which opens at
	// 17:00 CT (tradingDayKey flips at 17:00). The old midnight-CT anchor
	// broke after 17:00 CT: `now` is in the NEXT trading-day key while a
	// 00:05 CT bar is in the previous one, so the seed read the session as
	// missing and refused every entry.
	t := time.UnixMilli(now).In(kernel.CTLocation())
	sessOpen := time.Date(t.Year(), t.Month(), t.Day(), 17, 0, 0, 0, kernel.CTLocation())
	if sessOpen.After(t) {
		sessOpen = sessOpen.AddDate(0, 0, -1)
	}
	extra := sessOpen.Add(time.Minute).UnixMilli()
	have := false
	for _, r := range rows {
		if r.OpenTimeMs == extra {
			have = true
			break
		}
	}
	if !have {
		rows = append(rows, store.BarHistoryDB{
			Symbol: "MNQ", TF: "1m", Contract: "MNQ 12-26",
			Source: store.BarSourceLive, OpenTimeMs: extra,
			O: 29999, H: 30004, L: 29994, C: 30001, V: 1,
		})
	}
	// FLAKE-SEED-CLOCK: the per-source DEPTH seam ("today session" in
	// SeedDepths) reads the CALENDAR day (dayStartCT, midnight CT) while the
	// refusal check (seedDepthOf) reads the trading-day key (17:00 CT flip).
	// In the 00:00–01:00 CT window the current 4h bucket started 21:00 the
	// day before, so the regular bars stop short of midnight and the depth
	// seam reports "today session (0/1)" — add one bar at 00:01 CT of the
	// calendar day so both readers agree at every instant.
	cal := time.UnixMilli(now).In(kernel.CTLocation())
	calBar := time.Date(cal.Year(), cal.Month(), cal.Day(), 0, 1, 0, 0, kernel.CTLocation()).UnixMilli()
	if calBar < now {
		haveCal := false
		for _, r := range rows {
			if r.OpenTimeMs == calBar {
				haveCal = true
				break
			}
		}
		if !haveCal {
			rows = append(rows, store.BarHistoryDB{
				Symbol: "MNQ", TF: "1m", Contract: "MNQ 12-26",
				Source: store.BarSourceLive, OpenTimeMs: calBar,
				O: 29998, H: 30003, L: 29993, C: 30000, V: 1,
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].OpenTimeMs < rows[j].OpenTimeMs })
	return rows
}
