package trader

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	nt "nofx/provider/ninjatrader"
	"nofx/store"
	"nofx/telemetry"
)

// ── CANCEL-CONFIRMATION (2026-09-06) ─────────────────────────────────────────
//
// THE DEFECT, IN ONE SENTENCE: a send is not a settlement.
//
// nt.CancelOrder is a one-line pass-through to SendCancelOrder
// (trader/ninjatrader/tcp_trader.go:558-562); its error is the result of PUTTING
// A FRAME ON A SOCKET. Five call sites in armed_executor.go treated `cerr == nil`
// as proof the order was gone and wrote the ledger `cancelled` on the strength of
// it (:342, :447, :490, :517, :837), and the sync helper wrote `cancelled` even
// on ACK TIMEOUT (:1933, "flatten proceeds"). So the ledger could say cancelled
// while the order still rested at the broker — which is exactly what
// nt8_order_snapshots id 1664 recorded: NINE working stop orders for ONE arm slot
// (2026-09-04:NY v3 S2 leg 0 SHORT), against nine ledger rows all reading
// 'cancelled'. They were harmless only because the order was malformed and inert.
//
// THE RULE (A20, class 6): a cancel is proven by the ORDER'S ABSENCE FROM A FRESH
// BROKER SNAPSHOT. Never by a return value, never by a log line, never by a
// ledger row. This file is the adjudication, kept PURE so a test can drive every
// branch with a real book; the clock is the caller's (A28).
//
// SIBLING TO class 79 (the reaper: silence is not death) and class 33 (the boot
// sweep). Their shared shape: an ABSENCE OF EVIDENCE is not evidence of absence.

// slotAction is what the adjudicator decided. It is deliberately not a bool:
// "allowed" and "refused because we cannot see" must not collapse together.
type slotAction string

const (
	// slotFree — a fresh book was read and it holds nothing for this slot.
	slotFree slotAction = "free"
	// slotLive — a fresh book still holds a non-terminal order for this slot.
	slotLive slotAction = "live"
	// slotUnverifiable — no book, or a book too old to believe. AN
	// UNVERIFIABLE SLOT IS NOT AN EMPTY SLOT (A24: UNKNOWN never takes the
	// destructive branch, and here PLACING is the destructive branch).
	slotUnverifiable slotAction = "unverifiable"
)

// slotVerdict is the whole adjudication as one value, so a caller cannot act on
// half of it and a test can assert all of it.
type slotVerdict struct {
	Action     slotAction
	Why        string
	SignalID   string // the live signal that blocked it, when Action == slotLive
	OrderID    string
	State      string // the broker's own word for that order
	SnapshotID int64  // which snapshot settled it; 0 when none did
	BookAge    time.Duration
}

// Allowed reports whether a placement may proceed. ONLY slotFree allows.
func (v slotVerdict) Allowed() bool { return v.Action == slotFree }

// Refusal renders the refusal exactly as D3 specifies, naming the signal, the
// order and the snapshot — the three ids a reader needs to check the claim.
func (v slotVerdict) Refusal() string {
	switch v.Action {
	case slotLive:
		return fmt.Sprintf("refused: slot already live at the broker (signal %s, order %s, state %s, snapshot %d, book age %s)",
			shortID(v.SignalID), shortID(v.OrderID), v.State, v.SnapshotID, v.BookAge.Round(time.Second))
	case slotUnverifiable:
		return fmt.Sprintf("refused: slot unverifiable — %s (an unverifiable slot is not an empty slot)", v.Why)
	}
	return ""
}

// orderBelongsToSlot matches a book order to one of a slot's signal ids. NT8
// names the entry after the signal and its bracket children "<signal>-sl" /
// "<signal>-tp", so the prefix is the join key — the same identity the exit-fill
// audit used to resolve 54 of 58 closes with zero ambiguity.
func orderBelongsToSlot(name string, slotSignalIDs []string) (string, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return "", false
	}
	for _, sid := range slotSignalIDs {
		s := strings.ToLower(strings.TrimSpace(sid))
		if s == "" {
			continue
		}
		if n == s || strings.HasPrefix(n, s+"-") {
			return sid, true
		}
	}
	return "", false
}

