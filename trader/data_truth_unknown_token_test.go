package trader

import (
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
	ntTrader "vl/trader/ninjatrader"
)

// m55r3-h2 (release #55 round-3 review, no-bypass lens, defect 2): an
// unrecognised reader or scope token used to match NOBODY, so a block that
// Evaluate had already announced ("new entries PAUSED", owner push) was silently
// dropped by dataTruthReasons and every path admitted. An unknown token now
// applies to EVERYONE (fail closed): a scope may only narrow to a RECOGNISED
// reader / trader / account.
//
// Driven through dataTruthReasons - the call every entry door's verdict makes -
// on both entry windows.
//
// MUTANT: Reason.Concerns / Reason.AppliesTo go back to "match only the known
// tokens" (drop the !knownReader / !knownScope arms) -> every row below RED.
func TestDataTruthUnknownReaderOrScopeTokenAppliesToEveryone(t *testing.T) {
	deep, intra := ntTrader.IntegrityDeepOpenMin, ntTrader.IntegrityIntradayOpenMin
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	asker := truthAsker{id: "t1", account: "SIM101"}
	for _, tok := range []string{"Mentor", "mentr", "AI", "ai ", "trader:", "acct:", "trader: ", "foo:bar", "ai:x"} {
		for _, open := range []int{deep, intra} {
			datatruth.Reset()
			name := "unk"
			datatruth.RegisterScoped(name, func(string, time.Time) (bool, string, []string, error) {
				return true, "boom", []string{tok}, nil
			})
			v := dataTruthReasons("MNQ", open, now, asker)
			if !v.Blocked || !strings.Contains(v.Reason, name+":") {
				t.Errorf("token %q open=%d: an announced block with an unrecognised token must apply to everyone, got blocked=%v %q", tok, open, v.Blocked, v.Reason)
			}
		}
	}
	datatruth.Reset()

	// The recognised tokens still NARROW (the fix must not widen what it can read).
	reg := func(name string, tok ...string) {
		datatruth.RegisterScoped(name, func(string, time.Time) (bool, string, []string, error) {
			return true, "boom", tok, nil
		})
	}
	reg("other_trader", datatruth.ScopeTrader("t2"))
	reg("other_acct", datatruth.ScopeAccount("SIM999"))
	reg("mentor_only", datatruth.ReaderMentor)
	reg("mixed", datatruth.ReaderMentor, datatruth.ScopeTrader("t2"), "bogus:")
	v := dataTruthReasons("MNQ", deep, now, asker)
	if strings.Contains(v.Reason, "other_trader:") || strings.Contains(v.Reason, "other_acct:") || strings.Contains(v.Reason, "mentor_only:") || strings.Contains(v.Reason, "mixed:") {
		t.Errorf("recognised scopes must still narrow (a mentor-reader block does not reach the AI path): %q", v.Reason)
	}
	v = dataTruthReasons("MNQ", intra, now, asker)
	if !strings.Contains(v.Reason, "mixed:") {
		t.Errorf("a scope list holding one unrecognised token applies to every trader on the reader's path, even beside a recognised other-trader scope: %q", v.Reason)
	}
	if strings.Contains(v.Reason, "other_trader:") || strings.Contains(v.Reason, "other_acct:") {
		t.Errorf("recognised scopes must still narrow on the mentor path: %q", v.Reason)
	}
	datatruth.Reset()
}
