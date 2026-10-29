package checks

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vl/internal/datatruth"
)

// Registered check names (each its own datatruth check; the integrator can scope
// the swing ones to swing entries because the name says what failed).
const (
	CheckBadPrint        = "bad_print"
	CheckCrossFrame      = "cross_frame"
	CheckLookback        = "indicator_lookback"
	CheckLookbackSwing4h = "indicator_lookback_swing4h"
	CheckLookbackLevels  = "indicator_lookback_levels"
	// CheckLookbackSession judges the session-anchored VOLUME levels (session
	// VWAP + bands, eVWAP, pdVWAP, prior-day profile / nPOC, MID-O) - a sum over
	// the whole session, so a hole poisons it for hours (session_volume.go).
	CheckLookbackSession = "indicator_lookback_session"
	CheckIngestLoss      = "ingest_loss"
	CheckContractRoll    = "contract_roll"
)

// Sources is everything the checks read. The wiring implements it over the bar
// cache / store; tests implement it over real-data fixtures. Every method that
// can fail returns an error, and an error is a BLOCK (fail closed).
type Sources interface {
	// Bars returns the symbol's bars at tfMin minutes, ascending, as the
	// decision reads them (the live ring).
	Bars(symbol string, tfMin int) ([]Bar, error)
	// Partner names the correlated symbol used to confirm a spike ("" = none).
	Partner(symbol string) string
	// Open is the exchange calendar (kernel.IsCMEOpen, injected).
	Open() OpenFn
	// Contract returns the contract facts for the symbol.
	Contract(symbol string) (ContractFacts, error)
}

// Config bundles the optional pieces.
type Config struct {
	Src      Sources
	BadPrint BadPrintParams
	Cross    CrossParams
	CrossTFs []int
	Specs    []SeriesSpec
	Ledger   *Ledger
	Verify   VerifyFn
	// SwingInputs, when set, enables the swing-line and prior-session checks: the
	// store-backed 1m and 5m history (the ring is too shallow for a 34-bucket
	// 4h EMA), the evaluator's live line (0 = unknown) and the CT location.
	SwingInputs func(symbol string, now time.Time) (m1, m5 []Bar, botLine float64, err error)
	// Unrequired names checks this wiring deliberately does not supply inputs
	// for (a replay over a corpus that has no ingest queue or store). Everything
	// not listed is REQUIRED: a missing input blocks instead of silently
	// skipping the registration. Production passes nothing here.
	Unrequired  []string
	SwingPeriod int
	SwingTolPts float64
	LevelTolPts float64
	SessionVol  SessionVolParams
	Loc         *time.Location
}

// RegisterAll registers every check whose inputs the Config carries.
func RegisterAll(c Config) {
	if c.BadPrint.WindowMin <= 0 {
		c.BadPrint = DefaultBadPrint()
	}
	if c.Cross.WindowMin <= 0 {
		c.Cross = DefaultCross()
	}
	if len(c.CrossTFs) == 0 {
		c.CrossTFs = []int{3, 5, 15}
	}
	if c.Specs == nil {
		c.Specs = DefaultSpecs([]int{3, 5})
	}
	c.BadPrint = c.BadPrint.WithHold(c.Specs)
	c.Cross = c.Cross.WithHold(c.Specs)
	if c.SwingPeriod <= 0 {
		c.SwingPeriod = 34
	}
	if c.SwingTolPts <= 0 {
		c.SwingTolPts = 1.0
	}
	if c.LevelTolPts <= 0 {
		c.LevelTolPts = 2.0
	}
	if c.SessionVol.VWAPTolPts <= 0 {
		c.SessionVol = DefaultSessionVol()
	}
	// The wiring declares ALL eight required. A registration skipped because its
	// input was never supplied (no ledger, no swing reader) leaves a stub that
	// BLOCKS, so a wiring omission fails closed instead of reading as clean.
	skip := map[string]bool{}
	for _, n := range c.Unrequired {
		skip[n] = true
	}
	for _, n := range []string{CheckBadPrint, CheckCrossFrame, CheckLookback, CheckLookbackSwing4h,
		CheckLookbackLevels, CheckLookbackSession, CheckIngestLoss, CheckContractRoll} {
		if !skip[n] {
			datatruth.Require(n)
		}
	}
	datatruth.Register(CheckBadPrint, badPrintCheck(c))
	datatruth.Register(CheckCrossFrame, crossFrameCheck(c))
	datatruth.Register(CheckLookback, lookbackCheck(c))
	datatruth.Register(CheckContractRoll, contractCheck(c))
	if c.Ledger != nil && c.Verify != nil {
		datatruth.Register(CheckIngestLoss, func(symbol string, now time.Time) (bool, string, error) {
			b, why := c.Ledger.Judge(symbol, c.Verify)
			return b, why, nil
		})
	}
	if c.SwingInputs != nil {
		loc := c.Loc
		if loc == nil {
			if l, err := time.LoadLocation("America/Chicago"); err == nil {
				loc = l
			} else {
				loc = time.UTC
			}
		}
		c.Loc = loc
		var warm func(time.Time)
		c.SwingInputs, warm = cachedInputs(c.SwingInputs)
		warmSwingInputs.Store(warm)
		datatruth.Register(CheckLookbackSwing4h, swingCheck(c))
		datatruth.Register(CheckLookbackLevels, levelsCheck(c))
		datatruth.Register(CheckLookbackSession, sessionLevelsCheck(c))
	}
}

