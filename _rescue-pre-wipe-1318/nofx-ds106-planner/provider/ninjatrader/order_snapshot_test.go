// F12 — the order_snapshot frame.
//
// Until this wave the AddOn emitted position, fill, order_update, bar and
// account frames but NO working-order snapshot, so cutover leg 4 read OUR
// ledger and called it the broker's book. The gate's own note said a leg that
// cannot be evaluated FAILS, and leg 4 was the one leg still answered by our
// own bookkeeping.
//
// These pin the parse, the cache, the age (class 60: computed …At(now), never
// from a clock read underneath), and the A10 rule that a malformed frame is
// dropped rather than fatal.

package ninjatrader

import (
	"encoding/json"
	"testing"
	"time"
)

const snapshotFixture = `{
  "account": "Sim101",
  "build_id": "2026-09-03-f12",
  "emitted_at_ms": 1788480000000,
  "reason": "state_change",
  "orders": [
    {"order_id":"NT-1","name":"VL-S1-entry","action":"sell","type":"limit",
     "limit_price":29450.25,"stop_price":0,"quantity":1,"filled":0,
     "state":"Working","symbol":"MNQ","oco":"oco-1","time_ms":1788479990000},
    {"order_id":"NT-2","name":"VL-S1-stop","action":"buy","type":"stop",
     "limit_price":0,"stop_price":29475.00,"quantity":1,"filled":0,
     "state":"Accepted","symbol":"MNQ","oco":"oco-1","time_ms":1788479990000}
  ]
}`

func TestParseOrderSnapshotReadsBothOrders(t *testing.T) {
	p, err := ParseOrderSnapshot([]byte(snapshotFixture))
	if err != nil {
		t.Fatalf("ParseOrderSnapshot: %v", err)
	}
	if p.Account != "Sim101" {
		t.Errorf("account = %q", p.Account)
	}
	// the frame is account-scoped; the SYMBOL rides each order
	if p.Orders[0].Symbol != "MNQ" {
		t.Errorf("order symbol = %q, want MNQ", p.Orders[0].Symbol)
	}
	if p.BuildID != "2026-09-03-f12" {
		t.Errorf("build_id = %q — the frame must carry the AddOn build", p.BuildID)
	}
	if len(p.Orders) != 2 {
		t.Fatalf("orders = %d, want 2", len(p.Orders))
	}
	if p.Orders[1].Type != "stop" || p.Orders[1].StopPrice != 29475.00 {
		t.Errorf("second order = %+v, want the stop at 29475", p.Orders[1])
	}
}

// An EMPTY book is an explicit empty list, never an absent frame — that
// distinction is the whole reason leg 4 can be trusted. `orders: []` must parse
// to a snapshot with zero orders and NOT to "no snapshot".
func TestParseOrderSnapshotDistinguishesEmptyFromAbsent(t *testing.T) {
	p, err := ParseOrderSnapshot([]byte(`{"account":"Sim101","build_id":"b","emitted_at_ms":1,"orders":[]}`))
	if err != nil {
		t.Fatalf("empty book must parse: %v", err)
	}
	if p.Orders == nil {
		t.Errorf("an empty book must be an empty slice, not nil — absent and empty are different claims")
	}
	if len(p.Orders) != 0 {
		t.Errorf("orders = %d, want 0", len(p.Orders))
	}
}

// A10: a malformed frame is logged and dropped. It must never panic and never
// poison the cache with a half-parsed snapshot.
func TestParseOrderSnapshotRejectsMalformedWithoutPanicking(t *testing.T) {
	for _, bad := range []string{
		`{"account":`,
		`not json at all`,
		`{"account":"Sim101","orders":"not-a-list"}`,
		``,
	} {
		if _, err := ParseOrderSnapshot([]byte(bad)); err == nil {
			t.Errorf("malformed payload %q parsed without error", bad)
		}
	}
}

// A frame with no symbol cannot be filed against an instrument. It is refused
// rather than filed under "" where it would answer for every symbol.
func TestParseOrderSnapshotRefusesAnUnaddressableFrame(t *testing.T) {
	if _, err := ParseOrderSnapshot([]byte(`{"account":"","build_id":"b","orders":[]}`)); err == nil {
		t.Errorf("a snapshot with no ACCOUNT must be refused, not filed under \"\"")
	}
}

