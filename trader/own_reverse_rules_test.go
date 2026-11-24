package trader

import (
	"fmt"
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/trader/types"
)

// ── OWN REVERSE, rule by rule (spec 2026-11-24 section 3 C) ──────────────────
// own_reverse_look_twice_test.go replays the incidents; this file takes one
// condition at a time, on the same real-frames fixture. Each names its mutant.

func TestOwnReverseSetting(t *testing.T) {
	for v, want := range map[string]string{"": "alert", "alert": "alert", " OFF ": "off", "off": "off",
		"close": "alert", "0": "alert", "yes": "alert"} {
		t.Setenv(ownReverseEnv, v)
		if got := ownReverseMode(); got != want {
			t.Errorf("%s=%q = %s, want %s", ownReverseEnv, v, got, want)
		}
		if l := OwnReverseBootLine(); !strings.HasPrefix(l, "own-reverse: "+want) || !strings.Contains(l, "("+ownReverseEnv+"="+v+")") {
			t.Errorf("the boot line must READ the mode in force and the raw setting: %q", l)
		}
	}
	if ownReverseDelay != 3*time.Second {
		t.Errorf("the detector's wait is %s in production, want 3 s", ownReverseDelay)
	}
	t.Setenv(ownReverseEnv, "close")
	if l := OwnReverseBootLine(); !strings.Contains(l, "REFUSED") {
		t.Errorf("close is refused in this release and the boot line must say so: %q", l)
	}
	t.Setenv(ownReverseEnv, "yes")
	if l := OwnReverseBootLine(); !strings.Contains(l, "bad value") {
		t.Errorf("a bad value must show: %q", l)
	}
}

// MUTANTS: drop the send-age bound; drop the Market leg; match any signal id.
func TestOwnCloseOnRecord(t *testing.T) {
	read := time.Date(2026, 11, 19, 6, 47, 30, 388e6, chicagoLoc())
	mk := func(sig string, market bool, ago time.Duration) ntwire.OwnClose {
		return ntwire.OwnClose{SignalID: sig, Market: market, SentAt: read.Add(-ago)}
	}
	for name, c := range map[string]struct {
		sent []ntwire.OwnClose
		sig  string
		want bool
	}{
		"nothing sent":                            {nil, "Close", false},
		"our market close 138 ms before":          {[]ntwire.OwnClose{mk("a", true, 138*time.Millisecond)}, "Close", true},
		"our market close 11 s before":            {[]ntwire.OwnClose{mk("a", true, 11*time.Second)}, "Close", false},
		"our market close AFTER the fill":         {[]ntwire.OwnClose{mk("a", true, -time.Millisecond)}, "Close", false},
		"a limit exit does not explain a Close":   {[]ntwire.OwnClose{mk("a", false, time.Second)}, "Close", false},
		"our limit exit by id, 20 min later":      {[]ntwire.OwnClose{mk("a", false, 20*time.Minute)}, "a", true},
		"another signal's limit exit":             {[]ntwire.OwnClose{mk("a", false, time.Second)}, "b", false},
		"a market close does not explain a -sl":   {[]ntwire.OwnClose{mk("a", true, time.Second)}, "entry-1", false},
		"an older own close behind a newer limit": {[]ntwire.OwnClose{mk("a", true, time.Second), mk("b", false, 0)}, "Close", true},
	} {
		if got := ownCloseOnRecord(c.sent, store.NT8ExitReceipt{SignalID: c.sig}, read) != ""; got != c.want {
			t.Errorf("%s: ours = %v, want %v", name, got, c.want)
		}
	}
}

// over1119 is the 11-19 shape with the close sent by a HAND: no send of ours.
func over1119(t *testing.T, id string) *lookWire {
	t.Helper()
	w := open1193(t, id, stop1193)
	w.stops1193(exits1193{})
	w.until(ms1193(254))
	w.flat1193()
	w.until(ms1193(388))
	w.orderUpdate("Close", "filled", 27753.25, 1)
	w.closeFrame("Close", "short", "manual", 27753.25, 1, 0, at1193(t, ms1193(388)))
	return w
}

// MUTANT: treat every "Close" as ours.
func TestOwnReverseHandCloseIsAlertOnlyAndStaysManual(t *testing.T) {
	fastReverse(t)
	w := over1119(t, "or-handclose")
	w.positions(long(1, 27753.25))
	w.sync()
	w.mustFlagReverse(8 * time.Second)
	if p := w.reverseP0s()[0]; strings.Contains(p.Title, "bot's close") || !strings.Contains(p.Body, "No close order of the bot is on record") {
		t.Fatalf("no send of ours: the P0 must not blame the bot's close: %q / %q", p.Title, p.Body)
	}
	if row := w.materialize("LONG"); !store.IsManualOrigin(row.Origin) {
		t.Fatalf("no send of ours: the row must stay manual, got %q", row.Origin)
	}
}

