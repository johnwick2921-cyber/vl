names rewritten to vl on 2026-09-30 (VL rename)
# Stop and Target Geometry for a One-Contract MNQ Level Fade

The defensible practice is to identify **where the trade is wrong**, identify **where the market offers a plausible exit**, and accept the trade only when those two prices, the entry, execution costs, and the estimated outcome distribution fit together. A volatility floor cannot establish invalidation. A nearby annotation cannot establish a worthwhile target. Neither a high win rate nor an attractive reward-to-risk ratio establishes an edge.

**The missing number is still missing:** no verified study located supplies the distribution of penetration beyond this system's support/resistance zones on intraday NQ or MNQ, from which a defensible universal stop buffer could be read. There are useful index-futures exit comparisons, explicit practitioner rules, and an exact experiment that can estimate the missing quantity. None validates replacing 1.5×ATR with another universal number.

This report addresses the eight Round 22 questions. It does not reopen the earlier entry-adverse-selection, published-level, confluence, or higher-timeframe research. It does distinguish those findings from claims they cannot establish.

**The subsequent local geometry test is included.** Backtest 1B reports all 16 structural-stop/distance-qualified-target cells positive under its optimistic fill assumption and negative under through-tick fills. It retains material simulator limitations and changes both stop and target. It therefore strengthens the case for a correctly simulated geometry experiment, without validating a replacement or proving that a structural stop alone fixes the book.[^24]

**Reading the labels.** Every numbered answer begins with its evidence verdict. Recommendations carry **[R]** researched practice or evidence, **[T]** an experiment for the MNQ tape, **[I]** doctrine or analytical inference, or **[O]** an owner policy decision. [R] does not mean a practice has demonstrated profitability. Source classifications appear at first substantive use and in the numbered Sources notes. **[A]** denotes directly inspected evidence; **[B]** denotes an inference. Unreported sample sizes are explicitly unreported, not assumed zero.

The requested source taxonomy has no separate category for official staff research or theoretical papers. Accordingly, non-peer-reviewed official research and empirical preprints are labelled **VENDOR-OR-SELF-REPORTED**, with their actual status stated; this does not imply that a regulator is a vendor. **RIGOROUS-INDEPENDENT-BACKTEST** is reserved for a sufficiently disclosed, inspectable empirical test, not awarded merely because code or an SSRN identifier exists. No external source located meets that standard for the exact MNQ structural-stop/structural-target comparison.

## 1. Where the stop goes on a level trade

**MIXED — explicit placement rules exist; a superior rule for intraday MNQ level fades is untested.**

For a long, distinguish the reference price `L`, the support zone `[Zlo, Zhi]`, entry `E`, and invalidation boundary `B`. They can be different prices. The stop trigger is `S = B − buffer`, rounded outward to the tick. Risk is `E − S`. The short-side construction is symmetric. This is an analytical definition, not an estimated optimum.

### The actual rules and their costs

| Placement rule | Documented practice or explicit evidence gap | Cost and transfer to MNQ |
|---|---|---|
| Fixed ticks beyond the reference | Connors and Raschke's *Turtle Soup* puts the protective stop one tick beyond the failed breakout's extreme. That is not necessarily one tick beyond the original support line.[^2] | [I] Cheap invalidation can produce repeated small losses. Old futures tick conventions and examples do not validate a one-tick MNQ buffer. |
| Beyond the rejection candle's wick | PATS/Mack specifies a completed signal bar, entry beyond its opposite extreme, and protection one or two ticks beyond the signal bar. Its ES lesson limits initial risk to eight ticks.[^3] | [I] Confirmation changes both the entry price and the sample. The wick must exist before entry; it cannot be the subsequently completed touch bar in a passive-entry backtest. |
| Beyond the zone's far edge | [I] For a hypothesis that the whole support band should hold, `B = Zlo` is the internally consistent boundary. The exact claim that this outperforms wick or swing stops on MNQ is untested. | [I] A broad detector band creates a broad stop. The measured band-width artifact makes zone construction part of the hypothesis, not unquestionable structure. |
| Beyond the last relevant swing | Brooks illustrates a swing trade protected below the originating bull leg; the trade should retain its intended management rather than become a scalp under pressure.[^4] | [I] Requires identifying the particular swing that invalidates this trade. “The last swing” is not sufficient if it belongs to an unrelated timeframe. |
| Volatility multiple | Carter publishes ATR-based protection in his stock/options swing plan. His intraday futures material also contains fixed-distance and time rules, discussed below.[^5] | [I] Volatility scales exposure to movement; it does not prove structural invalidation. A daily ATR stock rule does not transfer to a 5-minute MNQ floor. |
| Fixed dollar risk | CME education sequences a logical stop with account risk and position size. It says a stop should not be placed randomly or where normal movement easily reaches it.[^6] | [I] With one MNQ, a dollar cap is an admission constraint. Moving the stop merely to fit the cap changes the trade. |
| Below the next minor level | No verified named primary source located establishes this as a universal index-futures rule. It is a possible structural rule only if that minor level is the declared invalidation boundary. | [T] Test it as its own rule. Do not search successively lower levels until a loss becomes tolerable or the old floor is satisfied. |

**Source status and samples.** Connors/Raschke, PATS, Brooks and CME are **VENDOR-OR-SELF-REPORTED** practitioner/education sources, not controlled stop-placement studies. The book includes 1990s futures and stock illustrations, including an intraday S&P example; PATS explicitly discusses ES 2,000-tick or 5-minute charts; Brooks uses E-mini teaching examples. None supplies a systematic MNQ sample or head-to-head test. PATS explicitly allows a better-priced retracement entry or skipping when its structural stop exceeds its limit; the stated cost is missing trades.[^2][^3][^4][^6]

### What has actually been compared on index futures

**[A] Wang, Wu and Chung (2015), PEER-REVIEWED:** TAIEX one-minute momentum strategy, **4 January 2010–25 March 2015; 1,291 days, 1,180 trades per main variant**. They compare 17 fixed stops, 20–100 points in five-point increments. **Costs excluded. Transfer: indirect; neither the TAIEX momentum result nor its distances establish an MNQ fade stop.**[^7]

**[A] Howard (2026), VENDOR-OR-SELF-REPORTED preprint:** ES **breakout continuation**, **January 2025–January 2026; 6,284 events, one-second data**. Its abstract reports all 12 price-stop distances underperforming an unmanaged horizon; a combined trailing/break-even/no-progress exit stack beats the price-stop baseline in 11/13 months. **Transfer: not MNQ fade calibration.** Full methodology, costs, exact grid and independent holdout could not be verified. The combined stack does not isolate a time-stop effect.[^8]

**[T] Buffer candidates:** ticks, a fraction of the frozen zone width, a fraction of trailing completed-bar ATR, and a training-only overshoot quantile are all testable. None has an empirically established winning value here. Buffer size must be evaluated by the entire resulting trade distribution, not just by how many eventual rebounds survive it.

## 2. The stop-hunt problem and the missing overshoot distribution

**MIXED — order clustering and cascades are supported; a safe MNQ distance beyond a cluster is untested.**

**[A] Osler (2003), PEER-REVIEWED, Journal of Finance:** the inspected working version contains approximately **9,700 conditional FX orders at one dealer, September 1999–April 2000**, principally dollar-yen, dollar-pound and euro-dollar. It documents different clustering of profit-taking and stop orders around round numbers. This is evidence about order placement, not proof that every chart zone contains the same queue of stops.[^9]

