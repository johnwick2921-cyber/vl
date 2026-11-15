package mentor

import (
	"fmt"

	"vl/market"
)

// ISB at a level it just broke — oct-exact D1 (knob mentor_isb_just_broken_level,
// default ON; OFF reverts to today: nothing here runs).
//
// October D3.3: price broke the level, reversed back through it, and an inside
// bar formed right there is "hoan toan khong duoc ... khi ma no break level xong
// no dong nguoc lai, dung bao gio bat kieu nay" [OctD3.3-R47 @38:10, the 10-30
// ~09:04 FTGH break-and-reverse ISB, closes 28,802.75]; "do NOT take the inside-bar
// short when the inside bar sits too close to the level ... wait: price drops away,
// comes back UP to retest the level from below, and only then sell down; a bare
// close below the level is not the entry" [R2 @00:46, R3 @01:00]. The way back in
// is the retest [R48 @39:20]. The mentor gives NO points distance, so none is
// invented here.
//
// The rule, on the two candles the ISB is read on: the MOTHER candle (prev) just
// crossed the box with its BODY (open at or beyond the near edge, close through
// it), and the inside bar (cur) has not retested the box since. A box is a zone:
// the edge facing the ISB is the one price must get back to (a short ISB under the
// box retests its BOTTOM edge, a long ISB above it its TOP edge). A retest (the
// inside bar's own wick reaching the edge) lifts it, and the check is stateless (it
// reads only prev and cur), so the next candle and the session roll reset it too.
//
// SCOPE: the FTGH/FTGL box edges, the level R47 names. NOT built, and why:
//   - trendlines (R1-R3): the mentor's own D2.2 E9 trade (a short ISB at 28,709.5
//     on 10-30 08:34, marked) is an ISB right after a candle closed through the
//     28,743.9 ascending trendline, 29 pts from it. The mentor's own video trade
//     wins, and the video gives no distance that separates E9 from R1-R3.
//   - key levels / old extremes / gap edges: no October example, and reading them
//     refuses the 11-02 08:43 long ISB at the 28,701 key level (D4.3 #7) for a
//     reason other than the 5m-box rule the CTO ruled for it.
func justBrokenLevel(side Side, prev, cur market.Kline, boxes []Box) string {
	if side != SideShort && side != SideLong {
		return ""
	}
	for _, b := range boxes {
		p := b.Bottom
		if side == SideLong {
			p = b.Top
		}
		if p <= 0 {
			continue
		}
		var crossed, retested bool
		if side == SideShort {
			crossed = prev.Open >= p && prev.Close < p
			retested = cur.High >= p
		} else {
			crossed = prev.Open <= p && prev.Close > p
			retested = cur.Low <= p
		}
		if crossed && !retested {
			return fmt.Sprintf("%s box edge %.2f", b.Kind, p)
		}
	}
	return ""
}

// isbJustBrokenRefusal is the evaluator hook: "" = allowed (knob OFF included).
func (e *Evaluator) isbJustBrokenRefusal(side Side, prev, cur market.Kline, boxes []Box) string {
	if !e.Cfg.ISBJustBrokenLevel {
		return ""
	}
	what := justBrokenLevel(side, prev, cur, boxes)
	if what == "" {
		return ""
	}
	e.note("ISB %s refused: it sits at a box that just broke (%s) with no retest yet — wait for the retest [OctD3.3-R47 @38:10, R2/R3 @00:46-01:00]", side, what)
	return "isb_just_broken_level"
}
