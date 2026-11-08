package trader

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"vl/internal/datareport"
	"vl/kernel"
	"vl/kernel/mentor"
	"vl/store"
)

// ── m469 WHY NO TRADE pins ─────────────────────────────────────────────────
//
// The fixture is the REAL funnel of the 11-05 and 11-06 sessions: the
// "mentor funnel [15m]" lines below are copied verbatim (prefix trimmed to
// "time | trader | ...") from data/vl_2026-11-04.log. That process booted
// 11-04 17:10:29 CT and ran until the release #62 restart at 11-06 13:45, so
// the 11-05 session is a since-boot session and the 11-06 one crosses a 17:00
// roll. The replay below drives the PRODUCTION funnel (mentorFunnelTickAt and
// the stage bumps) to the logged numbers, then reads it through the
// production WhyNoTradeInput and datareport.Collect.
var m469FunnelLog = []string{
	"11-04 18:12:35 | hoang | bars 1 · entry intents 0 · other actions 1 · kernel refusals {level_invalid_first_touch:1 phl_stop_ceiling:2 rolling_keypair_candle_too_big:1} · kernel fills {} · trader refusals {} · authored 0 · placed 0 · filled 0",
	"11-04 18:12:35 | 4567489 mess. | bars 1 · entry intents 0 · other actions 1 · kernel refusals {level_invalid_first_touch:1 phl_stop_ceiling:2 rolling_keypair_candle_too_big:1} · kernel fills {} · trader refusals {} · authored 0 · placed 0 · filled 0",
	"11-05 15:57:59 | hoang | bars 1307 · entry intents 3 · other actions 128 · kernel refusals {box_isb_ban:22 box_isb_ban_15m:6 box_isb_ban_30m:1 box_ping_pong:3 box_stop_ceiling:2 isb_box_blocked:9 isb_box_blocked_15m:3 isb_box_blocked_30m:2 isb_htf_conflict:94 isb_htf_side_mismatch:10 isb_stop_over_max:120 isb_target_below_floor:21 isb_tf_conflict:48 isb_tf_conflict_both_boxes:48 isb_trigger_side:10 isb_trigger_side_mismatch:9 isbrev_box_blocked:5 isbrev_box_blocked_30m:1 isbrev_day_off:10 isbrev_htf_side_mismatch:1 isbrev_stop_over_max:20 isbrev_tf_conflict:10 keypair_candle_too_big:23 level_invalid_first_touch:126 level_visit_cap:1 near_box:36 phl_15m_box_ban:6 phl_30m_box_ban:6 phl_5m_box_ban:22 phl_degenerate_geometry:1 phl_mid_range_boxed:29 phl_no_old_extreme_on_side:15 phl_not_higher_low:45 phl_not_location:37 phl_stop_ceiling:38 phl_target_below_floor:20 phl_tf_conflict:183 phl_too_close_to_extreme:2 rolling_htf_conflict:92 rolling_keypair_candle_too_big:1 rolling_near_box:11 rolling_phl_15m_box_ban:9 rolling_phl_30m_box_ban:3 rolling_phl_5m_box_ban:33 rolling_phl_degenerate_geometry:1 rolling_phl_mid_range_boxed:47 rolling_phl_spent_stop_cap:5 rolling_phl_stop_ceiling:12 rolling_phl_target_below_floor:8 rolling_phl_too_close_to_extreme:4 rolling_phl_trigger_side:1 rolling_room:7 room:3 tl_first_test_level_preferred:trendline:short:1793837760000:1793838600000:1 trendline_crossed_between_anchors:52} · kernel fills {} · trader refusals {placement_refused_data_integrity:1 stale_data_episodes:1} · authored 2 · placed 2 · filled 0",
	"11-05 15:57:59 | 4567489 mess. | bars 1307 · entry intents 3 · other actions 127 · kernel refusals {box_isb_ban:22 box_isb_ban_15m:6 box_isb_ban_30m:1 box_ping_pong:3 box_stop_ceiling:2 isb_box_blocked:9 isb_box_blocked_15m:3 isb_box_blocked_30m:2 isb_htf_conflict:94 isb_htf_side_mismatch:10 isb_stop_over_max:120 isb_target_below_floor:21 isb_tf_conflict:48 isb_tf_conflict_both_boxes:48 isb_trigger_side:10 isb_trigger_side_mismatch:9 isbrev_box_blocked:5 isbrev_box_blocked_30m:1 isbrev_day_off:10 isbrev_htf_side_mismatch:1 isbrev_stop_over_max:20 isbrev_tf_conflict:10 keypair_candle_too_big:23 level_invalid_first_touch:126 level_visit_cap:1 near_box:36 phl_15m_box_ban:6 phl_30m_box_ban:6 phl_5m_box_ban:22 phl_degenerate_geometry:1 phl_mid_range_boxed:29 phl_no_old_extreme_on_side:15 phl_not_higher_low:45 phl_not_location:37 phl_stop_ceiling:38 phl_target_below_floor:20 phl_tf_conflict:183 phl_too_close_to_extreme:2 rolling_htf_conflict:92 rolling_keypair_candle_too_big:1 rolling_near_box:11 rolling_phl_15m_box_ban:9 rolling_phl_30m_box_ban:3 rolling_phl_5m_box_ban:33 rolling_phl_degenerate_geometry:1 rolling_phl_mid_range_boxed:47 rolling_phl_spent_stop_cap:5 rolling_phl_stop_ceiling:12 rolling_phl_target_below_floor:8 rolling_phl_too_close_to_extreme:4 rolling_phl_trigger_side:1 rolling_room:7 room:3 tl_first_test_level_preferred:trendline:short:1793837760000:1793838600000:1 trendline_crossed_between_anchors:52} · kernel fills {} · trader refusals {placement_refused_data_integrity:1 stale_data_episodes:1 window_refused:2} · authored 0 · placed 0 · filled 0",
	"11-05 16:59:59 | hoang | bars 1309 · entry intents 3 · other actions 128 · kernel refusals {box_isb_ban:22 box_isb_ban_15m:6 box_isb_ban_30m:1 box_ping_pong:3 box_stop_ceiling:2 isb_box_blocked:9 isb_box_blocked_15m:3 isb_box_blocked_30m:2 isb_htf_conflict:94 isb_htf_side_mismatch:10 isb_stop_over_max:120 isb_target_below_floor:21 isb_tf_conflict:48 isb_tf_conflict_both_boxes:48 isb_trigger_side:10 isb_trigger_side_mismatch:9 isbrev_box_blocked:5 isbrev_box_blocked_30m:1 isbrev_day_off:10 isbrev_htf_side_mismatch:1 isbrev_stop_over_max:20 isbrev_tf_conflict:10 keypair_candle_too_big:23 level_invalid_first_touch:126 level_visit_cap:1 near_box:36 phl_15m_box_ban:7 phl_30m_box_ban:6 phl_5m_box_ban:22 phl_degenerate_geometry:1 phl_mid_range_boxed:29 phl_no_old_extreme_on_side:15 phl_not_higher_low:45 phl_not_location:37 phl_stop_ceiling:38 phl_target_below_floor:20 phl_tf_conflict:183 phl_too_close_to_extreme:2 rolling_htf_conflict:92 rolling_keypair_candle_too_big:1 rolling_near_box:11 rolling_phl_15m_box_ban:9 rolling_phl_30m_box_ban:3 rolling_phl_5m_box_ban:33 rolling_phl_degenerate_geometry:1 rolling_phl_mid_range_boxed:47 rolling_phl_spent_stop_cap:5 rolling_phl_stop_ceiling:12 rolling_phl_target_below_floor:8 rolling_phl_too_close_to_extreme:4 rolling_phl_trigger_side:1 rolling_room:7 room:3 tl_first_test_level_preferred:trendline:short:1793837760000:1793838600000:1 trendline_crossed_between_anchors:52} · kernel fills {} · trader refusals {placement_refused_data_integrity:1 stale_data_episodes:1} · authored 2 · placed 2 · filled 0",
	"11-05 17:00:59 | hoang | bars 1 · entry intents 0 · other actions 0 · kernel refusals {box_isb_ban:22 box_isb_ban_15m:6 box_isb_ban_30m:1 box_ping_pong:3 box_stop_ceiling:2 isb_box_blocked:9 isb_box_blocked_15m:3 isb_box_blocked_30m:2 isb_htf_conflict:94 isb_htf_side_mismatch:10 isb_stop_over_max:120 isb_target_below_floor:21 isb_tf_conflict:48 isb_tf_conflict_both_boxes:48 isb_trigger_side:10 isb_trigger_side_mismatch:9 isbrev_box_blocked:5 isbrev_box_blocked_30m:1 isbrev_day_off:10 isbrev_htf_side_mismatch:1 isbrev_stop_over_max:20 isbrev_tf_conflict:10 keypair_candle_too_big:23 level_invalid_first_touch:126 level_visit_cap:1 near_box:36 phl_15m_box_ban:7 phl_30m_box_ban:6 phl_5m_box_ban:22 phl_degenerate_geometry:1 phl_mid_range_boxed:29 phl_no_old_extreme_on_side:15 phl_not_higher_low:45 phl_not_location:37 phl_stop_ceiling:38 phl_target_below_floor:20 phl_tf_conflict:183 phl_too_close_to_extreme:2 rolling_htf_conflict:92 rolling_keypair_candle_too_big:1 rolling_near_box:11 rolling_phl_15m_box_ban:9 rolling_phl_30m_box_ban:3 rolling_phl_5m_box_ban:33 rolling_phl_degenerate_geometry:1 rolling_phl_mid_range_boxed:47 rolling_phl_spent_stop_cap:5 rolling_phl_stop_ceiling:12 rolling_phl_target_below_floor:8 rolling_phl_too_close_to_extreme:4 rolling_phl_trigger_side:1 rolling_room:7 room:3 tl_first_test_level_preferred:trendline:short:1793837760000:1793838600000:1 trendline_crossed_between_anchors:52} · kernel fills {} · trader refusals {placement_refused_data_integrity:1 stale_data_episodes:1} · authored 0 · placed 0 · filled 0",
	"11-06 13:45:00 | hoang | bars 1238 · entry intents 1 · other actions 132 · kernel refusals {box_day_off:49 box_isb_ban:25 box_isb_ban_15m:11 box_isb_ban_30m:9 box_ping_pong:3 box_stop_ceiling:4 inside_any_box:1 isb_box_blocked:15 isb_box_blocked_15m:3 isb_box_blocked_30m:4 isb_htf_conflict:184 isb_htf_side_mismatch:24 isb_stop_over_max:216 isb_target_below_floor:32 isb_tf_conflict:166 isb_tf_conflict_both_boxes:97 isb_trigger_side:24 isb_trigger_side_mismatch:11 isbrev_box_blocked:5 isbrev_box_blocked_30m:1 isbrev_day_off:49 isbrev_htf_side_mismatch:1 isbrev_stop_over_max:31 isbrev_target_below_floor:1 isbrev_tf_conflict:40 keypair_candle_too_big:27 level_invalid_first_touch:258 level_visit_cap:3 near_box:51 phl_15m_box_ban:43 phl_30m_box_ban:24 phl_5m_box_ban:61 phl_degenerate_geometry:3 phl_mid_range_boxed:78 phl_no_old_extreme_on_side:20 phl_not_higher_low:45 phl_not_location:88 phl_stop_ceiling:94 phl_target_below_floor:25 phl_tf_conflict:438 phl_too_close_to_extreme:4 rolling_htf_conflict:190 rolling_keypair_candle_too_big:3 rolling_near_box:31 rolling_phl_15m_box_ban:15 rolling_phl_30m_box_ban:4 rolling_phl_5m_box_ban:57 rolling_phl_degenerate_geometry:2 rolling_phl_mid_range_boxed:94 rolling_phl_spent_stop_cap:10 rolling_phl_stop_ceiling:29 rolling_phl_target_below_floor:9 rolling_phl_too_close_to_extreme:10 rolling_phl_trigger_side:1 rolling_room:7 room:3 tl_first_test_level_preferred:trendline:short:1793837760000:1793838600000:1 trendline_crossed_between_anchors:96} · kernel fills {} · trader refusals {cancel_refused_row_gone:2 news_hold:30 placement_refused_data_integrity:1 placement_refused_stale_data:1 stale_data_episodes:3} · authored 0 · placed 0 · filled 0",
}

