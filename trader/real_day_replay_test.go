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

// rdrAddonBuild is the AddOn build the live bot ran over 10-20/21 (m168:
// 2026-10-18-a).
const rdrAddonBuild = "2026-10-18-a"

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
// C52 W1 replay note (confluence A1/A2/B9, base 75707f1ed vs the W1 branch, the
// three days REAL_DAY_REPLAY=/tmp/realday-1021|1022|1023, REAL_DAY=2026-10-21|22|23):
// the diff tables (live trades, replay arms, ADDED arms) are IDENTICAL on all
// three days. One arm changes arm: the 10-23 03:34 CT ISB long (entry 29277.75,
// stop 29264.50, authored as replay row 10, cancelled) forks C with leg-1 TP
// 29304.25 (2R) instead of B, because the 15m/5m/1m ISBs align and the old
// MTF trigger conjunct no longer refuses it (B9). No box arm changes fork on
// those days (A1). The confluence leg-1 TP move (A2) never fires: 10-22 has
// four RESTING arms upgraded to C (isb-2, isb-3, mentor-1793007641765732667,
// box-ftgl:29516.00:29505.75-1792653599999), none fills in the replay.
//
// c52 W6 NOTE (RollingPHL default ON + pullback-colour + 5m-trigger gates,
// 2026-10-26): replayed 10-21 / 10-22 / 10-23 (REAL_DAY_REPLAY=/tmp/realday-
// 1021|1022|1023) with the flag OFF (base 75707f1ed), ON with the new gates,
// and ON with both gates bypassed: the ADDED-arm list and the live-trade
// verdict rows were IDENTICAL in all three runs on all three days (zero rolling
// arms). On 10-21 the roll was reached on 1091 ticks and stopped at "no HTF
// trend direction" on 1065, "level touch owns the entry" on 8, "too close to
// the old extreme" on 4, "no old extreme" on 1. So no arm changes in this
// replay set: the flip is dormant here, not proven by it.
//
// REAL_DAY_REPLAY note, release #52 W2 (exit drive: resonance A3/B18 + candle
// trail B1/B2), run 2026-10-26 at base d3dfe91fc vs fix/c52-w2,
// REAL_DAY_REPLAY=/tmp/realday-1021|1022|1023, REAL_DAY=2026-10-21|22|23,
// TZ=America/Chicago, -count=1: the "REAL-DAY REPLAY DIFF" tables (live trades,
// replay arms, ADDED arms) are IDENTICAL on all three days (10-21: 13 live
// trades / 20 replay arms; 10-22 and 10-23 likewise unchanged). No arm changes
// because W2 touches EXITS only, and NO replay arm FILLS on those days (every
// replay arm is cancelled / expired / still working: "taken" rows 10-21 5, 10-22
// 3, 10-23 0, all "taken->cancelled"), so the exit drive never runs: zero
// "resonance ARMED", "resonance runner UNCAPPED", "leg-1 live 1:1 stop" or
// "resonance BE" lines on either side. What WOULD change once a fill exists (all
// in the live path, none visible here, so the change is dormant in this replay
// set, not proven by it):
//   - A3: every mode-A runner gets its TP parked 600 pts out at the arm
//     (was: kept the PHL/PLH TP shy of the old high);
//   - B18: a 2m/5m-entry PHL/PLH resonance ISB at the 1st-3rd entry-frame candle
//     after the fill arms mode A (was: refused after 1m bar 3, i.e. most 2m
//     ISBs), and a 1m entry's window moves from 1m bars 1..3 to 1m bars 2..4
//     (the fill candle is no longer counted);
//   - B2: a split resonance position's BE waits for the confirmed 1:1 partial
//     and moves the runner legs only (was: +0.25R, every leg incl. leg 1);
//   - B1: leg 1 of a split position in candle-trail mode follows 2c-T after the
//     BE-arming candle (was: sat at BE until its TP filled).
//
// REAL_DAY_REPLAY NOTE c52 W9 (trendline + flag wall, B7-R7 / B21 / B22), run
// on 75707f1ed (base) vs fix/c52-w9, REAL_DAY_REPLAY=/tmp/realday-1021|1022|1023,
// REAL_DAY=2026-10-21|22|23, TZ=America/Chicago. The "attribution is broken"
// failure below is identical on both sides: it is not a W9 effect.
//
//	10-21: entry intents 32 -> 37, flag_wall_room 5 -> 1, replay arms 23 -> 22.
//	  Arm set: ADDED 10-20 18:34 short 29857.50 (new); 10-21 15:10 long 30049.75
//	  and 15:50 long 30038.25 (gone), 10-21 13:55 long 30033.75 (new). Cause [B],
//	  from the refusal counts, not traced arm by arm: fewer flag_wall_room
//	  refusals (B21 nearest flag, B22 PHL/PLH only), and the arms after a
//	  changed one shift with the day state. tl_first_test_level_preferred: +2
//	  lines (a 1H long line, 3 ticks; a 1m short line, 1 tick) now lose to an
//	  FTGH/FTGL box edge within 2 pts (B7-R7).
//	10-22: no change (same arm set, same refusals).
//	10-23: entry intents 19 -> 20, flag_wall_room 1 -> 0, flag_wall_target_used
//	  3 -> 2 (non-PHL/PLH entries no longer cut), replay arms 14 -> 15 with the
//	  same ADDED entry lines, +1 line (long, 1m) preferred over a box edge.
//
// Live trades 13 / 10 and their matched/unmatched split are unchanged.
//
// HONEST LIMITS (do not fake parity):
//   - fills are SIMULATED: the replay's ledger row flips to `filled` on a
//     candle touching the entry, not on a broker receipt; NT8 fill/ack latency
//     and partial fills are not modeled;
//   - the replay bar clock runs at replay speed, so any wall-clock-dependent
//     path (entry-latch evidence age, C8 sweep) is played at replay cadence;
//   - the replay calendar is the LIVE slice (10-19..10-21, exported from the
//     DB), NOT empty — a news day freezes the 1h/4h triggers via the real
//     slice;
//   - BE moves and the exit drive are the PRODUCTION kernel, but their inputs
//     are the simulated fills, so an exit time may shift a bar.
//
// REAL_DAY_REPLAY note, release #52 W5 (swing 4h ISB, A6 + A7), run 2026-10-26
// at base 75707f1ed vs fix/c52-w5, TZ=America/Chicago, -count=1:
//   - 10-21 (REAL_DAY=2026-10-21): TWO simulated SWING4H arms are gone, both
//     LONG 2-contract ISB-style entries after 14:30 CT: row 22 (ArmID
//     swing-1792613100000, 15:05 CT bar, stop AT the line, 49.3 pts, cancelled
//     by a later close through the line) and row 23 (swing-1792615500000,
//     15:45 CT bar, stop 37.8 pts). CAUSE (corrected after review, verified
//     with a temporary print in the Through branch [A]): NEITHER arm is
//     removed by A7. Both fully satisfy A7 (line 30000.50, approach long; the
//     14:55 bar closed through at 29999; at 15:05 candle 1 = the 15:00 bar is
//     green 29998.25 -> 30047.5, closed back above the line, and the 15:05
//     body is inside it; row 23 likewise: 15:40 green close-back, 15:45 inside
//     bar). They are removed by the A6 once-per-4h-bucket latch: the earlier
//     ISB (swing-1792611000000, 14:30 CT, 13:00-17:00 CT bucket, level already
//     invalid) set ISBFired, and ISBFired blocks every later ISB in the same
//     4h candle, EVEN after a fresh through-close (14:55) followed by a green
//     close-back and an inside bar. The old code re-armed the ISB on every
//     later qualifying pair. TRADE-SET REDUCTION to put in front of the owner
//     (design point, NOT a code bug): the plan says "make the ISB not re-arm
//     each bar" and a separate marker is plan-sanctioned, but the course
//     states "chi kiem Insaba" with no once-only limit, so a second ISB after
//     the first one's order was cancelled by a later through-close looks
//     course-valid. If the owner wants it, the minimal alternative is to clear
//     ISBFired when the first ISB's order is cancelled or filled and a new
//     through-close is recorded (not built: the owner has not ruled). The
//     other 21 arms (ISB/PLH/PHL and the 10:30 CT SWING4H short, row 16) are
//     identical.
//   - 10-22 and 10-23: arm and cancel streams identical (14 arms each).
//   - TestRealDayReplay itself exits FAIL on all three days at
//     real_day_replay_test.go:451 ("live mentor trade(s) neither matched nor
//     given a named refusal reason") on the base as well: a pre-existing
//     attribution gap, unchanged by this wave (2 / 1 / 1 trades).
//

