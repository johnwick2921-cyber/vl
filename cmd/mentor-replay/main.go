// mentor-replay — READ-ONLY offline replay of stored MNQ 1m bars through the
// PRODUCTION mentor path, one trading day at a time, emitting every detector
// event as JSONL.
//
//	go run ./cmd/mentor-replay -db /home/hoang/vl/data/data.db -last 90 -out .replay-out/run1
//	go run ./cmd/mentor-replay -db … -days 2026-11-10 -strategy a5b7662e -seed-at 08:15:25 \
//	    -live-log /home/hoang/vl/data/vl_2026-11-10.log -out .replay-out/parity
//
// WHAT RUNS. For each day the tool builds trader.NewMentorReplay (a minimal
// AutoTrader: no store, no broker), seeds it with the SAME seed body the boot
// runs (mentorSeedFrom = mentorSeedAtStart's body) over the stored bars AS OF
// the seed instant, then feeds every closed 1m bar of the day, in order, to the
// SAME per-bar entry the live event loop runs (mentorEvalOnce), with the mentor
// clock pinned to the bar's close instant and the 1500-bar current-contract
// window the live bar cache would hold. No detector is re-implemented here: the
// events are diffs of the evaluator's own state, its intents (tapped before the
// dispatch), its refusal ledgers, and the lines the trader logged.
//
// READ-ONLY. The database is opened with mode=ro and only SELECTed; the trader
// has no store, so nothing it does can write. Outputs go to -out only.
//
// OUTPUTS (in -out): <day>.jsonl (one event per line), days.tsv (per-day 5m RTH
// candle stats + event counts), summary.json, and parity-<day>.txt when
// -live-log is given.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"vl/calendar"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/logger"
	"vl/market"
	"vl/store"
	"vl/trader"
)

// window is the live tick depth (trader mentorBars1mDepth).
const window = 1500

type opts struct {
	db          string
	out         string
	days        string
	last        int
	strategy    string
	seedAt      string
	importShift bool
	liveLog     string
	reseedAt    string
	bigMean     float64
	bigN50      int
	quiet       bool
}

// capture is a logrus hook collecting every line the trader logs during a step.
type capture struct {
	lines []Event
}

func (c *capture) Levels() []logrus.Level { return logrus.AllLevels }
func (c *capture) Fire(e *logrus.Entry) error {
	c.lines = append(c.lines, Event{"level": e.Level.String(), "msg": e.Message})
	return nil
}
func (c *capture) take() []Event { out := c.lines; c.lines = nil; return out }

func main() {
	var o opts
	flag.StringVar(&o.db, "db", "data/data.db", "SQLite store (opened READ-ONLY, mode=ro)")
	flag.StringVar(&o.out, "out", ".replay-out/run", "output directory")
	flag.StringVar(&o.days, "days", "", "comma-separated trading days YYYY-MM-DD (added to -last)")
	flag.IntVar(&o.last, "last", 90, "replay the newest N RTH days with 1m bars (0 = none)")
	flag.StringVar(&o.strategy, "strategy", "", "strategy id (or unique prefix) whose config to load read-only; empty = default mentor config")
	flag.StringVar(&o.seedAt, "seed-at", "session", `seed instant: "session" = 17:00 CT the evening before the day; or HH:MM[:SS] CT on the day (a mid-session boot)`)
	flag.BoolVar(&o.importShift, "import-shift", true, "re-stamp close-stamped historical_import rows to their open (m460); false = read them as the live seed does")
	flag.StringVar(&o.liveLog, "live-log", "", "live vl_<date>.log to compare the 5m ISB box lines against (its day must be replayed)")
	flag.StringVar(&o.reseedAt, "reseed-at", "", "comma-separated HH:MM:SS CT instants (on each replayed day) at which to run the production history-refill re-seed before that bar's tick; with -live-log the log's own re-seed instants are added for its day")
	flag.Float64Var(&o.bigMean, "big-mean", 40, "BIG day: mean 5m RTH candle range >= this (pts)")
	flag.IntVar(&o.bigN50, "big-n50", 5, "BIG day: at least this many 5m RTH candles > 50 pts")
	flag.BoolVar(&o.quiet, "quiet", false, "no per-day progress lines")
	flag.Parse()
	if err := run(o, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "mentor-replay:", err)
		os.Exit(1)
	}
}

