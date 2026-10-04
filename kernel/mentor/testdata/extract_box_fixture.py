#!/usr/bin/env python3
"""Extract the 13-Sep-2026 golden box fixture from the READ-ONLY db copy.

The course frame (D3.3 FTGH/FTGL part1_06-25.jpg, verified by the CTO) shows
two 1m boxes on Sun 13 Sep 2026:
  FTGL ~ 28,982 -> 29,015 (~33 pts); FTGH ~ 29,097 -> 29,105 (~8 pts).
Window: 17:00-19:00 CT. Contract MNQ 09-26 (the frame's Sep 2026 expiry).
"""
import json
import sqlite3
import sys
from datetime import datetime, timedelta, timezone

CT = timezone(timedelta(hours=-5))


def ct_ms(date_str: str, hhmm: str) -> int:
    d = datetime.strptime(date_str + " " + hhmm, "%Y-%m-%d %H:%M").replace(tzinfo=CT)
    return int(d.timestamp() * 1000)


def main() -> None:
    db, out = sys.argv[1], sys.argv[2]
    lo, hi = ct_ms("2026-09-13", "17:00"), ct_ms("2026-09-13", "19:00")
    con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    cur = con.cursor()
    cur.execute(
        """SELECT open_time_ms, o, h, l, c FROM bars
           WHERE symbol='MNQ' AND tf='1m' AND contract='MNQ 09-26'
             AND open_time_ms>=? AND open_time_ms<? ORDER BY open_time_ms""",
        (lo, hi),
    )
    rows = [[ot, o, h, l, c] for ot, o, h, l, c in cur.fetchall()]
    con.close()
    with open(f"{out}/mnq_1m_2026-09-13_boxframe.json", "w") as f:
        json.dump(
            {"symbol": "MNQ", "date": "2026-09-13", "from": "17:00", "to": "19:00",
             "window_ct_ms": [lo, hi], "contract": "MNQ 09-26", "bars": {"1m": rows}},
            f,
        )
    print(f"mnq_1m_2026-09-13_boxframe: 1m={len(rows)}")


if __name__ == "__main__":
    main()
