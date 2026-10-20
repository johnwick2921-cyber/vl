import unittest,random
from unittest.mock import patch
import body_retest as v
M=v.e.MINUTE

def tape(op=1100,cl=1140,wicks=(1200,1050),m=1,short=False,after=(1120,1240)):
    raw=[(0,1000),(1,1120),(m*M,op),(m*M+M//4,wicks[0]),(2*m*M-2,wicks[1]),(2*m*M-1,cl)]
    raw += [(2*m*M+i,x) for i,x in enumerate(after)]
    return [(i,t,t,i,2120-x if short else x,1) for i,(t,x) in enumerate(raw)]

class Tests(unittest.TestCase):
    def check(self,op=1100,cl=1140,m=1,short=False,rule='half_body',mode='box',after=(1120,1240),wicks=(1200,1050)):
        return v.checked(tape(op,cl,wicks,m,short,after),0,390*M,m,mode,rule)
    def test_half_exactly_50_percent_long_short_all_frames(self):
        for m in (1,2,5):
            for short in (False,True):
                r=self.check(m=m,short=short)
                self.assertEqual(r['outside_fraction'],.5);self.assertEqual(r['confirm_row'],5)
                self.assertEqual(r['entry_row'],6);self.assertEqual(r['outcome'],'TP')
                self.assertEqual(r['eligible_event_ns'],2*m*M)
    def test_below_half_does_not_qualify(self):
        t=tape(1100,1139,after=())
        self.assertIsNone(v.confirmation(t,0,3*M,1,1120,1000,'half_body'))
    def test_full_body_both_endpoints_strictly_outside(self):
        for short in (False,True):
            r=self.check(1121,1140,short=short,rule='full_body')
            self.assertEqual(r['confirm_row'],5);self.assertEqual(r['outside_fraction'],1)
        self.assertIsNone(v.confirmation(tape(1120,1140,after=()),0,3*M,1,1120,1000,'full_body'))
    def test_wicks_are_not_body(self):
        self.assertIsNone(v.confirmation(tape(1100,1110,after=()),0,3*M,1,1120,1000,'half_body'))
        r=self.check(1121,1140,rule='full_body',wicks=(1300,900))
        self.assertEqual(r['outcome'],'TP')
    def test_doji_excluded(self):
        self.assertIsNone(v.confirmation(tape(1140,1140,after=()),0,3*M,1,1120,1000,'full_body'))
    def test_must_wait_closed_bar_no_same_bar_fill(self):
        r=self.check(wicks=(1120,1120))
        self.assertEqual(r['entry_row'],6)
        self.assertGreaterEqual(r['entry_event_ns'],r['confirm_end_ns'])
    def test_confirmation_must_be_received(self):
        t=tape(after=(1120,1120,1240));x=t[5];t[5]=(x[0],x[1],2*M+1,x[3],x[4],x[5])
        self.assertEqual(v.checked(t,0,390*M,1,'box')['entry_row'],7)
    def test_no_retest_and_cutoff(self):
        self.assertEqual(self.check(after=(1150,1240))['outcome'],'NO_RETEST')
        t=tape();x=t[6];t[6]=(x[0],390*M,390*M,x[3],x[4],x[5]);t=t[:7]
        self.assertEqual(v.checked(t,0,390*M,1,'box')['outcome'],'NO_RETEST')
    def test_stop_be_gap_and_no_reentry(self):
        self.assertEqual(self.check(mode='half_box',after=(1120,1060,1240))['pnl_planned_points'],-15)
        self.assertEqual(self.check(mode='20_to_BE',after=(1120,1200,1120,1240))['outcome'],'BE')
        r=self.check(after=(960,1240));self.assertEqual(r['outcome'],'SL');self.assertEqual(r['entry_row'],r['exit_row'])
    def test_close_direction_not_color(self):
        r=self.check(1160,1140,rule='full_body')
        self.assertEqual(r['side'],'LONG')  # no extra candle-color filter requested
    def test_day_callsite(self):
        with patch.object(v.e,'read_day',return_value=({'reason':'','contract':'MNQZ5'},tape())),patch.object(v.e,'boundaries',return_value=(0,0,390*M)):
            _,rows=v.day_job(('/unused','2025-10-20'))
        self.assertEqual(len(rows),18)
    def test_random_independent_oracle(self):
        rng=random.Random(20261022)
        for _ in range(150):
            for m in (1,2,5):
                ticks=[(0,0,0,0,1000,1),(1,1,1,1,1120,1)]
                for i in range(2,42):
                    ts=m*M+(i-2)*m*M//4
                    ticks.append((i,ts,ts,i,rng.randint(850,1300),1))
                for rule in ('half_body','full_body'):
                    for mode in v.MODES:
                        v.checked(ticks,0,390*M,m,mode,rule)
                        mirrored=[(*t[:4],2120-t[4],t[5]) for t in ticks]
                        v.checked(mirrored,0,390*M,m,mode,rule)

if __name__=='__main__':unittest.main()