// adjudicateSlot decides whether an arm slot may be placed into.
//
// haveBook says a snapshot was actually READ (not that it was non-empty — an
// account with no orders legitimately emits an empty list, and that is a FREE
// slot, not an unverifiable one). maxAge is the resolved staleness bound.
//
// PURE: no clock, no store, no wire. The caller supplies the book, its age and
// the bound (A28).
func adjudicateSlot(
	book []nt.NT8Order, haveBook bool, bookAge, maxAge time.Duration,
	snapshotID int64, slotSignalIDs []string,
) slotVerdict {
	if !haveBook {
		return slotVerdict{Action: slotUnverifiable, Why: "no broker snapshot has been received", BookAge: bookAge}
	}
	if maxAge > 0 && bookAge > maxAge {
		return slotVerdict{
			Action: slotUnverifiable,
			Why: fmt.Sprintf("broker book is %s old, older than the %s bound",
				bookAge.Round(time.Second), maxAge.Round(time.Second)),
			BookAge: bookAge, SnapshotID: snapshotID,
		}
	}
	for i := range book {
		o := book[i]
		// IsWorking is the ONE definition of non-terminal (leg 4 and the override
		// guard read the same set) — reused, never re-implemented (A24).
		// CancelSubmitted and CancelPending are NOT terminal, which is why a
		// cancel still in flight correctly keeps the slot locked: measured 130
		// and 22 occurrences respectively across 360 live frames.
		if !o.IsWorking() {
			continue
		}
		if sid, ok := orderBelongsToSlot(o.Name, slotSignalIDs); ok {
			return slotVerdict{
				Action: slotLive, SignalID: sid, OrderID: o.OrderID, State: o.State,
				SnapshotID: snapshotID, BookAge: bookAge,
				Why: "a non-terminal order for this slot is still at the broker",
			}
		}
	}
	return slotVerdict{Action: slotFree, SnapshotID: snapshotID, BookAge: bookAge,
		Why: "no non-terminal order for this slot in a fresh book"}
}

// cancelSettled decides whether a requested cancel is CONFIRMED GONE.
//
// The same rule from the other side: the order is gone when a FRESH book no
// longer lists it as non-terminal. A stale or absent book settles NOTHING — it
// must never promote a row to cancelled, because 'cancelled' is what unlocks a
// replacement (D2).
func cancelSettled(
	book []nt.NT8Order, haveBook bool, bookAge, maxAge time.Duration, signalID string,
) (settled bool, why string) {
	if strings.TrimSpace(signalID) == "" {
		return false, "no signal id — nothing to look for"
	}
	if !haveBook {
		return false, "no broker snapshot has been received"
	}
	if maxAge > 0 && bookAge > maxAge {
		return false, fmt.Sprintf("book is %s old, older than the %s bound", bookAge.Round(time.Second), maxAge.Round(time.Second))
	}
	for i := range book {
		o := book[i]
		if !o.IsWorking() {
			continue
		}
		if _, ok := orderBelongsToSlot(o.Name, []string{signalID}); ok {
			return false, "still at the broker as " + o.State
		}
	}
	return true, "absent from a fresh book"
}

// shortID keeps a log line readable without inventing a value.
func shortID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "n/a"
	}
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// ── READING THE BOOK ─────────────────────────────────────────────────────────
//
// TWO READERS, DELIBERATELY DIFFERENT, because the two decisions have opposite
// failure costs.
//
//   the SLOT GUARD may read the in-memory cache. Refusing every placement
//   because a forensics INSERT failed would be a trading outage caused by a
//   logging bug, and main.go says so in its own words at the sink: "Forensics
//   are worth having and never worth a frame."
//
//   CONFIRMING A CANCEL requires a PERSISTED snapshot id. 'cancelled' is the
//   word that unlocks a replacement, so the evidence for it must still be
//   citable tomorrow. If the snapshots table is not being written, cancels
//   simply do not confirm — they stay pending and say so, loudly, which is the
//   correct failure mode (D2) rather than a silent promotion.

