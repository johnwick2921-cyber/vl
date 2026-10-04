package trader

import (
	"strings"
	"time"

	"vl/kernel/mentor"
	"vl/market"
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
	"open_stop", "open_side", "leg_protection",
	"latest_price", "now", "confluence", "arm_expiry",
}

// mentorSeamBound reports whether one named seam is non-nil (the truth the
// boot line and the refusal read — test injection counts as bound).
func mentorSeamBound(name string) bool {
	switch name {
	case "open_stop":
		return mentorOpenStopSource != nil
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
//	arm expiry — store.SetArmExpiry (dev via #313).
func (at *AutoTrader) mentorWireProductionSeams() {
	if pr, ok := at.trader.(mentorPositionReader); ok {
		mentorOpenStopSource = func() (float64, bool) {
			orders, err := pr.GetOpenOrders("MNQ")
			if err != nil {
				return 0, false
			}
			for _, o := range orders {
				if o.Type == "STOP_MARKET" && o.StopPrice > 0 {
					return o.StopPrice, true
				}
			}
			return 0, false
		}

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
