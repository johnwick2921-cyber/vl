package trader

import (
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── EXTRAS-ALIGN (2026-10-14) — trader half ─────────────────────────────────
//
// Item 3: the runner trails on the frame its entry was READ on — "nếu như mình
// trade khung nào thì mình gồng theo khung đó, theo cây nến của khung đó"
// [X8 @18:02–18:14]. The pin runs the WHOLE production path on a real loopback
// bridge (r22Fixture): the evaluator's Tick (exec_2m_after_30m ON, 09:04 CT)
// emits the 2m ISB → mentorArmIntent authors the row → registerMentorLivePos
// (the fill) → dispatchPositionLegClosed (the leg-1 receipt) → mentorExitDrive
// on closed 1m bars; it asserts the move_stop frames the fake AddOn reads.
//
// Item 4: trail_tf 30s/45s are refused at config load (mentorSeedAtStart →
// mentorTrailTFLoadCheck): counted, WARN'd, and the resolver returns 1m.
//
// Named mutants (each anchored to ONE site, run RED then restored):
//   E3 trail on the 1m again — mentor_exit_drive.go mentorExitDrivePos
//      `if p.TrailTFMin > 1 {` (the trail-candle switch, the one that sets
//      trailH, trailL, trailOK) → `if false {` → TestExtrasAlign2mEntryTrailsOnClosed2mCandles RED.
//   E3b the frame never rides to the fill — mentor_actions.go mentorArmIntent
//      `if in.ExecTFMin > 1 {` → `if false {` → the same pin RED.
//   E4 silent load — mentor_mode.go mentorTrailTFLoadCheck
//      `if !mentorTrailTFSubMinute(v) {` → `if true {` → TestExtrasAlignTrailTFSubMinuteRefusedAtLoad RED.
//   E4b 30s honoured — mentor_mode.go mentorTrailTF
//      `return mentorTrailTFDefault // refused → the 1m` → `return v` → the same pin RED.

// x2mISBIntent runs the PRODUCTION evaluator Tick on a 2m-ISB tape at 09:04 CT
// and returns its ISB entry intent. exec2m picks the knob.
func x2mISBIntent(t *testing.T, exec2m bool) mentor.Intent {
	t.Helper()
	ct, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 15, 8, 58, 0, 0, ct).UnixMilli()
	mk := func(i int, o, h, l, c float64) market.Kline {
		ot := base + int64(i)*60_000
		return market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: o, High: h, Low: l, Close: c, Final: true}
	}
	bars := []market.Kline{
		mk(0, 121, 122, 95, 120.5), // head: keeps the EMA target above; its deep wick keeps the 5m trigger long
		mk(1, 121, 121.5, 120, 121),
		mk(2, 99, 104, 98.5, 103), // 2m candle A (09:00–09:01): green 99→106
		mk(3, 103, 106, 102, 106),
		mk(4, 101, 102.1, 100.9, 101.5), // 2m candle B (09:02–09:03): body inside A
		mk(5, 101.5, 102, 101, 102),
	}
	cfg := mentor.DefaultConfig()
	cfg.Enabled = true
	cfg.RoomMultiple = 0 // the pin is about the frame; the tape's own swing high sits in the room
	cfg.Exec2mAfter30m = exec2m
	e := mentor.New(cfg)
	now := mentor.BarCloseInstant(bars[len(bars)-1])
	e.State.Trigger = mentor.TriggerLine{Dir: mentor.SideLong, Price: 90}
	e.State.HTF = mentor.HTF{FourH: mentor.TriggerLine{Dir: mentor.SideLong, Price: 90}}
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, ct).UnixMilli()
	e.State.ORB = mentor.ORB{Day: day, High: 90, Low: 85, Drawn: true, Escaped: mentor.SideLong}
	e.State.Day = mentor.DayLatch{Key: "2026-09-15", Verdict: mentor.DayTrade}
	for _, in := range e.Tick(bars, now) {
		if (in.Action == mentor.PlaceStopLimitEntry || in.Action == mentor.PlaceStopEntry) && in.Setup == "ISB" {
			return in
		}
	}
	t.Fatalf("fixture: the evaluator emitted no ISB (exec2m=%v); refusals %v", exec2m, e.State.Refusals)
	return mentor.Intent{}
}