**[A] Osler (2005), PEER-REVIEWED, Journal of International Money and Finance:** the inspected working paper combines **9,655 dealer orders, August 1999–April 2000**, with separate **1996–1998 exchange-rate quotes**. Stops can amplify moves after clustered prices are reached. **Transfer for both papers: the mechanism is relevant to a fade's failure, but FX pips, cluster widths and event responses cannot be converted into an MNQ buffer. Neither paper identifies malicious targeting of this system's individual stop.**[^10]

**[A] Fett and McPhail (2017), VENDOR-OR-SELF-REPORTED, official CFTC staff research:** complete **2014–2016** execution audit trails for **ES, Treasury-note and crude-oil futures**, plus sampled order-book days. ES includes **22,789,175 contracts of executed stop volume**, not that many independent stop orders. Stops are not publicly visible before triggering; some serve latency strategies. The paper measures stop execution slippage and its intraday concentration. Its historical ES protection example is three points. **That is an execution protection range, not a measured reversal overshoot or an MNQ stop recommendation. Transfer: exchange-order mechanics, not stop-buffer calibration.**[^11]

### Four distinct responses to a possible sweep

| Response | What it changes | Assessment |
|---|---|---|
| Put the stop beyond the supposed cluster | Pays more on failures to survive more penetrations | [I] Plausible only with measured conditional recovery and cost. There is no universally observable “far side” of the stop cluster in minute OHLCV. |
| Wait for penetration and reclamation, then enter | Changes the entry event; protects beyond the observed failed-break extreme | [R] Documented failed-break practice; [T] a separate MNQ entry experiment. It may miss immediate rebounds and buy higher. |
| Take the original stop and accept the sweep | Keeps the original bounded thesis and loss distribution | [I] Coherent if the strategy remains profitable after those losses. Re-entry is a separate policy; it is prohibited in the present book. |
| Replace the price stop with a mental or time exit | Changes exposure to fast continuation, gaps and delayed action | [I] It removes some trigger-based exits, not adverse movement. No measured evidence here establishes superiority for this autonomous book. A timed exit can coexist with hard protection. |

A Brooks-course forum discussion describes exactly the one-contract difficulty: a trader cannot simply halve size to accommodate a wider stop. The discussion favors stronger entry selection rather than treating every one-tick stop-out as evidence to widen. **COMMUNITY-ANECDOTE**, February 2022, no audited trades or outcome sample; it is not a measured Brooks result.[^12]

**The requested number:** no verified **NQ/MNQ support-zone penetration-before-reversal distribution** was found. A March 2025 futures-community thread claims NQ overshoots more than ES even after ATR normalization, but provides no sample or calculation. **COMMUNITY-ANECDOTE; no quantitative transfer.**[^13] A stop-slippage distribution, winner-only MAE histogram, average candle range and failed-break screenshot are four different quantities; none substitutes for the missing distribution.

**[T] The useful output is a conditional table, not one global average:** level family × session × entry mechanism × pre-entry volatility, with counts, unrecovered fraction, time to reclaim, and the 50th/75th/90th/95th percentiles of penetration among defined reclamations. Section 8 specifies the event and the competing failure outcome. No buffer can promise survival of every sweep while also limiting genuine breakdown losses.

## 3. Where the target goes

**MIXED — structural targets are well documented; their superiority to fixed-R or trailing exits on MNQ fades is untested.**

“Next level” requires a frozen **eligible level set**, a directional rule and an executable price. For a long targeting resistance zone `[Tlo, Thi]`, the near edge is `Tlo`; its anchor and far edge are different targets. A sell limit slightly before the near edge sacrifices some reward to seek an earlier fill. This is a proposed implementation distinction, not a validated improvement.

| Target family | Concrete definition | Evidence and limitation |
|---|---|---|
| Nearest reference of any kind | Lowest map price above entry | [A] Close to the existing harness rule. It can select another anchor inside the same broad region, rather than a distinct opposing auction boundary. |
| Nearest eligible structural obstacle | First non-overlapping opposing decision zone from an ex-ante eligibility rule | [I] Most faithful codeable version of “next meaningful level.” No published ranking selects that eligibility rule for MNQ. |
| Nearest level beyond minimum reward | Ignore nearer references until a farther one satisfies a distance/ratio | [I] This is a different strategy, not a repair justified by arithmetic. It must explain why the intervening references do not merit exiting. |
| Opposite range edge | Other boundary of an identified balance, optionally comparing a midpoint exit | [I] Requires a range defined before entry. The day's final high/low cannot define the target retrospectively. |
| Prior-session extreme | Prior day high for a long, low for a short, if directionally valid | [T] A fixed structural alternative, with intervening obstacles recorded. Its distance says nothing by itself about reachability. |
| Measured move or extension | Target derived from a completed leg/range using a prespecified projection | [I] A projection rather than an already observed level. Carter's extension exits are doctrine, not independent outcome estimates.[^5] |
| Fixed R | `T = E + r(E−S)` for a long | [T] Useful control. It normalizes geometry but can move the target away from the market's actual stopping points. |
| Trail or timeout | Exit according to subsequent path or elapsed time | [T] An alternative payoff distribution; it must be compared on identical initial entries and risk. |

**Practitioner specificity.** Carter lists pivot retracement limits, a **0.50-point NQ entry offset**, and **ES three-point stop/two-point target/35-minute timeout** management. No audited sample or corresponding NQ stop distance is given. **VENDOR-OR-SELF-REPORTED** rules; unsupported success claims are **MARKETING**. **Transfer: candidate mechanics only.** The separate daily-ATR stock/options plan is not an intraday MNQ rule.[^5]

Dalton's *Daily Trading Routine* and his May 2019 preparation account distinguish balance, trend, timeframe and meaningful auction boundaries. Steidlmayer and Hawkins's *Steidlmayer on Markets* supplies the Market Profile framework. These are **VENDOR-OR-SELF-REPORTED** practitioner sources, not target-comparison datasets: **n not reported**, historical futures examples. No verified universal “next minor line,” buffer, or minimum grade follows from them. **Transfer: contextual vocabulary and hypotheses, not an MNQ target-ranking algorithm.**[^14][^15][^16]

**Measured comparison.** Adding a 60-point target to the TAIEX study's 30-point stop reduces gross mean return from **5.09 to 3.69 points**, while reducing maximum drawdown from **659 to 581** and raising the win rate from **44.58% to 46.10%**. That is a real return/drawdown tradeoff; it is not proof that targets are bad for mean reversion. The sample and transfer restrictions are in Section 1.[^7]

**Code repositories.** Wojtek0110's ES opening-range repository compares fixed stops/targets, break-even and range-scaled R settings. Its README explicitly excludes costs, acknowledges minute-bar ambiguity and lists walk-forward testing as future work; historical dates and trade count are not reported there. **VENDOR-OR-SELF-REPORTED; not a rigorous net-profit validation.** Its code makes variations inspectable, but provides no structural-target advantage for MNQ fades.[^17]

**[T] The primary comparison should keep entries unchanged and compare the first eligible zone's near edge, its anchor, a frozen range edge, fixed-R exits, and a trail.** Record the share reaching each nearer obstacle before eventually reaching the farther target. A farther target is credible only if the remaining conditional continuation justifies holding through those obstacles.

## 4. Choosing stop and target together, and refusing the trade

