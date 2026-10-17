# Contributing to nofx

This is the operational contract for shipping changes to `nofx`, the NQ futures trading bot. Read it once. The rules in here are not aesthetic — they are the procedural floor that keeps a live-validated trading bot live-validated across PRs.

For architectural context, read `docs/ONBOARDING.md` first. For decision rationale, read `docs/adr/ADR-001` through `ADR-007`.

---

## Before you start

Confirm these are true before you open an editor:

- You have read the **subsystem `CLAUDE.md`** for the directory you intend to touch (`kernel/`, `market/`, `provider/`, `trader/`, `web/`, plus any deeper `CLAUDE.md`).
- You have read the **ADRs** relevant to your change. `docs/adr/ADR-001..ADR-007`.
- You know which **plan** your change belongs to. Pure bug fixes are fine; new features should map to a plan or be small enough to stand alone.
- You can run the build green locally: `go build ./...` and `cd web && npm run build`.

If your change touches the AI prompt, the decision JSON shape, or any "high-cascade type" listed in `CLAUDE.md`, **stop and open a discussion first**.

---

## Branch + PR workflow

Branches off `main`:

```bash
git checkout main && git pull
git checkout -b feat/<short-slug>     # or fix/..., docs/..., chore/...
```

Naming conventions:

- `feat/` — new feature
- `fix/` — bug fix
- `docs/` — documentation only
- `chore/` — build, config, dependency, rename
- `refactor/` — code restructure with no behavior change
- `perf/` — performance, no behavior change
- `test/` — test-only

PR title in conventional-commits form:

```
feat(plan2): tick rounding + session calendar
fix(plan4.2): NinjaTrader exchange type missed at 5 downstream consumers
docs(adr): add ADR-007 Plan 1 critical-file integrity
```

PR body must include:

1. **What** — one paragraph describing the change.
2. **Why** — link to plan task or issue.
3. **Plan 1 critical file delta** — `git diff v1.0-plan1 -- <list>` output, or "no Plan 1 critical files touched."
4. **Test evidence** — `go test ./...` exit code, `npm run build` exit code, any smoke runs.
5. **UI verification** — for any frontend change, a live-DOM Playwright check, not just a bundle scan. See §6.

---

## Plan 1 critical file integrity

This is the most important rule in the document. Read ADR-007 for the full rationale.

**The following files were validated against a live SIM environment** (real SIM fills on SIM101 at NQ 29807 via NT Playback on 2026-05-22). They must remain **byte-identical** across every subsequent PR unless your PR has a spec-authorized exception:

```
provider/databento/historical.go
provider/databento/resolve.go
provider/databento/contract_calendar.go
provider/databento/client.go
provider/databento/mock_server.go
provider/ninjatrader/csv_writer.go
provider/ninjatrader/csv_tailer.go
provider/ninjatrader/types.go
provider/ninjatrader/mock_nt.go
cmd/nq_smoke/main.go                    (ADD-only — Plan 5 Task 29 dispatcher)
kernel/engine_prompt_futures.go
kernel/cme_calendar.go
kernel/risk_limits.go
kernel/engine_prompt_golden_test.go
market/databento_adapter.go
market/decimal_safe.go
market/data_freshness.go
trader/ninjatrader/tick_rounding.go
trader/ninjatrader/trader.go
```

Verify in every dispatch convergence phase and again before commit:

```bash
git diff v1.0-plan1 -- \
  provider/databento/historical.go \
  provider/databento/resolve.go \
  provider/databento/contract_calendar.go \
  provider/databento/client.go \
  provider/ninjatrader/csv_writer.go \
  provider/ninjatrader/csv_tailer.go \
  provider/ninjatrader/types.go \
  kernel/engine_prompt_futures.go \
  kernel/cme_calendar.go \
  kernel/risk_limits.go \
  market/databento_adapter.go \
  market/decimal_safe.go \
  market/data_freshness.go \
  trader/ninjatrader/tick_rounding.go \
  trader/ninjatrader/trader.go
```

Expected output: empty, **or** confined to the documented exceptions below.

**Exceptions (spec-authorized):**

- `cmd/nq_smoke/main.go` ADD-only — Plan 5 Task 29 adds dispatcher wiring; existing logic unchanged.
- Plan 4 Task 24 wrappers — retry / circuit-breaker around `provider/databento/client.go` and `provider/ninjatrader/csv_writer.go` are written as **wrappers in separate files**, never as in-place edits to the locked files.
- `provider/databento/mock_server.go` + `provider/ninjatrader/mock_nt.go` — added during Plan 5 and locked from that point forward.

If you need an exception that isn't listed, **the PR description must include a re-validation plan** (how will we re-prove the live SIM round-trip still works after this change?). Reviewers will ask.

---

## Parallel subagent dispatch

For multi-task work, dispatch parallel `general-purpose` subagents per ADR-005. Mechanics:

