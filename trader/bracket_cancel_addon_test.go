// BRACKET-OCO SEPARATION (2026-09-07) — the AddOn's cancel path, pinned from Go.
//
// There is no C# test harness in this repo (no .csproj, no .sln, no *Test*.cs);
// NinjaScript compiles only inside NT8. So this is a SOURCE pin on the shipped
// .cs text, the same technique wave_b_addon_slot_test.go uses, and it is stated
// as such in the report. The far-side proof is F2's received frames (A20).

package trader

import (
	"regexp"
	"strings"
	"testing"
)

// stripCSharpComments removes // and /* */ comments. The pin must judge CODE:
// this file's last ordering pin was satisfied by the prose above the call it
// meant to check, and the first draft of THIS pin failed on its own explanatory
// comment. A pin that reads commentary is measuring the wrong artifact in
// whichever direction it happens to err.
func stripCSharpComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	for i := 0; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "//"):
			j := strings.IndexByte(src[i:], '\n')
			if j < 0 {
				return b.String()
			}
			i += j // leave the newline so line structure survives
		case strings.HasPrefix(src[i:], "/*"):
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				return b.String()
			}
			i += 2 + j + 2
		default:
			b.WriteByte(src[i])
			i++
		}
	}
	return b.String()
}

// csharpMethodBody returns the EXECUTABLE text of one C# method, from its
// signature to the next member declaration at method indentation. Crude, and
// sufficient: the AddOn is one class with uniform 8-space member indentation.
func csharpMethodBody(t *testing.T, src, signature string) string {
	t.Helper()
	i := strings.Index(src, signature)
	if i < 0 {
		t.Fatalf("method %q not found in the AddOn source — the pin cannot read what it exists to check", signature)
	}
	rest := src[i+len(signature):]
	// the next member of the class: a blank line followed by 8 spaces and an
	// access modifier or a comment block that introduces one.
	end := regexp.MustCompile(`\n        (private|public|internal|protected) `).FindStringIndex(rest)
	if end == nil {
		return stripCSharpComments(rest)
	}
	return stripCSharpComments(rest[:end[0]])
}

// TestCancellingAnEntryNeverTouchesItsBracket — E1, redefined.
//
// THE INCIDENT. 2026-09-06 23:37:02 a cancel_order arrived for signal
// aa07e583, whose entry had filled ~100 seconds earlier. HandleCancelOrder was
// a TWO-PART cancel: the resting entry from workingEntries, and then —
// unconditionally — the SL and TP from placedBrackets. The entry was already
// gone (workingEntries.Remove fires on the fill), so part one did nothing and
// ONLY THE SECOND HALF RAN. It cancelled accepted stop 29554 and working target
// 29623. Position 592 then ran naked for 8h19m, through the Monday open.
//
// This was never OCO propagation: the entry has always carried an empty OCO
// group and the children their own shared "<signal>-exit" id. Our own code
// reached across and killed them.
//
// THE RULE (owner, 2026-09-07): "HandleCancelOrder cancels the ENTRY only; it
// never walks placedBrackets. Cancelling a bracket leg is its own explicit
// call, never a side effect of cancelling an entry."
func TestCancellingAnEntryNeverTouchesItsBracket(t *testing.T) {
	body := csharpMethodBody(t, addonSource(t), "private void HandleCancelOrder(")

	for _, forbidden := range []string{"placedBrackets", "SlOrder", "TpOrder"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("HandleCancelOrder still reaches %s — cancelling an entry must not be able to "+
				"reach a protective order. This is the 2026-09-06 naked-stop path: the entry had "+
				"already filled, so this was the ONLY half that ran.", forbidden)
		}
	}
	if !strings.Contains(body, "workingEntries") {
		t.Error("HandleCancelOrder no longer looks at workingEntries — it must still cancel the ENTRY")
	}
}

// TestBracketLegsAreStillCancelledWhereThatIsThePurpose — the other half of the
// rule, so the fix cannot be "stop cancelling brackets anywhere". Closing a
// position and sweeping a netted-flat symbol BOTH exist to retire protective
// orders; those are explicit calls and must survive.
func TestBracketLegsAreStillCancelledWhereThatIsThePurpose(t *testing.T) {
	src := addonSource(t)
	for _, sig := range []string{
		"private void CancelBracketForClose(",
		"private void SweepBracketsOnNettingFlat(",
	} {
		if !strings.Contains(src, sig) {
			continue // named differently; the count check below still governs
		}
	}
	// Cancelling bracket legs must remain possible — but in strictly fewer
	// places than before, and never from the cancel-an-entry handler.
	n := strings.Count(src, "toCancel.Add(pb.SlOrder)")
	if n < 2 {
		t.Fatalf("only %d site(s) still cancel a bracket's stop leg — the close path and the "+
			"netting-flat sweep must both keep doing so; protection must be retired when a "+
			"position ends", n)
	}
}

