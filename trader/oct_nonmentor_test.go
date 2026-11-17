package trader

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vl/store"
)

// TestOctNonMentorConfigUnchanged — oct-exact owner order: MENTOR MODE ONLY. An
// AI-mode strategy (MentorMode false) must behave exactly as before every changed
// knob path: the mentor entry points stay off, the changed mentor knobs are never
// consulted, and a default strategy config serialises with none of the new knobs
// (so the stored JSON of every non-mentor strategy is byte-identical).
func TestOctNonMentorConfigUnchanged(t *testing.T) {
	at := mentoredTrader(t, store.RiskControlConfig{MentorMode: false})

	if at.mentorEnabled() {
		t.Fatal("AI-mode trader reports mentor enabled")
	}
	if at.mentorEventPassAt(time.Now()) {
		t.Fatal("AI-mode trader ran a mentor event pass")
	}
	if _, ok := at.mentorMaxContracts(); ok {
		t.Fatal("AI-mode trader took the mentor contract ceiling")
	}
	// Mentor trader-side gates must be inert (pre-change they returned false for
	// a non-mentor config too: no stored news/done switch, no calendar read).
	if hold, _ := at.mentorNewsGate(); hold {
		t.Error("AI-mode trader held by the mentor news gate")
	}
	if refuse, _ := at.mentorDoneAfterWinGate(); refuse {
		t.Error("AI-mode trader refused by done_after_win")
	}

	// A default strategy config carries none of the oct-exact knobs: every one is
	// an omitempty pointer / zero, so non-mentor JSON does not change.
	raw, err := json.Marshal(store.GetDefaultStrategyConfig("en"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{
		"mentor_htf_gate_enabled", "mentor_day_gate_enabled", "mentor_swing4h_enabled",
		"mentor_orb_part2_enabled", "mentor_open5m_gate_enabled", "mentor_news_hold_enabled",
		"mentor_news_flatten_enabled", "mentor_loss_box_enabled", "mentor_15m_box_no_trade",
		"mentor_htf_box_gates", "mentor_tf_conflict", "mentor_mtf_confluence",
		"mentor_rolling_level_touch_owns", "keypair_rule_enabled", "mentor_trigger_retest_wrong_way",
		"mentor_box_ping_pong", "mentor_box_room_multiple", "trendline_max_lines_per_anchor",
		"mentor_isb_room_multiple", "mentor_level_first_touch_lock", "mentor_isbrev_fill_close", "mentor_isbrev_old_extreme_room",
		"mentor_isb_just_broken_level", "mentor_isb_just_broken_scope", "mentor_opening_isb_box_no_trade", "mentor_isb_opposite_arm_guard", "mentor_bar_close_guard", "mentor_ftgl_body_edge_partner", "mentor_leg1_fixed_pts",
		"mentor_counter_5m_scalp_pts", "mentor_resonance_trail", "mentor_runner_1to1_at_fill", "mentor_resonance_1lot_be_guard",
		"mentor_box_carry_first_latched", "mentor_box_counter_5m_scalp",
	} {
		if strings.Contains(string(raw), `"`+k+`"`) {
			t.Errorf("default (AI-mode) strategy config serialises the new mentor knob %q", k)
		}
	}
}
