package mentor

import (
	"strings"

	"vl/market"
)

// Box trade evaluation — BOX REUSE (R1) and CONFLUENCE (R2), CTO rulings
// 2026-10-03 verified in the sources [A].

// BoxReturn is one post-formation return visit [D3.2 p2 @ 06:25: "minh se
// dung box nay hoai ne" — we use this box again and again]. A visit =
// price was outside the box on the approach side, then a candle touches an
// edge; RefBar is that first touching candle — the visit's reference
// candle. EVERY return gets its own reference candle, the same reject rule
// (close outside on the approach side) and the same filters. The FIRST
// return is the third touch overall, counting the two extremes that built
// the box [D3.2 p1 @ 04:29].
type BoxReturn struct {
	N      int // 1-based return number (1 = the third touch)
	RefBar int // bar index of the visit's reference candle
}

// BoxReturnBars lists every post-formation return visit, one entry per
// visit (not per candle). The TOUCH is checked FIRST: a candle whose wick
// touches an edge while price was outside on the approach side is the
// visit's reference candle — even when that same candle closes back outside
// on the approach side (the REJECT candle, fact 4: that is exactly the
// trade reference). Consecutive touching candles are the same visit; a
// candle closing outside the box (approach side) opens the next visit.
//
// B10 T1 FOLD (CTO 21:16Z): ONE seeding rule for both walks — the full walk
// delegates to the incremental form, so both seed the outside-spell state
// from the FormedAt candle's close and a return on the very next bar is
// never lost.
func BoxReturnBars(bars []market.Kline, b Box, formedAt int, cfg BoxCfg) []BoxReturn {
	return BoxReturnBarsFrom(bars, b, formedAt+1, cfg)
}

// BoxReturnBarsFrom is the INCREMENTAL call used by Tick: the same walk as
// BoxReturnBars, starting at bar start with the outside-spell state already
// known from bars[start-1] (the approach side). Running the full walk from
// FormedAt every tick is O(n^2) on a long tape (the 30-day replay timed out
// at 437s); the incremental form is O(new bars). Under B10 T1 the box is
// BORN when the extreme's confirming bar closes (FormedAt = max(nearest,
// extreme+1)) and the walk starts after it — so the FIRST walk seeds the
// spell state from that formation candle's close (start-1 == FormedAt), and
// a return visit on the very next bar is not lost.
func BoxReturnBarsFrom(bars []market.Kline, b Box, start int, cfg BoxCfg) []BoxReturn {
	var out []BoxReturn
	if start >= len(bars) {
		return out
	}
	outside := false
	if start-1 >= b.FormedAt && start-1 < len(bars) {
		c := bars[start-1]
		switch b.Kind {
		case FTGH:
			outside = c.Close < b.Bottom
		case FTGL:
			outside = c.Close > b.Top
		}
	}
	for i := start; i < len(bars); i++ {
		c := bars[i]
		if c.CloseTime == 0 {
			continue
		}
		if touchesEdge(b, c, cfg) && outside {
			out = append(out, BoxReturn{N: len(out) + 1, RefBar: i})
		}
		switch b.Kind {
		case FTGH:
			outside = c.Close < b.Bottom
		case FTGL:
			outside = c.Close > b.Top
		}
	}
	return out
}

// BoxReturnReject classifies a return's reference candle [D3.2 p1
// @ 21:04–21:33]: close OUTSIDE the box on the approach side → the reject
// (the trade reference; place the stop order); close INSIDE → cancel.
func BoxReturnReject(b Box, ref market.Kline) bool {
	switch b.Kind {
	case FTGH:
		return ref.Close < b.Bottom
	case FTGL:
		return ref.Close > b.Top
	}
	return false
}

// pingPongVerdict — PING PONG (CTO 13:24:53Z): when the entry sits between
// an FTGL floor and an FTGH ceiling, the edge trade is allowed only when the
// gap between the floor TOP and the ceiling BOTTOM is >= 50 pts
// ("danh ping pong — KHONG DUOC DANH GIUA", D3.2 p2 @07:50-09:14; D4.2 p2
// @05:17). Below 50 pts it is refused: ping_pong_range_too_small. With fewer
// than two boxes around the price there is no ping-pong context.
func pingPongVerdict(boxes []Box, price float64) (ok bool, reason string) {
	var floor, ceil *Box
	for i := range boxes {
		switch boxes[i].Kind {
		case FTGL:
			if boxes[i].Top < price && (floor == nil || boxes[i].Top > floor.Top) {
				floor = &boxes[i]
			}
		case FTGH:
			if boxes[i].Bottom > price && (ceil == nil || boxes[i].Bottom < ceil.Bottom) {
				ceil = &boxes[i]
			}
		}
	}
	if floor == nil || ceil == nil {
		return true, ""
	}
	if ceil.Bottom-floor.Top < 50 {
		return false, "ping_pong_range_too_small"
	}
	return true, ""
}

