package mentor

import (
	"testing"

	"vl/market"
)

// Item 19 (D4.4-05): the 1h line resets. Record MovedAt (bucket of the last
// move) on TriggerLine; the 1h counts only when it moved at or after the 4h
// line's last move — an EARLIER 1h trigger is ignored and reads "silent", so
// case 2 ("1h ko có gì hết → follow the 4h") can happen live instead of a stale
// opposite 1h reading as case 3 [D4.4 p1 @18:36; p2 @03:10].

const (
	t4h = int64(12) * 3600_000 // the 4h line's last move
	t1h = int64(10) * 3600_000 // the 1h line's last move — earlier than the 4h
)

// TestOneHEarlierOppositeIsSilentCase2 — the headline: a 1h that fired BEFORE
// the 4h and points the other way is "silent", so the verdict is case 2 (follow
// the 4h), not case 3 (sit out).
func TestOneHEarlierOppositeIsSilentCase2(t *testing.T) {
	h := HTF{
		FourH: TriggerLine{Dir: SideLong, Price: 100, MovedAt: t4h},
		OneH:  TriggerLine{Dir: SideShort, Price: 90, MovedAt: t1h},
	}
	if h.OneH.MovedAt >= h.FourH.MovedAt {
		t.Fatalf("fixture: the 1h must have fired before the 4h (%d >= %d)", h.OneH.MovedAt, h.FourH.MovedAt)
	}
	if ok, side, _ := HTFVerdict(h); !ok || side != SideLong {
		t.Fatalf("an earlier opposite 1h must be silent → case 2 follow the 4h long: ok=%v side=%q", ok, side)
	}
	if HTFConflict(h) {
		t.Fatal("an earlier opposite 1h is not a conflict (it is silent)")
	}
}

// TestOneHLaterOppositeIsCase3 — a 1h that fired AT OR AFTER the 4h and points
// the other way is still case 3 (sit out) [D4.4 p1 @16:00].
func TestOneHLaterOppositeIsCase3(t *testing.T) {
	h := HTF{
		FourH: TriggerLine{Dir: SideLong, Price: 100, MovedAt: t4h},
		OneH:  TriggerLine{Dir: SideShort, Price: 90, MovedAt: t4h + 3600_000},
	}
	if ok, _, reason := HTFVerdict(h); ok || reason == "" {
		t.Fatalf("a 1h that fired after the 4h and is opposite must sit out: ok=%v reason=%q", ok, reason)
	}
	if !HTFConflict(h) {
		t.Fatal("a 1h that fired after the 4h and is opposite must conflict")
	}
}

// TestHTFAgreesSilentOneHDoesNotCount — a silent 1h (fired before the 4h) never
// counts toward the 20-contract tier, even when it points the same side [§5.4].
func TestHTFAgreesSilentOneHDoesNotCount(t *testing.T) {
	h := HTF{
		FourH: TriggerLine{Dir: SideLong, Price: 100, MovedAt: t4h},
		OneH:  TriggerLine{Dir: SideLong, Price: 95, MovedAt: t1h},
	}
	if HTFAgrees(h, SideLong) {
		t.Fatal("a silent 1h (fired before the 4h) must not count as agreement")
	}
	// after the 1h re-fires at/after the 4h move, it counts.
	h.OneH.MovedAt = t4h
	if !HTFAgrees(h, SideLong) {
		t.Fatal("a 1h that re-fired at/after the 4h must count as agreement")
	}
}

// TestMovedAtRecordedOnBreakAndNotOnRepeat — the mechanism: a break stamps
// MovedAt to the firing bucket; a same-direction repeat does not touch it.
func TestMovedAtRecordedOnBreakAndNotOnRepeat(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	bars := []market.Kline{
		{OpenTime: t1h, CloseTime: t1h + 3600_000 - 1, High: 100, Low: 96, Close: 97},
		{OpenTime: t1h + 3600_000, CloseTime: t1h + 2*3600_000 - 1, High: 103, Low: 97, Close: 99}, // breaks high → long
	}
	got := TriggerTick(TriggerLine{}, bars, 60, cfg)
	if got.Dir != SideLong || got.MovedAt != bars[1].OpenTime {
		t.Fatalf("the break must stamp MovedAt = the firing bucket: %+v", got)
	}
	// a same-direction repeat does not move the line or its MovedAt.
	bars = append(bars, market.Kline{OpenTime: t1h + 2*3600_000, CloseTime: t1h + 3*3600_000 - 1, High: 106, Low: 102, Close: 104})
	got2 := TriggerTick(got, bars, 60, cfg)
	if got2.MovedAt != got.MovedAt {
		t.Fatalf("a same-direction repeat must not update MovedAt: %+v → %+v", got, got2)
	}
}

// TestHTFAdvanceOneHEarlierOppositeIsSilentCase2 — production call site: feed
// the real 4h and 1h bar slices; the 1h fires (short) on an hour bucket BEFORE
// the 4h fires (long), so the verdict is case 2, not case 3.
func TestHTFAdvanceOneHEarlierOppositeIsSilentCase2(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	bars4h := []market.Kline{
		{OpenTime: 8 * 3600_000, CloseTime: 12*3600_000 - 1, High: 100, Low: 96, Close: 97},
		{OpenTime: t4h, CloseTime: 16*3600_000 - 1, High: 103, Low: 97, Close: 99}, // 4h long @ t4h
	}
	bars1h := []market.Kline{
		{OpenTime: 9 * 3600_000, CloseTime: 10*3600_000 - 1, High: 100, Low: 96, Close: 97},
		{OpenTime: t1h, CloseTime: 11*3600_000 - 1, High: 99, Low: 94, Close: 95}, // 1h short @ t1h (earlier)
	}
	h := HTFAdvance(HTF{}, bars4h, bars1h, cfg)
	if h.FourH.Dir != SideLong || h.OneH.Dir != SideShort {
		t.Fatalf("fixture lines wrong: 4h=%q 1h=%q", h.FourH.Dir, h.OneH.Dir)
	}
	if h.OneH.MovedAt >= h.FourH.MovedAt {
		t.Fatalf("fixture: the 1h must have fired before the 4h (%d >= %d)", h.OneH.MovedAt, h.FourH.MovedAt)
	}
	if ok, side, _ := HTFVerdict(h); !ok || side != SideLong {
		t.Fatalf("HTFAdvance: an earlier opposite 1h must be silent → case 2 follow the 4h long: ok=%v side=%q", ok, side)
	}
	if HTFConflict(h) {
		t.Fatal("HTFAdvance: an earlier opposite 1h is not a conflict")
	}
}
