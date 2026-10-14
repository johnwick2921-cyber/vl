package trader

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vl/kernel"
	"vl/market"
	nt "vl/provider/ninjatrader"
	"vl/store"
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
	protectionFlatten
)

func (a protectionAction) String() string {
	switch a {
	case protectionOK:
		return "ok"
	case protectionPlace:
		return "place"
	case protectionAlertOnly:
		return "alert-only"
	case protectionFlatten:
		return "flatten"
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

// isProtectiveStopFor requires a stop order acting against the position.
// A bracket suffix identifies intent, but cannot override contradictory type
// or action. Missing shape is handled as unknown by adjudicateProtection.
func isProtectiveStopFor(o nt.NT8Order, side string) bool {
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

// protectionMentorStopEnabled — PROTECTION_MENTOR_STOP (default ON, REL17-F4
// safety fix). "0"/"false"/"off"/"no" disables the mentor-driver stop override
// and the through-price flatten; everything else (or unset) ships the fix.
func protectionMentorStopEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("PROTECTION_MENTOR_STOP")))
	return v != "0" && v != "false" && v != "off" && v != "no"
}

// mentorDriverCurrentStop returns the tightest CURRENT resting stop of the
// registered mentor live position for (symbol, side), and whether one exists.
// "Tightest" = the live leg stop closest to price in the protective direction:
// for a long the HIGHEST leg stop, for a short the LOWEST leg stop. Legs with no
// quantity do not carry a stop. It reads the registry under mentorExitMu (via
// mentorLivePosList) so it never races the exit-drive loop's write-back.
func (at *AutoTrader) mentorDriverCurrentStop(symbol, side string) (float64, bool) {
	if at == nil || !protectionMentorStopEnabled() {
		return 0, false
	}
	best := 0.0
	found := false
	long := strings.EqualFold(side, "LONG")
	for _, lp := range at.mentorLivePosList() {
		if lp == nil || lp.Pos.Contracts <= 0 {
			continue
		}
		if !strings.EqualFold(lp.Pos.Symbol, symbol) || !strings.EqualFold(lp.Pos.Side, side) {
			continue
		}
		for _, leg := range lp.Legs {
			if leg.Qty <= 0 || leg.Stop <= 0 {
				continue
			}
			if !found {
				best, found = leg.Stop, true
				continue
			}
			if long {
				if leg.Stop > best {
					best = leg.Stop
				}
			} else {
				if leg.Stop < best {
					best = leg.Stop
				}
			}
		}
	}
	return best, found
}

// protectionFreshPrice returns the newest 1m bar CLOSE as the "last price" for
// the F4 through-check, but ONLY when it is FRESH — the same staleness rule the
// mentor stale-data block uses (kernel.StaleEntryGateFeed, ~75 s, CME-open/halt
// aware). No provider / no 1m bars / a stale feed → (0, false): the protective
// side cannot be judged, so the caller must NOT flatten.
//
// P1-b (REL17): StaleEntryGateFeed fail-opens (returns "not stale") whenever the
// CME is closed — during the 16:00–17:00 halt or the weekend an hours-old close
// would read as fresh. So the CME-open check is made FIRST: while the CME is
// closed there is no fresh price, full stop, and the caller places (never
// flattens) — a SWING4H held across the break is never flattened on a stale close.
func protectionFreshPrice(symbol string, now time.Time) (float64, bool) {
	if !kernel.IsCMEOpen(now) {
		return 0, false
	}
	if market.FuturesBarsProvider == nil {
		return 0, false
	}
	bars := market.FuturesBarsProvider(symbol, "1m", 1)
	if len(bars) == 0 {
		return 0, false
	}
	newest := bars[len(bars)-1]
	// FeedStaleAt = the B4 wall rule OR the monotonic receipt age (release
	// #22): a backward wall step can no longer make a dead feed's last close
	// read as fresh. The clock-step entry hold is NOT consulted here —
	// protection is never frozen by a clock step.
	if stale, _ := kernel.FeedStaleAt(symbol, newest.OpenTime, now); stale {
		return 0, false
	}
	return newest.Close, true
}

