package main

// October video-trade scorecard (RED baseline, 2026-11-14).
//
// Owner order: "mentor mode = the October course exactly". This test replays
// the days the mentor trades on video through the PRODUCTION mentor code (the
// same seed + per-bar entry the tool and the other goldens use, DEFAULT mentor
// config = what live strategies run, since they store no mentor_tuning) and
// scores the bot against his 55 checkable real-chart trades
// (testdata/oct_video_trades.json, from research-video-trades.md).
//
// Scoring (a "bot entry" = a place_stop_entry / place_stop_limit_entry intent):
//   - took / marked WITH a price: PASS if the bot arms an entry on the same side,
//     within +-2.0 pts of his entry, whose tick falls inside his time window
//     (+-2 one-minute bars), of a matching setup family.
//   - took / marked WITHOUT a price: PASS if the bot arms a same-side entry of a
//     matching family inside the window.
//   - refused: PASS if the bot does NOT arm a matching same-side entry in the
//     window (and, when his price(s) are given, none within +-2.0 pts of them).
//   - side "" (side-less items) matches either side; family ANY matches any entry.
//
// Family map (JSON "family" is a comma list = union). Intent.Setup is one of
// ISB / PHL / PLH / BOX / SWING4H; trendline, key-level and EMA entries ride the
// same setups and are told apart by the intent's reason text / level key:
//
//	ISB       Setup ISB (1m/5m ISB, reverse ISB, 5m-box ISB behaviour)
//	PHL       Setup PHL or PLH (PHL/PLH, rolling)
//	BOX       Setup BOX (FTGH / FTGL box)
//	TRENDLINE reason mentions "trendline"
//	LEVEL     intent carries a LevelKey, or reason mentions "level" (key level /
//	          level reject / PHL at a level)
//	EMA       reason mentions "EMA" or "reverse ISB" (isbrev / EMA 9 / EMA 34)
//	ANY       any entry
//
// Env: OCT_SCORECARD_REPORT=1 prints the table without failing (for the CTO's
// report). OCT_SCORECARD_DUMP=1 also prints every entry the bot armed per replayed
// day (harness debugging).
//
// acceptedMisses starts EMPTY: a trade may be listed there only with a written
// reason, by the CTO, after the build phase.

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"vl/calendar"
	"vl/kernel"
	"vl/kernel/mentor"
)

// acceptedMisses: id -> written reason. EMPTY at the baseline on purpose.
var acceptedMisses = map[string]string{}

const octPriceTol = 2.0 // points
const octSlackBars = 2  // one-minute bars either side of the window

type octTrade struct {
	ID      string    `json:"id"`
	Video   string    `json:"video"`
	Date    string    `json:"date"`
	Start   string    `json:"start"`
	End     string    `json:"end"`
	Family  string    `json:"family"`
	Side    string    `json:"side"`
	Entry   *float64  `json:"entry"`
	Entries []float64 `json:"entries"`
	Outcome string    `json:"outcome"`
	Cite    string    `json:"cite"`
	Note    string    `json:"note"`
}

type octFile struct {
	Skipped []struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	} `json:"skipped"`
	Trades []octTrade `json:"trades"`
}

// octArm is one entry the bot armed.
type octArm struct {
	At     time.Time // the tick's bar-close instant (CT)
	Side   string
	Setup  string
	Entry  float64
	Reason string
	Level  string
}

// octDay is everything one replayed session produced.
type octDay struct {
	arms []octArm
	logs []octLine // refusal reasons + related notes
}

type octLine struct {
	At   time.Time
	Text string
}

func octParseCT(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, kernel.CTLocation())
	if err != nil {
		panic(err)
	}
	return t
}

// octSession returns the replay day d whose session (17:00 CT the evening before
// -> 16:00 CT) contains the instant.
func octSession(at time.Time) string {
	if at.Hour() >= 17 {
		at = at.AddDate(0, 0, 1)
	}
	return at.Format("2006-01-02")
}

