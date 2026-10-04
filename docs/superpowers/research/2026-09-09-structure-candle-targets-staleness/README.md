> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# Rounds 12–15: structure, the forming candle, targets beyond the map, and plan staleness

Research date: **2026-09-09, America/Chicago**. Prepared by Codex in one research lane for hoang. Scope: literature review and read-only inspection of research and field definitions; no trading, configuration, database, or executable-code changes.

**Finding:** the literature does not establish a daily/weekly score multiplier, a profitable live-wick rule, an NQ continuation probability after 1.5 times its usual range, or a universal plan expiry time. It does establish reasons to test these questions separately: level memory depends on its definition and market; order flow and execution timing matter; fixed targets can change the return distribution; and different information decays on different clocks. The strongest nearby evidence concerns microstructure, not the four requested production rules.

Recommendations use **[R]** for a named study's supported implication, **[T]** for a proposed test on the owner's record, **[I— inference]** for reasoning not established by a trading experiment, **[I— doctrine]** for documented practitioner practice, and **[O]** for an owner decision. A verdict of **UNTESTED** means no qualifying direct test was verified in this review, not proof that nobody has ever studied the question. Full source details, samples and transfer limits appear in the numbered sources appendix; inaccessible information is explicitly marked unverified.

## Scope, inherited constraints and provenance

The owner supplies the following context: MNQ SIM, one contract and one position at a time; about twelve mapped levels and three to five conditional scenarios; a stop at the larger of structural distance and 1.5×ATR5m; flat at 14:45 CT. The reported first-touch result is **48.8%, n=423, independent-binomial Wilson interval [44.1%, 53.6%]**. The older [42.5%, 55.0%] interval is superseded. The reported trade sample is 58 trades over 12 CME days, with expectancy indistinguishable from zero. These are supplied measurements, not newly audited results; their row IDs and clustering are unavailable here.[^1]

The owner's brief also supplies the 2,500-bar ring limitation, absent daily/weekly detections, two-minute checks of a closed five-minute confirmation, 1,688 stored touch rows, and the September 3 move 483 points beyond the map. This review does not re-audit those counts or that session. The already-decided requirement to run every computation on every NT8-provided timeframe is **[O]**, independently of whether higher-timeframe observations deserve a higher trading score.[^1]

Round 10's execution constraints remain binding: the approximately 66% statistic is **1,269 adverse next-quote changes among 1,929 NQ simulated passive fills on April 25, 2024**, under a particular posting strategy. It is not a population loss probability for all NQ fades. Round 11's distinction between reaction, executable fill and profitable trade also remains binding. Neither 50% nor a touch-count denominator is automatically an appropriate trading null.[^2][^3][^4]

The four-clock and exclusion/permission discussion is documented in the earlier **four-policy report** as well as the owner's Round 11 framing. Its frozen sample was 102 plan versions, 18 date/session groups, 280 scenario documents and 111 complete geometries, not 102 independent sessions. Its captured authoring examples were approximately 6.30, 18.45 and 11.39 minutes: “11–18 minutes” is a useful warning, not a measured distribution of all plan latencies.[^5]

This worktree was cut from `origin/dev` at **8941ec68612cc019edc3002b999272ab2ed20516**, the tip at acceptance. The relevant `git log -1` records were:

```text
b2ba8c2110afbb5ee4d3cd519e37f978f7f51803 2026-09-09T13:20:57-05:00 docs: publish range-fade research for MNQ
0ec5bd2cb4c678e524cc21033d7622a7783765c1 2026-09-08T08:30:15-05:00 docs(research): publish four-policy trading study with 28 cited sources
```

The first record concerns `2026-09-08-range-fade/report.md`; the second concerns `2026-09-08-trading-policy/README.md`. Source-code observations below are **[A: directly read]** at that base. Proposed economic consequences are not code-verified performance claims. The repository's audit checklist informed this bounded inspection; the database was not queried.[^6]

## Question 12 — Higher-timeframe structure

**MIXED — level memory has empirical support in other markets; superiority of daily/weekly levels, their role-specific ranking, and the 1.2 multiplier are UNTESTED for intraday NQ/MNQ.**

### 12a and 12d. What is actually compared across timeframes?

Three quantities must be distinguished: the **formation timeframe** of a completed swing, the **lookback window** used to find a level, and the **age** since formation or last interaction. They are different experimental variables. A 240-minute rolling minimum is not a swing confirmed on a four-hour chart, and sampling a price process every 90 seconds is not testing weekly structure.

**Osler (2000)** contradicts the brief's “about as well” wording: Table 8 reports **60.8% versus 56.2%** bounce frequencies for published versus artificial levels. Her six-firm FX sample contains approximately 64,200 level values, January 1996–March 1998. It does not compare weekly versus five-minute formations.[^7]

**TRANSFERS to intraday Nasdaq-100 futures?** The comparator method transfers; the FX effect size does not establish an NQ reaction probability or timeframe weight.

**Garzarelli et al. (2014)** examine nine London stocks over 251 trading days in 2002, with time grainings from one to 180 seconds. Prior bounces contain information relative to shuffled series, but the memory weakens under coarser sampling. Table 1's tests remain significant through 60 seconds; the reported p-values at 90 and 180 seconds are not significant at 5%. This does not establish that longer-chart levels are stronger; it measures a different kind of scale dependence.[^8]

**TRANSFERS?** A reason to test bounce history and sampling resolution, not a daily/weekly NQ hierarchy. Stocks, their trading sessions and their level detector differ from a nearly continuous futures market.

**Chung and Bellotti (2021)** use 2018 minute prices: EUR/USD, 372,607 observations; Lloyds, 127,606; Brent, 307,678. Prior-bounce and decay effects vary by asset. Their lookback comparisons do not support a universal “longer is stronger” rule. Zone width uses a whole-series price-increment statistic, requiring care in a causal replication.[^9]

**TRANSFERS?** The hypotheses and algorithm can be tested on NQ; neither the window results nor a fitted decay parameter is an established NQ daily/weekly coefficient.

**Prior-day extremes have some directly relevant vendor statistics, with substantial limitations.** TradingStats.net reports 3,121 NQ days, approximately 12.5 years, with a 2014–2025 year table. It reports PDH crossed on 56.8% and PDL on 43.4% of days. These are full 18:00–16:00 ET session crossing frequencies, not reaction rates after an entry or target attainment by 14:45 CT. Counts also fail reconciliation: 1,376 “PDH only” plus 396 “both” equals 1,772, while the headline PDH count is 1,773. The stated leave-one-out exercise can train on later dates.[^10]

**TRANSFERS?** Same underlying index future, but insufficient validation for a trading probability. Exact sample endpoints, reconstructed independent data and chronological testing are needed. The figures do not rank prior-day against prior-week or prior-month levels.

**Prior-week and prior-month extremes:** **UNTESTED** in the requested sense. No verified primary study supplies an NQ intraday table of matched reaction rates, penetration distributions and subsequent continuation, separately for these levels and five-minute/hourly formations. No verified replication of Osler compares weekly and five-minute levels against random prices at the same distance. There is therefore no researched numerical ranking to insert here.

### 12b and 12c. Entry, target, invalidation — and the 1.2 multiplier

**UNTESTED:** no verified NQ study establishes that a daily/weekly level is a superior target or invalidation boundary while being an inferior entry, or that “higher timeframe” should multiply a score by 1.2. That coefficient is a design choice unless estimated and validated on the relevant objective. Calling all coefficients practitioner doctrine is also too broad: a coefficient can be an honestly fitted model parameter. This particular coefficient has no verified calibration in the cited literature.

**Osler (2003)** examines 9,667 conditional FX orders at one bank, September 1999–April 2000. Different stop-loss and take-profit clustering helps explain reversals and acceleration through levels.[^11]

**TRANSFERS?** A role-dependent mechanism, not quantitative NQ, weekly-level or stop-rule validation; dealer FX orders differ from centralized futures matching.

