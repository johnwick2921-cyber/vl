package trader

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	nt "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── PARTIAL CLOSE (2026-10-03, mentor mode) ────────────────────────────────
//
// The owner's mentor sizing scales OUT HALF at 1:1, then break-even, then
// trail (owner order 10-02 22:2x CT). Today that is impossible on the wire:
// close_position flattens the whole position. This file is the EXECUTION half:
// the exact-quantity market exit (reduce_position), the per-partial ledger,
// and the IN-PLACE bracket shrink the AddOn performs atomically with the
// reduce (v2, review 2026-10-03 — no cancel-and-replace). EVERYTHING sits
// behind PARTIAL_CLOSE_ENABLED (default OFF) — nothing changes for the AI
// mode, which keeps its 1-contract rule.

// reduceQuantityCheck is the pre-send guard, PURE. A reduce is refused when it
// would close the whole position — a full close still goes through
// close_position — or is not a positive partial.
func reduceQuantityCheck(openQty, want int) (ok bool, reason string) {
	switch {
	case want <= 0:
		return false, "reduce quantity must be positive"
	case openQty <= 0:
		return false, "no open position to reduce"
	case want >= openQty:
		return false, fmt.Sprintf("reduce quantity %d >= open %d — a full close goes through close_position", want, openQty)
	}
	return true, fmt.Sprintf("reduce %d of %d leaves %d", want, openQty, openQty-want)
}

// ── THE V2 (review 2026-10-03) REDESIGN IN ONE SENTENCE ────────────────────
//
// There is NO cancel-and-replace. The AddOn shrinks the existing SL and TP IN
// PLACE (Account.Change, the D2 pattern from cs:2769) as part of
// reduce_position — atomically with the reduce — and reports the new bracket
// quantity on the fill. Go verifies it on the next snapshot and FAILS CLOSED
// (flatten) on a mismatch or an absent quantity. A naked window, a doubled
// stop, and an OCO cascade are structurally impossible.

// partialCloseEnabled: the partial-close feature has its OWN knob (review P2:
// split the knobs), default OFF. Nothing changes for the AI mode.
func partialCloseEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PARTIAL_CLOSE_ENABLED"))) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// verifyBracketResize is PURE: the book agrees the protective pair now carries
// `remaining`. Every WORKING -sl and -tp order for the symbol must carry
// exactly `remaining` quantity, and at least one of each leg must exist — a
// book that shows fewer or more legs than the position needs is a mismatch.
func verifyBracketResize(book []nt.NT8Order, symbol string, remaining int) (ok bool, why string) {
	if remaining <= 0 {
		return false, fmt.Sprintf("remaining %d is not a positive quantity", remaining)
	}
	sls, tps := 0, 0
	for i := range book {
		o := book[i]
		if !o.IsWorking() {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(o.Symbol), strings.TrimSpace(symbol)) {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(o.Name))
		switch {
		case strings.HasSuffix(name, "-sl"):
			sls++
			if o.Quantity != remaining {
				return false, fmt.Sprintf("the %s stop quantity is %d, the remainder is %d", o.Name, o.Quantity, remaining)
			}
		case strings.HasSuffix(name, "-tp"):
			tps++
			if o.Quantity != remaining {
				return false, fmt.Sprintf("the %s target quantity is %d, the remainder is %d", o.Name, o.Quantity, remaining)
			}
		}
	}
	if sls < 1 || tps < 1 {
		return false, fmt.Sprintf("the book shows %d stop and %d target leg(s) for a %d-lot remainder", sls, tps, remaining)
	}
	return true, fmt.Sprintf("the broker book confirms the bracket at %d", remaining)
}

// bracketVerifyOutcome is PURE: the only two exits from the verification
// window. A verified bracket keeps the remainder protected by the in-place
// pair; an expired unverified bracket FAILS CLOSED — the remainder is
// flattened, never left on an unproven stop.
func bracketVerifyOutcome(verified, expired bool) (action string) {
	if verified {
		return "confirmed"
	}
	if expired {
		return "flatten"
	}
	return "wait"
}

// bracketVerifyDeadlines is the in-memory verification window: client id ->
// deadline (unix ms). Set when a reduce fill reports a positive remainder,
// cleared on confirmation or fail-closed flatten. In-memory is correct: a
// restart re-verifies from the snapshot via the position the broker holds,
// and an unverifiable position after a restart is the reconciler's book.
var (
	bracketVerifyMu       sync.Mutex
	bracketVerifyDeadline = map[string]int64{}
)

