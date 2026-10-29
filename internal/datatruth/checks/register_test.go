package checks

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/kernel"
)

type fakeSrc struct{}

func (fakeSrc) Bars(string, int) ([]Bar, error) { return nil, nil }
func (fakeSrc) Partner(string) string           { return "" }
func (fakeSrc) Open() OpenFn                    { return nil }
func (fakeSrc) Contract(string) (ContractFacts, error) {
	return ContractFacts{Live: "MNQ 12-26", History: "MNQ 12-26", Window: []string{"MNQ 12-26"}}, nil
}

// D6: the swing and levels checks both call SwingInputs - a full store read of
// ~200 four-hour buckets of 1m/5m - on every Evaluate. They share ONE read per
// symbol-minute now; a failed read is never cached.
func TestSwingInputsAreReadOncePerSymbolMinute(t *testing.T) {
	datatruth.Reset()
	defer datatruth.Reset()
	calls := 0
	fail := false
	RegisterAll(Config{Src: fakeSrc{},
		Unrequired: []string{CheckIngestLoss},
		SwingInputs: func(string, time.Time) ([]Bar, []Bar, float64, error) {
			calls++
			if fail {
				return nil, nil, 0, errors.New("store down")
			}
			return nil, nil, 0, nil
		}})
	t0 := time.Date(2026, 10, 27, 14, 3, 5, 0, time.UTC)
	for i := 0; i < 6; i++ { // six Evaluates inside one minute, both checks each
		datatruth.Evaluate("MNQ", t0.Add(time.Duration(i)*time.Second))
	}
	if calls != 1 {
		t.Fatalf("SwingInputs called %d times in one minute for two checks x six evaluations, want 1", calls)
	}
	datatruth.Evaluate("MNQ", t0.Add(time.Minute))
	if calls != 2 {
		t.Fatalf("a new minute reads again: calls=%d", calls)
	}
	fail = true
	datatruth.Evaluate("MNQ", t0.Add(2*time.Minute))
	before := calls
	datatruth.Evaluate("MNQ", t0.Add(2*time.Minute+time.Second))
	if calls == before {
		t.Fatal("a failed read must be retried, not cached")
	}
	b, rs := datatruth.Evaluate("MNQ", t0.Add(2*time.Minute+2*time.Second))
	if !b {
		t.Fatalf("an unreadable store blocks (fail closed): %+v", rs)
	}
}

// D5: a registration whose input is missing is a stub that blocks, not a gap.
func TestRegisterAllWithoutInputsLeavesRequiredChecksBlocking(t *testing.T) {
	datatruth.Reset()
	defer datatruth.Reset()
	RegisterAll(Config{Src: fakeSrc{}}) // no ledger, no verifier, no swing reader
	_, rs := datatruth.Evaluate("MNQ", time.Date(2026, 10, 27, 14, 3, 0, 0, time.UTC))
	got := map[string]bool{}
	for _, r := range rs {
		got[r.Check] = true
	}
	for _, n := range []string{CheckIngestLoss, CheckLookbackSwing4h, CheckLookbackLevels, CheckLookbackSession} {
		if !got[n] {
			t.Errorf("%s must block when its inputs were never wired: %+v", n, rs)
		}
	}
	datatruth.Reset()
	RegisterAll(Config{Src: fakeSrc{}, Unrequired: []string{CheckIngestLoss, CheckLookbackSwing4h, CheckLookbackLevels, CheckLookbackSession}})
	_, rs = datatruth.Evaluate("MNQ", time.Date(2026, 10, 27, 14, 3, 0, 0, time.UTC))
	for _, r := range rs {
		if r.Check == CheckIngestLoss || r.Check == CheckLookbackSwing4h || r.Check == CheckLookbackLevels || r.Check == CheckLookbackSession {
			t.Errorf("explicitly unrequired %s must not block a replay: %+v", r.Check, r)
		}
	}
}

// calSrc is fakeSrc with the real CME calendar.
type calSrc struct{ fakeSrc }

func (calSrc) Open() OpenFn {
	return func(ms int64) bool { return kernel.IsCMEOpen(time.UnixMilli(ms)) }
}

