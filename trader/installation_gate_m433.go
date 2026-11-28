package trader

import (
	"fmt"
	"strings"
	"time"

	"vl/kernel"
	nt "vl/provider/ninjatrader"
	"vl/store"
)

// ── m433 (release #52) — THE BROKER-QUIET FLAT RULE ─────────────────────────
//
// Owner, 2026-10-25: "Update must work in ANY scenario." The installation
// gate's per-trader legs 2 (api_positions) and 4 (working_orders) are answered
// by the broker (F12/F4): an unknown position book, or an order snapshot older
// than 2N, FAILS — correctly, because "a book we have not heard about is not a
// flat book". But when NT8's broker connection is down the AddOn sends nothing
// for the bound account, so on a closed-market Sunday those legs block an
// update for as long as the broker stays down, with nothing anyone can do.
//
// THE RULE (narrowest that is safe). A failing leg 2 or leg 4 is accepted ONLY
// when the failure is "no fresh broker answer" — never when the broker's last
// answer shows exposure — AND every one of these holds, each from its own
// source:
//
//	market     the CME session is CLOSED (kernel.IsCMEOpen, the one definition);
//	           nothing can fill or open while it is, and the broker-down state
//	           this exists for is a weekend/holiday/break state
//	db         trader_positions has no open row for the trader (cutover leg 1)
//	ledger     no placed / unconfirmed armed row (the ledger-only verdict that
//	           Leg4FromBrokerAt already computes — reused, not re-derived)
//	orders     the LAST order_snapshot the broker sent for the bound account
//	           (the in-memory book, however old, and/or the persisted
//	           nt8_order_snapshots row) carried ZERO working orders
//	positions  the LAST positions frame the broker sent for the bound account
//	           (the in-memory cache and/or the persisted nt_positions_last row)
//	           carried ZERO non-flat positions
//
// "AND/OR" above is one rule: at least ONE source must say flat, and NO source
// may say otherwise. An absent source is not a vote for flat, and two sources
// that disagree refuse. Absence of ALL evidence refuses (the state is then
// unknowable, and unknowable is not flat — F4).
//
// What this cannot pass: a known open position (leg 1, the cache or the
// persisted frame), a known working order (the book, the persisted row, the
// ledger), a LIVE book that disagrees (a fresh snapshot is never relaxed — its
// content verdict stands), an open market, or no evidence at all.
//
// It lives HERE, in the installation gate, and nowhere else: CutoverGateStatus
// (the per-trader deploy gate) is unchanged, so a deploy cutover keeps the
// strict broker-answered legs.

// installationGateNow is the clock of the rule (class 60/A28: a test states its
// own instant).
var installationGateNow = time.Now

