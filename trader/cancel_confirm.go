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

	nt "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
	ntTrader "vl/trader/ninjatrader"
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
	// ReportRegime marks a refusal produced by the cancel-report regime (an
	// unconfirmed cancel, or an AddOn below the floor). It is NOT a broker-book
	// outage, and must not raise the book-outage P0 or its counters.
	ReportRegime bool
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
	if bookAge < 0 {
		return false, "broker snapshot receipt is in the future relative to the evaluation clock"
	}
	if maxAge > 0 && bookAge > maxAge {
		return false, fmt.Sprintf("book is %s old, older than the %s bound", bookAge.Round(time.Second), maxAge.Round(time.Second))
	}
	for i := range book {
		o := book[i]
		if !o.IsWorking() {
			// N7 part 5 (CTO, release #4): a FILLED entry is terminal but it is
			// not a cancel — the fill path owns that row. Settling it here would
			// mark a filled entry cancelled before its fill frame lands, and the
			// N7 forget would then drop the latch's in-flight protection.
			if entryFilledInBook(o, signalID) {
				return false, "filled at the broker (" + o.State + ") — the filled path owns it"
			}
			continue
		}
		if _, ok := orderBelongsToSlot(o.Name, []string{signalID}); ok {
			return false, "still at the broker as " + o.State
		}
	}
	return true, "absent from a fresh book"
}

// entryFilledInBook reports whether o is the slot's ENTRY order (named exactly
// after the signal — never a "-sl"/"-tp" child) in a filled terminal state.
func entryFilledInBook(o nt.NT8Order, signalID string) bool {
	if !strings.EqualFold(strings.TrimSpace(o.Name), strings.TrimSpace(signalID)) {
		return false
	}
	return o.TerminalOutcome() == nt.OrderOutcomeFilled
}