// The account book holds every instrument; leg 4 asks about one.
func TestWorkingOrdersForFiltersBySymbol(t *testing.T) {
	p, err := ParseOrderSnapshot([]byte(`{"account":"Sim101","build_id":"b","emitted_at_ms":1,"orders":[
	  {"order_id":"1","state":"Working","type":"stop","symbol":"MNQ","quantity":1},
	  {"order_id":"2","state":"Working","type":"limit","symbol":"ES","quantity":1},
	  {"order_id":"3","state":"Filled","type":"limit","symbol":"MNQ","quantity":1}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := len(p.WorkingOrdersFor("MNQ")); got != 1 {
		t.Errorf("MNQ working = %d, want 1", got)
	}
	if got := len(p.WorkingOrdersFor("mnq")); got != 1 {
		t.Errorf("symbol match must be case-insensitive, got %d", got)
	}
	if got := len(p.WorkingOrders()); got != 2 {
		t.Errorf("whole-account working = %d, want 2", got)
	}
}

// ─────────────────────────────────────────────────────────────────────
// The cache: latest per account+symbol, with an age the CALLER's clock
// decides (class 60 / A28).
// ─────────────────────────────────────────────────────────────────────

func TestOrderSnapshotCacheKeepsLatestPerAccountSymbol(t *testing.T) {
	c := NewOrderSnapshotCache()
	t0 := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)

	first, _ := ParseOrderSnapshot([]byte(snapshotFixture))
	c.PutAt(first, t0)

	// a later snapshot for the same key replaces it
	second, _ := ParseOrderSnapshot([]byte(`{"account":"Sim101","build_id":"2026-09-03-f12","emitted_at_ms":2,"orders":[]}`))
	c.PutAt(second, t0.Add(30*time.Second))

	got, ok := c.Latest("Sim101")
	if !ok {
		t.Fatalf("no snapshot cached")
	}
	if len(got.Orders) != 0 {
		t.Errorf("latest should be the empty book, got %d orders", len(got.Orders))
	}
	// a different ACCOUNT is a different book
	if _, ok := c.Latest("SimOther"); ok {
		t.Errorf("another account must not resolve from Sim101's snapshot")
	}
}

func TestOrderSnapshotAgeUsesTheCallersClock(t *testing.T) {
	c := NewOrderSnapshotCache()
	t0 := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)
	p, _ := ParseOrderSnapshot([]byte(snapshotFixture))
	c.PutAt(p, t0)

	if age, ok := c.AgeAt("Sim101", t0.Add(45*time.Second)); !ok || age != 45*time.Second {
		t.Errorf("age = %v (ok=%v), want 45s from the caller's clock", age, ok)
	}
	if _, ok := c.AgeAt("SimOther", t0); ok {
		t.Errorf("age for an uncached key must report not-ok, never 0")
	}
}

// Working/accepted orders are what leg 4 counts; terminal ones are history and
// must not keep a gate closed forever.
func TestWorkingOrdersExcludesTerminalStates(t *testing.T) {
	p, err := ParseOrderSnapshot([]byte(`{"account":"Sim101","build_id":"b","emitted_at_ms":1,"orders":[
	  {"order_id":"1","state":"Working","type":"limit","quantity":1},
	  {"order_id":"2","state":"Accepted","type":"stop","quantity":1},
	  {"order_id":"3","state":"Filled","type":"limit","quantity":1},
	  {"order_id":"4","state":"Cancelled","type":"stop","quantity":1},
	  {"order_id":"5","state":"Rejected","type":"limit","quantity":1}
	]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	w := p.WorkingOrders()
	if len(w) != 2 {
		t.Fatalf("working = %d, want 2 (Working + Accepted); got %+v", len(w), w)
	}
}

// The envelope must round-trip: the Go side writes this type in tests and the
// AddOn writes it live, so the JSON tag set is the contract.
func TestOrderSnapshotEnvelopeRoundTrips(t *testing.T) {
	p, _ := ParseOrderSnapshot([]byte(snapshotFixture))
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	back, err := ParseOrderSnapshot(b)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if len(back.Orders) != 2 || back.BuildID != p.BuildID {
		t.Errorf("round-trip lost data: %+v", back)
	}
}
