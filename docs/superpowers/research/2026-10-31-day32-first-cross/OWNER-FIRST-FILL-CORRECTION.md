# Owner clarification — 2026-10-31

Owner confirmed: one FILLED trade per overnight session. Before any fill, a new
EMA9/34 crossover changes the intended direction and starts a new first-retest
opportunity. Do not consume the session on an unfilled cross. After entry, keep
TP/SL and take no other trades regardless of outcome. TP15; SL15 and SL16
separate. This explicitly supersedes the first-cross-only interpretation in
SPEC.md. Other modelling assumptions stay disclosed and unchanged.

New engine: first_fill.py; adapter: run_first_fill.py; independent reference:
verify_first_fill.py. Old code/results preserved, old report withdrawn.
Data unchanged (122 downloads, 109 accepted sessions, 21 not included).
Each crossover before fill is recorded with exact source row and timestamp.
No additional data purchased, no live changes. This does not force an entry
on days without a valid crossover/retest.