**MIXED — joint planning is documented; a universal minimum R:R improvement on intraday MNQ is untested.**

Both sequences occur. CME teaches logical stop, monetary risk, then size.[^6] Topstep's *Risk–reward framework* explicitly describes **target-first** planning from previous resistance and **stop-first** planning when the target is less clear. Its examples discuss structural breaks and volatility, but no comparative trading sample or measured refusal frequency. **VENDOR-OR-SELF-REPORTED**, 2026, futures education. **Transfer: an explicit planning procedure, not evidence that 2R is optimal.**[^18]

Topstep's *Trader's equation* article also recognizes probability as a third variable and provides risk/reward examples, including an unusually tight prior-low entry. Its currently displayed byline is **John Doherty**, not Al Brooks. Its numerical probabilities are teaching assertions without an audited denominator. **VENDOR-OR-SELF-REPORTED**, 2026, ES examples; **no empirical MNQ transfer.**[^19]

**[I] The strongest sequence is joint constraint checking:** declare the setup and invalidation; locate the first credible obstacle and intended target; compute executable risk/reward and outcome probabilities; admit or refuse. The first two steps can be performed in either order. Neither price should be manufactured to satisfy the gate.

### Why R:R cannot rescue a trade by itself

For a binary trade with gross target gain `g`, gross stop loss `d`, and constant round-trip friction `c`:

```text
Expected net points = p*g − (1−p)*d − c
Break-even target-before-stop probability = (d+c)/(d+g)
Net reward/risk at the two exits = (g−c)/(d+c)
```

These are arithmetic identities under the stated two-exit assumptions. Timed exits, gap losses and variable fills require the full empirical distribution.

Using the supplied rounded results, `0.899×4.12 − 0.101×24.57 − 2 ≈ −0.78` points. The observed win/loss sizes require about **92.6%** wins after two-point friction. Therefore **89.9% is insufficient**, but “no win rate saves it” is mathematically too strong. With the illustrative fixed **23-point loss and four-point gain**, two-point friction requires **25/27 = 92.59%** wins. In contrast, risking 15 to make 120 requires about **12.59%**, but the probability of reaching 120 before losing 15 is not supplied by a 50% level-hold statistic.

**[B] The critical selection effect:** moving a target farther ordinarily reduces its hit probability; moving a stop closer ordinarily increases premature exits. In a zero-drift continuous martingale with suitable stopping conditions, a target at `g` and stop at `d` have hit probability `d/(d+g)` and zero gross expectation. Costs make it negative. That is a mathematical null illustration, not a fitted model of Nasdaq. It shows why changing the ratio is not automatically creating information.

**[A] Leung and Li (2015), PEER-REVIEWED theory:** an Ornstein–Uhlenbeck mean-reverting spread with transaction costs yields jointly determined entry/exit regions, and changing the stop changes the optimal profit-taking boundary. **No empirical index-futures sample; n not applicable. Transfer: the principle that decisions interact, not any numerical rule for MNQ.**[^20]

**[T] Test two different operations separately:** (1) reject an already defined structural trade when its ratio is below `r`; (2) move its target to `r` times risk. The second does not test the first. Compare each filter on net points **per admitted trade and per eligible trading day**, including zero-trade days. A discretionary trader's selected winners cannot estimate a systematic filter unless every contemporaneous candidate, rejection and decision time is logged.

**How often professionals refuse:** no audited denominator was located for this style. A teaching statement about selecting two or three opportunities is not a measured refusal percentage. **[O] There is no evidence-derived trade quota.** If no admitted trade remains after honest constraints, zero trades is the policy outcome; it is not permission to relax an unvalidated gate until activity appears.

## 5. The minimum stop problem

**UNTESTED — no universal minimum NQ stop or 1.5×ATR5m floor is established by the inspected evidence.**

A minimum stop can attempt to protect against four distinct things: the price grid/spread, execution uncertainty, ordinary conditional price variation, or a poor entry location. These require different measurements. A wider stop can accommodate the third; it does not necessarily cure the fourth. ATR is an average range statistic, not a percentile of adverse movement conditional on this entry.

**[R] Documented practice is heterogeneous.** The PATS rule is a **maximum** initial ES stop, with an alternative entry or refusal when structure requires more; Brooks emphasizes enough room for the intended trade and less size when required risk rises.[^3][^4] Neither establishes a modern NQ minimum. The forum and futures-community complaints establish that traders experience stop-outs before reversals, not their unconditional frequency.[^12][^13]

**[I] Structural validity and affordable exposure must both hold.** If the chosen boundary is two points below entry but training data show that a viable version needs additional buffer, the entire trade must be recalculated. If the resulting risk no longer fits the available target or one-contract dollar cap, refuse it. Do not keep the tight stop solely to display 2R; do not automatically widen to 1.5ATR and retain a four-point target.

**[T] Distinguish a structural buffer from a minimum total distance.** `S = Zlo − b` and `S = E − max(E−Zlo, 1.5ATR)` are different hypotheses. The former measures tolerance beyond invalidation; the latter can place the stop far from that boundary whenever entry is nearby. Compare them explicitly with the same entry and target.

**[O] Preventing random LLM stops is a specification problem.** Require a boundary identifier, its timestamp, its numerical price, the selected buffer rule/version and the resulting trigger. Reject an unsupported price instead of silently replacing it with a volatility floor. **[T] Keep the legacy floor as a benchmark during research; no evidence here authorizes its replacement by an invented production constant.**

No measured figure in this report answers “a three-point NQ stop is hit by normal noise X% of the time.” That probability depends on entry, session, volatility, horizon and the competing target. An unconditional five-minute range cannot answer it.

## 6. When the next level is too close

**DOCTRINE-ONLY — refusing poor geometry is documented; the optimal room requirement is untested.**

The current four-point target/twenty-three-point stop has two problems: very little net reward after friction and a high required success probability. Reducing quantity scales dollars but does not improve those point economics. With a mandate of one MNQ, the allowed quantities are **one or zero**, not half a contract. MNQ's outright tick is 0.25 points, worth $0.50; the contract is $2 per index point.[^21]

**[I] There are four coherent choices, each requiring an explicit rule:**

1. **Refuse now.** The existing structural trade has insufficient room for its validated admission policy.
2. **Wait for a better entry.** Keep the original invalidation and target, improve price only if the market offers it, and model the changed fill selection. A missed trade is an allowed outcome.
3. **Trade a different setup.** A completed failed break may create a different invalidation boundary and a different population of trades. It is not the original resting fade with a retrospectively tighter stop.
4. **Use a different target only under an independent target rule.** The nearer feature may be excluded by a frozen definition of eligible decision zones, or holding through it may be part of a validated continuation policy. Failing the ratio by itself is not a valid exclusion reason.

**[T] Diagnose map crowding directly.** For each rejected candidate, save every reference between entry and target, whether zones overlap, the source families, and the reason each reference was included or excluded. Compare the nearest-all-anchor policy against a deduplicated decision-zone policy using the same entries. “Meaningful level” must become inspectable data, not an LLM adjective or an assumed higher-timeframe premium.

