# EMA9/EMA34 2m — week of 26–30 October 2026

Same first-filled overnight method as prior replay: each session begins 17:00 Houston on the preceding date and ends before 08:30 on the listed date. EMA seeded from 1,000 completed 2m bars of the same contract before open; intrabar crossover evaluated at exchange-event boundaries; moving EMA9 limit, touch-fill assumption; one filled trade per night; TP15, independent SL15/SL16. No trailing.

| Session ending | Contract | Signal Houston | Entry Houston | Side | Entry | SL15 result | SL16 result |
|---|---|---|---|---|---:|---|---|
| 2026-10-26 | MNQZ6 | 25/10 18:12:00 | 25/10 18:39:31 | SHORT | 29169.75 | TP 15 | TP 15 |
| 2026-10-27 | MNQZ6 | 26/10 17:00:01 | 26/10 17:12:08 | LONG | 29482.00 | TP 15 | TP 15 |
| 2026-10-28 | MNQZ6 | 27/10 17:12:00 | 27/10 17:14:04 | LONG | 29802.75 | TP 15 | TP 15 |
| 2026-10-29 | MNQZ6 | 28/10 18:10:00 | 28/10 18:18:59 | LONG | 29704.75 | TP 15 | TP 15 |
| 2026-10-30 | MNQZ6 | 29/10 18:34:00 | 29/10 18:38:24 | LONG | 29097.25 | TP 15 | TP 15 |

| Stop | Trades | TP | SL | Open | Win % | Won points | Lost points | Gross points |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 15 | 5 | 5 | 0 | 0 | 100.0 | 75 | 0 | 75 |
| 16 | 5 | 5 | 0 | 0 | 100.0 | 75 | 0 | 75 |

Independent execution replay: 10 rows, 0 mismatches; raw entry/exit checks 10; summary arithmetic PASS.

Data: newly retrieved GLBX.MDP3 TBBO for five nights, OHLCV-1m history from 19 October; provider condition and SHA256 receipts preserved. Download quotes were zero. Expired candidate MNQU6 did not resolve; selected contract is recorded above, no substitution or invented ticks.

Gross results before commissions and queue/latency costs. Touch-fill is a modeling assumption. This does not establish parity with the closed-source video indicator or with owner-observed chart trades; compare exact timestamps and directions above. No outcome was selected to match a desired weekly win streak.
