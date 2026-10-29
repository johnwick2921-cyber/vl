package checks

import (
	"fmt"
	"strings"
	"sync"
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
	// The wiring declares ALL seven required. A registration skipped because its
	// input was never supplied (no ledger, no swing reader) leaves a stub that
	// BLOCKS, so a wiring omission fails closed instead of reading as clean.
	skip := map[string]bool{}
	for _, n := range c.Unrequired {
		skip[n] = true
	}
	for _, n := range []string{CheckBadPrint, CheckCrossFrame, CheckLookback, CheckLookbackSwing4h,
		CheckLookbackLevels, CheckIngestLoss, CheckContractRoll} {
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
		c.SwingInputs = cachedInputs(c.SwingInputs)
		datatruth.Register(CheckLookbackSwing4h, swingCheck(c))
		datatruth.Register(CheckLookbackLevels, levelsCheck(c))
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
func cachedInputs(fn func(symbol string, now time.Time) (m1, m5 []Bar, botLine float64, err error)) func(symbol string, now time.Time) (m1, m5 []Bar, botLine float64, err error) {
	type key struct {
		sym    string
		minute int64
	}
	type val struct {
		m1, m5 []Bar
		line   float64
	}
	var mu sync.Mutex
	cache := map[key]val{}
	return func(symbol string, now time.Time) ([]Bar, []Bar, float64, error) {
		k := key{symbol, now.Unix() / 60}
		mu.Lock()
		v, ok := cache[k]
		mu.Unlock()
		if ok {
			return v.m1, v.m5, v.line, nil
		}
		m1, m5, line, err := fn(symbol, now)
		if err != nil {
			return nil, nil, 0, err
		}
		mu.Lock()
		if len(cache) > 8 {
			cache = map[key]val{}
		}
		cache[k] = val{m1, m5, line}
		mu.Unlock()
		return m1, m5, line, nil
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
