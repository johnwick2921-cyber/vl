package agent

import (
	"encoding/json"
	"testing"

	"vl/store"
)

// P&L-TRUTH WAVE — F4: the AgentBeta trade tool returns the strict shape
// (figure + resolved n + unresolved count) and it round-trips through JSON.
func TestPnlTruthTradeHistoryToolStrictShape(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	rows := []*store.TraderPosition{
		{ID: 1, Symbol: "MNQ", Side: "LONG", EntryPrice: 29000, ExitPrice: 29050, Quantity: 1, RealizedPnL: 100, PnlCorrected: f(100), CloseReason: "sync", EntryTime: 1_756_700_000_000, ExitTime: 1_756_700_060_000},
		{ID: 2, Symbol: "MNQ", Side: "SHORT", EntryPrice: 29459, ExitPrice: 0, Quantity: 1, RealizedPnL: 0, PnlCorrected: nil, CloseReason: store.CloseReasonUnresolved, EntryTime: 1_756_700_100_000, ExitTime: 1_756_700_160_000},
		{ID: 3, Symbol: "MNQ", Side: "LONG", EntryPrice: 29000, ExitPrice: 28950, Quantity: 1, RealizedPnL: -1458, PnlCorrected: nil, CloseReason: "sync", EntryTime: 1_756_700_200_000, ExitTime: 1_756_700_260_000}, // row-526 shape
		{ID: 4, Symbol: "MNQ", Side: "LONG", EntryPrice: 29000, ExitPrice: 29003, Quantity: 1, RealizedPnL: 6, PnlCorrected: f(6), CloseReason: store.CloseReasonTestSeam, EntryTime: 1_756_700_300_000, ExitTime: 1_756_700_360_000},
	}
	payload := buildTradeHistory([]tradeHistorySource{{Name: "hoang", TID: "8d5c8af5", Positions: rows}}, 10)
	if payload == nil {
		t.Fatal("payload")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Trades []struct {
			ID       int64    `json:"id"`
			PnL      *float64 `json:"pnl"`
			Resolved bool     `json:"resolved"`
		} `json:"trades"`
		Summary struct {
			TotalTrades        int     `json:"total_trades"`
			ResolvedTrades     int     `json:"resolved_trades"`
			UnresolvedExcluded int     `json:"unresolved_excluded"`
			TotalPnL           float64 `json:"total_pnl"`
			WinRate            string  `json:"win_rate"`
			PnLColumn          string  `json:"pnl_column"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Summary.ResolvedTrades != 1 || back.Summary.TotalTrades != 1 || back.Summary.UnresolvedExcluded != 2 || back.Summary.TotalPnL != 100 {
		t.Fatalf("summary must be +100 over 1 resolved with 2 unresolved excluded (test-seam quarantined), got %+v", back.Summary)
	}
	if back.Summary.WinRate != "100.0%" || back.Summary.PnLColumn == "" {
		t.Fatalf("summary: %+v", back.Summary)
	}
	if len(back.Trades) != 3 {
		t.Fatalf("test-seam row must be quarantined, got %d rows", len(back.Trades))
	}
	for _, tr := range back.Trades {
		switch tr.ID {
		case 1:
			if !tr.Resolved || tr.PnL == nil || *tr.PnL != 100 {
				t.Fatalf("resolved row: %+v", tr)
			}
		case 2, 3:
			if tr.Resolved || tr.PnL != nil {
				t.Fatalf("unresolved row must carry pnl=null, resolved=false (never raw −1458): %+v", tr)
			}
		}
	}
}

// R22 PNL-TRUTH (owner ruling R1): a manual NT8 row is LISTED (origin=manual)
// but excluded from the bot summary, which carries it as its own line.
func TestTradeHistoryToolExcludesManualRowsFromTheBotSummary(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	rows := []*store.TraderPosition{
		{ID: 636, Symbol: "MNQ", Side: "SHORT", Quantity: 5, PnlCorrected: f(-129), CloseReason: "stop", ExitTime: 1_791_380_000_000},
		{ID: 640, Symbol: "MNQ", Side: "SHORT", Quantity: 20, PnlCorrected: f(-1127.5), CloseReason: "manual", Origin: store.OriginManual, ExitTime: 1_791_467_480_000},
	}
	payload := buildTradeHistory([]tradeHistorySource{{Name: "hoang", TID: "8d5c8af5", Positions: rows}}, 10)
	sum := payload["summary"].(map[string]any)
	if sum["total_trades"].(int) != 1 || sum["total_pnl"].(float64) != -129 {
		t.Fatalf("bot summary = %v trades %v, want 1 / −129 (manual #640 excluded)", sum["total_trades"], sum["total_pnl"])
	}
	if sum["manual_excluded_trades"].(int) != 1 || sum["manual_excluded_pnl"].(float64) != -1127.5 {
		t.Fatalf("manual line = %v / %v, want 1 / −1127.50", sum["manual_excluded_trades"], sum["manual_excluded_pnl"])
	}
	trades := payload["trades"].([]map[string]any)
	if len(trades) != 2 {
		t.Fatalf("both rows stay LISTED, got %d", len(trades))
	}
	for _, tr := range trades {
		if tr["id"].(int64) == 640 && tr["origin"] != store.OriginManual {
			t.Fatalf("#640 must be listed with origin=manual, got %v", tr["origin"])
		}
	}
}
