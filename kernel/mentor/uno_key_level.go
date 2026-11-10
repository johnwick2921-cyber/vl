package mentor

import "vl/market"

// G2 D1.3-R21 (m466 P2, release #61) — the Uno Reverse of a KEY level.
//
// Course: "Kháng cự bị phá → sẽ thành hỗ trợ khi backtest / Hỗ trợ bị phá →
// sẽ thành kháng cự khi backtest" [D1.3 p1 @12:31–12:58]; the drawing: the
// level 29,104.25 bounces, breaks, and is backtested from below where it
// rejects. D5.2-R22 "cây nến chạm đầu tiên đóng ở trên level… invalid"
// [D5.2 p1 @19:44–20:25] is about the FIRST touch of a level only — it does
// not cover a level that has already HELD.
//
// Before #61 every wrong-way close of a new visit set ISBOnly[key] for the
// rest of the session day (touch.go TouchTick → handleTouchIntents), so the
// course's backtest reject could never arm a PHL/PLH there.
//
// The rule built here:
//   - a NON-moving, non-box KEY level (KindKeyLevel) that held today — at
//     least one reject visit counted in Visits — and has not flipped yet today
//     FLIPS when it breaks: no ISBOnly, counter level_broken_uno, the resting
//     order (if any) is still cancelled (the CancelArm intent is untouched);
//   - the touch is reset, so the NEXT candle reads the approach side again
//     (approachSide on its previous close): the level's role comes from the
//     side price now stands on, and the backtest reject arms a PHL/PLH through
//     every normal gate;
//   - a second wrong-way close after the flip — "missed" (D5.2-R24) [D5.2 p1 @20:31–21:43] — or a
//     wrong-way FIRST visit (the level never held) → ISBOnly exactly as before.
//
// Two shapes of "held, then broke" reach this file:
//   (a) a LATER visit's first touch closes through → TouchTick emits
//       LevelInvalid → handleTouchIntents asks unoKeyLevelBreak;
//   (b) the SAME visit: the reject candle held, and a later candle of that
//       visit closes through. The visit never ends (a support approached from
//       above is still "touched" by every candle below it), so no new first
//       touch could ever classify — unoSameVisitBreak catches it in the touch
//       loop before visitTick.
//
// Knob UnoKeyLevels "on" (default) | "off" (the pre-#61 behaviour, byte-identical).

// unoKeyLevelsOn reports whether the G2 flip is enabled ("" reads as on — the
// course default).
func unoKeyLevelsOn(cfg Config) bool {
	return cfg.UnoKeyLevels != UnoKeyLevelsOff
}

// unoEligible: only a stable KEY level flips — moving lines keep their own
// visit-scoped invalidity, box edges are outside the invalid rule entirely.
func unoEligible(lvl Level) bool {
	return lvl.Kind == KindKeyLevel && !isMovingLineKey(lvl.Key) && !isBoxEdge(lvl)
}

// keyLevelBreakRetestOn reports whether the October break-and-retest is on
// (levels audit LL-2; "" reads as on — the default).
func keyLevelBreakRetestOn(cfg Config) bool {
	return cfg.KeyLevelBreakRetest != KeyLevelBreakRetestOff
}

// unoKeyLevelBreak is called on a LevelInvalid intent (shape (a)). It returns
// true when the level FLIPPED (the caller must NOT set ISBOnly and drops the
// LevelInvalid intent); false = the pre-#61 ISB-only path applies.
//
// Two flips share the one-per-day UnoFlipped mark:
//   - Uno Reverse (knob uno_key_levels): the level HELD today, then broke;
//   - levels audit LL-2 (knob key_level_break_retest, default on): the level
//     NEVER held — its first touch closed through it. October trades that
//     break's first retest from the new side [OctD5.2-R20/R24/R25 @07:42,
//     13:35, 14:16], superseding the September "invalid, ISB-only" read of a
//     first touch [D5.2 p1 @19:44–20:25] (which knob off restores).
func (e *Evaluator) unoKeyLevelBreak(lvl Level) bool {
	if !unoEligible(lvl) {
		return false
	}
	if e.State.UnoFlipped[lvl.Key] {
		return false // a 2nd wrong-way close after the flip: missed → ISB-only (D5.2-R24) [D5.2 p1 @20:31–21:43]
	}
	if e.State.Visits[visitKeyFor(lvl, e.Cfg.CourseExits.EMA9VisitCap)] < 1 {
		// it never held today: the first touch closed through it.
		if !keyLevelBreakRetestOn(e.Cfg) {
			return false // September: invalid → ISB-only [D5.2 p1 @19:44–20:25]
		}
		e.levelFlip(lvl, "level_broken_retest",
			"key level %s @%.2f: the first touch closed through it — break and retest: the role flips, the first retest candle that touches from the other side and closes back on it is the reference (one flip per day) [OctD5.2-R20/R24/R25 @07:42, 13:35, 14:16]")
		return true
	}
	if !unoKeyLevelsOn(e.Cfg) {
		return false
	}
	e.unoFlip(lvl)
	return true
}

// unoSameVisitBreak is shape (b): the touch loop calls it BEFORE visitTick with
// the exec-frame candle tc. A held visit (TouchReject) whose later candle
// CLOSES THROUGH the level — below a support approached from above, above a
// resistance approached from below — flips the level and ends the visit. The
// breaking candle is not a new first touch (it returns true: skip visitTick
// this tick). A level that already flipped today is left to the ordinary
// machinery (its next visit's wrong-way first touch → ISB-only).
func (e *Evaluator) unoSameVisitBreak(tr *Touch, lvl Level, tc market.Kline) bool {
	if !unoKeyLevelsOn(e.Cfg) || !unoEligible(lvl) || e.State.UnoFlipped[lvl.Key] {
		return false
	}
	if tr.Outcome != TouchReject || tc.CloseTime <= tr.RefBar.CloseTime {
		return false
	}
	through := tr.ApproachedFrom == SideLong && tc.Close < lvl.Price ||
		tr.ApproachedFrom == SideShort && tc.Close > lvl.Price
	if !through {
		return false
	}
	e.unoFlip(lvl)
	*tr = Touch{}
	return true
}

// unoFlip records the flip: UnoFlipped (reset at the day roll with Visits), the
// level_broken_uno counter, a note naming the level, and a FRESH visit — the
// touch is reset and the once-per-visit marks cleared, so the backtest from the
// other side is a new first touch [D1.3 p1 @12:31–12:58].
func (e *Evaluator) unoFlip(lvl Level) {
	e.levelFlip(lvl, "level_broken_uno", "key level %s @%.2f held, then broke — Uno Reverse: the role flips, the backtest from the other side may trade (one flip per day) [D1.3 p1 @12:31–12:58]")
}

// levelFlip is the ONE flip body (Uno Reverse and the LL-2 break-and-retest):
// the one-per-day mark, the named counter, a note, and a FRESH visit.
func (e *Evaluator) levelFlip(lvl Level, counter, noteFmt string) {
	if e.State.UnoFlipped == nil {
		e.State.UnoFlipped = map[string]bool{}
	}
	e.State.UnoFlipped[lvl.Key] = true
	e.count(counter)
	e.note(noteFmt, lvl.Key, lvl.Price)
	if e.State.Touches != nil {
		e.State.Touches[lvl.Key] = Touch{}
	}
	delete(e.State.VisitCapRefused, lvl.Key)
	e.clearPHLRefusedVisit(lvl.Key)
}
