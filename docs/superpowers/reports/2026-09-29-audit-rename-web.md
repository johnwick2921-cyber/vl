# AUDIT-RENAME-WEB — READ-ONLY adversarial audit of the VL rename plan (WEB/UI slice)

DS-103 · 2026-09-29 · branch `audit/rename-web` · base origin/dev @ accept:

```
9d52f5dc6 RELEASE 4d5382069… — boot 7: #266 … + #265 … booted 01:00:43 CT 2026-09-28
```

Plan audited: `/home/hoang/rename-vl-plan/2026-09-29-rename-nofx-to-vl-plan.md`, md5 `b16759336fe0e264d11c1bc885ce5192` (matches the dispatch-stated b1675933; the full text was read before this report). Slice: dispatch CHECK 1–5 (R1b items 3, 5, 9 — header, browser storage, visible strings/guide, dev/web serving, wording).

Evidence tiers on every claim: **[A]** read/ran it at the base · **[B]** inferred · **[C]** speculation. No code changed anywhere; the only file this branch adds is this report.

---

## CHECK 1 — `X-NOFX-Update` → `X-VL-Update`, "exactly one of the two"

**What exists [A].** `api/handler_updates.go:65` `const UpdateHeader = "X-NOFX-Update"`; the CSRF rule at `api/handler_updates.go:408` is `if v := r.Header.Values(UpdateHeader); len(v) != 1 || v[0] != "1"` — one name, exactly one value, exactly "1". CORS at `api/server.go:98` allows only `Content-Type, Authorization` (the header is deliberately absent; pinned by `TestUpdatePreflightNeverAllowsTheUpdateHeader`). Web sender: `web/src/lib/api/updates.ts:14` `const UPDATE_HEADERS = { 'X-NOFX-Update': '1' }`; the wire is pinned two ways — Go-side `api/handler_updates_web_header_test.go` pins the name against `api.UpdateHeader` (auto-follows the constant), TS-side `web/src/lib/api/updates.header.test.ts` pins the outgoing request through the REAL httpClient.

**Is "exactly one of the two" enforceable as written?** Yes, but only if the D2 wording is tightened — see CHECK 5. The current code checks one name; the two-name rule needs an explicit total count: `len(r.Header.Values(vl)) + len(r.Header.Values(nofx)) == 1` and that single value `== "1"`. A browser page CAN send both names (fetch `Headers.append`) — the two-name total makes that 403, and a duplicated single name (`X-VL-Update: 1, 1`) stays 403 under the same count.

**Can a browser/proxy duplicate it? [B]** A proxy is already refused by the forwarding-header and Origin/Sec-Fetch-Site checks at `handler_updates.go:400-417` (loopback-direct only, per the runbook). A same-origin page is the only thing that can set it at all — the two-name count closes the remaining shape.

**Does the Updates page break between R1b merge and boot?** No, for the served dist: web + Go ship in one release commit and the server-side dual-accept lands in the same commit that flips the web sender, so the pair is always consistent. Two edge windows do exist:
- **old page bundle + new server** (a tab left open across the boot): the old bundle sends `X-NOFX-Update` → dual-accept keeps it working. ✓
- **new web from the :3000 dev server + old bot on :8080** (someone runs `npm run dev` from the renamed tree before the R2 boot): the old server 403s the new header. The freeze + T7 forbid lanes from doing this; the owner could. → **P3-2** below.

**Missed literal sites [A].** `telegram/agent/apicall_updates_test.go:37-38` carries the header in TWO extra shapes the plan never names: as a JSON body key `{"X-NOFX-Update": "1", ...}` (line 37) and as a query string `/api/updates/check?X-NOFX-Update=1` (line 38). A mechanical rewrite of the bare header string covers all three shapes, but the file must be on the list — it is not. → **P3-1**.

**Verified plan claim.** "JWT issuer `vlAI` (never checked at parse — no logout)" is TRUE: `auth/auth.go:230` sets `Issuer: "nofxAI"` and no file in the repo verifies the issuer (`grep VerifyIssuer|Issuer ==|claims.Issuer` → zero non-test hits). **[A]**

---

## CHECK 2 — browser storage migration

