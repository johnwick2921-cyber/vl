# MNQ ORB: three stop rules, six months

**Computational checks PASS, with simulation limitations below.**

Period: 2025-10-20 through 2026-04-19. 08:30 Houston open (DST aware). Separate 1m, 2m, 5m ranges. First trade one tick outside completed ORB enters at that trade price. One trade per day per test; no re-entry. TP always one full box width from entry. Box means ORB high minus low.

Rules: box = SL one full box (1:1); half_box = SL half box; 20_to_BE = initial SL20 points, move stop to entry after a trade reaches +20 points. A target reached before or on that activation print exits at TP, not BE. Moved BE stop becomes active for subsequent records; zero modeled amendment latency.

| Stop rule | ORB | Entries | TP | SL | BE | Open | TP % of entries | Points won | Points lost | Net planned points |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| box | 1 | 120 | 47 | 73 | 0 | 0 | 39.167 | 2514.500 | -4002.000 | -1487.500 |
| box | 2 | 120 | 59 | 61 | 0 | 0 | 49.167 | 3894.750 | -4121.500 | -226.750 |
| box | 5 | 121 | 65 | 54 | 0 | 2 | 53.719 | 6072.500 | -4324.000 | 1748.500 |
| half_box | 1 | 120 | 35 | 85 | 0 | 0 | 29.167 | 1903.500 | -2306.500 | -403.000 |
| half_box | 2 | 120 | 43 | 77 | 0 | 0 | 35.833 | 2876.500 | -2569.875 | 306.625 |
| half_box | 5 | 121 | 46 | 75 | 0 | 0 | 38.017 | 4259.500 | -3195.500 | 1064.000 |
| 20_to_BE | 1 | 120 | 22 | 68 | 30 | 0 | 18.333 | 1062.000 | -1360.000 | -298.000 |
| 20_to_BE | 2 | 120 | 26 | 63 | 31 | 0 | 21.667 | 1527.500 | -1260.000 | 267.500 |
| 20_to_BE | 5 | 121 | 20 | 58 | 43 | 0 | 16.529 | 1555.750 | -1160.000 | 395.750 |

No commission, spread, or latency costs deducted. Planned stop accounting assumes an exact stop fill. Observed crossing-trade PnL is separately preserved; a BE crossing can produce a negative observed result. These are trade-path outcomes, not broker-fill proof.

Half-box odd tick widths produce nominal 0.125-point increments, which are not valid MNQ order prices. Primary table is the exact half-box research calculation requested; outward rounding to 0.25-point ticks uses the same crossing tick and its net PnL is in net_tick_rounded_points. Do not submit half-tick stop prices to a broker.

Coverage: 130 candidate weekdays, 122 pass full-session checks. Same exclusions and entry row IDs as frozen baseline. Additional ORB exclusions are in summary.csv and trades.csv. Full 390-minute coverage gate excludes early closes and some incomplete sessions; exclusions are not all labeled holidays. Any still-open trade at 15:00 is OPEN_AT_CUTOFF, not assigned an invented exit.

## Verification

- 1098 variant rows checked with separate first-crossing math.
- Every included entry compared to the frozen six-month baseline.
- Raw file SHA256, record counts, contract selection, flags and timestamp checks use the prior audited decoder.
- 10 tests in CT and UTC; 6000 synthetic comparisons per timezone; 5 assertion-killed mutants.
- All summary arithmetic reconciled to per-trade rows. Excel cells round-trip checked against CSV.
- Independent algorithm shares raw decoding and data gates; no independent exchange-tape completeness claim.
- Audit checklist reference: docs/superpowers/AUDIT-CHECKLIST.md @ ba50edf22.
- Engine SHA256: dec4b8be971acf77d5bd7822321bc89993f0ba96c2743d3eedc72d328c91231f

All nine tests overlap in dates. Their sum is not a one-trade-per-day portfolio. This is historical research only; no production config, DB, NT8 or trading changes.