// dayStats is the per-day 5m RTH candle-size read (08:30–15:00 CT buckets).
type dayStats struct {
	Day       string  `json:"day"`
	N5m       int     `json:"n_5m"`
	MeanRange float64 `json:"mean_range"`
	MaxRange  float64 `json:"max_range"`
	Over50    int     `json:"over_50"`
	Big       bool    `json:"big"`
	Events    int     `json:"events"`
	Intents   int     `json:"intents"`
	Box5m     int     `json:"isb_box_5m_events"`
	Panics    int     `json:"panics,omitempty"`
	Contract  string  `json:"contract"`
}

func run(o opts, stdout io.Writer) error {
	if os.Getenv("MENTOR_PLACE") != "" {
		return fmt.Errorf("MENTOR_PLACE is set: unset it — the replay never places")
	}
	db, err := openReadOnly(o.db)
	if err != nil {
		return fmt.Errorf("open %s read-only: %w", o.db, err)
	}
	defer db.Close()
	var sc *store.StrategyConfig
	stratName := "default mentor config (store.RiskControlConfig{MentorMode:true})"
	if o.strategy != "" {
		if sc, stratName, err = loadStrategy(db, o.strategy); err != nil {
			return err
		}
	}
	cal, err := loadCalendar(db)
	if err != nil {
		return fmt.Errorf("calendar: %w", err)
	}
	t0 := time.Now()
	bs, err := loadBars(db, "MNQ", []string{"1m", "5m", "1h"}, o.importShift)
	if err != nil {
		return fmt.Errorf("bars: %w", err)
	}
	fmt.Fprintf(stdout, "loaded bars in %s (import-shift=%v shifted %v, dropped on collision %v); strategy: %s\n",
		time.Since(t0).Round(time.Millisecond), o.importShift, bs.importShifted, bs.importDropped, stratName)

	days := pickDays(bs, o.last, o.days)
	if len(days) == 0 {
		return fmt.Errorf("no RTH day with 1m bars selected")
	}
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return err
	}
	// Never overwrite: a run writes into a fresh (or empty) directory only.
	if ents, err := os.ReadDir(o.out); err != nil {
		return err
	} else if len(ents) > 0 {
		return fmt.Errorf("-out %s is not empty: pick a new directory (the tool never overwrites a file)", o.out)
	}
	fmt.Fprintf(stdout, "replaying %d day(s): %s .. %s → %s\n", len(days), days[0], days[len(days)-1], o.out)

	// The trader logs through the global logrus logger: capture every line,
	// print none.
	cap := &capture{}
	logger.Log.SetOutput(io.Discard)
	logger.Log.AddHook(cap)

	var stats []dayStats
	for _, d := range days {
		st, err := replayDay(o, bs, sc, cal, d, cap)
		if err != nil {
			return fmt.Errorf("%s: %w", d, err)
		}
		stats = append(stats, st)
		if !o.quiet {
			fmt.Fprintf(stdout, "%s  events %6d  intents %3d  5m-box %3d  5m RTH mean %.1f max %.1f >50:%d%s\n",
				d, st.Events, st.Intents, st.Box5m, st.MeanRange, st.MaxRange, st.Over50, map[bool]string{true: "  BIG", false: ""}[st.Big])
		}
	}
	if err := writeStats(o, stats, stratName); err != nil {
		return err
	}
	if o.liveLog != "" {
		if err := compareLiveLog(o, stdout); err != nil {
			return err
		}
	}
	return nil
}

