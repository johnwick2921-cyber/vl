// F12 — cutover leg 4 reads the BROKER, and the override guard becomes a check.
//
// Leg 4 was the last leg of the flat gate still answered by our own
// bookkeeping. Class 33 made it read the armed_orders ledger because
// GetOpenOrders was always empty, and it said so in its own source string:
// "armed_orders ledger (no NT8 order frame — F12 open)". That is a leg that
// cannot catch the one failure a flat gate exists for — the ledger and the
// broker disagreeing.
//
// With the order_snapshot frame the broker answers, and the ledger becomes the
// CROSS-CHECK: a disagreement fails the leg with both counts quoted, because
// when two sources disagree the honest move is to show both and refuse, not to
// pick the one that lets the cutover proceed.
package trader

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	nt "nofx/provider/ninjatrader"
	"nofx/store"
)

// Leg4FromBrokerAt computes leg 4 from the broker's book, cross-checked against
// the ledger. `now` is the caller's clock (class 60/A28) and `interval` is the
// AddOn's snapshot period N; a book older than 2N is stale.
//
// Every failure path names its evidence. A gate detail that says only "failed"
// sends the reader to the logs at exactly the moment they have least time.
func Leg4FromBrokerAt(
	c *nt.OrderSnapshotCache,
	account, symbol string,
	interval time.Duration,
	ledger []OpenOrder,
	now time.Time,
) (result CutoverLeg) {
	const name = "working_orders"
	workingLedger := make([]OpenOrder, 0, len(ledger))
	armed, unconfirmed := 0, 0
	for _, row := range ledger {
		if store.IsTerminalArmState(row.ArmState) {
			continue
		}
		if store.IsUnplacedArm(row.ArmState, row.OrderID) {
			armed++
			continue
		}
		workingLedger = append(workingLedger, row)
		if strings.TrimSpace(row.OrderID) == "" || strings.ToLower(strings.TrimSpace(row.ArmState)) == store.StatePlacePending {
			unconfirmed++
		}
	}
	ledger = workingLedger
	defer func() {
		result.ArmedUnplaced = &armed
		if unconfirmed > 0 {
			result.Detail += fmt.Sprintf("; %d unconfirmed placement(s)", unconfirmed)
		}
		result.Detail += fmt.Sprintf("\n%d armed (authorized, no signal id; informational)", armed)
	}()

	// NO SILENT FALLBACK. Without a snapshot the leg FAILS and names the source
	// it fell back to, so a cutover can never read a ledger answer as a broker
	// answer. This is the state an old AddOn produces, and it is exactly the
	// state F12 exists to end.
	// THE TRANSITION CASE (F1). The Go side boots BEFORE the AddOn is reloaded,
	// so an old DLL sends no snapshots at all. Blocking every cutover until the
	// owner has reloaded NT8 would make this wave undeployable, so leg 4 falls
	// back to the ledger — but it says so in the SOURCE, every time. The rule
	// being honoured is "never a silent fallback", not "never a fallback".
	if c == nil || !c.EverReceived() {
		return CutoverLeg{N: 4, Name: name, Pass: len(ledger) == 0,
			Detail: fmt.Sprintf("%d non-terminal arm(s) — NO SNAPSHOT YET, this leg is the LEDGER's answer, not the broker's", len(ledger)),
			Source: "armed_orders ledger (no snapshot yet — old AddOn or link down; reload the AddOn to make this leg read the broker)"}
	}
	snap, ok := c.Latest(account)
	age, haveAge := c.AgeAt(account, now)
	// A book existed on this link but not for THIS account+symbol, or it
	// vanished: that is a regression, not a cold start, and it FAILS.
	if !ok || !haveAge {
		return CutoverLeg{N: 4, Name: name, Pass: false,
			Detail: fmt.Sprintf("snapshots are arriving on this link but none for %s/%s — leg cannot be evaluated", account, symbol),
			Source: "broker — NT8 order_snapshot frame (absent for this key)"}
	}
	if age > 2*interval {
		return CutoverLeg{N: 4, Name: name, Pass: false,
			Detail: fmt.Sprintf("snapshot stale (age %ds, limit %ds) — a book we have not heard about is not a flat book",
				int(age.Seconds()), int(2*interval.Seconds())),
			Source: "broker — NT8 order_snapshot frame (STALE)"}
	}

	working := snap.WorkingOrdersFor(symbol)
	// The word "broker" is load-bearing in this string: the whole point of F12
	// is that a reader can tell at a glance whether leg 4 was answered by the
	// broker or by our own ledger.
	src := fmt.Sprintf("broker — NT8 order_snapshot frame (age %ds, build %s)", int(age.Seconds()), snap.BuildID)

	// The ledger is now the cross-check, not the answer.
	ledgerIDs := make([]string, 0, len(ledger))
	for _, o := range ledger {
		ledgerIDs = append(ledgerIDs, o.OrderID)
	}
	if len(working) != len(ledger) {
		brokerIDs := make([]string, 0, len(working))
		for _, o := range working {
			brokerIDs = append(brokerIDs, o.OrderID)
		}
		return CutoverLeg{N: 4, Name: name, Pass: false,
			Detail: fmt.Sprintf("broker %d vs ledger %d — MISMATCH; broker[%s] ledger[%s]",
				len(working), len(ledger),
				strings.Join(brokerIDs, ","), strings.Join(ledgerIDs, ",")),
			Source: src + " × armed_orders ledger"}
	}

	if len(working) > 0 {
		ids := make([]string, 0, len(working))
		for _, o := range working {
			ids = append(ids, fmt.Sprintf("%s(%s %s @%.2f)", o.OrderID, o.State, o.Type, orderPrice(o)))
		}
		return CutoverLeg{N: 4, Name: name, Pass: false,
			Detail: fmt.Sprintf("%d working order(s) at the broker: %s", len(working), strings.Join(ids, " · ")),
			Source: src}
	}
	return CutoverLeg{N: 4, Name: name, Pass: true,
		Detail: fmt.Sprintf("0 working order(s) at the broker (ledger agrees: %d)", len(ledger)),
		Source: src}
}