// cancelSeenInBook (N7 part 5) is the POSITIVE cancel proof the latch forget
// needs: the slot's entry order is in the fresh book as Cancelled / Rejected /
// Expired. Mere absence settles the ROW (cancelSettled) but never clears the
// broker-side pending marker — the 45s sweep owns that.
func cancelSeenInBook(book []nt.NT8Order, signalID string) bool {
	if strings.TrimSpace(signalID) == "" {
		return false
	}
	for i := range book {
		o := book[i]
		if !strings.EqualFold(strings.TrimSpace(o.Name), strings.TrimSpace(signalID)) {
			continue
		}
		if o.TerminalOutcome() == nt.OrderOutcomeUnfilled {
			return true
		}
	}
	return false
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

// ── CANCEL-REPORT REGIME (2026-10-03, knob default OFF) ──────────────────────
//
// The snapshot-based wave (2026-09-06) proves a cancel by the order's ABSENCE
// from a fresh book — and the AddOn's snapshot deliberately omits terminal
// orders (C# D3, 2026-09-07), so absence cannot distinguish "cancelled" from
// "never answered". For a mode that cancels and re-places constantly, the
// proof must be POSITIVE: the AddOn reports the order Cancelled (or Filled,
// which is the filled path's word) for that order id. This regime makes the
// positive report the ONLY settlement evidence. It is a knob, default OFF —
// with it OFF the bot is byte-identical to today.

// cancelConfirmRequireReport resolves the regime (L4 knob, default OFF).
// P2-2 (REVIEW-312 r3): ONE parser — this delegates to the provider's
// CancelReportRegimeOn so the knob has exactly one reader and cannot drift
// between packages.
func cancelConfirmRequireReport() bool {
	return nt.CancelReportRegimeOn()
}

// cancelReportBuildIDFor is the build-id seam the report pass reads (P1-2,
// REVIEW-312 r3): the production value is the far side's proven build; tests
// override it to pin the dispatch to the report pass without a live link.
var cancelReportBuildIDFor = func(at *AutoTrader) string { return at.farSideBuildID() }

// armedReportNow is the report receipt clock — swappable in tests (A28).
var armedReportNow = func() int64 { return time.Now().UTC().UnixMilli() }

// ResetArmedReportNowForTest restores the real clock.
func ResetArmedReportNowForTest() {
	armedReportNow = func() int64 { return time.Now().UTC().UnixMilli() }
}

// cancelReportQualifies reports whether a row's recorded report proves the
// cancel. PURE: every value comes from the row (A28). F9 in one place: the
// report must exist, be 'cancelled', and postdate the cancel request. A
// 'filled' report does NOT qualify HERE — that is the filled path's outcome,
// not a cancellation.
func cancelReportQualifies(r store.ArmedOrderDB) (bool, string) {
	if r.CancelReportMs <= 0 {
		return false, "no AddOn terminal report has been recorded for this order id"
	}
	if strings.ToLower(strings.TrimSpace(r.CancelReportState)) != "cancelled" {
		return false, fmt.Sprintf("the AddOn's report for this order id is %q, not cancelled", r.CancelReportState)
	}
	if r.CancelRequestedAtMs <= 0 {
		return false, "the cancel request time is unavailable — an undated request cannot be proven"
	}
	if r.CancelReportMs < r.CancelRequestedAtMs {
		return false, fmt.Sprintf("the AddOn's report (%d) predates the cancel request (%d)", r.CancelReportMs, r.CancelRequestedAtMs)
	}
	return true, "AddOn reported cancelled for this order id after the request"
}

// slotReportBlock is the report regime's slot-guard half. PURE. A slot whose
// ledger rows hold an UNCONFIRMED cancel_pending row stays BUSY even when the
// broker book no longer lists the order — the snapshot omits terminal orders,
// so book-absence is not a terminal report, and a placement here is exactly
// the id-1664 stacking. A regime with an uncertified AddOn blocks every slot
// (fail-closed: no confirmation → no stop-entry placement). Returns false
// when the slot is not blocked.
func slotReportBlock(requireReport, floorMet bool, rows []store.ArmedOrderDB, slotSignalIDs []string) (blocked bool, why string) {
	if !requireReport {
		return false, ""
	}
	if !floorMet {
		return true, "cancel-report regime ON but the AddOn does not certify cancel reports — no stop-entry placement (fail-closed)"
	}
	sigs := map[string]bool{}
	for _, s := range slotSignalIDs {
		if s = strings.TrimSpace(s); s != "" {
			sigs[strings.ToLower(s)] = true
		}
	}
	for i := range rows {
		r := rows[i]
		if r.State != store.StateCancelPending {
			continue
		}
		if !sigs[strings.ToLower(strings.TrimSpace(r.SignalID))] {
			continue
		}
		if ok, whyQ := cancelReportQualifies(r); !ok {
			return true, "slot busy: " + whyQ + " (signal " + shortID(r.SignalID) + ")"
		}
	}
	return false, ""
}

// cancelCensusLine renders the owner-visible census of unconfirmed cancels:
// every id named (sample-id law), with its signal, age and attempts — one
// WARN per pass, not one per row. READ, never inferred: the values come from
// the rows, and a row with no request time reads n/a rather than a zero age.
func cancelCensusLine(rows []store.ArmedOrderDB, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🧾 cancel CENSUS: %d cancel(s) unconfirmed past the %s timeout — NOT promoted, NOT re-armed, slots stay busy:",
		len(rows), cancelConfirmTimeout())
	for i := range rows {
		r := rows[i]
		age := "n/a"
		if r.CancelRequestedAtMs > 0 {
			age = time.Duration(now.UnixMilli() - r.CancelRequestedAtMs).Round(time.Second).String()
		}
		fmt.Fprintf(&b, " [id=%d signal=%s age=%s attempts=%d]", r.ID, shortID(r.SignalID), age, r.CancelAttempts)
	}
	return b.String()
}

