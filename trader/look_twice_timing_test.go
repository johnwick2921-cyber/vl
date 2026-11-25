package trader

import (
	"context"
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── LOOK TWICE, the VALUES of its timing constants ───────────────────────────
//
// Every number here is absolute on purpose. A test that computes its boundary from
// the constant it tests (protectionProofMaxAge + 100 ms) follows a changed constant:
// 30 s and 300 s of link-check age were both green. Pinned elsewhere by absolute
// numbers: the 3 s confirm and its 1 s / 30 s limits (TestLookTwiceConfirmSetting),
// the 70 s gap and the 35 s P0 (TestLookTwiceGap_LateCoverUsesOnlyItsOwnHold), the
// 60 s own death (TestLookTwiceGap_OwnDeath), the detector's 3 s
// (TestOwnReverseSetting), the 500 ms poll (TestOrderFillPollProductionInterval).

// freezeAt stops the wire's receive clock at one instant.
func (w *lookWire) freezeAt(at time.Time) {
	w.s.SetReceiptClockForTest(func() time.Time { return at })
}

// protectionProofMaxAge is 3 s: a link check 2.8 s old at the send is accepted, one
// 3.2 s old is refused. MUTANTS: 2 s, 4 s, 30 s, 300 s.
func TestLookTwiceTiming_ProofMaxAge(t *testing.T) {
	for _, act := range []protectionAction{protectionPlace, protectionFlatten} {
		for age, want := range map[time.Duration]int{2800 * time.Millisecond: 1, 3200 * time.Millisecond: 0} {
			t.Run(act.String()+"/"+age.String(), func(t *testing.T) {
				w, sid := nakedShort(t, "lt-tm-proof-"+act.String()+age.String(), 27740)
				w.cancelStop(sid)
				v := protectionVerdict{Action: act, StopPx: stop1193, Source: "t", Why: "naked."}
				p := w.proofFor(v)
				w.freezeAt(p.passStart.Add(age))
				before := len(w.ordersOut())
				w.protState(func() {
					w.at.actOnConfirmed(v, "MNQ", "SHORT", 1, sid, "t", 0, w.now(), p.check)
					w.at.finishProtectionActLocked("MNQ|SHORT", v, p)
				})
				out := w.ordersOut()
				for deadline := time.Now().Add(2 * time.Second); len(out)-before < want && time.Now().Before(deadline); out = w.ordersOut() {
					time.Sleep(5 * time.Millisecond)
				}
				if want == 0 {
					time.Sleep(150 * time.Millisecond)
					out = w.ordersOut()
				}
				if n := len(out) - before; n != want || strings.Contains(p.withheld, "link check") != (want == 0) {
					t.Fatalf("a link check %s old at the send: %d order(s), withheld %q — want %d", age, n, p.withheld, want)
				}
			})
		}
	}
}

// The same through a whole production pass: the ledger read of the confirming pass
// takes 2.8 s or 3.2 s. MUTANTS: the same; passStart taken after the reads.
func TestLookTwiceTiming_ProofMaxAgeThroughAPass(t *testing.T) {
	t.Setenv(lookTwiceEnv, "30000") // the re-look's own timer stays out of the way
	for stall, want := range map[time.Duration]int{2800 * time.Millisecond: 1, 3200 * time.Millisecond: 0} {
		t.Run(stall.String(), func(t *testing.T) {
			w, sid := nakedShort(t, "lt-tm-pass-"+stall.String(), 27740)
			w.cancelStop(sid)
			t0 := w.now()
			w.freezeAt(t0)
			w.at.reconcileProtectionAt(t0, "monitor")
			w.jump(31 * time.Second)
			t1 := w.now()
			w.freezeAt(t1)
			w.beat30()
			w.sync()
			stalled := make(chan struct{})
			w.onceOnTable("query", "UPPER(side)", func() { w.freezeAt(t1.Add(stall)); close(stalled) })
			w.at.reconcileProtectionAt(t1, "monitor") // settled: sends the link check; its ack starts the confirming pass
			select {
			case <-stalled:
			case <-time.After(5 * time.Second):
				t.Fatal("fixture: the ledger was not read")
			}
			w.waitOrders(want, 3*time.Second)
			time.Sleep(200 * time.Millisecond)
			if out := w.ordersOut(); len(out) != want {
				t.Fatalf("the pass stalled %s: %d order(s), want %d", stall, len(out), want)
			}
			if want == 0 {
				w.protState(func() {
					if h := w.at.protHolds["MNQ|SHORT"]; h == nil || !strings.Contains(h.lastWhy, "withheld: the link check") {
						t.Errorf("want the send withheld for the age of the link check, got %+v", h)
					}
				})
			}
		})
	}
}

// HeartbeatAckWait is 5 s: an ack read 4.9 s after the check counts, one read 5.1 s
// after it does not and the check is asked again. MUTANTS: 3 s, 8 s.
func TestLookTwiceTiming_LinkAckWait(t *testing.T) {
	w := gapWire(t, "tm-ack")
	p, err := w.s.SendLinkProbe()
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); !p.Acked(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("fixture: the link check was not answered")
		}
	}
	sent := w.now()
	for age, want := range map[time.Duration]bool{4900 * time.Millisecond: true, 5100 * time.Millisecond: false} {
		h := &protectionHold{since: sent, firstLook: sent, lastLook: sent, probe: p, probeAt: sent}
		w.freezeAt(sent.Add(age))
		w.protState(func() {
			w.at.protHolds = map[string]*protectionHold{"MNQ|SHORT": h}
			w.at.beginProtectionLookLocked(w.s, 3*time.Second)
			w.at.protHolds = nil
		})
		if h.proven != want || h.probe != nil {
			t.Errorf("an ack read %s after the check: proven %v (probe kept %v), want %v", age, h.proven, h.probe != nil, want)
		}
	}
}

