package trader

import (
	"fmt"
	"testing"
	"time"

	"vl/market"
	"vl/store"
	nttrader "vl/trader/ninjatrader"
)

// ── C52 W2 — exit drive: resonance + candle trail (A3, B18, B1, B2) ─────────
//
// Every pin drives a production entry point (mentorArmResonanceOnISB /
// mentorExitDrive / mentorExitDrivePos / mentorResonanceRunnerTP) and is RED on
// the base d3dfe91fc. Named mutants (each anchored to ONE site, run RED, then
// restored; see the report):
//   W2-M1 mentor_course_exits.go  `at.mentorResonanceRunnerUncap(nt, p)` in the knob-OFF branch deleted
//         → TestC52W2A3UncapIsSentByDefault, ...Retries..., ...ThreePair... RED.
//   W2-M2 mentor_course_exits.go  `mentorResonanceRunnerTPOn`: `if rc == nil { return false }` → `return true`
//         → TestC52W2A3KnobDefaultsAgree RED.
//   W2-M3 mentor_live_pos.go      `candlesAfterFill` 2m branch → `return p.BarsSinceFill` (the old 1m clock)
//         → TestC52W2B18ResonanceWindowCountsEntryFrameCandles RED.
//   W2-M4 mentor_trail_r22.go     `at.mentorLeg1RR11Step(nt, p, c)` deleted
//         → TestC52W2B1Leg1KeepsTheLive11StopInCandleMode RED.
//   W2-M5 mentor_exit_drive.go    `if !scaledNow { ... return }` (split branch) deleted
//         → TestM151ResonanceMovesLeg1 / TestC52W2B2... RED.

// ── A3 ──────────────────────────────────────────────────────────────────────

// The knob default is ONE value on both resolver paths: before W2 a nil risk
// control read ON and a loaded strategy with the key unset read OFF.
func TestC52W2A3KnobDefaultsAgree(t *testing.T) {
	if mentorResonanceRunnerTPOn(nil) {
		t.Fatal("rc == nil must read OFF (no cap), like a loaded strategy with the key unset")
	}
	if mentorResonanceRunnerTPOn(&store.RiskControlConfig{}) {
		t.Fatal("a loaded strategy with the key unset must read OFF (no cap)")
	}
	on := true
	if !mentorResonanceRunnerTPOn(&store.RiskControlConfig{MentorResonanceRunnerTPOldHigh: &on}) {
		t.Fatal("an explicit ON must stay ON (the old-high cap, legacy inference)")
	}
	if mentorCourseExitsResolve(&AutoTrader{}, nil).ResonanceRunnerTP {
		t.Fatal("the effective-settings resolver must show OFF for rc == nil")
	}
}

// A3: an unset knob (the live state of all 9 strategies) UNCAPS the runner at
// the arm — one real modify_bracket on the runner's wire leg with the parked
// TP — even with NO level beyond the old high on record (RunnerTarget 0).
// Before W2 nothing was sent: the runner kept the PHL/PLH TP shy of the old
// high, the very leg the mentor says continues.
func TestC52W2A3UncapIsSentByDefault(t *testing.T) {
	b := r22Fixture(t, store.RiskControlConfig{}) // knob key unset
	b.positions("short", 3)
	b.book(r22FullBook(r22Stop)...)
	b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
	b.candle(r22Minute(0), 30742.25, 30744.00, 30738.00, 30739.00) // the fill candle (bar 1)
	b.candle(r22Minute(1), 30739.00, 30741.00, 30737.00, 30738.00) // one candle after it
	b.at.mentorArmResonanceOnISB("short")
	lp := b.at.mentorLivePos[r22Sig]
	if lp == nil || lp.Pos.Mode != "A-resonance" {
		t.Fatalf("the resonance must arm mode A: %+v", lp)
	}
	want := r22Entry - mentorResonanceNoCapPts
	mods := b.waitMods(1)
	if len(mods) != 1 || mods[0].SignalID != r22Sig || mods[0].Leg != 2 || mods[0].NewTakeProfit != want || mods[0].NewStopLoss != 0 {
		t.Fatalf("want ONE modify_bracket {leg 2, TP %.2f}; got %+v", want, mods)
	}
	if tp := lp.Legs[1].TP; tp != want {
		t.Fatalf("the runner TP belief must follow the send: %.2f want %.2f", tp, want)
	}
	if lp.Legs[0].TP != r22L1TP {
		t.Fatalf("leg 1's +1R TP must stay: %.2f", lp.Legs[0].TP)
	}
	// later closed candles do not re-send it (sent once, tries latched).
	b.candle(r22Minute(2), 30738.00, 30739.00, 30736.00, 30737.00)
	if mods = b.waitMods(1); len(mods) != 1 {
		t.Fatalf("the uncap must be sent ONCE, got %d frames: %+v", len(mods), mods)
	}
	if n := b.at.mentorCountSnapshot()["resonance_runner_uncapped"]; n != 1 {
		t.Fatalf("resonance_runner_uncapped = %d, want 1", n)
	}
}

