#!/usr/bin/env python3
"""Independent chronological-price oracle and deliberate fault detection.

Synthetic paths verify logic, not the unknown historical tick ordering.
Creates new artifacts only. Does not modify the engine or live data.
"""
import argparse
import collections
import csv
import datetime as dt
import gzip
import hashlib
import importlib.util
import io
import json
import random
import sys
import types
import unittest
from pathlib import Path
from zoneinfo import ZoneInfo

import backtest


def oracle(paths, minutes):
    """Walk actual synthetic ticks in time order, no OHLC decision rules."""
    opening = [p for path in paths[:minutes] for p in path]
    upper, lower = max(opening)+1, min(opening)-1
    width = max(opening)-min(opening)
    side = 0
    entry = stop = target = mfe = 0
    if width <= 0:
        return "INVALID", 0, 0
    for minute in paths[minutes:]:
        for index, price in enumerate(minute):
            if side == 0:
                if price >= upper:
                    side = 1
                elif price <= lower:
                    side = -1
                else:
                    continue
                entry = price  # all within-minute steps are single ticks
                stop = entry-80*side
                target = entry+width*side
            elif side*(price-stop) <= 0:
                return "SL", side*(price-entry)*.25, mfe*.25
            elif side*(price-target) >= 0:
                return "TP", width*.25, mfe*.25
            mfe = max(mfe,side*(price-entry))
    if side:
        return "TIME",side*(paths[-1][-1]-entry)*.25,mfe*.25
    return "NO_ENTRY",0,0


def fuzz(cases=2000):
    rng = random.Random(20261020)
    counts = collections.Counter()
    checks = 0
    for case in range(cases):
        paths = []
        prev = 100000
        for minute in range(15):
            # Some discontinuities exist ONLY at bar open: exercise gap fills.
            start = prev+rng.choice([0,0,0,0,-100,100,-30,30])
            path = [start]
            for turn in range(10):
                step = rng.randint(-60,60)
                direction = 1 if step >= 0 else -1
                path.extend(range(path[-1]+direction,path[-1]+step+direction,direction))
            paths.append(path)
            prev = path[-1]
        bars = [(i*60000,p[0],max(p),min(p),p[-1]) for i,p in enumerate(paths)]
        for minutes in (1,2,5):
            kind,pnl,mfe = oracle(paths,minutes)
            result = backtest.simulate(bars,minutes)
            possible = result.get("possible_outcomes",result["outcome"]).split("|")
            assert kind in possible, (case,minutes,kind,result,bars)
            if "pnl_min_points" in result:
                assert result["pnl_min_points"] <= pnl <= result["pnl_max_points"], (case,minutes,pnl,result)
            if result["outcome"] == "SL":
                assert result["stop_mfe_min_points"] <= mfe <= result["stop_mfe_max_points"], (case,minutes,mfe,result)
            if result["outcome"] != "UNRESOLVED":
                assert result["outcome"] == kind
            counts[kind] += 1
            checks += 1
    return dict(seed=20261020,synthetic_paths=cases,comparisons=checks,
                actual_path_outcomes=dict(counts),result="PASS")


def mutants():
    base = Path(__file__).parent
    source = (base/"backtest.py").read_text()
    mutations = [
        ("wrong_stop_21_points","STOP = 80","STOP = 84"),
        ("no_breakout_tick","upper, lower = high+1, low-1","upper, lower = high, low"),
        ("lookahead_orb_high","max(b[2] for b in bars[:minutes])","max(b[2] for b in bars[:minutes+1])"),
        ("stop_gap_filled_at_old_stop","record(\"SL\",o,mfe_min,mfe_max)","record(\"SL\",stop,mfe_min,mfe_max)"),
        ("wrong_target_distance","entry+side*width","entry+side*STOP"),
        ("erase_surviving_entry_branch","entry_bar and not entry_at_open and len(candidates)==1","entry_bar and not entry_at_open and False"),
    ]
    results = []
    for name,old,new in mutations:
        assert source.count(old)==1,(name,source.count(old))
        mod = types.ModuleType("backtest")
        exec(compile(source.replace(old,new),"mutant_"+name,"exec"),mod.__dict__)
        sys.modules["backtest"] = mod
        try:
            spec = importlib.util.spec_from_file_location("test_mutant_"+name,base/"test_backtest.py")
            tests = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(tests)
            stream = io.StringIO()
            result = unittest.TextTestRunner(stream=stream).run(unittest.defaultTestLoader.loadTestsFromModule(tests))
            assert not result.wasSuccessful(), "SURVIVING MUTANT: "+name
            results.append(dict(name=name,killed=True,failures=len(result.failures),errors=len(result.errors)))
        finally:
            sys.modules["backtest"] = backtest
    return results


def crosscheck_csv():
    base = Path(__file__).parent
    rows = list(csv.DictReader((base/"trades.csv").open()))
    summary = json.loads((base/"summary.json").read_text())
    sessions = list(csv.DictReader((base/"sessions.csv").open()))
    included = {r["date"]:r["contract"] for r in sessions if r["reason"]=="included"}
    assert len(rows)==len({(r["date"],r["orb_minutes"]) for r in rows})==678
    all_groups = []
    for record in summary["results"]:
        tf = record["orb_minutes"]
        group = [r for r in rows if int(r["orb_minutes"])==tf]
        assert {r["date"] for r in group}==set(included)
        assert all(included[r["date"]]==r["contract"] for r in group)
        counts = dict(collections.Counter(r["outcome"] for r in group))
        assert counts==record["counts"]
        for key,column in [("gross_points_lower","pnl_min_points"),("gross_points_upper","pnl_max_points")]:
            assert sum(float(r[column]) for r in group)==record[key]
        all_groups.append(dict(orb_minutes=tf,counts=counts,rows=len(group)))
    return dict(result="PASS",groups=all_groups,rows=678,
                included_sessions=len(included),excluded_weekdays=len(sessions)-len(included))


