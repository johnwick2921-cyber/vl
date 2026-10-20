import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace

import databento_dbn as dbn
import databento as db
import zstandard
import pull_mnq as p


def fixture(path, req, wrong_tick=False, reverse_recv=False, reverse_event=False):
    start = p.ns(req['start'])
    meta = dbn.Metadata(dataset=p.DATASET, start=start, end=p.ns(req['end']),
        stype_in=dbn.SType.RAW_SYMBOL, stype_out=dbn.SType.INSTRUMENT_ID,
        schema=dbn.Schema.TBBO, symbols=p.SYMBOLS)
    records = []
    for i in range(2):
        event = start + (2 - i if reverse_event else i + 1) * 1_000_000_000
        recv = start + (3 - i if reverse_recv else i + 3) * 1_000_000_000
        records.append(dbn.MBP1Msg(publisher_id=1, instrument_id=123,
            ts_event=event, ts_recv=recv, price=25000_000_000_000 + int(wrong_tick),
            size=1, action=dbn.Action.TRADE, side=dbn.Side.BID, depth=0, sequence=i,
            levels=dbn.BidAskPair(bid_px=24999_750_000_000,
                                  ask_px=25000_000_000_000)))
    with Path(path).open('xb') as f:
        f.write(zstandard.ZstdCompressor().compress(meta.encode() + b''.join(bytes(r) for r in records)))


class Client:
    def __init__(self, costs=None, fail=False):
        self.costs = iter(costs) if costs else None
        self.calls = 0
        self.fail = fail
        self.metadata = SimpleNamespace(
            get_dataset_range=lambda **kw: {'start': '2020-01-01T00:00:00Z',
                                           'end': '2026-10-21T00:00:00Z'},
            get_cost=self.cost, get_record_count=lambda **kw: 2,
            get_billable_size=lambda **kw: 160)
        self.symbology = SimpleNamespace(resolve=lambda **kw:
            {'result': {s: [{'d0': '2025-10-20', 'd1': '2026-10-20', 's': '123'}]
                        for s in p.SYMBOLS}})
        self.timeseries = SimpleNamespace(get_range=self.get_range)

    def cost(self, **kw):
        return next(self.costs) if self.costs else 0

    def get_range(self, path, **req):
        self.calls += 1
        if self.fail:
            raise RuntimeError('interrupted')
        fixture(path, req)


