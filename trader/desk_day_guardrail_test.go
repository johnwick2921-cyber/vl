package trader

import (
	"os"
	"strings"
	"testing"

	"vl/store"
)

// TestDeskGuardrailRequiresBOTHToggles — class 82, caught before it bit.
//
// The daily-loss gate is kernel/risk_limits.go:309:
//
//	if g.DailyLossEnabled && g.DailyLossLimitUSD > 0 && g.DailyRealizedPnL <= -g.DailyLossLimitUSD
//
// TWO toggles must be true: the guardrails master (which gates the whole block)
// and daily_loss_enabled (which gates this leg). deskGuardrail checked only the
// master — so the moment the owner turns the master ON and leaves the leg's own
// toggle off, the desk strip would report the limit ENFORCED while the gate did
// nothing at all. A green word answering a narrower question than the reader
// will assume.
//
// The live config today has BOTH off, with daily_loss_limit_usd = 450.
func TestDeskGuardrailRequiresBOTHToggles(t *testing.T) {
	mk := func(master, leg *bool) *AutoTrader {
		cfg := &store.StrategyConfig{}
		cfg.RiskControl.GuardrailsEnabled = master
		cfg.RiskControl.DailyLossEnabled = leg
		cfg.RiskControl.DailyLossLimitUSD = 450
		at := &AutoTrader{}
		at.config.StrategyConfig = cfg
		return at
	}
	if _, _, enforced := mk(boolp(false), boolp(false)).deskGuardrail(); enforced {
		t.Error("both toggles off reported ENFORCED")
	}
	if _, _, enforced := mk(boolp(false), boolp(true)).deskGuardrail(); enforced {
		t.Error("master off reported ENFORCED")
	}
	if _, src, enforced := mk(boolp(true), boolp(false)).deskGuardrail(); enforced {
		t.Errorf("master ON but daily_loss_enabled OFF reported ENFORCED (source %q) — "+
			"kernel/risk_limits.go:309 requires BOTH, so this strip would tell the owner a limit "+
			"is live while the gate ignores it", src)
	}
	if _, _, enforced := mk(boolp(true), boolp(true)).deskGuardrail(); !enforced {
		t.Error("both toggles ON must report ENFORCED")
	}
	// r22 RISK-GATES (owner R4 + R5): an ABSENT toggle reads OFF — the same
	// default kernel.ResolveStrategyGuardrails resolves. Since r22 the gate
	// flattens, so it must never turn itself on for a strategy that never
	// saved the switch.
	if _, _, enforced := mk(boolp(true), nil).deskGuardrail(); enforced {
		t.Error("master ON with daily_loss_enabled ABSENT must read OFF (absent = OFF since r22)")
	}
	if _, _, enforced := mk(nil, boolp(true)).deskGuardrail(); enforced {
		t.Error("guardrails master ABSENT must read OFF (absent = OFF since r22)")
	}
}

// The DAY line must name BOTH toggles in plain words, with the value, so the
// owner reads what to click rather than "soft-audit only".
func TestDeskDayNamesBothTogglesInPlainWords(t *testing.T) {
	cfg := &store.StrategyConfig{}
	cfg.RiskControl.GuardrailsEnabled = boolp(false)
	cfg.RiskControl.DailyLossEnabled = boolp(false)
	cfg.RiskControl.DailyLossLimitUSD = 450
	at := &AutoTrader{}
	at.config.StrategyConfig = cfg
	_, _, enforced := at.deskGuardrail()
	if enforced {
		t.Fatal("fixture should be unenforced")
	}
	// WIRED, not merely written: the first draft of this pin called
	// deskDailyLimitText directly, so reverting deskDay to the old
	// "soft-audit only" literal left it GREEN. Built is not wired.
	src, err := os.ReadFile("desk_facts.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	fn := strings.Index(body, "func (at *AutoTrader) deskDay(")
	if fn < 0 {
		t.Fatal("deskDay not found")
	}
	end := strings.Index(body[fn+1:], "\nfunc ")
	if end < 0 {
		end = len(body) - fn - 1
	}
	if !strings.Contains(body[fn:fn+end], "deskDailyLimitText(") {
		t.Error("deskDay does not call deskDailyLimitText — the DAY line still carries its own literal, " +
			"so the plain-words text exists but nothing prints it")
	}

	txt := deskDailyLimitText(at, 450)
	for _, want := range []string{"450", "guardrails master OFF", "daily_loss_enabled OFF", "decorative"} {
		if !strings.Contains(strings.ToLower(txt), strings.ToLower(want)) {
			t.Errorf("the DAY line does not say %q — the owner cannot see which switch to move.\n  got: %s", want, txt)
		}
	}
}
