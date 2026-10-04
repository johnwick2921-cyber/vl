package mentor

import (
	"strings"
	"testing"
)

// §5.4 [D4.4 p1 @ 16:00] — the three cases, verbatim from the mentor's screen:
//   1. 4H trigger Buy, 1h trigger buy  → trade.
//   2. 4h trigger buy, 1h ko có gì hết → follow the 4h.
//   3. 4h trigger buy, 1h trigger sell → sit out until the 1h flips.

// TestHTFVerdictCase1Agree — case 1: both triggers the same direction → trade
// that side.
func TestHTFVerdictCase1Agree(t *testing.T) {
	h := HTF{FourH: TriggerLine{Dir: SideLong, Price: 30000}, OneH: TriggerLine{Dir: SideLong, Price: 29950}}
	if ok, side, _ := HTFVerdict(h); !ok || side != SideLong {
		t.Fatalf("4h long + 1h long: ok=%v side=%q, want trade long", ok, side)
	}
	if HTFConflict(h) {
		t.Fatal("same direction must not be a conflict")
	}
}

// TestHTFVerdictCase2Follow4h — case 2: 1h "ko có gì hết" (no line yet) →
// follow the 4h.
func TestHTFVerdictCase2Follow4h(t *testing.T) {
	h := HTF{FourH: TriggerLine{Dir: SideShort, Price: 29000}}
	if ok, side, _ := HTFVerdict(h); !ok || side != SideShort {
		t.Fatalf("4h short + 1h silent: ok=%v side=%q, want trade short", ok, side)
	}
}

// TestHTFVerdictCase3SitOut — case 3: 1h opposite the 4h → sit out until the
// 1h flips. "Ngồi chờ khi nào 1h trigger buy theo khung 4h thì trade."
func TestHTFVerdictCase3SitOut(t *testing.T) {
	h := HTF{FourH: TriggerLine{Dir: SideLong, Price: 30000}, OneH: TriggerLine{Dir: SideShort, Price: 29950}}
	if ok, _, reason := HTFVerdict(h); ok || reason == "" {
		t.Fatalf("4h long + 1h short: ok=%v reason=%q, want refused with a reason", ok, reason)
	}
	if !HTFConflict(h) {
		t.Fatal("opposite directions must conflict")
	}
	// the flip: 1h joins the 4h → the gate opens again
	h.OneH.Dir = SideLong
	if ok, side, _ := HTFVerdict(h); !ok || side != SideLong {
		t.Fatalf("after the 1h flip: ok=%v side=%q, want trade long", ok, side)
	}
}

// TestHTFVerdictNo4hFailClosed — with no 4h trigger there is nothing to
// follow: the read always starts from the 4-hour [D4.4 p1 @ 02:53]. The
// DISTINCT reason is asserted (audit row 5): dropping the no-4h branch
// would land in case 3 with a different reason.
func TestHTFVerdictNo4hFailClosed(t *testing.T) {
	h := HTF{OneH: TriggerLine{Dir: SideLong, Price: 29950}}
	if ok, _, reason := HTFVerdict(h); ok || reason == "" {
		t.Fatalf("1h-only trigger: ok=%v, want refused (no 4h direction)", ok)
	}
	if _, _, reason := HTFVerdict(h); !strings.Contains(reason, "4-hour is read first") {
		t.Fatalf("no-4h reason = %q, want the distinct '4-hour is read first' refusal", reason)
	}
}

// TestHTFTriggerOnRecordedTapes — canon 53: replay the recorded 4h and 1h
// tapes (db-copy, MNQ 12-26) in lockstep. Invariant: once the 4h line is
// drawn, the verdict never names a side other than the 4h side — a trade is
// either on the 4h direction or refused by case 3.
func TestHTFTriggerOnRecordedTapes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	bars4h := loadFixture(t, "mnq_4h_2026-09-14to10-02", "4h")
	bars1h := loadFixture(t, "mnq_1h_2026-09-28to10-02", "1h")

	h := HTF{}
	j := 0
	verdicts := 0
	for i := 1; i < len(bars4h); i++ {
		h.FourH = TriggerTick(h.FourH, bars4h[i-1:i+1], 240, cfg)
		for j+1 < len(bars1h) && bars1h[j+1].OpenTime < bars4h[i].OpenTime {
			j++
			h.OneH = TriggerTick(h.OneH, bars1h[j-1:j+1], 60, cfg)
		}
		if h.FourH.Dir == "" {
			continue
		}
		ok, side, _ := HTFVerdict(h)
		verdicts++
		if ok && side != h.FourH.Dir {
			t.Fatalf("bar %d: verdict side %q against the 4h %q", i, side, h.FourH.Dir)
		}
	}
	if h.FourH.Dir == "" {
		t.Fatal("the recorded 4h tape drew no trigger line")
	}
	if verdicts == 0 {
		t.Fatal("no verdicts produced over the tapes")
	}
	t.Logf("recorded tapes: 4h %q @ %.2f, 1h %q @ %.2f, verdicts=%d", h.FourH.Dir, h.FourH.Price, h.OneH.Dir, h.OneH.Price, verdicts)
}

// TestHTFSideIsOKPriceIsFree — the trigger having FIRED sets the direction;
// price need NOT sit on the trigger side of the line [D4.4 p2 @ 03:51].
func TestHTFSideIsOKPriceIsFree(t *testing.T) {
	// 4h triggered buy at 30000 but price trades 29500 (below the line) —
	// the direction gate still permits longs and refuses shorts.
	h := HTF{FourH: TriggerLine{Dir: SideLong, Price: 30000}}
	if !HTFSideIsOK(h, SideLong) {
		t.Fatal("long must pass even below the buy line — the fired trigger sets the direction [D4.4 p2 @ 03:51]")
	}
	if HTFSideIsOK(h, SideShort) {
		t.Fatal("short against the 4h direction must be refused")
	}
}
