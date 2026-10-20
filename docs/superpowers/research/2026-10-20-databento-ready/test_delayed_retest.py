import unittest,random
from unittest.mock import patch
import delayed_retest as v
M=v.e.MINUTE

def tape(events,m=1,short=False):
    out=[(0,0,0,0,1000,1),(1,1,1,1,1120,1)]
    for i,(sec,g) in enumerate(events,2):
        ts=m*M+int(sec*1e9)
        out.append((i,ts,ts,i,1000-g if short else 1120+g,1))
    return out

class Tests(unittest.TestCase):
    def runpath(self,events,m=1,d=1,mode='box',short=False):
        return v.checked(tape(events,m,short),0,390*M,m,mode,False,d)
    def test_exact_half_and_full_time_each_frame(self):
        for m in (1,2,5):
            for d in (1,2):
                sec=m*30*d
                for short in (False,True):
                    r=self.runpath([(0,1),(sec-1,0),(sec,0),(sec+1,120)],m,d,short=short)
                    self.assertEqual((r['entry_row'],r['exit_row']),(4,5))
                    self.assertEqual(r['pnl_planned_points'],30)
    def test_early_touch_does_not_fill(self):
        r=self.runpath([(0,1),(1,0),(30,1),(40,120)])
        self.assertEqual(r['outcome'],'NO_RETEST');self.assertEqual(r['entry_row'],'')
    def test_already_inside_at_activation_and_gap_stop(self):
        r=self.runpath([(0,1),(30,-150),(31,120)])
        self.assertTrue(r['activation_marketable']);self.assertEqual(r['outcome'],'SL')
        self.assertEqual(r['entry_row'],r['exit_row'])
    def test_half_may_same_candle_full_cannot(self):
        self.assertTrue(self.runpath([(0,1),(30,0),(31,120)])['same_candle_as_break'])
        self.assertFalse(self.runpath([(0,1),(60,0),(61,120)],d=2)['same_candle_as_break'])
    def test_be_and_half_box(self):
        self.assertEqual(self.runpath([(0,1),(30,0),(31,80),(32,0),(33,120)],mode='20_to_BE')['outcome'],'BE')
        self.assertEqual(self.runpath([(0,1),(30,0),(31,-60),(32,120)],mode='half_box')['pnl_planned_points'],-15)
    def test_delay_from_break_not_range_end(self):
        r=self.runpath([(20,1),(30,0),(49,0),(50,0),(51,120)])
        self.assertEqual(r['entry_row'],5)
    def test_late_break_no_eligible_entry(self):
        r=self.runpath([(389*60-10,1)])
        self.assertEqual(r['outcome'],'NO_RETEST')
    def test_day_job_18_variants(self):
        with patch.object(v.e,'read_day',return_value=({'reason':'','contract':'MNQZ5'},tape([(0,1),(30,0),(31,120)]))),patch.object(v.e,'boundaries',return_value=(0,0,390*M)):
            _,rows=v.day_job(('/unused','2025-10-20'))
        self.assertEqual(len(rows),18);self.assertEqual(len({(r['orb_minutes'],r['mode'],r['delay']) for r in rows}),18)
    def test_random_independent_oracle(self):
        rng=random.Random(345)
        for _ in range(300):
            events=[(i*15,rng.randint(-250,250)) for i in range(40)]
            for m in (1,2,5):
                for d in (1,2):
                    for short in (False,True):
                        for mode in v.MODES:self.runpath(events,m,d,mode,short)

if __name__=='__main__':unittest.main()