**[I— inference] “Exclusion is not invalidation” is an information-contract distinction, not a proven trading law.** A price omitted from an entry shortlist has failed a selection rule; it has not necessarily ceased to exist or ceased to matter to an open position. Conversely, retaining it as context does not prove it should be used as a stop or target. The earlier report supports this separation as system design, while Osler supplies only the narrower empirical observation that orders with different purposes have different clustering.[^5][^11]

**[T]** Estimate entry, target and invalidation usefulness separately. Entry quality is net value conditional on an executable entry; target usefulness is target-before-stop probability and net exit value from an already specified position; invalidation usefulness is the cost of abandoning versus retaining that position after a specified breach. Pooling these into one “level strength” rate changes the question.

### 12e. Detection definitions and whether they should differ

Lo, Mamaysky and Wang (2000) show how to replace visual patterns with kernel-smoothed extrema and formal recognition rules. Their daily US-equity study spans 1962–1996, sampling fifty stocks in each of seven five-year periods. It does not compare daily/weekly NQ zones or prove a best detector.[^12]

**TRANSFERS?** Formal, reproducible pattern definitions transfer as a method; the patterns' results do not validate a futures swing or supply/demand definition.

The following are **[T] candidate definitions**, not established winners:

| Family | Causal definition for an experiment | Timeframe issue |
|---|---|---|
| Completed period extreme | High/low of the last completed day, week or month, with a declared session calendar | RTH and full CME-session extremes are separate variables. A still-forming week's eventual extreme is unavailable. |
| n-bar fractal | A pivot exceeds the highs, or falls below the lows, of a fixed number of bars on each side | It becomes known only after the right-side bars close. Equal bar counts imply very different elapsed confirmation delays across timeframes. |
| ZigZag | A swing is confirmed after a predeclared point, percentage or volatility-scaled reversal | The last leg is provisional. The confirmation time, not the eventual pivot timestamp, determines availability. |
| Smoothed extrema | Local extrema of a declared causal smoother and window | A symmetric smoother using future observations is unsuitable for live recognition unless its delay is respected. |
| Volume-anchored base/zone | Explicit price bounds, volume window, departure condition and confirmation timestamp | No verified study establishes the best daily/weekly NQ base rule or an order-block definition. Volume allocation from OHLCV alone may not identify traded volume at each price. |

**[O]** The owner's every-timeframe coverage decision already stands. **[T]** Hold the family definition constant initially, distinguish parameters in bars from parameters in time or volatility, and test a timeframe-by-definition interaction. That is an experiment design, not evidence that the optimum must be invariant. No verified literature determines how the definition should change specifically between daily and weekly NQ bars.

**[T] A discriminating test:** freeze every available candidate before the touch, including candidates that were not shortlisted. Record detector, formation timeframe, confirmation time, age, previous bounces, zone width and distance from current price. Deduplicate coincident prices while retaining their separate origins. Compare against artificial prices matched on known distance, side, width, time of day and volatility; report probability of being reached separately from reaction conditional on being reached. Use the same forward horizons and executable trade policy for all groups. Chronological day blocks, repeated-touch clustering and a reserved final period are necessary to distinguish a timeframe effect from proximity, age, selection and repeated exposure. Contract rolls and back-adjustments must not manufacture historical levels.

**What this means for THIS system.** **[O]** Cover the required timeframes; **[R— Osler; Chung–Bellotti]** use artificial comparators and test age/bounce history instead of presuming a hierarchy. **[I— inference]** Do not describe the 1.2 multiplier or retaining an unselected level as validated alpha. **[T]** Before a ranking rule ships, measure marginal net value by formation timeframe and role, with distance, age, detector and selection controlled. The literature does not authorize a numerical higher-timeframe premium.

## Question 13 — The forming candle

**MIXED — order-book state has measured short-horizon information; profitable pre-close wick/volume rules at mapped NQ levels, and superiority over the same setups entered at bar close, are UNTESTED.**

### 13a. Rejection shape, penetration and failed pushes

**Fock, Klein and Zwergel (2005)** report no predictive success for tested intraday DAX/Bund candlesticks, with or without momentum filters. Period, n and precise horizon are **unverified** from the accessible primary abstract. These are completed patterns, not forming NQ candles at premarked levels.[^13]

**TRANSFERS?** Relevant caution against assuming named patterns create an edge; no direct rejection of all forming-candle rules on NQ. Bund is a bond future and DAX is a different equity index.

**Li, Nolte, Nolte and Yu (2025)** study realized candlestick wicks as a volatility measure. Their empirical application uses SPY, January 2, 2014–December 29, 2023, **2,493 days**, constructed from five-minute OHLC data. The forecasting target is volatility, including next-day forecasts, not the next few bars' direction at support/resistance.[^14]

**TRANSFERS?** Wicks can be informative statistics without being directional signals. This equity-ETF volatility result supplies no MNQ hold/break probability, entry threshold or profitable wick/body ratio.

**UNTESTED:** no verified primary study supplies the requested NQ out-of-sample directional result for forming wick/body ratio, penetration relative to a causally defined zone, or a failed push, with an explicit forward horizon and honest fills. There is no defensible percentage to report for these exact features.

### 13b. Volume, delta and footprint evidence

| Primary study | What is measured | TRANSFERS to intraday NQ? |
|---|---|---|
| **Cont, Kukanov and Stoikov (2014), peer-reviewed** | Fifty US stocks, April 2010. Order-flow imbalance explains short-interval price changes better than trade volume alone. This is primarily contemporaneous price impact, not a future level-rejection strategy. | Research method and feature distinction only. Equity TAQ/book events do not establish an NQ footprint entry edge.[^15] |
| **Gould and Bonart (2016), peer-reviewed** | Ten Nasdaq-listed stocks, 252 trading days in 2014. Queue imbalance predicts the direction of the next midprice move, with differences between large- and small-tick stocks. The sample excludes the open and last half-hour. | A genuine forward prediction result, but “Nasdaq stocks” are not Nasdaq-100 futures; next-tick classification is not five-minute fade profitability.[^16] |
| **Takahashi (2025), preprint** | ES, January 2, 2008–December 31, 2013: 1,490 days and 34,512,298 one-second observations. Structural VAR estimates show intraday and news-dependent return/order-flow dynamics. Most incremental impulse responses dissipate within about one second. | Closest futures evidence here, but it is ES, chiefly structural impact analysis rather than a tradable NQ forward classifier. A dissipating impulse response does not mean price returns to its old level.[^17] |
| **QuantProp Labs, vendor research** | A titled NQ order-flow prediction article was located, but its full page could not be retrieved reliably. Period, sample, horizon, costs and reported hit rate were not verified. | No adoptable numerical result. Its existence does not establish a peer-reviewed NQ edge.[^18] |

**[R— Cont et al.; Gould–Bonart]** Separate signed order-book imbalance from raw traded volume. “A lot traded,” aggressive buy-minus-sell volume, and resting bid-minus-ask depth are not interchangeable variables. **UNTESTED:** the specific rule “volume dries into this mapped NQ level, therefore fade it” is not established by these studies. Neither high volume nor low volume has a universally validated hold/break interpretation in this setting.

### 13c. Dwell time and acceptance

**Dufour and Engle (2000)** examine eighteen NYSE stocks from the TORQ period November 1, 1990–January 31, 1991, an input span of 62 trading days with exclusions and filtering. Shorter intertrade durations are associated with larger price impact. Their clock is time **between trades**, not time spent around an independently marked level.[^19]

**TRANSFERS?** It supports studying transaction intensity, not a calibrated NQ “accepted after X seconds” rule. Exact usable event count after filtering was not verified.

**[I— doctrine]** Dalton, Jones and Dalton's *Mind Over Markets* presents time-price opportunity, acceptance, rejection and auction development as interpretive tools. It is a practitioner text, with examples rather than a reproducible predictive sample or tested dwell threshold.[^20] **TRANSFERS?** A vocabulary for describing an auction; no established NQ predictive hit rate. Steidlmayer/Dalton acceptance doctrine must not be promoted into a statistical result merely because a table counts bars at a level.

### 13d and 13e. The cost of waiting, and what “repainting” means

