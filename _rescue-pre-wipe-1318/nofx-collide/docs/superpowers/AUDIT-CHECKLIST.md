# AUDIT-CHECKLIST — the permanent audit playbook

Codified 2026-08-28 from the campaign's then-18 proven bug classes; 53 as of
2026-09-03. **Every audit
dispatch MUST reference this file instead of re-deriving the probe list. Every
NEW bug class found gets appended here in the SAME PR that fixes it** (canon law
in CLAUDE.md).

---

## PART 1 — THE BUG CLASSES (name · root cause · probe · law)

*Highest occupied class: **108** (2026-09-10). Numbers are assigned AT MERGE and
never renumbered; a gap means a wave took a later slot to avoid a collision.*

1. **Self-imposed caps.** Root cause: an AI/HTTP/token cap chosen without
   measuring the provider ceiling or the observed need (the 32768-token
   truncation ceiling; provider ceiling 393216 probed 2026-08-19).
   **Probe:** `grep finish_reason=length` in the journal + compare every cap
   against the probed provider ceiling. **Law:** never self-cap below observed
   need; a cap change ships with the measured ceiling in the commit.

2. **Schema-gate vs validator.** Root cause: the per-attempt validator
   rejections in the 3-attempt loop logged NOTHING (silent `continue`) — the
   gate existed but forensics were blind (P0 side-quota lesson).
   **Probe:** a fixture AT THE GATE BOUNDARY — the validator and its golden test
   share one pure function ("the test IS the write path", T2 stamping).
   **Law:** every gate ships a gate-level fixture in the same PR.

3. **Connection starvation.** Root cause: NT8 TCP link gaps (01:25, 15:22-15:26
   CT) + the 60s ack timeout that closes the conn on a stalled heartbeat.
   **Probe:** the watchdog — `wire_liveness` lines (last_frame_age,
   frames_per_min) + dead-man reconnect verified from the far side.
   **Law:** a stalled heartbeat is a bug until the reconnect cycle is proven
   (e2e dead-man check).

4. **sync.Once / LoadOrStore wrong-owner.** Root cause: `nt.OrderUpdates()`
   evaluated as the LoadOrStore ARGUMENT every cycle → the map held a closed
   channel → 310,808 zero-value drains in 15s.
   **Probe:** grep LoadOrStore call sites — the key argument must be a VALUE,
   never a function call; channels subscribe on miss only + self-heal on close.
   **Law:** never call a function inside LoadOrStore.

5. **Deaf consumer / far-side frames.** Root cause: the C# AddOn filtered
   order_update to Filled/Rejected only, or ran a PRE-shipped binary (md5
   mismatch) — the Go consumer listened to frames the far side never emitted.
   **Probe:** byte-compare the deployed AddOn (md5 vs repo) + capture a RAW far
   -side frame before blaming the consumer. **Law:** prove the far side emits
   before you debug the listener.

6. **Go-side theater.** Root cause: a confident Go log line ("⚔️ armed S1 …")
   for a placement path that was not actually shipped (Phase 2 not live) — the
   first E2 was GO-OPTIMISTIC. **Probe:** demand a FAR-SIDE frame quote
   (dispatcher log / NT8 order state) for every wire claim.
   **Law:** a Go log line is not a wire frame; theater dies in the frame quote.

7. **Timestamp convention.** Root cause: close-stamped frames persisted raw →
   every replayed row landed at T+1m (the 2499/2500 mismatch class).
   **Probe:** `open_time_ms % 60000 == 0` on the whole table + fill-containment
   (own fills must sit inside `[L,H]` of the floor-minute bar; 0 may fit the
   T+1m bar). **Law:** ONE canonical open-stamp conversion shared by every
   reader; the residue check alone cannot catch T+1m (both divide 60000).

8. **Clamp-vs-knob.** Root cause: FE persists values above the engine ceiling
   (leverage 20 vs system 10) — saved but inert; proximity clamp 0.1-3.0.
   **Probe:** ONE shared resolver (file:line) read by UI + gate + prompt; the
   card renders the RESOLVED value. **Law:** a knob without a shared resolver
   is decoration.

9. **is_active-vs-binding.** Root cause: audits read `strategies.is_active=1
   LIMIT 1` and got a strategy NO trader binds (the false-strict-alarm: read
   4104ca0a advisory while the bound a5b7662e was strict×3).
   **Probe:** resolve strategy via `traders.strategy_id` (the TRADER BINDING),
   never is_active. **Law:** audits/sweeps query by binding; `is_active` is a
   legacy flag.

10. **Missing wire identity on materialized positions.** Root cause: the
    2026-08-25 reconcile incident — an NT8-held position with no DB row
    (untracked) materialized without full identity; sub-60s round-trips stayed
    invisible. **Probe:** `SELECT ... WHERE source='reconcile' AND
    (account='' OR entry_price<=0)` + the materialization regression test.
    **Law:** every materialized row carries source + account + entry; priced
    closes are consumed immediately (TestReconcileMaterializesUntrackedNT8Position).

11. **Enum spelling drift.** Root cause: the model wrote flip.rule "2x5m_close"
    vs the canonical "2x5m" → 2 silent rejections (armed wave 09:47/09:53).
    **Probe:** journal grep for the REJECTED spellings + a canonicalization
    function shared by parser and validator. **Law:** enum parsing normalizes
    known spellings and logs the unknown ones by name.

12. **Log-flood retention.** Root cause: per-frame INFO floods — bar_update
    (7.5M lines/day), backpressure WARN, order_update (1.48GB in ONE hour,
    25k lines/s) — each ate the journald 2G cap (retention < 1 day).
    **Probe:** after ANY logging change, re-project: measure bytes/hour of the
    busiest hour vs the 2G cap; target ≥7 days; per-frame logs are DEBUG +
    sampled (1/500) + a 1-line/min INFO summary. **Law:** every logging change
    ships with a retention re-projection.

13. **Concurrent-terminal.** Root cause: two dispatches in the main tree — one
    reset the other's uncommitted work out from under it (armed-orders vs
    level-truth; 6 dirty worktrees lost). **Probe:** porcelain gate
    (`git status --porcelain` empty) + `deploy/nofx-lock.sh acquire <session>
    <task>` before ANY main-tree work (atomic; a second acquire REFUSES — see
    class 70, which removed the pid this line used to name). **Law:** WORKTREE LAW — the
    main checkout belongs to exactly ONE dispatch; secondary work runs in
    `git worktree add ../nofx-<task>` + `git worktree lock`.

14. **Unattended deploys.** Root cause: timers/schedules performing cutovers
    (0-for-2 history — both failed). **Probe:** grep crontab/systemd timers for
    deploy commands — none may exist. **Law:** NO-UNATTENDED-DEPLOYS — owner
    ack within minutes OR a TESTED auto-rollback; timers are banned outright.

15. **Fantasy-R.** Root cause: planned R:R > ~6 on an arm — mathematically a
    fantasy target; the arm is refused every cycle and learns nothing.
    **Probe:** grep plans for `R:R = |target−entry| / |stop−entry|` > 6 + the
    arm feasibility contract (R:R ≥ 2.0 AND stop ≥ 1.0×ATR5m or the arm is
    refused). **Law:** the contract says what the gate enforces; fantasy
    targets get WARN-flagged at write.

16. **Small-n crowns.** Root cause: verdicts like "reject 75% win +665" quoted
    without n (a 3-trade week crowned as a rule). **Probe:** every verdict in a
    report MUST carry its n (e.g. "A-touch react 79% (n=52)"); n < 10 is
    explicitly labeled anecdote. **Law:** every verdict carries n; no crowns
    on small n.

17. **Secrets in baks.** Root cause: `.env.bak.*` and other backup files left
    in the tree with live keys; they also flip the binary's `+dirty` vcs flag.
    **Probe:** the dirty-flag account in every cutover report lists each
    untracked file by name + a secret-scan on every `.env*` and `*.bak`.
    **Law:** baks live OUTSIDE the repo (or are gitignored + scanned); the
    boot `+dirty` must be exactly the accounted list.

18. **Canon self-compliance.** Root cause: a wave changes a knob/play/gate but
    the guide still describes the old behavior — a guide that lies about the
    running binary is worse than no guide. **Probe:** in the SAME PR as any
    knob/play/chip/gate change: update `web/src/guide/content/*` + bump
    `GUIDE_BUILT_REV` to the shipped rev; the drift banner is a failsafe, not a
    maintenance strategy. **Law:** GUIDE CONTENT LAW.

19. **Half-shipped guard.** Root cause: a wave declares the guard's state
    (atomics, knob resolver, comments) but never wires the call sites — the
    pre-reopen F2 persist watchdog shipped `persistLastFlushAt`/
    `persistAlarmAt` + `persistWatchdogSeconds()` with ZERO `.Store`/`.Load`
    usages, so the alarm could never fire (caught 2026-08-29 by the
    pre-live-fire sweep). **Probe:** grep every guard atomic/knob for
    `.Store`/`.Load` call sites AND ship a BEHAVIOR fixture that makes the
    alarm FIRE (simulated stall → the ERROR line fires exactly once, quoted;

20. **OS-side fix that silently regresses.** Root cause: an OS-level remediation
    installs "successfully" but is handcuffed by a wrapper — chrony on WSL2 was
    started by chronyd-starter.sh which detected a container + missing
    CAP_SYS_TIME and appended `-x` ("Disabled control of system clock"), so
    `makestep 1 -1` never stepped, and the cron fallback called a binary that
    does not exist in the rootfs (`hwclock`). Result: 0.12s at 09:xx → −41s at
    17:01, NTPSynchronized=no. **Probe:** after ANY host-clock remediation,
    verify the fix's own mechanism actually fired (chronyc tracking shows a
    step-capable daemon; `journalctl -u chrony` free of "Disabled control of
    system clock") AND ship a machine-side escalation: at tolerance breach
    defer NEW plan authoring (negative drift = feed-in-future = provably
    broken) + widen T1 news windows by the measured drift. **Law:** the bot
    must never trade on a clock it knows is broken.
    resumed flush → stamp advances, no repeat) — existence tests cannot catch
    this class. **Law:** a guard without a firing fixture is decoration.

21. **Committed binaries / embedded secrets.** Root cause: `git add` of build
    artifacts — 14 tracked `nofx-bin.old*` binaries embedded a live-era
    DeepSeek `sk-` key in a PUBLIC repo (caught 2026-08-29 by T14's binary
    scan; every text-only secret scan missed it). **Probe:** `git ls-files`
    for binary artifacts + `strings`-scan EVERY tracked binary for `sk-`/
    key patterns; confirm any embedded key's hash ≠ every live key's hash
    (leak inert). **Law:** binaries are NEVER tracked; `.gitignore` covers
    every binary glob; a history rewrite requires the owner's explicit
    force-push ack and every clone/partner repo must re-clone.

22. **Log-lie counters.** Root cause: a success log reports a WRITE DELTA or
    a derived counter instead of the thing it claims — `maybeFetchCalendar`
    logged "fetched 0 events" on every healthy fetch because frozen
    `forexfactory` slices return `wrote=false` (`store/calendar.go:92-93`)
    and the line counted stored rows, not fetched events (23 false zeros
    08-27→08-29, caught by the 2026-08-29 news-feed forensics). A lying
    success line is worse than silence: it masks healthy state as broken
    and trains operators to ignore the signal. **Probe:** every "fetched N"
    / "processed N" log line must count the OBJECT of the verb, with write
    deltas logged separately; grep log lines against their counter
    definitions. **Law:** counters log what they name. Also: independent
    prompt re-renders (audit re-implementations) MUST enumerate EVERY input
    source — T9's renderer omitted `calendar_slices` and printed a false
    "(no filtered events)" for a populated prompt (same forensics).

23. **Unprobed supply chain.** Root cause: dependency audits were never
    automated — the F1 scan (2026-08-29) found 8 reachable Go
    vulnerabilities (x/text, quic-go ×2, pgx, go-ethereum ×3, jwt/v5) and
    14 npm findings incl. lodash + react-router HIGHs, all fixable by
    patch/minor, none ever bumped. **Probe:** `govulncheck ./...`
    (symbol-level reachability) + `npm audit --omit=dev` on EVERY wave;
    weekly CI + Dependabot (security-only) keep it that way.
    **Law:** CRITICAL/HIGH fixable by patch/minor bump in the SAME wave as
    the finding; major-version upgrades are owner-ruled, never auto-merged
    before a live-fire window.

24. **Report-only path panicked the trading loop.** Root cause
    (2026-08-30 entry-mechanics cutover): the E8 shadow A/B logger computed
    the counterfactual fill bar as `bucket_index × 5` — WRONG when the plan
    window starts mid-5m-bucket or spans <5 bars, so `w[5]` on a 4-bar
    window panicked `maybeManageArmedOrders` → `runCycle` → the whole bot,
    2 min after a clean boot. The shadow logger had zero gates downstream,
    yet one bad index took trading down. **Probe:** every advisory/report
    path MUST be panic-hardened — (a) boundary-safe index math proven by a
    fixture that reproduces the exact window shape (crossing a 5m boundary
    with 4 bars), (b) `recover()` at the call-site seam with a WARN, never
    a silent swallow. **Law:** a report-only path may degrade to a warning,
    never to a panic; the trading loop owns no `recover` blanket — each
    advisory seam carries its own.

25. **Armed re-place loop (manual cancel did NOT win).** Root cause:
    `UpsertArm` re-authorized TERMINAL rows every cycle while the confirm
    stayed MET and the placement band allowed the wrong side — the
    2026-08-30 S2 loop: terminal → armed → marketable fill (limit above
    market) → instant stop-out → terminal → armed… 8 generations in 26 min;
    an owner/NT8 cancel never stuck. **Probe:** (a) same-version
    re-authorization fixture (terminal row + UpsertArm same version → stays
    terminal; version bump → re-authorizes), (b) the wrong-side predicate
    `limitMarketableWrongSide` (long limit below market / short above =
    marketable, never placed), (c) journal has zero `WORKING` lines for a
    terminal row. **Law:** MANUAL-CANCEL-WINS — a terminal row is
    re-authorized ONLY on a plan-version change; a limit whose level the
    price already accepted through is cancelled, never placed.

26. **Far-side capability mismatch.** Root cause: the Go side sent a
    `stop_entry` frame to a pre-E7 AddOn, which executed the UNKNOWN frame
    type as MARKET — the 2026-08-30 test filled at 29346.25 instead of
    resting at 28700 (the far-side proof exists precisely to catch this).
    **Probe:** the AddOn reports a `build_id` on every heartbeat; the Go
    side refuses any frame type the far side hasn't proven
    (`FarSideProven` vs `FarSideBuildE7`) + a loopback fixture proving BOTH
    the refusal (no/old build) and the release (new build). **Law:** NO
    additive wire frame is sent before the far-side build proves it; a
    capability gate ships with its own negative fixture.

27. **The netting orphan: a broker that nets, a ledger that does not.** Root
    cause: NT8 nets positions per account, so a second entry in the same
    direction produced ONE broker position while our ledger held two rows —
    the second row had no broker counterpart and could never close. **Probe:**
    after any entry path change, open two same-direction positions on one
    account in SIM and diff the ledger row count against the NT8 snapshot; a
    close that leaves a row OPEN with no broker position is the signature.
    **Fix:** shipped a0c7ff0b (report 2026-08-31-netting-orphan-wave.md).
    **Law:** the ledger's model of the broker is a claim to be verified against
    the broker's own snapshot, never assumed from our own writes.

28. **Canonical casing.** Root cause: the same identifier written two ways
    (symbol, account, session) compares unequal and silently splits a lookup
    into a miss. **Probe:** for every map key and every WHERE clause built from
    a string, ask where the string was normalized and whether BOTH sides use
    the same normalizer. **Law:** one canonicalizer per identifier, called at
    the boundary where the value enters, not at each comparison.

29. **The silent-aggregate family.** Root cause: aggregates that answer
    confidently from data they should have excluded or never had — an exit
    price equal to the entry price yielding a clean 0.00 P&L, test-seam rows
    summed into production totals, a rate printed without its n. Each looks
    healthy precisely because it is wrong. **Probe:** for every aggregate, ask
    what a MISSING or SYNTHETIC row does to it — if the answer is "nothing
    visible", the aggregate is lying; require the exclusion count beside every
    sum. **Law:** an aggregate reports what it excluded, or it is not evidence.

30. **GORM alias scan silently reads 0.** Root cause: a `Scan` target struct
    field whose default GORM snake-casing disagrees with the SELECT alias —
    `TotalPnL` maps to `total_pn_l` (misses `total_pnl`), `NetPnL` maps to
    `net_pn_l` (misses `net_pnl`); the field silently scans zero while the
    query succeeds (found live 0A-2 in `GetPositionStats` total_pnl, again
    0C in `ab_confirm_log.net_pnl`). **Probe:** grep `Scan(&` targets for
    PnL-shaped fields without explicit `gorm:"column:…"` tags; ALWAYS assert
    a NONZERO expectation in the fixture (a zero assert passes the broken
    scan). **Law:** every Scan target ships an explicit column tag + a
    fixture whose expected value is nonzero.

31. **Inert-but-visible ledger state.** Root cause: a new non-terminal state
    name is invisible to `ListNonTerminal`'s `state IN ('armed','working')`
    filter, so assertion and cleanup paths silently skip it (0C's "shadowed"
    rows). **Probe:** when a new state ships, quote every `state IN` /
    `state =` filter site and add a fixture reading the new state through the
    SAME query the runtime uses. **Law:** a state is only "inert" if the
    queries that must skip it and the queries that must see it are BOTH
    fixture-pinned in the same PR.

32. **Time-scheduled action riding a data-gated cycle.** Root cause: the
    session read (registry 16:30/01:30/08:00) was invoked INSIDE `runCycle`,
    which `tickOnce` never enters when the bar-close gate or the no-new-data
    dedup idles the tick. 2026-08-31 evidence: CME is halted 16:00-17:00 →
    every cycle 16:26→16:38 logged `cycle_skip=no_new_data` → the 16:30 ASIA
    read fired at ~17:00:03 with the reopen tick — 30 minutes late, no error,
    no alarm, no plan at the open. Registry resolution, Sunday-defer (count=0)
    and trader liveness were all correct. **Probe:** for every time-scheduled
    action (reads, flats, rolls, weekly, digests), quote its invocation site
    and ask "does anything the market's calendar does delay this?" — a halted
    or quiet tape must never move wall-clock work. **Law:** scheduled work is
    evaluated on wall-clock BEFORE the data-gated skips; data gates may skip
    DATA work only. Halt-fired reads author from last stored bars and log
    `🗓 session read fired during halt … (newest <tf> <ts>, age <n>m)`.

33. **A cutover rite that checks exposure but not in-flight work, trusts a
    gate that cannot fail, and leaves the previous process's orders alive.**
    Three measured defects, one class. **(a) No in-flight leg.** PART 3
    steps 1-7 checked positions and orders, never running work: 2026-08-31
    17:34 CT a `kill -9` landed while a planner chain was on attempt 3/3 —
    the chain died silently, no v2, no fail-closed line, nothing re-claimed
    it. Four later cutovers held on this by agent discipline alone.
    **(b) Leg 4 could not fail.** `TCPTrader.GetOpenOrders` was
    `return []types.OpenOrder{}, nil` (tcp_trader.go:1149): the open-orders
    leg passed VACUOUSLY at cutovers 35, 36, 37, 38, 39, 40 and 41, and the
    full-system audit quoted "→ []" as evidence of flatness. NT8 emits no
    working-order frame (audit F12), so the `armed_orders` ledger is the only
    real source — it is what actually held the 09-01 swap (arm 29 WORKING).
    **(c) Pre-boot arms were orphaned.** 2026-09-02 00:16 CT a cutover ran on
    "just go" with S1 @29044 and S3 @29068.05 resting: the old process died,
    its broker orders did not, and they sat with NO listener for 15 minutes
    until the stale-window reconcile cancelled them at 00:31:48 — while the
    new binary re-armed its own S1/S3 and opened position 587 at 00:17:44,
    so for minutes TWO S3 orders existed at the broker. A fill on the dead
    process's order would have been a position no stop was attached to
    (class 27 again). It resolved by luck. **Probe:** for every gate leg, ask
    what input would make it FAIL — a leg with no such input is not a leg.
    For every restart, ask what the dying process leaves running at the
    broker. **Fix:** leg 4 reads the ledger (`AutoTrader.ledgerOpenOrders`,
    wired into `TCPTrader.SetOpenOrdersSource` at construction) and every row
    carries `source: "ledger (no NT8 order frame — F12 open)"`; an unwired or
    erroring source FAILS the leg instead of answering empty. Leg 5 is
    in-flight planner work (`AnyPlannerReadInFlight`, any date/session).
    `GET /api/cutover-gate` returns all five legs in ONE payload so an agent
    cannot quote four and skip the fifth; a leg that cannot be evaluated
    fails. The boot sweep (`sweepPreBootArms`, at the HEAD of
    `maybeManageArmedOrders`, before any authoring or placement) cancels every
    non-terminal row stamped with a different `boot_id` — cancel frame first,
    then `state=cancelled` with reason `boot_sweep: pre-boot order, process
    restarted`; a FAILED cancel leaves the row non-terminal and does not latch
    (never hide a live order behind a clean ledger); an authorized-but-never-
    placed row is left alone (nothing exists at the broker). Counter
    `arms_boot_swept_class33` in system_config. Boot line `🛡 cutover safety
    (class 33)`. **Law:** a gate that cannot fail is not a gate; a cutover
    checks running work as well as exposure; and no process may leave orders
    alive at the broker for a successor that never placed them.

34. **Validator hint naming a nonexistent condition.** Root cause: the
    breakdown-void reject said "author a reject/retest play instead" — the
    model authored condition `reject_retest`, and parse/schema rejected it:
    the model complied with the hint and was punished for it. 2026-08-31
    evidence: identical in BOTH ASIA chains, both fail-closed (v1 no_trade +
    the in-flight reset chain killed by the 0C cutover). Compounded by 0C:
    `breakout_retest` is shadowed, so the old hint steered toward either a
    nonexistent or a demoted condition. **Probe:** grep every validator
    message / repair-law excerpt for condition-shaped tokens; each must be in
    the enum AND resolve live. The registry + table test IS the guard
    (`kernel.ValidatorHints()` + `ValidateValidatorHints()`), re-run at boot,
    and the planner reject block now appends `Valid conditions: [<resolved
    live list>]`. **Law:** a hint is an instruction — instructions must be
    checkable; never name a composite or shadowed token as an authoring
    target.

35. **Counter inferred from row count (replan budget arithmetic).** Root
    cause: `ReplansUsedFrom = version − baseline` counted EVERY appended plan
    row as a spent re-plan, trigger-agnostic. 2026-09-01 LONDON: chain
    [planner_fail_closed, level_event, dormant:flip, level_event ×3] — zero
    death re-plans, zero owner re-reads — read as 5 of 4 spent, replans_left
    0; the next scenario death would have fail-closed a budget never touched.
    Compounded: death re-plan rows landed as `<S>_scheduled_read` (no class
    label), `trigger_reason` is overwritten in place by dormant/rearm
    transitions, and the card carried a THIRD formula
    (`noTradeVersion−2 : version−1`). Fourth silent-counter defect in one
    week: replan budget (this class) · guardrail ENTRIES count includes
    test-seam rows (open) · P&L summed realized_pnl not pnl_corrected (fixed)
    · GORM alias scan returned a plausible zero (class 30, fixed). **Probe:**
    for every cap/budget/quota, find the increment site — if the "used" value
    is derived (rows, versions, ids, timestamps) rather than written by the
    consuming path, it is inferred; fixture the live chain shape and assert
    the resolved value at the gate (`TestClass35PinTodayChain`). **Fix:**
    `store.GetReplanBudget` / `SpendReplan` — a recorded counter in
    system_config keyed `dayplan_replans_used:<trader>:<date>:<session>:b<baseline>`,
    incremented when a `death_replan` / `owner_reread` row lands; the card
    reads `replans_left` from the API. **Law:** counters record events; they
    do not infer them.

36. **Scheduled work inheriting the market's calendar — layer 2, the
    preflight (sibling of class 32).** Root cause: `plannerPreflight`
    (trader/auto_trader_feedwatch.go) compared the newest 1m bar's age to
    FEED_ALERT_S (600s) for EVERY planner trigger class. Class 32 fixed the
    TRIGGER (wall-clock evaluation), but the read then met a freshness check
    that is UNSATISFIABLE inside the 16:00–17:00 halt or on a weekend by
    definition. 2026-09-01: the 16:30 ASIA trigger fired; fifteen
    `stale_bars_1865s … 3545s` refusals 16:31→16:59; the read launched
    17:01:05 on the reopen tick, three attempts died on the same
    breakdown_continue-void class, fail-closed 17:23:14 (ASIA v1
    planner_fail_closed) — the halt refusal ate the 31 minutes that would
    have absorbed a retry BEFORE the open. The Sunday weekly read had the
    same shape (31 minutes late 2026-08-30) and ALSO still lived inside
    runCycle behind the data gate. Two contracts contradicted: "author from
    last stored bars" (class 32) vs the preflight's freshness requirement.
    **Probe:** for every scheduled action, walk the WHOLE path after the
    trigger — every gate it crosses must be satisfiable at the scheduled
    time by construction (a check that needs a live tape can never pass
    during a halt). **Fix:** the freshness check is SCOPED by trigger class
    (`preflightScheduledBypass`): scheduled session reads + the weekly
    bypass it only while `!IsCMEOpen(now)`; death_replan / owner_reread /
    level_event / structure_mss keep it; a scheduled read into a silent OPEN
    tape still refuses (the 08-19 outage class). The weekly read moved onto
    the wall-clock evaluator (`evaluateWallClockWeeklyRead`, before the
    session reads; `sundayAsiaDeferred` unchanged). Both outcomes are loud:
    `🗓 preflight bypass (class 36) …` (WARN) and `⛔ planner preflight refused
    <class>: <reason>` (ERROR). Executor halt block untouched
    (`cmeSessionClosedSkip` / `IsCMEOpen`). **Law:** never trading during a
    halt is the executor's rule; never AUTHORING during a halt defeats the
    open−30 design — a preflight may not refuse scheduled work because the
    market is closed when the work exists to run while the market is closed.

37. **Whole-request ceiling on a LIVE stream (split deadlines that did not
    split).** Root cause:
    the planner-speed wave (2026-08-31) moved the planner onto SSE with a 30s
    idle watchdog and DOCUMENTED "the whole-request ceiling stays
    http.Client.Timeout (600s) — a live-but-slow stream is never killed"
    (`kernel/planner_speed.go`, `mcp/client.go`, guide settings.ts). Both
    halves cannot be true: `http.Client.Timeout` bounds the body read, so every
    max-reasoning attempt still streaming at 600.0s died with
    `stream interrupted: context deadline exceeded (Client.Timeout …)`.
    2026-08-30 17:00 → 09-01 17:30 CT: 11 of 80 max full/re-author attempts
    (13.8%) killed at 600000-600001 ms with 71k-140k reasoning chars already
    received (ttfb 474-578 ms — the provider had answered); 0 of 22 repair and
    0 of 42 fast-reasoning attempts. Successful max reads p50 448s · p95 581s ·
    max 599.5s (right-censored). 2 of 9 fail-closed sessions had a kill consume
    an attempt (08-30 ASIA v3, 08-31 NY v2); 3 wake re-plans landed 15-30 min
    late. Compounded: the kill's transport text was fed to attempt 2 as a
    "validator reason" (planner_rejected_prompts 71-72), the `ai_call` line
    carried no failure class, and a failed call inherited the previous call's
    ttfb/reasoning numbers — so the owner saw "the API keeps failing".
    **Probe:** for every deadline claim in a comment/guide, find the
    transport-level timeout that still applies (`http.Client.Timeout`,
    `ResponseHeaderTimeout`, dialer) and fixture the live-but-slow case
    ACROSS that timeout (the 4.4 fixture used `Timeout: 10s` against a 0.5s
    stream — it never crossed the ceiling); grep `ai_call … ok=false` and
    demand a `class=` token on every line. **Fix:** planner stream rides
    `CallWithRequestStreamRetryDeadlines(idle, total)` — total =
    `AI_PLAN_TOTAL_DEADLINE_SECS` (default 1200, from the distribution) on a
    per-call http.Client copy with Timeout=0; the 600s ceiling stays on
    non-stream paths; `classifyAIError` + `context.Cause` stamp
    `class=total_deadline|idle_deadline|client_timeout|transport|http_status`,
    `http_status=`, `request_id=` on every ai_call failure; per-call telemetry
    is reset at call start; the planner logs `provider_row=` on failure; boot
    lines print idle/total/ceiling/retries/row. **Law:** a deadline a design
    says "never fires" is asserted by a fixture that crosses it, and every
    failed provider call carries a failure class — "the API keeps failing" is
    not a log line.

38. **Prompt/validator contract mismatch — the prompt offers what the
    validator refuses.** Root cause: the authoring contract lives in TWO
    places that drift. 2026-09-01 ASIA, ONE read, three attempts, three
    DIFFERENT rejects (`planner_rejected_prompts` 78/79/80): (a) the entry-law
    `Style` string — quoted VERBATIM into the rejection the model reads — said
    "2x5m legal ONLY here" while `confirm.rule`'s enum is
    touch|1x5m_close|2x5m_close|1m_mss|time_hold; "2x5m" belongs to the
    SEPARATE death/flip enum; (b) the model copied that token into
    confirm2.rule and was rejected for complying, and its repair came back
    unparseable; (c) the schema line offered `"legs":[…]` on EVERY scenario
    with no condition qualifier while its siblings fvg{}/breakdown{} on the
    same line DO carry "REQUIRED iff …", and the sweep_reclaim-only rule lived
    ONLY in plan_doc.go. Rendered-prompt counts before the fix: "arm_legs" 0 ·
    "split entry" 0 · "arm single" 0 · "EXACTLY 2 legs" 0 · "split contract" 0.
    Scale: 35 of 121 validator rejects in 72h were legs on a non-sweep
    condition (breakdown_continue 24, reject 11); 7 landed on attempt 3/3 and
    two sessions fail-closed on it. The class-34 guard stayed green throughout
    because it checked CONDITION tokens only. **Probe:** for every validator
    branch keyed by condition/field, grep the RENDERED prompt for the sentence
    that states it — absent = the defect; and scan every hint string for
    enum-valued tokens, checking each against the enum of ITS OWN field (the
    same spelling can be legal in one field and illegal in another).
    **Fix:** `kernel/prompt_contract.go` — a registry of the 17 condition-keyed
    restrictions with the sentence each requires, `ValidatePromptContracts`
    failing the build (table test) and shouting at boot
    (`📜 prompt/validator contract: N restrictions, all stated in prompt`);
    `ValidateValidatorHints` extended with `HintRuleField` so every rule token
    is checked against its own field enum, and the entry-law `Style` strings
    registered as hints; one token vocabulary everywhere, with the death/flip
    enum declared beside its own schema line. **Law:** the prompt states every
    restriction its validator enforces — a rule the author cannot read is a
    trap, and a hint is an instruction, so it must be legal in the field it
    describes.