// ResetBracketVerifyForTest clears the verification window (TESTS ONLY).
func ResetBracketVerifyForTest() {
	bracketVerifyMu.Lock()
	defer bracketVerifyMu.Unlock()
	bracketVerifyDeadline = map[string]int64{}
}

// bracketVerifyWindow is the bounded patience: two snapshot intervals is
// enough for the AddOn's Change + the next book.
func bracketVerifyWindow() time.Duration { return 2 * snapshotMaxAge() }

// recordBracketVerify opens the window for one client's bracket shrink.
func recordBracketVerify(clientID string, now time.Time) {
	bracketVerifyMu.Lock()
	defer bracketVerifyMu.Unlock()
	bracketVerifyDeadline[clientID] = now.Add(bracketVerifyWindow()).UnixMilli()
}

// bracketVerifyDue returns the client ids whose window has expired.
func bracketVerifyDue(now time.Time) []string {
	bracketVerifyMu.Lock()
	defer bracketVerifyMu.Unlock()
	var out []string
	for id, deadline := range bracketVerifyDeadline {
		if now.UnixMilli() >= deadline {
			out = append(out, id)
		}
	}
	return out
}

// ReducePosition runs the guarded partial close: knob, the quantity guard,
// the typed send (which itself refuses on an AddOn that never advertised
// reduce_position and applies the bound-account + SIM guards), and the
// request-time ledger row.
func (at *AutoTrader) ReducePosition(side string, qty, openQty int, who string) error {
	if !partialCloseEnabled() {
		return fmt.Errorf("partial close is disabled (PARTIAL_CLOSE_ENABLED off)")
	}
	nt := at.armedTrader()
	if nt == nil {
		return fmt.Errorf("partial close: no bound NT8 trader")
	}
	if ok, why := reduceQuantityCheck(openQty, qty); !ok {
		return fmt.Errorf("partial close REFUSED: %s", why)
	}
	side = strings.ToLower(strings.TrimSpace(side))
	if side != "long" && side != "short" {
		return fmt.Errorf("partial close: side must be long or short, got %q", side)
	}
	clientID := fmt.Sprintf("rx-%d-%s", time.Now().UnixMilli(), strings.ToLower(who))
	if err := nt.ReducePosition(side, qty, clientID); err != nil {
		return fmt.Errorf("partial close send refused: %w", err)
	}
	if at.store != nil {
		if err := at.store.PartialClose().RecordReduce(&store.PositionReduction{
			TraderID: at.id, Symbol: at.futuresSymbol(), Side: side,
			ClientID: clientID, Quantity: qty, Remaining: -1, BracketQty: -1, Who: who,
		}); err != nil {
			at.logWarnf("🧩 partial close: ledger write failed for %s: %v", clientID, err)
		}
	}
	at.logInfof("🧩 partial close REQUESTED %s %s qty=%d of %d (client=%s)", at.futuresSymbol(), side, qty, openQty, clientID)
	return nil
}

// armedReduceSubs caches each trader's reduce_fill channel. P0-1 (review
// 2026-10-03): SubscribeReduceFillsFor CLOSES and replaces the channel on
// every call, so a per-cycle subscribe drops every fill that arrives between
// cycles. Subscribe once on the miss path, exactly like armedSubs.
var armedReduceSubs sync.Map // trader id -> <-chan ReduceFillPayload

// reduceFillStream returns THIS trader's cached reduce_fill stream.
func (at *AutoTrader) reduceFillStream(tr *ntTrader.TCPTrader) <-chan nt.ReduceFillPayload {
	if v, ok := armedReduceSubs.Load(at.id); ok {
		if ch, _ := v.(<-chan nt.ReduceFillPayload); ch != nil {
			return ch
		}
	}
	ch := tr.ReduceFills()
	v, _ := armedReduceSubs.LoadOrStore(at.id, ch)
	stored, _ := v.(<-chan nt.ReduceFillPayload)
	return stored
}

// consumeReduceFills drains the trader's reduce_fill stream: every fill is
// applied to the ledger (latest-wins per client_id), and a fill with a
// positive remaining quantity opens the bracket-verify window. P1-7: gated —
// a no-op with the knob OFF.
func (at *AutoTrader) consumeReduceFills(nt *ntTrader.TCPTrader) {
	if !partialCloseEnabled() {
		return
	}
	ch := at.reduceFillStream(nt)
	if ch == nil {
		return
	}
	for {
		select {
		case f, open := <-ch:
			if !open {
				armedReduceSubs.Delete(at.id)
				at.logWarnf("📡 armed reduce_fill channel closed — re-subscribing next cycle")
				return
			}
			at.onReduceFill(nt, f)
		default:
			return
		}
	}
}