**UNTESTED:** no verified comparison here holds the same liquid-index-futures opportunities fixed and measures live intrabar entry versus closed-bar entry, with queue-aware passive fills, achievable market fills, costs and missed trades. A later entry can reduce remaining target distance or alter initial risk, but the net benefit of additional information is empirical. There is no verified universal false-signal rate for tick-through, minimum dwell, or two-bar confirmation.

**[R— Lo–MacKinlay–Zhang; Lalor–Swishchuk]** Round 10's execution constraints apply to both arms: executable fills and their selection must be modeled. The latter's one-day NQ adverse-fill result does not tell us which candle trigger wins.[^3][^4]

TradingView's *Repainting* documentation explains the relevant data problem: a realtime bar's high, low and close evolve; historical final OHLC does not preserve all intermediate states. Acting on a fluid value is not inherently invalid, but evaluating that decision later using final-bar values changes its information set.[^21]

NinjaTrader's *Developing for Tick Replay* documentation makes a second distinction: replaying last-trade events for indicator calculation is not an accurate order-fill simulation. Its available historical quote information is limited; it is not a reconstruction of the full sequence of depth events.[^22]

**TRANSFERS?** Both are platform/data-mechanics sources, with no statistical sample or claimed alpha. The causality and fill-model distinctions apply directly to an NT8 strategy; neither proves early entry is better. **[T]** Evaluate predeclared event rules at the time they actually fire, record cancellations and repeated triggers, and preserve the tick sequence or contemporaneous feature snapshots. Final five-minute candles cannot reconstruct their false-trigger denominator.

### 13f. The exact test on the reported 1,688-row table

**[A: directly read] The stored variables are not quite those implied by their informal names.** At the pinned source revision, `touch_episodes` is append-only with one row per **closed episode**. The implementation computes these fields as follows:[^23]

| Stored field | Verified meaning | Consequence for the proposed test |
|---|---|---|
| `wick_pen_pts` | Maximum high/low penetration through the level over the episode | Not the length of a candle's wick. |
| `body_pen_pts` | Maximum close penetration through the level over the episode | Not absolute open-to-close body length; its maximum need not occur on the same bar as the wick maximum. |
| `penetration_pts` | Maximum of the two penetration components | Largely redundant with those components; avoid claiming three independent predictors. |
| `vol_ratio` | Sum of episode volume divided by mean pre-episode per-bar volume | Increases mechanically with episode duration. It is not an equal-duration volume comparison. |
| `approach_atr` | Absolute approach move divided by an ATR value | Dimensionless; **not ATR in points**. It cannot serve as the denominator for a point-distance normalization. |
| `bars_in` | Episode bar count | Not tick dwell time or volume-at-price. |
| `shape` | Computed from close-side and penetration conditions | Predicting this label from the same close/penetration fields would partly reproduce the labeling algorithm. |
| `opened_at_ms`, `closed_at_ms`, `created_at` | Episode boundaries and persistence timestamp | The final row is not available at first touch. |

The row has no complete intrabar path, zone bounds, explicit approach-side field, bid/ask history, or standalone historical ATR-in-points field. The approach calculation uses the episode-opening bar's close, and its supplied ATR is used at episode finalization. It cannot simply be treated as information frozen before first contact. This is a limitation of a retrospective table, not evidence that its telemetry is useless.[^23]

The following is **[T] an exact first experiment**, with no production rule implied:

1. **Population and n.** Begin with the reported **1,688 rows as an unverified upper bound**, not 1,688 independent setups. Export row IDs, trader, symbol, CME day, label, price, touch number and timestamps. Report actual total, each exclusion count, eligible unique episodes, distinct CME days and distinct level encounters. Collapse duplicated observations of the same tape across traders for inference; cluster overlapping levels and repeated encounters. Audit historical field-definition changes and touch-number resets. The eligible `n` and day count are **unknown until this export**; they must not be borrowed from the separate 423-episode or 58-trade samples.

2. **Decision time.** For the existing completed-row test, set `t0` to the first usable quote after the latest of episode close, row creation and any proven receipt/availability timestamp. If receipt cannot be established, explicitly describe the historical-data assumption. Exclude rows whose usable time falls at or after 14:45 CT. This tests information **after the episode completes**. It cannot validate an entry during that episode.

3. **Features.** Reconstruct approach side and actual ATR5m in points from bars available at `t0`; freeze them. Test four small feature families: penetration components divided by this ATR; signed terminal displacement from the level divided by ATR; average relative activity `vol_ratio / bars_in` when the numerator and bar coverage reconcile; and the recorded dimensionless approach move. Include duration separately. Treat sentinel zero/missing values according to each field's generation rules. A true wick/body ratio or zone-relative penetration requires additional OHLC or zone-bound reconstruction and must be labeled a new feature, not a stored field.

4. **Primary outcome.** Let `s=+1` for a prospective long fade and `s=-1` for a short, determined from the original approach. Predict `Y = 1{s × (mid(t0+15 minutes) − mid(t0)) > 0}`; retain only episodes with the full primary horizon before 14:45. Predeclare five- and thirty-minute outcomes as secondary, with their own eligible counts. Also report signed returns, not only a directional hit rate. Missing quotes and incomplete sessions are explicit exclusions.

5. **Economic outcome.** Separately simulate one contract entered at the first achievable bid/ask after `t0`, using the existing stop formula, a frozen and genuinely available target, fees, slippage and the 14:45 liquidation. Report target-before-stop, net P&L, holding time and net P&L per opportunity. If a target or side cannot be reconstructed, exclude the row from this trade experiment while retaining it in an eligible prediction experiment. Respect one position at a time; independent hypothetical overlapping trades are not the book's attainable P&L.

6. **Null and comparator.** Fit a small regularized logistic baseline using time of day, actual ATR, touch number, detector/level type, distance from the level at `t0`, duration and recent market direction. Compare it with the same model plus each feature family. The primary null is **no reduction in held-out Brier loss** relative to this contextual baseline; the economic null is **no increase in paired net P&L per opportunity**. Neither null is “50% holds.” Do not use `shape`, same-episode rejection labels, or an ex-post winning-trade subset as the target.

7. **Split and uncertainty.** Reserve the last third of complete CME days chronologically; fit and choose regularization only on earlier days with day-block validation. Report paired day-block uncertainty and all four family comparisons, including failures. With too few distinct days or rare outcomes, the result is exploratory regardless of the raw row count. No universal minimum `n` guarantees a credible rule: the final report must show the actual independent-day support, effect size and interval, then accrue a fresh forward sample before promotion.

8. **The genuinely intrabar follow-up.** Use captured partial states at predeclared landmarks, for example 30, 60 and 120 seconds after first touch, without final-candle values. Compare the current closed-five-minute/two-minute-poll policy with an early rule on the **same initial opportunity set**, using thresholds chosen only in training. Keep initial absolute stop/target geometry fixed for a clean timing comparison; separately identify cases where delayed entry is no longer feasible. Model passive queues or achievable aggressive fills, cancellations and costs. Report missed opportunities as well as filled-trade results. If the partial states are not retained, this comparison needs new recording; the existing 1,688 final rows cannot supply them.

**What this means for THIS system.** **[R— Cont et al.; Gould–Bonart; platform documentation]** Distinguish volume from imbalance, and freeze the actual information available when an action could occur. **[I— inference]** Do not substitute `wick_pen_pts/body_pen_pts` for a candle wick/body ratio, normalize points by `approach_atr`, or call a prediction of `shape` evidence of future direction. **[T]** Run the completed-episode experiment first; measure a separate causal intrabar replay before changing confirmation. The literature does not establish that the current closed-bar wait is either optimal or wrong.

## Question 14 — Targets beyond the mapped range

**UNTESTED — the requested NQ probability conditional on already exceeding 1.5× its recent median range was not verified; related momentum, reversal and exit studies provide MIXED evidence, not that missing number.**

### 14a. The target menu and the evidence it actually has

The relevant event is not simply “price eventually touched this number.” It is target attainment **after a specified decision**, in the position's direction, before its stop and the 14:45 CT deadline. None of the six families below has a verified universal NQ hit rate for an entry after price leaves the pre-session map.

