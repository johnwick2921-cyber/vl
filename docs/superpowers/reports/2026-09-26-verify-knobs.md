# VERIFY-0926 — SETTINGS & KNOBS cross-check (DS-106, adversarial)

Independent verification of DS-102's `audit/0926-knobs` @883fe0fc (report + census CSV 419 rows) and draft PR #228. READ-ONLY. Base `04ae1c2f` (live boot 3 = 6cac1b89). Method: worktree `/home/hoang/nofx-ds-106-verify0926` @ 04ae1c2f; DB copy `v.db` (`.backup` of `data/data.db` opened `mode=ro`); live log read-only; every cited line re-read at this base. Seed for the 40-row sample: **926106**. No code changes, no tests run (L17 + box shared).

---

## 1. Census cross-check — seeded sample + special rows

Scored **68 unique rows** (40 seeded + 32 special: 16 DEAD + 13 SHADOWED + 3 CONFLICT; 4 overlaps deduped).

**Live-value column (strategy rows, json_extract against the live `a5b7662e` config):** 0 wrong. 9 differences were representation-only, not value errors (SQLite renders stored `false` as `0`, `true` as `1`; `"5m_close"` quote normalization): `use_ai500`, `enable_netflow_ranking`, `max_contracts_enabled`, `notional_cap_enabled` (false↔0), `evening_digest`, `levels_fresh_by_tf`, `structure_map`, `wake_on_htf_ob` (true↔1), `acceptance_rule` (quotes). Every genuinely comparable live value matches the CSV.

**Default column (68 rows re-derived from the `defined_at` line):** 65 match. 3 representation NOTES:
- `trailing_atr_mult` CSV `code_default=0` vs the struct comment "default 2.0" (`store/strategy.go:2128`) and the effective resolver default `defaultTrailingATRMult = 2.0` (`trader/auto_trader_trailing.go:26,44-46`) — the census column reads like "off" where the effective default is 2.0. **NOTE N2**.
- `use_ai500` / `enable_netflow_ranking` CSV `true`: plain `bool` fields (`store/strategy.go:1913/1975`), struct zero is `false`; the `true` traces to the seed/onboarding defaults (`api/server.go:361`, `:385`). Crypto-path fields; the live futures strategy stores `false` and the CSV's live column is correct. **NOTE N3** (column semantics: seeder default ≠ struct default).

**Readers spot-check (specials):** STOP_ENTRY_SEAM → `kernel/entry_law.go:138` ✓ · FAST_MARKET_REASONING → `trader/auto_trader_loop.go:92` ✓ · DATABENTO_DATASET → zero readers, confirmed by the code's own comment `config/config.go:211` ("loads removed — zero live") ✓ · wake_on_htf_ob folded-with-own-effect → boot line confirmed live ("⚙ folded knob wake_on_htf_ob (order-block class)=true honoured from stored config", 20:12:50 + 20:14:25) ✓.

**NEW N1 (P2) — two DEAD rows are dead only to the live binary, not "anywhere":** CSV says `NOFX_BACKEND_PORT` / `NOFX_FRONTEND_PORT` = "no reader anywhere", but `docker-compose.stable.yml:11,34` and `docker-compose.prod.yml:18,42` read both (`"${NOFX_BACKEND_PORT:-8080}:8080"`, `"${NOFX_FRONTEND_PORT:-3000}:80"`). Correct for the live systemd process (no reader in the binary); wrong as an "anywhere" claim. Severity P2 (a docker deployer changing them expects an effect and gets one — the wording is what misleads).

**NEW N4 (P2, report-text) — three non-settings named as candidate knobs:** the report's "9 candidate-unverified" list includes `context_limit`, `fixed_overhead`, `suggestions`, but those JSON tags belong to the token-estimation DTOs `TokenEstimate`/`TokenBreakdown`/`ModelLimit` (`store/strategy.go:2555-2575`) — they are API display payloads, not settings; the CSV correctly omits them. The report text overstates the candidate list.

## 2. P1/P2 verdicts

**P1-1 — stop-entry seam ON vs the 2026-09-05 ruling: CONFIRMED.**
- My own ruling quote: `docs/superpowers/reports/2026-09-05-wave-b-stop-entry.md:5` — "`STOP_ENTRY_SEAM=off` throughout, by owner ruling." and `:541` — "`STOP_ENTRY_SEAM=on` in `/home/hoang/nofx/.env`. This is a LIVE path, not a dormant one — that is how 21 malformed orders reached a broker."
- Live [A]: `.env:43` `STOP_ENTRY_SEAM=on` directly under `.env:42` "…Stays off until a cancel-confirmation wave lands." Reader `kernel/entry_law.go:135-138` ("Default OFF"); gate `trader/armed_executor.go:1378` `if !stopEntrySeamOn() { continue }` (the ONLY gate on the stop-entry wire path); boot line `🎛 entry law: … stop_entry_seam=ON` (main.go:643, both boot-3 boots).
- No later re-enable ruling exists anywhere in `docs/` [A]. The contradiction is real; intent (deliberate re-enable after the 2026-09-23 AddOn proof vs accidental `.env` drift) is for the owner/CTO — same conclusion as DS-102, now with the ruling quoted.

