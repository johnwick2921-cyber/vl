import json
from pathlib import Path
import random
import tempfile
import unittest
from types import SimpleNamespace as Obj

import databento_dbn as dbn
import zstandard
import tick_orb as e


OPEN = e.boundaries('2025-10-20')[1]
CLOSE = e.boundaries('2025-10-20')[2]


def tape(prices, times=None):
    if times is None:
        times = [OPEN, OPEN+1, *[OPEN+e.MINUTE+i for i in range(len(prices)-2)]]
    return [(i, t, t+100, i, price, 1) for i, (t, price) in enumerate(zip(times, prices))]


def oracle(prices, through=False):
    """Independent index-of-first-passage oracle for unique ordered events."""
    top, bottom = max(prices[:2]), min(prices[:2])
    width = top-bottom
    eligible = [(i, x) for i, x in enumerate(prices[2:], 2) if x > top or x < bottom]
    if not eligible: return 'NO_ENTRY', None, None
    ix, entry = eligible[0]
    sign = 1 if entry > top else -1
    profit = [(i, (x-entry)*sign) for i, x in enumerate(prices) if i > ix]
    stops = [i for i, g in profit if g <= -80]
    targets = [i for i, g in profit if g >= width+int(through)]
    stop_ix = min(stops, default=len(prices))
    target_ix = min(targets, default=len(prices))
    if stop_ix == target_ix: return 'OPEN_AT_CUTOFF', ix, None
    if stop_ix < target_ix: return 'SL', ix, (prices[stop_ix]-entry)*sign/4
    return 'TP', ix, width/4


def dbn_day(root, day='2025-10-20', missing=False, regress=False, flagged=False):
    folder = root/day
    folder.mkdir()
    pre, opened, close = e.boundaries(day)
    meta = dbn.Metadata(dataset='GLBX.MDP3', schema=dbn.Schema.TBBO,
        start=pre-10*e.MINUTE, end=close+10*e.MINUTE,
        stype_in=dbn.SType.RAW_SYMBOL, stype_out=dbn.SType.INSTRUMENT_ID,
        symbols=['MNQZ5'], mappings=[Obj(raw_symbol='MNQZ5', intervals=[Obj(
            start_date=e.date.fromisoformat(day),
            end_date=e.date.fromisoformat(day)+e.timedelta(days=1), symbol='123')])])
    records = []
    times = [pre+1, *[opened+i*e.MINUTE for i in range(390) if not (missing and i == 17)]]
    times.insert(2, opened+1000)
    for i, stamp in enumerate(times):
        price = 100_000 if i < 2 else (100_040 if i == 2 else 100_041 if i == 3 else 100_081)
        event = stamp if not (regress and i == 15) else times[13]
        records.append(dbn.MBP1Msg(publisher_id=1, instrument_id=123,
            ts_event=event, ts_recv=stamp+100, price=price*e.UNIT, size=1,
            action=dbn.Action.TRADE, side=dbn.Side.BID, depth=0, sequence=i,
            flags=1 if flagged and i == 19 else 0,
            levels=dbn.BidAskPair(bid_px=(price-1)*e.UNIT, ask_px=(price+1)*e.UNIT)))
    with (folder/'ticks.dbn.zst').open('xb') as f:
        f.write(zstandard.ZstdCompressor().compress(meta.encode()+b''.join(bytes(x) for x in records)))
    e.p.write_json(folder/'complete.json', dict(day=day, download_complete=True,
                   sha256=e.p.digest(folder/'ticks.dbn.zst'), records=len(records)))
    return folder


