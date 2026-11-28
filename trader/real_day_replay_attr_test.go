package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
)

// m431 — the real-day replay's refusal ATTRIBUTION is itself pinned (the
// replay is opt-in via REAL_DAY_REPLAY, so these run in every `go test`).
// Live trade 690 (10-22 18:26 CT short 29080) was refused by the kernel with
// isbrev_target_below_floor, a name the old fixed key list did not carry, and
// the harness called it "no named refusal". The harness must now attribute
// every kernel refusal inside the execution-frame window — and must still FAIL
// (return a "no ..." string) when nothing names the trade.

func rdrCT(t *testing.T, mmdd string, hh, mm int) time.Time {
	t.Helper()
	d, err := time.ParseInLocation("01-02", mmdd, kernel.CTLocation())
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(2026, d.Month(), d.Day(), hh, mm, 0, 0, kernel.CTLocation())
}

func TestM431RefusalAttributionNamesAnyKernelRefusalInTheFrameWindow(t *testing.T) {
	refs := []rdrBarRef{
		{ct: "10-22 18:24", kernel: map[string]int{"isbrev_target_below_floor": 1, "near_box": 1}},
	}
	// The live arm was authored on the 18:26 execution-frame close; the replay's
	// refusal landed two minutes earlier — inside the 5m frame window.
	got := rdrAttribute(rdrCT(t, "10-22", 18, 26), refs)
	if strings.HasPrefix(got, "no ") || !strings.Contains(got, "isbrev_target_below_floor=1") {
		t.Fatalf("trade 690 shape must be attributed to isbrev_target_below_floor, got %q", got)
	}
	// An ordinary kernel refusal outside the old fixed list also names a trade.
	refs = []rdrBarRef{{ct: "10-21 22:43", kernel: map[string]int{"phl_no_old_extreme_on_side": 2}}}
	if got := rdrAttribute(rdrCT(t, "10-21", 22, 43), refs); strings.HasPrefix(got, "no ") {
		t.Fatalf("phl_no_old_extreme_on_side must name trade 683, got %q", got)
	}
}

func TestM431RefusalAttributionPicksTheNearestBar(t *testing.T) {
	refs := []rdrBarRef{
		{ct: "10-22 18:22", kernel: map[string]int{"room": 1}},
		{ct: "10-22 18:27", kernel: map[string]int{"isbrev_target_below_floor": 1}},
	}
	got := rdrAttribute(rdrCT(t, "10-22", 18, 26), refs)
	if !strings.Contains(got, "isbrev_target_below_floor") || strings.Contains(got, "room") {
		t.Fatalf("the nearest bar (18:27) must win, got %q", got)
	}
}

// The harness must stay able to FAIL: a trade with no refusal in the window, a
// refusal only OUTSIDE the window, or only arm-management races / non-refusal
// tallies, is UNEXPLAINED.
func TestM431RefusalAttributionStillFailsWhenUnexplained(t *testing.T) {
	at := rdrCT(t, "10-22", 18, 26)
	cases := map[string][]rdrBarRef{
		"empty":          nil,
		"outside window": {{ct: "10-22 18:00", kernel: map[string]int{"room": 3}}},
		"only cancel races and tallies": {{ct: "10-22 18:26",
			trader: map[string]int{"cancel_refused_row_gone": 2, "cancel_refused_unknown_arm": 1, "extend_refused": 4, "extend_ok": 3, "intent_PLH": 1, "tier_base": 1}}},
	}
	for name, refs := range cases {
		if got := rdrAttribute(at, refs); !strings.HasPrefix(got, "no ") {
			t.Errorf("%s: must stay UNEXPLAINED (a \"no ...\" string), got %q", name, got)
		}
	}
}
