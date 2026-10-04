> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# News-Hygiene Micro-Wave — build & park record

**Branch:** `fix/news-hygiene` · **Base:** `f08a300a` (rewritten dev) · **Deploy:** Monday flat window, before NFP Friday 2026-09-04. Parked for the owner's "go".
**Commit-ref:** `https://github.com/johnwick2921-cyber/vl/blob/e3d125b0f1a532b2993af7ee57eab63ac3c996c6/docs/superpowers/plans/2026-08-29-news-hygiene-micro-wave.md`

## The T1 gate — quoted behavior (post-wave)

- **Windows:** ±15 min around each T1 event (`kernel.T1BlackoutMinutes=15`), minute-of-day CT, wrap-aware.
- **Pre-window force-flat:** from blackout-start −2 min (`t1ForceFlatLead=2`) through window end, open positions are flattened (limit-then-market, retried each cycle while open).
- **Entries:** `sessionGateDecision` → `InT1Blackout` refuses entries inside ±15m ("🔴 red-news blackout: …"). T2 = caution-only, never a hard block. The −17..−15 lead gap has no entry gate — a new entry there is flattened on the next cycle (~2 min later).
- **Armed orders (FIXED this wave):** `enforceT1ForceFlatAt` now cancels every non-terminal armed row FIRST — before the open-position check — with `reason=news_window`, ack-waited, ledger-flipped even on ack timeout. A FLAT trader's resting limit can no longer fill into the print. Pre-wave, the cancel sat behind `len(positions)==0 → return`, so a working limit survived the entire window (forensics item 3).

## Items shipped

| Item | Change | Proof |
|---|---|---|
| 3 | arm-cancel first + flat-trader action (`n+unacked>0`) | `TestT1NewsFlatTraderArmCancelled`, `TestT1NewsArmedCancelBeforeFlatten` (cancel-before-flatten wire order), `TestT1NewsEntryBlockedInWindow` — all PASS |
| S1 | `maybeFetchCalendar` logs the real fetched-total (write deltas separate) | `TestF0*` family re-run PASS |
| S2 | `calendar_static_t1.json` → NFP 09-04 12:30Z, CPI 09-10 12:30Z, FOMC 09-16 18:00Z + presser 18:30Z (USD T1) | json-valid, 4 events |
| 21 | `AUDIT-CHECKLIST` class 21 (log-lie counters) + T9 re-render source-enumeration note; header → THE 21 BUG CLASSES | committed |

## Gates (all run on the branch)

`go build ./...` EXIT 0 · `gofmt` clean on touched files · `go test ./...` **27/27 packages, 0 FAIL** (goldens PASS) · `TestT1News*` 3/3 · `TestF0*`/`TestSList*` families PASS.

## Cutover notes (Monday)

- One cutover, same marker sequence as sunday-shield (merge → marker commit R with `GUIDE_BUILT_REV`+`deploy/RELEASE` → temp-clone build at M → flat-gate all-origin → mv-swap → kill -9 → boot poll: rev==M · goldens PASS · `📅 calendar: fetched N events` now truthful).
- Boot proof for this wave: `🔒 T1-FORCE-FLAT: N armed order(s) cancelled before the red-news window` appears only in a real red-news window (event-driven; first live proof = next T1 event with a working arm).
- After deploy, verify the NEXT fetch logs a non-zero `fetched N events` (S1 fix visible on the idle-weekend boot fetch).
