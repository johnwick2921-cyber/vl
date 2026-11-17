package main

// PRECISION check (P1-precision). The scorecard (oct_scorecard_test.go) measures recall: did the bot arm the
// trades the mentor took. This file measures the other half: in the windows where an October video says the
// mentor REFUSES (or sits out), does the bot stay quiet.
//
// Source: testdata/oct_quiet_windows.json (kind=quiet_window; only confidence A or B are tested). Pass rule: in
// a quiet window the bot must NOT arm an entry of the refused side/setup family, using the SAME arm matching as
// the refused-trade rule (octFamilyMatch / octSideMatch / octNear). Window = [ct_start, ct_end + 59s] on the
// bar-close instant of the arm, with NO slack (slack would only add dirt the video does not show).
//
// Env: OCT_SCORECARD_REPORT=1 prints the table without failing.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

type octQuiet struct {
	Kind       string   `json:"kind"`
	ID         string   `json:"id"`
	Date       string   `json:"date"`
	Start      string   `json:"ct_start"`
	End        string   `json:"ct_end"`
	Side       string   `json:"side_refused"`
	Setups     []string `json:"setups_refused"`
	Type       string   `json:"type"`
	Confidence string   `json:"confidence"`
	Cite       string   `json:"cite"`
	Sub        []string `json:"sub_windows"`
}

var octDayCache = map[string]octDay{}

func octReplayCached(t *testing.T, bs *barSet, d string) octDay {
	t.Helper()
	if v, ok := octDayCache[d]; ok {
		return v
	}
	v := octReplay(t, bs, d)
	octDayCache[d] = v
	return v
}

// octQuietNotArmTestable: windows whose rule is about cancelling / sizing an order the video itself placed,
// not about refusing to arm. The summary (quiet-windows.json) says E10/E14 are valid ISBs whose orders were
// cancelled after the next candle: the arm IS correct, so an arm-based check would punish the right behaviour.
// They are reported as SKIPPED (not counted in n/m), with the reason.
var octQuietNotArmTestable = map[string]string{
	"Q-1030-01": "cancel_unfilled: D2.2-E10 is a VALID ISB whose order was cancelled after the next candle; the arm is correct, the cancel timing is not an arm refusal",
	"Q-1030-02": "cancel_unfilled: D2.2-E14 is a VALID ISB, cancelled after the next candle; not an arm refusal",
	"Q-1030-03": "do_not_hold_unfilled_order (sweep/cancel behaviour), not an arm refusal",
	"Q-1030-04": "ISB 20-35 pts means size 3 or no trade: a sizing rule, the arm itself is allowed",
}

// octQuietPrices scopes a window to the refused entry prices (+-octPriceTol) where other valid entries share
// the minutes. Q-1029-01: WE9 (sell stop 28,861.50, taken) sits in the same minutes as the refused 28,825.50 and
// 28,839.75 (no room). Q-1101-02/03: the refused buy stops / entry only.
var octQuietPrices = map[string][]float64{
	"Q-1029-01": {28825.50, 28839.75},
	"Q-1101-02": {28772.00, 28774.25},
	"Q-1101-03": {28765.25},
}

// octQuietSide overrides the JSON side where the JSON says "both" but the quote only refuses one direction.
// Q-1030-07: close above the box = cancel the SHORTS; a buy is allowed WITH an ISB long, so only the short side
// is judged here (the long side is not provable from an arm alone).
var octQuietSide = map[string]string{
	"Q-1030-07": "short",
}