// The owner reads which side, how many, at what price, and that nothing was sent.
func TestOwnReverseP0NamesOurCloseAndMissingStop(t *testing.T) {
	fastReverse(t)
	w := reverse1119(t, "or-title", 27753.25)
	w.mustFlagReverse(8 * time.Second)
	p := w.reverseP0s()[0]
	if p.Kind != alertKindOwnReverse || !strings.Contains(p.Title, "The bot's close opened a reverse, no stop") ||
		!strings.Contains(p.Body, "LONG MNQ ×1 @ 27753.25") || !strings.Contains(p.Body, "Nothing was sent") {
		t.Fatalf("P0 = %s / %q / %q", p.Kind, p.Title, p.Body)
	}
	if !ownerPushKind("P0", p.Kind) {
		t.Fatal("the reverse P0 must reach the owner's phone")
	}
}

// MUTANT: drop PositionsAfterFills (act on the pre-fill frame).
func TestOwnReverseWaitsForAFrameNewerThanTheFill(t *testing.T) {
	fastReverse(t)
	w := over1119(t, "or-late-frame")
	w.mustNotFlagReverse(ownReverseDelay + time.Second) // the newest frame is the flat one from before the fill
	w.positions(long(1, 27753.25))
	w.sync()
	w.mustFlagReverse(5 * time.Second)
}

// MUTANT: compare the quantity of the receipt instead of the arithmetic.
func TestOwnReverseQuantityMustMatch(t *testing.T) {
	fastReverse(t)
	w := over1119(t, "or-qty")
	w.positions(long(2, 27753.25)) // 1 over-closed, 2 held: a hand added one
	w.sync()
	w.mustNotFlagReverse(ownReverseDelay + time.Second)
}

// MUTANT: accept any held side. A same-side position is not a reverse.
func TestOwnReverseSameSideIsNotAReverse(t *testing.T) {
	fastReverse(t)
	w := over1119(t, "or-side")
	w.positions(short(1, 27753.25))
	w.sync()
	w.mustNotFlagReverse(ownReverseDelay + time.Second)
}

// MUTANT: drop EntryFillSince. A normal long entry at the exit price is ours.
func TestOwnReverseOurOwnEntryIsNotAReverse(t *testing.T) {
	fastReverse(t)
	w := over1119(t, "or-entry")
	w.send(ntwire.FrameFill, ntwire.FillPayload{SignalID: "new-long", Symbol: "MNQ", Account: "Sim101", Side: "long",
		FillPrice: 27753.25, Quantity: 1, Status: "filled", FillTime: w.now().UTC().Format(time.RFC3339)})
	w.positions(long(1, 27753.25))
	w.sync()
	w.mustNotFlagReverse(ownReverseDelay + time.Second)
}

// MUTANT: drop the open-row check.
func TestOwnReverseOpenRowWithItsOwnEntryIsNotAReverse(t *testing.T) {
	fastReverse(t)
	w := over1119(t, "or-row")
	w.row("new-long", "LONG", 1, 27753.25, w.now())
	w.positions(long(1, 27753.25))
	w.sync()
	w.mustNotFlagReverse(ownReverseDelay + time.Second)
}

// MUTANT: drop the NoteOwnReverse row correction. The reconcile materialized the
// row (manual) BEFORE the check could prove anything.
func TestOwnReverseCorrectsAnAlreadyMaterializedRow(t *testing.T) {
	w := reverse1119(t, "or-flip", 27753.25)
	row := w.materialize("LONG")
	if !store.IsManualOrigin(row.Origin) {
		t.Fatalf("fixture: the check has not run yet, so the row must be manual, got %q", row.Origin)
	}
	w.mustBecomeBot(row.ID)
}

func (w *lookWire) mustBecomeBot(id int64) {
	w.t.Helper()
	for deadline := time.Now().Add(6 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var r store.TraderPosition
		if err := w.st.GormDB().First(&r, id).Error; err == nil && !store.IsManualOrigin(r.Origin) {
			return
		}
		if time.Now().After(deadline) {
			w.t.Fatalf("row %d is still manual", id)
		}
	}
}

// MUTANT: drop the first-seen bound. The reverse went flat; three minutes later
// a hand opens the same side, size and price: that one is manual.
func TestOwnReverseProofDoesNotReachALaterPosition(t *testing.T) {
	fastReverse(t)
	w := reverse1119(t, "or-later", 27753.25)
	w.mustFlagReverse(8 * time.Second)
	w.positions()
	w.sync()
	w.jump(3 * time.Minute)
	w.positions(long(1, 27753.25))
	w.sync()
	if row := w.materialize("LONG"); !store.IsManualOrigin(row.Origin) {
		t.Fatalf("a later hand position must stay manual, got %q", row.Origin)
	}
}

