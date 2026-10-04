> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# VL • Trading-policy research

Targets, confirmation, level ranking and plan staleness

Prepared for the VL owner • September 8, 2026 • Research and proposals only

Four specialist research lanes, reconciled through independent source checks and frozen-audit arithmetic. The analytical standard is professional intraday index-futures preparation. No personal trading track record is claimed.

## 01 • Decisions this evidence supports

**The four policies deserve separate validation before being treated as trading advantages.** The research does not establish a universally superior first target, confirmation interval, confluence formula or plan expiry time for NQ/MNQ. That is a bounded finding about the sources reviewed, not proof that useful policies cannot exist.

| Question | Recommended research decision | Confidence and boundary |
|---|---|---|
| First target | Make the first obstacle and the action there explicit. Compare exit policies under common entries and initial risk. | High confidence in the need for coherent economics; insufficient evidence to prescribe one optimal exit. |
| Confirmation | Specify correct event timing first, then compare alternative confirmation rules using attainable entry prices. | High confidence in causal correctness; low confidence in universal 5-minute superiority. |
| Ranking | Preserve the full candidate map and test whether rank improves conditional reaction and net value. | High confidence that existing selected-only history cannot calibrate the score; weights remain unvalidated. |
| Staleness | Separate an enduring reference map from conditional entry permission. Test publication-time and pre-entry revalidation. | Strong rationale for state-aware review; no verified universal TTL or desk-wide reread interval. |

**Nothing has been changed in the trading system.** This report does not authorize implementation, new orders, altered stops, prompt changes or deployment. It gives reviewable specifications and falsifiable experiments for a later decision.

The distinction that matters throughout is between a measurement being correct, a setup being economically coherent, and a policy having demonstrated net advantage. Correct arithmetic proves only the first. A plausible narrative addresses the second. The third needs an appropriate comparison with uncertainty.

**Reading route.** Section 02 corrects the audit premises. Sections 03–06 answer the four questions. Sections 07–09 connect the policies, specify the proposed experiments and state the decision thresholds. Sections 10–11 document scope, verification, limitations and source access.

## 02 • What the frozen VL evidence actually says

