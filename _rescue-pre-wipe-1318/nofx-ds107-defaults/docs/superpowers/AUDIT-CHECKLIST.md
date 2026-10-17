# AUDIT-CHECKLIST — the permanent audit playbook

Codified 2026-08-28 from the campaign's then-18 proven bug classes; 53 as of
2026-09-03. **Every audit
dispatch MUST reference this file instead of re-deriving the probe list. Every
NEW bug class found gets appended here in the SAME PR that fixes it** (canon law
in CLAUDE.md).

---

## PART 1 — THE BUG CLASSES (name · root cause · probe · law)

*Highest occupied class: **268** (2026-09-24). Numbers are assigned AT MERGE and
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
    `git worktree add ../nofx-<task>` + `git worktree lock` — with the add's exit code
    CHECKED and the `cd` target VERIFIED, per the recipe in CLAUDE-canon.md §WORKTREE
    LAW (a lane once `cd`'d into another lane's tree after a failed add and pushed an
    empty claim onto their branch; cleanup batch 2 B6).

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
   *Instance 2026-09-24 (WAVE 3b-A):* `NOFX_RELEASE_DIR` was read in TWO
   packages — `api/release_dir.go` latched it behind a `sync.Once`, while
   `kernel/boot_integrity.go:230` called `os.Getenv` on every use. One
   identifier, two resolutions, and they DISAGREE the moment the environment
   moves: the process serves a bundle from one release while judging its boot
   integrity against another, each half internally consistent. **Resolving
   "once" per package is not resolving once.** Fixed: one resolver in
   `internal/installpath`, consumers pass through, exactly one `os.Getenv`
   left in non-test code. The api file's own comment had named this exact
   hazard — a comment warning about a hazard is not a control.

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

    **Instance 2026-09-24 M3:** (c) the CTO's dictated boot-runbook line predicted a phantom-epoch owner re-login from FOLD-M3-B without reading the code: tokens live 24 h (`auth/auth.go:146` at 0cd6df49), the `updated_at` ALTER is Postgres-only (`store/user.go:32-42`) and the live store is SQLite. The CTO put it on the record as "class 45/49 shape, mine this time", and the ha2 verifier's corrected line (no re-login expected) replaced it. A claim about what the running process will show, written where nothing compares it to the process; read with 105 (CTO on the record; ha2 verifier)

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

    **Instance 2026-09-24 M3:** **OPEN.** `ValidJobID`/`ValidReleaseID` are defined in both `internal/updateauth/ids.go` and `internal/updaterwire/ids.go`. They agreed on 2,000,014 and 2,402,098 differential samples, but only the comment was fixed and no parity pin exists [A: grep]. Owed: one source, or a parity pin such as `TestIDAllowListsAgreeAcrossAuthAndWire` (`34f24525`)

53. **One question, two answers: a predicate shared by two callers that fed it
    different inputs.** (Numbered 51 at merge against a tree that did not
    yet carry class 50's entry; renumbered to 53 by owner ruling 2026-09-02 —
    class 50 keeps 51, the no-trade band keeps 52. Class 46 is deliberately

    **Instance 2026-09-24 M3:** the Telegram bot's re-mint was tested only through its predicate `botTokenStale`, so a compiling revert of the call in runBot stayed green, and the bot would then 401 after every password change (ha-verify #1). The re-mint now lives in `botIdentity.refresh` and is pinned at that call site: `TestBotRefreshReMintsAfterAPasswordChangeAtItsCallSite`, `TestRunBotMintsOnlyThroughRefresh` (`d637b7e1`)

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

    **Instance 2026-09-24 M3:** FOLD-M3-B. `users.updated_at` was promoted to "credential epoch", but a legacy or migration value (the Postgres-only `ALTER … updated_at DEFAULT CURRENT_TIMESTAMP`, `store/user.go:42`) reads as a password change and retires older sessions. Documented and pinned by `TestLegacyUpdatedAtIsAnEpochWithoutAPasswordChange` (`b849e9eb`)

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

**Law amended 2026-09-23 (W-EXEC-TRUTH W2 A4, CTO amendment msg 1790166603535; see the obstacle-chain CLASS 191 at the end of this file):** the obstacle chain is no longer free prose. At NEW-authoring write it is a REFUSAL when target_chain is not sorted outward from entry in the trade direction, when first_obstacle is not the nearest SEATED level strictly between entry and the arm target (a capacity-cut candidate nearer on the path is accepted, never required; an empty seated path requires the arm target), when a seated level on that path is missing from `economics.path_levels` (refused by name), and when `reduce` rides a single-contract arm. The prompt's "a reduce response is a declared intention" sentence is RETIRED — the two never coexist. Off-tick authored chain prices are normalized to the tick grid and recorded, never refused. Role/use differences against a level's instruction and sub-1R obstacles still WARN only. Legacy reads are unchanged.

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

## CLASS 172 — A COUNT IS NOT A HORIZON

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

## CLASS 173 — A CROSS-SESSION COMPARISON IS A REGIME COMPARISON, NOT A MEASUREMENT

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

## CLASS 174 — A MARKER ON EVERY READ IS A MARKER ON NOTHING — and a COUNT cannot see a row that is NOT there

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

## CLASS 175 — A REPORT THAT CLAIMS NO BEHAVIOUR CHANGE WHILE THE BOOT LINE SHOWS ONE (class 82, restated)

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

**Instance 2026-09-24 M3:** eight M3 sites where prose described code that no test read: the admin.go crash comment (`a785fe45`); "censuswalk is test tooling only", now asked of the toolchain (`87255ef9`); `ids.go`'s "updateauth imports these" (`34f24525`); the guide's "refused once" and the same words in `auth/retire.go` (`06ca207d`, `efdf3988`); the census header's "spelled ONLY in paths.go" and `paths.go`'s "the ONLY place" (`3e368492`, `964396ef`); and the guide's bot sentence, true only for the bot's own account (`4699edce`). The census's false "can only over-report" is recorded in CLASS 259 (`a785fe45`, `87255ef9`, `34f24525`, `06ca207d`, `efdf3988`, `3e368492`, `964396ef`, `4699edce`)

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

**Instance 2026-09-24 M3:** Q8 on /api/updates is documented as STRICTER than the shared `auth.RetiredBy`, but the only same-second pin probed where the two agree, so moving Q8 onto `RetiredBy` stayed green (ha-verify #2). Pinned by `TestUpdatesQ8StaysStricterThanTheSharedRetireRule` (a never-changed row and a NULL row): one name per intent (`8f4b2d9b`)


## CLASS 108 — A SOURCE GUARD THAT SCANS NOTHING (assigned at arm-state cutover follow-up merge, 2026-09-10)

**Finding:** TestTZGuardSingleTimeSource searched for a directory basename ending in nofx and swallowed walk errors. `/tmp/nofx-arm-state` therefore scanned `/kernel`, `/trader`, `/api`, `/agent`, read nothing and passed. A clean clone named nofx exposed four pre-existing timezone violations. A successful process exit was not evidence that the guard had examined source.

**Law and pin:** resolve the package's actual repository parent, require go.mod and propagate directory/read errors. A restored bare layout must fail in a worktree whose name does not end in nofx. The four renderers now use canonical CT helpers with byte-identical output. This guard correction is independent of arm-state classification and never weakens a terminal-state or flat-gate check. Receipt: `reports/2026-09-10-arm-state-cutover.md`.

**Instance 2026-09-24 M3:** the worker import guard asserted a package-count floor (20 vs 21 roots), which could not say which root went missing; every guarded root must now be walked (`911e69a3`)

## CLASS 109 — A CENSUS THAT CANNOT SEE ITS OWN THIRD FORMAT (born 2026-09-10, fix/episode-contract)

**Root cause.** A16 says take a checklist number by `uniq -c` census, never `uniq`
alone, and lanes have been passing around a TWO-format census (`N. **…**` and
`## N. …`). This file has THREE shapes, and a two-shape census reported the ceiling
as 93 while 104 already existed. I took 93 for a rider on that count; it is now a
duplicate.

**This entry has itself been miscounted twice, in opposite directions, and both
corrections are the class.** nofx-b3 found the first while taking a number next to
mine:

- **Over-count.** A bare `grep -cE "^[0-9]+\. \*\*"` returns 100 and is wrong by
  11. It sweeps in the PRE-CUTOVER PROTOCOL's ordinary numbered steps
  (`1. **Tree gate:**`, `2. **Build:**` …) and bolded lists inside class BODIES.
  Fix: scan only ABOVE the protocol heading and above the first `## CLASS`.
- **Under-count.** b3's suggested `.**`-suffix filter returns 68 and is wrong by
  **22** the other way: it requires the title to end `.**` on its FIRST line, so
  every entry whose title WRAPS is dropped. The exact set, because a range here
  would repeat the error this entry is about:

        33 36 37 38 39 40 41 43 44 45 49 50 51 52 53 55 57 60 66 67 82 84

  All 22 are real classes — 33 is cutover safety, 40 is P&L truth, 45 is
  prompt-feeds-forward. **Note the hole at 42**: its title fits one line and
  survives, so "36–45" is wrong and b3 caught me writing it that way. A filter
  that fixes an over-count by inventing an under-count has not made the number
  true, only differently false — and a RANGE that approximates the damaged set
  is the same sin one level down.

**The corrected census at this commit**, and the duplicate list corrected with it:

    PART 1 entries (above the protocol heading) : 89   max 93
    '## CLASS N' headings                       : 26   max 111
    '**CLASS N' (third shape)                   : 0    count it anyway, it existed
    UNION = distinct classes                    : 110  ceiling 111

    TRUE duplicates (a number in BOTH formats)  : 75, 76, 77, 92, 93
    NOT duplicates                              : 1–7

**1 through 7 were never duplicates**, and the reason is stronger than "they were
miscounted". Earlier drafts of this very entry listed them as collisions. Measured:
`## CLASS 1` … `## CLASS 7` have **ZERO** headings between them, so not one of them
CAN be a cross-shape collision. What they are is the same shape repeating — classes
1–7, plus the pre-cutover protocol's steps 1–7, plus a third numbered list for
1, 2 and 3, which appear three times each rather than twice.

A same-shape repeat and a two-shape duplicate are different findings with different
fixes, and the census that cannot tell them apart reports both as "duplicate". A
census artifact was recorded as a collision, in the entry warning against census
artifacts, and a peer then carried the wrong list because I published it.

**Law.** Count every shape that starts a class, bound the region you count, and
then ASSERT the union against the file. A census is a claim about a file and must
be checked against it — "highest is 93" was falsifiable in one command and nobody
ran it, myself included, for three waves. **Probe:**

    b=$(grep -nE '^## CLASS [0-9]+' "$F" | head -1 | cut -d: -f1)
    p=$(grep -nE '^#+ .*PRE-CUTOVER' "$F" | head -1 | cut -d: -f1)
    # PART 1: distinct numbers above BOTH boundaries, no suffix filter
    awk -v n="${p:-$b}" 'NR<n' "$F" | grep -oE '^[0-9]+\. \*\*' | grep -oE '^[0-9]+' | sort -nu | wc -l
    grep -cE '^#+ CLASS [0-9]+' "$F"
    grep -cE '^\*\*CLASS [0-9]+' "$F"

Take the UNION for the ceiling, not the sum — the sum double-counts every number
that exists in two shapes, which is exactly the five true duplicates above. If
your shapes do not reconcile against the classes you can count by eye, your
ceiling is wrong; and verify by eye anyway, because **a count that matches for the
wrong reason is what this note exists to stop.**

**And a census is only true at the instant it runs.** These two classes were
written as 105/106 against a census that was correct when it ran. They were
renumbered FOUR times before landing, across five dev tips in one day:

    105/106  →  dispatch 103 merged its own 105 mid-rebase
    106/107  →  106 taken (class 104 generalised) and 107 taken (boot-sweep)
    108/109  →  108 taken by the arm-state lane, MERGED, so it held
    109/111  →  109 was uncontested and stayed; the other moved to ceiling+1

At the third collision, 108 was contested three ways at once — the arm-state
lane's (merged, so it won), nofx-b3's, and mine. The merged one holds and BOTH
unmerged ones move; that is the whole rule, and it needs no adjudication because
merge order already decided it.

That is A27 working, not failing: a number is not yours until the merge that
lands it, and the right response to the fourth collision is the same as to the
first. **The census tells you the ceiling; only the merge assigns the number.**
If renumbering at merge feels expensive, note that the alternative — reserving a
number at accept — is what produced the 75/76/77/92/93 duplicates above.

**Two lanes can also just talk.** Before taking 111 I messaged nofx-b3, whose
108 also had to move, and offered them 111 or 112 rather than letting us both
re-census into each other. Coordination is cheaper than a fifth renumber, and
the branch name on origin is the only claim this protocol has (class 70).

**Law.** Count every shape that starts a class, then assert the count against
the file: `grep -c` per format must sum to the number of classes you believe
exist. A census is a claim about a file and must be checked against it —
"highest is 93" was falsifiable in one command and nobody ran it, myself
included, for three waves. **Probe:** `grep -oE "^[#*[:space:]]*[0-9]{1,3}[.)]"`
plus `grep -cE "^#+ CLASS [0-9]+"`; if the shapes you counted do not add up to
the classes in the file, your ceiling is wrong.

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

**THE WALL CLOCK IS A ROW IN THAT TABLE (second instance, same day).** Hours
after this entry was filed, dev's Go suite was 31 ok / 0 fail at 11:26 and 11:58
and RED at 12:11 — eight `trader/` tests, one machine, one commit, one install,
nothing touched. The clock had crossed into the lunch no-trade band
(12:00–13:30 CT) that the session-risk wave added, and the tests were calling a
wall-clock entry point instead of the `…At(now)` seam beside it. It would have
gone green again at 13:30 on its own.

**This instance is strictly nastier than the version-skew one**, and it is why
the class is not a dependency-management story. The first needed two divergent
installs. This one needs no divergence at all: two lanes on identical trees, at
identical commits, with identical `node_modules`, will disagree if one runs at
12:15 and the other at 11:55 — and both will be certain, and neither report will
contain the one fact that explains it. **The hour of the day is an environment.**
So is the day of the week, the session calendar, and whether a holiday half-day
is in force.

(The underlying test defect is class 60 — the entry owns the clock, the rule
takes it as an argument. What makes it belong HERE is that the failure was
reported as a property of a commit by two lanes who did not record when they ran.)

**Probe, five questions:**
1. Does the report state the resolved version of every tool whose output it
   cites — **and the wall-clock time the suite ran at**? A pass count with no
   runner version is a characterisation standing in for the ref, read the way
   A21 makes you read a claim about rows with no sample ids; a pass count with
   no timestamp is the same thing for any rule that consults a clock.
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

**2026-09-13 correction follow-up (PR #116):** race-enabled CI caught a class-32
fixture restoring the global bars provider while an unrelated weekly-backfill
worker still read it. Seed the completed weekly plan for tests of session/data
scheduling, or join any spawned worker before teardown; do not count an ordinary
non-race pass as evidence of safe fixture lifetime. Keep the intended production
call-site assertions intact.

## CLASS 111 — THE UNIT AN EXPERIMENT NEEDS, WHICH THE RECORD NEVER HELD (born 2026-09-10, fix/episode-contract)

**Root cause.** Every experiment measures value PER OPPORTUNITY, and the system
had no such unit. It had levels, touches (`touch_outcomes`, 4,860 rows resolving
HOLD/BREAK/AMBIGUOUS per touch), scenarios (`plans.doc`), arms (`armed_orders`)
and trades (`trader_positions`) — and `plan_id` is the ONLY column common to all
four. Nothing across them identifies a level, a scenario, a window or an order,
so "this level was reachable from T1 to T2 with these terms" could not be
expressed at all.

**The measurement that mattered was a refutation of my own claim.** A join on
(plan_id, version, session) returns 1,205 rows and looks like the missing link.
It is a CROSS PRODUCT: 481 distinct touches × 9 arms × 7 positions, with one
plan-version pairing 176 touch rows against 2 armed scenarios. A join whose row
count exceeds every input's distinct count is a fan-out, not a correspondence —
**check the distinct counts of each side before believing a join exists.**

**The second refutation killed the fix as specified.** The link was to be
resolved "at seat/authoring time, not by price-matching at read". But
`PlanScenario` carries NO level reference — ID, Trigger, Condition, Direction,
TargetChain, Invalid, Confirm, Quality, Fvg, Breakdown, ChainAfter, Arm, and
Trigger/Invalid are free text. `kernel/scenario_state.go` already said so: "To
evaluate a scenario we must first decide WHICH LEVEL it is about, and that
resolution is a heuristic." Moving a heuristic earlier does not make it identity
— it performs the same guess sooner and stores it where it reads as a fact.

**Law.** A unit of analysis is defined by what can be JOINED, not by what can be
named. Before building a record, prove the join with distinct counts on both
sides. Where a link is a heuristic, NAME it one: the column is `ScenarioNearest`,
it carries its basis (`price_proximity` / `two_scenarios_within_band` /
`nearest_outside_band` / `no_scenario_at_seat`) and both distances, and ambiguity
is NULL rather than nearest-wins. The band is read from the map's own cluster
width, never restated (class 97).

**And the honest backfill is zero.** Over 4,860 in-era rows: 4,677 blocked by
absent formation (96.2%), 183 by the scenario link being a new column, **0
recomputed**. That is the research's claim measured rather than argued — a
never-confirmed setup is not a missing row someone can recover later; the inputs
were never written down. Per-opportunity figures begin at the boot that ships
this. **Probe:** before promising a backfill, count the rows that hold every
input it needs — if that count is zero, say so in the dispatch rather than in
the report.

## CLASS 112 — A CONFIGURED VALUE THE CODE SILENTLY DOES NOT UNDERSTAND (born 2026-09-10, dispatch 103 W-TF)

**Root cause:** an identifier crosses a boundary in a spelling the consumer does not recognise, and the consumer's rejection path is a bare `continue`. The setting is present, correct, and named FIRST; the feature it asks for simply never happens, and nothing anywhere says so. This is not a missing feature and not a bug in the consumer — both sides are individually reasonable. It is a **missing join between two vocabularies**, and the silence is what makes it survive.

The instance. The bound strategy's `planner_timeframes` reads `["D","4h","1h","15m","5m"]` and has since the default was written (`store/strategy.go:1407`). `"D"` is the daily timeframe and it is the FIRST entry. The level-detection gate `isHTFDetectionTF` (`levels_assemble.go`) recognised `"1d"` and had never heard of `"D"`, so every planner cycle passed `"D"` into `DetectHTFLevels`, hit `if !isHTFDetectionTF(tf) { continue }`, and dropped it. **Measured 2026-09-10 at running rev `8941ec68`: 0 of 297 stored plans carry a level from any daily timeframe** (C4), while the store holds 1d n=1902 back to 2019-05-02 and 1w n=384 back to 2019-04-26 (C2). The owner had configured daily structure and had never once received it.

**Why nothing caught it.** Every individual check passed. The config was valid — it round-tripped through the strategy loader unchanged. The gate was correct — it was written when the daily family genuinely was not supported, with a comment explaining why (`"TFs below 15m and \"D\" are skipped"`), so the exclusion was *documented and deliberate at the time* and simply outlived its reason. The detector ran, on the timeframes it did understand, and produced levels; a map full of 1h references looks exactly like a working map (C4: `·1h` 160 · `·5m` 72 · `·15m` 30 · `·4h` 8 across the 40 most recent plans). There was no error, no counter, no empty result — the request for daily was not refused, it was **never asked**. Compare class 88, where a signal stops because the work stops: here the work never started and the absence had no shape.

A second vocabulary gap sat beside it, unfired: `trader/auto_trader_planner.go:2051` states that `"D"` maps to the provider's `"1d"` interval. `structureSummaryLines` performs no such mapping. A comment describing a join that does not exist is class 105 in the same file as the join that was missing.

**Probe:** take every CONFIGURED identifier — timeframe, symbol, session name, mode, strategy key, account label — and trace it to the predicate that consumes it. Ask two questions and accept only measured answers. **(1) Does the consumer's vocabulary contain the exact string the config produces?** Not a similar one; the exact one, compared character by character, with the config's real live value read from the database rather than from a default in the source. `"D"` and `"1d"` are the same timeframe to a human and different strings to a `switch`. **(2) When the consumer rejects a value, what does an operator see?** If the answer is "nothing", the gap is unfalsifiable from outside and will be found by someone auditing the feature, not by anyone running it. A `continue` with no record is the whole class.

The cheapest detection is a count nobody has to request: emit, per configured value, whether it was CONSUMED or SKIPPED and why, and keep those two sets disjoint. `DetectHTFLevelsReport` does this — `Counts` and `Skipped` are separate maps, so "produced nothing" and "was never read" stop being the same observation.

**Law:** a configured value is either **honoured** or **refused out loud**; there is no third state. Where two components name the same thing differently, ONE canonicaliser owns the translation and lives where the value ENTERS the consumer (class 28), never at each use site. A consumer that silently discards input it does not recognise is indistinguishable from one that has no input, and the difference is invisible for exactly as long as nobody audits the feature — five months, in this instance, across every plan the machine has ever written. Related: class 105 (documentation describing code, in a place the code's tests cannot see — the comment claiming the `D`→`1d` map exists), class 107 (centralising a hand-typed list is a behaviour change wherever the lists differed — the same two-vocabularies shape, seen at merge time instead of at read time), class 88 (a liveness signal that is a side effect of activity — the neighbouring failure where a signal stops because the work stops, rather than never starting), and class 28 (one canonicaliser per identifier, at the boundary). Wave report: `reports/2026-09-10-every-detector-every-timeframe.md`, C1–C4.

## CLASS 113 — A GATE THAT CERTIFIES A NAME, NOT A PATH (born 2026-09-10, from two lanes' findings converging)

**Name.** A guard is written to prove a property of the system, and what it
actually proves is a property of an IDENTIFIER. The name is present, registered,
called, delegated — and the guard reports success. Whether the PATH through that
name has the property is a question the guard never asks, and cannot.

**Three instances found the same day, in different subsystems, by two lanes who
did not know they were describing one thing:**

1. **A29 wiring gate — a call from DEAD CODE counts as wiring.** Deleting the only
   production call to a wrapper left the inner function still "called", by the
   wrapper that had just become unreachable. **An unwired wrapper satisfies the
   gate for everything inside it.** The mutation survived; the gate stayed green
   on three of four items in a shipped boot.
2. **Clock-seam lint — a registered entry point vouches for everything beneath
   it.** `maybeManageArmedOrders:maybeManageArmedOrdersAt` was registered and BOTH
   halves verified — the `…At` variant exists, the entry is a one-line delegate.
   Both green for a full day while `runArmedPlacement`, called one line below the
   clock its caller had been handed, read `time.Now()` itself. The clock was
   threaded to the door and dropped. **Green meant "this entry delegates", never
   "this path is seamed"**, and the two are indistinguishable in a suite.
3. **The test-side seam check cannot even be written textually.** "No test calls a
   seamed entry point" needs to know whether `.Save(` is `at.Save(` or
   `db.Save(` — `clock-seams.list` contains entries named `Save` and `observe`.
   Matching text gives 35 hits, most of them false. The check is blocked on the
   same missing capability as (1) and (2).

**The common shape.** Each guard matches a NAME — in a registry, in a call
expression, in source text — and infers a property of the CALL GRAPH from it.
That inference is unsound in both directions: a name can be present on a path
that lacks the property (1 and 2), and a name can be absent from a path that has
it (3). **All three want the same thing and none of them has it: resolve the
graph instead of matching the token.**

**Why this class is expensive rather than merely wrong.** These guards are the
ones people rely on to stop looking. A green wiring gate says "it is wired"; a
green seam lint says "the clock is controlled". Both were consulted, both
answered, and both answered a narrower question than the one asked — which is
class 82's shape reaching the tools we check our own work with. (2) cost most of
a day and produced two wrong published mechanisms before the real one.

**Probe, five questions:**
1. Does the guard resolve a call graph, or match a string / a registry key? If it
   matches, it certifies a name.
2. For a registered ENTRY POINT, what asserts the property holds BELOW it? If
   nothing walks the callees, the registration covers exactly one function.
3. For a "this is called" check, is the CALLER reachable? A call inside dead code
   is a call. Delete the outermost production call site and see whether the guard
   notices.
4. Can the guard be defeated by a rename, a wrapper, or one more layer of
   indirection? Add a wrapper deliberately and re-run it.
5. What does GREEN entitle a reader to conclude? Write that sentence out. If it
   is narrower than what people use the guard for, the gap is this class.

**Law:** **a guard over a graph must resolve the graph.** Where it matches names
instead, its green means only "the name is present as expected" — and it must SAY
so, in its own failure text and its own header, so nobody spends a day treating a
token match as a property of the path.

**Corollary — the remedy is one piece of work, not three.** A receiver-aware pass
(`go/ast`, not `strings.Contains`) closes the callee walk, the dead-code caller
check and the test-side check together, because all three are the same query
against the same graph. Filed as OWED rather than built: it is a tool, it is
outside every current wave's footprint, and three separate half-measures would
cost more than the one pass.

## CLASS 114 — THE VERIFIER IS WRONG, AND ITS WRONGNESS READS AS A RESULT (born 2026-09-10, fix/cleanup-batch-1)

**Name.** A check written to prove something produces a confident answer while
being incapable of producing the right one. The answer is not an error, a crash
or a blank — it is a plausible verdict, which is why nobody re-reads it.

**Root cause.** Every check embeds a belief about the thing it inspects. When
that belief is wrong, the check still runs, still returns, and still reports. It
is the *verifier* that has drifted, not the subject — so re-running it, adding
more of them, or making them stricter does not help.

**Three instances, all in one wave, all found only by disbelieving a result:**

1. **A proof that can never pass.** The dispatch specified `git diff -w --stat`
   reading zero as proof that a `gofmt` diff is whitespace-only. It cannot read
   zero: gofmt also **reorders imports** and **expands single-line `if` bodies
   onto three lines**. Both are semantics-preserving; neither is whitespace, and
   `-w` shows them. A lane trusting the stated method would have concluded the
   gofmt had smuggled in a real change. **The correct proof — `gofmt(before)`
   byte-identical to after — is also strictly stronger**, because it additionally
   shows no hand edit rode along, which `-w` could never have shown.
2. **A guard that fails closed on a healthy system.** `[ -d "$W/.git" ]`, written
   to verify a worktree, reported failure on a perfectly good one: **in a
   worktree `.git` is a FILE** holding a `gitdir:` pointer, not a directory. The
   guard was correct-looking and green in its author's head. Remedy: ask the tool
   (`git rev-parse --is-inside-work-tree`), never guess at the tool's layout.
3. **A verdict inferred from an absence.** A mutation harness read the absence of
   `--- FAIL` as SURVIVED. Four times in one week that absence meant the sed
   never matched (class 89) or the mutant never compiled — the experiment had not
   run at all. Remedy: `scripts/mutate.sh`, which proves the edit landed and the
   mutant builds *before* it will read the suite, and names NOT-APPLIED and
   BUILD-FAILED as distinct verdicts.

**Probe, four questions.** (1) State what this check would print if the thing it
inspects were PERFECTLY healthy — then run it on a healthy case. A check that
marks a healthy subject is measuring itself. (2) State what it would print if the
subject did not exist at all. If that is indistinguishable from "fine", it cannot
report absence. (3) Does the check infer a verdict from something NOT being
present? Absence is only evidence once you have proved the experiment ran.
(4) Does the check encode a belief about a tool's internals (a file layout, an
exit code, an output format)? Ask the tool instead.

**Law:** **a verification method is itself unverified until it has been run
against a known-good and a known-bad case.** Quote both. Where a check embeds a
belief about a tool, the check asks the tool rather than reproducing its rules —
and where a verdict rests on an absence, the absence is only admissible after the
experiment is shown to have run.

**Relation to its neighbours.** This is the general case of **class 89** (a wave
verified by a toolchain that cannot see half of it) and the mirror of **class
105** (documentation describing code where tests cannot see it): 105 is prose
that drifts from the code, 114 is a *check* that drifts from the thing it checks.
Both are invisible for the same reason — the artifact keeps producing a
believable answer. Related: class 100 (dev moving under a branch), which this
wave hit **twice in one hour** despite a peer's explicit warning, because the
deletion count was read at merge time and then again a minute too early; the
remedy is to rebase and re-check *immediately* before the merge, since dev moves
in minutes.

## CLASS 115 — A LABEL THAT IMPLIES COVERAGE IT DOES NOT HAVE (born 2026-09-10, fix/fade-permission, dispatch 101 W2)

**Root cause.** A guard is specified FROM a motivating incident and shipped
without being replayed AGAINST it. The specification reads as though it would
have caught the case that justified it, and nobody checks, because the incident
is the reason the guard exists and that feels like proof.

**The evidence.** W2 was dispatched to give the level-fade book a permission
step after 2026-09-03, when it sold into a +483-point run. The dispatch named
three candidate exclusions derived from that day: opening range too wide, IB
broken and held by 09:30, price beyond the map. Replayed against the day itself
(C2, n=3 arms, ids 35/36/37):

    (a) OR too wide         09-03 OR = 62.25 = 0.77x the 13-session median.
                            BELOW median. Cannot fire at any k >= 1.0.
    (b) IB held by 09:30    09:30 close 29287.75, INSIDE the IB. The break
                            came at 10:00 — thirty minutes after the deadline.
    (c) beyond the map      price never cleared the authored map: the planner
                            re-seated ahead of price all morning
                            (29375.25 -> 29539.38 -> 29619.50).

**The one arm that FILLED — id 35, short 29285.00 at 09:02 — is covered by none
of the five exclusions.** The day that motivated the wave opened narrow, ranged
inside its IB for ninety minutes, and only then ran. Every exclusion keyed to
the open or to a first-hour deadline is blind to a trend that starts late.

**Law.** A guard derived from an incident is REPLAYED against that incident
before it ships, at the clock the guard would actually have run at, and the
result is written into the guard's own help text — "would not have fired on
2026-09-03 (OR 0.77x median)" — so the label cannot imply protection it lacks.
The honest headline, ruled by the owner: *on the one day we have, the label
would have permitted the damaging trade.* That sentence is the E3 experiment's
null, pre-registered; a label that admits it caught nothing is more useful than
no label, because it records the exclusions' verdicts per episode, which is the
only way E3 can ever find one that works. **Do not invent a further exclusion
to make the incident come out right** — that is fitting a rule to n=1, which is
how the published-classifier claim round 11 §1 warns against got made.

**Probe.** For every exclusion/gate/guard born from an incident: name the
incident's ids; state the clock at which the guard would have evaluated; quote
the guard's verdict at that clock; if it does not fire, say so in the guard's
text. C1: no gate reads day_type (16 references, 0 gates). C3: "fade" has two
definitions disagreeing 2.5x (157/269 by condition, 64/269 by direction) —
define once, read from one place. Read beside classes 82 and 113.

**Corollary — model-worded fields are not inputs.** `day_type` is not merely
model-authored; it is free text with ten distinct values in the corpus,
including "trend-down extension / oversold reversal watch". A naive `= 'trend'`
misses 77 of 269. An exclusion keyed on it would string-match an LLM's
adjectives and call the result a measurement. Pin it by reflecting over the
facts struct (`TestFadeFactsCarriesNoDayType`).

## CLASS 116 — AN IDENTITY HASH WITH INPUTS THE RECORD NEVER HELD (assigned at identity merge, 2026-09-10)

Assigned after the three-format census of dev `4dc0fae1`: ceiling 115; existing
duplicates 75/76/77 retained. An identity
hash requires every recorded input, including the separate formation close;
missing input is NULL, never a hash of a zero. Legacy scenario links stay
labelled heuristics. Recording identity must not replace the trading evaluator's
anchor: disagreement is a counted finding. Pins: parser→named map→episode,
separate closes, all-input backfill, WARN-only missing/unknown ID, distinct
candidate/evaluator prices, production call-site removals, and baseline output
parity. C1–C4 and W1's pinned basis are in the
[105 report](reports/2026-09-10-scenario-level-identity.md), initially published
in `f19afe5dbd04199ddaa253a51958c0b909b8c4ae` before implementation.

**Law amended 2026-09-23 (W-EXEC-TRUTH W2 A3, CTO amendment msg 1790166603535 +
tightening 1790178967603; see the identity≠price CLASS 190 at the end of this
file):** "WARN-only missing/unknown ID" and "disagreement is a counted finding"
no longer hold AT NEW-AUTHORING WRITE. A level_id / sweep_level_id /
reclaim_level_id the frozen map does not carry (invented, altered, or a `ref|`
id whose digest matches no map row) is REFUSED, and a resolved level_id whose
level sits outside its own zone ±3.00 of the scenario anchor is REFUSED; both
re-author within the existing attempts and fail closed after the third. A NULL
level_id stays the WARN path. What still holds: the evaluator's anchor is never
replaced, stored ids are never rewritten, stored/overlay reads never run the
refusal, and the runtime per-cycle observation stays a counted finding.

## CLASS 117 — ONE IDENTITY, TWO RESOLVERS, RESOLVED AT DIFFERENT MOMENTS (born 2026-09-10, the contract roll)

**Name.** Two subsystems need the same identity — here, *which futures
contract is "MNQ" right now* — and each resolves it for itself from the same
rule. The rule is deterministic, so they agree… **whenever they happen to
evaluate it at the same time.** They do not. One evaluates at subscribe-time and
caches; the other evaluates at request-time, fresh. Between the rule changing its
answer and the cached side re-evaluating, the two halves of one system are on
different instruments, and nothing in either half can tell.

**Root cause, measured [A].** `VLContractResolver.cs:80` picks the front month
from `DateTime.UtcNow` (expiry − 8 days). September 2026 expires the 18th, so
the rule flipped to December at 00:00 UTC on the 11th — **19:00 CT on the 10th**.

- **Bars** resolve at subscribe-time (`VLBarsSubscriptionManager.cs:177`) and do
  not re-evaluate until a reconnect. The subscription stayed on `MNQ 09-26`
  until the 21:15:03 CT reconnect ACKed `MNQ 12-26` — research archive facts
  16480370 (last September `bar_update`, 21:14:56) and 16480372 (first December
  `bars_historical`, 21:15:03) pin the switch to seven seconds.
- **Orders** resolve at request-time (`VLTraderTCPClient.cs:907`, `:1140`), fresh
  from the clock. **Order 152**, placed 19:11:05 CT at 29094 — a September
  price — resolved to `MNQ 12-26` and sat ~291 points below December's market
  until the boot sweep cancelled it at 21:15:03.

For 135 minutes the bars and the orders were on different contracts, on
different price scales, and every log line said `MNQ`.

**The second half — a basis change with no marker.** When the subscription did
roll, the ring kept ~2,000 September bars and appended December ones. Nothing
tagged the seam. A ~292-point step presented to every reader as a move: the desk
strip read a 359-point RANGE (10.5× ATR), plan v7 at 21:29 seated **7 of 12
levels on the retired scale** and one IFVG *inside the gap itself*, and three
touch episodes (ids 1886–1888) recorded the roll bar sweeping through phantom
levels as market behaviour. The 21:15/21:16/21:17 bars carried one contract's
open and the other's close — bodies of +284.50, +296.75, +276.00 on a tape whose
bodies are otherwise single digits.

**Why it is a class and not a bug.** Any identity resolved from a *rule* rather
than *told* by the authority has this shape: two callers of the rule are two
clocks. The fix is never "call the rule more often" — it is to have ONE source
that *announces* the identity and its change (here, the AddOn's `subscribed`
ACK), and to stamp every fact with the identity it was received under so a
change is visible in the record rather than only in the prices.

**Probe, five questions:**
1. For any identity two subsystems both need — contract, session, trade date,
   account, schema version — where does EACH one get it? If the answer is "each
   calls the same function", ask WHEN each calls it. Two call times are two
   answers.
2. Is the identity DERIVED (from a date, a config, a rule) or RECEIVED (from the
   authority that owns it)? Derived identities drift silently; received ones
   arrive with a timestamp you can stamp on everything after it.
3. Does the persisted record carry the identity beside each fact? If a series
   can change basis and nothing marks the row, every reader of the series is
   already reading across a seam it cannot see.
4. What does a reader see at the seam? Here: a fair-value gap, a 10× range, seven
   levels. **A basis change reads as the most dramatic market event of the day**,
   and the detectors will dutifully file it.
5. When the identity changes, what is PURGED? A cache keyed without the identity
   holds both sides of the change forever, and "rehydrate from the store" — the
   thing that made the ring deep — is the path that carried the mixed tape back
   in on the next boot.

**Law:** **an identity that two things share is RECEIVED from one authority and
STAMPED on every fact, never derived twice.** Where the authority announces a
change, the caches keyed without it are purged, the readers filter to the
current value, and the retired facts stay as history under their own label —
filtered, never deleted.

**Corollary.** The dispatch that ordered this fix stated the boundary as 19:00 CT
— the rule's flip — while its own cited report said 21:15. The tape decides:
every close-to-close delta from 18:55 to 21:13 is under seven points, and no
backfill keyed on 19:00 would have been right. **The rule's answer and the wire's
answer are different facts, and only the second is in the bars.** This is A17
(measure first) in its most literal form.

## CLASS 118 — A WORKAROUND BUILT ON A MISREAD CAUSE, AND THE TWO DEFECTS IT SHIPPED WITH (born 2026-09-10, the bar-source wave; corrected 2026-09-11)

**What was believed at 22:5x CT.** NT8's replay (`bars_historical`) and its
live feed (`bar_update`) reported the same minute of the same contract ~290
points apart — research facts 16516009 (live, 29358.25) vs 16518205 (replay,
29068.25), 22:37 CT, both labelled `MNQ 12-26`. The wave built a Go-side
defence: a source label on every bar, a live-wins upsert, a scale-mismatch
detector, and a hold that kept a replay out of the store until a live bar
verified its scale.

**What was true.** NT8 had not rolled. Its front month, its charts and its
fills were on **MNQ 09-26**. The AddOn's date rule (`VLContractResolver`,
expiry−8d on `DateTime.UtcNow`) had put both the bar subscription and the
order path on **MNQ 12-26** at 00:00 UTC. The December request's history was
served at September's prices under the December name; its live ticks were
December's. The ~290 was the Sep/Dec basis — carry at 2026 rates on ~29,000
over a quarter — not a scale defect. My own earlier note "carry implies ~90"
was arithmetic on the wrong rate, and it was the tell that a different
mechanism was in play; I read past it. (Class 117 already named the actual
defect: one identity, two resolvers. The evidence for it was in NT8's own
log — `Instrument='MNQ 12-26'` on position 605's fill — and in no Go log,
because the wire frames carry no instrument.)

**What the workaround did on its one boot (abc420f8, 00:15 CT).** Two
defects, both of the same shape — **a verification whose own precondition
had disappeared:**

1. **The 20×-median-body condition blinded the higher timeframes.** Added
   to stop a fixture false positive (a 1-pt move at price 100), it made a
   30m bar's body ×20 exceed the 296-pt gap, so 30m/1h/2h/4h/… replays were
   "verified" and released. The check assumed a body scale it never
   measured against the gap it was guarding.
2. **A re-seed cleared the per-seed verdict and the re-check had nothing
   to compare.** MNQ 1m was judged off-scale at 00:15:21; a second
   `bars_historical` re-armed the check; by then the ring had been refilled
   with live rows, the incoming seed collided with all of them and was
   dropped by the live-wins merge, and the next live bar found no
   historical bar before it — `last == nil` → "checked, on-scale" → 1998
   rows released at 00:15:50. Per-seed arming was itself the fix for a
   mutation survivor; the fix had a hole the pins did not cover.

Net store damage 60 rows, all labelled, all deleted under authorisation.
Rolled back to 0070fc79 on the owner's order.

**Why it is a class.** A defence built against the wrong cause can be
internally consistent, fully pinned, mutation-tested — and still be a
machine for a problem that does not exist, with failure modes of its own.
The probes:

1. Before building a defence, name the cause in the AUTHORITY's own record
   — here NT8's log, not Go's. If the authority is not consulted, the wave
   is defending against its own inference.
2. When a number does not square (290 vs "carry ~90"), STOP. A residual
   that size is the mechanism, not noise.
3. Every verification has a precondition (a seed to compare against, a body
   scale that matches the gap). Pin the precondition's absence, not only
   the verification's outcome.
4. When the cause is corrected, WITHDRAW the workaround explicitly. What
   stands from this wave: `bars.source` (a label), the live-wins upsert, the
   ring's live-over-replay merge. What is withdrawn: the scale-mismatch
   detector and the replay hold (D4/D5) — with the platform's contract
   correct they have nothing to catch, and they carry the two defects above.

**Law:** **a defence is built against a cause read from the authority's
record, never from the symptom's arithmetic; and when the cause moves, the
defence is withdrawn by name, not left running.**

## CLASS 119 — A REVIEW THAT PREDICTS AN INCIDENT AND IS NOT CONVERTED INTO A GATE (born 2026-09-11, from vet-06-risk.md:255)

**The instance.** `docs/superpowers/reports/2026-09-05-vet-06-risk.md:255`,
dev `2a66d91c`, five days before the roll:

> "The uncovered window is **19:00 CT 09-10 → 09-14: orders on DEC26, bars
> and gate on SEP26**, and the gate's 3-day window not yet open. **What I
> would refuse:** no new entries from the Thu 09-10 17:00 CT open until a
> human has confirmed in the log that the bars ACK carries 'MNQ 12-26' …"

The resolver flipped at 19:00 CT 09-10 as written. Order 152 went to
December at 19:11 CT as written. Position 605 filled on December at 23:04 CT.
The refusal the review specified — no entries until the contract is
confirmed in the log — was never installed. The review was merged into dev
as a document and nothing read it back as a rule.

**Why it is a class.** A prediction with a date, a mechanism and a specific
refusal is a gate that has already been designed. Filing it as prose
converts it into something that will be read after the incident, as this
one was — by grep, at 00:4x, while the bot was blind. The cost of the gate
was one `if` on the boot line; the cost of the prose was 5½ hours, one
position and the whole bar-source wave.

**Probes:**
1. Does the review name a DATE or a WINDOW? Then it names a gate with a
   clock in it. Build the gate in the same wave that merges the review, or
   open a named wave with that date as its deadline.
2. Does it say "what I would refuse"? Then the refusal is specified. Ship
   the refusal; the report can describe it afterwards.
3. Who reads reports? If the answer is "the next agent that greps for the
   symptom", the report is a post-mortem written in advance.
4. At merge, every vet/review report is walked for dated predictions; each
   becomes a tracked item with an owner or a written reason it is not one.

**Law:** **a dated prediction in a review is a gate with a deadline; it is
converted at merge or it is not merged as "reviewed".**

**Sibling finding, same night.** The May plan
(`docs/superpowers/plans/2026-05-22-nq-databento-ninjatrader.md:245`) ruled:

> "**`GetNextExpiry` is UNBOOTSTRAPPABLE on Tradovate.** It needs a
> `MasterInstrument`, which is obtained only from a qualified/continuous
> `GetInstrument` — and **Tradovate has no continuous contracts** → null →
> chicken-and-egg."

NT8's own log, 2026-09-11 00:40:29:

> `VLBarsSubscriptionManager: reconnect resolved MNQ -> MNQ ##-## => MNQ 09-26 (rolling->MNQ 09-26)`

`GetInstrument("MNQ ##-##")` returned an instrument, its `MasterInstrument`
answered `GetNextExpiry`, and the concrete September contract resolved.
The refutation was recorded as settled fact and closed the correct path for
a quarter. A refutation is an experiment with a date and a build; it is
re-run when the premise is load-bearing again, not cited.

## CLASS 120 — A STORED RECORD RE-VALIDATED WITH THE WRITER'S RULES, AT THE READER'S CAP — THE PARSE FAILS SILENTLY AND WEARS A DATA LABEL (born 2026-09-11, fix/one-setup, dispatch 102)

**The instance.** Section C of the one-setup wave asked how often the
first-touched scenario armed (C1) and needed the touch→scenario link W1
shipped. Since W1's boot, `touch_outcomes` held **999** rows and **0** carried
`scenario_nearest`: 889 read `unresolved:no_scenario_at_seat`, 68
`nearest_outside_band`, 42 NULL. `no_scenario_at_seat` is returned when the
recorder receives **no anchors at all** (`store.ResolveScenarioLink`,
`len(anchors)==0`). The anchors come from
`kernel.ParsePlanDoc(latest.Doc)` in `auto_trader_planner.go` — the
REJECT-at-write parser, re-run on the STORED document with its default level
cap. The bound strategy seats 12 levels; the parser's default is 8. Reproduced
on the stored ASIA v9 doc: `ParsePlanDoc ERROR: too many levels: 12 (max 8)`.
Every live read since W1's boot parsed nothing, handed nil anchors to the
recorder, and the recorder wrote a basis that reads like a fact about the tape
("no scenario at seat") for what was a fact about the reader. 105's
`identityDoc` on the same path uses `json.Unmarshal` and works.

**Why it is a class, not a bug.** Three habits compound: (1) a validator
built to REFUSE at write is reused to READ, and a read has no business
refusing — the record already exists; (2) the reader's cap is the package
default, not the strategy's resolved value (class 45/49's literal-vs-resolved,
on the read side); (3) the swallowed error is converted into a NULL whose
BASIS is a plausible data reason, so every later reader (including this
wave's Section C) starts by believing the tape. The null carried a label
that implied a measurement.

**Checks.**
1. Any `Parse*`/`Validate*` called on a value read FROM the store: does it
   re-run write-time refusals? If yes, the read must use the record's own
   decoder (`json.Unmarshal`) or the writer's RESOLVED caps, never the
   package default.
2. A swallowed parse/decode error on a read path must surface as
   `unrecomputable:<the error>` or a WARN with the error text — never as a
   basis string that could also mean "the data really was like that".
3. For every `unresolved:*` / `unrecomputable:*` label: is there a code path
   that reaches it with the record perfectly fine and the reader broken? If
   so the label needs a second word that says which.
4. At the first live boot of any linker/labeller: count the resolved share
   in the first hour. 0 of 999 is not a tape; it is a reader.

**Law:** **a stored record is decoded with the record's decoder and the
writer's resolved caps; a reader never re-refuses what was already accepted,
and a reader's failure never wears the data's label.**

**Two smaller lessons from the same wave, recorded here rather than lost.**
(a) An EQUIVALENT mutant reads SURVIVED: `through := false` → `true` was
overwritten two lines later, so the suite could not notice and
`scripts/mutate.sh` — which certifies that the edit landed and built — could
not know the edit was inert. The harness proves the experiment ran; the
operator still has to prove the experiment could fail. The real rule mutant
(the through-by-a-tick tests replaced by `true`) was killed. (b) The
SYSTEM-MAP MAPCHECK contract caught a 48-line shift in `armed_executor.go`
from this wave's seam edits — twelve `symbol :NNN` pairs recomputed from the
file, not by hand (class 75 working as designed).

Cites: C1 (78 plans with ≥2 armable scenarios; first-touched armed 3/3 with a
resolvable touch after birth, 44 never reached the ledger), C2 (`reject`
n=39 +$314.00 pnl_corrected — the only positive cell; every follow-family
cell negative), C3 (13 reads since W-TF's boot; the top-ranked level carried
a scenario on 2, an armed scenario on 1), C9 (day zero: above hold 72 /
break 42 / ambiguous 72, n=186; below 44 / 57 / 48, n=149), round 17 (not on
dev at merge; the dispatch's summary is the citation until it lands).

## CLASS 121 — A GATE ON AUTHORIZATION THAT DOES NOT COVER THE AUTHORIZATIONS THAT PREDATE IT (born 2026-09-11 01:47 CT, the one-setup boot; closed the same night by owner ruling)

**The instance.** One-setup gates ARM AUTHORIZATION at the seam: a scenario
is authorized only when its three verdicts hold. It booted at 01:45:07 CT over
arm 154, which the class-33 sweep cancelled (it had a broker order). The sweep
also left rows **150** (S2 reject LONG 29200.25) and **153** (S1 sweep_reclaim
LONG 29156.25) — authorized by the previous process, never placed, no signal
id — "armed for this process to place", by design. On the first cycle both
scenarios were DECLINED (`level_not_best`, `play_not_reject`,
`level_unresolved`) and counted `declined_while_resting=2`. The placement
engine places any non-terminal `armed` row inside its 25-pt band and consults
no verdict. Price 29236 → 150 was 36 pt from placement.

**Why it is a class.** A regime change that filters the CREATION of a thing
inherits every instance of the thing that already exists. The boot transition
is exactly where the old regime's authorizations meet the new regime's
placement engine, and a sweep that keeps unplaced rows "for this process"
hands them over uninspected. The first boot found it because the owner's
gate report quoted the resting rows by id and someone asked what the
placement engine would do with them.

**Checks.**
1. A new gate on creation: list every path that CONSUMES the created thing
   (placement, re-spec, re-arm, sweep). Each consumer either re-asks the gate
   or refuses anything the gate did not stamp.
2. At the boot after a new gate: `SELECT` every non-terminal row of the gated
   kind and ask, row by row, whether the gate would allow it NOW. Anything it
   would not is retired before the first placement pass, with the verdicts.
3. The sweep's "left for this process" branch is a hand-over, not a
   verdict: the receiving process re-judges what it inherits.

**Law:** **a gate on creation is also a gate on inheritance — at the first
cycle after a boot, every pre-existing instance is re-judged by the new gate
before any consumer runs, and what fails is retired with its reasons.**

The fix (`oneSetupRetireDeclined`, `trader/one_setup_wiring.go`): once per
cycle, before D4's slot check and before the placement pass, a non-terminal
`armed` row with no signal id whose scenario is currently declined is
cancelled by ledger state — `declined by one-setup; pre-boot authorization not
placed · level=… play=… permission=…` — and nothing is sent to the broker;
rows with a signal id are never touched. Pinned on the loopback wire: the
declined scenario's pre-boot row is retired, never placed; the allowed one
still places. Counted `one_setup:retired`; on the boot line.

**Instance 2026-09-24 M3:** H2. Q8 retired pre-change tokens on /api/updates only, and authMiddleware consulted no credential epoch, so a stolen pre-change token kept every other route and could `PUT /user/password` again to re-mint an identity (red-1 #2). fh's narrow `3103894d` still left `GET /api/my-traders`, `POST /api/telegram` and `DELETE /api/telegram/binding` open (fh-verify #1). `0f48b52b` puts the ONE retire predicate (`auth.RetiredBy`) in authMiddleware and folds the guard's `<` copy into it (CLASS 257); the bot's own token is one of the inherited authorizations and re-mints (`d637b7e1`). Pins: `TestRetiredTokenCannotActAnywhere`, `TestTokenWithoutAnAccountRowOrIatIsRefusedEverywhere`, `TestCredentialGuardReChecksRetirement`, `TestBotReMintsItsTokenWhenAPasswordChangeRetiresIt` (`3103894d`, `0f48b52b`, `d637b7e1`)

## CLASS 122 — A CLASSIFIER THAT NAMES ITS OWN BLIND SPOT AND REPORTS THROUGH IT ANYWAY (assigned at merge of cleanup batch 2, 2026-09-11)

**The instance.** The knob registry (`store/knob_registry_table.go`) classified
sixteen fields `candidate-unverified` from a FIELD grep on 2026-09-03. Each row's
note said, in full: *"A METHOD-based reader would NOT appear, so this is NOT
dead and must not be removed: it needs a method-level grep."* The panel then
rendered the derived label — **"no known reader — pending verification"** — and
the operator read six wake switches as doing nothing. Seven of the sixteen had
method readers: the five `wake_on_*` switches and `wake_min_interval_min`
through `DayPlanConfig` accessors consumed in `trader/auto_trader_wake_levels.go`,
and `acceptance_rule` through `AcceptanceRuleFor()` at three call sites. The
note was right about its own limits and nothing acted on it for eight days.

**Why it is a class.** A verifier that documents what it cannot see has done
the honest half. The dishonest half is letting its verdict reach a surface
that does not carry the caveat: the row said "unverified", the panel said "no
reader", and the reader of the panel had no way to know a method-level check
had never been run. The caveat lived where only the author would read it.

**Probes:**
1. Every derived status has a blind spot; is the blind spot a CHECK or a
   sentence? If a sentence, what turns it into a check, and when?
2. Does the rendered label carry the same uncertainty as the record? "No
   known reader" and "no reader found by a field grep" are different claims.
3. Who consumes the status — a person choosing whether to flip a switch? Then
   the status is a trading-adjacent fact and its verifier's limits are too.

**Law:** **a stated blind spot is a test that has not been written yet; write it
in the same wave, or render the caveat wherever the verdict is rendered.** Here:
`store/knob_method_readers_test.go` — reflection maps a json leaf to its Go
field, go/ast finds accessor methods reading it and their production call
sites; every `candidate-unverified` row must have none, and the wake knobs are
pinned live with the call sites the detector found.

Related: class 105 (documentation in a place the code's tests cannot see),
class 113 (a gate that certifies a name, not a path).



## CLASS 123 — A SEAT LIMIT THAT HIDES THE MAP (assigned at level-zones merge, 2026-09-11)

**Measured:** snapshot `7f9db41a-815f-4be8-a9e2-ea124db26b67`, rows
23093602–23094444, contains 843 references. The 5m/15m swings at 29475
already collapse but lose their seat; the daily demand anchor 29006.625 is
excluded by proximity although its band overlaps the region. A capped entry
shortlist is not the complete level map. Native bands were already retained
by detectors; missing rendering was not proof of missing detection.

**Law:** keep the full reference map separate from entry selection. Merge only
known-width references whose fixed anchors and union band satisfy the resolved
ATR bounds, choosing one nearest compatible cluster without transitive union.
Broad context and unknown-width references remain separate; every source name
survives. Family counts and zone ranking never modify the existing score or
execution anchor. The measured fixture yields 498 retained zones/references,
25 multi-source merges, widest merged band 41.50pt under 42.113595pt.

**Pins:** `TestLevelZonesOwnerSnapshot`, width/unknown, compatibility/no-chain,
family and ranking tests; unchanged Stage A score golden; production wiring
mutations. C1–C5 and exact sample evidence:
`reports/2026-09-11-level-zones.md` at
`2d30329feb064501c680dc073072a18e7261b4f3` (subsequent deployment receipts follow
on the same report). Round 21 full text has not landed; the owner's dispatch
RESEARCH LAW is the operative basis, with no fabricated research SHA. The
class census used bounded numbered entries plus both CLASS heading formats,
`sort -n | uniq -c`, highest 122 immediately before this assignment.

## CLASS 124 — A MAINTENANCE IMPORT THAT COULD UPSERT THE LIVE TAPE (born 2026-09-11, fix/historical-backfill, wave 101)

A history import is the most tempting place to reuse the bars writer's upsert
("fill what the live feed never wrote"), and it is exactly the shape of the
09-10 damage: a back-adjusted replay overwriting 186 live bars under one label.
The import door must be a DIFFERENT write path with no upsert at all — `ON
CONFLICT DO NOTHING`, collision counted as a skip — and the imported rows must
be a third source (`historical_import`) that (a) is refused by the live writer's
source whitelist, (b) is not live-readable (`IsReadableSource` unchanged), and
(c) is exempt from the retention prune by the same column. The import itself is
an env-gated admin seam (`HISTORICAL_IMPORT_SEAM=on`, default off) that rides
the running process because the wire holds exactly ONE client — a second
process can never claim the NT8 connection, and a pull for an EXPIRED contract
must name the contract explicitly (`Instrument.GetInstrument("MNQ 09-23")`,
`MergePolicy.DoNotMerge`), never derive it from a date (that is the
`VLContractResolver.cs:80` roll bug). A continuous backtest series is built
deliberately with an EXPLICIT per-seam basis, labelled `continuous:adjusted`,
and the store refuses to persist it — an accidental join can never be mistaken
for raw tape.

**Pins:** `TestImportBarsNeverOverwritesExistingRow` · `TestImportBarsRefusesUnstampedRows`
· `TestImportSeamContractFilterAndContinuousLabel` · `TestPruneNeverDeletesImportedHistory`
· `TestRoundTrip_BarsHistoryFrames`; mutations KILLED ×3 (upsert reintroduced,
prune exemption dropped, source stamp check disabled). The class census for the
number used both heading formats (`grep -oE "^## (Class|CLASS) [0-9]+" | sort -n
| uniq -c`), highest 123 immediately before this assignment.

## CLASS 125 — A BACKTEST THAT ASSUMES FILL-ON-TOUCH REPORTS AN OPTIMISM THE MARKET DENIES (born 2026-09-12, research/backtest-zone-fade, BACKTEST 1)

A resting-limit backtest whose only fill assumption is "filled when price
touches the anchor" states the round-10 adverse-selection bias as a result. On
4.4 years of contract-stamped MNQ (11,302 first touches of seated zones), the
zone fade loses −0.79 pts/trade net under touch fills, but the subset of touches
that actually print ONE TICK THROUGH the anchor (31.7% of them) loses −4.93
pts/trade — the fills that exist are the bad ones, and the touch-fill headline
hides exactly that. Every backtest of a passive entry must report at least three
fill assumptions side by side — (a) filled on touch, (b) filled only on a
through-tick print, (c) filled with one tick of adverse slippage — and must
state which of them its headline survives. A result that survives only (a) is
not a result; here the verdict was negative under all three, but the (a)-only
shape is the trap. Companion law (same wave): a zone-band "hold rate" of 73%
against a bare-anchor detector hold rate of 50.6% on the same tape is a band-
width artifact — a hold rate computed on wider barriers inflates with the width
and answers nothing about expectancy (hold 73.3%, net win rate 46.8%).


## CLASS 126 — STOP AND TARGET GEOMETRY MUST SHARE A FROZEN STRUCTURAL THESIS (assigned at structural-stop merge, 2026-09-13)

[A] The structural-stop wave measured 200 logged compositions (27 distinct
plan/version/scenario/leg specifications): the ATR floor won 180 (90%). These
are selected changed/unanchored log events, not 200 independent attempts. The
raw-touch target population and the already-admitted live ledger differ; do not
substitute one denominator for the other. C1/C2 IDs and measurement protocol:
[structural-stop report](reports/2026-09-12-structural-stop.md).

[A] C5 reproduced 11,302 touches; training held events n=6,181 supplied p95
4.3675648248 points, rounded outward to a 4.50-point MNQ buffer. This is a
conditional OHLC penetration proxy and an [I] knob, not a validated universal
noise allowance. Round 22's pinned basis is
`e81602bb5c4bacb237ae2921e0188f8aa1d752bf`:
[research candidate](research/2026-09-12-stop-target-geometry/README.md).
C1/C5/report evidence published at `316f1e468294e27311114ac26420164d92531a67`.

**Law:** freeze the entry zone, far-edge invalidation, buffer and first distinct
eligible opposing zone; derive stop/target prices once, then evaluate net gain,
unchanged owner R:R, followed by the existing daily-loss and other entry gates. Inadequate geometry is
quantity zero with its exact refusal. Never move either price or resize to pass.
Missing provenance records the ATR fallback and refuses. Owner correction 2026-09-13: the owner meant DAILY loss; the added mandatory per-trade cap is removed (class 1: self-imposed caps).
An available structural stop bypasses only the ATR minimum-stop leg.

**Probe:** production arm-seam fixtures pin long and short far-edge stops,
tighter-than-ATR behavior, fallback provenance, 23-for-4 refusal, net-before-R:R,
and immutable prices. Nine applied/build-green mutants were killed. A refusal
whose record or retirement fails MUST return before placement; otherwise an old
arm can still reach the broker. The loopback fault-injection fixture pins that
failure, not merely the pure arithmetic. Other selectors and post-entry exits
remain unchanged.

[A] The conservative corrected replay remains negative: at the p95 buffer,
721/11,302 touches have eligible geometry; model A 207 fills average -2.6039 net
points. At the initial boot the mistakenly mandatory per-trade cap made admission zero; the owner subsequently clarified DAILY loss and removed that added requirement. This class establishes
honest trade construction and refusal, not profitable trade selection.

---

**Class (number assigned at merge) — Platform rollover switched the data scale
without a subscription-name change (2026-09-14).** Root cause: the C# AddOn
stamps every bar frame with the contract name computed ONCE at subscribe time.
On 2026-09-14 the owner rolled MNQ in NT8's Database Management at 10:30 CT;
NT8's rolling instrument started serving December data under the still-live
September BarsRequest, so bars carried December prices wearing the September
label (a +303.25-point 1m candle at 15:26 UTC) and the Go roll detector — keyed
on the `subscribed` ACK name change — never fired, so the ring was not purged
and ~16 minutes of mixed-scale rows persisted. Probe: on every roll, compare the
ACK-named contract against the newest live bar's price scale (or re-derive the
frame label per bar / force re-resolve on platform roll). Law: the contract
label on the wire must be re-derived per emission, never cached for the life of
a subscription; a data-scale switch without a name change is the same defect as
a name change without a purge. Resolution that day: full NT8 restart → fresh
ACK naming MNQ 12-26 → roll wave purged and reseeded correctly.

## CLASS 127 — A GUARD THAT COMPARES ACROSS A TIME GAP AND READS IT AS A SCALE GAP (born 2026-09-16, fix/nt8-history-and-chart-depth, dispatch 101)

**Root cause.** A detector compares "the last replay bar" with "the first live
bar" without asking whether the two are ADJACENT. After a long live run the
last replay bar is hours old; any reconnect that re-arms the check hands it
that stale reference, and an ordinary overnight move reads as a change of
price scale. The response to a scale break is destructive (drop every replay
bar for the key), so a false positive costs the whole seed — and the refill
path (1m-only by an owner condition) cannot put it back.

**The evidence (2026-09-16, MNQ 5m).** NT8 delivered 2,000 5m bars at 22:15
CT on 09-15 (`emitted bars_historical MNQ|5M bars=2000` in the NT8 log; the
Go-side `📼 historical=139` is a STORE census and was misread as a delivery
count — that misreading was the dispatch's premise). The 5m horizon read
`served=2131 span=285h55m` at 09:10. The feed flapped five times that morning;
each reconnect's BarsRequest ran while the feed was down and emitted `bars=0`;
`SeedHistorical` re-armed the scale check BEFORE its `len(bars)==0` return; at
09:22:11 the next live bar (29467.25) was judged against the boot replay's last
bar (22:10 the previous evening, 29320.50 — the store confirms the price),
146.75 pts cleared the 0.5% line by 0.15 pt, and **1,999 5m and 1,832 1m
historical bars were dropped**. Horizon after: `served=134 span=11h5m`.

**Law.** A comparison between two bars is only a comparison when the bars are
adjacent — within one or two intervals. A reference older than that is not a
scale question, it is a time gap: SKIP, record both ages, say so (A9), leave
the check ARMED for a later adjacent pair, never drop. And an EMPTY replay
cannot re-arm anything: nothing to judge means nothing to arm. A destructive
guard needs a stronger precondition than a non-destructive one, because its
false positives are not free.

**Probe.** For every "compare A with B" guard: does the code assert A and B are
neighbours in time before comparing them? What does an empty input do to the
guard's state? Replay the guard's own worst logged event as the fixture, with
the clocks it actually saw. `provider/ninjatrader/scale_check_adjacency_test.go`
is the worked example: the 09:22 event reproduced to the number, both 5m and
1m, plus the rule that a real adjacent break still drops.

**Corollary — a store census is not a delivery count.** `📼 bar source:
live=57395 historical=139` counts ROWS BY SOURCE in the store; a 2,500-cap ring
cannot hold 57,395 of anything. The replay-hold keeps replay rows OUT of the
store by design, so "historical=139" says the hold is working, not that NT8
sent 139 bars. Two lanes and a dispatch read it the other way. Read beside
classes 82, 110 and 113: a number on a line is a claim about what the line
counts, and the line has to say what that is.

**Corollary — the dedupe key was the observer, not the condition.** The
horizon WARN keyed on (symbol, tf, caller, requested, served, gaps); served
grows by one per bar and requested differs per caller, so 8,258 lines fired
since boot in a 5.1 GB log. The key is the CONDITION (symbol, tf, why); the
callers are a list on the line.

## CLASS 128 — A FIXTURE THAT SEEDS THROUGH A SKIP-ON-CONFLICT PATH AND NEVER ASKS WHAT LANDED (born 2026-09-16, fix/nt8-history-and-chart-depth, dispatch 101, found by nofx-93)

**Shape.** A test seeds rows through a helper that silently SKIPS conflicts (`ImportBars` on the
bars PK `(symbol, tf, open_time_ms)` — no contract in the key), then "measures" that a reader
excludes those rows. The rows never landed; the reader excludes nothing; the assertion passes on an
empty set and the report says "measured, not assumed". The boot line then repeats the claim
(`import=excluded-by-reader`) with no code behind it — class 82 wearing a test.

**How it hid.** The colliding timestamps were plausible (inside the prior contract's window, the
09-07..09-14 shape) and the PK collision is invisible from the fixture's point of view. The reader's
real filter (`NOT IN (mixed, off-scale)`) was one grep away and nobody grepped it because the test
was green.

**Probes.**
- Every seed helper that goes through an upsert/skip/ignore path asserts its own `inserted/skipped`
  and fatals on a miss (the fixture proves its census, class 109 applied to test data).
- A "reader excludes X" assertion is paired with a "reader RETURNS X when X is present" assertion
  on the same fixture; if the second cannot be written, the first measures nothing.
- A boot-line literal that names a filter (`excluded-by-…`) is grepped to the line of code that
  filters. No line → the literal is a lie (class 82).
- When the PK lacks a column the domain has (contract), ask where rows carrying the OTHER value of
  that column can physically sit: only in slots the first series does not hold. Fixtures put them
  there; readers are judged on that shape.

**Fix pattern.** Assert the seed census; put the exclusion at one door with a counted result the
boot line prints; pin the reader's real behaviour separately.

## CLASS 129 — A DECISION DOOR THAT READS ANOTHER FEED'S PROVENANCE (born 2026-09-16, fix/planner-tape-nt8-only, dispatch 101 follow-up, ruled by the CTO under the owner's delegation)

**Shape.** A store column carries rows from more than one provenance (NT8's live feed, NT8's verified
replay, a backtest/history IMPORT file) under one table and one shared reader. A decision door — the
planner's 12,000-bar 1m tape, the weekly reader's weeks and window, the POC-touch historical leg that
retires levels — reads the shared reader and admits the imported rows into a LIVE decision tape. Measured
2026-09-16: 426 imported `MNQ 12-26` 1m bars inside the planner's tape (75,492 on any `09-26` resolve).
The chart may show them, labelled; a decision may not be fed them.

**How it hid.** The import flag existed and a DISPLAY filter used it, so "imports are filtered" was true
of one reader and assumed of all. The ruling's own enumeration of doors missed one (the POC-touch leg):
doors are found by reading every call site of the shared reader, not by listing the ones you remember.

**Probes.**
- Every reader of a multi-provenance table has a DECISION variant that excludes non-feed provenance in
  SQL, and every decision door names that variant (`LastNBarsFromNT8On` / `BarsBetweenFromNT8On`).
- A lint BY FUNCTION BODY (not by file position — a cut at the display seam missed `storeBarReader`,
  which sits after it; the mutation showed it) pins each door to the decision reader
  (`TestNoPlannerDoorReadsTheSharedBarReader`). Class-113 caveat stated: text, not the call graph.
- A real-store fixture puts the foreign rows NEWER than the feed rows, so a reader that merely trims the
  oldest cannot pass by accident (`TestPlannerStoreReaderServesNoImportRows`).
- The boot line prints the import CENSUS on the contract and the decision input BOTH ways through the
  production estimator (class 82) — a moved value is named, never inferred (`🧮 planner tape`).
- Readers that MEASURE (excursions, level stats, recorded-only backfills, follow-plan records) stay on
  the shared reader and are named LEFT with the reason — a measurement over imported minutes is a
  separate question from a decision over them.

**Fix pattern.** Add the decision reader beside the shared one (shared byte-identical); reroute every
door found by grep; lint by body; census on the boot line; E7-style zero-delta golden proving the rule did
not move.

## CLASS 130 — A HOOK THAT LOSES WHEN ITS EVENT BEATS ITS INSTALLER (born 2026-09-16 15:32 CT, boot of c6579347, fix/after-backfill-hook-race)

**Shape.** Package A exposes `SetHook(fn)` as a mailbox (`atomic.Value`); package B installs the hook at
its own load; A's event fires once, checks the mailbox, finds nil, and moves on. When the event is
fast (a quick restart — the replay already persisted, backfill done at +3 s) and the installer is slow
(the trader loads at +6 s), the hook never fires and NOTHING says so: the 📈 regime line, the 🧮 planner
tape line and the R1 "📊 bars after backfill" line were simply absent. At the previous boot the order
happened to be reversed, and it was called proven.

**How it hid.** A boot proof reads the lines that ARE there; an absent line has no glyph to grep. The
hook was born 2026-09-09 and had won every boot until a restart 58 minutes after the last one.

**Probes.**
- Any `SetXHook` seam whose event can fire once must REMEMBER the event: install-after-event fires
  immediately, exactly once, under the same lock as the event (no window between "checked nil" and
  "installed"). Pin BOTH orders (`TestAfterBackfillHookFiresWhenInstalledAfterTheEvent`,
  `…FiresOnceWhenInstalledBeforeTheEvent`).
- A boot proof lists the lines EXPECTED and fails on absence, not only on wrong content.
- A "STALE / fresh" judgement compares IDENTITY (the served bundle's `GUIDE_BUILT_REV` vs the binary's
  `vcs.revision`), never timestamps that happen to correlate: the 🖥 timestamp rule was right at this
  boot by luck (the wrong bundle was also older) and would have called the RIGHT bundle stale, built 17
  minutes before the binary (`TestUIBootLineIsFreshByRevEvenWhenTheBundleIsOlder`).

**Fix pattern.** landed + fired flags under one mutex; both orders pinned; identity over time.

## CLASS 131 — ONE TABLE FOR TWO JOBS: THE STRUCTURE SEAT RACE (born 2026-09-16, feat/structure-map, S1 under the CTO's delegation)

**Shape.** One 12-seat ranked table carries both jobs — where price may ENTER (15m/5m/1m + today's
references) and which way the higher timeframes LEAN (D/4h/1h). Every rule that makes the entry table
good for entries makes it blind to structure: `isTodayPriority` sorts today's references first,
`freshMult` decays a 4h zone after one 1m touch (1.12 → 0.67), `collapseLevelClusters` renames a 4h level
under the reference it sits beside, `seatHTF` caps HTF at 2, `zoneTierFor` folds 5m into the 1m tier.
Measured 2026-09-16 16:31 CT: 244 HTF levels detected (≈70 4h, ≈12 daily); 11 seated — 8 references,
3 HTF, zero daily. The plan read the day with no daily structure at all and nothing said so.

**How it hid.** The table was always full and always graded; a full table looks like coverage. No line
counted what was DETECTED against what was SEATED per timeframe.

**Probes.**
- Two jobs → two tables. A direction read (trend / last impulse / premium-discount / top zones per HTF)
  lives in its own structure (`kernel.StructureMap`), labels INTACT, never collapsed into references,
  never an entry; the entry table keeps its 12 seats and its rules.
- Every S-wave knob defaults OFF and the prompt is byte-identical with it off (the existing goldens are
  the proof); the live plan changes only after the measurement wave (S4).
- A field the read could not compute is ABSENT (`structure` omitted), never `{}` / `[]` (canon).
- The read logs what it saw per TF (`🗺 structure @<session>: D=… 4h=… 1h=… zones=<n> pd4h=…`) and the boot
  line names the knob from the bound strategy (`🗺 structure: off|on(D/4h/1h)|n/a`).
- Fixtures from the real store (read-only export, dated) at the CALL SITE, not rebuilt inputs; a 10-bar
  daily series reads `range` honestly rather than pretending a trend.

**Fix pattern.** S1 (this) the structure table · S2 fresh-by-TF (DS-101) · S3 validator contract
(DS-102) · S5 (DS-103) · S4 measurement (DS-R) before any knob turns on.
## CLASS 132 — a recorder that narrates every write at INFO (born 2026-09-16, dispatch 103, DS-103)

**Symptom:** the research-snapshot recorder emitted one INFO line per archived
fact — 324,807 "research snapshot written:" lines in a measured one-hour slice
(88.8% of all log lines; ~13.2M/day), ~16 GiB/day of archive with no retention,
and drop notices that only reached the INFO sink.

**Root cause:** a diagnostics archive narrating its own success at volume, wired
to the INFO logger, with no env gate and no retention.

**Law:** a recorder is not a narrator. A background archive may emit at most ONE
rollup line per period (rows per object, drops, queue depth) at INFO; drop
notices go to the WARN sink, coalesced to one line per minute with the delta;
the boot line's counters are read live, never hardcoded; an env gate
(RESEARCH_SNAPSHOT) leaves it OFF by default; retention (RESEARCH_RETAIN_DAYS,
default 7) prunes at boot and daily with NO automatic VACUUM on a ~77 GB file.
Fixed 2026-09-16: researchsnapshot/* + main.go:77 wiring; pins in
researchsnapshot/volume_test.go; measured before/after in
docs/superpowers/reports/2026-09-16-research-recorder-volume.md.

## CLASS 134 — A PROTECTED-FILE HASH PIN THAT OUTLIVED THE WAVE THAT CHANGED THE FILE (born at the #131/#132 merge 2026-09-16, found 2026-09-16 via PR #140 CI, fixed in W-brandscope)

**Shape.** `web/src/brand-scope.test.ts` pins sha256 hashes of load-bearing files (dispatch 102's
protected set) and throws "protected file changed" on any byte. A wave that legitimately changes one of
those files (here dispatch 101: `provider/ninjatrader/tcp_server.go`, two additive fields + one call) is
built and merged from a Go-only review — `go test ./...` green, the wave's own vitest never run — and the
frontend suite on dev goes red for every LATER PR's CI. The wave that changed the file is the only one
that knows why it changed; by the time CI complains, that lane has moved on.

**How it hid.** The Go suite and the web suite are two commands; a Go wave runs one. The pin's failure
names the FILE, not the WAVE, so the next lane sees a foreign red and either re-pins blind or waits.

**Probes.**
- Any diff touching a path in `web/src/test/brand-scope-baseline.json` runs `cd web && npx vitest run
  src/brand-scope.test.ts` in the SAME wave and re-pins in the SAME PR, with the commit, the reason and
  the guard lines quoted in the test's comment block (never a bare hash bump).
- A re-pin names what the pin protects and shows it intact by line number (`SubscribeBarsHistoryFor`,
  the `bars_history_request` write, the `_data`/`_error` fan-out).
- "Additive; no identifier renamed" is checked with the diff's REMOVED lines, not assumed — gofmt
  re-alignment removes and re-adds lines that must be shown to still exist.
- The mutation check (`rejects a removed protected guard`) stays enforced; a re-pin never weakens it.

**Fix pattern.** Re-pin with provenance in the same PR as the change; where a Go wave touches a pinned
file, the web pin is part of that wave's suite (class 110: a green suite is a claim about an environment,
and this environment has two suites).
## CLASS 133 — S2 by-TF freshness

**Shape.** A freshness grade computed from one timeframe's bars while the scoring
ladder it feeds was calibrated against another (1m touches). A 4h zone was
downgraded C by one 1m bar trading into it, so the 12-seat table stopped seating
HTF structure. **Rule:** when a grade changes MEANING as its source timeframe
changes, keep the display vocabulary and the scoring ladder separate — the new
grader may only feed the ladder through an explicit normalization map, and every
legacy string must pass through it as identity (proved by
`TestNormalizeByTFGrade_IdentityOnLegacy` + `TestScoreLevels_ByTFVocabScoresLikeCanonical`).
Second rule (F1): a test is a RE-ENTRY — the level's own formation bars are its
birth and never count; counting starts after the first own-TF bar that CLOSES
fully outside the band.

**Fix pattern (S2).** `kernel/levels_fresh_by_tf.go` grades HTF levels on their
own-TF bars (fresh/tested-1/tested-2/stale, re-entry semantics); `normalizeByTFGrade`
maps the new vocabulary onto the unchanged `freshMult`/`zoneFreshMult` tables; knob default
OFF so goldens stay byte-identical.
## CLASS 135 — A SEAT GUARANTEE UNDONE BY ITS OWN RESTORE SORT (born 2026-08-24, found 2026-09-16 by DS-102, fix/structure-seats-and-relation)

**Shape.** A seating pass promotes a tail candidate into the top-N head, then
"restores strict seating order" by re-sorting the WHOLE list with the same
comparator used to build the head. The head IS the top-N of that comparator, so
a promoted candidate — which loses the comparator to every head member — is
restored to the tail by construction. `seatHTF` (kernel/levels_score.go) shipped
its "2 guaranteed HTF seats" as a no-op since G2/G3 (2026-08-24): the pre-seat
sort (:594-606) and the restore sort (~:1082-1096) share the comparator, and the
caller slices [:maxLevels] afterwards.

**How it hid.** The shipped pin test (`TestSeatHTFPromotesSwingLevels`) was
vacuous: its HTF candidates OUTSCORED the head fillers, so the final sort alone
seated them — the promotion path was never exercised, and the test passed for
the wrong reason.

**Probes.**
- Any `seat*` pass: build a fixture where the promoted candidates score BELOW
  the head fillers (the case that matters) and assert the seats appear only
  under the knob that activates the pass.
- A pass that returns ONLY its own maxLevels list (membership swap) survives its
  reorder-sort (`seatBothSides` — probed healthy); a pass that re-sorts head and
  tail TOGETHER nullifies itself (`seatHTF`).
- Knob-gate the fix so the legacy path stays byte-identical: nil → the old
  whole-list re-sort verbatim; saved → effective promotion, head and tail sorted
  SEPARATELY.

**Fix pattern.** Effective path: `sort(head)` + `sort(tail)` + `head ++ tail` —
promoted slots keep their seats, order inside each block stays strict. Legacy
path preserved as its own function until measurement gates the new behaviour.
## CLASS 136 — A BOOT LINE THAT PRINTS A VALUE THE PROCESS NEVER READ (born 2026-09-16, fix/exit-posture-bootline-honest, D102-1)

**Shape.** A boot line states a knob's posture from the ENV / literal config
alone while the mechanics gate on a different source. The 🛑 exit line printed
`BE=on · trail=on` whenever `EXIT_MECHS_SUSPENDED=0`, reading ONLY the env seam
(`trader/exit_mechs_suspend.go` `ExitPolicyBootLine`), even though the runtime
truth is the AND of that seam with the per-strategy toggles
(`breakevenTrigger` `trader/auto_trader.go:205`,
`trailingConfig` `trader/auto_trader_trailing.go:43`). At the 22:14 restart on
09-15 both strategy toggles were OFF and the line said on.

**How it hid.** The line was honest about the seam and silent about the toggle —
a reader checking the boot block could not tell which source each field came
from, because the line named no source at all.

**Probes.**
- A boot/status line must state the SOURCE of every field it prints
  (`BE=off(strategy) · seam=SUSPENDED(env)`), never a bare value.
- When a source cannot be read at boot time (strategy not yet loaded), the field
  prints `n/a` — never a literal, never the file default.
- Pin the call sites, not the inputs: env on + strategy off must render off;
  no strategy loaded must render n/a (`TestExitPolicyBootLineStrategyOffSeamActive`,
  `TestExitPolicyBootLineNoStrategyReadsNA` in
  `trader/exit_mechs_suspend_bootline_test.go`).

**Fix pattern.** Render every posture field from the exact value the mechanics
gate on, with its source in parens; print the n/a branch where the boot cannot
know; log the resolved line again where the source becomes available (trader
load).

## CLASS 137 — A SWALLOWED CONFIG ERROR THAT SURFACES AS A DIFFERENT FAILURE (born 2026-09-16 on the partner install, fix/env-load-error-logged, W-ENV-PARSE-ERROR)

**Shape.** A config loader's error is discarded (`_ = godotenv.Load()`,
`main.go:40`), the loader is all-or-nothing, and the first thing to notice is a
consumer three layers down that names ITS symptom, not the cause. On the new
machine an RSA_PRIVATE_KEY pasted unquoted across several lines made `.env`
unparseable; `loadFile` sets nothing on a parse error, so every variable stayed
unset; the boot died as "secrets missing" in a 5-second systemd crash loop and
the journal never mentioned `.env` at all — the operator was told to go find a
secret that was sitting in the file the whole time.

**How it hid.** Fail-open was the DESIGN (absence of `.env` is normal on some
hosts), and the discard was written to cover that case; it covered the parse
case identically, because the code never distinguished "no file" from "a file
we could not read". The crash loop then re-ran the same silent path every five
seconds, so the volume of evidence grew while the information content stayed
zero.

**The second trap, found while fixing the first.** godotenv v1.5.1's parse
error is `unexpected character %q in variable name near %q`, and the second
`%q` is the ENTIRE REMAINDER OF THE FILE from the bad statement onward. On the
partner-install shape that is the private key body and every secret after it.
Logging `err` verbatim — the obvious one-line fix — would have shipped them to
journald, `data/nofx_*.log` and the DB sink (`logger/db_sink.go` ships WARN+).
It also does not name a line number, despite reading as if it would.

**Probes.**
- `grep -rn '_ = .*Load()' --include=*.go` — every discarded loader error is
  this class waiting for a malformed file. (Remaining after this wave:
  `cmd/planner_ab/main.go:108`, `cmd/nq_smoke/main.go:53`,
  `cmd/nq_smoke/smoke_resolver.go:17` — dev tools, run by hand, out of scope.)
- Absence and malformation must log DIFFERENTLY, at different levels: INFO for
  the normal case, WARN for the one an operator must act on.
- Before logging any third-party error at WARN or above, READ the library's
  `Errorf` format strings: does `%q`/`%s` carry input data? Redact by shape
  (`redactDotEnvErr`), and pin the redaction with a test whose fixture holds a
  fake secret and asserts it is absent from the log.
- Test at the production call site with a real temp file, not a mocked loader:
  malformed → WARN with the line named and no file contents;
  valid → silent and the variable set; absent → INFO
  (`TestLoadDotEnv_MalformedFileWarnsWithLineAndWithoutContents`,
  `TestLoadDotEnv_ValidFileIsSilentAndLoads`, `TestLoadDotEnv_AbsentFileLogsInfo`
  in `main_dotenv_test.go`).

**Fix pattern.** Capture the error; branch on `errors.Is(err, os.ErrNotExist)`;
keep the fail-open semantics byte-identical (nothing set on error, no exit);
log ONE line per outcome with the diagnosis and the line number, and with the
library's echo of the input stripped. When the library will not name the line,
recover it by parsing growing prefixes and taking the line after the LAST
prefix that parses (a quoted value may span lines, so the FIRST failing prefix
is wrong — `TestDotEnvErrorLine_MultiLineQuoteBeforeBadLine`).

## CLASS 138 — A TEST THAT SHARES THE WALL CLOCK WITH A REAL GATE (born 2026-09-09 with the session-risk band, found 2026-09-17 02:04 CT by the Chief, fix/split-arm-test-clock, W-CLOCK-TEST)

**Shape.** A test drives a REAL gated path (here the arm path,
`maybeManageArmedOrdersAt`) and hands it `time.Now()`. The gate refuses by the
clock — the first 5 minutes of every session, the lunch window — so the test
fails DETERMINISTICALLY in a window nobody is watching for, then passes again
with nobody touching anything. `TestSplitArmWritesTwoLedgerRows` was red
02:00:00–02:05:07 CT every day:

	🛑 arm REFUSED (session risk): no_trade_band: LONDON first-5m no-trade window
	split_entry_test.go: split arm must write 2 ledger rows (legs), got 0 ([])

and green at 02:05:08. Same commit, same machine, same install.

**How it hid.** Three ways, stacked. (1) It had been fixed once: on 2026-09-10
the lunch band (12:00–13:30) turned eight trader/ tests red between two green
runs and `armTestClock` was written to search for an armable moment — but this
test could not use it, because its plan provider resolved the session from
`time.Now()` with NO seam and an injected clock desynchronised the fixture from
the path ("got 0 legs" with no refusal line, because there was no plan). So it
got a `t.Skip` for the ONE band that had bitten, and a comment saying the real
fix was owed. (2) The seam then LANDED — cleanup batch 2 B3, 2026-09-11,
`installActivePlanProviderAt(at, st, clock)` — and nothing tied the owed note
to the wave that discharged it; the comment kept saying "no seam" for six days
after there was one. (3) A skip-list of bands is always one band short: the
lunch skip made the test green 12:00–13:30 and left first-5m of ASIA, LONDON
and NY (three windows of five minutes) to fail by the clock. Five minutes a
day is rare enough that every observer blamed their own branch first.

**Why it matters.** A suite that is red for five minutes a day, by the clock,
is a false signal to every lane that runs it then — and the merged-HEAD suite
of a cutover is run at whatever hour the cutover happens. Dispatch 102's
cutover ran into exactly this window on 2026-09-11 and lost a re-run to it.

**The fix shape.** ONE injected clock, threaded through EVERY clock read the
path makes: the fixture's plan (trade date), the tape (bars relative to it),
the provider (through its seam) and the entry call — so the fixture and the
path are provably reading one clock, never two that happen to agree. The base
is FIXED (a known weekday, mid-morning NY), searched by `armTestClockFrom` so a
registry change moves the moment instead of silently invalidating it, and an
env override (`SPLIT_ARM_TEST_CLOCK_CT`) lands the clock INSIDE a band on
purpose — the RED is now reproducible at any hour instead of five minutes a
day.

**Probes.**
- `grep -n 'time.Now()' trader/*_test.go` and, for each hit, ask whether the
  value reaches a gate that refuses by the clock (`sessionRiskGateAt`,
  `sessionEntryBlockedAt`, `InFirstNoTradeMinutes`, `InLunchNoTrade`,
  `InT1Blackout`). A read that only stamps a record or a log is not this class;
  a read that a verdict consumes is.
- A test that needs a `t.Skip` for a band is this class with a shorter fuse:
  the skip names one window and the gate has several. Count the windows the
  gate knows; count the skips; they differ.
- A seam that exists but that the tests do not use is half a seam
  (`arm_test_clock_test.go`). When a seam LANDS, grep the test tree for the
  comment that said it was owed — `grep -rn 'NO seam\|no seam\|OWED' *_test.go`
  — and discharge it in the same wave, or the note outlives the debt.
- Reproduce the RED on demand before calling it fixed: a failure that only the
  clock can produce is a failure whose fix cannot be proven by running the suite
  once at a convenient hour. Give the test a way to be placed inside the band.
- Census at this wave (trader/ tests reading `time.Now()` on a path that reaches
  the band gate, all of which currently SKIP or search rather than fail):
  `one_setup_golden_fixture_test.go:103` (identical shape — full
  `sessionEntryBlockedAt` skip, provider still on `time.Now`, same seam fix
  applies, skips ~100 min/day), `split_entry_test.go`
  `TestSplitArmSessionEndCancelsBothLegs` (plan/provider at `time.Now`, entry
  at `armTestClock` — two clocks; passes inside a band only because the band
  refusal itself cancels resting arms), `armed_executor_test.go` L42/L111/L142,
  `shadow_demotion_test.go` L40/L62/L78/L282, `slist_eod_race_test.go` L236
  (same two-clock shape: fixture at `time.Now`, entry at `armTestClock`). Not
  this class, checked: `class33_boot_sweep_test.go` L25/L156 stamp
  CreatedAt/UpdatedAt only and the sweep is not band-gated. None of the listed
  tests fails by the clock today; each is one registry change from doing so.
- **DISCHARGED IN PART, and the sentence above was already false when written —
  2026-09-23, W-EXEC-TRUTH W4 (`fix/w4-picture-evidence`), CTO ruling
  `1790198929468`.** Two tests that the census did not list DID fail by the
  clock, on the same afternoon, for two lanes at once:
  `TestFourPlacementPathsWaitForEntryReceipt` (subtests `limit`, `stop_entry`:
  "production path did not send") and
  `TestUnsentStopEntryNeverCancelsTheSiblingArm/sent_commits` ("signal on the
  wire = false, want true"). Both hand `time.Now()` to a production send path;
  the admission chain asks `kernel.CMEClosedReason(now)` on the clock it is
  GIVEN (`entry_admission.go:304`), so the 16:00–17:00 CT daily break refuses
  them with "🌙 cme closed: … REFUSED — daily break". They were green at 15:50
  CT and red at 16:13 CT at the SAME commit, and red on an unrelated branch
  (`fix/w5-picture-source @c1d96494`) in the same hour — confirmed by the CTO
  at clean dev `eb7294c9` at 16:27 CT.
  **Fixed (test-only):** `trader/rth_clock_test.go` `rthInstant()` — a fixed RTH
  instant (Wed 2026-09-23 10:30 CT) passed through the existing `…At` seams at
  the three production-facing sites. Verified by running both tests AT 16:30 CT,
  inside the band that had just failed them: green. No production change, no
  retry, no skip.
  **The lesson the census missed:** it searched for tests that SKIP or SEARCH on
  the band, and these two do neither — they simply do not send, and read as an
  ordinary assertion failure. A census of a failure mode must be built from the
  mode (which clock reaches a gate), not from the symptom it happened to show
  the day it was written.

## CLASS 139 — A HOLD THAT RESTARTS ON EVERY RE-READ: hysteresis anchored to the version, not the plan (born 2026-08-21 with the regime wave's G3 hold, reported by the owner 2026-09-17 "it went up all night and never flipped", fix/flip-hold-anchor, W-FLIP-HOLD-ANCHOR)

**Shape.** A hysteresis window ("no flip within N minutes of birth") measures
age from the created_at of the ROW it happens to be evaluating. The row is a
VERSION in an append-only chain, and something unrelated to state — a wake
re-read — appends a new version every 30–40 minutes. Every re-read is a new
birth, so the hold restarts, and a 30-minute hold becomes "held for most of
the session" whenever wakes fire faster than the hold expires. Nothing is
wrong on any single evaluation: each one truthfully reports the age of the row
it was handed.

**The live story (2026-09-16 ASIA, plan `2026-09-16:ASIA:…`).** v1 authored
16:35:56 (`ASIA_scheduled_read`), then twelve `level_event` re-reads: v10
23:36:02, v11 00:13:28, v12 00:39:28, v13 01:21:25 — every one bias SHORT with
a flip "above X → long" and X stepping DOWN (29500.25 → 29479.50 → 29450.50 →
29418.80) as the tape climbed. The 01:25 and 01:30 5m closes (29443.75,
29448.25) were both above v13's 29418.80. The journal at 01:35:00:

	flip_eval_skipped plan=… v13 flip=hold (plan age 815s < 30min)

and again 01:36:28 (903s) and 01:38:28 (1023s). 815s is exactly 01:21:25 →
01:35:00: the age of v13, a re-read that changed no state. The chain itself
was nine hours old and had held one bias since 21:00:40 (v5 neutral → v6
short). The plan went dormant at 01:50:15 on the DEATH line (29450.50), never
having flipped; the owner woke to "it went up all night and never flipped".

**Why it hid.** Three ways. (1) Every skip line was individually true — "plan
age 815s" IS v13's age — so nothing in the log contradicted itself. (2) The
G3 test (`TestG3FlipHold`) proved the hold with ONE version: fresh → held,
old → fires. A chain of versions was never in the fixture, so the restart
had no assertion to fail. (3) The two clocks were one variable: `sinceMs`
windowed the CONDITION's bars (correctly the version's birth — a new flip
line must be judged only on bars after it was written, P1c) AND clocked the
hold. The right value for the first was the wrong value for the second, and
sharing the name hid that they were different questions.

**The fix shape.** Separate the clocks. The condition window stays the
version's birth (touch gate + confirm closes untouched). The hold reads a
STATE anchor — `kernel.ResolveFlipHoldAnchor`: the latest of the chain's
first version, a deliberate re-plan version (death_replan / owner_reread /
owner_reset), a version whose bias.direction changed, the last flip→dormant
marker, the last re-arm marker. A same-bias wake re-read (level_event,
structure_mss, a scheduled read) moves nothing. The skip line now names the
anchor (`flip=hold (hold age 600s since session-plan-birth < 30min)`), the
🧬 boot line prints `flip_hold=<N>min anchored to latest of {…}` READ from
the knob and the resolver's own kinds table, and a chain the store cannot
read falls back to the version's birth TAGGED `version(fallback)` so the
pre-fix semantics are visible when they are in force. Death never had a hold
and keeps none; it shares only the condition window. Proof at the production
call site (`describeActivePlanDeath`): a same-bias re-read 11 min into a
45-min-old chain is EVALUATED; v1 at 12 min is held; a flip 20 min ago then a
re-read is held (and fires at 40); and the ASIA 09-16 chain replayed with the
live 1m tape reproduces the 815s hold on the old clock and flips at 01:35 on
the new one (`trader/flip_hold_anchor_test.go`).

**Probes.**
- `grep -n 'CreatedAt.UnixMilli\|created_at' kernel/*.go trader/*.go` and,
  for each hit that feeds a TIME WINDOW or a HOLD, ask: is this row the
  thing whose age matters, or merely the latest row about it? An append-only
  chain (plans, overlays, armed_orders ledger, lifecycle log) makes every
  "latest row" younger than the state it describes.
- One variable feeding two predicates with different correct values
  (`sinceMs` → condition window AND hold clock). Give the second its own
  name at the signature, even when today's caller passes the same number.
- A hysteresis / cooldown / debounce test with a single-row fixture. Add the
  chain: two rows, the second younger than the window, the first older — the
  verdict must come from the state, not the row.
- Journal counter-read: the hold says `hold age Ns since <kind>` (pre-fix journals read `plan age Ns`). If N never exceeds the
  wake cadence across a session, the hold is being restarted by the wakes.
- The partner mirror (`vlautoagenttraderv1`) carries the same evaluator; the
  fix propagates via `format-patch → am` (owner-run push).

## CLASS 140 — A VALIDATOR THAT CHECKS THE NUMBER AND NEVER THE DIRECTION (born 2026-08-27 with the structured flip{} object, found 2026-09-17 by the owner "same flip point it not flip", fix/flip-direction-validator, W-FLIP-DIRECTION)

**Shape.** A structured condition carries three facts — a price, a side, a
destination — and the write-site validator cross-checks exactly one of them
(the price must appear in the prose). The side and the destination are
enum-valid on their own ("below" is a legal side, "long" is a legal flip_to),
so a plan whose flip points the WRONG WAY for its bias passes every schema
check and ships. Nothing then fires it, because the move it names is the move
that CONFIRMS the bias rather than reverses it.

**The live story (2026-09-17 LONDON v3, and 20 of 341 plans in the store).**
bias.direction="short", flip={29474.90, side "below", rule "2x5m", flip_to
"long"}, death={29604.25 above}. A short bias reverses to long on a close
ABOVE a line; "below → long" can only fire on a continuation of the short,
i.e. never on the rally the owner watched all night. The number 29474.90 was
in the prose, so the only cross-check passed. The read path's
warnFlipDeathSanity judged the flip's ORPHAN status and its collision with
death, never its direction. 20/341 historical plans carry the same inverted
shape (query in the wave's report); every one of them "never flipped".

**Why it hid.** (1) The enum validators are per-field: each of side/flip_to/
bias is legal alone; the defect is a RELATION between three fields and no
check was relational. (2) The prose cross-check reads as "the flip is
verified" to anyone skimming the validator, so the direction question was
assumed answered. (3) The failure is silent by construction — an inverted
flip is a flip that never fires, indistinguishable in the journal from a
flip whose level was simply never reached.

**The fix shape.** ONE relational function, `kernel.FlipDirectionContradiction
(biasDir, flip)`, called from the write site (REJECT, `ValidatePlanDocWithCaps`)
and the read path (WARN, `noteFlipDirectionInverted`, called first by BOTH
stored-plan evaluators — `describeActivePlanDeath` for active plans and
`describeDormantCleared` for dormant ones — once per plan version, the
notePlanProviderNil idiom; the 09-17 review found the first draft of this WARN
sat in `warnFlipDeathSanity`, which only ever runs AFTER the write-site reject,
so it was dead code with a green direct-call test) so both speak one sentence:
`flip{below 29474.90 → long} contradicts bias short: a short bias flips to long
only on a close above the line`. Empty flip_to is read as the opposite of the
bias. Neutral bias and a flip_to that is not the opposite of the bias are not
this rule's question (no-op). Class-38 discipline: the law is a prompt-contract
row (`MustAppear` guarded by `ValidatePromptContracts`), a rendered prompt
sentence, and a repair excerpt `RepairFlipDirectionLaw` routed on the error's
own words ("contradicts bias") — the model is told the rule it is judged by.
Death is untouched (separate question). Tests at the production call site for
all six bias×side×flip_to cases, empty flip_to inference, repair routing, and
the trader WARN via captured log output at both evaluators (named once across two evaluations).

**Probes.**
- For every structured object with ≥2 enum fields (`PlanCondition`,
  `ArmSpec`, `Confirm`, scenario direction vs target chain), list the
  RELATIONS the fields must satisfy and grep the validator for a check that
  names both fields in one predicate. A validator made only of per-field
  enums has this class waiting.
- `sqlite3 -readonly data/data.db "select count(*) from … where bias='short' and flip_side='below'"` (and the mirror) — a non-zero count on a shipped
  rule is the class in the store, not a hypothetical.
- A "never fired" condition in the journal must be distinguishable from a
  "could never fire" one: the read-path WARN is the probe; if the journal has
  no `flip_direction_inverted` line for an old inverted plan, the read path
  does not judge direction. Check EVERY evaluator of the stored object (active
  AND dormant), and check the WARN's call site runs BEFORE any reject that
  would make it unreachable — a green test that calls the function directly
  proves nothing about the production call site.

## CLASS 141 — A FLIP THAT ONLY SLEEPS: THE FLIPPED BIAS WAS NEVER READ (born 2026-09-17, found by the owner, fix/flip-reread)

**Shape.** A structured flip fires, the plan goes DORMANT as designed (wick-noise
protection), and the re-arm predicate only ever restores the SAME plan when price
closes back on the old side. `doc.FlipStructured.FlipTo` is evaluated exactly once
— to build the killer string "flip-condition: … → bias <FlipTo>" — and nothing ever
authors a plan with the flipped bias. The owner watched a 120-pt overnight rally
with a live "flips to long" line and no long plan.

**How it hid.** The dormant write is correct and loud ("auto re-arms when price
closes back"), so every check of the lifecycle machinery passed while the promise
the flip line makes — a bias that FLIPS — had no producing code path.

**Probes.**
- For every killer string that names a direction ("→ bias long/short"), ask: what
  CODE produces a plan with that direction? A log line is not a code path.
- A hysteresis pair (dormant on breach, re-arm on close-back) restores the OLD
  plan; a flip is a NEW thesis and needs its own read.
- Pin the knob-gate: OFF = byte-identical dormant (no read, no key, the dormant
  line unchanged). ON = one SUCCESSFUL free re-read per fired flip, where
  success is decided by the STORE (a version newer than the dormant row with
  lifecycle "active"), never by the read call's bool — that bool means "this
  call claimed the read" and a wake-class read that exhausts its 3 attempts
  returns (0,"kept_active",nil) with NO row and still reports true. The
  once-key (`flip_reread_done:<plan>:<version>` in system_config) is written
  only AFTER that decision; while the read runs an in-memory in-flight guard
  stops a second launch. A read that is refused (preflight, wake cadence, an
  open stream) or that lands no new active version leaves the key clear
  ("0"/""), and the dormant branch of maybeRunSessionReadsAt calls
  maybeRereadAfterFlip again every cycle the row sleeps — subject to the same
  preflight and cadence — until a read succeeds or the row re-arms.
  Worst case, stated: ~1 launch per wake_min_interval_min (default 10 min,
  up to 3 model calls per launch), no hard cap on launches while the row
  stays dormant, class-35 free — the same cost shape as a level-event wake,
  bounded only by the session read window.
- The write site ENFORCES the flipped bias: `requiredBias :=
  kernel.FlipToDirection(priorKiller)` and "bias %s is MANDATORY". The model
  authors the flipped bias or the read writes nothing and the dormant plan
  stands. A same-bias plan therefore cannot come through the production write
  site; the goroutine names one if a non-production writer lands it, never
  loops.
- The prior line echoes the OLD bias before its arrow ("PRIOR PLAN v3 bias
  long — … → bias is now expected short … flip-condition: … → bias short").
  Any parser of it must read the LAST "→ bias <word>" only; a substring scan
  for "bias long" mandates the STALE bias and rejects every correct plan
  (the review's BLOCKER 1).
- The supersede of the dormant version must be a compare-and-set FROM
  "dormant" (`UpdatePlanLifecycleIf`): the planner call can run 20 minutes and
  the re-arm path may restore the row meanwhile. A refused CAS leaves both the
  re-armed vN and the new vN+1 as written; the newest version governs at read
  time (GetLatestPlanForTraderSession is ORDER BY version DESC). The goroutine
  also re-reads the row right before the planner call and skips a row that is
  no longer dormant.
- A test that substitutes the read seam proves only the request. Every one of
  the three blockers above sat behind a green recorder test; the real path
  (claimed read → planner core → write site → store) must be exercised with
  only the AI client scripted.

**Fix pattern.** W-FLIP-REREAD: after the dormant write (and again from the
dormant branch on every later cycle while the key is clear), request a
structure_flip read (class-35 free, same preflight + wake cadence as a level
wake) whose prompt carries "PRIOR PLAN v<N> bias <old> — … the prior plan is
dormant. <killer>"; the write site mandates the flipped bias; when the store
shows a newer active version the once-key is set and the old version is
superseded by CAS from dormant (`superseded:flip`, reason
`superseded:flip:v<N+1>`); otherwise the key is cleared and the dormant plan
stands until the next cycle's retry.

## CLASS 142 — A TEST HARNESS THAT POLLS AN UNSYNCHRONIZED BUFFER A BACKGROUND GOROUTINE WRITES (born 2026-09-17 with the CLASS 141 real-path tests, found by CI `go test -race` on the first dev push after the merge, fix/test-log-capture-race)

**Shape.** A test captures the journal by pointing the logger at a plain
`bytes.Buffer` and then POLLS `buf.String()` until a line appears. The code
under test logs from a goroutine it spawned (the structure_flip read, a wake).
`bytes.Buffer` is not goroutine-safe; logrus serializes its own writes but
nothing serializes the test's reads against them. Under the race detector the
test FAILS; without it the test passes and the suite is green, so the defect
ships to the one job that runs `-race` (CI "Go Unit Tests & Coverage") and
turns every subsequent push red: 0d54518c, d83bbfe0, 3445ee7f, 9a397b26,
13ef576c all failed on the same four tests while the local suites were 34/34.

**Why it hid.** (1) The local gate was `go test ./...` without `-race`; the
race only exists when two goroutines touch the buffer, which only the
real-path tests do. (2) The race report names `bytes/buffer.go` and
`logrus/entry.go`, not the test, so it reads like a library problem. (3) The
PR merge for the next wave was refused ("not mergeable", checks UNSTABLE)
before anyone looked at WHY CI was red.

**The fix shape.** ONE capture helper, `captureTraderLog`, returns a
mutex-guarded `syncLogBuf` (Write/String/Reset under the lock). Every test
that polls the journal uses it, so a background logger and a polling test
cannot race by construction. No production code changed.

**Probes.**
- Every `SetOutput(&buf)` / `bytes.Buffer` handed to a logger in a test:
  grep `SetOutput(&` and `var buf bytes.Buffer` in `*_test.go`; if the code
  under test can log from a goroutine, the buffer must be synchronized.
- Run `go test -race` on any package whose tests exercise a goroutine-spawning
  path BEFORE merge — CI runs it, and CI is the last gate, not the first.
- A red CI on the FIRST push after a merge is the merge's problem until proven
  otherwise: read the run's failing job before the next PR is opened.

## CLASS 144 — A DORMANT ROW JUDGED BY THE WRONG PREDICATE: DEATH-DORMANT RE-ARMS ON ITS FLIP LINE (born 2026-09-03 with D3's lifecycle-log move, found 2026-09-17 by the fix lane, fix/dormant-death-rearm, W-DORMANT-DEATH-REARM)

**Shape.** The dormant write sends its kind ("dormant:death:…" / "dormant:flip:…")
to the lifecycle log only — D3 made `plans.trigger_reason` the AUTHORING trigger.
The re-arm predicate kept keying off `trigger_reason`, so the prefix check is
always false: every D3+ dormant row falls to the FLIP condition. A death-dormant
plan re-arms when its flip line clears while the death line stays breached, or
(no flip line) re-arms immediately on "no machine condition" with the death line
still hit.

**How it hid.** The dormant write and the flip-rearm path were both individually
correct and individually tested; the death path had no re-arm test, and the
wrong-predicate outcome (a re-arm) looks like a SUCCESS unless the test asserts
WHICH line the reason names.

**Probes.**
- A transition writes a marker to a NEW home; grep every reader of the OLD home
  for the marker prefix — not just the writer.
- For every predicate that says "the SAME structured condition that parked it",
  pin the KIND: both dormant kinds must re-arm on their own line and only their
  own line, with the reason naming the line price.
- A re-arm test that only checks lifecycle=="active" cannot see a wrong
  predicate — assert the `rearmed:` reason names the right line.

**Fix pattern.** W-DORMANT-DEATH-REARM: `dormantDeathKillerOf` mirrors CLASS
141's `dormantFlipKillerOf` (most recent dormant event in the lifecycle log);
`describeDormantCleared` picks the condition by kind from the LOG, with a
pre-D3 `trigger_reason` fallback for legacy rows. Tests at the production call
site (`maybeRunSessionReadsAt`, real store row parked via `UpdatePlanLifecycle`).
## CLASS 143 — A STRAY ROW OF THE NEW CONTRACT BEFORE THE ROLL PULLS THE CHART BOUNDARY BACK AND ERASES THE OLD CONTRACT'S LAST WEEK (born 2026-09-14 at the Sept→Dec roll, reported by the owner 2026-09-17 16:40 CT "candles missing for several days", fix/chart-roll-hole, W-CHART-ROLL-HOLE)

**Shape.** The chart across a roll is a time split: prior-contract rows before
the current contract's first LIVE bar, current-contract rows after. The
`bars` PK is `(symbol, tf, open_time_ms)` — the `contract` column is OUTSIDE
the key — and inserts are INSERT-OR-IGNORE. NT8 served ~2,000 bars of
"MNQ 12-26" history per TF at subscribe; wherever a "MNQ 09-26" row already
held that open time the Dec row was dropped, and wherever it did not
(holidays, Sunday evenings, the NT8-off windows) a stray Dec row LANDED
(live 5m: 4 `historical_import` + 13 `historical` rows older than Dec's first
live bar; 1m: 451). `klinesAcrossRoll` then took the boundary from
`FirstLiveOn` (correct: 09-14 10:00 CT) and MOVED IT BACK to the base series'
oldest bar — "nothing older than the series' own oldest bar may overlap it" —
i.e. to the oldest surviving stray (09-10 22:10). `PriorContractBarsBefore`
only takes rows strictly before the boundary, so every Sept row from 09-10
22:10 to 09-14 09:55 was excluded and the only candles in that span were the
13 strays, 292 points up: a three-trading-day hole with a stray candle or two
in it.

**How it hid.** (1) The isolation filter DID drop the four one-per-day import
strays, so the boundary was not pulled back to 09-07 as the row census
suggests — it was pulled to the first DENSE stray (five contiguous replay
rows on the evening of 09-10), which no filter names. (2) Every existing pin
built its prior series ENDING exactly where the current series began, so
"base[0] < boundary" never fired in a test. (3) The kernel, levels and arm
readers are contract-scoped and never see a prior contract, so nothing
downstream disagreed with the chart. (4) The lonely candles looked like a
data gap, not a boundary rule.

**Probes.**
- Per TF: `SELECT count(*) FROM bars WHERE contract = <current> AND
  open_time_ms < (SELECT min(open_time_ms) FROM bars WHERE contract =
  <current> AND source = 'live' AND tf = <tf>)` — any non-zero count is a
  stray population the chart reader must DROP, never draw and never move the
  boundary for (live 2026-09-17 17:10 CT: 1m 451 · 3m 12 · 5m 17 · 15m 10 ·
  1h 4 · 3d 14).
- A boundary rule must be MONOTONE: derived from one source (`FirstLiveOn`)
  and never adjusted by the data it is about to split. "Never move the
  boundary earlier" is the invariant; a clamp to `base[0]` is a rule that
  lets the defect choose the boundary.
- Pin the pure function with strays IN the base (a base whose oldest row is
  older than the boundary) and the production route (`GET /api/klines`
  through the router + JWT) with the live per-day shape; assert per-day
  counts continuous across the roll, zero current-contract klines before the
  boundary, zero prior-contract klines after it.
- The prior-contract reader mirrors `LastNBarsOn`'s source filter
  (`mixed`, `replay:off-scale` excluded) — a roll-straddling row is accepted
  by no reader, the chart included.
- A hole in the prior series stays a hole (the store reader's own ruling):
  dropping a stray leaves its slot empty; drawing the next contract's price
  space there is never the answer.

**Fix.** `api/handler_klines.go klinesAcrossRoll`: `dropCurrentRowsBefore(base,
boundary)` replaces the clamp; `store/bar_history_across_roll.go
PriorContractBarsBefore`: source filter. Pins: `api/klines_across_roll_test.go
TestKlinesAcrossRollDropsCurrentStraysOlderThanTheRoll`,
`api/handler_klines_roll_hole_test.go TestKlinesAcrossRollNoHoleNoStrays`,
`store/bar_history_across_roll_test.go
TestPriorContractReaderExcludesMixedAndOffScaleSources`.

**OWNER-GATED, NOT DONE HERE.** The root is the schema: a PK without the
contract lets two contracts fight for one slot and the loser is silently
dropped. Adding `contract` to the key (or a partial unique index per
contract) is a migration over the live `bars` table and every reader that
assumes one row per open time — the owner's call, not a display wave's.

## CLASS 146 — A REACTION READ THROTTLED LIKE A SPECULATIVE WAKE (born 2026-09-17 with CLASS 141's flip re-read, reported by the owner 2026-09-17 22:5x CT "why does the plan go dormant when the bias flips", fix/flip-reread-immediate, W-FLIP-REREAD-IMMEDIATE)

**Shape.** The structure_flip read (CLASS 141) reused the level-wake gate
verbatim: class-47 cooldown (30m since the last wake-authored version) and the
shared `wake_min_interval_min` throttle on `at.lastPlannerWakeAt`. Both are LOAD
rules born from a 7-day measurement of wake FLOODS — a wake CONDITION that is
continuously true and needs pacing. A flip read is not that: it reacts to a
machine-confirmed event (two 5m closes beyond the flip line with the ATR
buffer) that fires once. Live shape: a level wake authors a version, the flip
fires minutes later, and the bot sits dormant with no plan in the new direction
for up to 30 minutes — the dormant line is loud and correct, and the skip line
reads like an ordinary wake being paced.

**How it hid.** "Same preflight and wake cadence as a level wake" was written
as a feature (reuse the proven gate) and reviewed as one. Nothing asked which of
the gate's rules are SAFETY (cutoff: a plan authored inside 25 min of the flat
can never be entered) and which are THROTTLE (cooldown, min-interval), so the
throttles rode along.

**Probes.**
- For every read trigger that reuses a wake gate, classify each rule in the
  gate as SAFETY or LOAD, and ask whether the trigger is a SPECULATIVE wake (a
  continuously-true condition that needs pacing) or a REACTION to a confirmed
  event (fires once, must not wait behind an unrelated earlier wake).
- A reaction read may set the shared wake clock (ordinary wakes back off from
  it) but must never READ it; grep the trigger's gate for
  `lastPlannerWakeAt` and `SkipForCooldown`.
- Removing a throttle from a retrying path needs its own bound: the dormant
  branch calls back every scan cycle, so a LAUNCH that wrote nothing (3 model
  calls) would relaunch every cycle. Bound it on the read's OWN last launch,
  per trader (a process-global map keyed by plan id let one trader's — and
  one test's — failed launch hold another's retry), and never on a refusal.
- A fixture for "the flip fires N minutes after a wake version" must respect
  `describeActivePlanDeath`'s `sinceMs = row.CreatedAt`: the condition's bars
  are windowed from the VERSION's birth (CLASS 139 anchors only the hold), so
  two 5m closes must fit after it — N is at least ~10–15, not 3.

**Fix pattern.** W-FLIP-REREAD-IMMEDIATE: `maybeRereadAfterFlip` keeps the
once-key, in-flight guard, preflight, class-47 CUTOFF and the one-stream defer;
drops SkipForCooldown (CooldownMin: 0) and the min-interval read; logs
"🗓️ structure_flip read … — immediate (flip reads are exempt from
cooldown/min-interval; cutoff + stream guard still apply): <which would have
held>" only when a throttle would have applied; still sets
`at.lastPlannerWakeAt` at launch; and holds a relaunch after a launch that
wrote nothing for `wake_min_interval_min` from that launch
(`at.flipRereadLaunchAt`). Tests at the production call site
(`maybeRunSessionReadsAt`, real read path, AI client scripted) in
`trader/flip_reread_cto_test.go`.
## CLASS 145 — A HALT'S AGE READ AS CLOCK DRIFT WIDENED A NEWS BLACKOUT BY HALF AN HOUR (born 2026-08-30 with F6's uncapped widening, reported by the owner 2026-09-17 21:4x CT "BOJ 21:30 ±15m +39m (clock drift) 20:36–22:24", fix/drift-widen-cap, W-DRIFT-WIDEN-CAP)

**Shape.** F6 measures "clock drift" as local clock minus the freshest 1m
bar's close (`kernel/clock_drift.go FeedClockDriftMs`: `now − (OpenTime +
60s)`). Under a live feed that is skew; under a HALTED feed it is the AGE of
the last bar. Class 36 lets a scheduled read author inside the CME 16:00–17:00
halt, and on 2026-09-17 the ASIA read did: measured 1,810,527 ms at 16:30:11
CT (journal 44334, the plan-write path → "+31m (clock drift)" stored in the
plan's no_trade lines) and 2,326,426 ms at 16:38:46 CT (journal 45208, the
arm path `t1WindowsFor` → "+39m" rendered on the card). `ClockHoldDecision`'s
own comment said POSITIVE drift "is also exactly what a CLOSED market's old
bars look like", and then returned `widenMs = |drift|` for it anyway;
`WidenCTWindows` widened by `ceil(|drift|/60s)` with no cap and labelled it
"(clock drift)" for anything ≥ 60 s. A ±15 min band became 20:36–22:24 CT,
1h48m of a session blocked by a clock that was never wrong.

**Why it hid.** (1) The label told the reader the CLOCK was the cause, so the
card was self-consistent and the halt never came up. (2) The measurement was
honest — the bar WAS 38 minutes old — so no clock-health line disagreed;
clock-health at the 17:00 roll read −51 s with the feed back. (3) No journal
line ever said "clock-hold" with the word "stale"; the F6 warn line printed
the raw milliseconds and a grep for a 39-minute clock skew finds nothing
because none existed. (4) Every F6 pin injected 41 s / 61 s / 90 s; the only
pin with a large positive value (600 s, "positive drift never defers")
asserted the DEFER verdict and never looked at the windows.

**The rule.** A clock can honestly demand `ceil(tolerance/60s)` = 1 minute of
widening plus one boundary-rounding minute: `ClockWidenCapMinutes = 2`, applied
INSIDE `kernel.WidenCTWindows` so every caller is capped. A measurement is
CALLED clock drift only when `60 s ≤ |drift| ≤ 5 min`; beyond that the card
says nothing about the clock and `kernel.ClockDriftStaleNote` gives the
journal the real cause ("feed stale 38m — halt or gap, not clock skew; news
windows NOT widened beyond the 2m cap"). Both call sites — `plannerT1Lines`
(plan write) and `t1WindowsFor` (arm) — go through the one function; the arm
path now passes the SIGNED measurement.

**Probes.**
- Any consumer of `FeedClockDriftMs` / `LastClockDrift` that scales a
  behaviour by the magnitude: grep `WidenCTWindows|ClockHoldDecision|
  LastClockDrift`; a positive value is feed age until a live bar proves
  otherwise, so no magnitude-scaled action may be uncapped.
- The plan's stored `no_trade` lines vs the arm gate's windows: the plan
  freezes "+Nm (clock drift)" text at write time; the arm gate re-reads the
  live calendar slice (`t1WindowsFor` → `Calendar().GetSlice`) and re-measures
  drift per evaluation, so a card and a gate can disagree — the card is the
  write-time claim, the gate is live. Pinned:
  `trader/clock_widen_cap_test.go TestArmPathFollowsLiveCalendarCorrection`.
- Journal grep for the class: `clock-hold: T1 .* widened by |drift| [0-9]{7,}ms`
  (≥ 1,000 s) on any read whose timestamp is inside 16:00–17:00 CT or a
  weekend. Post-fix the line reads `widened by Nm (|drift| Xms, cap 2m)` and
  is followed by the stale note.
- A pin that injects a large positive measurement MUST assert the WINDOWS and
  the LABEL, not only the defer verdict. Pinned: `kernel/clock_widen_cap_test.go`
  (2,326,426 ms → +2m, unlabelled, note names 38m), `trader/clock_widen_cap_test.go`
  (arm path via `currentT1Windows`, plan-write step via `plannerT1Lines`; 42 s
  → +1m unlabelled, 90 s → "+2m (clock drift)" unchanged).

## CLASS 148 — A FLIP LINE AUTHORED ON THE WRONG SIDE OF PRICE CAN NEVER BE TOUCHED, SO IT NEVER FIRES (born 2026-08-27 with the P1c touch gate on the structured flip{} object, reported by the owner 2026-09-17 ~23:00 CT "at the flip point it re-reads and the bias is still the same", fix/flip-line-side-of-price, W-FLIP-LINE-SIDE-OF-PRICE)

**Shape.** A structured line carries a price and a side, and the machine fires
it only after price TOUCHES the line from the near side after the plan is born
and then closes beyond it on the stated side (`PlanConditionFiredSince`, the
P1c touch gate: `if !levelTouched(judge, c.Price, nowMs) { return false, "" }`).
CLASS 140 taught the validator to judge the side against the BIAS; nothing
judged it against PRICE. A short bias with flip{side above} is the right
direction — but if the line already sits BELOW price when it is written, price
is on the far side of it from birth: it can never be touched from the near
side, so the flip can never fire and the plan cannot flip by construction. The
plan re-reads at the "flip point", the model (correctly, on its own terms)
keeps the bias, and the owner watches the same bias survive its own flip.

**The live story (2026-09-17 ASIA v2, plans table read-only) [A].**
Plan `2026-09-17:ASIA:8d5c8af5_…_deepseek_1781246265` v2, created 22:52:04 CT on
the structure_mss wake: bias.direction="short", flip={29747.50, side "above",
rule "5m_close", flip_to "long"}, death={29755.50, side "above", rule "2x5m"};
the authoring price (facts.Price, the last closed 1m close
`AssembleResearchLevels` handed the write site, levels_assemble.go:217) was
29764. BOTH lines sat below price with side "above". The direction check
passed (short → long on a close above IS the right side), the prose cross-check
passed (29747.50 was in the prose), and the plan shipped un-flippable. v1
(16:38, flip 29772.62) and v3 (23:18, flip 29769) had the line above price;
only v2 was born impossible. The death line was also born crossed — a plan
born dead — and the existing born-dead refusal (`validateAuthoredScenariosAt`)
never saw it, because it evaluates ONLY the scenario `invalid` prose grammar
on 1m closes and never reads the death object.
(W-EXEC-TRUTH W2 D5, 2026-09-23: the born check now also judges death{} and
flip{} on every 5m group closed between the read clock and publication — see
the CLASS 189 "a write-time check that judges only the read-time price or the
latest window" at the end of this file.)

**Why it hid.** (1) Two validators each answered a real question — side vs
bias, number vs prose — and a reader assumes "the flip is validated". The
third relation (side vs PRICE) was in nobody's list. (2) The touch gate is
correct and necessary (wick-through immunity), and its precondition — the
line starts on the far side — was an unstated assumption of the author, not a
rule. (3) The failure is silent in the same way as CLASS 140: an impossible
flip is indistinguishable in the journal from a flip whose level was never
reached.

**The fix shape.** ONE predicate, `kernel.lineBeyondPrice`, worn by two names:
`FlipLineBeyondPrice(flip, price)` and `DeathLineBeyondPrice(death, price)`,
siblings of `FlipDirectionContradiction`. Called from the write-site validator
(`ValidatePlanDocWithFactsMachine`, AFTER its `facts.Price <= 0 → schema-only`
skip, so an unknown authoring price NEVER rejects — the rule does not invent a
price; the write site WARNs `flip/death line side-of-price UNJUDGED` instead)
with the sentence `flip{above 29747.50 → long} is already below price 29764.00
at authoring: a flip line must sit on the far side of price (it can never be
touched from the near side)` (death: `… (the plan would be born dead)`; a line
AT price reads "already at price"). Class-38 discipline: prompt-contract row
(`MustAppear` guarded by `ValidatePromptContracts` for every
`plannerOutputContract` variant), a rendered sentence, and a repair excerpt
`RepairFlipSideOfPriceLaw` routed on the rejection's own words ("far side of
price") and registered in `ValidatorHints`. The doc now carries
`price_at_write` (the facts.Price the lines were judged against) so the read
path can judge stored plans without inventing a price; a row without the stamp
is judged from the tape's last close at or before its `created_at` (≤10 min),
else left UNJUDGED and unmarked. Read path: `noteLinesBeyondPrice`, called first
by BOTH stored-plan evaluators (`describeActivePlanDeath`,
`describeDormantCleared`), once per plan version per line —
`flip_line_beyond_price plan=… v… (site) …` / `death_line_beyond_price …` —
the CLASS 140 once-per-version idiom; the evaluation itself is unchanged.
Tests at the production call sites: the ASIA v2 shape rejected with the exact
sentence and the v3 repair accepted; the below-side mirror; unknown price →
WARN, no reject, no stamp (real attempt loop with a fake model); repair
excerpt routed from both texts and NOT from the direction text; contract
validated for 12 prompt variants; stored impossible lines named once across
two evaluations at both evaluators; the unstamped-row tape fallback.

**Probes.**
- For every structured line with a side (`death{}`, `flip{}`, `confirm{}`,
  arm legs), ask where PRICE was when it was authored and whether the
  evaluator's precondition (touch from the near side, close beyond) is
  satisfiable from that start. A rule that judges the side against another
  FIELD (CLASS 140) has not judged it against the WORLD.
- `sqlite3 -readonly data/data.db "select plan_id, version, json_extract(doc,'$.price_at_write'), json_extract(doc,'$.flip') from plans where json_extract(doc,'$.flip.side')='above' and json_extract(doc,'$.flip.price') < json_extract(doc,'$.price_at_write')"` (and the mirror) — a non-zero count on a shipped rule is the class in the store. Rows written before the stamp have no `price_at_write`; judge them from the tape at `created_at`, never from today's price.
- A "never fired" flip in the journal must be distinguishable from a "could
  never fire" one: `flip_line_beyond_price` is the probe. If the journal has no
  such line for an old impossible plan whose tape is still in the ring, the
  read path does not judge side-of-price.
- A born-dead refusal that names only scenarios has not looked at the death
  object. Grep the refusal's inputs, not its name.
## CLASS 147 — A WAKE RE-READ DURING A FLIP BREACH RESTARTS THE FLIP WINDOW: THE FLIP NEVER FIRES (born 2026-08-25 with the W6 wakes, reported by the owner 2026-09-17 23:2x CT "why at the flip point it re-reads and the bias is still the same", fix/flip-owns-the-breach, W-FLIP-OWNS-THE-BREACH; number assigned at merge)

**Shape.** CLASS 139 anchored the flip HOLD to the chain, and left the
flip CONDITION WINDOW on the version's birth on purpose (a new line must be
judged only on bars after it was written). But a wake re-read that keeps the
bias AND the line is a new version too, so its window restarts and the
confirm-close count returns to zero — and nothing stopped such a wake from
authoring while the line was mid-breach, or on a tape the flip evaluator had
just refused as stale. Two clocks were separated in CLASS 139; the third
(the condition window) and the wake behaviour were not.

**The live story (2026-09-17 ASIA, plan `2026-09-17:ASIA:…`, verified
against the store's 1m/5m bars).** v1 16:38:46 `ASIA_scheduled_read`, bias
short, flip `above 29772.62 → long` (5m_close), death `above 29797.88`. The
line was NEVER breached before v2: the highest 5m close before 22:52 was
29762.25 (22:45), the highest high 29764.5 (22:50). At 22:16:17 the 15m
MSS-up (29737.00 @22:15) woke the planner; the machine rebooted 22:38:27 and
the read was lost. At 22:42:33, on the post-boot cache, the journal reads

	flip_eval_skipped plan=… v1 flip=stale_bars (age 453s)
	🗓️ structure MSS on ASIA 2026-09-17 (MSS-up 29737.00 @22:15 CT …) — waking the planner

in the SAME second: the flip evaluator refused the tape and the MSS wake
authored on it (R3). v2 landed 22:52:04, bias short, flip MOVED to
`above 29747.50` (Δ25.12 pt) — BELOW the price at authoring (22:50 close
29764.0) — and no post-birth bar ever touched 29747.50 (22:55 low 29749.0,
23:00 low 29754.75), so the P1c touch gate could never pass on v2's flip.
At 23:10:46 v2 went `DORMANT — death-condition: 2x5m close above 29755.50`,
never having flipped. So on 09-17 the hypothesis "price crossed the line,
the wake restarted the count" is FALSE for v1; what the day shows is R3 (a
wake authored on the stale tape) plus the moved-line case, and the owner's
"re-read, bias same" is the same-bias v2. The count-restart (R2) is the
09-16 shape (v10–v13 stepping the line DOWN 29500.25 → 29418.80 as the tape
climbed) that CLASS 139 fixed only for the HOLD.

**The rules (kernel/flip_breach.go, no knobs).**
- R1 *the flip owns the breach*: while the ACTIVE plan's flip line is
  breached — touched in-window and ≥1 rule-TF close beyond the buffered
  line, the SAME measurement `PlanConditionFiredSince` fires on
  (`conditionCloses`) — and has not fired, ordinary wakes (level_event,
  structure_mss) are DEFERRED: `🗓️ wake deferred: flip line breached (<side>
  <price>, closes N/2) — the flip evaluator owns this plan until it fires or
  price closes back`, once per version; `🗓️ wakes resume …` once when price
  closes back inside. Scheduled reads, death re-plans, owner reads: untouched.
- R2 *a same-bias wake keeps the flip window*: `ResolveFlipConditionAnchor`
  walks the chain back from the version while bias, flip side and a flip
  price within `FlipLineClusterTolerance` (the level map's 12-tick / 3.00 pt
  width) hold; the window opens at the EARLIEST run member's birth
  (`🗓️ flip window: … keeps the chain's flip line … closes counted from vK's
  birth`). A line moved further is a new line: window from the version's
  birth, `🗓️ flip line MOVED on … Δ… > 3.00 pt tolerance`. A re-plan version
  starts a run; a bias change or a version with no line breaks it. The hold
  anchor (CLASS 139) is unchanged; death keeps the version window.
- R3 *stale bars block wakes too*: when the flip evaluation is skipped (G7,
  `flip=stale_bars`), ordinary wakes are deferred for the same reason
  (`🗓️ wake deferred: flip evaluation skipped (stale_bars, age Ns) …`). This
  closes the gap between the flip evaluator's 5m+90s staleness cap and the
  planner preflight's `feedDownAfter()`, which is where the 22:42:33 wake got
  through.
- A breach the evaluator cannot fire on (a line born beyond price and never
  touched — v2 above) is NOT a breach: deferring wakes on it would park the
  plan forever behind a flip that cannot fire.

**Probes.**
- For every predicate windowed by a row's birth, ask what ELSE appends a row:
  a wake re-read that changes no state restarts every window keyed on
  `row.CreatedAt` (CLASS 139 asked this of the hold; ask it of the window).
- A gate that refuses to JUDGE on a tape (G7 stale) must also refuse to
  AUTHOR on it: grep the wake paths for a freshness check that is weaker
  than the evaluator's (`FlipEvalMaxStaleMs` vs `feedDownAfter`).
- Two measurements of one line (breach vs fire) must be ONE function; a
  wake that measures the buffer or the touch gate differently from the
  evaluator will defer on breaches that cannot fire, or author through ones
  that can.
- Journal counter-read: a `🗓️ level wake … waking the planner` or
  `structure MSS … waking the planner` inside the minute of a
  `flip_eval_skipped … stale_bars` line, or while `closes N/2` is climbing,
  is this class.
- Pinned at the production call sites: `trader/flip_breach_test.go` (1/2 →
  deferred, 2/2 → dormant:flip + structure_flip read; closes back → resume;
  same-bias wake within tolerance fires from the chain window while the
  version window reproduces the miss; moved line → birth, logged; stale →
  both wakes deferred; death-dormant and a no-row scheduled read untouched;
  the ASIA 09-17 replay on the live tape `flip_breach_fixture_test.go`) and
  `kernel/flip_breach_test.go` (resolver runs/breaks, breach-state parity
  with the evaluator, two-window evaluator byte-identical when the windows
  agree).

## CLASS 149 — A BAR STORE KEYED WITHOUT THE CONTRACT DROPS THE NEW CONTRACT'S OVERLAP AT EVERY ROLL (born 2026-08-26 with the bars table, made visible 2026-09-14 at the Sept→Dec roll as CLASS 143's hole, owner-authorized schema change 2026-09-18 00:3x CT "full fix 4", fix/bars-contract-key, W-BARS-CONTRACT-KEY; number assigned at merge)

**Shape.** `bars` was keyed `(symbol, tf, open_time_ms)` with `contract`
outside the key, and both writers resolved a collision on that key alone
(InsertBars upsert; ImportBars DO NOTHING). At every quarterly roll NT8 serves
the NEW contract's history (~2,000 bars per TF at subscribe) for minutes the
OLD contract already holds: wherever the old contract had a row the new
contract's bar was silently dropped, wherever it did not (holidays, Sunday
evenings, NT8-off windows) it landed as a stray. Measured on the 2026-09-18
copy of the live DB (read-only `.backup`, 1,906,992 rows): the LANDED
complement — MNQ 12-26 rows older than 12-26's first live bar — is 1m 451 ·
3m 12 · 5m 17 · 15m 10 · 1h 4 · 3d 14; the DROPPED overlap cannot be counted
from the table at all, because the old key never stored it (the brief's
12–26 rows per roll is [B]). The root is the key: a table that cannot hold
two contracts on one minute cannot hold a roll.

**How it hid.** (1) The drop was `ON CONFLICT … DO UPDATE … WHERE NOT
(live overwrites)` / `DO NOTHING` — a silent, counted-nowhere path, exactly
the shape of "silent refusal paths". (2) `BarsIntegrity` asserted dups=0 on
the three-column key, so the table always looked clean. (3) Every decision
reader is contract-scoped and never asked for the minutes it could not have.
(4) CLASS 143 fixed the display symptom and named this as owner-gated.

**Probes.**
- Which key is live: `SELECT name FROM pragma_table_info('bars') WHERE pk>0
  ORDER BY pk;` → `symbol tf contract open_time_ms` after the wave.
- Roll overlaps (minutes held by two contracts): `SELECT symbol,tf,open_time_ms,
  COUNT(DISTINCT contract) FROM bars GROUP BY 1,2,3 HAVING COUNT(*)>1;` —
  EXPECTED non-empty after a roll on the new key; always empty on the old key
  (by construction, not by health). The nightly `✅ bars integrity` line prints
  `roll_overlaps=<n>`, read.
- Boot line, first boot: `🗄 bars: key migrated to (symbol,tf,contract,open_time_ms)
  — rows=<n> backup=<path> old_table=<name>`; later boots `🗄 bars:
  key=(symbol,tf,contract,open_time_ms) (migrated <date>) rows=<n>`; refusal
  `🗄 bars: migration FAILED — <err>; old table intact` (ERROR level, and the
  bot keeps writing on the legacy key).
- A time-only reader on the new key sees TWO rows per overlap minute. Every
  reader must either name a contract or dedupe per open time with a stated
  rank. The wave's audit table (PR body) lists each one; a NEW time-only
  reader is a new instance of this class.
- Any `ON CONFLICT(<columns>)` in this repo must name a key the table
  ACTUALLY has: the writers read it (`barsConflictTarget`) so a migration that
  failed open still writes. A hard-coded conflict target on a migrated table
  is refused by SQLite ("does not match any PRIMARY KEY or UNIQUE constraint")
  and persistence dies with one WARN per batch — which is what a
  PRE-MIGRATION BINARY does on the migrated table (tested:
  `TestBarsKeyOldBinaryStatementsOnTheMigratedTable`). Rollback is
  `deploy/RESTORE.md` "Roll back the bars CONTRACT-KEY migration".
- The renamed old table KEEPS its indexes on purpose: the old binary's Migrate
  checks `idx_bars_sym_tf_time_unique` by NAME only, and with no such index it
  runs the 2026-08-27 dedupe block that DELETEs every tf<>'1m' row. Dropping a
  backup table's indexes to tidy up would arm that.

**Fix.** `store/bar_contract_key.go` (detect · VACUUM INTO backup verified by
row count, refused → migration refused · bars_v2 copy + rename in one
transaction · idempotent · fail-open with the report's Err); `store/bar_history.go`
(Contract in the PK, legacy dedupe gated on the legacy key, conflict targets
from the live key, dups on the full key); `store/bar_contract_roll.go`
(LatestContract/ContractAt: on a shared minute the row that TRADED wins, then
the later expiry parsed from the broker's label); `api/handler_bar_truth.go`
(one contract); `cmd/bars-export` (contract+source columns);
`trader/ninjatrader/bar_persist_wire.go` (🗄 boot line, roll_overlaps).
Pins: `store/bar_contract_key_test.go` (legacy-shape migration, idempotent
second boot, backup-refused fail-open, fresh DB, two contracts one minute,
tie-break, old-binary statements, opt-in live-copy run via
`BARS_KEY_LIVE_COPY`), `trader/ninjatrader/bar_persist_contract_key_test.go`
(the persister's call site), `store/history_import_test.go` E1 (same-contract
collision skips; another contract lands beside).

## CLASS 150 — A T1 BLACKOUT THAT NEVER ASKED WHICH CURRENCY THE EVENT WAS IN (born 2026-08-19 with W3's red-news blackout, reported by the owner 2026-09-17 evening and ordered 2026-09-18 00:3x CT "fix 3", feat/t1-currencies, W-T1-CURRENCIES)

**Shape.** W3 turned every T1 (red / High-impact) event of a session's
calendar slice into a HARD ±15m no-trade window (`kernel.T1BlackoutWindows`)
and the session currency filter (`calendar.SessionCurrencies`: ASIA = USD+JPY+
CNY, LONDON = USD+EUR+GBP) decided which events were IN the slice at all. So
the only currency question anyone ever asked was "is this event relevant to
the session" — never "does a red print in THIS currency stop an MNQ trader".
The stored 2026-09-17 slice (`calendar_slices`, source forexfactory, created
1789707301008) carried `BOJ Policy Rate` and `Monetary Policy Statement` at
`2026-09-18T02:54:00Z` JPY T1 (= 21:54 CT, ASIA) and three GBP T1 rows at
`11:00Z` (Official Bank Rate, MPC votes, Monetary Policy Summary — LONDON);
the 2026-09-18 slice carries `BOJ Press Conference` at `05:30Z` JPY T1. The
MNQ bot sat in a HARD window for a Japanese rate decision, and the same code
would have blacked out the London morning for the Bank of England.

**Why it hid.** (1) The card, the plan's no_trade lines and the gate all
agreed — they were three renderings of one unfiltered function, so no parity
test could disagree. (2) `PlannerCalendarEvent.Currency` existed and was
printed in the prompt's Calendar section, which made the currency look
"handled". (3) The CLASS 145 investigation the night before looked straight at
the BOJ window (`BOJ 21:30 ±15m +39m (clock drift)`) and fixed the WIDENING;
nobody asked why a JPY event owned an MNQ window in the first place — the
first bug on a line hides the second.

**The rule.** A gate keyed on an event attribute must READ that attribute
through a knob with a stated default, and the events it declines to gate on
must stay VISIBLE. `day_plan.t1_currencies` (`store.DayPlanConfig.
T1CurrenciesFor`: absent/empty → `[USD]`; `ALL`/`*` → every currency, the
pre-wave behaviour byte-identical; canonicalised upper-case/trim/dedupe at the
resolver). ONE split, `kernel.SplitT1(events, set)`: in-set → HARD window;
out-of-set → `🟠 <title> <HH:MM> CT (<CCY>) — red news, advisory only
(t1_currencies=<set>)` in the plan's no_trade lines, on the card (RulesBlock
advisory row, never behind the notes toggle) and in the prompt's Calendar tag
("ADVISORY only — NOT a machine blackout"), and NEVER in a gate window; an
event with NO currency → HARD (fail closed) and one `⚠️ T1 event without
currency treated as hard: <title>` per trade date. Every consumer reads
`at.t1Currencies()`: the arm gate (`t1WindowsFor`), the plan write
(`plannerT1Lines` + the machine no-trade band), the fade facts
(`fadeFactsAt`) and the planner input (`PlannerInput.T1Currencies`). Boot line
READ from the resolver: `🔴 t1_blackout=USD(default)` / `USD,EUR(saved)` /
`ALL(saved) (W-T1-CURRENCIES)`.

**Probes.**
- Any gate that iterates calendar events: grep `Impact.*T1|T1BlackoutWindows|
  SplitT1`; a new caller must pass the resolved set (the signature no longer
  admits an unfiltered call — `T1BlackoutWindows(events, currencies)`).
- `calendar.EventsForSession` drops events whose currency is not in the
  session filter BEFORE the split, so an uncurrencied event cannot reach
  `SplitT1` from a stored slice today; the fail-closed branch is defensive and
  pinned at the kernel (`kernel/t1_currencies_test.go
  TestT1EventWithoutCurrencyIsHardAndNamed`). If the session filter ever
  admits unlabelled rows, the WARN line in `t1WindowsFor` is the tell.
- Card vs gate parity is proved at PRODUCTION call sites, not helper
  self-consistency (class 53): `trader/t1_currencies_test.go` runs the REAL
  write core (`runPlannerReadCoreWithFactsGrades` with `plannerT1Lines` as the
  extra lines) and reads the stored doc's `no_trade` + `no_trade_windows`
  against `currentT1Windows → sessionGateDecision(…, at.sessionRunnable)` and
  `fadeFactsAt(...).InT1Blackout`, under the default (BOJ advisory, USD hard)
  and under `ALL` (both hard).
- The pre-wave function is copied VERBATIM into `kernel/t1_currencies_test.go
  legacyT1BlackoutWindows` and `ALL` is pinned `reflect.DeepEqual` to it on a
  three-currency fixture, so "restores the old behaviour" is a comparison
  against what shipped, not against the new code's own idea of itself.
- Journal grep for the class: `red-news blackout: .*(JPY|GBP|EUR|CNY)` on an
  MNQ trader under the USD default — should never appear; the advisory line
  is `🟠 … advisory only (t1_currencies=USD)` on the card and in the plan row.
- CLASS 145's fixtures (`trader/clock_widen_cap_test.go`,
  `kernel/clock_widen_cap_test.go`) keep their JPY BOJ event by passing
  `T1Currencies: [ALL]` explicitly — the class was born under the every-
  currency regime and the cap is asserted there, not the currency split.

## CLASS 151 — A KNOB WITH NO CONTROL CAN STILL CARRY A VALUE THE OWNER SET, AND A REMOVAL THAT IGNORES IT CHANGES LIVE BEHAVIOUR SILENTLY (born with the Day Plan knob census 2026-08-19, ruled by the owner 2026-09-18 00:3x CT "full fix 6" on the Round 23/24 verdicts, feat/knob-prune, W-KNOB-PRUNE — not a bug class, a prune protocol)

**Shape.** Fourteen Day Plan knobs were ruled dead or unneeded (7 remove, 5
fold, 2 dead fields). Five of them were stored NON-default on the owner's live
MNQ strategy (`structure_map:true`, `scenario_cap:5`, `realign_cap:10`,
`wake_on_htf_ob:true`, `levels_fresh_by_tf:true`) and one on every strategy
(`evening_digest:true`). A removal that deletes the field, the accessor and
the control also deletes the stored value's EFFECT — the prompt, the wake
set and the caps of the live trader change on the next boot, with no log,
no diff in the Studio, and a green suite (the suite tests the new default).

**Rule (L3 + L8 of the dispatch, now canon).** Before removing a knob: read
the live stored values (`sqlite3 -readonly data.db "select name,
json_extract(config,'$.day_plan') from strategies"`). A knob that is stored
non-default anywhere is FOLDED, not deleted: the code path keeps the shipped
default as a constant, the control goes, the JSON field stays readable, the
stored value is honoured at read and logged once at trader load —
`⚙ folded knob <name>=<value> honoured from stored config`
(`store.DayPlanConfig.FoldedKnobLines`, registry status `folded`). Every
removed or folded knob is pinned by a golden WRITTEN AT THE BASE COMMIT and
re-run after the change (`kernel/knob_prune_pin_test.go`,
`trader/knob_prune_pin_test.go`, `KNOB_PRUNE_WRITE_GOLDEN=1`), per stored
shape (nil / `{}` / the Studio seed / the owner's row verbatim / the only
all-off shape). A pin that is generated after the change proves only
self-consistency (class 53).

**Probes.**
- `grep -rn 'json:"<leaf>' store/` → if the field is gone, `sqlite3
  -readonly … json_extract` for the leaf must return only NULL / the default.
- Registry: a leaf still in the struct must be classified (`TestKnobRegistryIsComplete`);
  `folded` rows must name the reader the method-level detector finds
  (`TestWakeKnobsAreLiveThroughTheirAccessors`).
- Boot: the owner's trader must print one `⚙ folded knob` line per stored
  non-default; zero lines on a default-only strategy.
- A DELIBERATE behaviour change inside a prune (here `htf_score_multiplier`
  1.2 → 1.0) is measured before the golden is overwritten: identity tape 0
  seat / 0 order changes; stage-A 64 fixtures 33 change seat membership, 112
  seated grades move — in the report, not discovered at the boot.
- A coupling the collapse introduces (here `WakeOnHTFOrderBlocks` ANDed
  with the single switch) gets its own pin
  (`TestKnobPrunePin_WakeCandidates_SingleSwitchOwnsOB`) and a registry note,
  even when inert for every stored strategy.

## CLASS 152 — A SWITCH RULED OFF "UNTIL ITS PRECONDITION LANDS" WAS NEVER BROUGHT BACK WHEN THE PRECONDITION LANDED (born 2026-09-05 with the STOP_ENTRY_SEAM ruling, precondition shipped 2026-09-06, found 2026-09-18 07:4x CT by the owner "no trade since NY yesterday", docs/stop-entry-seam, W-SEAM-DOCS)

**Shape.** `STOP_ENTRY_SEAM` (`kernel/entry_law.go:StopEntrySeamOn`, only the
literal `on`) was ruled OFF on 2026-09-05 because `nt.CancelOrder` reported
success on a SEND and the broker once held nine working stops for one arm slot.
The boot line said so in words: `seam=OFF — NO stop entry is placed (owner
ruling 2026-09-05: cancel-confirmation wave owed; broker-side stacking)`. The
cancel-confirmation wave shipped the next day (`🧾 cancels: confirm=
broker-snapshot`) and nobody re-opened the ruling: every `kind=stop_entry` arm
(reclaim / continuation) was written, logged and never sent. Owner's DB
2026-09-18, since 09-04: 30 stop_entry arms / 0 fills vs 61 limit arms / 16
fills. The switch lives in each machine's untracked `.env`; no tracked file
(`.env.example`, `docs/PARTNER-BUILD.md`, `docs/partner-sync/*`) mentioned it,
so the mirrors could not even know there was a value to set.

**Why it hid.** The boot line was honest and READ, not literal — it printed the
ruling and its reason every boot — but a line that says "until X" is a promise
with no owner: the wave that delivers X has no reason to grep for the lines
that were waiting on it. Twelve days of `⏳ armed … stop_entry` looked like a
scheduler working as designed; only "no trade since yesterday" made it a bug.

**Probes.**
- For every env/config switch whose boot line, comment or ruling says "until
  X" / "owed" / "precondition": `grep -rn 'until\|owed\|precondition' kernel/
  trader/ .env.example` → list each X and check whether X has SHIPPED (a boot
  line, a merged class). Shipped + switch still parked = this class.
- A wave that ships a precondition greps the tree for the switches that named
  it and either flips them or writes down, in its report, why not.
- Any switch a machine sets in `.env` has its line in `.env.example` AND in
  `docs/PARTNER-BUILD.md`; the mirror's boot line is pasted as proof.

**Fix.** This docs wave (`.env.example` line, PARTNER-BUILD section, this
class); the guide clause for the seam is OWED at the next boot (guide law).

## CLASS 153 — A SILENT STRUCTURAL-GEOMETRY REFUSAL MADE EVERY reject PLAY AT A REFERENCE LEVEL UNARMABLE (born 2026-09-12 with 540c9e8d, found 2026-09-18 05:42 CT LONDON v1 S1, fix/geometry-refusal, W-GEOMETRY-REFUSAL)

**Shape.** The geometry gate refuses with an INFO-only composition line plus a
system_config counter — no WARN, no ⚔️ arm REFUSED. The evaluator meanwhile prints
`🎯 scenario S1 → ≈triggered`, so the journal reads "condition met, executor idle".
Two machine roots made every reject play at a session reference level unarmable:
(1) the identity map emits id=NULL for reference levels whose source window is
still developing (levelidentity.ID requires formed_close_ms; ONH/ONL mid-window
have none), the prompt instructs null, and the resolver's legacy:no_level_id path
accepts it WARN-only; (2) the frozen-zone match skips a source whose tf differs
from the identity tf, and VWAP-family/ONH sources carry tf "" against identity
tf "1m".

**How it hid.** Refusal and evaluation lived in two different voices: a counter
that no journal reader sees, and an evaluator that says the condition fired. 95
refusals since 09-13, 0 WARN lines, 0 trades.

**Probes.**
- `SELECT count(*) FROM system_config WHERE key LIKE 'structural_geometry:%' AND
  value LIKE '%"reason":"no_provenance"%'` vs the total; and in the journal
  `grep -c '"reason":"no_provenance"'` vs `grep -c '⚔️ arm REFUSED.*geometry'` —
  must NOT be N vs 0. Every refusal class needs a WARN voice de-duped by key.
- For every identity id a map emits NULL, ask what the PLANNER is instructed to
  write and whether the executor can resolve it — a WARN-only accept at the write
  site is a refusal at the arm site.
- A machine-written hint ("author null when the map id is NULL") is a contract
  that downstream enforcers must be able to honour.

**Fix shape.** W-GEOMETRY-REFUSAL (executor side): (a) one de-duped
`⚔️ arm REFUSED … geometry_<reason> (<detail>) entry=… level_id=…` WARN per
(geometry key, reason) change; (b) behind day_plan.geometry_reference_levels
(default ON, owner ruling "both fix now" 2026-09-18): (b1) stable `ref|` sha ids
for reference-anchor kinds without a formation close (resolved by
LevelByReferenceID; strict LevelByID untouched; stored NULL ids keep the legacy
path), (b2) an empty zone-source tf is a wildcard in the frozen-zone match, and
a matched NULL-WIDTH reference LINE is admitted as a zero-width band at the
anchor so the structural stop composes (line − buffer) — without the admission
half, the wildcard merely re-labelled the refusal (55 source_not_frozen became
58 unusable and nothing became armable). DS-101 owns the write-time feasibility
hint; the executor seam for both lanes is
trader.ArmGeometryVerdict(doc, sc, geometryRefLevels).

## CLASS 154 — A WRITE-TIME FEASIBILITY WARN THAT SAYS "THE GATE WILL REFUSE IT" AND WRITES THE PLAN ANYWAY (born with the arm-feasibility WARN 2026-08-28, found by the owner 2026-09-18 08:3x CT "fix all", W-WRITE-TIME-FEASIBILITY, fix/write-time-feasibility)

**Shape.** `kernel.ArmFeasibilityWarnings` (F4, 2026-08-28) computed, at plan
write time, exactly which arms the gate-at-arm chain would refuse every cycle
(R:R below ARM_MIN_RR, stop closer than 1.5×ATR5m) — and then logged one WARN
per arm and wrote the plan anyway. The owner's persistent journal shows 5 such
WARNs since 2026-09-17 22:38 CT, every one followed by a PLAN written in-session
(LONDON v2 07:32:52, NY v1 08:06:20) — WARN-then-write 5/5. The model never saw
the WARN (it is not in the prompt), so it re-authored the same shape; the
executor then refused the arm at arm time, printing the refusal to a log the
model also never reads. The system knew the plan was dead on arrival and wrote
it anyway.

**Why it hid.** A WARN is invisible to every consumer except a human reading the
journal; the write path is the only place that both has the verdict AND can make
the author do something about it. "Warn-first" was the right rule for the
bias-coherent warning (owner ruling 2026-09-04) but was inherited, unreviewed,
by the arm-feasibility WARN, where the warning's own text ("the gate will
refuse it") made the write a knowing contradiction.

**Probes.**
- `journalctl -u nofx --since <window> | grep -E "arm feasibility"` — every
  WARN whose session then wrote a plan is an instance of this class.
- Any write-site warning whose text names a downstream refusal, but which does
  not feed the repair prompt or a disabled-arm stamp, is this class.

**Fix.** The write site now runs the executor's own gate-at-arm predicates
(`armGateVerdictFor` on the COMPOSED leg via `composeArmStop`, the executor's
own geometry composition — canon 53, no re-implementation) per enabled arm,
plus the executor's OWN stop-side placement guard (`decideStopEntry`, CTO
amendment 2026-09-18: 29 of 30 stop_entry arms since 09-04 were wrong-side at
write — source: DS-104 replay, bridge msg 1789737991919-898085): attempts
1..N-1 send the scenarios back as a restriction-with-hint repair error naming
the refusal, the numbers and the fix vocabulary; the last attempt writes the
unarmable arms with `arm.enabled=false` + `arm_disabled_reason` (the reason
CLASS: min_sl / rr / geometry_<code> / stop_side_wrong) + one WARN + the
`arm_disabled_at_write:<trader>:<date>:<session>:<class>` counter. The
classes reuse the executor's own armRefusalClass, which can also yield
`other` / `not_armable` / `veto` — the short list above is not exhaustive. The check
runs LAST among the validators so it never pre-empts a hard reject — the
cost of that ordering is one extra model round-trip when an earlier validator
has already burned attempts 1..N-1 (a two-defect model writes on attempt 2
OFF and attempt 3 ON; a three-defect chain now fail-closes where it wrote
before). TestWriteTimeFeasibilityNeverPreemptsHardRejects asserts that flow
shape (hard reject attempt 1, the hint rides the attempt-3 prompt, the arm
is disabled at attempt 3) — it does not measure the fail-closed rate, which
needs the live journals and is NOT claimed here. Knob
`day_plan.write_time_feasibility`, nil/unset = ON; explicit false = the old
WARN-only behaviour byte-identical (pinned by a parity test at the rendering
seam). The session-risk band is deliberately NOT judged at write (time-based).

## CLASS 155 — THE CARD SHOWED THE EVALUATOR'S VERDICT AND NEVER THE EXECUTOR'S (born 2026-08-27 with the scenario evaluator line, found 2026-09-18 by the owner — "why no trade" — feat/arm-state-ui, W-ARM-STATE-UI)

**Shape.** The plan card's per-scenario verdict (🎯 scenario S1 → ≈armed / ≈triggered)
is the EVALUATOR's: it is computed from price alone and can read "≈triggered" every
cycle while the EXECUTOR refuses the arm every cycle. The executor's verdict — whether
the arm was refused, and why — lived only in system_config counters and a single WARN
line (2026-09-18 LONDON v1 S1: ≈triggered 05:42:24, refused
`geometry_no_provenance/scenario_level_id_missing`, invalidated 05:46:24; 93 of 95
geometry records since 09-13 are refusals). The owner read "armed"/"triggered" all day
and believed the bot was about to trade; it was not.

**Rule (probe).** A scenario rendered ≈armed/≈triggered for >2 cycles with an executor
refusal record and no executor text on the card = this class. The card must show, per
scenario, what the EXECUTOR decided for the displayed plan version — `not attempted` /
`refused: <reason> (<detail>)` / `armed #<id>` / `filled #<id>` / `cancelled:
<state_reason>` — sourced ONLY from armed_orders rows and the executor geometry records;
when no record exists render nothing (no dash, no "ok"). An uncomputed executor state is
absent, never fabricated.

## CLASS 157 — A DEATH LINE THAT PARKS THE PLAN UNTIL PRICE RETURNS SITS OUT THE SESSION WHEN IT DOESN'T (born 2026-08-25 with the plan-lifecycle wave, found 2026-09-18 09:10 CT NY v2, fix/death-reread, W-DEATH-REREAD)

**Shape.** A structured death-condition kill writes `dormant:death:` and the
planner stops there; only a `flip-condition:` kill ever re-reads
(`maybeRereadAfterFlip`). When the market runs 100 pt away from a dead plan,
"wait for it to close back" means no plan for the whole session. 2026-09-18 NY:
v1 flipped short at 08:46:20, v2 (long, kill line 29767) died at 09:10:18, and
price sat at 29746 three hours later — the bot authored nothing for the rest of
the NY session. The owner: "why the fk my bot stop right here".

**Probe.** `dormant:death:` rows in plan_lifecycle_log with no later
`rearmed`/`superseded` for that plan_id+version during the session; count on the
DB copy since 09-01 with ids. First probe [A] on pre-bars-key-20260918-022516.db
(read-only): 15 `dormant:death:` events since 2026-09-01, 6 re-armed, **9 never
re-armed** (ids 16, 27, 28, 33, 39, 44, 51, 53, 56) — 60% of death lines sat the
session out.

**How it hid.** Dormancy is a protection, not a decision: the log line says
"auto re-arms when price closes back" and the re-arm predicate exists, so the
dead plan looks handled — nothing ever states "and if price does NOT return,
this session has no plan". The flip half of the hysteresis re-reads (the bias
was wrong); the death half only parks.

**Fix (W-DEATH-REREAD, owner ruling 2026-09-18 12:3x CT "fix all").**
`day_plan.death_reread` *bool, nil = ON: a fired death-condition kill still goes
dormant exactly as today AND launches ONE budgeted planner re-read (trigger
`death_replan` — it SPENDS one class-35 replan unit, unlike the free flip read;
at budget exhausted → dormant only with one WARN naming the budget). The read is
bias free and carries the death evidence (dead version, kill line with the price
at death, break direction); the fresh version supersedes the dormant one
(`superseded:death`). Guards: the once-key, in-flight guard, preflight, class-47
cutoff and self-backoff are the flip read's own body; a death-born plan carries
the 30-min flip hold anchor (class-35 trigger is a replan anchor) and cannot
itself die inside a 10-minute birth wick (its first death check runs only after
2 full 5m closes post-birth). Explicit false = today's behaviour byte-identical.
Counter: `death_reread:<trader>:<date>:<session>`.

## CLASS 156 — A ROLL STITCH THAT TRUSTS THE OLD CONTRACT'S STORED AGGREGATES AND SHIFTS NOTHING SHOWS A HOLE AND A 290-POINT CLIFF ON EVERY TIMEFRAME OF THE ROLL DAY (born 2026-09-14 at the Sep→Dec roll with the per-timeframe AddOn switch, found 2026-09-18 16:5x CT by the owner "chart on 14 no good on all tf", fix/roll-day-chart, W-ROLL-DAY-CHART)

**Shape.** Each timeframe rolled at a different hour, so the old contract's stored bars stop early on every tf (1m 10:33 · 3m 10:36 · 5m 09:55 · 15m 08:15 · 30m 06:00 · 1h 01:00) while the new contract's first live bars start at their own hour — the stitch showed a hole at the seam on every tf. And the stitch shifted nothing, so every tf showed the ~290-point Sep/Dec basis as a price cliff. The 1m rows of the old contract were otherwise complete.

**Rule (probe).** Per contract per tf, compare the last stored bar time on the roll day against the 1m last bar: any tf whose last bar is earlier than the 1m last bar is this class. The display fix derives the prior segment from that contract's 1m rows with the planner's own bucket helper, shifts it by the basis measured at THAT timeframe's own seam (the new contract's first bar of that tf vs the last prior 1m close before it — the pair sits <1 minute apart), marks derived/adjusted on each bar and the envelope, never touches the current contract or volume, and keeps CHART_ROLL_STITCH=legacy byte-identical. Follow-up (2026-09-19, owner: the first boot still showed the cliff): the shipped basis was measured at the TRUE 1m switch, hours after the 15m/30m/1h seam — the Sep/Dec basis decays from ~290 in the morning to ~15 at that switch, so one 15.25 shift left a ~274-280-point cliff; measuring at each tf's own seam makes the seam continuous by construction. Second follow-up (2026-09-19, owner: "5m day 11" hole on every tf): the prior contract's 1m rows have interior gaps where NT8 was off (Sept 11 ~00:29-07:45 CT) while the current contract's imported 1m rows cover them — the derive now fills such gaps with the current rows converted into the prior contract's price space (basis at the nearest minute both contracts share); gaps neither contract has stay gaps, never fabricated.

## CLASS 158 — Inverted async cancellation guard blanks a live chart

**Wave:** `fix/planner-chart-response-20260922`. **Found:** 2026-09-22.
The roll-chart change `0e7573485` inverted the PlanMiniChart response guard
from `stop` to `!stop`. Mounted charts start with `stop=false`, so every
successful response returned before `series.setData`. A disposed request could
also overwrite a replacement interval while its chart reference remained live.
The built frontend contained the same inversion. Existing chart tests exercised
the canvas-less placeholder and did not test the async candle delivery path.

**Probe:** mount the production component with a working chart adapter and a
nonempty successful response; assert candle delivery and polling. Resolve an
older interval request after the replacement request and assert it cannot
overwrite the series. Resolve after unmount and assert no write or further poll.
**Law:** test both live response delivery and cancellation at the production
component boundary; placeholder rendering alone does not verify chart loading.

## CLASS 159 — single-consumer pause mistaken for a global hold

**Wave:** `feat/one-button-m2-maintenance-hold` (W-ONE-BUTTON M2). **Found:** 2026-09-22, building the one-button partner update.
**Shape.** Before an update replaces the binary and the AddOn, every producer of new entries must stop. The only brake the bot had was `stop_until`/Resume, which pauses ONE consumer: one trader's AI decision path. Everything else kept sending:
- the armed executor;
- the Picture HTF evaluator, whose registry never unregisters, so a stopped or deleted trader still reaches `pictureHtfSend`;
- the planner (a 5–20 min AI call a restart would kill);
- the reconnect queue, which wrote a queued entry on the next connect with no permit held;
- the AddOn itself.

Any per-trader Resume could also lift the pause. A pause scoped to one consumer reads like a hold and is not one.

**Probe.**
- (1) Enumerate every producer of a wire entry: every `SendSignal` caller, every `CreateOrder` entry in the AddOn, and every registry that feeds one. Check that each consults the ONE installation hold at its send point.
- (2) Check that the queue and reconnect paths honour the hold **connected or not**. The first cut returned early on "no connection" before checking the hold, which parked held entries for as long as NT8 was down.
- (3) Check that no API route, Resume or clear-freeze can write or clear the hold. An AST scan pins the allowlist.
- (4) Check that the gate reading "drained" covers every trader id, loaded or not; every account the AddOn can see; and every planner-class claim. An unevaluable leg fails. A vacuous "not applicable" pass is a failure.
- (5) Check that a refused send never latches "placed" and never cancels siblings.

**Law:** a hold is installation-wide, file-backed, written only by the operator or the updater, and read at every send point. A pause is not a hold.

## CLASS 160 — a queued send recorded as a fill (pre-existing; found in M2, fix deferred)

**Found:** 2026-09-22 in W-ONE-BUTTON M2 (CTO condition 3 on M-2), [A] at the production caller (`TestDroppedAIEntryIsForgottenAndTheGateStaysClosed`).
**Shape.** An AI entry sent while NT8 is disconnected is QUEUED: `SendSignal` returns nil and `TCPTrader.placeEntry` returns `"submitted"`. Its result carries `"signal_id"` but no `"orderId"`, so `recordAndConfirmOrder` formats the missing key as the string `"<nil>"`, which is not skipped. It then:
- writes an order row;
- polls `GetOrderStatus` (still "pending") for ~3 s;
- falls through to `recordPositionChange`, which writes an **OPEN** `trader_positions` row at the mark price with `entry_order_id "<nil>"` and emits a P0 "Filled …" alert.

That happens for an entry that never left this process. If the queued entry is then refused (stale queue age, or the maintenance hold's drop), the DB carries a position NT8 never had, and nothing links that row to the signal.

**Probe:** send an AI entry with no client connected and run the production caller: an OPEN row with `entry_order_id "<nil>"` appears.

**Status:** M2 does not fabricate a close for it. A hold drop forgets the entry, logs ERROR naming the signal and `db_open_positions`, raises P1, and the installation gate stays closed on cutover leg 1 until an operator reconciles. **The fix needs its own owner-ruled wave:** record the signal id as the order id, and do not record a position before a received fill.

**Fixed in W-EXEC-TRUTH W0a (`fix/exec-admission-gate`, 8edf3d97).** The probe work found it wider than the entry above: a REJECTED entry was recorded OPEN too, the AI poll adopted another path's fill through the shared `lastEntrySignalID`, and every AI order row collapsed onto one `"<nil>"` row. NT8 opens are now keyed by the signal the entry returns, and a position is recorded only on a fill for THAT signal (`RecentFillFor`; a new `RecentRejectFor` ring for rejects). No evidence leaves the order row NEW and records no position; the maintenance drop settles that row CANCELED. Pinned: `TestQueuedAIEntryWithNoFillRecordsNoPosition`, `TestRejectedAIEntryRecordsNoPosition`, `TestFilledAIEntryRecordsThePositionAtItsFill`, `TestAnotherPathsFillIsNeverTheAIsFill`, `TestTwoAIEntriesAreTwoOrderRows`, `TestDroppedAIEntrySettlesItsOrderRowAndNoPositionExists`.

**Addition (W1b E15, `fix/executor-owed-1`):** a queued entry that fills LATE (after the ~3 s poll left its order row NEW) and is then materialized by reconcile as an untracked position is tagged with its OWN signal from the fill ring (`lateEntryFillFor` / `tagLateEntryFill`, `trader/ninjatrader/reconcile_late_fill.go`), claims its AI order row only while that row is still `NEW`, settles it FILLED at the ring's price, and writes the `trader_fills` row the normal path writes (`lateEntryFillRow`, trade id `nt8-late-entry-<order id>`). See the W1b class "a price-similarity lineage with no time bound…" below. Pinned: `TestLateAIFillMaterializesTaggedWithItsSignal`, `TestLateFillOfANonNewOrderRowStaysUntagged`, `TestLateAIFillWritesTheFillRowTheNormalPathWrites`.

## CLASS 161 — A HAND-SET BUILD LABEL TREATED AS PROOF OF WHAT IS RUNNING (born 2026-09-22, feat/one-button-updates, W-ONE-BUTTON M1)

**Shape.** The AddOn's `VL_BUILD_ID` is a constant a human bumps "on any additive wire change"
(`ninjascript/VLTraderTCPClient.cs:55`), mirrored by a Go constant (`provider/ninjatrader/order_snapshot.go:214`)
and pinned equal by a test that proves only that two literals match. Seven commits changed the AddOn under
`2026-09-07-h1` without a bump; at least 8 distinct source states reported the same id. The Go side keeps the last
received id in a process-global slot that no disconnect clears, with no connection or process epoch — so after an
NT8 restart the "received build" is the PREVIOUS process's until a new frame arrives, and a Go restart gets a
fresh hello from the SAME AddOn instance. Any verifier that says "the new AddOn is running" from this id is
reading a label, not the artifact.

**How it hid.** The id matched on every successful deploy (because every successful deploy also bumped it), and
the capability floors are string compares that pass on any later label. Nobody asked what the id is when two
different sources carry it.

**Probes.**
- A build identity used as proof must be DERIVED from the artifact (source hash over every compiled release file
  + the compiled assembly's MVID), not typed.
- Verification binds to a per-connection record (accept sequence after the old process was observed gone) and to
  the sending process's identity (PID + StartTime), never to a cached "last received" value.
- A test that two constants are equal proves the constants are equal. Say that in the test's name.
- Canon text about runtime behaviour ("AddOns do NOT hot-reload") is re-checked against the logs before a design
  rests on it — F5 does reload in-process (09-22 research_facts h1→p1 inside one NT8 process).

**Fix pattern.** Additive hello fields (process identity, MVID, source hash, activation nonce) + a Go
per-connection record + a verifier that refuses cached or wrong-epoch evidence (M2/M4 of W-ONE-BUTTON, subject
to owner ruling on "no new protocol work").

## CLASS 162 — an optional field a newer producer legitimately leaves nil, dereferenced by an older reader

**Found:** 2026-09-23, live after the M2 boot: `🔭 desk strip: line 10 (planner) panicked and was contained: nil pointer`, on every scan. Present before the boot too.
**Shape.**
- W-GEOMETRY-REFUSAL (b1) added reference-anchor level ids (`ref|…`, ONH/ONL/VWAP…). Those levels have **no formation close by construction**: `FormedCloseMs == nil`.
- The desk strip's planner line was written before that. It rendered every resolved level with `*r.Level.FormedCloseMs`.
- The containment (`deskSafe`) kept the loop alive but turned the line into UNKNOWN, for as long as any plan named a reference level.

**Probe:** for every pointer field a producer documents as optional, grep its readers for a bare `*x.Field`. Drive the reader at its production entry with the nil case; the fixture must use the producer's real nil-case shape. Fixed in M2.1: the line prints `formed_close_ms=n/a` (L7).

## CLASS 163 — a refusal that returns the same value as a send

**Found:** 2026-09-23 in W-EXEC-TRUTH W0 (the read-only map, proven by an overlay test and by live rows) [A].
**Shape.** `placeOneStopEntry` returned a bool meaning only "the hold refused this". A guard cancel ("never placed"), an un-adjudicated verdict, a refused slot, an AddOn too old to build the order and a real send all returned the SAME value, and the caller read it as "sent": it latched `placedThisPass` and cancelled every other arm of the plan `one_live_entry: <S> placed`. Live: rows 169 and 176 were cancelled that way 0.3 ms after the "placed" row itself was cancelled "never placed" (2026-09-21 00:30:59, 2026-09-22 05:50:10 UTC).
**Probe:** for every function whose return value decides whether a SEND happened, list its non-send outcomes and check each is distinguishable from a send at the caller. A two-valued return with more than two outcomes is the smell. Fixed in W0a: an explicit outcome (NOT_SENT / HELD / COMMITTED; a failure after the ledger stamp is COMMITTED, class 81). Pinned: `TestUnsentStopEntryNeverCancelsTheSiblingArm`, `TestPlaceOneStopEntryOutcomeFollowsTheLedgerStamp`.

## CLASS 164 — a safety leg fed a value compared BEFORE it was canonicalized

**Found:** 2026-09-23 in W-EXEC-TRUTH W0 [A], by probe at the production writers.
**Shape.** Three safety checks were dead for one reason: the value was compared raw.
- EntryGate leg 7 (one open position) is fed by builders that filtered `p.Side == "long" || p.Side == "short"` while every writer stores `"LONG"`/`"SHORT"`: leg 7 never saw a position (the builder even lower-cased the value — AFTER the comparison).
- The `executeOpen*` same-side guards compared NT8's `"LONG"` against `"long"`.
- `ntHeldPosition` required `positionAmt > 0`, but a short is signed negative on NT8 and on every crypto broker: a held SHORT read as flat, so the AI's pre-open reconcile let an entry net onto it.
**Probe:** for every comparison against a literal side/state/symbol, find where the compared value ENTERS and check it passes through the one canonicalizer before any comparison (canon 28). A test fixture that writes the value in the reader's casing hides it — drive the check with a row the PRODUCTION writer wrote. Fixed in W0a: `positionSide` / `brokerPositionSide` at every entry point. Pinned: `TestNtHeldPositionSeesBothSidesOnTheWire`, `TestDecisionLegSevenSeesAStoredPosition`, `TestArmLegSevenSeesAStoredPosition`, `TestOrderPathsReadBrokerSidesCanonically`.

**Instance 2026-09-24 M3:** M2. The logout blacklist is an exact-string map, while jwt v5's lenient base64url accepted 3 other spellings of a 43-char HS256 signature, so a logged-out token was live again, /api/updates included (red-1 #4). `WithStrictDecoding` leaves one accepted spelling per token; pinned by `TestLoggedOutTokenStaysRevokedUnderASignatureRespelling`, `TestValidateJWTRefusesNonCanonicalSignatureSpellings` (`62579330`)

## CLASS 165 — four doors to the broker, no lock between them

**Found:** 2026-09-23 in W-EXEC-TRUTH W0 (dispatch D10, confirmed by probe: three signal frames from three paths back to back on one TCPTrader with a position open) [A].
**Shape.** The AI decision, the armed path, Picture HTF and the side doors (agent chat, the debug test trade, the test-arm seam) each checked only their OWN evidence before sending, and their B3 dedupe keys never collide across paths. Nothing serialized the four entry functions, so two producers could each send an entry for one account and instrument inside the window before either fill was visible to the other.
**Probe:** list every function that puts an ENTRY on the wire and every caller of each. If two callers read different evidence and no lock spans the send, sequence them back to back over a real in-process connection and count the frames. Fixed in W0a: one latch inside the four entry functions, per account|wire-symbol, after the maintenance permit and before B3, fed by the same book and ledger definitions the armed path uses; the 🚦 boot line READS whether it is wired. Pinned: `TestEntryLatchRefusesAllFourEntryFunctionsOnEachClause`, `TestEntryLatchQueuedThenRecentThenOpen`, `TestEntryLatchSpansTradersOnOneAccount`, `TestEntryLatchRefusalDoesNotConsumeTheDedupeSlot`, `TestEntryLatchLedgersListPlacedRowsOnTheAccount`, `TestNewAutoTraderCallsWireNT8EntryLatchUnconditionally`.


## CLASS 166 — three entry paths, one set of rules on paper, three in code (a gate with a per-path copy / a producer that skips the chain)

**Found:** 2026-09-23 in W-EXEC-TRUTH W0 (dispatch D1, D8, D11, D26; the W0 six-lens map at base `855309b7`) [A].
**Shape.** The AI decision ran an ordered chain of entry gates inline in `executeDecisionWithRecord`; the armed path re-ran a partial copy at authoring and NONE at placement (G1: a leg the authoring EntryGate refused on a daily force-flat was placed in the same pass because it was already `armed`); Picture HTF called only the maintenance hold, its own positions read and its own pending rows, ran with the trader stopped and the Day Plan master off, and read its own R:R knob with no strategy floor; the agent chat's OpenLong/OpenShort ran none. Each copy was right about itself — every per-path test passed — and the set of rules was different on each path. A gate added to one copy was absent from the others, and nothing compared them.
**Probe:** enumerate every producer that can put an ENTRY on the wire (not the send functions — the callers that DECIDE to send) and, for each gate, ask which producers run it. A gate reached by a copy on each path is a finding even when every copy is correct today. Then drive each producer at its production call site with each gate tripped (a matrix, one row per gate per path) and remove each gate in turn: a gate whose removal turns no row red is not enforced anywhere the tests can see.
**Fixed in W0b:** one chain, `admitEntry` (`trader/entry_admission.go`), in A's pinned order, called by every producer — A (decision), B (armed, at placement, behind G1's per-pass admitted set, fail-closed on nil), C (Picture, pre-claim and again pre-send), the agent door (`AdmitManualEntryAt`). Pinned: the gate-parity matrix (`TestGateParityMatrix`, `trader/gate_parity_matrix_test.go`: 95 `<gate>@<path>` rows over the five producer entry points, RED per gate removed — `scripts/w0b_gate_parity_mutations.py`, report `docs/superpowers/reports/2026-09-23-w0b-gate-parity-red-proof.md`); the five D10 sequences over one real TCPTrader (`TestDupS1ArmedWorkingThenPictureIsRefused`, `TestDupS2PictureSentThenArmedIsRefused`, `TestDupS3ArmedWorkingThenAIOpenIsRefusedNeverFlattened`, `TestDupS4PictureWorkingThenAIOpenIsRefusedNeverFlattened`, `TestDupS5AISentThenArmedIsRefused`, `TestDupS5AIAndArmedRaceYieldOneEntry`, `TestDupS5PictureAndArmedRaceYieldOneEntry`, + `S1r`/`S2r` across a restart); `TestArmRefusedAtAuthoringIsNotPlacedThatPass`, `TestArmPlacementWithNoAuthoringPassPlacesNothing`, `TestPictureRefusedWhenStoppedOrDayPlanOff`, `TestPictureRRFloorIsTheStricterOfKnobAndStrategy`, `TestChatEntryIsRefusedUnderStrictLikeADecision`, `TestChatEntryWithNoStopIsRefused` (was `TestChatEntryIsAdmittedInAdvisoryModeLegs5And6Abstain`, which pinned "the REAL chain ADMITS a chat entry in advisory mode … legs 5/6 ABSTAIN" as W0b's contract; flipped in W1b E9 by CTO ruling — a chat/agent-door entry with no explicit stop is now REFUSED, see the W1b class "a fail-open leg on a path that never supplies its input" below), `TestManualEntryDoorErrorsSayWhatHappened`, `TestChatEntryErrorsSayWhatHappened`, `TestW0bGuideAndGateLabelsMatchTheBinary`. Since W1b E9 the agent door is `OpenManualEntryAt` → `AdmitManualEntryBracketAt` (the chain with the entry's own Stop/Target); `AdmitManualEntryAt` remains as the bracket-less wrapper used by trader tests only.

**Instance 2026-09-24 M3:** the machine-token deny lived in authMiddleware, which the PUBLIC `/api/reset-password` never runs, so a bot token met only the route's generic 410. `denyMachineBearer` now wraps the route itself, and the pin `TestEveryRuledMachineDeniedRouteRefusesMachineTokens` is written from the ruling, not read from `machineDeniedRoutes` (`f58088bb`)

## CLASS 167 — a refusal deduped on text that carries a moving value

**Found:** 2026-09-23, CTO pre-review #1 of W0b (M3) [A].
**Shape.** "Log and count once per change" was keyed on the refusal's REASON string. The re-entry cooldown's reason embeds the live price and its distance from the stop; EntryGate's R:R leg the execution price and the ratio; the breaker its loss count. So one unchanged refusal was a "change" on every tick the price moved, and was logged and counted again each time (RED: 4 counts for one cooldown refusal over four price ticks). Canon 35 (counters record events) was broken by the dedupe meant to uphold it.
**Probe:** for every `changed(key, value)` / last-seen dedupe, read what `value` is built from. If it is a formatted message, list every `%v`/`%.2f` in every message that can reach it — a number that moves while the condition stands makes the dedupe a no-op. Dedupe on the CLASS (the gate-block name), refined only by a stable sub-class; keep the reason in the log line.
**Fixed in W0b:** `admitDedupeClass` — the gate-block class; for `entry_gate`, the leg `armRefusalClass` reads (the same `entry_gate:<leg>` string the arm-refusal counter family is keyed on). Pinned: `TestArmRefusalWithAMovingReasonIsCountedOnce`, `TestEntryGateDedupeClassIsTheLeg`.

## CLASS 168 — a source guard that recognises one spelling of the predicate it forbids

**Found:** 2026-09-23, CTO pre-review #2 of W0b (M5) [A].
**Shape.** `TestArmStateNoRetypedLists` flags a re-typed arm-state set written as an `||` of two `State*` names. W0b's `reconcile_owned.go` wrote the Picture "send started" predicate that way and was caught — but W0a's `entry_latch_wiring.go` had written the SAME predicate as `!= … && !(… && …)` and passed the guard for a whole merged wave. Two copies of one predicate, one visible to the guard and one not; the guard's green on the second was read as "no copy exists".
**Probe:** when a guard forbids a pattern, write the forbidden thing three ways (the positive `||` form, the negated `!=`/`&&` form, a `switch`) and run the guard on each. When a guard fires, grep the tree for the same predicate in its OTHER spellings before fixing only the hit. Fix by giving the predicate ONE owner beside its classifier and calling it from every reader.
**Fixed in W0b:** `store.PictureSendStarted` (beside `IsTerminalArmState`), called from both readers. Pinned: `TestPictureSendStarted`; the negated-form copy is gone from `entry_latch_wiring.go`.

**Instance 2026-09-24 M3:** the checklist numbering guard recognised only two placeholder spellings ("(assigned at merge)", "(pending)"), so the M3 drafts' `M3-07`-style class headings passed it silently. It is now an allow pattern, `^## CLASS \d+ — `, pinned RED-first by `TestChecklistNumberingRefusesEveryNonNumberedClassHeading` (`0cd6df49`)

## CLASS 169 — a test that returns while the goroutine it launched still reads the seam its cleanup resets

**Found:** 2026-09-23, the CTO's full `-race` run of W0b at `41ac268e` (M4; pre-existing on dev, flaky — 0 of 40 local `-race` iterations reproduced it) [A].
**Shape.** `maybeRunSessionReadsAt` / `maybeRereadAfterDeath` / `maybeRereadAfterFlip` launch the planner read on a goroutine. `TestFlipRereadDeathConditionUnchanged` triggered the death re-read and returned; its deferred `market.FuturesBarsProvider = nil` ran while the goroutine was still inside `kernel.FeedClockDriftMs` reading the provider. The race detector saw it only when the scheduler interleaved that way, so the suite was green most runs and red on some — on whichever PR happened to be running.
**Probe:** for every test that calls a function which spawns a goroutine, find the package-level seams the test (or its helpers' `t.Cleanup`s) resets, and ask what joins the goroutine before the reset. Remember the order: defers run before any `t.Cleanup`, both after the test body — a join registered with `t.Cleanup` does NOT run before a `defer`'s reset. An in-flight marker stored before the `go` and deleted as the goroutine's last deferred act is an exact join; one claimed inside the goroutine is exact only after the test has observed the goroutine's work.
**Fixed in W0b (test-only):** `drainReReads(t)` waits, bounded, until the flip/death/planner in-flight maps are empty; registered as a `defer` right after the first trigger in 32 trigger tests (10 files), inline where a test moves a seam mid-body. Not audited: the wake-level, transition and weekly reads' own goroutines.

## CLASS 170 — a status view that finds its rows by text a later writer overwrites

**Found:** 2026-09-23, W0b's own (f) test suite (the withdraw builder's defect probes, confirmed 5/5 by its verifier) [A].
**Shape.** The withdraw marked each resting entry it asked NinjaTrader to cancel with `state_reason = "withdraw: maintenance job <id>"`, and `GET /api/maintenance` found the job's rows by that prefix. `state_reason` is a lifecycle field, and three writers replace it as a cancel progresses: the re-request, the received order_update, and the snapshot confirm. So a row left the view the moment its cancel moved on, and the view read "nothing pending" or "nothing confirmed" while the rows sat in the ledger. The same view also matched the job id as a bare prefix (`job1` matched `job10`). It reported `[]` for a ledger it never read, and listed a withdrawn entry that FILLED as "confirmed".
**Probe:** for every status surface that selects rows by a field value, list every writer of that field (grep the column name in the store, not the caller). If any writer can run after the marker is written, the selection is lossy. Either the marker lives where nothing overwrites it, or the store owns a rule that keeps it. Then drive the row through each writer at its production call site and assert the view still lists it. A prefix match on free text also needs a terminator.
**Fixed in W0b:** `store.reasonKeepingWithdraw`. Every lifecycle writer (`SetState`, `ApplyPlacementReceipt`, `RequestCancel`, `ConfirmCancel`) appends to a withdrawn row's reason after `' ‖ '`, never replacing the head. `store.ListWithdrawn` matches the exact head or the head plus the separator. The view lists pending / confirmed (`IsCancelledArmState`) / filled / ended and reports unread lists as absent. Pinned: `TestWithdrawHeadSurvivesEveryLifecycleWrite`, `TestListWithdrawnMatchesTheExactHeadNotAPrefix`, `TestMaintenanceWithdrawViewKeepsARowConfirmedByOrderUpdate`, `TestMaintenanceWithdrawViewKeepsARowThroughAReRequest`, `TestMaintenanceWithdrawViewDoesNotFabricateEmptyListsItNeverRead`, `TestMaintenanceWithdrawViewListsAFillAsFilledNeverConfirmed`.

## CLASS 171 — a wire fixture that dials and returns before the server registered the client; the producer races the accept

**Found:** 2026-09-23, CI "Backend Tests" on PR #186 (job 107165325139, 2-core runner), CTO M7 [A].
**Shape.** `net.Dial` returns when the TCP handshake completes. The in-process `TCPServer` registers the client later, on its accept loop (`acceptLoop` sets `s.conn` under `connMu`). Every immediate send (close_position, cancel_order, move_stop) reads `s.conn` and fails with "no NT client connected" until then. `newReconcileWire` dialed and returned, and the test then drove `reconcileBeforeOpenNT`. On CI the flatten was attempted before the "client connected" log line; `closeSent` waited 3 s for a frame that was never sent, and `TestReconcileStillFlattensAnUnexplainedShort` failed. Every developer box won the race, so nothing local ever showed it. 12 fixture sites in `trader/` had the shape, or had a barrier that fell through silently on timeout (a `FarSideProven` loop with no `t.Fatal`).
**Probe:** for every test fixture that dials an in-process server, find what it waits on before returning. "The dial returned" and "a reader goroutine started" are not registration. The barrier must read the SAME state the send path reads (`IsConnected()` ⇔ `s.conn != nil`), be bounded, and fail closed. Then check whether a fast machine is hiding the race: run the test at `-cpu 1` or on a 2-core runner.
**Fixed in W0b (test-only):** `waitAddonRegistered(t, s)` (`trader/wire_fixture_test.go`) — a bounded poll of `IsConnected()` with `t.Fatalf`, called right after the dial's error check at all 12 sites. `TestWireFixturesWaitForTheServerToRegisterTheClient` is a source guard: every `net.Dial` in the package's tests must be followed within 8 lines by `waitAddonRegistered`, or be allowlisted with a reason (two raw dials in `maintenance_drop_test.go` that assert on what does NOT arrive). It was RED on the 12 sites before the fix.

## CLASS 176 — a third-party market-data call on the trade path, keyed on the symbol instead of the venue

**Found:** 2026-09-23, W-NO-BINANCE Part A (dispatch §0; the read-only map `wf_862bacd5-080` and its completeness critic, G1) [A].
**Shape.** `market.GetWithExchange` called Binance's crypto-perp open-interest and funding endpoints for every symbol, MNQ included, on every AI open, close, hold-lock check and admission live-price read. That is a request that cannot succeed for a CME symbol, with a 30 s client timeout, inside an entry decision. The later futures branch was chosen by `IsCMEFuturesSymbol(symbol)` alone, so a NinjaTrader trader handed a non-CME symbol still fell through to the crypto branch: CoinAnk maps the unknown venue to `exchange=Binance`, and fapi OI/funding followed. The venue that defines the path was never consulted.
**Probe:** for every function on the trade path that performs I/O, list the hosts it can reach and the predicate that selects them. A predicate on the symbol alone, where the venue decides which data exists, is a finding. So is any outbound call inside an entry decision whose answer the decision does not use.
**Fixed in W-NO-BINANCE A:** `marketRouteFor(symbol, venue)` is the ONE decision: futures → NT8 bars with OI/funding absent (`futuresOIFunding`); the NinjaTrader venue with a non-CME symbol → REFUSED; everything else → crypto. The 📊 boot line READS the same route over the loaded traders' symbols. Pinned: `TestFuturesMarketReadMakesNoBinanceCall`, `TestAIOpenSendHalfMakesNoBinanceCall`, `TestAdmissionLiveReadMakesNoBinanceCall`, `TestNT8VenueRefusesANonCMESymbolWithNoOutboundCall` (a trap on hook.SET_HTTP_CLIENT AND http.DefaultTransport); the source guard `TestNoBinanceHostOnTheFuturesPathOutsideTheAllowlist` (every Binance host spelling) plus `TestBinanceFetchersAreCalledOnlyFromTheCryptoBranch`.

## CLASS 177 — a numeric field that cannot say "absent", so the code writes a fabricated 0

**Found:** 2026-09-23, W-NO-BINANCE Part A (R2; CTO F1) [A].
**Shape.** `market.Data.FundingRate` is a plain `float64`. With no way to say "not read", every writer that had no value wrote `0`. `GetWithTimeframes` wrote `OIData{0,0}` and `0` on futures by design; `GetWithExchange` substituted `{0,0}` on an OI error and discarded the funding error. So the AI prompt could read `Open Interest: Latest: 0.00 Average: 0.00` or `Funding Rate: 0.00e+00`: a confident zero standing in for "we have no data", which reads as a real market fact. The funding cache also stored a parse-failure 0 for an hour.
**Probe:** for every numeric field that feeds a prompt, a gate or a display, ask what the writer does when the source is unavailable. If the answer is "0", check whether a reader can tell that 0 from a real zero. If it cannot, the field needs presence (a pointer, or a `Known` flag), and the renderer must print n/a.
**Fixed in W-NO-BINANCE A:** `OpenInterest` nil = absent on futures; additive `Data.FundingRateKnown`; the prompt renders `n/a` for both (futures) and for an unread funding (crypto). Pinned: `TestFuturesPromptRendersAbsentOIAsNA`, `TestCryptoPromptRendersFundingOnlyWhenKnown`, `TestFuturesFetchThenRenderSaysOINA`. **Not fixed (Part B / crypto leftovers):** the crypto branch's `{0,0}` on an OI error, the `oi*0.999` "average", `market.Format`, and the agent and grid funding renderers.

## CLASS 178 — a precedence stated in prose, implemented by the append order of a first-match scan

**Found:** 2026-09-23, W-EXEC-TRUTH master dispatch D16, re-read at dev `656b7418` for W1 (c) [A].
**Shape.** `kernel.ConditionStatus` resolved the env step by scanning one composed string and returning the FIRST token that named the condition. `ShadowConditionsEnv` built that string with every `SHADOW_CONDITIONS` token first and every `LIVE_CONDITIONS` token after, so a condition named in both lists resolved SHADOW. The comment one line above the builder promised the opposite ("LIVE_CONDITIONS forces live, highest env priority"). The rule lived in a comment; the behaviour lived in an `append` order two functions away, and nothing compared them. Under a collision a SHADOW result means an arm is cancelled and never placed.
**Probe:** for every resolver that documents a precedence, find where the order is actually decided. If it is decided by the order values were concatenated, sorted or appended rather than by an explicit rank, write the collision test: put one name at two levels and assert the documented winner, with the inputs built by the production composer (for env, `t.Setenv` plus the real builder), not a hand-written string.
**Fixed in W1:** the env step records whether it saw a live and a shadow entry for the name, and live wins wherever each appears; the builder now emits the LIVE block first so the string also reads in precedence order. Pinned: `TestConditionEnvCollisionResolvesLive` (the resolver and the boot ledger), `TestConditionPrecedenceLadder` (all five levels), `TestPlannerPromptArmableLineHonoursLiveOverShadow`, and at the trader seams `TestEnvCollisionLetsTheConditionArm` (arm seam) and `TestEntryGateArmSeamAdmitsAConditionLiveOverShadow`; a strategy override still outranks both env lists (`TestStrategyShadowOutranksLiveConditionsAtEntryGate`). The first two trader tests FAIL on the base resolver [A].

## CLASS 179 — a window named by a duration but measured by a bar count (or by the previous bar)

**Found:** 2026-09-23, W-NO-BINANCE Part B's ticker re-point (the agent builder's concern, [A] at `market/data.go:177-194`), ruled into W1 by the CTO (`1790171256933-12445-000001`).
**Shape.** `market.Data.PriceChange1h` / `PriceChange4h` carried the names "1h" and "4h", but each writer counted bars. `GetWithExchange` took the close 20 bars back: 60 minutes on the CoinAnk 3m series, 100 minutes on the NT8 and Hyperliquid 5m series. Its "4h" was the previous bar of the LONG series: the prior 1h close on futures (0–60 min), the prior 4h close on crypto. `GetWithTimeframes` divided the window by the timeframe, so a 1h primary's "1h" was the previous close and a 4h primary's "1h" was a 4h change. Every writer returned 0 when the series ran short, and a reader could not tell that 0 from a flat market. The field name promised a duration; nothing measured one.
**Probe:** for every value named after a duration or a window, find how its reference point is chosen. A bar index (`len-N`, `len-2`) is a finding unless the bar's timeframe is fixed and asserted beside it. Then check the short-series branch: it must return absent, not 0. Then check resolution: a series whose bar is as long as the window can only report "the previous bar".
**Fixed in W1:** `market.ChangeOverWindow(bars, window) *float64` measures by bar close time and returns nil when the series does not reach back. `changeOnTimeframe` adds the resolution guard (at least 4 bars must fit in the window). The `Data` fields are `*float64`, and `market.PctOrNA` renders n/a at every reader: the BTC line, the grid prompt (zh/en) and the agent context. The futures values were never rendered, so no MNQ prompt moved. Pinned: `TestChangeOverWindowMeasuresBarTime`, `TestChangeOverWindowAcrossAGap`, `TestResolutionGuardRefusesAWindowTheSeriesCannotResolve`, `TestFuturesReadMeasuresBothWindowsByTime`, `TestFuturesReadWithAShortTapeReportsTheChangesAbsent`, `TestTimeframesReadMeasuresOnThePrimarySeries`, `TestUserPromptBTCLineRendersAnUnmeasurableWindowAsNA` and `TestGridPromptRendersAnUnmeasurableWindowAsNA` (goldens `user_prompt_crypto_change_na.txt`, `grid_user_en_change.txt`). RED: 5 of 5 mutations were CAUGHT (the 20-bar lookback, the previous long bar, the guard removed, absent rendered as 0, and no reference returned as 0).

## CLASS 180 — a drift counter that skips the fields a custom MarshalJSON re-emits

**Found:** 2026-09-23, W-EXEC-TRUTH W1 (f) (CTO master dispatch §2; read-only map at `f179a1bc`, re-read at `853981d2`) [A].
**Shape.** `store.EnumerateSchemaKnobs` built the settings schema by walking Go struct tags and skipped every `json:"-"` field. `StrategyConfig` has five of those (`CoinSource`, `Indicators`, `CustomPrompt`, `RiskControl`, `PromptSections`), and its custom `MarshalJSON` writes all five back out under `ai_config`. So the fields most likely to change behaviour (every risk control, indicator, coin-source and prompt-section setting) were never counted. The ⚙ boot line printed `schema=75` against the 167 paths a save actually writes. `UNCLASSIFIED` could not warn about a new risk field, because the walk never reached one. The audit list checked against the registry named `risk_control.*` and `indicator_config.external_data_sources`. Neither was in the walk, and the second path exists nowhere. They resolved only through the leaf-name fallback in `LookupKnob`. Separately, `env-shadows=0` came from a counter with no writer.
**Probe:** for every counter or inventory built by reflecting over a type, ask whether that type (or any type it contains) has a custom `MarshalJSON`/`UnmarshalJSON`, or `json:"-"` fields. If it does, the tags are not the schema, and the count must come from the bytes the production marshaller writes. Then check each classification that rests on a name fallback: does the same leaf mean something else under a different parent? And for every number on a boot line, find its writer. If nothing writes it, it prints n/a.
**Fixed in W1 (f):** the enumeration fills in every field by reflection, runs `json.Marshal` through the production `MarshalJSON` once for `ai_trading` and once for `grid_trading`, and walks the union of key paths (maps end at a sentinel key; arrays read element 0; memoized). The real paths are `ai_config.*`. `LookupKnob` tries exact, then exact with `ai_config.` stripped, then the leaf (149 of 167 paths still classify by leaf; that is a separate wave). Seven exact rows mark `ai_config.indicators.external_data_sources.*` ineffective, which stops them borrowing the live `name`/`type` rows and the `FetchExternalData` cites (that function has no production caller). The audit list now uses real paths. `env-shadows` prints `n/a (not counted)`, and `/api/config/resolved` omits the key. Pinned: `TestAuditDeadKnobsAreInTheSchemaWalk`, `TestEveryEnumeratedPathIsInTheMarshalledBytes`, `TestEveryMarshalledKeyIsEnumerated`, `TestEveryDashFieldLandsInsideTheEnumeration`, `TestSchemaKeySetSurvivesTheUnmarshalMarshalRoundTrip` (a legacy flat config included), `TestExternalDataSourcesChildrenClassifyIneffective`, `TestLookupKnobStripsTheAIConfigEnvelopeBeforeTheLeaf`, `TestKnobBootLineIsCounted` (schema= read, not literal) and `TestConfigResolvedSchemaIsTheMarshalWalkAndEnvShadowsAbsent`. RED: reverting to the tag walk is CAUGHT by five store tests and one api test; dropping the grid branch, the `ai_config.` strip, the seven exact rows or the n/a are each CAUGHT.

## CLASS 181 — a clamped copy stands in for the saved value

**Found:** 2026-09-23, W1 (g) builder, while building `GET /api/strategies/:id/effective` [A].
**Shape.** Some surfaces run a normaliser (`StrategyConfig.ClampLimits`) on the config and then serve or persist the result. That erases the difference between "you set 3" and "default 3". The strategy GET's `attachPublishConfig` (`api/strategy.go:41`) serves a clamped copy. The create path (`:205`) and the update path (`:339`) persist the clamped config, so an unset min R:R is written as 3, min_confidence 0 as 60 and max_positions 0 as 1. Each of those then reads back as a "saved value". The running engine clamps its live config pointer in place every cycle (`kernel/engine_analysis.go:252-253`), so anything reading that pointer sees the clamp as well.
**Probe:** store an absent value, a 0 and an explicit value, then read them back through every surface (API GET, editor, effective endpoint, boot line). The three origins must differ. Any normaliser that runs before a read or a write that reports origin is a finding.
**Fixed in W1 (partly):** the effective endpoint reads the STORED row (raw JSON for presence + `ParseConfig`) and never the clamped copy or the engine pointer. `TestEffectiveMinRROriginsAbsentZeroSaved` pins this; a mutation that reads the clamped config makes it FAIL. **Not fixed — OWED to a save-semantics wave (deferred by the CTO, ruling msg 1790176346377 R4):** the Studio save path still persists the ClampLimits'd config (unset min R:R saved as 3, min_confidence 0 as 60, max_positions 0 as 1). `TestEffectiveClampAtSaveErasesOriginPin` pins today's behaviour, and must be updated deliberately when the save path is fixed.

## CLASS 182 — a UI fallback literal names a default the runtime does not use

**Found:** 2026-09-23, W1 (g) builder [A].
**Shape.** Editors render `value ?? <literal>` for an unset field. The literal is a guess about the runtime default, written where the runtime can never correct it. Examples: `RiskControlEditor.tsx` renders "≤ {max_contracts_per_order ?? 10}", but the runtime resolves an unset value to 2 (`kernel.ResolveMaxContracts`) and then to 1 (the Stage-A cap). The breaker toggle showed an absent value as OFF while the runtime enforced 8. The min-confidence hint states the default "60" as a literal.
**Probe:** grep the editors for `?? <number>` / `|| <number>` and literal "default N" strings. For each one, compare it with the production resolver's result for an unset value. Either they are equal and a test pins it, or the row shows the server's effective value (the W1 chip) instead of a literal.
**Fixed in W1:** the effective chip carries the server-resolved value and origin on the covered rows. Any remaining literals are listed in the W1 PR.

## CLASS 183 — a 0 that means "unset" to one reader and "off" to another

**Found:** 2026-09-23, W-EXEC-TRUTH W1 (a)+(b), `fix/settings-truth` [A]. Sibling of the fabricated-0 class above (a numeric field that cannot say "absent"): there the WRITER invents a 0; here the stored 0 is real but two readers disagree on what it means, and the encoding makes the disagreement invisible.
**Shape.** `risk_control.consecutive_loss_halt` was an `int` with `omitempty`. The struct comment, `web/src/types/strategy.ts` and the Studio row all said "0 = OFF"; the runtime (`breakerHaltN`) read 0 as "unset → env `BREAKER_HALT_N` else 8". The Studio row rendered an ABSENT breaker as OFF while the runtime enforced 8, its OFF wrote a 0 that no save could store (every writer re-marshals the struct and omitempty drops it), and its ON wrote a literal 2. `day_plan.replan_cap` had the same shape at the strategy level: the session override (`*int`) honoured 0, the strategy level read 0 as the default 2. Nothing compared the three descriptions to the one resolver, and the unstorable 0 meant no test of the save path could ever observe the lie.
**Probe:** for every numeric knob, list every reader of a stored 0 — the resolver, the UI's enabled/checked expression, the struct comment, the guide. If any two disagree ("unset" vs "off" vs "none"), the knob needs presence (`*int`: nil inherits, &0 is 0) and ONE resolver. Then walk the WRITE path through the real handler and read the stored bytes back: an `omitempty` int cannot store 0, so "0 = off" there is a switch wired to nothing. And for any change of meaning of stored data, report every stored row's before/after effective value before the new binary acts on it.
**Fixed in W1:** both knobs are `*int` under the same JSON keys; `store.ResolveBreakerHalt` (saved incl. 0 → env `BREAKER_HALT_N`, an invalid env named in the source → 8) and `store.ResolveReplanCap` (session → strategy → 2) are the only rules, and `breakerHaltN` / `ReplanCapFor` delegate; `store.OriginLetter` is the one [O]/[E]/[I] mapping for the boot lines. The Studio writes OFF = 0, inherit = JSON null (the PUT merge keeps absent keys, so only null clears a 0), clear = null. The conversion is fail-closed with NO boot write: `store.SettingsTruthReport` prints every stored strategy's stored/before/after at boot, and a changed row no Studio save confirmed (`system_config settings_truth_zero:<id>`, written by POST/PUT `/api/strategies` in the SAME transaction as the row, carrying `saved_at` — see the class below) refuses its trader at `addTraderFromStore`, naming the strategy id, the exact field and both meanings (CTO ruling 1790173735176). The 09-16 research copy holds no explicit zero (9 rows) — expected live change 0 [A for the copy, B for live]. Pinned: `TestResolveBreakerHaltPrecedence`, `TestResolveReplanCapPrecedence`, `TestOriginLetterIsOneMapping`, `TestSettingsTruthReportGolden` + `TestSettingsTruthBootReportReadsTheStore` (one golden, the pure report and the store seat), `TestSettingsTruthResearchRowsChangeNothing`, `TestSettingsTruthRefusesOnlyUnconfirmedChanges`, `TestUpdateWithExplicitZerosReplacesTheRecordPerSave`, `TestDuplicateCarriesTheExplicitZeroRecord`; trader `TestW1BreakerBothPathsPresenceAware` (decision AND arm path on real closes), `TestW1SessionRiskBootLineReadsTheBreaker`, `TestW1StrategyReplanCapZeroIsZeroEverywhere`; api `TestW1BreakerSaveReloadThroughHandlers`, `TestW1ReplanCapSaveReloadThroughHandlers`, `TestW1LegacyZeroRefusesTheTraderUntilAStudioSave`; vitest `RiskControlEditor.test.tsx` / `DayPlanEditor.test.tsx` "W1" cases. RED: 14 of 14 Go mutations and 7 of 7 UI mutations were CAUGHT (the pre-W1 `> 0` rule at each resolver, an unnamed invalid env, env tagged [I], the tape clause on every N, the default printed as the breaker with ≠1 bound, no load refusal, no record at create / update, Duplicate dropping the record, the report's BEFORE using the new rule, the confirmation ignored, the legacy flat key missed, the boot line ignoring session overrides; in the UI: absent read OFF, ON writing 2, ON writing undefined, clear writing 0, a fake 2 in the replan box, a cleared replan box ignored, a session override seeded with a literal 2).

## CLASS 184 — a record that vouches for a row is written outside the row's transaction

**Found:** 2026-09-23, CTO review of W-EXEC-TRUTH W1 `fix/settings-truth` (ruling msg 1790176346377) [A]. Sibling of the 0-meaning class above: its fix introduced the record this class is about.
**Shape.** W1's load check trusts a stored explicit 0 only when a second row — `system_config settings_truth_zero:<strategy id>` — says a Studio save wrote it. The first build wrote that record in a SECOND statement after the strategy row had committed (`api/strategy.go` create and update: `Create`/`Update`, then `RecordExplicitZeros`, whose error was only `Warnf`'d and the save answered 200). A failure between the two left a saved 0 with no record — the owner's own OFF refused at the next load — and the handler told the owner the save had worked. The record also carried no time, so no surface could say WHICH save confirmed a 0; and a record that arrives without its row (or a row without its record — a DB restore, an import, a hand copy) had no defined reading.
**Probe:** for every "marker", "record", "confirmation", "claim" or "stamp" row that another reader uses to trust or refuse a primary row, find its writer. It must be in the SAME transaction as the primary write, and a failed marker must fail the whole save — so the caller sees an error, not a 200. Prove it by making ONLY the marker's write fail inside the database (a trigger, no seam in production code) and asserting the primary row is unchanged. Then read the marker's parser. A value it cannot vouch for (truncated, missing its time, naming an unknown field) must read as UNCONFIRMED, never as confirmed. Every surface that prints the verdict must say which save confirmed it, and when.
**Fixed in W1:** `StrategyStore.CreateWithExplicitZeros` / `UpdateWithExplicitZeros` run the row write and the record upsert/delete inside one `s.db.Transaction`; `Duplicate` reads the source, creates the copy and copies the record verbatim inside one transaction; POST/PUT `/api/strategies` call them and answer 500 when the record fails (the row rolls back); `RecordExplicitZeros` is gone. The record is `{"fields":[…],"saved_at":"<RFC3339 UTC>"}`; the first W1 shape (bare comma list) still confirms with no time (`n/a — the record predates saved_at`); anything else confirms nothing. `store.ExplicitZeroVerdict` is the one phrase — `OFF — confirmed by Studio save <2006-01-02 15:04 CT>` (replan: `0 — …`) or `explicit 0 UNCONFIRMED — re-save in Studio` — printed on the 🩺 boot line and appended to the effective endpoint's `origin`. Pinned: store `TestSaveRollsBackTheRowWhenTheRecordFails`, `TestParseExplicitZeroMarker`, `TestUpdateWithExplicitZerosReplacesTheRecordPerSave`, `TestSettingsTruthRefusesOnlyUnconfirmedChanges` + the report golden (confirmed, UNCONFIRMED, legacy-shape, unparsable and CST rows); api `TestStudioSaveRowAndRecordAreOneTransaction` (PUT and POST through the real router), `TestEffectiveExplicitZeroSaysConfirmedOrUnconfirmed`. RED: 7 of 7 mutations were CAUGHT (9 seat runs): two separate writes in all three writers, in create only, and in Duplicate only; the effective row dropping the verdict; the verdict ignoring the record (store and api); a JSON record without `saved_at` accepted; a UTC clock labelled CT (store and api).

**Instance 2026-09-24 M3:** the update enrollment is two files written by two renames. The half-written pair (a new device.key beside the old admin.json) was a WORKING enrollment of the incumbent, while the Enroll comment said the opposite (red-4 #4 [A]). Closed by the password-binding belt: the pair fails the binding, so it is 403 until an `enroll --replace` completes (`399ab651`, `a8f5db20`)

## CLASS 185 — a stored rule evaluated with an env default

**Found:** 2026-09-23, W-EXEC-TRUTH W2 `fix/confirm-resolver` (master dispatch §2 W2 + amendment A5, extends D19) [A]. Siblings: the W1 "0 that means unset to one reader and off to another" class above (one value, two readers), and CLASS 38 (the prompt offers what the validator refuses). Here the plan STORES the rule and the evaluator reads the env instead.
**Shape.** A scenario's `confirm.rule` is authored, validated against the entry law and stored in the plan row. `EvaluateScenarioConfirm`'s continuation branch (`kernel/plan_confirm.go:146` at f2ac79eb) and `BreakdownContinueState` (`kernel/breakdown_continue.go:151`) never read it. They synthesized `fmt.Sprintf("%dx5m_close", bdConfirmCloses())` at EVALUATION time. Plan row 435 (2026-09-21 LONDON v5, S1 `breakdown_continue` short, confirm `{2x5m_close, 30264, below}`) was therefore rendered, recorded, desked, armed and write-validated on ONE close. Leg 1 was MET at the 05:30 close; the stored rule makes it MET at 05:35. The `time_hold` duration had the same defect with nothing to store it in. Row 452 S2 says "holds above it for 3 minutes of 1m closes", and was judged on `ACCEPT_HOLD_MIN` = 10. Nothing reported the substitution. The arm log even printed the leg literal `touch` for every single arm (`armed_executor.go:597`).
**Probe:** for every stored rule-shaped field (confirm/confirm2 rule, hold duration, death/flip rule), list every reader that decides with it: evaluator, render, recorder/card, desk, arm gate + its log, write-time validator, planner facts. Grep each reader for an env/knob read (`os.Getenv`, a `*Min*()`/`*Closes()` helper) on the same path, and for a rule string built from a number (`Sprintf("%dx…")`). Then check two things. (1) A stored value exists and the env is set to something else. At least one reader must read the env, and a test must show the stored value winning (`t.Setenv` the env to the opposite). (2) Nothing is stored. The default must be NAMED as a default on every surface, never presented as the plan's own rule. Find prose that states a value its object does not store ("3 minutes"). The write site must refuse it, and history must never be re-judged.
**Fixed in W2:** `kernel/confirm_resolver.go` `ResolveConfirm` / `ResolveScenarioConfirm` returns `{rule, kind, closes, tf, hold_min, ref, side, SOURCE, why}`. It is the ONE reader. `BD_MIN_CLOSES` / `ACCEPT_HOLD_MIN` are read only there, plus `EntryLawBootLedger` and the prompt's authoring-default text (the source-scan pin). `BreakdownState.Rule` carries the resolution to the evaluator, `retestLegDetail`, the validator's "NO confirming close" message (now naming `rule 2x5m_close [stored]`), `RenderConfirmLines`, the recorded `scenario_meta.confirm` (new `rule_source`), the desk, the card chip ("(authoring default)" when it is one) and the arm log (`wait_confirm MET (leg 1 2x5m_close [stored] → leg 2 retest_fail)`). The decline consumer now reads the recorder's scenario set and the verdict's own ref. A5 adds `PlanConfirm.hold_min` (`*int`, omitempty — absent on every legacy row, resolved as "authoring default (no hold_min stored)"). It is structurally valid only on `time_hold` with a value > 0. It has a new-authoring-only refusal: the prose states minutes and `hold_min` is absent, or is none of them (`ValidateConfirmHoldProse`, `parsePlanDocument` newAuthoring branch; the stored-read path never runs it). The prompt schema states it, and a contract row, a hint and a repair excerpt were appended. Kept DISTINCT and untouched: touch, displacement (`BD_MIN_DISP_ATR`, write-time), plan death (`PlanIsDeadSince`), flip (`FlipConfirmCloses`). Pinned: `TestConfirmResolverStoredOneCloseIgnoresEnv`, `…StoredTwoCloses`, `…TouchUnchanged`, `…DisplacementUnchangedAcrossCloseRules`, `…FormingBoundary`, `…Row435`, `…Row452HoldMinutes`, `…HoldMinStructural`, `TestProseHoldMinutes`, `…SourceScanPin`, `…ReplayRenderByteIdentical` (RenderConfirmLines over the 09-08 replay corpus, golden written at the base before any code), `TestBreakdownContinueValidatorRealTape` (re-pinned as the correction), trader `TestConfirmResolverBreakdownArmStoredTwoCloses`, and vitest `ConfirmationTruth.test.tsx` W2 cases. RED: 17 of 17 Go mutations and 1 of 1 UI mutation were CAUGHT, after one survivor (a dropped leg-1 `rule_source`) was closed by a stronger pin. The mutations were: stored rule ignored, hold_min ignored, write check off, absent hold_min accepted, dedupe off, drop off, pre-birth kept, forming off by one, arm log literal, leg-1 source dropped, validator message unnamed, displacement measured after leg 1 only, decline set narrowed, env re-read in `retestLegDetail`, `hold_min` gone from the prompt schema, hold_min on any rule, and the timeframe tail not excluded; in the UI, the default label dropped.

## CLASS 186 — a close count that trusts the order of its input

**Found:** 2026-09-23, W-EXEC-TRUTH W2 (d) [A].
**Shape.** The confirmation counters assumed a sorted, unique 1m series and never checked it. There were three consequences. (1) `evaluateConfirmAfter` windowed with `BarsSince`, which returns `bars[i:]` from the first in-window bar, so a late PRE-BIRTH bar after it leaked in. (2) `aggregateToMinutes` merges only ADJACENT same-bucket bars, so a late copy of an earlier bucket's minute re-opened that bucket after a later one. That is a phantom completed close: buckets `[Z, A, Z′]` made run 2 → a `2x5m_close` MET. (3) `time_hold` counted every bar, so a repeated minute counted twice: nine distinct minutes plus one copy read "10/10 MET". The live BarCache keeps bars ascending and unique (`bar_cache.go` Upsert), so this is defence in depth — but nothing proved the input was ordered, and replay/test tapes are not the cache.
**Probe:** feed every counter (a) a duplicate minute, (b) a late copy of an earlier bucket's minute, (c) a late pre-window bar, and assert the count and the bucket open times (strictly increasing). Then check that the rejected input is RECORDED — a counter, not a silent skip.
**Fixed in W2:** `confirmationTape` (`kernel/confirmation_bucket.go`) is the one confirmation input. It keeps `[since, now)` by bar OPEN; a same-OpenTime copy replaces the kept bar (the freshest print); a strictly-older bar is dropped. Its recorded counters appear in the 🔎 confirmation boot line (`tape out-of-order drops=…`, `tape same-minute replacements=…`), with one WARN on the first drop in a process. `evaluateConfirmAfter` and `confirmationBuckets` read it. Plan death/flip windowing is untouched. The forming guard is unchanged (`EvaluateBucketClose`). Pinned: `TestConfirmResolverDuplicateMinuteCountsOnce`, `TestConfirmResolverOutOfOrderNoPhantomBucket`, `TestConfirmResolverFormingBoundary`. RED: dedupe off, drop off and pre-birth kept were each CAUGHT.

## CLASS 187 — a test file silently excluded by a GOOS/GOARCH filename suffix

**Found:** 2026-09-23, W2 builder, before commit [A]. It never shipped, but it cost a build cycle and would have shipped a green suite that never ran the test.
**Shape.** Go treats a `_<GOOS>` or `_<GOARCH>` filename suffix as a build constraint. A test named `confirm_resolver_arm_test.go` ("arm" as in armed orders) is built only on `GOARCH=arm`. On amd64, `go test -run <its test>` prints `testing: warning: no tests to run` and exits 0. `go list -f '{{.IgnoredGoFiles}}'` lists the file; nothing else does.
**Probe:** `go list -f '{{.ImportPath}}: {{join .IgnoredGoFiles " "}}' ./...` must list only deliberate build-tagged files. On 2026-09-23 that is exactly one: `store/sqlitedriver/backend_cgofree.go`, a cgo/driver build tag [A]. Suspicious suffixes in this repo's vocabulary: `_arm`, `_arm64`, `_js`, `_linux`, `_windows`. A test that you just wrote must be seen to RUN (`-v`, its `=== RUN` line) before it counts as green. `ok … [no tests to run]` is a finding.
**Fixed:** the file was renamed `confirm_resolver_armgate_test.go`. A repo scan for every GOOS/GOARCH suffix found no other hits (2026-09-23 [A]). Guarded (CTO ruling 1790181002671): `branding/test_file_build_suffix_test.go` `TestNoTestFileCarriesAPlatformBuildSuffix` walks every `*_test.go` and fails on a `_<GOOS>`/`_<GOARCH>` name suffix unless allowlisted with a reason (empty today); a vacuity floor of 500 files. RED: a planted `kernel/zz_probe_arm_test.go` is CAUGHT.

**Instance 2026-09-24 M3:** `TestCensusWalkIsTestToolingOnly` read `go list .Imports`, which covers the current GOOS/GOARCH only, and `./...` never matches a package every file of which is excluded. `TestNonTestImportersSeesEveryPlatform` asks every platform (`6e7e7117`)

## CLASS 188 — an unparseable safety sentence accepted as UNKNOWN

**Found:** 2026-09-23, W-EXEC-TRUTH W2 A1 (CTO amendment msg 1790166603535; lane decision D1) [A]. Sibling of CLASS 90 (whose closing note recorded "unsupported ... evidence is UNKNOWN and accepted" as the design) and of the class-38 prompt/validator contract.
**Shape.** The write-time born-dead check (`validateAuthoredScenariosAt` → `kernel.EvaluateAuthoredInvalidationAt`) parsed `scenario.invalid` with a small grammar and treated a sentence it could not parse exactly like a missing minute: UNKNOWN, `ACCEPTED for this check`, one `authored_unknown` count. The prompt never stated the grammar (the schema said `"invalid": "<line>"`), so the model had no way to comply. Plans rowid 455 (2026-09-23 LONDON v1) published four scenarios whose invalidation lines were all outside the grammar ("Any 5m close above 31075.75 invalidates the fade; stand aside for the breakout.", …; log_events 92916–92919). No machine check could judge any of them. The verdict struct had no field that told the author's defect (grammar) apart from the feed's (tape), so one number counted both.
**Probe:** for every write-time check that parses model-authored text, list its UNKNOWN outcomes and ask whose fault each one is. If the author could have written a parseable sentence, UNKNOWN must be a refusal inside the existing attempt budget. The refusal quotes the sentence, the prompt states the grammar the parser accepts (a class-38 row plus a parity test that runs the prompt's own example through the parser), and the repair prompt carries the law. An UNKNOWN the author could not have avoided (missing, malformed or duplicated tape) stays accepted and counted. Two different causes must never share one counter.
**Fixed in W2 A1:** `AuthoredInvalidationVerdict.Unknown` = `grammar` | `tape`. Grammar unknowns are refused through ONE combined error that opens with `invalidation grammar refusal` and quotes every offending sentence (`%q`), with one counted `authored_grammar_refusal` event per scenario. Tape unknowns keep `authored_unknown` (accepted). The prompt carries `AuthoredInvalidationGrammarLine()`: the four canonical forms plus ONE placeholder example (`PromptContracts` row). `RepairInvalidationGrammarLaw` sits on its own hint field (`scenario.invalid`, token set {2x5m}) and is routed in `lawExcerptsFor` on the marker. The 🧭 boot line READS `invalidation: <kernel.AuthoredInvalidationPolicy()>` and `grammar refusals=N`, and relabels the old count as tape-only; pre-W2 events mix both kinds, and the line says so. Pinned: kernel `TestBornCheckRow455FourGrammarRefusals`, `TestAuthoredInvalidationUnknownSplitsGrammarFromTape`, `TestAuthoredGrammarPromptExampleParses`, `TestAuthoredGrammarRefusalRoutesRepairLaw`, `TestClass38PromptContractsAllStated`. Trader, at `runPlannerReadCoreWithFactsGradesClock`: `TestW2Row455GrammarRefusedAtWriteSiteThenConformantPublishes`, `TestW2Row452GrammarRefusalAndTapeUnknownAccepted`, `TestPlanLivenessBootAndDeskUseRecordedFacts`. Store: `TestLivenessGrammarRefusalCountedSeparately`. RED: every one of these mutations was CAUGHT — grammar accepted, tape refused, marker routing removed, boot line fed the tape count, event kind not recorded, sentence unquoted, grammar line dropped from the prompt.

## CLASS 189 — a write-time check that judges only the read-time price or the latest window

**Found:** 2026-09-23, W-EXEC-TRUTH W2 A2 + D5 (CTO amendment msg 1790166603535) [A]. Sibling of CLASS 148 (side of price judged against the read price) and CLASS 91 (a close counted before its bucket closed).
**Shape.** A planner read is assembled at one clock and published at another. For plans rowid 455 the read clock was 01:30:27 CT (per the amendment) and the publish clock (row created_at) 01:51:47 CT. Between them four 5m groups closed: 01:35 31081.00, 01:40 31090.75, 01:45 31083.00, 01:50 31079.75 (MNQ 1m, open_time_ms 1790145000000..1790146140000). The scenario born-dead check sampled only the latest completed window at publish. The death/flip side-of-price rule (`DeathLineBeyondPrice` / `FlipLineBeyondPrice`) judged only `facts.Price`, the read-time price 31057.50. So the plan's own `death{2x5m above 31075.75}`, met by 01:35 + 01:40 before the plan existed, was never judged. Neither was `flip{5m_close above 31066.32}`, which fired at 01:35. The runtime evaluator judges only closes after birth, so a condition met in that gap is visible to nobody.
**Probe:** for every write-time validator that judges market state, name the clock it reads and the window it covers. If the model's input was assembled earlier than publication, the validator must cover the whole span (read, publish]: every group, not a sample. It must record the clocks and the judged groups on the row, so a reader can see what was checked. Build the discriminating fixture: a breach that exists ONLY between read and publish must refuse with the read clock and must be accepted by the old check. That is the proof the new window did the refusing.
**Fixed in W2 A2 / D5:** `kernel.PlanFacts.ReadAt` (`json:"-"`) = `input.Now`. `AuthoredBornGroups(read, publish)` returns every group whose close B satisfies read < B ≤ publish, plus the latest completed group. It uses the canonical `EvaluateBucketClose`, and each group needs all five minutes. A group with a gap is tape-UNKNOWN for that group only and is never a refusal. A zero read (legacy facts-less callers) is today's latest-window check, and the record says read n/a. `EvaluatePlanConditionBetween` judges death{} and flip{} on the same groups (raw line, rule, side). A met death joins the born-dead refusal; a fired flip refuses with "re-author on the flipped side". The runtime buffers, windows, touch gate and flip hold are unchanged. The record `{policy, read_clock_ms, publish_clock_ms, groups, verdicts}` lands on NULLable `plans.read_clock_ms / publish_clock_ms / born_check`, and in a refused attempt's liveness-event `born_check`. The card line reads it back (`authored_invalidation`; a pre-W2 row shows n/a). The shadow A/B replays it purely. Pinned: kernel `TestBornGroupsRow455ReadToPublish`, `TestBornCheckA2DiscriminatesReadToPublish` ("5m close above 31085.00" and "2x5m close above 31080.00": refused with the read clock, accepted with zero read), `TestBornCheckTapeUnknownIsPerGroup`, `TestBornCheckConformantPassesAndRecords`. Trader, at `runPlannerReadCoreWithFactsGradesClock`: `TestW2A2RefusesOnlyWithTheReadClock`, `TestW2DeathAndFlipMetBetweenReadAndPublishRefuse`, `TestW2AllAttemptsRefusedFailClosedRowHasNoBornCheck`, `TestW2LiveReadStampsTheReadClock` (the live `runPlannerReadWithTriggerClaimedCtx` path), `TestW2ShadowVerdictIncludesBornCheck`. Store: `TestPlanRowBornCheckColumnsNullable`, `TestPlanBornCheckColumnsMigrateExistingTable`, `TestLivenessEventCarriesBornCheck`. API: `TestAuthoredInvalidationViewReadsTheRow`. Vitest: `PlanLiveness.test.tsx`. RED: every one of these mutations was CAUGHT — the group set ignoring read, the write site passing a zero read, facts not stamped from `input.Now`, death/flip unjudged, bad minutes ignored, the record not stored, the NO-TRADE row inheriting a refused candidate's record, the API inferring a policy for a pre-W2 row, the shadow skipping the check, and three UI mutations.

## CLASS 190 — an identity that names one level while the scenario trades another, published as a "recorded" finding

**Found:** 2026-09-22/23, CTO amendment msg 1790166603535 [A]. Plans rowid 452 (2026-09-22:ASIA v1) S2: `level_id` resolves to PDH 30917.50 while trigger, confirm (`time_hold` ref 31009.75) and economics all trade SWG-H·5m 31009.75; log_events 90563 printed `🪪 heuristic-disagreed … candidate=30917.50 evaluator=31009.75 — recorded; evaluator unchanged` and the plan was PUBLISHED. `resolveEntryGeometryZone` takes the reject/fade entry zone BY level_id, so the stored identity steers execution geometry toward a level 92 points away. The 09-22 London row 443 carried `ref|`-prefixed ids with an altered digest that no map row carries.
**Shape.** A write-time check that was built as recording (class 116: "WARN-only", "a counted finding") and stayed recording after downstream code started to TRUST the recorded field. A published contradiction is two truths on one row; whichever reader you pick decides which one trades.
**Law.** At NEW-authoring write — never on stored or overlay reads — an authored id that does not resolve in the frozen map is refused, and a resolved level_id whose level lies outside its own zone [lo−3.00, hi+3.00] of the scenario's evaluator anchor (`kernel.ScenarioAnchor`, the same predicate the recording uses — `kernel.ResolveScenarioIdentity`) is refused. Two-anchor setups name `sweep_level_id` / `reclaim_level_id`: two different ids, each at its own leg's `ref_price` (confirm / confirm2). A NULL id stays WARN. Refusals re-author within the three attempts; the third failing attempt takes the existing fail-closed NO-TRADE path. History is untouched: no stored id is rewritten.
**Probe:** for every field a validator "records" instead of refusing, grep its readers. If any reader drives an order, a stop, a target or a gate from that field, the recording is a published contradiction — refuse at write, or stop the reader from trusting it.
**Fixed in W2:** `kernel.CheckScenarioWriteTruth` (A3 half `scenarioIdentityWriteIssues`) is called ONCE in the planner write loop, right after the born-dead check (`at.scenarioWriteTruth`), and in `shadowVerdictFor`. The error names both ids and both prices and routes to `RepairIdentityPriceLaw`. `IdentityLevelsFromCandidates` is the one projection shared with `StampAuthoredIdentity`. Counters `scenario_write_truth:<trader>:refused:<class>` (keyed by class, never by price text) and the `🪪📐 scenario write truth` boot line (n/a until the first recorded check). Pinned: trader `TestW2A3Row452S2RefusedThenRepublishedWithTheRightID`, `TestW2A3AlwaysDisagreeingFailsClosed`, `TestW2A3UnresolvedIDsRefusedThenRepublished` (invented strict id AND `ref|` altered digest), `TestW2A3MultiAnchorSweepReclaim`, `TestW2A3ZoneEdgeRejectNamingItsZoneIsAdmitted`, `TestW2LegacyStoredDocReadsUnchanged`, `TestW2ShadowVerdictParity`, `TestIdentityE1…` (re-pinned as the correction); kernel `TestW2A3FvgNamingItsOwnGapIsAdmitted`, `TestW2NilMapIsUnknownEmptyMapIsKnown`, `TestW2RepairLawRoutesTheNewRejections`, `TestW2Class38RowsRegisteredAndRendered`.

## CLASS 191 — a first obstacle that skips a seated level, and a path nobody had to list

**Found:** 2026-09-23, CTO amendment msg 1790166603535 [A]. Plans rowid 455 (2026-09-23:LONDON v1): S2 short from 31066.32 names RTH-H 31050.00 as `first_obstacle` (0.71R) while its own `target_chain` starts at SWG-H·15m 31059.00 (0.32R) — a seated level; S4 short from 31047.00 names PDC 31035.25 and omits SWG-H·5m 31043.00, a seated level 4 points from entry. Log 90561 (row 452): an arm disabled `net_nonpositive (gain 2 cost 2)`. The prompt accepted `reduce` on a single contract as "a declared intention".
**Shape.** The economics contract checked that an obstacle was PRESENT and not beyond the target, never that it was the FIRST one, and never that the levels in between were accounted for. The model's R-to-obstacle then measured to whichever level it chose.
**Law.** At NEW-authoring write: `target_chain` sorted outward from entry in the trade direction; `first_obstacle` equals the nearest SEATED level (the frozen `facts.IdentityMap`) strictly between entry and the arm target within one tick — a capacity-cut candidate nearer on the path is accepted, never required (the model never sees the cut pool); an empty seated path requires the arm target; every other seated level on that path appears in `economics.path_levels` `{price, level, level_id?, role: pass_through|reduce|exit}`, and a missing one is refused BY NAME; `reduce` with an arm of quantity 1 (a single arm, or legs summing to 1) is refused; no arm → quantity UNKNOWN → not checked. Excluded from the path: the scenario's own level_id candidate when the entry sits in its zone (the traded level itself; a level_id naming a validated reclaim leg further along stays on the path), anything inside `entry_zone` ±1 tick, and anything within a tick of entry or target. Authored chain prices (target_chain, first_obstacle, path_levels) are normalized to the tick grid and each normalization is recorded, never refused. `facts.IdentityMap` nil → the check is UNKNOWN and skipped (not []).
**Probe:** for any "first X" field, ask whether the validator ever computes the first X itself. If it only checks presence, a later X can stand in for it and every ratio computed from it is wrong.
**Fixed in W2:** `kernel.CheckScenarioWriteTruth` (A4 half `obstacleChainWriteIssues`) at the same single write-loop block and in `shadowVerdictFor`; `PlanFacts.CapacityCut` built from `input.Pool` minus the seated set (`CapacityCutCandidates`); errors route to `RepairObstacleChainLaw`; the prompt's reduce sentence is REPLACED (never both); four class-38 rows. Golden moved: `kernel/testdata/knob_prune/planner_prompt_defaults.txt` (the reduce sentence replaced; the A3/A4 sentences inserted after the NEW-authoring REFUSALS sentence — a correction, quoted in the PR). Pinned: trader `TestW2A4Row455S2AndS4RefusedByNameThenRepublished`, `TestW2A4ChainContractOnTheWriteLoop` (off-tick seated midpoint, skipped seated level, unsorted chain, reduce on a single arm, capacity-cut accepted/never required, nil map), `TestW2A4ShortMirrorSortedDescendingPasses`, `TestWriteTimeFeasibilityRejectPlayAtRealZoneAdmitted` (class-39 fixture: empty seated path, obstacle == arm target — stays green); kernel `TestW2CapacityCutIsPoolMinusSeated`, `TestW2Class38RowsRegisteredAndRendered`.

## CLASS 192 — a prompt header that tells the model the opposite of what the gate does

**Found:** 2026-09-23, W-EXEC-TRUTH W3 §3 (CTO amendment msg 1790166603535 §3) [A]. Decision 45139 (2026-09-23 06:38:50 UTC, plan 2026-09-22:ASIA v3) stored an executor system prompt whose PLAN BLOCK header read `preferred: follow it · a valid off-plan setup may still be traded (cite "off-plan")`, and whose field line offered `"off-plan" for a valid non-plan setup` — while plan_mode was strict and the entry gate refused every decision-path entry (log_events 92685: `entry_gate: refused: strict — plan_mode=strict executes plan scenarios on the ARM path only`). `kernel.RenderPlanBlock` rendered ONE header in every plan mode; the KEY LEVELS anchor line (`between them, a confirmed momentum/breakout may still be traded`) said the same.
**Shape.** A prompt sentence written for one gate configuration (advisory) and never re-derived when the gate became configurable. The model obeys the prompt; the gate refuses; the record reads "refused" and nobody reads the prompt that caused it.
**Probe:** for every gate the executor meets, list the prompt sentences that describe it and render the prompt under EVERY value of that gate's knob. A sentence that is true under one value and false under another must be rendered from the resolved value — through the SAME helper the production call site uses — and a golden per value pins it. When the advisory render is a boot-integrity golden, keep it byte-identical and add the new value as its own embedded case.
**Fixed in W3:** `kernel.RenderPlanBlockForMode(doc, session, mode)` (strict: `plan_mode=strict: entries execute ONLY through armed plan scenarios (market_in_zone policy); an AI decision is a nudge — cite the scenario; off-plan is refused`; direction: the bias rule; advisory/unknown: today's text); `RenderPlanBlock` = the advisory render, byte-identical (futures_mnq_plan.golden unchanged; the api Ask-Planner/realign callers stay advisory). ONE production selection `(*StrategyEngine).setExecutorPlanContext` (engine_analysis.go; `DayPlan.PlanModeFor(plan.Session)`) remembers the mode; the cited_scenario field line and the KEY LEVELS anchor are mode-aware under strict only. The boot self-check gained `futures-plan-strict` (embedded `futures_mnq_plan_strict.golden`, built through the same helper). Pinned: `TestW3FuturesPlanGoldenPerMode` (strict + direction goldens; advisory through the helper == the boot golden), `TestW3StrictPromptNeverOffersOffPlan`, `TestW3RenderPlanBlockForModeOnlyTheHeaderDiffers`, `TestW3ExecutorPlanModeHelperPerSessionOverride`, `TestW3Decision45139HeaderFixture` (redacted fixture), `TestW3BootSelfCheckHasFourCasesAllPassing`. RED: the strict header text drifted → `futures-plan-strict ok=false` with the three advisory cases `ok=true`; the helper ignoring the mode, and the field line not mode-aware, were each CAUGHT.

## CLASS 193 — an order policy stamped after the validator that refuses it

**Found:** 2026-09-23, W-EXEC-TRUTH W3 R4 (lane map, read-only @9ab006d4) [A]. The validator chain runs inside `ValidatePlanDocWithCaps` → `validateArmSpecs` → `ArmSpecValid`, whose legacy branch refuses acceptance / hold / breakout_retest arms and immediate waterfalls. A default entry policy stamped by the composer (or anywhere after parse) would never reach a plan that authored those arms: the plan is refused before the stamp exists, so the policy that makes them legal can never apply. The mirror defect: a default stamped where it is ILLEGAL (planned_order on a reclaim) turns a valid legacy arm into a refused one.
**Shape.** A field that changes what the validator accepts, written by a stage that runs after the validator. Order of operations decides the law, and nothing tests the order.
**Probe:** for every value that is stamped onto a document by the machine, name the validator that reads it and prove the stamp runs FIRST, on the new-authoring path only (a stored reader never stamps). Then prove the stamp is fail-closed: a stamp the validator would refuse is never written (the doc stays as authored), and a planner-set value is never overwritten.
**Fixed in W3:** `kernel.StampEntryPolicyDefault` runs in `parsePlanDocument`'s newAuthoring branch BEFORE `ValidatePlanDocWithCaps`, reached only through `kernel.ParsePlanDocForAuthoring(raw, maxL, maxS, AuthoringOpts{MinRR, EntryPolicyDefault, MinHoldMin})` (the planner write loop and the shadow A/B, one `plannerAuthoringOpts`); it stamps only when the policy is legal on EVERY leg (`EntryPolicyLegal`) and never over a planner-set policy; `ParsePlanDoc` / `ParsePlanDocCappedWithMinRR` stamp nothing. Pinned: kernel `TestW3StampAtParse`, `TestW3ValidatorNineConditionsByPolicyAndShape` (9 conditions × {absent, market_in_zone, planned_order} × {single, split}, the verdict derived from the policy table), `TestW3LegacyCorpusByteIdentical` (plan-{170,178,180,182,185,187,252,265,270} + class39 rows 69/85 through the stored reader: same verdicts, no policy key); trader `TestW3WriteLoopAcceptanceArmStoredWithPolicy`, `TestW3WriteLoopLegacyDefaultKeepsTheOldLaw`. RED: stamp a no-op, stamp ignoring legality, stored reader stamping, and the write loop parsing without the policy were each CAUGHT.

## CLASS 194 — a price range judged at one point of the range

**Found:** 2026-09-23, W-EXEC-TRUTH W3 (c) D7 [A]. A market_in_zone entry can fill anywhere in `economics.entry_zone`, but the write-time gates (`armGateVerdictFor`) judge ONE `leg.Entry`. Judging at the authored entry admits a zone whose far edge fails the R:R floor (write loop fixture: entry 15484 → R 3.58, far edge 15494 → R 2.24 < 3.0), and composing the stop from the far edge leaves the near-edge stop distance under the min-SL floor (authored stop 15479.75: 4.50 < 12.00).
**Shape.** A check written for a point applied to a range: it is correct for the one price it reads and silent about every other price the order can reach.
**Probe:** for every gate on an order that can fill across a range, name the fill that is WORST for that gate and judge it there — R:R at the far bound (least reward, most risk), stop distance at the near bound (least risk distance). A fixture must pass at the authored point and fail at the worst fill, or the test proves nothing.
**Fixed in W3:** `writeTimeZoneVerdicts` (trader/write_time_feasibility.go, NOT gated by write_time_feasibility — D5): `kernel.ArmZoneVerdict` first (a code → Kind "zone", arm_disabled_reason = the code), then the executor's `composeArmStop` from the NEAR bound and `armGateVerdictFor` at the FAR bound, then at the NEAR bound; the legacy feasibility path skips market_in_zone arms (no double judgement). Hinted with the `entry zone:` marker (`RepairEntryZoneLaw`), disabled on the last attempt. Pinned: trader `TestW3WriteLoopZoneBoundaries` (lo/hi inclusive, ±1 tick, width max / max+1 tick, trigger side ±1 tick, bracket, inward rounding, R:R far vs entry, stop from near), `TestW3WriteLoopZoneViolationHintedThenDisabled` (incl. write_time_feasibility OFF), `TestW3ZoneLegVerdictWorstFills`. RED: R:R at the authored entry and the stop composed from the far edge were each CAUGHT — the first only after its fixture was corrected: at the 2.0 floor it assumed, the case was refused at the entry too (the real floor is the 3.0 schema default) and the mutation SURVIVED.

## CLASS 195 — a re-arm loop that re-places a filled order within the same plan version

**Found:** 2026-09-23, W-EXEC-TRUTH W3 D15 (`fix/strict-follow-plan`, executor builder) [A for the code path, B for the live consequence]. Sibling of the MANUAL-CANCEL-WINS law (`store/armed_orders.go` UpsertArm, the E7 2026-08-30 re-place loop) and D5 (append-only placements).
**Shape.** UpsertArm's D5 branch mints a NEW armed row at `placement_seq+1` for ANY terminal row that reached the broker, whatever the plan version — pinned by `store/armed_placement_dup_test.go` and `armed_orders_test.go` for legacy rows. For a market_in_zone row that is the re-place loop by construction: the limit sits at the zone's far edge and is MARKETABLE whenever price is inside the zone, so fill → stop-out → the next pass re-authorizes the same scenario → seq+1 → a marketable limit → fill… And the W3 rest cap ("zone rest expired") would turn into a 30-minute re-placement timer. [B: the loop needs only the D5 mint plus a marketable price; no live row has shown it because no market_in_zone row existed before W3.]
**Probe:** for every order kind whose price can be marketable at authoring, walk fill → close → next pass → UpsertArm and count the rows and the signal frames. A terminal row that reached the broker under the SAME version must end its scenario's placements for that version unless a rule names why it may re-arm (today only the class-33 boot sweep). Check every terminal reason a rule of the wave produces (fill, rest-cap cancel, maintenance withdraw) and a new version (must re-arm).
**Fixed in W3:** the D15 pin in `UpsertArm`, before the D5 mint: `existing.State != armed && signal != '' && existing.Policy == market_in_zone && existing.Version == row.Version && !IsBootSweepReason` → `return nil`. Legacy rows keep today's mint byte-identically. Pinned: store `TestArmedZoneReArmPinnedWithinVersion` (filled / zone rest expired / withdraw: … each pinned; a new version mints ONE clean row with no inherited receipts; a legacy row still mints; a boot-swept policy row re-arms), trader `TestZoneReArmPinnedWithinVersionAtTheCallSite` (fill via `onArmedOrderUpdate` → the position closed → a FRESH tape inside the zone and a FRESH flat book → no new row and no frame; a new plan version re-arms and places once). RED: the pin removed is CAUGHT by both.

## CLASS 196 — a second pass trigger over state guarded only by "run-loop goroutine only"

**Found:** 2026-09-23, W-EXEC-TRUTH W3 D14 [A]. The armed pass's dedupe maps (`armRefusalLast`, `armStopCompLast`, `armAuthoredLast` — `trader/auto_trader.go`, commented "Run-loop goroutine only — no lock") and its in-pass placement latch were safe ONLY because one goroutine ever ran the pass. W3 adds two more triggers: the live-bar event pass (its own goroutine) and the strict nudge (inside the decision path).
**Shape.** A comment ("goroutine X only", "touched only from runCycle") is the lock. The moment a second caller exists, plain map writes race (a Go `concurrent map writes` fatal, not a wrong answer), and two passes can each read the same `armed` row and each believe it is the first placement of the plan. The ledger CAS (`BeginPlacement*`) stops a double stamp of ONE row, not two scenarios of one plan in two passes.
**Probe:** grep for "only — no lock", "single goroutine", "touched only from" on every field a new caller will reach. For each, name the lock that now guards it or prove the new caller cannot reach it. Then run the two callers at once with each held INSIDE its critical section long enough for the other to arrive, and assert they never overlap — a race detector alone can be satisfied by incidental happens-before edges (the logger's mutex, the DB) and stay green on a real race.
**Fixed in W3:** one per-trader `armedPassMu`, taken by `maybeManageArmedOrdersAtOpts` itself, so the scan (`maybeManageArmedOrdersAt`, runCycle), the event pass and the nudge are one pass at a time; no reentrancy (the nudge runs after the scan's pass returned). The event loop is started in Run and stopped in Stop, kicked non-blocking, ≤1 pass/s coalesced to the trailing edge. Test seam `armedPassEnterForTest` (nil in production, pinned). Pinned: `TestArmedEventAndScanPassesAreSerialized` (run under `-race`: exactly one frame, never two passes inside). RED: the lock removed is CAUGHT (the overlap assertion; the race detector did NOT flag it on its own — 0 DATA RACE reports in 4 runs, which is why the probe asks for a held critical section).

## CLASS 197 — a new trigger that places through the send point without the authoring gates

**Found:** 2026-09-23, CTO ruling on W3 D14 (relayed by the lane) [A for the rule; the defect was designed out before it was built].
**Shape.** `runArmedPlacementAt` is the SEND POINT. It places what an authoring pass admitted THIS pass (G1, W0b M2: a nil admitted set admits nothing). A new trigger — an event-driven pass, a nudge — that calls the send point "to place faster" must build an admitted set from somewhere, and the easy source is the ledger's armed rows: an arm the authoring gates would refuse this instant (daily force-flat, quality floor, invalidation, R:R at the live stop, HTF veto) is then placed because it was admitted by some EARLIER pass. That is G1 re-opened by a new road.
**Probe:** for every caller of the send point, show the admitted set it passes was computed by the authoring gates in the SAME pass. Build the discriminating fixture: an armed row from an earlier pass, the price inside, and an authoring gate that refuses it now — the trigger must place nothing.
**Fixed in W3:** the event pass and the nudge both run the FULL pass (`maybeManageArmedOrdersAtOpts`: authoring gates → admitted set → placement). The nudge's scenario scope filters PLACEMENT rows only, after the admitted set is computed and before `armAdmitted`, so other scenarios get no `arm_not_admitted` count. Pinned: `TestArmedEventPassHonoursTheAdmittedSet`, `TestStrictNudgeOnAGateExcludedScenarioPlacesNothing`, `TestStrictNudgeIsScopedToTheCitedScenario`. RED: the event pass calling `runArmedPlacementAt` bare with the armed rows admitted, and the nudge calling it bare with the cited scenario admitted, are each CAUGHT.

## CLASS 198 — a guard that reads a field its own caller never copied (fixed in W1b)

**Found:** 2026-09-23, W-EXEC-TRUTH W3 mapping (`fix/strict-follow-plan`), CTO ruling `1790185785421`: OWED, not W3 [A]. The churn guard in `trader/armed_executor.go` (`row.State == "working" && churnNeedsModify(row.StopPx, row.TargetPx, leg.Stop, leg.Target, tick)`, base `e74fce17` :867, now :904) can never be true. `row` is the struct the arm loop just built with `State: "armed"` and the NEW leg prices; when a ledger row already exists only `row.ID` is copied from it (:851). So the state is never `working` and the "old" bracket is the new one. `nt.ModifyBracket` is unreachable, and a re-spec'd plan never moves a working order's stop or target.
**Shape.** A guard compares "old vs new" on a value that was never loaded from the old record. It type-checks, reads plausibly, and is dead.
**Probe:** for every guard comparing a stored value with a fresh one, trace where the "stored" operand is assigned on the path that reaches the guard. If it comes from the same constructor as the fresh one, the guard is dead. A test at the call site must drive the guard TRUE at least once.
**Owed (as written at W3 — superseded by the fix below):** load `State`, `StopPx`, `TargetPx`, `SignalID` from the matched `existing[i]` (or delete the guard and its log line), with a call-site test that a working row whose plan moved the stop ≥ 2 ticks sends exactly one bracket modify. *The "one bracket modify" half was wrong:* the AddOn's `modify_bracket` acts only on a FILLED entry's bracket (`placedBrackets`, `HandleModifyBracket`), a working row is an unfilled entry, so the frame is refused ("no live bracket") and nothing in Go read the reply. The fix is cancel + re-arm, not modify.

**Fixed in W1b (fix/executor-owed-1):** the arm loop keeps the matched ledger row (`prior`) and hands it to ONE function, `respecWorkingArm` / the pure `armRespecFor` (`trader/arm_respec.go`), which issues AT MOST ONE cancel per row — E2 (the moved zone, class below) is asked first, E1 only when E2 is silent. E1 fires only for a WORKING row with a signal and no Picture source, under a NEWER plan version (`plan.Version > prior.Version`), when the AUTHORED stop or target of that scenario's leg moved by ≥ 2 ticks (`churnNeedsModify`) from what the row's own version AUTHORED. "Authored" is the plan doc's price before any composition: `armScenarioLegs` is the ONE turn of a scenario into legs, used by the loop and by the prior-version reader `armAuthoredLegAt`, which folds `prior.Version` through the ONE fold (`GetPlan` + `ListOverlays` + `kernel.ResolvePlanFinal`). The composed wire bracket (live-ATR min-SL floor, structure anchor, obstacle target) is NEVER compared: the first build compared placed vs composed, and its verifier showed an identical scenario re-published as v2 cancelling a working order as "SL 97.70→96.82" and counting `arm:respec_cancel` for a re-spec nobody made (class 35). An unreadable prior authored leg does NOT fire (absent ≠ changed, L7) and WARNs once ("armed re-spec NOT judged … the authored bracket of v<N> is absent"). The cancel goes through `cancelSafetyFor` (a refusal sends nothing, WARNs once per `plan:version:scenario:legN:respec` key, retried next pass) → `nt.CancelOrder` → `RequestCancel` with reason `bracket re-spec by v<N>: authored SL a→b TP c→d (…)` → counter `arm:respec_cancel`; the row is `cancel_pending` until the broker's book confirms it, then the next pass authors the scenario afresh and `UpsertArm` mints a new row under the new version (the D15 pin blocks a mint only within the version the old row was placed under); every existing gate judges the new placement. A leg cancelled in a pass IS in G1's admitted set (`admitted.admit` runs before the re-spec call); only that pass's geometry record stops short of "admitted" (the re-spec call sits before `saveArmGeometry`). **W1b FOLD-8** corrected the `trader/armed_executor.go` comment that claimed the leg was "not recorded admitted" (comment-only, line-neutral, no SYSTEM-MAP re-pin): there is no hole, because a `cancel_pending` row is never placed and TWO layers enforce it — the placement switch (`case "armed":`, `trader/armed_executor.go` :1344) and the `BeginPlacement` compare-and-set (`store/armed_orders.go` :634, `state = armed AND signal_id empty`). Pinned by `TestRespecCancelPendingRowIsNeverPlacedOnAFlatLiveBook` (a flat live book after the cancel, no persisted confirmation: no signal, no successor row, and neither "armed place failed" nor "placement requested" in the log). Mutating the switch alone is RED; the compare-and-set alone stays green behind the switch; both together re-send the v1 order (see the class "a pin overdetermined by a second guard" below — the respec tests' "place nothing" assertions do NOT pin the switch). The dead `ModifyBracket` call and its 📌 log are gone from the arm loop (`TCPTrader.ModifyBracket` itself is kept; no C# change, no NT8 deploy). The refresh `UpsertArm` of a non-armed row is skipped only when `prior.SourceRef == row.SourceRef`, so the W5 R13(a) typed `ErrArmSourceMismatch` refusal still fires for a working row. CTO ruling (Wave 1b Q1): version-gated cancel + re-arm, ONE function for E1+E2, ≤ 1 cancel per prior row, compare authored brackets across versions only. Pinned at `maybeManageArmedOrdersAt` over the real TCP wire: `TestWorkingArmBracketRespecByNewVersionReplacesTheOrder`, `TestLegacyWorkingArmAuthoredRespecByNewVersionReplacesTheOrder` (a legacy row: cancel_pending naming the AUTHORED change → the book confirms → ONE new row, `placement_seq` 1, v2, one new signal; geometry not recorded "admitted"), `TestArmRespecIgnoresATRDriftAcrossVersions` (version bump + ATR drift, authored bracket unchanged → NO cancel, NO counter; RED on the composed-compare mutation: "ATR drift across versions with an unchanged authored bracket must send nothing: sigs=0 cancels=1"), `TestRespecIgnoresATRDriftWithinOneVersion`, `TestArmRespecAbsentPriorAuthoredLegNeverCancels`, `TestRespecCancelRefusedByTheFilledArmGuardRetriesNextPass`, `TestRespecSkipKeepsTheTypedSourceRefusalForAWorkingRow`, plus the pure edges table `TestArmRespecForEdges`.
**Known limits (named, not fixed):** (1) an authored change the composition absorbs — e.g. a legacy arm whose structural (PDH) stop composes to the same wire stop either way — still cancels and re-places the SAME bracket once (the CTO formula compares authored prices; kept). (2) A re-spec'd legacy working limit loses its queue position, and while `cancel_pending` the row keeps the plan's slot (second-setup / one-contract / slot guards). (3) A working row placed under an older version costs one `GetPlan` + `ListOverlays` per pass until it settles. (4) Still OWED, fail-open as before: a new version whose zone is refused (`!zok`) `continue`s before the re-spec and leaves the old limit resting; so does a scenario removed or its arm disabled, a `wait_confirm` added, or a one-setup decline — the rest cap is the only exit in those cases. (A legacy or planned_order working limit whose AUTHORED ENTRY moved by ≥ 2 ticks without a bracket change no longer rests: W1b FOLD-1 re-specs it, see the next class.)
**Known limits (CTO rulings, Wave 1b folds — named, not folded):**
- **(L1) The re-spec cancel is fire-and-forget**, like the four existing cancel sites: `nt.CancelOrder` + `RequestCancel`, no ack wait, and a `cancel_pending` row is never re-judged by the re-spec. A lost cancel frame leaves the broker order live under a re-priced plan; the only retries are the settlement pass's bounded re-requests (`cancelReRequestMax`, `trader/cancel_confirm.go`) [B] and, inside a force-flat window or no-trade band only, FOLD-12's paced window re-send (it softens L1 there and nowhere else). **OWED:** the E11 one-`cancelArmRow` refactor (owner call).
- **(L4) Overlays on the LIVE version are never re-specced.** E1 judges only across VERSIONS (the version guard `planVersion <= prior.Version`, `trader/arm_respec.go` :156). An owner or machine overlay on the live version that moves a working arm's bracket (or, since FOLD-1, its entry) leaves the resting order as placed. And `armAuthoredLegAt` folds that version's overlays AS THEY ARE AT READ TIME onto `prior.Version` (`GetPlan` + `ListOverlays` + `ResolvePlanFinal`, `trader/arm_respec.go` :89-100), so an overlay added after placement makes `was` the patched value: a v2 equal to patched-v1 reads "unchanged" while the resting order still carries the pre-patch prices. Root [A]: the ledger records only the COMPOSED placed prices (`EntryPx`/`StopPx`/`TargetPx`, `store/armed_orders.go` :38-40), never the authored leg at placement. **OWED (separate wave, owner call):** three nullable `authored_*` columns stamped at placement; compare authored-at-placement with authored-now, version-free.

## CLASS 199 — a working order that outlives the plan version that priced it (fixed in W1b)

**Found:** 2026-09-23, W-EXEC-TRUTH W3 (`fix/strict-follow-plan`), CTO ruling `1790185785421`: OWED, not W3 [A]. When a new plan version moves a scenario's `entry_zone`, a `market_in_zone` limit already WORKING at the old far edge is not re-priced or cancelled by the version change. Only the zone rest cap (`day_plan.zone_rest_max_min`, default 30) ends it, so for up to 30 minutes the book can hold an order priced by a zone the current plan no longer states. Legacy arms have the same shape [B]: the arm loop matches an existing non-terminal row by plan id + scenario + leg, not version (:847-851), so a working legacy order keeps the price it was placed at.
**Shape.** The order's identity (plan, scenario, leg) survives a re-plan, but the price that justified it does not. Nothing compares the working order's price with the current version's.
**Probe:** for every resting order, name the plan version that priced it and the code path that runs when a newer version re-prices the same scenario. "The rest cap will end it" is a bound, not an answer.
**Owed:** on a new version, a working `market_in_zone` row whose zone no longer contains its limit is cancelled ("zone moved by v<N>") and the scenario re-arms under the new version; a call-site test drives a version bump with a moved zone.

**Fixed in W1b (fix/executor-owed-1):** E2 is the first question of the ONE re-spec function (`armRespecFor` / `respecWorkingArm`, `trader/arm_respec.go`; the E1 class above is asked only when E2 is silent, so a row gets at most one cancel). A WORKING `market_in_zone` planner row (signal set, no Picture source) whose resting limit lies outside the zone the CURRENT plan authorizes (`zl.v.Lo`–`zl.v.Hi`, 1e-9 tolerance) is cancelled through `cancelSafetyFor` → `nt.CancelOrder` → `RequestCancel` with reason `zone moved by v<N>: limit X outside Lo–Hi` → counter `market_in_zone:zone_moved`; once the book confirms, the scenario re-arms at the new zone's far edge as a new row under the new version. E2 is NOT version-gated: the zone comes only from `economics.entry_zone` plus the tick, never from ATR, and the row's limit IS the zone's far edge, so an unmoved zone always contains it and E2 is silent. **Named behaviour change:** an overlay that moves the zone WITHIN one version also cancels, and the D15 pin then blocks a re-arm for the rest of that version (fail-closed, deliberate). Pinned at `maybeManageArmedOrdersAt` over the real TCP wire: `TestZoneMovedByNewVersionCancelsTheWorkingLimitAndReArms` (cancel_pending "zone moved by v2", counter +1, then one re-arm at 100.00 under v2), `TestRespecLeavesAContainedUnchangedOrderAlone` (RED with containment forced true), `TestRespecCancelRefusedByTheFilledArmGuardRetriesNextPass`, `TestArmRespecForEdges` (E2 wins when both would fire; armed / place_pending / cancel_pending / blank-signal / Picture rows are never judged). The rest cap (`zone_rest_max_min`) is no longer the only exit — but it still is for the OWED cases named in the E1 class above (a refused zone, a removed/disabled scenario, a one-setup decline).

**Extended in W1b FOLD-1 (CTO ruling part 1, P1) — the ENTRY is a price the version priced too.** The first E1 compared only the bracket (`churnNeedsModify(was.Stop, was.Target, authored.Stop, authored.Target)`), so a v2 that moved a `planned_order` / legacy limit's ENTRY with the stop and target unchanged left the v1 limit resting at the old price, with v1's bracket, until the 30-minute rest cap [A at `75b83080`]. A non-market_in_zone row rests AT its authored entry (composition changes it only by `RoundToTick`) and the AddOn cannot move a resting entry, so the entry is judged like the bracket: in `armRespecFor` (`trader/arm_respec.go` :163-178), after the E2 zone check and the version guard and BEFORE the bracket (one decision per row), a row whose `Policy != market_in_zone`, with both entries > 0 (zero on either side = absent, never a change, L7), whose AUTHORED entry moved by ≥ 2 ticks (`churnNeedsModify(was.Entry, 0, authored.Entry, 0, tick)` — one threshold with the bracket) is cancelled with reason `entry re-spec by v<N>: E a→b (…)` and counted `arm:respec_cancel`; it re-arms under the new version once the book confirms, exactly like a bracket re-spec. A `market_in_zone` row keeps E2's zone rule (its limit is the zone's far edge, not the authored entry). Pinned at `maybeManageArmedOrdersAt` over the real TCP wire: `TestPlannedOrderEntryRespecByNewVersionReplacesTheOrder` (v1 cancelled once → cancel_pending, counter +1, settle, ONE new signal at the new entry under v2, `placement_seq` 1), `TestLegacyWorkingArmEntryRespecByNewVersionCancelsTheOrder`, `TestPlannedOrderEntryMovedUnderTwoTicksLeavesTheOrderAlone`, `TestMarketInZoneEntryMovedInsideTheSameZoneLeavesTheOrderAlone`, plus the pure `TestArmRespecForEntryEdges`. RED on a compiling revert of the 9-line rule ("a re-priced entry must cancel the working order … sigs=[] cancels=[]"), on dropping the market_in_zone exclusion, on a 1-tick threshold and on dropping the absent guards (the last caught only by the pure test — there is no call-site case for an absent prior entry).
**Known limits (FOLD-1):** (a) L4 (E1 class above) applies to the entry too: an overlay added after placement that moved the entry makes `was` the patched value. (b) [B, rounding edge] the rule compares RAW authored entries while the wire rests at `RoundToTick(authored)`: an off-grid authored pair whose raw move is between 1 and 2 ticks can round to a 2-tick wire move and not re-spec. A raw move ≥ 2 ticks always rounds to ≥ 2 ticks, so this can only MISS a move, never over-fire; the bracket rule has the same shape; unreachable when authored prices sit on the tick grid [C: planner grid enforcement not verified]. (c) The absent WARN still reads "the authored bracket of v<N> is absent" although the prior leg now feeds the entry rule too (`trader/arm_respec.go` ~:204, cosmetic).
**Probe (added):** for every resting order, list EVERY price it carries on the wire (entry, stop, target, trigger) and check the re-spec compares each one the new version can move; a re-spec over a subset of the order's prices is this class again.

## CLASS 200 — a producer with its own send path trades behind "No plan"

**Found:** 2026-09-23, W-EXEC-TRUTH W5 (D27; lane map `w5-map.md` §1/§2 at 42d90959) [A]. The Picture HTF evaluator claimed an opportunity and called `pictureHtfSubmitSeam`, bound (`trader/picture_htf_send.go` init) to `pictureHtfSend` → `TCPTrader.MarketEntryWithProtection` — a MARKET entry with its own admission, its own sizing and its own ledger, beside the Day Plan and never recorded in it. The plan card (`handlePlanToday`) rendered `found:false` → "No plan yet" while that path could trade under any non-strict plan mode.
**Shape.** A second producer owns a send. Every guard the plan path grew (the one fold, the executor's gates, the ledger's one-order pin, the card) is bypassed by construction, and the card's "No plan" is a statement about ONE producer presented as the truth about the account.
**Probe:** list every production caller of a broker entry method (`PlaceLimitEntry`, `PlaceStopEntry`, `placeEntry`, any market entry). Each must be reached from the Day Plan's armed executor or the gated decision path; a producer that reaches one directly is this class. Then ask the card: can an order exist while it says "No plan"? A recorded scenario must precede every order (created_at < placed_at_ms on the rows).
**Fixed in W5 (builder A):** the seam is the HAND-OFF (`trader/picture_plan_source.go` init binds `pictureHtfSubmitSeam = pictureHtfHandOffSeam`): the opportunity is RECORDED as a machine scenario — one machine overlay on an active plan, or a machine plan v1 when none exists, "Day Plan says no" when the plan is not active — the Picture row settles `planned` (`store.PictureHtfHandOff`), and only the armed executor may place it. Pinned: `TestPictureSeamIsTheHandOffAndNeverSends` (the production seam pointer + a real NT8 wire: zero signal frames), `TestPictureEvaluateHandsOffThroughTheProductionSeam`, `TestPictureHandOffOverlaysAnActivePlan`, `TestPictureHandOffWritesAMachinePlanWhenThereIsNone`, `TestPictureHandOffRefusesWhenTheDayPlanSaysNo`, `TestPictureHandOffPokesTheEventLoopOnlyAfterRecording`. The send path (`trader/picture_htf_send.go`) and `TCPTrader.MarketEntryWithProtection` are DELETED in W5 (retirement pinned by `TestPictureHasNoMarketEntryPath`).

## CLASS 201 — a new row kind every "latest row" reader already interprets

**Found:** 2026-09-23, W-EXEC-TRUTH W5 (F3 of the lane map; builder A's walk of every `GetLatestPlanForTraderSession` reader) [A]. The session scheduler fired the AI read only when `existing == nil` (`trader/auto_trader_planner.go:250`): ANY row meant "the read happened". A machine plan (the Picture no-plan door) is a row, so without a carve-out it would have silenced the AI session read for the whole session and fed the machine row to the dormant / death / flip / MSS / level-wake paths below it. The AI decision gate (`executorPlanDeadReason`) would have read the same row as an active plan and lifted the "planless entries refused" block.
**Shape.** A presence check stands in for a question about provenance ("did the AI read run?", "is there an AI plan?"). The moment a second writer can create the row, every reader of "the latest row" silently changes meaning.
**Probe:** when a new writer can produce a row kind, grep every reader of the table's "latest" row and answer, for each, what it does with the new kind. The list for plans at W5: the scheduler (fires the read — carved out), `executorPlanDeadReason` (refuses AI entries — carved out), the provider (serves the machine plan to the executor — intended), `lastWakeAuthoredVersionAge` / `carryMachineGrades` / the episode anchors / matched-random / the transition stand-down (inert on a machine doc: no levels, neutral bias — checked), and, outside builder A's files: the wake-levels / MSS / death re-read readers run only from the scheduler branches the carve-out skips (inert); the owner reset is owner-initiated; the OWNER RE-READ gate (`auto_trader_reread.go:61`) counted a machine plan as a plan — FIXED (CTO 1790194913337): the gate, its pre-read budget re-verify and the write-site spend treat a machine-only chain as the chain's first AI read (allowed, nothing spent), and the OWNER RESET gate refuses it like no plan (no AI chain to abandon); pinned by `TestOwnerRereadOnAMachineOnlyChainIsOneFreeRead`, `…IgnoresTheCounterLikeNoPlan`, `TestOwnerRereadOverAnAIPlanStillSpendsOne`, `TestOwnerResetRefusesAMachineOnlyChainLikeNoPlan`. Found on the way [A]: on a chain with NO row the re-read gate promises "the first read … costs no budget" (ReplansLeft = cap) but the write site spends one for `owner_reread` whatever the chain held (FIXED — CTO 1790195988056: nil IS the definition of no plan, and a recorded spend the gate denied is the class-35 lie; the write-site check is now `prev == nil || IsMachinePlan(prev)`, pinned by `TestOwnerRereadOnAnEmptyChainSpendsNothing`, RED with `prev == nil ||` removed: used 0 → 1); the cited-scenario quality read (`auto_trader_weekly.go:460`) reads the BASE doc, so a P-scenario cited on an AI plan reads "" (unknown) — pre-existing for owner overlays too (OWED — its own class below, CTO 1790194913337).
**Fixed in W5 (builder A):** `if existing == nil || store.IsMachinePlan(existing)` fires the read and `continue`s past every lifecycle path; `executorPlanDeadReason` returns "no AI day plan … planless AI entries refused" on a machine plan. Pinned: `TestMachineOnlyChainStillFiresTheAIReadAndP1RidesIntoV2` (the read fires, v2 is the scheduled read, no lifecycle event on v1, no budget spent), `TestExecutorPlanDeadReasonTreatsAMachinePlanAsNoAIPlan`.

## CLASS 202 — a failure reported after the durable effect

**Found:** 2026-09-23, W-EXEC-TRUTH W5 builder A, designing the hand-off's error contract [A for the contract; designed out before it was built]. The evaluator treats a seam error with `submitted_at = 0` as "never sent" and settles the opportunity `refused`. A hand-off that RECORDED the scenario and then failed to settle the row (a store error, a lost claim) would, by returning that error, have the opportunity marked "refused — never sent" while its scenario sits in plan_final and the armed executor places it.
**Shape.** A step returns an error for a failure that happened AFTER its durable effect; the caller reads the error as "nothing happened" and writes the opposite of the truth.
**Probe:** for every function with a durable write followed by more work, ask what its error means to the caller. Once the effect is durable the function must either succeed or leave a recoverable, named state — never an error the caller turns into "never happened".
**Fixed in W5 (builder A):** after the record `pictureHandOffAt` never returns an error; a failed settle WARNs and is left to the D17 sweep (`sweepInterruptedPictureHandOffsAt`), which settles from the RECORD (`planned`) and refuses only an unrecorded claim whose eligibility window has closed, under the same per-trader lock as the hand-off. Pinned: `TestPictureHandOffSweepSettlesFromTheRecord` (recorded-but-unsettled → planned; unrecorded + window open → untouched; closed → refused "hand-off interrupted — never sent"; recorded in a superseded version → planned).

## CLASS 203 — a plan reader that predates the ONE fold (OWED)

**Found:** 2026-09-23, W-EXEC-TRUTH W5 (`fix/w5-picture-source`, plan-source builder), CTO ruling `1790194913337`: OWED, not W5 [A]. The ONE-fold law: every reader of "the plan" folds through `kernel.ResolvePlanFinal` (base + user overlays, re-validated at the hard caps, then machine scenarios), so the executor, the card and every report see the same document. The cited-scenario quality reader in `trader/auto_trader_weekly.go` (base `eb7294c9` ~:460-470, `json.Unmarshal([]byte(row.Doc), &doc)`) reads the BASE doc: a cited Picture scenario (P<n>, only ever in an overlay) reads quality "", and so did a scenario an owner overlay added, before W5.
**Shape.** A second path to the same document, written before the fold existed, keeps answering from the base. Nothing fails; the answer is quietly different from what the executor traded.
**Probe:** grep every `Unmarshal([]byte(row.Doc)` / `ParsePlanDoc(row.Doc)` outside the fold; each is either a deliberate base read (named as such) or a reader that must fold.
**Owed:** route the reader through `kernel.ResolvePlanFinal(row.Doc, kernel.OverlayRefsFrom(overlays))`, with a call-site test citing a P scenario and an owner-added S scenario.

## CLASS 204 — entry permission that outlives the run or the switch that granted it

**Found:** 2026-09-23, W-EXEC-TRUTH W5 map F6 (`fix/w5-picture-source`, executor builder B) [A]. `Stop()` set isRunning=false, unregistered post-exit dispatch, stopped the event loop and the monitor — and cancelled nothing; the armed pass returned on `!dayPlanEnabled()` without a cancel; a reload builds a new AutoTrader in the same process, so the class-33 boot sweep (keyed on the process BootID) never fires. A resting or authorized entry therefore survived Stop, Day Plan OFF and a reload, and the next run's first pass could place a scenario the previous run recorded.
**Shape.** Permission is checked where an entry is AUTHORED and never again where it is SENT, and the thing that grants permission (the run, the master switch) has no hook that revokes what it already granted.
**Probe:** for every producer, name (1) what revokes its authorizations when the trader stops, when its master switch goes off, and when the process reloads without restarting; (2) where the send point re-asks those questions. Drive each: authorize, flip the switch / Stop / start a new run, run the pass — nothing may reach the wire, and every unplaced authorization must end terminal with a reason.
**Fixed in W5 (Picture rows):** a run epoch marked at `startArmedEventLoop` and stamped on every Picture scenario and row (`source_run_epoch`); the machine gate refuses a scenario from another epoch at authoring and `placeZoneRow` retires a row from another epoch at placement ("recorded by a previous run (epoch X, live Y) — not placed"); `admitChain` asks a source=picture arm row the running / Day Plan questions at its send point; `stopArmedEventLoop` clears the epoch and invalidates Picture rows under `armedPassMu` (unplaced → terminal "trader stopped", resting → cancel requested); the pass head's Day-Plan-OFF branch does the same. Wave 1b E6: the epoch belongs to the INSTANCE that Ran (`pictureRunEpochEntry{owner, epoch}`; the read stays id-level, last Run wins); `clearPictureRunEpoch` is owner-checked and `CompareAndDelete`s the exact entry it loaded; Stop's invalidation spares rows whose `source_run_epoch` is another instance's live epoch (`pictureOtherInstanceEpoch` — at Stop only a successor's is reachable), for event "stopped" only — Day Plan OFF still retires every Picture row of the trader, the successor's included. Planner rows are untouched (their behaviour is W3's; extending this to them is an owner call). Pinned: `TestPictureStopInvalidatesPictureRows`, `TestPictureDayPlanOffInvalidatesPictureRows`, `TestPictureRowFromAPreviousRunNeverPlacedAtPlacement`, `TestPictureScenarioRecordedByAPreviousRunIsRefused`, `TestPictureRowRefusedAtItsSendPointWhenStopped`, `TestPictureOldInstanceStopNeverClearsTheNewRunsEpoch`, `TestPictureOldInstanceStopNeverRetiresTheNewRunsRow`, `TestPictureDayPlanOffOnTheOldInstanceStillRetiresTheNewRunsRow`.

## CLASS 205 — a single-source rule applied to a second source

**Found:** 2026-09-23, W-EXEC-TRUTH W5 map F1 / F9, CTO rulings D8 + D9 (and the D9 mirror) [A]. Two rules written when the planner was the only source reached a second one the moment Picture became a plan scenario: one_setup (nil = ON) declines every non-`reject` play, so a Picture scenario (condition acceptance) would never place on any strategy that left the switch unset; and `cancelOtherArmsInPlan` ("one live entry per plan") would kill every unplaced planner arm the moment a Picture limit placed — and a planner placement would kill the Picture arm, which the source pin then keeps dead for good.
**Shape.** A rule's scope is "the plan" or "every scenario" in code, but its reason is about one producer's plays. A new producer inherits it silently: nothing refuses loudly, the other source just never places.
**Probe:** when a new producer joins a shared path, list every rule on that path whose REASON names the old producer's behaviour (a play filter, a slot rule, a cancel-the-rest rule) and decide per rule, in writing, whether it governs the newcomer. Drive both sources in one plan through the pass, in both orders, and count frames, cancels and terminal rows.
**Fixed in W5:** one_setup evaluates the planner-only doc and its consult is skipped for machine scenarios (a READ boot WARN says so while Picture is on and one_setup is ON); `cancelOtherArmsInPlan` never cancels an UNPLACED arm of the other source, in either direction (the one-live-entry guards refuse the second order until the first is terminal and flat); an other-source row already at the broker still gets its cancel requested. Pinned: `TestPictureScenarioPlacesWithOneSetupOn`, `TestPicturePlacementKeepsThePlannerArm`, `TestPlannerPlacementKeepsThePictureArm`, `TestPictureAndPlannerScenarioInOnePassYieldOneFrame`.

## CLASS 206 — a pass-clocked path that builds a wall-clock resolver (fixed in W1b)

**Found:** 2026-09-23, W-EXEC-TRUTH W5 builder B while wiring D13 [A]. `entryGateForArm` (trader/entry_gate.go) wires EntryGate leg 3 with `at.scenarioInvalidationResolver(plan)`, which closes over `time.Now` — inside `maybeManageArmedOrdersAt`, whose clock is `now`. The seam walk does not see it: `time.Now` is passed as a VALUE, not called. In production the two instants are microseconds apart; in a fixture pinned to another day the invalidation leg judges a different instant from every other leg of the same pass (a deadline evaluated there reads "passed" for every fixture window).
**Shape.** A seam that is honoured by every call but one, where the one hands the wall clock over as a function value.
**Probe:** grep every `…Clock(` / `…Resolver(` constructor reached beneath a registered `…At` variant for a `time.Now` argument; each must be handed the variant's `now`.
**Owed:** `entryGateForArm` takes the pass clock (`scenarioInvalidationResolverClock(plan, func() time.Time { return now })`) with its three test callers updated. W5 works around it for machine scenarios only: the resolver judges a machine deadline on the tape it read (`tapeClock`), and the pass-clock machine gate judges the deadline first (`pictureScenarioGate`).

**Fixed in W1b (fix/executor-owed-1, E3):** `entryGateForArm(plan, sc, leg, side, biasDir, atr5m, now, structural…)` takes the pass clock (before the variadic structural flag) and wires leg 3 as `at.scenarioInvalidationResolverClock(plan, func() time.Time { return now })`; its one production caller (`trader/armed_executor.go` ~:794, inside `maybeManageArmedOrdersAtOpts`) passes the pass's `now`, changed in place (line-neutral; the Class 33 byte-cap region untouched). The wall-clock wrapper `scenarioInvalidationResolver` is DELETED (nothing called it) and its `clock-seams.list` row is a "REMOVED (W1b E3)" note. The "three test callers" were four calls in three files (`entry_gate_test.go`, `condition_precedence_test.go` ×2, `position_side_casing_test.go`); all now pass fixed instants. `tapeClock` stays: besides the wall-clock workaround it keeps a 5m bucket the tape only partly holds from counting as closed on a stale tape (`AggregateToMinutes` sets a bucket's CloseTime to its end), so Picture behaves exactly as before. Pinned at the production call site: `TestArmInvalidationLegJudgesThePassClock` (`maybeManageArmedOrdersAt` on a rig clock fixed at 2026-09-11 15:00 UTC; reach proven twice — the main subtest places one limit, which comes after leg 3, and a control subtest on a pass clock 6 minutes later refuses exactly once at `entry_gate:invalidated`). RED on both compiling reverts (the resolver back on `time.Now`; the call site passing `time.Now()`): "leg 3 judged a 5m bucket that closes after the pass clock … the resolver is not on the pass clock".
**This is CONSISTENCY, not strictness (CTO-accepted, Wave 1b rulings).** The wall clock counts at least as many bars closed as the pass clock does, so moving leg 3 onto the pass clock can LOOSEN it: (a) by bars that close while the pass is running, and (b) by the `armedPassMu` wait — `now` is taken before `armedPassMu.Lock()`, so a pass queued behind another (e.g. a strict-nudge full pass with synchronous cancels) judges leg 3 at an instant older by the whole wait, where before it read the wall clock after the wait. An invalidation that lands in that gap can let one placement through; the next pass (event-paced, ≤ 1/s) sees it. Every other leg of the pass already judged on `now`; if fail-closed is ever wanted here, the ruling would be "the later of the pass clock and the wall clock" for leg 3.
**Still open (OWED):** `lastTouchFor` (`trader/no_chase.go` ~:232) still keys its touch count on `kernel.CMESessionDayKey(time.Now())` beneath `entryGateForArm` — the same class; the no-chase leg only WARNs, so only its telemetry's session day is wall-clocked; thread `now` into it. The seam walk (class 113) still cannot see `time.Now` passed as a VALUE, so this Probe remains a grep until the walk learns it. Only the Picture path is proven RED/GREEN at the call site; planner scenarios get the pass clock through the same line (`activeSessionName(now)`, `EvaluatePlanScenarios(…, now.UnixMilli())`) [B] with no twin test yet.

## CLASS 207 — a log line carries the account name inside a composite key (OWED for the pre-W5 lines)

**Found:** 2026-09-23, W-EXEC-TRUTH W5 (`fix/w5-picture-source`), CTO ruling `1790197560916` [A]. A Picture opportunity key is `"<strategy>|<account>|<contract>|<direction>|<role>|<levelOpen>|<h1Close>"` (`store.PictureHtfOppKey`). Printing the key prints the ACCOUNT NAME, which L12 forbids in logs, and nobody sees it because the key reads like an opaque id.
**Shape.** An identifier assembled from several fields is logged as one token; one of those fields is a secret-by-policy value.
**Fixed in W5:** every W5-authored line and error prints the key through ONE redactor, `store.RedactPictureOppKey` (account segment → "…"); the entry latch names a Picture holder by row id and stage only (`latched by picture row #<id> (<stage>)`). Pinned: `TestRedactPictureOppKey`, `TestHandOffLinesNeverCarryTheAccountName` (RED: redactor removed from the 🖼 hand-off line → caught), `TestEntryLatchLedgersListPlacedRowsOnTheAccount` (the latch text never carries the key), `TestReappendLinesNeverCarryTheAccountName` (RED: redactor removed from the re-append 🖼 line → caught; CTO round 1 R3 — the two re-append lines in `reappendLiveMachineScenarios` printed the raw ref). The plan doc, the ledger's `source_ref` and the API/card keep the full key — storage and the owner's own UI, not a log.
**Pre-W5 lines — CLOSED in W5 (CTO round 1 R5, ruled: W4 is dev's now):** the evaluator printed the raw key at two places, not the `:398` this entry first cited (that is the foreign-contract WARN, no key) — the generation-refusal WARN (`trader/picture_htf_evaluator.go` ~:696-697, "the trader that began this evaluation is gone … opportunity %s") and the 🔒 maintenance-hold WARN (~:731). Both now print `store.RedactPictureOppKey(oppKey)`. `trader/picture_htf_send.go:110` went with the file (W5 step 2 deleted the send path).
**Probe:** grep every log/format call for a variable that holds a composite key (`OppKey`, `oppKey`, `SourceRef`, `Machine.Ref`); each goes through the redactor or names the row by id.

**Instance 2026-09-24 M3:** `%v`, `%+v`, `%#v` and `%s` of `updateauth.Grant` printed its 64-hex MAC, and the install hand-off error is logged with `%v` (red-3 #6). Pinned by `TestGrantNeverFormatsItsMAC`, `TestInstallHandOffErrorNeverLogsTheGrantMAC` (`f1b7370d`)

## CLASS 208 — a level declared on fewer witnesses than its own definition names

**Found:** 2026-09-23, W-EXEC-TRUTH W4 (`fix/w4-picture-evidence`), D22 [A].

**Shape.** `kernel.BodyPivots4H` defines a 4H body pivot as a candle whose four
neighbours (i−2, i−1, i+1, i+2) all fail to exceed it. The loop ran
`i := 1 .. len-2` and the neighbour loop `continue`d past any neighbour that was
out of range or absent — so a "pivot" could be declared on as few as TWO
observed neighbours and still carry the authority of the four-neighbour rule.
The doc comment said "All four confirming neighbors"; the code skipped the ones
it could not find. Nothing in the type recorded how many had actually been read.

**Fixed in W4:** the range is `i := 2 .. len-3` and `fourNeighboursComplete`
requires all four to exist and i+2 to carry a completion stamp; a hole REFUSES
instead of being skipped. RED: "pivot formed at idx 1 with no i-2 neighbour".

**The tell to look for elsewhere:** a loop that states a quorum in prose and
implements it with `continue`. Ask what the code does when a witness is missing
— skipping one is not the same as not needing it.

## CLASS 209 — an absent timestamp that reads as permission

**Found:** 2026-09-23, W-EXEC-TRUTH W4, D22 [A]. Same wave as the above; the
two travel together and are worth reading as one.

**Shape.** `PictureHtfLevel.KnowableAt` was assigned only when i+2 happened to
exist. When it did not, the field kept Go's zero value — and all three
consumers (`ActiveLevels`, `H1CloseBreak`, `NearestOpposingZone`) tested
`KnowableAt == 0 || KnowableAt <= now`, i.e. read "we never established when
this became knowable" as "it has always been knowable". The safest-looking
reading of an absent value was the most permissive one. It was also anchored to
the OPEN of the confirming candle rather than its close, making every level
usable a whole period early.

**Fixed in W4:** the pivot rule guarantees i+2 exists, so `KnowableAt` is
always a real completion time and never 0; all three consumers now refuse 0 as
missing evidence. RED: "level carries KnowableAt=0 (absent standing in for a
real time)".

**The tell:** `x == 0 || x <= now`. A zero that shares a comparison with a real
value is a fabricated value wearing the type's clothes (canon: absent ≠ []).

**Instance 2026-09-24 M3:** **CLOSED** in `fix/m3-install-path-lows` `cb23f6ca` (fa `dd3c472e` cherry-picked): `Enroll` creates the seen store; once enrolled, a missing store is `ErrSeenCorrupt`, never empty; pinned by `TestEnrollCreatesTheSeenStoreAndAMissingOneAfterwardsIsCorrupt` and `TestInstallRefusesWhenTheSeenStoreIsMissingAfterEnrollment`.

## CLASS 210 — a requirement whose own fetch cannot satisfy it

**Found:** 2026-09-23, W-EXEC-TRUTH W4, D21/F2 [A] (arithmetic from the code;
101's read-only map raised it, verified independently at this wave's base).

**Shape.** The spec said "require ≥ PivotWindow+4 COMPLETED 4H candles". The
evaluator fetched exactly `PivotWindow+4` and only THEN filtered to completed
ones — and the provider returns the TAIL, which always holds the candle still
forming. So the rule could never pass: not rarely, by construction. Implemented
literally it would have refused every Picture entry within about a trading day.

**Fixed in W4:** `pictureHtfDepthMargin` makes the FETCH exceed the
REQUIREMENT, and `PictureDepthEvidence{Fetched, Completed, Required}` reports
the count that was READ ("insufficient depth 8/12"). RED at the provider seam:
"the 4H fetch asked for 12, which cannot yield 12 completed bars once the
forming bar is filtered out".

**The tell:** a threshold compared against a filtered subset of a fetch sized to
the threshold. Ask what the filter removes, then ask whether the fetch accounts
for it. A dispatch can carry this defect too — this one did, and was corrected
before it shipped rather than after.

## CLASS 211 — evidence stripped at a seam that rebuilds the type

**Found:** 2026-09-23, W-EXEC-TRUTH W4, D21 identity [A].

**Shape.** Three separate facts about a bar frame — the contract it belongs to,
whether the AddOn PROVED the bar closed, and when the source emitted it — were
each carried correctly by the wire and each lost in Go. `contract` had no field
on `BarsHistoricalPayload`/`BarUpdatePayload` at all, though the AddOn has
stamped it on EVERY bar frame since 2026-09-11. `Final` and `EmittedAt` existed
on both types but were dropped where `pictureHtfLiveBars` built its klines by
naming five fields: `market.Kline{OpenTime, Open, High, Low, Close}`. So the
Picture evaluator received frames stripped of everything that made them
evidence, and could not tell whose tape it was reading, a closed candle from a
forming one, or how old the data was.

**Fixed in W4:** the payloads parse `contract` (no AddOn change — only a
field); the consumer carries Final, EmittedAt and Contract; OnBars ignores and
counts a definite contract mismatch. RED at the type level: "p.Contract
undefined (type BarUpdatePayload has no field or method Contract)".

**The tell:** a struct literal that names fields instead of copying. Every field
added to the source type afterwards is silently dropped there, and no test
fails. Grep for the seams that rebuild a type rather than pass it.

## CLASS 212 — a clock that always dominates, added as belt-and-braces

**Found:** 2026-09-23, W-EXEC-TRUTH W4, D23/R1 — found by CTO review of this
wave's own push 6 [A]. Recorded because the defect was INTRODUCED by the fix
for the class above it, which is the more useful lesson.

**Shape.** Freshness had read only our own RECEIPT clock, so data emitted long
ago but delivered this instant passed. The fix bound the limit to the worst of
three clocks: source `emitted_at`, the candle's own close, and receipt. But NT8
emits a closed bar on the FIRST TICK OF THE NEXT BAR, so `emitted >= close`
ALWAYS and candle age dominates source age unconditionally. "Worst of three"
silently became "elapsed since the boundary ≤ freshness_sec" — 2s against a 10s
entry window — refusing the ordinary late-emission case outright: a candle
closing 14:00:00 and emitted 14:00:03 has a source age of 0.1s and a candle age
of 3s.

**Fixed in W4:** the limit binds worse(source, receipt) only; the candle close
stays as READ evidence and binds nothing. RED: "a candle emitted 3s after its
boundary is 3s late, not stale — it must send inside the 10s window, got 0".

**The tell:** adding a clock, a bound or a check "for safety" without asking
whether it can ever LOSE. A term that always dominates has replaced the rule,
not reinforced it.

## CLASS 213 — a registry entry evicted by an owner that has already been replaced

**Found:** 2026-09-23, W-EXEC-TRUTH W4, D25 [A].

**Shape.** `Stop()` never removed the trader from `pictureHtfTraders`, so a
stopped trader kept receiving live frames. The obvious fix — `Delete(at.id)` —
introduces a worse bug: a RESTARTED trader has already re-registered under the
same id, so a late Stop from the old instance evicts the live one. And that
registry is shared: `pictureHtfLiveBars` Ranges it to drive W3's armed-kick
pass, so evicting the wrong entry silently stops the armed event pass for a
running trader — a Picture change breaking an executor feature, with nothing in
either place to connect them.

**Fixed in W4:** `CompareAndDelete(at.id, at)`, plus a trader GENERATION that
an evaluation records at its start and re-checks immediately before the wire,
so an evaluation in flight across a Stop or restart cannot send for a trader
that is gone. RED: "the RESTARTED trader was evicted by the old instance's Stop
— W3's armed kicks would stop with it".

**The tell:** any `Delete(key)` on a shared registry whose values can be
replaced. Removal keyed by identity alone cannot tell "mine" from "the one that
took my place". Ask who else Ranges the map before changing what is in it.
**Second instance (Wave 1b E6):** W5 added `pictureRunEpochs` beside this very
registry, keyed by trader id and cleared by a bare `Delete(at.id)` — an old
instance's late Stop erased the successor's run epoch and retired its rows.
Fixed the same way (owner-checked `CompareAndDelete`), plus a Stop-time
cleanup that spares another instance's live rows; see the W1b class "ownership
state keyed by a shared id, cleared by a bare Delete (second instance)" below.

## CLASS 214 — a settlement half gated behind the switch whose OFF it must settle

**Found:** 2026-09-23, W-EXEC-TRUTH W5 CTO review round 2 (R8, two independent reviewers) [A]. The armed pass returned at its head when Day Plan was OFF (`trader/armed_executor.go` `maybeManageArmedOrdersAtOpts`, `!at.dayPlanEnabled() … return`). A Day Plan toggle reaches a live trader only through a strategy save → RemoveTrader → Stop → `retirePictureRowsOnStop` → a NEW trader (CTO 1790205640621). The Stop-side retire holds a resting Picture limit `cancel_pending` when the book is dark or stale (no blind wire cancel). The trader that comes back with the master OFF returns at the head, and the W5 OFF sweep inside that return can request more cancels. But every settlement site sat BELOW the return: the order-update drain, `confirmPendingCancels`, and the withdraw path only under a withdraw reason. The boot sweep (`SweepableArmStateSQL`) excludes `cancel_pending`, and the OFF sweep skips it on later passes. So the row never settled. The entry latch counts any non-terminal row with a signal as PLACED (`entry_latch_wiring.go` `entryLatchLedgers`), so the AI decision path — the only entry path while OFF — was refused on every entry until the Day Plan came back ON, and a restart did not clear it. Planner rows had the same freeze under OFF before W5 (pre-existing); W5 made it reachable on every OFF reload with a resting Picture limit (Stop retire → reload OFF → never settled).

**Fixed in W5:** the OFF head runs `dayPlanOffPassHead` (`trader/cancel_confirm.go`): the SETTLEMENT half first (`settleArmedLedgerWhileOff` — drain + `confirmPendingCancels`, re-requests through the filled-arm guard), then the Picture sweep. Nothing in it places, arms or authors. Pinned at the call site: `TestDayPlanOffStillSettlesTheCancelItRequested` (OFF + a `cancel_pending` Picture row + a fresh persisted book without the order → `cancelled`, `entryLatchLedgers` empty). RED with the settlement call removed: "Day Plan OFF must still settle the cancel from the fresh book (else the account latches)".

**Probe:** for every early return at the head of a periodic pass, list what the pass SETTLES below it (drains, confirmations, reapers) and ask whether anything that runs on the early-return branch — or anything a previous pass left behind — needs that settlement to reach a terminal state. A switch that turns off NEW work must not turn off the bookkeeping of work already sent.

## CLASS 215 — a reconcile sweep that annotates a row another sweep owns

**Found:** 2026-09-23, W-EXEC-TRUTH W5 CTO review round 2 (R11, confirmed by both skeptics) [A]. The Picture broker reconcile (`trader/picture_htf_broker.go` `pictureHtfReconcilePending`) read every `place_pending` / `working` row and skipped only `signal_id == ''`. Since W5 a hand-off in flight (or interrupted) sits `place_pending` with the evaluator's synthetic claim id (`store.PictureHtfClaimPrefix` + ms) and `submitted_at` 0 — nothing was sent. The sweep stamped it `reconcile:no_received_evidence (outcome unknown — will not resend)`, and the note survived the row settling `planned`: the ledger claimed a send that never happened.

**Fixed in W5:** the sweep skips `pictureHtfUnsentClaim` rows (claim prefix AND `submitted_at == 0`) — the exact input predicate of the D17 sweep (`PictureHtfHandOffPendingByTrader`), so every `place_pending` row belongs to exactly one sweep. Pinned: `TestPictureHtfReconcileNeverAnnotatesAnUnstampedClaim` (RED without the skip: "annotated a claim that was never sent"), `TestPictureHtfReconcileStillAnnotatesAStampedClaimPrefixRow` (RED with the `submitted_at` half dropped: "over-skip … got 0").

**Probe:** when two sweeps read the same table, write each one's input predicate side by side and prove they partition the rows. A row both can read gets two stories; a row neither reads is stranded.

## CLASS 216 — a version-insensitive ledger key under a version-local id mint

**Found:** 2026-09-23, W-EXEC-TRUTH W5 CTO review round 2 (R13, confirmed by both skeptics) [A]. `nextPictureScenarioID` minted a Picture scenario id (P<n>) from the CURRENT plan version only, while the armed ledger's key is `(plan, scenario, leg)` — version-INSENSITIVE. Opportunity B handed off onto v2 before A's re-append took A's `P1`; A's re-append was refused ("id already in the plan"), and `UpsertArm`'s armed branch rewrote A's unplaced row to B's `source_ref`, deadline, run epoch and prices with only an INFO. One opportunity silently became another.

**Fixed in W5:** (a) `UpsertArm` refuses any write whose existing row carries a non-empty `source_ref` different from the write's — typed `store.ErrArmSourceMismatch`, logged nowhere in the store (the error carries row, scenario, leg, state and both keys, redacted); the authoring loop turns it into a named refusal (`arm_source_mismatch`: scope note, one WARN + counter per change, the leg's G1 admit withdrawn) and is the ONLY place it is logged on the authoring path (Wave 1b E7: the store's own per-write WARN was removed — five passes had printed five store WARNs beside the loop's one; the trader-side `armSourceRefused` WARNs once per change). The shadow path and the API seams log or return the error themselves. (b) P ids are minted past every scenario id any EARLIER version of the plan used (`pictureIDsOfEarlierVersions`: resolved doc + every machine overlay; a read error refuses the hand-off). Pinned: `TestArmedRowNeverChangesOpportunity` (store; armed / cancelled-unplaced / filled), `TestPictureIDsNeverCollideAcrossVersions`, `TestAReusedPIDNeverRewritesAnotherOpportunitysRow`, `TestArmSourceMismatchWarnsOncePerChange`.

**Probe:** for every id a writer mints, find every table keyed on it and ask whether the key's scope (per version, per plan, per chain) is the mint's scope. A key wider than its mint lets two things share one row.

## CLASS 217 — a UI state not backed by an API field

**Found:** 2026-09-24, W-ONE-BUTTON M5 build [A]. The Updates page vocabulary
(Update available, Installing, Up to date) had no backing fields on
GET /api/updates — the M3 payload is exactly {enrolled, manifest_verifier,
install_enabled}. The page would have had to invent a verdict to fill them.
Shipped: the optional fields are typed but ABSENT today, and the badge maps
them to Unknown whenever the API does not affirm a state; the page's Blocked
is the only M3-reachable verdict.

**Probe:** for every label a component can render, ask which API field it reads.
A label with no field is either dead (remove it) or a future field (type it
optional, render Unknown / n/a until the server ships it) — never a browser-side
derivation.

## CLASS 218 — a spinner with no timeout

**Found:** 2026-09-24, W-ONE-BUTTON M5 build [A]. The header badge's first-fetch
spinner would hang forever if the poll promise neither resolved nor rejected
(a hung connection). Shipped: the first fetch races a 10 s cap and the badge
settles to Unknown; a rejected poll is caught and also settles to Unknown.

**Probe:** every spinner in the UI must name the condition that ends it, and one
of those conditions must be a clock.

## CLASS 219 — a null rendered as false

**Found:** 2026-09-24, W-ONE-BUTTON M5 build [A]. A null addon_ack (the AddOn
has not acknowledged the hold yet) read naturally as "false" in JS boolean
context; rendering it as false claims the AddOn REFUSED the hold, which the
server never said. Shipped: null renders "no ack yet" and a test pins that the
page never renders the word false for it.

**Probe:** every nullable verdict field rendered into a yes/no slot must have an
explicit null branch with its own text — null is "no answer yet", never "no".

## CLASS 220 — a re-spec gate that compares the COMPOSED wire price, so drift reads as a plan change

**Found:** 2026-09-23, W-EXEC-TRUTH Wave 1b E1 (lane Claude-101, `fix/executor-owed-1`), by the E1-E2 verifier's probe on the first build [A]. The first E1 fix compared the resting order's bracket (`prior.StopPx`/`prior.TargetPx`, composed when it was placed) with THIS pass's composed leg (`leg.Stop`/`leg.Target`) under a newer version. The wire stop is composed at every pass from live ATR (the min-SL floor), the structure anchor and the obstacle target, so any version bump — an overlay on another scenario, a replan, a Picture re-append — carried all accumulated ATR drift through as a "bracket re-spec": an identical scenario re-published as v2 cancelled a working order as "SL 97.70→96.82" and counted `arm:respec_cancel` for a change nobody made (class 35: counters record, never infer). The version gate only stopped the churn inside one version.
**Shape.** A "did the plan change?" question answered by comparing two values the EXECUTOR derived from the plan plus live inputs. Every derived value drifts with its inputs; only the authored value answers the question.
**Fixed in W1b E1 repair:** `armRespecFor` compares the AUTHORED leg of the current version (`legs[li]`, from `armScenarioLegs`, before any composition) with the AUTHORED leg of `prior.Version`, read by `armAuthoredLegAt` through the ONE fold (`GetPlan` + `ListOverlays` + `kernel.ResolvePlanFinal`) and the same scenario-to-legs function the loop uses. An unreadable prior leg does not fire and WARNs once (absent ≠ changed). E2 (a limit outside the current zone) is not version-gated because authoring alone moves the zone, never ATR. CTO ruling (Wave 1b Q1): "compare AUTHORED brackets across versions through the ONE fold of prior.Version, never composed/live-ATR brackets … RED it against the fake-cancel case".
**Probe:** for every gate that asks "did the plan/owner change X?", name where each operand comes from. If either passes through a composer that reads live data (ATR, the tape, a structure map), the gate is judging drift. Build the discriminating fixture: identical authored input, a version bump, and moved live data — nothing may be sent and no counter may move. Pinned: `TestArmRespecIgnoresATRDriftAcrossVersions` (RED on the composed-compare mutation: "ATR drift across versions with an unchanged authored bracket must send nothing: sigs=0 cancels=1"), `TestArmRespecAbsentPriorAuthoredLegNeverCancels`, `TestLegacyWorkingArmAuthoredRespecByNewVersionReplacesTheOrder`, `TestArmRespecForEdges` ("a < 2-tick AUTHORED move is not a re-spec").
**Known limit (named):** an authored change the composition absorbs (a legacy structural stop that composes from the PDH zone either way) still cancels and re-places the same bracket once — kept by the CTO formula.

## CLASS 221 — ownership state keyed by a shared id, cleared by a bare Delete (second instance)

**Found:** 2026-09-23, Wave 1b E6 (lane Claude-101, `fix/executor-owed-1`) [A]. W5's run epoch (`trader/picture_run_epoch.go`) lived in a process-wide `sync.Map` keyed by trader id and was cleared with `pictureRunEpochs.Delete(at.id)`. A reload builds a NEW AutoTrader under the SAME id; if the new instance Runs before the old one Stops, the old Stop erased the new run's epoch (every hand-off "trader not running (no live run epoch)", every placement retired its row), and `invalidatePictureRows` — scoped by trader id only — retired the rows the new run had recorded. The W4 `pictureHtfTraders` class ("a registry entry evicted by an owner that has already been replaced", above), repeated in the map W5 added beside it. Not proven reachable in production today [B: `RemoveTrader` is believed to stop synchronously before the reload builds — unverified]; pinned so no reload ordering can reach it.
**Shape.** Per-run state keyed by an id that outlives the run's owner; any cleanup keyed by that id alone cannot tell "my run" from "the run that replaced me".
**Fixed in W1b E6:** each entry names its owner (`pictureRunEpochEntry{owner, epoch}`); the read stays id-level (last Run wins); `clearPictureRunEpoch` is owner-checked and `CompareAndDelete`s the exact entry it loaded; Stop's invalidation spares rows whose `source_run_epoch` is another instance's live epoch (`pictureOtherInstanceEpoch`; at Stop only a successor's is reachable, because Stop requires `isRunning` and Run marks its epoch right after setting it — the one exception, a Stop between those two steps, leaves a still-live predecessor's rows to that predecessor's own Stop). The skip is event "stopped" ONLY — Day Plan OFF is the strategy's master switch, not a run boundary, and still retires every Picture row, the successor's included. No other instance's run = the pre-wave rule.
**Probe:** `grep -n '\.Delete(at\.id)\|\.Delete(.*\.id)' trader/*.go` on any registry whose values a later instance can replace → must be an owner/identity-checked `CompareAndDelete`. Any Stop-time cleanup scoped by trader id alone must say, in code and test, what it does to a successor's rows — and any exemption must be scoped to the event that is a run boundary. Pinned (order old.Run → new.Run → …, production `startPictureRunWith` + `Stop` + pass head): `TestPictureOldInstanceStopNeverClearsTheNewRunsEpoch`, `TestPictureOldInstanceStopNeverRetiresTheNewRunsRow`, `TestPictureDayPlanOffOnTheOldInstanceStillRetiresTheNewRunsRow` (RED with the event scoping removed: "Day Plan OFF on the old instance must retire the new run's row too (the successor skip is Stop's only): armed \"\"").
**Owed:** orphan-Run (`handleStartTrader` `go Run()` vs `RemoveTrader`) and double-Run-on-one-instance (`handleUpdateTrader`) lifecycle guards; the old instance's in-flight pass authoring under the id-level (successor's) epoch (follows the "last Run wins" ruling; not a regression); a same-nanosecond epoch collision between two Runs is unguarded [C] (placement already refuses a row whose run is not live).
**Known limit (L5, CTO ruling part 2 — named, not folded): E6 amplifies a PRE-EXISTING reload race.** `RemoveTrader` (`manager/trader_manager.go` :361-375) stops the old instance ONLY if its status already reads `is_running`; the update handler (`api/handler_trader.go` :736-752) removes, reloads, then starts the new instance with `go reloadedTrader.Run()`. A second reload while the previous `go Run()` has not yet set `isRunning` therefore skips `Stop` and orphans instance B; B's `Run` then stores its epoch unconditionally over the live successor's, and the successor's own `Stop` treats its rows as another instance's and strands them. So the "not proven reachable" note above is answered by a reachable ordering. Root = the reload path, not E6. **OWED there** (owner call): the orphan-Run / double-Run lifecycle guards named above.

## CLASS 222 — a loud log at a layer with no identity to dedupe on

**Found:** 2026-09-23, Wave 1b E7 [A]. W5 R13(a) put `logger.Warnf("⛔ arm write refused …")` inside `store.UpsertArm` at the `ErrArmSourceMismatch` refusal. Every event pass and cycle re-authors the same leg and re-hits the refusal: 5 passes → 5 store WARNs beside the trader's single once-per-change WARN. The store is process-shared (`{db}` only) and knows no plan/version/scenario/instance, so it cannot apply the once-per-change rule.
**Shape.** A refusal logged where it is DETECTED rather than where it is OWNED; the layer that detects it re-detects it every pass and has no key to dedupe on.
**Fixed in W1b E7:** the store refuses by type only, silently; the typed error carries every field the WARN printed (row, scenario, leg, state, both redacted keys). The trader-side `armSourceRefused` stays the single WARN + counter per change; the shadow path (`trader/armed_executor.go` ~:519) and the API seams (~:2808, ~:2865) log or return the error. The W5 R13 class above is corrected to match.
**Probe:** `grep -n 'logger\.\(Warn\|Error\)' store/*.go` beside a `return fmt.Errorf("%w` of a typed refusal → the log belongs to the caller that owns the dedupe key. Pinned: `TestArmSourceMismatchWarnsOncePerChange` (5 passes → 0 store lines, 1 loop WARN naming the row and both keys, counter 1).
**Owed:** the shadow path still WARNs "shadowed arm write failed" once per pass and runs `SetState(…"shadowed")` before the ownership refusal (`trader/armed_executor.go` ~:508-521) — a later armed_executor.go wave with a MAPCHECK re-pin.

## CLASS 223 — a price-similarity lineage with no time bound adopts a stale identity while exact signal evidence sits unread

**Found:** 2026-09-23, Wave 1b E15 (map overlay probes 1–2; verifier defects 1–3) [A]. Reconcile's untracked materialization recovered a position's identity only through `StampArmedLineageIfMatched`: any FILLED arm of this trader within ±1 tick, with no time bound. A late CLASS-160 AI fill therefore landed anonymous, or took the plan and signal of an arm filled 72 h earlier; move_stop and trailing then addressed a dead signal — while the fill ring held that fill's exact `SignalID`.
**Shape.** An identity recovered by similarity (price, side) when the process holds exact evidence (a signal-keyed ring or row) that nobody reads first.
**Fixed in W1b E15 and its repairs:** `lateEntryFillFor` (`trader/ninjatrader/reconcile_late_fill.go`) reads the ring FIRST: same-side entry fills with a signal id, same instrument root and account, fill time ≥ firstSeen − `lateEntryFillWindowMs` (`entryConfirmGraceMs` 45 s + `untrackedGraceMs` 60 s); a signal already used as ANY position's `entry_order_id` is excluded (`PositionStore.EntryOrderIDInUse`). Exactly one candidate: this trader's arm (`FindBySignal`) → lineage stamped from THAT arm; else this trader's `open_<side>` order row, claimed only while `Status == "NEW"` (`tagLateEntryFill`), settled FILLED at the ring's price and quantity, and its `trader_fills` row written through `CreateFill` (`lateEntryFillRow`: the `recordOrderFill` fields, `ExchangeTradeID` `nt8-late-entry-<order id>` so `CreateFill` dedupes it, `CreatedAt` = the ring's fill time; since W1b FOLD-13 the status and the fill row are ONE transaction written BEFORE the tag — see the class "an identity tagged before the record it names is settled" below; the first E15 build tagged first and let a failed fill write only WARN). Any other status, more than one candidate, a read error or an unowned signal → untagged with a WARN, no price guess. No same-side ring evidence → the price-match fallback, bounded to the ring's own window since W1b FOLD-4 (below). `PlanID` stays `UNRESOLVABLE` for AI fills.
**Probe:** for every identity recovery, list the exact evidence the process holds (the ring, signal-keyed rows) and check it is read before any similarity match. Each exact-evidence reader needs (a) a pinned time window, with a test feeding evidence older than the window, and (b) a status gate on the row it adopts — only an unresolved row may be claimed. More than one candidate means "untagged", never "first". Any row the recovery settles must also get the child records the normal path writes. Pinned: `TestLateAIFillMaterializesTaggedWithItsSignal`, `TestLateAIFillNeverAdoptsAnOldArmsLineage`, `TestLateArmedFillStampsItsOwnArmNotTheNewestSamePriceArm`, `TestLateFillAmbiguousAcrossTwoSignalsStaysUntagged`, `TestLateFillExcludesASignalAnotherPositionAlreadyOwns`, `TestLateFillOfAnUnownedSignalStaysUntagged`, `TestLateFillOlderThanTheWindowIsNotEvidence`, `TestLateFillOfANonNewOrderRowStaysUntagged` (FILLED / REJECTED / CANCELED), `TestLateAIFillWritesTheFillRowTheNormalPathWrites`, `TestUntrackedWithNoRingEvidenceKeepsPriceMatchFallback`.
**Known residual at the E15 merge (CLOSED by FOLD-4 below):** after a restart empties the ring, the price-match fallback still had no time bound. The ring's account and instrument-root filters are defence in depth (the router `dispatchToOwner` already routes fills by symbol and account) and survive mutation.

**Extended in W1b FOLD-4 (CTO ruling part 1, P2) — time-bounded AND identity-bounded.** With every ring candidate already in use the verdict was `NoEvidence` and `StampArmedLineageIfMatched` ran (`ListFilled(traderID, 20)`, ±1 tick, ANY age): a fill at an old arm's price adopted that arm's plan and signal, `rememberEntryOrderID` cached a terminal signal and move_stop went to a dead order. Fixed: the untracked materialization's fallback is `stampArmedLineageInWindow` (`trader/ninjatrader/reconcile_late_fill.go` :118-167) — this trader's FILLED arms whose `UpdatedAt` is at or after `firstSeen − lateEntryFillWindowMs` (the ring's own window, :57; no upper bound), judged on the PARSED instant (the new `ArmedOrderStore.ListFilledSince` only widens its SQL bound by `LedgerClockSlack`, because `updated_at` is zone-bearing text), newest instant first; and an in-window arm whose signal is already some position's `entry_order_id` (`EntryOrderIDInUse`) is skipped — the time bound alone missed the CTO's own path (every ring candidate in use, a FRESH arm re-adopted). A read failure leaves the row untagged with a 🔗 WARN. The doc comments say "no same-side evidence in the window" (`reconcile_late_fill.go` :35, `reconcile.go` :445). Pinned at `reconcilePositions` (two passes through the materialize wire): `TestPriceMatchFallbackNeverAdoptsYesterdaysArm` (the ruling's RED: "an arm older than the ring's window was adopted: entry_order_id=\"sig-armed-yesterday\" plan_id=\"2026-09-22:NY\" v3 scenario=\"S1\""), `TestPriceMatchFallbackAfterAllRingCandidatesInUseIsTimeBound`, `TestPriceMatchFallbackWindowEdge`, `TestPriceMatchFallbackComparesInstantsNotZoneText` (+14:00 old text that sorts after the bound; −05:00 fresh text that sorts before it), `TestPriceMatchFallbackNeverReusesAnArmWhoseSignalExplainsAnotherPosition`, `TestPriceMatchFallbackSkipsTheInUseArmForAnUnclaimedOne`; store `TestListFilledSinceIsTraderScopedAndZoneSafe`. RED on the old matcher, on the parsed-time check removed, on the zone-text compare, on the in-use skip removed and on the SQL slack removed.
**Known residuals after FOLD-4 (OWED, not ruled):** (1) [A, P1 per the FOLD-4 verifier] **the boot lineage repair undoes FOLD-4:** `RepairArmedLineage` (`trader/ninjatrader/reconcile.go` :619) runs once per TCPTrader from `StartPositionReconcile` (:86) — every boot and trader reload — over `ListUnlinked` (any `plan_version = 0` row, UNRESOLVABLE materializations included) and still calls the unbounded `StampArmedLineageIfMatched` (:544: `ListFilled(20)`, ±1 tick, any age, no in-use check). Verifier probe: a row FOLD-4 left untagged got `entry_order_id="sig-armed-yesterday" plan="2026-09-22:NY" v3 S1` from one repair call, and an in-use arm's signal ended up on two rows. Proposed rule (fail-closed): the repair matches only arms with `UpdatedAt ≥ EntryTime − untrackedGraceMs − lateEntryFillWindowMs` and applies the same in-use skip (it can reuse `stampArmedLineageInWindow`); [B] also limit it to `Source = "reconcile"` rows. (2) `updated_at` is the last write, not the fill time: a later write can freshen an old filled arm (`filled_at_ms` is written only for policy rows). (3) [B] the in-use skip trade-off: a phantom-closed row whose position NT8 still holds, re-materialized, now stays untagged where it used to get the live arm. (4) The `🔗 attribution: materialized … with NO recoverable lineage` WARN (`reconcile.go` :431) still prints before attribution, so it can contradict the 🧩 line two lines later (log only). (5) [C] `LedgerClockSlack` (24 h) cannot cover a +14:00 writer against a −12:00 reader (26 h); every realistic reader zone is covered.

## CLASS 224 — a running-process registry used as the scope of a ledger question

**Found:** 2026-09-23, Wave 1b E10, and the W0b pin `TestReconcileFreshFillOfAStoppedTraderIsNotSeen` (which pinned the limit and said "update this test"); verifier defects 4 and 6 [A]. The pre-open reconcile's check (iii) — "did any producer fill on this account in the last 120 s?" — walked `runningTraderIDs()`. A trader stopped between its fill and our open dropped out, and its position was flattened as an orphan. (ii) was scoped by owner id rather than by account, so another trader's OPEN row never explained the position. Read errors were swallowed by `err == nil` guards and fell through to "flatten". And a SQL time bound on zone-bearing text (`updated_at`) compares lexically, so a fresh row written in −05:00 sorts before a UTC bound and is dropped.
**Shape.** A question about the LEDGER (what filled, what is open, on this account) answered from a registry of live processes or from the caller's own id; and a read failure that lands on the destructive branch.
**Fixed in W1b E10 and its repairs (`trader/reconcile_owned.go`):** (iii) reads `ListFilledSinceAllTraders` and `PictureHtfFilledSinceAll` (store; the SQL bound widened by `LedgerClockSlack` = 24 h, the exact 120 s window re-checked in Go on the parsed time) filtered by account, side and root. New (ii′): an OPEN `trader_positions` row of ANOTHER trader, running or not, on this account, instrument and side explains the held position (a row with an empty account counts only when `onAccount(traderID)` is true), with the 🧷 WARN naming the row on every attempted open. EVERY read error in `ledgerExplainsPosition` counts as explained (`ledger unreadable (<what>: <err>) — fail-closed`): the AI entry is refused and nothing is flattened. `at.store == nil` still returns not-explained (no store is not a read error).
**Named availability change (ii′):** a STALE OPEN row of another trader on this account/instrument/side — including one with an empty account whose trader is unknown or deleted — blocks every AI open on that side for as long as NT8 holds a position there, until the row is closed. Fail-closed by design; the 🧷 WARN names the row. The 09-23 17:30 backup copy had 0 OPEN rows [A: verifier], so nothing is blocked today.
**Known limit (L2, CTO ruling, Wave 1b folds — named, not folded):** a STALE OPEN row of another trader (its NT8 stop fired while that trader was stopped, so the close was never recorded) explains ANY position NT8 later holds on that account, instrument and side. While it stands, the owner-ruled orphan flatten is disabled there — a real orphan on that side is never flattened — and every AI open on the account and instrument is refused at reconcile-before-open (`reconcileBeforeOpenNT` refuses on `ledgerExplainsPosition` before its flatten branch, `trader/auto_trader_orders.go` :293-296), until the row is closed. The 🧷 WARN names the row. **OWED:** a stale-OPEN-row detector (an OPEN row whose account/instrument/side NT8 has reported flat since the row's entry).
**Extended by W1b FOLD-11:** the (iii) freshness window no longer drops a row stamped after the captured `now` — see the class "a freshness window measured against a `now` captured before its reads" below.
**Probe:** for every check that answers a ledger question, ask what the data is keyed by: process liveness, or the caller's own id when the question is about an account, is a finding. For every read in the check, make that ONE read fail alone (rename a column only it needs) and assert refusal, never action. For every SQL time bound on a text column, write a row in another zone and assert it comes back. Pinned: `TestReconcileFreshFillOfAStoppedTraderExplainsThePosition` (replaces the W0b limit pin), `TestReconcileAnotherTradersOpenRowExplainsThePosition`, `TestReconcileOldFillOfAStoppedTraderStillFlattens`, `TestReconcileAnotherAccountsOpenRowDoesNotExplain`, `TestReconcileLedgerReadErrorIsExplainedNeverFlattened` (armed_orders / picture_htf_opportunities / trader_positions), `TestReconcileLaterLedgerReadErrorIsExplainedNeverFlattened` (the four later reads), `TestLedgerClockSlackReturnsACrossZoneFreshFill`, `TestListFilledSinceAllTradersIsLedgerWide`, `TestPictureHtfFilledSinceAllIsLedgerWide`. Note [A]: the armed filled-since guard's RED is on the wording only — with it removed, the per-id `ListFilled` read (a superset of its columns) still fails closed behind it.

## CLASS 225 — a floor judged on continuous prices, sent on the tick grid by a side-blind rounding

**Found:** 2026-09-23, Wave 1b E12(a) [A]. `RoundToTick` (`math.Round`) was the only rounding on the entry wire. A stop composed exactly on the min-SL floor (entry − 1.5×ATR5m, off the grid) went out half a tick inside it: 29575.90 → 29576.00, a broker distance of 24.00 against a 24.10 floor. EntryGate legs 5/6 judged the unrounded prices, so the gate approved one order and the broker received a tighter one. The away-rounded (wider) stop can also drop R:R up to a tick under its floor, and an off-grid entry that rounds toward the stop shortens the distance the same way (#191). Two traps were found in the first fix: (1) it rounded EVERY venue on the NT8 grid — a non-CME symbol fell to the 0.25 default, DOGEUSDT entry 0.12 rounded to 0, and legs 5/6 skipped an R:R 0.10 open that base refused (fail-open); (2) the gate and the wire keyed the tick table through different root reducers, so a contract-code symbol (`M2KU6`) got the 0.25 default instead of M2K's 0.10.
**Fixed in W1b E12(a) and its repair:** ONE helper, `WireBracket` (`trader/ninjatrader/tick_rounding.go`), rounds entry and target to the nearest tick and the stop AWAY from the entry (`WireStop`: long floor, short ceil, a 1e-6-tick epsilon keeps an on-grid stop on its own grid index — bit-identical to the authored price only on an exact, power-of-two tick such as 0.25 or 1.0; on 0.10 / 0.01 the wire float lands a few ulps off the authored price, `RoundToTick(2000.3, 0.10)` = 2000.3000000000002 (W1b FOLD-7 scoped this claim); an unknown side refuses the send). `placeEntry`, `PlaceLimitEntry` and `PlaceStopEntry` send its output, and EntryGate legs 5/6 judge that same output (`entryGateWirePrices`, `trader/entry_gate.go`), so a rounding that crosses a floor is REFUSED, not sent; a refusal caused by rounding appends "— judged on the wire-rounded prices (authored entry … SL … TP …)" — since W1b FOLD-7 only when rounding moved a price by more than `wireTickEps` of a tick (`ntTrader.WireMoved`, `trader/ninjatrader/tick_rounding.go` :71, called at `trader/entry_gate.go` :377); the first build compared with a bare `!=`, so on a non-power-of-two tick an ON-GRID leg's refusal claimed a rounding that moved nothing (M2K 0.10: "R:R 1.00 below floor … — judged on the wire-rounded prices (authored entry 2000.3000 …)"). An on-grid leg on any tick now keeps the pre-E12 refusal text byte-identical; live 0.25 (MNQ/NQ/ES/MES) is unaffected. The gate rounds ONLY a CME futures symbol (`market.IsCMEFuturesSymbol` or a non-empty `market.FuturesRoot`); every other venue is judged on the authored prices, exactly as before. The tick comes from ONE lookup, `InstrumentTickSize`, which resolves the root through `market.FuturesRoot` (the wire calls it on the trader symbol, the gate on the intent symbol; `entryGateTickRoot` is deleted). Named consequence: a contract-code symbol on a non-index root now gets its real tick on every `InstrumentTickSize` caller (`CLZ6` → 0.01, `M2KU6` → 0.10), including `PlaceProtectiveStop` and `MoveStopToBreakeven`; MNQ is unchanged. CTO ruling (Wave 1b E12(a)): stop rounds AWAY from entry, target nearest-tick, R:R judged on the WIRE-ROUNDED prices through one shared helper, fail-closed when rounding drops R:R under the floor, CME-only via the wire's own root/tick resolver.
**Remaining gap (recorded, not fixed):** a stop-entry arm is judged at `leg.Entry` (`entryGateForArm` at `trader/armed_executor.go` ~:794), but the wire sends the trigger `entry ± offset ticks` (`decideStopEntry`, ~:1681-1685), so its R:R is judged up to the offset better than the order sent (min-SL only widens, so that leg is safe). Fix when that file is owned: judge `decideStopEntry(...).Trigger`. Also still unrounded: `armGateVerdictFor`'s min-SL leg (~:2414; the EntryGate binds as the stricter check), the ledger / research / 🛑 stop values (up to one tick from the wire), `kernel` `MinSLVerdict` (pre-admission), and the nearest-rounded management moves (`PlaceProtectiveStop`, `MoveStopToBreakeven`, the deprecated CSV path).
**Probe:** for every gate that compares a price to a floor, name the function that produces the price the wire sends and check the gate calls THAT function, with the SAME tick lookup and the SAME venue scope. Grep the wire for `RoundToTick(` on a stop: a protective stop rounded to nearest is a finding unless it is a management move not judged against a floor. Feed the gate one non-CME symbol and one contract-code symbol and compare against base and against the wire frame. Pinned: `TestWireStopRoundsAwayFromEntryLimit` (+ StopEntry / Market), `TestWireStopTable`, `TestWireRefusesUnknownSideBeforeAnySend`, `TestEntryGateArmRRJudgedOnWireRoundedStop`, `TestEntryGateArmMinSLJudgedOnWireRoundedEntry`, `TestEntryGateDecisionRRJudgedOnWireRoundedStop`, `TestEntryGateArmMinSLFloorStopAdmittedOnWire`, `TestEntryGateNonCMESymbolJudgedOnAuthoredPrices` (the DOGEUSDT case), `TestEntryGateContractCodeSymbolJudgedOnWireTick`, `TestWireTickResolvesContractCodeRoot`, `TestInstrumentTickSizeResolvesRoot`. One fixture moved on purpose: `TestArmSeamATR5mIsTheOneResolver`'s target (at exactly R:R 3.00 the ceil-rounded short stop reads 2.98 and leg 5 refused first).
**W1b FOLD-7 (CTO ruling part 1, P2) — "did rounding move it?" is a tick-epsilon question, never `!=`.** Pinned at the production call sites `entryGateForArm` / `entryGateForDecisionAt` (`trader/entry_gate_wire_note_test.go`): `TestEntryGateArmOnGridNonPow2TickReportsNoWireRounding`, `TestEntryGateArmOnGridNonPow2TickMinSLReportsNoWireRounding`, `TestEntryGateDecisionOnGridNonPow2TickReportsNoWireRounding`, `TestEntryGateArmOffGridNonPow2TickStillReportsWireRounding` (the fix never hides a real move), `TestEntryGateArmOnGridQuarterTickByteIdentical` (live 0.25, text and `Float64bits`); helper `TestWireMovedJudgesMovementWithinTheWireTickEps`. RED on both compiling reverts (the call site back to `entry != in.Entry || …`; `WireMoved`'s body back to `wire != authored`). The note is text only — admission is unchanged, the legs still judge exactly the wire prices. **Probe (added):** wherever a float comparison means "did rounding move this value", compare within the epsilon the rounding itself uses; `!=` agrees only on an exact (power-of-two) grid. **Gap [A, FOLD-7 verifier]:** no test covers the ENTRY or TARGET operand of the note — deleting either `WireMoved` term compiles and passes (the gap predates FOLD-7); fix: two `entryGateForArm` cases on M2KU6, entry-only off-grid and target-only off-grid, asserting the note.
**Known limit L3 (CTO ruling part 1: "state whether decision.StopLoss can be re-composed between admitChain and SetStopLoss") — it cannot [A].** A grep of non-test `trader/` and `kernel/` for any write to a `StopLoss` field, `*decision =` or `*d =` finds one hit, `trader/auto_trader_watcher.go` :134 `th.StopLoss = d.StopLoss` — a copy FROM the decision into a thesis. The decision path in order (HEAD `48707044`): `admitEntry` (`trader/auto_trader_orders.go` :182; the gate reads `Stop: d.StopLoss` at `trader/entry_gate.go` :519 and rounds it with `WireStop`) → `recordPlanCitation` (:203, read-only) → `executeOpen*WithRecord` (:207 / :209) → the pre-entry `SetStopLoss(decision.StopLoss)` (:441 long, :589 short) → `placeEntry` → `WireBracket` (`trader/ninjatrader/tcp_trader.go` :562) → `WireStop` (`tick_rounding.go` :90), the same function the gate used, which depends only on side, stop and tick. The kernel's 0B floor on the decision path is a REFUSAL (`sl_too_tight`, `kernel/engine_position.go` ~:212-250), never a re-composition; `composeArmStop` (`trader/arm_stop_anchor.go` :76) is called only from `trader/armed_executor.go` :555 / :603 and `trader/write_time_feasibility.go` :115 / :145 / :352. The chat path puts the admitted stop by value into its Decision. So gate/wire parity holds on the decision path; the only way a DIFFERENT stop could reach the wire was the shared (symbol, side) map being overwritten between set and send — FOLD-10's interleaving (class below). Source: the FOLD-2 builder's and verifier's L3 answers, re-cited at this HEAD.
At HEAD `c4111476` (FOLD-3 + FOLD-10) the NT8 decision path is: `executeOpen*WithRecord` (`trader/auto_trader_orders.go` :380 / :401) → `executeOpenLong` / `executeOpenShort` (:386 / :407, reconcile-before-open) → `openEntryWithRecord` (:431) → `carrier.OpenWithBracket(…, decision.StopLoss, decision.TakeProfit)` (:581) → `placeEntryWith` (`trader/ninjatrader/tcp_trader.go` :517; the carried stop/target replace the map values it read, :571) → `WireBracket` (:589) → `WireStop`. The pre-entry `SetStopLoss(decision.StopLoss)` (:559) now runs only for a CME entry on a broker that does not carry its bracket (a map-keyed one), and FOLD-10's entry-send section (`lockEntrySend`, :553 → released :585) spans that set → the send on the AI and chat paths. The stop still reaches the wire by value from the admitted Decision, so gate/wire parity holds [A: code read at `c4111476`].

## CLASS 226 — a dedupe slot recorded at admit is consumed by refusals that never reached the wire

**Found:** 2026-09-23, Wave 1b E12(b) [A]. B3 `orderGuard.admit` recorded the key before the ledger registration, the SL/TP precondition and the send. Any refusal after B3 therefore blocked the legitimate retry for 55 s as a "duplicate" of an order the broker never saw. The armed paths also discarded B3's reason and never called `IncGateBlock`, so the panel's "Duplicate order dropped" never showed an armed refusal.
**Fixed in W1b E12(b) (reduced scope, CTO ruling Q3):** `orderGuard.reserve` (check; the key is IN FLIGHT — the same key is refused as a duplicate and reservations count toward the rate breaker, so the check/commit gap is closed fail-closed) is separate from `done(sent)` (commit: `sent` records the key and the action; `!sent` releases and records nothing; `sync.Once`). Each of the three entry functions defers `b3Done(b3Sent)` with `b3Sent = latchSent = sendAttempted(serr)` — the entry latch's own predicate (`ErrEntryHeld` = not sent; a queued send = sent). `TCPTrader.b3Reserve` logs every B3 refusal (⛔ / 🚨) and calls `telemetry.IncGateBlock("", b3_order_dedup | b3_rate_breaker)` on all three paths; the armed errors keep "B3 dupe/rate guard" and append the reason. Named consequence: the breaker now counts SENT actions plus reservations in flight, not admitted ones.
**By design, NOT changed (CTO ruling, W0a rule):** the 60 s entry latch's recent-send clause refuses any re-entry within 60 s of a SEND on the account/symbol, so a stop-out followed by a re-arm inside 60 s is refused. That is W0a's intended rule; the identity-key plumbing (`PlacementIdentity`, the `*As` methods, the `stopEntryPlacer` change) stays PARKED and the latch is not weakened.
**Probe:** for every dedupe or rate structure, check where the key is written relative to the last refusal before the send — a write before a later refusal is a finding. Check every refusal site calls the gate counter. For every "commit only on send" flag, delete its assignment and confirm a real duplicate on THAT path turns a test RED (the market path's flag was first unpinned: deleting it left the whole package green). Pinned: `TestB3UnsentLimitRefusalDoesNotConsumeSlot`, `TestB3UnsentStopEntryRefusalDoesNotConsumeSlot`, `TestB3UnsentMarketRefusalDoesNotConsumeSlot`, `TestB3ArmedDuplicateRefusedNamedAndCounted` (limit + stop-entry), `TestB3MarketDuplicateRefusedAndCounted`, `TestOrderGuard_ReserveCommitsOnlyOnSend`, `TestOrderGuard_ReservationsCountTowardBreaker`.

## CLASS 227 — a fail-open leg on a path that never supplies its input (abstain = permanently off)

**Found:** 2026-09-23, Wave 1b E9 [A]. The agent door (`AdmitManualEntryAt`) built a Decision with stop = target = 0. EntryGate legs 5 and 6 ABSTAIN on a missing stop, so every chat entry skipped R:R and the stop floor — W0b pinned that as the contract (`TestChatEntryIsAdmittedInAdvisoryModeLegs5And6Abstain`, CLASS 166). The NT8 broker then sent whatever SL/TP its per-(symbol, side) maps held from the last AI decision or a breakeven move. Found again in repair: every door error was told as "refused by the admission gate", including a broker send error and an entry that OPENED unprotected; a no-door fallback reachable only by test fakes kept a map-reading send path alive; and the `execute_trade` tool proposed opens with no bracket, asking the owner to confirm an order certain to be refused.
**Shape.** A gate leg that abstains when its input is absent, on a path that can NEVER supply that input — the leg is off for that path forever, and the test suite pins the abstention as behaviour.
**Fixed in W1b E9 and its repair (CTO ruling Q2: refuse a chat/agent-door entry with no explicit stop; flip the pinned test with the reason quoted):** `agentBracketRefusal` (`trader/entry_admission.go`) refuses by name, class `entry_gate`, when the stop, the target, the live price or ATR5m is missing, or when the bracket is on the wrong side of live ("entry_gate: refused: no explicit stop — …(fail-closed)"). `AdmitManualEntryBracketAt` carries the entry's own Stop/Target into the chain, so legs 5 and 6 judge it. `AutoTrader.OpenManualEntry[At]` is the door's ONE send: it admits with the bracket, then on CME futures sets its own SL/TP immediately BEFORE the entry (a failed set refuses; nothing is sent); on other venues it opens then sets, returning `*ManualEntryUnprotected` if the set fails. Errors are typed — `*ManualEntryRefusal` (admission refused, nothing sent), `*ManualEntryUnprotected` (opened, bracket failed), anything else is the broker's — and the agent labels each by its type (`chatEntryError`; `tradeFailureReply` records an unprotected entry as executed and replies "🚨 Trade OPENED but UNPROTECTED"). The door is part of `tradeSelectedTrader`, pinned at compile time by `var _ tradeSelectedTrader = (*trader.AutoTrader)(nil)`; the no-door fallback is deleted. `execute_trade` refuses `open_long` / `open_short` unless both `stop_loss > 0` and `take_profit > 0`, before any pending trade exists.
**Named behaviour, fail-closed:** chat entries currently CANNOT be sent on non-CME venues — `agentBracketRefusal` reads ATR5m through `armSeamATR5m`, which serves only the NT8 BarCache, so an Alpaca or crypto symbol is refused "ATR5m unknown" [A code read]. At the E9 merge the chat's symbol routing did not reach the NT8 door EITHER — this entry then said so [B, map probe] while the guide said a chat entry "can only pass the gate on an NT8 (CME) trader": one commit, two answers (CTO FOLD-5). The CTO verified the cause [A]: `isStockSymbol("MNQ")` was true (1–5 uppercase letters, not a known crypto base), so `resolveTradeExecutionContext` looked for a stock trader and replied "no running stock trader (Alpaca) found — configure one to trade stocks"; no chat entry could be sent on any venue.
**One truth since W1b FOLD-5 (the same story as the guide, `web/src/guide/content/status.ts`):** a CME chat symbol reaches the NT8 door. `isStockSymbol` asks the CME predicate (`isCMEFuturesChatSymbol` = `market.IsCMEFuturesSymbol` or a non-empty `market.FuturesRoot`) BEFORE the letters heuristic, and `resolveCMETrader` (`agent/trade.go`) picks the ONE running NinjaTrader trader whose wire instrument has that root — refused by name when none is running or two or more are (the chat never picks an account). So a chat entry can be sent on an NT8 (CME) trader, and only there: MNQ reaches `OpenManualEntry` [A at `c4111476`: `TestChatMNQEntryReachesTheNT8TraderDoor`, through `resolveTradeExecutionContext` → `executeTradeWith` → `OpenManualEntry` on an NT8 fake]. The "no running stock trader (Alpaca)" reply quoted above is the pre-FOLD-5 behaviour: a CME symbol no longer reaches that branch. E9's own send described above (set the bracket immediately before the entry, then open) is superseded too: since FOLD-2 the door sends through the AI decision's execute path, and since FOLD-3 the NT8 send carries the chat's bracket inside it (`OpenWithBracket`) — classes below. The non-CME open-then-set branch stays unreachable end-to-end and is tested only at `sendManualEntry`. **Still open at the E9 merge — folded in W1b:** the chat's Set→Open and the AI's Set→Open wrote the same broker SL/TP map keys without a shared lock [B at E9; CTO-verified as FOLD-10 [A]] — closed by FOLD-3 (`OpenWithBracket`: the NT8 send carries its own bracket) and FOLD-10 (one entry-send section per trader, taken by the AI and chat paths), classes below. `parseTradeCommand` builds a bracket-less open but has no production caller [A: grep].
**Probe:** for every EntryGate caller, name the input each abstaining leg needs and prove that path can supply it; a path that never can must refuse, not abstain. For every door that can fail in more than one way, check the caller's label comes from an error TYPE, never from the fact that some error occurred. For every interface a production type satisfies, ask whether a branch keyed on a type assertion is reachable only by test fakes. Pinned: `TestChatEntryWithNoStopIsRefused` (the flipped W0b pin), `TestChatEntryBracketIsJudgedByLegs5And6`, `TestChatEntryWithAStopSendsItsOwnBracket` (real TCPTrader + server: the wire carries the chat entry's own SL/TP, never the stale map values), `TestManualEntryDoorErrorsSayWhatHappened`, agent `TestChatEntryWithAStopGoesThroughTheBracketDoor`, `TestChatEntryErrorsSayWhatHappened`, `TestExecuteTradeProposalNeedsItsOwnBracket`.

## CLASS 228 — a flat window enforced only by a cancel: a second trigger re-arms what the first cancelled

**Found:** 2026-09-23, Wave 1b E13 [A]. The T1 force-flat lead [W.Start − 2m, W.Start) (`t1ForceFlatDue`, `t1ForceFlatLead` = 2) and an in-session EOD flat earlier than the last-entry cutoff were enforced only by CANCELS inside runCycle (`enforceT1ForceFlatAt`, `enforceEODFlatAt`). The live-bar event pass (W3 D14) never calls them: it re-minted (D5) a T1-cancelled placed LEGACY arm as the next placement under the same version, and placed arms authored before the lead; a scan that had nothing to cancel placed first-time arms inside the lead. (The map's first design — an unplaced `armZoneRow` — would have passed at base: MANUAL-CANCEL-WINS keeps an unplaced same-version row terminal, and the D15 pin protects placed market_in_zone rows; the critic re-routed the RED tests.) Found again in repair: the new refusal was counted under `no_trade_band` on the arm path and `session_gate` on the decision path — the counters inferred a class instead of recording it — and the check re-read the T1 calendar, doubling the "calendar FAIL-CLOSED" WARN.
**Shape.** A window whose only enforcement is "cancel what is resting when it opens", on a system with more than one trigger that can author and place.
**Fixed in W1b E13 and its repair:** one predicate, `forceFlatWindowAt(now, t1)` (`trader/auto_trader_clock.go`; Day Plan on and an active session, like the enforce functions), covers the T1 lead and the in-session EOD flat (`eodSessionFlatAt`, now also used by `enforceEODFlatAt` — the flatten behaves the same). `sessionRiskGateAt` reads it BEFORE the loss-run query (which fails OPEN), so the scan, the event pass and the arm/Picture send point all refuse inside the window; decision/agent admission refuses too. Both count it as its OWN class, `force_flat_window` (`asForceFlatWindow`; `admitRefuse(in, "force_flat_window", …)`), and the arm pass's cancel of resting arms keys on `sessionRiskWindowWords`, so its reason reads "force-flat window opened — force_flat_window: …" (the `no_trade_band` text is byte-identical to before). The T1 windows are read ONCE per gate call (`sessionEntryBlockedT1At` hands them to `forceFlatWindowAt`). The GateBlocksPanel label is 'Force-flat window (T1 lead / EOD flat)' (`TestW0bGuideAndGateLabelsMatchTheBinary`). The event pass only refuses; flattening stays with the scan and the monitor. Nothing validates `eod_flat_offset_min ≤ last_entry_offset_min` at save (the gap is refused at entry; blocking a saved config is an owner call); the defaults (last-entry 15, EOD flat 0) make the EOD half inert unless a session override sets it.
**Probe:** for every `cancelArmedOrdersSync` caller that represents a window, name the matching REFUSAL on every trigger (scan, event pass, nudge, send point) and every path. Every new refusal gets its own counter class; grep that no reason text uses another class's prefix. A window check placed after a fail-open read is skipped by that read's failure. Pinned: `TestArmPassRefusesInsideTheT1ForceFlatLead` (route (a): first-time authorization, with a reach control outside the lead), `TestEventPassNeverReMintsAT1CancelledLegacyArmInsideTheLead` (route (b): placed legacy row, T1 cancel settles, the event pass must not mint seq+1), `TestEventPassNeverPlacesInsideTheT1ForceFlatLead`, `TestArmPassRefusesPastAnEarlierInSessionEODFlat`, `TestDecisionPathRefusesInsideTheT1ForceFlatLead`, `TestForceFlatWindowSurvivesALossRunReadFailure` (the only test that detects the ordering), `TestArmPassCancelsAnArmIntoTheForceFlatWindowAsItself`, `TestForceFlatWindowReusesTheSessionGatesCalendarRead`. Test seams named: `clockHoldDriftFn` is set to "no drift" in the fixtures (the F6 2-minute widening happens to equal the lead and would mask it), and the route-(b) test swaps in a fresh TCPTrader to stand in for the wall-clock minute that clears B3's 55 s window.
**Extended by W1b FOLD-12:** the window's cancel sweep re-sent its cancel on every pass; it is now paced per row — see the class "a per-pass sweep that re-sends an unpaced cancel" below.

## CLASS 229 — a pin overdetermined by a second guard

**Found:** 2026-09-24, Wave 1b FOLD-8 (finisher + verifier, lane Claude-101, `fix/executor-owed-1`) [A]. The FOLD-8 comment fix (`8c220b35`) said the "no hole" claim was "pinned by the E1/E2 respec tests' 'place nothing' assertions on the cancelling pass". It was not. With the placement switch widened to `case "armed", "cancel_pending":`, all five respec call-site tests stayed green, because on the cancelling pass the one-contract guard ALSO refuses. The first pin (`7a5be0c8`) over-claimed the other way ("only the placement switch keeps it off the wire"): with the switch alone mutated, the `BeginPlacement` compare-and-set still refuses ("row 1 is no longer eligible for placement"). Corrected in `365aaa92`.
**Shape.** A "nothing happened" assertion on a pass where two guards both refuse pins neither of them. Each test is green for a reason other than the one its name gives, and a mutation of the named guard survives.
**Probe:** for every pin that claims to hold a specific guard, mutate THAT guard alone (compiling, restored with `cmp`) and confirm RED. When guards are stacked, assert on a signal only the named layer controls — a log line that proves the path was reached, a store state only that layer writes — not on the shared outcome "nothing was sent". Then mutate the layers together and confirm the harm the guards exist for actually appears.
**Pinned (the corrected FOLD-8 pin):** `TestRespecCancelPendingRowIsNeverPlacedOnAFlatLiveBook` (`trader/arm_respec_test.go`) — switch alone mutated → RED on the "armed place failed" log line; compare-and-set alone → green (the switch holds); both → RED, the v1 order is re-sent.

**Instance 2026-09-24 M3:** three cases. The update worker's second-Listen test was satisfied by both the flock AND the live-socket probe (`002ede41`, `TestListenRefusesWhileAnotherWorkerHoldsTheLock`); the triage repaired legs that were green for another reason (a trailing-newline leg with a zero key and a fake MAC, a Logf-only MAC check, a "within retention" leg that moved the clock forward); and the red-team's future-iat probe was refused by nbf before Q8 ran and was dropped, so the finding it masked returned later as CLASS 264 (`002ede41`, `0c0253db`)

## CLASS 230 — a log line that states an attribution the code never established

**Found:** 2026-09-23, CTO ruling FOLD-6 (Wave 1b part 1) [A]. `trader/ninjatrader/reconcile.go` printed `🧩 reconcile: MATERIALIZED … manual/NT8-side entry now tracked` unconditionally, two lines after `🔗` had tagged the same position as this trader's own late AI fill. The first repair (`7095ef4c`) still worded failures as answers: a failed store read printed "not claimable by this trader", and a failed fallback read fell through to "manual/NT8-side (no … evidence)".
**Shape.** A summary line prints a default origin whatever the code before it established. A failed read is worded as a negative answer ("not claimable", "no evidence"), which it never established.
**Fixed in W1b FOLD-6 (`7095ef4c`, `07a9d85c`):** `tagLateEntryFill` and `stampArmedLineageInWindow` return what the attribution step ESTABLISHED, and the 🧩 line (`reconcile.go` :475) names it: this trader's late AI fill, its late armed fill, an armed fill matched by price in the window, or `UNTAGGED entry (…)` with the reason, a failed read or write named as a failure ("… lookup failed — see the 🔗 WARN"). "manual/NT8-side entry" prints only when every read succeeded and none found this trader's entry. Pinned at `reconcilePositions`: `TestMaterializedLineNamesTheOriginAttributionEstablished` (7 cases), `TestMaterializedLineNamesAnAttributionThatFailed` (7 cases; failures injected by renaming `armed_orders` or by a `BEFORE UPDATE OF entry_order_id, plan_id` trigger). RED on the old unconditional text, on the "not claimable" text and on the dropped fallback-failure reason.
**Probe:** for every line that summarizes a decision ("X is now tracked as Y"), trace Y back to a value the code set on EVERY branch; a default that survives a failed branch is this class. Inject a failure into each read in turn and assert the line says "failed", never a negative answer.
**Residual [A]:** the `🔗 attribution: materialized … with NO recoverable lineage` WARN (`reconcile.go` :431) still prints before attribution runs, so it can contradict the 🧩 line two lines later. [B] The manual/NT8-side default also covers two unnamed cases: every ring fill already in use, and a same-side in-window arm more than one tick off.

## CLASS 231 — an identity tagged before the record it names is settled

**Found:** 2026-09-24, CTO ruling FOLD-13 (Wave 1b part 2) [A]. `tagLateEntryFill` wrote the position's `entry_order_id` FIRST and settled the AI order afterwards. A failed `UpdateOrderStatus` left the position tagged and the order NEW for good: the untracked branch never re-enters a tracked row, so nothing retried the settle.
**Shape.** A path both stamps an identity on record A and settles record B. It tags first, so a failed settle turns into a permanent orphan: the tagged record has left the set of rows the path re-enters.
**Fixed in W1b FOLD-13 (`9597f8d4`):** settle FIRST, tag LAST (`trader/ninjatrader/reconcile_late_fill.go` :210-228). The settle is ONE unit, `OrderStore.SettleFilledWithFill` (`store/order_settle.go` :12-20): `UpdateOrderStatus(…, "FILLED", …)` and `CreateFill` in one gorm transaction, so either both land or neither does. A failed settle WARNs `🔗 attribution: pos N (…) — AI order #X FILLED settle failed (signal S): <err> — order left NEW, position left UNTAGGED (no tag without a settled order)` and leaves both untouched. No tag is written and no signal is remembered, so move_stop and trailing cannot address a bracket whose order row is unresolved. A tag that fails AFTER the settle leaves the order FILLED with its one fill row (broker truth) and the position untagged, with a 🔗 WARN. The 🧩 line says which ("… its FILLED settle failed (order left NEW) …" / "… (settled FILLED) but the entry-order-id stamp failed …"). Pinned at the production call site, with real SQLite `RAISE(ABORT)` triggers: `TestLateAIFillSettleFailureLeavesOrderNewAndPositionUntagged` (status write fails; fill-row write fails), `TestLateAIFillSettlesBeforeItTags`. RED on the tag-first revert, on a failed settle falling through to the tag, and on a non-atomic settle ("the order row must be untouched (NEW, unfilled) … status=\"FILLED\"").
**Probe:** for every function that writes an identity onto one record and a state change onto another, check the order: the record whose failure must stay visible is written first, as one unit with its child rows, and the tag comes last. Inject a real store failure on each write in turn at the production call site, and assert what each record reads afterwards.
**Residuals (named, not ruled):** (1) [A, FOLD-13 verifier] the 🔗 settle-failure WARN (:220) and tag-failure WARN (:224) are not asserted on their own: the test's substring check also matches the 🧩 line, so deleting the 🔗 WARN stays green. Fix: assert one line carrying "🔗 attribution", the signal, "settle failed" and the injected error text. (2) [B] The settle is not a compare-and-set on `status = 'NEW'`. Another path settling the same order between the NEW read and this settle would be overwritten, and a second fill row added (`nt8-late-entry-<id>`). (3) [B] No periodic sweep reads NEW `trader_orders` rows. The row stays NEW and visible, but nothing brings it back up on its own; the CTO to name the sweep the ruling meant.

## CLASS 232 — a freshness window measured against a `now` captured before its reads

**Found:** 2026-09-24, CTO ruling FOLD-11 (Wave 1b part 2) [A]. In `trader/reconcile_owned.go`, `fresh()` was `d >= 0 && d < window`. The caller captures `now` BEFORE the ledger reads (`ledgerExplainsPosition(symbol, held, time.Now())`, `trader/auto_trader_orders.go` :293). So an armed row the settle pass moved working→filled AFTER that `now` had `d < 0` and was "not fresh". Filled is terminal, so check (i) no longer listed it either. The position was not yet materialized, came out UNEXPLAINED, and was FLATTENED. A sub-millisecond window, and exactly the D10 class that W0(c) exists to close.
**Shape.** A window check written as `0 ≤ age < window` against a clock read before the data. A row written between the clock read and the data read has a negative age; it is fresher than fresh and is read as stale.
**Fixed in W1b FOLD-11 (`410dc62f`):** `fresh := func(ts time.Time) bool { return now.Sub(ts) < window }` (`reconcile_owned.go` :84). The one closure judges the armed filled rows and the Picture filled rows. Pinned at `reconcileBeforeOpenNT`: `TestReconcileFillStampedAfterTheCapturedNowExplainsThePosition` (armed and Picture: "the held position was FLATTENED" on the revert, and on the lower bound put back on either loop alone), `TestReconcileFillStampedAfterNowOffThisAccountOrInstrumentExplainsNothing` (a fill stamped after `now` on another account or instrument still explains nothing; RED under a mutation that takes "after now" as owned before the account check).
**Probe:** grep `>= 0 &&` next to `now.Sub(` (or `d >= 0`, `age >= 0`) in any window check. Find where that `now` is captured: if it is captured before the read, negative ages must pass.
**Residual [B] (FOLD-11 builder C3):** `fresh()` has no lower bound now. A row whose `UpdatedAt` is in the future (a clock step back) reads as fresh until the wall clock passes it. This fails closed: the AI open is refused and nothing is flattened. Any future upper bound must exceed the test's +30 s stamp.

**Instance 2026-09-24 M3:** **CLOSED** in `fix/m3-install-path-lows` `c777235f` (fa `100371ff` cherry-picked): `Consume` now takes a clock function and judges expiry on a reading taken UNDER the seen-store lock; pinned by `TestInstallJudgesExpiryUnderTheSeenStoreLock`.

## CLASS 233 — a per-pass sweep that re-sends an unpaced cancel

**Found:** 2026-09-24, CTO ruling FOLD-12 (Wave 1b part 2) [A]. The E13 / no-trade-band window sweep (`trader/armed_executor.go` :377) re-sent a cancel EVERY pass — the scan AND each live-bar pass — to every non-terminal row, `cancel_pending` ones included: `cancelArmedOrdersSyncWith` filtered only `SignalID == ''`. Each re-send blocked under `armedPassMu` for up to 2× the ack timeout per row and incremented `cancel_attempts` when no ack came. On a dark book, the 2-minute T1 lead × per-bar passes could exhaust the re-request cap (`cancelReRequestMax`) the settlement pass paces by.
**Shape.** A sweep written as "make it so" on every pass, where each attempt has a cost (a blocking wait under a shared mutex, a count against another pass's budget) and the thing it waits on can be dark.
**Fixed in W1b FOLD-12 (`e2fc0c2b`; pins `48707044`):** only the window sweep passes a pace (`at.windowSweepPace(now)`, `trader/window_sweep_pace.go`). It skips a `cancel_pending` row whose latest request is under `windowSweepCancelPace` (30 s, :32) old. "Latest" is the later of the ledger's `cancel_requested_at_ms` and the sweep's own last send, judged on the pass clock. Past 30 s the cancel is re-sent once, so a lost frame is still retried, and the pace restarts from that send. The EOD flat, session end, news and T1 enforce callers pass no pace and keep their behaviour. Pinned: `TestWindowSweepPacesOnlyACancelPendingRow`, `TestWindowSweepPaceCountsAnotherPathsRequest`, `TestWindowSweepReSendsEveryThirtySecondsAcrossALongWindow` (cadence 0 s / 30 s / 60 s …), `TestWindowSweepPaceLeavesTheUnpacedCallersUnchanged`, and the ruling's RED (window open + dark book + 3 passes in 10 s → one cancel per row; "the sweep re-sent 2 cancel frame(s) to a cancel_pending row requested 4s ago"). RED on the call-site revert, on an always-false pace, on the verifier's deletion of the no-link pace check, on first-request-only pacing, on ignoring the ledger time, on pacing every state, and on a 20 s pace.
**Probe:** for every sweep that runs per pass, count the sends per row over N passes inside its pace. For each send, name what it costs (a mutex held, an attempt counted) and who else reads that budget. Separately: a test that "legacy callers are unchanged" must drive each legacy CALL SITE, not the shared helper. The FOLD-12 verifier's V12 (pacing passed at the EOD call site) survived every targeted test, because `TestWindowSweepPaceLeavesTheUnpacedCallersUnchanged` calls `cancelArmedOrdersSync` directly (canon 53).
**Residuals (named, CTO calls):** (1) [A] the pace limits how OFTEN, not how many: over a dark 2-minute T1 lead the sweep still sends at 0/30/60/90/120 s, so `cancel_attempts` reaches the cap of 5 and the settlement pass is capped soon after its timeout. Whether the sweep should respect the cap is open. (2) [A] Re-requests from the settlement pass are not timestamped (`RequestCancel` never updates `cancel_requested_at_ms` after the first request), so the sweep can re-send within 30 s of one. Closing it needs an additive `last_cancel_requested_at_ms` column. (3) [B] Paced skips leave no log line and no counter.

## CLASS 234 — a WIP snapshot taken while a mutation probe runs commits the mutation

**Found:** 2026-09-24, Wave 1b folds (CTO ruling part 2 names `b387f95f`; the fold lane found a second) [A]. An owner hold stopped the builders mid-run, and their uncommitted work was saved as `wip(STOP)` commits. On the f11 fold, `e3bff5d8` captured a mutation probe in flight: `trader/armed_executor.go` had lost the FOLD-12 pace check in the no-broker-link branch of `cancelArmedOrders` (3 lines deleted), and a verifier probe file `trader/zz_verifier_probe_test.go` (154 lines, "temporary, deleted after the run") had been added. The revert is `91dfadc4` (the file restored from the fold commit, the probe deleted, `git diff` against the pre-snapshot fold empty). `b387f95f` (M3, "snapshot at the second interruption") carries the same hazard and must never be cherry-picked alone.
**Shape.** A snapshot captures the tree as it IS at that moment. A probe that mutates a guard to prove its test goes RED is, at that moment, a weakened guard, and the snapshot commits it under a message that says nothing about it.
**Probe:** before building on any `wip(STOP)` / snapshot commit, diff it against the last fold commit and read every REMOVED line in production code; look for `zz_*probe*` files. Restore from the last fold commit, confirm with `cmp`, delete the probe file, and commit the revert as its own commit, naming the snapshot. Squash, or say so in the PR body, so nobody bisects or cherry-picks onto the mutated commit.

**Instance 2026-09-24 M3:** `b387f95f`, already named above, was restored by `a51115ae`; the `ef03e033` wip(STOP) carried the red-team `zz_*` probe files, and the triage verified that no production file was mutated [A] (`a51115ae`, `ef03e033`)

## CLASS 235 — a side door that re-implements the send skips the producer's execute-side rails

**Found:** 2026-09-24, CTO ruling FOLD-2 (Wave 1b part 1) [A at `4f0f2399`]. The E9 chat door `sendManualEntry` (`trader/entry_admission.go`) called `at.trader.OpenLong/OpenShort` directly. It skipped every rail the AI open runs AFTER admission: no `reconcileBeforeOpenNT`, no `enforceMaxPositions`, no same-side check, and no `futuresOrderQuantity(…, resolveMaxContracts())` cap. A chat "buy 3 MNQ" sent 3 against the Stage-A cap of 1; `validateTradeAction`'s USDT arithmetic was the only cap. There was also no `recordAndConfirmOrder`, so no order row: the position appeared untracked as `PlanUnresolvable`, and E15's AI-order branch could never tag it.
**Shape.** The admission chain was shared (CLASS 166), but the SEND was re-implemented. Every rail that lives in the producer's execute function, below the chain, was absent from the door, and each door test was green about its own copy.
**Fixed in W1b FOLD-2 (`349a7351`; repairs `c41293bd`, `77a8bfc5`, `c4111476` — f2 `6de15b84` / `f66aaada` / `7ff03ae6`):** the door sends through the AI decision's own execute path: `sendManualEntry` → `executeOpenLong/Short` (manual) → `openEntryWithRecord`, with a Decision carrying the chat's SL/TP. That runs reconcile-before-open, the max-positions limit and the same-side check. On CME a typed quantity is REFUSED, never clamped (`manualEntryQuantityRefusal`: "refused: quantity N exceeds the max-contracts cap C" / "refused: quantity N is not a whole number of contracts") — the owner typed that number. It writes an order row through `recordAndConfirmOrderAs` under the chat's signal id, never taking the AI's plan citation. A rail refusal is typed `ManualEntryRefusal{Execute: true}`, and the chat replies "entry refused before any send (the same execute-side rails as an AI decision): …". When reconcile had ALREADY submitted an orphan flatten (`reconcileBeforeOpenNTReport` returns `flattenSent` = true from the moment `CloseLong`/`CloseShort` is called), the refusal carries `FlattenSent` and `chatEntryError` replies "a flatten of a position no ledger row explains was SENT first (reconcile-before-open) — it is flat only if the reason below says so, check NT8; the entry was NOT sent: …" — never "nothing sent", and never that the orphan was closed: reconcile also refuses with `FlattenSent` when the flatten submit failed, the feed dropped mid-flatten, or flat was not confirmed within `reconcileFlattenTimeout` (`c4111476`). A broker failure of the ENTRY after an orphan flatten is not a refusal: `sendManualEntry` returns "an orphan flatten was sent first (reconcile-before-open); then the entry failed at the broker: …", which the chat tells as "entry send failed at the broker (not an admission refusal): …". The excursion row opened when the door confirms the chat's fill (`recordPositionChangeAs` → `excursionOnOpen(pos, manual.stop, manual.target, …)`) carries the chat's OWN authored stop/target, never a nearby AI decision's (`StopTargetNear`); with either level missing no row is opened. Pinned at `OpenManualEntryAt` (a fixed admission clock; `OpenManualEntry` itself is wall-clocked): `TestChatEntryOverTheMaxContractsCapIsRefusedNeverClamped`, `TestChatEntryIsReconciledBeforeOpenLikeAnAIEntry`, `TestChatEntryAtMaxPositionsIsRefusedLikeAnAIEntry`, `TestChatEntrySendIsRecordedUnderItsSignalID`, `TestChatPositionExcursionCarriesItsOwnBracketNeverAnAIDecisions`, `TestChatEntryRefusedAfterAnOrphanFlattenSaysTheFlattenWasSent` (both sides: `open_long` over an orphan short, `open_short` over an orphan long), `TestChatEntryBrokerFailureAfterAnOrphanFlattenSaysTheFlattenWasSent`; at `sendManualEntry` (the non-CME branch the admission chain refuses end-to-end): `TestChatNonCMEEntryOnTheNT8VenueIsRefusedBeforeAnySend`, `TestChatEntryOnAnAlreadyHeldSideIsRefusedLikeAnAIEntry`; agent `TestChatEntryExecuteRailRefusalSaysSo`, `TestChatEntryRefusalAfterAnOrphanFlattenSaysTheFlattenWasSent`, `TestChatEntryReplyNeverClaimsAnUnconfirmedFlattenClosedTheOrphan`.
**Probe:** for every door that puts an entry on the wire, name the execute function it shares with the main producer. List each rail of that function (reconcile, position limits, same-side, the quantity cap, the order record) and pin each one AT THE DOOR with the rail tripped. A door that calls the broker method directly is this class, even when its admission is shared.
**Residuals (named):** (1) [B; the bounds A: code read] on NT8 the door blocks its HTTP goroutine for up to 35 s of reconcile (`reconcileFlattenTimeout`, polled every 500 ms) plus ~3 s of confirm polling (500 ms, then up to 5 × 500 ms), plus any wait for another entry's send section (FOLD-10). (2) [A] E15 logs a late chat fill as a "late AI fill": order rows carry nothing that tells a chat order from an AI one (`createOrderRecord`). (3) [B] the hop `executeTradeWith → OpenManualEntry` is covered only by fakes; a clock seam for `OpenManualEntry` would let the literal ruling test run without failing in the CME halt hour. (4) [A: code read] the chat's own levels reach only the excursion row opened when the door CONFIRMS the fill inside that ~3 s poll. A later fill is materialized by the reconciler, which opens no excursion row (`excursionOnOpen` is called only from `recordPositionChangeAs` and `trader/armed_executor.go` :2318), and the flag-gated wave-A backfill (`BackfillExcursions`, `main.go` :472) reads levels through `StopTargetNear` for every closed position it walks, a chat position included — so it would judge that position's path and exit on a nearby AI decision's levels (its `SetLevels` never overwrites a row's non-zero stop). Named, not fixed.

## CLASS 236 — shared state written before a send that can fail, then read as truth

**Found:** 2026-09-24, CTO ruling FOLD-3 (Wave 1b part 1) [A at `4f0f2399`]. The E9 CME chat order wrote the shared (symbol, side) SL/TP maps BEFORE the open. A refused open (the entry latch, B3, an unbound account) left the chat's stop in the map. `MoveStopToBreakeven` reads that map as `cur` for the widen ban (`tcp_trader.go` :919-927 at the ruling's base), so a legitimate breakeven tighten against the REAL stop could be refused as a widen.
**Shape.** A value written ahead of a broker call that can refuse, never undone on the non-sent outcomes, and read later as a fact about the broker.
**Fixed in W1b FOLD-3 (`350e7c38`, pin `a474014e`; f2 `dac9d1c9` / `5db1442d`):** `TCPTrader.OpenWithBracket(symbol, side, qty, stop, target)` carries the bracket INTO the send (`placeEntryWith`: the carried stop/target replace the map values it read, so the maps feed the wire only when no bracket is carried). The maps are written only once the send was attempted (`if own != nil && latchSent`; `sendAttempted` = no error, or an error other than `ntwire.ErrEntryHeld`), so every refusal before the send, and the hold's provably-unsent drop, leave them byte-identical. An AMBIGUOUS send failure keeps the bracket in the maps by design (fail-closed: the entry may be live with exactly that stop). `openEntryWithRecord` uses it on CME through the `bracketCarryingEntrySender` assertion; the Trader interface stays 19 methods. Brokers without it (the CSV Trader, test fakes) keep set-then-open (the CSV Trader has no `MoveStopToBreakeven`, so the harm does not apply there [A]). Pinned: `TestOpenWithBracketRefusedLeavesTheMapsByteIdentical` (no bound account, a non-tradeable account, the maintenance permit, the one entry latch, an incomplete bracket, and the hold's provably-unsent drop), `TestOpenWithBracketSentRecordsItsOwnBracketAndABlockedRepeatDoesNot`, `TestRefusedEntryNeverTurnsABreakevenTightenIntoAWiden` (`trader/ninjatrader/entry_bracket_carry_test.go`), and at the chat door through `EntryBracketMapsForTest`: `TestRefusedChatOpenLeavesTheBracketMapsByteIdentical`, `TestSentChatOpenLeavesItsOwnBracketInTheMaps` (`trader/chat_bracket_maps_test.go`).
**Probe:** for every value written before a broker call, list the call's non-sent outcomes and check the value is undone or never written on each. Prefer passing the value INTO the send over writing shared state ahead of it.
**Residual [C] (FOLD-2 verifier N5):** the maps are written AFTER `SendSignal` (`tcp_trader.go` :628 → :631), so for a sub-millisecond window `MoveStopToBreakeven` can read the previous value (a 0 skips the widen ban, `cur > 0 &&`). SYSTEM-MAP's note (`docs/superpowers/SYSTEM-MAP.md` :332) that `SetStopLoss` "writes a local map a later `placeEntry` reads" now holds only for the legacy `OpenLong`/`OpenShort` → `placeEntry` path; on the carried path `SetStopLoss` still writes the map after the open, and its reader is `MoveStopToBreakeven`.

## CLASS 237 — a shape heuristic asked before the domain predicate

**Found:** 2026-09-24, CTO ruling FOLD-5 (Wave 1b part 1, P1 truth) [A]. `agent/tools.go` `isStockSymbol("MNQ")` returned true: three uppercase letters, not in `knownCryptoSymbols`. So `resolveTradeExecutionContext` skipped the NT8 trader, and every chat MNQ entry was refused "no running stock trader (Alpaca) found". The E9 door was unreachable for its only venue, while the E9 checklist entry and the guide gave two different answers about it in the same commit.
**Shape.** A classifier chain asks a generic SHAPE test ("1–5 uppercase letters = a stock") before the specific domain predicate that owns the symbol. Every symbol of the specific domain that happens to fit the shape is claimed by the wrong branch.
**Fixed in W1b FOLD-5 (`eb746183`, repair `d9d0ef4d`; f2 `820786e8` / `c0dda4c7`):** `isStockSymbol` asks `isCMEFuturesChatSymbol` (`market.IsCMEFuturesSymbol` or a non-empty `market.FuturesRoot`) FIRST. `chatTradeSymbol` canonicalizes a CME chat symbol to its root where it enters (canon 28). `resolveCMETrader` (`agent/trade.go`) picks the ONE running NinjaTrader trader whose `WireSymbol()` has that root; none, or two or more, is refused by name. Named consequence: a CME root that is also a stock ticker (CL, ES, NG) now routes only to NT8 — Colgate cannot be traded through chat, fail-closed. A chat close of a CME symbol now reaches the NT8 trader too (`resolveTradeExecutionContext` routes every action; it used to go to Alpaca). Pinned: `TestChatMNQEntryReachesTheNT8TraderDoor` (execute_trade → confirm → `resolveTradeExecutionContext` → `executeTradeWith` → `OpenManualEntry` on an NT8 fake), `TestChatCMEEntryResolvesOnlyTheOneNT8TraderOfItsInstrument` (MNQ, MNQU6, MNQ.c.0), `TestCMESymbolsAreNeverStocks`, `TestChatCMEResolverSelectsTheNT8TraderThroughTheProductionAdapter` (the production roster adapter; the compile-time `var _ interface{ WireSymbol() string } = (*ntTrader.TCPTrader)(nil)` sits in `agent/trade.go`).
**Probe:** in every classifier chain (symbol → venue, string → type), list the predicates in the order they are asked. A domain-specific predicate must come before any shape heuristic its members could match. Pin it with a call-site test through the RESOLVER, not the classifier alone, and build at least one test roster through the production adapter (a seam that replaces the whole candidate list hid a nil `tradeUnderlying`, verifier m5c).

## CLASS 238 — a set→send pair on shared state with no lock spanning it

**Found:** 2026-09-24, CTO ruling FOLD-10 (Wave 1b part 2, P1) [A at `4f0f2399`]. The (symbol, side) SL/TP maps were set-then-read across goroutines with NO lock spanning set → send: `SetStopLoss` / `SetTakeProfit` then `OpenLong` (`trader/auto_trader_orders.go`, no AutoTrader-level mutex). The AI entry runs on the cycle goroutine and the chat door on the HTTP goroutine, and `placeEntry` reads the maps only after the permit and the latch, milliseconds later. The interleaving C.set(S1,T1) → A.set(S2,T2) → C.send reads (S2,T2) sends the chat quantity with the AI's bracket, or the reverse. CLASS 165 serialized the four entry FUNCTIONS at the broker; this is the finer grain: the set and the send are two calls, and nothing held them together.
**Shape.** A write and the read that consumes it are separate calls on shared state, reachable from two goroutines, and the lock (if any) covers each call but not the pair.
**Fixed in W1b FOLD-10 (`5e49cb14`; f10 `8f7e0a9e`):** ONE AutoTrader-level entry-send section, `entrySendMu`, taken only in `openEntryWithRecord` through `lockEntrySend` (idempotent release, deferred for every early return and panic, and called where the section ends). Section 1 spans the pre-entry bracket set (run only when the broker does not carry the bracket, FOLD-3) → the open send (released as soon as the send returns; the order-confirmation poll runs outside it). Section 2 covers the post-open `SetStopLoss` / `SetTakeProfit`, which on a map-keyed broker would otherwise land inside ANOTHER entry's set → read. Both paths take it: the AI decision's and, since FOLD-2, the chat door's. No other send takes it: armed entries (`PlaceLimitEntry` / `PlaceStopEntry`) and the debug test trade (`DebugPlaceTestTrade` → `OpenWithBracket`) carry their own prices into the send. The Trader interface is unchanged (19 methods). On the live NT8 path, FOLD-3's `OpenWithBracket` already carries each entry's own bracket; the section closes the class for every map-keyed broker (legacy NT8 `placeEntry`, the CSV transport). Pinned: `TestConcurrentAIAndChatEntriesEachSendTheirOwnBracket` (production call sites `executeOpenLongWithRecord` + `OpenManualEntryAt`, a broker that records the maps' (sl, tp) AT OPEN and forces the interleave deterministically, both orders). RED at base ("AI sent (SL 28983.25, TP 29078.25) want (28981.25, 29080.25)") and on both compiling reverts (both sections off; post-open section only) — the builder's record, not re-run in the W1b docs pass.
**Probe:** for every shared map or field that a producer writes and a later call reads (`Set…` then `Open…`), find every goroutine that can reach the pair. If more than one can, one lock must span write → read. Test it by forcing the interleave in a fake that records what the read saw, in both orders — never by a timing loop.

**Instance 2026-09-24 M3:** **OPEN.** The H2 retire check reads `users.updated_at`, then bcrypt runs, then `UpdatePassword` writes by id alone, with no lock spanning the three, so a retired token's in-flight password write can land after the owner's rotation (fh-verify #4 [B]). The route now also needs the current password; it is a PR-body known limit (fh-verify #4)

## CLASS 239 — a test that pins a defect will defend the defect

**Found:** 2026-09-24, WAVE 3a (M4 release workflow) [A]. `TestReleaseWorkflowRefusesWithInstructionsWhenThePublicKeyIsMissing` asserted that `.github/workflows/release.yml` names `deploy/release.pub`. That path WAS the bug — `ssh-keygen -Y verify` reads its `-f` file as allowed-signers, so a bare public key can never verify (CLASS 240). When the fix landed the test went RED, and the red was the pin defending the broken shape, not the fix breaking anything. A green suite does not distinguish "this behaviour is correct" from "this behaviour is what I wrote down"; the pin had been written from the same wrong assumption as the code, in the same hour, by the same author.

**Fixed in 3a:** the assertion moved WITH the fix in the same commit (`53051073`) and now pins `deploy/release_allowed_signers`, carrying a comment at `deploy/release_contract_test.go:122-127` that names the shape it used to pin and why that shape was wrong — so the next reader learns the defect instead of re-deriving it. The correct behaviour is proven independently by `TestReleaseSignatureVerifiesOnlyWithAnAllowedSignersFile` (`deploy/release_contract_test.go:173`), which mints a throwaway keypair and exercises BOTH directions.

**Probe:** when a test goes red because a FIX landed, ask which of the two is wrong before touching either. If the test is, it moves with the fix, in the same commit, with a comment naming the superseded shape. Never adjust a fix to keep a pin green. A pin on a value the author has not independently verified is a pin on the author's own assumption.

**Instance 2026-09-24 M3:** fa's characterization pin `TestEnrollCommentTruthACrashBetweenTheTwoRenames` pinned the pre-belt "the incumbent still works". It flipped WITH the belt and the comment in one commit, as this class prescribes (`a8f5db20`)

## CLASS 240 — a verification step that cannot succeed as written

**Found:** 2026-09-24, WAVE 3a, CTO review of push 2 [A]. The release workflow ran `ssh-keygen -Y verify -f deploy/release.pub -I release`. `-Y verify` parses each line of `-f` as `<principal> <keytype> <base64>`; a `.pub` file's first field is `ssh-ed25519`, so the principal never matches `-I release` and verification ALWAYS fails. The step is named like a signature check, reads like one in review, and can never pass. Its realistic fate is deletion by whoever hits it mid-incident — which removes the guarantee entirely rather than fixing it, and does so under time pressure with no one reviewing.

**Fixed in 3a:** the workflow verifies against a COMMITTED allowed-signers file (`.github/workflows/release.yml:189-190`), refusing with owner instructions when it is absent; `deploy/release/README.md` gives the one-line creation step. `TestReleaseSignatureVerifiesOnlyWithAnAllowedSignersFile` proves both directions with a real generated keypair: the allowed-signers form VERIFIES, the bare-`.pub` form FAILS.

**Probe:** every verification step ships proven in BOTH directions — a real artifact that must pass, and a tampered one that must fail. A step that has only ever been observed failing has not been tested, it has been assumed; a step that has only ever been observed passing may not be looking at anything.

**Instances 2026-09-24 (WAVE 3b-A):** BOTH legs of `activation.Watch` were unpassable against the real machine — `/api/health` returns the SHORT sha and the boot line prints the short rev, while both comparisons used the full 40-hex, which appears ZERO times in the live log. Recorded in full as CLASS 267, because the cause there is not a mis-written step but evidence the system never emits in the form the proof expects.

## CLASS 241 — a parser written against a guessed output format

**Found:** 2026-09-24, WAVE 3a, first real `--dry-run` on the live box [A]. `cutover.sh` read the installed binary's revision with an awk that split on spaces and took `$3`. `go version -m` emits TAB-separated fields — `build\tvcs.revision=<sha>` is TWO fields, not three — so `$3` was empty and the script refused EVERY cutover with "cannot read vcs.revision from the CURRENT binary". Written from memory of what the output looks like; never compared against the command. No syntax check and no unit test would have shown it, because both would have been written from the same memory.

**Fixed in 3a:** `deploy/cutover.sh:82` normalises tabs and scans the fields for a `vcs.revision=` prefix rather than trusting a position, with the failure recorded in the comment above it (`deploy/cutover.sh:77-81`). The same read proves the NEW binary before anything is touched (`deploy/cutover.sh:49-53`).

**Probe:** a parser for another tool's output is written with that tool's REAL output in front of you — `cmd | cat -A` when whitespace decides — and pinned by a test fed the captured bytes. Positional field reads (`$3`, `[2]`, `split()[1]`) are the tell. This is the same shape as every other entry here: a statement ABOUT a tool, written where nothing compares it to the tool.

## CLASS 242 — a process guard that matches its own asker

**Found:** 2026-09-24, WAVE 3a [A]. The load-rule guard `pgrep -f 'go test.*-race'` matches the shell that is RUNNING the pgrep, because the pattern is in that shell's own command line. It reported "race run in flight" on a box with no `go test` running at all, and the rule on that answer is to SKIP — so the guard had been unconditionally skipping and never once guarding. It failed in the direction that looks safe, which is why nothing surfaced it: no run was ever wrongly started, so no symptom appeared. Proven both ways: `pgrep -af 'go test.*-race'` → 1 match (itself), `pgrep -af '[g]o test'` → the real runs only. Exact twin of `pgrep -f nofx-bin` also matching `go version -m nofx-bin`, already fixed once at `deploy/leveltruth-cutover.sh:34-37` by reading `systemctl show -p MainPID --value nofx`: the one call site was fixed, the PATTERN survived and reappeared in a lane's procedure.

**Probe:** a `pgrep -f` pattern either excludes itself (`'[g]o test'`) or is replaced by a positive identification of the target (`systemctl show -p MainPID`, a pidfile, a cgroup). Verify a guard by running it when the condition is KNOWN ABSENT and confirming it says absent — a guard is only trustworthy if its NEGATIVE answer has been observed. Fixing a pattern at one call site does not retire the pattern.

## CLASS 243 — a refusal that is actually an absence

**Found:** 2026-09-24, WAVE 3a [A]. A contract test asserting "this script refuses bad input" passed while the script did not exist: a missing file exits 127, which is non-zero, which the assertion read as a refusal. The test would have stayed GREEN if the entire guarantee had been deleted from the repo — the strongest possible false green, since it survives removal of the thing it tests.

**Fixed in 3a:** `runScript` in `deploy/release_contract_test.go:81` requires the path to exist AND be executable (`st.Mode()&0o111`) before any exit code may be interpreted, and the refusal assertions match the refusal's own MESSAGE, not merely its status.

**Probe:** for every test that asserts a non-zero exit, ask what happens if the subject is deleted. If the test still passes, it is asserting absence. Exit codes are a channel shared by "refused", "crashed", "not found" and "interpreter missing"; a refusal is identified by what it SAYS.

## CLASS 244 — a wrapper masks the exit code it is reporting

**Found:** 2026-09-24, WAVE 3a [A]. The db-compat run was reported as "exit code 0" while the script's own final line read `VERDICT EXIT=1`. `cmd | tee log` yields tee's status, not `cmd`'s. The harness was honest about what it observed; what it observed was the wrong process. Had the verdict line not been printed, a failing rollback proof would have been recorded as a passing one — and the whole point of that job is to be believed.

**Fixed in 3a:** every db-compat and cutover verdict is READ from the output text (`db-compat: PROVEN <old> <-> <new>` / `ROLLBACK OK` / `ROLLBACK FAILED`), never inferred from a wrapper's status, and the scripts print a verdict line a human reads.

**Probe:** any place a status crosses a pipe, a `tee`, a subshell, a timeout wrapper or a CI step boundary, name which process's status survived. Use `set -o pipefail` / `PIPESTATUS` where the code matters — and make the subject print its own verdict, so the truth does not depend on plumbing.

## CLASS 245 — a one-line `local` expands before it assigns

**Found:** 2026-09-24, WAVE 3a, first real run of the db-compat job [A]. `local bin="$1" dir="$2" label="$3" log="$WORK/$label.log"` expands `$label` BEFORE the assignment completes in the same statement: under `set -u` it aborts with "unbound variable", and without `set -u` it silently builds the wrong path. Invisible in review — it reads exactly like working code — and `bash -n` cannot see it, so the script had passed every check it had been given.

**Fixed in 3a:** split into two statements at `deploy/release/db-compat.sh:70-71`, with the failure recorded in the comment above (`:66-69`).

**Probe:** no assignment statement reads a name it assigns in the same statement — `local`, `declare`, `export`, and the same trap in `env A=1 B=$A`. Generalises past shell: a syntax check proves a file parses, never that it runs. A script whose only evidence is `bash -n` has not been tested.

## CLASS 246 — an unpinned installer inside the job that holds the key

**Found:** 2026-09-24, WAVE 3a, CTO review of push 2 [A]. The signing job ran `curl -sSfL <moving-branch>/install.sh | sh … || true` on the runner that LATER holds `RELEASE_SIGNING_KEY`: arbitrary code from a branch anyone upstream can move, executing beside the release private key. Worse, `|| true` meant a FAILED install silently downgraded the secret scan to a deny-list fallback that then PASSED — the control reported success in exactly the case where it had stopped working.

**Fixed in 3a:** a pinned gitleaks version with a verified sha256 before anything executes (`.github/workflows/release.yml:143-156`), every action pinned by full commit sha, and `GITLEAKS_REQUIRED: '1'` in CI (`:161`) so an absent scanner FAILS the job instead of degrading it. The local deny-list fallback remains, with its NOTE, only outside CI.

**Probe:** in any job that touches a signing key or a deploy credential, list every executable it fetches and ask who can change it between now and the next run. `| sh` from a branch, an unpinned action, `latest` — all the same finding. And `|| true` on a security control converts it into decoration: a control that cannot fail cannot protect.

## CLASS 247 — a guard exemption keyed by a function NAME silently expires on a rename

**Found:** 2026-09-24, the Wave 1b full gate at `3f0c831c` [A]. `store/arm_state_source_guard_test.go` `TestArmStateNoRetypedLists` exempts the reviewed broker order-status readers by FUNCTION NAME (its `brokerFunctions` map: `trader/auto_trader_decision.go` → `recordAndConfirmOrder`). W1b FOLD-2 moved the broker-status poll into `recordAndConfirmOrderAs`, and the old name became a thin wrapper. The exemption no longer covered the code it was written for: the guard read the broker's `FILLED`/`CANCELED` as a copied arm-state set and failed twice. Every fold builder's targeted run passed because none ran the store package; only the full gate saw it.

**Shape.** An allow-list keyed by a symbol name inside a static guard. A refactor that moves the reviewed code under a new name silently turns the exemption into a false positive (loud), or, when the old name keeps a different body, into a false negative (silent).

**Fixed in W1b (`99a7c323`):** the exemption names `recordAndConfirmOrderAs`, and nothing else in the guard changed. FOLD-12's pin (`TestWindowSweepPacesOnlyACancelPendingRow`) also retyped a state list; it now calls two named subtests.

**Probe:** for every guard exemption keyed by a name (function, file, type), grep the repo for that name at HEAD. Zero hits, or a hit that is now a thin wrapper, means the exemption has expired. A wave that renames code inside a guarded file must run that guard's package, not only the targeted tests of the files it edited.

**Instance 2026-09-24 M3:** the hold-writer census admitted the not-yet-existing `internal/updaterworker/hold.go` by a bare map entry that nothing proved admits exactly that path; `TestHoldWriterCensusAdmitsTheWorkerOnlyByName` proves it on a synthetic module (`b0749a47`)

## CLASS 248 — a verifier that rejects the artifact the workflow actually produces

**Found:** 2026-09-24, WAVE 3a, while building a binary to exercise `cutover.sh --dry-run` [A]. The script proves a new binary by reading `vcs.revision`/`vcs.modified` back out of it — and a binary built in a linked git worktree carries NO `vcs.*` entries at all. Proven by elimination on one box, one toolchain (go1.25.13), one module, one commit: worktree `go build` → 0 vcs lines; worktree `go build -buildvcs=true` → **rc=0 and still 0 vcs lines** (the flag whose purpose is to make stamping mandatory fails OPEN); clean clone of the same commit `0adaee41` → `vcs.revision=0adaee41…`, `vcs.modified=false`. WORKTREE LAW puts every lane in a worktree, so the wave's central guarantee refused a correct binary for everyone except the deploy lane — and refused it with "it is not the binary for this sha", which accuses the artifact when the cause is the build LOCATION. An operator checks the sha, finds it already correct, and concludes the check is broken. That is how a guard gets deleted at 3am. (A linked worktree's `.git` is a FILE, `gitdir: …/worktrees/<name>` — the same layout fact that broke a `[ -d "$W/.git" ]` guard on 09-10, biting a second time in a different tool.)

**Fixed in 3a:** `deploy/cutover.sh` tests for the zero-stamp case FIRST and refuses with the cause and the cure ("carries NO vcs stamps at all … Go does not stamp a build from a linked git worktree on this toolchain … build from a clean clone or the main tree", with the clone/checkout/build/verify lines); a stamped-but-different binary gets its own distinct message. `deploy/RESTORE.md` and `deploy/release/README.md` both say which refusal means what. Pinned by `TestCutoverDistinguishesAnUnstampedBinaryFromAWrongOne`, which asserts the two messages exist separately and that the wrong-sha wording appears on the wrong-sha path only.

**Probe:** for every verifier, build the artifact the way the TEAM actually builds it and run the verifier on it. A check whose passing case is unreachable from your own procedure is worse than no check: it trains people to bypass checks. And when one refusal can have two causes, give it two messages — a refusal that names the wrong cause sends the reader to fix something that is not broken.

## CLASS 249 — a guard that runs at module scope is not a build-time check

**Found:** 2026-09-24, WAVE 3a, by running the negative case instead of describing it [A]. The build-time `GUIDE_BUILT_REV` guarantee was a `throw` inside a function called at module scope in `web/src/guide/types.ts`, with a comment above it asserting "A PRODUCTION build with the variable missing … FAILS here". It does not. Vite does not EXECUTE the module while building — it substitutes `import.meta.env` and bundles the result — so `VITE_GUIDE_BUILT_REV= npx vite build` exited **0**, and `grep 'must be a 40-hex commit sha' dist/assets/*.js` found the throw compiled INTO the bundle, where it fires at module evaluation on page load. A release cut with a missing rev would have shown the owner a white screen on the trading UI.

**Why it is worse than what it replaced:** the hand-edited constant went stale and MISINFORMED; this shipped an AVAILABILITY failure on the trading UI over a documentation revision, and the code comment, the canon file and the owner-facing guide all asserted the opposite. Second order, it would also have blocked every release: `release.yml` carries a negative-proof step asserting that same build must FAIL, so the step — whose passing case was unreachable — would have failed the release job (CLASS 240).

**Fixed in 3a:** the refusal moved to `web/vite.config.ts` as the `guide-built-rev-is-a-build-input` plugin (`apply: 'build'`, validated in `config()`), which is the only place that can refuse a build. `types.ts` no longer throws: at runtime an unusable value degrades to `'unknown'` — an honest unknowable (A24), never a real-looking sha and never a crash path — and its comment now records WHY the check cannot live there. RED→GREEN at the real call site: before, empty rev → exit 0; after, exit 1 with the message; positive, exit 0 with the sha in the bundle and the throw string gone. `TestGuideRevIsRefusedByTheBUILDNotByAModuleScopeThrow` fails if the gate is deleted or moved back into application code — it is not the proof (it cannot run a build), it is the tripwire.

**Probe:** ask of every "build-time" check whether the build EXECUTES the file it lives in, or only transforms it. Bundlers, transpilers and code generators read and rewrite; they do not run your module. A guard in transformed code reads in review exactly like a build gate and ships as a runtime crash. The enforcement point must be something the build itself runs: a plugin, a prebuild script, a CI step — and the negative case must be RUN, not described.

## CLASS 250 — a required build input proven only NEGATIVELY, with no census of its producers

**Found:** 2026-09-24, WAVE 3a, by CI — the first producer to run [A]. `VITE_GUIDE_BUILT_REV` became a REQUIRED production build input (CLASS 249 moved the refusal into `web/vite.config.ts`). It shipped with a careful NEGATIVE proof — `release.yml` asserts the empty-rev build is refused, and that refusal was demonstrated RED→GREEN — and **no positive proof**: nobody asked who else builds this artifact. Five GitHub checks failed on PR #199 with one root cause, every one of them a production `npm run build` that supplies nothing: `pr-checks.yml` "Build and Type Check", `pr-checks-run.yml` (hidden behind `continue-on-error` as a "⚠️ Failed" line), `docker/Dockerfile.frontend`, and the image builders in `pr-docker-check.yml` / `docker-build.yml`; `docker-compose.yml` builds the same image for anyone running compose.

**The negative proof is what created the confidence.** Demonstrating that a guard REFUSES tells you the guard works; it tells you nothing about how many places now have to satisfy it. A new required input is a new obligation on every producer of the guarded artifact, and that set is a census — not something that can be inferred from the guard.

**A sixth producer nobody listed.** The first version of the pin used `t.Fatalf` and stopped at the FIRST offending file, so its census was itself incomplete — the exact failure being pinned, inside the pin. Fixed to accumulate and report all, it immediately found `.github/workflows/pr-docker-compose-healthcheck.yml`, which never types `npm run build` but runs `docker compose up` and therefore BUILDS the frontend image. It was on no one's list, and the `:?` guard added to compose would have broken it — trading one red check for another.

**Fixed in 3a:** every producer supplies the input (step `env:` from the head sha; `ARG`+`ENV` above the `RUN` in `Dockerfile.frontend`; `build-args` in both image workflows; `args:` in compose using `${VITE_GUIDE_BUILT_REV:?…}` so compose REFUSES loudly rather than building an unstamped guide; job-level `env:` in the healthcheck workflow because `compose down` reads the same file). Pinned by `TestEveryProductionFrontendBuildSuppliesTheGuideRev` + the compose and workflow boundary tests, which scan `.github/workflows/`, `docker/`, `docker-compose*.yml` and `deploy/`, skip comment lines (the pin failed on its own explanatory comments first), and carry a NAMED exemption for `pr-checks-comment.yml` with the reason — an unexplained exemption is how a real producer gets waved through later (CLASS 242). `deploy/release/README.md` lists the producers so the next one added knows the input exists.

**Probe:** when you add a required build input, enumerate every place that produces the artifact — workflows, Dockerfiles, compose, deploy scripts, the manual boot procedure — and make the enumeration a TEST, not a list in a PR. Prove the guard fires (negative) AND that every producer satisfies it (positive); the second is the one that gets skipped, because the first feels like proof. And a census that stops at its first finding is not a census: report all, or you will fix what you found and ship what you did not.

**Instance 2026-09-24 M3:** FOLD-M3-A. `auth/retire.go` rested the credential epoch on "updated_at is moved only by UpdatePassword", and no census of the users table's writers existed. `TestUsersTableWriterCensus` pins the writer set by (file, function, kind) and the comment names it; its walk hole was CLASS 258 (`7e47767d`), and the 11 compiling writer shapes it cannot see are named in its header (`efdf3988`) (`84954734`, `31605a24`)

## CLASS 251 — a check with no census of WHEN IT RUNS

**Found:** 2026-09-24, WAVE 3a [A]. `Test Docker Compose Healthcheck` failed on PR #199. It was not caused by the PR: `gh run list --workflow=pr-docker-compose-healthcheck.yml` returns FOUR runs in the workflow's entire life and **all four are failures** — three on an unrelated branch on 2026-09-13, one here. It has never once been green. The cause is one missing line: its "Create minimal .env for testing" step writes `DATA_ENCRYPTION_KEY` and `JWT_SECRET` and never `RSA_PRIVATE_KEY`, so since that key became mandatory the backend has FATALed at `main.go:60` and restart-looped until the healthcheck timed out. What hid it is the paths filter — `docker-compose.yml`, `docker/Dockerfile.frontend`, the workflow itself — narrow enough that months pass between triggers. A wave that touches none of those never sees it, and the one that does assumes it broke it.

**The inverse of CLASS 250.** There, a guard had no census of its PRODUCERS: many places had to satisfy it and nobody enumerated them. Here, a check has no census of its TRIGGERS: it almost never runs, so its permanent redness is invisible. Both are failures of enumeration, and both stay hidden for the same reason — the event that would reveal them is rare.

**A check that has never been green is not a check.** It is a name in a list. Worse, it teaches every lane that hits it to treat that name as noise, which is exactly the habit that lets a real failure through later.

**Fixed in 3a:** the step mints an EPHEMERAL RSA key per run (`openssl genrsa` into a 600-mode temp file, folded to one line with literal `\n` as `crypto/crypto.go:30` documents, written straight into `.env`, the file removed and the key never echoed — A25), exactly as `deploy/release/db-compat.sh` does for its throwaway boots. Both PEM encodings are accepted by the loader (`ParsePKCS1PrivateKey` / `ParsePKCS8PrivateKey`, `crypto/crypto.go:117-119`), verified by reading the parser rather than assuming the header.

**A never-green check does not stay broken in ONE way — it collects breakages, and each hides the next.** This one had TWO, both introduced by security hardening that was never re-run against it: the mandatory RSA key (above), and the probe `wget --spider http://localhost:8080/api/health`, where busybox resolves `localhost` to `[::1]` while the API has bound `127.0.0.1:8080` (IPv4 only) since the 08-15 P0 fix (`api/server.go:916-925`) [A]. The second was invisible until the first was fixed, because the container never got far enough to be probed. Fixed by using `127.0.0.1` in the compose healthcheck and the workflow's `docker exec` probe — the frontend probe beside it already did [A].

**KNOWN LIMIT, named rather than left implied [A]:** with that loopback bind, compose's published `8080:8080` forwards to the container's `eth0`, which the API never answers — so a `docker compose` deployment is DEAD ON ARRIVAL until the owner opts in with `API_SERVER_HOST=0.0.0.0` inside the container, where the container network is the security boundary (`config/config.go:134-140`). This check therefore proves the backend BOOTS and SERVES ON LOOPBACK; it does NOT prove the published port works, and nothing in this repo currently does [B]. Setting that variable is a security-posture decision and belongs to the owner, not to a CI fix.

**Probe:** for every workflow in `.github/workflows/`, name what triggers it and when it last ran GREEN — `gh run list --workflow=<file>` answers both in one line. A workflow with no green run in its history is broken or vestigial; decide which and act, rather than leaving a red name that everyone learns to ignore. Treat "this check has always been red" as a finding, never as context.

## CLASS 252 — a single-use ledger that forgets by wall clock re-admits a spent id after a clock step-back

**Found:** 2026-09-24 00:46 CT, M3 triage of the stop-snapshot red-team probes (`0c0253db`) [A]. The red team's `TestRT_ClockRollbackAfterPruneReopensReplay` and `TestRTA_ClockRollbackAfterPruneReplaysThroughTheRouter` (in the `ef03e033` wip(STOP)) were kept RED behind `NOFX_M3_OPEN_FINDINGS=1` as `TestConsumeRefusesAReplayAfterAClockRollbackPastRetention` and `TestInstallReplayRefusedAfterAClockStepBackPastRetention`.

The seen-job store pruned an entry once `expires_at < now − SeenRetention` (`internal/updateauth/seen.go`, then :116-119).

The replay:
1. Install A.
2. A later install at ≥ expA+600 s prunes A.
3. The wall clock steps back more than 600 s. Causes: NTP, a WSL2 resync, or chrony `makestep 1 -1` on this box (red-3).
4. POST A again. Expiry, MAC and Consume all pass.

RED at the router: `handler_updates_adversarial_test.go:321: job rollback-job-a0001 admitted twice after a prune + 611s step-back: 422 …, want 409`. That breaks the spec's unconditional "job_id single-use", and it disproved the design note's "pruning cannot re-open a replay" (§4.1).

**Shape.** A ledger whose forgetting is keyed on the wall clock assumes the clock only moves forward. Every test drove it forward, so every test agreed.

**Fixed in M3 (`f0a0c15e`):**
- The store is now v2, `{"v":2,"pruned_through":N,"ids":[…]}`, where N is the largest `expires_at` ever pruned.
- Any grant with `expires_at ≤ N` is refused with `ErrPrunedReplay`, whatever the clock says. It wraps `ErrReplay`, so the API answers 409 and logs a separate WARN.
- CTO ruling 00:53 CDT: "a clock step-back is a fault, refusing until the clock passes the floor is the right fail-closed".
- A v1 store is refused and never reset. That is safe only because M3 never shipped (`2a58cc84`).
- Pins: `TestConsumeRefusesAReplayAfterAClockRollbackPastRetention`, `TestInstallReplayRefusedAfterAClockStepBackPastRetention`, `TestPrunedThroughIsTheLargestPrunedExpiryAndBindsAtAnyClock`.

**OPEN, same shape.** These are not folded, because the CTO ruled LOWs to known limits:
- **Red-3 #2:** an UNUSED code that the server refused as expired is admitted after a step-back, because the watermark covers consumed-then-pruned ids only [A]. A `clock_floor` fix was built on fa `d970b7e0`, which is not on this branch.
- **Built-0 Q4** [B]: a far-forward jump followed by an install lifts the watermark into the future. Every later install then gets 409 until real time catches up.

**Probe:** for every single-use, replay or expiry verdict that consults the wall clock:
- prune, then step the clock BACK past retention, and replay;
- separately, let a code expire, step back, and resend it.

The verdict must not depend on the clock being monotonic. Persist a high-water mark of what the ledger forgot or refused, and refuse at or below it.

## CLASS 253 — a writer that can emit a record its own strict reader refuses (under never-reset, a permanent wedge)

**Found:** 2026-09-24 00:46 CT, M3 triage (`0c0253db`). The red-team probe `TestRT_ConsumedAtNonPositivePoisons` (in `ef03e033`) only logged the problem; the triage made it assert, as `TestConsumeNeverWritesARecordItsReaderRefuses` [A].

What happened:
- `Consume` wrote `ConsumedAt: now.Unix()` unchecked (`seen.go:126` then).
- The strict reader refuses `n <= 0` as corrupt (`strict.go:99`).
- A corrupt store is never reset, by design.

Reproduced through the router with a scratch probe:
1. An install at clock 0 returns 422, and the store records `"consumed_at":0`.
2. The next install at a real clock returns 403 "job-id store refused".
3. Every later install is refused until someone repairs the file by hand.

RED: `adversarial_test.go:262: clock 0: the store no longer accepts a fresh id at a sane clock: updateauth: seen-job store unreadable`.

**Shape.** Writer and reader are each correct alone. The writer never runs the predicate the reader will judge it by. A fail-closed, never-reset policy then turns one bad write into a permanent outage. It is invisible because it cannot be triggered by an attacker and it fails SAFE.

**CTO ruling (00:53 CDT), generalized as the class:** "every record the store WRITES is validated with the SAME predicate its reader applies BEFORE it is persisted (write-through-the-read-validator)".

**Fixed in M3:**
- `f0a0c15e`:
  - `Consume` refuses a clock at or before the epoch with `ErrBadClock`, before it creates the dir, takes the lock or writes.
  - `encodeSeen` runs every store through `parseSeen`.
  - `Enroll` never writes a degenerate key.
- `399ab651` applies the law to the enrollment: `validateAdmin` is the ONE predicate `LoadAdmin` applies to what it reads and `Enroll` applies to what it writes.
- Pins:
  - `TestConsumeAtAClockAtOrBeforeTheEpochRefusesAndTouchesNothing`
  - `TestEncodeSeenRefusesWhatItsReaderRefuses`
  - `TestConsumeNeverWritesARecordItsReaderRefuses`
  - `TestEnrollNeverWritesADegenerateKey`
  - `TestEnrollWritesTheRecordItsReaderAccepts`

**Read beside 120,** its mirror: there a reader re-refused what the writer had accepted, and the law says the reader yields. Here the strict reader is the security boundary and stays; the writer goes through it.

**Probe:** for every store with a strict reader:
- list each field the reader refuses;
- find the writer's path for each field, and confirm the writer calls the reader's validator on the exact bytes it persists;
- drive the writer at its boundaries (a clock ≤ 0, empty, maximum, a constant fill) and read back through the production reader.

## CLASS 254 — a secret checked for its form, never for whether anyone else can know it

**Found:** 2026-09-24. There are two instances of one shape.

**F2** (triage `0c0253db`; the red-team probe `TestRTA_AllZeroDeviceKeyIsAccepted` in `ef03e033`) [A]:
- `LoadDeviceKey` checked the length only (`admin.go:88` then), and `VerifyMAC` checked the key's length only.
- With a 32-byte all-zero `device.key`, mode 0600 and our uid (a zero-filled restore or a sparse copy [C]), anyone holding the admin JWT computes the second factor.
- RED: `handler_updates_adversarial_test.go:340: a MAC under the all-zero device.key = 422 …, want 403`.

**M1** (red-team 1 #3, ~01:2x CT) [A]:
- The /updates gate refused only the loader's default JWT secret.
- A token forged under the `.env.example` placeholder, under the CI workflow literal, or under a 1-byte secret was an admin identity (GET 200, install 422).
- RED: `handler_updates_secret_test.go:123: JWT secret tracked literal at .env.example:35 (len 41): GET /api/updates = 200 …`.

**Shape.** The check asks whether the secret is PRESENT and WELL-FORMED: non-empty, the right length, not the one known default. The property that matters is that nobody else can know it. A constant fill is guessable, and a value committed to a public repository is published. A deny-list of the one public value you remember is a list of one.

**Fixed in M3:**
- `f0a0c15e`: a degenerate key (all bytes equal) is refused by `LoadDeviceKey` (`ErrUnsafe` → uniform 403), by `VerifyMAC` and by `ComputeMAC`. `Enroll` draws from crypto/rand through a `randRead` seam and never writes one.
- `4e1283b3`: `config.JWTSecretUnfitForUpdates` refuses, on `/api/updates*` only, an empty secret, every public literal and anything under 32 bytes. Per the CTO ruling, the loader and its WARN are unchanged.
- Pins:
  - `TestDegenerateDeviceKeyIsRefusedByTheLoaderVerifierAndMinter`
  - `TestInstallRefusesAMACUnderAnAllZeroDeviceKey`
  - `TestUpdatesRefuseEveryPublicPlaceholderAndShortJWTSecret`. It is a census of every literal `JWT_SECRET` in the TRACKED tree (`git ls-files`, never the untracked `.env`). It fails if it stops finding `.env.example` or the CI workflow, and any new 32+-byte literal turns it RED until listed. It is the census that caught CLASS 261 at the merged head.

**Known limits:**
- The list is exact-match. An edited or re-cased placeholder passes (fa-verify [A]).
- The scan needs git, and fails closed without it.

**Probe:**
- For every secret a gate relies on, ask who else can know its value: the repo (census tracked files for the variable's name), a default, a constant fill, or a PID-sized family.
- Refuse the published values by census, not by memory.
- Refuse constant fills at the loader AND at the writer (CLASS 253).

## CLASS 255 — an identity factor a machine credential can re-mint through a credential route

**Found:** the design anticipated it; red-team 1 R1 and red-team 2 #1 proved it on 2026-09-24 ~01:2x CT [A].

**The design** (§4.4, 09-23) named the risk "an owner-only gate a same-user machine credential (bot JWT) passes". F1 answered it: the JWT's user_id AND email must equal admin.json, and the bot token used directly gets 403.

**Both red teams went round F1** (at the production router, and red-2 also through the agent's own `apiCallTool.execute`):
1. `GenerateBotToken` carries the OWNER's user_id.
2. `PUT /api/user/password` took only `new_password`, acted on the JWT's user_id, and returned 200.
3. `POST /api/login` with the owner's email (which is in the agent's prompt) returned a genuine owner token.
4. That token passed F1, F3 and Q8: `GET /api/updates` 200, install 422. The owner was locked out.

`GetAPIDocs` handed the LLM both links of the chain. The CTO verified it live at 662c79bd with the bot running (ruling 1790231205208, 01:26 CDT): fold in M3.

**Shape.** The test asked "does the machine token pass?" and not "what can the machine token MAKE?". A factor is only as strong as the weakest credential that can mint it. An account route with no current-password check let a bearer token re-mint the identity the gate trusts.

**Fixed in M3:**
- `18577f99`: `credentialActorRefusal` requires the token's email to equal the row's email. It runs first on the credential routes.
- `366d024c`: a `scope` claim. `IsMachine` is true for any scope, for the bot's constant email with no scope, and for nil claims. Machine tokens are denied by default on `/user/password`, `/reset-account`, `/telegram*` and `/updates*`.
- `36f94bb5`: the agent's route list drops these routes plus login, register, logout and reset-password.
- `399ab651`: the M3-local belt. admin.json binds HMAC(device.key, password_hash), so any password change un-enrolls until `enroll --replace`.
- `7db37ec9`: `current_password` is required, with the server and the SettingsPage field in ONE commit. A server that demands a field the UI does not send locks the owner out.
- `f58088bb`: `/api/reset-password` (the M3 instance under CLASS 166).
- Pins:
  - `TestBotTokenCannotChangeTheOwnersPassword`
  - `TestAgentToolCannotTakeOverTheOwnersAccount`
  - `TestMachineScopedTokenIsDeniedOnCredentialTelegramAndUpdateRoutes`
  - `TestAgentRouteListOmitsAccountBotConfigAndUpdateRoutes`
  - `TestPasswordChangeUnbindsTheEnrollment`
  - `TestPasswordChangeRequiresTheCurrentPassword`
  - `TestRedTeamChainAtTheProductionRouter`
  - `TestEveryRuledMachineDeniedRouteRefusesMachineTokens`
  - `TestGateJWTTokenPassesTheCutoverGateAndIsDeniedTheMachineDeniedRoutes` (`c3f5d247`: the cutover tool's scoped token keeps `/api/cutover-gate`).

**Known limits** (red-1, outside M3 [A read]):
- The bot binds to the first chat that sends `/start`.
- `/api/onboarding/beginner` returns the wallet key to any token carrying the owner's id.

**Probe:**
- For each factor of a gate, walk every account-management route (password, reset, email, register) with each machine token, and CHAIN the outputs: a login response is a new credential.
- Run the chain at the production router through the machine client's own request builder.
- Read the route list the client is handed; it is the client's map.

## CLASS 256 — a loopback check that a same-box relay satisfies for every remote client

**Found:** 2026-09-24 ~01:2x CT, red-team 2 #2, over real sockets [A].

The /updates gate judged `RemoteAddr` (loopback) and `Host` (a loopback name). Both are properties of the LAST HOP:
- An `httputil.ReverseProxy` on the box's LAN address rewrote Host to the upstream, as nginx's default does, and added X-Forwarded-*.
- A LAN client got GET 200 and install 422.
- `config/config.go:147` and the P0 report both tell operators to put "a firewall/reverse proxy" in front of an off-loopback API.

**Shape.** A locality check certifies the hop it can see. A relay on the same box is a local peer for everyone it relays, so the check certifies the relay. Every test dialled directly, and the positive control (a LAN client straight to a LAN-bound router gets 403) passed.

**Fixed in M3 (`2606f7f4`):**
- The gate refuses any request that carries any of these headers, whatever the value, even empty: Forwarded, any X-Forwarded-*, X-Real-IP, Via, CF-Connecting-IP, True-Client-IP, X-Client-IP, X-Cluster-Client-IP, Fastly-Client-IP, X-Original-Forwarded-For.
- Names are compared case-insensitively, with `_` read as `-`.
- The log names our own constant spelling, never the client's.
- Pins:
  - `TestUpdatesRefuseRequestsCarryingAForwardingHeader`: 13 names × canonical/lower-case/underscore/empty × 5 routes. Control: a look-alike `X-Forwarding-Note` is admitted.
  - `TestUpdatesRefuseAClientRelayedByASameBoxReverseProxy`: real sockets, both Rewrite+SetXForwarded and legacy Director proxies. Control: a direct client is admitted.

**Known limits (named):**
- A relay that adds no header (nginx's default `proxy_pass`, `ssh -L`, socat) cannot be told apart from a local client. Updates are loopback-DIRECT only (runbook `45739f9f`).
- fa-verify [A]: bare `X-Forwarded`, `X-Envoy-External-Address`, `X-Original-Host`, `Client-IP` and similar are still admitted. This is optional hardening.
- Under WSL mirrored networking, every Windows process is a loopback peer [B].

**Probe:**
- For every locality or identity check, name the hop it judges.
- Put a same-box relay (reverse proxy, tunnel) in front of it and dial from elsewhere. A check the relay satisfies for every client is a check on the relay.
- Write the runbook line for the relays that cannot be detected.

## CLASS 257 — a whole-second claim compared with a sub-second column by a strict operator

**Found:** 2026-09-24, red-team 1 #5 [A].

Q8 retires, on /updates, any token issued before the admin's last password change, and it compared `iat < updated_at`:
- A JWT `iat` is whole seconds. golang-jwt v5 has `TimePrecision = time.Second`, and a fractional iat is truncated on parse.
- GORM stored `users.updated_at` with sub-second precision (`…:29.9Z`).

So a token issued 800 ms BEFORE the change, in the same wall second, compared as not-before and was admitted (GET 200). The control, one second older, got 403.

A second copy then drifted: the H2 credential guard's own `issuedBefore` used `<` while Q8 had moved to `<=`. A same-second token was admitted there and refused on /updates (`0f48b52b` message).

**Shape.** Two timestamps of different precision are compared with a strict operator. The truncation window falls to whichever side the operator favours, and here that was the permissive side. Tests set times whole seconds apart, where every operator agrees.

**Fixed in M3:**
- `4df2a429`: iat must be STRICTLY after `updated_at` truncated to the second.
- `0f48b52b` made that THE rule: `auth.RetiredBy` / `IssuedNotAfter` / `CredentialEpoch` in `auth/retire.go`, used by authMiddleware, the credential guard, the bot and Q8.
- The cost, named: a sign-in within the change's own second succeeds, but its session is refused on first use. The guide says exactly that (`06ca207d`), and so does `auth/retire.go` since `efdf3988`.
- Pins:
  - `TestUpdatesQ8RefusesATokenIssuedInTheSameSecondAsThePasswordChange` (row at sec.900: tokens at sec.100 and sec.950 refused, sec+1 admitted; row at sec.000: iat=sec refused)
  - `TestRetiredByIsTheWholeSecondRuleAgainstTheCredentialEpoch`
  - RED on `<=` → `<`: `retire_test.go:33/:38`.

**Probe:**
- Wherever two timestamps meet, find each one's precision at its WRITER: a token claim, a DB column, a log field.
- Compare at the coarser precision and send the tie to the safe side.
- Test at .100 and .950 of the same second, never only whole seconds apart.
- Grep for every copy of the comparison; there must be one.

## CLASS 258 — a census walk that skips a directory NAME at any depth exempts compiled packages

**Found:** 2026-09-24 ~01:2x CT, red-team 4 #1 [A].

Five censuses each `SkipDir`'d any directory NAMED web, node_modules, vendor, .git, .claude, .Codex or .understand-anything, at ANY depth; the import guard also skipped testdata. The five were the hold-writer census, the worker import guard, the update-auth census, the worker-socket literal census and M2's maintenance-setter census.

Go compiles and links `api/web`, `internal/node_modules/x`, `api/.git`, `x/testdata/y`, `_x` and `api/.hidden` like any other package ([A] go1.25.13). A planted `api/web` minter passed `TestUpdateAuthCensus`, and `go list -deps` showed the app linking the worker side through `nofx/api/web`.

**It happened again inside M3.** FOLD-M3-A's users-table writer census (`84954734`) did its own walk and skipped `.*`, `_*` and testdata at any depth. The ha verifier (note A [A]) planted `api/.hidden/w.go`, a raw `UPDATE users`: it was linked (`go list -deps` names `nofx/api/.hidden`), and the census passed. A writer there would move the credential epoch unseen.

**Shape.** A skip list copied from habit ("keep the census off node_modules") reads as hygiene. It encodes a model of which directories the toolchain builds, and nobody compared that model with the toolchain. Even `go list ./...` is not the answer: it never matches `_x` or `testdata`, yet an import still links them.

**Fixed in M3:**
- `053d8618`: ONE walk, `internal/censuswalk`, which skips names only as direct children of the module root.
  - All five censuses call it.
  - `TestWalkCoversEveryPackageTheToolchainLinks` cross-checks the walk against `go list -deps`, run offline.
  - `TestTradingAppLinkageFromTheToolchain` answers the import guard from the toolchain.
- `7e47767d`: the users census moves onto `censuswalk.NonTestGoFiles`, accepted by the CTO at integration (1790242842706).
- `11348765`: **a third time inside M3**, found while drafting this entry. Two auth censuses M3 itself added, `TestOnlyLoginAndRegisterMintUnscopedTokens` (`366d024c`) and `TestServerNeverMintsAFutureIat` (`45e1404a`), skipped `.`, `_` and testdata at ANY depth. The first one's comment said "the way the go tool skips them", which is true of `./...` pattern matching and false of an import. Both now walk with censuswalk. `TestMintCensusesSeeNestedSkipNamedDirs` plants an unscoped `auth.GenerateJWT` and a direct `jwt.NewWithClaims` in every probe dir. It went RED with the old any-depth skip re-imposed ("api/.Codex/unscoped.go … the mint census counts 0 — want 1") [A].
- Pins: every census has a `…SeesNestedSkipNamedDirs` over `censuswalk.NestedProbeDirs`, including `TestUsersWriterCensusSeesNestedSkipNamedDirs` and `TestMintCensusesSeeNestedSkipNamedDirs`; plus `TestWalkSkipsOnlyAtTheModuleRoot` and `TestRootSkipsArePinned`.

**OPEN.** Nine older walks still use their own `SkipDir` and not censuswalk. The fc builder read them as skipping names at any depth [A at 2a58cc84]:
  - `trader/cancel_contract_test.go`
  - `trader/clock_seam_walk_test.go`
  - `kernel/confirm_resolver_test.go`
  - `trader/bars_store_depth_test.go`
  - `trader/ninjatrader/bar_horizon_warn_test.go`
  - `store/knob_method_readers_test.go`
  - `kernel/acceptance_interval_guard_test.go`
  - `trader/wiring_gate_test.go`
  - `branding/test_file_build_suffix_test.go`

The follow-up is to move all nine onto censuswalk.

**Read beside 108** (a source guard that scans nothing).

**Probe:**
- Grep every census for `SkipDir` and name-based skips. A skip must be anchored at the module root.
- Ask the toolchain: every file of every package that `go list -deps` links must lie inside the walk.
- Plant the offender at `api/<skipname>/` and `internal/<skipname>/p`, and require RED.

## CLASS 259 — a security census that judges spellings (names as IT resolves them), not the capability

**Found:** 2026-09-24, in three rounds, each ended by a FAIL [A].

**Round 1** (red-team 3 #1, red-team 4 #2, ~01:2x CT). The update-auth census pinned four NAMES (Enroll, Authorize, ComputeMAC, LoadDeviceKey) and admitted any importer under `internal/updater*`. With every census green, an API file minted a MAC from the raw key with `crypto/hmac` and `"device"+".key"` (422 at the router), a worker file minted through the exported `DeviceKeyPath` and `Message`, and the app could link the minting CLI. Fold M4 (`90ff04d0`) classified every exported identifier, admitted importers exactly, refused `crypto/hmac` beside the updater dir and constant-folded `+`.

**Round 2** (fc verifier FAIL). D1: one `alias` variable per file, so a second import name went unchecked, and V1 minted a MAC that production `VerifyMAC` accepted. D3: the key holder minted with `jwt.SigningMethodHS256.Sign`. Repairs: `eb999df0` resolves every import name; `d8fa81bf` (rule 6) confines the loaded key's VALUE to VerifyMAC, PasswordStillBound and the builtin `clear`.

**Round 3** (cc verifier FAIL). Rule 6 admitted by name: `updateauth := fakeNS{…}` shadowed the import (P2), and a generic receiver type parameter named `clear` made `clear(key)` a conversion (P1). Both minted accepted MACs while the census comment said rule 6 "can only over-report". `88d4af24` and `f6c5fe07` close them; `a3bccf5e` replaces the sentence with a test over 84 cells, go/types as the oracle. The directive round that followed is CLASS 262.

**Shape.** A census used as a SECURITY tripwire lists spellings of a capability, and the next spelling passes. CLASS 168 is this with honest duplicates; here the spelling is chosen. CLASS 113 certifies a name that is present; this certifies one that is absent. The remedy: confine where the protected VALUE flows, and take the oracle for names from the compiler.

**Pins:** `TestUpdateAuthCensusRefusesTheAPISideRawKeyMinter`, `TestUpdateAuthCensusRefusesTheWorkerSideMinter`, `TestUpdateAuthImporterAdmissionIsExact`, `TestWorkerImportGuardRefusesTheMintingCLI`, `TestUpdateAuthCensusResolvesEveryImportName`, `TestUpdateAuthCensusRefusesTheKeyHolderMintingViaJWT`, `TestUpdateAuthLoadedKeyFlowsOnlyIntoVerification`, `TestUpdateAuthKeyFlowRefusesTheVerifierProbesOnTheRealHandler`, `TestUpdateAuthKeyFlowAdmissionMatchesTheCompilersResolution`, `TestUpdateAuthResolutionMatrixIsNamedInTheCensus`.

**Stated limits** [A]: the census is syntactic (a run-time path, key bytes through an interface, a hand-rolled HMAC, a re-bound name in rule 1's fold `4c16dca2` all pass), and the process shares device.key's uid: a belt, never a boundary. **OPEN, same shape:** the hold-writer census matches direct call names and `"hold.json"` only (red-4 #3); the mint census matches only the `GenerateJWT` selector while `auth.JWTSecret` is exported (fh-verify #3); the users-writer census names 11 compiling writer shapes it cannot see (`efdf3988`).

**Probe:** plant the capability in every spelling that compiles (another helper, a second import name, a shadowed name, a receiver type parameter, an equal-power primitive, a directive: CLASS 262) and require RED for each. Confine the value's flow, not the primitive's import. Check any "can only over-report" claim against go/types; never assert it in prose.

## CLASS 260 — two waves each pinned their own side of a wire; nothing ran the call across it

**Found:** 2026-09-24 ~04:4x CT, integrating M3 with M5 (#196, merged via `760eb015`). The CTO ruled it a class [A].

The M3 gate (`updatesRefusal`, `api/handler_updates.go`) refuses any `/api/updates*` request without exactly one `X-NOFX-Update: 1`, with 403 "update header missing or wrong", before it reads the JWT. `web/src/lib/api/updates.ts` (M5 U1/U2) never sent the header [A: grep, no occurrence outside tests]. On the merged tree, the enrolled admin's own Updates page and header badge would have read not-authorized/Unknown forever.

**Shape.** Each wave pinned its own half:
- M5's shape pins mocked the transport.
- M3's pins drove the server with hand-built Go requests.

Both suites were green, and neither contained the call site that crosses the wire. The CTO called it "the canon 53 shape one level up": 53 is parity at one package's call sites; here the two call sites are in two languages and two waves. Ruling 1790243172089: M3 owns the wire contract.

**Fixed in M3** (`eece064d`, gofmt `9a2cf44a`):
- One `UPDATE_HEADERS` object, passed by the four /updates readers and by nothing else.
- `web/src/lib/api/updates.header.test.ts` runs the REAL `httpClient`, interceptors included, over a capturing axios adapter:
  - every /updates request carries the header once, with value "1";
  - `/api/health`, `/api/maintenance` and `/api/installation-gate` do not.
  - RED before the fix: `4 failed | 3 passed — expected [] to deeply equal [ '1' ]`.
- `api/handler_updates_web_header_test.go` `TestWebUpdatesClientSendsTheGatesHeader` is the Go↔TS parity pin. It checks the client's header object against `api.UpdateHeader` + "1", and checks that every `${API_BASE}/updates` call passes it. It goes RED on a TS rename, on one dropped call and on a Go constant rename.

**Known limits:**
- Under the Vite dev proxy (changeOrigin), POSTs read cross-origin, so they get 403 in dev only [B].
- The receipt link is a plain `<a href>` that carries neither the header nor the bearer token. M4/M5 must fetch it through the client.

**Read beside 5** (the far side never emitted what the consumer waits for) **and 53**.

**Probe:** for every contract that crosses a language or wave boundary (a header, a field, a route shape), find ONE test that runs the producer's real client code against the consumer's real rule. If each side has only its own pins (a mocked transport on one side, hand-built requests on the other), the wire is unpinned. Add a parity pin that reads the constant from one side and the call sites from the other.

**Instance 2026-09-24 PR #200 review F1:** the web client typed the install body's `expires_at` as a STRING while `rawUnixSeconds` takes only a bare JSON number, so every UI install would have been 400. Pinned by ONE committed byte string (`web/src/lib/api/testdata/updates-install-body.wire.txt`) that vitest (wire bytes), api (parser, router, and the TS-declared kinds derived from the parser) and updaterbootstrap (the line `Run` prints) all read (`21a22d04`). The same review found an older test mock that invented a 403 body (`'install: MAC mismatch'`, which is a LOG category); the real body is `forbidden` (`0dc7bd2f`).

## CLASS 261 — a literal that is green on each branch and red at the merged head

**Found:** 2026-09-24 04:55 CT, merging dev 710e96aa (#199, WAVE 3a) into M3 (merge `c27a8851`) [A].

M3's M1 census (CLASS 254) went RED at the merged head on all 5 routes:

`handler_updates_secret_test.go:128: JWT secret tracked literal at deploy/release/db-compat.sh:119 (len 40): GET /api/updates = 200 {"enrolled":true,...}, want 403 {"error":"forbidden"}`

db-compat's throwaway boot secret was a literal prefix + `$$` + a literal suffix. The repo is public, so the pattern is published and the values form a PID-sized family.

Each branch was green alone: 3a had no such census, and M3 had no db-compat.sh. The failure exists only on the merged tree.

**Shape.** Two lanes, each correct, each suite complete for its own tree. A tree-wide census on one side meets a new file on the other only at the merge. This is the CLAUDE.md canon "A branch green alone is not green merged" (09-03 boot: a bare time layout that failed only once two green lanes were on one HEAD). Until now that canon had no checklist slot.

**Fixed in M3 (`775b2bcb`):**
- `JWT_SECRET` is generated the way `DATA_ENCRYPTION_KEY` two lines above already was: `openssl rand -base64 48` (`deploy/release/db-compat.sh:121`).
- The public-literal list is NOT widened, so no repo literal is added.
- The cleanup trap still unsets it.
- The file belongs to Claude-103 (3a); the change was announced to the CTO.
- GREEN: `TestUpdatesRefuseEveryPublicPlaceholderAndShortJWTSecret`, `./deploy` (`TestDbCompatShredsItsEphemeralKeysOnExit` unchanged), and `bash -n`.

**Probe:**
- Run the FULL suite at the merged head before calling a merge green.
- When a merge turns a census RED, read it as a finding about the incoming file, not about the census.
- Fix the offender, not the census's allow-list. An allow-list entry for a published secret is a published secret.

**Instance 2026-09-24 PR #200 review folds:** four fold builders each went green on the packages they touched. At the merged head, the users-table writer census (`store/`) went RED on F5's served advice constant, `UPDATE users SET … updated_at=CURRENT_TIMESTAMP …` in `api/handler_user.go`. The fix was to review and list it, because it IS a way to move the credential epoch (the owner runs it by hand), not to weaken the census (`35899dea`). Run every real-tree census at the merged head, not only the touched packages.

## CLASS 262 — a census that reads one compiler directive but not its siblings

**Found:** 2026-09-24, in two verifier rounds on the update-auth census [A].
- **fc verifier D2:** the census parsed with mode 0, which drops comments. `//go:linkname v3ComputeMAC nofx/internal/updateauth.ComputeMAC` needs no import, no selector and no restricted name; a main linking `nofx/api` printed the exact HMAC with the census green. Fixed by `aa7da00a`: parse with comments, and refuse any `//go:linkname` in non-test code (`TestUpdateAuthCensusRefusesLinkname`).
- **The next census-repair verifier FAIL** (the commits title it "verify #3"): rule 5 now read comments, but for `//go:linkname` ONLY. With a random key under `data/updater/`, three probes built, vetted (rc 0) and minted a MAC that production `VerifyMAC` accepted, with `TestUpdateAuthCensus`, the store censuses and censuswalk all green:
  - **N1a:** `//go:embed data/updater/device.key` in the module-root package, minting through golang-jwt HS256;
  - **N1b:** the glob `//go:embed data/upd*r/dev*`, which never spells the file name;
  - **N2:** a cgo preamble with `.incbin "data/upd" "ater/dev" "ice.k" "ey"`.
- That made two of the census header's claims false: "spelled ONLY in paths.go" and "the direct spellings … fail loudly".
- Reach: an embed cannot cross a symlinked directory [A], so only a package whose directory holds the data dir can embed the key; `.incbin` takes `../` and absolute paths [B]. A clean-checkout CI build fails "no matching files", but `deploy/install-autostart.sh` builds in the checkout, where `data/` exists [B].

**Shape.** A syntactic census learned to read ONE compiler directive after a verifier used it, and still treated the rest of the comment channel as prose. Every directive the toolchain acts on at compile time is code: `//go:linkname` binds a symbol; `//go:embed` and a cgo preamble (`.incbin`, `#embed`, `#cgo LDFLAGS`) pull a file into the binary with no literal, import or call for the census to read. Closing the directive that was used, one at a time, is CLASS 259 again, one channel down. And the protected path is not a constant: the data dir is the directory of DB_PATH (`os.Args[1]`, else the environment, else `.env`, else `data/data.db`), anchored on the checkout the service runs in, so a configured data dir moves the boundary the census defends [A read; precedence corrected in `964396ef`].

**Fixed in M3:**
- `f59b848d`: `import "C"` is refused in every non-test file, under any import name and even behind a build tag. The module has no cgo [A: `go list` CgoFiles empty over `./...`].
- `ac947870`: no `//go:embed` at all in the module-root package; in every package, no pattern element that `path.Match`es "updater" or "device.key" (`all:` stripped; bare, quoted and raw forms parsed); an argument list that cannot be parsed is refused. The directive recognizer is a superset of what go/build and cmd/compile accept [A read, go1.25.13].
- `3e368492`: the header's claims are qualified to the Go source the walk reads, and WHAT THIS CANNOT PROVE is regrouped as run time, compile time, build time, by hand and test files.
- `964396ef` (the round's four notes): each pattern element is lower-cased before matching, because cmd/go resolves a literal pattern by Lstat and a case-insensitive filesystem would let `UPDATER/DEVICE.KEY` reach the key [C for the ext4 deploy; fail-closed anyway]. The DB-path precedence comment is corrected, `go.work` is named, and `paths.go`'s "the ONLY place" is qualified.
- Pins, each judged by `updateAuthOffenders` (the function `TestUpdateAuthCensus` runs over the real tree) in a synthetic module:
  - `TestUpdateAuthCensusRefusesCgo`: N2 byte-identical to the verifier's file, plus non-root, aliased and build-tag-excluded forms;
  - `TestUpdateAuthCensusRefusesRootPackageEmbed`: N1a and N1b byte-identical, and each must draw BOTH the root and the pattern offence;
  - `TestUpdateAuthCensusRefusesEmbedPatternsThatCanMatchTheEnrollment`, including `UPDATER/DEVICE.KEY` and `Updater`;
  - control `TestUpdateAuthCensusAdmitsOrdinaryEmbeds` (the real tree's embeds in agent, branding and kernel stay admitted).
- RED before, and RED on a compiling revert of each rule (cgo, root, pattern, unparseable, a widened recognizer), `vet` rc 0 each, restored `cmp`-identical [A: builder and verifier]. Round verdict PASS_WITH_NOTES at the cc worktree head `990d9096` (patch-identical to `f59b848d`/`ac947870`/`3e368492`), notes folded in `964396ef`. At that head the verifier replayed every earlier probe (RT3-1a/b, RT4-1, RT4-2a/b, V1–V3, P1, P2): all still refused [A].

**Still named, not closed** (census header, WHAT THIS CANNOT PROVE) [B]:
- a data dir configured strictly BELOW a package directory, reached by an embed of an ancestor directory (`DB_PATH=kernel/st/x.db` with `//go:embed st`);
- source the walk never opens: an `.s` `#include`, a `.syso`, SWIG, third-party modules, a `go.mod` replace or `go.work` use/replace pointing outside the tree;
- build inputs outside the source: `go generate`, `-toolexec`, `-ldflags -X`, `-overlay`, GOFLAGS/go.env;
- by hand: a key file copied into the tree under another name.

**Probe:** list every directive and non-Go input the toolchain acts on at compile time (for Go: `//go:linkname`, `//go:embed`, a cgo preamble behind `import "C"`, `.s` includes, `.syso`, SWIG). A census that reads source must read every one of them or refuse it outright; "we read comments for X" is a list of one. Plant each in a synthetic module at the census's production function, byte-identical to the probe that found it, and require RED on a compiling revert of each rule. Where the protected path is configurable, derive it the way the binary does and defend every place it can land.

## CLASS 263 — loop-owned state read from a goroutine the loop spawned

**Found:** 2026-09-24, ha verifier defect 3 [A read]. `telegram/bot.go` runBot answers each AI message on a goroutine, and that goroutine read `ident.agents`. The main loop's `ident.refresh()` (at start, on `/start`, before every AI call) reassigns the identity's agents, token, user id and email on every re-mint. The race predates M3 for a change of user; M3's H2 made it routine, because the bot now re-mints after every password change on its own account and at every 24 h expiry. `d637b7e1`'s own message names the race as not fixed there.

The builder found a second path of the same shape while fixing the first [A]. The LLM factory that `refresh` hands to `agent.NewManager` was `func() mcp.AIClient { return newLLMClient(b.st, b.userID) }`. The manager calls it on the per-message goroutine (`Manager.Run → agent.New → getLLM`, then `Agent.Run → getLLM`) while the next refresh writes `b.userID` on the main loop.

RED before the fix, structural [A]:
```
bot.go:196:13: ident.agents — read inside a closure in runBot
bot.go:263:45: b.st — in a closure built by (*botIdentity).refresh
bot.go:263:51: b.userID — in a closure built by (*botIdentity).refresh
```

**Shape.** A loop owns a struct and reassigns its fields, and a goroutine the loop spawns reads those fields: directly, or one call deep through a closure the struct handed to a long-lived consumer. No test without `-race` can see it, and the second path hides inside the consumer's call graph. Read beside CLASS 196, where a comment stood in for a lock until a second trigger arrived; here the second reader is created by the loop itself, once per message.

**Fixed in M3:**
- `570b58d4`: runBot captures `agents := ident.agents` on the main loop BEFORE the `go` statement and passes it in, so the goroutine references no field of `ident`. refresh's LLM factory closes over locals (`st, userID := b.st, b.userID`), never the receiver.
- Structural pins, go/parser over the package's production source, each with vacuity guards:
  - `TestRunBotGoroutinesReadNoBotIdentityField`: the identity variable is found from runBot's own `newBotIdentity(...)` assignment; no closure in runBot reads it, no `go` statement's function reads it, and none hands the pointer over. A selector in the `go` arguments is allowed, since it is evaluated on the loop.
  - `TestBotIdentityClosuresReadNoReceiverField`: no closure built by a `*botIdentity` method reads the receiver.
  - RED on each compiling revert: the goroutine back to `ident.agents.Run`, the factory back to `newLLMClient(b.st, b.userID)`, the pointer handed to the goroutine [A].
- `1591e1c5`: `TestRaceBotRefreshAgainstInFlightManager`, race-tagged (a `//go:build race` / `!race` constant pair; it SKIPs without the detector). The production `refresh` re-mints on every call on the test goroutine while 64 answers run through managers captured before each `go` statement. Vacuity guards: 64 rebuilds, 64 no-model replies.
  - **Run by the CTO in the race slot at `1591e1c5` [A, CTO-run]:** clean, `ok nofx/telegram 1.249s`. With the factory reverted to `newLLMClient(st, b.userID)`: `WARNING: DATA RACE` ×2, "race detected during execution of test", `--- FAIL: TestRaceBotRefreshAgainstInFlightManager (0.26s)`.
  - It cannot drive runBot itself (that needs a live Telegram API); the structural pin covers that half.

**Known limits:**
- **Pin by TYPE, not by name (hc, `a34f467d` → `87963759`).** The ha2 verifier's compiling revert `id := ident; go func(…){ agents := id.agents … }` brought the race back with both name-based pins green [A]. The pins now type-check package telegram (go/types, gc importer over `go list -export -deps`). They flag any value that HOLDS or points INTO a `*botIdentity`, wherever it reaches a closure, a go statement or a method value: a pointer into a field, a captured holder reached through a helper, a method value on a holder, a generic hand-off. `TestBotIdentityPinRulesCatchEveryRoad` runs the same rule code over 45 synthetic roads, 9 of them allowed controls. Two verify rounds found 11 and then 9 compiling escapes [A]. After the second, the CTO ruled the rest NAMED, not chased (1790248662535). The pin is a belt; the boundary is the capture-before-go code.
- **Named escapes** (the pin header): the interface conversion is read in single-value assignment contexts only. A tuple result (`f(g())`, `a, b = g()`, `return g()`), comma-ok, a re-declaring `:=`, `range =` and `panic`/`recover` each convert the identity unseen (hc re-verify T1–T9 [A]).
- A message in flight across a re-mint keeps the manager and token it started with. Its remaining API calls get 401 once the credential event lands, whether or not a refresh has run yet. That is fail-closed and ruled acceptable [A read].
- `/start` → `ident.agents.Reset` → `session.Memory.ResetFull` runs on the main loop while a goroutine may be inside `Agent.Run` on the same memory, and `telegram/session/memory.go` has no mutex [A read; the race itself B]. Named for a later wave (CTO ruling 1790244970032 (h)); untouched here.

**Probe:**
- For every `go` statement, list every identifier its function reads. Any field the spawning loop can reassign is a race: capture the value on the loop and pass it in.
- For every closure a method hands to a consumer that outlives the call (a factory, a callback, a handler), require that it captures locals, never the receiver.
- Pin by TYPE (go/types), not by variable name: an alias must not escape the pin.
- Pair the structural pin with a race-tagged reproduction that skips without the detector, and see it RED on the revert in the race slot before calling it a pin (CLASS 196: a detector can stay green on a real race when nothing drives the interleave).

**Instance 2026-09-24 PR #200 review F4b/F7:** the bot now re-mints when its own token is blacklisted, and a same-second re-mint is re-checked and retried once. Failing closed then had two gaps, both folded. On a USER change it kept acting for the PREVIOUS user; it now acts for nobody (`45197f48`). And the fail-closed pin's token check could never fire, because its starting token came from the same second (`89deaa5b`). Named, not folded: runBot still says "No account found" when an account exists but no admitted token could be minted.

## CLASS 264 — a clock-skew fix judged at one instant, on a token shape the server never mints

**Found:** 2026-09-24, three times, and each time the finding was narrower than it read.
1. **Triage O3** [B]: a token minted while the server clock ran ahead survives a later password change once real time passes its iat. The red-team probe `TestRTA_FutureIatTokenSurvivesPasswordChange` (in `ef03e033`) was DROPPED as broken: its helper set nbf = iat−1m, 59 minutes in the future, so nbf refused the token before Q8 ran (the M3 instance under CLASS 229). Red-1 marked the same residual [B].
2. **ha verifier defect 4** [A]: a token with iat = now+10 min and nbf = now−1 min, minted before a password change, still got `GET /api/my-traders` 200 after it. `RetiredBy(iat=t0+10m, epoch=t0+1m) = false`, and jwt v5 checks iat only when asked (`WithIssuedAt`).
3. **The corrections** [A]:
   - The ha2 builder: the proposed tightening, `WithIssuedAt`, refuses only while `now < iat − leeway` (jwt v5.2.2 `validator.go:198`). At t0+11m the same token validates again and is still not retired. It defers the hole; it does not close it.
   - The ha2 verifier (defect 6) and the hb builder: the probe token has a shape `signToken` never mints. signToken stamps nbf == iat == now (`auth/auth.go`), and nbf is always checked, so a MINTED token with a future iat was already refused by nbf. With the real shape, at t0+2m today's parser says "token is not valid yet", `WithIssuedAt` only adds "token used before issued", and at t0+11m the parser returns `<nil>` with `RetiredBy = false`. The exposure the finding described (a minted future token accepted now) did not exist. The residual (it survives once the clock reaches its iat) does.

**Shape.** One mistake with three faces: a time-based mitigation checked at a single point of its timeline, with inputs that are not the production inputs.
- **Judged at one instant.** A clock-skew fix was checked at the moment of skew. It must be checked across the whole timeline, including after the clock catches up. A fix that only defers is a known limit, not a closure.
- **On a token shape the server never mints.** The probe built its claims by hand. A defect argued on claims the minter never produces misstates the live exposure, and a pin assembled from the probe's shape can be green for a reason the name does not give.
- **A shared leeway widening a second consumer.** jwt v5.2.2 has ONE `leeway` for iat, nbf and exp (`validator.go:177/198/219`). Adding `WithLeeway(60 s)` to forgive iat also admitted a token for 60 s past its exp, while the logout blacklist still dropped each entry AT exp, so a logged-out token came back for that minute. RED at the router: `token_clock_window_test.go:140: a token logged out 30 s past its exp: GET /api/my-traders = 200 [] — want 401`, and 200 on /api/updates. That is CLASS 102 (a fix that rebuilds its defect one layer down) inside this fix. It is kept in this class rather than numbered separately: it was born of and closed in the same commit, and its probe is the same walk over every consumer of the moved window.

**Fixed in M3** (CTO ruling 1790243040753, TIGHTEN, fail-closed):
- `45e1404a`: `strictParser = jwt.NewParser(jwt.WithStrictDecoding(), jwt.WithIssuedAt(), jwt.WithLeeway(ClockLeeway))` with `ClockLeeway = 60 s`. A token whose iat is more than 60 s ahead is 401 on the protected group and the uniform 403 on /api/updates. `BlacklistToken` holds each entry until exp + ClockLeeway. The guide (`web/src/guide/content/updates.ts`) states the cost. RED before: `token_clock_window_test.go:79: iat = now+2min (nbf past): GET /api/my-traders = 200 [] — want 401` (all 5 /updates routes admitted too).
- Why 60 s: the leeway forgives this box's chrony backward steps, 138 since 09-23 20:40 CT with median 1.09 s, max 1.71 s and a median gap of 131 s [A, hb verifier]. Before, nbf == iat refused a just-minted session on ANY backward step. Now a step of 60 s or less costs nothing, and a larger one refuses the sessions signed in during the skipped interval until the clock catches up.
- `b9e2511e`: the census `TestServerNeverMintsAFutureIat` judged the SPELLING (every IssuedAt/NotBefore written `jwt.NewNumericDate(time.Now())`). The hb verifier's compiling mutation `claims.IssuedAt.Time = claims.IssuedAt.Time.Add(30*time.Second)` minted a future iat while the census and the whole auth package stayed green; only an unrelated password-change test caught it [A]. `TestEveryMintEntryPointStampsNowNotTheFuture` mints through `auth.GenerateJWT`, `agent.GenerateBotToken` and `auth.GenerateScopedJWT(gate-jwt)`, decodes each token and asserts that iat is the mint instant, nbf ≈ iat and exp ≈ iat+24h (±1 s). RED under that mutation: `mint_behaviour_test.go:52: auth.GenerateJWT (login/register): minted iat 05:19:21 is not the mint instant …`. That is CLASS 259's shape: a census of how a value is written, not of the value.
- Pins:
  - api `TestFutureIatTokenIsRefusedEverywhere` (iat now+2 min with nbf past; iat = nbf = now+2 min, the minted shape; iat now+30 s admitted; defect 4 end to end), `TestClockLeewayOnExpAndNbfIsBoundedAtSixtySeconds` (30 s admitted, 2 min refused), `TestLoggedOutTokenStaysRevokedThroughTheExpiryLeeway`;
  - auth `TestValidateJWTClockWindow`, `TestBlacklistEntryOutlivesExpByTheLeeway`, `TestServerNeverMintsAFutureIat`, `TestEveryMintEntryPointStampsNowNotTheFuture`.
  - RED on each compiling revert: no `WithIssuedAt`; no `WithLeeway` (the existing `token_retire_global_test.go:114` also goes RED, so the leeway is required); leeway 180 s; blacklist held only to exp; minted iat +30 s; strict decoding removed [A: hb builder M1–M6, hb verifier R1–R6].

**Known limits (named; fail-closed default):**
- A token minted while the clock ran ahead, whose iat still lies after a later password change's epoch once the clock returns, survives that change as soon as the clock reaches its iat. No iat-versus-now check can tell it apart [B].
- Inside the leeway: a token with iat up to +59 s, minted before a change, is admitted right after it (hb verifier: `GET /api/my-traders` 200; /api/updates 403 only because the change un-enrolls) [A].
- `/api/updates` answers its own uniform 403, not 401, and the 60 s exp grace reaches `POST /api/updates/install` (422 at exp+30 s) [A].
- **Proposed follow-up, NOT folded** (a new wave is an owner call): bind the token to the credential rather than to time, with a credential-generation claim (a fingerprint of the password hash, or a users-row counter that `UpdatePassword` bumps) checked in `authMiddleware`. It touches every mint site and the users-table writer census, and signs out every earlier token once.

**Read beside** 252 (a ledger that forgets by wall clock) and 257 (whole-second against sub-second): the M3 clock family. The hb builder also saw one full-suite failure at the same second as a chrony step (04:56:05, 1.34 s) in the unchanged whole-second H2 rule [A for the timing, B for the cause]. Whole-second tests are flaky on a stepping clock; that is a follow-up, not folded.

**Probe:**
- For every clock-based mitigation, assert at three points of the timeline: before the skew, during it, and after the clock catches up. A verdict that flips back once the clock passes a stamped instant is deferred, not fixed.
- Build every probe token with the production minter, or pin the probe's shape AND the minted shape side by side and say which one the fix changes.
- When a parser option widens an accepted window, list every other structure keyed on the same claims (blacklists, caches, TTL sweeps, the UI's own expiry) and pin, at the production router, that each covers the new window.
- Pin minted VALUES by decoding real mints from every entry point, not the spelling of the stamp (CLASS 259).

**Instance 2026-09-24 PR #200 review F6:** a credential epoch in the FUTURE (a clock step-back after a password change) refuses every new sign-in until the clock passes it. That stays fail-closed, and the refusal now says so: "credential epoch is Ns in the future — clock stepped back; sign-in refused until then". The bound is the size of the step. It is pinned at the production router for authMiddleware and, through a gorm hook between the two reads, for the credential guard (`53bc9734`, `fc17a1c9`). The /updates gate's Q8 line has no clock note (a follow-up).

## CLASS 267 — a proof whose evidence the system never emits in the form the proof expects

**Found:** 2026-09-24, WAVE 3b-A, by the read-only live evidence the dispatch required — not by any test [A]. `activation.Watch` proves an activation with two legs: a boot line written after the restart, and `/api/health` reporting the new revision. Both compared against the release's FULL 40-hex sha. The live box emits neither in that form:

- `/api/health` returns the SHORT sha — `{"revision":"662c79bd236f", …}` [A]
- the boot line prints the short rev too — `🔐 BOOT INTEGRITY OK — rev 662c79bd236f · built 2026-09-23T23:45:35Z` — and the full sha appears **zero times** in the live log (`grep -c` = 0) [A]

So BOTH legs of a two-leg proof were unpassable. Every real activation would have watched, failed, and rolled back, and the failure would have read as "the bot did not come up" — sending whoever was on the boot to investigate a process that had started perfectly, while the rollback undid a good release.

**The tests could not catch it, because they shared the defect.** Every unit test passed. They were written by the same author, in the same hour, from the same assumption, using full shas throughout. A test written from the author's belief about a value tests the belief, not the value.

**The rule already existed in this repo.** `kernel/boot_integrity.go` has carried "a prefix match so short SHAs work" since it was written. The knowledge was present and had not been carried across to new code that needed it — CLASS 242's lesson in a different costume.

**Fixed in 3b-A:** `revisionsAgree(reported, expected)` — the REPORTED value may abbreviate the expected one, never the reverse, with a 7-character floor so an abbreviation too short to identify anything is not evidence. `lineNamesRevision` scans a line's whitespace-separated TOKENS rather than substring-matching, so a hex-looking fragment inside another value cannot be mistaken for the revision. Pinned with the REAL artifacts copied off the live box: the actual boot line string, the actual short health value. A third defect fell out of the same run — logs are named by BOOT date, not calendar date (at 08:04 on 09-24 the active file was `nofx_2026-09-23.log`), so `NewestLogPath` now picks the file actually being written rather than building a path from today's date.

**Probe:** before writing any comparison, obtain the evidence from the RUNNING SYSTEM — `curl` the endpoint, `grep` the real log — and pin the test with those captured bytes. A proof is a claim about what the system EMITS; writing it from what you expect the system to emit produces a check that cannot pass and a suite that agrees with you. Ask of every compared value: does it have a short form, a prefix form, a different case, a trailing newline, a unit?

## CLASS 268 — a guard that no dry run reaches is first exercised during the cutover

**Found:** 2026-09-24, WAVE 3b-A, reading `deploy/cutover.sh` v6 while replacing it [A]. v6 read the process start time with `awk '{n=split($0,a," "); print a[22]}' /proc/$p/stat`. Field 2 of that line is the executable name in parentheses and MAY CONTAIN SPACES AND PARENTHESES — `(nofx bin (x))` — which shifts every later field. The identity check could therefore compare the wrong number: refusing a valid restart, or, worse, ACCEPTING a recycled pid, which is the single thing the identity check exists to prevent.

**It had never run.** Every `--dry-run` refuses earlier — at the token gate, or the dist check, or the binary proof — so the identity code sits *after* every exit a rehearsal takes. The rehearsal that exists to make the procedure safe never reached the line that makes it dangerous, and its first execution would have been during a real cutover, on a live trading box, under time pressure.

That is the shape worth naming: a dry run proves the steps it REACHES. Code after the last refusal a rehearsal hits is unexercised no matter how many times the rehearsal is run, and a passing dry run is therefore evidence about a PREFIX of the procedure, not the procedure.

**Fixed in 3b-A:** the parse moved into `internal/activation` and reads from the LAST `)` in the line, pinned by a test whose comm is literally `(nofx bin (x))`. `deploy/cutover.sh` v7 delegates rather than carrying its own copy, so the attended boot and the unattended worker share one implementation and one test suite.

**Probe:** for every procedure with a rehearsal mode, list the steps the rehearsal never reaches and ask what tests them. If the answer is "nothing", they are exercised first in production. Either the rehearsal must reach them (a seam, a fixture, a `--force-through` for the safe parts) or they must be moved into code a unit test can call — the second is usually right, because a step that only a live cutover can exercise is a step nobody can afford to debug.
