# DS-104 — minimal fix proposal (for DS-103/DS-106 review; NO production edit made here)

## Defect (source-verified [A], repo `359435ab`)

`VLTraderTCPClient.OnVLConnectionStatusUpdate` is subscribed to the **static**
`Connection.ConnectionStatusUpdate` (fires for every NT8 connection) but:

1. tracks loss in a single shared `bool dataFeedWasLost` — any connection's
   `Disconnected`/`ConnectionLost` sets it;
2. fires `barsManager.OnConnectionReconnected()` (dispose + recreate ALL BarsRequests)
   on **any** connection's `Connected` transition, because the handler never reads `sender`;
3. uses one global `RecreateDebounce=30s` / `lastRecreateUtc`, so an unrelated connection's
   flap consumes the window and can **suppress a genuine desired-feed recovery**.

A second-order effect: when a recovery is debounced, `dataFeedWasLost` is already cleared
inside the lock, so the desired feed is left dead until the slow 75-min stall watchdog.

## Proposal (minimal, provider-agnostic — does NOT select/change owner's provider or accounts)

Key the loss state and the debounce by the **connection identity that raised the event**
(the `sender` object — the `Connection` instance NT8 hands us), never by a hardcoded name.

1. Replace `bool dataFeedWasLost` with a per-connection set, e.g.
   `HashSet<object> lostConnections` (guarded by `connStatusLock`).
2. Replace `DateTime lastRecreateUtc` with `Dictionary<object, DateTime> lastRecreateUtc`
   (keyed by the same `sender`).
3. `Disconnected`/`ConnectionLost` → `lostConnections.Add(sender)`.
4. `Connected` → fire recreate only when **that same `sender`** is in `lostConnections`
   (genuine same-connection recovery) or `prevPriceStatus != Connected` (fresh connect);
   then `lostConnections.Remove(sender)` and apply the debounce **keyed by `sender`**.
5. Do NOT clear the loss flag when the debounce suppresses the recreate — leave the connection
   in the lost set so its next `Connected` edge can still recover (fixes the second-order bug).

### What this preserves
- Real recovery: the connection that lost data and then reconnects still triggers the
  full BarsRequest rebuild (same as today, minus cross-connection crosstalk).
- SIM-only behavior and account bindings: untouched. No provider name is referenced.

### What it does NOT fix (explicit boundary)
- A connection that flaps on its **own** (e.g., recurring blunox LogOnFailed) still recreates
  all BarsRequests when it recovers, because the AddOn does not know which connection carries
  the MNQ subscription. Eliminating that churn requires instrument→connection mapping and is
  out of minimal scope — recommend a separate DS-103 evaluation.

## Verification path
- Run `ConnectionStatusRepro` (this folder) on Windows: it should show Scenario A (unrelated
  flap → recreate) under the CURRENT code, and after the fix show Scenario A → no recreate for
  the unrelated identity, while a same-identity lost→recovered still recreates.
