package store

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func pepDayRow(trader, day string) PepDayDB {
	return PepDayDB{TraderID: trader, Day: day, Style: "A", Side: "long", Symbol: "MNQ", Contracts: 3, SignalID: "A|" + day + "|long|1"}
}

// One-trade-per-day is the DATABASE's: a claim survives closing and reopening
// the file, and the second claim for the same day is refused with the named
// error. MUTANT: ClaimEntry without the counted>=max check -> the second claim
// succeeds -> RED.
func TestPepDayClaimSurvivesARestartAndCapsTheDay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pep.db")
	st, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PepDay().ClaimEntry(pepDayRow("t1", "2026-10-29"), 1, 1000); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st2, err := New(path) // the restart
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	ds, err := st2.PepDay().Day("t1", "2026-10-29")
	if err != nil || ds.Counted != 1 {
		t.Fatalf("after the restart: %+v err %v, want 1 counted", ds, err)
	}
	if _, err := st2.PepDay().ClaimEntry(pepDayRow("t1", "2026-10-29"), 1, 2000); !errors.Is(err, ErrPepDayCapReached) {
		t.Fatalf("second claim same day = %v, want ErrPepDayCapReached", err)
	}
	// another CT day and another trader are independent
	if _, err := st2.PepDay().ClaimEntry(pepDayRow("t1", "2026-10-30"), 1, 3000); err != nil {
		t.Fatalf("next day: %v", err)
	}
	if _, err := st2.PepDay().ClaimEntry(pepDayRow("t2", "2026-10-29"), 1, 3000); err != nil {
		t.Fatalf("other trader: %v", err)
	}
	// a cap of 2 allows exactly two
	if _, err := st2.PepDay().ClaimEntry(pepDayRow("t3", "d"), 2, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := st2.PepDay().ClaimEntry(pepDayRow("t3", "d"), 2, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := st2.PepDay().ClaimEntry(pepDayRow("t3", "d"), 2, 3); !errors.Is(err, ErrPepDayCapReached) {
		t.Fatalf("third claim under a cap of 2 = %v", err)
	}
}

// Two racing claims for a cap of 1: exactly one wins - the database arbitrates,
// not a Go mutex.
func TestPepDayRacingClaimsHaveOneWinner(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "race.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := st.PepDay().ClaimEntry(pepDayRow("race", "d"), 1, int64(i)); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	ds, _ := st.PepDay().Day("race", "d")
	if wins != 1 || ds.Counted != 1 {
		t.Fatalf("wins %d, counted %d, want exactly 1", wins, ds.Counted)
	}
}

// Release frees a slot ONLY for a claim that never reached the wire; a
// sending row can never be released and keeps counting. MUTANT: Release
// without the state guard -> the sending row is freed -> RED.
func TestPepDayReleaseOnlyBeforeTheWire(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "rel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d := st.PepDay()
	r1, err := d.ClaimEntry(pepDayRow("t", "d"), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Release(r1.RowKey, "refused before the wire", 2); err != nil {
		t.Fatal(err)
	}
	if ds, _ := d.Day("t", "d"); ds.Counted != 0 {
		t.Fatalf("a released claim must not count: %+v", ds)
	}
	// the slot is reusable, and the released row is replaced, not duplicated
	r2, err := d.ClaimEntry(pepDayRow("t", "d"), 1, 3)
	if err != nil || r2.Slot != r1.Slot {
		t.Fatalf("re-claim: %+v err %v (slot %d)", r2, err, r1.Slot)
	}
	if err := d.MarkSending(r2.RowKey, 25001, 25081, 4); err != nil {
		t.Fatal(err)
	}
	if err := d.Release(r2.RowKey, "too late", 5); err == nil {
		t.Fatal("a sending row must NEVER be released")
	}
	if err := d.MarkSending(r2.RowKey, 1, 2, 6); err == nil {
		t.Fatal("sending twice must be refused")
	}
	ds, _ := d.Day("t", "d")
	if ds.Counted != 1 || ds.Last == nil || ds.Last.State != PepDaySending || ds.Last.Stop != 25001 {
		t.Fatalf("%+v", ds)
	}
	if _, err := d.ClaimEntry(pepDayRow("t", "d"), 1, 7); !errors.Is(err, ErrPepDayCapReached) {
		t.Fatalf("a sent entry keeps the day consumed: %v", err)
	}
}

func TestPepDayRefusesAnUnidentifiedClaimAndReadsFailLoud(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "id.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PepDay().ClaimEntry(PepDayDB{Day: "d"}, 1, 1); err == nil {
		t.Fatal("a claim without a trader id must be refused")
	}
	if _, err := st.PepDay().ClaimEntry(pepDayRow("t", "d"), 0, 1); err == nil {
		t.Fatal("a cap below 1 must be refused")
	}
	if _, err := st.PepDay().Day("", "d"); err == nil {
		t.Fatal("a read without a trader id must error")
	}
	_ = st.Close()
	if _, err := st.PepDay().Day("t", "d"); err == nil {
		t.Fatal("a read on a closed database must error (the trader fails closed on it)")
	}
}

func TestTradingModeNamesTheMode(t *testing.T) {
	on, off := true, false
	if (*StrategyConfig)(nil).TradingMode() != TradingModeAI || (&StrategyConfig{}).TradingMode() != TradingModeAI {
		t.Fatal("default is the AI mode")
	}
	m := &StrategyConfig{}
	m.RiskControl.MentorMode = true
	if m.TradingMode() != TradingModeMentor {
		t.Fatal("mentor")
	}
	p := &StrategyConfig{Pep: &PepConfig{Enabled: &on}}
	if p.TradingMode() != TradingModePep {
		t.Fatal("pep")
	}
	if (&StrategyConfig{Pep: &PepConfig{Enabled: &off}}).TradingMode() != TradingModeAI {
		t.Fatal("pep saved OFF is the AI mode")
	}
	p.RiskControl.MentorMode = true
	if p.TradingMode() != "conflict" {
		t.Fatal("pep+mentor is a conflict, never painted as either mode")
	}
}