| Target family | Tested on index futures for this exact condition? | Verified hit rate and horizon | Status |
|---|---|---|---|
| Last completed swing projected from a breakout | No qualifying study verified | **Unavailable** | Measured-move doctrine; a candidate to test. |
| Open ± k×ATR, projected session extremes | No qualifying study verified | **Unavailable** | Volatility scaling is a definition, not a probability calibration. ATR is not a normal-distribution standard deviation. |
| Prior-week/prior-month extremes | No qualifying conditional comparison verified | **Unavailable** | A known reference price when completed; no demonstrated target advantage over distance-matched alternatives. |
| Round numbers beyond the map | FX order clustering is verified; this NQ target policy is not | **Unavailable** for NQ target-before-stop | Osler's clustering evidence does not supply the desired target hit rate.[^11] |
| Fibonacci extensions | CME provides educational construction, not a statistical test | **Unavailable** | The exchange's educational example is practitioner instruction, not exchange-certified alpha.[^24] |
| Volatility bands | No verified conditional NQ target study for the specified event | **Unavailable** | A band needs a fitted conditional distribution and declared horizon; 68%/95% coverage cannot be assumed. |

**TRANSFERS?** CME's construction examples concern market charting, with no empirical instrument-period sample or tested hit rate. Osler's FX mechanism transfers only as a hypothesis. The NQ prior-day crossing statistics in 12a also do not answer this target question: their clock, conditioning event and stopping rule differ.

**[T]** If these families are compared, freeze their anchors before the decision, compare equal-distance alternatives, and count stop-first and timeout outcomes. A hindsight-selected swing makes a measured move look cleaner than the one a live trader could have drawn. A future weekly/monthly extreme is not a known target.

### 14b. “It already exceeded its typical range — how much further?”

**UNTESTED:** this review found no verified primary estimate for NQ satisfying all of: the first crossing of 1.5× the previous twenty completed sessions' median range; a causal intraday timestamp; continuation in the crossing direction; and a stated remaining-session horizon. The probability and number of points are **unknown**, not zero and not a figure inferred from unconditional daily-range percentiles.

**[T] The direct estimate to commission from the owner's history:**

1. **Fix the session and contract.** For this book, the primary observation window is 08:30–14:45 CT. Construct `M_d`, the median high-minus-low range of the previous twenty completed windows, excluding the current day. A conventional 08:30–15:00 cash-session version can be a separate sensitivity analysis. Do not mix a full overnight-session denominator with an RTH numerator without explicitly testing that definition. Use actual tradable contract prices and a declared roll rule.

2. **Define the event once per day.** Let `R_d(t)=H_d(t)−L_d(t)` since 08:30. Set `tau` to the first time before 14:45 when `R_d(t) ≥ 1.5 M_d`. Record whether that expansion came from a new high or low; this determines continuation sign `s`. Record price, time remaining, current range, overnight gap, news schedule, and whether price was outside the **map that was actually available at tau**. Separate “range exceeded” from “map exceeded”; they are not the same event.

3. **Measure continuation and reversal separately.** For horizons fifteen minutes, thirty minutes, and the remainder to 14:45, calculate `C_h=max s×(P(u)−P(tau))` and `A_h=max −s×(P(u)−P(tau))`, over times from `tau` through the earlier of `tau+h` and the cutoff. Record terminal signed return as well. The same-day range increasing because price subsequently makes a new extreme on the opposite side is not continuation in the original direction.

4. **Return usable numbers.** Report the probabilities that `C_h` reaches **0.10, 0.25 and 0.50 times M_d**, plus median, 75th and 90th percentiles in points and range units. These are proposed measurement thresholds, not recommended targets. Also report stop-before-target probabilities for the existing stop geometry, and all timeout outcomes. “Any further” without a positive distance threshold can amount to a single tick and is not an economically useful target.

5. **Report the denominator and uncertainty.** List the eligible dates, actual event count and excluded sessions, with chronological development/test periods and block uncertainty. Separate early and late crossings, directions and scheduled-news conditions only where sample support permits. Never compute a confidence interval as if every tick after the same crossing were a new event. For truncated horizons, state the exposure time or show a fixed-horizon subset.

6. **Connect the distribution to a position.** Simulate both a fade and continuation entry with specified achievable fills, the same dollar-risk convention and cutoff; include no-trade as a zero-return comparator. The best continuation quantile is not automatically a good target for a short fade. If the stored history cannot cover twenty preceding complete sessions plus a substantial evaluation period, this test requires additional historical data; the short in-memory ring cannot answer it.

### 14c. Does an extended day favor exhaustion or standing aside?

**Baltussen, Da, Lammers and Martens (2021)** find intraday momentum in futures. Their NQ series spans April 12, 1996–May 1, 2020, **6,017 days**; earlier-session returns predict the final half-hour. They do not condition on 1.5× median range or an authored map.[^25]

**TRANSFERS?** NQ continuation evidence, with historical-market limits. This book's 14:45 CT cutoff permits only half of the studied 14:30–15:00 closing interval.

**Grant, Wolf and Yu (2005)** find intraday reversal after large opening moves in US stock-index futures over approximately fifteen years, November 1987–September 2002. The accessible primary material supports the reversal finding and weakening after transaction costs; an exact event count was not verified. **Yu, Rentzler and Wolf (2005)** specifically study NQ momentum/reversal relationships involving overnight and prior-day returns; its accessible primary abstract does not establish a reproducible period or sample count for this report.[^26][^27]

**TRANSFERS?** These are index-futures counterexamples to “an early large move must continue.” Neither establishes a fade edge **after** the specified range threshold, with this entry, stop, target and cutoff. The access-limited NQ paper is not a source for a numeric trading rule.

Thus both blanket prescriptions exceed the evidence: “extended means exhausted, fade” and “outside the map means guaranteed continuation.” **[I— inference]** A map is a finite set of observations; its last listed price is not a boundary on possible prices. **[O]** Standing aside when the system lacks a calibrated opportunity can be an explicit risk policy. It is not a published theorem that the optimal response to every out-of-map event is no trade. **[T]** The conditional experiment in 14b decides whether this book's fades have positive net value in that state.

### 14d. Fixed targets versus trailing or structural exits

**Wang, Wu and Chung (2015/2016)** compare TAIEX exits over 2010–2015: **1,291 days, 1,180 trades per arm**. With a 30-point stop, adding a 60-point target lowers average gross profit from **5.09 to 3.69 points** and maximum drawdown from **659 to 581 points**; costs are excluded.[^28]

**TRANSFERS?** An index-futures exit tradeoff, not NQ out-of-map evidence or a test of trailing/structural superiority for this book.

**Leung and Zhang (2017, revised 2019)** derive optimal trading with a trailing stop under a diffusion model; their result can include a sell limit together with the trail. There is no empirical futures sample or observed target hit rate.[^29] **TRANSFERS?** Mathematical counterexample to a universal “trailing means never use a target” claim, not a calibrated MNQ exit policy.

**UNTESTED:** no verified study in this review establishes trailing/structural superiority on the same NQ entries that were identifiable in real time as leaving the map. **[T]** Compare fixed target, fixed initial stop plus time exit, and predeclared trailing/structural alternatives on identical feasible entries. Preserve one-contract execution; do not quietly introduce scaling. Define the out-of-map state at the decision time, rather than selecting completed winning runs afterward. Report net mean, loss tail, drawdown, time under water, missed upside and uncertainty. Optimizing only return on the eventual biggest trends creates selection bias.

**What this means for THIS system.** **[R— Baltussen et al.; Wang et al.]** Allow for continuation and assess how exit rules change the return distribution. **[I— inference]** Do not treat an exhausted map as proof of an exhausted market or assign a probability to an ATR/Fibonacci projection without calibration. **[T]** Measure the first-crossing continuation distribution and the book's conditional net results before adding a target beyond the map or a blanket stand-aside trigger. **[O]** A conservative no-trade policy while those quantities are unknown remains the owner's risk decision.

## Question 15 — Plan staleness