var (
	m469Boot     = time.Date(2026, 11, 4, 17, 10, 29, 0, kernel.CTLocation())
	funnelLineRe = regexp.MustCompile(`^(\d\d-\d\d \d\d:\d\d:\d\d) \| ([^|]+) \| bars (\d+) · entry intents (\d+) · other actions \d+ · kernel refusals \{([^}]*)\} · kernel fills \{[^}]*\} · trader refusals \{([^}]*)\} · authored (\d+) · placed (\d+) · filled (\d+)$`)
)

type funnelLogLine struct {
	at                       time.Time
	trader                   string
	bars, entries            int
	kernel, trader2          map[string]int
	authored, placed, filled int
}

// parseCounts reads "{a:1 b:2}" contents; a reason may itself hold colons
// (tl_first_test_level_preferred:trendline:...:1), so the count is after the LAST colon.
func parseCounts(t *testing.T, s string) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, f := range strings.Fields(s) {
		i := strings.LastIndexByte(f, ':')
		n, err := strconv.Atoi(f[i+1:])
		if i <= 0 || err != nil {
			t.Fatalf("bad count %q", f)
		}
		out[f[:i]] = n
	}
	return out
}

func parseFunnelLine(t *testing.T, l string) funnelLogLine {
	t.Helper()
	m := funnelLineRe.FindStringSubmatch(l)
	if m == nil {
		t.Fatalf("fixture line does not parse: %.120s", l)
	}
	ts, err := time.ParseInLocation("2006-01-02 15:04:05", "2026-"+m[1], kernel.CTLocation())
	if err != nil {
		t.Fatal(err)
	}
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	return funnelLogLine{at: ts, trader: m[2], bars: atoi(m[3]), entries: atoi(m[4]),
		kernel: parseCounts(t, m[5]), trader2: parseCounts(t, m[6]),
		authored: atoi(m[7]), placed: atoi(m[8]), filled: atoi(m[9])}
}