// TestProtectiveOrdersAreGTC — D6/E6, 2026-09-07.
//
// The protective stop and target were TimeInForce.Day. A Day order is dropped
// at session end; GTC survives NT8's daily maintenance window. A stop that
// expires while the position it protects does not is a naked position on a
// timer — the same exposure as the 09-06 incident, arriving by the calendar
// instead of by a cancel.
//
// The ENTRY stays Day on purpose: an unfilled entry from yesterday's plan must
// not wake up and fire into a market the plan never saw.
func TestProtectiveOrdersAreGTC(t *testing.T) {
	src := addonSource(t)
	body := csharpMethodBody(t, src, "private void SubmitBracketOnEntryFill(")

	for _, leg := range []string{`signalId + "-sl"`, `signalId + "-tp"`} {
		i := strings.Index(body, leg)
		if i < 0 {
			t.Fatalf("could not find the %s CreateOrder call — the pin cannot read what it checks", leg)
		}
		// the CreateOrder call this leg name belongs to starts before it
		start := strings.LastIndex(body[:i], "CreateOrder(")
		if start < 0 {
			t.Fatalf("no CreateOrder call precedes %s", leg)
		}
		call := body[start:i]
		if !strings.Contains(call, "TimeInForce.Gtc") {
			t.Errorf("the protective leg %s is not GTC — a Day protective order is dropped at "+
				"session end while the position it protects survives. call: %s", leg, strings.TrimSpace(call))
		}
	}

	// The entry must NOT have been swept up in the change.
	entry := csharpMethodBody(t, src, "private void HandleSignal(")
	if strings.Contains(entry, "TimeInForce.Gtc") {
		t.Error("the ENTRY order became GTC — an unfilled entry must die with its session, " +
			"not wake up tomorrow and fire into a market its plan never saw")
	}
}

// TestBracketQuantityComesFromTheFillEvent — D2/E2, 2026-09-07.
//
// The bracket quantity was read from the cached PendingBracket captured when
// the entry was SUBMITTED. Staff guidance is to update from the passed-in event
// parameters, not from a cached mirror or the return of Submit: event ordering
// across OrderUpdate / ExecutionUpdate / PositionUpdate is not guaranteed, and
// a part-fill makes the submitted quantity a different number from the one that
// actually needs protecting.
func TestBracketQuantityComesFromTheFillEvent(t *testing.T) {
	src := addonSource(t)

	sig := regexp.MustCompile(`private void SubmitBracketOnEntryFill\(([^)]*)\)`).FindStringSubmatch(src)
	if sig == nil {
		t.Fatal("SubmitBracketOnEntryFill not found")
	}
	params := sig[1]
	if !strings.Contains(params, "int filledQty") {
		t.Fatalf("SubmitBracketOnEntryFill(%s) does not take the FILLED quantity — it is still "+
			"protecting the quantity we asked for rather than the quantity we got", params)
	}

	body := csharpMethodBody(t, src, "private void SubmitBracketOnEntryFill(")
	for _, leg := range []string{`signalId + "-sl"`, `signalId + "-tp"`} {
		i := strings.Index(body, leg)
		start := strings.LastIndex(body[:i], "CreateOrder(")
		if start < 0 || i < 0 {
			t.Fatalf("could not read the %s CreateOrder call", leg)
		}
		if call := body[start:i]; strings.Contains(call, "b.Qty") {
			t.Errorf("the %s leg is still sized from the cached PendingBracket (b.Qty) rather than "+
				"the fill event", leg)
		}
	}

	// And the caller must pass the event's own numbers.
	if !regexp.MustCompile(`SubmitBracketOnEntryFill\(signalId,\s*e\.Filled`).MatchString(src) {
		t.Error("the fill branch does not pass e.Filled into SubmitBracketOnEntryFill — the " +
			"quantity must come from the event that reported the fill")
	}
}