// extrasAlignFillISB authors the intent through mentorArmIntent, fills it as a
// 3-contract split (leg 1 = 2 at +1R, the runner = 1) and registers it through
// the production fill path. Returns the signal, entry, leg-1 TP and stop.
func extrasAlignFillISB(t *testing.T, b *r22Bridge, in mentor.Intent) (sig string, entry, leg1TP, stop float64) {
	t.Helper()
	b.at.mentorArmIntent(in, mentorSizeChoice{Contracts: 3, Tier: "base", Why: "extras-align"}, in.RefBarMs, in.RefBarMs, "B", 0)
	var rows []store.ArmedOrderDB
	if err := b.st.ArmedOrders().DB().Where("scenario LIKE ?", in.ArmID+"-%").Find(&rows).Error; err != nil || len(rows) != 1 {
		t.Fatalf("mentorArmIntent must author ONE row: %d rows, err %v", len(rows), err)
	}
	row := rows[0]
	sig = "7c1e0d52-1f0a-4c2b-9e55-extrasalign01"
	entry, stop = in.Price, in.Stop
	leg1TP = entry + (entry - stop)
	prev := mentorSentSplit
	mentorSentSplit = func(_ *AutoTrader, s string) (nttrader.SentSplit, bool) {
		if s == sig {
			return nttrader.SentSplit{Leg1Qty: 2, Leg1TP: leg1TP}, true
		}
		return prev(b.at, s)
	}
	t.Cleanup(func() { mentorSentSplit = prev })
	row.SignalID, row.State, row.FillQuantity, row.FillPrice = sig, store.StateFilled, 3, entry
	b.at.registerMentorLivePos(row, ntwire.OrderUpdatePayload{SignalID: sig, State: "filled", Quantity: 3, FillPrice: entry, Account: "Sim101"})
	if lp := b.at.mentorLivePos[sig]; lp == nil || lp.Legs[1].Wire != 2 || lp.Pos.Origin != "ISB" {
		t.Fatalf("fixture: the split ISB position did not register (%+v)", lp)
	}
	b.positions("long", 3)
	b.book(r22StopOrder(sig+"-sl", stop, 2), r22StopOrder(sig+"-sl2", stop, 1), r22TP(sig+"-tp", leg1TP, 2), r22TP(sig+"-tp2", in.Target, 1))
	return sig, entry, leg1TP, stop
}

// extrasAlignDrive runs the tape: the fill candle (a losing close — no
// candle-3 modify), the leg-1 receipt mid-candle 1, candle 1 closes past BE
// (BE goes out), then candles 2 and 3 — ONE 2m candle (2–3), low 107.5 on
// candle 2. Returns the move_stop frames after candle 2 and after candle 3.
func extrasAlignDrive(t *testing.T, b *r22Bridge, sig string, leg1TP, stop float64, want2, want3 int) (after2, after3 []ntwire.MoveStopPayload, beStop float64) {
	t.Helper()
	m0 := (time.Now().Add(-30*time.Minute).UnixMilli() / 120_000) * 120_000 // an even minute: candles 0–1 and 2–3 are 2m candles
	var bars []market.Kline
	candle := func(i int, o, h, l, c float64) {
		ms := m0 + int64(i)*60_000
		bars = append(bars, market.Kline{OpenTime: ms, CloseTime: ms + 59_999, Open: o, High: h, Low: l, Close: c, Final: true})
		b.at.mentorExitDrive(bars)
	}
	candle(0, 103.6, 104, 103, 103.4) // the fill candle — closes below the entry: no candle-3 modify
	b.receiptLeg1(sig, "long", leg1TP, time.UnixMilli(m0+60_000+10_000))
	b.positions("long", 1)
	b.book(r22StopOrder(sig+"-sl2", stop, 1))
	candle(1, 103.4, 108.5, 103.3, 108) // closes past BE → the runner's BE
	got := b.waitMoves(1)
	if len(got) != 1 || got[0].Leg != 2 {
		t.Fatalf("after the confirmed partial the runner goes to BE first; got %+v", got)
	}
	beStop = got[0].NewStopLoss
	b.book(r22StopOrder(sig+"-sl2", beStop, 1))
	candle(2, 108, 109, 107.5, 108.5) // the FIRST half of the 2m candle 2–3
	after2 = b.waitMoves(want2)
	if len(after2) > 1 {
		b.book(r22StopOrder(sig+"-sl2", after2[len(after2)-1].NewStopLoss, 1)) // NT8 applied the trail
	}
	candle(3, 108.5, 109.5, 108, 109) // the 2m candle 2–3 closes: low 107.5
	after3 = b.waitMoves(want3)
	return after2, after3, beStop
}

// TestExtrasAlign2mEntryTrailsOnClosed2mCandles — item 3 RED→GREEN. The ISB
// was read on the 2m (exec_2m_after_30m ON); its runner must trail on CLOSED
// 2m candles: nothing on the 1m close that only half-fills a 2m candle, then
// one tick beyond the 2m candle's low (107.25 = 107.5 − 1 tick) when it closes.
// Pre-fix the runner trailed every 1m close (107.25 on candle 2, 107.75 on
// candle 3) — the "khung 1 phút rất dễ bị quét râu" sweep X8 trails the 2m to
// avoid.
func TestExtrasAlign2mEntryTrailsOnClosed2mCandles(t *testing.T) {
	b := r22Fixture(t, store.RiskControlConfig{})
	in := x2mISBIntent(t, true)
	sig, _, leg1TP, stop := extrasAlignFillISB(t, b, in)
	after2, after3, _ := extrasAlignDrive(t, b, sig, leg1TP, stop, 2, 2)
	if len(after2) != 1 {
		t.Fatalf("a 2m entry must NOT trail on a 1m close that only half-fills its 2m candle (X8 @18:02); moves after candle 2: %+v", after2)
	}
	if len(after3) != 2 || after3[1].Leg != 2 || after3[1].NewStopLoss != 107.25 {
		t.Fatalf("when the 2m candle closes the runner trails one tick beyond ITS low (107.25); moves: %+v", after3)
	}
	if n := b.at.mentorCountSnapshot()["trail_candle_moved_2m"]; n != 1 {
		t.Fatalf("the 2m trail move must be counted (trail_candle_moved_2m), got %d", n)
	}
}

