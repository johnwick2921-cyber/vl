// Dispatch 102 freezes load-bearing identifiers, including their surrounding guards.
// Lock baseline advanced after the separately authorized lock-keeper wave:
// deploy/nofx-lock.sh @ ace51598 (fix/lock-defects-release-meta-halfbuilt),
// following keeper @ 97a6525cb6d10d6c8898b2d277c0fe7581872c24.
// Only its recorded hash changes; protected-file mutation checks remain enforced.
// Auth baseline advanced 2026-09-24 for W-ONE-BUTTON M3 (CTO-dispatched,
// feat/one-button-m3-update-authz; red-team H1/H2/M2 + defect 4, each ruled).
// Delta against the PR base (dev 710e96aa): auth/auth.go +78 −4. The four
// removed lines, each re-issued: BlacklistToken's comment; its entry
// `items[token] = exp` → `exp.Add(ClockLeeway)` (a logged-out token must stay
// refused through the parser's 60 s exp leeway); GenerateJWT's comment; and
// `jwt.ParseWithClaims(` → `strictParser.ParseWithClaims(` (strict base64url
// + WithIssuedAt + WithLeeway(ClockLeeway)). Added: the Scope claim, the
// machine-token scopes and Claims.IsMachine (machine tokens are denied the
// credential, Telegram-config and update routes), GenerateScopedJWT, and one
// signToken every mint goes through. The protected guards are byte-untouched:
// the HMAC signing-method check, `&& token.Valid`, the Issuer, and the
// blacklist lookup. No identifier renamed.
// Bar-feed baseline advanced 2026-09-24 for DS-102 U6 (fix/owed-ui-ci-1):
//   provider/ninjatrader/tcp_server.go — the hello handshake log now renders an
//     absent NT8 identity as n/a via helloProcessPair (nt8_pid/assembly_mvid;
//     runbook 2026-09-23-addon-m21-f5.md C5, L7). Additive; no identifier
//     renamed.
// Bar-feed baselines advanced 2026-09-10 for two owner-dispatched waves that
// touched the protected files without renaming an identifier:
//   provider/ninjatrader/tcp_server.go  @ a53359ce (fix/contract-roll: the
//     subscribed ACK now routes through observeContract; rollMu/lastNamed/rolls
//     fields) — the roll wave's vitest ran in the MAIN tree, whose cwd-relative
//     read of this file was the pre-roll copy, so its 421/421 never saw this
//     change (class 110: a green suite is a claim about an environment).
//   provider/ninjatrader/tcp_framing.go @ c9b224a6 (fix/bar-source: Bar.Source,
//     Go-side only, json:"-").
//   ninjascript/VLTraderTCPClient.cs @ ed6bac8b (owner-ordered front-month fix,
//     2026-09-11: the three GetInstrument sites route through VLInstrumentLookup;
//     no identifier renamed). Advanced in cleanup batch 2 — the red was
//     pre-existing on dev since that commit.
// Bar-feed baselines advanced 2026-09-11 for wave 101 (fix/historical-backfill):
//   provider/ninjatrader/tcp_server.go  — bars_history_request/data/error fan-out
//     + SubscribeBarsHistoryFor (additive; no identifier renamed).
//   provider/ninjatrader/tcp_framing.go — the three new frame types + payloads
//     (additive; no identifier renamed).
//   ninjascript/VLTraderTCPClient.cs   — historyPulls wiring + the
//     bars_history_request dispatch (additive; no identifier renamed).
// Bar-feed baseline advanced 2026-09-16 for dispatch 101 (PRs #131/#132,
// fix/nt8-history-and-chart-depth), re-pinned in W-brandscope after PR #140's
// CI found the red — the wave that changed the file never re-pinned it:
//   provider/ninjatrader/tcp_server.go  @ 0252afe4 (dev 1e3ad705, sha256
//     0a757488…): 65f93f6b added the histAtSub field + one
//     s.histAtSub.note(...) call in enqueueBarHistorical (E1, the 🧯 history-at-
//     subscribe count); 0252afe4 added the histReplay field (D1'(3a), the
//     once-per-boot re-request; RequestHistoryReplayAt lives in
//     history_rerequest.go). Additive; the struct block re-aligned by gofmt
//     (addr / listener lines moved, not removed). The guards this pin protects
//     are intact on dev: SubscribeBarsHistoryFor (:421), the
//     bars_history_request write (:470), the bars_history_data / _error
//     fan-out (:1985 / :2008). Verified by diff 0aea0c2e..1e3ad705 on the file.
// go.mod baseline advanced 2026-09-17 for W-CGOFREE-SQLITE-UPSTREAM
// (fix/cgofree-sqlite-upstream): two require lines ADDED —
//   github.com/glebarez/go-sqlite v1.22.0 and github.com/glebarez/sqlite v1.11.0,
// the pure-Go backend behind `go build -tags cgofree` for machines without a C
// compiler (the partner mirror). Nothing removed or bumped: modernc.org/sqlite
// stays v1.40.0, libc stays v1.66.10, gorm.io/driver/sqlite stays v1.6.0. The
// Go security guard the pin protects (patched toolchain/deps) is intact.
// tcp_server.go baseline advanced 2026-09-21 for W-PICTURE-HTF (owner GO,
// merged 23050993): the baseline hash was last pinned at 42c35e2d (the picture
// branch's own 5/6 commit); the merged-HEAD delta vs that pin is EXACTLY the
// 111 inserted lines of the CTO-reviewed coordinated order-update fan-out —
// TCPServer fields (ouFanMu/ouFanNext/ouFanouts) + ListenOrderUpdates +
// runOrderUpdateFanout + the two test feed hooks. ADDITIVE only: zero
// removals, no identifier renamed, no guard removed; the bar-feed guards this
// pin protects (SubscribeBarsHistoryFor, bars_history_request write,
// bars_history_data/_error fan-out) are byte-untouched by that delta.
// Wire baselines advanced 2026-09-22 for W-ONE-BUTTON M2 site 7 (CTO-dispatched,
// feat/one-button-m2-maintenance-hold; the TCP schema must change in lockstep):
//   provider/ninjatrader/tcp_framing.go — +59 −0: HelloPayload's five omitempty
//     epoch fields + the maintenance / maintenance_ack frames and payloads.
//   provider/ninjatrader/tcp_server.go  — +94 −6: the maint field, the accept-time
//     connection record + maintenance push, the hello/ack record calls, and the
//     queue's hold drop. The six removed lines are flushPending's return shape
//     (it now wraps flushPendingReport), one comment, and the hello log's extra
//     args — the stale-age check and the requeue-on-error guard are intact.
//   ninjascript/VLTraderTCPClient.cs   — +197 −3: VL_BUILD_ID 2026-09-20-p1 →
//     2026-09-22-m2 (lockstep with ExpectedAddonBuild), SendHello builds its
//     dictionary before the same WriteEnvelope (hello is still the first frame),
//     HandleMaintenance + census ack + the entry refusal. No identifier renamed.
// Wire baselines advanced 2026-09-23 for W-ONE-BUTTON M2.1 (review hardening;
// feat/one-button-m2.1-hardening), each delta measured against the M2 head:
//   provider/ninjatrader/tcp_framing.go — +4 −0: CensusConnection.Settled.
//   provider/ninjatrader/tcp_server.go  — +16 −4: the flush re-checks the hold
//     with the writer lock held (review F1), and threads the caller's own signal
//     id through it so a drop inside its own send is reported Own (review N2).
//     The four removed lines are M2's own flushPendingReport / reportDrops call
//     shapes, re-issued with the id; no pre-M2 line, guard or identifier touched.
//   ninjascript/VLTraderTCPClient.cs   — +60 −38: VL_BUILD_ID 2026-09-22-m2 →
//     2026-09-23-m21; the census M2 added is restructured to snapshot each NT8
//     collection under its own lock (no nesting) and to report 'settled';
//     source_hash is taken at activation. Every removed line is M2's own census
//     code; no pre-M2 line, guard or identifier is touched.
// Wire baselines advanced 2026-09-23 for W-EXEC-TRUTH W4 (PR #192,
// fix/w4-picture-evidence; found RED by the CTO's own gate at 17:03 CT — the
// wave's PR line had reported the GUIDE vitest subset, 5 files / 24 tests,
// which is a claim about a subset and not about the suite: class 110 again,
// this time on the lane that had just filed it against others). Deltas
// measured against the PR base eb7294c9:
//   provider/ninjatrader/tcp_framing.go — +12 −2: the Contract field on
//     BarsHistoricalPayload and BarUpdatePayload (json contract,omitempty)
//     plus their doc comments. The AddOn has stamped `contract` on EVERY bar
//     frame since 2026-09-11 (VLBarsSubscriptionManager.cs:488 / :562) and Go
//     had no field for it. The two removed lines are the two `Bars []Bar`
//     lines re-issued with gofmt's new field alignment — nothing else. No
//     identifier renamed, no frame type changed, no C# change in the wave.
//   provider/ninjatrader/tcp_server.go  — +20 −5: barIngestMsg.contract; the
//     D24 liveFrameTooOld gate around the fan-out in drainBarIngest (a frame
//     that reached us long after it was emitted is CACHED but not treated as
//     a live entry event); and the contract threaded through enqueueBarUpdate
//     to its readLoop caller. EVERY removed line is re-issued with the
//     contract added: the `bars []Bar` field (gofmt realignment), the
//     fanOutLiveBars call (now in the else branch), enqueueBarUpdate's
//     signature, its barIngestMsg literal, and its call site. The cache write
//     (s.barCache.Upsert) is byte-untouched and still runs for every frame,
//     including a refused one. No identifier renamed, no guard removed.
//   The bar-feed guards this pin protects are byte-untouched by both deltas:
//   SubscribeBarsHistoryFor, the bars_history_request write, and the
//   bars_history_data / _error fan-out.
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { expect, it } from 'vitest'
import baseline from './test/brand-scope-baseline.json'

