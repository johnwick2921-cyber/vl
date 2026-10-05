# G1/G2 fed from real broker fills — draft design (no code)

Status: DRAFT — follow-up to the X-07 review (2026-10-05). Owner: DS-104 (offered).
Base: origin/dev @ c28fa23db. Not for the current install.

## Problem

The G1 leg budget and the G2 loss box live in `kernel/mentor/limits.go` and are
currently fed by SIMULATED pending fills: `Limits.Apply` registers a `pendOrder`
for every entry intent the evaluator EMITS, then `simulate` materializes a fill
when a later candle crosses the entry. Because the pend is registered at
EMIT time — before the trader actually places the order — every trader-side
refusal must REVERSE it. X-07 (#401) added `Limits.DropArm` and a deferred guard
in `mentorDispatchEntry`, but that only reaches the injector's own refusal paths.
The armed pass runs on the executor goroutine and refuses AFTER
`mentorRegisterLiveArm` (N4 wire R:R, `mentor_no_contracts`, `addon_build`,
`far_side_unproven`, `maintenance_hold`), and it has no safe way to drop the pend
(the sim lives under `mentorEvalMu`). Result: a refused entry phantom-fills on a
later candle, spending the leg budget / opening a loss box for an order that
never existed. Today every such case only makes the sim MORE conservative
(fewer entries), never riskier — but it is wrong bookkeeping and the class keeps
reopening as new refusal paths are added.

## Goal

Feed G1/G2 from REAL broker fills, not simulated pends. The trader's fill
callback calls `Limits.RecordFill(...)` with the actual fill evidence (ArmID,
side, entry, stop, target, isISB). A fill that never happened can then never
spend the budget, by construction — the whole phantom-fill class closes.

## Design

1. **`Limits.RecordFill(armID string, side Side, entry, stop, target float64, isISB bool, levels []Level)`** — a new method that performs the same registration `simulate`'s fill branch does today (`registerLeg` + open a `Place` band at the real entry/stop), keyed by the real fill. Idempotent per fill receipt (dedup by a deterministic receipt id, the same pattern `close_sync` uses for exits).

2. **Stop pre-registering pends for mentor entry intents.** `Limits.Apply` keeps its G2 departure + block logic, but the `pendOrder` fill simulation for mentor-origin entries is removed (or made a no-op). The swing stays exempt as today. If any non-mentor path still relies on simulated fills, keep that path isolated behind the existing origin flag.

3. **Fill → evaluator bridge with a queued drain.** The fill callback runs on the executor goroutine; the sim lives under `mentorEvalMu`. The callback must NOT touch `Limits` directly. It enqueues a fill record into a bounded channel; `mentorEvalOnce` drains the queue under `mentorEvalMu` and applies `Limits.RecordFill` in order. (This is the "queued drain inside mentorEvalOnce" the CTO named; it is the only lock-safe seam.)

4. **Arm identity.** The fill carries the signalID; the evaluator's arms are keyed by the evaluator ArmID (`isb-N` / `lvl-N`). Reuse the existing `mentorLiveArms` mapping (ArmID → row) to resolve the fill's ArmID and its side/entry/stop. A fill whose ArmID is unknown in this process (a restart, or a foreign arm) is DROPPED, never fabricated — fail-closed.

5. **Loss-box band from the real fill.** `RecordFill` computes the place key (`isbBandKey` for ISBs, the level/EMA anchor otherwise) and the prior swing from the ACTUAL entry and the current `levels`, exactly as `registerLeg`/`normalizePlace` do today, so the boxed band reflects where the fill really happened.

6. **Ordering / replay.** Fills are applied in arrival order, once each. A partial fill registers the partial qty; the cumulative total is what matters, so a partial-then-full sequence registers the full qty exactly once (the dedup is on the receipt, not the cumulative). A fill that arrives before its entry row is visible (the R6-style case close_sync already handles) is PARKED and re-applied when the row catches up.

7. **Rollout.** Keep X-07's `DropArm` guard as belt-and-suspenders until the real-fill path is proven in SIM, then remove the simulated pends entirely. No live-enable until an owner-attended SIM proof.

## Call-site pins (for the implementation wave)

- A real fill registers a leg / loss-box entry EXACTLY once (a retransmitted fill does not double-register).
- A fill for an unknown ArmID is dropped and counted, never fabricated.
- A refused entry (injector OR armed-pass refusal) produces NO leg / loss-box entry — the class this closes, now true for every refusal path.
- A partial then full fill registers the cumulative qty once, with the band at the real entry/stop.
- Swing fills are never fed to G1/G2 (exempt).

## Open questions

- Does the NT8 fill path (executor goroutine) already carry the evaluator ArmID, or only the signalID? Confirm the signalID → ArmID resolution survives a restart (currently `mentorLiveArms` is memory-only; the arm epoch resets on reload — N1). May need the ArmID stamped on the wire frame, or a persisted signalID→scenario row.
- The exit-drive "flat read" unregister and the leg reset (`resetBreaks`) must consume the same real-fill ledger, not the sim, so a leg reset happens on a real close, not a simulated one.