// pickDays: the newest `last` CT dates holding >= 300 RTH 1m bars (of 390) on
// one contract, plus every explicitly named day that has any 1m bar.
func pickDays(bs *barSet, last int, named string) []string {
	loc := kernel.CTLocation()
	rth := map[string]map[string]int{}
	any := map[string]bool{}
	for c, rs := range bs.byTF["1m"] {
		for _, r := range rs {
			t := time.UnixMilli(r.T).In(loc)
			d := t.Format("2006-01-02")
			any[d] = true
			m := t.Hour()*60 + t.Minute()
			if m >= 8*60+30 && m < 15*60 {
				if rth[d] == nil {
					rth[d] = map[string]int{}
				}
				rth[d][c]++
			}
		}
	}
	var full []string
	for d, byC := range rth {
		for _, n := range byC {
			if n >= 300 {
				full = append(full, d)
				break
			}
		}
	}
	sort.Strings(full)
	set := map[string]bool{}
	if last > 0 {
		if len(full) > last {
			full = full[len(full)-last:]
		}
		for _, d := range full {
			set[d] = true
		}
	}
	for _, d := range strings.Split(named, ",") {
		if d = strings.TrimSpace(d); d != "" && any[d] {
			set[d] = true
		}
	}
	out := sortedKeys(set)
	return out
}

// seedInstant resolves -seed-at for day d.
func seedInstant(spec, d string) (time.Time, error) {
	loc := kernel.CTLocation()
	day, err := time.ParseInLocation("2006-01-02", d, loc)
	if err != nil {
		return time.Time{}, err
	}
	if spec == "" || spec == "session" {
		p := day.AddDate(0, 0, -1)
		return time.Date(p.Year(), p.Month(), p.Day(), 17, 0, 0, 0, loc), nil
	}
	for _, layout := range []string{"15:04:05", "15:04"} {
		if t, err := time.ParseInLocation(layout, spec, loc); err == nil {
			return time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc), nil
		}
	}
	return time.Time{}, fmt.Errorf("bad -seed-at %q", spec)
}

// replayer drives one day. Split out so the parity test can run the tool's own
// stepping over a fixture bar set.
type replayer struct {
	r    *trader.MentorReplay
	src  asOfSource
	cap  *capture
	tag  string
	emit func(Event)
}

func newReplayer(bs *barSet, sc *store.StrategyConfig, cal map[string][]calendar.Event, cap *capture, emit func(Event)) *replayer {
	r := trader.NewMentorReplay("replay", sc)
	r.SetCalendar(func(date string) ([]calendar.Event, bool) {
		evs, ok := cal[date]
		return evs, ok
	})
	rp := &replayer{r: r, src: asOfSource{bs: bs, symbol: "MNQ"}, cap: cap, tag: r.LogTag(), emit: emit}
	// The live bar cache, AS OF the pinned clock: closed bars of the current
	// contract (a stored bar is final, so mentorClosedBars drops nothing).
	market.FuturesBarsProvider = func(symbol, tf string, n int) []market.Kline {
		if symbol != "MNQ" {
			return nil
		}
		if _, ok := tfMs[tf]; !ok {
			return nil
		}
		return rp.src.window(tf, n)
	}
	return rp
}

func (rp *replayer) logs(phase string, at int64) {
	for _, l := range rp.cap.take() {
		msg := stripTag(fmt.Sprint(l["msg"]), rp.tag)
		rp.emit(Event{"kind": "log", "phase": phase, "t": ct(at), "level": l["level"], "cat": logCat(msg), "msg": msg})
	}
}

// seed runs the production seed at instant at.
func (rp *replayer) seed(at time.Time) {
	rp.r.SetNow(at)
	rp.src.AsOf = at.UnixMilli()
	rp.cap.take()
	rp.r.Seed(rp.src)
	rp.logs("seed", at.UnixMilli())
}