function verifyScope(path: string, bytes: Buffer, expected: string) {
  if (createHash('sha256').update(bytes).digest('hex') !== expected) {
    throw new Error(`Dispatch 102 protected file changed: ${path}`)
  }
}
it.each(Object.entries(baseline))(
  'preserves %s byte for byte',
  (path, hash) => {
    expect(() =>
      verifyScope(path, readFileSync(resolve('..', path)), hash)
    ).not.toThrow()
  }
)
it('rejects a removed protected guard, instead of only checking the issuer word', () => {
  const path = 'auth/auth.go'
  const original = readFileSync(resolve('..', path), 'utf8')
  const removed = original.replace('&& token.Valid', '')
  expect(removed).not.toBe(original)
  expect(() => verifyScope(path, Buffer.from(removed), baseline[path])).toThrow(
    'protected file changed'
  )
})

it('preserves every existing TypeScript import target in changed files', async (ctx) => {
  const { execFileSync } = await import('node:child_process')
  const ts = await import('typescript')
  const base = '954f11b15f2e7615678f7d2b708c47895faebf1e'
  const git = (args: string[]) =>
    execFileSync('git', args, {
      cwd: resolve('..'),
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
    })
  // The base is a nofx commit. A mirror clone (the VL partner repo) does not
  // carry nofx history, so the pin cannot be evaluated there: skip with the
  // reason stated instead of failing on `git diff` (bad object). In nofx itself
  // the commit exists and the check runs unchanged. TypeScript twin of the Go
  // skip in branding/scope_test.go (TestExistingGoImportTargetsPreserved).
  let baseIsPresent = true
  try {
    git(['cat-file', '-e', `${base}^{commit}`])
  } catch {
    baseIsPresent = false
  }
  if (!baseIsPresent)
    ctx.skip(
      `base commit ${base.slice(0, 8)} is not in this repository (mirror clone) — import-target pin not evaluable here`
    )
  const targets = (source: string) => {
    const file = ts.createSourceFile(
      'source.tsx',
      source,
      ts.ScriptTarget.Latest,
      true,
      ts.ScriptKind.TSX
    )
    return file.statements.flatMap((node) =>
      ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)
        ? [node.moduleSpecifier.text]
        : []
    )
  }
  for (const path of git(['diff', '--name-only', base, '--', '*.ts', '*.tsx'])
    .trim()
    .split('\n')
    .filter(Boolean)) {
    let old: string
    try {
      old = git(['show', `${base}:${path}`])
    } catch {
      continue
    }
    const current = targets(readFileSync(resolve('..', path), 'utf8'))
    for (const target of targets(old))
      expect(current, `${path}: existing import target ${target}`).toContain(
        target
      )
  }
})