// A3: a refused send is retried on later closed candles, bounded to
// mentorResonanceUncapMaxTries, then never again.
func TestC52W2A3UncapRetriesAfterAFailedSendThenStops(t *testing.T) {
	at, _ := newDriveAT(t)
	var sent []float64
	fail := true
	old := modifyBracketWire
	modifyBracketWire = func(_ *nttrader.TCPTrader, _ string, _ string, leg int, tp float64) error {
		sent = append(sent, tp)
		if fail {
			return fmt.Errorf("broker refused")
		}
		return nil
	}
	t.Cleanup(func() { modifyBracketWire = old })
	p := bPos("A-resonance", "short", 100, 102, 90, 2)
	p.Pos.Origin = "PLH"
	p.Pos.Leg1, p.Pos.Leg2, p.Pos.Leg1TP = 1, 1, 98
	p.Legs[0] = mentorLeg{SignalID: "entry", Qty: 1, TP: 98, Stop: 102, Wire: 1}
	p.Legs[1] = mentorLeg{SignalID: "entry", Qty: 1, TP: 90, Stop: 102, Final: true, Wire: 2}
	at.mentorRegisterLivePos("entry", p)
	for i := 0; i < 5; i++ { // five closed candles, every send refused
		at.mentorExitDrivePos(nil, p, 99, 100, 98.5, 0)
	}
	if len(sent) != mentorResonanceUncapMaxTries {
		t.Fatalf("a refused uncap must be tried exactly %d times, got %d: %v", mentorResonanceUncapMaxTries, len(sent), sent)
	}
	if p.Legs[1].TP != 90 {
		t.Fatalf("a refused send must not move the TP belief: %.2f", p.Legs[1].TP)
	}
	// a fresh position whose first send is refused and the second applied.
	sent, fail = nil, true
	q := bPos("A-resonance", "short", 100, 102, 90, 2)
	q.Pos.Origin = "PLH"
	q.Pos.Leg1, q.Pos.Leg2, q.Pos.Leg1TP = 1, 1, 98
	q.Legs[0] = mentorLeg{SignalID: "q", Qty: 1, TP: 98, Stop: 102, Wire: 1}
	q.Legs[1] = mentorLeg{SignalID: "q", Qty: 1, TP: 90, Stop: 102, Final: true, Wire: 2}
	at.mentorRegisterLivePos("q", q)
	at.mentorExitDrivePos(nil, q, 99, 100, 98.5, 0)
	fail = false
	at.mentorExitDrivePos(nil, q, 99, 100, 98.5, 0)
	at.mentorExitDrivePos(nil, q, 99, 100, 98.5, 0)
	if len(sent) != 2 || q.Legs[1].TP != 100-mentorResonanceNoCapPts {
		t.Fatalf("one refused + one applied send expected, TP belief %.2f: %v", q.Legs[1].TP, sent)
	}
}