func (at *AutoTrader) onReduceFill(nt *ntTrader.TCPTrader, f nt.ReduceFillPayload) {
	if at.store != nil {
		if err := at.store.PartialClose().ApplyReduceFill(f.ClientID, f.FillPrice, f.Remaining, f.BracketQty); err != nil {
			at.logWarnf("🧩 partial close: fill ledger write failed for %s: %v", f.ClientID, err)
		}
	}
	at.logInfof("🧩 partial close FILLED %s %s client=%s qty=%d @ %.2f remaining=%d bracket_qty=%d",
		f.Symbol, f.Side, f.ClientID, f.Quantity, f.FillPrice, f.Remaining, f.BracketQty)
	// P1-5: an ABSENT remaining (-1) is unknown, never flat — fail closed.
	if f.Remaining < 0 {
		at.logWarnf("🧩 partial close fill %s carries remaining=%d (absent) — failing closed", f.ClientID, f.Remaining)
		at.flattenAfterResizeFailure("the reduce fill did not report the remaining quantity")
		return
	}
	if f.Remaining <= 0 {
		return // the remainder is flat: nothing to verify
	}
	if f.BracketQty != f.Remaining {
		// The AddOn's own report says the shrink did not land at the right
		// quantity. Do not wait for a snapshot that will disagree — fail
		// closed now.
		at.logWarnf("🧩 partial close fill %s bracket_qty=%d != remaining=%d — failing closed", f.ClientID, f.BracketQty, f.Remaining)
		at.flattenAfterResizeFailure("the AddOn reported a bracket quantity that does not match the remainder")
		return
	}
	recordBracketVerify(f.ClientID, time.Now())
}

// verifyBracketResizes is the per-cycle verification pass: every open window
// is checked against the FRESH broker book. A book that confirms the bracket
// at the remaining quantity closes the window; a window that expires without
// a confirming book FAILS CLOSED and flattens the remainder — never a silent
// unproven stop.
func (at *AutoTrader) verifyBracketResizes(now time.Time) {
	if !partialCloseEnabled() {
		return
	}
	due := bracketVerifyDue(now)
	if len(due) == 0 {
		return
	}
	book, have, age := at.liveBook(now)
	for _, clientID := range due {
		// The window expired; the book is the last word.
		verified := false
		if have && (snapshotMaxAge() <= 0 || age <= snapshotMaxAge()) {
			// The remaining quantity is the fill's own report, stored in the
			// ledger; the book must agree with IT.
			if at.store != nil {
				if rows, err := at.store.PartialClose().ListReductions(at.id); err == nil {
					for i := range rows {
						if rows[i].ClientID != clientID || rows[i].Remaining <= 0 {
							continue
						}
						if ok, _ := verifyBracketResize(book, at.futuresSymbol(), rows[i].Remaining); ok {
							verified = true
						}
					}
				}
			}
		}
		switch bracketVerifyOutcome(verified, true) {
		case "confirmed":
			bracketVerifyMu.Lock()
			delete(bracketVerifyDeadline, clientID)
			bracketVerifyMu.Unlock()
			at.logInfof("🧩 bracket resize CONFIRMED client=%s — the broker book agrees the protective pair covers the remainder", clientID)
		case "flatten":
			bracketVerifyMu.Lock()
			delete(bracketVerifyDeadline, clientID)
			bracketVerifyMu.Unlock()
			at.flattenAfterResizeFailure(fmt.Sprintf("the bracket shrink for %s was never confirmed by the book (age=%s)", clientID, age.Round(time.Second)))
		default:
			// wait: the due list is recomputed next cycle.
		}
	}
}

// flattenAfterResizeFailure is the fail-closed exit: close_position flattens
// the WHOLE remaining position at market. It runs only when the in-place
// bracket shrink cannot be verified — the remainder is never left on an
// unproven stop.
func (at *AutoTrader) flattenAfterResizeFailure(why string) {
	nt := at.armedTrader()
	if nt == nil {
		at.logErrorf("🧩 bracket resize FAIL-CLOSED but no bound trader to flatten: %s", why)
		return
	}
	at.logWarnf("🧩 bracket resize FAIL-CLOSED — flattening the remainder: %s", why)
	if _, err := nt.CloseLong(at.futuresSymbol(), 0); err != nil {
		at.logErrorf("🧩 bracket resize FAIL-CLOSED flatten SEND failed: %v", err)
	}
}