// reseed runs the production refill re-seed at the bar-close instant at (the
// live trader runs it inside mentorEvalOnce just before that bar's tick).
func (rp *replayer) reseed(at int64, why string) {
	rp.r.SetNow(time.UnixMilli(at))
	rp.src.AsOf = at
	rp.cap.take()
	rp.r.Reseed(rp.src, at, why)
	rp.emit(Event{"kind": "reseed", "t": ct(at), "why": why})
	rp.logs("reseed", at)
}

// reseedRe reads the live log's refill re-seed instant (nowMs = the tick's bar
// close, printed HH:MM:SS CT).
var reseedRe = regexp.MustCompile(`mentor re-seed after a history refill \(([^)]*)\) at (\d{2}:\d{2}:\d{2}) CT`)

// reseedInstants: -reseed-at on day d, plus the -live-log's own re-seed lines
// when the log is day d's (first trader tag only).
func reseedInstants(o opts, d string) (map[int64]string, error) {
	loc := kernel.CTLocation()
	day, err := time.ParseInLocation("2006-01-02", d, loc)
	if err != nil {
		return nil, err
	}
	at := func(hms string) (int64, error) {
		t, err := time.ParseInLocation("15:04:05", hms, loc)
		if err != nil {
			return 0, err
		}
		return time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc).UnixMilli(), nil
	}
	out := map[int64]string{}
	for _, x := range strings.Split(o.reseedAt, ",") {
		if x = strings.TrimSpace(x); x == "" {
			continue
		}
		ms, err := at(x)
		if err != nil {
			return nil, fmt.Errorf("bad -reseed-at %q", x)
		}
		out[ms] = "-reseed-at"
	}
	if o.liveLog != "" && strings.HasSuffix(o.liveLog, "vl_"+d+".log") {
		raw, err := os.ReadFile(o.liveLog)
		if err != nil {
			return nil, err
		}
		tag := ""
		tagRe := regexp.MustCompile(`\[trader_id=([^ \]]+)`)
		for _, l := range strings.Split(string(raw), "\n") {
			m := reseedRe.FindStringSubmatch(l)
			tm := tagRe.FindStringSubmatch(l)
			if m == nil || tm == nil {
				continue
			}
			if tag == "" {
				tag = tm[1]
			}
			if tm[1] != tag {
				continue
			}
			ms, err := at(m[2])
			if err != nil {
				continue
			}
			out[ms] = "live log: " + m[1]
		}
	}
	return out, nil
}

// step ticks the closed 1m bar b (the newest bar of the window).
// notBefore is the seed instant: the live boot's first tick evaluates the last
// bar closed before the boot with the wall clock at the boot, so the clock never
// reads earlier than the seed.
func (rp *replayer) step(b market.Kline, notBefore int64) (panicked bool) {
	closeAt := mentor.BarCloseInstant(b)
	now := closeAt
	if now < notBefore {
		now = notBefore
	}
	rp.r.SetNow(time.UnixMilli(now))
	rp.src.AsOf = closeAt
	win := rp.src.window("1m", window)
	if len(win) == 0 || win[len(win)-1].OpenTime != b.OpenTime {
		return false
	}
	return rp.observe(b, win, func() []mentor.Intent {
		ins, _ := rp.r.EvalOnce(win)
		return ins
	})
}

// observe runs one production tick (fn) and emits what it changed: the state
// diff, the intents, the logged lines. Shared by the tool's stepping and the
// parity test's live-path driver, so the two are compared on the same terms.
func (rp *replayer) observe(b market.Kline, win []market.Kline, fn func() []mentor.Intent) (panicked bool) {
	closeAt := mentor.BarCloseInstant(b)
	before := takeSnap(rp.r.Evaluator(), rp.r.Counts())
	var intents []mentor.Intent
	func() {
		defer func() {
			if p := recover(); p != nil {
				panicked = true
				rp.emit(Event{"kind": "panic", "t": ct(closeAt), "bar": ct(b.OpenTime), "err": fmt.Sprint(p)})
			}
		}()
		intents = fn()
	}()
	after := takeSnap(rp.r.Evaluator(), rp.r.Counts())
	tick := Event{"bar": ct(b.OpenTime), "t": ct(closeAt)}
	for _, ev := range diffSnaps(before, after, win) {
		rp.emit(merge(ev, tick))
	}
	for _, in := range intents {
		rp.emit(merge(intentEv(in), tick))
	}
	rp.logs("tick", closeAt)
	return panicked
}