class Tests(unittest.TestCase):
    def test_long_and_short_target_stop_mirror(self):
        for values, kind in (([400,440,441,481],'TP'), ([400,440,441,460,361],'SL')):
            for prices in (values, [1000-x for x in values]):
                r=e.replay(tape(prices),OPEN,CLOSE,1)
                self.assertEqual(r['outcome'],kind)
                self.assertEqual(r['pnl_points'],10 if kind=='TP' else -20)
                if kind=='SL': self.assertTrue(r['comeback_stop'])

    def test_no_reentry_after_first_loss(self):
        r=e.replay(tape([400,440,441,360,700,300]),OPEN,CLOSE,1)
        self.assertEqual((r['outcome'],r['entry_row'],r['exit_row']),('SL',2,3))
        self.assertEqual(r['pnl_points'],-20.25)

    def test_gap_entry_and_stop_use_observed_trade(self):
        r=e.replay(tape([400,440,460,370]),OPEN,CLOSE,1)
        self.assertEqual((r['entry_points'],r['stop_points'],r['target_points']),(115,95,125))
        self.assertEqual(r['pnl_points'],-22.5)
        self.assertEqual(r['net_2pt_scenario'],-24.5)

    def test_touch_vs_trade_through(self):
        rows=tape([400,440,441,481,361])
        self.assertEqual(e.replay(rows,OPEN,CLOSE,1)['outcome'],'TP')
        self.assertEqual(e.replay(rows,OPEN,CLOSE,1,True)['outcome'],'SL')

    def test_exact_boundary_is_not_breakout(self):
        r=e.replay(tape([400,440,440,400,420]),OPEN,CLOSE,1)
        self.assertEqual(r['outcome'],'NO_ENTRY')

    def test_last_orb_tick_excluded_from_entry_and_next_tick_included(self):
        for m in (1,2,5):
            end=OPEN+m*e.MINUTE
            rows=tape([400,440,450,451,501],[OPEN,OPEN+1,end-1000,end,end+1])
            r=e.replay(rows,OPEN,CLOSE,m)
            self.assertEqual(r['outcome'],'TP')
            self.assertEqual((r['orb_high'],r['entry_row'],r['outcome']),(112.5,3,'TP'))

    def test_late_range_data_excluded(self):
        rows=tape([400,440,441,481])
        row=list(rows[1]);row[2]=OPEN+e.MINUTE;rows[1]=tuple(row)
        self.assertEqual(e.replay(rows,OPEN,CLOSE,1)['outcome'],'EXCLUDED')

    def test_no_forced_close_or_next_session_exit(self):
        rows=tape([400,440,441,481],[OPEN,OPEN+1,OPEN+e.MINUTE,CLOSE])
        r=e.replay(rows,OPEN,CLOSE,1)
        self.assertEqual(r['outcome'],'OPEN_AT_CUTOFF')
        self.assertEqual(r['pnl_points'],'')

    def test_same_event_sequence_conflict_not_guessed(self):
        rows=tape([400,440,441,361])
        row=list(rows[3]);row[1:4]=rows[2][1:4];rows[3]=tuple(row)
        self.assertEqual(e.replay(rows,OPEN,CLOSE,1,conservative_ties=True)['outcome'],'UNRESOLVED')
        self.assertEqual(e.replay(rows,OPEN,CLOSE,1)['outcome'],'SL')
        rows=tape([400,440,441,481,361])
        row=list(rows[4]);row[1:4]=rows[3][1:4];rows[4]=tuple(row)
        self.assertEqual(e.replay(rows,OPEN,CLOSE,1,conservative_ties=True)['outcome'],'UNRESOLVED')
        self.assertEqual(e.replay(rows,OPEN,CLOSE,1)['outcome'],'TP')

    def test_identical_price_tie_and_later_same_timestamp_sequence(self):
        rows=tape([400,440,441,441,481])
        row=list(rows[3]);row[1:4]=rows[2][1:4];rows[3]=tuple(row)
        self.assertEqual(e.replay(rows,OPEN,CLOSE,1)['outcome'],'TP')

    def test_zero_and_missing_range(self):
        self.assertEqual(e.replay(tape([400,400,401]),OPEN,CLOSE,1)['outcome'],'EXCLUDED')
        self.assertEqual(e.replay([],OPEN,CLOSE,1)['outcome'],'EXCLUDED')

    def test_dst_changes_utc_not_houston_open(self):
        for day,hour in (('2025-10-20',13),('2025-11-10',14),('2026-03-10',13)):
            from datetime import timezone
            opened=e.boundaries(day)[1]
            self.assertEqual(e.datetime.fromtimestamp(opened/1e9,timezone.utc).hour,hour)

    def test_random_first_passage_oracle(self):
        rng=random.Random(20261020)
        for _ in range(3000):
            lo=rng.randrange(400,800); hi=lo+rng.randrange(1,240)
            values=[lo,hi]
            for j in range(100): values.append(values[-1]+rng.randrange(-100,101))
            for through in (False,True):
                expected,entry,pnl=oracle(values,through)
                got=e.replay(tape(values),OPEN,CLOSE,1,through)
                self.assertEqual(got['outcome'],expected)
                if entry is not None: self.assertEqual(got['entry_row'],entry)
                if pnl is not None: self.assertEqual(got['pnl_points'],pnl)

    def test_real_dbn_callsite_and_no_overwrite(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);dbn_day(root)
            result=e.run(root,root/'out','2025-10-20','2025-10-21')
            self.assertEqual(result['included_sessions'],1)
            self.assertEqual(result['summaries'][0]['counts'],{'TP':1})
            with self.assertRaises(FileExistsError): e.run(root,root/'out','2025-10-20','2025-10-21')

    def test_data_quality_exclusions_and_hash_gate(self):
        for name in ('missing','regress','flagged'):
            with tempfile.TemporaryDirectory() as tmp:
                root=Path(tmp);folder=dbn_day(root,**{name:True})
                census,_=e.read_day(folder,'2025-10-20')
                self.assertTrue(census['reason'])
                # Valid empty compressed frame changes bytes without relying on
                # a later decoder failure to catch a missing hash comparison.
                with (folder/'ticks.dbn.zst').open('ab') as f:
                    f.write(zstandard.ZstdCompressor().compress(b''))
                with self.assertRaisesRegex(e.p.Stop,'Hash mismatch'): e.read_day(folder,'2025-10-20')

    def test_missing_downloads_block_report(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)
            with self.assertRaisesRegex(e.p.Stop,'missing'): e.run(root,root/'out')
            self.assertFalse((root/'out').exists())

    def test_summary_denominators_include_unknown_and_open(self):
        rows=[]
        for kind in ('TP','SL','UNRESOLVED','OPEN_AT_CUTOFF','NO_ENTRY','EXCLUDED'):
            rows.append(dict(orb_minutes=1,target_rule='touch',ordering='provider_record_order',outcome=kind,pnl_points=10,
                             net_2pt_scenario=8,comeback_stop=kind=='SL'))
        r=e.summarize(rows)[0]
        self.assertEqual((r['entry_or_unresolved'],r['tp_pct_of_entries'],r['tp_pct_of_tp_sl_only']),
                         (4,25,50))
        self.assertEqual(r['closed_only_gross_points'],20)


if __name__=='__main__': unittest.main()
