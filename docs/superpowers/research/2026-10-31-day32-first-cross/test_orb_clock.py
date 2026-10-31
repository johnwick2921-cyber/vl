import sys,unittest
from pathlib import Path
sys.path.append('/home/hoang/vl-1017-databento-ready/docs/superpowers/research/2026-10-20-databento-ready')
import orb_ema_receipt as e
import ema_orb as old

class Clock(unittest.TestCase):
    def test_received_next_minute_is_next_bar(self):
        m=e.e.MINUTE
        ticks=[(0,0,1,1,400,1),(1,m-1,m+1,2,440,1),(2,m+2,m+3,3,450,1)]
        self.assertEqual([[t[0] for t in b] for _,b in e.bars(ticks,0,3*m,1)],[[0],[1,2]])
        self.assertNotEqual(e.bars(ticks,0,3*m,1),old.bars(ticks,0,3*m,1))

    def test_receipt_cutoff_excludes_late_record(self):
        m=e.e.MINUTE
        ticks=[(0,0,1,1,400,1),(1,2*m-1,2*m+1,2,450,1)]
        self.assertEqual(len(e.bars(ticks,0,2*m,1)),1)

    def test_receipt_break_production_and_oracle(self):
        m=e.e.MINUTE
        ticks=[(0,0,1,1,400,1),(1,5,6,2,404,1),
               (2,m-1,m+1,3,500,1),(3,2*m,2*m+1,4,501,1),
               (4,2*m+2,2*m+3,5,581,1)]
        r=e.checked(ticks,0,4*m,1,20,[400]*300)
        self.assertEqual(r['orb_high'],101)
        self.assertEqual(r['entry_row'],3)
        self.assertEqual(r['outcome'],'TP')
        self.assertNotEqual(old.checked(ticks,0,4*m,1,20,[400]*300)['outcome'],'TP')

if __name__=='__main__':unittest.main()
