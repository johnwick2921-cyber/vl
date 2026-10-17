// F12 — the order_snapshot frame: the BROKER's working-order book.
//
// WHY THIS EXISTS. The AddOn emitted position, fill, order_update, bar and
// account frames — everything except the one thing a cutover needs to know:
// what orders are resting at the broker RIGHT NOW. Class 33 made cutover leg 4
// read our own armed_orders ledger, and the gate's own note admitted it: "the
// armed_orders ledger (no NT8 order frame — F12 open)". A gate leg answered by
// our own bookkeeping cannot catch the case where the ledger and the broker
// disagree, which is exactly the case a flat gate exists to catch.
//
// order_update frames are per-EVENT: a Go restart loses the picture until the
// next event happens, which may be never on a quiet book. A periodic snapshot
// makes the broker's book re-derivable at any moment, and an EMPTY book arrives
// as `orders: []` — an explicit empty, never an absent frame. That distinction
// is the whole reason leg 4 can be trusted: "no orders" and "no answer" are
// different claims and must never render as the same one.
package ninjatrader

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// NT8Order is one order in the broker's book, as the AddOn sees it.
type NT8Order struct {
	OrderID    string  `json:"order_id"`
	Symbol     string  `json:"symbol,omitempty"`
	Name       string  `json:"name,omitempty"`
	Action     string  `json:"action,omitempty"` // buy | sell
	Type       string  `json:"type,omitempty"`   // limit | stop | stop_limit | market
	LimitPrice float64 `json:"limit_price,omitempty"`
	StopPrice  float64 `json:"stop_price,omitempty"`
	Quantity   int     `json:"quantity,omitempty"`
	Filled     int     `json:"filled,omitempty"`
	State      string  `json:"state,omitempty"`
	OCO        string  `json:"oco,omitempty"`
	// TimeInForce rides the book from AddOn build 2026-09-07-h1 so D6 can be
	// READ from a received frame rather than asserted (A11). An older AddOn
	// omits it and it stays "" — which renders as n/a, never as "Day".
	TimeInForce string `json:"tif,omitempty"`
	TimeMs      int64  `json:"time_ms,omitempty"`
}

// The terminal set and IsWorking now live in order_state.go: one graded
// classifier, so "still on the books" and "protecting at the exchange" stop
// being answered by the same bool. `unknown` is no longer terminal (owner
// ruling 2026-09-07) — an unreadable state is not a closed order.

// OrderSnapshotPayload is the frame body.
// The frame is ACCOUNT-scoped: NT8's Account.Orders is an account collection and
// the AddOn has no persistent per-instrument handle to scope it by. Each order
// carries its own symbol and the reader filters — which also keeps an empty book
// representable, since an account with no orders still emits orders: [].
type OrderSnapshotPayload struct {
	Account string `json:"account"`
	// BuildID is the AddOn's VL_BUILD_ID. It rides every snapshot so the running
	// DLL can be identified from a RECEIVED frame — the only proof that a
	// distributed change actually landed (class 6). A build id read from our own
	// source proves nothing about what NT8 loaded.
	BuildID   string     `json:"build_id"`
	EmittedMs int64      `json:"emitted_at_ms"`
	Reason    string     `json:"reason,omitempty"` // periodic | state_change
	Orders    []NT8Order `json:"orders"`
}

// WorkingOrders is the non-terminal subset of the whole account book.
func (p OrderSnapshotPayload) WorkingOrders() []NT8Order {
	return p.WorkingOrdersFor("")
}

// WorkingOrdersFor is the non-terminal subset for one instrument — what leg 4
// counts. An empty symbol means the whole account. Matching is case-insensitive
// because NT8 reports MasterInstrument.Name and our side carries the canonical
// symbol; one canonicalizer, applied where the values meet.
func (p OrderSnapshotPayload) WorkingOrdersFor(symbol string) []NT8Order {
	want := strings.ToUpper(strings.TrimSpace(symbol))
	out := make([]NT8Order, 0, len(p.Orders))
	for _, o := range p.Orders {
		if !o.IsWorking() {
			continue
		}
		if want != "" && strings.ToUpper(strings.TrimSpace(o.Symbol)) != want {
			continue
		}
		out = append(out, o)
	}
	return out
}

// ParseOrderSnapshot decodes a frame body. A malformed frame is an ERROR the
// caller logs and drops (A10) — never a panic, and never a half-parsed snapshot
// that would poison the cache with a book that was never received.
func ParseOrderSnapshot(b []byte) (OrderSnapshotPayload, error) {
	var p OrderSnapshotPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return OrderSnapshotPayload{}, fmt.Errorf("order_snapshot: bad payload: %w", err)
	}
	// A frame with no ACCOUNT cannot be filed. Filing it under "" would let one
	// account's book answer for every account.
	if strings.TrimSpace(p.Account) == "" {
		return OrderSnapshotPayload{}, fmt.Errorf("order_snapshot: no account — unaddressable frame")
	}
	if p.Orders == nil {
		// An absent list and an empty list must not collapse into each other:
		// the AddOn sends [] for an empty book, and a nil here would later read
		// as "we never got a book".
		p.Orders = []NT8Order{}
	}
	return p, nil
}

// cachedSnapshot is a received frame plus the instant WE received it. Age is
// measured against receipt, not against the AddOn's emitted_at: the two clocks
// are different machines, and a Windows clock skew must not make a stale book
// look fresh.
type cachedSnapshot struct {
	Payload    OrderSnapshotPayload
	ReceivedAt time.Time
}

// OrderSnapshotCache holds the latest book per account+symbol.
type OrderSnapshotCache struct {
	mu    sync.RWMutex
	byKey map[string]cachedSnapshot
}

