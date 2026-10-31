import unittest
from engine import simulate,Tick
class FirstCrossTests(unittest.TestCase):
    def test_reversal_before_touch_consumes_first_cross(self):
        t=[Tick(i,i*1000,i*1000+1,p) for i,p in enumerate([1800,1950,2000,1800,1990])]
        r=simulate(t,0,100000,list(range(1000,2000)),15)
        self.assertEqual(r['signal_row'],0)
        self.assertEqual(r['cancel_row'],1)
        self.assertEqual(r['outcome'],'FIRST_CROSS_CANCELLED')
        self.assertIsNone(r['entry_row'])
    def test_short_limit_retest_wins_without_reversal(self):
        t=[Tick(i,i*1000,i*1000+1,p) for i,p in enumerate([1800,1990,1800])]
        r=simulate(t,0,100000,list(range(1000,2000)),15)
        self.assertEqual(r['entry_row'],1)
        self.assertEqual(r['side'],-1)
        self.assertEqual(r['outcome'],'TP')
if __name__=='__main__':unittest.main()
