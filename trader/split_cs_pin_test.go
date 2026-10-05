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
		"Leg1Qty = b.Leg1Qty, Leg2Qty = leg2Qty, Leg1Tp = b.Leg1Tp, RunnerTp = b.Tp,",
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
// TestSplitAddonFlatOnlyAccountRemoval pins positionAccountBySymbol: it is
// removed only in OnPositionUpdate (the position has settled) and only when
// the account that went flat is the recorded owner. REVIEW-SPLIT-2 P2-2 moved
// this out of the exit-fill handler, where e.Order.Account.Positions can still
// show the pre-fill quantity and wrongly keep the mapping. Mutant: remove on
// any exit fill → RED.
func TestSplitAddonFlatOnlyAccountRemoval(t *testing.T) {
	src := readSplitAddonSource(t)
	for _, want := range []string{
		"// REVIEW-SPLIT-2 P2-2: drop the account-ownership mapping HERE,",
		"positionAccountBySymbol.TryGetValue(flatRoot, out owner)",
		"string.Equals(owner.Name, acc.Name, StringComparison.OrdinalIgnoreCase)",
		"positionAccountBySymbol.Remove(flatRoot);",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("AddOn lost the settled flat-only account-removal: missing %q", want)
		}
	}
}
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

// TestSplitAddonLeg2ExitReportsSameSignalID pins the exit-fill name parse: leg 2's
// -sl2/-tp2 orders strip to the SAME entry signal id (its residual qty drives
// ApplyNT8Exit's partial path). Mutant: no -sl2/-tp2 parse → RED.
func TestSplitAddonLeg2ExitReportsSameSignalID(t *testing.T) {
	src := readSplitAddonSource(t)
	for _, want := range []string{
		`if (signalId.EndsWith("-sl2")) { exitReason = "sl"; signalId = signalId.Substring(0, signalId.Length - 4); }`,
		`if (signalId.EndsWith("-tp2")) { exitReason = "tp"; signalId = signalId.Substring(0, signalId.Length - 4); }`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("AddOn lost the leg-2 exit name parse: missing %q", want)
		}
	}
}

// TestSplitAddonNettingFlatSweepCancelsLeg2 pins CancelAllBracketsFor (the
// netting-flat sweep): leg 2's stop/TP are cancelled too. Mutant: leg 2 omitted
// from the sweep → RED.
func TestSplitAddonNettingFlatSweepCancelsLeg2(t *testing.T) {
	src := readSplitAddonSource(t)
	n := strings.Count(src, "// P0-2: the netting-flat sweep must cancel leg 2 too.")
	if n != 1 {
		t.Fatalf("the netting-flat sweep must carry exactly one P0-2 leg-2 marker, got %d", n)
	}
	for _, want := range []string{
		"if (pb.SlOrder2 != null) toCancel.Add(pb.SlOrder2);",
		"if (pb.TpOrder2 != null) toCancel.Add(pb.TpOrder2);",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the netting-flat sweep lost a leg-2 cancel: missing %q", want)
		}
	}
}

// TestIsBracketChildKnowsLeg2 pins the Go side of P0-2: -sl2/-tp2 are protective
// children (never "working entries"), so the one-contract guard does not count
// an orphaned leg-2 stop as an entry. Mutant: drop -sl2/-tp2 → RED.
func TestIsBracketChildKnowsLeg2(t *testing.T) {
	for _, name := range []string{"x-sl2", "X-TP2", "x-sl", "x-tp", "x-lx"} {
		if !isBracketChild(name) {
			t.Errorf("isBracketChild(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"x-entry", "x", "stoplimit-x"} {
		if isBracketChild(name) {
			t.Errorf("isBracketChild(%q) = true, want false", name)
		}
	}
}

// TestSplitAddonPositionCloseLegField pins the P1-2 wire field: the
// position_close frame carries `leg` so the Go receipt identity can tell two
// same-ms stop exits (leg 2 then leg 1) apart. Mutant: drop ["leg"] → RED.
func TestSplitAddonPositionCloseLegField(t *testing.T) {
	src := readSplitAddonSource(t)
	for _, want := range []string{
		`string acctName = "", int leg = 0)`,
		`["leg"]           = leg,`,
		`if (orderName.EndsWith("-sl2") || orderName.EndsWith("-tp2")) closeLeg = 2;`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("AddOn lost the position_close leg field: missing %q", want)
		}
	}
}

// TestSplitAddonOrderUpdateStripsLeg2 pins SendOrderUpdateFrame: it strips
// -sl2/-tp2 BEFORE -sl/-tp so an order_update for a leg-2 bracket reports the
// base signal id the Go side tracks. Mutant: -sl2 not stripped → RED.
func TestSplitAddonOrderUpdateStripsLeg2(t *testing.T) {
	src := readSplitAddonSource(t)
	if !strings.Contains(src, "// REVIEW-SPLIT-2 P2-3: strip the leg-2 suffixes first") {
		t.Error("AddOn lost the order_update leg-2 strip marker")
	}
	for _, want := range []string{
		`if (signalId.EndsWith("-sl2") || signalId.EndsWith("-tp2"))`,
		`signalId = signalId.Substring(0, signalId.Length - 4);`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("AddOn lost the order_update leg-2 strip: missing %q", want)
		}
	}
}