// With no ack at all the re-look asks again about 5 s after the last check, not sooner
// and not seconds later. MUTANTS: HeartbeatAckWait 3 s or 8 s; the loop's extra wait 3 s.
// (An extra wait of 0 is equivalent: the wait starts after probeAt, so 5 s of it is
// already more than 5 s after probeAt.)
func TestLookTwiceTiming_NoAckAsksAgainAfter5s(t *testing.T) {
	fastConfirm(t)
	w, sid := nakedShort(t, "lt-tm-noack", 27740)
	w.noAcks()
	w.cancelStop(sid)
	checks := func(n int, d time.Duration) time.Time {
		t.Helper()
		for deadline := time.Now().Add(d); w.count(ntwire.FrameHeartbeat) < n; time.Sleep(2 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("want %d heartbeats from the server, got %d", n, w.count(ntwire.FrameHeartbeat))
			}
		}
		return time.Now()
	}
	n := w.count(ntwire.FrameHeartbeat)
	w.look()
	first := checks(n+1, 5*time.Second)
	if took := checks(n+2, 12*time.Second).Sub(first); took < 4800*time.Millisecond || took > 7*time.Second {
		t.Fatalf("the link check was asked again %s after the first, want about 5 s", took)
	}
	w.mustBeSilent("no ack")
}

// protectionRelookFloor is 10 ms. MUTANTS: 1 ms, 200 ms.
func TestLookTwiceTiming_RelookFloor(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	at := &AutoTrader{}
	at.protHolds = map[string]*protectionHold{"a": {firstLook: now.Add(-4 * time.Second), lastLook: now.Add(-4 * time.Second)}}
	if got := at.protectionRelookWaitLocked(now, 3*time.Second); got != 10*time.Millisecond {
		t.Fatalf("a re-look that is already due waits %s, want 10 ms", got)
	}
}