// liveBook returns the freshest book this process can see for the slot guard,
// preferring the in-memory cache the cutover gate reads.
func (at *AutoTrader) liveBook(now time.Time) (orders []nt.NT8Order, haveBook bool, age time.Duration) {
	cache, account, _ := at.brokerBook()
	if cache == nil {
		return nil, false, 0
	}
	snap, receivedAt, ok := cache.LatestReceived(account)
	if !ok || receivedAt.IsZero() {
		return nil, false, 0
	}
	return snap.Orders, true, now.Sub(receivedAt)
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
	if book == nil || row.ReceivedMs <= 0 {
		// A nil book is not "the order is gone" — it is "we cannot read what the
		// broker held", and a snapshot with no receipt time is undated evidence
		// (F9, port of #117 efcb13c9): settling a cancel on either is guessing.
		return nil, false, 0, row.ID
	}
	age = time.Duration(now.UnixMilli()-row.ReceivedMs) * time.Millisecond
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
	// ── CANCEL-REPORT REGIME (2026-10-03, knob default OFF) ──
	// Runs BEFORE the book: a slot whose cancel is unconfirmed stays busy even
	// when the book no longer lists the order, because the snapshot omits
	// terminal orders — absence from a book is not a terminal report, and a
	// placement here is the id-1664 stacking again.
	if cancelConfirmRequireReport() {
		if blocked, why := slotReportBlock(true, nt.CancelReportProven(at.farSideBuildID()), ledgerRows, sigs); blocked {
			return slotVerdict{Action: slotUnverifiable, Why: why, SignalID: shortID(r.SignalID), ReportRegime: true}
		}
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
		if v.ReportRegime {
			// The cancel-report regime's own refusal: an unconfirmed cancel or
			// an uncertified AddOn. Counted and WARNed apart from a book
			// outage, and it never raises the book-outage P0.
			class = "slot_cancel_unconfirmed"
		} else {
			// Owner ruling 2026-09-06: a dark AddOn must read as an OUTAGE, not
			// as a quiet no-trade day. Once per outage, with the book age.
			at.raiseBookOutageAlert(v.BookAge, now)
		}
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

// forgetPendingEntry drops the broker-side queued-entry marker for a signal
// whose row has just SETTLED CANCELLED (confirmed gone from the book). This is
// the N7 hook: the one-entry latch's queued_entry / recent_send refusals read
// the TCPTrader's pending map, which a cancel never clears on its own — only a
// fill, a rejection, or the 45s stale sweep do. Forgetting it here lets the
// entry that follows a confirmed cancel place instead of being refused and
// lost. Non-NT traders are a no-op.
func (at *AutoTrader) forgetPendingEntry(signalID string) {
	if signalID == "" {
		return
	}
	if ntTCP, ok := at.trader.(*ntTrader.TCPTrader); ok {
		ntTCP.ForgetPending(signalID)
	}
}

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

	// ── CANCEL-REPORT REGIME (2026-10-03, knob default OFF) ──
	// With the knob ON a cancel settles ONLY on a positive per-order terminal
	// report. The caller holds cancelConfirmMu, so the helper must not lock.
	if cancelConfirmRequireReport() {
		return at.confirmPendingCancelsReport(ledger, cancelFn, rows, now, timeout, cap, cancelReportBuildIDFor(at))
	}

	for i := range rows {
		r := rows[i]
		ok, why := cancelSettled(book, have, age, maxAge, r.SignalID)
		if ok && (r.CancelRequestedAtMs <= 0 || now.Add(-age).UnixMilli() < r.CancelRequestedAtMs) {
			// F9: a snapshot persisted BEFORE the cancel request cannot prove the
			// cancel; an undated request cannot be proven either.
			ok, why = false, "broker snapshot predates the cancel request or request time is unavailable"
		}
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
				if cancelSeenInBook(book, r.SignalID) {
					at.forgetPendingEntry(r.SignalID) // N7 part 5: positive cancel only
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
			if cancelSeenInBook(book, r.SignalID) {
				at.forgetPendingEntry(r.SignalID) // N7 part 5: positive cancel only
			}
			// P3 (rel9): a row still cancel_pending when the side went flat was
			// unregistered WITHOUT its split-record forget — forget it here, now
			// that it settles TERMINAL.
			at.mentorForgetSplitMaps(r.SignalID)
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
		attempts := r.CancelAttempts
		if r.CancelAttemptsBoot != store.ProcessBootID() {
			// Attempts from an earlier process are not THIS process's re-requests;
			// RequestCancel records the first attempt of this boot below.
			attempts = 0
		}
		if attempts >= cap {
			at.logWarnf("🧾 cancel UNCONFIRMED %s signal=%s after %s and %d attempt(s) — attempt cap reached, NOT re-requesting and NOT promoting to cancelled (%s)",
				r.Scenario, shortID(r.SignalID), reqAge.Round(time.Second), attempts, why)
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
			r.Scenario, shortID(r.SignalID), reqAge.Round(time.Second), why, attempts+1, cap, store.StateCancelPending)
	}
	return settled, stillPending, reRequested
}

// confirmPendingCancelsReport is the report regime's settlement pass. It runs
// instead of the snapshot-based pass when CANCEL_CONFIRM_REQUIRE_REPORT is ON,
// and the caller (confirmPendingCancels) already holds cancelConfirmMu.
//
// RULES, in dispatch order:
//   - an uncertified AddOn (below MinAddonBuildCancelReport) confirms NOTHING:
//     every row stays cancel_pending, no promotion, no re-arm, fail-closed;
//   - a cancel is DONE only when the ledger holds the AddOn's positive report
//     ('cancelled') for that order id, recorded at-or-after the request (F9);
//     snapshot absence settles nothing here — the snapshot omits terminal
//     orders, so absence is not a report;
//   - until then the slot is BUSY (armSlotGuard's report block) and no
//     placement happens, never a silent re-place;
//   - a timeout leads to a CENSUS — one owner-visible WARN naming every
//     unconfirmed id — and then the capped re-request, exactly like the
//     snapshot pass; re-requests are cancels, not placements.
func (at *AutoTrader) confirmPendingCancelsReport(ledger *store.ArmedOrderStore, cancelFn func(string) error, rows []store.ArmedOrderDB, now time.Time, timeout time.Duration, cap int, buildID string) (settled, stillPending, reRequested int) {
	buildStr := buildID
	if buildStr == "" {
		buildStr = "n/a"
	}
	if !nt.CancelReportProven(buildID) {
		at.logWarnf("🧾 cancel-report regime ON but the AddOn does not certify cancel reports (build %s < %s) — %d cancel(s) CANNOT be confirmed: no promotion, no re-arm, the slot stays busy (fail-closed)",
			buildStr, nt.MinAddonBuildCancelReport, len(rows))
		return 0, len(rows), 0
	}
	// P1-2 (review 2026-10-03): loop 2 must NEVER touch a row loop 1 settled.
	// The census and the re-request walk the SAME stale `rows` slice — a row
	// just confirmed (or re-armed) would otherwise get cancelFn + RequestCancel
	// and be resurrected into cancel_pending.
	settledIDs := map[int64]bool{}
	for i := range rows {
		r := rows[i]
		ok, why := cancelReportQualifies(r)
		if !ok {
			continue // stays pending; the census below names it
		}
		if rearm, rearmWhy := at.zoneRestReArmOnConfirm(r, why); rearm {
			if err := ledger.ResetToArmedUnplaced(r.ID, rearmWhy); err != nil {
				at.logWarnf("🧾 cancel-report confirm: re-arm write failed for %s: %v", r.Scenario, err)
				continue
			}
			at.forgetPendingEntry(r.SignalID)
			settled++
			settledIDs[r.ID] = true
			at.logInfof("🧾 cancel-report CONFIRMED %s signal=%s — %s (report_ms=%d state=%s attempts=%d) — returned to armed-unplaced, re-placeable",
				r.Scenario, shortID(r.SignalID), why, r.CancelReportMs, r.CancelReportState, r.CancelAttempts)
			continue
		}
		// The ORIGINAL reason survives the confirmation — same law as the
		// snapshot pass: the reason is the only record of why the cancel was
		// requested, and the confirmation must not overwrite it.
		if err := ledger.ConfirmCancelByReport(r.ID, strings.TrimSpace(r.StateReason)+" — report: "+why); err != nil {
			at.logWarnf("🧾 cancel-report confirm failed for %s: %v", r.Scenario, err)
			continue
		}
		at.forgetPendingEntry(r.SignalID)
		// P3 (rel9): forget the split record on the TERMINAL settle — the
		// flat-unregister may have skipped it while the row was still pending.
		at.mentorForgetSplitMaps(r.SignalID)
		settled++
		settledIDs[r.ID] = true
		at.logInfof("🧾 cancel-report CONFIRMED %s signal=%s — %s (report_ms=%d state=%s attempts=%d)",
			r.Scenario, shortID(r.SignalID), why, r.CancelReportMs, r.CancelReportState, r.CancelAttempts)
	}
	// Census + re-request for every row still pending past the timeout.
	var overdue []store.ArmedOrderDB
	for i := range rows {
		r := rows[i]
		if settledIDs[r.ID] {
			continue // settled in loop 1 — never re-requested, never resurrected
		}
		if r.CancelRequestedAtMs <= 0 {
			continue
		}
		reqAge := time.Duration(now.UnixMilli()-r.CancelRequestedAtMs) * time.Millisecond
		if reqAge >= timeout {
			overdue = append(overdue, r)
		}
	}
	if len(overdue) > 0 {
		at.logWarnf("%s", cancelCensusLine(overdue, now))
		for i := range overdue {
			r := overdue[i]
			reqAge := time.Duration(now.UnixMilli()-r.CancelRequestedAtMs) * time.Millisecond
			telemetry.IncGateBlock(at.id, "cancel_unconfirmed_report")
			attempts := r.CancelAttempts
			if r.CancelAttemptsBoot != store.ProcessBootID() {
				attempts = 0
			}
			if attempts >= cap {
				at.logWarnf("🧾 cancel UNCONFIRMED %s signal=%s after %s and %d attempt(s) — attempt cap reached, NOT re-requesting and NOT promoting to cancelled (no qualifying AddOn terminal report)",
					r.Scenario, shortID(r.SignalID), reqAge.Round(time.Second), attempts)
				continue
			}
			if cancelFn == nil {
				at.logWarnf("🧾 cancel UNCONFIRMED %s signal=%s after %s — no wire to re-request on (no qualifying AddOn terminal report)",
					r.Scenario, shortID(r.SignalID), reqAge.Round(time.Second))
				continue
			}
			if cerr := cancelFn(r.SignalID); errors.Is(cerr, errCancelRefused) {
				at.logWarnf("🧾 cancel re-request REFUSED %s signal=%s after %s — not sent, not recorded (no qualifying AddOn terminal report)",
					r.Scenario, shortID(r.SignalID), reqAge.Round(time.Second))
				continue
			} else if cerr != nil {
				at.logWarnf("🧾 cancel re-request SEND FAILED %s signal=%s: %v", r.Scenario, shortID(r.SignalID), cerr)
			}
			// The re-request is recorded whether or not the SEND returned nil —
			// because the send is not the point.
			if err := ledger.RequestCancel(r.ID, "re-requested after "+reqAge.Round(time.Second).String()+" unconfirmed (report regime)", now.UnixMilli()); err != nil {
				at.logWarnf("🧾 cancel re-request: ledger write failed for %s: %v", r.Scenario, err)
				continue
			}
			reRequested++
			at.logWarnf("🧾 cancel UNCONFIRMED %s signal=%s after %s (no qualifying AddOn terminal report) — re-requested, attempt %d of %d; the row stays %s",
				r.Scenario, shortID(r.SignalID), reqAge.Round(time.Second), attempts+1, cap, store.StateCancelPending)
		}
	}
	stillPending = len(rows) - settled
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
func CancelBootLine(st *store.Store, rec ReconcileCounts, nowMs int64, buildID string) string {
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
	// The report regime is READ, never asserted: the knob is resolved from the
	// env and the floor from the far-side build; at boot there is no build yet
	// (it arrives on the first hello/heartbeat), so unproven reads "n/a" —
	// which is the truth at that instant, not a fabricated yes or no.
	regime := "report-regime=off"
	if cancelConfirmRequireReport() {
		switch {
		case buildID == "":
			regime = "report-regime=on(addon-proof=n/a — fail-closed until the hello carries a build)"
		case nt.CancelReportProven(buildID):
			regime = fmt.Sprintf("report-regime=on(addon-proof=yes build=%s)", buildID)
		default:
			regime = fmt.Sprintf("report-regime=on(addon-proof=NO build=%s < %s — fail-closed)", buildID, nt.MinAddonBuildCancelReport)
		}
	}
	return fmt.Sprintf(
		"cancels: confirm=broker-snapshot · pending=%d · unconfirmed=%d · slot-guard=on(refuse-on-live|stale) · timeout=%s · stale-bound=%s · rerequest-cap=%d budget=per-process carry=%s (inert until a cancel is re-requested after a restart) · %s · %s",
		pending, unconfirmed, cancelConfirmTimeout(), snapshotMaxAge(), cancelReRequestMax(), carryStr, reconciled, regime)
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
