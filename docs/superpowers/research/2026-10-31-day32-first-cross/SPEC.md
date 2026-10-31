# Day 3.2 — first crossover overnight research

Status: engine preparation; no measured strategy results yet.

## Owner requirements
MNQ; 2-minute EMA9/EMA34. Only the FIRST crossover of each overnight
session is eligible. Upcross buys first EMA9 retest; downcross sells it.
Moving limit follows EMA9. At most one filled trade, never a later cross.
TP15 points; two separate tests SL15 and SL16. No trailing or break-even.
This supersedes the video's multiple-cross examples for this backtest only.

## Prespecified modelling conventions (not proven by the video)
Sample session dates 2025-10-20 through 2026-04-19; weekdays only initially,
with exchange/data exclusions recorded individually. Session starts at 17:00
America/Chicago the previous calendar day and ends at 08:30 on the labelled
date. DST is converted per date. No 21:00 cutoff.
Use intrabar crossover detection and a moving limit because the examples show
forming candles and a moving resting order; exact amendment cadence is not
specified in the video. This is a modelling assumption, not claimed NT8 parity.
Forming EMA = previous completed EMA + alpha*(current price-previous EMA).
It must not compound on each tick. EMA seed uses 1000 prior closed 2m bars of
the same raw contract, without overnight resets. Instrument selection must use
only information already available before the overnight open.

Process previous resting orders before amending from the new market record.
No signal, fill, and exit on the same record. A new/amended limit becomes eligible
only on subsequent records. BUY limits round down to a tick; SELL round up.
Only non-marketable limits are armed. An opposite crossover before a fill
cancels the first opportunity permanently for that session. No second signal.
Primary model is trade-touch fill; this does not prove queue position or fills.
Stop gaps use the first observed adverse price, so actual losses may exceed
15/16 points; also report nominal stop loss separately. Target fills are capped
at their limit price. No fees are invented. Still-open positions at 08:30 are
reported separately with an observed mark, not labelled TP or SL.

## Data and audit
Existing daytime TBBO starts near 07:50 and cannot prove overnight outcomes.
Existing ETH 1m data may seed EMA, not establish tick execution order.
130 metadata quotes succeeded: $3.098402023317 for overnight TBBO (3 raw
contracts MNQZ5, MNQH6, MNQM6). Paid retrieval awaits owner cost approval;
no paid retrieval has occurred for this task. Requested timestamps include
one minute after cutoff for validation, but the strategy excludes that minute.
Video SHA256: 3c8e9dd672114cadc3dad11aaa84abae0daa5eccc9576c798dfe5186858ddb07.
Transcript evidence: /home/hoang/fleet-1017/out/day32-whisper-20261031/.
Audit reference: docs/superpowers/AUDIT-CHECKLIST.md, latest change at accept
 e962377badbccbf1bf4b325138eb40cbf17ab894 (2026-10-30).
Branch base: 7e2231f01; claim: 10136992a. Offline only; no live service changes.