**MIXED — information decay and state-dependent price discovery are tested; an NQ plan half-life, loss of edge per minute, or optimal replan interval for an 11–18-minute authoring process is UNTESTED.**

### 15a. There is no single clock for “the plan”

Osler's published FX levels retained predictive content for several business days, while Chung–Bellotti find asset-dependent decay of their intraday level memory.[^7][^9] These are different objects from a bias forecast or a remaining-session range estimate. Neither supplies a daily/weekly NQ half-life, and a useful old reference does not establish that today's entry permission remains valid.

**Andersen, Bollerslev, Diebold and Vega (2007)** study real-time price discovery around macroeconomic announcements across stock, bond and FX futures. Their July 1998–December 2002 analysis includes S&P 500 futures, **15,764 five-minute returns across 682 announcement days**. Announcement surprises affect prices rapidly, with business-cycle-dependent responses.[^30]

**TRANSFERS?** Relevant evidence that an intervening information event can change the context within a planner's authoring interval. It does not quantify degradation per minute of NQ planning lag, and its sample is not MNQ or an LLM plan experiment.

**Gârleanu and Pedersen (2013)** model dynamic trading with predictable returns and transaction costs, with an application to fifteen commodity futures, January 1996–January 2009, using signals at much longer horizons. Signal persistence and costs affect the desirable rate of portfolio adjustment; exact usable daily observation count was not verified.[^31]

**TRANSFERS?** A general reason to distinguish signal decay from adjustment cost. It neither sets a minute-scale TTL for an NQ plan nor recommends partial scaling for a one-contract strategy.

**UNTESTED:** a source-specific half-life for this system's open-time bias call, session-range estimate, or stored scenario was not verified. A one-second order-flow response, a multi-day FX level and a daily commodity signal cannot be averaged into a plan TTL. An unchanged price level and a changed market state can coexist.

### 15b and 15c. Re-evaluation cadence and publication lag

**UNTESTED:** no verified study measures fixed-interval versus event-driven versus continuous rereading for this kind of intraday NQ plan, including false replans and missed moves. There is no supportable statement that each additional minute costs a specified fraction of edge. Authoring time is also not necessarily source age: retries can reuse earlier facts while some later checks read newer data.[^5]

**[I— inference]** The four clocks describe distinct events:

| Clock | What it dates | What it does not establish |
|---|---|---|
| Observation | When the source fact was measured | That the system received it immediately. |
| Availability | When that fact could actually be used | That a scenario already existed. |
| Publication | When the plan became available to its consumer | That the facts were refreshed at publication. |
| Permission | When the current action was authorized under the scenario | That an old scenario's assumptions still hold. |

An administrative confirmation clock can start at publication. An **information-age** clock cannot truthfully reset there unless the underlying facts are refreshed. This is a causal/accounting distinction, not an estimated trading-performance penalty. A plan may also have a vector of fact ages rather than one timestamp.[^5]

**[T]** Distinguish three interventions before comparing them: reread market state, revalidate an existing scenario, and author a new plan. Revalidation may be a short deterministic comparison; reauthoring may incur the full model latency. An event-driven state check need not discard valid historical structure or produce a new narrative every time price moves. Whether its benefit exceeds false cancellations, computation and missed entries remains a test.

### 15d. What desks document — and what remains private

**[I— doctrine]** Steve Spencer's SMB account, *Why I Game Plan Every Day*, describes preplanned ideas that become actionable as intraday conditions develop. This is an equity-desk practitioner account, not a systematic NQ experiment or published optimal refresh cadence.[^32]

**[I— doctrine]** Wouter Frans's 2024 Optiver account describes automated risk monitoring and coordinated review of positions and economic events. It documents oversight, not a disclosed level-fade algorithm or validated plan TTL.[^33]

**[I— doctrine]** Dalton's auction framework uses developing market information to revise interpretation.[^20] It is not a controlled comparison of invalidation-on-drift, pre-entry revalidation and plan-free execution.

**TRANSFERS?** These sources document practices in other settings, with no statistical sample, dates of measured trades or audited hit rates. They justify labeling the ideas as practice. They do not establish that systematic desks uniformly operate a particular cadence. The exact private implementation and cost of those alternatives are **unverified**.

### 15e. Exact test on stored plans and drift

The source defines immutable `plans` rows keyed by `(plan_id, version)` and append-only `plan_overlays`, with executor references to plan, version, overlay and scenario. `plans.strategy_id` historically stores the **trader ID**. Planner-read facts contain prompt hashes, ATR5m, stop-floor and scope information, but plan/version may be unknown at render time; newer horizon fields cannot be presumed present in older rows.[^34]

The following is **[T]**, a proposed read-only study, not a result obtained in this dispatch:

1. **Stored population.** Enumerate all available NY plan versions and their overlays for the tested traders, including versions never traded, superseded versions, failed/repaired authoring attempts and rejected reads where logs exist. Join exact executor decisions through `(plan_id, plan_version, overlay_version, cited_scenario_id)`; join rendered facts only through verified identity/hash/time links. Retain ASIA/LONDON as separate analyses if included. The prior report's **102 versions/18 date-session groups** is a frozen reference sample, not a claim about today's available population. Report new counts and every included plan/version and read-fact ID.

2. **Reconstruct the information set.** For each material fact, recover observation and availability timestamps, then publication and permission/entry times. Preserve retries and intermediate overlays. Report four distinct ages: availability minus observation, publication minus observation, permission minus publication, and permission minus observation. Do not infer a fresh source timestamp from `created_at` or from a newly written document. Unrecoverable clocks are missing, with exclusions reported; they are not zero lag.

3. **Drift measures.** Freeze actual ATR5m `A_obs` at the fact's observation time. Record signed price drift `s×(P_pub−P_obs)/A_obs` for each scenario direction, maximum absolute excursion during authoring, and the same quantities at permission. Also record remaining target distance divided by current prescribed stop distance; number of premapped zones traversed; new range divided by the prior-twenty-session median; violation of an explicit authored range/bias condition; time remaining to 14:45; and intervening scheduled news. Record both elapsed age and these state changes. Missing ATR excludes normalized calculations, without invented replacements.

4. **Outcomes.** At publication and, separately, at first eligible permission, evaluate a fixed scenario policy using achievable quotes, costs, the original known stop/target geometry and the 14:45 cutoff. Record net P&L per eligible opportunity, target-first/stop-first/timeout, adverse/favorable excursion and whether the scenario was already infeasible when received. Preserve the full untraded opportunity set. Do not evaluate the document against the later plan that eventually replaced it. For actual trade joins use `pnl_corrected`; show and exclude unresolved NULL rows rather than substituting uncorrected P&L.

5. **Forecast comparison and null.** Compare an age-only model, a state/drift model, and the state/drift model plus age, using chronological day blocks and a reserved final period. The primary null is that age adds no held-out predictive improvement once observed state changes are accounted for. Evaluate Brier loss for the fixed barrier outcome and net value of a predeclared admission rule. This separates “old facts tend to be bad” from “facts become bad when the state changes.” It still cannot turn observational authoring delays into an unconfounded causal per-minute decay estimate.

6. **Policy comparison.** Replay the same frozen scenarios under no expiry, fixed revalidation checks at 5/15/30 minutes, event-triggered checks for crossed assumptions, and revalidation immediately before entry. Those intervals are a small proposed experimental grid, not recommended defaults. Keep the response to a failed check explicit: cancel permission, suspend a scenario, or request a new plan. Include the latency and unavailability of replacements. A historical LLM rerun cannot be treated as the original plan; only actually recorded publications were available then.

7. **Lag experiment that can be reconstructed honestly.** Add delays of 0, 2, 5 and 10 minutes **after the actual publication** of the same content and replay only still-feasible entries. This estimates the cost of extra dissemination delay within the tested period. Do not pretend that content taking eighteen minutes to author was available at minute zero. A causal test of faster authoring requires contemporaneous alternative outputs or a prospective design, not a backdated final document.

