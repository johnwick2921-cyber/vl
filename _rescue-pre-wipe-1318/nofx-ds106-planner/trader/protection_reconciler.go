package trader

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	nt "nofx/provider/ninjatrader"
)

// ── D5 — A POSITION WITHOUT PROTECTION (2026-09-07) ──────────────────────────
//
// C6 established that nothing rebuilds the bracket after a restart or a
// reconnect. The AddOn's placedBrackets is an in-memory Dictionary written in
// exactly one place — SubmitBracketOnEntryFill — and repopulated from nowhere.
// After NT8 restarts, the stop and target may still be resting at the broker
// (working orders survive and are re-synced) while OUR side has forgotten they
// exist; or they may be gone, and nothing notices.
//
// The 09-06 incident is the same hole reached by a different route: position
// 592 held no protection for 8h19m and NOTHING in this process was looking.
// Every mechanism we had watched orders it believed in. None asked the plain
// question: is there an open position, and does the broker hold a stop for it?
//
// OWNER RULING 2026-09-07: "a position without protection is the one case where
// acting beats reporting." So this raises the P0 AND places the stop.
//
// A24 GOVERNS THE UNKNOWNS, and there are three of them:
//   · no book, or a stale one            → say so, do nothing
//   · a protective order in an unreadable state → say so, do nothing
//   · protection present but not sized   → say so, do nothing
// Only "the broker holds NOTHING protective for an open position" is acted on.

type protectionAction int

const (
	protectionOK protectionAction = iota
	protectionUnknown
	protectionPlace
	protectionAlertOnly
)

func (a protectionAction) String() string {
	switch a {
	case protectionOK:
		return "ok"
	case protectionPlace:
		return "place"
	case protectionAlertOnly:
		return "alert-only"
	}
	return "unknown"
}

type protectionVerdict struct {
	Action  protectionAction
	Why     string
	StopPx  float64
	Source  string // accepted_risk | plan
	NeedQty int
	HaveQty int
}

// isProtectiveStopFor reports whether this order is a stop that would CLOSE a
// position on the given side. Two independent tells, because either can be
// absent: our own bracket naming ("<signal>-sl"), and the order's own shape (a
// stop order acting against the position).
func isProtectiveStopFor(o nt.NT8Order, side string) bool {
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(o.Name)), "-sl") {
		return true
	}
	if !strings.Contains(strings.ToLower(o.Type), "stop") {
		return false
	}
	act := strings.ToLower(strings.TrimSpace(o.Action))
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "LONG":
		return strings.Contains(act, "sell")
	case "SHORT":
		return strings.Contains(act, "buy")
	}
	return false
}