39. **Reject-where-normalize-was-deterministic (legs on a non-sweep
    condition).** Root cause: the arm contract says every non-sweep_reclaim
    condition arms SINGLE, and the validator REJECTED any `legs[]` on such a
    scenario (`arm_legs_sweep_reclaim_only`, plan_doc.go ArmSpecValid) —
    burning one of three attempts (~450 s of max-reasoning each) on a shape
    whose correct form was already fully determined: the top-level arm. 72 h to
    2026-09-01: 35 of 121 validator rejects (breakdown_continue 24, reject 11);
    7 landed on attempt 3/3; two sessions fail-closed directly on it
    (`planner_rejected_prompts` rows 69, 80). The only retained instance (row
    69 S1, breakdown_continue, ONE leg, rule=touch) already carried a valid
    top-level arm that mirrored the leg — dropping the array alone made it
    pass; the two sweep_reclaim one-leg instances (rows 69 S2, 85 S1) must
    keep rejecting because there the fix would be authoring. **Probe:** for
    every validator reject, ask whether the correct shape is uniquely
    determined by the contract with NO invented value; if yes, it is a
    normalization (WARN) not a reject; if any value must be synthesized, it
    stays a reject. Precedent in the same file: level auto-collapse. **Fix
    (owner ruling 2026-09-01, verbatim):** on a non-sweep_reclaim condition
    with ANY legs array — drop the array, re-run the full arm validation on
    what remains; valid → proceed with a ⚖ WARN naming every dropped leg;
    still invalid → REJECT UNCHANGED with the original reason, no second pass;
    never synthesize a leg; never normalize the reverse. Implemented in
    `NormalizePlanDocRules` (runs before `validateArmSpecs`), recorded on the
    plan doc (`arm_normalizations`), stamped on the E8 row
    (`normalized`, `dropped_legs`), counted in system_config
    (`arms_normalized_class39`); `plannerRejectedCap` 20 → 200 so the next
    class has a sample. **Law:** the validator's job is to refuse what is
    ambiguous or unsafe, not what is merely misspelled — when the contract
    fully determines the answer, normalize and WARN; when it does not, reject
    with the original reason.

---

40. **Coerced aggregator inside the model's context window (corrected-column
    law on prompt-facing P&L).** Root cause: `EffectivePnL()` — corrected
    value if present, else raw `realized_pnl` — was the accessor every P&L
    aggregator summed (`GetFullStats`, `GetSymbolStats`, `GetRecentTrades`,
    `GetHoldingTimeStats`, `GetDirectionStats`, `GetHistorySummary`); two
    more kept a dead `COALESCE(pnl_corrected, realized_pnl)` fallback in SQL;
    the AgentBeta trade tool read the raw column outright. 2026-09-01
    evidence: decision record 36090 (23:07:13 CT, Sim101) told the executor
    `Total PnL: -203.68 USDT` over 220 trades, where the strict truth is
    **+304.32 over 105 resolved trades, 115 unresolved excluded** (rows
    237–586; row 526 alone: raw −1,458.00 vs corrected −69.43, the ×21
    lot-math artifact riding straight into the prompt); an unresolved short
    with exit 0 rendered as `Profit +0.00 USDT (+100.00%)`. Sign, magnitude
    and count were all wrong — every executor decision was made against a
    fabricated track record. The dashboard header showed the NT8-native
    total (0.00) beside a +212.00 ledger day total. Fourth silent
    counter/aggregator defect in a week (35 replan budget · guardrail ENTRIES
    · GORM alias zero · this) and the first found INSIDE the model's context.
    **Probe:** for every figure a prompt or tool renders, trace the column to
    the row: any accessor with an `else raw` branch, any COALESCE onto the raw
    column, any sum that does not return its exclusion count is coercion.
    **Fix:** `CorrectedPnL() (float64, bool)`; every aggregator strict, NULL
    rows counted as `UnresolvedExcluded` and excluded from sums/averages/win
    rates/streaks; prompt line `Track record: +X over N resolved trades (K
    unresolved trades excluded — see note)` and `#id side entry→? UNRESOLVED`
    rows; `/api/account` ledger day total (footer rule); build-time lint
    (`store/pnl_surface_guard_test.go`) fails on any raw aggregation outside
    the allow-list; boot line `🧾 P&L surfaces: N aggregators strict-corrected,
    0 raw`. **Law:** the model never reads a fabricated track record — a
    figure travels with its resolved n and its unresolved count, and an
    unknown is UNRESOLVED, never a coerced number and never a plausible zero.

41. **Provider mid-stream cut treated as a validator reject (transport
    resets).** Root cause: on 2026-09-01 four of 81 planner SSE calls (4.9 per
    100; 0 of 31 on 08-31) died mid-body — 01:46 `connection reset by peer`
    (RST), 23:47:13 / 23:47:33 / 23:52:41 `stream interrupted: unexpected
    EOF` after 250 s / 18 s / 308 s with 55k / 3k / 70k reasoning chars, all
    `http_status=200 request_id=""`. WHO closed the socket: reproduced
    in-process (`mcp/transport_cut_probe_test.go`) — a peer FIN mid-body
    yields exactly `stream interrupted: unexpected EOF` class=transport, a
    peer RST yields exactly the 01:46 string, and the idle watchdog yields
    `stream idle deadline exceeded … class=idle_deadline` (context.Cause is
    checked BEFORE the reader error, so a watchdog kill can never be
    mislabelled). Verdict: **THEM** — the peer (DeepSeek edge, a CloudFront
    distribution at `api.deepseek.com` → `d3bbv8sr76az5s.cloudfront.net`)
    or its origin closed a live HTTP/1.1 chunked response; our Go code is
    excluded [A]; a middlebox on the WSL2-mirrored / Windows path cannot be
    excluded from strings alone (passive socket-state watcher armed). Our
    side then made it worse twice: (1) the client retry waited a FIXED 2 s
    and call 2 died 18 s later on the same flap; (2) the planner loop
    treated the exhausted transport error as a VALIDATOR reject — attempt 2
    re-authored with `still failed after 2 retries: stream interrupted:
    unexpected EOF` appended as its "validator reason" (owner ruling class
    37 M4 had said: identical prompt, no reject block). **Probe:** for every
    failure path, ask whether the model ever answered; if not, there is
    nothing to repair and no reason to append — resend. For every kill
    switch (watchdog, deadline, ceiling), ask whether it LOGS when it fires;
    an unlogged switch makes "0 kills" an absence of evidence. **Fix:**
    `mcp.IsProviderFailure` (transport / idle_deadline / total_deadline /
    client_timeout / context) → the planner attempt loop re-sends the
    byte-identical prompt (`resend-identical`, no reject block, no
    rejected-prompt row); stream retries count CALLS via
    `AI_PLAN_STREAM_TRIES` (default 3) with the exponential schedule
    `AI_PLAN_STREAM_BACKOFF` (default 2s→15s→45s); the idle watchdog logs a
    `⏱ stream idle watchdog FIRED: Ns since last SSE line` WARN when it fires;
    dialer keepalive 30 s confirmed in effect (`ss -o` timer), unchanged;
    executor serialization NOT added (all 71 overlapped streams: 4 cuts; 6
    non-overlapped: 0 — no power, no effect shown). Boot line `🔁 planner
    stream policy (class 41)`. **Law:** a provider failure is retried, a
    validator reject is repaired — never append a transport error to a
    prompt; and every kill switch logs its own fire.

42. **Optimising the 4% (a wave aimed at the wrong term).** Root cause: the
    planner call was slow (p50 448 s, p95 581 s) and the plan JSON was assumed
    to be the output. It is not. Measured 2026-09-02 over n=67 full-author
    calls (2026-08-31 → 09-02, `prompt>9000`): **p50 completion 23,769 tokens,
    mean 22,376, mean wall 349 s, mean reasoning 72,477 chars**; the stored
    plan docs (n=61 since 2026-08-28) average **3,088 bytes ≈ 920 tokens**.
    The plan JSON is therefore **~4% of the output and reasoning is ~96%** —
    deleting the entire schema could not deliver the 40-50% cut the wave was
    scoped for, and the field-by-field audit found **no removable field**
    (every one of the 9 top-level fields has a reader: levels ~402 tok,
    scenarios ~237, reasoning ~161, no_trade ~42, bias ~33, death_condition
    ~18, flip ~14, death ~10, day_type ~3). **Probe:** before optimising a
    cost, measure its COMPOSITION and quote the share of the term you intend
    to cut; if the term is under ~20% of the total, the optimisation cannot
    reach a headline target no matter how well it is executed. Ask what the
    other 80% is made of. **Fix:** no schema cut shipped (it would have been
    risk without reward); the finding is pinned by two tests
    (`TestRootFixEveryPlanFieldHasAReader`, which fails if any field ever
    loses its last reader, and `TestRootFixPlanJSONIsASmallFractionOfOutput`,
    which fails if the JSON share ever exceeds 15%) and by the boot line
    `✂ planner schema`. The real lever — reasoning MODE — ships as a
    measurement instrument, not a change: a fast-mode shadow A/B
    (`SHADOW_AB_ENABLED`, default OFF) that re-runs the identical prompt at
    reasoning=fast AFTER the live read, validates it through the full chain
    offline, writes nothing, and is judged against a criterion registered
    BEFORE the data (legal-rate ≥ max AND median wall ≤50% of max at n≥10).
    **Law:** measure the composition before you optimise a total, and state
    the share you can actually reach; a pre-registered criterion beats a
    narrative when the result arrives.

43. **Uncited code-canon governing money (the [C] knob that was never
    researched).** Root cause: `MIN_SL_ATR_MULT = 1.0×ATR5m` shipped as the
    stop floor with no citation — the knob census labelled it **[C]
    code-canon**, and three gates read it (arm-time, AI-entry, planner
    authoring WARN). Round-7 research tests the day-trade range at
    **1.5–2.5×ATR** and finds stop-out rates above 60% on noise alone below
    1.0×; our own tape has **6 of 8 losers with MAE beyond the stop** and
    **15 of 27 losers stopped-too-tight**. Worse, width alone was never the
    defect: on the five biggest losers **0 of 5 stops sat ON a seated level**
    and **2 of 5 sat in dead zones 40+ points away** — a wider stop in a dead
    zone is still a stop in a dead zone. Two live exit mechanisms (BE+40, the
    ATR trail) were moving those stops **with zero measurement** (2 BE moves
    and 8 trail ratchets on 09-01; $719.50 of giveback with zero trail EXITS
    ever), and the size resolvers disagreed in production — arm-leg capacity
    resolved 1 while order sizing resolved 2 and the boot line said
    capacity=1. **Probe:** for every knob that decides money, demand its
    citation. A default with no research behind it is a guess with a
    confidence interval of the whole real line; a mechanism that moves a live
    stop without a measurement is worse than one that does nothing.
    **Fix (0B):** floor 1.0→1.5 (the BOTTOM of the researched range, not the
    middle); the stop is COMPOSED — beyond the nearest seated level on the
    risk side + clearance, floored at the ATR multiple, widest wins, never
    tighter than authored, `stop_unanchored` + ATR floor in a dead zone and a
    level is NEVER invented; BE and the trail suspended behind
    `EXIT_MECHS_SUSPENDED` with a single wire seam so a fixture proves zero
    move_stop frames; Stage-A size ceiling of 1 contract until n≥30 with a
    positive lower-CI expectancy (the floor raises dollar risk ~50% at
    constant size — which is why size does not move with it). **Law:** a knob
    that decides money carries a citation or a suspension — never a number
    someone once typed.

44. **The repair prompt judged the model against a vocabulary it never
    showed it.** Root cause: attempt ≥2 defaults to a REPAIR call
    (`BuildPlannerRepairPrompt`). It carried the rejected output, the validator
    errors and a law excerpt — but never `LiveConditionsLine`, which only the
    RE-AUTHOR tail appended (`plannerRejectBlock`). So from the moment class 34
    shipped the condition vocabulary, the DEFAULT retry path ran without it.
    Worse, `lawExcerptsFor` was a first-match `switch` whose cases matched
    neither `fade_requires_touch` nor `invalid (` — the two commonest confirm
    defects — so those fell through to a GENERIC excerpt about level labels and
    targets. **Measured** (all repair attempts, 2026-09-01 → 09-02, n=28): 18
    rejected at the parse/schema step (64%), 8 accepted, 2 rejected later.
    Of the 18: **1** packaging failure (`cannot unmarshal number 0.5 into …
    PlanArmLeg…size of type int` — a fractional contract size, 04:24:17) and
    **17 that PARSED CLEANLY** and were rejected on field values — 10 of them
    confirm-rule vocabulary errors (`"2x5m"` and `"displacement"` written into
    `confirm2.rule`, `1x5m_close` on a fade). **11 of 17 received an irrelevant
    law excerpt.** **Probe:** when a retry keeps failing, read what the retry
    prompt actually CONTAINS, not what the author path contains — a default
    path and a fallback path drift apart silently. And check whether an error
    router is first-match when errors can be plural. **Fix:** the repair prompt
    carries `LiveConditionsLine`; `lawExcerptsFor` collects EVERY applicable
    excerpt; a new `RepairConfirmVocabLaw` names the confirm enum and states
    that the death/flip enum is a DIFFERENT vocabulary (class-38 rule); the
    return contract is restated at head AND tail (lost-in-the-middle); a
    fragment gets its own reason instead of a confusing schema error; outcomes
    are classified (`ok|content|packaging|fragment|no_outcome`) and RECORDED in
    system_config, replacing a log line that called all 18 "UNPARSEABLE".
    **Note the dispatch's premise was wrong and the audit's was right about the
    RATE only:** extraction already tolerated fences and prose
    (`extractJSONObject` scans to the first `{`), so an extractor rewrite would
    have fixed 0 of 18. **Law:** every retry path shows the model the same
    vocabulary the validator will judge it by; and a diagnosis label must name
    what actually happened, or it hides the defect it was added to expose.

45. **A pantry nobody could reach (two bar layers, no resolver) — and a
    provider calendar that is not ours.** Root cause: two unconnected bar
    layers existed with no single door between them. The NT8 BarCache held
    native per-TF series (measured live on 0465a10b, 2026-09-02: **1w 383
    bars back to 2019-05-03 · 1d 1500 back to 2020-11-11 · 4h 1500 back to
    2025-09-11 · 1h 1500**), all memory-only; the persisted `bars` table held
    **1m only**, because `InsertBars` carried the line `if r.TF != "1m" {
    continue }` — so every restart discarded the entire coarse pantry. The
    weekly reader read the 1m table directly (`BarsBetween(symbol,"1m",…)`),
    which starts 2026-08-19, saw **2 completed weeks** against a ≥4 guard, and
    rendered "WEEKLY thin · low" while 383 native weekly bars sat unread. Four
    hand-rolled 1m→TF aggregators each bucketed on their own convention and no
    caller could answer where a daily bar came from. **The second, sharper
    defect:** NT8's native weekly bars run **Friday 00:00 → Thursday 23:59**,
    while every weekly concept in this system is Monday-governed
    (`weekStartMonday`; "Sunday 17:00 CT first print"; PWH/PWL from the prior
    Monday week). Pointing the weekly reader at native 1w — the obvious
    "nt8-first" fix — would have shifted every week by three days and replaced
    an honestly-labelled *thin* with silently WRONG data. **Probe:** before
    consuming a provider's aggregate, print its first and last bar timestamps
    and ask which calendar they are on; never infer the convention from the
    TF's name. And for any two-layer store, ask what single function answers
    "give me completed bars for X" — if none exists, every consumer has its
    own answer. **Fix:** one resolver (`market.CompletedBars` /
    `CompletedBar`) with the fallback ladder as DATA (`barLadder`), the
    repaint law applied at one chokepoint (`dropForming`), and the source
    (`nt8|nt8_agg|own1m`) travelling with the bars. Weekly's ladder is
    `1d → 1m`: native 1w is EXCLUDED with its reason recorded in
    `ladderExclusions`, and `StampAligned()` catches the same mismatch class
    generically. Every cached TF is now persisted; retention became PER-TF
    (`tfRetentionDays`) because the old TF-blind 90-day cutoff would have
    deleted the deep weekly history on the first nightly prune after it was
    stored — 1m 90d, 5m 180d, 15m 365d, 1h and coarser forever, ≈31 MB steady
    state. Native 1w is persisted labelled `convention=fri_thu` for research
    only. **Law:** one resolver, one stamp convention, and a provider's
    calendar is a measurement — not an assumption you inherit from a
    timeframe's name.

47. **A scheduled mechanism paced by its own throttle, not by events.** Root
    cause: the level-event wake had exactly one limiter — `wake_min_interval_min`
    (30 min) — and no notion of whether a wake could still produce a TRADEABLE
    plan. Measured 2026-09-02: 60 wake re-plans in 7 days against 33 arm rows, 23
    ever placed and 9 ever working/filled; today's wakes fired at 08:42:30 ·
    09:12:30 · 09:42:30 · 10:14:30 · 10:44:30 · 11:15 · 11:45 · 12:16:30 ·
    12:48:29 · 13:18:29 · 13:48:29 · 14:20:29 — a clean ~30-minute drumbeat,
    which is the signature of a condition that is CONTINUOUSLY true being paced
    by the throttle rather than by events. NY bought 12 plan versions that way,
    including a max-reasoning read at 14:20:29 that sat 10 minutes from the
    last-entry cutoff and 25 from the flat: a plan that could never be entered.
    Two adjacent defects fell out of the same audit: (a) the planner in-flight
    claim is keyed per (trader, trade_date, session), so a LONDON read and an NY
    read hold different claims and stream concurrently — 08:01:06 today opened a
    second max-reasoning stream while 07:51:06 was still running; and (b) a
    NEVER-PLACED arm row survives its own plan version indefinitely — NY row 32
    (v5, S3, no signal id, 10:30:30) stayed non-terminal until the 14:45 EOD
    flat, ~4h15m across v5→v12, holding the class-33 cutover gate's leg 4 shut
    the whole time. **Probe:** for any periodic mechanism, plot its firing times.
    Even spacing at exactly the throttle interval means the throttle is the
    scheduler and the trigger is noise; then ask what the work it produces is
    still USED for. **Fix:** cutoff and cooldown land WARN-first — they log
    `would_skip` with a recorded per-session counter and the wake still runs, so
    the suppression ruling is made on a week of counts rather than impressions;
    a wake (never a scheduled read) defers while any planner stream is open; a
    never-placed arm from a superseded version goes terminal `superseded`, with
    placed rows untouched. **Law:** a mechanism whose only limiter is its own
    throttle is not scheduled, it is idling at rate — and before suppressing it,
    measure what suppression would have cost.

48. **The decision path bypassed the arm-seam gates.** Root cause: the five
    entry protections lived ONLY at the arm seam (`armed_executor.go`): the
    R:R floor (`armMinRR`), the 0C shadow map (`conditionShadowedFor`),
    scenario-direction consistency, stop composition (`composeArmStop`) and
    one-live-arm (`oneLiveArmGuard`). The AI market-entry path
    (`auto_trader_orders.go` → `executeOpenLongWithRecord` → `trader.OpenLong`)
    ran a different, thinner chain: `validateDecision` enforced R:R + min-SL +
    HTF veto at the PROMPT-TIME SNAPSHOT price while the fill is a MARKET
    order ~10 points away, and nothing checked shadow / scenario-direction /
    one-live-arm / stop composition; the agent chat `execute_trade` ran almost
    none. **Measured 2026-09-02:** 587 R:R eval `2.03 → PASS` @ snapshot
    29069.50, filled 29079.25 → real R:R **1.09** (below the owner's 2.0
    floor); 589 and 590 traded the SHADOWED condition `breakout_retest`; the
    08:13 R:R 3→2 save had no persisted row (the class-44 `config_changes`
    table wires only future saves). **Probe:** for every protection, list the
    call sites — a gate whose only callers live in one file is absent from the
    other path; and a floor judged on a stale reference is not a floor.
    **Fix:** ONE `EntryGate` (`trader/entry_gate.go`) — legs direction →
    shadow → R:R-at-live-price → min-SL → one-live-arm — called by BOTH seams
    before any order leaves; refusals recorded per path
    (`decision_records.Error` + gate-block counter; arm-refusal counters).
    **Law:** a protection that exists on one order path and not the other is
    not a protection — it is a suggestion.

    **RIDER — "one gate, two ATRs" (no-trade-forensic 2026-09-03):** the
    class-48 decision-path min-SL leg initially read `kernel.PlanDATRFor`, but
    `SetPlanDATR(..., dATR)` stores the **DAILY ATR** (`plan_render.go:370`) —
    not ATR5m. Measured: decision records 36640/36641/36642 (09-02 18:48-18:52)
    refused against `1.5×ATR5m = 450.56` (dATR 300.4) while the arm seam in the
    same minutes used ATR5m 12.78-14.12; all three targets printed within 28
    minutes (19:07/19:15/19:16). Fix: both seams now read ONE resolver —
    `armSeamATR5m` = the arm seam's chain (`FuturesBarsProvider`
    AISVPBarInterval/Count → `AcceptanceBars(...,"2x5m")` → `ExportCalculateATR`
    14) — and the doubled `entry_gate: entry_gate:` prefix collapsed. **Law:**
    when one gate serves two paths, it reads ONE resolver per input — a gate
    with two ATRs is two gates, and one of them is lying.

49. **Instrument theatre: a boot line that could not be wrong, a label that
    usually was, and a watchdog that never fired.** Root cause: the
    instruments reporting on the transport wave were themselves unmeasured.
    **(a) The class-41 boot line printed `watchdog_log=on`,
    `serialize_executor=off`, `resend_identical=on` and `keepalive=30s` as
    STRING LITERALS, and its fixture asserted the same literals** — the pair
    could only ever agree with each other, never with reality, and reality
    differed: keepalive on the wire ran 14-20 s. Class 6 (Go-side theatre) on
    the very wave shipped to make transport honest. **(b) `timeout_source`
    DEFAULTED to `"transport"`** and was overridden for four sentinels only,
    so it tagged 5xx bodies, parse failures and empty 200s as transport: right
    on 5 of 50 audited failures, wrong on 23. Two labelling systems
    (`timeout_source` and `class=`) disagreed on the same line. **(c)
    `IsProviderFailure` returned false for `class=other`**, so an empty 200, a
    parse failure or an over-long answer was appended to the next prompt as
    the MODEL's defect ("fix this: unexpected EOF") — the class-34/37 poisoned-
    feedback disease, still open. **(d) The idle watchdog reset on every
    scanned SSE LINE, including DeepSeek's `: keep-alive` comments**, so a
    stalled generation that was still heartbeating ran to the 1200 s ceiling.
    It had never fired once — and its close was indistinguishable from a peer
    EOF, so "0 idle kills" could not be told from "0 idle kills we can see".
    **(e) A 503 burst produced 3 planner attempts × 3 client tries = 9
    provider calls in ~7 s** at an edge already shedding load. **(f) The
    sockwatch bash loop wrote 12,947 lines and caught ZERO FIN/CLOSE-WAIT
    states** — blind to the single thing it was built for. **Probe:** for
    every field an instrument prints, ask which function ENFORCES it and
    whether the fixture calls that function; a fixture that asserts the same
    literal as the code tests nothing. For every default label, ask what
    fraction of cases it is right about. For every kill switch, ask what input
    would make it fire, and whether its output is distinguishable from the
    thing it is meant to detect. **Fix:** `mcp.PlannerClientPolicyLine()` —
    every field read from its enforcer, keepalive from the one place that sets
    it, and `observed=n/a` rather than implying the set value is the seen one;
    `timeout_source` DELETED and `ClassifyFailure(err, httpStatus)` the only
    classifier (it sees the status, so a 503 body is `http_5xx` even when its
    text says EOF, and `http_status` splits 5xx/4xx so retry policy can tell
    "provider overloaded" from "our request is wrong");
    `FailureIsProviderSide` decides resend-vs-repair, with `parse` deliberately
    MODEL-side (resending an unparseable document identically would loop
    forever — the pre-existing class-41 fixture caught that over-generalisation
    during this wave); a two-timer watchdog (pre-token, heartbeats allowed;
    post-token, reset ONLY by content/reasoning deltas) closing with its own
    `ErrWatchdogIdle`; a per-READ storm cap (`AI_PLAN_STORM_CAP`, default 5);
    and `httptrace` reporting `closed_by=peer_fin|local_close|clean` from
    inside the process, labelled INFERRED because httptrace sees no TCP flags.
    Rider (owner ruling): the confirm enum attaches to every repair whose
    DOCUMENT carries a confirm object, not only when the incoming error names
    one. **Law:** an instrument that cannot disagree with the code is not
    evidence — every printed field reads from its enforcer, every default
    label earns its default, and every kill switch must have an input that
    fires it and an output you can tell apart.

50. **The prompt withheld what the validator enforces — and the correction
    remembered only the last mistake.** (Dispatch "class 45"; checklist slot 45
    was already the pantry class, hence 50.) Root cause: the planner prompt and
    the plan validator are two statements of the same law, written in different
    places, and nobody diffed them. **(a) The prompt ORDERED A PLAY, the
    validator VOIDED it.** Line 589 read "If price sits BELOW PDL you MUST write
    a continuation short" — an unconditional MUST naming one condition. But
    `BreakdownContinueState` voids a breakdown continuation once a close comes
    back across the level, so on any reclaimed level the only compliant answer
    was a rejected one. The model obeyed and was punished; the 2026-09-02 LONDON
    01:32 read burned attempt 1 exactly this way. **(b) The stop floor was
    enforced but never stated.** 0B raised the arm-time floor to 1.5xATR5m and
    the planner was told no number at all, so it authored stops that were
    silently widened at arm time — and the R:R gate then judged the WIDER stop,
    refusing arms the model believed it had sized correctly. **(c) The
    correction block carried only the LAST defect.** Same read: attempt 1
    rejected for the voided breakdown, attempt 2 for a fade with no touch,
    attempt 3 told only about the fade — it fixed the fade and walked back into
    the void, spending the whole chain on two defects it had already been told
    about separately. The block also sat at the TAIL of a ~6,600-token prompt,
    the position most likely to be skimmed. **Probe:** enumerate every MUST in
    the prompt and name the validator function that can reject a document
    obeying it — any MUST that names a CONDITION rather than a DIRECTION is a
    contradiction waiting for the right market. For every gate that can rewrite
    or refuse the model's output, ask whether its threshold appears in the
    prompt as a number. For every retry, ask whether it states the defects of
    ALL prior attempts or only the most recent. **Fix:** the MUST now orders a
    DIRECTION and names the legal conditions; `ComputeVoidBreakdownLevels`
    lists every already-reclaimed breakdown level in the prompt by CALLING
    `BreakdownContinueState` itself (a parity fixture pins 40/40 checks across
    20 tapes, so the list cannot drift from the verdict); `RenderStopFloorLine`
    prints the floor with the live ATR reading; `addDistinctReject` accumulates
    the chain's DISTINCT defects and `plannerRejectHeader`/`plannerRejectTail`
    state them at the TOP and the TAIL (~240 tokens), recorded at all eight
    reject sites. **Law:** the prompt must state every rule the validator will
    enforce, in the validator's own words and by calling the validator's own
    code where a verdict is involved; a correction is cumulative or it teaches
    the model to trade one mistake for another.

51. **A direction was shipped on evidence that never existed — the weekly bias
    was anti-predictive.** (Dispatch "weekly refs only"; class 50 wave.) Root
    cause: the weekly-bias design (2026-08-30) assumed the Sunday doc's
    directional call carried signal, and every consumer — the W4 invalidation
    watch, the F5 write-time DOA stamp, the draw-alignment tag, the
    WEEKLY-COUNTER shadow — read `WeeklyDoc.Bias` as a direction. The first
    out-of-sample test of that direction (bias calibration 2026-09-02,
    pre-registered; report `docs/superpowers/reports/2026-09-02-bias-calibration.md`)
    found the reconstructed rule ANTI-predictive on holdout (raw hit 25–28%,
    called-only 45–51%; net-of-friction t ≈ −14) — a label was governing prompts
    with negative signal, and no reader had ever asked for its evidence.
    **Probe:** for every advisory/soft-law label in a prompt, ask what
    out-of-sample test earned it the label and what it would take to strip it;
    a "shadow" annotation that READS a direction is a consumer, not a shadow.
    **Fix:** the weekly doc is REFS ONLY — weekly_levels (PWH/PWL/IPDA/NWOG)
    plus a facts-only narrative; the validator now REJECTS directional tokens
    (r4) instead of demanding them; the chip and both prompt lines render
    "WEEKLY: refs only — PWH x · PWL y"; the invalidation watch, DOA stamp,
    counter shadow and draw-align tag are retired (nothing reads bias as a
    direction anymore); the deterministic rule survives as `shadow_bias` on the
    doc + a log line so the anti-prediction keeps being measured — never read,
    never inverted. **Law:** a directional label is evidence or it is noise; a
    label with measured negative signal must be demoted to shadow, and nothing
    — not even a "shadow" annotation — may consume it as a direction.

52. **A rule rendered from the clock it was WRITTEN by, not the clock it is
    READ by.** The plan card's no-trade list was the model's prose, stored at
    authoring time and shown verbatim for the rest of the session. On
    2026-09-02 the ASIA card at 23:00 CT listed three constraints and not one
    of them could refuse an entry: the first-5m band had closed six hours
    earlier, the lunch window belongs to NY and cannot apply to an ASIA
    session, and the red-news blackout had fired fourteen hours before. The
    same defect had a second face: the windows were defined THREE times — the
    entry gate's `cur < start+N` and its `InBlackoutWindow(t,"12:00","13:30")`,
    the adherence grader's own copies, and whatever prose the model happened to
    write — so the gate refused one window, the grader scored another and the
    card claimed a third, with nothing that could fail if they drifted apart. A
    fourth copy sat in the prompt, teaching the model windows nobody enforced.
    And the clock-drift widening appended "+1m (clock drift)" for ANY nonzero
    offset, so a healthy 108 ms NTP reading put "the clock is drifting" on the
    card for the whole trading day. **Probe:** for every rule a surface
    displays, ask WHEN it was evaluated and against WHOSE clock; a rule
    rendered from stored text is a rule frozen at write time. For every window,
    threshold or window name that appears on more than one surface, find the
    single function all of them call — if there isn't one, count the copies.
    For every advisory suffix, ask which measurement makes it TRUE and whether
    that measurement can be zero. **Fix:** `kernel/no_trade_band.go` holds one
    definition each (`FirstNoTradeMinutes`, `LunchWindowCT`) read by the gate,
    the grader and the card; the machine writes `no_trade_windows` onto the doc
    at plan time, taking red news from `t1WindowsFor` — literally the windows
    the gate will refuse inside, widening and fail-closed fallback included, so
    the card cannot compute a second answer; `EvaluateNoTradeWindows` stamps
    each one live / elapsed / other_session against the READER's clock, asking
    session geometry first (does this window touch the session at all) and
    elapsed second; the model's prose is untouched and renders as notes; the
    prompt's example and rule sentence are generated from the same functions,
    with a literal scan allowing exactly one copy of each bound at the
    definition site; and the drift claim is made only when the offset alone can
    move an event by a whole minute, while the minute of boundary protection is
    kept for every input. **Law:** a surface renders a rule's STATUS, never its
    text — and a rule with more than one definition has none.

