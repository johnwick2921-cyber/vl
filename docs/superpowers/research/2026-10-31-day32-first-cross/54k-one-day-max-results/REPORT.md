# Maximum eligible withdrawals, initial $54,000 and one qualifying day

Period May 1–October 30, 2026. E EMA9 ORB2m before 09:00 Houston; half-box SL, whole-box TP, no stop movement. Initial $4,000 profit is pre-existing. Funded withdrawal = min(50% remaining profit, $2,000, balance above $50,100 plus owner reserve), minimum $500, 90% received; five qualifying days per cycle. Fee assumption $1/MNQ round trip.

With $2,000 owner reserve: 3 MNQ receives $6,294.60; 6 MNQ receives $8,730.00; five funded payouts each, zero live payouts. One-tick stress receives $6,256.80 / $8,697.60 respectively. No extra owner reserve gives $7,114.95 / $9,466.20, with five/six funded payouts respectively.

2,000 randomized integer-vs-Decimal oracle comparisons and 18 boundary assertions pass; source paths hash verified. These checks validate the modeled calculation, not actual execution or provider approval. Next-signal live transition after payout five is hypothetical. Live daily excess withdrawals unchanged from prior simulation. Live breach followed by 14-day cooldown and hypothetical funded replacement; no evaluation time/purchase fees. Both sizes breach live in this sample. Six-contract case also breaches three funded accounts. Results are withdrawals, not net investment profits; residual funded balances at live transition are not included in received cash. Original entry-time filter selected after observing sample.

See audit.json and CSVs for complete ledger and stage accounting. No runtime changes.