func sameSymbolLoose(a, b string) bool {
	a = strings.ToUpper(strings.TrimSpace(a))
	b = strings.ToUpper(strings.TrimSpace(b))
	if a == "" || b == "" {
		return true // the frame did not scope it; do not exclude on absence
	}
	return a == b || strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// adjudicateProtection is PURE. It answers one question: does the broker hold a
// live stop covering this open position?
func adjudicateProtection(symbol, side string, posQty int, book []nt.NT8Order, haveBook bool,
	acceptedStop *float64, planStop float64) protectionVerdict {

	if posQty <= 0 {
		return protectionVerdict{Action: protectionOK, Why: "no open position"}
	}
	if !haveBook {
		return protectionVerdict{Action: protectionUnknown, NeedQty: posQty,
			Why: "no broker book — whether this position is protected is UNKNOWN, and an unknown is not a licence to place a second stop"}
	}

	covered, liveStops, unreadable, unsized := 0, 0, 0, 0
	localOnly, dying := 0, 0
	for i := range book {
		o := book[i]
		if !sameSymbolLoose(o.Symbol, symbol) {
			continue
		}
		if !isProtectiveStopFor(o, side) {
			continue
		}
		if !o.IsStateReadable() {
			unreadable++
			continue
		}
		if !o.IsLiveAtExchange() {
			// dying, local-only (TriggerPending) or merely pending: NOT
			// protection at the exchange. Counted nowhere, claimed nowhere —
			// but NAMED, because "there is a stop object and it is not
			// protecting you" is a different thing for the owner to read than
			// "there is no stop at all".
			switch {
			case o.IsHeldLocally():
				localOnly++
			case o.IsCancelInFlight():
				dying++
			}
			continue
		}
		liveStops++
		if o.Quantity <= 0 {
			unsized++
			continue
		}
		covered += o.Quantity
	}

	if covered >= posQty {
		return protectionVerdict{Action: protectionOK, NeedQty: posQty, HaveQty: covered,
			Why: fmt.Sprintf("%d live protective stop(s) at the exchange covering %d of %d", liveStops, covered, posQty)}
	}
	if unreadable > 0 {
		return protectionVerdict{Action: protectionUnknown, NeedQty: posQty, HaveQty: covered,
			Why: fmt.Sprintf("%d protective order(s) in a state this build cannot read — treating as possibly live and taking no action", unreadable)}
	}
	if unsized > 0 {
		return protectionVerdict{Action: protectionUnknown, NeedQty: posQty, HaveQty: covered,
			Why: fmt.Sprintf("%d live protective stop(s) carry no quantity — coverage UNKNOWN, so no second stop is placed", unsized)}
	}
	if liveStops > 0 {
		// PARTIAL cover. The owner's "acting beats reporting" is about a
		// position with NO protection. Adding a second stop beside an existing
		// one, into an OCO topology we cannot see, risks a double exit — so
		// this reports loudly and leaves the decision to a human.
		return protectionVerdict{Action: protectionAlertOnly, NeedQty: posQty, HaveQty: covered,
			Why: fmt.Sprintf("PARTIALLY protected: %d live stop(s) cover %d of %d — a second stop beside an existing one could double-exit, so this is raised, not fixed", liveStops, covered, posQty)}
	}

	// NOTHING protective at the exchange. This is the case that gets acted on.
	notProtection := ""
	if localOnly > 0 || dying > 0 {
		notProtection = fmt.Sprintf(" (%d held locally by NT8 and %d being cancelled — present, but not protection at the exchange)", localOnly, dying)
	}
	if acceptedStop != nil && *acceptedStop > 0 {
		return protectionVerdict{Action: protectionPlace, StopPx: *acceptedStop, Source: "accepted_risk",
			NeedQty: posQty, Why: "NO protective order at the broker for an open position" + notProtection + " — placing the stop the broker itself accepted"}
	}
	if planStop > 0 {
		return protectionVerdict{Action: protectionPlace, StopPx: planStop, Source: "plan",
			NeedQty: posQty, Why: "NO protective order at the broker for an open position" + notProtection + ", and no accepted-risk record — placing the plan's composed stop"}
	}
	return protectionVerdict{Action: protectionAlertOnly, NeedQty: posQty,
		Why: "NO protective order at the broker for an open position" + notProtection + ", and NO price to place one at (no accepted-risk record, no composed plan stop) — raised, not invented"}
}

// ── THE WIRED HALF ───────────────────────────────────────────────────────────

// protectionUnprotected counts positions found with NO protection at the
// broker, for the boot line. A COUNTER, never an inference (checklist 35).
var protectionUnprotected sync.Map // trader id → *int64

// protectionLastAction throttles the log: a verdict repeats every minute while
// the condition lasts, and only a CHANGE is worth a line.
var protectionLastAction sync.Map // trader id + symbol + side → string

// UnprotectedFound is the boot line's figure — READ from the counter (A11).
func UnprotectedFound(traderID string) int64 {
	if v, ok := protectionUnprotected.Load(traderID); ok {
		return atomic.LoadInt64(v.(*int64))
	}
	return 0
}

func incUnprotected(traderID string) {
	v, _ := protectionUnprotected.LoadOrStore(traderID, new(int64))
	atomic.AddInt64(v.(*int64), 1)
}

// protectionPricesFor resolves the two prices D5 may place at, in the owner's
// order of preference: the stop the BROKER accepted, else the plan's composed
// stop. Both may be absent, and absence is reported, never invented (A24).
func (at *AutoTrader) protectionPricesFor(symbol, side string) (signalID string, acceptedStop *float64, planStop float64) {
	if at == nil || at.store == nil {
		return "", nil, 0
	}
	// The open position row carries the entry's signal identity.
	if ps := at.store.Position(); ps != nil {
		if rows, err := ps.GetOpenPositions(at.id); err == nil {
			for _, r := range rows {
				if r != nil && strings.EqualFold(r.Symbol, symbol) &&
					strings.EqualFold(r.Side, side) && r.EntryOrderID != "" {
					signalID = r.EntryOrderID
					break
				}
			}
		}
	}
	// The plan's composed stop, from the arm that filled into this position.
	//
	// STRICTLY BY SIGNAL ID. ArmedOrderDB carries no symbol, so with no signal
	// id there is no way to prove a filled arm belongs to THIS position — and a
	// stop taken from another instrument's plan is worse than no stop at all.
	// An unidentifiable position therefore yields no price, and the verdict
	// becomes a P0 that places nothing and says why (A24: never invent).
	if signalID != "" {
		if ao := at.store.ArmedOrders(); ao != nil {
			if rows, err := ao.ListFilled(at.id, 20); err == nil {
				for _, r := range rows {
					if r.SignalID == signalID {
						planStop = r.StopPx
						break
					}
				}
			}
		}
	}
	// The broker's OWN accepted price wins over our composition of it.
	if ar := at.store.AcceptedRisk(); ar != nil && signalID != "" {
		if rows, err := ar.ForSignal(signalID); err == nil {
			for i := len(rows) - 1; i >= 0; i-- {
				if rows[i].AcceptedStopPx != nil && *rows[i].AcceptedStopPx > 0 {
					v := *rows[i].AcceptedStopPx
					acceptedStop = &v
					break
				}
			}
		}
	}
	return signalID, acceptedStop, planStop
}

// reconcileProtectionAt is the D5 pass. It runs on the 1-minute monitor beat and
// on the reconnect edge. now is the caller's clock (A28).
//
// IT IS DELIBERATELY NOT IN maybeManageArmedOrders. That path returns early
// unless day_plan is enabled (armed_executor.go), and a position is unprotected
// regardless of day_plan. A safety check that a config flag can silence is not
// a safety check.
func (at *AutoTrader) reconcileProtectionAt(now time.Time, trigger string) {
	if at == nil || at.trader == nil || at.exchange != "ninjatrader" {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			at.logWarnf("🧷 protection reconcile panicked and was contained: %v", rec)
		}
	}()

	positions, err := at.trader.GetPositions()
	if err != nil {
		// A24: a read failure is not "flat". Say so and do nothing.
		at.logWarnf("🧷 protection UNVERIFIED (%s): positions unreadable: %v — no action taken", trigger, err)
		return
	}
	if len(positions) == 0 {
		return
	}

	book, have, age := at.liveBook(now)
	if have && snapshotMaxAge() > 0 && age > snapshotMaxAge() {
		// A book older than the bound answers about the past. The reconnect
		// edge in particular fires BEFORE any fresh snapshot: the AddOn sends
		// hello, accounts, balances and positions on connect, but no
		// order_snapshot until its next beat.
		have = false
	}

	for _, p := range positions {
		symbol, _ := p["symbol"].(string)
		side, _ := p["side"].(string)
		qtyF, _ := p["quantity"].(float64)
		qty := int(math.Round(qtyF))
		if qty <= 0 || symbol == "" || side == "" {
			continue
		}
		signalID, acceptedStop, planStop := at.protectionPricesFor(symbol, side)
		v := adjudicateProtection(symbol, side, qty, book, have, acceptedStop, planStop)
		at.actOnProtection(v, symbol, side, qty, signalID, trigger, age, now)
	}
}

