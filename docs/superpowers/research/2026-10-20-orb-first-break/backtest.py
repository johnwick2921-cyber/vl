#!/usr/bin/env python3
"""Offline one-trade ORB. Read-only SQLite input; exclusive-create artifacts.

No guessed intrabar path: uncertain first break / exit ordering is unresolved.
Prices are integer MNQ ticks (0.25 points). Date bounds are Houston-local.
"""
import argparse
import collections
import csv
import datetime as dt
import gzip
import hashlib
import json
import math
from pathlib import Path
import sqlite3
from zoneinfo import ZoneInfo

CT = ZoneInfo("America/Chicago")
TICK = 0.25
STOP = 80
OPEN = 8 * 60 + 30
CLOSE = 15 * 60


def simulate(bars, minutes):
    """bars: ordered (epoch_open_ms, o,h,l,c) ticks, exactly 390 minutes."""
    high = max(b[2] for b in bars[:minutes])
    low = min(b[3] for b in bars[:minutes])
    width = high - low
    result = dict(orb_minutes=minutes, orb_high=high*TICK,
                  orb_low=low*TICK, width_points=width*TICK,
                  outcome="NO_ENTRY", entry_ms="", side="", entry="",
                  exit_ms="", exit="", pnl_points="", reason="",
                  stop_mfe_min_points="", stop_mfe_max_points="")
    if width <= 0:
        result.update(outcome="INVALID", reason="zero_range")
        return result
    upper, lower = high+1, low-1
    candidates = []
    for first, (stamp, o, h, l, c) in enumerate(bars[minutes:], start=minutes):
        if o >= upper:
            candidates = [(1, o, True)]
        elif o <= lower:
            candidates = [(-1, o, True)]
        else:
            if h >= upper:
                candidates.append((1, upper, False))
            if l <= lower:
                candidates.append((-1, lower, False))
        if candidates:
            break
    if not candidates:
        return result
    outcomes = []
    reasons = set()
    if len(candidates) > 1:
        reasons.add("both_breakout_sides_in_first_trigger_bar")
    for side, entry, entry_at_open in candidates:
        stop, target = entry-side*STOP, entry+side*width
        mfe_min = mfe_max = 0
        for index in range(first, len(bars)):
            stamp, o, h, l, c = bars[index]
            entry_bar = index == first

            def record(kind, price, low_mfe, high_mfe):
                outcomes.append(dict(kind=kind,price=price*TICK,stamp=stamp,
                                     pnl=(price-entry)*side*TICK,
                                     mfe_min=low_mfe*TICK,mfe_max=high_mfe*TICK))

            # At subsequent bar opens price gaps can worsen a stop fill.
            # Target limits receive no assumed favorable price improvement.
            if not entry_bar:
                if (o-stop)*side <= 0:
                    record("SL",o,mfe_min,mfe_max)
                    break
                if (o-target)*side >= 0:
                    record("TP",target,mfe_min,mfe_max)
                    break
            adverse = l if side == 1 else h
            favorable = h if side == 1 else l
            hit_stop = (adverse-stop)*side <= 0
            hit_target = (favorable-target)*side >= 0
            stop_min = max(mfe_min, (o-entry)*side if not entry_bar else 0)
            stop_max = max(mfe_max, (favorable-entry)*side, 0)
            if hit_stop:
                record("SL",stop,stop_min,stop_max)
                if hit_target:
                    record("TP",target,mfe_min,mfe_max)
                    reasons.add("stop_and_target_same_bar")
                    break
                if (entry_bar and not entry_at_open and len(candidates)==1
                        and (c-stop)*side > 0):
                    # Low/high could precede the breakout. Keep BOTH branches:
                    # stopped now, or still alive. A later SL collapses both
                    # branches to a definite SL, without inventing an order.
                    reasons.add("entry_bar_stop_order_unknown")
                else:
                    break
            elif hit_target:
                record("TP",target,mfe_min,mfe_max)
                break
            mfe_min = mfe_max = max(mfe_max,(favorable-entry)*side,0)
            if index == len(bars)-1:
                record("TIME",c,mfe_min,mfe_max)
    kinds = sorted({x["kind"] for x in outcomes})
    prices = {x["price"] for x in outcomes}
    pnls = [x["pnl"] for x in outcomes]
    stamps = {x["stamp"] for x in outcomes}
    result.update(
        outcome=kinds[0] if len(kinds)==1 else "UNRESOLVED",
        possible_outcomes="|".join(kinds), entry_ms=bars[first][0],
        side=("LONG" if candidates[0][0]==1 else "SHORT") if len(candidates)==1 else "UNKNOWN",
        entry=candidates[0][1]*TICK if len(candidates)==1 else "",
        exit_ms=next(iter(stamps)) if len(stamps)==1 else "",
        exit=next(iter(prices)) if len(prices)==1 else "",
        pnl_points=pnls[0] if min(pnls)==max(pnls) else "",
        pnl_min_points=min(pnls),pnl_max_points=max(pnls),
        reason="|".join(sorted(reasons)),
    )
    if kinds == ["SL"]:
        result.update(stop_mfe_min_points=min(x["mfe_min"] for x in outcomes),
                      stop_mfe_max_points=max(x["mfe_max"] for x in outcomes))
    return result