// A3: in a 3-pair plan only the Final runner (wire 3) is uncapped — the runner
// partial (wire 2, its own line TP) and leg 1 keep theirs. A ONE-BRACKET
// position is never touched (a TP modify would move the WHOLE position).
func TestC52W2A3UncapTargetsOnlyTheFinalRunner(t *testing.T) {
	at, _ := newDriveAT(t)
	type sentMod struct {
		leg int
		tp  float64
	}
	var sent []sentMod
	old := modifyBracketWire
	modifyBracketWire = func(_ *nttrader.TCPTrader, _ string, _ string, leg int, tp float64) error {
		sent = append(sent, sentMod{leg, tp})
		return nil
	}
	t.Cleanup(func() { modifyBracketWire = old })

	p := bPos("B", "long", 100, 98, 106, 5)
	p.Pos.Origin = "PHL"
	p.Legs[0] = mentorLeg{SignalID: "e", Qty: 2, TP: 102, Stop: 98, Wire: 1}
	p.Legs[1] = mentorLeg{SignalID: "e", Qty: 1, TP: 104, Stop: 98, Wire: 2}
	p.Legs[2] = mentorLeg{SignalID: "e", Qty: 2, TP: 106, Stop: 98, Final: true, Wire: 3}
	at.mentorResonanceRunnerTP(nil, p, 0)
	if len(sent) != 1 || sent[0].leg != 3 || sent[0].tp != 100+mentorResonanceNoCapPts {
		t.Fatalf("3-pair: only wire 3 → %.2f; got %+v", 100+mentorResonanceNoCapPts, sent)
	}
	if p.Legs[0].TP != 102 || p.Legs[1].TP != 104 {
		t.Fatalf("leg 1 / the runner partial keep their TPs: %.2f / %.2f", p.Legs[0].TP, p.Legs[1].TP)
	}

	sent = nil
	one := bPos("B", "long", 100, 98, 106, 1) // single bracket, Wire 0
	one.Pos.Origin = "PHL"
	at.mentorResonanceRunnerTP(nil, one, 0)
	if len(sent) != 0 {
		t.Fatalf("a one-bracket position must never get a TP modify: %+v", sent)
	}
	if n := at.mentorCountSnapshot()["resonance_runner_uncap_not_split"]; n != 1 {
		t.Fatalf("resonance_runner_uncap_not_split = %d, want 1", n)
	}
}

// ── B18 ─────────────────────────────────────────────────────────────────────

// B18: the 3-candle window is counted in candles of the frame the ENTRY was read
// on, from the first candle AFTER the fill candle. A 2m entry: the fill's 2m
// candle is not counted; the ISB at the 2nd and 3rd 2m candle after it arms mode
// A (on the 1m clock those are 1m bars 6 and 8 — refused before W2), the 4th is
// outside the window. A 1m entry keeps the 1m clock (fill candle = bar 1, ISB at
// 1..3 candles after it).
func TestC52W2B18ResonanceWindowCountsEntryFrameCandles(t *testing.T) {
	armAfter := func(t *testing.T, tf, closed1m int) string {
		t.Helper()
		b := r22Fixture(t, store.RiskControlConfig{})
		b.positions("short", 3)
		b.book(r22FullBook(r22Stop)...)
		b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
		b.at.mentorLivePos[r22Sig].TrailTFMin = tf
		base := time.Now().Add(-90 * time.Minute).Truncate(10 * time.Minute) // aligned to every frame
		var bars []market.Kline
		for i := 0; i < closed1m; i++ {
			ms := base.Add(time.Duration(i) * time.Minute).UnixMilli()
			bars = append(bars, market.Kline{OpenTime: ms, CloseTime: ms + 59_999, Open: 30739, High: 30741, Low: 30737, Close: 30738, Final: true})
			b.at.mentorExitDrive(bars)
		}
		b.at.mentorArmResonanceOnISB("short")
		return b.at.mentorLivePos[r22Sig].Pos.Mode
	}
	cases := []struct {
		name     string
		tf       int
		closed1m int
		want     string
	}{
		{"2m entry: 1st 2m candle after the fill candle", 2, 4, "A-resonance"},
		{"2m entry: 2nd 2m candle after (1m bar 6 — refused on the old clock)", 2, 6, "A-resonance"},
		{"2m entry: 3rd 2m candle after (1m bar 8 — refused on the old clock)", 2, 8, "A-resonance"},
		{"2m entry: 4th 2m candle after is outside the window", 2, 10, "B"},
		{"2m entry: the fill's own 2m candle is not 'after the fill'", 2, 2, "B"},
		{"2m entry: a 2m candle still forming adds nothing", 2, 5, "A-resonance"}, // 1 closed 2m after the fill candle... (4 → 1; 5 forming → still 1)
		{"5m entry: 1st 5m candle after", 5, 10, "A-resonance"},
		{"5m entry: 4th 5m candle after", 5, 25, "B"},
		{"1m entry: candle after the fill candle (bar 2)", 1, 2, "A-resonance"},
		{"1m entry: 3rd candle after (bar 4)", 1, 4, "A-resonance"},
		{"1m entry: 4th candle after (bar 5) is outside", 1, 5, "B"},
		{"1m entry: the fill candle itself (bar 1) is not after the fill", 1, 1, "B"},
	}
	for _, c := range cases {
		if got := armAfter(t, c.tf, c.closed1m); got != c.want {
			t.Errorf("%s: mode %q, want %q", c.name, got, c.want)
		}
	}
}

