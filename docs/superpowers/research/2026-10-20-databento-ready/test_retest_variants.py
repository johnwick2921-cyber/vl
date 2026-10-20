import random
import unittest
from unittest.mock import patch
import retest_variants as v
M=v.e.MINUTE

def tape(gains,width=120,short=False,minutes=1):
    out=[(0,0,0,0,1000,1),(1,1,1,1,1000+width,1)]
    for i,g in enumerate(gains,2):
        price=1000+width+g if not short else 1000-g
        out.append((i,minutes*M+i,minutes*M+i,i,price,1))
    return out

class Tests(unittest.TestCase):
    def check(self,g,mode='box',w=120,short=False,through=False,m=1):
        return v.checked(tape(g,w,short,m),0,10*M,m,mode,through)
    def test_break_then_retest_limit_entry_long_short(self):
        for short in (False,True):
            r=self.check([0,1,8,0,120],short=short)
            self.assertEqual((r['break_row'],r['entry_row'],r['exit_row']),(3,5,6))
            self.assertEqual(r['entry_points'],250 if short else 280)
            self.assertEqual(r['pnl_planned_points'],30)
    def test_no_break_or_no_return(self):
        self.assertEqual(self.check([0,-10,0])['outcome'],'NO_BREAKOUT')
        r=self.check([1,10,120]);self.assertEqual(r['outcome'],'NO_RETEST');self.assertEqual(r['entry_row'],'')
    def test_through_requires_extra_tick_and_price_stays_limit(self):
        self.assertEqual(self.check([1,0,120],through=True)['outcome'],'NO_RETEST')
        r=self.check([1,-1,120],through=True)
        self.assertEqual(r['entry_points'],280);self.assertEqual(r['entry_trigger_points'],279.75)
    def test_gap_crossing_stop_on_fill_cannot_be_rescued(self):
        for short in (False,True):
            r=self.check([1,-150,120],short=short)
            self.assertEqual(r['outcome'],'SL');self.assertEqual(r['entry_row'],r['exit_row'])
            self.assertEqual(r['pnl_planned_points'],-30);self.assertEqual(r['pnl_observed_points'],-37.5)
    def test_half_box_and_odd_tick_width(self):
        r=self.check([1,0,-61,121],'half_box',121)
        self.assertEqual(r['pnl_planned_points'],-15.125);self.assertEqual(r['pnl_tick_rounded_points'],-15.25)
    def test_be_arm_only_at_20_points_and_future_record(self):
        for short in (False,True):
            r=self.check([1,0,80,0,120],'20_to_BE',short=short)
            self.assertEqual((r['outcome'],r['be_row'],r['exit_row']),('BE',4,5))
            self.assertEqual(self.check([1,0,79,0,-80,120],'20_to_BE',short=short)['outcome'],'SL')
    def test_target_at_or_before_be(self):
        for width in (40,80):
            r=self.check([1,0,80,0],'20_to_BE',width)
            self.assertEqual(r['outcome'],'TP');self.assertEqual(r['be_row'],'')
    def test_no_second_trade_after_stop(self):
        r=self.check([1,0,-120,120,0,120])
        self.assertEqual((r['outcome'],r['exit_row']),('SL',4))
    def test_equal_timestamp_record_order(self):
        t=tape([1,0,80,0,120]);t=[(x[0],M+2,M+2,2,x[4],1) if x[0]>=2 else x for x in t]
        self.assertEqual(v.checked(t,0,10*M,1,'20_to_BE')['outcome'],'BE')
    def test_cutoff_and_unclosed(self):
        t=tape([1,0,120]);x=t[-1];t[-1]=(x[0],10*M,10*M,x[3],x[4],1)
        self.assertEqual(v.checked(t,0,10*M,1,'box')['outcome'],'OPEN_AT_CUTOFF')
        t=tape([1,0]);x=t[-1];t[-1]=(x[0],10*M,10*M,x[3],x[4],1)
        self.assertEqual(v.checked(t,0,10*M,1,'box')['outcome'],'NO_RETEST')
    def test_late_formation_and_missing_or_zero_range(self):
        t=tape([1,0,120]);t[1]=(1,1,M,1,1120,1)
        self.assertEqual(v.checked(t,0,10*M,1,'box')['outcome'],'EXCLUDED')
        self.assertEqual(v.checked([],0,10*M,1,'box')['outcome'],'EXCLUDED')
        self.assertEqual(self.check([1,0],w=0)['outcome'],'EXCLUDED')
    def test_break_exactly_at_orb_end(self):
        t=tape([1,0,120]);x=t[2];t[2]=(2,M,M,2,x[4],1)
        self.assertEqual(v.checked(t,0,10*M,1,'box')['break_row'],2)
    def test_day_job_calls_all_variants(self):
        ticks=tape([1,0,120]);c={'reason':'','contract':'MNQZ5'}
        with patch.object(v.e,'read_day',return_value=(c,ticks)),patch.object(v.e,'boundaries',return_value=(0,0,10*M)):
            census,rows=v.day_job(('/not/read','2025-10-20'))
        self.assertEqual(len(rows),18)
        self.assertEqual(len({(r['orb_minutes'],r['mode'],r['fill_rule']) for r in rows}),18)
    def test_random_oracle_all_timeframes_sides_and_fill_rules(self):
        rng=random.Random(20261021)
        for _ in range(300):
            w=rng.randint(1,400);g=[rng.randint(-w-100,w+100) for i in range(30)]
            for m in (1,2,5):
                for short in (False,True):
                    for mode in v.MODES:
                        for through in (False,True):self.check(g,mode,w,short,through,m)

if __name__=='__main__':unittest.main()
