package mentor

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// m463 — release #59, swing-4h exactly as D5.2 (owner order 2026-11-01 "full
// of it"). REAL replay of the live swing days of trades 707, 709/710, 713/714
// and 715 (10-28 17:00 CT → 10-29 07:00 CT): the recorded MNQ 12-26 1m tape
// (testdata/m463_mnq_1m_20261028_0800ct.csv, extracted read-only from
// data/data.db bars tf=1m — sqlite3 -readonly, no copy of the DB), driven
// minute by minute through runSwing (the production call site the Evaluator's
// Tick uses: eval.go runSwing → SwingTick → the named-refusal drain).
//
// The swing line is seeded WARM at the 17:00 CT bucket with the value the live
// arms 469-478 carried (armed_orders.mentor_snapshot_json swing.line =
// 29645.5290201388); every later bucket's line is FOLDED by the production
// recurrence and must reproduce the live values (21:00 = 29642.0416475594,
// rows 481/482; 01:00 = 29644.4821248417, row 487; 05:00 = 29647.5117177079,
// row 488) — a parity check that the replay reads the same line the bot read.

const (
	m463Line1700 = 29645.5290201388 // 10-28 17:00 CT bucket (arms 469-478)
	m463Line2100 = 29642.0416475594 // 10-28 21:00 CT bucket (arms 481/482)
	m463Line0100 = 29644.4821248417 // 10-29 01:00 CT bucket (arm 487)
	m463Line0500 = 29647.5117177079 // 10-29 05:00 CT bucket (arm 488)

	m463Bucket1700 = int64(1793224800000) // 2026-10-28 17:00 CT
)

// m466 P4 (release #61): every m463 replay pin below runs m61PreP4 — the
// pre-#61 swing ISB knobs (mentor_swing_isb_close_back=required, keep OFF),
// the strict R24 read these real-trade assertions were written against. The
// #61 defaults on this same tape are pinned in m61_p4_swing_test.go
// (TestM61SwingReplay1028Default).

type m463Event struct {
	At     int64 // ms, the tick's now
	Intent Intent
}

// m463ReplayResult is everything the real replay produced.
type m463ReplayResult struct {
	Entries  []m463Event
	Cancels  []m463Event
	Ends     []string // rv59: the emulated broker's end receipts ("<CT> <arm> stop|target|hold @price")
	Lines    map[int64]float64 // bucket start → line in force
	Eval     *Evaluator
	NoteDump []string
}

func m463Replay(t *testing.T, cfgMut func(*Config)) m463ReplayResult {
	t.Helper()
	bars := m413Load(t, "testdata/m463_mnq_1m_20261028_0800ct.csv")
	cfg := DefaultConfig()
	if cfgMut != nil {
		cfgMut(&cfg)
	}
	e := New(cfg)
	// Seed WARM at the 17:00 CT bucket (the live state the arms were cut from).
	// LastBarTime = the last 5m bar of the 13:00 bucket (15:55 CT), so the walk
	// starts at the first 5m bar of the 17:00 bucket like the live tick did.
	e.State.Swing = SwingState{
		Line:        m463Line1700,
		BucketStart: m463Bucket1700,
		EmaCount:    FourHEMA34Min,
		LastBarTime: m463Bucket1700 - 65*60_000, // 15:55 CT
	}
	res := m463ReplayResult{Lines: map[int64]float64{m463Bucket1700: m463Line1700}, Eval: e}
	// rv59: the replay emulates the trader's receipts for a FILLED swing — the
	// BE move applied, and the position ENDED at its stop (the BE stop once
	// moved), its target, or the hold-close — exactly the SwingBEApplied /
	// SwingEnded calls the live trader makes. Before rv59 nothing ever ended a
	// position here, so one filled swing blocked every later setup of the day
	// (swing_position_open) — a harness artefact the live bot never has.
	beArm := ""
	for i := range bars {
		if bars[i].OpenTime < m463Bucket1700 {
			continue
		}
		now := bars[i].CloseTime + 1
		if p := e.State.Swing.Pos; p != nil {
			b := bars[i]
			stop := p.Stop
			if beArm == p.ArmID {
				stop = p.Entry
			}
			why, px := "", 0.0
			switch {
			case p.Side == SideLong && b.Low <= stop, p.Side == SideShort && b.High >= stop:
				why, px = "stop", stop
			case p.Target > 0 && (p.Side == SideLong && b.High >= p.Target || p.Side == SideShort && b.Low <= p.Target):
				why, px = "target", p.Target
			}
			if why != "" {
				res.Ends = append(res.Ends, fmt.Sprintf("%s %s %s @%.2f", m463CT(b.OpenTime), p.ArmID, why, px))
				e.SwingEnded(p.ArmID)
			}
		}
		for _, in := range runSwing(e, bars[:i+1], now) {
			switch in.Action {
			case PlaceStopEntry:
				res.Entries = append(res.Entries, m463Event{At: now, Intent: in})
			case CancelArm:
				res.Cancels = append(res.Cancels, m463Event{At: now, Intent: in})
			case ActionMoveStopBE:
				if e.SwingBEApplied(in.ArmID) {
					beArm = in.ArmID
				}
			case ActionClosePosition:
				if e.SwingEnded(in.ArmID) {
					res.Ends = append(res.Ends, fmt.Sprintf("%s %s hold @%.2f", m463CT(bars[i].OpenTime), in.ArmID, bars[i].Close))
				}
			}
		}
		res.Lines[e.State.Swing.BucketStart] = e.State.Swing.Line
	}
	res.NoteDump = e.TakeNotes()
	return res
}