// ── B2 ──────────────────────────────────────────────────────────────────────

// B2 through the full drive: a split resonance position moves NOTHING before the
// 1:1 partial is confirmed (even at +0.5R), and after the broker's receipt only
// the RUNNER goes to break-even. Before W2 BE fired on every leg at +0.25R.
func TestC52W2B2ResonanceBEWaitsForThePartialAndMovesTheRunnerOnly(t *testing.T) {
	b := r22Fixture(t, store.RiskControlConfig{})
	b.positions("short", 3)
	b.book(r22FullBook(r22Stop)...)
	b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
	b.candle(r22Minute(0), 30742.25, 30744.00, 30738.00, 30739.00)
	b.candle(r22Minute(1), 30739.00, 30741.00, 30737.00, 30738.00)
	b.at.mentorArmResonanceOnISB("short")
	if lp := b.at.mentorLivePos[r22Sig]; lp == nil || lp.Pos.Mode != "A-resonance" {
		t.Fatalf("fixture: mode A expected: %+v", lp)
	}
	// +0.5R close (30733.25 is half of R 18): the old rule's 0.25R pre-partial BE.
	b.candle(r22Minute(2), 30738.00, 30738.50, 30732.00, 30733.00)
	if got := b.waitMoves(0); len(got) != 0 {
		t.Fatalf("B2: no BE before the 1:1 partial is confirmed; got %+v", got)
	}
	b.receiptLeg1(r22Sig, "short", r22L1TP, r22Minute(3).Add(10*time.Second))
	b.positions("short", 1)
	b.book(r22RunnerBook(r22Stop)...)
	b.candle(r22Minute(3), 30733.00, 30734.00, 30724.00, 30726.00)
	got := b.waitMoves(1)
	if len(got) != 1 || got[0].Leg != 2 || got[0].NewStopLoss != r22Entry {
		t.Fatalf("B2: after the partial ONLY the runner (leg 2) goes to BE %.2f; got %+v", r22Entry, got)
	}
}

// ── B1 ──────────────────────────────────────────────────────────────────────