// ownReverseGiveUp is 70 s. MUTANTS: 55 s, 100 s.
func TestLookTwiceTiming_OwnReverseGiveUp(t *testing.T) {
	ownReverseDelay = 10 * time.Millisecond
	t.Cleanup(func() { ownReverseDelay = ownReverseDelayDefault })
	w, r := overClosed(t, "tm-giveup", "MNQ", "SHORT", long(1, 27753.25))
	w.redial() // no positions frame on this connection: nothing can be proven
	now := w.now()
	w.freezeAt(now)
	for waited, want := range map[time.Duration]float64{69 * time.Second: 0, 71 * time.Second: 1} {
		before := traderEvents(w.at.id, "own_reverse_unresolved")
		run, cancel := context.WithCancel(context.Background())
		ended := make(chan struct{})
		go func() { w.at.ownReverseLoop(run, w.s, w.nt, r, now.Add(-waited), ""); close(ended) }()
		time.Sleep(300 * time.Millisecond) // about 25 checks
		cancel()
		ends(t, 3*time.Second, "the watch never ends", func() { <-ended })
		if n := traderEvents(w.at.id, "own_reverse_unresolved") - before; n != want {
			t.Errorf("%s after the receipt: gave up %.0f times, want %.0f", waited, n, want)
		}
	}
}

// ownReverseGroup is 10 s: receipts 9.9 s apart are one over-close, 10.1 s apart are
// not. MUTANTS: 5 s, 20 s.
func TestLookTwiceTiming_OwnReverseGroup(t *testing.T) {
	for apart, want := range map[int64]int{9_900: 1, 10_100: 0} {
		w, r := overClosed(t, "tm-group", "MNQ", "SHORT", long(2, 27753.25))
		b := r
		b.ID, b.ExitMs = "over-2", r.ExitMs+apart
		if err := w.st.GormDB().Create(&b).Error; err != nil {
			t.Fatal(err)
		}
		if done, why := w.reverseCheck(b); !done || len(w.reverseP0s()) != want {
			t.Errorf("two receipts %d ms apart: done %v why %q P0s %d, want %d", apart, done, why, len(w.reverseP0s()), want)
		}
	}
}

// ownReverseSendWindow is 10 s. MUTANTS: 5 s, 14 s.
func TestLookTwiceTiming_OwnReverseSendWindow(t *testing.T) {
	read := time.Date(2026, 11, 19, 12, 47, 30, 0, time.UTC)
	for age, want := range map[time.Duration]bool{9900 * time.Millisecond: true, 10100 * time.Millisecond: false} {
		sent := []ntwire.OwnClose{{SignalID: "a", Market: true, SentAt: read.Add(-age)}}
		if got := ownCloseOnRecord(sent, store.NT8ExitReceipt{SignalID: "Close"}, read) != ""; got != want {
			t.Errorf("our market close %s before the fill: ours = %v, want %v", age, got, want)
		}
	}
}

// ownReverseEntrySlack is 5 s: an entry of ours that filled 4.9 s before the exit
// explains the position, 5.1 s before it does not. MUTANTS: 3 s, 8 s.
func TestLookTwiceTiming_OwnReverseEntrySlack(t *testing.T) {
	for ago, want := range map[int64]int{4_900: 0, 5_100: 1} {
		w, r := overClosed(t, "tm-slack", "MNQ", "SHORT", long(1, 27753.25))
		w.send(ntwire.FrameFill, ntwire.FillPayload{SignalID: "new-long", Symbol: "MNQ", Account: "Sim101", Side: "long",
			FillPrice: 27753.25, Quantity: 1, Status: "filled", FillTime: time.UnixMilli(r.ExitMs - ago).UTC().Format(time.RFC3339Nano)})
		for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(2 * time.Millisecond) {
			if _, ok := w.nt.EntryFillSince("MNQ", "LONG", 0); ok {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("fixture: the entry fill was not recorded")
			}
		}
		w.positions(long(1, 27753.25)) // the frame after the fill
		w.sync()
		if done, why := w.reverseCheck(r); !done || len(w.reverseP0s()) != want {
			t.Errorf("an entry of ours %d ms before the exit: done %v why %q P0s %d, want %d", ago, done, why, len(w.reverseP0s()), want)
		}
	}
}
