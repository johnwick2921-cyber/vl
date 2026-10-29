#!/usr/bin/env python3
"""Read-only export of the real-bar corpus the m446 data-truth replay tests use.

    python3 -I scripts/datatruth_export_corpus.py data/data.db OUT.json.gz '2026-10-19 04:00' '2026-10-29 07:00'

Opens the DB with mode=ro (never writes). Output: {"SYM|tf": [[t,o,h,l,c,v,rowid,source],...]}
for MNQ 12-26 and ES 12-26 at 1m/3m/5m/15m. Point DATATRUTH_CORPUS at the file
(default /tmp/datatruth-f7a/corpus_1019_1029.json.gz); the replay tests skip when absent.
"""
import datetime as dt
import gzip
import json
import sqlite3
import sys
from zoneinfo import ZoneInfo

CT = ZoneInfo("America/Chicago")


def ms(s):
    return int(dt.datetime.strptime(s, "%Y-%m-%d %H:%M").replace(tzinfo=CT).timestamp() * 1000)


def main(db_path, out, a, b):
    db = sqlite3.connect("file:%s?mode=ro" % db_path, uri=True)
    res = {}
    for sym, con in (("MNQ", "MNQ 12-26"), ("ES", "ES 12-26")):
        for tf in ("1m", "3m", "5m", "15m"):
            rows = db.execute(
                "select open_time_ms,o,h,l,c,v,rowid,source from bars "
                "where symbol=? and tf=? and contract=? and open_time_ms>=? and open_time_ms<? order by 1",
                (sym, tf, con, ms(a), ms(b)),
            ).fetchall()
            res["%s|%s" % (sym, tf)] = [list(r) for r in rows]
    with gzip.open(out, "wt") as f:
        json.dump(res, f, separators=(",", ":"))


if __name__ == "__main__":
    main(*sys.argv[1:5])
