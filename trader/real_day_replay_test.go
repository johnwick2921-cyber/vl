package trader

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// rdrAddonBuild is the AddOn build the live bot ran over 10-20/21 (far-side
// build_id=2026-10-16-a, read from the live log).
const rdrAddonBuild = "2026-10-16-a"

// ── REAL-DAY REPLAY (m157) ─────────────────────────────────────────────────
//
// TestRealDayReplay replays the LIVE window 10-20 17:00 CT → 10-21 now through
// the PRODUCTION mentor path (the same drive as TestFullDayReplay: the TCP
// server, the fake AddOn, the armed ledger, the event pass) and diffs the
// authored arms against the live record (trader_positions 663–675 +
// armed_orders).
//
// Run:
//
//	REAL_DAY_REPLAY=/tmp/realday go test ./trader/ -run TestRealDayReplay -v
//
// The dir must contain (all exported read-only from the live DB):
//   - mnq_realday.json.gz   {bars:{1m:[[ot,o,h,l,c,contract]…],1h:[…]}}
//   - strategy_a5b7662e.json  the live strategy config JSON
//   - live_record.json     {trades:[…], arms:[…]}
//
// HONEST LIMITS (do not fake parity):
//   - fills are SIMULATED: the replay's ledger row flips to `filled` on a
//     candle touching the entry, not on a broker receipt; NT8 fill/ack latency
//     and partial fills are not modeled;
//   - the replay bar clock runs at replay speed, so any wall-clock-dependent
//     path (entry-latch evidence age, C8 sweep) is played at replay cadence;
//   - the replay calendar is EMPTY (no red-folder news windows) unless a
//     calendar fixture is added — a news day would freeze the 1h/4h triggers
//     differently;
//   - BE moves and the exit drive are the PRODUCTION kernel, but their inputs
//     are the simulated fills, so an exit time may shift a bar.
type rdrLiveTrade struct {
	ID       int     `json:"id"`
	Side     string  `json:"side"`
	Entry    float64 `json:"entry"`
	EntryMs  int64   `json:"entry_ms"`
	Exit     float64 `json:"exit"`
	ExitMs   int64   `json:"exit_ms"`
	PnL      float64 `json:"pnl"`
	Reason   string  `json:"reason"`
	TraderID string  `json:"trader"`
}

type rdrLiveArm struct {
	ID       int     `json:"id"`
	Scenario string  `json:"scenario"`
	Side     string  `json:"side"`
	Entry    float64 `json:"entry"`
	Stop     float64 `json:"stop"`
	Target   float64 `json:"target"`
	State    string  `json:"state"`
	Created  string  `json:"created"`
}

type rdrLiveRecord struct {
	Trades   []rdrLiveTrade `json:"trades"`
	Arms     []rdrLiveArm   `json:"arms"`
	Calendar []rdrLiveCal   `json:"calendar"`
}

type rdrLiveCal struct {
	Date   string `json:"date"`
	Source string `json:"source"`
	Events string `json:"events"`
}

// rdrReplayArm is one armed_orders row the replay authored, at its terminal
// state when the drive ended.
type rdrReplayArm struct {
	Side   string
	Entry  float64
	Stop   float64
	Target float64
	State  string
	BornCT string
}

func rdrDir(t *testing.T) string {
	d := os.Getenv("REAL_DAY_REPLAY")
	if d == "" {
		t.Skip("REAL_DAY_REPLAY=<dir> not set (the real-day replay is opt-in)")
	}
	return d
}

// loadRealDayFixture loads the real-day bar fixture (same shape as
// loadFullDayFixture, but from the env dir and with 1m + 1h only).
func loadRealDayFixture(t *testing.T, dir string) ([]fixtureBar, []fixtureBar) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "mnq_realday.json.gz"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()
	var doc struct {
		Bars map[string][][]json.RawMessage `json:"bars"`
	}
	if err := json.NewDecoder(gz).Decode(&doc); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	parse := func(tf string, durMs int64) []fixtureBar {
		rows := doc.Bars[tf]
		out := make([]fixtureBar, 0, len(rows))
		for _, r := range rows {
			if len(r) < 6 {
				continue
			}
			var ot, o, h, l, c float64
			var contract string
			if json.Unmarshal(r[0], &ot) != nil || json.Unmarshal(r[1], &o) != nil ||
				json.Unmarshal(r[2], &h) != nil || json.Unmarshal(r[3], &l) != nil ||
				json.Unmarshal(r[4], &c) != nil || json.Unmarshal(r[5], &contract) != nil {
				continue
			}
			oo := int64(ot)
			out = append(out, fixtureBar{
				kline: market.Kline{
					Open: o, High: h, Low: l, Close: c,
					OpenTime: oo, CloseTime: oo + durMs - 1, Final: true,
				},
				contract: contract,
			})
		}
		return out
	}
	return parse("1m", 60_000), parse("1h", 3_600_000)
}

