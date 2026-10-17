names rewritten to vl on 2026-09-30 (VL rename)
# Dispatch 102 — visible brand implementation (not deployed)

Updated 2026-09-09. Branch `fix/brand-visible`; lane `brand-visible-0b955fbc/root[unlisted]`; isolated worktree `/tmp/vl-brand-visible`. Accepted from dev `954f11b15f2e7615678f7d2b708c47895faebf1e`, then merged refreshed dev before implementation. This lane owns only the visible-brand changes and pins. It does not own Stage A's archive repair or the held `fix/rebrand-phase-1-2` work.

**Implemented and merged to dev at `05125bd6efffd2a9afd179728f32b29d679f71ac`; not deployed.** Stage A released its lock after publishing its passed-boot marker. This lane acquired the now-free lock on 2026-09-09 with automatic heartbeat started at acquisition; no reclaim was used. Main was updated only by fast-forward under this lane’s lock and pushed from that same tree. No RELEASE update, dist swap, service restart, DB write or gate edit occurred. A new cutover still requires the dispatch's owner GO, safe window and fresh five-leg gate. No live brand result is claimed.

**Latest owner ruling, 2026-09-09: WAIT; do not boot over arm 133.** Conditional GO is now recorded for the first eligible flat gate after that resting order fills, cancels or is superseded, with no working orders, no open position and no planner read in flight. The A7 window remains in force. The earlier proposed one-order exception is not used. No boot has occurred. See the class-33 finding below; it belongs to a separate adopt-or-cancel-by-scenario-validity wave.

## Owner correction — imports are allowed

The owner clarified on 2026-09-09: Section B protects the module path and every existing import target; adding the imports needed by D2 is in scope. The earlier import STOP is resolved. **No existing import target changed.** The module remains `vl`.

Production imports added:

- `vl/branding` in `main.go`, `agent/{agent,i18n,onboard,prompt_persona,scheduler,planner_runtime,skill_domain_context}.go`, `telegram/bot.go`, `telegram/agent/{agent,prompt}.go`.
- Blank `embed` in new `branding/branding.go`.
- `../../../branding/product.txt?raw` and `../../../branding/persona.txt?raw` in `web/src/constants/branding.ts`.
- `../../constants/branding` in ChatInput, ChatMessages, WelcomeScreen, HeaderBar and Guide content/welcome; `../constants/branding` in GuidePage, TraderDashboardPage and i18n/translations.
- `node:fs` in `web/vite.config.ts` to transform the HTML title from the same product source.

The [machine-readable import ledger](2026-09-08-brand-visible-data/added-imports.json) names each file. E2 parses Go imports with `go/parser` and TypeScript imports with the TypeScript AST, comparing existing targets against `954f11b1`. Added imports are allowed; a removed or renamed existing target fails. The Go negative pin changes `vl/config` to `vl/config` in memory and verifies rejection. Test-only added imports are standard parser/test/filesystem/process utilities and `typescript`; they do not change a production target.

## Shared display names and exact strings

`branding/product.txt` contains exactly **VL Intelligent**. `branding/persona.txt` contains exactly **VL**. Go embeds those files behind `branding.ProductName()` / `branding.PersonaName()`; the existing frontend branding module imports those same files. Vite reads the product file for the HTML title. The values are private embedded data, not environment knobs, identifiers or inferred telemetry. No parser/failure fallback/panic is introduced.

Product form: boot banner, page title, dashboard label, Guide heading/card, product persona prose, Telegram system/help prose and README product prose. Short form: chat sender/status/help/greetings/disclaimer, translated Studio labels and header. The pre-existing correct VL translations still render VL, but now consume the shared source.

