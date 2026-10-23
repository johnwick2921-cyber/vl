E / D / A — LucidFlex funded 50K: offline verification, 22/10/2026

Sample: 20/10/2025–19/04/2026. Separate 3 and 6 MNQ accounts. No DLL, no withdrawals, no resets. Current published rules applied counterfactually.

E: EMA9 ORB2m, candle-close signal, SL half box, TP whole box, no trailing. D: ORB5m full-body confirmation then retest limit, SL half box, TP whole box. A: immediate ORB5m breakout, SL whole box, TP whole box.

Verified: 26 tests in Chicago and UTC; 5,000 randomized E/D/A exit paths, 4,000 other exit paths and 2,000 account paths per timezone. Nine deliberately injected faults were detected. Full tick replay recomputed source entries and exits. Independent Decimal-dollar audit matched 12 account summaries, 1,140 ledger rows and 1,072 rolling windows; file hashes matched.

This is research-engine verification. It does not certify the live bot, NT8 order hooks, broker fills, or future profitability. No deployment or live execution was performed.

Primary — trade-print proxy

| Method | MNQ | Trades | Net USD | End USD | Breach date | Max loss/day USD | Max win/day USD | 20-trade breaches |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| E | 3 | 115 | 7,708.50 | 57,708.50 | None in valid sample | -403.50 | 901.50 | 0/96 |
| E | 6 | 115 | 15,417.00 | 65,417.00 | None in valid sample | -807.00 | 1,803.00 | 33/96 |
| D | 3 | 89 | 8,943.00 | 58,943.00 | None in valid sample | -562.50 | 1,464.00 | 6/70 |
| D | 6 | 9 | -1,200.00 | 48,800.00 | 2025-11-04 | -513.00 | 1,098.00 | 35/70 |
| A | 3 | 121 | 9,756.00 | 59,756.00 | None in valid sample | -982.50 | 1,464.00 | 43/102 |
| A | 6 | 121 | 19,512.00 | 69,512.00 | None in valid sample | -1,965.00 | 2,928.00 | 73/102 |

Stress — one tick

| Method | MNQ | Trades | Net USD | End USD | Breach date | Max loss/day USD | Max win/day USD | 20-trade breaches |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| E | 3 | 115 | 7,603.50 | 57,603.50 | None in valid sample | -405.00 | 901.50 | 0/96 |
| E | 6 | 115 | 15,207.00 | 65,207.00 | None in valid sample | -810.00 | 1,803.00 | 33/96 |
| D | 3 | 89 | 7,906.50 | 57,906.50 | None in valid sample | -564.00 | 1,464.00 | 6/70 |
| D | 6 | 9 | -1,206.00 | 48,794.00 | 2025-11-04 | -516.00 | 1,098.00 | 35/70 |
| A | 3 | 121 | 9,669.00 | 59,669.00 | None in valid sample | -984.00 | 1,464.00 | 52/102 |
| A | 6 | 121 | 19,338.00 | 69,338.00 | None in valid sample | -1,968.00 | 2,928.00 | 73/102 |

Pipeline: receipt/hash and contract/timestamp checks → complete ORB → E continuous-session EMA warmup / D closed full-body confirmation / A first breakout → entry → integer-tick stop/target → chronological exit print → fees → integer-cent daily ledger → intraday loss-limit check → EOD floor update → report. Independent account checker imports neither engine.

Math: MNQ 0.25 point/tick, $0.50/tick/contract. Gross USD = signed gain ticks × $0.50 × quantity. Fee assumption = $1 round trip per contract (Lucid published commission schedule), reserved at entry. Net = gross minus fee. This fee has not been checked against an owner statement.

SL half box is rounded UP to a valid whole tick, so an odd-tick box has slightly less than 2:1 planned reward/risk. Targets use the full box. Stops use first crossing trade price, not a guaranteed stop price. No fractional-tick orders.

Account: initial $50,000; floor $48,000. Equity at or below floor breaches immediately. Next EOD floor = min($50,100, max(previous floor, highest EOD balance − $2,000)). Stop the account on breach; never include later profits. Breach balance is a trade-print mark, not a guaranteed liquidation fill.

Primary uses trade-print entry/stop proxies and touched target limits. Stress: 1 tick adverse on market entries and stop/cutoff exits; target must trade 1 tick through. D entry must trade 1 tick through the limit, filled at the limit. Queue position, quotes, latency and actual broker fills are not available.

At 15:00 Houston, unresolved trades use the last observed trade BEFORE cutoff as a forced-close proxy. E includes 1 such trade, D 3, A 2. Prior reports excluded these open trades; totals therefore differ, also because of tick rounding, stop overshoots and fees.

130 weekdays in the requested span; 122 pass whole-session coverage. Eight excluded days are gaps, not confirmed no-trade days. E has another four excluded days; A/D another one. E has 115 entries, D 89, A 121. Different eligible cohorts mean this is not a perfectly controlled ranking.

Rolling tests start a fresh $50,000 account at each eligible entry and follow up to 20 trades, stopping on breach. Windows overlap and omit unavailable days; fractions are descriptive counts, NOT independent estimates of failure probability. They are not 20 calendar days.

After floor locks at $50,100, peak-to-trough drawdown can exceed $2,000 without a breach if accumulated equity remains above the fixed floor. No payout withdrawals were modeled; withdrawals could materially change survival.

Findings: D with 6 MNQ breaches on 04/11/2025 in both fill models. E with 3 MNQ had 0/96 rolling-window breaches in both models; E with 6 had 33/96. A had higher October-start profits but many more rolling-window breaches. These are historical conditional results, not proof of a safe or best future strategy.

Sources (checked for this study): https://support.lucidtrading.com/en/articles/12945795-lucidflex-funded-account ; https://support.lucidtrading.com/en/articles/12945815-lucidflex-drawdown ; https://support.lucidtrading.com/en/articles/12945808-lucidflex-scaling-plan ; https://support.lucidtrading.com/en/articles/12945796-lucidflex-payouts ; https://support.lucidtrading.com/en/articles/11508978-approved-products-and-commissions ; https://info.tradovate.com/pricing/commissions ; https://www.cmegroup.com/markets/equities/nasdaq/micro-e-mini-nasdaq-100.contractSpecs.html

Evidence: audit.json, engine-verification.json, independent-account-verification.json, paths.json, accounts.csv, ledger.csv, rolling20.csv. Audit reference: docs/superpowers/AUDIT-CHECKLIST.md @ ba50edf22.