**[A] A relevant but limited entry comparison:** Gandhi (2026), **VENDOR-OR-SELF-REPORTED** preprint, reports **3,908 Nifty futures trend-continuation trades, 2010–2026**. An approximately 0.25% delayed-entry threshold retains 63% of signals and raises reported Sharpe from 1.38 to 3.33 after stated costs. Fills are inferred retrospectively from trade MAE; the author discloses a financial interest. **Transfer: no MNQ buffer or structural-stop calibration.** The abstract does not establish an intraday holding constraint, independent replication or queue-aware fills. It illustrates the price-versus-participation tradeoff, not a validated shortcut around path reconstruction.[^23]

**[O] On days with no room, this book has no trade.** Watching, refreshing an invalidated map, or researching another strategy does not require taking a fade. The sources do not establish that profitable traders always manufacture a daily opportunity or that one trade per day is optimal.

## 7. Scaling, partials and the runner

**UNTESTED — no verified evidence establishes a superior one-contract MNQ exit; partials are mechanically unavailable within one MNQ.**

The target remains the intended exit of the entire contract. In a multiple-unit position, one can realize part near an obstacle and retain part for continuation. With one unit, these are mutually exclusive actions. Trading another instrument or increasing size is outside the stated strategy.

**[A] Connors/Raschke's Turtle Soup Plus One describes partial profit-taking within two to six bars and trailing the balance. Their failed-break examples also discuss accepting losses and subsequent opportunities. These are documented management rules, not a randomized comparison of partials versus a one-unit exit.**[^2]

**[A] Bellafiore's 2013 SMB account of an EDU trade uses a break of the intraday trend as a reason to cover and a later resistance test as a new short.** The example couples structural risk with share sizing and allows re-entry. **VENDOR-OR-SELF-REPORTED**, one illustrated US-stock trade sequence, no controlled sample. **Transfer: management must match the setup; its stock sizing and re-entry cannot be silently imported into this fixed one-contract book.**[^22]

**[I] A runner is not free upside.** Holding the full contract for a farther target forfeits the partial profit that a scaled trader would already have taken. A trailing stop can retain large moves but also return unrealized gains. A fixed target can stabilize outcomes while truncating a favorable tail. Which matters more for this fade is empirical.

**[T] Compare three full-contract exits with identical initial entry/stop:** first structural target; a prespecified closed-bar trail; and a prespecified timed/no-progress exit with hard protection. Also compare fixed-R controls. Keep stop movement and re-entry disabled in the primary structural-target arm. No inspected comparison determines the best exit for this book.

## 8. The numbers that would settle it

**UNTESTED — the following is a preregistrable experiment, not a claim that the existing tape already validates a replacement.**

### First establish what the existing result measures

**[A] The frozen local research report and code are primary system evidence, VENDOR-OR-SELF-REPORTED.** They report **11,302 zone touches**, including **3,613 NY**, **3,708 Asia** and **3,981 London** episodes, over **11 April 2022–11 September 2026**. The current NY book is therefore not represented by 11,302 independent eligible trades. The reported detector hold rate uses **8,232 resolved episodes with 3,070 excluded**, not all 11,302. The confluence comparison is **three-or-more families versus one**, not every multi-source zone versus every single-source zone.[^1]

The negative aggregate and 0/81 positive surface cells are reasons to withhold confidence. They do **not** prove that geometry alone caused the loss. The old surface changes width, merging, family cap and ATR floor, rather than comparing the structural-stop/structural-target rules specified here. Nor does a hold/break proportion establish target-before-stop probability.

Four directly inspected simulator details must be addressed before the old excursions become calibration data:

| [A] Finding | Exact frozen evidence | Why it matters |
|---|---|---|
| A band touch is automatically treated as an anchor fill | `firstTouchIdx` tests band intersection; `FillA = true` follows without an anchor-trade condition | A bar can touch the zone without trading the resting limit price. |
| The “adverse” entry offset is favorable | Short `EntryC = anchor + 0.25`; long `EntryC = anchor − 0.25` | It improves every otherwise identical outcome by 0.25 points. The −0.54 figure is not adverse slippage. |
| Exit evaluation begins after the touch bar | `simulateTrade` starts at `touchIdx + 1` | Stop/target hits and excursion on the fill bar are omitted. Later stop-first treatment does not repair this. |
| Episodes are evaluated independently | Per-zone simulations lack a one-position portfolio scheduler | Overlapping opportunities cannot all become one-contract trades. Episode-order cumulative drawdown is not necessarily a tradable equity curve. |

The first three code locations are linked by line in Source 1; the fourth is the evaluation/aggregation design. **No harness rerun or production modification is represented here.** Holding the old paths fixed, reversing the entry-offset sign would change −0.79 to approximately **−1.04**, not −0.54; this is algebraic sensitivity only, not a corrected backtest. Anchor-fill and fill-bar corrections need actual reruns and can change the paths.

### The subsequent Backtest 1B: useful measurement, not a validated fix

**[A] The newer local report, committed at 11:14 CT on 12 September, tests four structural buffers × four minimum target-distance ratios on the same reported 11,302-touch cohort.** Its CSV's all-era fill-A means range from **+0.63 to +4.77 points**; every corresponding fill-B cell is negative, **−3.44 to −1.69**, with **3,586 through-tick fills per cell**. The widely quoted **+2.41 median / +0.39 to +4.21 range describes the in-sample surface**, not the all-era table. These are directly read artifact results, not independently replicated trades.[^24]

**[A] Its copied simulator still gives fill C favorable entry prices and begins exit evaluation after the touch bar.** A band intersection still automatically qualifies fill A. The new target function skips closer anchors until distance exceeds `minR × risk`; if no target qualifies, the trade remains eligible to stop or flatten. That is **target relocation plus a stop change**, not the proposed experiment of fixing a structural target and refusing bad geometry. The through-tick condition is applied on the original band-touch bar, so it also does not reconstruct all possible later resting-order fills.[^24]

**[B] Consequently, neither “the fill reality is whichever assumption the owner chooses” nor “the stop was the whole difference” follows.** Fill behavior is an empirical execution question, and the stop's effect is confounded with target relocation. The sign-flip significance also requires the distributional/dependence qualifications below. **[T] Preserve these 16 cells as additional benchmarks, with the existing reference cell, but rerun them after semantic corrections.** A fair next measurement includes fixed-target/changed-stop and fixed-stop/changed-target arms. Do not discard the negative fill-B result or promote the positive fill-A result as executable expectancy.

### A. Repair measurement semantics before optimizing

**[T] Freeze and verify:** contract identity, open/close timestamp convention, decision-time detector inputs, completed-bar ATR, zone eligibility, limit submission time, cancellations, session cutoffs and tie-breaking. Add small semantic fixtures for band-only touch, actual through-price fill, favorable/adverse offsets, fill-bar stop, fill-bar target, a gap across the stop, simultaneous triggers and overlapping candidate orders. The verification should exercise the actual simulation call path.

**[T] Use minute data honestly.** Require the limit to have been resting before the bar. A trade-through condition is a conservative fill proxy, not a queue reconstruction. On any bar where entry and exit ordering is unresolved, calculate feasible favorable/adverse path bounds or use finer data; do not omit that bar. Conservative ordering must be conditional on a feasible fill, not a stop that happened before entry. Report the ambiguous-event count and the P&L bound attributable to it.

**[T] Preserve both an episode panel and a portfolio replay.** The panel estimates conditional paths. The replay has one position, one deterministic candidate priority, no re-entry and no fractional contracts; it cancels incompatible pending entries once filled. Mark equity chronologically, including unrealized P&L and zero-trade days. Report closed-equity and marked-equity drawdowns separately. Use the exact existing session eligibility and 14:45 CT flat rule as the primary policy, not all sessions pooled.

