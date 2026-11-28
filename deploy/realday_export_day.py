#!/usr/bin/env python3
"""realday_export_day.py — per-session-day export for the m157 real-day replay.

Reads the LIVE data.db READ-ONLY and writes, into <outdir>, the fixture for the
CME session that CLOSES on <day> (i.e. <day-1> 17:00 CT -> <day> 16:00 CT):
  - mnq_realday.json.gz   {bars:{1m:[[ot,o,h,l,c,contract]...],1h:[...]}}
  - strategy_a5b7662e.json
  - live_record.json      {trades, arms, calendar, clock_steps}

Usage: python3 deploy/realday_export_day.py /tmp/realday-1021 2026-10-21
"""
import gzip, json, sqlite3, sys
from datetime import datetime, timezone, timedelta

CT = timezone(timedelta(hours=-5))  # CDT

def ct_ms(y, mo, d, h, mi):
    return int(datetime(y, mo, d, h, mi, tzinfo=CT).timestamp() * 1000)

def main():
    out = sys.argv[1] if len(sys.argv) > 1 else "/tmp/realday"
    day = sys.argv[2] if len(sys.argv) > 2 else "2026-10-21"
    y, mo, d = map(int, day.split("-"))
    # session: day-1 17:00 CT -> day 16:00 CT
    prev = datetime(y, mo, d, tzinfo=CT) - timedelta(days=1)
    from_ms = ct_ms(prev.year, prev.month, prev.day, 17, 0)
    to_ms = ct_ms(y, mo, d, 16, 0)

    db = "/home/hoang/vl/data/data.db"
    con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    cur = con.cursor()
    warm = 1787760000000  # ~56 days warm-up (levels/old-extremes/EMA seed)

    def bars(tf):
        cur.execute("SELECT open_time_ms,o,h,l,c,contract FROM bars WHERE symbol='MNQ' AND tf=? AND open_time_ms>=? AND open_time_ms<=? ORDER BY open_time_ms", (tf, warm, to_ms))
        return [[r[0], r[1], r[2], r[3], r[4], r[5]] for r in cur.fetchall()]

    import os
    os.makedirs(out, exist_ok=True)
    with gzip.open(f"{out}/mnq_realday.json.gz", "wt") as f:
        json.dump({"bars": {"1m": bars("1m"), "1h": bars("1h")}}, f)

    cur.execute("SELECT config FROM strategies WHERE id='a5b7662e-7bf7-49bb-9f09-7efa48f95ac8'")
    with open(f"{out}/strategy_a5b7662e.json", "w") as f:
        f.write(cur.fetchone()[0])

    cur.execute("SELECT id, side, entry_price, entry_time, exit_price, exit_time, realized_pnl, close_reason, trader_id FROM trader_positions WHERE exit_time>=? AND exit_time<=? ORDER BY id", (from_ms, to_ms))
    trades = [dict(id=r[0], side=r[1], entry=r[2], entry_ms=r[3], exit=r[4], exit_ms=r[5], pnl=r[6], reason=r[7], trader=r[8]) for r in cur.fetchall()]

    from_utc = datetime.fromtimestamp(from_ms/1000, tz=timezone.utc).strftime("%Y-%m-%d %H:%M:%S")
    cur.execute("SELECT id, scenario, side, entry_px, stop_px, target_px, state, created_at FROM armed_orders WHERE trader_id LIKE '%1781246265%' AND created_at >= ? ORDER BY id", (from_utc,))
    arms = [dict(id=r[0], scenario=r[1], side=r[2], entry=r[3], stop=r[4], target=r[5], state=r[6], created=r[7]) for r in cur.fetchall()]

    d0 = prev.strftime("%Y-%m-%d"); d1 = day; d2 = (datetime(y, mo, d, tzinfo=CT) + timedelta(days=1)).strftime("%Y-%m-%d")
    cur.execute("SELECT trade_date, source, events_json FROM calendar_slices WHERE trade_date IN (?,?,?) ORDER BY trade_date", (d0, d1, d2))
    cal = [dict(date=r[0], source=r[1], events=r[2]) for r in cur.fetchall()]

    with open(f"{out}/live_record.json", "w") as f:
        json.dump({"trades": trades, "arms": arms, "calendar": cal, "clock_steps": []}, f, indent=1)
    con.close()
    print(f"wrote {out} day={day}: trades={len(trades)} arms={len(arms)} calendar={len(cal)} bars1m={len(bars('1m')) if False else '?'}")

if __name__ == "__main__":
    main()