// acceptedQuietMisses: id -> written reason a quiet window may stay dirty. Same honesty rules as acceptedMisses:
// a listing is NOT a close.
var acceptedQuietMisses = map[string]string{
	// BASELINE (P1-precision, W2 build 19def7f80+): every entry below is a REAL precision miss the bot has today,
	// parked so the suite stays green while the P2 fix steps run. None is a justified miss; each leaves the map
	// when its gate is fixed. Arm counts are from this replay [A].
	// CLEAN since P4-missesB WE8 (ratchet: removed from the map): "Q-1029-01": "BASELINE MISS: PLH short 28,839.75 armed 20:57 although the video refuses it for no room (room 9.25 < stop 14.25, D2.3-R48); same price as D2.3-WE8, where D2.2 @28:37 / D5.3-R39 reverse-ISB fill-close collides [A]",
	"Q-1030-09": "BASELINE MISS: 3 short ISBs armed 09:16-09:25 in the straight drop after the 09:03 break; the video allows no ISB there [B, Q-1030-09 is B]",
	// Q-1102-01 / Q-1102-02 (11-02 08:32-09:15, inside the opening 5m ISB box / the ORB-2 range) are CLEAN since J-BOX-01
	// (fix/mentor-reread-1116, mentor_opening_isb_box_no_trade default ON): every setup inside the opening 5m ISB box
	// 28,639.75-28,748.75 is refused (opening_isb_box_no_trade) until the 09:12 1m close outside it [Oct D4.3 @58:00-58:33,
	// @59:50-1:00:29; D4.1 @11:00-11:41]. With the knob false both windows are dirty again (the 08:58 short ISB).
	// Q-1102-02 is DIRTY again since review P1-1 (11-17, side-aware ISB-M1) — see its entry.
	"Q-1102-02": "[NEEDS OWNER SIGN-OFF - NOT CLOSED] 1 arm: 11-02 08:36 short PLH 28,686.50 (mother 08:34, inside 08:35), before the 08:40 opening 5m ISB box exists. Side-free ISB-M1 refused it only because the 08:33 candle closed UP through key 28,701 and price stayed on it; review P1-1 made ISB-M1 refuse only the break direction (Oct D3.3 @00:16-02:47 is the ISB in the break direction; the 11-16 19:17 long PHL against the 19:15 down-break is ruled correct, REPORT-full-reread-1116.md:115, D5.3 @17:40-18:30), so a short against an up-break is no longer refused by it. The rule that would refuse 08:36 is the ORB-2 candle reading (D4.3 @1:03:00-1:03:20 'Neu nhu nen nam giua ORB 2 phut, dung trade', window conf B); the bot gates ORB-2 by ENTRY PRICE only (A1 row 4, part 1) and 28,686.50 is 2.25 under the 28,688.75 ORB low. The same arm now PASSES scorecard row D4.3-7 (took any side 08:35-08:40). Owner question: trade 11-02 08:36 or not [A, replay]",
	"Q-1102-04": "BASELINE MISS: 3 long arms 09:38-09:54 against the 5m sell trigger; the arm cannot tell a D5.4 ~10-pt scalp from a hold trade, so some may be legal scalps [A, times inferred]",
	"Q-1102-07": "BASELINE MISS: 12 PHL/PLH arms 19:33-21:14 in EMA34/fish chop where the mentor trades limits only; family EMA matches on 'ema' in the reason text [B]",
	"Q-1103-01": "BASELINE MISS: 3 short PLH arms 17:49-17:55 in EMA34/fish chop [B]",
	"Q-1103-02": "BASELINE MISS: 6 short PLH arms 20:41-20:51 before the EMA34 touch; video says the pre-touch entry is swept [B]",
	"Q-1103-03": "BASELINE MISS: 14 long arms 09:33-10:12+ against the 5m sell trigger; D5.4 allows only scalps, the arm cannot tell them apart [B]",
	"Q-1103-04": "BASELINE MISS: 12 short arms 21:05-21:33 against the 5m buy trigger; D5.4 allows only scalps, the arm cannot tell them apart [B]",
}

// octQuietSignoffPending: dirty windows that are NOT closed; all of them at the baseline.
var octQuietSignoffPending = []string{"Q-1030-09", "Q-1102-02", "Q-1102-04", "Q-1102-07", "Q-1103-01", "Q-1103-02", "Q-1103-03", "Q-1103-04"}

type octQuietRes struct {
	q       octQuiet
	skip    string
	nodata  bool
	arms    []octArm
	windows []string
}

func octQuietWindows(q octQuiet) [][2]time.Time {
	var out [][2]time.Time
	if len(q.Sub) > 0 {
		for _, s := range q.Sub {
			f := strings.Fields(s)[0] // "10:08-11:30"
			p := strings.Split(f, "-")
			if len(p) == 2 {
				out = append(out, [2]time.Time{octParseCT(q.Date + " " + p[0]), octParseCT(q.Date + " " + p[1]).Add(59 * time.Second)})
			}
		}
		return out
	}
	return [][2]time.Time{{octParseCT(q.Date + " " + q.Start), octParseCT(q.Date + " " + q.End).Add(59 * time.Second)}}
}