func (at *AutoTrader) actOnProtection(v protectionVerdict, symbol, side string, qty int,
	signalID, trigger string, bookAge time.Duration, now time.Time) {

	key := at.id + "|" + symbol + "|" + side
	prev, _ := protectionLastAction.Load(key)
	changed := prev == nil || prev.(string) != v.Action.String()
	protectionLastAction.Store(key, v.Action.String())

	switch v.Action {
	case protectionOK:
		if changed {
			at.logInfof("🧷 protection OK (%s): %s %s ×%d — %s", trigger, side, symbol, qty, v.Why)
		}

	case protectionUnknown:
		if changed {
			at.logWarnf("🧷 protection UNVERIFIED (%s): %s %s ×%d — %s (book age %s)",
				trigger, side, symbol, qty, v.Why, bookAge.Round(time.Second))
		}

	case protectionAlertOnly:
		if !changed {
			return
		}
		incUnprotected(at.id)
		at.emitAlert("P0", "unprotected_position",
			fmt.Sprintf("unprotected:%s:%s:%d", symbol, side, now.UnixMilli()),
			"Open position is NOT protected at the broker",
			fmt.Sprintf("%s %s ×%d. %s No stop was placed. Check NinjaTrader and set a stop by hand if this persists.",
				side, symbol, qty, v.Why))
		at.logErrorf("🚨 UNPROTECTED POSITION (%s): %s %s ×%d — %s", trigger, side, symbol, qty, v.Why)

	case protectionPlace:
		incUnprotected(at.id)
		at.emitAlert("P0", "unprotected_position",
			fmt.Sprintf("unprotected:%s:%s:%d", symbol, side, now.UnixMilli()),
			"Open position had NO stop — one is being placed",
			fmt.Sprintf("%s %s ×%d had no protective order at the broker. Placing a stop at %.2f from %s. %s",
				side, symbol, qty, v.StopPx, v.Source, v.Why))
		at.logErrorf("🚨 UNPROTECTED POSITION (%s): %s %s ×%d — placing stop %.2f from %s",
			trigger, side, symbol, qty, v.StopPx, v.Source)

		placer, ok := at.trader.(interface {
			PlaceProtectiveStop(symbol, positionSide string, quantity int, stopPrice float64, signalID, reason string) error
		})
		if !ok {
			at.logErrorf("🚨 protection NOT placed: this broker cannot place a standalone protective stop — %s %s ×%d stays unprotected", side, symbol, qty)
			return
		}
		if signalID == "" {
			signalID = fmt.Sprintf("recon-%s-%d", strings.ToLower(symbol), now.UnixMilli())
		}
		if err := placer.PlaceProtectiveStop(symbol, side, qty, v.StopPx, signalID,
			"d5-reconciler:"+trigger); err != nil {
			// The build gate lives behind this error. Naming it is the point:
			// an old AddOn cannot honour the frame, and pretending otherwise
			// would record a protection that does not exist (class 81).
			at.logErrorf("🚨 protection PLACEMENT REFUSED/FAILED: %s %s ×%d stop=%.2f — %v", side, symbol, qty, v.StopPx, err)
			return
		}
		at.logWarnf("🧷 protective stop SENT: %s %s ×%d stop=%.2f source=%s signal=%s — SENT is not CONFIRMED; the next pass reads the book",
			side, symbol, qty, v.StopPx, v.Source, shortID(signalID))
	}
}

