package trader

import (
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
)

// m437b (release #53, recheck lens p6) — a PHL/PLH position REBUILT from the
// broker book after a restart has an unknown fill age: BarsSinceFill and
// FramesAfterFill restart at 0, so the 3-candle resonance window re-opened on
// the 2nd-4th candle after the boot and a same-side ISB flipped a mature runner
// to mode A (and, with #52 A3, uncapped its TP). The rebuild must mark the
// window spent. Production path: mentorRebuildLivePosAtBoot builds the
// position; mentorMaybeArmResonance is the gate mentorArmResonanceOnISB calls
// with p.candlesAfterFill().
func TestM437RebuiltPHLPositionNeverReopensResonanceWindow(t *testing.T) {
	for _, tf := range []int{0, 2} {
		at, st := f2Trader(t)
		sig := "aa07e583-0000-0000-0000-0000000004b7"
		arm := f2Arm(sig, "PHL", 2, 1, 29600, 29590, 29610, nil)
		if err := st.ArmedOrders().UpsertArm(arm); err != nil {
			t.Fatal(err)
		}
		if err := st.Position().CreateOpenPosition(f2Pos(sig, 2, 2, 29600)); err != nil {
			t.Fatal(err)
		}
		f2PinBook(t, []ntwire.NT8Order{
			f2StopOrder(sig+"-sl", 29590, 1),
			f2StopOrder(sig+"-sl2", 29590, 1),
			f2TPOrder(sig+"-tp", 29610),
		}, true, 0)
		if rebuilt, refused, ran := at.mentorRebuildLivePosAtBoot(time.Now()); !ran || rebuilt != 1 || refused != 0 {
			t.Fatalf("rebuilt=%d refused=%d ran=%v want 1/0/true", rebuilt, refused, ran)
		}
		lp := at.mentorLivePos[sig]
		if lp == nil || lp.Pos.Origin != "PHL" {
			t.Fatalf("PHL position not rebuilt: %+v", lp)
		}
		lp.TrailTFMin = tf
		// the drive's counters restart at 0 after the boot: the 2nd..4th closed candle.
		for n := 2; n <= 4; n++ {
			lp.BarsSinceFill, lp.FramesAfterFill = n, n-1
			armed, _, _ := mentorMaybeArmResonance(at, &lp.Pos, lp.Pos.Side, lp.candlesAfterFill(), 29650)
			if armed || lp.Pos.Mode == "A-resonance" {
				t.Fatalf("tf=%d candle %d after the boot: a REBUILT PHL position must not re-open the resonance window (mode=%q)", tf, n, lp.Pos.Mode)
			}
		}
	}
}

// Control: the same counters on a position that was NOT rebuilt (a live fill)
// still arm — the fix must not switch resonance off for fresh fills.
func TestM437LiveFilledPHLPositionStillArmsResonance(t *testing.T) {
	at, _ := f2Trader(t)
	lp := &mentorLivePos{
		Pos:           mentorPosition{Symbol: "MNQ", Side: "long", Origin: "PHL", Entry: 29600, Stop: 29590, Target: 29610, R: 10, Mode: "B"},
		BarsSinceFill: 2,
	}
	armed, _, _ := mentorMaybeArmResonance(at, &lp.Pos, "long", lp.candlesAfterFill(), 29650)
	if !armed || lp.Pos.Mode != "A-resonance" {
		t.Fatalf("a live (not rebuilt) PHL fill must still arm resonance: armed=%v mode=%q", armed, lp.Pos.Mode)
	}
}
