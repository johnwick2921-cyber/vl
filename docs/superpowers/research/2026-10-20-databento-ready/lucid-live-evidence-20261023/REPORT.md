Lucid — two conditional continuation cases | verified 2026-10-23

Scope: E EMA9 ORB2m SL half box/TP whole box; D full-body ORB5m retest half-box SL/whole-box TP; A immediate ORB5m breakout whole-box SL/TP. Three or six MNQ per account. Historical data: 2025-10-20–2026-04-19.

Correction: the earlier status LIVE_TRANSITION_AFTER_5_PAYOUTS means the simulation stopped at a review milestone. It was not evidence of approval. In this study only, approved transition/new account is assumed before the next available session after payout five. No same-day trade is reused. D/3, D/6 and A/3 never reached payout five, so no continuation is invented.

## 1. Fresh $50k hypothetical — primary fills

| Method | MNQ | Extra received USD | End balance USD | End date | Status |
| --- | --- | --- | --- | --- | --- |
| E | 3 | 0.00 | 51,416.00 | 2026-04-17 | END_OF_VALID_SAMPLE |
| E | 6 | 615.60 | 50,099.00 | 2026-02-19 | MLL_BREACH |
| A | 6 | 0.00 | 47,999.00 | 2026-01-14 | MLL_BREACH |

## 2. Conditional live — trade_print_proxy

| Method | MNQ | Withdrawal policy | Extra received USD | End balance USD | End date | Status |
| --- | --- | --- | --- | --- | --- | --- |
| E | 3 | None | 0.00 | 1,416.00 | 2026-04-17 | END_OF_VALID_SAMPLE |
| E | 3 | Illustrative | 0.00 | 1,416.00 | 2026-04-17 | END_OF_VALID_SAMPLE |
| E | 6 | None | 0.00 | 4,494.00 | 2026-04-17 | END_OF_VALID_SAMPLE |
| E | 6 | Illustrative | 1,350.00 | 99.00 | 2026-02-17 | LIVE_MLL_BREACH |
| A | 6 | None | 0.00 | -2,001.00 | 2026-01-14 | LIVE_MLL_BREACH |
| A | 6 | Illustrative | 0.00 | -2,001.00 | 2026-01-14 | LIVE_MLL_BREACH |

## 2. Conditional live — stress1tick

| Method | MNQ | Withdrawal policy | Extra received USD | End balance USD | End date | Status |
| --- | --- | --- | --- | --- | --- | --- |
| E | 3 | None | 0.00 | 1,363.50 | 2026-04-17 | END_OF_VALID_SAMPLE |
| E | 3 | Illustrative | 0.00 | 1,363.50 | 2026-04-17 | END_OF_VALID_SAMPLE |
| E | 6 | None | 0.00 | 99.00 | 2026-03-20 | LIVE_MLL_BREACH |
| E | 6 | Illustrative | 1,341.90 | 99.00 | 2026-02-17 | LIVE_MLL_BREACH |
| A | 6 | None | 0.00 | -2,001.00 | 2026-01-14 | LIVE_MLL_BREACH |
| A | 6 | Illustrative | 0.00 | -2,001.00 | 2026-01-14 | LIVE_MLL_BREACH |

Case 1 — owner hypothetical: a fresh $50,000 SIM account after payout five, same prior payout policy (five net-$150 days, positive cycle, half of retained profit capped at $2,000, $500 minimum, retain $52,100). Starting a new account does not carry old equity; no evaluation costs or permission to buy another SIM account modeled. This is NOT an assertion that Lucid permits such an account while live.

Case 2 — standard-live-rule core, conditional start: $0 balance, initial floor −$2,000. EOD trail; locked floor $100. No DLL. SIM residual equity is recorded in the old report but not added to new live cash. Regular live withdrawals use 90/10. Scaling is CME 20/30/40 micros at $0/$2k/$4k; 3/6 stay below these tiers. Negative-equity tier is assumed to remain the lowest tier.

Unresolved live payout minimum/cap: illustrative withdrawal scenario uses owner reserve $2,000 above $100, minimum $500 and cap $2,000, checked daily. Those minimum/cap values are assumptions carried for sensitivity, NOT verified live rules. No 50% or five-day SIM rule is automatically copied into live. A no-withdrawal control is also run.

Live fee assumption: $1 RT per MNQ, retained from earlier SIM study; actual live all-in fees not verified. Results are not promised live cash. No broker connections, account configuration or deployments were changed.

Current rule core: risk team decides transition timing. The current help page says lock at $100 once profits reach $2,000; engine uses the EOD high-water balance for that test. Also tested $2,100 trigger sensitivity. Daily live payouts are described, but account-specific payout minimum/cap and approval delay are unresolved. Current bonus objectives: $4,000 net live profit and 20 qualifying days. Engine flags first objective completion but credits ZERO bonus; approval and repeat-cycle timing are not invented.

Older public article describes a different one-time bonus. It is excluded. Legacy Flex purchased/reset before 2026-02-28 has another capital-transfer scheme and is not mixed into this model. These are current-rule counterfactuals, not proof of rules in force during historical sample.

Verification: 9 live tests in Chicago and UTC, each with 3,000 randomized paths against a separate Decimal oracle. Reconciled 24 executed live summaries and six fresh-$50k summaries; 24 additional live rows correctly report no fifth-payout transition. Five injected faults caught. Strict-vs-inclusive breach mutation was equivalent on this fixed-quantity cents lattice and is not claimed caught. Engine still uses inclusive <= floor. Earlier 35 tests also rerun.

Limitations carried forward: 8 whole-session dates excluded, plus method-specific exclusions; unknown days are not proven flat. Fill models are trade-print proxies and one-tick stress, not queue-aware fills. Last pre-15:00 Houston price closes unresolved trades as a proxy. Hypothetical transition has no onboarding delay. No bonuses/cashout/insurance/refunds counted. No independent accounts are combined into a real multi-account household scenario.

Balance conservation: opening balance + net trading P&L − gross withdrawals = ending balance. Trader cash received is separate. A breach stops subsequent trades. Previously withdrawn SIM cash remains separate; remaining failed account equity is not spendable cash.

Sources checked 2026-10-23: current New Live Structure (Joel), https://support.lucidtrading.com/en/articles/16558826-new-live-structure ; older variant (Doug), https://support.lucidtrading.com/en/articles/13425130-new-live-structure ; legacy https://support.lucidtrading.com/en/articles/13424914-lucidflex-live-legacy ; scaling https://support.lucidtrading.com/en/articles/15245873-live-scaling-plan ; regular live 90/10 https://lucidtrading.com/about-us/ .

Audit reference: docs/superpowers/AUDIT-CHECKLIST.md @ ba50edf22. Engine lucid_live.py; tests test_lucid_live.py; independent checks verify_live_cases.py. Input hashes in audit.json; per-day record IDs in ledger.csv and new50k_ledger.csv.