func m463CT(ms int64) string {
	return time.UnixMilli(ms).In(ctime()).Format("01-02 15:04")
}

func (r m463ReplayResult) dump() string {
	var sb strings.Builder
	for _, ev := range r.Entries {
		in := ev.Intent
		fmt.Fprintf(&sb, "  ENTRY %s %s %s entry %.2f stop %.2f (%.2f pts) target %.2f [%s]\n",
			m463CT(ev.At), in.ArmID, in.Side, in.Price, in.Stop, abs(in.Stop-in.Price), in.Target, in.Reason)
	}
	for _, ev := range r.Cancels {
		fmt.Fprintf(&sb, "  CANCEL %s %s [%s]\n", m463CT(ev.At), ev.Intent.ArmID, ev.Intent.Reason)
	}
	for _, end := range r.Ends {
		fmt.Fprintf(&sb, "  END %s\n", end)
	}
	fmt.Fprintf(&sb, "  refusals %v\n", r.Eval.State.Refusals)
	for _, n := range r.NoteDump {
		if strings.Contains(n, "swing") {
			fmt.Fprintf(&sb, "  NOTE %s\n", n)
		}
	}
	return sb.String()
}

func (r m463ReplayResult) entryAt(armMs int64) (Intent, bool) {
	id := fmt.Sprintf("swing-%d", armMs)
	for _, ev := range r.Entries {
		if ev.Intent.ArmID == id {
			return ev.Intent, true
		}
	}
	return Intent{}, false
}

