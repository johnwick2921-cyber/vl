package trader

import (
	"strings"
	"time"

	"vl/kernel"
	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
	"vl/trader/types"
)

// ── P0 PRODUCTION WIRING (CTO 1791039541371) ───────────────────────────────
//
// One function, called at trader start when mentor_mode is ON, binds every
// mentor seam to its REAL production source. The tests inject these seams;
// production must not depend on that. A missing binding REFUSES entries,
// named (mentorSeamMissing, consumed by the placement gate).

// mentorPositionReader is the production surface the wiring reads: the bound
// account's position snapshot and the working (bracket) orders. The NT8 TCP
// trader satisfies it (types.Trader has both methods).
type mentorPositionReader interface {
	GetPositions() ([]map[string]interface{}, error)
	GetOpenOrders(symbol string) ([]types.OpenOrder, error)
}

// mentorSeamNames is the canonical seam order for the boot line and refusals.
var mentorSeamNames = []string{
	"open_side", "leg_protection",
	"latest_price", "now", "confluence", "arm_expiry",
	"day_net", "closed_profit",
}

// mentorSeamBound reports whether one named seam is non-nil (the truth the
// boot line and the refusal read — test injection counts as bound).
func mentorSeamBound(name string) bool {
	switch name {
	case "open_side":
		return mentorOpenSideSource != nil
	case "leg_protection":
		return mentorLegProtectedSource != nil
	case "latest_price":
		return mentorLatestPriceSource != nil
	case "now":
		return mentorNowSource != nil
	case "confluence":
		return mentorConfluenceForIntent != nil
	case "arm_expiry":
		return mentorSetArmExpiryWire != nil
	case "day_net":
		return mentorDayNetSource != nil
	case "closed_profit":
		return mentorClosedProfitSource != nil
	}
	return false
}