// snapshotMaxAge is the resolved staleness bound: leg 4's own multiple of the
// resolved snapshot interval. ONE definition of stale, reused (A24).
func snapshotMaxAge() time.Duration { return 2 * OrderSnapshotInterval() }

// cancelConfirmTimeout is how long a requested cancel may go unconfirmed before
// it is called out and re-requested. [I] PROVISIONAL — owner-set default,
// resolved from the environment so it can be moved without a build.
func cancelConfirmTimeout() time.Duration {
	if v := strings.TrimSpace(os.Getenv("CANCEL_CONFIRM_TIMEOUT_S")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 90 * time.Second
}

// cancelReRequestMax bounds re-requests so a broker that never answers cannot
// make us send forever.
func cancelReRequestMax() int {
	if v := strings.TrimSpace(os.Getenv("CANCEL_REREQUEST_MAX")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 5
}

// liveBook returns the freshest book this process can see for the slot guard,
// preferring the in-memory cache the cutover gate reads.
func (at *AutoTrader) liveBook(now time.Time) (orders []nt.NT8Order, haveBook bool, age time.Duration) {
	cache, account, _ := at.brokerBook()
	if cache == nil {
		return nil, false, 0
	}
	snap, ok := cache.Latest(account)
	if !ok {
		return nil, false, 0
	}
	if a, ok2 := cache.AgeAt(account, now); ok2 {
		age = a
	}
	return snap.Orders, true, age
}

// persistedBook returns the freshest PERSISTED snapshot — the one that carries
// an id a later reader can check. Used only where the evidence must survive.
func (at *AutoTrader) persistedBook(now time.Time) (orders []nt.NT8Order, haveBook bool, age time.Duration, snapshotID int64) {
	if at == nil || at.store == nil {
		return nil, false, 0, 0
	}
	_, account, _ := at.brokerBook()
	// symbol is deliberately empty: the writer (main.go) never sets it, so every
	// stored row carries symbol='' and a symbol-scoped lookup would find nothing.
	row, err := at.store.NT8OrderSnapshots().Latest(account, "")
	if err != nil || row == nil {
		return nil, false, 0, 0
	}
	var book []nt.NT8Order
	if err := json.Unmarshal([]byte(row.OrdersJSON), &book); err != nil {
		at.logWarnf("🧾 cancel: snapshot %d has unreadable orders_json — settling nothing from it: %v", row.ID, err)
		return nil, false, 0, row.ID
	}
	if row.ReceivedMs > 0 {
		age = time.Duration(now.UnixMilli()-row.ReceivedMs) * time.Millisecond
	}
	return book, true, age, row.ID
}

// slotSignalIDs collects every signal id this arm slot has ever been given.
//
// THE SLOT KEY IS (plan_id, scenario, leg_index) — the same key the DB's unique
// index and UpsertArm's own lookup use. VERSION IS NOT IN IT: armed_orders.Version
// is documented at store/armed_orders.go:25-28 as the LAST version that touched
// the row, not the one it was armed under, so keying on it would split one slot
// into several and let a re-authorized slot place beside its own live order.
//
// Side is not in it either. Twenty-two rows shared this key on 2026-09-04 and
// twenty of them reached the broker; the guard must ask about all of their
// signals, not only the row it is about to place.
func slotSignalIDs(rows []store.ArmedOrderDB, r store.ArmedOrderDB) []string {
	out := make([]string, 0, 4)
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	add(r.SignalID)
	for i := range rows {
		o := rows[i]
		if o.TraderID == r.TraderID && o.PlanID == r.PlanID && o.Scenario == r.Scenario &&
			o.LegIndex == r.LegIndex {
			add(o.SignalID)
		}
	}
	return out
}

// ── THE TWO PRODUCTION ENTRY POINTS ──────────────────────────────────────────

// armSlotGuard is D3: before ANY placement for an arm slot, the broker's fresh
// book must show ZERO non-terminal orders carrying that slot's signal ids.
// A live order refuses. A book we cannot see ALSO refuses.
//
// This is the wave's only new reject, and it is owner-ruled.
func (at *AutoTrader) armSlotGuard(ledgerRows []store.ArmedOrderDB, r store.ArmedOrderDB, now time.Time) slotVerdict {
	sigs := slotSignalIDs(ledgerRows, r)
	if len(sigs) == 0 {
		// A slot that has never been given a signal id cannot have an order at
		// the broker under any name we could match. That is a genuinely free
		// slot, not an unverifiable one — and refusing it would deadlock the
		// very first placement of every arm.
		return slotVerdict{Action: slotFree, Why: "slot has never been placed (no signal id to look for)"}
	}
	book, have, age := at.liveBook(now)
	_, _, _, snapID := at.persistedBook(now)
	v := adjudicateSlot(book, have, age, snapshotMaxAge(), snapID, sigs)
	return v
}

// refuseSlot logs and counts a D3 refusal. Deduped per slot+class so a refusal
// that persists for many cycles is stated once per distinct condition, not once
// per cycle (the armRefusalChanged pattern this file reuses rather than
// reinvents).
func (at *AutoTrader) refuseSlot(r store.ArmedOrderDB, v slotVerdict, what string, now time.Time) {
	class := "slot_live_at_broker"
	if v.Action == slotUnverifiable {
		class = "slot_unverifiable"
		// Owner ruling 2026-09-06: a dark AddOn must read as an OUTAGE, not as
		// a quiet no-trade day. Once per outage, with the book age.
		at.raiseBookOutageAlert(v.BookAge, now)
	}
	key := r.PlanID + ":" + strconv.Itoa(r.Version) + ":" + r.Scenario + ":leg" +
		strconv.Itoa(r.LegIndex+1) + ":slotguard"
	if armRefusalChanged(&at.armRefusalLast, key, class) {
		telemetry.IncGateBlock(at.id, "arm_"+class)
		at.logWarnf("🧾 armed %s %s REFUSED — %s", r.Scenario, what, v.Refusal())
	}
}

// confirmPendingCancels is the per-cycle settlement pass (D1/D2). It is the
// ONLY place a cancel becomes 'cancelled' through the cancel path, and the
// only place a zone-rest row re-arms after a CONFIRMED cancel (WAVE PLANNER
// B1, P1 fold — the re-arm is booked on the book's word, never on the
// request).
//
// A10/class 23: it is telemetry-shaped — a failed read WARNs and returns; it
// never stops the loop and never promotes a row on ignorance.
// errCancelRefused is what a re-request's cancelFn returns when the filled-arm
// guard refused the cancel: nothing was sent (W-EXEC-TRUTH W0 (f), canon 35).
var errCancelRefused = errors.New("cancel refused by the filled-arm guard")

func (at *AutoTrader) confirmPendingCancels(ledger *store.ArmedOrderStore, cancelFn func(string) error, now time.Time) (settled, stillPending, reRequested int) {
	if at == nil || ledger == nil {
		return 0, 0, 0
	}
	// W-EXEC-TRUTH W0 (f): the armed pass (runCycle) and the withdraw
	// (monitorTick) both settle cancels — never both at once, or one re-request
	// is sent and counted twice.
	if !at.cancelConfirmMu.TryLock() {
		return 0, 0, 0
	}
	defer at.cancelConfirmMu.Unlock()
	rows, err := ledger.ListCancelPending(at.id)
	if err != nil {
		at.logWarnf("🧾 cancel confirm: ledger read failed — settling nothing this cycle: %v", err)
		return 0, 0, 0
	}
	if len(rows) == 0 {
		return 0, 0, 0
	}
	// The EVIDENCE reader: confirming requires a persisted snapshot id.
	book, have, age, snapID := at.persistedBook(now)
	maxAge := snapshotMaxAge()
	timeout := cancelConfirmTimeout()
	cap := cancelReRequestMax()

	for i := range rows {
		r := rows[i]
		ok, why := cancelSettled(book, have, age, maxAge, r.SignalID)
		if ok && snapID > 0 {
			// WAVE PLANNER B1 (P1 fold, CTO #213): a zone-rest cancel the book
			// CONFIRMS is not the end of the arm — the row returns to
			// armed-unplaced (placement stamp cleared, seq+1) for a NEW signal
			// on the next placement. The reset runs on the SAME evidence
			// ConfirmCancel demands (snapID > 0): a book that never proved the
			// order gone can never re-arm the row.
			if rearm, rearmWhy := at.zoneRestReArmOnConfirm(r, why); rearm {
				if err := ledger.ResetToArmedUnplaced(r.ID, rearmWhy); err != nil {
					at.logWarnf("🧾 cancel confirm: re-arm write failed for %s: %v", r.Scenario, err)
					continue
				}
				settled++
				at.logInfof("🧾 cancel CONFIRMED %s signal=%s — %s (snapshot %d, book age %s, attempts %d) — returned to armed-unplaced, re-placeable",
					r.Scenario, shortID(r.SignalID), why, snapID, age.Round(time.Second), r.CancelAttempts)
				continue
			}
			// The ORIGINAL reason survives the confirmation. Each cancel site
			// names WHY it cancelled (gate changed, one_live_arm_guard,
			// entry_gate, condition_shadowed…) and that word is the only record
			// of the decision; a confirmation that overwrote it would trade one
			// kind of blindness for another.
			if err := ledger.ConfirmCancel(r.ID, snapID, strings.TrimSpace(r.StateReason)+" — confirmed: "+why); err != nil {
				at.logWarnf("🧾 cancel confirm: ledger write failed for %s: %v", r.Scenario, err)
				continue
			}
			settled++
			at.logInfof("🧾 cancel CONFIRMED %s signal=%s — %s (snapshot %d, book age %s, attempts %d)",
				r.Scenario, shortID(r.SignalID), why, snapID, age.Round(time.Second), r.CancelAttempts)
			continue
		}
		stillPending++
		// A9 — every unconfirmed cancel says so, with its age and its reason.
		reqAge := time.Duration(0)
		if r.CancelRequestedAtMs > 0 {
			reqAge = time.Duration(now.UnixMilli()-r.CancelRequestedAtMs) * time.Millisecond
		}
		if reqAge < timeout {
			continue // still inside its window; nothing to say yet
		}
		telemetry.IncGateBlock(at.id, "cancel_unconfirmed")
		if r.CancelAttempts >= cap {
			at.logWarnf("🧾 cancel UNCONFIRMED %s signal=%s after %s and %d attempt(s) — attempt cap reached, NOT re-requesting and NOT promoting to cancelled (%s)",
				r.Scenario, shortID(r.SignalID), reqAge.Round(time.Second), r.CancelAttempts, why)
			continue
		}
		if cancelFn == nil {
			at.logWarnf("🧾 cancel UNCONFIRMED %s signal=%s after %s — no wire to re-request on (%s)",
				r.Scenario, shortID(r.SignalID), reqAge.Round(time.Second), why)
			continue
		}
		if cerr := cancelFn(r.SignalID); errors.Is(cerr, errCancelRefused) {
			// The filled-arm guard refused it: nothing was sent, so nothing is
			// recorded or counted as a re-request (canon 35). The row stays
			// cancel_pending and the guard is asked again next pass.
			at.logWarnf("🧾 cancel re-request REFUSED %s signal=%s after %s — not sent, not recorded (%s)",
				r.Scenario, shortID(r.SignalID), reqAge.Round(time.Second), why)
			continue
		} else if cerr != nil {
			at.logWarnf("🧾 cancel re-request SEND FAILED %s signal=%s: %v", r.Scenario, shortID(r.SignalID), cerr)
		}
		// The re-request is recorded whether or not the SEND returned nil —
		// because the send is not the point.
		if err := ledger.RequestCancel(r.ID, "re-requested after "+reqAge.Round(time.Second).String()+" unconfirmed", now.UnixMilli()); err != nil {
			at.logWarnf("🧾 cancel re-request: ledger write failed for %s: %v", r.Scenario, err)
			continue
		}
		reRequested++
		at.logWarnf("🧾 cancel UNCONFIRMED %s signal=%s after %s (%s) — re-requested, attempt %d of %d; the row stays %s",
			r.Scenario, shortID(r.SignalID), reqAge.Round(time.Second), why, r.CancelAttempts+1, cap, store.StateCancelPending)
	}
	return settled, stillPending, reRequested
}

// ── D4 — RECONCILE WHAT IS ALREADY WRONG (three-state, A30) ──────────────────
//
// For every non-terminal ledger row, ask the freshest book. NOTHING is
// auto-cancelled: a row the broker still lists is a WARN and a count, and the
// owner decides. Never delete a row.

// ReconcileCounts is one pass's three-state result. Counts, never rates (A24).
type ReconcileCounts struct {
	ConfirmedGone int     // the book agrees the order is gone
	LiveAtBroker  int     // the book still lists it — the dangerous case
	Unconfirmed   int     // no fresh book; nothing can be said
	LiveIDs       []int64 // sample-id law (A21)
	SnapshotID    int64
	Ran           bool // false until a book has been seen at all
}

// cancelReconcileDone latches the once-per-boot reconciliation per trader. It
// is NOT latched when no book was available, so the pass retries until it can
// actually answer — an unanswered question is not a finished one.
var cancelReconcileDone sync.Map // trader id -> ReconcileCounts

// ResetCancelReconcileForTest clears the latch.
func ResetCancelReconcileForTest() { cancelReconcileDone = sync.Map{} }

// LastReconcile returns what the once-per-boot pass measured, for the boot line.
func LastReconcile(traderID string) ReconcileCounts {
	if v, ok := cancelReconcileDone.Load(traderID); ok {
		if c, ok2 := v.(ReconcileCounts); ok2 {
			return c
		}
	}
	return ReconcileCounts{}
}

// reconcileOncePerBoot runs the D4 pass at the first cycle where a book exists.
func (at *AutoTrader) reconcileOncePerBoot(ledger *store.ArmedOrderStore, now time.Time) {
	if _, done := cancelReconcileDone.Load(at.id); done {
		return
	}
	_, have, age, _ := at.persistedBook(now)
	if !have || (snapshotMaxAge() > 0 && age > snapshotMaxAge()) {
		return // no usable book yet — ask again next cycle rather than invent
	}
	cancelReconcileDone.Store(at.id, at.reconcileAgainstBroker(ledger, now))
}

// reconcileAgainstBroker compares the ledger's live rows to the broker's book.
// It runs once the book is available — NOT at process start, where there is no
// book and any answer would be invented.
func (at *AutoTrader) reconcileAgainstBroker(ledger *store.ArmedOrderStore, now time.Time) ReconcileCounts {
	var c ReconcileCounts
	if at == nil || ledger == nil {
		return c
	}
	rows, err := ledger.ListNonTerminal(at.id)
	if err != nil {
		at.logWarnf("🧾 cancel reconcile: ledger read failed — reconciling nothing: %v", err)
		return c
	}
	book, have, age, snapID := at.persistedBook(now)
	c.SnapshotID = snapID
	c.Ran = true
	maxAge := snapshotMaxAge()
	for i := range rows {
		r := rows[i]
		if strings.TrimSpace(r.SignalID) == "" {
			// Never placed: there is nothing at the broker to reconcile
			// against, and calling that "confirmed gone" would be a claim we
			// did not earn.
			c.Unconfirmed++
			continue
		}
		gone, _ := cancelSettled(book, have, age, maxAge, r.SignalID)
		switch {
		case !have || (maxAge > 0 && age > maxAge):
			c.Unconfirmed++
		case gone:
			c.ConfirmedGone++
		default:
			c.LiveAtBroker++
			c.LiveIDs = append(c.LiveIDs, r.ID)
		}
	}
	if c.LiveAtBroker > 0 {
		at.logWarnf("🧾 cancel reconcile: %d ledger row(s) are LIVE AT THE BROKER right now (ids %v, snapshot %d, book age %s) — NOT auto-cancelled; the owner decides",
			c.LiveAtBroker, c.LiveIDs, c.SnapshotID, age.Round(time.Second))
	}
	at.logInfof("🧾 cancel reconcile: confirmed_gone=%d live_at_broker=%d unconfirmed=%d (snapshot %d, book age %s)",
		c.ConfirmedGone, c.LiveAtBroker, c.Unconfirmed, c.SnapshotID, age.Round(time.Second))
	return c
}

// CancelBootLine is D6. EVERY field is READ (A11): the counts come from the
// table, the timeout and the staleness bound from their own resolvers.
//
// The reconciliation half prints n/a until a book has been seen — at process
// start there is no broker book, and a number invented there would be exactly
// the fabrication this wave exists to remove.
//
// NOTE for whoever writes a watcher: the 🧾 glyph is shared by nine other log
// sites in this tree. Key on the text "cancels:", never on the glyph (A24).
func CancelBootLine(st *store.Store, rec ReconcileCounts, nowMs int64) string {
	pending, unconfirmed := int64(0), int64(0)
	// B2 — rows whose attempts a DEPARTED process counted. -1 means the ledger
	// could not be read, and prints UNKNOWN rather than a zero nobody measured.
	carry := int64(-1)
	if st != nil {
		ao := st.ArmedOrders()
		pending = ao.CountCancelPending()
		unconfirmed = ao.CountCancelUnconfirmed(nowMs, cancelConfirmTimeout().Milliseconds())
		carry = ao.CountForeignBootCancelAttempts()
	}
	carryStr := "UNKNOWN"
	if carry >= 0 {
		carryStr = strconv.FormatInt(carry, 10)
	}
	reconciled := "reconciled=n/a (no broker book yet)"
	if rec.Ran {
		reconciled = fmt.Sprintf("reconciled(confirmed=%d live=%d unconfirmed=%d snapshot=%d)",
			rec.ConfirmedGone, rec.LiveAtBroker, rec.Unconfirmed, rec.SnapshotID)
	}
	return fmt.Sprintf(
		"cancels: confirm=broker-snapshot · pending=%d · unconfirmed=%d · slot-guard=on(refuse-on-live|stale) · timeout=%s · stale-bound=%s · rerequest-cap=%d budget=per-process carry=%s (inert until a cancel is re-requested after a restart) · %s",
		pending, unconfirmed, cancelConfirmTimeout(), snapshotMaxAge(), cancelReRequestMax(), carryStr, reconciled)
}

// ── A DARK ADDON IS AN OUTAGE, NOT A QUIET DAY (owner ruling 2026-09-06) ─────
//
// D3 refuses placements on a stale or absent book. Refusals are counted
// (arm_slot_unverifiable) — but a counter nobody is looking at renders a dark
// AddOn as a day on which nothing happened to trade. It is not: it is an
// outage, and it is the owner's to see.
//
// ONE P0 PER OUTAGE. emitAlert dedupes on EventID, so the id carries the
// outage's START instant: every refusal inside one outage collapses to the
// alert already on screen, and a LATER outage raises a new one. When the book
// returns the alert is acked by that same id, which clears the banner.
//
// The counter is unchanged — the owner asked for one alert beside it, not a
// second counter.

// bookOutageSince holds the start of the current book outage per trader.
// Absent = the book is healthy as far as we last saw.
var bookOutageSince sync.Map // trader id -> int64 (unix ms of outage start)

// ResetBookOutageForTest clears the outage latch.
func ResetBookOutageForTest() { bookOutageSince = sync.Map{} }

// bookOutageEventID is stable for the life of ONE outage, which is what makes
// the alert fire once and clear cleanly.
func bookOutageEventID(startMs int64) string {
	return "cancel_book_stale:" + strconv.FormatInt(startMs, 10)
}

// raiseBookOutageAlert opens an outage if none is open and raises the P0 once.
func (at *AutoTrader) raiseBookOutageAlert(age time.Duration, now time.Time) {
	if at == nil {
		return
	}
	startMs := now.UnixMilli()
	if prev, loaded := bookOutageSince.LoadOrStore(at.id, startMs); loaded {
		// An outage is already open — the alert for it is already on screen.
		_ = prev
		return
	}
	at.emitAlert("P0", "broker_book",
		bookOutageEventID(startMs),
		"Broker order book is dark — arm placement is REFUSED",
		fmt.Sprintf("No fresh NT8 order_snapshot: the newest book is %s old against a %s bound. "+
			"Every arm placement is refused while this lasts, because an unverifiable slot is not an empty slot — "+
			"this is an OUTAGE, not a quiet session. Check that NinjaTrader and the VL AddOn are running. "+
			"This alert clears itself when a fresh book arrives.",
			age.Round(time.Second), snapshotMaxAge()))
	at.logErrorf("🚨 broker book DARK — book %s old against a %s bound; arm placement REFUSED until it returns (P0 raised once for this outage)",
		age.Round(time.Second), snapshotMaxAge())
}

// clearBookOutageIfHealthy closes an open outage when a fresh book is seen and
// acks the alert that announced it.
func (at *AutoTrader) clearBookOutageIfHealthy(now time.Time) {
	if at == nil || at.store == nil {
		return
	}
	_, have, age := at.liveBook(now)
	fresh := have && (snapshotMaxAge() <= 0 || age <= snapshotMaxAge())
	if !fresh {
		return
	}
	v, open := bookOutageSince.Load(at.id)
	if !open {
		return
	}
	startMs, _ := v.(int64)
	bookOutageSince.Delete(at.id)
	if _, err := at.store.Alert().AckByEvent(at.id, bookOutageEventID(startMs)); err != nil {
		at.logWarnf("🚨 broker book recovered but the outage alert could not be acked: %v", err)
	}
	at.logWarnf("🚨 broker book RECOVERED after %s — arm placement resumes (outage alert cleared)",
		time.Duration(now.UnixMilli()-startMs)*time.Millisecond)
}

// dayPlanOffPassHead is the armed pass's head while Day Plan is OFF (or the
// trader is not an NT8 trader with a store): settle first, so a fill or a
// confirmed cancel is in the ledger before the Picture sweep reads it (the
// FIX 1 drain-before-guards order), then the D21 Picture sweep.
func (at *AutoTrader) dayPlanOffPassHead(now time.Time) {
	at.settleArmedLedgerWhileOff(now)
	at.pictureDayPlanOffSweep(now)
}

// settleArmedLedgerWhileOff is the SETTLEMENT half of the pass, run at the
// head while Day Plan is OFF (W5 R8, CTO round 2): drain the order updates and
// confirm every requested cancel from the fresh broker book, exactly as when
// ON. Nothing here places, arms or authors. Without it a cancel the OFF sweep
// requested stayed cancel_pending forever (every settlement site sat below the
// OFF return, the boot sweep excludes cancel_pending), and the entry latch —
// which counts a non-terminal row with a signal as PLACED — refused every AI
// entry until the Day Plan came back ON. Planner rows had the same freeze
// before W5; this closes it for both.
func (at *AutoTrader) settleArmedLedgerWhileOff(now time.Time) {
	if at == nil || at.store == nil || at.exchange != "ninjatrader" || at.dayPlanEnabled() {
		return
	}
	ledger := at.store.ArmedOrders()
	nt := at.armedTrader()
	if ledger == nil || nt == nil {
		return
	}
	at.consumeArmedOrderUpdates(nt, ledger)
	at.confirmPendingCancels(ledger, func(sid string) error {
		// A re-request is still a cancel: the filled-arm guard decides (W0 (f)).
		if !at.cancelSignalIfSafe(nt.CancelOrder, sid, "cancel re-request", now) {
			return errCancelRefused
		}
		return nil
	}, now)
}
