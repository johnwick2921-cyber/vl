import unittest
from engine import Tick,simulate
W=[1000]*1000
class AtomicTests(unittest.TestCase):
    def test_cross_waits_event_end(self):
        a=[Tick(0,0,1,1100,(0,10),False),Tick(1,0,1,1000,(0,10),True)]
        r=simulate(a,0,1000,W,15)
        self.assertEqual(r['outcome'],'NO_CROSS')
    def test_no_same_event_entry_after_cross(self):
        a=[Tick(0,0,1,1000,(0,10),False),Tick(1,0,1,1100,(0,10),True)]
        r=simulate(a,0,1000,W,15)
        self.assertEqual(r['signal_row'],1)
        self.assertIsNone(r['entry_row'])
    def test_resting_fill_before_event_end(self):
        a=[Tick(0,0,1,1100,(0,10),True),Tick(1,10,11,1020,(10,11),False),Tick(2,10,11,1080,(10,11),True)]
        r=simulate(a,0,1000,W,15)
        self.assertEqual(r['entry_row'],1)
        self.assertEqual(r['outcome'],'OPEN_AT_CUTOFF')
    def test_exit_next_event(self):
        a=[Tick(0,0,1,1100,(0,10),True),Tick(1,10,11,1020,(10,11),False),Tick(2,10,11,1080,(10,11),True),Tick(3,20,21,1080,(20,12),True)]
        self.assertEqual(simulate(a,0,1000,W,15)['exit_row'],3)
if __name__=='__main__':unittest.main()