53. **One question, two answers: a predicate shared by two callers that fed it
    different inputs.** (Numbered 51 at merge against a tree that did not
    yet carry class 50's entry; renumbered to 53 by owner ruling 2026-09-02 —
    class 50 keeps 51, the no-trade band keeps 52. Class 46 is deliberately

54. **A refresh that deletes before it knows what comes back.** (Renumbered 52→54 AT MERGE, 2026-09-03 combined boot — 52 was taken by the no-trade-band class. Dispatch
    "bar-arbiter merge"; class 52 wave, 2026-09-02.) Root cause: the
    `/api/nt/bar-arbiter` `backfill` action cleared the replay window
    (`ClearSince`) BEFORE the deep bars_subscribe was sent — on 2026-09-02 a 1m
    ask for 1,000,000 bars came back capped at ~2,005 and the pre-wipe deleted
    three weeks of accumulated 1m (14,508 rows → ~2,000) that the replay could
    not replace. The destroy-first order is the bug: the endpoint deletes rows
    whose replacement it has not received and cannot guarantee. **Probe:** for
    every refresh/backfill path, ask whether any delete happens before the
    replacement data has ARRIVED and been counted — a wipe whose refill is
    bounded by a third party's cap is a one-way door. **Fix:** the backfill is
    now a MERGE — the wipe is gone and the persister's `INSERT OR REPLACE`
    replaces exactly the bars the replay returns; rows it cannot replace are
    never deleted (response states `cleared_rows: 0` + the merge note). Pinned
    twice: a store test (14,000-row 1m table + 2,000-bar replay → 14,000+ rows,
    earliest row survives, revisions win inside the replay range) and a
    handler-source wiring lint (no `ClearSince` in the backfill branch).
    **Law:** never delete history the refill cannot reproduce — a refresh
    merges into what exists, it does not clear a window on faith.

55. **Two spellings of "we don't know", and a field that meant something else
    than its name.** (Highest occupied at merge: 51.) Root cause: three states
    were stored as two values. `position_plan_join`'s comment has said since
    2026-08-26 that unresolvable rows carry `plan_id='UNRESOLVABLE'`, and four
    do (530, 539, 545, 546) — but three others carry `''` (566, 571, 580),
    which is also what a row looks like *before* anything stamps it. A consumer
    testing the sentinel misses the empty ones; a consumer testing empty misses
    the sentinel; neither can tell "we looked and found nothing" from "nobody
    has looked yet". Separately, `armed_orders.version` was overwritten by
    `UpsertArm` on every re-authorization, so an arm's stored version was the
    LAST version that touched it, not the version it was armed under — and a
    cadence audit that leaned on it could not defend its own reading.
    **Probe:** for every column that encodes an absence, ask how many distinct
    absences exist and whether each has its own value; for every column whose
    name implies a moment ("armed under", "created by", "first seen"), find the
    write path and check nothing later overwrites it. **Fix:** one sentinel with
    a `ClassifyPlanLink` three-state classifier that consumers call instead of
    comparing strings; the materializer stamps the sentinel at creation, loudly,
    so a row is never born `''`; a scoped idempotent convergence for the
    day-plan era ONLY (pre-era history is left alone — calling a crypto-era
    trade "unresolvable" would imply we looked for a plan that never existed,
    the same lie in the other direction); and `armed_under_version`, set once at
    first authorization, never overwritten, with `version` documented as
    last-touch. **The measurement that shaped the wave:** the dispatch believed
    the join was blind for 25% of a week and that reconcile-materialized rows
    could never be stamped. Measured: since the day-plan era, 51/51 `system` and
    5/5 `armed_entry` rows carry a link, 8 of 11 `reconcile` rows do, September
    was 0 unlinked, and the two rows named as unstamped (584, 586) were already
    fully stamped. The wave shrank to roughly a fifth of its dispatch.
    **Law:** every distinct absence gets its own value and one classifier; a
    column named for a moment must be written once; and measure the rate before
    building for it — an alarming percentage over the wrong denominator will
    buy a large fix for a small problem.

56. **A default of 0 on a column that means "how far did it go against us".**
    (Highest occupied at merge: 53.) `trader_positions.mae REAL DEFAULT 0`
    and `mfe` the same. A trade whose excursion was never computed and a trade
    that never went against us are then the same bit pattern, and no reader can
    tell them apart. Measured 2026-09-02: of 586 closed positions **517 carried
    mae=0 AND mfe=0** — the never-computed signature, since price always moves —
    while 9 carried a single genuine zero beside a real number. Round 7 ruled
    that exit rules, stop sizes and targets come from MAE/MFE distributions, so
    every one of those rulings was waiting on a column that was 88% unreadable.
    Two more faces of the same defect: `kernel.LearningLine` guarded its average
    with `if t.MAE > 0 || t.MFE > 0`, which silently drops a genuine zero AND
    counts an uncomputed row as merely absent, then printed an average with no n
    at all; and `ComputeExcursion` filtered bars with `b.OpenTime < entryMs →
    skip`, so unless a fill landed exactly on a bar boundary the bar CONTAINING
    the entry — the one holding the first adverse move — was excluded. **Probe:**
    for every numeric column, ask what value means "not measured" and count the
    rows holding it; if that value is also a legal measurement, the column
    cannot answer the question it exists for. For every average, ask what its n
    is and whether the reader is told. For every window over bars, ask whether
    the boundary bar is in or out and whether the answer is the same at both
    ends. **Fix:** `trade_excursions` — one row per position, every numeric
    NULLABLE and NULL until computed, written by the machine at open, recomputed
    each tick while the position lives, closed with the exit half, and carrying
    what a distribution needs: extremes with their timestamps and bar offsets,
    bars held, bars that reached BOTH the stop and the target, the resolution
    the path was built from, and pnl_corrected. A hold the tape does not reach
    is marked `resolution="none"` and keeps its NULLs — never a guessed number,
    and the count of those rows rides the boot line. `mae`/`mfe` on
    trader_positions became `*float64` with a migration that nulls only the
    517-row pair; `LearningTrade` carries `Measured` and the line prints
    "(n=1 of 2)"; `ComputePathExcursion` counts every bar whose window
    intersects the hold. **Law:** a column that cannot say "unknown" cannot be
    the input to a ruling — and an aggregate that hides its n is not evidence.

57. **A magic epoch as a scope constant — and the fixture holding the same wrong
    copy of it.** (Highest occupied at merge: 56.) Root cause: a guarded,
    WHERE-scoped, idempotent migration (`ConvergePlanLinkSentinel`) took its
    boundary from the literal `1755230400000`, commented `// 2026-08-15`. That
    epoch is **2025-08-15** — a year early. The migration ran at the 2026-09-03
    boot and converted **516 pre-era rows** to a sentinel the wave's own report
    had explicitly said to leave alone, instead of the 3 rows in scope. Every
    other guard held: the write was scoped, idempotent, flag-gated, logged and
    counted — and none of that helps when the SCOPE ITSELF is wrong. **The
    second half is worse than the first:** the fixture defined its own
    `const eraMs = 1755230400000` with the same wrong comment, so the test and
    the code agreed with each other and disagreed with reality. The test passed
    while the migration over-reached by 172×. (Class 49's shape, in data: an
    instrument that cannot disagree with the code.) A third instance appeared
    during the fix — the assertion written to pin the epoch had a hand-typed
    number that was also wrong. **Probe:** for every date/threshold constant
    that SCOPES a write, ask (a) what date does this literal actually resolve
    to, asserted in a test, (b) does any fixture hold its own copy, and (c) does
    the boot line report the resulting COUNT so an over-reach is visible on the
    first boot. Here (c) is what caught it: `unresolvable=525` when ~9 was
    expected. **Fix:** one named `DayPlanEraStart = time.Date(2026, 8, 15, …)`;
    the migration and every fixture derive from it; a test asserts the resolved
    DATE (not a typed epoch) and explicitly refuses the 2025 value.
    **A FOURTH instance, found by the owner asking which zone the era is
    defined in:** the corrected constant used **UTC** midnight, which is
    2026-08-14 **19:00 CT** — five hours early. The era's own definition is CT,
    and the first day-plan row proves it: `created_at` 2026-08-16 00:44:31 UTC
    with `trade_date` **2026-08-15** (session NY). `trade_date` is a CT calendar
    date and it is the key the era is NAMED by, so a UTC boundary cannot express
    "trade_date >= 2026-08-15". That error was LATENT — the disputed five-hour
    window held 0 rows — and would have bitten the first row to land in it.
    **Law:** a scope constant is a named date **in a stated zone**, with a test
    that prints the resolved instant in BOTH zones and refuses every prior wrong
    value by name; no fixture keeps its own copy; and a migration prints the row
    count it touched where a human will read it on the next boot. Four wrong
    values in one constant (a year early, a fixture copy of the same, a
    hand-typed assertion, then a zone) is what an unstated zone and a typed
    epoch cost.

58. **A mode that existed only in a comment.** (Highest occupied at merge: 57.)
    Root cause: `plan_mode` was documented as `advisory | direction | strict`
    in a doc comment (`store/strategy.go:919`) and offered in the Studio
    selector, but **`strict` was never implemented**: `PlanModeFor` returned a
    saved `"strict"` unchanged (no self-heal, and no `"normal"` mode ever
    existed), and **no consumer in non-test code compared against it** — the
    only mode any consumer tested was `direction`. An owner could select it,
    the value would persist, resolve, and render, and nothing anywhere would
    behave differently. The Studio audit reached it from the other side, listing
    it as a dead option to REMOVE. **Probe:** for every enum value a UI offers,
    grep for a consumer that compares against that specific value — a value
    present in a doc comment, a schema and a selector but absent from every
    comparison is a control wired to nothing. Do the same for every mode named
    in a comment listing alternatives. **Fix:** `strict` was **documented,
    never implemented; first implementation 2026-09-03 by owner ruling** — it is
    a NEW GATE, not a restoration, and it is recorded as such because the
    dispatch that ordered it believed it was reviving deprecated behaviour.
    Semantics: only plan scenarios execute · arm path only · decision-path
    market entries refused · direction must equal the cited scenario's ·
    refusals logged `refused: strict`. Implemented as leg 0 of the ONE
    EntryGate (class 48) so its refusal is the one the journal shows, with pins
    in both directions (refuses a decision-path open; allows an arm whose side
    matches its scenario) and a pin that `advisory`/`direction` are unchanged.
    **Law:** an option a user can select is a promise; either a consumer
    compares against it or it does not appear in the selector. A comment
    listing modes is not an implementation, and "deprecated" and "never built"
    are different findings that call for different fixes.

59. **A verdict the system published to itself and never read.** (Renumbered 57→59 AT MERGE, 2026-09-03 combined boot 2 — 57 was taken by the magic-epoch class. Highest
    occupied at merge: 56.) The scenario evaluator writes, every cycle,
    "🎯 scenario S1 → ≈invalidated @ 29285.00 (price accepted through the level
    against the trade — **display-only estimate, never execution-wired**)". The
    label was honest and was the defect. On 2026-09-03 the verdict landed at
    08:50:54; the arm seam armed that same S1 short at 29285 at 09:02:54, it
    filled at 09:03:53 and stopped at 09:20:45 for −$140. No gate leg read the
    conclusion the system had already reached about the setup it was about to
    trade. Three companions of the same shape, all "the record exists and
    nothing consumes it": the plan card rendered the LIVE plan (v3 S1 long) over
    a position armed under v2 S1 short — both called "S1", so the owner read
    long while holding short, and `armed_under_version` had existed since the
    attribution wave with **zero readers**; armed row 35 read `state=filled`
    with `fill_quantity=0` beside a position of quantity 1, because nothing ever
    wrote the column and 0 is also a legal "nothing filled"; and "⚔️ armed …"
    re-logged four times after that row had filled, because the ledger lookup is
    `ListNonTerminal` and a filled row is invisible to it, so the authored
    branch ran again. **Probe:** for every verdict, estimate or status the system
    PUBLISHES, name the code that consumes it — a verdict with no consumer is a
    comment. For every column added by a wave, grep for readers before trusting
    it. For every "display-only" or "advisory" label, ask what would change if
    it were wrong, and whether anyone would notice. For every dedup key, ask
    which state transitions it is blind to. **Fix:** EntryGate leg 3 (arm path
    only) refuses on the evaluator's own verdict, reached through
    `kernel.EvaluatePlanScenarios` — the same function and the same plan-birth
    windowing as the display path, never a second predicate — recorded under its
    own refusal class `invalidated`; an unresolved verdict PASSES and says
    "invalidation check unavailable", because an unresolved check is not a
    refusal. The evaluator is stateless, so it stamps `scenario_invalidated_at`
    once on the transition and the refusal names the verdict time rather than
    the check time. `/plan/today` gains `open_position` provenance and
    `ArmedUnderBlock` states a position's own version BEFORE the plan on screen,
    rendering "version not recorded" for pre-attribution rows instead of "v0".
    `fill_quantity` is stamped on the lineage path and a zero never overwrites a
    measurement. The authored-log dedup value carries the row's state, and a
    terminal row never logs "armed" at all. **Law:** a verdict the system
    publishes to itself must have a named consumer or be deleted — and a column
    nothing reads is not a feature, it is a rumour.
    nothing reads is not a feature, it is a rumour. **Rider (2026-09-03, after
    the boot):** the `fill_quantity` fix shipped INCOMPLETE and proves the law
    against itself. It stamped at fill time only — and the fill frame lands
    BEFORE the position row materializes, so `stampArmedFillLineage` returns on
    that path first. measured on the
    current rev, **10 of 10 filled armed rows carry `fill_quantity=0`** — the
    stamp never lands. TWO mechanisms, the second dominant: (a) the
    materialization race (row 35 filled 09:03:53, position 591 materialized
    09:05:14 — 81s), and (b) **class 28 again** — `GetOpenPositionBySymbol`
    compared `side = ?` case-sensitively while `armed_orders.side` is always
    lowercase and `trader_positions.side` is overwhelmingly uppercase (LONG 280
    / SHORT 304 vs long 1 / short 2); `side='short'` matched 0 rows for position
    591 and `side='SHORT'` matched 1, so the fill-time lookup could never
    succeed whatever the timing. Fixed with `UPPER(side)=UPPER(?)` there and at
    two siblings. **The log line hid it:** "position row not materialized yet"
    prints whenever `pos == nil` — true for either mechanism — asserting the
    race as fact, which sent two sessions after a timing bug. **And a late stamp
    cannot repair the damage:** `RepairArmedLineage` clears the adherence grade
    for regrading only when it is `"F"` (reconcile.go:588), while a close with
    no citation grades `"D"` (adherence.go:52), so positions 584 and 586 now
    carry full lineage and a permanent off-plan D. Quantified: **4 closed rows are
    PROVABLY mis-graded** (575, 584, 586, 591 — `plan_matched=1`,
    `plan_band=armed_fill`, grade D), because a cited+matched close grades base
    A and two penalties reach only C, so D can only mean it was graded while
    `Cited` was false. Two more Ds are legitimate and were nearly miscounted:
    582 has `matched=0` → base C − 1 penalty, and 530 cites the literal sentinel
    `off-plan`. **And the predicate is not impossible, it is partial:** base D
    steps to F under either penalty (`InNoTrade`, `!InKillzone`), so the repair
    silently SUCCEEDS on penalised uncited rows (566, 571 → F) and silently
    FAILS on clean ones (580 → D) — which is why it survived, since a
    spot-check lands on a working case. Fix keys on the ABSENCE OF LINEAGE, never
    on a letter that encodes lineage plus two unrelated penalties; widening it to
    "D" would promote 580, which deserves its D. Whether to backfill the 4 is an
    owner call — a silent backfill moves a published distribution. **The
    discriminator needs a lineage clause:** the ladder argument alone
    (`plan_matched=1 AND plan_band NOT IN ('off_band','struct') AND grade='D'`)
    returns FIVE, catching 572 — an `e7_farside_test` seam row with
    `cited_scenario_id 'TEST-E7'` and `plan_version 0`. Same test-seam
    contamination that §D-3 of the 09-01 audit found in
    `store/position_query.go`'s unfiltered counts, recurring in a new query. All
    four real rows are `source=reconcile`, stated as absence of a counter-example
    rather than proof: the single `armed_entry` D row (582, `plan_matched=0`) is
    observationally identical under both hypotheses — base C minus a penalty and
    base D from a grade-before-stamp are both D — so the armed path is untested,
    not exonerated. Probes: ask which BRANCH a
    write sits on; when a log line names a CAUSE, check the code can
    distinguish it from the alternatives; and when a repair path clears a value
    to trigger a recompute, check it matches the value the broken path actually
    writes.

61. **A mode that existed only in a comment.** (Highest occupied at merge: 57.)
    Root cause: `plan_mode` was documented as `advisory | direction | strict`
    in a doc comment (`store/strategy.go:919`) and offered in the Studio
    selector, but **`strict` was never implemented**: `PlanModeFor` returned a
    saved `"strict"` unchanged (no self-heal, and no `"normal"` mode ever
    existed), and **no consumer in non-test code compared against it** — the
    only mode any consumer tested was `direction`. An owner could select it,
    the value would persist, resolve, and render, and nothing anywhere would
    behave differently. The Studio audit reached it from the other side, listing
    it as a dead option to REMOVE. **Probe:** for every enum value a UI offers,
    grep for a consumer that compares against that specific value — a value
    present in a doc comment, a schema and a selector but absent from every
    comparison is a control wired to nothing. Do the same for every mode named
    in a comment listing alternatives. **Fix:** `strict` was **documented,
    never implemented; first implementation 2026-09-03 by owner ruling** — it is
    a NEW GATE, not a restoration, and it is recorded as such because the
    dispatch that ordered it believed it was reviving deprecated behaviour.
    Semantics: only plan scenarios execute · arm path only · decision-path
    market entries refused · direction must equal the cited scenario's ·
    refusals logged `refused: strict`. Implemented as leg 0 of the ONE
    EntryGate (class 48) so its refusal is the one the journal shows, with pins
    in both directions (refuses a decision-path open; allows an arm whose side
    matches its scenario) and a pin that `advisory`/`direction` are unchanged.
    **Law:** an option a user can select is a promise; either a consumer
    compares against it or it does not appear in the selector. A comment
    listing modes is not an implementation, and "deprecated" and "never built"
    are different findings that call for different fixes.

60. **A gate signal that is time-of-day dependent — green at 11:00, red at
    14:50, and honest both times.** (Highest occupied at merge: 59.)
    `TestMaybeWakePlannerOnLevelEventsThrottleDedupe` injects a fixed clock into
    its FIXTURE (bars anchored 2026-08-25 10:00 CT) but called the production
    entry point, which reads `time.Now()`. That was harmless for as long as the
    class-47 cadence cutoffs only WARNED. The hour they began ENFORCING, the
    last-window cutoff started asking how many minutes remain to the session
    flat — so the code path's behaviour became a function of the wall clock, and
    the test with it. At 10:20 CT there were 265 minutes to NY's 14:45 flat and
    the wake proceeded; at 14:44 there was 1 minute, `SkipForCutoff` fired, no
    candidate was recorded, and the first assertion failed on an empty key. The
    deploy lane certified "27 ok / 0 FAIL" at ~11:00 and shipped; the identical
    command failed the same afternoon. **Neither reading was wrong.** That is
    what makes it worse than a flake: it is reproducible in ONE DIRECTION, so
    re-running never surfaces it, and a suite verified in the morning is not the
    suite you are deploying after lunch. Two sessions also mis-diagnosed it —
    one blamed the fixture's bar dates, the other reasoned from a stale
    timestamp read off an earlier command and believed it was 11:30 when it was
    14:47. **Probe:** for every gate or guard promoted from WARN to ENFORCE, ask
    what NEW inputs its verdict now depends on, and grep the suite for tests
    that reach that path through a wall-clock entry point with a fixed fixture —
    they will pass all morning. For any suite used as a deploy gate, ask whether
    a run at 11:00 and a run at 15:00 can disagree; if they can, the gate's
    freshness is part of its result. **Fix:** the clock seam — the entry point
    keeps `time.Now()` and delegates to an `…At(now, …)` variant; production is
    unchanged and the test states its own clock instead of borrowing the
    machine's. Applied to `maybeWakePlannerOnLevelEvents`; the comment records
    WHY the clock became load-bearing, so the next reader does not re-derive it
    at 14:44. **Law:** when a guard starts enforcing, every clock it consults
    becomes load-bearing — and a green suite is only evidence about the moment
    it ran.

    **W7 recurrence (2026-09-08, scenario-economics cutover):** the old
    consumed-level fixture straddled the 17:00 CME day; at 17:18–17:21 its
    completed-day range excluded the test level from the activation window.
    Reproduced 3/3 on running source as well as the merged candidate: pre-existing.
    Owner-authorized fix: recordLevelState delegates once to recordLevelStateAt(now),
    registered in clock-seams.list; eight fixed clocks and an explicit in-window
    fixture preserve all consumed/re-arm/retouch assertions. Predicate body and
    nine golden files are unchanged apart from clock injection. Evidence:
    reports/2026-09-08-scenario-economics.md, class-60 correction section.

    **OWNER LAW (2026-09-03), added at merge.** `time.Now()` lives ONLY at the
    entry point; everything underneath takes an explicit `…At(now, …)` variant,
    and tests state their own clock. **And the suite runs at MERGE TIME,
    immediately before the build — never hours before.** Both of today's boots
    were verified in the morning and shipped in the afternoon; the readings were
    honest when taken and were not green at deploy time. A gate whose answer
    depends on when you asked is not a gate.
    **SWEEP — CLOSED by the clock-seam wave (class 72), 2026-09-03.** Listed here, fixed there: eight test files
    build a fixed `time.Date` clock and call an entry point that reads
    `time.Now()` — `auto_trader_wake_levels_test.go` (FIXED here),
    `auto_trader_reset_test.go`, `auto_trader_transition_test.go`,
    `auto_trader_weekly_test.go`, `auto_trader_clock_test.go`,
    `kernel/risk_limits_test.go`, `kernel/tz_test.go`,
    `trader/binance/order_sync_test.go`. Only the wake path currently consults
    the enforcing cadence inputs (`minutesToSessionFlat` / `SkipForCutoff` /
    `SkipForCooldown` appear in exactly two production files), so the blast
    radius today is one path — but every future guard promoted from WARN to
    ENFORCE puts its own inputs into this class, which is why the probe is
    phrased around the PROMOTION and not around this test.
    **Failure window, measured:** 14:20–14:45 CT — from the 25-minute cutoff to
    NY's flat. Before and after it, the test passes, which is why it read as a
    flake rather than a bomb.

62. **A stored column that reads as money and is not.** (Number assigned at
    merge; highest occupied at authoring: 61.) Found building wave 1D, from the
    FIRST live read of the new table: the E8 counterfactual side-table produced
    cell means of **−29,926, −29,210 and −32,893** on an instrument trading near
    29,900. `ab_confirm_log.net_pnl` is not a P&L on those rows — it is
    approximately **−(entry price × multiplier)**, i.e. the exit was treated as
    zero. Measured over the whole table: **188 rows — 40 price-scale, 92 a bare
    `0` beside a RESOLVED outcome (win/loss/target/stop), 56 usable.** Only 30%
    of the column is arithmetic-able, and nothing said so: the column is REAL,
    typed, populated, and named like money, so every consumer that averages it
    publishes a number with a currency symbol and no meaning. This is distinct
    from the known E8 short-side sign defect — that one corrupts a subset's
    sign; this one makes the magnitude a price.
    **Probe:** for any stored numeric column a report will average, compare its
    magnitude against a quantity it can never legitimately reach. For a P&L on a
    1–2 lot instrument, `|value| >= entry_price` is impossible; for a rate,
    anything outside [0,1]. Then count the bare zeros beside a resolved outcome
    separately — a zero that means "uncomputed" is not a zero that means
    "break-even", and only the second one may enter a mean.
    **Law:** a column that is populated is not a column that is computed. Before
    a read model averages a stored number, it must state which rows it is
    willing to arithmetic on and COUNT the ones it refused — and when nothing is
    usable it must return ABSENT, never 0. 1D REPORTS this; it does not repair
    it. The writer (`kernel/shadow_ab.go`) is out of the wave's footprint, and a
    read model that silently patched its input would hide the defect it found.

63. **Two true numbers that mislead when read together.** (Number assigned at
    merge.) Also found on 1D's first live read. The boot line said
    `cells=41 with_n>=30=0` — both true: no cell of the five-dimensional table
    reaches the sample floor. At the same instant the `reject` CONDITION
    roll-up stood at **n=31 and was judged FAILS**. The verdicts are made on the
    roll-ups, not on the five-dimensional cells, so a line reporting only cells
    reads as "nothing is judgeable yet" while a verdict exists. No number was
    wrong; the omission was.
    **Probe:** when a summary line reports a count over one projection of a
    model that is also consumed through another projection, ask what a reader
    would conclude from the line alone, and whether that conclusion is true.
    **Law:** a boot line is READ, and being read means it is a claim. Two
    honest numbers whose juxtaposition implies a false third are a defect in the
    line, not in the reader. Fixed by counting judged roll-ups as their own
    field (`judged_rollups=`), pinned by a test whose fixture makes every
    five-dimensional cell sub-floor while three roll-ups clear the floor.

65. **A test harness scoring in a production table.** (Renumbered 62→65 AT
    MERGE, 2026-09-03 — 62 and 63 were taken by wave 1D and 64 by the detector
    wave, all merged first. Highest occupied at this merge: 64.) Root cause: position 572 is an `ARMED_TEST_SEAM` experiment run
    against the live wire. It sat in the graded population holding a D, and when
    the 2026-09-03 15:02 boot recomputed adherence with repaired lineage, W5
    promoted it to an **A** — a harness outscoring most real trades and counting
    toward the plan-adherence rate. Nothing was broken; the grader did exactly
    its job on a row that should never have been offered to it. Discovered only
    because a migration's before/after distribution was printed and a grade
    moved that nobody had asked to move. **Probe:** for every production
    aggregate, ask which rows are SYNTHETIC and name the predicate that removes
    them; then check the predicate is applied at the WRITE, not only at each
    reader — a reader-side filter is one forgotten query away from being wrong.
    Ask the same of anything that RECOMPUTES: a value can enter an aggregate
    long after the row was created. **Fix:** `IsSeamSource` (the known seam plus
    a naming convention, so a future harness need not be remembered in five
    places); `SetAdherence` REFUSES a grade on a seam row and stamps
    `"excluded: test seam"` on the row itself, so the reason lives in the data
    and not only in a log line; W5 skips the grader entirely for seam rows
    (defence in depth, not the only guard); the distribution excludes them and
    RETURNS THE COUNT; a boot line reports `adherence: seam rows excluded=<n>`
    read from the table. One superseded fixture migrated with its reason: a seam
    row can no longer "keep its D", which is a stronger outcome than the
    assertion it replaced. **Law:** synthetic rows are excluded at the write,
    counted where they are excluded, and stamped with the reason in the row —
    a test that can score is a test that will eventually be quoted as evidence.

66. **Two price spaces subtracted from each other — and the repair that would
    have dressed the wreckage as repaired.** (Renumbered 63→64→66 AT MERGE,
    combined boot 4. 63 went to the two-true-numbers class, then 64 collided
    with the detector class and 65 with the seam class — both allocated by
    nofx-47 and nofx-ed. Highest occupied at merge: 65.)
    `shadow_ab.go` mirrored stop/target/ref into a NEGATIVE price space for
    shorts so that "MFE is always favorable-positive in the replay". The
    close-rule fill returned the REAL close and `row.StopPx/TargetPx` were
    stored REAL, so every downstream number subtracted one space from the other:
    `risk := FillPx − stop` → `29204.50 − (−29226.00)` = **58 430.50**, and
    `RR = (target − FillPx)/risk` = **−0.9984**. Measured with direction read
    from the PLAN and never inferred (all 188 rows resolve): **121 short rows,
    109 with RR < −0.9, 46 with |MAE| > 1000; all 67 long rows clean.** The
    same mirror poisoned `net_pnl`: 40 rows below −1000 because the exit was the
    MIRRORED target — `(−29418.62 − 29413.00) × 2 = −117 664`, i.e.
    `−(target+fill)×pv`, not "exit treated as zero". **And it broke more than
    arithmetic:** `above` was flipped AND `ref` negated, so the short close-rule
    became `b.Close > −ref`, true for every bar — the FILL BAR was wrong too.
    **Probe:** wherever a sign convention is imposed "so the replay reads
    nicely", find every value that ENTERS after the conversion and every value
    that LEAVES before it — a mirror is only safe if nothing crosses it. Count
    the rows the two spaces would make impossible (a distance larger than the
    bracket that contains it, an RR pinned near −1) and you have the blast
    radius without reading any code. **Fix:** no mirroring; risk and reward are
    distances, always positive, and which side of the fill each sits on is the
    direction's business. **And the repair's own lesson:** the first backfill
    re-derived arithmetic for all 109 rows and produced 23 negative RRs and 14
    impossible MAEs still labelled "recomputed" — a precise answer about the
    wrong moment. The shipped version refuses them: a short whose stored stop
    sits BELOW its fill is geometrically impossible, so its fill came from the
    wrong bar and no arithmetic on it can be trusted. Three states, not two —
    `recomputed` **55** (all clean), `unrecomputable:fill-bar` **54**,
    `unrecomputable:no-inputs` **12** — and the side-table's honest `usable`
    count is 55. **Law:** a repair that cannot distinguish "fixed" from
    "recomputed from poison" has not repaired anything — and a number nobody can
    act on must be labelled, never averaged.


64. **An instrument that could not have reported otherwise.** (Highest
    occupied at merge: 63.) Root cause: two production detectors whose
    predicates guaranteed their own answers. `touch_telemetry.go` called a touch
    a REJECTION when the close was still on the side it approached from — true
    ≈69% of the time on a driftless walk BY CONSTRUCTION. `level_stats_calc.go`
    counted ANY ≥reactPts move away from the level, either side, so a
    blast-through scored identically to a rejection. Three touch geometries
    coexisted. Every reaction rate ever published from them — 84%, 70.3%, 75.1%
    — is an artifact of the predicate, not a property of the tape, and each was
    quoted as evidence for trading decisions. **Probe:** for every rate an
    instrument reports, simulate its predicate on IID-SHUFFLED tape of the same
    instrument's own scale. If the answer is not ≈0.50, the instrument has an
    opinion built into it; a detector that cannot be wrong is not measuring.
    Then ask whether the geometry is symmetric: barriers anchored on the level
    at ±k·Δ make a driftless walk a gambler's-ruin coin flip, barriers anchored
    anywhere else do not. **Fix:** D1′ — one detector, ported line-for-line from
    the reference so a parity fixture is byte-equal (151 episodes), calibrated
    to p(hold)=0.4988 [0.4894, 0.5083] on IID-shuffled REAL MNQ tape at k=3,
    exit_on=close, H=12; ambiguous episodes RECORDED, counted and excluded from
    the rate; `touch_outcomes` writes one row per episode with the scope it was
    judged under; `candidate_pool` records every level the constructor produced
    INCLUDING the cut ones with their propensities, because the selection
    question is off-policy and cannot be answered from the seated levels alone;
    both legacy verdicts carry a retirement notice and no surface renders a rate
    from them. **Two synthetic calibration tapes lied before the real one:** an
    Ornstein-Uhlenbeck tape read 0.5221 (hold-biased by construction, since it
    pulls price back to the level) and a pure random walk at one median level
    read 0.4573 on n=2,445 (underpowered and selection-skewed). The byte-equal
    parity with the reference is what proved the fixture wrong rather than the
    detector. **Law:** calibrate every rate-producing instrument against noise
    of its own scale before trusting one number it emits; a fixture that
    disagrees with a verified port is the fixture's error; and a rate ships with
    its n, its interval and its excluded count or it does not ship.