**[T] Reconcile aggregates to event identifiers.** Save `contract`, `CME_day`, `read_id`, `zone_id`, `touch_time`, `order_time`, `fill_time`, `entry`, `boundary_id`, `S`, `T`, `exit`, `exit_reason`, costs and every refusal reason. All counts should reconcile from this ledger; unresolved rows remain unresolved. The frozen repository contains aggregates and simulator source, not a newly verified row-level event ledger for this review.

### B. Measure overshoot without selecting only successful trades

**[T] Long-side definition:** freeze the zone and its lower boundary `Zlo` before the touch. From the first actual entry-eligible encounter, observe the subsequent path to a fixed horizon `H`. Define penetration at time `t` as `max(0, Zlo − low_so_far(t))`. Record first penetration, first completed-minute close back at or above `Zlo` after penetration, and maximum penetration **before that first reclaim**. Short-side signs reverse.

**[T] Report non-penetrations and unreclaimed penetrations separately.** A reclaim is not a profitable trade: also record whether the selected target is reached before further failure, how long the reclaim takes, and the costed outcome. Calculate path statistics without censoring at the old strategy's stop; otherwise the data cannot evaluate wider alternatives. For a reclaim and deeper low within the same minute, publish bounds because their order is unknown.

**[T] Use horizons `{5, 15, 30, 60}` minutes, truncated at the policy cutoff.** These are proposed research horizons, not discovered constants. For each, provide the unreclaimed mass and conditional penetration quantiles `{50, 75, 90, 95}%` in points, ticks, zone widths and pre-entry ATR units. Use only training observations to set a quantile-based buffer. A 95th percentile among reclaimed events does not imply a 95% trade win rate or protect against the unreclaimed tail.

**[T] Stratify sparingly:** NY versus other sessions first, then broad volatility groups and the frozen source-family groups. Report cells too small to estimate rather than continuously subdividing until a favorable tail appears. This is the experiment that can return the useful number absent from external literature.

### C. A bounded stop/target comparison

**[T] Hold fixed in the primary experiment:** the eligible touch cohort, resting-entry rule, live detector outputs, map construction, contract/session policy, costs, one-position scheduling, cutoff, no scaling, no stop movement, no re-entry and all entry filters. A completed rejection or failed-break entry belongs in a separate experiment; a future wick must never determine the resting-entry stop.

**[T] Primary grid: 24 structural cells.** Cross two boundaries × four buffers × three structural targets:

| Component | Preregistered variants | Exact definition |
|---|---|---|
| Invalidation boundary, 2 | Zone far edge; last relevant confirmed swing beyond that edge | Long: `Zlo`; or most recently confirmed swing low at/below `Zlo`, known before entry. No qualifying swing means no candidate in that arm. Mirror for shorts. Freeze the swing detector/version. |
| Tick buffer, 4 | `{1, 2, 4, 8}` MNQ ticks | `{0.25, 0.50, 1.00, 2.00}` points beyond the boundary, with outward tick rounding. These are coarse candidates, not a recommended optimum or guaranteed sufficient noise allowance. |
| Structural target, 3 | Nearest eligible zone near edge; same zone anchor; prior-session directional extreme | Use only pre-entry information. Nearest means price order, not nearest that passes R:R. Exclude the entry zone and merge overlapping intervals into one component before identifying a distinct opposing zone. A missing/invalid target means refuse. |

**[T] Freeze a transparent decision-zone definition before seeing returns.** For the first run, include every complete-width, seated, directionally valid zone from the frozen live map, collapse overlapping intervals, and use a stable ID for ties. Do not add an untested quality score. “Opposing” initially means a distinct zone in the profit direction; a resistance/support polarity requirement is a separately declared eligibility experiment. Preserve source identities when collapsing intervals, so the effect of deduplication remains inspectable.

**[T] Controls, seven additional cells:** the unchanged legacy rule; zone-edge stop with a two-tick buffer and targets at `{0.5, 1, 2, 3}R`; an anchor-based fixed three-point stop; and an anchor-based fixed twenty-four-point stop, the latter two targeting the nearest eligible zone's near edge. The fixed stops are diagnostics, not structural recommendations. This makes **31 initial geometry cells**. If a rule yields an impossible bracket, record a refusal rather than adjust it retrospectively.

**[T] Second-stage buffer comparison:** within each training fold only, take its selected structural boundary/target and compare the four tick buffers with ATR fractions `{0.05, 0.10, 0.20, 0.30}`, zone-width fractions `{0.05, 0.10, 0.20}`, and training-derived reclaim-overshoot quantiles `{50, 75, 90, 95}%` at a prespecified 15-minute horizon. That is **15 buffer choices**, of which four already appeared. Record every trial; no outside-fold selection. Round non-tick distances outward. Undefined/underpowered quantiles make that candidate unavailable, not zero.

**[T] Third-stage admission comparison:** on the training-selected structural geometry, test no ratio gate and gross minimum ratios `{1.0, 1.5, 2.0}`. Separately report the net ratios after costs. Do not move the structural target when it fails. Retain the currently required 2R arm as the production-policy benchmark. This isolates the filter's effect from the effect of manufacturing a new target.

**[T] Exit/entry extensions are separate finite studies.** After freezing the first study, compare the chosen full-contract target against: fixed time exits `{5,15,30,60}` minutes; a no-progress exit at `{5,15,30}` minutes when marked gross P&L is non-positive; and a trail behind the last completed `{1,3,5}` one-minute bars, activated after `1R` favorable movement and updated only after bar close. All retain initial hard protection and the cutoff. Treat these ten alternatives as additional trials. A later closed-rejection/failed-break entry study must restart selection accounting and preserve entry-time availability of every stop boundary.

### D. Chronology, costs and selection

**[T] Use expanding chronological development folds.** Start with approximately twelve months of training, test the next three months, then roll forward three months. Purge any overlapping position/label horizons at fold boundaries. Every buffer, shortlist and admission choice is selected inside training only. Aggregate the resulting genuinely forward-within-design segments, with separate yearly and session diagnostics.

**[T] The previously disclosed final year is no longer pristine for a new geometry hypothesis.** It remains useful as an explicitly exposed validation period, but the new rule was motivated partly by its results. Freeze the eventual candidate and collect an additional prospective SIM segment before claiming new confirmation. Do not rename reused data “untouched holdout.”

**[T] Costs:** keep two points round trip for comparison and stress three and four points, with stop-gap and limit-fill handling explicit. If fills already include adverse price offsets, do not charge those same offsets again as slippage inside the fixed cost. Break out commissions, spread/price impact proxies and any remaining friction. The stress levels are research choices, not empirical estimates of this account's brokerage costs.

**[T] Report:** eligible touches, admitted orders, fills, distinct days, ambiguous bars, refusals by cause, gross/net mean, profit factor, target-before-stop rate, win/loss sizes, time exits, expected shortfall, worst day, marked drawdown, drawdown duration and recovery time. Show distributions by year and volatility regime. Include a paired policy comparison on common days; comparing only each policy's selected fills conceals the economic cost of exclusion.

### E. Null hypotheses, minimum sample and power

**[T] Primary profitability null:** `H0: μ_net ≤ 0` for the complete one-contract policy under conservative feasible execution and stated costs. **Secondary improvement null:** `H0: μ_new − μ_legacy ≤ 0` using paired day outcomes. Beating a negative legacy strategy is insufficient for admission.