// adjudicateProtection is PURE. It answers one question: does the broker hold a
// live stop covering this open position? When the position is a registered
// mentor live position, the price to place is the driver's CURRENT stop (the
// tightest live leg stop) instead of the accepted-risk / plan stop, and a stop
// already through the FRESH last price flattens rather than places.
func adjudicateProtection(symbol, side string, posQty int, book []nt.NT8Order, haveBook bool,
	acceptedStop *float64, planStop float64, mentorStop *float64, freshPrice *float64) protectionVerdict {
	return adjudicateProtectionWith(symbol, side, posQty, book, haveBook, acceptedStop, planStop, mentorStop, nil, freshPrice)
}

// knownBrokerStop is the TIGHTEST stop the broker is known to have held for
// the position's own signal (P2-1): a stop this process MOVED (auto-BE / trail,
// TCPTrader.KnownStopForSignal) or the newest "<signal>-sl" price in the
// persisted F12 book history. From names which, for the log and the alert.
type knownBrokerStop struct {
	Px   float64
	From string
}

// stopTighter reports whether a is TIGHTER than b for the position side: a
// long's stop higher, a short's stop lower (closer to price, less risk).
func stopTighter(side string, a, b float64) bool {
	if strings.EqualFold(strings.TrimSpace(side), "SHORT") {
		return a < b
	}
	return a > b
}

// stopThrough reports whether a FRESH last price is already through the stop:
// the stop would have filled, so placing it would put it behind the market.
func stopThrough(side string, price, stop float64) bool {
	if strings.EqualFold(strings.TrimSpace(side), "LONG") {
		return price <= stop
	}
	return price >= stop
}

