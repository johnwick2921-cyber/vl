#!/usr/bin/env python3
"""Extract the MENTOR-SEED-STATE real-bar fixture (release #23 fix wave).

READ-ONLY: opens the DB with mode=ro. One MNQ 12-26 1m span (CT):
  2026-10-11 17:00 -> 2026-10-14 11:00  (the Sunday open through the 10-14
  release #22 boot at 08:22:40, its 03:44-07:47 history HOLE refilled at
  08:23:00, and the CPI 07:30 print)
Each row: [open_ms, o, h, l, c, v, refill] — refill=1 marks a 'historical'
row inside the 10-14 03:44-07:47 hole (the rows the AddOn's replay released
to the store at 08:23:00, AFTER the 08:22:40 seed read the store).
Same filters as the store readers: source NOT IN ('mixed','replay:off-scale').

Usage:
  python3 -I extract_seed_state_fixture.py <db-path> <out.json.gz>
"""
import gzip
import json
import sqlite3
import sys
from datetime import datetime
from zoneinfo import ZoneInfo

CT = ZoneInfo("America/Chicago")
FROM, TO = "2026-10-11 17:00", "2026-10-14 11:00"
HOLE_FROM, HOLE_TO = "2026-10-14 03:44", "2026-10-14 07:47"


def ct_ms(s: str) -> int:
    return int(datetime.strptime(s, "%Y-%m-%d %H:%M").replace(tzinfo=CT).timestamp() * 1000)


def main() -> None:
    db, out = sys.argv[1], sys.argv[2]
    con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    cur = con.cursor()
    cur.execute(
        """SELECT open_time_ms, o, h, l, c, v, COALESCE(source,'') FROM bars
           WHERE symbol='MNQ' AND tf='1m' AND contract='MNQ 12-26'
             AND COALESCE(source,'') NOT IN ('mixed','replay:off-scale')
             AND open_time_ms>=? AND open_time_ms<? ORDER BY open_time_ms""",
        (ct_ms(FROM), ct_ms(TO)),
    )
    hf, ht = ct_ms(HOLE_FROM), ct_ms(HOLE_TO)
    rows = []
    for r in cur.fetchall():
        refill = 1 if (r[6] == "historical" and hf <= r[0] <= ht) else 0
        rows.append([r[0], r[1], r[2], r[3], r[4], r[5] or 0, refill])
    doc = {"symbol": "MNQ", "contract": "MNQ 12-26", "tf": "1m", "from_ct": FROM, "to_ct": TO,
           "hole_ct": [HOLE_FROM, HOLE_TO], "bars": rows}
    print(len(rows), "rows,", sum(r[6] for r in rows), "refill rows")
    with gzip.open(out, "wt") as f:
        json.dump(doc, f, separators=(",", ":"))


if __name__ == "__main__":
    main()
