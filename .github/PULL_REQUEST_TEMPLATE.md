## Summary

- Problem:
- What changed:
- What did NOT change (scope boundary):

## Quality checklist

<!--
One line per row of docs/QUALITY-CHECKLIST.md. After each "proof:" write what
proves the row: test names, a course frame cite, or the pasted red line (8+
characters). If a row does not apply, replace "proof:" with "n/a:" and say why
(10+ characters). A tick alone is not proof. Every Go test you name must appear
in this PR's diff. The "Quality checklist" check turns red otherwise.
-->

- Q1 course frame cited, worked example is the first test: proof:
- Q2 every ending of new state (fill, cancel, expiry, reject, restart, day roll): proof:
- Q3 guard removed, its test went red, tests call production code: proof:
- Q4 time logic across DST, daily break, weekend, half day, contract roll: proof:
- Q5 full suite on the merged code, Chicago and UTC: proof:
- Q6 dependency killed, alert still arrives, every P0 pushed or listed: proof:
- Q7 what the decision reads vs what the check reads, clean-day replay: proof:
- Q8 each read made to fail, unknown never counts as clear, non-test caller shown: proof:
- Q9 every path to the wire or the rule listed, none skips the gates: proof:
- Q10 last-LIVE updater on the new artifact, NT8 restart for AddOn changes: proof:
- Q11 live behaviour read after Update (deploy/post-install-check.sh): proof:
- Q12 escaped bug adds or tightens a row and a test: proof:
- Q13 adversarial review by someone who did not write it: proof:

## Rules / errors

- Rules: SIM-only, DB read-only, no deletes, guide content updated with any knob/default change, data or trading changes behind a switch that defaults OFF.
- Traceable errors: for each new failure path give file:line, log level, the ids in the message, the counter it bumps and where it is stored. No silent `_ =` on the trade path.
