names rewritten to vl on 2026-09-30 (VL rename)
# Sunday-Shield Wave — W1–W4 build & park record

**Branch:** `fix/sunday-shield` · **Base:** `da0d6fe1` (dev tip) · **Rev target:** `b0549ff2`-era findings fixed, parked for the owner's "go cutover" (deploy target: today, well before Sunday 16:00 CT).
**Commit-ref:** `https://github.com/johnwick2921-cyber/nofx/blob/2639ad5c/docs/superpowers/plans/2026-08-29-sunday-shield-wave.md`

## What shipped in this wave

| Item | Fix | Proof |
|---|---|---|
| **W1** | Persist watchdog is now FRAME-AWARE: alarms only when live bar frames flow (`persistLastFrameAt`, stamped on live ingest in `fanOutBarPersist`, never historical) while flushes don't. Idle wire (weekend, daily 16:00–17:00 break, NT8 closed) → silent. | `TestPersistWatchdogIdleWireStaysSilent` + updated `TestPersistWatchdogAlarmFiresOnceAndRecovers` — PASS |
| **W2** | `flushPending` re-queues the unsent TAIL + in-flight signal on mid-flush conn death (was: current signal only, tail silently eaten). | `TestFlushPendingRequeuesUnsentTailOnConnDeath` — PASS (3 queued, 3 re-queued after peer death) |
| **W3** | A REJECTED fill echoing `account:""` (the C# guard-reject path — `SendFillFrame` acctName default) is tolerated on the account leg only. seq+signal_id still prove identity; trader_id still verifies; non-rejected empty account and wrong-account rejects still FREEZE. | `TestCheckEcho` + `TestEchoRegistry_ProcessMatch_FreezeMismatch` extended — PASS |
| **W4** | Guide card `PERSIST_STALL_WATCHDOG_S` updated (frame-aware wording). The `GUIDE_BUILT_REV` bump = the **cutover-marker commit** at "go cutover" (below) — the merge sha cannot exist before the merge (self-reference). | `web/src/guide/content/settings.ts` |

## Gates (all run fresh on this branch)

- `go build ./...` EXIT 0 · `go vet ./provider/ninjatrader/` EXIT 0
- `go test ./...` → **27/27 packages PASS, 0 FAIL** (includes `TestVerifyPromptGoldensPasses` goldens — PASS)
- `web` vitest → **33 files / 277 tests PASS** (includes `GuidePage.test.tsx`)
- gofmt clean on all touched Go files (`tcp_framing.go` pre-existing unformatted — untouched, unrelated)

## W4 — CUTOVER-MARKER SEQUENCE (runs at "go cutover", makes W1–W4 one cutover)

The drift banner compares `GUIDE_BUILT_REV` (prefix) against the Go binary's vcs stamp. Zero-drift sequence for the cutover dispatch:

```bash
# 1. merge (creates merge sha M)
git checkout dev && git merge --no-ff fix/sunday-shield   # M = merge sha

# 2. cutover marker commit R (RELEASE + guide rev → short(M))
sed -i "s/export const GUIDE_BUILT_REV = '[0-9a-f]*'/export const GUIDE_BUILT_REV = '$(git rev-parse --short HEAD)'/" web/src/guide/types.ts
echo "$(git rev-parse --short HEAD)" > deploy/RELEASE
git add web/src/guide/types.ts deploy/RELEASE
git commit -m "deploy: sunday-shield cutover marker — RELEASE=<M>, GUIDE_BUILT_REV=<M>"

# 3. build the Go BINARY from the MERGE commit M (revision stamp = M)
#    build the FRONTEND from dev@R (marker = short(M)) — web/dist is gitignored,
#    nginx serves it from disk, so this yields ZERO drift (marker == revision).

# 4. deploy per the standing protocol: temp-clone build, mv-swap, kill -9,
#    boot-ack from owner, goldens green, 90s observed. NO timers.
```

Expected boot evidence Sunday: `🔕 PERSIST WATCHDOG` lines **0** from deploy to ~17:01 CT (pre-W1: 373 fires since the 08-29 boot); after 17:00 reopen the alarm may only fire if a real flush stall happens while bars flow.

## W5 — PENDING the owner's "key rotated" confirmation (repo-only, no deploy)

Do NOT start before the owner confirms the embedded key was rotated. Then, in ONE dispatch:

1. `git rm` the 14 tracked binaries (`git ls-files | grep vl-bin` → `vl-bin.old.00090003 … f9ac3796`; every sampled binary embeds the same `sk-` key, repo is PUBLIC).
2. `.gitignore`: add `vl-bin.old*` (currently only `/vl-bin` is covered) + keep `vl-bin*` family covered.
3. `git filter-repo --invert-paths --path-glob 'vl-bin*'` history purge (on a fresh clone — do NOT run on the live checkout).
4. **Force-push only on the owner's explicit ack** — history rewrite. All clones/forks must re-clone; **the partner repo `vlautoagenttraderv1` must re-clone** — append this to the Binnie runbook (`deploy/` or P2 report section) as a mandatory step.
5. Verify: `git log --all -S 'sk-' --oneline | wc -l` == 0 · `strings` scan of every remaining tracked binary → 0 `sk-` hits · repo tree scan clean.
6. **AUDIT-CHECKLIST class 20** (append in the SAME dispatch): "committed binaries / embedded secrets: `strings`-scan every tracked binary; binaries are never tracked; `.gitignore` covers every binary glob."

## Not in this wave (queued post-Sunday)

T15 S1 16:55 closed-market read (owner-decide: exempt vs ledger), T15 S3 level_stats Sunday give-up, T5 S1 decision_records prune, T9 S2/S3 text fixes, T7 S2 delete confirm, T1 S1 fantasy-R WARN, T10 armed palimpsest, T12 S1 reconnect census, C# trio (reject-account populate, CancelBracketsFor, dead sub) — owner NT8 copy/F5/restart dance.
