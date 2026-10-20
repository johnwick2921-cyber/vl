"""Offline tick-path ORB research. No broker/DB/network access or invented fills.

Preserve provider order, including timestamp/sequence ties. An additional strict
tie sensitivity leaves conflicting price groups unresolved. Exclusive outputs.
"""
import argparse
from collections import Counter
import csv
from datetime import date, datetime, time, timedelta
from itertools import groupby
import json
from pathlib import Path
import warnings

import databento as db
import pull_mnq as p

UNIT = 250_000_000  # one MNQ tick in DBN fixed-point units
STOP = 80          # 20 points
MINUTE = 60_000_000_000


def boundaries(day):
    d = date.fromisoformat(day)
    return tuple(int(datetime.combine(d, time(h, m), p.CT).timestamp()) * 10**9
                 for h, m in ((8, 0), (8, 30), (15, 0)))


def replay(ticks, opened, closed, minutes, through=False, conservative_ties=False):
    # tuples: file row, event_ns, recv_ns, sequence, price_ticks, publisher
    end = opened + minutes * MINUTE
    opening = [t for t in ticks if opened <= t[1] < end]
    result = dict(orb_minutes=minutes, target_rule='through_1_tick' if through else 'touch',
                  ordering='conservative_ties' if conservative_ties else 'provider_record_order',
                  outcome='NO_ENTRY', reason='', side='', entry_row='', exit_row='',
                  entry_event_ns='', exit_event_ns='', entry_recv_ns='', exit_recv_ns='',
                  entry_points='', exit_points='', stop_points='', target_points='',
                  pnl_points='', net_2pt_scenario='', mfe_points='', comeback_stop='')
    if not opening:
        return dict(result, outcome='EXCLUDED', reason='missing_opening_range')
    if any(t[2] >= end for t in opening):
        return dict(result, outcome='EXCLUDED', reason='opening_range_record_received_after_decision')
    high, low = max(t[4] for t in opening), min(t[4] for t in opening)
    width = high-low
    result.update(orb_high=high/4, orb_low=low/4, width_points=width/4)
    if width <= 0:
        return dict(result, outcome='EXCLUDED', reason='zero_range')
    side, entry, mfe = 0, None, 0
    last = None
    # Never sort or deduplicate exchange records, even identical trade prints.
    for _, group in groupby((t for t in ticks if end <= t[1] < closed),
                            key=lambda t: (t[1], t[3], t[5])):
        batch = list(group)
        prices = {t[4] for t in batch}
        if not side:
            actionable = any(x >= high+1 or x <= low-1 for x in prices)
        else:
            actionable = any((x-stop)*side <= 0 or
                             (x-target)*side >= int(through) for x in prices)
        if conservative_ties and len(prices) > 1 and actionable:
            return dict(result, outcome='UNRESOLVED',
                        reason='different_prices_same_event_and_sequence',
                        ambiguous_first_row=batch[0][0], ambiguous_last_row=batch[-1][0])
        for t in batch:
            last = t
            price = t[4]
            if not side:
                if price >= high+1:
                    side = 1
                elif price <= low-1:
                    side = -1
                else:
                    continue
                entry = price  # observed trade, not an invented boundary fill
                stop, target = entry-side*STOP, entry+side*width
                result.update(side='LONG' if side == 1 else 'SHORT', entry_row=t[0],
                              entry_event_ns=t[1], entry_recv_ns=t[2], entry_points=entry/4,
                              stop_points=stop/4, target_points=target/4)
                continue       # entry print cannot also fill a later bracket
            favorable = (price-entry)*side
            mfe = max(mfe, favorable)
            stopped = (price-stop)*side <= 0
            targeted = (price-target)*side >= int(through)
            if stopped or targeted:
                exit_price = price if stopped else target
                pnl = (exit_price-entry)*side/4
                return dict(result, outcome='SL' if stopped else 'TP', exit_row=t[0],
                            exit_event_ns=t[1], exit_recv_ns=t[2], exit_points=exit_price/4,
                            pnl_points=pnl, net_2pt_scenario=pnl-2, mfe_points=mfe/4,
                            comeback_stop=bool(stopped and mfe > 0))
    if side:
        # Owner did not prescribe a time-exit fill. Do not invent one at 15:00.
        return dict(result, outcome='OPEN_AT_CUTOFF', mfe_points=mfe/4,
                    mark_event_ns=last[1], mark_points=last[4]/4,
                    mark_pnl_points=(last[4]-entry)*side/4)
    return result