func badPrintCheck(c Config) datatruth.CheckFn {
	return func(symbol string, now time.Time) (bool, string, error) {
		bars, err := c.Src.Bars(symbol, 1)
		if err != nil {
			return false, "", err
		}
		var partner []Bar
		if p := c.Src.Partner(symbol); p != "" {
			// a partner that cannot be read cannot confirm: SuspectBars then
			// relies on the continuation leg alone (fail closed per bar).
			partner, _ = c.Src.Bars(p, 1)
		}
		s := SuspectBars(bars, partner, now, c.BadPrint)
		if len(s) == 0 {
			return false, "", nil
		}
		return true, fmt.Sprintf("%d suspect 1m bar(s): %s", len(s), s[0].Detail), nil
	}
}

func crossFrameCheck(c Config) datatruth.CheckFn {
	return func(symbol string, now time.Time) (bool, string, error) {
		m1, err := c.Src.Bars(symbol, 1)
		if err != nil {
			return false, "", err
		}
		var msgs []string
		for _, tf := range c.CrossTFs {
			htf, err := c.Src.Bars(symbol, tf)
			if err != nil {
				return false, "", err
			}
			mm, _, _ := CompareFrames(m1, htf, tf, now, c.Src.Open(), c.Cross)
			for _, m := range mm {
				msgs = append(msgs, m.Detail)
			}
		}
		if len(msgs) == 0 {
			return false, "", nil
		}
		return true, fmt.Sprintf("%d frame mismatch(es): %s", len(msgs), msgs[0]), nil
	}
}

func lookbackCheck(c Config) datatruth.CheckFn {
	return func(symbol string, now time.Time) (bool, string, error) {
		var why []string
		cache := map[int][]Bar{}
		for _, sp := range c.Specs {
			bars, ok := cache[sp.TFMin]
			if !ok {
				var err error
				bars, err = c.Src.Bars(symbol, sp.TFMin)
				if err != nil {
					return false, "", err
				}
				cache[sp.TFMin] = bars
			}
			if b, w := JudgeSeries(sp, bars, now, c.Src.Open()); b {
				why = append(why, w)
			}
		}
		if len(why) == 0 {
			return false, "", nil
		}
		return true, strings.Join(why, " | "), nil
	}
}

func contractCheck(c Config) datatruth.CheckFn {
	return func(symbol string, now time.Time) (bool, string, error) {
		f, err := c.Src.Contract(symbol)
		if err != nil {
			return false, "", err
		}
		b, why := JudgeContract(f)
		return b, why, nil
	}
}