**P2-1 (prompt says exits SUSPENDED; env unsuspended): CONFIRMED.** Live strategy `a5b7662e` prompt text: "the saved breakeven_trigger_points (40) and trailing are both SUSPENDED at the wire (trader/exit_mechs_suspend.go)" [A, json_extract]; `EXIT_MECHS_SUSPENDED=0` → `exitMechsSuspended()==false` (`trader/exit_mechs_suspend.go:37-45`); knobs `breakeven_trigger_points:40, trailing_enabled:false` stored. Guide rows stale as claimed.

**P2-2 (dead .env keys): CONFIRMED.** `NOFX_TIMEZONE` zero readers [A]; `CLAW402_DEFAULT_MODEL` only writers (`api/handler_onboarding.go:72,257`, both writing the constant) [A]; `FAST_MARKET_REASONING` ×4 in `.env` (lines 50/53/56/59), all `max` [A].

**P2-3 (9 defaults-ON knobs, no Studio surface): CONFIRMED.** Each of the 9 has 0 non-test, non-guide references in `web/src` [A].

**P2-4 (planner_contract absent from the guide): CONFIRMED.** 0 matches across all 17 `web/src/guide/content/*.ts` files [A].

**P2-5 (folded wake_on_htf_ob keeps its own effect): CONFIRMED.** Boot line quoted above.

**P2-6 (sessions_enabled=["NY"] while ASIA/LONDON run): CONFIRMED.** Live config `"sessions_enabled":["NY"]` [A, json_extract]; per-session overrides carry `enable:true`; `sessionEnabledForStrategy` (`trader/auto_trader_planconfig.go:74-79`) makes the override authoritative [A].

**P2-7 (two is_default=1 strategy rows): CONFIRMED.** Rows: id `""` "MNQ SIM Default" (is_default=1, is_active=1) and `578ac8f6-…` (is_default=1, is_active=0) [A]. Nothing live binds the default (trader binds `a5b7662e`), NOTE-level as stated.

**P2-8 (ineffective knobs still editable in Studio): CONFIRMED.** `max_margin_usage` + `min_position_size` in `RiskControlEditor.tsx:677-680, 747-750` [A].

## 3. Miss hunt — env + strategy tags vs the CSV

- `os.Getenv`/`os.LookupEnv` census at 04ae1c2f: **188 names; 0 missing from the CSV** [A]. DS-102's env coverage is complete.
- Strategy JSON tags at 04ae1c2f: 201; 21 not in the CSV leaf names — all 21 are non-settings (DTOs: `TokenEstimate/TokenBreakdown/ModelLimit` tags; `StrategyDB` row tags `id/user_id/is_active/is_default/created_at/updated_at`; plan-stats tags `total/breakdown/level/…`). Correctly omitted. The only substantive delta is the report-text overclaim (N4).
- No settings missed by the census.

## 4. Owner-rule contradictions

- REASONING = MAX everywhere: no contradiction. `mcp/config.go:72` `ReasoningEffort: getEnvString("AI_REASONING_EFFORT", "max")` — default max; the two enabled `ai_models` rows carry empty `thinking_mode`/`reasoning_effort` (DeepSeek AI enabled=1) → defaults apply → max. `FAST_MARKET_REASONING=max` ×4 [A]. Consistent.
- SIM-only: `isAccountTradeable` (`tcp_trader.go:489`, called at `:554`) intact at 04ae1c2f; no path weakened [A].
- One-position + EOD-flat 14:45: enforced (EntryGate leg 7; EOD flat at `trader/auto_trader_clock.go:454`; verified live on row 619 in the trading audit) [A].
- The one live contradiction to an owner RULE is exactly P1-1 (the 09-05 stop-entry ruling) — already named.

---

## 5. Counts

CONFIRMED 10 (P1-1 + P2-1..P2-8) · REFUTED 0 · PARTIAL 1 (N1 — the two port keys: correct for the live binary, wrong wording) · NEW 4 (N1 P2, N2 NOTE, N3 NOTE, N4 P2). CSV row scores: 68/68 live correct (9 representation-normalized), 65/68 default exact, 3 default-column representation notes.

## 6. What I did NOT verify

`git log -1 -- docs/…` freshness beyond the two input branches quoted above (both re-read at 04ae1c2f); the Studio UI rendering of every knob (only the 9-knob absence and RiskControlEditor were checked); the guide's 1069-line settings.ts in full (spot-checked P2-1 rows only). No go tests run (L17, shared box).
