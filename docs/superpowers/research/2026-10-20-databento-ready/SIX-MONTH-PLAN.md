# First-six-month tick backtest and verification

Owner instruction: report the earliest six months first and recheck the entire
research engine before presenting results. Frozen dates: **2025-10-20 inclusive
to 2026-04-20 exclusive**, 130 candidate weekdays. This is the first half of the
requested year, not the most recent six months. Downloader continues the full
year separately. No live trading or production strategy change is part of this.

## Rules and assumptions

08:30 Houston, America/Chicago including DST; independent ORB lengths 1, 2 and
5 minutes. Immediate first trade at least 0.25 point beyond the completed range.
One trade per session per variant. Stop 20 points from entry. Target one complete
ORB width from entry. Entry and adverse stop gaps use the observed trade price;
targets receive no assumed price improvement. No re-entry or stop movement.

Primary target-touch results and a one-tick trade-through sensitivity are shown
separately. Neither is proof of execution, available bid/ask liquidity, latency
or queue position. Two points round-trip friction is a scenario, not measured
broker costs. Positions still open at the research cutoff (15:00 Houston,
retained from the earlier report's disclosed assumption) are OPEN_AT_CUTOFF,
not assigned a fictional fill. They have a separate mark and no realized P&L.

Selection uses only 08:00–08:30 pre-open trade volume received before 08:30,
with raw-symbol mappings from each DBN file. Volume ties are excluded explicitly.
No continuous back adjustment or selection using future daily volume/P&L.
Late opening-range records, regressions, invalid records, nonzero quality flags
and missing RTH minutes are flagged/excluded. The conservative 390-minute gate
also excludes early closes: show every reason, and never call the remaining
sample a complete census of all possible NY trading days. Minute presence does
not independently prove that no trades are missing within that minute.

The original file order is retained, including equal timestamps/sequence values.
Databento's [CME normalization documentation](https://databento.com/docs/venues-and-datasets/glbx-mdp3)
describes normalization of trade summaries and preservation of instrument
message order. Equal timestamps alone are not evidence of missing order. A
separate, intentionally conservative tie sensitivity labels an actionable
multi-price event/sequence group unresolved; it must not replace the primary
provider-order results or be represented as proven data loss.

## Verification work and actual evidence

Audit reference: `docs/superpowers/AUDIT-CHECKLIST.md`, timestamp class 7,
call-site tests, provenance, denominators and no invented values. Last checklist
commit: `ba50edf220dbc7fc639a8f9e449470134ebf8e33`.

- `tick_orb.py`: decoding, contract selection, coverage census and trade replay.
- `test_tick_orb.py`: 17 tests; real compressed DBN integration, first-hit order,
  mirrored long/short, gap fills, target sensitivity, boundaries, DST, no second
  trade, missing files, late-known opening range, hash preservation and counts.
- `verify_tick_engine.py`: fresh Chicago and UTC runs, 6,000 seeded independent
  synthetic first-passage comparisons per timezone; seven deliberate defects
  must produce assertion failures with zero unrelated test errors. Actual PASS
  evidence: `verification-tick-engine.json`. This tests the offline research
  code, not the production trading engine.
- `audit_tick_orb.py`: separate first-passage implementation checks every primary
  result and reconciles counts/denominators/P&L back to CSV. It shares decoding
  and coverage with the main harness; it is not an independent vendor audit.
- Four real days (2025-10-20 through 2025-10-23) exercised decoding and replay;
  24 primary rows passed the independent calculation. Local pilot2 results are
  at `/home/hoang/fleet-1017/out/orb-tick-engine-pilot2-20261020/`. The earlier
  pilot used conservative ties only and is preserved as superseded exploration,
  not a six-month result. The first packaging attempt found an unavailable
  optional Excel dependency; the final exporter uses only the standard library.
- The packaged pilot has 48 total variant rows, four census rows and an Excel
  ZIP whose CRC, XML and row counts are checked. Nanosecond integers remain text
  in Excel (no rounding beyond its 15-digit numeric precision).

## Automatic milestone, not automatic publication

`six_month_milestone.py` waits for every one of the 130 completed-day receipts,
reruns engine tests, runs replay into an exclusive new folder, independently
audits the result, and renders Markdown/CSV/Excel/ZIP. It sends a completion or
failure message through the **existing** agent bridge watcher; it does not
install another notification transport. A stopped downloader is reported, and
the milestone keeps waiting for repaired/resumed downloads.

The generated report says **pending final CTO review**. Inspect actual sample
coverage, exclusion reasons, all counts, source hashes and audit evidence before
presenting it as final. A failure never becomes a successful report. No claim
of 100% real-world execution accuracy is made.

Outputs: `/home/hoang/fleet-1017/out/orb-tick-six-month-20261020/`.
Milestone log: `/home/hoang/fleet-1017/out/orb-six-month-milestone-20261020.log`.
These paths are planned until the job creates them; completed pilot data is
not mislabeled as the six-month deliverable.