This is a research extension of the [frozen Planner audit](https://github.com/johnwick2921-cyber/vl/blob/6095ca58fe5901ba398be374e4f9d3488d0bed6b/docs/superpowers/reports/2026-09-07-planner-preparation-audit/README.md), not a fresh audit of whatever version is running today. Source revision: **5457ac5accd97c3519bf6d16ead147a0db2ab0d0**. Published audit revision: **6095ca58fe5901ba398be374e4f9d3488d0bed6b**. Planner-history cutoff: **September 7, 2026, 22:17 Chicago**.

The retained sample contains **102 Planner versions**, **18 date/session groups**, **280 scenario documents**, and **111 complete entry–stop–arm-target geometries**. Repeated versions reuse market history; none of these counts is a count of independent trades. The other 169 scenarios must not receive invented entry or stop prices.

### Target arithmetic: two important corrections

**45/111, or 40.54%, have a first listed target at a positive distance below 1R.** This is an authored-geometry statistic. It is neither a losing-trade rate nor proof that 45 exits were executed. The original nominal Wilson interval, 31.9%–49.8%, treats rows like independent Bernoulli observations and therefore must not be used as confidence in an underlying trading edge. [Frozen scenario evidence](https://github.com/johnwick2921-cyber/vl/blob/6095ca58fe5901ba398be374e4f9d3488d0bed6b/docs/superpowers/reports/2026-09-07-planner-preparation-audit/all-scenarios.csv)

The stronger premise “every scenario clears the R:R gate on a final target” is not established. A new independent calculation finds **6/111 authored arm-target ratios below 2R**, with a minimum of **0.55625R**. The six records are retained below for reproducibility.

| Frozen logical reference | Scenario | Authored arm-target R |
|---|---|---:|
| 2026-08-31:NY:v3 | S1 | 1.237336 |
| 2026-09-01:LONDON:v4 | S1 | 0.905574 |
| 2026-09-01:LONDON:v6 | S1 | 1.593750 |
| 2026-09-01:NY:v3 | S2 | 0.556250 |
| 2026-09-01:NY:v5 | S1 | 0.601695 |
| 2026-09-04:NY:v2 | S1 | 0.838150 |

Calculation: R = |arm target − entry| / |entry − stop|, using the stored arm fields. All 111 stored arms have stop and arm target on the appropriate side of entry. Ratios are before rounding, costs and later stop composition. Historical configuration and admission events are not reconstructed, so these six rows do **not** prove a gate malfunction. [Scenario CSV](https://github.com/johnwick2921-cyber/vl/blob/6095ca58fe5901ba398be374e4f9d3488d0bed6b/docs/superpowers/reports/2026-09-07-planner-preparation-audit/all-scenarios.csv); [frozen arm R:R gate](https://github.com/johnwick2921-cyber/vl/blob/5457ac5accd97c3519bf6d16ead147a0db2ab0d0/trader/armed_executor.go#L1905)

There is a real semantic distinction: the R:R calculation uses the separate arm target, whereas the target chain lists intermediate prices. Four arm targets are absent from that chain even allowing half a tick. A displayed chain, a gate input and an executable order are therefore different evidence objects. The audit does not establish that every intervening level is an opposing obstacle for every scenario. [Audit target analysis](https://github.com/johnwick2921-cyber/vl/blob/6095ca58fe5901ba398be374e4f9d3488d0bed6b/docs/superpowers/reports/2026-09-07-planner-preparation-audit/README.md); [chain validation](https://github.com/johnwick2921-cyber/vl/blob/5457ac5accd97c3519bf6d16ead147a0db2ab0d0/kernel/plan_doc.go#L913)

### Confirmation and ranking: what was proven

Four synthetic probes executed against the frozen confirmation functions demonstrated premature acceptance of unfinished five-minute bars or acceptance without the intended sweep-before-reclaim ordering. These are counterexamples to implementation semantics, not four historical losing trades and not evidence that the intended rules would be profitable. They have not been rerun here against a newer production build. [Probe results](https://github.com/johnwick2921-cyber/vl/blob/6095ca58fe5901ba398be374e4f9d3488d0bed6b/docs/superpowers/reports/2026-09-07-planner-preparation-audit/confirmation-probe.json); [frozen confirmation implementation](https://github.com/johnwick2921-cyber/vl/blob/5457ac5accd97c3519bf6d16ead147a0db2ab0d0/kernel/plan_confirm.go#L49)

All **600 candidate records** have empty score components. They are an intermediate pool across 25 reads, not a complete history of all raw detections. The **124 validity-filtered touch observations** contain 46 holds, 41 breaks and 37 ambiguous outcomes, across only two date groups and selected demand/supply/order-block levels. There is no valid unselected comparison group. Current validity filtering exists; the older claim that every duplicate necessarily enters the displayed rate is not repeated. [Frozen metrics](https://github.com/johnwick2921-cyber/vl/blob/6095ca58fe5901ba398be374e4f9d3488d0bed6b/docs/superpowers/reports/2026-09-07-planner-preparation-audit/metrics.json)

### Staleness: generation time is not a universal TTL

The captured authoring calls were 378.0 seconds for v1, 683.7 plus 423.3 seconds for the failed/repair sequence producing v2, and 683.4 seconds for v3: approximately **6.30, 18.45 and 11.39 minutes** respectively. The shorthand “11–18 minutes” omits the faster first version. These durations are not a measured distribution of all plans, nor exact end-to-end data age. The initial facts survive retries while some specialized checks read newer bars. [Authoring log](https://github.com/johnwick2921-cyber/vl/blob/6095ca58fe5901ba398be374e4f9d3488d0bed6b/docs/superpowers/reports/2026-09-07-planner-preparation-audit/planning-log.txt); [request and retry handling](https://github.com/johnwick2921-cyber/vl/blob/5457ac5accd97c3519bf6d16ead147a0db2ab0d0/trader/auto_trader_planner.go#L1508)

The frozen audit also corroborated session extrema against retained bars. A changed bias or a persistent old level is not automatically wrong. The concern is whether the plan's claims remained true when publication or entry permission occurred.

## 03 • Targets: where should the first one be?

**Answer:** the first opposing structure should be identified as a decision location, but the evidence does not justify automatically making it the mandatory full exit. A first target below 1R can be viable with a sufficiently favorable outcome distribution and costs; a final target above 2R can be unattractive if rarely reached. A professional preparation document must explain the path and the action, not merely display an attractive ratio.

This is a recommendation about the clarity of the plan, not a validated VL trading rule. It must remain possible to decline a setup, wait for a different entry, or retain a runner only when the chosen policy makes that behavior explicit.

### What the target literature contributes

**A useful matched counterexample, with important limits.** Wang, Wu and Chung study Taiwan index futures using minute data from January 4, 2010 to March 25, 2015, covering 1,291 days. Tables 2 and 4 report 1,180 trades per arm. With a 30-point stop unchanged, adding a 60-point target reduces average gross profit from **5.09 to 3.69 points**, raises win rate from **44.58% to 46.10%**, and reduces maximum drawdown from **659 to 581 points**. The conclusion explicitly excludes transaction costs. Entry detail, intrabar ordering and a held-out replication are insufficiently established. A paired-return confidence interval is unavailable here. This is evidence of an objective-dependent trade-off, not a prescription for NQ. [Wang, Wu & Chung, 2015](https://www.researchgate.net/publication/304299040_Empirical_Evaluations_on_Momentum_Effects_of_Taiwan_Index_Futures_Market)

**Other-index evidence is mixed.** Yu and Rentzler examine S&P 500 futures day-trading rules over September 1988–June 2003. Accessible abstract and indexed excerpts suggest that some stop-loss rules assist trend-following, while profit-lock conclusions are less compelling and depend on the setup. Full methods and costs were inaccessible in this review, so this paper cannot select VL parameters. [Yu & Rentzler, 2004](https://joim.com/issue/2004q1/)

**Theory is conditional.** Leung and Zhang formulate optimal entry and liquidation with a trailing stop under diffusion-model assumptions, costs and discounting. Their result can include an earlier sell limit; another case permits continuing until the trailing stop. This establishes that the answer depends on the process and objective, not that a particular ATR multiplier or structural target is optimal in index futures. No empirical NQ sample is involved. [Leung & Zhang, theorem and qualification](https://arxiv.org/pdf/1701.03960v2)

**ATR and partial exits have weaker practical evidence.** Davey's daily-bar practitioner experiments include ES and other futures, random entries and optimized ATR, trailing and indicator exits. Different methods change exposure and subsequent entries; costs and fixed-initial-risk equivalence are not sufficiently specified. They motivate comparisons, not intraday NQ calibration. Bryant's mini-Russell example compares partial, target and trailing approaches, but allocation and parameter optimization change the result. It provides no universal partial-exit advantage. [Davey, 2010](https://kjtradingsystems.com/Act/davey0710.pdf); [Bryant, 2009](https://www.adaptrade.com/BreakoutFutures/Newsletters/Newsletter0309.htm)

### Policy comparison to put before the owner

| Candidate | Plausible use and principal trade-off | What must be fixed in advance |
|---|---|---|
| Next opposing structure | Makes nearby impediments explicit; can truncate a continuing trend or reward extremely small moves. | Which level qualifies, near/far zone edge, buffer, full exit versus review point, ties and missing structures. |
| Fixed multiple of initial R | Clean baseline and easy risk accounting; may ignore market obstacles and available session range. | R measured at feasible entry, protective stop, discrete target grid, end-of-session exit. |
| ATR or volatility-scaled target | Adapts price distance to measured movement; can expand after a shock or depend on stale estimates. | Exact estimator, lookback, completed data, freeze/update rule and relation to structural stop. |
| Partial then runner | Mixes realized near reward with continuation exposure; reduces exposure to large winners and adds management choices. | Integer contracts, allocation, runner target/stop, break-even timing, all fees and total initial dollar risk. |
| Trailing or condition exit | Can retain long tails; gives back open profit and depends on path and market liquidity. | Trigger price source, update timing, ratchet rule, thesis failure, time exit and gap handling. |
| No preset take-profit | Useful uncapped benchmark; not an absence of risk control. | Protective stop, session/time liquidation and any thesis invalidation. |

Structural placement also has implementation choices with economic consequences. A sell limit just before a resistance zone may fill more often than one beyond it; a target exactly at the high can be touched without the order filling. The replay must model the chosen order and queue assumption rather than count every chart touch as profit.

“ATR-scaled” must not silently use whatever variable happens to be named ATR. The frozen selector's daily-range proxy differs from the regime's daily ATR14. A volatility experiment must identify the estimator and the information available at the decision. [Frozen range construction](https://github.com/johnwick2921-cyber/vl/blob/5457ac5accd97c3519bf6d16ead147a0db2ab0d0/kernel/levels_assemble.go#L291)

### The arithmetic a target policy must survive

Let a trade have only two gross outcomes: +bR with probability p, or −1R otherwise; let total cost be cR. Then:

**E[net R] = p × b − (1 − p) − c; break-even p = (1 + c)/(1 + b).**

| Winning payoff b | Break-even without costs | With illustrative cost of 0.04R |
|---:|---:|---:|
| 0.5R | 66.67% | 69.33% |
| 1R | 50.00% | 52.00% |
| 2R | 33.33% | 34.67% |
| 3R | 25.00% | 26.00% |

These are checked mathematical examples, not estimated VL win rates or quoted transaction fees. Multiple targets, time exits, partials, gaps and variable losses require the full outcome distribution instead.

At a first target of +0.5R, suppose continuing can end only at +2R or −1R. With no incremental costs and an expected-P&L objective, continuing beats closing at +0.5R only when the conditional probability of +2R exceeds 50%. That conditional probability is unknown here. An attractive initial R:R ratio cannot answer the decision after the first obstacle.

A 50% exit at +0.5R followed by a 50% exit at +3R yields **+1.75R**, not +3R, when both succeed. With one MNQ contract, a half-contract exit is infeasible. Adding another contract to enable scaling changes risk; it is not a free comparison. CME specifies MNQ at $2 per index point and a 0.25-point tick worth $0.50. [CME specifications](https://www.cmegroup.com/markets/equities/nasdaq/micro-e-mini-nasdaq-100.contractSpecs.html)

### Target proposal and falsification

**Proposed scenario contract:** record entry zone; trigger; feasible entry; structural invalidation; protective stop; first opposing obstacle and provenance; planned response there; final objective; estimated net economics; and the condition for standing aside.

The audit's September 7 v2 S2 long illustrates why: entry 29664.50, stop 29640.00 and arm target 29721.25 imply 24.50 points risk and 2.316R to the arm target, while the first listed target is only 0.282R away. These facts support asking what happens at the first target. They do not establish that exiting there or holding through it is superior. [Captured scenario evidence](https://github.com/johnwick2921-cyber/vl/blob/6095ca58fe5901ba398be374e4f9d3488d0bed6b/docs/superpowers/reports/2026-09-07-planner-preparation-audit/all-scenarios.csv)

**Proposed test:** freeze the same eligible entries and protective stops, then replay the six exit families with stated costs. Report paired net P&L, drawdown, tail contribution, time in market and first-obstacle behavior. Follow with a whole-policy test that permits exits to change capacity for later entries. Reject an exit change if its apparent advantage vanishes after attainable fills, costs, chronological validation or equal initial risk. Do not “repair” a weak payoff by moving a valid structural stop inside the thesis-failure point.


## 04 • Confirmation: what counts as evidence a level held?

**Answer:** confirmation is a measurable event that must precede the decision it authorizes. It is not proof of future success. A completed five-minute close is one candidate observation; it does not automatically dominate a tick cross, dwell time, consecutive bars or order-flow condition.

Correcting unfinished-bar or reversed-sequence semantics is logically separate from selecting the trading rule. A future implementation can be perfectly faithful to an unprofitable rule. Conversely, a promising rule cannot be evaluated with future information or impossible event ordering.

### Define the alternatives before comparing them

| Rule | Required definition | Failure mode or information limit |
|---|---|---|
| Tick through | Last trade, bid, ask or mid; boundary and minimum excursion; first versus repeat crossing. | A momentary crossing does not establish sustained acceptance. Spread changes can alter the event. |
| Completed 5m close | Exact session alignment, bar close timestamp, boundary and required side; only finalized information. | A crossing at 10:04:59 followed by a 10:05 close is not five minutes of holding. |
| Two-bar rule | Two consecutive completed bars, required side and minimum displacement, reset on failure. | Costs additional time and may enter after most available reward has disappeared. |
| Time-at-price | Continuous dwell versus cumulative occupancy, zone width, timer reset and allowable interruptions. | Exact occupancy is unavailable from ordinary OHLCV alone; no ticks does not necessarily mean acceptance. |
| Volume condition | Total volume, relative volume, aggressor imbalance, queue imbalance or order-flow imbalance; causal baseline. | These are different measurements. High total volume does not specify buyer or seller control. |
| Sweep then reclaim | Named anchor, excursion side/depth, ordered excursion and reclaim, expiry and association with the same episode. | A previous reclaim plus a later sweep must not qualify. A chart excursion does not prove hidden stops were triggered. |

An illustrative five-minute bar covering [10:00, 10:05) remains incomplete at 10:04. A previously completed bar is valid information but may predate the current encounter and therefore fail the intended sequence. Both “is this bar closed?” and “does it belong to this setup episode?” must be answered.

OHLCV can establish some ordering. A low below the boundary followed by a close above it establishes at least one excursion before that close. It cannot recover every crossing, exact dwell duration, queue state or aggressor flow. If two required events could occur in either order inside a bar, preserve ambiguity or use finer archived data. Never assume the favorable path.

### What the empirical evidence measures

**Microstructure supports distinguishing information types.** Cont, Kukanov and Stoikov analyze 50 S&P 500 stocks in April 2010. Order-flow imbalance explains short-interval price changes better than raw traded volume in their design. Crucially, the relationship is contemporaneous: it is not a forecast of profitable future NQ entries, and genuine order-flow imbalance requires order-book event information. It cannot be reconstructed by renaming minute-bar volume. [Cont, Kukanov & Stoikov](https://arxiv.org/html/1011.6402v3)

Gould and Bonart examine queue imbalance and the direction of the next mid-price change in ten Nasdaq stocks using 2014 data. Predictive classification improves, particularly for large-tick stocks, but the study uses a random train/test partition and does not establish net futures trading after latency, fills and costs. Nasdaq-listed stocks are not Nasdaq-100 futures. [Gould & Bonart, 2015 version](https://arxiv.org/abs/1512.03492v1)

**The opposing mechanism matters.** Osler's FX cascade study links stop-loss concentrations to continuation rather than automatic reversal. Minute exchange-rate data from 1996–1998 and a separate bank-order sample from 1999–2000 are not contemporaneous. It is mechanism evidence and a reason to test both continuation and rejection, not a controlled sweep/reclaim strategy comparison. [Osler, Staff Report 150](https://www.newyorkfed.org/medialibrary/media/research/staff_reports/sr150.pdf)

**Direct index-futures vendor evidence is suggestive, not decisive.** TradingStats reports ES 30-minute opening-range double breaks falling from 47.9% under wick detection to 33.7% with a five-minute close. Its “continuation” outcome asks whether the session closes on the corresponding side of the range midpoint. That is not the P&L from entering after confirmation. Delaying detection changes classifications mechanically, so these figures cannot establish a net edge. Data/code replication and costed execution were not verified. [TradingStats, confirmation table and outcome definition](https://tradingstats.net/orb-breakout-strategy-guide/)

Cascade Indicators reports negative NQ confirmation differences at a 30-minute horizon for both proxy flow and actual aggressor-flow variants. It is useful counterevidence to an automatic volume-confirmation benefit, but repeated crossings, limited samples, missing execution economics and unverified independent preregistration limit its weight. The conflicting vendor results do not measure the same rule or outcome. [Cascade Indicators disclosure](https://www.cascadeindicators.com/research)

### The price paid for confirmation

Consider a purely illustrative long at 100, stop 90 and target 125. Gross reward/risk is 25/10 = 2.5R. If confirmation arrives at a feasible entry of 103 with the same stop and target, reward/risk is 22/13 = **1.6923R**. Holding dollar risk fixed may require fewer contracts; holding contract count fixed increases dollar risk. Either choice must be explicit.

A test that shows fewer failed breakouts but assumes the original entry price is invalid. A test that compares only confirmed winners against all unconfirmed opportunities is also invalid. Delayed rules must carry the cost of missed trades, poorer entries and timeouts.

### Confirmation proposal and falsification

**Proposed state sequence:** candidate encounter → required excursion, if any → confirmation becomes observable → current plan/risk permission checked → executable entry opportunity → resolution or timeout. Track timestamps and episode IDs throughout. This is a proposed specification, not a claim that the live bot currently implements it.

Compare rules from the same initial encounter time. Keep never-confirmed cases as zero-trade outcomes for per-opportunity economics, with separate per-trade statistics. Record confirmation delay, entry deterioration, avoided losses, missed winners and expiry. Use both a common encounter-to-session horizon and a secondary entry-relative horizon; one cannot silently replace the other.

If the historical feed lacks the data required for a rule, mark that arm **not testable**, rather than synthesize tick flow or time-at-price from candles. Test mirrored long/short sequences, boundary equality, missing and delayed bars, reconnects, duplicate messages, backfilled history and out-of-order arrival.

Reject a more elaborate confirmation if it fails to improve net value per initial opportunity beyond the predeclared minimum effect, or if it relies on prices unavailable when its evidence completed. If uncertainty is wide, the result is inconclusive. It is not a license to choose the most appealing chart examples.

## 05 • Level ranking: does confluence predict reaction?

**Answer:** published research gives reasons to study some level features, but it does not calibrate VL's grading ladder. Discovering a useful reference, predicting that price will reach it, predicting a reaction after touch, and ranking executable opportunities are separate tasks.

The existing weights are **uncalibrated heuristics**, not proven errors. A multiplier of 1.2 does not mean 20% more chance of a reaction. A grade of A is not a probability unless calibration demonstrates that interpretation.

### Evidence that supports and challenges ranking

**The strongest discovery-versus-ranking distinction:** Osler studies six firms' published FX levels with minute indicative quotes from 1996–1998 and artificial-level comparators. A bounce is being on the appropriate side fifteen minutes after an encounter, not uninterrupted survival or a costed trade. Published levels have information, but agreement across firms adds little consistent predictive power, and available strength categories do not rank bounces reliably. Some predictive information persists five business days. This is not a test of VL's distinct-family confluence or modern NQ. [Osler, definitions and Tables 10–12](https://www.newyorkfed.org/medialibrary/media/research/epr/00v06n2/0007osle.pdf)

**A testable memory hypothesis, with caveats:** Chung and Bellotti examine 2018 minute EURUSD, Lloyds and BRENT series using rolling-extrema zones, shuffled returns and simulations. More previous bounces often accompany higher bounce probability; elapsed-time decay is not uniform, with no significant decay coefficient for Lloyds in the reported groups. Longer discovery windows do not uniformly improve results. Zone width uses whole-series average price increments, which requires a past-only replacement for prospective testing. There is no costed held-out NQ comparison; BRENT construction is insufficiently specified for futures equivalence. [Chung & Bellotti, Eq. 1 and sections 3–4](https://arxiv.org/html/2101.07410v1)

**Clustering is not strength.** Osler's separate study of 9,667 conditional FX orders finds different stop-loss and take-profit clustering around round numbers, offering mechanisms for both reversal and acceleration. It does not estimate an out-of-sample ranking system. Chung and Chiang find price clustering in historical E-mini and floor-traded index futures, including Nasdaq-100. The often-cited 97% zero-digit statistic belongs to floor-traded Nasdaq futures, not NQ. Neither paper proves a high conditional net return at round levels. [Osler, Staff Report 125](https://www.newyorkfed.org/medialibrary/media/research/staff_reports/sr125.pdf); [Chung & Chiang, publisher abstract](https://onlinelibrary.wiley.com/doi/10.1002/fut.20196)

**Some positive confluence evidence is highly conditional.** Doucouliagos examines 20 Australian stocks using daily data, 1986–2001, and interactions between psychological prices and timing/exhaustion signals. Significant terms concern selected swing extrema; the analysis does not estimate success across all prospective touches. Variable selection and absent net-cost validation weaken transfer to intraday NQ. This is not the same as overlapping price-level families. [Doucouliagos working paper](https://www.deakin.edu.au/__data/assets/pdf_file/0006/402585/swp2003_06.pdf)

**Family prestige is not validation.** Tsinaslanidis, Guijarro and Voukelatos compare Fibonacci with non-Fibonacci zones on daily constituent-stock data. Tested Fibonacci zones do not demonstrate superior bounce probabilities or trading returns; broadening zones changes measured reactions. The study is gross of costs and not a test of incremental Fibonacci confluence in futures. [Tsinaslanidis et al., 2022](https://riunet.upv.es/entities/publication/07e4aacf-2e17-4914-9554-76e9eb41951b)

Chan and coauthors report neural-model improvements from support/resistance distance features in hourly FX data with a chronological split. That is feature inclusion, not ranking candidate levels. The body and abstract disagree on pair counts; stop specifications also conflict, and causal ZigZag availability and costs are insufficiently established. Model repetitions do not create independent market histories. [Chan et al., 2022](https://www.mdpi.com/2227-7390/10/20/3888)

The 2025 WIG20 volume-profile paper describes 163 recorded patterns, including overlapping consolidation/rebound categories and untouched cases. Its roughly 90% reaction headline is not an independent-touch success rate, has no competing-level comparator and does not establish POC superiority. Exact tradable instrument and volume construction are insufficiently clear for futures transfer. [Jóźwicki & Trippner, 2025](https://czasopisma.uni.lodz.pl/fipf/article/download/28410/27868/72359)

### What the VL score actually commits to

The frozen line formula is: **kind weight × freshness × (1 + 0.20 × capped distinct-family confluence) × HTF multiplier**. Confluence is capped at three; HTF multiplier is 1.2. This already groups families, so “just count distinct families” is not a newly discovered fix. The unresolved questions include family correlation, overlap distance, directional relevance and whether the increments predict anything. [Frozen score and family definitions](https://github.com/johnwick2921-cyber/vl/blob/5457ac5accd97c3519bf6d16ead147a0db2ab0d0/kernel/levels_score.go#L87)

The effective selector also includes grade floors/caps, anchor exceptions, merging, reservations and side balance. Testing only the numeric score would miss these overrides. A change in a displayed grade can result from the intended freshness multiplier; it is not automatically a computational bug.

| Feature or intervention | Proposed hypothesis | Essential alternative explanation |
|---|---|---|
| Kind weight | Some reference families add useful reaction information. | Different distance, width, session or selection frequency explains the result. |
| Prior bounces and age | Their interaction predicts conditional reaction. | Raw touches are not successful bounces; survivor selection creates apparent strength. |
| Confluence | Additional independently informative features improve ranking. | Shared source extremes or overlapping definitions duplicate evidence. |
| Higher timeframe | Longer-context references improve an outcome that matters. | Wider zones or different opportunities, rather than timeframe, drive the result. |
| Reservations and caps | A compact map preserves important decision roles. | A rejected entry candidate remains a critical obstacle, stop anchor or invalidation reference. |

A higher timeframe, a longer rolling discovery window and an older surviving level are different variables. None should be used as a proxy for the others without testing.

### Ranking proposal and falsification

**Preserve the market map separately from entry priority.** A level may be unsuitable for entry while remaining relevant as a target, obstacle or thesis-failure boundary. This is a proposed information contract. It does not require displaying every raw candidate equally prominently.

Retain all candidate states before selection, with original families and shared origins, formation/availability timestamps, zone bounds, distance, raw components, capped components, overrides, final score/rank/grade and exact exclusion cause. Use stable zone and episode IDs. Preserve both selected and unselected outcomes.

Evaluate separately: probability of touch before a fixed horizon; reaction conditional on touch; penetration/survival; and expected net value under a fixed trade policy. Define favorable/adverse excursions, timeout and episode reset before collecting labels. Untouched levels are not failed reactions. Replans and overlapping zones must not manufacture new independent observations.

Compare the current selector with distance/context alone, simple reduced-feature alternatives and, only if justified, a small regularized model. Calibrate on training data; evaluate on later untouched sessions. Test grouped feature removal so correlated inputs are not misread as independent causes.

Reject a bonus if its apparent gain disappears after controlling for distance, width, shared origin and selection bias. Keep the simpler rule if learning adds no material net value. The existing selected-only sample cannot answer this comparison, regardless of how sophisticated the statistical model is.


## 06 • Plan staleness: how long is preparation useful?

**Answer:** one age limit cannot describe all parts of a plan. A historical session high can remain a correct reference while an entry near it becomes obsolete within seconds. Conversely, elapsed time alone need not invalidate a still-relevant map.

The proposed approach is to version the reference map, the market-state interpretation and the permission to enter separately. Revalidation should answer whether the thesis and executable economics remain true. A fresh publication timestamp is not evidence that all underlying observations are fresh.

### Evidence on decay and professional practice

**News can change the state rapidly.** Andersen, Bollerslev, Diebold and Vega study nine futures markets over a common July 1998–December 2002 sample, with 15,764 five-minute observations around announcements. Most systematic response is concentrated in the first post-release five-minute interval; equity response varies with economic state. The study includes S&P 500 futures, not NQ, and cannot resolve precise within-bucket timing or choose a trading TTL. [Federal Reserve IFDP 871](https://www.federalreserve.gov/pubs/ifdp/2006/871/ifdp871.pdf)

Kurov and coauthors examine E-mini S&P 500 and Treasury futures around macro releases, principally January 2008–March 2014. Seven of 21 market-moving announcement types show robust pre-release drift beginning roughly 30 minutes beforehand. The result is not universal across releases, and conditioning on the subsequently known surprise does not create a public-information trading strategy. It motivates event-specific preparation, not an automatic 30-minute blackout. [ECB Working Paper 1901](https://www.ecb.europa.eu/pub/pdf/scpwps/ecbwp1901.en.pdf)

**Observed desk processes combine preparation and reassessment.** SMB's first-person account describes selecting several ideas, discussing them before the open and setting alerts/scripts; some ideas activate later in the day. Its favorable performance assertions lack a replicable comparison. Optiver describes automated risk monitoring, calendar/event discussions and joint trading/research/risk review. Its six-week risk committee cadence is not an intraday plan refresh schedule. Both are documented practice, not proof of a profitable NQ TTL. [Spencer, SMB](https://www.smbtraining.com/blog/why-i-game-plan-every-day); [Frans, Optiver](https://www.optiver.com/join-us/stories/risk-and-reward-within-a-dynamic-trading-firm-insights-from-optivers-cro-europe/)

**State-triggered interruption has a real operational precedent.** The CFTC/SEC May 6 report documents automated pauses after rapid price changes or data-integrity concerns, followed by reassessment; widespread withdrawals also reduced liquidity. This exceptional event supports the existence of state-dependent controls, not a universal optimal pause or evidence that constantly reactive trading is superior. [CFTC/SEC report, summary and data-integrity discussion](https://www.sec.gov/news/studies/2010/marketevents-report.pdf)

CME's educational worksheet frames preparation through objectives, instrument, risk, entry/exit criteria and review. It does not prescribe VL score weights, a minimum target multiple or a five-minute confirmation rule. [CME futures trade-plan worksheet](https://www.cmegroup.com/education/courses/files/download-trade-plan.pdf)

### Distinguish four clocks

| Clock | What it measures | Why it matters |
|---|---|---|
| Observation time | When the underlying price, level, calendar or state observation occurred. | Old facts can be packaged in a newly generated document. |
| Availability time | When that observation was available to this system. | Backfilled data cannot be credited to an earlier decision. |
| Publication time | When a plan version was accepted and exposed. | Measures authoring latency but does not reset market history. |
| Permission/revalidation time | When the current setup was assessed for action. | Determines whether an old map still authorizes this entry now. |

Store both source event time and receipt time, including timezone and daylight-saving interpretation. Distinguish input age, authoring duration and time since publication. Repeatedly checking an unchanged old snapshot does not make it new evidence.

### Revalidation policies worth comparing

| Policy | Advantage to test | Cost or risk to measure |
|---|---|---|
| Session plan without refresh | Stable reference and low computation; clean baseline. | Missed state changes and stale permissions. |
| Fixed reread cadence | Simple and reproducible. | Too slow during shocks, needless updates in quiet periods; cadence may exceed authoring speed. |
| Revalidate at publication and before entry | Tests whether the authored setup survived and is still executable. | Extra delay; genuinely fleeting opportunities may be lost. |
| Invalidate on drift or structural change | Responds to a specific thesis failure or economics change. | Noisy thresholds can cause excessive switching or lockout. |
| News/state-aware review | Anticipates known events and reacts to spread/volatility/data-quality changes. | Overbroad restrictions can discard profitable windows; recovery criteria need definition. |
| Plan-free reactive baseline | Separates the value of a long-form Planner from a direct state policy. | Still needs causal features, risk constraints and a fixed playbook; otherwise the comparison is unfair. |

“Plan-free” should mean no lengthy narrative authoring in that experimental arm. It does not mean no policy, no stop or unconstrained discretionary execution. Give it the same information available at the decision and the same risk limits.

### Proposed validity record and falsification

Each scenario should carry a map/version ID, source snapshot ID, thesis, regime assumptions, trigger, explicit invalidation, entry tolerance, obstacle path and permission state. Record why permission was retained, withdrawn or reissued. A new plan must not silently relabel an existing order as if it originated from the new version.

Use drift measured in both points and units meaningful to the setup, such as original risk or a frozen past-only volatility estimate. Define the anchor and denominator in advance. A drifting denominator must not make a deteriorating setup appear stable. Use a reset threshold or minimum stable period if needed to avoid rapid oscillation; those values are test candidates, not established constants.

At publication, reassess what happened during generation. If the level broke and was reclaimed while the plan was being authored, do not pretend the history started at publication. Decide explicitly whether that event is eligible to support the new scenario. The published plan must still be acted upon only after it was actually available.

The history must connect input → attempts/repairs → accepted version → confirmation episode → permission → executor context. Report both the currently viewed plan and the plan authorizing an existing position/order. The frozen audit's prompt-attribution weaknesses make chronology alone insufficient to recover every historical chain. [Frozen authoring and attribution analysis](https://github.com/johnwick2921-cyber/vl/blob/6095ca58fe5901ba398be374e4f9d3488d0bed6b/docs/superpowers/reports/2026-09-07-planner-preparation-audit/README.md)

**Proposed test:** compare the policies on the same market episodes, including avoided losses, missed winners, delayed entries, idle time, replan churn and computation time. Test news, session transitions and quiet periods separately without choosing regimes from later price outcomes. Reject a refresh policy whose net advantage disappears after its own delay and churn. If it merely prints more recent timestamps without changing valid decisions, it has not demonstrated value.

## 07 • The four choices form one trading decision

The following flow is a proposed review contract. It is not new behavior installed in VL.

**Known-at-the-time data → complete reference map → setup and market state → confirmation event → fresh permission → feasible entry/stop/obstacle/target → management → outcome and review.**

A failure at an earlier step cannot be repaired by an attractive number later in the chain. A high-grade level cannot excuse stale evidence. Stronger confirmation cannot recover a target already consumed before entry. A well-written new plan cannot retroactively authorize an old opportunity.

### Preparation quality by trading style

| Setup hypothesis | What preparation must explain | What would challenge the thesis |
|---|---|---|
| Range rejection/fade | Why the boundary is relevant, why rejection is plausible, reachable opposite structure, clear failure boundary. | Persistent acceptance outside the range or an intervening obstacle leaving inadequate reward. |
| Breakout continuation | What distinguishes acceptance from a brief cross, when entry remains affordable, route through opposing references. | Failed acceptance, immediate return inside, or confirmation arriving after the reward has been spent. |
| Sweep/reclaim reversal | The exact anchor and event order; what price action contradicts the attempted reversal. | Reclaim occurred before the sweep; continuation persists; no feasible post-confirmation entry. |
| Pullback in trend | A causal trend definition, pullback depth, resumption evidence and target path. | Structural trend failure, regime transition, or target/stop geometry inconsistent with entry. |
| Event-driven preparation | Known event schedule, information-release state, liquidity assumptions and conditions for participation. | Feed uncertainty, spread/volatility outside the tested envelope, or a new information state unsupported by the plan. |

These are hypotheses to make explicit, not claims that the listed styles are profitable. A level can support a fade hypothesis before acceptance and a continuation hypothesis afterward; the transition requires evidence and timestamps, not a retrospective change of story.

### An illustrative review that ties the questions together

Suppose a plan expects a long from a reclaimed support, with a protective stop beneath thesis failure, a nearby overhead reference and a distant session objective.

1. Verify that the reference existed when selected and that the regime description used only available observations.
2. Ask whether its rank reflects demonstrated reaction information or just an uncalibrated score. Preserve the nearby overhead reference even if it is not an entry candidate.
3. Require the specified sweep and reclaim in order. If confirmation is a completed close, record exactly when it became observable.
4. Recompute feasible entry and remaining reward after that delay. Keep structural invalidation separate from protective-stop mechanics.
5. Determine whether information arriving during plan generation changed the thesis or obstacle path.
6. Apply the previously specified action at the first obstacle. Do not invent a partial exit when only one contract exists.
7. Evaluate the episode even if the rule never entered. Compare it with alternatives without knowing which later won.

A professional review may conclude **“coherent but unvalidated,” “not currently executable,” “invalidated,” or “insufficient evidence.”** These are more informative than forcing every scenario into “good trade” or “bad trade.”

## 08 • A defensible experiment programme

All items below are proposals. No new strategy backtest, production patch or live-money experiment was executed for this report.

### Stage A — establish what can be known

Build a reproducible, read-only research snapshot before any policy comparison. Required fields are listed by role rather than prescribing a database implementation.

| Evidence object | Minimum retained information |
|---|---|
| Market data | Actual contract, feed, timezone, event and receipt timestamps, finalized/forming status, corrections, missing intervals, spread/quotes where required. |
| Candidate level | Stable ID, raw origin, family, bounds, availability time, prior episodes, score components, overrides, selection and explicit exclusion. |
| Plan | Exact input snapshot, prompt/model/config version, attempts and repairs, accepted output, normalization changes, publication time. |
| Scenario and permission | Scenario ID/version, ordered predicates, initial risk, target path, timestamps, invalidation, expiry and revalidation reason. |
| Execution and outcome | Order semantics, attainable entry/exit, fills or explicit simulation assumptions, costs, position size, common horizon, ambiguity and timeout. |

Preserve the actual contract's price scale. A back-adjusted continuous series can move historical levels and gaps; document rolls and avoid treating a back-adjusted price as an orderable tick. Keep NQ and MNQ economics separate. They share underlying market information, so their observations of the same move are not independent experiments.

Audit the data before adding high-resolution rules. A bar-only archive can support bar-close studies with correct availability times; it cannot support an exact order-book or dwell-time experiment. Missing data and disconnected periods are exclusions with disclosed counts, not automatically flat or profitable outcomes.

### Stage B — isolate each policy

| Research lane | Hold fixed first | Vary | Primary result and falsification |
|---|---|---|---|
| Targets | Eligible entry events, initial stop/risk and capacity assumption. | Predeclared exit family and small parameter set. | Paired net outcome per opportunity; reject improvement dependent on impossible fills, unequal risk or isolated tuned values. |
| Confirmation | Candidate encounter and frozen map, common session horizon. | Evidence event and resulting attainable entry. | Net value including nontrades, delay and missed opportunities; reject classification-only improvement. |
| Ranking | Candidate universe and downstream entry/exit policy. | Score terms and final selection overrides. | Incremental calibrated reaction and net value; reject gains explained by distance, width or duplicated episodes. |
| Staleness | Information stream, scenario families and risk constraints. | Refresh/invalidation policy and its measured latency. | Net avoided harm versus missed reward and churn; reject benefits requiring retroactive information. |

Then run a limited interaction study: confirmation × exit, ranking × map completeness, and revalidation × authoring delay. Select these interactions because they change the economic meaning of the individual tests, not because a large search grid is convenient.

### Stage C — prevent false confidence

Use chronological training, validation and final holdout blocks. Fit all feature normalization, widths, volatility estimates and thresholds only on the appropriate past data. Keep overlapping outcome horizons and the same zone episode together or remove boundary overlap. Group uncertainty by trading day/session and, where feasible, by shared zone/event.

Sullivan, Timmermann and White show why selecting the best rule matters. In their 1984–1996 S&P 500 futures comparison, a best-rule nominal p-value of **0.042** becomes **0.908** under the Reality Check over the tested universe. Earlier historical results differ, so the paper does not prove all technical rules useless. Its direct lesson is to account for the full search, not only the winner. This is daily historical evidence, not a modern intraday NQ performance estimate. [Sullivan et al., Table III](https://eprints.lse.ac.uk/119144/1/dp303.pdf)

Record every tested variation, including abandoned versions and model/prompt choices. Predeclare the principal metric, cost assumptions, minimum worthwhile improvement and the maximum acceptable deterioration in tail risk. Use paired, session-blocked uncertainty for net-outcome differences. Report practical effect size alongside intervals; “not significant” is not proof of equality.

For score calibration, assess reliability of predicted probabilities and performance against a simple contextual baseline, as well as discrimination. A good ranking can still be miscalibrated, and a statistically detectable reaction can still be too small to trade after costs.

No fixed sample size is justified from the present evidence. Estimate required independent sessions using pilot variance, clustering and a predeclared minimum effect, then reserve the final holdout. The 102 versions and 124 selected touches are not a substitute for that calculation.

### Stage D — test execution economics without making it the whole audit

Use commissions and fees for the actual account and date; model spreads, slippage, order type and partial fills. Stress costs beyond the base estimate. If stop and target occur inside the same bar with unknown sequence, publish conservative/optimistic bounds or require finer data. Do not choose the favorable path by default.

For a target-only isolation test, simulate comparable independent opportunities. For deployable whole-policy economics, also account for one-position limits, pending orders and the trades a longer holding period displaces. These are different questions and should both be reported.

If later approved, progress from historical replay to prospective shadow evaluation and then simulation under the actual data path. A paper's positive result alone is insufficient reason to activate a new rule.

## 09 • Proposed priorities and acceptance criteria

| Priority | Proposed work | Evidence required before calling it successful |
|---|---|---|
| 1 | Correctly specify completed bars, event sequence and source availability. | Long/short causal fixtures and replay; no acceptance before the required evidence exists. This proves semantics, not profit. |
| 2 | Make first obstacle, planned response, arm target and risk geometry agree. | Each admitted scenario explains its path; ticks, size and costs are explicit; no invented historical gate result. |
| 3 | Preserve all candidate states and the full Planner-to-permission chain. | A third party can reconstruct effective ranks and exclusions, including overrides, and identify exactly what the decision knew. |
| 4 | Compare simple target and confirmation alternatives. | Untouched-session net effect and tail-risk evidence under feasible entries and equal risk. |
| 5 | Test ranking and revalidation against simple baselines. | Incremental benefit survives width/distance/episode controls and includes latency, missed trades and churn. |

**Do not prescribe now:** a mandatory 1R first target; one universal 5m close rule; a replacement confluence coefficient; “fresh is always better”; a five-minute plan TTL; automatic ML ranking; or removal of protective stops. The reviewed evidence does not establish these as improvements for VL.

**Retain as explicit design aims:** causal information, consistent event semantics, meaningful structural invalidation, complete obstacle context, feasible contract sizing and traceable plan history. These make the policy evaluable. They do not guarantee an edge.

A later implementation brief should quote the exact approved behavior, pinned source references, expected before/after examples, tests and rollback boundaries. The owner has requested research first; this report does not silently convert a hypothesis into an implementation instruction.

## 10 • Scope, verification and remaining uncertainty

Four agents performed distinct research lanes: Turing on targets, Dalton on confirmation, Newton on ranking and Plato on staleness. The coordinator reconciled definitions, checked consequential source passages and independently recalculated audit geometry and illustrative economics. These are research assistants, not evidence of personal trading experience.

The search covered original empirical finance and microstructure studies, optimal-stopping theory, exchange/regulator material and first-party professional practice. Targeted follow-up examined counterevidence, denominators, time ordering, transaction costs and transfer from other instruments. General searches were stopped when they produced repeated, weaker or indirect evidence rather than comparable NQ/MNQ policy tests.

### Material boundaries

- **No new profitability claim.** The audit sample is preparation history, not a representative realized-P&L sample. No new NQ/MNQ policy comparison was run.
- **Frozen system evidence.** The report concerns a pinned historical snapshot. It does not assert that earlier defects remain in a later update.
- **External validity remains limited.** FX, equities, Taiwan futures, daily bars and diffusion theory illuminate mechanisms or methods; none automatically validates VL intraday settings.
- **Access limits are visible.** Some sources were abstract-only; two ranking full texts read by the researcher could not be re-opened by the coordinator. Their conclusions are lower-weight and not the sole basis of any recommendation.
- **Recent and commercial evidence receives less weight.** Vendor pages lack verified independent replication. Mesfin's 2026 MNQ preprint concerns a broader OHLCV signal search; it is not used here to establish an exit-only winner or an impossibility result. [Mesfin preprint record](https://arxiv.org/abs/2605.04004)
- **Professional practice is not efficacy evidence.** Desk accounts describe actual processes but do not supply controlled estimates of the best target, confirmation or refresh policy.
- **Research uncertainty is not removed by careful presentation.** Complete recordkeeping and independent checks can improve reliability; they cannot establish absolute certainty about future prices.

### Reproduction and evidence quality

The prior audit verifier passed when rerun against the retained bundle. The coordinator recomputed the 111-arm target-ratio premise and identified the six under-2R rows, checked the first-target fraction, and independently verified the equations and examples in this report.

The strongest numerical external comparison—the Taiwan stop-only versus stop-and-target example—was checked against the original tables and cost disclaimer. Primary passages were also checked for the discovery/ranking distinction, whole-series zone-width issue, contemporaneous flow design, news response, desk practice and multiple-testing example.

A source supports only the claim beside it. Links to the frozen audit use immutable revisions; external URLs are the accessed publisher/author records and may later change. No source PDF is claimed to be embedded in this deliverable. Source details and access limitations follow.

## 11 • Source register

All external sources below were accessed on September 8, 2026. Version dates distinguish working papers from later journal publications. “Full text” describes the version read; it does not imply that the underlying dataset or code was independently replicated.

### Frozen VL Planner audit

VL audit team. September 7–8, 2026. [Open Frozen VL Planner audit](https://github.com/johnwick2921-cyber/vl/blob/6095ca58fe5901ba398be374e4f9d3488d0bed6b/docs/superpowers/reports/2026-09-07-planner-preparation-audit/README.md).

Access and scope: Full local report, history, CSV, metrics and probes inspected; verifier rerun. Snapshot, not current runtime.

### Micro E-mini Nasdaq-100 contract specifications

CME Group. Current page, accessed September 8, 2026. [Open Micro E-mini Nasdaq-100 contract specifications](https://www.cmegroup.com/markets/equities/nasdaq/micro-e-mini-nasdaq-100.contractSpecs.html).

Access and scope: Official contract terms; no strategy-performance claim.

### Empirical Evaluations on Momentum Effects of Taiwan Index Futures Market

Wang, Wu and Chung; IEEE RVSP, pp. 82–85. 2015. [Open Empirical Evaluations on Momentum Effects of Taiwan Index Futures Market](https://www.researchgate.net/publication/304299040_Empirical_Evaluations_on_Momentum_Effects_of_Taiwan_Index_Futures_Market).

Access and scope: Author-uploaded original full text; Tables 2 and 4 and conclusion independently checked. DOI 10.1109/RVSP.2015.29.

### Can Simple Buy and Sell Rules Increase Index Future Day Trading Profitability?

Yu and Rentzler; Journal of Investment Management 2(1), pp. 55–75. Q1 2004. [Open Can Simple Buy and Sell Rules Increase Index Future Day Trading Profitability?](https://joim.com/issue/2004q1/).

Access and scope: Abstract and indexed original excerpts only; full-PDF access failed. Costs and event sequence not verified.

### Optimal Trading with a Trailing Stop

Tim Leung and Hongzhong Zhang; Applied Mathematics & Optimization. 2019; arXiv v2 March 22, 2019. [Open Optimal Trading with a Trailing Stop](https://arxiv.org/pdf/1701.03960v2).

Access and scope: Full original preprint; Theorem 4.2 and Remark 4.2 checked. DOI 10.1007/s00245-019-09559-0.

### Exiting on a High Note

Kevin J. Davey; Active Trader, pp. 24–29. July 2010. [Open Exiting on a High Note](https://kjtradingsystems.com/Act/davey0710.pdf).

Access and scope: Author-hosted full article. Practitioner experiment, not peer-reviewed intraday NQ evidence.

### The Ins and Outs of Scaling Out

Mike Bryant; Breakout Bulletin. March 2009. [Open The Ins and Outs of Scaling Out](https://www.adaptrade.com/BreakoutFutures/Newsletters/Newsletter0309.htm).

Access and scope: Full vendor/practitioner article. Sizing and optimization confound exit comparisons.

### Support for Resistance: Technical Analysis and Intraday Exchange Rates

Carol L. Osler; Federal Reserve Bank of New York, Economic Policy Review 6(2), pp. 53–68. July 2000. [Open Support for Resistance: Technical Analysis and Intraday Exchange Rates](https://www.newyorkfed.org/medialibrary/media/research/epr/00v06n2/0007osle.pdf).

Access and scope: Full original paper; outcome definition and Tables 10–12 independently checked.

### Evidence and Behaviour of Support and Resistance Levels in Financial Time Series

Ken Chung and Anthony Bellotti; arXiv:2101.07410v1. January 19, 2021. [Open Evidence and Behaviour of Support and Resistance Levels in Financial Time Series](https://arxiv.org/html/2101.07410v1).

Access and scope: Full versioned original preprint; Eq. 1, prior-bounce and decay results checked.

### The Price Impact of Order Book Events

Rama Cont, Arseniy Kukanov and Sasha Stoikov; Journal of Financial Econometrics 12(1), pp. 47–88. 2014; read arXiv v3, 2011. [Open The Price Impact of Order Book Events](https://arxiv.org/html/1011.6402v3).

Access and scope: Full original preprint; contemporaneous explanatory design independently checked.

### Queue Imbalance as a One-Tick-Ahead Price Predictor in a Limit Order Book

Martin D. Gould and Julius Bonart; arXiv:1512.03492v1. December 11, 2015. [Open Queue Imbalance as a One-Tick-Ahead Price Predictor in a Limit Order Book](https://arxiv.org/abs/1512.03492v1).

Access and scope: Full original paper reviewed by confirmation researcher; parent verified canonical record. Nasdaq stocks, not NQ futures.

### Stop-Loss Orders and Price Cascades in Currency Markets

Carol L. Osler; Federal Reserve Bank of New York Staff Report 150. July 2002; PDF dated June 2002. [Open Stop-Loss Orders and Price Cascades in Currency Markets](https://www.newyorkfed.org/medialibrary/media/research/staff_reports/sr150.pdf).

Access and scope: Full working paper; later journal version is 2005. Historical quotes and order samples differ in date.

### Currency Orders and Exchange-Rate Dynamics: Explaining the Success of Technical Analysis

Carol L. Osler; Federal Reserve Bank of New York Staff Report 125. April 2001; PDF dated March 2001. [Open Currency Orders and Exchange-Rate Dynamics: Explaining the Success of Technical Analysis](https://www.newyorkfed.org/medialibrary/media/research/staff_reports/sr125.pdf).

Access and scope: Full working paper; distinct from the 2003 journal version. Order-count and mechanism checked.

### Price Clustering in E-mini and Floor-traded Index Futures

Huimin Chung and Shumei Chiang; Journal of Futures Markets 26(3), pp. 269–295. March 2006; online January 9. [Open Price Clustering in E-mini and Floor-traded Index Futures](https://onlinelibrary.wiley.com/doi/10.1002/fut.20196).

Access and scope: Publisher abstract plus indexed primary methods excerpts; complete PDF not retrieved.

### Price Exhaustion and Number Preference: Time and Price Confluence in Australian Stock Prices

Hristos Doucouliagos; Deakin working paper 2003/06; European Journal of Finance 11(3), pp. 207–221. 2003 working paper; 2005 journal. [Open Price Exhaustion and Number Preference: Time and Price Confluence in Australian Stock Prices](https://www.deakin.edu.au/__data/assets/pdf_file/0006/402585/swp2003_06.pdf).

Access and scope: Ranking researcher read full working paper; parent repeat retrieval blocked. Journal DOI 10.1080/1351847042000254194. Lower-weight evidence.

### Automatic Identification and Evaluation of Fibonacci Retracements: Empirical Evidence from Three Equity Markets

Prodromos Tsinaslanidis, Francisco Guijarro and Nikolaos Voukelatos; Expert Systems with Applications 187, 115893. January 2022. [Open Automatic Identification and Evaluation of Fibonacci Retracements: Empirical Evidence from Three Equity Markets](https://riunet.upv.es/entities/publication/07e4aacf-2e17-4914-9554-76e9eb41951b).

Access and scope: Ranking researcher read repository full text; parent repeat retrieval met bot challenge. DOI 10.1016/j.eswa.2021.115893.

### Support Resistance Levels towards Profitability in Intelligent Algorithmic Trading Models

Jireh Yi-Le Chan, Seuk Wai Phoong, Wai Khuen Cheng and Yen-Lin Chen; Mathematics 10(20), 3888. October 20, 2022. [Open Support Resistance Levels towards Profitability in Intelligent Algorithmic Trading Models](https://www.mdpi.com/2227-7390/10/20/3888).

Access and scope: Researcher read original PDF via author-upload mirror; parent canonical retrieval attempted. Access and method caveats retained.

### Use of the Volume Profile in Making Investment Decisions on the Stock Market

Rafał Jóźwicki and Paweł Trippner; Journal of Finance and Financial Law 4(48), pp. 87–98. December 2025. [Open Use of the Volume Profile in Making Investment Decisions on the Stock Market](https://czasopisma.uni.lodz.pl/fipf/article/download/28410/27868/72359).

Access and scope: Full original article. DOI 10.18778/2391-6478.4.48.04. Descriptive categories, not an independent-touch trading test.

### Real-Time Price Discovery in Global Stock, Bond and Foreign Exchange Markets

Torben G. Andersen, Tim Bollerslev, Francis X. Diebold and Clara Vega; Federal Reserve IFDP 871. September 2006 working paper; journal 2007. [Open Real-Time Price Discovery in Global Stock, Bond and Foreign Exchange Markets](https://www.federalreserve.gov/pubs/ifdp/2006/871/ifdp871.pdf).

Access and scope: Full working paper; common sample, observation count and five-minute response checked.

### Price Drift before U.S. Macroeconomic News: Private Information about Public Announcements?

Alexander Kurov, Alessio Sancetta, Georg Strasser and Marketa Halova Wolfe; ECB Working Paper 1901. May 2016. [Open Price Drift before U.S. Macroeconomic News: Private Information about Public Announcements?](https://www.ecb.europa.eu/pub/pdf/scpwps/ecbwp1901.en.pdf).

Access and scope: Full working paper; seven-of-21 result and event window independently checked.

### Findings Regarding the Market Events of May 6, 2010

Staffs of the CFTC and SEC. September 30, 2010. [Open Findings Regarding the Market Events of May 6, 2010](https://www.sec.gov/news/studies/2010/marketevents-report.pdf).

Access and scope: Full official report; executive summary and data-integrity pause discussion checked. Exceptional episode, not a controlled policy test.

### Why I Game Plan Every Day

Steven Spencer; SMB Training. February 19, 2018. [Open Why I Game Plan Every Day](https://www.smbtraining.com/blog/why-i-game-plan-every-day).

Access and scope: Full first-party practice account; unverified performance assertions not adopted.

### Risk and Reward within a Dynamic Trading Firm: Insights from Optiver’s CRO Europe

Wouter Frans; Optiver. January 12, 2024. [Open Risk and Reward within a Dynamic Trading Firm: Insights from Optiver’s CRO Europe](https://www.optiver.com/join-us/stories/risk-and-reward-within-a-dynamic-trading-firm-insights-from-optivers-cro-europe/).

Access and scope: Full first-party professional-practice account; recruitment context and no performance experiment.

### Data-Snooping, Technical Trading Rule Performance, and the Bootstrap

Ryan Sullivan, Allan Timmermann and Halbert White; LSE discussion paper 303; Journal of Finance 54(5), pp. 1647–1691. 1998 working paper; 1999 journal. [Open Data-Snooping, Technical Trading Rule Performance, and the Bootstrap](https://eprints.lse.ac.uk/119144/1/dp303.pdf).

Access and scope: Full original working paper; Table III and S&P futures sample independently checked. Journal DOI 10.1111/0022-1082.00163.

### Opening Range Breakout Strategy: 6,142 Days of ES & NQ

TradingStats. February 20, 2026. [Open Opening Range Breakout Strategy: 6,142 Days of ES & NQ](https://tradingstats.net/orb-breakout-strategy-guide/).

Access and scope: Full vendor study; confirmation table and continuation definition independently checked. Not a costed execution comparison.

### Validation Study: Pre-Specified Tests on Futures Data

Cascade Indicators. Undated; accessed September 8, 2026. [Open Validation Study: Pre-Specified Tests on Futures Data](https://www.cascadeindicators.com/research).

Access and scope: Full vendor disclosure; negative reported confirmation differences checked. Raw reproducibility and independent preregistration not established.

### Structural Limits of OHLCV-Based Intraday Signals in MNQ Futures: A Systematic Falsification Study

Mathias Mesfin; arXiv:2605.04004. v1 May 5; v2 July 13, 2026. [Open Structural Limits of OHLCV-Based Intraday Signals in MNQ Futures: A Systematic Falsification Study](https://arxiv.org/abs/2605.04004).

Access and scope: Target researcher read original preprint; parent verified canonical record. Not used to establish an exit-only result or impossibility theorem.

### Master the Trade: Futures Trade Plan

CME Group. Undated; accessed September 8, 2026. [Open Master the Trade: Futures Trade Plan](https://www.cmegroup.com/education/courses/files/download-trade-plan.pdf).

Access and scope: Full official educational worksheet. Preparation framework, not validation of any scoring or confirmation threshold.