// TestSnapshotDoesNotDropUnknownOrders — the owner's UNKNOWN ruling, enforced
// where it is actually decided.
//
// Go was made to treat `unknown` as non-terminal (order_state.go). That fix is
// worthless on its own, because the AddOn filters the book BEFORE Go sees it and
// dropped Unknown along with Filled/Cancelled/Rejected/Expired. An Unknown order
// therefore did not arrive as "unknown" — it arrived as ABSENT, and every Go
// conclusion of "gone" is drawn from absence: the reaper cancels and marks
// cancelled, cancel_pending is promoted to cancelled, entryIsResting reports no
// children so a cancel is allowed, leg 4 counts zero working orders, and the D5
// reconciler concludes a position is unprotected and places a SECOND stop.
//
// Filtering there was reasonable when the set was "history". Unknown is not
// history; it is the one state we are least entitled to act on.
func TestSnapshotDoesNotDropUnknownOrders(t *testing.T) {
	src := stripCSharpComments(addonSource(t))
	i := strings.Index(src, `st == "Filled"`)
	if i < 0 {
		t.Fatal("the snapshot's terminal filter was not found — this pin has lost its subject")
	}
	end := strings.Index(src[i:], "continue")
	if end < 0 {
		t.Fatal("could not read the end of the snapshot filter")
	}
	filter := src[i : i+end]
	if strings.Contains(filter, `"Unknown"`) {
		t.Fatalf("the order_snapshot filter still drops Unknown orders:\n    %s\n"+
			"An order whose state the AddOn cannot read must be SHIPPED, not omitted. "+
			"Omitted, it reaches Go as absent, and absence is what every 'this order is gone' "+
			"branch keys on — including the D5 reconciler, which would place a second stop "+
			"beside an invisible live one.", strings.TrimSpace(filter))
	}
	for _, terminal := range []string{`"Filled"`, `"Cancelled"`, `"Rejected"`} {
		if !strings.Contains(filter, terminal) {
			t.Errorf("the snapshot filter no longer drops %s — genuinely terminal orders must "+
				"still be filtered, or the book grows without bound", terminal)
		}
	}
}

// TestRejectedLimitExitDoesNotCancelTheBracket — the same naked-position bug,
// through a door the incident report never opened. Found 2026-09-07 by an
// adversarial reader of the cancel-path census, not by any test.
//
// OnOrderUpdate's state gate admits Filled, Rejected AND PartFilled. The "-lx"
// branch below it — the limit-then-market exit — called CancelBracketsFor with
// NO state check at all. So a limit exit the SIM REJECTS (and this file
// documents that exact rejection: "There is no market data available to drive
// the simulation engine") cancelled the position's stop and target while the
// position was still OPEN.
//
// It is D1's rule again: retiring a bracket is legitimate when the position is
// ENDING, and this branch fires when the attempt to end it FAILED.
func TestRejectedLimitExitDoesNotCancelTheBracket(t *testing.T) {
	src := stripCSharpComments(addonSource(t))
	i := strings.Index(src, `EndsWith("-lx")`)
	if i < 0 {
		t.Fatal(`the "-lx" exit branch was not found — this pin has lost its subject`)
	}
	call := strings.Index(src[i:], "CancelBracketsFor(")
	if call < 0 {
		return // the branch no longer cancels a bracket at all: also fine
	}
	branch := src[i : i+call]
	if !strings.Contains(branch, "OrderState.Filled") {
		t.Fatalf("the \"-lx\" branch cancels the bracket without checking the exit actually FILLED:\n    %s\n"+
			"A REJECTED or PART-FILLED limit exit leaves the position OPEN — and this strips its stop "+
			"and target. That is the 2026-09-06 naked position, reached from the exit side.",
			strings.TrimSpace(branch))
	}
}

// TestCancelBracketsForCancelsEveryMatchedBracket — QUEUE-47 (DS-104). The
// (account,instrument) fallback loop used to keep ONLY the LAST matched bracket
// (`pb = cand` overwrote it) while retiring EVERY matched key, so a second
// bracket on the same instrument was dropped from tracking with its orders still
// live. The fix must collect EVERY match and retire each key only after that
// bracket's own orders were cancelled (a failed cancel leaves the key so a retry
// can still find it). MUTANT: restore `pb = cand` → RED.
func TestCancelBracketsForCancelsEveryMatchedBracket(t *testing.T) {
	body := csharpMethodBody(t, addonSource(t), "private void CancelBracketsFor(")
	if strings.Contains(body, "pb = cand;") {
		t.Error("CancelBracketsFor still overwrites pb in the fallback loop — only the last matched bracket is cancelled")
	}
	if !strings.Contains(body, "victims.Add(kv)") {
		t.Error("CancelBracketsFor no longer collects EVERY (account,root)-matched bracket")
	}
	if !strings.Contains(body, "placedBrackets.Remove(v.Key)") {
		t.Error("CancelBracketsFor must retire each key after its own bracket was cancelled (per-key, not a shared keysToRemove list)")
	}
}
