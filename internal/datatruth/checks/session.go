package checks

import (
	"fmt"
	"time"
)

// SessionExtremes compares a session's high/low computed from the 1m with the
// same span computed from an independent frame (5m): levels from prior sessions
// (PDH/PDL, overnight and key levels) read these extremes, and a hole in the 1m
// that hid the extreme - or a bad print that invented one - makes the level
// wrong for the whole next day. Blocks when they differ by more than tol.
// fromMs/toMs bound the session [from, to). Missing data in either frame is a
// block (the level cannot be verified).
func SessionExtremes(m1, m5 []Bar, fromMs, toMs int64, tol float64) (blocked bool, why string) {
	hi := func(bars []Bar) (h, l float64, n int) {
		for _, b := range bars {
			if b.T < fromMs || b.T >= toMs {
				continue
			}
			if n == 0 || b.H > h {
				h = b.H
			}
			if n == 0 || b.L < l {
				l = b.L
			}
			n++
		}
		return
	}
	h1, l1, n1 := hi(m1)
	h5, l5, n5 := hi(m5)
	if n1 == 0 || n5 == 0 {
		return true, fmt.Sprintf("session %s: %d x 1m and %d x 5m bars - the prior-session levels cannot be verified",
			time.UnixMilli(fromMs).UTC().Format("01-02 15:04Z"), n1, n5)
	}
	if abs(h1-h5) > tol || abs(l1-l5) > tol {
		return true, fmt.Sprintf("session %s: high %.2f (1m) vs %.2f (5m), low %.2f (1m) vs %.2f (5m) - prior-session levels disagree between frames",
			time.UnixMilli(fromMs).UTC().Format("01-02 15:04Z"), h1, h5, l1, l5)
	}
	return false, ""
}