// MUTANTS: not bound to the Run's stop context; not joined by monitorWg.
func TestOwnReverseStopEndsTheWait(t *testing.T) {
	fastReverse(t)
	w := reverse1119(t, "or-stop", 27753.25)
	done := make(chan struct{})
	began := time.Now()
	go func() { w.at.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop must not wait out the check")
	}
	if took := time.Since(began); took > time.Second {
		t.Fatalf("Stop took %s", took)
	}
	w.mustNotFlagReverse(ownReverseDelay + time.Second)
}

// Another account's receipt, and a stopped trader, start nothing.
func TestOwnReverseStartsOnlyForTheBoundAccountOfARunningTrader(t *testing.T) {
	w := newLookWire(t, "or-bound", ctInstant(t, "2026-11-18 10:00:05.000"))
	r := store.NT8ExitReceipt{ID: "x", Account: "Sim999", Symbol: "MNQ", Side: "SHORT", SignalID: "Close", Quantity: 1, Price: 1}
	w.at.watchOwnReverse(r)
	w.at.isRunningMutex.Lock()
	w.at.isRunning = false
	w.at.isRunningMutex.Unlock()
	r.Account = "Sim101"
	w.at.watchOwnReverse(r)
	done := make(chan struct{})
	go func() { w.at.monitorWg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a check was started")
	}
	w.at.isRunningMutex.Lock()
	w.at.isRunning = true
	w.at.isRunningMutex.Unlock()
}

// ── THE FLATTEN P0 SAYS WHAT WAS SENT ────────────────────────────────────────

type closeFailTrader struct {
	MockTrader
	err error
}

func (c *closeFailTrader) CloseLong(string, float64) (map[string]interface{}, error) {
	return map[string]interface{}{}, c.err
}

var _ types.Trader = (*closeFailTrader)(nil)

// 11-19 06:47:30: the push read "flattening" before anything was sent. MUTANT:
// emit before the send / ignore the outcome.
func TestProtectionFlattenP0SaysWhatWasSent(t *testing.T) {
	flat := protectionVerdict{Action: protectionFlatten, StopPx: 31163, NeedQty: 1, Why: "through the fresh last price."}
	run := func(id string, err error, before func(at *AutoTrader, now time.Time)) *store.AlertDB {
		at, st := resetTrader(t, store.StrategyConfig{})
		at.id, at.exchange = id, "ninjatrader"
		at.trader = &closeFailTrader{err: err}
		t.Cleanup(func() { at.sweepStaleProtectionState(nil) })
		now := time.Now()
		if before != nil {
			before(at, now)
		}
		at.actOnProtection(flat, "MNQ", "LONG", 1, "sig", "t", 0, now)
		all, lerr := st.Alert().List(at.id, 10)
		if lerr != nil || len(all) != 1 {
			t.Fatalf("%s: want one P0, got %d (%v)", id, len(all), lerr)
		}
		return all[0]
	}
	sent := run("fl-sent", nil, nil)
	if !strings.Contains(sent.Title, "a market close was sent") || !strings.Contains(sent.Body, "Sent is not filled") {
		t.Errorf("sent: %q / %q", sent.Title, sent.Body)
	}
	failed := run("fl-failed", fmt.Errorf("no NT client connected"), nil)
	if !strings.Contains(failed.Title, "could NOT be sent") || strings.Contains(failed.Title+failed.Body, "was SENT") {
		t.Errorf("failed: %q / %q", failed.Title, failed.Body)
	}
	inflight := run("fl-inflight", nil, func(at *AutoTrader, now time.Time) {
		if got := at.flattenPositionAtMarket(&store.TraderPosition{TraderID: at.id, Symbol: "MNQ", Side: "LONG"}, "t", now, nil); got != marketFlatSent {
			t.Fatalf("fixture: %v", got)
		}
	})
	if !strings.Contains(inflight.Title, "already in flight") || !strings.Contains(inflight.Body, "no second one was sent") {
		t.Errorf("in flight: %q / %q", inflight.Title, inflight.Body)
	}
	for _, a := range []*store.AlertDB{sent, failed, inflight} {
		if strings.Contains(strings.ToLower(a.Title), "flattening") || strings.Contains(a.Body, "Closing the position at market now") {
			t.Errorf("the P0 still claims a close in progress: %q / %q", a.Title, a.Body)
		}
	}
}
