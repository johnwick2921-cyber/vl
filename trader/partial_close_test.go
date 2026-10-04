package trader

import (
	"strings"
	"testing"
	"time"

	nt "vl/provider/ninjatrader"
)

// ── PARTIAL CLOSE (2026-10-03) — the decision cores ────────────────────────
//
// The pure seams are the production call sites' decision cores:
// reduceQuantityCheck is the pre-send guard, resizeAfterReduceDecision is the
// post-fill stop policy, findProtectiveLeg is the leg discovery.

func TestReduceQuantityCheck(t *testing.T) {
	cases := []struct {
		name    string
		openQty int
		wantQty int
		ok      bool
		why     string
	}{
		{"reduce 3 of 5", 5, 3, true, "leaves 2"},
		{"reduce 2 of 5", 5, 2, true, "leaves 3"},
		{"quantity >= open is REFUSED", 5, 5, false, "full close"},
		{"quantity > open is REFUSED", 5, 6, false, "full close"},
		{"zero quantity refused", 5, 0, false, "positive"},
		{"negative quantity refused", 5, -1, false, "positive"},
		{"no open position refused", 0, 1, false, "no open position"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, why := reduceQuantityCheck(tc.openQty, tc.wantQty)
			if ok != tc.ok {
				t.Fatalf("want ok=%v, got ok=%v (why=%q)", tc.ok, ok, why)
			}
			if !strings.Contains(why, tc.why) {
				t.Fatalf("why %q must mention %q", why, tc.why)
			}
		})
	}
}

// THE TWO MUTANTS THE DISPATCH NAMES, PINNED IN v2 SHAPE:
//   - "ignore the quantity (flatten all)": the decision refuses whenever
//     quantity >= open, and the remaining is open-want.
//   - "skip the stop resize": the resize is now the AddOn's IN-PLACE shrink
//     verified on the snapshot — an unverified bracket FAILS CLOSED (flatten),
//     never a silent no-op.
func TestBracketVerifyPinsTheTwoMutants(t *testing.T) {
	if got, _ := reduceQuantityCheck(5, 3); !got {
		t.Fatalf("fixture: reduce 3 of 5 must pass")
	}
	book := []nt.NT8Order{
		{OrderID: "s", Symbol: "MNQ", Name: "sig-1-sl", State: "Working", Type: "stop", Quantity: 2},
		{OrderID: "t", Symbol: "MNQ", Name: "sig-1-tp", State: "Working", Type: "limit", Quantity: 2},
	}
	if ok, why := verifyBracketResize(book, "MNQ", 2); !ok {
		t.Fatalf("a bracket at the remaining quantity must verify, got %q", why)
	}
	// The mutant that ignores the quantity: a pair still at the OLD size must
	// NOT verify — the remainder would be mis-protected.
	stale := []nt.NT8Order{
		{OrderID: "s", Symbol: "MNQ", Name: "sig-1-sl", State: "Working", Type: "stop", Quantity: 5},
		{OrderID: "t", Symbol: "MNQ", Name: "sig-1-tp", State: "Working", Type: "limit", Quantity: 5},
	}
	if ok, _ := verifyBracketResize(stale, "MNQ", 2); ok {
		t.Fatalf("an UNSHRUNK pair must not verify against the remainder")
	}
	// The mutant that skips the resize: an expired, unverified window must be
	// flatten — the only other exits are wait and confirmed.
	if got := bracketVerifyOutcome(false, true); got != "flatten" {
		t.Fatalf("an expired unverified window must flatten, got %q", got)
	}
	if got := bracketVerifyOutcome(true, false); got != "confirmed" {
		t.Fatalf("a verified window must confirm, got %q", got)
	}
}

func TestVerifyBracketResizeRejectsAbsence(t *testing.T) {
	if ok, _ := verifyBracketResize([]nt.NT8Order{}, "MNQ", 2); ok {
		t.Fatalf("no legs at all must not verify")
	}
	if ok, _ := verifyBracketResize([]nt.NT8Order{
		{OrderID: "s", Symbol: "MNQ", Name: "sig-1-sl", State: "Working", Quantity: 2},
	}, "MNQ", 2); ok {
		t.Fatalf("a missing TP leg must not verify")
	}
	if ok, _ := verifyBracketResize([]nt.NT8Order{
		{OrderID: "s", Symbol: "MNQ", Name: "sig-1-sl", State: "Working", Quantity: 2},
		{OrderID: "t", Symbol: "MNQ", Name: "sig-1-tp", State: "Working", Quantity: 2},
	}, "MNQ", 0); ok {
		t.Fatalf("a non-positive remainder must not verify")
	}
}

