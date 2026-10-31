# EMA9 / EMA34 — owner-requested fresh rerun, 2026-10-31

Offline MNQ research. This is the 2-minute EMA9/EMA34 overnight video setup, NOT ORB.

## Rules actually replayed

- 17:00 Houston on the preceding date to before 08:30 Houston on the session date; dates sampled within 2025-10-20–2026-04-19. The cutoff is the declared research convention.
- Same raw contract, 1,000 completed 2-minute historical bars before each session; EMA9 and EMA34 run through the session and do not reset at 17:00. Seed uses alpha=2/(period+1), Decimal precision 40.
- During the forming 2-minute candle, evaluate EMA crossover after the last record of an exchange event. Bar partition uses receipt time, consistent with warmup OHLCV.
- Following a crossover, use a moving EMA9 retest limit. Fill assumption: touch at the previously resting, tick-rounded limit. No same-event signal-to-fill reaction.
- Before any fill, later crossover may switch direction and re-arm. First FILLED trade only; after that, fixed TP15 and SL15 or SL16, no second trade, no trailing.
- Stop outcome uses observed adverse trade print, so a gap can lose more than the requested stop distance. This is not a guarantee of live fill prices.

## Fresh results

| SL | TP | Trades | TP hits | SL hits | Win rate | Won points | Lost points | Gross net points | Worst trade |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 15 | 15 | 109 | 56 | 53 | 51.38% | +840.00 | -795.50 | +44.50 | -15.25 |
| 16 | 15 | 109 | 57 | 52 | 52.29% | +855.00 | -834.25 | +20.75 | -17.00 |

All points are per contract, gross before fees, commissions, latency and queue effects. No unresolved/open trades among these included sessions.

## Coverage and verification

[A] 130 planned session dates; 109 included. Exclusions: 8 nonzero-quoted downloads not acquired; 9 provider-degraded session/warmup dates; 2 sessions with no overnight ticks; 1 timestamp anomaly; 1 insufficient-coverage session. Excluded days are NOT no-signal days, and this is not a complete six-month result.

[A] Fresh raw-file hash validation, contract selection from pre-session information, historical-warmup checks, and complete replay. All 218 ledger records are identical to the prior corrected receipt-clock run.

[A] Independent weighted-seed floating-point implementation checked 218 rows and their raw entry/exit records, zero mismatches. Independent total/count arithmetic passed. Twenty-five engine tests passed again in this rerun.

[A] Earlier dedicated EMA audit remains supporting evidence: 2,150,995 EMA decision pairs and 50,685 bar closes checked. The fresh rerun did not repeat that entire pandas trace; it reran the raw history and separate execution oracle.

Exact parity with the closed-source TradingView video indicator is NOT established. Touch limit fills and event-boundary reaction are research assumptions. Tests establish the declared model, not guaranteed live profitability.

Artifacts: trades.csv, sessions.csv, ledger.json, summary.json, independent-audit.json and provenance.json alongside this report. Live services, broker configuration and trading database were untouched.
