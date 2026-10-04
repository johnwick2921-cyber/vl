package mentor

import (
	"vl/kernel"
	"vl/market"
)

// ContractsToNotionalUSD expresses a contract count as the notional
// (position_size_usd) the executor's futuresOrderQuantity turns back into
// exactly that count: notional = contracts × price × pointValue. A
// non-positive point value or price returns 0 (the caller refuses before it
// can place anything).
func ContractsToNotionalUSD(symbol string, price float64, contracts int) float64 {
	pv := market.FuturesPointValue(symbol)
	if pv <= 0 || price <= 0 || contracts <= 0 {
		return 0
	}
	return float64(contracts) * price * pv
}

// BuildDecision converts an evaluator intent into a synthetic kernel.Decision
// for the existing executeDecisionWithRecord pipeline (P3: every admission
// gate is reused; the executor's sizing turns the notional back into exactly
// `contracts`). The decision is marked MentorSourced so the mentor-mode
// switches (AI-entry suppression, mentor max-contracts) see it.
func BuildDecision(in Intent, symbol string, contracts int) *kernel.Decision {
	action := "open_long"
	if in.Side == SideShort {
		action = "open_short"
	}
	return &kernel.Decision{
		Symbol:          symbol,
		Action:          action,
		EntryPrice:      in.Price, // stop-entry trigger price (mentor branch)
		StopLoss:        in.Stop,
		TakeProfit:      in.Target,
		PositionSizeUSD: ContractsToNotionalUSD(symbol, in.Price, contracts),
		Leverage:        1,
		Confidence:      80,
		Reasoning:       "mentor: " + in.Reason,
		MentorSourced:   true,
	}
}
