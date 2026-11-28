package trader

import (
	"path/filepath"
	"testing"

	"vl/store"
)

// m421a C1 — GetPositions (the /api/positions repackager) must pass the
// broker's UI-only keys through when present, and attach the real open time
// from the trader_positions row; absent stays absent (never fabricated).
func TestGetPositionsPassesBracketPointValueAndEntryTime(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Position().Create(&store.TraderPosition{
		TraderID: "td1", Account: "Sim101", Symbol: "MNQ", Side: "LONG",
		Quantity: 1, EntryPrice: 21500, Status: "OPEN", EntryTime: 1_760_000_000_000, CreatedAt: 1, UpdatedAt: 1,
	}); err != nil {
		t.Fatal(err)
	}
	bracket := map[string]interface{}{"stop": 21480.0, "target": 21540.0}
	at := &AutoTrader{id: "td1", store: st, trader: &stubTrader{positions: []map[string]interface{}{
		{"symbol": "MNQ", "side": "LONG", "entryPrice": 21500.0, "markPrice": 21510.0, "positionAmt": 1.0,
			"bracket": bracket, "point_value": 2.0, "position_value": 43020.0},
		{"symbol": "ES", "side": "SHORT", "entryPrice": 5000.0, "markPrice": 5000.0, "positionAmt": -1.0},
	}}}
	got, err := at.GetPositions()
	if err != nil || len(got) != 2 {
		t.Fatalf("GetPositions = %v, %v", got, err)
	}
	m := got[0]
	if m["bracket"] == nil || m["point_value"] != 2.0 || m["position_value"] != 43020.0 {
		t.Fatalf("bracket/point_value/position_value dropped: %+v", m)
	}
	if m["entry_time"] != int64(1_760_000_000_000) {
		t.Fatalf("entry_time = %v, want the stored open time", m["entry_time"])
	}
	e := got[1]
	for _, k := range []string{"bracket", "point_value", "position_value", "entry_time"} {
		if _, ok := e[k]; ok {
			t.Fatalf("%s must stay ABSENT when unknown, got %v", k, e[k])
		}
	}
}