// whyAT builds a mentor-mode trader that started at boot.
func whyAT(id, name string, boot time.Time) *AutoTrader {
	return &AutoTrader{
		id: id, name: name, startTime: boot,
		config:     AutoTraderConfig{StrategyConfig: &store.StrategyConfig{RiskControl: store.RiskControlConfig{MentorMode: true}}},
		mentorEval: mentor.New(mentor.DefaultConfig()),
	}
}

// replayFunnel drives at's funnel to the numbers of every fixture line for
// its trader up to (and including) until: the kernel and trader ledgers are
// set to the logged values, then one production tick carries the bar and
// entry-intent deltas, then the stage bumps carry the authored/placed/filled
// deltas.
func replayFunnel(t *testing.T, at *AutoTrader, until time.Time) {
	t.Helper()
	for _, raw := range m469FunnelLog {
		l := parseFunnelLine(t, raw)
		if l.trader != at.name || l.at.After(until) {
			continue
		}
		at.mentorEval.State.Refusals = l.kernel
		at.mentorCounterMu.Lock()
		at.mentorCounters = copyCounts(l.trader2)
		at.mentorCounterMu.Unlock()

		f := &at.mentorFunnel
		f.mu.Lock()
		rolls := f.dayKey != kernel.CMESessionDayKey(l.at)
		bars, entries := f.bars, f.entries
		f.mu.Unlock()
		if rolls {
			bars, entries = 0, 0
		}
		intents := make([]mentor.Intent, l.entries-entries)
		for i := range intents {
			intents[i] = mentor.Intent{Action: mentor.PlaceStopEntry}
		}
		at.mentorFunnelTickAt(l.at, l.bars-bars, intents)

		f.mu.Lock()
		a, p, fl := f.authored, f.placed, f.filled
		f.mu.Unlock()
		for ; a < l.authored; a++ {
			f.bumpAuthored()
		}
		for ; p < l.placed; p++ {
			f.bumpPlaced()
		}
		for ; fl < l.filled; fl++ {
			f.bumpFilled()
		}
	}
}

