// ONE CONTRACT PER ACCOUNT — the broker-side invariant (owner ruling
// 2026-09-06, money-danger).
//
// WHY THIS EXISTS, in the words of the tape that produced it. On 2026-09-06 the
// live ASIA plan held three arms from ONE plan and the broker's book carried TWO
// of them WORKING at the same time — snapshot 7812, working_count 2: arm 106
// (S2, buy limit 29541.25) and arm 109 (S1, buy limit 29530.25), with arm 110
// armed behind them. Either fill opens a contract; both filling opens two.
//
// Every guard that existed let this through, and each was doing its job:
//
//   - entry_gate leg 7 (one_open_position, entry_gate.go) reads
//     OpenPositionSide — a FILLED position. With the account flat that string is
//     empty and the leg refuses nothing. It is a POSITION check; three resting
//     orders are not a position. `grep -n "IsWorking\|working\|OpenOrders"
//     trader/entry_gate.go` returns NOTHING: the gate never reads the book.
//   - the class-81 per-slot invariant (armSlotGuard, cancel_confirm.go) DOES
//     read the book, and correctly — but its unit is the SLOT
//     (plan+scenario+leg_index). S1-leg0, S1-leg1 and S2-leg0 are three
//     different slots, so three placements each pass their own check.
//
// A per-slot invariant answers "is this slot doubled". Nobody was asking "does
// this ACCOUNT already have a contract coming". That is the question here, and
// its unit is the ACCOUNT — not the slot, not the scenario, not the plan.
//
// The rule: before ANY placement, a FRESH book must show zero working ENTRY
// orders and the account zero open positions. A book we cannot see refuses too
// — placing is the destructive branch, and an unverifiable book is not an empty
// book (A24, and the same reasoning as class 81's slotUnverifiable).
package trader

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	nt "nofx/provider/ninjatrader"
	"nofx/store"
	"nofx/telemetry"
)

// contractAction is the adjudication. Like slotAction it is deliberately not a
// bool: "the account is clear" and "we cannot see the account" must never
// collapse into one another.
type contractAction string

const (
	// contractFree — a fresh book was read; no working entry, no open position.
	contractFree contractAction = "free"
	// contractLive — a fresh book shows a working entry, or a position is open.
	contractLive contractAction = "live"
	// contractUnverifiable — no book, or a book too old to believe.
	contractUnverifiable contractAction = "unverifiable"
)

// contractVerdict is the whole adjudication as one value.
type contractVerdict struct {
	Action         contractAction
	Why            string
	WorkingEntries int
	OpenPositions  int
	// FirstOrderID / FirstName name ONE blocking order so the refusal can be
	// checked against the book by hand (A21: a claim names what it rests on).
	FirstOrderID string
	FirstName    string
	SnapshotID   int64
	BookAge      time.Duration
}

// Allowed reports whether a placement may proceed. ONLY contractFree allows.
func (v contractVerdict) Allowed() bool { return v.Action == contractFree }

// Refusal renders the refusal with the ids a reader needs to check it.
func (v contractVerdict) Refusal() string {
	switch v.Action {
	case contractLive:
		if v.OpenPositions > 0 && v.WorkingEntries > 0 {
			return fmt.Sprintf("refused: account already committed — %d open position(s) AND %d working entry order(s) (first %s / %s, snapshot %d, book age %s); one contract per account",
				v.OpenPositions, v.WorkingEntries, shortID(v.FirstName), shortID(v.FirstOrderID), v.SnapshotID, v.BookAge.Round(time.Second))
		}
		if v.OpenPositions > 0 {
			return fmt.Sprintf("refused: account already committed — %d open position(s) (snapshot %d, book age %s); one contract per account",
				v.OpenPositions, v.SnapshotID, v.BookAge.Round(time.Second))
		}
		return fmt.Sprintf("refused: account already committed — %d working entry order(s) at the broker (first %s / %s, snapshot %d, book age %s); one contract per account",
			v.WorkingEntries, shortID(v.FirstName), shortID(v.FirstOrderID), v.SnapshotID, v.BookAge.Round(time.Second))
	case contractUnverifiable:
		return fmt.Sprintf("refused: account unverifiable — %s (an unverifiable book is not an empty book)", v.Why)
	}
	return ""
}