| Surface | Before | After |
| --- | --- | --- |
| Boot product word | `🚀 VL - AI-Powered Trading System` | `🚀 VL Intelligent - AI-Powered Trading System` |
| Browser title | `VL Trader - AI Trading System` | `VL Intelligent - AI Trading System` |
| Server-authored status card | `VLi Status` / `VLi 状态` | `VL Status` / `VL 状态` |
| Sender label | `VLi · <time>` | `VL · <time>` |
| Input EN/ID fallback | `Ask VLi anything...  ⌘K` | `Ask VL anything...  ⌘K` |
| Input ZH | `跟 VLi 聊点什么...  ⌘K` | `跟 VL 聊点什么...  ⌘K` |
| Disclaimer | `VLi may make mistakes. Always verify trading decisions.` | `VL may make mistakes. Always verify trading decisions.` |
| Welcome ZH | `跟 VLi 聊点什么` | `跟 VL 聊点什么` |
| Guide | `VL System Guide`, `VL / VL` | `VL Intelligent System Guide`, `VL Intelligent` |
| Dashboard brand | `VL Trader` | `VL Intelligent` |
| Registration example | `user@vl.os` | `user@example.com` (example address, not an invented VL domain) |
| README headings/prose | `VL` | `VL Intelligent` |

The boot banner changes its product word only. No new boot line or field is added. Its existing border/padding remains; revision, integrity, units and log file names are untouched. An AST pin checks the **actual main logger call** reads `branding.ProductName()` and rejects even a same-looking literal replacement.

[Every changed string, with source paths, line-numbered hunks and before/after text](2026-09-08-brand-visible-data/visible-strings.patch) is in the full string ledger. [All existing URLs in those sources compare unchanged](2026-09-08-brand-visible-data/urls-unchanged.json). SYSTEM-MAP and Guide content are updated with the implementation in the same commit.

## C1 — reproduced Group 1, corrected scope and counts

Basis: census `docs/superpowers/reports/2026-09-03-rebrand-census.md`, last changed by `878f9e7f089c1e3e01385ff69c0c32e0e8d00ba5`. It is evidence; Dispatch 102 Section B controls scope. Its table has **11 numbered Group 1 rows**, and its summary says **17 locations / approximately 35 direct string hits**, plus 25 CSS hits and 379 docs/i18n hits. Those mixed measures are not a count of authorized replacements.

[A] Initial running baseline was `6f677b55daa1c7da33b8c35f8bcc67883f36b470` (health and `/proc/3726840/exe`, clean VCS build). At resumed acceptance health read `954f11b15f2e`; independently `/proc/438/exe` read full revision `954f11b15f2e7615678f7d2b708c47895faebf1e`, `vcs.modified=false`. This is Stage A's revision, not this brand wave. The relevant visible-brand source remained unchanged between those running revisions. Source references below use the measured resumed baseline; the ledger includes resulting line locations.

| Census row | Reproduced at running source | Action/correction |
| --- | --- | --- |
| 1.1 | `main.go:45`, one VL product word | Changes through shared product source; old census line 44 moved |
| 1.2 | ChatMessages:133; ChatInput:123,124,187; WelcomeScreen:118 — five VLi strings in three files | All five changed; census's “4 files” is not reproduced for this listed frontend set |
| 1.3 | `agent/prompt_persona.go:5-8` — five name occurrences, including first-line persona and product | Five shared-name substitutions |
| 1.4 | `telegram/bot.go:376,387,419,440` — four rendered name occurrences | Four substitutions |
| 1.5 | Telegram prompt:10,19,24 plus agent.go:22 — four occurrences | Four substitutions; tool/API identifiers unchanged |
| 1.6 | Root README:1 heading plus product prose/demo alt/diagram/revenue prose | Six product occurrences changed, rather than only counting the heading |
| 1.7 | docs/i18n has 379 raw case-insensitive matches in 18 files | Only six translated product READMEs change: 25 product occurrences; legal/contribution text, handles and paths excluded |
| 1.8 | SECURITY.md has 13 raw matches | Outside Section B; no edit. External handle is not a product-name replacement |
| 1.9 | index.css has 26 raw matches, not 25 | Invisible CSS namespace is outside scope; all styling identifiers retained |
| 1.10 | welcome.ts:14 `VL / VL`; :37 technical `vl-bin` example | Product card changed; accurate binary example retained. GuidePage:487 heading also needed correction |
| 1.11 | favicon has zero vl text; VL icon already exists | No asset edit |