// Release #55 review (swing cache): the swing verdict was cached per 4h bucket,
// so an owner's repair (POST /api/admin/bars/import: "read back within a
// minute") did not lift a blocking verdict for up to four hours. The cache is now
// keyed by a fingerprint of the inputs: a changed store is judged on the next
// read. Production call site: RegisterAll -> datatruth.Evaluate.
//
// MUTANT S1: swingCheck's cache key drops the fingerprint (sig) -> RED.
func TestSwing4hRepairLiftsThePauseWithinTheSameBucket(t *testing.T) {
	datatruth.Reset()
	defer datatruth.Reset()
	loc := kernel.CTLocation()
	now0 := time.Date(2026, 10, 14, 10, 0, 30, 0, loc)
	from := time.Date(2026, 9, 14, 17, 0, 0, 0, loc)
	good1 := dstPerfectBars(from, now0, 1)
	good5 := dstPerfectBars(from, now0, 5)
	// a 4h stretch lost in BOTH frames inside the warm-up (the unrepairable shape)
	holeFrom := time.Date(2026, 10, 7, 13, 0, 0, 0, loc).UnixMilli()
	holeTo := holeFrom + 4*3600_000
	cut := func(bars []Bar) []Bar {
		var o []Bar
		for _, b := range bars {
			if b.T < holeFrom || b.T >= holeTo {
				o = append(o, b)
			}
		}
		return o
	}
	cur1, cur5 := cut(good1), cut(good5)
	RegisterAll(Config{Src: calSrc{}, Unrequired: []string{CheckIngestLoss},
		SwingInputs: func(string, time.Time) ([]Bar, []Bar, float64, error) { return cur1, cur5, 0, nil }})
	swing := func(now time.Time) (bool, string) {
		_, rs := datatruth.Evaluate("MNQ", now)
		for _, r := range rs {
			if r.Check == CheckLookbackSwing4h {
				return true, r.Detail
			}
		}
		return false, ""
	}
	if b, why := swing(now0); !b {
		t.Fatal("fixture: a 4h bucket lost in both frames inside the warm-up must pause SWING4H")
	} else if !strings.Contains(why, "exist in no frame") {
		t.Fatalf("paused for the wrong reason: %s", why)
	}
	// the owner repairs the store; two minutes later, SAME 4h bucket
	cur1, cur5 = good1, good5
	if b, why := swing(now0.Add(2 * time.Minute)); b {
		t.Fatalf("the repair must lift the SWING4H pause on the next read, not at the next 4h bucket: %s", why)
	}
}

// Release #55 review (P3): the prior-session levels check returned CLEAR when
// neither frame held a single bar in the five prior sessions - an unknown read
// as clear. It now blocks while the market is open.
//
// MUTANT L1: levelsCheck falls off the loop with `return false` again -> RED.
func TestLevelsCheckFailsClosedOnAnEmptyStore(t *testing.T) {
	datatruth.Reset()
	defer datatruth.Reset()
	loc := kernel.CTLocation()
	RegisterAll(Config{Src: calSrc{}, Unrequired: []string{CheckIngestLoss},
		SwingInputs: func(string, time.Time) ([]Bar, []Bar, float64, error) { return nil, nil, 0, nil }})
	open := time.Date(2026, 10, 14, 10, 0, 30, 0, loc)
	closed := time.Date(2026, 10, 17, 10, 0, 30, 0, loc) // Saturday
	_, rs := datatruth.Evaluate("MNQ", open)
	got := false
	for _, r := range rs {
		if r.Check == CheckLookbackLevels {
			got = true
			if !strings.Contains(r.Detail, "cannot be verified") {
				t.Errorf("reason: %s", r.Detail)
			}
		}
	}
	if !got {
		t.Fatalf("an empty store with the market open must block the levels check: %+v", rs)
	}
	_, rs = datatruth.Evaluate("MNQ", closed)
	for _, r := range rs {
		if r.Check == CheckLookbackLevels {
			t.Errorf("a closed market asks for no entry: the levels check must not raise on an empty store: %+v", r)
		}
	}
}

// framesSrc serves a fixed bar set per timeframe.
type framesSrc struct {
	calSrc
	bars map[int][]Bar
}

func (f framesSrc) Bars(_ string, tf int) ([]Bar, error) { return f.bars[tf], nil }

