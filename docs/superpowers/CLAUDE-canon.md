> R5 rename note (2026-10-02): names in this file were rewritten from the pre-rename repo name to `vl`.

names rewritten to vl on 2026-09-30 (VL rename)
# CLAUDE-canon — the operating rules that must SURVIVE, in a file a wave can reach

**Why this file exists.** `~/vl/CLAUDE.md` is **untracked**. Every rule in it is
invisible to git: no wave can correct it on a branch, no review can see it drift, and
no test can assert it still matches the code. On 2026-09-10 that cost real safety —
the lock keeper wave (`417599a3`, checklist classes 101–104) made hand-beating a
heartbeat *unsafe*, and the one file instructing every lane to hand-beat was the one
file the wave could not touch. A lane following CLAUDE.md faithfully would have become
the second writer the wave existed to eliminate.

This file is the TRACKED mirror. Where the two disagree, **this one is newer by
construction** — it can be changed by a wave; the other cannot.

---

## MAIN-TREE LOCK LAW — the corrected verbs

```
deploy/vl-lock.sh acquire <session> "<task>" [minutes]   # atomic; REFUSES if held; STARTS THE KEEPER
deploy/vl-lock.sh heartbeat <session>                    # acquire starts the keeper; do NOT hand-beat — a second writer into the lock dir is the class-102 failure the keeper closed
deploy/vl-lock.sh with-heartbeat <session> -- <cmd>      # wrap long steps (builds, suites)
deploy/vl-lock.sh status                                 # human-readable: holder, task, heartbeat age, expiry, auto-beat on/ENDED/off
deploy/vl-lock.sh check                                  # CHECK rc: 0 free · 1 held · 2 stale · 3 incomplete · 4 abandoned-incomplete
deploy/vl-lock.sh reclaim <you> <stale> "<corroboration>"  # succession, ON THE RECORD; REFUSED while the heartbeat is fresh; returns RECLAIM rc 3 (inherited, not taken)
deploy/vl-lock.sh release <session>                      # only the holder may release; ends the keeper group, WAITS, and FAILS (rc 1) if the directory survives
deploy/vl-lock.sh clear-incomplete                       # removes a lock that names NOBODY; refuses one with meta, and one younger than 30s
```

Run the lock tool ONLY from the MAIN TREE: `<main tree>/deploy/vl-lock.sh`, never a copy in your worktree. Until the R1b merge the main tree has only its old-name twin (`<main tree>/deploy/<old>-lock.sh`); after it, either tool works. Until R5 both take the SAME lock home (A5-S6 P3-2).

**RC CODES ARE PER-VERB, AND TWO OF THEM COLLIDE ON 3.** `check` rc 3 means *an
acquire is in flight — never take this over*; `reclaim` rc 3 means *you have
INHERITED an abandoned lock rather than taken a free one*. Those are close to
opposite, they sit on adjacent lines, and a lane skimming the block can carry
away "rc 3" as a fact about the tool rather than about a verb. Hence the
qualifiers above. (Spotted by a peer reading the block, not by a test — no
assertion can see this, because both lines are individually correct.)

**`check` rc 3 and 4 (added `757eb578`) are ADDITIVE.** 0/1/2 keep the meanings the
tree-guard spec was written against, so a caller that has not been taught the new
codes still sees a non-zero "not free" — the safe reading. A caller that HAS been
taught them must not fold them back together: **rc 3 means an acquire is in flight
and is NEVER grounds for a takeover**, while rc 2 (held, stopped beating)
sometimes is. rc 4 means the directory has stood past the abandon window with no
meta — an acquire that died before writing its identity — and `clear-incomplete`
is the only verb that can address it.

A lock directory with no `meta` is an INCOMPLETE lock, not a held one: `mkdir` is
the atomic step and `meta` lands ~7ms later (measured n=10: 6.92–7.71ms), and
every reader used to treat that gap as a complete lock whose fields were empty.

**What changed on 2026-09-10 (`417599a3`), and what did not.**

