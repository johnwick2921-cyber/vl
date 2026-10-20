#!/usr/bin/env python3
"""Offline MNQ research downloads only. No bot/DB access; no automatic paid retry."""
import argparse
from collections import Counter
from datetime import date, datetime, time, timedelta, timezone
from decimal import Decimal
import fcntl
import getpass
import hashlib
import json
import os
from pathlib import Path
import stat
import sys
import time as clock_time
import warnings
from uuid import uuid4
from zoneinfo import ZoneInfo

DATASET = 'GLBX.MDP3'
SYMBOLS = ['MNQZ5', 'MNQH6', 'MNQM6', 'MNQU6', 'MNQZ6']
CT = ZoneInfo('America/Chicago')
DEFAULT_KEY = Path('/home/hoang/.config/vl-research/databento.key')


class Stop(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise Stop(message)


def write_json(path, obj):
    with Path(path).open('x') as f:
        json.dump(obj, f, indent=2, sort_keys=True)
        f.write('\n')


def digest(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as f:
        for block in iter(lambda: f.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def ns(value):
    import pandas as pd
    t = pd.Timestamp(value)
    require(t.tzinfo is not None, 'Timestamp must include timezone')
    return t.value


def make_plan(start='2025-10-20', end='2026-10-20'):
    first, last = date.fromisoformat(start), date.fromisoformat(end)
    require(date(2025, 10, 20) <= first < last <= date(2026, 10, 20),
            'This contract list is scoped to 2025-10-20 through 2026-10-19')
    days = []
    day = first
    while day < last:
        if day.weekday() < 5:
            # Receive-time API bounds padded around event-time research hours.
            a = datetime.combine(day, time(7, 50), CT).astimezone(timezone.utc)
            b = datetime.combine(day, time(15, 10), CT).astimezone(timezone.utc)
            days.append({'day': str(day), 'request': {
                'dataset': DATASET, 'schema': 'tbbo', 'symbols': SYMBOLS,
                'stype_in': 'raw_symbol', 'start': a.isoformat(), 'end': b.isoformat()}})
        day += timedelta(days=1)
    return {'version': 1, 'start': start, 'end_exclusive': end,
            'timezone': 'America/Chicago', 'event_window': '08:00 <= time < 15:00',
            'orb_open': '08:30', 'days': days}


def load_plan(path):
    plan = json.loads(Path(path).read_text())
    require(plan == make_plan(plan['start'], plan['end_exclusive']),
            'Plan differs from the fixed MNQ research specification')
    return plan


def save_key(path):
    path = Path(path)
    require(not path.exists() and not path.is_symlink(),
            'Key file already exists; preserve it, choose another path')
    key = getpass.getpass('Databento API key (hidden): ').strip()
    require(key and not any(c.isspace() for c in key), 'Invalid empty/whitespace key')
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as f:
        f.write(key + '\n')
    print('Saved private key file; contents not displayed.')


def read_key(path):
    path = Path(path)
    require(not path.is_symlink(), 'Key must be a regular private file')
    s = path.stat()
    require(stat.S_ISREG(s.st_mode) and s.st_uid == os.getuid()
            and stat.S_IMODE(s.st_mode) & 0o077 == 0,
            'Key must be owned by this user with mode 0600')
    value = path.read_text().strip()
    require(value and not any(c.isspace() for c in value), 'Invalid key file')
    return value


def client_for(path):
    import databento as db
    # Never read or mutate the bot .env, account config, or existing credentials.
    return db.Historical(read_key(path))


def available(client, plan):
    ranges = client.metadata.get_dataset_range(dataset=DATASET)
    r = ranges.get('schema', {}).get('tbbo', ranges)
    require(ns(r['start']) <= ns(plan['days'][0]['request']['start']) and
            ns(r['end']) >= ns(plan['days'][-1]['request']['end']),
            'Requested window outside TBBO availability; do not silently shorten year')
    return ranges


def money(value):
    d = Decimal(str(value))
    require(d.is_finite() and d >= 0, 'Cost estimate is invalid')
    return d


def quote(client, request):
    # Metadata only: retry transient server errors, never retry a data download.
    import databento as db
    for attempt in range(3):
        try:
            return money(client.metadata.get_cost(**request))
        except db.BentoServerError:
            if attempt == 2:
                raise Stop('Databento cost metadata server error at ' + request['start'])
            clock_time.sleep(attempt + 1)


def preflight(client, plan_path, out):
    plan = load_plan(plan_path)
    out = Path(out)
    out.mkdir(parents=True, exist_ok=False)
    write_json(out / 'availability.json', available(client, plan))
    resolved = client.symbology.resolve(dataset=DATASET, symbols=SYMBOLS,
        stype_in='raw_symbol', stype_out='instrument_id',
        # Resolve the study universe over its full lifetime window, even for a
        # one-day pilot. Expired candidates need not resolve on the pilot date.
        start_date='2025-10-20', end_date='2026-10-20')
    write_json(out / 'symbology.json', resolved)
    require(all(resolved.get('result', {}).get(s) for s in SYMBOLS),
            'At least one MNQ contract did not resolve; inspect symbology.json')
    total, size = Decimal(0), 0
    for day in plan['days']:
        req = day['request']
        cost = money(client.metadata.get_cost(**req))
        count = int(client.metadata.get_record_count(**req))
        raw_size = int(client.metadata.get_billable_size(**req))
        require(count >= 0 and raw_size >= 0, 'Invalid count/size')
        row = dict(day, cost_usd=str(cost), records=count, billable_bytes=raw_size)
        write_json(out / (day['day'] + '.json'), row)
        total += cost
        size += raw_size
        print(day['day'], 'records', count, 'estimated USD', cost, flush=True)
    result = {'plan_sha256': digest(plan_path), 'days': len(plan['days']),
              'estimated_usd': str(total), 'billable_bytes': size,
              'zero_extra_usage_quote': total == 0,
              'note': 'Estimate respects plan discounts; not a provider billing guarantee.'}
    write_json(out / 'summary.json', result)
    return result


def validate_dbn(path, request):
    import databento as db
    store = db.DBNStore.from_file(path)
    try:
        with warnings.catch_warnings():
            warnings.filterwarnings('error', message='.*truncated.*')
            return validate_records(store, request)
    finally:
        store.reader.close()


def validate_records(store, request):
    require(str(store.metadata.schema) == 'tbbo', 'Wrong DBN schema')
    require(store.metadata.dataset == DATASET, 'Wrong DBN dataset')
    count, prev, first, last = 0, None, None, None
    symbols, flags, minutes = Counter(), Counter(), Counter()
    event_regressions, quote_issues = 0, 0
    previous_event = {}
    lo, hi = ns(request['start']), ns(request['end'])
    for rec in store:
        require(hasattr(rec, 'levels') and str(rec.action) == 'T', 'Non-TBBO record')
        require(lo <= rec.ts_recv < hi, 'Record outside requested receive-time range')
        require(prev is None or rec.ts_recv >= prev, 'Receive timestamp regression')
        require(rec.price > 0 and rec.price % 250_000_000 == 0 and rec.size > 0,
                'Invalid MNQ trade price/tick/size')
        iid = str(rec.instrument_id)
        if iid in previous_event and rec.ts_event < previous_event[iid]:
            event_regressions += 1
        previous_event[iid] = rec.ts_event
        level = rec.levels[0]
        if (level.bid_px <= 0 or level.ask_px <= 0 or level.bid_px >= level.ask_px
                or level.ask_px >= 9223372036854775807
                or level.bid_px % 250_000_000 or level.ask_px % 250_000_000):
            quote_issues += 1
        symbols[iid] += 1
        flags[str(rec.flags)] += 1
        # Preserve original nanoseconds in DBN; this conversion is for coverage only.
        stamp = datetime.fromtimestamp(rec.ts_event // 1_000_000_000, CT)
        minutes[f'{iid}|{stamp:%Y-%m-%d %H:%M}'] += 1
        prev = rec.ts_recv
        first = rec.ts_event if first is None else min(first, rec.ts_event)
        last = rec.ts_event if last is None else max(last, rec.ts_event)
        count += 1
    return {'records': count, 'by_instrument_id': dict(symbols), 'flags': dict(flags),
            'first_event_ns': first, 'last_event_ns': last,
            'event_timestamp_regressions': event_regressions,
            'quote_issues': quote_issues, 'minute_trade_counts': dict(minutes),
            'review_required': count == 0 or event_regressions > 0 or quote_issues > 0,
            'coverage_note': 'No trades in a minute is not by itself proof of feed loss. '
                             'Calendar/flags/symbol mapping review still required.'}


def download_day(client, day, root, daily_quote, validator):
    import databento as db
    folder = root / day['day']
    for attempt in range(1, 4):
        folder.mkdir(exist_ok=False)
        write_json(folder / 'started.json', dict(day, quoted_usd=str(daily_quote), attempt=attempt))
        path = folder / 'ticks.dbn.zst'
        try:
            downloaded = client.timeseries.get_range(**day['request'], path=path)
        except db.BentoServerError as exc:
            write_json(folder / 'server-error.json', {
                'type': type(exc).__name__, 'attempt': attempt,
                'quoted_usd': str(daily_quote)})
            # Only a zero-priced request may retry automatically. Keep every
            # failed attempt, including partial bytes, before a new request.
            if daily_quote != 0 or attempt == 3:
                raise Stop('Data server error at ' + day['day'] + '; attempt preserved') from None
            archive = root / ('failed-attempt-' + day['day'] + '-' + uuid4().hex)
            archive.mkdir(exist_ok=False)
            folder.rename(archive / day['day'])
            print('Server retry', day['day'], attempt, 'preserved at', archive, flush=True)
            require(quote(client, day['request']) == 0, 'Retry quote is no longer zero')
            clock_time.sleep(attempt * 2)
            continue
        if downloaded is not None:
            downloaded.reader.close()
        validation = validator(path, day['request'])
        write_json(folder / 'validation.json', validation)
        done = {'day': day['day'], 'quoted_usd': str(daily_quote), 'bytes': path.stat().st_size,
                'sha256': digest(path), 'records': validation['records'],
                'download_complete': True, 'research_ready': False}
        write_json(folder / 'complete.json', done)
        print(day['day'], 'download validated:', validation['records'], 'records', flush=True)
        return done


def download(client, plan_path, out, max_cost='0', validator=validate_dbn):
    plan = load_plan(plan_path)
    cap = money(max_cost)
    root = Path(out)
    root.mkdir(parents=True, exist_ok=True)
    # Advisory process lock; file retained on exit, never deleted.
    with (root / '.download.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        identity = root / 'identity.json'
        expected = {'plan_sha256': digest(plan_path)}
        if identity.exists():
            require(json.loads(identity.read_text()) == expected, 'Different plan in output folder')
        else:
            write_json(identity, expected)
        available(client, plan)
        spent, pending = Decimal(0), []
        for day in plan['days']:
            folder = root / day['day']
            receipt = folder / 'complete.json'
            if receipt.exists():
                done = json.loads(receipt.read_text())
                require(digest(folder / 'ticks.dbn.zst') == done['sha256'],
                        'Saved data hash mismatch; preserve and investigate')
                spent += money(done['quoted_usd'])
            else:
                require(not folder.exists(), 'Interrupted attempt exists; preserve and inspect '
                        'before any retry: ' + str(folder))
                pending.append(day)
        # Monthly supersets include all pending daily windows plus intervening
        # nights/weekends. Smaller requests avoid the full-year gateway timeout.
        groups = {}
        for day in pending:
            groups.setdefault(day['day'][:7], []).append(day)
        month_quotes = {}
        for month, days in groups.items():
            req = dict(days[0]['request'], end=days[-1]['request']['end'])
            month_quotes[month] = quote(client, req)
            print('Quote', month, 'USD', month_quotes[month], flush=True)
        quotes = list(month_quotes.values())
        require(spent + sum(quotes, Decimal(0)) <= cap,
                'Estimated additional usage exceeds cap; no new data downloaded')
        for day in pending:
            original_quote = month_quotes[day['day'][:7]]
            # Recheck immediately before download; rolling plan windows can change.
            daily_quote = quote(client, day['request'])
            require(daily_quote <= original_quote and spent + daily_quote <= cap,
                    'Quote increased; stop before billable request')
            download_day(client, day, root, daily_quote, validator)
            spent += daily_quote
        return {'downloaded_now': len(pending), 'total_quoted_usd': str(spent)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    p = sub.add_parser('save-key')
    p.add_argument('--key-file', type=Path, default=DEFAULT_KEY)
    p = sub.add_parser('plan')
    p.add_argument('--start', default='2025-10-20')
    p.add_argument('--end', default='2026-10-20')
    p.add_argument('--out', type=Path, required=True)
    for command in ('preflight', 'download'):
        p = sub.add_parser(command)
        p.add_argument('--plan', type=Path, required=True)
        p.add_argument('--out', type=Path, required=True)
        p.add_argument('--key-file', type=Path, default=DEFAULT_KEY)
        if command == 'download':
            p.add_argument('--max-cost-usd', default='0')
    args = parser.parse_args()
    if args.command == 'save-key':
        save_key(args.key_file)
    elif args.command == 'plan':
        write_json(args.out, make_plan(args.start, args.end))
        print('Offline plan written:', args.out)
    else:
        client = client_for(args.key_file)
        if args.command == 'preflight':
            result = preflight(client, args.plan, args.out)
        else:
            result = download(client, args.plan, args.out, args.max_cost_usd)
        print(json.dumps(result, indent=2))


if __name__ == '__main__':
    try:
        main()
    except Exception as exc:
        # Third-party error text may contain request data. Do not print secrets.
        if isinstance(exc, (Stop, FileNotFoundError, FileExistsError)):
            print(f'STOP: {exc}', file=sys.stderr)
        else:
            print(f'STOP: {type(exc).__name__}; inspect locally, key not logged.', file=sys.stderr)
        sys.exit(1)