**Full key inventory [A]** (every `localStorage`/`sessionStorage` key in `web/src` that carries `nofx`):

| Old key | Sites | New name per plan |
|---|---|---|
| `nofx_user_mode` | `web/src/lib/onboarding.ts:3` | `vl.userMode` ✓ |
| `nofx_beginner_wallet_address` | `onboarding.ts:4`, `SetupPage.tsx:68`, `AuthContext.tsx:256` | `vl.beginnerWalletAddress` ✓ |
| `nofx_beginner_onboarding_completed` | `onboarding.ts:5`, `SetupPage.tsx:67`, `AuthContext.tsx:255` | `vl.beginnerOnboardingCompleted` ✓ |
| `nofxi-agent-chat` | `agentChatStorage.ts:1` (LEGACY_AGENT_CHAT_STORAGE_KEY) | `vl.agentChat…` — **name is an ellipsis, see P1-2** |
| `nofxi-agent-chat:<uid>` | `agentChatStorage.ts:15` (template at write time) | same |
| `nofxi-agent-chat-draft:<uid>` | `agentChatStorage.ts:19` | same |

No `sessionStorage` key carries nofx. Keys without a brand (`auth_token`, `auth_user`, `language`, `returnUrl`, `from401`, `user_id`, `session_id`) are correctly left alone.

**P1-2 — the migration algorithm is underspecified where the data lives.** The plan (R1b item 4 / D2 item 5) says "on first load migrate … copy then delete old; never drop chat history (vitest: history + draft survive)". Four holes, each with a concrete failure:

1. **Per-user keys are unbounded.** `nofxi-agent-chat:<uid>` / `-draft:<uid>` are keyed by `userId || 'guest'`; the migration must ENUMERATE `Object.keys(localStorage)` by prefix, not a fixed three-key list. Fix text: "migrate every key matching `^nofxi-agent-chat(:|-draft:)` by prefix scan".
2. **The new key names are an ellipsis (`vl.agentChat…`).** The migration and the post-rename `agentChatStorage.ts` must agree byte-for-byte; pin them: `vl.agentChat` (legacy), `vl.agentChat:<uid>`, `vl.agentChatDraft:<uid>` — whatever the implementer picks, the plan names it exactly, once.
3. **Two-tab / run-twice / overwrite race.** If the migration copies old→new unconditionally, a second tab that already wrote a NEW message under the new key has it overwritten by stale old data. Fix text: "copy only when the new key is ABSENT (never overwrite); delete the old key only after the copy of that key succeeded".
4. **Failure handling.** `JSON.parse` errors on corrupt chat blobs and `QuotaExceededError` on `setItem` must leave the old key IN PLACE (never delete what was not safely copied) and retry on the next load; parse-failure on one key must not abort the others.

Also name the entry point: "on first load" must be ONE module imported before any reader reads a key (e.g. run in `main.tsx` before the first render), otherwise a page can read the new (empty) key before the migration ran and show a blank chat that then survives as the "existing new key" that rule 3 refuses to overwrite. **[B] for consequences; [A] that the plan text says none of this.**

---

## CHECK 3 — visible-string guards and the guide/FAQ/i18n surface

**The guards [A].** `web/src/brand-i18n.test.ts:26-27` strips `NOFX_[A-Z_]+` from prose then asserts `not.toMatch(/NOFXi|\bNOFX\b|VL Trader/)`; `web/src/brand-visible.test.tsx:53,74,110` asserts rendered pages contain no `NOFXi?`, no `VL Trader`, and no literal `'NOFX / VL'`. `branding/scope_test.go:48` guards the Go import namespace. After the rename these stay green **provided** every visible string flips — and the guard as written also tolerates `VL` (it forbids `VL Trader`, not `\bVL\b`), which is exactly the target brand. No change needed to the guard's logic for bare-VL strings.