// REAL_DAY_REPLAY note, release #52 W11 (fix/c52-w11, invented-constant knobs
// B8 / B10 / B11 / B27 / C16 + the C15 box-return-walk hygiene),
// REAL_DAY_REPLAY=/tmp/realday-1021|1022|1023, REAL_DAY=2026-10-21|22|23,
// TZ=America/Chicago, before = adc0ee140 (a detached worktree), after = this
// branch: the diff tables (live trades, replay arms, ADDED arms) and every
// refusal/funnel line are IDENTICAL on all three days (only wall-clock
// latencies and random ids differ; compared with log timestamps masked). That
// is the intended result: every new knob defaults to the value the kernel
// hard-coded before (cancel candle 4, tiny stop off, evening slow 100 pts from
// 15:00 cumulative, near-box 2x incl. PHL/PLH, PHL shy 5, old-extreme +/-2,
// in-range gap = the ping-pong 50) and Config.Box equals DefaultBoxCfg(), so no
// arm may change; a default flip is a later mentor/owner call. The existing
// "attribution is broken" assertion fails on both sides and is not part of this
// change.

// REAL_DAY_REPLAY note, release #52 W10 (fix/c52-w10, B20 direction-aware 5m
// pullback warning; A10 NOT built: the plan records two conflicting course
// statements), REAL_DAY_REPLAY=/tmp/realday-1021|1022|1023, REAL_DAY=2026-10-21|
// 22|23, TZ=America/Chicago, before = 1fd721a2f, after = this branch. The diff
// tables, arm lists and every refusal/funnel line are IDENTICAL on all three
// days (the flag only changes the SIZE tier, which the replay does not print).
// The flag stamps themselves (ISB intents carrying pullback_warn, counted with a
// temporary print at isbFlagsFor, removed) CHANGE: base 7 / 0 / 4 on 10-21 / 10-22
// / 10-23 (11 stamps: 9 SHORT, 2 LONG), branch 3 / 2 / 2 (7 stamps: 5 SHORT,
// 2 LONG). The base rule was "previous 5m candle green and this candle broke
// its low", side-blind; the branch reads the 5m swing structure (HH+HL up, LH+LL
// down), warns only the side that trades WITH that trend, and adds the mirror.
// Sample stamps (CT, intent close time): base only, 10-20 17:45 short, 10-20
// 17:55 short, 10-21 02:55 long, 10-21 11:19 short, 10-21 13:03 short, 10-21
// 15:55 short, 10-22 17:19 short, 10-22 18:29 short, 10-23 13:53 short, 10-23
// 15:29 short; branch only, 10-21 10:59 long, 10-21 13:35 short, 10-21 18:15
// short, 10-21 21:09 short, 10-23 06:11 short, 10-23 13:57 short; both, 10-21 11:21 long. (Times are
// evaluator ticks, not necessarily armed or filled trades.) The "attribution is
// broken" assertion at the end already fails on both and is not part of this
// change.