// Release #55 review (per-reader holds): the lookback check says WHICH readers a
// hole concerns. A hole in the 3m/5m rings only the AI path's indicators read is
// not a mentor trader's pause; a hole in the 1m ring both read concerns everyone.
//
// MUTANT R2: lookbackCheck returns nil readers for every blocked spec -> RED.
func TestLookbackNamesTheReadersOfTheHole(t *testing.T) {
	loc := kernel.CTLocation()
	now := time.Date(2026, 10, 14, 10, 0, 30, 0, loc)
	from := time.Date(2026, 10, 12, 17, 0, 0, 0, loc)
	full := map[int][]Bar{1: dstPerfectBars(from, now, 1), 3: dstPerfectBars(from, now, 3), 5: dstPerfectBars(from, now, 5)}
	for i := range full[1] { // a choppy tape: a missing minute can matter
		full[1][i].C = 30000 + 3*float64(i%2)
	}
	run := func(bars map[int][]Bar) (blocked bool, rd []string) {
		datatruth.Reset()
		defer datatruth.Reset()
		RegisterAll(Config{Src: framesSrc{bars: bars}, Unrequired: []string{CheckIngestLoss, CheckLookbackSwing4h, CheckLookbackLevels, CheckLookbackSession}})
		_, rs := datatruth.Evaluate("MNQ", now)
		for _, r := range rs {
			if r.Check == CheckLookback {
				return true, r.Readers
			}
		}
		return false, nil
	}
	if b, _ := run(full); b {
		t.Fatal("fixture: perfect frames must not block the lookback")
	}
	// the 3m ring empty (1m and 5m intact): only the AI path reads the native 3m
	b, rd := run(map[int][]Bar{1: full[1], 5: full[5]})
	if !b || len(rd) != 1 || rd[0] != datatruth.ReaderAI {
		t.Fatalf("a hole in the 3m ring concerns the AI reader only: blocked=%v readers=%v", b, rd)
	}
	// the native 5m ring empty: the AI path reads it AND the mentor's strong-day test
	// reads the last 12 native 5m bars (review round 1, finding 3) - everyone is held
	b, rd = run(map[int][]Bar{1: full[1], 3: full[3]})
	if !b || len(rd) != 0 {
		t.Fatalf("a hole in the native 5m ring concerns the mentor too (strong-day test): blocked=%v readers=%v", b, rd)
	}
	// ONE lost native 5m bar inside the last 12 pauses the mentor reader: it can be the strong one
	var m5 []Bar
	lost := (now.UnixMilli()-5*minuteMs)/(5*minuteMs)*(5*minuteMs) - 3*5*minuteMs
	for _, x := range full[5] {
		if x.T != lost {
			m5 = append(m5, x)
		}
	}
	b, rd = run(map[int][]Bar{1: full[1], 3: full[3], 5: m5})
	mentorHeld := len(rd) == 0 || rd[0] == datatruth.ReaderMentor || (len(rd) > 1 && rd[1] == datatruth.ReaderMentor)
	if !b || !mentorHeld {
		t.Fatalf("one lost native 5m bar among the last 12 must pause the mentor: blocked=%v readers=%v", b, rd)
	}
	// a 20-minute hole in the 1m ring: both readers read the 1m EMAs
	var m1 []Bar
	cutFrom := now.Add(-30 * time.Minute).UnixMilli()
	for _, x := range full[1] {
		if x.T >= cutFrom && x.T < cutFrom+20*minuteMs {
			continue
		}
		m1 = append(m1, x)
	}
	b, rd = run(map[int][]Bar{1: m1, 3: full[3], 5: full[5]})
	if !b {
		t.Fatal("a 20-minute hole in the last 30 1m bars must block the 1m EMA9")
	}
	if len(rd) != 0 {
		t.Fatalf("a hole in the 1m ring concerns every reader (the AI path is held to the whole spec list): %v", rd)
	}
}

// The P1 pinned at the production call site: RegisterAll -> datatruth.Evaluate,
// perfect data, the real CME calendar, evaluated at the first minutes after each
// DST change and weeks later (the phantom bucket stayed in the warm-up).
//
// MUTANT D1: Buckets4h does not clamp a bucket at the next 17:00 anchor -> RED.
func TestSwing4hDoesNotPauseAPerfectFeedAcrossEitherDSTChange(t *testing.T) {
	loc := kernel.CTLocation()
	for _, c := range []struct{ name, from, evals string }{
		{"fall-back", "2026-09-28 17:00", "2026-11-01 17:30,2026-11-02 08:00,2026-11-15 10:00,2026-11-27 12:00"},
		{"spring-forward", "2027-02-08 17:00", "2027-03-14 17:30,2027-03-15 08:00,2027-03-29 10:00,2027-04-09 12:00"},
	} {
		for _, ev := range strings.Split(c.evals, ",") {
			datatruth.Reset()
			now, err := time.ParseInLocation("2006-01-02 15:04", ev, loc)
			if err != nil {
				t.Fatal(err)
			}
			from, _ := time.ParseInLocation("2006-01-02 15:04", c.from, loc)
			m1, m5 := dstPerfectBars(from, now, 1), dstPerfectBars(from, now, 5)
			RegisterAll(Config{Src: calSrc{}, Unrequired: []string{CheckIngestLoss},
				SwingInputs: func(string, time.Time) ([]Bar, []Bar, float64, error) { return m1, m5, 0, nil }})
			_, rs := datatruth.Evaluate("MNQ", now)
			for _, r := range rs {
				if r.Check == CheckLookbackSwing4h {
					t.Errorf("%s @%s: SWING4H paused on a perfect feed: %s", c.name, ev, r.Detail)
				}
			}
			datatruth.Reset()
		}
	}
}