// m433Relax judges one failed cutover leg (n = 2 or 4) for trader at. It
// returns relax=true with the evidence quoted, or relax=false with the reason
// the rule did not apply (appended to the leg's own detail so a refusal says
// why the broker-quiet rule did not rescue it).
func m433Relax(at *AutoTrader, st *store.Store, n int, legs []CutoverLeg) (relax bool, detail string) {
	defer func() {
		if r := recover(); r != nil {
			relax, detail = false, fmt.Sprintf("broker-quiet rule panicked: %v — fail-closed", r)
		}
	}()
	if n != 2 && n != 4 {
		return false, ""
	}
	now := installationGateNow()
	if kernel.IsCMEOpen(now) {
		return false, "broker-quiet rule n/a: the CME session is open"
	}
	tr := at.armedTrader()
	if tr == nil {
		return false, "broker-quiet rule n/a: not an NT8 trader"
	}
	acct := strings.TrimSpace(tr.BoundAccount())
	if acct == "" {
		return false, "broker-quiet rule n/a: no bound account"
	}
	// db: cutover leg 1 of THIS evaluation must have passed.
	dbFlat := false
	for _, l := range legs {
		if l.N == 1 {
			dbFlat = l.Pass
		}
	}
	if !dbFlat {
		return false, "broker-quiet rule n/a: the DB shows an open position (or leg 1 could not be read)"
	}
	// This leg's own failure must be "no fresh broker answer", never exposure.
	if tr.GetServer() == nil {
		return false, "broker-quiet rule n/a: no wire server"
	}
	var book *nt.OrderSnapshotCache
	var bookAcct, sym string
	book, bookAcct, sym = at.brokerBook()
	if n == 2 {
		if pos, perr := at.gatePositions(); perr == nil {
			return false, fmt.Sprintf("broker-quiet rule n/a: the position book is KNOWN (%d position(s))", len(pos))
		}
	} else if book != nil {
		if age, have := book.AgeAt(bookAcct, now); have && age <= 2*OrderSnapshotInterval() {
			return false, "broker-quiet rule n/a: the order book is LIVE — its content verdict stands"
		}
	}
	// ledger: the ledger-only verdict (nil cache forces the fallback branch).
	orders, oerr := at.gateOpenOrders()
	if oerr != nil {
		return false, fmt.Sprintf("broker-quiet rule n/a: ledger unreadable: %v", oerr)
	}
	if lv := Leg4FromBrokerAt(nil, bookAcct, sym, OrderSnapshotInterval(), orders, now); !lv.Pass {
		return false, "broker-quiet rule n/a: the armed ledger shows a placed/unconfirmed order"
	}

	// orders evidence (last broker order_snapshot)
	var ordersFlat, ordersBad []string
	if book != nil {
		if snap, rcv, ok := book.LatestReceived(bookAcct); ok {
			w := len(snap.WorkingOrders())
			if w == 0 {
				ordersFlat = append(ordersFlat, "in-memory book of "+kernel.ClockCTSeconds(rcv))
			} else {
				ordersBad = append(ordersBad, fmt.Sprintf("in-memory book has %d working order(s)", w))
			}
		}
	}
	if st != nil && st.NT8OrderSnapshots() != nil {
		row, err := st.NT8OrderSnapshots().Latest(acct, "")
		switch {
		case err != nil && !store.IsRecordNotFound(err):
			return false, "broker-quiet rule n/a: persisted order snapshot unreadable: " + err.Error()
		case err == nil && row != nil && row.WorkingCount == 0:
			ordersFlat = append(ordersFlat, "persisted snapshot of "+store.LastSeenCT(row.ReceivedMs))
		case err == nil && row != nil:
			ordersBad = append(ordersBad, fmt.Sprintf("persisted snapshot has %d working order(s)", row.WorkingCount))
		}
	}
	// positions evidence (last broker positions frame)
	var posFlat, posBad []string
	if open, rcv, entryAfter, ok := tr.LastPositionsFrame(); ok {
		switch {
		case !entryAfter.IsZero() && !rcv.After(entryAfter):
			// W117 F1: a frame received at-or-before the latest entry receipt
			// predates that entry — it is not evidence of flat.
			posBad = append(posBad, "in-memory frame predates the latest entry receipt")
		case open == 0:
			posFlat = append(posFlat, "in-memory frame of "+kernel.ClockCTSeconds(rcv))
		default:
			posBad = append(posBad, fmt.Sprintf("in-memory frame has %d open position(s)", open))
		}
	}
	if st != nil && st.NTPositionsLast() != nil {
		row, err := st.NTPositionsLast().Get(acct)
		switch {
		case err != nil:
			return false, "broker-quiet rule n/a: persisted positions frame unreadable: " + err.Error()
		case row != nil && row.OpenCount == 0:
			posFlat = append(posFlat, "persisted frame of "+store.LastSeenCT(row.ReceivedAtMs))
		case row != nil:
			posBad = append(posBad, fmt.Sprintf("persisted frame has %d open position(s)", row.OpenCount))
		}
	}
	if len(ordersBad) > 0 || len(posBad) > 0 {
		return false, "broker-quiet rule REFUSED: the broker's last answer shows exposure — " + strings.Join(append(ordersBad, posBad...), "; ")
	}
	if len(ordersFlat) == 0 {
		return false, "broker-quiet rule n/a: no last order snapshot from the broker (in memory or persisted) — the book is unknowable, not flat"
	}
	if len(posFlat) == 0 {
		return false, "broker-quiet rule n/a: no last positions frame from the broker (in memory or persisted) — the book is unknowable, not flat"
	}
	return true, fmt.Sprintf("m433 broker-quiet: CME closed, DB flat, armed ledger flat; the broker's LAST answers were flat — orders [%s], positions [%s] (no fresh frame; account %s)",
		strings.Join(ordersFlat, ", "), strings.Join(posFlat, ", "), acct)
}