def write_csv(path, rows):
    if not rows:
        return
    with path.open("x", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(dict.fromkeys(k for row in rows for k in row)))
        w.writeheader()
        w.writerows(rows)


def run(db, output, start, end, input_csv=None):
    output.mkdir(parents=True, exist_ok=False)
    # Pull extra minute at the end for imported close-stamped rows.
    lo = int(dt.datetime.combine(start, dt.time(), CT).timestamp()*1000)
    hi = int(dt.datetime.combine(end, dt.time(), CT).timestamp()*1000)+60000
    query = """SELECT contract,open_time_ms,o,h,l,c,v,source
               FROM bars WHERE symbol='MNQ' AND tf='1m'
               AND open_time_ms>=? AND open_time_ms<?
               ORDER BY contract,open_time_ms"""
    sessions = collections.defaultdict(lambda: collections.defaultdict(dict))
    prevol = collections.defaultdict(float)
    bad = set()
    sources = collections.Counter()
    digest = hashlib.sha256()
    rows_read = 0
    conn = None
    input_file = None
    if input_csv:
        input_file = gzip.open(input_csv, "rt", newline="")
        reader = csv.DictReader(input_file)
        rows = ((r["contract"],int(r["stored_ms"]),
                 *(float(r[k]) if r[k] else None for k in ("o","h","l","c","v")),
                 r["source"] or None) for r in reader)
    else:
        conn = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
        conn.execute("PRAGMA query_only=ON")
        rows = conn.execute(query, (lo, hi))
    with gzip.open(output/"input-1m.csv.gz", "xt", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["contract","stored_ms","o","h","l","c","v","source"])
        for contract, stamp, o, h, l, c, v, source in rows:
            raw = (contract, stamp, o, h, l, c, v, source)
            writer.writerow(raw)
            digest.update((json.dumps(raw, separators=(",", ":"))+"\n").encode())
            rows_read += 1
            sources[str(source)] += 1
            if source not in ("historical_import", "historical", "live"):
                continue
            opened = stamp-60000 if source == "historical_import" else stamp
            local = dt.datetime.fromtimestamp(opened/1000, CT)
            date = local.date()
            if not start <= date < end or date.weekday() >= 5:
                continue
            minute = local.hour*60+local.minute
            if not 8*60 <= minute < CLOSE:
                continue
            key = (date, contract)
            valid = (local.second == 0 and opened % 60000 == 0 and
                     all(x is not None and math.isfinite(x) for x in (o,h,l,c,v)) and
                     0 < l <= min(o,c) <= max(o,c) <= h and v >= 0 and
                     all(abs(x*4-round(x*4)) < 1e-6 for x in (o,h,l,c)))
            if not valid:
                bad.add(key)
                continue
            bar = (opened, *(round(x*4) for x in (o,h,l,c)))
            old = sessions[date][contract].get(minute)
            if old is not None and old != bar:
                bad.add(key)
            if minute < OPEN and old is None:
                prevol[key] += v
            sessions[date][contract][minute] = bar
    if conn:
        conn.close()
    if input_file:
        input_file.close()
    trades, census = [], []
    date = start
    while date < end:
        if date.weekday() >= 5:
            date += dt.timedelta(days=1)
            continue
        contracts = sessions.get(date, {})
        eligible = [(prevol[(date,k)], k) for k in contracts if prevol[(date,k)] > 0]
        chosen = max(eligible)[1] if eligible else ""
        reason = ""
        if not contracts:
            reason = "no_session_rows_holiday_or_missing"
        elif not chosen:
            reason = "no_preopen_volume_for_contract_selection"
        else:
            table = contracts[chosen]
            if (date,chosen) in bad:
                reason = "invalid_or_conflicting_normalized_rows"
            elif any(m not in table for m in range(OPEN,CLOSE)):
                reason = "incomplete_390_minute_session_including_early_closes"
        census.append(dict(date=date.isoformat(),contract=chosen,reason=reason or "included",
                           rth_bars=sum(m in contracts.get(chosen,{}) for m in range(OPEN,CLOSE))))
        if not reason:
            bars = [contracts[chosen][m] for m in range(OPEN,CLOSE)]
            for minutes in (1,2,5):
                result = simulate(bars,minutes)
                trades.append(dict(date=date.isoformat(),contract=chosen,**result))
        date += dt.timedelta(days=1)
    write_csv(output/"trades.csv", trades)
    write_csv(output/"sessions.csv", census)
    summaries = []
    for minutes in (1,2,5):
        group = [t for t in trades if t["orb_minutes"] == minutes]
        counts = collections.Counter(t["outcome"] for t in group)
        n = sum(counts[k] for k in ("TP","SL","TIME","UNRESOLVED"))
        stops = [t for t in group if t["outcome"] == "SL"]
        resolved = [t for t in group if t["outcome"] in ("TP","SL","TIME")]
        summaries.append(dict(
            orb_minutes=minutes,counts=dict(counts),entries=n,
            tp_rate_lower_pct=100*counts["TP"]/n if n else None,
            tp_rate_upper_pct=100*(counts["TP"]+counts["UNRESOLVED"])/n if n else None,
            resolved_only_tp_pct=100*counts["TP"]/len(resolved) if resolved else None,
            definite_comeback_stops=sum(t["stop_mfe_min_points"]>=TICK for t in stops),
            possible_comeback_stops=sum(t["stop_mfe_max_points"]>=TICK for t in stops),
            gross_points_lower=sum(t["pnl_min_points"] for t in group if "pnl_min_points" in t),
            gross_points_upper=sum(t["pnl_max_points"] for t in group if "pnl_max_points" in t),
            net_points_lower_2pt_cost=sum(t["pnl_min_points"]-2 for t in group if "pnl_min_points" in t),
            net_points_upper_2pt_cost=sum(t["pnl_max_points"]-2 for t in group if "pnl_max_points" in t),
            ambiguous_reasons=dict(collections.Counter(t["reason"] for t in group if t["outcome"]=="UNRESOLVED")),
        ))
    summary = dict(start_inclusive=str(start),end_exclusive=str(end),
                   timezone="America/Chicago",input_rows=rows_read,
                   input_canonical_sha256=digest.hexdigest(),sources=dict(sources),
                   sessions=len(census),session_counts=dict(collections.Counter(s["reason"] for s in census)),
                   results=summaries,
                   assumptions=dict(entry_buffer_points=.25,stop_points=20,
                                    target="1x ORB width from actual entry",
                                    session="08:30-15:00 CT",trades_per_day_per_test=1,
                                    contract_selection="highest observed 08:00-08:30 same-day volume; before entry",
                                    timestamp="historical_import -60000ms; live/historical unchanged",
                                    cost_points_round_trip=2,
                                    cost_note="scenario inherited from prior research; not measured broker fees"))
    with (output/"summary.json").open("x") as f:
        json.dump(summary,f,indent=2)
    print(json.dumps(summary,indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    inputs = parser.add_mutually_exclusive_group(required=True)
    inputs.add_argument("--db")
    inputs.add_argument("--input-csv")
    parser.add_argument("--out",required=True)
    parser.add_argument("--start",default="2025-10-20")
    parser.add_argument("--end",default="2026-10-20")
    args = parser.parse_args()
    run(Path(args.db) if args.db else None,Path(args.out),
        dt.date.fromisoformat(args.start),dt.date.fromisoformat(args.end),args.input_csv)
