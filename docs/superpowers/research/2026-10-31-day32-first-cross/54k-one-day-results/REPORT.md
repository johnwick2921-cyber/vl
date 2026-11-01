# E before 09:00 — initial $54,000, one qualifying day

Offline conditional replay: May 1–October 30, 2026. EMA9 ORB2m, SL half box, TP whole box, one trade per day, entries before 09:00 Houston. Initial $4,000 profit is supplied capital state, not profit earned by this replay. Initial qualifying days=1; no prior payouts. Initial locked MLL=$50,100. Replacement funded accounts start $50,000 with no prior qualifying days.

Fixed funded request $500 gross / $450 received. Fee assumption $1 per MNQ round trip. Owner reserve $2,000 above MLL retains $52,100. Approval assumed immediate; no evaluation costs/time. After fifth payout, hypothetical next-session live transition; live withdrawals use prior daily-excess-over-$2,100 policy. Lost live imposes 14 calendar days before hypothetical replacement funded account. This is not a provider approval forecast. Funded balances remaining at live transition are not counted as cash received.

| MNQ | Owner reserve | Funded payouts | Received | Funded accounts used | Funded breaches | Live breaches |
|---|---:|---:|---:|---:|---:|---:|
|3|$0|5|$2,250|1|0|1|
|3|$2,000|5|$2,250|1|0|1|
|6|$0|6|$2,700|5|3|1|
|6|$2,000|5|$2,250|5|3|1|

No live payouts in these runs. First funded payout May 18 for both sizes and reserves. Original account payouts: May 18, June 11, June 30, July 16, July 31. Six payouts in the no-reserve six-contract case span multiple accounts.

Verification: integer-cent simulator vs independent Decimal oracle, 2,000 randomized cases spanning fresh and pre-funded initialization; 18 boundary assertions including four new qualifying days after the existing one, subsequent five-day reset, fresh replacement initialization. Source paths SHA-256 verified. All 101 signal days per execution case partition into trades/cooldown skips exactly. Stress-one-tick rerun has identical payout counts but different balances. Does not prove real fills, NT8 indicator parity, or future profitability. Entry filter was selected after inspecting this sample.

Artifacts: summary.csv, stages.csv, ledger.csv, payouts.csv, skipped.csv, audit.json. Amounts ending _cents are integer USD cents. No bot/runtime changes.