**Measured implementation count: 78 old product-name/example occurrences corrected**, comprising 33 Go display/persona occurrences, 10 frontend/title/registration occurrences, and 35 root/translated/index README occurrences. This count includes newly found surfaces and README prose omitted by the census's small direct-string estimate; it does not pretend to be a 78-versus-35 like-for-like subtraction. The pre-existing 108 correct VL language occurrences and two frontend shared-name consumers (header/BRAND_INFO) are source consolidation, not old-name corrections.

Additional discoveries: `agent/i18n.go:5,14,25,26,73,74` (six), `agent/scheduler.go:75` (one), `agent/agent.go:376,393,559,647,923,925` (six), onboarding greetings `agent/onboard.go:531,535` (two), Guide title/card, dashboard title, page's noncanonical VL Trader title, and registration placeholder. The stored-name producer `agent/onboard.go:412` (`VLi-%s`) is expressly **unchanged** under Section B.

C2 correction: “VLi Status” is not a standalone status-card component literal. `Agent.handleStatus` formats `agent/i18n.go`; ChatMessages/MessageRenderer renders that server-produced text. The test traverses those production call sites.

## D3 / E3 — language coverage

`web/src/i18n/translations.ts`: **EN 37, ZH 37, ID 34** existing VL occurrences now read the shared short name; zero old product-brand occurrences remain in tested language values after excluding URL/environment identifiers. The two placeholder branches are EN (also the existing ID fallback) and ZH. The status handler similarly preserves existing EN fallback for ID. No translation coverage was fabricated or claimed where fallback exists.

Agent translation source `agent/i18n.go`: three name occurrences per EN and ZH (help, status, persona prompt). Telegram `/start` and `/help`: two per EN/ZH. The separate strategy-translations.ts names the **external VLOS provider** in EN/ZH/ES and remains unchanged, including its existing language availability.

Translated README occurrence counts: JA 4, KO 4, RU 4, UK 4, VI 4, ZH-CN 5; root README 6; bilingual language-index README 4. These are actual product-name occurrences; paths, referral code and external links remain unchanged.

## E1–E6 — measured tests and mutations

[A] Initial production source: **8/8 rendered pins RED**. Status expected VL but received VLi; placeholders expected VL but received VLi; title expected VL Intelligent but received VL Trader; Guide's new heading was absent. The initial Vite/jsdom environment failure was discarded; the valid title RED came from Vite's actual HTML transformation in Node. Preflight commit `66e2c09a8709d178f0cffc83eb470b809ce9e440` records those failures.

[A] Implementation: rendered pins **8/8 GREEN**; scope suite **18/18 GREEN**; language suite **4/4 GREEN**. The frontend fixture executes the real Go status handler, then the real ChatMessages renderer. It does not copy the expected status text into both sides.

[A] Full frontend suite: **57 files / 408 tests PASS**; `tsc --noEmit` PASS. Full `go test ./...` PASS. Targeted Go display/import tests PASS. Embedded prompt self-check `TestVerifyPromptGoldensPasses` PASS; no golden file changed. New code introduces no time predicate or clock seam; class 60 is not applicable.

[A] E5 mutations (each restored in a finally block):

- `branding/persona.txt:1`: exact bytes **VL → VLi**, surface suite exit 1; status/sender and input assertions fail.
- `branding/product.txt:1`: exact bytes **VL Intelligent → VL**, surface suite exit 1; page-title and Guide assertions fail.
- `web/src/i18n/translations.ts` EN `appTitle`: **PERSONA_NAME → 'VL'**, language suite exit 1; EN appTitle fails.

[Mutation receipts](2026-09-08-brand-visible-data/mutations.json). E2 additionally rejects in-memory deletion of the exact JWT guard `&& token.Valid`; no auth file is edited for that negative check. Module/import guard rejects `vl/config → vl/config`. Boot-call guard rejects replacing `branding.ProductName()` with a literal `"VL Intelligent"` even though the visual result would match.