// REAL_DAY_REPLAY note, release #52 W3 (fix/c52-w3, level-arm lifecycle A4 a+b;
// B13 and B23 not built), REAL_DAY_REPLAY=/tmp/realday-1021|1022|1023,
// REAL_DAY=2026-10-21|22|23, TZ=America/Chicago, before = d3dfe91fc eval.go,
// after = this branch: the diff tables (live trades, replay arms, ADDED arms)
// and every refusal/funnel line are IDENTICAL on all three days (only random
// broker order ids and wall-clock latencies differ). No resting level ("lvl-")
// order exists in the replayed arms and no "level order rest cancelled" fires on
// any of the three days, so A4 is DORMANT in this replay set: it is proven by
// the unit pins (TestC52W3*), not by the replay. The existing "attribution is
// broken" assertion fails on both sides and is not part of this change.

// REAL_DAY_REPLAY note, release #52 W8 (fix/c52-w8; replays REAL_DAY_REPLAY=
// /tmp/realday-1021|1022|1023 with REAL_DAY=2026-10-21|22|23, before = the
// 1h rung re-added, after = this branch; the "attribution is broken" assertion
// at the end already fails on both and is not part of this change):
//   - B5 (the 1h trigger line is no longer a target rung): ONE arm changes over
//     the three days: 10-23 03:48 short entry 29229.00 stop 29241.50, target
//     29188.50 -> 29168.56 (a 1h line at 29188.50 no longer stops it; the next
//     level beyond is the target). 10-21 and 10-22: every arm identical.
//     INTENT-LEVEL (funnel counter) changes from B5, same days, deterministic
//     (base vs base and branch vs branch identical), authored/placed/filled
//     counts identical on all three days. 10-21 branch vs base:
//     phl_target_below_floor 8 -> 4, trigger_ftgh_sell_zone 5 -> 8, room
//     36 -> 37, new flag_wall_target_used:1, ISB arm sequence isb-22 -> isb-25
//     (same trigger and price). 10-22: phl_day_off 2 -> 14, room 9 -> 8. With
//     the 1h rung re-added the 10-21/22 counters equal base exactly. VISIBLE
//     LOSS: the 1h trigger line also leaves the chart/ChartLevels/Truth-card
//     level set (the 4h line stays drawn); guide cards updated to say so.
//   - B19 (orb_escape_completely, default OFF): no arm changes (the knob is
//     off in the replay config); ON would move only WHEN the ORB opens.
//   - B6 (label only) and B4 (not built, needs a mentor answer): no change.

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
	// ClockSteps is the live wall-vs-monotonic step series the guard saw on
	// the live day: [[ts_utc_ms, step_ms], …]. step_ms is signed (negative =
	// backward). Injected into the replay so the clock-step hold reproduces
	// the live entry refusals (m168 item 2).
	ClockSteps [][2]int64 `json:"clock_steps"`
}

