package trader

// W2 — boot-backfill idempotence + the WEEKLY plan-row storage contract.
// No network, no LLM: the scheduler verdict is a pure function and the
// storage path is exercised through the real plan store.

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/store"
)

func TestWeeklyReadVerdictBootBackfillIdempotent(t *testing.T) {
	dl := time.Date(2026, 8, 30, 16, 30, 0, 0, kernel.CTLocation()) // Sunday read time

	// before the deadline → wait (nothing happens on a pre-read boot).
	if got := weeklyReadVerdict(dl.Add(-time.Hour), dl, false); got != "wait" {
		t.Fatalf("proving line: before read time → wait, got %q", got)
	}
	// Monday boot past the deadline with no doc → READ exactly once (backfill).
	if got := weeklyReadVerdict(dl.Add(24*time.Hour), dl, false); got != "read" {
		t.Fatalf("proving line: boot-backfill fires on a post-deadline boot — got %q", got)
	}
	// same instant, doc now exists → skip forever (idempotent; never re-run).
	if got := weeklyReadVerdict(dl.Add(24*time.Hour), dl, true); got != "skip" {
		t.Fatalf("proving line: stored doc → skip (idempotent) — got %q", got)
	}
}

// TestWeeklyPlanRowStorageContract proves the W2 storage shape: one plans row
// with session='WEEKLY', trade_date = the governing Monday, the doc JSON
// carrying facts_hash — and that a second lookup finds it (the idempotence
// gate the scheduler checks).
func TestWeeklyPlanRowStorageContract(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	now := time.Date(2026, 8, 30, 17, 0, 0, 0, kernel.CTLocation())
	monday := kernel.WeekGoverningMonday(now).Format("2006-01-02")
	if monday != "2026-08-31" {
		t.Fatalf("proving line: Sunday 17:00 CT belongs to the FOLLOWING Monday — got %s", monday)
	}
	doc := kernel.WeeklyDoc{
		Bias: "bull", Conviction: "high",
		Draw:         kernel.WeeklyDraw{Name: "PWH", Px: 30500.25},
		Invalidation: kernel.WeeklyInvalidation{Px: 30300.00, Basis: "1h close beyond 30300.00"},
		WeeklyLevels: []kernel.WeeklyLevel{{Name: "PWH", Px: 30500.25}},
		Narrative:    "holding above the open",
		FactsHash:    "deadbeef",
	}
	docJSON, _ := json.Marshal(&doc)
	version, err := st.Plan().AppendPlan(&store.PlanDB{
		PlanID:     st.Plan().ResolvePlanID(monday, "WEEKLY", "trader-1"),
		StrategyID: "trader-1", TradeDate: monday, Session: "WEEKLY",
		TriggerReason: "sunday_weekly_read", Lifecycle: "active",
		ModelID: "deepseek-v4-pro", PromptHash: "deadbeef", Doc: string(docJSON),
	})
	if err != nil || version != 1 {
		t.Fatalf("proving line: weekly row appended v1 — err=%v", err)
	}
	row, err := st.Plan().GetLatestPlanForTraderSession(monday, "WEEKLY", "trader-1")
	if err != nil || row == nil {
		t.Fatalf("proving line: weekly row lookup round-trips — err=%v", err)
	}
	var got kernel.WeeklyDoc
	if json.Unmarshal([]byte(row.Doc), &got) != nil || got.Bias != "bull" || got.FactsHash != "deadbeef" {
		t.Fatalf("proving line: doc JSON round-trips with facts_hash — got %+v", got)
	}
	if row.Session != "WEEKLY" || row.TradeDate != monday || row.Lifecycle != "active" {
		t.Fatalf("proving line: row identity (session/trade_date/lifecycle) — got %+v", row)
	}
}
