# Live daily excess withdrawal — owner scenario, 2026-10-23

Withdraw all positive end-of-day excess above $2,100; no $500 minimum and no $2,000 cap. Retain $2,000 above locked $100 MLL. 90/10 split. These payout constraints are owner scenario assumptions, not verified standard-live eligibility rules.

Continue only after the first simulated account reaches payout five, with immediate next-session live approval assumed. This is NOT the repeated funded-account purchase scenario. No automatic replacement after live breach. Source sample 2025-10-20–2026-04-19; available live segment ends 2026-04-17.

| Method | MNQ | Payouts | Gross USD | Received USD | End balance USD | Outcome |
|---|---:|---:|---:|---:|---:|---|
| E | 3 | 0 | 0 | 0 | 1,416.00 | No breach in valid sample |
| E | 6 | 4 | 1,533.00 | 1,379.70 | 99.00 | Breach 2026-02-17 |
| A | 6 | 0 | 0 | 0 | -2,001.00 | Breach 2026-01-14 |

A/3 did not reach payout five; no live phase fabricated. Prior SIM payouts are not included in the live amounts above. Stress execution: E/6 receives $1,366.20; E/3 ends $1,363.50 without payouts; A/6 remains breached after two trades.

Verified every executed path against the independent Decimal-dollar oracle. Every nonbreach day withdraws exactly its positive excess; every payout leaves exactly $2,100. A fixture confirms a $51 withdrawal is allowed under the new policy. Original input hashes verified. New tick downloads/replays were not performed; prior verified entry/exit paths reused.

Remaining limitations: live fee modeled as $1 per MNQ round trip; actual live costs unverified. Trade-print proxies, no queue/latency, excluded dates retained, pre-cutoff exit proxy, no bonus credited. Provider approval, timing, minimum/cap remain unverified. No claim of guaranteed actual cash. MLL follows published $2,000 trigger / $100 lock interpretation used in preceding report.

Engine: lucid_live.py; runner: live_daily_excess.py. Full data: summary.csv, ledger.csv, payouts.csv; checks and hashes: verification.json.
