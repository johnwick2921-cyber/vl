# Quality checklist

One page. Thirteen rows. Each row comes from bugs that really shipped here and
came back after a written rule already said "don't". Rules that had a test
behind them held; rules that were only prose did not. So every row asks for
**proof**, not a promise.

The full history (every bug class, with the incident that created it) is
`docs/superpowers/AUDIT-CHECKLIST.md`. Read that when a row's reason is not
clear. This page is the part you can read before every change.

## How to use this

**Before you code.** Read the rows. Mark the ones your change touches. For each
one, decide now what the proof will be (which test, which frame, which log line)
so you write that test first, not last.

**Before merge.** The PR body has one line per row (from
`.github/PULL_REQUEST_TEMPLATE.md`). Each line says `proof: <what proves it>` or
`n/a: <why it does not apply>`. A tick alone is not proof. Every test you name
must be in the PR's diff. The "Quality checklist" PR check turns red otherwise.
A reviewer who did not write the change then tries to break it (Q13).

**After Update.** Run `deploy/post-install-check.sh` and read the live log, not
the diff (Q11). `TOO EARLY` (exit 3) is not a pass: re-run at the time it names
(12 min after boot, or 45 min after the CME reopen for a closed-market install).
If a bug escaped anyway, the fix PR adds or tightens a row here and adds a test
for it (Q12).

## The rows

"Enforced by" says who checks the row. `ci: …` is a CI job. `test: Name` is a Go
test that fails when the rule is broken. `self` means the PR author writes the
proof and a reviewer checks it. `deploy/quality_checklist_test.go` fails if a
named test disappears, if the page grows past 120 lines, or if the number of
`self` rows goes up. Rows can only move from `self` to a machine check.

| # | Rule | Bug type it stops (entries found) | Proof | Enforced by |
|---|---|---|---|---|
| Q1 | Course rule: cite the video frame, and the on-screen example is the first test. A number with no citation ships OFF. | Course drift (~35; release #61 guard re-guessed each review) | Frame cite + test | self |
| Q2 | List every ending of any new state: fill, cancel, expiry, reject, restart, day roll. Test each. | State lifecycle (~22; classes 87, 96, 199, 204) | Tests | self |
| Q3 | Delete the guard and its switch-on line; the test must go red. Tests call production code, not a helper. | Tests proving nothing (~20; classes 53, 86, 108, 114) | Paste the red | self |
| Q4 | Run time logic across DST, daily break, weekend, half day and contract roll. | Calendar (~20; classes 138, 143, 149; release #60 DST) | Tests | self |
| Q5 | Full suite on the merged code, in Chicago and UTC. | Green here, red elsewhere (~18; class 110; 09-03 bare time layout) | Release gate | ci: release gate |
| Q6 | Kill the dependency in a test and check the alert still arrives. Every P0 is pushed or listed as not pushed, with a reason. | Owner told wrong or nothing (~18; release #62 alert dropped on its own outage) | P0 census test | test: TestOwnerPushP0Census |
| Q7 | Write down what the decision reads and what the check reads. Replay clean days and expect no pause. | Wrong data judged (~15; classes 97, 129, 205) | Replay output | self |
| Q8 | Make each read fail: unknown must never count as clear. Show a non-test caller of the gate. | Silent safety (~14; classes 188, 209, 227) | Test + grep | self |
| Q9 | List every path to the wire or the rule. A second path can't skip the gates. | Side doors (~13; classes 165, 166, 197, 235) | Site list | self |
| Q10 | Run the last-LIVE updater on the new artifact; for an AddOn change, restart NT8 and grep "frame failed". | Real-box only (~9; #48/#48.1 reverted on "c0") | Log line | self |
| Q11 | After Update, read live behaviour, not diffs. | 95 refusals missed for 6 days (10-23 to 10-29) | `deploy/post-install-check.sh` | self |
| Q12 | Any bug that escapes adds or tightens a row and a test in the same fix PR. | Repeats (12+ classes came back after a rule existed) | PR | self |
| Q13 | Before merge, a reviewer who didn't write the fix tries to break it, using these rows. | Caught the most bugs in our history | Review findings | self |

## What this page cannot do

It cannot promise zero bugs. It makes the known bug types harder to ship, and
it makes every new escape leave a test behind. Only Q5 (the release gate) and
Q6 (the P0 census test) are checked by a machine today. When another row gets
a machine check, its row changes from `self` to `test: <name>` and the `self`
cap in `deploy/quality_checklist_test.go` drops by one.
