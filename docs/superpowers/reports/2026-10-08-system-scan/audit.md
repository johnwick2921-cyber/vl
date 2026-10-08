# CTO system scan — 2026-10-08

Audit window: approximately 08:02–08:11 America/Chicago (CDT). Read-only against the running system; independent source inspection and tests in locked worktrees. This report is an audit, not a deployment approval or a profitability certification.

## Verdict

The system is running, connected to NT8, receiving MNQ candles and has recorded overnight SIM trades. It is **not problem-free**. The highest-priority operational facts are that the latest release is not running and the saved daily dollar-loss limit is disabled. Two source defects were identified: a reference-level logging panic and warning deduplication defeated by dynamic timestamps. The live dispatcher also recorded 74 dropped frames; the resulting strategy impact remains unverified.

Evidence labels: **[A]** directly inspected or executed; **[B]** inference from evidence; **[C]** hypothesis. Priorities below are remediation priorities, not claims of realized trading loss. PROVEN denotes observed behavior; BROKEN denotes a demonstrated defect; UNVERIFIED denotes a check not completed.

## Identity and scope

| Surface | Directly observed identity |
|---|---|
| Running service | `vl`, active, PID 214, started 2026-10-07 23:39:48 CT |
| Executable | `/home/hoang/vl/vl-bin`, inspected through `/proc/214/exe` |
| Running Go build | `7a8b71d9896add0df050612cdb6a46a97721e7af`, `vcs.modified=false`, release #10 |
| Running health revision | `7a8b71d9896a` |
| Local release marker | Same running revision |
| Available `origin/dev` at acceptance | `207759877aa7f8de8ee7291b41de4168616297f3`, release #11 |
| Main checkout source HEAD | `2d2d8b0c51176766bff0b7c580bb085f0bf698db`; this is not the running binary's source revision |
| Repo | `/home/hoang/nofx` resolves to `/home/hoang/vl`; origin is `johnwick2921-cyber/vl` |
| Broker/account | NinjaTrader, `Sim101`; trader `8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265` |
| Strategy row | `a5b7662e-7bf7-49bb-9f09-7efa48f95ac8` |
| AddOn receipt | 2026-10-07 23:40:04 CT: received build `2026-10-04-d1`, expected same, match yes |

[A] The runtime worktree was detached at the executable's exact commit: `/tmp/vl-running-audit-20261008`. The latest-source worktree was cut from freshly fetched dev: `/tmp/vl-audit-20261008`, branch `audit/system-scan-20261008-cto`, empty claim `fe7e79d45`. Both were locked. Main-tree untracked files were left untouched. Production DB connections used SQLite `mode=ro`.

Checklist: `docs/superpowers/AUDIT-CHECKLIST.md`, including pre-audit R1–R10, evidence/provenance and production-call-site rules. Canon inspected: `docs/superpowers/CLAUDE-canon.md`; the old root instructions' `Codex-canon.md` path is absent. Latest relevant source records at the audited base:

- `a7424d22ab8b6e5c58d01e6f438315f5e096dcc6 2026-10-02T21:16:13-05:00 rename-r5 web & docs slice: zero pre-rename names in web/** and docs/**` — tracked canon.
- `628a7b111331038e06af4a7d498ed2047273b56f 2026-10-04T10:38:46-05:00 fix(mentor): HTFAgree is set from the evaluator's 4h+1h triggers — the 20-contract tier is reachable` — checklist.
- `0e73e62f63f9ae741fa956fb2db8dbac7f6eba0d 2026-10-08T00:12:06-05:00 mentor stale-data block: refuse NEW arms while the 1m feed is stale (B4 formula)` — latest Mentor guard.

This was a broad operational and update audit: runtime/build identity, recent logs, read-only trading/config records, candle inventory and chart depth path, latest release diff, source paths behind observed errors, Go/web suites, targeted race checks, backup status and frontend production dependency audit. It was not a line-by-line review of every repository file, a live NT8 GUI inspection, a browser interaction audit, a full penetration test, or a strategy backtest.

## Findings, in priority order

### 1. Latest release is available but not active — P1 operational, PROVEN [A]

The executable and health endpoint both identify release #10, while fetched dev contains release #11. The 14-file update adds Mentor stale-data blocking and fixes the seed-depth boot message. The new guard is called before Mentor arm authoring (`trader/mentor_tick.go:320`) and armed placement (`trader/armed_executor.go:2907`); implementation is `trader/mentor_mode.go:241`, formula entry point `kernel/stale_data.go:99` at the latest reviewed revision.

