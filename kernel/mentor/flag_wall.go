package mentor

import "math"

// Ported from PR #416 (feat/flag-wall-target @50c8d0ddc, never merged) by
// release #25 fix/mentor-course-entries (CE-11, CTO 2026-10-14): knob
// risk_control.mentor_tuning.flag_wall_target, default ON since "course-defaults-30"
// (it shipped OFF in #25 — COMMON-25 case b: X9 is an extra, older than the
// course; the course's own trendline target is trendline_is_target [D3.3 p1
// @05:18–05:35], and the room rule [D5.3 p1 @09:16] it keeps is what this pass
// relaxes for a scalp inside a flag). c52 W9 (B21/B22) narrowed it to what the
// mentor actually said: the FLAG the entry sits in (the nearest live line each
// side, same frame first) and a POTENTIAL HIGH/LOW order only.
//
// FLAG-TARGET (owner "ok do it" 2026-10-05 22:3x): when a trade is taken INSIDE
// a live flag — both 1m trendlines exist and are unbroken, the entry strictly
// between them — the OPPOSITE flag wall becomes a target candidate and a room
// barrier: the target is the NEAREST of {the existing target, the wall}, and
// the wall can only bring it CLOSER. A wall that leaves < 1:1 refuses the entry
// (fail-closed; a scalp under 1:1 is not taken). Every other floor stays — the
// room rule and the wire R:R floor were already checked against the ORIGINAL
// target, and a closer wall only tightens the target, never loosens a floor.
//
// The mentor's words [X9 @ 05:47–06:12]: "Nếu mà anh chị vô cái lệnh Potential
// high low chỗ này, thì cái target anh chị về chỗ nào? … Có phải là target của
// anh chị là chỉ là trên cái đỉnh của cái flag không, là trên cạnh trên không?
// Thì nếu như anh chị nào vẫn muốn vô thì mình vẫn scalping được, không sai."
// The flag break itself stays untraded (D4.1 @ 16:06; X12 "NÓ KHÔNG PHẢI LÀ
// SETUP") — this feature only re-targets an entry ALREADY inside the flag.

// flagWalls holds every live (unbroken) trendline captured once per Tick, in
// build order. Which two of them form THE flag depends on the entry, so the
// choice is made per entry in wallFor (c52 W9 B21), not once at build time.
type flagWalls struct {
	lines []Trendline
}

// buildFlagWalls keeps the live lines of the current tick's TrendlinesBuild
// result, dropping any 5m-close-broken line. It used to keep only the FIRST
// support and the FIRST resistance — the OLDEST pair of the day — so on a
// multi-line day the "flag" was a stale pair far from price (c52 W9 B21).
func buildFlagWalls(ts []Trendline) flagWalls {
	var fw flagWalls
	for _, t := range ts {
		if t.Dead {
			continue
		}
		if t.Side != SideLong && t.Side != SideShort {
			continue
		}
		fw.lines = append(fw.lines, t)
	}
	return fw
}

// nearestWall returns, for one side, the line whose value at barTime is the
// NEAREST to price on the right side of it (support: below; resistance: above).
// "Joining the two trendlines gives one flag" and the target is the edge of THE
// flag the entry sits in [Cách vẽ trendline/part1 @00:05:04–00:05:09, @00:05:47–
// 00:06:06], so the nearest line is the flag edge. Lines of the traded frame
// (TFMin == 0) are preferred over persistent 1H/4H lines: an HTF line is a wall
// only when no traded-frame line of that side brackets the entry (c52 W9 B21).
// A still-forming line (< 3 touches) counts, as before: whether it is a flag
// edge is an open mentor question.
func (fw flagWalls) nearestWall(price float64, side Side, barTime int64) (wall float64, ok bool) {
	best := [2]float64{} // [0] = traded frame, [1] = HTF
	have := [2]bool{}
	for _, t := range fw.lines {
		if t.Side != side {
			continue
		}
		v := t.priceAt(barTime)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		g := 0
		if t.TFMin != 0 {
			g = 1
		}
		if side == SideLong { // support: below the entry, nearest = highest
			if v >= price || (have[g] && v <= best[g]) {
				continue
			}
		} else { // resistance: above the entry, nearest = lowest
			if v <= price || (have[g] && v >= best[g]) {
				continue
			}
		}
		best[g], have[g] = v, true
	}
	switch {
	case have[0]:
		return best[0], true
	case have[1]:
		return best[1], true
	}
	return 0, false
}

// wallFor returns the flag wall for the entry side and ok=true when a live flag
// exists at barTime and `price` sits STRICTLY between the two projected walls
// (the nearest live support below and the nearest live resistance above, see
// nearestWall). A long's wall is the resistance (upper edge); a short's is the
// support (lower edge).
func (fw flagWalls) wallFor(price float64, side Side, barTime int64) (wall float64, ok bool) {
	sup, okS := fw.nearestWall(price, SideLong, barTime)
	res, okR := fw.nearestWall(price, SideShort, barTime)
	if !okS || !okR {
		return 0, false
	}
	if side == SideLong {
		return res, true
	}
	return sup, true
}

// flagWallSetup reports whether the intent is a POTENTIAL HIGH/LOW order — the
// only setup the mentor's flag-target sentence is about ("Nếu mà anh chị vô cái
// lệnh Potential high low chỗ này, thì cái target anh chị về chỗ nào?"
// [Cách vẽ trendline/part1 @00:05:47–00:06:12]; the flag break itself is not
// traded, @05:36). ISB, box, ORB and SWING4H entries are NOT cut to a flag wall
// (c52 W9 B22; owner blanket go 2026-10-26, course read literally). A reverse
// ISB the mentor reads as PHL/PLH carries Setup "PHL"/"PLH" (reverseISBRetag)
// and so stays in.
func flagWallSetup(in Intent) bool {
	return in.Setup == "PHL" || in.Setup == "PLH"
}

// applyFlagWallTarget is the FLAG-TARGET pass: for each entry intent, when the
// switch is ON and the entry is inside a live flag, the opposite wall becomes
// the target if it is on the target side and CLOSER than the existing target.
// A wall that leaves < 1:1 drops the intent and counts flag_wall_room; a wall
// that becomes the target counts flag_wall_target_used. Only PHL/PLH entries
// are touched (flagWallSetup). Byte-identical when FlagWallTarget is OFF.
func applyFlagWallTarget(out []Intent, fw flagWalls, barTime int64, cfg Config, refuse func(string)) []Intent {
	if !cfg.FlagWallTarget {
		return out
	}
	kept := out[:0]
	for _, in := range out {
		if (in.Action != PlaceStopEntry && in.Action != PlaceStopLimitEntry) || !flagWallSetup(in) {
			kept = append(kept, in)
			continue
		}
		wall, ok := fw.wallFor(in.Price, in.Side, barTime)
		if !ok {
			kept = append(kept, in)
			continue
		}
		onSide := in.Side == SideLong && wall > in.Price || in.Side == SideShort && wall < in.Price
		closer := in.Side == SideLong && wall < in.Target || in.Side == SideShort && wall > in.Target
		if !onSide || !closer || in.Target == 0 {
			kept = append(kept, in)
			continue
		}
		// The wall can only bring the target closer; a closer wall that still
		// leaves < 1:1 refuses (the scalp under 1:1 is not taken).
		if !targetFloorOK(in.Price, in.Stop, wall) {
			refuse("flag_wall_room")
			continue
		}
		in.Target = wall
		in.TargetPts = abs(in.Target - in.Price)
		refuse("flag_wall_target_used")
		kept = append(kept, in)
	}
	return kept
}