8. **Costs, n and reporting.** Report independent CME days, plan versions, scenarios, feasible permissions and executed trades separately. Quantify avoided losses, rejected opportunities that would have profited, total return forgone, additional authoring/compute time, and delayed or missed entries. Report day-clustered uncertainty and performance by early/late session and news state where sample sizes permit. Multiple versions reuse the same market path; 102 versions are not 102 independent experiments. With only a small set of dates, the result remains exploratory and needs a fresh forward period.

**What this means for THIS system.** **[R— Andersen et al.; Gârleanu–Pedersen]** Distinguish changing information from elapsed time and the cost of reacting. **[I— inference]** Do not call a publication timestamp a factual refresh, or borrow an order-flow timescale as a plan TTL. **[T]** Measure exact source age, drift and remaining feasible opportunity; compare revalidation policies before setting a timer. **[O]** The acceptable cost of suspended opportunities, repeated authoring and uncertainty belongs to the owner. No published result establishes that 11–18 minutes of authoring is uniformly fatal or harmless.

## The order to act in

1. **[O] Honor the already-decided coverage requirement; [T] establish available information first.** Every-timeframe detection and adequate history are prerequisites to testing daily/weekly structure. That does not award those detections a premium. Freeze candidate availability, episode availability and plan-source clocks before claiming any result. The documented source semantics in Questions 13 and 15 are the clearest immediately actionable findings.

2. **[T] Run the completed-touch and plan-drift studies on the existing record.** These can identify whether stored information adds forward value and whether plans lose feasible geometry before use. They must expose IDs, missingness and independent days. They are tests to perform, not analyses already completed on the owner's database.

3. **[R— microstructure studies] Give execution and causal timing the strongest evidentiary weight; [T] test actual early-versus-late policies.** Of the four questions, Question 13 has the strongest nearby empirical foundation, including forward next-tick stock prediction and direct ES/NQ execution/impact evidence. Its exact forming-candle rule still has no validated hit rate. Round 10's fill constraints and Round 11's reaction-versus-trade distinction apply throughout.

4. **[T] Estimate the conditional NQ continuation distribution before choosing out-of-map targets or an extended-day filter.** Question 14's requested probability has no verified answer. Neither an ATR multiple nor a Fibonacci line supplies it. The distribution needs enough complete sessions and enough first-crossing events, with this book's cutoff.

5. **[T] Fit role-specific level rankings and compare refresh policies only after the causal datasets exist.** Question 12's 1.2 multiplier and Question 15's optimal TTL have no direct empirical foundation. Use a held-out net-value comparison before promoting either. **[O]** Risk tolerance may justify a conservative temporary policy, but it must be named as an owner choice rather than literature-backed alpha.

The evidence **actually contradicts** the brief's weak paraphrase of Osler's artificial-level comparison; an interpretation of raw stored penetration fields as candle wick/body lengths; use of dimensionless `approach_atr` as ATR points; a same-episode label test presented as forward prediction; and evaluation of intrabar entries using their final-bar states. Round 10 already contradicts fill-on-touch as an adequate execution model. These are specific statistical, data or methodological findings.

It does **not** establish that the current closed-five-minute wait, the 1.5×ATR stop floor, passive entry itself, a fixed target, or a particular plan age makes this book unprofitable. The higher-timeframe multiplier is **unvalidated**, not proven harmful. Missing required timeframes violates the owner's stated specification, independently of whether those levels improve expectancy. Repeatedly fading outside the map has no support from “the market has gone far enough,” but the exact stand-aside threshold still needs testing.

**[I— inference] What not to do:** convert absence of evidence into an invented hit rate; rank timeframes using lookback-window results; treat source age as publication age; fit a classifier to its own label construction; select only filled or eventually winning setups; import an ES, equity or FX coefficient into MNQ; or infer an exit policy from one favorable trend-day example. **[T]** Preserve these distinctions in every subsequent experiment and report negative results alongside positive ones.

## Sources appendix

References below identify the primary material used. “Not verified” means the accessible primary source did not permit confirmation; it is not an assertion that the full paper omits the information. Counts of ticks, bars, orders, trades and days are deliberately distinguished. NQ evidence concerns the index future; execution parameters still require MNQ-specific validation because the contracts have separate order books. Search and access were completed on September 9, 2026.

[^1]: Hoang (2026-09-09). *Research Round 12–15 — four questions, one report, one agent*. Owner-supplied research brief, attachment `d7bdbbd2-740c-49ee-a940-bf7c935bf98f/pasted-text.txt`; no public DOI. Instrument: this MNQ SIM system. Reported samples: 423 first-touch episodes; 58 trades/12 CME days; 1,688 stored touch rows. Exact sample dates and row IDs were not supplied or re-audited. **TRANSFERS:** directly describes the subject system; not independent validation of its measurements or an estimated trading edge.