[Exact mutation failure text](2026-09-08-brand-visible-data/mutation-failures.json). A follow-up production-call trace found finalPlanResponseSystemPrompt in agent/planner_runtime.go still used VLi in its EN/ZH user-reply identity. TestUserReplyPersonaUsesVisibleName first failed for EN/ZH/ID, then passed after only those two persona strings changed. The two corresponding recommendation-persona references in skill_domain_context.go and four product references in docs/i18n/README.md were also corrected. Internal planning/module labels and all instruction logic remain unchanged.

E4/A29: the production consumers are nonzero and explicit: `branding.ProductName()` / `PersonaName()` in the eleven Go rendering/persona files, `PRODUCT_NAME` / `PERSONA_NAME` in the existing frontend branding module and visible surfaces, and the product file in Vite. None is a test-only constant. [The production call-site ledger](2026-09-08-brand-visible-data/production-call-sites.json) records each call site; the boot and status pins exercise production callers.

E6 will be rerun at the **final merged release HEAD immediately before its clean-clone build**. Current green source includes refreshed dev, but is not a release or proof of an unperformed build. A green branch is not substituted for the required final merged-head suite.

## C3 / E2 — identifiers left alone

Sixteen protected source files remain byte-identical to the measured base: Go module, JWT mint/validator, log filename logic, service and timer units, deploy lock/claim/backup scripts, TCP server/schema, NT8 AddOn and chat-storage keys. E2 compares their hashes and all existing Go/TS import targets in changed files. No existing URL in the changed display sources changed.

- Units/ExecStart: `deploy/vl.service:41` and `deploy/systemd-user/vl-backup.service:7`; binary and repo paths are operational references.
- Lock: `deploy/vl-lock.sh:43`, `vl-main.lock.d`; claim and backup paths unchanged.
- Module: `go.mod:1`, `module vl`; all existing import targets preserved.
- JWT: `auth/auth.go:93`, issuer `vlAI` unchanged. **Correction:** its ValidateJWT function does not explicitly require that issuer; an issuer-specific consumer requirement is not established from that function. Signature/method/validity behavior is unchanged.
- Logs: `logger/logger.go:90` constructs `vl_%s.log`; no log glob renamed. A specific current reader-glob dependency is **NOT ESTABLISHED**, not invented to make C3 read stronger.
- Wire: `provider/ninjatrader/tcp_server.go:1749` emits `vl-go`; tcp_framing.go:114 documents the source pair. No NT8/wire edit.
- DB: config's existing DB filename and store schemas remain unchanged. This wave has no migration and no stored-value edit. Browser chat keys remain in `web/src/lib/agentChatStorage.ts`; the new-trader stored-name producer remains in agent/onboard.go.
- GitHub/raw URLs and external handles remain exact. C3's blanket premise that **every** excluded string is proven load-bearing is too strong: exclusions remain in force even where a reader dependency was not established.

## A15 / A20 — live truth and retained text

**Not live:** the initial unauthenticated walk observed the old title, Agent disclaimer `VLi may make mistakes.` and placeholder `Ask VLi anything...  ⌘K`, plus technical references in FAQ; protected pages redirected to login. [Initial route results](2026-09-08-brand-visible-data/live-before.json). Automated approval review rejected reading JWT secret material to mint a token. The owner selected **“Use an owner-authenticated browser instead”** and subsequently supplied credentials for normal browser sign-in. That sign-in succeeded; no signing-secret read or token minting occurred.

[A] The authenticated read-only walk on 2026-09-09 at 13:50 CT reached Agent, Traders, Dashboard, Strategy, Guide, Settings and Welcome without a login redirect. All seven retained the live title `VL Trader - AI Trading System`; Agent rendered `VLi` and `Ask VLi anything...  ⌘K`, Dashboard rendered `VL Trader`, and Guide rendered `VL`. [Authenticated route evidence](2026-09-08-brand-visible-data/authenticated-ui-before.json). Output is restricted to product-word matches and brand placeholders; credentials, tokens and account names are excluded. This proves the current release still serves the old Group 1 text; no after-boot proof is claimed. Unit-rendered status/sender text remains code evidence, not an observed new live message.

Known retained old-name occurrences, deliberately outside Section B:

