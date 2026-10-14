package api

import (
	"path/filepath"
	"testing"

	"vl/manager"
	"vl/store"
)

// RT-7 (release #24, N1) — TWO "BOT RECORD" FIGURES, EACH SAYS ITS SCOPE. The
// dashboard's "Bot P&L (record)" card (/api/account?account=…) is scoped to
// the selected account; Position History's total is trader-wide. Which scope
// is the record is an owner decision, so no figure changes: the scoped card
// carries the bot rows it leaves out, COUNTED from the same reader (the live
// shape: hoang's 16 bot rows, −547.00, on the other account, ids 383–489),
// and the unscoped read says "all accounts". MUTANT: drop the other-accounts
// count → RED.
func TestBotRecordCardSaysItsScopeAndCountsOtherAccounts(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "rt7.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := NewServer(manager.NewTraderManager(), st, nil, "127.0.0.1", 0)
	base := int64(1_790_000_000_000)
	row := func(id int64, acct string, pnl float64, origin string) {
		p := &store.TraderPosition{ID: id, TraderID: "rt7", Account: acct, Symbol: "MNQ", Side: "LONG",
			Quantity: 1, EntryPrice: 100, ExitPrice: 100 + pnl/2, RealizedPnL: pnl, PnlCorrected: &pnl,
			Status: "CLOSED", CloseReason: "sync", Origin: origin,
			EntryTime: base + id*60_000 - 30_000, ExitTime: base + id*60_000, CreatedAt: base, UpdatedAt: base}
		if err := st.GormDB().Create(p).Error; err != nil {
			t.Fatal(err)
		}
	}
	row(1, "Sim101", -100, "")
	row(2, "Sim101", 40, "")
	row(3, "Sim101", -3202, store.OriginManual)
	row(383, "SimAccount1", -500, "")
	row(489, "SimAccount1", -47, "")

	scoped := map[string]interface{}{}
	s.attachBotRecord(scoped, "rt7", "Sim101")
	if scoped["bot_record_pnl"] != -60.0 || scoped["bot_record_trades"] != 2 {
		t.Fatalf("the scoped figure itself must not change (−60 over 2), got %v / %v", scoped["bot_record_pnl"], scoped["bot_record_trades"])
	}
	if scoped["bot_record_scope"] != "this account" || scoped["bot_record_other_accounts_trades"] != 2 || scoped["bot_record_other_accounts_pnl"] != -547.0 {
		t.Fatalf("the scoped card must say its scope and COUNT the 2 bot trades (−547.00) on other accounts, got scope=%v trades=%v pnl=%v",
			scoped["bot_record_scope"], scoped["bot_record_other_accounts_trades"], scoped["bot_record_other_accounts_pnl"])
	}
	wide := map[string]interface{}{}
	s.attachBotRecord(wide, "rt7", "")
	if wide["bot_record_scope"] != "all accounts" || wide["bot_record_pnl"] != -607.0 || wide["bot_record_trades"] != 4 {
		t.Fatalf("the trader-wide read must say all accounts (−607 over 4), got scope=%v pnl=%v trades=%v", wide["bot_record_scope"], wide["bot_record_pnl"], wide["bot_record_trades"])
	}
	if _, has := wide["bot_record_other_accounts_trades"]; has {
		t.Fatal("a trader-wide read has no other accounts: the field must be absent, not a fabricated 0")
	}
}