**[T] Level-information null:** compare actual level entries with training-specified matched placebo references/times preserving session, volatility, distance to price, zone-width distribution and geometry. This tests information in the selected levels rather than whether an asymmetric bracket can generate many wins. A 50% hold null does not answer this question.

**[T] Minimum reporting threshold proposed here: 200 filled trades across at least 60 distinct CME trading days in the evaluation cell.** This is an operational floor, not a published guarantee of power. Require at least 20 loss events before making even coarse loss-tail claims. A 95th-percentile penetration estimate with about 20 expected observations beyond it needs approximately **400 reclaimed events**, before allowing for dependence; sparse family/session cells should remain uncalibrated. Profitability and overshoot estimates have different denominators.

**[T] Determine actual required sample from variability and the smallest worthwhile effect.** With the supplied two-outcome approximation, standard deviation is about **8.65 points**. An independent-trade, one-sided 5% test with 80% power needs roughly **463 trades** to distinguish a one-point mean from zero, or **1,850** for half a point. These are illustrations using `(1.645+0.842)^2 × σ² / δ²`; real variable exits, day dependence, heavy tails and model selection can require substantially more. The 200-trade floor is not enough to promise detection of a one-point edge.

**[T] Inference:** resample complete CME days, and use contiguous multi-day blocks as a serial-dependence sensitivity check. Preserve within-day event order and paired policies. Construct simultaneous confidence bounds across every prespecified policy considered, or use a selection-valid nested procedure with a genuinely fresh final confirmation. Do not treat 31 cells as independent or ignore later trials. Simple sign-flipping of individual, highly skewed trade returns does not automatically provide a valid zero-mean null; symmetry/exchangeability requires justification.

**[O] Define the smallest economically worthwhile net effect `δ` and the acceptable dollar/marked-drawdown budget before selection.** Neither the literature nor the old backtest determines those business constraints. A policy can have positive average returns but an unacceptable drawdown or such low activity that it is not useful.

### F. Promotion, no-deployment and abandonment are different conclusions

**[T] Evidence for promotion:** the selected policy has a selection-adjusted lower confidence bound above zero net of primary costs, sufficient days/trades and tail observations, acceptable owner-defined drawdown, and no reliance on optimistic fill-bar ordering. Show sensitivity to higher costs and adjacent parameters; a single fragile maximum is insufficient. Prospective SIM confirmation must use the frozen rule. These are proposed acceptance requirements, not an authorization to deploy.

**[T] Strong statistical kill criterion for the tested family:** after adequate-power evaluation, simultaneous upper confidence bounds for **all preregistered plausible policies** are at or below zero under the relevant conservative execution model. That supports abandoning this defined level-fade family. If all upper bounds are below the owner's positive `δ`, the conclusion is economically inadequate, not necessarily mathematically negative.

**[O] Operational kill criterion:** if no candidate demonstrates positive net expectancy with sufficient evidence and acceptable risk, leave this book disabled/undeployed rather than keep optimizing its geometry indefinitely. This decision does not require proving that every imaginable fade loses.

**[I] Failure to reject zero is not proof of zero or negative expectancy.** A sparse cell with a wide interval is inconclusive. No finite sweep can disprove all future level-fade strategies. The claim must remain bounded to the frozen detectors, level eligibility, entries, sessions, execution model and tested stop/target family.

## THE TRADE, AS PRECISELY AS THE EVIDENCE ALLOWS

**Status: [I]/[T] a codeable research candidate, not a validated replacement.** No external evidence fixes its buffer or proves that it will turn the losing book positive.

**[O] Mandate:** one MNQ, SIM; one position at a time; no scaling, no re-entry, no stop movement in the primary arm; cancel entries and flatten at 14:45 CT. Preserve the existing permitted session policy and monetary risk cap. A missing monetary cap is an unresolved required owner parameter, not unlimited permission.

**[I] Setup:** a predeclared seated support zone, with complete boundaries and provenance available before the limit is submitted. For the primary candidate, retain the existing anchor entry to isolate geometry. Do not use a rejection wick that forms after submission.

**[I] Stop:** below the entry zone's far edge for a long, above it for a short, plus the frozen buffer. This declares exactly what the hypothesis is: penetration through the whole zone beyond the allowed tolerance invalidates the trade. **[T] Initial tick-buffer sweep: 1, 2, 4, 8 ticks; subsequent ATR/width/overshoot alternatives are specified in Section 8. No value is selected by this report.**

**[I] Target:** the near edge of the **first distinct eligible zone in the profit direction**, after collapsing overlapping intervals under the frozen map rule. Do not skip it because its distance is inconvenient. If a later study validates a narrower eligible level set, that set must be declared before looking at the ratio. **[T] Compare zero versus one-tick front-running only as an additional recorded execution experiment; the primary geometry study uses the edge itself.**

**[I] Sequence:** freeze entry, invalidation and eligible target; calculate prices with tick rounding; compute gross and net geometry and dollar exposure; apply the admission policy; submit the linked bracket only if every required condition holds. Keep outcome probability attached to the tested policy, not a generic “level holds 50%” claim.

```text
INPUTS: direction, E, frozen entry_zone, frozen decision_zones,
        buffer_rule/version, tick=0.25, point_value=2,
        round_trip_cost_model, risk_cap_dollars, min_gross_RR,
        session_policy, cutoff=14:45 America/Chicago

REFUSE if session/order/position/data-validity rules fail
REFUSE if buffer parameter, risk cap or structural provenance is absent

LONG:
    B = entry_zone.lower_edge
    S = floor_to_tick(B - buffer)
    O = first distinct eligible zone above entry_zone, in price order
    REFUSE if O is absent
    T = floor_to_tick(O.lower_edge)       # primary: no target offset
    d = E - S
    g = T - E

SHORT:
    B = entry_zone.upper_edge
    S = ceil_to_tick(B + buffer)
    O = first distinct eligible zone below entry_zone, in price order
    REFUSE if O is absent
    T = ceil_to_tick(O.upper_edge)
    d = S - E
    g = E - T

REFUSE if d <= 0 or g <= 0 or target geometry is unresolved
REFUSE if expected net gain at target <= 0
REFUSE if g/d < min_gross_RR              # existing policy: 2.0
REFUSE if modeled one-contract stop loss, including costs,
          exceeds risk_cap_dollars
REFUSE if no validated policy exists for this candidate's execution/regime

Otherwise submit ONE MNQ with linked stop and target.
On refusal: quantity = 0; record exact reason.
Never widen S, tighten S or move T merely to make this gate pass.
```

**[T] This pseudocode defines the candidate's structure, not an estimated noise floor.** The existing 2R policy remains an owner constraint until its separate filter experiment is judged. Changing it requires an explicit policy decision; no generic book rule establishes the correct replacement. The validation condition prevents an untested candidate from becoming production simply because its arithmetic looks attractive.

**[I] For the specific “risk 23 to make 4” case, the answer is refusal under the existing 2R rule.** A smaller quantity cannot fix the point expectancy, and a 120-point target needs an independent structural/continuation justification. The next research question is whether honest structural invalidation and a coherent decision map produce a sufficiently frequent, cost-positive subset—not how to make every marked level tradable.