// BenchmarkSwingVerdictFullWindow measures what swingCheck costs when the store
// changed (a new 1m bar every minute makes the fingerprint a new key): the hash of
// a FULL window (27000 1m + 5400 5m rows, the production N) plus Buckets4h and
// JudgeSwing4h. The figure is quoted in swingCheck's comment (review round 1, minor).
func BenchmarkSwingVerdictFullWindow(b *testing.B) {
	loc := kernel.CTLocation()
	now := time.Date(2026, 10, 14, 10, 0, 30, 0, loc)
	from := time.Date(2026, 9, 17, 17, 0, 0, 0, loc)
	m1, m5 := dstPerfectBars(from, now, 1), dstPerfectBars(from, now, 5)
	b.Logf("rows: 1m=%d 5m=%d", len(m1), len(m5))
	open := OpenFn(func(ms int64) bool { return kernel.IsCMEOpen(time.UnixMilli(ms)) })
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = barsSig(m1, m5)
		_ = JudgeSwing4h(Buckets4h(m1, m5, open, now, loc), 34, 0, 1.0)
	}
}

// m452 (release #55) - SINGLE-FLIGHT. The first Evaluate of each minute pays the
// ~100 ms store read, and N concurrent first callers (three traders on the minute
// edge; the armed pass holding armedPassMu through armAdmitted) each repeated it.
// Driven through RegisterAll + datatruth.Evaluate (the production call path, both
// checks asking per Evaluate).
// MUTANT: cachedInputs without the inflight map -> N reads -> RED.
func TestSwingInputsReadIsSingleFlightAcrossConcurrentCallers(t *testing.T) {
	datatruth.Reset()
	defer datatruth.Reset()
	var calls atomic.Int64
	release := make(chan struct{})
	fail := atomic.Bool{}
	RegisterAll(Config{Src: fakeSrc{},
		Unrequired: []string{CheckIngestLoss},
		SwingInputs: func(string, time.Time) ([]Bar, []Bar, float64, error) {
			calls.Add(1)
			<-release // the slow store read
			if fail.Load() {
				return nil, nil, 0, errors.New("store busy")
			}
			return nil, nil, 0, nil
		}})
	t0 := time.Date(2026, 10, 27, 14, 3, 5, 0, time.UTC)
	const callers = 8
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			datatruth.Evaluate("MNQ", t0.Add(time.Duration(i)*time.Millisecond))
		}(i)
	}
	time.Sleep(150 * time.Millisecond) // everyone is parked behind the one read
	close(release)
	wg.Wait()
	if c := calls.Load(); c != 1 {
		t.Fatalf("%d concurrent first callers of one symbol-minute read the store %d times, want 1 (single-flight)", callers, c)
	}

	// a failed read is shared by the burst's waiters but never cached: the next
	// minute's first caller retries
	fail.Store(true)
	before := calls.Load()
	b, _ := datatruth.Evaluate("MNQ", t0.Add(time.Minute))
	if !b || calls.Load() < before+1 {
		t.Fatalf("an unreadable store still blocks (fail closed) and is read: blocked=%v reads=%d", b, calls.Load()-before)
	}
	mid := calls.Load()
	datatruth.Evaluate("MNQ", t0.Add(time.Minute+time.Second))
	if calls.Load() <= mid {
		t.Fatal("a failed read must not be cached - the next Evaluate retries")
	}
}

// The warmer hook reads the current minute for every symbol already seen, so the
// entry paths find it hot. A symbol never asked about is not read.
// MUTANT: WarmSwingInputs a no-op -> the post-warm Evaluate pays a read -> RED.
func TestWarmSwingInputsMakesTheNextMinuteAHit(t *testing.T) {
	datatruth.Reset()
	defer datatruth.Reset()
	var calls atomic.Int64
	RegisterAll(Config{Src: fakeSrc{},
		Unrequired: []string{CheckIngestLoss},
		SwingInputs: func(string, time.Time) ([]Bar, []Bar, float64, error) {
			calls.Add(1)
			return nil, nil, 0, nil
		}})
	t0 := time.Date(2026, 10, 27, 14, 3, 5, 0, time.UTC)
	datatruth.Evaluate("MNQ", t0)
	if calls.Load() != 1 {
		t.Fatalf("fixture: first Evaluate reads once, got %d", calls.Load())
	}
	WarmSwingInputs(t0.Add(time.Minute))
	if calls.Load() != 2 {
		t.Fatalf("the warmer must read the new minute for the seen symbol: reads=%d", calls.Load())
	}
	datatruth.Evaluate("MNQ", t0.Add(time.Minute+10*time.Second))
	if calls.Load() != 2 {
		t.Fatalf("after the warm the entry path's Evaluate must be a cache hit: reads=%d", calls.Load())
	}
}
