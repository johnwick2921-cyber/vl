package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── m445 round 4 (D1): a roll seam is not a hole ───────────────────────────
//
// The REAL 2026-09-14 shape, built into a real bar store and run through the
// production seed (mentorSeedAtStart → mentorFullKeyLevelHistory → noteSeedSeries
// → dataIntegrityVerdictAt): MNQ 09-26 has a 22-minute RTH hole (09:36–09:57 CT,
// its last stored bar 10:33), MNQ 12-26 STORES those minutes (source=historical)
// and the live bars after. The stitch cuts the older at its last bar and the head
// after it, so the series loses them although the market was recorded. Round 3
// paused every mentor trader for ~45 days on it with a remedy (an NT8 reload) that
// cannot work.

func r4CT(mo time.Month, d, h, m int) time.Time {
	return time.Date(2026, mo, d, h, m, 0, 0, kernel.CTLocation())
}

// r4Rows builds one contract's open 1m rows over [from, to) (CME-open minutes
// only), skipping skip(m), plus its native 1h rows over the same span.
func r4Rows(contract, source string, base float64, from, to time.Time, skip func(time.Time) bool) []store.BarHistoryDB {
	var rows []store.BarHistoryDB
	for m := from; m.Before(to); m = m.Add(time.Minute) {
		if !kernel.IsCMEOpen(m) || (skip != nil && skip(m)) {
			continue
		}
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: "1m", Contract: contract, Source: source, OpenTimeMs: m.UnixMilli(),
			O: base, H: base + 1, L: base - 1, C: base})
	}
	return rows
}

func r4Rows1H(contract string, base float64, from, to time.Time) []store.BarHistoryDB {
	var rows []store.BarHistoryDB
	for h := from.Truncate(time.Hour); h.Before(to); h = h.Add(time.Hour) {
		if !kernel.IsCMEOpen(h) {
			continue
		}
		rows = append(rows, store.BarHistoryDB{Symbol: "MNQ", TF: "1h", Contract: contract, Source: store.BarSourceLive, OpenTimeMs: h.UnixMilli(),
			O: base, H: base + 1, L: base - 1, C: base})
	}
	return rows
}

// r4Store fills a store with the two-contract 09-14 roll. headHasTheMinutes
// says whether MNQ 12-26 stores 09:36–09:57 (the real shape) or nobody does.
func r4Store(t *testing.T, at *AutoTrader, headHasTheMinutes bool) {
	t.Helper()
	bh := store.NewBarHistoryStore(at.store.GormDB())
	holeFrom, holeTo := r4CT(time.September, 14, 9, 36), r4CT(time.September, 14, 9, 58)
	inHole := func(m time.Time) bool { return !m.Before(holeFrom) && m.Before(holeTo) }
	end := r4CT(time.September, 16, 9, 59)
	var rows []store.BarHistoryDB
	// MNQ 09-26: live until its last stored bar 10:33 CT, with the 22-minute hole
	rows = append(rows, r4Rows("MNQ 09-26", store.BarSourceLive, 100, r4CT(time.September, 8, 17, 0), r4CT(time.September, 14, 10, 34), inHole)...)
	// MNQ 12-26: the live bars from 09:58, and (the real shape) the historical rows for the hole
	rows = append(rows, r4Rows("MNQ 12-26", store.BarSourceLive, 110, holeTo, end, nil)...)
	if headHasTheMinutes {
		rows = append(rows, r4Rows("MNQ 12-26", store.BarSourceHistorical, 110, holeFrom, holeTo, nil)...)
	}
	rows = append(rows, r4Rows1H("MNQ 09-26", 100, r4CT(time.September, 8, 17, 0), r4CT(time.September, 14, 10, 0))...)
	rows = append(rows, r4Rows1H("MNQ 12-26", 110, r4CT(time.September, 8, 17, 0), end)...)
	if err := bh.InsertBars(rows); err != nil {
		t.Fatal(err)
	}
}

func r4Seed(t *testing.T, headHasTheMinutes bool) (*AutoTrader, time.Time) {
	t.Helper()
	at := seedStateTrader(t)
	at.exchange = "ninjatrader"
	f := &intFake{blockedFor: func(int) bool { return false }} // the tracker says clear: only the seed leg is under test
	armIntegrityGate(t, f)
	r4Store(t, at, headHasTheMinutes)
	now := r4CT(time.September, 16, 10, 0)
	setMentorNowSource(func() time.Time { return now })
	at.mentorSeedAtStart()
	return at, now
}