// whyReport renders the report's why-no-trade lines (production Collect +
// Lines) for the traders, the AI ones read from st.
func whyReport(st *store.Store, sess datareport.Session, traders ...*AutoTrader) []string {
	src := datareport.Sources{Funnel: func() []datareport.TraderFunnel {
		var out []datareport.TraderFunnel
		for _, at := range traders {
			out = append(out, at.WhyNoTradeInput())
		}
		return out
	}}
	if st != nil {
		src.DB = st.GormDB()
	}
	lines := datareport.Collect(src, sess).Lines()
	return lines[1 : 1+len(traders)] // straight under the header
}

func whyStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "why.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// logCycles writes n decision rows for a trader, one a minute from start.
func logCycles(t *testing.T, st *store.Store, traderID string, start time.Time, n int, ok bool, class, msg string) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := st.Decision().LogDecision(&store.DecisionRecord{TraderID: traderID, CycleNumber: i + 1,
			Timestamp: start.Add(time.Duration(i) * time.Minute), Success: ok, ErrorClass: class, ErrorMessage: msg}); err != nil {
			t.Fatalf("LogDecision: %v", err)
		}
	}
}

const (
	m469ID12345 = "8d5c8af5_396db319-d3fe-4d63-9b97-516ff0008f53_deepseek_0751c0b6_1791870197"
	m469Msg402  = `Failed to get AI decision: AI API call failed: API returned error (status 402): {"error":{"message":"Insufficient Balance"}}`
	m469MsgCtx  = `Failed to build trading context: failed to get account balance: ninjatrader/tcp: balance unavailable for bound account "SimAccount1" — no snapshot`
)