**[T] Null, minimum n and kill condition:** net policy expectancy at or below zero; a proposed reporting floor of 200 fills/60 days with power and tail requirements often higher; abandon the preregistered family when adequately powered simultaneous upper bounds exclude positive expectancy, or retire it operationally when no candidate earns promotion. No rule here promises that geometry alone will save a fade.

## Sources and evidence provenance

The following numbered source notes supply the full citations and the evidence class. Source dates are publication/version dates where available; undated educational pages are identified as such. The evidence cutoff is 12 September 2026. Numbers from local aggregates are reported as such, not represented as independently rerun trade-level results.

[^1]: **VENDOR-OR-SELF-REPORTED — local primary system research and code.** *Backtest 1: Zone Fade*, 12 September 2026, vl research repository. Frozen source commit `88f32aa90eacf7a6ff9dbd7478b276726c779e4d`; accepted dev base `6b3fddf7030d3a078425af66c9133f3ae009aa0b`. [Report](https://github.com/johnwick2921-cyber/nofx/blob/88f32aa90eacf7a6ff9dbd7478b276726c779e4d/docs/superpowers/research/2026-09-12-backtest-zone-fade/README.md), [C3 detector counts](https://github.com/johnwick2921-cyber/nofx/blob/88f32aa90eacf7a6ff9dbd7478b276726c779e4d/docs/superpowers/research/2026-09-12-backtest-zone-fade/artifacts/c3_baseline.json), [band-touch definition](https://github.com/johnwick2921-cyber/nofx/blob/88f32aa90eacf7a6ff9dbd7478b276726c779e4d/docs/superpowers/research/2026-09-12-backtest-zone-fade/harness/eval.go#L93), [fill offsets](https://github.com/johnwick2921-cyber/nofx/blob/88f32aa90eacf7a6ff9dbd7478b276726c779e4d/docs/superpowers/research/2026-09-12-backtest-zone-fade/harness/main.go#L258), [target selection and exit loop](https://github.com/johnwick2921-cyber/nofx/blob/88f32aa90eacf7a6ff9dbd7478b276726c779e4d/docs/superpowers/research/2026-09-12-backtest-zone-fade/harness/eval.go#L175), [aggregation](https://github.com/johnwick2921-cyber/nofx/blob/88f32aa90eacf7a6ff9dbd7478b276726c779e4d/docs/superpowers/research/2026-09-12-backtest-zone-fade/harness/out.go). Source freshness record: `88f32aa9 2026-09-12 08:35:00 -0500 research(backtest 1): the fade at a zone does not pay — negative net expectancy on 4.4y MNQ under every fill assumption, 0/81 surface cells positive`. Read-only verification references the repository's [AUDIT-CHECKLIST](https://github.com/johnwick2921-cyber/nofx/blob/6b3fddf7030d3a078425af66c9133f3ae009aa0b/docs/superpowers/AUDIT-CHECKLIST.md). No production or database change is part of this report.

[^2]: **VENDOR-OR-SELF-REPORTED — practitioner book.** Laurence A. Connors and Linda Bradford Raschke. *Street Smarts: High Probability Short-Term Trading Strategies*. M. Gordon Publishing, 1996 edition. Chapters 4–5, particularly printed pp. 13–14, 22 and 27–29. [Inspected book text](https://nnty.fun/downloads/books/cdn.preterhuman.net/texts/unsorted2/Stock%20books%20045/Street%20Smarts%20%28Laurence%20Connors%29.pdf). Illustrative 1990s futures/equities, not a disclosed comparative stop-placement sample. A mirrored copy of the authors' text, not a third-party strategy summary.

[^3]: **VENDOR-OR-SELF-REPORTED — trading education.** PATS/Mack. [*How To Use Price Action Structure To Find Proper Stop Placement*](https://priceactiontradingsystem.com/how-to-use-price-action-structure-to-find-proper-stop-placement/). Undated. ES 2,000-tick/5-minute setups; no evaluation dates or audited sample. Signal-bar, buffer, maximum-risk and alternative-entry rules.

[^4]: **VENDOR-OR-SELF-REPORTED — practitioner presentation.** Al Brooks. [*Losing Because of Mistakes*](https://www.brookstradingcourse.com/wp-content/uploads/2016/12/Al-Brooks-futuresIO-Losing-because-of-mistakes-16Dec2016.pdf). futures.io webinar, 16 December 2016, slides 5–8 and 25–28. E-mini illustrations; no systematic sample or stop-comparison experiment.

[^5]: **VENDOR-OR-SELF-REPORTED for rules; MARKETING for unsupported success claims.** John Carter / Simpler Trading. [*John Carter: Trading Plan and Trading Strategies*](https://www.simplertrading.com/join/futures/john-carter). Undated current page, “Pivot Points” and stock/options swing-plan sections. Mixed futures/equity timeframes; sample and validation period unreported. The two plans are not interchangeable.

[^6]: **VENDOR-OR-SELF-REPORTED — exchange education.** CME Group. [*Proper Position Size*](https://www.cmegroup.com/education/courses/trade-and-risk-management/proper-position-size). Undated course. General futures risk instruction; no empirical sample, evaluation period or optimal stop estimate.

[^7]: **PEER-REVIEWED — conference proceedings.** Chia-Hung Wang, Mu-En Wu and Wei-Ho Chung. *Empirical Evaluations on Momentum Effects of Taiwan Index Futures Market*. 2015 Third International Conference on Robot, Vision and Signal Processing, pp. 82–85. DOI [10.1109/RVSP.2015.29](https://doi.org/10.1109/RVSP.2015.29). [Authors' full paper](https://www.researchgate.net/publication/304299040_Empirical_Evaluations_on_Momentum_Effects_of_Taiwan_Index_Futures_Market), Tables 1–4 and Figure 4. TAIEX, 2010–2015; costs explicitly omitted. Do not conflate with later similarly titled publications.

[^8]: **VENDOR-OR-SELF-REPORTED — empirical preprint, abstract-level verification.** Theo Howard. *Stop Distance, Exit Methodology, and Signal Preservation in Intraday Value Area Breakouts: Evidence from E-mini S&P 500 Futures*. Written 5 March 2026; SSRN revision 9 April. [SSRN 6350238](https://papers.ssrn.com/sol3/papers.cfm?abstract_id=6350238), DOI [10.2139/ssrn.6350238](https://doi.org/10.2139/ssrn.6350238); [accessible abstract](https://www.researchgate.net/publication/410962089_Stop_Distance_Exit_Methodology_and_Signal_Preservation_in_Intraday_Value_Area_Breakouts_Evidence_from_E-mini_SP_500_Futures). No independent replication established.

[^9]: **PEER-REVIEWED.** Carol L. Osler. *Currency Orders and Exchange Rate Dynamics: An Explanation for the Predictive Success of Technical Analysis*. Journal of Finance 58(5), 1791–1819, 2003. DOI [10.1111/1540-6261.00588](https://doi.org/10.1111/1540-6261.00588). [Inspected 2001 FRBNY working version, Staff Report 125](https://www.newyorkfed.org/medialibrary/media/research/staff_reports/sr125.pdf). Sample description in this report is version-specific.

[^10]: **PEER-REVIEWED.** Carol L. Osler. *Stop-Loss Orders and Price Cascades in Currency Markets*. Journal of International Money and Finance 24(2), 219–241, 2005. DOI [10.1016/j.jimonfin.2004.12.002](https://doi.org/10.1016/j.jimonfin.2004.12.002). [Inspected FRBNY Staff Report 150, 2002](https://www.newyorkfed.org/medialibrary/media/research/staff_reports/sr150.pdf), especially Table I. Order and quote samples are distinct.

[^11]: **VENDOR-OR-SELF-REPORTED — non-peer-reviewed official regulator research, not vendor education.** Nicholas Fett and Lihong McPhail. [*Stop Orders in Select Futures Markets*](https://www.cftc.gov/sites/default/files/Stoploss_final_ada.pdf). CFTC Office of the Chief Economist Staff Papers and Reports 2017-009, 29 August 2017. Table 2 and sections 4.6–4.7. ES/ZN/CL, 2014–2016; execution volume is not an order count.

[^12]: **COMMUNITY-ANECDOTE.** Huy Nguyen and forum participants. [*Stop 1 Tick Below Get Triggered*](https://www.brookstradingcourse.com/support-forum/general-trading-discussion/stop-1-tick-below-get-triggered/). Brooks Trading Course support forum, February 2022. Individual chart discussion; no audited trade sample. Hosting does not make participants' advice a personally authored Brooks rule.

[^13]: **COMMUNITY-ANECDOTE.** r/FuturesTrading participants. [*People Who Trade Supports and Resistance, How Do You Deal with Days Where None of That Matters?*](https://www.reddit.com/r/FuturesTrading/comments/1jc6acv/people_who_trade_supports_and_resistance_how_do/). March 2025. Unverified NQ/ES observations; no disclosed overshoot calculation, period or sample.

[^14]: **VENDOR-OR-SELF-REPORTED — practitioner account.** James F. Dalton. [*Jim Reveals His Daily Trading Routine*](https://jimdaltontrading.com/routine/). 2018; text dated 24 March. Multi-timeframe futures preparation; no controlled trade sample.

[^15]: **VENDOR-OR-SELF-REPORTED — practitioner preparation.** James F. Dalton. [*What Has Happened Before Greatly Influences the Market Going Forward*](https://jimdaltontrading.com/special-preparation-for-may-6-2019/). Preparation for 6 May 2019. S&P futures auction examples; no audited stop/target comparison. Its probability language is not used here as an empirical estimate.

[^16]: **VENDOR-OR-SELF-REPORTED — practitioner book.** J. Peter Steidlmayer and Steven B. Hawkins. [*Steidlmayer on Markets: Trading with Market Profile*, second edition](https://uat.store.wiley.com/en-us/steidlmayer-on-markets-trading-with-market-profile-2nd-edition-p-9780471420989). Wiley, 2003; ISBN 9780471420989. Publisher description and scope verified; not cited as a fully inspected numerical stop rule or empirical trial.

[^17]: **VENDOR-OR-SELF-REPORTED — public quant repository.** Wojtek0110. [*Opening Range Breakout Backtest on S&P 500 E-mini Futures*](https://github.com/Wojtek0110/opening_range_breakout_backtest). Undated repository, inspected September 2026. README sections “Experiments,” “Limitations” and “Possible next steps.” ES minute-bar case study; historical sample size/dates absent from inspected README; costs and walk-forward validation not established.

[^18]: **VENDOR-OR-SELF-REPORTED — funded-trader education.** Topstep. [*The Risk–Reward Framework Profitable Traders Use*](https://www.topstep.com/blog/larger-gains-and-smaller-losses-whats-the-secret). 7 April 2026, target/stop sections. No empirical refusal denominator or systematic futures result.

[^19]: **VENDOR-OR-SELF-REPORTED — funded-trader education.** John Doherty, currently displayed byline. [*The Trader's Equation: Should I Take This Trade?*](https://www.topstep.com/blog/the-traders-equation-should-i-take-this-trade). Topstep, 20 March 2026. ES teaching examples, not a disclosed empirical probability study.

[^20]: **PEER-REVIEWED — mathematical model, not backtest.** Tim Leung and Xin Li. *Optimal Mean Reversion Trading with Transaction Costs and Stop-Loss Exit*. International Journal of Theoretical and Applied Finance 18(3), 2015. [arXiv:1411.5062v3](https://arxiv.org/abs/1411.5062). Ornstein–Uhlenbeck spread and optimal stopping; no MNQ instrument-period/sample.

[^21]: **VENDOR-OR-SELF-REPORTED — official exchange contract facts.** CME Group. [*Micro E-mini Equity Index Futures: Frequently Asked Questions*](https://www.cmegroup.com/articles/faqs/micro-e-mini-equity-index-futures-frequently-asked-questions.html). Current contract-information page, inspected September 2026. MNQ outright tick/value; sample and backtest period not applicable.

[^22]: **VENDOR-OR-SELF-REPORTED — desk teaching account.** Mike Bellafiore. [*How Do You Stay in That Position?*](https://www.smbtraining.com/blog/how-do-you-stay-in-that-position). SMB Training, 7 January 2013. EDU intraday stock example and *PlayBook* teaching context; no controlled exit-comparison dataset.

[^23]: **VENDOR-OR-SELF-REPORTED — empirical preprint, abstract-level verification.** Purvang Gandhi. *Entry-Timing Confirmation via Maximum Adverse Excursion: An Empirical Test on a Systematic Index Futures Strategy*. Written 6 July 2026, revised 23 July. [SSRN 7063478](https://papers.ssrn.com/sol3/Delivery.cfm/7063478.pdf?abstractid=7063478&mirid=1), DOI [10.2139/ssrn.7063478](https://doi.org/10.2139/ssrn.7063478). Proprietary Nifty strategy; author financial interest disclosed.

[^24]: **VENDOR-OR-SELF-REPORTED — local primary backtest and code.** *Backtest 1B — Structure Stop + Next-Real-Level Target: Measure First*. Source commit `6f3f7f185c0407519bfd2125b6d7eb1ba0bfe067`, merged on dev at `35587311`; incorporated after refreshing this report's base. [Report](https://github.com/johnwick2921-cyber/nofx/blob/6f3f7f185c0407519bfd2125b6d7eb1ba0bfe067/docs/superpowers/research/2026-09-12-backtest-structure-fade/README.md), [all-era and era CSV](https://github.com/johnwick2921-cyber/nofx/blob/6f3f7f185c0407519bfd2125b6d7eb1ba0bfe067/docs/superpowers/research/2026-09-12-backtest-structure-fade/artifacts/geometry_cells.csv), [fill implementation](https://github.com/johnwick2921-cyber/nofx/blob/6f3f7f185c0407519bfd2125b6d7eb1ba0bfe067/docs/superpowers/research/2026-09-12-backtest-structure-fade/harness/main.go#L242), [exit loop](https://github.com/johnwick2921-cyber/nofx/blob/6f3f7f185c0407519bfd2125b6d7eb1ba0bfe067/docs/superpowers/research/2026-09-12-backtest-structure-fade/harness/eval.go#L221), [qualified-target function](https://github.com/johnwick2921-cyber/nofx/blob/6f3f7f185c0407519bfd2125b6d7eb1ba0bfe067/docs/superpowers/research/2026-09-12-backtest-structure-fade/harness/eval.go#L294). Freshness record: `6f3f7f18 2026-09-12 11:14:09 -0500 research(backtest 1B): structure stop + next-real-level target — MEASURE FIRST: 16/16 geometry cells net-positive on fill (a), negative on fill (b)`. Same instrument/period as Source 1; full session bars and cohort identity are claims of the underlying report, not a new tape audit here.