**P1-1 — the plan never mentions the Tailwind/CSS theme, which is 545 of the web's nofx hits [A].**
- `web/tailwind.config.js:10-28` defines the palette `nofx-gold`, `nofx-bg`, `nofx-accent`, `nofx-text`, `nofx-success`, `nofx-danger` (plus `nofx-gold.dim` in the `neon` shadows at :71-72).
- `web/src/index.css:19-23` mirrors them as `--nofx-gold` etc. plus `--nofx-glass`, `--nofx-border`, and `.nofx-toast` at :310 (and `:108`, `:192` use the vars).
- 545 class-name occurrences across `web/src` (`TraderDashboardPage.tsx` 92, `StrategyStudioPage.tsx` 75, `SettingsPage.tsx` 10, `PageNotFound.tsx` 5, `BeginnerOnboardingPage.tsx` 7, …).

Consequence: D2's census guard (item 10, "any other hit fails") would demand an allow-list of ~545 CSS class names — an allow-list so wide it hides real misses — or the rename misses them and the census fails at gate time, late. Fix text (new R1b item): "**11. Design tokens**: rename the `nofx-*` palette in `tailwind.config.js` and the `--nofx-*`/`.nofx-*` definitions in `web/src/index.css` to `vl-*`, and rewrite every class occurrence (mechanical, one commit). The census allow-list then contains NO theme classes."

**P2-1 — visible install commands live in i18n, not only in FAQContent.tsx [A].** The plan's item 9 names "FAQContent.tsx repo links". The VISIBLE install instructions are in `web/src/i18n/translations.ts`: `:638` `'git clone https://github.com/NoFxAiOS/nofx and switch to dev branch…'` and `:739` the curl `raw.githubusercontent.com/NoFxAiOS/nofx/main/install.sh` one-liner (en; the zh/id tables carry the same pair). These are shown to users in the FAQ; after the rename they must point at `johnwick2921-cyber/vl` (post-R4) — or be explicitly listed in the allow-list with a dated "upstream history" note. The ABOUT strings (`nofxNotAnotherBot`, `nofxDescription1-5`, `aboutNofx`, `whatIsNofx` at :610-625, :707) already interpolate `PERSONA_NAME` — no change needed, worth saying in the dispatch so the lane does not "fix" them.

