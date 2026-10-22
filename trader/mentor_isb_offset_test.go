package trader

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── P2-1 (rel22 mentor audit) — ONE definition of the ISB entry offset ──────
//
// The course: the ISB order sits 1.5 points past the candle's extreme — "1 đến
// 2 điểm — 1 điểm rưỡi đi. Đừng 1 điểm" [D2.1 p1 @ 05:36; 00-METHOD §2.1
// "Buffer: 1.5 points"]. 1.5 pts = 6 MNQ ticks.
//
// The kernel authors extreme ± ISBBufferPts (1.5) (kernel/mentor/isb.go
// ISBOrders) and the executor's decideStopEntry then added the planner's E7
// stop-entry offset (STOP_ENTRY_OFFSET_TICKS = 2 → 0.5 pt) on top, so every
// regular ISB reached NT8 2.0 pts past the extreme: row 357 (10-13 22:18 CT)
// authored 30890.25 = I1 high 30888.75 + 1.5 and went out at trigger
// 30890.75; fills 252, 254, 261, 316, 347 each landed 0.5 past the ledger
// entry. The reverse ISB had been patched to pre-subtract that 0.5 — two
// definitions of one number.
//
// Now the kernel's authored price IS the wire trigger for every mentor row;
// the E7 offset stays on planner (non-mentor) stop entries only, by spec.
//
// This pin runs the REAL kernel order builders on a candle, dispatches the
// intent through mentorDispatchIntent, places it with the armed pass, and
// reads the trigger off the raw signal frame on the wire.
//
// MUTANT: give mentor rows the E7 offset again (stopEntryWireOffset returns
// the offset for every origin) → the regular ISB lands at +2.0 and this test
// goes RED.
func TestMentorISBEntryIsOnePointFivePastTheExtremeOnTheWire(t *testing.T) {
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	cfg.ISBReverseEMA9Enabled = true
	if cfg.ISBBufferPts != 1.5 {
		t.Fatalf("fixture: the course buffer is 1.5 pts [D2.1 p1 @ 05:36], DefaultConfig has %.2f", cfg.ISBBufferPts)
	}
	mother := market.Kline{Open: 29590, Close: 29604, High: 29605, Low: 29586} // green → long
	i1 := market.Kline{Open: 29598, Close: 29596, High: 29601, Low: 29592, CloseTime: rthInstant().UnixMilli() - 1}

	_, regular, ok, why := mentor.ISBStopLimitOrder(mother, i1, cfg)
	if !ok {
		t.Fatalf("fixture: the regular ISB must emit: %s", why)
	}
	redMother := market.Kline{Open: 29604, Close: 29590, High: 29605, Low: 29586} // red ISB in an uptrend → reverse long
	reverse, ok, why := mentor.ReverseISBAtEMA9(redMother, i1, 29595, mentor.SideLong, cfg)
	if !ok {
		t.Fatalf("fixture: the reverse ISB must emit: %s", why)
	}

	for _, c := range []struct {
		name string
		in   mentor.Intent
	}{
		{"regular ISB long", regular},
		{"reverse ISB at EMA 9 long", reverse},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("MENTOR_PLACE", "on")
			at, ledger, raw := mentorLoopbackRaw(t, ntwire.MinAddonBuildStopLimit)
			mentorWireSeams(t, at, ledger)
			mentorPinClockToRTH(t)
			in := c.in
			in.ArmID = "isb-offset"
			in.Target = 29640
			in.StopPts = math.Abs(in.Price - in.Stop)
			in.TargetPts = math.Abs(in.Target - in.Price)
			in.ExpiryMs = rthInstant().UnixMilli() + 60_000
			at.mentorDispatchIntent(in, mentorTierInputs{}, i1.CloseTime, i1.CloseTime+100) // the newest closed bar IS I1 (N10 stale-intent rule)
			now := rthInstant()
			at.armedTrader().GetServer().OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
			at.runArmedPlacementAt([]market.Kline{{Close: 29599}}, now.Add(-time.Hour).UnixMilli(), now, nil)
			var payload []byte
			select {
			case payload = <-raw:
			case <-time.After(2 * time.Second):
				t.Fatal("no signal frame reached the wire")
			}
			var p ntwire.SignalPayload
			if err := json.Unmarshal(payload, &p); err != nil {
				t.Fatal(err)
			}
			want := i1.High + 1.5 // 29602.50 — 6 ticks past the ISB high
			if p.StopPrice != want {
				t.Fatalf("%s: the wire trigger is %.2f = ISB high %.2f + %.2f; the course says +1.5 (6 ticks) → %.2f [D2.1 p1 @ 05:36]",
					c.name, p.StopPrice, i1.High, p.StopPrice-i1.High, want)
			}
			var row store.ArmedOrderDB
			if err := ledger.DB().Where("scenario LIKE ?", "isb-offset-%").First(&row).Error; err != nil {
				t.Fatal(err)
			}
			if row.EntryPx != p.StopPrice {
				t.Fatalf("%s: ONE definition — the ledger entry %.2f must equal the wire trigger %.2f", c.name, row.EntryPx, p.StopPrice)
			}
		})
	}
}