// B1 (SHORT 3 @ 30742.25, stop 30760.25, R 18, leg-1 TP 30724.25, candle mode):
// once BE is armed (the candle that arms it only arms), leg 1 — still unfilled —
// keeps the LIVE 1:1 stop 2·close − T on every closed candle, never widening;
// the runner is untouched. Before W2 leg 1 stayed at BE until its TP filled
// (an 18 → 11 style pullback exited it at 0R instead of banking the gain).
func TestC52W2B1Leg1KeepsTheLive11StopInCandleMode(t *testing.T) {
	b := r22Fixture(t, store.RiskControlConfig{}) // BE-before-partial default ON, trail mode candle
	b.positions("short", 3)
	b.book(r22FullBook(r22Stop)...)
	b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)

	b.candle(r22Minute(0), 30742.25, 30744.00, 30738.00, 30739.00)
	// candle 1: close 30732.00 is past half-way (30733.25) — BE arms, BOTH legs
	// to 30742.25; the 1:1 rule does NOT run on the arming candle.
	b.candle(r22Minute(1), 30739.00, 30739.50, 30731.00, 30732.00)
	got := b.waitMoves(2)
	if len(got) != 2 {
		t.Fatalf("the arming candle moves exactly the two BE stops; got %+v", got)
	}
	b.book(r22StopOrder(r22Sig+"-sl", r22Entry, 2), r22StopOrder(r22Sig+"-sl2", r22Entry, 1),
		r22TP(r22Sig+"-tp", r22L1TP, 2), r22TP(r22Sig+"-tp2", r22Target, 1))

	// candle 2: close 30730.00 → leg 1 stop = 2·30730 − 30724.25 = 30735.75.
	b.candle(r22Minute(2), 30732.00, 30733.00, 30729.00, 30730.00)
	got = b.waitMoves(3)
	if len(got) != 3 || got[2].Leg != 1 || got[2].NewStopLoss != 30735.75 {
		t.Fatalf("B1: leg 1 → live 1:1 stop 30735.75 (leg 1 ONLY); got %+v", got)
	}
	b.book(r22StopOrder(r22Sig+"-sl", 30735.75, 2), r22StopOrder(r22Sig+"-sl2", r22Entry, 1),
		r22TP(r22Sig+"-tp", r22L1TP, 2), r22TP(r22Sig+"-tp2", r22Target, 1))

	// candle 3: price back off to 30736.00 → 2·c − T = 30747.75 would WIDEN: nothing.
	b.candle(r22Minute(3), 30730.00, 30737.00, 30729.50, 30736.00)
	if got = b.waitMoves(3); len(got) != 3 {
		t.Fatalf("B1: the 1:1 stop never widens; got %+v", got)
	}
	// candle 4: close 30722.00 is past leg 1's target (30724.25): 2c − T lands
	// THROUGH the market — skipped, counted, never sent (NT8 refuses it).
	b.candle(r22Minute(4), 30736.00, 30736.50, 30721.00, 30722.00)
	if got = b.waitMoves(3); len(got) != 3 {
		t.Fatalf("B1: a close past the target must not send a through-the-market stop; got %+v", got)
	}
	if n := b.at.mentorCountSnapshot()["leg1_rr11_skipped_at_market"]; n != 1 {
		t.Fatalf("leg1_rr11_skipped_at_market = %d, want 1", n)
	}
	if n := b.at.mentorCountSnapshot()["leg1_rr11_moved"]; n != 1 {
		t.Fatalf("leg1_rr11_moved = %d, want 1", n)
	}
	for _, m := range got[2:] {
		if m.Leg != 1 {
			t.Fatalf("only leg 1 may move on the 1:1 rule (the runner waits for the partial): %+v", got)
		}
	}
}

// B1 is the course 1:1 rule of mentor_be_before_partial ON; with the knob OFF
// (owner R3: nothing moves before the confirmed partial) leg 1 keeps its stop.
func TestC52W2B1KnobOffMovesNothingBeforeThePartial(t *testing.T) {
	off := false
	b := r22Fixture(t, store.RiskControlConfig{MentorBEBeforePartial: &off})
	b.positions("short", 3)
	b.book(r22FullBook(r22Stop)...)
	b.registerSplit(r22Sig, "short", "PLH", 3, 2, r22Entry, r22Stop, r22L1TP, r22Target)
	b.candle(r22Minute(0), 30742.25, 30744.00, 30738.00, 30739.00)
	b.candle(r22Minute(1), 30739.00, 30739.50, 30731.00, 30732.00)
	b.candle(r22Minute(2), 30732.00, 30733.00, 30729.00, 30730.00)
	if got := b.waitMoves(0); len(got) != 0 {
		t.Fatalf("knob OFF (R3): nothing moves before the confirmed partial; got %+v", got)
	}
}
