import unittest,random
import ema_halfbox_lock as x

def base(side='LONG',width=80):
 return dict(day='2026-01-02',contract='MNQH6',orb_minutes='1',outcome='SL',entry_row='1',entry_event_ns='10',entry_recv_ns='10',entry_points='100',side=side,orb_high=str(100+width/4),orb_low='100',eligible_ns='9',signal_end_ns='9')
def ticks(prices):return [(i+1,10+i,10+i,i+1,p,1) for i,p in enumerate(prices)]
class Tests(unittest.TestCase):
 def test_long_lock(self):
  r=x.simulate(ticks([400,441,440,480]),base(),100,2)
  self.assertEqual((r['outcome'],r['lock_row'],r['exit_row'],r['pnl_points']),('LOCK',2,3,10))
 def test_short_lock(self):
  r=x.simulate(ticks([400,359,360]),base('SHORT'),100,2)
  self.assertEqual((r['outcome'],r['pnl_points']),('LOCK',10))
 def test_one_R_touch_not_activation(self):
  r=x.simulate(ticks([400,440,400,360]),base(),100,2)
  self.assertEqual((r['outcome'],r['lock_row']),('SL',''))
 def test_target_jump(self):
  r=x.simulate(ticks([400,480,440]),base(),100,2)
  self.assertEqual((r['outcome'],r['lock_row'],r['pnl_points']),('TP','',20))
 def test_lock_then_target(self):
  r=x.simulate(ticks([400,441,450,480]),base(),100,2)
  self.assertEqual(r['outcome'],'TP')
 def test_half_tick_trigger_and_rounding(self):
  r=x.simulate(ticks([400,441,400,359]),base(width=81),100,2)
  self.assertEqual(r['outcome'],'SL')
  r=x.simulate(ticks([400,442,441,440]),base(width=81),100,2)
  self.assertEqual((r['outcome'],r['exit_row'],r['pnl_points'],r['rounded_pnl_points']),('LOCK',4,10.125,10))
 def test_no_continued_trailing(self):
  r=x.simulate(ticks([400,441,479,442,480]),base(),100,2)
  self.assertEqual(r['outcome'],'TP')
 def test_cutoff_open(self):
  r=x.simulate(ticks([400,441,440]),base(),12,2)
  self.assertEqual(r['outcome'],'OPEN_AT_CUTOFF')
 def test_gap_loss_is_separate(self):
  r=x.simulate(ticks([400,441,390]),base(),100,2)
  self.assertEqual((r['pnl_points'],r['observed_pnl_points']),(10,-2.5))
 def test_wrong_entry(self):
  with self.assertRaises(AssertionError):x.simulate(ticks([401,480]),base(),100,2)
 def test_same_timestamp_order(self):
  t=[(r[0],10,10,r[3],r[4],1) for r in ticks([400,441,440])]
  self.assertEqual(x.simulate(t,base(),100,2)['outcome'],'LOCK')
 def test_no_signal(self):
  b=base();b.update(entry_row='',outcome='NO_SIGNAL')
  self.assertEqual(x.simulate([],b,100,2)['outcome'],'NO_SIGNAL')
 def test_rr3_refused(self):
  with self.assertRaises(AssertionError):x.simulate(ticks([400,441]),base(),100,3)
 def test_random_oracle(self):
  rng=random.Random(221122)
  for _ in range(4000):
   ps=[400]
   for i in range(100):ps.append(ps[-1]+rng.randint(-30,30))
   x.simulate(ticks(ps),base(rng.choice(['LONG','SHORT']),rng.randint(1,500)),1000,2)
if __name__=='__main__':unittest.main()
