de4cf900256570636a9077e028e092e8.json is the REAL job file of the 2026-10-04
install (release v2026.10.04.1, 52f1989ca -> a057ab834), copied read-only from
the box after it ended recovery_needed: "worker_swapped ran 3 times without
finishing (attempts cap 3)". It holds no secrets (paths, shas, boot-line
evidence only). worker_swap_restart_test.go rebuilds the state the worker
entered worker_swapped in (drops the last transition, re-enters
worker_swapped/started with attempts 1) and replays the restarts.
