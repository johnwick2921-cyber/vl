# VLTrader TCP Wire Protocol

This is the local C#-side copy of the wire protocol. The authoritative implementation is `provider/ninjatrader/tcp_server.go` in the Go repo; any drift between this doc and the Go impl is a bug — file an issue.

Spec source: `docs/superpowers/plans/2026-05-22-nq-databento-ninjatrader.md` lines 4343-4447.

## Transport

- TCP loopback: `127.0.0.1:36974` (note: NOT NinjaTrader's ATI port 36973).
- Single concurrent client: NT8 AddOn → Go server. A second client is rejected.
- Connection is opened by the C# AddOn at NT8 startup; reconnects every 5 seconds on disconnect.

## Framing

Each frame on the TCP stream is:

1. 4-byte **big-endian** unsigned 32-bit length prefix.
2. UTF-8 JSON payload of exactly `length` bytes.

Maximum frame size: **1 MB (1,048,576 bytes)**. Oversized frames are a protocol error — the receiving side closes the connection.

## Envelope

Every JSON body has the same outer shape:

```json
{
  "type": "signal" | "fill" | "heartbeat" | "ack" | "bars_subscribe" | "bars_historical" | "bar_update" | "bars_unsubscribe",
  "payload": { }
}
```

## Message types

### 1. `signal` (Go server → C# AddOn)

Outgoing trade signal. Each numeric field is tick-rounded by the Go side before emission (see `trader/ninjatrader/tick_rounding.go`).

```json
{
  "type": "signal",
  "payload": {
    "symbol": "MNQ",
    "side": "long",
    "quantity": 1,
    "entry": 21500.25,
    "stop_loss": 21450.0,
    "take_profit": 21550.0,
    "signal_id": "uuid-v4-string",
    "timestamp": "2026-05-26T18:00:00Z"
  }
}
```

**Field semantics:**

- `symbol`: NT8 instrument symbol (e.g. `MNQ`, `NQ`, `ES`, `MES`).
- `side`: `long` (buy) or `short` (sell short).
- `quantity`: number of contracts.
- `entry`: market entry reference (NT8 uses market orders; this is the AI's planned entry — used only for slippage attribution in the fill frame).
- `stop_loss`: stop price (tick-rounded).
- `take_profit`: limit price (tick-rounded).
- `signal_id`: UUID. Used as the OCO group ID and as the fill correlation key.
- `timestamp`: RFC3339 UTC. The AddOn rejects signals older than 60 seconds as stale.

### 1b. `close_position` (Go server → C# AddOn)

Flatten a held position. Historical behavior: immediate market flatten +
working-order cancel. **4.3 (limit-then-market, dormant):** `limit_price > 0`
submits a LIMIT exit at that price instead — the Go side owns the timing and
sends the market fallback as a later `close_position` frame WITHOUT
`limit_price`, whose Flatten cancels the resting limit.

```json
{
  "type": "close_position",
  "payload": {
    "symbol": "MNQ",
    "side": "long",
    "quantity": 1,
    "limit_price": 0,
    "signal_id": "uuid-v4-string",
    "account": "Sim101",
    "trader_id": "hoang",
    "seq": 42
  }
}
```

**Field semantics:**

- `limit_price` (4.3): 0/absent = market flatten (historical, byte-identical
  framing). `> 0` = submit a LIMIT exit at this price. The order is named
  `<signal_id>-lx`; its fill routes to `position_close` with reason `"limit"`
  and cancels the still-live SL/TP bracket legs (`CancelBracketsFor`) so they
  can never re-enter the now-flat position.
- All other fields unchanged (identity stamp, account routing, SIM-only guard).

### 2. `fill` (C# AddOn → Go server)

Outgoing fill notification.

```json
{
  "type": "fill",
  "payload": {
    "signal_id": "matching-uuid",
    "fill_price": 21500.5,
    "fill_time": "2026-05-26T18:00:01Z",
    "side": "long",
    "quantity": 1,
    "slippage_ticks": 1.0,
    "status": "filled"
  }
}
```

**Field semantics:**

- `signal_id`: matches the originating signal's `signal_id`.
- `fill_price`: actual average fill price.
- `fill_time`: RFC3339 UTC.
- `slippage_ticks`: `(fill_price - entry) / tick_size`, signed (positive = paid more than planned for a long, less than planned for a short).
- `status`: `filled`, `rejected`, or `partial`. Rejected fills indicate NT8 refused the order; the Go side does NOT retry — manual operator intervention is required.
- `status: "duplicate_ignored"` (FIX-DOUBLE-ENTRY, 2026-09-26): the AddOn's
  seen-signal dedupe answered a REPLAYED entry frame — the same `signal_id`
  already arrived and was submitted (or is still tracked). No order was placed
  for this frame. The Go side treats this as ALREADY HANDLED: no pending
  drop, no cached fill, no reject, no re-arm. The original entry's own
  fill/order_update owns the state. A pre-dedupe AddOn never emits this
  status, and the Go guard does not depend on it. The seen set is TTL-bounded
  (10 min, never cleared on fill) and HARD-capped (oldest-first eviction past
  the cap, logged with a WARN).
- `symbol` (P5.2, protocol v2): the order's instrument root (e.g. `"MNQ"`), for multi-symbol attribution. EMPTY/absent = legacy (pre-v2) AddOn → the Go side attributes the fill to the trader's primary symbol. The Go fill consumer REJECTS (warn + drop) a non-empty symbol that doesn't match the trader's instrument — the split-brain defense.
- `account` (Phase 4): the NT8 account the fill executed on.

### 2b. `hello` (bidirectional) — P5.2, protocol v2

The C# AddOn sends `hello` as the FIRST frame on every (re)connect; the Go server replies with its own `hello`.

```json
{ "type": "hello", "payload": { "protocol_version": 2, "source": "vltrader-addon" } }
```

**Semantics:**

- `protocol_version`: the peer's wire generation (`ProtocolVersion` in `tcp_framing.go` == `PROTOCOL_VERSION` in `VLTraderTCPClient.cs`; v2 = symbol-tagged fills + this handshake). Bump ONLY with a lockstep C#+Go ship.
- Go server on MISMATCH: logs `PROTOCOL VERSION MISMATCH — refusing connection` and CLOSES (never silently misparses). The AddOn's reconnect loop will retry + log its own mismatch warning from the server's reply.
- Go server on a connection that NEVER sends hello: tolerated as a LEGACY (pre-v2) AddOn with a one-time warning — so the lockstep deploy window (new Go, not-yet-F5'd C#) cannot brick the bar feed.
- `source`: `"vltrader-addon"` or `"vl-go"` (diagnostics only).

### 3. `heartbeat` (bidirectional)

Empty payload. Sent every 30 seconds by both sides.

```json
{ "type": "heartbeat", "payload": {} }
```

### 4. `ack` (bidirectional)

```json
{ "type": "ack", "payload": { "acks": "heartbeat" } }
```

Or for a specific signal:

```json
{ "type": "ack", "payload": { "acks": "uuid-v4-string" } }
```

### 5. `bars_subscribe` (Go server → C# AddOn) — Plan 4.4 Stage 1

Request native multi-timeframe bar subscriptions for a single NT8 instrument.
For each timeframe the C# AddOn opens one `BarsRequest` against NT8's data
engine, emits one `bars_historical` frame on the initial load, then streams
`bar_update` frames as ticks arrive.

```json
{
  "type": "bars_subscribe",
  "payload": {
    "symbol": "MNQ",
    "timeframes": ["1m", "5m", "15m", "1h"],
    "bars_back": 500
  }
}
```

**Field semantics:**

- `symbol`: NT8 instrument symbol (e.g. `MNQ`). The AddOn resolves it via
  `Instrument.GetInstrument(symbol)` exactly like the signal path.
- `timeframes`: list of timeframe strings the engine wants. Valid values mirror
  `store/strategy.go::normalizeTimeframe`: `1m, 3m, 5m, 15m, 30m, 1h, 2h, 4h,
  6h, 8h, 12h, 1d, 3d, 1w`. The AddOn maps each to a `BarsPeriod` and opens a
  separate `BarsRequest`. Unknown values are skipped with a log warn.
- `bars_back`: number of historical bars to load on initial subscription
  (default 500 if omitted). Same value used for every requested timeframe.

Re-subscribing to a `(symbol, timeframe)` that already has an active
subscription is idempotent — the second `bars_subscribe` returns immediately
without re-opening the `BarsRequest`.

### 6. `bars_historical` (C# AddOn → Go server) — Plan 4.4 Stage 1

Sent ONCE per `(symbol, timeframe)` when the initial `BarsRequest` completes.
Carries the full historical batch in a single frame (bounded by the 1 MB
envelope ceiling — at ~80 bytes per bar that's ~13k bars headroom, more than
the engine ever requests).

```json
{
  "type": "bars_historical",
  "payload": {
    "symbol": "MNQ",
    "timeframe": "1m",
    "bars": [
      { "t": 1748352000000, "o": 21500.25, "h": 21501.00, "l": 21500.00, "c": 21500.75, "v": 42 },
      { "t": 1748352060000, "o": 21500.75, "h": 21501.25, "l": 21500.50, "c": 21501.00, "v": 38 }
    ]
  }
}
```

**Field semantics:**

- `symbol`, `timeframe`: echo the originating `bars_subscribe` keys.
- `bars`: ordered ascending by time, each bar is a compact 6-field object:
  - `t`: Unix epoch milliseconds, **UTC**. The C# side converts NT8's
    local-time `bars.GetTime(i)` to UTC using
    `bars.TradingHours.TimeZoneInfo` before emit. Charts + indicators on the
    Go side assume UTC; mis-timezoned bars are the #1 cause of off-by-an-hour
    bugs.
  - `o, h, l, c`: bar OHLC, double-precision.
  - `v`: volume, may be a `double` for tick-volume instruments — emitted as
    JSON number, no special encoding.

### 7. `bar_update` (C# AddOn → Go server) — Plan 4.4 Stage 1

Streaming updates from `BarsRequest.Update`. **The `bars` field is an array
even when only one bar changed** — a single tick can update multiple bar
indices when crossing minute boundaries with high-frequency feeds, and the
NT8 contract is to walk `MinIndex..MaxIndex`. Always emit every bar in that
range, in ascending order.

```json
{
  "type": "bar_update",
  "payload": {
    "symbol": "MNQ",
    "timeframe": "1m",
    "bars": [
      { "t": 1748352120000, "o": 21501.00, "h": 21501.50, "l": 21500.75, "c": 21501.25, "v": 17 }
    ]
  }
}
```

**Field semantics:** identical to `bars_historical`. The Go side dedupes
by `(symbol, timeframe, t)` and treats a later frame for the same `t` as an
update-in-progress.

**`final` + `emitted_at` (W-PICTURE-HTF, 2026-09-20, ADDITIVE):** the AddOn
marks forming bars `final:false`. NT8 never re-emits the just-closed bar, so
at the boundary the AddOn re-emits the previous bar ONCE with `final:true` —
its proof the bar CLOSED. `emitted_at` is the AddOn's emission clock (ms,
UTC) for every bar. A bar once `final:true` is never re-emitted as forming.
The Go side's deterministic two-picture evaluator consumes only final-marked
bars, and the mode stays UNAVAILABLE below build `2026-09-20-p1` (proven by
receipt of `build_id` on the heartbeat, same rule as every capability floor).
Old Go ignores the new fields; old AddOns never send them (zero = unproven).

### 8. `bars_unsubscribe` (Go server → C# AddOn) — Plan 4.4 Stage 1

Tear down one or more `(symbol, timeframe)` subscriptions cleanly.

```json
{
  "type": "bars_unsubscribe",
  "payload": {
    "symbol": "MNQ",
    "timeframes": ["1m", "5m"]
  }
}
```

Omitting `timeframes` (empty or missing) is equivalent to "all timeframes
for this symbol." The AddOn disposes the affected `BarsRequest`s, stops
emitting `bar_update` frames for them, and removes them from its
subscription registry.

### 8b. `bars_history_request` / `bars_history_data` / `bars_history_error` — HISTORY IMPORT (wave 101)

The live `bars_subscribe` path resolves the PLATFORM's front month and can
never ask for an expired contract. These three frames are the named-contract
channel: a one-off pull of `"MNQ 09-23"` over an explicit `[from, to)` window,
answered in chunks. It never touches the live subscription state.

```json
// Go server → C# AddOn
{ "type": "bars_history_request",
  "payload": {
    "request_id": "imp-3f9a…",     // caller-chosen correlation id
    "symbol":      "MNQ",
    "contract":    "MNQ 09-23",    // EXPLICIT name — never a date rule
    "timeframe":   "1m",           // 1m 3m 5m 15m 30m 1h 2h 4h 6h 8h 12h 1d
    "from_ms":     1700000000000,  // first bar open, epoch ms UTC (0 = contract start)
    "to_ms":       1710000000000   // exclusive end (0 = contract end)
  } }

// C# AddOn → Go server, one chunk per ~8k bars, ascending by time,
// seq from 1, last=true terminates the stream.
{ "type": "bars_history_data",
  "payload": {
    "request_id": "imp-3f9a…",
    "symbol":      "MNQ",
    "contract":    "MNQ 09-23",    // the instrument's REAL ContractName — echoed
    "timeframe":   "1m",
    "seq": 2, "last": true,
    "bars": [ { "t": 1700000000000, "o": 21500.25, "h": 21501.0,
                "l": 21500.0, "c": 21500.75, "v": 42 } ]
  } }

// C# AddOn → Go server — a pull that cannot be served is ANSWERED, never silent.
{ "type": "bars_history_error",
  "payload": { "request_id": "imp-3f9a…", "contract": "MNQ 09-23",
               "reason": "unavailable: instrument MNQ 09-23 not found on this platform" } }
```

Rules the pull obeys: `MergePolicy.DoNotMerge` ALWAYS (a back-adjusted series is
a different price scale wearing the same label — the 09-10 replay damage);
`TradingHours` = CME US Index Futures ETH; the request is disposable and the
AddOn tears it down on completion or terminate. The Go importer writes only
through `store.ImportBars` (no upsert; collision = keep + count) with
`source=historical_import`, and refuses a data frame whose echoed contract
differs from the one it asked for.

### 9. `subscribed` / `unsubscribed` / `subscribe_error` (C# AddOn → Go server) — P5.3

Subscription lifecycle acks, sent by the AddOn in response to `bars_subscribe` / `bars_unsubscribe`:

```json
{ "type": "subscribed",      "payload": { "symbol": "NQ", "resolved_contract": "NQ 06-26" } }
{ "type": "unsubscribed",    "payload": { "symbol": "NQ", "removed": 14 } }
{ "type": "subscribe_error", "payload": { "symbol": "XYZ", "reason": "qualified contract 'XYZ 06-26' not found in NT8 (not loaded?)" } }
```

**Semantics:**

- `subscribed`: emitted after the BarsRequests open, with the resolved front-month (`instrument.FullName`). The Go side marks the symbol `subscribed` (visible via `GET /api/nt/symbols`).
- `unsubscribed`: emitted after `bars_unsubscribe` disposal; `removed` counts the disposed `(symbol|timeframe)` subscriptions (0 = wasn't subscribed).
- `subscribe_error`: a FAILED subscribe (instrument unresolved / not in NT8's DB) — surfaces Go-side instead of dying silently in the NT8 Output window.
- ADDITIVE: a pre-P5.3 AddOn never sends these (the Go state shows `pending`; bars still flow). A pre-P5.3 Go logs them as unknown frames (harmless warn) — ship Go before the AddOn F5, as with v2.

### 10. `move_stop` (Go server → C# AddOn) — auto-breakeven

Moves an OPEN position's resting stop-loss to a new price WITHOUT closing it.
Used by auto-breakeven (once the trade is +N points in profit → stop → entry).

```json
{ "type": "move_stop",
  "payload": { "symbol": "MNQ", "signal_id": "<entry uuid>",
               "new_stop_loss": 30352.00, "timestamp": "RFC3339" } }
```

- The AddOn finds the live bracket by `signal_id` and MODIFIES THE SAME resting
  stop order IN PLACE: `SlOrder.StopPriceChanged = new_stop_loss` →
  `account.Change(SlOrder)`. Same order object, same OCO group, no new order and no
  cancel → the take-profit and the OCO group are never disturbed.
- **Why not submit-new-then-cancel-old (the 2026-08-07 fix):** the old AddOn created
  a NEW `StopMarket` in the SAME OCO group and cancelled the old stop. NT8 OCO
  cancels the WHOLE group when any member is cancelled, so the take-profit AND the
  new stop both died → NAKED position. Proven live 2026-08-07 11:25:05 (signal
  `b846e082…`: `-tp` Cancelled + both `-sl` orders Cancelled). `account.Change`
  never issues a cancel, so no cascade is possible.
- Guards: only acts when the stop is in a changeable state (`Working`/`Accepted`);
  no-op if the bracket already exited or the stop is already at that price (½-tick
  idempotency); on a non-changeable stop or a `Change` exception it replies
  `ack: move_stop_error` (not success) so Go re-arms/retries next cycle. Success
  replies `ack: move_stop`.
- **Additive frame:** an OLD AddOn (pre-`move_stop`) logs "unknown frame type" and
  ignores it — the original stop keeps protecting the trade. Activating breakeven
  therefore REQUIRES the paired redeploy (cp `ninjascript/*.cs` → AddOns → F5 →
  clean NT8 restart).

## Failure modes

- **TCP disconnect**: the server holds the signal queue; on reconnect, it sends pending signals with their original timestamps. The C# AddOn may reject signals older than 60s as stale (emits a `status=rejected` fill).
- **Heartbeat timeout**: the server closes the connection after 60s without an ack; the C# AddOn reconnects every 5s.
- **Invalid frame** (bad length, >1 MB, malformed JSON): the receiver logs a warning and closes the connection; the other side reconnects.
- **Order rejection by NT8**: the AddOn emits a fill frame with `status=rejected`; the Go side logs the rejection and does NOT retry.

## Cross-references

- Go-side authoritative implementation: `provider/ninjatrader/tcp_server.go`.
- Go-side framing codec: `provider/ninjatrader/tcp_framing.go` (length-prefix + JSON envelope shared with this AddOn).
- Architectural rationale: `docs/adr/ADR-001-csv-bridge-vs-tcp.md`.
- Plan 1.5 spec: `docs/superpowers/plans/2026-05-22-nq-databento-ninjatrader.md` lines 4343-4447.
- Plan 1 critical-file integrity guard: `docs/adr/ADR-007-plan1-critical-file-integrity.md` (Plan 1.5 is purely additive — none of the CSV bridge files are modified).
- Plan 4.4 deep spec: same plan doc, Plan 4.4 Deep Spec section. Defines `bars_subscribe`, `bars_historical`, `bar_update`, `bars_unsubscribe` envelopes consumed by the new C# `VLBarsSubscriptionManager`.
- Plan 4.4 Stage 1 C# implementation: `ninjascript/VLBarsSubscriptionManager.cs`. Isolates BarsRequest logic from the proven signal/fill/heartbeat path in `VLTraderTCPClient.cs` (which gains only a field, a constructor call, and two switch cases).

## order_snapshot (F12, 2026-09-03) — AddOn → Go

The BROKER's working-order book. Every other frame in this protocol is an
EVENT; `order_update` fires on a state change, so a Go-side restart loses the
picture until the next change happens — which on a quiet book may be never.
Cutover leg 4 therefore had to read the Go side's own `armed_orders` ledger and
call it the broker's book.

Emitted (a) every `ORDER_SNAPSHOT_INTERVAL_MS` (30 s, riding the heartbeat loop)
and (b) immediately after any order state change.

**ACCOUNT-SCOPED.** `Account.Orders` is an account collection and the AddOn holds
no persistent per-instrument handle, so one frame covers the account and every
order carries its own `symbol`. The Go side files the book per account and
filters by instrument. This is what keeps an EMPTY book representable: an account
with no working orders still emits `orders: []`. **"No orders" and "no answer"
are different claims** and leg 4 must be able to tell them apart.

```json
{"type":"order_snapshot","payload":{
  "account":"Sim101",
  "build_id":"2026-09-03-f12",
  "emitted_at_ms":1788480000000,
  "reason":"periodic|state_change",
  "orders":[
    {"order_id":"NT-1","name":"VL-S1-entry","action":"buy|sell",
     "type":"limit|stop|stop_limit|market","limit_price":29450.25,"stop_price":0,
     "quantity":1,"filled":0,"state":"Working","oco":"oco-1","symbol":"MNQ"}
  ]}}
```

Terminal orders (`Filled`, `Cancelled`, `Rejected`, `Expired`, `Unknown`) are
omitted by the AddOn — sending the whole history every 30 s would grow without
bound. The Go side filters again; **one definition of "working" lives in Go**
(`NT8Order.IsWorking`), so the two cannot drift on what the word means.

**STALENESS IS A SHARED CONSTANT.** The Go side calls a book older than
**2 × 30 s** stale (`trader.DefaultOrderSnapshotSecs`, env
`NT8_ORDER_SNAPSHOT_SECS`). The AddOn's `ORDER_SNAPSHOT_INTERVAL_MS` must match.
Changing one without the other changes what "stale" means on only one side.

**Age is measured against RECEIPT, not `emitted_at_ms`.** Those are two machines'
clocks, and a Windows-side skew must never make a stale book look fresh.

### build_id on `hello` and `heartbeat`

`hello` now carries `build_id` alongside `protocol_version` and `source`
(`omitempty` — an older AddOn's wire stays byte-identical), so the running DLL is
identifiable from the FIRST frame. `heartbeat` has carried it since E7.

**This is the only honest answer to "which DLL is NT8 running".** `VL_BUILD_ID` in
this repo is what we INTEND to be running; NT8 keeps executing whatever was last
compiled with F5. The Go boot line prints
`build_id=<received> expected=<source> match=yes|NO` and says **NO** until a frame
proves otherwise — a change to a distributed system is proven by a received
frame, never by a ledger write on the sending side.

### Go receive extension: placement truth (2026-09-07)

`fill` and `order_update` accept optional `reason` (JSON string). On an entry
rejection Go stores that text verbatim. The deployed `2026-09-07-h1` AddOn
**does not emit this field** for these frames; Go records
`reason unavailable (NT8 frame omitted reason)` when it is missing/blank.
The C# producer is explicitly deferred to the next owner-run AddOn wave.
This additive receive-only extension does not change the protocol version or
claim that h1 supplies rejection reasons.

### `order_update` cancel-report echo (2026-10-03-c1) — AddOn → Go

The cancel-report regime (`CANCEL_CONFIRM_REQUIRE_REPORT`, default OFF) settles
a cancel ONLY on a POSITIVE per-order terminal report. The `order_snapshot`
deliberately omits terminal orders (Filled/Cancelled/Rejected/Expired are
history — C# D3, 2026-09-07), so absence from a book cannot distinguish
"cancelled" from "never answered".

From build `2026-10-03-c1` the AddOn emits, on EVERY `cancel_order` receipt, an
`order_update` frame for the target order whose payload carries the new field:

| field | type | meaning |
|---|---|---|
| `cancel_report` | bool | `true` marks this frame as the cancel-request echo, as distinct from a state-change event |

The echo carries the order's CURRENT state (`state` field, unchanged semantics):
for an order still resting that is `working`/`accepted`; for one already
terminal it is `filled`/`cancelled`/`rejected`. The real terminal transition
follows via the normal `OnOrderUpdate` stream, which continues to emit
`state=cancelled` etc. The echo BYPASSES the per-order-name state dedupe — it
is the report the gate waits for, not a state-change event.

**Fail-closed by construction.** If the target order is not found in the
account's order collection (never placed, or purged), the AddOn emits NOTHING.
The Go side then records no report, the row stays `cancel_pending`, the slot
stays BUSY, and a timeout prints the owner-visible census WARN. An absence the
AddOn cannot explain is never dressed up as a report.

Go-side gates, all keyed on the build id (bytewise date-prefix floor, same rule
as every other capability):

- `MinAddonBuildCancelReport = "2026-10-03-c1"` (`provider/ninjatrader/tcp_framing.go`)
- an AddOn below the floor, while the regime is ON: no cancel confirms, no
  re-arm, no stop-entry placement — fail-closed, owner-visible WARN.
- the regime is OFF by default; with it OFF this frame is received and ignored
  (the field is additive), and the bot is byte-identical to today.


Entry `signal.timestamp` is UTC command creation time (RFC3339 with fractional
seconds), independent of the market bar close used to compose entry prices.
Go checks that payload timestamp before enqueue and again immediately before
writing, including reconnect/retry. Missing, invalid, future, or older-than-60s
payload clocks are refused with the measured age (or age unavailable) logged.
Retries retain the original timestamp. This does not make stale market data
fresh or bypass any existing data/entry gates.

The armed ledger registers `signal_id` and `place_pending` atomically before
sending. Only a received live entry `order_update` or fresh `order_snapshot` naming
that exact entry promotes to `working`;
a received entry rejection (`fill.status` or `order_update.state`) settles as
`rejected`. Protective-leg updates cannot promote or reject the entry row.
A local socket return is not broker acceptance. Unanswered placements retain
their pending state and slot; a queue-age refusal is logged, never presented as
an NT8 rejection or silently re-authorized as another placement.

## Installation maintenance hold (W-ONE-BUTTON M2, 2026-09-22) — build `2026-09-22-m2`

A partner update holds the whole installation (`data/updater/hold.json`) while it
drains. Go refuses new entries on every path first; these two frames are the
AddOn's half. **Additive; protocol version unchanged (3).**

### `maintenance` (Go server → C# AddOn)

```json
{ "type": "maintenance", "payload": { "held": true, "job_id": "upd-2026-09-22-01" } }
```

- Sent **only while a hold is present**: at accept (after the account allowlist,
  **before** the queued-signal flush), on change, and **re-sent every 5 s** so each
  ack carries a fresh census. When the hold clears, **one** `{"held": false}` goes to
  a connection that was told it was held. **With no hold file nothing is sent**, so
  the wire of an installation that never updates is byte-identical to before M2.
- `job_id` is omitted when released. A corrupt hold file is sent as held with no
  `job_id` (fail-closed).
- AddOn: while held, `HandleSignal` **refuses every new entry** with the existing
  `fill status=rejected` frame. It never touches protection, brackets on fill,
  part-fill amends, flatten, cancel or modify. A `maintenance` frame without a
  boolean `held` is treated as **held**. The AddOn resets to not-held on every
  (re)connect: a hold belongs to the connection it was sent on, and Go re-sends it
  at accept.

### `maintenance_ack` (C# AddOn → Go server)

```json
{ "type": "maintenance_ack", "payload": {
    "held": true, "job_id": "upd-2026-09-22-01", "queued_commands": 0,
    "build_id": "2026-09-23-m21",
    "connections": [ { "sim": true,  "connected": true,  "settled": true },
                     { "sim": false, "connected": false, "settled": true } ],
    "accounts":    [ { "sim": true, "positions": 0, "working": 0 } ] } }
```

- Sent in answer to **every** `maintenance` frame.
- **No names, ever.** Flags and counts only: this repo is public and Go logs the ack.
- `connections[]`: every connection in `Connection.Connections`. `sim` is true only
  when **every** account seen on that connection is a SIM account (`IsSimAccount`);
  a connection with no account seen reads **non-SIM** (fail-closed). `connected` is
  `Status == ConnectionStatus.Connected`. `settled` (M2.1) is `Status` being Connected
  or Disconnected; any other state (Connecting, ConnectionLost …) cannot vouch for
  the zeros reported for its accounts, and the gate fails on it. An ack without
  `settled` (an older build) reads false: fail-closed.
- `accounts[]`: every account in `Account.All`. `positions` counts non-flat
  positions. `working` counts orders **of any action** in any state except
  Filled / Cancelled / Rejected (Unknown counts as working): a resting exit can
  reverse a flat account, so exits and protection are counted too.
- `census_error` (with `connections` / `accounts` **absent**) when the AddOn could not
  take the census. It carries the exception type only, never the message, which could
  carry an account name. Go never reads an absent list as an empty one.
- `queued_commands`: the AddOn executes frames synchronously on its read thread, so
  when the ack is written every earlier frame has already run. The depth is **0 by
  construction**. A future asynchronous dispatcher must report its real depth.
- The census takes each NT8 collection under **its own** lock and counts outside it; it never
  holds one lock while taking another, because it runs on the read thread that also carries
  close and protective frames (M2.1).
- Go records the ack on the **current connection's record only**. A reconnect
  starts a fresh record, so connection N's ack is never reported for N+1.

**What Go's installation gate does with it (CTO ruling Q1):** it fails on
- no ack for the current connection, or a stale one;
- an ack for another job;
- a census error or an absent list;
- **any connected non-SIM connection**;
- **any position or working order on any account.**

An AddOn older than `2026-09-22-m2` never acks, so the gate reads `addon_ack: n/a` and **fails**.

### `hello` epoch fields (CTO ruling Q3)

`hello` (AddOn → Go) gains additive, `omitempty` fields that identify **this
activation**, so an update verifier can bind "the new build is running" to
evidence rather than to the hand-set `VL_BUILD_ID` (M1 finding F4):

| field | meaning |
|---|---|
| `nt8_pid` | `NinjaTrader.exe` process id |
| `nt8_start_ms` | that process's start time, Unix ms UTC |
| `assembly_mvid` | module version id of the loaded NinjaScript assembly; **changes on every compile** |
| `source_hash` | SHA-256 of `AddOns\VLTraderTCPClient.cs`, taken when the AddOn **activates** (`State.Active`), not at the first hello, so a copy made before the next F5 cannot change it (M2.1). Omitted when unreadable |
| `activation_nonce` | minted once per AddOn activation |

A value the AddOn cannot read is **left out**, never guessed. The Go reply sets none of them; its bytes are `{"protocol_version":3,"source":"vl-go"}`, pinned by `TestGoHelloReplyIsByteIdentical` (`provider/ninjatrader/maintenance_wire_test.go:270`). Go stores each connection's hello, together with
its `accept_seq`, monotonic accept time and `remote_port`, as that connection's record.
The verifier binds to that record, **never** to `FarSideBuildID()`. `VL_BUILD_ID` keeps
its ISO-date prefix, because the capability floors compare it bytewise.

## `signal.stop_limit` (2026-10-03-c2) — mentor stop-LIMIT entries (PR B)

The `signal` payload gains `stop_limit` (bool, omitempty). When true and
`order_type` is `stop_entry`, the AddOn builds `OrderType.StopLimit` with
`LimitPrice == StopPrice` — the entry fills at its price or misses, never a
stop-MARKET (D1.4 p1 @24:41, p2 @00:00). Go sets the flag for EVERY
mentor-origin stop entry (since 2026-10-04 no environment variable gates it) and
only when the far side proves `MinAddonBuildStopLimit` = `2026-10-03-c2`
(fail-closed: an older AddOn would build StopMarket and fill sloppily, so the
arm is refused instead). A non-mentor stop entry keeps the byte-identical
stop-market wire.

N12: with limit == stop and Day time-in-force, a gap through the trigger leaves
a RESTING limit that can fill later at a stale price. Go closes that window
with a per-order EXPIRY, authored by the mentor evaluator's intent, not a
blanket timer: armed_orders.expiry_ms will be stamped when the mentor injector
lands (DS-102, #316 — a level touch or a single ISB expires at the close of
the NEXT 1m candle; ISB stacking is extended while the candles stay inside and
cancelled at the 4th; the swing runs its 5m rule). Stop-limit + expiry apply
to MENTOR-MODE arms ONLY, and the discriminator is the arm's EXPLICIT origin
(armed_orders.origin = 'mentor', stamped by the mentor injector), never the
expiry as a proxy: mentor + knob ON + expiry_ms > 0 routes the stop entry
through the limit variant; mentor + knob ON + no expiry is REFUSED fail-closed
with a reason + counter and NO stop-market fallback; a non-mentor arm takes
today's stop-market path whatever its expiry, and the knob OFF is that same
path for everyone. At now >= expiry_ms the
armed pass sends the cancel_order frame on the SAME pass (an expired arm that
was never placed — armed, no signal id — ends terminal 'expired' in the
ledger); a row with no expiry is never auto-cancelled. The AddOn needs no new
frame for this.

## Split legs (REVIEW-353 / REVIEW-SPLIT-2, wire v4) — build `2026-10-04-d1`

One mentor entry of n contracts takes half off at its own target and runs the
rest. It stays ONE entry order and ONE signal id; the AddOn builds TWO OCO
pairs on the fill. Go sends the split only when the far side has proven
`MinAddonBuildSplitLegs` (`2026-10-04-d1`); an older AddOn gets the single
bracket, byte-identical.

**`signal` (Go → AddOn), additive:**

- `leg1_qty` (int): leg 1's contracts. Absent/0 = the single bracket. The
  runner (leg 2) is `filled − leg1_qty`, so Go sends `leg1_qty = n − runner`.
  With the spent-day cap (runner ≤ 2), the contracts the cap removes from the
  runner go to leg 1. The AddOn builds the leg-2 pair only once the fill
  exceeds `leg1_qty`.
- `leg1_tp` (float): leg 1's own take-profit, rounded to the nearest tick by
  Go and on the profit side of the entry. A leg-1 TP that fails either check
  drops the split; the single bracket is sent instead.

**Order names under the one signal id:** leg 1 = `<sid>-sl` / `<sid>-tp`
(one OCO pair), leg 2 = `<sid>-sl2` / `<sid>-tp2` (a second OCO pair).

**`move_stop` / `modify_bracket` (Go → AddOn), additive:**

- `leg` (int): 1 = leg 1 only, 2 = leg 2 only, absent/0 = every live leg.
- A terminal leg (e.g. leg 1 after its TP filled) is skipped. The frame errors
  only when no selected leg is live.
- Go's stop-widen ban is judged per leg (`<sid>#leg1` / `<sid>#leg2`).

**`position_close` (AddOn → Go), additive:**

- `leg` (int): which leg exited — 1 (`-sl`/`-tp`) or 2 (`-sl2`/`-tp2`). 0 or
  absent means the whole position (manual close, flatten, or a single bracket).
- Go adds the leg to the exit-receipt identity only when it is > 0. Two
  same-time stop exits of equal size (2+2) then stay two receipts, and a
  single-bracket receipt key is unchanged from before the split.
- The exit-drive record for the signal is kept while either leg is live; it
  is dropped when the last leg exits.