def crosscheck_input(snapshot):
    """Recompute ORB and FIRST crossing from raw prices, not simulate()."""
    ct = ZoneInfo("America/Chicago")
    base = Path(__file__).parent
    summary = json.loads((base/"summary.json").read_text())
    trades = list(csv.DictReader((base/"trades.csv").open()))
    sessions = list(csv.DictReader((base/"sessions.csv").open()))
    observed = collections.defaultdict(dict)
    prevol = collections.defaultdict(float)
    sources = collections.Counter()
    digest = hashlib.sha256()
    n = 0
    with gzip.open(snapshot,"rt") as f:
        for row in csv.DictReader(f):
            n += 1
            raw = (row["contract"],int(row["stored_ms"]),
                   *(float(row[k]) if row[k] else None for k in ("o","h","l","c","v")),
                   row["source"] or None)
            digest.update((json.dumps(raw,separators=(",",":"))+"\n").encode())
            sources[str(raw[-1])] += 1
            if raw[-1] not in ("historical_import","live","historical"):
                continue
            timestamp = dt.datetime.fromtimestamp(raw[1]/1000,dt.timezone.utc)
            if raw[-1]=="historical_import":
                timestamp -= dt.timedelta(minutes=1)
            local = timestamp.astimezone(ct)
            minute = local.hour*60+local.minute
            if not 480 <= minute < 900:
                continue
            key=(local.date().isoformat(),raw[0])
            if minute not in observed[key] and minute<510:
                prevol[key] += raw[6]
            observed[key][minute]=(int(timestamp.timestamp()*1000),*raw[2:6])
    assert n==summary["input_rows"]
    assert dict(sources)==summary["sources"]
    assert digest.hexdigest()==summary["input_canonical_sha256"]
    for record in sessions:
        if record["reason"]!="included":
            continue
        date,contract=record["date"],record["contract"]
        selected=max((v,c) for (d,c),v in prevol.items() if d==date and v>0)[1]
        assert selected==contract
        assert all(m in observed[(date,contract)] for m in range(510,900))
    late=[]
    entry_times=collections.defaultdict(list)
    for trade in trades:
        table=observed[(trade["date"],trade["contract"])]
        length=int(trade["orb_minutes"])
        first_minutes=[table[i] for i in range(510,510+length)]
        top=max(x[2] for x in first_minutes)
        bottom=min(x[3] for x in first_minutes)
        assert top==float(trade["orb_high"])
        assert bottom==float(trade["orb_low"])
        assert top-bottom==float(trade["width_points"])
        trigger=next(m for m in range(510+length,900)
                     if table[m][2]>top or table[m][3]<bottom)
        stamp,o,h,l,c=table[trigger]
        assert stamp==int(trade["entry_ms"])
        entry_times[length].append(f"{trigger//60:02}:{trigger%60:02}")
        if o>top:
            side,entry="LONG",o
        elif o<bottom:
            side,entry="SHORT",o
        elif h>top and l<bottom:
            side,entry="UNKNOWN",None
        elif h>top:
            side,entry="LONG",top+.25
        else:
            side,entry="SHORT",bottom-.25
        assert side==trade["side"]
        assert (entry is None and trade["entry"]=="") or entry==float(trade["entry"])
        if trigger>=540:
            late.append(dict(date=trade["date"],orb_minutes=length,
                             entry_houston=f"{trigger//60:02}:{trigger%60:02}",
                             all_390_bars_present=True,earlier_bars_inside_orb=True))
    dst_checks=[]
    for date,contract,expected in [("2025-10-20","MNQ 12-25","13:30"),("2025-11-07","MNQ 12-25","14:30")]:
        stamp=observed[(date,contract)][510][0]
        actual=dt.datetime.fromtimestamp(stamp/1000,dt.timezone.utc).strftime("%H:%M")
        assert actual==expected
        dst_checks.append(dict(date=date,houston="08:30",utc=actual,result="PASS"))
    return dict(result="PASS",raw_rows=n,canonical_sha256=digest.hexdigest(),
                complete_sessions_verified=226,contract_choices_verified=226,
                orb_ranges_and_first_entries_verified=len(trades),
                houston_entry_times={str(k):dict(first=min(v),last=max(v),mode=collections.Counter(v).most_common(1)) for k,v in entry_times.items()},
                dst_checks=dst_checks,
                late_entry_examples=late[:10],
                late_entry_note="Late entry is delayed FIRST PRICE BREAKOUT; all opening/session bars are present.")


if __name__=="__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--out",required=True)
    parser.add_argument("--snapshot",required=True)
    args = parser.parse_args()
    report = dict(engine_sha256=hashlib.sha256((Path(__file__).parent/"backtest.py").read_bytes()).hexdigest(),
                  chronological_price_oracle=fuzz(),mutation_tests=mutants(),table_crosscheck=crosscheck_csv(),
                  independent_input_crosscheck=crosscheck_input(args.snapshot))
    with Path(args.out).open("x") as f:
        json.dump(report,f,indent=2)
    print(json.dumps(report,indent=2))