- `acquire` now spawns the keeper itself and records its process GROUP in `keeper.pid`
  as a **stop handle only**. Nothing reads it as liveness. **Liveness is still the
  heartbeat and only the heartbeat** — class 70 stays green, and a pid still answers
  the wrong question ("does a process exist") rather than the right one ("is the owner
  still working").
- The keeper beats until your **declared expiry**, then stops and records why in
  `keeper.ended`. It **never auto-extends**. Need longer: re-acquire or extend
  explicitly.
- **The expiry is now ENFORCED at `cmd_heartbeat`** — the single place a heartbeat can
  be written, so it binds every writer including a hand-rolled one. Past your declared
  expiry a beat is REFUSED. A lock acquired *before* this landed carries no
  `expiry_epoch` and stays unbounded, deliberately: retroactively bounding a live lock
  could refuse a holder's next heartbeat mid-cutover.
- **DO NOT run your own beater.** A `while true; do vl-lock.sh heartbeat …; sleep N; done`
  alongside the keeper makes you a second writer into one lock dir. That is class 88.

**The trap that nearly shipped inside the fix** (worth reading before you write any
process-group code anywhere): `setsid … &` starts in the **invoking shell's** process
group and moves only once `setsid` execs, while `$!` returns before that. So a naive
`keeper.pid` can capture *your own shell's* group, and `release` will then
`kill -TERM -- "-$pg"` **you**. It SIGTERMed a test run (exit 143) before it was caught,
and the exit code was first misread as an environment problem. Before any
`kill -- -$N`: confirm `N` is a genuine group leader (`pgrp == pid`) and positively
identify the target. A bare pid signals whatever unrelated group owns that number.

**THE WORKER ACQUIRES ITS OWN LOCK (WORKER-TAKES-THE-LOCK, owner order
2026-10-02 12:1x CT; CTO fold 12:40 CT).** The updater worker takes the
main-tree lock itself, with the SAME atomic `acquire` humans use, as session
`updater-<job id first 12>`, with an expiry covering the job budget — right
before preflight. It **never reclaims, never takes a held or stale lock, and
never clear-incomplete**. ANY lock held by anyone else refuses preflight
naming the holder — an attended install is just a button install: the CTO
does NOT hold the lock before pressing, and **humans never hold the lock
across a button install**. The worker releases its own lock at complete /
rolled_back / refused; on recovery_needed it KEEPS the lock and names it in
the job, so a human looks before anything else touches the tree. A worker
restart mid-job finds its own lock by the deterministic session name and
continues — it never double-acquires. After the worker releases at complete,
the CTO acquires the lock for the RELEASE-marker commit as usual (canon
four halves unchanged). (The old "the worker never acquires — the attended
deploy does" rule is superseded; `MainTreeLockHeld`'s check-only semantics
are unchanged.)

---

## THE STANDING RULE THIS FILE ENCODES

**An operating rule that lives only in an untracked file is a rule no wave can correct
and no test can check.** When a rule changes, change it *here* — and treat any
disagreement between this file and `CLAUDE.md` as evidence that `CLAUDE.md` is stale,
not that this file is wrong. The same shape has now bitten twice: `api/handler_svp.go:50`
calling `2000` "the cache cap" when the cap is `2500`, and `CLAUDE.md:205` instructing
every lane to hand-beat after hand-beating became unsafe. Both were prose describing
code, in a place the code's own tests could not see.

**Mirrored 2026-09-10 by lane `claude-canon-2bdef526/vl-07[aa8e26]`, on owner order,
from `~/vl/CLAUDE.md:205` — quoted before and after in the wave's message. Detail:
`docs/superpowers/reports/2026-09-10-lock-keeper-on-acquire.md`, checklist classes
101–104.**

---

## WORKTREE LAW — `git worktree add` IS CHECKED, AND THE CHECK IS CHECKED

A lane committed onto another lane's branch this week by scripting
`git worktree add … && cd …` and never reading the exit code. When the add fails,
`cd` lands in whatever directory the shell was already in — very often the MAIN
TREE — and the next `git commit` goes somewhere nobody chose. The failure is
silent because every command after it succeeds.

```
W=/home/hoang/vl-<task>
git worktree add --detach "$W" origin/dev || { echo "worktree add FAILED"; exit 1; }
git -C "$W" rev-parse --is-inside-work-tree >/dev/null 2>&1 \
  || { echo "$W is not a worktree"; exit 1; }
cd "$W" || exit 1
[ "$(pwd -P)" = "$W" ] || { echo "cd landed at $(pwd -P), not $W"; exit 1; }
```

**Three checks, because each catches something the others cannot:** the exit code
catches a refused add; the `rev-parse` catches a directory that exists but is not
a worktree; `pwd -P` catches a `cd` that silently landed elsewhere (a symlink, a
`CDPATH`, a stale shell).

**AND THE CHECK ITSELF CAN BE WRONG — this is the part worth reading.** On
2026-09-10 a lane wrote `[ -d "$W/.git" ]` as its verification and it reported
failure on a perfectly good worktree. **In a worktree, `.git` is a FILE, not a
directory** — it contains `gitdir: /path/to/.git/worktrees/<name>`. The guard was
correct-looking, ran green in the author's head, and failed closed on a healthy
tree. Use `git rev-parse --is-inside-work-tree`, which asks git rather than
guessing at git's layout.

That is the same shape as everything else in this file: a statement ABOUT the
tool, written where nothing compares it to the tool. The remedy is the same —
ask the tool.

---

## Updates page truth rules (W-ONE-BUTTON M5, 2026-09-24)

The Settings → Updates page is the owner's face of the one-button update and is
bound harder than any other page by the no-fabrication laws:

- **Renders only API truth.** Every panel value comes from a READ endpoint
  (`/api/health`, `/api/maintenance`, `/api/installation-gate`, `/api/updates`,
  `/api/updates/jobs/:id`, `/api/strategies/:id/effective`) — the page derives
  nothing it could not read, computes nothing in the browser, and invents no
  optimistic state.
- **n/a, never a guess.** A value the binary cannot know prints n/a (the MVID /
  hello match, the completed 4H-bar count, the last-progress age and the
  paused-while-loading flag have no endpoint yet; each is OWED, not fabricated).
- **Absent ≠ [].** A job that has not run renders "no update job", never an
  empty timeline; a null addon ack renders "no ack yet", never "false"; a null
  withdraw renders nothing, never empty lists.
- **Exact blocker text.** A refusal is shown with the server's own error text; a
  403 install refusal renders "not authorized on this device" with that text and
  is never retried in a loop.
- **The install button is gated on the review.** While the M3 update
  authorization adversarial review is open, the install button is disabled with
  the text "install authorization under review" and no install POST can fire —
  a constant flag pinned by its own test. The header badge polls once a minute,
  shows Unknown on any error, and its first-fetch spinner is capped at 10 s so
  it can never spin forever.
---

## RELEASE AND MANUAL BOOT (W-ONE-BUTTON M4)

**The guide's rev is a BUILD INPUT, not a literal.** `web/src/guide/types.ts`
reads `VITE_GUIDE_BUILT_REV`, but the REFUSAL lives in `web/vite.config.ts`
(`guide-built-rev-is-a-build-input`, `apply: 'build'`) — a PRODUCTION build with
it missing or not 40-hex FAILS THERE; a dev build renders `dev`, and a
production bundle that somehow lacks it renders `unknown`, never a crash. The
refusal CANNOT live in types.ts: a `throw` at module scope is not executed by
the build, only bundled, so the first version exited 0 and shipped the throw
into the page (CLASS 240's shape, found 2026-09-24 by running the negative
case instead of describing it). The old boot step — grep
`GUIDE_BUILT_REV = '<sha>'` out of that file — cannot work any more, and its
silence is not a pass. Build with `VITE_GUIDE_BUILT_REV=<sha> npm run build`,
then VERIFY by finding that sha in `web/dist/assets/*.js`. That is the only
place the truth can be checked once the constant is gone.

Why it moved: a bump that lives as a step in a deploy procedure is a step
someone skips under pressure — which is exactly when a guide that disagrees
with the running binary does the most damage.

**A release is cut from an APPROVED TAG**, in a protected Environment with the
owner as required reviewer, never from a push to a branch. The tree must be
clean before the build and `go version -m` is read BACK to prove
`vcs.modified=false` and that the binary carries the tag's revision. Only the
allow-list is packaged; the staged tree AND the unpacked archive are scanned
separately, because they are different things and a check that sees only one is
bypassed by whatever happens in between. The manifest is signed, and verified
against the committed ALLOWED-SIGNERS file before upload — a bare `.pub` does
not work here: `ssh-keygen -Y verify -f` parses its file as
`<principal> <keytype> <base64>`, so a bare key is read as principal
`ssh-ed25519` and matches nothing.

**A rollback pair is advertised `tested:true` only when it was PERFORMED**: the
old binary creates a fresh DB, the new one migrates it forward, and then the
OLD binary boots the MIGRATED DB. That third step is the rollback, it is the
one nobody runs, and it is where a migration that dropped a column the old
binary still SELECTs fails — at the moment a rollback is needed. A pair that
could not be proven is carried `tested:false`, never omitted silently. The
proof is BOOT-TIME ONLY: it says the schema survives the round trip, not that
the rollback is safe in every respect.

**Restart identity is `(MainPID, /proc/<pid>/stat` field 22 starttime`)`,
never wall clock** — M1 measured two start times disagreeing by 151 s after a
WSL clock step. Never `pgrep -f vl-bin`: that also matches
`go version -m vl-bin`, so it can return the pid of a tool inspecting the
binary instead of the server running it. Ask systemd for MainPID.

**The boot proof lives in `data/vl_<date>.log`, not journald** (journald keeps
(a box not yet through the R2 boot still writes the old-prefix log)
about 4 h for uid 1000). The watchdog wants the `BOOT INTEGRITY OK — rev <sha>`
line there, `/api/health` reporting the new revision, and the served bundle
carrying it.

**Only release-owned entries switch**: `vl-bin`, `web/dist`,
`deploy/RELEASE`, `calendar_static_t1.json`. The WORKING DIRECTORY is never
symlinked, or `data/` and `.env` resolve inside a release and are lost at the
next switch.