// cachedInputs memoises the swing/levels store read by (symbol, wall-clock
// minute). The swing and levels checks both call SwingInputs on every Evaluate
// and each call is a full store read of ~200 four-hour buckets of 1m and 5m
// (review round 1, D6); both checks now share one read per symbol-minute. A
// failed read is never cached (the next Evaluate retries).
//
// m452 (release #55) - SINGLE-FLIGHT. The read is ~100 ms warm (27000 1m + 5400 5m
// rows) and used to be repeated by every caller that found the minute cold:
// three traders asking on the minute edge each paid it, and one of them could be
// the armed placement pass holding armedPassMu through armAdmitted. Now the
// FIRST caller of a (symbol, minute) reads and every concurrent caller of the
// same key waits for THAT read and shares its result - error included, so a down
// store is read once per burst, not once per caller. The returned warm func reads
// (and caches) the current minute for every symbol this cache has seen; a minute-
// edge warmer (trader/ninjatrader WireDataTruthChecks) calls it so the entry
// paths and the armed pass find the minute already hot.
func cachedInputs(fn func(symbol string, now time.Time) (m1, m5 []Bar, botLine float64, err error)) (get func(symbol string, now time.Time) (m1, m5 []Bar, botLine float64, err error), warm func(now time.Time)) {
	type key struct {
		sym    string
		minute int64
	}
	type val struct {
		m1, m5 []Bar
		line   float64
		err    error
	}
	type flight struct {
		done chan struct{}
		v    val
	}
	var mu sync.Mutex
	cache := map[key]val{}
	inflight := map[key]*flight{}
	seen := map[string]bool{}
	get = func(symbol string, now time.Time) ([]Bar, []Bar, float64, error) {
		k := key{symbol, now.Unix() / 60}
		mu.Lock()
		seen[symbol] = true
		if v, ok := cache[k]; ok {
			mu.Unlock()
			return v.m1, v.m5, v.line, nil
		}
		if f, ok := inflight[k]; ok {
			mu.Unlock()
			<-f.done // someone is already reading this very minute: share it
			return f.v.m1, f.v.m5, f.v.line, f.v.err
		}
		f := &flight{done: make(chan struct{})}
		inflight[k] = f
		mu.Unlock()
		func() {
			defer func() {
				// a panic in the read must still release the waiters, and reads as a
				// failed read (fail closed), never as a cached value
				if r := recover(); r != nil {
					f.v = val{err: fmt.Errorf("swing inputs read panicked: %v", r)}
				}
				mu.Lock()
				delete(inflight, k)
				if f.v.err == nil {
					if len(cache) > 8 {
						cache = map[key]val{}
					}
					cache[k] = f.v // a failed read is never cached
				}
				mu.Unlock()
				close(f.done)
			}()
			m1, m5, line, err := fn(symbol, now)
			f.v = val{m1, m5, line, err}
		}()
		return f.v.m1, f.v.m5, f.v.line, f.v.err
	}
	warm = func(now time.Time) {
		mu.Lock()
		syms := make([]string, 0, len(seen))
		for s := range seen {
			syms = append(syms, s)
		}
		mu.Unlock()
		for _, s := range syms {
			get(s, now) // a hit when the minute is already hot; otherwise the one shared read
		}
	}
	return get, warm
}

// warmSwingInputs is the hook WarmSwingInputs calls; set by RegisterAll.
var warmSwingInputs atomic.Value // func(time.Time)

// WarmSwingInputs reads the current minute's swing/levels store inputs for every
// symbol the data-truth cache has been asked about, so the entry paths find the
// minute hot (m452). A no-op until RegisterAll has wired SwingInputs. Safe to
// call from any goroutine; concurrent callers share one read.
func WarmSwingInputs(now time.Time) {
	if f, ok := warmSwingInputs.Load().(func(time.Time)); ok && f != nil {
		f(now)
	}
}