func replayDay(o opts, bs *barSet, sc *store.StrategyConfig, cal map[string][]calendar.Event, d string, cap *capture) (dayStats, error) {
	st := dayStats{Day: d}
	seedAt, err := seedInstant(o.seedAt, d)
	if err != nil {
		return st, err
	}
	loc := kernel.CTLocation()
	day, _ := time.ParseInLocation("2006-01-02", d, loc)
	sessEnd := time.Date(day.Year(), day.Month(), day.Day(), 16, 0, 0, 0, loc).UnixMilli()

	f, err := os.Create(filepath.Join(o.out, d+".jsonl"))
	if err != nil {
		return st, err
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	defer w.Flush()
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	emit := func(ev Event) {
		ev["day"] = d
		_ = enc.Encode(ev)
		st.Events++
		switch k := ev["kind"]; {
		case k == "intent":
			st.Intents++
		case k == "panic":
			st.Panics++
		case strings.HasPrefix(fmt.Sprint(k), "isb_box_5m_"):
			st.Box5m++
		}
	}
	reseeds, err := reseedInstants(o, d)
	if err != nil {
		return st, err
	}
	rp := newReplayer(bs, sc, cal, cap, emit)
	emit(Event{"kind": "replay_start", "seed_at": seedAt.Format(time.RFC3339), "import_shift": o.importShift, "window": window})
	rp.seed(seedAt)

	// Tick every bar that CLOSES after the seed instant, to the session end, on
	// the contract current at the session end. The live boot's first tick is the
	// first FINAL bar after the boot: at the boot the NT8 bar cache is still
	// empty (11-10 08:15:25: "no live 1m frame received since boot"; the
	// funnel's first bar is the 08:15 bar, ticked at 08:16:00).
	seedMs := seedAt.UnixMilli()
	contract := asOfSource{bs: bs, symbol: "MNQ", AsOf: sessEnd}.currentContract("1m")
	st.Contract = contract
	rows := bs.byTF["1m"][contract]
	first := sort.Search(len(rows), func(i int) bool { return rows[i].T+60_000 > seedMs })
	for i := first; i < len(rows) && rows[i].T+60_000 <= sessEnd; i++ {
		b := klines(rows[i:i+1], 60_000)[0]
		if why, ok := reseeds[mentor.BarCloseInstant(b)]; ok {
			rp.reseed(mentor.BarCloseInstant(b), why)
		}
		rp.step(b, seedMs)
	}
	emit(Event{"kind": "replay_end", "counts": rp.r.Counts(), "refusals": rp.r.Evaluator().State.Refusals})

	// 5m RTH candle sizes (08:30–15:00 CT buckets) from the same rows.
	var rth []market.Kline
	for _, r := range rows {
		t := time.UnixMilli(r.T).In(loc)
		if t.Format("2006-01-02") != d {
			continue
		}
		if m := t.Hour()*60 + t.Minute(); m >= 8*60+30 && m < 15*60 {
			rth = append(rth, klines([]row{r}, 60_000)[0])
		}
	}
	var sum float64
	for _, k := range market.AggregateToTF(rth, 1, 5) {
		rg := k.High - k.Low
		sum += rg
		st.N5m++
		if rg > st.MaxRange {
			st.MaxRange = rg
		}
		if rg > 50 {
			st.Over50++
		}
	}
	if st.N5m > 0 {
		st.MeanRange = sum / float64(st.N5m)
	}
	st.Big = st.MeanRange >= o.bigMean || st.Over50 >= o.bigN50
	return st, nil
}

func writeStats(o opts, stats []dayStats, strat string) error {
	f, err := os.Create(filepath.Join(o.out, "days.tsv"))
	if err != nil {
		return err
	}
	fmt.Fprintln(f, "day\tcontract\tn_5m\tmean_range\tmax_range\tover_50\tbig\tevents\tintents\tisb_box_5m_events\tpanics")
	var big []string
	for _, s := range stats {
		fmt.Fprintf(f, "%s\t%s\t%d\t%.2f\t%.2f\t%d\t%v\t%d\t%d\t%d\t%d\n", s.Day, s.Contract, s.N5m, s.MeanRange, s.MaxRange, s.Over50, s.Big, s.Events, s.Intents, s.Box5m, s.Panics)
		if s.Big {
			big = append(big, s.Day)
		}
	}
	f.Close()
	sum := map[string]any{"strategy": strat, "import_shift": o.importShift, "seed_at": o.seedAt, "days": stats, "big_days": big,
		"big_rule": fmt.Sprintf("mean 5m RTH range >= %.0f pts OR >= %d candles > 50 pts (tool knobs, not course numbers)", o.bigMean, o.bigN50)}
	js, _ := json.MarshalIndent(sum, "", "  ")
	return os.WriteFile(filepath.Join(o.out, "summary.json"), js, 0o644)
}

// liveBoxRe picks the 5m ISB box lines (and the isb_5m break-lock lines) from a
// live log; the trader tag and wall-clock prefix are dropped so the replay's
// lines compare by content.
var liveBoxRe = regexp.MustCompile(`🧑‍🏫 (.*(5m ISB box|isb_5m box).*)$`)

func boxLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		if m := liveBoxRe.FindStringSubmatch(l); m != nil {
			out = append(out, strings.TrimSpace(m[1]))
		}
	}
	return out
}

