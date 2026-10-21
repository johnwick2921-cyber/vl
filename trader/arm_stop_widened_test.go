package trader

import (
	"regexp"
	"strings"
	"testing"

	"vl/kernel"
	"vl/store"
)

// R22 P2 (PLANNER.md F9) — the ATR floor stays (the owner's open question),
// but every widening past the authored stop is logged in points and dollars
// and each widened arm is RECORDED once. #649: authored 30873.00, composed
// 30882.78 → +9.78 pts = $19.55 per MNQ contract ($2/pt), and the trade lost
// $79.00 against about $59.50 at the authored stop.
func TestArmStopWideningLineCarriesPointsAndDollars(t *testing.T) {
	c := StopComposition{Stop: 30882.78, Authored: 30873.00, Bound: "atr_floor", ATRFloorStop: 30882.78, Unanchored: true}
	line := armStopCompositionLine("ASIA", "S2", 1, "short", c, 22.52, 1.5, 2.0)
	if !strings.Contains(line, "(authored 30873.00 WIDENED +9.78 pts = $19.56/contract past the authored stop)") {
		t.Fatalf("the widening must say how far and what it costs:\n%s", line)
	}
	if l := armStopCompositionLine("ASIA", "S2", 1, "short", c, 22.52, 1.5); !strings.Contains(l, "+9.78 pts = $ n/a") {
		t.Fatalf("an unknown point value prints $ n/a, never a guessed dollar figure:\n%s", l)
	}
}

// Production call site: the arm pass (maybeManageArmedOrdersAt) composing a
// market_in_zone stop from the NEAR bound — zoneDoc's stop widens 98 → ~97.70.
// Three passes: the widening is logged with its points/$ and counted ONCE.
func TestArmStopWideningCountedOncePerArm(t *testing.T) {
	r := newZoneRig(t, "r22-stop-widened", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	logBuf := captureTraderLog(t)
	// Short of the zone (authored, not placed). The ATR moves between passes, so
	// the composed stop moves and the composition line RE-LOGS each pass (the
	// live 10-13 shape: 119 WIDENED lines for a handful of arms).
	for i := 0; i < 3; i++ {
		tape := zoneTape(99.0, r.now, 0)
		for j := range tape {
			w := 0.5 + 0.1*float64(i)
			tape[j].High, tape[j].Low = tape[j].Close+w, tape[j].Close-w
		}
		r.setTape(tape)
		r.at.maybeManageArmedOrdersAt(nil, r.now)
	}
	if n := strings.Count(logBuf.String(), "authored 98.00 WIDENED"); n < 2 {
		t.Fatalf("fixture: the moving ATR must re-log the widening (got %d lines)", n)
	}
	if !regexp.MustCompile(`authored 98\.00 WIDENED \+0\.\d\d pts = \$\d+\.\d\d/contract past the authored stop`).MatchString(logBuf.String()) {
		t.Fatalf("the composition line must carry the widening in points and $:\n%s", logBuf.String())
	}
	sess := kernel.ActivePlanFor(r.at.id, r.at.futuresSymbol()).Session
	key := "arm_stop_widened:" + r.at.id + ":" + kernel.PlanTradeDateFor(kernel.ActivePlanFor(r.at.id, r.at.futuresSymbol())) + ":" + sess
	if n, err := store.SystemCounter(r.st, key); err != nil || n != 1 {
		t.Fatalf("one widened arm, three passes → counted once: %s = %d (%v)", key, n, err)
	}
}
