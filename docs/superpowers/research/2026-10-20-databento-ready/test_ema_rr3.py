import unittest,random
import ema_rr3 as x

def base(side='LONG',width=81):
 return dict(day='2026-01-02',contract='MNQH6',orb_minutes='1',outcome='SL',entry_row='1',entry_event_ns='10',entry_recv_ns='10',entry_points='100',side=side,orb_high=str(100+width/4),orb_low='100',eligible_ns='9',signal_end_ns='9')
def ticks(prices):return [(i+1,10+i,10+i,i+1,p,1) for i,p in enumerate(prices)]
class Tests(unittest.TestCase):
 def test_long_target(self):
  r=x.simulate(ticks([400,521,522,300]),base(),100)
  self.assertEqual((r['outcome'],r['pnl_points']),('TP',30.375))
 def test_long_half_tick_stop(self):
  r=x.simulate(ticks([400,360,359,481]),base(),100)
  self.assertEqual((r['outcome'],r['exit_row'],r['pnl_points'],r['rounded_pnl_points']),('SL',3,-10.125,-10.25))
 def test_short_target(self):
  r=x.simulate(ticks([400,279,278,500]),base('SHORT'),100)
  self.assertEqual((r['outcome'],r['pnl_points']),('TP',30.375))
 def test_short_stop_gap(self):
  r=x.simulate(ticks([400,450,319]),base('SHORT'),100)
  self.assertEqual((r['pnl_points'],r['observed_pnl_points']),(-10.125,-12.5))
 def test_cutoff_open(self):
  r=x.simulate(ticks([400,401,522]),base(),12)
  self.assertEqual(r['outcome'],'OPEN_AT_CUTOFF');self.assertEqual(r['pnl_points'],'')
 def test_excluded_passthrough(self):
  b=base();b.update(entry_row='',outcome='EXCLUDED')
  self.assertEqual(x.simulate([],b,100)['outcome'],'EXCLUDED')
 def test_wrong_entry_rejected(self):
  with self.assertRaises(AssertionError):x.simulate(ticks([401,481]),base(),100)
 def test_random_first_passage(self):
  rng=random.Random(22)
  for i in range(2000):
   width=rng.randint(1,500);side=rng.choice(['LONG','SHORT']);ps=[400]
   for j in range(100):ps.append(ps[-1]+rng.randint(-30,30))
   x.simulate(ticks(ps),base(side,width),1000)
if __name__=='__main__':unittest.main()

class ThreeRTests(unittest.TestCase):
 def test_two_R_touch_does_not_exit_before_stop(self):
  r=x.simulate(ticks([400,481,359]),base(),100)
  self.assertEqual((r['outcome'],r['pnl_points']),('SL',-10.125))