Implication: an update being merged or available does not mean that running process has the new guard. The latest seed-line correction also is not active: the existing boot log reports one set of current-contract depths at line 66 and another actual seeded set at line 79. This is a reporting inconsistency, not proof that the evaluator was seeded incorrectly.

The observed feed was fresh during checks; this audit did not demonstrate a stale-data entry. The new guard explicitly leaves exits/protection and already-resting orders alone, and returns fail-open for an absent provider/empty cache; do not describe it as complete disconnected-feed protection.

Next: use the existing owner-attended release/cutover process and verify the executable SHA, health revision, stale-data boot line and seed line after activation. No deployment was performed or authorized by this scan.

### 2. The $450 daily-loss setting is not enforced — P1 owner/configuration, PROVEN [A]

Read from the named strategy's `ai_config.risk_control`: `daily_loss_limit_usd=450`, `daily_loss_enabled=false`, `guardrails_enabled=false`. Runtime corroboration is explicit at boot log line 156: `daily=$450[O] DECORATIVE (guardrails master off, daily_loss_enabled off — both must be on)`. Line 82 also reports master OFF.

Production code corroborates the two-switch requirement: `trader/mentor_tick.go:1267–1298` and `trader/desk_facts.go:512` at latest dev. The Mentor dollar-loss check returns without enforcing when the guardrail is not enabled. This is a daily-loss issue, not a request for a new per-trade stop cap. A configured number alone does not protect the account.

The same saved config also disables daily-profit, max-daily-trades, max-contracts and notional-cap switches. These are configuration facts, not automatically implementation bugs. Other session/position/protection rules still exist; this is not evidence that every risk control is off. Owner intent for these switches was not changed. If $450 is intended to be enforced, the existing daily-loss and master switches must be enabled through the supported UI/reload path, then verified in the runtime boot/status evidence. No raw DB writes.

### 3. Valid reference levels trigger a contained nil-pointer panic — P2 code, BROKEN [A]

Observed: log line 6247, 2026-10-08 01:46:51 CT, `level identity recording panic contained: runtime error: invalid memory address or nil pointer dereference`.

Source: `trader/scenario_level_identity.go:18–26`. `stampPlanIdentity` guards only `r.Level`, then dereferences `r.Level.FormedCloseMs` and `r.Level.TF` while logging. Reference IDs intentionally permit an unknown formation timestamp: `kernel/scenario_level_identity.go:30–44`, `:208–264`. The kernel resolves such a reference as a valid candidate; the trader logging layer does not honor that nullable contract.

Reproduction: audit-only `TestAuditReferenceIdentityPanicProbe` used a valid ONH reference with no formation timestamp, called the production `stampPlanIdentity`, verified `Named == 1`, and captured the contained-panic message. It reproduced at the actual running SHA. The relevant files are unchanged in release #11. The probe was removed from the isolated source tree after execution and is preserved alongside this report as text.

Impact: the deferred recover keeps the process alive and the computed identity result is returned, but subsequent logging in that invocation is skipped. This is not proof of an order rejection or process crash. Because the runtime log lacks a stack trace, attributing that exact historical panic to this reproduced path is [B], not [A]; the code defect itself is reproduced [A].

Fix direction: render nullable fields as unknown, without fabricating formation times; add a permanent production-call-site regression test that asserts no panic warning for valid reference identities. Include the corresponding checklist update with the fix.

### 4. Dynamic reason strings defeat warning deduplication — P2 observability/resource use, BROKEN [A]

From the runtime log through line 32627 (last line 08:09:37 CT), `picture_htf_evaluator.go:146` emitted **9,080** first-occurrence warnings, each with a unique reason string. Of these, **6,035** were source-age messages and **3,041** were elapsed-entry-window messages. Four were other reasons.

Source: `trader/picture_htf_evaluator.go:135–148` stores `stage + "|" + reason` in `watchReasons`. Call sites `:627–629` and `:644–647` embed changing millisecond values. Each new numeric value makes a new map key and a new WARN, defeating the explicit first-occurrence design. This also grows the per-evaluator reason map with timing values instead of a bounded set of reason types. The defect remains in latest dev.

Fix direction: stable reason codes for counters/deduplication, timing values as separate message fields, and a regression test across many different ages. Preserve the actual refusal logic. Do not suppress freshness checks merely to remove the warnings.

### 5. Live dispatch dropped 74 frames — P1 investigation, loss PROVEN [A], trading impact UNVERIFIED

At **03:03:51 CT**, log lines **14353, 14355, 14357, 14359, 14361, 14363, 14365, 14367, 14369** report nine ended drop bursts: **1 + 53 + 1 + 3 + 6 + 7 + 1 + 1 + 1 = 74 frames**.