// traderSide marks lines that depend on the ledger, the broker, the wall clock or
// boot wiring — state the replay does not have — so they are left out of the
// wider note comparison (they are still in the JSONL).
var traderSide = regexp.MustCompile(`funnel|positions rebuilt|stale-data|seams|sources wired|course-|AI decision|ledger|placement|REFUSED|SIZED|armed|placed|stall|day-gate|re-seed|swing runner|swing "|caught up|seed|limits rebuild|key-level history|exit fork|exit mode|exit-drive`)

var msRe = regexp.MustCompile(`\s*\[\d+ ms\]`)

func normNote(m string) string {
	m = strings.TrimSpace(msRe.ReplaceAllString(m, ""))
	if m == "" || traderSide.MatchString(m) {
		return ""
	}
	return m
}

// compareLiveLog diffs the replay's 5m ISB box lines for the log's day against
// the live log's lines for ONE trader (the first trader tag seen; every mentor
// trader evaluates the same bars).
func compareLiveLog(o opts, stdout io.Writer) error {
	m := regexp.MustCompile(`vl_(\d{4}-\d{2}-\d{2})\.log$`).FindStringSubmatch(o.liveLog)
	if m == nil {
		return fmt.Errorf("-live-log must be a vl_<YYYY-MM-DD>.log")
	}
	day := m[1]
	raw, err := os.ReadFile(o.liveLog)
	if err != nil {
		return err
	}
	tagRe := regexp.MustCompile(`\[trader_id=([^ \]]+)`)
	var tag string
	var liveSel []string
	for _, l := range strings.Split(string(raw), "\n") {
		if !liveBoxRe.MatchString(l) {
			continue
		}
		tm := tagRe.FindStringSubmatch(l)
		if tm == nil {
			continue
		}
		if tag == "" {
			tag = tm[1]
		}
		if tm[1] == tag {
			liveSel = append(liveSel, l)
		}
	}
	live := boxLines(liveSel)
	jf, err := os.ReadFile(filepath.Join(o.out, day+".jsonl"))
	if err != nil {
		return fmt.Errorf("the live log's day %s was not replayed: %w", day, err)
	}
	var rep []string
	for _, l := range strings.Split(string(jf), "\n") {
		if l == "" {
			continue
		}
		var ev map[string]any
		if json.Unmarshal([]byte(l), &ev) != nil || ev["kind"] != "log" || ev["phase"] != "tick" {
			continue
		}
		msg := fmt.Sprint(ev["msg"])
		if mm := liveBoxRe.FindStringSubmatch("🧑‍🏫 " + strings.TrimPrefix(msg, "🧑‍🏫 ")); mm != nil {
			rep = append(rep, strings.TrimSpace(mm[1]))
		}
	}
	// The replay may start earlier than the live boot (or the reverse): compare
	// only the window both cover — from the first live line's bar onward.
	out := &strings.Builder{}
	fmt.Fprintf(out, "PARITY %s — live trader %s: %d 5m-box lines; replay: %d\n", day, tag, len(live), len(rep))
	lset, rset := map[string]int{}, map[string]int{}
	for _, l := range live {
		lset[l]++
	}
	for _, l := range rep {
		rset[l]++
	}
	diff := 0
	for _, l := range live {
		if rset[l] == 0 {
			fmt.Fprintf(out, "LIVE ONLY:   %s\n", l)
			diff++
		}
	}
	for _, l := range rep {
		if lset[l] == 0 {
			fmt.Fprintf(out, "REPLAY ONLY: %s\n", l)
			diff++
		}
	}
	if diff == 0 {
		fmt.Fprintf(out, "MATCH: every live 5m-box line is in the replay and vice versa\n")
	}
	// The wider read: every evaluator note line (box, trigger, level, ORB,
	// trendline, swing …) after the live boot, minus trader-side lines that
	// depend on the ledger / broker / wall clock, which the replay has not.
	liveAll, repAll := map[string]int{}, map[string]int{}
	firstLive := ""
	for _, l := range strings.Split(string(raw), "\n") {
		if tm := tagRe.FindStringSubmatch(l); tm == nil || tm[1] != tag {
			continue
		}
		i := strings.Index(l, "🧑‍🏫")
		if i < 0 {
			continue
		}
		msg := normNote(l[i+len("🧑‍🏫"):])
		if msg == "" {
			continue
		}
		if firstLive == "" && len(l) >= 14 {
			firstLive = l[6:14]
		}
		liveAll[msg]++
	}
	for _, l := range strings.Split(string(jf), "\n") {
		var ev map[string]any
		if l == "" || json.Unmarshal([]byte(l), &ev) != nil || ev["kind"] != "log" || ev["phase"] != "tick" {
			continue
		}
		if msg := normNote(strings.TrimPrefix(fmt.Sprint(ev["msg"]), "🧑‍🏫")); msg != "" {
			repAll[msg]++
		}
	}
	nl, nr := 0, 0
	for _, v := range liveAll {
		nl += v
	}
	for _, v := range repAll {
		nr += v
	}
	fmt.Fprintf(out, "\nALL EVALUATOR NOTES (trader-side lines excluded): live %d, replay %d\n", nl, nr)
	for _, k := range sortedKeys(liveAll) {
		if d := liveAll[k] - repAll[k]; d > 0 {
			fmt.Fprintf(out, "LIVE ONLY   x%d: %s\n", d, k)
		}
	}
	for _, k := range sortedKeys(repAll) {
		if d := repAll[k] - liveAll[k]; d > 0 {
			fmt.Fprintf(out, "REPLAY ONLY x%d: %s\n", d, k)
		}
	}
	fmt.Fprint(stdout, out.String())
	return os.WriteFile(filepath.Join(o.out, "parity-"+day+".txt"), []byte(out.String()), 0o644)
}