// The 11-05 session (11-04 17:00 -> 11-05 16:00 CT) with its REAL numbers.
// The process booted at 17:10, inside the session, so the mentor lines say
// "since boot 17:10 CT". 12345 is an AI-planner trader: its 460 cycles (439
// DeepSeek 402s + 21 "no balance snapshot" context failures, counted from
// data.db) all failed. Its rows before the session and after the close, and
// the mentor traders' own "skipped" rows, must not be counted.
func TestWhyNoTrade1105RealSession(t *testing.T) {
	sess := datareport.SessionFor(time.Date(2026, 11, 5, 16, 10, 0, 0, kernel.CTLocation()))
	hoang := whyAT("hoang-id", "hoang", m469Boot)
	mess := whyAT("mess-id", "4567489 mess.", m469Boot)
	ai := whyAT(m469ID12345, "12345", m469Boot)
	ai.config.StrategyConfig.RiskControl.MentorMode = false
	replayFunnel(t, hoang, sess.End)
	replayFunnel(t, mess, sess.End)

	st := whyStore(t)
	logCycles(t, st, m469ID12345, sess.Start.Add(30*time.Second), 439, false, "ai_payment_402", m469Msg402)
	logCycles(t, st, m469ID12345, sess.Start.Add(10*time.Hour+30*time.Second), 21, false, "", m469MsgCtx)
	logCycles(t, st, m469ID12345, sess.Start.Add(-30*time.Minute), 5, true, "", "")     // the session before
	logCycles(t, st, m469ID12345, sess.End.Add(time.Minute), 5, true, "", "")           // after the close
	logCycles(t, st, "hoang-id", sess.Start.Add(time.Hour), 50, true, "", "")           // mentor "skipped" rows
	logCycles(t, st, "hoang-id", sess.Start.Add(2*time.Hour), 3, false, "", m469MsgCtx) // mentor context failures

	got := whyReport(st, sess, hoang, mess, ai)
	// Mentor traders first (by name), then the AI-planner traders.
	want := []string{
		"4567489 mess.: setups 3, placed 0, filled 0 · trader refusals window_refused 2, placement_refused_data_integrity 1, stale_data_episodes 1 · top refusals phl_tf_conflict 183, level_invalid_first_touch 126, isb_stop_over_max 120 (since boot 17:10 CT)",
		"hoang: setups 3, placed 2, filled 0 · trader refusals placement_refused_data_integrity 1, stale_data_episodes 1 · top refusals phl_tf_conflict 183, level_invalid_first_touch 126, isb_stop_over_max 120 (since boot 17:10 CT)",
		"12345: AI failed 460/460 (402)",
	}
	assertLines(t, got, want)
}

