#!/usr/bin/env python3
"""Extract MNQ 12-26 1m fixtures for the oct round-2 box/trendline tests.
READ-ONLY (mode=ro). Usage: extract_round2_boxes.py <db> <out-dir>"""
import sqlite3, sys
from datetime import datetime
from zoneinfo import ZoneInfo
CT = ZoneInfo("America/Chicago")
W = [("r2_mnq_1m_20261029_1700_to_20261030_1200ct.csv", (2026,10,29,17,0), (2026,10,30,12,0)),
     ("r2_mnq_1m_20261101_1700_to_20261102_0000ct.csv", (2026,11,1,17,0), (2026,11,2,0,0)),
     ("r2_mnq_1m_20261028_1700_to_20261029_1200ct.csv", (2026,10,28,17,0), (2026,10,29,12,0))]
db = sqlite3.connect("file:%s?mode=ro" % sys.argv[1], uri=True)
for name, a, b in W:
    ms = lambda t: int(datetime(*t, tzinfo=CT).timestamp() * 1000)
    rows = db.execute("select open_time_ms,o,h,l,c,v from bars where symbol='MNQ' and tf='1m' and contract='MNQ 12-26' and open_time_ms>=? and open_time_ms<? order by open_time_ms", (ms(a), ms(b))).fetchall()
    with open(sys.argv[2] + "/" + name, "w") as f:
        for r in rows:
            f.write(",".join(str(x) for x in r) + "\n")
    print(name, len(rows))