This is the live sink queue, not the candle persistence queue. `provider/ninjatrader/bar_live_sink.go:94–113` runs the sink worker; the burst tracker records drops at `:126–136`. The sink dispatch at `trader/picture_htf_live.go:51–59` calls Picture evaluation, armed-pass notification and Mentor notification. The old `picture-htf` log prefix understates the shared downstream scope.

The message says the evaluator rebuilds from cache. Cache recovery does not alone prove an entry deadline or every notification was preserved. Conversely, these counters do not prove stored candles were lost or a trade was missed. A causal link between the warning flood and queue overflow is [C]; it has not been demonstrated.

Next: replay or load-test this burst through the production queue with completed-frame timestamps; assert latest-bar recovery, Mentor/armed wakeups and deadline outcomes. Measure slow work in the shared sink before choosing a fix. Do not increase queue capacity and declare the issue solved without the timing proof.

### 6. Dependency advisories in production and development tooling — P2 maintenance, PROVEN [A]

`npm audit --omit=dev --json` reports **one low-severity production dependency issue**, KaTeX, advisory `GHSA-238p-pmpm-9mq7` (existing prototype pollution can bypass trust restrictions). Latest-source `web/package.json` declares `katex ^0.16.27`; audit's vulnerable range is `>=0.11.0 <0.18.2`. Audit proposes 0.19.0 as a breaking-version fix.

This proves a dependency advisory match, not a reachable exploit in this app. No exploitation or full frontend data-flow review was performed. Review upstream fix compatibility and regression-test rendered math before upgrading. Do not run a forced audit fix blindly. A subsequent full `npm audit --json` found **10 affected-package entries: 7 high, 2 moderate and 1 low**. These include transitive/metavulnerability chains, not necessarily ten independent advisories. The production-only result remains one low entry. `source-map-js` 1.2.1 and `postcss-selector-parser` 6.1.4 are marked `dev: true` in the lockfile. GitHub independently lists three open alerts: #116 PostCSS selector parsing CPU exhaustion (`GHSA-rj75-hqrm-r3gf`, medium), #117 source-map-js event-loop denial of service (`GHSA-68fv-2mgg-jv7q`, high), and #118 KaTeX (low). The full npm result also includes braces/chokidar/fast-glob/lint-staged/micromatch/postcss-nested/tailwindcss chains. This requires dependency triage for build and development environments; it is not evidence of a remotely reachable production exploit. Go module vulnerability scanning remains UNVERIFIED.

## Other observations and non-findings

- **Clock/feed-age hold [A]:** at 04:24:59 CT, log line 23276 says LONDON planner authoring was deferred at 60,718 ms versus 60,000 ms tolerance. `kernel/clock_drift.go` measures local time against an estimated bar close; it cannot distinguish host clock skew from feed delay by itself. Actual NTP drift is UNVERIFIED. Exits/armed management were explicitly unaffected by that message. This is a real planning deferral, not evidence that Mentor stopped entirely.
- **No full candle-history outage [A]:** current-contract rows exist and advance. At approximately 08:08 CT, MNQ 12-26 had 25,143 1m rows (latest open 08:07 CT), 5,802 5m rows (08:00), 1,917 1h rows (07:00), and 499 4h rows (01:00). These are inventory counts, not a completeness certificate. A 4h open time naturally trails wall time; it is not judged against 1m freshness. Total stored depth does not prove the live evaluator consumed all those bars.
- **Chart short-horizon logs are insufficient evidence of a broken chart [A]:** the provider ring reports some 5m requests as 5,000 asked / 2,500 served. `api/handler_klines.go:192–210` subsequently deepens from the current-contract store, and later code can stitch prior contracts or aggregate finer bars. A provider warning occurs before the final chart response. Browser-rendered coverage and the authenticated final response were not inspected, so actual chart completeness is UNVERIFIED.
- **Old excursion row [A]:** repeated missing-1m-coverage warnings identify position **572**, source `e7_farside_test`, a closed August row with `pnl_corrected=NULL`. It already has stored MAE/MFE values; the warning's “left NULL” wording is misleading for that row. It is not evidence that today's MNQ feed is missing. Exclude its unresolved P&L from any totals and report the exclusion if measuring a period that includes it.
- **Zero-drop WARN summaries [A]:** the persistence path emits regular WARN summaries with drop counters all zero and a remembered peak depth. `provider/ninjatrader/bar_persist.go:343–355` logs unconditionally once per minute and loads the peak without resetting it. This is noisy telemetry, not evidence of 4,096 lost bars.
- **Old strategy expectations are not the current operating spec [A]:** saved Mentor mode is on; the runtime says AI entry decisions are skipped for Mentor. Picture is also enabled in the saved day-plan config. Overnight Mentor trades used three contracts. The September one-contract picture discussion cannot by itself make those October Mentor trades a bug. Simultaneous config flags do not by themselves prove duplicate execution.

