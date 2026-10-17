# Three pre-existing lock defects — Section G report

**Wave:** the three pre-existing lock defects (owner-pinned Section C, 2026-09-10)
**Branch:** `fix/lock-defects-release-meta-halfbuilt`
**Claim:** `lockdefects-554049f5/nofx-8e[88742a]`
**Scope:** `deploy/nofx-lock.sh`, `deploy/nofx-lock-test.sh`. No Go, no binary, no
boot. SIM untouched.

---

## The one root under all three

The owner pinned three defects. They are three symptoms of a single fact:

> **`mkdir` is the atomic step and `meta` arrives ~7ms later. In between, the
> directory exists and describes nobody — and every reader treated that as a
> COMPLETE lock whose fields happened to be empty.**

`_age()` fell back to `${hb:-0}`, so a meta-less lock reported an age of
**1,789,058,294 seconds** and every consumer downstream drew the obvious
conclusion. "Being created right now" and "held by someone who stopped beating"
reached the reader as the same answer — **and only the second is ever grounds for
a takeover.**

## C1 — `release` discarded `rm`'s exit status [A]

```bash
rm -rf "$LOCK_DIR"; echo "released by $session"     # the `;` throws rm's status away
```

The function returned `echo`'s 0. Proved with a read-only parent directory:

```
release said : rm: cannot remove '…/lock.d': Permission denied
               released by c1
release rc   : 0
lock dir     : STILL PRESENT
next acquire : REFUSED — lock already held:
```

**It is worse than a wrong exit code.** `_stop_keeper` runs *before* the `rm`, so
a failed removal leaves a lock whose heartbeat has **already been stopped**. It
goes STALE within `HEARTBEAT_STALE_SECONDS` on a holder that is alive and
believes it finished — and the next lane to look sees an abandoned lock that is
nothing of the kind. The defect manufactures exactly the false-STALE the whole
heartbeat model exists to prevent.

**Fix.** `rm`'s status is checked AND the directory is confirmed gone, because a
zero exit is a claim and the absent directory is the fact. On failure: rc 1, and
a message that says the lock is still held, that the keeper is already stopped,
and what to look at.

## C2 — a lock directory with no `meta` was TERMINAL [A]

`_require_holder` compares against an empty session, so **every verb refused**:

```
release by anyone : REFUSED — 'somebody' is not the holder ('', task: ).
reclaim attempt   : REFUSED — name the session you are taking over. The holder is '', you named '<nothing>'.
acquire attempt   : REFUSED — lock already held:
```

Clearable only by an `rm -rf` outside the tool — the one thing this tool exists
to stop people doing by hand.

**Fix.** A `clear-incomplete` verb, guarded on both sides. It refuses a lock that
HAS meta (that one has a holder — use `release`, or `reclaim` on the record), and
refuses one younger than the abandon window (that one is an acquire in flight, and
taking it would be class 70's replacement bug wearing a new hat). Both refusals
matter: this is the only verb that takes no session, so it is the only one an
impatient reader could aim at a live lock.

## C3 — a half-built lock read STALE with an empty holder [A]

```
STALE — held by '' (task: ), heartbeat 1789058294s old (> 300s), expiry  · auto-beat: off.
check says: stale  rc=2
```

**Fix.** Two new states, reported before the age branch, which cannot describe
them: `INCOMPLETE` (rc 3) while an acquire is in flight, `ABANDONED-INCOMPLETE`
(rc 4) once the directory has stood past the abandon window with no meta.

**`check`'s new codes are ADDITIVE.** 0/1/2 keep the meanings the tree-guard spec
was written against, so a caller that has not been taught 3 and 4 still sees a
non-zero "not free" — the safe reading. One that has can tell "an acquire is in
flight" from "held by someone who stopped beating".

## The threshold is measured, not chosen

`INCOMPLETE_ABANDON_SECONDS` defaults to 30s. The window it bounds was measured
by instrumenting `cmd_acquire` between `mkdir` and the end of `_write_meta`:

```
n=10  min=6.92ms  mean=7.35ms  max=7.71ms
```

30s is ~4000× the observed window — wide enough that a loaded machine cannot
cross it, narrow enough that a lock orphaned mid-creation clears within the
minute. Override with `NOFX_LOCK_INCOMPLETE_SECONDS`.

## PREMISE CORRECTION — the tree guard does not exist

The dispatch described `check` as "the tree guard's unattended interface", which
set C3's severity. **There is no tree guard.** `deploy/` contains
`nofx-clock-guard.sh` (a different tool) and no `nofx-tree-guard.sh`; what exists
is a SPEC, `docs/superpowers/plans/2026-09-02-tree-guard-spec.md`, last touched
`f9b00935` 2026-09-03.

And that spec does not act on rc 2 — it WARNs:

> `held-stale (rc 2): checks 1 and 3 WARN, naming the session and the heartbeat
> age. STALE IS NOT DEAD — the guard reports, a human corroborates.`

So C3 was real, deterministic within its ~7ms window, and had **no unattended
consumer**. Its consequence today is a confusing status line; it becomes live the
moment the guard is built to this spec — at which point the guard would WARN
naming an empty session. Recorded because the fix should not look more urgent
than the evidence supports, and because whoever builds that guard should know
rc 3 and 4 now exist.

## Suites

- Lock suite **101 pass / 0 fail** (was 85; 16 new pins).
- Mutation-tested against the pre-fix script — see the commit for which pins died.

## Not done / next

- `_write_meta` still `mv`s into `$LOCK_DIR/meta` by absolute path with no
  identity check (class 102's mechanism). The group-kill closed the window that
  made it reachable; the unguarded write is still there and is worth its own
  wave.
- The tree guard itself remains unbuilt. If it is built, it should consume rc 3
  and 4 rather than folding them into "not free".
