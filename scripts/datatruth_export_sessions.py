#!/usr/bin/env python3
"""Read-only export of whole CME sessions (17:00 CT -> 17:00 CT) of MNQ 12-26,
1m and 5m, for the m446 session-volume (VWAP / profile) pins.

    python3 -I scripts/datatruth_export_sessions.py data/data.db OUT.json 2026-10-21 2026-10-22 2026-10-26 2026-10-19

Each argument is a session START date (17:00 CT). Opens the DB with mode=ro.
Each entry also carries the 24 h BEFORE the session (the prior session whole and
the 15:00 CT cash-close hour the eVWAP anchors on).
Output: {"YYYY-MM-DD": {"start":ms,"end":ms,"m1":[[t,o,h,l,c,v,rowid],...],"m5":[...]}}
"""
import datetime as dt
import json
import sqlite3
import sys
from zoneinfo import ZoneInfo

CT = ZoneInfo("America/Chicago")


def main(db_path, out, *days):
    db = sqlite3.connect("file:%s?mode=ro" % db_path, uri=True)
    res = {}
    for d in days:
        y, m, dd = map(int, d.split("-"))
        s = dt.datetime(y, m, dd, 17, 0, tzinfo=CT)
        a = int(s.timestamp() * 1000)
        b = int((s + dt.timedelta(days=1)).timestamp() * 1000)
        ent = {"start": a, "end": b}
        for k, tf in (("m1", "1m"), ("m5", "5m")):
            rows = db.execute(
                "select open_time_ms,o,h,l,c,v,rowid from bars where symbol='MNQ' and tf=? "
                "and contract='MNQ 12-26' and open_time_ms>=? and open_time_ms<? order by 1",
                (tf, a - 86400000, b),
            ).fetchall()
            ent[k] = [list(r) for r in rows]
        res[d] = ent
    with open(out, "w") as f:
        json.dump(res, f, separators=(",", ":"))


if __name__ == "__main__":
    main(*sys.argv[1:])