func octReplay(t *testing.T, bs *barSet, d string) octDay {
	t.Helper()
	seedAt, err := seedInstant("session", d)
	if err != nil {
		t.Fatal(err)
	}
	loc := kernel.CTLocation()
	day, _ := time.ParseInLocation("2006-01-02", d, loc)
	endMs := time.Date(day.Year(), day.Month(), day.Day(), 16, 0, 0, 0, loc).UnixMilli()
	var out octDay
	var cur []Event
	testCap.take()
	rp := newReplayer(bs, nil, map[string][]calendar.Event{}, testCap, func(ev Event) { cur = append(cur, ev) })
	rp.seed(seedAt)
	for _, r := range bs.byTF["1m"][fixtureContract] {
		if r.T+60_000 <= seedAt.UnixMilli() || r.T+60_000 > endMs {
			continue
		}
		cur = nil
		b := klines([]row{r}, 60_000)[0]
		if rp.step(b, seedAt.UnixMilli()) {
			t.Fatalf("panic on the %s bar: %v", ct(b.OpenTime), cur)
		}
		for _, ev := range cur {
			ts, _ := ev["t"].(string)
			if ts == "" {
				continue
			}
			at := octParseCT(ts)
			switch fmt.Sprint(ev["kind"]) {
			case "intent":
				a := fmt.Sprint(ev["action"])
				if a != string(mentor.PlaceStopEntry) && a != string(mentor.PlaceStopLimitEntry) {
					continue
				}
				px, _ := ev["entry"].(float64)
				out.arms = append(out.arms, octArm{At: at, Side: fmt.Sprint(ev["side"]), Setup: fmt.Sprint(ev["setup"]),
					Entry: px, Reason: fmt.Sprint(ev["reason"]), Level: fmt.Sprint(ev["level_key"])})
			case "refusal":
				out.logs = append(out.logs, octLine{at, fmt.Sprintf("refusal[%v] %v x%v", ev["src"], ev["reason"], ev["n"])})
			case "log":
				if c := fmt.Sprint(ev["cat"]); c == "refusal" || c == "phl_plh" || c == "key_level" || c == "ftg_box" || c == "trendline" || c == "reverse_isb" || c == "isb_box_5m" || c == "break_lock" {
					out.logs = append(out.logs, octLine{at, fmt.Sprintf("%s: %v", c, ev["msg"])})
				}
			}
		}
	}
	return out
}

func octFamilyMatch(fam string, a octArm) bool {
	for _, f := range strings.Split(fam, ",") {
		reason := strings.ToLower(a.Reason)
		ok := false
		switch strings.TrimSpace(f) {
		case "ANY":
			ok = true
		case "ISB":
			ok = a.Setup == "ISB"
		case "PHL":
			ok = a.Setup == "PHL" || a.Setup == "PLH"
		case "BOX":
			ok = a.Setup == "BOX"
		case "TRENDLINE":
			ok = strings.Contains(reason, "trendline")
		case "LEVEL":
			ok = (a.Level != "" && a.Level != "<nil>") || strings.Contains(reason, "level")
		case "EMA":
			ok = strings.Contains(reason, "ema") || strings.Contains(reason, "reverse isb")
		default:
			panic("unknown family " + f)
		}
		if ok {
			return true
		}
	}
	return false
}

func octPrices(tr octTrade) []float64 {
	var p []float64
	if tr.Entry != nil {
		p = append(p, *tr.Entry)
	}
	return append(p, tr.Entries...)
}

func octSideMatch(want, got string) bool {
	if want == "" {
		return true
	}
	return (want == "long" && got == string(mentor.SideLong)) || (want == "short" && got == string(mentor.SideShort))
}

func octNear(prices []float64, px float64) bool {
	for _, p := range prices {
		if math.Abs(px-p) <= octPriceTol+1e-9 {
			return true
		}
	}
	return false
}

