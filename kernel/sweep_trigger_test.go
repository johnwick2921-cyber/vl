package kernel

import (
	"strings"
	"testing"

	"vl/market"
)

// R22 F1 — trade #649 (armed_orders 350, 2026-10-13 ASIA v2 S2) on its REAL
// tape. The plan was born 20:12:34.948 CT with price ~55 pts under ONH
// 30849.75; the live log read "wait_confirm MET (1x5m_close [stored])" at
// 20:15:01 — the first completed 5m close after birth, with no sweep. The bars
// below are bars rows MNQ 12-26 1m 01:12Z–01:23Z (open_time_ms 1791940320000…
// 1791940980000), read with sqlite3 -readonly.
func real649Tape() []market.Kline {
	raw := [][5]float64{
		{1791940320000, 30787.25, 30799.5, 30783.0, 30794.5},
		{1791940380000, 30794.75, 30795.0, 30785.5, 30788.25},
		{1791940440000, 30787.5, 30800.75, 30787.25, 30796.25},
		{1791940500000, 30796.0, 30796.0, 30781.75, 30788.75},
		{1791940560000, 30788.25, 30794.75, 30786.75, 30791.25},
		{1791940620000, 30792.5, 30794.25, 30785.5, 30792.25},
		{1791940680000, 30792.5, 30802.75, 30791.25, 30792.0},
		{1791940740000, 30792.25, 30796.5, 30787.25, 30791.75},
		{1791940800000, 30793.0, 30799.75, 30790.75, 30794.5},
		{1791940860000, 30794.25, 30801.5, 30792.0, 30794.25},
		{1791940920000, 30794.25, 30804.0, 30794.0, 30799.25},
		{1791940980000, 30799.5, 30815.25, 30798.0, 30813.5},
	}
	out := make([]market.Kline, 0, len(raw))
	for _, r := range raw {
		o := int64(r[0])
		out = append(out, market.Kline{OpenTime: o, CloseTime: o + 59999, Open: r[1], High: r[2], Low: r[3], Close: r[4]})
	}
	return out
}

func scenario649() PlanScenario {
	return PlanScenario{ID: "S2", Condition: "reclaim", Direction: "short",
		Trigger: "Sweep of 30849.75 ONH then a 1m close back below 30849.75 reclaims the level",
		Confirm: &PlanConfirm{Rule: "1x5m_close", RefPrice: 30849.75, Side: "below"},
		Arm:     &PlanArmSpec{Enabled: true, Entry: 30843.5, Stop: 30873, Target: 30744.5, WaitConfirm: true, Policy: EntryPolicyMarketInZone}}
}

const birth649 = int64(1791940354948) // plans.created_at 2026-10-13 20:12:34.948 CT (v2)

// Production call site: EvaluateScenarioConfirm — the function the arm gate
// (trader/armed_executor.go), the recorder and the executor prompt all read.
func TestSweepSetupConfirm649NoSweepIsNotMet(t *testing.T) {
	sc := scenario649()
	now := int64(1791940501453) // 20:15:01.453 CT — the live "wait_confirm MET" line
	v := EvaluateScenarioConfirm(sc, real649Tape(), birth649, now)
	if v.Met {
		t.Fatalf("#649 shape: a reclaim confirm with NO sweep of 30849.75 must be NOT MET (live read MET at 20:15:01): %+v", v)
	}
	if v.Refusal != SweepRefusalNoSweep || !strings.Contains(v.Detail, "waiting for the sweep") {
		t.Fatalf("the verdict must name the missing sweep: refusal=%q detail=%q", v.Refusal, v.Detail)
	}
	// Through 20:24 the 1m high never reaches the ref (max 30815.25): still NOT MET.
	if v := EvaluateScenarioConfirm(sc, real649Tape(), birth649, 1791941040000); v.Met {
		t.Fatalf("no sweep through 20:24 — still NOT MET: %s", v.Detail)
	}
}

func TestSweepSetupConfirmMetOnlyAfterSweepThenClose(t *testing.T) {
	sc := scenario649()
	tape := real649Tape()
	add := func(o int64, h, l, c float64) {
		tape = append(tape, market.Kline{OpenTime: o, CloseTime: o + 59999, Open: c, High: h, Low: l, Close: c})
	}
	// 20:25 — the sweep: a wick to 30851.00 (through ONH), close back under.
	add(1791941100000, 30851.00, 30838.00, 30845.00)
	for i := int64(1); i <= 4; i++ { // 20:26–20:29 hold below the ref
		add(1791941100000+i*60000, 30846.00, 30836.00, 30840.00)
	}
	// 20:29:30 — the 20:25–20:30 bucket is still forming: NOT MET.
	if v := EvaluateScenarioConfirm(sc, tape, birth649, 1791941370000); v.Met {
		t.Fatalf("forming bucket after the sweep must not be MET: %s", v.Detail)
	}
	// 20:30:01 — swept, then a completed 5m close back below: MET.
	v := EvaluateScenarioConfirm(sc, tape, birth649, 1791941401000)
	if !v.Met || !strings.Contains(v.Detail, "sweep of the ref") {
		t.Fatalf("sweep then a 5m close back below must be MET with the sweep as its reference: %+v", v)
	}
	// A touch AT the ref (no tick through) is the sweep evidence too.
	tape2 := real649Tape()
	tape = tape2
	add(1791941100000, 30849.75, 30838.00, 30845.00)
	for i := int64(1); i <= 4; i++ {
		add(1791941100000+i*60000, 30846.00, 30836.00, 30840.00)
	}
	if v := EvaluateScenarioConfirm(sc, tape, birth649, 1791941401000); !v.Met {
		t.Fatalf("a touch at the ref is the sweep evidence: %s", v.Detail)
	}
}

// Non-sweep conditions and a touch part-one are untouched: a hold confirm with
// the same tape still reads MET from birth exactly as before.
func TestSweepSetupGateLeavesOtherConditionsAlone(t *testing.T) {
	sc := scenario649()
	sc.Condition = "hold"
	if v := EvaluateScenarioConfirm(sc, real649Tape(), birth649, 1791940501453); !v.Met {
		t.Fatalf("a non-sweep condition keeps its close count from birth: %s", v.Detail)
	}
	if SweepSetupCondition("reject") || SweepSetupCondition("hold") || !SweepSetupCondition(" Sweep_Reclaim ") || !SweepSetupCondition("reclaim") {
		t.Fatal("SweepSetupCondition must be exactly the reclaim family")
	}
	// A long reclaim (closes ABOVE) is swept by a LOW at/below the ref.
	long := PlanConfirm{Rule: "1x5m_close", RefPrice: 30790, Side: "above"}
	if at, ok := firstSweepOfRef(long, real649Tape(), birth649, 1791941040000); !ok || at != 1791940380000 {
		t.Fatalf("long sweep = first closed bar with low ≤ ref (20:13 low 30785.5): at=%d ok=%v", at, ok)
	}
	if _, ok := firstSweepOfRef(PlanConfirm{Rule: "1x5m_close", RefPrice: 30790, Side: ""}, real649Tape(), birth649, 1791941040000); ok {
		t.Fatal("an unknown side has no sweep side — fail-closed")
	}
}