func (r m463ReplayResult) noteHas(subs ...string) bool {
	for _, n := range r.NoteDump {
		ok := true
		for _, sub := range subs {
			if !strings.Contains(n, sub) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// TestM463ReplayLineParity — the replay folds the same 4h EMA34 the live bot
// used for every bucket of the window (rows 481/482, 487, 488).
func TestM463ReplayLineParity(t *testing.T) {
	r := m463Replay(t, m61PreP4)
	for _, c := range []struct {
		bucket int64
		want   float64
		rows   string
	}{
		{m463Bucket1700, m463Line1700, "469-478"},
		{m463Bucket1700 + 4*3600_000, m463Line2100, "481/482"},
		{m463Bucket1700 + 8*3600_000, m463Line0100, "487"},
		{m463Bucket1700 + 12*3600_000, m463Line0500, "488"},
	} {
		if got := r.Lines[c.bucket]; abs(got-c.want) > 0.001 {
			t.Fatalf("bucket %s line %.6f, live rows %s carried %.6f", m463CT(c.bucket), got, c.rows, c.want)
		}
	}
}

// TestM463ReplayTrades707_709_710Refused — the three swing losses of 10-28
// (-$200, -$409, -$409) are impossible on the real tape, each with a NAMED
// reason in the evaluator's refusal ledger and a note carrying the rule ids:
//
//   - 707 (arm 475, 17:40 CT short 29625.75): the 17:35 candle closed THROUGH
//     the line (29633 < 29645.53) → the level is invalid for the 17:00 4h
//     candle (D5.2-R22/R23) — and the 17:40 high 29644.25 never reached the
//     line (1.28 pts short, D5.2-R12). Named swing_level_invalid.
//   - 709/710 (arms 477/478, 19:05 CT long 29717.75, stop 102.25): the same
//     invalid level, 90 minutes later — only an inside bar may trade it
//     (D5.2-R22/R24); the 19:05 low 29646 is 0.47 above the line (R12); the
//     stop would be 102.22 > 90 (R31; 102.25 on the tick-rounded wire). Never
//     armed: since rv59 the 19:00 inside bar is the level's trade and 19:05 is
//     its fill bar (below).
func TestM463ReplayTrades707_709_710Refused(t *testing.T) {
	r := m463Replay(t, m61PreP4)
	if in, ok := r.entryAt(1793227200000); ok {
		t.Fatalf("trade 707 (arm 475, 17:40 short) armed: %+v\n%s", in, r.dump())
	}
	if in, ok := r.entryAt(1793232300000); ok {
		t.Fatalf("trades 709/710 (arms 477/478, 19:05 long) armed: %+v\n%s", in, r.dump())
	}
	if r.Eval.State.Refusals[SwingRefusedLevelInvalid] == 0 {
		t.Fatalf("the refusals are not NAMED in the ledger: %v\n%s", r.Eval.State.Refusals, r.dump())
	}
	if !r.noteHas("17:40", SwingRefusedLevelInvalid, "D5.2-R22", "1.28") {
		t.Fatalf("707: no named note for the 17:40 candle (level invalid + 1.28-pt near touch)\n%s", r.dump())
	}
	// rv59 (release #59 review, course-swing P2): the 18:25 inside-bar order
	// (stop 85.22) was CANCELLED unfilled at 18:40 by a close back through the
	// line — that does not spend the level (D5.2-R25, no time limit). The 19:00
	// bar (body 29,668.75–29,674.25) is an inside bar of the 18:55 close-back
	// candle (29,658.75–29,694.50) and closes above the line: the course's way
	// back in arms — LONG 29,699.25, stop AT the line 29,645.53 (53.72 pts,
	// D5.2-R24/R29). The 19:05 candle (709/710's reject shape, 0.47 short of the
	// line, would-be stop 102.22) is the FILL bar of that inside bar, not a new
	// setup — 709/710's own 19:05 arm stays impossible. Before rv59 the 19:05
	// candle was refused swing_level_invalid with the 0.47 / 102.22 note, and the
	// 19:00 inside bar got NO note at all (ISBFired still latched).
	isb, ok := r.entryAt(1793232000000)
	if !ok || isb.Side != SideLong || isb.Price != 29699.25 || abs(isb.Stop-m463Line1700) > 0.01 {
		t.Fatalf("the 19:00 inside bar after the cancelled 18:25 inside bar must arm LONG 29,699.25 stop AT the line %.2f, got %+v ok=%v\n%s", m463Line1700, isb, ok, r.dump())
	}
	// 713/714 (arms 481/482, 23:55 short off the close-back bar) stay refused.
	if in, ok := r.entryAt(1793249700000); ok {
		t.Fatalf("trades 713/714 (23:55 short ISB on the close-back bar) armed: %+v\n%s", in, r.dump())
	}
}

// TestM463ReplayISBNinetyPlusRefused — the replay's 18:10 CT inside bar (the
// way-back-in long after the 17:35 through close) has its stop AT the line
// 90.22 pts away: above the 90-pt ceiling → "đừng vô" (D5.2-R31), named
// swing_max_stop. HEAD before m463 armed it.
func TestM463ReplayISBNinetyPlusRefused(t *testing.T) {
	r := m463Replay(t, m61PreP4)
	if in, ok := r.entryAt(1793229000000); ok {
		t.Fatalf("the 18:10 inside bar with a 90.22-pt stop armed: %+v\n%s", in, r.dump())
	}
	if r.Eval.State.Refusals[SwingRefusedMaxStop] == 0 || !r.noteHas("18:10", SwingRefusedMaxStop, "90.22") {
		t.Fatalf("the 18:10 refusal must be named swing_max_stop with the stop size\n%s", r.dump())
	}
}

// TestM463ReplayTrade715StillTrades — trade 715 (arm 487, 10-29 01:55 CT long
// 29654.25, stop AT the line 29644.48, -$40) is a textbook D5.2-R24/R29 entry:
// 01:30 first touch closed back above (reject armed), 01:35 closed THROUGH
// (cancel, level invalid), 01:45 closed back above, 01:50 is its inside bar.
// The 5m/1h/4h triggers were all short, but the swing follows the 4H EMA34,
// not the 5m trend (D5.2-R7/R35 — "4 giờ đụng resistance thì mình đánh xuống"
// mirrored: price above the 4H EMA = support, long). m463 keeps it.
func TestM463ReplayTrade715StillTrades(t *testing.T) {
	r := m463Replay(t, m61PreP4)
	in, ok := r.entryAt(1793256600000)
	if !ok {
		t.Fatalf("trade 715's inside bar (arm 487) must still arm\n%s", r.dump())
	}
	if in.Side != SideLong || in.Price != 29654.25 || abs(in.Stop-m463Line0100) > 0.01 || !strings.Contains(in.Reason, "inside bar") {
		t.Fatalf("715: want LONG ISB 29654.25 stop AT the line %.2f, got %+v", m463Line0100, in)
	}
}

// TestM463ReplayDump prints the whole replay (go test -run TestM463ReplayDump -v).
func TestM463ReplayDump(t *testing.T) {
	r := m463Replay(t, m61PreP4)
	t.Logf("lines: 17:00 %.6f | 21:00 %.6f | 01:00 %.6f | 05:00 %.6f",
		r.Lines[m463Bucket1700], r.Lines[m463Bucket1700+4*3600_000], r.Lines[m463Bucket1700+8*3600_000], r.Lines[m463Bucket1700+12*3600_000])
	t.Logf("\n%s", r.dump())
}

// TestM463DumpM434Swing (diagnostic, M463_DUMP=1): prints every SWING4H intent
// and swing note the real Evaluator emits on the m434 golden tapes, so a
// deliberate swing change can be read tick by tick before the m434 golden is
// regenerated (release #59: the 10-23 tape moved on 5 ticks).
func TestM463DumpM434Swing(t *testing.T) {
	if os.Getenv("M463_DUMP") == "" {
		t.Skip("diagnostic: set M463_DUMP=1")
	}
	cases := m434Cases()[:1]
	if os.Getenv("M463_DUMP") == "all" {
		cases = m434Cases()
	}
	for _, c := range cases {
		old := flatNearScale
		flatNearScale = c.scale
		bars := c.tape(t)
		e := New(c.cfg())
		for i := c.from; i < len(bars); i++ {
			lo := i + 1 - 1500
			if lo < 0 {
				lo = 0
			}
			out := e.Tick(bars[lo:i+1], BarCloseInstant(bars[i]))
			for _, in := range out {
				switch in.Action {
				case ActionMoveStopBE:
					e.SwingBEApplied(in.ArmID)
				case ActionClosePosition:
					e.SwingEnded(in.ArmID)
				}
				if in.Setup == "SWING4H" || strings.HasPrefix(in.ArmID, "swing-") {
					t.Logf("%s bar %d %s %s %s %s entry %.2f stop %.2f tgt %.2f — %s", c.name, i, m463CT(bars[i].OpenTime), in.Action, in.ArmID, in.Side, in.Price, in.Stop, in.Target, in.Reason)
				}
			}
			for _, n := range e.TakeNotes() {
				if strings.Contains(n, "swing") {
					t.Logf("%s bar %d NOTE %s", c.name, i, n)
				}
			}
		}
		flatNearScale = old
	}
}