// isBracketChild reports whether a book order is a protective child rather than
// an entry. NT8 names bracket children "<signal>-sl" / "<signal>-tp" — the same
// join key orderBelongsToSlot uses (cancel_confirm.go). Counting a stop as an
// "entry" would make a protected position block its own management.
func isBracketChild(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return strings.HasSuffix(n, "-sl") || strings.HasSuffix(n, "-tp")
}

// adjudicateAccountContract is the whole decision as a PURE function: book in,
// verdict out, no clock and no store. Tests drive it with the shapes the tape
// actually produced (class 60/A28 — nothing under here reads a clock).
func adjudicateAccountContract(
	book []nt.NT8Order,
	haveBook bool,
	age, maxAge time.Duration,
	snapshotID int64,
	openPositions int,
) contractVerdict {
	// UNVERIFIABLE FIRST. Order matters: a stale book that happens to look
	// empty must not read as free, which is exactly the plausible-zero A24
	// forbids and the reason class 81 refuses on the same condition.
	if !haveBook {
		return contractVerdict{
			Action: contractUnverifiable,
			Why:    "no order snapshot has been received on this link",
		}
	}
	if maxAge > 0 && age > maxAge {
		return contractVerdict{
			Action:     contractUnverifiable,
			Why:        fmt.Sprintf("book age %s exceeds the %s bound", age.Round(time.Second), maxAge.Round(time.Second)),
			SnapshotID: snapshotID,
			BookAge:    age,
		}
	}

	v := contractVerdict{SnapshotID: snapshotID, BookAge: age, OpenPositions: openPositions}
	for _, o := range book {
		if !o.IsWorking() {
			continue
		}
		if isBracketChild(o.Name) {
			continue
		}
		v.WorkingEntries++
		if v.FirstOrderID == "" {
			v.FirstOrderID, v.FirstName = o.OrderID, o.Name
		}
	}

	if v.WorkingEntries > 0 || v.OpenPositions > 0 {
		v.Action = contractLive
		return v
	}
	v.Action = contractFree
	return v
}

// oneContractGuard is the production entry point: read the fresh book and the
// account's open positions, adjudicate, return the verdict.
//
// It is evaluated ONCE per placement pass and applies to BOTH wire paths (limit
// and stop). A guard on one placement path is not a guard — class 81 learned
// that the expensive way and this repo has two routes to the wire.
func (at *AutoTrader) oneContractGuard(now time.Time) contractVerdict {
	book, have, age := at.liveBook(now)
	_, _, _, snapID := at.persistedBook(now)
	return adjudicateAccountContract(book, have, age, snapshotMaxAge(), snapID, at.openPositionCount())
}

// openPositionCount counts the account's open positions. A read error is NOT a
// zero: it returns -1, which adjudicateAccountContract's caller turns into a
// refusal via the unverifiable path rather than a confident "flat".
func (at *AutoTrader) openPositionCount() int {
	if at == nil || at.trader == nil {
		return 0
	}
	ps, err := at.trader.GetPositions()
	if err != nil {
		// A24: an unreadable position list is UNKNOWN. Report it as one open
		// position so the guard refuses — the safe direction, since placing is
		// the destructive branch.
		at.logWarnf("🔒 one-contract: position read failed (%v) — treating the account as COMMITTED, not flat", err)
		return 1
	}
	n := 0
	for _, p := range ps {
		// positionAmt is the canonical size key on this interface — the same
		// one the drawdown monitor reads (auto_trader_risk.go:87). A missing or
		// non-float value yields 0 and is NOT counted: a malformed row must not
		// silently block every placement, and the book half of this guard
		// already refuses on anything the broker actually holds.
		if q, ok := p["positionAmt"].(float64); ok && q != 0 {
			n++
		}
	}
	return n
}