**Plan-complete sites verified [A]:** guide `welcome.ts:38` (`nofx-bin` diagram line — item 9 cites welcome ~38 ✓); `status.ts:408` (`NOFX_CHART_ACROSS_ROLL` — env name, R1a's list has it; status.ts is cited ✓); `settings.ts:1784` (`data/nofx_YYYY-MM-DD.log` — item 9 "settings.ts log name" ✓); `updates.ts:39` (`nofx_updates_refused_total` metric + `NOFX_UPDATER` env — metrics are item 2, guide text must follow under GUIDE CONTENT LAW ✓ implied). `strategy-translations.ts`'s 6 hits are all `nofxos*` (NofxOS provider — plan-excluded third party ✓; the `:1` "NOFX i18n Consolidation" header comment is cosmetic).

---

## CHECK 4 — :3000 dev/web server and the build/serving path

**Serving [A].** Go serves `web/dist` via `api/ui_serving.go:35` (`UIDistDir = "web/dist"`) — relative, no name in the path; the web build keys off `VITE_GUIDE_BUILT_REV` (env-injected, no name); `web/vite.config.*` and `web/index.html` carry zero `nofx` hits. `web/package.json:2` `"name": "nofx-web"` — covered by the plan (npm name `nofx-web` → `vl-web`, R1b item 1). ✓

**The :3000 unit [A].** `deploy/nofx-web.service` exists (Description "NOFX frontend (vite dev server :3000)" at :18, proxies /api → :8080). The plan's item 6 rename list does name `vl-web.service` — **covered**. Two adjacent misses:
- `deploy/install-autostart.sh:114` prints `journalctl -u nofx -u nofx-web` and installs the unit names — the plan's item 6 rename list names vl-lock/vl-db-backup/vl-claim/vl-clock-guard/cutover/install-updater-worker but **not** `install-autostart.sh`. → **P3-3**.
- Fixture strings: `api/release_dir_test.go:65` (`/srv/nofx/releases/current`), `api/handler_updates_starter_test.go:122` (`Binary: dir + "/nofx"`). Harmless, but the census grep (item 10) flags them — allow-list or flip them. → **P3-4**.

---

## CHECK 5 — D2 wording (items 3, 5, 9)

- **Item 3**, plan text: *"server accepts EXACTLY ONE of X-VL-Update / X-NOFX-Update equal to '1' (both present → 403)"*. Ambiguity: "exactly one … equal to '1'" does not say what "one" counts — one NAME carrying one value, or one VALUE across names? A request with `X-VL-Update: 1` and `X-VL-Update: 1` (duplicated same name) or one value under each name must also 403. Fix text: *"the TOTAL number of header values across BOTH names must be exactly 1 and it must equal `"1"` — duplicated values under one name, one value under each name, any other value, all 403"*.
- **Item 5**, plan text: *"… `nofxi-agent-chat:<uid>`, `nofxi-agent-chat-draft:<uid>` → `vl.agentChat…`"*. The ellipsis pins nothing — the migration and the new reader must agree exactly. Fix text as in P1-2 (pin `vl.agentChat`, `vl.agentChat:<uid>`, `vl.agentChatDraft:<uid>`; enumerate by prefix; copy-only-when-absent; delete-only-after-copy; parse/quota failures keep the old key).
- **Item 9**, plan text: *"FAQ/i18n install commands (translations.ts, FAQContent.tsx repo links → github.com/johnwick2921-cyber/vl)"*. Ambiguity: translations.ts's ABOUT strings must NOT be rewritten (they interpolate PERSONA_NAME) while its install-command VALUES must be — "repo links" reads like the about-section and history links are in scope. Fix text: *"in translations.ts change ONLY the install/clone command values (:638, :739 and the zh/id twins) to the new repo; leave the PERSONA_NAME-interpolated ABOUT strings untouched; no other visible string may contain nofx (brand-i18n guard)"*.

---

## Findings ranked

| # | Rank | Finding | Where | Evidence |
|---|---|---|---|---|
| 1 | P1 | Tailwind palette + CSS vars + 545 class occurrences not in the plan; the census allow-list would need ~545 theme entries | `web/tailwind.config.js:10-28,71-72`, `web/src/index.css:19-23,108,192,310`, 545 hits in `web/src` | [A] |
| 2 | P1 | Storage migration algorithm: per-uid prefix enumeration, exact new key names, copy-only-when-absent, delete-after-copy, parse/quota failure rules, named entry point | plan R1b item 4 / D2 item 5 vs `agentChatStorage.ts:1,15,19` | [A]/[B] |
| 3 | P2 | Visible install commands in `translations.ts:638,739` (NoFxAiOS clone/curl) not named; plan only cites FAQContent.tsx repo links | `web/src/i18n/translations.ts` | [A] |
| 4 | P2 | "Exactly one of the two" header rule needs the two-name total spelled out | `api/handler_updates.go:408`, plan D2 item 3 | [A] |
| 5 | P3 | `telegram/agent/apicall_updates_test.go:37-38` header literals in body+query shapes not listed | same | [A] |
| 6 | P3 | :3000 dev server + old bot mismatch window (new header → 403) if the dev server runs from the renamed tree before boot; add a runbook line | [B] |
| 7 | P3 | `deploy/install-autostart.sh` missing from item 6's rename list (unit names, journalctl hint at :114) | [A] |
| 8 | P3 | Test fixtures with nofx paths flag the census grep: `api/release_dir_test.go:65`, `api/handler_updates_starter_test.go:122` | [A] |
| 9 | P3 | Cosmetic comments that say X-NOFX-Update/NOFX: `updates.ts:9,320`, `UpdatesPage.tsx:160`, `UpdatesPage.test.tsx:205`, `strategy-translations.ts:1`, `brand-scope.test.ts:208` — flip or allow-list | [A] |

Verified-correct plan claims (no action): issuer never checked at parse [A]; the CORS allow-list already excludes the header and the new name must simply not be added [A]; `web/dist` serving path and `vite.config`/`index.html` carry no name [A]; the storage-key list covers all six old keys [A]; brand-i18n/brand-visible guards stay valid post-rename [A]; `nofx-web.service` is covered by item 6 [A].

## What I did NOT do
No code change anywhere (the branch adds only this report). No worktree edit outside this file, no deploy, no unit stop/start, no DB read/write, no lock acquire, no web build, no npm install. `go build ./...` + `go vet ./...` clean at the claim head; race tail in the gate section.