## Runtime and data checks that passed

[A] Health at 08:10:18 CT returned `status=ok`, `db=ok`, NT8 `link=up`, `feed_status=Connected`, `last_bar_age_ms=18770`, one running trader, revision `7a8b71d9896a`. PID/start time remained unchanged during the audit. A prior 08:02 health sample also showed a connected feed. These are point-in-time observations, not uninterrupted uptime proof.

[A] Two overnight filled Mentor arms were present: arm IDs **304** and **305**, three contracts each. Closed position IDs **638** and **639** have `pnl_corrected=-85.5` and `168` respectively; these values are reported only to establish identified trade activity, not as a daily performance result. Both selected rows have non-NULL corrected P&L. No OPEN positions were present at the initial query. Order snapshot **92899**, `Sim101`, received at the initial read, reported zero working orders. This is not a current-at-publication flat gate and must not be reused to authorize deployment.

[A] Backup timer `vl-backup.timer` was active; `vl-backup.service` reported success/exit 0 for 05:00:01 CT. The file `/home/hoang/vl-backups/auto/daily/vl-2026-10-08_050001.db.gz` exists, **519,670,581 bytes**, and `gzip -t` exited 0. A decompression integrity check is not a SQLite restore drill; application restore usability remains UNVERIFIED. Production DB was not modified or copied by this audit.

[A] Disk was about 49% used with about 493 GB available; memory had about 20 GB available and no observed swap use. No resource-exhaustion conclusion follows from the warning flood alone.

## Verification results

| Check | Revision/scope | Result |
|---|---|---|
| `go test ./... -count=1` | Actual running 7a8b71d9 | exit 0; 47 tested packages, 35 packages without tests |
| `go test ./... -count=1` | Latest dev 20775987 source, docs claim only | exit 0; 47 tested packages, 36 packages without tests |
| `npm ci --ignore-scripts --no-audit --no-fund` | latest web | exit 0 |
| `npm test` | latest web | exit 0; 107 files, 1,017 tests passed |
| `npx tsc --noEmit` | latest web | exit 0 |
| Production web build, correct SHA supplied | latest web | exit 0; large-chunk warning remains |
| Targeted race: `TestMentorStaleDataBlock*` and `TestSeedLine*` | latest trader and kernel/mentor | exit 0; no race report; not a full repository race run |
| Audit reference-identity panic probe | actual running source | exit 0 **means defect reproduced**, not fixed |
| Production npm dependency audit | latest web | exit 1 for the one low advisory above |
| Full npm dependency audit | latest web | exit 1; 7 high, 2 moderate, 1 low affected-package entries, including development chains |
| Backup gzip integrity | latest 05:00 archive | exit 0 |

The initial plain `npm run build` correctly refused because `VITE_GUIDE_BUILT_REV` was absent. Re-running with `VITE_GUIDE_BUILT_REV=207759877aa7f8de8ee7291b41de4168616297f3` succeeded. This is a working release-provenance guard, not a build defect. Web tests printed jsdom canvas warnings but finished successfully.

Reproduction artifacts are local `/tmp/vl-audit-20261008-*` logs. A compact verification transcript and the panic probe are included with this report so the main conclusions do not depend only on temporary files.

## Next work, with acceptance evidence

1. Resolve whether the existing $450 daily-loss setting is intended to enforce. This is the owner's configuration decision; the current OFF state is explicit. Verify supported reload and runtime state after any owner-requested change.
2. Activate release #11 only through the established attended cutover. Verify running revision, AddOn compatibility, actual data readiness, unchanged SIM binding and post-boot receipts. Passing source tests does not itself activate the release.
3. Fix nullable identity logging and dynamic warning keys together with production-call-site regression tests. Keep numerical timing detail in log fields rather than deduplication keys.
4. Investigate the shared live-sink burst with queue/timing evidence. Demonstrate whether any final frame's loss can delay a Mentor/armed trigger beyond its allowed window; repair the scheduling path if so.
5. Clean up misleading zero-drop and historical-excursion warning text, and triage the production/development dependency advisories with controlled upgrades.

No production files, configuration, account binding, DB rows, orders or services were changed by this scan. The audit branch contains reporting/probe text only. No blanket claim that the system is fully verified end-to-end is made: broker protection during faults, actual rendered chart coverage, full restore, and natural-market behavior of release #11 remain outside the proof collected here.