// orderPrice is the price that matters for the order's type — a stop's trigger,
// otherwise the limit. Used only for the human-readable detail.
func orderPrice(o nt.NT8Order) float64 {
	if o.StopPrice > 0 {
		return o.StopPrice
	}
	return o.LimitPrice
}

// OverrideAllowedAt replaces the blanket rule "no override with a position
// open" with the check that rule stood in for.
//
// The rule was written after 0B (2026-09-02 07:49) waived flat with position
// 588 open: the resting stop COULD NOT be verified, because no frame carried
// the broker's book. A blanket refusal was the right answer to an unanswerable
// question. Now the question is answerable, so it gets asked: is there a
// WORKING stop for this instrument at the expected price, within tolerance, in
// a book fresh enough to believe?
//
// Stale or absent still refuses. A stale answer is not a permissive one.
func OverrideAllowedAt(
	c *nt.OrderSnapshotCache,
	account, symbol string,
	expectedStop, tolerance float64,
	interval time.Duration,
	now time.Time,
) (bool, string) {
	if c == nil {
		return false, "override refused: no order-snapshot cache — the resting stop cannot be verified"
	}
	snap, ok := c.Latest(account)
	age, haveAge := c.AgeAt(account, now)
	if !ok || !haveAge {
		return false, fmt.Sprintf("override refused: no order frame received for %s/%s — the resting stop cannot be verified (this is the 0B state)", account, symbol)
	}
	if age > 2*interval {
		return false, fmt.Sprintf("override refused: order book stale (age %ds, limit %ds) — a stale answer is not a permissive one",
			int(age.Seconds()), int(2*interval.Seconds()))
	}
	if expectedStop <= 0 {
		return false, "override refused: no expected stop price to check against"
	}

	var best *nt.NT8Order
	candidates := snap.WorkingOrdersFor(symbol)
	for i := range candidates {
		o := candidates[i]
		if !strings.Contains(strings.ToLower(o.Type), "stop") {
			continue
		}
		if best == nil || math.Abs(o.StopPrice-expectedStop) < math.Abs(best.StopPrice-expectedStop) {
			best = &candidates[i]
		}
	}
	if best == nil {
		return false, fmt.Sprintf("override refused: no working STOP order in the broker's book (age %ds, %d working order(s)) — the position is unprotected or the stop is not where we think",
			int(age.Seconds()), len(candidates))
	}
	if d := math.Abs(best.StopPrice - expectedStop); d > tolerance {
		return false, fmt.Sprintf("override refused: the resting stop is at %.2f, expected %.2f (Δ %.2f > tolerance %.2f)",
			best.StopPrice, expectedStop, d, tolerance)
	}
	return true, fmt.Sprintf("override allowed: working stop %s at %.2f matches expected %.2f (± %.2f), book age %ds",
		best.OrderID, best.StopPrice, expectedStop, tolerance, int(age.Seconds()))
}

// brokerBook reaches the broker's book through the bound NT8 trader and
// returns the account+symbol it must be read with — the TCPTrader is the only
// thing that knows both, so asking it avoids a second copy of that binding
// drifting out of step with the router's.
//
// A nil cache is handled by Leg4FromBrokerAt as a FAILING leg, never as
// "no orders".
func (at *AutoTrader) brokerBook() (*nt.OrderSnapshotCache, string, string) {
	n := at.armedTrader()
	if n == nil {
		return nil, "", ""
	}
	srv := n.GetServer()
	if srv == nil {
		return nil, "", ""
	}
	return srv.OrderSnapshots(), n.BoundAccount(), n.BarsPrimarySymbol()
}

// OrderSnapshotInterval is the AddOn's snapshot period N, RESOLVED (A11): env
// override first, else the default the AddOn also ships with. Leg 4 calls a
// book older than 2N stale.
func OrderSnapshotInterval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("NT8_ORDER_SNAPSHOT_SECS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return DefaultOrderSnapshotSecs * time.Second
}

// DefaultOrderSnapshotSecs is the ONE definition of the snapshot period. The C#
// side reads its own constant; the protocol doc names this as the contract so
// the two cannot silently disagree about what "stale" means.
const DefaultOrderSnapshotSecs = 30

// Leg4SourceLabel is the one-word answer to "who answers leg 4 right now" —
// `broker` once snapshots are arriving, `ledger` until then. It exists so the
// boot line and the gate cannot disagree: both derive it from the same cache.
func Leg4SourceLabel(c *nt.OrderSnapshotCache, account, symbol string, now time.Time) string {
	if c == nil || !c.EverReceived() {
		return "ledger (no snapshot yet)"
	}
	if _, ok := c.Latest(account); !ok {
		return "ledger (no book for " + account + ")"
	}
	if age, ok := c.AgeAt(account, now); ok && age > 2*OrderSnapshotInterval() {
		return "STALE"
	}
	return "broker"
}

// farSideBuildID is the AddOn build id as RECEIVED on the wire, "" when no
// frame has carried one. Never falls back to our own source constant.
func (at *AutoTrader) farSideBuildID() string {
	n := at.armedTrader()
	if n == nil {
		return ""
	}
	srv := n.GetServer()
	if srv == nil {
		return ""
	}
	return srv.FarSideBuildID()
}
