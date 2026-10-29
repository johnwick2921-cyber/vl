package checks

import (
	"fmt"
	"math"
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
	datatruth.RegisterScoped(CheckBadPrint, badPrintCheck(c))
	datatruth.RegisterScoped(CheckCrossFrame, crossFrameCheck(c))
	datatruth.RegisterScoped(CheckLookback, lookbackCheck(c))
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
		datatruth.Register(CheckLookbackSession, sessionLevelsCheck(c))
	}
}

// unionReaders merges the readers several findings concern: empty means EVERY
// reader, so one finding that concerns everyone makes the union everyone.
func unionReaders(sets [][]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, rs := range sets {
		if len(rs) == 0 {
			return nil
		}
		for _, r := range rs {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	if len(out) >= 2 {
		return nil // both known readers = everyone
	}
	return out
}

func badPrintCheck(c Config) datatruth.ScopedCheckFn {
	return func(symbol string, now time.Time) (bool, string, []string, error) {
		bars, err := c.Src.Bars(symbol, 1)
		if err != nil {
			return false, "", nil, err
		}
		var partner []Bar
		if p := c.Src.Partner(symbol); p != "" {
			// a partner that cannot be read cannot confirm: SuspectBars then
			// relies on the continuation leg alone (fail closed per bar).
			partner, _ = c.Src.Bars(p, 1)
		}
		s := SuspectBars(bars, partner, now, c.BadPrint)
		if len(s) == 0 {
			return false, "", nil, nil
		}
		var sets [][]string
		for _, x := range s {
			sets = append(sets, x.Readers)
		}
		return true, fmt.Sprintf("%d suspect 1m bar(s): %s", len(s), s[0].Detail), unionReaders(sets), nil
	}
}

func crossFrameCheck(c Config) datatruth.ScopedCheckFn {
	return func(symbol string, now time.Time) (bool, string, []string, error) {
		m1, err := c.Src.Bars(symbol, 1)
		if err != nil {
			return false, "", nil, err
		}
		var msgs []string
		var sets [][]string
		for _, tf := range c.CrossTFs {
			htf, err := c.Src.Bars(symbol, tf)
			if err != nil {
				return false, "", nil, err
			}
			mm, _, _ := CompareFrames(m1, htf, tf, now, c.Src.Open(), c.Cross)
			for _, m := range mm {
				msgs = append(msgs, m.Detail)
				sets = append(sets, m.Readers)
			}
		}
		if len(msgs) == 0 {
			return false, "", nil, nil
		}
		return true, fmt.Sprintf("%d frame mismatch(es): %s", len(msgs), msgs[0]), unionReaders(sets), nil
	}
}

func lookbackCheck(c Config) datatruth.ScopedCheckFn {
	return func(symbol string, now time.Time) (bool, string, []string, error) {
		var why []string
		var sets [][]string
		cache := map[int][]Bar{}
		for _, sp := range c.Specs {
			if sp.Extreme {
				continue // a raw high/low read prices a WRONG bar (hold.go), not a missing one
			}
			bars, ok := cache[sp.TFMin]
			if !ok {
				var err error
				bars, err = c.Src.Bars(symbol, sp.TFMin)
				if err != nil {
					return false, "", nil, err
				}
				cache[sp.TFMin] = bars
			}
			if b, w := JudgeSeries(sp, bars, now, c.Src.Open()); b {
				why = append(why, w)
				if sp.Reader == "" {
					sets = append(sets, nil)
				} else {
					sets = append(sets, []string{sp.Reader})
				}
			}
		}
		if len(why) == 0 {
			return false, "", nil, nil
		}
		return true, strings.Join(why, " | "), unionReaders(sets), nil
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

// barsSig fingerprints the swing inputs (every open time and close of both
// frames): the swing verdict is cached by it, so ANY change to the store - a
// refill, a re-import, one repaired bar - is a different key and is judged afresh.
func barsSig(m1, m5 []Bar) uint64 {
	h := uint64(14695981039346656037)
	mix := func(x uint64) {
		h ^= x
		h *= 1099511628211
	}
	mix(uint64(len(m1)))
	mix(uint64(len(m5)))
	for _, b := range m1 {
		mix(uint64(b.T))
		mix(math.Float64bits(b.C))
	}
	mix(0xfeedface)
	for _, b := range m5 {
		mix(uint64(b.T))
		mix(math.Float64bits(b.C))
	}
	return h
}

// swingCheck caches by (symbol, current 4h bucket, evaluator line, INPUT
// FINGERPRINT). The judgement over the buckets is the expensive part (the store
// read itself is shared by cachedInputs, one per symbol-minute). The cache used
// to be keyed by the bucket alone: an owner's repair (POST /api/admin/bars/import,
// "read back within a minute") did not lift a blocking verdict until the next 4h
// bucket - up to four hours (release #55 review). The fingerprint makes a changed
// store a changed key, so a refill is judged on the very next read.
//
// COST (measured, BenchmarkSwingVerdictFullWindow, 25861 1m + 5173 5m rows): the
// fingerprint changes with every new 1m bar, so the verdict is re-judged about
// once a minute per symbol, not once per 4h bucket; hash + Buckets4h + JudgeSwing4h
// take about 1.7 ms of CPU per judgement. The store read is shared (cachedInputs).
func swingCheck(c Config) datatruth.CheckFn {
	type key struct {
		sym    string
		bucket int64
		line   float64
		sig    uint64
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
		k := key{symbol, bucket, line, barsSig(m1, m5)}
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
		// five prior sessions without one stored bar in either frame: the levels
		// cannot be verified. Unknown is never CLEAR (release #55 review) - but a
		// closed market asks for no entry, so only an open one raises it (the same
		// guard sessionLevelsCheck's storeSilent uses).
		if c.Src.Open().open(now.UnixMilli()) {
			return true, "prior-session levels cannot be verified: no stored 1m or 5m bar in the last 5 sessions", nil
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
