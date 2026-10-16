# Swing-4h-trail — redesign the SWING exit per the course (dispatch, owner order 2026-10-16)

- **Lane:** DS-101 · **Branch:** `fix/swing-4h-trail` · **Base:** origin/dev AFTER DS-104 lands the #559 swing revert (this doc is written ahead of that; the branch will be rebased).
- **Inputs (gated reports):** `w30-559-GATE-REPORT.md`, `w30-559-REGATE-REPORT.md`, `w30-559-GATE3-REPORT.md`. The defects below are numbered from GATE3.
- **Course (raw transcripts, re-read by the gate):**
  - X8 @16:02–18:09 — *"Cứ mỗi cây nến 2 phút đóng xong … em sẽ kéo stop đặt ở dưới đáy cây nến 2 phút đó … Nếu như mình trade khung nào thì mình gồng theo khung đó, theo cây nến của khung đó"* — the general rule: after entry, move the stop behind each CLOSED candle **of the frame the trade is held on**. @17:28–17:37 the trail runs on candles AFTER the entry.
  - D5.2 p1 @05:48–06:18 — the swing is held on the **4h** frame: *"em gồng theo khung 4 giờ … chỉ cần 1 cây nến 4 giờ thôi"*.
  - D5.2 p2 @01:40–01:48 / @06:14–06:23 / @11:17–11:24 — *"target đầu tiên là EMA 34 của 05 phút … đừng vô 1 MNQ … 2 … để có đường bắn bớt"* / *"target cứ at least là 1 1 trước … hoặc là EMA 34 cũng 5 phút, còn hơn nữa chạy"* — the swing splits: sell part at the first target / +1R, run the rest.
  - D1.2 p1 @12:34–13:12 / @15:56–15:59 — *"Risk reward … lúc nào cũng phải maintain … ở mức 1-1 … em phải kéo stop … về break even"* — BE at a **1R run**, universal.
- **Ruling (CTO dispatch):** after the first target / +1R break-even, trail the runner behind each CLOSED 4h candle AFTER the entry, never widening, never wrong side of the market; Friday flat before the weekend close using the CME holiday calendar (2026-11-27 and 2026-12-24 close 12:15 CT); the swing is cleared ONLY on a broker-confirmed close (never on a read error, never by re-reading the fill bar against a moved stop); `close_position` only when the broker still holds the swing (an AddOn `close_position` flattens the WHOLE account).

## State machine (target)

