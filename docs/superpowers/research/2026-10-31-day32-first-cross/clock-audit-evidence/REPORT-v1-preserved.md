# ORB EMA9 clock recheck — 2026-10-31

Assessment: **Needs revision for old EMA9 ORB result reports; share this clock-sensitivity rerun with caveats.**

[A] Replayed 122 retained data sessions in the requested 2025-10-20–2026-04-19 window. Reproduced all 714 non-provider-excluded base rows against the original saved CSV, including every returned field, before comparing the receipt-clock variant. Another 18 base rows remain excluded by the existing provider-quality gate. This is not a claim that all calendar days have usable data.

## Finding and scope

[A] Original ema_orb.py groups intraday trades by event timestamp (bars at lines 30–32; oracle line 78), but warmup loads pre-aggregated OHLCV (lines 106–113 and 126–134). The new variant groups bars by receive timestamp to make the bar clock consistent with that warmup. The replay reads the complete selected contract around the open so events received after the open are not lost by the old event-time prefilter. Execution eligibility rules otherwise remain unchanged.

This is a controlled clock-model comparison, not proof that Databento receive timestamps reproduce NinjaTrader/TradingView chart timestamps exactly. The old late-receipt exclusions were conservative causality guards; changing the bar definition legitimately changes which records fall in those ranges.

[A] 32 base rows differ (TP20 and TP30 are separate rows): 5 previously excluded day/timeframe combinations become trades; 2 existing combinations change entry prices; 9 other combinations change signal-source row only. Affected economic inputs: 2025-10-31 ORB1m; 2025-11-04 ORB2m; 2025-12-04 ORB2m; 2025-12-16 ORB2m; 2026-02-10 ORB5m; 2026-02-27 ORB5m; 2026-03-12 ORB1m.

[A] Recomputed half-box/full-box exits for the complete retained base sample. Recomputed 91 old/new downstream policy pairs on the 7 changed economic-input combinations, checked those old outcomes/rows/points against hashed original CSVs, and updated 39 downstream setting summaries. Unchanged entry/side/width and tick paths give unchanged economic results for the remaining rows; that dependency argument is [B], not a fresh replay of every unchanged downstream row.

## Base methods: old versus consistent receipt-clock variant

Points below use observed trade prints at stops, not ideal stop prices. All are gross points per one contract, before commissions and realistic execution costs. OPEN trades are counted and excluded from closed-trade totals.

| Method | ORB | Old TP/SL/open | New TP/SL/open | Old gross points | New gross points | Delta |
|---|---:|---|---|---:|---:|---:|
| SL20_TP20 | 1m | 53/65/0 | 55/65/0 | -240.500 | -200.500 | +40.000 |
| SL20_TP20 | 2m | 64/51/0 | 64/53/0 | +259.250 | +219.250 | -40.000 |
| SL20_TP20 | 5m | 58/53/0 | 58/54/0 | +99.500 | +79.500 | -20.000 |
| SL20_TP30 | 1m | 46/72/0 | 48/72/0 | -60.500 | -0.500 | +60.000 |
| SL20_TP30 | 2m | 49/66/0 | 49/68/0 | +149.250 | +109.250 | -40.000 |
| SL20_TP30 | 5m | 44/67/0 | 44/68/0 | -20.500 | -40.500 | -20.000 |
| halfbox_fullbox | 1m | 44/74/0 | 44/76/0 | +324.750 | +289.000 | -35.750 |
| halfbox_fullbox | 2m | 47/67/1 | 48/68/1 | +1276.750 | +1317.500 | +40.750 |
| halfbox_fullbox | 5m | 35/72/4 | 35/73/4 | +246.750 | +214.750 | -32.000 |

## Downstream policy comparisons

Arguments identify (RR, lock points) where applicable. ema_box_lock2025 is a one-time lock at +20/+25 from entry, NOT a continuous trailing stop. Nominal and observed values are both retained in JSON; this table uses observed stop trade prints.