func loadRealDayStrategy(t *testing.T, dir string) *store.StrategyConfig {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "strategy_a5b7662e.json"))
	if err != nil {
		t.Fatalf("read strategy: %v", err)
	}
	cfg, err := (&store.Strategy{ID: "a5b7662e-7bf7-49bb-9f09-7efa48f95ac8", Config: string(raw)}).ParseConfig()
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	return cfg
}

func loadRealDayRecord(t *testing.T, dir string) rdrLiveRecord {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "live_record.json"))
	if err != nil {
		t.Fatalf("read live record: %v", err)
	}
	var rec rdrLiveRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("parse live record: %v", err)
	}
	return rec
}

// newRealDayWorld mirrors newFdrWorld but loads the real-day fixture and seeds
// the store with warm-up bars up to the replay start (10-20 17:00 CT).
func newRealDayWorld(t *testing.T, dir string, replayFromMs int64) (*fdrWorld, []fixtureBar, []fixtureBar) {
	t.Helper()
	w := &fdrWorld{t: t}
	w.fix1m, w.fix1h = loadRealDayFixture(t, dir)
	for _, b := range w.fix1m {
		if b.contract == fdrContract {
			w.bars = append(w.bars, b.kline)
		}
	}
	if len(w.bars) == 0 {
		t.Fatalf("fixture holds no %s bars", fdrContract)
	}
	w.cur.Store(-1)

	st, err := store.New(filepath.Join(t.TempDir(), "real-day-replay.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	w.st = st
	if err := store.NewBarHistoryStore(st.GormDB()).Migrate(); err != nil {
		t.Fatalf("bars migrate: %v", err)
	}
	w.storedTo = -1 << 62
	w.storeBarsUpTo(replayFromMs)
	for _, c := range loadRealDayRecord(t, dir).Calendar {
		if _, err := w.st.Calendar().SaveSliceIfAbsent(&store.CalendarSliceDB{
			TradeDate: c.Date, Source: c.Source, EventsJSON: c.Events, CreatedAt: replayFromMs,
		}); err != nil {
			t.Fatalf("calendar slice %s: %v", c.Date, err)
		}
	}

	prevProvider := market.FuturesBarsProvider
	prevNow := getMentorNowSource()
	market.FuturesBarsProvider = w.provide
	setMentorNowSource(w.now)
	prevRebuildBook := mentorRebuildBook
	mentorRebuildBook = func(at *AutoTrader, _ time.Time) ([]ntwire.NT8Order, bool, time.Duration) {
		return at.liveBook(w.now())
	}
	t.Cleanup(func() {
		mentorRebuildBook = prevRebuildBook
		market.FuturesBarsProvider = prevProvider
		setMentorNowSource(prevNow)
		mentorLatestPriceSource = nil
		mentorConfluenceForIntent = nil
		mentorSetArmExpiryWire = nil
	})
	resetMentorLiveArms(t)
	t.Setenv("MENTOR_PLACE", "1")
	t.Setenv("STOP_ENTRY_SEAM", "on")
	withMaintenanceDir(t)

	w.srv = ntwire.NewTCPServer(nil)
	w.srv.SetAddrForTest("127.0.0.1:0")
	acct := "Sim101"
	w.srv.SetAccountsList([]ntwire.AccountInfo{{Name: acct, IsSim: true}}, acct)
	ctx, cancel := context.WithCancel(context.Background())
	if err := w.srv.Start(ctx); err != nil {
		cancel()
		t.Fatalf("server start: %v", err)
	}
	t.Cleanup(func() { _ = w.srv.Stop(); cancel() })
	conn, err := net.Dial("tcp", w.srv.ListenAddrForTest().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	waitAddonRegistered(t, w.srv)
	w.addon = newFdrAddon(t, w, conn, rdrAddonBuild, []string{acct}, map[string]bool{acct: true})
	w.srv.SeedPositionsForTest(acct, []ntwire.OpenPosition{})
	return w, w.fix1m, w.fix1h
}

// TestRealDayReplay drives the production mentor path over the live window and
// prints the diff table. It does NOT assert parity — it REPORTS (the control vs
// #43 difference is judged by the CTO; fills are simulated and latency is not
// modeled).
func TestRealDayReplay(t *testing.T) {
	dir := rdrDir(t)
	const replayFromMs = 1792533600000 // 10-20 17:00 CT
	w, fix1m, _ := newRealDayWorld(t, dir, replayFromMs)
	rec := loadRealDayRecord(t, dir)
	cfg := loadRealDayStrategy(t, dir)

	acct := "Sim101"
	ft := &fdrTrader{id: "8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265", account: acct, levelPx: map[string]float64{}}
	w.traders = []*fdrTrader{ft}
	w.newFdrTrader(ft, cfg)
	l := &fdrLedger{rows: map[int64]*fdrRowLife{}}

	start := w.barIndexAtOrAfter(replayFromMs)
	// Replay through the newest bar whose close we know (the fixture's last bar).
	end := len(w.bars)
	w.setNow(replayFromMs)
	w.cur.Store(int64(start - 1))

	for i := start; i < end; i++ {
		w.step(i, w.traders, l)
	}
	w.scanLedger(l, end-1)

	// Collect the replay's authored mentor arms (any row whose Origin is mentor).
	var replayArms []rdrReplayArm
	for _, life := range l.rows {
		r := life.row
		if !isMentorArmOrigin(r) {
			continue
		}
		replayArms = append(replayArms, rdrReplayArm{
			Side: strings.ToLower(r.Side), Entry: r.EntryPx, Stop: r.StopPx, Target: r.TargetPx,
			State:  r.State,
			BornCT: time.UnixMilli(life.bornMs).In(kernel.CTLocation()).Format("01-02 15:04"),
		})
	}
	sort.Slice(replayArms, func(i, j int) bool { return replayArms[i].BornCT < replayArms[j].BornCT })

	fmt.Printf("\n=== REAL-DAY REPLAY DIFF (live trades %d, replay arms %d) ===\n", len(rec.Trades), len(replayArms))
	fmt.Printf("%-4s %-5s %-9s %-9s %-9s %-7s %s\n", "id", "side", "liveEntry", "replayEntry", "replayStop", "state", "verdict")
	for _, tr := range rec.Trades {
		// Only the mentor trader's trades (the other account's rows are not
		// driven here).
		if !strings.Contains(tr.TraderID, "1781246265") {
			continue
		}
		best := matchReplayArm(tr, replayArms)
		if best == nil {
			fmt.Printf("%-4d %-5s %-9.2f %-9s %-9s %-7s %s\n", tr.ID, strings.ToLower(tr.Side), tr.Entry, "-", "-", "-", "NOT AUTHORED (refused)")
			continue
		}
		verdict := "taken"
		if best.State != "filled" && best.State != "working" && best.State != "armed" {
			verdict = "taken→" + best.State
		}
		fmt.Printf("%-4d %-5s %-9.2f %-9.2f %-9.2f %-7s %s\n", tr.ID, strings.ToLower(tr.Side), tr.Entry, best.Entry, best.Stop, best.State, verdict)
	}
	fmt.Printf("\nReplay arms the live bot did NOT author (or authored at a different time):\n")
	used := map[*rdrReplayArm]bool{}
	for _, tr := range rec.Trades {
		if strings.Contains(tr.TraderID, "1781246265") {
			if a := matchReplayArm(tr, replayArms); a != nil {
				used[a] = true
			}
		}
	}
	for _, a := range replayArms {
		if !used[&a] {
			fmt.Printf("  EXTRA %s %s entry %.2f stop %.2f target %.2f (%s)\n", a.BornCT, a.Side, a.Entry, a.Stop, a.Target, a.State)
		}
	}
	_ = fix1m
}

// matchReplayArm finds the replay arm nearest a live trade: same side and
// entry within 1.0 pt.
func matchReplayArm(tr rdrLiveTrade, arms []rdrReplayArm) *rdrReplayArm {
	side := strings.ToLower(tr.Side)
	best := -1
	for i := range arms {
		if arms[i].Side != side {
			continue
		}
		d := arms[i].Entry - tr.Entry
		if d < 0 {
			d = -d
		}
		if d > 1.0 {
			continue
		}
		if best < 0 || d < (arms[best].Entry-tr.Entry) || d == (arms[best].Entry-tr.Entry) {
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	return &arms[best]
}
