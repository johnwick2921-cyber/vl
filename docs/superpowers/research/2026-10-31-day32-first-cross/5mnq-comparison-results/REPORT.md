# 5 MNQ, fresh $50K versus $54K

Period May 1–October 30, 2026. Same E EMA9 ORB2m before09:00, half-box SL/whole-box TP, one trade/day. No extra funded reserve. Maximum eligible requests, same existing payout policy. $50K starts zero qualified days; $54K starts one qualified day and $4,000 pre-existing profit. Fee $1/MNQ round trip assumed.

|Initial|Received after90/10|Funded payouts|Funded accounts used/lost|Live accounts used/lost|
|---|---:|---:|---|---|
|$50K|$6,780.37|6|7/6|0/0|
|$54K|$9,480.37|6|5/3|1/1|

One-tick stress receipts $6,730.87 / $9,462.37 respectively. Zero live payouts. Multiple account replacements; receipts do not deduct purchase fees/evaluation time. Replacement funded account at next eligible signal is hypothetical. Live approval assumed next signal after fifth payout, not provider guarantee; live reserve remains prior policy. Lost-live cooldown14calendar days retained. Residual funded balance at live transition not counted as cash. Selection bias: entry cutoff chosen after inspecting sample.

Initial attempt stopped before output because inherited live study permitted only3/6MNQ. New offline copy lucid_live_5mnq.py differs solely in quantity allowlist/error text; no deployed code changes. Preserved failed wrapper as cashout_5mnq_compare_initial.py. Final run:2,000 funded and2,000 live independent Decimal oracle comparisons,8boundary checks,4initialbalance checks pass. Source path hash checked, complete signal partition retained. These validate modeled arithmetic, not guaranteed fills or provider approval. Full stage/payout/day ledgers attached.
