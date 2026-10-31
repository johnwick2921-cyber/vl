# EMA9 / EMA34 2m — 1 May to 30 October 2026

Same engine as the separately verified 5/5 winning week. One first FILLED trade per overnight session, TP15, SL15 and SL16 separately. Before first fill, subsequent crosses may switch direction. EMA9 retest limit updates at event boundaries; touch-fill assumption. No trailing.

Session date labels the morning: 17:00 Houston on the prior date to before 08:30 Houston on the listed date. Both EMAs use 1,000 completed 2m bars of the same raw contract before each session; no EMA reset. Historical OHLCV and intraday bar grouping use a consistent receive-time clock. Intrabar crossover is evaluated after the final record of an exchange event, not only at candle close.

| SL | TP | Entries | TP hits | SL hits | Open | Closed win % | Won points | Lost points | Gross points |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 15 | 15 | 129 | 66 | 63 | 0 | 51.16 | +990.00 | -946.50 | +43.50 |
| 16 | 15 | 129 | 66 | 63 | 0 | 51.16 | +990.00 | -1009.75 | -19.75 |

## Coverage

Planned session dates: 131. Coverage counts: `{'INCLUDED': 129, 'DEGRADED_PROVIDER_DATE': 2}`.
Excluded sessions are not counted as no-signal days. Open positions are excluded from closed P&L; their counts remain visible. No missing trades are invented.

## Monthly results

| Month | SL | Trades | TP | SL hits | Open | Gross points |
|---|---:|---:|---:|---:|---:|---:|
| 2026-05 | 15 | 19 | 8 | 11 | 0 | -45.00 |
| 2026-05 | 16 | 19 | 8 | 11 | 0 | -56.00 |
| 2026-06 | 15 | 22 | 13 | 9 | 0 | +60.00 |
| 2026-06 | 16 | 22 | 13 | 9 | 0 | +51.00 |
| 2026-07 | 15 | 23 | 10 | 13 | 0 | -46.50 |
| 2026-07 | 16 | 23 | 10 | 13 | 0 | -58.75 |
| 2026-08 | 15 | 21 | 10 | 11 | 0 | -15.00 |
| 2026-08 | 16 | 21 | 10 | 11 | 0 | -26.00 |
| 2026-09 | 15 | 22 | 12 | 10 | 0 | +30.00 |
| 2026-09 | 16 | 22 | 12 | 10 | 0 | +20.00 |
| 2026-10 | 15 | 22 | 13 | 9 | 0 | +60.00 |
| 2026-10 | 16 | 22 | 13 | 9 | 0 | +50.00 |

## Verification and limitations

[A] Independent weighted-seed EMA/execution replay: 258 rows; 258 raw entry/exit checks; zero mismatches; count/P&L arithmetic PASS. All ten economic results of the separately checked final week agree.
[A] Raw data SHA256 and historical warmup SHA256 checked before replay. Contract selection uses only pre-session information. Provider quality gates remain in force. Engine and inputs are recorded in provenance.json.
Gross results are before commissions and real queue/latency costs. Stops use observed adverse trade prints, so realized loss can exceed 15/16 points. Touch limit fills are an assumption; exact parity with the closed-source TradingView indicator is not proven. These checks verify the declared model, not live profitability. No live service, trading account or production DB was changed.