func octFmtArm(a octArm) string {
	return fmt.Sprintf("%s %s %s@%.2f", a.At.Format("15:04"), a.Side, a.Setup, a.Entry)
}

type octResult struct {
	tr      octTrade
	pass    bool
	expect  string
	bot     string
	reasons []string
}

func octScore(tr octTrade, days map[string]octDay) octResult {
	start := octParseCT(tr.Date + " " + tr.Start).Add(-octSlackBars * time.Minute)
	end := octParseCT(tr.Date + " " + tr.End).Add(octSlackBars * time.Minute)
	// A tick at close time c evaluates the bar that opened c-1m; the window is on
	// bar opens, so compare the open.
	inWin := func(at time.Time) bool {
		open := at.Add(-time.Minute)
		return !open.Before(start) && !open.After(end)
	}
	prices := octPrices(tr)
	var arms []octArm // same-side, family-matching arms inside the window
	var all []octArm  // every arm inside the window (for the "bot did" column)
	var lines []octLine
	seen := map[string]bool{}
	for _, d := range []string{octSession(start), octSession(end)} {
		if seen[d] {
			continue
		}
		seen[d] = true
		for _, a := range days[d].arms {
			if inWin(a.At) {
				all = append(all, a)
				if octSideMatch(tr.Side, a.Side) && octFamilyMatch(tr.Family, a) {
					arms = append(arms, a)
				}
			}
		}
		for _, l := range days[d].logs {
			if inWin(l.At) {
				lines = append(lines, l)
			}
		}
	}
	var priced []octArm
	for _, a := range arms {
		if len(prices) == 0 || octNear(prices, a.Entry) {
			priced = append(priced, a)
		}
	}
	res := octResult{tr: tr}
	side := tr.Side
	if side == "" {
		side = "any side"
	}
	where := fmt.Sprintf("%s %s-%s", tr.Family, tr.Start, tr.End)
	px := ""
	if len(prices) > 0 {
		px = fmt.Sprintf(" ~%v (+-%.1f)", prices, octPriceTol)
	}
	switch tr.Outcome {
	case "took", "marked":
		res.expect = fmt.Sprintf("ARM %s %s%s", side, where, px)
		res.pass = len(priced) > 0
		if res.pass {
			res.bot = "armed " + octFmtArm(priced[0])
		} else if len(arms) > 0 {
			res.bot = "armed same side/family but off-price: " + octFmtArm(arms[0])
		} else if len(all) > 0 {
			res.bot = "armed only other side/family: " + octFmtArm(all[0])
		} else {
			res.bot = "no entry armed"
		}
	case "refused":
		res.expect = fmt.Sprintf("NO ARM %s %s%s", side, where, px)
		res.pass = len(priced) == 0
		if res.pass {
			res.bot = "no matching entry"
		} else {
			res.bot = "ARMED " + octFmtArm(priced[0])
			if a := priced[0]; a.Reason != "" {
				res.bot += " [" + octTrunc(a.Reason, 90) + "]"
			}
		}
	default:
		panic("bad outcome " + tr.Outcome)
	}
	if !res.pass {
		res.reasons = octReasons(tr, lines)
	}
	return res
}

