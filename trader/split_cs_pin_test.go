package trader

import (
	"os"
	"strings"
	"testing"
)

// ── REVIEW-353 C# code-level pins ─────────────────────────────────────────
// The AddOn (VLTraderTCPClient.cs) is the other half of the wire contract; its
// two-OCO-pair placement is pinned here by source so a drift in the .cs fails
// this suite even though NT8's compile only happens on the owner's F5.

const splitAddonSourcePath = "../ninjascript/VLTraderTCPClient.cs"

func readSplitAddonSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(splitAddonSourcePath)
	if err != nil {
		t.Fatalf("cannot read the AddOn source: %v", err)
	}
	return string(b)
}

// TestSplitAddonPlacesTwoOCOPairs pins SubmitBracketOnEntryFill: when
// leg1_qty > 0 the AddOn submits FOUR orders (leg 1 SL/TP + leg 2 SL/TP) under
// the one entry signal id. Mutant: one OCO pair only (drop sl2/tp2) → RED.
func TestSplitAddonPlacesTwoOCOPairs(t *testing.T) {
	src := readSplitAddonSource(t)
	for _, want := range []string{
		"var sl1 = ba.CreateOrder(",
		"var tp1 = ba.CreateOrder(",
		"var sl2 = ba.CreateOrder(",
		"var tp2 = ba.CreateOrder(",
		"ba.Submit(new[] { sl1, tp1, sl2, tp2 });",
		"Leg1Qty = leg1Qty, Leg2Qty = leg2Qty, Leg1Tp = b.Leg1Tp, RunnerTp = b.Tp,",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("AddOn lost the two-OCO-pair placement: missing %q", want)
		}
	}
}

// TestSplitAddonPartialFillLeg1First pins AmendBracketQuantity: a later fill
// allocates leg 1 first up to its qty, then leg 2. Mutant: leg 2 first → RED.
func TestSplitAddonPartialFillLeg1First(t *testing.T) {
	src := readSplitAddonSource(t)
	for _, want := range []string{
		"int leg1Qty = Math.Min(pb.Leg1Qty, filledQty);",
		"int leg2Qty = Math.Max(0, filledQty - leg1Qty);",
		"// Leg 2's pair was never placed (partial first fill): create it now.",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("AddOn lost the leg-1-first partial-fill allocation: missing %q", want)
		}
	}
}

// TestSplitAddonFlatOnlyAccountRemoval pins positionAccountBySymbol: it is
// removed only when the account's instrument position is FLAT. Mutant: remove on
// any exit fill → RED.
func TestSplitAddonFlatOnlyAccountRemoval(t *testing.T) {
	src := readSplitAddonSource(t)
	if !strings.Contains(src, "pos.Quantity != 0 && pos.MarketPosition != MarketPosition.Flat") {
		t.Error("AddOn lost the flat-only guard — positionAccountBySymbol could be removed on a partial exit")
	}
	if !strings.Contains(src, "// PHASE 4: drop the account ownership ONLY when that account's") {
		t.Error("AddOn lost the REVIEW-353 flat-only account-removal comment")
	}
}

// TestSplitAddonCancelAllBrackets pins CancelBracketsFor: EVERY bracket of the
// signal is cancelled (leg 1 + leg 2). Mutant: only leg 1 → RED.
func TestSplitAddonCancelAllBrackets(t *testing.T) {
	src := readSplitAddonSource(t)
	for _, want := range []string{
		"if (pb.SlOrder2 != null) toCancel.Add(pb.SlOrder2);",
		"if (pb.TpOrder2 != null) toCancel.Add(pb.TpOrder2);",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("AddOn lost the all-brackets cancel: missing %q", want)
		}
	}
}

// TestSplitAddonLegFieldOnMoveModify pins the `leg` field: move_stop and
// modify_bracket honour 1/2/absent=all. Mutant: drop the leg parse → RED.
func TestSplitAddonLegFieldOnMoveModify(t *testing.T) {
	src := readSplitAddonSource(t)
	for _, want := range []string{
		"int    leg      = GetInt(p, \"leg\");   // 1 = leg 1, 2 = leg 2, 0/absent = ALL",
		"if (leg != 2 && pb.SlOrder != null) stops.Add(pb.SlOrder);",
		"if (leg != 1 && pb.SlOrder2 != null) stops.Add(pb.SlOrder2);",
		"if (leg != 2)",
		"if (leg != 1)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("AddOn lost the leg field on move_stop/modify_bracket: missing %q", want)
		}
	}
}
