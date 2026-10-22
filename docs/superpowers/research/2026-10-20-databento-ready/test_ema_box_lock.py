import unittest,random
import ema_box_lock as x

def base(side='LONG',width=160):
 return dict(day='2026-01-02',contract='MNQH6',orb_minutes='1',outcome='SL',entry_row='1',entry_event_ns='10',entry_recv_ns='10',entry_points='100',side=side,orb_high=str(100+width/4),orb_low='100',eligible_ns='9',signal_end_ns='9')
def ticks(prices):return [(i+1,10+i,10+i,i+1,p,1) for i,p in enumerate(prices)]
class Tests(unittest.TestCase):
 def test_all_locks_both_targets_and_sides(self):
  for rr in (2,3):
   for lock in (5,10,15):
    for side in ('LONG','SHORT'):
     s=1 if side=='LONG' else -1
     r=x.simulate(ticks([400,400+s*80,400+s*lock*4,400+s*rr*80]),base(side),100,rr,lock)
     self.assertEqual((r['outcome'],r['lock_row'],r['exit_row'],r['pnl_points']),('LOCK',2,3,lock))
 def test_before_one_R_original_stop(self):
  r=x.simulate(ticks([400,479,420,319]),base(),100,2,5)
  self.assertEqual((r['outcome'],r['lock_row'],r['pnl_points']),('SL','',-20))
 def test_tp_jump_priority(self):
  r=x.simulate(ticks([400,640,400]),base(),100,3,15)
  self.assertEqual((r['outcome'],r['lock_row'],r['pnl_points']),('TP','',60))
 def test_lock_then_target(self):
  r=x.simulate(ticks([400,480,421,560]),base(),100,2,5)
  self.assertEqual(r['outcome'],'TP')
 def test_gap_can_lose_despite_lock(self):
  r=x.simulate(ticks([400,480,395]),base(),100,3,15)
  self.assertEqual((r['outcome'],r['pnl_points'],r['observed_pnl_points']),('LOCK',15,-1.25))
 def test_small_R_waits_above_lock(self):
  r=x.simulate(ticks([400,432,460,400,367]),base(width=64),100,2,15)
  self.assertEqual((r['outcome'],r['lock_row']),('SL',''))
  r=x.simulate(ticks([400,432,461,460]),base(width=64),100,2,15)
  self.assertEqual((r['outcome'],r['lock_row'],r['pnl_points']),('LOCK',3,15))
 def test_small_R_target_before_lock(self):
  r=x.simulate(ticks([400,416,432,460]),base(width=32),100,2,15)
  self.assertEqual((r['outcome'],r['lock_row'],r['pnl_points']),('TP','',8))
 def test_R_equal_lock_still_needs_tick(self):
  r=x.simulate(ticks([400,420,400,379]),base(width=40),100,3,5)
  self.assertEqual(r['outcome'],'SL')
 def test_half_tick_activation(self):
  r=x.simulate(ticks([400,440,420,359]),base(width=81),100,3,5)
  self.assertEqual(r['outcome'],'SL')
  r=x.simulate(ticks([400,441,420]),base(width=81),100,3,5)
  self.assertEqual(r['outcome'],'LOCK')
 def test_same_timestamp_next_record(self):
  t=[(r[0],10,10,r[3],r[4],1) for r in ticks([400,480,420])]
  r=x.simulate(t,base(),100,2,5)
  self.assertEqual((r['outcome'],r['lock_row'],r['exit_row']),('LOCK',2,3))
 def test_cutoff_no_invented_fill(self):
  r=x.simulate(ticks([400,480,420]),base(),12,2,5)
  self.assertEqual((r['outcome'],r['lock_row']),('OPEN_AT_CUTOFF',2))
 def test_no_further_trailing(self):
  r=x.simulate(ticks([400,480,550,440,560]),base(),100,2,5)
  self.assertEqual(r['outcome'],'TP')
 def test_wrong_entry_rejected(self):
  with self.assertRaises(AssertionError):x.simulate(ticks([401,480]),base(),100,2,5)
 def test_no_entry_preserved(self):
  b=base();b.update(entry_row='',outcome='NO_SIGNAL')
  self.assertEqual(x.simulate([],b,100,3,15)['outcome'],'NO_SIGNAL')
 def test_random_oracle(self):
  rng=random.Random(2251015)
  for i in range(6000):
   price=400;t=[price]
   for _ in range(100):price+=rng.randint(-30,30);t.append(price)
   x.simulate(ticks(t),base(rng.choice(['LONG','SHORT']),rng.randint(1,500)),1000,rng.choice([2,3]),rng.choice([5,10,15]))
if __name__=='__main__':unittest.main()