// NOTE ON build_id: this cache deliberately does NOT keep one. The server has
// owned the far-side build id since the E7 handshake (TCPServer.farSideBuild,
// exposed as FarSideBuildID) and the snapshot frame feeds THAT field rather
// than a second copy here. Two stores of the same received value are free to
// disagree, and a boot line would then have to pick one — which is how a
// "received" value quietly becomes a guess.

func NewOrderSnapshotCache() *OrderSnapshotCache {
	return &OrderSnapshotCache{byKey: map[string]cachedSnapshot{}}
}

func snapKey(account string) string {
	return strings.ToUpper(strings.TrimSpace(account))
}

// PutAt stores a snapshot. The receipt instant is passed IN (class 60 / A28):
// nothing under here reads a clock, so a test states its own and the boot line,
// the gate and the guard all agree on one instant.
func (c *OrderSnapshotCache) PutAt(p OrderSnapshotPayload, receivedAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byKey[snapKey(p.Account)] = cachedSnapshot{Payload: p, ReceivedAt: receivedAt}
}

// EverReceived reports whether ANY snapshot has arrived on this link. It is
// what separates "the AddOn has not been reloaded yet" (F1: a LOUD ledger
// fallback, so the Go boot is not blocked by the old DLL) from "we had a book
// and lost it" (a regression, which FAILS). Without the distinction the two
// render identically and the second one hides inside the first.
func (c *OrderSnapshotCache) EverReceived() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.byKey) > 0
}

// Latest returns the newest snapshot for the key.
func (c *OrderSnapshotCache) Latest(account string) (OrderSnapshotPayload, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.byKey[snapKey(account)]
	return s.Payload, ok
}

// LatestReceived returns one book and its local receipt instant under the same
// lock. Display metadata must date the frame whose values it shows.
func (c *OrderSnapshotCache) LatestReceived(account string) (OrderSnapshotPayload, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.byKey[snapKey(account)]
	return s.Payload, s.ReceivedAt, ok
}

// AgeAt is how long ago the latest snapshot for the key was RECEIVED. The
// second return is false when there is no snapshot at all — the caller must
// distinguish "no book" from "a book of age 0", which is precisely the
// plausible-zero the gate rules forbid (A24).
func (c *OrderSnapshotCache) AgeAt(account string, now time.Time) (time.Duration, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.byKey[snapKey(account)]
	if !ok {
		return 0, false
	}
	return now.Sub(s.ReceivedAt), true
}

// OrderSnapshots exposes the cache so the trader layer can read the broker's
// book for cutover leg 4 and the override guard.
func (s *TCPServer) OrderSnapshots() *OrderSnapshotCache { return s.orderSnaps }

// SetOrderSnapshotSink installs the persistence hook. Called once at wiring
// time; nil disables persistence without disabling the cache, so a store
// failure can never cost the gate its broker read.
func (s *TCPServer) SetOrderSnapshotSink(fn func(OrderSnapshotPayload)) { s.orderSnapCB = fn }

// ExpectedAddonBuild is the VL_BUILD_ID this Go binary was built alongside — the
// value in ninjascript/VLTraderTCPClient.cs at the time of the build.
//
// It is the EXPECTED side of the comparison and never the reported one. The
// whole reason the boot line exists is that our source and NT8's loaded DLL can
// differ: VL_BUILD_ID has been bumped in source repeatedly while NT8 kept
// running an older compile, and a line that read this constant as if it were
// the running build would report success for a change that never landed
// (class 6 — proof is a RECEIVED frame).
const ExpectedAddonBuild = "2026-09-23-m21"

// AddonBuildLine renders the build-id half of the boot line. `received` comes
// from TCPServer.FarSideBuildID() — a value that arrived on the wire.
func AddonBuildLine(received, expected string) string {
	got := strings.TrimSpace(received)
	if got == "" {
		// "none", never the expected value: an unknown build must not be able to
		// render as agreement (A24 — a check that cannot fail is not a check).
		return "nt8 addon: build_id=" + BuildIDForLog(received) + " expected=" + expected + " match=NO (no frame carrying a build_id received yet)"
	}
	if got == expected {
		return "nt8 addon: build_id=" + got + " expected=" + expected + " match=yes"
	}
	return "nt8 addon: build_id=" + got + " expected=" + expected +
		" match=NO (reload the AddOn (F5) or restart NT8)"
}

// BuildIDForLog renders a RECEIVED build id for a human: "none" when no frame
// has carried one, never "" — an unknown must not read as an empty datum (A24).
// One definition, so the boot line and the stop-entry refusal cannot disagree
// about what "we have not heard from the AddOn" looks like.
func BuildIDForLog(received string) string {
	if got := strings.TrimSpace(received); got != "" {
		return got
	}
	return "none"
}

// OrderSnapshotLineAt renders the snapshot half: age, working count, and which
// source cutover leg 4 will therefore use. Every field is READ from the cache;
// a missing book says "none" rather than age=0, which would read as a book
// received this instant.
func OrderSnapshotLineAt(c *OrderSnapshotCache, account, symbol string, now time.Time) string {
	if c == nil || !c.EverReceived() {
		return "order_snapshot: last=none · leg4 source=ledger (no snapshot yet)"
	}
	snap, ok := c.Latest(account)
	age, haveAge := c.AgeAt(account, now)
	if !ok || !haveAge {
		return "order_snapshot: last=none for " + account + "/" + symbol +
			" · leg4 source=ledger (snapshots arriving, but not for this key)"
	}
	return fmt.Sprintf("order_snapshot: last age=%ds orders=%d · leg4 source=broker",
		int(age.Seconds()), len(snap.WorkingOrdersFor(symbol)))
}
