package trader

import (
	"fmt"
	"strings"
	"time"

	"nofx/store"
)

// ── CLASS 33 (2026-09-02) — THE FIVE-LEG CUTOVER GATE ────────────────────────
// PART 3 of the audit checklist checked EXPOSURE (positions, orders) and never
// IN-FLIGHT WORK. 2026-08-31 17:34 CT a kill -9 landed while a planner chain
// was on attempt 3/3: the chain died silently — no v2, no fail-closed line,
// nothing re-claimed it. Agents have held on that by discipline four times
// since; the rite itself had no leg for it. Leg 5 is that leg.
//
// The legs are computed here, in ONE payload, so an agent cannot quote four
// and skip the fifth. Every leg that cannot be EVALUATED fails (A24: a check
// that cannot fail is not a check).

// CutoverLeg is one gate leg's verdict.
type CutoverLeg struct {
	N      int    `json:"n"`
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"`
	Source string `json:"source,omitempty"`
	// Informational authorization count; nil means the ledger was unavailable.
	ArmedUnplaced *int `json:"armed_unplaced,omitempty"`
}

// CutoverGate is the whole five-leg verdict. Ready is true only when EVERY
// leg passes — an unevaluable leg is a failed leg, never an absent one.
type CutoverGate struct {
	TraderID string       `json:"trader_id"`
	Ready    bool         `json:"ready"`
	Legs     []CutoverLeg `json:"legs"`
	Note     string       `json:"note"`
}

// AnyPlannerReadInFlight (leg 5) reports whether ANY planner read is claimed
// for THIS trader — any trade date, any session. PlannerReadInFlight answers
// for one (date, session); a cutover must ask about all of them.
func (at *AutoTrader) AnyPlannerReadInFlight() (bool, string) {
	suffix := ":" + at.id
	held := ""
	plannerReadInFlight.Range(func(k, _ any) bool {
		key, _ := k.(string)
		if strings.HasSuffix(key, suffix) {
			held = key
			return false
		}
		return true
	})
	return held != "", held
}

// CutoverGateStatus computes all five legs. Read-only.
func (at *AutoTrader) CutoverGateStatus() CutoverGate {
	g := CutoverGate{TraderID: at.id}
	add := func(n int, name string, pass bool, detail, source string) {
		g.Legs = append(g.Legs, CutoverLeg{N: n, Name: name, Pass: pass, Detail: detail, Source: source})
	}

	// Leg 1 — the DB's own record of exposure.
	if at.store == nil || at.store.Position() == nil {
		add(1, "db_open_positions", false, "position store unavailable — leg cannot be evaluated", "sqlite trader_positions")
	} else if rows, err := at.store.Position().GetOpenPositions(at.id); err != nil {
		add(1, "db_open_positions", false, fmt.Sprintf("query failed: %v", err), "sqlite trader_positions")
	} else {
		add(1, "db_open_positions", len(rows) == 0, fmt.Sprintf("%d open row(s)", len(rows)), "sqlite trader_positions")
	}

	// Leg 2 — what the API would answer (the broker's book for this trader).
	// A gate NEVER panics (A10): an absent broker link is a FAILED leg.
	pos, perr := at.gatePositions()
	if perr != nil {
		add(2, "api_positions", false, fmt.Sprintf("query failed: %v", perr), "trader.GetPositions")
	} else {
		add(2, "api_positions", len(pos) == 0, fmt.Sprintf("%d position(s)", len(pos)), "trader.GetPositions")
	}

	// Leg 3 — the NT8 snapshot for the BOUND account, quoted separately from
	// leg 2 so a per-account routing fault cannot hide behind one number.
	if nt := at.armedTrader(); nt == nil {
		add(3, "nt8_positions_snapshot", true, "not an NT8 trader — leg not applicable", "n/a")
	} else if snap, serr := nt.GetPositions(); serr != nil {
		add(3, "nt8_positions_snapshot", false, fmt.Sprintf("snapshot failed: %v", serr), "NT8 positions frame")
	} else {
		add(3, "nt8_positions_snapshot", len(snap) == 0, fmt.Sprintf("count=%d", len(snap)), "NT8 positions frame")
	}

	// Leg 4 — working orders. THE class-33 leg: this was a stub returning
	// empty, so it passed vacuously at every cutover 35 → 41.
	// The ledger is read DIRECTLY, not through the broker link: resting orders
	// exist whether or not our connection does, so a dead link must never turn
	// leg 4 into "no working orders". (The same function feeds the TCPTrader's
	// GetOpenOrders, so /api/open-orders and this leg cannot disagree.)
	//
	// F12 (2026-09-03): the BROKER now answers this leg. The ledger is the
	// cross-check, and a disagreement FAILS with both counts quoted — when two
	// sources disagree the honest move is to show both and refuse, never to
	// pick the one that lets the cutover proceed. With no snapshot (an old
	// AddOn, or the link down) the leg FAILS and names the ledger as the source
	// out loud: a silent fallback would let a ledger answer be read as a broker
	// answer, which is the whole defect F12 closes.
	orders, oerr := at.gateOpenOrders()
	switch {
	case oerr != nil:
		add(4, "working_orders", false, fmt.Sprintf("source failed: %v", oerr), "armed_orders ledger")
	default:
		book, acct, sym := at.brokerBook()
		leg := Leg4FromBrokerAt(book, acct, sym, OrderSnapshotInterval(), orders, time.Now())
		g.Legs = append(g.Legs, leg)
	}

	// Leg 5 — IN-FLIGHT WORK (the 2026-08-31 17:34 defect).
	inFlight, key := at.AnyPlannerReadInFlight()
	detail := "no planner read claimed"
	if inFlight {
		detail = "planner read IN FLIGHT: " + key
	}
	add(5, "planner_in_flight", !inFlight, detail, "plannerReadInFlight claim")

	g.Ready = true
	for _, l := range g.Legs {
		if !l.Pass {
			g.Ready = false
		}
	}
	g.Note = "leg 4 compares broker working orders with placed/unconfirmed ledger rows; armed rows without a signal are counted separately and do not fail the leg. Boot sweep counter key: " + store.BootSweptKey
	return g
}

// gatePositions / gateOpenOrders are the panic-safe wrappers the gate uses: an
// uninitialised broker link is reported as a FAILED leg, never a crash and
// never a silent pass.
func (at *AutoTrader) gatePositions() (pos []map[string]interface{}, err error) {
	if at.trader == nil {
		return nil, fmt.Errorf("broker link not initialised")
	}
	defer func() {
		if r := recover(); r != nil {
			pos, err = nil, fmt.Errorf("panic while reading positions: %v", r)
		}
	}()
	return at.GetPositions()
}

func (at *AutoTrader) gateOpenOrders() (orders []OpenOrder, err error) {
	defer func() {
		if r := recover(); r != nil {
			orders, err = nil, fmt.Errorf("panic while reading working orders: %v", r)
		}
	}()
	return at.ledgerOpenOrders(at.futuresSymbol())
}