- Guide stack example: `vl-bin` in welcome.ts. Actual binary/clean-clone/log/process/unit/path names remain vl; held identifier wave `fix/rebrand-phase-1-2` is the relevant existing dispatch, not this lane.
- FAQ/README/translation examples: current clone/build commands, env keys, repo/raw URLs and external community handles. The held phase-1-2 branch is not merged by this lane; GitHub/external renaming is later scope.
- Strategy configuration: **VLOS** is an external provider label, not a claim that this product is VL. Renaming that provider is not part of Dispatch 102.
- Historical chat content, stored trader labels (including the `VLi-` producer), localStorage keys and API health's existing agent identifier remain untouched. Stored-value/API migration is later scope, not retrospectively attributed to this lane.
- `.github/SECURITY.md`, translated legal policies, older reports and operational docs retain historical text. Section B authorizes product READMEs and the Guide, not a repository-wide prose sweep.

The after-boot A15 list is **pending observation**, not asserted empty. No remaining current Group 1 product literal was found in the changed executable rendering sources. Internal orchestration prompts still contain VLi as an internal module persona; those non-rendered planning instructions were not swept, and arbitrary model-generated/historical text cannot be claimed scrubbed by a UI rename.

## Cutover, rollback and numbering status

The release candidate and dist have been built in the isolated clean clone, **not swapped**. The candidate Guide was stamped from that binary. Main’s served dist, binary, RELEASE and tracked Guide revision continue to name the existing running release; candidate metadata is not misrepresented as a passed boot. At cutover: coordinate lock succession, acquire with heartbeat at acquire, merge current dev, enumerate checklist classes with `uniq -c`, assign the new class, run full merged suite in a clean clone named **vl**, build a clean VCS binary, read its actual vcs.revision, stamp Guide, then build dist. Read own five-leg gate including broker leg 4 and in-flight state. Apply A7 (14:45–16:30 CT, or after 17:10 flat/no arms/no position); no mid-session override is inferred from “continue.”

Preserve the binary actually running at cutover as `vl-bin.old.<verified held revision>`, with DB/dist rollback artifacts. RELEASE → atomic mv → VERIFY → **print the exact owner kill command**; do not execute it under this dispatch. After an acknowledged good boot, verify all five references, read the real banner, push marker from the same main tree before releasing the lock. No old gate-script exception or previous wave's override is reused.

Pre-assignment numbering census (both formats, sorted with `uniq -c`): highest **93**; existing duplicates **75/76/77** each count 2. At merge preparation the repeated `sort -n | uniq -c` census again showed highest 93; **class 94** is assigned to this wave. Existing duplicated classes are untouched. Class 94 cites C1 and the display/identifier scope boundary. The implementation and this report are **on dev**, first published there at `05125bd6efffd2a9afd179728f32b29d679f71ac`. Publication is not a claim that the candidate is live.

## Source freshness

[Exact `git log -1 --format='%h %aI %s' -- <file>` outputs for every changed production file and basis document](2026-09-08-brand-visible-data/source-freshness.json). The census last changed at 878f9e7f; SYSTEM-MAP and AUDIT-CHECKLIST at 954f11b1. Neither spec moved after this implementation base. Recheck at merge.


## Integration receipt

Current dev advanced with Stage A’s passed-boot receipts and the separately authored range-fade research report. They were incorporated by merge; this lane did not author them. The prescribed pull --rebase flattened local integration history, so the first main fast-forward attempt correctly refused and left main unchanged. Current dev ancestry was restored on the isolated branch, then main fast-forwarded and pushed successfully at **05125bd6**. No reset, forced update or peer-file deletion was used.

The ordinary clean clone is `/tmp/brand-visible-build/vl`, pinned to that merged HEAD, with clean porcelain before suite/build. Its frontend suite is **57 files / 408 tests PASS**, TypeScript PASS. The full merged Go suite and embedded prompt goldens **PASS**. The candidate build and dist **PASS**, with the binary stamp verified below. This is preparation; the subsequent authenticated broker-backed gate failed leg 4, and owner GO and boot remain outstanding.


## Clean-clone build receipt — ready for owner-controlled cutover