67. **A gate leg that asks our own bookkeeping and calls the answer the
    broker's.** (Number assigned at merge; 66 was taken on dev by the
    two-price-spaces class at combined boot 4, and my own UI wave claims 66 on
    an unmerged branch — that one needs renumbering before it lands.) The flat
    gate had five legs and four of them asked NT8. Leg 4 — working orders — asked
    `armed_orders`, our own ledger, because the AddOn emitted position, fill,
    order_update, bar and account frames and nothing that said what was RESTING
    at the broker. The leg even said so in its own source string: *"armed_orders
    ledger (no NT8 order frame — F12 open)"*. A leg answered by our own
    bookkeeping cannot detect the one failure a flat gate exists to detect — the
    ledger and the broker disagreeing — and it had passed at every cutover since
    35 on exactly that basis.
    The consequence was not theoretical: on 2026-09-02 the 0B cutover waived flat
    with position 588 open, and **the resting stop could not be verified**,
    because nothing on the wire could say whether it was there. The standing rule
    that followed ("no override with a position open") was the right answer to an
    unanswerable question — a blanket refusal standing in for a check that had no
    data.
    **Probe:** for every gate leg, name the SYSTEM that answers it, not the
    function that computes it. If the answer comes from state we ourselves wrote,
    the leg cannot fail in the case where our state is wrong — which is the case
    the gate is for. Ask separately: what does this leg do when its source is
    silent? "Passes" is the wrong answer, and so is "fails" if silence is the
    normal state during a rollout.
    **Law:** a gate leg must be answered by the system it is asserting about. When
    it cannot be, the fallback is permitted but must be NAMED in the leg's own
    source string on every read — the rule is *never a SILENT fallback*, not
    *never a fallback*. And a blanket refusal standing in for a missing
    measurement should be recorded as a MEASUREMENT DEBT, not as policy: it looks
    like caution and behaves like a permanent no.

68. **A production surface with no production path.** (Number assigned at
    merge, A16. Highest occupied on dev at merge was 66, the data-integrity
    wave's; 67 is taken by the F12 order-snapshot wave merged in the same boot —
    so this takes 68. The branch was authored claiming 66, which had been free
    when it was written and was not by the time it merged.)
    Found during the 1D cutover, incidentally: the **entire operator UI** was
    served by a Vite DEVELOPMENT server on :3000, started by hand, supervised by
    nothing. The Go server registered **no static route at all** — `:8080/`
    returned 404 — and `web/dist` on disk had been **stale since 2026-08-31**
    while binaries shipped on 09-01, 09-02 and 09-03. Nothing was broken, which
    is why nobody noticed: the owner's browser pointed at :3000 and :3000 always
    worked, because the process happened never to have died. A reboot, an OOM
    kill, or a closed terminal would have taken the whole interface with it and
    left a live trading bot with no operator surface.
    **Probe:** for every surface a human depends on, ask what process serves it,
    what restarts that process, and what the last built artifact's timestamp is.
    "It is up right now" answers none of those. Specifically: does the artifact
    on disk predate the binary that is running?
    **Law:** a dev server is not a production path, and supervising one does not
    make it one — it makes an unsupervised dev server a supervised one, with the
    minification, memory and websocket behaviour unchanged. A surface that has no
    owner process, no restart story, and no freshness check is unowned no matter
    how long it has stayed up. **Fixed by:** the bot serves its own UI
    (`api/ui_serving.go`), and the boot line
    `🖥 ui: served-by=go-static build=<ts>` is READ from the bundle and logged as
    a WARNING when the bundle predates the binary — the 08-31-dist-under-a-09-03-
    binary state becomes impossible to miss instead of invisible.
69. **Reported wired, called by nobody.** (Highest occupied at merge: 68; renumbered
    69 on the rebase when another lane landed 67-68 — theirs untouched, per A16.)
    Root cause: code that ANNOUNCES its own wiring while no production caller
    exists, so every downstream consumer reads an empty store as a fact.
    `trader/detector_record.go` carried the banner "1B — THE PRODUCTION CALL
    PATH … Called once per planner read" from `89aeb8be`; `recordDetectorOutputs`
    had **0 production call sites**. `touch_outcomes` and `candidate_pool`
    therefore booted `0 · 0` on every boot including the running `4d846e26`,
    and the boot line printed those zeros as if they were measurements. A test
    named `TestDetectorWritesThroughTheProductionPath` passed throughout —
    because it called the hook directly. Three instances in 24h: the seam
    migration, this hook, and `ab_confirm_log`'s "usable" count (healthy-looking
    because what it dropped was never counted). **Probe:** for any store a
    decision depends on, ask what WROTE the last row, not whether the writer
    exists — `grep` the call sites of the writer and require ≥1 outside
    `_test.go`; and never let a test that calls a hook stand in for a test that
    drives the path. An empty store is UNKNOWN, never "no change". **Fix:**
    wired at the one intended site beside the void scope
    (`auto_trader_planner.go`, so the detector judges the same resolved tape the
    prompt and validator read); a production-path test that drives
    `assemblePlannerInputWithCtx` on a fixture and asserts both stores non-empty
    (RED with the call site removed — the old test stayed GREEN, which is the
    whole point); and a STANDING GATE, `TestEveryClaimedProductionPathHasACallSite`,
    which parses every non-test .go file, collects wiring claims from BOTH
    function docs and FILE-LEVEL banners (1B's claim was a file banner, so a
    func-doc-only scan would have missed the very case it exists for), and
    FAILS on 0 production call sites. **Law:** a claim of being wired is a
    testable assertion, not a comment — if a file says it is a production call
    path, a gate must be able to prove it wrong.



70. **A lock that answers the wrong question.** (Number assigned at merge,
    A16 — highest occupied on dev was 69, the reported-wired-called-by-nobody
    class from the wake-predicate boot.) Root cause: `~/nofx-main.lock` was ONE
    FLAT FILE, written with `>`, carrying a **pid**, and read with `kill -0`. It
    failed in three directions on 2026-09-03 alone, and no single failure was
    caught by the file — every one was caught by a peer asking:
    (a) **dead pid, live owner** — agents wrote `pid=$$`, but every tool call is
    a fresh shell, so the recorded pid was dead within a second while its owner
    worked on; a peer found an unexpired lock whose pid did not resolve and
    correctly did NOT clear it;
    (b) **live pid, silently replaced** — `>` truncates, so a second acquirer
    clobbered an active cutover's lock with no error and no trace, last writer
    winning in silence;
    (c) **stale pid after resume** — a session resumed under a new pid and wrote
    a lock naming its own former, now-dead process.
    The deeper fault under all three: a pid answers *"does some process exist"*,
    which was never the question. The question is *"is the owner still
    working"*, and only the owner can answer it. **Probe:** try to acquire a
    held lock — if it succeeds, the lock is a suggestion, not a lock; then read
    the lock and grep it for a pid. **Law:** the lock is an ATOMIC CREATE
    (`mkdir ~/nofx-main.lock.d`, which fails if it exists, so (b) cannot be
    represented) recording **session · task · acquired · expiry · heartbeat**
    and **no pid field at all**. Liveness is the heartbeat, rewritten by the
    holder every 2 min; older than 5 min reads **STALE**, and STALE IS NOT
    DEAD — the surface never prints "dead" and never self-clears. Corroboration
    stays mandatory before clearing: ask the named session, watch whether HEAD
    moves, look for a build in flight. Fixed in `deploy/nofx-lock.sh`
    (`acquire`/`heartbeat`/`release`/`status`/`check`/`with-heartbeat`), pinned
    by `deploy/nofx-lock-test.sh` — 56 assertions including a second acquire
    refusing, a stale heartbeat never reading "dead", and a source pin that the
    script cannot express `kill -0`, `pgrep` or `$$`. `with-heartbeat` beats
    **`acquire` now STARTS A KEEPER (2026-09-09)** that beats for you until the
    window you declared, then stops — because the message had promised
    "heartbeat every 120s" and started nothing, so a holder who simply WAITED
    (for a position to close, for an owner to run the kill) went STALE at 300s
    with no writer in existence, and `status` printed the reclaim recipe over a
    live cutover. **The keeper never extends the window:** need longer,
    re-acquire or extend explicitly. Its pid is a STOP HANDLE in `keeper.pid`,
    never in `meta` and never consulted as liveness — that is still the
    heartbeat alone (class 70). `status` reports `auto-beat: on/off` from the
    file, never by probing a process, which is what finally tells "the holder is
    gone" apart from "the tool never beat for a holder who was waiting".
    only for the lifetime of the command it wraps: a beater that outlived its
    job would reinvent the pid problem in a new costume. **Succession is on the
    record:** `reclaim <new> <stale> "<corroboration>"` is refused while the
    heartbeat is fresh (a reclaim that can take a live lock is replacement with
    better manners), requires the caller to name the holder it is taking over
    and what it checked, appends who/from-whom/when/why to a `history` file
    printed by `status` and again at `release`, and returns **rc 3** rather than
    acquire's 0 so a lane can tell "took a free lock" from "inherited an
    abandoned one" and refuse to inherit. **Provenance hazard found the same
    day:** every commit in this repo carries the identical author identity, so
    git cannot answer "which lane wrote this" — provenance must come from the
    branch, the worktree and the timestamp, and a lane's work can otherwise be
    absorbed under another lane's name. That is what PART 3 step 0 exists to
    prevent.
    **SECOND-ORDER HAZARD THIS WAVE CREATED, found by nofx-ed.** Changing the
    lock changed `docs/superpowers/plans/2026-09-02-tree-guard-spec.md`, whose
    expected-dirty rule the tree-guard wave was implementing AT THE SAME TIME,
    from a worktree cut before the change. They built the old model —
    `~/nofx-main.lock`, a pid, `kill -0` — and under the new lock there is no
    legacy file, so during a cutover that guard would have found "no live
    holder", seen a legitimately dirty tree, and **ALARMED at exactly the moment
    it is meant to be trusted**, while running and printing normally. A guard
    that cries wolf on every deploy is worse than no guard: the next real alarm
    is the one everyone scrolls past. Fixed at `ac345a7a` — the lock DIRECTORY
    is authoritative, and the legacy file is surfaced but NEVER honoured for
    liveness, since honouring it would restore the `kill -0` test this class
    removed. **Law:** a spec on `dev` is a MOVING artifact, and a worktree cut
    from an older base silently freezes it. **Probe:** before building against a
    spec, `git log -1 -- <spec>` and compare against your worktree's base; if it
    moved after you branched, re-read it. Same family as this class — a value
    read once, at a moment nobody recorded.

71. **Global state that no worktree owns — the stash stack.** (Number assigned
    at merge, A16 — highest occupied on dev was 70. Found by nofx-47 on
    2026-09-03, by accident, while isolating a test result.) Root cause: `git
    stash` is per-REPOSITORY, not per-worktree. This repo has **56 worktrees**,
    the main tree among them, and a plain `git stash pop` in ANY of them applies
    whatever is on top — regardless of which lane parked it, which branch it was
    taken from, or how old it is. `stash@{0}` was
    `6b770196` "On dev: class45-found-revert-1203" (2026-09-02 12:03:34): the
    preserved evidence of the class-45 VS Code stale-buffer revert, **127
    insertions / 596 deletions** of shipped safety code across six files.
    nofx-47 popped it into an unrelated worktree by routine stash/pop; three
    files conflicted because dev had moved, and **three applied CLEANLY and
    staged**, deleting among other things:

    ```
    -	// CLASS 33 (2026-09-02) — BOOT SWEEP FIRST. Before ANY authoring, gating,
    -	at.sweepPreBootArms(ledger)
    ```

    A lane doing that mid-wave and committing without reading the diff re-ships
    the exact deletion class 45 exists to prevent — this time with no editor to
    blame. **WORKTREE LAW and the class-70 lock do not cover it:** both govern
    branches and directories, and the stash stack is neither. **Probe:** `git
    stash list` before ANY stash operation, and never `pop` blind — `git stash
    show -p stash@{0}` first, and prefer `git stash push -m` + `apply` of a
    named entry over `pop` of whatever is on top. **Law:** evidence is never
    parked on the stash stack. It is preserved as a file and a tag, both of
    which are inert, and never as a poppable entry that every worktree shares.
    Disarmed here without destroying anything. **The annotated tag
    `class45-found-revert-1203` is what makes a drop lossless** — it references
    `6b770196`, so dropping the stack entry cannot let gc collect the object.
    The human-readable copy is
    `docs/superpowers/reports/class45-found-revert-1203.patch.txt`, a
    convenience and not the guarantee; it carries a `.txt` suffix because
    `.gitignore:143` is `*.patch`, which silently swallowed the first attempt to
    commit it — `git add -A` skipped it with no error and the file was reported
    as landed when it had not (a second instance of this checklist's own
    never-claim-an-unverified-state rule, caught by nofx-47 reading dev rather
    than reading my report). **DROPPED 2026-09-03 on explicit owner
    authorisation**, after a four-point pre-flight (one entry on the stack · its
    sha IS `6b770196` · the local AND origin tags both dereference to it · the
    evidence file present on `origin/dev`). `git stash list` is empty and
    `git cat-file -t 6b770196` still answers `commit`, reachable through the
    tag — the landmine is gone and the evidence is not.

72. **A flaky test in the gate — and the flake was the same clock.** (Number
    assigned at merge, A16 — highest occupied on dev was 71.) Root cause:
    `TestFanOutClosesLastResortIsHonest` failed roughly 1 full-suite run in 4-6
    and passed in isolation every time, so it was carried for days as "a load
    flake" — including by me, in this checklist. It was not load. The drop path
    increments `persistDropped` / `persistDroppedCloses` and then calls
    `barPersistSummary()`, which is rate-limited to once per 60 **wall-clock**
    seconds and, when it fires, `Swap(0)`s both counters — erasing the increment
    one line before the test reads it. At ~6.3 s per iteration, roughly every
    tenth run crossed a 60-second boundary and read 0. **Measured, and the
    numbers are what identified it:** failures returned in **6.01 s** with
    `closes_dropped=0 AND queue_drops=0` — neither branch's counter survived,
    which is impossible for either code path — while passes took **6.30 s** with
    both at 1. Two earlier hypotheses were wrong and both were "obviously"
    right: a stale queue from the previous `-count` iteration, and the worker
    freeing a slot mid-retry. Fixing them made the test no better; the queue read
    4096/4096 in every run, pass and fail alike. **Probe:** a test that fails at
    a stable RATE rather than randomly is a clock, not a race — measure the
    period. Grep any assertion on a counter for a destructive reader
    (`Swap`, `Store(0)`) on the same counter's path. **Law:** a gate with a
    flaky test is a gate you learn to re-run, and then it is not a gate. Fixed
    deterministically, test-side only: the queue is filled until it is
    OBSERVABLY at capacity instead of by a magic count, the worker is confirmed
    wedged inside the persister before the assertion begins, and the summary's
    rate limiter is held open across the window so the destructive reset cannot
    fire. **Zero production behaviour change** — `barPersistSummary` gained a
    clock seam and still summarises on its own schedule in the bot. `-race
    -count=30` clean. **The flake was itself a class-60 instance**, which is why
    the two halves of this wave belong together: the sweep went looking for
    wall-clock rules and found one hiding behind a counter.

73. **A hook registered one start too late.** (Renumbered 69→70 on its branch, then 70→73 at THIS merge — 70/71/72 were
    taken by the lock, stash-stack and flaky-clock classes landing in the same
    boot. A27: numbers are assigned AT MERGE. Original branch note: 69
    landed on dev first as "Reported wired, called by nobody". **Read that one
    with this one — they are the same family**, a registration whose success
    nobody verified. 69 is a hook nothing ever called; 70 is a hook installed
    after the thing that reads it had already read it. Neither could fail a test
    of the hook's own logic.)
    F12 wired a sink so every received `order_snapshot` frame would be persisted
    for forensics. The sink was correct, the store was correct, the tests were
    green — and `nt8_order_snapshots` held **0 rows** while frames arrived every
    30 s and the cutover gate correctly read the broker off those same frames.
    `main.go` registered the sink at line 366. `LoadTradersFromStore` at line 203
    builds the first trader, which lazily starts the TCP server, which reads the
    hook **exactly once, at start**. The server came up with a nil sink; the
    registration then ran 163 lines later and set a variable nothing would read
    again.
    **Nothing looked wrong**, and that is the defect. The CACHE is what the gate
    reads, by design, so the feature that mattered kept working. The missing half
    produced no error, no warning and no empty-state message — just a table that
    was always empty, which is indistinguishable from a table nothing has
    happened for yet.
    **Probe:** for any hook, callback, sink or observer, find the line that READS
    it and ask whether the registration is guaranteed to have run first. Lazy
    singletons hide this: the read happens inside whatever call first needs the
    subsystem, which is rarely near the registration. Then ask the harder
    question — *if this hook were never installed, what would I see?* If the
    answer is "nothing", the wiring needs its own test, because no test of the
    hook's logic can fail on it. (69's probe is the sibling: ask who CALLS it.)
    **Law:** a hook is not wired by being written. It is wired by being installed
    BEFORE the thing that reads it starts, and only a test that starts that thing
    can prove it. This is the write-side twin of "parity tests must exercise
    production call sites": the existing tests called the sink directly and could
    never see the order. Pinned by a test that starts a real server, drives a real
    frame through a real client and asserts a row lands — and by a source-order
    guard that fails if the two calls are swapped back (verified RED at offsets
    17584 > 8148, GREEN after).

74. **A swap that failed and a kill that succeeded — the null cutover.** (Number
    assigned at merge, A16 — highest occupied on dev was 73. Found by nofx-47
    during combined boot 7, 2026-09-03.) Root cause: `cp nofx-bin.next nofx-bin`
    against a RUNNING binary fails with **`Text file busy`** — the inode is
    executing. The kill sat in the same command block and had already fired, so
    systemd relaunched the **OLD** binary while the operator's screen showed a
    completed deploy. Nothing about the sequence looked wrong: a kill that works,
    a process that comes back, a version that never moved.
    **What caught it was RELEASE-before-the-kill**, and only that:

    ```
    23:11:59 [ERRO] 🔐 TRADING REFUSED — binary is revision "89673ccc5984"
                     but the intended release is "530009ff" — a stale binary is running
    23:12:55 [INFO] 🔐 BOOT INTEGRITY OK — rev 530009ffd540 · goldens PASS
    ```

    Had RELEASE been written after the boot, the stale binary would have booted,
    agreed with itself, and traded — for however long it took someone to notice a
    revision that had not moved. **The sharp part: PART 3 step 6 ALREADY SAID
    `mv`.** This is not a gap in the procedure, it is a deviation from it whose
    failure mode is silent enough to read as success. `mv` replaces the directory
    entry and works on a busy inode; `cp` writes THROUGH it and cannot.
    **Probe:** after any swap, verify `go version -m nofx-bin` BEFORE the kill,
    not after — and never put the swap and the kill in one block, because a
    `&&` chain hides which half failed and a `;` chain runs the kill anyway.
    **Law:** swap with `mv`, verify the swapped artifact, then kill — three
    steps, three exit codes, never one block. A19's RELEASE-before-the-kill is
    what makes the guard able to speak; this class is why it is not optional.
    **Family: "a healthy-looking absence"** (PART 3 step 0) — with 76, and with
    the lost dispatch that has no claim branch.

75. **The build directory is in every log line.** (Found 2026-09-03 in boot 7's
    output; going into the tree guard as its own check.) Root cause: Go embeds
    the **build directory name** in source paths, so a binary built from a clean
    clone in a directory called `cleanclone/` logs `cleanclone/main.go:291`
    where every previous binary logged `nofx/main.go:291`. Measured on the
    running binary: **41 lines carrying the `cleanclone/` prefix against 35
    carrying `nofx/`** in the same tail. Behaviourally harmless and completely
    invasive for anything that reads logs by path — greps, alerts, the
    journald filters, and every runbook that says "look for `nofx/main.go`".
    **Probe:** after a clean-clone build, grep one log line for the module
    directory name before shipping. **Law:** the clean clone is named `nofx`.
    A build artifact carries its build path into production, so the build
    directory is part of the deploy, not scratch space.