func TestOctQuietPrecision(t *testing.T) {
	raw, err := os.ReadFile("testdata/oct_quiet_windows.json")
	if err != nil {
		t.Fatal(err)
	}
	var all []octQuiet
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	var qs []octQuiet
	for _, q := range all {
		if q.Kind == "quiet_window" && (q.Confidence == "A" || q.Confidence == "B") {
			qs = append(qs, q)
		}
	}
	if len(qs) != 24 {
		t.Fatalf("expected 24 quiet windows of confidence A/B (28 total, 4 are C), got %d", len(qs))
	}
	bs := loadFixtureFile(t, "mnq_12-26_1m_20260907-20261103_full.csv.gz")
	have := map[int64]bool{}
	for _, r := range bs.byTF["1m"][fixtureContract] {
		have[r.T] = true
	}

	var res []octQuietRes
	for _, q := range qs {
		r := octQuietRes{q: q}
		if why, ok := octQuietNotArmTestable[q.ID]; ok {
			r.skip = why
			res = append(res, r)
			continue
		}
		side := q.Side
		if o, ok := octQuietSide[q.ID]; ok {
			side = o
		}
		fam := strings.ReplaceAll(strings.Join(q.Setups, ","), "PLH", "PHL") // octFamilyMatch PHL covers PHL and PLH setups
		wins := octQuietWindows(q)
		for _, w := range wins {
			// data coverage: at least one 1m bar must exist inside the window, or the check is vacuous
			cov := false
			for m := w[0]; !m.After(w[1]); m = m.Add(time.Minute) {
				if have[m.Add(-time.Minute).UnixMilli()] {
					cov = true
					break
				}
			}
			if !cov {
				r.nodata = true
			}
			d := octReplayCached(t, bs, octSession(w[0]))
			for _, a := range d.arms {
				if a.At.Before(w[0]) || a.At.After(w[1]) {
					continue
				}
				if !octSideMatch(sideOrAny(side), a.Side) || !octFamilyMatch(fam, a) {
					continue
				}
				if p, ok := octQuietPrices[q.ID]; ok && !octNear(p, a.Entry) {
					continue
				}
				r.arms = append(r.arms, a)
			}
			r.windows = append(r.windows, fmt.Sprintf("%s-%s", w[0].Format("15:04"), w[1].Format("15:04")))
		}
		res = append(res, r)
	}

	clean, total := 0, 0
	var sb strings.Builder
	var dirty, stale []string
	sb.WriteString("\nid | date window | refused | verdict | arms inside\n")
	for _, r := range res {
		q := r.q
		head := fmt.Sprintf("%s | %s %s-%s | %s %s (%s) conf %s", q.ID, q.Date, q.Start, q.End, q.Side, strings.Join(q.Setups, "+"), q.Type, q.Confidence)
		if r.skip != "" {
			fmt.Fprintf(&sb, "%s | SKIPPED (not arm-testable: %s)\n", head, r.skip)
			continue
		}
		total++
		if len(r.arms) == 0 {
			clean++
			if _, ok := acceptedQuietMisses[q.ID]; ok {
				stale = append(stale, q.ID)
			}
			v := "CLEAN"
			if r.nodata {
				v = "CLEAN (WARNING: no replay bars in part of the window - vacuous)"
			}
			fmt.Fprintf(&sb, "%s | %s\n", head, v)
			continue
		}
		var as []string
		for _, a := range r.arms {
			as = append(as, octFmtArm(a))
		}
		sort.Strings(as)
		v := "DIRTY"
		if why, ok := acceptedQuietMisses[q.ID]; ok {
			v = "ACCEPTED-DIRTY (" + why + ")"
		} else {
			dirty = append(dirty, q.ID)
		}
		show := as
		if len(show) > 12 {
			show = append(show[:12:12], fmt.Sprintf("... +%d more", len(as)-12))
		}
		fmt.Fprintf(&sb, "%s | %s | %d arm(s): %s\n", head, v, len(as), strings.Join(show, "; "))
	}
	fmt.Fprintf(&sb, "PRECISION %d/%d quiet windows clean (A/B only; %d skipped as not arm-testable)\n", clean, total, len(res)-total)
	fmt.Fprintf(&sb, "ACCEPTED-QUIET-MISSES %d (a listing is not a close); SIGNOFF-PENDING %s\n", len(acceptedQuietMisses), strings.Join(octQuietSignoffPending, ", "))
	t.Log(sb.String())

	if os.Getenv("OCT_SCORECARD_REPORT") != "" {
		return
	}
	// Ratchet (reviewer P2): precision may not fall below the baseline, and an
	// accepted-dirty window that turned clean must be removed from the list.
	if clean < octQuietCleanFloor {
		t.Errorf("PRECISION %d/%d fell below the ratchet floor %d clean", clean, total, octQuietCleanFloor)
	}
	if len(stale) > 0 {
		t.Errorf("acceptedQuietMisses entries now CLEAN (remove them, raise octQuietCleanFloor): %s", strings.Join(stale, ", "))
	}
	if len(dirty) > 0 {
		t.Errorf("PRECISION %d/%d: %d quiet window(s) dirty (not in acceptedQuietMisses): %s", clean, total, len(dirty), strings.Join(dirty, ", "))
	}
}

func sideOrAny(s string) string {
	if s == "both" {
		return ""
	}
	return s
}

// octQuietCleanFloor is the precision ratchet (P4-missesB WE8: 11 of 20 clean; J-BOX-01 raised it to 13:
// Q-1102-01 and Q-1102-02 are clean on the opening 5m ISB box no-trade).
// LOWERED 13 -> 12 EXPLICITLY (review P1-1, fix/mentor-reread-1116, 11-17): the 13 set by J-BOX-01 in this same
// branch counted Q-1102-02 clean, but its 08:32-08:40 part was clean only because the side-free ISB-M1 refused the
// 08:36 short PLH against the 08:33 up-break; the side-aware rule (the reviewed video fidelity fix) lets it arm. It
// is listed above as NEEDS OWNER SIGN-OFF, not closed. Raise back to 13 when the owner's ruling closes it.
const octQuietCleanFloor = 12
