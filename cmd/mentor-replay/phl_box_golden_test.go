package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"vl/calendar"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
)

// replayFixtureDayState is replayFixtureDay with a per-tick look at the
// evaluator's state: the production seed at 17:00 CT the evening before d,
// then every closed 1m bar through the production step (mentorEvalOnce);
// fn runs after every replayed bar (label "MM-DD HH:MM" CT, the bar's open)
// up to endHM on day d.
func replayFixtureDayState(t *testing.T, fixture, d, endHM string, sc *store.StrategyConfig, fn func(hm string, st *mentor.State)) {
	t.Helper()
	bs := loadFixtureFile(t, fixture)
	seedAt, err := seedInstant("session", d)
	if err != nil {
		t.Fatal(err)
	}
	loc := kernel.CTLocation()
	day, _ := time.ParseInLocation("2006-01-02", d, loc)
	end, _ := time.ParseInLocation("15:04", endHM, loc)
	endMs := time.Date(day.Year(), day.Month(), day.Day(), end.Hour(), end.Minute(), 0, 0, loc).UnixMilli()
	testCap.take()
	rp := newReplayer(bs, sc, map[string][]calendar.Event{}, testCap, func(Event) {})
	rp.seed(seedAt)
	for _, r := range bs.byTF["1m"][fixtureContract] {
		if r.T+60_000 <= seedAt.UnixMilli() || r.T+60_000 > endMs {
			continue
		}
		b := klines([]row{r}, 60_000)[0]
		if rp.step(b, seedAt.UnixMilli()) {
			t.Fatalf("panic on the %s bar", ct(b.OpenTime))
		}
		fn(time.UnixMilli(b.OpenTime).In(loc).Format("01-02 15:04"), &rp.r.Evaluator().State)
	}
}

func ftgBoxStr(b mentor.Box) string {
	return fmt.Sprintf("%s %.2f–%.2f formed %s", strings.ToUpper(string(b.Kind)), b.Bottom, b.Top, ct(b.FormedAtMs))
}

// liveFTG returns the LIVE box of a role (the newest post-escape box, else the
// latched one — mentor.liveBoxes' rule) or nil.
func liveFTG(st *mentor.State, k mentor.BoxKind) *mentor.Box {
	for i := len(st.PostEscapeBoxes) - 1; i >= 0; i-- {
		if st.PostEscapeBoxes[i].Kind == k {
			b := st.PostEscapeBoxes[i]
			return &b
		}
	}
	if b, ok := st.LatchedBoxes[string(k)]; ok {
		return &b
	}
	return nil
}

func ftgStr(b *mentor.Box) string {
	if b == nil {
		return "<none>"
	}
	return ftgBoxStr(*b)
}

// TestPHL2E10PostEscapeFTGLIsTheCourseBox — phl audit PHL-2, the course's own
// worked example E10 (Fri 2026-10-30, MNQ 12-26 1m from data.db, the
// production seed at 17:00 CT 10-29 then every closed bar through
// mentorEvalOnce, DEFAULT config).
//
// OctD3.3-R23 [Oct D3.3 @ 14:20] levelness by a horizontal line; R25 [@ 15:46]
// the box = the highest (lowest) wick of PEAK 1 drawn to the nearest body of
// peak 2; R28 [@ 17:37] peak 2's wick is never included; R30 [@ 19:31] "Tui
// lấy cái râu đỉnh 1 làm chuẩn ... chuyện bắt buộc"; R54 [@ 43:14] FTGL is the
// mirror. E10: bottom 1 = 09:25 L 28,614.75 (confirmed by 09:26 L 28,622.75),
// bottom 2 = 10:42 L 28,612.50 (body 28,626.50) — the mentor drew 28,615.00–
// 28,625.25. Both bottoms form after the 08:43 FTGL 28,710.75–28,742.75
// escaped (09:14), so this is a POST-ESCAPE box.
//
// Before PHL-2 the post-escape box paired the most extreme swing with the
// nearest later swing that did not exceed it (the September rule, no
// levelness, no peak-1 anchor): the live FTGL was 28,612.50–28,619.00 (anchor
// 10:42, partner 10:46), formed 10:47 — 2.25 pts lower, 7 pts thinner, 4
// minutes late.
func TestPHL2E10PostEscapeFTGLIsTheCourseBox(t *testing.T) {
	var at1043, at1050 *mentor.Box
	replayFixtureDayState(t, "mnq_12-26_1m_20261030-20261102_1000.csv.gz", "2026-10-30", "10:50", nil, func(hm string, st *mentor.State) {
		switch hm {
		case "10-30 10:43":
			at1043 = liveFTG(st, mentor.FTGL)
		case "10-30 10:49":
			at1050 = liveFTG(st, mentor.FTGL)
		}
	})
	for _, c := range []struct {
		at string
		b  *mentor.Box
	}{{"10:43", at1043}, {"10:49", at1050}} {
		if b := c.b; b == nil || b.Bottom != 28614.75 || b.Top != 28626.50 || ct(b.FormedAtMs) != "2026-10-30 10:43" {
			t.Fatalf("%s: the live FTGL = %s, want the course box 28,614.75–28,626.50 formed 10:43 (bottom 1 09:25 wick → bottom 2 10:42 body; mentor 28,615.00–28,625.25)", c.at, ftgStr(b))
		}
	}
}

// TestPHL2PeakOneAnchorsThePostEscapeBox — 10-29 19:4x (the same fixture):
// the bottoms 19:46 L 28,961.75 and 19:48 L 28,958.75 are level; the box is
// anchored on peak 1's wick (19:46), never on peak 2's lower wick [OctD3.3-R28
// @ 17:37, R30 @ 19:31]. Before PHL-2: 28,958.75–28,968.25 anchored on 19:48.
func TestPHL2PeakOneAnchorsThePostEscapeBox(t *testing.T) {
	var got *mentor.Box
	replayFixtureDayState(t, "mnq_12-26_1m_20261030-20261102_1000.csv.gz", "2026-10-30", "08:00", nil, func(hm string, st *mentor.State) {
		if hm == "10-29 19:50" {
			got = liveFTG(st, mentor.FTGL)
		}
	})
	if got == nil || got.Bottom != 28961.75 || got.Top != 28964.75 || ct(got.FormedAtMs) != "2026-10-29 19:49" {
		t.Fatalf("10-29 19:50: the live FTGL = %s, want 28,961.75–28,964.75 formed 19:49 (peak 1 = the 19:46 wick, peak 2 = the 19:48 body)", ftgStr(got))
	}
}

// TestPHL2NoVReversalPostEscapeBox — 11-10 (MNQ 12-26 1m, production seed at
// 17:00 CT 11-09): the 07:42 spike low 27,598.50 and the 07:51 low 27,682.75
// are 84.25 pts apart — a V reversal, not a failure to go lower. Before PHL-2
// the post-escape build paired them into the FTGL 27,598.50–27,691.00 (live
// 07:53–08:43). Two swings further apart than flatNearPts are never a box
// [OctD3.3-R23 @ 14:20].
func TestPHL2NoVReversalPostEscapeBox(t *testing.T) {
	replayFixtureDayState(t, "mnq_12-26_1m_20261106-20261110.csv.gz", "2026-11-10", "08:42", nil, func(hm string, st *mentor.State) {
		if hm < "11-10 07:40" {
			return
		}
		if b := liveFTG(st, mentor.FTGL); b != nil && b.Bottom == 27598.50 {
			t.Fatalf("%s: the V-reversal FTGL %s is live — its two bottoms are 84.25 pts apart", hm, ftgStr(b))
		}
	})
}