type rdrLiveCal struct {
	Date   string `json:"date"`
	Source string `json:"source"`
	Events string `json:"events"`
}

// rdrBarRef is one bar's diffed refusal counters (per-bar attribution).
type rdrBarRef struct {
	ct     string
	trader map[string]int
	kernel map[string]int
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
	replayFromMs := int64(1792533600000) // 10-20 17:00 CT (default)
	endMs := int64(0)
	if day := os.Getenv("REAL_DAY"); day != "" {
		// One CME session-day: seed at the prior calendar day 17:00 CT, run to
		// this day's 16:00 CT (m280 per-day replay).
		d, err := time.ParseInLocation("2006-01-02", day, kernel.CTLocation())
		if err != nil {
			t.Fatalf("REAL_DAY %q: %v", day, err)
		}
		prev := d.AddDate(0, 0, -1)
		replayFromMs = time.Date(prev.Year(), prev.Month(), prev.Day(), 17, 0, 0, 0, kernel.CTLocation()).UnixMilli()
		endMs = time.Date(d.Year(), d.Month(), d.Day(), 16, 0, 0, 0, kernel.CTLocation()).UnixMilli()
	}
	w, fix1m, _ := newRealDayWorld(t, dir, replayFromMs)
	rec := loadRealDayRecord(t, dir)
	cfg := loadRealDayStrategy(t, dir)

	acct := "Sim101"
	ft := &fdrTrader{id: "8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265", account: acct, levelPx: map[string]float64{}}
	w.traders = []*fdrTrader{ft}

	start := w.barIndexAtOrAfter(replayFromMs)
	// Replay through the newest bar whose close we know (the fixture's last bar),
	// or the session close when REAL_DAY is set.
	end := len(w.bars)
	if endMs > 0 {
		end = w.barIndexAtOrAfter(endMs)
	}
	// The seed clock MUST be the replay clock BEFORE the seed runs — a seed at
	// the zero time (1970-01-01) blocks SWING4H (m168 item 1).
	w.setNow(replayFromMs)
	w.cur.Store(int64(start - 1))

	// m168 item 2: install the live wall-vs-monotonic clock so the clock-step
	// hold reproduces the live entry refusals. The SteppedClock's mono advances
	// in lockstep with the replay wall; each live step is applied as a pure wall
	// Step and observed, so a >= CLOCK_STEP_FREEZE_MS step holds new entries for
	// 120 s of mono time exactly as on the live box. The freeze threshold is the
	// env (CLOCK_STEP_FREEZE_MS): the control run sets it to the live build's
	// 2000 ms to reproduce the refusals; the #43 head's default 6000 ms treats
	// the same sub-6 s steps as noise — the fix under test.
	clk := kernel.NewSteppedClock(time.UnixMilli(replayFromMs))
	restoreClock := kernel.ClockSampleForTest(clk.Sample)
	defer restoreClock()
	var clkStepIdx, clkMonoMs int64
	applyClockStepsUpTo := func(untilMs int64) {
		if untilMs > clkMonoMs {
			clk.Advance(time.Duration(untilMs-clkMonoMs) * time.Millisecond)
			clkMonoMs = untilMs
		}
		for clkStepIdx < int64(len(rec.ClockSteps)) && rec.ClockSteps[clkStepIdx][0] <= untilMs {
			s := rec.ClockSteps[clkStepIdx]
			clk.Step(time.Duration(s[1]) * time.Millisecond)
			kernel.ObserveClock()
			clkStepIdx++
		}
	}
	applyClockStepsUpTo(replayFromMs) // prime the guard's first sample at the replay start

	// Receipt seam: the monotonic receipt leg stays UNWIRED (nil) on purpose.
	// The fixture feed is always alive, and the live receipt leg refused
	// nothing on this day (all 16 arms 387–406 were authored), so a wired
	// receipt could only fabricate a dead-feed refusal on the replay's fast
	// cadence. The injected leg is the clock-step HOLD, not the receipt age
	// (m168 item 2).

	w.newFdrTrader(ft, cfg)
	l := &fdrLedger{rows: map[int64]*fdrRowLife{}}

	// Per-bar refusal attribution: the trader counters + kernel refusals that
	// fired on each bar, so a dropped live trade names its reason.
	var barRefs []rdrBarRef

	for i := start; i < end; i++ {
		beforeT := ft.at.mentorCountSnapshot()
		beforeK := mentorKernelRefusals(ft.at)
		applyClockStepsUpTo(w.bars[i].CloseTime) // live steps that fired at/before this bar close (m168 item 2)
		w.step(i, w.traders, l)
		afterT := ft.at.mentorCountSnapshot()
		afterK := mentorKernelRefusals(ft.at)
		dt := map[string]int{}
		for k, v := range afterT {
			if d := v - beforeT[k]; d > 0 {
				dt[k] = d
			}
		}
		dk := map[string]int{}
		for k, v := range afterK {
			if d := v - beforeK[k]; d > 0 {
				dk[k] = d
			}
		}
		if len(dt) > 0 || len(dk) > 0 {
			barRefs = append(barRefs, rdrBarRef{ct: fdrCT(w.bars[i].CloseTime + 1), trader: dt, kernel: dk})
		}
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
	// m168: the harness must be able to FAIL. A mentor trade that is neither
	// matched nor given a NAMED refusal reason is a harness bug, not a result.
	unexplained := 0
	for _, tr := range rec.Trades {
		// Only the mentor trader's trades (the other account's rows are not
		// driven here).
		if !strings.Contains(tr.TraderID, "1781246265") {
			continue
		}
		idx := matchReplayArm(tr, liveArmCreated(rec, tr), replayArms)
		if idx < 0 {
			reason := refusalFor(t, rec, tr, barRefs)
			if strings.HasPrefix(reason, "no ") || reason == "" {
				unexplained++
			}
			fmt.Printf("%-4d %-5s %-9.2f %-9s %-9s %-7s %s\n", tr.ID, strings.ToLower(tr.Side), tr.Entry, "-", "-", "-", "NOT AUTHORED — "+reason)
			continue
		}
		best := replayArms[idx]
		verdict := "taken"
		if best.State != "filled" && best.State != "working" && best.State != "armed" {
			verdict = "taken→" + best.State
		}
		fmt.Printf("%-4d %-5s %-9.2f %-9.2f %-9.2f %-7s %s\n", tr.ID, strings.ToLower(tr.Side), tr.Entry, best.Entry, best.Stop, best.State, verdict)
	}
	fmt.Printf("\nArms the candidate adds vs live (no live counterpart in armed_orders):\n")
	used := map[int]bool{}
	for _, tr := range rec.Trades {
		if strings.Contains(tr.TraderID, "1781246265") {
			if idx := matchReplayArm(tr, liveArmCreated(rec, tr), replayArms); idx >= 0 {
				used[idx] = true
			}
		}
	}
	added := 0
	for i, a := range replayArms {
		if used[i] {
			continue
		}
		// A replay arm that matches a live CANCELLED arm is a faithful replay,
		// NOT an addition — the live record's arms include cancelled rows
		// (m168 item 3).
		if liveArmMatched(a, rec) {
			continue
		}
		added++
		// These are the live-refused arms the 6000 ms freeze threshold makes
		// placeable (the live bot held them on a sub-6 s clock step; see the
		// #43 disclosure). They are a RESULT, not a harness bug: the control
		// run with CLOCK_STEP_FREEZE_MS=2000 refuses them and matches live
		// (m168 item 2).
		fmt.Printf("  ADDED %s %s entry %.2f stop %.2f target %.2f (%s)\n", a.BornCT, a.Side, a.Entry, a.Stop, a.Target, a.State)
	}
	_ = fix1m

	// m168: the harness FAILS when it cannot explain itself.
	if len(replayArms) == 0 {
		t.Fatalf("the replay authored ZERO mentor arms — the seed is broken (check the warm-up depth and the seed clock)")
	}
	if unexplained > 0 {
		t.Fatalf("%d live mentor trade(s) were neither matched nor given a named refusal reason — attribution is broken", unexplained)
	}
	if added > 0 {
		t.Logf("%d arm(s) added vs live — these are the 6000 ms freeze consequence (live refused on a sub-6 s clock step); the control at CLOCK_STEP_FREEZE_MS=2000 refuses them", added)
	}
}

// liveArmCreated returns the live armed_orders author instant for a trade
// (side + entry within 1.5 pt), or zero if the record has no arm for it. The
// author instant — not the fill — is the correct anchor for a replay arm whose
// born time is the bar close (the fill can land a bar later; m168 item 3).
func liveArmCreated(rec rdrLiveRecord, tr rdrLiveTrade) time.Time {
	side := strings.ToLower(tr.Side)
	for i := range rec.Arms {
		a := &rec.Arms[i]
		if strings.ToLower(a.Side) != side {
			continue
		}
		d := a.Entry - tr.Entry
		if d < 0 {
			d = -d
		}
		if d > 1.5 {
			continue
		}
		if t, err := parseRdrCreated(a.Created); err == nil {
			return t
		}
		return time.Time{}
	}
	return time.Time{}
}

// liveArmMatched reports whether a replay arm has a live armed_orders
// counterpart (filled OR cancelled): side + entry within 1.0 pt + author
// instant within ±1 bar (120 s). The live record's arms include cancelled
// rows, so a cancelled-but-authored arm is a faithful replay, not an extra
// (m168 item 3).
func liveArmMatched(a rdrReplayArm, rec rdrLiveRecord) bool {
	born, err := time.ParseInLocation("01-02 15:04", a.BornCT, kernel.CTLocation())
	if err != nil {
		return false
	}
	born = time.Date(2026, born.Month(), born.Day(), born.Hour(), born.Minute(), 0, 0, kernel.CTLocation())
	for i := range rec.Arms {
		la := &rec.Arms[i]
		if strings.ToLower(la.Side) != a.Side {
			continue
		}
		d := la.Entry - a.Entry
		if d < 0 {
			d = -d
		}
		if d > 1.0 {
			continue
		}
		lt, err := parseRdrCreated(la.Created)
		if err != nil {
			continue
		}
		td := born.Sub(lt).Seconds()
		if td < 0 {
			td = -td
		}
		if td > 120 {
			continue
		}
		return true
	}
	return false
}

// matchReplayArm finds the replay arm nearest a live trade: same side, entry
// within 1.0 pt, AND born within ±1 bar (±120 s) of the LIVE ARM's author
// instant (author→author, not fill→bar-close). Price alone matched too much
// (m168 item 3).
func matchReplayArm(tr rdrLiveTrade, liveCreated time.Time, arms []rdrReplayArm) int {
	side := strings.ToLower(tr.Side)
	best := -1
	bestScore := -1.0
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
		if !liveCreated.IsZero() {
			born, err := time.ParseInLocation("01-02 15:04", arms[i].BornCT, kernel.CTLocation())
			if err != nil {
				continue
			}
			born = time.Date(2026, born.Month(), born.Day(), born.Hour(), born.Minute(), 0, 0, kernel.CTLocation())
			td := born.Sub(liveCreated).Seconds()
			if td < 0 {
				td = -td
			}
			if td > 120 {
				continue
			}
		}
		score := d // price error dominates; time is a tiebreak
		if best < 0 || score < bestScore {
			best, bestScore = i, score
		}
	}
	return best
}

