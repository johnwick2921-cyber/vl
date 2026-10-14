package ninjatrader

import "math"

// ── GAP-BACKFILL (2026-10-14) — a replay that overlaps traded minutes is
// judged ON THOSE MINUTES ──────────────────────────────────────────────────
//
// The scale check (detectScaleMismatch) compares the replay's LAST historical
// bar with the FIRST live bar after it, and only when the two are adjacent
// (101 D1' rule 2). That fits a boot replay, which lands in an empty ring. It
// can never fit a replay asked for MID-SESSION: by the time it lands, every
// minute since the feed came back already traded live, mergeSeedKeepingLive
// keeps those live bars, and the newest historical bar left in the ring is the
// last minute of the hole — minutes before the next live bar. The check SKIPS
// ("not adjacent"), stays armed, never resolves, and the replay-hold keeps the
// whole replay out of the store for the life of the process. Live proof, 10-14
// 07:50:20 CT: "🕳 scale check SKIPPED for MNQ 15m … replay rows held from the
// store". The gap re-request would refill the ring and never the store.
//
// The better evidence is already in hand at seed time: the replay carries its
// own bar for every minute the ring holds live. Same minute, same contract,
// two feeds — exactly the comparison the 09-10 finding was (live 29358.25 vs
// replay 29068.25 for the 22:37 minute). When EVERY such pair agrees within
// ScaleMismatchPct of price, the seed is judged on-scale and SeedVerdict says
// so; the hold then releases the replay as 'historical' into the minutes live
// never wrote (the upsert rule still keeps every live row).
//
// It only ever ADDS a verdict. A disagreeing pair leaves the key armed exactly
// as before (the first-live-bar check stands), nothing is dropped here, and a
// seed with no live overlap — every boot replay — is untouched.

// overlapScaleVerdict compares, minute by minute, the incoming replay with the
// LIVE bars the ring already holds at the same open time. pairs = how many
// same-minute pairs had a usable close on both sides; agree = every one of
// them is within ScaleMismatchPct of the live close. Both inputs ascending by T.
func overlapScaleVerdict(existing, incoming []Bar) (pairs int, agree bool) {
	agree = true
	i, j := 0, 0
	for i < len(existing) && j < len(incoming) {
		switch {
		case existing[i].T < incoming[j].T:
			i++
		case existing[i].T > incoming[j].T:
			j++
		default:
			ex, in := existing[i], incoming[j]
			i++
			j++
			if ex.Source != BarSourceLive || ex.C == 0 || in.C == 0 {
				continue
			}
			pairs++
			if math.Abs(in.C-ex.C) > ScaleMismatchPct*math.Abs(ex.C) {
				agree = false
			}
		}
	}
	if pairs == 0 {
		agree = false
	}
	return pairs, agree
}

// OverlapVerdictCounts is READ by the health surface (A11): seeds judged
// on-scale by same-minute overlap, and seeds whose overlap DISAGREED (left
// armed for the first-live-bar check, nothing dropped).
func (c *BarCache) OverlapVerdictCounts() (agreed, disagreed int64) {
	if c == nil {
		return 0, 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.overlapAgreed, c.overlapDisagreed
}
