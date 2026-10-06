package mentor

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

// flagWalls is the two live (unbroken) trendlines captured once per Tick.
type flagWalls struct {
	support        Trendline // SideLong
	resistance     Trendline // SideShort
	haveSupport    bool
	haveResistance bool
}

// buildFlagWalls keeps one support and one resistance line from the current
// tick's TrendlinesBuild result, dropping any 5m-close-broken line.
func buildFlagWalls(ts []Trendline) flagWalls {
	var fw flagWalls
	for _, t := range ts {
		if t.Dead {
			continue
		}
		switch t.Side {
		case SideLong:
			if !fw.haveSupport {
				fw.support, fw.haveSupport = t, true
			}
		case SideShort:
			if !fw.haveResistance {
				fw.resistance, fw.haveResistance = t, true
			}
		}
	}
	return fw
}

// wallFor returns the flag wall for the entry side and ok=true when a live flag
// exists at barTime and `price` sits STRICTLY between the two projected walls.
// A long's wall is the resistance (upper edge); a short's is the support
// (lower edge). Crossed lines (support >= resistance) are not a flag.
func (fw flagWalls) wallFor(price float64, side Side, barTime int64) (wall float64, ok bool) {
	if !fw.haveSupport || !fw.haveResistance {
		return 0, false
	}
	sup := fw.support.priceAt(barTime)
	res := fw.resistance.priceAt(barTime)
	if sup >= res {
		return 0, false
	}
	if price <= sup || price >= res {
		return 0, false
	}
	if side == SideLong {
		return res, true
	}
	return sup, true
}

// applyFlagWallTarget is the FLAG-TARGET pass: for each entry intent, when the
// switch is ON and the entry is inside a live flag, the opposite wall becomes
// the target if it is on the target side and CLOSER than the existing target.
// A wall that leaves < 1:1 drops the intent and counts flag_wall_room; a wall
// that becomes the target counts flag_wall_target_used. Byte-identical when
// FlagWallTarget is OFF.
func applyFlagWallTarget(out []Intent, fw flagWalls, barTime int64, cfg Config, refuse func(string)) []Intent {
	if !cfg.FlagWallTarget {
		return out
	}
	kept := out[:0]
	for _, in := range out {
		if in.Action != PlaceStopEntry && in.Action != PlaceStopLimitEntry {
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
