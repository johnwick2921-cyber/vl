import unittest
from first_fill import Tick,simulate
class FirstFillTests(unittest.TestCase):
    def test_unfilled_opposite_cross_switches_side(self):
        ticks=[Tick(i,i*1000,i*1000+1,p) for i,p in enumerate([1800,1950,2000,1990,2058])]
        r=simulate(ticks,0,100000,list(range(1000,2000)),15)
        self.assertEqual(r['side'],1)
        self.assertEqual(r['entry_row'],3)
        self.assertEqual(r['signal_row'],1)
        self.assertEqual(len(r['crosses']),2)
        self.assertEqual(r['outcome'],'TP')
    def test_stops_after_first_filled_trade(self):
        ticks=[Tick(i,i*1000,i*1000+1,p) for i,p in enumerate([1100,1020,960,900,980,920])]
        r=simulate(ticks,0,100000,[1000]*1000,15)
        self.assertEqual((r['entry_row'],r['exit_row'],r['outcome']),(1,2,'SL'))
    def test_no_forced_entry_without_cross(self):
        r=simulate([Tick(0,0,1,1000)],0,100000,[1000]*1000,15)
        self.assertIsNone(r['entry_row'])
        self.assertEqual(r['outcome'],'NO_CROSS')
if __name__=='__main__':unittest.main()