1. **One subagent per disjoint task block.** Task blocks are disjoint when their file sets do not overlap, or only overlap additively under ownership markers.
2. **Ownership markers in shared files** — `// Plan N Task M — owned by TX` delimits added regions in files multiple tasks share.
3. **Convergence verification phase** — serial pass that runs `go build ./...`, `go test ./...`, `cd web && npm run build`, and a `git diff` review against the dispatch brief. No commit until convergence is green.
4. **Plan 1 critical files are read-only inside dispatched work** (see §3 above and ADR-007).
5. **`general-purpose` subagent type only.** The `feature-dev:code-architect` subagent is read-only — it blocks `Bash`, `Edit`, and `Write`, and will return "I would do X" without producing any diff. This was the failure mode of PR #3 Stage 5E. See ADR-005.

---

## Tool manifest (hard rule)

Every dispatched implementation subagent receives exactly this tool manifest:

```
Bash, Read, Edit, Write, Grep, Glob
```

Do **not** downgrade this list. Do **not** swap `general-purpose` for `code-architect`. Do **not** add the Playwright tools to an implementation subagent — Playwright is for verification, run from the main thread after convergence.

---

## Playwright UI verification

For any frontend change, **bundle scan ≠ live DOM verification**.

Plan 4.2 exists because Plan 4 changed an exchange-type enum and Plan 4.1's verification scanned the JS bundle for the new string. The string was there. But five live-DOM consumers (a dropdown filter, a card label, a hover tooltip, a Settings header, a Decisions-tab tag) had not been updated — they read the old enum value and silently rendered nothing or misrendered. The bundle scan said "all good." The user opened the dashboard and saw an empty exchange dropdown.

Lesson: **after `npm run build`, navigate the live DOM with Playwright** and assert against rendered text and visible elements, not against bundle strings. See `docs/operations/TRADER_MODE.md` §5 for the emergency-flat runbook style.

---

## Release tagging

The repo carries one tag per shipped plan. In order:

- `v1.0-plan1` — CSV bridge SIM-validated.
- `v1.0-plan2` — CME futures domain (tick rounding, calendar, roll, decimal-safe).
- `v1.0-plan3` — Risk limits + force-flat + stale-data drift.
- `v1.0-plan4` — Observability (audit trail, retry/breaker, Prometheus, Emergency Flat).
- `v1.0-plan4-1` — UI gaps (NinjaTrader UI, Decisions tab, VLTrader rebrand sweep).
- `v1.0-plan4-2` — Integration bug fix-up (five downstream consumers).
- `v1.0-plan5` — Testing matrix (Databento mock, NT mock, prompt goldens, smoke sub-commands).
- `v1.0-plan6` — Operational runbooks.

Each tag is the merge commit of its PR into `main`. Tag from the merged HEAD:

```bash
git checkout main && git pull
git tag -a v1.0-planN -m "feat(planN): <subject>"
git push origin v1.0-planN
```

A tag is the rollback target referenced in `docs/operations/ROLLBACK.md` §1. Do not move or delete a tag once published — write a new one.

---

## Don't break the build

These commands must exit 0 before you push:

```bash
go build ./...
go test ./...
cd web && npm run build && cd ..
```

If you touched the AI prompt path, run the goldens explicitly:

```bash
go test ./kernel -run TestPromptGolden
```

If you touched any Plan 1 critical file, the verification diff in §3 must be clean — or your PR description carries the spec-authorized exception text.

If you touched the CME calendar, re-read ADR-006 and run:

```bash
go test ./kernel -run TestIsCMEOpen
go test ./kernel -run TestHoliday
```

**Annual maintenance:** the holiday table in `kernel/cme_calendar.go` is hardcoded. When the calendar year rolls over, somebody updates the table. There is no test that auto-fires on Jan 1. This is documented in ADR-006 — treat the table review as a January item.

---

## Code style

- **Go:** `go fmt`, `go vet`, errors wrapped with `fmt.Errorf("...: %w", err)`. The 19-method `trader/types.Trader` interface uses a compile-time check (`var _ types.Trader = (*Trader)(nil)`) — don't add or remove methods without updating every broker. See `trader/CLAUDE.md`.
- **TypeScript:** strict mode, `npm run lint` clean. No `any`. Three i18n languages (`en`, `zh`, `id`) — every new user-facing string needs all three.
- **CSV protocol for `vltrader.cs`:** 5-field signals, 3-field fills. Pinned in `CLAUDE.md`. Do not change without coordinated Go + NinjaScript updates and a rollback plan.

---

## Where to find help

- `docs/ONBOARDING.md` — system tour for new engineers.
- `docs/adr/ADR-001..ADR-007` — architecture rationale.
- `docs/operations/STARTUP.md` / `MONITORING.md` / `TRADER_MODE.md` / `ROLLBACK.md` / `DR.md` — operator runbooks.
- `docs/superpowers/plans/2026-05-22-nq-databento-ninjatrader.md` — the 37-task implementation plan.
- `CLAUDE.md` at repo root — project-level instructions, including the high-cascade types list and the JWT secret gate.
