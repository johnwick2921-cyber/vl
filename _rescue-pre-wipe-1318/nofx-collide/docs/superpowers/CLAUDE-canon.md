# CLAUDE-canon — the operating rules that must SURVIVE, in a file a wave can reach

**Why this file exists.** `~/nofx/CLAUDE.md` is **untracked**. Every rule in it is
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
deploy/nofx-lock.sh acquire <session> "<task>" [minutes]   # atomic; REFUSES if held; STARTS THE KEEPER
deploy/nofx-lock.sh heartbeat <session>                    # acquire starts the keeper; do NOT hand-beat — a second writer into the lock dir is the class-102 failure the keeper closed
deploy/nofx-lock.sh with-heartbeat <session> -- <cmd>      # wrap long steps (builds, suites)
deploy/nofx-lock.sh status                                 # human-readable: holder, task, heartbeat age, expiry, auto-beat on/ENDED/off
deploy/nofx-lock.sh check                                  # CHECK rc: 0 free · 1 held · 2 stale · 3 incomplete · 4 abandoned-incomplete
deploy/nofx-lock.sh reclaim <you> <stale> "<corroboration>"  # succession, ON THE RECORD; REFUSED while the heartbeat is fresh; returns RECLAIM rc 3 (inherited, not taken)
deploy/nofx-lock.sh release <session>                      # only the holder may release; ends the keeper group, WAITS, and FAILS (rc 1) if the directory survives
deploy/nofx-lock.sh clear-incomplete                       # removes a lock that names NOBODY; refuses one with meta, and one younger than 30s
```

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
- **DO NOT run your own beater.** A `while true; do nofx-lock.sh heartbeat …; sleep N; done`
  alongside the keeper makes you a second writer into one lock dir. That is class 88.

**The trap that nearly shipped inside the fix** (worth reading before you write any
process-group code anywhere): `setsid … &` starts in the **invoking shell's** process
group and moves only once `setsid` execs, while `$!` returns before that. So a naive
`keeper.pid` can capture *your own shell's* group, and `release` will then
`kill -TERM -- "-$pg"` **you**. It SIGTERMed a test run (exit 143) before it was caught,
and the exit code was first misread as an environment problem. Before any
`kill -- -$N`: confirm `N` is a genuine group leader (`pgrp == pid`) and positively
identify the target. A bare pid signals whatever unrelated group owns that number.

---

## THE STANDING RULE THIS FILE ENCODES

**An operating rule that lives only in an untracked file is a rule no wave can correct
and no test can check.** When a rule changes, change it *here* — and treat any
disagreement between this file and `CLAUDE.md` as evidence that `CLAUDE.md` is stale,
not that this file is wrong. The same shape has now bitten twice: `api/handler_svp.go:50`
calling `2000` "the cache cap" when the cap is `2500`, and `CLAUDE.md:205` instructing
every lane to hand-beat after hand-beating became unsafe. Both were prose describing
code, in a place the code's own tests could not see.

**Mirrored 2026-09-10 by lane `claude-canon-2bdef526/nofx-07[aa8e26]`, on owner order,
from `~/nofx/CLAUDE.md:205` — quoted before and after in the wave's message. Detail:
`docs/superpowers/reports/2026-09-10-lock-keeper-on-acquire.md`, checklist classes
101–104.**