// swingCheck caches by (symbol, current 4h bucket, evaluator line): the line
// only moves at a bucket flip, and the judgement over the buckets is the
// expensive part (the store read itself is shared by cachedInputs).
func swingCheck(c Config) datatruth.CheckFn {
	type key struct {
		sym    string
		bucket int64
		line   float64
	}
	var mu sync.Mutex
	cache := map[key]Swing4hVerdict{}
	return func(symbol string, now time.Time) (bool, string, error) {
		t := now.In(c.Loc)
		a := time.Date(t.Year(), t.Month(), t.Day(), 17, 0, 0, 0, c.Loc)
		if t.Before(a) {
			a = a.AddDate(0, 0, -1)
		}
		d := t.Sub(a)
		bucket := a.Add(d - d%(4*time.Hour)).UnixMilli()
		// the line is read first (cheap) so a changed line busts the cache
		m1, m5, line, err := c.SwingInputs(symbol, now)
		if err != nil {
			return false, "", err
		}
		k := key{symbol, bucket, line}
		mu.Lock()
		v, ok := cache[k]
		mu.Unlock()
		if !ok {
			v = JudgeSwing4h(Buckets4h(m1, m5, c.Src.Open(), now, c.Loc), c.SwingPeriod, line, c.SwingTolPts)
			mu.Lock()
			if len(cache) > 16 {
				cache = map[key]Swing4hVerdict{}
			}
			cache[k] = v
			mu.Unlock()
		}
		return v.Blocked, v.Why, nil
	}
}

// levelsCheck judges the PRIOR trading session's extremes (17:00 CT to 17:00 CT).
func levelsCheck(c Config) datatruth.CheckFn {
	return func(symbol string, now time.Time) (bool, string, error) {
		m1, m5, _, err := c.SwingInputs(symbol, now)
		if err != nil {
			return false, "", err
		}
		t := now.In(c.Loc)
		cur := time.Date(t.Year(), t.Month(), t.Day(), 17, 0, 0, 0, c.Loc)
		if t.Before(cur) {
			cur = cur.AddDate(0, 0, -1)
		}
		// previous OPEN session: walk back over closed days (weekend)
		to := cur
		for i := 0; i < 5; i++ {
			from := to.AddDate(0, 0, -1)
			if hasBars(m1, from.UnixMilli(), to.UnixMilli()) || hasBars(m5, from.UnixMilli(), to.UnixMilli()) {
				b, why := SessionExtremes(m1, m5, from.UnixMilli(), to.UnixMilli(), c.LevelTolPts)
				return b, why, nil
			}
			to = from
		}
		return false, "", nil
	}
}

func hasBars(bars []Bar, from, to int64) bool {
	for _, b := range bars {
		if b.T >= from && b.T < to {
			return true
		}
	}
	return false
}

// SessionLevelsCheck exposes the indicator_lookback_session check on its own for
// a replay whose corpus carries only 1m/5m bars (no 4h-store reader, so the
// swing / levels checks cannot be wired): c needs Src, SwingInputs and Loc.
func SessionLevelsCheck(c Config) datatruth.CheckFn {
	if c.SessionVol.VWAPTolPts <= 0 {
		c.SessionVol = DefaultSessionVol()
	}
	if c.LevelTolPts <= 0 {
		c.LevelTolPts = 2.0
	}
	if c.Loc == nil {
		c.Loc = time.UTC
	}
	return sessionLevelsCheck(c)
}

// sessionLevelsCheck is the indicator_lookback_session check: the session-
// anchored volume levels recomputed with the missing volume restored from the
// 5m (JudgeSessionLevels). The verdict is deterministic in (inputs, 5-minute
// slot), so it is memoised per symbol-minute; the store read behind it is the
// shared cachedInputs one.
func sessionLevelsCheck(c Config) datatruth.CheckFn {
	type key struct {
		sym    string
		minute int64
	}
	type val struct {
		blocked bool
		why     string
	}
	var mu sync.Mutex
	cache := map[key]val{}
	return func(symbol string, now time.Time) (bool, string, error) {
		k := key{symbol, now.Unix() / 60}
		mu.Lock()
		v, ok := cache[k]
		mu.Unlock()
		if ok {
			return v.blocked, v.why, nil
		}
		m1, m5, _, err := c.SwingInputs(symbol, now)
		if err != nil {
			return false, "", err
		}
		b, why := JudgeSessionLevels(m1, m5, now, c.Loc, c.Src.Open(), c.SessionVol, c.LevelTolPts)
		mu.Lock()
		if len(cache) > 8 {
			cache = map[key]val{}
		}
		cache[k] = val{b, why}
		mu.Unlock()
		return b, why, nil
	}
}