// boxEntryIntent is the CALL SITE of the box trade: one return visit's
// reference candle becomes a stop order [D3.4 p3 @ 07:02 — "stop order away
// from the box, with the REJECTING candle as the reference"] when the
// reject test passes; a close inside cancels. Gates, in order: 5m trigger
// verdict (between two lines / wrong side), the mid-range ban, never-inside
// the box, the stop ceiling, the §6 target ladder and the room rule. The R2
// confluence flag [00-METHOD Risk-reward, D3.4 p3 @ 07:38] rides the intent.
func boxEntryIntent(ref market.Kline, b Box, boxes []Box, levels []Level, trig TriggerLine, cfg Config) []Intent {
	if !BoxReturnReject(b, ref) {
		return nil // close inside the box = cancel
	}
	var side Side
	var price, stop float64
	if b.Kind == FTGL {
		side, price, stop = SideLong, ref.High, ref.Low
	} else {
		side, price, stop = SideShort, ref.Low, ref.High
	}
	if cfg.LocTriggerFilter {
		if ok, ts, _ := TriggerVerdict(trig, price); !ok || ts != "" && ts != side {
			return nil
		}
	}
	// PING PONG (CTO 13:24:53Z): between two boxes the edge trade is legal
	// only when the gap is >= 50 pts — below that, refused (the MIDDLE is for
	// ping pong, not the edges, and a narrow range cannot support the bounce).
	if ok, _ := pingPongVerdict(boxes, price); !ok {
		return nil
	}
	if InsideAnyBox(boxes, price) {
		return nil
	}
	risk := abs(price - stop)
	if risk > cfg.StopCeilingPts {
		return nil
	}
	target := nextLevelBeyond(levels, price, side)
	if target == 0 {
		return nil // no level beyond → no setup [D4.1 p1 @ 01:45]
	}
	if abs(target-price) < cfg.RoomMultiple*risk {
		return nil
	}
	fl := ConfluenceVerdict(b, side, trig)
	// G2 place (CTO R-b / 13:20:08Z): a box is ONE place — the key WITHOUT the
	// ":top"/":bottom" suffix, the anchor is the box MIDPOINT (the replay's).
	base := strings.TrimSuffix(strings.TrimSuffix(b.Key, ":top"), ":bottom")
	return []Intent{{
		AnchorKey:  base,
		Anchor:     (b.Top + b.Bottom) / 2,
		Action:     PlaceStopEntry,
		Setup:      "BOX",
		Side:       side,
		Price:      price,
		Stop:       stop,
		Target:     target,
		Confluence: fl.On,
		Reason:     "box edge return: reject close outside → stop order with the rejecting candle as the reference [D3.2 p1 @ 21:04–21:33; D3.4 p3 @ 07:02]",
	}}
}

// ConfluenceFlag is the R2 output for DS-102's exit-C / size-10 branch.
// On=false means normal sizing; the size-20 escalation (4h AND 1h agree AND
// room >= 30 pts) is DS-102's tier rule, not computed here.
type ConfluenceFlag struct {
	On   bool
	Side Side // the confluence side ("" when off)
}

// ConfluenceVerdict evaluates the R2 confluence test [00-METHOD Risk-reward,
// D3.4 p3 @ 07:38] for one box trade setup:
//
//	LONG  = an FTGL box (support) AND a key level inside the box or within
//	        2 pts of its edge AND the 5m BUY trigger agrees.
//	SHORT = FTGH + key level + 5m SELL trigger.
//
// side is the trade side; trig is the 5m trigger line. Fail-closed: no
// trigger line (empty direction) can never agree, so confluence stays off.
// ConfluenceVerdict is B3 (10-03 ruling, D3.4 p3 @07:38–08:22): confluence
// = an FTGL/FTGH entry + the 5m trigger agrees — NO key-level condition. LONG
// = FTGL (support); SHORT = FTGH. Feeds DS-102's exit-C / size-10.
func ConfluenceVerdict(b Box, side Side, trig TriggerLine) ConfluenceFlag {
	if side != SideLong && side != SideShort {
		return ConfluenceFlag{}
	}
	entry := b.Top
	if side == SideShort {
		entry = b.Bottom
	}
	if ok, trigSide, _ := TriggerVerdict(trig, entry); !ok || trigSide == "" || trigSide != side {
		return ConfluenceFlag{}
	}
	switch side {
	case SideLong:
		if b.Kind != FTGL {
			return ConfluenceFlag{}
		}
	case SideShort:
		if b.Kind != FTGH {
			return ConfluenceFlag{}
		}
	}
	return ConfluenceFlag{On: true, Side: side}
}