// parseRdrCreated parses the live armed_orders created_at, which SQLite returns
// as "2006-01-02 15:04:05.999999999-07:00" (space, not the RFC3339 T).
func parseRdrCreated(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999Z07:00", time.RFC3339Nano} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable created_at %q", s)
}

// rdrAttributionWindow is how far (either side) of a live arm's authoring
// instant the replay's refusal bars are searched. The live author instant is
// the CLOSE of the execution-frame candle (1m, 2m after 09:00 CT's 30m rule, or
// a 5m frame), and the replay evaluates every 1m bar, so the kernel's refusal
// for the same setup can land on a neighbouring 1m bar: up to one 5m frame
// either side (m431: trade 690's isbrev_target_below_floor was one bar off).
const rdrAttributionWindow = 5 * time.Minute

// rdrRefusalShaped reports whether a trader counter name is a refusal/hold
// (not an extend_ok / intent_<setup> / tier_<size> tally).
func rdrRefusalShaped(k string) bool {
	for _, w := range []string{"refus", "blocked", "invalid", "floor", "hold", "freeze", "halt", "room", "eod"} {
		if strings.Contains(k, w) {
			return true
		}
	}
	return false
}

// rdrNamedRefusals lists the entry-blocking refusals recorded on one replay
// bar: EVERY kernel refusal counter (the kernel's State.Refusals / limits maps
// hold nothing but refusals), plus the trader-side counters except the
// arm-management races (cancel/close/extend of a row that is already gone),
// which say nothing about why an entry was not authored. Sorted, "k=n".
func rdrNamedRefusals(r rdrBarRef) []string {
	var parts []string
	for k, v := range r.kernel {
		if v > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", k, v))
		}
	}
	for k, v := range r.trader {
		if v <= 0 || strings.HasPrefix(k, "cancel_") || strings.HasPrefix(k, "close_") || k == "extend_refused" {
			continue
		}
		// The trader counter map also holds non-refusal tallies (extend_ok,
		// intent_<setup>, tier_<size>): keep only the refusal-shaped names.
		if !rdrRefusalShaped(k) {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%d", k, v))
	}
	sort.Strings(parts)
	return parts
}

