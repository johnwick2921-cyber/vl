// Dispatch 102 freezes load-bearing identifiers, including their surrounding guards.
// Wire baseline advanced 2026-10-26 for m433 (fix/m433-install-gate-broker-down), Go-only:
//   provider/ninjatrader/tcp_server.go  sha256 f5d05417… — the FramePositions case hands an
//     optional persistence hook (SetPositionsSink) the account and its non-flat count, after
//     the cache write and outside the lock. ADDITIVE: no wire schema change, no guard removed.
// Wire baseline advanced 2026-10-23 for m416 (fix/m416-sonnet-final-bar-persist), Go-only:
//   provider/ninjatrader/tcp_server.go  sha256 d399f97d… — enqueueBarUpdate eviction now spares
//     final-carrying frames (evictForIngest, ingestEnqMu). No wire schema change.
// Wire baselines advanced 2026-10-21 for release #43 re-integration:
//   ninjascript/VLTraderTCPClient.cs sha256 65b120fa… — reconnect backoff resets
//     only on the server hello, hello-first write gate, unreadable connect.json
//     classified corrupt, empty-key error (fix/addon-reconnect-json 9639e2e5b,
//     DS-103) and VL_BUILD_ID advances to 2026-10-21-c; the order name / account
//     / signal-id contract is untouched. #45: bug-hunt #4 (m227 25e3743dc, DS-103)
//     re-keys the order_update de-dupe to state:filled so each larger PartFilled
//     partial is sent — the order-name / account / signal-id contract is untouched.
//     #46: m337 (f9dfe5e14, DS-103) bumps VL_BUILD_ID to 2026-10-22-a and adds a
//     sha256 guard over all 6 ninjascript/*.cs files (trader/addon_source_snapshot_test.go);
//     the wire / order-name / account / signal-id contract is untouched.
//     #46: m373 (P0, DS-103) fixes order_update FormatException — e.Filled is an
//     int so ToString("R") throws on every order_update (47× in NT8 log since
//     #45 boot); drops "R", bumps VL_BUILD_ID to 2026-10-22-b. Contract untouched.
//     #48: m387 (DS-102) CancelBracketsFor cancels EVERY (account,root)-matched
//     bracket and retires each key only after its own orders were cancelled
//     (QUEUE-47); adds the anchored VL_BUILD_ID_ANCHOR marker and bumps
//     VL_BUILD_ID to 2026-10-22-c. Wire / order-name / account / signal-id
//     contract untouched.
//     #48.2: m400 (DS-102) makes VL_BUILD_ID_ANCHOR static readonly (not const)
//     so the folded marker is a #US user string with a terminal byte instead of
//     a #Blob const blob that the INSTALLED older reader misread as a trailing
//     '0'; bumps VL_BUILD_ID to 2026-10-22-d. Wire / order-name / account /
//     signal-id contract untouched.
//   provider/ninjatrader/tcp_server.go  sha256 a59169a4… — connect-key auth gate
//     root fix (fix/connect-key-auth-gate e64281dca, DS-104) + m188 keyed-conn
//     deadline fix (7f1a89133, CTO): a pre-auth dial is held as a pending conn
//     and published as s.conn only after a matching hello; the first-hello
//     deadline is cleared on auth and connGone runs in a closure so a close frees
//     the slot. Additive; no frame/order/position path changed. #45: m220 #36
//     (ecc0053ea, DS-104) makes sampleIngestDepth session-day aware (CMESessionDayStart)
//     and CAS against the previously stored day — additive; no wire change.
// Wire baselines advanced 2026-10-19 for P0 feed-freshness (release #40,
// C# fix/p0-addon-feed-freshness 52a88a004 + Go/UI fix/p0-feed-freshness-status
// f5ff7e0ae): the additive feed_status freshness field is added to the TCP wire
// schema (C# AddOn + Go framing + server, changed in lockstep). Additive only —
// no identifier renamed, no frame/order/position path touched.
//   ninjascript/VLTraderTCPClient.cs    sha256 7aae2529… — additive feed_status.
//   provider/ninjatrader/tcp_framing.go sha256 9b5c00cf… — decode feed_status mode.
//   provider/ninjatrader/tcp_server.go  sha256 bdd96a75… — session-scope feed mode.
// go.mod baseline advanced 2026-10-22 for m391 (release #48): the friend app
//   (cmd/vl-friend) merged from friend/build63 @2ebcd2ee0 promotes
//   golang.org/x/sys v0.48.0 and golang.org/x/term v0.46.0 from indirect to
//   direct (cmd/vl-friend/run_windows.go imports both). No wire/identifier
//   change; the app exe remains byte-identical (fb20c28a).
// go.mod baseline advanced 2026-10-20 for GO-2026-6809 (release #42): the go
//   directive 1.26.9 -> 1.26.10 (crypto/x509 fix); no require line changed.
// go.mod baseline advanced 2026-10-13 for GO-VULN-1026 (owner order 2026-10-13):
//   go 1.26.9 + golang.org/x/net v0.60.0 — clears GO-2026-6617/6613/6612.
//   sha256 58e9a9bc… — toolchain bump only; no identifier, frame or wire change.
// Wire baseline advanced 2026-10-04 for the UPDATER-NT8-CLOSED never-connected
// seed (fix/updater-nt8-never-connected, DS-101):
//   provider/ninjatrader/tcp_server.go   sha256 5451949a… — Start records the
//     listener-up stamp (maint.listening / maint.listenMono, under maint.mu) and
//     SetListeningSinceForTest (tests only). Additive; no identifier renamed, no
//     frame, handshake, order or position path touched.
// Wire baselines advanced 2026-10-03 for PR A (cancel-confirm report regime,
// feat/cancel-confirm-a, DS-101):
//   provider/ninjatrader/tcp_framing.go  sha256 d3a570e4… — SignalPayload gains
//     cancel_report (parse-only, omitempty) + the cancel-report build floor and
//     CancelReportRegimeOn. Additive JSON; no identifier renamed.
//   provider/ninjatrader/tcp_server.go   sha256 5a59d739… — the order_update
//     read loop drops a cancel-report echo when the regime is OFF, before any
//     consumer. Knob-gated; no identifier renamed.
//   ninjascript/VLTraderTCPClient.cs     sha256 b0c7f01a… — SendCancelReport
//     echo (cancel_report=true) + the R5 account.txt branch drop + build id
//     2026-10-03-c1. The order-name / account / signal-id contract is
//     byte-untouched.
// Stack top (PR B, feat/stop-limit rebased on #312, 2026-10-03) re-pins the two
// files PR B touches; tcp_server.go is untouched by PR B and keeps 5a59d739…:
//   ninjascript/VLTraderTCPClient.cs     sha256 c3f4b51a… — build id
//     2026-10-03-c2 + the stop_limit construction (OrderType.StopLimit with
//     LimitPrice == StopPrice) and the stop-limit cancel-request type label.
//     The protected order-name / account / signal-id contract is byte-untouched.
//   provider/ninjatrader/tcp_framing.go  sha256 e46c12be… — SignalPayload.StopLimit
//     + MinAddonBuildStopLimit (c2). Additive JSON; no identifier renamed.
// OWNER RULING 2026-09-30 (plan v7 FINAL R1b.10 / Z21, reverses the 09-08
// Dispatch 102): the protected namespace is now `vl/…`; the old module prefix
// is forbidden (Go: TestNoOldModuleImport; this file's base stays the
// pre-rename commit — no TS import target holds the old token).
// Ops baselines advanced 2026-09-30 for RENAME-R1a (feat/rename-vl-r1a, DS-103,
// owner ruling 2026-09-29 "dual readers"): three pinned deploy scripts become
// dual readers by dispatch, not by drift —
//   deploy/vl-claim.sh      sha256 99d09313… — VL_SESSION wins over
//     VL_SESSION (the shell twin); the refusal names both keys.
//   deploy/vl-db-backup.sh  sha256 10c0cfbf… — D1-FOLD (DS-105): the
//     prune also handles vl-*.db.gz beside the vl-*.db.gz it always pruned
//     (write side stays vl until the rename boot; R5 removes the vl prune).
//     VL_ → VL_ → default; DB/DB_RESEARCH defaults use the install-root rule
//     ($HOME/vl when present, else $HOME/vl — never /home/hoang).
//   deploy/vl-lock.sh       sha256 bb0b09d5… — the five lock envs are the
//     shell twin; the lock dir defaults to ~/vl-main.lock.d.
// The protected guards are byte-untouched by all three deltas (R5 removes the
// VL twins later).
// Auth baseline advanced 2026-09-26 for FIX-SEC (fix/sec-0926-auth, DS-106,
// audit 0926-system): auth/auth.go adds the TokenBlacklistStore interface +
// fingerprint (P2-10 persistence behind the memory map) and the
// BlacklistStoreEnabled reader (B-rule pass re-pinned the hash again — the
// protected guards are byte-untouched: the HMAC signing-method check,
// `&& token.Valid`, the Issuer, and the blacklist lookup; no identifier
// renamed).
// Lock baseline advanced after the separately authorized lock-keeper wave:
// deploy/vl-lock.sh @ ace51598 (fix/lock-defects-release-meta-halfbuilt),
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
// Bar-feed baseline advanced 2026-10-15 for WAVE 26 bar-stale-print (fix/bar-stale-print,
// PR #537; re-pinned by the CTO on release/2026-10-15-1):
//   provider/ninjatrader/tcp_server.go sha256 3f59f5a6… — the accept loop and the
//     bars_subscribe send arm the stale-print guard (NoteReconnect / rebuild
//     triggers). Additive call sites only; no identifier, frame or wire change.
// Wire baseline advanced 2026-10-15 for DEADMAN-ACCEPT-NS (fix/deadman-accept-ns,
// PR #555, DS-101):
//   provider/ninjatrader/tcp_server.go sha256 8aaa9e53… — acceptClock seam +
//     acceptNow at the accept: the accept loop stamps the accept instant through
//     an acceptClock func (nil = time.Now) so a test can state its own, and
//     SeedPositionsForTest delegates to SeedPositionsForTestAt (receipt instant
//     passed in). Additive; no identifier, frame or wire change; the accept-time
//     connection record and the bar-feed guards this pin protects are
//     byte-untouched.
// Bar-feed baseline advanced 2026-09-24 for DS-102 U6 (fix/owed-ui-ci-1):
//   provider/ninjatrader/tcp_server.go — the hello handshake log now renders an
//     absent NT8 identity as n/a via helloProcessPair (nt8_pid/assembly_mvid;
//     runbook 2026-09-23-addon-m21-f5.md C5, L7). Additive; no identifier
//     renamed.
// Bar-feed baselines advanced 2026-09-25 for W117 PR-A execution-evidence
// (fix/w117-a-exec-evidence, PR #218; re-pinned after the CTO's frontend gate
// found the red at HEAD cb025d64 — the wave changed both files and never
// re-pinned, class 110 again). Deltas against the PR base 0fb0f980, +63 −38:
//   provider/ninjatrader/tcp_framing.go  sha256 09344e8b… — F1 c66e2d5e
//     (entry receipt fences: EntryReceipt/ReadOnlyReceipt bookkeeping and the
//     readLoop note) + F2 e3af23ec (ordered execution dispatch frame fields).
//   provider/ninjatrader/tcp_server.go   sha256 92bcd868… — F1's GetPositions
//     fence and F2's ordered-execution dispatch: OrderedHandled guards around
//     the order fan-out, the handleFill extraction, installNTOrderedExecutions.
//   No identifier renamed, no guard removed; every removed line re-issued.
// Bar-feed baselines advanced AGAIN the same day (2026-09-25, CTO review
// of PR #218): F2 was ruled OUT of the PR and reverted (e3af23ec + b49f659c),
// and the F-1 P0 fix landed on the PRODUCTION FramePositions path.
//   provider/ninjatrader/tcp_framing.go  sha256 5c5a015c… — the F2 revert
//     restored the pre-F2 bytes exactly (the earlier 09344e8b… pin is gone).
//   provider/ninjatrader/tcp_server.go   sha256 479de30b… — the F2 revert
//     minus the F-1 receipt-clock stamp in the FramePositions case.
//   No identifier renamed, no guard removed.
// Bar-feed baselines advanced AGAIN on the dev merge (origin/dev 9c106d0b,
// #216/#219 touched tcp_server.go): the MERGED bytes pin to sha256 fdb54226…
//   (re-computed from the merged tree — both pre-merge pins are superseded).
// Bar-feed baseline advanced 2026-09-26 for FIX-P1A (fix/farside-proof-reconnect,
// the far-side AddOn build proof must be cleared on disconnect and re-proven by
// the new connection's hello):
//   provider/ninjatrader/tcp_server.go — closeConn gains one line
//     `s.farSideBuild.Store("")` plus its comment (the FIX-P1A retirement of the
//     proof). Additive; no identifier renamed, no guard removed, the hello/
//     heartbeat/order_snapshot prove sites are byte-untouched.
// Baseline advanced AGAIN 2026-09-26 for the FIX-P1A CI-red fix-up (same branch,
// the CTO's traceability ruling): provider/ninjatrader/tcp_framing.go gains
//   `ErrFarSideNotProven` (a new sentinel beside ErrAddonBuildTooOld so a
//   link-down / not-yet-re-proven refusal names its true reason). Additive; no
//   existing identifier renamed, no wire shape changed.
// Bar-feed baseline advanced 2026-09-25 for DS-105 DEFAULTS-SANE (#212 fold):
//   provider/ninjatrader/tcp_server.go — the live-sink age bound's name is
//     exported for the Picture floor pins (liveFrameMaxAgeMs → LiveFrameMaxAgeMs
//     in bar_live_sink.go; tcp_server.go only re-qualifies its WARN field, one
//     identifier renamed, no guard touched).
// Bar-feed baseline advanced 2026-09-28 for UPDATER-FLAT-LIVE-OK (DS-103,
// fix/updater-flat-live-ok): provider/ninjatrader/tcp_framing.go is
// COMMENT-ONLY — the maintenance_ack census doc records the owner ruling
// 2026-09-28 (a connected non-SIM connection is allowed when every account is
// flat; TRADING stays SIM-only, see isAccountTradeable). No identifier
// renamed, no guard removed, no wire shape changed.
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
// go.mod baseline advanced 2026-09-25 for WAVE 117 PR-I (fix/w117-i-deps,
// CTO-dispatched dependency-security fold, CTO ruling on F33):
//   golang.org/x/crypto v0.53.0 -> v0.55.0 — the highest 1.25-compatible
//     (v0.56.0 declares go 1.26.0; the toolchain wave owns the remainder).
//     Clears GO-2026-6303; GO-2026-6354/6355 remain, unreachable per
//     govulncheck; GO-2026-5932 has no fix.
//   github.com/consensys/gnark-crypto v0.19.0 -> v0.19.2 (patched).
//   tidy's x/net v0.56.0 -> v0.57.0, x/text v0.39.0 -> v0.41.0,
//     x/sync v0.21.0 -> v0.22.0, x/sys v0.46.0 -> v0.47.0.
// The go directive is BYTE-UNTOUCHED: `go 1.25.13` stays (CTO §1(a): a
// go 1.26.0 directive is a separate owner-visible toolchain wave).
// tcp_server.go baseline advanced 2026-09-21 for W-PICTURE-HTF (owner GO,
// merged 23050993): the baseline hash was last pinned at 42c35e2d (the picture
// branch's own 5/6 commit); the merged-HEAD delta vs that pin is EXACTLY the
// 111 inserted lines of the CTO-reviewed coordinated order-update fan-out —
// TCPServer fields (ouFanMu/ouFanNext/ouFanouts) + ListenOrderUpdates +
// runOrderUpdateFanout + the two test feed hooks. ADDITIVE only: zero
// removals, no identifier renamed, no guard removed; the bar-feed guards this
// pin protects (SubscribeBarsHistoryFor, bars_history_request write,
// bars_history_data/_error fan-out) are byte-untouched by that delta.
// tcp_server.go baseline advanced 2026-09-24 for WAVE 1a-plan N2 (comment
// hunks only, owner GO): the two stale "NT8 does not re-emit the just-closed
// bar" comments corrected to the real re-emit behaviour
// (VLBarsSubscriptionManager.cs:539-551, cache finalises the re-emitted bar).
// Zero code change, no identifier renamed, no guard removed.
// tcp_server.go baseline advanced again 2026-09-24 by the origin/dev merge of
// PR #199 (one-button M5): helloProcessPair renders nt8_pid/assembly_mvid as
// "n/a" when absent (L7 — an unread value must not read as a datum, runbook
// 2026-09-23-addon-m21-f5.md C5). ADDITIVE helper + one log line; the bar-feed
// guards this pin protects are byte-untouched by that delta.
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
// Ops baselines advanced 2026-09-26 for DS-104 FIX-OPS (fix/ops-observability,
// CTO-dispatched observability/ops wave) — the wave's P2 items change two pinned
// files BY DISPATCH, not by drift:
//   deploy/vl-db-backup.sh  sha256 6d49d291… — P2-5: dual-DB online backup
//     (main + 212 GB research.db) via a backup_one(src,prefix) helper, prune
//     and promote_weekly stages, VL_DB_RESEARCH / VL_KEEP_RESEARCH_* knobs.
//     The pre-existing main-DB backup path, quick_check and gzip steps are
//     preserved byte-for-byte inside backup_one; the script's CLI contract
//     (daily|weekly|prune) is unchanged.
//   deploy/vl-db-backup.sh  sha256 0e9dae38… — REVISED same wave after the
//     CTO's P0 gate on the first version (a 213 GB research snapshot every
//     timer run would fill the disk): research is now OPT-IN
//     (VL_BACKUP_RESEARCH=1, default OFF — default run = main DB only),
//     a disk-space precheck (2.5 × source size AND a VL_BACKUP_MIN_FREE_GB
//     50 GB post-backup floor) refuses loudly with nothing written, and the
//     opted-in retention defaults to 1/1 instead of mirroring 14/8.
//     Contract tests: deploy/vl_db_backup_test.go (fake small DBs + a PATH
//     df shim) — default-touches-only-main, opt-in-backs-up-research,
//     refuse-on-low-space, refuse-on-floor; mutation "default ON" fails the
//     first one.
//   logger/logger.go          sha256 318022bbb6… — P2-2: logRetentionDays()
//     (LOG_RETENTION_DAYS, 0 = OFF) and pruneOldLogs(dir, now, days, current)
//     called once at init; never today's/live file. Default OFF: with the knob
//     unset the shipped behavior is identical to before the change.
// The protections these pins exist for are byte-untouched: the NT8 wire
// (tcp_server.go / tcp_framing.go), the auth guard, and the bar-feed guards.
// Wire baseline advanced 2026-09-25 for W117 slice B (fix/w117-b-cancel-truth,
// port of #117 2f4db4f3): tcp_server.go's history-delivery send now holds
// histSubMu through the nonblocking channel send (teardown closes the channel
// under the write lock, so the read lock must span lookup + delivery). The
// bars_history_data / _error fan-out guards themselves are byte-untouched;
// only the lock release point moved. No identifier renamed.
// Wire baselines advanced 2026-09-25 for W117 slice A (fix/w117-a2-ordered-exec,
// the F2 rebuild — per-(symbol,account) FIFO workers instead of the failed
// read-goroutine consumers), each delta measured against the PR base:
//   provider/ninjatrader/tcp_framing.go — the three wire structs gain
//     OrderedOwned / BookGate json:"-" fields (routing flags, never
//     serialized). No identifier renamed, no frame type changed.
//   provider/ninjatrader/tcp_server.go  — orderedMu/orderedOwners/snapSeq
//     fields; the read loop enqueues order/fill/close to the owner's worker
//     before the advisory channel send, and advances the snapshot watermark.
//     The bar-feed guards this pin protects are byte-untouched by the delta.
//     Re-pinned at the dev-merge heads against the MERGED bytes (093a40e7 →
//     9ef5a75b…, 9c106d0b → 664cc10b…).
// Wire baselines advanced 2026-09-26 for FIX-DOUBLE-ENTRY (fix/double-entry-replay,
// the reconnect re-send double-entry guard), each delta measured against the
// wave base 04ae1c2f:
//   provider/ninjatrader/tcp_framing.go — +1 −1: FillPayload.Status's comment
//     gains "duplicate_ignored" (AddOn seen-signal dedupe). No field, frame or
//     identifier changed.
//   provider/ninjatrader/tcp_server.go  — +56 −2: the attempted-replay guard in
//     the flush path (settle/resend-once/hold/drop before the write), the
//     writeFrameHook test seam, the accept-time reconnect stamp, the fill-echo
//     note, the boot line, and the B2/B3 log hardening (settle/drop WARN with
//     trader_id + symbol + signal_id + op). The two removed lines are the old
//     write call re-issued with the seam — the stale-age check, the hold checks
//     and the Sunday-Shield re-queue are intact. (Re-pinned once after the
//     first full -race run caught a botched accept-loop edit — beginConnectionRecord
//     and the client-connected log were restored byte-for-byte; that fix is
//     inside the +56.)
//   ninjascript/VLTraderTCPClient.cs   — +45 −0: the seen-signal set
//     (SEEN_SIGNAL_TTL_MINUTES 10 / SEEN_SIGNAL_CAP 1024, never cleared on
//     fill) answering a replayed entry with fill status "duplicate_ignored"
//     before CreateOrder; P3 (DS-101 review) hardened the cap to
//     oldest-first eviction past the cap + a WARN. ADDITIVE only: no
//     identifier renamed, no guard removed, no VL_BUILD_ID bump (the owner
//     compiles + F5s).
// Wire baselines advanced 2026-09-28 for UPDATER-NT8-CLOSED
// (feat/updater-nt8-closed): provider/ninjatrader/tcp_server.go — closeConn
//   stamps DisconnectedMonoMs on the maintenance record (the nt8_absent
//   link-down measurement, read through ConnectionRecord.DisconnectedAt).
//   ADDITIVE only: no identifier renamed, no guard removed; the far-side
//   proof retirement and the connection lifecycle are byte-untouched.
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { expect, it } from 'vitest'
// CRYPTO REMOVAL (owner order 2026-09-30; CR-A 7497a02d3): go.mod re-pinned 2026-10-01 — crypto SDK requires dropped
// ONE-BUTTON P-E (owner order 2026-10-02 "do all now"; DS-101 ac8983b32): auth/auth.go re-pinned 2026-10-02 — cutover-worker scope, per-scope TTL, two-route allowlist
// TOKEN-IAT-SAME-SECOND (owner order 2026-10-02 "one merge per update"; DS-101 #302 f16954d0f/c54792d98, bundled in #305): auth/auth.go re-pinned 2026-10-02 — every mint carries a distinct jti + auth.Now clock seam
// RENAME-R5 (owner order 2026-10-02 'finish rename'; DS-104, integrated head): the three pre-rename deploy scripts deleted — keys removed, renamed vl-*.sh re-pinned from the merged tree 2026-10-02
// SPLIT REDESIGN (owner order 2026-10-04 "with everything"; REVIEW-353 + REVIEW-SPLIT-2, DS-103 C# + CTO Go, release/2026-10-05-1): ninjascript/VLTraderTCPClient.cs + provider/ninjatrader/tcp_framing.go re-pinned 2026-10-04 — two OCO pairs per entry (leg1_qty/leg1_tp), leg on move_stop/modify_bracket/position_close; ADDITIVE wire fields, VL_BUILD_ID 2026-10-04-d1 (owner copy + F5 + NT8 restart)
// SPLIT REVIEW FOLDS (DS-104 review → DS-103 4dddb0993 + 07f562b2b): ninjascript/VLTraderTCPClient.cs re-pinned 2026-10-04 — leg-1 exit keeps Leg1Qty (Leg1Exited/LastStop), later fills protect under leg 2, InitialStop fallback, never a 0-priced stop; same build id d1 (not yet installed anywhere)
// B11 LINK-IDLE (owner order 2026-10-05 "build all"; DS-102 #428, release/2026-10-06-1): provider/ninjatrader/tcp_server.go re-pinned 2026-10-06 — read-idle watcher (NT8_LINK_IDLE_SECONDS, default 60) drops a silent AddOn link outside the CME break and the weekend; wire schema unchanged
// B11 LINK-IDLE FOLD (CTO verification review 2026-10-06; DS-102 #431): provider/ninjatrader/tcp_server.go re-pinned 2026-10-06 — the read-idle watcher is wg-tracked so Stop() joins it (one line at Start)
// REL19 (owner merge #485 2026-10-13, DS-101 #484): ninjascript/VLTraderTCPClient.cs re-pinned 2026-10-13 — SendAllOrderSnapshots (every SIM + bound account; connect/periodic/account_register), VL_BUILD_ID 2026-10-04-d1 -> 2026-10-13-a (owner copy + F5 + NT8 restart)
// CT-EVERYWHERE (owner order 2026-10-13 "do all, send all agent out"; DS-103 fix/ct-everywhere): logger/logger.go + deploy/systemd-user/vl-backup.timer re-pinned 2026-10-13 — the compact formatter renders its line timestamp and the log-file date in America/Chicago (CTLocation, LoadLocation with the UTC fail-closed fallback kernel.CTLocation uses; logger cannot import kernel — cycle), never the host zone; the backup timer's two OnCalendar lines gain an America/Chicago zone suffix. No identifier renamed; the log-line shape ("MM-DD HH:MM:SS [LEVEL] caller msg") and the vl_<YYYY-MM-DD>.log name are unchanged, and the timer's schedule (05:00/17:30) is unchanged.
// ADDON-CLOSE-ROUTING (owner orders 2026-10-14 "recheck verify everything then fix all"; release #24 lane fix/addon-close-routing): ninjascript/VLTraderTCPClient.cs re-pinned 2026-10-14 — close_position honours its `account` field (one by-name resolver shared with the entry path; a named account that cannot be resolved is REFUSED, never another account), the position-owner map is keyed by (root, account), every close refusal sends position_close_rejected, and the 4.3 limit exit is sized to the account's whole position; VL_BUILD_ID 2026-10-13-a -> 2026-10-14-a (owner copy + F5 + NT8 restart). No wire shape change.
// REL31 LEG-3 BRACKET (owner order 2026-10-16 "release #31"; DS-107 fix/leg3-bracket): ninjascript/VLTraderTCPClient.cs + provider/ninjatrader/tcp_framing.go re-pinned 2026-10-16 — an optional THIRD OCO pair (leg2_qty/leg2_tp) so the runner sells half at the first line beyond the leg-1 exit; ADDITIVE wire fields, VL_BUILD_ID 2026-10-16-a (owner copy + F5 + NT8 restart), old AddOn builds get the 2-leg plan.
// REL31 FOLD #563 (gate fold, DS-108): ninjascript/VLTraderTCPClient.cs re-pinned 2026-10-16 — both strip chains test -sl3/-tp3 before -sl/-tp (D2), the PlacedBracket record keeps the frame's Leg2Qty/Leg2Tp on every branch so AmendBracketQuantity can build leg 3 on a later fill (D3), and leg 2 is never created/amended to a zero qty after its exit (D10). Same build id 2026-10-16-a.
// DATA-TRUTH m446 (owner principle 2026-10-29 "nothing related to data loss or incorrect data can continue to trade"; release #54 lane fix/m446-data-truth-checks): provider/ninjatrader/tcp_server.go re-pinned 2026-10-29 (f5d05417… -> 76c0ec3e…) — 8 ADDITIVE lines: the ingest drop sites (evictForIngest x5, enqueueBarHistorical timeout) call noteIngestLoss(msg) so a dropped FINAL or HISTORICAL bar is recorded by identity (symbol, frame, open time) in the data-truth ledger, and drainBarIngest notes the live frame's contract. No counter, no eviction decision, no frame, no identifier changed; the m416 final-sparing eviction order is byte-for-byte as it was.
// DATA-TRUTH m446 round 1 (review BACK on e4c04d616, D3): provider/ninjatrader/tcp_server.go re-pinned 2026-10-29 (76c0ec3e… -> d74795e6…) — ADDITIVE: enqueueBarHistorical now delegates to enqueueBarHistoricalContract, which carries BarsHistoricalPayload.Contract (parsed since 2026-09-11, dropped here until now) through barIngestMsg, and the drain records it in histContracts after the seed so the contract-roll guard can compare the history the ring was built from with the live contract. No behaviour change on the cache write path.
// M456 MAINACK (release #56, fix/m456-mainack be7e27c4b): provider/ninjatrader/tcp_server.go re-pinned 2026-10-30 (d74795e6… -> f8a9c10d…) — the connection record is now begun INSIDE the connMu critical section that publishes s.conn, at both publication sites (no-key accept, authenticateKeyHello), so IsConnected() and ConnectionRecord() appear together (lock order connMu -> maint.mu, already the established order); the accept instant is read before connMu; acceptClock becomes an atomic.Pointer (test seam only). No frame, no wire schema, no guard, no identifier changed.
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
  // The base is a pre-rename commit (A4-S3 P3-7). A mirror clone (the VL
  // partner repo) does not carry vl history, so the pin cannot be evaluated
  // there: skip with the reason stated instead of failing on `git diff` (bad
  // object). In vl itself the commit exists and the check runs unchanged.
  // TypeScript twin of the Go skip in branding/scope_test.go
  // (TestExistingGoImportTargetsPreserved).
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
  // W-EXEC-TRUTH W0 twin (the Go preserveImports move rule): a target that
  // left THIS file but is still imported by another tracked file was MOVED —
  // a legitimate refactor (e.g. the D2-WEB fold deleted the VlOS Studio
  // surface and IndicatorEditor.tsx dropped its `../ui/select` import while
  // five other files keep importing it) — and is preserved; a target that
  // vanished from the module is rejected.
  const headTargets = new Set<string>()
  const headFiles = new Set(
    git(['ls-files', '--', '*.ts', '*.tsx']).trim().split('\n').filter(Boolean)
  )
  for (const f of headFiles) {
    for (const t of targets(readFileSync(resolve('..', f), 'utf8')))
      headTargets.add(t)
  }
  // EXPLICIT removal pins for the crypto wave (file+specifier, with the
  // commit that removed each):
  const removedByCryptoWave = new Set<string>([
    'web/src/components/trader/ExchangeConfigModal.tsx\u0000../modals/TwoStageKeyModal', // CR-C b7 (secure-key modal usage cut)
    'web/src/components/trader/ExchangeConfigModal.tsx\u0000./Tooltip', // CR-C b7 (only the cut sections used it)
    'web/src/components/trader/ModelConfigModal.tsx\u0000qrcode.react', // CR-C b6 (claw402 QR flow)
    // CR-C b1 f30c26ab8 deleted the wallet-onboarding files (OnboardingModeSelector,
    // lib/onboarding, BeginnerOnboardingPage) and MarketTicker; these are the
    // importers it orphaned, pinned one by one:
    'web/src/components/auth/LoginPage.tsx\u0000./OnboardingModeSelector',
    'web/src/components/auth/LoginPage.tsx\u0000../../lib/onboarding',
    'web/src/components/common/HeaderBar.tsx\u0000../../lib/onboarding',
    'web/src/components/modals/SetupPage.tsx\u0000../auth/OnboardingModeSelector',
    'web/src/components/modals/SetupPage.tsx\u0000../../lib/onboarding',
    'web/src/contexts/AuthContext.tsx\u0000../lib/onboarding',
    'web/src/pages/AgentChatPage.tsx\u0000../components/agent/MarketTicker',
    'web/src/router/AppRoutes.tsx\u0000../pages/BeginnerOnboardingPage',
    'web/src/router/AppRoutes.tsx\u0000../lib/onboarding',
    // release #22 UI-GUIDE-TRUTH LF-1: /faq now redirects to /guide — the
    // inherited crypto/Docker FAQ page (with its pkill / delete-the-WAL
    // advice) is no longer routed:
    'web/src/router/AppRoutes.tsx\u0000../pages/FAQPage',
  ])
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
    let current: string[]
    try {
      current = targets(readFileSync(resolve('..', path), 'utf8'))
    } catch {
      // the file no longer exists at head (deleted by a merged wave) — its old
      // import targets have nothing left to preserve
      continue
    }
    for (const target of targets(old)) {
      if (current.includes(target) || headTargets.has(target)) continue
      // owner-ordered crypto removal (2026-09-30 wave) — pinned pairs,
      // never a loosened rule:
      if (removedByCryptoWave.has(`${path}\u0000${target}`)) continue
      expect(current, `${path}: existing import target ${target}`).toContain(
        target
      )
    }
  }
}, 30_000) // explicit timeout: the git/ts walk took 5089 ms on CI vs vitest's 5 s default (flake seen on #321, #288)
