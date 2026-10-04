#!/usr/bin/env python3
"""Extract recorded MNQ bar fixtures for the mentor evaluator tests.

READ-ONLY: opens the DB copy with mode=ro, never the live DB.
Source of truth per dispatch P2: the recorded bars the owner's bot stored
(the same tape the mentor rules must be evaluated on).

Usage:
  python3 extract_fixtures.py <db-copy-path> <out-dir>
"""
import json
import sqlite3
import sys
from datetime import datetime, timedelta, timezone

CT = timezone(timedelta(hours=-5))

# (label, date in CT, from_hhmm, to_hhmm, timeframes)
WINDOWS = [
    ("mnq_1m_2026-09-15_rth", "2026-09-15", "08:30", "15:00", ["1m"]),
    ("mnq_5m_2026-09-15_rth", "2026-09-15", "08:00", "15:00", ["5m"]),
    ("mnq_1m_2026-09-16_rth", "2026-09-16", "08:30", "15:00", ["1m"]),
    ("mnq_5m_2026-09-16_rth", "2026-09-16", "08:00", "15:00", ["5m"]),
    ("mnq_1m_2026-08-28_rth", "2026-08-28", "08:30", "15:00", ["1m"]),
    ("mnq_5m_2026-08-28_rth", "2026-08-28", "08:00", "15:00", ["5m"]),
]


def ct_ms(date_str: str, hhmm: str) -> int:
    d = datetime.strptime(date_str + " " + hhmm, "%Y-%m-%d %H:%M").replace(
        tzinfo=CT)
    return int(d.timestamp() * 1000)


def main() -> None:
    db, out = sys.argv[1], sys.argv[2]
    con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    cur = con.cursor()
    for label, date, frm, to, tfs in WINDOWS:
        lo, hi = ct_ms(date, frm), ct_ms(date, to)
        bars = {}
        for tf in tfs:
            cur.execute(
                """SELECT contract, count(*) FROM bars
                   WHERE symbol='MNQ' AND tf=? AND open_time_ms>=? AND open_time_ms<?
                   GROUP BY contract ORDER BY 2 DESC LIMIT 1""",
                (tf, lo, hi),
            )
            row = cur.fetchone()
            if row is None:
                print(f"{label} {tf}: no rows")
                continue
            contract = row[0]
            cur.execute(
                """SELECT open_time_ms, o, h, l, c FROM bars
                   WHERE symbol='MNQ' AND tf=? AND contract=? AND open_time_ms>=? AND open_time_ms<?
                   ORDER BY open_time_ms""",
                (tf, contract, lo, hi),
            )
            bars[tf] = [[ot, o, h, l, c] for ot, o, h, l, c in cur.fetchall()]
        with open(f"{out}/{label}.json", "w") as f:
            json.dump(
                {"symbol": "MNQ", "date": date, "from": frm, "to": to,
                 "window_ct_ms": [lo, hi], "contract": contract, "bars": bars},
                f,
            )
            print(f"{label}: " + ", ".join(f"{tf}={len(v)}" for tf, v in bars.items()))
    con.close()


if __name__ == "__main__":
    main()
