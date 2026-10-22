#!/usr/bin/env python3
"""Generate single-session-day replay fixtures (m277 r3) from the live bars table, READ-ONLY.

Shape and seed convention are byte-identical to trader/testdata/mnq_1m_2026-10-21_multiday.json.gz
(m186 DS-108): {"bars":{"1m":[[openMs,o,h,l,c,contract],..],"1h":[..]},"seed_now_ct_ms":S,
"window_ct_ms":[S,E]}, compact JSON, MNQ contracts 'MNQ 09-26' + 'MNQ 12-26', ORDER BY open_time_ms,
contract, 1m and 1h both from the first 1m bar on record (seed depth = the whole warm-up history), cut
at window end + TAIL_MS so the first post-close bar exists. S = the session open (17:00 CT the prior
day; Sunday 17:00 CT for Monday), E = 16:00 CT of the trade date.

Usage: gen_day_fixtures.py <db> <outdir> <YYYY-MM-DD>...   (db opened with mode=ro)
Self-check: --selftest <db> <multiday.json.gz>  re-derives the DS-108 multi-day fixture byte-for-byte.
"""
import datetime, gzip, json, sqlite3, sys
from zoneinfo import ZoneInfo

CT = ZoneInfo("America/Chicago")
FIRST_1M = 1780437660000          # first MNQ 09-26 1m bar on record (the 10-21 fixture's first row)
TAIL_MS = 2 * 3600 * 1000         # bars kept after the 16:00 CT window end (10-21 fixture keeps ~2.7 h)
CONTRACTS = ("MNQ 09-26", "MNQ 12-26")


def ct_ms(y, m, d, hh):
    return int(datetime.datetime(y, m, d, hh, tzinfo=CT).timestamp() * 1000)


def session(date):
    y, m, d = map(int, date.split("-"))
    day = datetime.date(y, m, d)
    prev = day - datetime.timedelta(days=1)  # Monday's session opens Sunday 17:00 CT; Saturday has none
    return ct_ms(prev.year, prev.month, prev.day, 17), ct_ms(y, m, d, 16)


def rows(con, tf, last):
    q = ("select open_time_ms,o,h,l,c,contract from bars where symbol='MNQ' and tf=? and contract in (?,?) "
         "and open_time_ms>=? and open_time_ms<=? order by open_time_ms,contract")
    return [list(r) for r in con.execute(q, (tf, *CONTRACTS, FIRST_1M, last))]


def dump(con, seed, end, last):
    doc = {"bars": {"1m": rows(con, "1m", last), "1h": rows(con, "1h", last)},
           "seed_now_ct_ms": seed, "window_ct_ms": [seed, end]}
    return json.dumps(doc, separators=(",", ":")).encode()


def main(argv):
    if argv[0] == "--selftest":
        con = sqlite3.connect(f"file:{argv[1]}?mode=ro", uri=True)
        raw = gzip.open(argv[2]).read()
        d = json.loads(raw)
        last = d["bars"]["1m"][-1][0]
        # the multi-day fixture's 1h tail ends before the 1m tail; reuse each series' own last open
        doc = {"bars": {}, "seed_now_ct_ms": d["seed_now_ct_ms"], "window_ct_ms": d["window_ct_ms"]}
        for tf in ("1m", "1h"):
            doc["bars"][tf] = rows(con, tf, d["bars"][tf][-1][0])
        ok = json.dumps(doc, separators=(",", ":")).encode() == raw
        print("selftest byte-identical:", ok)
        return 0 if ok else 1
    db, outdir, dates = argv[0], argv[1], argv[2:]
    con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    for date in dates:
        seed, end = session(date)
        raw = dump(con, seed, end, end + TAIL_MS)
        path = f"{outdir}/mnq_1m_{date}_day.json.gz"
        with open(path, "wb") as f:
            f.write(gzip.compress(raw, 9, mtime=0))
        print(date, seed, end, len(raw), path)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