76. **Canon relocated to where nothing could read it.** (Found 2026-09-03,
    minutes after boot 7; fixed by the same lane at `86a11888`.) Root cause: the
    standing laws lived in a **gitignored** `CLAUDE.md`, which is a real defect —
    a law that cannot travel on a branch cannot be reviewed, guarded, or reach a
    clone. The repair was right and the ORDER was wrong: the live file was turned
    into a two-line `@import` pointer while its target existed only on an
    unmerged branch. For roughly twenty minutes every reader of `CLAUDE.md` got a
    dangling reference **and a comment instructing them to STOP and read a path
    that did not exist** — no laws at all, stated with authority.
    **The guard could not have seen it.** The tree guard's canon check alarms on
    CHANGE; a pointer that is *born* dangling never changes, so a change-only
    check is structurally blind to it. An unchanged pointer to nothing is the
    worst case: stable, silent, empty. Fixed by check **5b**, which resolves
    every `^@import` and alarms naming the unresolved target, pinned by a test
    for the born-dangling case with no baseline. The live file now carries a FULL
    MIRROR with a header saying it is a mirror and not the source, and becomes a
    pointer only once the target is on `dev` — **a stale-but-present copy beats a
    correct-but-empty one.** **Probe:** after any indirection, resolve it from a
    tree that is NOT the one you authored it in. **Law:** never point at a target
    that is not yet on `dev`, and a comment asserting a guarantee ("the guard
    alarms if this changes") is a claim to TEST, not documentation — that comment
    was making a promise the guard did not keep. Class 71's shape with the stakes
    inverted: not an unguarded writer mutating canon, but canon moved beyond
    reach by the lane building the guard for exactly that.
    **Family: "a healthy-looking absence"** (PART 3 step 0) — with 74, and with
    the lost dispatch that has no claim branch.

77. **A counter destroyed by the act of reporting it.** (Number assigned at
    merge, A27 — 74/75/76 were already taken on this base when I checked, two of
    them by my own boot-7 failures written up by another lane, so this took the
    next free number rather than the one I first assumed.)
    `barPersistSummary()` formatted `persistDropped` and `persistDroppedCloses`
    by calling `.Swap(0)` on both, so publishing the number and erasing it were
    the same operation. The log line was therefore the ONLY consumer that could
    ever observe a nonzero value; every other reader was correct or wrong
    depending on whether it happened to run before or after a summary fell due,
    with no error and no trace either way. The summary is rate-limited to once
    per 60 WALL-CLOCK seconds, so the erasure arrived on a schedule unrelated to
    the code under test — which is why `TestFanOutClosesLastResortIsHonest` was
    carried as a **load flake** for weeks, by more than one lane and in this
    checklist. **What identified it was the impossible reading:** the drop path
    increments both counters and the non-close path increments one, so
    `closes_dropped=0 queue_drops=0` cannot be produced by either branch. Zero
    was not a branch outcome; it was a third party having been there first. A
    reading that no branch can produce points at a destructive reader, not at
    the branch taken. **Probe:** for any counter, grep its consumers for `Swap(`
    / `Store(0)` and ask whether the reset is a *side effect of reading*; then
    ask which consumer can ever see a nonzero value — if the answer is "only the
    log line", the counter is not a counter. **Law:** counters RECORD (class 35).
    Reporting reads; it never resets. Where a per-interval figure is wanted,
    measure it against a **reported-baseline** stored beside the counter, and
    make reset a separate explicit verb (`rollPersistCounters`) that nothing
    calls implicitly. A negative delta means an unsynchronised rollover and must
    report zero, never a negative — a negative count reads as a fix. Fixed in
    `provider/ninjatrader/bar_persist.go`; the log message is byte-identical and
    reports the same interval numbers it always did.

78. **A plan that could only trade one direction.** (Number assigned at merge,
    A16 — highest occupied on dev was 76 when this was written; 77 is claimed by
    this lane's own unmerged persist-counter wave, so this took 78. Re-check at
    merge.) With `plan_mode` strict the decision path is closed and ARMS ARE THE
    ONLY ENTRY, so a plan whose bias direction carries no armed scenario cannot
    act on the direction it just argued for. NY 2026-09-03 v7 was biased **long**
    and authored `S1 long breakout_retest` (no arm), `S2 long reclaim` (no arm),
    `S3 short reject` (**armed**): both confirms went true at 11:58 CT and the
    long had no way to reach the market. Long arm-enablement ran 1/23 (4.3%)
    against 8/18 (44.4%) for shorts. **The cause was not planner preference —
    it was vocabulary.** Every long-friendly play the model reached for was
    un-armable: `reclaim` was excluded ("close-confirm first → AI path"),
    `breakout_retest` was excluded by GAR-F4 *and* shadowed, and nothing in the
    prompt said either thing. Measured across 171 stored directional plans, 51
    of 70 longs and 66 of 104 shorts carried no arm in their own bias direction,
    and **19 longs could never have complied** because no play they wrote was
    armable at all. **Probe:** for each bias direction, ask whether ANY condition
    the planner is told to use is both armable and live; then count plans whose
    bias direction has zero armed scenarios. A capability that exists, is wired,
    has its seam ON and has been used ZERO times is not a capability — it is a
    secret (`stop_entry` was built, wired, seam ON, and used 0 times because the
    prompt never named it: built ≠ wired ≠ used, and the third gap is invisible
    to every grep that proves the first two). **Law:** the prompt states the
    armable AND live vocabulary, generated from the same table the validator
    warns from — never hand-listed, so a condition changing status changes the
    prompt with it. Entry TYPE follows the condition, derived by the machine and
    a contradiction refused by name, never silently corrected. A rule that would
    reject two thirds of real traffic ships as a COUNTER first and is promoted by
    a later ruling. Fixed 2026-09-04: `reclaim` armable as a stop-entry (owner's
    word — the gate change that makes longs armable), `ArmKindFor` as the single
    table, `BiasArmWarning` + per-side counts, far-arm counter at 3.0×ATR5m
    ([I] provisional), and the arm ledger made append-only under a live broker
    order.

79. **Silence read as death.** (Number assigned at merge; highest occupied on
    dev at authoring: 78.) The stale-working reaper cancelled any armed row that
    had seen no `order_update` for the stale window. But `order_update` is an
    EVENT frame: a resting limit that nobody touches emits **nothing at all**, so
    after N minutes a perfectly healthy order is byte-for-byte indistinguishable
    from a dead one. The reaper turned an ABSENCE OF EVIDENCE into a cancel — and
    a cancel is not a read, it is an execution against a live order at the broker.
    The failure was not that it reaped too eagerly. It is that the only input it
    had could not answer the question it was asking. No threshold tuning fixes
    that: a longer window delays the wrong answer, it does not make the evidence
    exist.
    **Probe:** for every predicate that concludes something is DEAD, GONE, DONE or
    STALE, ask what the healthy case emits. If the healthy case is also silent,
    the predicate cannot distinguish them and its threshold is decoration. Then
    ask the harder one: **what does this code do when it does not know?** If the
    answer is the same as what it does when the thing is dead, ignorance and death
    are the same branch, and the destructive one wins by default.
    **Law:** silence is not evidence. A destructive action needs a POSITIVE
    observation from the system that owns the fact — here the broker's own book
    (F12's `order_snapshot`), which answers whether or not anything happened.
    Where no such observation exists, the answer is a THIRD verdict — unknown —
    and unknown must do NOTHING. Fixed by giving the reaper three verdicts
    (alive / gone / unknown) where it had a boolean; silence now selects which
    rows to ASK about and never decides the answer.

80. **A guard that cannot guard the rows it was built for.** (Number assigned
    at merge, A16 — highest occupied on dev at authoring: 79. Re-check at merge.)
    WAVE A was dispatched to stop the touch recorder fabricating episodes, on
    evidence that 677 rows are 423 episodes and that all 14 RTH-L episodes opened
    BEFORE the level existed. The specified fix — "the scan starts at
    `max(level.FormedAtMs, watermark)`" — is correct, and implementing it
    verbatim would have produced a guard that **does nothing on the very rows
    that motivated it**: `FormedAtMs` is populated only by
    `kernel/levels_zones.go` (DEMAND/SUPPLY/OB/FVG), while every LINE level is
    built by `lineLevel` (`kernel/levels.go:93-95`), which never sets it — 503 of
    677 live rows (74.3%), INCLUDING ALL 140 RTH-L ROWS THAT WERE THE EVIDENCE.
    The E1 pin passed on the first attempt only because the fixture supplied a
    formation time; production supplies none. **What identified it** was not the
    test — the test was green — but asking, separately from the code, what
    fraction of the live corpus the new predicate could actually reach:
    `SELECT level_kind, COUNT(*) ... GROUP BY level_kind` against the zone/line
    split, which inverts the projection in one query. **Probe:** for every new
    guard, gate or filter, measure the share of the LIVE population its predicate
    can even evaluate, and quote that share with n beside the pass/fail counts. A
    predicate that is structurally NULL on most rows is a silent pass, and a
    silent pass looks exactly like a clean bill of health. **Law:** a guard ships
    with its COVERAGE, measured on live rows, not with its logic alone. Where the
    input is missing, the row is marked as uncertifiable (here
    `validity='unverified:no_formation'`) and EXCLUDED from every rate at one
    chokepoint — never defaulted to the passing value. Corollary for schema: a
    new classification column takes NO SQL default, because SQLite's
    `ADD COLUMN ... DEFAULT 'valid'` stamps every pre-existing contaminated row
    as certified the moment AutoMigrate runs, which is the exact fabrication the
    column exists to prevent. Related: class 49/53 (a plausible zero), class 69
    (built ≠ wired — here, wired ≠ effective).

81. **A send read as a settlement.** (Number assigned at merge, A16 — highest
    occupied on dev at authoring: 80. Re-check at merge.)
    **READ THE COROLLARY FIRST IF YOU CAME HERE FROM SNAPSHOT 1664.** That
    incident was NINE CONCURRENT PLACEMENTS, not nine failed cancels — the
    dispatch that opened this class said the latter and the tape says the
    former (owner-accepted correction, 2026-09-06). The send-as-settlement
    defect below is real and separately evidenced; it is simply not what
    produced 1664. `nt.CancelOrder` is a
    one-line pass-through to `SendCancelOrder`; its error is the result of
    putting a frame on a socket, and it returns non-nil in exactly two cases —
    no client connected, or `WriteFrame` failed. Everything past the socket
    (did the AddOn find the order, did `Account.Cancel` throw, did NT8 accept
    it) returns nil. Five sites in `armed_executor.go` read `cerr == nil` as
    proof the order was gone and wrote the ledger terminal on it, and the sync
    helper wrote `cancelled` even on ACK TIMEOUT with the reason
    "flatten proceeds" — UNKNOWN taking the destructive branch, where
    "cancelled" is destructive because it is the word that unlocks a
    replacement. **Measured cost:** across the ledger's whole lifetime, 47 rows
    reached the broker and exactly THREE cancels were ever confirmed by NT8
    (ids 8, 37, 102). The other 34 are our own assertions — 'cancelled' has been
    ~92% unverified. **What identified it** was not a failure: it was asking, of
    a row that read 'cancelled', which *frame* said so — and finding that the
    question had no answer for 34 of 37 rows. **Probe:** for any state written
    after an outbound call, ask what RECEIVED evidence justifies it; if the
    answer is the call's own return, it is an intention, not an observation.
    Then count how many rows in that state can name their evidence.
    **Law:** a distributed claim is settled by a RECEIVED far-side frame
    (A20/class 6). A cancel is proven by the ORDER'S ABSENCE FROM A FRESH BROKER
    SNAPSHOT — never by a return value, a log line, or a ledger row. The row
    moves to a NON-TERMINAL `cancel_pending` and only a snapshot may finish it,
    recording WHICH snapshot did. A stale or absent book settles nothing and
    promotes nothing.
    **Corollary — THE INCIDENT'S ACTUAL FIX, and the half to build first.** The nine live
    orders of `nt8_order_snapshots` id 1664 were NOT nine failed cancels: NT8
    honoured all 22 cancels for that slot within ~110-260 ms. They were nine
    concurrent PLACEMENTS, and the mechanism is arithmetic — mint every ~2 min
    ÷ retire after 15 min = 7-8 alive. A per-slot invariant (the broker's fresh
    book must show ZERO non-terminal orders for the slot before any placement)
    takes that to 1 and is the load-bearing half. **A guard on one placement
    path is not a guard** — this repo has two.
    Sibling to class 79 (silence is not death) and class 33 (the boot sweep):
    all three are the same shape — an ABSENCE OF EVIDENCE is not evidence of
    absence. Related: class 70 (built ≠ wired), class 49/53 (a plausible zero).

    **Placement-side recurrence (2026-09-07, `fix/placement-truth-0907`):**
    Four sends wrote `working` without receipts. Register `signal_id` and
    `place_pending` atomically BEFORE the wire, so an immediate rejection can
    find the row; never overwrite it when the send returns. Exercise both
    `fill.status=rejected` (h1's pre-submit refusal) and `order_update`; the
    latter alone misses the incident. Distinguish an omitted reason from a
    received reason. Fresh row/queue timestamps do not prove payload freshness:
    seed a 30-minute-old bar through the actual command composer and inspect
    the received payload clock. Run receipt routing with the race detector;
    a listener installed asynchronously is another pre-receipt identity gap.

82. **A green word that answers a narrower question than the reader will
    assume.** (Number assigned at merge, A16 — highest occupied on dev at
    authoring: 81. Re-check at merge.) The dashboard header read
    `SYSTEM_STATUS::ONLINE` in green whenever `/api/health` returned
    `status:"ok"` — which reports that the HTTP process answered a request, and
    nothing else. Not the feed, not the broker link, not any risk control. On
    2026-09-03 it stayed green through 113 minutes of feed silence and 48
    minutes of blindness. An earlier wave had already fixed it once, from a
    STATIC string to a real poll; the poll was honest and the WORD was still
    wrong, which is why the second fix was needed and why the first felt
    sufficient. **What identified it** was not a bug report: it was reading the
    handler and asking what the green state actually excludes. **Probe:** for
    every status word on every surface, write down the question it truly
    answers, then the question a tired reader at 09:30 will think it answers.
    If those differ, the word is the defect — not the plumbing behind it.
    **Law:** a compound question gets a compound answer. Name the parts
    (`process responding · feed <age> · link <state> · book <age>`) or name the
    narrow question (`PROCESS::RESPONDING`). Never one word, never one colour,
    for several independent facts. Corollary, from the same wave: an uncomputed
    value is UNKNOWN **with its reason**, never 0, never a dash and never the
    last known number silently — and a row with no timestamp is not a fact, it
    is a rumour. Related: class 49/53 (a plausible zero), class 24 (a check that
    prints but does not gate).

83. **An acknowledgement mistaken for a settlement — outside the broker.**
    (Number assigned at merge, A16 — highest occupied on dev at authoring: 82.
    Re-check at merge.) Class 81 named this shape at the broker socket. It is
    not a broker defect. It is what happens anywhere a *receipt* is read as
    proof of the *state it was requested to produce*, and one wave
    (`docs/v5-precheck-0906`, 2026-09-06) hit it three times in three different
    layers in a single session — which is why it is filed as its own class
    rather than as a corollary of 81.
    **The three instances, one shape:**
    (a) **At the CDN.** The A14 closeout check fetched the report's raw URL by
    BRANCH path and got `HTTP 200`, 228,895 bytes — the *previous* revision,
    while `origin/dev` already held `ac48aea6…` at 229,575.
    `raw.githubusercontent.com` caches branch paths ~5 min. A 200 on a branch
    URL can therefore certify a revision that no longer exists, a file that was
    just deleted, or a push that has not propagated.
    (b) **In the tooling.** A redaction pass over the 313 KB companion report
    ran `open(P,"w").write(red(open(P).read()))` — Python opens for write, and
    truncates, BEFORE it reads. The file was emptied. The verification printed
    `residual account names: 0`, which was true, and true of an empty file. The
    check and the damage had the same cause, so the check could not see it.
    (c) **In the emergency control.** `api/handler_risk.go:121` writes
    `PositionsFlattened: 1` after `CloseLong` returns nil — the return of
    putting a frame on a socket. The value is never persisted and counts
    nothing; the endpoint has been invoked `n=0` times across 29,692
    `log_events` rows, so the defect has never fired in anger. (Same site as
    class 81, reached from the opposite direction: 81 found it in the ledger,
    this wave found it in the HTTP response.)
    **What identified it** was not any of the three failing. Each *succeeded*.
    It was asking, of each success, WHICH RECEIVED ARTIFACT the success
    describes — and finding that in all three the answer was the request's own
    acknowledgement.
    **Probe:** for every check that gates a claim, ask what it would print if
    the thing it checks were absent, empty, stale or deleted. If the answer is
    the same as the passing output, the check is decorative. Two specific
    smells: a verifier that shares a mutable resource with the operation it
    verifies (b); and a success code read without the payload it should carry
    (a, c).
    **Law:** **a status code is not a verification, and a return value is not an
    observation.** Verify against the ARTIFACT, pinned by identity, compared on
    content: a commit SHA and a byte count, not a branch name and a 200; a
    re-read of the file by a separate process, not the writer's own report; a
    fresh far-side snapshot, not the send's error. Where the artifact cannot be
    named, the claim is UNKNOWN with its reason (class 82's corollary), never a
    pass.
    Sibling to class 81 (a send read as a settlement — the broker case) and
    class 24 (a check that prints but does not gate). Related: class 49/53 (a
    plausible zero), class 79 (silence is not death), class 82 (a green word
    answering a narrower question). Operationalised as **R10**, PART 2.


84. **A boolean standing in for a spectrum — and the warning that fires by
    construction beside it.** (Number assigned at merge, A16 — highest occupied
    on dev at authoring: 83. Re-check at merge.) `isCMEHoliday` answered yes/no
    and the gate treated every holiday as a FULL closure. The code conceded it in
    its own comment — *"for v1 we treat them as full closures and refuse to trade.
    Refine in Plan 3 if it becomes restrictive"* — and on 2026-09-07 it became
    restrictive: MNQ traded **980 bars across 153.50 points** on Labor Day while
    the gate called the market shut, every cycle was skipped and no LONDON plan
    was ever read. Most US holidays are EARLY CLOSES, not closures; the boolean
    had no way to say so.
    **The second half, which the first hid.** While the market was "closed" the
    loop idled on a deliberate 3-minute backoff and an overrun check compared it
    against a 2-minute scan interval. A 3-minute sleep can never fit inside a
    2-minute interval, so the warning was GUARANTEED on every closed tick —
    **165 in one boot log, every one reading `3m0.0XXs > 2m0s`**. A warning that
    cannot indicate a fault trains the reader to skip the line that one day does.
    **What identified it** was not a failure: it was the dashboard contradicting
    itself — a MODE row reading `CME CLOSED (holiday)` beside a feed carrying a
    bar seconds old — and someone asking which of the two was lying.
    **Probe:** for every boolean that gates behaviour, ask what the WORLD's third
    state is and what the code does with it. Then, for every warning: construct
    the case where it fires; if that case is reachable by design rather than by
    fault, it is noise. Count how many times it fired last week and how many of
    those a human acted on.
    **Law:** a calendar is DATA, not code — one dated file, one row per special
    date, a cited source per row, and no date literal in the language. Where a
    date's treatment cannot be established it takes the SAFE side and is NAMED on
    the boot line, never guessed silently. And one fact has ONE owner: this wave
    found the same early close in three places with two key conventions and two
    different times, the gate stopping at 12:00 on days the sourced file said
    12:15.
    Sibling to class 82 (a green word answering a narrower question — same
    dashboard row, same day). Related: class 49/53 (a plausible zero), class 24
    (a check that prints but does not gate), class 28/77 (one canonicalizer, one
    owner).

88. **A liveness signal that is a side effect of activity.** (Number assigned at
    merge, A16 — two-format census run at merge: highest occupied 87, duplicates
    75/76/77 pre-existing. Re-check both formats; a census in one convention
    cannot see the other.) The main-tree lock proves the holder is alive by a
    heartbeat. On 2026-09-07 a cutover lane beat that heartbeat **inside the
    `until` loops that waited on its background builds** — so the beating stopped
    exactly when the work stopped. The lock went **STALE at 2107 s** while the
    binary was swapped in, RELEASE already led it, and the old process was still
    serving, waiting on a human to run the kill. From outside it was
    indistinguishable from an abandoned lock, and a lane corroborating on "is
    HEAD moving?" would have found HEAD static since the build and had a
    plausible case for `reclaim` — landing on a half-applied swap.
    **The generalisation, which is the reason this is a class and not an
    anecdote:** anything whose liveness signal is a side effect of activity goes
    quiet **precisely when the thing you are waiting on is a human**. The busiest
    lane looks most alive and the blocked one looks dead, which is exactly
    backwards — a blocked lane is the one holding something.
    **What identified it** was not the tool: `status` said STALE and nobody was
    reading it. It was a PEER who had held the same lock an hour earlier, waited
    75 minutes on the same human, and recognised the shape. Their own heartbeat
    survived only because their keeper was **detached from the work**, not
    because they were more careful.
    **Probe:** for every liveness or freshness signal, ask what emits it and
    whether that emitter runs when the system is IDLE. If the signal rides on
    work, it is an activity meter wearing a health badge. Then ask what the
    longest legitimate idle stretch is — for anything gated on a human, it is
    unbounded.
    **Law:** decouple the heartbeat from the work. A detached keeper that
    self-exits when the resource is released:
    ```
    nohup bash -c 'while true; do deploy/nofx-lock.sh heartbeat <session> \
      >/dev/null 2>&1 || exit 0; sleep 100; done' >/dev/null 2>&1 &
    ```
    And note what does NOT save you: the lock's `expiry` field is written at
    acquire and only ever PRINTED — the ALIVE/STALE branch compares heartbeat
    AGE alone (`nofx-lock.sh`). Expiry is display; the keeper is the mechanism.
    **Corollary, from the same hour and the same two lanes.** Both lanes ran a
    census of this file and both were blind, in opposite ways: one grepped only
    `## CLASS N` and could not see the `NN. **Title.**` entries where 78-84 live;
    the other counted BOTH formats but with `sort -n | uniq` instead of
    `uniq -c`, **deduplicating while hunting duplicates**. The second is the more
    instructive error — a regex that cannot see a format is fixed on sight, but
    collapsing duplicates before counting them reproduces anywhere. Neither lane
    found 75/76/77 alone; the duplicates surfaced only when a challenge forced a
    third, wider query. Related: class 85 (the branch you handle carefully is the
    one you can see), class 24 (a check that prints but does not gate), class 83
    (a status code is not a verification).

89. **A wave verified by a toolchain that cannot see half of it.** (Number
    assigned at merge, A16 — two-format census at merge: highest occupied 88,
    duplicates 75/76/77 pre-existing.) On 2026-09-07 the place-confirmation wave
    edited `ninjascript/VLTraderTCPClient.cs` and shipped it to dev with **two
    compile errors**: an `if` statement inside a C# collection initializer, and a
    `reason:` argument dropped into a branch where `ageSec` is out of scope. The
    commit reported "Suite: 28/28, 0 FAIL" and that was TRUE — of the Go half.
    `go build ./...`, `go vet` and `go test ./...` cannot compile NinjaScript, so
    every check that passed was blind to the file that was broken. Another lane
    repaired it before the next F5.
    **Why it did no runtime damage, and why that is not comfort.** NinjaScript
    only takes effect after copy → F5 → full NT8 restart, so the broken file sat
    inert. The damage would have landed on whoever ran that dance next, at the
    moment they most needed the AddOn to build — and the commit gave them no
    reason to suspect it.
    **What identified it** was not the suite and not review: it was the NEXT
    wave's audit reading the same file for an unrelated reason. Nothing in the
    wave's own process could have found it, which is the whole point.
    **Probe:** list every LANGUAGE and every RUNTIME a wave's diff touches. For
    each, name the command that compiles or executes it. Any language with no
    such command in the wave is UNVERIFIED — say so by name. Two smells: a
    green-suite claim in a commit whose diff spans more than one toolchain, and
    a file whose deploy path is manual (copy/F5/restart, a DLL, a browser
    extension, a device) — manual deploy is exactly where "it compiles" stops
    being checked by anything.
    **Law:** **a wave states which half its toolchain verified and which half it
    did not.** Green is a claim about what ran, never about the diff. Where a
    language cannot be compiled in the wave, the commit says so in words and the
    boot line or report names what remains unproven — the same rule as an
    uncomputed value being UNKNOWN rather than 0 (A24, class 49/53).
    **Pin (standing):** any wave touching `.cs` states plainly that NinjaScript
    is outside the Go toolchain and names what was and was not compiled. A
    commit that reports a green suite while its diff contains `.cs` and says
    nothing about it fails this entry on its face.
    **Second pin (owner ruling 2026-09-08, from this entry's own follow-up):
    A MUTATION THAT PASSES IS NOT EVIDENCE.** A mutation test proves a pin bites
    only if the mutation ACTUALLY CHANGED THE SOURCE — so the wave must show the
    changed line, not merely report that the suite went red or green. The
    signal-clock wave mutated `Timestamp:   time.Now()` (three spaces) where
    gofmt had aligned `Timestamp: time.Now()` (one). The search matched ZERO
    occurrences, nothing was mutated, the suite passed, and that pass was
    reported as "the pins bite" until a second look showed the count was 0. A
    no-op mutation reporting green is the same hollow verification as a green Go
    suite over a broken `.cs`: the check ran, and it ran on nothing.
    **How to satisfy it:** assert the occurrence count before mutating
    (`assert t.count(old) == 1`), or print the diff of the mutated file. The
    corrected run then failed by name — "move_stop stamp is 31m1s old with a
    31-minute-stale bar cache" — which is what the first run should have shown
    and did not. Sibling to class 83: trusting a tool'"'"'s success message over the
    artifact, one layer further in, because here the tool was the verifier itself.
    Sibling to class 83 (a status code is not a verification — there, a 200
    proved only that something answered; here, a green suite proves only that
    Go compiled). Related: class 24 (a check that prints but does not gate),
    class 88 (a liveness signal that is a side effect of activity).

90. **A timestamp whose key omits the version.** (Assigned at merge of
    `fix/plan-liveness`, 2026-09-08.) **Root cause:** the first-observed S1
    invalidation stamp was keyed by plan and scenario, but scenario IDs are
    reused across immutable versions. ASIA v4's gate attached v1's 20:51 time
    to v4's 29753.25 anchor. **Probe:** two versions sharing S1 and different
    anchors must preserve separate records through the production recorder and
    gate resolver; removing the version must fail that pin. Store the judged
    anchor with the time and reject mismatched evidence. **Law:** evidence
    identity includes every version dimension the reader uses; legacy evidence
    without that dimension stays unknown, never inferred into a new version.
    The C1/C3 corrections and C2/C4 limits are in
    `reports/2026-09-08-plan-liveness.md`; the original 20:51 line is not a
    born-dead fixture. A separate real D3 replay uses plan row 265 / bar 451050:
    complete authored close rules are checked at write; unsupported or incomplete
    evidence is UNKNOWN and accepted. Exhaustion remains warning-only because
    its proposed live causal premise was not established. Versioned status and
    metadata feed the card/desk; the existing evaluator's verdict is unchanged.

91. **A named close counted before its bucket closed; a sequence lost its reference.**
    (Assigned at merge of `fix/confirmation-truth`, 2026-09-08; highest occupied
    was 90. Two-format `uniq -c` also found the existing duplicate 75/76/77;
    none was renumbered.) Decision **38329**, LONDON v5 S3, snapshot **08:16:59
    CT**, said MET while the 08:15 five-minute bucket closed at 08:20. The
    generic counter admitted a forming bucket; the waterfall path counted raw
    minutes under a 5m label. A missing touch lookup returned zero and the
    sequence silently substituted plan birth, admitting pre-touch reclaims.
    **Law:** one explicit-clock bucket-end predicate for every confirmation,
    validation and feeds-forward consumer; one `(instant, ok)` reference
    lookup; UNKNOWN never satisfies and never substitutes publication. Ordered
    arms use the scenario verdict. Touch OHLC references name their closed-minute
    observation upper bound, not an invented tick timestamp.
    **Owner-accepted validation correction:** the closure-only audit removed
    **23 REJECTs across two scenarios**; minute-only reclaims never established
    the named 5m void. This is correct rule application, not a regression.
    The separately authorized **`1m_displacement`** rule adds **one** further
    PASS (**163/S1, decision 34790**) by preserving immediate-mode preparation:
    **24 final REJECT→PASS observations**. The boot receipt labels both audit
    counts separately from live refusal counters. Pullback/void facts use 5m;
    immediate displacement has its own function, label and tests.
    **Coverage:** 789 stored scenarios, 627 replayed, 176 confirmation-changing
    in 299 observations; 168 have supporting saved-prompt matches. **162 stay
    unevaluated**, never inferred from that subset. Four stored waterfall
    scenarios changed displacement in 31 observations and last close in 162;
    corrected planner facts may intentionally change subsequent authoring.
    **Pins:** explicit before/at/after boundaries; 38329; reversed, missing and
    equal-time references; real arm/record/desk callers; separate 1m rule and
    34790. Four actual-line mutations fail assertions; 50 already-closed verdict
    goldens are unchanged. Full evidence: `reports/2026-09-08-confirmation-truth.md`.

92. **A description that lives in a DB row nobody audits.** (Number assigned at
    merge, A16 — highest occupied on dev was 91 at the time of writing; re-check
    with a `uniq -c` census before merging.) The system's description of itself
    contradicted its code in twelve places (docs/superpowers/reports/2026-09-08-
    the-strategy.md @ 5519d494, section D4). All twelve reproduced at the running
    rev 954f11b1 — none had been quietly fixed. Two structural facts made them
    durable. **(a) Four of the false sentences were not in the repo at all.** They
    live in `strategies.config → ai_config.prompt_sections` in `data/data.db`, are
    rendered into every AI call, and are invisible to every grep, every test and
    every code review the project runs — "2-4 trades per day", "50 point move
    stop loss to breakeven", "avoid sideways oscillation", "avoid immediately
    restarting after closing positions", none of them enforced by anything.
    Worse, the same text sat in **three unbound `New Strategy` presets** beside
    the bound MNQ row, so correcting a source template fixes nothing and binding
    a different strategy resurrects every claim. **(b) A Guide sentence can be
    true when written and false later without any signal.** `GUIDE_BUILT_REV`
    proves when the Guide was BUILT; it says nothing about whether the prose
    still matches behaviour, so a stamped, in-date Guide can be confidently
    wrong. **Probe:** for every sentence that states a behaviour, name the code
    that performs it and the path that reaches it — a mechanism that exists but
    is never reached is not a behaviour. Then grep the DB for prose:
    `sqlite3 data/data.db "SELECT id FROM strategies WHERE config LIKE '%<claim>%'"`
    and check EVERY row, not the bound one. **Law:** a sentence stating a number
    names its resolver (the Guide already had the pattern in tradingDay.ts and it
    was the only file of five to use it); a sentence stating enforcement names
    the PATH that enforces it, because the same rule can bind one path and not
    another — the lunch and first-N no-trade bands are read by the AI-decision
    gate and the adherence grader and by NOTHING in `trader/armed_executor.go`,
    so under `plan_mode=strict`, where a resting order is the only way into the
    market, both bands refuse nothing while the Guide called them "all of them
    enforcing". Fixed in W5 as words only; the code defect (arm path ignores the
    band) is filed for a later wave.

93. **A guard that derives its expectation from the thing it guards.** (Number
    assigned at merge, A16 — highest occupied on dev was 92 by a two-format
    `uniq -c` census; class **97** does NOT yet exist on dev, it is inbound on
    fix/session-risk-limits, and this entry is its WORKED EXAMPLE — renumber or
    cross-reference at whichever merges second.) W5 shipped a contract test to
    stop the Guide's lunch window drifting from `kernel.LunchWindowCT()`. It
    built its forbidden-literal list FROM the resolver and asked whether the
    Guide contained those literals. Mutate the resolver to `12:15`/`13:45` and
    the Guide's stale `12:00–13:30` matches none of the new literals, the loop
    falls through, and **the test reports `ok`**. It could only ever confirm
    today's agreement. A sibling lane's class 97 ("one source, both readers —
    never two readers that happen to agree") is the general law; this is the
    same defect inside a guard written to prevent it, one function from where
    its author was fixing that very class. **The wave's own E5 step — "mutate
    the resolved value E1 reads" — was owed and unrun; when finally run, it
    refuted the fixture.** **Probe:** for any test asserting agreement between a
    document and a resolver, MUTATE the resolver. If the test still passes, the
    test compares the document with itself. **Law:** a drift guard derives its
    expectation from ONE side and asserts EQUALITY against the other — find
    every value the document states, then require it to equal what the code
    resolves. Never enumerate "forbidden" values from the resolver, because the
    stale value you are hunting is by definition not among them. Fixed in
    `kernel/guide_clock_contract_test.go`: the inverted pin finds every
    `HH:MM–HH:MM` range written near "lunch" and requires it to equal the
    resolved pair; the same mutation now fails on plays.ts, settings.ts and
    tradingDay.ts at once. A second lesson from the fix itself: the first
    inversion used a ±160-character window and failed on tradingDay.ts's
    unrelated NY session range `08:30–14:45` — a guard calibrated by guesswork
    fails on correct text, so the window was calibrated against the real
    sentences instead.

## PART 2 — PRE-AUDIT (standing hard rules)

- **R1 fresh evidence only** — produced THIS run: CT-timestamped queries,
  quoted journal lines, committed script deltas. Citing any prior report as
  proof = automatic UNVERIFIED.
- **R2 independent math** — recompute from raw stores; never call the function
  under test (the recompute is its own implementation).
- **R3 twin paths** — long/short mirrors both exercised.
- **R4 file:line** — every code claim cites the exact location.
- **R5 grades** — S/A/B/C; S-findings listed first.
- **R6 verdict grammar** — PROVEN / EVENT-WAIT (SHIPPED-UNPROVEN + the exact
  awaited event) / BROKEN / UNVERIFIED; never upgrade a grade without fresh
  evidence.
- **R7 pnl rule** — `pnl_corrected` everywhere + `excluded_null_pnl` for the
  354 legacy NULL rows (`WHERE pnl_corrected IS NOT NULL` in every expectancy
  query; position_query.go).
- **R8 times** — all times CT.
- **R9 isolation** — read-only sweeps run in a worktree at the RUNNING rev;
  zero code/config/DB/env changes; no restarts. Main tree untouched.
- **R10 closeout publication** — a report is published only when the ARTIFACT
  is verified, never when the push command exits 0. Fetch the raw URL with the
  **commit SHA** in the path — never a branch path, which
  `raw.githubusercontent.com` caches for ~5 min and will happily serve at
  `HTTP 200` for a superseded or deleted revision — and compare
  `size_download` against `git ls-tree -r --long <sha> -- <path>` for that
  blob. A 200 alone proves that something answered. (Amends A14; born class 83,
  2026-09-06, `docs/v5-precheck-0906`.)
```
SHA=$(git rev-parse HEAD)
git ls-tree -r --long "$SHA" -- docs/superpowers/reports/<report>.md
curl -s -o /dev/null -w "HTTP %{http_code}  %{size_download} bytes\n" \
  "https://raw.githubusercontent.com/johnwick2921-cyber/nofx/$SHA/docs/superpowers/reports/<report>.md"
```

---

## PART 3 — PRE-CUTOVER (standing 8-step protocol, 0-7; flat gate = 5 legs, class 33)

0. **PUSH EMPTY AT ACCEPT — claim the wave before you build it.** The moment you
   accept a dispatch, create the named branch and push it with an empty commit,
   BEFORE writing a line:

   ```
   NOFX_SESSION=<your session>  deploy/nofx-claim.sh new <branch> "<wave>"
   ```

   which is exactly:

   ```
   git checkout -b <branch> origin/dev
   git commit --allow-empty -m "claim: <wave> — <session>, $(date -Is)"
   git push -u origin <branch>          # rejected or already there? STOP.
   ```

   If the branch already exists on origin, **another lane has this wave**: stop
   and coordinate before doing any work. Fold the empty commit into your first
   real one (`--amend`) or leave it; it costs nothing either way.

   **THE SESSION NAME AND THE ISO TIMESTAMP ARE MANDATORY, NOT DECORATION**
   (owner ruling 2026-09-04). The message MUST match
   `claim: <wave> — <session>, <ISO-8601 with offset>`, and
   `deploy/nofx-claim.sh check <branch>` FAILS on anything else —
   `audit` sweeps every claim on origin.

   **THE SESSION FIELD CARRIES BOTH IDENTIFIERS — A CLAIM MUST BE ROUTABLE, NOT
   MERELY ATTRIBUTABLE** (owner ruling 2026-09-07). Write it as:

   ```
   NOFX_SESSION="<wave>-<session-uuid-prefix>/<ListAgents-name>[<ref>]"
   # e.g.  claimid-ee7f9468/nofx-db[ca9c60]
   ```

   The 2026-09-04 rule fixed attribution: a claim now names WHO. It did not fix
   ADDRESSING, and the two are different problems. The uuid prefix is the lane's
   identity in the claim/lock namespace; `ListAgents` addresses sessions by a
   short ref in a DIFFERENT namespace, and nothing joins them. So a lane that
   hits a collision can read the holder's name and still be unable to send it a
   message.

   **The evidence, 2026-09-07.** `fix/session-calendar` was claimed by
   `session-calendar-554049f5`. A second lane hit the collision at step 0
   exactly as designed, stood down exactly as designed — and then had to relay
   two owner additions to a lane it could not address: `ListAgents` offered
   `nofx-2c / nofx-ba / nofx-e7 / nofx-6b` and no row matching `554049f5`. The
   message went to **all four sessions** because there was no way to send it to
   one. Three lanes paid an interrupt for a message that concerned none of them,
   and the fourth may not be the holder either. One message, four sends, delivery
   still unconfirmed. The same gap had already appeared on 09-05 in the main-tree
   lock, whose holder `wave-a-record-554049f5` was likewise absent from every
   listing — so this is the second sighting, not a one-off.

   Both halves are needed and neither substitutes for the other: the uuid prefix
   survives in git after the session ends and is what a later reader greps; the
   `name[ref]` is what `SendMessage` can actually deliver to while the lane is
   alive. A claim carrying only the first is a forwarding address for a lane that
   has moved out.

   **Enforcement is a NAMED FOLLOW-UP, not part of this entry.** The regex in
   `deploy/nofx-claim.sh` (`CLAIM_RE`) treats the session field as `.+`, so the
   composite form passes `check` today and so does the old bare form — verified
   on this entry's own claim, which uses the new form and passes unchanged.
   Until that regex is tightened by owner ruling, **this is a convention the
   checker does not gate** (class 24: a check that prints but does not gate).
   Stating that here rather than leaving the doc to imply an enforcement that
   does not exist.

   **A lane whose `[ref]` is unknown at claim time writes `[unlisted]`** — never
   a guess, and never omitted silently (A24).

   Why it is a rule and not a template: on 2026-09-04 step 0 worked perfectly and
   still left the lane stuck. `fix/reaper-reads-snapshot` was claimed as
   `claim: reaper reads the snapshot, not order_update silence (PART 3 step 0)`.
   The next lane pushed 70 seconds later, hit the collision step 0 exists to
   catch — and then could not act on it, because the claim named no one. In this
   repo every commit carries the identical git author, so the author field
   answers nothing (PROVENANCE, CLAUDE.md): the claim message is the ONLY place a
   reachable identity can live. **A claim without a session proves a collision
   and cannot resolve it.** The template already showed `<session>`; a template is
   a suggestion, and two of the five claims on origin at the time this shipped
   omitted it. Pins: `deploy/nofx-claim-test.sh` (10, including the real
   malformed reaper message as REAL-1 — a checker that passes the message which
   caused the incident is decoration; mutation-tested by hollowing the regex).

   **AND A PEER'S STATED PLAN IS NOT PROVENANCE EITHER (added 2026-09-10).**
   PROVENANCE says provenance comes from the branch, the worktree and the
   timestamp, never from the author field. The same applies to an INTENTION. A
   lane wrote "that work is owned by lane <X>" into class 99's Law on the
   strength of a message in which X had said *"I am folding it into my next
   wave's Section C."* X never built it; by then it was a day old on
   `fix/arm-state-predicate`, claimed by a different lane — and one
   `git ls-remote --heads origin | grep arm` would have said so.

   Two reasons this is worse than an ordinary credit error. First, an
   **attribution is a POINTER**: it tells the next reader where to go and whom to
   ask, so a wrong one costs everybody who follows it, not just the person
   miscredited. Second, a plan is the one input that looks authoritative and is
   guaranteed stale — the lane that stated it may have been reassigned, ended, or
   beaten to it, which is exactly what happened here. **Before naming an owner in
   a durable document, resolve it against `git ls-remote --heads origin` and the
   claim commit, not against what someone told you they were going to do.**

   **QUOTE THE BRANCH, NEVER THE CLAIM SHA** (owner ruling 2026-09-04). A claim
   commit does NOT survive a routine `git pull --rebase origin dev` — the rebase
   replays it onto the new base and it comes back with a different sha, so the
   branch needs a `--force-with-lease` and anyone holding the old sha is now
   holding a commit that no longer exists. This wave hit it: the claim pushed as
   `4d485b19` and landed as `5bcb5455`, same message, same author, same wave.
   So a claim is addressed by its BRANCH, and `nofx-claim.sh check` reads the
   FIRST COMMIT AHEAD OF `origin/dev` on that branch — whatever its sha — rather
   than a recorded one. A coordination message that cites a claim sha will go
   stale the first time its lane rebases; cite `fix/<wave>` instead.

   Born 2026-09-03, during class 70 itself. Two lanes independently wrote ~250
   lines of the same lock wave inside an hour, and a third lane's branch was
   consumed into dev without its author ever being told — and the ONLY thing that
   surfaced any of it was a non-fast-forward rejection at push time, after all
   the work was done. A branch name on origin is the only claim this protocol
   has. Claiming it costs one empty commit and one second; not claiming it cost
   a day's duplicate work and an attribution that git cannot reconstruct.

   **THE SECOND THING THIS BUYS — a lost dispatch stops looking like a quiet
   lane.** (Found by nofx-ed 2026-09-03, the same night: four dispatches
   misrouted, three to the wrong lane and one — "TWO-DAY AUDIT" — whose title
   reached a lane and whose body reached nobody.) A dispatch delivered to the
   WRONG lane is self-correcting: the receiver sees a mismatch and says so, which
   is how three of the four surfaced within minutes. A dispatch delivered to NO
   lane is **silent**, and silence is exactly what a lane working quietly looks
   like. Nothing anywhere is in a state that differs from success.

   The claim branch makes the two distinguishable, which is the whole point:
   **an assignment with no claim branch on origin after ~15 minutes is either
   unstarted or LOST — and either way it is now a question somebody can ask.**
   Fifteen minutes because that is long enough to read a dispatch and cut a
   worktree, and short enough to catch a loss in the session that lost it; the
   number matters far less than the fact that the absence is now checkable at
   all. `git ls-remote --heads origin | grep <wave>` is the whole probe.

   **FAMILY: "a healthy-looking absence."** The failure is not that something
   went wrong loudly, it is that the *absence of the thing* is indistinguishable
   from its presence. Same shape as **class 74** (a kill that worked, a process
   that came back, a version that never moved — a null cutover reading as a
   deploy) and **class 76** (a pointer born dangling: stable, silent, empty, and
   invisible to a guard that only watches for change). Three instances in one
   evening. When a check can only observe CHANGE or ERROR, ask what its silence
   would look like if the thing had never existed at all — and if the answer is
   "identical to success", the check does not cover the case.

1. **Tree gate:** porcelain-clean + `deploy/nofx-lock.sh acquire <session>
   <task> [minutes]` (atomic create; records session · task · acquired ·
   expiry · heartbeat, NO pid — class 70) + HEAD is the single allowed branch
   for this dispatch. Beat it as you work (`nofx-lock.sh heartbeat <session>`,
   or wrap long steps in `nofx-lock.sh with-heartbeat <session> -- <cmd>`); a
   heartbeat older than 5 min reads STALE, which means NOT CHECKED IN, never
   dead, and never clear one without corroboration.

2. **Build:** from the MAIN checkout at the deploy commit (worktree builds lose
   vcs stamping → `<no-vcs>` → INTEGRITY REFUSED). `go build -o nofx-bin.next`.

3. **Marker:** `deploy/RELEASE` = the 8-char build rev, committed (marker AFTER
   build; RELEASE must equal the BUILD sha).

4. **Flat gate — FIVE legs (class 33), all quoted:** `GET /api/cutover-gate`
   returns them in one payload; quote it, do not assemble them by hand.
   (1) DB `trader_positions` OPEN = 0 · (2) API positions `[]` · (3) NT8
   positions snapshot count = 0 · (4) **working orders = the `armed_orders`
   ledger's non-terminal rows** (NT8 emits no working-order frame, audit F12 —
   before 2026-09-02 this leg was a stub returning empty and passed vacuously
   at cutovers 35→41) · (5) **no in-flight planner work** — `replan_in_flight`
   false AND no planner read claimed for this trader on any date/session (the
   2026-08-31 17:34 defect: a kill landed on attempt 3/3 and the chain died
   silently). A leg that cannot be EVALUATED fails. `ready:false` = HOLD.

5. **Owner ack:** explicit "go" — reachable and acking the boot line within
   minutes, OR a TESTED auto-rollback. Timers banned. **Override rule
   (class 33):** the owner MAY override leg 4/5 and swap with arms resting —
   the override is permitted, leaving orders alive is not. Such a cutover
   REQUIRES the boot sweep to run and its result to be quoted in the report
   (`🛡 boot sweep CANCELLED pre-boot arm …` per row, or `cancelled 0`).

6. **Swap:** `mv nofx-bin nofx-bin.old.<tag>` → `mv nofx-bin.next nofx-bin` →
   VERIFY (`go version -m nofx-bin` shows the deploy rev) → `kill -9 <PID>`
   (SIGKILL — SIGTERM exits 0 and systemd does NOT relaunch). The classifier
   denies the kill to the agent: print the command and have the OWNER run it.
   **`mv`, never `cp`** — `cp` onto a running binary fails `Text file busy`,
   and if the kill is in the same block it fires anyway and relaunches the OLD
   binary: a null cutover that reads like a deploy (class 74). Three steps,
   three exit codes, never one block.

7. **Boot checklist (within 90s):**
   `🔐 BOOT INTEGRITY OK — rev <8char> +dirty · built <ts> · expected <8char> ·
   goldens PASS` + exactly ONE PID + feed warmed (bars_historical replay ~30s
   before decisions). **Rollback rule:** no boot line within 90s OR goldens
   fail → restore the prior binary + RELEASE, kill -9, restart, alert the owner.

## CLASS 75 — SYSTEM-MAP CONTRACT (born 2026-09-04, docs/system-map-0904)

**Symptom:** a knob, gate leg, window, threshold, refusal string, or boot line
changes in a wave; the map of the system goes stale; the next lane believes the
map and not the code.

**Root cause:** the map (`docs/superpowers/SYSTEM-MAP.md`) is documentation
nobody is forced to read, so nothing forces it to stay true.

**Probe:** compare the map's boot-line quotes against the running binary's
journal; compare knob values against the settings registry and resolvers.

**Law:** **every wave that changes a rule updates its map section in the SAME
commit** — knob value, gate leg, window, threshold, refusal string, or boot
line. A contract test greps the map for the boot-line text: a boot line with no
matching text in the map fails the suite, and a map quote with no code match
fails it too (the text is the join key). Labels `[R]/[X]/[T]/[I]/[O]` move with
evidence (legend: belief-census 2026-09-02:8-16). A wave that renames the boot
line must update the map in the same commit or the contract test fails both
sides.

## CLASS 76 — THE POSITIONAL ARGUMENT IN THE WRONG SLOT (born 2026-09-05, fix/wave-b-stop-entry, C1-C3)

**Symptom:** an order the broker ACCEPTS, acknowledges, and lists in its own
book — and then never acts on. No reject, no error, no counter, nothing in any
log on our side. The feature reads as live and idle rather than broken. Here: 22
of 22 stop-market ENTRIES over two days went to NT8 as `Limit price=<trigger>
Stop price=0`, a stop whose trigger is zero. Lifetime fill rate 0/22, and one of
those inert orders had already been accepted as the capability's own PROOF.

**Root cause, two of them, and they MASKED EACH OTHER.** (1) `Account.CreateOrder`
is positional — after `quantity` come (limitPrice, stopPrice, …) — and the entry
call computed a single price and passed it into the limitPrice slot for both a
Limit and a StopMarket. (2) The stop-entry branch reused the LIMIT wrong-side
predicate with the trigger in the entry argument, which inverts all four of its
answers for a resting stop. Bug 2 admitted 21 orders the market had already run
50-103 points past; bug 1 made them inert. Fixing either one alone is worse than
fixing neither: the slot fix alone turns 21 inert orders into 21 the broker acts
on immediately, at ~75 points adverse.

**Probe, four questions:**
1. For every positional API call with two or more same-typed arguments, is there
   a call to the SAME API built correctly elsewhere in the same file? Diff them.
   Here the bracket stop-loss 850 lines down had always been right.
2. Does the log print the ARGUMENTS PASSED, or the variables parsed just before?
   The AddOn logged `stop@29590.5` on all 21 malformed submissions. A log that
   reads back your own intent cannot witness a slot bug.
3. Read the broker's OWN record, not ours. NT8's order log and the
   `order_snapshot` book both said `Stop price=0` for two days. (Trap: a
   `json:",omitempty"` field VANISHES at zero — absence there means zero, not
   unknown. Trap: NT8 writes `Type='Stop Market'` WITH A SPACE; grepping
   `StopMarket` returns 0 hits and reads as "no such orders ever existed".)
4. Is one predicate shared by two order kinds? A boundary that is strict for one
   is inclusive for the other (a limit AT its price rests; a stop AT its trigger
   fires). One function per kind, chosen BY KIND, never a shared one with a
   default fallthrough.

**Law:** **a capability is proven by a RECEIVED frame that carries the value,
never by the order's behaviour.** "It rested and it cancelled" was the 2026-08-31
acceptance criterion, and a zero-trigger stop rests perfectly, forever. The
acceptance test must read the PRICE SLOT back. When the far side is a separately
deployed artifact, the minimum-build floor is the mechanism: bump it in the same
PR as the fix so an un-recompiled far side is REFUSED loudly rather than sent
something it will mis-execute — and remember the floor is a BYTEWISE string
compare, so it must advance the ISO DATE, never only the suffix.

**Corollary (why it survived):** every layer reported the value it INTENDED to
send. Go logged the trigger, the AddOn logged the trigger, the ledger said
"working stop" for 48 minutes. Only NinjaTrader ever said zero, and nothing read
it back. When two components agree, check whether they are agreeing about the
same artifact or merely echoing one source.


## CLASS 77 — A CANONICALIZER ADDED AT ONE BOUNDARY, CONSUMERS LEFT ORDINAL (born 2026-09-05, fix/wave-b-stop-entry repair pass)

**Symptom:** none, for two days, and then a live order in the OPPOSITE
DIRECTION. Class 28 ("one canonicalizer per identifier, called where the value
ENTERS") was applied correctly to `armed_orders.side` on 2026-09-03: the store
now uppercases at the write, so `trader_positions` and the ledger can finally be
compared. Nothing audited the CONSUMERS. Two of them still compared that column
to the lowercase literal `"long"`, ordinally:

- `armed_executor.go` chose a stop entry's trigger with `if r.Side == "long"`,
  so a LONG arm got `entry − offset` — a buy stop BELOW the level it must sit
  above — and the new stop-side guard then adjudicated that mis-signed trigger.
- `VLTraderTCPClient.cs` chose the ORDER ACTION with
  `side == "long" ? OrderAction.Buy : OrderAction.SellShort`, so `"LONG"` fell
  to the else branch: a long entry, limit or stop, submitted as a live SELL.

Neither ever fired, and the reason is the trap: **every row written since the
canonicalizer happened to be SHORT** (21 stop_entry + 9 limit rows, ids 38-102
and the 09-04 limit group), and for SHORT the wrong branch is accidentally the
right answer. A census of the column reads "no problem" precisely because the
half that breaks has no rows.

**Probe, five questions:**
1. When a canonicalizer is ADDED to a column, grep every consumer of that field
   for an ordinal comparison — `== "`, `switch`, a map key, a ternary. The write
   side is one line; the read side is however many places already existed.
2. Does the value CROSS A LANGUAGE BOUNDARY? A Go `strings.ToLower` and a C#
   `==` are different contracts. The wire spec said lowercase
   (`vltrader_tcp_PROTOCOL.md` L58) and one call site's comment even said
   `// lowercase per spec L4390` — while the sibling call two functions away sent
   whatever the store returned.
3. Is the fallback branch of the comparison SAFE when the input is unrecognised?
   `x == "long" ? Buy : SellShort` answers "sell" to *every* question it does not
   understand. A directional default is never a safe default.
4. Do the tests feed the STORED value or a hand-written one? Every fixture in the
   wave passed lowercase `"long"`/`"short"` — the casing the store has not
   produced since 2026-09-03. A table over the canonical form is one line and
   would have caught both halves.
5. Is the untouched half of the enum reachable? "It has never happened" and "it
   cannot happen" differ by whichever accident is currently supplying the rows.

**Law:** **a canonicalizer is a WIRE-CONTRACT CHANGE, not a storage detail.**
Fold ONCE where the value enters the path that uses it, hand the folded value to
every consumer on that path, and make the far side fold again on arrival —
because the far side is deployed separately and will, at some point, be running
the version that does not. Where the unrecognised branch has a DIRECTION, refuse
instead of defaulting.

**Corollary (how it was found):** not by a test and not by the census, but by an
adversarial reviewer asking what `r.Side` actually contains at runtime rather
than what the struct comment says (`Side string // long | short`,
`store/armed_orders.go:36` — still true of the type, false of the data).

## ⚠ NUMBERING HAZARD — THIS FILE CARRIES TWO FORMATS (added 2026-09-07)

**Before assigning a class number, count BOTH formats.** PART 1 numbers its
entries `NN. **Title.**`; the appended sections below use `## CLASS NN — TITLE`.
A grep for one is blind to the other, and "the highest is N" from a single-format
read is how duplicates get born.

I filed three entries as CLASS 78/79/80 on the strength of
`grep -oE "^## CLASS [0-9]+"`, which returned 75-77 and nothing higher. PART 1
already held 78 (a plan that could only trade one direction), 79 (silence read as
death) and 80 (a guard that cannot guard the rows it was built for). Mine are now
85/86/87. A peer lane caught it; my own census did not, because the census asked
the question in the format I happened to have written in.

**75, 76 and 77 are STILL duplicated across the two formats** — from waves before
this one. They are not renumbered here: A16 forbids renumbering another lane's
entry, and a number already cited elsewhere is worse to move than to leave. They
are recorded so the next reader knows the collision is real and pre-existing.

Count with this, which sees both:

```
F=docs/superpowers/AUDIT-CHECKLIST.md
b=$(grep -nE '^## CLASS [0-9]+' "$F" | head -1 | cut -d: -f1); b="${b:-999999}"
{ awk -v n="$b" 'NR<n' "$F" | grep -oE '^[0-9]+\. \*\*[^*]+\.\*\*' | grep -oE '^[0-9]+'
  grep -oE '^## CLASS [0-9]+' "$F" | grep -oE '[0-9]+$'
} | sort -n | uniq -c | awk '$1>1{print "DUPLICATE: "$2} $1==1{l=$2} END{print "highest: "l}'
```

**THE CENSUS ITSELF HAS BEEN WRONG TWICE (corrected 2026-09-10).** Both failures
were in the PART 1 half, and both reported FALSE DUPLICATES while still giving
the right maximum — which is why they survived: everyone ran it for the maximum.

- The first version matched `^[0-9]{2}\. \*\*` — exactly two digits. It silently
  skips single-digit classes 1-9, and now that the file has passed 99 it **also
  skips every three-digit PART 1 entry**, because `107. ` has no `.` in the third
  position. Harmless today only because the maximum currently lives in the
  `## CLASS` half.
- Widening it to `^[0-9]+\. \*\*` then swept in two things that are not class
  numbers: the **pre-cutover protocol's** ordinary steps (`1. **Tree gate:**`,
  `2. **Build:**` …), and **bolded numbered lists inside class bodies** — the
  entry for class 107 has a three-item list that made the census report 1, 2 and
  3 as duplicates. A class about format-blind censuses broke the census.

Hence the two filters above, which are both load-bearing: scan for PART 1 entries
only ABOVE the first `## CLASS` heading (excludes class-body prose), and require
the title to end `.**` (excludes the protocol steps, which end `:**`). Verified
2026-09-10: reports exactly 75/76/77/92/93 and `highest: 107`, and each of those
five was confirmed by eye to be a genuine two-format collision.

**AND THE CENSUS IS ONLY AS FRESH AS THE CHECKOUT YOU RUN IT IN.** Class 93's
own text records that its number was taken because "highest occupied on dev was
92 by a two-format `uniq -c` census". `## CLASS 93` had been on dev since
2026-09-08 16:53; the PART 1 entry was written 2026-09-09 18:36, a day later. The
appendix half of the command handles `## CLASS NN` correctly and WOULD have
found it — so the census did not fail on its pattern, it failed on its BASE. A
census run in a worktree cut from an older dev reports a free number that dev
already holds, and it prints a confident maximum while doing so. This is the
SPEC-FRESHNESS LAW (class 73) applied to the checklist itself. **Fetch, then
census at your actual merge point** — not at your branch base, and not from
memory of a run you did earlier in the wave.

(Recorded because a peer read this collision as more evidence for the `^[0-9]{2}`
pattern bug. It is not: that bug only ever affected the PART 1 half, and the
number missed here lived in the appendix half. Two separate defects in one tool,
and the fix for the first does nothing for the second.)

**AND ASSERT THE SHAPES SUM TO THE FILE (added 2026-09-10).** Both fixes above
make the census see the shapes we KNOW about. Neither can see a shape nobody has
thought of — and this file has grown a new one more than once. So make the census
falsifiable against the file instead of trusting its own coverage:

```
F=docs/superpowers/AUDIT-CHECKLIST.md
A=$(grep -cE '^#+ CLASS [0-9]+' "$F")                       # appendix shape
B=$(awk -v n="$(grep -nE '^## CLASS [0-9]+' "$F" | head -1 | cut -d: -f1)" 'NR<n' "$F" \
      | grep -cE '^[0-9]+\. \*\*[^*]+\.\*\*')              # PART 1 shape
echo "counted $((A+B)) class entries"     # ← compare against the file yourself
grep -cE '^\*\*CLASS [0-9]+|^#+ [0-9]+\. |^CLASS [0-9]+ —' "$F"   # shapes at 0 — count them anyway
```

**If the shapes you counted do not add up to the classes actually in the file,
your ceiling is wrong** — and "the highest is N" is falsifiable in one command
that nobody had run for three waves. At `1dd6eac1`: 23 appendix + 68 PART 1, the
three other shapes at 0, and three `N. **…**` lines below the boundary that are
PROSE inside class 107's body, not entries. Verified by eye, because a count that
matches for the wrong reason is the thing this note exists to stop.

Owed to the lane that filed *a census that cannot see its own third format* after
taking a number on a two-format count.

**Read the duplicate line as a DIFF, not as a pass/fail.** Five collisions are
pre-existing and permanent (below). A clean run is not "no duplicates" — it is
"the same duplicates as dev's copy, and no more". Caught by a peer lane whose own
census would otherwise have reported a free number a second time.

**Law:** a "highest occupied" read is only as wide as the format it greps for.
Where a document has grown more than one convention, the census must enumerate
every convention or it will confidently report a free number that is taken.

## CLASS 85 — THE CASE YOUR FIX HANDLES NEVER ARRIVES (born 2026-09-07, fix/bracket-oco-separation, C3)

**Name.** A classifier is corrected to handle a value carefully. An earlier stage,
on the other side of a wire, DROPS that value. The careful branch is unreachable,
every test of it passes, and the behaviour in production is exactly what it was.

**Root cause.** The owner ruled that `unknown` is not terminal: "an unreadable
state is not history. UNKNOWN is non-terminal and takes no destructive branch."
Go was changed accordingly — `unknown` left `terminalOrderStates`,
`ClassifyOrderState` returned `LivenessUnknown`, `IsStateReadable` let callers
refuse to act, and a table test proved all of it.

None of it could ever run. The C# AddOn builds the order snapshot and filters
first:

```csharp
if (st == "Filled" || st == "Cancelled" || st == "Rejected" ||
    st == "Expired" || st == "Unknown")
    continue;
```

An order in `OrderState.Unknown` was not shipped as unknown. It was not in the
BOOK at all.

**Precision matters here, and the first draft of this entry got it wrong.**
Unknown still reached Go on the per-event `order_update` frame, which is emitted
before the actionable-state gate and is not filtered. What it never reached was
the `order_snapshot` — the periodic, re-derivable picture of the whole book. That
is the one that matters, because **absence in the BOOK is what every "this order
is gone" branch keys on**: the
stale reaper cancels and marks the row cancelled, a cancel_pending row is
promoted to cancelled, `entryIsResting` sees no children and ALLOWS the cancel,
cutover leg 4 counts zero working orders, and the new protection reconciler
concludes a position is unprotected and places a SECOND stop beside an invisible
live one. The Go fix made the tree careful about a value the tree never received.

The filter was not wrong when written — its comment says "Terminal orders are
history … shipping the whole history every 30s would grow without bound", and for
Filled/Cancelled/Rejected/Expired that is right. `Unknown` was smuggled into a
list of things that are over.

The event stream is not a substitute: `order_update` is per-EVENT, so a Go
restart loses the picture until the next transition, which on a quiet book may be
never. The snapshot exists precisely to be the thing you can ask at any moment —
which is why a state missing from it is a state that does not exist.

**It was not found by a test.** Every Go test passed, including the new ones. It
was found by a reader sent to enumerate every classifier in the tree *including
the far side*, who noticed the two filters disagreed about one word.

**Probe, five questions:**
1. For the value your fix handles: trace it from where it is PRODUCED, not from
   where you handle it. How many stages sit between? Which of them can drop,
   coalesce, or default it?
2. Does any upstream stage have a filter list? Read the list ITEM BY ITEM against
   the vocabulary your fix defines. A list that was right when written acquires
   an entry that no longer belongs.
3. Can your careful branch be reached in a test that starts at the PRODUCER? If
   the only way to exercise it is to hand-build the value at your own front door,
   you have tested a function, not a path.
4. Does the drop turn "I could not read this" into "this does not exist"? Absence
   and unreadability are different claims. Anything that renders them identically
   converts every downstream refusal into permission.
5. Is the filter on the other side of a deploy boundary? Then the two halves ship
   separately, and there is a window where the careful side runs against the
   dropping side. Say what degrades in that window.

**Law:** **a vocabulary is defined at the point of PRODUCTION, not at the point of
use.** When you add or reclassify a state, fix every filter between the producer
and you in the SAME wave — and where the producer is a separately deployed
artifact, gate on its build id so the careful branch is not silently unreachable.
An unreadable state must be SHIPPED, never omitted: the receiver can decide to do
nothing, but only if it is told.

**Sibling trap (how this entry itself nearly shipped wrong).** Having found the
filter, the obvious next sentence — "so Go never receives Unknown" — is FALSE,
because a second, unfiltered path carries the same value for a different purpose.
When you find a drop, establish which CONSUMERS it starves, not which value it
removes: the answer is usually "some of them".

**Corollary.** "The tests pass" is the expected outcome of this class, not
evidence against it. A fix whose branch cannot be reached is indistinguishable
from a fix that works, by every means except reading the producer.

## CLASS 86 — THE FIX THAT VALIDATES ITSELF (born 2026-09-07, fix/bracket-oco-separation, C1)

**Name.** A wave is dispatched against a stated mechanism. The mechanism is not
the cause. The fix is correct, ships clean, reviews well, and changes nothing —
and because the ticket closes, the real defect is now harder to find than before.

**Root cause.** The 2026-09-06 naked stop was attributed to OCO grouping: the
entry sharing an OCO id with its stop and target, so cancelling the entry took
the protections with it. It is an entirely plausible mechanism. NinjaTrader OCO
really does behave that way, the symptom matches exactly, and the remedy —
separate the groups — is sound engineering.

The entry had carried an EMPTY OCO group for some time
(`Account.CreateOrder(..., string.Empty, signalId, ...)`), and the stop and
target their own shared `"<signal>-exit"` id. The separation the wave was
dispatched to build was already there, with a comment explaining why.

The actual cause was twelve lines further down the same handler:
`HandleCancelOrder` cancelled the resting entry, and THEN, unconditionally,
cancelled `SlOrder` and `TpOrder` out of its own `placedBrackets` dictionary. The
entry had already filled, so the first half found nothing and only the second
half ran. Our own code reached across and killed the protections by hand.

Had the wave built what it was asked for, it would have separated two OCO groups
that were already separate, passed every test, deployed, closed the incident —
and left the naked-stop path fully open, now with a report saying it was fixed.

**Probe, five questions:**
1. Before building, can you make the CURRENT code produce the incident? Not
   "could this mechanism cause it" — "does the code in front of me do this". If
   the pin you write to prove the bug cannot be made to fail on today's tree, the
   premise is wrong, not the pin.
2. Is the named mechanism GENERIC to the technology (OCO, GC, retries, caching)
   or SPECIFIC to this code? A generic mechanism that matches the symptom is the
   easiest wrong answer to accept, because it explains everything and predicts
   nothing.
3. Which line, by file and number, does the thing? A mechanism you cannot anchor
   to a line is a hypothesis wearing a diagnosis.
4. Search the incident's own artefacts for the OTHER path. Snapshot 8208 showed
   two children and no entry — an entry-cancel that reaches children is one
   explanation; a handler that cancels children explicitly is another, and only
   one of them is in the file.
5. If the fix is correct-but-inert, what happens next? A closed ticket is not
   neutral: it removes the incident from the queue and makes the second
   occurrence read as a regression of a fix that was never load-bearing.

**Law:** **verify the premise against the code before building on it, and say so
in the report when it fails.** A wrong premise is a STOP and a correction, never a
build. The strongest signal is a bug-pin that will not go red: if you cannot make
the current tree fail the test that describes the incident, you have not found
the incident.

**Corollary.** The dispatch already required this (A17, "MEASURE FIRST"). What
made it work was writing the pin FIRST and watching it refuse to fail — the
mechanism was refuted by an artefact, not by an argument.


## CLASS 87 — THE CLEANUP THAT RUNS WHEN THE THING IT CLEANS UP AFTER DID NOT HAPPEN (born 2026-09-07, fix/bracket-oco-separation, adversarial review)

**Name.** A handler tidies up after an event. Its enclosing gate admits the
event's FAILURE modes as well as its success, and the tidying does not check
which it got — so a failed operation triggers the cleanup for a successful one.

**Root cause.** `OnOrderUpdate`'s actionable-state gate admits `Filled`,
`Rejected` and `PartFilled`. Below it, the `"-lx"` branch (the limit-then-market
exit) called `CancelBracketsFor(...)` with no state check at all. Its comment
states the justification plainly — "cancel the still-live bracket legs so they
can never re-enter the now-flat position" — and every word of that is conditional
on the exit having FILLED.

A limit exit the SIM rejects is a documented, ordinary event in this very file
("There is no market data available to drive the simulation engine"). On that
rejection the position is still OPEN, and the handler stripped its stop and
target. Same naked position as 2026-09-06, reached from the exit side instead of
the cancel side. A PART fill is the same trap more quietly: the remaining
quantity still needs the protection that just went away.

**Probe, five questions:**
1. Read the cleanup's own comment and extract the precondition it ASSERTS ("the
   now-flat position"). Is that precondition checked, or assumed?
2. What states does the enclosing gate admit? A cleanup written under a gate that
   once admitted only success is a time bomb the day a failure state is added to
   that gate.
3. Does the operation have a PARTIAL outcome? Partial success is where "it
   happened" and "it did not" are both wrong, and it is the case cleanups forget.
4. Is the cleanup DESTRUCTIVE and the operation RETRYABLE? Then the failure path
   destroys the state the retry needs.
5. Ask it as one sentence: "we are undoing X because Y finished" — then find the
   line that proves Y finished.

**Law:** **a cleanup names the outcome it cleans up after, and checks it.** Where
the cleanup removes protection, the default on any state that is not the success
state is to do NOTHING and say so.

**Corollary.** Found by an adversarial reader sent to map cancel paths, not by
the incident it duplicates and not by any test. The census that found it was
pointed at "every path that cancels a bracket" — the question was broad enough to
reach a door the incident report never opened.

## CLASS 92 — SCENARIO ECONOMICS: A PATH, AN ORDER TARGET, AND MISSING LEGACY DECLARATIONS

Assigned at merge for `fix/scenario-economics`, 2026-09-08, after a fresh two-format `sort -n | uniq -c` census: prior highest 91; 75/76/77 each duplicated twice, unchanged.

**Evidence:** frozen C1 45/111 first-listed target distances below 1R; C2 six under-2R authored arm geometries; C3 four off-path arm targets (plans 178/S1, 194/S3, 230/S4, 259/S1); C4 London plan 270/S2 targets a confluence level, S3 invalidates on a target level. Authored geometry is not execution or a loss rate. Research `982091d4d908f4a5b8b65022cedf5b8c7c8202d5` §§02/03/07/09 requires coherent obstacle/response/target/R; §09 says “Do not prescribe now: a mandatory 1R first target.” §05 preserves target/obstacle/invalidation uses of entry-excluded levels. C5 correction: E=p*b-(1-p)-c, break-even p=(1+c)/(1+b). C6 NOT ESTABLISHED, dropped.

**Law:** complete economics belongs to NEW AUTHORING only. Stored legacy UNKNOWN is first-class, never inferred/backfilled, never refused. The new-authoring parser refuses missing completeness and the three owner-approved contradictions: target off path without exception; obstacle beyond target; R inconsistent with geometry by more than one registry tick of price distance. Role differences and sub-1R obstacles WARN and count, never refuse. No target policy, R:R/stop floor, arm/gate/confirmation/cadence change. Process counters count authoring checks, including retries, not independent trades.

**Pins:** production parser RED→GREEN on real C3; missing new obstacle refused while legacy reads remain UNKNOWN/accepted; full writer retries then persists contract; 276 retained plan-read verdicts covering 799 scenarios match baseline exactly (394 scenarios in accepted plans, zero new legacy refusals); both London warnings; all six C2 UI ratios; actual D2/D4/call-site/counter mutations fail; Guide C5 table independent arithmetic. See `reports/2026-09-08-scenario-economics.md` and its pinned evidence. Boot line reads `ScenarioEconomicsBootLine`; card and desk show both Rs. Built/tested is not live proof.


## CLASS 93 — COMPUTED EVIDENCE LOST AT THE RECORDER BOUNDARY

Assigned at merge for `fix/stage-a-snapshot`, 2026-09-08. Fresh two-format `sort -n | uniq -c` census at integrated head `13017618`: highest occupied 92; existing 75/76/77 duplicates retained.

**Evidence (C1–C5):** candidate IDs 1–936 across 39 reads all have empty components; 469 cut IDs (enumerated in the report census) lost score/grade and share a cap string. The scorer had computed the terms before a bare-type projection discarded them. The dispatch's ordinal premise is corrected: 3,093 touch rows include 743 ordinal-1 rows; current valid IDs 733–737 advance 2–6 within their session, rather than resetting per read. Planner facts 1–56 carry default version/token/bias fields and lack the outer attempt/repair timeline. Historical bar receipt/availability and confirmation event clocks cannot be supplied by a writer or publication timestamp.

**Law:** capture the scorer's actual terms and each exclusion at computation, never rederive a score and call it captured. NULL means uncaptured, with its reason; real zero and computed [] survive. Keep observation, receipt, publication and permission independent. Record existing verdicts without evaluating again. Exclusion is not invalidation. Async telemetry drops with counted WARN and contains faults; trading decisions never read the research archive.

**Pins:** baseline empty-score RED; exact capture/reason, NULL/SQL NULL, clock binding, outer retry reply, manifest, queue/drop/budget and real producer-call mutations fail. Restored integration pins pass; 64 pre-wave scorer fixtures preserve seated JSON byte-for-byte. The report carries exact mutations, source freshness, per-field NULL dictionary, bounded admission and offline overhead measurements. Full merged-head validation and live rows are separate receipts. No scoring, gate, target, contract, cadence or order decision changes belong to this record-only wave.


### Class 93 follow-up — production path context is part of the startup pin

Stage A's first combined boot (`6f677b55`, 2026-09-08 18:11:54 CT) read schema=UNKNOWN. The separate scenario-economics lane reproduced, without changing Stage A, that `url.URL{Scheme:"file", Path:"data/data.db.research.db"}` serialized as `file://data/data.db.research.db`; absolute paths opened. Earlier tests all used absolute TempDir filenames and never exercised the service default. This is a repair of the same unshipped Stage A record, not a claim that the failed boot shipped research capture.

**Pin:** production `Start` → installed archive → `CurrentBootLineAt`, from a temporary working directory with the real relative path; persist a row, require actual schema and count, export the same archive read-only. Include spaces and URI punctuation. Removing either writer/reader path resolution fails; fabricating the schema on failed initialization also fails. Actual initialization failure still WARNs and remains UNKNOWN. No trading gate, policy, database configuration or existing trading rows change.


## CLASS 94 — VISIBLE BRAND IS NOT AN OPERATIONAL IDENTIFIER

**Root cause (Dispatch 102, C1):** the rebrand census mixed rendered product text, invisible CSS identifiers, binary examples, external providers/URLs and migration-bound names. Its approximate direct-string count omitted server-authored status and later Guide/dashboard/persona surfaces. Renaming all grep matches would change contracts; renaming only one frontend label would leave the actual server-authored message old.

**Probe:** render the real Go status-handler output through ChatMessages; check sender, placeholder, Vite-transformed HTML title and Guide heading; enumerate EN/ZH/ID. Mutate each shared display-name file and require RED. Pin the actual main logger call to the shared source, even against a visually identical literal. E2 preserves module/import targets and byte-pins units, binary paths, JWT, lock, log naming, wire and stored chat keys. A removed JWT guard and renamed import must be rejected.

**Law:** product display names come from `branding/product.txt` and `branding/persona.txt`, read by Go and the UI. Display-name imports may be added; no existing import target or operational/stored identifier is renamed in a visible-only wave. URLs, provider names, historical records and accurate technical examples remain explicitly inventoried. Browser/unit evidence is never presented as proof of an unobserved live boot. Dispatch 102 report: `reports/2026-09-08-brand-visible.md`.

## CLASS 95 — THE GATE IS ON THE PATH THAT CANNOT TRADE (born 2026-09-09, fix/session-risk-limits, C2 + the band addendum)

**Name.** A guard is written, wired, registered, covered by tests, and listed in
the knob registry as live — on a code path that the current mode forbids from
trading. It guards a door nobody walks through. Every check for "is it wired?"
answers yes.

**Root cause.** This desk has two entry paths: the DECISION path (the AI opens a
position directly) and the ARM path (a plan scenario places a resting order).
`plan_mode=strict` — the live setting — routes everything through the arm path:
*"plan_mode=strict executes plan scenarios on the ARM path only, and this is a
%s-path market entry"* (`trader/entry_gate.go`). The decision path cannot enter.

Two independent session-risk guards were found on the wrong side of that split
in one wave, by two different routes:

  · **the consecutive-loss breaker** — `consecutive_loss_halt`, registered
    `KnobLive` with a named consumer, wired at
    `trader/auto_trader_orders.go:250` inside `executeDecisionWithRecord`.
  · **the lunch / first-N no-trade band** — enforced at
    `trader/auto_trader_orders.go:281` and NOWHERE else.
    `grep -c` over `armed_executor.go` returned **0** for `InLunchNoTrade`,
    `InFirstNoTradeMinutes` and `sessionEntryBlocked`.

Both are real, correct, tested implementations. Both were unreachable by the
only path that places an order.

**Why the usual checks miss it.** A29 asks "does this function have a production
call site?" — and it does. The knob registry asks "is this knob consumed?" — and
it is, with a file:line. A wiring-gate test asks "is it called?" — yes. Every
one of those questions is about the FUNCTION. None is about the PATH, and the
path is where the trading happens.

**Probe, five questions:**
1. Name every path that can open a position in the CURRENT mode. Not every path
   the code has — the ones the live configuration permits. Which does the guard
   sit on?
2. Invert it: for the path that actually trades, list every guard it consults,
   in order. Anything absent from that list is not guarding this desk.
3. Does a mode/flag (strict, dormant, shadow) make one path unreachable? Then a
   guard on that path is dead code with a passing test suite.
4. `grep -c <guard> <the-file-that-trades>` — a literal zero on the file that
   places orders is the whole finding, and it takes one command.
5. If the mode changed tomorrow, would the guard start firing for the first time
   in production, untested against real flow? That is the same defect wearing a
   different hat.

**Law:** **wire a guard to the PATH, not to a function.** A guard's home is the
narrowest point every entry must pass through in the mode you actually run; if
two paths exist, either both consult it or the wave says in writing which one
does not and why. "It is wired" is an answer to a question nobody was asking.

**Corollary.** Neither instance was found by a test, a review or the registry.
The breaker was found by reading its own comment ("NOT gated by the guardrails
master") and asking which callers exist; the band was found by the owner running
one `grep -c` against the file that places orders.

## CLASS 96 — THE LATCH WITH NO AUTOMATIC RELEASE (born 2026-09-09, fix/session-risk-limits, D4(c))

**Name.** A flag is set by an automatic condition and cleared only by a human. Its
release is described in a comment, implemented in a function, and that function
is called from exactly one place: the manual operator endpoint. Nothing on the
recurring path ever calls it.

**Root cause.** `SetDailyForceFlat` records a daily-loss trip that blocks new
entries. `clearAllDailyForceFlat` lifts every trip. The comment above the state
says plainly: *"Cleared by the CME session-day reset (ResetDailyPnLAt), so a trip
lasts the session-day and lifts with the daily window."*

`MaybeResetDaily` — the once-per-cycle rollover detector — rolled the DATE and
did not touch the trips:

```go
if lastDailyResetDate == "" || lastDailyResetDate != today {
    lastDailyResetDate = today
    logger.Infof("daily window reset to CME session-day %s", today)
    return true
}
```

`grep -rn clearAllDailyForceFlat` returned its definition and ONE caller —
`ResetDailyPnLAt`, whose only production entry is `POST /api/risk/force-flat`. So
a tripped desk stayed blocked across the roll, the next session, and the one
after, until a human reset it or the process restarted.

**Nobody had seen it**, because the guardrails master is OFF and the trip has
never fired. A restart also clears it, since the state is an in-memory map — so
even a live occurrence would likely have been erased before anyone correlated it.

**Probe, five questions:**
1. For every latch that BLOCKS something: name the line that clears it, and the
   caller of that line. If the only caller is an operator endpoint, the latch is
   permanent in practice.
2. Does a comment promise a release the code does not perform? Grep the named
   clearing function and count its callers before believing the sentence.
3. Is the state in memory? Then a restart hides the defect, and "we have never
   seen it" is evidence about restart frequency, not about correctness.
4. Is the setting condition currently disabled (a master switch off, a feature
   flagged)? Then the latch has never been exercised and its release has never
   been observed — schedule the test, do not infer from silence.
5. Does the release belong to a CLOCK event (a roll, a session, a day)? Then the
   thing that detects that event is where the release belongs, and it is worth
   asserting that the detector clears it.

**Law:** **a latch is not shipped until its RELEASE has a test.** Set and clear
are one feature. Where the release is tied to a recurring boundary, the code that
detects the boundary performs the release — and a pin asserts that crossing the
boundary lifts the latch, because the comment saying so is the thing most likely
to be wrong.

## CLASS 97 — TWO READERS THAT HAPPEN TO AGREE (born 2026-09-09, fix/session-risk-limits, the wave's own boot line)

**Name.** One fact, resolved independently in two places. They agree on the day
they are written — which is why nobody notices they are two — and then one of
them is right for a reason the other does not share.

**Read beside class 82** (*a green word that answers a narrower question than the
reader will assume*) and **class 92**. 82 is what the reader sees; this is why it
was there.

**Root cause.** "Which risk knobs govern this desk?" had two answers in one
wave, written hours apart by the same author:

  · `deskGuardrail` (`trader/desk_facts.go`) read `at.config.StrategyConfig` —
    the trader's OWN bound row.
  · `bootRiskFacts` (`trader/session_risk.go`) scanned every strategy row and
    returned the first carrying risk-shaped fields.

Both were tested. Both passed. On a store with ONE strategy they return the same
thing forever, and the difference is invisible.

The live store held **nine**. The scan found `70695b25` — "New Strategy",
`daily_loss_enabled=true`, `consecutive_loss_halt=2` — a row **bound to no
trader at all**, sorting before the real one. The boot line printed
`daily_loss_enabled on · breaker=2` about a desk whose bound strategy
(`a5b7662e`) had the leg OFF and no halt knob.

The RUNTIME was never wrong: the gate itself reads the trader's config, so the
breaker really was the default 8. Only the line that exists to say what is
enforced was wrong — and it was wrong in exactly the class the same wave had
just fixed one function away.

**Why the usual checks miss it.** Each reader has a test; each test seeds the
data ITS reader expects. A29 asks whether the function is called — it is. A
review of either function alone finds nothing: both are correct implementations
of *a* rule. The defect is not in either place; it is that there are two.

**Probe, five questions:**
1. For any fact a surface reports, ask: how many functions resolve it? If the
   answer is more than one, they are already drifting; you are asking when, not
   whether.
2. Seed the ADVERSARIAL shape and see if they still agree — more rows than one,
   an orphan row, a row that sorts first. A fixture with one of everything is a
   fixture that cannot see this class.
3. Which reader has the AUTHORITY? The runtime path is the authority; the
   reporting path must resolve through the same join, not through a query that
   returns the same answer today.
4. Does one reader join and the other scan? A scan with no join is a guess with
   a stable seed.
5. If they disagreed right now, which would you believe — and does anything in
   the code make that the one it uses?

**Law:** **one source, both readers — never two readers that happen to agree.**
Where a surface reports what a gate enforces, it resolves through the gate's own
lookup. Two implementations that return the same value are not a redundancy;
they are a scheduled divergence, and the day they part is the day the surface
starts lying with a passing test suite.

**Corollary.** Found by its own boot line on its own cutover, not by review, and
not by any of the fourteen tests the wave added. The store on a developer's
machine has one strategy; the store on the desk had nine.

**Numbering note (2026-09-09).** `92` is now duplicated across this file's two
formats — `92. **A description that lives in a DB row nobody audits.**` and
`## CLASS 92 — SCENARIO ECONOMICS…` — joining 75/76/77. Left per A16 (never
renumber another lane's entry) and recorded here so the collision is visible
rather than discovered. Count with the two-format census in the NUMBERING HAZARD
note above; a single-format grep reports a free number that is taken.

---

## PENDING NUMBERS — appended by wave BARS HORIZON (2026-09-09), branch `fix/bars-horizon`

*Numbers are assigned AT MERGE (A16). Two classes, both found by measurement
during this wave and both fixed in the same branch.*

### (pending) A COUNT IS NOT A HORIZON

**Root cause.** A recorded or rendered COUNT — "2000 bars served", "8 rows" —
cannot express a SPAN or a HOLE, and every reader silently treats it as if it
could. Two instances, one in the prompt and one in the record:

- `kernel/planner_prompt.go` baked the row count into the table title as a
  literal and truncated only when the slice was LONG, so
  `"daily session candles (last 8)"` stood over 2 or 3 rows. Measured over the
  stored prompts (`planner_rejected_prompts`, n=54 carrying a Candles block,
  ids 70–142): 15m rendered 12 in 54/54, 1h 12 in 54/54, 4h 8 in 54/54, daily
  **2 rows in 19 and 3 rows in 35 — never 8, in 0 of 54**. The prompt then told
  the model "On conflict, trust the candles".
- `planner_read_facts` rows 64 and 66 were identical in every recorded field,
  yet id 66's tape spanned 3,055 minutes with **696 open-market minutes
  missing** inside it.

**Probe.**
1. For every count a reader is shown, ask what it would look like if the
   underlying window were HALF PRESENT. If the answer is "the same number",
   the count is not a horizon.
2. Compare the literal in a heading against the rows actually rendered, over a
   population of STORED artefacts — not one sample and not the code's intent.
3. For any bucketed/aggregated row, check whether the bucket's first element is
   assumed to be the window's start. `DailySessionBars` took the first bar it
   SAW as the session Open with no completeness check, so a tape starting at
   02:39 CT presented a part-session as a whole one.

**Law.** **Any surface that states a quantity of history states HELD vs
REQUESTED, and marks a partial window as partial** — including when it is
complete, because a disclosure that appears only on failure is one the reader
learns to skim past. Coverage is measured against the SAME calendar the
trading gate reads, an unclassifiable day is UNKNOWN and never guessed, and a
missing bar is MARKED — never interpolated, carried forward or synthesised.

### (pending) A CROSS-SESSION COMPARISON IS A REGIME COMPARISON, NOT A MEASUREMENT

**Root cause.** This wave's opening dispatch asserted, as fact, that the ATR was
inflated and that a resting arm was over-sized on it. The claim rested on
comparing **today's NY read against today's ASIA read**. ASIA and NY are
different volatility regimes; the comparison could only ever produce a
difference, and that difference was read as a defect.

Held to the same session, from `planner_read_facts`, the claim inverts:

| session | id | atr5m | stop_floor_pts | created (CT) |
|---|---|---|---|---|
| 09-08 NY | 54 | 36.7439730211274 | 55.115959531691 | 2026-09-08 10:02:43 |
| 09-08 NY | 55 | 32.2843805014897 | 48.4265707522345 | 2026-09-08 10:45:01 |
| 09-08 NY | 56 | 28.0296201145494 | 42.044430171824 | 2026-09-08 12:15:00 |
| 09-09 NY | 65 | 20.514183795639 | 30.7712756934586 | 2026-09-09 13:08:08 |
| 09-09 NY | 66 | 19.7631429163709 | 29.6447143745564 | 2026-09-09 13:18:13 |
| 09-09 NY | 67 | 16.6611226001067 | 24.99168390016 | 2026-09-09 13:55:06 |

Today's NY ATR is roughly HALF yesterday's NY ATR. Nothing was inflated.

**Probe.** Before any "X is elevated / depressed / wrong" claim: name the two
populations being compared and ask whether they differ in SESSION, day type,
calendar class or regime. If they do, the number measures that difference, not
the thing under audit. Re-run same-session, same-class, and quote both rows.

**Law.** **A comparison across sessions is a regime comparison.** A claim about
a level, a floor, a size or a threshold is only a measurement when both sides
come from the same session and the same calendar class, and the report quotes
the row ids on both sides (A21).

### (pending) A MARKER ON EVERY READ IS A MARKER ON NOTHING — and a COUNT cannot see a row that is NOT there

*Appended by the SAME wave, after review, 2026-09-09. Both instances are the
wave's own defect class reproduced one layer down, INSIDE the fix — which is
why they are filed rather than quietly patched.*

**Root cause A — the boundary that rounds the wrong way.** A disclosure that
measures "held vs expected" must decide what is expected AT `now`. The first cut
rounded UP to the end of the minute `now` falls in, counting the minute still IN
PROGRESS as an interval the tape ought to hold. Bars are delivered on close, so
on a PERFECTLY GAPLESS tape the newest row of all four tables rendered
`⏳FORMING ⚠PARTIAL — holds only 1309 of the 1310 open 1m intervals` while the
TAPE line six lines above it said `gaps 0` — a self-contradiction, on 100% of
live reads, under a prompt that tells the model to trust the candles. Nobody
noticed because the builder's own sample render contained it and read as normal.

**Root cause B — the row that does not exist is measured by nothing.** Row
coverage measures INSIDE a rendered row; the heading counts ROWS. An aggregator
emits no row for a bucket that holds no bars, so a whole absent bucket is
invisible to BOTH. A tape holding 18:00–19:00 and 20:00–22:00 CT (market open
throughout) rendered
`### 15m — HELD 12 of 12 requested rows · all held rows COMPLETE`
with FOUR whole 15m windows missing between two adjacent-LOOKING rows.

**Probe.**
1. Render the disclosure on a **HEALTHY** input. If it marks something, the
   marker is noise and the reader will learn to ignore it. Assert the healthy
   case explicitly — most suites only assert the broken one.
2. Render it at two clocks that differ only in whether the forming element has
   been delivered. If the output differs, the boundary is counting the element
   in progress on one side only.
3. Take any list rendered from bucketed data and ask what an EMPTY bucket
   produces. If the answer is "no row", then no per-row check and no row count
   can see it: the gap must be measured BETWEEN rows, on the grid.
4. Distinguish an ABSENT window from a CLOSED one. A weekend, a holiday or a
   maintenance halt has zero expected elements and must NOT be reported missing,
   or the marker becomes noise again (root cause A, by another door).

**Law.** **A disclosure is judged on the healthy case first.** The element in
progress is counted on NEITHER side. Gaps BETWEEN rendered rows are measured on
the expected grid, not inferred from the row count, and an unmeasurable gap is
UNKNOWN — never zero. And a completeness claim is scoped to what was actually
printed (`every row PRINTED here is COMPLETE`), never to rows that were never
rendered.

### (pending) A REPORT THAT CLAIMS NO BEHAVIOUR CHANGE WHILE THE BOOT LINE SHOWS ONE (class 82, restated)

**Root cause.** A wave corrected the INPUT to an estimator and then wrote, in
three shipped artifacts at once — the report, `SYSTEM-MAP.md` and a user-facing
guide card — that "the computed value did not change". The estimator was indeed
byte-for-byte unchanged; the value it produced moved, because the tape it was
fed got deeper. The parity test offered as proof built BOTH sides' inputs itself
(class 53), so it could not see the input move, and the guide's drift banner was
silent because `GUIDE_BUILT_REV` happened to equal the running rev.

**Probe.**
1. For every "unchanged" claim, ask **unchanged at which layer** — the function,
   or the value the system renders? Name the layer in the sentence.
2. Grep the wave's own boot/log lines for anything that reports a before/after.
   If one exists, the claim of no change is refuted by the wave's own output.
3. Enumerate EVERY consumer of a widened input, not just the one the wave was
   about. Here a 1m tape widened for candle tables also fed
   `CompletedWeekCount` (0 → 2), `WeeklyShadowRefs` (1 → 3) and
   `ComputeWeeklyFacts` — none of which the change inventory mentioned.

**Law.** **Name every value the wave moves, with its measured delta, in the same
document that claims the rule is unchanged.** A parity pin must exercise the
PRODUCTION CALL SITE, not a fixture both sides share. And when a change is
authorised by ruling rather than by scope, say so plainly — an authorised
expansion recorded as "no change" is indistinguishable from a scope violation.

## CLASS 98 — A REFERENCE ON THE MAP IS NOT AN ENTRY (born 2026-09-09, dispatch 103 W3)

*RENUMBERED AT MERGE from 95 by the combined-boot lane: `origin/dev` already carried CLASS 95 (THE GATE IS ON THE PATH THAT CANNOT TRADE) and CLASS 96 from `fix/session-risk-limits` when this entry was written against an older dev. A16 forbids renumbering a LANDED entry; numbers are assigned AT MERGE, so the unlanded one moves. Text is otherwise untouched.*

**Root cause (Dispatch 103, W3):** every seated level was handed to the model as if each were a place to trade. Three consequences, all measured on 2026-09-09 at rev `954f11b1`. (1) **29.8% of seats sat >100 pt from price** (n=84, ids 1009–1176, mean 150.1 pt) — the dispatch's premise said 22%, so it is worse, not better. (2) Overlapping references were counted as independent confirmation: a prior close, a VWAP band and a supply zone at one price each credited the others, and the credit is a direct multiplicative score factor `(1 + 0.20*effConf)` capped at 3, so one price wearing three names earned **1.40×** (1.60× at the cap). (3) **Nothing existed beyond the map**: on 2026-09-03 price ran +483 pt, left the highest seated level, and the plan then held no target while the fade book kept selling. `PWH`/`PWL` could never fill that gap — they are guarded by `priorWeekMinBars=4320` on a ~33 h 1 m ring, and `candidate_pool` has held **zero** PWH/PWL rows ever.

**Probe:** build the map view from `[]ScoredLevel` and assert on the VIEW, never on a score. Three references inside one zone-width must collapse to one candidate carrying three names and contributing one credit. A level whose nearest opposing reference is under the resolved minimum must be refused ENTRY CANDIDACY while remaining in the returned set with a role and a stated reason. Two candidates where the nearer scores lower must list nearest-first with both scores shown. With price beyond every mapped reference, at least one row above it must be a labelled projection carrying its method, and no projection may be an entry. `TestStageAScoreParityLegacy` must stay byte-identical against the 685,898-byte golden. Mutate the merge width, the target minimum and the ordering key and require RED on each — a surviving mutant means the pin never exercised the branch (the stop-floor default survived first pass here and needed its own pin).

**Law:** selection, ordering and presentation may change without touching a score. Overlapping references merge for the card, the model's table and the entry shortlist; the scored confluence term is NOT recomputed, because whether confluence is worth anything is unmeasured — that comparison is experiment E4 and a wave may not pre-empt it. A level is an ENTRY candidate only with an opposing reference at ≥ the resolved minimum (default: the stop floor), and that refusal governs candidacy only, never a scenario already authored. The entry shortlist ranks by reachability with the score carried and shown; every such rule is labelled `[I]` on the boot line, in the code and in the Guide until measured. Projections beyond the mapped range are TARGETS and OBSTACLES only, carry the word `projection` and their method, and carry no detector grade. **Prior-week anchors come from the DAILY bar source — never from a relaxed ring guard, which would compute a "prior-week high" from a day and a half.** Exclusion is not invalidation: see class 93, which this wave does not restate. A boot line may not print a per-read count before any read has happened, and may not print one trader's resolved value as if it were global. Dispatch 103 report: `reports/2026-09-09-candidates-not-entitlements.md`.

## Arm-state implementation of classes 99 and 107

A hand-typed lifecycle list is a second classifier. `store.IsTerminalArmState` now owns the terminal set; `TerminalArmStateSQL` is derived from that classifier, and `NonTerminalArmStateSQL` negates it. All ledger liveness readers use the Go predicate or its SQL. Watches/audits use `cmd/arm-state-sql` or `scripts/arm_state.py`. Unknown and NULL states are non-terminal; they cannot disappear from exposure reads. `TestArmStateNoRetypedLists` scans Go, Python, shell and SQL readers, including executable audit scripts and untracked source; `TestArmStateGrepRejectsRetypedListAnywhere` proves a copied list fails outside the original files. Historical captured receipts and broker-state classifiers are not arm-lifecycle readers.

Leg 4 compares broker orders against placed/unconfirmed non-terminal ledger rows. An `armed` row with no signal id is an authorization, counted separately on the informational `armed_unplaced` line. It never fails the leg. `place_pending` is working/unconfirmed; a missing signal on a pending/working/unknown row does not make it informational. Pins: two unplaced authorizations + empty broker PASS; pending placement + empty broker FAIL (unconfirmed); broker order + no ledger placement FAIL; copied list FAIL. Existing broker freshness and explicitly labelled pre-first-snapshot fallback are unchanged.

The class-33 boot sweep still has no scenario-validity adoption policy. Its state selection uses `SweepableArmStateSQL`, derived from non-terminal MINUS cancel_pending. The exclusion is deliberate: the sweep’s raw SetState must never bypass ConfirmCancel and its persisted snapshot id; confirmPendingCancels owns the excluded rows. The production sweep-to-settlement pin proves both halves; it still skips empty signal ids and cancels selected prior-process placed rows. Adopt-or-cancel by scenario validity remains a separate wave.
## CLASS 99 — A GATE QUERY THAT RETYPES THE TERMINAL SET (born 2026-09-09, dispatch 103 W3)

**Root cause:** the pre-cutover flat check was written as a hand-typed exclusion list — `state NOT IN ('filled','cancelled','canceled','expired','done')` — while the code's own predicate `isTerminalArmState` (`trader/one_contract.go:285-291`) returns true for **seven** states: `filled · cancelled · canceled · rejected · expired · superseded · shadowed`. The query matched five of the seven and invented a sixth (`done`) that the code never sets. On 2026-09-09 it counted **10** rows as resting arms blocking a cutover; the true count was **0** — every row was `superseded`, several of them days old. The wrong number was reported to the owner as the reason to hold.

**Correction of this entry, 2026-09-10 (A24).** This entry first said **11**, and the query never returned 11 — every reading in the transcript is **10**. The 11 came from a hardcoded shell label, `echo "--- what the 11 arms are ---"`, printed directly above output that read `10`, and it propagated from there into this entry, into `reports/2026-09-09-candidates-not-entitlements.md`, and into what was told the owner and a peer lane. A count typed into an `echo` above a query is a placeholder that reads as data; it survived because it sat beside the real number and agreed with the story being told. The entry about not hand-typing values carried a hand-typed value in its own headline — the class caught its own author, in its own text, five days running.

**Second instance — the mirror image, same predicate, same day, one hour later.** Lane nofx-07 ran, repeatedly between 21:05 and 22:21 CT on 2026-09-09 and inside three successive Monitor loops:

```sql
select count(*) from armed_orders where state in ('armed','working')
```

This was an **uncommitted operator query, not a code site** — there is no file:line to open, and it is recorded that way deliberately so no reader goes hunting for one. It is a POSITIVE list of live states, the mirror of the negative list above. At 22:12:53 CT it returned **0** while arm **143** rested at the broker in state `place_pending` (ASIA · S5 · SHORT · entry 29435.58 · stop 29464.48 · target 29372.50; the broker's own snapshot read `order_count=1 working_count=1` at 22:13:36; the arm later went `working` and FILLED at 29435.50). `place_pending` is in neither operator's list. Had the owner not independently ordered a wait, that gate would have called the desk flat and handed over a kill, and the class-33 boot sweep would have cancelled a live order placed 90 seconds earlier.

Reproduced 2026-09-10, three predicates against the same table at the same instant:

| predicate | form | rows |
|---|---|---|
| `isTerminalArmState` — the code's seven | negative | **0** |
| nofx-07's `IN ('armed','working')` | positive | **0** |
| this entry's five-of-seven `NOT IN` | negative | **10** |

The table has only ever PERSISTED three states — `cancelled` 77, `filled` 22, `superseded` 10. Every live state (`armed`, `place_pending`, `working`, `cancel_pending`) is real, code-set and transient, so none of them is visible in a snapshot. **`select distinct state` cannot catch this class**; only the code's predicate knows the full set.

**Probe:** for any query that decides whether it is safe to act, name the Go predicate it is standing in for and diff the two sets. `grep` the predicate, list its cases, and compare them to the SQL literal character by character. A gate whose SQL and whose code disagree is a gate that will hold when it should release, or release when it should hold — and the direction of the error is not predictable from reading either side alone.

**Law:** a gate query reads the code's terminal set; it never retypes it. **A negative list of terminal states that omits one OVER-reports live rows and fails SAFE; a positive list of live states that omits one UNDER-reports and fails OPEN — the gate calls the desk flat while a real order rests at the broker.** A hand-typed list is therefore not merely wrong, it is wrong in a direction that depends on which way you happened to type it, and that is the argument for reading the code's set rather than for typing a better list.

The structural remedy is to export ONE SQL fragment derived from the predicate, so the switch and every gate query have a single source. **Read class 107 before building it.** As stated, this paragraph is dangerous on its own: a lane that centralised every arm-state list onto a single `NonTerminalArmStateSQL()` pointed `store/boot_sweep.go` at a set that INCLUDES `cancel_pending`, and the sweep's raw `cancelled` write then re-cancelled rows whose cancels were sent but never confirmed. The three-state list it replaced was a deliberate, undocumented exception. The remedy is a single **SOURCE**, never a single **PREDICATE** — one name per intent (`SweepableArmStateSQL()` alongside `NonTerminalArmStateSQL()`), each carrying the comment that says why its set differs. **Attribution corrected 2026-09-10:** that work was NOT built by lane nofx-80. It exists, largely complete, on the unmerged branch `origin/fix/arm-state-predicate` @`45d677f5`, claimed by `arm-state-0b955fbc/root[unlisted]` on 2026-09-09 — a lane that has since ended. It exports `armStates` / `TerminalArmStateSQL()` / `NonTerminalArmStateSQL()` and retires every hand-typed list, and **it carries the class-107 regression described above**. Still not on dev (`origin/dev` keeps the safe three-state list at `store/boot_sweep.go:47`), and it must not merge until the sweep reads a named predicate. Finding, A/B and the owner-ruled fix: `reports/2026-09-10-boot-sweep-cancel-pending.md`. A caveat for whoever builds it, which is class 102 in this file seen from another angle: a `[]string` sitting *beside* a hardcoded `switch` does not close this class, it moves it — two hand-typed lists in one file diverge as readily as one in Go and one in SQL. It closes only when the switch ranges over the same slice the SQL is built from. Until then the fallback applies: the query quotes the predicate's file:line beside the literal and a test pins them equal, so a state added to the Go switch fails the test instead of silently widening the gate. The same rule covers any "is it finished / is it safe" list: order states, position states, plan lifecycle states. Related: class 53 (parity tests exercise production CALL SITES — a test that builds both sides' inputs proves only self-consistency). A worked example — the wrong query annotated in place beside the correct one — is preserved at `reports/2026-09-04-two-day-audit.md` §0. Dispatch 103 report: `reports/2026-09-09-candidates-not-entitlements.md`.

## CLASS 100 — A BRANCH ON A STALE BASE IS A DELETION PATCH (born 2026-09-09, dispatch 103 W3)

**Root cause:** this wave's branch was cut from dev's tip at accept and was correct for eight hours. While it held for a flat gate, a combined-boot lane merged it — plus three other waves — onto a shared boot head. Every one of this wave's files was then ALREADY on dev. The branch, still based on the pre-merge tip, no longer described "my work added"; it described "dev, as it looked before three other lanes landed." `git diff --stat origin/dev HEAD` read **37 files changed, 150 insertions, 5,403 deletions** — `bar_horizon_warn.go`, `regime_input_window.go`, `read_facts_horizon_test.go` (102) and `weeklyBias.ts` (101) among the casualties. Nothing conflicted. Nothing failed. A `--ff-only` merge was impossible, but an ordinary merge would have committed the deletions as an intended change.

**Probe:** before ANY merge, run `git diff --stat origin/dev HEAD` and read the DELETION count, not the conflict list. A wave that adds a feature should show deletions only in files it deliberately edits; a four-figure deletion count against a branch that added code means the base moved under it. Cross-check with `git log --oneline origin/dev..HEAD` and `git log --oneline HEAD..origin/dev` — the second list is what landed while you were not looking. Then confirm your own files: `git cat-file -e origin/dev:<path>` for each one you created. If they are already there, your work landed by another route and the branch is now a rollback of everything that landed after it.

**Law:** a branch is only as safe as its base is fresh, and staleness is silent — no conflict, no test failure, no hook. Diff against the CURRENT dev and read deletions before merging, every time, including when the branch has not been touched since it was green. When your own files are already on dev, do not merge the branch: reset onto the current tip and re-apply only the genuinely unlanded deltas, then re-run the same deletion check to prove the reset is additive. Related: the SPEC-FRESHNESS LAW (CLAUDE.md canon — a worktree cut from an older base freezes a moving spec; it has NO checklist slot, and CLAUDE.md's "Checklist class 73" names the hook class instead — see class 105 instance 3, which corrects this line) and PUSH-EMPTY-AT-ACCEPT, whose founding incident was a lane's branch merged into dev without its author ever being told. This is that incident seen from the author's side. Dispatch 103 report: `reports/2026-09-09-candidates-not-entitlements.md`.

## CLASS 101 — A BOUND THAT IS WRITTEN, PRINTED, AND NEVER COMPARED (born 2026-09-10, fix/lock-keeper-on-acquire)

**Name.** A field that looks like a constraint and is decoration. It is set at
creation, rendered on every status line, cited in documentation and in people's
reasoning — and no code path ever compares it to anything.

**Read beside class 88** (*a liveness signal that is a side effect of activity*),
which is the defect this one was found while fixing, and **class 97**.

**Root cause.** `deploy/nofx-lock.sh` wrote `expiry` at `acquire` and printed it
in every `status` line, ALIVE and STALE alike. Three occurrences in the file:
written once, printed twice. `cmd_heartbeat` refused on exactly two conditions —
no lock directory, and not the holder. **Past its expiry a lock beat happily,
forever.**

Everyone read the field as a bound. It appeared beside the holder and the task on
every status line a lane looked at, and lanes reasoned with it out loud ("expiry
16:53, so I have an hour"). Nothing enforced it, and for months nothing needed
to — because the tool started no keeper, so the only writer was a human running
the verb by hand and locks went stale on their own.

**Then the keeper wave made it load-bearing.** Bounding the keeper's own loop by
the expiry looked sufficient and was not: it constrains the keeper THIS SCRIPT
starts and nothing else, and every lane on the machine had been running a
hand-rolled beater for exactly as long as the tool had failed to start one. The
first design would have shipped an invariant that held only for the writer that
did not exist yesterday.

**The fix is where, not what.** Refusing at `cmd_heartbeat` — the single place a
heartbeat can be written — makes it an invariant for EVERY writer, hand-rolled or
not, and terminates the keeper for free because its loop breaks on the same
non-zero rc. Bounding the loop would have been the same rule enforced at one of
its callers.

**Probe, five questions:**
1. For every field that reads like a limit — expiry, deadline, max, ttl, cap —
   grep it. Count the sites that WRITE it, the sites that PRINT it, and the sites
   that COMPARE it. A comparison count of zero is the finding, and it takes one
   command.
2. Do people reason with the field in prose, tickets or chat? A decorative bound
   is most dangerous exactly when it is trusted, and being quoted is the evidence
   that it is.
3. If you are about to make it load-bearing, ask who else writes the thing it
   bounds. Enforcing in your own new code path constrains your own new code path.
4. Where is the narrowest chokepoint every writer must pass? Enforce there. A
   rule enforced at a caller is a rule with as many holes as there are callers.
5. When it starts being enforced, does anything now FAIL that used to pass — and
   does the failure explain itself? A bound that begins biting silently reads
   exactly like the bug you were fixing.

**Law:** **a bound is enforced at the point of WRITE, or it is a comment with a
timestamp.** Where a field constrains an action, the code that performs the
action compares it — not the code that happens to have started the actor.

**Corollary.** Found by a peer lane reading the shipped file while the fix was
still staged, and its own caveat was the useful part: it had read dev's pre-fix
copy, so half its finding was moot and it said so. The half that survived was
architectural and better than my design — enforce at the source, not at the loop.

## CLASS 102 — THE FIX THAT REBUILDS ITS OWN DEFECT ONE LAYER DOWN (born 2026-09-10, fix/lock-keeper-on-acquire, adversarial pass)

**Name.** A wave fixes a defect and introduces the same defect class inside the
fix — because the fix adds a new actor of exactly the kind the original defect
was about, and the wave's attention is on the old actor.

**Root cause.** The lock's header names its three founding failures, the second
being *a live pid silently overwritten by a second writer*. The keeper wave added
a background heartbeat writer — the first the tool had ever had — and then
stopped it by killing the loop only. The loop runs its beat as a foreground
CHILD, so killing the parent ORPHANED that child, and `_write_meta` mv's into
`$LOCK_DIR/meta` by absolute path with no identity check.

An orphan that had passed `_require_holder` while A held the lock landed A's meta
into the lock B created at the same path seconds later. B held the tree, the lock
said A: **B could not release its own lock, and A — holding nothing — could**,
freeing the tree under a live cutover. The atomic `mv` is what made it silent;
the wrong content landed whole, never torn. Failure (2) from the file's own
header, rebuilt one layer down by the wave that existed to make holding safer.

**Why the wave could not see it.** Every test was about the OLD failure — does a
waiting holder stay alive, does the keeper stop at expiry, is the pid never
liveness. The new actor was the subject of the fix and therefore not the subject
of suspicion. It was found by an adversarial pass told to attack the fix, and
four of that pass's findings were pre-existing; only this one was created by the
wave.

**Probe, five questions:**
1. Does your fix introduce a new WRITER, PROCESS, CACHE or FILE? Then re-read the
   defect list this component already has and ask which entries now apply to your
   new thing.
2. Read the component's own header or postmortem list. A file that documents
   three failures is telling you which three to re-check against every change.
3. If your fix spawns something, what kills it — and does that reach everything
   it spawned? A process that spawns children needs its GROUP ended, not its pid.
4. Where is the identity check on the write? "The right process is writing" is
   not the same claim as "this write belongs in this object", and only the second
   survives a race.
5. Would an attacker told "break this fix" find it in an hour? If you have not
   asked someone to try, the wave's tests are all arguing for the same side.

**Law:** **the defect list a component already carries is the test list for any
change to it** — most of all for a change that adds an actor of the kind those
defects were about.

**Corollary.** The pin for it took four attempts, and the first three passed with
the defect fully present: it asserted the keeper FILE was gone (`rm -rf` does
that anyway), then the PROCESS but on a beat so short an unstopped keeper exited
by itself first (measuring the OS, not the code), then SAMPLED the race eight
times against a natural rate near one in sixty. It bites only with a CONDITIONAL
timing shim that parks exactly one write. **A race pin that does not widen its
window is testing luck**, and three drafts of mine reported success from it.

## CLASS 103 — THE FALLBACK THAT HIDES THE FAILURE OF THE PATH IT BACKS UP (born 2026-09-10, one hour after the keeper shipped)

**Name.** A primary path fails totally and permanently; a fallback beneath it
produces a plausible value; behaviour is therefore correct, and every
behavioural test passes. The defensive code is dead and the hazard it was
written to defend against is live — and nothing in the observable behaviour of
the system says so.

**Read beside class 102** (*the fix that rebuilds its own defect one layer
down*), which this was found while smoking, and class 88.

**Root cause.** `_spawn_keeper` reads the keeper's process GROUP from
`/proc/PID/stat`, because `release` must kill loop and child together and the
comment two lines above states plainly that `$!` is *not reliably the group
leader*. The line was written with the `'"'"'` form — the correct way to embed a
quote inside an ALREADY single-quoted string. It was not inside one. At top
level bash read `{print $5}` in a **double**-quoted region, expanded `$5` against
the function's own empty argument list, and `set -u` aborted the substitution.

Two things followed, both live on dev for an hour:

1. Every `acquire` printed `line 143: $5: unbound variable` to stderr. The verb
   still succeeded and still printed its success line, so the warning read as
   noise attached to a working command — the shape a lane learns to scroll past.
2. The `/proc` read never executed once. `pgid` came from
   `[ -n "$pgid" ] || pgid="$kpid"` — from `$!`, the exact value the comment
   above it says cannot be trusted.

**Why it survived 75 green tests.** Behaviour was correct. `setsid` execs rather
than forks when it is not already a session leader, so on this platform
`pid == pgid` and the fallback's answer happened to equal the right one. Every
assertion in the suite was behavioural — does the keeper beat, does release end
the group, does no writer outlive its lock — and behaviour was right for a
reason that had nothing to do with the code under test. **The suite was
measuring the platform, not the implementation.**

**What actually found it.** Running the shipped verb once, by hand, and reading
its stderr — before announcing it to other lanes. Not the suite.

**Probe, five questions:**
1. Does the verb write anything to stderr on the SUCCESS path? Run it with
   `2>&1 >/dev/null` and look at what is left. A command that must warn in order
   to succeed has an unexamined failure inside it.
2. For every fallback (`||`, `or`, `except:`, a default on a nil read), ask: if
   the primary path never ran at all, what would I observe? If the answer is
   "nothing", the fallback is a mask and needs its own assertion.
3. Is there a comment explaining why the primary path is necessary? That comment
   is a testable claim. Here it said `$!` is unreliable — so a test should prove
   the code is not using `$!`, and none did.
4. Did the value come out right for a reason the code controls, or for a reason
   the platform happens to guarantee today? Change the platform assumption and
   see whether the test still passes.
5. Are your assertions all behavioural? Behaviour is downstream of the fallback.
   Assert on the ARTEFACT — stderr, the recorded value, the syscall — when the
   defect can be invisible downstream.

**Law:** **a fallback must be observable when it fires.** Where code has a
primary path and a backup, something must record which one ran — a counter, a
log line, or a test that asserts the primary's own output. Otherwise the backup
silently becomes the only path, and the first evidence is the day the platform
assumption changes.

**Corollary — pin the artefact, not the outcome.** The pin that catches this is
`acquire writes nothing to stderr`, mutation-tested to fail on the shipped script
with the exact `$5: unbound variable` line. No behavioural assertion could have
caught it, because there was nothing wrong with the behaviour.

## CLASS 104 — THE CORRECTION THAT IS MORE DANGEROUS THAN THE DEFECT (born 2026-09-10, minutes after class 103)

**Name.** Broken code is inert. Fixing it makes it *run* — and the code that had
never executed carries a hazard nobody reviewed, because until now it did
nothing. The repair is the moment the latent bug goes live.

**Read immediately after class 103**, which is the defect this is the correction
to. The pair is the lesson; neither half is complete alone.

**Root cause.** Class 103 was a `/proc` group read that never once executed —
mis-quoting aborted the substitution and a fallback silently supplied a working
value. The obvious fix was to make the read work.

Making it work exposed a race that the broken version had been hiding:

> Job control is off in a non-interactive shell, so a background job does **not**
> get its own process group — it starts in the SHELL'S. `setsid` moves it only
> once it execs. `kpid=$!` returns before that.

So a `/proc` read that WINS the race returns the **parent's** pgrp. That value
landed in `keeper.pid`, and `_stop_keeper` ran `kill -TERM -- "-$pg"` against it
— sending SIGTERM to the process group of whoever invoked the script. **It killed
the test run that found it, exit 143**, and the exit code was first misread as an
unrelated environment problem.

**The broken version was accidentally safer.** It always fell through to
`pgid="$kpid"`, and `$kpid` after `setsid` genuinely IS its own group leader. The
defect and the safety were the same line. Removing the defect removed the safety.

**Probe, five questions:**
1. You are fixing code that never ran. What does it DO once it runs? Review it as
   NEW code, because operationally it is — it has never executed in production
   even once.
2. What was the broken path doing INSTEAD, and was that behaviour load-bearing?
   A fallback that has served for months is the de-facto implementation; the
   "real" path is the untested one.
3. Does the newly-live code compute a value that something DESTRUCTIVE consumes —
   a kill, a delete, a truncate, a force-push? Then the fix is not done until the
   consumer validates what it is handed.
4. Is there a race between recording a value and that value becoming true? `$!`,
   a pid before exec, a row before commit, a file before rename — all give a
   correct read of a not-yet-correct state.
5. After the fix, did an unrelated thing start failing? An exit 143, a killed
   runner, a vanished shell — do not attribute it to the environment before
   grepping your own diff for the signal it sends. Here the entire codebase
   contained exactly ONE `SIGTERM` and it was the new code.

**Law:** **a fix to code that never executed is a new feature, not a repair** —
and it earns a new feature's review, especially where it feeds a destructive
operation. Corollary: **never signal, delete, or overwrite a target you have not
positively identified.** `kill -- -N` on a number that is not a verified group
leader signals a group you did not create; the fix is identification
(`/proc/<pid>/cmdline` names this lock), not a narrower race window.

**Corollary — two layers or none.** The fix here is at the WRITER (accept a pgid
only once `pgrp == pid`, true exactly when `setsid` completed) *and* at the
CONSUMER (signal only a positively-identified target). Layer one alone still
trusts whatever is already in the file; layer two alone still writes a dangerous
value for anything else to read.

## CLASS 105 — DOCUMENTATION DESCRIBING CODE, IN A PLACE THE CODE'S TESTS CANNOT SEE (born 2026-09-10, dispatch 103)

**Root cause:** a statement ABOUT the code — a cap, a verb, a sequence, a rule — written where nothing can compare it to the code it describes. It is correct until the code moves, and from then on it is wrong silently and for as long as anyone leaves it. The build passes, the suite passes, review sees nothing, because no test reads prose. Three instances, across the whole range: one in tracked source that tests still cannot reach, and two in a file git cannot reach at all — the second of which caught the author of this entry mid-draft.

**Instance 1 — a comment in tracked source.** `api/handler_svp.go:50`:

```go
// Default 5m. Pull up to 2000 bars (the cache cap); sessions off the visible
// range are skipped by the renderer.
bars := provider(symbol, interval, 2000)
```

The cache cap is **2500** — `DefaultBarCacheMaxBars` (`provider/ninjatrader/bar_cache.go:24`), which `market/data.go:225` names correctly as "2500 = the BarCache cap". What makes this sharper than drift: **2000 is not a stale number.** It is `AISVPBarCount` (`kernel/svp.go:47`) and it is the correct argument to pass. The comment was REWRITTEN at `f94118e6`, which replaced "1m/2000 matches the AI's exact input" with "2000 bars (the cache cap)". The value survived the rewrite; its description did not. A reader now learns a wrong cap from a line sitting directly above correct code — and a reader who later "fixes" the code to match the comment would break the AI's SVP input. Nothing failed, because a comment is unreachable from a test even when the file it lives in is fully covered.

**Instance 2 — a rule in an untracked file.** `CLAUDE.md:205` instructed every lane on this machine:

```
deploy/nofx-lock.sh heartbeat <session>                    # beat every ~2 min as you work
```

On 2026-09-10 the lock-keeper wave (`417599a3`, classes 101–104) made `acquire` start the heartbeat itself. Hand-beating became a **second writer into the lock dir** — the precise failure that wave existed to close. The one file instructing every lane to hand-beat was the one file the wave could not touch: `CLAUDE.md` is **untracked**, so no branch could correct it, no review could see it drift, and no test could assert it still matched the script. This is the worse half of the class. Instance 1 misleads a reader; instance 2 **instructs** one, and a lane following it faithfully would have caused the defect the wave had just removed.

**Instance 3 — the one that caught the author of this entry, while writing it.** `CLAUDE.md`'s SPEC-FRESHNESS block ends: *"(Checklist class 73, read beside 70 and 72.)"* Checklist slot **73** is **"A hook registered one start too late"**, and slot 73's own text records why: it was renumbered 69→70→73 at merge because "70/71/72 were taken by the lock, stash-stack and flaky-clock classes landing in the same boot." SPEC-FRESHNESS was numbered on its branch, the number moved under it at merge (A27), and the untracked file kept the branch number. **SPEC-FRESHNESS has no checklist slot at all** — it is CLAUDE.md-only canon, and the only two occurrences of the string in this file are the Related lines discussed below.

Drafting this entry, I read that citation, believed it, and wrote "class 73 (SPEC-FRESHNESS)" into the Related line of **class 100 — which is on dev**, and into the first draft of this one. It was caught only by checking every citation against the file before merge, and only because this file's own ⚠ NUMBERING HAZARD section (added 2026-09-07) says it carries two heading formats, which forced a second grep in the other format. The same paragraph also cost a second wrong citation: "the prompt feeds forward" is **slot 50**, not 45, and slot 50's own text says so — *"(Dispatch 'class 45'; checklist slot 45 was already the pantry class, hence 50.)"* The dispatch's name for a wave and its merged slot number are different facts, and a memory or a doc that records the first will keep asserting it after the second is decided. Class 100's Related line is corrected in the same commit as this entry.

**Probe:** for every statement about the code that a reader could ACT on — a cap, a limit, a default, a verb, an ordering — ask two questions, in this order. **Is it in a file git tracks?** If not, it cannot be corrected by a wave, and its being wrong is not a bug anyone can fix on a branch. **Is there a test that fails when the code moves?** If not, being right today is luck. Then check the statement itself: a comment that names a value should name the CONSTANT (`DefaultBarCacheMaxBars`), not a transcription of it, so a reader who follows the name arrives at the truth. Be most suspicious of parentheticals that explain what a number *is* — `2000 (the cache cap)` — because the number is verified by the compiler and the gloss by nobody. When a comment is rewritten rather than written, diff what the prose asserted before and after: values are reviewed, descriptions are not.

**Law:** a rule about the code lives in a **tracked file with a contract test**, and untracked guidance **points at it** rather than restating it. A restatement is a second copy that drifts from the code and from the original, independently and silently; a pointer cannot be wrong about anything except where to look.

Partial remedy already on dev: `docs/superpowers/CLAUDE-canon.md` (landed `557494c7`) mirrors the MAIN-TREE LOCK LAW into a tracked file, and it declares itself newer by construction because it is the copy a wave can reach. That closes the git half. **The test half is not closed** — `git grep CLAUDE-canon -- '*.go' '*.sh'` returns zero, so nothing asserts the mirror still matches `deploy/nofx-lock.sh`, and a mirror with no contract test is this class with one more copy in it. The pointer half is not closed either: `CLAUDE.md` cannot be made to point at the canon file by any wave, only by the owner. Until both halves land, treat the canon file as authoritative over `CLAUDE.md` and the script as authoritative over both.

Related: **slot 50** (the prompt withheld what the validator enforces — a document that instructs a reader to do the thing a guard forbids; the dispatch called it "class 45", the merged slot is 50), the **SPEC-FRESHNESS LAW** (CLAUDE.md canon — it has NO checklist slot, and CLAUDE.md's claim that it is class 73 is instance 3 above), and the **GUIDE CONTENT LAW**, which is this law already applied to one surface: a guide that lies about the running binary is worse than no guide.

## CLASS 106 — A CORRECT READ OF A NOT-YET-CORRECT STATE (born 2026-09-10; generalised out of class 104 at a peer's suggestion)

**Number note (updated at merge):** 105 is now OCCUPIED on dev by *documentation
describing code where the code's tests cannot see it*. A second lane also claims
105 on `origin/docs/worktree-tmp-locked-prune` (locked worktrees making a
dangling registration immortal) — **that branch must renumber before it lands.**
Numbers land at merge per A27, and this collision is exactly what A27 exists to
catch: two lanes both read "highest is 104" and both took the next one.

**Name.** You read a value. The read succeeds, the value is real, and it is the
right value *for the state the system is in at that instant* — but that state is
about to change into the one you actually meant to ask about. Nothing errors.
Nothing is null. The read is simply early, and an early read of a mutable
identity is indistinguishable from a correct one.

**Read after class 104**, which is the instance this was generalised from, and
beside class 88.

**The instance.** `kpid=$!` after `setsid nohup bash -c '…' &`. Job control is
off in a non-interactive shell, so a background job does **not** get its own
process group — it starts in the SHELL'S, and `setsid` moves it only once it
execs. `$!` returns before that. So `/proc/$kpid/stat`'s `pgrp` field is a
perfectly valid read that returns the PARENT's group, and the consumer then
signalled it: `kill -TERM -- "-$pg"` against the invoking shell's process group.

**Why this is its own class and not a shell footnote.** The shape has nothing to
do with process groups. The value is real before the operation that determines
what it *means*:

| read | the operation that gives it meaning |
|---|---|
| a pid | the `exec` that changes what that pid IS |
| a row | the commit that makes it visible/durable |
| a filename | the rename that puts it at its final path |
| a branch diff | the base moving under it (SPEC-FRESHNESS, class 73) |
| a config value | the reload that makes it the running config |
| a price/quote | the fill that makes it a transacted price |

Every one of them reads fine at the moment you read it. Retrying does not help,
because there is no error. Logging does not help, because the logged value looks
right. **Only a predicate that is FALSE before the transition and TRUE after it
distinguishes the two states** — here, `pgrp == pid`, which is true exactly when
`setsid` has completed and is impossible for a value borrowed from the parent.

**Probe, five questions:**
1. Between reading this value and using it, is there an operation that changes
   what the value MEANS rather than what it is? Name it. If you can, you have
   this class.
2. What predicate is false before that operation and true after? If you cannot
   state one, you cannot detect the early read — and a sleep is not a predicate.
3. Does the early value look VALID? The dangerous case is when it does. A null or
   an error is a gift; a plausible wrong number is this class.
4. Who consumes it, and is that consumer destructive? An early read feeding a
   log is a cosmetic bug; feeding a kill, a delete, or an overwrite, it is an
   incident.
5. Does a retry loop "fix" it? If the loop has no predicate it is not waiting for
   the transition, it is waiting for luck — and it will pass in testing.

**Law:** **wait on the predicate, not on the clock, and re-validate at the point
of use.** Where a value's meaning is established by a later operation, the reader
waits for a condition that operation makes true, and the consumer re-checks
identity before acting — because the reader does not control who wrote the value
it is handed.

**Corollary.** Found because the early read reached a `kill`. It had presumably
been early many times before that without consequence, which is the ordinary
career of this bug: invisible until it feeds something that bites.

## CLASS 107 — CENTRALIZING A HAND-TYPED LIST IS A BEHAVIOUR CHANGE WHEREVER THE LISTS DIFFERED (born 2026-09-10, boot-sweep cancel_pending)

**Name.** Several sites hand-type the same list and the lists disagree. You fix
that — correctly — by deriving one predicate from a single source and pointing
every site at it. **The differences you just erased were not all typos.** Some
were deliberate exceptions that nobody wrote down, and each one becomes a defect
the moment the sites agree.

**This is the INVERSE of class 99**, and must be read with it. 99 is *two
hand-typed lists diverge and one silently widens a gate*. 107 is *the remedy for
99, applied without asking why each site's list is the shape it is.* Fixing 99
without 107 trades a divergence bug for a uniformity bug — and the uniformity bug
is harder to see, because the code now looks principled.

**Root cause.** `store/boot_sweep.go:47` hand-typed
`state IN ('armed','place_pending','working')`; `store/armed_orders.go:242`
hand-typed the same four states **with** `cancel_pending`. Five such lists
existed across two files, with three distinct memberships. The one-predicate wave
exported `NonTerminalArmStateSQL()` from a single `armStates` map and pointed the
sweep at it.

But the sweep's omission was **load-bearing**. Its terminal write is
`SetState(id, "cancelled", …)` — a raw update — while `ConfirmCancel` is
documented as *the only way a row becomes 'cancelled', and it requires the id of
the snapshot whose book no longer listed the order.* `cancel_pending` means a
cancel was sent and NOT confirmed: **the order may still be live at the broker.**
Sweeping it marks the row cancelled with no evidence — the precise blindness a
previous wave had closed. A/B on one seeded row: dev `swept=0`, row stays
`cancel_pending`; branch `swept=1`, row `cancelled`, `settled_snapshot_id=0`.

**Why nobody caught it.** Three reinforcing reasons, all of which generalise:

1. **The doc comment already disagreed with the code.** `ListPreBoot`'s comment
   said it "returns ONE trader's non-terminal rows" while the SQL listed three of
   the four non-terminal states. Anyone checking intent against implementation
   read that as the bug — and "fixed" it.
2. **The suite stayed green.** Every existing test seeded `working` rows. No test
   named `cancel_pending` and the boot sweep together, because the exclusion had
   never been written down as a behaviour.
3. **The path is barely trodden.** In all history 5 rows ever requested a cancel,
   max attempts 1 against a cap of 5, and `ConfirmCancel` had never once fired.
   A regression on a cold path ships green and stays quiet.

**Probe, five questions:**
1. Before unifying, DIFF THE MEMBERSHIPS and list every element that appears in
   some sites and not others. That set is the entire risk surface, and it takes
   one command.
2. For each difference, ask "what does this site DO with the rows it selects?" A
   site that only READS can usually widen safely. A site that WRITES, cancels,
   deletes, or signals cannot.
3. Is the narrower list the one attached to the destructive action? Then assume
   deliberate until proven otherwise — A24's never-list, applied to refactoring.
4. Does a comment near the site disagree with the code? Do not assume the code is
   wrong. Find out which one is load-bearing BEFORE aligning them; here the
   comment was wrong and the SQL was right.
5. After unifying, does any test fail? If none does, that is not reassurance.
   **A test suite cannot distinguish "this path is correct" from "this path is
   never taken", and centralisation is exactly the kind of change that touches
   many paths while being exercised on few.** Count the production rows that
   ever went down the differing branch before you trust a green run: here it was
   5 cancel requests in the entire history, max 1 attempt against a cap of 5, and
   `ConfirmCancel` had never once fired. **A regression on a cold path ships
   green.** (Generalisation owed to the author of class 99.)

**Law:** **a shared predicate needs a NAME PER INTENT, not one name for all
callers.** Where two sites legitimately select different sets, derive BOTH from
the single source and give each its own named function whose comment states why
it differs — `SweepableArmStateSQL()` (non-terminal MINUS `cancel_pending`,
"because the sweep's write bypasses ConfirmCancel") alongside
`NonTerminalArmStateSQL()`. One source, several named intents. The class-99
remedy is the single SOURCE, never the single PREDICATE.

**Corollary.** Found only because the dispatch ordered *establish whether the
omission is deliberate BEFORE changing anything, and quote the code path*. Asking
"is this a bug or a decision?" first is what separates 99 from 107; the
refactor had already been written, tested and pushed on the other reading.


## CLASS 108 — A SOURCE GUARD THAT SCANS NOTHING (assigned at arm-state cutover follow-up merge, 2026-09-10)

**Finding:** TestTZGuardSingleTimeSource searched for a directory basename ending in nofx and swallowed walk errors. `/tmp/nofx-arm-state` therefore scanned `/kernel`, `/trader`, `/api`, `/agent`, read nothing and passed. A clean clone named nofx exposed four pre-existing timezone violations. A successful process exit was not evidence that the guard had examined source.

**Law and pin:** resolve the package's actual repository parent, require go.mod and propagate directory/read errors. A restored bare layout must fail in a worktree whose name does not end in nofx. The four renderers now use canonical CT helpers with byte-identical output. This guard correction is independent of arm-state classification and never weakens a terminal-state or flat-gate check. Receipt: `reports/2026-09-10-arm-state-cutover.md`.

## CLASS 110 — A GREEN SUITE IS A CLAIM ABOUT AN ENVIRONMENT, NOT ABOUT A COMMIT (born 2026-09-10, settlement wave)

**Number note:** 108 is three-way contested at birth — dev holds *a source guard
that scans nothing*, and `docs/worktree-tmp-locked-prune` and
`fix/episode-contract` each carry a different 108 unmerged. 109 is claimed by
`fix/episode-contract`. Taken as 110 per A27: the census gives the ceiling, only
the merge assigns the number.

**Name.** Two lanes run the same suite on the same commit and get opposite
answers. Both are honest, both reports are internally consistent, and neither is
falsifiable by the other — because neither recorded the environment the suite ran
in. The disagreement is not the defect. **The unfalsifiability is.**

**Root cause.** A tamper-guard on a protected file went red on a legitimate
change. One lane reported the guard *blinded* — twelve test files dying at import,
sixty tests never running. Another lane (me) reported the guard *working and
correctly red*, with 413 of 414 tests passing, and "corrected" the first.

Both measurements were real. The variable was the installed runner:

```
tracked web/package-lock.json pins   vite 6.4.3   → 12 files fail at import
the deploy tree actually had         vite 6.4.1   → suite runs, guard legibly red
web/package.json declares            ^6.0.7       → admits both
```

Settled by a controlled experiment — same worktree, same commit, same config,
`npm install vite@6.4.1 --no-save`, then `npm ci` to restore — reproducing both
directions on one variable. **The repo's DECLARED state was the failing one**, so
the green was the artifact: a stale install predating the lockfile move. Any
`npm ci` — fresh clone, new worktree, the clean-clone deploy path — gets the red.

**The two failure modes are not equally bad.** *Environment recorded and differs*
is a productive disagreement: two lanes compare, one reinstalls, done in a
minute. *Environment not recorded* is not a weak result, it is a **non-result** —
and it is what both reports had. What made them falsifiable was two agents
arguing, which is not a mechanism anyone can rely on.

**It is not about test runners.** It is about anything **installed rather than
committed**: the Go toolchain, sqlite3, node itself, a linter's version. This
repo's single most documented gotcha is already this class — NT8 compiles
NinjaScript only from the Windows AddOns path, so editing a repo `.cs` does
nothing until copy → F5 → **full restart**. A file in the repo that is not the
artifact actually running is the same defect wearing different clothes.

**Probe, five questions:**
1. Does the report state the resolved version of every tool whose output it
   cites? A pass count with no runner version is a characterisation standing in
   for the ref — read it the way A21 makes you read a claim about rows with no
   sample ids.
2. Does `node_modules` (or the venv, or the toolchain) agree with the LOCKFILE?
   `npm ls --depth=0`, `go version` against `go.mod`. Disagreement is the finding.
3. When did the install happen? A stale install is invisible in git and invisible
   in the test output, and it is the only thing that differed here.
4. If a peer reported the opposite result, could you tell which of you is right
   from the two reports ALONE? If not, neither report is evidence yet.
5. Is the artifact under test the one the repo describes, or a copy that was
   built, installed or deployed earlier? Ask it of binaries and AddOns, not only
   of packages.

**Law:** **record the environment beside every result you cite, or the result is
not evidence.** One line is enough — resolved version plus lockfile-agreement
status — and its absence should read as loudly as a missing sample id.

**Corollary.** Found because a peer refused to accept my correction and ran an
experiment instead of a restatement. My "correction" was a single observation in
an environment I had not pinned, offered against a bisect; I also matched their
error text to mine (`Denied ID` from vite's `fs.allow` vs `ERR_MODULE_NOT_FOUND`
from node's resolver — different layers) on the strength of "the import fails",
which is this file's recurring failure in miniature. Their own first mechanism
was wrong twice before the version skew surfaced. **Neither of us got there
alone, and nothing in either report would have gotten there without the other.**