| Engine / args | ORB | Old TP/SL/BE/LOCK/open | New TP/SL/BE/LOCK/open | Old gross points | New gross points |
|---|---:|---|---|---:|---:|
| ema_rr3 [] | 1m | 34/84/0/0/0 | 34/86/0/0/0 | +494.750 | +459.000 |
| ema_rr3 [] | 2m | 35/76/0/0/4 | 36/77/0/0/4 | +1495.750 | +1569.750 |
| ema_rr3 [] | 5m | 27/77/0/0/7 | 27/78/0/0/7 | +437.000 | +405.000 |
| ema_box_be [2] | 1m | 33/57/28/0/0 | 33/58/29/0/0 | +136.750 | +124.500 |
| ema_box_be [2] | 2m | 32/51/31/0/1 | 33/52/31/0/1 | +681.000 | +721.750 |
| ema_box_be [2] | 5m | 27/55/26/0/3 | 27/56/26/0/3 | +307.500 | +275.500 |
| ema_box_be [3] | 1m | 23/57/38/0/0 | 23/58/39/0/0 | +317.500 | +305.250 |
| ema_box_be [3] | 2m | 22/51/40/0/2 | 22/52/41/0/2 | +671.750 | +646.000 |
| ema_box_be [3] | 5m | 18/55/32/0/6 | 18/56/32/0/6 | +145.000 | +113.000 |
| ema_halfbox_lock [2] | 1m | 4/57/0/57/0 | 4/58/0/58/0 | +77.500 | +88.750 |
| ema_halfbox_lock [2] | 2m | 0/51/0/64/0 | 0/52/0/65/0 | +612.750 | +619.750 |
| ema_halfbox_lock [2] | 5m | 0/55/0/56/0 | 0/56/0/56/0 | +359.750 | +327.750 |
| ema_box_lock [2, 5] | 1m | 27/57/0/34/0 | 27/58/0/35/0 | +13.750 | +6.500 |
| ema_box_lock [2, 5] | 2m | 29/51/0/34/1 | 30/52/0/34/1 | +681.000 | +721.750 |
| ema_box_lock [2, 5] | 5m | 26/55/0/27/3 | 26/56/0/27/3 | +397.500 | +365.500 |
| ema_box_lock [2, 10] | 1m | 21/57/0/40/0 | 21/58/0/41/0 | -33.750 | -36.000 |
| ema_box_lock [2, 10] | 2m | 24/51/0/39/1 | 24/52/0/40/1 | +574.250 | +558.500 |
| ema_box_lock [2, 10] | 5m | 25/55/0/28/3 | 25/56/0/28/3 | +470.750 | +438.750 |
| ema_box_lock [2, 15] | 1m | 16/59/0/43/0 | 16/60/0/44/0 | -30.250 | -27.500 |
| ema_box_lock [2, 15] | 2m | 18/52/0/44/1 | 18/53/0/45/1 | +426.000 | +415.250 |
| ema_box_lock [2, 15] | 5m | 23/55/0/30/3 | 23/56/0/30/3 | +496.250 | +464.250 |
| ema_box_lock [3, 5] | 1m | 18/57/0/43/0 | 18/58/0/44/0 | +125.500 | +118.250 |
| ema_box_lock [3, 5] | 2m | 18/51/0/44/2 | 18/52/0/45/2 | +533.250 | +512.500 |
| ema_box_lock [3, 5] | 5m | 17/55/0/33/6 | 17/56/0/33/6 | +242.500 | +210.500 |
| ema_box_lock [3, 10] | 1m | 14/57/0/47/0 | 14/58/0/48/0 | +84.000 | +81.750 |
| ema_box_lock [3, 10] | 2m | 12/51/0/51/1 | 12/52/0/52/1 | +274.000 | +258.250 |
| ema_box_lock [3, 10] | 5m | 16/55/0/34/6 | 16/56/0/34/6 | +309.500 | +277.500 |
| ema_box_lock [3, 15] | 1m | 10/59/0/49/0 | 10/60/0/50/0 | +19.500 | +22.250 |
| ema_box_lock [3, 15] | 2m | 9/52/0/53/1 | 9/53/0/54/1 | +194.500 | +183.750 |
| ema_box_lock [3, 15] | 5m | 15/55/0/36/5 | 15/56/0/36/5 | +411.500 | +379.500 |
| ema_box_lock2025 [2, 20] | 1m | 11/62/0/45/0 | 11/63/0/46/0 | -143.500 | -135.750 |
| ema_box_lock2025 [2, 20] | 2m | 12/53/0/49/1 | 12/54/0/50/1 | +327.500 | +321.750 |
| ema_box_lock2025 [2, 20] | 5m | 18/55/0/36/2 | 18/56/0/36/2 | +366.750 | +334.750 |
| ema_box_lock2025 [2, 25] | 1m | 8/63/0/47/0 | 8/64/0/48/0 | -67.250 | -54.500 |
| ema_box_lock2025 [2, 25] | 2m | 9/53/0/52/1 | 9/54/0/53/1 | +356.000 | +355.000 |
| ema_box_lock2025 [2, 25] | 5m | 17/55/0/37/2 | 17/56/0/37/2 | +511.250 | +479.250 |
| ema_box_lock_rr5 [5, 5] | 1m | 13/57/0/46/2 | 13/58/0/47/2 | +227.250 | +220.000 |
| ema_box_lock_rr5 [5, 5] | 2m | 9/51/0/52/3 | 9/52/0/53/3 | +164.250 | +143.500 |
| ema_box_lock_rr5 [5, 5] | 5m | 8/55/0/37/11 | 8/56/0/37/11 | -294.250 | -326.250 |