func octTrunc(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// octReasons: the bot's refusal/gate notes in the window — counters grouped by
// reason, plus up to 4 distinct notes naming the same side.
func octReasons(tr octTrade, lines []octLine) []string {
	counts := map[string]int{}
	var notes []string
	seen := map[string]bool{}
	for _, l := range lines {
		if strings.HasPrefix(l.Text, "refusal[") {
			parts := strings.SplitN(l.Text, "] ", 2)
			if len(parts) == 2 {
				k := strings.Fields(parts[1])[0]
				var n int
				fmt.Sscanf(strings.TrimPrefix(parts[1][strings.LastIndex(parts[1], "x"):], "x"), "%d", &n)
				counts[k] += n
			}
			continue
		}
		low := strings.ToLower(l.Text)
		if tr.Side != "" {
			other := "short"
			if tr.Side == "short" {
				other = "long"
			}
			if strings.Contains(low, other) && !strings.Contains(low, tr.Side) {
				continue
			}
		}
		key := octTrunc(l.Text, 110)
		if !seen[key] && len(notes) < 3 {
			seen[key] = true
			notes = append(notes, l.At.Format("15:04")+" "+key)
		}
	}
	var ks []string
	for k := range counts {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool {
		if counts[ks[i]] != counts[ks[j]] {
			return counts[ks[i]] > counts[ks[j]]
		}
		return ks[i] < ks[j]
	})
	var out []string
	for i, k := range ks {
		if i >= 5 {
			break
		}
		out = append(out, fmt.Sprintf("refusal counter %s x%d", k, counts[k]))
	}
	return append(out, notes...)
}

func TestOctVideoScorecard(t *testing.T) {
	raw, err := os.ReadFile("testdata/oct_video_trades.json")
	if err != nil {
		t.Fatal(err)
	}
	var f octFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Trades) != 55 {
		t.Fatalf("scorecard must carry the 55 checkable trades, got %d", len(f.Trades))
	}
	bs := loadFixtureFile(t, "mnq_12-26_1m_20260907-20261103_full.csv.gz")

	// Replay every session any trade window touches, once.
	need := map[string]bool{}
	for _, tr := range f.Trades {
		need[octSession(octParseCT(tr.Date+" "+tr.Start).Add(-octSlackBars*time.Minute))] = true
		need[octSession(octParseCT(tr.Date+" "+tr.End).Add(octSlackBars*time.Minute))] = true
	}
	var dayKeys []string
	for d := range need {
		dayKeys = append(dayKeys, d)
	}
	sort.Strings(dayKeys)
	days := map[string]octDay{}
	for _, d := range dayKeys {
		days[d] = octReplay(t, bs, d)
		if os.Getenv("OCT_SCORECARD_DUMP") != "" {
			var sb strings.Builder
			for _, a := range days[d].arms {
				fmt.Fprintf(&sb, "\n  %s %-5s %-5s %.2f  %s", a.At.Format("01-02 15:04"), a.Side, a.Setup, a.Entry, octTrunc(a.Reason, 100))
			}
			t.Logf("DUMP session %s: %d entries armed%s", d, len(days[d].arms), sb.String())
		}
	}

	var res []octResult
	pass := 0
	var failed []octResult
	for _, tr := range f.Trades {
		r := octScore(tr, days)
		res = append(res, r)
		if r.pass {
			pass++
		} else if _, ok := acceptedMisses[tr.ID]; !ok {
			failed = append(failed, r)
		}
	}

	var sb strings.Builder
	sb.WriteString("\nid | outcome | expected | bot | PASS/FAIL | reason\n")
	for _, r := range res {
		v := "PASS"
		if !r.pass {
			v = "FAIL"
			if why, ok := acceptedMisses[r.tr.ID]; ok {
				v = "ACCEPTED-MISS (" + why + ")"
			}
		}
		fmt.Fprintf(&sb, "%s | %s | %s | %s | %s | %s\n", r.tr.ID, r.tr.Outcome, r.expect, r.bot, v, strings.Join(r.reasons, " ; "))
	}
	for _, s := range f.Skipped {
		fmt.Fprintf(&sb, "SKIPPED %s: %s\n", s.ID, s.Reason)
	}
	fmt.Fprintf(&sb, "SCORE %d/%d\n", pass, len(res))
	t.Log(sb.String())

	if os.Getenv("OCT_SCORECARD_REPORT") != "" {
		return
	}
	if len(failed) > 0 {
		var ids []string
		for _, r := range failed {
			ids = append(ids, r.tr.ID)
		}
		t.Errorf("SCORE %d/%d: %d trade(s) fail the October video scorecard (not in acceptedMisses): %s", pass, len(res), len(failed), strings.Join(ids, ", "))
	}
}
