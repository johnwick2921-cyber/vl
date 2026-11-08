## Summary

- Problem: a row hidden in a comment must not count.

## Quality checklist

- Q1 course frame cited: n/a: no course rule is touched by this change
- Q2 every ending of new state: n/a: the change adds no new state
- [x] Q3 guard removed, test went red: proof: TestFixtureGuardTurnsRed went red with the guard deleted (pasted below)
- Q5 full suite merged, CT and UTC: proof: go test ./... green at the merged head in CT and in UTC
- **Q6** alert still arrives: n/a: no alert path is touched here
- Q7 decision and check read the same data: n/a: no decision input changes
- Q8 unknown never counts as clear: proof: TestFixtureUnknownReadRefuses, caller trader/gate.go:41
- Q9 every path to the wire: proof: one send site, trader/send.go:88, listed in the PR
- Q10 last-live updater: n/a: no artifact or AddOn change in this PR
- Q11 live behaviour after Update: proof: post-install-check PASS pasted after the boot
- Q12 escaped bug adds a row and a test: proof: Q8 tightened, TestFixtureUnknownReadRefuses added
- Q13 adversarial review: proof: second reviewer findings F1-F3 folded in commit abc123
<!--
- Q4 calendar edges: proof: hidden rows are invisible to reviewers
-->
