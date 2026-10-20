import random
import unittest
import stop_variants as v

M=v.e.MINUTE

def tape(prices,width=120,short=False):
    # Form range 1000..1000+width before one-minute decision.
    out=[(0,0,0,0,1000,1),(1,1,1,1,1000+width,1)]
    for i,p in enumerate(prices,2):
        price=1000+width+1+p if not short else 999-p
        out.append((i,M+i,M+i,i,price,1))
    return out

class Tests(unittest.TestCase):
    def check(self,prices,mode,w=120,short=False):
        return v.checked(tape(prices,w,short),0,10*M,1,mode)
    def test_box_equal_risk_and_target_long_short(self):
        for short in (False,True):
            self.assertEqual(self.check([0,120],'box',short=short)['pnl_planned_points'],30)
            self.assertEqual(self.check([0,-120,120],'box',short=short)['pnl_planned_points'],-30)
    def test_half_box(self):
        self.assertEqual(self.check([0,-59,120],'half_box')['outcome'],'TP')
        self.assertEqual(self.check([0,-60,120],'half_box')['pnl_planned_points'],-15)
    def test_half_tick_nominal_vs_rounded(self):
        r=self.check([0,-61],'half_box',121)
        self.assertEqual(r['pnl_planned_points'],-15.125)
        self.assertEqual(r['pnl_tick_rounded_points'],-15.25)
    def test_be_threshold_and_no_reentry(self):
        for short in (False,True):
            r=self.check([0,80,0,120],'20_to_BE',short=short)
            self.assertEqual(r['outcome'],'BE');self.assertEqual(r['pnl_planned_points'],0)
            self.assertEqual(r['exit_row'],4)
            self.assertEqual(self.check([0,79,0,-80,120],'20_to_BE',short=short)['outcome'],'SL')
    def test_be_gap_is_separate(self):
        r=self.check([0,80,-8],'20_to_BE')
        self.assertEqual(r['pnl_planned_points'],0);self.assertEqual(r['pnl_observed_points'],-2)
    def test_target_before_or_at_be_activation(self):
        for w in (40,80):
            r=self.check([0,80,0],'20_to_BE',w)
            self.assertEqual(r['outcome'],'TP');self.assertEqual(r['be_row'],'')
    def test_entry_print_not_exit_and_cutoff(self):
        self.assertEqual(self.check([0],'box')['outcome'],'OPEN_AT_CUTOFF')
        t=tape([0,120]);t[-1]=(3,10*M,10*M,3,t[-1][4],1)
        self.assertEqual(v.checked(t,0,10*M,1,'box')['outcome'],'OPEN_AT_CUTOFF')
    def test_boundary_touch_not_entry_and_late_orb(self):
        t=tape([0,120]);t[2]=(2,M+2,M+2,2,1120,1)
        self.assertEqual(v.checked(t,0,10*M,1,'box')['entry_row'],3)
        t[1]=(1,1,M,1,1120,1)
        self.assertEqual(v.checked(t,0,10*M,1,'box')['outcome'],'EXCLUDED')
    def test_equal_timestamp_preserves_row_order(self):
        t=tape([0,80,0,120]);t=[(x[0],M+2,M+2,2,x[4],1) if x[0]>=2 else x for x in t]
        self.assertEqual(v.checked(t,0,10*M,1,'20_to_BE')['outcome'],'BE')
    def test_random_independent_first_passage(self):
        rng=random.Random(20261020)
        for _ in range(1000):
            w=rng.randint(1,400);prices=[0]+[rng.randint(-500,500) for i in range(30)]
            for short in (False,True):
                for mode in v.MODES:self.check(prices,mode,w,short)

if __name__=='__main__':unittest.main()
