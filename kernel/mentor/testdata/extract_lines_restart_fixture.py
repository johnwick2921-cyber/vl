#!/usr/bin/env python3
"""Extract the LINES-RESTART real-bar fixture (release #22).

READ-ONLY: opens the DB with mode=ro. Two MNQ 12-26 1m spans (CT):
  A  2026-10-06 13:00 -> 2026-10-08 16:00  (the 10-07/10-08 sessions — P1-1's
     11/138 HTF-verdict divergence: 10-07 21:00 4h bucket broke high 21:33,
     then low 22:52)
  B  2026-10-12 05:00 -> 2026-10-14 02:15  (the 10-13 session — the 05:00 4h
     bucket went short 08:28 then long 08:41 — plus the 18:57 and 02:10 boots)
Same filters as the store readers: source NOT IN ('mixed','replay:off-scale').

Usage:
  python3 -I extract_lines_restart_fixture.py <db-path> <out.json.gz>
"""
import gzip
import json
import sqlite3
import sys
from datetime import datetime
from zoneinfo import ZoneInfo

CT = ZoneInfo("America/Chicago")
SPANS = [
    ("A", "2026-10-06 13:00", "2026-10-08 16:00"),
    ("B", "2026-10-12 05:00", "2026-10-14 02:15"),
]


def ct_ms(s: str) -> int:
    return int(datetime.strptime(s, "%Y-%m-%d %H:%M").replace(tzinfo=CT).timestamp() * 1000)


def main() -> None:
    db, out = sys.argv[1], sys.argv[2]
    con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    cur = con.cursor()
    doc = {"symbol": "MNQ", "contract": "MNQ 12-26", "tf": "1m", "spans": {}}
    for label, frm, to in SPANS:
        cur.execute(
            """SELECT open_time_ms, o, h, l, c, v FROM bars
               WHERE symbol='MNQ' AND tf='1m' AND contract='MNQ 12-26'
                 AND COALESCE(source,'') NOT IN ('mixed','replay:off-scale')
                 AND open_time_ms>=? AND open_time_ms<? ORDER BY open_time_ms""",
            (ct_ms(frm), ct_ms(to)),
        )
        rows = [[r[0], r[1], r[2], r[3], r[4], r[5] or 0] for r in cur.fetchall()]
        doc["spans"][label] = {"from_ct": frm, "to_ct": to, "bars": rows}
        print(label, len(rows))
    with gzip.open(out, "wt") as f:
        json.dump(doc, f, separators=(",", ":"))


if __name__ == "__main__":
    main()