class Tests(unittest.TestCase):
    def setUp(self):
        # Only this test's own temp directory is cleaned up by TemporaryDirectory.
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.plan = self.root / 'plan.json'
        p.write_json(self.plan, p.make_plan('2025-10-20', '2025-10-21'))
        self.req = p.load_plan(self.plan)['days'][0]['request']

    def test_year_dates_dst_and_exact_symbols(self):
        days = p.make_plan()['days']
        self.assertEqual(len(days), 261)
        self.assertEqual(days[-1]['day'], '2026-10-19')
        self.assertIn('12:50:00+00:00', days[0]['request']['start'])
        winter = next(x for x in days if x['day'] == '2025-11-07')
        self.assertIn('13:50:00+00:00', winter['request']['start'])
        self.assertTrue(all(x['request']['symbols'] == p.SYMBOLS for x in days))

    def test_real_compressed_dbn_download_validate_resume(self):
        c = Client()
        out = self.root / 'data'
        result = p.download(c, self.plan, out)
        self.assertEqual(result['downloaded_now'], 1)
        report = json.loads((out / '2025-10-20' / 'validation.json').read_text())
        self.assertEqual(report['records'], 2)
        self.assertFalse(report['review_required'])
        self.assertEqual(p.download(c, self.plan, out)['downloaded_now'], 0)
        self.assertEqual(c.calls, 1)

    def test_positive_quote_blocks_before_download(self):
        c = Client([0.01, 0.01])
        with self.assertRaises(p.Stop):
            p.download(c, self.plan, self.root / 'out')
        self.assertEqual(c.calls, 0)

    def test_full_budget_checked_before_any_day_downloads(self):
        plan = self.root / 'two-days.json'
        p.write_json(plan, p.make_plan('2025-10-31', '2025-11-04'))
        c = Client([0.75, 0.75, 0.75, 0.75])
        with self.assertRaises(p.Stop):
            p.download(c, plan, self.root / 'out', '1')
        self.assertEqual(c.calls, 0)

    def test_quote_increase_blocks(self):
        c = Client([0, 0.01])
        with self.assertRaises(p.Stop):
            p.download(c, self.plan, self.root / 'out', '1')
        self.assertEqual(c.calls, 0)

    def test_interrupted_attempt_never_auto_retried(self):
        c = Client(fail=True)
        out = self.root / 'out'
        with self.assertRaises(RuntimeError):
            p.download(c, self.plan, out)
        with self.assertRaises(p.Stop):
            p.download(c, self.plan, out)
        self.assertEqual(c.calls, 1)

    def test_hash_tampering_blocks_resume(self):
        c = Client()
        out = self.root / 'out'
        p.download(c, self.plan, out)
        with (out / '2025-10-20' / 'ticks.dbn.zst').open('ab') as f:
            f.write(b'corruption')
        with self.assertRaises(p.Stop):
            p.download(c, self.plan, out)
        self.assertEqual(c.calls, 1)

    def test_no_overwrite_and_plan_tamper(self):
        with self.assertRaises(FileExistsError):
            p.write_json(self.plan, {})
        bad = p.make_plan('2025-10-20', '2025-10-21')
        bad['days'][0]['request']['symbols'] = ['ALL_SYMBOLS']
        f = self.root / 'bad.json'
        p.write_json(f, bad)
        with self.assertRaises(p.Stop):
            p.load_plan(f)

    def test_schema_specific_availability(self):
        c = Client()
        c.metadata.get_dataset_range = lambda **kw: {
            'start': '2020-01-01T00:00:00Z', 'end': '2026-10-21T00:00:00Z',
            'schema': {'tbbo': {'start': '2026-01-01T00:00:00Z',
                                'end': '2026-10-21T00:00:00Z'}}}
        with self.assertRaises(p.Stop):
            p.download(c, self.plan, self.root / 'out')
        self.assertEqual(c.calls, 0)

    def test_preflight_metadata_only(self):
        c = Client()
        report = p.preflight(c, self.plan, self.root / 'quote')
        self.assertTrue(report['zero_extra_usage_quote'])
        self.assertEqual(c.calls, 0)

    def test_pilot_resolves_study_universe_not_only_pilot_day(self):
        c = Client()
        old = c.symbology.resolve
        seen = {}
        def resolve(**kw):
            seen.update(kw)
            return old(**kw)
        c.symbology.resolve = resolve
        p.preflight(c, self.plan, self.root / 'quote')
        self.assertEqual(seen['start_date'], '2025-10-20')
        self.assertEqual(seen['end_date'], '2026-10-20')

    def test_private_key_and_preserve_existing(self):
        key = self.root / 'key'
        with patch('getpass.getpass', return_value='synthetic-not-real'):
            p.save_key(key)
            self.assertEqual(p.read_key(key), 'synthetic-not-real')
            with self.assertRaises(p.Stop):
                p.save_key(key)
        key.chmod(0o644)
        with self.assertRaises(p.Stop):
            p.read_key(key)

    def test_real_dbn_validation_bad_tick_and_order(self):
        for option in ('wrong_tick', 'reverse_recv'):
            path = self.root / option
            fixture(path, self.req, **{option: True})
            with self.assertRaises(p.Stop):
                p.validate_dbn(path, self.req)

    def test_event_regression_is_reported_not_silently_sorted(self):
        path = self.root / 'event-order'
        fixture(path, self.req, reverse_event=True)
        result = p.validate_dbn(path, self.req)
        self.assertEqual(result['event_timestamp_regressions'], 1)
        self.assertTrue(result['review_required'])

    def test_invalid_cost_is_rejected(self):
        for value in ('NaN', 'Infinity', '-1'):
            with self.assertRaises(p.Stop):
                p.money(value)

    def test_metadata_server_retry_is_bounded(self):
        c = Client()
        with patch.object(c.metadata, 'get_cost', side_effect=[
                db.BentoServerError(504), 0]) as cost, patch.object(p.clock_time, 'sleep'):
            self.assertEqual(p.quote(c, self.req), 0)
            self.assertEqual(cost.call_count, 2)
        with patch.object(c.metadata, 'get_cost', side_effect=db.BentoServerError(504)) as cost, \
                patch.object(p.clock_time, 'sleep'):
            with self.assertRaises(p.Stop):
                p.quote(c, self.req)
            self.assertEqual(cost.call_count, 3)
        self.assertEqual(c.calls, 0)

    def test_truncated_dbn_never_passes_validation(self):
        path = self.root / 'full'
        fixture(path, self.req)
        raw = zstandard.ZstdDecompressor().decompress(path.read_bytes())
        broken = self.root / 'broken'
        with broken.open('xb') as f:
            f.write(raw[:-1])
        with self.assertRaises(Warning):
            p.validate_dbn(broken, self.req)


if __name__ == '__main__':
    unittest.main()