// The 11-06 session (11-05 17:00 -> 11-06 16:00 CT), same process, read at
// its last funnel line (13:45, the release #62 restart). The kernel and
// trader ledgers kept counting across the 17:00 roll, so the session's
// figures are the 13:45 ledgers MINUS the ledgers at the last tick before the
// roll (11-05 16:59:59). Raw, the top three would be phl_tf_conflict 438,
// level_invalid_first_touch 258, isb_stop_over_max 216 (two sessions summed).
func TestWhyNoTrade1106RollDiffsAgainstSessionStart(t *testing.T) {
	sess := datareport.SessionFor(time.Date(2026, 11, 6, 16, 10, 0, 0, kernel.CTLocation()))
	hoang := whyAT("hoang-id", "hoang", m469Boot)
	replayFunnel(t, hoang, time.Date(2026, 11, 6, 13, 45, 0, 0, kernel.CTLocation()))

	in := hoang.WhyNoTradeInput()
	if in.DayKey != "2026-11-05" || !in.Since.Equal(sess.Start) || in.CountersRestarted {
		t.Fatalf("day %s since %v restarted %v, want 2026-11-05 / %v / false", in.DayKey, in.Since, in.CountersRestarted, sess.Start)
	}
	if got := in.KernelRefusals["isb_stop_over_max"]; got != 96 {
		t.Fatalf("isb_stop_over_max this session = %d, want 216-120 = 96", got)
	}
	got := whyReport(nil, sess, hoang)
	want := []string{
		"hoang: setups 1, placed 0, filled 0 · trader refusals news_hold 30, cancel_refused_row_gone 2, stale_data_episodes 2 · top refusals phl_tf_conflict 255, level_invalid_first_touch 132, isb_tf_conflict 118",
	}
	assertLines(t, got, want)
}

// A process restart loses every in-memory counter: the new process's funnel
// holds only what it saw since its own start and says so. Here the 11-06
// restart at 13:45:53 (release #62): one tick at 13:52 after a fresh boot.
func TestWhyNoTradeRestartStartsOverSinceBoot(t *testing.T) {
	sess := datareport.SessionFor(time.Date(2026, 11, 6, 16, 10, 0, 0, kernel.CTLocation()))
	boot := time.Date(2026, 11, 6, 13, 45, 53, 0, kernel.CTLocation())
	at := whyAT("hoang-id", "hoang", boot)
	at.mentorEval.State.Refusals = map[string]int{"phl_tf_conflict": 2}
	at.mentorFunnelTickAt(time.Date(2026, 11, 6, 13, 52, 0, 0, kernel.CTLocation()), 1, nil)
	assertLines(t, whyReport(nil, sess, at), []string{
		"hoang: setups 0, placed 0, filled 0 · top refusals phl_tf_conflict 2 (since boot 13:45 CT)",
	})
}

// A ledger rebuilt INSIDE the session (the evaluator recreated) counts lower
// than the roll's baseline: diffing would give negatives and hide the reason,
// so the whole ledger is this session's and the line says the figure is partial.
func TestWhyNoTradeLedgerRebuiltMidSession(t *testing.T) {
	sess := datareport.SessionFor(time.Date(2026, 11, 6, 16, 10, 0, 0, kernel.CTLocation()))
	at := whyAT("hoang-id", "hoang", m469Boot)
	at.mentorEval.State.Refusals = map[string]int{"phl_tf_conflict": 183, "isb_stop_over_max": 120}
	at.mentorFunnelTickAt(time.Date(2026, 11, 5, 15, 59, 0, 0, kernel.CTLocation()), 1, nil)
	at.mentorEval.State.Refusals = map[string]int{"phl_tf_conflict": 190, "isb_stop_over_max": 120}
	at.mentorFunnelTickAt(time.Date(2026, 11, 5, 17, 1, 0, 0, kernel.CTLocation()), 1, nil) // the roll
	at.mentorEval = mentor.New(mentor.DefaultConfig())                                      // rebuilt
	at.mentorEval.State.Refusals = map[string]int{"isb_stop_over_max": 4}
	at.mentorFunnelTickAt(time.Date(2026, 11, 6, 9, 0, 0, 0, kernel.CTLocation()), 1, nil)
	assertLines(t, whyReport(nil, sess, at), []string{
		"hoang: setups 0, placed 0, filled 0 · top refusals isb_stop_over_max 4 (refusal counters restarted mid-session: partial)",
	})
}

// Mentor mode off: no funnel (the report reads the AI cycles instead).
func TestWhyNoTradeMentorOffHasNoFunnel(t *testing.T) {
	at := whyAT("x", "x", m469Boot)
	at.config.StrategyConfig.RiskControl.MentorMode = false
	at.mentorFunnelTickAt(time.Date(2026, 11, 5, 9, 0, 0, 0, kernel.CTLocation()), 1, nil)
	if in := at.WhyNoTradeInput(); in.Mentor || in.Have || in.ID != "x" || in.Name != "x" {
		t.Fatalf("mentor-off input = %+v, want ID/Name only", in)
	}
}

func assertLines(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d\n got: %s\nwant: %s", i, got[i], want[i])
		}
	}
}