[A] Build source **05125bd6efffd2a9afd179728f32b29d679f71ac**, the merged dev HEAD. Ordinary clone leaf directory **vl**: `/tmp/brand-visible-build/vl`. Full Go suite, 57 Vitest files / 408 tests, TypeScript and embedded prompt goldens passed there before the build. Actual `go version -m vl-bin.next` reads **vcs.revision=05125bd6efffd2a9afd179728f32b29d679f71ac**, **vcs.modified=false**. Binary SHA-256 **070ef7cf432da6e11201996f5512e29134c0bb0b9fc0b10480bbd7d17aae6d5b**.

[A] Only after that binary existed, its embedded revision was parsed to stamp the **candidate clone’s** `GUIDE_BUILT_REV` from 954f11b1… to full 05125bd6…. Dist was then built: **92 files**; its HTML title is `VL Intelligent - AI Trading System`; its compiled JS contains that exact Guide revision. [Build validation](2026-09-08-brand-visible-data/build-validation.json) and [full binary/dist manifest](2026-09-08-brand-visible-data/candidate.json). The clone’s Guide change is intentional post-binary metadata and does not change the already verified clean binary stamp. It will be carried into the release metadata at the authorized swap, not silently served ahead of the binary.

[A] A separate loopback preview of the actual candidate dist rendered `VL Intelligent - AI Trading System`, `Ask VL anything...  ⌘K`, and `VL may make mistakes.` in the connected browser. [Exact preview text](2026-09-08-brand-visible-data/candidate-preview.json). This is evidence from **4173**, not a claim that 8080 or the Go status/banner has changed. The subsequent owner-authenticated live walk is recorded above. The preview process was stopped after recording this evidence (exit 130); the live service was not stopped.

**No cutover performed — STOP at leg 4.** [A] The owner-authenticated read-only gate at **2026-09-09 13:50:06 CT** returned HTTP 200 and `ready=false` for trader id `8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265`. [Exact five-leg evidence](2026-09-08-brand-visible-data/authenticated-gate.json):

| Leg | Verdict | Observed evidence |
|---|---|---|
| 1 DB positions | PASS | `0 open row(s)` |
| 2 API positions | PASS | `0 position(s)` |
| 3 NT8 positions | PASS | `count=0` |
| 4 Working orders | **FAIL** | `broker 1 vs ledger 3 — MISMATCH`; broker snapshot age 2s, build `2026-09-07-h1`, cross-checked against armed_orders ledger |
| 5 Planner in flight | PASS | `no planner read claimed` |

Leg 4 sample ids: broker `db758fae647843aa93b691bc8ffc6df6`; ledger `5d4716c8-298a-4f86-bc72-4d6529d73483` plus two empty order ids, quoted as returned. An earlier independent read at 13:49:42 CT also failed leg 4. No root cause is inferred from these counts. Dispatch A5 forbids overriding this mismatch; A23 requires STOP/report/wait. No gate, order, arm, binding or DB correction is authorized by this visible-brand wave, and none was made.

No owner GO has been given for this swap, and the observation precedes the 14:45 CT A7 window. Successful browser authentication resolves the earlier access prerequisite, not the failed gate. No kill is printed before RELEASE → mv → VERIFY, and no stale PID command is offered. Full Go, 57 Vitest files / 408 tests and TypeScript also passed at report-only receipt HEAD `efdff525652b32c4f3bc61e9a358d4dce2dba01e`; this subsequent report/evidence update changes no executable source.

This preparation receipt is pushed from the same clean main tree under the lock before releasing it. Lock release ends preparation only; the future cutover must reacquire, check current dev/source equivalence, rerun any required merged validation if source changed, make fresh backups/gates and obtain the owner GO. The report will be updated with actual boot/five-reference evidence after that boot; it is not a boot marker today.

