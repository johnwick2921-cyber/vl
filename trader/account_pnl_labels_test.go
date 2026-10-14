package trader

import "testing"

// RISK-PNL P1-2 — the dashboard "Total P&L" card showed total_pnl, which for
// an NT8 account is NT8's SESSION realized+unrealized (it resets each session:
// +74.00 on 10-14 = #648 + #651) while its tooltip said "equity − initial"
// (47,695 − 50,000 = −2,305). The account figures now ride under their own
// names so a label can never again describe a different number. total_pnl
// keeps its value for its other readers and says what it holds.
func TestGetAccountInfoLabelsEveryAccountFigure(t *testing.T) {
	m := &MockTrader{balanceReady: true, balance: map[string]interface{}{
		"totalWalletBalance": 47695.0, "totalEquity": 47695.0, "availableBalance": 47695.0,
		"totalRealizedProfit": 74.0, "totalUnrealizedProfit": 0.0, "brokerNativePnL": true,
	}}
	at := &AutoTrader{trader: m, initialBalance: 50000}
	info, err := at.GetAccountInfo()
	if err != nil {
		t.Fatal(err)
	}
	if got := info["nt8_session_pnl"]; got != 74.0 {
		t.Errorf("nt8_session_pnl = %v, want 74 (NT8's own session figure)", got)
	}
	if got := info["account_pnl_vs_initial"]; got != -2305.0 {
		t.Errorf("account_pnl_vs_initial = %v, want −2305 (equity 47,695 − initial 50,000)", got)
	}
	if got := info["total_pnl_source"]; got != "nt8_session" {
		t.Errorf("total_pnl_source = %v, want nt8_session — total_pnl holds the NT8 session figure here", got)
	}
	if got := info["total_pnl"]; got != 74.0 {
		t.Errorf("total_pnl = %v, want 74 (value unchanged for its other readers)", got)
	}

	// A broker without its own P&L: total_pnl IS equity − initial, and there
	// is no NT8 session figure to show (absent, never a fabricated 0).
	m.balance = map[string]interface{}{"totalWalletBalance": 1000.0, "totalEquity": 1100.0}
	at.initialBalance = 1000
	info, _ = at.GetAccountInfo()
	if _, ok := info["nt8_session_pnl"]; ok {
		t.Errorf("nt8_session_pnl must be ABSENT when the broker reports no session P&L, got %v", info["nt8_session_pnl"])
	}
	if info["total_pnl_source"] != "equity_minus_initial" || info["account_pnl_vs_initial"] != 100.0 {
		t.Errorf("source=%v vs_initial=%v, want equity_minus_initial / 100", info["total_pnl_source"], info["account_pnl_vs_initial"])
	}

	// No initial balance: the vs-initial figure is UNKNOWN, so it is absent.
	at.initialBalance = 0
	info, _ = at.GetAccountInfo()
	if _, ok := info["account_pnl_vs_initial"]; ok {
		t.Errorf("account_pnl_vs_initial must be ABSENT without an initial balance, got %v", info["account_pnl_vs_initial"])
	}
}
