// Package ninjatrader — TCP framing codec for Plan 1.5.
//
// Wire format: 4-byte big-endian length prefix followed by UTF-8 JSON payload.
// Max frame size: 1 MB. Oversized frames are an error (server closes the
// connection per spec L4376, L4416).
//
// Four message types per spec L4378-4410: signal | fill | heartbeat | ack.
// SEPARATE FILE from tcp_server.go per spec file-manifest L4360.
package ninjatrader

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// FrameType enumerates the 4 wire envelope types per spec L4382.
type FrameType string

const (
	FrameSignal    FrameType = "signal"
	FrameFill      FrameType = "fill"
	FrameHeartbeat FrameType = "heartbeat"
	FrameAck       FrameType = "ack"
)

// Envelope wraps every frame on the TCP stream per spec L4381-4385.
type Envelope struct {
	Type    FrameType       `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// SignalPayload is the Go-server → C#-AddOn signal frame per spec L4387-4396.
type SignalPayload struct {
	Symbol string `json:"symbol"`
	// Account (P5.4): the NT8 sub-account this order must submit on. EMPTY =
	// legacy → the AddOn's active account (byte-identical wire via omitempty).
	// When set, the AddOn's Phase-2/3 resolve+guard+route path (already
	// shipped) submits to THIS account — per-(symbol,account) routing.
	Account    string  `json:"account,omitempty"`
	Side       string  `json:"side"` // "long" | "short"
	Quantity   int     `json:"quantity"`
	Entry      float64 `json:"entry"` // tick-rounded
	StopLoss   float64 `json:"stop_loss"`
	TakeProfit float64 `json:"take_profit"`
	// Leg1Qty / Leg1TP (REVIEW-353, wire v4): the split AT ENTRY rides the ONE
	// entry frame — leg 1 = ceil(n/2) with its own TP, leg 2 = the runner with
	// TakeProfit. omitempty → a pre-v4 AddOn sees the single-bracket frame
	// byte-identical. leg1_qty == 0 → the AddOn places ONE OCO pair as today.
	Leg1Qty   int     `json:"leg1_qty,omitempty"`
	Leg1TP    float64 `json:"leg1_tp,omitempty"`
	SignalID  string  `json:"signal_id"` // UUID
	Timestamp string  `json:"timestamp"` // RFC3339
	// A2 (G1, wire v3) — identity stamp. trader_id is the OWNING trader; seq is the
	// server's monotonic per-connection op counter. The AddOn ECHOES (trader_id,
	// account, seq) on the paired ack/fill/close/reject so Go can verify the AddOn
	// acted for the originator. omitempty → a pre-v3 wire stays byte-identical.
	TraderID string `json:"trader_id,omitempty"`
	Seq      uint64 `json:"seq,omitempty"`
	// PHASE 2 armed orders (additive, back-compat): OrderType "market" (default)
	// | "limit" + LimitPrice for RESTING limit entries. A pre-Phase-2 AddOn
	// ignores unknown fields → market, byte-identical to today.
	// E7 (entry-mechanics 2026-08-30, additive): OrderType "stop_entry" +
	// StopPrice for STOP-MARKET entries (breakout-retest fallback). A
	// pre-E7 AddOn ignores the new fields and would place a MARKET order —
	// the Go side therefore NEVER sends stop_entry frames unless the
	// STOP_ENTRY_SEAM is ON (owner-enabled, proven far-side frames first).
	OrderType  string  `json:"order_type,omitempty"`
	LimitPrice float64 `json:"limit_price,omitempty"`
	StopPrice  float64 `json:"stop_price,omitempty"`
	// StopLimit (MENTOR STOP-LIMIT, PR B 2026-10-03): when true and OrderType is
	// "stop_entry", the AddOn builds OrderType.StopLimit with
	// LimitPrice == StopPrice — the mentor rule "fills at its price or misses,
	// never a stop-MARKET" (D1.4 p1 @24:41, p2 @00:00). The N12 resting-limit
	// window is closed Go-side by a per-order expiry (armed_orders.expiry_ms,
	// authored by the evaluator's intent): an unfilled order is cancelled when
	// its expiry lapses. Go sets this only when the mentor stop-limit knob is
	// ON and the AddOn proves MinAddonBuildStopLimit; an older AddOn ignores
	// the field and would build StopMarket, which is why the knob is
	// fail-closed below the floor.
	StopLimit bool `json:"stop_limit,omitempty"`
}

// FillPayload is the C#-AddOn → Go-server fill frame per spec L4398-4406.
// P5.2 adds Symbol (the order's root, e.g. "MNQ") so multi-symbol fills are
// attributable. Empty = legacy AddOn (pre-P5.2) → consumers treat it as the
// primary trading symbol (back-compat; new field is additive JSON).
type FillPayload struct {
	Reason   string `json:"reason,omitempty"` // Optional; h1 omits rejection reasons.
	SignalID string `json:"signal_id"`
	Symbol   string `json:"symbol,omitempty"` // P5.2 — order's root symbol; empty = legacy (primary)
	// Account is the NT sub-account this fill executed on (H3 fix). The C# AddOn
	// already sends it (VLTraderTCPClient SendFillFrame ["account"]); Go now parses
	// it so fills route to the OWNING trader by (symbol,account) and a trader never
	// caches another account's fill. omitempty keeps legacy (account-less) fill
	// goldens byte-identical.
	Account       string  `json:"account,omitempty"`
	FillPrice     float64 `json:"fill_price"`
	FillTime      string  `json:"fill_time"` // RFC3339
	Side          string  `json:"side"`
	Quantity      int     `json:"quantity"`
	SlippageTicks float64 `json:"slippage_ticks"`
	Status        string  `json:"status"` // "filled" | "rejected" | "partial" | "duplicate_ignored" (AddOn seen-signal dedupe, FIX-DOUBLE-ENTRY)
	// A2 (G1, wire v3) — echoed identity from the originating signal. Go verifies
	// (trader_id, account, seq) against the pending op; a present mismatch freezes the
	// trader (A4). Empty = pre-v3 AddOn (echo absent) → tolerated in the deploy window.
	TraderID string `json:"trader_id,omitempty"`
	Seq      uint64 `json:"seq,omitempty"`
	// W117 F2 — true when a registered ordered-execution owner will apply this
	// frame on its worker (receive order). Advisory consumers skip it so the
	// fill is never double-applied. json:"-" keeps goldens byte-identical.
	OrderedOwned bool `json:"-"`
}

// P5.2 — protocol handshake. The C# AddOn sends `hello` as the FIRST frame on
// every (re)connect; the Go server replies with its own `hello`. On an explicit
// VERSION MISMATCH the server refuses the connection with a loud log (never
// silently misparse). A connection that never sends hello is a LEGACY AddOn
// (pre-P5.2): tolerated with a warning so the lockstep deploy window (new Go,
// not-yet-F5'd C#) cannot brick the bar feed.
const FrameHello FrameType = "hello"

// ProtocolVersion is the current wire protocol generation. v2 = P5.2
// (symbol-tagged fills + hello handshake). Bump ONLY with a lockstep C#+Go ship.
// v3 = A2 (G1) identity stamp + echo-verify: order/modify/cancel frames carry
// (trader_id, account, seq) and the AddOn echoes all three on ack/fill/close/reject.
// Both sides tolerate unknown fields, so a v2 peer only loses the echo check.
const ProtocolVersion = 3

// HelloPayload identifies the peer + its protocol generation.
type HelloPayload struct {
	ProtocolVersion int    `json:"protocol_version"`
	Source          string `json:"source"` // "vltrader-addon" | "vl-go"
	// BuildID (F12) is the AddOn's VL_BUILD_ID, carried on the handshake so the
	// running DLL is identifiable from the FIRST received frame rather than
	// only after a snapshot arrives. omitempty keeps the wire byte-identical
	// for an older AddOn that does not send it.
	BuildID string `json:"build_id,omitempty"`

	// W-ONE-BUTTON M2 (CTO ruling Q3) — the running AddOn's EPOCH, so a
	// verifier can bind "this connection is the new build" to evidence rather
	// than to the hand-set BuildID (M1 F4). All additive + omitempty: an older
	// AddOn sends none, and the Go reply (which never sets them) stays
	// byte-identical. nt8_pid + nt8_start_ms identify the NinjaTrader.exe
	// process; assembly_mvid is the loaded assembly's module version id (it
	// changes on every compile); source_hash is content-derived over the
	// AddOn source; activation_nonce is minted once per AddOn activation.
	NT8PID          int    `json:"nt8_pid,omitempty"`
	NT8StartMs      int64  `json:"nt8_start_ms,omitempty"`
	AssemblyMVID    string `json:"assembly_mvid,omitempty"`
	SourceHash      string `json:"source_hash,omitempty"`
	ActivationNonce string `json:"activation_nonce,omitempty"`
}

// ── W-ONE-BUTTON M2 site 7 — the installation maintenance frames ───────────
//
// maintenance (Go → AddOn) tells the AddOn the installation is held for an
// update: it must refuse NEW entry signals (never protection, brackets on
// fill, part-fill amends, flatten, cancel or modify). Sent ONLY while a hold is
// present (on accept, on change, re-sent every few seconds) plus one held:false
// release — with no hold file the wire is byte-identical to before M2.
//
// maintenance_ack (AddOn → Go) echoes held/job_id and reports what the AddOn
// can see, WITHOUT NAMES (the repo is public; the census is counts and flags
// only). The installation gate fails on: no ack for the current connection, an
// ack for another job, a census the AddOn could not enumerate (CensusError, or
// a nil list), any unsettled (transitional) connection, or any position /
// working order on ANY account (CTO ruling Q1). A connected non-SIM (live)
// connection alone does NOT fail: when every account is flat the update may
// proceed (owner ruling 2026-09-28 — the updater never disconnects anything;
// TRADING stays SIM-only, see isAccountTradeable).
const (
	FrameMaintenance    FrameType = "maintenance"
	FrameMaintenanceAck FrameType = "maintenance_ack"
)

// MaintenancePayload is the Go → AddOn hold notice.
type MaintenancePayload struct {
	Held  bool   `json:"held"`
	JobID string `json:"job_id,omitempty"`
}

// CensusConnection is one NT8 connection, by flags only.
type CensusConnection struct {
	Sim       bool `json:"sim"`       // every account on it is a SIM account (false when it has none: fail-closed)
	Connected bool `json:"connected"` // ConnectionStatus.Connected
	// Settled (M2.1): Status is Connected or Disconnected. Any other state
	// (Connecting, ConnectionLost, …) means the census cannot vouch for the
	// accounts on it; absent (an older build) reads false — fail-closed.
	Settled bool `json:"settled"`
}

// CensusAccount is one NT8 account, by flags and counts only.
type CensusAccount struct {
	Sim       bool `json:"sim"`
	Positions int  `json:"positions"` // non-flat positions
	Working   int  `json:"working"`   // non-terminal orders of ANY action (entries, exits, protection)
}

// MaintenanceAckPayload is the AddOn → Go acknowledgement + census.
type MaintenanceAckPayload struct {
	Held           bool   `json:"held"`
	JobID          string `json:"job_id,omitempty"`
	QueuedCommands int    `json:"queued_commands"`
	BuildID        string `json:"build_id,omitempty"`
	// Connections / Accounts are nil when the AddOn could not enumerate them
	// (never a fabricated empty list); CensusError says why.
	Connections []CensusConnection `json:"connections"`
	Accounts    []CensusAccount    `json:"accounts"`
	CensusError string             `json:"census_error,omitempty"`
}

// PHASE 2 armed orders — order-management frames (Go-server → C#-AddOn) +
// the order_update event frame (C#-AddOn → Go-server).
const (
	FrameCancelOrder   FrameType = "cancel_order"
	FrameModifyBracket FrameType = "modify_bracket"
	FrameOrderUpdate   FrameType = "order_update"
	// FrameOrderSnapshot (F12) is the AddOn's periodic + on-change dump of the
	// BROKER's working-order book. order_update is per-event and a Go restart
	// loses the picture until the next event; the snapshot makes the book
	// re-derivable at any moment. Payload: OrderSnapshotPayload.
	FrameOrderSnapshot FrameType = "order_snapshot"
)

// CancelOrderPayload asks the AddOn to cancel a working resting limit entry
// and/or its live bracket legs (managed Account.Cancel, idempotent).
type CancelOrderPayload struct {
	Symbol   string `json:"symbol"`
	SignalID string `json:"signal_id"`
	Account  string `json:"account,omitempty"`
	TraderID string `json:"trader_id,omitempty"`
	Seq      uint64 `json:"seq,omitempty"`
}

// ModifyBracketPayload asks the AddOn to modify the live bracket IN PLACE
// (same safe Change pattern as move_stop). NewStopLoss/NewTakeProfit ≤ 0 =
// leave that leg untouched.
type ModifyBracketPayload struct {
	Symbol        string  `json:"symbol"`
	SignalID      string  `json:"signal_id"`
	NewStopLoss   float64 `json:"new_stop_loss,omitempty"`
	NewTakeProfit float64 `json:"new_take_profit,omitempty"`
	// Leg (REVIEW-353, wire v4): 1 = leg 1 only, 2 = leg 2 only, absent/0 = all.
	Leg      int    `json:"leg,omitempty"`
	Account  string `json:"account,omitempty"`
	TraderID string `json:"trader_id,omitempty"`
	Seq      uint64 `json:"seq,omitempty"`
}

// OrderUpdatePayload is every NT8 order-state change (deduped per order name)
// — the armed engine's working/cancelled/filled visibility.
type OrderUpdatePayload struct {
	Reason    string  `json:"reason,omitempty"` // Additive Go receive support; next AddOn wave emits it.
	SignalID  string  `json:"signal_id"`
	OrderName string  `json:"order_name"`
	State     string  `json:"state"` // accepted|working|partfilled|filled|rejected|cancelled
	FillPrice float64 `json:"fill_price"`
	Quantity  int     `json:"quantity"`
	Symbol    string  `json:"symbol"`
	Account   string  `json:"account"`
	TraderID  string  `json:"trader_id,omitempty"`
	Seq       uint64  `json:"seq,omitempty"`

	// CANCEL-REPORT (2026-10-03-c1): true when this order_update is the
	// AddOn's positive per-order report emitted on cancel_order receipt
	// (the echo), as distinct from a state-change event. The report regime
	// (CANCEL_CONFIRM_REQUIRE_REPORT) settles cancels on these reports; an
	// older AddOn never sets it (and predates the floor anyway).
	CancelReport bool `json:"cancel_report,omitempty"`

	// W117 F2 — see FillPayload.OrderedOwned.
	OrderedOwned bool `json:"-"`
	// W117 F2 — whether the broker book is proven post-change at application
	// time (bookFresh / bookNotFresh / bookUnspecified, ordered_exec.go).
	BookGate int8 `json:"-"`
}

// P5.3 — subscription acks (C#-AddOn → Go-server). The AddOn confirms or// rejects each bars_subscribe/bars_unsubscribe so the Go side (and the owner
// API) sees subscription state without reading the NT8 Output window. ADDITIVE:
// a pre-P5.3 AddOn simply never sends them (state shows "pending").
const (
	FrameSubscribed     FrameType = "subscribed"
	FrameUnsubscribed   FrameType = "unsubscribed"
	FrameSubscribeError FrameType = "subscribe_error"
)

// SubscribedPayload acks a successful bars_subscribe: the root + the resolved
// front-month contract the BarsRequests were opened against.
type SubscribedPayload struct {
	Symbol           string `json:"symbol"`
	ResolvedContract string `json:"resolved_contract"`
}

// UnsubscribedPayload acks a bars_unsubscribe; Removed counts the disposed
// (symbol|timeframe) subscriptions.
type UnsubscribedPayload struct {
	Symbol  string `json:"symbol"`
	Removed int    `json:"removed"`
}

// SubscribeErrorPayload reports a FAILED bars_subscribe (instrument unresolved
// / not in NT8's DB) so a typo'd symbol fails loudly Go-side instead of dying
// silently in the NT8 Output window.
type SubscribeErrorPayload struct {
	Symbol string `json:"symbol"`
	Reason string `json:"reason"`
}

// AckPayload acknowledges a heartbeat or a specific signal_id per spec L4410.
// A2 (G1, wire v3): an order ack ALSO echoes the originating identity so Go can
// verify it (the heartbeat ack leaves them empty). SignalID names the acked order.
type AckPayload struct {
	Acks     string `json:"acks"` // "heartbeat" or "<signal_id>"
	SignalID string `json:"signal_id,omitempty"`
	TraderID string `json:"trader_id,omitempty"`
	Account  string `json:"account,omitempty"`
	Seq      uint64 `json:"seq,omitempty"`
}

// HeartbeatPayload is the client (AddOn) heartbeat body. E7 CAPABILITY
// HANDSHAKE (2026-08-30): the AddOn reports its BUILD_ID on every heartbeat;
// the Go side refuses frame types the far side hasn't proven (see
// FarSideBuildE7 / FarSideProven). Old AddOns send no build_id → "".
type HeartbeatPayload struct {
	BuildID string `json:"build_id,omitempty"`
}

// FarSideBuildE7 is the ORIGINAL stop-entry floor (2026-08-30). It proved the
// AddOn PARSED a stop_entry frame and built an OrderType.StopMarket — and that
// is ALL it proved. It did not prove the trigger reached NinjaTrader's stopPrice
// argument, and it did not: every stop entry this build family sent went out as
// `Limit price=<trigger> Stop price=0` (22 of 22 lifetime submissions, 0 fills,
// 2026-08-31 and 2026-09-04). SUPERSEDED by MinAddonBuildStopSlot, which is the
// value the gate now reads; E7 is retained as the named historical floor (it is
// referenced by tcp_server.go's farSideBuild comment) and as the negative
// fixture in the stop-entry wire pins. It gates nothing.
const FarSideBuildE7 = "2026-08-30-e7"

// MinAddonBuildStopSlot is the minimum AddOn build that proves a stop entry is
// CONSTRUCTED correctly: the trigger passed in CreateOrder's stopPrice argument
// rather than its limitPrice one (WAVE B / D1, 2026-09-05). This is the value
// PlaceStopEntry gates on.
//
// THE DATE PREFIX IS WHAT DECIDES. FarSideProven compares strings bytewise and
// build suffixes are NOT zero-padded — "2026-09-03-f9" >= "2026-09-03-f12" is
// TRUE, so an older same-date build would satisfy a newer same-date minimum.
// Every future minimum MUST advance the ISO DATE, never only the suffix.
const MinAddonBuildStopSlot = "2026-09-05-g2"

// MinAddonBuildStopLimit is the minimum AddOn build that builds a stop_entry
// as OrderType.StopLimit (LimitPrice == StopPrice) when the signal carries
// stop_limit=true — the mentor's no-stop-MARKET rule (D1.4 p1 @24:41). Below
// this floor an AddOn would build StopMarket and fill sloppily, so the knob is
// fail-closed: Go refuses to set the flag unless the far side proves this
// build. A NEW id, not the cancel-report id: the two capabilities ship in
// separate PRs and an AddOn that proves cancel reports does not necessarily
// build stop-limits.
const MinAddonBuildStopLimit = "2026-10-03-c2"

// MinAddonBuildProtectiveStop is the minimum AddOn build that can honour
// place_protective_stop — the frame D5's reconciler uses to restore a stop for a
// position the broker holds unprotected. There was NO wire command for this
// before 2026-09-07: SetStopLoss writes a local map that a later entry reads
// (trader/ninjatrader/tcp_trader.go), move_stop needs a stop that already
// exists, and modify_bracket needs a live bracket. So an older AddOn cannot
// place one, and the reconciler must REFUSE to send rather than log a placement
// that never happened.
//
// The ISO DATE advances, per the rule above: a suffix-only bump would let an
// older same-date build satisfy this.
const MinAddonBuildProtectiveStop = "2026-09-07-h1"

// MinAddonBuildPictureHtf is the minimum AddOn build that proves the
// two-picture evidence surface: bar frames carry final + emitted_at and
// order_update frames carry the rejection reason. The picture mode stays
// UNAVAILABLE below this floor — its timing law rests on the final marker,
// and capability is proven by RECEIPT, never assumed (same rule as every
// other floor).
const MinAddonBuildPictureHtf = "2026-09-20-p1"

// MinAddonBuildCancelReport is the minimum AddOn build that certifies the
// CANCEL-REPORT surface: on EVERY cancel_order receipt the AddOn echoes the
// target order's current state as an order_update carrying cancel_report=true
// (dedupe-bypassed), and its OnOrderUpdate stream continues to carry terminal
// states. The report regime (CANCEL_CONFIRM_REQUIRE_REPORT) settles cancels
// ONLY on these positive per-order reports — the order_snapshot deliberately
// omits terminal orders (C# D3, 2026-09-07), so absence from a book is not a
// terminal report and must not be treated as one when the regime is ON.
//
// The ISO date advances, per the suffix rule above.
const MinAddonBuildCancelReport = "2026-10-03-c1"

// CancelReportProven reports whether the far-side AddOn certifies cancel
// reports. Unknown ("") NEVER satisfies — capability is proven by receipt,
// not assumed (same rule as every other floor).
func CancelReportProven(buildID string) bool {
	return FarSideProven(buildID, MinAddonBuildCancelReport)
}

// CancelReportRegimeOn mirrors the trader's CANCEL_CONFIRM_REQUIRE_REPORT knob
// (the provider package cannot import the trader package). N9 (review r2,
// 2026-10-03): with the regime OFF the read loop DROPS cancel-report echo
// frames BEFORE NoteEntryExecution, the ordered worker's snapshot wait,
// RetryPendingNT8Exits and the picture-HTF consumer can see them — a report
// frame is only meaningful to the regime, and the wire stays byte-identical
// with the knob OFF.
func CancelReportRegimeOn() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CANCEL_CONFIRM_REQUIRE_REPORT"))) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// ErrAddonBuildTooOld is the sentinel behind a stop entry refused because the
// AddOn NT8 has loaded predates the stop-slot fix. Callers errors.Is on it so a
// build refusal is counted apart from a transport or account failure.
var ErrAddonBuildTooOld = errors.New("addon build predates the stop-slot fix")

// ErrFarSideNotProven is the sentinel behind a capability refusal whose cause
// is NOT an old build but the ABSENCE of a current proof: no hello / heartbeat
// has carried a build id on the current connection yet, or the connection that
// proved one has since disconnected — closeConn retires the proof the moment
// the link drops (FIX-P1A), so build "" after a disconnect means exactly
// "not proven since reconnect". It is deliberately distinct from
// ErrAddonBuildTooOld (which means a build DID report and predates the floor):
// a link-down refusal must not be counted or reported as an AddOn-too-old
// refusal (B-rules traceability).
var ErrFarSideNotProven = errors.New("far side not proven since reconnect")

// FarSideProven reports whether the far-side build id satisfies a minimum
// build requirement. Unknown ("") NEVER satisfies — capability is proven by
// receipt, not assumed.
func FarSideProven(buildID, minBuild string) bool {
	return buildID != "" && buildID >= minBuild
}

// Plan 4.4 Stage 2 — bar frame types (envelope format identical to signal/fill/heartbeat/ack: 4-byte big-endian length + JSON {type, payload}).
// See ninjascript/vltrader_tcp_PROTOCOL.md sections 5-8 for field semantics.
const (
	FrameBarsSubscribe   FrameType = "bars_subscribe"
	FrameBarsHistorical  FrameType = "bars_historical"
	FrameBarUpdate       FrameType = "bar_update"
	FrameBarsUnsubscribe FrameType = "bars_unsubscribe"
)

// HISTORY IMPORT (wave 101, 2026-09-11) — contract-qualified historical pull,
// Go-server → C#-AddOn → Go-server. The live bars_subscribe path resolves the
// PLATFORM's front month (VLInstrumentLookup.cs) and can never ask for an
// expired contract; a backtest needs years, contract by contract. These three
// frames are the named-contract channel:
//
//	bars_history_request  Go → C# : ask for one named contract, one timeframe,
//	                               one [from, to) window. The contract is the
//	                               EXPLICIT name ("MNQ 09-23") — never derived
//	                               from a date (that is the 09-10 roll bug).
//	bars_history_data     C# → Go : one chunk of the answer, ascending by time.
//	                               The C# side chunks at ~8k bars to stay under
//	                               the 1 MB envelope; seq/last terminate the
//	                               stream.
//	bars_history_error    C# → Go : the contract/timeframe could not be served
//	                               (three-state honesty: unavailable is named).
//
// A bars_history_request NEVER touches the live BarsRequest subscriptions and
// never mutates NT8 state; the AddOn disposes its request on completion.
const (
	FrameBarsHistoryRequest FrameType = "bars_history_request"
	FrameBarsHistoryData    FrameType = "bars_history_data"
	FrameBarsHistoryError   FrameType = "bars_history_error"
)

// BarsHistoryRequestPayload is one named-contract historical pull. FromMs/ToMs
// are the CLOSED bar open-time window [from, to).
type BarsHistoryRequestPayload struct {
	RequestID string `json:"request_id"` // caller-chosen correlation id
	Symbol    string `json:"symbol"`     // root, e.g. "MNQ"
	Contract  string `json:"contract"`   // EXPLICIT name, e.g. "MNQ 09-23"
	Timeframe string `json:"timeframe"`  // "1m" | "5m" | "15m" | "1h" | ...
	FromMs    int64  `json:"from_ms"`    // first bar open, epoch ms UTC
	ToMs      int64  `json:"to_ms"`      // exclusive end, epoch ms UTC
}

// BarsHistoryDataPayload is one chunk of a named-contract pull. Seq numbers the
// chunks from 1; Last=true ends the stream. Contract echoes the ACKed name the
// bars were actually served from (the C# side sends the instrument's real
// ContractName — never the request string).
type BarsHistoryDataPayload struct {
	RequestID string `json:"request_id"`
	Symbol    string `json:"symbol"`
	Contract  string `json:"contract"`
	Timeframe string `json:"timeframe"`
	Seq       int    `json:"seq"`
	Last      bool   `json:"last"`
	Bars      []Bar  `json:"bars"` // ascending by time
}

// BarsHistoryErrorPayload names a pull the AddOn could not serve, and why.
type BarsHistoryErrorPayload struct {
	RequestID string `json:"request_id"`
	Contract  string `json:"contract"`
	Reason    string `json:"reason"`
}

// Plan 4.11 — real NT account balance. C#-AddOn → Go-server, additive frame
// (same envelope: 4-byte BE length + JSON {type, payload}). The C# AddOn emits
// it from the resolved Sim account on AccountItemUpdate + periodically, so the
// dashboard shows the real SIM balance instead of the $50k mock. Existing
// frames are unchanged.
const FrameAccountBalance FrameType = "account_balance"

// accounts_list frame: C#-AddOn → Go-server, unsolicited event emitted on connect
// and when NT8 accounts change. Reports all available accounts with SIM detection flag.
const FrameAccountsList FrameType = "accounts_list"

// AccountSelectPayload requests the C# AddOn to switch to a different account.
// Go-server → C#-AddOn command.
const FrameAccountSelect FrameType = "account_select"

// AccountBalancePayload carries an NT account snapshot. Fields map to
// NinjaTrader AccountItem values (CashValue, BuyingPower, RealizedProfitLoss,
// UnrealizedProfitLoss). NetLiquidation is the dashboard's "total equity"
// (cash + unrealized PnL); the C# side sends it directly if NT exposes it,
// else the Go side derives it as CashValue + UnrealizedPnL.
type AccountBalancePayload struct {
	Account        string  `json:"account"` // e.g. "Sim101"
	CashValue      float64 `json:"cash_value"`
	BuyingPower    float64 `json:"buying_power"`
	RealizedPnL    float64 `json:"realized_pnl"`
	UnrealizedPnL  float64 `json:"unrealized_pnl"`
	NetLiquidation float64 `json:"net_liquidation"` // total equity
}

// Position-history fix — C#-AddOn → Go-server, additive frame (same envelope).
// Emitted when an OCO exit leg (SL or TP) fills and the position goes flat, so
// the Go side can mark the open trader_position CLOSED with the real exit price.
// NT closes positions broker-side via the bracket; the bot never issues a
// close_* order, and NT has no order-sync, so without this frame the open
// position record never transitions to CLOSED and position history stays empty.
const FramePositionClose FrameType = "position_close"

// PositionClosePayload reports a closed (or partially closed) NT position.
// PositionSide is the side that was HELD ("long"/"short"), not the exit order's
// action. RealizedPnL is left to the Go side to compute against the recorded
// entry × the futures point value (single source of truth: market.FuturesPointValue).
type PositionClosePayload struct {
	SignalID     string  `json:"signal_id"`     // entry signal_id, for correlation
	Symbol       string  `json:"symbol"`        // root symbol, e.g. "MNQ"
	PositionSide string  `json:"position_side"` // "long" | "short" (held side)
	ExitPrice    float64 `json:"exit_price"`
	Quantity     int     `json:"quantity"`
	ExitReason   string  `json:"exit_reason"` // "sl" | "tp" | "manual"
	ExitTime     string  `json:"exit_time"`   // RFC3339
	// Account: the NT8 sub-account this close is on. The C# AddOn already SENDS it
	// (SendPositionCloseFrame ["account"]); Go simply wasn't parsing it. Used by
	// close-sync owner-routing to match the close to the trader that OWNS the row.
	Account string `json:"account"` // e.g. "Sim101"
	// A2 (G1, wire v3) — echoed originator identity + op seq for echo-verify.
	TraderID string `json:"trader_id,omitempty"`
	Seq      uint64 `json:"seq,omitempty"`
	// W117 F2 — see FillPayload.OrderedOwned.
	OrderedOwned bool `json:"-"`
}

// Rejected exit/flatten — C#-AddOn → Go-server, additive frame. The SIM (or
// broker) REJECTED an exit/flatten order (e.g. "There is no market data available
// to drive the simulation engine" while the Tradovate feed is down). The close did
// NOT take effect — the position is STILL OPEN in NT8 — so the bot must not treat
// it as closed. Pairs with the close routing (decision closes wait for the
// position_close fill frame): this frame turns a silent rejected flatten into a
// loud alarm so the orphan can't be netted onto by the next entry.
const FramePositionCloseRejected FrameType = "position_close_rejected"

// PositionCloseRejectedPayload reports a rejected exit/flatten order.
type PositionCloseRejectedPayload struct {
	SignalID     string `json:"signal_id"`     // order name (entry signal_id, or "Close" for a flatten)
	Symbol       string `json:"symbol"`        // root symbol, e.g. "MNQ"
	PositionSide string `json:"position_side"` // "long" | "short" (held side)
	Reason       string `json:"reason"`        // NT8 reject comment, e.g. "There is no market data..."
	Account      string `json:"account"`       // e.g. "Sim101"
	RejectTime   string `json:"reject_time"`   // RFC3339
	// A2 (G1, wire v3) — echoed originator identity + op seq for echo-verify.
	TraderID string `json:"trader_id,omitempty"`
	Seq      uint64 `json:"seq,omitempty"`
}

// Feed status — C#-AddOn → Go-server, additive frame. Carries the NT8 price-feed
// PriceStatus (from OnVLConnectionStatusUpdate). The bot gates opens/closes when
// the feed is not Connected: the SIM rejects orders with "no market data", and
// acting into a dead feed is the upstream condition behind the phantom-close mess.
const FrameFeedStatus FrameType = "feed_status"

// FeedStatusPayload reports the NT8 price-feed status.
type FeedStatusPayload struct {
	PriceStatus string `json:"price_status"` // "Connected" | "ConnectionLost" | "Connecting" | "Disconnected"
	Time        string `json:"time"`         // RFC3339
}

// instrument_info — C#-AddOn → Go-server, additive frame. Carries the RESOLVED
// NT8 instrument's REAL contract specs (MasterInstrument.PointValue/.TickSize +
// the qualified contract name) so Go can source ground-truth from NT8 and
// cross-check the hardcoded FuturesPointValue/FuturesTickSize tables (drift
// detection). The tables remain the fallback for parked/unresolved symbols.
const FrameInstrumentInfo FrameType = "instrument_info"

// InstrumentInfoPayload reports a resolved instrument's NT8 specs.
type InstrumentInfoPayload struct {
	Symbol     string  `json:"symbol"`      // root, e.g. "MNQ"
	Contract   string  `json:"contract"`    // qualified, e.g. "MNQ 06-26"
	PointValue float64 `json:"point_value"` // NT8 MasterInstrument.PointValue
	TickSize   float64 `json:"tick_size"`   // NT8 MasterInstrument.TickSize
}

// Manual close — Go-server → C#-AddOn, additive frame. The AddOn flattens the
// position for the symbol (account.Flatten), which closes at market AND cancels
// the protective bracket so no orphaned SL/TP can re-open a position. The
// flatten's market fill comes back as a position_close (reason "manual").
const FrameClosePosition FrameType = "close_position"

// ClosePositionPayload requests a flatten of the symbol's position. Quantity is
// advisory (the AddOn flattens the whole position); Side is informational.
type ClosePositionPayload struct {
	Symbol   string `json:"symbol"`
	Side     string `json:"side"` // "long" | "short" (informational)
	Quantity int    `json:"quantity"`
	// LimitPrice (4.3): when > 0, the AddOn submits a LIMIT exit at this price
	// instead of an immediate market flatten. A follow-up close_position frame
	// WITHOUT limit_price is the market fallback (Go owns the timing). 0 =
	// historical market flatten, byte-identical framing.
	LimitPrice float64 `json:"limit_price,omitempty"`
	SignalID   string  `json:"signal_id"`
	// A2 (G1, wire v3) — identity stamp: the account this flatten targets + the
	// owning trader + the op seq. The AddOn echoes them on the resulting
	// position_close so Go verifies the close came back for the right originator.
	Account  string `json:"account,omitempty"`
	TraderID string `json:"trader_id,omitempty"`
	Seq      uint64 `json:"seq,omitempty"`
}

// A3 (G2) — account allowlist. The Go server sends this (once per connect, before
// flushing queued signals, and again whenever a new trader binds) with the FULL set
// of accounts this session's bound traders use. The AddOn REPLACES its allowlist
// with this owner-declared set and refuses to execute on anything outside it — the
// allowlist is thus established independently of the signal payload (never
// self-registered from a signal, which would be vacuous). A pre-A3 Go never sends
// it, so a new AddOn treats an all-absent allowlist as legacy → fail-open (allow),
// keeping the lockstep deploy window safe.
const FrameAccountRegister FrameType = "account_register"

// AccountRegisterPayload carries the authoritative bound-account allowlist.
type AccountRegisterPayload struct {
	Accounts []string `json:"accounts"`
}

// FrameMoveStop asks the AddOn to move a RESTING stop-loss order (keyed by the
// entry's signal_id) to a new price WITHOUT closing the position — auto-breakeven.
// Additive frame: an OLD AddOn (pre-move_stop) logs "unknown frame type" and
// ignores it, so the original protective stop keeps guarding the trade — which is
// exactly why the paired C# redeploy (cp → F5 → NT8 restart) is required to
// activate breakeven.
const FrameMoveStop FrameType = "move_stop"

// FramePlaceProtectiveStop asks the AddOn to place a STANDALONE protective stop
// for a position that has none — D5, 2026-09-07. It is not part of a bracket
// and joins no existing OCO group: it exists because the position is naked, and
// a group is what would let something else cancel it.
const FramePlaceProtectiveStop FrameType = "place_protective_stop"

// PlaceProtectiveStopPayload is the Go-server → C#-AddOn request. Quantity is
// the position size to cover; StopPrice is tick-rounded by the caller.
type PlaceProtectiveStopPayload struct {
	Symbol       string  `json:"symbol"`
	SignalID     string  `json:"signal_id"`     // names the order "<signal>-sl"
	PositionSide string  `json:"position_side"` // LONG | SHORT — the side HELD
	Quantity     int     `json:"quantity"`
	StopPrice    float64 `json:"stop_price"`
	Reason       string  `json:"reason,omitempty"`
	Timestamp    string  `json:"timestamp"`
	Account      string  `json:"account,omitempty"`
	TraderID     string  `json:"trader_id,omitempty"`
	Seq          uint64  `json:"seq,omitempty"`
}

// MoveStopPayload is the Go-server → C#-AddOn move-stop request.
type MoveStopPayload struct {
	Symbol      string  `json:"symbol"`
	SignalID    string  `json:"signal_id"`     // the entry's signal_id (the bracket key)
	NewStopLoss float64 `json:"new_stop_loss"` // tick-rounded new stop price
	Timestamp   string  `json:"timestamp"`     // RFC3339
	// Leg (REVIEW-353, wire v4): 1 = leg 1 only, 2 = leg 2 only, absent/0 = all.
	Leg int `json:"leg,omitempty"`
	// A2 (G1, wire v3) — identity stamp: target account + owning trader + op seq.
	Account  string `json:"account,omitempty"`
	TraderID string `json:"trader_id,omitempty"`
	Seq      uint64 `json:"seq,omitempty"`
}

// Open-position read-back — C#-AddOn → Go-server, additive snapshot frame (same
// envelope: 4-byte BE length + JSON {type, payload}, snake_case). The AddOn
// emits the selected account's CURRENT open positions: on account_select, on
// connect, and on any Account.PositionUpdate (INCLUDING a MANUAL open/close in
// NT8). This is the previously-missing position read-back — before it, Go only
// knew positions it opened itself (via a fill), so it lost the position on
// account switch-back and never saw manual trades. The frame is a FULL
// per-account snapshot (the complete non-flat set); the Go side REPLACES
// acctPositions[Account] with it, so a flat instrument simply drops out.
const FramePositions FrameType = "positions"

// OpenPosition is one non-flat NT position (per instrument) within a snapshot.
type OpenPosition struct {
	Symbol   string  `json:"symbol"`    // root symbol, e.g. "MNQ"
	Side     string  `json:"side"`      // "long" | "short"
	Quantity int     `json:"quantity"`  // contracts (NT8 nets per instrument)
	AvgPrice float64 `json:"avg_price"` // NT8 Position.AveragePrice
}

// PositionsPayload is the C#-AddOn → Go-server open-position snapshot for one
// account. Positions is the COMPLETE current non-flat set for Account (NT8 is
// the source of truth); the Go side replaces its per-account cache with it.
type PositionsPayload struct {
	Account   string         `json:"account"`
	Positions []OpenPosition `json:"positions"`
}

// Bar is the compact 6-field OHLCV bar used in bars_historical and bar_update
// frames (protocol §6-7). Volume is a float because NT8 tick-volume
// instruments report fractional values.
type Bar struct {
	T int64   `json:"t"` // Unix epoch ms, UTC
	O float64 `json:"o"`
	H float64 `json:"h"`
	L float64 `json:"l"`
	C float64 `json:"c"`
	V float64 `json:"v"` // volume can be tick-volume (fractional)
	// Final (W-PICTURE-HTF, 2026-09-20) is the AddOn's proof this bar CLOSED:
	// the boundary frame re-emits the just-closed bar with final=true, forming
	// bars carry final=false. ADDITIVE + omitempty — old AddOns never send it,
	// so a zero value means "unproven", never "closed".
	Final bool `json:"final,omitempty"`
	// EmittedAt is the AddOn's emission clock for this frame (ms, UTC) — the
	// evidence timestamp for H1-completion / freshness verdicts.
	EmittedAt int64 `json:"emitted_at,omitempty"`
	// Source is GO-SIDE ONLY (never on the wire; the AddOn does not send it):
	// which feed delivered this bar — "live" (bar_update), "historical"
	// (bars_historical replay) or "mixed" (a boot minute whose open came from a
	// replay and whose close from live). Stamped by the cache at Seed/Upsert.
	// BAR-SOURCE WAVE 2026-09-10: NT8's replay served the same minutes ~290
	// points from the live feed under one label, and nothing could tell which
	// it was holding.
	Source string `json:"-"`
}

// BarsSubscribePayload is the Go-server → C#-AddOn subscribe frame per
// protocol §5. One frame requests N timeframes for a single symbol; the AddOn
// opens one BarsRequest per timeframe.
type BarsSubscribePayload struct {
	Symbol     string   `json:"symbol"`
	Timeframes []string `json:"timeframes"` // e.g. ["1m","5m","15m","1h"]
	BarsBack   int      `json:"bars_back"`  // default 500 per protocol
}

// BarsHistoricalPayload is the C#-AddOn → Go-server one-shot batch sent when
// the initial BarsRequest completes per protocol §6. Bars are ordered
// ascending by time.
type BarsHistoricalPayload struct {
	Symbol    string `json:"symbol"`
	Timeframe string `json:"timeframe"`
	// Contract is the front month these bars belong to. The AddOn has named
	// it on EVERY bar frame since 2026-09-11 (owner ruling;
	// VLBarsSubscriptionManager.cs:488) and Go parsed it nowhere, so every
	// consumer was blind to which instrument it was reading. ADDITIVE and
	// omitempty: an AddOn that does not send it leaves this "", which reads
	// as UNKNOWN, never as a match.
	Contract string `json:"contract,omitempty"`
	Bars     []Bar  `json:"bars"` // ascending by time
}

// BarUpdatePayload is the C#-AddOn → Go-server streaming update per protocol
// §7. Bars is ALWAYS an array — a single NT8 tick can update multiple bar
// indices (MinIndex..MaxIndex) when crossing timeframe boundaries.
type BarUpdatePayload struct {
	Symbol    string `json:"symbol"`
	Timeframe string `json:"timeframe"`
	// Contract — see BarsHistoricalPayload.Contract
	// (VLBarsSubscriptionManager.cs:562). "" means UNKNOWN, never a match.
	Contract string `json:"contract,omitempty"`
	Bars     []Bar  `json:"bars"` // ALWAYS an array — single tick can update multiple indices (NT8 multi-bar gotcha). Walk MinIndex..MaxIndex.
}

// BarsUnsubscribePayload is the Go-server → C#-AddOn teardown frame per
// protocol §8. Empty/nil Timeframes means "all timeframes for this symbol".
type BarsUnsubscribePayload struct {
	Symbol     string   `json:"symbol"`
	Timeframes []string `json:"timeframes,omitempty"` // empty/nil = all for this symbol
}

// AccountInfo carries a single NT account's name + SIM flag.
type AccountInfo struct {
	Name  string `json:"name"`   // e.g. "Sim101" or "PropAcct"
	IsSim bool   `json:"is_sim"` // true if this is a simulation account
}

// AccountsListPayload reports all available NT accounts discovered by the C# AddOn.
// Emitted unsolicited on connect and on account list change, so the Go server can
// populate the UI's account selector and validate account_select commands.
type AccountsListPayload struct {
	Accounts []AccountInfo `json:"accounts"`
}

// AccountSelectPayload requests the C# AddOn to switch to a different account.
// The AddOn unsubscribes from the old account's events, resolves the new account
// by name, subscribes to its events, and re-emits account_balance immediately.
type AccountSelectPayload struct {
	Account string `json:"account"` // e.g. "Sim101"
}

// ErrFrameTooLarge signals the peer sent a length header > TCPMaxFrameBytes.
// Per spec L4416, the server logs and closes the connection on this error.
var ErrFrameTooLarge = errors.New("tcp_framing: frame exceeds max size")

// WriteFrame writes a single length-prefixed JSON frame to w.
// Returns an error if the marshalled body exceeds TCPMaxFrameBytes.
func WriteFrame(w io.Writer, frameType FrameType, payload any) error {
	body, err := marshalEnvelope(frameType, payload)
	if err != nil {
		return err
	}
	if len(body) > TCPMaxFrameBytes {
		return fmt.Errorf("tcp_framing: frame too large (%d > %d)", len(body), TCPMaxFrameBytes)
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(body)))
	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("tcp_framing: write header: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("tcp_framing: write body: %w", err)
	}
	return nil
}

// ReadFrame reads a single length-prefixed JSON frame from r.
// Returns ErrFrameTooLarge if the length header exceeds TCPMaxFrameBytes
// (server closes the connection per spec L4416).
func ReadFrame(r io.Reader) (Envelope, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Envelope{}, err
	}
	length := binary.BigEndian.Uint32(hdr[:])
	if length > TCPMaxFrameBytes {
		return Envelope{}, ErrFrameTooLarge
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return Envelope{}, fmt.Errorf("tcp_framing: read body: %w", err)
	}
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return Envelope{}, fmt.Errorf("tcp_framing: bad JSON: %w", err)
	}
	return env, nil
}

func marshalEnvelope(frameType FrameType, payload any) ([]byte, error) {
	var raw json.RawMessage
	if payload == nil {
		raw = json.RawMessage("{}")
	} else {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("tcp_framing: marshal payload: %w", err)
		}
		raw = b
	}
	body, err := json.Marshal(Envelope{Type: frameType, Payload: raw})
	if err != nil {
		return nil, fmt.Errorf("tcp_framing: marshal envelope: %w", err)
	}
	return body, nil
}