[^2]: Codex (2026). *Range-fade / level-rejection intraday strategies on index futures — what the style requires to work*. Repository research report, [commit-pinned Markdown](https://github.com/johnwick2921-cyber/vl/blob/8941ec68612cc019edc3002b999272ab2ed20516/docs/superpowers/research/2026-09-08-range-fade/report.md). This is the prior Round 11 synthesis, not a new primary empirical study; its studies have separate instruments and samples. **TRANSFERS:** inherited methodological constraints, not additional independent observations. Round 10's summary is owner-supplied in source 1; its primary execution sources are 3 and 4 below.

[^3]: Lalor, Luca, and Anatoliy Swishchuk (2024; revised 2026). *Market Simulation under Adverse Selection*. [arXiv **2409.12721v3**, §2.2, Table 2](https://arxiv.org/html/2409.12721v3). NQ June 2024 contract, April 25, 2024: n=1,929 TT simulated fills. **TRANSFERS:** execution mechanism, not a universal MNQ rate or candle comparison. Preprint, not audited live fades.

[^4]: Lo, Andrew W., A. Craig MacKinlay, and June Zhang (2002). *Econometric Models of Limit-Order Executions*. **Journal of Financial Economics 65(1), 31–71**. DOI [10.1016/S0304-405X(02)00134-4](https://doi.org/10.1016/S0304-405X(02)00134-4); [primary working version, NBER 6257](https://www.nber.org/system/files/working_papers/w6257/w6257.pdf). Actual limit orders in the 100 largest S&P 500 stocks, August 1994–August 1995; exact order count not verified here. Hypothetical price-based execution measures poorly describe actual execution times. **TRANSFERS:** execution-model caution; not an NQ queue model or fill probability.

[^5]: Codex (2026-09-08). *Four-policy trading study*, [commit-pinned research report](https://github.com/johnwick2921-cyber/vl/blob/8941ec68612cc019edc3002b999272ab2ed20516/docs/superpowers/research/2026-09-08-trading-policy/README.md). Repository synthesis and frozen planner-record analysis: 102 versions, 18 date/session groups, 280 scenario documents, 111 complete geometries. The report links its underlying frozen records; this dispatch did not re-query them. **TRANSFERS:** directly relevant historical system evidence and information-contract reasoning, not validated TTL or independent observations per version. Authoring examples and four clocks are cited from this report rather than attributed to an external trading study.

[^6]: vl contributors (revision inspected 2026-09-09). *AUDIT-CHECKLIST.md*, [pinned repository checklist](https://github.com/johnwick2921-cyber/vl/blob/8941ec68612cc019edc3002b999272ab2ed20516/docs/superpowers/AUDIT-CHECKLIST.md). Methodological source for bounded read-only verification and provenance. No instrument-period empirical sample; **TRANSFERS:** repository process only, not trading evidence.

[^7]: Osler, Carol L. (2000). *Support for Resistance: Technical Analysis and Intraday Exchange Rates*. **FRBNY Economic Policy Review 6(2), 53–68**. [Primary paper, Tables 8 and 10](https://www.newyorkfed.org/medialibrary/media/research/epr/00v06n2/0007osle.pdf). USD/DEM, USD/JPY, GBP/USD, January 1996–March 1998. Approximate n=64,200, summing the paper's rounded 23,700/22,800/17,700 level counts; not independent touches. **TRANSFERS:** comparator method, not NQ ranking.

[^8]: Garzarelli, Federico, Matthieu Cristelli, Gabriele Pompa, Andrea Zaccaria, and Luciano Pietronero (2014). *Memory effects in stock price dynamics: evidences of technical trading*. **Scientific Reports 4, 4487**. DOI [10.1038/srep04487](https://doi.org/10.1038/srep04487); [primary full text and Table 1](https://pmc.ncbi.nlm.nih.gov/articles/PMC3967202/). Nine LSE stocks, 251 days in 2002; exact tick count not verified. **TRANSFERS:** scale-dependent memory hypothesis, not daily/weekly NQ superiority.

[^9]: Chung, Ken, and Anthony Bellotti (2021). *Evidence and Behaviour of Support and Resistance Levels in Financial Time Series*. [arXiv **2101.07410v1**, primary text](https://arxiv.org/html/2101.07410v1). Minute data in 2018: EUR/USD 372,607 observations, Lloyds 127,606, Brent 307,678. The inspected version is the preprint; window length, previous bounces and elapsed time are distinct analyses. **TRANSFERS:** testable level-memory hypotheses, not NQ formation-timeframe weights or a universal half-life.

[^10]: TradingStats.net (2026-03-22). *PDH/PDL Probability: How Often Does Price Break Previous Day High and Low?* [Vendor's primary article](https://tradingstats.net/pdh-pdl-sweep-probability/). NQ one-minute data; claimed 3,121 days/~12.5 years, year table 2014–2025; exact endpoints unverified. **TRANSFERS:** same-index descriptive crossing hypothesis only. Count inconsistencies and nonchronological validation preclude adoption as a calibrated probability; no intraday reaction, weekly/monthly comparison or target-before-stop result.

[^11]: Osler, Carol L. (2003). *Currency Orders and Exchange Rate Dynamics: An Explanation for the Predictive Success of Technical Analysis*. **Journal of Finance 58(5), 1791–1819**. DOI [10.1111/1540-6261.00588](https://doi.org/10.1111/1540-6261.00588); [primary working version, FRBNY Staff Report 125 (2001)](https://www.newyorkfed.org/medialibrary/media/research/staff_reports/sr125.pdf). USD/JPY, EUR/USD, GBP/USD; September 1, 1999–April 11, 2000; n=9,667 orders. **TRANSFERS:** mechanism, not NQ calibration.

[^12]: Lo, Andrew W., Harry Mamaysky, and Jiang Wang (2000). *Foundations of Technical Analysis: Computational Algorithms, Statistical Inference, and Empirical Implementation*. **Journal of Finance 55(4), 1705–1765**. [Author-hosted primary paper](https://web.mit.edu/wangj/www/pap/LoMamayskyWang00.pdf). Daily US stocks, 1962–1996; fifty stocks sampled in each of seven five-year subperiods, with replacement across sampling; not necessarily 350 distinct firms. **TRANSFERS:** formal detection methodology, not tested daily/weekly NQ zones or a best detector. Exact observation count varies across patterns.

[^13]: Fock, J. Henning, Christian Klein, and Bernhard Zwergel (2005). *Performance of Candlestick Analysis on Intraday Futures Data*. **Journal of Derivatives 13(1), 28–40**. DOI [10.3905/jod.2005.580514](https://doi.org/10.3905/jod.2005.580514); [primary abstract on author publication record](https://www.researchgate.net/publication/236964657_Performance_of_Candlestick_Analysis_on_Intraday_Futures_Data). DAX and Bund futures; period, n and precise prediction horizon **not verified** from accessible primary material. **TRANSFERS:** limited caution about tested candlesticks, not a direct NQ forming-candle rejection test.

[^14]: Li, Yifan, Ingmar Nolte, Sandra Nolte, and Shifan Yu (2025). *Realized Candlestick Wicks*. **Journal of Econometrics 250, 106014**. DOI [10.1016/j.jeconom.2025.106014](https://doi.org/10.1016/j.jeconom.2025.106014); SSRN **4507161**; [author manuscript](https://eprints.lancs.ac.uk/id/eprint/229139/1/RCW-JEsubmission.pdf). SPY, January 2, 2014–December 29, 2023; 2,493 days, five-minute OHLC inputs. **TRANSFERS:** volatility-measure methodology; no directional NQ level-rejection coefficient. Do not misidentify the empirical sample as ES.

[^15]: Cont, Rama, Arseniy Kukanov, and Sasha Stoikov (2014). *The Price Impact of Order Book Events*. **Journal of Financial Econometrics 12(1), 47–88**. DOI [10.1093/jjfinec/nbs029](https://doi.org/10.1093/jjfinec/nbs029); [primary arXiv version 1011.6402](https://arxiv.org/abs/1011.6402). Fifty US stocks, April 2010; exact total event count not verified. **TRANSFERS:** order-flow feature/impact methodology only, not future NQ hold probability or trade profitability.

[^16]: Gould, Martin D., and Julius Bonart (2016). *Queue Imbalance as a One-Tick-Ahead Price Predictor in a Limit Order Book*. **Quantitative Finance 16(11), 1659–1671**; [primary arXiv **1512.03492v1**](https://arxiv.org/html/1512.03492v1). Ten Nasdaq-listed stocks, 252 trading days in 2014, sampled 10:00–15:30 ET; event counts vary by stock. **TRANSFERS:** forward microstructure hypothesis; not Nasdaq-100 futures, level-conditioned five-minute prediction or cost-adjusted execution.

[^17]: Takahashi, Makoto (2025). *Returns and Order Flow Imbalances: Intraday Dynamics and Macroeconomic News Effects*. [arXiv **2508.06788v4**, October 8, 2025](https://arxiv.org/html/2508.06788v4), §§3–5. ES BBO data, January 2, 2008–December 31, 2013; 1,490 trading days, 34,512,298 one-second observations. **TRANSFERS:** direct equity-index-futures impact evidence, indirectly relevant to NQ; no tested footprint fade rule or plan TTL. Preprint, not described here as peer-reviewed.

[^18]: QuantProp Labs (date **not verified**). *[Paper-001] Order Flow Imbalance (OFI) as a Short-Term Price Predictor on Nasdaq-100 Futures*. [Publisher's article URL](https://quantprop.io/ofi-short-term-price-predictor-nq/). Vendor-published, located through indexed material; direct retrieval failed. Claimed instrument: NQ. Period, n, horizon, methods, costs and hit rate **not verified**. **TRANSFERS:** no numerical conclusion adopted. Included to distinguish a located vendor claim from verified primary evidence.

[^19]: Dufour, Alfonso, and Robert F. Engle (2000). *Time and the Price Impact of a Trade*. **Journal of Finance 55(6), 2467–2498**. DOI [10.1111/0022-1082.00297](https://doi.org/10.1111/0022-1082.00297); [primary 1999 manuscript](https://escholarship.org/content/qt62c0h04j/qt62c0h04j.pdf). Eighteen NYSE stocks, TORQ November 1, 1990–January 31, 1991, 62-day input span; exclusions apply, exact final tick n unverified. **TRANSFERS:** transaction-duration mechanism, not NQ time-in-zone acceptance prediction.

[^20]: Dalton, James F., Eric T. Jones, and Robert B. Dalton (1990; updated 2013). *Mind Over Markets: Power Trading with Market Generated Information*. **Wiley**, updated practitioner edition; [publisher's introductory excerpt](https://catalogimages.wiley.com/images/db/pdf/9781118531730.excerpt.pdf), [author's books page](https://jimdaltontrading.com/books/). Market-profile/auction interpretation; no reproducible instrument-period sample or tested classifier n. **TRANSFERS:** practitioner vocabulary and process only, not an established NQ dwell threshold or automatic trading rule.

[^21]: TradingView (undated; accessed 2026-09-09). *Repainting*. **Pine Script documentation**, [official source](https://www.tradingview.com/pine-script-docs/concepts/repainting/). Platform explanation of historical/realtime information differences; instrument, period and statistical n not applicable. **TRANSFERS:** causal-data principles apply to NQ bar-based testing; no prediction accuracy or comparative entry profitability is claimed.

[^22]: NinjaTrader (undated; accessed 2026-09-09). *Developing for Tick Replay*. **NinjaTrader 8 Help Guide**, [official source](https://ninjatrader.com/support/helpguides/nt8/developing_for__tick_replay.htm). Documentation, not a trade sample: instrument, period and n not applicable. **TRANSFERS:** directly relevant NT8 replay limitations; replayed indicator events do not establish accurate fills or a complete historical depth stream.

[^23]: vl contributors (revision **8941ec68612cc019edc3002b999272ab2ed20516**, inspected 2026-09-09). Primary field definitions: [`store/touch_episode.go`](https://github.com/johnwick2921-cyber/vl/blob/8941ec68612cc019edc3002b999272ab2ed20516/store/touch_episode.go#L10), and [`kernel/touch_telemetry.go`, penetration, volume, approach and shape functions](https://github.com/johnwick2921-cyber/vl/blob/8941ec68612cc019edc3002b999272ab2ed20516/kernel/touch_telemetry.go#L300). **[A]** Source read directly; no database rows counted. Instrument: the system's touch telemetry; period/n of actual rows not audited. **TRANSFERS:** directly establishes current field semantics, not historical sample consistency or predictive efficacy. The proposed study must check the code version under which each row was generated.

[^24]: CME Group (undated; accessed 2026-09-09). *Fibonacci Retracements and Extensions*. **Technical Analysis education course**, [official lesson](https://www.cmegroup.com/education/courses/technical-analysis/fibonacci-retracements-and-extensions). Educational construction and illustrations; no controlled instrument-period sample, statistical n or target hit rate. **TRANSFERS:** charting definition only; exchange authorship is not evidence of profitable NQ extensions.

[^25]: Baltussen, Gert, Zhi Da, Steven Lammers, and Martin Martens (2021). *Hedging Demand and Market Intraday Momentum*. **Journal of Financial Economics 142, 377–403**. DOI [10.1016/j.jfineco.2021.04.029](https://doi.org/10.1016/j.jfineco.2021.04.029); [author-hosted paper, Appendix Table A.1](https://academicweb.nd.edu/~zda/intramom.pdf). NQ series: April 12, 1996–May 1, 2020, 6,017 daily observations; early history predates E-mini NQ. **TRANSFERS:** NQ intraday continuation evidence, not a threshold-conditioned range study or a strategy ending at 14:45 CT.

[^26]: Grant, James L., Avner Wolf, and Susana Yu (2005). *Intraday Price Reversals in the US Stock Index Futures Market: A 15-Year Study*. **Journal of Banking & Finance 29(5), 1311–1327**. DOI [10.1016/j.jbankfin.2004.04.006](https://doi.org/10.1016/j.jbankfin.2004.04.006); [author institution's publication record](https://researchwith.montclair.edu/en/publications/intraday-price-reversals-in-the-us-stock-index-futures-market-a-1). US stock-index/S&P futures, November 1987–September 2002; exact usable event n not verified. **TRANSFERS:** opening-move reversal evidence, not an MNQ fade after the specified range threshold. Access-limited sample detail is not used for calibration.

[^27]: Yu, Susana, Joel Rentzler, and Avner Wolf (2005). *Nasdaq-100 Index Futures: Intraday Momentum or Reversal?* **Journal of Investment Management 3(3), 55–81**; [SSRN **712168**](https://papers.ssrn.com/sol3/papers.cfm?abstract_id=712168), [journal issue](https://joim.com/issue/2005q3/). NQ; period and n **not verified** from accessible primary material. **TRANSFERS:** instrument relevance only for this review's qualitative comparison; no verified out-of-map or 1.5×-range conditional estimate adopted.

[^28]: Wang, Chia-Hung, Mu-En Wu, and Wei-Ho Chung (2015 conference; proceedings 2016). *Empirical Evaluations on Momentum Effects of Taiwan Index Futures Market*. **Third International Conference on Robot, Vision and Signal Processing (RVSP), 82–85**, IEEE. DOI [10.1109/RVSP.2015.29](https://doi.org/10.1109/RVSP.2015.29); [primary author-posted paper](https://www.researchgate.net/publication/304299040_Empirical_Evaluations_on_Momentum_Effects_of_Taiwan_Index_Futures_Market). TAIEX minute data, January 4, 2010–March 25, 2015; 1,291 days, 1,180 trades per compared arm, Tables 2 and 4. **TRANSFERS:** exit tradeoffs, not NQ trailing-stop superiority; gross results exclude costs.

[^29]: Leung, Tim, and Hongzhong Zhang (2017; revised March 22, 2019). *Optimal Trading with a Trailing Stop*. [arXiv **1701.03960v2**](https://arxiv.org/abs/1701.03960v2); related journal DOI [10.1007/s00245-019-09559-0](https://doi.org/10.1007/s00245-019-09559-0). Diffusion/optimal-stopping theory, with an exponential Ornstein–Uhlenbeck illustration; no empirical instrument, period or n. **TRANSFERS:** conditional mathematical insight, not an observed intraday NQ exit advantage. The reviewed source is the dated preprint version.

[^30]: Andersen, Torben G., Tim Bollerslev, Francis X. Diebold, and Clara Vega (2007). *Real-Time Price Discovery in Global Stock, Bond and Foreign Exchange Markets*. **Journal of International Economics 73, 251–277**. DOI [10.1016/j.jinteco.2007.02.004](https://doi.org/10.1016/j.jinteco.2007.02.004); [primary working version, Federal Reserve IFDP 871 (2006)](https://www.federalreserve.gov/pubs/ifdp/2006/871/ifdp871.pdf). Nine futures markets including S&P 500, July 1, 1998–December 31, 2002; 15,764 five-minute returns/682 announcement days in the cited analysis. **TRANSFERS:** event-sensitive price discovery, not NQ planning-lag decay.

[^31]: Gârleanu, Nicolae, and Lasse Heje Pedersen (2013). *Dynamic Trading with Predictable Returns and Transaction Costs*. **Journal of Finance 68(6), 2309–2340**. DOI [10.1111/jofi.12080](https://doi.org/10.1111/jofi.12080); [author-hosted paper](https://w4.stern.nyu.edu/facdir/lpederse/papers/DynamicTrading.pdf). Fifteen commodity futures, January 1, 1996–January 23, 2009; exact retained daily n not verified. **TRANSFERS:** decay/cost framework only, not an NQ minute-scale replan interval or a one-contract scaling policy.

[^32]: Spencer, Steve (2018-02-19). *Why I Game Plan Every Day*. **SMB Training**, [primary practitioner account](https://www.smbtraining.com/blog/why-i-game-plan-every-day). Equity trading desk; no audited instrument-period sample or statistical n. **TRANSFERS:** conditional preparation doctrine, not systematic NQ revalidation efficacy or refresh timing.

[^33]: Frans, Wouter (2024-01-12). *Risk and reward within a dynamic trading firm: Insights from Optiver’s CRO Europe*. **Optiver**, [primary practitioner account](https://www.optiver.com/join-us/stories/risk-and-reward-within-a-dynamic-trading-firm-insights-from-optivers-cro-europe/). Automated trading firm's risk process; no disclosed level-fade system or empirical instrument-period/n. **TRANSFERS:** documented monitoring practice, not tested NQ plan decay, a universal desk practice or a numerical cadence.

[^34]: vl contributors (revision **8941ec68612cc019edc3002b999272ab2ed20516**, inspected 2026-09-09). Primary schemas: [`store/plan.go`](https://github.com/johnwick2921-cyber/vl/blob/8941ec68612cc019edc3002b999272ab2ed20516/store/plan.go#L11) and [`store/planner_read_facts.go`](https://github.com/johnwick2921-cyber/vl/blob/8941ec68612cc019edc3002b999272ab2ed20516/store/planner_read_facts.go#L10). **[A]** Read-only source inspection. Actual plans/decisions were not queried; sample period and current row n remain unverified. **TRANSFERS:** directly defines the system's available joins and missingness risks, not the result of the proposed drift test.