// TestStopEntryWireOffsetIsPlannerOnly — the ONE definition of the hidden
// stop-entry offset, read by the production placement branch. A planner
// stop entry keeps the E7 offset by spec (kernel/entry_law.go
// StopEntryOffsetTicks, "the stop trigger sits N ticks beyond the break
// candle"; MATH-EXECUTOR row 167: authored 29835.75, filled 29836.25); a
// mentor row gets none — its kernel buffer is the whole offset.
func TestStopEntryWireOffsetIsPlannerOnly(t *testing.T) {
	const tick = 0.25
	planner := store.ArmedOrderDB{Kind: "stop_entry", Origin: ""}
	if got, want := stopEntryWireOffset(planner, tick), float64(stopEntryOffsetTicks())*tick; got != want {
		t.Fatalf("planner stop entry offset = %.2f, want the E7 offset %.2f", got, want)
	}
	mentorRow := store.ArmedOrderDB{Kind: "stop_entry", Origin: store.ArmOriginMentor}
	if got := stopEntryWireOffset(mentorRow, tick); got != 0 {
		t.Fatalf("mentor stop entry offset = %.2f, want 0 (the kernel's buffer is the wire)", got)
	}
	d := decideStopEntry("LONG", 29835.75, stopEntryWireOffset(planner, tick), tick, 29830)
	if d.Trigger != 29836.25 || d.OffsetTicks != 2 {
		t.Fatalf("planner trigger %.2f offset %dt, want 29836.25 / 2t (row 167's fill)", d.Trigger, d.OffsetTicks)
	}
	d = decideStopEntry("LONG", 30890.25, stopEntryWireOffset(mentorRow, tick), tick, 30885.25)
	if d.Trigger != 30890.25 || d.OffsetTicks != 0 {
		t.Fatalf("mentor trigger %.2f offset %dt, want 30890.25 / 0t (row 357: I1 high 30888.75 + 1.5)", d.Trigger, d.OffsetTicks)
	}
}

// TestMentorStopEntrySeamOffIsLoggedAndCounted — rel22 item 4 (SETUPS P2:
// silent refusals). With STOP_ENTRY_SEAM off, the stop_entry branch skipped a
// MENTOR row with a bare `continue`: the arm sat armed forever and nothing
// said why. Now it is counted once per arm (stop_entry:seam_off) with a WARN;
// a second pass on the same arm does not re-count. Production call site: the
// armed pass (runArmedPlacementAt).
//
// MUTANT: drop the mentor branch above the `continue` → the count is 0 → RED.
func TestMentorStopEntrySeamOffIsLoggedAndCounted(t *testing.T) {
	t.Setenv("MENTOR_PLACE", "on")
	at, st, ledger, frames := mentorLoopback(t, ntwire.MinAddonBuildStopLimit)
	mentorWireSeams(t, at, ledger)
	mentorPinClockToRTH(t)
	t.Setenv("STOP_ENTRY_SEAM", "off")
	now := rthInstant()
	at.mentorDispatchIntent(mentor.Intent{Action: mentor.PlaceStopLimitEntry, ArmID: "isb-seam", Setup: "ISB", Side: mentor.SideLong,
		Price: 29600, Stop: 29595, Target: 29640, StopPts: 5, TargetPts: 40, ExpiryMs: now.UnixMilli() + 60_000}, mentorTierInputs{}, 1000, 1100)
	var row store.ArmedOrderDB
	if err := ledger.DB().Where("scenario LIKE ?", "isb-seam-%").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		at.runArmedPlacementAt([]market.Kline{{Close: 29599}}, now.Add(-time.Hour).UnixMilli(), now, nil)
	}
	if sawMentorFrame(t, frames, ntwire.FrameSignal, 200*time.Millisecond) {
		t.Fatal("seam off: nothing may reach the wire")
	}
	if n := store.ArmRefusalCount(st, at.id, armRowTradeDate(row.PlanID, now), row.Session, "stop_entry:seam_off"); n != 1 {
		t.Fatalf("stop_entry:seam_off counted %d, want exactly 1 (once per arm, never silent)", n)
	}
}