## Other report status

- [A] Pure ORB breakout/retest/body-confirmation paths do not consume the EMA warmup. Their existing tests passed, but their complete historical ledgers were not replayed in this clock audit. Do not call this whole-engine certification.
- [A] Funded E consumes EMA9 ORB2m entries. Its input changed, so prior payout/liquidation/account-count totals must not be represented as reverified. The account lifecycle was NOT rerun here. A/D do not consume this EMA entry path; that establishes dependency scope only, not a fresh cashflow audit.
- [A] Separate overnight video-strategy audit: 109 sessions, 2,150,995 decision EMA pairs checked against pandas; maximum error 1.019e-10 ticks. 50,685 completed 2m close comparisons, zero missing buckets/differences after clock consistency. 218 independent entry/exit checks passed. Old versus new economic ledger fields unchanged on all 218 trades. The custom video indicator remains closed-source, so exact indicator parity is unproven.

## Tests and reproducibility

[A] 139 existing tests passed: test_ema_orb, test_ema_halfbox, test_ema_rr3, test_ema_box_be, test_ema_box_lock, test_ema_halfbox_lock, test_ema_box_lock2025, test_ema_box_lock_rr5, test_body_retest, test_retest_variants, test_stop_variants. Three new clock regression tests passed, including actual simulate/independent-oracle calls and an assertion that the old engine differs. Twenty-five overnight engine tests passed.

Run recheck_orb_clock.py and recheck_orb_downstream.py with databento installed. All outputs are exclusive-created; preserve prior runs. Source input hashes are checked by the decoder and original report manifests. No new download, trading, deployment, live DB write, or service operation occurred.

Audit checklist reference: e962377badbccbf1bf4b325138eb40cbf17ab894 — AUDIT-CHECKLIST: m461 locked pep styles class.

Original engine and immutable inputs remain preserved; receipt-clock scripts are separate research variants. Failed audit drafts are preserved: receipt audit v1 compared a receipt timestamp under an event-time label; v2 counted 6 post-cutoff buckets; v3 corrects the audit bookkeeping and is the evidence used here.

Limitations: trade-print execution proxy, no queue position/latency proof, sample exclusions, unclosed trades excluded from closed totals, model-selection bias across many tested settings, and no proven chart-provider clock equivalence. Nominal half-tick stops are not exchange prices; observed-stop totals avoid reporting a fractional-tick stop fill as executable.