// ── D7 — THE BOOT LINE ───────────────────────────────────────────────────────
//
// Every field is READ (A11). Three of them describe what the C# AddOn does, and
// this process cannot know those from its own source — so they are read from the
// broker's book, and print n/a until a book carrying the shape arrives. A boot
// line that asserted "entry-oco=own" from a Go constant would be claiming the
// AddOn's behaviour on the strength of a comment.
func BracketsBootLine(book []nt.NT8Order, haveBook bool, addonBuild string, unprotected int64) string {
	entryOCO, bracketOCO, tif := "n/a (no book yet)", "n/a (no book yet)", "n/a (no book yet)"
	if haveBook {
		entryOCO, bracketOCO, tif = "n/a (no entry seen)", "n/a (no bracket seen)", "n/a (no protective order seen)"
		var childOCO string
		childOCOConsistent := true
		for i := range book {
			o := book[i]
			n := strings.ToLower(strings.TrimSpace(o.Name))
			isChild := strings.HasSuffix(n, "-sl") || strings.HasSuffix(n, "-tp")
			if isChild {
				if o.TimeInForce != "" {
					tif = o.TimeInForce
				}
				if childOCO == "" {
					childOCO = o.OCO
				} else if childOCO != o.OCO {
					childOCOConsistent = false
				}
				continue
			}
			// an entry: the order named after the signal itself
			if o.OCO == "" {
				entryOCO = "own(none)"
			} else {
				entryOCO = "SHARED(" + o.OCO + ")"
			}
		}
		if childOCO != "" {
			switch {
			case !childOCOConsistent:
				bracketOCO = "MIXED"
			default:
				bracketOCO = "on-fill(shared)"
			}
		}
	}
	source := "none (no book)"
	if haveBook {
		source = "broker"
	}
	canPlace := "no (addon " + nt.BuildIDForLog(addonBuild) + " < " + nt.MinAddonBuildProtectiveStop + ")"
	if nt.FarSideProven(addonBuild, nt.MinAddonBuildProtectiveStop) {
		canPlace = "yes"
	}
	return fmt.Sprintf(
		"brackets: entry-oco=%s · bracket-oco=%s · state-source=%s · protective-tif=%s · reconcile-on-reconnect=on · can-place-stop=%s · unprotected-found=%d",
		entryOCO, bracketOCO, source, tif, canPlace, unprotected)
}