// rdrAttribute names the replay's refusal for a live arm authored at `created`:
// among the replay bars within rdrAttributionWindow that carry at least one
// named refusal, the NEAREST one wins. A window with no refusal bar returns a
// "no ..." string, which the caller counts as UNEXPLAINED (the harness must be
// able to fail on a genuinely unattributed trade).
func rdrAttribute(created time.Time, refs []rdrBarRef) string {
	best := -1
	bestD := time.Duration(1<<63 - 1)
	var bestParts []string
	for i, r := range refs {
		ct, err := time.ParseInLocation("01-02 15:04", r.ct, kernel.CTLocation())
		if err != nil {
			continue
		}
		// The bar's close year is the replay year (2026); the live created is UTC.
		ct = time.Date(2026, ct.Month(), ct.Day(), ct.Hour(), ct.Minute(), 0, 0, kernel.CTLocation())
		d := created.Sub(ct)
		if d < 0 {
			d = -d
		}
		if d > rdrAttributionWindow || d >= bestD {
			continue
		}
		parts := rdrNamedRefusals(r)
		if len(parts) == 0 {
			continue
		}
		bestD, best, bestParts = d, i, parts
	}
	if best < 0 {
		return fmt.Sprintf("no named refusal within %s of the live arm", rdrAttributionWindow)
	}
	return fmt.Sprintf("@%s (%+ds) %s", refs[best].ct, int(bestD.Seconds()), strings.Join(bestParts, " "))
}

