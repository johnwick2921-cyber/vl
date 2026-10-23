A / E — repeat account purchase hypothetical | 2026-10-23

Sample 2025-10-20–2026-04-19. Start with one funded $50,000 account. After a breach OR fifth payout, replace with a fresh funded account before the next eligible trading session. Continue to end of data. Never reuse the breach-day trade. No extra purchase after the final trade.

A: immediate ORB5m breakout, SL whole box, TP whole box. E: EMA9 ORB2m confirmed-close entry, SL half box, TP whole box, fixed stop. One trade/day per strategy; 3 and 6 MNQ studied separately.

Payout policy unchanged: five days net >= $150, positive cycle net P&L, request min(50% retained profit, $2,000, balance minus $52,100), minimum request $500. Trader gets 90%; full request deducted. Keep $2,000 above locked $50,100 at withdrawal. That reserve may decline on later losses.

## trade_print_proxy

| Method | MNQ | Accounts used | Breached | Payout-5 accounts | Payouts | Trader receives USD | Last balance USD |
| --- | --- | --- | --- | --- | --- | --- | --- |
| A | 3 | 3 | 2 | 0 | 7 | 6,959.25 | 52,124.00 |
| A | 6 | 9 | 7 | 1 | 10 | 16,343.10 | 52,765.00 |
| E | 3 | 2 | 0 | 1 | 5 | 3,773.25 | 51,416.00 |
| E | 6 | 4 | 2 | 1 | 8 | 10,218.60 | 51,929.00 |

## stress1tick

| Method | MNQ | Accounts used | Breached | Payout-5 accounts | Payouts | Trader receives USD | Last balance USD |
| --- | --- | --- | --- | --- | --- | --- | --- |
| A | 3 | 3 | 2 | 0 | 7 | 6,912.00 | 52,118.00 |
| A | 6 | 9 | 7 | 1 | 10 | 16,302.60 | 52,750.00 |
| E | 3 | 2 | 0 | 1 | 5 | 3,726.00 | 51,363.50 |
| E | 6 | 4 | 2 | 1 | 8 | 10,094.40 | 51,917.00 |

CRITICAL: immediate funded access is a research assumption, not a verified purchasable Lucid product. Evaluation phase, failures, purchase prices, onboarding/cooldown, real-live transition and provider permission are NOT modeled. Repeated SIM purchases after five payouts are hypothetical. These results must not be described as realizable net income.

All received amounts are after modeled trading fees and 90/10 split, BEFORE purchase/evaluation costs and tax. Account prices are unknown, not zero. If each account costs C and you include the initial account, net = reported received cash minus accounts_used × C. If initial cost is sunk, incremental cost uses replacements × C. Residual account equity is not added to payouts.

Primary uses trade-print fill proxies; stress adds one adverse tick to market entries/stop exits and requires one tick through limits. Round-trip fee assumption $1/MNQ; no queue/latency model. Original incomplete-data exclusions retained, so results are conditional on available sessions. No new raw tick download or replay; verified prior tick paths reused.

Verification: each actual account reconciled against an independent Decimal oracle; daily balance + P&L − withdrawal checked; every eligible trade consumed exactly once. 14 tests in Chicago/UTC, including 1,000 randomized multi-account sequences and earlier 3,000 account sequences per timezone. Initial export lacked the quantity key in the daily ledger: fixed and regenerated before delivery, with first attempt preserved. Final CSVs identify method, execution, quantity, account and date.

Audit reference docs/superpowers/AUDIT-CHECKLIST.md @ ba50edf22. Engine lucid_rebuy.py wraps verified lucid_payout.py. Evidence files: accounts.csv, ledger.csv, payouts.csv, summary.csv, audit.json, verification.json.