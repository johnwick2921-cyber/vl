package main

import (
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
)

// J-BOX-01 (P0, full re-read 11-16): the opening 5m ISB box is a NO-TRADE box.
//
// Mon 02 Nov 2026 (MNQ 12-26, the D4.3 / D4.1 morning): the 08:30 RTH 5m candle is
// the RED mother (O 28,721.75 H 28,739.25 L 28,672.75 C 28,709.50), the 08:35 5m
// candle the inside bar (H 28,727.00 L 28,639.75); 08:40-09:05 close inside and
// merge into it head to toe (OctD4.3-R59 @58:00) -> 28,639.75-28,748.75. The first
// 1m close outside is the 09:12 candle (C 28,749.00); the 09:10 5m candle closes
// outside (28,769.50) and deletes the box at 09:15.
//
//   - Oct D4.3 @58:00-58:33: "nguyen mot cai box nay buoi sang ngay hom nay minh
//     khong duoc tray" (this whole box, this morning, we may not trade).
//   - @59:50-1:00:29: a SHORT in the mother's direction inside it? "Khong ... Chung
//     nao thoat khoi INSABA moi duoc trade" (no; only once price leaves the ISB).
//   - @1:04:54-1:05:29: a 1m close outside gives permission; @1:05:33-1:06:13 the
//     box is deleted on a 5m close outside.
//   - Oct D4.1 @11:00-11:41, the same morning: "em khong dung toi ... Khi nao
//     thoat ra di em trade".
//
// Before J-BOX-01 the bot armed the 08:58 short ISB 28,672.00 inside the box
// (ISBBoxAllows passes a 1m ISB in the box's own direction).
func TestJBOX01_1102OpeningBoxNoTradeUntilA1mCloseOutside(t *testing.T) {
	bs := loadFixtureFile(t, "mnq_12-26_1m_20260907-20261103_full.csv.gz")
	end := time.Date(2026, 11, 2, 9, 20, 0, 0, kernel.CTLocation())
	arms, ref, _ := jbReplay(t, bs, "2026-11-02", end, nil)

	const boxLow, boxHigh = 28639.75, 28748.75
	from := time.Date(2026, 11, 2, 8, 40, 0, 0, kernel.CTLocation())  // the 08:35 inside bar closes: box drawn
	until := time.Date(2026, 11, 2, 9, 12, 0, 0, kernel.CTLocation()) // last tick before the 09:12 1m close outside
	for _, a := range arms {
		if a.Setup == "SWING4H" || a.At.Before(from) || a.At.After(until) {
			continue
		}
		if a.Entry >= boxLow && a.Entry <= boxHigh {
			t.Errorf("default: %s %s %s %.2f armed INSIDE the opening 5m ISB box %.2f-%.2f before any 1m close outside [Oct D4.3 @58:00-58:33, @59:50-1:00:29]",
				a.At.Format("15:04"), a.Side, a.Setup, a.Entry, boxLow, boxHigh)
		}
	}
	if jbArmAt(arms, "08:58", "short", "ISB", 28672.00) {
		t.Errorf("default: the 08:58 short ISB 28,672.00 (same side as the red mother) still arms inside the opening box")
	}
	if ref["08:58 opening_isb_box_no_trade"] == 0 {
		t.Errorf("default: no 08:58 opening_isb_box_no_trade refusal; 08:58 refusals: %v", jbAt(ref, "08:58"))
	}
	// Once a 1m candle has closed outside (09:12), trading is allowed again
	// ("thoat ra khoi box roi thi minh duoc trade", @1:05:29).
	if !jbArmAt(arms, "09:16", "long", "ISB", 28780.25) {
		t.Errorf("default: the 09:16 long ISB 28,780.25 (after the 09:12 1m close outside the box) did not arm; refusals then: %v", jbAt(ref, "09:16"))
	}
	for k := range ref {
		if k[:5] > "09:12" && len(k) > 6 && k[6:] == "opening_isb_box_no_trade" {
			t.Errorf("default: %s - the opening box still refuses after the 09:12 1m close outside it", k)
		}
	}
}

// Revert (mentor_opening_isb_box_no_trade=false): the plain 5m box gate lets the
// same-direction 1m ISB through again (R70 @1:08:24) - the 08:58 short ISB
// 28,672.00 arms exactly as before J-BOX-01, and nothing is refused by name.
func TestJBOX01_1102KnobOffRevertsTo0858ShortISB(t *testing.T) {
	bs := loadFixtureFile(t, "mnq_12-26_1m_20260907-20261103_full.csv.gz")
	end := time.Date(2026, 11, 2, 9, 20, 0, 0, kernel.CTLocation())
	off := false
	sc := &store.StrategyConfig{}
	sc.RiskControl.MentorTuning = &store.MentorTuning{MentorOpeningISBBoxNoTrade: &off}
	arms, ref, _ := jbReplay(t, bs, "2026-11-02", end, sc)
	if !jbArmAt(arms, "08:58", "short", "ISB", 28672.00) {
		t.Errorf("revert (knob false): the 08:58 short ISB 28,672.00 did not come back; refusals then: %v", jbAt(ref, "08:58"))
	}
	for k, n := range ref {
		if len(k) > 6 && k[6:] == "opening_isb_box_no_trade" {
			t.Errorf("revert (knob false): %s x%d still refused by the opening box", k, n)
		}
	}
}