// refusalFor names the replay refusal for a live trade that was NOT authored:
// the live arm's created instant (from the record) is matched to the replay
// bars within rdrAttributionWindow whose diffed trader/kernel refusals name why.
func refusalFor(t *testing.T, rec rdrLiveRecord, tr rdrLiveTrade, refs []rdrBarRef) string {
	side := strings.ToLower(tr.Side)
	// The live arm that produced this trade (same side, entry within 0.5).
	var created time.Time
	for _, a := range rec.Arms {
		if strings.ToLower(a.Side) != side {
			continue
		}
		d := a.Entry - tr.Entry
		if d < 0 {
			d = -d
		}
		if d > 0.5 {
			continue
		}
		if c, err := parseRdrCreated(a.Created); err == nil {
			created = c
			break
		}
	}
	if created.IsZero() {
		return "no matching live arm"
	}
	return rdrAttribute(created, refs)
}

// C52-W4 REAL_DAY_REPLAY note (A5 part 1, reverse ISB at EMA 9 reads the 2m
// EMA 9 on a 2m exec pair). Run vs base 75707f1ed, same box and load, REAL_DAY
// 2026-10-21 / 22 / 23 (the replay's PASS/FAIL is the same on both trees: its
// "neither matched nor named" attribution gate was already red on the base).
// Arms that change (placed stop-entry set diffed):
//   - 10-21 day (dir /tmp/realday-1021): 10-20 17:42 SHORT 29925.50 (reverse
//     ISB, tagged PHL) is no longer placed - the 1m EMA 9 touched the 2m ISB
//     candle, the 2m EMA 9 does not; 10-21 03:44 SHORT 29802.00 and 10-21 08:24
//     LONG 29882.00 likewise (the base ones were refused at the wire by the B3
//     dupe/rate guard anyway). 10-20 17:16 SHORT 29937.25 is now a NORMAL ISB
//     (isb-2-...): the base reverse ISB on that pair used to suppress it (R85
//     suppressSamePairCounterISB), and with no reverse the normal ISB arms.
//     Live trade #666's refusal label moves from refused_same_side_working to
//     isb_trigger_side_mismatch (still not authored).
//   - 10-22 day: placed-arm set identical (replay-arm count 14 -> 13 is a
//     non-placed arm, not an authored order).
//   - 10-23 day: identical.
// No other arm changed. Which frame the EMA is read on for the 5m slow-evening
// pair is unchanged (owner Q9).