// refuseContract logs and counts a one-contract refusal, deduped per slot+class
// the same way refuseSlot does — a refusal that persists for many cycles is
// stated once per distinct condition, not once per cycle.
//
// inPass distinguishes the two halves of the invariant, because they mean
// different things to a reader: "the account was already committed when this
// pass began" versus "this pass itself just committed it".
func (at *AutoTrader) refuseContract(r store.ArmedOrderDB, v contractVerdict, inPass bool, what string, now time.Time) {
	class := "one_contract_live"
	reason := v.Refusal()
	if inPass {
		class = "one_contract_placed_this_pass"
		reason = "refused: another arm in this plan already reached the wire this cycle; one live entry per plan"
	} else if v.Action == contractUnverifiable {
		class = "one_contract_unverifiable"
		at.raiseBookOutageAlert(v.BookAge, now)
	}
	key := r.PlanID + ":" + strconv.Itoa(r.Version) + ":" + r.Scenario + ":leg" +
		strconv.Itoa(r.LegIndex+1) + ":onecontract"
	if armRefusalChanged(&at.armRefusalLast, key, class) {
		telemetry.IncGateBlock(at.id, "arm_"+class)
		at.logWarnf("🔒 armed %s %s REFUSED — %s", r.Scenario, what, reason)
	}
}

// cancelOtherArmsInPlan is the second half of the owner's ruling: when ONE arm
// places, every other arm in the SAME PLAN is cancelled. A plan gets one live
// entry, not one per scenario.
//
// It cancels through the SAME seam the entry-gate refusal path uses — a cancel
// is REQUESTED and settles only when a fresh broker snapshot stops showing the
// order (class 81). Nothing here writes a terminal state on a send.
func (at *AutoTrader) cancelOtherArmsInPlan(ledger *store.ArmedOrderStore, rows []store.ArmedOrderDB, placed store.ArmedOrderDB, now time.Time) {
	if at == nil || ledger == nil {
		return
	}
	ntTrader := at.armedTrader()
	for _, rr := range rows {
		if rr.TraderID != at.id || rr.PlanID != placed.PlanID {
			continue
		}
		// The row that just placed keeps its own state.
		if rr.ID == placed.ID {
			continue
		}
		if store.IsTerminalArmState(rr.State) {
			continue
		}
		// A ROW THAT CARRIES A SIGNAL ID IS AT THE BROKER, and it goes to
		// cancel_pending REGARDLESS of whether we can reach the wire right now.
		// The first draft of this function made ntTrader != nil part of the
		// same condition, so an unreachable AddOn sent the row down the
		// terminal branch below — writing 'cancelled' on an order the broker
		// still holds, which is class 81 exactly, committed by the wave that
		// exists to honour it. TestOneLiveEntryPerPlan_OthersCancelled caught
		// it; review had not.
		if rr.SignalID != "" {
			if ntTrader != nil {
				// D4 (2026-09-07) — THE SAME FILLED-ARM GUARD AS THE SEVEN IN
				// armed_executor.go. This sender was missed by the 09-06 wave:
				// each site was reviewed on its own, and this one sends the
				// identical frame. store.IsTerminalArmState above already skips a
				// row the LEDGER calls filled — but the ledger is a memory, and
				// at 23:37:02 it was a minute out of date. The book decides.
				if v := at.cancelSafetyFor(rr, now); !v.Allow {
					at.logWarnf("🛟 armed cancel REFUSED (one_live_entry): %s %s leg %d signal=%s — %s",
						rr.Session, rr.Scenario, rr.LegIndex+1, shortID(rr.SignalID), v.Why)
					continue
				}
				if cerr := ntTrader.CancelOrder(rr.SignalID); cerr != nil {
					at.logWarnf("✕ armed cancel SEND failed (one_live_entry): %s %s leg %d: %v",
						rr.Session, rr.Scenario, rr.LegIndex+1, cerr)
				}
			} else {
				at.logWarnf("✕ armed cancel UNSENDABLE (one_live_entry): %s %s leg %d — no broker link; row held cancel_pending, never promoted",
					rr.Session, rr.Scenario, rr.LegIndex+1)
			}
			_ = ledger.RequestCancel(rr.ID, "one_live_entry: "+placed.Scenario+" placed", now.UnixMilli())
			at.logWarnf("✕ armed cancel REQUESTED (one_live_entry): %s %s leg %d — %s reached the wire; pending broker confirmation",
				rr.Session, rr.Scenario, rr.LegIndex+1, placed.Scenario)
			continue
		}
		// No signal id — never placed, so there is nothing at the broker to
		// confirm against and this one may go terminal directly.
		_ = ledger.SetState(rr.ID, "cancelled", "one_live_entry: "+placed.Scenario+" placed")
		at.logWarnf("✕ armed %s leg %d cancelled — %s reached the wire (one live entry per plan)",
			rr.Scenario, rr.LegIndex+1, placed.Scenario)
	}
}
