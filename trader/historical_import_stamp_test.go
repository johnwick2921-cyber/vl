package trader

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// m460 (2026-10-30) — THE IMPORT-LABEL PIN, through the production call site.
//
// NT8 stamps every bar at its CLOSE (Bars.GetTime). The bars table is keyed by
// the bar's OPEN. The importer used to store the AddOn's stamp raw, so every
// imported row sat one full timeframe late. This test plays the AddOn: a fake
// client on the real TCPServer answers the importer's bars_history_request with
// CLOSE-stamped frames, exactly as VLHistoryPull.cs does, and the importer
// (ImportNamedContractHistory -> pullOne -> ImportBars) writes a real sqlite
// store. The assertions read the ROWS BACK from the store.
//
// 2026-10-29 is a Thursday (CDT, UTC-5). The 08:30 CT 5m bar opens 13:30 UTC.
func TestImporterOpenStampsEveryRowItWrites(t *testing.T) {
	t.Setenv("HISTORICAL_IMPORT_SEAM", "on")
	t.Setenv("NT_CONNECT_KEY", "") // owner box: the conn is published at accept

	srv := ntwire.NewTCPServer(nil)
	srv.SetAddrForTest("127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	ntTrader.SetTCPServerForTest(srv)

	conn, err := net.Dial("tcp", srv.ListenAddrForTest().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	const contract = "MNQ 12-26"
	open1m := time.Date(2026, 10, 29, 13, 30, 0, 0, time.UTC).UnixMilli() // 08:30 CT
	// AddOn side: for each request, answer with CLOSE-stamped bars (open + tf).
	go func() {
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			if env.Type != ntwire.FrameBarsHistoryRequest {
				continue
			}
			var req ntwire.BarsHistoryRequestPayload
			if json.Unmarshal(env.Payload, &req) != nil {
				return
			}
			step := int64(60_000)
			if req.Timeframe == "5m" {
				step = 300_000
			}
			var bars []ntwire.Bar
			for i := int64(0); i < 3; i++ {
				openMs := open1m + i*step
				bars = append(bars, ntwire.Bar{T: openMs + step, O: 100 + float64(i), H: 101 + float64(i), L: 99, C: 100.5, V: 10 + float64(i)})
			}
			_ = ntwire.WriteFrame(conn, ntwire.FrameBarsHistoryData, ntwire.BarsHistoryDataPayload{
				RequestID: req.RequestID, Symbol: req.Symbol, Contract: contract, Timeframe: req.Timeframe, Seq: 1, Last: true, Bars: bars,
			})
		}
	}()
	// the server must have published the client as its conn before we ask
	deadline := time.Now().Add(5 * time.Second)
	var res []ImportResult
	st, err := store.New(filepath.Join(t.TempDir(), "m460.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.BarHistory().Migrate(); err != nil {
		t.Fatal(err)
	}
	for {
		res, err = ImportNamedContractHistory(ctx, st, "MNQ", contract, []string{"1m", "5m"}, open1m-3_600_000, open1m+3_600_000)
		if err == nil && len(res) == 2 && res[0].Status == "imported" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("import never succeeded: err=%v res=%+v", err, res)
		}
		time.Sleep(50 * time.Millisecond)
	}

	for _, tc := range []struct {
		tf   string
		step int64
	}{{"1m", 60_000}, {"5m", 300_000}} {
		rows, err := st.BarHistory().BarsBetweenOn("MNQ", tc.tf, contract, 0, open1m+3_600_000)
		if err != nil || len(rows) != 3 {
			t.Fatalf("%s: read back %d rows err=%v, want 3", tc.tf, len(rows), err)
		}
		for i, r := range rows {
			want := open1m + int64(i)*tc.step
			if r.OpenTimeMs != want {
				t.Fatalf("%s row %d stored at %s, want the OPEN %s (the importer stored the AddOn's CLOSE stamp raw)",
					tc.tf, i, time.UnixMilli(r.OpenTimeMs).UTC().Format(time.RFC3339), time.UnixMilli(want).UTC().Format(time.RFC3339))
			}
			if r.Convention != store.ConventionEpochFloorOpen || r.Source != store.BarSourceHistoricalImport {
				t.Fatalf("%s row %d tagged %q/%q, want %q/historical_import", tc.tf, i, r.Convention, r.Source, store.ConventionEpochFloorOpen)
			}
		}
	}
	if res[0].FirstBarMs != open1m {
		t.Fatalf("1m FirstBarMs=%d want the open %d", res[0].FirstBarMs, open1m)
	}
}

// The helper alone, for the edge the wire test does not drive: an unsorted
// chunk, no in-place mutation of the AddOn's frame, and parity with the live door.
func TestImportRowsFromChunkMatchesTheLiveDoor(t *testing.T) {
	in := []ntwire.Bar{{T: 1_000_000_000_000 + 120_000, C: 2}, {T: 1_000_000_000_000 + 60_000, C: 1}}
	rows, first, last := importRowsFromChunk("MNQ", "1m", "MNQ 12-26", in)
	if len(rows) != 2 || rows[0].OpenTimeMs != 1_000_000_000_000 || rows[1].OpenTimeMs != 1_000_000_060_000 || first != rows[0].OpenTimeMs || last != rows[1].OpenTimeMs {
		t.Fatalf("rows=%+v first=%d last=%d", rows, first, last)
	}
	if in[0].T != 1_000_000_000_000+120_000 {
		t.Fatalf("the AddOn's chunk was mutated in place")
	}
	// the live door's own output for the same input is the oracle (rows are sorted ascending)
	live := ntwire.OpenStampBars(in, "1m")
	if live[1].T != rows[0].OpenTimeMs || live[0].T != rows[1].OpenTimeMs {
		t.Fatalf("importer and live door disagree: live=%+v rows=%+v", live, rows)
	}
}