func TestRollSeamMinutesStoredByTheHeadAreNotAHoleAtTheProductionSeed(t *testing.T) {
	at, now := r4Seed(t, true)
	if n := at.seedSeries.nBars; n == 0 {
		t.Fatal("setup: the seed measured no series")
	}
	for _, w := range []int{ntTrader.IntegrityIntradayOpenMin, ntTrader.IntegritySwingOpenMin} {
		if v := at.dataIntegrityVerdictAt(w, now, false); v.Blocked {
			t.Fatalf("MNQ 12-26 stores 09-14 09:36–09:57 CT: the roll cut must not read as lost data (window %d): %+v", w, v)
		}
	}
	if got := at.HealthSeedSeriesPause(now); len(got) != 0 {
		t.Fatalf("/api/health must not list a seam as a seeded hole: %v", got)
	}
	// a mentor arm crosses the chain unblocked by data (production call site)
	if reason, refused := at.admitEntry(admitIntent{Path: admitArm, MentorArm: true, Symbol: "MNQ", Action: "open_long", Now: now, Key: "k"}); refused && strings.HasPrefix(reason, "data_integrity: ") {
		t.Fatalf("a mentor arm must not be refused for the seam: %q", reason)
	}
}

// The negative control: the SAME 22 minutes stored by NO contract are lost data
// — the gate blocks, names them in CT, and the remedy text is one that works.
func TestRollSeamMinutesStoredByNobodyStillBlockWithAnHonestRemedy(t *testing.T) {
	at, now := r4Seed(t, false)
	v := at.dataIntegrityVerdictAt(ntTrader.IntegrityIntradayOpenMin, now, false)
	if !v.Blocked || v.Class != ntTrader.IntegrityClassHole || len(v.Windows) != 1 || v.Windows[0] != "09-14 09:36–09:58 CT" {
		t.Fatalf("minutes no contract stores are lost data: the mentor verdict must block and name them: %+v", v)
	}
	// the remedy must not send the owner to an NT8 reload (replays ~2000 bars, ~33 h)
	// nor promise an import fixes it without the restart that re-measures
	for _, bad := range []string{ntTrader.IntegrityAction, "Reload All Historical Data", "wait for the auto re-request"} {
		if strings.Contains(v.Reason, bad) {
			t.Fatalf("the remedy for a days-old seeded hole must not be %q (it cannot work): %q", bad, v.Reason)
		}
	}
	for _, want := range []string{"no NT8 reload reaches", "/api/admin/bars/import", "restart", "clears by itself 12-13"} {
		if !strings.Contains(v.Reason, want) {
			t.Fatalf("the refusal must say what clears it and when (%q missing): %q", want, v.Reason)
		}
	}
}

func TestRemedyTextsNameOnlyWhatWorks(t *testing.T) {
	retired := ntTrader.IntegrityHole{State: "retired"}
	seeded := ntTrader.IntegrityHole{State: "seeded"}
	fresh := ntTrader.IntegrityHole{State: "refilling"}
	spent := ntTrader.IntegrityHole{State: "unfilled", Unfillable: true}
	if fresh.Remedy() != ntTrader.IntegrityAction {
		t.Fatalf("a hole NT8 can still replay: the reload action, got %q", fresh.Remedy())
	}
	for name, h := range map[string]ntTrader.IntegrityHole{"retired": retired, "seeded": seeded} {
		r := h.Remedy()
		if strings.Contains(r, "Reload All Historical Data") || !strings.Contains(r, "no NT8 reload reaches") {
			t.Fatalf("%s: a stretch no reload reaches must not send the owner to a reload: %q", name, r)
		}
	}
	if !strings.Contains(spent.Remedy(), "restart the bot") {
		t.Fatalf("a hole whose auto re-requests are spent needs the reload AND the restart: %q", spent.Remedy())
	}
}

// The refusal text for a RETIRED tracker hole (outside NT8's replay) is built by
// integrityHoleReason — the one function every refusal and the owner push use.
func TestRetiredHoleRefusalTextSaysWhatClearsItAndWhen(t *testing.T) {
	clears := time.Date(2026, 11, 6, 13, 57, 0, 0, kernel.CTLocation()).UnixMilli()
	reason := integrityHoleReason("MNQ", ntTrader.IntegrityVerdict{Holes: []ntTrader.IntegrityHole{{
		State: "retired", Missing: 297, Window: "10-26 17:00–21:57 CT", Unfillable: true, ClearsAtMs: clears}}})
	for _, want := range []string{"10-26 17:00–21:57 CT", "retired", "clears by itself 11-06 13:57 CT", "no NT8 reload reaches", "/api/admin/bars/import"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("the refusal must contain %q: %q", want, reason)
		}
	}
	if strings.Contains(reason, "Reload All Historical Data") {
		t.Fatalf("a retired hole must not send the owner to a reload: %q", reason)
	}
	fresh := integrityHoleReason("MNQ", ntTrader.IntegrityVerdict{Holes: []ntTrader.IntegrityHole{{State: "refilling", Missing: 5, Window: "10-28 09:00–09:05 CT"}}})
	if !strings.Contains(fresh, ntTrader.IntegrityAction) {
		t.Fatalf("a hole NT8 can still replay keeps the reload action: %q", fresh)
	}
}
