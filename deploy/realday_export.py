#!/usr/bin/env python3
"""realday_export.py — export the m157 real-day replay fixture + live record.

Reads the LIVE data.db READ-ONLY (URI mode=ro) and writes, into a target dir:
  - mnq_realday.json.gz   {bars:{1m:[[ot,o,h,l,c,contract]...],1h:[...]}}
  - strategy_a5b7662e.json  the live mentor strategy config
  - live_record.json      {trades:[663..675], arms:[mentor armed_orders], calendar:[slices]}

Usage:  python3 deploy/realday_export.py /tmp/realday
The live DB is never written; bars are extracted read-only.
"""
import gzip
import json
import sqlite3
import sys

def main() -> None:
    out = sys.argv[1] if len(sys.argv) > 1 else "/tmp/realday"
    db = "/home/hoang/vl/data/data.db"
    con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    cur = con.cursor()

    warm = 1787760000000  # 2026-08-26 16:00 UTC warm-up (~56 days: levels/old-extremes/EMA seed)
    def bars(tf):
        cur.execute(
            "SELECT open_time_ms, o, h, l, c, contract FROM bars "
            "WHERE symbol='MNQ' AND tf=? AND open_time_ms>=? ORDER BY open_time_ms",
            (tf, warm),
        )
        return [[r[0], r[1], r[2], r[3], r[4], r[5]] for r in cur.fetchall()]

    with gzip.open(f"{out}/mnq_realday.json.gz", "wt") as f:
        json.dump({"bars": {"1m": bars("1m"), "1h": bars("1h")}}, f)

    cur.execute("SELECT config FROM strategies WHERE id='a5b7662e-7bf7-49bb-9f09-7efa48f95ac8'")
    with open(f"{out}/strategy_a5b7662e.json", "w") as f:
        f.write(cur.fetchone()[0])

    cur.execute(
        "SELECT id, side, entry_price, entry_time, exit_price, exit_time, realized_pnl, close_reason, trader_id "
        "FROM trader_positions WHERE id BETWEEN 663 AND 675 ORDER BY id"
    )
    trades = [dict(id=r[0], side=r[1], entry=r[2], entry_ms=r[3], exit=r[4],
                   exit_ms=r[5], pnl=r[6], reason=r[7], trader=r[8]) for r in cur.fetchall()]

    cur.execute(
        "SELECT id, scenario, side, entry_px, stop_px, target_px, state, created_at "
        "FROM armed_orders WHERE trader_id LIKE '%1781246265%' AND created_at >= '2026-10-20 17:00:00' ORDER BY id"
    )
    arms = [dict(id=r[0], scenario=r[1], side=r[2], entry=r[3], stop=r[4],
                 target=r[5], state=r[6], created=r[7]) for r in cur.fetchall()]

    cur.execute(
        "SELECT trade_date, source, events_json FROM calendar_slices "
        "WHERE trade_date IN ('2026-10-19','2026-10-20','2026-10-21') ORDER BY trade_date"
    )
    cal = [dict(date=r[0], source=r[1], events=r[2]) for r in cur.fetchall()]

    with open(f"{out}/live_record.json", "w") as f:
        json.dump({"trades": trades, "arms": arms, "calendar": cal}, f, indent=1)
    con.close()
    print(f"wrote {out}: trades={len(trades)} arms={len(arms)} calendar={len(cal)}")

if __name__ == "__main__":
    main()
