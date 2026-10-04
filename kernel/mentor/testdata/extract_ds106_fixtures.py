#!/usr/bin/env python3
"""Extract DS-106 P2 fixtures (HTF direction + day gate) from the READ-ONLY
DB copy.

READ-ONLY: opens /home/hoang/mm-course/mentor-mode/db-copy/data.db with mode=ro,
never the live DB.

Windows (CT, America/Chicago — DST-aware):
  - mnq_5m_2026-09-15_globex : §7 pre-session window 2026-09-14 17:00 CT →
    2026-09-15 08:30 CT (the §7 "daily already run" measurement).
  - mnq_4h_2026-09-14to10-02 : 4h trigger tape (~18 days) for the §5.4 gate.
  - mnq_1h_2026-09-28to10-02 : 1h trigger tape (~4.7 days) for the §5.4 gate.

Usage:
  python3 extract_ds106_fixtures.py <db-copy-path> <out-dir>
"""
import json
import sqlite3
import sys
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

CT = ZoneInfo("America/Chicago")

# (label, date-from CT, from_hhmm, date-to CT, to_hhmm, timeframes)
WINDOWS = [
    ("mnq_5m_2026-09-15_globex", "2026-09-14", "17:00", "2026-09-15", "08:30",
     ["5m"]),
    ("mnq_4h_2026-09-14to10-02", "2026-09-14", "17:00", "2026-10-02", "17:00",
     ["4h"]),
    ("mnq_1h_2026-09-28to10-02", "2026-09-28", "00:00", "2026-10-02", "17:00",
     ["1h"]),
]


def ct_ms(date_str: str, hhmm: str) -> int:
    d = datetime.strptime(date_str + " " + hhmm, "%Y-%m-%d %H:%M").replace(
        tzinfo=CT)
    return int(d.timestamp() * 1000)


def main() -> None:
    db, out = sys.argv[1], sys.argv[2]
    con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    cur = con.cursor()
    for label, dfrom, frm, dto, to, tfs in WINDOWS:
        lo, hi = ct_ms(dfrom, frm), ct_ms(dto, to)
        bars = {}
        contract = None
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
                   WHERE symbol='MNQ' AND tf=? AND contract=? AND open_time_ms>=?
                   AND open_time_ms<? ORDER BY open_time_ms""",
                (tf, contract, lo, hi),
            )
            bars[tf] = [[ot, o, h, l, c] for ot, o, h, l, c in cur.fetchall()]
        with open(f"{out}/{label}.json", "w") as f:
            json.dump(
                {"symbol": "MNQ", "date": dfrom, "from": frm, "to_date": dto,
                 "to": to, "window_ct_ms": [lo, hi], "contract": contract,
                 "bars": bars},
                f,
            )
            print(f"{label}: " + ", ".join(f"{tf}={len(v)}" for tf, v in bars.items()))
    con.close()


if __name__ == "__main__":
    main()
