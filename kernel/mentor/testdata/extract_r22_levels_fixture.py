#!/usr/bin/env python3
"""Extract trading-day 1m tapes for the r22 LEVELS tests (10-13 and 10-12).

READ-ONLY: opens the DB with mode=ro (sqlite URI), never writes.
Window per session: 1600 1m bars BEFORE the 17:00 CT session open (the
evaluator's 1500-bar live window needs them for the first tick) through the
16:00 CT close — trading day 10-13 (opens 10-12 17:00) and trading day 10-12
(opens Sun 10-11 17:00),
MNQ 12-26, the same source filter the store readers use
(source NOT IN ('mixed','replay:off-scale')).

Usage:
  python3 extract_r22_levels_fixture.py /home/hoang/vl/data/data.db <out-dir>
"""
import json
import sqlite3
import sys

SESSIONS = [
    # (fixture name, trading day, open ms, close ms) — CT is CDT (UTC-5)
    ("mnq_1m_2026-10-13_session", "2026-10-13", 1791842400000, 1791925200000),
    ("mnq_1m_2026-10-12_session", "2026-10-12", 1791756000000, 1791838800000),
]
PRE_BARS = 1600
CONTRACT = "MNQ 12-26"


def main() -> None:
    db, out = sys.argv[1], sys.argv[2]
    con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    cur = con.cursor()
    for name, day, open_ms, end_ms in SESSIONS:
        cur.execute(
            """SELECT open_time_ms, o, h, l, c FROM bars
               WHERE symbol='MNQ' AND tf='1m' AND contract=? AND open_time_ms<?
                 AND source NOT IN ('mixed','replay:off-scale')
               ORDER BY open_time_ms DESC LIMIT ?""",
            (CONTRACT, open_ms, PRE_BARS),
        )
        pre = list(reversed(cur.fetchall()))
        cur.execute(
            """SELECT open_time_ms, o, h, l, c FROM bars
               WHERE symbol='MNQ' AND tf='1m' AND contract=? AND open_time_ms>=? AND open_time_ms<?
                 AND source NOT IN ('mixed','replay:off-scale')
               ORDER BY open_time_ms""",
            (CONTRACT, open_ms, end_ms),
        )
        sess = cur.fetchall()
        rows = [[ot, o, h, l, c] for ot, o, h, l, c in pre + sess]
        with open(f"{out}/{name}.json", "w") as f:
            json.dump(
                {"symbol": "MNQ", "date": day, "from": "pre-1600", "to": "16:00",
                 "window_ct_ms": [open_ms, end_ms], "contract": CONTRACT,
                 "bars": {"1m": rows}},
                f, separators=(",", ":"),
            )
        print(f"{name}: pre={len(pre)} session={len(sess)} total={len(rows)}")
    con.close()


if __name__ == "__main__":
    main()