// adjudicateProtectionWith is adjudicateProtection plus P2-1's known broker
// stop. PURE. The price order for a non-mentor position is: the tightest stop
// the broker is KNOWN to have held (never placed wider than it — a BE move must
// never be undone by the net), else the accepted-risk stop, else the plan's.
func adjudicateProtectionWith(symbol, side string, posQty int, book []nt.NT8Order, haveBook bool,
	acceptedStop *float64, planStop float64, mentorStop *float64, brokerStop *knownBrokerStop, freshPrice *float64) protectionVerdict {

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
		// F12 (port of #117 52134afc): a "-sl" NAME is intent, not coverage. A
		// named order whose shape is unreadable, and that is live (or whose state
		// cannot be read), is POSSIBLE protection — neither coverage nor absence
		// is proved.
		namedStop := strings.HasSuffix(strings.ToLower(strings.TrimSpace(o.Name)), "-sl")
		if namedStop && (strings.TrimSpace(o.Type) == "" || strings.TrimSpace(o.Action) == "") &&
			(o.IsLiveAtExchange() || !o.IsStateReadable()) {
			unreadable++
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
	// REL17-F4: a registered mentor live position places at the driver's CURRENT
	// stop, never the original. If a FRESH last price is already through that
	// stop, the stop would have filled — flatten at market instead of placing a
	// stop that is already behind the market. The only price that may flatten is
	// the newest 1m BarCache close that passes the mentor stale-data freshness
	// rule (~75 s); markPrice / the entry fallback / a stale close must NOT
	// flatten (a BE stop equals entry, so the entry fallback would false-flatten
	// a healthy position right after a restart or a feed drop).
	if mentorStop != nil && *mentorStop > 0 {
		if freshPrice != nil && *freshPrice > 0 {
			long := strings.EqualFold(side, "LONG")
			through := (long && *freshPrice <= *mentorStop) || (!long && *freshPrice >= *mentorStop)
			if through {
				return protectionVerdict{Action: protectionFlatten, StopPx: *mentorStop,
					NeedQty: posQty,
					Why:     fmt.Sprintf("NO protective order at the broker and the mentor driver's stop %.2f is already through the fresh last price %.2f — the stop would have filled, so the position is flattened", *mentorStop, *freshPrice)}
			}
			return protectionVerdict{Action: protectionPlace, StopPx: *mentorStop, Source: "mentor_driver",
				NeedQty: posQty, Why: "NO protective order at the broker for an open mentor position" + notProtection + " — placing the driver's CURRENT (moved BE/1:1/trail) stop, not the original"}
		}
		return protectionVerdict{Action: protectionPlace, StopPx: *mentorStop, Source: "mentor_driver",
			NeedQty: posQty, Why: "NO protective order at the broker for an open mentor position" + notProtection + " — no fresh last price, so the protective side could NOT be checked; placing the driver's CURRENT (moved BE/1:1/trail) stop anyway, not the original"}
	}
	// P2-1 (release #22 rescan): a stop the broker is KNOWN to have held for
	// THIS position's signal — moved by auto-BE / a trail, or read from the
	// book history — is the floor. The plan stop (armed_orders.stop_px) and
	// the accepted-risk stop are the ORIGINAL; placing either after a BE move
	// re-opens the risk the move removed (654: plan 30551.25 vs BE 30456.50 =
	// 94.75 pts, $189.50). The B1 widen ban guards move_stop only, so this is
	// where the net must refuse to widen.
	if brokerStop != nil && brokerStop.Px > 0 {
		px, src := brokerStop.Px, "broker_stop("+brokerStop.From+")"
		if acceptedStop != nil && *acceptedStop > 0 && stopTighter(side, *acceptedStop, px) {
			px, src = *acceptedStop, "accepted_risk"
		}
		if freshPrice != nil && *freshPrice > 0 && stopThrough(side, *freshPrice, px) {
			return protectionVerdict{Action: protectionFlatten, StopPx: px, Source: src,
				NeedQty: posQty,
				Why:     fmt.Sprintf("NO protective order at the broker and the tightest known stop %.2f (%s) is already through the fresh last price %.2f — the stop would have filled, so the position is flattened", px, src, *freshPrice)}
		}
		checked := " — no fresh last price, so the protective side could NOT be checked"
		if freshPrice != nil && *freshPrice > 0 {
			checked = fmt.Sprintf(" — fresh last price %.2f is on the protective side", *freshPrice)
		}
		planNote := ""
		if planStop > 0 && math.Abs(planStop-px) > 1e-9 {
			planNote = fmt.Sprintf(", NOT the plan's original %.2f", planStop)
		}
		return protectionVerdict{Action: protectionPlace, StopPx: px, Source: src,
			NeedQty: posQty, Why: "NO protective order at the broker for an open position" + notProtection +
				fmt.Sprintf(" — placing the TIGHTEST stop the broker is known to have held (%.2f from %s)%s%s", px, src, planNote, checked)}
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

// protectionFlattenRetries counts the per-(trader,symbol,side) flatten close
// re-sends, so a failing flatten logs its retry count instead of a new P0 each
// pass (REL17-F4 P2: the alert is deduped, the close retry is not).
var protectionFlattenRetries sync.Map // trader id + symbol + side → int64

// protectionFlattenEpisode records the per-(trader,symbol,side) episode start
// (Unix ms) on the transition INTO flatten, so the P0 event id can carry it
// (P1-a: a STABLE id would suppress every later episode forever). Cleared when
// the verdict leaves flatten.
var protectionFlattenEpisode sync.Map // trader id + symbol + side → int64

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

// protectionHistoryLookback bounds the book-history scan for a signal's last
// "-sl" price. A swing held across a weekend is still inside it.
const protectionHistoryLookback = 14 * 24 * time.Hour

// knownBrokerStopFor (P2-1) returns the TIGHTEST stop the broker is known to
// have held for signalID: the stop this process last MOVED it to (auto-BE or a
// trail, recorded per signal by the TCPTrader after each sent move) and the
// newest "<signal>-sl"/"-sl2" price in the persisted F12 book history (which
// survives a restart, and still holds the price after the live book lost the
// order). Both absent → ok=false and the caller keeps the accepted/plan stop.
func (at *AutoTrader) knownBrokerStopFor(signalID, side string, now time.Time) (knownBrokerStop, bool) {
	var out knownBrokerStop
	if at == nil || strings.TrimSpace(signalID) == "" {
		return out, false
	}
	found := false
	consider := func(px float64, from string) {
		if px <= 0 {
			return
		}
		if !found || stopTighter(side, px, out.Px) {
			out, found = knownBrokerStop{Px: px, From: from}, true
		}
	}
	if k, ok := at.trader.(interface {
		KnownStopForSignal(signalID, side string) (float64, bool)
	}); ok {
		if px, ok := k.KnownStopForSignal(signalID, side); ok {
			consider(px, "moved this process")
		}
	}
	if at.store != nil {
		if _, account, _ := at.brokerBook(); account != "" {
			row, err := at.store.NT8OrderSnapshots().LatestWithOrderNamed(account, signalID+"-sl",
				now.Add(-protectionHistoryLookback).UnixMilli())
			if err != nil {
				at.logWarnf("🧷 protection: book-history read for signal %s failed — the moved stop (if any) still applies: %v", shortID(signalID), err)
			} else if row != nil {
				var orders []nt.NT8Order
				if jerr := json.Unmarshal([]byte(row.OrdersJSON), &orders); jerr == nil {
					for _, o := range orders {
						if o.Name == signalID+"-sl" || o.Name == signalID+"-sl2" {
							consider(o.StopPrice, fmt.Sprintf("book snapshot %d", row.ID))
						}
					}
				}
			}
		}
	}
	return out, found
}

// sweepStaleProtectionState deletes this trader's per-(symbol, side) protection
// state — protectionLastAction, protectionFlattenEpisode, protectionFlattenRetries —
// for every key whose (symbol, side) is NOT in the positions just read. It runs
// only on a successful positions read, so a read error never forgets state (A24).
func (at *AutoTrader) sweepStaleProtectionState(positions []map[string]interface{}) {
	live := make(map[string]bool)
	for _, p := range positions {
		symbol, _ := p["symbol"].(string)
		side, _ := p["side"].(string)
		if symbol == "" || side == "" {
			continue
		}
		live[symbol+"|"+side] = true
	}
	prefix := at.id + "|"
	sweep := func(m *sync.Map) {
		m.Range(func(k, _ any) bool {
			key, ok := k.(string)
			if !ok || !strings.HasPrefix(key, prefix) {
				return true
			}
			rest := strings.TrimPrefix(key, prefix) // symbol|side
			if !live[rest] {
				m.Delete(k)
			}
			return true
		})
	}
	sweep(&protectionLastAction)
	sweep(&protectionFlattenEpisode)
	sweep(&protectionFlattenRetries)
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
	// P1-c (REL17-F4): after a SUCCESSFUL positions read (including the empty
	// case), drop this trader's protection state for every (symbol, side) that
	// is no longer held. Without this, a side that flattened stays "flatten" in
	// protectionLastAction forever — the position closed, so no later pass ever
	// visits the key — and the NEXT naked-and-through episode on that side reads
	// changed=false and flattens SILENTLY (no P0, no episode stamp). A read error
	// changes nothing (A24: an unreadable book is not a licence to forget state).
	at.sweepStaleProtectionState(positions)
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
		// REL17-F4: the registered mentor live position's CURRENT driver stop
		// wins over the accepted-risk / plan stop when a stop must be placed.
		mentorStop, hasMentorStop := at.mentorDriverCurrentStop(symbol, side)
		var mentorStopP *float64
		if hasMentorStop {
			mentorStopP = &mentorStop
		}
		// The through-check may only use a FRESH 1m close (never markPrice, never
		// the entry fallback). nil = no fresh price → the caller must not flatten.
		var freshPriceP *float64
		if fp, ok := protectionFreshPrice(symbol, now); ok {
			freshPriceP = &fp
		}
		v := adjudicateProtection(symbol, side, qty, book, have, acceptedStop, planStop, mentorStopP, freshPriceP)
		// P2-1: only a verdict that would PLACE a non-mentor stop asks for the
		// tightest stop the broker is known to have held (the book-history read
		// is a database scan; a protected position never pays for it).
		if (v.Action == protectionPlace || (v.Action == protectionAlertOnly && v.HaveQty == 0)) && v.Source != "mentor_driver" && !hasMentorStop {
			if ks, ok := at.knownBrokerStopFor(signalID, side, now); ok {
				v = adjudicateProtectionWith(symbol, side, qty, book, have, acceptedStop, planStop, mentorStopP, &ks, freshPriceP)
			}
		}
		at.actOnProtection(v, symbol, side, qty, signalID, trigger, age, now)
	}
}

func (at *AutoTrader) actOnProtection(v protectionVerdict, symbol, side string, qty int,
	signalID, trigger string, bookAge time.Duration, now time.Time) {

	key := at.id + "|" + symbol + "|" + side
	prev, _ := protectionLastAction.Load(key)
	prevAction := ""
	if prev != nil {
		prevAction = prev.(string)
	}
	changed := prevAction != v.Action.String()
	protectionLastAction.Store(key, v.Action.String())

	// P1-a (REL17): the flatten episode is bounded by the verdict LEAVING
	// flatten. A later flatten is a NEW episode and must raise a NEW P0 (its
	// event id carries the new episode start). Leaving clears both the episode
	// start and the retry count, so a new episode counts from 1.
	if v.Action != protectionFlatten && prevAction == "flatten" {
		protectionFlattenEpisode.Delete(key)
		protectionFlattenRetries.Delete(key)
	}

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
			// B1-B4: the refusal carries the trader id + symbol + qty + stop, and
			// incUnprotected above already counted this pass. WARN (not ERROR):
			// the P0 alert above is the human-pager; this line is the forensic
			// trail. The NEXT 1-minute pass re-attempts once the reconnect hello
			// re-proves the far-side build (FIX-P1A).
			at.logWarnf("🧷 protection PLACEMENT REFUSED: trader=%s %s %s ×%d stop=%.2f — %v",
				at.id, side, symbol, qty, v.StopPx, err)
			return
		}
		at.logWarnf("🧷 protective stop SENT: %s %s ×%d stop=%.2f source=%s signal=%s — SENT is not CONFIRMED; the next pass reads the book",
			side, symbol, qty, v.StopPx, v.Source, shortID(signalID))

	case protectionFlatten:
		incUnprotected(at.id)
		// REL17-F4 P2/P1-a: the P0 is deduped per (symbol, side) EPISODE. The
		// episode start (Unix ms) is stamped on the transition INTO flatten and
		// carries into the event id, so a later episode raises a NEW P0 while
		// repeats inside one episode stay silent. The close retry still runs
		// every pass, with its count logged; the count resets per episode.
		if changed {
			episodeStart := now.UnixMilli()
			protectionFlattenEpisode.Store(key, episodeStart)
			protectionFlattenRetries.Delete(key)
			at.emitAlert("P0", "unprotected_position",
				fmt.Sprintf("flatten:%s:%s:%d", symbol, side, episodeStart),
				"Open position was naked and the stop is already behind the market — flattening",
				fmt.Sprintf("%s %s ×%d. %s Closing the position at market now.", side, symbol, qty, v.Why))
			at.logErrorf("🧷 protection: price already through the intended stop %.2f — flattened (%s %s ×%d): %s",
				v.StopPx, side, symbol, qty, v.Why)
		}
		retries := int64(0)
		if prevN, ok := protectionFlattenRetries.Load(key); ok {
			retries = prevN.(int64)
		}
		retries++
		protectionFlattenRetries.Store(key, retries)
		if retries > 1 {
			at.logWarnf("🧷 protection flatten retry #%d: %s %s ×%d — close re-sent (P0 alert deduped, not re-raised)",
				retries, side, symbol, qty)
		}
		at.flattenPosition(&store.TraderPosition{Symbol: symbol, Side: side}, "protection-through-stop")
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