// mentorWireProductionSeams binds every seam to its REAL source:
//
//	open stop / open side — the bound account's bracket snapshot
//	  (the STOP_MARKET working order and the position snapshot);
//	latest price — the BarCache's last 1m close;
//	now — time.Now;
//	leg protection — a working bracket stop at the broker;
//	confluence — the evaluator intent's Confluence flag;
//	arm expiry — store.SetArmExpiry (dev via #313);
//	day net / closed profit — the strict-corrected session-day P&L read
//	  (pnl_corrected only; a NULL row → unresolved → fail closed).
func (at *AutoTrader) mentorWireProductionSeams() {
	if pr, ok := at.trader.(mentorPositionReader); ok {
		mentorOpenSideSource = func() string {
			pos, err := pr.GetPositions()
			if err != nil {
				return ""
			}
			for _, p := range pos {
				switch strings.ToLower(strings.TrimSpace(p["side"].(string))) {
				case "long":
					return "long"
				case "short":
					return "short"
				}
			}
			return ""
		}

		mentorLegProtectedSource = func(leg string) bool {
			orders, err := pr.GetOpenOrders("MNQ")
			if err != nil {
				return false
			}
			for _, o := range orders {
				if o.Type == "STOP_MARKET" && o.StopPrice > 0 {
					return true
				}
			}
			return false
		}
	}

	mentorLatestPriceSource = func() (float64, bool) {
		if market.FuturesBarsProvider == nil {
			return 0, false
		}
		bars := market.FuturesBarsProvider("MNQ", "1m", 1)
		if len(bars) == 0 {
			return 0, false
		}
		return bars[len(bars)-1].Close, true
	}

	mentorNowSource = func() time.Time { return time.Now() }

	mentorConfluenceForIntent = func(in mentor.Intent) bool { return in.Confluence }

	// B1 (DS-104): day net + closed profit — the closed-trade P&L read
	// (pnl_corrected only). A NULL pnl_corrected closed row today is
	// UNRESOLVED → the seam returns ok=false → the gate fails CLOSED.
	//
	// FIX-DONE-AFTER-WIN-NY-DAY (owner ruling 2026-10-09): done-after-win reads
	// the NY day (mentor_done_after_win_day_start, default 08:30 CT). Stop-after
	// -loss keeps the CME session day (17:00 CT) — UNCHANGED.
	if at.store != nil {
		ps := at.store.Position()
		doneAfterWinActivity := func() (store.MentorDayActivity, bool) {
			start := at.mentorDoneAfterWinDayStart()
			day, ok := mentorDayStartAt(start, mentorClockNow())
			if !ok {
				at.logWarnf("🧑‍🏫 mentor done-after-win day-start %q unparseable — refusing (fail-closed)", start)
				return store.MentorDayActivity{}, false
			}
			act, err := ps.MentorDayActivity(at.id, day.UnixMilli(), at.currentAccountName())
			if err != nil {
				at.logWarnf("🧑‍🏫 mentor done-after-win day read failed (%v) — refusing (fail-closed)", err)
				return store.MentorDayActivity{}, false
			}
			if act.Unresolved > 0 {
				at.logWarnf("🧑‍🏫 mentor done-after-win day UNRESOLVED (%d closed row(s) with NULL pnl_corrected today) — refusing (fail-closed)", act.Unresolved)
				return store.MentorDayActivity{}, false
			}
			return act, true
		}
		lossDayActivity := func() (store.MentorDayActivity, bool) {
			sinceMs := kernel.CMESessionDayStart(mentorClockNow()).UnixMilli()
			act, err := ps.MentorDayActivity(at.id, sinceMs, at.currentAccountName())
			if err != nil {
				at.logWarnf("🧑‍🏫 mentor stop-after-loss day read failed (%v) — refusing (fail-closed)", err)
				return store.MentorDayActivity{}, false
			}
			if act.Unresolved > 0 {
				at.logWarnf("🧑‍🏫 mentor stop-after-loss day UNRESOLVED (%d closed row(s) with NULL pnl_corrected today) — refusing (fail-closed)", act.Unresolved)
				return store.MentorDayActivity{}, false
			}
			return act, true
		}
		mentorDayNetSource = func() (float64, bool) {
			act, ok := doneAfterWinActivity()
			if !ok {
				return 0, false
			}
			return act.DayNetPnl, true
		}
		mentorClosedProfitSource = func() (bool, bool) {
			act, ok := doneAfterWinActivity()
			if !ok {
				return false, false
			}
			return act.ClosedInProfit, true
		}
		mentorClosedLossSource = func() (bool, bool) {
			act, ok := lossDayActivity()
			if !ok {
				return false, false
			}
			return act.ClosedInLoss, true
		}
		// B3 day-stop-sweep hook: trips ONLY on a DEFINITE loss (the placement
		// gate is fail-closed; the sweep never force-cancels on an unknown).
		mentorStopAfterLossTripped = at.mentorStopAfterLossTrip
	}

	if at.store != nil && at.store.ArmedOrders() != nil {
		arms := at.store.ArmedOrders()
		mentorSetArmExpiryWire = func(armID int64, expiryMs int64) error {
			return arms.SetArmExpiry(armID, expiryMs)
		}
	}
}

// mentorSeamBootLine is the one boot line: every seam, wired or missing.
func mentorSeamBootLine() string {
	parts := make([]string, 0, len(mentorSeamNames))
	for _, n := range mentorSeamNames {
		s := "missing"
		if mentorSeamBound(n) {
			s = "wired"
		}
		parts = append(parts, n+"="+s)
	}
	return "🧑‍🏫 mentor seams: " + strings.Join(parts, " ")
}

// mentorSeamMissing names every seam the placement requires that is unbound —
// a missing one REFUSES entries, named.
func mentorSeamMissing() []string {
	var missing []string
	for _, n := range mentorSeamNames {
		if n == "now" {
			continue // mentorClockNow falls back to time.Now — never missing
		}
		if !mentorSeamBound(n) {
			missing = append(missing, n)
		}
	}
	return missing
}