// TestExtrasAlign1mEntryTrailsAsBefore — the knob OFF (the live default): the
// ISB is read on the 1m, carries no frame, and its runner trails every closed
// 1m candle (107.25 on candle 2, 107.75 on candle 3 — the low − 1 tick).
func TestExtrasAlign1mEntryTrailsAsBefore(t *testing.T) {
	b := r22Fixture(t, store.RiskControlConfig{})
	in := x2mISBIntent(t, false)
	sig, _, leg1TP, stop := extrasAlignFillISB(t, b, in)
	after2, after3, _ := extrasAlignDrive(t, b, sig, leg1TP, stop, 2, 3)
	if len(after2) != 2 || after2[1].NewStopLoss != 107.25 {
		t.Fatalf("knob OFF: the runner trails the 1m candle 2 (low 107.5 − 1 tick = 107.25); moves: %+v", after2)
	}
	if len(after3) != 3 || after3[2].NewStopLoss != 107.75 {
		t.Fatalf("knob OFF: the runner trails the 1m candle 3 (low 108 − 1 tick = 107.75); moves: %+v", after3)
	}
	if n := b.at.mentorCountSnapshot()["trail_candle_moved_2m"]; n != 0 {
		t.Fatalf("knob OFF: no 2m trail, got %d", n)
	}
}

// TestExtrasAlignTrailTFSubMinuteRefusedAtLoad — item 4. The bot has no
// sub-minute bars (the AddOn streams 1m and up), so trail_tf 30s/45s never
// did anything while the guide said they did. At config load (the mentor
// seed, boot and every reload) the value is REFUSED: counted + WARN'd, and the
// resolver returns 1m. Pre-fix the load was silent and the resolver handed
// back "30s" (RED).
func TestExtrasAlignTrailTFSubMinuteRefusedAtLoad(t *testing.T) {
	for _, v := range []string{"30s", "45s"} {
		at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorTrailTF: v}})
		withMaintenanceDir(t)
		at.resetMentorCountersForTest()
		at.mentorSeedAtStart()
		if n := at.mentorCountSnapshot()["trail_tf_subminute_refused"]; n != 1 {
			t.Fatalf("trail_tf %s must be refused AT CONFIG LOAD (trail_tf_subminute_refused=1), got %d", v, n)
		}
		if got := at.mentorTrailTF(); got != "1m" {
			t.Fatalf("trail_tf %s must resolve to the 1m (no sub-minute bars exist), got %q", v, got)
		}
		if !mentorTrailEnabled(at.mentorTrailTF()) {
			t.Fatalf("trail_tf %s: the 1m trail still runs", v)
		}
	}
	// 1m / off / unset load silently, unchanged.
	for _, v := range []string{"", "1m", "off"} {
		at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorTrailTF: v}})
		withMaintenanceDir(t)
		at.resetMentorCountersForTest()
		at.mentorSeedAtStart()
		if n := at.mentorCountSnapshot()["trail_tf_subminute_refused"]; n != 0 {
			t.Fatalf("trail_tf %q is not refused, got %d", v, n)
		}
	}
}

// TestTrailTFRefusalCountedOncePerValueNotPerDrivePass — #23 review P3-6. The
// exit drive resolves trail_tf on EVERY pass of EVERY position
// (mentorExitDrivePos); a stored 30s counted trail_tf_subminute_refused each
// time, so the counter measured candles × positions, not refusals. Now: once
// at config load, and the drive adds nothing for a value already said.
func TestTrailTFRefusalCountedOncePerValueNotPerDrivePass(t *testing.T) {
	for _, tc := range []struct{ v, counter string }{{"30s", "trail_tf_subminute_refused"}, {"garbage", "trail_tf_bad_value"}} {
		at, _ := resetTrader(t, store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true, MentorTrailTF: tc.v}})
		withMaintenanceDir(t)
		at.resetMentorCountersForTest()
		at.mentorSeedAtStart() // the config load: said + counted once
		if n := at.mentorCountSnapshot()[tc.counter]; n != 1 {
			t.Fatalf("trail_tf %q: the load must count once, got %d", tc.v, n)
		}
		p := &mentorLivePos{Pos: mentorPosition{Mode: "swing", Side: "long"}}
		for i := 0; i < 50; i++ { // 50 drive passes (candles × positions)
			at.mentorExitDrivePos(nil, p, 100, 101, 99, int64(i)*60_000)
		}
		if n := at.mentorCountSnapshot()[tc.counter]; n != 1 {
			t.Fatalf("trail_tf %q: 50 drive passes must not re-count the refusal, %s=%d (want 1)", tc.v, tc.counter, n)
		}
	}
}
