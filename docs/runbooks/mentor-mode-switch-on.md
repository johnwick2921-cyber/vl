# Mentor mode — switch-on runbook (SIM)

How to turn the mentor mode ON for one trader, verify it is really ready, and
turn it OFF again. Every string in this file is the real log text or knob key
from the code (branch `feat/mentor-p3`, PR #316).

## 1. Preconditions

1. The Mentor bundle is installed: the running binary is the bundle build that
   carries `feat/mentor-eval`, `feat/mentor-p3`, and the stop-limit frame
   (`release/2026-10-03-2` per the CTO bundle plan).
2. The NT8 AddOn is compiled (F5) with `VL_BUILD_ID = 2026-10-03-c2`, and the
   boot line shows a match:
   ```
   nt8 addon: build_id=2026-10-03-c2 expected=2026-10-03-c2 match=yes
   ```
   (the exact line is `AddonBuildLine` in `provider/ninjatrader/order_snapshot.go`).
   If it says `match=NO` (or "no frame carrying a build_id received yet"): the
   AddOn running inside NT8 is the wrong build — re-copy the `.cs`, F5, full NT8
   restart.
3. The trader is on the NinjaTrader venue with a SIM account bound. The boot
   line for the venue is:
   ```
   🏦 [<name>] Using NinjaTrader (transport via NT_TRANSPORT env, CME futures via SIM)
   ```
   `isAccountTradeable` blocks non-SIM accounts — mentor mode never routes to a
   live account. Do not try to bypass it.

## 2. The switch

The knob key is `mentor_mode` (per-strategy, in the strategy's
`risk_control` config, JSON field `mentor_mode` — `store.RiskControlConfig`).
Default is OFF.

- In the UI: Strategy Studio → the strategy's Risk Control editor
  (`web/src/components/strategy/RiskControlEditor.tsx`). The mentor card ships
  with the guide wave (`feat/mentor-guide`); until that card is in your build,
  set `"mentor_mode": true` in the strategy's `risk_control` JSON directly.
- Placements are additionally gated by the env knob `MENTOR_PLACE` (real gate:
  `mentorPlaceEnv`, `trader/mentor_tick.go`). Until it is set, mentor entries
  are sized, logged and counted but NOT sent. The log you will see instead:
  ```
  🧑‍🏫 mentor intent SIZED, NOT PLACED (MENTOR_PLACE env off — set MENTOR_PLACE=1 to place): ...
  ```

  **Owner step — setting `MENTOR_PLACE=1`.** The `.env` is never edited by a
  lane. The CTO drafts the exact `.env` line; the owner pastes it into `.env`
  and restarts the bot (the update button, or the owner's own restart). After
  the restart, `mentorPlaceEnv` reads the env knob (`1`, `true`, `on`, `yes`
  all mean ON) and placements go live — SIM only, see precondition 3.

## 3. Boot / seed lines — is it really ready?

At trader start a mentor-mode trader loads the stored 1m + 1h bars (read-only;
`data.db` is never written), seeds the evaluator, and prints the seed boot line:

```
🧑‍🏫 mentor seed: 4h EMA34 108/102 1m EMA34 201/102 1H RTH levels 124 candles levels 10
```

Each number is a real depth against its floor (from `mentor.SeedLine`,
`kernel/mentor/seed.go`):
- `4h EMA34 n/102` — closed 4h candles (17:00 CT anchor) for the 4h EMA 34.
- `1m EMA34 n/102` — closed 1m bars for the 1m EMA 34.
- `1H RTH levels n candles` — 1H RTH candles in the level history.
- `levels n` — key levels drawn from that history.

If anything is missing, the same boot prints a refusal line naming each short
source and which entry kinds it blocks:

```
🧑‍🏫 mentor seed REFUSING SWING4H entries only (intraday entries allowed) — missing: bar history depth: 4h EMA34 warm-up (94/102 4h candles) [blocks SWING4H entries only]
🧑‍🏫 mentor seed REFUSING ALL entries — missing: bar history depth: 1m EMA34 warm-up (50/102 1m bars) [blocks ALL entries]; ...
```

The 4h EMA 34 feeds ONLY the swing line (`State.Swing.Line`), so a short 4h EMA
blocks SWING4H entries only (counter `seed_missing_4h_ema_warmup`); the
intraday setups (ISB, PHL/PLH, box, level touches) read the 1m EMA 34 and the
1H level set and keep trading. Every other missing source blocks ALL entries
(counter `seed_missing_source`). Cancels always pass.

The missing-source strings are exactly these (from `mentor.SeedMissing`):
- `bar history depth: 4h EMA34 warm-up (n/102 4h candles)` — not enough 4h
  history. Wait for bars to accumulate (the stored 1m/1h tape grows as NT8
  streams). Never trade on a cold EMA: every entry is refused while this is
  missing.
- `bar history depth: 1m EMA34 warm-up (n/102 1m bars)` — same for the 1m.
- `bar history depth: 1H RTH level history (n candles)` — fewer than 2 RTH
  1h candles: no level set exists yet.
- `bar history depth: today's session` — no 1m bar of the current CT day yet.
- `bar history depth: closed 15m candle (n)` — no closed 15m candle yet.

While any of these is missing, the evaluator refuses the entries it blocks
(fail-closed, `scopedFailClosedFilter` in `kernel/mentor/seed.go`, called from
`Tick`) AND the injector refuses at placement naming the short source
(`mentorSourcesBlocking`, `history: <name> (n/min)`). Do not bypass either;
wait for the tape.

No restart is needed: the depth is re-checked after every tick from the
evaluator's own state (`advanceDepth` in `kernel/mentor/seed.go`,
`mentorRefreshDepths` in `trader/mentor_tick.go`). Each observed 4h rollover
adds one closed 4h candle, and when every source meets its floor the trader
logs ONE line and entries are allowed:

```
🧑‍🏫 mentor seed: depth met — entries allowed
```

The floors are never lowered (102 closed 4h candles, 102 closed 1m bars).

## 4. First-session checks

With `MENTOR_PLACE=1` and the seed line complete, verify these in order:

1. **First arm carries its expiry.** Every mentor placement logs:
   ```
   🧑‍🏫 mentor placed: <setup> <side> <n> contracts (tier <tier>) — close→ack <ms>ms, expiry <ms>
   ```
   The `expiry` field must be non-zero (the injector always stamps
   `ExpiryMs`). An arm without an expiry is refused:
   `mentor placement REFUSED — F3: a mentor stop-limit arm without an expiry is refused ...`
   (guard: `mentorExpiryGuard`). The exit branch is chosen at entry and logged
   on the same path:
   ```
   🧑‍🏫 mentor exit fork: <B|C|swing> — <why> (leg 1 TP <px>)
   ```
2. **The ORB is drawn from the 08:30–08:31 CT 1m bars** (real-UTC fix, T1 —
   the ORB minutes are derived with `time.UnixMilli(bar.OpenTime).In(ctime())`,
   never wall offsets). It is enforced from the 08:32 CT bar by `orbGateFilter`
   (knob `OrbGateEnabled`); entries outside the ORB are filtered with no log
   line — verify by behaviour: before 08:32 CT no intraday mentor entry
   appears, from 08:32 they do (inside the ORB).
3. **The trading window is 08:30–09:30 CT** (knob keys `mentor_window_start`
   default `08:30`, `mentor_window_minutes` default `60`). Outside it, the
   placement is refused:
   ```
   🧑‍🏫 mentor placement REFUSED — outside the trading window 08:30–09:30 CT — no new entries [D1.2 p1 @23:52–24:59]
   ```
   SWING4H is exempt from the window (the swing may be at any hour).
4. **SIM behaviour only.** Every fill should land on the SIM account; if you
   ever see a mentor order on a live account, stop and re-check precondition 3.

## 5. Switch OFF

Set `"mentor_mode": false` in the strategy's `risk_control` (or flip the card
off in the Risk Control editor once it ships). To also unset `MENTOR_PLACE` in
`.env`, use the same owner step as above (the CTO drafts the line, the owner
pastes it, restart). With `mentor_mode` OFF every mentor path returns before
touching anything — the trader is byte-identical to AI mode (L4). There is no
other switch.

## Guide

The in-app guide lives at the `/guide` route (`web/src/router/paths.ts`,
`ROUTES.guide`). The mentor knob cards and today's rulings (per-visit levels,
box ping-pong ≥ 50, every box return, intraday confluence, EMA loss rule, leg
budget, loss box) land with the guide wave (`feat/mentor-guide`,
`GUIDE_BUILT_REV` bumped in the same PR).
