package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
)

// Event is one JSONL line. Every value is READ from the production evaluator
// (state before/after the tick, the intents it emitted, its refusal ledger, or
// the line the trader logged); nothing here decides anything.
type Event map[string]any

func ct(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).In(kernel.CTLocation()).Format("2006-01-02 15:04")
}

// snap is the part of the evaluator state the replay diffs per tick.
type snap struct {
	box5, box15, box30 *mentor.ISBBox
	latched            map[string]mentor.Box
	postEscape         []mentor.Box
	carried            map[string]mentor.Box
	trig5              mentor.TriggerLine
	trig15, trig30     *mentor.TriggerLine
	govTF              int
	breakLock          string
	breakLockV         *mentor.BreakLock
	keyLevels          map[string]mentor.Level
	levels             map[string]mentor.Level
	trendlines         map[string]mentor.Trendline
	isbArms            map[string]mentor.ISBArm
	levelArms          map[string]mentor.LevelArm
	swing              string
	orb                mentor.ORB
	refusals           map[string]int
	limitRefusals      map[string]int
	counters           map[string]int
	trader             map[string]int
	dayGateKey         string
	htf                string
}

func cloneBox(b *mentor.ISBBox) *mentor.ISBBox {
	if b == nil {
		return nil
	}
	c := *b
	return &c
}

