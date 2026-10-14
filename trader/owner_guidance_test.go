package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
)

// R22 R4 (owner ruling): the Studio custom prompt REACHES THE PLANNER. The
// owner's live text ("No trade inside sideway zone … respect the stop loss")
// reached only the executor; the v2 attempt-1 planner prompt (rejected row 747,
// 165,018 chars) carried no "sideway". Production call site: the planner's own
// assembly (assemblePlannerInputWithCtx → kernel.BuildPlannerPrompt), exactly
// the path a scheduled/wake read renders.
const ownerStudioText = "No trade inside sideway zone. Wait for a clean break. Always respect the stop loss."

func TestStudioPromptReachesThePlannerAssembly(t *testing.T) {
	restore := a3WireBars()
	defer restore()
	at := plannerTestTrader(t)
	at.config.NinjaTraderSymbol = "MNQ"
	at.config.StrategyConfig.CustomPrompt = ownerStudioText
	if at.strategyEngine != nil {
		at.strategyEngine.GetConfig().CustomPrompt = ownerStudioText
	}
	prompt := kernel.BuildPlannerPrompt(at.assemblePlannerInputWithCtx(time.Now(), "ASIA", "2026-09-07", "", nil))
	for _, frag := range []string{"## OWNER GUIDANCE", "<<<OWNER_GUIDANCE\n" + ownerStudioText + "\nOWNER_GUIDANCE>>>", "It never overrides the machine rules"} {
		if !strings.Contains(prompt, frag) {
			t.Fatalf("the planner prompt must carry the owner's Studio prompt, delimited: missing %q", frag)
		}
	}
	if g, ok := kernel.OwnerGuidanceInPrompt(prompt); !ok || g != ownerStudioText {
		t.Fatalf("the delimited block must round-trip the exact text: ok=%v %q", ok, g)
	}
	// No Studio prompt → no block at all (absent, never an empty header).
	at.config.StrategyConfig.CustomPrompt = ""
	if at.strategyEngine != nil {
		at.strategyEngine.GetConfig().CustomPrompt = ""
	}
	if p := kernel.BuildPlannerPrompt(at.assemblePlannerInputWithCtx(time.Now(), "ASIA", "2026-09-07", "", nil)); strings.Contains(p, "OWNER GUIDANCE") {
		t.Fatal("an empty Studio prompt must render no owner-guidance block")
	}
}

// Every plan write logs the guidance LENGTH and a short HASH read from the
// prompt the planner was sent — never the text.
func TestStudioPromptFingerprintLoggedOnPlanWrite(t *testing.T) {
	at := feasPlannerTrader(t, nil)
	feasStubBars(t)
	at.config.NinjaTraderSymbol = "MNQ"
	at.config.StrategyConfig.CustomPrompt = ownerStudioText
	if at.strategyEngine != nil {
		at.strategyEngine.GetConfig().CustomPrompt = ownerStudioText
	}
	prompt := kernel.BuildPlannerPrompt(at.assemblePlannerInputWithCtx(time.Now(), "NY", "2026-08-14", "", nil))
	logBuf := captureTraderLog(t)
	green := strings.ReplaceAll(infeasibleFeasPlanJSON, `"target":15620`, `"target":15630`)
	green = strings.Replace(green, `"target_chain": [15550, 15620]`, `"target_chain": [15550, 15630]`, 1)
	green = strings.Replace(green, `"r_to_arm_target":7.0`, `"r_to_arm_target":8.0`, 1)
	_, lc, err := at.runPlannerReadCoreWithFactsGradesClock(feasClock(), "NY", "2026-08-14", "owner_reset",
		"deepseek-v4-pro", "hashOwner1", "", "", "", prompt,
		kernel.PlanFacts{Price: 15550, DATR: 300}, nil, map[float64]string{15480: "PWL", 15620: "PDH"}, nil, true,
		func(string) (string, error) { return green, nil })
	if err != nil || lc != "active" {
		t.Fatalf("write: lc=%q err=%v\nlog:\n%s", lc, err, logBuf.String())
	}
	log := logBuf.String()
	want := "🗣️ owner guidance in the planner prompt for 2026-08-14 NY v1: " + kernel.OwnerGuidanceFingerprint(prompt)
	if !strings.Contains(log, want) || !strings.Contains(want, "len=83 chars sha256=") {
		t.Fatalf("the plan write must log the guidance length + hash (%q):\n%s", want, log)
	}
	if strings.Contains(log, "sideway") {
		t.Fatalf("the guidance TEXT must never be logged:\n%s", log)
	}
}