def read_day(folder, day):
    receipt = json.loads((folder/'complete.json').read_text())
    raw = folder/'ticks.dbn.zst'
    p.require(receipt['download_complete'] and receipt['day'] == day, 'Bad receipt')
    p.require(p.digest(raw) == receipt['sha256'], 'Hash mismatch: '+day)
    pre, opened, closed = boundaries(day)
    volumes = Counter()
    store = db.DBNStore.from_file(raw)
    try:
        p.require(str(store.metadata.schema) == 'tbbo' and
                  store.metadata.dataset == p.DATASET, 'Wrong dataset/schema')
        mapping = {}
        for symbol, intervals in store.metadata.mappings.items():
            p.require(symbol in p.SYMBOLS, 'Unexpected symbol')
            for interval in intervals:
                if interval['start_date'] <= date.fromisoformat(day) < interval['end_date']:
                    iid = int(interval['symbol'])
                    p.require(iid not in mapping or mapping[iid] == symbol, 'Conflicting mapping')
                    mapping[iid] = symbol
        total = 0
        with warnings.catch_warnings():
            warnings.filterwarnings('error', message='.*truncated.*')
            for t in store:
                total += 1
                if pre <= t.ts_event < opened and t.ts_recv < opened:
                    volumes[int(t.instrument_id)] += t.size
        p.require(total == receipt['records'], 'Receipt row-count mismatch')
    finally:
        store.reader.close()
    census = dict(day=day, file_sha256=receipt['sha256'], records=total,
                  preopen_volume=json.dumps(dict(volumes), sort_keys=True), contract='',
                  reason='', selected_records=0, missing_minutes='', flags='')
    if not volumes:
        return dict(census, reason='no_preopen_volume_holiday_or_missing'), []
    # Equal volume is reported rather than silently favoring a contract by name.
    winners = [k for k, v in volumes.items() if v == max(volumes.values())]
    if len(winners) != 1:
        return dict(census, reason='preopen_contract_volume_tie'), []
    chosen = winners[0]
    p.require(chosen in mapping, 'Selected instrument has no raw-symbol mapping')
    census.update(contract=mapping[chosen], instrument_id=chosen)
    ticks, minutes_seen, flags, reasons = [], set(), Counter(), set()
    previous = None
    store = db.DBNStore.from_file(raw)
    try:
        with warnings.catch_warnings():
            warnings.filterwarnings('error', message='.*truncated.*')
            for index, t in enumerate(store):
                if t.instrument_id != chosen or not opened <= t.ts_event < closed:
                    continue
                if (str(t.action) != 'T' or t.price <= 0 or t.price % UNIT or t.size <= 0):
                    reasons.add('invalid_trade')
                flags[int(t.flags)] += 1
                row = (index, t.ts_event, t.ts_recv, t.sequence, t.price//UNIT, t.publisher_id)
                if t.ts_recv < t.ts_event:
                    reasons.add('negative_receive_delay')
                if previous:
                    if row[1] < previous[1]: reasons.add('event_time_regression')
                    if row[2] < previous[2]: reasons.add('receive_time_regression')
                    if row[5] != previous[5]: reasons.add('publisher_changed')
                    if row[3] < previous[3]: reasons.add('sequence_regression')
                previous = row
                ticks.append(row)
                minutes_seen.add((row[1]-opened)//MINUTE)
    finally:
        store.reader.close()
    missing = sorted(set(range(390))-minutes_seen)
    if missing: reasons.add('incomplete_390_minute_session_including_early_closes')
    # Conservative review gate until each nonzero flag is explicitly interpreted.
    if any(k for k in flags): reasons.add('nonzero_flags_require_review')
    census.update(selected_records=len(ticks), missing_minutes=json.dumps(missing),
                  flags=json.dumps(dict(flags), sort_keys=True), reason='|'.join(sorted(reasons)))
    return census, ticks


def write_csv(path, rows):
    fields = list(dict.fromkeys(k for row in rows for k in row))
    with path.open('x', newline='') as f:
        w = csv.DictWriter(f, fieldnames=fields)
        w.writeheader()
        w.writerows(rows)


def summarize(rows):
    output = []
    for m in (1, 2, 5):
        for rule, ordering in ((r,o) for r in ('touch','through_1_tick')
                               for o in ('provider_record_order','conservative_ties')):
            group = [x for x in rows if x['orb_minutes'] == m and x['target_rule'] == rule
                     and x['ordering'] == ordering]
            count = Counter(x['outcome'] for x in group)
            n = sum(count[x] for x in ('TP', 'SL', 'OPEN_AT_CUTOFF', 'UNRESOLVED'))
            closed = count['TP']+count['SL']
            output.append(dict(orb_minutes=m, target_rule=rule, ordering=ordering, sessions=len(group),
                               counts=dict(count), entry_or_unresolved=n,
                               tp_pct_of_entries=100*count['TP']/n if n else None,
                               sl_pct_of_entries=100*count['SL']/n if n else None,
                               unresolved_pct=100*count['UNRESOLVED']/n if n else None,
                               tp_pct_of_tp_sl_only=100*count['TP']/closed if closed else None,
                               comeback_stop=sum(x.get('comeback_stop') is True for x in group),
                               closed_only_gross_points=sum(x['pnl_points'] for x in group
                                                           if x['outcome'] in ('TP', 'SL')),
                               closed_only_net_2pt_scenario=sum(x['net_2pt_scenario'] for x in group
                                                               if x['outcome'] in ('TP', 'SL'))))
    return output


def run(root, out, start='2025-10-20', end='2026-04-20'):
    plan = p.make_plan(start, end)
    missing = [d['day'] for d in plan['days'] if not (root/d['day']/'complete.json').exists()]
    p.require(not missing, 'Not enough downloaded data; missing '+str(len(missing))+' weekdays')
    out.mkdir(parents=True, exist_ok=False)
    census, rows = [], []
    for d in plan['days']:
        day = d['day']
        c, ticks = read_day(root/day, day)
        census.append(c)
        if not c['reason']:
            _, opened, closed = boundaries(day)
            for m in (1, 2, 5):
                for through in (False, True):
                    for ties in (False, True):
                        rows.append(dict(day=day, contract=c['contract'],
                                         **replay(ticks, opened, closed, m, through, ties)))
        print(day, c['reason'] or 'processed', flush=True)
    write_csv(out/'sessions.csv', census)
    write_csv(out/'trades.csv', rows)
    report = dict(status='COMPUTED_PENDING_INDEPENDENT_VERIFICATION', start=start,
                  end_exclusive=end, candidate_weekdays=len(census),
                  included_sessions=sum(not x['reason'] for x in census),
                  excluded_sessions=sum(bool(x['reason']) for x in census),
                  summaries=summarize(rows), engine_sha256=p.digest(Path(__file__)),
                  caveat='Observed trade path, not broker fills. Missing-minute gate excludes early closes. '
                         'OPEN and UNRESOLVED outcomes are not fabricated realized PnL. '
                         '2-point friction is a scenario, not measured execution costs.')
    p.write_json(out/'summary.json', report)
    return report


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--start', default='2025-10-20')
    parser.add_argument('--end', default='2026-04-20')
    a = parser.parse_args()
    run(a.root, a.out, a.start, a.end)