func copyInts(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// movingLevel: kinds whose price (and so key) moves every bar — reported by
// their own detectors (EMA lines are not detector events; trendlines are diffed
// as lines), never as level create/delete churn.
func movingLevel(k mentor.LevelKind) bool {
	switch k {
	case mentor.KindEMA34, mentor.KindEMA9, mentor.KindEMA34HTF, mentor.KindTrendline, mentor.KindTrendlineTarget:
		return true
	}
	return false
}

func tlKey(t mentor.Trendline) string {
	return fmt.Sprintf("%s|tf%d|%d|%d", t.Side, t.TFMin, t.P0T, t.P1T)
}

func takeSnap(e *mentor.Evaluator, traderCounts map[string]int) snap {
	st := &e.State
	s := snap{
		box5: cloneBox(st.ISBBox), box15: cloneBox(st.ISBBox15m), box30: cloneBox(st.ISBBox30m),
		latched: map[string]mentor.Box{}, carried: map[string]mentor.Box{},
		trig5: st.Trigger, govTF: st.TriggerGovTF,
		keyLevels: map[string]mentor.Level{}, levels: map[string]mentor.Level{},
		trendlines: map[string]mentor.Trendline{}, isbArms: map[string]mentor.ISBArm{}, levelArms: map[string]mentor.LevelArm{},
		orb: st.ORB, refusals: copyInts(st.Refusals), limitRefusals: copyInts(st.Limits.Refusals),
		counters: copyInts(st.Counters), trader: copyInts(traderCounts), dayGateKey: st.DayGateReport.Key,
	}
	for k, v := range st.LatchedBoxes {
		s.latched[k] = v
	}
	s.postEscape = append(s.postEscape, st.PostEscapeBoxes...)
	for k, v := range st.CarriedBoxes {
		s.carried[k] = v
	}
	if st.Trigger15 != nil {
		t := *st.Trigger15
		s.trig15 = &t
	}
	if st.Trigger30 != nil {
		t := *st.Trigger30
		s.trig30 = &t
	}
	if st.BreakLock != nil {
		b := *st.BreakLock
		b.Under = nil
		s.breakLockV = &b
		js, _ := json.Marshal(st.BreakLock)
		s.breakLock = string(js)
	}
	for _, l := range st.SeedLevels {
		s.keyLevels[l.Key] = l
	}
	for _, l := range st.Levels {
		if !movingLevel(l.Kind) && l.Kind != mentor.KindKeyLevel {
			s.levels[l.Key] = l
		}
	}
	for _, t := range st.ChartTrendlines {
		s.trendlines[tlKey(t)] = t
	}
	for k, v := range st.ISBArms {
		s.isbArms[k] = v
	}
	for k, v := range st.LevelArms {
		s.levelArms[k] = v
	}
	sw := struct {
		Line        float64 `json:"line"`
		BucketStart string  `json:"bucket"`
		FirstTouch  any     `json:"first_touch,omitempty"`
		Pending     any     `json:"pending,omitempty"`
		Pos         any     `json:"pos,omitempty"`
		LineStale   bool    `json:"line_stale,omitempty"`
	}{st.Swing.Line, ct(st.Swing.BucketStart), st.Swing.FirstTouch, st.Swing.Pending, st.Swing.Pos, st.Swing.LineStale}
	js, _ := json.Marshal(sw)
	s.swing = string(js)
	hj, _ := json.Marshal(st.HTF)
	s.htf = string(hj)
	return s
}

func boxFields(prefix string, b *mentor.ISBBox, tfMin int, bars []market.Kline) Event {
	ev := Event{prefix + "dir": string(b.Dir), prefix + "low": b.Low, prefix + "high": b.High,
		prefix + "inside_at": ct(b.AtTime), prefix + "mother_at": ct(b.AtTime - int64(tfMin)*60_000)}
	if b.Provisional {
		ev[prefix+"provisional"] = true
	}
	if b.Escaped != "" {
		ev[prefix+"escaped"] = string(b.Escaped)
	}
	if b.MergedTo != 0 {
		ev[prefix+"merged_to"] = ct(b.MergedTo)
	}
	// Display enrichment only: the mother / inside candle OHLC aggregated from
	// the same closed 1m window the tick read (market.AggregateToTF, the
	// persister's bucket convention). The box itself is the evaluator's.
	if tfMin > 0 && len(bars) > 0 {
		agg := market.AggregateToTF(bars, 1, tfMin)
		for _, k := range agg {
			switch k.OpenTime {
			case b.AtTime:
				ev[prefix+"inside_ohlc"] = []float64{k.Open, k.High, k.Low, k.Close}
			case b.AtTime - int64(tfMin)*60_000:
				ev[prefix+"mother_ohlc"] = []float64{k.Open, k.High, k.Low, k.Close}
			}
		}
	}
	return ev
}

func merge(dst Event, src Event) Event {
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func diffISBBox(tf string, tfMin int, a, b *mentor.ISBBox, bars []market.Kline) []Event {
	kind := "isb_box_" + tf
	switch {
	case a == nil && b == nil:
		return nil
	case a == nil:
		return []Event{merge(Event{"kind": kind + "_formed"}, boxFields("", b, tfMin, bars))}
	case b == nil:
		return []Event{merge(Event{"kind": kind + "_deleted"}, boxFields("", a, tfMin, bars))}
	case a.AtTime == b.AtTime && a.Dir == b.Dir && a.Provisional == b.Provisional && a.MergedTo != b.MergedTo:
		// ISB5-F1 mode merge: the same box took in a newer 5m candle (it may
		// widen, and a 1m break during that candle is taken back).
		if a.High == b.High && a.Low == b.Low && a.Escaped == b.Escaped {
			return nil
		}
		ev := merge(Event{"kind": kind + "_merged"}, boxFields("", b, tfMin, bars))
		return []Event{merge(ev, boxFields("old_", a, tfMin, bars))}
	case a.AtTime != b.AtTime || a.High != b.High || a.Low != b.Low || a.Dir != b.Dir || a.Provisional != b.Provisional:
		ev := merge(Event{"kind": kind + "_replaced"}, boxFields("", b, tfMin, bars))
		return []Event{merge(ev, boxFields("old_", a, tfMin, bars))}
	case a.Escaped != b.Escaped:
		return []Event{merge(Event{"kind": kind + "_broken"}, boxFields("", b, tfMin, bars))}
	}
	return nil
}

func boxEv(kind string, b mentor.Box) Event {
	return Event{"kind": kind, "box_kind": string(b.Kind), "key": b.Key, "top": b.Top, "bottom": b.Bottom,
		"formed_at": ct(b.FormedAtMs), "tf": b.TF, "flipped": b.Flipped, "touches": b.Touches}
}

func diffBoxes(kind string, a, b map[string]mentor.Box) []Event {
	var out []Event
	for _, k := range sortedKeys(b) {
		v := b[k]
		if o, ok := a[k]; !ok || o.Key != v.Key {
			ev := boxEv(kind+"_set", v)
			ev["role"] = k
			out = append(out, ev)
		}
	}
	for _, k := range sortedKeys(a) {
		v := a[k]
		if _, ok := b[k]; !ok {
			ev := boxEv(kind+"_removed", v)
			ev["role"] = k
			out = append(out, ev)
		}
	}
	return out
}

func trigEv(tf string, a, b *mentor.TriggerLine) []Event {
	if b == nil && a == nil {
		return nil
	}
	if b == nil {
		return []Event{{"kind": "trigger_" + tf + "_cleared"}}
	}
	if a != nil && a.Dir == b.Dir && a.Price == b.Price {
		return nil
	}
	ev := Event{"kind": "trigger_" + tf + "_moved", "dir": string(b.Dir), "price": b.Price, "bar": ct(b.LastBar.OpenTime)}
	if a != nil {
		ev["old_dir"], ev["old_price"] = string(a.Dir), a.Price
	}
	return []Event{ev}
}

// diffSnaps turns two state snapshots into detector events (sorted keys for a
// stable file).
func diffSnaps(a, b snap, bars []market.Kline) []Event {
	var out []Event
	out = append(out, diffISBBox("5m", 5, a.box5, b.box5, bars)...)
	out = append(out, diffISBBox("15m", 15, a.box15, b.box15, bars)...)
	out = append(out, diffISBBox("30m", 30, a.box30, b.box30, bars)...)
	out = append(out, diffBoxes("ftg_box_latched", a.latched, b.latched)...)
	out = append(out, diffBoxes("ftg_box_carried", a.carried, b.carried)...)
	if len(b.postEscape) > len(a.postEscape) {
		for _, bx := range b.postEscape[len(a.postEscape):] {
			out = append(out, boxEv("ftg_box_post_escape_added", bx))
		}
	} else if len(b.postEscape) < len(a.postEscape) {
		out = append(out, Event{"kind": "ftg_box_post_escape_cleared", "n": len(a.postEscape) - len(b.postEscape)})
	}
	t5a, t5b := a.trig5, b.trig5
	out = append(out, trigEv("5m", &t5a, &t5b)...)
	out = append(out, trigEv("15m", a.trig15, b.trig15)...)
	out = append(out, trigEv("30m", a.trig30, b.trig30)...)
	if a.govTF != b.govTF {
		out = append(out, Event{"kind": "trigger_escalation", "from_tf": a.govTF, "to_tf": b.govTF})
	}
	if a.breakLock != b.breakLock {
		if b.breakLockV == nil {
			out = append(out, Event{"kind": "break_lock_cleared"})
		} else {
			l := b.breakLockV
			out = append(out, Event{"kind": "break_lock_set", "side": string(l.Side), "lo": l.Lo, "hi": l.Hi, "at": ct(l.At), "src": l.Src})
		}
	}
	for _, k := range sortedKeys(b.keyLevels) {
		if _, ok := a.keyLevels[k]; !ok {
			l := b.keyLevels[k]
			out = append(out, Event{"kind": "key_level_created", "key": k, "price": l.Price, "at": ct(l.AtTime), "role": l.Role, "tf": l.TF})
		}
	}
	for _, k := range sortedKeys(a.keyLevels) {
		if _, ok := b.keyLevels[k]; !ok {
			l := a.keyLevels[k]
			out = append(out, Event{"kind": "key_level_deleted", "key": k, "price": l.Price, "at": ct(l.AtTime), "role": l.Role, "tf": l.TF})
		}
	}
	for _, k := range sortedKeys(b.levels) {
		if _, ok := a.levels[k]; !ok {
			l := b.levels[k]
			out = append(out, Event{"kind": "level_created", "level_kind": string(l.Kind), "key": k, "price": l.Price, "at": ct(l.AtTime), "role": l.Role})
		}
	}
	for _, k := range sortedKeys(a.levels) {
		if _, ok := b.levels[k]; !ok {
			l := a.levels[k]
			out = append(out, Event{"kind": "level_deleted", "level_kind": string(l.Kind), "key": k, "price": l.Price, "at": ct(l.AtTime), "role": l.Role})
		}
	}
	for _, k := range sortedKeys(b.trendlines) {
		t := b.trendlines[k]
		o, ok := a.trendlines[k]
		switch {
		case !ok:
			out = append(out, Event{"kind": "trendline_created", "side": string(t.Side), "tf_min": t.TFMin, "p0": ct(t.P0T), "p0_px": t.P0Px, "p1": ct(t.P1T), "p1_px": t.P1Px, "valid": t.ValidAt > 0, "dead": t.Dead})
		case !o.Dead && t.Dead:
			out = append(out, Event{"kind": "trendline_dead", "side": string(t.Side), "tf_min": t.TFMin, "p0": ct(t.P0T), "p1": ct(t.P1T)})
		case o.ValidAt == 0 && t.ValidAt > 0:
			out = append(out, Event{"kind": "trendline_valid", "side": string(t.Side), "tf_min": t.TFMin, "p0": ct(t.P0T), "p1": ct(t.P1T)})
		}
	}
	for _, k := range sortedKeys(a.trendlines) {
		if _, ok := b.trendlines[k]; !ok {
			t := a.trendlines[k]
			out = append(out, Event{"kind": "trendline_removed", "side": string(t.Side), "tf_min": t.TFMin, "p0": ct(t.P0T), "p1": ct(t.P1T)})
		}
	}
	for _, k := range sortedKeys(b.isbArms) {
		if _, ok := a.isbArms[k]; !ok {
			v := b.isbArms[k]
			out = append(out, Event{"kind": "isb_arm_created", "arm": k, "side": string(v.Side), "exec_tf": v.TFMin,
				"first_bar": ct(v.FirstBar.OpenTime), "first_ohlc": []float64{v.FirstBar.Open, v.FirstBar.High, v.FirstBar.Low, v.FirstBar.Close}})
		}
	}
	for _, k := range sortedKeys(a.isbArms) {
		if _, ok := b.isbArms[k]; !ok {
			out = append(out, Event{"kind": "isb_arm_removed", "arm": k})
		}
	}
	for _, k := range sortedKeys(b.levelArms) {
		if _, ok := a.levelArms[k]; !ok {
			v := b.levelArms[k]
			out = append(out, Event{"kind": "level_arm_created", "level_key": k, "arm": v.ArmID, "side": string(v.Side), "level_price": v.LevelPrice})
		}
	}
	for _, k := range sortedKeys(a.levelArms) {
		if _, ok := b.levelArms[k]; !ok {
			out = append(out, Event{"kind": "level_arm_removed", "level_key": k, "arm": a.levelArms[k].ArmID})
		}
	}
	if a.swing != b.swing {
		var v any
		_ = json.Unmarshal([]byte(b.swing), &v)
		out = append(out, Event{"kind": "swing4h_state", "state": v})
	}
	if a.htf != b.htf {
		var v any
		_ = json.Unmarshal([]byte(b.htf), &v)
		out = append(out, Event{"kind": "htf_state", "state": v})
	}
	if a.orb != b.orb {
		out = append(out, Event{"kind": "orb", "high": b.orb.High, "low": b.orb.Low, "drawn": b.orb.Drawn, "escaped": string(b.orb.Escaped)})
	}
	out = append(out, diffCounts("refusal", "kernel", a.refusals, b.refusals)...)
	out = append(out, diffCounts("refusal", "limits", a.limitRefusals, b.limitRefusals)...)
	out = append(out, diffCounts("counter", "kernel", a.counters, b.counters)...)
	out = append(out, diffCounts("trader_counter", "trader", a.trader, b.trader)...)
	return out
}

func diffCounts(kind, src string, a, b map[string]int) []Event {
	var out []Event
	for _, k := range sortedKeys(b) {
		if d := b[k] - a[k]; d != 0 {
			out = append(out, Event{"kind": kind, "src": src, "reason": k, "n": d})
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func intentEv(in mentor.Intent) Event {
	ev := Event{"kind": "intent", "action": string(in.Action), "setup": in.Setup, "side": string(in.Side),
		"arm": in.ArmID, "reason": in.Reason}
	if in.Price != 0 || in.Stop != 0 || in.Target != 0 {
		ev["entry"], ev["stop"], ev["target"] = in.Price, in.Stop, in.Target
		ev["stop_pts"], ev["target_pts"] = in.StopPts, in.TargetPts
	}
	if in.LevelKey != "" {
		ev["level_key"] = in.LevelKey
	}
	if in.Flag != "" {
		ev["flag"] = in.Flag
	}
	if in.ExecTFMin != 0 {
		ev["exec_tf"] = in.ExecTFMin
	}
	if in.RefBarMs != 0 {
		ev["ref_bar_close"] = ct(in.RefBarMs)
	}
	if in.Confluence {
		ev["confluence"] = true
	}
	if in.RunnerTarget != 0 {
		ev["runner_target"] = in.RunnerTarget
	}
	return ev
}

// logCats classify a captured log line (the trader logs every evaluator note
// verbatim). The category is a label for grepping, never a decision.
var logCats = []struct {
	re  *regexp.Regexp
	cat string
}{
	{regexp.MustCompile(`5m ISB box`), "isb_box_5m"},
	{regexp.MustCompile(`15m early read`), "early_read_15m"},
	{regexp.MustCompile(`(?i)trigger.*(escalat|govern)`), "trigger_escalation"},
	{regexp.MustCompile(`break lock`), "break_lock"},
	{regexp.MustCompile(`(?i)reverse ISB|isbrev`), "reverse_isb"},
	{regexp.MustCompile(`(?i)\bftg[hl]\b|carried box|box latch`), "ftg_box"},
	{regexp.MustCompile(`(?i)trendline`), "trendline"},
	{regexp.MustCompile(`(?i)key level|key-level`), "key_level"},
	{regexp.MustCompile(`(?i)invalid`), "level_invalid"},
	{regexp.MustCompile(`(?i)swing`), "swing4h"},
	{regexp.MustCompile(`(?i)\bPHL\b|\bPLH\b`), "phl_plh"},
	{regexp.MustCompile(`(?i)ORB`), "orb"},
	{regexp.MustCompile(`(?i)trigger`), "trigger"},
	{regexp.MustCompile(`(?i)funnel`), "funnel"},
	{regexp.MustCompile(`(?i)refus|REFUSED`), "refusal"},
}

func logCat(msg string) string {
	for _, c := range logCats {
		if c.re.MatchString(msg) {
			return c.cat
		}
	}
	return "other"
}

func stripTag(msg, tag string) string {
	return strings.TrimSpace(strings.TrimPrefix(msg, tag))
}