func TestKnobsAreSplitAndDefaultOff(t *testing.T) {
	t.Setenv("PARTIAL_CLOSE_ENABLED", "")
	t.Setenv("MENTOR_STOP_LIMIT", "")
	t.Setenv("CANCEL_CONFIRM_REQUIRE_REPORT", "")
	if partialCloseEnabled() || stopLimitEntriesEnabled() || cancelConfirmRequireReport() {
		t.Fatalf("all three knobs must default OFF")
	}
	t.Setenv("PARTIAL_CLOSE_ENABLED", "1")
	if !partialCloseEnabled() {
		t.Fatalf("the partial-close knob must turn ON alone")
	}
	if cancelConfirmRequireReport() {
		t.Fatalf("the partial-close knob must NOT turn the report regime on — they are split")
	}
	t.Setenv("MENTOR_STOP_LIMIT", "true")
	if !stopLimitEntriesEnabled() {
		t.Fatalf("the stop-limit knob must turn ON")
	}
}

// CALL-SITE PINS on onReduceFill:
//   - a fill with a positive remaining and a matching bracket_qty must OPEN
//     the verification window (removing the recordBracketVerify call goes
//     RED);
//   - a fill with remaining=-1 (absent) must fail closed, never be read as
//     flat;
//   - a fill whose bracket_qty disagrees with the remaining must fail closed
//     without waiting for the snapshot.
func TestOnReduceFillOpensTheVerifyWindow(t *testing.T) {
	ResetBracketVerifyForTest()
	t.Setenv("PARTIAL_CLOSE_ENABLED", "1")
	defer t.Setenv("PARTIAL_CLOSE_ENABLED", "")
	at := &AutoTrader{id: "t1"}
	at.onReduceFill(nil, nt.ReduceFillPayload{
		ClientID: "rx-1", Symbol: "MNQ", Side: "long", Quantity: 3,
		FillPrice: 100.25, Remaining: 2, BracketQty: 2,
	})
	if due := bracketVerifyDue(time.Now().Add(4 * snapshotMaxAge())); len(due) != 1 || due[0] != "rx-1" {
		t.Fatalf("the fill must open the verification window, due=%v", due)
	}
}

func TestOnReduceFillAbsentRemainingFailsClosed(t *testing.T) {
	ResetBracketVerifyForTest()
	t.Setenv("PARTIAL_CLOSE_ENABLED", "1")
	defer t.Setenv("PARTIAL_CLOSE_ENABLED", "")
	at := &AutoTrader{id: "t1"}
	at.onReduceFill(nil, nt.ReduceFillPayload{
		ClientID: "rx-1", Symbol: "MNQ", Side: "long", Quantity: 3,
		FillPrice: 100.25, Remaining: -1, BracketQty: -1,
	})
	if due := bracketVerifyDue(time.Now().Add(4 * snapshotMaxAge())); len(due) != 0 {
		t.Fatalf("an absent remaining must NOT open a verify window, due=%v", due)
	}
}

func TestOnReduceFillMismatchedBracketFailsClosed(t *testing.T) {
	ResetBracketVerifyForTest()
	t.Setenv("PARTIAL_CLOSE_ENABLED", "1")
	defer t.Setenv("PARTIAL_CLOSE_ENABLED", "")
	at := &AutoTrader{id: "t1"}
	at.onReduceFill(nil, nt.ReduceFillPayload{
		ClientID: "rx-1", Symbol: "MNQ", Side: "long", Quantity: 3,
		FillPrice: 100.25, Remaining: 2, BracketQty: 5, // the AddOn reports a wrong shrink
	})
	if due := bracketVerifyDue(time.Now().Add(4 * snapshotMaxAge())); len(due) != 0 {
		t.Fatalf("a mismatched bracket_qty must fail closed NOW, not open a window, due=%v", due)
	}
}

// The verification outcome map is PURE and exhaustive.
func TestBracketVerifyOutcomeIsExhaustive(t *testing.T) {
	for _, tc := range []struct {
		verified, expired bool
		want              string
	}{
		{false, false, "wait"},
		{true, false, "confirmed"},
		{false, true, "flatten"},
		{true, true, "confirmed"},
	} {
		if got := bracketVerifyOutcome(tc.verified, tc.expired); got != tc.want {
			t.Fatalf("outcome(%v,%v) = %q, want %q", tc.verified, tc.expired, got, tc.want)
		}
	}
}
