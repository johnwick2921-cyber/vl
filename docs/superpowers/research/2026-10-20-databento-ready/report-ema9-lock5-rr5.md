# EMA9 ORB: TP5R, SL half box, lock +5 at +1R

Period: 2025-10-20 through 2026-04-19. 08:30 America/Chicago open. Same frozen continuous-EMA9 closed-candle entry. One entry/day per independent timeframe. Primary requested comparison: ORB2m.

| orb_minutes | entries | TP | SL | LOCK | open | won_points | lost_points | net_points | rounded_net_points |
|---|---|---|---|---|---|---|---|---|---|
| 1 | 118 | 13 | 57 | 46 | 2 | 1836.875 | -1606.0 | 230.875 | 227.25 |
| 2 | 115 | 9 | 51 | 52 | 3 | 1797.5 | -1627.125 | 170.375 | 167.5 |
| 5 | 111 | 8 | 55 | 37 | 11 | 1985.0 | -2276.125 | -291.125 | -294.25 |

Gross nominal realized points only; fees/slippage excluded. OPEN_AT_CUTOFF at15:00 omitted from realized totals; marks retained in trades.csv. Half-tick nominal levels are not executable: rounded totals are separate. Lock activates at max(1R,5.25 points), becomes effective next record, and stays fixed. No continuous trailing.

Validation: 16 tests in CT and UTC, 6000 randomized paths each, 3 caught mutations; raw tick checksums and entry/ORB checked during replay; independent first-passage exit oracle; all366 rows reconciled with original entries and Decimal arithmetic. Shared source/decoder, not independent data or broker fills. Audit reference docs/superpowers/AUDIT-CHECKLIST.md @ba50edf22.