[A] Publication check: the complete authenticated census/failed-gate receipt at [commit e3a6de6dcdb267942c9d6eabce3cdc88aec1032a](https://raw.githubusercontent.com/johnwick2921-cyber/nofx/e3a6de6dcdb267942c9d6eabce3cdc88aec1032a/docs/superpowers/reports/2026-09-08-brand-visible.md) returned **HTTP 200, size_download=24,901 bytes**; `git ls-tree -l` reported **24,901 bytes**, and `cmp` passed. Origin dev and fix/brand-visible both resolved to that commit after the push from main. This subsequent publication note does not amend source, tests or gate evidence. The final report revision is separately checked after its push; no self-referential commit id is fabricated.

## Arm 133 comparison and class-33 boot sweep finding — owner WAIT

[A] A read-only SQLite transaction at **2026-09-09 13:57:47 CT**, scoped to trader `8d5c8af5_8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek_1781246265` and the production non-terminal state set, found:

| Ledger id | Scenario | State | signal_id | Entry |
|---|---|---|---|---:|
| 133 | S1 | working | `5d4716c8-298a-4f86-bc72-4d6529d73483` | 29424.50 |
| 134 | S2 | armed | empty | 29424.50 |
| 135 | S3 | armed | empty | 29503.25 |

Broker snapshot **14449**, received at epoch-ms **1788980251312**, was **16.17 seconds old** at that read. Its one MNQ limit order was `db758fae647843aa93b691bc8ffc6df6`, name `5d4716c8-298a-4f86-bc72-4d6529d73483`, price **29424.50**, state **Working**. That name and price match row 133 exactly. Age is snapshot freshness, not order lifetime. The two extra ledger rows are authorized, never placed; neither claims a signal absent from the broker book. The persisted frame is forensic evidence; the gate itself reads the live in-memory broker snapshot. The snapshot envelope symbol is empty; MNQ is on the contained order, so an envelope `symbol='MNQ'` filter does not find this frame.

[A] The fresh authenticated gate at **13:59:32 CT** still read broker **1** / ledger **3** with snapshot age **2 seconds**, while legs 1/2/3 were flat and leg 5 reported a planner read **IN FLIGHT** for the same trader's `2026-09-09:NY` chain. No override was executed, no gate was edited, and no arm or order was changed.

**Finding:** the class-33 sweep cancels prior-process placed orders without checking scenario validity. It cannot distinguish an orphan from an intended resting order whose scenario remains valid. Precisely, `store/boot_sweep.go:42-48` selects this trader's `armed`, `place_pending` and `working` rows with a different/null boot id; `trader/class33_boot_sweep.go:78-87` skips empty signal ids and calls the production `nt.CancelOrder` delegate for every other selected row. It does not literally enumerate every broker order: its selection is the ledger state set above. No scenario-validity check exists on that path. The production call site is `trader/armed_executor.go:210`.

[A] Rows 133/134/135 carried boot id `438-1788976927048`. A new process receives a different id (`store.ProcessBootID`, pid plus first-call timestamp), so row **133** would be selected and a cancel sent after restart if it remained working. Rows **134/135** would instead be left armed; the earlier claim that the sweep clears never-placed residue is corrected. This conclusion comes from the actual selection/delegate path, not a live cancellation experiment. Scenario validity was not independently re-evaluated in this read-only comparison; the defect is that the sweep never asks it.

Running source pinned at **954f11b15f2e7615678f7d2b708c47895faebf1e** and candidate source **05125bd6efffd2a9afd179728f32b29d679f71ac** have identical sweep behavior. Source freshness: `git log -1 -- trader/class33_boot_sweep.go` = `6310eaf8 2026-09-07T23:51:51-05:00 merge: reconcile placement confirmation with pre-send identity and owner ruling`; `git log -1 -- store/boot_sweep.go` = `6262bf42 2026-09-07T23:34:58-05:00 fix: stamp entry creation time and await received placement truth`.

**Owner disposition:** separate wave, **adopt-or-cancel by scenario validity**. Dispatch 102 remains docs-and-strings and does not change this execution path. WAIT until arm 133 becomes terminal and the broker has no working orders; a superseding plan alone is not proof of broker cancellation. Recheck all five legs immediately before any swap, including positions after any fill and the planner in-flight claim. The conditional GO does not authorize booting over arm 133, editing a gate to pass, or bypassing A7. No boot marker is written until a real passed boot; any eventual marker must quote the actual cutover state rather than reuse the historical one-order snapshot.