1. **Fill** — a swing fills (stop entry through the swing line). Record `FillBarTime` at the production fill writer.
2. **Break-even at +1R** — on the first 5m close at or beyond +1R from the entry (the course's 1:1 rule), move the stop to the entry. Do NOT clear anything yet.
3. **Trail behind closed 4h candles AFTER the entry** — after BE, on each 4h candle that CLOSED after the entry, move the runner stop to that candle's low (long) / high (short), but:
   - **never widen** — only ever tighten vs the current stop;
   - **never wrong side** — a stop must stay on the risk side of the market (below the last close for a long, above for a short).
   - The first 4h candle considered is the one that closed after the entry; pre-entry 4h candles are NEVER walked (the re-gate P0 fix).
4. **Friday/weekend flat** — before the weekend close, using the **CME holiday calendar** (the session calendar the EOD flat already reads), flat the swing; the flat is **re-emitted until the broker confirms the close**, and it uses the EARLY close time (12:15 CT) on 2026-11-27 (Fri) and 2026-12-24 (Thu; 12-25 closed).
5. **Clear** — the swing leaves `SwingState.Pos` ONLY on a broker-CONFIRMED close. Never on a read error (`known=false` → refuse the move, keep `Pos`), never by re-reading the fill bar against a moved stop.

## Defects this dispatch closes (GATE3 numbering), each with a production-call-site pin + anchored mutant

### D1 (P0) — the stop-trade scan clears an OPEN swing one tick after BE / after every trail move
- **Root cause:** the scan runs over bars `OpenTime >= FillBarTime` against the CURRENT `p.Stop`. After BE `p.Stop = p.Entry` and the fill bar (a stop entry) traded through the entry by construction → cleared. After a trail move, the bar that made the new low has `Low == p.Stop` → cleared.
- **Fix:** the scan reads only bars that CLOSED AFTER the stop was SET — a `StopSince` watermark set at BE and at each trail step, compared against the bar's close time.
- **Pin:** `Evaluator.Tick` with a REALISTIC fill bar (a buy-stop fill bar whose low is through the entry) → BE → the next tick must NOT clear `Pos`, and the trail must keep firing.
- **Mutant:** `StopSince` dropped (back to `FillBarTime`) → RED.

### D2 (P1) — a FAILED broker read clears an open swing
- **Root cause:** the BE/trail drive does `if open, known := mentorSwingBrokerOpen(side); !known || !open { … mentorClearSwingPos }` — a `GetPositions` error (`known=false`) clears the swing.
- **Fix:** `known=false` → refuse the move, KEEP `Pos` (the drive's own "a read error proves nothing"). Only `known=true && !open` (broker provably flat) clears.
- **Pin:** `GetPositions` fails at the +1R tick → the move is refused, `Pos` stays, and the trail/Friday-flat still run later.
- **Mutant:** `!known || !open` (read error still clears) → RED.

### D3 (P1) — required pins restored / wrong pins corrected / impossible fixtures replaced
- **Restore:** FDR (c) asserts the first move = BE at the entry price + time; FDR (c2) restores the restart guard; the `runSwing` wiring guard in `eval_test.go`.
- **Fix the wrong pin:** FDR (c) must NOT forbid `price > entry` (a correct LONG trail is always above the entry). It pins the BE price + the first trail move's time, and that every trail move is ≥ the previous stop (never widen) and on the risk side of the market.
- **Replace impossible fill-bar fixtures:** a buy-stop fill bar must have `Low <= entry` (it traded through the entry), not `Low > entry`.
- **New pins (each with an anchored mutant):**
  - S3 never-widen (`swing4h.go` trail) — a widening candidate is skipped.
  - N5 BE leaves `p.Stop = p.Entry` (so D1's scan has a real stop to read).
  - N6 short-slice management (fewer than 2 closed 4h buckets) still manages the swing.
  - N7 the marketable-TP entry clamp (`mentor_trail_r22.go` `mentorClampMarketableTP`) on both sides.
  - N12 CONFIRMED writes the belief (so a later candle can re-modify).
  - N13 the Friday close clears `Pos` only on the broker-confirmed close.
  - N14 `FillBarTime` recorded at the production fill writer (`swing4h.go` fill path).

### D4 (P2) — Friday flat clears on SUBMIT, not confirmed; holiday weeks never reach Friday 15:55
- **Fix (a):** the swing flat time is resolved from the CME holiday calendar — on a day whose session close is EARLIER than 16:00 CT (2026-11-27 Fri 12:15; 2026-12-24 Thu 12:15, 12-25 closed), the flat fires before that early close. Reuse the EOD flat's calendar read (`trader/auto_trader_clock.go` `effectiveEODFlatCT`).
- **Fix (b):** the swing is cleared only on the broker-CONFIRMED close — the close is re-emitted until the broker book no longer shows the swing position; "frame submitted" is not a close.
- **Pin:** a swing held into a 12:15-CT early-close day → a flat fires before 12:15; and a refused/failed flat is retried (not cleared).
- **Mutant:** fixed `Friday 15:55` (no calendar) → RED.

### D5 (P2) — a closed swing the kernel still holds → Friday flat sends a full-account Flatten
- **Fix:** `mentorClosePosition` gains the same broker side-open check the moves already have — it sends `close_position` ONLY when the broker still holds the swing on that side. A provably-flat side → clear the swing without a wire close (no Flatten).
- **Pin:** side flat at the broker → NO `close_position` frame, `Pos` cleared. Side open → exactly one close.
- **Mutant:** the side-open check dropped → RED.

### D6 (P2) — the trader's never-widen reads the ENTRY stop (never updated after a move)
- **Fix:** the trader move guard judges against the CURRENT stop (`SwingState.Pos.Stop`), not the arm row's `StopPx` (which is never updated).
- **Pin:** after a trail move, a widening candidate back toward the entry stop is refused.
- **Mutant:** guard reads `r.StopPx` again → RED.

### D7 (P2) — merged interaction: an open swing blocks every mentor entry (product call)
- **Out of scope for this dispatch's code** — it is dev #560's NO-FLIP × the removed time exit. The guide must say it (an open swing holds the mentor flat until it exits). Recorded for the CTO/owner; no code change here beyond the guide sentence.

### D8 (P2) — CONFIRMED verdict re-fires every candle (log/counter lie)
- **Fix:** `confirm` clears the sent-marker while keeping `TPModWant`, so a later in-band candle re-modifies ONCE per new close, and the `modify_bracket_confirmed` counter counts confirms, not re-prints.

### D9 (P2) — guide + code text
- Guide SWING4H row: trail behind closed 4h candles AFTER the entry, never widening, never wrong side; Friday flat via the holiday calendar, retried until confirmed.
- Code comments: `swing4h.go` (hold-close / EOD-flat text), `mentor_live_pos.go`, `mentor_actions.go:848`, `mentor_course_exits.go`, `eval_test.go:32`, FDR subtest names.

### D10 (P2) — gofmt
- `gofmt` the touched files (CI's fmt step is continue-on-error; the PR must not rely on it).

## Acceptance (the CTO's list, mapped)

| CTO acceptance phrase | Defect | Production pin |
|---|---|---|
| P0 cleared after BE | D1 | `Evaluator.Tick` realistic fill bar → BE → next tick keeps `Pos` |
| ghost swing | D5 / D6 | side-flat check on the Friday close; never-widen reads the current stop |
| failed read | D2 | `GetPositions` error at BE → refuse, keep `Pos` |
| ghost close | D5 | no `close_position` when the broker is already flat |
| impossible fill-bar fixtures | D3 | buy-stop fill bar `Low <= entry` |

## Test plan (files)

- `kernel/mentor/swing4h_test.go` — D1 (BE-then-tick keeps Pos, realistic fill bar), D3 (trail post-entry, never-widen, short-slice, FillBarTime writer), D4 (calendar flat).
- `trader/mentor_actions_test.go` — D2 (read error keeps Pos), D5 (side-flat → no close, side-open → one close), D6 (never-widen reads current stop), N13 (Friday close clears only on confirmed close).
- `trader/full_day_replay_test.go` — restore FDR (c) BE price/time pin + (c2) restart guard; fix the wrong `price > entry` pin.
- `trader/mentor_r22_exit_test.go` / `mentor_exit_drive_test.go` — N7 clamp, N12 CONFIRMED belief (already-owned R1 pins kept green).
- `web/src/guide/content/mentor.ts` — SWING4H row + the no-flip sentence (D7).

## Sequencing

1. (this turn) design + this doc + the branch claim. No code.
2. DS-104 lands the #559 swing revert on dev.
3. Rebase `fix/swing-4h-trail` onto the new dev, port the pins, implement D1–D10.
4. Gates: CT + UTC on `kernel/mentor`, `trader`, `trader/ninjatrader`; `-race` on touched; FULL_DAY_REPLAY base vs head (the injected swing and the kernel swing both trail correctly, no ghost moves); tsc + whole vitest.
5. PR to dev; post URL + head + mutant REDs.
