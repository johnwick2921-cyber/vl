import unittest,random
import ema_box_lock2025 as x

def base(side='LONG',width=240):
 return dict(day='2026-01-02',contract='MNQH6',orb_minutes='1',outcome='SL',entry_row='1',entry_event_ns='10',entry_recv_ns='10',entry_points='100',side=side,orb_high=str(100+width/4),orb_low='100',eligible_ns='9',signal_end_ns='9')
def ticks(prices):return [(i+1,10+i,10+i,i+1,p,1) for i,p in enumerate(prices)]
class Tests(unittest.TestCase):
 def test_locks_both_sides(self):
  for lock in (20,25):
   for side in ('LONG','SHORT'):
    s=1 if side=='LONG' else -1
    r=x.simulate(ticks([400,400+s*120,400+s*lock*4]),base(side),100,2,lock)
    self.assertEqual((r['outcome'],r['lock_row'],r['pnl_points']),('LOCK',2,lock))
 def test_wait_for_one_R(self):
  r=x.simulate(ticks([400,519,480,279]),base(),100,2,20)
  self.assertEqual((r['outcome'],r['lock_row']),('SL',''))
 def test_not_continuous_trailing(self):
  r=x.simulate(ticks([400,520,630,510,640]),base(),100,2,25)
  self.assertEqual(r['outcome'],'TP')
 def test_target_first(self):
  r=x.simulate(ticks([400,640,480]),base(),100,2,20)
  self.assertEqual((r['outcome'],r['lock_row']),('TP',''))
 def test_small_R_requires_valid_stop(self):
  r=x.simulate(ticks([400,440,480,400,359]),base(width=80),100,2,25)
  self.assertEqual((r['outcome'],r['lock_row']),('TP',''))
  r=x.simulate(ticks([400,460,500,400,339]),base(width=120),100,2,25)
  self.assertEqual((r['outcome'],r['lock_row']),('SL',''))
  r=x.simulate(ticks([400,460,501,500]),base(width=120),100,2,25)
  self.assertEqual((r['outcome'],r['lock_row'],r['pnl_points']),('LOCK',3,25))
 def test_equal_R_and_lock_needs_tick(self):
  r=x.simulate(ticks([400,480,400,319]),base(width=160),100,2,20)
  self.assertEqual(r['outcome'],'SL')
 def test_gap_honesty(self):
  r=x.simulate(ticks([400,520,390]),base(),100,2,25)
  self.assertEqual((r['pnl_points'],r['observed_pnl_points']),(25,-2.5))
 def test_cutoff(self):
  r=x.simulate(ticks([400,520,480]),base(),12,2,20)
  self.assertEqual(r['outcome'],'OPEN_AT_CUTOFF')
 def test_same_timestamp_next_record(self):
  t=[(r[0],10,10,r[3],r[4],1) for r in ticks([400,520,480])]
  self.assertEqual(x.simulate(t,base(),100,2,20)['exit_row'],3)
 def test_wrong_entry(self):
  with self.assertRaises(AssertionError):x.simulate(ticks([401,520]),base(),100,2,20)
 def test_no_entry(self):
  b=base();b.update(entry_row='',outcome='NO_SIGNAL')
  self.assertEqual(x.simulate([],b,100,2,20)['outcome'],'NO_SIGNAL')
 def test_rr3_rejected(self):
  with self.assertRaises(AssertionError):x.simulate(ticks([400,520]),base(),100,3,20)
 def test_random_oracle(self):
  rng=random.Random(222025)
  for _ in range(4000):
   ps=[400]
   for i in range(100):ps.append(ps[-1]+rng.randint(-30,30))
   x.simulate(ticks(ps),base(rng.choice(['LONG','SHORT']),rng.randint(1,500)),1000,2,rng.choice([20,25]))
if __name__=='__main__':unittest.main()
